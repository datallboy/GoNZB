package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/segmentio/ksuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrUnauthorized       = errors.New("unauthorized")
	ErrForbidden          = errors.New("forbidden")
)

type Store interface {
	EnsureAuthDefaults(ctx context.Context, roles []Role, adminUsername, adminPasswordHash string) error
	GetAuthUserByUsername(ctx context.Context, username string) (*StoredUser, error)
	GetAuthUserByID(ctx context.Context, userID string) (*StoredUser, error)
	ListAuthUsers(ctx context.Context) ([]StoredUser, error)
	UpsertAuthUser(ctx context.Context, user StoredUser) error
	DeleteAuthUser(ctx context.Context, userID string) error
	ListAuthRoles(ctx context.Context) ([]Role, error)
	UpsertAuthRole(ctx context.Context, role Role) error
	DeleteAuthRole(ctx context.Context, roleID string) error
	ReplaceAuthUserRoles(ctx context.Context, userID string, roleIDs []string) error
	ListAuthUserRoleIDs(ctx context.Context, userID string) ([]string, error)
	CreateAuthSession(ctx context.Context, session Session) error
	GetAuthSession(ctx context.Context, sessionID string) (*Session, error)
	TouchAuthSession(ctx context.Context, sessionID string, seenAt time.Time) error
	DeleteAuthSession(ctx context.Context, sessionID string) error
	CreateAuthToken(ctx context.Context, token StoredToken) error
	ListAuthTokens(ctx context.Context) ([]Token, error)
	GetAuthTokenByHash(ctx context.Context, tokenHash string) (*StoredToken, error)
	TouchAuthToken(ctx context.Context, tokenID string, seenAt time.Time) error
	RevokeAuthToken(ctx context.Context, tokenID string) error
}

type StoredUser struct {
	User
	PasswordHash string
}

type Session struct {
	ID         string
	UserID     string
	ExpiresAt  time.Time
	CreatedAt  time.Time
	LastSeenAt time.Time
}

type StoredToken struct {
	Token
	TokenHash string
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{
		store: store,
		now:   time.Now,
	}
}

func (s *Service) Bootstrap(ctx context.Context) error {
	if s == nil || s.store == nil {
		return ErrUnauthorized
	}
	hash, err := bcrypt.GenerateFromPassword([]byte("admin"), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("hash default admin password: %w", err)
	}
	return s.store.EnsureAuthDefaults(ctx, DefaultRoles(), "admin", string(hash))
}

func (s *Service) AuthenticatePassword(ctx context.Context, username, password string) (*Session, *Principal, error) {
	user, err := s.store.GetAuthUserByUsername(ctx, strings.TrimSpace(username))
	if err != nil || user == nil || !user.Enabled {
		return nil, nil, ErrInvalidCredentials
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)) != nil {
		return nil, nil, ErrInvalidCredentials
	}
	session := &Session{
		ID:         ksuid.New().String(),
		UserID:     user.ID,
		CreatedAt:  s.now().UTC(),
		LastSeenAt: s.now().UTC(),
		ExpiresAt:  s.now().UTC().Add(7 * 24 * time.Hour),
	}
	if err := s.store.CreateAuthSession(ctx, *session); err != nil {
		return nil, nil, err
	}
	principal, err := s.principalForUser(ctx, user)
	if err != nil {
		return nil, nil, err
	}
	return session, principal, nil
}

func (s *Service) AuthenticateSession(ctx context.Context, sessionID string) (*Principal, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, ErrUnauthorized
	}
	session, err := s.store.GetAuthSession(ctx, sessionID)
	if err != nil || session == nil {
		return nil, ErrUnauthorized
	}
	if session.ExpiresAt.Before(s.now().UTC()) {
		_ = s.store.DeleteAuthSession(ctx, sessionID)
		return nil, ErrUnauthorized
	}
	_ = s.store.TouchAuthSession(ctx, sessionID, s.now().UTC())
	user, err := s.store.GetAuthUserByID(ctx, session.UserID)
	if err != nil || user == nil || !user.Enabled {
		return nil, ErrUnauthorized
	}
	return s.principalForUser(ctx, user)
}

