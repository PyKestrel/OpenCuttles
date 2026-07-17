package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

type SQLite struct {
	db *sql.DB
}

func OpenSQLite(path string) (*SQLite, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	for _, pragma := range []string{
		`PRAGMA journal_mode=WAL`,
		`PRAGMA busy_timeout=5000`,
		`PRAGMA foreign_keys=ON`,
		`PRAGMA synchronous=NORMAL`,
	} {
		if _, err := db.Exec(pragma); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	store := &SQLite{db: db}
	if err := store.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLite) Close() error {
	return s.db.Close()
}

func (s *SQLite) migrate(ctx context.Context) error {
	statements := []string{
		`CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS images (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			android_api TEXT NOT NULL DEFAULT '',
			description TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS instances (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			host_id TEXT NOT NULL,
			image_id TEXT NOT NULL,
			state TEXT NOT NULL,
			cpu_cores INTEGER NOT NULL,
			memory_mb INTEGER NOT NULL,
			adb_port INTEGER NOT NULL,
			webrtc_port INTEGER NOT NULL,
			console_provider TEXT NOT NULL,
			console_url TEXT NOT NULL,
			last_error TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			FOREIGN KEY(image_id) REFERENCES images(id)
		)`,
		`CREATE TABLE IF NOT EXISTS operations (
			id TEXT PRIMARY KEY,
			instance_id TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			status TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			finished_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_operations_created_at ON operations(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			display_name TEXT NOT NULL,
			role TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			disabled INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			token_hash TEXT NOT NULL UNIQUE,
			expires_at TEXT NOT NULL,
			created_at TEXT NOT NULL,
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_token_hash ON sessions(token_hash)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON sessions(expires_at)`,
		`CREATE TABLE IF NOT EXISTS audit_events (
			id TEXT PRIMARY KEY,
			actor_id TEXT NOT NULL DEFAULT '',
			actor_name TEXT NOT NULL DEFAULT '',
			action TEXT NOT NULL,
			resource TEXT NOT NULL,
			resource_id TEXT NOT NULL DEFAULT '',
			outcome TEXT NOT NULL,
			message TEXT NOT NULL DEFAULT '',
			source_ip TEXT NOT NULL DEFAULT '',
			user_agent TEXT NOT NULL DEFAULT '',
			request_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events(created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS settings (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS identity_providers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			issuer_url TEXT NOT NULL,
			client_id TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS external_identities (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			provider_id TEXT NOT NULL,
			subject TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(provider_id, subject),
			FOREIGN KEY(user_id) REFERENCES users(id) ON DELETE CASCADE,
			FOREIGN KEY(provider_id) REFERENCES identity_providers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS group_role_mappings (
			id TEXT PRIMARY KEY,
			provider_id TEXT NOT NULL,
			group_name TEXT NOT NULL,
			role TEXT NOT NULL,
			created_at TEXT NOT NULL,
			UNIQUE(provider_id, group_name),
			FOREIGN KEY(provider_id) REFERENCES identity_providers(id) ON DELETE CASCADE
		)`,
		`CREATE TABLE IF NOT EXISTS tests (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			steps TEXT NOT NULL,
			created_at TEXT NOT NULL
		)`,
		// test_id has no FK to tests: cycle case-runs set test_id='' (no parent
		// test) and reference a case_id instead. DeleteTest cascades runs in code.
		`CREATE TABLE IF NOT EXISTS test_runs (
			id TEXT PRIMARY KEY,
			test_id TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL,
			status TEXT NOT NULL,
			passed INTEGER NOT NULL DEFAULT 0,
			steps TEXT NOT NULL DEFAULT '[]',
			video TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			finished_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_test_runs_started_at ON test_runs(started_at DESC)`,
		// v4: QMetry-style test management — cases, cycles, cycle runs, builds.
		`CREATE TABLE IF NOT EXISTS test_cases (
			id TEXT PRIMARY KEY,
			summary TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			precondition TEXT NOT NULL DEFAULT '',
			priority TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT '',
			labels TEXT NOT NULL DEFAULT '[]',
			components TEXT NOT NULL DEFAULT '[]',
			folder_path TEXT NOT NULL DEFAULT '',
			steps TEXT NOT NULL DEFAULT '[]',
			external_key TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_test_cases_folder ON test_cases(folder_path)`,
		`CREATE TABLE IF NOT EXISTS test_cycles (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			platform TEXT NOT NULL DEFAULT 'android',
			build_id TEXT NOT NULL DEFAULT '',
			environment TEXT NOT NULL DEFAULT '',
			case_ids TEXT NOT NULL DEFAULT '[]',
			cron TEXT NOT NULL DEFAULT '',
			timezone TEXT NOT NULL DEFAULT '',
			on_new_build INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			last_run_at TEXT,
			next_run_at TEXT,
			created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS cycle_runs (
			id TEXT PRIMARY KEY,
			cycle_id TEXT NOT NULL,
			trigger TEXT NOT NULL DEFAULT 'manual',
			build_id TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'running',
			totals TEXT NOT NULL DEFAULT '{}',
			started_at TEXT NOT NULL,
			finished_at TEXT,
			FOREIGN KEY(cycle_id) REFERENCES test_cycles(id) ON DELETE CASCADE
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cycle_runs_started_at ON cycle_runs(started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS builds (
			id TEXT PRIMARY KEY,
			platform TEXT NOT NULL,
			filename TEXT NOT NULL,
			path TEXT NOT NULL,
			size_bytes INTEGER NOT NULL DEFAULT 0,
			version TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL DEFAULT 'uploaded',
			note TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_builds_platform ON builds(platform, created_at DESC)`,
		// Explicit case folders so empty folders persist (the tree otherwise derives
		// folders only from cases' folder_path).
		`CREATE TABLE IF NOT EXISTS case_folders (
			path TEXT PRIMARY KEY,
			created_at TEXT NOT NULL
		)`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}

	// v2: additive columns for Android-version image fetch + richer instance options.
	additive := []struct{ table, column, ddl string }{
		{"images", "build_target", `ALTER TABLE images ADD COLUMN build_target TEXT NOT NULL DEFAULT ''`},
		{"images", "version_id", `ALTER TABLE images ADD COLUMN version_id TEXT NOT NULL DEFAULT ''`},
		{"images", "status", `ALTER TABLE images ADD COLUMN status TEXT NOT NULL DEFAULT 'ready'`},
		{"images", "size_bytes", `ALTER TABLE images ADD COLUMN size_bytes INTEGER NOT NULL DEFAULT 0`},
		{"images", "last_error", `ALTER TABLE images ADD COLUMN last_error TEXT NOT NULL DEFAULT ''`},
		{"instances", "android_version", `ALTER TABLE instances ADD COLUMN android_version TEXT NOT NULL DEFAULT ''`},
		{"instances", "display_width", `ALTER TABLE instances ADD COLUMN display_width INTEGER NOT NULL DEFAULT 0`},
		{"instances", "display_height", `ALTER TABLE instances ADD COLUMN display_height INTEGER NOT NULL DEFAULT 0`},
		{"instances", "dpi", `ALTER TABLE instances ADD COLUMN dpi INTEGER NOT NULL DEFAULT 0`},
		{"instances", "device_id", `ALTER TABLE instances ADD COLUMN device_id TEXT NOT NULL DEFAULT ''`},
		// v3: multi-OS targets. Existing rows default to android; desktop targets
		// carry a computer-use MCP endpoint and an encrypted bearer token.
		{"instances", "platform", `ALTER TABLE instances ADD COLUMN platform TEXT NOT NULL DEFAULT 'android'`},
		{"instances", "control_endpoint", `ALTER TABLE instances ADD COLUMN control_endpoint TEXT NOT NULL DEFAULT ''`},
		{"instances", "control_token_ciphertext", `ALTER TABLE instances ADD COLUMN control_token_ciphertext TEXT NOT NULL DEFAULT ''`},
		// v4: per-case executions within a cycle run reuse the test_runs table.
		{"test_runs", "cycle_run_id", `ALTER TABLE test_runs ADD COLUMN cycle_run_id TEXT NOT NULL DEFAULT ''`},
		{"test_runs", "case_id", `ALTER TABLE test_runs ADD COLUMN case_id TEXT NOT NULL DEFAULT ''`},
		// v5: cron schedules interpret their wall-clock fields in this IANA zone.
		// Empty preserves the previous UTC-only behavior for existing rows.
		{"test_cycles", "timezone", `ALTER TABLE test_cycles ADD COLUMN timezone TEXT NOT NULL DEFAULT ''`},
	}
	for _, column := range additive {
		if err := s.ensureColumn(ctx, column.table, column.column, column.ddl); err != nil {
			return err
		}
	}

	// v4: drop the test_runs→tests foreign key on existing DBs so cycle case-runs
	// (test_id='') are allowed. Idempotent — only rebuilds if the FK is present.
	if err := s.rebuildTestRunsIfFK(ctx); err != nil {
		return err
	}

	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (1, ?)`, formatTime(time.Now().UTC()))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (2, ?)`, formatTime(time.Now().UTC()))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (3, ?)`, formatTime(time.Now().UTC()))
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (4, ?)`, formatTime(time.Now().UTC()))
	if err != nil {
		return err
	}
	return nil
}

