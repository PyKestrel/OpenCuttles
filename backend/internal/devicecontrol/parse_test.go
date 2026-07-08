package devicecontrol

import "testing"

const sampleDump = `<?xml version='1.0' encoding='UTF-8' standalone='yes' ?>
<hierarchy rotation="0">
  <node index="0" text="" resource-id="" class="android.widget.FrameLayout" package="com.android.settings" content-desc="" clickable="false" scrollable="false" focused="false" bounds="[0,0][1080,2340]">
    <node index="0" text="Settings" resource-id="com.android.settings:id/title" class="android.widget.TextView" package="com.android.settings" content-desc="Settings title" clickable="true" scrollable="false" focused="true" bounds="[40,100][300,180]" />
    <node index="1" text="Network" resource-id="com.android.settings:id/network" class="android.widget.TextView" package="com.android.settings" content-desc="" clickable="true" scrollable="false" focused="false" bounds="[0,200][1080,320]" />
  </node>
</hierarchy>
UI hierchary dumped to: /dev/tty`

func TestParseUIHierarchy(t *testing.T) {
	tree, err := ParseUIHierarchy([]byte(sampleDump))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(tree.Children) != 1 {
		t.Fatalf("expected 1 root node, got %d", len(tree.Children))
	}
	frame := tree.Children[0]
	if frame.Class != "android.widget.FrameLayout" {
		t.Fatalf("unexpected root class %q", frame.Class)
	}
	if len(frame.Children) != 2 {
		t.Fatalf("expected 2 children, got %d", len(frame.Children))
	}

	title := frame.Children[0]
	if title.Text != "Settings" {
		t.Errorf("text = %q, want Settings", title.Text)
	}
	if title.ResourceID != "com.android.settings:id/title" {
		t.Errorf("resourceId = %q", title.ResourceID)
	}
	if title.ContentDesc != "Settings title" {
		t.Errorf("contentDesc = %q", title.ContentDesc)
	}
	if !title.Clickable {
		t.Errorf("expected clickable")
	}
	if !title.Focused {
		t.Errorf("expected focused")
	}
	// Center of [40,100][300,180] = (170,140)
	if title.Center == nil || title.Center.X != 170 || title.Center.Y != 140 {
		t.Errorf("center = %+v, want {170 140}", title.Center)
	}
}

func TestParseUIHierarchyNoXML(t *testing.T) {
	if _, err := ParseUIHierarchy([]byte("error: could not dump")); err == nil {
		t.Fatal("expected error for missing hierarchy")
	}
}

func TestCenterOfBounds(t *testing.T) {
	p, ok := centerOfBounds("[0,200][1080,320]")
	if !ok {
		t.Fatal("expected parse ok")
	}
	if p.X != 540 || p.Y != 260 {
		t.Errorf("center = %+v, want {540 260}", p)
	}
	if _, ok := centerOfBounds("garbage"); ok {
		t.Error("expected failure on garbage bounds")
	}
}

func TestParseBatteryLevel(t *testing.T) {
	dump := "Current Battery Service state:\n  AC powered: false\n  level: 87\n  scale: 100\n"
	if got := parseBatteryLevel(dump); got != 87 {
		t.Errorf("battery level = %d, want 87", got)
	}
}

func TestNormalizeKeycode(t *testing.T) {
	cases := map[string]string{
		"HOME":         "KEYCODE_HOME",
		"keycode_back": "KEYCODE_BACK",
		"3":            "3",
		"":             "KEYCODE_UNKNOWN",
	}
	for in, want := range cases {
		if got := normalizeKeycode(in); got != want {
			t.Errorf("normalizeKeycode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEscapeInputText(t *testing.T) {
	if got := escapeInputText("hello world"); got != "hello%sworld" {
		t.Errorf("escape = %q", got)
	}
}
