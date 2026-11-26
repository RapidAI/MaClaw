package cloudagent

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/botmgmt"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const timeLayout = "2006-01-02T15:04:05.000000000Z"

func formatTS(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(timeLayout)
}

func parseTS(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	if t, err := time.Parse(timeLayout, value); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return t
	}
	return time.Time{}
}

// SrvSettingsKey is the system-settings key of the tenant maclawsrv record.
// It matches botmgmt's storage key. Cloud code reads the JSON itself.
func SrvSettingsKey(tenantID string) string {
	tenantID = store.NormalizeTenantID(tenantID)
	if tenantID == "" || tenantID == store.DefaultTenantID {
		return botmgmt.SettingsKey
	}
	return "tenant:" + tenantID + ":" + botmgmt.SettingsKey
}

// Service is the Hub-side cloud-agent record.
type Service struct {
	db       *sql.DB
	settings Settings
	disp     *Dispatcher
	now      func() time.Time
	miss     time.Duration

	// Directory resolves departments for the opening scope. Nil skips that step.
	Directory Directory

	// NodeHTTP dials a node's srv through the edge tunnel. The node link's
	// BaseURL is the virtual srv.<id>.node.invalid host; only this client
	// can reach it. Nil or a nil client keeps node conns offline.
	NodeHTTP func(nodeID string) *http.Client

	startOnce sync.Once
}

// New opens the cloud-agent tables. A nil database disables the feature.
func New(db *sql.DB, settings Settings) *Service {
	if db == nil {
		return nil
	}
	svc := &Service{
		db:       db,
		settings: settings,
		now:      time.Now,
		miss:     HeartbeatMiss,
	}
	svc.disp = newDispatcher(svc)
	if err := svc.ensureSchema(context.Background()); err != nil {
		log.Printf("[cloud-agent] schema: %v", err)
		return nil
	}
	return svc
}

// Dispatcher is the remote-control cloud target.
func (s *Service) Dispatcher() *Dispatcher {
	if s == nil {
		return nil
	}
	return s.disp
}

// Start interrupts stale turns every minute. Tests call InterruptStale directly.
func (s *Service) Start(ctx context.Context) {
	if s == nil || ctx == nil {
		return
	}
	s.startOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(60 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case now := <-ticker.C:
					_ = s.InterruptStale(context.Background(), now)
				}
			}
		}()
	})
}

