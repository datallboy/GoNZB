package pgindex

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/datallboy/gonzb/internal/gonzbnet/evidence"
)

type YEncEvidenceRecord struct {
	SourcePostedAt  time.Time
	MessageID       string
	FileName        string
	PartNumber      int
	TotalParts      int
	FileSize        int64
	PartBegin       int64
	PartEnd         int64
	Provenance      string
	SourcePoolID    string
	SourceNodeID    string
	SourceBundleID  string
	AcceptanceState string
}

type BinaryExchangeIdentityCandidate struct {
	BinaryID   int64
	FileName   string
	FileIndex  int
	FileTotal  int
	TotalParts int
	FileSize   int64
	Recovered  bool
	Confidence float64
}

type BinaryEvidenceDiagnostic struct {
	PoolID              string
	PeerNodeID          string
	Direction           string
	EvidenceKind        string
	RequestCount        int
	HitCount            int
	ItemCount           int
	BodyRequestsAvoided int
	ResponseBytes       int64
	Conflicts           int
	Quarantines         int
	LatencyMS           int64
	ErrorText           string
}

type BinaryEvidenceDiagnosticRecord struct {
	DiagnosticID        int64     `json:"diagnostic_id"`
	PoolID              string    `json:"pool_id"`
	PeerNodeID          string    `json:"peer_node_id"`
	Direction           string    `json:"direction"`
	EvidenceKind        string    `json:"evidence_kind"`
	RequestCount        int       `json:"request_count"`
	HitCount            int       `json:"hit_count"`
	ItemCount           int       `json:"item_count"`
	BodyRequestsAvoided int       `json:"body_requests_avoided"`
	ResponseBytes       int64     `json:"response_bytes"`
	Conflicts           int       `json:"conflicts"`
	Quarantines         int       `json:"quarantines"`
	LatencyMS           int64     `json:"latency_ms"`
	ErrorText           string    `json:"error_text,omitempty"`
	CreatedAt           time.Time `json:"created_at"`
}

type BinaryEvidencePeer struct {
	PoolID  string
	NodeID  string
	BaseURL string
}

type BinaryEvidenceRepairCandidate struct {
	BinaryID     int64
	Scheme       string
	MatchID      string
	MissingParts []int
	Anchors      []string
}

type binaryExchangeIdentityWrite struct {
	BinaryID   int64           `json:"binary_id"`
	Scheme     string          `json:"scheme"`
	MatchID    string          `json:"match_id"`
	Identity   json.RawMessage `json:"identity"`
	Confidence float64         `json:"confidence"`
}

func (s *Store) SeedBinaryEvidenceRepairWork(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	result, err := s.db.ExecContext(ctx, `
		WITH latest_stats AS (
			SELECT DISTINCT ON (bos.binary_id)
			       bos.binary_id, bos.total_parts, bos.updated_at
			FROM binary_observation_stats bos
			WHERE bos.total_parts > 1
			ORDER BY bos.binary_id, bos.updated_at DESC
		), incomplete AS (
			SELECT stats.binary_id, identity.scheme, identity.match_id
			FROM latest_stats stats
			JOIN LATERAL (
				SELECT candidate.scheme, candidate.match_id, candidate.confidence
				FROM binary_exchange_identities candidate
				WHERE candidate.binary_id = stats.binary_id
				  AND candidate.scheme IN ('yenc_v1', 'subject_multipart_v1')
				ORDER BY CASE candidate.scheme WHEN 'yenc_v1' THEN 0 ELSE 1 END,
				         candidate.confidence DESC,
				         candidate.updated_at DESC
				LIMIT 1
			) identity ON TRUE
			WHERE identity.confidence >= 0.70
			  AND stats.total_parts > (
			    SELECT COUNT(DISTINCT part.part_number)
			    FROM binary_effective_parts part
			    WHERE part.binary_id = stats.binary_id
			  )
			  AND NOT EXISTS (
			    SELECT 1 FROM binary_evidence_repair_work_items work
			    WHERE work.binary_id = stats.binary_id
			      AND work.status IN ('ready', 'running')
			  )
			ORDER BY stats.updated_at DESC
			LIMIT $1
		)
		INSERT INTO binary_evidence_repair_work_items (binary_id, scheme, match_id)
		SELECT binary_id, scheme, match_id
		FROM incomplete
		ON CONFLICT (binary_id) DO UPDATE SET
			scheme = EXCLUDED.scheme,
			match_id = EXCLUDED.match_id,
			status = CASE
				WHEN binary_evidence_repair_work_items.status = 'done' THEN 'ready'
				ELSE binary_evidence_repair_work_items.status
			END,
			ready_at = CASE
				WHEN binary_evidence_repair_work_items.status = 'done' THEN NOW()
				ELSE binary_evidence_repair_work_items.ready_at
			END,
			updated_at = NOW()`, limit)
	if err != nil {
		return 0, fmt.Errorf("seed binary evidence repair work: %w", err)
	}
	count, _ := result.RowsAffected()
	return int(count), nil
}

