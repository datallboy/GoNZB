package publisher

import (
	"context"
	"crypto/ed25519"
	"fmt"
	"testing"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/health"
	"github.com/datallboy/gonzb/internal/gonzbnet/identity"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifestavailability"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/validation"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

func TestPublishOnceSignsStoresAndSkipsUnchangedReleaseCards(t *testing.T) {
	ctx := context.Background()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	store := &fakeStore{
		candidates:       []releasecard.LocalRelease{testPublisherRelease()},
		eventsByBodyHash: make(map[string]string),
	}
	svc := New(node, store, "pool.local")
	svc.now = func() time.Time { return time.Date(2026, 7, 7, 12, 0, 0, 0, time.UTC) }

	first, err := svc.PublishOnce(ctx, 10)
	if err != nil {
		t.Fatalf("first publish: %v", err)
	}
	if first.Scanned != 1 || first.Published != 1 || first.Projected != 1 || first.Skipped != 0 {
		t.Fatalf("unexpected first result: %+v", first)
	}
	if len(store.events) != 1 {
		t.Fatalf("expected one stored event, got %d", len(store.events))
	}
	if store.events[0].EventType != "ReleaseCard" {
		t.Fatalf("expected ReleaseCard event, got %q", store.events[0].EventType)
	}

	second, err := svc.PublishOnce(ctx, 10)
	if err != nil {
		t.Fatalf("second publish: %v", err)
	}
	if second.Scanned != 1 || second.Published != 0 || second.Projected != 1 || second.Skipped != 1 {
		t.Fatalf("unexpected second result: %+v", second)
	}
	if len(store.events) != 1 {
		t.Fatalf("expected unchanged card to reuse stored event, got %d events", len(store.events))
	}
}

func TestPublishOnceUsesScanOutputWithoutIndexerCandidates(t *testing.T) {
	ctx := context.Background()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	scanRelease := testPublisherRelease()
	scanRelease.LocalReleaseID = "scan-1"
	scanRelease.SourceKind = "local_scan_output"
	store := &fakeStore{
		scanCandidates:   []releasecard.LocalRelease{scanRelease},
		eventsByBodyHash: make(map[string]string),
	}
	svc := New(node, store, "pool.local")
	svc.SetManifestAvailabilityPublishing(true)
	svc.now = func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) }

	result, err := svc.PublishOnce(ctx, 10)
	if err != nil {
		t.Fatalf("publish scan output: %v", err)
	}
	if result.Scanned != 1 || result.Published != 1 || result.Projected != 1 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if store.publishedScanOutputs["scan-1"] == "" {
		t.Fatalf("expected scan output to be marked published")
	}
	if len(store.projections) != 1 || store.projections[0].Card.Source.Kind != "local_scan_output" {
		t.Fatalf("expected local_scan_output projection, got %+v", store.projections)
	}
	if len(store.manifestAvailability) != 1 {
		t.Fatalf("expected one manifest availability projection, got %d", len(store.manifestAvailability))
	}
	availability := store.manifestAvailability[0].Attestation
	if availability.SourceNodeID != store.nodeID || availability.PoolID != "pool.local" || !availability.Available || availability.FetchPolicy != manifestavailability.FetchPolicyLocalOnly {
		t.Fatalf("unexpected manifest availability body: %+v", availability)
	}
}

func TestPublishOnceEmitsSeparateEventsForEachPool(t *testing.T) {
	ctx := context.Background()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	store := &fakeStore{
		candidates:       []releasecard.LocalRelease{testPublisherRelease()},
		eventsByBodyHash: make(map[string]string),
	}
	for _, poolID := range []string{"pool.one", "pool.two"} {
		service := New(node, store, poolID)
		service.now = func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) }
		result, err := service.PublishOnce(ctx, 10)
		if err != nil {
			t.Fatalf("publish %s: %v", poolID, err)
		}
		if result.Published != 1 {
			t.Fatalf("expected one event for %s, got %+v", poolID, result)
		}
	}
	if len(store.events) != 2 {
		t.Fatalf("expected one signed event per pool, got %d", len(store.events))
	}
	if store.events[0].PoolIDs[0] != "pool.one" || store.events[1].PoolIDs[0] != "pool.two" {
		t.Fatalf("unexpected event scopes: %v %v", store.events[0].PoolIDs, store.events[1].PoolIDs)
	}
	if store.projections[0].Card.ReleaseID != store.projections[1].Card.ReleaseID || store.projections[0].Card.ManifestID != store.projections[1].Card.ManifestID {
		t.Fatal("stable release and manifest IDs changed across pools")
	}
}

