package retention

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeSettings struct{ days string }

func (f fakeSettings) GetSetting(_ context.Context, _ string) (string, error) { return f.days, nil }

func TestPruneRemovesOldRunDirsOnly(t *testing.T) {
	root := t.TempDir()
	mk := func(name string, age time.Duration) string {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		ts := time.Now().Add(-age)
		if err := os.Chtimes(dir, ts, ts); err != nil {
			t.Fatal(err)
		}
		return dir
	}
	oldRun := mk("run_old", 40*24*time.Hour)
	newRun := mk("run_new", 2*24*time.Hour)
	builds := mk("builds", 90*24*time.Hour) // must be preserved regardless of age

	p := New(root, fakeSettings{days: "30"}, nil)
	p.pruneOnce(context.Background())

	if _, err := os.Stat(oldRun); !os.IsNotExist(err) {
		t.Errorf("old run dir should be pruned")
	}
	if _, err := os.Stat(newRun); err != nil {
		t.Errorf("recent run dir should survive: %v", err)
	}
	if _, err := os.Stat(builds); err != nil {
		t.Errorf("builds dir must never be pruned by age: %v", err)
	}
}

func TestPruneDisabledWhenZero(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "run_ancient")
	_ = os.MkdirAll(dir, 0o755)
	ts := time.Now().Add(-365 * 24 * time.Hour)
	_ = os.Chtimes(dir, ts, ts)

	New(root, fakeSettings{days: "0"}, nil).pruneOnce(context.Background())
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("retention.days=0 should disable pruning, but dir was removed: %v", err)
	}
}
