package orchestrator

import (
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/opencuttles/opencuttles/backend/internal/domain"
	"github.com/opencuttles/opencuttles/backend/internal/store"
)

type Service struct {
	store  *store.SQLite
	runner Runner
	logger *slog.Logger
}

func NewService(store *store.SQLite, runner Runner, logger *slog.Logger) *Service {
	return &Service{store: store, runner: runner, logger: logger}
}

func (s *Service) Host(ctx context.Context) domain.Host {
	hostName, _ := os.Hostname()
	return domain.Host{
		ID:            "local",
		Name:          hostName,
		CPUCount:      runtime.NumCPU(),
		MemoryBytes:   memoryBytes(),
		DiskFreeBytes: s.diskFreeBytes(ctx),
		Prerequisites: []domain.Prerequisite{
			s.pathCheck("cvd", "Install Google Cuttlefish host tools."),
			s.pathCheck("adb", "Install Android platform tools."),
			s.kvmCheck(),
		},
		UpdatedAt: time.Now().UTC(),
	}
}

func (s *Service) Health(ctx context.Context) domain.HealthReport {
	host := s.Host(ctx)
	status := "ok"
	checks := []domain.HealthCheck{
		{Name: "execution_mode", Status: "ok", Message: executionMode()},
	}
	if err := s.store.Ping(ctx); err != nil {
		checks = append([]domain.HealthCheck{{Name: "database", Status: "failed", Message: err.Error()}}, checks...)
		status = "degraded"
	} else {
		checks = append([]domain.HealthCheck{{Name: "database", Status: "ok", Message: "SQLite ping succeeded"}}, checks...)
	}
	for _, prerequisite := range host.Prerequisites {
		checkStatus := "ok"
		if !prerequisite.OK {
			checkStatus = "failed"
			status = "degraded"
		}
		checks = append(checks, domain.HealthCheck{Name: prerequisite.Name, Status: checkStatus, Message: prerequisite.Detail})
	}
	return domain.HealthReport{Status: status, Checks: checks, GeneratedAt: time.Now().UTC()}
}

func (s *Service) Reconcile(ctx context.Context) error {
	instances, err := s.store.ListInstances(ctx)
	if err != nil {
		return err
	}
	for _, instance := range instances {
		switch instance.State {
		case domain.StateStarting, domain.StateBooting:
			if realCuttlefishExecutionEnabled() {
				if err := s.waitReady(ctx, instance); err == nil {
					_, _ = s.store.UpdateInstanceState(ctx, instance.ID, domain.StateRunning, "")
					continue
				}
			}
			_, _ = s.store.UpdateInstanceState(ctx, instance.ID, domain.StateError, "startup reconciliation found interrupted launch")
		case domain.StateRunning:
			if realCuttlefishExecutionEnabled() && !s.isADBReachable(ctx, instance) {
				_, _ = s.store.UpdateInstanceState(ctx, instance.ID, domain.StateError, "startup reconciliation could not reach ADB")
			}
		case domain.StateStopping, domain.StateDeleting:
			_, _ = s.store.UpdateInstanceState(ctx, instance.ID, domain.StateError, "startup reconciliation found interrupted operation")
		}
	}
	return nil
}

func (s *Service) StartInstance(ctx context.Context, id string) (domain.Instance, domain.Operation, error) {
	current, err := s.store.GetInstance(ctx, id)
	if err != nil {
		return domain.Instance{}, domain.Operation{}, err
	}
	if current.State != domain.StateStopped && current.State != domain.StateError {
		return domain.Instance{}, domain.Operation{}, fmt.Errorf("cannot start instance from state %q", current.State)
	}
	operation, err := s.store.CreateOperation(ctx, id, "start", "starting Cuttlefish instance")
	if err != nil {
		return domain.Instance{}, domain.Operation{}, err
	}

	instance, err := s.store.UpdateInstanceState(ctx, id, domain.StateStarting, "")
	if err != nil {
		return domain.Instance{}, operation, err
	}

	go s.runStart(id, operation.ID)
	return instance, operation, nil
}

func (s *Service) runStart(id, operationID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	instance, err := s.store.GetInstance(ctx, id)
	if err != nil {
		_, _ = s.store.FinishOperation(ctx, operationID, "failed", err.Error())
		return
	}
	if err := s.launch(ctx, instance); err != nil {
		instance, _ = s.store.UpdateInstanceState(ctx, id, domain.StateError, err.Error())
		_, _ = s.store.FinishOperation(ctx, operationID, "failed", err.Error())
		return
	}

	instance, err = s.store.UpdateInstanceState(ctx, id, domain.StateBooting, "")
	if err != nil {
		_, _ = s.store.FinishOperation(ctx, operationID, "failed", err.Error())
		return
	}

	if err := s.waitReady(ctx, instance); err != nil {
		_, _ = s.store.UpdateInstanceState(ctx, id, domain.StateError, err.Error())
		_, _ = s.store.FinishOperation(ctx, operationID, "failed", err.Error())
		return
	}

	instance, err = s.store.UpdateInstanceState(ctx, id, domain.StateRunning, "")
	if err != nil {
		_, _ = s.store.FinishOperation(ctx, operationID, "failed", err.Error())
		return
	}
	_, _ = s.store.FinishOperation(ctx, operationID, "succeeded", "instance is running")
}

