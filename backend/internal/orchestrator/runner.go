package orchestrator

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
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
	LookPath(command string) (string, error)
}

type ExecRunner struct {
	logger *slog.Logger
}

func NewExecRunner(logger *slog.Logger) ExecRunner {
	return ExecRunner{logger: logger}
}

func (r ExecRunner) Run(ctx context.Context, command string, args ...string) (CommandResult, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, command, args...)
	output, err := cmd.CombinedOutput()
	result := CommandResult{
		Command:  command,
		Args:     args,
		Output:   strings.TrimSpace(string(output)),
		Duration: time.Since(start),
	}
	if r.logger != nil {
		r.logger.Info("command finished", "command", command, "args", args, "duration", result.Duration, "error", err)
	}
	return result, err
}

func (r ExecRunner) LookPath(command string) (string, error) {
	return exec.LookPath(command)
}

func realCuttlefishExecutionEnabled() bool {
	return os.Getenv("OPENCUTTLES_EXECUTE_CVD") == "1"
}
