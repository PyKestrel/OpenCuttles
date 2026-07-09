// Package mcp exposes the devicecontrol service as Model Context Protocol tools
// over a streamable HTTP handler. A local cognitive-core agent (MiniCPM5 via the
// Flue sidecar) connects to this endpoint and drives Android devices through the
// tools defined here: it perceives the screen as the uiautomator accessibility
// tree (get_ui_tree) and acts with tap/swipe/type/press_key/launch_app.
//
// Device targeting: the server keeps an "active device" (an instance ID). Tools
// operate on the active device unless a call passes an explicit deviceId. The
// agent selects a device with select_device and can switch at any time — this is
// how it stays scoped to the operator's selected device yet can retarget on
// instruction.
package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	png2 "image/png"
	"log/slog"
	"net/http"
	"sync"
	"time"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/opencuttles/opencuttles/backend/internal/devicecontrol"
	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/vision"
)

// InstanceStore is the store subset the MCP tools need to enumerate and resolve
// devices. *store.SQLite satisfies it.
type InstanceStore interface {
	ListInstances(ctx context.Context) ([]domain.Instance, error)
	GetInstance(ctx context.Context, id string) (domain.Instance, error)
}

// Service wires the devicecontrol service to an MCP server and tracks the active
// device shared across tool calls.
type Service struct {
	devices *devicecontrol.Service
	store   InstanceStore
	vision  *vision.Client
	logger  *slog.Logger
	server  *mcpsdk.Server

	mu     sync.Mutex
	active string
}

// New builds the MCP service and registers all device tools. vision may be nil,
// in which case the vision-backed tools return a clear "not configured" error.
func New(devices *devicecontrol.Service, store InstanceStore, vis *vision.Client, logger *slog.Logger) *Service {
	s := &Service{
		devices: devices,
		store:   store,
		vision:  vis,
		logger:  logger,
		server:  mcpsdk.NewServer(&mcpsdk.Implementation{Name: "opencuttles", Version: "0.1.0"}, nil),
	}
	s.registerTools()
	return s
}

// Handler returns the streamable HTTP handler for the MCP endpoint. The same
// server instance is reused across sessions (device state lives on the Service).
func (s *Service) Handler() http.Handler {
	return mcpsdk.NewStreamableHTTPHandler(func(*http.Request) *mcpsdk.Server { return s.server }, nil)
}

// resolveDevice picks the target instance ID for a tool call: an explicit id if
// given, else the active device, else the sole running instance if unambiguous.
func (s *Service) resolveDevice(ctx context.Context, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	if active != "" {
		return active, nil
	}
	// Convenience: if exactly one instance is running, target it.
	instances, err := s.store.ListInstances(ctx)
	if err != nil {
		return "", err
	}
	var running []domain.Instance
	for _, inst := range instances {
		if inst.State == domain.StateRunning {
			running = append(running, inst)
		}
	}
	if len(running) == 1 {
		return running[0].ID, nil
	}
	return "", fmt.Errorf("no active device selected; call select_device with a device id (list_devices to see options)")
}

// --- tool input/output types ------------------------------------------------

type deviceRef struct {
	DeviceID string `json:"deviceId,omitempty" jsonschema:"instance id to target; omit to use the active device"`
}

type deviceInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	State    string `json:"state"`
	DeviceID string `json:"deviceId,omitempty"`
	Android  string `json:"androidVersion,omitempty"`
}

type statusOut struct {
	Status string `json:"status"`
	Device string `json:"device"`
}