func TestPublishOnceBuildsAndCachesLocalManifestWhenEnabled(t *testing.T) {
	ctx := context.Background()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("identity: %v", err)
	}
	store := &fakeStore{
		candidates:       []releasecard.LocalRelease{testPublisherRelease()},
		eventsByBodyHash: make(map[string]string),
	}
	svc := New(node, store, "pool.local")
	svc.SetManifestBuilding(true)
	svc.now = func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) }
	if _, err := svc.PublishOnce(ctx, 10); err != nil {
		t.Fatalf("publish with local builder: %v", err)
	}
	if len(store.storedManifests) != 1 {
		t.Fatalf("expected one stored local manifest, got %d", len(store.storedManifests))
	}
	stored := store.storedManifests[0]
	if stored.Manifest.ManifestID == "" || stored.Manifest.ReleaseID != store.projections[0].Card.ReleaseID || len(stored.GeneratedNZB) == 0 {
		t.Fatalf("unexpected local manifest record: %+v", stored.Manifest)
	}
	if _, err := manifest.Validate(stored.Manifest); err != nil {
		t.Fatalf("stored local manifest should validate: %v", err)
	}
	if len(store.events) != 2 || store.events[1].EventType != manifest.Type || stored.SourceEventID != store.events[1].EventID {
		t.Fatalf("expected stored manifest to reference its signed event: events=%d source=%q", len(store.events), stored.SourceEventID)
	}
	if !store.requiredSignedManifest {
		t.Fatal("manifest builder did not request stale scan-output manifest repair candidates")
	}
}

func TestPublishHealthOnceSignsCompleteAndIncompleteAttestations(t *testing.T) {
	ctx := context.Background()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	complete := testPublisherRelease()
	incomplete := testPublisherRelease()
	incomplete.LocalReleaseID = "release-2"
	incomplete.Title = "Example.Release.2026.720p.WEB-DL"
	incomplete.Files[0].ArticleCount = 1
	incomplete.Files[0].TotalParts = 3
	incomplete.Files[0].Segments = incomplete.Files[0].Segments[:1]

	store := &fakeStore{
		candidates:       []releasecard.LocalRelease{complete, incomplete},
		eventsByBodyHash: make(map[string]string),
	}
	svc := New(node, store, "pool.local")
	svc.now = func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) }

	result, err := svc.PublishHealthOnce(ctx, 10)
	if err != nil {
		t.Fatalf("publish health: %v", err)
	}
	if result.Scanned != 2 || result.Published != 2 || result.Projected != 2 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(store.healthProjections) != 2 {
		t.Fatalf("expected two health projections, got %d", len(store.healthProjections))
	}
	if store.healthProjections[0].Attestation.Status != health.StatusComplete {
		t.Fatalf("expected complete status, got %q", store.healthProjections[0].Attestation.Status)
	}
	if store.healthProjections[1].Attestation.Status != health.StatusIncomplete {
		t.Fatalf("expected incomplete status, got %q", store.healthProjections[1].Attestation.Status)
	}
	for _, event := range store.events {
		if event.EventType != "HealthAttestation" {
			t.Fatalf("expected HealthAttestation event, got %q", event.EventType)
		}
	}
}

func TestPublishValidationOnceSignsAvailabilityWithoutIndexer(t *testing.T) {
	ctx := context.Background()
	node, err := identity.LoadOrCreate(t.TempDir())
	if err != nil {
		t.Fatalf("load identity: %v", err)
	}
	manifestBody := testPublisherManifest(t)
	store := &fakeStore{
		eventsByBodyHash: make(map[string]string),
		validationTasks: []pgindex.ValidationTask{{
			TaskID:     7,
			ManifestID: manifestBody.ManifestID,
			ReleaseID:  manifestBody.ReleaseID,
			PoolID:     "pool.local",
		}, {
			TaskID:     8,
			ManifestID: manifestBody.ManifestID,
			ReleaseID:  manifestBody.ReleaseID,
			PoolID:     "pool.other",
		}},
		manifests: map[string]*manifest.ResolutionManifest{
			manifestBody.ManifestID: &manifestBody,
		},
	}
	svc := New(node, store, "pool.local")
	svc.now = func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) }

	result, err := svc.PublishValidationOnce(ctx, 10, ValidationOptions{ChecksumEnabled: false, MaxTasksPerHour: 10})
	if err != nil {
		t.Fatalf("publish validation: %v", err)
	}
	if result.Claimed != 1 || result.CapacityPublished != 1 || result.Published != 1 || result.Projected != 1 || result.Failed != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
	if len(store.availabilityProjections) != 1 {
		t.Fatalf("expected one availability projection, got %d", len(store.availabilityProjections))
	}
	if store.availabilityProjections[0].Attestation.Status != validation.StatusUnverified {
		t.Fatalf("expected unverified availability, got %q", store.availabilityProjections[0].Attestation.Status)
	}
	if store.completedTasks[7] != "completed" {
		t.Fatalf("expected task completed, got %q", store.completedTasks[7])
	}
	if len(store.events) != 2 {
		t.Fatalf("expected capacity and availability events, got %d", len(store.events))
	}
}

