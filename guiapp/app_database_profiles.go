package guiapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/database"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"github.com/zalando/go-keyring"
)

// This file implements the desktop configuration-plane bindings for database
// profiles (design doc §8/§9: profile CRUD, connection test, keyring-backed
// secret writes). Secrets never enter AppConfig: the panel submits the
// password through DatabaseProfileSetSecret, which writes only to the OS
// keyring and stores a secret_ref pointer on the profile.
//
// Keyring entry naming follows the existing secret_ref convention
// "keyring://maclaw/database/<profile-id>", i.e. service "maclaw" and item
// "database/<profile-id>" — the same pair databaseKeyringSecret resolves.

const databaseKeyringService = "maclaw"

// databaseKeyringSet / databaseKeyringDelete are package-level indirections so
// tests can substitute a fake store; the OS keyring is unavailable in most
// test environments.
var (
	databaseKeyringSet    = keyring.Set
	databaseKeyringDelete = keyring.Delete
	databaseKeyringGet    = keyring.Get
)

func databaseProfileKeyringItem(profileID string) string {
	return "database/" + profileID
}

func databaseProfileManagedSecretRef(profileID string) string {
	return "keyring://" + databaseKeyringService + "/" + databaseProfileKeyringItem(profileID)
}

// databaseProfileIDRe keeps profile ids safe for keyring item names, URLs and
// audit references.
var databaseProfileIDRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func validDatabaseProfileID(id string) bool {
	return databaseProfileIDRe.MatchString(strings.TrimSpace(id))
}

// rejectInlineDatabaseSecrets refuses profile payloads that carry credentials
// under password/secret/token-style keys. Credentials must go through
// DatabaseProfileSetSecret (keyring); profiles only persist secret_ref.
func rejectInlineDatabaseSecrets(profile map[string]interface{}) error {
	for key := range profile {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "secret_ref" {
			continue
		}
		if strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
			return errors.New("inline credentials are not accepted; use the secret field (stored in the OS keyring)")
		}
	}
	return nil
}

func decodeDatabaseProfile(profile map[string]interface{}) (database.Profile, error) {
	raw, err := json.Marshal(profile)
	if err != nil {
		return database.Profile{}, errors.New("invalid profile payload")
	}
	var p database.Profile
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return database.Profile{}, fmt.Errorf("invalid profile payload: %v", err)
	}
	return p, nil
}

// databaseProfileSummary projects a profile to the non-sensitive shape the
// panel renders. DSN and secret values never leave the host; has_secret_ref /
// has_dsn let the UI distinguish bound from unbound profiles.
func databaseProfileSummary(p database.Profile) map[string]interface{} {
	status := "configured"
	if err := database.ValidateProfile(p); err != nil {
		status = "invalid"
	}
	return map[string]interface{}{
		"id":                     p.ID,
		"name":                   p.Name,
		"type":                   string(p.Type),
		"status":                 status,
		"host":                   p.Host,
		"port":                   p.Port,
		"database":               p.Database,
		"username":               p.Username,
		"default_schema":         p.DefaultSchema,
		"file_path":              p.FilePath,
		"sheet":                  p.Sheet,
		"schema_version":         p.SchemaVersion,
		"read_only":              p.ReadOnly,
		"write_enabled":          p.WriteEnabled,
		"allowed_tables":         p.AllowedTables,
		"masked_columns":         p.MaskedColumns,
		"has_secret_ref":         strings.TrimSpace(p.SecretRef) != "",
		"has_dsn":                strings.TrimSpace(p.DSN) != "",
		"allow_external_host":    p.AllowExternalHost,
		"disabled":               p.Disabled,
		"allow_ddl":              p.AllowDDL,
		"allowed_schemas":        p.AllowedSchemas,
		"denied_columns":         p.DeniedColumns,
		"allowed_operations":     p.AllowedOperations,
		"max_affected_rows":      p.MaxAffectedRows,
		"tls_mode":               p.TLS.Mode,
		"data_classification":    p.DataClassification,
		"ssh_session_id":         p.SSHSessionID,
		"replica_host":           p.ReplicaHost,
		"replica_port":           p.ReplicaPort,
		"replica_ssh_session_id": p.ReplicaSSHSessionID,
	}
}

