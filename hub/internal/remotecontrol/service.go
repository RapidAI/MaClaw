package remotecontrol

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	PermView    = "view"
	PermOperate = "operate"

	StatusPending  = "pending"
	StatusActive   = "active"
	StatusRejected = "rejected"
	StatusRevoked  = "revoked"
	StatusExpired  = "expired"

	OpListTasks   = "list_tasks"
	OpGetTask     = "get_task"
	OpCreateTask  = "create_task"
	OpSendMessage = "send_message"
	OpStopTask    = "stop_task"
	OpRenameTask  = "rename_task"
	OpPinTask     = "pin_task"
	OpFetchFile   = "fetch_file"
	OpWatchTask   = "watch_task"
	OpUnwatchTask = "unwatch_task"

	inviteTTL = 7 * 24 * time.Hour
	callWait  = 20 * time.Second
)

// Invite and result notices. The HTTP layer maps these onto the notification
// sources of the same name.
const (
	NoticeInvite = "remote_control_invite"
	NoticeWatch  = "remote_control_notice"
	NoticeResult = "remote_control_result"
)

var (
	ErrNotFound = errors.New("没有找到这次远控")
	ErrDenied   = errors.New("没有远控权限")
	ErrInvalid  = errors.New("远控请求无效")
	ErrOffline  = errors.New("对方离线")
	ErrTimeout  = errors.New("对方没有响应")
)

// StatusError is a failure the HTTP layer can return as-is.
type StatusError struct {
	Code    string
	Message string
}

func (e *StatusError) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

func invalid(message string) error {
	return &StatusError{Code: "INVALID", Message: message}
}

func denied(message string) error {
	if message == "" {
		message = ErrDenied.Error()
	}
	return &StatusError{Code: "FORBIDDEN", Message: message}
}

// Grant is one controller-machine to target-machine authorization.
type Grant struct {
	ID                    string
	TenantID              string
	ControllerUserID      string
	ControllerMachineID   string
	ControllerMachineName string
	TargetUserID          string
	TargetAccount         string
	TargetMachineID       string
	TargetMachineName     string
	ControllerHub         string
	TargetHub             string
	FederationSecret      string
	Permission            string
	Status                string
	SameAccount           bool
	TargetIgnored         bool
	InviteNotificationID  string
	NoticeNotificationID  string
	CreatedAt             time.Time
	DecidedAt             time.Time
	ExpiresAt             time.Time
	UpdatedAt             time.Time
}

// Machine is one desktop registered to a Hub user.
type Machine struct {
	ID     string
	Name   string
	Online bool
}

// Accounts resolves the contact typed into the add dialog.
type Accounts interface {
	Lookup(ctx context.Context, tenantID, contact string) (userID string, ok bool)
}

// Machines answers ownership and liveness for a desktop.
type Machines interface {
	BelongsTo(ctx context.Context, userID, machineID string) (Machine, bool)
	Online(machineID string) bool
}

// Sender delivers one envelope to an online desktop.
type Sender interface {
	Send(machineID string, msg any) error
}

// Notice is one bell message.
type Notice struct {
	AudienceUserID string
	Title          string
	Content        string
	Source         string
	SourceID       string
	Important      bool
}

// Notifier delivers and withdraws bell messages.
type Notifier interface {
	Notify(ctx context.Context, notice Notice) (string, error)
	Revoke(ctx context.Context, notificationID string)
}

// State is the task-list snapshot for one desktop.
type State struct {
	MachineID  string        `json:"machine_id"`
	Machines   []MachineView `json:"machines"`
	Grants     []GrantView   `json:"grants"`
	Pending    []GrantView   `json:"pending"`
	Invites    []GrantView   `json:"invites"`
	Controlled []GrantView   `json:"controlled"`
}

// MachineView is another desktop of the signed-in account.
type MachineView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Online bool   `json:"online"`
}

// GrantView is one row the task list or the add dialog renders.
type GrantView struct {
	ID          string `json:"id"`
	MachineID   string `json:"machine_id,omitempty"`
	MachineName string `json:"machine_name,omitempty"`
	Account     string `json:"account,omitempty"`
	Permission  string `json:"permission"`
	Status      string `json:"status"`
	SameAccount bool   `json:"same_account"`
	Online      bool   `json:"online"`
	FromMachine string `json:"from_machine,omitempty"`
}

// Service stores grants and relays task operations over the Hub connection.
type Service struct {
	db       *sql.DB
	accounts Accounts
	machines Machines
	sender   Sender
	notify   Notifier

	// RelayFederatedRevoke, when set, tells the controller's own Hub that a
	// grant it originated was ended here, so the remote copy is revoked and
	// the remote controller is notified. The HTTP layer wires it because
	// cross-Hub calls live outside this package.
	RelayFederatedRevoke func(ctx context.Context, grant Grant)

	mu            sync.Mutex
	waiters       map[string]waiter
	activateMu    sync.Mutex
	streamBuckets map[string]*streamBucket
}

type waiter struct {
	machineID string
	ch        chan callResult
}

type callResult struct {
	body json.RawMessage
	err  string
}

// New opens the grant store. A nil database disables the feature.
func New(db *sql.DB, accounts Accounts, machines Machines, sender Sender, notify Notifier) *Service {
	if db == nil {
		return nil
	}
	svc := &Service{
		db:       db,
		accounts: accounts,
		machines: machines,
		sender:   sender,
		notify:   notify,
		waiters:  map[string]waiter{},
	}
	if err := svc.ensureSchema(context.Background()); err != nil {
		return svc
	}
	return svc
}

