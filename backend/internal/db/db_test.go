package db

import (
	"database/sql"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// ---------------------------------------------------------------------------
// Open
// ---------------------------------------------------------------------------

func TestOpen_FreshDatabase(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "sentinel.db")
	d, err := Open(dbPath, true, testLogger())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	// Database file should exist on disk.
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("database file not created: %v", err)
	}

	// The _migrations tracking table must exist.
	var count int
	if err := d.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&count); err != nil {
		t.Fatalf("_migrations table not created: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one applied migration, got 0")
	}
}

func TestOpen_WALModeEnabled(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "wal.db")
	d, err := Open(dbPath, true, testLogger())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	var mode string
	if err := d.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("querying journal_mode: %v", err)
	}
	if mode != "wal" {
		t.Errorf("journal_mode = %q, want %q", mode, "wal")
	}
}

func TestOpen_WALModeDisabled(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "nowal.db")
	d, err := Open(dbPath, false, testLogger())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	var mode string
	if err := d.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatalf("querying journal_mode: %v", err)
	}
	// When walMode=false, SQLite defaults to "delete" (or "memory" for :memory:).
	// It should NOT be "wal".
	if mode == "wal" {
		t.Error("journal_mode should not be wal when walMode=false")
	}
}

func TestOpen_ForeignKeysEnabled(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "fk.db")
	d, err := Open(dbPath, true, testLogger())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	var fk int
	if err := d.QueryRow("PRAGMA foreign_keys").Scan(&fk); err != nil {
		t.Fatalf("querying foreign_keys: %v", err)
	}
	if fk != 1 {
		t.Errorf("foreign_keys = %d, want 1", fk)
	}
}

func TestOpen_BusyTimeout(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "bt.db")
	d, err := Open(dbPath, true, testLogger())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	var timeout int
	if err := d.QueryRow("PRAGMA busy_timeout").Scan(&timeout); err != nil {
		t.Fatalf("querying busy_timeout: %v", err)
	}
	if timeout != 5000 {
		t.Errorf("busy_timeout = %d, want 5000", timeout)
	}
}

func TestOpen_ConnectionPoolSettings(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "pool.db")
	d, err := Open(dbPath, true, testLogger())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	stats := d.Stats()
	if stats.MaxOpenConnections != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", stats.MaxOpenConnections)
	}
}

func TestOpen_InvalidPath(t *testing.T) {
	t.Parallel()
	// A path inside a non-existent directory should fail.
	badPath := filepath.Join(t.TempDir(), "no", "such", "dir", "test.db")
	d, err := Open(badPath, true, testLogger())
	if err == nil {
		d.Close()
		t.Fatal("expected error for invalid path, got nil")
	}
}

func TestOpen_InMemory(t *testing.T) {
	t.Parallel()
	d, err := Open(":memory:", true, testLogger())
	if err != nil {
		t.Fatalf("Open :memory: failed: %v", err)
	}
	defer d.Close()

	// Should still have migrations applied.
	var count int
	if err := d.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&count); err != nil {
		t.Fatalf("_migrations query failed: %v", err)
	}
	if count == 0 {
		t.Fatal("expected at least one migration applied in :memory: DB")
	}
}

func TestOpen_SchemaTablesExist(t *testing.T) {
	t.Parallel()
	d, err := Open(":memory:", true, testLogger())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	// Core tables expected after all migrations.
	tables := []string{
		"cameras",
		"events",
		"users",
		"recordings",
		"_migrations",
	}
	for _, tbl := range tables {
		var name string
		err := d.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", tbl,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", tbl, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Migrations
// ---------------------------------------------------------------------------

func TestMigrations_RunInOrder(t *testing.T) {
	t.Parallel()
	d, err := Open(":memory:", false, testLogger())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	rows, err := d.Query("SELECT version FROM _migrations ORDER BY version")
	if err != nil {
		t.Fatalf("querying _migrations: %v", err)
	}
	defer rows.Close()

	var versions []int
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			t.Fatalf("scanning version: %v", err)
		}
		versions = append(versions, v)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("rows iteration error: %v", err)
	}

	if len(versions) == 0 {
		t.Fatal("no migrations found")
	}

	// Verify strictly ascending order.
	for i := 1; i < len(versions); i++ {
		if versions[i] <= versions[i-1] {
			t.Errorf("migration order violated: version %d is not greater than %d",
				versions[i], versions[i-1])
		}
	}
}

