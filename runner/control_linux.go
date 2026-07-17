//go:build linux

package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// x11Screen drives an X11 desktop through xdotool (input) and whichever
// screenshot tool is installed. Shelling out keeps the runner cgo-free and
// avoids pulling an X11 binding into the build, at the cost of two runtime
// dependencies — which newScreen reports clearly rather than failing cryptically
// on the first click.
type x11Screen struct {
	shot screenshotTool
	// run executes a command and returns stdout; swapped in tests.
	run func(name string, args ...string) ([]byte, error)
}

// screenshotTool is one way to capture the root window to PNG bytes.
type screenshotTool struct {
	name     string
	args     []string // args producing PNG on stdout, or writing to the {} placeholder
	toStdout bool
}

// screenshotTools are tried in order of preference: stdout-capable tools first
// (no temp file), then file-based ones.
var screenshotTools = []screenshotTool{
	{name: "maim", args: []string{"--hidecursor"}, toStdout: true},
	{name: "import", args: []string{"-window", "root", "png:-"}, toStdout: true},
	{name: "scrot", args: []string{"--overwrite", "{}"}, toStdout: false},
	{name: "gnome-screenshot", args: []string{"-f", "{}"}, toStdout: false},
	{name: "spectacle", args: []string{"-b", "-n", "-o", "{}"}, toStdout: false},
}

func newScreen() (screen, error) {
	if os.Getenv("DISPLAY") == "" {
		if os.Getenv("WAYLAND_DISPLAY") != "" {
			return nil, fmt.Errorf("this looks like a Wayland session, which does not allow synthetic input from another process; log in to an X11/Xorg session (or enable XWayland with DISPLAY set) and restart the runner")
		}
		return nil, fmt.Errorf("no DISPLAY is set — the runner must run inside a graphical X11 session (not over a plain SSH shell)")
	}
	if _, err := exec.LookPath("xdotool"); err != nil {
		return nil, fmt.Errorf("xdotool is required for input control: install it (Debian/Ubuntu: sudo apt install xdotool; Fedora: sudo dnf install xdotool)")
	}
	shot, err := pickScreenshotTool()
	if err != nil {
		return nil, err
	}
	return &x11Screen{shot: shot, run: runCommand}, nil
}

func pickScreenshotTool() (screenshotTool, error) {
	for _, t := range screenshotTools {
		if _, err := exec.LookPath(t.name); err == nil {
			return t, nil
		}
	}
	var names []string
	for _, t := range screenshotTools {
		names = append(names, t.name)
	}
	return screenshotTool{}, fmt.Errorf("no screenshot tool found — install one of: %s (Debian/Ubuntu: sudo apt install maim)", strings.Join(names, ", "))
}

