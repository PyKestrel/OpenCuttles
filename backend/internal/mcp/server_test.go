package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"strings"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/opencuttles/opencuttles/backend/internal/devicecontrol"
	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

type fakeStore struct{ instances []domain.Instance }

func (f *fakeStore) ListInstances(context.Context) ([]domain.Instance, error) { return f.instances, nil }

func (f *fakeStore) GetInstance(_ context.Context, id string) (domain.Instance, error) {
	for _, inst := range f.instances {
		if inst.ID == id {
			return inst, nil
		}
	}
	return domain.Instance{}, fmt.Errorf("instance %s not found", id)
}

// decode round-trips a tool's structured content into v.
func decode(t *testing.T, res *mcpsdk.CallToolResult, v any) {
	t.Helper()
	raw, err := json.Marshal(res.StructuredContent)
	if err != nil {
		t.Fatalf("marshal structured content: %v", err)
	}
	if err := json.Unmarshal(raw, v); err != nil {
		t.Fatalf("unmarshal into %T: %v", v, err)
	}
}

func TestMCPToolsAndDeviceSelection(t *testing.T) {
	store := &fakeStore{instances: []domain.Instance{
		{ID: "cvd_a", Name: "Alpha", State: domain.StateRunning, DeviceID: "cvd_1-1-1"},
		{ID: "cvd_b", Name: "Beta", State: domain.StateStopped, DeviceID: "cvd_2-2-2"},
	}}
	devices := devicecontrol.NewService(store, nil, slog.Default())
	svc := New(devices, store, nil, slog.Default())

	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()

	ctx := context.Background()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	// tools/list should expose the core device tools.
	tools, err := session.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	got := map[string]bool{}
	for _, tool := range tools.Tools {
		got[tool.Name] = true
	}
	for _, want := range []string{"list_devices", "select_device", "get_active_device", "get_ui_tree", "scroll", "type_text", "press_key", "launch_app", "tap_element", "find_element", "ask_screen"} {
		if !got[want] {
			t.Errorf("missing tool %q", want)
		}
	}
	// The coordinate-based and image tools are intentionally not exposed to the
	// text-only agent (it uses tap_element/scroll/ask_screen instead).
	for _, gone := range []string{"tap", "swipe", "screenshot"} {
		if got[gone] {
			t.Errorf("tool %q should no longer be exposed to the agent", gone)
		}
	}

	// list_devices returns both instances.
	res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "list_devices"})
	if err != nil {
		t.Fatalf("list_devices: %v", err)
	}
	var devs struct {
		Devices []deviceInfo `json:"devices"`
	}
	decode(t, res, &devs)
	if len(devs.Devices) != 2 {
		t.Fatalf("expected 2 devices, got %d", len(devs.Devices))
	}

	// select_device sets the active device; get_active_device reflects it.
	if _, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "select_device", Arguments: map[string]any{"deviceId": "cvd_b"}}); err != nil {
		t.Fatalf("select_device: %v", err)
	}
	res, err = session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get_active_device"})
	if err != nil {
		t.Fatalf("get_active_device: %v", err)
	}
	var active deviceInfo
	decode(t, res, &active)
	if active.ID != "cvd_b" {
		t.Fatalf("active device = %q, want cvd_b", active.ID)
	}

	// A hallucinated explicit deviceId must produce a directive, model-readable
	// error — not a raw "sql: no rows in result set" the agent can't interpret.
	res, err = session.CallTool(ctx, &mcpsdk.CallToolParams{Name: "get_ui_tree", Arguments: map[string]any{"deviceId": "ghost"}})
	if err != nil {
		t.Fatalf("get_ui_tree(ghost): %v", err)
	}
	if !res.IsError {
		t.Fatalf("expected an error result for a bogus deviceId")
	}
	var text string
	for _, c := range res.Content {
		if tc, ok := c.(*mcpsdk.TextContent); ok {
			text += tc.Text
		}
	}
	if strings.Contains(text, "no rows") || !strings.Contains(text, "do not guess") {
		t.Errorf("bogus-id error not sanitized/directive: %q", text)
	}
}

// TestDesktopToolsRedirect verifies that the Android-only tools return a directive
// "this is a desktop, use tap_element/Start menu" error instead of running ADB, so
// a confused agent gets redirected rather than looping.
func TestDesktopToolsRedirect(t *testing.T) {
	store := &fakeStore{instances: []domain.Instance{
		{ID: "win_1", Name: "Laptop", Platform: domain.PlatformWindows, State: domain.StateOnline},
	}}
	devices := devicecontrol.NewService(store, nil, slog.Default())
	svc := New(devices, store, nil, slog.Default())
	ts := httptest.NewServer(svc.Handler())
	defer ts.Close()

	ctx := context.Background()
	client := mcpsdk.NewClient(&mcpsdk.Implementation{Name: "test", Version: "0"}, nil)
	session, err := client.Connect(ctx, &mcpsdk.StreamableClientTransport{Endpoint: ts.URL}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer session.Close()

	for _, tool := range []string{"launch_app", "list_apps", "get_ui_tree", "current_activity"} {
		args := map[string]any{"deviceId": "win_1"}
		if tool == "launch_app" {
			args["package"] = "com.android.settings"
		}
		res, err := session.CallTool(ctx, &mcpsdk.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
		if !res.IsError {
			t.Errorf("%s on a desktop should return an error", tool)
			continue
		}
		var text string
		for _, c := range res.Content {
			if tc, ok := c.(*mcpsdk.TextContent); ok {
				text += tc.Text
			}
		}
		if !strings.Contains(text, "DESKTOP") || !strings.Contains(text, "tap_element") {
			t.Errorf("%s error not directive for desktop: %q", tool, text)
		}
	}
}
