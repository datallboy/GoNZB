package pgindex

import (
	"context"
	"fmt"
	"slices"
	"strings"
)

var indexerStorageReclaimTableOrder = []string{
	"release_family_readiness_summaries",
	"binary_grouping_evidence",
	"article_header_ingest_payloads",
}

var indexerStorageReclaimTableAliases = map[string]string{
	"readiness":                          "release_family_readiness_summaries",
	"readiness-summaries":                "release_family_readiness_summaries",
	"release_family_readiness_summaries": "release_family_readiness_summaries",
	"grouping-evidence":                  "binary_grouping_evidence",
	"binary_grouping_evidence":           "binary_grouping_evidence",
	"payloads":                           "article_header_ingest_payloads",
	"article-header-ingest-payloads":     "article_header_ingest_payloads",
	"article_header_ingest_payloads":     "article_header_ingest_payloads",
}

type IndexerStorageReclaimOptions struct {
	Tables []string
	Full   bool
}

type IndexerStorageReclaimTableResult struct {
	Table       string
	BeforeBytes int64
	AfterBytes  int64
}

type IndexerStorageReclaimResult struct {
	Mode   string
	Tables []IndexerStorageReclaimTableResult
}

func normalizeIndexerStorageReclaimTables(values []string) ([]string, error) {
	if len(values) == 0 {
		return append([]string(nil), indexerStorageReclaimTableOrder...), nil
	}

	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.ToLower(strings.TrimSpace(value))
		table, ok := indexerStorageReclaimTableAliases[key]
		if !ok {
			return nil, fmt.Errorf("unsupported reclaim table %q", value)
		}
		if _, exists := seen[table]; exists {
			continue
		}
		seen[table] = struct{}{}
		normalized = append(normalized, table)
	}

	slices.SortStableFunc(normalized, func(left, right string) int {
		return slices.Index(indexerStorageReclaimTableOrder, left) - slices.Index(indexerStorageReclaimTableOrder, right)
	})
	return normalized, nil
}

func indexerStorageReclaimStatement(table string, full bool) (string, error) {
	if !slices.Contains(indexerStorageReclaimTableOrder, table) {
		return "", fmt.Errorf("unsupported reclaim table %q", table)
	}
	if full {
		return fmt.Sprintf("VACUUM (FULL, ANALYZE) %s", table), nil
	}
	return fmt.Sprintf("VACUUM (ANALYZE) %s", table), nil
}

func (s *Store) RunIndexerStorageReclaim(ctx context.Context, options IndexerStorageReclaimOptions) (*IndexerStorageReclaimResult, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("pgindex store is not initialized")
	}

	tables, err := normalizeIndexerStorageReclaimTables(options.Tables)
	if err != nil {
		return nil, err
	}

	mode := "analyze"
	if options.Full {
		mode = "full"
	}

	conn, err := s.db.Conn(ctx)
	if err != nil {
		return nil, fmt.Errorf("open reclaim connection: %w", err)
	}
	defer conn.Close()

	result := &IndexerStorageReclaimResult{
		Mode:   mode,
		Tables: make([]IndexerStorageReclaimTableResult, 0, len(tables)),
	}

	for _, table := range tables {
		beforeBytes, err := s.tableTotalBytes(ctx, table)
		if err != nil {
			return nil, err
		}

		statement, err := indexerStorageReclaimStatement(table, options.Full)
		if err != nil {
			return nil, err
		}
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return nil, fmt.Errorf("run %s for %s: %w", mode, table, err)
		}

		afterBytes, err := s.tableTotalBytes(ctx, table)
		if err != nil {
			return nil, err
		}

		result.Tables = append(result.Tables, IndexerStorageReclaimTableResult{
			Table:       table,
			BeforeBytes: beforeBytes,
			AfterBytes:  afterBytes,
		})
	}

	return result, nil
}