func (s *Service) ensureSchema(ctx context.Context) error {
	if s == nil || s.db == nil {
		return nil
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS remote_control_grants (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			controller_user_id TEXT NOT NULL,
			controller_machine_id TEXT NOT NULL,
			controller_machine_name TEXT NOT NULL DEFAULT '',
			target_user_id TEXT NOT NULL DEFAULT '',
			target_account TEXT NOT NULL DEFAULT '',
			target_machine_id TEXT NOT NULL DEFAULT '',
			target_machine_name TEXT NOT NULL DEFAULT '',
			controller_hub TEXT NOT NULL DEFAULT '',
			target_hub TEXT NOT NULL DEFAULT '',
			federation_secret TEXT NOT NULL DEFAULT '',
			permission TEXT NOT NULL,
			status TEXT NOT NULL,
			same_account INTEGER NOT NULL DEFAULT 0,
			target_ignored INTEGER NOT NULL DEFAULT 0,
			invite_notification_id TEXT NOT NULL DEFAULT '',
			notice_notification_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			decided_at TEXT NOT NULL DEFAULT '',
			expires_at TEXT NOT NULL DEFAULT '',
			updated_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_rc_grant_controller ON remote_control_grants(tenant_id, controller_user_id, status)`,
		`CREATE INDEX IF NOT EXISTS idx_rc_grant_target ON remote_control_grants(tenant_id, target_user_id, status)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	for _, column := range []string{"controller_hub", "target_hub", "federation_secret", "target_machine_id"} {
		if _, err := s.db.ExecContext(ctx, "ALTER TABLE remote_control_grants ADD COLUMN "+column+" TEXT NOT NULL DEFAULT ''"); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
			return err
		}
	}
	_, err := s.db.ExecContext(ctx, `CREATE INDEX IF NOT EXISTS idx_rc_grant_target_machine ON remote_control_grants(target_machine_id, status)`)
	return err
}

// GrantSameAccount authorizes another desktop of the same user immediately.
func (s *Service) GrantSameAccount(ctx context.Context, tenantID, userID, controllerMachineID, targetMachineID, permission string) (Grant, error) {
	if err := s.ready(); err != nil {
		return Grant{}, err
	}
	permission, err := normalizePermission(permission)
	if err != nil {
		return Grant{}, err
	}
	controller, err := s.requireOwnMachine(ctx, userID, controllerMachineID)
	if err != nil {
		return Grant{}, err
	}
	targetMachineID = strings.TrimSpace(targetMachineID)
	if targetMachineID == "" || targetMachineID == controller.ID {
		return Grant{}, invalid("不能添加本机")
	}
	target, ok := s.machines.BelongsTo(ctx, userID, targetMachineID)
	if !ok {
		return Grant{}, invalid("没有找到这台机器")
	}
	s.expirePending(ctx, tenantID, userID)
	s.activateMu.Lock()
	var displaced []Grant
	if existing, found := s.findActivePair(ctx, tenantID, controller.ID, target.ID); found {
		name := displayMachine(target)
		permissionChanged := existing.Permission != permission
		if !permissionChanged && existing.TargetMachineName == name {
			displaced = s.displacePeerControllers(ctx, target.ID, existing.ID)
			s.activateMu.Unlock()
			s.notifyDisplaced(ctx, displaced)
			return existing, nil
		}
		existing.Permission = permission
		existing.TargetMachineName = name
		existing.UpdatedAt = time.Now().UTC()
		changed, err := s.updateExpected(ctx, existing, StatusActive)
		if err != nil {
			s.activateMu.Unlock()
			return Grant{}, err
		}
		if changed {
			displaced = s.displacePeerControllers(ctx, target.ID, existing.ID)
			s.activateMu.Unlock()
			s.notifyDisplaced(ctx, displaced)
			if permissionChanged {
				s.notifyWatch(ctx, existing)
			}
			return existing, nil
		}
		// A concurrent revoke ended the pair between the lookup and the
		// update. Grant fresh below instead of writing it back to life.
	}
	now := time.Now().UTC()
	grant := Grant{
		ID:                    newID(),
		TenantID:              strings.TrimSpace(tenantID),
		ControllerUserID:      userID,
		ControllerMachineID:   controller.ID,
		ControllerMachineName: displayMachine(controller),
		TargetUserID:          userID,
		TargetAccount:         userID,
		TargetMachineID:       target.ID,
		TargetMachineName:     displayMachine(target),
		Permission:            permission,
		Status:                StatusActive,
		SameAccount:           true,
		CreatedAt:             now,
		DecidedAt:             now,
		UpdatedAt:             now,
	}
	if err := s.insert(ctx, grant); err != nil {
		s.activateMu.Unlock()
		return Grant{}, err
	}
	displaced = s.displacePeerControllers(ctx, target.ID, grant.ID)
	s.activateMu.Unlock()
	s.notifyDisplaced(ctx, displaced)
	s.notifyWatch(ctx, grant)
	return grant, nil
}

// Invite asks another account to accept on whichever desktop they choose.
func (s *Service) Invite(ctx context.Context, tenantID, userID, controllerMachineID, account, permission string) (Grant, error) {
	if err := s.ready(); err != nil {
		return Grant{}, err
	}
	permission, err := normalizePermission(permission)
	if err != nil {
		return Grant{}, err
	}
	controller, err := s.requireOwnMachine(ctx, userID, controllerMachineID)
	if err != nil {
		return Grant{}, err
	}
	account = strings.TrimSpace(account)
	if account == "" {
		return Grant{}, invalid("请填写对方帐号")
	}
	if s.accounts == nil {
		return Grant{}, invalid("没有找到这个帐号")
	}
	targetUserID, ok := s.accounts.Lookup(ctx, tenantID, account)
	if !ok || targetUserID == "" {
		return Grant{}, invalid("没有找到这个帐号")
	}
	if targetUserID == userID {
		return Grant{}, invalid("同帐号的机器请从列表里添加")
	}
	s.expirePending(ctx, tenantID, userID)
	if existing, found := s.findOpenInvite(ctx, tenantID, userID, controller.ID, targetUserID); found {
		if existing.InviteNotificationID == "" {
			s.notifyInvite(ctx, existing)
		}
		return existing, nil
	}
	now := time.Now().UTC()
	grant := Grant{
		ID:                    newID(),
		TenantID:              strings.TrimSpace(tenantID),
		ControllerUserID:      userID,
		ControllerMachineID:   controller.ID,
		ControllerMachineName: displayMachine(controller),
		TargetUserID:          targetUserID,
		TargetAccount:         account,
		Permission:            permission,
		Status:                StatusPending,
		CreatedAt:             now,
		ExpiresAt:             now.Add(inviteTTL),
		UpdatedAt:             now,
	}
	if err := s.insert(ctx, grant); err != nil {
		return Grant{}, err
	}
	s.notifyInvite(ctx, grant)
	return grant, nil
}

// Accept binds the invitation to the desktop that answered it.
func (s *Service) Accept(ctx context.Context, tenantID, userID, machineID, grantID string) (Grant, error) {
	grant, err := s.pendingForTarget(ctx, tenantID, userID, grantID)
	if err != nil {
		current, loadErr := s.load(ctx, grantID)
		machineID = strings.TrimSpace(machineID)
		if loadErr == nil && current.Status == StatusActive && current.TargetUserID == userID && current.TargetMachineID == machineID {
			return current, nil
		}
		return Grant{}, err
	}
	machine, err := s.requireOwnMachine(ctx, userID, machineID)
	if err != nil {
		return Grant{}, err
	}
	// The stored tenant is the inviter's. The caller here is the invitee, who
	// may belong to another tenant, so the pair has to be found on the grant.
	s.activateMu.Lock()
	var displaced []Grant
	if existing, found := s.findActivePair(ctx, grant.TenantID, grant.ControllerMachineID, machine.ID); found {
		now := time.Now().UTC()
		grant.Status = StatusRevoked
		grant.DecidedAt = now
		grant.UpdatedAt = now
		changed, err := s.updateExpected(ctx, grant, StatusPending)
		if err != nil {
			s.activateMu.Unlock()
			return Grant{}, err
		}
		if !changed {
			s.activateMu.Unlock()
			return Grant{}, ErrNotFound
		}
		existing.Permission = grant.Permission
		existing.UpdatedAt = now
		changed, err = s.updateExpected(ctx, existing, StatusActive)
		if err != nil {
			s.activateMu.Unlock()
			return Grant{}, err
		}
		if !changed {
			s.activateMu.Unlock()
			return Grant{}, ErrNotFound
		}
		displaced = s.displacePeerControllers(ctx, machine.ID, existing.ID)
		s.activateMu.Unlock()
		s.notifyDisplaced(ctx, displaced)
		s.revokeNotice(ctx, grant.InviteNotificationID)
		s.notifyControllerResult(ctx, grant, "远控已接受", fmt.Sprintf("%s 已接受远控，权限：%s。", displayMachine(machine), permissionLabel(grant.Permission)), existing.ID)
		s.notifyWatch(ctx, existing)
		return existing, nil
	}
	now := time.Now().UTC()
	grant.TargetMachineID = machine.ID
	grant.TargetMachineName = displayMachine(machine)
	grant.Status = StatusActive
	grant.TargetIgnored = false
	grant.DecidedAt = now
	grant.UpdatedAt = now
	changed, err := s.updateExpected(ctx, grant, StatusPending)
	if err != nil {
		s.activateMu.Unlock()
		return Grant{}, err
	}
	if !changed {
		s.activateMu.Unlock()
		current, loadErr := s.load(ctx, grant.ID)
		if loadErr == nil && current.Status == StatusActive && current.TargetUserID == userID && current.TargetMachineID == machine.ID {
			return current, nil
		}
		return Grant{}, ErrNotFound
	}
	displaced = s.displacePeerControllers(ctx, machine.ID, grant.ID)
	s.activateMu.Unlock()
	s.notifyDisplaced(ctx, displaced)
	s.revokeNotice(ctx, grant.InviteNotificationID)
	s.notifyControllerResult(ctx, grant, "远控已接受", fmt.Sprintf("%s 已接受远控，权限：%s。任务列表里已出现这台机器。", grant.TargetMachineName, permissionLabel(grant.Permission)), grant.ID)
	s.notifyWatch(ctx, grant)
	return grant, nil
}

// Reject tells the inviter and drops the invitation.
func (s *Service) Reject(ctx context.Context, tenantID, userID, grantID string) error {
	grant, err := s.pendingForTarget(ctx, tenantID, userID, grantID)
	if err != nil {
		current, loadErr := s.load(ctx, grantID)
		if loadErr == nil && current.Status == StatusRejected && current.TargetUserID == userID {
			return nil
		}
		return err
	}
	now := time.Now().UTC()
	grant.Status = StatusRejected
	grant.DecidedAt = now
	grant.UpdatedAt = now
	changed, err := s.updateExpected(ctx, grant, StatusPending)
	if err != nil {
		return err
	}
	if !changed {
		current, loadErr := s.load(ctx, grant.ID)
		if loadErr == nil && current.Status == StatusRejected && current.TargetUserID == userID {
			return nil
		}
		return ErrNotFound
	}
	s.revokeNotice(ctx, grant.InviteNotificationID)
	who := strings.TrimSpace(grant.TargetAccount)
	if who == "" {
		who = "对方"
	}
	s.notifyControllerResult(ctx, grant, "远控已拒绝", who+" 已拒绝远控。", grant.ID)
	return nil
}

// Ignore hides the invitation from the invitee and does not tell the inviter.
func (s *Service) Ignore(ctx context.Context, tenantID, userID, grantID string) error {
	grant, err := s.pendingForTarget(ctx, tenantID, userID, grantID)
	if err != nil {
		current, loadErr := s.load(ctx, grantID)
		if loadErr == nil && current.TargetUserID == userID && current.TargetIgnored {
			return nil
		}
		return err
	}
	grant.TargetIgnored = true
	grant.UpdatedAt = time.Now().UTC()
	changed, err := s.updateExpected(ctx, grant, StatusPending)
	if err != nil {
		return err
	}
	if !changed {
		return ErrNotFound
	}
	s.revokeNotice(ctx, grant.InviteNotificationID)
	return nil
}

// Revoke ends an active grant or cancels a pending invite.
// The caller is the controller when they own the controller machine, otherwise
// the controlled account.
func (s *Service) Revoke(ctx context.Context, tenantID, userID, machineID, grantID string) error {
	grant, err := s.load(ctx, grantID)
	if err != nil {
		return err
	}
	// The grant keeps the inviter's tenant. The invitee may belong to another
	// tenant; user ids are global, so they can still end the grant.
	if grant.TenantID != strings.TrimSpace(tenantID) && grant.TargetUserID != userID {
		return ErrNotFound
	}
	// Ending from the controlling machine stays the controller path, including
	// same-account, so that close does not also count as the target ending it.
	// The target notice is delivered to the account, so any of that account's
	// desktops can end the grant. The bound machine is not the only one.
	asController := grant.ControllerUserID == userID && grant.ControllerMachineID == strings.TrimSpace(machineID)
	asTarget := !asController && grant.TargetUserID == userID
	if !asController && !asTarget {
		return denied("")
	}
	if grant.Status == StatusRevoked {
		return nil
	}
	if grant.Status != StatusActive && grant.Status != StatusPending {
		return ErrNotFound
	}
	for attempt := 0; attempt < 2; attempt++ {
		now := time.Now().UTC()
		previous := grant.Status
		grant.Status = StatusRevoked
		grant.DecidedAt = now
		grant.UpdatedAt = now
		changed, err := s.updateExpected(ctx, grant, previous)
		if err != nil {
			return err
		}
		if !changed {
			current, loadErr := s.load(ctx, grant.ID)
			if loadErr != nil {
				return loadErr
			}
			if current.Status == StatusRevoked {
				return nil
			}
			if current.Status == StatusActive || current.Status == StatusPending {
				grant = current
				continue
			}
			return ErrNotFound
		}
		s.revokeNotice(ctx, grant.InviteNotificationID)
		s.revokeNotice(ctx, grant.NoticeNotificationID)
		if asController && !grant.SameAccount && previous == StatusActive {
			s.notifyTargetResult(ctx, grant, "远控已结束", "对方已结束对这台电脑的远控。", grant.ID)
		}
		if asTarget && previous == StatusActive {
			s.notifyControllerResult(ctx, grant, "远控已结束", grant.TargetMachineName+" 已结束远控。", grant.ID)
		}
		return nil
	}
	return ErrNotFound
}

// Snapshot lists tabs, waiting invites, and machines of this account.
func (s *Service) Snapshot(ctx context.Context, tenantID, userID, machineID string) (State, error) {
	state := State{MachineID: strings.TrimSpace(machineID), Machines: []MachineView{}, Grants: []GrantView{}, Pending: []GrantView{}, Invites: []GrantView{}, Controlled: []GrantView{}}
	if err := s.ready(); err != nil {
		return state, err
	}
	s.expirePending(ctx, tenantID, userID)
	machineID = strings.TrimSpace(machineID)
	rows, err := s.listForDesktop(ctx, tenantID, userID, machineID)
	if err != nil {
		return state, err
	}
	for _, grant := range rows {
		// A stale second-active row must never read as "controlling": retire
		// it so both sides' views converge without waiting for the next op.
		if grant.Status == StatusActive && s.retireStaleController(ctx, grant) {
			continue
		}
		switch {
		case grant.ControllerUserID == userID && grant.ControllerMachineID == machineID && grant.Status == StatusActive:
			state.Grants = append(state.Grants, s.controllerView(ctx, grant))
		case grant.ControllerUserID == userID && grant.ControllerMachineID == machineID && grant.Status == StatusPending:
			state.Pending = append(state.Pending, GrantView{
				ID:          grant.ID,
				Account:     grant.TargetAccount,
				Permission:  grant.Permission,
				Status:      StatusPending,
				SameAccount: grant.SameAccount,
			})
		case grant.TargetUserID == userID && grant.Status == StatusPending && !grant.TargetIgnored:
			state.Invites = append(state.Invites, GrantView{
				ID:          grant.ID,
				Permission:  grant.Permission,
				Status:      StatusPending,
				FromMachine: grant.ControllerMachineName,
			})
		case grant.TargetUserID == userID && grant.Status == StatusActive && grant.TargetMachineID == machineID:
			state.Controlled = append(state.Controlled, GrantView{
				ID:          grant.ID,
				Permission:  grant.Permission,
				Status:      StatusActive,
				SameAccount: grant.SameAccount,
				FromMachine: grant.ControllerMachineName,
			})
		}
	}
	return state, nil
}

// ListOwnMachines returns the account's other desktops.
func (s *Service) ListOwnMachines(ctx context.Context, userID, selfID string, list func(context.Context, string) ([]Machine, error)) ([]Machine, error) {
	if list == nil {
		return nil, invalid("没有找到这台机器")
	}
	items, err := list(ctx, userID)
	if err != nil {
		return nil, err
	}
	selfID = strings.TrimSpace(selfID)
	out := make([]Machine, 0, len(items))
	for _, item := range items {
		if item.ID == "" || item.ID == selfID {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

// Exec forwards one task operation to the controlled desktop.
func (s *Service) Exec(ctx context.Context, tenantID, userID, controllerMachineID, grantID, op string, body json.RawMessage) (json.RawMessage, error) {
	if err := s.ready(); err != nil {
		return nil, err
	}
	if !allowedOp(op) {
		return nil, invalid("不支持这个远控操作")
	}
	grant, err := s.load(ctx, grantID)
	if err != nil {
		return nil, err
	}
	if grant.TenantID != strings.TrimSpace(tenantID) || grant.Status != StatusActive {
		return nil, ErrNotFound
	}
	if grant.ControllerUserID != userID || grant.ControllerMachineID != strings.TrimSpace(controllerMachineID) {
		return nil, denied("不能把远控转给其它机器")
	}
	if s.retireStaleController(ctx, grant) {
		return nil, ErrNotFound
	}
	if !permissionAllows(grant.Permission, op) {
		return nil, denied("当前权限只能查看")
	}
	if s.sender == nil || s.machines == nil || !s.machines.Online(grant.TargetMachineID) {
		return nil, &StatusError{Code: "OFFLINE", Message: ErrOffline.Error()}
	}
	return s.call(ctx, grant.TargetMachineID, grant.ID, op, body)
}

// StreamGrant validates one remote_control.stream envelope sent by a target
// (controlled) machine and returns the grant it belongs to. The envelope must
// name a grant whose controlled machine is the sender and which is still
// active; anything else is dropped by the caller without a reply.
func (s *Service) StreamGrant(ctx context.Context, grantID, fromMachineID string) (Grant, error) {
	if err := s.ready(); err != nil {
		return Grant{}, err
	}
	grant, err := s.load(ctx, grantID)
	if err != nil || grant.TargetMachineID != strings.TrimSpace(fromMachineID) || grant.Status != StatusActive {
		return Grant{}, ErrNotFound
	}
	return grant, nil
}

// AllowStream is a small per-grant token bucket for remote_control.stream
// relay. Coalesced token events top out around a handful per second; the
// bucket leaves headroom for progress bursts while still bounding a target
// that misbehaves.
func (s *Service) AllowStream(grantID string) bool {
	grantID = strings.TrimSpace(grantID)
	if grantID == "" {
		return false
	}
	const ratePerSec = 30.0
	const burst = 60.0
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.streamBuckets == nil {
		s.streamBuckets = map[string]*streamBucket{}
	}
	b, ok := s.streamBuckets[grantID]
	if !ok {
		b = &streamBucket{tokens: burst, at: now}
		s.streamBuckets[grantID] = b
	}
	b.tokens += now.Sub(b.at).Seconds() * ratePerSec
	if b.tokens > burst {
		b.tokens = burst
	}
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type streamBucket struct {
	tokens float64
	at     time.Time
}

// Complete delivers the desktop's reply to the waiting call.
func (s *Service) Complete(machineID, requestID string, payload json.RawMessage) {
	if s == nil {
		return
	}
	var decoded struct {
		Body  json.RawMessage `json:"body"`
		Error string          `json:"error"`
	}
	_ = json.Unmarshal(payload, &decoded)
	s.mu.Lock()
	w, ok := s.waiters[strings.TrimSpace(requestID)]
	s.mu.Unlock()
	if !ok || w.machineID != strings.TrimSpace(machineID) {
		return
	}
	select {
	case w.ch <- callResult{body: decoded.Body, err: strings.TrimSpace(decoded.Error)}:
	default:
	}
}

func (s *Service) call(ctx context.Context, machineID, grantID, op string, body json.RawMessage) (json.RawMessage, error) {
	requestID := newID()
	ch := make(chan callResult, 1)
	s.mu.Lock()
	s.waiters[requestID] = waiter{machineID: machineID, ch: ch}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.waiters, requestID)
		s.mu.Unlock()
	}()
	if len(body) == 0 {
		body = json.RawMessage(`{}`)
	}
	err := s.sender.Send(machineID, map[string]any{
		"type":       "remote_control.exec",
		"request_id": requestID,
		"machine_id": machineID,
		"payload": map[string]any{
			"grant_id": grantID,
			"op":       op,
			"body":     body,
		},
	})
	if err != nil {
		return nil, &StatusError{Code: "OFFLINE", Message: ErrOffline.Error()}
	}
	timer := time.NewTimer(callWait)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, &StatusError{Code: "TIMEOUT", Message: ErrTimeout.Error()}
	case res := <-ch:
		if res.err != "" {
			return nil, &StatusError{Code: "REMOTE", Message: res.err}
		}
		if len(res.body) == 0 {
			return json.RawMessage(`{}`), nil
		}
		return res.body, nil
	}
}

func (s *Service) ready() error {
	if s == nil || s.db == nil {
		return invalid("远控尚未就绪")
	}
	return nil
}

func (s *Service) requireOwnMachine(ctx context.Context, userID, machineID string) (Machine, error) {
	machineID = strings.TrimSpace(machineID)
	if s.machines == nil || machineID == "" {
		return Machine{}, invalid("请先登录这台电脑")
	}
	machine, ok := s.machines.BelongsTo(ctx, userID, machineID)
	if !ok || machine.ID == "" {
		return Machine{}, invalid("请先登录这台电脑")
	}
	return machine, nil
}

func (s *Service) pendingForTarget(ctx context.Context, _ string, userID, grantID string) (Grant, error) {
	grant, err := s.load(ctx, grantID)
	if err != nil {
		return Grant{}, err
	}
	// Tenant is the inviter's. Matching it would hide the invite from an
	// invitee who lives in another tenant. The target user id is the check.
	if grant.TargetUserID != userID || grant.Status != StatusPending || grant.TargetIgnored {
		return Grant{}, ErrNotFound
	}
	if !grant.ExpiresAt.IsZero() && time.Now().UTC().After(grant.ExpiresAt) {
		s.expireOne(ctx, grant)
		return Grant{}, ErrNotFound
	}
	return grant, nil
}

func (s *Service) expirePending(ctx context.Context, tenantID, userID string) {
	rows, err := s.db.QueryContext(ctx, grantSelect+`
		WHERE status = ? AND expires_at <> '' AND (
			(tenant_id = ? AND controller_user_id = ?)
			OR target_user_id = ?
		)`,
		StatusPending, strings.TrimSpace(tenantID), userID, userID)
	if err != nil {
		return
	}
	now := time.Now().UTC()
	var due []Grant
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			_ = rows.Close()
			return
		}
		if !grant.ExpiresAt.IsZero() && now.After(grant.ExpiresAt) {
			due = append(due, grant)
		}
	}
	_ = rows.Close()
	// SQLite allows one statement on the connection. Expire after the
	// cursor is closed, or the status update is silently skipped.
	for _, grant := range due {
		s.expireOne(ctx, grant)
	}
}

