package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// screen is the platform-specific desktop control surface. Coordinates are in
// screen pixels of the primary display.
type screen interface {
	Screenshot() ([]byte, error) // PNG bytes of the current screen
	Click(x, y int) error
	Drag(x1, y1, x2, y2, durationMs int) error
	Type(text string) error
	Key(name string) error
	ListApps() ([]string, error)         // installed/launchable app display names
	OpenApp(name string) (string, error) // launch by name; returns the app actually launched
	CurrentActivity() (string, error)    // foreground window / app title
	// RunInstaller runs a downloaded app-under-test installer silently. args
	// overrides the default silent flags for the file type; empty uses defaults.
	RunInstaller(path, args string) error
}

// controller maps the appliance's server-agnostic control vocabulary to the
// platform screen. This is the same vocabulary devicecontrol.mcpDriver speaks.
type controller struct {
	screen screen
	base   string // appliance base URL, for fetching build artifacts
	token  string // runner enrollment token

	mu       sync.Mutex
	installs map[string]*installState // buildID → progress
}

type installState struct {
	done bool
	err  string
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
	case "list_apps":
		apps, err := c.screen.ListApps()
		if err != nil {
			return nil, err
		}
		return map[string]any{"apps": apps}, nil
	case "open_app":
		var p struct{ Name string }
		_ = json.Unmarshal(params, &p)
		opened, err := c.screen.OpenApp(p.Name)
		if err != nil {
			return nil, err
		}
		return map[string]any{"opened": opened}, nil
	case "current_activity":
		act, err := c.screen.CurrentActivity()
		if err != nil {
			return nil, err
		}
		return map[string]any{"activity": act}, nil
	case "install_app":
		// Async: fetch + run the installer in the background and return at once
		// (installers can take minutes; the tunnel call timeout is short). The
		// appliance polls install_status.
		var p struct{ BuildID, Filename, Args string }
		_ = json.Unmarshal(params, &p)
		if p.BuildID == "" {
			return nil, fmt.Errorf("install_app needs a buildId")
		}
		c.mu.Lock()
		if c.installs == nil {
			c.installs = map[string]*installState{}
		}
		if _, running := c.installs[p.BuildID]; !running {
			c.installs[p.BuildID] = &installState{}
			go c.doInstall(p.BuildID, p.Filename, p.Args)
		}
		c.mu.Unlock()
		return map[string]string{"status": "installing"}, nil
	case "install_status":
		var p struct{ BuildID string }
		_ = json.Unmarshal(params, &p)
		c.mu.Lock()
		st := c.installs[p.BuildID]
		c.mu.Unlock()
		switch {
		case st == nil:
			return map[string]string{"status": "unknown"}, nil
		case st.err != "":
			return map[string]string{"status": "error", "detail": st.err}, nil
		case st.done:
			return map[string]string{"status": "done"}, nil
		default:
			return map[string]string{"status": "installing"}, nil
		}
	default:
		return nil, fmt.Errorf("unknown method %q", method)
	}
}

// doInstall fetches a build artifact from the appliance and runs it silently.
func (c *controller) doInstall(buildID, filename, args string) {
	path, err := c.fetchBuild(buildID, filename)
	if err == nil {
		defer os.Remove(path)
		err = c.screen.RunInstaller(path, args)
	}
	c.mu.Lock()
	if err != nil {
		c.installs[buildID].err = err.Error()
	} else {
		c.installs[buildID].done = true
	}
	c.mu.Unlock()
}

func (c *controller) fetchBuild(buildID, filename string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, c.base+"/api/v1/runner/build/"+buildID, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	client := &http.Client{Timeout: 10 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch build %s: HTTP %d", buildID, resp.StatusCode)
	}
	ext := filepath.Ext(filename)
	if ext == "" {
		ext = ".bin"
	}
	tmp, err := os.CreateTemp("", "ocbuild-*"+ext)
	if err != nil {
		return "", err
	}
	if _, err := io.Copy(tmp, resp.Body); err != nil {
		tmp.Close()
		return "", err
	}
	tmp.Close()
	return tmp.Name(), nil
}
