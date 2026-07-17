// Package retention prunes old on-disk test-run artifacts so evidence
// directories don't grow without bound. It removes run artifact directories
// (per-step screenshots + session video) older than a configurable window,
// leaving the small DB rows intact. Uploaded build installers are left alone —
// they're referenced by builds/cycles and are removed explicitly, not by age.
package retention

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SettingsReader reads the retention window (implemented by *store.SQLite).
type SettingsReader interface {
	GetSetting(ctx context.Context, key string) (string, error)
}

// RetentionDaysSettingKey holds the number of days to keep run artifacts; "0"
// (or a non-positive value) disables pruning.
const RetentionDaysSettingKey = "retention.days"

const (
	defaultDays  = 30
	pollInterval = 6 * time.Hour
)

// Pruner periodically deletes stale run artifact directories under root.
type Pruner struct {
	root     string
	settings SettingsReader
	logger   *slog.Logger
	interval time.Duration
	now      func() time.Time
}

// New builds a Pruner over the artifact root directory.
func New(root string, settings SettingsReader, logger *slog.Logger) *Pruner {
	return &Pruner{root: root, settings: settings, logger: logger, interval: pollInterval, now: time.Now}
}

// Run prunes once immediately, then every interval until ctx is cancelled.
func (p *Pruner) Run(ctx context.Context) {
	p.pruneOnce(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.pruneOnce(ctx)
		}
	}
}

func (p *Pruner) pruneOnce(ctx context.Context) {
	days := p.retentionDays(ctx)
	if days <= 0 {
		return // disabled
	}
	cutoff := p.now().Add(-time.Duration(days) * 24 * time.Hour)
	entries, err := os.ReadDir(p.root)
	if err != nil {
		return // artifact root may not exist yet
	}
	removed := 0
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "builds" || strings.HasPrefix(e.Name(), ".") {
			continue // never touch the builds tree or hidden dirs
		}
		info, err := e.Info()
		if err != nil || !info.ModTime().Before(cutoff) {
			continue
		}
		if err := os.RemoveAll(filepath.Join(p.root, e.Name())); err != nil {
			if p.logger != nil {
				p.logger.Warn("prune artifact dir failed", "dir", e.Name(), "error", err)
			}
			continue
		}
		removed++
	}
	if removed > 0 && p.logger != nil {
		p.logger.Info("pruned old run artifacts", "removed", removed, "olderThanDays", days)
	}
}

func (p *Pruner) retentionDays(ctx context.Context) int {
	raw, err := p.settings.GetSetting(ctx, RetentionDaysSettingKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return defaultDays
	}
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return defaultDays
	}
	return n
}
