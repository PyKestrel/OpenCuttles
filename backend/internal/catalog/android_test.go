package catalog

import "testing"

func TestAndroidVersionsNotEmpty(t *testing.T) {
	versions := AndroidVersions()
	if len(versions) == 0 {
		t.Fatal("expected at least one Android version")
	}
	for _, version := range versions {
		if version.ID == "" || version.Branch == "" || version.BuildTarget == "" {
			t.Fatalf("incomplete version entry: %+v", version)
		}
	}
}

func TestLookup(t *testing.T) {
	version, ok := Lookup("aosp-main")
	if !ok {
		t.Fatal("expected aosp-main to be present")
	}
	if got, want := DefaultBuild(version), version.Branch+"/"+version.BuildTarget; got != want {
		t.Fatalf("default build = %q, want %q", got, want)
	}

	if _, ok := Lookup("does-not-exist"); ok {
		t.Fatal("expected lookup miss for unknown version")
	}
}

func TestDefaultIsFirst(t *testing.T) {
	if Default().ID != AndroidVersions()[0].ID {
		t.Fatalf("default %q is not the first catalog entry", Default().ID)
	}
}
