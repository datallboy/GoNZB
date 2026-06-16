package pgindex

import (
	"context"
	"database/sql"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type binaryRecoverySeed struct {
	ID                int64
	ProviderID        int64
	NewsgroupID       int64
	ReleaseFamilyKey  string
	ReleaseName       string
	BinaryName        string
	FileName          string
	FileIndex         int
	ExpectedFileCount int
	TotalBytes        int64
	IsAuxiliary       bool
	IsMainPayload     bool
}

var binaryRecoveryUnsafeRE = regexp.MustCompile(`[^A-Za-z0-9._ -]+`)

func (s *Store) ApplyBinaryRecovery(ctx context.Context, in BinaryRecoveryRecord) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("pgindex store is not initialized")
	}
	if in.BinaryID <= 0 {
		return fmt.Errorf("binary id is required")
	}

	in.Kind = strings.TrimSpace(strings.ToLower(in.Kind))
	in.Extension = normalizeRecoveredExtension(in.Extension)
	in.Source = strings.TrimSpace(strings.ToLower(in.Source))
	if in.Kind == "" || in.Extension == "" {
		return fmt.Errorf("binary recovery kind and extension are required")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin binary recovery tx: %w", err)
	}
	defer rollbackTx(tx)

	seed, err := loadBinaryRecoverySeed(ctx, tx, in.BinaryID)
	if err != nil {
		return err
	}

	newName := recoveredFileName(seed.FileName, seed.ReleaseName, seed.BinaryName, seed.FileIndex, seed.ExpectedFileCount, in.Kind, in.Extension)
	renamePredicate := `file_name = '' OR LOWER(file_name) LIKE '%.bin' OR file_name !~ '\.[A-Za-z0-9]{1,8}$'`
	recoveryRenamePredicate := `recovered_file_name = '' OR LOWER(recovered_file_name) LIKE '%.bin' OR recovered_file_name !~ '\.[A-Za-z0-9]{1,8}$'`
	if _, err := tx.ExecContext(ctx, `
		UPDATE binary_recovery_current
		SET recovered_kind = $2,
		    recovered_extension = $3,
		    recovered_source = $4,
		    recovered_confidence = GREATEST(recovered_confidence, $5),
		    recovered_file_name = CASE
		    	WHEN $6 AND (`+recoveryRenamePredicate+`) THEN $7
		    	ELSE recovered_file_name
		    END,
		    recovered_at = NOW(),
		    updated_at = NOW()
		WHERE binary_id = $1`,
		in.BinaryID,
		in.Kind,
		in.Extension,
		in.Source,
		in.Confidence,
		in.Canonicalize,
		newName,
	); err != nil {
		return fmt.Errorf("update binary recovery %d: %w", in.BinaryID, err)
	}
	if in.Canonicalize {
		if _, err := tx.ExecContext(ctx, `
			UPDATE binary_identity_current
			SET file_name = CASE
			    	WHEN `+renamePredicate+` THEN $2
			    	ELSE file_name
			    END,
			    updated_at = NOW()
			WHERE binary_id = $1`,
			in.BinaryID,
			newName,
		); err != nil {
			return fmt.Errorf("update binary recovery identity %d: %w", in.BinaryID, err)
		}
	}

	if in.Canonicalize {
		if _, err := tx.ExecContext(ctx, `
			UPDATE release_files
			SET file_name = $2,
			    is_pars = CASE WHEN $3 THEN TRUE ELSE is_pars END
			WHERE binary_id = $1
			  AND (`+renamePredicate+`)`,
			in.BinaryID,
			newName,
			in.Kind == "par2",
		); err != nil {
			return fmt.Errorf("update release file recovery %d: %w", in.BinaryID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE binary_parts
			SET file_name = $2
			WHERE binary_id = $1
			  AND (`+renamePredicate+`)`,
			in.BinaryID,
			newName,
		); err != nil {
			return fmt.Errorf("update binary part recovery %d: %w", in.BinaryID, err)
		}
	}
	if in.Kind == "par2" {
		if _, err := tx.ExecContext(ctx, `
			UPDATE release_files
			SET is_pars = TRUE
			WHERE binary_id = $1`, in.BinaryID); err != nil {
			return fmt.Errorf("mark release file par2 recovery %d: %w", in.BinaryID, err)
		}
		if _, err := tx.ExecContext(ctx, `
			UPDATE releases
			SET has_par2 = TRUE,
			    updated_at = NOW()
			WHERE release_id IN (
				SELECT release_id
				FROM release_files
				WHERE binary_id = $1
			)`, in.BinaryID); err != nil {
			return fmt.Errorf("mark release par2 recovery %d: %w", in.BinaryID, err)
		}
	}

	if in.Canonicalize && in.Kind == "archive" {
		siblings, err := loadBinaryRecoverySiblings(ctx, tx, seed)
		if err != nil {
			return err
		}
		if shouldApplyArchiveFamilyRecovery(seed, siblings) {
			familyCount := seed.ExpectedFileCount
			if familyCount <= 1 {
				familyCount = len(siblings)
			}
			for idx, sibling := range siblings {
				recoveredName := recoveredFileName(sibling.FileName, sibling.ReleaseName, sibling.BinaryName, idx+1, familyCount, in.Kind, in.Extension)
				if _, err := tx.ExecContext(ctx, `
					UPDATE binary_recovery_current
					SET recovered_kind = CASE
					    	WHEN recovered_kind = '' THEN $2
					    	ELSE recovered_kind
					    END,
					    recovered_extension = CASE
					    	WHEN recovered_extension = '' THEN $3
					    	ELSE recovered_extension
					    END,
					    recovered_source = CASE
					    	WHEN recovered_source = '' THEN $4
					    	ELSE recovered_source
					    END,
					    recovered_confidence = GREATEST(recovered_confidence, $5),
					    recovered_file_name = CASE
					    	WHEN `+recoveryRenamePredicate+` THEN $6
					    	ELSE recovered_file_name
					    END,
					    recovered_at = NOW(),
					    updated_at = NOW()
					WHERE binary_id = $1`,
					sibling.ID,
					"archive",
					in.Extension,
					"family_signature",
					in.Confidence*0.85,
					recoveredName,
				); err != nil {
					return fmt.Errorf("update sibling binary recovery %d: %w", sibling.ID, err)
				}
				if _, err := tx.ExecContext(ctx, `
					UPDATE binary_identity_current
					SET file_name = CASE
					    	WHEN `+renamePredicate+` THEN $2
					    	ELSE file_name
					    END,
					    updated_at = NOW()
					WHERE binary_id = $1`,
					sibling.ID,
					recoveredName,
				); err != nil {
					return fmt.Errorf("update sibling binary recovery identity %d: %w", sibling.ID, err)
				}
				if _, err := tx.ExecContext(ctx, `
					UPDATE release_files
					SET file_name = $2
					WHERE binary_id = $1
					  AND (`+renamePredicate+`)`,
					sibling.ID,
					recoveredName,
				); err != nil {
					return fmt.Errorf("update sibling release file recovery %d: %w", sibling.ID, err)
				}
				if _, err := tx.ExecContext(ctx, `
					UPDATE binary_parts
					SET file_name = $2
					WHERE binary_id = $1
					  AND (`+renamePredicate+`)`,
					sibling.ID,
					recoveredName,
				); err != nil {
					return fmt.Errorf("update sibling binary parts recovery %d: %w", sibling.ID, err)
				}
			}
		}
	}

	if err := markReleaseFamilyDirty(ctx, tx, seed.ProviderID, seed.NewsgroupID, "release_family", seed.ReleaseFamilyKey); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit binary recovery tx: %w", err)
	}
	return nil
}

func loadBinaryRecoverySeed(ctx context.Context, tx *sql.Tx, binaryID int64) (binaryRecoverySeed, error) {
	var row binaryRecoverySeed
	err := tx.QueryRowContext(ctx, `
		SELECT
			bc.binary_id,
			bc.provider_id,
			bc.newsgroup_id,
			bic.release_family_key,
			COALESCE(bic.release_name, ''),
			COALESCE(bic.binary_name, ''),
			COALESCE(bic.file_name, ''),
			COALESCE(bic.file_index, 0),
			COALESCE(bic.expected_file_count, 0),
			COALESCE(bos.total_bytes, 0),
			bic.is_auxiliary,
			bic.is_main_payload
		FROM binary_core bc
		JOIN binary_identity_current bic ON bic.binary_id = bc.binary_id
		JOIN binary_observation_stats bos ON bos.binary_id = bc.binary_id
		WHERE bc.binary_id = $1`,
		binaryID,
	).Scan(
		&row.ID,
		&row.ProviderID,
		&row.NewsgroupID,
		&row.ReleaseFamilyKey,
		&row.ReleaseName,
		&row.BinaryName,
		&row.FileName,
		&row.FileIndex,
		&row.ExpectedFileCount,
		&row.TotalBytes,
		&row.IsAuxiliary,
		&row.IsMainPayload,
	)
	if err == sql.ErrNoRows {
		return binaryRecoverySeed{}, fmt.Errorf("binary %d not found for recovery", binaryID)
	}
	if err != nil {
		return binaryRecoverySeed{}, fmt.Errorf("load binary recovery seed %d: %w", binaryID, err)
	}
	return row, nil
}

func loadBinaryRecoverySiblings(ctx context.Context, tx *sql.Tx, seed binaryRecoverySeed) ([]binaryRecoverySeed, error) {
	rows, err := tx.QueryContext(ctx, `
		SELECT
			bc.binary_id,
			bc.provider_id,
			bc.newsgroup_id,
			bic.release_family_key,
			COALESCE(bic.release_name, ''),
			COALESCE(bic.binary_name, ''),
			COALESCE(bic.file_name, ''),
			COALESCE(bic.file_index, 0),
			COALESCE(bic.expected_file_count, 0),
			COALESCE(bos.total_bytes, 0),
			bic.is_auxiliary,
			bic.is_main_payload
		FROM binary_core bc
		JOIN binary_identity_current bic ON bic.binary_id = bc.binary_id
		JOIN binary_observation_stats bos ON bos.binary_id = bc.binary_id
		WHERE bc.provider_id = $1
		  AND bc.newsgroup_id = $2
		  AND bic.release_family_key = $3
		  AND (bic.is_main_payload = TRUE OR bic.is_auxiliary = FALSE)
		ORDER BY CASE WHEN bic.file_index > 0 THEN bic.file_index ELSE 2147483647 END, bc.binary_id`,
		seed.ProviderID,
		seed.NewsgroupID,
		seed.ReleaseFamilyKey,
	)
	if err != nil {
		return nil, fmt.Errorf("load binary recovery siblings %d/%d %q: %w", seed.ProviderID, seed.NewsgroupID, seed.ReleaseFamilyKey, err)
	}
	defer rows.Close()

	out := make([]binaryRecoverySeed, 0, seed.ExpectedFileCount)
	for rows.Next() {
		var row binaryRecoverySeed
		if err := rows.Scan(
			&row.ID,
			&row.ProviderID,
			&row.NewsgroupID,
			&row.ReleaseFamilyKey,
			&row.ReleaseName,
			&row.BinaryName,
			&row.FileName,
			&row.FileIndex,
			&row.ExpectedFileCount,
			&row.TotalBytes,
			&row.IsAuxiliary,
			&row.IsMainPayload,
		); err != nil {
			return nil, fmt.Errorf("scan binary recovery sibling: %w", err)
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate binary recovery siblings: %w", err)
	}
	return out, nil
}

func normalizeRecoveredExtension(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	return ext
}

func recoveredFileName(currentName, releaseName, binaryName string, ordinal, expected int, kind, ext string) string {
	stem := recoveryFileStem(currentName, releaseName, binaryName)
	if stem == "" {
		stem = "recovered-binary"
	}
	switch {
	case kind == "archive" && expected > 1 && ext == ".7z":
		return fmt.Sprintf("%s.7z.%03d", stem, normalizedOrdinal(ordinal))
	case kind == "archive" && expected > 1 && ext == ".zip":
		return fmt.Sprintf("%s.zip.%03d", stem, normalizedOrdinal(ordinal))
	case kind == "archive" && expected > 1 && ext == ".rar":
		return fmt.Sprintf("%s.part%02d.rar", stem, normalizedOrdinal(ordinal))
	default:
		return stem + ext
	}
}

func recoveryFileStem(currentName, releaseName, binaryName string) string {
	for _, candidate := range []string{releaseName, binaryName, strings.TrimSuffix(currentName, filepath.Ext(currentName))} {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		candidate = binaryRecoveryUnsafeRE.ReplaceAllString(candidate, ".")
		candidate = strings.Trim(candidate, ". ")
		candidate = strings.ReplaceAll(candidate, " ", ".")
		candidate = strings.Trim(candidate, ".")
		if candidate != "" {
			return candidate
		}
	}
	return ""
}

func normalizedOrdinal(v int) int {
	if v <= 0 {
		return 1
	}
	return v
}

func shouldApplyArchiveFamilyRecovery(seed binaryRecoverySeed, siblings []binaryRecoverySeed) bool {
	if len(siblings) <= 1 {
		return false
	}
	if seed.ExpectedFileCount > 1 {
		return true
	}

	opaque := 0
	sizes := make([]int64, 0, len(siblings))
	for _, sibling := range siblings {
		if isOpaqueRecoveryName(sibling.FileName) {
			opaque++
		}
		if sibling.TotalBytes > 0 {
			sizes = append(sizes, sibling.TotalBytes)
		}
	}
	if opaque < 3 || len(sizes) < 3 {
		return false
	}

	sort.Slice(sizes, func(i, j int) bool { return sizes[i] < sizes[j] })
	median := float64(sizes[len(sizes)/2])
	if median <= 0 {
		return false
	}

	tolerance := math.Max(256*1024, median*0.20)
	coherent := 0
	for _, size := range sizes {
		if math.Abs(float64(size)-median) <= tolerance {
			coherent++
		}
	}
	required := int(math.Ceil(float64(len(sizes)) * 0.75))
	if required < 3 {
		required = 3
	}
	return coherent >= required
}

func isOpaqueRecoveryName(name string) bool {
	lower := strings.ToLower(strings.TrimSpace(name))
	return lower == "" || strings.HasSuffix(lower, ".bin") || filepath.Ext(lower) == ""
}
