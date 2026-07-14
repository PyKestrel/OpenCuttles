package devicecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// RunnerCaller sends control RPCs to a connected desktop runner over the dial-home
// tunnel. *runnerhub.Hub satisfies it. The appliance speaks a small, server-
// agnostic vocabulary (screenshot/click/drag/type/key); the runner translates
// each call to the bundled computer-use MCP server's own tools.
type RunnerCaller interface {
	Online(deviceID string) bool
	Call(ctx context.Context, deviceID, method string, params any) (json.RawMessage, error)
}

// mcpDriver implements Driver for desktop targets by forwarding the core control
// primitives to the device's runner.
type mcpDriver struct {
	runners RunnerCaller
	inst    domain.Instance
}

func (d mcpDriver) Screenshot(ctx context.Context) ([]byte, error) {
	raw, err := d.runners.Call(ctx, d.inst.ID, "screenshot", struct{}{})
	if err != nil {
		return nil, err
	}
	var out struct {
		PNGBase64 string `json:"pngBase64"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return base64.StdEncoding.DecodeString(out.PNGBase64)
}

func (d mcpDriver) Tap(ctx context.Context, x, y int) error {
	_, err := d.runners.Call(ctx, d.inst.ID, "click", map[string]int{"x": x, "y": y})
	return err
}

func (d mcpDriver) Swipe(ctx context.Context, x1, y1, x2, y2, durationMs int) error {
	_, err := d.runners.Call(ctx, d.inst.ID, "drag", map[string]int{
		"x1": x1, "y1": y1, "x2": x2, "y2": y2, "durationMs": durationMs,
	})
	return err
}

func (d mcpDriver) Text(ctx context.Context, text string) error {
	_, err := d.runners.Call(ctx, d.inst.ID, "type", map[string]string{"text": text})
	return err
}

func (d mcpDriver) Key(ctx context.Context, key string) error {
	_, err := d.runners.Call(ctx, d.inst.ID, "key", map[string]string{"key": key})
	return err
}

// Capabilities: desktop control supports the core primitives plus app listing/
// launching and the foreground window (via the runner). The accessibility tree is
// not exposed yet (the agent uses vision/ask_screen on desktops).
func (mcpDriver) Capabilities() Capabilities {
	return Capabilities{Apps: true}
}
