package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// Existing desktop devices predate the source column and must be classified by
// the backfill, not left to be guessed at.
func TestSourceBackfillClassifiesExistingDevices(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(dir, "images"))
	dbPath := filepath.Join(dir, "opencuttles.db")

	db, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	desktop, err := db.CreateDesktopInstance(ctx, "desk-1", domain.PlatformWindows, "hash-a")
	if err != nil {
		t.Fatalf("create desktop: %v", err)
	}
	android, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{Name: "phone-1"})
	if err != nil {
		t.Fatalf("create android: %v", err)
	}

	// Simulate rows written before the column existed.
	if _, err := db.db.ExecContext(ctx, `UPDATE instances SET source = ''`); err != nil {
		t.Fatalf("clear source: %v", err)
	}
	if _, err := db.db.ExecContext(ctx, `DELETE FROM schema_migrations WHERE version = 5`); err != nil {
		t.Fatalf("clear migration marker: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Reopening runs the migration.
	db2, err := OpenSQLite(dbPath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer db2.Close()

	gotDesktop, err := db2.GetInstance(ctx, desktop.ID)
	if err != nil {
		t.Fatalf("get desktop: %v", err)
	}
	if gotDesktop.Source() != domain.SourceRunner {
		t.Fatalf("desktop source = %q, want runner", gotDesktop.Source())
	}
	gotAndroid, err := db2.GetInstance(ctx, android.ID)
	if err != nil {
		t.Fatalf("get android: %v", err)
	}
	if gotAndroid.Source() != domain.SourceCuttlefish {
		t.Fatalf("android source = %q, want cuttlefish", gotAndroid.Source())
	}

	// Migrating again must be a no-op, not a re-stamp: an operator may change a
	// device's source deliberately and a repeated backfill would overwrite it.
	if _, err := db2.db.ExecContext(ctx, `UPDATE instances SET source = ? WHERE id = ?`,
		domain.SourcePhysical, gotAndroid.ID); err != nil {
		t.Fatalf("reclassify: %v", err)
	}
	if err := db2.migrate(ctx); err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	again, err := db2.GetInstance(ctx, gotAndroid.ID)
	if err != nil {
		t.Fatalf("get after re-migrate: %v", err)
	}
	if again.Source() != domain.SourcePhysical {
		t.Fatalf("re-running the migration overwrote a deliberate source: got %q", again.Source())
	}
}

// The safety property that matters most in this refactor: the token lookup
// switched from "platform is not android" to "source = runner". If a physical
// Android device could ever be returned here, an enrollment token would resolve
// to someone's phone.
func TestEnrollmentTokenNeverResolvesToAPhysicalDevice(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	hash := func(tok string) string {
		sum := sha256.Sum256([]byte(tok))
		return hex.EncodeToString(sum[:])
	}

	desktop, err := db.CreateDesktopInstance(ctx, "desk-1", domain.PlatformWindows, hash("desktop-token"))
	if err != nil {
		t.Fatalf("create desktop: %v", err)
	}

	// A physical Android device carrying the *same* token hash — the strongest
	// form of the mistake this guards against.
	phone, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{Name: "phone-1"})
	if err != nil {
		t.Fatalf("create phone: %v", err)
	}
	if _, err := db.db.ExecContext(ctx,
		`UPDATE instances SET source = ?, adb_target = ?, control_token_ciphertext = ? WHERE id = ?`,
		domain.SourcePhysical, "R5CT30ABCDE", hash("desktop-token"), phone.ID); err != nil {
		t.Fatalf("set up phone: %v", err)
	}

	found, err := db.FindDesktopByTokenHash(ctx, hash("desktop-token"))
	if err != nil {
		t.Fatalf("lookup: %v", err)
	}
	if found.ID == phone.ID {
		t.Fatal("an enrollment token resolved to a physical device")
	}
	if found.ID != desktop.ID {
		t.Fatalf("lookup returned %q, want the desktop %q", found.ID, desktop.ID)
	}

	// The same must hold for the token-setting path.
	if ok, err := db.SetDesktopTokenHash(ctx, phone.ID, hash("new")); err != nil {
		t.Fatalf("set token: %v", err)
	} else if ok {
		t.Fatal("a physical device accepted an enrollment token")
	}
}

func TestNewDesktopDevicesAreTaggedAsRunners(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	inst, err := db.CreateDesktopInstance(ctx, "desk-1", domain.PlatformMacOS, "hash")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if inst.Source() != domain.SourceRunner {
		t.Fatalf("returned instance source = %q", inst.Source())
	}
	stored, err := db.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.Source() != domain.SourceRunner {
		t.Fatalf("stored source = %q, want runner", stored.Source())
	}
}

// Cuttlefish instances keep an empty ADB target so their addressing is unchanged.
func TestCuttlefishInstancesKeepPortAddressing(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	inst, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{Name: "phone-1"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	stored, err := db.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.ADBTarget != "" {
		t.Fatalf("cuttlefish instance got an adb_target %q", stored.ADBTarget)
	}
	if stored.Source() != domain.SourceCuttlefish {
		t.Fatalf("cuttlefish source = %q", stored.Source())
	}
	if !stored.IsProvisioned() {
		t.Fatal("a cuttlefish instance should report as provisioned")
	}
}
