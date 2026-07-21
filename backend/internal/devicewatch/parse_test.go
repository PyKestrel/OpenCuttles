package devicewatch

import "testing"

// Real `adb devices -l` output. Note the TAB between serial and state — that is
// what adb actually emits, and splitting on a space parses nothing at all.
const sampleOutput = "* daemon not running; starting now at tcp:5037\n" +
	"* daemon started successfully\n" +
	"List of devices attached\n" +
	"R5CT30ABCDE\tdevice usb:1-3 product:raven model:Pixel_6_Pro device:raven transport_id:1\n" +
	"192.168.1.42:5555\tdevice product:sunfish model:Pixel_4a device:sunfish transport_id:2\n" +
	"emulator-5554\toffline\n" +
	"1234567890ABCDEF\tunauthorized usb:1-4 transport_id:3\n" +
	"FEDCBA0987654321\tno permissions (user in plugdev group; are your udev rules wrong?); see [http://developer.android.com/tools/device.html]\n"

func TestParseADBDevices(t *testing.T) {
	got := ParseADBDevices(sampleOutput)

	if len(got) != 5 {
		t.Fatalf("parsed %d devices, want 5: %+v", len(got), got)
	}

	cases := map[string]string{
		"R5CT30ABCDE":       StateDevice,
		"192.168.1.42:5555": StateDevice,
		"emulator-5554":     StateOffline,
		"1234567890ABCDEF":  StateUnauthorized,
		"FEDCBA0987654321":  StateNoPermissions,
	}
	for serial, want := range cases {
		d, ok := got[serial]
		if !ok {
			t.Errorf("%s missing from the parse", serial)
			continue
		}
		if d.State != want {
			t.Errorf("%s state = %q, want %q", serial, d.State, want)
		}
	}

	// The -l extras help an operator recognize a device in a pick-list.
	if d := got["R5CT30ABCDE"]; d.Model != "Pixel_6_Pro" || d.Product != "raven" {
		t.Errorf("extras not parsed: model=%q product=%q", d.Model, d.Product)
	}
}

// "no permissions" contains a space. Taking the state as "the second field"
// yields "no", which falls through to unknown — and an operator with a udev
// problem is told their device simply is not there.
func TestParseADBDevicesHandlesNoPermissions(t *testing.T) {
	out := "List of devices attached\n" +
		"ABC123\tno permissions (user in plugdev group); see [http://example]\n"
	got := ParseADBDevices(out)

	d, ok := got["ABC123"]
	if !ok {
		t.Fatal("device missing")
	}
	if d.State != StateNoPermissions {
		t.Fatalf("state = %q, want %q — the multi-word state was mis-split", d.State, StateNoPermissions)
	}
}

// adb uses a tab, but the exact spacing is not something to depend on.
func TestParseADBDevicesAcceptsSpaceSeparators(t *testing.T) {
	out := "List of devices attached\nR5CT30ABCDE            device usb:1-3\n"
	if d, ok := ParseADBDevices(out)["R5CT30ABCDE"]; !ok || d.State != StateDevice {
		t.Fatalf("space-separated line not parsed: %+v ok=%v", d, ok)
	}
}

func TestParseADBDevicesIgnoresNoise(t *testing.T) {
	for _, out := range []string{
		"",
		"List of devices attached\n",
		"* daemon not running; starting now at tcp:5037\n* daemon started successfully\nList of devices attached\n",
		"\r\nList of devices attached\r\n",
		// A bare serial with no state carries no usable information.
		"List of devices attached\nR5CT30ABCDE\n",
	} {
		if got := ParseADBDevices(out); len(got) != 0 {
			t.Errorf("expected no devices from %q, got %+v", out, got)
		}
	}
}

// Windows line endings must not become part of the state.
func TestParseADBDevicesHandlesCRLF(t *testing.T) {
	out := "List of devices attached\r\nR5CT30ABCDE\tdevice usb:1-3\r\n"
	got := ParseADBDevices(out)
	d, ok := got["R5CT30ABCDE"]
	if !ok {
		t.Fatal("device missing with CRLF input")
	}
	if d.State != StateDevice {
		t.Fatalf("state = %q, want device", d.State)
	}
}

func TestParseDensity(t *testing.T) {
	cases := map[string]int{
		"Physical density: 420":                          420,
		"Physical density: 420\nOverride density: 320":   320, // override wins
		"Physical density: 560\r\nOverride density: 480": 480,
		"":        0,
		"garbage": 0,
	}
	for in, want := range cases {
		if got := parseDensity(in); got != want {
			t.Errorf("parseDensity(%q) = %d, want %d", in, got, want)
		}
	}
}