// rebuildTestRunsIfFK recreates test_runs without the tests foreign key when an
// older schema still carries it, preserving all rows. Runs after the v4 additive
// columns so the copy includes cycle_run_id/case_id.
func (s *SQLite) rebuildTestRunsIfFK(ctx context.Context) error {
	var ddl string
	if err := s.db.QueryRowContext(ctx, `SELECT sql FROM sqlite_master WHERE type='table' AND name='test_runs'`).Scan(&ddl); err != nil {
		if err == sql.ErrNoRows {
			return nil
		}
		return err
	}
	if !strings.Contains(ddl, "FOREIGN KEY") {
		return nil // already FK-free
	}
	stmts := []string{
		`CREATE TABLE test_runs_new (
			id TEXT PRIMARY KEY,
			test_id TEXT NOT NULL DEFAULT '',
			instance_id TEXT NOT NULL,
			status TEXT NOT NULL,
			passed INTEGER NOT NULL DEFAULT 0,
			steps TEXT NOT NULL DEFAULT '[]',
			video TEXT NOT NULL DEFAULT '',
			error TEXT NOT NULL DEFAULT '',
			started_at TEXT NOT NULL,
			finished_at TEXT,
			cycle_run_id TEXT NOT NULL DEFAULT '',
			case_id TEXT NOT NULL DEFAULT ''
		)`,
		`INSERT INTO test_runs_new (id, test_id, instance_id, status, passed, steps, video, error, started_at, finished_at, cycle_run_id, case_id)
			SELECT id, test_id, instance_id, status, passed, steps, video, error, started_at, finished_at, cycle_run_id, case_id FROM test_runs`,
		`DROP TABLE test_runs`,
		`ALTER TABLE test_runs_new RENAME TO test_runs`,
		`CREATE INDEX IF NOT EXISTS idx_test_runs_started_at ON test_runs(started_at DESC)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("rebuild test_runs: %w", err)
		}
	}
	return nil
}

// ensureColumn adds a column when it is not already present so that schema
// upgrades on existing databases are idempotent.
func (s *SQLite) ensureColumn(ctx context.Context, table, column, ddl string) error {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return err
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var (
			cid     int
			name    string
			ctype   string
			notNull int
			dflt    sql.NullString
			pk      int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			found = true
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = s.db.ExecContext(ctx, ddl)
	return err
}

const imageColumns = `id, name, path, android_api, description, build_target, version_id, status, size_bytes, last_error, created_at`

func (s *SQLite) insertImage(ctx context.Context, image domain.Image) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO images (`+imageColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		image.ID, image.Name, image.Path, image.AndroidAPI, image.Description,
		image.BuildTarget, image.VersionID, image.Status, image.SizeBytes, image.LastError,
		formatTime(image.CreatedAt))
	return err
}

