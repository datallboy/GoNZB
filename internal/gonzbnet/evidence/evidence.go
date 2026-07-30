package evidence

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base32"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/canonical"
)

const (
	SchemaVersion     = "1.0"
	YEncQueryType     = "YEncEvidenceQuery"
	SegmentQueryType  = "BinarySegmentQuery"
	BundleType        = "BinaryEvidenceBundle"
	MaxYEncQueryItems = 1000
	MaxSegmentItems   = 5000
)

var base32NoPadding = base32.StdEncoding.WithPadding(base32.NoPadding)

type Signer interface {
	NodeID(context.Context) (string, error)
	Sign(context.Context, []byte) ([]byte, error)
}

type YEncQuery struct {
	SchemaVersion    string   `json:"schema_version"`
	Type             string   `json:"type"`
	RequestID        string   `json:"request_id"`
	PoolID           string   `json:"pool_id"`
	RequestingNodeID string   `json:"requesting_node_id"`
	MessageIDs       []string `json:"message_ids"`
	CreatedAt        string   `json:"created_at"`
}

type PartRange struct {
	Start int `json:"start"`
	End   int `json:"end"`
}

type SegmentQuery struct {
	SchemaVersion    string      `json:"schema_version"`
	Type             string      `json:"type"`
	RequestID        string      `json:"request_id"`
	PoolID           string      `json:"pool_id"`
	RequestingNodeID string      `json:"requesting_node_id"`
	Scheme           string      `json:"scheme"`
	MatchID          string      `json:"match_id"`
	Missing          []PartRange `json:"missing"`
	Anchors          []string    `json:"anchors,omitempty"`
	CreatedAt        string      `json:"created_at"`
}

type YEncHeader struct {
	MessageID      string `json:"message_id"`
	SourcePostedAt string `json:"source_posted_at"`
	FileName       string `json:"file_name"`
	PartNumber     int    `json:"part_number"`
	TotalParts     int    `json:"total_parts"`
	FileSize       int64  `json:"file_size"`
	PartBegin      int64  `json:"part_begin,omitempty"`
	PartEnd        int64  `json:"part_end,omitempty"`
}

type Segment struct {
	PartNumber     int      `json:"part_number"`
	TotalParts     int      `json:"total_parts"`
	MessageID      string   `json:"message_id"`
	Bytes          int64    `json:"bytes"`
	PostedAt       string   `json:"posted_at,omitempty"`
	SourcePostedAt string   `json:"source_posted_at"`
	Groups         []string `json:"groups,omitempty"`
	FileName       string   `json:"file_name,omitempty"`
	FileSize       int64    `json:"file_size,omitempty"`
}

type Bundle struct {
	SchemaVersion    string       `json:"schema_version"`
	Type             string       `json:"type"`
	BundleID         string       `json:"bundle_id"`
	PoolID           string       `json:"pool_id"`
	RequestID        string       `json:"request_id"`
	RequestingNodeID string       `json:"requesting_node_id"`
	SourceNodeID     string       `json:"source_node_id"`
	CreatedAt        string       `json:"created_at"`
	ExpiresAt        string       `json:"expires_at"`
	YEncHeaders      []YEncHeader `json:"yenc_headers,omitempty"`
	Segments         []Segment    `json:"segments,omitempty"`
	Signature        string       `json:"signature"`
}

func ValidateYEncQuery(in YEncQuery, now time.Time) error {
	if in.SchemaVersion != SchemaVersion || in.Type != YEncQueryType {
		return fmt.Errorf("unsupported yenc evidence query")
	}
	if strings.TrimSpace(in.RequestID) == "" || strings.TrimSpace(in.PoolID) == "" || strings.TrimSpace(in.RequestingNodeID) == "" {
		return fmt.Errorf("request_id, pool_id, and requesting_node_id are required")
	}
	if len(in.MessageIDs) == 0 || len(in.MessageIDs) > MaxYEncQueryItems {
		return fmt.Errorf("message_ids must contain 1-%d items", MaxYEncQueryItems)
	}
	seen := make(map[string]struct{}, len(in.MessageIDs))
	for _, value := range in.MessageIDs {
		value = strings.TrimSpace(value)
		if !validMessageID(value) {
			return fmt.Errorf("invalid message_id")
		}
		if _, ok := seen[value]; ok {
			return fmt.Errorf("duplicate message_id")
		}
		seen[value] = struct{}{}
	}
	return validateCreatedAt(in.CreatedAt, now)
}

