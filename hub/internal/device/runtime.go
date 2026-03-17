package device

import (
	"context"
	"errors"
	"log"
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
	ListAll(ctx context.Context) ([]*store.Machine, error)
	Delete(ctx context.Context, machineID string) error
	DeleteByUserID(ctx context.Context, userID string) (int64, error)
	ForceDeleteByUserID(ctx context.Context, userID string) (int64, error)
	DeleteOffline(ctx context.Context) (int64, error)
	UpdateMetadata(ctx context.Context, machineID string, metadata store.MachineMetadata) error
	UpdateStatus(ctx context.Context, machineID string, status string) error
	UpdateHeartbeat(ctx context.Context, machineID string, at time.Time) error
	UpdateAlias(ctx context.Context, machineID string, alias string) error
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
	Alias                string     `json:"alias,omitempty"`
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
	LLMConfigured        bool       `json:"llm_configured"`
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
	log.Printf("[device] BindDesktop: machine_id=%s user_id=%s conn=%v", machineID, safeConnUserID(ctx), ctx.Conn != nil)
	s.runtime.mu.Lock()
	prev := s.runtime.desktopsByMachine[machineID]
	if prev != nil {
		log.Printf("[device] BindDesktop: replacing existing connection for machine_id=%s (prev_user=%s)", machineID, safeConnUserID(prev))
	}
	s.runtime.desktopsByMachine[machineID] = ctx
	log.Printf("[device] BindDesktop: runtime now has %d machines in desktopsByMachine", len(s.runtime.desktopsByMachine))
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
	log.Printf("[device] UnbindDesktop: machine_id=%s user_id=%s", machineID, safeConnUserID(conn))
	s.runtime.mu.Lock()
	current := s.runtime.desktopsByMachine[machineID]
	if current == conn || conn == nil {
		delete(s.runtime.desktopsByMachine, machineID)
		log.Printf("[device] UnbindDesktop: removed machine_id=%s from runtime (match=%v, conn_nil=%v)", machineID, current == conn, conn == nil)
		s.appendEventLocked(MachineEvent{
			Timestamp: time.Now().Unix(),
			MachineID: machineID,
			UserID:    safeConnUserID(current),
			Type:      "unbind",
			Message:   "machine websocket unbound",
		})
	} else {
		log.Printf("[device] UnbindDesktop: skipped removal for machine_id=%s (conn mismatch: current=%p, provided=%p)", machineID, current, conn)
	}
	log.Printf("[device] UnbindDesktop: runtime now has %d machines in desktopsByMachine", len(s.runtime.desktopsByMachine))
	s.runtime.mu.Unlock()

	if s.repo == nil {
		return nil
	}
	log.Printf("[device] UnbindDesktop: updating DB status to 'offline' for machine_id=%s", machineID)
	return s.repo.UpdateStatus(ctx, machineID, "offline")
}

func (s *Service) MarkOnline(ctx context.Context, machineID string, hello ws.MachineHelloPayload) error {
	log.Printf("[device] MarkOnline: machine_id=%s name=%s platform=%s hostname=%s arch=%s version=%s heartbeat=%d",
		machineID, hello.Name, hello.Platform, hello.Hostname, hello.Arch, hello.AppVersion, hello.HeartbeatIntervalSec)
	now := time.Now()
	s.runtime.mu.Lock()
	conn := s.runtime.desktopsByMachine[machineID]
	if conn == nil {
		log.Printf("[device] MarkOnline WARNING: no connection in desktopsByMachine for machine_id=%s", machineID)
	} else {
		log.Printf("[device] MarkOnline: connection found for machine_id=%s user_id=%s conn_valid=%v", machineID, safeConnUserID(conn), conn.Conn != nil)
	}
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
	// Extract LLM configuration status from capabilities.
	if caps, ok := hello.Capabilities["llm_configured"]; ok {
		if v, ok := caps.(bool); ok {
			info.LLMConfigured = v
		}
	}
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
		log.Printf("[device] MarkOnline: no repo, skipping DB update for machine_id=%s", machineID)
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
		log.Printf("[device] MarkOnline ERROR: UpdateMetadata failed for machine_id=%s: %v", machineID, err)
		return err
	}
	if err := s.repo.UpdateStatus(ctx, machineID, "online"); err != nil {
		log.Printf("[device] MarkOnline ERROR: UpdateStatus failed for machine_id=%s: %v", machineID, err)
		return err
	}
	log.Printf("[device] MarkOnline: DB status set to 'online' for machine_id=%s", machineID)
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
	if heartbeat.LLMConfigured != nil {
		info.LLMConfigured = *heartbeat.LLMConfigured
	}
	lastAccepted := s.runtime.lastHeartbeatAt[machineID]
	shouldAccept := lastAccepted.IsZero() || now.Sub(lastAccepted) >= 5*time.Second
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

	log.Printf("[device] ListOnlineMachines: desktopsByMachine has %d entries", len(s.runtime.desktopsByMachine))
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
			info.LLMConfigured = meta.LLMConfigured
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
			Alias:                item.Alias,
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
			info.LLMConfigured = meta.LLMConfigured
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

