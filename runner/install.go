package main

// Auto-start installation. The platform-specific file (install_windows.go /
// install_linux.go / install_darwin.go) implements runInstall/runUninstall; the
// content this file builds — the scheduled-task command, the .desktop entry, the
// LaunchAgent plist — is pure so it can be unit-tested from any host, following
// the same "keep the mapping testable" split as the desktop controllers.

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

const (
	// winRunValueName is the Windows HKCU\...\Run value name (per-user autostart,
	// no admin needed — a scheduled task would require elevation).
	winRunValueName = "OpenCuttlesRunner"
	// launchLabel is the macOS LaunchAgent label / plist basename.
	launchLabel = "com.opencuttles.runner"
	// autostartFile is the Linux XDG autostart entry basename.
	autostartFile = "opencuttles-runner.desktop"
	// installedBinName is the runner's filename once copied to its stable home.
	installedBinName = "opencuttles-runner"
)

// copySelf copies the currently-running executable to dst (creating parent
// directories), so auto-start points at a stable path rather than the temp/CWD
// location the one-line installer downloaded it to. A no-op when already there.
func copySelf(dst string) error {
	src, err := os.Executable()
	if err != nil {
		return err
	}
	if sameFile(src, dst) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func sameFile(a, b string) bool {
	ap, err1 := filepath.Abs(a)
	bp, err2 := filepath.Abs(b)
	return err1 == nil && err2 == nil && strings.EqualFold(ap, bp)
}

// containsFold reports whether substr occurs in s, case-insensitively.
func containsFold(s, substr string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(substr))
}

// runArgs is the argument list the auto-start entry runs the binary with.
func runArgs(appliance, token string) []string {
	return []string{"--appliance", appliance, "--token", token}
}

// ---- Windows: HKCU\...\Run registry value ----

// winRunCommand builds the command string stored in the Run key: the quoted
// binary path plus its run args, so a Program Files-style space can't split it.
func winRunCommand(binPath, appliance, token string) string {
	return fmt.Sprintf(`"%s" --appliance "%s" --token "%s"`, binPath, appliance, token)
}

// ---- Linux: XDG autostart .desktop ----

func desktopEntry(binPath, appliance, token string) string {
	// Exec values with spaces are wrapped in double quotes per the Desktop Entry
	// spec; the token is hex and the URL simple, so no inner escaping is needed.
	exec := fmt.Sprintf(`"%s" --appliance "%s" --token "%s"`, binPath, appliance, token)
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=OpenCuttles Runner\n" +
		"Comment=Dials home so the OpenCuttles appliance can drive this desktop\n" +
		"Exec=" + exec + "\n" +
		"X-GNOME-Autostart-enabled=true\n" +
		"NoDisplay=true\n"
}

// ---- macOS: LaunchAgent plist ----

func launchAgentPlist(binPath, appliance, token string) string {
	args := append([]string{binPath}, runArgs(appliance, token)...)
	var items strings.Builder
	for _, a := range args {
		items.WriteString("\n    <string>" + xmlEscape(a) + "</string>")
	}
	return `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key>
  <string>` + launchLabel + `</string>
  <key>ProgramArguments</key>
  <array>` + items.String() + `
  </array>
  <key>RunAtLoad</key>
  <true/>
  <key>KeepAlive</key>
  <true/>
  <key>ProcessType</key>
  <string>Interactive</string>
</dict>
</plist>
`
}

func xmlEscape(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}
