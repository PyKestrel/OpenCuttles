package orchestrator

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
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
			s.virtualizationCheck(),
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

// Deploy provisions an instance end to end: it fetches the backing Android
// image when needed (cvd fetch), launches Cuttlefish, and waits for boot. It is
// the one-click path used right after an instance record is created.
func (s *Service) Deploy(ctx context.Context, id string) (domain.Operation, error) {
	current, err := s.store.GetInstance(ctx, id)
	if err != nil {
		return domain.Operation{}, err
	}
	if current.State != domain.StateStopped && current.State != domain.StateError {
		return domain.Operation{}, fmt.Errorf("cannot deploy instance from state %q", current.State)
	}
	operation, err := s.store.CreateOperation(ctx, id, "deploy", "deploying Android instance")
	if err != nil {
		return domain.Operation{}, err
	}
	if _, err := s.store.UpdateInstanceState(ctx, id, domain.StateProvisioning, ""); err != nil {
		return operation, err
	}
	go s.runDeploy(id, operation.ID)
	return operation, nil
}

func (s *Service) runDeploy(id, operationID string) {
	// Image fetches can pull several GB, so allow a generous deadline.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel()

	fail := func(err error) {
		_, _ = s.store.UpdateInstanceState(ctx, id, domain.StateError, err.Error())
		_, _ = s.store.FinishOperation(ctx, operationID, "failed", err.Error())
	}

	instance, err := s.store.GetInstance(ctx, id)
	if err != nil {
		_, _ = s.store.FinishOperation(ctx, operationID, "failed", err.Error())
		return
	}
	image, err := s.store.GetImage(ctx, instance.ImageID)
	if err != nil {
		fail(fmt.Errorf("image lookup: %w", err))
		return
	}
	if err := s.ensureImage(ctx, image); err != nil {
		fail(err)
		return
	}

	if _, err := s.store.UpdateInstanceState(ctx, id, domain.StateStarting, ""); err != nil {
		fail(err)
		return
	}
	if err := s.launch(ctx, instance); err != nil {
		fail(err)
		return
	}
	if _, err := s.store.UpdateInstanceState(ctx, id, domain.StateBooting, ""); err != nil {
		fail(err)
		return
	}
	if err := s.waitReady(ctx, instance); err != nil {
		fail(err)
		return
	}
	if _, err := s.store.UpdateInstanceState(ctx, id, domain.StateRunning, ""); err != nil {
		fail(err)
		return
	}
	_, _ = s.store.FinishOperation(ctx, operationID, "succeeded", "instance is running")
}

// ensureImage downloads the backing image with cvd fetch when it is not already
// present on disk. In dry-run mode it simply marks the image ready.
func (s *Service) ensureImage(ctx context.Context, image domain.Image) error {
	if !realCuttlefishExecutionEnabled() {
		if image.Status != domain.ImageStatusReady {
			return s.store.UpdateImageStatus(ctx, image.ID, domain.ImageStatusReady, 0, "")
		}
		return nil
	}
	// A custom registered image with no build target is expected to exist on disk already.
	if strings.TrimSpace(image.BuildTarget) == "" {
		if image.Status != domain.ImageStatusReady {
			return s.store.UpdateImageStatus(ctx, image.ID, domain.ImageStatusReady, 0, "")
		}
		return nil
	}
	// Trust a "ready" record only if the files are actually present. A ready
	// status can be left over from a dry-run deploy that never fetched anything,
	// or from a manually cleaned image directory; in those cases we re-fetch.
	if image.Status == domain.ImageStatusReady && imagePopulated(image.Path) {
		return nil
	}

	if err := s.store.UpdateImageStatus(ctx, image.ID, domain.ImageStatusFetching, 0, ""); err != nil {
		return err
	}
	if err := os.MkdirAll(image.Path, 0o750); err != nil {
		_ = s.store.UpdateImageStatus(ctx, image.ID, domain.ImageStatusError, 0, err.Error())
		return fmt.Errorf("create image dir: %w", err)
	}
	// cvd downloads artifacts to a cache (hardcoded at /var/tmp/cvd) and then
	// hardlinks them into the target directory. If that cache and the image
	// volume are on different filesystems, the hardlink fails with EXDEV. Keep
	// the cache on the image filesystem so the hardlink stays in-device.
	ensureCvdCacheColocated(image.Path)
	result, err := s.runner.Run(ctx, "cvd", "fetch",
		"--default_build="+image.BuildTarget,
		"--target_directory="+image.Path,
	)
	if err != nil {
		msg := fmt.Sprintf("cvd fetch failed: %v: %s", err, result.Output)
		_ = s.store.UpdateImageStatus(ctx, image.ID, domain.ImageStatusError, 0, msg)
		return fmt.Errorf("%s", msg)
	}
	return s.store.UpdateImageStatus(ctx, image.ID, domain.ImageStatusReady, 0, "")
}

