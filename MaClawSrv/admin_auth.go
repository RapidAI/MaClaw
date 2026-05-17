package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"golang.org/x/crypto/bcrypt"
)

const (
	adminBootstrapVersion       = 1
	adminSessionTokenPrefix     = "mca_"
	adminSessionDuration        = 12 * time.Hour
	adminPasswordMinLength      = 12
	maxAdminSessionsPerUser     = 20
	adminBootstrapSetupTokenEnv = "MACLAW_ADMIN_SETUP_TOKEN"
)

type adminBootstrapState struct {
	Version       int       `json:"version"`
	Initialized   bool      `json:"initialized"`
	InitializedAt time.Time `json:"initialized_at,omitempty"`
	InitializedBy string    `json:"initialized_by,omitempty"`
}

type adminUserRecord struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	DisplayName  string    `json:"display_name,omitempty"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	Locale       string    `json:"locale,omitempty"`
	PasswordHash string    `json:"password_hash"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	LastLoginAt  time.Time `json:"last_login_at,omitempty"`
}

type adminUserPublic struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	DisplayName string    `json:"display_name,omitempty"`
	Role        string    `json:"role"`
	Status      string    `json:"status"`
	Locale      string    `json:"locale,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	LastLoginAt time.Time `json:"last_login_at,omitempty"`
}

type adminSessionRecord struct {
	ID        string    `json:"id"`
	TokenHash string    `json:"token_hash"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	RevokedAt time.Time `json:"revoked_at,omitempty"`
	RemoteIP  string    `json:"remote_ip,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
}

type adminSessionPublic struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type adminBootstrapInitializeRequest struct {
	SetupToken  string `json:"setup_token,omitempty"`
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
	Locale      string `json:"locale,omitempty"`
}

type adminLoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type adminChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}

func (s *HTTPServer) handleAdminBootstrapStatus(w http.ResponseWriter, r *http.Request) {
	initialized := adminBootstrapInitialized(s.svc.DataRoot())
	writeJSON(w, http.StatusOK, map[string]any{
		"initialized":          initialized,
		"setup_required":       !initialized,
		"setup_token_required": strings.TrimSpace(os.Getenv(adminBootstrapSetupTokenEnv)) != "",
		"password_policy": map[string]any{
			"min_length":            adminPasswordMinLength,
			"require_mixed_classes": false,
		},
	})
}

func (s *HTTPServer) handleAdminBootstrapInitialize(w http.ResponseWriter, r *http.Request) {
	var in adminBootstrapInitializeRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	clientIP := requestClientIP(r)
	limitKey := "admin-bootstrap:" + clientIP
	now := time.Now().UTC()
	if allowed, retryAfter := s.authLimiter.AllowWithRetry(limitKey, now); !allowed {
		writeAdminRateLimitError(w, retryAfter)
		return
	}
	if adminBootstrapInitialized(s.svc.DataRoot()) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "admin already initialized"})
		return
	}
	if !adminSetupTokenValid(in.SetupToken) {
		blockFor := s.authLimiter.RegisterFailure(limitKey, now)
		_ = s.recordAdminAudit(r.Context(), "admin.bootstrap_failed", "admin_bootstrap", "", map[string]string{"remote_ip": clientIP, "reason": "invalid_setup_token"})
		if blockFor > 0 {
			writeAdminRateLimitError(w, blockFor)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid setup token"})
		return
	}
	username := normalizeAdminUsername(in.Username)
	if err := validateAdminUsername(username); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateAdminPassword(in.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	users, err := loadAdminUsers(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if len(users) > 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "admin user already exists"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
		return
	}
	user := adminUserRecord{ID: newAdminID("admin_user"), Username: username, DisplayName: strings.TrimSpace(in.DisplayName), Role: "owner", Status: "active", Locale: normalizeAdminLocale(in.Locale), PasswordHash: string(hash), CreatedAt: now, UpdatedAt: now}
	if err := saveAdminUsers(s.svc.DataRoot(), []adminUserRecord{user}); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	state := adminBootstrapState{Version: adminBootstrapVersion, Initialized: true, InitializedAt: now, InitializedBy: user.ID}
	if err := saveAdminBootstrapState(s.svc.DataRoot(), state); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.authLimiter.ResetFailures(limitKey)
	_ = s.recordAdminAudit(r.Context(), "admin.bootstrap_initialize", "admin_user", user.ID, map[string]string{"username": user.Username, "remote_ip": clientIP})
	writeJSON(w, http.StatusCreated, map[string]any{"initialized": true, "admin": publicAdminUser(user)})
}

func (s *HTTPServer) handleAdminAuthLogin(w http.ResponseWriter, r *http.Request) {
	var in adminLoginRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	clientIP := requestClientIP(r)
	username := normalizeAdminUsername(in.Username)
	limitKey := "admin-login:" + clientIP + ":" + username
	now := time.Now().UTC()
	if allowed, retryAfter := s.authLimiter.AllowWithRetry(limitKey, now); !allowed {
		_ = s.recordAdminAudit(r.Context(), "admin.login_rate_limited", "admin_user", username, map[string]string{"remote_ip": clientIP})
		writeAdminRateLimitError(w, retryAfter)
		return
	}
	user, users, err := findAdminUserByUsername(s.svc.DataRoot(), username)
	if err != nil || user == nil || user.Status != "active" || bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.Password)) != nil {
		blockFor := s.authLimiter.RegisterFailure(limitKey, now)
		_ = s.recordAdminAudit(r.Context(), "admin.login_failed", "admin_user", username, map[string]string{"remote_ip": clientIP})
		if blockFor > 0 {
			writeAdminRateLimitError(w, blockFor)
			return
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid admin credentials"})
		return
	}
	token, session, err := newAdminSession(*user, clientIP, r.UserAgent(), now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to create session"})
		return
	}
	sessions, err := loadAdminSessions(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	sessions = pruneAdminSessions(append(sessions, session), user.ID, now)
	if err := saveAdminSessions(s.svc.DataRoot(), sessions); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for i := range users {
		if users[i].ID == user.ID {
			users[i].LastLoginAt = now
			users[i].UpdatedAt = now
			*user = users[i]
			break
		}
	}
	_ = saveAdminUsers(s.svc.DataRoot(), users)
	s.authLimiter.ResetFailures(limitKey)
	_ = s.recordAdminAudit(r.Context(), "admin.login", "admin_user", user.ID, map[string]string{"username": user.Username, "remote_ip": clientIP})
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "token_type": "admin_secret", "expires_at": session.ExpiresAt, "admin": publicAdminUser(*user), "session": publicAdminSession(session)})
}

func (s *HTTPServer) handleAdminAuthLogout(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("X-MaClaw-Admin-Secret")
	session, err := revokeAdminSession(s.svc.DataRoot(), token, time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.logout", "admin_session", session.ID, map[string]string{"username": session.Username, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

func (s *HTTPServer) handleAdminAuthMe(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("X-MaClaw-Admin-Secret")
	session, user, err := getAdminSessionUser(s.svc.DataRoot(), token, time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"auth_type": "admin_secret", "session": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"auth_type": "admin_session", "admin": publicAdminUser(*user), "session": publicAdminSession(*session)})
}

func (s *HTTPServer) handleAdminAuthChangePassword(w http.ResponseWriter, r *http.Request) {
	token := r.Header.Get("X-MaClaw-Admin-Secret")
	session, user, err := getAdminSessionUser(s.svc.DataRoot(), token, time.Now().UTC())
	if err != nil || session == nil || user == nil {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin session is required"})
		return
	}
	var in adminChangePasswordRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(in.OldPassword)) != nil {
		_ = s.recordAdminAudit(r.Context(), "admin.password_change_failed", "admin_user", user.ID, map[string]string{"username": user.Username, "reason": "invalid_old_password", "remote_ip": requestClientIP(r)})
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid old password"})
		return
	}
	if err := validateAdminPassword(in.NewPassword); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if in.OldPassword == in.NewPassword {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "new password must be different"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
		return
	}
	users, err := loadAdminUsers(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	updated := false
	now := time.Now().UTC()
	for i := range users {
		if users[i].ID == user.ID {
			users[i].PasswordHash = string(hash)
			users[i].UpdatedAt = now
			*user = users[i]
			updated = true
			break
		}
	}
	if !updated {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "admin user not found"})
		return
	}
	if err := saveAdminUsers(s.svc.DataRoot(), users); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	passwordChanged := true
	revoked, err := revokeAdminUserSessionsExcept(s.svc.DataRoot(), user.ID, session.ID, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.password_changed", "admin_user", user.ID, map[string]string{"username": user.Username, "revoked_sessions": strconv.Itoa(revoked), "password_changed": strconv.FormatBool(passwordChanged), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "admin": publicAdminUser(*user), "revoked_sessions": revoked})
}

func (s *HTTPServer) adminSecretAuthorized(provided string) bool {
	return s.adminSecret != "" && subtle.ConstantTimeCompare([]byte(provided), []byte(s.adminSecret)) == 1
}

func (s *HTTPServer) adminSessionAuthorized(provided string) bool {
	_, _, err := getAdminSessionUser(s.svc.DataRoot(), provided, time.Now().UTC())
	return err == nil
}

func (s *HTTPServer) recordAdminAudit(ctx context.Context, action, resourceType, resourceID string, metadata map[string]string) error {
	return s.svc.RecordAuditEvent(ctx, agentservice.AuditEvent{ActorType: "admin", Action: action, ResourceType: resourceType, ResourceID: resourceID, Metadata: metadata})
}

func adminBootstrapInitialized(dataRoot string) bool {
	state, err := loadAdminBootstrapState(dataRoot)
	if err == nil && state.Initialized {
		return true
	}
	users, err := loadAdminUsers(dataRoot)
	if err != nil {
		return false
	}
	for _, user := range users {
		if user.Role == "owner" && user.Status == "active" {
			return true
		}
	}
	return false
}

func loadAdminBootstrapState(dataRoot string) (adminBootstrapState, error) {
	var state adminBootstrapState
	if err := readAdminJSON(dataRoot, "admin_bootstrap.json", &state); err != nil {
		return adminBootstrapState{}, err
	}
	return state, nil
}

func saveAdminBootstrapState(dataRoot string, state adminBootstrapState) error {
	return writeAdminJSON(dataRoot, "admin_bootstrap.json", state)
}

func loadAdminUsers(dataRoot string) ([]adminUserRecord, error) {
	var users []adminUserRecord
	if err := readAdminJSON(dataRoot, "admin_users.json", &users); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []adminUserRecord{}, nil
		}
		return nil, err
	}
	return users, nil
}

func saveAdminUsers(dataRoot string, users []adminUserRecord) error {
	return writeAdminJSON(dataRoot, "admin_users.json", users)
}

func loadAdminSessions(dataRoot string) ([]adminSessionRecord, error) {
	var sessions []adminSessionRecord
	if err := readAdminJSON(dataRoot, "admin_sessions.json", &sessions); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []adminSessionRecord{}, nil
		}
		return nil, err
	}
	return sessions, nil
}

func saveAdminSessions(dataRoot string, sessions []adminSessionRecord) error {
	return writeAdminJSON(dataRoot, "admin_sessions.json", sessions)
}

func findAdminUserByUsername(dataRoot, username string) (*adminUserRecord, []adminUserRecord, error) {
	users, err := loadAdminUsers(dataRoot)
	if err != nil {
		return nil, nil, err
	}
	for i := range users {
		if users[i].Username == username {
			return &users[i], users, nil
		}
	}
	return nil, users, os.ErrNotExist
}

func getAdminSessionUser(dataRoot, token string, now time.Time) (*adminSessionRecord, *adminUserRecord, error) {
	if !strings.HasPrefix(strings.TrimSpace(token), adminSessionTokenPrefix) {
		return nil, nil, os.ErrNotExist
	}
	tokenHash := hashAdminToken(token)
	sessions, err := loadAdminSessions(dataRoot)
	if err != nil {
		return nil, nil, err
	}
	for i := range sessions {
		session := sessions[i]
		if session.TokenHash != tokenHash || !session.RevokedAt.IsZero() || !session.ExpiresAt.After(now) {
			continue
		}
		users, err := loadAdminUsers(dataRoot)
		if err != nil {
			return nil, nil, err
		}
		for j := range users {
			if users[j].ID == session.UserID && users[j].Status == "active" {
				return &sessions[i], &users[j], nil
			}
		}
	}
	return nil, nil, os.ErrNotExist
}

func revokeAdminSession(dataRoot, token string, now time.Time) (*adminSessionRecord, error) {
	tokenHash := hashAdminToken(token)
	sessions, err := loadAdminSessions(dataRoot)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		if sessions[i].TokenHash == tokenHash && sessions[i].RevokedAt.IsZero() {
			sessions[i].RevokedAt = now
			if err := saveAdminSessions(dataRoot, sessions); err != nil {
				return nil, err
			}
			return &sessions[i], nil
		}
	}
	return nil, os.ErrNotExist
}

func revokeAdminUserSessionsExcept(dataRoot, userID, keepSessionID string, now time.Time) (int, error) {
	sessions, err := loadAdminSessions(dataRoot)
	if err != nil {
		return 0, err
	}
	revoked := 0
	for i := range sessions {
		if sessions[i].UserID != userID || sessions[i].ID == keepSessionID || !sessions[i].RevokedAt.IsZero() || !sessions[i].ExpiresAt.After(now) {
			continue
		}
		sessions[i].RevokedAt = now
		revoked++
	}
	if revoked == 0 {
		return 0, nil
	}
	return revoked, saveAdminSessions(dataRoot, sessions)
}

func newAdminSession(user adminUserRecord, remoteIP, userAgent string, now time.Time) (string, adminSessionRecord, error) {
	token, err := randomAdminToken()
	if err != nil {
		return "", adminSessionRecord{}, err
	}
	session := adminSessionRecord{ID: newAdminID("admin_session"), TokenHash: hashAdminToken(token), UserID: user.ID, Username: user.Username, Role: user.Role, CreatedAt: now, ExpiresAt: now.Add(adminSessionDuration), RemoteIP: remoteIP, UserAgent: trimMax(userAgent, 200)}
	return token, session, nil
}

func pruneAdminSessions(sessions []adminSessionRecord, userID string, now time.Time) []adminSessionRecord {
	activeForUser := 0
	out := make([]adminSessionRecord, 0, len(sessions))
	for i := len(sessions) - 1; i >= 0; i-- {
		s := sessions[i]
		if !s.RevokedAt.IsZero() || !s.ExpiresAt.After(now) {
			continue
		}
		if s.UserID == userID {
			activeForUser++
			if activeForUser > maxAdminSessionsPerUser {
				continue
			}
		}
		out = append(out, s)
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

func adminSetupTokenValid(provided string) bool {
	expected := strings.TrimSpace(os.Getenv(adminBootstrapSetupTokenEnv))
	if expected == "" {
		return true
	}
	provided = strings.TrimSpace(provided)
	if len(provided) != len(expected) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(provided), []byte(expected)) == 1
}

func readAdminJSON(dataRoot, name string, out any) error {
	data, err := os.ReadFile(filepath.Join(adminStateDir(dataRoot), name))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func writeAdminJSON(dataRoot, name string, value any) error {
	dir := adminStateDir(dataRoot)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o600)
}

func adminStateDir(dataRoot string) string {
	return filepath.Join(dataRoot, "state")
}

func validateAdminUsername(username string) error {
	if len(username) < 3 || len(username) > 64 {
		return errors.New("username must be 3-64 characters")
	}
	for _, r := range username {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			continue
		}
		return errors.New("username may contain lowercase letters, digits, dot, dash, and underscore")
	}
	return nil
}

func validateAdminPassword(password string) error {
	if len(password) < adminPasswordMinLength {
		return errors.New("password is too short")
	}
	return nil
}

func normalizeAdminUsername(v string) string {
	return strings.ToLower(strings.TrimSpace(v))
}

func normalizeAdminRole(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "", "operator":
		return "operator"
	case "owner":
		return "owner"
	default:
		return ""
	}
}
func normalizeAdminLocale(v string) string {
	switch strings.ToLower(strings.ReplaceAll(strings.TrimSpace(v), "_", "-")) {
	case "en", "en-us":
		return "en-US"
	case "zh", "zh-cn", "zh-hans", "zh-hans-cn":
		return "zh-CN"
	default:
		return "zh-CN"
	}
}

func publicAdminUser(user adminUserRecord) adminUserPublic {
	return adminUserPublic{ID: user.ID, Username: user.Username, DisplayName: user.DisplayName, Role: user.Role, Status: user.Status, Locale: user.Locale, CreatedAt: user.CreatedAt, UpdatedAt: user.UpdatedAt, LastLoginAt: user.LastLoginAt}
}

func publicAdminSession(session adminSessionRecord) adminSessionPublic {
	return adminSessionPublic{ID: session.ID, UserID: session.UserID, Username: session.Username, Role: session.Role, CreatedAt: session.CreatedAt, ExpiresAt: session.ExpiresAt}
}

func randomAdminToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return adminSessionTokenPrefix + base64.RawURLEncoding.EncodeToString(b[:]), nil
}

func newAdminID(prefix string) string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return prefix + "_" + hex.EncodeToString([]byte(time.Now().UTC().Format("20060102150405.000000000")))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}

func hashAdminToken(token string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(token)))
	return hex.EncodeToString(sum[:])
}

func trimMax(value string, max int) string {
	value = strings.TrimSpace(value)
	if max <= 0 || len(value) <= max {
		return value
	}
	out := value[:max]
	for !utf8.ValidString(out) && len(out) > 0 {
		out = out[:len(out)-1]
	}
	return out
}

func writeAdminRateLimitError(w http.ResponseWriter, retryAfter time.Duration) {
	seconds := int(retryAfter.Seconds())
	if seconds <= 0 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	writeJSON(w, http.StatusTooManyRequests, map[string]any{"error": "too many admin attempts", "retry_after_seconds": seconds})
}

type adminCreateUserRequest struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	DisplayName string `json:"display_name,omitempty"`
	Role        string `json:"role,omitempty"`
	Locale      string `json:"locale,omitempty"`
}
type adminUpdateUserRequest struct {
	Status        *string `json:"status,omitempty"`
	DisplayName   *string `json:"display_name,omitempty"`
	Locale        *string `json:"locale,omitempty"`
	NewPassword   *string `json:"new_password,omitempty"`
	ConfirmUnsafe bool    `json:"confirm_unsafe,omitempty"`
}

type adminSessionAdminView struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	Username  string    `json:"username"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
	RevokedAt time.Time `json:"revoked_at,omitempty"`
	RemoteIP  string    `json:"remote_ip,omitempty"`
	UserAgent string    `json:"user_agent,omitempty"`
	Active    bool      `json:"active"`
}