func ValidateSegmentQuery(in SegmentQuery, now time.Time) error {
	if in.SchemaVersion != SchemaVersion || in.Type != SegmentQueryType {
		return fmt.Errorf("unsupported segment evidence query")
	}
	if strings.TrimSpace(in.RequestID) == "" || strings.TrimSpace(in.PoolID) == "" ||
		strings.TrimSpace(in.RequestingNodeID) == "" || strings.TrimSpace(in.MatchID) == "" {
		return fmt.Errorf("request_id, pool_id, requesting_node_id, and match_id are required")
	}
	if in.Scheme != "subject_multipart_v1" && in.Scheme != "yenc_v1" && in.Scheme != "content_v1" {
		return fmt.Errorf("unsupported match scheme")
	}
	if len(in.Missing) == 0 || len(in.Missing) > MaxSegmentItems {
		return fmt.Errorf("missing ranges are required")
	}
	total := 0
	for _, item := range in.Missing {
		if item.Start <= 0 || item.End < item.Start {
			return fmt.Errorf("invalid missing part range")
		}
		total += item.End - item.Start + 1
		if total > MaxSegmentItems {
			return fmt.Errorf("missing parts exceed %d", MaxSegmentItems)
		}
	}
	if len(in.Anchors) == 0 || len(in.Anchors) > 8 {
		return fmt.Errorf("anchors must contain 1-8 message_ids")
	}
	for _, value := range in.Anchors {
		if !validMessageID(strings.TrimSpace(value)) {
			return fmt.Errorf("invalid anchor message_id")
		}
	}
	return validateCreatedAt(in.CreatedAt, now)
}

