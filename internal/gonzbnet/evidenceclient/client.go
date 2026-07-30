package evidenceclient

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/evidence"
	gonzbnetmetrics "github.com/datallboy/gonzb/internal/gonzbnet/metrics"
	"github.com/datallboy/gonzb/internal/gonzbnet/requestauth"
	"github.com/datallboy/gonzb/internal/gonzbnet/transportpolicy"
	"github.com/datallboy/gonzb/internal/store/pgindex"
)

type Identity interface {
	requestauth.Signer
}

type Store interface {
	FindAcceptedYEncEvidence(context.Context, []string, bool, int) ([]pgindex.YEncEvidenceRecord, error)
	UpsertYEncHeaderEvidence(context.Context, []pgindex.YEncEvidenceRecord) (int, int, error)
	ListBinaryEvidencePeers(context.Context, string, int) ([]pgindex.BinaryEvidencePeer, error)
	GetFederationNodePublicKey(context.Context, string) (ed25519.PublicKey, error)
	RecordBinaryEvidenceDiagnostic(context.Context, pgindex.BinaryEvidenceDiagnostic) error
	ImportPeerSegments(context.Context, int64, string, string, string, []evidence.Segment) (int, int, error)
}

type SegmentLookupResult struct {
	Imported      int
	Conflicts     int
	PeerRequests  int
	ResponseBytes int64
}

func (c *Client) LookupSegments(ctx context.Context, binaryID int64, scheme, matchID string, missing []int, anchors []string) (SegmentLookupResult, error) {
	var result SegmentLookupResult
	missing = normalizeParts(missing, evidence.MaxSegmentItems)
	if c == nil || !c.opts.Enabled || c.identity == nil || c.store == nil || len(missing) == 0 {
		return result, nil
	}
	nodeID, err := c.identity.NodeID(ctx)
	if err != nil {
		return result, err
	}
	peers, err := c.store.ListBinaryEvidencePeers(ctx, nodeID, c.opts.PeerFanout)
	if err != nil {
		return result, err
	}
	for _, peer := range peers {
		if len(missing) == 0 || c.peerCoolingDown(peer.NodeID) {
			continue
		}
		started := time.Now()
		result.PeerRequests++
		bundle, size, err := c.querySegmentPeer(ctx, nodeID, peer, scheme, matchID, missing, anchors)
		diagnostic := pgindex.BinaryEvidenceDiagnostic{
			PoolID: peer.PoolID, PeerNodeID: peer.NodeID, Direction: "consume",
			EvidenceKind: "segments", RequestCount: 1, ResponseBytes: size,
			LatencyMS: time.Since(started).Milliseconds(),
		}
		if err != nil {
			diagnostic.ErrorText = err.Error()
			_ = c.store.RecordBinaryEvidenceDiagnostic(ctx, diagnostic)
			c.tripPeer(peer.NodeID)
			gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidencePeerFailuresTotal, 1)
			recordTimeout(err)
			continue
		}
		result.ResponseBytes += size
		gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidenceResponseBytesTotal, uint64(maxInt64(size, 0)))
		imported, conflicts, err := c.store.ImportPeerSegments(ctx, binaryID, peer.PoolID, peer.NodeID, bundle.BundleID, bundle.Segments)
		if err != nil {
			diagnostic.ErrorText = err.Error()
			_ = c.store.RecordBinaryEvidenceDiagnostic(ctx, diagnostic)
			return result, err
		}
		result.Imported += imported
		result.Conflicts += conflicts
		diagnostic.ItemCount = len(bundle.Segments)
		diagnostic.HitCount = imported
		diagnostic.Conflicts = conflicts
		_ = c.store.RecordBinaryEvidenceDiagnostic(ctx, diagnostic)
		gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidenceConflictsTotal, uint64(maxInt(conflicts, 0)))
		if imported > 0 {
			gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidenceSegmentsImportedTotal, uint64(imported))
		}
		present := make(map[int]struct{}, len(bundle.Segments))
		for _, segment := range bundle.Segments {
			present[segment.PartNumber] = struct{}{}
		}
		next := missing[:0]
		for _, part := range missing {
			if _, ok := present[part]; !ok {
				next = append(next, part)
			}
		}
		missing = next
	}
	return result, nil
}

