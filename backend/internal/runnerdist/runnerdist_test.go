package runnerdist

import "testing"

func TestParseTarget(t *testing.T) {
	tests := []struct {
		name              string
		wantOK            bool
		platform, arch    string
		downloadName      string
	}{
		{"opencuttles-runner-windows-amd64.exe", true, "windows", "amd64", "opencuttles-runner.exe"},
		{"opencuttles-runner-linux-amd64", true, "linux", "amd64", "opencuttles-runner"},
		{"opencuttles-runner-darwin-amd64", true, "macos", "amd64", "opencuttles-runner"},
		{"opencuttles-runner-darwin-arm64", true, "macos", "arm64", "opencuttles-runner"},
		{".gitkeep", false, "", "", ""},
		{"opencuttles-runner-freebsd-amd64", false, "", "", ""}, // unmapped OS
		{"opencuttles-runner-windows", false, "", "", ""},       // missing arch
		{"something-else", false, "", "", ""},
	}
	for _, tt := range tests {
		got, ok := parseTarget(tt.name)
		if ok != tt.wantOK {
			t.Errorf("%s: ok=%v, want %v", tt.name, ok, tt.wantOK)
			continue
		}
		if !ok {
			continue
		}
		if got.Platform != tt.platform || got.Arch != tt.arch || got.DownloadName != tt.downloadName {
			t.Errorf("%s: got %+v", tt.name, got)
		}
		// The user-facing download name must never leak the goos/goarch suffix.
		if got.DownloadName != "opencuttles-runner" && got.DownloadName != "opencuttles-runner.exe" {
			t.Errorf("%s: download name should be the plain runner name, got %q", tt.name, got.DownloadName)
		}
	}
}

// TestListIsWellFormed runs against whatever is embedded — nothing in a clean
// checkout (only .gitkeep), or the four cross-compiled binaries after a build.
// Either way, every returned target must be valid and the .gitkeep must never
// leak through as a bogus entry.
func TestListIsWellFormed(t *testing.T) {
	for _, target := range List() {
		if target.Platform == "" || target.Arch == "" {
			t.Errorf("List returned an incomplete target: %+v", target)
		}
		if target.DownloadName != "opencuttles-runner" && target.DownloadName != "opencuttles-runner.exe" {
			t.Errorf("List target has a leaked download name: %+v", target)
		}
		if (target.Platform == "windows") != (target.DownloadName == "opencuttles-runner.exe") {
			t.Errorf("only windows should carry the .exe download name: %+v", target)
		}
	}
	// Open on an unknown platform is an error, never a nil-panic.
	if _, _, err := Open("plan9", ""); err == nil {
		t.Error("Open on an unknown platform should error")
	}
}
