//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// runKey is the per-user autostart location. HKCU needs no elevation, unlike a
// scheduled task or an HKLM entry.
const runKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

// Windows process creation flags for a fully detached child.
const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
)

// installBinPath is the stable location the runner lives at once installed.
func installBinPath() (string, error) {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "AppData", "Local")
	}
	return filepath.Join(base, "OpenCuttles", installedBinName+".exe"), nil
}

// runInstall copies the runner to a stable path, registers a per-user logon
// autostart via HKCU\...\Run, and starts it now. Per-user (not a service)
// because the runner drives the interactive desktop — a Session-0 service would
// only see a black screen — and HKCU so no admin prompt is needed.
func runInstall(appliance, token string) error {
	binPath, err := installBinPath()
	if err != nil {
		return err
	}
	if err := copySelf(binPath); err != nil {
		return fmt.Errorf("copy runner to %s: %w", binPath, err)
	}

	if err := autostartRegister(binPath, appliance, token); err != nil {
		return err
	}

	if err := startDetached(binPath, appliance, token); err != nil {
		fmt.Printf("Installed, but could not start it now (it will start at your next login): %v\n", err)
	} else {
		fmt.Println("OpenCuttles runner installed — it will auto-start at login and is connecting now.")
	}
	fmt.Printf("Installed to %s. Remove with: opencuttles-runner uninstall\n", binPath)
	return nil
}

func runUninstall() error {
	return autostartUnregister()
}

// autostartRegister writes the HKCU\...\Run value pointing at binPath. Shared by
// the CLI install and the tray's "Start at login" toggle.
func autostartRegister(binPath, appliance, token string) error {
	// /f overwrites an existing value so re-running install just updates the token.
	add := exec.Command("reg", "add", runKey, "/v", winRunValueName,
		"/t", "REG_SZ", "/d", winRunCommand(binPath, appliance, token), "/f")
	if out, err := add.CombinedOutput(); err != nil {
		return fmt.Errorf("set autostart registry value: %w: %s", err, out)
	}
	return nil
}

func autostartUnregister() error {
	// Ignore "value not found" so uninstall is idempotent.
	del := exec.Command("reg", "delete", runKey, "/v", winRunValueName, "/f")
	if out, err := del.CombinedOutput(); err != nil {
		if !isRegValueMissing(out) {
			return fmt.Errorf("remove autostart registry value: %w: %s", err, out)
		}
	}
	return nil
}

// autostartEnabled reports whether the HKCU\...\Run value is present.
func autostartEnabled() bool {
	q := exec.Command("reg", "query", runKey, "/v", winRunValueName)
	return q.Run() == nil
}

func isRegValueMissing(out []byte) bool {
	s := string(out)
	return containsFold(s, "unable to find") || containsFold(s, "cannot find")
}

// startDetached launches the installed runner in its own process, detached from
// the installing shell so it survives that shell closing.
func startDetached(binPath, appliance, token string) error {
	cmd := exec.Command(binPath, runArgs(appliance, token)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
	return cmd.Start()
}
