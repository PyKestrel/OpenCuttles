//go:build darwin

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// macScreen drives a macOS desktop with the built-in `screencapture` for images
// and AppleScript (System Events) for input, keeping the runner cgo-free and
// dependency-free out of the box.
//
// macOS gates synthetic input behind TCC: the process running the runner (the
// terminal app, or the runner itself if launched directly) must be granted
// Accessibility AND Screen Recording in System Settings › Privacy & Security,
// or every call fails with a permission error. newScreen surfaces that up front.
//
// AppleScript has no mouse-wheel verb, so a real wheel needs the optional
// `cliclick` helper; without it Scroll degrades to keyboard paging and says so.
type macScreen struct {
	hasCliclick bool
	run         func(name string, args ...string) ([]byte, error)
}

func newScreen() (screen, error) {
	if _, err := exec.LookPath("osascript"); err != nil {
		return nil, fmt.Errorf("osascript not found — this does not look like a macOS system")
	}
	s := &macScreen{run: runCommandDarwin}
	if _, err := exec.LookPath("cliclick"); err == nil {
		s.hasCliclick = true
	}
	// Probe System Events once so a missing Accessibility grant is reported now,
	// with the fix, instead of as a cryptic -1743 on the first click.
	if _, err := s.run("osascript", "-e", `tell application "System Events" to get name of first process`); err != nil {
		return nil, fmt.Errorf("cannot drive System Events — grant Accessibility to the app running this runner in System Settings › Privacy & Security › Accessibility (and Screen Recording for screenshots), then restart it: %w", err)
	}
	return s, nil
}

func runCommandDarwin(name string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

// Screenshot captures the main display. screencapture writes to a file (its
// stdout mode is not available on all versions), so this round-trips a temp PNG.
func (s *macScreen) Screenshot() ([]byte, error) {
	tmp, err := os.CreateTemp("", "ocshot-*.png")
	if err != nil {
		return nil, err
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(path)
	// -x silences the shutter sound; -C omits the cursor.
	if _, err := s.run("screencapture", "-x", "-t", "png", path); err != nil {
		return nil, fmt.Errorf("%w (Screen Recording permission may be missing)", err)
	}
	return os.ReadFile(path)
}

func (s *macScreen) Click(x, y int, button string, count int) error {
	upper := strings.ToUpper(strings.TrimSpace(button))
	needsCliclick := upper != "" && upper != "LEFT"
	if needsCliclick || (s.hasCliclick && count >= 2) {
		if !s.hasCliclick {
			return fmt.Errorf("%s-click needs the cliclick helper on macOS (AppleScript can only left-click): brew install cliclick", strings.ToLower(upper))
		}
		args, err := cliclickClickArgs(x, y, button, count)
		if err != nil {
			return err
		}
		_, err = s.run("cliclick", args...)
		return err
	}
	_, err := s.run("osascript", "-e", macClickScript(x, y, count))
	return err
}

// Scroll uses cliclick's wheel when available. AppleScript has no wheel verb, so
// without cliclick this degrades to keyboard paging — which real wheel surfaces
// (maps, canvases) ignore, so say so rather than silently doing nothing useful.
func (s *macScreen) Scroll(x, y, dx, dy int) error {
	if s.hasCliclick {
		if _, err := s.run("cliclick", fmt.Sprintf("m:%d,%d", x, y)); err != nil {
			return err
		}
		// cliclick scrolls vertically only, in wheel steps.
		if dy != 0 {
			amount := -dy // cliclick: positive scrolls up
			if _, err := s.run("cliclick", fmt.Sprintf("w:%d", amount)); err != nil {
				return err
			}
		}
		if dx != 0 {
			return fmt.Errorf("horizontal scrolling is not supported on macOS")
		}
		return nil
	}
	if dx != 0 {
		return fmt.Errorf("horizontal scrolling on macOS needs the cliclick helper: brew install cliclick")
	}
	key := "121" // Page Down
	n := dy
	if dy < 0 {
		key = "116" // Page Up
		n = -dy
	}
	for i := 0; i < n; i++ {
		if _, err := s.run("osascript", "-e", fmt.Sprintf(`tell application "System Events" to key code %s`, key)); err != nil {
			return err
		}
	}
	return nil
}

func (s *macScreen) Drag(x1, y1, x2, y2, durationMs int) error {
	if !s.hasCliclick {
		return fmt.Errorf("drag needs the cliclick helper on macOS (AppleScript cannot hold the mouse button): brew install cliclick")
	}
	if _, err := s.run("cliclick", fmt.Sprintf("m:%d,%d", x1, y1), fmt.Sprintf("dd:%d,%d", x1, y1)); err != nil {
		return err
	}
	steps := 10
	for i := 1; i <= steps; i++ {
		x := x1 + (x2-x1)*i/steps
		y := y1 + (y2-y1)*i/steps
		if _, err := s.run("cliclick", fmt.Sprintf("m:%d,%d", x, y)); err != nil {
			_, _ = s.run("cliclick", fmt.Sprintf("du:%d,%d", x, y)) // never leave it stuck
			return err
		}
		if durationMs > 0 {
			time.Sleep(time.Duration(durationMs/steps) * time.Millisecond)
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	_, err := s.run("cliclick", fmt.Sprintf("du:%d,%d", x2, y2))
	return err
}

func (s *macScreen) Type(text string) error {
	script := fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escapeAppleScript(text))
	_, err := s.run("osascript", "-e", script)
	return err
}

func (s *macScreen) Key(name string) error {
	script, err := macKeyScript(name)
	if err != nil {
		return err
	}
	_, err = s.run("osascript", "-e", script)
	return err
}

func (s *macScreen) Chord(keys []string) error {
	script, err := macChordScript(keys)
	if err != nil {
		return err
	}
	_, err = s.run("osascript", "-e", script)
	return err
}

// appDirs are where macOS applications live.
var appDirs = []string{"/Applications", "/Applications/Utilities", "/System/Applications", "/System/Applications/Utilities"}

func (s *macScreen) appNames() []string {
	seen := map[string]bool{}
	var out []string
	dirs := append([]string{}, appDirs...)
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, "Applications"))
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !strings.HasSuffix(e.Name(), ".app") {
				continue
			}
			name := strings.TrimSuffix(e.Name(), ".app")
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	sort.Strings(out)
	return out
}

func (s *macScreen) ListApps() ([]string, error) { return s.appNames(), nil }

func (s *macScreen) OpenApp(name string) (string, error) {
	match := matchApp(s.appNames(), name)
	if match == "" {
		return "", fmt.Errorf("no application matching %q — call list_apps for the exact names", name)
	}
	if _, err := s.run("open", "-a", match); err != nil {
		return "", err
	}
	return match, nil
}

func (s *macScreen) CurrentActivity() (string, error) {
	out, err := s.run("osascript", "-e", `tell application "System Events" to get name of first process whose frontmost is true`)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// RunInstaller installs a downloaded build. .pkg needs root, so the runner must
// be able to sudo without a password prompt.
func (s *macScreen) RunInstaller(path, args string) error {
	name, argv, err := darwinInstallCommand(path, args)
	if err != nil {
		return err
	}
	_, err = s.run(name, argv...)
	return err
}

