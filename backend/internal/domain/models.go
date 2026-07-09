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

	ConsoleProviderCuttlefishWebRTC = "cuttlefish-webrtc"

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

// StepResult is the outcome of one executed test step, with its evidence.
type StepResult struct {
	Index      int     `json:"index"`
	Text       string  `json:"text"`
	Verb       string  `json:"verb"`
	Target     string  `json:"target,omitempty"`
	Value      string  `json:"value,omitempty"`
	X          int     `json:"x,omitempty"`
	Y          int     `json:"y,omitempty"`
	ModelOut   string  `json:"modelOutput,omitempty"`
	Pass       bool    `json:"pass"`
	Detail     string  `json:"detail,omitempty"`
	DurationMs int64   `json:"durationMs"`
	Screenshot string  `json:"screenshot,omitempty"`
	Battery    int     `json:"battery,omitempty"`
}

// TestRun is one execution of a Test against a device, with per-step results
// and artifact references (per-step screenshots + a session video).
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
}

type CreateTestRequest struct {
	Name  string   `json:"name"`
	Steps []string `json:"steps"`
}

type CreateInstanceRequest struct {
	Name           string `json:"name"`
	ImageID        string `json:"imageId,omitempty"`
	AndroidVersion string `json:"androidVersion,omitempty"`
	CPUCores       int    `json:"cpuCores"`
	MemoryMB       int    `json:"memoryMb"`
	DisplayWidth   int    `json:"displayWidth,omitempty"`
	DisplayHeight  int    `json:"displayHeight,omitempty"`
	DPI            int    `json:"dpi,omitempty"`
}
