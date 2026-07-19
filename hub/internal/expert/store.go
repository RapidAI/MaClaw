package expert

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// expertIDPattern 限定客户端自带 id 的字符集（多设备同步 id，防路径/注入式 id）。
var expertIDPattern = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

const (
	maxNameRunes         = 200
	maxSystemPromptBytes = 32 * 1024
	// futureSkew 容忍的时钟漂移上限：updated_at 超过 now+5min 时钳到 now，防时钟投毒 LWW。
	futureSkew = 5 * time.Minute
)

// ValidationError 标识客户端输入校验失败（handler 映射 400；其余错误一律 500 且不回显）。
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func validationErrf(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

// Store 是租户级 expert 权威存储（Hub SQLite）。
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) InitSchema(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS experts (
			id TEXT NOT NULL,
			tenant_id TEXT NOT NULL,
			name TEXT NOT NULL,
			definition_json TEXT NOT NULL,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, id)
		)`,
		// 墓碑：删除后早于/等于 deleted_at 的重放写不得复活该资源。
		`CREATE TABLE IF NOT EXISTS expert_tombstones (
			tenant_id TEXT NOT NULL,
			id TEXT NOT NULL,
			deleted_at TEXT NOT NULL,
			PRIMARY KEY(tenant_id, id)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("expert schema: %w", err)
		}
	}
	return nil
}

// validateName trims and validates the expert name (create 与 update 共用).
func validateName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", validationErrf("name required")
	}
	if len([]rune(name)) > maxNameRunes {
		return "", validationErrf("name too long (max %d)", maxNameRunes)
	}
	return name, nil
}

// validateSystemPrompt 拒绝 trim 后为空的 system_prompt，上限 32KiB。
func validateSystemPrompt(sp string) error {
	if strings.TrimSpace(sp) == "" {
		return validationErrf("system_prompt required")
	}
	if len(sp) > maxSystemPromptBytes {
		return validationErrf("system_prompt too long (max %d)", maxSystemPromptBytes)
	}
	return nil
}

// normalizeTimestamp 解析 RFC3339（允许带 offset / 小数秒），统一为 UTC RFC3339Nano，
// 保证文本比较（LWW / ORDER BY）前提成立；超过 now+futureSkew 的未来时间钳到 now。
func normalizeTimestamp(raw string, now time.Time) (string, error) {
	t, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return "", validationErrf("invalid updated_at (want RFC3339)")
	}
	t = t.UTC()
	if t.After(now.Add(futureSkew)) {
		t = now
	}
	return t.Format(time.RFC3339Nano), nil
}