func (s *Service) expireOne(ctx context.Context, grant Grant) {
	grant.Status = StatusExpired
	grant.UpdatedAt = time.Now().UTC()
	grant.DecidedAt = grant.UpdatedAt
	changed, err := s.updateExpected(ctx, grant, StatusPending)
	if err != nil || !changed {
		return
	}
	s.revokeNotice(ctx, grant.InviteNotificationID)
	who := strings.TrimSpace(grant.TargetAccount)
	if who == "" {
		who = "对方"
	}
	s.notifyControllerResult(ctx, grant, "远控邀请已过期", "发给 "+who+" 的远控邀请已过期。", grant.ID)
}

func (s *Service) notifyInvite(ctx context.Context, grant Grant) {
	if s.notify == nil {
		return
	}
	id, err := s.notify.Notify(ctx, Notice{
		AudienceUserID: grant.TargetUserID,
		Title:          "远控请求",
		Content:        fmt.Sprintf("%s 请求远控你的电脑，权限：%s。在这台电脑上接受后，对方看到的任务都在这台电脑上运行。", grant.ControllerMachineName, permissionLabel(grant.Permission)),
		Source:         NoticeInvite,
		SourceID:       grant.ID,
		Important:      true,
	})
	if err != nil || id == "" {
		return
	}
	// The notice is created after the invite row exists. Accept, reject, or
	// ignore can land in that gap. Writing the whole row back would restore
	// the pending snapshot and undo that decision.
	if !s.attachNoticeID(ctx, grant.ID, "invite_notification_id", id, StatusPending) {
		s.revokeNotice(ctx, id)
	}
}

