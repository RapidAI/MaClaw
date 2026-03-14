package device

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/store"
	"github.com/RapidAI/CodeClaw/hub/internal/ws"
)

var ErrMachineOffline = errors.New("machine is offline")

type MachineRepository interface {
	GetByID(ctx context.Context, id string) (*store.Machine, error)
	ListByUserID(ctx context.Context, userID string) ([]*store.Machine, error)
	UpdateMetadata(ctx context.Context, machineID string, metadata store.MachineMetadata) error
	UpdateStatus(ctx context.Context, machineID string, status string) error
	UpdateHeartbeat(ctx context.Context, machineID string, at time.Time) error
}

type Runtime struct {
	mu sync.RWMutex

	desktopsByMachine map[string]*ws.ConnContext
	metadataByMachine map[string]MachineRuntimeInfo
	lastHeartbeatAt   map[string]time.Time
	events            []MachineEvent
}

type Service struct {
	repo    MachineRepository
	runtime *Runtime
}

func (s *Service) IsMachineOnline(machineID string) bool {
	s.runtime.mu.RLock()
	defer s.runtime.mu.RUnlock()
	conn := s.runtime.desktopsByMachine[machineID]
	return conn != nil && conn.Conn != nil
}

type MachineRuntimeInfo struct {
	MachineID            string     `json:"machine_id"`
	UserID               string     `json:"user_id,omitempty"`
	Name                 string     `json:"name,omitempty"`
	Platform             string     `json:"platform,omitempty"`
	Hostname             string     `json:"hostname,omitempty"`
	Arch                 string     `json:"arch,omitempty"`
	AppVersion           string     `json:"app_version,omitempty"`
	HeartbeatIntervalSec int        `json:"heartbeat_interval_sec,omitempty"`
	ActiveSessions       int        `json:"active_sessions,omitempty"`
	Status               string     `json:"status,omitempty"`
	LastSeenAt           *time.Time `json:"last_seen_at,omitempty"`
	Role                 string     `json:"role,omitempty"`
	Online               bool       `json:"online"`
}

type MachineEvent struct {
	Timestamp int64  `json:"timestamp"`
	MachineID string `json:"machine_id"`
	UserID    string `json:"user_id,omitempty"`
	Type      string `json:"type"`
	Message   string `json:"message,omitempty"`
}

func NewRuntime() *Runtime {
	return &Runtime{
		desktopsByMachine: map[string]*ws.ConnContext{},
		metadataByMachine: map[string]MachineRuntimeInfo{},
		lastHeartbeatAt:   map[string]time.Time{},
		events:            make([]MachineEvent, 0, 128),
	}
}

func NewService(repo MachineRepository, runtime *Runtime) *Service {
	return &Service{repo: repo, runtime: runtime}
}

func (s *Service) BindDesktop(machineID string, ctx *ws.ConnContext) {
	s.runtime.mu.Lock()
	s.runtime.desktopsByMachine[machineID] = ctx
	s.appendEventLocked(MachineEvent{
		Timestamp: time.Now().Unix(),
		MachineID: machineID,
		UserID:    safeConnUserID(ctx),
		Type:      "bind",
		Message:   "machine websocket bound",
	})
	s.runtime.mu.Unlock()
}

func (s *Service) UnbindDesktop(ctx context.Context, machineID string, conn *ws.ConnContext) error {
	s.runtime.mu.Lock()
	current := s.runtime.desktopsByMachine[machineID]
	if current == conn || conn == nil {
		delete(s.runtime.desktopsByMachine, machineID)
		s.appendEventLocked(MachineEvent{
			Timestamp: time.Now().Unix(),
			MachineID: machineID,
			UserID:    safeConnUserID(current),
			Type:      "unbind",
			Message:   "machine websocket unbound",
		})
	}
	s.runtime.mu.Unlock()

	if s.repo == nil {
		return nil
	}
	return s.repo.UpdateStatus(ctx, machineID, "offline")
}

