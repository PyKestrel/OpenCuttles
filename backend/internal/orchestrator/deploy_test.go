package orchestrator

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// recordingRunner captures every command it is asked to run so tests can assert
// the exact CLI invocations the orchestrator produces.
type recordingRunner struct {
	mu      sync.Mutex
	calls   []CommandResult
	paths   map[string]string
	outputs map[string]string
}

func (r *recordingRunner) Run(_ context.Context, command string, args ...string) (CommandResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	result := CommandResult{Command: command, Args: args}
	if r.outputs != nil {
		if out, ok := r.outputs[command]; ok {
			result.Output = out
		}
	}
	r.calls = append(r.calls, result)
	return result, nil
}

func (r *recordingRunner) LookPath(command string) (string, error) {
	if path, ok := r.paths[command]; ok {
		return path, nil
	}
	return "", errors.New("not found")
}

func (r *recordingRunner) find(command string) (CommandResult, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, call := range r.calls {
		if call.Command == command {
			return call, true
		}
	}
	return CommandResult{}, false
}

func hasArgContaining(args []string, substr string) bool {
	for _, arg := range args {
		if strings.Contains(arg, substr) {
			return true
		}
	}
	return false
}

func TestEnsureImageFetches(t *testing.T) {
	t.Setenv("OPENCUTTLES_EXECUTE_CVD", "1")
	tempDir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(tempDir, "images"))

	ctx := context.Background()
	db := openTestStore(t)
	runner := &recordingRunner{}
	service := NewService(db, runner, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	buildTarget := "aosp-android14-gsi/aosp_cf_x86_64_phone-userdebug"
	image, err := db.GetOrCreateVersionImage(ctx, "android14", "Android 14 (GSI)", buildTarget)
	if err != nil {
		t.Fatalf("get or create version image: %v", err)
	}

	if err := service.ensureImage(ctx, image); err != nil {
		t.Fatalf("ensure image: %v", err)
	}

	fetch, ok := runner.find("cvd")
	if !ok {
		t.Fatalf("expected a cvd invocation, got %+v", runner.calls)
	}
	if len(fetch.Args) == 0 || fetch.Args[0] != "fetch" {
		t.Fatalf("expected cvd fetch, got args %v", fetch.Args)
	}
	if !hasArgContaining(fetch.Args, "--default_build="+buildTarget) {
		t.Fatalf("missing default_build arg: %v", fetch.Args)
	}
	if !hasArgContaining(fetch.Args, "--target_directory="+image.Path) {
		t.Fatalf("missing target_directory arg: %v", fetch.Args)
	}

	updated, err := db.GetImage(ctx, image.ID)
	if err != nil {
		t.Fatalf("get image: %v", err)
	}
	if updated.Status != domain.ImageStatusReady {
		t.Fatalf("image status = %q, want ready", updated.Status)
	}
}

func TestLaunchPassesDisplayFlags(t *testing.T) {
	t.Setenv("OPENCUTTLES_EXECUTE_CVD", "1")
	tempDir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(tempDir, "images"))
	t.Setenv("OPENCUTTLES_INSTANCE_ROOT", filepath.Join(tempDir, "instances"))

	imageDir := filepath.Join(tempDir, "img")
	if err := os.MkdirAll(imageDir, 0o750); err != nil {
		t.Fatalf("mkdir image: %v", err)
	}

	ctx := context.Background()
	db := openTestStore(t)
	runner := &recordingRunner{}
	service := NewService(db, runner, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	image, err := db.CreateImage(ctx, domain.CreateImageRequest{Name: "AOSP", Path: imageDir})
	if err != nil {
		t.Fatalf("create image: %v", err)
	}
	instance, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{
		Name:          "android-01",
		ImageID:       image.ID,
		DisplayWidth:  1080,
		DisplayHeight: 1920,
		DPI:           440,
	})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	if err := service.launch(ctx, instance); err != nil {
		t.Fatalf("launch: %v", err)
	}

	start, ok := runner.find("cvd")
	if !ok {
		t.Fatalf("expected a cvd invocation, got %+v", runner.calls)
	}
	if len(start.Args) == 0 || start.Args[0] != "start" {
		t.Fatalf("expected cvd start, got %v", start.Args)
	}
	for _, want := range []string{"--start_webrtc=true", "--x_res=1080", "--y_res=1920", "--dpi=440", "--base_instance_num=1"} {
		if !hasArgContaining(start.Args, want) {
			t.Fatalf("missing %q in launch args %v", want, start.Args)
		}
	}
	if hasArgContaining(start.Args, "--webrtc_port") {
		t.Fatalf("did not expect per-instance webrtc_port flag: %v", start.Args)
	}
}

func TestDeployDryRunReachesRunning(t *testing.T) {
	// No OPENCUTTLES_EXECUTE_CVD: dry-run path should orchestrate to running
	// without touching the host network or cvd binaries.
	tempDir := t.TempDir()
	t.Setenv("OPENCUTTLES_IMAGE_ROOT", filepath.Join(tempDir, "images"))

	ctx := context.Background()
	db := openTestStore(t)
	runner := &recordingRunner{}
	service := NewService(db, runner, slog.New(slog.NewTextHandler(os.Stdout, nil)))

	image, err := db.GetOrCreateVersionImage(ctx, "android14", "Android 14 (GSI)", "aosp-android14-gsi/aosp_cf_x86_64_phone-userdebug")
	if err != nil {
		t.Fatalf("version image: %v", err)
	}
	instance, err := db.CreateInstance(ctx, domain.CreateInstanceRequest{Name: "android-01", ImageID: image.ID})
	if err != nil {
		t.Fatalf("create instance: %v", err)
	}

	if _, err := service.Deploy(ctx, instance.ID); err != nil {
		t.Fatalf("deploy: %v", err)
	}

	waitForState(t, db, instance.ID, domain.StateRunning)

	readyImage, err := db.GetImage(ctx, image.ID)
	if err != nil {
		t.Fatalf("get image: %v", err)
	}
	if readyImage.Status != domain.ImageStatusReady {
		t.Fatalf("image status = %q, want ready", readyImage.Status)
	}
}
