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

func lockBinaryIdentityKeys(ctx context.Context, tx *sql.Tx, locks []binaryIdentityLock) error {
	if tx == nil {
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
	for _, lock := range deduped {
		if err := lockBinaryIdentityKey(ctx, tx, lock.ProviderID, lock.NewsgroupID, lock.BinaryKey); err != nil {
			return err
		}
	}
	return nil
}
