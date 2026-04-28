package agentruntime

import (
	"time"

	corel "github.com/RapidAI/CodeClaw/corelib"
)

type Instance struct {
	TenantID            string    `json:"tenant_id"`
	WorkerID            string    `json:"worker_id"`
	InstanceID          string    `json:"instance_id"`
	Role                string    `json:"role"`
	Status              string    `json:"status"`
	OrgUnitID           string    `json:"org_unit_id,omitempty"`
	Capabilities        []string  `json:"capabilities"`
	MemoryAuthority     string    `json:"memory_authority"`
	LocalCacheMode      string    `json:"local_cache_mode"`
	HostID              string    `json:"host_id,omitempty"`
	ProcessID           int       `json:"process_id,omitempty"`
	StartedAt           time.Time `json:"started_at"`
	LastHeartbeatAt     time.Time `json:"last_heartbeat_at"`
	HeartbeatAgeSeconds int64     `json:"heartbeat_age_seconds"`
	EffectiveStatus     string    `json:"effective_status"`
}

type HeartbeatRequest struct {
	WorkerID        string   `json:"worker_id"`
	InstanceID      string   `json:"instance_id"`
	Role            string   `json:"role"`
	Status          string   `json:"status"`
	OrgUnitID       string   `json:"org_unit_id"`
	Capabilities    []string `json:"capabilities"`
	MemoryAuthority string   `json:"memory_authority"`
	LocalCacheMode  string   `json:"local_cache_mode"`
	HostID          string   `json:"host_id"`
	ProcessID       int      `json:"process_id"`
	StartedAt       string   `json:"started_at"`
}

type HeartbeatResult struct {
	Instance          Instance       `json:"instance"`
	RuntimeSkills     []RuntimeSkill `json:"runtime_skills,omitempty"`
	RuntimeSkillError string         `json:"runtime_skill_error,omitempty"`
}

type RuntimeSkill struct {
	CapabilityID string             `json:"capability_id"`
	Name         string             `json:"name"`
	Source       string             `json:"source"`
	Version      string             `json:"version"`
	RiskLevel    string             `json:"risk_level"`
	Entry        corel.NLSkillEntry `json:"entry"`
}