func runCommand(name string, args ...string) ([]byte, error) {
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(name, args...)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("%s %s: %w: %s", name, strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return stdout.Bytes(), nil
}

func (s *x11Screen) Screenshot() ([]byte, error) {
	if s.shot.toStdout {
		return s.run(s.shot.name, s.shot.args...)
	}
	tmp, err := os.CreateTemp("", "ocshot-*.png")
	if err != nil {
		return nil, err
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(path)

	args := make([]string, len(s.shot.args))
	for i, a := range s.shot.args {
		args[i] = strings.ReplaceAll(a, "{}", path)
	}
	if _, err := s.run(s.shot.name, args...); err != nil {
		return nil, err
	}
	return os.ReadFile(path)
}

func (s *x11Screen) Click(x, y int, button string, count int) error {
	args, err := x11ClickArgs(x, y, button, count)
	if err != nil {
		return err
	}
	_, err = s.run("xdotool", args...)
	return err
}

func (s *x11Screen) Scroll(x, y, dx, dy int) error {
	for _, args := range x11ScrollArgs(x, y, dx, dy) {
		if _, err := s.run("xdotool", args...); err != nil {
			return err
		}
	}
	return nil
}

func (s *x11Screen) Drag(x1, y1, x2, y2, durationMs int) error {
	if _, err := s.run("xdotool", "mousemove", strconv.Itoa(x1), strconv.Itoa(y1), "mousedown", "1"); err != nil {
		return err
	}
	steps := 10
	for i := 1; i <= steps; i++ {
		x := x1 + (x2-x1)*i/steps
		y := y1 + (y2-y1)*i/steps
		if _, err := s.run("xdotool", "mousemove", strconv.Itoa(x), strconv.Itoa(y)); err != nil {
			_, _ = s.run("xdotool", "mouseup", "1") // never leave the button stuck down
			return err
		}
		if durationMs > 0 {
			time.Sleep(time.Duration(durationMs/steps) * time.Millisecond)
		} else {
			time.Sleep(10 * time.Millisecond)
		}
	}
	_, err := s.run("xdotool", "mouseup", "1")
	return err
}

func (s *x11Screen) Type(text string) error {
	// "--" stops xdotool parsing text that begins with a dash as flags.
	_, err := s.run("xdotool", "type", "--delay", "12", "--", text)
	return err
}

func (s *x11Screen) Key(name string) error {
	k, err := x11Keysym(name)
	if err != nil {
		return err
	}
	_, err = s.run("xdotool", "key", k)
	return err
}

func (s *x11Screen) Chord(keys []string) error {
	combo, err := x11ChordKeysym(keys)
	if err != nil {
		return err
	}
	_, err = s.run("xdotool", "key", combo)
	return err
}

// desktopDirs are the standard locations of .desktop application entries.
var desktopDirs = []string{
	"/usr/share/applications",
	"/usr/local/share/applications",
	"/var/lib/flatpak/exports/share/applications",
}

func userDesktopDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local/share/applications")
	}
	return ""
}

func (s *x11Screen) desktopEntries() map[string]string { // display name → desktop id
	out := map[string]string{}
	dirs := append([]string{}, desktopDirs...)
	if u := userDesktopDir(); u != "" {
		dirs = append(dirs, u)
	}
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".desktop") {
				continue
			}
			content, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			name, hidden := parseDesktopEntry(content)
			if name == "" || hidden {
				continue
			}
			out[name] = strings.TrimSuffix(e.Name(), ".desktop")
		}
	}
	return out
}

func (s *x11Screen) ListApps() ([]string, error) {
	entries := s.desktopEntries()
	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	return names, nil
}

func (s *x11Screen) OpenApp(name string) (string, error) {
	entries := s.desktopEntries()
	names := make([]string, 0, len(entries))
	for n := range entries {
		names = append(names, n)
	}
	match := matchApp(names, name)
	if match == "" {
		return "", fmt.Errorf("no application matching %q — call list_apps for the exact names", name)
	}
	id := entries[match]
	if _, err := exec.LookPath("gtk-launch"); err != nil {
		return "", fmt.Errorf("gtk-launch is required to open apps by name: install it (Debian/Ubuntu: sudo apt install libgtk-3-bin)")
	}
	if _, err := s.run("gtk-launch", id+".desktop"); err != nil {
		return "", err
	}
	return match, nil
}

func (s *x11Screen) CurrentActivity() (string, error) {
	out, err := s.run("xdotool", "getactivewindow", "getwindowname")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// RunInstaller installs a downloaded build. Package installs need root, so the
// runner must be able to sudo without a password prompt (or the file must be a
// self-contained executable such as an AppImage/.run).
func (s *x11Screen) RunInstaller(path, args string) error {
	name, argv, needsChmod, err := linuxInstallCommand(path, args)
	if err != nil {
		return err
	}
	if needsChmod {
		if err := os.Chmod(path, 0o755); err != nil {
			return fmt.Errorf("make installer executable: %w", err)
		}
	}
	_, err = s.run(name, argv...)
	return err
}