// normalizeList 保证序列化为 [] 而非 null。
func normalizeList(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

// Upsert 创建或按 LWW 覆盖：同 tenant 同 id 已存在时，仅当提交的 updated_at
// 不早于现值才更新（旧设备回放的过期写不会覆盖新值）。id 空时服务端生成 uuid。
// 返回 applied=false 表示写入未生效（LWW 过期写或命中墓碑），Expert 为当前值/未持久化的提交值。
func (s *Store) Upsert(ctx context.Context, tenantID string, in CreateInput) (*Expert, bool, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return nil, false, validationErrf("tenant required")
	}
	name, err := validateName(in.Name)
	if err != nil {
		return nil, false, err
	}
	if err := validateSystemPrompt(in.SystemPrompt); err != nil {
		return nil, false, err
	}
	id := strings.TrimSpace(in.ID)
	if id != "" {
		if !expertIDPattern.MatchString(id) {
			return nil, false, validationErrf("invalid id (want [A-Za-z0-9._-], max 128)")
		}
	} else {
		id = uuid.NewString()
	}
	now := time.Now().UTC()
	nowStr := now.Format(time.RFC3339Nano)
	updatedAt := strings.TrimSpace(in.UpdatedAt)
	if updatedAt == "" {
		updatedAt = nowStr
	} else {
		var err error
		updatedAt, err = normalizeTimestamp(updatedAt, now)
		if err != nil {
			return nil, false, err
		}
	}
	// 已存在行保留原 created_at。
	createdAt := nowStr
	if existing, err := s.Get(ctx, tenantID, id); err == nil {
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	ex := &Expert{
		ID:           id,
		TenantID:     tenantID,
		Name:         name,
		Description:  strings.TrimSpace(in.Description),
		Icon:         strings.TrimSpace(in.Icon),
		SystemPrompt: in.SystemPrompt,
		Tools:        normalizeList(in.Tools),
		Skills:       normalizeList(in.Skills),
		CreatedAt:    createdAt,
		UpdatedAt:    updatedAt,
	}

	// 墓碑裁决：updated_at <= deleted_at 的重放写静默忽略（applied=false）；
	// 比墓碑新的写入清除墓碑后正常落库。
	writeAt, _ := time.Parse(time.RFC3339Nano, updatedAt)
	var tombstoneAt string
	err = s.db.QueryRowContext(ctx, `SELECT deleted_at FROM expert_tombstones WHERE tenant_id=? AND id=?`, tenantID, id).Scan(&tombstoneAt)
	switch {
	case err == nil:
		if deletedAt, perr := time.Parse(time.RFC3339Nano, tombstoneAt); perr == nil && !writeAt.After(deletedAt) {
			return ex, false, nil
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM expert_tombstones WHERE tenant_id=? AND id=?`, tenantID, id); err != nil {
			return nil, false, err
		}
	case errors.Is(err, sql.ErrNoRows):
	default:
		return nil, false, err
	}

	def, err := json.Marshal(ex)
	if err != nil {
		return nil, false, fmt.Errorf("marshal expert: %w", err)
	}
	// LWW：仅当 excluded.updated_at 不早于现值时覆盖（created_at 保持不变）。
	res, err := s.db.ExecContext(ctx, `INSERT INTO experts(id,tenant_id,name,definition_json,created_at,updated_at)
		VALUES(?,?,?,?,?,?)
		ON CONFLICT(tenant_id,id) DO UPDATE SET
			name=excluded.name, definition_json=excluded.definition_json, updated_at=excluded.updated_at
			WHERE excluded.updated_at >= experts.updated_at`,
		ex.ID, tenantID, ex.Name, string(def), ex.CreatedAt, ex.UpdatedAt)
	if err != nil {
		return nil, false, err
	}
	// 冲突且 WHERE 未通过时 DO UPDATE 被跳过：过期写未生效。
	applied := true
	if n, _ := res.RowsAffected(); n == 0 {
		applied = false
	}
	stored, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, false, err
	}
	return stored, applied, nil
}

func decodeExpert(tenantID, def string) (*Expert, error) {
	var ex Expert
	if err := json.Unmarshal([]byte(def), &ex); err != nil {
		return nil, fmt.Errorf("corrupt expert definition: %w", err)
	}
	ex.TenantID = tenantID
	return &ex, nil
}

func (s *Store) Get(ctx context.Context, tenantID, id string) (*Expert, error) {
	row := s.db.QueryRowContext(ctx, `SELECT definition_json FROM experts WHERE tenant_id=? AND id=?`, tenantID, id)
	var def string
	if err := row.Scan(&def); err != nil {
		return nil, err
	}
	return decodeExpert(tenantID, def)
}

// List 返回租户全量 expert，按 updated_at 倒序。
func (s *Store) List(ctx context.Context, tenantID string) ([]Expert, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT definition_json FROM experts WHERE tenant_id=? ORDER BY updated_at DESC`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Expert{}
	for rows.Next() {
		var def string
		if err := rows.Scan(&def); err != nil {
			return nil, err
		}
		ex, err := decodeExpert(tenantID, def)
		if err != nil {
			return nil, err
		}
		out = append(out, *ex)
	}
	return out, rows.Err()
}

func (s *Store) Update(ctx context.Context, tenantID, id string, in UpdateInput) (*Expert, error) {
	ex, err := s.Get(ctx, tenantID, id)
	if err != nil {
		return nil, err
	}
	if in.Name != nil {
		name, err := validateName(*in.Name)
		if err != nil {
			return nil, err
		}
		ex.Name = name
	}
	if in.Description != nil {
		ex.Description = strings.TrimSpace(*in.Description)
	}
	if in.Icon != nil {
		ex.Icon = strings.TrimSpace(*in.Icon)
	}
	if in.SystemPrompt != nil {
		if err := validateSystemPrompt(*in.SystemPrompt); err != nil {
			return nil, err
		}
		ex.SystemPrompt = *in.SystemPrompt
	}
	if in.Tools != nil {
		ex.Tools = normalizeList(*in.Tools)
	}
	if in.Skills != nil {
		ex.Skills = normalizeList(*in.Skills)
	}
	ex.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	def, err := json.Marshal(ex)
	if err != nil {
		return nil, fmt.Errorf("marshal expert: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `UPDATE experts SET name=?, definition_json=?, updated_at=? WHERE tenant_id=? AND id=?`,
		ex.Name, string(def), ex.UpdatedAt, tenantID, id)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}
	return s.Get(ctx, tenantID, id)
}

// Delete 物理删除行并写墓碑（同事务）：删除时间不早于任何未到达的过期重放写。
func (s *Store) Delete(ctx context.Context, tenantID, id string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO expert_tombstones(tenant_id,id,deleted_at) VALUES(?,?,?)
		ON CONFLICT(tenant_id,id) DO UPDATE SET deleted_at=excluded.deleted_at`, tenantID, id, now); err != nil {
		return err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM experts WHERE tenant_id=? AND id=?`, tenantID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}