func (s *Service) notifyWatch(ctx context.Context, grant Grant) {
	if s.notify == nil {
		return
	}
	s.revokeNotice(ctx, grant.NoticeNotificationID)
	title := "远控已开始"
	if grant.SameAccount {
		title = "同帐号远控"
	}
	id, err := s.notify.Notify(ctx, Notice{
		AudienceUserID: grant.TargetUserID,
		Title:          title,
		Content:        fmt.Sprintf("%s 正在以「%s」使用这台电脑上的任务。任务仍在这台电脑上运行。你可以结束这次远控。", grant.ControllerMachineName, permissionLabel(grant.Permission)),
		Source:         NoticeWatch,
		SourceID:       grant.ID,
		Important:      true,
	})
	if err != nil || id == "" {
		return
	}
	// Same gap as the invite notice: a full-row write can revive a grant the
	// other side already ended while this notice was being created.
	if !s.attachNoticeID(ctx, grant.ID, "notice_notification_id", id, StatusActive) {
		s.revokeNotice(ctx, id)
	}
}

func (s *Service) notifyResult(ctx context.Context, userID, title, content, sourceID string) {
	if s.notify == nil || strings.TrimSpace(userID) == "" {
		return
	}
	_, _ = s.notify.Notify(ctx, Notice{
		AudienceUserID: userID,
		Title:          title,
		Content:        content,
		Source:         NoticeResult,
		SourceID:       sourceID,
	})
}