func (s *SQLite) CreateImage(ctx context.Context, req domain.CreateImageRequest) (domain.Image, error) {
	if err := ValidateImagePath(req.Path, false); err != nil {
		return domain.Image{}, err
	}
	now := time.Now().UTC()
	image := domain.Image{
		ID:          newID("img"),
		Name:        req.Name,
		Path:        req.Path,
		AndroidAPI:  req.AndroidAPI,
		Description: req.Description,
		Status:      domain.ImageStatusReady,
		CreatedAt:   now,
	}
	if err := s.insertImage(ctx, image); err != nil {
		return domain.Image{}, err
	}
	return image, nil
}

// GetOrCreateVersionImage returns the image row backing an Android version,
// creating a pending placeholder (to be populated by cvd fetch) if needed.
func (s *SQLite) GetOrCreateVersionImage(ctx context.Context, versionID, label, buildTarget string) (domain.Image, error) {
	path := filepath.Join(imageRoot(), versionID)
	if err := ValidateImagePath(path, false); err != nil {
		return domain.Image{}, err
	}
	row := s.db.QueryRowContext(ctx, `SELECT `+imageColumns+` FROM images WHERE version_id = ? ORDER BY created_at LIMIT 1`, versionID)
	image, err := scanImage(row)
	if err == nil {
		// Self-heal rows created by an older catalog: if the build target the
		// catalog now resolves to differs from what we stored, the previously
		// fetched artifacts (if any) are for the wrong target. Refresh the
		// target and force a re-fetch so "cvd fetch" gets the correct
		// "branch/build_target" instead of falling back to its default target.
		if image.BuildTarget != buildTarget {
			if _, err := s.db.ExecContext(ctx,
				`UPDATE images SET build_target = ?, name = ?, status = ?, last_error = '' WHERE id = ?`,
				buildTarget, label, domain.ImageStatusPending, image.ID); err != nil {
				return domain.Image{}, err
			}
			image.BuildTarget = buildTarget
			image.Name = label
			image.Status = domain.ImageStatusPending
			image.LastError = ""
		}
		return image, nil
	}
	if !IsNotFound(err) {
		return domain.Image{}, err
	}
	image = domain.Image{
		ID:          newID("img"),
		Name:        label,
		Path:        path,
		BuildTarget: buildTarget,
		VersionID:   versionID,
		Status:      domain.ImageStatusPending,
		Description: "Auto-fetched with cvd fetch by OpenCuttles.",
		CreatedAt:   time.Now().UTC(),
	}
	if err := s.insertImage(ctx, image); err != nil {
		return domain.Image{}, err
	}
	return image, nil
}

