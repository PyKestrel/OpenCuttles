package orchestrator

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

type CommandResult struct {
	Command  string
	Args     []string
	Output   string
	Duration time.Duration
}

type Runner interface {
	Run(ctx context.Context, command string, args ...string) (CommandResult, error)
	// RunInDir runs a command with its working directory set to dir. Cuttlefish's
	// "cvd create" opens the current working directory and fails if the service
	// user cannot access it (e.g. the systemd WorkingDirectory or an admin's
	// home). Running from an instance directory the service owns avoids that.
	RunInDir(ctx context.Context, dir, command string, args ...string) (CommandResult, error)
	LookPath(command string) (string, error)
}

type ExecRunner struct {
	logger *slog.Logger
}

func NewExecRunner(logger *slog.Logger) ExecRunner {
	return ExecRunner{logger: logger}
}

func (r ExecRunner) Run(ctx context.Context, command string, args ...string) (CommandResult, error) {
	return r.RunInDir(ctx, "", command, args...)
}

func (r ExecRunner) RunInDir(ctx context.Context, dir, command string, args ...string) (CommandResult, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, command, args...)
	if dir != "" {
		cmd.Dir = dir
	}
	output, err := cmd.CombinedOutput()
	result := CommandResult{
		Command:  command,
		Args:     args,
		Output:   strings.TrimSpace(string(output)),
		Duration: time.Since(start),
	}
	if r.logger != nil {
		r.logger.Info("command finished", "command", command, "args", args, "dir", dir, "duration", result.Duration, "error", err)
	}
	return result, err
}

func (r ExecRunner) LookPath(command string) (string, error) {
	return exec.LookPath(command)
}

func realCuttlefishExecutionEnabled() bool {
	return os.Getenv("OPENCUTTLES_EXECUTE_CVD") == "1"
}

// operatorPort is the host-wide cuttlefish-operator HTTPS port that serves the
// WebRTC console. Current Cuttlefish uses 1443; older builds used 8443. Override
// with OPENCUTTLES_OPERATOR_PORT.
func operatorPort() int {
	if v := os.Getenv("OPENCUTTLES_OPERATOR_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil && p > 0 {
			return p
		}
	}
	return 1443
}