func TestMigrations_IdempotentRerun(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "idempotent.db")

	// First open: applies all migrations.
	d1, err := Open(dbPath, true, testLogger())
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}

	var countBefore int
	if err := d1.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&countBefore); err != nil {
		t.Fatalf("counting migrations: %v", err)
	}
	d1.Close()

	// Second open: re-runs the migration runner — must not error or re-apply.
	d2, err := Open(dbPath, true, testLogger())
	if err != nil {
		t.Fatalf("second Open failed (idempotency broken): %v", err)
	}
	defer d2.Close()

	var countAfter int
	if err := d2.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&countAfter); err != nil {
		t.Fatalf("counting migrations after rerun: %v", err)
	}

	if countAfter != countBefore {
		t.Errorf("migration count changed from %d to %d on re-run", countBefore, countAfter)
	}
}

func TestMigrations_VersionsCoverExpected(t *testing.T) {
	t.Parallel()
	d, err := Open(":memory:", false, testLogger())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	// The project has migrations 001 through 014 (no 015 yet).
	// Verify all expected versions are present.
	expected := []int{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14}
	for _, v := range expected {
		var count int
		err := d.QueryRow("SELECT COUNT(*) FROM _migrations WHERE version = ?", v).Scan(&count)
		if err != nil {
			t.Errorf("checking migration %d: %v", v, err)
		}
		if count != 1 {
			t.Errorf("migration %d: count = %d, want 1", v, count)
		}
	}
}

func TestMigrations_ForeignKeyCascade(t *testing.T) {
	t.Parallel()
	d, err := Open(":memory:", true, testLogger())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	// Migration 003 should have set up ON DELETE CASCADE for events.camera_id.
	// Insert a camera and an event, delete the camera, verify event is gone.
	_, err = d.Exec(`INSERT INTO cameras (name, main_stream) VALUES ('test-cam', 'rtsp://localhost/test')`)
	if err != nil {
		t.Fatalf("inserting camera: %v", err)
	}

	var camID int
	if err := d.QueryRow("SELECT id FROM cameras WHERE name='test-cam'").Scan(&camID); err != nil {
		t.Fatalf("selecting camera: %v", err)
	}

	_, err = d.Exec(`INSERT INTO events (camera_id, type, label) VALUES (?, 'motion', 'test')`, camID)
	if err != nil {
		t.Fatalf("inserting event: %v", err)
	}

	// Delete the camera — cascade should remove the event.
	_, err = d.Exec("DELETE FROM cameras WHERE id = ?", camID)
	if err != nil {
		t.Fatalf("deleting camera: %v", err)
	}

	var eventCount int
	if err := d.QueryRow("SELECT COUNT(*) FROM events WHERE camera_id = ?", camID).Scan(&eventCount); err != nil {
		t.Fatalf("counting events: %v", err)
	}
	if eventCount != 0 {
		t.Errorf("expected 0 events after cascade delete, got %d", eventCount)
	}
}

// ---------------------------------------------------------------------------
// splitStatements
// ---------------------------------------------------------------------------

