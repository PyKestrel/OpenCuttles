package devicecontrol

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
)

// ExecRunner runs commands with exec and returns raw stdout. Unlike the
// orchestrator's runner it does not combine stderr into the output or trim it,
// because callers such as Screenshot need the exact binary bytes on stdout.
// stderr is captured separately and surfaced only when the command fails.
type ExecRunner struct {
	logger *slog.Logger
}

// NewExecRunner builds the default exec-backed runner. Exported so other
// packages (the physical-device poller) can shell out to adb with the same
// raw-stdout semantics rather than defining a near-duplicate.
func NewExecRunner(logger *slog.Logger) ExecRunner {
	return ExecRunner{logger: logger}
}

func (r ExecRunner) Run(ctx context.Context, stdin []byte, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	if len(stdin) > 0 {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if r.logger != nil {
		r.logger.Debug("devicecontrol command", "command", name, "args", args, "stdout_bytes", stdout.Len(), "error", err)
	}
	if err != nil {
		// Prefer stderr for the error detail; fall back to stdout.
		if stderr.Len() > 0 {
			return stderr.Bytes(), err
		}
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}
