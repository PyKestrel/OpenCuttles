// Package mcp exposes the devicecontrol service as Model Context Protocol tools
// over a streamable HTTP handler. A local cognitive-core agent (MiniCPM5 via the
// Flue sidecar) connects to this endpoint and drives Android devices through the
// tools defined here: it perceives the screen with vision (ask_screen /
// tap_element / find_element) or the accessibility tree (get_ui_tree) and acts
// with tap_element/scroll/type_text/press_key/launch_app.
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
	"os"
	"path/filepath"
	"strings"
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

// RunSink receives per-step results captured by report_step_result during an
// agent-driven cycle case run (implemented by *store.SQLite).
type RunSink interface {
	AppendStep(ctx context.Context, runID string, step domain.StepResult) error
}

// VisionQuerier answers a natural-language question about a screenshot. When set
// (via SetVQA), ask_screen prefers it over the local caption model for real
// visual question answering, falling back to the caption on any error.
type VisionQuerier interface {
	Query(ctx context.Context, png []byte, question string) (string, error)
}

// boundRun ties a device to the cycle case-run currently executing on it, so
// report_step_result knows where to write evidence.
type boundRun struct {
	runID     string
	runDir    string
	steps     int
	stepTexts []string
}

// Service wires the devicecontrol service to an MCP server and tracks the active
// device shared across tool calls.
type Service struct {
	devices *devicecontrol.Service
	store   InstanceStore
	vision  *vision.Client
	vqa     VisionQuerier
	logger  *slog.Logger
	server  *mcpsdk.Server
	sink    RunSink

	mu     sync.Mutex
	active string
	runs   map[string]*boundRun // deviceID → in-flight cycle case run
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
		runs:    map[string]*boundRun{},
	}
	s.registerTools()
	return s
}

// SetSink wires the store that receives report_step_result evidence.
func (s *Service) SetSink(sink RunSink) { s.sink = sink }

// SetVQA wires an optional visual-question-answering backend (the configured
// LLM) that ask_screen prefers over the local caption model.
func (s *Service) SetVQA(v VisionQuerier) { s.vqa = v }

// SetActive sets the device that tool calls operate on. Used by the headless
// cycle executor before dispatching an agent run for a case.
func (s *Service) SetActive(id string) {
	s.mu.Lock()
	s.active = id
	s.mu.Unlock()
}

// BindRun ties a device to a cycle case-run so report_step_result records
// evidence into it; stepTexts[i] labels the i-th (1-based) reported step.
func (s *Service) BindRun(deviceID, runID, runDir string, stepTexts []string) {
	s.mu.Lock()
	s.runs[deviceID] = &boundRun{runID: runID, runDir: runDir, stepTexts: stepTexts}
	s.mu.Unlock()
}

// UnbindRun clears the device→run binding after a case completes.
func (s *Service) UnbindRun(deviceID string) {
	s.mu.Lock()
	delete(s.runs, deviceID)
	s.mu.Unlock()
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
		// Validate the id rather than passing a hallucinated value downstream
		// (which used to surface a raw "sql: no rows" error that the agent
		// could not interpret and would spiral on).
		if _, err := s.store.GetInstance(ctx, explicit); err != nil {
			return "", fmt.Errorf("no device with id %q — do not guess or invent device ids. You already operate on the active device, so omit deviceId. Only use list_devices/select_device when the user names a specific other device", explicit)
		}
		return explicit, nil
	}
	s.mu.Lock()
	active := s.active
	s.mu.Unlock()
	if active != "" {
		return active, nil
	}
	// Convenience: if exactly one device is reachable (a running Android VM or an
	// online desktop), target it.
	instances, err := s.store.ListInstances(ctx)
	if err != nil {
		return "", err
	}
	var reachable []domain.Instance
	for _, inst := range instances {
		if inst.State == domain.StateRunning || inst.State == domain.StateOnline {
			reachable = append(reachable, inst)
		}
	}
	if len(reachable) == 1 {
		return reachable[0].ID, nil
	}
	return "", fmt.Errorf("no active device selected; call select_device with a device id (list_devices to see options)")
}

// requireAndroidDevice returns a directive error when the target is a desktop, so
// the Android-only tools (packages / app list / UI tree / activity) redirect the
// agent to the desktop approach instead of failing cryptically and sending it into
// a loop of more Android-only calls.
func (s *Service) requireAndroidDevice(ctx context.Context, id string) error {
	inst, err := s.store.GetInstance(ctx, id)
	if err != nil {
		return err
	}
	if inst.Platform != "" && inst.Platform != domain.PlatformAndroid {
		return fmt.Errorf("this device is a %s DESKTOP, not Android — this particular tool (package launch / accessibility UI tree) is Android-only. Instead: OPEN an app with open_app {name}; LIST apps with list_apps; READ the screen with ask_screen; CLICK with tap_element. Those all work on this desktop", inst.Platform)
	}
	return nil
}

