package devicecontrol

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// scriptedRunner captures every adb invocation and can fail on demand, so the
// retry and gating behavior can be asserted without a device.
type scriptedRunner struct {
	mu    sync.Mutex
	calls [][]string
	// failUntilConnect makes device-scoped calls fail with "device not found"
	// until an `adb connect` has been seen, mimicking an unregistered TCP
	// transport.
	failUntilConnect bool
	connected        bool
}

func (r *scriptedRunner) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{name}, args...))

	if len(args) > 0 && args[0] == "connect" {
		r.connected = true
		return []byte("connected"), nil
	}
	if r.failUntilConnect && !r.connected {
		return []byte("error: device '192.168.1.42:5555' not found"), errStub
	}
	return []byte("ok"), nil
}

func (r *scriptedRunner) invocations() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, len(r.calls))
	copy(out, r.calls)
	return out
}

type stubErr struct{}

func (stubErr) Error() string { return "exit status 1" }

var errStub = stubErr{}

// A physical device addressed over TCP needs `adb connect` before it can be
// driven, and that registration is lost whenever the adb server restarts.
// Without the retry, a perfectly reachable phone reports "device not found".
func TestADBReconnectsTCPTargets(t *testing.T) {
	runner := &scriptedRunner{failUntilConnect: true}
	svc := &Service{runner: runner, execute: false}

	phone := domain.Instance{
		ID: "phy_1", Src: domain.SourcePhysical,
		State: domain.StateOnline, ADBTarget: "192.168.1.42:5555",
	}
	out, err := svc.adb(t.Context(), phone, "shell", "echo", "hi")
	if err != nil {
		t.Fatalf("adb should have recovered via connect: %v", err)
	}
	if string(out) != "ok" {
		t.Fatalf("output = %q", out)
	}

	calls := runner.invocations()
	if len(calls) != 3 {
		t.Fatalf("expected try / connect / retry, got %d calls: %v", len(calls), calls)
	}
	if calls[1][1] != "connect" || calls[1][2] != "192.168.1.42:5555" {
		t.Fatalf("second call should be `adb connect <target>`, got %v", calls[1])
	}
}

// A USB serial is not a TCP transport; `adb connect` is meaningless for it and
// must not be attempted.
func TestADBDoesNotConnectUSBSerials(t *testing.T) {
	runner := &scriptedRunner{failUntilConnect: true}
	svc := &Service{runner: runner}

	phone := domain.Instance{
		ID: "phy_1", Src: domain.SourcePhysical,
		State: domain.StateOnline, ADBTarget: "R5CT30ABCDE",
	}
	if _, err := svc.adb(t.Context(), phone, "shell", "true"); err == nil {
		t.Fatal("expected the underlying failure to surface")
	}
	for _, call := range runner.invocations() {
		if len(call) > 1 && call[1] == "connect" {
			t.Fatalf("adb connect was attempted for a USB serial: %v", call)
		}
	}
}

// The risk most likely to produce an angry user: this setting is persistent and
// weakens Play Protect until someone turns it back on. Fine for a disposable VM,
// not for a person's handset.
func TestPlayProtectIsOnlyDisabledForProvisionedDevices(t *testing.T) {
	sawVerifierSettings := func(runner *scriptedRunner) bool {
		for _, call := range runner.invocations() {
			if strings.Contains(strings.Join(call, " "), "verifier_verify_adb_installs") {
				return true
			}
		}
		return false
	}

	t.Run("cuttlefish VM: disabled as before", func(t *testing.T) {
		runner := &scriptedRunner{}
		svc := &Service{runner: runner, logger: discardLogger()}
		svc.disableInstallVerification(context.Background(),
			domain.Instance{ID: "cvd_1", ADBPort: 6520})
		if !sawVerifierSettings(runner) {
			t.Fatal("a provisioned VM should still have verification disabled")
		}
	})

	t.Run("physical handset: left alone", func(t *testing.T) {
		runner := &scriptedRunner{}
		svc := &Service{runner: runner, logger: discardLogger()}
		svc.disableInstallVerification(context.Background(),
			domain.Instance{ID: "phy_1", Src: domain.SourcePhysical, ADBTarget: "R5CT30ABCDE"})
		if sawVerifierSettings(runner) {
			t.Fatal("Play Protect was persistently weakened on a real device")
		}
	})
}

// OpenApp and InstallDesktopBuild go over the runner tunnel. They used to call
// through unconditionally after resolve, so a physical handset would have failed
// deep inside the tunnel rather than with a usable message.
func TestRunnerOnlyOperationsRejectPhysicalDevices(t *testing.T) {
	phone := domain.Instance{
		ID: "phy_1", Src: domain.SourcePhysical,
		State: domain.StateOnline, ADBTarget: "R5CT30ABCDE",
	}
	svc := &Service{runner: &scriptedRunner{}, store: stubStore{inst: phone}}

	if _, err := svc.OpenApp(t.Context(), phone.ID, "Notepad"); err == nil ||
		!strings.Contains(err.Error(), "launch_app") {
		t.Fatalf("OpenApp on a handset should redirect to launch_app, got %v", err)
	}
	if err := svc.InstallDesktopBuild(t.Context(), phone.ID, "b1", "app.msi", ""); err == nil ||
		!strings.Contains(err.Error(), "APK") {
		t.Fatalf("desktop install on a handset should redirect to APK install, got %v", err)
	}
}

type stubStore struct{ inst domain.Instance }

func (s stubStore) GetInstance(ctx context.Context, id string) (domain.Instance, error) {
	return s.inst, nil
}

// discardLogger keeps best-effort debug logging from polluting test output.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
