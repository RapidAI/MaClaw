package sqlite

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type TenantUserMigrationOptions struct {
	MappingPath      string `json:"mapping_path"`
	FromTenant       string `json:"from_tenant"`
	DryRun           bool   `json:"dry_run"`
	CopyTenantConfig bool   `json:"copy_tenant_config"`
}

type TenantUserMigrationResult struct {
	DryRun               bool                          `json:"dry_run"`
	FromTenant           string                        `json:"from_tenant"`
	CopyTenantConfig     bool                          `json:"copy_tenant_config"`
	UsersTotal           int                           `json:"users_total"`
	UsersMatched         int                           `json:"users_matched"`
	UsersMoved           int                           `json:"users_moved"`
	ByTenant             map[string]int                `json:"by_tenant"`
	TableUpdates         map[string]int64              `json:"table_updates"`
	TenantResourceCopies map[string]map[string]int64   `json:"tenant_resource_copies,omitempty"`
	Warnings             []string                      `json:"warnings,omitempty"`
	Users                []TenantUserMigrationUserItem `json:"users"`
}

type TenantMergeOptions struct {
	FromTenant   string `json:"from_tenant"`
	ToTenant     string `json:"to_tenant"`
	DryRun       bool   `json:"dry_run"`
	DeleteSource bool   `json:"delete_source"`
}

type TenantMergeResult struct {
	DryRun         bool                        `json:"dry_run"`
	FromTenant     string                      `json:"from_tenant"`
	ToTenant       string                      `json:"to_tenant"`
	DeleteSource   bool                        `json:"delete_source"`
	Tables         map[string]TenantMergeTable `json:"tables"`
	SystemSettings TenantMergeSystemSettings   `json:"system_settings"`
	Warnings       []string                    `json:"warnings,omitempty"`
}

type TenantMergeTable struct {
	SourceRows int64 `json:"source_rows"`
	MovedRows  int64 `json:"moved_rows"`
	MergedRows int64 `json:"merged_rows"`
}

type TenantMergeSystemSettings struct {
	SourceKeys int64 `json:"source_keys"`
	MovedKeys  int64 `json:"moved_keys"`
	MergedKeys int64 `json:"merged_keys"`
}

var ErrTenantMergeConflict = errors.New("tenant merge conflict")

type TenantUserMigrationUserItem struct {
	Email      string           `json:"email"`
	UserID     string           `json:"user_id,omitempty"`
	FromTenant string           `json:"from_tenant"`
	ToTenant   string           `json:"to_tenant"`
	Found      bool             `json:"found"`
	Updates    map[string]int64 `json:"updates,omitempty"`
	Warnings   []string         `json:"warnings,omitempty"`
}

type tenantUserMapping struct {
	Email    string
	TenantID string
}

type tenantUserRow struct {
	ID       string
	Email    string
	TenantID string
}

type securityGroupRow struct {
	ID        string
	Name      string
	ParentID  string
	CreatedAt string
	UpdatedAt string
}

func MigrateTenantUsers(ctx context.Context, db *sql.DB, opts TenantUserMigrationOptions) (*TenantUserMigrationResult, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	fromTenant := store.NormalizeTenantID(opts.FromTenant)
	mappings, err := readTenantUserMappings(opts.MappingPath)
	if err != nil {
		return nil, err
	}
	result := &TenantUserMigrationResult{
		DryRun:               opts.DryRun,
		FromTenant:           fromTenant,
		CopyTenantConfig:     opts.CopyTenantConfig,
		UsersTotal:           len(mappings),
		ByTenant:             map[string]int{},
		TableUpdates:         map[string]int64{},
		TenantResourceCopies: map[string]map[string]int64{},
		Users:                make([]TenantUserMigrationUserItem, 0, len(mappings)),
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureTenantExists(ctx, tx, fromTenant); err != nil {
		return nil, err
	}
	targetTenants := map[string]struct{}{}
	for _, m := range mappings {
		if err := ensureTenantExists(ctx, tx, m.TenantID); err != nil {
			return nil, err
		}
		if m.TenantID != fromTenant {
			targetTenants[m.TenantID] = struct{}{}
		}
	}
	if opts.CopyTenantConfig {
		for tenantID := range targetTenants {
			copies, err := copyTenantConfig(ctx, tx, fromTenant, tenantID)
			if err != nil {
				return nil, err
			}
			result.TenantResourceCopies[tenantID] = copies
		}
	}

	for _, m := range mappings {
		item := TenantUserMigrationUserItem{Email: m.Email, FromTenant: fromTenant, ToTenant: m.TenantID, Updates: map[string]int64{}}
		result.ByTenant[m.TenantID]++
		if m.TenantID == fromTenant {
			item.Found = true
			item.Warnings = append(item.Warnings, "target tenant is same as source tenant; skipped")
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s target tenant is source tenant; skipped", m.Email))
			result.Users = append(result.Users, item)
			continue
		}

		user, err := findUserByTenantEmail(ctx, tx, fromTenant, m.Email)
		if err != nil {
			return nil, err
		}
		if user == nil {
			item.Warnings = append(item.Warnings, "user not found in source tenant")
			result.Warnings = append(result.Warnings, fmt.Sprintf("%s not found in source tenant %s", m.Email, fromTenant))
			result.Users = append(result.Users, item)
			continue
		}
		item.Found = true
		item.UserID = user.ID
		result.UsersMatched++

		existing, err := findUserByTenantEmail(ctx, tx, m.TenantID, m.Email)
		if err != nil {
			return nil, err
		}
		if existing != nil && existing.ID != user.ID {
			return nil, fmt.Errorf("target tenant %s already has email %s as user %s", m.TenantID, m.Email, existing.ID)
		}

		updates, err := migrateOneTenantUser(ctx, tx, fromTenant, m.TenantID, user.ID, m.Email)
		if err != nil {
			return nil, fmt.Errorf("migrate %s: %w", m.Email, err)
		}
		item.Updates = updates
		for table, count := range updates {
			result.TableUpdates[table] += count
		}
		if updates["users"] > 0 {
			result.UsersMoved++
		}
		result.Users = append(result.Users, item)
	}

	if opts.DryRun {
		return result, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}

func MergeTenants(ctx context.Context, db *sql.DB, opts TenantMergeOptions) (*TenantMergeResult, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}
	fromTenant := store.NormalizeTenantID(opts.FromTenant)
	toTenant := store.NormalizeTenantID(opts.ToTenant)
	if fromTenant == "" || toTenant == "" || fromTenant == toTenant {
		return nil, errors.New("source and target tenants must be different")
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()

	if err := ensureTenantExists(ctx, tx, fromTenant); err != nil {
		return nil, err
	}
	if err := ensureTenantExists(ctx, tx, toTenant); err != nil {
		return nil, err
	}
	if err := ensureTenantMergeUserEmailsDoNotConflict(ctx, tx, fromTenant, toTenant); err != nil {
		return nil, err
	}
	groupIDMap, err := mergeTenantSecurityRoots(ctx, tx, fromTenant, toTenant)
	if err != nil {
		return nil, err
	}

	result := &TenantMergeResult{DryRun: opts.DryRun, FromTenant: fromTenant, ToTenant: toTenant, DeleteSource: opts.DeleteSource, Tables: map[string]TenantMergeTable{}}
	tables, err := tenantScopedTables(ctx, tx)
	if err != nil {
		return nil, err
	}
	for _, table := range tables {
		summary, err := mergeTenantTable(ctx, tx, table, fromTenant, toTenant)
		if err != nil {
			return nil, fmt.Errorf("merge table %s: %w", table, err)
		}
		if summary.SourceRows > 0 {
			result.Tables[table] = summary
		}
	}
	deleteSourceSettings := opts.DeleteSource && fromTenant != store.DefaultTenantID
	settings, err := mergeTenantSystemSettings(ctx, tx, fromTenant, toTenant, groupIDMap, deleteSourceSettings)
	if err != nil {
		return nil, err
	}
	result.SystemSettings = settings

	if opts.DeleteSource {
		if fromTenant == store.DefaultTenantID {
			result.Warnings = append(result.Warnings, "default tenant cannot be deleted; data was merged but source tenant remains active")
		} else if _, err := tx.ExecContext(ctx, `UPDATE tenants SET status = 'deleted', deleted_at = ?, updated_at = ? WHERE id = ? AND deleted_at IS NULL`, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339), fromTenant); err != nil {
			return nil, fmt.Errorf("delete source tenant: %w", err)
		}
	}
	if !opts.DeleteSource || fromTenant == store.DefaultTenantID {
		if err := ensureTenantMergeRootExists(ctx, tx, fromTenant); err != nil {
			return nil, err
		}
	}

	if opts.DryRun {
		return result, nil
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	committed = true
	return result, nil
}

func readTenantUserMappings(path string) ([]tenantUserMapping, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("mapping path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	r.TrimLeadingSpace = true
	rows := make([]tenantUserMapping, 0)
	seen := map[string]struct{}{}
	line := 0
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read csv line %d: %w", line+1, err)
		}
		line++
		if len(rec) == 0 {
			continue
		}
		if line == 1 && len(rec) >= 2 && strings.EqualFold(strings.TrimSpace(rec[0]), "email") {
			continue
		}
		if len(rec) < 2 {
			return nil, fmt.Errorf("csv line %d requires email,tenant_id", line)
		}
		email := strings.ToLower(strings.TrimSpace(rec[0]))
		tenantID := store.NormalizeTenantID(rec[1])
		if email == "" || tenantID == "" {
			return nil, fmt.Errorf("csv line %d has empty email or tenant_id", line)
		}
		if _, ok := seen[email]; ok {
			return nil, fmt.Errorf("duplicate email in mapping: %s", email)
		}
		seen[email] = struct{}{}
		rows = append(rows, tenantUserMapping{Email: email, TenantID: tenantID})
	}
	if len(rows) == 0 {
		return nil, errors.New("mapping csv contains no users")
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Email < rows[j].Email })
	return rows, nil
}

