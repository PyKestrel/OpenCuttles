// Package devicecontrol provides interactive control of a running Cuttlefish
// Android instance over ADB: screenshots, input injection (tap/swipe/type/keys),
// the uiautomator accessibility tree, app management, shell, and performance
// snapshots. It is the foundation for the manual control UI, the MCP tool
// surface, and the automated test runner.
//
// Every operation targets an instance by ID, resolves its ADB port from the
// store, and shells out to `adb -s 127.0.0.1:<port> ...`, mirroring the pattern
// already used for boot-readiness polling in the orchestrator. Interactive
// control requires real execution (OPENCUTTLES_EXECUTE_CVD=1); in dry-run mode
// operations return ErrExecutionDisabled so callers can surface a clear message.
package devicecontrol

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// ErrExecutionDisabled is returned when device control is attempted while
// OPENCUTTLES_EXECUTE_CVD is not set to "1" (dry-run mode). There is no real
// device to talk to, so callers should map this to an unavailable response.
var ErrExecutionDisabled = errors.New("device control requires OPENCUTTLES_EXECUTE_CVD=1")

// ErrNotRunning is returned when the target instance is not in the running
// state and therefore cannot accept input or produce a screenshot.
var ErrNotRunning = errors.New("instance is not running")

// InstanceStore is the subset of the store needed to resolve an instance's ADB
// endpoint. *store.SQLite satisfies it.
type InstanceStore interface {
	GetInstance(ctx context.Context, id string) (domain.Instance, error)
}

// CommandRunner executes a single command and returns its raw stdout. Keeping
// stdout as bytes (rather than a trimmed string) matters for binary payloads
// such as `screencap -p` PNG output and `adb pull` file contents.
type CommandRunner interface {
	Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)
}

// Service performs control operations against instances, dispatching to an ADB
// driver (Android) or a tunnel-backed MCP driver (desktop targets).
type Service struct {
	store   InstanceStore
	runner  CommandRunner
	runners RunnerCaller // desktop tunnel; nil until wired by the API server
	logger  *slog.Logger
	execute bool
}

// SetRunners injects the desktop runner tunnel (the runnerhub). Called after
// construction so NewService's signature stays stable for existing callers/tests.
func (s *Service) SetRunners(r RunnerCaller) {
	s.runners = r
}

// NewService builds a device control service. When runner is nil the default
// exec-based runner is used. execute mirrors OPENCUTTLES_EXECUTE_CVD; pass the
// resolved value so tests can force either mode.
func NewService(store InstanceStore, runner CommandRunner, logger *slog.Logger) *Service {
	if runner == nil {
		runner = ExecRunner{logger: logger}
	}
	return &Service{
		store:   store,
		runner:  runner,
		logger:  logger,
		execute: os.Getenv("OPENCUTTLES_EXECUTE_CVD") == "1",
	}
}

// resolve loads the instance and validates it is controllable. Android requires
// a running Cuttlefish VM with an ADB port and real execution enabled; a desktop
// target requires a registered control endpoint and an online/running state (the
// OPENCUTTLES_EXECUTE_CVD gate is Cuttlefish-only and does not apply to it).
func (s *Service) resolve(ctx context.Context, id string) (domain.Instance, error) {
	instance, err := s.store.GetInstance(ctx, id)
	if err != nil {
		return domain.Instance{}, err
	}
	if isAndroid(instance.Platform) {
		if !s.execute {
			return domain.Instance{}, ErrExecutionDisabled
		}
		if instance.State != domain.StateRunning {
			return domain.Instance{}, ErrNotRunning
		}
		if instance.ADBPort == 0 {
			return domain.Instance{}, fmt.Errorf("instance %s has no ADB port", id)
		}
		return instance, nil
	}
	// Desktop target.
	if instance.ControlEndpoint == "" {
		return domain.Instance{}, fmt.Errorf("device %s has no control endpoint", id)
	}
	if instance.State != domain.StateOnline && instance.State != domain.StateRunning {
		return domain.Instance{}, ErrNotRunning
	}
	return instance, nil
}

