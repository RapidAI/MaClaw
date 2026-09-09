package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/database"
)

// databaseProfileRefresher is the runtime bridge used by the admin database
// profile configuration API. *agentservice.CoreAgentExecutor implements it;
// tests may inject a fake. Nil is allowed: refresh then becomes a no-op.
type databaseProfileRefresher interface {
	RefreshDatabaseProfiles(tenantID, userID string, profiles []database.Profile)
}

// SetDatabaseProfileRuntime wires the runtime database bridge used by the
// admin profile configuration endpoints. The refresher receives profile
// updates for cached per-user managers; the resolver is used by the test
// endpoint to resolve secret_ref values without exposing them to responses.
type databaseApprovalIssuer interface {
	IssueDatabaseApproval(req agentservice.ExecuteRequest, approvalReq database.ApprovalRequest) (database.ApprovalContext, error)
	SetDatabaseEnabled(tenantID, userID string, enabled bool)
	SnapshotDatabaseMetrics(tenantID, userID string) database.MetricsSnapshot
}

func (s *HTTPServer) SetDatabaseProfileRuntime(refresher databaseProfileRefresher, resolver database.SecretResolver) {
	s.databaseProfileRefresher = refresher
	s.databaseSecretResolver = resolver
	if issuer, ok := refresher.(databaseApprovalIssuer); ok {
		s.databaseApprovalIssuer = issuer
	}
}

// adminDatabaseProfileSummary is the non-sensitive projection returned by the
// profile list endpoint. secret_ref, DSN and file paths never leave the host;
// has_secret_ref lets the admin UI distinguish bound from unbound profiles.
type adminDatabaseProfileSummary struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	Type                database.SourceType `json:"type"`
	Status              string              `json:"status"`
	Host                string              `json:"host,omitempty"`
	Port                int                 `json:"port,omitempty"`
	Database            string              `json:"database,omitempty"`
	Username            string              `json:"username,omitempty"`
	DefaultSchema       string              `json:"default_schema,omitempty"`
	Sheet               string              `json:"sheet,omitempty"`
	SchemaVersion       int                 `json:"schema_version,omitempty"`
	ReadOnly            bool                `json:"read_only"`
	WriteEnabled        bool                `json:"write_enabled,omitempty"`
	AllowDDL            bool                `json:"allow_ddl,omitempty"`
	AllowExternalHost   bool                `json:"allow_external_host,omitempty"`
	Disabled            bool                `json:"disabled,omitempty"`
	TLSMode             string              `json:"tls_mode,omitempty"`
	DataClassification  string              `json:"data_classification,omitempty"`
	HasSecretRef        bool                `json:"has_secret_ref"`
	SSHSessionID        string              `json:"ssh_session_id,omitempty"`
	ReplicaHost         string              `json:"replica_host,omitempty"`
	ReplicaPort         int                 `json:"replica_port,omitempty"`
	ReplicaSSHSessionID string              `json:"replica_ssh_session_id,omitempty"`
}

// mergeDatabaseProfileUpdate keeps bound credentials and local paths when the
// admin form omits them. List responses never echo secret_ref, DSN or
// file_path, so an empty update must not wipe the stored values. Fields that
// the list does return (host, replica, ssh session ids) are caller-authoritative:
// omitting them clears the stored value.
func mergeDatabaseProfileUpdate(existing, incoming database.Profile) database.Profile {
	if strings.TrimSpace(incoming.SecretRef) == "" {
		incoming.SecretRef = existing.SecretRef
	}
	if strings.TrimSpace(incoming.DSN) == "" {
		incoming.DSN = existing.DSN
	}
	if strings.TrimSpace(incoming.FilePath) == "" {
		incoming.FilePath = existing.FilePath
	}
	if incoming.SchemaVersion == 0 {
		incoming.SchemaVersion = existing.SchemaVersion
	}
	if strings.TrimSpace(incoming.TLS.CAFile) == "" {
		incoming.TLS.CAFile = existing.TLS.CAFile
	}
	return incoming
}

