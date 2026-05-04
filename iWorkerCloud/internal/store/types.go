package store

import "time"

// Center represents a registered iWorkerCenter instance.
type Center struct {
	ID                        string    `json:"id"`
	MachineID                 string    `json:"machine_id"`
	CompanyID                 string    `json:"company_id"`
	CompanyName               string    `json:"company_name"`
	AdminEmail                string    `json:"admin_email"`
	AdminPhone                string    `json:"admin_phone"`
	Address                   string    `json:"address"`
	LegalPerson               string    `json:"legal_person"`
	BaseURL                   string    `json:"base_url"`
	SupportsMultiTenant       bool      `json:"-"`
	TenantCount               int       `json:"-"`
	CloudControlMode          string    `json:"cloud_control_mode"`
	LastSyncStatus            string    `json:"last_sync_status"`
	IWorkerReady              bool      `json:"iworker_ready"`
	IWorkerReadinessStatus    string    `json:"iworker_readiness_status"`
	IWorkerTenantCount        int       `json:"-"`
	IWorkerRoleCount          int       `json:"-"`
	IWorkerColleagueCount     int       `json:"-"`
	IWorkerLocalAccountCount  int       `json:"-"`
	IWorkerAgentInstanceCount int       `json:"iworker_agent_instance_count"`
	IWorkerReadinessJSON      string    `json:"-"`
	RuntimeStatusJSON         string    `json:"-"`
	Status                    string    `json:"status"` // pending, active, disabled
	SecretHash                string    `json:"-"`
	ManagementSecret          string    `json:"-"`
	LastHeartbeat             time.Time `json:"last_heartbeat"`
	CreatedAt                 time.Time `json:"created_at"`
	UpdatedAt                 time.Time `json:"updated_at"`
}

// License represents an authorization certificate for a Center.
type License struct {
	ID          string     `json:"id"`
	CenterID    string     `json:"center_id"`
	Modules     string     `json:"modules"` // JSON array of module names
	Type        string     `json:"type"`    // trial, manual
	ExpiresAt   time.Time  `json:"expires_at"`
	IsLongTerm  bool       `json:"is_long_term"` // 0 days = permanent
	Certificate string     `json:"certificate"`  // signed JSON
	CreatedAt   time.Time  `json:"created_at"`
	RevokedAt   *time.Time `json:"revoked_at,omitempty"`
}

// Skill represents a cloud skill market package managed by iWorkerCloud.
type Skill struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Category       string    `json:"category"`
	Version        string    `json:"version"`
	Tags           string    `json:"tags"`
	RiskLevel      string    `json:"risk_level"`
	Status         string    `json:"status"`
	Price          int64     `json:"price"`
	Author         string    `json:"author"`
	AuthorEmail    string    `json:"author_email"`
	SourceCenterID string    `json:"source_center_id"`
	AvgRating      float64   `json:"avg_rating"`
	DownloadCount  int       `json:"download_count"`
	PackageFormat  string    `json:"package_format"`
	PackageContent string    `json:"-"`
	PackageSHA256  string    `json:"package_sha256"`
	PackageSize    int64     `json:"package_size"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Admin represents a cloud admin account.
type Admin struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	CreatedAt    time.Time `json:"created_at"`
}

// SystemSetting is a key-value pair for system configuration.
type SystemSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}
