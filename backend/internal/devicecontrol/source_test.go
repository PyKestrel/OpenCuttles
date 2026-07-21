package devicecontrol

import (
	"errors"
	"strings"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// adbTarget is the seam that lets a physical device exist at all: Cuttlefish
// keeps its loopback port, everything else carries its own address.
func TestADBTarget(t *testing.T) {
	cases := []struct {
		name    string
		inst    domain.Instance
		want    string
		wantErr bool
	}{
		{
			name: "cuttlefish uses its allocated loopback port",
			inst: domain.Instance{ID: "cvd_1", ADBPort: 6520},
			want: "127.0.0.1:6520",
		},
		{
			name: "physical device by USB serial",
			inst: domain.Instance{ID: "dev_1", Src: domain.SourcePhysical, ADBTarget: "R5CT30ABCDE"},
			want: "R5CT30ABCDE",
		},
		{
			name: "physical device over adb-tcp",
			inst: domain.Instance{ID: "dev_1", Src: domain.SourcePhysical, ADBTarget: "192.168.1.42:5555"},
			want: "192.168.1.42:5555",
		},
		{
			// An explicit target wins, so a Cuttlefish row could be repointed
			// without changing how it is addressed everywhere else.
			name: "explicit target overrides the port",
			inst: domain.Instance{ID: "cvd_1", ADBPort: 6520, ADBTarget: "R5CT30ABCDE"},
			want: "R5CT30ABCDE",
		},
		{
			name:    "no address at all is an error, not a malformed transport",
			inst:    domain.Instance{ID: "dev_1", Src: domain.SourcePhysical},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := adbTarget(tc.inst)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("adbTarget = %q, want %q", got, tc.want)
			}
		})
	}
}

// resolve's gate is the heart of this change. Cuttlefish behavior must be
// byte-identical to before, physical devices must NOT be subject to the
// Cuttlefish-only preconditions, and runner desktops must be unchanged.
func TestResolveGateBySource(t *testing.T) {
	cases := []struct {
		name    string
		execute bool
		inst    domain.Instance
		wantErr error
		errText string
	}{
		// --- Cuttlefish: unchanged ---
		{
			name:    "cuttlefish running with execution enabled",
			execute: true,
			inst:    domain.Instance{ID: "a", State: domain.StateRunning, ADBPort: 6520},
		},
		{
			name:    "cuttlefish refused when execution is disabled",
			execute: false,
			inst:    domain.Instance{ID: "a", State: domain.StateRunning, ADBPort: 6520},
			wantErr: ErrExecutionDisabled,
		},
		{
			name:    "cuttlefish refused when not running",
			execute: true,
			inst:    domain.Instance{ID: "a", State: domain.StateStopped, ADBPort: 6520},
			wantErr: ErrNotRunning,
		},
		{
			name:    "cuttlefish refused without an ADB port",
			execute: true,
			inst:    domain.Instance{ID: "a", State: domain.StateRunning},
			errText: "no ADB port",
		},
		{
			// Legacy rows have an empty source and must behave as Cuttlefish.
			name:    "empty source is treated as cuttlefish",
			execute: false,
			inst:    domain.Instance{ID: "a", Src: "", State: domain.StateRunning, ADBPort: 6520},
			wantErr: ErrExecutionDisabled,
		},

		// --- Physical: the point of the change ---
		{
			name: "physical online works with execution disabled",
			// EXECUTE_CVD governs launching VMs; it has nothing to say about
			// talking to a handset. Requiring it was the blocker.
			execute: false,
			inst: domain.Instance{ID: "p", Src: domain.SourcePhysical,
				State: domain.StateOnline, ADBTarget: "R5CT30ABCDE"},
		},
		{
			name:    "physical refused when offline",
			execute: true,
			inst: domain.Instance{ID: "p", Src: domain.SourcePhysical,
				State: domain.StateOffline, ADBTarget: "R5CT30ABCDE"},
			wantErr: ErrNotRunning,
		},
		{
			name:    "physical refused without a target",
			execute: true,
			inst:    domain.Instance{ID: "p", Src: domain.SourcePhysical, State: domain.StateOnline},
			errText: "no ADB target",
		},

		// --- Runner desktop: unchanged ---
		{
			name:    "runner online with an endpoint",
			execute: false,
			inst: domain.Instance{ID: "d", Src: domain.SourceRunner, Platform: domain.PlatformWindows,
				State: domain.StateOnline, ControlEndpoint: "tunnel"},
		},
		{
			name:    "runner refused without an endpoint",
			execute: false,
			inst: domain.Instance{ID: "d", Src: domain.SourceRunner, Platform: domain.PlatformWindows,
				State: domain.StateOnline},
			errText: "no control endpoint",
		},
		{
			name:    "runner refused when offline",
			execute: false,
			inst: domain.Instance{ID: "d", Src: domain.SourceRunner, Platform: domain.PlatformWindows,
				State: domain.StateOffline, ControlEndpoint: "tunnel"},
			wantErr: ErrNotRunning,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := &Service{execute: tc.execute}
			err := svc.checkResolvable(tc.inst)
			switch {
			case tc.wantErr != nil:
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
			case tc.errText != "":
				if err == nil || !strings.Contains(err.Error(), tc.errText) {
					t.Fatalf("err = %v, want one containing %q", err, tc.errText)
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			}
		})
	}
}

// Routing must follow the source, not the platform. A physical Android device
// takes the ADB driver; only runner desktops go over the tunnel.
func TestDriverRoutingFollowsSource(t *testing.T) {
	svc := &Service{}

	for _, inst := range []domain.Instance{
		{ID: "a", ADBPort: 6520},                                        // legacy/cuttlefish
		{ID: "b", Src: domain.SourceCuttlefish, ADBPort: 6520},          // explicit
		{ID: "c", Src: domain.SourcePhysical, ADBTarget: "R5CT30ABCDE"}, // real handset
	} {
		drv, err := svc.driverFor(inst)
		if err != nil {
			t.Fatalf("%s: driverFor errored: %v", inst.ID, err)
		}
		if _, ok := drv.(adbDriver); !ok {
			t.Fatalf("%s: expected the ADB driver, got %T", inst.ID, drv)
		}
	}

	// A runner desktop with no hub configured must fail rather than silently
	// falling through to ADB, which it does not have.
	if _, err := svc.driverFor(domain.Instance{
		ID: "d", Src: domain.SourceRunner, Platform: domain.PlatformWindows,
	}); err == nil {
		t.Fatal("a runner device with no tunnel should not resolve to a driver")
	}
}

// ADB must be refused for runner desktops, and allowed for anything
// ADB-reachable. The old test was "not Android", which a physical Android
// device would have passed for the wrong reason.
func TestADBRejectedOnlyForRunnerDevices(t *testing.T) {
	svc := &Service{runner: nil}

	_, err := svc.adb(t.Context(), domain.Instance{
		ID: "d", Src: domain.SourceRunner, Platform: domain.PlatformWindows,
	}, "shell", "true")
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("runner device should be ErrUnsupported for ADB, got %v", err)
	}
}