func adminDatabaseProfileSummaryOf(p database.Profile) adminDatabaseProfileSummary {
	status := "configured"
	if p.Disabled {
		status = "disabled"
	} else if err := database.ValidateProfile(p); err != nil {
		status = "invalid"
	}
	return adminDatabaseProfileSummary{
		ID:                  p.ID,
		Name:                p.Name,
		Type:                p.Type,
		Status:              status,
		Host:                p.Host,
		Port:                p.Port,
		Database:            p.Database,
		Username:            p.Username,
		DefaultSchema:       p.DefaultSchema,
		Sheet:               p.Sheet,
		SchemaVersion:       p.SchemaVersion,
		ReadOnly:            p.ReadOnly,
		WriteEnabled:        p.WriteEnabled,
		AllowDDL:            p.AllowDDL,
		AllowExternalHost:   p.AllowExternalHost,
		Disabled:            p.Disabled,
		TLSMode:             p.TLS.Mode,
		DataClassification:  p.DataClassification,
		HasSecretRef:        strings.TrimSpace(p.SecretRef) != "",
		SSHSessionID:        p.SSHSessionID,
		ReplicaHost:         p.ReplicaHost,
		ReplicaPort:         p.ReplicaPort,
		ReplicaSSHSessionID: p.ReplicaSSHSessionID,
	}
}

// databaseAdminScope resolves and validates the target tenant/user of a
// database profile admin call. Both identifiers are required query
// parameters and must survive path-traversal checks before touching storage.
func (s *HTTPServer) databaseAdminScope(w http.ResponseWriter, r *http.Request) (agentservice.Principal, bool) {
	tenantID := strings.TrimSpace(r.URL.Query().Get("tenant_id"))
	userID := strings.TrimSpace(r.URL.Query().Get("user_id"))
	if tenantID == "" || userID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_id and user_id are required"})
		return agentservice.Principal{}, false
	}
	if !isSafeID(tenantID) || !isSafeID(userID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid tenant_id or user_id"})
		return agentservice.Principal{}, false
	}
	if !s.requireExistingTenantUser(w, r, tenantID, userID) {
		return agentservice.Principal{}, false
	}
	return agentservice.Principal{TenantID: tenantID, UserID: userID}, true
}

// databaseAdminRateLimited enforces a small per-admin-identity rate limit on
// the database profile configuration endpoints. The key is the credential
// hash, never the credential itself, so bucket state cannot leak secrets.
func (s *HTTPServer) databaseAdminRateLimited(w http.ResponseWriter, r *http.Request) bool {
	key := "admin-database:" + hashAdminToken(strings.TrimSpace(r.Header.Get("X-MaClaw-Admin-Secret"))) + ":" + requestClientIP(r)
	if allowed, retryAfter := s.databaseAdminLimiter.AllowWithRetry(key, time.Now().UTC()); !allowed {
		writeAdminRateLimitError(w, retryAfter)
		return true
	}
	return false
}

// databaseProfilesForScope reads the user's stored (unsanitized) config and
// returns its database profiles. A missing config file means no profiles.
func (s *HTTPServer) databaseProfilesForScope(r *http.Request, p agentservice.Principal) (corelib.AppConfig, []database.Profile, error) {
	cfg, err := s.svc.GetRawUserConfig(r.Context(), p)
	if err != nil {
		if errors.Is(err, agentservice.ErrUserConfigNotFound) {
			return corelib.AppConfig{}, nil, nil
		}
		return corelib.AppConfig{}, nil, err
	}
	return cfg.AppConfig, cfg.AppConfig.DatabaseProfiles, nil
}

func (s *HTTPServer) refreshDatabaseProfilesRuntime(p agentservice.Principal, profiles []database.Profile) {
	if s.databaseProfileRefresher == nil {
		return
	}
	s.databaseProfileRefresher.RefreshDatabaseProfiles(p.TenantID, p.UserID, profiles)
}