func (s *Store) ClaimBinaryEvidenceRepairWork(ctx context.Context, owner string, limit int, lease time.Duration) ([]BinaryEvidenceRepairCandidate, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	if lease <= 0 {
		lease = 2 * time.Minute
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollbackTx(tx)
	rows, err := tx.QueryContext(ctx, `
		WITH claim AS (
			SELECT work.binary_id
			FROM binary_evidence_repair_work_items work
			WHERE (
			    work.status = 'ready' AND work.ready_at <= NOW()
			  ) OR (
			    work.status = 'running' AND work.lease_expires_at < NOW()
			  )
			ORDER BY work.ready_at, work.binary_id
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		)
		UPDATE binary_evidence_repair_work_items work
		SET status = 'running',
		    attempts = attempts + 1,
		    lease_owner = $2,
		    lease_expires_at = NOW() + ($3 * INTERVAL '1 millisecond'),
		    updated_at = NOW()
		FROM claim
		WHERE work.binary_id = claim.binary_id
		RETURNING work.binary_id, work.scheme, work.match_id`,
		limit, strings.TrimSpace(owner), lease.Milliseconds())
	if err != nil {
		return nil, err
	}
	candidates := make([]BinaryEvidenceRepairCandidate, 0)
	for rows.Next() {
		var item BinaryEvidenceRepairCandidate
		if err := rows.Scan(&item.BinaryID, &item.Scheme, &item.MatchID); err != nil {
			rows.Close()
			return nil, err
		}
		candidates = append(candidates, item)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	for index := range candidates {
		item := &candidates[index]
		missingRows, err := tx.QueryContext(ctx, `
			WITH expected AS (
				SELECT generate_series(1, MAX(total_parts)) AS part_number
				FROM binary_effective_parts
				WHERE binary_id = $1
			)
			SELECT expected.part_number
			FROM expected
			WHERE NOT EXISTS (
				SELECT 1 FROM binary_effective_parts part
				WHERE part.binary_id = $1
				  AND part.part_number = expected.part_number
			)
			ORDER BY expected.part_number
			LIMIT $2`, item.BinaryID, evidence.MaxSegmentItems)
		if err != nil {
			return nil, err
		}
		for missingRows.Next() {
			var part int
			if err := missingRows.Scan(&part); err != nil {
				missingRows.Close()
				return nil, err
			}
			item.MissingParts = append(item.MissingParts, part)
		}
		missingRows.Close()
		anchorRows, err := tx.QueryContext(ctx, `
			SELECT message_id
			FROM binary_effective_parts
			WHERE binary_id = $1
			ORDER BY part_number
			LIMIT 8`, item.BinaryID)
		if err != nil {
			return nil, err
		}
		for anchorRows.Next() {
			var messageID string
			if err := anchorRows.Scan(&messageID); err != nil {
				anchorRows.Close()
				return nil, err
			}
			item.Anchors = append(item.Anchors, messageID)
		}
		anchorRows.Close()
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return candidates, nil
}

func (s *Store) CompleteBinaryEvidenceRepairWork(ctx context.Context, binaryID int64, completed bool, errText string) error {
	status := "ready"
	if completed {
		status = "done"
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE binary_evidence_repair_work_items
		SET status = $2,
		    ready_at = CASE
		      WHEN $2 = 'ready' THEN NOW() + LEAST(INTERVAL '1 hour', (INTERVAL '1 minute' * GREATEST(1, attempts)))
		      ELSE ready_at
		    END,
		    lease_owner = NULL,
		    lease_expires_at = NULL,
		    last_error = $3,
		    updated_at = NOW()
		WHERE binary_id = $1`, binaryID, status, strings.TrimSpace(errText))
	return err
}

func (s *Store) ListBinaryEvidencePeers(ctx context.Context, localNodeID string, limit int) ([]BinaryEvidencePeer, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 || limit > 20 {
		limit = 3
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT local.pool_id, remote.node_id
		FROM pool_members local
		JOIN trust_pools pool ON pool.pool_id = local.pool_id
		JOIN pool_members remote
		  ON remote.pool_id = local.pool_id
		 AND remote.node_id <> local.node_id
		JOIN federation_nodes node ON node.node_id = remote.node_id
		WHERE local.node_id = $1
		  AND local.status = 'active'
		  AND remote.status = 'active'
		  AND pool.enabled = TRUE
		  AND COALESCE((pool.policy_json->>'allow_binary_evidence_exchange')::boolean, FALSE)
		  AND node.status <> 'blocked'
		  AND (
		    local.role = 'admin' OR local.allowed_capabilities ? 'binary_evidence_exchange'
		  )
		  AND (
		    remote.role = 'admin' OR remote.allowed_capabilities ? 'binary_evidence_exchange'
		  )
		ORDER BY local.pool_id, remote.node_id
		LIMIT $2`, strings.TrimSpace(localNodeID), limit)
	if err != nil {
		return nil, fmt.Errorf("list binary evidence peers: %w", err)
	}
	defer rows.Close()
	out := make([]BinaryEvidencePeer, 0)
	for rows.Next() {
		var item BinaryEvidencePeer
		if err := rows.Scan(&item.PoolID, &item.NodeID); err != nil {
			return nil, err
		}
		endpoint, err := s.ResolveFederationNodeEndpoint(ctx, item.NodeID)
		if err != nil {
			return nil, err
		}
		if endpoint == nil {
			continue
		}
		item.BaseURL = endpoint.Locator
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) UpsertYEncHeaderEvidence(ctx context.Context, records []YEncEvidenceRecord) (int, int, error) {
	if s == nil || s.db == nil {
		return 0, 0, fmt.Errorf("pgindex store is not initialized")
	}
	if len(records) == 0 {
		return 0, 0, nil
	}
	type payloadRecord struct {
		SourcePostedAt time.Time `json:"source_posted_at"`
		MessageID      string    `json:"message_id"`
		FileName       string    `json:"file_name"`
		PartNumber     int       `json:"part_number"`
		TotalParts     int       `json:"total_parts"`
		FileSize       int64     `json:"file_size"`
		PartBegin      int64     `json:"part_begin"`
		PartEnd        int64     `json:"part_end"`
		Provenance     string    `json:"provenance"`
		SourcePoolID   string    `json:"source_pool_id"`
		SourceNodeID   string    `json:"source_node_id"`
		SourceBundleID string    `json:"source_bundle_id"`
		State          string    `json:"acceptance_state"`
	}
	payload := make([]payloadRecord, 0, len(records))
	seenKeys := make(map[string]struct{}, len(records))
	messageFacts := make(map[string]string, len(records))
	for _, record := range records {
		record.MessageID = strings.TrimSpace(record.MessageID)
		record.FileName = strings.TrimSpace(record.FileName)
		record.Provenance = strings.TrimSpace(record.Provenance)
		state := strings.TrimSpace(record.AcceptanceState)
		if state == "" {
			state = "accepted"
		}
		if record.SourcePostedAt.IsZero() || !evidence.ValidMessageID(record.MessageID) || record.FileName == "" ||
			(record.Provenance != "local_body" && record.Provenance != "peer") ||
			(state != "accepted" && state != "quarantined" && state != "rejected") {
			return 0, 0, fmt.Errorf("invalid yenc evidence record")
		}
		key := record.MessageID + "\x00" + record.Provenance + "\x00" + record.SourceNodeID
		if _, duplicate := seenKeys[key]; duplicate {
			return 0, 0, fmt.Errorf("duplicate yenc evidence source record")
		}
		seenKeys[key] = struct{}{}
		facts := fmt.Sprintf("%s\x00%d\x00%d\x00%d", record.FileName, record.PartNumber, record.TotalParts, record.FileSize)
		if previous, exists := messageFacts[record.MessageID]; exists && previous != facts {
			return 0, 0, fmt.Errorf("conflicting yenc evidence in one batch")
		}
		messageFacts[record.MessageID] = facts
		payload = append(payload, payloadRecord{
			SourcePostedAt: record.SourcePostedAt.UTC(), MessageID: record.MessageID,
			FileName: record.FileName, PartNumber: record.PartNumber,
			TotalParts: record.TotalParts, FileSize: record.FileSize,
			PartBegin: record.PartBegin, PartEnd: record.PartEnd,
			Provenance: record.Provenance, SourcePoolID: record.SourcePoolID,
			SourceNodeID: record.SourceNodeID, SourceBundleID: record.SourceBundleID,
			State: state,
		})
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer rollbackTx(tx)
	var accepted, quarantined int
	err = tx.QueryRowContext(ctx, `
		WITH input AS MATERIALIZED (
			SELECT *
			FROM jsonb_to_recordset($1::jsonb) AS item(
				source_posted_at timestamptz,
				message_id text,
				file_name text,
				part_number integer,
				total_parts integer,
				file_size bigint,
				part_begin bigint,
				part_end bigint,
				provenance text,
				source_pool_id text,
				source_node_id text,
				source_bundle_id text,
				acceptance_state text
			)
		), quarantine_peers AS (
			UPDATE yenc_header_evidence existing
			SET acceptance_state = 'quarantined',
			    updated_at = NOW()
			FROM input
			WHERE input.provenance = 'local_body'
			  AND existing.message_id = input.message_id
			  AND existing.provenance = 'peer'
			  AND existing.acceptance_state = 'accepted'
			  AND (
			    existing.decoded_file_name <> input.file_name
			    OR existing.part_number <> input.part_number
			    OR existing.total_parts <> input.total_parts
			    OR existing.file_size <> input.file_size
			  )
			RETURNING existing.evidence_id
		), persisted AS (
			INSERT INTO yenc_header_evidence (
				source_posted_at, message_id, decoded_file_name, part_number,
				total_parts, file_size, part_begin, part_end, provenance,
				source_pool_id, source_node_id, source_bundle_id, acceptance_state
			)
			SELECT input.source_posted_at, input.message_id, input.file_name,
			       input.part_number, input.total_parts, input.file_size,
			       input.part_begin, input.part_end, input.provenance,
			       input.source_pool_id, input.source_node_id, input.source_bundle_id,
			       CASE WHEN input.provenance = 'peer' AND EXISTS (
			         SELECT 1
			         FROM yenc_header_evidence existing
			         WHERE existing.message_id = input.message_id
			           AND existing.acceptance_state = 'accepted'
			           AND (
			             existing.decoded_file_name <> input.file_name
			             OR existing.part_number <> input.part_number
			             OR existing.total_parts <> input.total_parts
			             OR existing.file_size <> input.file_size
			           )
			       ) THEN 'quarantined' ELSE input.acceptance_state END
			FROM input
			ON CONFLICT (message_id, provenance, source_node_id) DO UPDATE SET
				acceptance_state = CASE
					WHEN yenc_header_evidence.decoded_file_name <> EXCLUDED.decoded_file_name
					  OR yenc_header_evidence.part_number <> EXCLUDED.part_number
					  OR yenc_header_evidence.total_parts <> EXCLUDED.total_parts
					  OR yenc_header_evidence.file_size <> EXCLUDED.file_size
					THEN CASE
					  WHEN EXCLUDED.provenance = 'local_body' THEN yenc_header_evidence.acceptance_state
					  ELSE 'quarantined'
					END
					ELSE EXCLUDED.acceptance_state
				END,
				source_bundle_id = EXCLUDED.source_bundle_id,
				updated_at = NOW()
			RETURNING acceptance_state
		)
		SELECT COUNT(*) FILTER (WHERE acceptance_state = 'accepted'),
		       COUNT(*) FILTER (WHERE acceptance_state = 'quarantined')
		FROM persisted`, string(raw)).Scan(&accepted, &quarantined)
	if err != nil {
		return 0, 0, fmt.Errorf("bulk upsert yenc evidence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	return accepted, quarantined, nil
}

func (s *Store) FindAcceptedYEncEvidence(ctx context.Context, messageIDs []string, localOnly bool, limit int) ([]YEncEvidenceRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	messageIDs = normalizeStrings(messageIDs)
	if len(messageIDs) == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > evidence.MaxYEncQueryItems {
		limit = evidence.MaxYEncQueryItems
	}
	raw, _ := json.Marshal(messageIDs)
	rows, err := s.db.QueryContext(ctx, `
		WITH requested AS (
			SELECT value AS message_id
			FROM jsonb_array_elements_text($1::jsonb)
		), ranked AS (
			SELECT e.*,
			       ROW_NUMBER() OVER (
			         PARTITION BY e.message_id
			         ORDER BY CASE e.provenance WHEN 'local_body' THEN 0 ELSE 1 END,
			                  e.updated_at DESC
			       ) AS rank
			FROM yenc_header_evidence e
			JOIN requested r ON r.message_id = e.message_id
			WHERE e.acceptance_state = 'accepted'
			  AND (NOT $2 OR e.provenance = 'local_body')
		)
		SELECT source_posted_at, message_id, decoded_file_name, part_number,
		       total_parts, file_size, part_begin, part_end, provenance,
		       source_pool_id, source_node_id, source_bundle_id, acceptance_state
		FROM ranked
		WHERE rank = 1
		ORDER BY message_id
		LIMIT $3`, raw, localOnly, limit)
	if err != nil {
		return nil, fmt.Errorf("find accepted yenc evidence: %w", err)
	}
	defer rows.Close()
	out := make([]YEncEvidenceRecord, 0)
	for rows.Next() {
		var item YEncEvidenceRecord
		if err := rows.Scan(
			&item.SourcePostedAt, &item.MessageID, &item.FileName,
			&item.PartNumber, &item.TotalParts, &item.FileSize,
			&item.PartBegin, &item.PartEnd, &item.Provenance,
			&item.SourcePoolID, &item.SourceNodeID, &item.SourceBundleID,
			&item.AcceptanceState,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) RefreshBinaryExchangeIdentities(ctx context.Context, limit int) (int, error) {
	if s == nil || s.db == nil {
		return 0, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := s.db.QueryContext(ctx, `
		WITH identity AS (
			SELECT DISTINCT ON (bic.binary_id)
			       bic.binary_id, bic.file_name, bic.file_index,
			       bic.expected_file_count, bic.match_confidence,
			       COALESCE(NULLIF(brc.recovered_file_name, ''), bic.file_name) AS recovered_name,
			       COALESCE(NULLIF(bos.total_parts, 0), 0) AS total_parts,
			       COALESCE((
			         SELECT MAX(NULLIF(bp.total_parts, 0))
			         FROM binary_parts bp
			         WHERE bp.binary_id = bic.binary_id
			       ), 0) AS part_total,
			       COALESCE((
			         SELECT MAX(NULLIF(payload.yenc_file_size, 0))
			         FROM binary_parts bp
			         JOIN article_header_ingest_payloads payload
			           ON payload.source_posted_at = bp.source_posted_at
			          AND payload.article_header_id = bp.article_header_id
			         WHERE bp.binary_id = bic.binary_id
			       ), 0) AS file_size,
			       COALESCE(brc.recovered_file_name <> '', FALSE) AS recovered
			FROM binary_identity_current bic
			LEFT JOIN binary_observation_stats bos
			  ON bos.binary_id = bic.binary_id
			 AND bos.source_posted_at = bic.source_posted_at
			LEFT JOIN binary_recovery_current brc
			  ON brc.binary_id = bic.binary_id
			 AND brc.source_posted_at = bic.source_posted_at
			WHERE NOT EXISTS (
				SELECT 1 FROM binary_exchange_identities bei
				WHERE bei.binary_id = bic.binary_id
				  AND bei.scheme = CASE
				    WHEN COALESCE(brc.recovered_file_name, '') <> '' THEN 'yenc_v1'
				    ELSE 'subject_multipart_v1'
				  END
				  AND bei.updated_at >= GREATEST(
				    bic.updated_at,
				    COALESCE(brc.updated_at, bic.updated_at),
				    COALESCE(bos.updated_at, bic.updated_at)
				  )
			)
			ORDER BY bic.binary_id, bic.updated_at DESC
			LIMIT $1
		)
		SELECT binary_id, file_name, file_index, expected_file_count,
		       GREATEST(total_parts, part_total), file_size, recovered_name,
		       recovered, match_confidence
		FROM identity`, limit)
	if err != nil {
		return 0, fmt.Errorf("list binary exchange identities: %w", err)
	}
	defer rows.Close()
	type pending struct {
		candidate     BinaryExchangeIdentityCandidate
		recoveredName string
	}
	items := make([]pending, 0)
	for rows.Next() {
		var item pending
		if err := rows.Scan(
			&item.candidate.BinaryID, &item.candidate.FileName,
			&item.candidate.FileIndex, &item.candidate.FileTotal,
			&item.candidate.TotalParts, &item.candidate.FileSize,
			&item.recoveredName, &item.candidate.Recovered,
			&item.candidate.Confidence,
		); err != nil {
			return 0, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	if err := rows.Close(); err != nil {
		return 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer rollbackTx(tx)
	identityWrites := make([]binaryExchangeIdentityWrite, 0, len(items))
	for _, item := range items {
		scheme := "subject_multipart_v1"
		name := item.candidate.FileName
		if item.candidate.Recovered && strings.TrimSpace(item.recoveredName) != "" {
			scheme = "yenc_v1"
			name = item.recoveredName
		}
		var matchID string
		var canonicalIdentity []byte
		if scheme == "yenc_v1" {
			matchID, canonicalIdentity, err = evidence.CanonicalYEncIdentity(name, item.candidate.TotalParts, item.candidate.FileSize)
		} else {
			matchID, canonicalIdentity, err = evidence.CanonicalSubjectIdentity(
				name, item.candidate.FileIndex, item.candidate.FileTotal,
				item.candidate.TotalParts, item.candidate.FileSize,
			)
		}
		if err != nil || strings.TrimSpace(name) == "" || item.candidate.TotalParts <= 0 {
			continue
		}
		identityWrites = append(identityWrites, binaryExchangeIdentityWrite{
			BinaryID: item.candidate.BinaryID, Scheme: scheme, MatchID: matchID,
			Identity: canonicalIdentity, Confidence: item.candidate.Confidence,
		})
	}
	written, err := bulkUpsertBinaryExchangeIdentities(ctx, tx, identityWrites)
	if err != nil {
		return 0, err
	}
	contentRows, err := tx.QueryContext(ctx, `
		WITH latest_stats AS (
			SELECT DISTINCT ON (stats.binary_id)
			       stats.binary_id, stats.total_parts, stats.observed_parts
			FROM binary_observation_stats stats
			WHERE stats.total_parts > 0
			  AND stats.observed_parts >= stats.total_parts
			  AND NOT EXISTS (
			    SELECT 1
			    FROM binary_exchange_identities identity
			    WHERE identity.binary_id = stats.binary_id
			      AND identity.scheme = 'content_v1'
			  )
			ORDER BY stats.binary_id, stats.updated_at DESC
		), ranked_parts AS (
			SELECT part.binary_id, part.part_number, part.message_id,
			       ROW_NUMBER() OVER (
			         PARTITION BY part.binary_id, part.part_number
			         ORDER BY CASE part.part_source WHEN 'local' THEN 0 ELSE 1 END,
			                  part.source_posted_at, part.message_id
			       ) AS keep_rank
			FROM binary_effective_parts part
			JOIN latest_stats stats ON stats.binary_id = part.binary_id
		), complete AS (
			SELECT parts.binary_id,
			       jsonb_agg(
			         jsonb_build_object(
			           'part_number', parts.part_number,
			           'message_id', parts.message_id
			         )
			         ORDER BY parts.part_number
			       ) AS parts
			FROM ranked_parts parts
			JOIN latest_stats stats ON stats.binary_id = parts.binary_id
			WHERE parts.keep_rank = 1
			GROUP BY parts.binary_id, stats.total_parts
			HAVING COUNT(*) = stats.total_parts
			ORDER BY parts.binary_id
			LIMIT $1
		)
		SELECT binary_id, parts
		FROM complete`, limit)
	if err != nil {
		return 0, fmt.Errorf("list complete binary content identities: %w", err)
	}
	type contentCandidate struct {
		binaryID int64
		parts    []evidence.Segment
	}
	contentItems := make([]contentCandidate, 0)
	for contentRows.Next() {
		var binaryID int64
		var raw []byte
		if err := contentRows.Scan(&binaryID, &raw); err != nil {
			contentRows.Close()
			return 0, err
		}
		var parts []evidence.Segment
		if err := json.Unmarshal(raw, &parts); err != nil {
			contentRows.Close()
			return 0, fmt.Errorf("decode complete binary content identity: %w", err)
		}
		contentItems = append(contentItems, contentCandidate{binaryID: binaryID, parts: parts})
	}
	if err := contentRows.Close(); err != nil {
		return 0, err
	}
	contentWrites := make([]binaryExchangeIdentityWrite, 0, len(contentItems))
	for _, item := range contentItems {
		matchID, canonicalIdentity, err := evidence.CanonicalContentIdentity(item.parts)
		if err != nil {
			continue
		}
		contentWrites = append(contentWrites, binaryExchangeIdentityWrite{
			BinaryID: item.binaryID, Scheme: "content_v1", MatchID: matchID,
			Identity: canonicalIdentity, Confidence: 1,
		})
	}
	contentWritten, err := bulkUpsertBinaryExchangeIdentities(ctx, tx, contentWrites)
	if err != nil {
		return 0, err
	}
	written += contentWritten
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return written, nil
}

func bulkUpsertBinaryExchangeIdentities(ctx context.Context, tx *sql.Tx, items []binaryExchangeIdentityWrite) (int, error) {
	if len(items) == 0 {
		return 0, nil
	}
	raw, err := json.Marshal(items)
	if err != nil {
		return 0, err
	}
	var written int
	err = tx.QueryRowContext(ctx, `
		WITH input AS (
			SELECT *
			FROM jsonb_to_recordset($1::jsonb) AS item(
				binary_id bigint,
				scheme text,
				match_id text,
				identity jsonb,
				confidence double precision
			)
		), persisted AS (
			INSERT INTO binary_exchange_identities
				(binary_id, scheme, match_id, identity_json, confidence)
			SELECT binary_id, scheme, match_id, identity, confidence
			FROM input
			ON CONFLICT (binary_id, scheme) DO UPDATE SET
				match_id = EXCLUDED.match_id,
				identity_json = EXCLUDED.identity_json,
				confidence = EXCLUDED.confidence,
				updated_at = NOW()
			RETURNING 1
		)
		SELECT COUNT(*) FROM persisted`, string(raw)).Scan(&written)
	if err != nil {
		return 0, fmt.Errorf("bulk upsert binary exchange identities: %w", err)
	}
	return written, nil
}

func (s *Store) FindLocalBinarySegments(ctx context.Context, scheme, matchID string, parts []int, anchors []string, limit int) ([]evidence.Segment, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if len(parts) == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > evidence.MaxSegmentItems {
		limit = evidence.MaxSegmentItems
	}
	partsJSON, _ := json.Marshal(parts)
	anchorsJSON, _ := json.Marshal(normalizeStrings(anchors))
	rows, err := s.db.QueryContext(ctx, `
		WITH candidate AS (
			SELECT bei.binary_id
			FROM binary_exchange_identities bei
			WHERE bei.scheme = $1 AND bei.match_id = $2
			  AND (
			    jsonb_array_length($4::jsonb) = 0 OR EXISTS (
			      SELECT 1 FROM binary_parts anchor
			      WHERE anchor.binary_id = bei.binary_id
			        AND anchor.message_id IN (
			          SELECT value FROM jsonb_array_elements_text($4::jsonb)
			        )
			    )
			  )
		), unique_candidate AS (
			SELECT MIN(binary_id) AS binary_id
			FROM candidate
			HAVING COUNT(*) = 1
		), requested AS (
			SELECT value::integer AS part_number
			FROM jsonb_array_elements_text($3::jsonb)
		)
		SELECT DISTINCT ON (bp.part_number)
		       bp.part_number, bp.total_parts, bp.message_id, bp.segment_bytes,
		       ah.date_utc, bp.source_posted_at, bp.file_name,
		       COALESCE(NULLIF(payload.yenc_file_size, 0), 0),
		       COALESCE((
		         SELECT jsonb_agg(DISTINCT groups.name ORDER BY groups.name)
		         FROM (
		           SELECT ng.group_name AS name
		           WHERE ng.group_name <> ''
		           UNION
		           SELECT cg.observed_group_name
		           FROM article_header_crosspost_groups cg
		           WHERE cg.source_posted_at = bp.source_posted_at
		             AND cg.article_header_id = bp.article_header_id
		             AND cg.observed_group_name <> ''
		         ) groups
		       ), '[]'::jsonb)
		FROM unique_candidate uc
		JOIN binary_parts bp ON bp.binary_id = uc.binary_id
		JOIN requested req ON req.part_number = bp.part_number
		LEFT JOIN article_headers ah
		  ON ah.source_posted_at = bp.source_posted_at
		 AND ah.id = bp.article_header_id
		LEFT JOIN article_header_ingest_payloads payload
		  ON payload.source_posted_at = bp.source_posted_at
		 AND payload.article_header_id = bp.article_header_id
		LEFT JOIN newsgroups ng ON ng.id = ah.newsgroup_id
		ORDER BY bp.part_number, bp.source_posted_at DESC, bp.id DESC
		LIMIT $5`, scheme, matchID, partsJSON, anchorsJSON, limit)
	if err != nil {
		return nil, fmt.Errorf("find local binary segments: %w", err)
	}
	defer rows.Close()
	out := make([]evidence.Segment, 0)
	for rows.Next() {
		var item evidence.Segment
		var postedAt sql.NullTime
		var sourcePostedAt time.Time
		var groupsJSON []byte
		if err := rows.Scan(
			&item.PartNumber, &item.TotalParts, &item.MessageID, &item.Bytes,
			&postedAt, &sourcePostedAt, &item.FileName, &item.FileSize, &groupsJSON,
		); err != nil {
			return nil, err
		}
		if postedAt.Valid {
			item.PostedAt = postedAt.Time.UTC().Format(time.RFC3339)
		}
		item.SourcePostedAt = sourcePostedAt.UTC().Format(time.RFC3339)
		_ = json.Unmarshal(groupsJSON, &item.Groups)
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) ImportPeerSegments(ctx context.Context, binaryID int64, poolID, nodeID, bundleID string, segments []evidence.Segment) (int, int, error) {
	if s == nil || s.db == nil {
		return 0, 0, fmt.Errorf("pgindex store is not initialized")
	}
	if binaryID <= 0 {
		return 0, 0, fmt.Errorf("binary_id is required")
	}
	type segmentPayload struct {
		SourcePostedAt time.Time  `json:"source_posted_at"`
		PartNumber     int        `json:"part_number"`
		TotalParts     int        `json:"total_parts"`
		MessageID      string     `json:"message_id"`
		Bytes          int64      `json:"segment_bytes"`
		PostedAt       *time.Time `json:"posted_at,omitempty"`
		Groups         []string   `json:"observed_groups"`
		FileName       string     `json:"file_name"`
		FileSize       int64      `json:"file_size"`
	}
	payload := make([]segmentPayload, 0, len(segments))
	seenParts := make(map[int]struct{}, len(segments))
	for _, segment := range segments {
		sourcePostedAt, err := time.Parse(time.RFC3339, segment.SourcePostedAt)
		if err != nil || segment.PartNumber <= 0 || segment.TotalParts < segment.PartNumber ||
			!evidence.ValidMessageID(segment.MessageID) || segment.Bytes < 0 || segment.FileSize < 0 {
			return 0, 0, fmt.Errorf("invalid peer segment")
		}
		if _, duplicate := seenParts[segment.PartNumber]; duplicate {
			return 0, 0, fmt.Errorf("duplicate peer segment part")
		}
		seenParts[segment.PartNumber] = struct{}{}
		var postedAt *time.Time
		if strings.TrimSpace(segment.PostedAt) != "" {
			value, err := time.Parse(time.RFC3339, segment.PostedAt)
			if err != nil {
				return 0, 0, fmt.Errorf("invalid peer segment posted date")
			}
			value = value.UTC()
			postedAt = &value
		}
		payload = append(payload, segmentPayload{
			SourcePostedAt: sourcePostedAt.UTC(), PartNumber: segment.PartNumber,
			TotalParts: segment.TotalParts, MessageID: strings.TrimSpace(segment.MessageID),
			Bytes: segment.Bytes, PostedAt: postedAt,
			Groups: normalizeStrings(segment.Groups), FileName: strings.TrimSpace(segment.FileName),
			FileSize: segment.FileSize,
		})
	}
	if len(payload) == 0 {
		return 0, 0, nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, 0, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer rollbackTx(tx)
	var imported, conflicts int
	if err := tx.QueryRowContext(ctx, `
		WITH input AS MATERIALIZED (
			SELECT *
			FROM jsonb_to_recordset($1::jsonb) AS item(
				source_posted_at timestamptz,
				part_number integer,
				total_parts integer,
				message_id text,
				segment_bytes bigint,
				posted_at timestamptz,
				observed_groups jsonb,
				file_name text,
				file_size bigint
			)
		), persisted AS (
			INSERT INTO binary_peer_segments (
				binary_id, source_posted_at, part_number, total_parts,
				message_id, segment_bytes, posted_at, observed_groups,
				file_name, file_size, source_pool_id, source_node_id,
				source_bundle_id, acceptance_state
			)
			SELECT $2, input.source_posted_at, input.part_number,
			       input.total_parts, input.message_id, input.segment_bytes,
			       input.posted_at, input.observed_groups, input.file_name,
			       input.file_size, $3, $4, $5,
				CASE WHEN EXISTS (
					SELECT 1 FROM binary_parts local
					WHERE local.binary_id = $2
					  AND local.part_number = input.part_number
					  AND local.message_id <> input.message_id
					) THEN 'quarantined' ELSE 'accepted' END
				FROM input
				ON CONFLICT (binary_id, part_number, message_id) DO NOTHING
				RETURNING acceptance_state
		)
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE acceptance_state = 'quarantined')
		FROM persisted`,
		string(raw), binaryID, strings.TrimSpace(poolID),
		strings.TrimSpace(nodeID), strings.TrimSpace(bundleID),
	).Scan(&imported, &conflicts); err != nil {
		return 0, 0, fmt.Errorf("bulk import peer segments: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}
	if imported > 0 {
		if err := s.RefreshBinaryStatsForPeerEvidence(ctx, binaryID); err != nil {
			return imported, conflicts, err
		}
	}
	return imported, conflicts, nil
}

func (s *Store) RefreshBinaryStatsForPeerEvidence(ctx context.Context, binaryID int64) error {
	if s == nil || s.db == nil || binaryID <= 0 {
		return fmt.Errorf("store and binary_id are required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackTx(tx)
	if _, err := refreshBinaryStatsIDsInTx(ctx, tx, []int64{binaryID}); err != nil {
		return fmt.Errorf("refresh binary stats for peer evidence: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func (s *Store) BinaryEffectiveComplete(ctx context.Context, binaryID int64) (bool, error) {
	if s == nil || s.db == nil || binaryID <= 0 {
		return false, fmt.Errorf("store and binary_id are required")
	}
	var complete bool
	err := s.db.QueryRowContext(ctx, `
		SELECT COALESCE((
			SELECT stats.total_parts > 0 AND stats.observed_parts >= stats.total_parts
			FROM binary_observation_stats stats
			WHERE stats.binary_id = $1
			ORDER BY stats.updated_at DESC
			LIMIT 1
		), FALSE)`, binaryID).Scan(&complete)
	return complete, err
}

func (s *Store) ResolveBinaryExchangeIdentity(ctx context.Context, scheme, matchID string, anchors []string) (int64, error) {
	anchorsJSON, _ := json.Marshal(normalizeStrings(anchors))
	var binaryID int64
	err := s.db.QueryRowContext(ctx, `
		WITH candidate AS (
			SELECT bei.binary_id
			FROM binary_exchange_identities bei
			WHERE bei.scheme = $1 AND bei.match_id = $2
			  AND (
			    jsonb_array_length($3::jsonb) = 0 OR EXISTS (
			      SELECT 1 FROM binary_effective_parts part
			      WHERE part.binary_id = bei.binary_id
			        AND part.message_id IN (
			          SELECT value FROM jsonb_array_elements_text($3::jsonb)
			        )
			    )
			  )
		)
		SELECT MIN(binary_id)
		FROM candidate
		HAVING COUNT(*) = 1`, scheme, matchID, anchorsJSON).Scan(&binaryID)
	return binaryID, err
}

func (s *Store) RefundBodyRequestBudget(ctx context.Context, budgetKey string, requests int) error {
	if s == nil || s.db == nil || strings.TrimSpace(budgetKey) == "" || requests <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		UPDATE indexer_body_request_budget_state
		SET requests_used = GREATEST(0, requests_used - $2),
		    updated_at = NOW()
		WHERE budget_key = $1
		  AND window_started_at >= date_trunc('hour', NOW())`,
		strings.TrimSpace(budgetKey), requests)
	return err
}

func (s *Store) RecordBinaryEvidenceDiagnostic(ctx context.Context, item BinaryEvidenceDiagnostic) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO binary_evidence_exchange_diagnostics (
			pool_id, peer_node_id, direction, evidence_kind, request_count,
			hit_count, item_count, body_requests_avoided, response_bytes,
			conflicts, quarantines, latency_ms, error_text
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13)`,
		item.PoolID, item.PeerNodeID, item.Direction, item.EvidenceKind,
		item.RequestCount, item.HitCount, item.ItemCount, item.BodyRequestsAvoided,
		item.ResponseBytes, item.Conflicts, item.Quarantines, item.LatencyMS,
		item.ErrorText)
	return err
}

func (s *Store) ListBinaryEvidenceDiagnostics(ctx context.Context, poolID string, limit int) ([]BinaryEvidenceDiagnosticRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT diagnostic_id, pool_id, peer_node_id, direction, evidence_kind,
		       request_count, hit_count, item_count, body_requests_avoided,
		       response_bytes, conflicts, quarantines, latency_ms, error_text,
		       created_at
		FROM binary_evidence_exchange_diagnostics
		WHERE ($1 = '' OR pool_id = $1)
		ORDER BY created_at DESC, diagnostic_id DESC
		LIMIT $2`, strings.TrimSpace(poolID), limit)
	if err != nil {
		return nil, fmt.Errorf("list binary evidence diagnostics: %w", err)
	}
	defer rows.Close()
	out := make([]BinaryEvidenceDiagnosticRecord, 0)
	for rows.Next() {
		var item BinaryEvidenceDiagnosticRecord
		if err := rows.Scan(
			&item.DiagnosticID, &item.PoolID, &item.PeerNodeID,
			&item.Direction, &item.EvidenceKind, &item.RequestCount,
			&item.HitCount, &item.ItemCount, &item.BodyRequestsAvoided,
			&item.ResponseBytes, &item.Conflicts, &item.Quarantines,
			&item.LatencyMS, &item.ErrorText, &item.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) CompactBinaryEvidenceDiagnostics(ctx context.Context, before time.Time) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer rollbackTx(tx)
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM binary_evidence_exchange_diagnostics
		WHERE created_at < $1`, before.UTC()); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `
		DELETE FROM yenc_header_evidence evidence
		WHERE evidence.source_posted_at < $1
		  AND NOT EXISTS (
		    SELECT 1
		    FROM article_headers header
		    WHERE header.message_id = evidence.message_id
		  )`, before.UTC()); err != nil {
		return err
	}
	return tx.Commit()
}