func (s *Service) ensureSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS cloud_agents (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			owner_user_id TEXT NOT NULL,
			name TEXT NOT NULL,
			kind TEXT NOT NULL,
			llm_mode TEXT NOT NULL DEFAULT 'official',
			llm_label TEXT NOT NULL DEFAULT '',
			llm_model TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			cloud_workspace_id TEXT NOT NULL DEFAULT '',
			srv_node_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			deleted_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cloud_agents_owner ON cloud_agents(tenant_id, owner_user_id, deleted_at)`,
		`CREATE TABLE IF NOT EXISTS cloud_agent_turns (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			owner_user_id TEXT NOT NULL,
			instance_id TEXT NOT NULL,
			task_id TEXT NOT NULL,
			request_id TEXT NOT NULL,
			status TEXT NOT NULL,
			started_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			finished_at TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_cloud_agent_turns_owner ON cloud_agent_turns(tenant_id, owner_user_id, status)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_cloud_agent_turns_request ON cloud_agent_turns(instance_id, request_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE cloud_agents ADD COLUMN srv_node_id TEXT NOT NULL DEFAULT ''`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	if err := s.ensureLimits(ctx); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `ALTER TABLE cloud_agent_limits ADD COLUMN srv_id TEXT`); err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate column") {
		return err
	}
	return nil
}

func newID(prefix string) string {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return prefix + formatTS(time.Now())
	}
	return prefix + hex.EncodeToString(buf[:])
}

// FeatureEnabled reports the global switch. A missing global row is off.
// A department or user row does not change this result.
func (s *Service) FeatureEnabled(ctx context.Context, tenantID string) (bool, error) {
	_, _, enabled, err := s.EffectiveLimits(ctx, tenantID, "")
	return enabled, err
}

// AdminView returns the tenant card values. Missing numbers use the defaults.
func (s *Service) AdminView(ctx context.Context, tenantID string) (AdminView, error) {
	instances, turns, enabled, err := s.EffectiveLimits(ctx, tenantID, "")
	if err != nil {
		return AdminView{}, err
	}
	view := AdminView{CloudAgentsEnabled: enabled, MaxInstances: instances, MaxConcurrentTurns: turns}
	pin, _, pinErr := s.ScopeSrvPin(ctx, tenantID, "")
	if pinErr == nil && pin != nil {
		view.SrvID = *pin
	}
	return view, nil
}

// SaveAdminSettings writes the tenant default row. A nil SrvID keeps the
// stored global pin; a set value replaces it.
func (s *Service) SaveAdminSettings(ctx context.Context, tenantID string, in AdminSettings) (AdminView, error) {
	if s == nil || s.db == nil {
		return AdminView{}, fail(503, "UNAVAILABLE", "云端管理暂时不可用")
	}
	if in.CloudAgentsEnabled == nil || in.MaxInstances == nil || in.MaxConcurrentTurns == nil {
		return AdminView{}, failInvalid("请同时填写开关和两个上限")
	}
	if *in.MaxInstances < 0 || *in.MaxConcurrentTurns < 0 {
		return AdminView{}, failInvalid("上限不能是负数")
	}
	var srv sql.NullString
	if in.SrvID != nil {
		pin := strings.TrimSpace(*in.SrvID)
		srv = sql.NullString{String: pin, Valid: true}
		if pin != "" {
			if err := s.checkSrvPin(ctx, tenantID, pin); err != nil {
				return AdminView{}, err
			}
		}
	}
	tenantID = store.NormalizeTenantID(tenantID)
	enabled := 0
	if *in.CloudAgentsEnabled {
		enabled = 1
	}
	srvArg := nullSrv(srv)
	if in.SrvID == nil {
		// Keep the stored pin: the UPDATE never touches srv_id.
		_, err := s.db.ExecContext(ctx, `INSERT INTO cloud_agent_limits (tenant_id, scope, target_id, max_instances, max_concurrent_turns, enabled, srv_id, updated_at)
			VALUES (?, 'global', '', ?, ?, ?, NULL, ?)
			ON CONFLICT(tenant_id, scope, target_id) DO UPDATE SET
				max_instances = excluded.max_instances,
				max_concurrent_turns = excluded.max_concurrent_turns,
				enabled = excluded.enabled,
				updated_at = excluded.updated_at`,
			tenantID, *in.MaxInstances, *in.MaxConcurrentTurns, enabled, formatTS(s.now()))
		if err != nil {
			return AdminView{}, err
		}
	} else {
		_, err := s.db.ExecContext(ctx, `INSERT INTO cloud_agent_limits (tenant_id, scope, target_id, max_instances, max_concurrent_turns, enabled, srv_id, updated_at)
			VALUES (?, 'global', '', ?, ?, ?, ?, ?)
			ON CONFLICT(tenant_id, scope, target_id) DO UPDATE SET
				max_instances = excluded.max_instances,
				max_concurrent_turns = excluded.max_concurrent_turns,
				enabled = excluded.enabled,
				srv_id = excluded.srv_id,
				updated_at = excluded.updated_at`,
			tenantID, *in.MaxInstances, *in.MaxConcurrentTurns, enabled, srvArg, formatTS(s.now()))
		if err != nil {
			return AdminView{}, err
		}
	}
	return s.AdminView(ctx, tenantID)
}

// SaveUserLimits writes one user's numeric override. Null fields inherit.
// It does not write the switch: enabled stays whatever the scope row already
// says, and a new row inherits.
func (s *Service) SaveUserLimits(ctx context.Context, tenantID, userID string, in UserLimits) error {
	if s == nil || s.db == nil {
		return fail(503, "UNAVAILABLE", "云端管理暂时不可用")
	}
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return failInvalid("缺少用户")
	}
	if in.MaxInstances != nil && *in.MaxInstances < 0 {
		return failInvalid("上限不能是负数")
	}
	if in.MaxConcurrentTurns != nil && *in.MaxConcurrentTurns < 0 {
		return failInvalid("上限不能是负数")
	}
	tenantID = store.NormalizeTenantID(tenantID)
	_, err := s.db.ExecContext(ctx, `INSERT INTO cloud_agent_limits (tenant_id, scope, target_id, max_instances, max_concurrent_turns, enabled, updated_at)
		VALUES (?, 'user', ?, ?, ?, NULL, ?)
		ON CONFLICT(tenant_id, scope, target_id) DO UPDATE SET
			max_instances = excluded.max_instances,
			max_concurrent_turns = excluded.max_concurrent_turns,
			updated_at = excluded.updated_at`,
		tenantID, userID, nullInt(in.MaxInstances), nullInt(in.MaxConcurrentTurns), formatTS(s.now()))
	return err
}

func nullInt(v *int) any {
	if v == nil {
		return nil
	}
	return *v
}

// Create inserts one instance. Kind cannot be changed later. The feature
// switch gates creation. A runtime miss still keeps the row as provision_failed.
func (s *Service) Create(ctx context.Context, tenantID, userID string, in CreateInput) (Instance, error) {
	if s == nil || s.db == nil {
		return Instance{}, fail(503, "UNAVAILABLE", "云端管理暂时不可用")
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	name := strings.TrimSpace(in.Name)
	kind := strings.TrimSpace(in.Kind)
	mode := strings.TrimSpace(in.LLMMode)
	if mode == "" {
		mode = LLMOfficial
	}
	if userID == "" {
		return Instance{}, failInvalid("缺少用户")
	}
	if name == "" || len([]rune(name)) > 80 {
		return Instance{}, failInvalid("请填写实例名称")
	}
	if kind != KindSession && kind != KindCoding {
		return Instance{}, failInvalid("请选择会话或编程")
	}
	if mode != LLMOfficial {
		return Instance{}, failInvalid("新建实例使用官方模型，自定义模型请在创建后设置")
	}
	instances, _, enabled, err := s.EffectiveLimits(ctx, tenantID, userID)
	if err != nil {
		return Instance{}, err
	}
	if !enabled {
		return Instance{}, failDisabled()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Instance{}, err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_agents WHERE tenant_id = ? AND owner_user_id = ? AND deleted_at = ''`, tenantID, userID).Scan(&n); err != nil {
		return Instance{}, err
	}
	if instances <= 0 || n >= instances {
		return Instance{}, failQuotaInstances()
	}
	now := formatTS(s.now())
	inst := Instance{
		ID:              newID("cag_"),
		TenantID:        tenantID,
		OwnerUserID:     userID,
		Name:            name,
		Kind:            kind,
		LLMMode:         mode,
		LLMThinkingMode: normalizeThinkingLevel(in.ThinkingMode),
		Status:          StatusProvisioning,
		CloudWorkspaceID: "",
		CreatedAt:       s.now(),
		UpdatedAt:       s.now(),
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO cloud_agents (
		id, tenant_id, owner_user_id, name, kind, llm_mode, llm_label, llm_model, status, cloud_workspace_id, created_at, updated_at, deleted_at
	) VALUES (?, ?, ?, ?, ?, ?, '', '', ?, '', ?, ?, '')`,
		inst.ID, tenantID, userID, name, kind, mode, StatusProvisioning, now, now)
	if err != nil {
		return Instance{}, err
	}
	if err := tx.Commit(); err != nil {
		return Instance{}, err
	}
	if err := s.disp.provision(ctx, &inst); err != nil {
		log.Printf("[cloud-agent] provision %s: %s", inst.ID, logDetail(err))
		_ = s.setStatus(ctx, inst.ID, StatusProvisionFailed)
		inst.Status = StatusProvisionFailed
	} else {
		_ = s.setStatus(ctx, inst.ID, StatusIdle)
		inst.Status = StatusIdle
	}
	inst.RuntimeStatus = s.runtimeStatus(ctx, inst)
	return inst, nil
}

func (s *Service) setStatus(ctx context.Context, id, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cloud_agents SET status = ?, updated_at = ? WHERE id = ? AND deleted_at = ''`,
		status, formatTS(s.now()), id)
	return err
}

// MarkProvisionFailed records that maclawsrv could not be used for this instance.
func (s *Service) MarkProvisionFailed(ctx context.Context, id string) error {
	if s == nil {
		return nil
	}
	return s.setStatus(ctx, id, StatusProvisionFailed)
}

func (s *Service) runtimeStatus(ctx context.Context, inst Instance) string {
	rec, err := s.srvRecord(ctx, inst.TenantID)
	if err != nil {
		return "unreachable"
	}
	return s.runtimeStatusFor(ctx, rec, inst.SrvNodeID)
}

func (s *Service) runtimeStatusFor(ctx context.Context, rec srvRecord, srvNodeID string) string {
	var conn srvConn
	var err error
	if srvNodeID == "" {
		conn, err = rec.publicConn()
	} else {
		// Recorded ids are registered srv links or edge node links;
		// linkConn resolves both.
		conn, err = rec.linkConn(srvNodeID)
	}
	if err != nil {
		return "unreachable"
	}
	if conn.NodeID != "" {
		if s.nodeSrvReachable(ctx, conn) {
			return "ready"
		}
		return "unreachable"
	}
	if !dialReachable(ctx, conn.BaseURL) {
		return "unreachable"
	}
	return "ready"
}

// nodeSrvReachable pings the node srv over the edge tunnel. A missing
// tunnel client or any request failure counts as unreachable.
func (s *Service) nodeSrvReachable(ctx context.Context, conn srvConn) bool {
	if s.NodeHTTP == nil {
		return false
	}
	client := s.NodeHTTP(conn.NodeID)
	if client == nil {
		return false
	}
	ping, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ping, http.MethodGet, conn.BaseURL+"/health", nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	_ = resp.Body.Close()
	return resp.StatusCode >= 200 && resp.StatusCode < 500
}

// List returns the caller's live instances. Stale turns are interrupted first.
func (s *Service) List(ctx context.Context, tenantID, userID string) ([]Instance, error) {
	if s == nil || s.db == nil {
		return nil, fail(503, "UNAVAILABLE", "云端管理暂时不可用")
	}
	_ = s.InterruptStale(ctx, s.now())
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	rows, err := s.db.QueryContext(ctx, instanceSelect+` WHERE tenant_id = ? AND owner_user_id = ? AND deleted_at = '' ORDER BY created_at ASC`, tenantID, userID)
	if err != nil {
		return nil, err
	}
	var out []Instance
	for rows.Next() {
		inst, err := scanInstance(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out = append(out, inst)
	}
	scanErr := rows.Err()
	_ = rows.Close()
	if scanErr != nil {
		return nil, scanErr
	}
	if out == nil {
		out = []Instance{}
	}
	// Close the list query before the settings read. Tests use one connection.
	// Statuses cache by srv binding so same-srv rows share one probe.
	rec, recErr := s.srvRecord(ctx, tenantID)
	statuses := map[string]string{}
	for i := range out {
		if recErr != nil {
			out[i].RuntimeStatus = "unreachable"
			continue
		}
		st, ok := statuses[out[i].SrvNodeID]
		if !ok {
			st = s.runtimeStatusFor(ctx, rec, out[i].SrvNodeID)
			statuses[out[i].SrvNodeID] = st
		}
		out[i].RuntimeStatus = st
	}
	return out, nil
}

// Get returns one live instance owned by the caller, including a coding git view.
func (s *Service) Get(ctx context.Context, tenantID, userID, id string) (Instance, error) {
	inst, err := s.getOwned(ctx, tenantID, userID, id)
	if err != nil {
		return Instance{}, err
	}
	inst.RuntimeStatus = s.runtimeStatus(ctx, inst)
	if inst.Kind == KindCoding {
		view := s.gitPublic(ctx, inst)
		inst.Git = &view
	}
	return inst, nil
}

func (s *Service) getOwned(ctx context.Context, tenantID, userID, id string) (Instance, error) {
	if s == nil || s.db == nil {
		return Instance{}, fail(503, "UNAVAILABLE", "云端管理暂时不可用")
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	id = strings.TrimSpace(id)
	row := s.db.QueryRowContext(ctx, instanceSelect+` WHERE id = ? AND tenant_id = ? AND owner_user_id = ? AND deleted_at = ''`, id, tenantID, userID)
	inst, err := scanInstance(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Instance{}, failNotFound()
	}
	return inst, err
}

// setSrvNode records which srv an instance was provisioned on. Instance
// calls keep routing there even when the owner's pin moves.
func (s *Service) setSrvNode(ctx context.Context, id, nodeID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cloud_agents SET srv_node_id = ? WHERE id = ?`, strings.TrimSpace(nodeID), strings.TrimSpace(id))
	return err
}

func (s *Service) getByID(ctx context.Context, id string) (Instance, error) {
	row := s.db.QueryRowContext(ctx, instanceSelect+` WHERE id = ? AND deleted_at = ''`, strings.TrimSpace(id))
	inst, err := scanInstance(row)
	if errors.Is(err, sql.ErrNoRows) {
		return Instance{}, failNotFound()
	}
	return inst, err
}

// InstanceOwner is the tenant, user and srv binding of one live instance.
// The official LLM route uses this so the caller cannot name a different user.
func (s *Service) InstanceOwner(ctx context.Context, id string) (tenantID, userID, srvNodeID string, err error) {
	inst, err := s.getByID(ctx, id)
	if err != nil {
		return "", "", "", err
	}
	return inst.TenantID, inst.OwnerUserID, inst.SrvNodeID, nil
}

// Patch renames an instance or changes its saved LLM label. A different kind is rejected and nothing is written.
func (s *Service) Patch(ctx context.Context, tenantID, userID, id string, in PatchInput) (Instance, error) {
	inst, err := s.getOwned(ctx, tenantID, userID, id)
	if err != nil {
		return Instance{}, err
	}
	if in.Kind != nil && strings.TrimSpace(*in.Kind) != inst.Kind {
		return Instance{}, failKind()
	}
	name := inst.Name
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
		if name == "" || len([]rune(name)) > 80 {
			return Instance{}, failInvalid("请填写实例名称")
		}
	}
	mode := inst.LLMMode
	if in.LLMMode != nil {
		mode = strings.TrimSpace(*in.LLMMode)
		if mode == LLMCustom {
			return Instance{}, failInvalid("自定义模型请通过模型接口保存")
		}
		if mode != LLMOfficial {
			return Instance{}, failInvalid("LLM 模式无效")
		}
	}
	// Switching back to official must reach maclawsrv: its llm.json overrides
	// the mode, so a Hub-only write would show official while turns still run
	// on the custom endpoint.
	if mode == LLMOfficial && inst.LLMMode == LLMCustom {
		conn, err := s.srvConnForNode(ctx, inst.TenantID, inst.SrvNodeID)
		if err != nil {
			return Instance{}, failOffline()
		}
		if _, err := s.disp.call(ctx, http.MethodDelete, conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID)+"/llm", nil); err != nil {
			return Instance{}, failOffline()
		}
	}
	label := inst.LLMLabel
	if in.LLMLabel != nil {
		label = strings.TrimSpace(*in.LLMLabel)
	}
	model := inst.LLMModel
	if in.LLMModel != nil {
		model = strings.TrimSpace(*in.LLMModel)
	}
	_, err = s.db.ExecContext(ctx, `UPDATE cloud_agents SET name = ?, llm_mode = ?, llm_label = ?, llm_model = ?, updated_at = ? WHERE id = ? AND deleted_at = ''`,
		name, mode, label, model, formatTS(s.now()), inst.ID)
	if err != nil {
		return Instance{}, err
	}
	return s.Get(ctx, inst.TenantID, inst.OwnerUserID, inst.ID)
}

// Delete soft-deletes the Hub row and asks maclawsrv to tombstone the directory.
func (s *Service) Delete(ctx context.Context, tenantID, userID, id string) error {
	inst, err := s.getOwned(ctx, tenantID, userID, id)
	if err != nil {
		return err
	}
	now := formatTS(s.now())
	_, err = s.db.ExecContext(ctx, `UPDATE cloud_agents SET status = ?, deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at = ''`,
		StatusDeleted, now, now, inst.ID)
	if err != nil {
		return err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE cloud_agent_turns SET status = ?, finished_at = ?, updated_at = ? WHERE instance_id = ? AND status = ?`,
		TurnInterrupted, now, now, inst.ID, TurnRunning)
	if conn, err := s.srvConnForNode(ctx, inst.TenantID, inst.SrvNodeID); err == nil {
		if _, err := s.disp.call(ctx, "DELETE", conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID), nil); err != nil {
			log.Printf("[cloud-agent] delete %s on runtime: %s", inst.ID, logDetail(err))
		}
	}
	return nil
}

// Stop hard-stops the instance. The workspace is left in place.
func (s *Service) Stop(ctx context.Context, tenantID, userID, id string) (Instance, error) {
	inst, err := s.getOwned(ctx, tenantID, userID, id)
	if err != nil {
		return Instance{}, err
	}
	now := formatTS(s.now())
	_, err = s.db.ExecContext(ctx, `UPDATE cloud_agents SET status = ?, updated_at = ? WHERE id = ? AND deleted_at = ''`, StatusStopped, now, inst.ID)
	if err != nil {
		return Instance{}, err
	}
	_, _ = s.db.ExecContext(ctx, `UPDATE cloud_agent_turns SET status = ?, finished_at = ?, updated_at = ? WHERE instance_id = ? AND status = ?`,
		TurnCancelled, now, now, inst.ID, TurnRunning)
	if conn, err := s.srvConnForNode(ctx, inst.TenantID, inst.SrvNodeID); err == nil {
		if _, err := s.disp.call(ctx, "POST", conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID)+"/stop", map[string]any{"owner_user_id": inst.OwnerUserID}); err != nil {
			log.Printf("[cloud-agent] stop %s on runtime: %s", inst.ID, logDetail(err))
		}
	}
	return s.Get(ctx, inst.TenantID, inst.OwnerUserID, inst.ID)
}

