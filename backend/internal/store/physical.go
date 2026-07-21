package store

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// adbTargetPattern is what an ADB address may contain: a USB serial, or
// host:port for adb-over-TCP.
//
// This is validated on write because the value lands in an argv position
// (`adb -s <target> …`). No shell is involved, so this is not shell injection —
// but a target beginning with "-" would be read by adb as a *flag*, turning a
// device name into an option. Everything else here is ordinary hygiene.
var adbTargetPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:\-]{0,127}$`)

// ValidateADBTarget checks an operator-supplied ADB address.
func ValidateADBTarget(target string) error {
	t := strings.TrimSpace(target)
	if t == "" {
		return fmt.Errorf("an ADB target is required (a USB serial, or host:port)")
	}
	if !adbTargetPattern.MatchString(t) {
		return fmt.Errorf("%q is not a valid ADB target: use a USB serial or host:port "+
			"(letters, digits, dot, dash, underscore, colon; must not start with '-')", t)
	}
	return nil
}

// CreatePhysicalAndroid registers a physical Android device.
//
// Unlike a Cuttlefish instance this appliance does not provision anything: no
// ports are allocated, no cvd device id is synthesized, and there is no launch.
// The device starts offline and comes online when ADB can reach it — the same
// shape as a desktop runner, which is the existing precedent for "registered,
// not provisioned".
func (s *SQLite) CreatePhysicalAndroid(ctx context.Context, name, adbTarget string) (domain.Instance, error) {
	if strings.TrimSpace(name) == "" {
		return domain.Instance{}, fmt.Errorf("a device name is required")
	}
	if err := ValidateADBTarget(adbTarget); err != nil {
		return domain.Instance{}, err
	}
	target := strings.TrimSpace(adbTarget)

	// A device is identified by its ADB target; registering the same one twice
	// would give two rows racing to drive one handset.
	var existing string
	err := s.db.QueryRowContext(ctx,
		`SELECT name FROM instances WHERE adb_target = ? AND source = ? LIMIT 1`,
		target, domain.SourcePhysical).Scan(&existing)
	if err == nil {
		return domain.Instance{}, fmt.Errorf("ADB target %q is already registered as %q", target, existing)
	}

	img, err := s.GetOrCreateDefaultImage(ctx)
	if err != nil {
		return domain.Instance{}, err
	}
	now := time.Now().UTC()
	inst := domain.Instance{
		ID:              newID("phy"),
		Name:            name,
		HostID:          "local",
		Platform:        domain.PlatformAndroid,
		Src:             domain.SourcePhysical,
		ADBTarget:       target,
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
		platform, control_endpoint, source, adb_target
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		inst.ID, inst.Name, inst.HostID, inst.ImageID, "", inst.State,
		0, 0, 0, 0, 0, 0, 0, "",
		inst.ConsoleProvider, "", "",
		formatTime(now), formatTime(now),
		inst.Platform, "", inst.Src, inst.ADBTarget)
	if err != nil {
		return domain.Instance{}, err
	}
	return inst, nil
}

// ListPhysicalAndroid returns every registered physical device, for the
// reachability poller.
func (s *SQLite) ListPhysicalAndroid(ctx context.Context) ([]domain.Instance, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT `+instanceColumns+` FROM instances WHERE source = ? ORDER BY name`,
		domain.SourcePhysical)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]domain.Instance, 0)
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inst)
	}
	return out, rows.Err()
}

// UpdateInstanceDisplay records a device's real screen geometry.
//
// Needed because tap and scroll coordinates are in the device's input space.
// The ADB driver otherwise falls back to a hardcoded 720x1280, which is wrong
// for essentially every real handset — and it is re-read on each online
// transition rather than only at registration, since display size is a setting
// a user can change.
func (s *SQLite) UpdateInstanceDisplay(ctx context.Context, id string, width, height, dpi int) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE instances SET display_width = ?, display_height = ?, dpi = ?, updated_at = ? WHERE id = ?`,
		width, height, dpi, formatTime(time.Now().UTC()), id)
	return err
}