// driverFor returns the control driver for an instance's platform: ADB for
// Android, or the tunnel-backed MCP driver for a desktop target whose runner is
// connected.
func (s *Service) driverFor(inst domain.Instance) (Driver, error) {
	if isAndroid(inst.Platform) {
		return adbDriver{svc: s, inst: inst}, nil
	}
	if s.runners == nil {
		return nil, fmt.Errorf("%w: desktop control tunnel is not configured", ErrUnsupported)
	}
	if !s.runners.Online(inst.ID) {
		return nil, fmt.Errorf("device %s is offline (no runner connected)", inst.ID)
	}
	return mcpDriver{runners: s.runners, inst: inst}, nil
}

// Capabilities reports which optional operations a device supports.
func (s *Service) Capabilities(ctx context.Context, id string) (Capabilities, error) {
	inst, err := s.store.GetInstance(ctx, id)
	if err != nil {
		return Capabilities{}, err
	}
	drv, err := s.driverFor(inst)
	if err != nil {
		return Capabilities{}, err
	}
	return drv.Capabilities(), nil
}


// adb runs an adb command scoped to the instance's transport and returns raw
// stdout. args should not include the leading "-s <target>". ADB is Android-only;
// desktop targets that reach an ADB-only operation are rejected here so they get
// a clear message instead of a malformed transport.
func (s *Service) adb(ctx context.Context, instance domain.Instance, args ...string) ([]byte, error) {
	if !isAndroid(instance.Platform) {
		return nil, fmt.Errorf("%w: ADB operations are Android-only", ErrUnsupported)
	}
	target := fmt.Sprintf("127.0.0.1:%d", instance.ADBPort)
	full := append([]string{"-s", target}, args...)
	out, err := s.runner.Run(ctx, nil, "adb", full...)
	if err != nil {
		return out, fmt.Errorf("adb %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// adbShell runs `adb ... shell <args>` and returns trimmed text output.
func (s *Service) adbShell(ctx context.Context, instance domain.Instance, args ...string) (string, error) {
	out, err := s.adb(ctx, instance, append([]string{"shell"}, args...)...)
	return strings.TrimSpace(string(out)), err
}

// Screenshot captures the current screen as PNG bytes.
func (s *Service) Screenshot(ctx context.Context, id string) ([]byte, error) {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return nil, err
	}
	drv, err := s.driverFor(instance)
	if err != nil {
		return nil, err
	}
	return drv.Screenshot(ctx)
}

// Tap injects a single tap at the given screen coordinates (pixels).
func (s *Service) Tap(ctx context.Context, id string, x, y int) error {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return err
	}
	drv, err := s.driverFor(instance)
	if err != nil {
		return err
	}
	return drv.Tap(ctx, x, y)
}

// Swipe drags from (x1,y1) to (x2,y2) over durationMs milliseconds. A duration
// of 0 lets the platform pick its default. A swipe to the same point with a long
// duration acts as a long press.
func (s *Service) Swipe(ctx context.Context, id string, x1, y1, x2, y2, durationMs int) error {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return err
	}
	drv, err := s.driverFor(instance)
	if err != nil {
		return err
	}
	return drv.Swipe(ctx, x1, y1, x2, y2, durationMs)
}

// LongPress presses and holds at (x,y) for durationMs (default 800ms).
func (s *Service) LongPress(ctx context.Context, id string, x, y, durationMs int) error {
	if durationMs <= 0 {
		durationMs = 800
	}
	return s.Swipe(ctx, id, x, y, x, y, durationMs)
}

// Text types a UTF-8 string.
func (s *Service) Text(ctx context.Context, id, text string) error {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return err
	}
	drv, err := s.driverFor(instance)
	if err != nil {
		return err
	}
	return drv.Text(ctx, text)
}

// Key sends a key event. keycode may be a numeric code or a key name such as
// HOME, BACK, ENTER, APP_SWITCH (recents), VOLUME_UP, POWER (Android); desktop
// drivers map common names to their own key syntax.
func (s *Service) Key(ctx context.Context, id, keycode string) error {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return err
	}
	drv, err := s.driverFor(instance)
	if err != nil {
		return err
	}
	return drv.Key(ctx, keycode)
}