// Resume moves a stopped instance back to idle.
func (s *Service) Resume(ctx context.Context, tenantID, userID, id string) (Instance, error) {
	inst, err := s.getOwned(ctx, tenantID, userID, id)
	if err != nil {
		return Instance{}, err
	}
	if inst.Status != StatusStopped {
		return Instance{}, failInvalid("实例不在停止状态")
	}
	if err := s.setStatus(ctx, inst.ID, StatusIdle); err != nil {
		return Instance{}, err
	}
	if conn, err := s.srvConnForNode(ctx, inst.TenantID, inst.SrvNodeID); err == nil {
		if _, err := s.disp.call(ctx, "POST", conn, "/api/v1/cloud-agents/"+url.PathEscape(inst.ID)+"/resume", map[string]any{"owner_user_id": inst.OwnerUserID}); err != nil {
			log.Printf("[cloud-agent] resume %s on runtime: %s", inst.ID, logDetail(err))
		}
	}
	return s.Get(ctx, inst.TenantID, inst.OwnerUserID, inst.ID)
}

const instanceSelect = `SELECT id, tenant_id, owner_user_id, name, kind, llm_mode, llm_label, llm_model, status, cloud_workspace_id, srv_node_id, created_at, updated_at, deleted_at FROM cloud_agents`

