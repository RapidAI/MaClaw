package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

const (
	llmEndpointAPIKeysKey         = "llm_endpoint_api_keys_v1"
	llmEndpointAPIKeyIndexKey     = "llm_endpoint_api_key_index_v1"
	llmEndpointAPIKeyPrefix       = "sk-llmg-"
	llmEndpointAPIKeyMaxPerTenant = 50
	llmEndpointAPIKeyStoreVersion = 1
	llmEndpointAPIKeyNameMax      = 80
)

type llmEndpointAPIKeyAuthContextKey struct{}

type llmEndpointAPIKeyAuth struct {
	KeyID          string
	ServiceGroupID string
}

type llmEndpointAPIKey struct {
	ID               string    `json:"id"`
	TenantID         string    `json:"tenant_id"`
	Name             string    `json:"name,omitempty"`
	ServiceGroupID   string    `json:"service_group_id"`
	TokenHash        string    `json:"token_hash"`
	TokenPrefix      string    `json:"token_prefix"`
	CreatedByAdminID string    `json:"created_by_admin_id,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
}

type llmEndpointAPIKeyStore struct {
	Version int                 `json:"version"`
	Keys    []llmEndpointAPIKey `json:"keys,omitempty"`
}

type llmEndpointAPIKeyIndexEntry struct {
	TenantID string `json:"tenant_id"`
	KeyID    string `json:"key_id"`
}

type llmEndpointAPIKeyIndex struct {
	Version int                                    `json:"version"`
	Entries map[string]llmEndpointAPIKeyIndexEntry `json:"entries,omitempty"`
}

type llmEndpointAPIKeyView struct {
	ID               string `json:"id"`
	Name             string `json:"name,omitempty"`
	ServiceGroupID   string `json:"service_group_id"`
	ServiceGroupName string `json:"service_group_name,omitempty"`
	TokenPrefix      string `json:"token_prefix"`
	CreatedAt        string `json:"created_at"`
	Account          string `json:"account"`
}

type llmEndpointAPIKeyCreateRequest struct {
	ServiceGroupID string `json:"service_group_id"`
	Name           string `json:"name"`
}

var (
	llmEndpointAPIKeyMu            sync.RWMutex
	errLLMEndpointAPIKeyLimit      = fmt.Errorf("at most %d service-group API keys are allowed", llmEndpointAPIKeyMaxPerTenant)
	errSystemLLMUserInactive       = errors.New("sys_user account is not active")
	llmEndpointAPIKeyCreateMaxBody = int64(64 << 10)
)

func withLLMEndpointAPIKeyAuth(ctx context.Context, auth llmEndpointAPIKeyAuth) context.Context {
	if strings.TrimSpace(auth.ServiceGroupID) == "" {
		return ctx
	}
	return context.WithValue(ctx, llmEndpointAPIKeyAuthContextKey{}, auth)
}

func withLLMEndpointPrincipalContext(ctx context.Context, principal *auth.ViewerPrincipal) context.Context {
	if principal == nil || !principal.IsLLMEndpointAPIKey() {
		return ctx
	}
	return withLLMEndpointAPIKeyAuth(ctx, llmEndpointAPIKeyAuth{
		KeyID:          strings.TrimSpace(principal.APIKeyID),
		ServiceGroupID: strings.TrimSpace(principal.ServiceGroupID),
	})
}

func llmEndpointAPIKeyAuthFromContext(ctx context.Context) (llmEndpointAPIKeyAuth, bool) {
	auth, ok := ctx.Value(llmEndpointAPIKeyAuthContextKey{}).(llmEndpointAPIKeyAuth)
	if !ok || strings.TrimSpace(auth.ServiceGroupID) == "" {
		return llmEndpointAPIKeyAuth{}, false
	}
	return auth, true
}

func authenticateLLMEndpointRequest(r *http.Request, identity *auth.IdentityService, system store.SystemSettingsRepository) (*auth.ViewerPrincipal, error) {
	authz := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(strings.ToLower(authz), "bearer ") {
		return nil, auth.ErrInvalidUserCredentials
	}
	token := strings.TrimSpace(authz[7:])
	if strings.HasPrefix(token, llmEndpointAPIKeyPrefix) {
		return authenticateLLMEndpointAPIKey(r.Context(), identity, system, token)
	}
	return authenticateViewerRequest(r, identity)
}

func llmEndpointRateLimitBucket(principal *auth.ViewerPrincipal) string {
	if principal == nil {
		return ""
	}
	if principal.IsLLMEndpointAPIKey() && strings.TrimSpace(principal.APIKeyID) != "" {
		return principal.TenantID + "\x00llmkey:" + strings.TrimSpace(principal.APIKeyID)
	}
	return principal.TenantID + "\x00" + principal.Email
}

func authenticateLLMEndpointAPIKey(ctx context.Context, identity *auth.IdentityService, system store.SystemSettingsRepository, rawToken string) (*auth.ViewerPrincipal, error) {
	key, err := lookupLLMEndpointAPIKey(ctx, system, rawToken)
	if err != nil {
		return nil, err
	}
	if key == nil || strings.TrimSpace(key.ServiceGroupID) == "" {
		return nil, auth.ErrInvalidUserCredentials
	}
	user, err := ensureHubSystemLLMUser(ctx, identity, key.TenantID)
	if err != nil {
		if errors.Is(err, errSystemLLMUserInactive) {
			return nil, auth.ErrInvalidUserCredentials
		}
		return nil, err
	}
	return &auth.ViewerPrincipal{
		TenantID:       key.TenantID,
		UserID:         user.ID,
		Email:          llmservice.SystemLLMUserEmail,
		Kind:           auth.ViewerKindLLMEndpointAPIKey,
		ServiceGroupID: key.ServiceGroupID,
		APIKeyID:       key.ID,
	}, nil
}

func ensureHubSystemLLMUser(ctx context.Context, identity *auth.IdentityService, tenantID string) (*store.User, error) {
	tenantID = store.NormalizeTenantID(tenantID)
	email := llmservice.SystemLLMUserEmail
	userID := llmservice.SystemLLMUserID(tenantID)
	if identity == nil || identity.UsersRepo() == nil {
		return &store.User{ID: userID, TenantID: tenantID, Email: email, Status: "active", EnrollmentStatus: "approved"}, nil
	}
	users := identity.UsersRepo()
	if existing, err := users.GetByTenantEmail(ctx, tenantID, email); err != nil {
		return nil, err
	} else if existing != nil {
		if !systemLLMUserUsable(existing) {
			return nil, errSystemLLMUserInactive
		}
		return existing, nil
	}
	if existing, err := users.GetByID(ctx, userID); err != nil {
		return nil, err
	} else if existing != nil {
		if store.NormalizeTenantID(existing.TenantID) != tenantID || !strings.EqualFold(strings.TrimSpace(existing.Email), email) {
			return nil, fmt.Errorf("sys_user id %q is already bound to another account", userID)
		}
		if !systemLLMUserUsable(existing) {
			return nil, errSystemLLMUserInactive
		}
		return existing, nil
	}
	now := time.Now().UTC()
	user := &store.User{
		ID:               userID,
		TenantID:         tenantID,
		Email:            email,
		SN:               "SN-SYSUSER-" + tenantID,
		Status:           "active",
		EnrollmentStatus: "approved",
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if err := users.Create(ctx, user); err != nil {
		if existing, getErr := users.GetByTenantEmail(ctx, tenantID, email); getErr == nil && existing != nil {
			if !systemLLMUserUsable(existing) {
				return nil, errSystemLLMUserInactive
			}
			return existing, nil
		}
		if existing, getErr := users.GetByID(ctx, userID); getErr == nil && existing != nil {
			if store.NormalizeTenantID(existing.TenantID) != tenantID || !strings.EqualFold(strings.TrimSpace(existing.Email), email) {
				return nil, err
			}
			if !systemLLMUserUsable(existing) {
				return nil, errSystemLLMUserInactive
			}
			return existing, nil
		}
		return nil, err
	}
	return user, nil
}

func ListLLMEndpointAPIKeysHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tenantID := store.NormalizeTenantID(RequestTenantID(r))
		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		llmEndpointAPIKeyMu.RLock()
		keys, err := loadLLMEndpointAPIKeys(r.Context(), tenantSystem)
		llmEndpointAPIKeyMu.RUnlock()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_ENDPOINT_API_KEY_LOAD_FAILED", err.Error())
			return
		}
		keys = filterLLMEndpointAPIKeysByTenant(keys, tenantID)
		reg, err := llmservice.LoadRegistry(r.Context(), tenantSystem)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"keys":    llmEndpointAPIKeyViews(keys, reg),
			"account": llmservice.SystemLLMUserEmail,
		})
	}
}

func CreateLLMEndpointAPIKeyHandler(identity *auth.IdentityService, system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req llmEndpointAPIKeyCreateRequest
		if r.Body != nil {
			if err := json.NewDecoder(io.LimitReader(r.Body, llmEndpointAPIKeyCreateMaxBody)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
				writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
				return
			}
		}
		tenantID := store.NormalizeTenantID(RequestTenantID(r))
		serviceGroupID := strings.TrimSpace(req.ServiceGroupID)
		if serviceGroupID == "" {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_GROUP_REQUIRED", "service_group_id is required")
			return
		}
		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		reg, err := llmservice.LoadRegistry(r.Context(), tenantSystem)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		group := reg.FindModelServiceGroup(serviceGroupID)
		if group == nil {
			writeError(w, http.StatusBadRequest, "LLM_SERVICE_GROUP_NOT_FOUND", fmt.Sprintf("service group %q is not configured", serviceGroupID))
			return
		}
		if _, err := ensureHubSystemLLMUser(r.Context(), identity, tenantID); err != nil {
			if errors.Is(err, errSystemLLMUserInactive) {
				writeError(w, http.StatusConflict, "LLM_ENDPOINT_API_KEY_USER_INACTIVE", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "LLM_ENDPOINT_API_KEY_USER_FAILED", err.Error())
			return
		}
		rawToken, err := newLLMEndpointAPIKeyToken()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_ENDPOINT_API_KEY_ISSUE_FAILED", err.Error())
			return
		}
		adminID := ""
		if admin := AdminFromContext(r.Context()); admin != nil {
			adminID = strings.TrimSpace(admin.ID)
		}
		key := llmEndpointAPIKey{
			ID:               llmservice.NewID("llmk"),
			TenantID:         tenantID,
			Name:             truncateLLMEndpointAPIKeyName(req.Name),
			ServiceGroupID:   group.ID,
			TokenHash:        hashLLMEndpointAPIKey(rawToken),
			TokenPrefix:      llmEndpointAPIKeyDisplayPrefix(rawToken),
			CreatedByAdminID: adminID,
			CreatedAt:        time.Now().UTC(),
		}
		if err := saveCreatedLLMEndpointAPIKey(r.Context(), system, tenantSystem, key); err != nil {
			if errors.Is(err, errLLMEndpointAPIKeyLimit) {
				writeError(w, http.StatusBadRequest, "LLM_ENDPOINT_API_KEY_LIMIT", err.Error())
				return
			}
			writeError(w, http.StatusInternalServerError, "LLM_ENDPOINT_API_KEY_SAVE_FAILED", err.Error())
			return
		}
		writeLLMServiceCardAdminAudit(r.Context(), audit, tenantID, "llm.endpoint_api_key.create", map[string]any{
			"id":               key.ID,
			"service_group_id": key.ServiceGroupID,
			"name":             key.Name,
			"account":          llmservice.SystemLLMUserEmail,
		})
		view := llmEndpointAPIKeyViewFrom(key, reg)
		writeJSON(w, http.StatusOK, map[string]any{
			"key":              view,
			"email":            llmservice.SystemLLMUserEmail,
			"account":          llmservice.SystemLLMUserEmail,
			"access_token":     rawToken,
			"token_type":       "Bearer",
			"auth_header":      "Bearer " + rawToken,
			"service_group_id": key.ServiceGroupID,
		})
	}
}

func DeleteLLMEndpointAPIKeyHandler(system store.SystemSettingsRepository, audit store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := strings.TrimSpace(r.PathValue("id"))
		if id == "" {
			writeError(w, http.StatusBadRequest, "LLM_ENDPOINT_API_KEY_ID_REQUIRED", "id is required")
			return
		}
		tenantID := store.NormalizeTenantID(RequestTenantID(r))
		tenantSystem := scopedSystemSettingsForTenant(tenantID, system)
		deleted, err := deleteLLMEndpointAPIKey(r.Context(), system, tenantSystem, tenantID, id)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_ENDPOINT_API_KEY_DELETE_FAILED", err.Error())
			return
		}
		if !deleted {
			writeError(w, http.StatusNotFound, "LLM_ENDPOINT_API_KEY_NOT_FOUND", "api key not found")
			return
		}
		writeLLMServiceCardAdminAudit(r.Context(), audit, tenantID, "llm.endpoint_api_key.delete", map[string]any{
			"id":      id,
			"account": llmservice.SystemLLMUserEmail,
		})
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "id": id})
	}
}

func saveCreatedLLMEndpointAPIKey(ctx context.Context, system, tenantSystem store.SystemSettingsRepository, key llmEndpointAPIKey) error {
	llmEndpointAPIKeyMu.Lock()
	defer llmEndpointAPIKeyMu.Unlock()
	storeDoc, err := loadLLMEndpointAPIKeyStore(ctx, tenantSystem)
	if err != nil {
		return err
	}
	live := 0
	for _, existing := range storeDoc.Keys {
		if strings.TrimSpace(existing.ID) != "" {
			live++
		}
	}
	if live >= llmEndpointAPIKeyMaxPerTenant {
		return errLLMEndpointAPIKeyLimit
	}
	storeDoc.Keys = append(storeDoc.Keys, key)
	if err := persistLLMEndpointAPIKeyStore(ctx, tenantSystem, storeDoc); err != nil {
		return err
	}
	rollback := func() {
		dropLLMEndpointAPIKey(storeDoc, key.ID)
		if rbErr := persistLLMEndpointAPIKeyStore(ctx, tenantSystem, storeDoc); rbErr != nil {
			log.Printf("[llm-endpoint-api-key] rollback tenant store after index failure: %v", rbErr)
		}
	}
	index, err := loadLLMEndpointAPIKeyIndex(ctx, system)
	if err != nil {
		rollback()
		return err
	}
	if index.Entries == nil {
		index.Entries = map[string]llmEndpointAPIKeyIndexEntry{}
	}
	index.Entries[key.TokenHash] = llmEndpointAPIKeyIndexEntry{TenantID: key.TenantID, KeyID: key.ID}
	if err := persistLLMEndpointAPIKeyIndex(ctx, system, index); err != nil {
		rollback()
		return err
	}
	return nil
}

func deleteLLMEndpointAPIKey(ctx context.Context, system, tenantSystem store.SystemSettingsRepository, tenantID, id string) (bool, error) {
	llmEndpointAPIKeyMu.Lock()
	defer llmEndpointAPIKeyMu.Unlock()
	storeDoc, err := loadLLMEndpointAPIKeyStore(ctx, tenantSystem)
	if err != nil {
		return false, err
	}
	filtered := storeDoc.Keys[:0]
	var removed *llmEndpointAPIKey
	for i := range storeDoc.Keys {
		key := storeDoc.Keys[i]
		if key.ID == id && store.NormalizeTenantID(key.TenantID) == tenantID {
			copyKey := key
			removed = &copyKey
			continue
		}
		filtered = append(filtered, key)
	}
	if removed == nil {
		return false, nil
	}
	storeDoc.Keys = filtered
	if err := persistLLMEndpointAPIKeyStore(ctx, tenantSystem, storeDoc); err != nil {
		return false, err
	}
	index, err := loadLLMEndpointAPIKeyIndex(ctx, system)
	if err != nil {
		log.Printf("[llm-endpoint-api-key] deleted %s but index load failed: %v", id, err)
		return true, nil
	}
	if index.Entries != nil {
		delete(index.Entries, removed.TokenHash)
	}
	if err := persistLLMEndpointAPIKeyIndex(ctx, system, index); err != nil {
		log.Printf("[llm-endpoint-api-key] deleted %s but index save failed: %v", id, err)
	}
	return true, nil
}

func lookupLLMEndpointAPIKey(ctx context.Context, system store.SystemSettingsRepository, rawToken string) (*llmEndpointAPIKey, error) {
	llmEndpointAPIKeyMu.RLock()
	defer llmEndpointAPIKeyMu.RUnlock()
	hash := hashLLMEndpointAPIKey(rawToken)
	index, err := loadLLMEndpointAPIKeyIndex(ctx, system)
	if err != nil {
		return nil, err
	}
	entry, ok := index.Entries[hash]
	if !ok || strings.TrimSpace(entry.KeyID) == "" {
		return nil, nil
	}
	entryTenantID := store.NormalizeTenantID(entry.TenantID)
	tenantSystem := scopedSystemSettingsForTenant(entryTenantID, system)
	keys, err := loadLLMEndpointAPIKeys(ctx, tenantSystem)
	if err != nil {
		return nil, err
	}
	for i := range keys {
		if keys[i].ID == entry.KeyID && keys[i].TokenHash == hash && store.NormalizeTenantID(keys[i].TenantID) == entryTenantID {
			copyKey := keys[i]
			return &copyKey, nil
		}
	}
	return nil, nil
}

func loadLLMEndpointAPIKeys(ctx context.Context, tenantSystem store.SystemSettingsRepository) ([]llmEndpointAPIKey, error) {
	storeDoc, err := loadLLMEndpointAPIKeyStore(ctx, tenantSystem)
	if err != nil {
		return nil, err
	}
	return append([]llmEndpointAPIKey(nil), storeDoc.Keys...), nil
}

func loadLLMEndpointAPIKeyStore(ctx context.Context, tenantSystem store.SystemSettingsRepository) (*llmEndpointAPIKeyStore, error) {
	doc := &llmEndpointAPIKeyStore{Version: llmEndpointAPIKeyStoreVersion}
	if tenantSystem == nil {
		return doc, nil
	}
	raw, err := tenantSystem.Get(ctx, llmEndpointAPIKeysKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return doc, err
	}
	if err := json.Unmarshal([]byte(raw), doc); err != nil {
		return nil, err
	}
	if doc.Version == 0 {
		doc.Version = llmEndpointAPIKeyStoreVersion
	}
	return doc, nil
}

func persistLLMEndpointAPIKeyStore(ctx context.Context, tenantSystem store.SystemSettingsRepository, doc *llmEndpointAPIKeyStore) error {
	if tenantSystem == nil {
		return fmt.Errorf("system settings unavailable")
	}
	if doc == nil {
		doc = &llmEndpointAPIKeyStore{Version: llmEndpointAPIKeyStoreVersion}
	}
	doc.Version = llmEndpointAPIKeyStoreVersion
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return tenantSystem.Set(ctx, llmEndpointAPIKeysKey, string(data))
}

func loadLLMEndpointAPIKeyIndex(ctx context.Context, system store.SystemSettingsRepository) (*llmEndpointAPIKeyIndex, error) {
	doc := &llmEndpointAPIKeyIndex{Version: llmEndpointAPIKeyStoreVersion, Entries: map[string]llmEndpointAPIKeyIndexEntry{}}
	base := globalSystemSettings(system)
	if base == nil {
		return doc, nil
	}
	raw, err := base.Get(ctx, llmEndpointAPIKeyIndexKey)
	if err != nil || strings.TrimSpace(raw) == "" {
		return doc, err
	}
	if err := json.Unmarshal([]byte(raw), doc); err != nil {
		return nil, err
	}
	if doc.Entries == nil {
		doc.Entries = map[string]llmEndpointAPIKeyIndexEntry{}
	}
	if doc.Version == 0 {
		doc.Version = llmEndpointAPIKeyStoreVersion
	}
	return doc, nil
}

func persistLLMEndpointAPIKeyIndex(ctx context.Context, system store.SystemSettingsRepository, doc *llmEndpointAPIKeyIndex) error {
	base := globalSystemSettings(system)
	if base == nil {
		return fmt.Errorf("system settings unavailable")
	}
	if doc == nil {
		doc = &llmEndpointAPIKeyIndex{Version: llmEndpointAPIKeyStoreVersion, Entries: map[string]llmEndpointAPIKeyIndexEntry{}}
	}
	if doc.Entries == nil {
		doc.Entries = map[string]llmEndpointAPIKeyIndexEntry{}
	}
	doc.Version = llmEndpointAPIKeyStoreVersion
	data, err := json.Marshal(doc)
	if err != nil {
		return err
	}
	return base.Set(ctx, llmEndpointAPIKeyIndexKey, string(data))
}

func llmEndpointAPIKeyViews(keys []llmEndpointAPIKey, reg *llmservice.Registry) []llmEndpointAPIKeyView {
	sorted := append([]llmEndpointAPIKey(nil), keys...)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].CreatedAt.Equal(sorted[j].CreatedAt) {
			return sorted[i].ID > sorted[j].ID
		}
		return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
	})
	out := make([]llmEndpointAPIKeyView, 0, len(sorted))
	for _, key := range sorted {
		out = append(out, llmEndpointAPIKeyViewFrom(key, reg))
	}
	return out
}

func dropLLMEndpointAPIKey(storeDoc *llmEndpointAPIKeyStore, id string) {
	if storeDoc == nil {
		return
	}
	filtered := storeDoc.Keys[:0]
	for _, key := range storeDoc.Keys {
		if key.ID == id {
			continue
		}
		filtered = append(filtered, key)
	}
	storeDoc.Keys = filtered
}

func llmEndpointRateLimitLogMeta(principal *auth.ViewerPrincipal, extra map[string]any) map[string]any {
	if extra == nil {
		extra = map[string]any{}
	}
	attachLLMEndpointAPIKeyLogMeta(extra, principal)
	return extra
}

func filterLLMEndpointAPIKeysByTenant(keys []llmEndpointAPIKey, tenantID string) []llmEndpointAPIKey {
	tenantID = store.NormalizeTenantID(tenantID)
	out := make([]llmEndpointAPIKey, 0, len(keys))
	for _, key := range keys {
		if store.NormalizeTenantID(key.TenantID) == tenantID {
			out = append(out, key)
		}
	}
	return out
}

func systemLLMUserUsable(user *store.User) bool {
	if user == nil {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(user.Status))
	return status == "" || status == "active"
}

func attachLLMEndpointAPIKeyLogMeta(metadata map[string]any, principal *auth.ViewerPrincipal) {
	if metadata == nil || principal == nil || !principal.IsLLMEndpointAPIKey() {
		return
	}
	if id := strings.TrimSpace(principal.APIKeyID); id != "" {
		metadata["api_key_id"] = id
	}
	if group := strings.TrimSpace(principal.ServiceGroupID); group != "" {
		metadata["service_group_id"] = group
	}
	metadata["account"] = llmservice.SystemLLMUserEmail
}

func llmEndpointAPIKeyViewFrom(key llmEndpointAPIKey, reg *llmservice.Registry) llmEndpointAPIKeyView {
	name := strings.TrimSpace(key.Name)
	groupName := key.ServiceGroupID
	if reg != nil {
		if group := reg.FindModelServiceGroup(key.ServiceGroupID); group != nil {
			groupName = firstNonEmpty(group.Name, group.ID)
			if name == "" {
				name = groupName
			}
		}
	}
	if name == "" {
		name = key.ServiceGroupID
	}
	createdAt := ""
	if !key.CreatedAt.IsZero() {
		createdAt = key.CreatedAt.UTC().Format(time.RFC3339)
	}
	return llmEndpointAPIKeyView{
		ID:               key.ID,
		Name:             name,
		ServiceGroupID:   key.ServiceGroupID,
		ServiceGroupName: groupName,
		TokenPrefix:      key.TokenPrefix,
		CreatedAt:        createdAt,
		Account:          llmservice.SystemLLMUserEmail,
	}
}

func newLLMEndpointAPIKeyToken() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return llmEndpointAPIKeyPrefix + hex.EncodeToString(buf), nil
}

func hashLLMEndpointAPIKey(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func llmEndpointAPIKeyDisplayPrefix(raw string) string {
	raw = strings.TrimSpace(raw)
	if len(raw) <= 16 {
		return raw
	}
	return raw[:16]
}

func truncateLLMEndpointAPIKeyName(name string) string {
	name = strings.TrimSpace(name)
	runes := []rune(name)
	if len(runes) <= llmEndpointAPIKeyNameMax {
		return name
	}
	return string(runes[:llmEndpointAPIKeyNameMax])
}
