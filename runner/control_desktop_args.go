package main

// Pure command/script construction for the Linux (X11/xdotool) and macOS
// (AppleScript/cliclick) controllers. This file is deliberately NOT build-tagged
// so the mapping logic — buttons, wheel directions, keysyms, chords, installer
// flags — is unit-testable from any platform, rather than only compiling on the
// target OS where it would go untested.

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// ---- shared ----

// matchApp finds an app by exact (case-insensitive) name, else a prefix match,
// else a substring — so "chrome" finds "Google Chrome". Ties break
// alphabetically to keep the choice deterministic.
func matchApp(names []string, want string) string {
	w := strings.ToLower(strings.TrimSpace(want))
	if w == "" {
		return ""
	}
	var prefix, contains string
	for _, n := range names {
		l := strings.ToLower(n)
		switch {
		case l == w:
			return n
		case strings.HasPrefix(l, w):
			if prefix == "" || n < prefix {
				prefix = n
			}
		case strings.Contains(l, w):
			if contains == "" || n < contains {
				contains = n
			}
		}
	}
	if prefix != "" {
		return prefix
	}
	return contains
}

// ---- Linux / X11 (xdotool) ----

// x11Buttons maps a button name to its X11 button number.
var x11Buttons = map[string]string{"": "1", "LEFT": "1", "MIDDLE": "2", "RIGHT": "3"}

// x11ClickArgs builds `xdotool mousemove X Y click [--repeat N] BUTTON`.
func x11ClickArgs(x, y int, button string, count int) ([]string, error) {
	b, ok := x11Buttons[strings.ToUpper(strings.TrimSpace(button))]
	if !ok {
		return nil, fmt.Errorf("unsupported mouse button %q (want left, right, or middle)", button)
	}
	if count <= 0 {
		count = 1
	}
	args := []string{"mousemove", strconv.Itoa(x), strconv.Itoa(y), "click"}
	if count > 1 {
		args = append(args, "--repeat", strconv.Itoa(count), "--delay", "60")
	}
	return append(args, b), nil
}

// x11ScrollArgs builds the xdotool invocations for a wheel scroll. X11 models
// the wheel as buttons: 4=up, 5=down, 6=left, 7=right.
func x11ScrollArgs(x, y, dx, dy int) [][]string {
	var out [][]string
	add := func(button string, n int) {
		if n <= 0 {
			return
		}
		out = append(out, []string{
			"mousemove", strconv.Itoa(x), strconv.Itoa(y),
			"click", "--repeat", strconv.Itoa(n), "--delay", "20", button,
		})
	}
	if dy > 0 {
		add("5", dy) // down
	} else if dy < 0 {
		add("4", -dy) // up
	}
	if dx > 0 {
		add("7", dx) // right
	} else if dx < 0 {
		add("6", -dx) // left
	}
	return out
}

// x11Keys maps portable key names to X11 keysyms.
var x11Keys = map[string]string{
	"ENTER": "Return", "RETURN": "Return",
	"TAB": "Tab", "ESC": "Escape", "ESCAPE": "Escape",
	"BACKSPACE": "BackSpace", "BACK": "BackSpace",
	"DELETE": "Delete", "DEL": "Delete",
	"SPACE": "space",
	"UP":    "Up", "DOWN": "Down", "LEFT": "Left", "RIGHT": "Right",
	"HOME": "Home", "END": "End", "PAGEUP": "Prior", "PAGEDOWN": "Next",
	"WIN": "super", "SUPER": "super", "META": "super", "CMD": "super", "COMMAND": "super",
	"CTRL": "ctrl", "CONTROL": "ctrl", "ALT": "alt", "OPTION": "alt", "SHIFT": "shift",
	"INSERT": "Insert", "PRINTSCREEN": "Print",
}