type scanner interface {
	Scan(dest ...any) error
}

func scanInstance(row scanner) (Instance, error) {
	var inst Instance
	var created, updated, deleted string
	err := row.Scan(
		&inst.ID, &inst.TenantID, &inst.OwnerUserID, &inst.Name, &inst.Kind,
		&inst.LLMMode, &inst.LLMLabel, &inst.LLMModel, &inst.Status, &inst.CloudWorkspaceID,
		&inst.SrvNodeID, &created, &updated, &deleted,
	)
	if err != nil {
		return Instance{}, err
	}
	inst.CreatedAt = parseTS(created)
	inst.UpdatedAt = parseTS(updated)
	inst.DeletedAt = parseTS(deleted)
	return inst, nil
}

func (s *Service) instanceBusy(ctx context.Context, instanceID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_agent_turns WHERE instance_id = ? AND status = ?`, instanceID, TurnRunning).Scan(&n)
	return n > 0, err
}

// ensureTurnRoom rejects a send that would exceed the user cap or the one-turn-per-instance rule.
func (s *Service) ensureTurnRoom(ctx context.Context, tenantID, userID, instanceID string) error {
	_, turns, enabled, err := s.EffectiveLimits(ctx, tenantID, userID)
	if err != nil {
		return err
	}
	if !enabled {
		return failDisabled()
	}
	if turns <= 0 {
		return failQuotaTurns()
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_agent_turns WHERE tenant_id = ? AND owner_user_id = ? AND status = ?`,
		store.NormalizeTenantID(tenantID), userID, TurnRunning).Scan(&n); err != nil {
		return err
	}
	if n >= turns {
		return failQuotaTurns()
	}
	var own int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM cloud_agent_turns WHERE instance_id = ? AND status = ?`, instanceID, TurnRunning).Scan(&own); err != nil {
		return err
	}
	if own > 0 {
		return fail(409, "BUSY", "这个实例正在跑一轮")
	}
	return nil
}

// NoteAcceptedTurn records a turn only after maclawsrv has accepted it.
// The same request id does not open a second row.
func (s *Service) NoteAcceptedTurn(ctx context.Context, tenantID, userID, instanceID, taskID, requestID string) error {
	if s == nil || strings.TrimSpace(requestID) == "" || strings.TrimSpace(taskID) == "" {
		return nil
	}
	tenantID = store.NormalizeTenantID(tenantID)
	instanceID = strings.TrimSpace(instanceID)
	requestID = strings.TrimSpace(requestID)
	now := formatTS(s.now())
	var existing string
	err := s.db.QueryRowContext(ctx, `SELECT id FROM cloud_agent_turns WHERE instance_id = ? AND request_id = ?`, instanceID, requestID).Scan(&existing)
	inserted := int64(0)
	if errors.Is(err, sql.ErrNoRows) {
		var res sql.Result
		res, err = s.db.ExecContext(ctx, `INSERT INTO cloud_agent_turns (
			id, tenant_id, owner_user_id, instance_id, task_id, request_id, status, started_at, updated_at, finished_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '')
		ON CONFLICT(instance_id, request_id) DO NOTHING`,
			newID("ctu_"), tenantID, userID, instanceID, taskID, requestID, TurnRunning, now, now)
		if err != nil {
			return err
		}
		inserted, _ = res.RowsAffected()
	} else if err != nil {
		return err
	}
	// A new request is accepted only when this instance has no running turn.
	// Retire older rows even when the finish notice arrived before this accept.
	if err := s.retireOtherRunning(ctx, instanceID, requestID, now); err != nil {
		return err
	}
	if inserted > 0 {
		return s.setStatus(ctx, instanceID, StatusRunning)
	}
	return s.releaseIdle(ctx, instanceID, now)
}

// CancelTurns settles exactly the request ids maclawsrv reported for a
// cloud_stop_task. The response carries ids rather than a task-wide update so
// a queued row that starts immediately after the stop cannot be cancelled in
// Hub shadow state by accident.
func (s *Service) CancelTurns(ctx context.Context, instanceID string, requestIDs []string) error {
	if s == nil || s.db == nil {
		return nil
	}
	instanceID = strings.TrimSpace(instanceID)
	if instanceID == "" {
		return nil
	}
	stamp := formatTS(s.now())
	for _, requestID := range requestIDs {
		requestID = strings.TrimSpace(requestID)
		if requestID == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE cloud_agent_turns SET status = ?, finished_at = ?, updated_at = ? WHERE instance_id = ? AND request_id = ? AND status = ?`,
			TurnCancelled, stamp, stamp, instanceID, requestID, TurnRunning); err != nil {
			return err
		}
	}
	return s.releaseIdle(ctx, instanceID, stamp)
}