// UITree returns the current screen's accessibility hierarchy as a compact tree
// (resource-id, text, content-desc, class, bounds, clickability, center point).
// This is the primary "eyes" for a text-only agent.
func (s *Service) UITree(ctx context.Context, id string) (*UINode, error) {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return nil, err
	}
	out, err := s.adb(ctx, instance, "exec-out", "uiautomator", "dump", "/dev/tty")
	if err != nil {
		return nil, err
	}
	return ParseUIHierarchy(out)
}

// ListApps returns installed package names. thirdPartyOnly limits the result to
// user-installed apps (pm list packages -3).
func (s *Service) ListApps(ctx context.Context, id string, thirdPartyOnly bool) ([]string, error) {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return nil, err
	}
	if !isAndroid(instance.Platform) {
		var out struct {
			Apps []string `json:"apps"`
		}
		if err := s.callRunner(ctx, id, "list_apps", struct{}{}, &out); err != nil {
			return nil, err
		}
		return out.Apps, nil
	}
	args := []string{"pm", "list", "packages"}
	if thirdPartyOnly {
		args = append(args, "-3")
	}
	out, err := s.adbShell(ctx, instance, args...)
	if err != nil {
		return nil, err
	}
	var pkgs []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "package:"))
		if line != "" {
			pkgs = append(pkgs, line)
		}
	}
	return pkgs, nil
}

// LaunchApp starts an app's launcher activity by package name.
func (s *Service) LaunchApp(ctx context.Context, id, pkg string) error {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.adbShell(ctx, instance, "monkey", "-p", pkg, "-c", "android.intent.category.LAUNCHER", "1")
	return err
}

// CurrentActivity returns the package/activity (Android) or the foreground window
// title (desktop) that currently has focus.
func (s *Service) CurrentActivity(ctx context.Context, id string) (string, error) {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return "", err
	}
	if !isAndroid(instance.Platform) {
		var out struct {
			Activity string `json:"activity"`
		}
		if err := s.callRunner(ctx, id, "current_activity", struct{}{}, &out); err != nil {
			return "", err
		}
		return out.Activity, nil
	}
	out, err := s.adbShell(ctx, instance, "dumpsys", "activity", "activities")
	if err != nil {
		return "", err
	}
	return parseResumedActivity(out), nil
}

// OpenApp launches an app by its display name on a desktop target (the runner
// resolves it against the Start menu). Android app-opening goes through
// LaunchApp with a resolved package instead.
func (s *Service) OpenApp(ctx context.Context, id, name string) error {
	if _, err := s.resolve(ctx, id); err != nil {
		return err
	}
	return s.callRunner(ctx, id, "open_app", map[string]string{"name": name}, nil)
}

// callRunner sends a method to a desktop device's runner over the tunnel and, if
// out is non-nil, decodes the JSON result into it.
func (s *Service) callRunner(ctx context.Context, id, method string, params, out any) error {
	if s.runners == nil {
		return fmt.Errorf("%w: desktop control tunnel is not configured", ErrUnsupported)
	}
	raw, err := s.runners.Call(ctx, id, method, params)
	if err != nil {
		return err
	}
	if out != nil && len(raw) > 0 {
		return json.Unmarshal(raw, out)
	}
	return nil
}

// InstallAPK installs an APK from a path on the OpenCuttles host, replacing any
// existing install (-r).
func (s *Service) InstallAPK(ctx context.Context, id, hostPath string) error {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return err
	}
	_, err = s.adb(ctx, instance, "install", "-r", hostPath)
	return err
}

// Rotate sets a fixed display orientation (0,1,2,3 = 0/90/180/270 degrees) and
// disables auto-rotation so the orientation sticks.
func (s *Service) Rotate(ctx context.Context, id string, orientation int) error {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return err
	}
	if orientation < 0 || orientation > 3 {
		return fmt.Errorf("orientation must be 0..3")
	}
	if _, err := s.adbShell(ctx, instance, "settings", "put", "system", "accelerometer_rotation", "0"); err != nil {
		return err
	}
	_, err = s.adbShell(ctx, instance, "settings", "put", "system", "user_rotation", strconv.Itoa(orientation))
	return err
}