func (s *Service) StopInstance(ctx context.Context, id string) (domain.Instance, domain.Operation, error) {
	current, err := s.store.GetInstance(ctx, id)
	if err != nil {
		return domain.Instance{}, domain.Operation{}, err
	}
	if !isPossiblyLive(current.State) {
		return domain.Instance{}, domain.Operation{}, fmt.Errorf("cannot stop instance from state %q", current.State)
	}
	operation, err := s.store.CreateOperation(ctx, id, "stop", "stopping Cuttlefish instance")
	if err != nil {
		return domain.Instance{}, domain.Operation{}, err
	}

	instance, err := s.store.UpdateInstanceState(ctx, id, domain.StateStopping, "")
	if err != nil {
		return domain.Instance{}, operation, err
	}

	go s.runStop(id, operation.ID, instance)
	return instance, operation, nil
}

func (s *Service) runStop(id, operationID string, instance domain.Instance) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	if err := s.stop(ctx, instance); err != nil {
		instance, _ = s.store.UpdateInstanceState(ctx, id, domain.StateError, err.Error())
		_, _ = s.store.FinishOperation(ctx, operationID, "failed", err.Error())
		return
	}

	_, err := s.store.UpdateInstanceState(ctx, id, domain.StateStopped, "")
	if err != nil {
		_, _ = s.store.FinishOperation(ctx, operationID, "failed", err.Error())
		return
	}
	_, _ = s.store.FinishOperation(ctx, operationID, "succeeded", "instance is stopped")
}

func (s *Service) DeleteInstance(ctx context.Context, id string) (domain.Operation, error) {
	instance, err := s.store.GetInstance(ctx, id)
	if err != nil {
		return domain.Operation{}, err
	}
	if instance.State == domain.StateDeleting {
		return domain.Operation{}, fmt.Errorf("instance is already deleting")
	}
	operation, err := s.store.CreateOperation(ctx, id, "delete", "deleting Cuttlefish instance")
	if err != nil {
		return domain.Operation{}, err
	}

	instance, err = s.store.UpdateInstanceState(ctx, id, domain.StateDeleting, "")
	if err != nil {
		return operation, err
	}

	go s.runDelete(id, operation.ID, instance)
	return operation, nil
}

func (s *Service) runDelete(id, operationID string, instance domain.Instance) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	if isPossiblyLive(instance.State) {
		if err := s.stop(ctx, instance); err != nil {
			_, _ = s.store.UpdateInstanceState(ctx, id, domain.StateError, err.Error())
			_, _ = s.store.FinishOperation(ctx, operationID, "failed", err.Error())
			return
		}
	}

	if err := s.store.DeleteInstance(ctx, id); err != nil {
		_, _ = s.store.FinishOperation(ctx, operationID, "failed", err.Error())
		return
	}
	_, _ = s.store.FinishOperation(ctx, operationID, "succeeded", "instance deleted")
}

func (s *Service) launch(ctx context.Context, instance domain.Instance) error {
	image, err := s.store.GetImage(ctx, instance.ImageID)
	if err != nil {
		return fmt.Errorf("image lookup: %w", err)
	}
	if err := store.ValidateImagePath(image.Path, realCuttlefishExecutionEnabled()); err != nil {
		return err
	}
	if !realCuttlefishExecutionEnabled() {
		return nil
	}
	instanceNumber := instance.ADBPort - 6520 + 1
	instanceDir := fmt.Sprintf("/var/lib/opencuttles/instances/%s", instance.ID)
	if err := os.MkdirAll(instanceDir, 0750); err != nil {
		return fmt.Errorf("create instance dir: %w", err)
	}
	args := []string{
		"--num_instances=1",
		fmt.Sprintf("--base_instance_num=%d", instanceNumber),
		fmt.Sprintf("--cpus=%d", instance.CPUCores),
		fmt.Sprintf("--memory_mb=%d", instance.MemoryMB),
		fmt.Sprintf("--system_image_dir=%s", image.Path),
		fmt.Sprintf("--instance_dir=%s", instanceDir),
		fmt.Sprintf("--adb_port=%d", instance.ADBPort),
		fmt.Sprintf("--webrtc_port=%d", instance.WebRTCPort),
	}
	command, commandArgs := s.startCommand(args)
	result, err := s.runner.Run(ctx, command, commandArgs...)
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", command, err, result.Output)
	}
	return nil
}