// rejectInlineDatabaseSecrets refuses bodies that carry credentials under
// password/secret/token-style keys. DisallowUnknownFields already rejects
// unknown fields on decode; this explicit scan gives a clearer, stable error
// and stays effective if the profile schema ever grows.
func rejectInlineDatabaseSecrets(body []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return errors.New("invalid json body")
	}
	for key := range fields {
		lower := strings.ToLower(strings.TrimSpace(key))
		if lower == "secret_ref" {
			continue
		}
		if strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "secret") {
			return errors.New("inline credentials are not accepted; bind credentials with secret_ref")
		}
	}
	return nil
}

// --- Idempotency-Key persistence -------------------------------------------

const (
	adminDatabaseProfilesIdemFile = "database_profiles_idem.json"
	adminDatabaseProfilesIdemTTL  = 24 * time.Hour
)

type adminDatabaseProfileIdemRecord struct {
	Fingerprint string          `json:"fingerprint"`
	Status      int             `json:"status"`
	Result      json.RawMessage `json:"result"`
	CreatedAt   time.Time       `json:"created_at"`
}

// adminDatabaseIdemScopeKey scopes an Idempotency-Key to the target
// tenant/user so the same key value can be reused across users.
func adminDatabaseIdemScopeKey(p agentservice.Principal, key string) string {
	return p.TenantID + "\x00" + p.UserID + "\x00" + key
}

func loadAdminDatabaseIdemRecords(dataRoot string) map[string]adminDatabaseProfileIdemRecord {
	records := map[string]adminDatabaseProfileIdemRecord{}
	if err := readAdminJSON(dataRoot, adminDatabaseProfilesIdemFile, &records); err != nil {
		// Missing or unreadable store starts empty; a corrupt file must not
		// block configuration writes (the write below replaces it).
		return map[string]adminDatabaseProfileIdemRecord{}
	}
	return records
}

// replayAdminDatabaseIdem looks up a stored idempotency record. Same
// fingerprint replays the first result; a different one conflicts (409).
// Entries older than the TTL are pruned whenever the store is written.
func (s *HTTPServer) replayAdminDatabaseIdem(w http.ResponseWriter, p agentservice.Principal, key, fingerprint string) (handled bool) {
	records := loadAdminDatabaseIdemRecords(s.svc.DataRoot())
	record, ok := records[adminDatabaseIdemScopeKey(p, key)]
	if !ok {
		return false
	}
	if record.Fingerprint != fingerprint {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "idempotency key was already used with a different request", "code": "idempotency_conflict"})
		return true
	}
	w.Header().Set("Idempotent-Replay", "true")
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(record.Status)
	_, _ = w.Write(record.Result)
	return true
}

func (s *HTTPServer) storeAdminDatabaseIdem(p agentservice.Principal, key, fingerprint string, status int, result []byte) {
	records := loadAdminDatabaseIdemRecords(s.svc.DataRoot())
	now := time.Now().UTC()
	for k, record := range records {
		if now.Sub(record.CreatedAt) > adminDatabaseProfilesIdemTTL {
			delete(records, k)
		}
	}
	records[adminDatabaseIdemScopeKey(p, key)] = adminDatabaseProfileIdemRecord{
		Fingerprint: fingerprint,
		Status:      status,
		Result:      json.RawMessage(result),
		CreatedAt:   now,
	}
	if err := writeAdminJSON(s.svc.DataRoot(), adminDatabaseProfilesIdemFile, records); err != nil {
		// The mutation already succeeded; losing the replay record degrades a
		// retried request into a repeat write, which upsert semantics tolerate.
		_ = s.recordAdminAudit(context.Background(), "admin.database_profile_idem_store_failed", "database_profile", key, map[string]string{"tenant_id": p.TenantID, "user_id": p.UserID})
	}
}