// Shell runs an arbitrary shell command on the device and returns its output.
func (s *Service) Shell(ctx context.Context, id, command string) (string, error) {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return "", err
	}
	return s.adbShell(ctx, instance, command)
}

// Logcat returns a snapshot of the most recent log lines (adb logcat -d -t N).
// Live streaming is layered on top of this in a later phase.
func (s *Service) Logcat(ctx context.Context, id string, lines int) (string, error) {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return "", err
	}
	if lines <= 0 || lines > 5000 {
		lines = 500
	}
	return s.adbShell(ctx, instance, "logcat", "-d", "-t", strconv.Itoa(lines))
}

// recordPath is where screenrecord writes on the device before being pulled.
const recordPath = "/sdcard/opencuttles-rec.mp4"

// StartRecording begins an on-device screen recording in the background. The
// adb process is started detached (shell nohup) so the API call returns
// immediately; StopRecording signals it and pulls the file.
func (s *Service) StartRecording(ctx context.Context, id string) error {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return err
	}
	// Kill any stale recorder, then start a fresh one detached. --time-limit
	// caps a runaway recording at screenrecord's 3-minute maximum.
	_, _ = s.adbShell(ctx, instance, "pkill", "-INT", "screenrecord")
	_, err = s.adbShell(ctx, instance, "nohup", "screenrecord", "--time-limit", "180", recordPath, ">/dev/null", "2>&1", "&")
	return err
}

// StopRecording ends the recording and returns the MP4 bytes. screenrecord
// finalizes the file on SIGINT, so a short settle follows the signal.
func (s *Service) StopRecording(ctx context.Context, id string) ([]byte, error) {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return nil, err
	}
	_, _ = s.adbShell(ctx, instance, "pkill", "-INT", "screenrecord")
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(2 * time.Second):
	}
	video, err := s.adb(ctx, instance, "exec-out", "cat", recordPath)
	if err != nil {
		return nil, err
	}
	_, _ = s.adbShell(ctx, instance, "rm", "-f", recordPath)
	if len(video) < 1024 {
		return nil, fmt.Errorf("recording too small (%d bytes); screenrecord may not have started", len(video))
	}
	return video, nil
}

// Perf collects a lightweight performance snapshot. When pkg is empty, only
// system-wide battery/memory summaries are gathered.
func (s *Service) Perf(ctx context.Context, id, pkg string) (PerfSnapshot, error) {
	instance, err := s.resolve(ctx, id)
	if err != nil {
		return PerfSnapshot{}, err
	}
	snap := PerfSnapshot{Package: pkg}
	if battery, err := s.adbShell(ctx, instance, "dumpsys", "battery"); err == nil {
		snap.BatteryLevel = parseBatteryLevel(battery)
	}
	if pkg != "" {
		if mem, err := s.adbShell(ctx, instance, "dumpsys", "meminfo", pkg); err == nil {
			snap.TotalPSSKB = parseTotalPSS(mem)
		}
	}
	return snap, nil
}

// escapeInputText prepares a string for `input text`, which treats spaces
// specially and mishandles a few shell metacharacters.
func escapeInputText(text string) string {
	replacer := strings.NewReplacer(
		" ", "%s",
		"'", "\\'",
		"\"", "\\\"",
		"&", "\\&",
		"<", "\\<",
		">", "\\>",
		"(", "\\(",
		")", "\\)",
		"|", "\\|",
		";", "\\;",
	)
	return replacer.Replace(text)
}

// normalizeKeycode accepts a numeric code, a bare key name, or a KEYCODE_-
// prefixed name and returns the form `input keyevent` expects.
func normalizeKeycode(code string) string {
	code = strings.TrimSpace(code)
	if code == "" {
		return "KEYCODE_UNKNOWN"
	}
	if _, err := strconv.Atoi(code); err == nil {
		return code
	}
	upper := strings.ToUpper(code)
	if strings.HasPrefix(upper, "KEYCODE_") {
		return upper
	}
	return "KEYCODE_" + upper
}