func (s *Service) retireOtherRunning(ctx context.Context, instanceID, requestID, stamp string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cloud_agent_turns SET status = ?, finished_at = ?, updated_at = ? WHERE instance_id = ? AND status = ? AND request_id <> ?`,
		TurnFinished, stamp, stamp, instanceID, TurnRunning, requestID)
	return err
}

// settleRunning finishes every running turn of one instance. The runtime
// already confirmed nothing is running — these rows are shadow state left by
// a lost finish beat.
func (s *Service) settleRunning(ctx context.Context, instanceID, stamp string) error {
	if _, err := s.db.ExecContext(ctx, `UPDATE cloud_agent_turns SET status = ?, finished_at = ?, updated_at = ? WHERE instance_id = ? AND status = ?`,
		TurnFinished, stamp, stamp, instanceID, TurnRunning); err != nil {
		return err
	}
	return s.releaseIdle(ctx, instanceID, stamp)
}

func (s *Service) releaseIdle(ctx context.Context, instanceID, stamp string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE cloud_agents SET status = ?, updated_at = ? WHERE id = ? AND status = ? AND id NOT IN (
		SELECT instance_id FROM cloud_agent_turns WHERE instance_id = ? AND status = ?
	)`, StatusIdle, stamp, instanceID, StatusRunning, instanceID, TurnRunning)
	return err
}

