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
	if instance.ConsoleURL != "/api/v1/instances/"+instance.ID+"/console" {
		t.Fatalf("console url = %q", instance.ConsoleURL)
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