func (c *Client) querySegmentPeer(ctx context.Context, nodeID string, peer pgindex.BinaryEvidencePeer, scheme, matchID string, missing []int, anchors []string) (evidence.Bundle, int64, error) {
	var out evidence.Bundle
	requestID, err := randomID()
	if err != nil {
		return out, 0, err
	}
	query := evidence.SegmentQuery{
		SchemaVersion: evidence.SchemaVersion, Type: evidence.SegmentQueryType,
		RequestID: requestID, PoolID: peer.PoolID, RequestingNodeID: nodeID,
		Scheme: scheme, MatchID: matchID, Missing: compactRanges(missing),
		Anchors: anchors, CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(query)
	if err != nil {
		return out, 0, err
	}
	endpoint := strings.TrimRight(peer.BaseURL, "/") + "/evidence/segments/query"
	if err := transportpolicy.ValidateHTTPURL(endpoint, c.opts.AllowInsecurePeerHTTP); err != nil {
		return out, 0, err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return out, 0, err
	}
	auth, err := requestauth.Sign(ctx, c.identity, http.MethodPost, parsed.Path, parsed.RawQuery, payload, time.Now())
	if err != nil {
		return out, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return out, 0, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/gonzbnet+json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return out, 0, err
	}
	defer resp.Body.Close()
	body, err := readLimited(resp.Body, c.opts.MaxResponseBytes)
	if err != nil {
		return out, 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, int64(len(body)), fmt.Errorf("peer segment evidence status=%d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, int64(len(body)), err
	}
	publicKey, err := c.store.GetFederationNodePublicKey(ctx, peer.NodeID)
	if err != nil {
		return out, int64(len(body)), err
	}
	if out.SourceNodeID != peer.NodeID {
		return out, int64(len(body)), fmt.Errorf("evidence source node mismatch")
	}
	if err := evidence.VerifyBundle(out, publicKey, peer.PoolID, requestID, nodeID, time.Now().UTC()); err != nil {
		return out, int64(len(body)), err
	}
	requested := make(map[int]struct{}, len(missing))
	for _, part := range missing {
		requested[part] = struct{}{}
	}
	for _, segment := range out.Segments {
		if _, ok := requested[segment.PartNumber]; !ok {
			return out, int64(len(body)), fmt.Errorf("peer returned unrequested segment")
		}
	}
	return out, int64(len(body)), nil
}

func compactRanges(parts []int) []evidence.PartRange {
	if len(parts) == 0 {
		return nil
	}
	out := make([]evidence.PartRange, 0)
	start, end := parts[0], parts[0]
	for _, part := range parts[1:] {
		if part == end+1 {
			end = part
			continue
		}
		out = append(out, evidence.PartRange{Start: start, End: end})
		start, end = part, part
	}
	out = append(out, evidence.PartRange{Start: start, End: end})
	return out
}

func normalizeParts(parts []int, limit int) []int {
	seen := make(map[int]struct{}, len(parts))
	out := make([]int, 0, len(parts))
	for _, part := range parts {
		if part <= 0 {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	sort.Ints(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

type Options struct {
	Enabled                bool
	AllowInsecurePeerHTTP  bool
	PeerTimeout            time.Duration
	PeerFanout             int
	BatchSize              int
	MaxResponseBytes       int64
	CircuitBreakerCooldown time.Duration
}

type LookupResult struct {
	Headers       map[string]pgindex.YEncEvidenceRecord
	CacheHits     int
	PeerHits      int
	PeerRequests  int
	ResponseBytes int64
	Conflicts     int
	Quarantines   int
}

type Client struct {
	identity   Identity
	store      Store
	opts       Options
	httpClient *http.Client
	mu         sync.Mutex
	cooldown   map[string]time.Time
}

func New(identity Identity, store Store, opts Options) *Client {
	if opts.PeerTimeout <= 0 {
		opts.PeerTimeout = 3 * time.Second
	}
	if opts.PeerFanout <= 0 {
		opts.PeerFanout = 3
	}
	if opts.BatchSize <= 0 || opts.BatchSize > evidence.MaxYEncQueryItems {
		opts.BatchSize = evidence.MaxYEncQueryItems
	}
	if opts.MaxResponseBytes <= 0 {
		opts.MaxResponseBytes = 10 * 1024 * 1024
	}
	if opts.CircuitBreakerCooldown <= 0 {
		opts.CircuitBreakerCooldown = 5 * time.Minute
	}
	return &Client{
		identity: identity, store: store, opts: opts,
		httpClient: &http.Client{Timeout: opts.PeerTimeout},
		cooldown:   make(map[string]time.Time),
	}
}

func (c *Client) LookupYEnc(ctx context.Context, messageIDs []string) (LookupResult, error) {
	result := LookupResult{Headers: make(map[string]pgindex.YEncEvidenceRecord)}
	if c == nil || c.store == nil {
		return result, nil
	}
	messageIDs = normalize(messageIDs)
	if len(messageIDs) == 0 {
		return result, nil
	}
	for offset := 0; offset < len(messageIDs); offset += c.opts.BatchSize {
		end := minInt(offset+c.opts.BatchSize, len(messageIDs))
		local, err := c.store.FindAcceptedYEncEvidence(ctx, messageIDs[offset:end], false, c.opts.BatchSize)
		if err != nil {
			return result, err
		}
		for _, item := range local {
			result.Headers[item.MessageID] = item
			result.CacheHits++
		}
	}
	if !c.opts.Enabled || c.identity == nil || len(result.Headers) == len(messageIDs) {
		return result, nil
	}
	nodeID, err := c.identity.NodeID(ctx)
	if err != nil {
		return result, err
	}
	peers, err := c.store.ListBinaryEvidencePeers(ctx, nodeID, c.opts.PeerFanout)
	if err != nil {
		return result, err
	}
	for offset := 0; offset < len(messageIDs); offset += c.opts.BatchSize {
		end := minInt(offset+c.opts.BatchSize, len(messageIDs))
		batch := messageIDs[offset:end]
		for _, peer := range peers {
			missing := unresolved(batch, result.Headers)
			if len(missing) == 0 {
				break
			}
			if c.peerCoolingDown(peer.NodeID) {
				continue
			}
			started := time.Now()
			result.PeerRequests++
			bundle, responseBytes, err := c.queryYEncPeer(ctx, nodeID, peer, missing)
			diagnostic := pgindex.BinaryEvidenceDiagnostic{
				PoolID: peer.PoolID, PeerNodeID: peer.NodeID, Direction: "consume",
				EvidenceKind: "yenc", RequestCount: 1, ResponseBytes: responseBytes,
				LatencyMS: time.Since(started).Milliseconds(),
			}
			if err != nil {
				diagnostic.ErrorText = err.Error()
				_ = c.store.RecordBinaryEvidenceDiagnostic(ctx, diagnostic)
				c.tripPeer(peer.NodeID)
				gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidencePeerFailuresTotal, 1)
				recordTimeout(err)
				continue
			}
			result.ResponseBytes += responseBytes
			gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidenceResponseBytesTotal, uint64(maxInt64(responseBytes, 0)))
			records := make([]pgindex.YEncEvidenceRecord, 0, len(bundle.YEncHeaders))
			for _, item := range bundle.YEncHeaders {
				if _, requested := result.Headers[item.MessageID]; requested {
					continue
				}
				sourcePostedAt, err := time.Parse(time.RFC3339, item.SourcePostedAt)
				if err != nil {
					result.Conflicts++
					continue
				}
				records = append(records, pgindex.YEncEvidenceRecord{
					SourcePostedAt: sourcePostedAt.UTC(), MessageID: item.MessageID,
					FileName: item.FileName, PartNumber: item.PartNumber,
					TotalParts: item.TotalParts, FileSize: item.FileSize,
					PartBegin: item.PartBegin, PartEnd: item.PartEnd,
					Provenance: "peer", SourcePoolID: peer.PoolID,
					SourceNodeID: peer.NodeID, SourceBundleID: bundle.BundleID,
					AcceptanceState: "accepted",
				})
			}
			accepted, quarantined, err := c.store.UpsertYEncHeaderEvidence(ctx, records)
			if err != nil {
				diagnostic.ErrorText = err.Error()
				_ = c.store.RecordBinaryEvidenceDiagnostic(ctx, diagnostic)
				continue
			}
			result.Quarantines += quarantined
			gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidenceQuarantinesTotal, uint64(maxInt(quarantined, 0)))
			diagnostic.ItemCount = len(records)
			diagnostic.HitCount = accepted
			diagnostic.BodyRequestsAvoided = accepted
			diagnostic.Quarantines = quarantined
			_ = c.store.RecordBinaryEvidenceDiagnostic(ctx, diagnostic)
			acceptedRecords, readErr := c.store.FindAcceptedYEncEvidence(ctx, missing, false, c.opts.BatchSize)
			if readErr != nil {
				continue
			}
			for _, record := range acceptedRecords {
				if _, already := result.Headers[record.MessageID]; already {
					continue
				}
				result.Headers[record.MessageID] = record
				result.PeerHits++
			}
		}
	}
	if result.PeerRequests > 0 {
		gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidencePeerRequestsTotal, uint64(result.PeerRequests))
	}
	if result.PeerHits > 0 {
		gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidenceYEncHitsTotal, uint64(result.PeerHits))
	}
	return result, nil
}

func (c *Client) queryYEncPeer(ctx context.Context, nodeID string, peer pgindex.BinaryEvidencePeer, messageIDs []string) (evidence.Bundle, int64, error) {
	var out evidence.Bundle
	requestID, err := randomID()
	if err != nil {
		return out, 0, err
	}
	query := evidence.YEncQuery{
		SchemaVersion: evidence.SchemaVersion, Type: evidence.YEncQueryType,
		RequestID: requestID, PoolID: peer.PoolID, RequestingNodeID: nodeID,
		MessageIDs: messageIDs, CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}
	payload, err := json.Marshal(query)
	if err != nil {
		return out, 0, err
	}
	endpoint := strings.TrimRight(peer.BaseURL, "/") + "/evidence/yenc/query"
	if err := transportpolicy.ValidateHTTPURL(endpoint, c.opts.AllowInsecurePeerHTTP); err != nil {
		return out, 0, err
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return out, 0, err
	}
	auth, err := requestauth.Sign(ctx, c.identity, http.MethodPost, parsed.Path, parsed.RawQuery, payload, time.Now())
	if err != nil {
		return out, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return out, 0, err
	}
	req.Header.Set("Authorization", auth)
	req.Header.Set("Content-Type", "application/gonzbnet+json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return out, 0, err
	}
	defer resp.Body.Close()
	body, err := readLimited(resp.Body, c.opts.MaxResponseBytes)
	if err != nil {
		return out, 0, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return out, int64(len(body)), fmt.Errorf("peer evidence status=%d", resp.StatusCode)
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, int64(len(body)), err
	}
	publicKey, err := c.store.GetFederationNodePublicKey(ctx, peer.NodeID)
	if err != nil {
		return out, int64(len(body)), err
	}
	if out.SourceNodeID != peer.NodeID {
		return out, int64(len(body)), fmt.Errorf("evidence source node mismatch")
	}
	if err := evidence.VerifyBundle(out, publicKey, peer.PoolID, requestID, nodeID, time.Now().UTC()); err != nil {
		return out, int64(len(body)), err
	}
	requested := make(map[string]struct{}, len(messageIDs))
	for _, value := range messageIDs {
		requested[value] = struct{}{}
	}
	for _, header := range out.YEncHeaders {
		if _, ok := requested[header.MessageID]; !ok {
			return out, int64(len(body)), fmt.Errorf("peer returned unrequested message id")
		}
	}
	return out, int64(len(body)), nil
}

func (c *Client) peerCoolingDown(nodeID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	until := c.cooldown[nodeID]
	if until.IsZero() || time.Now().After(until) {
		delete(c.cooldown, nodeID)
		return false
	}
	return true
}

func (c *Client) tripPeer(nodeID string) {
	c.mu.Lock()
	c.cooldown[nodeID] = time.Now().Add(c.opts.CircuitBreakerCooldown)
	c.mu.Unlock()
}

func unresolved(all []string, found map[string]pgindex.YEncEvidenceRecord) []string {
	out := make([]string, 0, len(all))
	for _, value := range all {
		if _, ok := found[value]; !ok {
			out = append(out, value)
		}
	}
	return out
}

func normalize(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func readLimited(reader io.Reader, limit int64) ([]byte, error) {
	body, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("peer evidence response exceeds limit")
	}
	return body, nil
}

func randomID() (string, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return "req_" + hex.EncodeToString(value[:]), nil
}

func recordTimeout(err error) {
	if errors.Is(err, context.DeadlineExceeded) {
		gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidenceTimeoutsTotal, 1)
		return
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		gonzbnetmetrics.Default.Add(gonzbnetmetrics.BinaryEvidenceTimeoutsTotal, 1)
	}
}

func maxInt(value, floor int) int {
	if value < floor {
		return floor
	}
	return value
}

func maxInt64(value, floor int64) int64 {
	if value < floor {
		return floor
	}
	return value
}

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