func findDatabaseProfile(profiles []database.Profile, id string) (database.Profile, int) {
	for i, profile := range profiles {
		if profile.ID == id {
			return profile, i
		}
	}
	return database.Profile{}, -1
}

// refreshDatabaseProfilesRuntime pushes the persisted profile list into every
// live database manager so edits take effect without a restart. Refresh
// happens only after a successful config write so the runtime never diverges
// ahead of durable config.
func (a *App) refreshDatabaseProfilesRuntime() {
	if a == nil {
		return
	}
	handlers := map[*IMMessageHandler]struct{}{}
	if a.imHandler != nil {
		handlers[a.imHandler] = struct{}{}
	}
	if hub := a.hubClient(); hub != nil {
		if h := hub.currentIMHandler(); h != nil {
			handlers[h] = struct{}{}
		}
	}
	for h := range handlers {
		refreshDatabaseManager(a, h.databaseManager)
	}
}

// DatabaseProfilesList returns non-sensitive summaries of all configured
// database profiles. It never returns secret values, DSNs or keyring contents.
func (a *App) migrateDatabaseProfileSecretsLocked(cfg *corelib.AppConfig) error {
	if cfg == nil {
		return nil
	}
	next, err := database.MigratePlaintextSecrets(cfg.DatabaseProfiles, func(id, secret string) (string, error) {
		if err := databaseKeyringSet(databaseKeyringService, databaseProfileKeyringItem(id), secret); err != nil {
			return "", err
		}
		return databaseProfileManagedSecretRef(id), nil
	})
	if err != nil {
		return err
	}
	changed := false
	if len(next) != len(cfg.DatabaseProfiles) {
		changed = true
	} else {
		for i := range next {
			if next[i].Password != cfg.DatabaseProfiles[i].Password || next[i].SecretRef != cfg.DatabaseProfiles[i].SecretRef {
				changed = true
				break
			}
		}
	}
	if !changed {
		return nil
	}
	return a.PatchConfig(func(current *corelib.AppConfig) {
		current.DatabaseProfiles = next
	})
}

func (a *App) DatabaseProfilesList() ([]map[string]interface{}, error) {
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	if err := a.migrateDatabaseProfileSecretsLocked(&cfg); err != nil {
		return nil, err
	}
	cfg, err = a.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	items := make([]map[string]interface{}, 0, len(cfg.DatabaseProfiles))
	for _, profile := range cfg.DatabaseProfiles {
		items = append(items, databaseProfileSummary(profile))
	}
	return items, nil
}

// databaseProfilePayloadCarries reports whether the payload explicitly carries
// a key with a non-null value. Absent or JSON-null keys mean "not provided",
// which DatabaseProfileSave treats as "keep the existing value" for policy
// fields the caller did not send.
func databaseProfilePayloadCarries(profile map[string]interface{}, key string) bool {
	value, ok := profile[key]
	return ok && value != nil
}