// ensureCvdCacheColocated makes cvd's download cache (/var/tmp/cvd) live on the
// same filesystem as the image target, so cvd can hardlink fetched artifacts
// into the target directory instead of failing with EXDEV across mounts. The
// cache is placed beside the image root (the parent of imagePath). It only
// creates a symlink when /var/tmp/cvd is absent or already our symlink; it never
// clobbers an existing real directory (which may belong to another cvd user).
func ensureCvdCacheColocated(imagePath string) {
	cache := filepath.Join(filepath.Dir(imagePath), ".cvd-cache")
	if err := os.MkdirAll(cache, 0o750); err != nil {
		return
	}
	const link = "/var/tmp/cvd"
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink == 0 {
			// A real directory already exists; leave it alone.
			return
		}
		if dst, _ := os.Readlink(link); dst == cache {
			return
		}
		_ = os.Remove(link)
	}
	_ = os.MkdirAll(filepath.Dir(link), 0o1777)
	_ = os.Symlink(cache, link)
}

// imagePopulated reports whether an image directory exists and contains files.
func imagePopulated(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) > 0
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
	instanceDir := filepath.Join(instanceRoot(), instance.ID)
	if err := os.MkdirAll(instanceDir, 0750); err != nil {
		return fmt.Errorf("create instance dir: %w", err)
	}
	args := []string{
		"--num_instances=1",
		fmt.Sprintf("--base_instance_num=%d", instanceNumber),
		"--start_webrtc=true",
		fmt.Sprintf("--cpus=%d", instance.CPUCores),
		fmt.Sprintf("--memory_mb=%d", instance.MemoryMB),
		fmt.Sprintf("--system_image_dir=%s", image.Path),
		fmt.Sprintf("--instance_dir=%s", instanceDir),
		fmt.Sprintf("--adb_port=%d", instance.ADBPort),
	}
	if instance.DisplayWidth > 0 && instance.DisplayHeight > 0 {
		args = append(args, fmt.Sprintf("--x_res=%d", instance.DisplayWidth), fmt.Sprintf("--y_res=%d", instance.DisplayHeight))
	}
	if instance.DPI > 0 {
		args = append(args, fmt.Sprintf("--dpi=%d", instance.DPI))
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
			return s.waitWebRTC(ctx, operatorPort())
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
	// The cuttlefish-operator serves HTTPS with a self-signed certificate.
	client := http.Client{
		Timeout:   2 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	deadline := time.Now().Add(45 * time.Second)
	url := fmt.Sprintf("https://127.0.0.1:%d", port)
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

func (s *Service) virtualizationCheck() domain.Prerequisite {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return domain.Prerequisite{Name: "cpu-virtualization", OK: false, Detail: err.Error(), Remedy: "Run on a Linux host with hardware virtualization enabled."}
	}
	text := string(data)
	if strings.Contains(text, "vmx") || strings.Contains(text, "svm") {
		return domain.Prerequisite{Name: "cpu-virtualization", OK: true, Detail: "hardware virtualization (vmx/svm) available"}
	}
	return domain.Prerequisite{Name: "cpu-virtualization", OK: false, Detail: "vmx/svm flag not present", Remedy: "Enable nested virtualization for this VM/host."}
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

func instanceRoot() string {
	root := strings.TrimSpace(os.Getenv("OPENCUTTLES_INSTANCE_ROOT"))
	if root == "" {
		root = "/var/lib/opencuttles/instances"
	}
	return root
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