func tenantScopedTables(ctx context.Context, tx *sql.Tx) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' AND name <> 'tenants' ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var candidates []string
	for rows.Next() {
		var table string
		if err := rows.Scan(&table); err != nil {
			return nil, err
		}
		if tenantMergeSafeIdent(table) {
			candidates = append(candidates, table)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var tables []string
	for _, table := range candidates {
		hasTenantID, err := tenantMergeTableHasTenantID(ctx, tx, table)
		if err != nil {
			return nil, err
		}
		if hasTenantID {
			tables = append(tables, table)
		}
	}
	return tables, nil
}

func tenantMergeTableHasTenantID(ctx context.Context, tx *sql.Tx, table string) (bool, error) {
	rows, err := tx.QueryContext(ctx, `PRAGMA table_info(`+tenantMergeQuoteIdent(table)+`)`)
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue any
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if name == "tenant_id" {
			return true, nil
		}
	}
	return false, rows.Err()
}

func mergeTenantTable(ctx context.Context, tx *sql.Tx, table, fromTenant, toTenant string) (TenantMergeTable, error) {
	if !tenantMergeSafeIdent(table) {
		return TenantMergeTable{}, fmt.Errorf("unsafe table name %q", table)
	}
	quoted := tenantMergeQuoteIdent(table)
	var sourceRows int64
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+quoted+` WHERE tenant_id = ?`, fromTenant).Scan(&sourceRows); err != nil {
		return TenantMergeTable{}, err
	}
	if sourceRows == 0 {
		return TenantMergeTable{}, nil
	}
	res, err := tx.ExecContext(ctx, `UPDATE OR IGNORE `+quoted+` SET tenant_id = ? WHERE tenant_id = ?`, toTenant, fromTenant)
	if err != nil {
		return TenantMergeTable{}, err
	}
	moved, _ := res.RowsAffected()
	merged := sourceRows - moved
	if merged > 0 {
		return TenantMergeTable{}, fmt.Errorf("%w: table %s has %d source rows that conflict with existing target rows; resolve duplicates before merging", ErrTenantMergeConflict, table, merged)
	}
	return TenantMergeTable{SourceRows: sourceRows, MovedRows: moved, MergedRows: merged}, nil
}

func mergeTenantSystemSettings(ctx context.Context, tx *sql.Tx, fromTenant, toTenant string, groupIDMap map[string]string, deleteSource bool) (TenantMergeSystemSettings, error) {
	fromPrefix := "tenant:" + fromTenant + ":"
	toPrefix := "tenant:" + toTenant + ":"
	type settingRow struct{ key, value string }
	var settings []settingRow
	if fromTenant == store.DefaultTenantID {
		for _, key := range []string{llmservice.RegistryKey, "security_settings"} {
			raw, err := getSystemSetting(ctx, tx, key)
			if err != nil {
				return TenantMergeSystemSettings{}, err
			}
			if strings.TrimSpace(raw) != "" {
				settings = append(settings, settingRow{key: key, value: raw})
			}
		}
	} else {
		rows, err := tx.QueryContext(ctx, `SELECT key, value_json FROM system_settings WHERE key LIKE ?`, fromPrefix+"%")
		if err != nil {
			return TenantMergeSystemSettings{}, err
		}
		defer rows.Close()
		for rows.Next() {
			var item settingRow
			if err := rows.Scan(&item.key, &item.value); err != nil {
				return TenantMergeSystemSettings{}, err
			}
			settings = append(settings, item)
		}
		if err := rows.Err(); err != nil {
			return TenantMergeSystemSettings{}, err
		}
	}
	result := TenantMergeSystemSettings{SourceKeys: int64(len(settings))}
	for _, item := range settings {
		settingName := strings.TrimPrefix(item.key, fromPrefix)
		targetKey := toPrefix + settingName
		if settingName == llmservice.RegistryKey {
			merged, err := mergeTenantLLMRegistrySetting(ctx, tx, item.key, targetKey, item.value, groupIDMap, deleteSource)
			if err != nil {
				return TenantMergeSystemSettings{}, err
			}
			if merged {
				result.MergedKeys++
			} else {
				result.MovedKeys++
			}
			continue
		}
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?)`, targetKey, item.value, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			return TenantMergeSystemSettings{}, err
		}
		moved, _ := res.RowsAffected()
		if moved > 0 {
			result.MovedKeys++
		} else {
			result.MergedKeys++
		}
		if deleteSource {
			if _, err := tx.ExecContext(ctx, `DELETE FROM system_settings WHERE key = ?`, item.key); err != nil {
				return TenantMergeSystemSettings{}, err
			}
		}
	}
	return result, nil
}

func mergeTenantLLMRegistrySetting(ctx context.Context, tx *sql.Tx, sourceKey, targetKey, sourceRaw string, groupIDMap map[string]string, deleteSource bool) (bool, error) {
	sourceReg, err := decodeLLMRegistry(sourceRaw)
	if err != nil {
		return false, fmt.Errorf("decode source llm registry: %w", err)
	}
	remapRegistryGroupIDs(sourceReg, groupIDMap)
	targetRaw, err := getSystemSetting(ctx, tx, targetKey)
	if err != nil {
		return false, err
	}
	merged := strings.TrimSpace(targetRaw) != ""
	if !merged {
		if err := saveLLMRegistrySetting(ctx, tx, targetKey, sourceReg); err != nil {
			return false, err
		}
	} else {
		targetReg, err := decodeLLMRegistry(targetRaw)
		if err != nil {
			return false, fmt.Errorf("decode target llm registry: %w", err)
		}
		mergeFullLLMRegistry(targetReg, sourceReg)
		if err := saveLLMRegistrySetting(ctx, tx, targetKey, targetReg); err != nil {
			return false, err
		}
	}
	if deleteSource {
		if _, err := tx.ExecContext(ctx, `DELETE FROM system_settings WHERE key = ?`, sourceKey); err != nil {
			return false, err
		}
	}
	return merged, nil
}

func mergeTenantSecurityRoots(ctx context.Context, tx *sql.Tx, fromTenant, toTenant string) (map[string]string, error) {
	sourceRootID, err := tenantMergeRootGroupID(ctx, tx, fromTenant)
	if err != nil || sourceRootID == "" {
		return nil, err
	}
	targetRootID, err := tenantMergeRootGroupID(ctx, tx, toTenant)
	if err != nil || targetRootID == "" {
		return nil, err
	}
	if sourceRootID == targetRootID {
		return nil, nil
	}
	if _, err := tx.ExecContext(ctx, `UPDATE security_groups SET parent_id = ? WHERE tenant_id = ? AND parent_id = ?`, targetRootID, fromTenant, sourceRootID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE security_group_members SET group_id = ? WHERE tenant_id = ? AND group_id = ?`, targetRootID, fromTenant, sourceRootID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM security_policies WHERE tenant_id = ? AND group_id = ?`, fromTenant, sourceRootID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM security_groups WHERE tenant_id = ? AND id = ?`, fromTenant, sourceRootID); err != nil {
		return nil, err
	}
	return map[string]string{sourceRootID: targetRootID}, nil
}

