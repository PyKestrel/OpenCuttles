package domain

import "time"

const (
	StateProvisioning = "provisioning"
	StateStarting     = "starting"
	StateBooting      = "booting"
	StateRunning      = "running"
	StateStopping     = "stopping"
	StateStopped      = "stopped"
	StateError        = "error"
	StateDeleting     = "deleting"
	// Desktop targets (Windows/Linux/macOS) use a simpler reachability lifecycle
	// than provisioned Cuttlefish VMs.
	StateOnline  = "online"
	StateOffline = "offline"

	// Platform identifies what a device is and which control driver runs it.
	// Empty is treated as android for pre-multi-OS rows.
	PlatformAndroid = "android"
	PlatformWindows = "windows"
	PlatformLinux   = "linux"
	PlatformMacOS   = "macos"

	ConsoleProviderCuttlefishWebRTC = "cuttlefish-webrtc"
	// ConsoleProviderScreenshot streams periodic screenshots for desktop targets
	// that have no WebRTC console.
	ConsoleProviderScreenshot = "screenshot"

	ImageStatusPending  = "pending"
	ImageStatusFetching = "fetching"
	ImageStatusReady    = "ready"
	ImageStatusError    = "error"

	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"

	PermissionAdmin     = "admin"
	PermissionOperate   = "operate"
	PermissionView      = "view"
	PermissionOpenConsole = "console"
	// PermissionControl guards interactive device control (input injection,
	// screenshots, shell, app install) via the devicecontrol service.
	PermissionControl = "control"
	// PermissionTest guards authoring and running vision-grounded device tests.
	PermissionTest = "test"
)

type Host struct {
	ID             string        `json:"id"`
	Name           string        `json:"name"`
	CPUCount       int           `json:"cpuCount"`
	MemoryBytes    uint64        `json:"memoryBytes"`
	DiskFreeBytes  uint64        `json:"diskFreeBytes"`
	Prerequisites  []Prerequisite `json:"prerequisites"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}

type Prerequisite struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Detail  string `json:"detail"`
	Remedy  string `json:"remedy,omitempty"`
}

type Image struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	AndroidAPI  string    `json:"androidApi,omitempty"`
	Description string    `json:"description,omitempty"`
	BuildTarget string    `json:"buildTarget,omitempty"`
	VersionID   string    `json:"versionId,omitempty"`
	Status      string    `json:"status,omitempty"`
	SizeBytes   int64     `json:"sizeBytes,omitempty"`
	LastError   string    `json:"lastError,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

type AndroidVersion struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Branch      string `json:"branch"`
	BuildTarget string `json:"buildTarget"`
	Description string `json:"description,omitempty"`
}

type Instance struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	HostID          string    `json:"hostId"`
	// Platform is android (default) or a desktop OS (windows/linux/macos).
	Platform        string    `json:"platform"`
	// ControlEndpoint is the desktop target's computer-use MCP server URL; empty
	// for Android (which is controlled over ADB).
	ControlEndpoint string    `json:"controlEndpoint,omitempty"`
	ImageID         string    `json:"imageId"`
	AndroidVersion  string    `json:"androidVersion,omitempty"`
	State           string    `json:"state"`
	CPUCores        int       `json:"cpuCores"`
	MemoryMB        int       `json:"memoryMb"`
	DisplayWidth    int       `json:"displayWidth"`
	DisplayHeight   int       `json:"displayHeight"`
	DPI             int       `json:"dpi"`
	ADBPort         int       `json:"adbPort"`
	WebRTCPort      int       `json:"webrtcPort"`
	DeviceID        string    `json:"deviceId"`
	ConsoleProvider string    `json:"consoleProvider"`
	ConsoleURL      string    `json:"consoleUrl"`
	LastError       string    `json:"lastError,omitempty"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`

	// Src is where this device comes from: a Cuttlefish VM we provision, a
	// physical handset we merely talk to, or a desktop running the dial-home
	// runner. Empty means cuttlefish, for rows written before this existed.
	//
	// This exists because "platform" was carrying two unrelated meanings. Code
	// kept asking "is it Android?" when it actually meant "is it a Cuttlefish VM
	// we launched?" — which is why a physical Android phone had nowhere to live.
	// Use Source(), not this field, so the legacy default is applied.
	Src string `json:"source,omitempty"`

	// ADBTarget is how ADB addresses this device: a USB serial, or host:port for
	// adb-over-TCP. Empty for Cuttlefish, which is addressed by its ADBPort.
	ADBTarget string `json:"adbTarget,omitempty"`
}

// Device sources.
const (
	SourceCuttlefish = "cuttlefish" // a VM this appliance launches and manages
	SourcePhysical   = "physical"   // a real handset reached over ADB
	SourceRunner     = "runner"     // a desktop running the dial-home runner
)

// Source returns the device's source, treating an empty value as cuttlefish so
// rows predating the column keep working.
func (i Instance) Source() string {
	if i.Src == "" {
		return SourceCuttlefish
	}
	return i.Src
}