// x11Keysym resolves a portable key name to an X11 keysym, passing single
// characters through (xdotool accepts "a", "5", …).
func x11Keysym(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	upper := strings.ToUpper(trimmed)
	if k, ok := x11Keys[upper]; ok {
		return k, nil
	}
	if isFunctionKey(upper) {
		return upper, nil
	}
	if r := []rune(trimmed); len(r) == 1 {
		// xdotool reads an UPPERCASE keysym as Shift+key, so "C" would turn
		// ctrl+C into Ctrl+Shift+C — a different command in most apps. The
		// portable API expresses shift with an explicit SHIFT modifier, so a
		// lone letter is always normalised to lowercase.
		return strings.ToLower(string(r)), nil
	}
	return "", fmt.Errorf("unsupported key %q on this platform", name)
}

// isFunctionKey reports whether name is F1..F12.
func isFunctionKey(name string) bool {
	if !strings.HasPrefix(name, "F") || len(name) < 2 || len(name) > 3 {
		return false
	}
	n, err := strconv.Atoi(name[1:])
	return err == nil && n >= 1 && n <= 12
}

// x11ChordKeysym joins a combination into xdotool's "ctrl+shift+c" syntax.
func x11ChordKeysym(keys []string) (string, error) {
	if len(keys) == 0 {
		return "", fmt.Errorf("chord needs at least one key")
	}
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		sym, err := x11Keysym(k)
		if err != nil {
			return "", err
		}
		parts = append(parts, sym)
	}
	return strings.Join(parts, "+"), nil
}

// parseDesktopEntry pulls the display Name out of a .desktop file and reports
// whether the launcher would hide it.
func parseDesktopEntry(content []byte) (name string, hidden bool) {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Name=") && name == "":
			name = strings.TrimPrefix(line, "Name=")
		case line == "NoDisplay=true" || line == "Hidden=true":
			hidden = true
		}
	}
	return name, hidden
}

// linuxInstallCommand picks the silent install command for a build artifact.
// needsChmod marks self-contained executables that must be made runnable first.
func linuxInstallCommand(path, args string) (name string, argv []string, needsChmod bool, err error) {
	if strings.TrimSpace(args) != "" {
		return path, strings.Fields(args), true, nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".deb":
		return "sudo", []string{"-n", "apt-get", "install", "-y", path}, false, nil
	case ".rpm":
		return "sudo", []string{"-n", "dnf", "install", "-y", path}, false, nil
	case ".appimage", ".run", ".sh":
		return path, nil, true, nil
	default:
		return "", nil, false, fmt.Errorf("don't know how to install %q silently; set per-build installer args", filepath.Base(path))
	}
}

// ---- macOS (AppleScript / cliclick) ----

// escapeAppleScript quotes a string for embedding in an AppleScript literal.
func escapeAppleScript(s string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s)
}

// macKeyCodes maps portable key names to macOS virtual key codes.
var macKeyCodes = map[string]int{
	"ENTER": 36, "RETURN": 36,
	"TAB": 48, "ESC": 53, "ESCAPE": 53,
	"BACKSPACE": 51, "BACK": 51,
	"DELETE": 117, "DEL": 117,
	"SPACE": 49,
	"UP":    126, "DOWN": 125, "LEFT": 123, "RIGHT": 124,
	"HOME": 115, "END": 119, "PAGEUP": 116, "PAGEDOWN": 121,
	"F1": 122, "F2": 120, "F3": 99, "F4": 118, "F5": 96, "F6": 97,
	"F7": 98, "F8": 100, "F9": 101, "F10": 109, "F11": 103, "F12": 111,
}

// macModifiers maps modifier names to AppleScript "using" clause elements. WIN
// maps to command so a portable ["WIN","R"]-style chord still means "the
// platform's primary modifier".
var macModifiers = map[string]string{
	"CMD": "command down", "COMMAND": "command down", "WIN": "command down",
	"SUPER": "command down", "META": "command down",
	"CTRL": "control down", "CONTROL": "control down",
	"ALT": "option down", "OPTION": "option down",
	"SHIFT": "shift down",
}

