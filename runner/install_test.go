package main

import (
	"encoding/xml"
	"strings"
	"testing"
)

func TestWinRunCommand(t *testing.T) {
	got := winRunCommand(`C:\Program Files\OpenCuttles\opencuttles-runner.exe`, "http://host", "tok123")
	want := `"C:\Program Files\OpenCuttles\opencuttles-runner.exe" --appliance "http://host" --token "tok123"`
	if got != want {
		t.Errorf("winRunCommand =\n %q\nwant\n %q", got, want)
	}
	// The binary path is quoted so a Program Files-style space can't split the arg.
	if !strings.HasPrefix(got, `"`) {
		t.Error("binary path must be quoted")
	}
}

func TestDesktopEntry(t *testing.T) {
	got := desktopEntry("/home/me/.local/share/opencuttles/opencuttles-runner", "http://host", "tok123")
	for _, want := range []string{
		"[Desktop Entry]",
		"Type=Application",
		"X-GNOME-Autostart-enabled=true",
		`Exec="/home/me/.local/share/opencuttles/opencuttles-runner" --appliance "http://host" --token "tok123"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("desktop entry missing %q in:\n%s", want, got)
		}
	}
}

// The plist must be well-formed XML and carry the exact program arguments — a
// malformed plist means launchctl silently refuses to load it.
func TestLaunchAgentPlistIsValidXML(t *testing.T) {
	got := launchAgentPlist("/Users/me/Library/Application Support/OpenCuttles/opencuttles-runner", "http://host", "tok123")

	// Parses as XML (DOCTYPE and all).
	dec := xml.NewDecoder(strings.NewReader(got))
	for {
		_, err := dec.Token()
		if err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("plist is not valid XML: %v", err)
		}
	}
	for _, want := range []string{
		"<string>com.opencuttles.runner</string>",
		"<key>RunAtLoad</key>",
		"<true/>",
		"<string>--appliance</string>",
		"<string>http://host</string>",
		"<string>--token</string>",
		"<string>tok123</string>",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("plist missing %q", want)
		}
	}
	// Each argument is its own <string>, so a path with a space stays one arg.
	if !strings.Contains(got, "<string>/Users/me/Library/Application Support/OpenCuttles/opencuttles-runner</string>") {
		t.Errorf("binary path should be a single argument element:\n%s", got)
	}
}

func TestXMLEscape(t *testing.T) {
	// A token or URL containing XML metacharacters must not break the plist.
	if got := xmlEscape("a&b<c>"); got != "a&amp;b&lt;c&gt;" {
		t.Errorf("xmlEscape = %q", got)
	}
	plist := launchAgentPlist("/bin/x", "http://host?a=1&b=2", "tok")
	if strings.Contains(plist, "a=1&b=2") {
		t.Error("ampersand in the URL must be XML-escaped in the plist")
	}
	dec := xml.NewDecoder(strings.NewReader(plist))
	for {
		if _, err := dec.Token(); err != nil {
			if err.Error() == "EOF" {
				break
			}
			t.Fatalf("plist with special chars is not valid XML: %v", err)
		}
	}
}

func TestSameFile(t *testing.T) {
	if !sameFile("/a/b/c", "/a/b/c") {
		t.Error("identical paths should match")
	}
	if sameFile("/a/b/c", "/a/b/d") {
		t.Error("different paths should not match")
	}
}
