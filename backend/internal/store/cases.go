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

// QMetry-style test management persistence: test cases (with structured steps),
// cycles (ordered case sets + schedule), cycle runs (per-case fan-out), and
// uploaded builds. List/step columns are JSON blobs, matching tests.go.

const testCaseColumns = `id, summary, description, precondition, priority, status, labels, components, folder_path, steps, external_key, created_at, updated_at`

func (s *SQLite) insertTestCase(ctx context.Context, c domain.TestCase) error {
	labels, _ := json.Marshal(nonNilStrings(c.Labels))
	components, _ := json.Marshal(nonNilStrings(c.Components))
	steps, _ := json.Marshal(nonNilSteps(c.Steps))
	_, err := s.db.ExecContext(ctx, `INSERT INTO test_cases (`+testCaseColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.ID, c.Summary, c.Description, c.Precondition, c.Priority, c.Status,
		string(labels), string(components), c.FolderPath, string(steps), c.ExternalKey,
		formatTime(c.CreatedAt), formatTime(c.UpdatedAt))
	return err
}

// CreateTestCase persists a new case, filling id/timestamps and step indexes.
func (s *SQLite) CreateTestCase(ctx context.Context, c domain.TestCase) (domain.TestCase, error) {
	if strings.TrimSpace(c.Summary) == "" {
		return domain.TestCase{}, fmt.Errorf("a test case needs a summary")
	}
	c.ID = newID("case")
	now := time.Now().UTC()
	c.CreatedAt, c.UpdatedAt = now, now
	reindexSteps(c.Steps)
	return c, s.insertTestCase(ctx, c)
}

// BulkCreateCases inserts many cases in one transaction (QMetry import).
func (s *SQLite) BulkCreateCases(ctx context.Context, cases []domain.TestCase) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	now := time.Now().UTC()
	created := 0
	for _, c := range cases {
		if strings.TrimSpace(c.Summary) == "" {
			continue
		}
		c.ID = newID("case")
		c.CreatedAt, c.UpdatedAt = now, now
		reindexSteps(c.Steps)
		labels, _ := json.Marshal(nonNilStrings(c.Labels))
		components, _ := json.Marshal(nonNilStrings(c.Components))
		steps, _ := json.Marshal(nonNilSteps(c.Steps))
		if _, err := tx.ExecContext(ctx, `INSERT INTO test_cases (`+testCaseColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.Summary, c.Description, c.Precondition, c.Priority, c.Status,
			string(labels), string(components), c.FolderPath, string(steps), c.ExternalKey,
			formatTime(c.CreatedAt), formatTime(c.UpdatedAt)); err != nil {
			return 0, err
		}
		created++
	}
	return created, tx.Commit()
}

func (s *SQLite) GetTestCase(ctx context.Context, id string) (domain.TestCase, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+testCaseColumns+` FROM test_cases WHERE id = ?`, id)
	return scanTestCase(row)
}

func (s *SQLite) ListTestCases(ctx context.Context) ([]domain.TestCase, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+testCaseColumns+` FROM test_cases ORDER BY folder_path, summary`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cases := make([]domain.TestCase, 0)
	for rows.Next() {
		c, err := scanTestCase(rows)
		if err != nil {
			return nil, err
		}
		cases = append(cases, c)
	}
	return cases, rows.Err()
}

