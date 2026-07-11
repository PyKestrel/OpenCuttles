//go:build !windows

package main

import "fmt"

// newScreen on non-Windows builds compiles but reports that control isn't
// implemented yet. Linux (X11) and macOS controllers land on this same seam.
func newScreen() (screen, error) {
	return nil, fmt.Errorf("desktop control is only implemented for Windows in this build")
}