// IsProvisioned reports whether this appliance creates and destroys the device
// itself. Provisioned devices have a start/stop lifecycle; the rest are simply
// reachable or not, and deleting one only deregisters it.
func (i Instance) IsProvisioned() bool {
	return i.Source() == SourceCuttlefish
}

type Operation struct {
	ID         string    `json:"id"`
	InstanceID string    `json:"instanceId,omitempty"`
	Action     string    `json:"action"`
	Status     string    `json:"status"`
	Message    string    `json:"message,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"displayName"`
	Role         string    `json:"role"`
	PasswordHash string    `json:"-"`
	Disabled     bool      `json:"disabled"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type Session struct {
	ID        string    `json:"id"`
	UserID    string    `json:"userId"`
	TokenHash string    `json:"-"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type Principal struct {
	UserID      string   `json:"userId"`
	Username    string   `json:"username"`
	DisplayName string   `json:"displayName"`
	Role        string   `json:"role"`
	Permissions []string `json:"permissions"`
}

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Principal Principal `json:"principal"`
	ExpiresAt  time.Time `json:"expiresAt"`
}

type BootstrapStatus struct {
	Required bool `json:"required"`
}

type BootstrapAdminRequest struct {
	Username    string `json:"username"`
	DisplayName string `json:"displayName,omitempty"`
	Password    string `json:"password"`
	Token       string `json:"token,omitempty"`
}

type IdentityProvider struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	IssuerURL    string    `json:"issuerUrl"`
	ClientID     string    `json:"clientId"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

type ExternalIdentity struct {
	ID         string    `json:"id"`
	UserID     string    `json:"userId"`
	ProviderID string    `json:"providerId"`
	Subject    string    `json:"subject"`
	CreatedAt  time.Time `json:"createdAt"`
}

type GroupRoleMapping struct {
	ID         string    `json:"id"`
	ProviderID string    `json:"providerId"`
	GroupName  string    `json:"groupName"`
	Role       string    `json:"role"`
	CreatedAt  time.Time `json:"createdAt"`
}

type AuditEvent struct {
	ID         string    `json:"id"`
	ActorID    string    `json:"actorId,omitempty"`
	ActorName  string    `json:"actorName,omitempty"`
	Action     string    `json:"action"`
	Resource   string    `json:"resource"`
	ResourceID string    `json:"resourceId,omitempty"`
	Outcome    string    `json:"outcome"`
	Message    string    `json:"message,omitempty"`
	SourceIP   string    `json:"sourceIp,omitempty"`
	UserAgent  string    `json:"userAgent,omitempty"`
	RequestID  string    `json:"requestId,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type HealthReport struct {
	Status       string         `json:"status"`
	Checks       []HealthCheck  `json:"checks"`
	GeneratedAt  time.Time      `json:"generatedAt"`
}

type HealthCheck struct {
	Name    string `json:"name"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type CreateImageRequest struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	AndroidAPI  string `json:"androidApi,omitempty"`
	Description string `json:"description,omitempty"`
}

// Test is a write-once natural-language device test: an ordered list of atomic
// steps ("tap X", "type Y into Z", "assert W is visible") grounded per run by
// the vision sidecar.
type Test struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Steps     []string  `json:"steps"`
	CreatedAt time.Time `json:"createdAt"`
}

// Step execution statuses (QMetry-compatible). Pass stays as a bool too for the
// legacy DSL runner + report renderer.
const (
	StepPass    = "pass"
	StepFail    = "fail"
	StepBlocked = "blocked"
)

// StepResult is the outcome of one executed test step, with its evidence.
type StepResult struct {
	Index      int    `json:"index"`
	Text       string `json:"text"`
	Verb       string `json:"verb"`
	Target     string `json:"target,omitempty"`
	Value      string `json:"value,omitempty"`
	X          int    `json:"x,omitempty"`
	Y          int    `json:"y,omitempty"`
	ModelOut   string `json:"modelOutput,omitempty"`
	Pass       bool   `json:"pass"`
	// Status is the QMetry-style result ("pass"/"fail"/"blocked"); set by the
	// agent-driven cycle executor. Empty for legacy DSL runs (use Pass instead).
	Status     string `json:"status,omitempty"`
	Detail     string `json:"detail,omitempty"`
	DurationMs int64  `json:"durationMs"`
	Screenshot string `json:"screenshot,omitempty"`
	Battery    int    `json:"battery,omitempty"`
}

// TestRun is one execution of a Test (or, in a cycle, a TestCase) against a
// device, with per-step results and artifact references (per-step screenshots +
// a session video).
type TestRun struct {
	ID         string       `json:"id"`
	TestID     string       `json:"testId"`
	TestName   string       `json:"testName,omitempty"`
	InstanceID string       `json:"instanceId"`
	Status     string       `json:"status"`
	Passed     bool         `json:"passed"`
	Steps      []StepResult `json:"steps"`
	Video      string       `json:"video,omitempty"`
	Error      string       `json:"error,omitempty"`
	StartedAt  time.Time    `json:"startedAt"`
	FinishedAt *time.Time   `json:"finishedAt,omitempty"`
	// Set when this run is a case executed as part of a cycle run.
	CycleRunID string `json:"cycleRunId,omitempty"`
	CaseID     string `json:"caseId,omitempty"`
}

type CreateTestRequest struct {
	Name  string   `json:"name"`
	Steps []string `json:"steps"`
}

// TestStep is one QMetry-style step of a test case: an action to perform, the
// input data, and the expected result to verify.
type TestStep struct {
	Index    int    `json:"index"`
	Action   string `json:"action"`
	TestData string `json:"testData,omitempty"`
	Expected string `json:"expected,omitempty"`
}

// TestCase is a reusable, QMetry-compatible test definition executed by the
// agent. Steps are free-form (action + expected) and interpreted per run.
type TestCase struct {
	ID           string     `json:"id"`
	Summary      string     `json:"summary"`
	Description  string     `json:"description,omitempty"`
	Precondition string     `json:"precondition,omitempty"`
	Priority     string     `json:"priority,omitempty"`
	Status       string     `json:"status,omitempty"`
	Labels       []string   `json:"labels"`
	Components   []string   `json:"components"`
	FolderPath   string     `json:"folderPath,omitempty"`
	Steps        []TestStep `json:"steps"`
	ExternalKey  string     `json:"externalKey,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	UpdatedAt    time.Time  `json:"updatedAt"`
}

