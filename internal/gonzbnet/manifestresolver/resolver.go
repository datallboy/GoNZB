package manifestresolver

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/activity"
	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
	"github.com/datallboy/gonzb/internal/gonzbnet/eventbody"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifest"
	gonzbnetmetrics "github.com/datallboy/gonzb/internal/gonzbnet/metrics"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
	"github.com/datallboy/gonzb/internal/gonzbnet/transportpolicy"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type Identity interface {
	requestauth.Signer
}

type Store interface {
	GetCachedFederatedNZBByReleaseID(ctx context.Context, releaseID string) ([]byte, bool, error)
	FindFederatedManifestSource(ctx context.Context, releaseID string) (*pgindex.FederatedManifestSource, error)
	FindAcceptedResolutionManifestEvent(ctx context.Context, source pgindex.FederatedManifestSource) (*events.SignedEvent, error)
	AuthorizeFederatedManifestSource(ctx context.Context, source pgindex.FederatedManifestSource, authorNodeID string) error
	AppendVerifiedFederationEvent(ctx context.Context, event *events.SignedEvent, validation *events.ValidationResult) error
	StoreResolutionManifest(ctx context.Context, record pgindex.ResolutionManifestRecord) error
	RecordFederatedManifestSourceSuccess(ctx context.Context, source pgindex.FederatedManifestSource) error
	RecordFederatedManifestSourceFailure(ctx context.Context, source pgindex.FederatedManifestSource) error
}

type Resolver struct {
	identity              Identity
	store                 Store
	client                *http.Client
	allowInsecurePeerHTTP bool
	traversalEnabled      bool
	eventTimeTolerance    time.Duration
	maxEventAge           time.Duration
	maxManifestBytes      int64
	fetchTimeout          time.Duration
}

type Options struct {
	AllowInsecurePeerHTTP bool
	EventTimeTolerance    time.Duration
	MaxEventAge           time.Duration
	MaxManifestBytes      int64
	FetchTimeout          time.Duration
	TraversalEnabled      bool
	HTTPClient            *http.Client
}

func New(identity Identity, store Store) *Resolver {
	return NewWithOptions(identity, store, Options{})
}

func NewWithOptions(identity Identity, store Store, opts Options) *Resolver {
	eventTimeTolerance := opts.EventTimeTolerance
	if eventTimeTolerance <= 0 {
		eventTimeTolerance = 2 * time.Minute
	}
	maxManifestBytes := opts.MaxManifestBytes
	if maxManifestBytes <= 0 {
		maxManifestBytes = 10485760
	}
	fetchTimeout := opts.FetchTimeout
	if fetchTimeout <= 0 {
		fetchTimeout = 20 * time.Second
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: fetchTimeout}
	}
	return &Resolver{
		identity:              identity,
		store:                 store,
		allowInsecurePeerHTTP: opts.AllowInsecurePeerHTTP,
		traversalEnabled:      opts.TraversalEnabled,
		eventTimeTolerance:    eventTimeTolerance,
		maxEventAge:           opts.MaxEventAge,
		maxManifestBytes:      maxManifestBytes,
		fetchTimeout:          fetchTimeout,
		client:                client,
	}
}

