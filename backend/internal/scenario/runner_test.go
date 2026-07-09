package scenario

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		step, verb, target, value string
	}{
		{"Open Settings", VerbOpen, "Settings", ""},
		{"open the Clock app", VerbOpen, "Clock", ""},
		{"launch Chrome", VerbOpen, "Chrome", ""},
		{"Tap Wi-Fi", VerbTap, "Wi-Fi", ""},
		{"click on the Sign in button", VerbTap, "Sign in button", ""},
		{"toggle the Airplane mode switch", VerbTap, "Airplane mode switch", ""},
		{"press back", VerbKey, "BACK", ""},
		{"tap home", VerbKey, "HOME", ""},
		{"Type hello world into the search field", VerbType, "search field", "hello world"},
		{`enter "user@example.com" into the email field`, VerbType, "email field", "user@example.com"},
		{"type 12345", VerbType, "", "12345"},
		{"scroll down", VerbSwipe, "down", ""},
		{"swipe up", VerbSwipe, "up", ""},
		{"assert Wi-Fi settings are shown", VerbAssert, "Wi-Fi settings", ""},
		{"verify that Airplane mode is visible", VerbAssert, "Airplane mode", ""},
		{"check Network & internet is displayed", VerbAssert, "Network & internet", ""},
		{"I should see Welcome", VerbAssert, "Welcome", ""},
		{"wait", VerbWait, "", ""},
		{"wait a moment", VerbWait, "", ""},
	}
	for _, c := range cases {
		verb, target, value := Classify(c.step)
		if verb != c.verb || target != c.target || value != c.value {
			t.Errorf("Classify(%q) = (%q,%q,%q), want (%q,%q,%q)", c.step, verb, target, value, c.verb, c.target, c.value)
		}
	}
}

func TestClassifyUnknown(t *testing.T) {
	verb, _, _ := Classify("do a barrel roll somehow")
	if verb != "" {
		t.Errorf("expected empty verb for unknown step, got %q", verb)
	}
}

func TestContainsLoosely(t *testing.T) {
	caption := "The image shows the Network & internet settings screen. On-screen text: Internet Calls SMS Airplane mode Hotspot"
	if !containsLoosely(caption, "Airplane mode") {
		t.Error("expected match for 'Airplane mode'")
	}
	if !containsLoosely(caption, "the internet settings") {
		t.Error("expected match with stop-word skipped")
	}
	if containsLoosely(caption, "Bluetooth devices") {
		t.Error("did not expect match for 'Bluetooth devices'")
	}
}
