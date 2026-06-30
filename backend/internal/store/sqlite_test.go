package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

func TestStoreAuthAuditAndInstancePersistence(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(tempDir, "images"))
	db, err := OpenSQLite(filepath.Join(tempDir, "opencuttles.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	user, err := db.CreateUser(ctx, "admin", "Admin", domain.RoleAdmin, "hash")
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	if count, err := db.UserCount(ctx); err != nil || count != 1 {
		t.Fatalf("user count = %d, err = %v", count, err)
	}
	if _, err := db.CreateSession(ctx, user.ID, "token-hash", time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("create session: %v", err)
	}
	if _, sessionUser, err := db.GetSessionUser(ctx, "token-hash"); err != nil || sessionUser.ID != user.ID {
		t.Fatalf("session user = %+v, err = %v", sessionUser, err)
	}

	image, err := db.CreateImage(ctx, domain.CreateImageRequest{Name: "AOSP", Path: "/var/lib/opencuttles/images/aosp"})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	instance, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{Name: "android-01", ImageID: image.ID, CPUCores: 4, MemoryMB: 8192})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	wantConsole := "/api/v1/instances/" + instance.ID + "/console/devices/" + instance.DeviceID + "/files/client.html"
	if instance.ConsoleURL != wantConsole {
		t.Fatalf("console url = %q, want %q", instance.ConsoleURL, wantConsole)
	}
	if instance.DeviceID != "cvd_1-1-1" {
		t.Fatalf("device id = %q, want cvd_1-1-1", instance.DeviceID)
	}
	if instance.WebRTCPort != webrtcOperatorPort {
		t.Fatalf("webrtc port = %d, want %d", instance.WebRTCPort, webrtcOperatorPort)
	}

	if _, err := db.CreateAuditEvent(ctx, domain.AuditEvent{ActorID: user.ID, Action: "test", Resource: "instance", ResourceID: instance.ID, Outcome: "succeeded"}); err != nil {
		t.Fatalf("create audit: %v", err)
	}
	events, err := db.ListAuditEvents(ctx, 10)
	if err != nil {
		t.Fatalf("list audit: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("audit events = %d", len(events))
	}
}

func TestCreateInstanceAutoRegistersDefaultImage(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(tempDir, "images"))

	db, err := OpenSQLite(filepath.Join(tempDir, "opencuttles.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	instance, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{Name: "android-auto"})
	if err != nil {
		t.Fatalf("create instance with default image: %v", err)
	}
	if instance.ImageID == "" {
		t.Fatalf("expected image id to be populated")
	}
	images, err := db.ListImages(ctx)
	if err != nil {
		t.Fatalf("list images: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("images = %d, want 1", len(images))
	}
	if images[0].Name != "Default Cuttlefish image" {
		t.Fatalf("default image name = %q", images[0].Name)
	}
}

func TestGetOrCreateVersionImage(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(tempDir, "images"))

	db, err := OpenSQLite(filepath.Join(tempDir, "opencuttles.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	buildTarget := "aosp-android14-gsi/aosp_cf_x86_64_phone-userdebug"
	image, err := db.GetOrCreateVersionImage(ctx, "android14", "Android 14 (GSI)", buildTarget)
	if err != nil {
		t.Fatalf("get or create version image: %v", err)
	}
	if image.Status != domain.ImageStatusPending {
		t.Fatalf("status = %q, want pending", image.Status)
	}
	if image.BuildTarget != buildTarget || image.VersionID != "android14" {
		t.Fatalf("build target/version mismatch: %+v", image)
	}

	again, err := db.GetOrCreateVersionImage(ctx, "android14", "Android 14 (GSI)", buildTarget)
	if err != nil {
		t.Fatalf("get or create version image (again): %v", err)
	}
	if again.ID != image.ID {
		t.Fatalf("expected same image id, got %q and %q", image.ID, again.ID)
	}

	if err := db.UpdateImageStatus(ctx, image.ID, domain.ImageStatusReady, 1234, ""); err != nil {
		t.Fatalf("update image status: %v", err)
	}
	ready, err := db.GetImage(ctx, image.ID)
	if err != nil {
		t.Fatalf("get image: %v", err)
	}
	if ready.Status != domain.ImageStatusReady || ready.SizeBytes != 1234 {
		t.Fatalf("image not marked ready: %+v", ready)
	}
}

func TestGetOrCreateVersionImageRefreshesBuildTarget(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(tempDir, "images"))

	db, err := OpenSQLite(filepath.Join(tempDir, "opencuttles.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	// Simulate a stale row from an older catalog that stored only the branch.
	stale, err := db.GetOrCreateVersionImage(ctx, "android15", "Android 15 (GSI)", "aosp-android15-gsi")
	if err != nil {
		t.Fatalf("seed stale image: %v", err)
	}
	if err := db.UpdateImageStatus(ctx, stale.ID, domain.ImageStatusReady, 999, "stale"); err != nil {
		t.Fatalf("mark stale ready: %v", err)
	}

	// A later deploy resolves the corrected "branch/build_target" from the
	// catalog; the same row must be reused but refreshed and reset to pending.
	want := "aosp-android15-gsi/aosp_cf_x86_64_only_phone-userdebug"
	healed, err := db.GetOrCreateVersionImage(ctx, "android15", "Android 15 (GSI)", want)
	if err != nil {
		t.Fatalf("heal image: %v", err)
	}
	if healed.ID != stale.ID {
		t.Fatalf("expected same row, got %q and %q", stale.ID, healed.ID)
	}
	if healed.BuildTarget != want {
		t.Fatalf("build target = %q, want %q", healed.BuildTarget, want)
	}
	if healed.Status != domain.ImageStatusPending {
		t.Fatalf("status = %q, want pending so it re-fetches", healed.Status)
	}
	if healed.LastError != "" {
		t.Fatalf("last error = %q, want cleared", healed.LastError)
	}
}

func TestCreateInstanceDeployFields(t *testing.T) {
	ctx := context.Background()
	tempDir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(tempDir, "images"))

	db, err := OpenSQLite(filepath.Join(tempDir, "opencuttles.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	image, err := db.CreateImage(ctx, domain.CreateImageRequest{Name: "AOSP", Path: filepath.Join(tempDir, "img")})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	instance, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{
		Name:           "android-01",
		ImageID:        image.ID,
		AndroidVersion: "android14",
		DisplayWidth:   1080,
		DisplayHeight:  1920,
		DPI:            440,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}
	if instance.AndroidVersion != "android14" {
		t.Fatalf("android version = %q", instance.AndroidVersion)
	}
	if instance.DisplayWidth != 1080 || instance.DisplayHeight != 1920 || instance.DPI != 440 {
		t.Fatalf("display options not persisted: %+v", instance)
	}
	if instance.DeviceID != "cvd_1-1-1" {
		t.Fatalf("device id = %q, want cvd_1-1-1", instance.DeviceID)
	}

	// Round-trip through the store to confirm scanning of the new columns.
	loaded, err := db.GetInstance(ctx, instance.ID)
	if err != nil {
		t.Fatalf("get instance: %v", err)
	}
	if loaded.DisplayWidth != 1080 || loaded.DeviceID != "cvd_1-1-1" || loaded.AndroidVersion != "android14" {
		t.Fatalf("loaded instance mismatch: %+v", loaded)
	}
}
