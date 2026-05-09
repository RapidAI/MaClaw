package diworkerauth

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/platform/idgen"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// LDAPConfig holds LDAP server connection parameters.
type AuthMethodStatus struct {
	Method      string `json:"method"`
	Label       string `json:"label"`
	Enabled     bool   `json:"enabled"`
	Implemented bool   `json:"implemented"`
	Status      string `json:"status"`
	Description string `json:"description"`
}

type OIDCConfig struct {
	Enabled        bool     `json:"enabled"`
	IssuerURL      string   `json:"issuer_url"`
	ClientID       string   `json:"client_id"`
	ClientSecret   string   `json:"client_secret,omitempty"`
	RedirectURL    string   `json:"redirect_url"`
	Scopes         []string `json:"scopes"`
	AllowedDomains []string `json:"allowed_domains"`
}
type LDAPConfig struct {
	Enabled bool   `json:"enabled"`
	Host    string `json:"host"`
	Port    int    `json:"port"`
	UseTLS  bool   `json:"use_tls"`
	BaseDN  string `json:"base_dn"`
	BindFmt string `json:"bind_fmt"` // e.g. "{user}@example.com"
}

type Handler struct {
	write, read *sql.DB
}

func NewHandler(write, read *sql.DB) *Handler {
	return &Handler{write: write, read: read}
}

func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/diworker-auth/ldap", h.handleLDAP)
	mux.HandleFunc("/admin/diworker-auth/ldap/test", h.handleLDAPTest)
	mux.HandleFunc("/admin/diworker-auth/oidc", h.handleOIDC)
	mux.HandleFunc("/admin/diworker-auth/methods", h.handleMethods)
	mux.HandleFunc("/admin/diworker-auth/accounts", h.handleAccounts)
	mux.HandleFunc("/admin/diworker-auth/accounts/", h.handleAccountByID)
	mux.HandleFunc("/admin/diworker-auth/import-csv", h.handleImportCSV)
}

// RegisterAuthRoutes registers the public authentication endpoint.
func (h *Handler) RegisterAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/diworker-auth/methods", h.handleMethods)
	mux.HandleFunc("/diworker-auth/authenticate", h.handleAuthenticate)
	mux.HandleFunc("/diworker-auth/enrollment/verify", h.handleEnrollmentVerify)
}

// ── LDAP config (stored in system-like key-value via a dedicated row) ──

const ldapConfigKey = "diworker_ldap_config"
const oidcConfigKey = "diworker_oidc_config"
const maxDiWorkerAuthJSONBodyBytes = 64 << 10
const maxAccountImportRows = 5000

func decodeDiWorkerAuthJSON(body io.Reader, dst any) error {
	data, err := io.ReadAll(io.LimitReader(body, maxDiWorkerAuthJSONBodyBytes+1))
	if err != nil {
		return err
	}
	if len(data) > maxDiWorkerAuthJSONBodyBytes {
		return errors.New("diworker auth json body exceeds size limit")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return errors.New("diworker auth json body contains trailing data")
		}
		return err
	}
	return nil
}

func (h *Handler) loadLDAPConfig() LDAPConfig {
	var raw string
	err := h.read.QueryRow(`SELECT value_json FROM system_settings WHERE key=?`, ldapConfigKey).Scan(&raw)
	if err != nil || raw == "" {
		return LDAPConfig{Port: 389, BindFmt: "{user}@example.com"}
	}
	var cfg LDAPConfig
	_ = json.Unmarshal([]byte(raw), &cfg)
	return cfg
}

func (h *Handler) saveLDAPConfig(cfg LDAPConfig) error {
	data, _ := json.Marshal(cfg)
	now := time.Now().Format(time.RFC3339)
	_, err := h.write.Exec(
		`INSERT INTO system_settings (key, value_json, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value_json=excluded.value_json, updated_at=excluded.updated_at`,
		ldapConfigKey, string(data), now)
	return err
}

func (h *Handler) handleLDAP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		response.OK(w, h.loadLDAPConfig())
	case http.MethodPost:
		var cfg LDAPConfig
		if err := decodeDiWorkerAuthJSON(r.Body, &cfg); err != nil {
			response.BadRequest(w, "INVALID_BODY", "invalid JSON")
			return
		}
		if err := h.saveLDAPConfig(cfg); err != nil {
			response.Internal(w, err.Error())
			return
		}
		response.OK(w, map[string]string{"status": "ok"})
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleLDAPTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeDiWorkerAuthJSON(r.Body, &req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	cfg := h.loadLDAPConfig()
	if !cfg.Enabled || cfg.Host == "" {
		response.OK(w, map[string]any{"success": false, "error": "LDAP is not configured or enabled"})
		return
	}
	if err := ldapBind(&cfg, req.Username, req.Password); err != nil {
		response.OK(w, map[string]any{"success": false, "error": err.Error()})
		return
	}
	response.OK(w, map[string]any{"success": true})
}

