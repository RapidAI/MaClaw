package app

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	centercompute "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/compute"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
)

type ReadinessCheck struct {
	Name   string `json:"name"`
	Ready  bool   `json:"ready"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
	Count  int    `json:"count,omitempty"`
}

type AuthReadiness struct {
	Method      string `json:"method"`
	Label       string `json:"label"`
	Ready       bool   `json:"ready"`
	Implemented bool   `json:"implemented"`
	Status      string `json:"status"`
	Detail      string `json:"detail,omitempty"`
}

type IWorkerReadiness struct {
	Ready               bool             `json:"ready"`
	Status              string           `json:"status"`
	TenantCount         int              `json:"tenant_count"`
	RoleCount           int              `json:"role_count"`
	ColleagueCount      int              `json:"colleague_count"`
	LocalAccountCount   int              `json:"local_account_count"`
	AgentInstanceCount  int              `json:"agent_instance_count"`
	AgentRuntimeReady   bool             `json:"agent_runtime_ready"`
	GoalWatchReady      bool             `json:"goalwatch_ready"`
	RequiredClientPaths []string         `json:"required_client_paths"`
	Checks              []ReadinessCheck `json:"checks"`
	AuthMethods         []AuthReadiness  `json:"auth_methods"`
}

type RuntimeStatus struct {
	Status              string                           `json:"status"`
	RuntimeType         string                           `json:"runtime_type"`
	ProductKind         string                           `json:"product_kind"`
	AdminConsole        string                           `json:"admin_console"`
	ProviderCount       int                              `json:"provider_count"`
	ConfigPath          string                           `json:"config_path"`
	ComputeSource       string                           `json:"compute_source,omitempty"`
	ComputePermission   bool                             `json:"compute_permission"`
	ComputeSyncStatus   *centercompute.ComputeSyncStatus `json:"compute_sync_status,omitempty"`
	CloudProviderCount  int                              `json:"cloud_provider_count,omitempty"`
	RuntimeProviderMode string                           `json:"runtime_provider_mode,omitempty"`
	CloudHeartbeat      *tenant.CloudHeartbeatSnapshot   `json:"cloud_heartbeat,omitempty"`
	IWorkerReadiness    *IWorkerReadiness                `json:"iworker_readiness,omitempty"`
}

func (c *Center) RuntimeStatusSnapshot() RuntimeStatus {
	status := RuntimeStatus{
		Status:              "ok",
		RuntimeType:         "service",
		ProductKind:         "iworkercenter",
		AdminConsole:        "web_console",
		ProviderCount:       configuredProviderCount(),
		ConfigPath:          centerSettingsPath(),
		RuntimeProviderMode: "settings",
	}
	if c == nil {
		return status
	}
	if c.ComputeSourceManager != nil {
		status.ComputeSource = c.ComputeSourceManager.GetSource()
		providers, effectiveSource, fallback := c.ComputeSourceManager.GetActiveProviderSnapshot()
		switch effectiveSource {
		case "cloud":
			status.RuntimeProviderMode = "cloud_sync"
		case "local":
			status.RuntimeProviderMode = "local_self_managed"
		case "local_fallback":
			status.RuntimeProviderMode = "local_settings_fallback"
		}
		if fallback || effectiveSource == "local" || effectiveSource == "local_fallback" {
			status.ProviderCount = len(providers)
		}
	}
	if c.ComputeSyncManager != nil {
		syncStatus := c.ComputeSyncManager.GetSyncStatus()
		status.ComputeSyncStatus = &syncStatus
		status.ComputePermission = c.ComputeSyncManager.GetComputePermission()
		status.CloudProviderCount = len(c.ComputeSyncManager.GetProviders())
		if status.CloudProviderCount > 0 {
			status.ProviderCount = status.CloudProviderCount
		}
	}
	if c.CloudHeartbeatMonitor != nil {
		snapshot := c.CloudHeartbeatMonitor.Snapshot()
		status.CloudHeartbeat = &snapshot
	}
	readiness := c.IWorkerReadinessSnapshot()
	status.IWorkerReadiness = &readiness
	return status
}

func centerSettingsPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".iworkercenter", "settings.json")
}

func configuredProviderCount() int {
	path := centerSettingsPath()
	if path == "" {
		return 0
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	var cfg struct {
		Providers []any `json:"providers"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return 0
	}
	return len(cfg.Providers)
}

