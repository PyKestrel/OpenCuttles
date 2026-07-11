package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

// screen is the platform-specific desktop control surface. Coordinates are in
// screen pixels of the primary display.
type screen interface {
	Screenshot() ([]byte, error) // PNG bytes of the current screen
	Click(x, y int) error
	Drag(x1, y1, x2, y2, durationMs int) error
	Type(text string) error
	Key(name string) error
}

// controller maps the appliance's server-agnostic control vocabulary to the
// platform screen. This is the same vocabulary devicecontrol.mcpDriver speaks.
type controller struct {
	screen screen
}

func (c *controller) handle(method string, params json.RawMessage) (any, error) {
	switch method {
	case "screenshot":
		png, err := c.screen.Screenshot()
		if err != nil {
			return nil, err
		}
		return map[string]string{"pngBase64": base64.StdEncoding.EncodeToString(png)}, nil
	case "click":
		var p struct{ X, Y int }
		_ = json.Unmarshal(params, &p)
		return map[string]any{}, c.screen.Click(p.X, p.Y)
	case "drag":
		var p struct{ X1, Y1, X2, Y2, DurationMs int }
		_ = json.Unmarshal(params, &p)
		return map[string]any{}, c.screen.Drag(p.X1, p.Y1, p.X2, p.Y2, p.DurationMs)
	case "type":
		var p struct{ Text string }
		_ = json.Unmarshal(params, &p)
		return map[string]any{}, c.screen.Type(p.Text)
	case "key":
		var p struct{ Key string }
		_ = json.Unmarshal(params, &p)
		return map[string]any{}, c.screen.Key(p.Key)
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}