// UpdateImageStatus records progress of an image fetch.
func (s *SQLite) UpdateImageStatus(ctx context.Context, id, status string, sizeBytes int64, lastError string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE images SET status = ?, size_bytes = ?, last_error = ? WHERE id = ?`,
		status, sizeBytes, lastError, id)
	return err
}

func (s *SQLite) ListImages(ctx context.Context) ([]domain.Image, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+imageColumns+` FROM images ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	images := make([]domain.Image, 0)
	for rows.Next() {
		image, err := scanImage(rows)
		if err != nil {
			return nil, err
		}
		images = append(images, image)
	}
	return images, rows.Err()
}

func (s *SQLite) GetImage(ctx context.Context, id string) (domain.Image, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+imageColumns+` FROM images WHERE id = ?`, id)
	return scanImage(row)
}

func imageRoot() string {
	root := strings.TrimSpace(os.Getenv("OPENCUTTLES_IMAGE_ROOT"))
	if root == "" {
		root = "/var/lib/opencuttles/images"
	}
	return root
}

func (s *SQLite) GetOrCreateDefaultImage(ctx context.Context) (domain.Image, error) {
	defaultPath := strings.TrimSpace(os.Getenv("OPENCUTTLES_DEFAULT_IMAGE_PATH"))
	if defaultPath == "" {
		defaultPath = filepath.Join(imageRoot(), "default")
	}
	if err := ValidateImagePath(defaultPath, false); err != nil {
		return domain.Image{}, err
	}

	row := s.db.QueryRowContext(ctx, `SELECT `+imageColumns+` FROM images WHERE path = ? ORDER BY created_at LIMIT 1`, defaultPath)
	image, err := scanImage(row)
	if err == nil {
		return image, nil
	}
	if !IsNotFound(err) {
		return domain.Image{}, err
	}
	return s.CreateImage(ctx, domain.CreateImageRequest{
		Name:        "Default Cuttlefish image",
		Path:        defaultPath,
		Description: "Automatically registered by OpenCuttles for one-click instance creation.",
	})
}