// databaseProfileIdempotencyKey validates the optional Idempotency-Key
// header. Multiple values or a blank value are rejected (400) so a replay
// can never be ambiguous.
func databaseProfileIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	values := r.Header.Values("Idempotency-Key")
	if len(values) == 0 {
		return "", true
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Idempotency-Key must be a single non-empty value"})
		return "", false
	}
	return strings.TrimSpace(values[0]), true
}

// --- Handlers --------------------------------------------------------------

func (s *HTTPServer) handleAdminDatabaseProfilesList(w http.ResponseWriter, r *http.Request) {
	if s.databaseAdminRateLimited(w, r) {
		return
	}
	// Listing exposes host/database names of every tenant, so it is an
	// owner-only operation just like the mutating endpoints.
	if !s.requireAdminOwner(w, r) {
		return
	}
	p, ok := s.databaseAdminScope(w, r)
	if !ok {
		return
	}
	_, profiles, err := s.databaseProfilesForScope(r, p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	items := make([]adminDatabaseProfileSummary, 0, len(profiles))
	for _, profile := range profiles {
		items = append(items, adminDatabaseProfileSummaryOf(profile))
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleAdminDatabaseProfileUpsert(w http.ResponseWriter, r *http.Request) {
	if s.databaseAdminRateLimited(w, r) {
		return
	}
	if !s.requireAdminOwner(w, r) {
		return
	}
	p, ok := s.databaseAdminScope(w, r)
	if !ok {
		return
	}
	idemKey, ok := databaseProfileIdempotencyKey(w, r)
	if !ok {
		return
	}
	body, err := readLimitedBody(w, r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	if err := rejectInlineDatabaseSecrets(body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	fingerprint := sha256.Sum256(body)
	fingerprintHex := hex.EncodeToString(fingerprint[:])
	// Serialize the idempotency check-and-store window so concurrent retries
	// of the same key cannot both commit the write. The same mutex also
	// covers the read→persist→refresh of rotate-secret/delete, so all three
	// mutating handlers are atomic against each other.
	s.databaseAdminIdemMu.Lock()
	defer s.databaseAdminIdemMu.Unlock()
	if idemKey != "" && s.replayAdminDatabaseIdem(w, p, idemKey, fingerprintHex) {
		return
	}
	var profile database.Profile
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&profile); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	profile.ID = strings.TrimSpace(profile.ID)
	if !isSafeID(profile.ID) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid profile id"})
		return
	}
	appConfig, profiles, err := s.databaseProfilesForScope(r, p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	replaced := false
	if existing, found := findDatabaseProfile(profiles, profile.ID); found {
		profile = mergeDatabaseProfileUpdate(existing, profile)
		replaced = true
	}
	if err := database.ValidateProfile(profile); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	next := append([]database.Profile(nil), profiles...)
	if replaced {
		next = replaceDatabaseProfile(next, profile)
	} else {
		next = append(next, profile)
	}
	if err := s.persistDatabaseProfiles(w, r, p, appConfig, next); err != nil {
		return
	}
	action := "admin.database_profile_created"
	if replaced {
		action = "admin.database_profile_updated"
	}
	_ = s.recordAdminAudit(r.Context(), action, "database_profile", profile.ID, map[string]string{"tenant_id": p.TenantID, "user_id": p.UserID, "type": string(profile.Type)})
	result, _ := json.Marshal(map[string]any{"status": "ok", "profile": adminDatabaseProfileSummaryOf(profile)})
	if idemKey != "" {
		s.storeAdminDatabaseIdem(p, idemKey, fingerprintHex, http.StatusOK, result)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(result)
}

func (s *HTTPServer) handleAdminDatabaseProfileTest(w http.ResponseWriter, r *http.Request) {
	if s.databaseAdminRateLimited(w, r) {
		return
	}
	// The probe dials the configured host, which would otherwise give any
	// admin session an SSRF oracle against internal networks.
	if !s.requireAdminOwner(w, r) {
		return
	}
	p, ok := s.databaseAdminScope(w, r)
	if !ok {
		return
	}
	profileID := strings.TrimSpace(r.PathValue("profileId"))
	_, profiles, err := s.databaseProfilesForScope(r, p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	profile, found := findDatabaseProfile(profiles, profileID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "database profile not found"})
		return
	}
	if strings.TrimSpace(profile.SecretRef) != "" && s.databaseSecretResolver == nil {
		// Fail closed with a stable class instead of attempting a credential-
		// less connection that would surface driver banners.
		writeJSON(w, http.StatusOK, map[string]any{"status": "failed", "error_class": "authentication", "error": "secret resolver is not configured"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	// Probe with a throwaway manager so test traffic never touches cached
	// runtime connections and ping/inspect cannot execute user SQL.
	manager := database.NewManager([]database.Profile{profile}, s.databaseSecretResolver)
	defer manager.Close()
	if provider, ok := s.databaseProfileRefresher.(interface {
		DatabaseTunnelDialer(tenantID, userID string) database.TunnelDialer
	}); ok {
		manager.SetTunnelDialer(provider.DatabaseTunnelDialer(p.TenantID, p.UserID))
	}
	_, capabilities, err := manager.Connect(ctx, profile.ID)
	if err != nil {
		class, message := splitDatabaseErrorClass(err)
		writeJSON(w, http.StatusOK, map[string]any{"status": "failed", "error_class": class, "error": redactSupportBundleText(s.svc.DataRoot(), message)})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.database_profile_tested", "database_profile", profile.ID, map[string]string{"tenant_id": p.TenantID, "user_id": p.UserID, "type": string(profile.Type), "result": "ok"})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "capabilities": capabilities})
}

func (s *HTTPServer) handleAdminDatabaseProfileRotateSecret(w http.ResponseWriter, r *http.Request) {
	if s.databaseAdminRateLimited(w, r) {
		return
	}
	if !s.requireAdminOwner(w, r) {
		return
	}
	p, ok := s.databaseAdminScope(w, r)
	if !ok {
		return
	}
	var in struct {
		SecretRef string `json:"secret_ref"`
	}
	if !decodeJSON(w, r, &in) {
		return
	}
	in.SecretRef = strings.TrimSpace(in.SecretRef)
	if in.SecretRef == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "secret_ref is required"})
		return
	}
	profileID := strings.TrimSpace(r.PathValue("profileId"))
	// Serialize read→persist→refresh under the same mutex as upsert/delete so
	// concurrent profile mutations cannot overwrite each other (lost update).
	s.databaseAdminIdemMu.Lock()
	defer s.databaseAdminIdemMu.Unlock()
	appConfig, profiles, err := s.databaseProfilesForScope(r, p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	profile, found := findDatabaseProfile(profiles, profileID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "database profile not found"})
		return
	}
	profile.SecretRef = in.SecretRef
	// Bumping the schema version invalidates approval contexts and cursors
	// minted against the old credential, while UpdateProfiles below closes
	// live connections and purges pending mutations of the changed profile.
	profile.SchemaVersion++
	next := replaceDatabaseProfile(profiles, profile)
	if err := s.persistDatabaseProfiles(w, r, p, appConfig, next); err != nil {
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.database_profile_secret_rotated", "database_profile", profile.ID, map[string]string{"tenant_id": p.TenantID, "user_id": p.UserID, "type": string(profile.Type)})
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "profile": adminDatabaseProfileSummaryOf(profile)})
}

func (s *HTTPServer) handleAdminDatabaseProfileDelete(w http.ResponseWriter, r *http.Request) {
	if s.databaseAdminRateLimited(w, r) {
		return
	}
	if !s.requireAdminOwner(w, r) {
		return
	}
	if err := requireAdminConfirmation(r, "database profile delete operations"); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	p, ok := s.databaseAdminScope(w, r)
	if !ok {
		return
	}
	profileID := strings.TrimSpace(r.PathValue("profileId"))
	// Serialize read→persist→refresh under the same mutex as upsert/rotate so
	// concurrent profile mutations cannot overwrite each other (lost update).
	s.databaseAdminIdemMu.Lock()
	defer s.databaseAdminIdemMu.Unlock()
	appConfig, profiles, err := s.databaseProfilesForScope(r, p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	profile, found := findDatabaseProfile(profiles, profileID)
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "database profile not found"})
		return
	}
	next := make([]database.Profile, 0, len(profiles))
	for _, existing := range profiles {
		if existing.ID != profile.ID {
			next = append(next, existing)
		}
	}
	if err := s.persistDatabaseProfiles(w, r, p, appConfig, next); err != nil {
		return
	}
	// Keep the audit reference after removal so connection history remains
	// attributable; the profile itself is gone from the runtime projection.
	_ = s.recordAdminAudit(r.Context(), "admin.database_profile_deleted", "database_profile", profile.ID, map[string]string{"tenant_id": p.TenantID, "user_id": p.UserID, "type": string(profile.Type)})
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "id": profile.ID})
}

