package devicecontrol

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// recordingRunner captures the adb command lines a driver produces.
type recordingRunner struct{ calls [][]string }

func (r *recordingRunner) Run(_ context.Context, _ []byte, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, append([]string{name}, args...))
	return nil, nil
}

func (r *recordingRunner) last() string {
	if len(r.calls) == 0 {
		return ""
	}
	return strings.Join(r.calls[len(r.calls)-1], " ")
}

func androidDriver() (adbDriver, *recordingRunner) {
	rec := &recordingRunner{}
	svc := &Service{runner: rec, execute: true}
	inst := domain.Instance{ID: "cvd_1", Platform: domain.PlatformAndroid, ADBPort: 6520, DisplayWidth: 900, DisplayHeight: 1500}
	return adbDriver{svc: svc, inst: inst}, rec
}

// A touchscreen has no right button. Silently treating it as a normal tap would
// let a test "pass" while doing the wrong thing, so it must be rejected.
func TestAndroidClickRejectsNonLeftButtons(t *testing.T) {
	d, _ := androidDriver()
	for _, button := range []string{"right", "middle"} {
		err := d.Click(context.Background(), 10, 20, button, 1)
		if !errors.Is(err, ErrUnsupported) {
			t.Errorf("button %q: want ErrUnsupported, got %v", button, err)
		}
	}
}

func TestAndroidClickLeftAndDouble(t *testing.T) {
	d, rec := androidDriver()
	if err := d.Click(context.Background(), 10, 20, "", 1); err != nil {
		t.Fatalf("left click: %v", err)
	}
	if got := rec.last(); !strings.Contains(got, "input tap 10 20") {
		t.Errorf("single tap = %q", got)
	}

	d2, rec2 := androidDriver()
	if err := d2.Click(context.Background(), 5, 6, "left", 2); err != nil {
		t.Fatalf("double click: %v", err)
	}
	if len(rec2.calls) != 2 {
		t.Errorf("double-click should emit 2 taps, got %d", len(rec2.calls))
	}
}

// Android has no wheel: scroll becomes a swipe in the opposite direction, since
// dragging up reveals content below.
func TestAndroidScrollBecomesOppositeSwipe(t *testing.T) {
	d, rec := androidDriver()
	// Scroll down 1 notch from the centre (450,750) of a 900x1500 display:
	// dy=1 -> 1*1500/3 = 500px, so the finger drags from 750 up to 250.
	if err := d.Scroll(context.Background(), 0, 0, 0, 1); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	if got := rec.last(); !strings.Contains(got, "input swipe 450 750 450 250") {
		t.Errorf("scroll-down swipe = %q, want a drag from 750 up to 250", got)
	}

	d2, rec2 := androidDriver()
	if err := d2.Scroll(context.Background(), 0, 0, 0, -1); err != nil {
		t.Fatalf("scroll up: %v", err)
	}
	if got := rec2.last(); !strings.Contains(got, "input swipe 450 750 450 1250") {
		t.Errorf("scroll-up swipe = %q, want a drag from 750 down to 1250", got)
	}
}

// A huge notch count must stay on-screen; the input system ignores off-screen
// gestures, which would look like "scroll silently did nothing".
func TestAndroidScrollClampsToDisplay(t *testing.T) {
	d, rec := androidDriver()
	if err := d.Scroll(context.Background(), 0, 0, 0, 99); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	got := rec.last()
	if !strings.Contains(got, "input swipe 450 750 450 1 ") && !strings.HasSuffix(got, "input swipe 450 750 450 1 300") {
		t.Errorf("scroll = %q, want the endpoint clamped to y=1", got)
	}
}

// Chords are meaningless on a touchscreen and must say so rather than no-op.
func TestAndroidChordUnsupported(t *testing.T) {
	d, _ := androidDriver()
	err := d.Chord(context.Background(), []string{"CTRL", "C"})
	if !errors.Is(err, ErrUnsupported) {
		t.Errorf("want ErrUnsupported, got %v", err)
	}
	if !strings.Contains(err.Error(), "press_key") {
		t.Errorf("error should point at the Android alternative: %v", err)
	}
}