func (s *Service) registerTools() {
	srv := s.server

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_devices",
		Description: "List the Android instances OpenCuttles manages, with their state and device id.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, struct {
		Devices []deviceInfo `json:"devices"`
	}, error) {
		var out struct {
			Devices []deviceInfo `json:"devices"`
		}
		instances, err := s.store.ListInstances(ctx)
		if err != nil {
			return nil, out, err
		}
		for _, inst := range instances {
			out.Devices = append(out.Devices, deviceInfo{ID: inst.ID, Name: inst.Name, State: inst.State, DeviceID: inst.DeviceID, Android: inst.AndroidVersion})
		}
		return nil, out, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "select_device",
		Description: "Set the active device that subsequent tool calls operate on.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		DeviceID string `json:"deviceId" jsonschema:"the instance id to make active"`
	}) (*mcpsdk.CallToolResult, deviceInfo, error) {
		inst, err := s.store.GetInstance(ctx, in.DeviceID)
		if err != nil {
			return nil, deviceInfo{}, err
		}
		s.mu.Lock()
		s.active = inst.ID
		s.mu.Unlock()
		return nil, deviceInfo{ID: inst.ID, Name: inst.Name, State: inst.State, DeviceID: inst.DeviceID, Android: inst.AndroidVersion}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_active_device",
		Description: "Return the currently active device.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, _ struct{}) (*mcpsdk.CallToolResult, deviceInfo, error) {
		id, err := s.resolveDevice(ctx, "")
		if err != nil {
			return nil, deviceInfo{}, err
		}
		inst, err := s.store.GetInstance(ctx, id)
		if err != nil {
			return nil, deviceInfo{}, err
		}
		return nil, deviceInfo{ID: inst.ID, Name: inst.Name, State: inst.State, DeviceID: inst.DeviceID, Android: inst.AndroidVersion}, nil
	})

	// get_ui_tree returns the accessibility tree as JSON text content. The tree
	// is recursive (nodes contain children), which the SDK's output-schema
	// inference cannot represent, so it is delivered as text rather than a typed
	// structured output — which is also the form the agent reads.
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "get_ui_tree",
		Description: "Return the current screen's accessibility tree as JSON (resource ids, text, content-desc, bounds, tap centers). This is how you 'see' the screen: read it, find the target element, then tap its center.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in deviceRef) (*mcpsdk.CallToolResult, struct{}, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, struct{}{}, err
		}
		tree, err := s.devices.UITree(ctx, id)
		if err != nil {
			return nil, struct{}{}, err
		}
		data, err := json.Marshal(tree)
		if err != nil {
			return nil, struct{}{}, err
		}
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: string(data)}},
		}, struct{}{}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "screenshot",
		Description: "Capture the current screen as a PNG image.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in deviceRef) (*mcpsdk.CallToolResult, struct{}, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, struct{}{}, err
		}
		png, err := s.devices.Screenshot(ctx, id)
		if err != nil {
			return nil, struct{}{}, err
		}
		return &mcpsdk.CallToolResult{
			Content: []mcpsdk.Content{&mcpsdk.ImageContent{Data: png, MIMEType: "image/png"}},
		}, struct{}{}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "tap",
		Description: "Tap at screen coordinates (device pixels).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		X int `json:"x"`
		Y int `json:"y"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, statusOut{}, err
		}
		if err := s.devices.Tap(ctx, id, in.X, in.Y); err != nil {
			return nil, statusOut{}, err
		}
		return nil, statusOut{Status: "ok", Device: id}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "swipe",
		Description: "Swipe/drag from (x,y) to (x2,y2) over duration milliseconds.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		X        int `json:"x"`
		Y        int `json:"y"`
		X2       int `json:"x2"`
		Y2       int `json:"y2"`
		Duration int `json:"duration,omitempty"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, statusOut{}, err
		}
		if err := s.devices.Swipe(ctx, id, in.X, in.Y, in.X2, in.Y2, in.Duration); err != nil {
			return nil, statusOut{}, err
		}
		return nil, statusOut{Status: "ok", Device: id}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "type_text",
		Description: "Type UTF-8 text into the currently focused input field.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		Text string `json:"text"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, statusOut{}, err
		}
		if err := s.devices.Text(ctx, id, in.Text); err != nil {
			return nil, statusOut{}, err
		}
		return nil, statusOut{Status: "ok", Device: id}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "press_key",
		Description: "Press a key: HOME, BACK, APP_SWITCH (recents), ENTER, VOLUME_UP, VOLUME_DOWN, POWER, or a numeric keycode.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		Key string `json:"key"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, statusOut{}, err
		}
		if err := s.devices.Key(ctx, id, in.Key); err != nil {
			return nil, statusOut{}, err
		}
		return nil, statusOut{Status: "ok", Device: id}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "launch_app",
		Description: "Launch an installed app by package name.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		Package string `json:"package"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, statusOut{}, err
		}
		if err := s.devices.LaunchApp(ctx, id, in.Package); err != nil {
			return nil, statusOut{}, err
		}
		return nil, statusOut{Status: "ok", Device: id}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_apps",
		Description: "List installed package names. Set thirdPartyOnly to exclude system apps.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		ThirdPartyOnly bool `json:"thirdPartyOnly,omitempty"`
	}) (*mcpsdk.CallToolResult, struct {
		Packages []string `json:"packages"`
	}, error) {
		var out struct {
			Packages []string `json:"packages"`
		}
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, out, err
		}
		pkgs, err := s.devices.ListApps(ctx, id, in.ThirdPartyOnly)
		if err != nil {
			return nil, out, err
		}
		out.Packages = pkgs
		return nil, out, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "current_activity",
		Description: "Return the package/activity currently in the foreground.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in deviceRef) (*mcpsdk.CallToolResult, struct {
		Activity string `json:"activity"`
	}, error) {
		var out struct {
			Activity string `json:"activity"`
		}
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, out, err
		}
		act, err := s.devices.CurrentActivity(ctx, id)
		if err != nil {
			return nil, out, err
		}
		out.Activity = act
		return nil, out, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "wait",
		Description: "Wait for a number of seconds (max 30) to let the UI settle before observing again.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		Seconds float64 `json:"seconds"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		secs := in.Seconds
		if secs <= 0 {
			secs = 1
		}
		if secs > 30 {
			secs = 30
		}
		select {
		case <-ctx.Done():
			return nil, statusOut{}, ctx.Err()
		case <-time.After(time.Duration(secs * float64(time.Second))):
		}
		return nil, statusOut{Status: "ok"}, nil
	})

	// --- vision tools: the agent's "eyes" (Moondream) --------------------------

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "tap_element",
		Description: "Tap the on-screen element that best matches a natural-language description (e.g. 'the Settings gear icon', 'the blue Sign in button', 'the search field'). Vision locates it and taps — you do NOT need coordinates. Prefer this over tap+coordinates for anything you can describe.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		Description string `json:"description" jsonschema:"what to tap, described in plain language"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, statusOut{}, err
		}
		x, y, found, err := s.locate(ctx, id, in.Description)
		if err != nil {
			return nil, statusOut{}, err
		}
		if !found {
			return nil, statusOut{}, fmt.Errorf("could not find %q on screen; try describing it differently, scroll to reveal it, or use get_ui_tree", in.Description)
		}
		if err := s.devices.Tap(ctx, id, x, y); err != nil {
			return nil, statusOut{}, err
		}
		return nil, statusOut{Status: "ok", Device: id}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "find_element",
		Description: "Locate an on-screen element by description without tapping it. Returns whether it was found and its screen coordinates (useful as a swipe endpoint or to check that something is present).",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		Description string `json:"description"`
	}) (*mcpsdk.CallToolResult, struct {
		Found bool `json:"found"`
		X     int  `json:"x"`
		Y     int  `json:"y"`
	}, error) {
		var out struct {
			Found bool `json:"found"`
			X     int  `json:"x"`
			Y     int  `json:"y"`
		}
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, out, err
		}
		x, y, found, err := s.locate(ctx, id, in.Description)
		if err != nil {
			return nil, out, err
		}
		out.Found, out.X, out.Y = found, x, y
		return nil, out, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "ask_screen",
		Description: "Ask a question about what is currently visible on the screen (e.g. 'Is Airplane mode on?', 'What screen am I on?', 'Is there an error message?'). Uses vision to answer.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		Question string `json:"question"`
	}) (*mcpsdk.CallToolResult, struct {
		Answer string `json:"answer"`
	}, error) {
		var out struct {
			Answer string `json:"answer"`
		}
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, out, err
		}
		if s.vision == nil {
			return nil, out, errVisionUnavailable
		}
		png, err := s.devices.Screenshot(ctx, id)
		if err != nil {
			return nil, out, err
		}
		answer, err := s.vision.Query(ctx, png, in.Question)
		if err != nil {
			return nil, out, err
		}
		out.Answer = answer
		return nil, out, nil
	})
}

var errVisionUnavailable = fmt.Errorf("vision is not configured; set OPENCUTTLES_VISION_URL and run the vision sidecar")

// locate screenshots the device, asks the vision model to point at the
// description, and scales the first result to device pixels. found is false when
// the model returns no match.
func (s *Service) locate(ctx context.Context, id, description string) (x, y int, found bool, err error) {
	if s.vision == nil {
		return 0, 0, false, errVisionUnavailable
	}
	png, err := s.devices.Screenshot(ctx, id)
	if err != nil {
		return 0, 0, false, err
	}
	cfg, err := png2.DecodeConfig(bytes.NewReader(png))
	if err != nil {
		return 0, 0, false, fmt.Errorf("decode screenshot: %w", err)
	}
	points, err := s.vision.Point(ctx, png, description)
	if err != nil {
		return 0, 0, false, err
	}
	if len(points) == 0 {
		return 0, 0, false, nil
	}
	x, y = points[0].Pixels(cfg.Width, cfg.Height)
	return x, y, true, nil
}
