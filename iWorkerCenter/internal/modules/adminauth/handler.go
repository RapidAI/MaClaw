package adminauth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	mrand "math/rand"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Handler provides auth endpoints.
type Handler struct {
	write    *sql.DB
	read     *sql.DB
	captchas *captchaStore
	sessions *sessionStore
}

// NewHandler creates an auth Handler. It ensures a default admin exists.
func NewHandler(write, read *sql.DB) *Handler {
	h := &Handler{
		write:    write,
		read:     read,
		captchas: newCaptchaStore(),
		sessions: newSessionStore(),
	}
	h.ensureDefaultAdmin()
	return h
}

// RegisterRoutes registers public auth routes (no auth required).
func (h *Handler) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/auth/captcha", h.handleCaptcha)
	mux.HandleFunc("/auth/login", h.handleLogin)
	mux.HandleFunc("/auth/check", h.handleCheck)
}

// RegisterAdminRoutes registers admin-only profile routes.
func (h *Handler) RegisterAdminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/admin/profile", h.handleProfile)
	mux.HandleFunc("/admin/password", h.handlePassword)
}

// extractToken gets the session token from cookie or header.
func extractToken(r *http.Request) string {
	if c, err := r.Cookie("iwc_session"); err == nil && c.Value != "" {
		return c.Value
	}
	return r.Header.Get("X-Session-Token")
}

// Authenticate checks if the request has a valid session token.
// It also injects the tenant_id into the request context.
func (h *Handler) Authenticate(r *http.Request) bool {
	token := extractToken(r)
	if token == "" {
		return false
	}
	return h.sessions.valid(token)
}

// AuthenticateWithContext checks the session and returns a new request
// with tenant_id injected into the context.
func (h *Handler) AuthenticateWithContext(r *http.Request) (*http.Request, bool) {
	token := extractToken(r)
	if token == "" {
		return r, false
	}
	sess := h.sessions.get(token)
	if sess == nil {
		return r, false
	}
	ctx := tenant.WithTenantID(r.Context(), sess.tenantID)
	return r.WithContext(ctx), true
}

// AuthenticatedTenantID returns the tenant ID from the session.
func (h *Handler) AuthenticatedTenantID(r *http.Request) string {
	token := extractToken(r)
	if token == "" {
		return ""
	}
	sess := h.sessions.get(token)
	if sess == nil {
		return ""
	}
	return sess.tenantID
}

// authenticatedUserID returns the user ID from the session, or empty string.
func (h *Handler) authenticatedUserID(r *http.Request) string {
	token := extractToken(r)
	if token == "" {
		return ""
	}
	sess := h.sessions.get(token)
	if sess == nil {
		return ""
	}
	return sess.userID
}

// --- Captcha ---

func (h *Handler) handleCaptcha(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	id, question, _ := h.captchas.generate()
	response.OK(w, map[string]string{"captcha_id": id, "question": question})
}

// --- Login ---

