package devicewatch

import (
	"strings"
	"unicode"
)

// Device states as reported by `adb devices`.
const (
	StateDevice        = "device"         // ready
	StateOffline       = "offline"        // known but not responding
	StateUnauthorized  = "unauthorized"   // USB debugging prompt not accepted
	StateNoPermissions = "no permissions" // host cannot open the USB device
	StateUnknown       = ""               // not listed at all
)

// DeviceStatus is one line of `adb devices -l`.
type DeviceStatus struct {
	Serial string
	State  string
	// Model and Product come from the -l extras and are only used to help an
	// operator recognize a device in the pick-list.
	Model   string
	Product string
}

// ParseADBDevices reads `adb devices -l` output.
//
// The format is deceptively awkward. Most lines are "serial<tab/spaces>state
// key:value key:value", but two states break naive field splitting:
//
//	1234567890  no permissions (user in plugdev group; are your udev rules wrong?); see [http://...]
//
// "no permissions" contains a space and is followed by free prose, so the state
// cannot be taken as "the second field". Treating it as such yields the state
// "no", which silently becomes "unknown" — and an operator with a udev problem
// is told their device simply is not there.
func ParseADBDevices(out string) map[string]DeviceStatus {
	devices := map[string]DeviceStatus{}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(strings.ReplaceAll(line, "\r", ""))
		if line == "" || strings.HasPrefix(line, "List of devices") {
			continue
		}
		// Daemon chatter appears on stdout on first run.
		if strings.HasPrefix(line, "*") || strings.HasPrefix(line, "adb server") {
			continue
		}

		// adb separates the serial from the state with a TAB, not a space —
		// splitting on " " parses nothing at all against a real device. Cut at
		// the first run of whitespace of any kind.
		idx := strings.IndexFunc(line, unicode.IsSpace)
		if idx < 0 {
			// A serial with no state is not usable information.
			continue
		}
		serial := strings.TrimSpace(line[:idx])
		rest := strings.TrimSpace(line[idx:])
		if serial == "" || rest == "" {
			continue
		}

		status := DeviceStatus{Serial: serial, State: parseState(rest)}
		for _, field := range strings.Fields(rest) {
			if key, value, ok := strings.Cut(field, ":"); ok {
				switch key {
				case "model":
					status.Model = value
				case "product":
					status.Product = value
				}
			}
		}
		devices[serial] = status
	}
	return devices
}

// parseState classifies the text following the serial.
//
// Ordered longest-first so "no permissions" is not shadowed by a prefix match,
// and matched by prefix because adb appends prose after the state.
func parseState(rest string) string {
	switch {
	case strings.HasPrefix(rest, StateNoPermissions):
		return StateNoPermissions
	case strings.HasPrefix(rest, StateUnauthorized):
		return StateUnauthorized
	case strings.HasPrefix(rest, StateOffline):
		return StateOffline
	case strings.HasPrefix(rest, StateDevice):
		return StateDevice
	default:
		return StateUnknown
	}
}