func (s *Service) MarkOnline(ctx context.Context, machineID string, hello ws.MachineHelloPayload) error {
	now := time.Now()
	s.runtime.mu.Lock()
	conn := s.runtime.desktopsByMachine[machineID]
	info := s.runtime.metadataByMachine[machineID]
	info.MachineID = machineID
	info.Name = defaultMachineName(hello.Name)
	info.Platform = defaultMachinePlatform(hello.Platform)
	info.Hostname = strings.TrimSpace(hello.Hostname)
	info.Arch = strings.TrimSpace(hello.Arch)
	info.AppVersion = strings.TrimSpace(hello.AppVersion)
	info.HeartbeatIntervalSec = normalizeHeartbeatInterval(hello.HeartbeatIntervalSec)
	info.Online = true
	info.Status = "online"
	info.LastSeenAt = &now
	s.runtime.metadataByMachine[machineID] = info
	s.runtime.lastHeartbeatAt[machineID] = now
	s.appendEventLocked(MachineEvent{
		Timestamp: now.Unix(),
		MachineID: machineID,
		UserID:    safeConnUserID(conn),
		Type:      "online",
		Message:   "machine marked online",
	})
	s.runtime.mu.Unlock()

	if s.repo == nil {
		return nil
	}
	if err := s.repo.UpdateMetadata(ctx, machineID, store.MachineMetadata{
		Name:                 info.Name,
		Platform:             info.Platform,
		Hostname:             info.Hostname,
		Arch:                 info.Arch,
		AppVersion:           info.AppVersion,
		HeartbeatIntervalSec: info.HeartbeatIntervalSec,
	}); err != nil {
		return err
	}
	if err := s.repo.UpdateStatus(ctx, machineID, "online"); err != nil {
		return err
	}
	return s.repo.UpdateHeartbeat(ctx, machineID, now)
}

func (s *Service) Heartbeat(ctx context.Context, machineID string, heartbeat ws.MachineHeartbeatPayload) error {
	now := time.Now()
	s.runtime.mu.Lock()
	conn := s.runtime.desktopsByMachine[machineID]
	info := s.runtime.metadataByMachine[machineID]
	info.MachineID = machineID
	info.ActiveSessions = heartbeat.ActiveSessions
	if strings.TrimSpace(heartbeat.AppVersion) != "" {
		info.AppVersion = strings.TrimSpace(heartbeat.AppVersion)
	}
	if heartbeat.HeartbeatIntervalSec > 0 {
		info.HeartbeatIntervalSec = normalizeHeartbeatInterval(heartbeat.HeartbeatIntervalSec)
	}
	info.Online = true
	info.Status = "online"
	lastAccepted := s.runtime.lastHeartbeatAt[machineID]
	shouldAccept := lastAccepted.IsZero() || now.Sub(lastAccepted) >= 30*time.Second
	if shouldAccept {
		info.LastSeenAt = &now
		s.runtime.lastHeartbeatAt[machineID] = now
	}
	s.runtime.metadataByMachine[machineID] = info
	s.appendEventLocked(MachineEvent{
		Timestamp: now.Unix(),
		MachineID: machineID,
		UserID:    safeConnUserID(conn),
		Type:      heartbeatEventType(shouldAccept),
		Message:   heartbeatEventMessage(shouldAccept),
	})
	s.runtime.mu.Unlock()

	if s.repo == nil {
		return nil
	}
	if err := s.repo.UpdateMetadata(ctx, machineID, store.MachineMetadata{
		Name:                 defaultMachineName(info.Name),
		Platform:             defaultMachinePlatform(info.Platform),
		Hostname:             info.Hostname,
		Arch:                 info.Arch,
		AppVersion:           info.AppVersion,
		HeartbeatIntervalSec: info.HeartbeatIntervalSec,
	}); err != nil {
		return err
	}
	if !shouldAccept {
		return nil
	}
	return s.repo.UpdateHeartbeat(ctx, machineID, now)
}

func (s *Service) SendToMachine(machineID string, msg any) error {
	s.runtime.mu.RLock()
	conn := s.runtime.desktopsByMachine[machineID]
	s.runtime.mu.RUnlock()
	if conn == nil || conn.Conn == nil {
		s.recordEvent(MachineEvent{
			Timestamp: time.Now().Unix(),
			MachineID: machineID,
			Type:      "send.failed",
			Message:   "machine offline during command dispatch",
		})
		return ErrMachineOffline
	}
	s.recordEvent(MachineEvent{
		Timestamp: time.Now().Unix(),
		MachineID: machineID,
		UserID:    safeConnUserID(conn),
		Type:      "send",
		Message:   "command dispatched to machine",
	})
	return conn.Conn.WriteJSON(msg)
}

