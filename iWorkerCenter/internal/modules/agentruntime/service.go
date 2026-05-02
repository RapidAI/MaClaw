package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const DefaultOfflineAfter = 90 * time.Second

type Service struct {
	repo                 *Repo
	runtimeSkillProvider RuntimeSkillProvider
}

func NewService(repo *Repo) *Service { return &Service{repo: repo} }

type RuntimeSkillProvider interface {
	RuntimeSkillsForWorker(ctx context.Context, tenantID, workerID string) ([]RuntimeSkill, error)
}

func (s *Service) SetRuntimeSkillProvider(provider RuntimeSkillProvider) {
	if s != nil {
		s.runtimeSkillProvider = provider
	}
}

func (s *Service) Heartbeat(tenantID string, req HeartbeatRequest, now time.Time) (HeartbeatResult, error) {
	if s == nil || s.repo == nil {
		return HeartbeatResult{}, fmt.Errorf("agent runtime service is unavailable")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	startedAt := now
	if strings.TrimSpace(req.StartedAt) != "" {
		if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(req.StartedAt)); err == nil {
			startedAt = parsed
		}
	}
	item, err := s.repo.UpsertHeartbeat(tenantID, Instance{
		WorkerID:        req.WorkerID,
		InstanceID:      req.InstanceID,
		Role:            req.Role,
		Status:          req.Status,
		OrgUnitID:       req.OrgUnitID,
		Capabilities:    req.Capabilities,
		MemoryAuthority: req.MemoryAuthority,
		LocalCacheMode:  req.LocalCacheMode,
		WorkStatus:      req.WorkStatus,
		HostID:          req.HostID,
		ProcessID:       req.ProcessID,
		StartedAt:       startedAt,
		LastHeartbeatAt: now,
	})
	if err != nil {
		return HeartbeatResult{}, err
	}
	result := HeartbeatResult{Instance: applyHealth(item, now, DefaultOfflineAfter)}
	if s.runtimeSkillProvider != nil {
		skills, err := s.runtimeSkillProvider.RuntimeSkillsForWorker(context.Background(), tenantID, item.WorkerID)
		if err != nil {
			result.RuntimeSkillError = err.Error()
		} else {
			result.RuntimeSkills = skills
		}
	}
	return result, nil
}

func (s *Service) List(tenantID, workerID string) ([]Instance, error) {
	return s.ListWithHealth(tenantID, workerID, time.Now().UTC(), DefaultOfflineAfter)
}

func (s *Service) ListWithHealth(tenantID, workerID string, now time.Time, offlineAfter time.Duration) ([]Instance, error) {
	if s == nil || s.repo == nil {
		return nil, fmt.Errorf("agent runtime service is unavailable")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if offlineAfter <= 0 {
		offlineAfter = DefaultOfflineAfter
	}
	items, err := s.repo.List(tenantID, workerID)
	if err != nil {
		return nil, err
	}
	for i := range items {
		items[i] = applyHealth(items[i], now, offlineAfter)
	}
	return items, nil
}

func applyHealth(item Instance, now time.Time, offlineAfter time.Duration) Instance {
	if item.LastHeartbeatAt.IsZero() {
		item.EffectiveStatus = "offline"
		return item
	}
	age := now.Sub(item.LastHeartbeatAt)
	if age < 0 {
		age = 0
	}
	item.HeartbeatAgeSeconds = int64(age.Seconds())
	item.EffectiveStatus = item.Status
	if age > offlineAfter {
		item.EffectiveStatus = "offline"
	}
	if item.EffectiveStatus == "" {
		item.EffectiveStatus = normalizeStatus(item.Status)
	}
	return item
}