func (c *Center) IWorkerReadinessSnapshot() IWorkerReadiness {
	readiness := IWorkerReadiness{
		Status: "needs_bootstrap",
		RequiredClientPaths: []string{
			"/auth/tenants",
			"/client/roles",
			"/client/colleagues",
			"/client/goalwatch/pushes",
			"/client/iworker/instances",
			"/client/iworker/memory-stats",
			"/diworker-auth/methods",
			"/diworker-auth/enrollment/verify",
		},
	}
	if c == nil {
		readiness.Checks = []ReadinessCheck{{Name: "bootstrap", Ready: false, Status: "missing", Detail: "center is not initialized"}}
		readiness.AuthMethods = defaultAuthReadiness(0, false, false)
		return readiness
	}

	dbReady := c.DB != nil && c.DB.Read != nil
	if dbReady {
		readiness.TenantCount = countRows(c.DB.Read, `SELECT COUNT(*) FROM tenants WHERE status='active'`)
		readiness.RoleCount = countRows(c.DB.Read, `SELECT COUNT(*) FROM roles WHERE status='active'`)
		readiness.ColleagueCount = countRows(c.DB.Read, `SELECT COUNT(*) FROM colleagues WHERE status='active'`)
		readiness.LocalAccountCount = countRows(c.DB.Read, `SELECT COUNT(*) FROM diworker_accounts WHERE disabled=0`)
		readiness.AgentInstanceCount = countRows(c.DB.Read, `SELECT COUNT(*) FROM iworker_agent_instances WHERE status IN ('online','busy','idle','degraded')`)
	}
	ldapConfigured := dbReady && boolSettingEnabled(c.DB.Read, "diworker_ldap_config", "host")
	oidcConfigured := dbReady && boolSettingEnabled(c.DB.Read, "diworker_oidc_config", "issuer_url")
	readiness.AgentRuntimeReady = c.AgentRuntime != nil
	readiness.GoalWatchReady = c.GoalWatch != nil && c.GoalMonitor != nil
	readiness.AuthMethods = defaultAuthReadiness(readiness.LocalAccountCount, ldapConfigured, oidcConfigured)
	readiness.Checks = []ReadinessCheck{
		{Name: "database", Ready: dbReady, Status: readyStatus(dbReady, "ready", "missing")},
		{Name: "tenant", Ready: readiness.TenantCount > 0, Status: readyStatus(readiness.TenantCount > 0, "ready", "needs_setup"), Count: readiness.TenantCount},
		{Name: "roles", Ready: readiness.RoleCount > 0, Status: readyStatus(readiness.RoleCount > 0, "ready", "needs_roles"), Count: readiness.RoleCount},
		{Name: "iworkers", Ready: readiness.ColleagueCount > 0, Status: readyStatus(readiness.ColleagueCount > 0, "ready", "needs_iworkers"), Count: readiness.ColleagueCount},
		{Name: "agent_instances", Ready: readiness.AgentInstanceCount > 0, Status: readyStatus(readiness.AgentInstanceCount > 0, "ready", "waiting_for_heartbeat"), Count: readiness.AgentInstanceCount},
		{Name: "local_accounts", Ready: readiness.LocalAccountCount > 0, Status: readyStatus(readiness.LocalAccountCount > 0, "ready", "optional"), Count: readiness.LocalAccountCount},
		{Name: "agent_runtime", Ready: readiness.AgentRuntimeReady, Status: readyStatus(readiness.AgentRuntimeReady, "ready", "missing")},
		{Name: "goalwatch", Ready: readiness.GoalWatchReady, Status: readyStatus(readiness.GoalWatchReady, "ready", "missing")},
		{Name: "routes", Ready: c.Mux != nil, Status: readyStatus(c.Mux != nil, "ready", "missing"), Detail: strings.Join(readiness.RequiredClientPaths, ",")},
	}

	readiness.Ready = dbReady && readiness.TenantCount > 0 && readiness.RoleCount > 0 && readiness.ColleagueCount > 0 && readiness.AgentInstanceCount > 0 && readiness.AgentRuntimeReady && readiness.GoalWatchReady && c.Mux != nil
	if readiness.Ready {
		readiness.Status = "ready"
	}
	return readiness
}

