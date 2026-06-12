package pgindex

import "testing"

func TestExpectedMigrationVersionTracksLatestEmbeddedMigration(t *testing.T) {
	migs, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("loadEmbeddedMigrations() error = %v", err)
	}
	if len(migs) == 0 {
		t.Fatal("expected embedded migrations")
	}

	latest := migs[len(migs)-1].version
	if got := expectedMigrationVersion(); got != latest {
		t.Fatalf("expectedMigrationVersion() = %d, want %d", got, latest)
	}
}
