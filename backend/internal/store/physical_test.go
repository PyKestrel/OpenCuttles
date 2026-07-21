package store

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// The ADB target lands in an argv position (`adb -s <target> …`). No shell is
// involved, so this is not shell injection — but a value starting with "-"
// would be read by adb as a flag rather than a device name.
func TestValidateADBTarget(t *testing.T) {
	valid := []string{
		"R5CT30ABCDE",       // USB serial
		"192.168.1.42:5555", // adb over TCP
		"emulator-5554",     // emulator serial
		"my-lab_phone.01",   // punctuation we allow
	}
	for _, target := range valid {
		if err := ValidateADBTarget(target); err != nil {
			t.Errorf("ValidateADBTarget(%q) rejected a valid target: %v", target, err)
		}
	}

	invalid := map[string]string{
		"":                       "empty",
		"   ":                    "whitespace only",
		"-e":                     "leading dash would be read as an adb flag",
		"--help":                 "leading dashes would be read as an adb flag",
		"host;rm -rf /":          "punctuation outside the allowed set",
		"host name":              "embedded space",
		"tab\there":              "embedded tab",
		strings.Repeat("a", 200): "absurdly long",
	}
	for target, why := range invalid {
		if err := ValidateADBTarget(target); err == nil {
			t.Errorf("ValidateADBTarget(%q) accepted an invalid target (%s)", target, why)
		}
	}
}

func TestCreatePhysicalAndroid(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	inst, err := db.CreatePhysicalAndroid(ctx, "Pixel 8", "R5CT30ABCDE")
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if inst.Source() != domain.SourcePhysical {
		t.Fatalf("source = %q, want physical", inst.Source())
	}
	if inst.Platform != domain.PlatformAndroid {
		t.Fatalf("platform = %q, want android", inst.Platform)
	}
	if inst.IsProvisioned() {
		t.Fatal("a physical device must not report as provisioned — it has no start/stop lifecycle")
	}
	// Nothing is allocated: no ports, no synthesized cvd device id.
	if inst.ADBPort != 0 || inst.WebRTCPort != 0 || inst.DeviceID != "" {
		t.Fatalf("physical device got provisioning artifacts: port=%d webrtc=%d deviceID=%q",
			inst.ADBPort, inst.WebRTCPort, inst.DeviceID)
	}
	if inst.State != domain.StateOffline {
		t.Fatalf("state = %q, want offline until the poller sees it", inst.State)
	}
	if inst.ConsoleProvider != domain.ConsoleProviderScreenshot {
		t.Fatalf("console provider = %q, want screenshot", inst.ConsoleProvider)
	}

	stored, err := db.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.ADBTarget != "R5CT30ABCDE" {
		t.Fatalf("stored target = %q", stored.ADBTarget)
	}
}

// Two rows pointing at one handset would race to drive it.
func TestCreatePhysicalAndroidRejectsDuplicateTargets(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	if _, err := db.CreatePhysicalAndroid(ctx, "Pixel 8", "R5CT30ABCDE"); err != nil {
		t.Fatalf("first create: %v", err)
	}
	_, err := db.CreatePhysicalAndroid(ctx, "Pixel 8 again", "R5CT30ABCDE")
	if err == nil {
		t.Fatal("the same ADB target was registered twice")
	}
	if !strings.Contains(err.Error(), "already registered") {
		t.Fatalf("error should name the conflict, got: %v", err)
	}
}

func TestCreatePhysicalAndroidValidatesInput(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	if _, err := db.CreatePhysicalAndroid(ctx, "", "R5CT30ABCDE"); err == nil {
		t.Error("a device with no name was accepted")
	}
	if _, err := db.CreatePhysicalAndroid(ctx, "Pixel", "-e"); err == nil {
		t.Error("an ADB target that adb would read as a flag was accepted")
	}
}

func TestListPhysicalAndroidExcludesOtherSources(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	phone, err := db.CreatePhysicalAndroid(ctx, "Pixel 8", "R5CT30ABCDE")
	if err != nil {
		t.Fatalf("create phone: %v", err)
	}
	if _, err := db.CreateDesktopInstance(ctx, "desk-1", domain.PlatformWindows, "hash"); err != nil {
		t.Fatalf("create desktop: %v", err)
	}
	if _, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{Name: "cvd-1"}); err != nil {
		t.Fatalf("create cuttlefish: %v", err)
	}

	list, err := db.ListPhysicalAndroid(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || list[0].ID != phone.ID {
		t.Fatalf("expected only the physical device, got %d: %+v", len(list), list)
	}
}

// Tap and scroll coordinates are in the device's input space; the ADB driver
// otherwise falls back to a hardcoded 720x1280, wrong for essentially any real
// handset.
func TestUpdateInstanceDisplay(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)

	inst, err := db.CreatePhysicalAndroid(ctx, "Pixel 8", "R5CT30ABCDE")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := db.UpdateInstanceDisplay(ctx, inst.ID, 1080, 2400, 420); err != nil {
		t.Fatalf("update display: %v", err)
	}
	stored, err := db.GetInstance(ctx, inst.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.DisplayWidth != 1080 || stored.DisplayHeight != 2400 || stored.DPI != 420 {
		t.Fatalf("display = %dx%d @%d", stored.DisplayWidth, stored.DisplayHeight, stored.DPI)
	}
}

// A device-farm appliance may have no Cuttlefish image at all. Registration
// still routes through GetOrCreateDefaultImage to satisfy the instances.image_id
// foreign key, so this pins that the placeholder is cosmetic rather than a
// precondition — without it, a phones-and-desktops-only deployment would be
// unable to register anything.
func TestRegistrationWorksWithoutACuttlefishImage(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(dir, "images"))
	t.Setenv("OPENCUTTLES_DEFAULT_IMAGE_PATH", "")

	db, err := OpenSQLite(filepath.Join(dir, "oc.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if _, err := db.CreatePhysicalAndroid(ctx, "phone", "R5CT30ABCDE"); err != nil {
		t.Fatalf("registering a handset needs no Cuttlefish image: %v", err)
	}
	if _, err := db.CreateDesktopInstance(ctx, "desk", domain.PlatformWindows, "h"); err != nil {
		t.Fatalf("onboarding a desktop needs no Cuttlefish image: %v", err)
	}
}
