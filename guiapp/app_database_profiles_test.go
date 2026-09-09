package guiapp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/database"
)

func newDatabaseProfileTestApp(t *testing.T) *App {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	return &App{testHomeDir: tmpHome}
}

// installFakeDatabaseKeyring swaps the package-level keyring hooks for an
// in-memory store and returns it. Tests must not run in parallel: the hooks
// are process-wide.
func installFakeDatabaseKeyring(t *testing.T) map[string]string {
	t.Helper()
	store := map[string]string{}
	oldSet, oldDelete, oldGet := databaseKeyringSet, databaseKeyringDelete, databaseKeyringGet
	databaseKeyringSet = func(service, item, secret string) error {
		store[service+"/"+item] = secret
		return nil
	}
	databaseKeyringDelete = func(service, item string) error {
		delete(store, service+"/"+item)
		return nil
	}
	databaseKeyringGet = func(service, item string) (string, error) {
		secret, ok := store[service+"/"+item]
		if !ok {
			return "", errors.New("secret not found")
		}
		return secret, nil
	}
	t.Cleanup(func() {
		databaseKeyringSet, databaseKeyringDelete, databaseKeyringGet = oldSet, oldDelete, oldGet
	})
	return store
}

func mysqlProfilePayload(id string) map[string]interface{} {
	return map[string]interface{}{
		"id":            id,
		"name":          "CRM",
		"type":          "mysql",
		"host":          "127.0.0.1",
		"port":          3306,
		"database":      "crm",
		"username":      "reader",
		"read_only":     true,
		"write_enabled": false,
	}
}

func TestDatabaseProfileSaveAndList(t *testing.T) {
	app := newDatabaseProfileTestApp(t)

	if err := app.DatabaseProfileSave(mysqlProfilePayload("crm")); err != nil {
		t.Fatalf("DatabaseProfileSave() error = %v", err)
	}

	items, err := app.DatabaseProfilesList()
	if err != nil {
		t.Fatalf("DatabaseProfilesList() error = %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("DatabaseProfilesList() len = %d, want 1", len(items))
	}
	item := items[0]
	if item["id"] != "crm" || item["type"] != "mysql" || item["host"] != "127.0.0.1" {
		t.Fatalf("unexpected summary: %#v", item)
	}
	if item["status"] != "configured" {
		t.Fatalf("status = %v, want configured", item["status"])
	}
	if item["has_secret_ref"] != false || item["read_only"] != true {
		t.Fatalf("unexpected flags: %#v", item)
	}
	if _, ok := item["secret_ref"]; ok {
		t.Fatal("summary must not expose secret_ref")
	}
	if _, ok := item["dsn"]; ok {
		t.Fatal("summary must not expose dsn")
	}
	if _, ok := item["password"]; ok {
		t.Fatal("summary must not expose password")
	}
	if item["schema_version"] != 1 {
		t.Fatalf("schema_version = %v, want 1 for a new profile", item["schema_version"])
	}

	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.DatabaseProfiles) != 1 || cfg.DatabaseProfiles[0].ID != "crm" {
		t.Fatalf("profile not persisted: %#v", cfg.DatabaseProfiles)
	}
}

