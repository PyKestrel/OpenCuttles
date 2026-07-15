package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
)

// Steps and step results are stored as JSON columns: they are read and written
// as a unit and never queried into, matching how small the test volume is.

func (s *SQLite) CreateTest(ctx context.Context, req domain.CreateTestRequest) (domain.Test, error) {
	steps := make([]string, 0, len(req.Steps))
	for _, step := range req.Steps {
		if trimmed := strings.TrimSpace(step); trimmed != "" {
			steps = append(steps, trimmed)
		}
	}
	if len(steps) == 0 {
		return domain.Test{}, fmt.Errorf("a test needs at least one step")
	}
	test := domain.Test{
		ID:        newID("test"),
		Name:      strings.TrimSpace(req.Name),
		Steps:     steps,
		CreatedAt: time.Now().UTC(),
	}
	encoded, err := json.Marshal(test.Steps)
	if err != nil {
		return domain.Test{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO tests (id, name, steps, created_at) VALUES (?, ?, ?, ?)`,
		test.ID, test.Name, string(encoded), formatTime(test.CreatedAt))
	return test, err
}

func (s *SQLite) GetTest(ctx context.Context, id string) (domain.Test, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, steps, created_at FROM tests WHERE id = ?`, id)
	return scanTest(row)
}

func (s *SQLite) ListTests(ctx context.Context) ([]domain.Test, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, steps, created_at FROM tests ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tests := make([]domain.Test, 0)
	for rows.Next() {
		test, err := scanTest(rows)
		if err != nil {
			return nil, err
		}
		tests = append(tests, test)
	}
	return tests, rows.Err()
}

func (s *SQLite) DeleteTest(ctx context.Context, id string) error {
	// test_runs no longer has an ON DELETE CASCADE FK (case-runs set test_id=''),
	// so remove a test's runs explicitly.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM test_runs WHERE test_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM tests WHERE id = ?`, id)
	return err
}

func (s *SQLite) CreateTestRun(ctx context.Context, testID, instanceID string) (domain.TestRun, error) {
	run := domain.TestRun{
		ID:         newID("run"),
		TestID:     testID,
		InstanceID: instanceID,
		Status:     "running",
		Steps:      []domain.StepResult{},
		StartedAt:  time.Now().UTC(),
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO test_runs (id, test_id, instance_id, status, started_at) VALUES (?, ?, ?, ?, ?)`,
		run.ID, run.TestID, run.InstanceID, run.Status, formatTime(run.StartedAt))
	return run, err
}

// UpdateTestRun persists progress: current step results and (when finished)
// status, pass/fail, video artifact, and error.
func (s *SQLite) UpdateTestRun(ctx context.Context, run domain.TestRun) error {
	encoded, err := json.Marshal(run.Steps)
	if err != nil {
		return err
	}
	var finished any
	if run.FinishedAt != nil {
		finished = formatTime(*run.FinishedAt)
	}
	_, err = s.db.ExecContext(ctx,
		`UPDATE test_runs SET status = ?, passed = ?, steps = ?, video = ?, error = ?, finished_at = ? WHERE id = ?`,
		run.Status, boolToInt(run.Passed), string(encoded), run.Video, run.Error, finished, run.ID)
	return err
}

func (s *SQLite) GetTestRun(ctx context.Context, id string) (domain.TestRun, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT r.id, r.test_id, t.name, r.instance_id, r.status, r.passed, r.steps, r.video, r.error, r.started_at, r.finished_at
		 FROM test_runs r LEFT JOIN tests t ON t.id = r.test_id WHERE r.id = ?`, id)
	return scanTestRun(row)
}

func (s *SQLite) ListTestRuns(ctx context.Context) ([]domain.TestRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.test_id, t.name, r.instance_id, r.status, r.passed, r.steps, r.video, r.error, r.started_at, r.finished_at
		 FROM test_runs r LEFT JOIN tests t ON t.id = r.test_id ORDER BY r.started_at DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]domain.TestRun, 0)
	for rows.Next() {
		run, err := scanTestRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func scanTest(row scanner) (domain.Test, error) {
	var test domain.Test
	var steps, created string
	if err := row.Scan(&test.ID, &test.Name, &steps, &created); err != nil {
		return domain.Test{}, err
	}
	if err := json.Unmarshal([]byte(steps), &test.Steps); err != nil {
		return domain.Test{}, err
	}
	test.CreatedAt = parseTime(created)
	return test, nil
}

func scanTestRun(row scanner) (domain.TestRun, error) {
	var run domain.TestRun
	var passed int
	var testName sql.NullString
	var steps, started string
	var finished sql.NullString
	if err := row.Scan(&run.ID, &run.TestID, &testName, &run.InstanceID, &run.Status, &passed, &steps, &run.Video, &run.Error, &started, &finished); err != nil {
		return domain.TestRun{}, err
	}
	run.Passed = passed != 0
	run.TestName = testName.String
	if err := json.Unmarshal([]byte(steps), &run.Steps); err != nil {
		return domain.TestRun{}, err
	}
	run.StartedAt = parseTime(started)
	if finished.Valid {
		t := parseTime(finished.String)
		run.FinishedAt = &t
	}
	return run, nil
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
