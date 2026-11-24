package cloudagent

import (
	"context"
	"database/sql"
	"strings"

	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	ScopeGlobal     = "global"
	ScopeDepartment = "department"
	ScopeUser       = "user"

	AccessInherit = "inherit"
	AccessOn      = "on"
	AccessOff     = "off"

	limitsDDL = `CREATE TABLE IF NOT EXISTS cloud_agent_limits (
		tenant_id TEXT NOT NULL,
		scope TEXT NOT NULL,
		target_id TEXT NOT NULL DEFAULT '',
		max_instances INTEGER,
		max_concurrent_turns INTEGER,
		enabled INTEGER,
		srv_id TEXT,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (tenant_id, scope, target_id)
	)`
)

// Directory resolves one user's department chain. A nil directory skips that
// step. The concrete value is the security directory the desktop pool already
// holds. This package does not call botmgmt.
type Directory interface {
	Email(ctx context.Context, userID string) (string, error)
	GroupID(ctx context.Context, email string) (string, error)
	ParentID(ctx context.Context, groupID string) (string, error)
}

// AccessView is one opening-scope row. Enabled is inherit, on, or off.
// A null limit inherits the next level. Zero is a real cap. SrvID picks
// which maclawsrv this scope's agents provision on: null inherits, an
// empty string explicitly names the system default, anything else is a
// registered srv link or node srv id.
type AccessView struct {
	Scope              string  `json:"scope"`
	TargetID           string  `json:"target_id"`
	Enabled            string  `json:"enabled"`
	MaxInstances       *int    `json:"max_instances"`
	MaxConcurrentTurns *int    `json:"max_concurrent_turns"`
	SrvID              *string `json:"srv_id"`
}

type limitSlot struct {
	enabled   sql.NullInt64
	instances sql.NullInt64
	turns     sql.NullInt64
	srv       sql.NullString
}

func limitKey(scope, target string) string {
	return scope + "\n" + target
}

func (s *Service) ensureLimits(ctx context.Context) error {
	cols, err := s.limitColumns(ctx)
	if err != nil {
		return err
	}
	_, hasUser := cols["user_id"]
	_, hasScope := cols["scope"]
	if hasUser && !hasScope {
		return s.migrateLimits(ctx)
	}
	if len(cols) == 0 {
		_, err = s.db.ExecContext(ctx, limitsDDL)
	}
	return err
}

func (s *Service) limitColumns(ctx context.Context) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `PRAGMA table_info(cloud_agent_limits)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]struct{}{}
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return nil, err
		}
		cols[name] = struct{}{}
	}
	return cols, rows.Err()
}

// migrateLimits rebuilds the pre-scope table. A tenant row (user_id empty)
// becomes the global switch. A user row keeps its numbers and inherits the
// switch, so a migration cannot turn the feature on.
func (s *Service) migrateLimits(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	steps := []string{
		`DROP TABLE IF EXISTS cloud_agent_limits_v2`,
		`CREATE TABLE cloud_agent_limits_v2 (
			tenant_id TEXT NOT NULL,
			scope TEXT NOT NULL,
			target_id TEXT NOT NULL DEFAULT '',
			max_instances INTEGER,
			max_concurrent_turns INTEGER,
			enabled INTEGER,
			srv_id TEXT,
			updated_at TEXT NOT NULL,
			PRIMARY KEY (tenant_id, scope, target_id)
		)`,
		`INSERT INTO cloud_agent_limits_v2 (tenant_id, scope, target_id, max_instances, max_concurrent_turns, enabled, updated_at)
			SELECT tenant_id,
				CASE WHEN user_id = '' THEN 'global' ELSE 'user' END,
				CASE WHEN user_id = '' THEN '' ELSE user_id END,
				max_instances,
				max_concurrent_turns,
				CASE WHEN user_id = '' THEN enabled ELSE NULL END,
				updated_at
			FROM cloud_agent_limits`,
		`DROP TABLE cloud_agent_limits`,
		`ALTER TABLE cloud_agent_limits_v2 RENAME TO cloud_agent_limits`,
	}
	for _, stmt := range steps {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Service) loadLimitSlots(ctx context.Context, tenantID string) (map[string]limitSlot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT scope, target_id, max_instances, max_concurrent_turns, enabled, srv_id FROM cloud_agent_limits WHERE tenant_id = ?`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	slots := map[string]limitSlot{}
	for rows.Next() {
		var scope, target string
		var slot limitSlot
		if err := rows.Scan(&scope, &target, &slot.instances, &slot.turns, &slot.enabled, &slot.srv); err != nil {
			return nil, err
		}
		slots[limitKey(scope, target)] = slot
	}
	return slots, rows.Err()
}

