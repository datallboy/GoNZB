package pgindex

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

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
	if latest < v090BaselineVersion {
		t.Fatalf("latest migration = %d, want at least v0.9 baseline version %d", latest, v090BaselineVersion)
	}
	baseline, err := baselineFS.ReadFile(v090BaselinePath)
	if err != nil {
		t.Fatalf("read embedded v0.9 baseline: %v", err)
	}
	if len(baseline) == 0 {
		t.Fatal("embedded v0.9 baseline is empty")
	}
}

func TestFreshBaselineMigration(t *testing.T) {
	store := openPostgresTestStore(t)

	version, err := store.SchemaVersion(context.Background())
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != expectedMigrationVersion() {
		t.Fatalf("schema version = %d, want %d", version, expectedMigrationVersion())
	}
	if err := store.RunMigrations(); err != nil {
		t.Fatalf("repeat migrations on current schema: %v", err)
	}

	var hasLegacyBinaries bool
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT EXISTS (
			SELECT 1
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public'
			  AND c.relname = 'binaries'
			  AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
		)`).Scan(&hasLegacyBinaries); err != nil {
		t.Fatalf("check retired binaries relation: %v", err)
	}
	if hasLegacyBinaries {
		t.Fatalf("fresh v0.8.0 baseline must not create retired public.binaries")
	}
	var recoveryProfileDefault string
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT column_default
		FROM information_schema.columns
		WHERE table_schema = 'public'
		  AND table_name = 'indexer_recovery_capacity_state'
		  AND column_name = 'recovery_profile'`,
	).Scan(&recoveryProfileDefault); err != nil {
		t.Fatalf("check recovery profile migration: %v", err)
	}
	if !strings.Contains(recoveryProfileDefault, "balanced") {
		t.Fatalf("recovery profile default = %q, want balanced", recoveryProfileDefault)
	}
	var hasFederationChainIssues bool
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.tables
			WHERE table_schema = 'public'
			  AND table_name = 'federation_event_chain_issues'
		)`).Scan(&hasFederationChainIssues); err != nil {
		t.Fatalf("check federation chain issue table: %v", err)
	}
	if !hasFederationChainIssues {
		t.Fatalf("fresh schema must create federation_event_chain_issues")
	}

	report, err := store.CheckCriticalIndexerIntegrity(context.Background(), false)
	if err != nil {
		t.Fatalf("check critical indexer integrity on fresh schema: %v", err)
	}
	if report.HasFailures() {
		t.Fatalf("fresh schema critical integrity failed: %s", report.FailureSummary())
	}
	var checkedPartitionParent bool
	for _, check := range report.Checks {
		if check.AccessMethod == "partitioned" {
			checkedPartitionParent = true
			break
		}
	}
	if !checkedPartitionParent {
		t.Fatalf("fresh schema integrity check did not cover partitioned parents: %+v", report.Checks)
	}

	idDefaultTables := []string{
		"binary_inspections",
		"binary_inspection_artifacts",
		"binary_archive_entries",
		"binary_text_evidence",
		"binary_media_streams",
		"binary_par2_sets",
		"binary_par2_targets",
	}
	for _, table := range idDefaultTables {
		var columnDefault sql.NullString
		if err := store.DB().QueryRowContext(context.Background(), `
			SELECT column_default
			FROM information_schema.columns
			WHERE table_schema = 'public'
			  AND table_name = $1
			  AND column_name = 'id'`, table).Scan(&columnDefault); err != nil {
			t.Fatalf("check %s id default: %v", table, err)
		}
		if !columnDefault.Valid || !strings.Contains(columnDefault.String, "nextval(") {
			t.Fatalf("%s.id must have sequence default, got %q", table, columnDefault.String)
		}
	}

	redundantIndexes := []string{
		"idx_binary_grouping_evidence_source_posted",
		"idx_federation_events_author_sequence",
		"idx_release_archive_detail_subtitle_release",
		"idx_release_family_readiness_source_posted",
		"idx_release_ready_candidates_source_posted",
		"idx_release_recovered_file_set_candidates_source_posted",
		"idx_release_stage_dirty_families_source_posted",
		"idx_scrape_checkpoints_provider_newsgroup",
	}
	for _, indexName := range redundantIndexes {
		var exists bool
		if err := store.DB().QueryRowContext(context.Background(), `
			SELECT to_regclass('public.' || $1) IS NOT NULL`, indexName).Scan(&exists); err != nil {
			t.Fatalf("check redundant index %s: %v", indexName, err)
		}
		if exists {
			t.Fatalf("fresh schema must not retain redundant index %s", indexName)
		}
	}

	var baseStemPredicate string
	if err := store.DB().QueryRowContext(context.Background(), `
		SELECT pg_get_expr(i.indpred, i.indrelid)
		FROM pg_index i
		JOIN pg_class c ON c.oid = i.indexrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relname = 'idx_binary_identity_base_stem_lookup'`,
	).Scan(&baseStemPredicate); err != nil {
		t.Fatalf("load release base-stem lookup predicate: %v", err)
	}
	if strings.Contains(baseStemPredicate, "expected_file_count") ||
		strings.Contains(baseStemPredicate, "expected_archive_file_count") {
		t.Fatalf("release base-stem lookup must cover singleton families, predicate=%q", baseStemPredicate)
	}
}

func TestFreshBaselinePathMatchesHistoricalMigrationChain(t *testing.T) {
	db, store := openMigrationTestDatabase(t)

	resetMigrationTestSchema(t, db)
	if err := store.RunMigrations(); err != nil {
		t.Fatalf("apply v0.9 baseline: %v", err)
	}
	baselineSnapshot := migrationSchemaSnapshot(t, db)

	resetMigrationTestSchema(t, db)
	if err := ensureModuleVersionTable(context.Background(), db); err != nil {
		t.Fatalf("create historical migration version table: %v", err)
	}
	migrations, err := loadEmbeddedMigrations()
	if err != nil {
		t.Fatalf("load historical migrations: %v", err)
	}
	for _, migration := range migrations {
		sqlBytes, err := migrationFS.ReadFile(migration.path)
		if err != nil {
			t.Fatalf("read historical migration %s: %v", migration.path, err)
		}
		if err := applyMigrationSQL(
			context.Background(),
			db,
			migration.path,
			migration.version,
			sqlBytes,
		); err != nil {
			t.Fatalf("apply historical migration %s: %v", migration.path, err)
		}
	}
	historicalSnapshot := migrationSchemaSnapshot(t, db)

	if baselineSnapshot != historicalSnapshot {
		t.Fatalf(
			"fresh baseline path differs from migrations 001-%03d:\n%s",
			expectedMigrationVersion(),
			firstSchemaDifference(baselineSnapshot, historicalSnapshot),
		)
	}
}

func TestConcurrentFreshMigrationsAreSerialized(t *testing.T) {
	db, store := openMigrationTestDatabase(t)
	resetMigrationTestSchema(t, db)

	start := make(chan struct{})
	errs := make(chan error, 2)
	for range 2 {
		go func() {
			<-start
			errs <- store.RunMigrations()
		}()
	}
	close(start)

	for range 2 {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent RunMigrations() error: %v", err)
		}
	}
	version, err := currentVersion(context.Background(), db, "pgindex")
	if err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != v090BaselineVersion {
		t.Fatalf("schema version = %d, want %d", version, v090BaselineVersion)
	}
}

func TestMigrationRejectsUnknownSchemaStates(t *testing.T) {
	db, store := openMigrationTestDatabase(t)

	t.Run("unversioned objects", func(t *testing.T) {
		resetMigrationTestSchema(t, db)
		if err := ensureModuleVersionTable(context.Background(), db); err != nil {
			t.Fatalf("create module version table: %v", err)
		}
		if _, err := db.Exec(`CREATE TABLE public.unknown_prebaseline_table (id bigint)`); err != nil {
			t.Fatalf("create unknown schema object: %v", err)
		}
		err := store.RunMigrations()
		if err == nil || !strings.Contains(err.Error(), "unversioned public objects") {
			t.Fatalf("RunMigrations() error = %v, want unversioned-schema rejection", err)
		}
	})

	t.Run("newer version", func(t *testing.T) {
		resetMigrationTestSchema(t, db)
		if err := ensureModuleVersionTable(context.Background(), db); err != nil {
			t.Fatalf("create module version table: %v", err)
		}
		if _, err := db.Exec(`
			INSERT INTO module_schema_version (module_name, version)
			VALUES ('pgindex', 999)`); err != nil {
			t.Fatalf("create newer schema marker: %v", err)
		}
		err := store.RunMigrations()
		if err == nil || !strings.Contains(err.Error(), "newer than this GoNZB build supports") {
			t.Fatalf("RunMigrations() error = %v, want newer-schema rejection", err)
		}
	})

	t.Run("diagnostic extensions", func(t *testing.T) {
		resetMigrationTestSchema(t, db)
		if _, err := db.Exec(`CREATE EXTENSION amcheck`); err != nil {
			t.Fatalf("create diagnostic extension: %v", err)
		}
		if err := store.RunMigrations(); err != nil {
			t.Fatalf("apply v0.9 baseline alongside extension-owned objects: %v", err)
		}
		version, err := currentVersion(context.Background(), db, "pgindex")
		if err != nil {
			t.Fatalf("read schema version: %v", err)
		}
		if version != v090BaselineVersion {
			t.Fatalf("schema version = %d, want %d", version, v090BaselineVersion)
		}
		if _, err := db.Exec(`DROP EXTENSION amcheck`); err != nil {
			t.Fatalf("drop diagnostic extension: %v", err)
		}
	})
}

func TestV080BaselineUpgradesWithoutLosingData(t *testing.T) {
	db, store := openMigrationTestDatabase(t)

	resetMigrationTestSchema(t, db)
	baseline, err := migrationFS.ReadFile("migrations/001_v0_8_0_baseline.up.sql")
	if err != nil {
		t.Fatalf("read v0.8.0 baseline: %v", err)
	}
	if _, err := db.Exec(string(baseline)); err != nil {
		t.Fatalf("apply v0.8.0 baseline: %v", err)
	}
	if err := ensureModuleVersionTable(context.Background(), db); err != nil {
		t.Fatalf("create module version table: %v", err)
	}
	if _, err := db.Exec(`
		INSERT INTO module_schema_version (module_name, version) VALUES ('pgindex', 1);
		INSERT INTO usenet_providers (provider_key, display_name)
		VALUES ('v080-upgrade-sentinel', 'Preserved v0.8 Provider');
		INSERT INTO newsgroups (group_name)
		VALUES ('alt.binaries.v080-upgrade-sentinel')`); err != nil {
		t.Fatalf("populate v0.8.0 sentinel data: %v", err)
	}

	if err := store.RunMigrations(); err != nil {
		t.Fatalf("upgrade populated v0.8.0 schema: %v", err)
	}
	version, err := currentVersion(context.Background(), db, "pgindex")
	if err != nil {
		t.Fatalf("read upgraded schema version: %v", err)
	}
	if version != expectedMigrationVersion() {
		t.Fatalf("upgraded schema version = %d, want %d", version, expectedMigrationVersion())
	}

	var providerName string
	if err := db.QueryRow(`
		SELECT display_name
		FROM usenet_providers
		WHERE provider_key = 'v080-upgrade-sentinel'`).Scan(&providerName); err != nil {
		t.Fatalf("read preserved v0.8 provider: %v", err)
	}
	if providerName != "Preserved v0.8 Provider" {
		t.Fatalf("preserved provider name = %q", providerName)
	}
	var newsgroupCount int
	if err := db.QueryRow(`
		SELECT count(*)
		FROM newsgroups
		WHERE group_name = 'alt.binaries.v080-upgrade-sentinel'`).Scan(&newsgroupCount); err != nil {
		t.Fatalf("read preserved v0.8 newsgroup: %v", err)
	}
	if newsgroupCount != 1 {
		t.Fatalf("preserved v0.8 newsgroups = %d, want 1", newsgroupCount)
	}

	var hardeningIndexExists bool
	if err := db.QueryRow(`SELECT to_regclass('public.idx_federation_events_outbox') IS NOT NULL`).Scan(&hardeningIndexExists); err != nil {
		t.Fatalf("check protocol-hardening migration: %v", err)
	}
	if !hardeningIndexExists {
		t.Fatal("upgraded schema is missing the protocol-hardening outbox index")
	}
}

func openMigrationTestDatabase(t *testing.T) (*sql.DB, *Store) {
	t.Helper()

	postgresTestDatabaseMu.Lock()
	t.Cleanup(postgresTestDatabaseMu.Unlock)

	db, err := sql.Open("pgx", requireTestPostgresDSN(t))
	if err != nil {
		t.Fatalf("open disposable PostgreSQL database: %v", err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Errorf("close disposable PostgreSQL database: %v", err)
		}
	})

	store := &Store{db: db}
	requireDisposableTestDatabase(t, store)
	t.Cleanup(func() {
		resetMigrationTestSchema(t, db)
		if err := store.RunMigrations(); err != nil {
			t.Errorf("restore current PostgreSQL schema after migration test: %v", err)
		}
	})
	return db, store
}

func resetMigrationTestSchema(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatalf("reset PostgreSQL schema: %v", err)
	}
}

func migrationSchemaSnapshot(t *testing.T, db *sql.DB) string {
	t.Helper()

	rows, err := db.Query(`
		WITH schema_objects AS (
			SELECT
				'relation'::text AS object_kind,
				c.relname::text AS object_name,
				concat_ws('|',
					c.relkind::text,
					c.relpersistence::text,
					c.relispartition::text,
					COALESCE(am.amname, ''),
					COALESCE(pg_get_partkeydef(c.oid), ''),
					COALESCE(pg_get_expr(c.relpartbound, c.oid), ''),
					COALESCE(parent.relname, ''),
					c.relrowsecurity::text,
					c.relforcerowsecurity::text
				) AS object_definition
			FROM pg_class c
			JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_am am ON am.oid = c.relam
			LEFT JOIN pg_inherits inh ON inh.inhrelid = c.oid
			LEFT JOIN pg_class parent ON parent.oid = inh.inhparent
			WHERE n.nspname = 'public'
			  AND c.relkind IN ('r', 'p', 'S', 'v', 'm', 'f')
			  AND NOT EXISTS (
				  SELECT 1 FROM pg_depend d
				  WHERE d.classid = 'pg_class'::regclass
				    AND d.objid = c.oid
				    AND d.deptype = 'e'
			  )

			UNION ALL

			SELECT
				'column',
				c.relname || '.' || a.attname,
				concat_ws('|',
					format_type(a.atttypid, a.atttypmod),
					a.attnotnull::text,
					a.attidentity::text,
					a.attgenerated::text,
					COALESCE(pg_get_expr(def.adbin, def.adrelid), ''),
					COALESCE(coll.collname, ''),
					COALESCE(pg_get_serial_sequence(format('%I.%I', n.nspname, c.relname), a.attname), '')
				)
			FROM pg_attribute a
			JOIN pg_class c ON c.oid = a.attrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			LEFT JOIN pg_attrdef def ON def.adrelid = a.attrelid AND def.adnum = a.attnum
			LEFT JOIN pg_collation coll ON coll.oid = a.attcollation
			WHERE n.nspname = 'public'
			  AND c.relkind IN ('r', 'p', 'v', 'm', 'f')
			  AND a.attnum > 0
			  AND NOT a.attisdropped
			  AND NOT EXISTS (
				  SELECT 1 FROM pg_depend d
				  WHERE d.classid = 'pg_class'::regclass
				    AND d.objid = c.oid
				    AND d.deptype = 'e'
			  )

			UNION ALL

			SELECT
				'constraint',
				c.relname || '.' || con.conname,
				pg_get_constraintdef(con.oid, true)
			FROM pg_constraint con
			JOIN pg_class c ON c.oid = con.conrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public'

			UNION ALL

			SELECT
				'index',
				table_class.relname || '.' || index_class.relname,
				pg_get_indexdef(index_class.oid)
			FROM pg_index idx
			JOIN pg_class index_class ON index_class.oid = idx.indexrelid
			JOIN pg_class table_class ON table_class.oid = idx.indrelid
			JOIN pg_namespace n ON n.oid = table_class.relnamespace
			WHERE n.nspname = 'public'

			UNION ALL

			SELECT
				'trigger',
				c.relname || '.' || trigger.tgname,
				pg_get_triggerdef(trigger.oid, true)
			FROM pg_trigger trigger
			JOIN pg_class c ON c.oid = trigger.tgrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public'
			  AND NOT trigger.tgisinternal

			UNION ALL

			SELECT
				'function',
				p.proname || '(' || pg_get_function_identity_arguments(p.oid) || ')',
				pg_get_functiondef(p.oid)
			FROM pg_proc p
			JOIN pg_namespace n ON n.oid = p.pronamespace
			WHERE n.nspname = 'public'
			  AND NOT EXISTS (
				  SELECT 1 FROM pg_depend d
				  WHERE d.classid = 'pg_proc'::regclass
				    AND d.objid = p.oid
				    AND d.deptype = 'e'
			  )

			UNION ALL

			SELECT
				'sequence',
				c.relname,
				concat_ws('|',
					format_type(seq.seqtypid, NULL),
					seq.seqstart::text,
					seq.seqincrement::text,
					seq.seqmax::text,
					seq.seqmin::text,
					seq.seqcache::text,
					seq.seqcycle::text
				)
			FROM pg_sequence seq
			JOIN pg_class c ON c.oid = seq.seqrelid
			JOIN pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = 'public'
		)
		SELECT object_kind, object_name, object_definition
		FROM schema_objects
		ORDER BY object_kind, object_name, object_definition`)
	if err != nil {
		t.Fatalf("query PostgreSQL schema snapshot: %v", err)
	}
	defer rows.Close()

	var snapshot strings.Builder
	for rows.Next() {
		var kind, name, definition string
		if err := rows.Scan(&kind, &name, &definition); err != nil {
			t.Fatalf("scan PostgreSQL schema snapshot: %v", err)
		}
		snapshot.WriteString(kind)
		snapshot.WriteByte('|')
		snapshot.WriteString(name)
		snapshot.WriteByte('|')
		snapshot.WriteString(definition)
		snapshot.WriteByte('\n')
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate PostgreSQL schema snapshot: %v", err)
	}
	return snapshot.String()
}

func firstSchemaDifference(baseline, historical string) string {
	baselineLines := strings.Split(baseline, "\n")
	historicalLines := strings.Split(historical, "\n")
	lineCount := min(len(baselineLines), len(historicalLines))
	for i := range lineCount {
		if baselineLines[i] != historicalLines[i] {
			return "baseline: " + baselineLines[i] + "\nhistory:  " + historicalLines[i]
		}
	}
	if len(baselineLines) != len(historicalLines) {
		return "object counts differ"
	}
	return "snapshots differ"
}