func (s *Service) AuthenticateToken(ctx context.Context, rawToken string) (*Principal, error) {
	rawToken = strings.TrimSpace(rawToken)
	if rawToken == "" {
		return nil, ErrUnauthorized
	}
	hash := hashToken(rawToken)
	token, err := s.store.GetAuthTokenByHash(ctx, hash)
	if err != nil || token == nil || token.RevokedAt != nil {
		return nil, ErrUnauthorized
	}
	_ = s.store.TouchAuthToken(ctx, token.ID, s.now().UTC())
	user, err := s.store.GetAuthUserByID(ctx, token.UserID)
	if err != nil || user == nil || !user.Enabled {
		return nil, ErrUnauthorized
	}
	return s.principalForUser(ctx, user)
}

func (s *Service) LogoutSession(ctx context.Context, sessionID string) error {
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	return s.store.DeleteAuthSession(ctx, sessionID)
}

func (s *Service) ListUsers(ctx context.Context) ([]StoredUser, error) {
	return s.store.ListAuthUsers(ctx)
}

func (s *Service) UpsertUser(ctx context.Context, user StoredUser, password string, roleIDs []string) (*StoredUser, error) {
	if strings.TrimSpace(user.ID) == "" {
		user.ID = ksuid.New().String()
	}
	now := s.now().UTC()
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now
	if strings.TrimSpace(password) != "" {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		user.PasswordHash = string(hash)
	} else if existing, _ := s.store.GetAuthUserByID(ctx, user.ID); existing != nil {
		user.PasswordHash = existing.PasswordHash
	}
	if user.PasswordHash == "" {
		return nil, fmt.Errorf("password is required")
	}
	if err := s.store.UpsertAuthUser(ctx, user); err != nil {
		return nil, err
	}
	if err := s.store.ReplaceAuthUserRoles(ctx, user.ID, roleIDs); err != nil {
		return nil, err
	}
	out, err := s.store.GetAuthUserByID(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Service) DeleteUser(ctx context.Context, userID string) error {
	return s.store.DeleteAuthUser(ctx, userID)
}

func (s *Service) ListRoles(ctx context.Context) ([]Role, error) {
	return s.store.ListAuthRoles(ctx)
}

func (s *Service) UpsertRole(ctx context.Context, role Role) (*Role, error) {
	if strings.TrimSpace(role.ID) == "" {
		role.ID = ksuid.New().String()
	}
	now := s.now().UTC()
	if role.CreatedAt.IsZero() {
		role.CreatedAt = now
	}
	role.UpdatedAt = now
	if err := s.store.UpsertAuthRole(ctx, role); err != nil {
		return nil, err
	}
	roles, err := s.store.ListAuthRoles(ctx)
	if err != nil {
		return nil, err
	}
	for _, item := range roles {
		if item.ID == role.ID {
			roleCopy := item
			return &roleCopy, nil
		}
	}
	return &role, nil
}

func (s *Service) DeleteRole(ctx context.Context, roleID string) error {
	return s.store.DeleteAuthRole(ctx, roleID)
}

func (s *Service) CreateToken(ctx context.Context, userID, name string) (*Token, string, error) {
	raw := ksuid.New().String() + ksuid.New().String()
	prefix := raw
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	token := StoredToken{
		Token: Token{
			ID:        ksuid.New().String(),
			UserID:    userID,
			Name:      strings.TrimSpace(name),
			Prefix:    prefix,
			CreatedAt: s.now().UTC(),
		},
		TokenHash: hashToken(raw),
	}
	if err := s.store.CreateAuthToken(ctx, token); err != nil {
		return nil, "", err
	}
	out := token.Token
	return &out, raw, nil
}

func (s *Service) ListTokens(ctx context.Context) ([]Token, error) {
	return s.store.ListAuthTokens(ctx)
}

func (s *Service) RevokeToken(ctx context.Context, tokenID string) error {
	return s.store.RevokeAuthToken(ctx, tokenID)
}

func (s *Service) principalForUser(ctx context.Context, user *StoredUser) (*Principal, error) {
	roleIDs, err := s.store.ListAuthUserRoleIDs(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	roles, err := s.store.ListAuthRoles(ctx)
	if err != nil {
		return nil, err
	}
	roleSet := make(map[string]struct{}, len(roleIDs))
	for _, id := range roleIDs {
		roleSet[id] = struct{}{}
	}
	perms := make(map[string]struct{})
	for _, role := range roles {
		if _, ok := roleSet[role.ID]; !ok {
			continue
		}
		for _, perm := range role.Permissions {
			perms[perm] = struct{}{}
		}
	}
	return &Principal{
		UserID:      user.ID,
		Username:    user.Username,
		Permissions: perms,
	}, nil
}

func hashToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}