// FinishTurn records that maclawsrv completed this request. A notice that
// arrives before the accept row exists still occupies the request id, so a
// later accept cannot turn the finished request back into a running one.
func (s *Service) FinishTurn(ctx context.Context, instanceID, taskID, requestID string, now time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	instanceID = strings.TrimSpace(instanceID)
	taskID = strings.TrimSpace(taskID)
	requestID = strings.TrimSpace(requestID)
	if instanceID == "" || taskID == "" || requestID == "" {
		return failInvalid("心跳无效")
	}
	inst, err := s.getByID(ctx, instanceID)
	if err != nil {
		return err
	}
	stamp := formatTS(now)
	_, err = s.db.ExecContext(ctx, `INSERT INTO cloud_agent_turns (
		id, tenant_id, owner_user_id, instance_id, task_id, request_id, status, started_at, updated_at, finished_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(instance_id, request_id) DO UPDATE SET
		status = excluded.status,
		finished_at = excluded.finished_at,
		updated_at = excluded.updated_at
	WHERE cloud_agent_turns.status = ?`,
		newID("ctu_"), inst.TenantID, inst.OwnerUserID, inst.ID, taskID, requestID, TurnFinished, stamp, stamp, stamp, TurnRunning)
	if err != nil {
		return err
	}
	return s.releaseIdle(ctx, inst.ID, stamp)
}

// TouchHeartbeat refreshes one running turn and reports whether the feature is still on.
func (s *Service) TouchHeartbeat(ctx context.Context, instanceID, taskID, requestID string, now time.Time) (enabled bool, err error) {
	inst, err := s.getByID(ctx, instanceID)
	if err != nil {
		return false, err
	}
	_, _, enabled, err = s.EffectiveLimits(ctx, inst.TenantID, inst.OwnerUserID)
	if err != nil {
		return false, err
	}
	stamp := formatTS(now)
	if !enabled {
		_, _ = s.db.ExecContext(ctx, `UPDATE cloud_agent_turns SET status = ?, finished_at = ?, updated_at = ? WHERE instance_id = ? AND status = ?`,
			TurnInterrupted, stamp, stamp, inst.ID, TurnRunning)
		if inst.Status == StatusRunning {
			_ = s.setStatus(ctx, inst.ID, StatusIdle)
		}
		return false, nil
	}
	if strings.TrimSpace(taskID) == "" || strings.TrimSpace(requestID) == "" {
		return enabled, nil
	}
	// A turn that was accepted while hub was unreachable can start
	// heartbeating before its accept bookkeeping is ever replayed. The beat
	// is itself proof the turn is running, so upsert the shadow row here;
	// without it the instance remains falsely idle and the control plane may
	// settle/reuse state it does not actually own.
	res, err := s.db.ExecContext(ctx, `INSERT INTO cloud_agent_turns (
		id, tenant_id, owner_user_id, instance_id, task_id, request_id, status, started_at, updated_at, finished_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, '')
	ON CONFLICT(instance_id, request_id) DO UPDATE SET
		task_id = excluded.task_id,
		updated_at = excluded.updated_at
	WHERE cloud_agent_turns.status = ?`,
		newID("ctu_"), inst.TenantID, inst.OwnerUserID, inst.ID, taskID, requestID, TurnRunning, stamp, stamp, TurnRunning)
	if err != nil {
		return enabled, err
	}
	if rows, _ := res.RowsAffected(); rows > 0 {
		// srv runs one turn per instance. If this beat is the first evidence
		// hub has for that turn, older running rows are necessarily shadow
		// state from a lost finish beat.
		if err := s.retireOtherRunning(ctx, inst.ID, requestID, stamp); err != nil {
			return enabled, err
		}
		_ = s.setStatus(ctx, inst.ID, StatusRunning)
	}
	return enabled, nil
}