func TestSplitStatements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  []string
	}{
		{
			name:  "simple two statements",
			input: "CREATE TABLE a (id INT); CREATE TABLE b (id INT);",
			want:  []string{"CREATE TABLE a (id INT)", "CREATE TABLE b (id INT)"},
		},
		{
			name:  "trailing statement without semicolon",
			input: "SELECT 1; SELECT 2",
			want:  []string{"SELECT 1", "SELECT 2"},
		},
		{
			name:  "empty input",
			input: "",
			want:  nil,
		},
		{
			name:  "only whitespace",
			input: "   \n\t  ",
			want:  nil,
		},
		{
			name:  "only semicolons",
			input: ";;;",
			want:  nil,
		},
		{
			name:  "semicolon inside single-quoted string",
			input: "INSERT INTO t VALUES ('hello; world');",
			want:  []string{"INSERT INTO t VALUES ('hello; world')"},
		},
		{
			name:  "escaped single quote with semicolon",
			input: "INSERT INTO t VALUES ('it''s; a test');",
			want:  []string{"INSERT INTO t VALUES ('it''s; a test')"},
		},
		{
			name:  "semicolon inside double-quoted identifier",
			input: `CREATE TABLE "my;table" (id INT);`,
			want:  []string{`CREATE TABLE "my;table" (id INT)`},
		},
		{
			name:  "escaped double quote with semicolon",
			input: `CREATE TABLE "tab""le;x" (id INT);`,
			want:  []string{`CREATE TABLE "tab""le;x" (id INT)`},
		},
		{
			name:  "line comment hides semicolon",
			input: "SELECT 1; -- this is a comment; not a separator\nSELECT 2;",
			want:  []string{"SELECT 1", "-- this is a comment; not a separator\nSELECT 2"},
		},
		{
			name:  "line comment at end of input",
			input: "SELECT 1; -- trailing comment",
			want:  []string{"SELECT 1", "-- trailing comment"},
		},
		{
			name:  "block comment hides semicolon",
			input: "SELECT /* ; hidden */ 1; SELECT 2;",
			want:  []string{"SELECT /* ; hidden */ 1", "SELECT 2"},
		},
		{
			name:  "multiline block comment",
			input: "SELECT /*\n;\n*/ 1;",
			want:  []string{"SELECT /*\n;\n*/ 1"},
		},
		{
			name: "complex migration-like SQL",
			input: `CREATE TABLE IF NOT EXISTS events_new (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    camera_id INTEGER REFERENCES cameras(id) ON DELETE CASCADE
);
INSERT INTO events_new SELECT id, camera_id FROM events;
DROP TABLE events;
ALTER TABLE events_new RENAME TO events;`,
			want: []string{
				"CREATE TABLE IF NOT EXISTS events_new (\n    id INTEGER PRIMARY KEY AUTOINCREMENT,\n    camera_id INTEGER REFERENCES cameras(id) ON DELETE CASCADE\n)",
				"INSERT INTO events_new SELECT id, camera_id FROM events",
				"DROP TABLE events",
				"ALTER TABLE events_new RENAME TO events",
			},
		},
		{
			name:  "string with escaped quotes and semicolons",
			input: "INSERT INTO t (v) VALUES ('a''b;c''d'); SELECT 1;",
			want:  []string{"INSERT INTO t (v) VALUES ('a''b;c''d')", "SELECT 1"},
		},
		{
			name:  "mixed quotes and comments",
			input: "INSERT INTO t VALUES ('val'); -- comment;\nCREATE TABLE \"x;y\" (id INT);",
			want: []string{
				"INSERT INTO t VALUES ('val')",
				"-- comment;\nCREATE TABLE \"x;y\" (id INT)",
			},
		},
		{
			name:  "block comment immediately before semicolon",
			input: "SELECT 1 /* comment */; SELECT 2;",
			want:  []string{"SELECT 1 /* comment */", "SELECT 2"},
		},
		{
			name:  "nested-looking block comment",
			input: "SELECT /* a /* b */ 1; SELECT 2;",
			want:  []string{"SELECT /* a /* b */ 1", "SELECT 2"},
		},
		{
			name:  "single statement no semicolon",
			input: "SELECT 42",
			want:  []string{"SELECT 42"},
		},
		{
			name:  "whitespace between semicolons",
			input: "SELECT 1;  ;  ; SELECT 2;",
			want:  []string{"SELECT 1", "SELECT 2"},
		},
		{
			name: "trigger-style SQL with semicolons inside body",
			input: `CREATE TRIGGER IF NOT EXISTS trg_update AFTER UPDATE ON cameras
BEGIN
    UPDATE cameras SET updated_at = datetime('now') WHERE id = NEW.id;
END;
SELECT 1;`,
			// The splitter splits on semicolons outside quotes/comments.
			// Trigger bodies have inner semicolons that the splitter WILL split
			// (unless wrapped in quotes). This is the known behavior — the
			// migration files in this project don't use triggers, but we
			// document the behavior here.
			want: []string{
				"CREATE TRIGGER IF NOT EXISTS trg_update AFTER UPDATE ON cameras\nBEGIN\n    UPDATE cameras SET updated_at = datetime('now') WHERE id = NEW.id",
				"END",
				"SELECT 1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := splitStatements(tt.input)

			if len(got) != len(tt.want) {
				t.Fatalf("splitStatements() returned %d statements, want %d\ngot:  %q\nwant: %q",
					len(got), len(tt.want), got, tt.want)
			}
			for i := range got {
				if got[i] != tt.want[i] {
					t.Errorf("statement[%d]:\n  got:  %q\n  want: %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

// ---------------------------------------------------------------------------
// runMigrations (via helper that opens a raw DB)
// ---------------------------------------------------------------------------

func openRawDB(t *testing.T) *sql.DB {
	t.Helper()
	d, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	d.SetMaxOpenConns(1)
	d.SetMaxIdleConns(1)
	if _, err := d.Exec("PRAGMA foreign_keys = ON"); err != nil {
		d.Close()
		t.Fatalf("setting foreign_keys: %v", err)
	}
	return d
}

func TestRunMigrations_CreatesTrackingTable(t *testing.T) {
	t.Parallel()
	d := openRawDB(t)
	defer d.Close()

	if err := runMigrations(d, testLogger()); err != nil {
		t.Fatalf("runMigrations failed: %v", err)
	}

	// _migrations table must exist with the expected columns.
	row := d.QueryRow("SELECT version, name, applied_at FROM _migrations LIMIT 1")
	var version int
	var name, appliedAt string
	if err := row.Scan(&version, &name, &appliedAt); err != nil {
		t.Fatalf("scanning _migrations row: %v", err)
	}
	if version < 1 {
		t.Errorf("unexpected version %d", version)
	}
	if name == "" {
		t.Error("migration name is empty")
	}
	if appliedAt == "" {
		t.Error("applied_at is empty")
	}
}

func TestRunMigrations_Idempotent(t *testing.T) {
	t.Parallel()
	d := openRawDB(t)
	defer d.Close()

	// Run once.
	if err := runMigrations(d, testLogger()); err != nil {
		t.Fatalf("first run failed: %v", err)
	}
	var c1 int
	d.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&c1)

	// Run again.
	if err := runMigrations(d, testLogger()); err != nil {
		t.Fatalf("second run failed: %v", err)
	}
	var c2 int
	d.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&c2)

	if c1 != c2 {
		t.Errorf("migration count changed: %d → %d", c1, c2)
	}
}

func TestRunMigrations_AllVersionsRecorded(t *testing.T) {
	t.Parallel()
	d := openRawDB(t)
	defer d.Close()

	if err := runMigrations(d, testLogger()); err != nil {
		t.Fatalf("runMigrations failed: %v", err)
	}

	// Read embedded migration files to determine expected count.
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		t.Fatalf("reading embedded migrations: %v", err)
	}
	expectedCount := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			expectedCount++
		}
	}

	var actualCount int
	d.QueryRow("SELECT COUNT(*) FROM _migrations").Scan(&actualCount)

	if actualCount != expectedCount {
		t.Errorf("applied %d migrations, want %d (matching embedded .sql files)", actualCount, expectedCount)
	}
}

// ---------------------------------------------------------------------------
// Open with existing data (preserves data across re-opens)
// ---------------------------------------------------------------------------

func TestOpen_PreservesDataAcrossReopen(t *testing.T) {
	t.Parallel()
	dbPath := filepath.Join(t.TempDir(), "preserve.db")

	// Open, insert data, close.
	d1, err := Open(dbPath, true, testLogger())
	if err != nil {
		t.Fatalf("first Open failed: %v", err)
	}
	_, err = d1.Exec(`INSERT INTO cameras (name, main_stream) VALUES ('persist-cam', 'rtsp://localhost/test')`)
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}
	d1.Close()

	// Reopen — data should still be there.
	d2, err := Open(dbPath, true, testLogger())
	if err != nil {
		t.Fatalf("second Open failed: %v", err)
	}
	defer d2.Close()

	var name string
	if err := d2.QueryRow("SELECT name FROM cameras WHERE name='persist-cam'").Scan(&name); err != nil {
		t.Fatalf("data not preserved: %v", err)
	}
	if name != "persist-cam" {
		t.Errorf("name = %q, want %q", name, "persist-cam")
	}
}

