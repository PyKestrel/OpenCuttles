package devicewatch

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

type stateChange struct {
	id      string
	state   string
	message string
}

type fakeStore struct {
	mu       sync.Mutex
	devices  []domain.Instance
	changes  []stateChange
	displays map[string][3]int
}

func (f *fakeStore) ListPhysicalAndroid(ctx context.Context) ([]domain.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.Instance, len(f.devices))
	copy(out, f.devices)
	return out, nil
}

func (f *fakeStore) UpdateInstanceState(ctx context.Context, id, state, message string) (domain.Instance, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.changes = append(f.changes, stateChange{id: id, state: state, message: message})
	for i := range f.devices {
		if f.devices[i].ID == id {
			f.devices[i].State = state
			f.devices[i].LastError = message
		}
	}
	return domain.Instance{}, nil
}

func (f *fakeStore) UpdateInstanceDisplay(ctx context.Context, id string, w, h, dpi int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.displays == nil {
		f.displays = map[string][3]int{}
	}
	f.displays[id] = [3]int{w, h, dpi}
	return nil
}

func (f *fakeStore) recorded() []stateChange {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]stateChange, len(f.changes))
	copy(out, f.changes)
	return out
}

// fakeRunner answers adb invocations from a scripted table.
type fakeRunner struct {
	mu         sync.Mutex
	devicesOut string
	calls      [][]string
	sizeOut    string
	densityOut string
}

func (r *fakeRunner) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, append([]string{name}, args...))

	joined := strings.Join(args, " ")
	switch {
	case strings.HasPrefix(joined, "devices"):
		return []byte(r.devicesOut), nil
	case strings.Contains(joined, "wm size"):
		return []byte(r.sizeOut), nil
	case strings.Contains(joined, "wm density"):
		return []byte(r.densityOut), nil
	default:
		return []byte(""), nil
	}
}

func (r *fakeRunner) invocations() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]string, len(r.calls))
	copy(out, r.calls)
	return out
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestPollBringsAVisibleDeviceOnline(t *testing.T) {
	store := &fakeStore{devices: []domain.Instance{{
		ID: "phy_1", Src: domain.SourcePhysical,
		ADBTarget: "R5CT30ABCDE", State: domain.StateOffline,
	}}}
	runner := &fakeRunner{
		devicesOut: "List of devices attached\nR5CT30ABCDE\tdevice usb:1-3\n",
		sizeOut:    "Physical size: 1080x2400",
		densityOut: "Physical density: 420",
	}

	New(store, runner, quietLogger(), 0).pollOnce(context.Background())

	changes := store.recorded()
	if len(changes) != 1 || changes[0].state != domain.StateOnline {
		t.Fatalf("expected one transition to online, got %+v", changes)
	}
	// Real geometry must be persisted: the ADB driver otherwise falls back to a
	// hardcoded 720x1280 and every tap lands in the wrong place.
	if got := store.displays["phy_1"]; got != [3]int{1080, 2400, 420} {
		t.Fatalf("display = %v, want [1080 2400 420]", got)
	}
}

// The single most common real-world failure. Reporting it as plain "offline"
// sends someone hunting for a cable problem instead of looking at the phone.
func TestUnauthorizedIsReportedDistinctly(t *testing.T) {
	store := &fakeStore{devices: []domain.Instance{{
		ID: "phy_1", Src: domain.SourcePhysical,
		ADBTarget: "R5CT30ABCDE", State: domain.StateOffline,
	}}}
	runner := &fakeRunner{devicesOut: "List of devices attached\nR5CT30ABCDE\tunauthorized usb:1-3\n"}

	New(store, runner, quietLogger(), 0).pollOnce(context.Background())

	changes := store.recorded()
	if len(changes) != 1 {
		t.Fatalf("expected one change, got %+v", changes)
	}
	if changes[0].state != domain.StateOffline {
		t.Fatalf("state = %q, want offline", changes[0].state)
	}
	if !strings.Contains(changes[0].message, "not authorized") {
		t.Fatalf("message should explain the prompt, got %q", changes[0].message)
	}
}

func TestNoPermissionsIsReportedDistinctly(t *testing.T) {
	store := &fakeStore{devices: []domain.Instance{{
		ID: "phy_1", Src: domain.SourcePhysical,
		ADBTarget: "ABC123", State: domain.StateOffline,
	}}}
	runner := &fakeRunner{devicesOut: "List of devices attached\nABC123\tno permissions (udev)\n"}

	New(store, runner, quietLogger(), 0).pollOnce(context.Background())

	changes := store.recorded()
	if len(changes) != 1 || !strings.Contains(changes[0].message, "udev") {
		t.Fatalf("expected a udev-specific message, got %+v", changes)
	}
}