func (s *Service) ListOnlineMachines() []MachineRuntimeInfo {
	s.runtime.mu.RLock()
	defer s.runtime.mu.RUnlock()

	out := make([]MachineRuntimeInfo, 0, len(s.runtime.desktopsByMachine))
	for machineID, conn := range s.runtime.desktopsByMachine {
		info := MachineRuntimeInfo{
			MachineID: machineID,
			Online:    true,
		}
		if meta, ok := s.runtime.metadataByMachine[machineID]; ok {
			info.Name = meta.Name
			info.Platform = meta.Platform
			info.Hostname = meta.Hostname
			info.Arch = meta.Arch
			info.AppVersion = meta.AppVersion
			info.HeartbeatIntervalSec = meta.HeartbeatIntervalSec
			info.ActiveSessions = meta.ActiveSessions
			info.LastSeenAt = meta.LastSeenAt
			info.Status = meta.Status
		}
		if conn != nil {
			info.UserID = conn.UserID
			info.Role = conn.Role
		}
		out = append(out, info)
	}
	return out
}

func (s *Service) ListMachines(ctx context.Context, userID string) ([]MachineRuntimeInfo, error) {
	if s.repo == nil {
		return s.ListOnlineMachines(), nil
	}

	items, err := s.repo.ListByUserID(ctx, userID)
	if err != nil {
		return nil, err
	}

	s.runtime.mu.RLock()
	defer s.runtime.mu.RUnlock()

	out := make([]MachineRuntimeInfo, 0, len(items))
	for _, item := range items {
		info := MachineRuntimeInfo{
			MachineID:            item.ID,
			UserID:               item.UserID,
			Name:                 item.Name,
			Platform:             item.Platform,
			Hostname:             item.Hostname,
			Arch:                 item.Arch,
			AppVersion:           item.AppVersion,
			HeartbeatIntervalSec: item.HeartbeatSec,
			Status:               item.Status,
			LastSeenAt:           item.LastSeenAt,
		}
		if meta, ok := s.runtime.metadataByMachine[item.ID]; ok {
			if info.Name == "" {
				info.Name = meta.Name
			}
			if info.Platform == "" {
				info.Platform = meta.Platform
			}
			if info.Hostname == "" {
				info.Hostname = meta.Hostname
			}
			if info.Arch == "" {
				info.Arch = meta.Arch
			}
			if info.AppVersion == "" {
				info.AppVersion = meta.AppVersion
			}
			if info.HeartbeatIntervalSec == 0 {
				info.HeartbeatIntervalSec = meta.HeartbeatIntervalSec
			}
			info.ActiveSessions = meta.ActiveSessions
			if meta.LastSeenAt != nil {
				info.LastSeenAt = meta.LastSeenAt
			}
		}
		if conn, ok := s.runtime.desktopsByMachine[item.ID]; ok && conn != nil && conn.Conn != nil {
			info.Role = conn.Role
			info.Online = true
		}
		out = append(out, info)
	}
	return out, nil
}

func (s *Service) ListEvents(limit int) []MachineEvent {
	s.runtime.mu.RLock()
	defer s.runtime.mu.RUnlock()

	if limit <= 0 || limit > len(s.runtime.events) {
		limit = len(s.runtime.events)
	}
	start := len(s.runtime.events) - limit
	if start < 0 {
		start = 0
	}

	out := make([]MachineEvent, 0, len(s.runtime.events)-start)
	for i := len(s.runtime.events) - 1; i >= start; i-- {
		out = append(out, s.runtime.events[i])
	}
	return out
}

func (s *Service) recordEvent(event MachineEvent) {
	s.runtime.mu.Lock()
	defer s.runtime.mu.Unlock()
	s.appendEventLocked(event)
}

func (s *Service) appendEventLocked(event MachineEvent) {
	s.runtime.events = append(s.runtime.events, event)
	if len(s.runtime.events) > 200 {
		s.runtime.events = s.runtime.events[len(s.runtime.events)-200:]
	}
}

func safeConnUserID(ctx *ws.ConnContext) string {
	if ctx == nil {
		return ""
	}
	return ctx.UserID
}

func defaultMachineName(v string) string {
	if strings.TrimSpace(v) == "" {
		return "CodeClaw Desktop"
	}
	return strings.TrimSpace(v)
}

func defaultMachinePlatform(v string) string {
	if strings.TrimSpace(v) == "" {
		return "unknown"
	}
	return strings.TrimSpace(v)
}

func normalizeHeartbeatInterval(v int) int {
	if v < 30 {
		return 60
	}
	return v
}

func heartbeatEventType(accepted bool) string {
	if accepted {
		return "heartbeat"
	}
	return "heartbeat.ignored"
}

func heartbeatEventMessage(accepted bool) string {
	if accepted {
		return "machine heartbeat received"
	}
	return "machine heartbeat ignored because it arrived too quickly"
}
