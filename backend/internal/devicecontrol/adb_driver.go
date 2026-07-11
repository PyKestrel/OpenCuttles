package devicecontrol

import (
	"context"
	"strconv"

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

func (adbDriver) Capabilities() Capabilities {
	return Capabilities{UITree: true, Apps: true, Shell: true, Rotate: true, Logcat: true, InstallApp: true, ScreenRecord: true}
}
