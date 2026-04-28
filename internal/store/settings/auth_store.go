package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/datallboy/gonzb/internal/auth"
)

func (s *Store) EnsureAuthDefaults(ctx context.Context, roles []auth.Role) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	for _, role := range roles {
		permsJSON, err := json.Marshal(role.Permissions)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO auth_roles (id, name, builtin, permissions_json, created_at, updated_at)
			VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
			ON CONFLICT(id) DO UPDATE SET
				name = excluded.name,
				builtin = excluded.builtin,
				permissions_json = excluded.permissions_json,
				updated_at = CURRENT_TIMESTAMP`,
			role.ID, role.Name, role.Builtin, string(permsJSON),
		); err != nil {
			return err
		}
	}

	return tx.Commit()
}

func (s *Store) CountAuthUsers(ctx context.Context) (int, error) {
	var count int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_users`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (s *Store) CreateInitialAuthUser(ctx context.Context, user auth.StoredUser, roleIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	var count int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM auth_users`).Scan(&count); err != nil {
		return err
	}
	if count > 0 {
		return auth.ErrSetupCompleted
	}
	if _, err := tx.ExecContext(ctx, `
		INSERT INTO auth_users (id, username, password_hash, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.PasswordHash, user.Enabled, user.CreatedAt.UTC(), user.UpdatedAt.UTC(),
	); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO auth_user_roles (user_id, role_id)
			VALUES (?, ?)`, user.ID, roleID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetAuthUserByUsername(ctx context.Context, username string) (*auth.StoredUser, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, enabled, created_at, updated_at
		FROM auth_users
		WHERE username = ?`, username)
	return scanStoredUser(row)
}

func (s *Store) GetAuthUserByID(ctx context.Context, userID string) (*auth.StoredUser, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, enabled, created_at, updated_at
		FROM auth_users
		WHERE id = ?`, userID)
	return scanStoredUser(row)
}

func (s *Store) ListAuthUsers(ctx context.Context) ([]auth.StoredUser, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, password_hash, enabled, created_at, updated_at
		FROM auth_users
		ORDER BY username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []auth.StoredUser{}
	for rows.Next() {
		item, err := scanStoredUser(rows)
		if err != nil {
			return nil, err
		}
		if item != nil {
			roleIDs, err := s.ListAuthUserRoleIDs(ctx, item.ID)
			if err != nil {
				return nil, err
			}
			item.RoleIDs = roleIDs
			out = append(out, *item)
		}
	}
	return out, rows.Err()
}

func (s *Store) UpsertAuthUser(ctx context.Context, user auth.StoredUser) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_users (id, username, password_hash, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			username = excluded.username,
			password_hash = excluded.password_hash,
			enabled = excluded.enabled,
			updated_at = excluded.updated_at`,
		user.ID, user.Username, user.PasswordHash, user.Enabled, user.CreatedAt.UTC(), user.UpdatedAt.UTC(),
	)
	return err
}

func (s *Store) DeleteAuthUser(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_users WHERE id = ?`, userID)
	return err
}

func (s *Store) ListAuthRoles(ctx context.Context) ([]auth.Role, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, name, builtin, permissions_json, created_at, updated_at
		FROM auth_roles
		ORDER BY builtin DESC, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []auth.Role{}
	for rows.Next() {
		role, err := scanRole(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, role)
	}
	return out, rows.Err()
}

func (s *Store) UpsertAuthRole(ctx context.Context, role auth.Role) error {
	permsJSON, err := json.Marshal(role.Permissions)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO auth_roles (id, name, builtin, permissions_json, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			builtin = excluded.builtin,
			permissions_json = excluded.permissions_json,
			updated_at = excluded.updated_at`,
		role.ID, role.Name, role.Builtin, string(permsJSON), role.CreatedAt.UTC(), role.UpdatedAt.UTC(),
	)
	return err
}

func (s *Store) DeleteAuthRole(ctx context.Context, roleID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_roles WHERE id = ? AND builtin = 0`, roleID)
	return err
}

func (s *Store) ReplaceAuthUserRoles(ctx context.Context, userID string, roleIDs []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM auth_user_roles WHERE user_id = ?`, userID); err != nil {
		return err
	}
	for _, roleID := range roleIDs {
		if _, err := tx.ExecContext(ctx, `
			INSERT OR IGNORE INTO auth_user_roles (user_id, role_id)
			VALUES (?, ?)`, userID, roleID,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListAuthUserRoleIDs(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT role_id
		FROM auth_user_roles
		WHERE user_id = ?
		ORDER BY role_id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var roleID string
		if err := rows.Scan(&roleID); err != nil {
			return nil, err
		}
		out = append(out, roleID)
	}
	return out, rows.Err()
}

func (s *Store) CreateAuthSession(ctx context.Context, session auth.Session) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_sessions (id, user_id, expires_at, created_at, last_seen_at)
		VALUES (?, ?, ?, ?, ?)`,
		session.ID, session.UserID, session.ExpiresAt.UTC(), session.CreatedAt.UTC(), session.LastSeenAt.UTC(),
	)
	return err
}

func (s *Store) GetAuthSession(ctx context.Context, sessionID string) (*auth.Session, error) {
	var item auth.Session
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, expires_at, created_at, last_seen_at
		FROM auth_sessions
		WHERE id = ?`, sessionID,
	).Scan(&item.ID, &item.UserID, &item.ExpiresAt, &item.CreatedAt, &item.LastSeenAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) TouchAuthSession(ctx context.Context, sessionID string, seenAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE auth_sessions
		SET last_seen_at = ?
		WHERE id = ?`, seenAt.UTC(), sessionID,
	)
	return err
}

func (s *Store) DeleteAuthSession(ctx context.Context, sessionID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM auth_sessions WHERE id = ?`, sessionID)
	return err
}

