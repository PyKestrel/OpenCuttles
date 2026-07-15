// Package scenario runs vision-grounded device tests. Each test is an ordered
// list of atomic natural-language steps. The runner classifies each step's verb
// deterministically (regex — no model in the parse path, so runs are
// reproducible) and grounds the target visually with the Florence-2 sidecar:
// actions locate-and-tap, assertions caption+OCR the screen and check for the
// expected text. Because grounding re-resolves from pixels every run, tests
// self-heal across layout changes.
//
// Evidence per run: a per-step screenshot (with the grounding point recorded),
// per-step model output/pass/timing, and a full-session screenrecord video.
package scenario

import (
	"bytes"
	"context"
	"fmt"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/devicecontrol"
	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/vision"
)

// Store is the persistence surface the runner needs.
type Store interface {
	GetTest(ctx context.Context, id string) (domain.Test, error)
	GetInstance(ctx context.Context, id string) (domain.Instance, error)
	CreateTestRun(ctx context.Context, testID, instanceID string) (domain.TestRun, error)
	UpdateTestRun(ctx context.Context, run domain.TestRun) error
}

type Runner struct {
	store    Store
	devices  *devicecontrol.Service
	vision   *vision.Client
	logger   *slog.Logger
	artifact string
}

// ArtifactRoot resolves where run evidence is stored on disk.
func ArtifactRoot() string {
	if root := os.Getenv("OPENCUTTLES_ARTIFACT_ROOT"); root != "" {
		return root
	}
	return "./data/artifacts"
}

func New(store Store, devices *devicecontrol.Service, vis *vision.Client, logger *slog.Logger) *Runner {
	return &Runner{store: store, devices: devices, vision: vis, logger: logger, artifact: ArtifactRoot()}
}

// Start launches a test run asynchronously and returns the pending run record.
// Progress is persisted after every step so the report endpoint can stream it.
func (r *Runner) Start(ctx context.Context, testID, instanceID string) (domain.TestRun, error) {
	test, err := r.store.GetTest(ctx, testID)
	if err != nil {
		return domain.TestRun{}, err
	}
	instance, err := r.store.GetInstance(ctx, instanceID)
	if err != nil {
		return domain.TestRun{}, err
	}
	if instance.State != domain.StateRunning {
		return domain.TestRun{}, fmt.Errorf("instance %s is not running", instance.Name)
	}
	run, err := r.store.CreateTestRun(ctx, test.ID, instance.ID)
	if err != nil {
		return domain.TestRun{}, err
	}
	// Detached from the request context: the run outlives the HTTP request.
	go r.execute(context.Background(), test, instance, run)
	return run, nil
}

func (r *Runner) execute(ctx context.Context, test domain.Test, instance domain.Instance, run domain.TestRun) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Minute)
	defer cancel()

	runDir := filepath.Join(r.artifact, run.ID)
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		r.finish(ctx, run, false, fmt.Sprintf("create artifact dir: %v", err))
		return
	}

	// screenrecord is adb-only; desktop runs rely on per-step screenshots.
	recording := instance.Platform == "" || instance.Platform == domain.PlatformAndroid
	if recording {
		recording = r.devices.StartRecording(ctx, instance.ID) == nil
	}

	passed := true
	for i, text := range test.Steps {
		result := r.runStep(ctx, instance.ID, i, text, runDir)
		run.Steps = append(run.Steps, result)
		_ = r.store.UpdateTestRun(ctx, run)
		if !result.Pass {
			passed = false
			break // remaining steps depend on this one
		}
		// Let the UI settle before observing the next step.
		select {
		case <-ctx.Done():
			r.finish(ctx, run, false, "run timed out")
			return
		case <-time.After(1200 * time.Millisecond):
		}
	}

	if recording {
		if video, err := r.devices.StopRecording(ctx, instance.ID); err == nil {
			name := "session.mp4"
			if err := os.WriteFile(filepath.Join(runDir, name), video, 0o644); err == nil {
				run.Video = name
			}
		} else if r.logger != nil {
			r.logger.Warn("screenrecord failed", "run", run.ID, "error", err)
		}
	}
	r.finish(ctx, run, passed, "")
}

func (r *Runner) finish(ctx context.Context, run domain.TestRun, passed bool, errMsg string) {
	now := time.Now().UTC()
	run.FinishedAt = &now
	run.Passed = passed && errMsg == ""
	run.Error = errMsg
	if run.Passed {
		run.Status = "passed"
	} else {
		run.Status = "failed"
	}
	if err := r.store.UpdateTestRun(ctx, run); err != nil && r.logger != nil {
		r.logger.Error("persist test run failed", "run", run.ID, "error", err)
	}
}

