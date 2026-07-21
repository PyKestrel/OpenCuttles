// Package devicewatch keeps registered physical Android devices' reachability
// up to date.
//
// Desktop runners report their own presence: the tunnel connecting and dropping
// is the signal. A physical handset has no such channel — it is simply plugged
// in or not — so something has to look. This polls `adb devices -l` and flips
// each registered device between online and offline, which is the same
// online/offline lifecycle desktops already use.
//
// Deliberately opt-in. Without a switch, running the API on a developer's laptop
// would start driving whatever phone happened to be plugged into it.
package devicewatch

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/devicecontrol"
	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

const (
	defaultInterval = 5 * time.Second
	// pollTimeout bounds one sweep. `adb devices` is normally instant, but a
	// wedged adb server can hang, and a stuck sweep must not stall the next one.
	pollTimeout = 20 * time.Second
)

// Store is the slice of persistence the watcher needs.
type Store interface {
	ListPhysicalAndroid(ctx context.Context) ([]domain.Instance, error)
	UpdateInstanceState(ctx context.Context, id, state, message string) (domain.Instance, error)
	UpdateInstanceDisplay(ctx context.Context, id string, width, height, dpi int) error
}

// Runner executes a command and returns its stdout.
type Runner interface {
	Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error)
}

// Watcher polls ADB for device reachability.
type Watcher struct {
	store    Store
	runner   Runner
	logger   *slog.Logger
	interval time.Duration
}

// New builds a watcher. interval <= 0 uses the default.
func New(store Store, runner Runner, logger *slog.Logger, interval time.Duration) *Watcher {
	if interval <= 0 {
		interval = defaultInterval
	}
	return &Watcher{store: store, runner: runner, logger: logger, interval: interval}
}

// Enabled reports whether the poller should run.
//
// Off unless OPENCUTTLES_WATCH_PHYSICAL_DEVICES=1. This is opt-in rather than
// "on when adb exists" because adb is present on plenty of machines that are
// not appliances, and the failure mode of guessing wrong is an appliance
// quietly taking over a developer's phone.
func Enabled() bool {
	return strings.TrimSpace(os.Getenv("OPENCUTTLES_WATCH_PHYSICAL_DEVICES")) == "1"
}

// Run polls until the context is cancelled.
func (w *Watcher) Run(ctx context.Context) {
	if !Enabled() {
		return
	}
	if w.logger != nil {
		w.logger.Info("watching physical devices", "interval", w.interval)
	}
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()

	w.pollOnce(ctx) // don't make the first device wait a full interval
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.pollOnce(ctx)
		}
	}
}

// pollOnce reconciles every registered physical device against what ADB sees.
func (w *Watcher) pollOnce(parent context.Context) {
	ctx, cancel := context.WithTimeout(parent, pollTimeout)
	defer cancel()

	devices, err := w.store.ListPhysicalAndroid(ctx)
	if err != nil {
		w.log("list physical devices failed", "error", err)
		return
	}
	if len(devices) == 0 {
		return
	}

	// A device reached over TCP only appears in `adb devices` after a connect,
	// and that registration is lost whenever the adb server restarts. Reconnect
	// before listing so a reachable device is not reported offline forever.
	seen := w.adbDevices(ctx)
	for _, d := range devices {
		target := strings.TrimSpace(d.ADBTarget)
		if target == "" || !strings.Contains(target, ":") {
			continue
		}
		if st, ok := seen[target]; !ok || st.State == StateOffline {
			// Best-effort: a device that is genuinely gone simply stays offline.
			_, _ = w.runner.Run(ctx, nil, "adb", "connect", target)
		}
	}
	if len(devices) > 0 {
		seen = w.adbDevices(ctx) // re-read after any reconnects
	}

	for _, device := range devices {
		w.reconcile(ctx, device, seen[strings.TrimSpace(device.ADBTarget)])
	}
}

// reconcile brings one device's stored state in line with what ADB reports.
func (w *Watcher) reconcile(ctx context.Context, device domain.Instance, status DeviceStatus) {
	wantState := domain.StateOffline
	message := ""

	switch status.State {
	case StateDevice:
		wantState = domain.StateOnline
	case StateUnauthorized:
		// By far the most common real-world failure: the phone is plugged in and
		// visible but the USB debugging prompt has not been accepted. Saying
		// "offline" would send someone hunting for a cable problem.
		message = "USB debugging is not authorized on the device — accept the prompt on its screen"
	case StateNoPermissions:
		message = "the appliance cannot access this device (udev rules or group membership)"
	case StateOffline:
		message = "device is offline"
	default:
		message = "device not visible to adb"
	}

	if device.State == wantState && device.LastError == message {
		return // nothing changed; don't churn updated_at every 5s
	}

	if _, err := w.store.UpdateInstanceState(ctx, device.ID, wantState, message); err != nil {
		w.log("update device state failed", "device", device.ID, "error", err)
		return
	}
	w.log("physical device state changed",
		"device", device.ID, "target", device.ADBTarget, "state", wantState, "detail", message)

	// On the way up, refresh the screen geometry. Re-read on every transition
	// rather than only at registration: display size is a setting users change,
	// and stale dimensions put taps in the wrong place.
	if wantState == domain.StateOnline {
		w.refreshDisplay(ctx, device)
	}
}

// refreshDisplay records the device's real input geometry. Best-effort — a
// device that will not answer is still usable for everything else.
func (w *Watcher) refreshDisplay(ctx context.Context, device domain.Instance) {
	target := strings.TrimSpace(device.ADBTarget)
	sizeOut, err := w.runner.Run(ctx, nil, "adb", "-s", target, "shell", "wm", "size")
	if err != nil {
		w.log("read display size failed", "device", device.ID, "error", err)
		return
	}
	width, height, err := devicecontrol.ParseWMSize(string(sizeOut))
	if err != nil {
		w.log("parse display size failed", "device", device.ID, "error", err)
		return
	}

	dpi := 0
	if densityOut, err := w.runner.Run(ctx, nil, "adb", "-s", target, "shell", "wm", "density"); err == nil {
		dpi = parseDensity(string(densityOut))
	}

	if err := w.store.UpdateInstanceDisplay(ctx, device.ID, width, height, dpi); err != nil {
		w.log("persist display size failed", "device", device.ID, "error", err)
	}
}

// adbDevices runs `adb devices -l` and returns what it saw, keyed by serial.
func (w *Watcher) adbDevices(ctx context.Context) map[string]DeviceStatus {
	out, err := w.runner.Run(ctx, nil, "adb", "devices", "-l")
	if err != nil {
		w.log("adb devices failed", "error", err)
		return map[string]DeviceStatus{}
	}
	return ParseADBDevices(string(out))
}

func (w *Watcher) log(msg string, args ...any) {
	if w.logger != nil {
		w.logger.Warn(msg, args...)
	}
}

// parseDensity reads `wm density`, preferring an override over the physical
// value for the same reason ParseWMSize does.
func parseDensity(out string) int {
	physical, override := 0, 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		value, ok := strings.CutPrefix(line, "Physical density:")
		if ok {
			physical, _ = strconv.Atoi(strings.TrimSpace(value))
			continue
		}
		if value, ok := strings.CutPrefix(line, "Override density:"); ok {
			override, _ = strconv.Atoi(strings.TrimSpace(value))
		}
	}
	if override > 0 {
		return override
	}
	return physical
}