func (s *HTTPServer) handleAdminAuthCreateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in adminCreateUserRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	username := normalizeAdminUsername(in.Username)
	if err := validateAdminUsername(username); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if err := validateAdminPassword(in.Password); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	role := normalizeAdminRole(in.Role)
	if role == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "role must be owner or operator"})
		return
	}
	users, err := loadAdminUsers(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	for _, user := range users {
		if user.Username == username {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "admin username already exists"})
			return
		}
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(in.Password), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
		return
	}
	now := time.Now().UTC()
	user := adminUserRecord{ID: newAdminID("admin_user"), Username: username, DisplayName: trimMax(in.DisplayName, 120), Role: role, Status: "active", Locale: normalizeAdminLocale(in.Locale), PasswordHash: string(hash), CreatedAt: now, UpdatedAt: now}
	users = append(users, user)
	if err := saveAdminUsers(s.svc.DataRoot(), users); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.user_created", "admin_user", user.ID, map[string]string{"username": user.Username, "role": user.Role, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusCreated, map[string]any{"admin": publicAdminUser(user)})
}
func (s *HTTPServer) handleAdminAuthUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	users, err := loadAdminUsers(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	items := make([]adminUserPublic, 0, len(users))
	for _, user := range users {
		items = append(items, publicAdminUser(user))
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Username < items[j].Username })
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleAdminAuthUpdateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	var in adminUpdateUserRequest
	if !decodeJSON(w, r, &in) {
		return
	}
	userID := r.PathValue("adminUserId")
	users, err := loadAdminUsers(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	idx := -1
	for i := range users {
		if users[i].ID == userID {
			idx = i
			break
		}
	}
	if idx < 0 {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "admin user not found"})
		return
	}
	oldStatus := users[idx].Status
	if in.Status != nil {
		status := strings.ToLower(strings.TrimSpace(*in.Status))
		if status != "active" && status != "suspended" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "status must be active or suspended"})
			return
		}
		if users[idx].Role == "owner" && status != "active" && activeOwnerCount(users) <= 1 && !in.ConfirmUnsafe {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm_unsafe is required when suspending the last active owner"})
			return
		}
		users[idx].Status = status
	}
	if in.DisplayName != nil {
		users[idx].DisplayName = trimMax(*in.DisplayName, 120)
	}
	if in.Locale != nil {
		users[idx].Locale = normalizeAdminLocale(*in.Locale)
	}
	passwordChanged := false
	if in.NewPassword != nil {
		if err := validateAdminPassword(*in.NewPassword); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		hash, err := bcrypt.GenerateFromPassword([]byte(*in.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to hash password"})
			return
		}
		users[idx].PasswordHash = string(hash)
		passwordChanged = true
	}
	users[idx].UpdatedAt = time.Now().UTC()
	if err := saveAdminUsers(s.svc.DataRoot(), users); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	revoked := 0
	if (oldStatus == "active" && users[idx].Status != "active") || passwordChanged {
		revoked, err = revokeAdminUserSessionsExcept(s.svc.DataRoot(), users[idx].ID, "", time.Now().UTC())
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}
	_ = s.recordAdminAudit(r.Context(), "admin.user_updated", "admin_user", users[idx].ID, map[string]string{"username": users[idx].Username, "old_status": oldStatus, "new_status": users[idx].Status, "revoked_sessions": strconv.Itoa(revoked), "password_changed": strconv.FormatBool(passwordChanged), "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"admin": publicAdminUser(users[idx]), "revoked_sessions": revoked})
}

func (s *HTTPServer) handleAdminAuthSessions(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	sessions, err := loadAdminSessions(s.svc.DataRoot())
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	now := time.Now().UTC()
	items := make([]adminSessionAdminView, 0, len(sessions))
	for _, session := range sessions {
		if userID != "" && session.UserID != userID {
			continue
		}
		items = append(items, adminSessionAdminView{ID: session.ID, UserID: session.UserID, Username: session.Username, Role: session.Role, CreatedAt: session.CreatedAt, ExpiresAt: session.ExpiresAt, RevokedAt: session.RevokedAt, RemoteIP: session.RemoteIP, UserAgent: session.UserAgent, Active: session.RevokedAt.IsZero() && session.ExpiresAt.After(now)})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleAdminAuthRevokeSession(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdminOwner(w, r) {
		return
	}
	confirm, err := parseOptionalBoolQuery(r, "confirm")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	if confirm == nil || !*confirm {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "confirm=true is required"})
		return
	}
	session, err := revokeAdminSessionByID(s.svc.DataRoot(), r.PathValue("sessionId"), time.Now().UTC())
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "admin session not found"})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.session_revoked", "admin_session", session.ID, map[string]string{"username": session.Username, "remote_ip": requestClientIP(r)})
	writeJSON(w, http.StatusOK, map[string]any{"status": "revoked", "session": publicAdminSession(*session)})
}

func (s *HTTPServer) requireAdminOwner(w http.ResponseWriter, r *http.Request) bool {
	token := r.Header.Get("X-MaClaw-Admin-Secret")
	if s.adminSecretAuthorized(token) {
		return true
	}
	_, user, err := getAdminSessionUser(s.svc.DataRoot(), token, time.Now().UTC())
	if err == nil && user != nil && user.Role == "owner" && user.Status == "active" {
		return true
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "admin owner is required"})
	return false
}

func activeOwnerCount(users []adminUserRecord) int {
	count := 0
	for _, user := range users {
		if user.Role == "owner" && user.Status == "active" {
			count++
		}
	}
	return count
}

func revokeAdminSessionByID(dataRoot, sessionID string, now time.Time) (*adminSessionRecord, error) {
	sessions, err := loadAdminSessions(dataRoot)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		if sessions[i].ID == sessionID {
			if sessions[i].RevokedAt.IsZero() {
				sessions[i].RevokedAt = now
				if err := saveAdminSessions(dataRoot, sessions); err != nil {
					return nil, err
				}
			}
			return &sessions[i], nil
		}
	}
	return nil, os.ErrNotExist
}