type fakeStore struct {
	scanCandidates          []releasecard.LocalRelease
	candidates              []releasecard.LocalRelease
	events                  []*events.SignedEvent
	eventsByBodyHash        map[string]string
	projections             []releasecard.Projection
	healthProjections       []pgindex.HealthAttestationProjection
	validationTasks         []pgindex.ValidationTask
	manifests               map[string]*manifest.ResolutionManifest
	capacityProjections     []pgindex.ValidatorCapacityProjection
	availabilityProjections []pgindex.ArticleAvailabilityProjection
	checksumProjections     []pgindex.ChecksumAttestationProjection
	manifestAvailability    []pgindex.ManifestAvailabilityProjection
	storedManifests         []pgindex.ResolutionManifestRecord
	publishedScanOutputs    map[string]string
	completedTasks          map[int64]string
	nodeID                  string
	publicKey               ed25519.PublicKey
	requiredSignedManifest  bool
}

func (s *fakeStore) ListGoNZBNetScanOutputCandidates(_ context.Context, _ string, requireSignedManifest bool, _ int) ([]releasecard.LocalRelease, error) {
	s.requiredSignedManifest = requireSignedManifest
	return s.scanCandidates, nil
}

func (s *fakeStore) ListGoNZBNetLocalReleaseCandidates(context.Context, int, pgindex.ReleaseReadyPolicy) ([]releasecard.LocalRelease, error) {
	return s.candidates, nil
}

func (s *fakeStore) MarkGoNZBNetScanOutputPublished(_ context.Context, scanID, eventID, _ string) error {
	if s.publishedScanOutputs == nil {
		s.publishedScanOutputs = map[string]string{}
	}
	s.publishedScanOutputs[scanID] = eventID
	return nil
}

func (s *fakeStore) UpsertFederationNodeIdentity(_ context.Context, nodeID string, publicKey ed25519.PublicKey) error {
	s.nodeID = nodeID
	s.publicKey = publicKey
	return nil
}

func (s *fakeStore) NextFederationEventSequence(context.Context, string) (int64, *string, error) {
	if len(s.events) == 0 {
		return 1, nil, nil
	}
	previous := s.events[len(s.events)-1].EventID
	return int64(len(s.events) + 1), &previous, nil
}

func (s *fakeStore) FindFederationEventByBodyHash(_ context.Context, _, _, bodyHash, poolID string) (string, error) {
	return s.eventsByBodyHash[poolID+"\x00"+bodyHash], nil
}

func (s *fakeStore) AppendVerifiedFederationEvent(_ context.Context, event *events.SignedEvent, validation *events.ValidationResult) error {
	s.events = append(s.events, event)
	poolID := ""
	if len(event.PoolIDs) == 1 {
		poolID = event.PoolIDs[0]
	}
	s.eventsByBodyHash[poolID+"\x00"+event.BodyHash] = event.EventID
	return nil
}

func (s *fakeStore) UpsertFederatedReleaseCardProjection(_ context.Context, projection releasecard.Projection) error {
	s.projections = append(s.projections, projection)
	return nil
}

func (s *fakeStore) ProjectManifestAvailability(_ context.Context, projection pgindex.ManifestAvailabilityProjection) error {
	s.manifestAvailability = append(s.manifestAvailability, projection)
	return nil
}

func (s *fakeStore) StoreResolutionManifest(_ context.Context, record pgindex.ResolutionManifestRecord) error {
	s.storedManifests = append(s.storedManifests, record)
	return nil
}

func (s *fakeStore) ProjectHealthAttestation(_ context.Context, projection pgindex.HealthAttestationProjection) error {
	s.healthProjections = append(s.healthProjections, projection)
	return nil
}