func (s *SQLite) CreateInstance(ctx context.Context, req domain.CreateInstanceRequest) (domain.Instance, error) {
	imageID := strings.TrimSpace(req.ImageID)
	if imageID == "" {
		image, err := s.GetOrCreateDefaultImage(ctx)
		if err != nil {
			return domain.Instance{}, fmt.Errorf("default image: %w", err)
		}
		imageID = image.ID
	} else if _, err := s.GetImage(ctx, imageID); err != nil {
		return domain.Instance{}, fmt.Errorf("image lookup: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return domain.Instance{}, err
	}
	defer tx.Rollback()

	now := time.Now().UTC()
	adbPort, webrtcPort, err := nextPortsTx(ctx, tx)
	if err != nil {
		return domain.Instance{}, err
	}
	instanceID := newID("cvd")
	instanceNumber := adbPort - basePort + 1
	// Provisional device id; the orchestrator overwrites it with the operator's
	// real id (e.g. "cvd_1-1-1") once the device registers after launch.
	deviceID := fmt.Sprintf("cvd_%d-%d-1", instanceNumber, instanceNumber)

	instance := domain.Instance{
		ID:              instanceID,
		Name:            req.Name,
		HostID:          "local",
		Platform:        domain.PlatformAndroid,
		ImageID:         imageID,
		AndroidVersion:  req.AndroidVersion,
		State:           domain.StateStopped,
		CPUCores:        nonZero(req.CPUCores, 2),
		MemoryMB:        nonZero(req.MemoryMB, 4096),
		DisplayWidth:    nonZero(req.DisplayWidth, 720),
		DisplayHeight:   nonZero(req.DisplayHeight, 1280),
		DPI:             nonZero(req.DPI, 320),
		ADBPort:         adbPort,
		WebRTCPort:      webrtcPort,
		DeviceID:        deviceID,
		ConsoleProvider: domain.ConsoleProviderCuttlefishWebRTC,
		ConsoleURL:      ConsoleClientURL(instanceID, deviceID),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO instances (
		id, name, host_id, image_id, android_version, state, cpu_cores, memory_mb,
		display_width, display_height, dpi, adb_port, webrtc_port, device_id,
		console_provider, console_url, last_error, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		instance.ID, instance.Name, instance.HostID, instance.ImageID, instance.AndroidVersion, instance.State,
		instance.CPUCores, instance.MemoryMB, instance.DisplayWidth, instance.DisplayHeight, instance.DPI,
		instance.ADBPort, instance.WebRTCPort, instance.DeviceID,
		instance.ConsoleProvider, instance.ConsoleURL, instance.LastError,
		formatTime(instance.CreatedAt), formatTime(instance.UpdatedAt))
	if err != nil {
		return domain.Instance{}, err
	}
	return instance, tx.Commit()
}

func (s *SQLite) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}

func ValidateImagePath(path string, requireExisting bool) error {
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("image path is required")
	}
	allowedRoot := strings.TrimSpace(os.Getenv("OPENCUTTLES_IMAGE_ROOT"))
	if allowedRoot == "" {
		allowedRoot = "/var/lib/opencuttles/images"
	}
	cleanRoot, err := filepath.Abs(filepath.Clean(allowedRoot))
	if err != nil {
		return fmt.Errorf("invalid image root: %w", err)
	}
	cleanPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return fmt.Errorf("invalid image path: %w", err)
	}
	if cleanPath != cleanRoot && !strings.HasPrefix(cleanPath, cleanRoot+string(os.PathSeparator)) {
		return fmt.Errorf("image path must be under %s", cleanRoot)
	}
	if strings.Contains(filepath.ToSlash(cleanPath), "/../") {
		return fmt.Errorf("invalid image path")
	}
	if requireExisting {
		resolved, err := filepath.EvalSymlinks(cleanPath)
		if err != nil {
			return fmt.Errorf("image path is not readable: %w", err)
		}
		resolvedAbs, err := filepath.Abs(filepath.Clean(resolved))
		if err != nil {
			return fmt.Errorf("invalid resolved image path: %w", err)
		}
		if resolvedAbs != cleanRoot && !strings.HasPrefix(resolvedAbs, cleanRoot+string(os.PathSeparator)) {
			return fmt.Errorf("resolved image path must remain under %s", cleanRoot)
		}
		info, err := os.Stat(resolvedAbs)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("image path is not a readable directory: %s", resolvedAbs)
		}
	}
	return nil
}

