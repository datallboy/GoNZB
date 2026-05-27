package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"strings"
)

func lockBinaryIdentityKey(ctx context.Context, tx *sql.Tx, providerID, newsgroupID int64, binaryKey string) error {
	if tx == nil {
		return fmt.Errorf("binary identity lock tx is required")
	}
	binaryKey = strings.TrimSpace(binaryKey)
	if providerID <= 0 || newsgroupID <= 0 || binaryKey == "" {
		return nil
	}
	lockKey := fmt.Sprintf("gonzb-binary-key:%d:%d:%s", providerID, newsgroupID, binaryKey)
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, lockKey); err != nil {
		return fmt.Errorf("lock binary identity key %q: %w", binaryKey, err)
	}
	return nil
}

type binaryIdentityLock struct {
	ProviderID  int64
	NewsgroupID int64
	BinaryKey   string
}

func lockBinaryIdentityKeys(ctx context.Context, runner sqlExecQueryer, locks []binaryIdentityLock) error {
	if runner == nil {
		return fmt.Errorf("binary identity lock tx is required")
	}
	if len(locks) == 0 {
		return nil
	}

	deduped := make([]binaryIdentityLock, 0, len(locks))
	seen := make(map[string]struct{}, len(locks))
	for _, lock := range locks {
		lock.BinaryKey = strings.TrimSpace(lock.BinaryKey)
		if lock.ProviderID <= 0 || lock.NewsgroupID <= 0 || lock.BinaryKey == "" {
			continue
		}
		compoundKey := fmt.Sprintf("%d:%d:%s", lock.ProviderID, lock.NewsgroupID, lock.BinaryKey)
		if _, ok := seen[compoundKey]; ok {
			continue
		}
		seen[compoundKey] = struct{}{}
		deduped = append(deduped, lock)
	}
	sort.Slice(deduped, func(i, j int) bool {
		if deduped[i].ProviderID != deduped[j].ProviderID {
			return deduped[i].ProviderID < deduped[j].ProviderID
		}
		if deduped[i].NewsgroupID != deduped[j].NewsgroupID {
			return deduped[i].NewsgroupID < deduped[j].NewsgroupID
		}
		return deduped[i].BinaryKey < deduped[j].BinaryKey
	})
	values := make([]string, 0, len(deduped))
	args := make([]any, 0, len(deduped)*3)
	for i, lock := range deduped {
		base := (i * 3) + 1
		values = append(values, fmt.Sprintf("($%d::bigint,$%d::bigint,$%d::text)", base, base+1, base+2))
		args = append(args, lock.ProviderID, lock.NewsgroupID, lock.BinaryKey)
	}
	rows, err := runner.QueryContext(ctx, fmt.Sprintf(`
		WITH requested(provider_id, newsgroup_id, binary_key) AS (
			VALUES %s
		)
		SELECT pg_advisory_xact_lock(hashtext('gonzb-binary-key:' || provider_id::text || ':' || newsgroup_id::text || ':' || binary_key))
		FROM requested
		ORDER BY provider_id, newsgroup_id, binary_key`, strings.Join(values, ",")), args...)
	if err != nil {
		return fmt.Errorf("lock binary identity keys batch size=%d: %w", len(deduped), err)
	}
	defer rows.Close()
	for rows.Next() {
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate binary identity lock batch size=%d: %w", len(deduped), err)
	}
	return nil
}
