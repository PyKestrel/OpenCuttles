//go:build darwin

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

func installBinPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "OpenCuttles", installedBinName), nil
}

func launchAgentPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "LaunchAgents", launchLabel+".plist"), nil
}

// runInstall installs a per-user LaunchAgent (RunAtLoad + KeepAlive) and loads
// it, which starts the runner immediately. A LaunchAgent — not a daemon — so it
// runs in the user's GUI session, where the Accessibility/Screen-Recording
// grants the runner needs apply.
func runInstall(e enrollment) error {
	binPath, err := installBinPath()
	if err != nil {
		return err
	}
	if err := copySelf(binPath); err != nil {
		return fmt.Errorf("copy runner to %s: %w", binPath, err)
	}

	plistPath, err := launchAgentPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(plistPath), 0o755); err != nil {
		return err
	}
	// 0600: the plist embeds the enrollment token.
	if err := os.WriteFile(plistPath, []byte(launchAgentPlist(binPath, e)), 0o600); err != nil {
		return fmt.Errorf("write LaunchAgent: %w", err)
	}

	// Reload cleanly on re-install: unload the old definition first (ignored if
	// absent), then load -w to enable + start now.
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if out, err := exec.Command("launchctl", "load", "-w", plistPath).CombinedOutput(); err != nil {
		fmt.Printf("Installed, but could not start it now (it will start at your next login): %v: %s\n", err, out)
	} else {
		fmt.Println("OpenCuttles runner installed — it will auto-start at login and is connecting now.")
	}
	fmt.Printf("Installed to %s (LaunchAgent: %s). Remove with: opencuttles-runner uninstall\n", binPath, plistPath)
	return nil
}

func runUninstall() error {
	plistPath, err := launchAgentPath()
	if err != nil {
		return err
	}
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := os.Remove(plistPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove LaunchAgent: %w", err)
	}
	return nil
}