const instanceColumns = `id, name, host_id, image_id, android_version, state, cpu_cores, memory_mb, display_width, display_height, dpi, adb_port, webrtc_port, device_id, console_provider, console_url, last_error, created_at, updated_at, platform, control_endpoint`

func (s *SQLite) ListInstances(ctx context.Context) ([]domain.Instance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+instanceColumns+` FROM instances ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	instances := make([]domain.Instance, 0)
	for rows.Next() {
		instance, err := scanInstance(rows)
		if err != nil {
			return nil, err
		}
		instances = append(instances, instance)
	}
	return instances, rows.Err()
}

func (s *SQLite) GetInstance(ctx context.Context, id string) (domain.Instance, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+instanceColumns+` FROM instances WHERE id = ?`, id)
	return scanInstance(row)
}

// ConsoleClientURL builds the per-instance console path that the API reverse
// proxies to the cuttlefish-operator's per-device WebRTC client page
// (/devices/<deviceID>/files/client.html on the operator).
func ConsoleClientURL(instanceID, deviceID string) string {
	return fmt.Sprintf("/api/v1/instances/%s/console/devices/%s/files/client.html", instanceID, deviceID)
}

// UpdateInstanceConsole records the operator-assigned device id and the console
// URL derived from it, after the device registers post-launch.
func (s *SQLite) UpdateInstanceConsole(ctx context.Context, id, deviceID, consoleURL string) (domain.Instance, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE instances SET device_id = ?, console_url = ?, updated_at = ? WHERE id = ?`,
		deviceID, consoleURL, formatTime(now), id)
	if err != nil {
		return domain.Instance{}, err
	}
	return s.GetInstance(ctx, id)
}

func (s *SQLite) UpdateInstanceState(ctx context.Context, id, state, lastError string) (domain.Instance, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE instances SET state = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		state, lastError, formatTime(now), id)
	if err != nil {
		return domain.Instance{}, err
	}
	return s.GetInstance(ctx, id)
}