// --- tool input/output types ------------------------------------------------

type deviceRef struct {
	DeviceID string `json:"deviceId,omitempty" jsonschema:"instance id to target; omit to use the active device"`
}

type deviceInfo struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	State    string `json:"state"`
	DeviceID string `json:"deviceId,omitempty"`
	Android  string `json:"androidVersion,omitempty"`
}

type statusOut struct {
	Status string `json:"status"`
	Device string `json:"device"`
}

// androidPackages maps common display names to launcher packages, for open_app.
var androidPackages = map[string]string{
	"settings": "com.android.settings", "chrome": "com.android.chrome", "browser": "com.android.chrome",
	"clock": "com.android.deskclock", "contacts": "com.android.contacts",
	"phone": "com.android.dialer", "dialer": "com.android.dialer",
	"messages": "com.android.messaging", "messaging": "com.android.messaging",
	"camera": "com.android.camera2", "calculator": "com.android.calculator2",
	"files": "com.android.documentsui", "gallery": "com.android.gallery3d",
}

// settle waits briefly (interruptible) between the steps of a composite action.
func (s *Service) settle(ctx context.Context, d time.Duration) {
	select {
	case <-ctx.Done():
	case <-time.After(d):
	}
}

func (s *Service) registerTools() {
	srv := s.server

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_devices",
		Description: "List the managed Android instances with their ids. You rarely need this: you already operate on the active device. Only use it when the user explicitly names a different device to switch to.",
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
			out.Devices = append(out.Devices, deviceInfo{ID: inst.ID, Name: inst.Name, Platform: inst.Platform, State: inst.State, DeviceID: inst.DeviceID, Android: inst.AndroidVersion})
		}
		return nil, out, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "select_device",
		Description: "Switch the active device. Only needed when the user names a different device; pass an id exactly as returned by list_devices. Never invent an id.",
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
		return nil, deviceInfo{ID: inst.ID, Name: inst.Name, Platform: inst.Platform, State: inst.State, DeviceID: inst.DeviceID, Android: inst.AndroidVersion}, nil
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
		return nil, deviceInfo{ID: inst.ID, Name: inst.Name, Platform: inst.Platform, State: inst.State, DeviceID: inst.DeviceID, Android: inst.AndroidVersion}, nil
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
		if err := s.requireAndroidDevice(ctx, id); err != nil {
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
		Name:        "scroll",
		Description: "Scroll the screen to reveal off-screen content. direction is one of: down, up, left, right (down reveals content further down the page). Optionally set amount (default 3) for how far, and x/y to scroll a specific pane rather than the screen centre. Use this instead of computing swipe coordinates.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		Direction string `json:"direction" jsonschema:"one of: down, up, left, right"`
		Amount    int    `json:"amount,omitempty" jsonschema:"how far to scroll (wheel notches / screen-thirds); default 3"`
		X         int    `json:"x,omitempty" jsonschema:"optional point to scroll over (e.g. inside a specific pane)"`
		Y         int    `json:"y,omitempty" jsonschema:"optional point to scroll over"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, statusOut{}, err
		}
		amount := in.Amount
		if amount <= 0 {
			amount = 3
		}
		dx, dy := 0, amount
		switch strings.ToLower(strings.TrimSpace(in.Direction)) {
		case "up":
			dx, dy = 0, -amount
		case "left":
			dx, dy = -amount, 0
		case "right":
			dx, dy = amount, 0
		}
		x, y := in.X, in.Y
		if x <= 0 && y <= 0 {
			// Default to the screen centre; the driver resolves this per platform.
			inst, err := s.store.GetInstance(ctx, id)
			if err != nil {
				return nil, statusOut{}, err
			}
			w, h := inst.DisplayWidth, inst.DisplayHeight
			if w <= 0 || h <= 0 {
				if iw, ih, e := s.devices.InputSize(ctx, id); e == nil && iw > 0 && ih > 0 {
					w, h = iw, ih
				} else {
					w, h = 720, 1280
				}
			}
			x, y = w/2, h/2
		}
		// Scroll uses a real wheel on desktop and an equivalent swipe on Android.
		if err := s.devices.Scroll(ctx, id, x, y, dx, dy); err != nil {
			return nil, statusOut{}, err
		}
		return nil, statusOut{Status: "ok", Device: id}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "click",
		Description: "Click exact coordinates with a specific mouse button — use for right-click (context menus) and double-click (open an item). button is left/right/middle (default left), count 1 or 2 (default 1). DESKTOP ONLY for right/middle; a touchscreen has no such buttons. For a plain left click prefer tap_element or tap.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		X      int    `json:"x" jsonschema:"x pixel coordinate"`
		Y      int    `json:"y" jsonschema:"y pixel coordinate"`
		Button string `json:"button,omitempty" jsonschema:"left, right, or middle; default left"`
		Count  int    `json:"count,omitempty" jsonschema:"number of clicks; 2 for a double-click. default 1"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, statusOut{}, err
		}
		if err := s.devices.Click(ctx, id, in.X, in.Y, in.Button, in.Count); err != nil {
			return nil, statusOut{}, err
		}
		return nil, statusOut{Status: "ok", Device: id}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "press_chord",
		Description: "Press a keyboard combination together, e.g. keys ['CTRL','C'] to copy, ['ALT','TAB'] to switch windows, ['WIN','R'] to open Run, ['ALT','F4'] to close. Modifiers are CTRL/ALT/SHIFT/WIN and must come before the final key. DESKTOP ONLY — Android has no modifier keys, use press_key there.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		Keys []string `json:"keys" jsonschema:"the combination in order, modifiers first, e.g. [\"CTRL\",\"C\"]"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, statusOut{}, err
		}
		if len(in.Keys) == 0 {
			return nil, statusOut{}, fmt.Errorf("press_chord needs a non-empty keys array, e.g. [\"CTRL\",\"C\"]")
		}
		if err := s.devices.Chord(ctx, id, in.Keys); err != nil {
			return nil, statusOut{}, err
		}
		return nil, statusOut{Status: "ok", Device: id}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "type_text",
		Description: "Type UTF-8 text into the currently focused input field. Tap the target field first (tap_element) so it has focus.",
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
		Description: "Press a key. Android: HOME, BACK, APP_SWITCH (recents), ENTER, VOLUME_UP, VOLUME_DOWN, POWER, or a numeric keycode. Desktop (Windows/Linux/macOS): ENTER, TAB, ESC, BACKSPACE, DELETE, UP, DOWN, LEFT, RIGHT, PAGEUP, PAGEDOWN, WIN.",
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
		Description: "Launch an installed app by exact package name (e.g. com.android.settings). If unsure of the package, call list_apps — never guess or invent a package name.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		Package string `json:"package"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, statusOut{}, err
		}
		if err := s.requireAndroidDevice(ctx, id); err != nil {
			return nil, statusOut{}, err
		}
		if err := s.devices.LaunchApp(ctx, id, in.Package); err != nil {
			return nil, statusOut{}, fmt.Errorf("could not launch %q — it may not be installed. Call list_apps for exact installed package names, or tap the app's icon with tap_element. Do not invent package names", in.Package)
		}
		return nil, statusOut{Status: "ok", Device: id}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "open_app",
		Description: "Open an application by its display name (e.g. \"Settings\", \"Notepad\", \"Chrome\") on ANY platform — the preferred way to open an app. On a desktop it opens the OS launcher (Start menu), types the name, and presses Enter; on Android it launches the matching app. After calling it, use ask_screen to confirm the app opened.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		Name string `json:"name" jsonschema:"the app's display name, e.g. Settings"`
	}) (*mcpsdk.CallToolResult, statusOut, error) {
		id, err := s.resolveDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, statusOut{}, err
		}
		inst, err := s.store.GetInstance(ctx, id)
		if err != nil {
			return nil, statusOut{}, err
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			return nil, statusOut{}, fmt.Errorf("name is required")
		}
		if inst.Platform == "" || inst.Platform == domain.PlatformAndroid {
			pkg := androidPackages[strings.ToLower(name)]
			if pkg == "" {
				return nil, statusOut{}, fmt.Errorf("don't know the Android package for %q — call list_apps and use launch_app with the exact package name", name)
			}
			if err := s.devices.LaunchApp(ctx, id, pkg); err != nil {
				return nil, statusOut{}, err
			}
			return nil, statusOut{Status: "ok", Device: id}, nil
		}
		// Desktop: the runner resolves the name against the Start menu and launches
		// it, reporting back which app it matched so the agent can verify (and
		// re-issue with a more specific name if it opened the wrong one).
		opened, err := s.devices.OpenApp(ctx, id, name)
		if err != nil {
			return nil, statusOut{}, err
		}
		return nil, statusOut{Status: "opened " + opened, Device: id}, nil
	})

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "list_apps",
		Description: "List installed/launchable apps on ANY platform — package names on Android, Start-menu app names on a desktop. (thirdPartyOnly excludes system apps on Android.) Open one with open_app.",
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
		Description: "Return what's in the foreground on ANY platform — the package/activity on Android, or the active window title on a desktop.",
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
		Name:        "tap",
		Description: "Tap exact screen coordinates (pixels) in the device's input space — the SAME space get_ui_tree bounds use. Deterministic (no vision), so it never mis-identifies which element you meant. Use it when tap_element keeps hitting the wrong thing: call get_ui_tree, take the target element's bounds [x1,y1][x2,y2], and tap its center x=(x1+x2)/2, y=(y1+y2)/2. Prefer tap_element for anything you can describe by sight.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		deviceRef
		X int `json:"x" jsonschema:"x pixel coordinate in the device's input space (e.g. the horizontal center of a get_ui_tree element's bounds)"`
		Y int `json:"y" jsonschema:"y pixel coordinate in the device's input space (e.g. the vertical center of a get_ui_tree element's bounds)"`
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
		if s.vqa == nil && s.vision == nil {
			return nil, out, errVisionUnavailable
		}
		png, err := s.devices.Screenshot(ctx, id)
		if err != nil {
			return nil, out, err
		}
		// Prefer real visual Q&A via the configured LLM; fall back to the local
		// caption model on any error (unsupported provider, network, bad config).
		if s.vqa != nil {
			if answer, qErr := s.vqa.Query(ctx, png, in.Question); qErr == nil && strings.TrimSpace(answer) != "" {
				out.Answer = answer
				return nil, out, nil
			} else if qErr != nil && s.logger != nil {
				s.logger.Debug("ask_screen VQA failed; falling back to caption", "error", qErr)
			}
		}
		if s.vision == nil {
			return nil, out, errVisionUnavailable
		}
		answer, err := s.vision.Query(ctx, png, in.Question)
		if err != nil {
			return nil, out, err
		}
		out.Answer = answer
		return nil, out, nil
	})

	// report_step_result is how an automated test cycle captures per-step results.
	// It is a safe no-op during interactive agent chat (no run is bound), so it
	// never disrupts the AgentTab.
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "report_step_result",
		Description: "Report the outcome of the test-case step you just performed and verified. Call this exactly once per numbered step, right after checking its Expected Result. status must be 'pass', 'fail', or 'blocked'; note briefly states what you observed. Only meaningful while running an automated test cycle — a harmless no-op otherwise.",
	}, func(ctx context.Context, _ *mcpsdk.CallToolRequest, in struct {
		Index  int    `json:"index" jsonschema:"the 1-based step number being reported"`
		Status string `json:"status" jsonschema:"pass, fail, or blocked"`
		Note   string `json:"note,omitempty" jsonschema:"what you observed that decided the result"`
	}) (*mcpsdk.CallToolResult, struct {
		Recorded bool `json:"recorded"`
	}, error) {
		var out struct {
			Recorded bool `json:"recorded"`
		}
		id, err := s.resolveDevice(ctx, "")
		if err != nil {
			return nil, out, nil // no device to attribute to; do not error the agent
		}
		s.mu.Lock()
		bound := s.runs[id]
		s.mu.Unlock()
		if bound == nil || s.sink == nil {
			return nil, out, nil // interactive chat: nothing to record
		}

		status := strings.ToLower(strings.TrimSpace(in.Status))
		if status != domain.StepFail && status != domain.StepBlocked {
			status = domain.StepPass
		}
		text := fmt.Sprintf("Step %d", in.Index)
		if in.Index >= 1 && in.Index <= len(bound.stepTexts) {
			text = bound.stepTexts[in.Index-1]
		}
		step := domain.StepResult{
			Text:   text,
			Status: status,
			Pass:   status == domain.StepPass,
			Detail: strings.TrimSpace(in.Note),
		}

		// Best-effort screenshot evidence into the run directory.
		s.mu.Lock()
		n := bound.steps
		bound.steps++
		runDir, runID := bound.runDir, bound.runID
		s.mu.Unlock()
		if runDir != "" {
			if png, shotErr := s.devices.Screenshot(ctx, id); shotErr == nil {
				name := fmt.Sprintf("step-%02d.png", n)
				if os.WriteFile(filepath.Join(runDir, name), png, 0o644) == nil {
					step.Screenshot = name
				}
			}
		}
		if err := s.sink.AppendStep(ctx, runID, step); err != nil {
			return nil, out, err
		}
		out.Recorded = true
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
	points, err := s.vision.Locate(ctx, png, description)
	if err != nil {
		return 0, 0, false, err
	}
	if len(points) == 0 {
		return 0, 0, false, nil
	}
	// Scale the normalized point by the INPUT coordinate space, not the screenshot
	// resolution: on Android `input tap` uses `wm size`, which can differ from the
	// `screencap` resolution (a display-size override), which would otherwise land
	// the tap off-target. Fall back to the screenshot dimensions (desktops, or if
	// wm size is unavailable).
	w, h := cfg.Width, cfg.Height
	if iw, ih, e := s.devices.InputSize(ctx, id); e == nil && iw > 0 && ih > 0 {
		w, h = iw, ih
	}
	x, y = points[0].Pixels(w, h)
	return x, y, true, nil
}