func ldapBind(cfg *LDAPConfig, username, password string) error {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	bindDN := strings.ReplaceAll(cfg.BindFmt, "{user}", username)

	var conn net.Conn
	var err error
	dialer := &net.Dialer{Timeout: 10 * time.Second}

	if cfg.UseTLS || cfg.Port == 636 {
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, &tls.Config{ServerName: cfg.Host})
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("LDAP connection failed: %w", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))

	// Build LDAP v3 Simple Bind Request (multi-byte length safe)
	dnBytes := []byte(bindDN)
	pwBytes := []byte(password)
	version := []byte{0x02, 0x01, 0x03}
	name := berOctetString(0x04, dnBytes)
	auth := berOctetString(0x80, pwBytes)
	bindBody := concat(version, name, auth)
	bindReq := berWrap(0x60, bindBody)
	msgID := []byte{0x02, 0x01, 0x01}
	envelope := concat(msgID, bindReq)
	packet := berWrap(0x30, envelope)

	if _, err := conn.Write(packet); err != nil {
		return fmt.Errorf("LDAP bind request failed: %w", err)
	}
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("LDAP bind response failed: %w", err)
	}
	// Parse result code from BindResponse
	if rc := parseLDAPResult(buf[:n]); rc != 0 {
		return fmt.Errorf("LDAP authentication failed (result code %d)", rc)
	}
	return nil
}

func parseLDAPResult(data []byte) int {
	if len(data) < 10 {
		return -1
	}
	idx := 0
	// Skip SEQUENCE tag + length
	if idx >= len(data) || data[idx] != 0x30 {
		return -1
	}
	idx++
	idx += berLengthSize(data[idx:])
	// Skip messageID (INTEGER)
	if idx >= len(data) || data[idx] != 0x02 {
		return -1
	}
	idx++
	if idx >= len(data) {
		return -1
	}
	idLen := int(data[idx])
	idx += 1 + idLen
	// BindResponse (APPLICATION 1 = tag 0x61)
	if idx >= len(data) || data[idx] != 0x61 {
		return -1
	}
	idx++
	idx += berLengthSize(data[idx:])
	// resultCode (ENUMERATED, tag 0x0a)
	if idx >= len(data) || data[idx] != 0x0a {
		return -1
	}
	idx++
	if idx >= len(data) {
		return -1
	}
	rcLen := int(data[idx])
	idx++
	if idx+rcLen > len(data) {
		return -1
	}
	rc := 0
	for i := 0; i < rcLen; i++ {
		rc = (rc << 8) | int(data[idx+i])
	}
	return rc
}

// berLengthSize returns how many bytes the BER length field occupies (to skip past it).
func berLengthSize(data []byte) int {
	if len(data) == 0 {
		return 0
	}
	if data[0] < 0x80 {
		return 1
	}
	n := int(data[0] & 0x7f)
	return 1 + n
}

// ── Local account CRUD ──

func (h *Handler) handleAccounts(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.listAccounts(w, r)
	case http.MethodPost:
		h.createAccount(w, r)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or POST")
	}
}

func (h *Handler) handleAccountByID(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimPrefix(r.URL.Path, "/admin/diworker-auth/accounts/")
	if id == "" {
		response.BadRequest(w, "MISSING_ID", "account id required")
		return
	}
	switch r.Method {
	case http.MethodPut:
		h.updateAccount(w, r, id)
	case http.MethodDelete:
		h.deleteAccount(w, r, id)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT or DELETE")
	}
}