func (s *Service) ListAllMachines(ctx context.Context) ([]MachineRuntimeInfo, error) {
	if s.repo == nil {
		return s.ListOnlineMachines(), nil
	}

	items, err := s.repo.ListAll(ctx)
	if err != nil {
		return nil, err
	}

	s.runtime.mu.RLock()
	defer s.runtime.mu.RUnlock()

	log.Printf("[device] ListAllMachines: DB returned %d machines, runtime has %d in desktopsByMachine", len(items), len(s.runtime.desktopsByMachine))
	for mid, conn := range s.runtime.desktopsByMachine {
		log.Printf("[device] ListAllMachines: runtime entry machine_id=%s conn_nil=%v ws_nil=%v user_id=%s", mid, conn == nil, conn != nil && conn.Conn == nil, safeConnUserID(conn))
	}

	out := make([]MachineRuntimeInfo, 0, len(items))
	for _, item := range items {
		info := MachineRuntimeInfo{
			MachineID:            item.ID,
			UserID:               item.UserID,
			Name:                 item.Name,
			Alias:                item.Alias,
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
			info.LLMConfigured = meta.LLMConfigured
			if meta.LastSeenAt != nil {
				info.LastSeenAt = meta.LastSeenAt
			}
		}
		if conn, ok := s.runtime.desktopsByMachine[item.ID]; ok && conn != nil && conn.Conn != nil {
			info.Role = conn.Role
			info.Online = true
			log.Printf("[device] ListAllMachines: machine_id=%s -> ONLINE (conn found, ws valid)", item.ID)
		} else {
			log.Printf("[device] ListAllMachines: machine_id=%s -> OFFLINE (in_map=%v, conn_nil=%v, ws_nil=%v, db_status=%s)",
				item.ID, ok, !ok || conn == nil, ok && conn != nil && conn.Conn == nil, item.Status)
		}
		out = append(out, info)
	}
	return out, nil
}

func (s *Service) DeleteMachine(ctx context.Context, machineID string) error {
	if s.repo == nil {
		return errors.New("no repository configured")
	}
	if s.IsMachineOnline(machineID) {
		return errors.New("cannot delete an online machine")
	}
	return s.repo.Delete(ctx, machineID)
}

func (s *Service) RenameMachine(ctx context.Context, machineID string, alias string) error {
	if s.repo == nil {
		return errors.New("no repository configured")
	}
	return s.repo.UpdateAlias(ctx, machineID, alias)
}

func (s *Service) ClearOfflineMachines(ctx context.Context) (int64, error) {
	if s.repo == nil {
		return 0, errors.New("no repository configured")
	}
	return s.repo.DeleteOffline(ctx)
}

func (s *Service) DeleteMachinesByUser(ctx context.Context, userID string) (int64, error) {
	if s.repo == nil {
		return 0, errors.New("no repository configured")
	}
	return s.repo.DeleteByUserID(ctx, userID)
}

func (s *Service) ForceDeleteMachinesByUser(ctx context.Context, userID string) (int64, error) {
	if s.repo == nil {
		return 0, errors.New("no repository configured")
	}

	// Delete from DB first so subsequent reads won't see these machines
	count, err := s.repo.ForceDeleteByUserID(ctx, userID)
	if err != nil {
		return 0, err
	}

	// Clean up runtime state and close WebSocket connections
	var connsToClose []*ws.ConnContext
	s.runtime.mu.Lock()
	for mid, conn := range s.runtime.desktopsByMachine {
		if conn != nil && conn.UserID == userID {
			connsToClose = append(connsToClose, conn)
			delete(s.runtime.desktopsByMachine, mid)
			delete(s.runtime.metadataByMachine, mid)
			delete(s.runtime.lastHeartbeatAt, mid)
		}
	}
	s.runtime.mu.Unlock()

	// Close WebSocket connections outside the lock to prevent reconnect/re-register
	for _, conn := range connsToClose {
		if conn.Conn != nil {
			_ = conn.Conn.Close()
		}
	}

	return count, nil
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
		return "MaClaw Desktop"
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
	if v < 5 {
		return 5
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
