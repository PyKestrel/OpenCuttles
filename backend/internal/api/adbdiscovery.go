package api

import (
	"context"
	"net/http"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/devicewatch"
)

// discoveredDevice is one entry from `adb devices -l`, annotated with whether
// this appliance already knows about it.
type discoveredDevice struct {
	Serial string `json:"serial"`
	// State is adb's own word: device, offline, unauthorized, no permissions.
	State   string `json:"state"`
	Model   string `json:"model,omitempty"`
	Product string `json:"product,omitempty"`
	// Registered marks a device that already has a row, so the UI can show it
	// greyed out rather than silently omitting it — an operator looking for a
	// phone they just plugged in should see it either way.
	Registered bool `json:"registered"`
	// RegisteredAs is the device's name here, when it has one.
	RegisteredAs string `json:"registeredAs,omitempty"`
}

// listADBDevices reports what ADB can currently see, to populate the
// registration pick-list.
//
// Discovery only assists; it never creates rows. A device that is plugged in
// briefly would otherwise litter the inventory, and worse, break the stable
// device-id contract that test runs and cycles reference — a row that appears
// and vanishes with a USB cable is not something a test result can point at.
func (s *Server) listADBDevices(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()

	out, err := s.cmdRunner.Run(ctx, nil, "adb", "devices", "-l")
	if err != nil {
		// Almost always "adb is not installed", which is worth saying plainly
		// rather than returning an empty list that looks like "no devices".
		writeError(w, clientError{
			status:  http.StatusServiceUnavailable,
			message: "could not run adb on the appliance: " + err.Error(),
		})
		return
	}

	known := map[string]string{} // adb target -> device name
	if registered, err := s.store.ListPhysicalAndroid(ctx); err == nil {
		for _, d := range registered {
			known[d.ADBTarget] = d.Name
		}
	}

	seen := devicewatch.ParseADBDevices(string(out))
	devices := make([]discoveredDevice, 0, len(seen))
	for serial, status := range seen {
		name, registered := known[serial]
		devices = append(devices, discoveredDevice{
			Serial:       serial,
			State:        status.State,
			Model:        status.Model,
			Product:      status.Product,
			Registered:   registered,
			RegisteredAs: name,
		})
	}
	sortDiscovered(devices)

	writeJSON(w, http.StatusOK, map[string]any{"devices": devices})
}

// sortDiscovered puts usable, unregistered devices first — the ones an operator
// is most likely to be looking for — then orders by serial for stability so the
// list does not reshuffle between polls.
func sortDiscovered(devices []discoveredDevice) {
	rank := func(d discoveredDevice) int {
		switch {
		case !d.Registered && d.State == devicewatch.StateDevice:
			return 0
		case !d.Registered:
			return 1
		default:
			return 2
		}
	}
	for i := 1; i < len(devices); i++ {
		for j := i; j > 0; j-- {
			a, b := devices[j-1], devices[j]
			if rank(a) < rank(b) || (rank(a) == rank(b) && a.Serial <= b.Serial) {
				break
			}
			devices[j-1], devices[j] = b, a
		}
	}
}