func (r *Resolver) ResolveNZB(ctx context.Context, releaseID string) (reader io.ReadCloser, err error) {
	finishActivity := activity.Default.Begin(activity.ComponentManifestResolver, "")
	defer func() {
		items := int64(0)
		if reader != nil {
			items = 1
		}
		finishActivity(activity.Result{ItemsIn: 1, ItemsOut: items, Err: err})
	}()
	if r == nil || r.identity == nil || r.store == nil {
		return nil, fmt.Errorf("manifest resolver dependencies are required")
	}
	releaseID = strings.TrimSpace(releaseID)
	if releaseID == "" {
		return nil, fmt.Errorf("release_id is required")
	}
	if payload, ok, err := r.store.GetCachedFederatedNZBByReleaseID(ctx, releaseID); err != nil {
		return nil, err
	} else if ok {
		activity.Default.Record(activity.ComponentManifestCache, "", activity.Result{ItemsIn: 1, ItemsOut: 1, BytesOut: int64(len(payload))})
		return io.NopCloser(bytes.NewReader(payload)), nil
	}
	started := time.Now()
	failed := true
	gonzbnetmetrics.Default.Add(gonzbnetmetrics.ManifestRequestsTotal, 1)
	defer func() {
		gonzbnetmetrics.Default.ObserveManifestResolution(time.Since(started))
		if failed {
			gonzbnetmetrics.Default.Add(gonzbnetmetrics.ManifestRequestFailuresTotal, 1)
		}
	}()
	source, err := r.store.FindFederatedManifestSource(ctx, releaseID)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, fmt.Errorf("federated manifest source not found")
	}
	localEvent, err := r.store.FindAcceptedResolutionManifestEvent(ctx, *source)
	if err != nil {
		return nil, err
	}
	if localEvent != nil {
		// The event already passed federation intake checks. Re-verify its
		// signature and current time window, but do not apply the inbound event
		// age limit again: accepted events remain durable protocol state.
		validation, err := events.VerifyAt(localEvent, time.Now(), r.eventTimeTolerance)
		if err != nil {
			return nil, err
		}
		if validation == nil || !validation.OK {
			return nil, fmt.Errorf("cached manifest event verification failed: %s", validationReason(validation))
		}
		nzbPayload, err := r.materializeManifest(ctx, *source, localEvent, validation, false)
		if err != nil {
			return nil, err
		}
		failed = false
		return io.NopCloser(bytes.NewReader(nzbPayload)), nil
	}
	if err := r.store.AuthorizeFederatedManifestSource(ctx, *source, ""); err != nil {
		return nil, err
	}
	failSource := func(err error) (io.ReadCloser, error) {
		_ = r.store.RecordFederatedManifestSourceFailure(ctx, *source)
		return nil, err
	}
	event, err := r.fetchManifest(ctx, *source)
	if err != nil {
		return failSource(err)
	}
	validation, err := events.VerifyWithin(event, time.Now(), r.eventTimeTolerance, r.maxEventAge)
	if err != nil {
		return failSource(err)
	}
	if validation == nil || !validation.OK {
		return failSource(fmt.Errorf("manifest event verification failed: %s", validationReason(validation)))
	}
	nzbPayload, err := r.materializeManifest(ctx, *source, event, validation, true)
	if err != nil {
		return failSource(err)
	}
	_ = r.store.RecordFederatedManifestSourceSuccess(ctx, *source)
	failed = false
	return io.NopCloser(bytes.NewReader(nzbPayload)), nil
}

func (r *Resolver) materializeManifest(
	ctx context.Context,
	source pgindex.FederatedManifestSource,
	event *events.SignedEvent,
	validation *events.ValidationResult,
	appendEvent bool,
) ([]byte, error) {
	if event.EventType != manifest.Type {
		return nil, fmt.Errorf("unexpected manifest event type %q", event.EventType)
	}
	if err := eventbody.Validate(event, time.Now().UTC(), r.eventTimeTolerance); err != nil {
		return nil, fmt.Errorf("invalid manifest event body: %w", err)
	}
	if event.BodySchema != manifest.BodySchema {
		return nil, fmt.Errorf("unexpected manifest body schema %q", event.BodySchema)
	}
	if len(event.PoolIDs) != 1 || strings.TrimSpace(event.PoolIDs[0]) != strings.TrimSpace(source.PoolID) {
		return nil, fmt.Errorf("manifest event pool mismatch")
	}
	var body manifest.ResolutionManifest
	if err := json.Unmarshal(event.Body, &body); err != nil {
		return nil, err
	}
	canonicalCore, err := manifest.Validate(body)
	if err != nil {
		return nil, err
	}
	if body.ManifestID != source.ManifestID {
		return nil, fmt.Errorf("manifest_id mismatch")
	}
	if body.ReleaseID != source.ReleaseID {
		return nil, fmt.Errorf("release_id mismatch")
	}
	if err := r.store.AuthorizeFederatedManifestSource(ctx, source, event.AuthorNodeID); err != nil {
		return nil, err
	}
	nzbPayload, err := manifest.GenerateNZB(body)
	if err != nil {
		return nil, err
	}
	if appendEvent {
		if err := r.store.AppendVerifiedFederationEvent(ctx, event, validation); err != nil {
			return nil, err
		}
	}
	if err := r.store.StoreResolutionManifest(ctx, pgindex.ResolutionManifestRecord{
		Manifest:              body,
		SourceNodeID:          event.AuthorNodeID,
		FetchedFromNodeID:     source.SourceNodeID,
		SourceEventID:         event.EventID,
		PoolID:                source.PoolID,
		CanonicalManifestJSON: canonicalCore,
		GeneratedNZB:          nzbPayload,
	}); err != nil {
		return nil, err
	}
	activity.Default.Record(activity.ComponentManifestCache, source.PoolID, activity.Result{ItemsIn: 1, ItemsOut: 1, BytesIn: int64(len(nzbPayload))})
	return nzbPayload, nil
}