// ListCaseFolders returns the distinct non-empty folder paths for the tree.
func (s *SQLite) ListCaseFolders(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT folder_path FROM test_cases WHERE folder_path != '' ORDER BY folder_path`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	folders := make([]string, 0)
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return nil, err
		}
		folders = append(folders, f)
	}
	return folders, rows.Err()
}

func (s *SQLite) UpdateTestCase(ctx context.Context, c domain.TestCase) error {
	c.UpdatedAt = time.Now().UTC()
	reindexSteps(c.Steps)
	labels, _ := json.Marshal(nonNilStrings(c.Labels))
	components, _ := json.Marshal(nonNilStrings(c.Components))
	steps, _ := json.Marshal(nonNilSteps(c.Steps))
	_, err := s.db.ExecContext(ctx,
		`UPDATE test_cases SET summary=?, description=?, precondition=?, priority=?, status=?, labels=?, components=?, folder_path=?, steps=?, external_key=?, updated_at=? WHERE id=?`,
		c.Summary, c.Description, c.Precondition, c.Priority, c.Status,
		string(labels), string(components), c.FolderPath, string(steps), c.ExternalKey, formatTime(c.UpdatedAt), c.ID)
	return err
}

func (s *SQLite) DeleteTestCase(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM test_cases WHERE id = ?`, id)
	return err
}

func scanTestCase(row scanner) (domain.TestCase, error) {
	var c domain.TestCase
	var labels, components, steps, created, updated string
	if err := row.Scan(&c.ID, &c.Summary, &c.Description, &c.Precondition, &c.Priority, &c.Status,
		&labels, &components, &c.FolderPath, &steps, &c.ExternalKey, &created, &updated); err != nil {
		return domain.TestCase{}, err
	}
	_ = json.Unmarshal([]byte(labels), &c.Labels)
	_ = json.Unmarshal([]byte(components), &c.Components)
	_ = json.Unmarshal([]byte(steps), &c.Steps)
	c.Labels = nonNilStrings(c.Labels)
	c.Components = nonNilStrings(c.Components)
	c.Steps = nonNilSteps(c.Steps)
	c.CreatedAt = parseTime(created)
	c.UpdatedAt = parseTime(updated)
	return c, nil
}

// ---- Test cycles ----

const testCycleColumns = `id, name, platform, build_id, environment, case_ids, cron, on_new_build, enabled, last_run_at, next_run_at, created_at`

func (s *SQLite) CreateTestCycle(ctx context.Context, c domain.TestCycle) (domain.TestCycle, error) {
	if strings.TrimSpace(c.Name) == "" {
		return domain.TestCycle{}, fmt.Errorf("a test cycle needs a name")
	}
	c.ID = newID("cycle")
	c.CreatedAt = time.Now().UTC()
	if c.Platform == "" {
		c.Platform = domain.PlatformAndroid
	}
	return c, s.writeTestCycle(ctx, c, true)
}

func (s *SQLite) UpdateTestCycle(ctx context.Context, c domain.TestCycle) error {
	return s.writeTestCycle(ctx, c, false)
}

func (s *SQLite) writeTestCycle(ctx context.Context, c domain.TestCycle, insert bool) error {
	caseIDs, _ := json.Marshal(nonNilStrings(c.CaseIDs))
	if insert {
		_, err := s.db.ExecContext(ctx, `INSERT INTO test_cycles (`+testCycleColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.ID, c.Name, c.Platform, c.BuildID, c.Environment, string(caseIDs), c.Cron,
			boolToInt(c.OnNewBuild), boolToInt(c.Enabled), nullableTime(c.LastRunAt), nullableTime(c.NextRunAt), formatTime(c.CreatedAt))
		return err
	}
	_, err := s.db.ExecContext(ctx,
		`UPDATE test_cycles SET name=?, platform=?, build_id=?, environment=?, case_ids=?, cron=?, on_new_build=?, enabled=?, last_run_at=?, next_run_at=? WHERE id=?`,
		c.Name, c.Platform, c.BuildID, c.Environment, string(caseIDs), c.Cron,
		boolToInt(c.OnNewBuild), boolToInt(c.Enabled), nullableTime(c.LastRunAt), nullableTime(c.NextRunAt), c.ID)
	return err
}

func (s *SQLite) GetTestCycle(ctx context.Context, id string) (domain.TestCycle, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+testCycleColumns+` FROM test_cycles WHERE id = ?`, id)
	return scanTestCycle(row)
}