// InterruptStale marks running turns with no heartbeat inside the miss window,
// and turns whose owner is outside the opening scope.
func (s *Service) InterruptStale(ctx context.Context, now time.Time) error {
	if s == nil || s.db == nil {
		return nil
	}
	miss := s.miss
	if miss <= 0 {
		miss = HeartbeatMiss
	}
	cutoff := formatTS(now.Add(-miss))
	stamp := formatTS(now)
	if _, err := s.db.ExecContext(ctx, `UPDATE cloud_agent_turns SET status = ?, finished_at = ?, updated_at = ? WHERE status = ? AND updated_at < ? AND updated_at <> ''`,
		TurnInterrupted, stamp, stamp, TurnRunning, cutoff); err != nil {
		return err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT tenant_id, owner_user_id FROM cloud_agent_turns WHERE status = ?`, TurnRunning)
	if err != nil {
		return err
	}
	type ownerRef struct {
		tenant string
		owner  string
	}
	var owners []ownerRef
	for rows.Next() {
		var owner ownerRef
		if err := rows.Scan(&owner.tenant, &owner.owner); err != nil {
			_ = rows.Close()
			return err
		}
		owners = append(owners, owner)
	}
	scanErr := rows.Err()
	_ = rows.Close()
	if scanErr != nil {
		return scanErr
	}
	for _, owner := range owners {
		_, _, enabled, err := s.EffectiveLimits(ctx, owner.tenant, owner.owner)
		if err != nil {
			return err
		}
		if enabled {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `UPDATE cloud_agent_turns SET status = ?, finished_at = ?, updated_at = ? WHERE tenant_id = ? AND owner_user_id = ? AND status = ?`,
			TurnInterrupted, stamp, stamp, owner.tenant, owner.owner, TurnRunning); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `UPDATE cloud_agents SET status = ?, updated_at = ? WHERE status = ? AND deleted_at = '' AND id NOT IN (
		SELECT instance_id FROM cloud_agent_turns WHERE status = ?
	)`, StatusIdle, stamp, StatusRunning, TurnRunning)
	return err
}

// AdminSecretMatches compares the header with the admin secret of the
// maclawsrv this instance's calls route to: the node srv recorded on the
// instance, or the public connection when srvNodeID is empty.
func (s *Service) AdminSecretMatches(ctx context.Context, tenantID, srvNodeID, got string) bool {
	conn, err := s.srvConnForNode(ctx, tenantID, srvNodeID)
	if err != nil {
		return false
	}
	want := conn.AdminSecret
	if want == "" || got == "" || len(want) != len(got) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

// SrvConnIsNode reports whether srvNodeID routes to a node srv reached over
// the edge tunnel. False on any resolution error.
func (s *Service) SrvConnIsNode(ctx context.Context, tenantID, srvNodeID string) bool {
	conn, err := s.srvConnForNode(ctx, tenantID, srvNodeID)
	return err == nil && conn.NodeID != ""
}

// SrvAuth resolves an instance's srv binding once and answers both auth
// questions the internal callbacks need: isNode says whether the conn is
// a non-public srv (node or registered — non-loopback callers allowed),
// authed says the presented admin secret matches that conn.
func (s *Service) SrvAuth(ctx context.Context, tenantID, srvNodeID, got string) (isNode, authed bool) {
	conn, err := s.srvConnForNode(ctx, tenantID, srvNodeID)
	if err != nil {
		return false, false
	}
	want := conn.AdminSecret
	return conn.LinkID != "", want != "" && got != "" && len(want) == len(got) &&
		subtle.ConstantTimeCompare([]byte(want), []byte(got)) == 1
}

type srvConn struct {
	BaseURL     string
	AdminSecret string
	// LinkID is the registry id recorded on provisioned instances
	// (srv_node_id). Empty is the public connection; a node link uses its
	// node id and a registered srv link its own id.
	LinkID string
	// NodeID is set when this conn is a private node srv reached through the
	// edge tunnel. Its BaseURL is a virtual srv.<id>.node.invalid host that
	// only resolves through the NodeHTTP client. A registered srv link has
	// a LinkID but no NodeID: it dials its own base_url directly.
	NodeID string
}

// srvRecord is the parsed maclawsrv connection document: the public
// connection plus every node link, every registered srv link, and the
// per-owner cloud pins.
type srvRecord struct {
	baseURL        string
	adminSecret    string
	ownerCloudNode map[string]string
	nodeLinks      map[string]srvConn
	srvLinks       map[string]srvConn
}

// publicConn is the system connection registered on the hub settings
// record — the address every unpinned cloud agent uses. It may be the
// co-located srv's loopback address or a remote srv's http(s) address:
// the admin saved it on the same record that carries the admin secret,
// so it is dialed with that secret exactly like a registered srv link.
func (r srvRecord) publicConn() (srvConn, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(r.baseURL), "/")
	if !dialableBaseURL(baseURL) {
		return srvConn{}, failOfflineDetail("no usable maclawsrv address on the record: " + r.baseURL)
	}
	if strings.TrimSpace(r.adminSecret) == "" {
		return srvConn{}, failOfflineDetail("admin_secret is not configured")
	}
	return srvConn{BaseURL: baseURL, AdminSecret: r.adminSecret}, nil
}

// nodeConn is one node's srv link. A missing or incomplete link fails
// closed rather than silently falling back to the public connection.
func (r srvRecord) nodeConn(nodeID string) (srvConn, error) {
	conn, ok := r.nodeLinks[nodeID]
	if !ok || strings.TrimSpace(conn.BaseURL) == "" || strings.TrimSpace(conn.AdminSecret) == "" {
		return srvConn{}, failOfflineDetail("no usable node link for " + nodeID)
	}
	return conn, nil
}

// defaultConn resolves the tenant's system-default srv: when exactly one
// endpoint is usable — the public connection counts alongside registered
// srv links — it is the default; otherwise the public connection is.
func (r srvRecord) defaultConn() (srvConn, error) {
	var only srvConn
	count := 0
	if conn, err := r.publicConn(); err == nil {
		only, count = conn, count+1
	}
	for _, conn := range r.srvLinks {
		if strings.TrimSpace(conn.BaseURL) == "" || strings.TrimSpace(conn.AdminSecret) == "" {
			continue
		}
		only, count = conn, count+1
	}
	if count == 1 {
		return only, nil
	}
	return r.publicConn()
}

// pinConn resolves a scope pin: an empty string asks for the effective
// system default (defaultConn), a named id resolves as linkConn.
func (r srvRecord) pinConn(pin string) (srvConn, error) {
	if strings.TrimSpace(pin) == "" {
		return r.defaultConn()
	}
	return r.linkConn(pin)
}

// linkConn resolves one srv id: an empty string is the public connection,
// then a registered srv link, then a node srv link. A named-but-missing
// link fails closed.
func (r srvRecord) linkConn(pin string) (srvConn, error) {
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return r.publicConn()
	}
	if conn, ok := r.srvLinks[pin]; ok {
		if strings.TrimSpace(conn.BaseURL) == "" || strings.TrimSpace(conn.AdminSecret) == "" {
			return srvConn{}, failOfflineDetail("no usable srv link for " + pin)
		}
		return conn, nil
	}
	return r.nodeConn(pin)
}

func (s *Service) srvRecord(ctx context.Context, tenantID string) (srvRecord, error) {
	rec := srvRecord{ownerCloudNode: map[string]string{}, nodeLinks: map[string]srvConn{}, srvLinks: map[string]srvConn{}}
	if s == nil || s.settings == nil {
		return rec, failOfflineDetail("settings store unavailable")
	}
	raw, err := s.settings.Get(ctx, SrvSettingsKey(tenantID))
	if err != nil {
		return rec, failOfflineDetail("srv settings read: " + err.Error())
	}
	if strings.TrimSpace(raw) == "" {
		return rec, failOfflineDetail("no maclawsrv connection is configured for tenant " + tenantID)
	}
	var doc struct {
		BaseURL        string            `json:"base_url"`
		AdminSecret    string            `json:"admin_secret"`
		DialNode       string            `json:"dial_node"`
		OwnerCloudNode map[string]string `json:"owner_cloud_node"`
		NodeLinks      []struct {
			NodeID      string `json:"node_id"`
			BaseURL     string `json:"base_url"`
			AdminSecret string `json:"admin_secret"`
		} `json:"node_links"`
		SrvLinks []struct {
			ID          string `json:"id"`
			BaseURL     string `json:"base_url"`
			AdminSecret string `json:"admin_secret"`
		} `json:"srv_links"`
	}
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return rec, failOfflineDetail("maclawsrv record is not valid JSON")
	}
	if strings.TrimSpace(doc.DialNode) != "" {
		return rec, failOfflineDetail("maclawsrv record delegates to dial_node")
	}
	rec.baseURL = doc.BaseURL
	rec.adminSecret = doc.AdminSecret
	for user, node := range doc.OwnerCloudNode {
		if id := strings.TrimSpace(node); id != "" {
			rec.ownerCloudNode[user] = id
		}
	}
	for _, link := range doc.NodeLinks {
		if id := strings.TrimSpace(link.NodeID); id != "" {
			rec.nodeLinks[id] = srvConn{
				BaseURL:     strings.TrimRight(strings.TrimSpace(link.BaseURL), "/"),
				AdminSecret: link.AdminSecret,
				LinkID:      id,
				NodeID:      id,
			}
		}
	}
	for _, link := range doc.SrvLinks {
		if id := strings.TrimSpace(link.ID); id != "" {
			rec.srvLinks[id] = srvConn{
				BaseURL:     strings.TrimRight(strings.TrimSpace(link.BaseURL), "/"),
				AdminSecret: link.AdminSecret,
				LinkID:      id,
			}
		}
	}
	return rec, nil
}

// srvConn resolves which maclawsrv a new instance provisions on. The chain
// is: the admin's user-scope pin, then the owner's own private srv pin,
// then the nearest department-scope pin, then the global-scope pin, then
// the system default — the one registered endpoint when there is exactly
// one, else the public connection. A named-but-missing link fails closed.
func (s *Service) srvConn(ctx context.Context, tenantID, ownerID string) (srvConn, error) {
	rec, err := s.srvRecord(ctx, tenantID)
	if err != nil {
		return srvConn{}, err
	}
	pin, userScoped, err := s.ScopeSrvPin(ctx, tenantID, ownerID)
	if err != nil {
		return srvConn{}, err
	}
	if pin != nil && userScoped {
		return rec.pinConn(*pin)
	}
	if ownerID != "" {
		if nodeID := rec.ownerCloudNode[ownerID]; nodeID != "" {
			return rec.nodeConn(nodeID)
		}
	}
	if pin != nil {
		return rec.pinConn(*pin)
	}
	return rec.defaultConn()
}

// srvConnForNode resolves the conn an existing instance runs on: the srv
// link recorded on it at provision — a node srv or a registered srv — or
// the public connection when empty. An empty id stays the literal public
// connection even if the effective default has since become a registered
// link: changing pins does not move live instances.
func (s *Service) srvConnForNode(ctx context.Context, tenantID, srvNodeID string) (srvConn, error) {
	rec, err := s.srvRecord(ctx, tenantID)
	if err != nil {
		return srvConn{}, err
	}
	if strings.TrimSpace(srvNodeID) == "" {
		return rec.publicConn()
	}
	return rec.linkConn(srvNodeID)
}