func (h *Handler) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}
	var req struct {
		TenantID   string `json:"tenant_id"`
		Username   string `json:"username"`
		Password   string `json:"password"`
		CaptchaID  string `json:"captcha_id"`
		CaptchaAns int    `json:"captcha_answer"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}

	// Verify captcha first
	if !h.captchas.verify(req.CaptchaID, req.CaptchaAns) {
		response.Error(w, http.StatusForbidden, "CAPTCHA_FAILED", "验证码错误")
		return
	}

	username := strings.TrimSpace(req.Username)
	if username == "" {
		username = "admin"
	}
	tenantID := strings.TrimSpace(req.TenantID)

	// If tenant_id not provided, auto-select if only one tenant exists
	if tenantID == "" {
		var count int
		_ = h.read.QueryRow(`SELECT COUNT(*) FROM tenants WHERE status='active'`).Scan(&count)
		if count == 1 {
			_ = h.read.QueryRow(`SELECT id FROM tenants WHERE status='active' LIMIT 1`).Scan(&tenantID)
		}
	}

	if tenantID == "" {
		response.BadRequest(w, "TENANT_REQUIRED", "请选择企业")
		return
	}

	// Constant-time-ish credential check: always hash even on user-not-found
	var storedHash, userID, email, salt string
	err := h.read.QueryRow(
		`SELECT id, password_hash, salt, email FROM admin_users WHERE username=? AND tenant_id=?`,
		username, tenantID).Scan(&userID, &storedHash, &salt, &email)
	if err != nil {
		// Hash anyway to prevent timing attacks
		_ = hashPassword("dummy", "dummy_salt")
		response.Error(w, http.StatusUnauthorized, "AUTH_FAILED", "用户名或密码错误")
		return
	}
	if hashPassword(req.Password, salt) != storedHash {
		response.Error(w, http.StatusUnauthorized, "AUTH_FAILED", "用户名或密码错误")
		return
	}

	// Create session with tenant_id
	token := h.sessions.create(userID, username, tenantID)
	http.SetCookie(w, &http.Cookie{
		Name:     "iwc_session",
		Value:    token,
		Path:     "/",
		MaxAge:   86400 * 7,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	response.OK(w, map[string]any{
		"status":    "ok",
		"username":  username,
		"email":     email,
		"tenant_id": tenantID,
	})
}

// --- Check session ---

func (h *Handler) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET")
		return
	}
	token := extractToken(r)
	sess := h.sessions.get(token)
	if sess == nil {
		response.Error(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未登录")
		return
	}
	var email string
	_ = h.read.QueryRow(`SELECT email FROM admin_users WHERE id=?`, sess.userID).Scan(&email)
	response.OK(w, map[string]any{"username": sess.username, "email": email, "tenant_id": sess.tenantID})
}

// --- Profile (email) ---

func (h *Handler) handleProfile(w http.ResponseWriter, r *http.Request) {
	userID := h.authenticatedUserID(r)
	if userID == "" {
		response.Error(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未登录")
		return
	}
	switch r.Method {
	case http.MethodGet:
		h.getProfile(w, userID)
	case http.MethodPut:
		h.updateProfile(w, r, userID)
	default:
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use GET or PUT")
	}
}

func (h *Handler) getProfile(w http.ResponseWriter, userID string) {
	var username, email string
	err := h.read.QueryRow(`SELECT username, email FROM admin_users WHERE id=?`, userID).Scan(&username, &email)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]string{"username": username, "email": email})
}

func (h *Handler) updateProfile(w http.ResponseWriter, r *http.Request, userID string) {
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	email := strings.TrimSpace(req.Email)
	now := time.Now().Format(time.RFC3339)
	_, err := h.write.Exec(`UPDATE admin_users SET email=?, updated_at=? WHERE id=?`, email, now, userID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

// --- Password ---

func (h *Handler) handlePassword(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use PUT")
		return
	}
	userID := h.authenticatedUserID(r)
	if userID == "" {
		response.Error(w, http.StatusUnauthorized, "NOT_AUTHENTICATED", "未登录")
		return
	}
	var req struct {
		OldPassword string `json:"old_password"`
		NewPassword string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}
	if len(req.NewPassword) < 4 {
		response.BadRequest(w, "WEAK_PASSWORD", "密码至少 4 个字符")
		return
	}

	// Verify old password for this specific user
	var storedHash, salt string
	err := h.read.QueryRow(`SELECT password_hash, salt FROM admin_users WHERE id=?`, userID).Scan(&storedHash, &salt)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	if hashPassword(req.OldPassword, salt) != storedHash {
		response.Error(w, http.StatusForbidden, "WRONG_PASSWORD", "旧密码错误")
		return
	}

	// Generate new salt for new password
	newSalt := generateSalt()
	newHash := hashPassword(req.NewPassword, newSalt)
	now := time.Now().Format(time.RFC3339)
	_, err = h.write.Exec(`UPDATE admin_users SET password_hash=?, salt=?, updated_at=? WHERE id=?`, newHash, newSalt, now, userID)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}
	response.OK(w, map[string]string{"status": "ok"})
}

// --- Default admin ---

func (h *Handler) ensureDefaultAdmin() {
	// In multi-tenant mode, admin users are created during tenant setup.
	// No default admin is created automatically.
	var count int
	_ = h.read.QueryRow(`SELECT COUNT(*) FROM tenants`).Scan(&count)
	if count > 0 {
		return // tenants exist, admin was created during setup
	}
	// If no tenants exist yet, don't create a default admin either.
	// The first admin will be created via /auth/setup-tenant.
	log.Printf("[adminauth] no tenants found, waiting for initial setup via /auth/setup-tenant")
}

// --- Helpers ---

func generateSalt() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func hashPassword(password, salt string) string {
	h := sha256.Sum256([]byte(salt + ":" + password))
	return hex.EncodeToString(h[:])
}

func randomToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// --- Captcha store ---

type captchaEntry struct {
	answer    int
	expiresAt time.Time
}

type captchaStore struct {
	mu      sync.Mutex
	entries map[string]captchaEntry
}

func newCaptchaStore() *captchaStore {
	return &captchaStore{entries: make(map[string]captchaEntry)}
}

func (cs *captchaStore) generate() (id, question string, answer int) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	// Clean expired entries to prevent memory leak
	now := time.Now()
	for k, v := range cs.entries {
		if now.After(v.expiresAt) {
			delete(cs.entries, k)
		}
	}
	a := mrand.Intn(20) + 1
	b := mrand.Intn(20) + 1
	op := "+"
	ans := a + b
	if mrand.Intn(2) == 0 && a > b {
		op = "-"
		ans = a - b
	}
	question = fmt.Sprintf("%d %s %d = ?", a, op, b)
	idBytes := make([]byte, 8)
	_, _ = rand.Read(idBytes)
	id = hex.EncodeToString(idBytes)
	cs.entries[id] = captchaEntry{answer: ans, expiresAt: now.Add(5 * time.Minute)}
	return id, question, ans
}

func (cs *captchaStore) verify(id string, answer int) bool {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	entry, ok := cs.entries[id]
	if !ok {
		return false
	}
	delete(cs.entries, id) // one-time use
	if time.Now().After(entry.expiresAt) {
		return false
	}
	return entry.answer == answer
}

// --- Session store ---

type sessionEntry struct {
	userID   string
	username string
	tenantID string
	expires  time.Time
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[string]*sessionEntry
}

func newSessionStore() *sessionStore {
	ss := &sessionStore{sessions: make(map[string]*sessionEntry)}
	// Background cleanup of expired sessions every 10 minutes
	go func() {
		for {
			time.Sleep(10 * time.Minute)
			ss.cleanup()
		}
	}()
	return ss
}

func (ss *sessionStore) create(userID, username, tenantID string) string {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	token := randomToken()
	ss.sessions[token] = &sessionEntry{
		userID:   userID,
		username: username,
		tenantID: tenantID,
		expires:  time.Now().Add(7 * 24 * time.Hour),
	}
	return token
}

func (ss *sessionStore) valid(token string) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	s, ok := ss.sessions[token]
	if !ok {
		return false
	}
	return time.Now().Before(s.expires)
}

func (ss *sessionStore) get(token string) *sessionEntry {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	s, ok := ss.sessions[token]
	if !ok || time.Now().After(s.expires) {
		return nil
	}
	return s
}

func (ss *sessionStore) cleanup() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	now := time.Now()
	for k, v := range ss.sessions {
		if now.After(v.expires) {
			delete(ss.sessions, k)
		}
	}
}