func (h *Handler) listAccounts(w http.ResponseWriter, r *http.Request) {
	tenantID := tenant.RequestTenantID(r)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	var total int
	_ = h.read.QueryRow(`SELECT COUNT(*) FROM diworker_accounts WHERE tenant_id=?`, tenantID).Scan(&total)

	rows, err := h.read.Query(
		`SELECT id, username, identifier, expires_at, disabled, created_at FROM diworker_accounts WHERE tenant_id=? ORDER BY created_at DESC LIMIT ? OFFSET ?`,
		tenantID, limit, offset)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	defer rows.Close()

	type acct struct {
		ID         string  `json:"id"`
		Username   string  `json:"username"`
		Identifier string  `json:"identifier"`
		ExpiresAt  *string `json:"expires_at"`
		Disabled   bool    `json:"disabled"`
		CreatedAt  string  `json:"created_at"`
	}
	var list []acct
	for rows.Next() {
		var a acct
		var expiresAt sql.NullString
		var disabledInt int
		if err := rows.Scan(&a.ID, &a.Username, &a.Identifier, &expiresAt, &disabledInt, &a.CreatedAt); err != nil {
			continue
		}
		a.Disabled = disabledInt != 0
		if expiresAt.Valid {
			a.ExpiresAt = &expiresAt.String
		}
		list = append(list, a)
	}
	if list == nil {
		list = []acct{}
	}
	response.OK(w, map[string]any{"items": list, "total": total})
}

func (h *Handler) createAccount(w http.ResponseWriter, r *http.Request) {
	tenantID := tenant.RequestTenantID(r)
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		Identifier string `json:"identifier"`
		ExpiryDays int    `json:"expiry_days"`
	}
	if err := decodeDiWorkerAuthJSON(r.Body, &req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	if req.Username == "" || req.Password == "" {
		response.BadRequest(w, "MISSING_FIELDS", "用户名和密码必填")
		return
	}
	// Check uniqueness
	var exists int
	_ = h.read.QueryRow(`SELECT COUNT(*) FROM diworker_accounts WHERE tenant_id=? AND username=?`, tenantID, req.Username).Scan(&exists)
	if exists > 0 {
		response.Error(w, http.StatusConflict, "USERNAME_EXISTS", "用户名已存在")
		return
	}

	id := idgen.New("dwa")
	salt := generateSalt()
	hash := hashPwd(req.Password, salt)
	now := time.Now().Format(time.RFC3339)
	var expiresAt *string
	if req.ExpiryDays > 0 {
		exp := time.Now().Add(time.Duration(req.ExpiryDays) * 24 * time.Hour).Format(time.RFC3339)
		expiresAt = &exp
	}

	_, err := h.write.Exec(
		`INSERT INTO diworker_accounts (id, tenant_id, username, password_hash, salt, identifier, expires_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, tenantID, req.Username, hash, salt, req.Identifier, expiresAt, now, now)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]any{"id": id, "username": req.Username})
}

func (h *Handler) updateAccount(w http.ResponseWriter, r *http.Request, id string) {
	tenantID := tenant.RequestTenantID(r)
	var req struct {
		Username   string `json:"username"`
		Password   string `json:"password"`
		Identifier string `json:"identifier"`
		ExpiryDays *int   `json:"expiry_days"` // nil=不修改, 0=永久, >0=天数
		Disabled   bool   `json:"disabled"`
	}
	if err := decodeDiWorkerAuthJSON(r.Body, &req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	now := time.Now().Format(time.RFC3339)

	sets := []string{"updated_at=?"}
	args := []any{now}
	if req.Username != "" {
		sets = append(sets, "username=?")
		args = append(args, req.Username)
	}
	if req.Password != "" {
		salt := generateSalt()
		sets = append(sets, "password_hash=?", "salt=?")
		args = append(args, hashPwd(req.Password, salt), salt)
	}
	if req.Identifier != "" {
		sets = append(sets, "identifier=?")
		args = append(args, req.Identifier)
	}
	sets = append(sets, "disabled=?")
	args = append(args, boolToInt(req.Disabled))

	if req.ExpiryDays != nil {
		if *req.ExpiryDays > 0 {
			exp := time.Now().Add(time.Duration(*req.ExpiryDays) * 24 * time.Hour).Format(time.RFC3339)
			sets = append(sets, "expires_at=?")
			args = append(args, exp)
		} else {
			sets = append(sets, "expires_at=NULL")
		}
	}

	args = append(args, id, tenantID)
	_, err := h.write.Exec(fmt.Sprintf("UPDATE diworker_accounts SET %s WHERE id=? AND tenant_id=?", strings.Join(sets, ",")), args...)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

func (h *Handler) deleteAccount(w http.ResponseWriter, r *http.Request, id string) {
	tenantID := tenant.RequestTenantID(r)
	_, err := h.write.Exec(`DELETE FROM diworker_accounts WHERE id=? AND tenant_id=?`, id, tenantID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "deleted"})
}

// ── CSV import ──

func (h *Handler) handleImportCSV(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	tenantID := tenant.RequestTenantID(r)
	var reader io.Reader = r.Body
	if err := r.ParseMultipartForm(10 << 20); err == nil {
		if file, _, err := r.FormFile("file"); err == nil {
			defer file.Close()
			reader = file
		}
	}

	csvR := csv.NewReader(reader)
	csvR.TrimLeadingSpace = true
	csvR.FieldsPerRecord = -1

	type result struct {
		Created int      `json:"created"`
		Skipped int      `json:"skipped"`
		Errors  []string `json:"errors,omitempty"`
	}
	res := result{}
	line := 0
	indexes := map[string]int{"username": 0, "password": 1, "identifier": 2, "expiry_days": 3}
	for {
		record, err := csvR.Read()
		if err == io.EOF {
			break
		}
		line++
		if line > maxAccountImportRows+1 {
			res.Errors = append(res.Errors, fmt.Sprintf("import is limited to %d account rows", maxAccountImportRows))
			res.Skipped++
			break
		}
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: %v", line, err))
			res.Skipped++
			continue
		}
		if line == 1 && isAccountImportHeader(record) {
			indexes = accountImportHeaderIndexes(record)
			continue
		}
		if len(record) < 2 {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: username and password are required", line))
			res.Skipped++
			continue
		}
		username := csvField(record, indexes["username"])
		password := csvField(record, indexes["password"])
		if username == "" || password == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("line %d: username or password is empty", line))
			res.Skipped++
			continue
		}
		identifier := csvField(record, indexes["identifier"])
		expiryDays := 0
		if value := csvField(record, indexes["expiry_days"]); value != "" {
			if d, err := strconv.Atoi(value); err == nil {
				expiryDays = d
			}
		}

		var exists int
		_ = h.read.QueryRow(`SELECT COUNT(*) FROM diworker_accounts WHERE tenant_id=? AND username=?`, tenantID, username).Scan(&exists)
		if exists > 0 {
			res.Errors = append(res.Errors, fmt.Sprintf("行 %d: 用户名 %q 已存在", line, username))
			res.Skipped++
			continue
		}

		id := idgen.New("dwa")
		salt := generateSalt()
		hash := hashPwd(password, salt)
		now := time.Now().Format(time.RFC3339)
		var expiresAt *string
		if expiryDays > 0 {
			exp := time.Now().Add(time.Duration(expiryDays) * 24 * time.Hour).Format(time.RFC3339)
			expiresAt = &exp
		}
		_, err = h.write.Exec(
			`INSERT INTO diworker_accounts (id, tenant_id, username, password_hash, salt, identifier, expires_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			id, tenantID, username, hash, salt, identifier, expiresAt, now, now)
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("行 %d: %v", line, err))
			res.Skipped++
			continue
		}
		res.Created++
	}
	response.OK(w, res)
}

