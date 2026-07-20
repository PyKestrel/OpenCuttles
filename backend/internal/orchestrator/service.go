package orchestrator

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/json"
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
	"github.com/opencuttles/opencuttles/backend/internal/vision"
)

type Service struct {
	store  *store.SQLite
	runner Runner
	logger *slog.Logger
}

func NewService(store *store.SQLite, runner Runner, logger *slog.Logger) *Service {
	return &Service{store: store, runner: runner, logger: logger}
}

// setState persists an instance's state. A dropped write here is how the
// dashboard silently starts lying about a device (DB says running, reality
// disagrees), so the error is always logged rather than discarded. The returned
// instance is zero-valued on failure — callers that use it should tolerate that.
func (s *Service) setState(ctx context.Context, id, state, message string) domain.Instance {
	instance, err := s.store.UpdateInstanceState(ctx, id, state, message)
	if err != nil && s.logger != nil {
		s.logger.Error("persist instance state failed", "instance", id, "state", state, "detail", message, "error", err)
	}
	return instance
}

// finishOp marks an operation terminal, logging (never swallowing) a failed
// write — otherwise an operation sticks at "running" forever with no trace.
func (s *Service) finishOp(ctx context.Context, operationID, status, message string) {
	if _, err := s.store.FinishOperation(ctx, operationID, status, message); err != nil && s.logger != nil {
		s.logger.Error("persist operation status failed", "operation", operationID, "status", status, "detail", message, "error", err)
	}
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
			s.userNamespaceCheck(),
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

	// Disk is the failure the appliance actually hits: Android images run 10-20
	// GB each and test artifacts accumulate, so surface it before it wedges.
	if check, degraded := s.diskCheck(); check.Name != "" {
		checks = append(checks, check)
		if degraded {
			status = "degraded"
		}
	}

	// The vision sidecar is the grounding engine for every agent test. Without
	// this probe the dashboard reports green while no test can actually run.
	if check, degraded := s.visionCheck(ctx); check.Name != "" {
		checks = append(checks, check)
		if degraded {
			status = "degraded"
		}
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
					s.setState(ctx, instance.ID, domain.StateRunning, "")
					continue
				}
			}
			s.setState(ctx, instance.ID, domain.StateError, "startup reconciliation found interrupted launch")
		case domain.StateRunning:
			if realCuttlefishExecutionEnabled() && !s.isADBReachable(ctx, instance) {
				s.setState(ctx, instance.ID, domain.StateError, "startup reconciliation could not reach ADB")
			}
		case domain.StateStopping, domain.StateDeleting:
			s.setState(ctx, instance.ID, domain.StateError, "startup reconciliation found interrupted operation")
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
		s.finishOp(ctx, operationID, "failed", err.Error())
		return
	}
	if err := s.launch(ctx, instance); err != nil {
		instance = s.setState(ctx, id, domain.StateError, err.Error())
		s.finishOp(ctx, operationID, "failed", err.Error())
		return
	}

	instance, err = s.store.UpdateInstanceState(ctx, id, domain.StateBooting, "")
	if err != nil {
		s.finishOp(ctx, operationID, "failed", err.Error())
		return
	}

	if err := s.waitReady(ctx, instance); err != nil {
		s.setState(ctx, id, domain.StateError, err.Error())
		s.finishOp(ctx, operationID, "failed", err.Error())
		return
	}

	instance, err = s.store.UpdateInstanceState(ctx, id, domain.StateRunning, "")
	if err != nil {
		s.finishOp(ctx, operationID, "failed", err.Error())
		return
	}
	s.finalizeConsole(ctx, instance)
	s.finishOp(ctx, operationID, "succeeded", "instance is running")
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
		s.setState(ctx, id, domain.StateError, err.Error())
		s.finishOp(ctx, operationID, "failed", err.Error())
	}

	instance, err := s.store.GetInstance(ctx, id)
	if err != nil {
		s.finishOp(ctx, operationID, "failed", err.Error())
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
	s.finalizeConsole(ctx, instance)
	s.finishOp(ctx, operationID, "succeeded", "instance is running")
}

