//go:build !windows && !linux && !darwin

package main

import (
	"fmt"
	"runtime"
)

// newScreen on platforms without a controller compiles but reports that control
// isn't implemented. Windows, Linux (X11), and macOS have real implementations;
// this is the fallback for everything else (e.g. BSD).
func newScreen() (screen, error) {
	return nil, fmt.Errorf("desktop control is not implemented for %s — supported: windows, linux (X11), macos", runtime.GOOS)
}
