package main

import (
	"strings"
	"time"
)

type AgentInstanceRole string

const (
	AgentRoleInteraction AgentInstanceRole = "interaction"
	AgentRoleExecutor    AgentInstanceRole = "executor"
	AgentRoleWatcher     AgentInstanceRole = "watcher"
)

type AgentInstanceStatus string

const (
	AgentStatusOnline   AgentInstanceStatus = "online"
	AgentStatusDegraded AgentInstanceStatus = "degraded"
)

type AgentInstance struct {
	ID              string              `json:"id"`
	WorkerID        string              `json:"worker_id"`
	Role            AgentInstanceRole   `json:"role"`
	Status          AgentInstanceStatus `json:"status"`
	Capabilities    []string            `json:"capabilities"`
	StartedAt       string              `json:"started_at"`
	LastHeartbeatAt string              `json:"last_heartbeat_at"`
}

type AgentRuntimeSnapshot struct {
	WorkerID            string          `json:"worker_id"`
	TenantID            string          `json:"tenant_id"`
	OrgUnitID           string          `json:"org_unit_id"`
	CenterRegistered    bool            `json:"center_registered"`
	MemoryAuthority     string          `json:"memory_authority"`
	LocalMemoryBehavior string          `json:"local_memory_behavior"`
	ParallelModel       string          `json:"parallel_model"`
	Instances           []AgentInstance `json:"instances"`
}

func (a *App) GetAgentRuntimeSnapshot() AgentRuntimeSnapshot {
	settings, err := readDiWorkerSettings()
	if err != nil {
		settings = defaultDiWorkerSettings()
	}
	return BuildDefaultAgentRuntimeSnapshot(settings)
}

func BuildDefaultAgentRuntimeSnapshot(settings DiWorkerSettings) AgentRuntimeSnapshot {
	return buildAgentRuntimeSnapshot(normalizeDiWorkerSettings(settings), time.Now().UTC())
}

func buildAgentRuntimeSnapshot(settings DiWorkerSettings, now time.Time) AgentRuntimeSnapshot {
	workerID := resolvedWorkerID(settings)
	tenantID := resolvedTenantID(settings)
	orgUnitID := resolvedDepartmentID(settings)
	status := AgentStatusOnline
	memoryAuthority := "iWorkerCenter"
	if !settings.Center.Enabled {
		status = AgentStatusDegraded
		memoryAuthority = "unregistered-iworkercenter"
	}

	stamp := now.Format(time.RFC3339)
	instances := []AgentInstance{
		{
			ID:              agentInstanceID(workerID, AgentRoleInteraction),
			WorkerID:        workerID,
			Role:            AgentRoleInteraction,
			Status:          status,
			Capabilities:    []string{"human_im", "voice_dialogue", "clarification", "notification", "goal_push_ack"},
			StartedAt:       stamp,
			LastHeartbeatAt: stamp,
		},
		{
			ID:              agentInstanceID(workerID, AgentRoleExecutor),
			WorkerID:        workerID,
			Role:            AgentRoleExecutor,
			Status:          status,
			Capabilities:    []string{"business_task_execution", "tool_use", "workflow_step", "memory_writeback"},
			StartedAt:       stamp,
			LastHeartbeatAt: stamp,
		},
		{
			ID:              agentInstanceID(workerID, AgentRoleWatcher),
			WorkerID:        workerID,
			Role:            AgentRoleWatcher,
			Status:          status,
			Capabilities:    []string{"goalwatch_polling", "heartbeat", "local_cache_refresh"},
			StartedAt:       stamp,
			LastHeartbeatAt: stamp,
		},
	}

	return AgentRuntimeSnapshot{
		WorkerID:            workerID,
		TenantID:            tenantID,
		OrgUnitID:           orgUnitID,
		CenterRegistered:    settings.Center.Enabled,
		MemoryAuthority:     memoryAuthority,
		LocalMemoryBehavior: "cache_only",
		ParallelModel:       "one_logical_iworker_multiple_agent_instances_shared_center_memory",
		Instances:           instances,
	}
}

func agentInstanceID(workerID string, role AgentInstanceRole) string {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		workerID = "local-iworker"
	}
	return workerID + ":" + string(role)
}