// finalizeConsole asks the cuttlefish-operator for the device id it assigned to
// this instance (matched by ADB port) and records the console URL derived from
// it. Best-effort: the console simply stays on its provisional id if discovery
// fails.
func (s *Service) finalizeConsole(ctx context.Context, instance domain.Instance) {
	if !realCuttlefishExecutionEnabled() {
		return
	}
	deviceID, ok := s.discoverDeviceID(instance)
	if !ok {
		return
	}
	if _, err := s.store.UpdateInstanceConsole(ctx, instance.ID, deviceID, store.ConsoleClientURL(instance.ID, deviceID)); err != nil && s.logger != nil {
		// Losing this write leaves the device without a working console URL.
		s.logger.Error("persist instance console failed", "instance", instance.ID, "device", deviceID, "error", err)
	}
}

// discoverDeviceID resolves the operator's device id for this instance via the
// operator's /devices endpoint, matching on ADB port (falling back to the group
// name). Returns false when the operator is unreachable or the device is absent.
func (s *Service) discoverDeviceID(instance domain.Instance) (string, bool) {
	client := http.Client{
		Timeout:   3 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	resp, err := client.Get(fmt.Sprintf("https://127.0.0.1:%d/devices", operatorPort()))
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var devices []struct {
		DeviceID  string `json:"device_id"`
		GroupName string `json:"group_name"`
		ADBPort   int    `json:"adb_port"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&devices); err != nil {
		return "", false
	}
	for _, d := range devices {
		if d.ADBPort == instance.ADBPort && d.DeviceID != "" {
			return d.DeviceID, true
		}
	}
	group := groupName(instance.ADBPort - 6520 + 1)
	for _, d := range devices {
		if d.GroupName == group && d.DeviceID != "" {
			return d.DeviceID, true
		}
	}
	return "", false
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
		s.markImageError(ctx, image.ID, err.Error())
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
		s.markImageError(ctx, image.ID, msg)
		return fmt.Errorf("%s", msg)
	}
	return s.store.UpdateImageStatus(ctx, image.ID, domain.ImageStatusReady, 0, "")
}

// markImageError records an image's failure. A dropped write would strand the
// image at "fetching" forever with no trace, so the error is logged.
func (s *Service) markImageError(ctx context.Context, imageID, message string) {
	if err := s.store.UpdateImageStatus(ctx, imageID, domain.ImageStatusError, 0, message); err != nil && s.logger != nil {
		s.logger.Error("persist image error status failed", "image", imageID, "detail", message, "error", err)
	}
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
		instance = s.setState(ctx, id, domain.StateError, err.Error())
		s.finishOp(ctx, operationID, "failed", err.Error())
		return
	}

	_, err := s.store.UpdateInstanceState(ctx, id, domain.StateStopped, "")
	if err != nil {
		s.finishOp(ctx, operationID, "failed", err.Error())
		return
	}
	s.finishOp(ctx, operationID, "succeeded", "instance is stopped")
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
			s.setState(ctx, id, domain.StateError, err.Error())
			s.finishOp(ctx, operationID, "failed", err.Error())
			return
		}
	}

	// Release the cvd group from the instance database so its number/resources
	// are reusable and a future create cannot collide. Best-effort.
	if realCuttlefishExecutionEnabled() {
		instanceNumber := instance.ADBPort - 6520 + 1
		instanceDir := filepath.Join(instanceRoot(), instance.ID)
		s.clearStaleGroup(ctx, instanceDir, instanceNumber)
	}

	if err := s.store.DeleteInstance(ctx, id); err != nil {
		s.finishOp(ctx, operationID, "failed", err.Error())
		return
	}
	s.finishOp(ctx, operationID, "succeeded", "instance deleted")
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
	// cvd derives the host ADB port from the instance number (6520 +
	// base_instance_num - 1); there is no --adb_port flag. base_instance_num is
	// chosen so the derived port equals instance.ADBPort.
	args := []string{
		"--num_instances=1",
		fmt.Sprintf("--base_instance_num=%d", instanceNumber),
		"--start_webrtc=true",
		fmt.Sprintf("--cpus=%d", instance.CPUCores),
		fmt.Sprintf("--memory_mb=%d", instance.MemoryMB),
	}
	if instance.DisplayWidth > 0 && instance.DisplayHeight > 0 {
		args = append(args, fmt.Sprintf("--x_res=%d", instance.DisplayWidth), fmt.Sprintf("--y_res=%d", instance.DisplayHeight))
	}
	if instance.DPI > 0 {
		args = append(args, fmt.Sprintf("--dpi=%d", instance.DPI))
	}
	// Clear any stale group left by a crash or earlier failed launch so "cvd
	// create" does not fail with "conflicts with existing instance".
	s.clearStaleGroup(ctx, instanceDir, instanceNumber)
	command, commandArgs := s.startCommand(args, image.Path, instanceDir, groupName(instanceNumber))
	// Run from the instance directory (owned by the service user). cvd opens the
	// cwd; inheriting the systemd WorkingDirectory or an admin home would fail
	// with PERMISSION_DENIED.
	result, err := s.runner.RunInDir(ctx, instanceDir, command, commandArgs...)
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", command, err, result.Output)
	}
	return nil
}

func (s *Service) startCommand(launchArgs []string, imagePath, instanceDir, group string) (string, []string) {
	if _, err := s.runner.LookPath("launch_cvd"); err == nil {
		args := append([]string{}, launchArgs...)
		args = append(args, "--system_image_dir="+imagePath, "--instance_dir="+instanceDir)
		return "launch_cvd", args
	}
	// Modern cvd: "create" provisions a new instance group from the images and
	// starts it ("cvd start" only resumes an already-created group). cvd locates
	// the fetched host tools + images via --host_path/--product_path, both of
	// which "cvd fetch" populated under the image directory. A stable group name
	// lets stop/remove target this instance deterministically.
	args := append([]string{"create"}, launchArgs...)
	args = append(args, "--host_path="+imagePath, "--product_path="+imagePath, "--group_name="+group)
	return "cvd", args
}

// groupName is the cvd instance-group name OpenCuttles assigns to an instance.
// cvd would otherwise auto-name groups like "cvd_1"; setting it explicitly makes
// stop and remove deterministic across restarts.
func groupName(instanceNumber int) string {
	return fmt.Sprintf("cvd_%d", instanceNumber)
}

// clearStaleGroup best-effort stops and removes any existing cvd group with this
// instance's name. Errors are intentionally ignored: a missing group (the normal
// first-launch case) reports an error that is not actionable here.
func (s *Service) clearStaleGroup(ctx context.Context, dir string, instanceNumber int) {
	if _, err := s.runner.LookPath("launch_cvd"); err == nil {
		return // legacy toolchain tracks instances by number, not named groups
	}
	group := groupName(instanceNumber)
	_, _ = s.runner.RunInDir(ctx, dir, "cvd", "stop", "--group_name="+group)
	_, _ = s.runner.RunInDir(ctx, dir, "cvd", "remove", "--group_name="+group)
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
	instanceDir := filepath.Join(instanceRoot(), instance.ID)
	result, err := s.runner.RunInDir(ctx, instanceDir, command, args...)
	if err != nil {
		return fmt.Errorf("%s failed: %w: %s", command, err, result.Output)
	}
	return nil
}

func (s *Service) stopCommand(instanceNumber int) (string, []string) {
	if _, err := s.runner.LookPath("stop_cvd"); err == nil {
		return "stop_cvd", []string{fmt.Sprintf("--instance_num=%d", instanceNumber)}
	}
	return "cvd", []string{"stop", "--group_name=" + groupName(instanceNumber)}
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

// diskFreeWarnPercent is the free-space floor below which the appliance reports
// degraded. A single Cuttlefish image is 10-20 GB, so 10% on a typical appliance
// disk is roughly "one more image and you are wedged".
const diskFreeWarnPercent = 10

// diskCheck reports free space on the volume holding the image root. Returns a
// zero-valued check when free space cannot be determined, so an unsupported
// platform simply omits the check rather than reporting a false failure.
func (s *Service) diskCheck() (domain.HealthCheck, bool) {
	root := imageRootPath()
	free, total, err := diskFree(root)
	if err != nil || total == 0 {
		return domain.HealthCheck{}, false
	}
	percent := float64(free) / float64(total) * 100
	message := fmt.Sprintf("%.1f GiB free of %.1f GiB (%.0f%%) on %s",
		float64(free)/(1<<30), float64(total)/(1<<30), percent, root)

	if percent < diskFreeWarnPercent {
		return domain.HealthCheck{
			Name:    "disk_space",
			Status:  "failed",
			Message: message + " — delete unused images or prune artifacts",
		}, true
	}
	return domain.HealthCheck{Name: "disk_space", Status: "ok", Message: message}, false
}

// visionCheck probes the Florence-2 sidecar. It is only reported when a vision
// URL is actually configured: on an install that never enabled vision, a failing
// check would be noise rather than signal.
func (s *Service) visionCheck(ctx context.Context) (domain.HealthCheck, bool) {
	client := vision.NewFromEnv()
	if !client.Configured() {
		return domain.HealthCheck{}, false
	}
	probeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()

	if err := client.Ping(probeCtx); err != nil {
		return domain.HealthCheck{
			Name:    "vision",
			Status:  "failed",
			Message: fmt.Sprintf("%s unreachable: %v — agent tests cannot ground on screen content", client.BaseURL(), err),
		}, true
	}
	return domain.HealthCheck{Name: "vision", Status: "ok", Message: client.BaseURL()}, false
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

// userNamespaceCheck verifies the host allows the unprivileged user namespaces
// that crosvm's minijail sandbox requires. Ubuntu 23.10+/24.04 default this to
// restricted, which makes crosvm fail ("unshare(CLONE_NEWNS): Operation not
// permitted") and the guest never boots.
func (s *Service) userNamespaceCheck() domain.Prerequisite {
	const remedy = "Set kernel.apparmor_restrict_unprivileged_userns=0 (and kernel.unprivileged_userns_clone=1) via /etc/sysctl.d, then `sudo sysctl --system`."
	if data, err := os.ReadFile("/proc/sys/kernel/apparmor_restrict_unprivileged_userns"); err == nil {
		if strings.TrimSpace(string(data)) == "1" {
			return domain.Prerequisite{Name: "user-namespaces", OK: false, Detail: "apparmor_restrict_unprivileged_userns=1 blocks crosvm sandbox", Remedy: remedy}
		}
	}
	if data, err := os.ReadFile("/proc/sys/kernel/unprivileged_userns_clone"); err == nil {
		if strings.TrimSpace(string(data)) == "0" {
			return domain.Prerequisite{Name: "user-namespaces", OK: false, Detail: "unprivileged_userns_clone=0 blocks crosvm sandbox", Remedy: remedy}
		}
	}
	return domain.Prerequisite{Name: "user-namespaces", OK: true, Detail: "unprivileged user namespaces permitted"}
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

// diskFreeBytes reports free space on the volume holding the image root.
//
// This used to shell out to `df -Pk /var/lib` and parse the output: it spawned a
// subprocess per health poll, reported zero on any non-Linux host, and measured
// /var/lib rather than the configured image root — which is often a separate
// mount, so the number could describe the wrong filesystem entirely. It now uses
// the same statfs call as diskCheck.
func (s *Service) diskFreeBytes(context.Context) uint64 {
	free, _, err := diskFree(imageRootPath())
	if err != nil {
		return 0
	}
	return free
}

// imageRootPath is where images live; the volume holding it is what actually
// fills up.
func imageRootPath() string {
	root := strings.TrimSpace(os.Getenv("OPENCUTTLES_IMAGE_ROOT"))
	if root == "" {
		root = "/var/lib/opencuttles/images"
	}
	return root
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
