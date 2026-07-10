package eventbody

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/coverage"
	"github.com/datallboy/gonzb/internal/gonzbnet/events"
	"github.com/datallboy/gonzb/internal/gonzbnet/health"
	"github.com/datallboy/gonzb/internal/gonzbnet/manifestavailability"
	"github.com/datallboy/gonzb/internal/gonzbnet/moderation"
	"github.com/datallboy/gonzb/internal/gonzbnet/pools"
	"github.com/datallboy/gonzb/internal/gonzbnet/releasecard"
	"github.com/datallboy/gonzb/internal/gonzbnet/trust"
	"github.com/datallboy/gonzb/internal/gonzbnet/validation"
)

var privateFieldNames = map[string]struct{}{
	"api_key":              {},
	"apikey":               {},
	"download_history":     {},
	"grab_history":         {},
	"indexer_credentials":  {},
	"nntp_password":        {},
	"nntp_username":        {},
	"provider_credentials": {},
	"search_history":       {},
	"search_query":         {},
	"user_id":              {},
	"username":             {},
}

func Validate(event *events.SignedEvent, now time.Time, futureTolerance time.Duration) error {
	if event == nil {
		return fmt.Errorf("event is required")
	}
	if strings.TrimSpace(event.BodySchema) != pools.BodySchema(event.EventType) {
		return fmt.Errorf("body_schema does not match event_type")
	}
	var raw any
	if err := json.Unmarshal(event.Body, &raw); err != nil {
		return fmt.Errorf("invalid event body json: %w", err)
	}
	if field := findPrivateField(raw); field != "" {
		return fmt.Errorf("private field %q is not allowed", field)
	}
	var metadata struct {
		SchemaVersion string `json:"schema_version"`
		Type          string `json:"type"`
		PoolID        string `json:"pool_id"`
	}
	if err := json.Unmarshal(event.Body, &metadata); err != nil {
		return fmt.Errorf("invalid event body metadata: %w", err)
	}
	if strings.TrimSpace(metadata.SchemaVersion) != "1.0" {
		return fmt.Errorf("unsupported body schema_version")
	}
	if strings.TrimSpace(metadata.Type) != event.EventType {
		return fmt.Errorf("body type does not match event_type")
	}
	if metadata.PoolID != "" && !contains(event.PoolIDs, metadata.PoolID) {
		return fmt.Errorf("body pool_id is not present in event pool_ids")
	}

	switch event.EventType {
	case pools.EventTypeReleaseCard:
		var body releasecard.ReleaseCard
		if err := decode(event.Body, &body); err != nil {
			return err
		}
		return releasecard.Validate(body, now, futureTolerance)
	case pools.EventTypeHealthAttestation:
		var body health.Attestation
		if err := decode(event.Body, &body); err != nil {
			return err
		}
		return health.Validate(body, now, futureTolerance)
	case pools.EventTypeTrustAttestation:
		var body trust.Attestation
		if err := decode(event.Body, &body); err != nil {
			return err
		}
		return trust.Validate(body, now.UTC())
	case pools.EventTypeTombstone:
		var body moderation.Tombstone
		if err := decode(event.Body, &body); err != nil {
			return err
		}
		return moderation.Validate(body, now, futureTolerance)
	case pools.EventTypeValidatorCapacity:
		var body validation.ValidatorCapacity
		if err := decode(event.Body, &body); err != nil {
			return err
		}
		if body.NodeID != event.AuthorNodeID {
			return fmt.Errorf("validator capacity node_id does not match event author")
		}
		return validation.ValidateCapacity(body, now, futureTolerance)
	case pools.EventTypeArticleAvailabilityAttestation:
		var body validation.ArticleAvailabilityAttestation
		if err := decode(event.Body, &body); err != nil {
			return err
		}
		return validation.ValidateArticleAvailability(body, now, futureTolerance)
	case pools.EventTypeChecksumAttestation:
		var body validation.ChecksumAttestation
		if err := decode(event.Body, &body); err != nil {
			return err
		}
		return validation.ValidateChecksum(body, now, futureTolerance)
	case pools.EventTypeManifestAvailability:
		var body manifestavailability.Attestation
		if err := decode(event.Body, &body); err != nil {
			return err
		}
		if body.SourceNodeID != event.AuthorNodeID {
			return fmt.Errorf("manifest availability source_node_id does not match event author")
		}
		return manifestavailability.Validate(body, now, futureTolerance)
	case pools.EventTypePoolGenesis:
		var body pools.Genesis
		return decode(event.Body, &body)
	case pools.EventTypePoolJoinRequest:
		var body pools.JoinRequest
		if err := decode(event.Body, &body); err != nil {
			return err
		}
		if body.CandidateNodeID != event.AuthorNodeID {
			return fmt.Errorf("candidate_node_id does not match event author")
		}
		return nil
	case pools.EventTypePoolMemberApproved:
		var body pools.MemberApproved
		return decode(event.Body, &body)
	case pools.EventTypePoolMemberRevoked:
		var body pools.MemberRevoked
		return decode(event.Body, &body)
	case pools.EventTypePoolCheckpoint:
		var body pools.Checkpoint
		return decode(event.Body, &body)
	default:
		return validateCoverage(event, now, futureTolerance)
	}
}