func (c *Center) CloudRuntimeStatusSnapshot() *tenant.CloudCenterRuntime {
	status := c.RuntimeStatusSnapshot()
	runtime := &tenant.CloudCenterRuntime{
		ProviderCount:       status.ProviderCount,
		RuntimeProviderMode: status.RuntimeProviderMode,
		ComputeSource:       status.ComputeSource,
		ComputePermission:   status.ComputePermission,
		CloudProviderCount:  status.CloudProviderCount,
	}
	if status.ComputeSyncStatus != nil {
		runtime.ComputeSyncStatus = &tenant.CloudComputeSyncStatus{
			LastSyncAt:    status.ComputeSyncStatus.LastSyncAt,
			Status:        status.ComputeSyncStatus.Status,
			Error:         status.ComputeSyncStatus.Error,
			ProviderCount: status.ComputeSyncStatus.ProviderCount,
			NonBlocking:   status.ComputeSyncStatus.NonBlocking,
			RuntimeImpact: status.ComputeSyncStatus.RuntimeImpact,
		}
	}
	return runtime
}

func (c *Center) CloudIWorkerReadinessSnapshot() *tenant.CloudIWorkerReadiness {
	readiness := c.IWorkerReadinessSnapshot()
	var readDB *sql.DB
	if c != nil && c.DB != nil {
		readDB = c.DB.Read
	}
	return &tenant.CloudIWorkerReadiness{
		Ready:              readiness.Ready,
		Status:             readiness.Status,
		AgentInstanceCount: readiness.AgentInstanceCount,
		AgentRuntimeReady:  readiness.AgentRuntimeReady,
		GoalWatchReady:     readiness.GoalWatchReady,
		WorkloadSummary:    cloudWorkloadSummary(readDB),
	}
}

func cloudWorkloadSummary(db *sql.DB) *tenant.CloudWorkloadSummary {
	if db == nil {
		return nil
	}
	rows, err := db.Query(`SELECT work_status_json FROM iworker_agent_instances WHERE status IN ('online','busy','idle','degraded')`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	summary := &tenant.CloudWorkloadSummary{}
	for rows.Next() {
		var raw string
		if err := rows.Scan(&raw); err != nil {
			continue
		}
		summary.AgentInstanceCount++
		var work struct {
			ActiveCount    int    `json:"active_count"`
			CompletedCount int    `json:"completed_count"`
			ReviewCount    int    `json:"review_count"`
			BlockedCount   int    `json:"blocked_count"`
			UpdatedAt      string `json:"updated_at"`
		}
		if strings.TrimSpace(raw) == "" || json.Unmarshal([]byte(raw), &work) != nil {
			continue
		}
		summary.ActiveCount += work.ActiveCount
		summary.CompletedCount += work.CompletedCount
		summary.ReviewCount += work.ReviewCount
		summary.BlockedCount += work.BlockedCount
		if work.UpdatedAt > summary.UpdatedAt {
			summary.UpdatedAt = work.UpdatedAt
		}
	}
	return summary
}

func defaultAuthReadiness(localAccountCount int, ldapConfigured, oidcConfigured bool) []AuthReadiness {
	localReady := localAccountCount > 0
	return []AuthReadiness{
		{Method: "local", Label: "Local account", Ready: localReady, Implemented: true, Status: readyStatus(localReady, "ready", "needs_accounts"), Detail: "Manual or imported username/password accounts."},
		{Method: "ldap", Label: "LDAP", Ready: ldapConfigured, Implemented: true, Status: readyStatus(ldapConfigured, "ready", "not_configured"), Detail: "Enterprise directory bind authentication."},
		{Method: "oidc", Label: "OIDC / OAuth SSO", Ready: false, Implemented: false, Status: readyStatus(oidcConfigured, "reserved_configured", "reserved"), Detail: "Reserved adapter for zero-trust SSO providers."},
	}
}

func countRows(db *sql.DB, query string) int {
	if db == nil {
		return 0
	}
	var count int
	if err := db.QueryRow(query).Scan(&count); err != nil {
		return 0
	}
	return count
}

func boolSettingEnabled(db *sql.DB, key, requiredField string) bool {
	if db == nil {
		return false
	}
	var raw string
	if err := db.QueryRow(`SELECT value_json FROM system_settings WHERE key=?`, key).Scan(&raw); err != nil || strings.TrimSpace(raw) == "" {
		return false
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return false
	}
	enabled, _ := cfg["enabled"].(bool)
	required := strings.TrimSpace(requiredField)
	if required == "" {
		return enabled
	}
	value, _ := cfg[required].(string)
	return enabled && strings.TrimSpace(value) != ""
}

func readyStatus(ok bool, ready, notReady string) string {
	if ok {
		return ready
	}
	return notReady
}
