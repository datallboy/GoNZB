package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

var criticalIndexerIndexNames = []string{
	"public.article_headers_pkey",
	"public.article_headers_newsgroup_id_article_number_key",
	"public.article_headers_newsgroup_id_message_id_key",
}

var criticalIndexerReindexNames = []string{
	"public.article_headers_newsgroup_id_article_number_key",
	"public.article_headers_newsgroup_id_message_id_key",
}

type IndexerIntegrityCheck struct {
	Relation     string `json:"relation"`
	AccessMethod string `json:"access_method"`
	MetadataOK   bool   `json:"metadata_ok"`
	AmcheckRan   bool   `json:"amcheck_ran"`
	OK           bool   `json:"ok"`
	Detail       string `json:"detail"`
}

type IndexerIntegrityReport struct {
	AmcheckAvailable bool                    `json:"amcheck_available"`
	Checks           []IndexerIntegrityCheck `json:"checks"`
}

func (r *IndexerIntegrityReport) HasFailures() bool {
	if r == nil {
		return false
	}
	for _, check := range r.Checks {
		if !check.OK {
			return true
		}
	}
	return false
}

func (r *IndexerIntegrityReport) FailureSummary() string {
	if r == nil {
		return ""
	}
	failures := make([]string, 0, len(r.Checks))
	for _, check := range r.Checks {
		if check.OK {
			continue
		}
		failures = append(failures, fmt.Sprintf("%s: %s", check.Relation, check.Detail))
	}
	return strings.Join(failures, "; ")
}

type IndexerIntegrityRepairResult struct {
	Reindexed []string `json:"reindexed"`
}

func (s *Store) CheckCriticalIndexerIntegrity(ctx context.Context, ensureExtension bool) (*IndexerIntegrityReport, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}

	available, err := s.ensureAMCheckExtension(ctx, ensureExtension)
	if err != nil {
		return nil, err
	}

	report := &IndexerIntegrityReport{
		AmcheckAvailable: available,
		Checks:           make([]IndexerIntegrityCheck, 0, len(criticalIndexerIndexNames)),
	}
	for _, relation := range criticalIndexerIndexNames {
		check, err := s.checkCriticalIndexerRelation(ctx, relation, available)
		if err != nil {
			return nil, err
		}
		report.Checks = append(report.Checks, check)
	}
	return report, nil
}

func (s *Store) ReindexCriticalIndexerIndexes(ctx context.Context) (*IndexerIntegrityRepairResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}

	out := &IndexerIntegrityRepairResult{Reindexed: make([]string, 0, len(criticalIndexerReindexNames))}
	for _, relation := range criticalIndexerReindexNames {
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("REINDEX INDEX %s", relation)); err != nil {
			return nil, fmt.Errorf("reindex critical index %s: %w", relation, err)
		}
		out.Reindexed = append(out.Reindexed, relation)
	}
	return out, nil
}

func (s *Store) ensureAMCheckExtension(ctx context.Context, ensure bool) (bool, error) {
	var exists bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM pg_extension WHERE extname = 'amcheck')`).Scan(&exists); err != nil {
		return false, fmt.Errorf("check amcheck extension: %w", err)
	}
	if exists || !ensure {
		return exists, nil
	}
	if _, err := s.db.ExecContext(ctx, `CREATE EXTENSION IF NOT EXISTS amcheck`); err != nil {
		return false, fmt.Errorf("create amcheck extension: %w", err)
	}
	return true, nil
}

func (s *Store) checkCriticalIndexerRelation(ctx context.Context, relation string, amcheckAvailable bool) (IndexerIntegrityCheck, error) {
	check := IndexerIntegrityCheck{
		Relation: relation,
	}

	var (
		accessMethod sql.NullString
		indisvalid   sql.NullBool
		indisready   sql.NullBool
	)
	if err := s.db.QueryRowContext(ctx, `
		SELECT
			am.amname,
			COALESCE(i.indisvalid, FALSE),
			COALESCE(i.indisready, FALSE)
		FROM pg_class c
		LEFT JOIN pg_index i ON i.indexrelid = c.oid
		LEFT JOIN pg_am am ON am.oid = c.relam
		WHERE c.oid = $1::regclass`,
		relation,
	).Scan(&accessMethod, &indisvalid, &indisready); err != nil {
		return check, fmt.Errorf("inspect critical index metadata %s: %w", relation, err)
	}

	check.AccessMethod = accessMethod.String
	check.MetadataOK = indisvalid.Bool && indisready.Bool
	if !check.MetadataOK {
		check.OK = false
		check.Detail = "index metadata is not valid/ready"
		return check, nil
	}

	if !amcheckAvailable {
		check.OK = true
		check.Detail = "metadata-only check passed; amcheck extension unavailable"
		return check, nil
	}

	if _, err := s.db.ExecContext(ctx, `SELECT bt_index_check($1::regclass, FALSE)`, relation); err != nil {
		check.AmcheckRan = true
		check.OK = false
		check.Detail = err.Error()
		return check, nil
	}

	check.AmcheckRan = true
	check.OK = true
	check.Detail = "amcheck passed"
	return check, nil
}