// displacePeerControllers ends every other active grant on the same target:
// one desktop answers to exactly one controller at a time, so whoever
// activates last wins. It runs under activateMu and returns the grants it
// displaced so the caller can notify them after the lock is released.
func (s *Service) displacePeerControllers(ctx context.Context, targetMachineID, keepID string) []Grant {
	targetMachineID = strings.TrimSpace(targetMachineID)
	if targetMachineID == "" {
		return nil
	}
	rows, err := s.db.QueryContext(ctx, grantSelect+` WHERE target_machine_id = ? AND status = ? AND id <> ?`,
		targetMachineID, StatusActive, strings.TrimSpace(keepID))
	if err != nil {
		return nil
	}
	var displaced []Grant
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			_ = rows.Close()
			return displaced
		}
		displaced = append(displaced, grant)
	}
	_ = rows.Close()
	var ended []Grant
	for _, grant := range displaced {
		now := time.Now().UTC()
		grant.Status = StatusRevoked
		grant.DecidedAt = now
		grant.UpdatedAt = now
		changed, err := s.updateExpected(ctx, grant, StatusActive)
		if err != nil || !changed {
			continue
		}
		ended = append(ended, grant)
	}
	return ended
}

// retireStaleController enforces one controller per desktop at exec time as
// well as at activation: when a newer grant already controls the same machine
// this row is a stale leftover — end it and tell its side, matching what the
// activation sweep would have produced. Returns true when the grant lost.
func (s *Service) retireStaleController(ctx context.Context, grant Grant) bool {
	row := s.db.QueryRowContext(ctx, `SELECT id FROM remote_control_grants
		WHERE target_machine_id = ? AND status = ? AND (decided_at > ? OR (decided_at = ? AND id > ?))
		LIMIT 1`, grant.TargetMachineID, StatusActive, formatTime(grant.DecidedAt), formatTime(grant.DecidedAt), grant.ID)
	var newerID string
	if err := row.Scan(&newerID); err != nil {
		return false
	}
	now := time.Now().UTC()
	grant.Status = StatusRevoked
	grant.DecidedAt = now
	grant.UpdatedAt = now
	changed, err := s.updateExpected(ctx, grant, StatusActive)
	if err != nil || !changed {
		return true
	}
	s.notifyDisplaced(ctx, []Grant{grant})
	return true
}

