package auth_test

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/datallboy/gonzb/internal/auth"
	settingsstore "github.com/datallboy/gonzb/internal/store/settings"
)

func TestBootstrapRequiresInitialSetup(t *testing.T) {
	ctx := context.Background()
	store := newTestAuthStore(t)
	svc := auth.NewService(store)

	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	required, err := svc.SetupRequired(ctx)
	if err != nil {
		t.Fatalf("setup required: %v", err)
	}
	if !required {
		t.Fatalf("expected setup required after bootstrap with no users")
	}

	users, err := svc.ListUsers(ctx)
	if err != nil {
		t.Fatalf("list users: %v", err)
	}
	if len(users) != 0 {
		t.Fatalf("expected no default users, got %d", len(users))
	}

	if _, _, err := svc.AuthenticatePassword(ctx, "admin", "admin"); !errors.Is(err, auth.ErrSetupRequired) {
		t.Fatalf("expected setup required auth error, got %v", err)
	}
}

func TestSetupInitialUserOnlyAllowedOnce(t *testing.T) {
	ctx := context.Background()
	store := newTestAuthStore(t)
	svc := auth.NewService(store)

	if err := svc.Bootstrap(ctx); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}

	session, principal, err := svc.SetupInitialUser(ctx, "owner", "very-secure-pass")
	if err != nil {
		t.Fatalf("setup initial user: %v", err)
	}
	if session == nil || principal == nil {
		t.Fatalf("expected session and principal")
	}
	if principal.Username != "owner" {
		t.Fatalf("unexpected principal username %q", principal.Username)
	}
	if !principal.Has(auth.PermissionAuthUsersWrite) {
		t.Fatalf("expected initial user to be admin")
	}

	required, err := svc.SetupRequired(ctx)
	if err != nil {
		t.Fatalf("setup required: %v", err)
	}
	if required {
		t.Fatalf("expected setup to be completed")
	}

	if _, _, err := svc.SetupInitialUser(ctx, "other", "another-secure-pass"); !errors.Is(err, auth.ErrSetupCompleted) {
		t.Fatalf("expected setup completed error, got %v", err)
	}
}

func newTestAuthStore(t *testing.T) *settingsstore.Store {
	t.Helper()
	dir := t.TempDir()
	store, err := settingsstore.NewStore(filepath.Join(dir, "auth.db"))
	if err != nil {
		t.Fatalf("new settings store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}
