package orchestrator

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/store"
)

type fakeRunner struct {
	paths map[string]string
	err   error
}

func (f fakeRunner) Run(ctx context.Context, command string, args ...string) (CommandResult, error) {
	return CommandResult{Command: command, Args: args}, f.err
}

func (f fakeRunner) LookPath(command string) (string, error) {
	if path, ok := f.paths[command]; ok {
		return path, nil
	}
	return "", errors.New("not found")
}

func TestServiceLifecycleDryRun(t *testing.T) {
	ctx := context.Background()
	db := openTestStore(t)
	service := NewService(db, fakeRunner{}, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	image, err := db.CreateImage(ctx, domain.CreateImageRequest{Name: "AOSP", Path: "/var/lib/opencuttles/images/aosp"})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	instance, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{Name: "android-01", ImageID: image.ID})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	instance, operation, err := service.StartInstance(ctx, instance.ID)
	if err != nil {
		t.Fatalf("start instance: %v", err)
	}
	if instance.State != domain.StateStarting {
		t.Fatalf("state = %q, want %q", instance.State, domain.StateStarting)
	}
	if operation.Status != "running" {
		t.Fatalf("operation status = %q", operation.Status)
	}
	instance = waitForState(t, db, instance.ID, domain.StateRunning)

	instance, operation, err = service.StopInstance(ctx, instance.ID)
	if err != nil {
		t.Fatalf("stop instance: %v", err)
	}
	if instance.State != domain.StateStopping {
		t.Fatalf("state = %q, want %q", instance.State, domain.StateStopping)
	}
	if operation.Status != "running" {
		t.Fatalf("operation status = %q", operation.Status)
	}
	instance = waitForState(t, db, instance.ID, domain.StateStopped)

	operation, err = service.DeleteInstance(ctx, instance.ID)
	if err != nil {
		t.Fatalf("delete instance: %v", err)
	}
	if operation.Status != "succeeded" {
		t.Fatalf("operation status = %q", operation.Status)
	}
	if _, err := db.GetInstance(ctx, instance.ID); !store.IsNotFound(err) {
		t.Fatalf("deleted instance lookup error = %v, want not found", err)
	}
}

func waitForState(t *testing.T, db *store.SQLite, id, state string) domain.Instance {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		instance, err := db.GetInstance(context.Background(), id)
		if err == nil && instance.State == state {
			return instance
		}
		time.Sleep(10 * time.Millisecond)
	}
	instance, _ := db.GetInstance(context.Background(), id)
	t.Fatalf("state = %q, want %q", instance.State, state)
	return domain.Instance{}
}

func TestHostPrerequisites(t *testing.T) {
	db := openTestStore(t)
	service := NewService(db, fakeRunner{
		paths: map[string]string{
			"cvd": "/usr/bin/cvd",
			"adb": "/usr/bin/adb",
		},
	}, nil)

	host := service.Host(context.Background())
	if host.ID != "local" {
		t.Fatalf("host id = %q", host.ID)
	}
	if len(host.Prerequisites) != 4 {
		t.Fatalf("prerequisites = %d, want 4", len(host.Prerequisites))
	}
	if !host.Prerequisites[0].OK {
		t.Fatalf("first prerequisite should pass: %+v", host.Prerequisites[0])
	}
}

func openTestStore(t *testing.T) *store.SQLite {
	t.Helper()
	db, err := store.OpenSQLite(filepath.Join(t.TempDir(), "opencuttles.db"))
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}