// notifyDisplaced withdraws a displaced grant's notices and tells the old
// controller the session is over. Rows this Hub imported for a remote
// controller go through RelayFederatedRevoke so the controller's own Hub ends
// its copy too; local grants and controller-side copies notify the local
// controller user directly.
func (s *Service) notifyDisplaced(ctx context.Context, displaced []Grant) {
	for _, grant := range displaced {
		s.revokeNotice(ctx, grant.InviteNotificationID)
		s.revokeNotice(ctx, grant.NoticeNotificationID)
		if grant.TargetUserID == "" || grant.ControllerHub == "" {
			s.notifyResult(ctx, grant.ControllerUserID, "远控已结束", "另一台设备已开始远控这台电脑，本次远控已结束。", grant.ID)
			continue
		}
		if s.RelayFederatedRevoke != nil {
			s.RelayFederatedRevoke(ctx, grant)
		}
	}
}

// attachNoticeID records a notice on a grant that is still in expectStatus.
// It returns false when the grant already moved on, so the caller can drop
// the notice instead of rewriting status, machine, or permission.
func (s *Service) attachNoticeID(ctx context.Context, grantID, column, noticeID, expectStatus string) bool {
	var query string
	switch column {
	case "invite_notification_id":
		query = `UPDATE remote_control_grants SET invite_notification_id = ?, updated_at = ? WHERE id = ? AND status = ?`
	case "notice_notification_id":
		query = `UPDATE remote_control_grants SET notice_notification_id = ?, updated_at = ? WHERE id = ? AND status = ?`
	default:
		return false
	}
	res, err := s.db.ExecContext(ctx, query, noticeID, formatTime(time.Now().UTC()), grantID, expectStatus)
	if err != nil {
		return false
	}
	n, err := res.RowsAffected()
	return err == nil && n > 0
}