// runStep executes one step and returns its result with evidence attached.
func (r *Runner) runStep(ctx context.Context, instanceID string, index int, text, runDir string) domain.StepResult {
	start := time.Now()
	result := domain.StepResult{Index: index, Text: text}
	result.Verb, result.Target, result.Value = Classify(text)

	// Screenshot first: it is both the grounding input and the step evidence.
	shot, err := r.devices.Screenshot(ctx, instanceID)
	if err != nil {
		return fail(result, start, fmt.Sprintf("screenshot: %v", err))
	}
	name := fmt.Sprintf("step-%02d.png", index)
	if err := os.WriteFile(filepath.Join(runDir, name), shot, 0o644); err == nil {
		result.Screenshot = name
	}
	if snap, err := r.devices.Perf(ctx, instanceID, ""); err == nil {
		result.Battery = snap.BatteryLevel
	}

	switch result.Verb {
	case VerbWait:
		select {
		case <-ctx.Done():
			return fail(result, start, "cancelled")
		case <-time.After(2 * time.Second):
		}
		return pass(result, start)

	case VerbKey:
		if err := r.devices.Key(ctx, instanceID, result.Target); err != nil {
			return fail(result, start, err.Error())
		}
		return pass(result, start)

	case VerbOpen:
		if pkg, ok := knownPackages[strings.ToLower(result.Target)]; ok {
			if err := r.devices.LaunchApp(ctx, instanceID, pkg); err != nil {
				return fail(result, start, err.Error())
			}
			return pass(result, start)
		}
		// Unknown app name: fall through to tapping its icon by description.
		return r.groundAndTap(ctx, instanceID, result, shot, runDir, start)

	case VerbTap:
		return r.groundAndTap(ctx, instanceID, result, shot, runDir, start)

	case VerbType:
		// Ground the field, tap to focus, then type.
		if result.Target != "" {
			res := r.groundAndTap(ctx, instanceID, result, shot, runDir, start)
			if !res.Pass {
				return res
			}
			result = res
			time.Sleep(600 * time.Millisecond)
		}
		if err := r.devices.Text(ctx, instanceID, result.Value); err != nil {
			return fail(result, start, err.Error())
		}
		return pass(result, start)

	case VerbSwipe:
		// Direction words scroll a screen-relative gesture.
		x1, y1, x2, y2 := swipeCoords(result.Target, shot)
		if err := r.devices.Swipe(ctx, instanceID, x1, y1, x2, y2, 300); err != nil {
			return fail(result, start, err.Error())
		}
		return pass(result, start)

	case VerbAssert:
		// Retry like grounding: the asserted content may still be rendering
		// after a preceding navigation when this step begins.
		for attempt := 0; attempt < 3; attempt++ {
			answer, err := r.vision.Query(ctx, shot, "Describe the screen and all visible text.")
			if err != nil {
				return fail(result, start, fmt.Sprintf("vision query: %v", err))
			}
			result.ModelOut = answer
			if containsLoosely(answer, result.Target) {
				return pass(result, start)
			}
			select {
			case <-ctx.Done():
				return fail(result, start, "cancelled")
			case <-time.After(1500 * time.Millisecond):
			}
			if fresh, err := r.devices.Screenshot(ctx, instanceID); err == nil {
				shot = fresh
				if result.Screenshot != "" {
					_ = os.WriteFile(filepath.Join(runDir, result.Screenshot), shot, 0o644)
				}
			}
		}
		return fail(result, start, fmt.Sprintf("expected %q on screen", result.Target))

	default:
		return fail(result, start, fmt.Sprintf("could not understand step %q", text))
	}
}

// groundAndTap locates the target and taps it, retrying with fresh screenshots
// because a screen may still be rendering (e.g. an app cold-launch) when the
// step begins. The evidence screenshot is updated to the capture that grounded.
func (r *Runner) groundAndTap(ctx context.Context, instanceID string, result domain.StepResult, shot []byte, runDir string, start time.Time) domain.StepResult {
	var points []vision.Point
	for attempt := 0; attempt < 4; attempt++ {
		pts, err := r.vision.Locate(ctx, shot, result.Target)
		if err != nil {
			return fail(result, start, fmt.Sprintf("vision point: %v", err))
		}
		if len(pts) > 0 {
			points = pts
			break
		}
		// Not found yet: let the screen settle and re-capture for another try.
		select {
		case <-ctx.Done():
			return fail(result, start, "cancelled")
		case <-time.After(1500 * time.Millisecond):
		}
		if fresh, err := r.devices.Screenshot(ctx, instanceID); err == nil {
			shot = fresh
			if result.Screenshot != "" {
				_ = os.WriteFile(filepath.Join(runDir, result.Screenshot), shot, 0o644)
			}
		}
	}
	if len(points) == 0 {
		return fail(result, start, fmt.Sprintf("could not find %q on screen", result.Target))
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(shot))
	if err != nil {
		return fail(result, start, fmt.Sprintf("decode screenshot: %v", err))
	}
	result.X, result.Y = points[0].Pixels(cfg.Width, cfg.Height)
	if err := r.devices.Tap(ctx, instanceID, result.X, result.Y); err != nil {
		return fail(result, start, err.Error())
	}
	return pass(result, start)
}

func pass(result domain.StepResult, start time.Time) domain.StepResult {
	result.Pass = true
	result.DurationMs = time.Since(start).Milliseconds()
	return result
}

