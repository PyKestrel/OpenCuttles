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

	RoleAdmin    = "admin"
	RoleOperator = "operator"
	RoleViewer   = "viewer"

	PermissionAdmin     = "admin"
	PermissionOperate   = "operate"
	PermissionView      = "view"
	PermissionOpenConsole = "console"
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
	CreatedAt   time.Time `json:"createdAt"`
}

type Instance struct {
	ID              string    `json:"id"`
	Name            string    `json:"name"`
	HostID          string    `json:"hostId"`
	ImageID         string    `json:"imageId"`
	State           string    `json:"state"`
	CPUCores        int       `json:"cpuCores"`
	MemoryMB        int       `json:"memoryMb"`
	ADBPort         int       `json:"adbPort"`
	WebRTCPort      int       `json:"webrtcPort"`
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

type CreateInstanceRequest struct {
	Name     string `json:"name"`
	ImageID  string `json:"imageId,omitempty"`
	CPUCores int    `json:"cpuCores"`
	MemoryMB int    `json:"memoryMb"`
}
