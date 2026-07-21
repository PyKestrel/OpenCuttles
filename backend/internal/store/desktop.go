package store

import (
	"context"
	"database/sql"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// CreateDesktopInstance registers a desktop target (Windows/Linux/macOS). Unlike
// Android instances these are not provisioned by the orchestrator: they start
// offline and come online when their runner dials home. tokenHash is the SHA-256
// of the enrollment token the runner presents (the plaintext is shown once and
// never stored). A placeholder image id satisfies the instances.image_id FK.
func (s *SQLite) CreateDesktopInstance(ctx context.Context, name, platform, tokenHash string) (domain.Instance, error) {
	img, err := s.GetOrCreateDefaultImage(ctx)
	if err != nil {
		return domain.Instance{}, err
	}
	now := time.Now().UTC()
	inst := domain.Instance{
		ID:              newID("dev"),
		Name:            name,
		HostID:          "local",
		Platform:        platform,
		Src:             domain.SourceRunner,
		ControlEndpoint: "tunnel", // dial-home; the runner connects to us
		ImageID:         img.ID,
		State:           domain.StateOffline,
		ConsoleProvider: domain.ConsoleProviderScreenshot,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO instances (
		id, name, host_id, image_id, android_version, state, cpu_cores, memory_mb,
		display_width, display_height, dpi, adb_port, webrtc_port, device_id,
		console_provider, console_url, last_error, created_at, updated_at,
		platform, control_endpoint, control_token_ciphertext, source
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.ID, inst.Name, inst.HostID, inst.ImageID, "", inst.State,
		0, 0, 0, 0, 0, 0, 0, "",
		inst.ConsoleProvider, "", "",
		formatTime(now), formatTime(now),
		inst.Platform, inst.ControlEndpoint, tokenHash, inst.Src)
	if err != nil {
		return domain.Instance{}, err
	}
	return inst, nil
}

// SetDesktopTokenHash replaces a desktop device's enrollment credential.
//
// Passing an empty hash revokes it: no presented token can match, because
// FindDesktopByTokenHash refuses an empty lookup outright (otherwise a runner
// sending no token would match every revoked device at once).
//
// Returns whether a desktop row was actually updated, so the caller can answer
// 404 rather than silently succeeding on an unknown or Android device.
func (s *SQLite) SetDesktopTokenHash(ctx context.Context, id, tokenHash string) (bool, error) {
	res, err := s.db.ExecContext(ctx,
		`UPDATE instances SET control_token_ciphertext = ?, updated_at = ?
		 WHERE id = ? AND source = 'runner'`,
		tokenHash, formatTime(time.Now().UTC()), id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// FindDesktopByTokenHash resolves a runner's presented enrollment token (hashed)
// to its device. Used to authenticate the dial-home tunnel.
func (s *SQLite) FindDesktopByTokenHash(ctx context.Context, tokenHash string) (domain.Instance, error) {
	if tokenHash == "" {
		return domain.Instance{}, sql.ErrNoRows
	}
	row := s.db.QueryRowContext(ctx,
		`SELECT `+instanceColumns+` FROM instances
		 WHERE control_token_ciphertext = ? AND source = 'runner' LIMIT 1`, tokenHash)
	return scanInstance(row)
}