// DatabaseProfileSave creates or updates (matched by id) a database profile
// after structural validation. Inline credential fields are rejected; the
// secret_ref and DSN of an existing profile are preserved when the payload
// leaves them empty so an edit never silently drops bindings. A caller may
// only bind the managed keyring reference of the profile itself — arbitrary
// external secret_ref values are rejected (see DatabaseProfileDelete for why
// that matters). Policy fields the GUI form does not expose (allowed_schemas,
// allowed_operations, denied_columns, default_schema) are preserved on update
// when the payload does not carry them. The form owns allow_ddl,
// allow_external_host, max_affected_rows, tls and data_classification and
// always sends those keys so emptying a field clears it.
func (a *App) DatabaseProfileSave(profile map[string]interface{}) error {
	if profile == nil {
		return errors.New("profile payload is required")
	}
	if err := rejectInlineDatabaseSecrets(profile); err != nil {
		return err
	}
	next, err := decodeDatabaseProfile(profile)
	if err != nil {
		return err
	}
	next.ID = strings.TrimSpace(next.ID)
	if !validDatabaseProfileID(next.ID) {
		return errors.New("invalid profile id: use 1-64 chars of letters, digits, dot, underscore or dash")
	}
	// secret_ref must be empty (unbound, or "keep existing" on update) or the
	// exact managed keyring reference of this very profile. Anything else
	// would bind another application's credentials and turn profile delete
	// into an arbitrary keyring-delete primitive.
	if ref := strings.TrimSpace(next.SecretRef); ref != "" && ref != databaseProfileManagedSecretRef(next.ID) {
		return errors.New("secret_ref must be empty or this profile's managed keyring reference; bind credentials via the secret field")
	}
	if err := database.ValidateProfile(next); err != nil {
		return err
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		existing, idx := findDatabaseProfile(cfg.DatabaseProfiles, next.ID)
		if idx >= 0 {
			// The panel is not a credential editor: empty secret_ref / DSN on an
			// update means "keep the existing binding", and the schema version is
			// host-managed (secret rotation bumps it).
			if strings.TrimSpace(next.SecretRef) == "" {
				next.SecretRef = existing.SecretRef
			}
			if strings.TrimSpace(next.DSN) == "" {
				next.DSN = existing.DSN
			}
			next.SchemaVersion = existing.SchemaVersion
			// Absent (or JSON-null) keys mean "keep the existing policy",
			// never "clear it" — a rename payload that omitted these would
			// otherwise drop the safety boundary. A caller that explicitly
			// carries a key (even a zero value) replaces the field. The GUI
			// form always sends the fields it owns (allow_ddl, tls, …);
			// allowed_tables / masked_columns stay payload-authoritative.
			if !databaseProfilePayloadCarries(profile, "allowed_schemas") {
				next.AllowedSchemas = existing.AllowedSchemas
			}
			if !databaseProfilePayloadCarries(profile, "allowed_operations") {
				next.AllowedOperations = existing.AllowedOperations
			}
			if !databaseProfilePayloadCarries(profile, "denied_columns") {
				next.DeniedColumns = existing.DeniedColumns
			}
			if !databaseProfilePayloadCarries(profile, "max_affected_rows") {
				next.MaxAffectedRows = existing.MaxAffectedRows
			}
			if !databaseProfilePayloadCarries(profile, "allow_ddl") {
				next.AllowDDL = existing.AllowDDL
			}
			if !databaseProfilePayloadCarries(profile, "allow_external_host") {
				next.AllowExternalHost = existing.AllowExternalHost
			}
			if !databaseProfilePayloadCarries(profile, "read_only") {
				next.ReadOnly = existing.ReadOnly
			}
			if !databaseProfilePayloadCarries(profile, "write_enabled") {
				next.WriteEnabled = existing.WriteEnabled
			}
			if !databaseProfilePayloadCarries(profile, "default_schema") {
				next.DefaultSchema = existing.DefaultSchema
			}
			if !databaseProfilePayloadCarries(profile, "tls") {
				next.TLS = existing.TLS
			} else if strings.TrimSpace(next.TLS.CAFile) == "" {
				// The GUI only edits tls.mode. An empty ca_file in the payload
				// must not wipe a host-managed CA; switching to disable/default
				// drops it so the saved profile stays valid.
				mode := strings.ToLower(strings.TrimSpace(next.TLS.Mode))
				if mode == "require" || mode == "verify-full" {
					next.TLS.CAFile = existing.TLS.CAFile
				}
			}
			if !databaseProfilePayloadCarries(profile, "disabled") {
				next.Disabled = existing.Disabled
			}
			if !databaseProfilePayloadCarries(profile, "data_classification") {
				next.DataClassification = existing.DataClassification
			}
			cfg.DatabaseProfiles[idx] = next
			return
		}
		if next.SchemaVersion <= 0 {
			next.SchemaVersion = 1
		}
		cfg.DatabaseProfiles = append(cfg.DatabaseProfiles, next)
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	a.refreshDatabaseProfilesRuntime()
	return nil
}

// DatabaseProfileDelete removes a profile and refreshes live connections. The
// keyring entry is deleted best-effort only when the profile references the
// managed "keyring://maclaw/database/" namespace — refs pointing at foreign
// services or items are never touched, so a profile cannot be used to delete
// arbitrary keyring entries of other applications. A keyring failure is
// logged but does not fail the delete.
func (a *App) DatabaseProfileDelete(id string) error {
	id = strings.TrimSpace(id)
	if !validDatabaseProfileID(id) {
		return errors.New("invalid profile id")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	profile, idx := findDatabaseProfile(cfg.DatabaseProfiles, id)
	if idx < 0 {
		return errors.New("profile_not_found")
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		next := make([]database.Profile, 0, len(cfg.DatabaseProfiles))
		for _, existing := range cfg.DatabaseProfiles {
			if existing.ID != id {
				next = append(next, existing)
			}
		}
		cfg.DatabaseProfiles = next
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	a.refreshDatabaseProfilesRuntime()
	if ref := strings.TrimSpace(profile.SecretRef); ref != "" {
		if u, parseErr := url.Parse(ref); parseErr == nil && u.Scheme == "keyring" && u.Host == databaseKeyringService && strings.HasPrefix(strings.Trim(u.Path, "/"), "database/") {
			if delErr := databaseKeyringDelete(u.Host, strings.Trim(u.Path, "/")); delErr != nil {
				log.Printf("[database] WARNING: keyring delete failed for profile %s: %v", id, delErr)
			}
		}
	}
	return nil
}

// DatabaseProfileSetSecret writes a profile credential to the OS keyring and
// rebinds the profile's secret_ref. The secret only passes through memory: it
// is never persisted to AppConfig, returned, or logged. The schema version
// bump invalidates approvals/cursors minted against the old credential, and
// the runtime refresh closes live connections of the changed profile.
func (a *App) saveProposedDatabaseProfile(fields map[string]interface{}, secret string) error {
	if a == nil {
		return errors.New("app is not initialized")
	}
	payload := map[string]interface{}{}
	for _, key := range []string{"id", "name", "type", "host", "database", "username"} {
		if v := strings.TrimSpace(fmt.Sprint(fields[key])); v != "" && v != "<nil>" {
			payload[key] = v
		}
	}
	if id := strings.TrimSpace(fmt.Sprint(fields["profile_id"])); id != "" && id != "<nil>" {
		payload["id"] = id
	}
	if n, ok := numberFromAny(fields["port"]); ok {
		port := int(n)
		if port > 0 && port <= 65535 {
			payload["port"] = port
		}
	}
	if _, ok := fields["allow_external_host"]; ok {
		payload["allow_external_host"] = boolFromAny(fields["allow_external_host"])
	}
	id := strings.TrimSpace(fmt.Sprint(payload["id"]))
	if id == "" {
		return errors.New("profile id is required")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if _, idx := findDatabaseProfile(cfg.DatabaseProfiles, id); idx < 0 {
		// New sources from chat default to read-only. A password retry must not
		// send read_only/write_enabled, or a write-enabled profile would flip.
		payload["read_only"] = true
	}
	if err := a.DatabaseProfileSave(payload); err != nil {
		return err
	}
	return a.DatabaseProfileSetSecret(id, secret)
}

func (a *App) DatabaseProfileSetSecret(id, secret string) error {
	id = strings.TrimSpace(id)
	if !validDatabaseProfileID(id) {
		return errors.New("invalid profile id")
	}
	if secret == "" {
		return errors.New("secret must not be empty")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if _, idx := findDatabaseProfile(cfg.DatabaseProfiles, id); idx < 0 {
		return errors.New("profile_not_found")
	}
	if err := databaseKeyringSet(databaseKeyringService, databaseProfileKeyringItem(id), secret); err != nil {
		return fmt.Errorf("failed to store secret in the OS keyring: %v", err)
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		if _, idx := findDatabaseProfile(cfg.DatabaseProfiles, id); idx >= 0 {
			cfg.DatabaseProfiles[idx].SecretRef = databaseProfileManagedSecretRef(id)
			cfg.DatabaseProfiles[idx].SchemaVersion++
		}
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	a.refreshDatabaseProfilesRuntime()
	return nil
}

// DatabaseProfileCopy duplicates a profile without copying secret_ref or DSN.
func (a *App) DatabaseProfileCopy(id, newID string) error {
	id = strings.TrimSpace(id)
	newID = strings.TrimSpace(newID)
	if !validDatabaseProfileID(id) || !validDatabaseProfileID(newID) {
		return errors.New("invalid profile id")
	}
	if id == newID {
		return errors.New("copy requires a new id")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	src, idx := findDatabaseProfile(cfg.DatabaseProfiles, id)
	if idx < 0 {
		return errors.New("profile_not_found")
	}
	if _, exists := findDatabaseProfile(cfg.DatabaseProfiles, newID); exists >= 0 {
		return errors.New("profile id already exists")
	}
	copyProfile := src
	copyProfile.ID = newID
	copyProfile.Name = strings.TrimSpace(src.Name) + " copy"
	copyProfile.SecretRef = ""
	copyProfile.DSN = ""
	copyProfile.Password = ""
	copyProfile.SchemaVersion = 1
	if err := database.ValidateProfile(copyProfile); err != nil {
		return err
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.DatabaseProfiles = append(cfg.DatabaseProfiles, copyProfile)
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	a.refreshDatabaseProfilesRuntime()
	return nil
}

// DatabaseProfileSetDisabled is the per-profile kill switch.
func (a *App) DatabaseProfileSetDisabled(id string, disabled bool) error {
	id = strings.TrimSpace(id)
	if !validDatabaseProfileID(id) {
		return errors.New("invalid profile id")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	if _, idx := findDatabaseProfile(cfg.DatabaseProfiles, id); idx < 0 {
		return errors.New("profile_not_found")
	}
	if err := a.PatchConfig(func(cfg *corelib.AppConfig) {
		if _, idx := findDatabaseProfile(cfg.DatabaseProfiles, id); idx >= 0 {
			cfg.DatabaseProfiles[idx].Disabled = disabled
			cfg.DatabaseProfiles[idx].SchemaVersion++
		}
	}); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}
	a.refreshDatabaseProfilesRuntime()
	return nil
}

// DatabaseProfileTest probes a saved profile with a throwaway manager: ping
// plus capability detection. It never executes user SQL and never touches the
// cached runtime connections. Error messages are classified and scrubbed of
// local paths, DSNs and workspace details.
func (a *App) DatabaseProfileTest(id string) (map[string]interface{}, error) {
	id = strings.TrimSpace(id)
	if !validDatabaseProfileID(id) {
		return nil, errors.New("invalid profile id")
	}
	cfg, err := a.LoadConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}
	profile, idx := findDatabaseProfile(cfg.DatabaseProfiles, id)
	if idx < 0 {
		return nil, errors.New("profile_not_found")
	}
	result := map[string]interface{}{}
	if profile.Type == database.SourceAccess {
		if hint := database.DetectAccessODBC().Hint(); hint != "" {
			result["odbc_hint"] = hint
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	manager := database.NewManager([]database.Profile{profile}, databaseKeyringSecret)
	defer manager.Close()
	if handler := a.ensureLocalIMHandler(); handler != nil {
		manager.SetTunnelDialer(remote.SSHTunnelDialer(func() *remote.SSHSessionManager {
			return handler.ensureSSHManager()
		}))
	}
	_, capabilities, err := manager.Connect(ctx, profile.ID)
	if err != nil {
		class, message := splitDatabaseErrorClass(err)
		result["status"] = "failed"
		result["error_class"] = class
		result["error"] = sanitizeDatabaseTestError(a, profile, message)
		return result, nil
	}
	result["status"] = "ok"
	result["capabilities"] = map[string]interface{}{
		"read":          capabilities.Read,
		"write":         capabilities.Write,
		"transactions":  capabilities.Transactions,
		"cursors":       capabilities.Cursors,
		"explain":       capabilities.Explain,
		"max_page_size": capabilities.MaxPageSize,
	}
	return result, nil
}

// SelectDatabaseProfileFile opens a native file picker for Access/Excel
// profiles. kind selects the filter ("access" or "excel"); currentPath, when
// set, is used as the dialog starting directory. Cancel, unsupported kind,
// or error returns "".
func (a *App) SelectDatabaseProfileFile(kind, currentPath string) string {
	if a == nil {
		return ""
	}
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind != "access" && kind != "excel" {
		return ""
	}
	selection, err := runtime.OpenFileDialog(a.ctx, databaseProfileFileDialogOptions(kind, currentPath))
	if err != nil {
		return ""
	}
	return selection
}

func databaseProfileFileDialogOptions(kind, currentPath string) runtime.OpenDialogOptions {
	kind = strings.ToLower(strings.TrimSpace(kind))
	opts := runtime.OpenDialogOptions{
		Title: "Select Database File",
		Filters: []runtime.FileFilter{
			{DisplayName: "Database Files (*.accdb;*.mdb;*.xlsx;*.xlsm;*.xls)", Pattern: "*.accdb;*.mdb;*.xlsx;*.xlsm;*.xls"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		},
	}
	switch kind {
	case "access":
		opts.Title = "Select Access Database"
		opts.Filters = []runtime.FileFilter{
			{DisplayName: "Access Database (*.accdb;*.mdb)", Pattern: "*.accdb;*.mdb"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		}
	case "excel":
		opts.Title = "Select Excel Workbook"
		opts.Filters = []runtime.FileFilter{
			{DisplayName: "Excel Workbook (*.xlsx;*.xlsm;*.xls)", Pattern: "*.xlsx;*.xlsm;*.xls"},
			{DisplayName: "All Files (*.*)", Pattern: "*.*"},
		}
	}
	applyDatabaseProfileFileDialogStart(currentPath, &opts)
	return opts
}

func applyDatabaseProfileFileDialogStart(currentPath string, opts *runtime.OpenDialogOptions) {
	if opts == nil {
		return
	}
	currentPath = strings.Trim(strings.TrimSpace(currentPath), `"'`)
	if currentPath == "" {
		return
	}
	currentPath = filepath.Clean(currentPath)
	if abs, err := filepath.Abs(currentPath); err == nil {
		currentPath = abs
	}

	info, err := os.Stat(currentPath)
	if err == nil && info.IsDir() {
		opts.DefaultDirectory = currentPath
		return
	}

	base := filepath.Base(currentPath)
	if base != "." && base != ".." && base != string(filepath.Separator) {
		opts.DefaultFilename = base
	}
	if err == nil {
		opts.DefaultDirectory = filepath.Dir(currentPath)
		return
	}
	opts.DefaultDirectory = firstExistingDir(filepath.Dir(currentPath))
}

func firstExistingDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return ""
	}
	for {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() {
			return path
		}
		parent := filepath.Dir(path)
		if parent == path {
			return ""
		}
		path = parent
	}
}

// splitDatabaseErrorClass extracts the stable "class: message" prefix emitted
// by corelib/database error classification so the panel can show the class
// without driver details beyond the scrubbed message.
func splitDatabaseErrorClass(err error) (string, string) {
	msg := err.Error()
	if idx := strings.Index(msg, ":"); idx > 0 {
		class := msg[:idx]
		if !strings.ContainsAny(class, " \t") && len(class) <= 32 {
			return class, strings.TrimSpace(msg[idx+1:])
		}
	}
	return "connection", msg
}

// sanitizeDatabaseTestError removes host-local details (profile file path,
// DSN, workspace root) from a driver error before it reaches the UI.
func sanitizeDatabaseTestError(a *App, p database.Profile, msg string) string {
	var pairs []string
	if path := strings.TrimSpace(p.FilePath); path != "" {
		pairs = append(pairs, path, "[file]")
	}
	if dsn := strings.TrimSpace(p.DSN); dsn != "" {
		pairs = append(pairs, dsn, "[dsn]")
	}
	if a != nil {
		if root := strings.TrimSpace(a.GetCurrentProjectPath()); root != "" {
			pairs = append(pairs, root, "[workspace]")
		}
	}
	if len(pairs) == 0 {
		return msg
	}
	return strings.NewReplacer(pairs...).Replace(msg)
}