func (s *SQLite) DeleteInstance(ctx context.Context, id string) error {
	result, err := s.db.ExecContext(ctx, `DELETE FROM instances WHERE id = ?`, id)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (s *SQLite) CreateOperation(ctx context.Context, instanceID, action, message string) (domain.Operation, error) {
	now := time.Now().UTC()
	operation := domain.Operation{
		ID:         newID("op"),
		InstanceID: instanceID,
		Action:     action,
		Status:     "running",
		Message:    message,
		CreatedAt:  now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO operations (id, instance_id, action, status, message, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		operation.ID, operation.InstanceID, operation.Action, operation.Status, operation.Message, formatTime(operation.CreatedAt))
	return operation, err
}

func (s *SQLite) FinishOperation(ctx context.Context, id, status, message string) (domain.Operation, error) {
	now := time.Now().UTC()
	_, err := s.db.ExecContext(ctx, `UPDATE operations SET status = ?, message = ?, finished_at = ? WHERE id = ?`,
		status, message, formatTime(now), id)
	if err != nil {
		return domain.Operation{}, err
	}
	return s.GetOperation(ctx, id)
}

func (s *SQLite) GetOperation(ctx context.Context, id string) (domain.Operation, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, instance_id, action, status, message, created_at, finished_at FROM operations WHERE id = ?`, id)
	return scanOperation(row)
}

func (s *SQLite) ListOperations(ctx context.Context) ([]domain.Operation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, instance_id, action, status, message, created_at, finished_at FROM operations ORDER BY created_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	operations := make([]domain.Operation, 0)
	for rows.Next() {
		operation, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		operations = append(operations, operation)
	}
	return operations, rows.Err()
}

func (s *SQLite) nextPorts(ctx context.Context) (int, int, error) {
	return nextPortsTx(ctx, s.db)
}

type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// basePort is the first ADB port Cuttlefish assigns (instance 1 -> cvd-1).
const basePort = 6520

// webrtcOperatorPort is the host-wide port served by cuttlefish-operator; the
// interactive console for every device is multiplexed through it by deviceId.
// Current Cuttlefish serves on 1443 (older builds used 8443). The API resolves
// the live port from OPENCUTTLES_OPERATOR_PORT at proxy time, so this is just the
// recorded default for new instances.
const webrtcOperatorPort = 1443

func nextPortsTx(ctx context.Context, q queryer) (int, int, error) {
	var maxADB sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT MAX(adb_port) FROM instances`).Scan(&maxADB); err != nil {
		return 0, 0, err
	}
	nextADB := basePort
	if maxADB.Valid {
		nextADB = int(maxADB.Int64) + 1
	}
	return nextADB, webrtcOperatorPort, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanImage(row scanner) (domain.Image, error) {
	var image domain.Image
	var createdAt string
	if err := row.Scan(&image.ID, &image.Name, &image.Path, &image.AndroidAPI, &image.Description,
		&image.BuildTarget, &image.VersionID, &image.Status, &image.SizeBytes, &image.LastError, &createdAt); err != nil {
		return domain.Image{}, err
	}
	image.CreatedAt = parseTime(createdAt)
	return image, nil
}

func scanInstance(row scanner) (domain.Instance, error) {
	var instance domain.Instance
	var createdAt, updatedAt string
	if err := row.Scan(
		&instance.ID, &instance.Name, &instance.HostID, &instance.ImageID, &instance.AndroidVersion, &instance.State,
		&instance.CPUCores, &instance.MemoryMB, &instance.DisplayWidth, &instance.DisplayHeight, &instance.DPI,
		&instance.ADBPort, &instance.WebRTCPort, &instance.DeviceID,
		&instance.ConsoleProvider, &instance.ConsoleURL, &instance.LastError, &createdAt, &updatedAt,
		&instance.Platform, &instance.ControlEndpoint,
	); err != nil {
		return domain.Instance{}, err
	}
	instance.CreatedAt = parseTime(createdAt)
	instance.UpdatedAt = parseTime(updatedAt)
	if instance.Platform == "" {
		instance.Platform = domain.PlatformAndroid
	}
	return instance, nil
}

func scanOperation(row scanner) (domain.Operation, error) {
	var operation domain.Operation
	var createdAt string
	var finishedAt sql.NullString
	if err := row.Scan(&operation.ID, &operation.InstanceID, &operation.Action, &operation.Status, &operation.Message, &createdAt, &finishedAt); err != nil {
		return domain.Operation{}, err
	}
	operation.CreatedAt = parseTime(createdAt)
	if finishedAt.Valid {
		t := parseTime(finishedAt.String)
		operation.FinishedAt = &t
	}
	return operation, nil
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func newID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("random id: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func nonZero(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func IsNotFound(err error) bool {
	return errors.Is(err, sql.ErrNoRows)
}
