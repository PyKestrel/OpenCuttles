package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLiteDSNEscaping(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{"plain unix path", "/var/lib/opencuttles/opencuttles.db", "file:/var/lib/opencuttles/opencuttles.db?"},
		{"space", "/var/lib/open cuttles/oc.db", "file:/var/lib/open%20cuttles/oc.db?"},
		{"question mark", "/var/lib/oc?.db", "file:/var/lib/oc%3F.db?"},
		{"hash", "/var/lib/oc#1.db", "file:/var/lib/oc%231.db?"},
		// '%' must be escaped first, or the escapes above get double-encoded.
		{"percent", "/var/lib/oc%20.db", "file:/var/lib/oc%2520.db?"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := sqliteDSN(tc.path)
			if !strings.HasPrefix(got, tc.want) {
				t.Fatalf("sqliteDSN(%q)\n got %q\nwant prefix %q", tc.path, got, tc.want)
			}
			for _, pragma := range []string{"busy_timeout(5000)", "foreign_keys(1)", "journal_mode(WAL)"} {
				if !strings.Contains(got, pragma) {
					t.Errorf("DSN missing %s: %s", pragma, got)
				}
			}
		})
	}
}

// The bug this guards: the pragmas used to be applied with db.Exec after
// opening, which only configures whichever pooled connection served that call.
// database/sql can retire and reopen connections, and a replacement would come
// up with foreign_keys OFF and no busy_timeout. As DSN parameters the driver
// applies them to every connection.
func TestPragmasApplyToReplacementConnections(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	check := func(stage string) {
		t.Helper()
		var fk, busy, journal string
		if err := db.db.QueryRowContext(ctx, `PRAGMA foreign_keys`).Scan(&fk); err != nil {
			t.Fatalf("%s foreign_keys: %v", stage, err)
		}
		if err := db.db.QueryRowContext(ctx, `PRAGMA busy_timeout`).Scan(&busy); err != nil {
			t.Fatalf("%s busy_timeout: %v", stage, err)
		}
		if err := db.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journal); err != nil {
			t.Fatalf("%s journal_mode: %v", stage, err)
		}
		if fk != "1" {
			t.Fatalf("%s: foreign_keys = %q, want 1", stage, fk)
		}
		if busy != "5000" {
			t.Fatalf("%s: busy_timeout = %q, want 5000", stage, busy)
		}
		if !strings.EqualFold(journal, "wal") {
			t.Fatalf("%s: journal_mode = %q, want wal", stage, journal)
		}
	}

	check("initial")

	// Force the pool to discard its connection and open a fresh one.
	db.db.SetMaxIdleConns(0)
	for i := 0; i < 3; i++ {
		if _, err := db.db.ExecContext(ctx, `SELECT 1`); err != nil {
			t.Fatalf("churn %d: %v", i, err)
		}
	}
	check("after connection churn")
}

// WAL mode is what makes the backup script's sqlite3 .backup safe; confirm the
// sidecar files actually appear on disk.
func TestWALFilesCreated(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(tempDir, "images"))
	path := filepath.Join(tempDir, "opencuttles.db")
	db, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	var mode string
	if err := db.db.QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&mode); err != nil {
		t.Fatalf("journal_mode: %v", err)
	}
	if !strings.EqualFold(mode, "wal") {
		t.Fatalf("journal_mode = %q, want wal", mode)
	}
}
