package main

import (
	"strings"
	"testing"
)

// ---- shared app matching ----

func TestMatchApp(t *testing.T) {
	names := []string{"Google Chrome", "Chromium", "Settings", "Text Editor", "Terminal"}
	tests := []struct{ want, expect string }{
		{"Settings", "Settings"},    // exact
		{"settings", "Settings"},    // case-insensitive
		{"chromium", "Chromium"},    // exact
		{"chrome", "Google Chrome"}, // "Chromium" is NOT a prefix of "chrome", so the substring match wins
		{"chromi", "Chromium"},      // prefix beats substring
		{"google", "Google Chrome"}, // prefix
		{"term", "Terminal"},        // prefix
		{"editor", "Text Editor"},   // substring only
		{"nonexistent", ""},         // no match
		{"", ""},                    // empty
	}
	for _, tt := range tests {
		if got := matchApp(names, tt.want); got != tt.expect {
			t.Errorf("matchApp(%q) = %q, want %q", tt.want, got, tt.expect)
		}
	}
}

// ---- Linux / xdotool ----

func TestX11ClickArgs(t *testing.T) {
	got, err := x11ClickArgs(10, 20, "", 1)
	if err != nil {
		t.Fatalf("left click: %v", err)
	}
	if strings.Join(got, " ") != "mousemove 10 20 click 1" {
		t.Errorf("left click = %v", got)
	}

	got, _ = x11ClickArgs(5, 6, "right", 1)
	if strings.Join(got, " ") != "mousemove 5 6 click 3" {
		t.Errorf("right click should be X11 button 3, got %v", got)
	}
	got, _ = x11ClickArgs(5, 6, "middle", 1)
	if strings.Join(got, " ") != "mousemove 5 6 click 2" {
		t.Errorf("middle click should be X11 button 2, got %v", got)
	}
	got, _ = x11ClickArgs(1, 2, "left", 2)
	if !strings.Contains(strings.Join(got, " "), "--repeat 2") {
		t.Errorf("double click should repeat, got %v", got)
	}
	if _, err := x11ClickArgs(1, 2, "scroll", 1); err == nil {
		t.Error("bogus button should error")
	}
}

// X11 models the wheel as buttons 4/5 (up/down) and 6/7 (left/right). Getting
// the direction backwards is the classic bug here.
func TestX11ScrollArgs(t *testing.T) {
	down := x11ScrollArgs(100, 200, 0, 3)
	if len(down) != 1 {
		t.Fatalf("scroll down should be one invocation, got %v", down)
	}
	joined := strings.Join(down[0], " ")
	if !strings.Contains(joined, "--repeat 3") || !strings.HasSuffix(joined, " 5") {
		t.Errorf("scroll down should click button 5 three times, got %q", joined)
	}
	if !strings.HasPrefix(joined, "mousemove 100 200") {
		t.Errorf("scroll should move to the point first, got %q", joined)
	}
	up := x11ScrollArgs(0, 0, 0, -2)
	if len(up) != 1 {
		t.Fatalf("scroll up should be one invocation, got %v", up)
	}
	if joined := strings.Join(up[0], " "); !strings.Contains(joined, "--repeat 2") || !strings.HasSuffix(joined, " 4") {
		t.Errorf("scroll up should click button 4 twice, got %q", joined)
	}
	right := x11ScrollArgs(0, 0, 1, 0)
	if len(right) != 1 || !strings.HasSuffix(strings.Join(right[0], " "), " 7") {
		t.Errorf("scroll right should click button 7, got %v", right)
	}
	left := x11ScrollArgs(0, 0, -1, 0)
	if len(left) != 1 || !strings.HasSuffix(strings.Join(left[0], " "), " 6") {
		t.Errorf("scroll left should click button 6, got %v", left)
	}
	if got := x11ScrollArgs(0, 0, 0, 0); len(got) != 0 {
		t.Errorf("a zero scroll should do nothing, got %v", got)
	}
	if got := x11ScrollArgs(0, 0, 1, 1); len(got) != 2 {
		t.Errorf("a diagonal scroll needs both axes, got %v", got)
	}
}