// persistDatabaseProfiles writes the updated profile list back to the user's
// config and refreshes cached runtime managers. Refresh happens only after a
// successful write so the runtime never diverges ahead of durable config.
func (s *HTTPServer) persistDatabaseProfiles(w http.ResponseWriter, r *http.Request, p agentservice.Principal, appConfig corelib.AppConfig, profiles []database.Profile) error {
	appConfig.DatabaseProfiles = profiles
	if _, err := s.svc.UpdateUserConfig(r.Context(), p, appConfig); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return err
	}
	s.refreshDatabaseProfilesRuntime(p, profiles)
	return nil
}

func findDatabaseProfile(profiles []database.Profile, id string) (database.Profile, bool) {
	for _, profile := range profiles {
		if profile.ID == id {
			return profile, true
		}
	}
	return database.Profile{}, false
}

func replaceDatabaseProfile(profiles []database.Profile, next database.Profile) []database.Profile {
	out := make([]database.Profile, 0, len(profiles))
	for _, profile := range profiles {
		if profile.ID == next.ID {
			out = append(out, next)
			continue
		}
		out = append(out, profile)
	}
	return out
}

// splitDatabaseErrorClass extracts the stable "class: message" prefix emitted
// by corelib/database error classification so transport responses can expose
// the class without leaking driver details beyond the redacted message.
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