// ---------------------------------------------------------------------------
// Concurrent reads are safe with the single-connection pool
// ---------------------------------------------------------------------------

func TestOpen_ConcurrentReads(t *testing.T) {
	t.Parallel()
	d, err := Open(":memory:", true, testLogger())
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer d.Close()

	// Insert seed data.
	_, err = d.Exec(`INSERT INTO cameras (name, main_stream) VALUES ('cam1', 'rtsp://x')`)
	if err != nil {
		t.Fatalf("inserting: %v", err)
	}

	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			var n string
			errs <- d.QueryRow("SELECT name FROM cameras LIMIT 1").Scan(&n)
		}()
	}
	for i := 0; i < 10; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent read %d failed: %v", i, err)
		}
	}
}

// ---------------------------------------------------------------------------
// Edge cases for splitStatements
// ---------------------------------------------------------------------------

func TestSplitStatements_OnlyComments(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{"line comment only", "-- just a comment", 1},
		{"block comment only", "/* just a comment */", 1},
		{"line comment with newline", "-- comment\n", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := splitStatements(tt.input)
			if len(got) != tt.want {
				t.Errorf("splitStatements(%q) returned %d stmts, want %d: %q",
					tt.input, len(got), tt.want, got)
			}
		})
	}
}

func TestSplitStatements_RealMigrationSQL(t *testing.T) {
	t.Parallel()

	// Read an actual embedded migration to verify the splitter handles it.
	content, err := migrationsFS.ReadFile("migrations/003_fix_events_fk_cascade.sql")
	if err != nil {
		t.Fatalf("reading migration 003: %v", err)
	}

	stmts := splitStatements(string(content))
	// Migration 003 has: CREATE TABLE, INSERT INTO, DROP TABLE, ALTER TABLE,
	// CREATE INDEX (x2) = 6 statements (plus comments embedded in them).
	if len(stmts) < 4 {
		t.Errorf("expected at least 4 statements from migration 003, got %d", len(stmts))
	}

	// Verify no empty statements snuck through.
	for i, s := range stmts {
		trimmed := strings.TrimSpace(s)
		if trimmed == "" {
			t.Errorf("statement[%d] is empty after trimming", i)
		}
	}
}

func TestSplitStatements_MigrationWithDatetimeFunction(t *testing.T) {
	t.Parallel()

	// Migration 012 has datetime('now') and strftime calls with single-quoted args.
	content, err := migrationsFS.ReadFile("migrations/012_faces.sql")
	if err != nil {
		t.Fatalf("reading migration 012: %v", err)
	}

	stmts := splitStatements(string(content))
	if len(stmts) == 0 {
		t.Fatal("expected at least one statement from migration 012")
	}

	// None should be broken mid-string.
	for i, s := range stmts {
		// After removing escaped '' pairs, single-quote count must be even.
		unescaped := strings.ReplaceAll(s, "''", "")
		if strings.Count(unescaped, "'")%2 != 0 {
			t.Errorf("statement[%d] has unmatched single quotes: %q", i, s)
		}
	}
}