func SignBundle(ctx context.Context, signer Signer, bundle *Bundle) error {
	if signer == nil || bundle == nil {
		return fmt.Errorf("signer and bundle are required")
	}
	nodeID, err := signer.NodeID(ctx)
	if err != nil {
		return err
	}
	bundle.SchemaVersion = SchemaVersion
	bundle.Type = BundleType
	bundle.SourceNodeID = nodeID
	if strings.TrimSpace(bundle.CreatedAt) == "" {
		bundle.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if strings.TrimSpace(bundle.ExpiresAt) == "" {
		created, _ := time.Parse(time.RFC3339, bundle.CreatedAt)
		bundle.ExpiresAt = created.Add(5 * time.Minute).UTC().Format(time.RFC3339)
	}
	bundle.Signature = ""
	bundle.BundleID = ""
	core, err := canonical.Marshal(bundle)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(core)
	bundle.BundleID = "evb_" + strings.ToLower(base32NoPadding.EncodeToString(sum[:]))
	payload, err := canonical.Marshal(bundle)
	if err != nil {
		return err
	}
	signature, err := signer.Sign(ctx, payload)
	if err != nil {
		return err
	}
	bundle.Signature = canonical.Base64URL(signature)
	return nil
}

func VerifyBundle(bundle Bundle, publicKey ed25519.PublicKey, expectedPool, expectedRequest, expectedRecipient string, now time.Time) error {
	if bundle.SchemaVersion != SchemaVersion || bundle.Type != BundleType {
		return fmt.Errorf("unsupported evidence bundle")
	}
	if bundle.PoolID != expectedPool || bundle.RequestID != expectedRequest || bundle.RequestingNodeID != expectedRecipient {
		return fmt.Errorf("evidence bundle binding mismatch")
	}
	created, err := time.Parse(time.RFC3339, bundle.CreatedAt)
	if err != nil {
		return fmt.Errorf("invalid created_at")
	}
	expires, err := time.Parse(time.RFC3339, bundle.ExpiresAt)
	if err != nil || !expires.After(created) || now.UTC().After(expires.UTC()) {
		return fmt.Errorf("evidence bundle expired")
	}
	signature, err := canonical.DecodeBase64URL(bundle.Signature)
	if err != nil {
		return fmt.Errorf("invalid evidence signature")
	}
	gotID := bundle.BundleID
	bundle.Signature = ""
	bundle.BundleID = ""
	core, err := canonical.Marshal(bundle)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(core)
	expectedID := "evb_" + strings.ToLower(base32NoPadding.EncodeToString(sum[:]))
	if gotID != expectedID {
		return fmt.Errorf("evidence bundle id mismatch")
	}
	bundle.BundleID = gotID
	payload, err := canonical.Marshal(bundle)
	if err != nil {
		return err
	}
	if !ed25519.Verify(publicKey, payload, signature) {
		return fmt.Errorf("evidence bundle signature verification failed")
	}
	return nil
}

func CanonicalSubjectIdentity(fileName string, fileIndex, fileTotal, totalParts int, fileSize int64) (string, []byte, error) {
	return computeIdentity("subject_multipart_v1", map[string]any{
		"file_name": canonicalName(fileName), "file_index": fileIndex,
		"file_total": fileTotal, "total_parts": totalParts, "file_size": fileSize,
	})
}

func CanonicalYEncIdentity(fileName string, totalParts int, fileSize int64) (string, []byte, error) {
	return computeIdentity("yenc_v1", map[string]any{
		"file_name": canonicalName(fileName), "total_parts": totalParts, "file_size": fileSize,
	})
}

func BinaryContentID(parts []Segment) (string, error) {
	id, _, err := CanonicalContentIdentity(parts)
	return id, err
}

func CanonicalContentIdentity(parts []Segment) (string, []byte, error) {
	ordered := append([]Segment(nil), parts...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].PartNumber < ordered[j].PartNumber })
	core := make([]map[string]any, 0, len(ordered))
	seen := make(map[int]struct{}, len(ordered))
	for _, part := range ordered {
		if part.PartNumber <= 0 || !validMessageID(part.MessageID) {
			return "", nil, fmt.Errorf("invalid content segment")
		}
		if _, ok := seen[part.PartNumber]; ok {
			return "", nil, fmt.Errorf("duplicate content part")
		}
		seen[part.PartNumber] = struct{}{}
		core = append(core, map[string]any{"part_number": part.PartNumber, "message_id": part.MessageID})
	}
	return computeIdentity("content_v1", core)
}

func computeIdentity(scheme string, value any) (string, []byte, error) {
	core, err := canonical.Marshal(map[string]any{"scheme": scheme, "value": value})
	if err != nil {
		return "", nil, err
	}
	sum := sha256.Sum256(core)
	return scheme + ":" + strings.ToLower(base32NoPadding.EncodeToString(sum[:])), core, nil
}

func canonicalName(value string) string {
	return strings.ToLower(strings.TrimSpace(strings.ReplaceAll(value, "\\", "/")))
}

func validMessageID(value string) bool {
	return len(value) >= 3 && len(value) <= 998 && value[0] == '<' && value[len(value)-1] == '>' &&
		!strings.ContainsAny(value, " \t\r\n")
}

func ValidMessageID(value string) bool {
	return validMessageID(strings.TrimSpace(value))
}

func validateCreatedAt(value string, now time.Time) error {
	created, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return fmt.Errorf("invalid created_at")
	}
	delta := now.UTC().Sub(created.UTC())
	if delta < -2*time.Minute || delta > 2*time.Minute {
		return fmt.Errorf("created_at outside tolerance")
	}
	return nil
}
