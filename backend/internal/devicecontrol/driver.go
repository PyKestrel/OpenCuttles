package devicecontrol

import (
	"context"
	"errors"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// ErrUnsupported is returned when an operation isn't available for a device's
// platform (e.g. logcat/app-launch on a Windows desktop). Callers should surface
// it as a clear "not supported on this platform" message.
var ErrUnsupported = errors.New("operation not supported for this device platform")

// Capabilities advertises which optional operations a driver supports beyond the
// core primitives (screenshot/tap/swipe/type/key), so the MCP tool surface and
// the UI can hide what a platform can't do.
type Capabilities struct {
	UITree       bool `json:"uiTree"`
	Apps         bool `json:"apps"`
	Shell        bool `json:"shell"`
	Rotate       bool `json:"rotate"`
	Logcat       bool `json:"logcat"`
	InstallApp   bool `json:"installApp"`
	ScreenRecord bool `json:"screenRecord"`
}

// Driver performs the platform-agnostic control primitives against one device.
// Android is driven over ADB (adbDriver); desktop OSes are driven over a
// computer-use MCP server (mcpDriver).
type Driver interface {
	Screenshot(ctx context.Context) ([]byte, error)
	Tap(ctx context.Context, x, y int) error
	Swipe(ctx context.Context, x1, y1, x2, y2, durationMs int) error
	Text(ctx context.Context, text string) error
	Key(ctx context.Context, key string) error
	Capabilities() Capabilities
}

// isAndroid reports whether a platform string denotes Android. Empty is treated
// as android so pre-multi-OS rows keep working.
func isAndroid(platform string) bool {
	return platform == "" || platform == domain.PlatformAndroid
}