func TestDatabaseProfileSaveRejectsInlineSecrets(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	for _, key := range []string{"password", "db_password", "secret", "api_token"} {
		payload := mysqlProfilePayload("crm")
		payload[key] = "top-secret"
		if err := app.DatabaseProfileSave(payload); err == nil {
			t.Fatalf("DatabaseProfileSave with inline %q must fail", key)
		}
	}
	items, err := app.DatabaseProfilesList()
	if err != nil {
		t.Fatalf("DatabaseProfilesList() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("rejected profiles must not persist, got %d", len(items))
	}
}

func TestDatabaseProfileSaveValidation(t *testing.T) {
	app := newDatabaseProfileTestApp(t)

	missingHost := mysqlProfilePayload("crm")
	delete(missingHost, "host")
	if err := app.DatabaseProfileSave(missingHost); err == nil {
		t.Fatal("save without host must fail validation")
	}

	badID := mysqlProfilePayload("bad id/slash")
	if err := app.DatabaseProfileSave(badID); err == nil {
		t.Fatal("save with unsafe id must fail")
	}

	unknownField := mysqlProfilePayload("crm")
	unknownField["bogus_field"] = "x"
	if err := app.DatabaseProfileSave(unknownField); err == nil {
		t.Fatal("save with unknown field must fail")
	}

	if err := app.DatabaseProfileSave(nil); err == nil {
		t.Fatal("save with nil payload must fail")
	}
}

func TestDatabaseProfileSaveUpdatePreservesBindings(t *testing.T) {
	app := newDatabaseProfileTestApp(t)

	payload := mysqlProfilePayload("crm")
	payload["secret_ref"] = "keyring://maclaw/database/crm"
	payload["dsn"] = "legacy-dsn"
	if err := app.DatabaseProfileSave(payload); err != nil {
		t.Fatalf("DatabaseProfileSave() error = %v", err)
	}
	if err := app.DatabaseProfileSetSecret("crm", "pw-1"); err != nil {
		t.Fatalf("DatabaseProfileSetSecret() error = %v", err)
	}

	// An edit that omits secret_ref / dsn must keep the existing bindings and
	// the host-managed schema version.
	update := mysqlProfilePayload("crm")
	update["name"] = "CRM v2"
	if err := app.DatabaseProfileSave(update); err != nil {
		t.Fatalf("DatabaseProfileSave(update) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.DatabaseProfiles) != 1 {
		t.Fatalf("update must replace by id, got %d profiles", len(cfg.DatabaseProfiles))
	}
	p := cfg.DatabaseProfiles[0]
	if p.Name != "CRM v2" {
		t.Fatalf("name = %q, want CRM v2", p.Name)
	}
	if p.SecretRef != "keyring://maclaw/database/crm" {
		t.Fatalf("secret_ref dropped on update: %q", p.SecretRef)
	}
	if p.DSN != "legacy-dsn" {
		t.Fatalf("dsn dropped on update: %q", p.DSN)
	}
	if p.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want 2 (1 create + 1 secret rotation)", p.SchemaVersion)
	}
}

func TestDatabaseProfileSetSecret(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	store := installFakeDatabaseKeyring(t)

	if err := app.DatabaseProfileSetSecret("crm", "x"); err == nil {
		t.Fatal("SetSecret for unknown profile must fail")
	}
	if err := app.DatabaseProfileSave(mysqlProfilePayload("crm")); err != nil {
		t.Fatalf("DatabaseProfileSave() error = %v", err)
	}
	if err := app.DatabaseProfileSetSecret("crm", ""); err == nil {
		t.Fatal("empty secret must fail")
	}
	if err := app.DatabaseProfileSetSecret("crm", "s3cret-value"); err != nil {
		t.Fatalf("DatabaseProfileSetSecret() error = %v", err)
	}

	if got := store["maclaw/database/crm"]; got != "s3cret-value" {
		t.Fatalf("keyring value = %q, want the submitted secret", got)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	p := cfg.DatabaseProfiles[0]
	if p.SecretRef != "keyring://maclaw/database/crm" {
		t.Fatalf("secret_ref = %q", p.SecretRef)
	}
	if p.SchemaVersion != 2 {
		t.Fatalf("schema_version = %d, want bump after secret write", p.SchemaVersion)
	}

	// The secret value must never reach the persisted config file.
	configPath := filepath.Join(app.testHomeDir, ".maclaw", "config.json")
	raw, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile(config.json) error = %v", err)
	}
	if strings.Contains(string(raw), "s3cret-value") {
		t.Fatal("secret value leaked into config.json")
	}
}

func TestDatabaseProfileSetSecretKeyringFailure(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	oldSet := databaseKeyringSet
	databaseKeyringSet = func(service, item, secret string) error {
		return errors.New("keyring unavailable")
	}
	t.Cleanup(func() { databaseKeyringSet = oldSet })

	if err := app.DatabaseProfileSave(mysqlProfilePayload("crm")); err != nil {
		t.Fatalf("DatabaseProfileSave() error = %v", err)
	}
	err := app.DatabaseProfileSetSecret("crm", "pw")
	if err == nil {
		t.Fatal("SetSecret must fail when the keyring write fails")
	}
	if strings.Contains(err.Error(), "pw") {
		t.Fatal("keyring failure must not echo the secret")
	}
	// The profile must remain unbound after a failed write.
	cfg, cfgErr := app.LoadConfig()
	if cfgErr != nil {
		t.Fatalf("LoadConfig() error = %v", cfgErr)
	}
	if cfg.DatabaseProfiles[0].SecretRef != "" {
		t.Fatal("failed keyring write must not rebind secret_ref")
	}
}

func TestDatabaseProfileDelete(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	store := installFakeDatabaseKeyring(t)

	if err := app.DatabaseProfileDelete("missing"); err == nil {
		t.Fatal("delete of unknown profile must fail")
	}
	if err := app.DatabaseProfileSave(mysqlProfilePayload("crm")); err != nil {
		t.Fatalf("DatabaseProfileSave() error = %v", err)
	}
	if err := app.DatabaseProfileSetSecret("crm", "pw"); err != nil {
		t.Fatalf("DatabaseProfileSetSecret() error = %v", err)
	}
	if err := app.DatabaseProfileDelete("crm"); err != nil {
		t.Fatalf("DatabaseProfileDelete() error = %v", err)
	}
	items, err := app.DatabaseProfilesList()
	if err != nil {
		t.Fatalf("DatabaseProfilesList() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("profile still listed after delete: %#v", items)
	}
	if _, ok := store["maclaw/database/crm"]; ok {
		t.Fatal("keyring entry must be removed on profile delete")
	}
}

func TestDatabaseProfileTest(t *testing.T) {
	app := newDatabaseProfileTestApp(t)

	if _, err := app.DatabaseProfileTest("missing"); err == nil {
		t.Fatal("test of unknown profile must fail")
	}

	// Excel profile pointing at a real (empty) file: ping/capability probe
	// succeeds without executing user SQL.
	tmpFile := filepath.Join(t.TempDir(), "data.xlsx")
	if err := os.WriteFile(tmpFile, []byte("placeholder"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	excelPayload := map[string]interface{}{
		"id":        "reports",
		"name":      "Reports",
		"type":      "excel",
		"file_path": tmpFile,
		"read_only": true,
	}
	if err := app.DatabaseProfileSave(excelPayload); err != nil {
		t.Fatalf("DatabaseProfileSave(excel) error = %v", err)
	}
	result, err := app.DatabaseProfileTest("reports")
	if err != nil {
		t.Fatalf("DatabaseProfileTest() error = %v", err)
	}
	if result["status"] != "ok" {
		t.Fatalf("status = %v, want ok (%v)", result["status"], result["error"])
	}
	caps, ok := result["capabilities"].(map[string]interface{})
	if !ok || caps["read"] != true {
		t.Fatalf("capabilities missing or unreadable: %#v", result["capabilities"])
	}

	// Missing file: classified, path-scrubbed failure.
	badPayload := map[string]interface{}{
		"id":        "gone",
		"type":      "excel",
		"file_path": filepath.Join(t.TempDir(), "no-such-file.xlsx"),
		"read_only": true,
	}
	if err := app.DatabaseProfileSave(badPayload); err != nil {
		t.Fatalf("DatabaseProfileSave(gone) error = %v", err)
	}
	failed, err := app.DatabaseProfileTest("gone")
	if err != nil {
		t.Fatalf("DatabaseProfileTest() error = %v", err)
	}
	if failed["status"] != "failed" {
		t.Fatalf("status = %v, want failed", failed["status"])
	}
	if failed["error_class"] != "connection" {
		t.Fatalf("error_class = %v, want connection", failed["error_class"])
	}
	msg, _ := failed["error"].(string)
	if strings.Contains(msg, badPayload["file_path"].(string)) {
		t.Fatalf("error must scrub the file path, got %q", msg)
	}
}

func TestDatabaseProfileSaveRejectsUnknownType(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	payload := mysqlProfilePayload("crm")
	payload["type"] = "oracle"
	if err := app.DatabaseProfileSave(payload); err == nil {
		t.Fatal("save with unsupported source type must fail")
	}
}

func TestDatabaseProfileSaveRejectsForeignSecretRef(t *testing.T) {
	app := newDatabaseProfileTestApp(t)

	// Refs outside the managed keyring://maclaw/database/<id> namespace would
	// bind another application's credentials and give profile delete an
	// arbitrary keyring-delete primitive.
	for _, ref := range []string{
		"keyring://other-app/cred",
		"keyring://maclaw/database/other-profile",
		"keyring://maclaw/other/crm",
		"file:///tmp/secret",
	} {
		payload := mysqlProfilePayload("crm")
		payload["secret_ref"] = ref
		if err := app.DatabaseProfileSave(payload); err == nil {
			t.Fatalf("DatabaseProfileSave with secret_ref %q must fail", ref)
		}
	}
	items, err := app.DatabaseProfilesList()
	if err != nil {
		t.Fatalf("DatabaseProfilesList() error = %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("rejected profiles must not persist, got %d", len(items))
	}

	// The managed reference of the profile itself stays accepted.
	payload := mysqlProfilePayload("crm")
	payload["secret_ref"] = "keyring://maclaw/database/crm"
	if err := app.DatabaseProfileSave(payload); err != nil {
		t.Fatalf("DatabaseProfileSave with managed secret_ref error = %v", err)
	}
}

func TestDatabaseProfileSaveUpdatePreservesPolicyFields(t *testing.T) {
	app := newDatabaseProfileTestApp(t)

	// Seed a profile with the deny-by-default policy fields set, bypassing the
	// panel (as the admin API or a hand-edited config would).
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.DatabaseProfiles = []database.Profile{{
			ID:                "crm",
			Name:              "CRM",
			Type:              database.SourceMySQL,
			Host:              "127.0.0.1",
			Port:              3306,
			Database:          "crm",
			Username:          "reader",
			ReadOnly:          true,
			SchemaVersion:     3,
			SecretRef:         "keyring://maclaw/database/crm",
			AllowedSchemas:    []string{"dbo"},
			AllowedOperations: []string{"query"},
			DeniedColumns:     []string{"ssn"},
			MaxAffectedRows:   100,
			AllowDDL:          true,
			AllowExternalHost: true,
			DefaultSchema:     "dbo",
		}}
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	// A panel-style save (no policy keys in the payload) must keep them all.
	update := mysqlProfilePayload("crm")
	update["name"] = "CRM v2"
	if err := app.DatabaseProfileSave(update); err != nil {
		t.Fatalf("DatabaseProfileSave(update) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.DatabaseProfiles) != 1 {
		t.Fatalf("profiles = %#v", cfg.DatabaseProfiles)
	}
	p := cfg.DatabaseProfiles[0]
	if p.Name != "CRM v2" {
		t.Fatalf("name = %q, want CRM v2", p.Name)
	}
	if len(p.AllowedSchemas) != 1 || p.AllowedSchemas[0] != "dbo" {
		t.Fatalf("allowed_schemas dropped on update: %#v", p.AllowedSchemas)
	}
	if len(p.AllowedOperations) != 1 || p.AllowedOperations[0] != "query" {
		t.Fatalf("allowed_operations dropped on update: %#v", p.AllowedOperations)
	}
	if len(p.DeniedColumns) != 1 || p.DeniedColumns[0] != "ssn" {
		t.Fatalf("denied_columns dropped on update: %#v", p.DeniedColumns)
	}
	if p.MaxAffectedRows != 100 || !p.AllowDDL || !p.AllowExternalHost || p.DefaultSchema != "dbo" {
		t.Fatalf("scalar policy fields dropped on update: %#v", p)
	}

	// A caller that explicitly carries a key replaces the field, even with a
	// zero value — "absent" and "explicitly cleared" stay distinguishable.
	clearing := mysqlProfilePayload("crm")
	clearing["allowed_schemas"] = []interface{}{}
	clearing["allowed_operations"] = []interface{}{}
	clearing["denied_columns"] = []interface{}{}
	clearing["max_affected_rows"] = 0
	clearing["allow_ddl"] = false
	clearing["allow_external_host"] = false
	clearing["default_schema"] = ""
	if err := app.DatabaseProfileSave(clearing); err != nil {
		t.Fatalf("DatabaseProfileSave(clearing) error = %v", err)
	}
	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	p = cfg.DatabaseProfiles[0]
	if len(p.AllowedSchemas) != 0 || len(p.AllowedOperations) != 0 || len(p.DeniedColumns) != 0 {
		t.Fatalf("explicitly cleared slice fields must stay cleared: %#v", p)
	}
	if p.MaxAffectedRows != 0 || p.AllowDDL || p.AllowExternalHost || p.DefaultSchema != "" {
		t.Fatalf("explicitly cleared scalar fields must stay cleared: %#v", p)
	}
}

func TestDatabaseProfileSavePreservesTLSCAFile(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.DatabaseProfiles = []database.Profile{{
			ID:       "crm",
			Name:     "CRM",
			Type:     database.SourceMySQL,
			Host:     "127.0.0.1",
			Port:     3306,
			Database: "crm",
			Username: "reader",
			ReadOnly: true,
			TLS:      database.TLSSettings{Mode: "require", CAFile: "certs/db.pem"},
		}}
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}

	keep := mysqlProfilePayload("crm")
	keep["tls"] = map[string]interface{}{"mode": "require"}
	if err := app.DatabaseProfileSave(keep); err != nil {
		t.Fatalf("DatabaseProfileSave(keep tls) error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.DatabaseProfiles[0].TLS.CAFile != "certs/db.pem" {
		t.Fatalf("tls.ca_file dropped on mode-only update: %#v", cfg.DatabaseProfiles[0].TLS)
	}

	disable := mysqlProfilePayload("crm")
	disable["tls"] = map[string]interface{}{"mode": "disable"}
	if err := app.DatabaseProfileSave(disable); err != nil {
		t.Fatalf("DatabaseProfileSave(disable tls) error = %v", err)
	}
	cfg, err = app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	p := cfg.DatabaseProfiles[0]
	if p.TLS.Mode != "disable" {
		t.Fatalf("tls.mode = %q, want disable", p.TLS.Mode)
	}
	if p.TLS.CAFile != "" {
		t.Fatalf("tls.ca_file must clear when mode is disable: %#v", p.TLS)
	}
}

func TestDatabaseProfileDeleteKeepsForeignKeyringEntries(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	store := installFakeDatabaseKeyring(t)
	store["other-app/cred"] = "sibling-secret"
	store["maclaw/other/crm"] = "non-database-item"

	// Profiles whose secret_ref points outside the managed
	// keyring://maclaw/database/ namespace (e.g. written by hand or by an
	// older version) must not have their keyring entries deleted.
	if err := app.PatchConfig(func(cfg *corelib.AppConfig) {
		cfg.DatabaseProfiles = []database.Profile{
			{ID: "foreign", Name: "Foreign", Type: database.SourceMySQL, Host: "127.0.0.1", Database: "crm", SecretRef: "keyring://other-app/cred"},
			{ID: "sibling", Name: "Sibling", Type: database.SourceMySQL, Host: "127.0.0.1", Database: "crm", SecretRef: "keyring://maclaw/other/crm"},
		}
	}); err != nil {
		t.Fatalf("PatchConfig() error = %v", err)
	}
	for _, id := range []string{"foreign", "sibling"} {
		if err := app.DatabaseProfileDelete(id); err != nil {
			t.Fatalf("DatabaseProfileDelete(%s) error = %v", id, err)
		}
	}
	if got := store["other-app/cred"]; got != "sibling-secret" {
		t.Fatalf("foreign keyring entry must survive profile delete, got %q", got)
	}
	if got := store["maclaw/other/crm"]; got != "non-database-item" {
		t.Fatalf("non-database keyring entry must survive profile delete, got %q", got)
	}
}

func TestDatabaseProfileFileDialogOptions(t *testing.T) {
	access := databaseProfileFileDialogOptions("Access", "")
	if access.Title != "Select Access Database" {
		t.Fatalf("access title = %q", access.Title)
	}
	if len(access.Filters) == 0 || !strings.Contains(access.Filters[0].Pattern, "*.accdb") || !strings.Contains(access.Filters[0].Pattern, "*.mdb") {
		t.Fatalf("access filters = %#v", access.Filters)
	}
	if access.DefaultDirectory != "" || access.DefaultFilename != "" {
		t.Fatalf("empty path must not seed the dialog start, got dir=%q file=%q", access.DefaultDirectory, access.DefaultFilename)
	}

	excel := databaseProfileFileDialogOptions(" excel ", "")
	if excel.Title != "Select Excel Workbook" {
		t.Fatalf("excel title = %q", excel.Title)
	}
	if len(excel.Filters) == 0 || !strings.Contains(excel.Filters[0].Pattern, "*.xlsx") {
		t.Fatalf("excel filters = %#v", excel.Filters)
	}

	other := databaseProfileFileDialogOptions("mysql", "")
	if other.Title != "Select Database File" {
		t.Fatalf("fallback title = %q", other.Title)
	}
}

func TestDatabaseProfileFileDialogStart(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.xlsx")
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	opts := databaseProfileFileDialogOptions("excel", path)
	if opts.DefaultDirectory != dir {
		t.Fatalf("DefaultDirectory = %q, want %q", opts.DefaultDirectory, dir)
	}
	if opts.DefaultFilename != "data.xlsx" {
		t.Fatalf("DefaultFilename = %q", opts.DefaultFilename)
	}

	opts = databaseProfileFileDialogOptions("excel", dir)
	if opts.DefaultDirectory != dir {
		t.Fatalf("directory DefaultDirectory = %q, want %q", opts.DefaultDirectory, dir)
	}
	if opts.DefaultFilename != "" {
		t.Fatalf("directory must not set DefaultFilename, got %q", opts.DefaultFilename)
	}

	missing := filepath.Join(dir, "missing", "file.mdb")
	opts = databaseProfileFileDialogOptions("access", missing)
	if opts.DefaultDirectory != dir {
		t.Fatalf("missing nested file DefaultDirectory = %q, want existing ancestor %q", opts.DefaultDirectory, dir)
	}
	if opts.DefaultFilename != "file.mdb" {
		t.Fatalf("missing file DefaultFilename = %q", opts.DefaultFilename)
	}

	quoted := `"` + path + `"`
	opts = databaseProfileFileDialogOptions("excel", quoted)
	if opts.DefaultDirectory != dir || opts.DefaultFilename != "data.xlsx" {
		t.Fatalf("quoted path dir=%q file=%q", opts.DefaultDirectory, opts.DefaultFilename)
	}
}

func TestSelectDatabaseProfileFileNilApp(t *testing.T) {
	var app *App
	if got := app.SelectDatabaseProfileFile("excel", ""); got != "" {
		t.Fatalf("nil app returned %q", got)
	}
}

func TestSelectDatabaseProfileFileRejectsNonFileKinds(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	if got := app.SelectDatabaseProfileFile("mysql", ""); got != "" {
		t.Fatalf("mysql kind returned %q", got)
	}
}

func TestSaveProposedDatabaseProfileCoercesStringPort(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	store := installFakeDatabaseKeyring(t)
	err := app.saveProposedDatabaseProfile(map[string]interface{}{
		"profile_id": "mysql-192-168-1-242",
		"name":       "lab mysql",
		"type":       "mysql",
		"host":       "192.168.1.242",
		"port":       "3306",
		"database":   "mysql",
		"username":   "root",
	}, "form-secret")
	if err != nil {
		t.Fatalf("saveProposedDatabaseProfile() error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if len(cfg.DatabaseProfiles) != 1 {
		t.Fatalf("profiles = %#v", cfg.DatabaseProfiles)
	}
	p := cfg.DatabaseProfiles[0]
	if p.ID != "mysql-192-168-1-242" || p.Port != 3306 || p.Database != "mysql" {
		t.Fatalf("saved profile = %#v", p)
	}
	if p.ReadOnly != true {
		t.Fatal("proposed profiles must default to read_only")
	}
	if store["maclaw/database/mysql-192-168-1-242"] != "form-secret" {
		t.Fatalf("keyring = %#v", store)
	}
}

func TestSaveProposedDatabaseProfilePreservesAllowExternalHost(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	_ = installFakeDatabaseKeyring(t)
	err := app.saveProposedDatabaseProfile(map[string]interface{}{
		"id":                  "public-db",
		"type":                "mysql",
		"host":                "db.example.com",
		"database":            "app",
		"allow_external_host": true,
	}, "form-secret")
	if err != nil {
		t.Fatalf("saveProposedDatabaseProfile() error = %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !cfg.DatabaseProfiles[0].AllowExternalHost {
		t.Fatalf("allow_external_host was dropped: %#v", cfg.DatabaseProfiles[0])
	}
}

func TestSaveProposedDatabaseProfilePasswordRetryKeepsWritePolicy(t *testing.T) {
	app := newDatabaseProfileTestApp(t)
	_ = installFakeDatabaseKeyring(t)
	if err := app.DatabaseProfileSave(map[string]interface{}{
		"id":            "crm",
		"type":          "mysql",
		"host":          "192.168.1.242",
		"port":          3306,
		"database":      "crm",
		"username":      "root",
		"read_only":     false,
		"write_enabled": true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := app.saveProposedDatabaseProfile(map[string]interface{}{
		"id":       "crm",
		"type":     "mysql",
		"host":     "192.168.1.242",
		"database": "crm",
		"username": "root",
		"port":     3306,
	}, "rotated-secret"); err != nil {
		t.Fatalf("password retry save: %v", err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	p := cfg.DatabaseProfiles[0]
	if p.ReadOnly || !p.WriteEnabled {
		t.Fatalf("password retry flipped policy: %#v", p)
	}
}
