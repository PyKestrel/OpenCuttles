package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/devicewatch"
)

// The pick-list should surface what an operator is looking for first: a phone
// they just plugged in that is ready to register. Registered devices stay
// visible (so a search for "where is my phone" succeeds) but sink to the bottom.
func TestDiscoveredDeviceOrdering(t *testing.T) {
	devices := []discoveredDevice{
		{Serial: "zzz-registered", State: devicewatch.StateDevice, Registered: true, RegisteredAs: "lab-1"},
		{Serial: "bbb-unauthorized", State: devicewatch.StateUnauthorized},
		{Serial: "aaa-ready", State: devicewatch.StateDevice},
		{Serial: "ccc-ready", State: devicewatch.StateDevice},
		{Serial: "aaa-registered", State: devicewatch.StateDevice, Registered: true},
	}
	sortDiscovered(devices)

	got := make([]string, len(devices))
	for i, d := range devices {
		got[i] = d.Serial
	}
	want := []string{
		"aaa-ready", "ccc-ready", // unregistered and ready, by serial
		"bbb-unauthorized",                 // unregistered but not usable yet
		"aaa-registered", "zzz-registered", // already known
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
	}
}

// Ordering must be stable across polls, or the list reshuffles under the cursor.
func TestDiscoveredDeviceOrderingIsStable(t *testing.T) {
	build := func() []discoveredDevice {
		return []discoveredDevice{
			{Serial: "ccc", State: devicewatch.StateDevice},
			{Serial: "aaa", State: devicewatch.StateDevice},
			{Serial: "bbb", State: devicewatch.StateDevice},
		}
	}
	first, second := build(), build()
	sortDiscovered(first)
	sortDiscovered(second)
	for i := range first {
		if first[i].Serial != second[i].Serial {
			t.Fatalf("ordering is not deterministic: %v vs %v", first, second)
		}
	}
	if first[0].Serial != "aaa" || first[2].Serial != "ccc" {
		t.Fatalf("expected serial order within a rank, got %v", first)
	}
}

func TestSortDiscoveredHandlesEmptyAndSingle(t *testing.T) {
	sortDiscovered(nil)
	one := []discoveredDevice{{Serial: "x"}}
	sortDiscovered(one)
	if len(one) != 1 || one[0].Serial != "x" {
		t.Fatal("a single-element slice was disturbed")
	}
}

// fakeADB answers `adb devices -l` from a canned string.
type fakeADB struct {
	out string
	err error
}

func (f fakeADB) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	return []byte(f.out), f.err
}

func TestListADBDevicesAnnotatesRegistration(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	srv, handler := newTestAPIServer(t)
	cookies := adminCookies(t, handler)

	// One device already registered, one waiting to be.
	if _, err := srv.store.CreatePhysicalAndroid(t.Context(), "lab-1", "AAA111"); err != nil {
		t.Fatalf("register: %v", err)
	}
	srv.cmdRunner = fakeADB{out: "List of devices attached\n" +
		"AAA111\tdevice usb:1-3 model:Pixel_6\n" +
		"BBB222\tdevice usb:1-4 model:Pixel_8\n"}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/adb/devices", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("discovery: %d %s", rec.Code, rec.Body.String())
	}

	var body struct {
		Devices []discoveredDevice `json:"devices"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Devices) != 2 {
		t.Fatalf("expected both devices, got %+v", body.Devices)
	}
	// The unregistered, ready device comes first — that is what the operator
	// is looking for.
	if body.Devices[0].Serial != "BBB222" || body.Devices[0].Registered {
		t.Fatalf("first entry should be the unregistered device, got %+v", body.Devices[0])
	}
	if body.Devices[0].Model != "Pixel_8" {
		t.Fatalf("model not surfaced: %+v", body.Devices[0])
	}
	if !body.Devices[1].Registered || body.Devices[1].RegisteredAs != "lab-1" {
		t.Fatalf("registered device should be marked and named: %+v", body.Devices[1])
	}
}

// adb missing must say so, not return an empty list that reads as "no devices
// are plugged in" — those need completely different responses from an operator.
func TestListADBDevicesReportsAMissingADB(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	srv, handler := newTestAPIServer(t)
	cookies := adminCookies(t, handler)
	srv.cmdRunner = fakeADB{err: errNoADB{}}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/adb/devices", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing adb = %d, want 503: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "adb") {
		t.Fatalf("error should name adb: %s", rec.Body.String())
	}
}

type errNoADB struct{}

func (errNoADB) Error() string { return `exec: "adb": executable file not found in $PATH` }

// Discovery is read-only: looking must never create inventory.
func TestListADBDevicesCreatesNothing(t *testing.T) {
	t.Setenv("OPENCUTTLES_SECURE_COOKIES", "0")
	srv, handler := newTestAPIServer(t)
	cookies := adminCookies(t, handler)
	srv.cmdRunner = fakeADB{out: "List of devices attached\nCCC333\tdevice usb:1-5\n"}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/adb/devices", nil)
	for _, c := range cookies {
		req.AddCookie(c)
	}
	handler.ServeHTTP(httptest.NewRecorder(), req)

	devices, err := srv.store.ListPhysicalAndroid(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(devices) != 0 {
		t.Fatalf("discovery created %d rows; it must only look: %+v", len(devices), devices)
	}
}
