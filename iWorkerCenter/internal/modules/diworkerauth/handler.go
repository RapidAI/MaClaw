package diworkerauth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
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
	mux.HandleFunc("/admin/diworker-auth/accounts", h.handleAccounts)
	mux.HandleFunc("/admin/diworker-auth/accounts/", h.handleAccountByID)
	mux.HandleFunc("/admin/diworker-auth/import-csv", h.handleImportCSV)
}

// RegisterAuthRoutes registers the public authentication endpoint.
func (h *Handler) RegisterAuthRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/diworker-auth/authenticate", h.handleAuthenticate)
}

// ── LDAP config (stored in system-like key-value via a dedicated row) ──

const ldapConfigKey = "diworker_ldap_config"

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
		if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	cfg := h.loadLDAPConfig()
	if !cfg.Enabled || cfg.Host == "" {
		response.OK(w, map[string]any{"success": false, "error": "LDAP 未配置或未启用"})
		return
	}
	if err := ldapBind(&cfg, req.Username, req.Password); err != nil {
		response.OK(w, map[string]any{"success": false, "error": err.Error()})
		return
	}
	response.OK(w, map[string]any{"success": true})
}

// ── LDAP bind (lightweight ASN.1/BER) ──

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
		return fmt.Errorf("连接失败: %w", err)
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
		return fmt.Errorf("发送失败: %w", err)
	}
	buf := make([]byte, 1024)
	n, err := conn.Read(buf)
	if err != nil {
		return fmt.Errorf("读取失败: %w", err)
	}
	// Parse result code from BindResponse
	if rc := parseLDAPResult(buf[:n]); rc != 0 {
		return fmt.Errorf("认证失败 (LDAP result code %d)", rc)
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
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
	for {
		record, err := csvR.Read()
		if err == io.EOF {
			break
		}
		line++
		if err != nil {
			res.Errors = append(res.Errors, fmt.Sprintf("行 %d: %v", line, err))
			res.Skipped++
			continue
		}
		if len(record) < 2 {
			res.Errors = append(res.Errors, fmt.Sprintf("行 %d: 至少需要用户名和密码", line))
			res.Skipped++
			continue
		}
		username := strings.TrimSpace(record[0])
		password := strings.TrimSpace(record[1])
		if username == "" || password == "" {
			res.Errors = append(res.Errors, fmt.Sprintf("行 %d: 用户名或密码为空", line))
			res.Skipped++
			continue
		}
		identifier := ""
		if len(record) > 2 {
			identifier = strings.TrimSpace(record[2])
		}
		expiryDays := 0
		if len(record) > 3 {
			if d, err := strconv.Atoi(strings.TrimSpace(record[3])); err == nil {
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
		Method   string `json:"method"` // "ldap" or "local"
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}

	switch req.Method {
	case "ldap":
		cfg := h.loadLDAPConfig()
		if !cfg.Enabled || cfg.Host == "" {
			response.OK(w, map[string]any{"authenticated": false, "error": "LDAP 未配置"})
			return
		}
		if err := ldapBind(&cfg, req.Username, req.Password); err != nil {
			response.OK(w, map[string]any{"authenticated": false, "error": err.Error()})
			return
		}
		response.OK(w, map[string]any{"authenticated": true})

	case "local":
		tenantID := tenant.RequestTenantID(r)
		var storedHash, salt string
		var disabledInt int
		var expiresAt sql.NullString
		err := h.read.QueryRow(
			`SELECT password_hash, salt, disabled, expires_at FROM diworker_accounts WHERE tenant_id=? AND username=?`,
			tenantID, req.Username).Scan(&storedHash, &salt, &disabledInt, &expiresAt)
		if err != nil {
			_ = hashPwd("dummy", "dummy_salt") // timing
			response.OK(w, map[string]any{"authenticated": false, "error": "用户名或密码错误"})
			return
		}
		if disabledInt != 0 {
			response.OK(w, map[string]any{"authenticated": false, "error": "账户已禁用"})
			return
		}
		if expiresAt.Valid {
			if t, err := time.Parse(time.RFC3339, expiresAt.String); err == nil && time.Now().After(t) {
				response.OK(w, map[string]any{"authenticated": false, "error": "账户已过期"})
				return
			}
		}
		if hashPwd(req.Password, salt) != storedHash {
			response.OK(w, map[string]any{"authenticated": false, "error": "用户名或密码错误"})
			return
		}
		response.OK(w, map[string]any{"authenticated": true})

	default:
		response.BadRequest(w, "UNKNOWN_METHOD", "method 必须是 ldap 或 local")
	}
}

// ── helpers ──

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
