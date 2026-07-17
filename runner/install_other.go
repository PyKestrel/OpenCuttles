//go:build !windows && !linux && !darwin

package main

import (
	"fmt"
	"runtime"
)

func runInstall(appliance, token string) error {
	return fmt.Errorf("auto-start install is not implemented for %s — run the runner directly instead", runtime.GOOS)
}

func runUninstall() error {
	return fmt.Errorf("auto-start install is not implemented for %s", runtime.GOOS)
}