func (s *SQLite) ListTestCycles(ctx context.Context) ([]domain.TestCycle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+testCycleColumns+` FROM test_cycles ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cycles := make([]domain.TestCycle, 0)
	for rows.Next() {
		c, err := scanTestCycle(rows)
		if err != nil {
			return nil, err
		}
		cycles = append(cycles, c)
	}
	return cycles, rows.Err()
}

func (s *SQLite) DeleteTestCycle(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM test_cycles WHERE id = ?`, id)
	return err
}

// SetCycleSchedule persists computed last/next run times after a trigger.
func (s *SQLite) SetCycleSchedule(ctx context.Context, id string, lastRun, nextRun *time.Time) error {
	_, err := s.db.ExecContext(ctx, `UPDATE test_cycles SET last_run_at=?, next_run_at=? WHERE id=?`,
		nullableTime(lastRun), nullableTime(nextRun), id)
	return err
}

// ListDueCycles returns enabled, cron-scheduled cycles whose next run is due and
// that have no cycle run already in flight.
func (s *SQLite) ListDueCycles(ctx context.Context, now time.Time) ([]domain.TestCycle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+testCycleColumns+` FROM test_cycles c
		WHERE c.enabled = 1 AND c.cron != '' AND c.next_run_at IS NOT NULL AND c.next_run_at <= ?
		AND NOT EXISTS (SELECT 1 FROM cycle_runs cr WHERE cr.cycle_id = c.id AND cr.status = 'running')`,
		formatTime(now.UTC()))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cycles := make([]domain.TestCycle, 0)
	for rows.Next() {
		c, err := scanTestCycle(rows)
		if err != nil {
			return nil, err
		}
		cycles = append(cycles, c)
	}
	return cycles, rows.Err()
}

// ListCyclesForBuild returns enabled on-new-build cycles matching a platform.
func (s *SQLite) ListCyclesForBuild(ctx context.Context, platform string) ([]domain.TestCycle, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+testCycleColumns+` FROM test_cycles
		WHERE enabled = 1 AND on_new_build = 1 AND platform = ?`, platform)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cycles := make([]domain.TestCycle, 0)
	for rows.Next() {
		c, err := scanTestCycle(rows)
		if err != nil {
			return nil, err
		}
		cycles = append(cycles, c)
	}
	return cycles, rows.Err()
}

func scanTestCycle(row scanner) (domain.TestCycle, error) {
	var c domain.TestCycle
	var caseIDs, created string
	var onNewBuild, enabled int
	var lastRun, nextRun sql.NullString
	if err := row.Scan(&c.ID, &c.Name, &c.Platform, &c.BuildID, &c.Environment, &caseIDs, &c.Cron,
		&onNewBuild, &enabled, &lastRun, &nextRun, &created); err != nil {
		return domain.TestCycle{}, err
	}
	_ = json.Unmarshal([]byte(caseIDs), &c.CaseIDs)
	c.CaseIDs = nonNilStrings(c.CaseIDs)
	c.OnNewBuild = onNewBuild != 0
	c.Enabled = enabled != 0
	c.LastRunAt = parseNullableTime(lastRun)
	c.NextRunAt = parseNullableTime(nextRun)
	c.CreatedAt = parseTime(created)
	if c.Platform == "" {
		c.Platform = domain.PlatformAndroid
	}
	return c, nil
}

// ---- Cycle runs ----

const cycleRunColumns = `id, cycle_id, trigger, build_id, instance_id, status, totals, started_at, finished_at`

func (s *SQLite) CreateCycleRun(ctx context.Context, r domain.CycleRun) (domain.CycleRun, error) {
	r.ID = newID("cyc")
	r.Status = "running"
	r.StartedAt = time.Now().UTC()
	totals, _ := json.Marshal(r.Totals)
	_, err := s.db.ExecContext(ctx, `INSERT INTO cycle_runs (`+cycleRunColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.CycleID, r.Trigger, r.BuildID, r.InstanceID, r.Status, string(totals), formatTime(r.StartedAt), nil)
	return r, err
}

