package pgindex

import (
	"context"
	"database/sql"
	"fmt"
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