func (s *Service) revokeNotice(ctx context.Context, id string) {
	if s.notify == nil || strings.TrimSpace(id) == "" {
		return
	}
	s.notify.Revoke(ctx, id)
}

func (s *Service) controllerView(ctx context.Context, grant Grant) GrantView {
	name := grant.TargetMachineName
	online := false
	if s.machines != nil {
		online = s.machines.Online(grant.TargetMachineID)
		if machine, ok := s.machines.BelongsTo(ctx, grant.TargetUserID, grant.TargetMachineID); ok {
			name = displayMachine(machine)
		}
	}
	return GrantView{
		ID:          grant.ID,
		MachineID:   grant.TargetMachineID,
		MachineName: name,
		Account:     grant.TargetAccount,
		Permission:  grant.Permission,
		Status:      StatusActive,
		SameAccount: grant.SameAccount,
		Online:      online,
	}
}

func normalizePermission(permission string) (string, error) {
	switch strings.TrimSpace(permission) {
	case PermView, PermOperate:
		return strings.TrimSpace(permission), nil
	default:
		return "", invalid("请选择仅查看或可操作")
	}
}

func permissionAllows(permission, op string) bool {
	switch op {
	case OpListTasks, OpGetTask, OpFetchFile, OpWatchTask, OpUnwatchTask:
		return permission == PermView || permission == PermOperate
	case OpCreateTask, OpSendMessage, OpStopTask, OpRenameTask, OpPinTask:
		return permission == PermOperate
	default:
		return false
	}
}

func allowedOp(op string) bool {
	switch op {
	case OpListTasks, OpGetTask, OpCreateTask, OpSendMessage, OpStopTask, OpRenameTask, OpPinTask, OpFetchFile, OpWatchTask, OpUnwatchTask:
		return true
	default:
		return false
	}
}

func permissionLabel(permission string) string {
	if permission == PermOperate {
		return "可操作"
	}
	return "仅查看"
}

func displayMachine(machine Machine) string {
	name := strings.TrimSpace(machine.Name)
	if name == "" {
		name = machine.ID
	}
	return name
}

func newID() string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return fmt.Sprintf("rc_%d", time.Now().UnixNano())
	}
	return "rc_" + hex.EncodeToString(buf[:])
}