func TestX11Keysym(t *testing.T) {
	tests := map[string]string{
		"ENTER": "Return", "enter": "Return",
		"PAGEDOWN": "Next", "PAGEUP": "Prior",
		"ESC": "Escape", "BACKSPACE": "BackSpace",
		"WIN": "super", "CTRL": "ctrl",
		"F5": "F5", "F12": "F12",
		"a": "a", "5": "5",
	}
	for in, want := range tests {
		got, err := x11Keysym(in)
		if err != nil || got != want {
			t.Errorf("x11Keysym(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"F13", "NOPE", ""} {
		if _, err := x11Keysym(bad); err == nil {
			t.Errorf("x11Keysym(%q) should error", bad)
		}
	}
}

func TestX11ChordKeysym(t *testing.T) {
	got, err := x11ChordKeysym([]string{"CTRL", "C"})
	if err != nil || got != "ctrl+c" {
		t.Errorf("ctrl+c = %q, %v", got, err)
	}
	got, _ = x11ChordKeysym([]string{"ALT", "TAB"})
	if got != "alt+Tab" {
		t.Errorf("alt+tab = %q", got)
	}
	got, _ = x11ChordKeysym([]string{"CTRL", "SHIFT", "N"})
	if got != "ctrl+shift+n" {
		t.Errorf("3-key chord = %q", got)
	}
	got, _ = x11ChordKeysym([]string{"WIN", "R"})
	if got != "super+r" {
		t.Errorf("win+r = %q", got)
	}
	if _, err := x11ChordKeysym(nil); err == nil {
		t.Error("empty chord should error")
	}
}

// An UPPERCASE keysym means Shift to xdotool/AppleScript, so ctrl+"C" would fire
// Ctrl+SHIFT+C — a different command in most apps. The portable API spells shift
// out as a modifier, so a lone letter must always normalise to lowercase.
func TestChordLettersNeverImplyShift(t *testing.T) {
	for _, keys := range [][]string{{"CTRL", "C"}, {"CTRL", "c"}} {
		got, err := x11ChordKeysym(keys)
		if err != nil || got != "ctrl+c" {
			t.Errorf("x11 %v = %q, %v; want ctrl+c", keys, got, err)
		}
	}
	// Explicit shift still works — via the modifier, not letter case.
	if got, _ := x11ChordKeysym([]string{"CTRL", "SHIFT", "C"}); got != "ctrl+shift+c" {
		t.Errorf("explicit shift = %q", got)
	}
	for _, keys := range [][]string{{"CMD", "C"}, {"CMD", "c"}} {
		got, err := macChordScript(keys)
		if err != nil || !strings.Contains(got, `keystroke "c"`) {
			t.Errorf("mac %v = %q, %v; want a lowercase keystroke", keys, got, err)
		}
	}
	if got, _ := macKeyScript("C"); !strings.Contains(got, `keystroke "c"`) {
		t.Errorf("macKeyScript(C) = %q, want lowercase", got)
	}
	if got, _ := x11Keysym("C"); got != "c" {
		t.Errorf("x11Keysym(C) = %q, want c", got)
	}
}

func TestParseDesktopEntry(t *testing.T) {
	name, hidden := parseDesktopEntry([]byte("[Desktop Entry]\nName=Text Editor\nExec=gedit\n"))
	if name != "Text Editor" || hidden {
		t.Errorf("got %q hidden=%v", name, hidden)
	}
	// Entries the launcher hides must not be offered to the agent.
	_, hidden = parseDesktopEntry([]byte("[Desktop Entry]\nName=Internal\nNoDisplay=true\n"))
	if !hidden {
		t.Error("NoDisplay=true should be hidden")
	}
	_, hidden = parseDesktopEntry([]byte("[Desktop Entry]\nName=Old\nHidden=true\n"))
	if !hidden {
		t.Error("Hidden=true should be hidden")
	}
	// The first Name wins (localized Name[xx] variants follow).
	name, _ = parseDesktopEntry([]byte("Name=First\nName=Second\n"))
	if name != "First" {
		t.Errorf("first Name should win, got %q", name)
	}
}

func TestLinuxInstallCommand(t *testing.T) {
	name, argv, chmod, err := linuxInstallCommand("/tmp/app.deb", "")
	if err != nil || name != "sudo" || chmod {
		t.Errorf(".deb = %q %v chmod=%v %v", name, argv, chmod, err)
	}
	if strings.Join(argv, " ") != "-n apt-get install -y /tmp/app.deb" {
		t.Errorf(".deb args = %v", argv)
	}
	if name, _, _, _ := linuxInstallCommand("/tmp/app.rpm", ""); name != "sudo" {
		t.Errorf(".rpm should use sudo, got %q", name)
	}
	// A self-contained binary runs directly but must be made executable first.
	name, argv, chmod, err = linuxInstallCommand("/tmp/App.AppImage", "")
	if err != nil || name != "/tmp/App.AppImage" || len(argv) != 0 || !chmod {
		t.Errorf(".AppImage = %q %v chmod=%v %v", name, argv, chmod, err)
	}
	// Explicit args always win.
	name, argv, _, err = linuxInstallCommand("/tmp/x.bin", "--silent --now")
	if err != nil || name != "/tmp/x.bin" || strings.Join(argv, " ") != "--silent --now" {
		t.Errorf("override = %q %v %v", name, argv, err)
	}
	if _, _, _, err := linuxInstallCommand("/tmp/mystery.xyz", ""); err == nil {
		t.Error("unknown type should error rather than guess")
	}
}

// ---- macOS ----

func TestEscapeAppleScript(t *testing.T) {
	if got := escapeAppleScript(`say "hi"`); got != `say \"hi\"` {
		t.Errorf("quotes not escaped: %s", got)
	}
	if got := escapeAppleScript(`back\slash`); got != `back\\slash` {
		t.Errorf("backslash not escaped: %s", got)
	}
}

func TestMacKeyScript(t *testing.T) {
	got, err := macKeyScript("ENTER")
	if err != nil || !strings.Contains(got, "key code 36") {
		t.Errorf("ENTER = %q, %v", got, err)
	}
	got, _ = macKeyScript("a")
	if !strings.Contains(got, `keystroke "a"`) {
		t.Errorf("single char = %q", got)
	}
	if _, err := macKeyScript("NOPE"); err == nil {
		t.Error("unknown key should error")
	}
}

func TestMacChordScript(t *testing.T) {
	got, err := macChordScript([]string{"CMD", "C"})
	if err != nil || !strings.Contains(got, `keystroke "c" using {command down}`) {
		t.Errorf("cmd+c = %q, %v", got, err)
	}
	// A portable ["CTRL","C"] must map to macOS's control, not silently to cmd.
	got, _ = macChordScript([]string{"CTRL", "C"})
	if !strings.Contains(got, "control down") {
		t.Errorf("ctrl+c = %q", got)
	}
	// WIN is the portable "primary modifier" and becomes command on macOS.
	got, _ = macChordScript([]string{"WIN", "R"})
	if !strings.Contains(got, "command down") {
		t.Errorf("win+r should map to command, got %q", got)
	}
	got, _ = macChordScript([]string{"CMD", "SHIFT", "4"})
	if !strings.Contains(got, "command down") || !strings.Contains(got, "shift down") {
		t.Errorf("multi-modifier chord = %q", got)
	}
	// A named key in a chord uses its key code, not a keystroke.
	got, _ = macChordScript([]string{"ALT", "TAB"})
	if !strings.Contains(got, "key code 48") || !strings.Contains(got, "option down") {
		t.Errorf("alt+tab = %q", got)
	}
	// F5's key code is 96 — a zero-value bug here would silently press the wrong key.
	got, _ = macChordScript([]string{"CMD", "F5"})
	if !strings.Contains(got, "key code 96") {
		t.Errorf("cmd+F5 = %q", got)
	}
	if _, err := macChordScript([]string{"C", "CMD"}); err == nil {
		t.Error("a non-modifier in a modifier position should error")
	}
	if _, err := macChordScript(nil); err == nil {
		t.Error("empty chord should error")
	}
}

func TestMacClickScript(t *testing.T) {
	got := macClickScript(10, 20, 1)
	if strings.Count(got, "click at {10, 20}") != 1 {
		t.Errorf("single click = %q", got)
	}
	got = macClickScript(1, 2, 2)
	if strings.Count(got, "click at {1, 2}") != 2 || !strings.Contains(got, "delay") {
		t.Errorf("double click should click twice with a delay: %q", got)
	}
}

func TestCliclickClickArgs(t *testing.T) {
	got, err := cliclickClickArgs(3, 4, "", 1)
	if err != nil || got[0] != "c:3,4" {
		t.Errorf("left = %v, %v", got, err)
	}
	got, _ = cliclickClickArgs(3, 4, "right", 1)
	if got[0] != "rc:3,4" {
		t.Errorf("right = %v", got)
	}
	got, _ = cliclickClickArgs(3, 4, "left", 2)
	if got[0] != "dc:3,4" {
		t.Errorf("double = %v", got)
	}
	if _, err := cliclickClickArgs(1, 1, "middle", 1); err == nil {
		t.Error("middle-click is unsupported on macOS and should error")
	}
}

func TestDarwinInstallCommand(t *testing.T) {
	name, argv, err := darwinInstallCommand("/tmp/app.pkg", "")
	if err != nil || name != "sudo" || strings.Join(argv, " ") != "-n installer -pkg /tmp/app.pkg -target /" {
		t.Errorf(".pkg = %q %v %v", name, argv, err)
	}
	if name, _, _ := darwinInstallCommand("/tmp/app.zip", ""); name != "unzip" {
		t.Errorf(".zip should unzip, got %q", name)
	}
	// .dmg has no reliable silent install — it must say so, not half-try.
	if _, _, err := darwinInstallCommand("/tmp/app.dmg", ""); err == nil {
		t.Error(".dmg should be rejected with guidance")
	} else if !strings.Contains(err.Error(), ".pkg") {
		t.Errorf(".dmg error should suggest an alternative: %v", err)
	}
	if _, _, err := darwinInstallCommand("/tmp/x.xyz", ""); err == nil {
		t.Error("unknown type should error")
	}
}