// readLimitedBody reads a bounded JSON body for handlers that need the raw
// bytes (idempotency fingerprinting) before decoding.
func readLimitedBody(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	defer r.Body.Close()
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, errors.New("empty body")
	}
	return bytes.TrimSpace(body), nil
}

// databaseAuditEventToService maps a metadata-only database audit event onto
// the central tenant audit chain. No SQL text, parameter values or tokens are
// carried — only fingerprints, counts and non-secret identifiers; the service
// applies its standard metadata redaction on top.
func databaseAuditEventToService(p agentservice.Principal, event database.AuditEvent) agentservice.AuditEvent {
	metadata := map[string]string{}
	if event.SessionID != "" {
		metadata["session_id"] = event.SessionID
	}
	if event.ConnectionID != "" {
		metadata["connection_id"] = event.ConnectionID
	}
	if event.SQLFingerprint != "" {
		metadata["sql_fingerprint"] = event.SQLFingerprint
	}
	if event.ParameterCount > 0 {
		metadata["parameter_count"] = strconv.Itoa(event.ParameterCount)
	}
	if event.AffectedRows != 0 {
		metadata["affected_rows"] = strconv.FormatInt(event.AffectedRows, 10)
	}
	if event.Risk != "" {
		metadata["risk"] = event.Risk
	}
	if event.ResultClass != "" {
		metadata["result_class"] = event.ResultClass
	}
	if event.ApprovalID != "" {
		metadata["approval_id"] = event.ApprovalID
	}
	if event.ReceiptID != "" {
		metadata["receipt_id"] = event.ReceiptID
	}
	return agentservice.AuditEvent{
		TenantID:     strings.TrimSpace(p.TenantID),
		UserID:       strings.TrimSpace(p.UserID),
		ActorType:    "user",
		ActorTenant:  strings.TrimSpace(p.TenantID),
		ActorUser:    strings.TrimSpace(p.UserID),
		Action:       "database." + strings.TrimSpace(event.Action),
		ResourceType: "database_profile",
		ResourceID:   strings.TrimSpace(event.ProfileID),
		Metadata:     metadata,
		CreatedAt:    event.Timestamp,
	}
}