func fail(result domain.StepResult, start time.Time, detail string) domain.StepResult {
	result.Pass = false
	result.Detail = detail
	result.DurationMs = time.Since(start).Milliseconds()
	return result
}

// --- deterministic step classification --------------------------------------

const (
	VerbTap    = "tap"
	VerbType   = "type"
	VerbOpen   = "open"
	VerbSwipe  = "swipe"
	VerbAssert = "assert"
	VerbWait   = "wait"
	VerbKey    = "key"
)

var knownPackages = map[string]string{
	"settings":  "com.android.settings",
	"clock":     "com.android.deskclock",
	"chrome":    "com.android.chrome",
	"browser":   "com.android.chrome",
	"contacts":  "com.android.contacts",
	"phone":     "com.android.dialer",
	"dialer":    "com.android.dialer",
	"messaging": "com.android.messaging",
	"messages":  "com.android.messaging",
	"camera":    "com.android.camera2",
	"gallery":   "com.android.gallery3d",
}

var (
	reOpen   = regexp.MustCompile(`(?i)^(?:open|launch|start)\s+(?:the\s+)?(.+?)(?:\s+app)?\s*$`)
	reType   = regexp.MustCompile(`(?i)^(?:type|enter|input|write)\s+"?([^"]+?)"?(?:\s+(?:into|in|to)\s+(?:the\s+)?(.+?))?\s*$`)
	reTap    = regexp.MustCompile(`(?i)^(?:tap|click|press|select|choose|toggle)\s+(?:on\s+)?(?:the\s+)?(.+?)\s*$`)
	reSwipe  = regexp.MustCompile(`(?i)^(?:swipe|scroll)\s*(?:to(?:wards)?\s+)?(?:the\s+)?(up|down|left|right)?\w*\s*$`)
	reAssert = regexp.MustCompile(`(?i)^(?:assert|verify|check|confirm|expect)(?:\s+that)?\s+"?(.+?)"?(?:\s+(?:is|are)\s+(?:visible|shown|displayed|on(?:\s+the)?\s+screen|present))?\s*$`)
	reWait   = regexp.MustCompile(`(?i)^wait\b`)
	reSee    = regexp.MustCompile(`(?i)^(?:i\s+)?(?:should\s+)?see\s+"?(.+?)"?\s*$`)
)

var keyNames = map[string]string{
	"home": "HOME", "back": "BACK", "recents": "APP_SWITCH", "enter": "ENTER",
}

// Classify maps a natural-language step to (verb, target, value) with plain
// regex — deterministic and fast, per the plan.
func Classify(text string) (verb, target, value string) {
	step := strings.TrimSpace(text)
	switch {
	case reWait.MatchString(step):
		return VerbWait, "", ""
	case reAssert.MatchString(step):
		return VerbAssert, reAssert.FindStringSubmatch(step)[1], ""
	case reSee.MatchString(step):
		return VerbAssert, reSee.FindStringSubmatch(step)[1], ""
	case reType.MatchString(step):
		m := reType.FindStringSubmatch(step)
		return VerbType, strings.TrimSpace(m[2]), strings.TrimSpace(m[1])
	case reOpen.MatchString(step):
		return VerbOpen, strings.TrimSpace(reOpen.FindStringSubmatch(step)[1]), ""
	case reSwipe.MatchString(step):
		dir := strings.ToLower(reSwipe.FindStringSubmatch(step)[1])
		if dir == "" {
			dir = "up"
		}
		return VerbSwipe, dir, ""
	case reTap.MatchString(step):
		t := strings.TrimSpace(reTap.FindStringSubmatch(step)[1])
		if key, ok := keyNames[strings.ToLower(t)]; ok {
			return VerbKey, key, ""
		}
		return VerbTap, t, ""
	}
	return "", step, ""
}

// swipeCoords converts a direction word into a centered, screen-relative
// gesture. "up" scrolls content up (finger moves up).
func swipeCoords(direction string, shot []byte) (int, int, int, int) {
	w, h := 720, 1280
	if cfg, err := png.DecodeConfig(bytes.NewReader(shot)); err == nil {
		w, h = cfg.Width, cfg.Height
	}
	cx, cy := w/2, h/2
	dx, dy := w/3, h/3
	switch direction {
	case "down":
		return cx, cy - dy, cx, cy + dy
	case "left":
		return cx + dx, cy, cx - dx, cy
	case "right":
		return cx - dx, cy, cx + dx, cy
	default: // up
		return cx, cy + dy, cx, cy - dy
	}
}

// containsLoosely checks whether all significant words of expected appear in
// haystack, case-insensitively — tolerant of caption phrasing differences.
func containsLoosely(haystack, expected string) bool {
	hay := strings.ToLower(haystack)
	words := strings.Fields(strings.ToLower(expected))
	matched := 0
	for _, w := range words {
		w = strings.Trim(w, `.,!?"'`)
		if len(w) <= 2 || w == "the" || w == "and" {
			matched++ // skip stop-words
			continue
		}
		if strings.Contains(hay, w) {
			matched++
		}
	}
	return len(words) > 0 && matched == len(words)
}