func (s *Service) insert(ctx context.Context, grant Grant) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO remote_control_grants (
		id, tenant_id, controller_user_id, controller_machine_id, controller_machine_name,
		target_user_id, target_account, target_machine_id, target_machine_name,
		controller_hub, target_hub, federation_secret,
		permission, status, same_account, target_ignored, invite_notification_id, notice_notification_id,
		created_at, decided_at, expires_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		grant.ID, grant.TenantID, grant.ControllerUserID, grant.ControllerMachineID, grant.ControllerMachineName,
		grant.TargetUserID, grant.TargetAccount, grant.TargetMachineID, grant.TargetMachineName,
		grant.ControllerHub, grant.TargetHub, grant.FederationSecret,
		grant.Permission, grant.Status, boolInt(grant.SameAccount), boolInt(grant.TargetIgnored), grant.InviteNotificationID, grant.NoticeNotificationID,
		formatTime(grant.CreatedAt), formatTime(grant.DecidedAt), formatTime(grant.ExpiresAt), formatTime(grant.UpdatedAt),
	)
	return err
}

func (s *Service) updateExpected(ctx context.Context, grant Grant, expectedStatus string) (bool, error) {
	query := `UPDATE remote_control_grants SET
		target_machine_id = ?, target_machine_name = ?, permission = ?, status = ?, target_ignored = ?,
		invite_notification_id = ?, notice_notification_id = ?, decided_at = ?, updated_at = ?
		WHERE id = ?`
	args := []any{
		grant.TargetMachineID, grant.TargetMachineName, grant.Permission, grant.Status, boolInt(grant.TargetIgnored),
		grant.InviteNotificationID, grant.NoticeNotificationID, formatTime(grant.DecidedAt), formatTime(grant.UpdatedAt), grant.ID,
	}
	if expectedStatus != "" {
		query += ` AND status = ?`
		args = append(args, expectedStatus)
	}
	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, err
	}
	changed, err := res.RowsAffected()
	return changed == 1, err
}

func (s *Service) load(ctx context.Context, id string) (Grant, error) {
	row := s.db.QueryRowContext(ctx, grantSelect+` WHERE id = ?`, strings.TrimSpace(id))
	grant, err := scanGrant(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Grant{}, ErrNotFound
	}
	return grant, err
}

func (s *Service) findActivePair(ctx context.Context, tenantID, controllerMachineID, targetMachineID string) (Grant, bool) {
	row := s.db.QueryRowContext(ctx, grantSelect+` WHERE tenant_id = ? AND controller_machine_id = ? AND target_machine_id = ? AND status = ?`,
		tenantID, controllerMachineID, targetMachineID, StatusActive)
	grant, err := scanGrant(row)
	if err != nil {
		return Grant{}, false
	}
	return grant, true
}

func (s *Service) findOpenInvite(ctx context.Context, tenantID, controllerUserID, controllerMachineID, targetUserID string) (Grant, bool) {
	row := s.db.QueryRowContext(ctx, grantSelect+` WHERE tenant_id = ? AND controller_user_id = ? AND controller_machine_id = ? AND target_user_id = ? AND status = ? AND target_ignored = 0`,
		tenantID, controllerUserID, controllerMachineID, targetUserID, StatusPending)
	grant, err := scanGrant(row)
	if err != nil {
		return Grant{}, false
	}
	return grant, true
}

func (s *Service) listForDesktop(ctx context.Context, tenantID, userID, machineID string) ([]Grant, error) {
	rows, err := s.db.QueryContext(ctx, grantSelect+`
		WHERE (
			tenant_id = ? AND controller_user_id = ? AND controller_machine_id = ? AND status IN (?, ?)
		) OR (
			target_user_id = ? AND status IN (?, ?)
		)
		ORDER BY created_at ASC`,
		strings.TrimSpace(tenantID),
		userID, machineID, StatusActive, StatusPending,
		userID, StatusActive, StatusPending,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Grant
	for rows.Next() {
		grant, err := scanGrant(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, grant)
	}
	return out, rows.Err()
}

const grantSelect = `SELECT id, tenant_id, controller_user_id, controller_machine_id, controller_machine_name,
	target_user_id, target_account, target_machine_id, target_machine_name,
	controller_hub, target_hub, federation_secret,
	permission, status, same_account, target_ignored, invite_notification_id, notice_notification_id,
	created_at, decided_at, expires_at, updated_at FROM remote_control_grants`

type grantScanner interface {
	Scan(dest ...any) error
}

func scanGrant(row grantScanner) (Grant, error) {
	var grant Grant
	var same, ignored int
	var created, decided, expires, updated string
	err := row.Scan(
		&grant.ID, &grant.TenantID, &grant.ControllerUserID, &grant.ControllerMachineID, &grant.ControllerMachineName,
		&grant.TargetUserID, &grant.TargetAccount, &grant.TargetMachineID, &grant.TargetMachineName,
		&grant.ControllerHub, &grant.TargetHub, &grant.FederationSecret,
		&grant.Permission, &grant.Status, &same, &ignored, &grant.InviteNotificationID, &grant.NoticeNotificationID,
		&created, &decided, &expires, &updated,
	)
	if err != nil {
		return Grant{}, err
	}
	grant.SameAccount = same != 0
	grant.TargetIgnored = ignored != 0
	grant.CreatedAt = parseTime(created)
	grant.DecidedAt = parseTime(decided)
	grant.ExpiresAt = parseTime(expires)
	grant.UpdatedAt = parseTime(updated)
	return grant, nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return time.Time{}
		}
	}
	return parsed
}
