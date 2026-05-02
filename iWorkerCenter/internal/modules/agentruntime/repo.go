package agentruntime

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type Repo struct {
	write *sql.DB
	read  *sql.DB
}

func NewRepo(write, read *sql.DB) *Repo {
	return &Repo{write: write, read: read}
}

func (r *Repo) UpsertHeartbeat(tenantID string, instance Instance) (Instance, error) {
	if r == nil || r.write == nil {
		return Instance{}, fmt.Errorf("agent runtime repo is unavailable")
	}
	tenantID = normalizeTenantID(tenantID)
	instance.TenantID = tenantID
	instance.WorkerID = strings.TrimSpace(instance.WorkerID)
	instance.InstanceID = strings.TrimSpace(instance.InstanceID)
	instance.Role = normalizeRole(instance.Role)
	instance.Status = normalizeStatus(instance.Status)
	instance.OrgUnitID = strings.TrimSpace(instance.OrgUnitID)
	instance.MemoryAuthority = firstNonEmpty(strings.TrimSpace(instance.MemoryAuthority), "iWorkerCenter")
	instance.LocalCacheMode = firstNonEmpty(strings.TrimSpace(instance.LocalCacheMode), "cache_only")
	instance.HostID = strings.TrimSpace(instance.HostID)
	if instance.WorkerID == "" {
		return Instance{}, fmt.Errorf("worker_id is required")
	}
	if instance.InstanceID == "" {
		instance.InstanceID = instance.WorkerID + ":" + instance.Role
	}
	if instance.StartedAt.IsZero() {
		instance.StartedAt = time.Now().UTC()
	}
	if instance.LastHeartbeatAt.IsZero() {
		instance.LastHeartbeatAt = time.Now().UTC()
	}
	capabilities, _ := json.Marshal(normalizeStringSlice(instance.Capabilities))
	workStatus := normalizeWorkStatus(instance.WorkStatus)
	workStatusJSON := ""
	if workStatus != nil {
		data, _ := json.Marshal(workStatus)
		workStatusJSON = string(data)
	}
	_, err := r.write.Exec(`INSERT INTO iworker_agent_instances
		(tenant_id, worker_id, instance_id, role, status, org_unit_id, capabilities_json, memory_authority, local_cache_mode, work_status_json, host_id, process_id, started_at, last_heartbeat_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(tenant_id, instance_id) DO UPDATE SET
			worker_id=excluded.worker_id,
			role=excluded.role,
			status=excluded.status,
			org_unit_id=excluded.org_unit_id,
			capabilities_json=excluded.capabilities_json,
			memory_authority=excluded.memory_authority,
			local_cache_mode=excluded.local_cache_mode,
			work_status_json=excluded.work_status_json,
			host_id=excluded.host_id,
			process_id=excluded.process_id,
			started_at=excluded.started_at,
			last_heartbeat_at=excluded.last_heartbeat_at,
			updated_at=excluded.updated_at`,
		tenantID, instance.WorkerID, instance.InstanceID, instance.Role, instance.Status, instance.OrgUnitID,
		string(capabilities), instance.MemoryAuthority, instance.LocalCacheMode, workStatusJSON, instance.HostID, instance.ProcessID,
		instance.StartedAt.Format(time.RFC3339), instance.LastHeartbeatAt.Format(time.RFC3339), instance.LastHeartbeatAt.Format(time.RFC3339))
	if err != nil {
		return Instance{}, err
	}
	return r.Get(tenantID, instance.InstanceID)
}

func (r *Repo) Get(tenantID, instanceID string) (Instance, error) {
	row := r.read.QueryRow(`SELECT tenant_id, worker_id, instance_id, role, status, org_unit_id, capabilities_json, memory_authority, local_cache_mode, work_status_json, host_id, process_id, started_at, last_heartbeat_at
		FROM iworker_agent_instances WHERE tenant_id=? AND instance_id=?`, normalizeTenantID(tenantID), strings.TrimSpace(instanceID))
	return scanInstance(row)
}

func (r *Repo) List(tenantID, workerID string) ([]Instance, error) {
	tenantID = normalizeTenantID(tenantID)
	workerID = strings.TrimSpace(workerID)
	var rows *sql.Rows
	var err error
	if workerID == "" {
		rows, err = r.read.Query(`SELECT tenant_id, worker_id, instance_id, role, status, org_unit_id, capabilities_json, memory_authority, local_cache_mode, work_status_json, host_id, process_id, started_at, last_heartbeat_at
			FROM iworker_agent_instances WHERE tenant_id=? ORDER BY worker_id, role`, tenantID)
	} else {
		rows, err = r.read.Query(`SELECT tenant_id, worker_id, instance_id, role, status, org_unit_id, capabilities_json, memory_authority, local_cache_mode, work_status_json, host_id, process_id, started_at, last_heartbeat_at
			FROM iworker_agent_instances WHERE tenant_id=? AND worker_id=? ORDER BY role`, tenantID, workerID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []Instance{}
	for rows.Next() {
		item, err := scanInstanceRows(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

type scanner interface {
	Scan(dest ...any) error
}

func scanInstance(row scanner) (Instance, error) {
	var item Instance
	var caps, workStatusJSON, started, heartbeat string
	if err := row.Scan(&item.TenantID, &item.WorkerID, &item.InstanceID, &item.Role, &item.Status, &item.OrgUnitID, &caps, &item.MemoryAuthority, &item.LocalCacheMode, &workStatusJSON, &item.HostID, &item.ProcessID, &started, &heartbeat); err != nil {
		return Instance{}, err
	}
	_ = json.Unmarshal([]byte(caps), &item.Capabilities)
	if strings.TrimSpace(workStatusJSON) != "" {
		var workStatus WorkStatusSummary
		if err := json.Unmarshal([]byte(workStatusJSON), &workStatus); err == nil {
			item.WorkStatus = &workStatus
		}
	}
	item.StartedAt, _ = time.Parse(time.RFC3339, started)
	item.LastHeartbeatAt, _ = time.Parse(time.RFC3339, heartbeat)
	return item, nil
}

func scanInstanceRows(rows *sql.Rows) (Instance, error) { return scanInstance(rows) }

func normalizeTenantID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "default"
	}
	return value
}

func normalizeRole(value string) string {
	switch strings.TrimSpace(value) {
	case "interaction", "executor", "watcher":
		return strings.TrimSpace(value)
	default:
		return "executor"
	}
}

func normalizeStatus(value string) string {
	switch strings.TrimSpace(value) {
	case "online", "busy", "idle", "degraded", "offline":
		return strings.TrimSpace(value)
	default:
		return "online"
	}
}

func normalizeWorkStatus(status *WorkStatusSummary) *WorkStatusSummary {
	if status == nil {
		return nil
	}
	next := *status
	next.CurrentTask = strings.TrimSpace(next.CurrentTask)
	next.CurrentDetail = strings.TrimSpace(next.CurrentDetail)
	next.UpdatedAt = strings.TrimSpace(next.UpdatedAt)
	return &next
}

func normalizeStringSlice(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