func (r *Resolver) fetchManifest(ctx context.Context, source pgindex.FederatedManifestSource) (*events.SignedEvent, error) {
	nodeID, err := r.identity.NodeID(ctx)
	if err != nil {
		return nil, err
	}
	requestID, err := randomRequestID()
	if err != nil {
		return nil, err
	}
	reqBody, err := json.Marshal(manifest.Request{
		SchemaVersion:    "1.0",
		Type:             "ManifestRequest",
		RequestID:        requestID,
		ManifestID:       source.ManifestID,
		ReleaseID:        source.ReleaseID,
		PoolID:           source.PoolID,
		RequestingNodeID: nodeID,
		Reason:           "user_get",
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	endpoint := strings.TrimRight(source.BaseURL, "/") + "/manifests/" + url.PathEscape(source.ManifestID) + "/request"
	if err := transportpolicy.ValidatePeerRequestURL(endpoint, r.allowInsecurePeerHTTP, r.traversalEnabled); err != nil {
		return nil, err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return nil, err
	}
	authorization, err := requestauth.Sign(ctx, r.identity, http.MethodPost, parsed.Path, parsed.RawQuery, reqBody, time.Now())
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/gonzbnet+json")
	httpReq.Header.Set("Authorization", authorization)
	resp, err := r.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	payload, err := readLimited(resp.Body, r.maxManifestBytes)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("manifest request status=%d body=%s", resp.StatusCode, strings.TrimSpace(string(payload)))
	}
	var out manifest.Response
	if err := canonical.ValidateJSON(payload); err != nil {
		return nil, fmt.Errorf("invalid manifest response json: %w", err)
	}
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil, err
	}
	if out.SchemaVersion != "1.0" || out.Type != "ManifestResponse" {
		return nil, fmt.Errorf("invalid manifest response schema or type")
	}
	if strings.TrimSpace(out.RequestID) != requestID {
		return nil, fmt.Errorf("manifest response request_id mismatch")
	}
	if out.Status != "ok" || out.ManifestEvent == nil {
		return nil, fmt.Errorf("manifest response error: %s", firstNonBlank(out.Message, out.Code, "missing manifest_event"))
	}
	return out.ManifestEvent, nil
}

func readLimited(reader io.Reader, maxBytes int64) ([]byte, error) {
	if maxBytes <= 0 {
		maxBytes = 10485760
	}
	payload, err := io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(payload)) > maxBytes {
		return nil, fmt.Errorf("manifest response exceeds max_manifest_bytes")
	}
	return payload, nil
}

func randomRequestID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "req_" + canonical.Base64URL(raw[:]), nil
}

func validationReason(result *events.ValidationResult) string {
	if result == nil {
		return "missing validation result"
	}
	return result.Reason
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