// ── Authentication endpoint ──

func (h *Handler) handleAuthenticate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req struct {
		Method   string `json:"method"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := decodeDiWorkerAuthJSON(r.Body, &req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	provider, ok := h.authProvider(req.Method)
	if !ok {
		response.BadRequest(w, "UNKNOWN_METHOD", unknownAuthMethodError(req.Method))
		return
	}
	result := provider.Authenticate(AuthCredential{
		TenantID: tenant.RequestTenantID(r),
		Username: strings.TrimSpace(req.Username),
		Password: req.Password,
	})
	if !result.Authenticated {
		response.OK(w, map[string]any{"authenticated": false, "method": provider.Method(), "error": result.Error})
		return
	}
	response.OK(w, map[string]any{"authenticated": true, "method": provider.Method(), "username": strings.TrimSpace(req.Username)})
}

// ── helpers ──

func (h *Handler) handleEnrollmentVerify(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req struct {
		Method   string `json:"method"`
		Username string `json:"username"`
		Password string `json:"password"`
		WorkerID string `json:"worker_id"`
	}
	if err := decodeDiWorkerAuthJSON(r.Body, &req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	credential := AuthCredential{
		TenantID: tenant.RequestTenantID(r),
		Username: strings.TrimSpace(req.Username),
		Password: req.Password,
		WorkerID: strings.TrimSpace(req.WorkerID),
	}
	if credential.Username == "" || credential.Password == "" || credential.WorkerID == "" {
		response.BadRequest(w, "MISSING_FIELDS", "username, password, and worker_id are required")
		return
	}
	provider, ok := h.authProvider(req.Method)
	if !ok {
		response.BadRequest(w, "UNKNOWN_METHOD", unknownAuthMethodError(req.Method))
		return
	}
	result := provider.Authenticate(credential)
	if !result.Authenticated {
		response.OK(w, map[string]any{"verified": false, "authenticated": false, "method": provider.Method(), "error": result.Error})
		return
	}
	if !identifierAllowsWorker(result.Identifier, credential.Username, credential.WorkerID) {
		response.OK(w, map[string]any{"verified": false, "authenticated": true, "method": provider.Method(), "error": "account is not allowed to bind this iWorker"})
		return
	}
	response.OK(w, map[string]any{"verified": true, "authenticated": true, "method": provider.Method(), "username": credential.Username, "worker_id": credential.WorkerID})
}
func (h *Handler) authenticateLocalAccount(tenantID, username, password string) (bool, string, string) {
	var storedHash, salt, identifier string
	var disabledInt int
	var expiresAt sql.NullString
	err := h.read.QueryRow(
		`SELECT password_hash, salt, identifier, disabled, expires_at FROM diworker_accounts WHERE tenant_id=? AND username=?`,
		tenantID, username).Scan(&storedHash, &salt, &identifier, &disabledInt, &expiresAt)
	if err != nil {
		_ = hashPwd("dummy", "dummy_salt")
		return false, "", "username or password is incorrect"
	}
	if disabledInt != 0 {
		return false, "", "account is disabled"
	}
	if expiresAt.Valid {
		if t, err := time.Parse(time.RFC3339, expiresAt.String); err == nil && time.Now().After(t) {
			return false, "", "account is expired"
		}
	}
	if hashPwd(password, salt) != storedHash {
		return false, "", "username or password is incorrect"
	}
	return true, identifier, ""
}

func identifierAllowsWorker(identifier, username, workerID string) bool {
	workerID = strings.TrimSpace(workerID)
	if workerID == "" {
		return false
	}
	candidates := []string{identifier, username}
	for _, value := range candidates {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t' }) {
			part = strings.TrimSpace(part)
			if part == "*" || strings.EqualFold(part, workerID) {
				return true
			}
		}
	}
	return false
}

func normalizeAuthMethod(method string) string {
	method = strings.ToLower(strings.TrimSpace(method))
	switch method {
	case "", "manual", "password":
		return "local"
	case "oauth", "oauth2", "sso", "oidc":
		return "oidc"
	default:
		return method
	}
}
func isAccountImportHeader(record []string) bool {
	for _, field := range record {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "username", "user", "account", "password", "identifier", "worker_id", "allowed_workers", "expiry_days", "expiry", "expires_in_days":
			return true
		}
	}
	return false
}

func accountImportHeaderIndexes(record []string) map[string]int {
	indexes := map[string]int{"username": 0, "password": 1, "identifier": 2, "expiry_days": 3}
	for i, field := range record {
		switch strings.ToLower(strings.TrimSpace(field)) {
		case "username", "user", "account":
			indexes["username"] = i
		case "password":
			indexes["password"] = i
		case "identifier", "worker_id", "allowed_workers":
			indexes["identifier"] = i
		case "expiry_days", "expiry", "expires_in_days":
			indexes["expiry_days"] = i
		}
	}
	return indexes
}

func csvField(record []string, index int) string {
	if index < 0 || index >= len(record) {
		return ""
	}
	return strings.TrimSpace(record[index])
}
func generateSalt() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashPwd(password, salt string) string {
	h := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(h[:])
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

// berLength encodes an ASN.1 BER definite length (handles >127 bytes).
func berLength(n int) []byte {
	if n < 0x80 {
		return []byte{byte(n)}
	}
	// Multi-byte length
	var buf []byte
	for v := n; v > 0; v >>= 8 {
		buf = append([]byte{byte(v & 0xff)}, buf...)
	}
	return append([]byte{byte(0x80 | len(buf))}, buf...)
}

// berWrap wraps payload with tag + BER length.
func berWrap(tag byte, payload []byte) []byte {
	return append(append([]byte{tag}, berLength(len(payload))...), payload...)
}

// berOctetString builds a BER TLV with the given tag.
func berOctetString(tag byte, data []byte) []byte {
	return berWrap(tag, data)
}

// concat joins multiple byte slices.
func concat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}
