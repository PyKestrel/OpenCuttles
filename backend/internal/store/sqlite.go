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
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO schema_migrations (version, applied_at) VALUES (1, ?)`, formatTime(time.Now().UTC()))
	if err != nil {
		return err
	}
	return nil
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
		CreatedAt:   now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO images (id, name, path, android_api, description, created_at) VALUES (?, ?, ?, ?, ?, ?)`,
		image.ID, image.Name, image.Path, image.AndroidAPI, image.Description, formatTime(image.CreatedAt))
	return image, err
}

func (s *SQLite) ListImages(ctx context.Context) ([]domain.Image, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, path, android_api, description, created_at FROM images ORDER BY created_at DESC`)
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
	row := s.db.QueryRowContext(ctx, `SELECT id, name, path, android_api, description, created_at FROM images WHERE id = ?`, id)
	return scanImage(row)
}

func (s *SQLite) CreateInstance(ctx context.Context, req domain.CreateInstanceRequest) (domain.Instance, error) {
	if _, err := s.GetImage(ctx, req.ImageID); err != nil {
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

	instance := domain.Instance{
		ID:              instanceID,
		Name:            req.Name,
		HostID:          "local",
		ImageID:         req.ImageID,
		State:           domain.StateStopped,
		CPUCores:        nonZero(req.CPUCores, 2),
		MemoryMB:        nonZero(req.MemoryMB, 4096),
		ADBPort:         adbPort,
		WebRTCPort:      webrtcPort,
		ConsoleProvider: domain.ConsoleProviderCuttlefishWebRTC,
		ConsoleURL:      fmt.Sprintf("/api/v1/instances/%s/console", instanceID),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	_, err = tx.ExecContext(ctx, `INSERT INTO instances (
		id, name, host_id, image_id, state, cpu_cores, memory_mb, adb_port, webrtc_port,
		console_provider, console_url, last_error, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		instance.ID, instance.Name, instance.HostID, instance.ImageID, instance.State,
		instance.CPUCores, instance.MemoryMB, instance.ADBPort, instance.WebRTCPort,
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

func (s *SQLite) ListInstances(ctx context.Context) ([]domain.Instance, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, host_id, image_id, state, cpu_cores, memory_mb, adb_port, webrtc_port, console_provider, console_url, last_error, created_at, updated_at FROM instances ORDER BY created_at DESC`)
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
	row := s.db.QueryRowContext(ctx, `SELECT id, name, host_id, image_id, state, cpu_cores, memory_mb, adb_port, webrtc_port, console_provider, console_url, last_error, created_at, updated_at FROM instances WHERE id = ?`, id)
	return scanInstance(row)
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

func nextPortsTx(ctx context.Context, q queryer) (int, int, error) {
	var maxADB sql.NullInt64
	if err := q.QueryRowContext(ctx, `SELECT MAX(adb_port) FROM instances`).Scan(&maxADB); err != nil {
		return 0, 0, err
	}
	nextADB := 6520
	if maxADB.Valid {
		nextADB = int(maxADB.Int64) + 1
	}
	return nextADB, 8443 + (nextADB - 6520), nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanImage(row scanner) (domain.Image, error) {
	var image domain.Image
	var createdAt string
	if err := row.Scan(&image.ID, &image.Name, &image.Path, &image.AndroidAPI, &image.Description, &createdAt); err != nil {
		return domain.Image{}, err
	}
	image.CreatedAt = parseTime(createdAt)
	return image, nil
}

func scanInstance(row scanner) (domain.Instance, error) {
	var instance domain.Instance
	var createdAt, updatedAt string
	if err := row.Scan(
		&instance.ID, &instance.Name, &instance.HostID, &instance.ImageID, &instance.State,
		&instance.CPUCores, &instance.MemoryMB, &instance.ADBPort, &instance.WebRTCPort,
		&instance.ConsoleProvider, &instance.ConsoleURL, &instance.LastError, &createdAt, &updatedAt,
	); err != nil {
		return domain.Instance{}, err
	}
	instance.CreatedAt = parseTime(createdAt)
	instance.UpdatedAt = parseTime(updatedAt)
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