// A device that disappears goes offline.
func TestUnpluggedDeviceGoesOffline(t *testing.T) {
	store := &fakeStore{devices: []domain.Instance{{
		ID: "phy_1", Src: domain.SourcePhysical,
		ADBTarget: "R5CT30ABCDE", State: domain.StateOnline,
	}}}
	runner := &fakeRunner{devicesOut: "List of devices attached\n"}

	New(store, runner, quietLogger(), 0).pollOnce(context.Background())

	changes := store.recorded()
	if len(changes) != 1 || changes[0].state != domain.StateOffline {
		t.Fatalf("expected a transition to offline, got %+v", changes)
	}
}

// The poll runs every few seconds. Writing on every pass would churn
// updated_at forever and bury real changes in the audit trail.
func TestNoWriteWhenNothingChanged(t *testing.T) {
	store := &fakeStore{devices: []domain.Instance{{
		ID: "phy_1", Src: domain.SourcePhysical,
		ADBTarget: "R5CT30ABCDE", State: domain.StateOnline,
	}}}
	runner := &fakeRunner{
		devicesOut: "List of devices attached\nR5CT30ABCDE\tdevice usb:1-3\n",
		sizeOut:    "Physical size: 1080x2400",
	}

	w := New(store, runner, quietLogger(), 0)
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())
	w.pollOnce(context.Background())

	if got := store.recorded(); len(got) != 0 {
		t.Fatalf("a steady device produced %d writes: %+v", len(got), got)
	}
}

// A TCP target only appears in `adb devices` after a connect, and that
// registration is lost whenever the adb server restarts.
func TestTCPTargetsAreReconnected(t *testing.T) {
	store := &fakeStore{devices: []domain.Instance{{
		ID: "phy_1", Src: domain.SourcePhysical,
		ADBTarget: "192.168.1.42:5555", State: domain.StateOffline,
	}}}
	runner := &fakeRunner{devicesOut: "List of devices attached\n"}

	New(store, runner, quietLogger(), 0).pollOnce(context.Background())

	sawConnect := false
	for _, call := range runner.invocations() {
		if len(call) >= 3 && call[1] == "connect" && call[2] == "192.168.1.42:5555" {
			sawConnect = true
		}
	}
	if !sawConnect {
		t.Fatalf("no adb connect was attempted for a missing TCP target: %v", runner.invocations())
	}
}

// USB serials are not TCP transports; connect is meaningless for them.
func TestUSBSerialsAreNotReconnected(t *testing.T) {
	store := &fakeStore{devices: []domain.Instance{{
		ID: "phy_1", Src: domain.SourcePhysical,
		ADBTarget: "R5CT30ABCDE", State: domain.StateOffline,
	}}}
	runner := &fakeRunner{devicesOut: "List of devices attached\n"}

	New(store, runner, quietLogger(), 0).pollOnce(context.Background())

	for _, call := range runner.invocations() {
		if len(call) >= 2 && call[1] == "connect" {
			t.Fatalf("adb connect attempted for a USB serial: %v", call)
		}
	}
}

// Never on by accident: an appliance quietly driving a developer's phone is a
// worse failure than the feature not running.
func TestDisabledByDefault(t *testing.T) {
	t.Setenv("OPENCUTTLES_WATCH_PHYSICAL_DEVICES", "")
	if Enabled() {
		t.Fatal("the watcher should be off unless explicitly enabled")
	}
	t.Setenv("OPENCUTTLES_WATCH_PHYSICAL_DEVICES", "1")
	if !Enabled() {
		t.Fatal("the watcher should be on when explicitly enabled")
	}

	// Run must return immediately when disabled rather than spinning.
	t.Setenv("OPENCUTTLES_WATCH_PHYSICAL_DEVICES", "")
	store := &fakeStore{devices: []domain.Instance{{ID: "phy_1", ADBTarget: "X"}}}
	runner := &fakeRunner{}
	New(store, runner, quietLogger(), 0).Run(context.Background())
	if len(runner.invocations()) != 0 {
		t.Fatal("a disabled watcher touched adb")
	}
}

// Nothing registered means nothing to do — in particular, no adb invocation, so
// an appliance with no physical devices never shells out.
func TestNoDevicesMeansNoADB(t *testing.T) {
	store := &fakeStore{}
	runner := &fakeRunner{}
	New(store, runner, quietLogger(), 0).pollOnce(context.Background())
	if len(runner.invocations()) != 0 {
		t.Fatalf("adb was invoked with no devices registered: %v", runner.invocations())
	}
}
