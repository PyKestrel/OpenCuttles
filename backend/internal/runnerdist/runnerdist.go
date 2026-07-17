// Package runnerdist embeds prebuilt desktop-runner binaries so the dashboard
// can offer them as a direct download — no "clone the repo and go build" step.
//
// The bin directory is populated by the build (the Makefile cross-compiles the
// runner for each OS/arch into here before "go build"). A committed bin/.gitkeep
// keeps the embed directive valid even before the binaries have been built, so a
// plain `go build` of the API still compiles; the download endpoint then reports
// that no runners are bundled rather than failing.
package runnerdist

import (
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

//go:embed all:bin
var binFS embed.FS

// Target describes one downloadable runner build.
type Target struct {
	// Platform is the domain platform name (windows/linux/macos).
	Platform string `json:"platform"`
	// Arch is the CPU architecture (amd64/arm64).
	Arch string `json:"arch"`
	// DownloadName is the filename the user saves and runs.
	DownloadName string `json:"downloadName"`
	// SizeBytes is the binary size.
	SizeBytes int64 `json:"sizeBytes"`

	embeddedName string // filename inside bin/
}

// goosToPlatform maps a Go OS to the domain platform name.
var goosToPlatform = map[string]string{
	"windows": "windows",
	"linux":   "linux",
	"darwin":  "macos",
}

// platformToGoos is the inverse; macos → darwin is the one that trips people up.
var platformToGoos = map[string]string{
	"windows": "windows",
	"linux":   "linux",
	"macos":   "darwin",
}

// List returns the runner builds embedded in this server, sorted for stable
// output. Empty when the API was built without cross-compiling the runners.
func List() []Target {
	entries, err := binFS.ReadDir("bin")
	if err != nil {
		return nil
	}
	var targets []Target
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "opencuttles-runner-") {
			continue
		}
		t, ok := parseTarget(e.Name())
		if !ok {
			continue
		}
		if info, err := e.Info(); err == nil {
			t.SizeBytes = info.Size()
		}
		targets = append(targets, t)
	}
	sort.Slice(targets, func(i, j int) bool {
		if targets[i].Platform != targets[j].Platform {
			return targets[i].Platform < targets[j].Platform
		}
		return targets[i].Arch < targets[j].Arch
	})
	return targets
}

// Open returns a reader for the runner matching a domain platform + arch, along
// with its metadata. arch may be empty to accept the sole build for a platform.
func Open(platform, arch string) (fs.File, Target, error) {
	goos, ok := platformToGoos[platform]
	if !ok {
		return nil, Target{}, fmt.Errorf("unknown platform %q", platform)
	}
	var match *Target
	for _, t := range List() {
		if platformToGoos[t.Platform] != goos {
			continue
		}
		if arch != "" && t.Arch != arch {
			continue
		}
		if match != nil && arch == "" {
			return nil, Target{}, fmt.Errorf("multiple %s runners are available; specify an arch (%s or %s)", platform, match.Arch, t.Arch)
		}
		tt := t
		match = &tt
	}
	if match == nil {
		return nil, Target{}, fmt.Errorf("no runner bundled for %s/%s", platform, arch)
	}
	f, err := binFS.Open("bin/" + match.embeddedName)
	if err != nil {
		return nil, Target{}, err
	}
	return f, *match, nil
}

// parseTarget reads "opencuttles-runner-<goos>-<goarch>[.exe]".
func parseTarget(name string) (Target, bool) {
	base := strings.TrimSuffix(name, ".exe")
	rest := strings.TrimPrefix(base, "opencuttles-runner-")
	parts := strings.Split(rest, "-")
	if len(parts) != 2 {
		return Target{}, false
	}
	platform, ok := goosToPlatform[parts[0]]
	if !ok {
		return Target{}, false
	}
	dl := "opencuttles-runner"
	if parts[0] == "windows" {
		dl += ".exe"
	}
	return Target{
		Platform:     platform,
		Arch:         parts[1],
		DownloadName: dl,
		embeddedName: name,
	}, true
}
