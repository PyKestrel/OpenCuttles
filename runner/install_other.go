//go:build !windows && !linux && !darwin

package main

import (
	"fmt"
	"runtime"
)

// installBinPath has no stable home on unsupported platforms; dataDir falls back
// to the user config dir when this errors.
func installBinPath() (string, error) {
	return "", fmt.Errorf("no install location on %s", runtime.GOOS)
}

func runInstall(e enrollment) error {
	return fmt.Errorf("auto-start install is not implemented for %s — run the runner directly instead", runtime.GOOS)
}

func runUninstall() error {
	return fmt.Errorf("auto-start install is not implemented for %s", runtime.GOOS)
}
