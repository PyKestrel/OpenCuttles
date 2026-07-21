//go:build linux

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

func installBinPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "share", "opencuttles", installedBinName), nil
}

func autostartPath() (string, error) {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", autostartFile), nil
}

// runInstall installs a per-user XDG autostart entry (runs at graphical login)
// and starts the runner now so it connects immediately. A user-level autostart —
// not a system service — because the runner needs the logged-in X11 session.
func runInstall(e enrollment) error {
	binPath, err := installBinPath()
	if err != nil {
		return err
	}
	if err := copySelf(binPath); err != nil {
		return fmt.Errorf("copy runner to %s: %w", binPath, err)
	}
	// Before anything starts: the runner reads this at launch.
	if err := installIdentity(e); err != nil {
		return err
	}

	entryPath, err := autostartPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(entryPath), 0o755); err != nil {
		return err
	}
	// 0600: the entry embeds the enrollment token.
	if err := os.WriteFile(entryPath, []byte(desktopEntry(binPath, e)), 0o600); err != nil {
		return fmt.Errorf("write autostart entry: %w", err)
	}

	if err := startDetached(binPath, e); err != nil {
		fmt.Printf("Installed, but could not start it now (it will start at your next login): %v\n", err)
	} else {
		fmt.Println("OpenCuttles runner installed — it will auto-start at login and is connecting now.")
	}
	fmt.Printf("Installed to %s (autostart: %s). Remove with: opencuttles-runner uninstall\n", binPath, entryPath)
	return nil
}

func runUninstall() error {
	entryPath, err := autostartPath()
	if err != nil {
		return err
	}
	if err := os.Remove(entryPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove autostart entry: %w", err)
	}
	return nil
}

// startDetached launches the installed runner in its own session so it survives
// the installing shell exiting.
func startDetached(binPath string, e enrollment) error {
	cmd := exec.Command(binPath, runArgs(e)...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	return cmd.Start()
}