func (s *Store) CreateAuthToken(ctx context.Context, token auth.StoredToken) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO auth_api_tokens (id, user_id, name, prefix, token_hash, created_at, last_used_at, revoked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		token.ID, token.UserID, token.Name, token.Prefix, token.TokenHash, token.CreatedAt.UTC(), nullableTime(token.LastUsedAt), nullableTime(token.RevokedAt),
	)
	return err
}

func (s *Store) ListAuthTokens(ctx context.Context) ([]auth.Token, error) {
	return s.listAuthTokensWhere(ctx, `
		SELECT id, user_id, name, prefix, created_at, last_used_at, revoked_at
		FROM auth_api_tokens
		ORDER BY created_at DESC`)
}

func (s *Store) ListAuthTokensByUserID(ctx context.Context, userID string) ([]auth.Token, error) {
	return s.listAuthTokensWhere(ctx, `
		SELECT id, user_id, name, prefix, created_at, last_used_at, revoked_at
		FROM auth_api_tokens
		WHERE user_id = ?
		ORDER BY created_at DESC`, userID)
}

func (s *Store) listAuthTokensWhere(ctx context.Context, query string, args ...any) ([]auth.Token, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []auth.Token
	for rows.Next() {
		item, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (s *Store) GetAuthTokenByID(ctx context.Context, tokenID string) (*auth.StoredToken, error) {
	var (
		item              auth.StoredToken
		lastUsed, revoked sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, prefix, token_hash, created_at, last_used_at, revoked_at
		FROM auth_api_tokens
		WHERE id = ?`, tokenID,
	).Scan(&item.ID, &item.UserID, &item.Name, &item.Prefix, &item.TokenHash, &item.CreatedAt, &lastUsed, &revoked)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastUsed.Valid {
		t := lastUsed.Time.UTC()
		item.LastUsedAt = &t
	}
	if revoked.Valid {
		t := revoked.Time.UTC()
		item.RevokedAt = &t
	}
	return &item, nil
}

func (s *Store) GetAuthTokenByHash(ctx context.Context, tokenHash string) (*auth.StoredToken, error) {
	var (
		item              auth.StoredToken
		lastUsed, revoked sql.NullTime
	)
	err := s.db.QueryRowContext(ctx, `
		SELECT id, user_id, name, prefix, token_hash, created_at, last_used_at, revoked_at
		FROM auth_api_tokens
		WHERE token_hash = ?`, tokenHash,
	).Scan(&item.ID, &item.UserID, &item.Name, &item.Prefix, &item.TokenHash, &item.CreatedAt, &lastUsed, &revoked)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if lastUsed.Valid {
		t := lastUsed.Time.UTC()
		item.LastUsedAt = &t
	}
	if revoked.Valid {
		t := revoked.Time.UTC()
		item.RevokedAt = &t
	}
	return &item, nil
}

func (s *Store) TouchAuthToken(ctx context.Context, tokenID string, seenAt time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE auth_api_tokens
		SET last_used_at = ?
		WHERE id = ?`, seenAt.UTC(), tokenID,
	)
	return err
}

func (s *Store) RevokeAuthToken(ctx context.Context, tokenID string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE auth_api_tokens
		SET revoked_at = CURRENT_TIMESTAMP
		WHERE id = ?`, tokenID,
	)
	return err
}

func scanStoredUser(scanner interface{ Scan(dest ...any) error }) (*auth.StoredUser, error) {
	var item auth.StoredUser
	err := scanner.Scan(&item.ID, &item.Username, &item.PasswordHash, &item.Enabled, &item.CreatedAt, &item.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func scanRole(scanner interface{ Scan(dest ...any) error }) (auth.Role, error) {
	var (
		item      auth.Role
		permsJSON string
	)
	if err := scanner.Scan(&item.ID, &item.Name, &item.Builtin, &permsJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
		return auth.Role{}, err
	}
	if permsJSON == "" {
		permsJSON = "[]"
	}
	if err := json.Unmarshal([]byte(permsJSON), &item.Permissions); err != nil {
		return auth.Role{}, fmt.Errorf("decode role permissions: %w", err)
	}
	return item, nil
}

func scanToken(scanner interface{ Scan(dest ...any) error }) (auth.Token, error) {
	var (
		item              auth.Token
		lastUsed, revoked sql.NullTime
	)
	if err := scanner.Scan(&item.ID, &item.UserID, &item.Name, &item.Prefix, &item.CreatedAt, &lastUsed, &revoked); err != nil {
		return auth.Token{}, err
	}
	if lastUsed.Valid {
		t := lastUsed.Time.UTC()
		item.LastUsedAt = &t
	}
	if revoked.Valid {
		t := revoked.Time.UTC()
		item.RevokedAt = &t
	}
	return item, nil
}

func nullableTime(in *time.Time) any {
	if in == nil {
		return nil
	}
	return in.UTC()
}