func (s *SQLite) UpdateCycleRun(ctx context.Context, r domain.CycleRun) error {
	totals, _ := json.Marshal(r.Totals)
	var finished any
	if r.FinishedAt != nil {
		finished = formatTime(*r.FinishedAt)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE cycle_runs SET status=?, totals=?, instance_id=?, build_id=?, finished_at=? WHERE id=?`,
		r.Status, string(totals), r.InstanceID, r.BuildID, finished, r.ID)
	return err
}

func (s *SQLite) GetCycleRun(ctx context.Context, id string) (domain.CycleRun, error) {
	row := s.db.QueryRowContext(ctx, `SELECT r.id, r.cycle_id, c.name, r.trigger, r.build_id, r.instance_id, r.status, r.totals, r.started_at, r.finished_at
		FROM cycle_runs r LEFT JOIN test_cycles c ON c.id = r.cycle_id WHERE r.id = ?`, id)
	return scanCycleRun(row)
}

func (s *SQLite) ListCycleRuns(ctx context.Context) ([]domain.CycleRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.id, r.cycle_id, c.name, r.trigger, r.build_id, r.instance_id, r.status, r.totals, r.started_at, r.finished_at
		FROM cycle_runs r LEFT JOIN test_cycles c ON c.id = r.cycle_id ORDER BY r.started_at DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]domain.CycleRun, 0)
	for rows.Next() {
		r, err := scanCycleRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func scanCycleRun(row scanner) (domain.CycleRun, error) {
	var r domain.CycleRun
	var name sql.NullString
	var totals, started string
	var finished sql.NullString
	if err := row.Scan(&r.ID, &r.CycleID, &name, &r.Trigger, &r.BuildID, &r.InstanceID, &r.Status, &totals, &started, &finished); err != nil {
		return domain.CycleRun{}, err
	}
	r.CycleName = name.String
	_ = json.Unmarshal([]byte(totals), &r.Totals)
	r.StartedAt = parseTime(started)
	if finished.Valid {
		t := parseTime(finished.String)
		r.FinishedAt = &t
	}
	return r, nil
}

// CreateCaseRun opens a per-case execution (a test_runs row) inside a cycle run.
func (s *SQLite) CreateCaseRun(ctx context.Context, cycleRunID, caseID, instanceID string) (domain.TestRun, error) {
	run := domain.TestRun{
		ID:         newID("run"),
		InstanceID: instanceID,
		Status:     "running",
		Steps:      []domain.StepResult{},
		StartedAt:  time.Now().UTC(),
		CycleRunID: cycleRunID,
		CaseID:     caseID,
	}
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO test_runs (id, test_id, instance_id, status, started_at, cycle_run_id, case_id) VALUES (?, '', ?, ?, ?, ?, ?)`,
		run.ID, run.InstanceID, run.Status, formatTime(run.StartedAt), run.CycleRunID, run.CaseID)
	return run, err
}