func (s *Service) startCommand(launchArgs []string) (string, []string) {
	if _, err := s.runner.LookPath("launch_cvd"); err == nil {
		return "launch_cvd", launchArgs
	}
	return "cvd", append([]string{"start"}, launchArgs...)
}

func (s *Service) isADBReachable(ctx context.Context, instance domain.Instance) bool {
	if !realCuttlefishExecutionEnabled() {
		return true
	}
	adbTarget := fmt.Sprintf("127.0.0.1:%d", instance.ADBPort)
	result, err := s.runner.Run(ctx, "adb", "-s", adbTarget, "get-state")
	return err == nil && strings.Contains(result.Output, "device")
}

func (s *Service) stop(ctx context.Context, instance domain.Instance) error {
	if !realCuttlefishExecutionEnabled() {
		return nil
	}
	instanceNumber := instance.ADBPort - 6520 + 1
	command, args := s.stopCommand(instanceNumber)
	result, err := s.runner.Run(ctx, command, args...)
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", command, err, result.Output)
	}
	return nil
}

func (s *Service) stopCommand(instanceNumber int) (string, []string) {
	if _, err := s.runner.LookPath("stop_cvd"); err == nil {
		return "stop_cvd", []string{fmt.Sprintf("--instance_num=%d", instanceNumber)}
	}
	return "cvd", []string{"stop", fmt.Sprintf("--instance_num=%d", instanceNumber)}
}

func (s *Service) waitReady(ctx context.Context, instance domain.Instance) error {
	if !realCuttlefishExecutionEnabled() {
		return nil
	}
	adbTarget := fmt.Sprintf("127.0.0.1:%d", instance.ADBPort)
	if result, err := s.runner.Run(ctx, "adb", "connect", adbTarget); err != nil {
		return fmt.Errorf("adb connect failed: %w: %s", err, result.Output)
	}
	deadline := time.Now().Add(4 * time.Minute)
	for time.Now().Before(deadline) {
		result, err := s.runner.Run(ctx, "adb", "-s", adbTarget, "shell", "getprop", "sys.boot_completed")
		if err == nil && strings.TrimSpace(result.Output) == "1" {
			return s.waitWebRTC(ctx, instance.WebRTCPort)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(3 * time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for Android boot completion")
}

func (s *Service) waitWebRTC(ctx context.Context, port int) error {
	client := http.Client{Timeout: 2 * time.Second}
	deadline := time.Now().Add(45 * time.Second)
	url := fmt.Sprintf("http://127.0.0.1:%d", port)
	for time.Now().Before(deadline) {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for Cuttlefish WebRTC endpoint")
}

func (s *Service) pathCheck(command, remedy string) domain.Prerequisite {
	path, err := s.runner.LookPath(command)
	if err != nil {
		return domain.Prerequisite{Name: command, OK: false, Detail: "not found on PATH", Remedy: remedy}
	}
	return domain.Prerequisite{Name: command, OK: true, Detail: path}
}

func (s *Service) kvmCheck() domain.Prerequisite {
	info, err := os.Stat("/dev/kvm")
	if err != nil {
		return domain.Prerequisite{Name: "/dev/kvm", OK: false, Detail: err.Error(), Remedy: "Enable KVM or nested virtualization and grant the service user access."}
	}
	return domain.Prerequisite{Name: "/dev/kvm", OK: true, Detail: info.Mode().String()}
}

func memoryBytes() uint64 {
	file, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, _ := strconv.ParseUint(fields[1], 10, 64)
				return kb * 1024
			}
		}
	}
	return 0
}

func (s *Service) diskFreeBytes(ctx context.Context) uint64 {
	if runtime.GOOS != "linux" {
		return 0
	}
	result, err := s.runner.Run(ctx, "df", "-Pk", "/var/lib")
	if err != nil {
		return 0
	}
	lines := strings.Split(result.Output, "\n")
	if len(lines) < 2 {
		return 0
	}
	fields := strings.Fields(lines[len(lines)-1])
	if len(fields) < 4 {
		return 0
	}
	kb, _ := strconv.ParseUint(fields[3], 10, 64)
	return kb * 1024
}

func executionMode() string {
	if realCuttlefishExecutionEnabled() {
		return "live Cuttlefish execution enabled"
	}
	return "dry-run mode; set OPENCUTTLES_EXECUTE_CVD=1 for live execution"
}

func isPossiblyLive(state string) bool {
	switch state {
	case domain.StateStarting, domain.StateBooting, domain.StateRunning, domain.StateStopping, domain.StateError:
		return true
	default:
		return false
	}
}
