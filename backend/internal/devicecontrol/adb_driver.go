package devicecontrol

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// adbDriver implements Driver for Android instances by reusing the Service's ADB
// helpers, preserving the exact behavior the device stack has always had.
type adbDriver struct {
	svc  *Service
	inst domain.Instance
}

func (d adbDriver) Screenshot(ctx context.Context) ([]byte, error) {
	return d.svc.adb(ctx, d.inst, "exec-out", "screencap", "-p")
}

func (d adbDriver) Tap(ctx context.Context, x, y int) error {
	_, err := d.svc.adbShell(ctx, d.inst, "input", "tap", strconv.Itoa(x), strconv.Itoa(y))
	return err
}

func (d adbDriver) Swipe(ctx context.Context, x1, y1, x2, y2, durationMs int) error {
	args := []string{"input", "swipe", strconv.Itoa(x1), strconv.Itoa(y1), strconv.Itoa(x2), strconv.Itoa(y2)}
	if durationMs > 0 {
		args = append(args, strconv.Itoa(durationMs))
	}
	_, err := d.svc.adbShell(ctx, d.inst, args...)
	return err
}

func (d adbDriver) Text(ctx context.Context, text string) error {
	_, err := d.svc.adbShell(ctx, d.inst, "input", "text", escapeInputText(text))
	return err
}

func (d adbDriver) Key(ctx context.Context, key string) error {
	_, err := d.svc.adbShell(ctx, d.inst, "input", "keyevent", normalizeKeycode(key))
	return err
}

// Click on Android: only a left "button" exists (a finger). count>1 becomes
// repeated taps (a double-tap). Right/middle are rejected rather than silently
// treated as a normal tap, which would look like a passing but wrong action.
func (d adbDriver) Click(ctx context.Context, x, y int, button string, count int) error {
	switch strings.ToUpper(strings.TrimSpace(button)) {
	case "", "LEFT":
	default:
		return fmt.Errorf("%w: a touchscreen has no %s button", ErrUnsupported, button)
	}
	if count <= 0 {
		count = 1
	}
	for i := 0; i < count; i++ {
		if i > 0 {
			time.Sleep(60 * time.Millisecond) // within the double-tap window
		}
		if err := d.Tap(ctx, x, y); err != nil {
			return err
		}
	}
	return nil
}

// Scroll on Android has no wheel: it becomes a swipe in the opposite direction
// (dragging up reveals content below). One notch ≈ a third of the screen.
func (d adbDriver) Scroll(ctx context.Context, x, y, dx, dy int) error {
	w, h := d.inst.DisplayWidth, d.inst.DisplayHeight
	if w <= 0 || h <= 0 {
		w, h = 720, 1280
	}
	if x <= 0 && y <= 0 {
		x, y = w/2, h/2
	}
	// Clamp the gesture inside the display so a large notch count can't swipe
	// off-screen (which the input system ignores).
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	dxPx, dyPx := dx*w/3, dy*h/3
	x2 := clamp(x-dxPx, 1, w-1)
	y2 := clamp(y-dyPx, 1, h-1)
	if x2 == x && y2 == y {
		return nil // nothing to do
	}
	return d.Swipe(ctx, x, y, x2, y2, 300)
}

// Chord is meaningless on a touchscreen — there are no modifier keys to hold.
func (adbDriver) Chord(context.Context, []string) error {
	return fmt.Errorf("%w: modifier chords are desktop-only; use press_key on Android", ErrUnsupported)
}

func (adbDriver) Capabilities() Capabilities {
	return Capabilities{UITree: true, Apps: true, Shell: true, Rotate: true, Logcat: true, InstallApp: true, ScreenRecord: true}
}