// ListTestRunsByCycleRun returns the per-case runs of a cycle run, ordered by
// start, with the case summary hydrated as the run name.
func (s *SQLite) ListTestRunsByCycleRun(ctx context.Context, cycleRunID string) ([]domain.TestRun, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT r.id, r.test_id, COALESCE(NULLIF(t.name,''), tc.summary, ''), r.instance_id, r.status, r.passed, r.steps, r.video, r.error, r.started_at, r.finished_at, r.cycle_run_id, r.case_id
		 FROM test_runs r LEFT JOIN tests t ON t.id = r.test_id LEFT JOIN test_cases tc ON tc.id = r.case_id
		 WHERE r.cycle_run_id = ? ORDER BY r.started_at ASC`, cycleRunID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	runs := make([]domain.TestRun, 0)
	for rows.Next() {
		run, err := scanCaseRun(rows)
		if err != nil {
			return nil, err
		}
		runs = append(runs, run)
	}
	return runs, rows.Err()
}

func scanCaseRun(row scanner) (domain.TestRun, error) {
	var run domain.TestRun
	var passed int
	var name sql.NullString
	var steps, started string
	var finished sql.NullString
	if err := row.Scan(&run.ID, &run.TestID, &name, &run.InstanceID, &run.Status, &passed, &steps,
		&run.Video, &run.Error, &started, &finished, &run.CycleRunID, &run.CaseID); err != nil {
		return domain.TestRun{}, err
	}
	run.Passed = passed != 0
	run.TestName = name.String
	_ = json.Unmarshal([]byte(steps), &run.Steps)
	run.StartedAt = parseTime(started)
	if finished.Valid {
		t := parseTime(finished.String)
		run.FinishedAt = &t
	}
	return run, nil
}

// AppendStep appends a StepResult to a run and re-persists (RunSink for the
// report_step_result MCP tool).
func (s *SQLite) AppendStep(ctx context.Context, runID string, step domain.StepResult) error {
	run, err := s.GetTestRun(ctx, runID)
	if err != nil {
		return err
	}
	step.Index = len(run.Steps)
	run.Steps = append(run.Steps, step)
	return s.UpdateTestRun(ctx, run)
}

// ---- Builds ----

const buildColumns = `id, platform, filename, path, size_bytes, version, status, note, created_at`

func (s *SQLite) CreateBuild(ctx context.Context, b domain.Build) (domain.Build, error) {
	b.ID = newID("build")
	b.CreatedAt = time.Now().UTC()
	if b.Status == "" {
		b.Status = "uploaded"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO builds (`+buildColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.ID, b.Platform, b.Filename, b.Path, b.SizeBytes, b.Version, b.Status, b.Note, formatTime(b.CreatedAt))
	return b, err
}

func (s *SQLite) GetBuild(ctx context.Context, id string) (domain.Build, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+buildColumns+` FROM builds WHERE id = ?`, id)
	return scanBuild(row)
}

func (s *SQLite) ListBuilds(ctx context.Context, platform string) ([]domain.Build, error) {
	q := `SELECT ` + buildColumns + ` FROM builds`
	args := []any{}
	if platform != "" {
		q += ` WHERE platform = ?`
		args = append(args, platform)
	}
	q += ` ORDER BY created_at DESC LIMIT 200`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	builds := make([]domain.Build, 0)
	for rows.Next() {
		b, err := scanBuild(rows)
		if err != nil {
			return nil, err
		}
		builds = append(builds, b)
	}
	return builds, rows.Err()
}

func (s *SQLite) UpdateBuildStatus(ctx context.Context, id, status, note string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE builds SET status=?, note=? WHERE id=?`, status, note, id)
	return err
}

func scanBuild(row scanner) (domain.Build, error) {
	var b domain.Build
	var created string
	if err := row.Scan(&b.ID, &b.Platform, &b.Filename, &b.Path, &b.SizeBytes, &b.Version, &b.Status, &b.Note, &created); err != nil {
		return domain.Build{}, err
	}
	b.CreatedAt = parseTime(created)
	return b, nil
}

// ---- helpers ----

func nonNilStrings(v []string) []string {
	if v == nil {
		return []string{}
	}
	return v
}

func nonNilSteps(v []domain.TestStep) []domain.TestStep {
	if v == nil {
		return []domain.TestStep{}
	}
	return v
}

func reindexSteps(steps []domain.TestStep) {
	for i := range steps {
		steps[i].Index = i
	}
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return formatTime(*t)
}

func parseNullableTime(v sql.NullString) *time.Time {
	if !v.Valid || v.String == "" {
		return nil
	}
	t := parseTime(v.String)
	return &t
}