// wireDatabaseAuditSink routes database tool audit events into the central
// tenant audit store. It replaces the executor's per-user JSONL fallback with
// the unified chain the design doc requires; sink failures are logged, never
// surfaced to the tool call.
func (s *HTTPServer) handleAdminDatabaseApprovalIssue(w http.ResponseWriter, r *http.Request) {
	if s.databaseAdminRateLimited(w, r) {
		return
	}
	if !s.requireAdminOwner(w, r) {
		return
	}
	p, ok := s.databaseAdminScope(w, r)
	if !ok {
		return
	}
	if s.databaseApprovalIssuer == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "database runtime is not wired"})
		return
	}
	var body struct {
		SessionID         string `json:"session_id"`
		InstanceID        string `json:"instance_id"`
		ProfileID         string `json:"profile_id"`
		SQLFingerprint    string `json:"sql_fingerprint"`
		ParamsFingerprint string `json:"params_fingerprint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	if strings.TrimSpace(body.SQLFingerprint) == "" || strings.TrimSpace(body.SessionID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "session_id and sql_fingerprint are required"})
		return
	}
	cfg, _, err := s.databaseProfilesForScope(r, p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	issued, err := s.databaseApprovalIssuer.IssueDatabaseApproval(agentservice.ExecuteRequest{
		Principal: p,
		Session:   agentservice.Session{ID: strings.TrimSpace(body.SessionID)},
		Instance:  agentservice.Instance{ID: strings.TrimSpace(body.InstanceID)},
		Config:    cfg,
		DataDir:   s.svc.DataRoot(),
	}, database.ApprovalRequest{
		ProfileID:         strings.TrimSpace(body.ProfileID),
		SQLFingerprint:    strings.TrimSpace(body.SQLFingerprint),
		ParamsFingerprint: strings.TrimSpace(body.ParamsFingerprint),
		OwnerID:           p.UserID,
		SessionID:         strings.TrimSpace(body.SessionID),
	})
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	_ = s.recordAdminAudit(r.Context(), "admin.database_approval_issued", "database_approval", issued.ID, map[string]string{"tenant_id": p.TenantID, "user_id": p.UserID, "session_id": strings.TrimSpace(body.SessionID), "profile_id": strings.TrimSpace(body.ProfileID)})
	writeJSON(w, http.StatusOK, map[string]any{"approval_id": issued.ID, "expires_at": issued.ExpiresAt.UTC()})
}

func (s *HTTPServer) handleAdminDatabaseRuntime(w http.ResponseWriter, r *http.Request) {
	if s.databaseAdminRateLimited(w, r) {
		return
	}
	if !s.requireAdminOwner(w, r) {
		return
	}
	p, ok := s.databaseAdminScope(w, r)
	if !ok {
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json body"})
		return
	}
	appConfig, profiles, err := s.databaseProfilesForScope(r, p)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	enabled := body.Enabled
	appConfig.DatabaseToolEnabled = &enabled
	if err := s.persistDatabaseProfiles(w, r, p, appConfig, profiles); err != nil {
		return
	}
	if s.databaseApprovalIssuer != nil {
		s.databaseApprovalIssuer.SetDatabaseEnabled(p.TenantID, p.UserID, enabled)
	}
	_ = s.recordAdminAudit(r.Context(), "admin.database_runtime", "database_tool", p.UserID, map[string]string{"tenant_id": p.TenantID, "user_id": p.UserID, "enabled": strconv.FormatBool(enabled)})
	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled})
}

func (s *HTTPServer) handleAdminDatabaseMetrics(w http.ResponseWriter, r *http.Request) {
	if s.databaseAdminRateLimited(w, r) {
		return
	}
	if !s.requireAdminOwner(w, r) {
		return
	}
	p, ok := s.databaseAdminScope(w, r)
	if !ok {
		return
	}
	if s.databaseApprovalIssuer == nil {
		writeJSON(w, http.StatusOK, map[string]any{"metrics": database.MetricsSnapshot{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"metrics": s.databaseApprovalIssuer.SnapshotDatabaseMetrics(p.TenantID, p.UserID)})
}

func (s *HTTPServer) handleAdminDatabaseReceipt(w http.ResponseWriter, r *http.Request) {
	if s.databaseAdminRateLimited(w, r) {
		return
	}
	if !s.requireAdminOwner(w, r) {
		return
	}
	p, ok := s.databaseAdminScope(w, r)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.PathValue("receiptId"))
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "receiptId is required"})
		return
	}
	if rec, ok := s.lookupDatabaseReceiptFromAudit(r.Context(), p, id); ok {
		writeJSON(w, http.StatusOK, map[string]any{"receipt": rec})
		return
	}
	store, err := database.NewFileAuditStore(filepath.Join(s.svc.DataRoot(), "database_audit.jsonl"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": redactSupportBundleText(s.svc.DataRoot(), err.Error())})
		return
	}
	rec, err := store.LookupReceipt(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "receipt not found"})
		return
	}
	if owner, _ := rec["owner_id"].(string); owner != "" && owner != p.UserID {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "receipt not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"receipt": rec})
}

func (s *HTTPServer) lookupDatabaseReceiptFromAudit(ctx context.Context, p agentservice.Principal, id string) (map[string]any, bool) {
	if s == nil || s.svc == nil || id == "" {
		return nil, false
	}
	events, err := s.svc.ListAuditEvents(ctx, agentservice.ListAuditEventsInput{
		TenantID:     p.TenantID,
		UserID:       p.UserID,
		ResourceType: "database_profile",
	})
	if err != nil {
		return nil, false
	}
	for i := len(events) - 1; i >= 0; i-- {
		ev := events[i]
		if ev.Metadata["receipt_id"] != id || ev.Metadata["result_class"] != "ok" {
			continue
		}
		return receiptFromServiceAudit(ev), true
	}
	return nil, false
}

func receiptFromServiceAudit(ev agentservice.AuditEvent) map[string]any {
	rec := map[string]any{
		"kind":       "receipt",
		"receipt_id": ev.Metadata["receipt_id"],
		"action":     strings.TrimPrefix(ev.Action, "database."),
		"profile_id": ev.ResourceID,
		"owner_id":   ev.UserID,
		"timestamp":  ev.CreatedAt.UTC().Format(time.RFC3339Nano),
	}
	for _, key := range []string{"approval_id", "session_id", "connection_id", "sql_fingerprint", "parameter_count", "affected_rows"} {
		if v := ev.Metadata[key]; v != "" {
			rec[key] = v
		}
	}
	return rec
}

func wireDatabaseAuditSink(svc *agentservice.Service, executor *agentservice.CoreAgentExecutor) {
	if svc == nil || executor == nil {
		return
	}
	executor.DatabaseAuditSink = func(ctx context.Context, p agentservice.Principal, event database.AuditEvent) {
		if err := svc.RecordAuditEvent(ctx, databaseAuditEventToService(p, event)); err != nil {
			log.Printf("[database] central audit sink failed: %v", err)
		}
	}
}