func tenantMergeRootGroupID(ctx context.Context, tx *sql.Tx, tenantID string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM security_groups WHERE tenant_id = ? AND parent_id = '' LIMIT 1`, tenantID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func ensureTenantMergeRootExists(ctx context.Context, tx *sql.Tx, tenantID string) error {
	existing, err := tenantMergeRootGroupID(ctx, tx, tenantID)
	if err != nil || existing != "" {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := "root_" + tenantID + "_" + randomTenantMigrationSuffix()
	_, err = tx.ExecContext(ctx, `INSERT INTO security_groups (tenant_id, id, name, parent_id, created_at, updated_at) VALUES (?, ?, ?, '', ?, ?)`, tenantID, id, "Root", now, now)
	return err
}

func randomTenantMigrationSuffix() string {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func ensureTenantMergeUserEmailsDoNotConflict(ctx context.Context, tx *sql.Tx, fromTenant, toTenant string) error {
	rows, err := tx.QueryContext(ctx, `SELECT s.email FROM users s JOIN users t ON lower(s.email) = lower(t.email) WHERE s.tenant_id = ? AND t.tenant_id = ? AND s.id <> t.id ORDER BY s.email LIMIT 5`, fromTenant, toTenant)
	if err != nil {
		return err
	}
	defer rows.Close()
	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return err
		}
		emails = append(emails, email)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(emails) > 0 {
		return fmt.Errorf("target tenant already has users with these emails: %s", strings.Join(emails, ", "))
	}
	return nil
}

func tenantMergeSafeIdent(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

func tenantMergeQuoteIdent(name string) string {
	return `"` + name + `"`
}

func ensureTenantExists(ctx context.Context, tx *sql.Tx, tenantID string) error {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM tenants WHERE id = ? AND deleted_at IS NULL`, tenantID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("tenant %s does not exist", tenantID)
	}
	return err
}

