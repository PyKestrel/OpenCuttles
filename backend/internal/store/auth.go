package store

import (
	"context"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

func (s *SQLite) UserCount(ctx context.Context) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&count)
	return count, err
}

func (s *SQLite) CreateUser(ctx context.Context, username, displayName, role, passwordHash string) (domain.User, error) {
	now := time.Now().UTC()
	user := domain.User{
		ID:           newID("usr"),
		Username:     username,
		DisplayName:  displayName,
		Role:         role,
		PasswordHash: passwordHash,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (id, username, display_name, role, password_hash, disabled, created_at, updated_at) VALUES (?, ?, ?, ?, ?, 0, ?, ?)`,
		user.ID, user.Username, user.DisplayName, user.Role, user.PasswordHash, formatTime(user.CreatedAt), formatTime(user.UpdatedAt))
	return user, err
}

func (s *SQLite) GetUserByUsername(ctx context.Context, username string) (domain.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, display_name, role, password_hash, disabled, created_at, updated_at FROM users WHERE username = ?`, username)
	return scanUser(row)
}

func (s *SQLite) GetUserByID(ctx context.Context, id string) (domain.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, username, display_name, role, password_hash, disabled, created_at, updated_at FROM users WHERE id = ?`, id)
	return scanUser(row)
}

func (s *SQLite) CreateSession(ctx context.Context, userID, tokenHash string, expiresAt time.Time) (domain.Session, error) {
	now := time.Now().UTC()
	session := domain.Session{
		ID:        newID("ses"),
		UserID:    userID,
		TokenHash: tokenHash,
		ExpiresAt: expiresAt.UTC(),
		CreatedAt: now,
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO sessions (id, user_id, token_hash, expires_at, created_at) VALUES (?, ?, ?, ?, ?)`,
		session.ID, session.UserID, session.TokenHash, formatTime(session.ExpiresAt), formatTime(session.CreatedAt))
	return session, err
}

func (s *SQLite) GetSessionUser(ctx context.Context, tokenHash string) (domain.Session, domain.User, error) {
	row := s.db.QueryRowContext(ctx, `SELECT
		s.id, s.user_id, s.token_hash, s.expires_at, s.created_at,
		u.id, u.username, u.display_name, u.role, u.password_hash, u.disabled, u.created_at, u.updated_at
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = ? AND s.expires_at > ? AND u.disabled = 0`, tokenHash, formatTime(time.Now().UTC()))

	var session domain.Session
	var user domain.User
	var sessionExpiresAt, sessionCreatedAt, userCreatedAt, userUpdatedAt string
	var disabled int
	if err := row.Scan(
		&session.ID, &session.UserID, &session.TokenHash, &sessionExpiresAt, &sessionCreatedAt,
		&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.PasswordHash, &disabled, &userCreatedAt, &userUpdatedAt,
	); err != nil {
		return domain.Session{}, domain.User{}, err
	}
	session.ExpiresAt = parseTime(sessionExpiresAt)
	session.CreatedAt = parseTime(sessionCreatedAt)
	user.Disabled = disabled != 0
	user.CreatedAt = parseTime(userCreatedAt)
	user.UpdatedAt = parseTime(userUpdatedAt)
	return session, user, nil
}

func (s *SQLite) DeleteSession(ctx context.Context, tokenHash string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE token_hash = ?`, tokenHash)
	return err
}

func (s *SQLite) PruneExpiredSessions(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM sessions WHERE expires_at <= ?`, formatTime(time.Now().UTC()))
	return err
}

func (s *SQLite) CreateAuditEvent(ctx context.Context, event domain.AuditEvent) (domain.AuditEvent, error) {
	if event.ID == "" {
		event.ID = newID("aud")
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now().UTC()
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO audit_events (
		id, actor_id, actor_name, action, resource, resource_id, outcome, message, source_ip, user_agent, request_id, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID, event.ActorID, event.ActorName, event.Action, event.Resource, event.ResourceID,
		event.Outcome, event.Message, event.SourceIP, event.UserAgent, event.RequestID, formatTime(event.CreatedAt))
	return event, err
}

func (s *SQLite) ListAuditEvents(ctx context.Context, limit int) ([]domain.AuditEvent, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, actor_id, actor_name, action, resource, resource_id, outcome, message, source_ip, user_agent, request_id, created_at FROM audit_events ORDER BY created_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	events := make([]domain.AuditEvent, 0)
	for rows.Next() {
		event, err := scanAuditEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

func scanUser(row scanner) (domain.User, error) {
	var user domain.User
	var createdAt, updatedAt string
	var disabled int
	if err := row.Scan(&user.ID, &user.Username, &user.DisplayName, &user.Role, &user.PasswordHash, &disabled, &createdAt, &updatedAt); err != nil {
		return domain.User{}, err
	}
	user.Disabled = disabled != 0
	user.CreatedAt = parseTime(createdAt)
	user.UpdatedAt = parseTime(updatedAt)
	return user, nil
}

func scanAuditEvent(row scanner) (domain.AuditEvent, error) {
	var event domain.AuditEvent
	var createdAt string
	if err := row.Scan(
		&event.ID, &event.ActorID, &event.ActorName, &event.Action, &event.Resource, &event.ResourceID,
		&event.Outcome, &event.Message, &event.SourceIP, &event.UserAgent, &event.RequestID, &createdAt,
	); err != nil {
		return domain.AuditEvent{}, err
	}
	event.CreatedAt = parseTime(createdAt)
	return event, nil
}