func (s *fakeStore) ClaimValidationTasks(_ context.Context, _, poolID string, _ int) ([]pgindex.ValidationTask, error) {
	items := make([]pgindex.ValidationTask, 0, len(s.validationTasks))
	for _, task := range s.validationTasks {
		if task.PoolID == "" || task.PoolID == poolID {
			items = append(items, task)
		}
	}
	return items, nil
}

func (s *fakeStore) GetResolutionManifest(_ context.Context, manifestID string) (*manifest.ResolutionManifest, error) {
	if s.manifests == nil {
		return nil, nil
	}
	return s.manifests[manifestID], nil
}

func (s *fakeStore) ProjectValidatorCapacity(_ context.Context, projection pgindex.ValidatorCapacityProjection) error {
	s.capacityProjections = append(s.capacityProjections, projection)
	return nil
}

func (s *fakeStore) ProjectArticleAvailabilityAttestation(_ context.Context, projection pgindex.ArticleAvailabilityProjection) error {
	s.availabilityProjections = append(s.availabilityProjections, projection)
	return nil
}

func (s *fakeStore) ProjectChecksumAttestation(_ context.Context, projection pgindex.ChecksumAttestationProjection) error {
	s.checksumProjections = append(s.checksumProjections, projection)
	return nil
}

func (s *fakeStore) CompleteValidationTask(_ context.Context, taskID int64, status, _ string) error {
	if s.completedTasks == nil {
		s.completedTasks = map[int64]string{}
	}
	s.completedTasks[taskID] = status
	return nil
}

func testPublisherRelease() releasecard.LocalRelease {
	posted := time.Date(2026, 7, 7, 10, 55, 0, 0, time.UTC)
	return releasecard.LocalRelease{
		LocalReleaseID: "release-1",
		Title:          "Example.Release.2026.1080p.WEB-DL",
		Category:       "movies",
		CategoryID:     2040,
		SizeBytes:      1500,
		PostedAt:       &posted,
		FileCount:      1,
		Groups:         []string{"alt.binaries.example"},
		PasswordState:  "not_passworded",
		Availability:   0.9,
		Files: []releasecard.LocalFile{
			{
				Name:         "example.mkv",
				Subject:      "Example.Release.2026.1080p.WEB-DL example.mkv yEnc",
				PostedAt:     &posted,
				SizeBytes:    1500,
				FileIndex:    1,
				ArticleCount: 1,
				TotalParts:   1,
				Segments: []releasecard.LocalSegment{
					{Number: 1, Bytes: 1500, MessageID: "<seg1@example.invalid>"},
				},
			},
		},
	}
}

func testPublisherManifest(t *testing.T) manifest.ResolutionManifest {
	t.Helper()
	core := manifest.ManifestCore{
		Groups:   []string{"alt.binaries.example"},
		Poster:   "poster@example.invalid",
		PostedAt: "2026-07-09T12:00:00Z",
		Files: []manifest.ManifestFile{{
			Name:      "example.mkv",
			Subject:   "Example example.mkv yEnc",
			Date:      "2026-07-09T12:01:00Z",
			SizeBytes: 1000,
			Segments: []manifest.ManifestSegment{{
				Number:    1,
				Bytes:     1000,
				MessageID: "<seg1@example.invalid>",
			}},
		}},
		NZB: manifest.NZBInfo{Generator: "GoNZBNet", XMLCharset: "utf-8"},
	}
	manifestID, _, err := manifest.ComputeID(core)
	if err != nil {
		t.Fatalf("compute manifest id: %v", err)
	}
	return manifest.ResolutionManifest{
		SchemaVersion: "1.0",
		Type:          manifest.Type,
		ManifestID:    manifestID,
		ReleaseID:     "rel_manifest",
		ManifestCore:  core,
	}
}

func TestArticleAvailabilityFromNNTP(t *testing.T) {
	item := testPublisherManifest(t)
	service := &Service{now: func() time.Time { return time.Date(2026, 7, 9, 12, 0, 0, 0, time.UTC) }}
	service.articleChecker = func(_ context.Context, messageID string, _ []string) error {
		if messageID == "<seg1@example.invalid>" {
			return nil
		}
		return fmt.Errorf("missing")
	}
	result := service.articleAvailabilityFromNNTP(context.Background(), item)
	if result.Status != validation.StatusAvailable || result.ArticlesTotal != 1 || result.ArticlesAvailable != 1 || result.MissingArticles != 0 {
		t.Fatalf("unexpected NNTP availability result: %+v", result)
	}
	if result.Method != "nntp_fetch_body_prefix" {
		t.Fatalf("unexpected validation method: %s", result.Method)
	}
}