func validateCoverage(event *events.SignedEvent, now time.Time, futureTolerance time.Duration) error {
	var body any
	switch event.EventType {
	case coverage.TypeScannerCapacity:
		var item coverage.ScannerCapacity
		if err := decode(event.Body, &item); err != nil {
			return err
		}
		if item.NodeID != event.AuthorNodeID {
			return fmt.Errorf("scanner capacity node_id does not match event author")
		}
		body = item
	case coverage.TypeScannerHeartbeat:
		var item coverage.ScannerHeartbeat
		if err := decode(event.Body, &item); err != nil {
			return err
		}
		if item.NodeID != event.AuthorNodeID {
			return fmt.Errorf("scanner heartbeat node_id does not match event author")
		}
		body = item
	case coverage.TypeGroupObservation:
		var item coverage.GroupObservation
		if err := decode(event.Body, &item); err != nil {
			return err
		}
		body = item
	case coverage.TypeCoveragePlan:
		var item coverage.CoveragePlan
		if err := decode(event.Body, &item); err != nil {
			return err
		}
		body = item
	case coverage.TypeCoverageAssignment:
		var item coverage.CoverageAssignment
		if err := decode(event.Body, &item); err != nil {
			return err
		}
		body = item
	case coverage.TypeRangeClaim:
		var item coverage.RangeClaim
		if err := decode(event.Body, &item); err != nil {
			return err
		}
		if item.NodeID != event.AuthorNodeID {
			return fmt.Errorf("range claim node_id does not match event author")
		}
		body = item
	case coverage.TypeTimeWindowClaim:
		var item coverage.TimeWindowClaim
		if err := decode(event.Body, &item); err != nil {
			return err
		}
		if item.NodeID != event.AuthorNodeID {
			return fmt.Errorf("time window claim node_id does not match event author")
		}
		body = item
	case coverage.TypeCoverageCheckpoint:
		var item coverage.CoverageCheckpoint
		if err := decode(event.Body, &item); err != nil {
			return err
		}
		body = item
	case coverage.TypeRangeComplete:
		var item coverage.RangeComplete
		if err := decode(event.Body, &item); err != nil {
			return err
		}
		if item.NodeID != event.AuthorNodeID {
			return fmt.Errorf("range completion node_id does not match event author")
		}
		body = item
	case coverage.TypeRangeFailed:
		var item coverage.RangeFailed
		if err := decode(event.Body, &item); err != nil {
			return err
		}
		if item.NodeID != event.AuthorNodeID {
			return fmt.Errorf("range failure node_id does not match event author")
		}
		body = item
	default:
		return fmt.Errorf("unsupported event_type")
	}
	return coverage.Validate(event.EventType, body, now, futureTolerance)
}

func decode(raw json.RawMessage, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		return fmt.Errorf("invalid %T body: %w", out, err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("invalid event body trailing data")
	}
	return nil
}

func findPrivateField(value any) string {
	switch item := value.(type) {
	case map[string]any:
		for key, child := range item {
			normalized := strings.ToLower(strings.TrimSpace(key))
			if _, found := privateFieldNames[normalized]; found {
				return normalized
			}
			if found := findPrivateField(child); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range item {
			if found := findPrivateField(child); found != "" {
				return found
			}
		}
	}
	return ""
}

func contains(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