// macKeyScript builds the AppleScript to press one key.
func macKeyScript(name string) (string, error) {
	trimmed := strings.TrimSpace(name)
	upper := strings.ToUpper(trimmed)
	if code, ok := macKeyCodes[upper]; ok {
		return fmt.Sprintf(`tell application "System Events" to key code %d`, code), nil
	}
	if r := []rune(trimmed); len(r) == 1 {
		return fmt.Sprintf(`tell application "System Events" to keystroke "%s"`, escapeAppleScript(normalizeChar(string(r)))), nil
	}
	return "", fmt.Errorf("unsupported key %q on this platform", name)
}

// normalizeChar lowercases a lone letter. AppleScript's `keystroke "C"` types a
// capital — i.e. Shift+c — so cmd+C would fire Cmd+Shift+C. Shift is expressed
// with an explicit SHIFT modifier instead.
func normalizeChar(s string) string { return strings.ToLower(s) }

// macChordScript builds `keystroke "c" using {command down}` style AppleScript.
// Modifiers come first; the final element is the key actually pressed.
func macChordScript(keys []string) (string, error) {
	if len(keys) == 0 {
		return "", fmt.Errorf("chord needs at least one key")
	}
	var mods []string
	for _, k := range keys[:len(keys)-1] {
		m, ok := macModifiers[strings.ToUpper(strings.TrimSpace(k))]
		if !ok {
			return "", fmt.Errorf("%q is not a modifier — put modifiers first and the real key last", k)
		}
		mods = append(mods, m)
	}
	final := strings.TrimSpace(keys[len(keys)-1])
	upper := strings.ToUpper(final)

	var action string
	if code, ok := macKeyCodes[upper]; ok {
		action = fmt.Sprintf("key code %d", code)
	} else if len([]rune(final)) == 1 {
		action = fmt.Sprintf(`keystroke "%s"`, escapeAppleScript(normalizeChar(final)))
	} else {
		return "", fmt.Errorf("unsupported key %q in chord", final)
	}
	if len(mods) == 0 {
		return fmt.Sprintf(`tell application "System Events" to %s`, action), nil
	}
	return fmt.Sprintf(`tell application "System Events" to %s using {%s}`, action, strings.Join(mods, ", ")), nil
}

// macClickScript builds AppleScript for a left click (System Events cannot
// right-click; callers fall back to cliclick).
func macClickScript(x, y, count int) string {
	if count <= 0 {
		count = 1
	}
	var b strings.Builder
	b.WriteString(`tell application "System Events"`)
	for i := 0; i < count; i++ {
		fmt.Fprintf(&b, "\n\tclick at {%d, %d}", x, y)
		if i < count-1 {
			b.WriteString("\n\tdelay 0.06")
		}
	}
	b.WriteString("\nend tell")
	return b.String()
}

// cliclickClickArgs builds a cliclick invocation for a click.
func cliclickClickArgs(x, y int, button string, count int) ([]string, error) {
	var verb string
	switch strings.ToUpper(strings.TrimSpace(button)) {
	case "", "LEFT":
		verb = "c"
		if count >= 2 {
			verb = "dc" // cliclick has a dedicated double-click
		}
	case "RIGHT":
		verb = "rc"
	case "MIDDLE":
		return nil, fmt.Errorf("middle-click is not supported on macOS")
	default:
		return nil, fmt.Errorf("unsupported mouse button %q (want left or right)", button)
	}
	return []string{fmt.Sprintf("%s:%d,%d", verb, x, y)}, nil
}

// darwinInstallCommand picks the silent install command for a build artifact. A
// .dmg needs mounting and a drag-install with no reliable silent form, so it is
// rejected with guidance rather than half-attempted.
func darwinInstallCommand(path, args string) (string, []string, error) {
	if strings.TrimSpace(args) != "" {
		return path, strings.Fields(args), nil
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pkg":
		return "sudo", []string{"-n", "installer", "-pkg", path, "-target", "/"}, nil
	case ".zip":
		return "unzip", []string{"-o", path, "-d", "/Applications"}, nil
	case ".dmg":
		return "", nil, fmt.Errorf(".dmg has no reliable silent install (it needs mounting and a drag-install); ship a .pkg or .zip, or set per-build installer args")
	default:
		return "", nil, fmt.Errorf("don't know how to install %q silently; set per-build installer args", filepath.Base(path))
	}
}