func findUserByTenantEmail(ctx context.Context, tx *sql.Tx, tenantID, email string) (*tenantUserRow, error) {
	var row tenantUserRow
	err := tx.QueryRowContext(ctx, `SELECT id, tenant_id, email FROM users WHERE tenant_id = ? AND lower(email) = lower(?)`, tenantID, email).Scan(&row.ID, &row.TenantID, &row.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func migrateOneTenantUser(ctx context.Context, tx *sql.Tx, fromTenant, toTenant, userID, email string) (map[string]int64, error) {
	updates := map[string]int64{}
	securityGroupID, err := userSecurityGroupID(ctx, tx, fromTenant, email)
	if err != nil {
		return nil, err
	}
	userMachineIDs, err := machineIDsForTenantUser(ctx, tx, fromTenant, userID)
	if err != nil {
		return nil, err
	}
	steps := []struct {
		table string
		where string
		args  []any
	}{
		{table: "users", where: "id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "user_enrollments", where: "lower(email) = lower(?) AND tenant_id = ?", args: []any{email, fromTenant}},
		{table: "email_blocklist", where: "lower(email) = lower(?) AND tenant_id = ?", args: []any{email, fromTenant}},
		{table: "email_invites", where: "lower(email) = lower(?) AND tenant_id = ?", args: []any{email, fromTenant}},
		{table: "login_tokens", where: "lower(email) = lower(?) AND tenant_id = ?", args: []any{email, fromTenant}},
		{table: "viewer_tokens", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "machines", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "sessions", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "content_audit_logs", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "failure_event_logs", where: "lower(email) = lower(?) AND tenant_id = ?", args: []any{email, fromTenant}},
		{table: "invitation_codes", where: "lower(used_by_email) = lower(?) AND tenant_id = ?", args: []any{email, fromTenant}},
		{table: "understanding_sessions", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "workflow_states", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "workflow_definitions", where: "owner_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "mcp_secret_bindings", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "mcp_hub_secrets", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "user_capability_inventory", where: "lower(user_email) = lower(?) AND tenant_id = ?", args: []any{email, fromTenant}},
		{table: "security_group_members", where: "lower(email) = lower(?) AND tenant_id = ?", args: []any{email, fromTenant}},
		{table: "chat_channels", where: "created_by = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "chat_members", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "chat_messages", where: "sender_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "chat_files", where: "uploader_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "chat_voice_calls", where: "caller_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "chat_voice_participants", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "chat_push_tokens", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
		{table: "chat_presence", where: "user_id = ? AND tenant_id = ?", args: []any{userID, fromTenant}},
	}
	for _, step := range steps {
		count, err := updateTenantForTable(ctx, tx, step.table, toTenant, step.where, step.args...)
		if err != nil {
			return nil, err
		}
		updates[step.table] = count
	}
	llmUpdates, err := moveLLMServiceAssignments(ctx, tx, fromTenant, toTenant, email)
	if err != nil {
		return nil, err
	}
	for key, count := range llmUpdates {
		updates[key] = count
	}
	imUpdates, err := moveIMBindings(ctx, tx, fromTenant, toTenant, email)
	if err != nil {
		return nil, err
	}
	for key, count := range imUpdates {
		updates[key] = count
	}
	workflowUpdates, err := moveWorkflowRuntime(ctx, tx, fromTenant, toTenant, userID)
	if err != nil {
		return nil, err
	}
	for key, count := range workflowUpdates {
		updates[key] = count
	}
	settingUpdates, err := moveUserSystemSettings(ctx, tx, fromTenant, toTenant, userID)
	if err != nil {
		return nil, err
	}
	for key, count := range settingUpdates {
		updates[key] = count
	}
	a2aUpdates, err := moveA2AGroupState(ctx, tx, fromTenant, toTenant, userMachineIDs)
	if err != nil {
		return nil, err
	}
	for key, count := range a2aUpdates {
		updates[key] = count
	}
	if securityGroupID != "" && updates["security_group_members"] > 0 {
		targetGroupID, err := ensureSecurityGroupPath(ctx, tx, fromTenant, toTenant, securityGroupID, map[string]string{})
		if err != nil {
			return nil, err
		}
		res, err := tx.ExecContext(ctx, `UPDATE security_group_members SET group_id = ? WHERE tenant_id = ? AND lower(email) = lower(?)`, targetGroupID, toTenant, email)
		if err != nil {
			return nil, err
		}
		updates["security_group_member_group_remap"], _ = res.RowsAffected()
		groupBindingCount, err := copyLLMServiceGroupBinding(ctx, tx, fromTenant, toTenant, securityGroupID, targetGroupID)
		if err != nil {
			return nil, err
		}
		updates["llm_service_group_bindings"] = groupBindingCount
	}
	return updates, nil
}

func moveLLMServiceAssignments(ctx context.Context, tx *sql.Tx, fromTenant, toTenant, email string) (map[string]int64, error) {
	updates := map[string]int64{}
	sourceKey := tenantRegistryKey(fromTenant)
	targetKey := tenantRegistryKey(toTenant)
	sourceRaw, err := getSystemSetting(ctx, tx, sourceKey)
	if err != nil || strings.TrimSpace(sourceRaw) == "" {
		return updates, err
	}
	sourceReg, err := decodeLLMRegistry(sourceRaw)
	if err != nil {
		return nil, fmt.Errorf("decode source llm registry: %w", err)
	}
	email = strings.ToLower(strings.TrimSpace(email))
	var movedBindings []llmservice.UserBinding
	var keptBindings []llmservice.UserBinding
	for _, binding := range sourceReg.UserBindings {
		if strings.EqualFold(strings.TrimSpace(binding.Email), email) {
			binding.Email = email
			movedBindings = append(movedBindings, binding)
			continue
		}
		keptBindings = append(keptBindings, binding)
	}
	var movedGrants []llmservice.Grant
	var keptGrants []llmservice.Grant
	for _, grant := range sourceReg.Grants {
		if strings.EqualFold(strings.TrimSpace(grant.Email), email) {
			grant.Email = email
			movedGrants = append(movedGrants, grant)
			continue
		}
		keptGrants = append(keptGrants, grant)
	}
	var movedCards []llmservice.RechargeCard
	var keptCards []llmservice.RechargeCard
	for _, card := range sourceReg.Cards {
		if strings.EqualFold(strings.TrimSpace(card.RedeemedByEmail), email) {
			card.RedeemedByEmail = email
			movedCards = append(movedCards, card)
			continue
		}
		keptCards = append(keptCards, card)
	}
	if len(movedBindings) == 0 && len(movedGrants) == 0 && len(movedCards) == 0 {
		return updates, nil
	}
	targetReg, err := loadTargetLLMRegistry(ctx, tx, targetKey, sourceReg)
	if err != nil {
		return nil, err
	}
	targetReg.UserBindings = mergeUserBindings(targetReg.UserBindings, movedBindings)
	targetReg.Grants = mergeGrants(targetReg.Grants, movedGrants)
	targetReg.Cards = mergeCards(targetReg.Cards, movedCards)
	sourceReg.UserBindings = keptBindings
	sourceReg.Grants = keptGrants
	sourceReg.Cards = keptCards
	if err := saveLLMRegistrySetting(ctx, tx, sourceKey, sourceReg); err != nil {
		return nil, err
	}
	if err := saveLLMRegistrySetting(ctx, tx, targetKey, targetReg); err != nil {
		return nil, err
	}
	updates["llm_service_user_bindings"] = int64(len(movedBindings))
	updates["llm_service_grants"] = int64(len(movedGrants))
	updates["llm_service_redeemed_cards"] = int64(len(movedCards))
	return updates, nil
}

func copyLLMServiceGroupBinding(ctx context.Context, tx *sql.Tx, fromTenant, toTenant, sourceGroupID, targetGroupID string) (int64, error) {
	if sourceGroupID == "" || targetGroupID == "" {
		return 0, nil
	}
	sourceKey := tenantRegistryKey(fromTenant)
	sourceRaw, err := getSystemSetting(ctx, tx, sourceKey)
	if err != nil || strings.TrimSpace(sourceRaw) == "" {
		return 0, err
	}
	sourceReg, err := decodeLLMRegistry(sourceRaw)
	if err != nil {
		return 0, fmt.Errorf("decode source llm registry: %w", err)
	}
	var bindings []llmservice.GroupBinding
	for _, binding := range sourceReg.GroupBindings {
		if strings.TrimSpace(binding.GroupID) != sourceGroupID {
			continue
		}
		binding.GroupID = targetGroupID
		bindings = append(bindings, binding)
	}
	if len(bindings) == 0 {
		return 0, nil
	}
	targetKey := tenantRegistryKey(toTenant)
	targetReg, err := loadTargetLLMRegistry(ctx, tx, targetKey, sourceReg)
	if err != nil {
		return 0, err
	}
	before := len(targetReg.GroupBindings)
	targetReg.GroupBindings = mergeGroupBindings(targetReg.GroupBindings, bindings)
	if err := saveLLMRegistrySetting(ctx, tx, targetKey, targetReg); err != nil {
		return 0, err
	}
	return int64(len(targetReg.GroupBindings) - before), nil
}

func copyLLMRegistryBase(ctx context.Context, tx *sql.Tx, fromTenant, toTenant string) (int64, error) {
	sourceRaw, err := getSystemSetting(ctx, tx, tenantRegistryKey(fromTenant))
	if err != nil || strings.TrimSpace(sourceRaw) == "" {
		return 0, err
	}
	sourceReg, err := decodeLLMRegistry(sourceRaw)
	if err != nil {
		return 0, fmt.Errorf("decode source llm registry: %w", err)
	}
	targetKey := tenantRegistryKey(toTenant)
	targetRaw, err := getSystemSetting(ctx, tx, targetKey)
	if err != nil {
		return 0, err
	}
	if strings.TrimSpace(targetRaw) != "" {
		return 0, nil
	}
	base := cloneLLMRegistryBase(sourceReg)
	if err := saveLLMRegistrySetting(ctx, tx, targetKey, base); err != nil {
		return 0, err
	}
	return 1, nil
}

func loadTargetLLMRegistry(ctx context.Context, tx *sql.Tx, targetKey string, sourceBase *llmservice.Registry) (*llmservice.Registry, error) {
	targetRaw, err := getSystemSetting(ctx, tx, targetKey)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(targetRaw) == "" {
		return cloneLLMRegistryBase(sourceBase), nil
	}
	return decodeLLMRegistry(targetRaw)
}

func decodeLLMRegistry(raw string) (*llmservice.Registry, error) {
	var reg llmservice.Registry
	if strings.TrimSpace(raw) != "" {
		if err := json.Unmarshal([]byte(raw), &reg); err != nil {
			return nil, err
		}
	}
	reg.Normalize()
	return &reg, nil
}

func cloneLLMRegistryBase(source *llmservice.Registry) *llmservice.Registry {
	if source == nil {
		reg := &llmservice.Registry{}
		reg.Normalize()
		return reg
	}
	base := *source
	base.ModelServiceGroups = append([]llmservice.ModelServiceGroup(nil), source.ModelServiceGroups...)
	base.GlobalServiceGroupIDs = append([]string(nil), source.GlobalServiceGroupIDs...)
	base.GroupBindings = nil
	base.UserBindings = nil
	base.Cards = nil
	base.Grants = nil
	base.Normalize()
	return &base
}

func saveLLMRegistrySetting(ctx context.Context, tx *sql.Tx, key string, reg *llmservice.Registry) error {
	if reg == nil {
		reg = &llmservice.Registry{}
	}
	reg.Normalize()
	data, err := json.Marshal(reg)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now')) ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`, key, string(data))
	return err
}

func getSystemSetting(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT value_json FROM system_settings WHERE key = ?`, key).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return raw, err
}

func moveUserSystemSettings(ctx context.Context, tx *sql.Tx, fromTenant, toTenant, userID string) (map[string]int64, error) {
	updates := map[string]int64{}
	keys := []string{"shortcuts_" + strings.TrimSpace(userID)}
	for _, tenantKey := range keys {
		if strings.TrimSpace(tenantKey) == "" {
			continue
		}
		count, err := moveTenantSystemSetting(ctx, tx, fromTenant, toTenant, tenantKey)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			updates["system_settings:"+tenantKey] = count
		}
	}
	return updates, nil
}

func moveTenantSystemSetting(ctx context.Context, tx *sql.Tx, fromTenant, toTenant, tenantKey string) (int64, error) {
	sourceKey := tenantSystemSettingStorageKey(fromTenant, tenantKey)
	targetKey := tenantSystemSettingStorageKey(toTenant, tenantKey)
	if sourceKey == targetKey {
		return 0, nil
	}
	raw, err := getSystemSetting(ctx, tx, sourceKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return 0, err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now')) ON CONFLICT(key) DO UPDATE SET value_json = excluded.value_json, updated_at = excluded.updated_at`, targetKey, raw); err != nil {
		return 0, err
	}
	res, err := tx.ExecContext(ctx, `DELETE FROM system_settings WHERE key = ?`, sourceKey)
	if err != nil {
		return 0, err
	}
	return rowsAffected(res), nil
}

func tenantSystemSettingStorageKey(tenantID, tenantKey string) string {
	tenantID = store.NormalizeTenantID(tenantID)
	tenantKey = strings.TrimSpace(tenantKey)
	if tenantID == store.DefaultTenantID {
		return tenantKey
	}
	return "tenant:" + tenantID + ":" + tenantKey
}
func moveWorkflowRuntime(ctx context.Context, tx *sql.Tx, fromTenant, toTenant, userID string) (map[string]int64, error) {
	updates := map[string]int64{}
	ok, err := tableHasColumns(ctx, tx, "workflow_instances", "tenant_id", "instance_data")
	if err != nil || !ok {
		return updates, err
	}
	instanceIDs, err := workflowInstanceIDsForUser(ctx, tx, fromTenant, userID)
	if err != nil || len(instanceIDs) == 0 {
		return updates, err
	}
	where, args := tenantMigrationInClause("id", instanceIDs)
	args = append([]any{toTenant}, args...)
	res, err := tx.ExecContext(ctx, `UPDATE workflow_instances SET tenant_id = ? WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	updates["workflow_instances"] = rowsAffected(res)

	confirmOK, err := tableHasColumns(ctx, tx, "confirmations", "tenant_id", "instance_id")
	if err != nil {
		return nil, err
	}
	if confirmOK {
		where, args = tenantMigrationInClause("instance_id", instanceIDs)
		args = append([]any{toTenant, fromTenant}, args...)
		res, err = tx.ExecContext(ctx, `UPDATE confirmations SET tenant_id = ? WHERE tenant_id = ? AND `+where, args...)
		if err != nil {
			return nil, err
		}
		updates["confirmations"] = rowsAffected(res)
	}

	auditOK, err := tableHasColumns(ctx, tx, "approval_audit_trail", "tenant_id", "instance_id")
	if err != nil {
		return nil, err
	}
	if auditOK {
		if err := withApprovalAuditTrailTenantRewrite(ctx, tx, func() error {
			where, args = tenantMigrationInClause("instance_id", instanceIDs)
			args = append([]any{toTenant, fromTenant}, args...)
			res, err = tx.ExecContext(ctx, `UPDATE approval_audit_trail SET tenant_id = ? WHERE tenant_id = ? AND `+where, args...)
			if err != nil {
				return err
			}
			updates["approval_audit_trail"] = rowsAffected(res)
			return nil
		}); err != nil {
			return nil, err
		}
	}
	return updates, nil
}

func workflowInstanceIDsForUser(ctx context.Context, tx *sql.Tx, tenantID, userID string) ([]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM workflow_instances
		WHERE tenant_id = ? AND (
			json_extract(instance_data, '$.initiator_id') = ? OR
			json_extract(instance_data, '$.requester_id') = ? OR
			json_extract(instance_data, '$.approver_id') = ?
		)`, tenantID, userID, userID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func withApprovalAuditTrailTenantRewrite(ctx context.Context, tx *sql.Tx, fn func() error) error {
	if _, err := tx.ExecContext(ctx, `DROP TRIGGER IF EXISTS trg_audit_trail_no_update`); err != nil {
		return err
	}
	if err := fn(); err != nil {
		_ = recreateApprovalAuditTrailNoUpdateTrigger(ctx, tx)
		return err
	}
	return recreateApprovalAuditTrailNoUpdateTrigger(ctx, tx)
}

func recreateApprovalAuditTrailNoUpdateTrigger(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TRIGGER IF NOT EXISTS trg_audit_trail_no_update
		BEFORE UPDATE ON approval_audit_trail
		BEGIN
		  SELECT RAISE(ABORT, 'approval_audit_trail is immutable: UPDATE not allowed');
		END;`)
	return err
}

func tenantMigrationInClause(column string, values []string) (string, []any) {
	placeholders := make([]string, 0, len(values))
	args := make([]any, 0, len(values))
	for _, value := range values {
		placeholders = append(placeholders, "?")
		args = append(args, value)
	}
	return column + " IN (" + strings.Join(placeholders, ",") + ")", args
}

func rowsAffected(res sql.Result) int64 {
	if res == nil {
		return 0
	}
	n, _ := res.RowsAffected()
	return n
}

type tenantMigrationBindingInfo struct {
	Email    string `json:"email,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
}

type tenantMigrationFeishuBindingInfo struct {
	OpenID   string `json:"open_id,omitempty"`
	Email    string `json:"email,omitempty"`
	TenantID string `json:"tenant_id,omitempty"`
	Mobile   string `json:"mobile,omitempty"`
}

func moveIMBindings(ctx context.Context, tx *sql.Tx, fromTenant, toTenant, email string) (map[string]int64, error) {
	updates := map[string]int64{}
	for _, key := range []string{"dingtalk_bindings", "wecom_bindings", "qqbot_bindings"} {
		count, err := moveStringIMBindingSetting(ctx, tx, key, fromTenant, toTenant, email)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			updates["im_bindings:"+key] = count
		}
	}
	remoteUpdates, err := moveRemoteGatewayBindingSettings(ctx, tx, fromTenant, toTenant, email)
	if err != nil {
		return nil, err
	}
	for key, count := range remoteUpdates {
		updates[key] = count
	}
	feishuCount, err := moveFeishuOpenIDMap(ctx, tx, fromTenant, toTenant, email)
	if err != nil {
		return nil, err
	}
	if feishuCount > 0 {
		updates["im_bindings:feishu_openid_map"] = feishuCount
	}
	return updates, nil
}

func moveRemoteGatewayBindingSettings(ctx context.Context, tx *sql.Tx, fromTenant, toTenant, email string) (map[string]int64, error) {
	updates := map[string]int64{}
	rows, err := tx.QueryContext(ctx, `SELECT key FROM system_settings WHERE key LIKE 'im\_%\_bindings' ESCAPE '\'`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, key := range keys {
		count, err := moveRemoteGatewayBindingSetting(ctx, tx, key, fromTenant, toTenant, email)
		if err != nil {
			return nil, err
		}
		if count > 0 {
			updates["im_bindings:"+key] = count
		}
	}
	return updates, nil
}

func moveRemoteGatewayBindingSetting(ctx context.Context, tx *sql.Tx, key, fromTenant, toTenant, email string) (int64, error) {
	raw, err := getSystemSetting(ctx, tx, key)
	if err != nil || strings.TrimSpace(raw) == "" {
		return 0, err
	}
	var bindings map[string]string
	if err := json.Unmarshal([]byte(raw), &bindings); err != nil {
		return 0, nil
	}
	out := make(map[string]string, len(bindings))
	put := func(bindingKey, value string) error {
		if existing, ok := out[bindingKey]; ok && existing != value {
			return fmt.Errorf("remote IM binding collision for setting %s key %q", key, bindingKey)
		}
		out[bindingKey] = value
		return nil
	}
	var moved int64
	for platformUID, value := range bindings {
		info := decodeTenantMigrationBindingValue(value)
		bindingTenantID := tenantMigrationRemoteTenantFromKeyValue(platformUID, value)
		plainUID := tenantMigrationRemotePlatformUIDFromKey(platformUID)
		if bindingTenantID == fromTenant && strings.EqualFold(info.Email, email) {
			if err := put(tenantMigrationRemoteBindingKey(toTenant, plainUID), encodeTenantMigrationBindingValue(toTenant, email)); err != nil {
				return 0, err
			}
			moved++
			continue
		}
		if err := put(tenantMigrationRemoteBindingKey(bindingTenantID, plainUID), value); err != nil {
			return 0, err
		}
	}
	if moved == 0 {
		return 0, nil
	}
	data, err := json.Marshal(out)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE system_settings SET value_json = ?, updated_at = datetime('now') WHERE key = ?`, string(data), key)
	return moved, err
}
func moveStringIMBindingSetting(ctx context.Context, tx *sql.Tx, key, fromTenant, toTenant, email string) (int64, error) {
	raw, err := getSystemSetting(ctx, tx, key)
	if err != nil || strings.TrimSpace(raw) == "" {
		return 0, err
	}
	var bindings map[string]string
	if err := json.Unmarshal([]byte(raw), &bindings); err != nil {
		return 0, nil
	}
	var moved int64
	for platformUID, value := range bindings {
		info := decodeTenantMigrationBindingValue(value)
		if info.TenantID == fromTenant && strings.EqualFold(info.Email, email) {
			bindings[platformUID] = encodeTenantMigrationBindingValue(toTenant, email)
			moved++
		}
	}
	if moved == 0 {
		return 0, nil
	}
	data, err := json.Marshal(bindings)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE system_settings SET value_json = ?, updated_at = datetime('now') WHERE key = ?`, string(data), key)
	return moved, err
}

func moveFeishuOpenIDMap(ctx context.Context, tx *sql.Tx, fromTenant, toTenant, email string) (int64, error) {
	const key = "feishu_openid_map"
	raw, err := getSystemSetting(ctx, tx, key)
	if err != nil || strings.TrimSpace(raw) == "" {
		return 0, err
	}
	var entries map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return 0, nil
	}
	out := make(map[string]tenantMigrationFeishuBindingInfo, len(entries))
	var moved int64
	for entryKey, entryRaw := range entries {
		info, ok := decodeTenantMigrationFeishuBinding(entryKey, entryRaw)
		if !ok || info.OpenID == "" {
			continue
		}
		info.TenantID = store.NormalizeTenantID(info.TenantID)
		info.Email = normalizeTenantMigrationEmail(info.Email)
		if info.TenantID == fromTenant && strings.EqualFold(info.Email, email) {
			info.TenantID = toTenant
			info.Email = normalizeTenantMigrationEmail(email)
			out[tenantMigrationFeishuKey(toTenant, email)] = info
			moved++
			continue
		}
		out[tenantMigrationFeishuKey(info.TenantID, info.Email)] = info
	}
	if moved == 0 {
		return 0, nil
	}
	data, err := json.Marshal(out)
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE system_settings SET value_json = ?, updated_at = datetime('now') WHERE key = ?`, string(data), key)
	return moved, err
}

func tenantMigrationRemoteBindingKey(tenantID, platformUID string) string {
	tenantID = store.NormalizeTenantID(tenantID)
	platformUID = strings.TrimSpace(platformUID)
	if tenantID == store.DefaultTenantID {
		return platformUID
	}
	return tenantID + "\x00" + platformUID
}

func tenantMigrationRemoteTenantFromKeyValue(key, value string) string {
	if tenantID, _, ok := strings.Cut(key, "\x00"); ok {
		return store.NormalizeTenantID(tenantID)
	}
	return decodeTenantMigrationBindingValue(value).TenantID
}

func tenantMigrationRemotePlatformUIDFromKey(key string) string {
	if _, platformUID, ok := strings.Cut(key, "\x00"); ok {
		return platformUID
	}
	return strings.TrimSpace(key)
}
func decodeTenantMigrationBindingValue(raw string) tenantMigrationBindingInfo {
	var info tenantMigrationBindingInfo
	if strings.HasPrefix(strings.TrimSpace(raw), "{") && json.Unmarshal([]byte(raw), &info) == nil {
		info.Email = normalizeTenantMigrationEmail(info.Email)
		info.TenantID = store.NormalizeTenantID(info.TenantID)
		return info
	}
	return tenantMigrationBindingInfo{Email: normalizeTenantMigrationEmail(raw), TenantID: store.DefaultTenantID}
}

func encodeTenantMigrationBindingValue(tenantID, email string) string {
	tenantID = store.NormalizeTenantID(tenantID)
	email = normalizeTenantMigrationEmail(email)
	if tenantID == store.DefaultTenantID {
		return email
	}
	data, _ := json.Marshal(tenantMigrationBindingInfo{Email: email, TenantID: tenantID})
	return string(data)
}

func decodeTenantMigrationFeishuBinding(entryKey string, raw json.RawMessage) (tenantMigrationFeishuBindingInfo, bool) {
	var info tenantMigrationFeishuBindingInfo
	if err := json.Unmarshal(raw, &info); err == nil && info.OpenID != "" {
		if strings.TrimSpace(info.Email) == "" {
			info.Email = tenantMigrationFeishuEmailFromKey(entryKey)
		}
		return info, true
	}
	var openID string
	if err := json.Unmarshal(raw, &openID); err != nil || strings.TrimSpace(openID) == "" {
		return tenantMigrationFeishuBindingInfo{}, false
	}
	return tenantMigrationFeishuBindingInfo{OpenID: strings.TrimSpace(openID), Email: tenantMigrationFeishuEmailFromKey(entryKey), TenantID: store.DefaultTenantID}, true
}

func tenantMigrationFeishuKey(tenantID, email string) string {
	tenantID = store.NormalizeTenantID(tenantID)
	email = normalizeTenantMigrationEmail(email)
	if tenantID == store.DefaultTenantID {
		return email
	}
	return tenantID + "\x00" + email
}

func tenantMigrationFeishuEmailFromKey(key string) string {
	if i := strings.IndexByte(key, '\x00'); i >= 0 {
		return normalizeTenantMigrationEmail(key[i+1:])
	}
	return normalizeTenantMigrationEmail(key)
}

func normalizeTenantMigrationEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}
func tenantRegistryKey(tenantID string) string {
	tenantID = store.NormalizeTenantID(tenantID)
	if tenantID == store.DefaultTenantID {
		return llmservice.RegistryKey
	}
	return "tenant:" + tenantID + ":" + llmservice.RegistryKey
}

func mergeGroupBindings(existing, incoming []llmservice.GroupBinding) []llmservice.GroupBinding {
	out := make([]llmservice.GroupBinding, 0, len(existing)+len(incoming))
	seen := map[string]int{}
	for _, item := range existing {
		key := strings.TrimSpace(item.GroupID)
		seen[key] = len(out)
		out = append(out, item)
	}
	for _, item := range incoming {
		key := strings.TrimSpace(item.GroupID)
		if idx, ok := seen[key]; ok {
			out[idx] = item
			continue
		}
		seen[key] = len(out)
		out = append(out, item)
	}
	return out
}

func mergeUserBindings(existing, incoming []llmservice.UserBinding) []llmservice.UserBinding {
	out := make([]llmservice.UserBinding, 0, len(existing)+len(incoming))
	seen := map[string]int{}
	for _, item := range existing {
		key := strings.ToLower(strings.TrimSpace(item.Email))
		seen[key] = len(out)
		out = append(out, item)
	}
	for _, item := range incoming {
		key := strings.ToLower(strings.TrimSpace(item.Email))
		if idx, ok := seen[key]; ok {
			out[idx] = item
			continue
		}
		seen[key] = len(out)
		out = append(out, item)
	}
	return out
}

func mergeGrants(existing, incoming []llmservice.Grant) []llmservice.Grant {
	out := append([]llmservice.Grant(nil), existing...)
	seen := map[string]struct{}{}
	for _, item := range existing {
		seen[item.ID] = struct{}{}
	}
	for _, item := range incoming {
		if _, ok := seen[item.ID]; ok && item.ID != "" {
			continue
		}
		out = append(out, item)
		seen[item.ID] = struct{}{}
	}
	return out
}

func mergeCards(existing, incoming []llmservice.RechargeCard) []llmservice.RechargeCard {
	out := append([]llmservice.RechargeCard(nil), existing...)
	seen := map[string]struct{}{}
	for _, item := range existing {
		seen[item.ID] = struct{}{}
	}
	for _, item := range incoming {
		if _, ok := seen[item.ID]; ok && item.ID != "" {
			continue
		}
		out = append(out, item)
		seen[item.ID] = struct{}{}
	}
	return out
}

func remapRegistryGroupIDs(reg *llmservice.Registry, groupIDMap map[string]string) {
	if reg == nil || len(groupIDMap) == 0 {
		return
	}
	for i := range reg.GroupBindings {
		if mapped := groupIDMap[strings.TrimSpace(reg.GroupBindings[i].GroupID)]; mapped != "" {
			reg.GroupBindings[i].GroupID = mapped
		}
	}
}

func mergeFullLLMRegistry(target, source *llmservice.Registry) {
	if target == nil || source == nil {
		return
	}
	target.ModelServiceGroups = mergeModelServiceGroups(target.ModelServiceGroups, source.ModelServiceGroups)
	target.GlobalServiceGroupIDs = mergeStringIDs(target.GlobalServiceGroupIDs, source.GlobalServiceGroupIDs)
	target.GroupBindings = mergeGroupBindings(target.GroupBindings, source.GroupBindings)
	target.UserBindings = mergeUserBindings(target.UserBindings, source.UserBindings)
	target.Cards = mergeCards(target.Cards, source.Cards)
	target.Grants = mergeGrants(target.Grants, source.Grants)
	target.DefaultNewUserServiceGroups = mergeStringIDs(target.DefaultNewUserServiceGroups, source.DefaultNewUserServiceGroups)
	if target.DefaultNewUserDurationDays == 0 {
		target.DefaultNewUserDurationDays = source.DefaultNewUserDurationDays
	}
	if target.DefaultNewUserCredits == 0 {
		target.DefaultNewUserCredits = source.DefaultNewUserCredits
	}
	if target.TokensPerCredit == 0 {
		target.TokensPerCredit = source.TokensPerCredit
	}
	target.Normalize()
}

func mergeModelServiceGroups(existing, incoming []llmservice.ModelServiceGroup) []llmservice.ModelServiceGroup {
	out := append([]llmservice.ModelServiceGroup(nil), existing...)
	seen := map[string]struct{}{}
	for _, item := range existing {
		seen[strings.TrimSpace(item.ID)] = struct{}{}
	}
	for _, item := range incoming {
		key := strings.TrimSpace(item.ID)
		if _, ok := seen[key]; ok && key != "" {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func mergeStringIDs(existing, incoming []string) []string {
	out := append([]string(nil), existing...)
	seen := map[string]struct{}{}
	for _, item := range existing {
		seen[strings.TrimSpace(item)] = struct{}{}
	}
	for _, item := range incoming {
		key := strings.TrimSpace(item)
		if key == "" {
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func copyTenantConfig(ctx context.Context, tx *sql.Tx, fromTenant, toTenant string) (map[string]int64, error) {
	copies := map[string]int64{}
	settings, err := copySystemSettingsForTenant(ctx, tx, fromTenant, toTenant)
	if err != nil {
		return nil, err
	}
	copies["system_settings"] = settings
	authz, err := copyDigitalEmployeeAuthorization(ctx, tx, fromTenant, toTenant)
	if err != nil {
		return nil, err
	}
	copies["tenant_digital_employee_authorizations"] = authz
	llmBase, err := copyLLMRegistryBase(ctx, tx, fromTenant, toTenant)
	if err != nil {
		return nil, err
	}
	copies["llm_service_registry_base"] = llmBase
	return copies, nil
}

func copySystemSettingsForTenant(ctx context.Context, tx *sql.Tx, fromTenant, toTenant string) (int64, error) {
	prefix := "tenant:" + fromTenant + ":"
	rows, err := tx.QueryContext(ctx, `SELECT key, value_json FROM system_settings`)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var copied int64
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return 0, err
		}
		tenantKey := ""
		switch {
		case fromTenant == store.DefaultTenantID && !strings.HasPrefix(key, "tenant:") && !isGlobalTenantMigrationSettingKey(key) && key != llmservice.RegistryKey:
			tenantKey = key
		case fromTenant != store.DefaultTenantID && strings.HasPrefix(key, prefix):
			tenantKey = strings.TrimPrefix(key, prefix)
			if tenantKey == llmservice.RegistryKey {
				continue
			}
		}
		if tenantKey == "" || isGlobalTenantMigrationSettingKey(tenantKey) {
			continue
		}
		targetKey := "tenant:" + toTenant + ":" + tenantKey
		res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO system_settings (key, value_json, updated_at) VALUES (?, ?, datetime('now'))`, targetKey, value)
		if err != nil {
			return 0, err
		}
		n, _ := res.RowsAffected()
		copied += n
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	return copied, nil
}

func copyDigitalEmployeeAuthorization(ctx context.Context, tx *sql.Tx, fromTenant, toTenant string) (int64, error) {
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tenant_digital_employee_authorizations
		(tenant_id, enabled, quota, used, valid_from, valid_until, status, source, metadata_json, updated_by_admin_id, updated_at, created_at)
		SELECT ?, enabled, quota, 0, valid_from, valid_until, status, 'tenant_migration', metadata_json, updated_by_admin_id, datetime('now'), datetime('now')
		FROM tenant_digital_employee_authorizations WHERE tenant_id = ?`, toTenant, fromTenant)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func isGlobalTenantMigrationSettingKey(key string) bool {
	switch strings.TrimSpace(key) {
	case "", "center_base_url", "center_registration", "admin_email", "hub_installation_id", "server_public_base_url":
		return true
	default:
		return false
	}
}

func userSecurityGroupID(ctx context.Context, tx *sql.Tx, tenantID, email string) (string, error) {
	ok, err := tableHasColumns(ctx, tx, "security_group_members", "tenant_id", "group_id")
	if err != nil || !ok {
		return "", err
	}
	var groupID string
	err = tx.QueryRowContext(ctx, `SELECT group_id FROM security_group_members WHERE tenant_id = ? AND lower(email) = lower(?)`, tenantID, email).Scan(&groupID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return groupID, err
}

func ensureSecurityGroupPath(ctx context.Context, tx *sql.Tx, fromTenant, toTenant, sourceGroupID string, cache map[string]string) (string, error) {
	if cached := cache[sourceGroupID]; cached != "" {
		return cached, nil
	}
	group, err := getSecurityGroup(ctx, tx, fromTenant, sourceGroupID)
	if err != nil || group == nil {
		return "", err
	}
	targetParentID := ""
	if group.ParentID != "" {
		targetParentID, err = ensureSecurityGroupPath(ctx, tx, fromTenant, toTenant, group.ParentID, cache)
		if err != nil {
			return "", err
		}
	}
	existingID, err := findSecurityGroupByNameParent(ctx, tx, toTenant, group.Name, targetParentID)
	if err != nil {
		return "", err
	}
	if existingID != "" {
		cache[sourceGroupID] = existingID
		return existingID, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	targetID := "sg_" + randomHex(12)
	_, err = tx.ExecContext(ctx, `INSERT INTO security_groups (tenant_id, id, name, parent_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?)`, toTenant, targetID, group.Name, targetParentID, now, now)
	if err != nil {
		return "", err
	}
	if err := copySecurityPolicy(ctx, tx, fromTenant, sourceGroupID, toTenant, targetID); err != nil {
		return "", err
	}
	cache[sourceGroupID] = targetID
	return targetID, nil
}

func getSecurityGroup(ctx context.Context, tx *sql.Tx, tenantID, groupID string) (*securityGroupRow, error) {
	ok, err := tableHasColumns(ctx, tx, "security_groups", "tenant_id")
	if err != nil || !ok {
		return nil, err
	}
	var row securityGroupRow
	err = tx.QueryRowContext(ctx, `SELECT id, name, parent_id, created_at, updated_at FROM security_groups WHERE tenant_id = ? AND id = ?`, tenantID, groupID).Scan(&row.ID, &row.Name, &row.ParentID, &row.CreatedAt, &row.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func findSecurityGroupByNameParent(ctx context.Context, tx *sql.Tx, tenantID, name, parentID string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT id FROM security_groups WHERE tenant_id = ? AND name = ? AND parent_id = ? LIMIT 1`, tenantID, name, parentID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}

func copySecurityPolicy(ctx context.Context, tx *sql.Tx, fromTenant, fromGroupID, toTenant, toGroupID string) error {
	ok, err := tableHasColumns(ctx, tx, "security_policies", "tenant_id")
	if err != nil || !ok {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO security_policies (tenant_id, group_id, policy_json, updated_at)
		SELECT ?, ?, policy_json, datetime('now') FROM security_policies WHERE tenant_id = ? AND group_id = ?`, toTenant, toGroupID, fromTenant, fromGroupID)
	return err
}

func machineIDsForTenantUser(ctx context.Context, tx *sql.Tx, tenantID, userID string) ([]string, error) {
	ok, err := tableHasColumns(ctx, tx, "machines", "tenant_id", "user_id")
	if err != nil || !ok {
		return nil, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM machines WHERE tenant_id = ? AND user_id = ?`, tenantID, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if strings.TrimSpace(id) != "" {
			ids = append(ids, strings.TrimSpace(id))
		}
	}
	return ids, rows.Err()
}

func moveA2AGroupState(ctx context.Context, tx *sql.Tx, fromTenant, toTenant string, machineIDs []string) (map[string]int64, error) {
	updates := map[string]int64{}
	machineSet := stringSet(machineIDs)
	if len(machineSet) == 0 {
		return updates, nil
	}
	profileCount, err := moveA2AProfiles(ctx, tx, fromTenant, toTenant, machineIDs)
	if err != nil {
		return nil, err
	}
	if profileCount > 0 {
		updates["a2a_group_profiles"] = profileCount
	}
	sessionIDs, sessionCount, err := moveA2ASessions(ctx, tx, fromTenant, toTenant, machineSet)
	if err != nil {
		return nil, err
	}
	if sessionCount > 0 {
		updates["a2a_group_sessions"] = sessionCount
	}
	inviteCount, err := moveA2AInvites(ctx, tx, fromTenant, toTenant, machineSet, sessionIDs)
	if err != nil {
		return nil, err
	}
	if inviteCount > 0 {
		updates["a2a_group_invites"] = inviteCount
	}
	return updates, nil
}

func moveA2AProfiles(ctx context.Context, tx *sql.Tx, fromTenant, toTenant string, machineIDs []string) (int64, error) {
	ok, err := tableHasColumns(ctx, tx, "a2a_group_profiles", "tenant_id", "agent_id", "profile_json")
	if err != nil || !ok {
		return 0, err
	}
	where, args := tenantMigrationInClause("agent_id", machineIDs)
	selectArgs := append([]any{fromTenant}, args...)
	rows, err := tx.QueryContext(ctx, `SELECT agent_id, display_name, discoverable, available, updated_at, profile_json FROM a2a_group_profiles WHERE tenant_id = ? AND `+where, selectArgs...)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	var moved int64
	for rows.Next() {
		var agentID, displayName, updatedAt, raw string
		var discoverable, available int
		if err := rows.Scan(&agentID, &displayName, &discoverable, &available, &updatedAt, &raw); err != nil {
			return 0, err
		}
		patched := patchJSONTenantID(raw, toTenant)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO a2a_group_profiles (tenant_id, agent_id, display_name, discoverable, available, updated_at, profile_json) VALUES (?, ?, ?, ?, ?, ?, ?)`, toTenant, agentID, displayName, discoverable, available, updatedAt, patched); err != nil {
			return 0, err
		}
		moved++
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	deleteArgs := append([]any{fromTenant}, args...)
	if _, err := tx.ExecContext(ctx, `DELETE FROM a2a_group_profiles WHERE tenant_id = ? AND `+where, deleteArgs...); err != nil {
		return 0, err
	}
	return moved, nil
}

func moveA2ASessions(ctx context.Context, tx *sql.Tx, fromTenant, toTenant string, machineSet map[string]struct{}) ([]string, int64, error) {
	ok, err := tableHasColumns(ctx, tx, "a2a_group_sessions", "tenant_id", "session_id", "session_json")
	if err != nil || !ok {
		return nil, 0, err
	}
	rows, err := tx.QueryContext(ctx, `SELECT session_id, status, topic, created_at, updated_at, session_json FROM a2a_group_sessions WHERE tenant_id = ?`, fromTenant)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	type sessionRow struct {
		id, status, topic, createdAt, updatedAt, raw string
	}
	var candidates []sessionRow
	for rows.Next() {
		var row sessionRow
		if err := rows.Scan(&row.id, &row.status, &row.topic, &row.createdAt, &row.updatedAt, &row.raw); err != nil {
			return nil, 0, err
		}
		if a2aSessionReferencesAny(row.raw, machineSet) {
			candidates = append(candidates, row)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	movedIDs := make([]string, 0, len(candidates))
	for _, row := range candidates {
		patched := patchJSONTenantID(row.raw, toTenant)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO a2a_group_sessions (tenant_id, session_id, status, topic, created_at, updated_at, session_json) VALUES (?, ?, ?, ?, ?, ?, ?)`, toTenant, row.id, row.status, row.topic, row.createdAt, row.updatedAt, patched); err != nil {
			return nil, 0, err
		}
		movedIDs = append(movedIDs, row.id)
	}
	if len(movedIDs) == 0 {
		return nil, 0, nil
	}
	where, args := tenantMigrationInClause("session_id", movedIDs)
	deleteArgs := append([]any{fromTenant}, args...)
	if _, err := tx.ExecContext(ctx, `DELETE FROM a2a_group_sessions WHERE tenant_id = ? AND `+where, deleteArgs...); err != nil {
		return nil, 0, err
	}
	return movedIDs, int64(len(movedIDs)), nil
}

func moveA2AInvites(ctx context.Context, tx *sql.Tx, fromTenant, toTenant string, machineSet map[string]struct{}, movedSessionIDs []string) (int64, error) {
	ok, err := tableHasColumns(ctx, tx, "a2a_group_invites", "tenant_id", "invite_id", "session_id", "to_id", "from_id", "invite_json")
	if err != nil || !ok {
		return 0, err
	}
	movedSessions := stringSet(movedSessionIDs)
	rows, err := tx.QueryContext(ctx, `SELECT invite_id, session_id, to_id, from_id, role, status, created_at, responded_at, invite_json FROM a2a_group_invites WHERE tenant_id = ?`, fromTenant)
	if err != nil {
		return 0, err
	}
	defer rows.Close()
	type inviteRow struct {
		id, sessionID, toID, fromID, role, status, createdAt, respondedAt, raw string
	}
	var candidates []inviteRow
	for rows.Next() {
		var row inviteRow
		if err := rows.Scan(&row.id, &row.sessionID, &row.toID, &row.fromID, &row.role, &row.status, &row.createdAt, &row.respondedAt, &row.raw); err != nil {
			return 0, err
		}
		_, sessionMoved := movedSessions[strings.TrimSpace(row.sessionID)]
		_, toMoved := machineSet[strings.TrimSpace(row.toID)]
		_, fromMoved := machineSet[strings.TrimSpace(row.fromID)]
		if sessionMoved || toMoved || fromMoved {
			candidates = append(candidates, row)
		}
	}
	if err := rows.Err(); err != nil {
		return 0, err
	}
	movedIDs := make([]string, 0, len(candidates))
	for _, row := range candidates {
		patched := patchJSONTenantID(row.raw, toTenant)
		if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO a2a_group_invites (tenant_id, invite_id, session_id, to_id, from_id, role, status, created_at, responded_at, invite_json) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, toTenant, row.id, row.sessionID, row.toID, row.fromID, row.role, row.status, row.createdAt, row.respondedAt, patched); err != nil {
			return 0, err
		}
		movedIDs = append(movedIDs, row.id)
	}
	if len(movedIDs) == 0 {
		return 0, nil
	}
	where, args := tenantMigrationInClause("invite_id", movedIDs)
	deleteArgs := append([]any{fromTenant}, args...)
	if _, err := tx.ExecContext(ctx, `DELETE FROM a2a_group_invites WHERE tenant_id = ? AND `+where, deleteArgs...); err != nil {
		return 0, err
	}
	return int64(len(movedIDs)), nil
}

func a2aSessionReferencesAny(raw string, machineSet map[string]struct{}) bool {
	var session struct {
		Participants []struct {
			ID string `json:"id"`
		} `json:"participants"`
		Messages []struct {
			FromID string   `json:"from_id"`
			ToIDs  []string `json:"to_ids"`
		} `json:"messages"`
		Proposals []struct {
			AuthorID string `json:"author_id"`
		} `json:"proposals"`
		Reviews []struct {
			ReviewerID string `json:"reviewer_id"`
		} `json:"reviews"`
		Decision *struct {
			DecidedBy []string `json:"decided_by"`
		} `json:"decision"`
		Escalation *struct {
			RaisedBy string `json:"raised_by"`
		} `json:"escalation"`
	}
	if json.Unmarshal([]byte(raw), &session) != nil {
		return false
	}
	for _, participant := range session.Participants {
		if _, ok := machineSet[strings.TrimSpace(participant.ID)]; ok {
			return true
		}
	}
	for _, msg := range session.Messages {
		if _, ok := machineSet[strings.TrimSpace(msg.FromID)]; ok {
			return true
		}
		for _, toID := range msg.ToIDs {
			if _, ok := machineSet[strings.TrimSpace(toID)]; ok {
				return true
			}
		}
	}
	for _, proposal := range session.Proposals {
		if _, ok := machineSet[strings.TrimSpace(proposal.AuthorID)]; ok {
			return true
		}
	}
	for _, review := range session.Reviews {
		if _, ok := machineSet[strings.TrimSpace(review.ReviewerID)]; ok {
			return true
		}
	}
	if session.Decision != nil {
		for _, decidedBy := range session.Decision.DecidedBy {
			if _, ok := machineSet[strings.TrimSpace(decidedBy)]; ok {
				return true
			}
		}
	}
	if session.Escalation != nil {
		_, ok := machineSet[strings.TrimSpace(session.Escalation.RaisedBy)]
		return ok
	}
	return false
}

func patchJSONTenantID(raw, tenantID string) string {
	var obj map[string]any
	if json.Unmarshal([]byte(raw), &obj) != nil {
		return raw
	}
	obj["tenant_id"] = tenantID
	data, err := json.Marshal(obj)
	if err != nil {
		return raw
	}
	return string(data)
}

func stringSet(values []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out[value] = struct{}{}
		}
	}
	return out
}
func randomHex(bytesLen int) string {
	buf := make([]byte, bytesLen)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func updateTenantForTable(ctx context.Context, tx *sql.Tx, table, toTenant, where string, args ...any) (int64, error) {
	ok, err := tableHasColumns(ctx, tx, table, "tenant_id")
	if err != nil || !ok {
		return 0, err
	}
	query := fmt.Sprintf("UPDATE %s SET tenant_id = ? WHERE %s", table, where)
	res, err := tx.ExecContext(ctx, query, append([]any{toTenant}, args...)...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func tableHasColumns(ctx context.Context, tx *sql.Tx, table string, columns ...string) (bool, error) {
	rows, err := tx.QueryContext(ctx, "PRAGMA table_info("+table+")")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	have := map[string]bool{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return false, err
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, col := range columns {
		if !have[col] {
			return false, nil
		}
	}
	return true, nil
}