// Cycle trigger sources.
const (
	CycleTriggerManual = "manual"
	CycleTriggerCron   = "cron"
	CycleTriggerBuild  = "build"
)

// TestCycle is a named, user-selected + ordered set of test cases targeting a
// platform, optionally bound to a build + environment, with a cron schedule and
// an on-new-build trigger.
type TestCycle struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Platform    string     `json:"platform"`
	BuildID     string     `json:"buildId,omitempty"`
	Environment string     `json:"environment,omitempty"`
	CaseIDs     []string   `json:"caseIds"`
	Cron        string     `json:"cron,omitempty"`
	// Timezone is the IANA zone (e.g. "America/New_York") the Cron's wall-clock
	// fields are interpreted in. Empty means UTC.
	Timezone    string     `json:"timezone,omitempty"`
	OnNewBuild  bool       `json:"onNewBuild"`
	Enabled     bool       `json:"enabled"`
	LastRunAt   *time.Time `json:"lastRunAt,omitempty"`
	NextRunAt   *time.Time `json:"nextRunAt,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
}

// CycleTotals is the per-status rollup of a cycle run's case results.
type CycleTotals struct {
	Cases   int `json:"cases"`
	Pass    int `json:"pass"`
	Fail    int `json:"fail"`
	Blocked int `json:"blocked"`
	NotRun  int `json:"notRun"`
}

// CycleRun is one execution of a test cycle: it fans out to a TestRun per case
// and aggregates their results.
type CycleRun struct {
	ID         string      `json:"id"`
	CycleID    string      `json:"cycleId"`
	CycleName  string      `json:"cycleName,omitempty"`
	Trigger    string      `json:"trigger"`
	BuildID    string      `json:"buildId,omitempty"`
	InstanceID string      `json:"instanceId,omitempty"`
	Status     string      `json:"status"`
	Totals     CycleTotals `json:"totals"`
	StartedAt  time.Time   `json:"startedAt"`
	FinishedAt *time.Time  `json:"finishedAt,omitempty"`
}

// Build is an uploaded app-under-test artifact (APK for Android, installer for a
// desktop) that Testral installs on the target before running a cycle.
type Build struct {
	ID        string    `json:"id"`
	Platform  string    `json:"platform"`
	Filename  string    `json:"filename"`
	Path      string    `json:"path"`
	SizeBytes int64     `json:"sizeBytes"`
	Version   string    `json:"version,omitempty"`
	Status    string    `json:"status"`
	Note      string    `json:"note,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	// SHA256 of the stored artifact, hex, computed while the upload streams to
	// disk. The runner verifies it before executing the file — without it, a
	// tampered or truncated artifact would simply be run.
	SHA256 string `json:"sha256,omitempty"`
}

type CreateInstanceRequest struct {
	Name           string `json:"name"`
	// Platform selects the device kind. Empty/"android" deploys a Cuttlefish VM;
	// windows/linux/macos onboard a desktop target reached over the dial-home
	// runner tunnel.
	Platform       string `json:"platform,omitempty"`
	ImageID        string `json:"imageId,omitempty"`
	AndroidVersion string `json:"androidVersion,omitempty"`
	CPUCores       int    `json:"cpuCores"`
	MemoryMB       int    `json:"memoryMb"`
	DisplayWidth   int    `json:"displayWidth,omitempty"`
	DisplayHeight  int    `json:"displayHeight,omitempty"`
	DPI            int    `json:"dpi,omitempty"`

	// Source selects how the device is reached. Empty keeps the existing
	// behavior (Cuttlefish for android, the runner tunnel for a desktop
	// platform); "physical" registers a real handset we only talk to.
	Source string `json:"source,omitempty"`
	// ADBTarget addresses a physical device: a USB serial, or host:port for
	// adb-over-TCP.
	ADBTarget string `json:"adbTarget,omitempty"`
}