// EffectiveLimits walks user, then the department chain from nearest to root,
// then the global row. The first explicit on or off wins. Each limit does the
// same walk on its own; a blank number inherits, and zero is a real cap.
// No explicit switch leaves the feature off. An empty user reads only the
// global row.
func (s *Service) EffectiveLimits(ctx context.Context, tenantID, userID string) (instances, turns int, enabled bool, err error) {
	if s == nil || s.db == nil {
		return 0, 0, false, fail(503, "UNAVAILABLE", "云端管理暂时不可用")
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	slots, err := s.loadLimitSlots(ctx, tenantID)
	if err != nil {
		return 0, 0, false, err
	}
	var departments []string
	if userID != "" {
		departments = s.departmentChain(ctx, tenantID, userID)
	}
	instances, turns, enabled = resolveLimits(slots, userID, departments)
	return instances, turns, enabled, nil
}

// ScopeSrvPin resolves the maclawsrv pin for one account from the scope
// rows only: the user row first, then the department chain nearest to
// root, then the global row. The first non-null srv_id wins. A nil pin
// means every level inherits — the caller then falls back to the owner's
// self-picked private srv or the public connection. An empty string is an
// explicit pin to the system default. userScoped reports whether the pin
// came from the user row, so the caller can place the owner's own private
// srv pin between the user level and the wider scopes.
func (s *Service) ScopeSrvPin(ctx context.Context, tenantID, userID string) (pin *string, userScoped bool, err error) {
	if s == nil || s.db == nil {
		return nil, false, fail(503, "UNAVAILABLE", "云端管理暂时不可用")
	}
	tenantID = store.NormalizeTenantID(tenantID)
	userID = strings.TrimSpace(userID)
	slots, err := s.loadLimitSlots(ctx, tenantID)
	if err != nil {
		return nil, false, err
	}
	if userID != "" {
		if row, ok := slots[limitKey(ScopeUser, userID)]; ok && row.srv.Valid {
			v := row.srv.String
			return &v, true, nil
		}
	}
	var departments []string
	if userID != "" {
		departments = s.departmentChain(ctx, tenantID, userID)
	}
	for _, id := range departments {
		id = strings.TrimSpace(id)
		if row, ok := slots[limitKey(ScopeDepartment, id)]; ok && row.srv.Valid {
			v := row.srv.String
			return &v, false, nil
		}
	}
	if row, ok := slots[limitKey(ScopeGlobal, "")]; ok && row.srv.Valid {
		v := row.srv.String
		return &v, false, nil
	}
	return nil, false, nil
}

func resolveLimits(slots map[string]limitSlot, userID string, departments []string) (instances, turns int, enabled bool) {
	instances = DefaultMaxInstances
	turns = DefaultMaxConcurrentTurns
	var chain []limitSlot
	if userID != "" {
		if row, ok := slots[limitKey(ScopeUser, userID)]; ok {
			chain = append(chain, row)
		}
		for _, id := range departments {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if row, ok := slots[limitKey(ScopeDepartment, id)]; ok {
				chain = append(chain, row)
			}
		}
	}
	if row, ok := slots[limitKey(ScopeGlobal, "")]; ok {
		chain = append(chain, row)
	}
	enabledSet := false
	instanceSet := false
	turnSet := false
	for _, row := range chain {
		if !enabledSet && row.enabled.Valid {
			enabled = row.enabled.Int64 != 0
			enabledSet = true
		}
		if !instanceSet && row.instances.Valid {
			instances = int(row.instances.Int64)
			instanceSet = true
		}
		if !turnSet && row.turns.Valid {
			turns = int(row.turns.Int64)
			turnSet = true
		}
	}
	return instances, turns, enabled
}

// departmentChain walks the user's group to the root. A nil directory, a
// missing email, or a lookup error stops the walk. Depth is capped and a
// cycle stops. The security tenant has to be on the context or the lookup
// reads the default tenant and misses every department row.
func (s *Service) departmentChain(ctx context.Context, tenantID, userID string) []string {
	if s == nil || s.Directory == nil || strings.TrimSpace(userID) == "" {
		return nil
	}
	ctx = security.WithTenant(ctx, tenantID)
	email, err := s.Directory.Email(ctx, userID)
	if err != nil || strings.TrimSpace(email) == "" {
		return nil
	}
	groupID, err := s.Directory.GroupID(ctx, strings.TrimSpace(email))
	if err != nil {
		return nil
	}
	groupID = strings.TrimSpace(groupID)
	var chain []string
	seen := map[string]bool{}
	for groupID != "" && len(chain) < 32 && !seen[groupID] {
		seen[groupID] = true
		chain = append(chain, groupID)
		parent, err := s.Directory.ParentID(ctx, groupID)
		if err != nil {
			break
		}
		groupID = strings.TrimSpace(parent)
	}
	return chain
}

// ListAccess returns every saved scope row for the tenant.
func (s *Service) ListAccess(ctx context.Context, tenantID string) ([]AccessView, error) {
	if s == nil || s.db == nil {
		return nil, fail(503, "UNAVAILABLE", "云端管理暂时不可用")
	}
	tenantID = store.NormalizeTenantID(tenantID)
	rows, err := s.db.QueryContext(ctx, `SELECT scope, target_id, max_instances, max_concurrent_turns, enabled, srv_id
		FROM cloud_agent_limits WHERE tenant_id = ?
		ORDER BY CASE scope WHEN 'global' THEN 0 WHEN 'department' THEN 1 ELSE 2 END, target_id`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []AccessView{}
	for rows.Next() {
		var item AccessView
		var instances, turns, enabled sql.NullInt64
		var srv sql.NullString
		if err := rows.Scan(&item.Scope, &item.TargetID, &instances, &turns, &enabled, &srv); err != nil {
			return nil, err
		}
		item.MaxInstances = nullToPtr(instances)
		item.MaxConcurrentTurns = nullToPtr(turns)
		item.Enabled = enabledWord(item.Scope, enabled)
		if srv.Valid {
			v := srv.String
			item.SrvID = &v
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func nullToPtr(v sql.NullInt64) *int {
	if !v.Valid {
		return nil
	}
	n := int(v.Int64)
	return &n
}

func enabledWord(scope string, v sql.NullInt64) string {
	if !v.Valid {
		if scope == ScopeGlobal {
			return AccessOff
		}
		return AccessInherit
	}
	if v.Int64 != 0 {
		return AccessOn
	}
	return AccessOff
}

// SaveAccess writes one scope row. A department or user that inherits and
// leaves both limits blank is deleted. The global row cannot inherit.
func (s *Service) SaveAccess(ctx context.Context, tenantID string, in AccessView) (AccessView, bool, error) {
	if s == nil || s.db == nil {
		return AccessView{}, false, fail(503, "UNAVAILABLE", "云端管理暂时不可用")
	}
	scope := strings.TrimSpace(in.Scope)
	target := strings.TrimSpace(in.TargetID)
	switch scope {
	case ScopeGlobal:
		if target != "" {
			return AccessView{}, false, failInvalid("全局范围没有对象")
		}
	case ScopeDepartment, ScopeUser:
		if target == "" {
			return AccessView{}, false, failInvalid("请选择部门或用户")
		}
	default:
		return AccessView{}, false, failInvalid("请选择全局、部门或用户")
	}
	if in.MaxInstances != nil && *in.MaxInstances < 0 {
		return AccessView{}, false, failInvalid("上限不能是负数")
	}
	if in.MaxConcurrentTurns != nil && *in.MaxConcurrentTurns < 0 {
		return AccessView{}, false, failInvalid("上限不能是负数")
	}
	enabled, err := parseEnabled(scope, strings.TrimSpace(in.Enabled))
	if err != nil {
		return AccessView{}, false, err
	}
	if scope == ScopeGlobal && (in.MaxInstances == nil || in.MaxConcurrentTurns == nil) {
		return AccessView{}, false, failInvalid("请同时填写开关和两个上限")
	}
	var srv sql.NullString
	if in.SrvID != nil {
		pin := strings.TrimSpace(*in.SrvID)
		srv = sql.NullString{String: pin, Valid: true}
		if pin != "" {
			if err := s.checkSrvPin(ctx, tenantID, pin); err != nil {
				return AccessView{}, false, err
			}
		}
	}
	tenantID = store.NormalizeTenantID(tenantID)
	if scope != ScopeGlobal && !enabled.Valid && in.MaxInstances == nil && in.MaxConcurrentTurns == nil && !srv.Valid {
		if _, err := s.db.ExecContext(ctx, `DELETE FROM cloud_agent_limits WHERE tenant_id = ? AND scope = ? AND target_id = ?`, tenantID, scope, target); err != nil {
			return AccessView{}, false, err
		}
		return AccessView{Scope: scope, TargetID: target, Enabled: AccessInherit}, true, nil
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO cloud_agent_limits
		(tenant_id, scope, target_id, max_instances, max_concurrent_turns, enabled, srv_id, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(tenant_id, scope, target_id) DO UPDATE SET
			max_instances = excluded.max_instances,
			max_concurrent_turns = excluded.max_concurrent_turns,
			enabled = excluded.enabled,
			srv_id = excluded.srv_id,
			updated_at = excluded.updated_at`,
		tenantID, scope, target, nullInt(in.MaxInstances), nullInt(in.MaxConcurrentTurns), nullEnabled(enabled), nullSrv(srv), formatTS(s.now())); err != nil {
		return AccessView{}, false, err
	}
	saved := AccessView{
		Scope:              scope,
		TargetID:           target,
		Enabled:            enabledWord(scope, enabled),
		MaxInstances:       in.MaxInstances,
		MaxConcurrentTurns: in.MaxConcurrentTurns,
	}
	if srv.Valid {
		v := srv.String
		saved.SrvID = &v
	}
	return saved, false, nil
}

func nullSrv(v sql.NullString) any {
	if !v.Valid {
		return nil
	}
	return v.String
}

// checkSrvPin refuses a scope pin that names no registered srv link or
// node srv, so a typo cannot silently land agents on the public srv.
func (s *Service) checkSrvPin(ctx context.Context, tenantID, pin string) error {
	rec, err := s.srvRecord(ctx, tenantID)
	if err != nil {
		return failInvalid("无法校验这个 maclawsrv，请先保存连接")
	}
	if _, ok := rec.srvLinks[pin]; ok {
		return nil
	}
	if _, ok := rec.nodeLinks[pin]; ok {
		return nil
	}
	return failInvalid("这个 maclawsrv 没有注册")
}

func parseEnabled(scope, word string) (sql.NullInt64, error) {
	switch word {
	case AccessOn:
		return sql.NullInt64{Int64: 1, Valid: true}, nil
	case AccessOff:
		return sql.NullInt64{Int64: 0, Valid: true}, nil
	case AccessInherit, "":
		if scope == ScopeGlobal {
			return sql.NullInt64{}, failInvalid("全局开关不能继承")
		}
		return sql.NullInt64{}, nil
	default:
		return sql.NullInt64{}, failInvalid("请选择继承、开通或关闭")
	}
}

func nullEnabled(v sql.NullInt64) any {
	if !v.Valid {
		return nil
	}
	return v.Int64
}
