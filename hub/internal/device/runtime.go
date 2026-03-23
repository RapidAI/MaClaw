package device

import (
	"context"
	"errors"
	"fmt"
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

	// OnMultiDeviceOnline is called (in a goroutine) when a user's second
	// device comes online. The callback receives the userID and a list of
	// all online machine display names. Used to push IM usage guidance.
	OnMultiDeviceOnline func(userID string, machineNames []string)
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
	// Apply nickname from hello payload (persisted on client side).
	var assignedNickname string
	if nn := strings.TrimSpace(hello.Nickname); nn != "" {
		// Client reported a nickname — check for Alias conflict with
		// same-user online machines before accepting it.
		aliasConflict := false
		if conn != nil {
			for otherID, otherConn := range s.runtime.desktopsByMachine {
				if otherID == machineID || otherConn == nil || otherConn.Conn == nil {
					continue
				}
				if otherConn.UserID != conn.UserID {
					continue
				}
				if otherMeta, ok := s.runtime.metadataByMachine[otherID]; ok {
					if strings.EqualFold(otherMeta.Alias, nn) {
						aliasConflict = true
						break
					}
				}
			}
		}
		if aliasConflict {
			// Nickname conflicts with another online device — reassign.
			assignedNickname = s.pickNicknameLocked(machineID, conn)
			if assignedNickname != "" {
				info.Alias = assignedNickname
				log.Printf("[device] MarkOnline: nickname=%q conflicts, reassigned=%q for machine_id=%s", nn, assignedNickname, machineID)
			}
		} else {
			info.Alias = nn
			log.Printf("[device] MarkOnline: nickname=%q for machine_id=%s", nn, machineID)
		}
	} else if info.Alias == "" {
		// No nickname provided and none previously set — auto-assign one
		// so the device has a stable identity in IM from the start.
		assignedNickname = s.pickNicknameLocked(machineID, conn)
		if assignedNickname != "" {
			info.Alias = assignedNickname
			log.Printf("[device] MarkOnline: auto-assigned nickname=%q for machine_id=%s", assignedNickname, machineID)
		}
	}
	info.Online = true
	info.Status = "online"
	info.LastSeenAt = &now
	// Extract LLM configuration status from capabilities.
	if caps, ok := hello.Capabilities["llm_configured"]; ok {
		if v, ok := caps.(bool); ok {
			info.LLMConfigured = v
		}
	}

	// Check for name conflict among same-user online machines.
	var conflictWith string
	if conn != nil && info.Name != "" && assignedNickname == "" {
		for otherID, otherConn := range s.runtime.desktopsByMachine {
			if otherID == machineID || otherConn == nil || otherConn.Conn == nil {
				continue
			}
			if otherConn.UserID != conn.UserID {
				continue
			}
			if otherMeta, ok := s.runtime.metadataByMachine[otherID]; ok {
				if strings.EqualFold(otherMeta.Name, info.Name) {
					conflictWith = otherMeta.Name
					break
				}
			}
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

	// Count online machines for the same user (for multi-device guide).
	var onlineNames []string
	if conn != nil {
		for otherID, otherConn := range s.runtime.desktopsByMachine {
			if otherConn == nil || otherConn.Conn == nil || otherConn.UserID != conn.UserID {
				continue
			}
			if otherMeta, ok := s.runtime.metadataByMachine[otherID]; ok && otherMeta.Online {
				name := otherMeta.Alias
				if name == "" {
					name = otherMeta.Name
				}
				onlineNames = append(onlineNames, name)
			}
		}
	}

	s.runtime.mu.Unlock()

	// Notify the machine about its auto-assigned nickname so it can
	// remember it and report it on subsequent connections.
	if assignedNickname != "" {
		_ = s.SendToMachine(machineID, map[string]any{
			"type": "machine.nickname_assigned",
			"payload": map[string]any{
				"nickname": assignedNickname,
			},
		})
	}

	// Notify the machine about name conflict via WebSocket (non-blocking).
	if conflictWith != "" {
		log.Printf("[device] MarkOnline: name conflict for machine_id=%s name=%q (already used by another online machine)", machineID, conflictWith)
		_ = s.SendToMachine(machineID, map[string]any{
			"type": "machine.name_conflict",
			"payload": map[string]any{
				"name":    conflictWith,
				"message": fmt.Sprintf("昵称 %q 已被您的另一台在线设备使用，请修改昵称以便通过 IM 区分设备。", conflictWith),
			},
		})
	}

	// When the second device comes online, push a usage guide to the user's IM.
	if len(onlineNames) == 2 && s.OnMultiDeviceOnline != nil {
		userID := safeConnUserID(conn)
		go s.OnMultiDeviceOnline(userID, onlineNames)
	}

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
	// Persist Alias to DB when set (from hello payload or auto-assigned).
	if info.Alias != "" {
		if err := s.repo.UpdateAlias(ctx, machineID, info.Alias); err != nil {
			log.Printf("[device] MarkOnline WARNING: UpdateAlias failed for machine_id=%s: %v", machineID, err)
		}
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
	log.Printf("[device] SendToMachine: machine_id=%s conn=%p sendCh_len=%d", machineID, conn, len(conn.SendChDiag()))
	s.recordEvent(MachineEvent{
		Timestamp: time.Now().Unix(),
		MachineID: machineID,
		UserID:    safeConnUserID(conn),
		Type:      "send",
		Message:   "command dispatched to machine",
	})
	if !conn.Send(msg) {
		log.Printf("[device] SendToMachine: Send returned false (buffer full) machine_id=%s", machineID)
		return ErrMachineOffline
	}
	log.Printf("[device] SendToMachine: Send OK machine_id=%s", machineID)
	return nil
}

// FindOnlineMachineByName returns the machine ID of an online device belonging
// to the given user whose Name matches (case-insensitive). Returns ("", false)
// if no match is found.
func (s *Service) FindOnlineMachineByName(userID, name string) (machineID string, found bool) {
	s.runtime.mu.RLock()
	defer s.runtime.mu.RUnlock()
	// First pass: match Alias (priority).
	for mid, conn := range s.runtime.desktopsByMachine {
		if conn == nil || conn.Conn == nil || conn.UserID != userID {
			continue
		}
		if meta, ok := s.runtime.metadataByMachine[mid]; ok {
			if meta.Alias != "" && strings.EqualFold(meta.Alias, name) {
				return mid, true
			}
		}
	}
	// Second pass: match Name (fallback).
	for mid, conn := range s.runtime.desktopsByMachine {
		if conn == nil || conn.Conn == nil || conn.UserID != userID {
			continue
		}
		if meta, ok := s.runtime.metadataByMachine[mid]; ok {
			if strings.EqualFold(meta.Name, name) {
				return mid, true
			}
		}
	}
	return "", false
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
			info.Alias = meta.Alias
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
			// Runtime Alias takes precedence over DB — unlike other fields
			// which use DB-first fallback, Alias uses runtime-first override
			// because the runtime value reflects the most recent nickname
			// update from the machine (best-effort persisted to DB).
			if meta.Alias != "" {
				info.Alias = meta.Alias
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
			// Runtime Alias override — see comment in ListMachines.
			if meta.Alias != "" {
				info.Alias = meta.Alias
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
	if err := s.repo.UpdateAlias(ctx, machineID, alias); err != nil {
		return err
	}
	// Keep runtime in sync so the change is visible immediately.
	s.setRuntimeAlias(machineID, alias)
	return nil
}

// SetAlias updates the machine Alias in both runtime memory and DB (best-effort).
// Unlike RenameMachine (which requires a repo and returns errors), this is
// designed for the WS nickname_update path where we always want the runtime
// update to succeed and treat DB persistence as non-blocking.
func (s *Service) SetAlias(ctx context.Context, machineID string, alias string) {
	s.setRuntimeAlias(machineID, alias)
	log.Printf("[device] SetAlias: machine_id=%s alias=%q", machineID, alias)

	// Best-effort DB persistence — failure is logged but does not block the caller.
	if s.repo != nil {
		if err := s.repo.UpdateAlias(ctx, machineID, alias); err != nil {
			log.Printf("[device] SetAlias WARNING: DB persist failed for machine_id=%s: %v", machineID, err)
		}
	}
}

// setRuntimeAlias is the shared helper that updates the in-memory Alias.
func (s *Service) setRuntimeAlias(machineID string, alias string) {
	s.runtime.mu.Lock()
	info := s.runtime.metadataByMachine[machineID]
	info.Alias = alias
	s.runtime.metadataByMachine[machineID] = info
	s.runtime.mu.Unlock()
}

// CheckAliasConflict returns true if another online machine belonging to the
// same user already uses the given alias (case-insensitive).
func (s *Service) CheckAliasConflict(machineID, userID, alias string) bool {
	s.runtime.mu.RLock()
	defer s.runtime.mu.RUnlock()
	for otherID, otherConn := range s.runtime.desktopsByMachine {
		if otherID == machineID || otherConn == nil || otherConn.Conn == nil {
			continue
		}
		if otherConn.UserID != userID {
			continue
		}
		if otherMeta, ok := s.runtime.metadataByMachine[otherID]; ok {
			if strings.EqualFold(otherMeta.Alias, alias) {
				return true
			}
		}
	}
	return false
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

// defaultNicknames is the pool of friendly Chinese nicknames auto-assigned
// to machines that connect without a nickname.
var defaultNicknames = []string{"张三", "李四", "王五", "赵六", "孙七"}

// pickNicknameLocked selects a nickname for the given machine from the pool,
// avoiding names already used by other online machines of the same user.
// Must be called with s.runtime.mu held.
func (s *Service) pickNicknameLocked(machineID string, conn *ws.ConnContext) string {
	if conn == nil {
		return ""
	}
	used := make(map[string]bool)
	for otherID, otherConn := range s.runtime.desktopsByMachine {
		if otherID == machineID || otherConn == nil || otherConn.Conn == nil {
			continue
		}
		if otherConn.UserID != conn.UserID {
			continue
		}
		if otherMeta, ok := s.runtime.metadataByMachine[otherID]; ok {
			if otherMeta.Alias != "" {
				used[otherMeta.Alias] = true
			}
		}
	}
	for _, n := range defaultNicknames {
		if !used[n] {
			return n
		}
	}
	// Fallback: 助手N
	for i := len(defaultNicknames) + 1; ; i++ {
		n := fmt.Sprintf("助手%d", i)
		if !used[n] {
			return n
		}
	}
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
