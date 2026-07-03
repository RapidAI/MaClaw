package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"html"
	"math"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/hub/internal/auth"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type KnowledgeShareUserView struct {
	KnowledgeID     string         `json:"knowledge_id"`
	TenantID        string         `json:"tenant_id"`
	OwnerUserID     string         `json:"owner_user_id,omitempty"`
	OwnerUserEmail  string         `json:"owner_user_email,omitempty"`
	Title           string         `json:"title"`
	Description     string         `json:"description"`
	VisibilityScope string         `json:"visibility_scope"`
	VisibilityUsers []string       `json:"visibility_users,omitempty"`
	SourceSummary   map[string]any `json:"source_summary,omitempty"`
	ShareURL        string         `json:"share_url"`
	HubID           string         `json:"hub_id,omitempty"`
	StorageRef      string         `json:"storage_ref,omitempty"`
	PackageURL      string         `json:"package_url,omitempty"`
	Status          string         `json:"status"`
	ViewCount       int64          `json:"view_count"`
	ImportCount     int64          `json:"import_count"`
	CreatedAt       string         `json:"created_at"`
	UpdatedAt       string         `json:"updated_at"`
	PublishedAt     string         `json:"published_at"`
	ExpiresAt       string         `json:"expires_at,omitempty"`
	AgentImport     string         `json:"agent_import"`
}

type knowledgeShareMutationRequest struct {
	KnowledgeID     string          `json:"knowledge_id"`
	Title           string          `json:"title"`
	Description     string          `json:"description"`
	VisibilityScope string          `json:"visibility_scope"`
	VisibilityUsers []string        `json:"visibility_users"`
	SourceSummary   map[string]any  `json:"source_summary"`
	HubID           string          `json:"hub_id"`
	StorageRef      string          `json:"storage_ref"`
	ExpiresAt       string          `json:"expires_at"`
	TTL             string          `json:"ttl"`
	PackageJSON     json.RawMessage `json:"package_json"`
	Package         json.RawMessage `json:"package"`
}

const knowledgeShareRequestMaxBytes = 52 << 20

func ListMyKnowledgeSharesHandler(repo store.KnowledgeShareRepository, identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		if repo == nil {
			writeError(w, http.StatusNotImplemented, "KNOWLEDGE_SHARES_UNAVAILABLE", "Knowledge shares are unavailable")
			return
		}
		limit, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("limit")))
		if limit <= 0 || limit > 50 {
			limit = 50
		}
		offset, _ := strconv.Atoi(strings.TrimSpace(r.URL.Query().Get("offset")))
		if offset < 0 {
			offset = 0
		}
		items, total, err := repo.List(r.Context(), store.KnowledgeShareFilter{
			TenantID:       principal.TenantID,
			TenantScoped:   true,
			OwnerUserID:    principal.UserID,
			OwnerUserEmail: principal.Email,
			Sort:           strings.TrimSpace(r.URL.Query().Get("sort")),
			Offset:         offset,
			Limit:          limit,
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_KNOWLEDGE_SHARES_FAILED", err.Error())
			return
		}
		views := make([]KnowledgeShareUserView, 0, len(items))
		for _, item := range items {
			if item != nil {
				views = append(views, knowledgeShareUserView(r, item, true))
			}
		}
		writeJSON(w, http.StatusOK, map[string]any{"items": views, "total": total, "offset": offset, "limit": limit})
	}
}

func CreateKnowledgeShareHandler(repo store.KnowledgeShareRepository, identity *auth.IdentityService, packageDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		if repo == nil {
			writeError(w, http.StatusNotImplemented, "KNOWLEDGE_SHARES_UNAVAILABLE", "Knowledge shares are unavailable")
			return
		}
		var req knowledgeShareMutationRequest
		if err := decodeKnowledgeShareMutationRequest(w, r, &req); err != nil {
			writeKnowledgeShareDecodeError(w, err)
			return
		}
		item, ok := knowledgeShareFromMutationRequest(w, r, principal, req, "")
		if !ok {
			return
		}
		if raw := knowledgeSharePackagePayload(req); len(raw) > 0 {
			storageRef, err := saveKnowledgeSharePackage(packageDir, item.KnowledgeID, raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "PACKAGE_SAVE_FAILED", err.Error())
				return
			}
			item.StorageRef = storageRef
		}
		if err := repo.Create(r.Context(), item); err != nil {
			writeError(w, http.StatusInternalServerError, "CREATE_KNOWLEDGE_SHARE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, knowledgeShareUserView(r, item, true))
	}
}

func UpdateMyKnowledgeShareHandler(repo store.KnowledgeShareRepository, identity *auth.IdentityService, packageDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		if repo == nil {
			writeError(w, http.StatusNotImplemented, "KNOWLEDGE_SHARES_UNAVAILABLE", "Knowledge shares are unavailable")
			return
		}
		knowledgeID := strings.TrimSpace(r.PathValue("knowledgeID"))
		existing, ok := requireOwnedKnowledgeShare(w, r, repo, principal, knowledgeID)
		if !ok {
			return
		}
		var req knowledgeShareMutationRequest
		if err := decodeKnowledgeShareMutationRequest(w, r, &req); err != nil {
			writeKnowledgeShareDecodeError(w, err)
			return
		}
		if strings.TrimSpace(req.Title) == "" {
			req.Title = existing.Title
		}
		if strings.TrimSpace(req.Description) == "" {
			req.Description = existing.Description
		}
		if strings.TrimSpace(req.VisibilityScope) == "" {
			req.VisibilityScope = existing.VisibilityScope
		}
		if req.VisibilityUsers == nil {
			_ = json.Unmarshal([]byte(existing.VisibilityUsersJSON), &req.VisibilityUsers)
		}
		if req.SourceSummary == nil {
			_ = json.Unmarshal([]byte(existing.SourceSummaryJSON), &req.SourceSummary)
		}
		if strings.TrimSpace(req.HubID) == "" {
			req.HubID = existing.HubID
		}
		if strings.TrimSpace(req.StorageRef) == "" {
			req.StorageRef = existing.StorageRef
		}
		if strings.TrimSpace(req.ExpiresAt) == "" && strings.TrimSpace(req.TTL) == "" {
			if existing.ExpiresAt != nil && !existing.ExpiresAt.IsZero() {
				req.ExpiresAt = existing.ExpiresAt.Format(time.RFC3339)
			} else {
				req.TTL = "permanent"
			}
		}
		req.KnowledgeID = knowledgeID
		item, ok := knowledgeShareFromMutationRequest(w, r, principal, req, existing.ShareURL)
		if !ok {
			return
		}
		if raw := knowledgeSharePackagePayload(req); len(raw) > 0 {
			storageRef, err := saveKnowledgeSharePackage(packageDir, item.KnowledgeID, raw)
			if err != nil {
				writeError(w, http.StatusBadRequest, "PACKAGE_SAVE_FAILED", err.Error())
				return
			}
			item.StorageRef = storageRef
		}
		item.CreatedAt = existing.CreatedAt
		item.PublishedAt = existing.PublishedAt
		item.ViewCount = existing.ViewCount
		item.ImportCount = existing.ImportCount
		if err := repo.UpdateOwner(r.Context(), item); err != nil {
			writeError(w, http.StatusInternalServerError, "UPDATE_KNOWLEDGE_SHARE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, knowledgeShareUserView(r, item, true))
	}
}

func decodeKnowledgeShareMutationRequest(w http.ResponseWriter, r *http.Request, req *knowledgeShareMutationRequest) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, knowledgeShareRequestMaxBytes)).Decode(req)
}

func writeKnowledgeShareDecodeError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		writeError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "Knowledge share request body is too large")
		return
	}
	writeError(w, http.StatusBadRequest, "INVALID_JSON", err.Error())
}

func DeleteMyKnowledgeShareHandler(repo store.KnowledgeShareRepository, identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		principal, ok := requireKnowledgeShareViewer(w, r, identity)
		if !ok {
			return
		}
		if repo == nil {
			writeError(w, http.StatusNotImplemented, "KNOWLEDGE_SHARES_UNAVAILABLE", "Knowledge shares are unavailable")
			return
		}
		knowledgeID := strings.TrimSpace(r.PathValue("knowledgeID"))
		if _, ok := requireOwnedKnowledgeShare(w, r, repo, principal, knowledgeID); !ok {
			return
		}
		if err := repo.DeleteOwner(r.Context(), knowledgeID, principal.TenantID, principal.UserID, time.Now().UTC()); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_KNOWLEDGE_SHARE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "knowledge_id": knowledgeID, "status": "deleted"})
	}
}

func GetKnowledgeSharePublicHandler(repo store.KnowledgeShareRepository, identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, principal, ok := readVisibleKnowledgeShare(w, r, repo, identity)
		if !ok {
			return
		}
		if repo != nil {
			importDelta := int64(0)
			if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("intent")), "import") {
				importDelta = 1
			}
			_ = repo.IncrementCounters(r.Context(), item.KnowledgeID, 1, importDelta, time.Now().UTC())
		}
		owner := principal != nil && principal.UserID == item.OwnerUserID && store.NormalizeTenantID(principal.TenantID) == store.NormalizeTenantID(item.TenantID)
		importIntent := strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("intent")), "import")
		writeJSON(w, http.StatusOK, knowledgeShareUserView(r, item, owner || importIntent))
	}
}

func DownloadKnowledgeSharePackageHandler(repo store.KnowledgeShareRepository, identity *auth.IdentityService, packageDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, _, ok := readVisibleKnowledgeShare(w, r, repo, identity)
		if !ok {
			return
		}
		path, ok := knowledgeSharePackagePath(packageDir, item.StorageRef)
		if !ok {
			writeError(w, http.StatusNotFound, "KNOWLEDGE_PACKAGE_NOT_FOUND", "Knowledge package is not stored on this Hub")
			return
		}
		if repo != nil {
			_ = repo.IncrementCounters(r.Context(), item.KnowledgeID, 0, 1, time.Now().UTC())
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\""+item.KnowledgeID+".json\"")
		http.ServeFile(w, r, path)
	}
}

func KnowledgeSharePublicPageHandler(repo store.KnowledgeShareRepository, identity *auth.IdentityService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		item, _, ok := readVisibleKnowledgeShare(w, r, repo, identity)
		if !ok {
			return
		}
		if repo != nil {
			_ = repo.IncrementCounters(r.Context(), item.KnowledgeID, 1, 0, time.Now().UTC())
		}
		view := knowledgeShareUserView(r, item, false)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write([]byte(renderKnowledgeSharePublicHTML(view)))
	}
}

func requireKnowledgeShareViewer(w http.ResponseWriter, r *http.Request, identity *auth.IdentityService) (*auth.ViewerPrincipal, bool) {
	if identity == nil {
		writeError(w, http.StatusNotImplemented, "IDENTITY_UNAVAILABLE", "Viewer identity is unavailable")
		return nil, false
	}
	principal, err := authenticateViewerRequest(r, identity)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "Viewer authentication failed")
		return nil, false
	}
	return principal, true
}

func optionalKnowledgeShareViewer(r *http.Request, identity *auth.IdentityService) *auth.ViewerPrincipal {
	if identity == nil {
		return nil
	}
	principal, err := authenticateViewerRequest(r, identity)
	if err != nil {
		return nil
	}
	return principal
}

func requireOwnedKnowledgeShare(w http.ResponseWriter, r *http.Request, repo store.KnowledgeShareRepository, principal *auth.ViewerPrincipal, knowledgeID string) (*store.KnowledgeShare, bool) {
	if strings.TrimSpace(knowledgeID) == "" {
		writeError(w, http.StatusBadRequest, "KNOWLEDGE_ID_REQUIRED", "Knowledge ID is required")
		return nil, false
	}
	item, err := repo.Get(r.Context(), knowledgeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "GET_KNOWLEDGE_SHARE_FAILED", err.Error())
		return nil, false
	}
	if item == nil || strings.EqualFold(item.Status, "deleted") {
		writeError(w, http.StatusNotFound, "KNOWLEDGE_SHARE_NOT_FOUND", "Knowledge share not found")
		return nil, false
	}
	if knowledgeShareExpired(item, time.Now()) {
		writeError(w, http.StatusNotFound, "KNOWLEDGE_SHARE_EXPIRED", "Knowledge share has expired")
		return nil, false
	}
	if principal == nil || item.OwnerUserID != principal.UserID || store.NormalizeTenantID(item.TenantID) != store.NormalizeTenantID(principal.TenantID) {
		writeError(w, http.StatusForbidden, "KNOWLEDGE_SHARE_FORBIDDEN", "Knowledge share is not owned by the current user")
		return nil, false
	}
	return item, true
}

func readVisibleKnowledgeShare(w http.ResponseWriter, r *http.Request, repo store.KnowledgeShareRepository, identity *auth.IdentityService) (*store.KnowledgeShare, *auth.ViewerPrincipal, bool) {
	if repo == nil {
		writeError(w, http.StatusNotImplemented, "KNOWLEDGE_SHARES_UNAVAILABLE", "Knowledge shares are unavailable")
		return nil, nil, false
	}
	knowledgeID := strings.TrimSpace(r.PathValue("knowledgeID"))
	if knowledgeID == "" {
		writeError(w, http.StatusBadRequest, "KNOWLEDGE_ID_REQUIRED", "Knowledge ID is required")
		return nil, nil, false
	}
	item, err := repo.Get(r.Context(), knowledgeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "GET_KNOWLEDGE_SHARE_FAILED", err.Error())
		return nil, nil, false
	}
	if item == nil || strings.EqualFold(item.Status, "deleted") {
		writeError(w, http.StatusNotFound, "KNOWLEDGE_SHARE_NOT_FOUND", "Knowledge share not found")
		return nil, nil, false
	}
	if knowledgeShareExpired(item, time.Now()) {
		writeError(w, http.StatusNotFound, "KNOWLEDGE_SHARE_EXPIRED", "Knowledge share has expired")
		return nil, nil, false
	}
	principal := optionalKnowledgeShareViewer(r, identity)
	if !knowledgeShareVisibleToPrincipal(item, principal) {
		writeError(w, http.StatusForbidden, "KNOWLEDGE_SHARE_FORBIDDEN", "Knowledge share is not visible to the current viewer")
		return nil, principal, false
	}
	return item, principal, true
}

func knowledgeShareFromMutationRequest(w http.ResponseWriter, r *http.Request, principal *auth.ViewerPrincipal, req knowledgeShareMutationRequest, existingShareURL string) (*store.KnowledgeShare, bool) {
	description := strings.TrimSpace(req.Description)
	if description == "" {
		writeError(w, http.StatusBadRequest, "DESCRIPTION_REQUIRED", "Knowledge description is required")
		return nil, false
	}
	scope := normalizeKnowledgeVisibilityScope(req.VisibilityScope)
	users := compactKnowledgeShareStrings(req.VisibilityUsers)
	if scope == "users" && len(users) == 0 {
		writeError(w, http.StatusBadRequest, "VISIBILITY_USERS_REQUIRED", "Visibility users are required when visibility_scope is users")
		return nil, false
	}
	usersJSON, _ := json.Marshal(users)
	summaryJSON, _ := json.Marshal(sanitizeKnowledgeShareSummary(req.SourceSummary))
	now := time.Now().UTC()
	expiresAt, ok := knowledgeShareExpiryFromRequest(w, req, now)
	if !ok {
		return nil, false
	}
	knowledgeID := strings.TrimSpace(req.KnowledgeID)
	if knowledgeID == "" {
		knowledgeID = newKnowledgeShareID()
	}
	shareURL := strings.TrimSpace(existingShareURL)
	if shareURL == "" {
		shareURL = absoluteKnowledgeShareURL(r, knowledgeID)
	}
	return &store.KnowledgeShare{
		KnowledgeID:         knowledgeID,
		TenantID:            principal.TenantID,
		OwnerUserID:         principal.UserID,
		OwnerUserEmail:      principal.Email,
		Title:               strings.TrimSpace(req.Title),
		Description:         description,
		VisibilityScope:     scope,
		VisibilityUsersJSON: string(usersJSON),
		SourceSummaryJSON:   string(summaryJSON),
		ShareURL:            shareURL,
		HubID:               strings.TrimSpace(req.HubID),
		StorageRef:          strings.TrimSpace(req.StorageRef),
		Status:              "active",
		CreatedAt:           now,
		UpdatedAt:           now,
		PublishedAt:         now,
		ExpiresAt:           expiresAt,
	}, true
}

func knowledgeShareUserView(r *http.Request, item *store.KnowledgeShare, includeOwner bool) KnowledgeShareUserView {
	visibilityUsers := []string{}
	if strings.TrimSpace(item.VisibilityUsersJSON) != "" {
		_ = json.Unmarshal([]byte(item.VisibilityUsersJSON), &visibilityUsers)
	}
	sourceSummary := map[string]any{}
	if strings.TrimSpace(item.SourceSummaryJSON) != "" {
		_ = json.Unmarshal([]byte(item.SourceSummaryJSON), &sourceSummary)
	}
	shareURL := strings.TrimSpace(item.ShareURL)
	if shareURL == "" && r != nil {
		shareURL = absoluteKnowledgeShareURL(r, item.KnowledgeID)
	}
	view := KnowledgeShareUserView{
		KnowledgeID:     item.KnowledgeID,
		TenantID:        item.TenantID,
		Title:           item.Title,
		Description:     item.Description,
		VisibilityScope: item.VisibilityScope,
		VisibilityUsers: visibilityUsers,
		SourceSummary:   sourceSummary,
		ShareURL:        shareURL,
		HubID:           item.HubID,
		PackageURL:      "/api/knowledge/shares/" + url.PathEscape(item.KnowledgeID) + "/package",
		Status:          item.Status,
		ViewCount:       item.ViewCount,
		ImportCount:     item.ImportCount,
		CreatedAt:       item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       item.UpdatedAt.Format(time.RFC3339),
		PublishedAt:     item.PublishedAt.Format(time.RFC3339),
		AgentImport:     "/api/knowledge/shares/" + url.PathEscape(item.KnowledgeID) + "?intent=import",
	}
	if item.ExpiresAt != nil && !item.ExpiresAt.IsZero() {
		view.ExpiresAt = item.ExpiresAt.UTC().Format(time.RFC3339)
	}
	if includeOwner {
		view.OwnerUserID = item.OwnerUserID
		view.OwnerUserEmail = item.OwnerUserEmail
		view.StorageRef = item.StorageRef
	}
	return view
}

func knowledgeSharePackagePayload(req knowledgeShareMutationRequest) json.RawMessage {
	if len(req.PackageJSON) > 0 {
		return req.PackageJSON
	}
	return req.Package
}

func saveKnowledgeSharePackage(packageDir, knowledgeID string, raw json.RawMessage) (string, error) {
	if !json.Valid(raw) {
		return "", knowledgeSharePackageError("package_json must be valid JSON")
	}
	if len(raw) > 50<<20 {
		return "", knowledgeSharePackageError("knowledge package must be 50MB or smaller")
	}
	packageDir = strings.TrimSpace(packageDir)
	if packageDir == "" {
		return "", knowledgeSharePackageError("knowledge package storage is unavailable")
	}
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		return "", err
	}
	filename := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_").Replace(strings.TrimSpace(knowledgeID)) + ".json"
	path := filepath.Join(packageDir, filename)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		return "", err
	}
	return "local:knowledge-packages/" + filename, nil
}

func knowledgeSharePackagePath(packageDir, storageRef string) (string, bool) {
	storageRef = strings.TrimSpace(storageRef)
	if !strings.HasPrefix(storageRef, "local:knowledge-packages/") || strings.TrimSpace(packageDir) == "" {
		return "", false
	}
	name := strings.TrimPrefix(storageRef, "local:knowledge-packages/")
	if name == "" || strings.ContainsAny(name, `/\`) {
		return "", false
	}
	path := filepath.Join(packageDir, name)
	if _, err := os.Stat(path); err != nil {
		return "", false
	}
	return path, true
}

type knowledgeSharePackageError string

func (e knowledgeSharePackageError) Error() string { return string(e) }

func knowledgeShareVisibleToPrincipal(item *store.KnowledgeShare, principal *auth.ViewerPrincipal) bool {
	if knowledgeShareExpired(item, time.Now()) {
		return false
	}
	scope := normalizeKnowledgeVisibilityScope(item.VisibilityScope)
	if scope == "public" || scope == "hub" {
		return true
	}
	if principal == nil {
		return false
	}
	if item.OwnerUserID == principal.UserID && store.NormalizeTenantID(item.TenantID) == store.NormalizeTenantID(principal.TenantID) {
		return true
	}
	if scope == "tenant" {
		return store.NormalizeTenantID(item.TenantID) == store.NormalizeTenantID(principal.TenantID)
	}
	if scope == "users" {
		users := []string{}
		_ = json.Unmarshal([]byte(item.VisibilityUsersJSON), &users)
		for _, user := range users {
			if strings.EqualFold(strings.TrimSpace(user), principal.Email) || strings.TrimSpace(user) == principal.UserID {
				return true
			}
		}
	}
	return false
}

func knowledgeShareExpired(item *store.KnowledgeShare, now time.Time) bool {
	if item == nil || item.ExpiresAt == nil || item.ExpiresAt.IsZero() {
		return false
	}
	return !item.ExpiresAt.After(now.UTC())
}

func knowledgeShareExpiryFromRequest(w http.ResponseWriter, req knowledgeShareMutationRequest, now time.Time) (*time.Time, bool) {
	if raw := strings.TrimSpace(req.ExpiresAt); raw != "" {
		parsed, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_EXPIRES_AT", "expires_at must be RFC3339")
			return nil, false
		}
		parsed = parsed.UTC()
		if !parsed.After(now) {
			writeError(w, http.StatusBadRequest, "INVALID_EXPIRES_AT", "expires_at must be in the future")
			return nil, false
		}
		return &parsed, true
	}
	switch strings.ToLower(strings.TrimSpace(req.TTL)) {
	case "", "7d", "7day", "7days", "week":
		expiresAt := now.AddDate(0, 0, 7)
		return &expiresAt, true
	case "1m", "month", "30d", "30days":
		expiresAt := now.AddDate(0, 1, 0)
		return &expiresAt, true
	case "1y", "year", "365d", "365days":
		expiresAt := now.AddDate(1, 0, 0)
		return &expiresAt, true
	case "permanent", "forever", "never", "none":
		return nil, true
	default:
		writeError(w, http.StatusBadRequest, "INVALID_TTL", "ttl must be one of 7d, month, year, permanent")
		return nil, false
	}
}

func normalizeKnowledgeVisibilityScope(scope string) string {
	switch strings.ToLower(strings.TrimSpace(scope)) {
	case "public", "global", "world":
		return "public"
	case "tenant", "tenant_public":
		return "tenant"
	case "hub", "hub_public":
		return "hub"
	case "users", "user_list":
		return "users"
	default:
		return "private"
	}
}

func compactKnowledgeShareStrings(values []string) []string {
	seen := map[string]struct{}{}
	out := []string{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sanitizeKnowledgeShareSummary(summary map[string]any) map[string]any {
	out := map[string]any{}
	for key, value := range summary {
		lowered := strings.ToLower(strings.TrimSpace(key))
		if lowered == "" || (!knowledgeShareSummarySafeContentMetric(lowered) && (strings.Contains(lowered, "body") || strings.Contains(lowered, "content") || strings.Contains(lowered, "fact") || strings.Contains(lowered, "card") || strings.Contains(lowered, "payload"))) {
			continue
		}
		switch v := value.(type) {
		case string:
			out[key] = truncateKnowledgeShareString(v, 500)
		case []string:
			out[key] = sanitizeKnowledgeShareSummaryStringList(v)
		case []any:
			out[key] = sanitizeKnowledgeShareSummaryAnyList(v)
		case float64, bool, int, int64:
			out[key] = v
		default:
			raw, _ := json.Marshal(v)
			out[key] = truncateKnowledgeShareString(string(raw), 500)
		}
	}
	return out
}

func knowledgeShareSummarySafeContentMetric(key string) bool {
	switch key {
	case "content_sources", "content_source_count":
		return true
	default:
		return false
	}
}

func sanitizeKnowledgeShareSummaryStringList(values []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = truncateKnowledgeShareString(value, 500)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func sanitizeKnowledgeShareSummaryAnyList(values []any) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if text, ok := value.(string); ok {
			out = append(out, text)
		}
	}
	return sanitizeKnowledgeShareSummaryStringList(out)
}

func truncateKnowledgeShareString(value string, limit int) string {
	runes := []rune(strings.TrimSpace(value))
	if len(runes) <= limit {
		return string(runes)
	}
	return string(runes[:limit]) + "..."
}

func absoluteKnowledgeShareURL(r *http.Request, knowledgeID string) string {
	scheme := "http"
	if r != nil {
		if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
			scheme = strings.Split(proto, ",")[0]
		} else if r.TLS != nil {
			scheme = "https"
		}
	}
	host := ""
	if r != nil {
		host = strings.TrimSpace(r.Host)
		if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-Host")); forwarded != "" {
			host = strings.Split(forwarded, ",")[0]
		}
	}
	if host == "" {
		return "/hub/knowledge/shares/" + url.PathEscape(knowledgeID)
	}
	return scheme + "://" + host + "/hub/knowledge/shares/" + url.PathEscape(knowledgeID)
}

func newKnowledgeShareID() string {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err == nil {
		return "kn_" + hex.EncodeToString(buf[:])
	}
	return "kn_" + strconv.FormatInt(time.Now().UnixNano(), 36)
}

func renderKnowledgeSharePublicHTML(view KnowledgeShareUserView) string {
	rawTitle := firstNonEmptyKnowledgeShare(view.Title)
	title := html.EscapeString(rawTitle)
	titleHeading := ""
	if strings.TrimSpace(view.Title) != "" {
		titleHeading = `<h1 class="hero-title">` + html.EscapeString(strings.TrimSpace(view.Title)) + `</h1>`
	}
	description := html.EscapeString(view.Description)
	jsonURL := html.EscapeString(knowledgeShareSafeHref(view.AgentImport))
	shareURL := html.EscapeString(knowledgeShareSafeHref(view.ShareURL))
	expires := html.EscapeString(firstNonEmptyKnowledgeShare(view.ExpiresAt, "Permanent"))
	expiresZH := html.EscapeString(firstNonEmptyKnowledgeShare(view.ExpiresAt, "永久"))
	sourceCount := knowledgeShareSummaryNumber(view.SourceSummary, "source_count")
	contentSources := knowledgeShareSummaryNumber(view.SourceSummary, "content_sources")
	if contentSources == 0 {
		contentSources = sourceCount
	}
	sourceCountText := strconv.FormatInt(sourceCount, 10)
	contentSourcesText := strconv.FormatInt(contentSources, 10)
	viewCountText := strconv.FormatInt(nonNegativeInt64(view.ViewCount), 10)
	importCountText := strconv.FormatInt(nonNegativeInt64(view.ImportCount), 10)
	page := `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta name="description" content="{{DESCRIPTION}}">
<meta property="og:type" content="article">
<meta property="og:title" content="{{TITLE}}">
<meta property="og:description" content="{{DESCRIPTION}}">
<meta property="og:url" content="{{SHARE_URL}}">
<meta name="twitter:card" content="summary">
<meta name="twitter:title" content="{{TITLE}}">
<meta name="twitter:description" content="{{DESCRIPTION}}">
<link rel="alternate" type="application/json" href="{{JSON_URL}}">
<title>{{TITLE}} - MaClaw Knowledge Share</title>
<style>
:root{--page:#eef3f7;--panel:#fff;--panel-soft:#f7f9fb;--ink:#172234;--muted:#5f6e82;--line:#dbe4ed;--line-strong:#c5d1de;--brand:#385875;--brand-strong:#263f59;--brand-soft:#e8f0f7;--success:#2f7a55}
*{box-sizing:border-box}body{margin:0;font-family:Inter,ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,"Segoe UI",sans-serif;background:radial-gradient(circle at 18% 10%,rgba(104,135,163,.16),transparent 28%),linear-gradient(135deg,#f8fafc 0%,var(--page) 58%,#e6edf4 100%);color:var(--ink)}a{color:var(--brand-strong)}.shell{width:min(1120px,calc(100% - 32px));margin:0 auto;padding:44px 0 56px}.topbar{display:flex;align-items:center;justify-content:space-between;gap:14px;margin-bottom:18px}.brand{display:flex;align-items:center;gap:10px;color:var(--brand-strong);font-weight:900;letter-spacing:.02em}.mark{width:34px;height:34px;border-radius:10px;display:grid;place-items:center;background:var(--brand-strong);color:#fff;font-weight:950}.lang-toggle{display:inline-flex;gap:4px;padding:4px;border:1px solid var(--line);border-radius:999px;background:rgba(255,255,255,.78)}.lang-toggle button{border:0;border-radius:999px;background:transparent;color:var(--muted);cursor:pointer;font-weight:850;padding:7px 11px}.lang-toggle button[aria-pressed=true]{background:var(--brand);color:#fff}.hero{overflow:hidden;border:1px solid var(--line);border-radius:20px;background:rgba(255,255,255,.92);box-shadow:0 24px 70px rgba(38,61,84,.14)}.hero-grid{display:grid;grid-template-columns:minmax(0,1.55fr) minmax(320px,.9fr);gap:0}.main{padding:34px}.side{display:grid;align-content:start;gap:14px;border-left:1px solid var(--line);background:linear-gradient(180deg,#fbfcfe 0%,#f4f7fa 100%);padding:28px}.kicker{display:inline-flex;align-items:center;gap:8px;width:max-content;padding:5px 9px;border:1px solid var(--line);border-radius:999px;background:var(--panel-soft);color:var(--brand);font-size:12px;font-weight:900;letter-spacing:.08em;text-transform:uppercase}.kicker::before{content:"";width:7px;height:7px;border-radius:99px;background:var(--success)}.hero-title{margin:16px 0 12px;font-size:clamp(30px,4vw,48px);line-height:1.08;letter-spacing:-.03em;color:var(--ink);overflow-wrap:anywhere}.desc{max-width:72ch;margin:16px 0 0;color:#415166;font-size:17px;line-height:1.78;white-space:pre-wrap}.stats{display:grid;grid-template-columns:repeat(4,minmax(110px,1fr));gap:10px;margin-top:26px}.stat{padding:12px;border:1px solid var(--line);border-radius:14px;background:var(--panel-soft)}.stat strong{display:block;color:var(--ink);font-size:22px;line-height:1.05}.stat span{display:block;margin-top:5px;color:var(--muted);font-size:12px;font-weight:850}.actions{display:flex;flex-wrap:wrap;gap:10px;margin-top:28px}.button{display:inline-flex;min-height:44px;align-items:center;justify-content:center;gap:8px;padding:11px 15px;border-radius:10px;border:1px solid var(--line-strong);background:#fff;text-decoration:none;font-weight:900;color:var(--ink);cursor:pointer}.button.primary{border-color:var(--brand);background:var(--brand);color:#fff}.button:hover{border-color:var(--brand);box-shadow:0 0 0 3px var(--brand-soft)}.section-title{margin:0;color:var(--ink);font-size:14px;font-weight:950}.meta{display:grid;gap:10px}.meta-row{display:grid;gap:4px;padding:12px;border:1px solid var(--line);border-radius:12px;background:#fff}.meta-row span{color:var(--muted);font-size:12px;font-weight:850}.meta-row code,.meta-row strong{min-width:0;color:var(--ink);font-size:13px;overflow-wrap:anywhere}.linkbox{display:grid;gap:8px;padding:14px;border:1px solid var(--line);border-radius:14px;background:#fff}.linkbox a{overflow-wrap:anywhere;font-size:13px}.note{margin:0;color:var(--muted);font-size:13px;line-height:1.55}.copied{color:var(--success);font-weight:850}.footer{margin-top:18px;color:var(--muted);font-size:12px;text-align:center}.zh [data-lang=en],.en [data-lang=zh]{display:none}@media(max-width:840px){.shell{padding-top:24px}.hero-grid{grid-template-columns:1fr}.side{border-left:0;border-top:1px solid var(--line)}.main{padding:24px}.topbar{align-items:flex-start}.brand{font-size:14px}.stats{grid-template-columns:repeat(2,minmax(0,1fr))}}@media(prefers-reduced-motion:no-preference){.button,.lang-toggle button{transition:border-color .18s ease,box-shadow .18s ease,background .18s ease,color .18s ease}}
</style>
</head>
<body class="zh">
<main class="shell">
<div class="topbar">
<div class="brand"><div class="mark">M</div><span>MaClaw Hub</span></div>
<div class="lang-toggle" aria-label="Language / 语言"><button type="button" data-set-lang="zh" aria-pressed="true">中文</button><button type="button" data-set-lang="en" aria-pressed="false">English</button></div>
</div>
<article class="hero">
<div class="hero-grid">
<section class="main">
<div class="kicker" data-lang="zh">知识分享</div><div class="kicker" data-lang="en">Knowledge Share</div>
{{TITLE_HEADING}}
<p class="desc">{{DESCRIPTION}}</p>
<div class="stats" aria-label="Knowledge share statistics">
<div class="stat"><strong>{{SOURCE_COUNT}}</strong><span data-lang="zh">来源条目</span><span data-lang="en">Sources</span></div>
<div class="stat"><strong>{{CONTENT_SOURCES}}</strong><span data-lang="zh">可导入内容</span><span data-lang="en">Importable</span></div>
<div class="stat"><strong>{{VIEW_COUNT}}</strong><span data-lang="zh">浏览次数</span><span data-lang="en">Views</span></div>
<div class="stat"><strong>{{IMPORT_COUNT}}</strong><span data-lang="zh">导入次数</span><span data-lang="en">Imports</span></div>
</div>
<div class="actions">
<a class="button primary" href="{{JSON_URL}}"><span data-lang="zh">Agent 导入 JSON</span><span data-lang="en">Agent import JSON</span></a>
<button class="button" type="button" data-copy="{{SHARE_URL}}"><span data-lang="zh">复制分享链接</span><span data-lang="en">Copy share link</span></button>
<a class="button" href="/hub/knowledge/shares/mine" data-manage-shares><span data-lang="zh">管理我的分享</span><span data-lang="en">Manage my shares</span></a>
</div>
</section>
<aside class="side">
<h2 class="section-title" data-lang="zh">分享信息</h2><h2 class="section-title" data-lang="en">Share Details</h2>
<div class="meta">
<div class="meta-row"><span data-lang="zh">知识 ID</span><span data-lang="en">Knowledge ID</span><code>{{KNOWLEDGE_ID}}</code></div>
<div class="meta-row"><span data-lang="zh">可见范围</span><span data-lang="en">Visibility</span><strong>{{VISIBILITY}}</strong></div>
<div class="meta-row"><span data-lang="zh">发布时间</span><span data-lang="en">Published</span><strong>{{PUBLISHED}}</strong></div>
<div class="meta-row"><span data-lang="zh">有效期</span><span data-lang="en">Expiry</span><strong data-lang="zh">{{EXPIRES_ZH}}</strong><strong data-lang="en">{{EXPIRES}}</strong></div>
</div>
<div class="linkbox"><strong data-lang="zh">人可阅读，Agent 可导入</strong><strong data-lang="en">Readable by people, importable by agents</strong><a href="{{SHARE_URL}}">{{SHARE_URL}}</a><p class="note" data-lang="zh">把此链接发给其他用户即可查看说明；Agent 也可以根据该链接或知识 ID 导入数据。</p><p class="note" data-lang="en">Share this link for a human-readable page; agents can also import the knowledge by link or ID.</p><span class="copied" hidden data-copied aria-live="polite" data-lang="zh">已复制</span><span class="copied" hidden data-copied aria-live="polite" data-lang="en">Copied</span></div>
</aside>
</div>
</article>
<div class="footer">MaClaw Knowledge Hub</div>
</main>
<script>
(() => {
  const root = document.body;
  const buttons = Array.from(document.querySelectorAll('[data-set-lang]'));
  const apply = (lang) => {
    const next = lang === 'en' ? 'en' : 'zh';
    root.classList.toggle('en', next === 'en');
    root.classList.toggle('zh', next !== 'en');
    document.documentElement.lang = next === 'en' ? 'en' : 'zh-CN';
    buttons.forEach((button) => button.setAttribute('aria-pressed', String(button.dataset.setLang === next)));
    try { localStorage.setItem('maclawKnowledgeShareLang', next); } catch (_) {}
  };
  buttons.forEach((button) => button.addEventListener('click', () => apply(button.dataset.setLang)));
  try {
    const saved = localStorage.getItem('maclawKnowledgeShareLang');
    apply(saved || ((navigator.language || '').toLowerCase().startsWith('zh') ? 'zh' : 'en'));
  } catch (_) { apply('zh'); }
  const copyText = async (value) => {
    try { await navigator.clipboard.writeText(value); return true; } catch (_) {}
    const area = document.createElement('textarea');
    area.value = value;
    area.setAttribute('readonly', '');
    area.style.position = 'fixed';
    area.style.opacity = '0';
    document.body.appendChild(area);
    area.select();
    try { return document.execCommand('copy'); } catch (_) { return false; } finally { area.remove(); }
  };
  document.querySelectorAll('[data-copy]').forEach((button) => button.addEventListener('click', async () => {
    await copyText(button.dataset.copy || '');
    document.querySelectorAll('[data-copied]').forEach((node) => { node.hidden = false; window.setTimeout(() => { node.hidden = true; }, 1600); });
  }));
  const tokenSuffix = (() => {
    const read = (params) => params.get('token') || params.get('viewer_token') || params.get('access_token') || '';
    const hash = String(window.location.hash || '').replace(/^#/, '');
    const hashToken = read(new URLSearchParams(hash));
    if (hashToken) return '#token=' + encodeURIComponent(hashToken);
    const queryToken = read(new URLSearchParams(window.location.search || ''));
    if (queryToken) return '#token=' + encodeURIComponent(queryToken);
    return '';
  })();
  if (tokenSuffix) {
    document.querySelectorAll('[data-manage-shares]').forEach((node) => {
      node.setAttribute('href', '/hub/knowledge/shares/mine' + tokenSuffix);
    });
  }
})();
</script>
</body>
</html>`
	return strings.NewReplacer(
		"{{TITLE}}", title,
		"{{TITLE_HEADING}}", titleHeading,
		"{{DESCRIPTION}}", description,
		"{{JSON_URL}}", jsonURL,
		"{{SHARE_URL}}", shareURL,
		"{{KNOWLEDGE_ID}}", html.EscapeString(view.KnowledgeID),
		"{{VISIBILITY}}", html.EscapeString(view.VisibilityScope),
		"{{PUBLISHED}}", html.EscapeString(view.PublishedAt),
		"{{EXPIRES}}", expires,
		"{{EXPIRES_ZH}}", expiresZH,
		"{{SOURCE_COUNT}}", sourceCountText,
		"{{CONTENT_SOURCES}}", contentSourcesText,
		"{{VIEW_COUNT}}", viewCountText,
		"{{IMPORT_COUNT}}", importCountText,
	).Replace(page)
}

func knowledgeShareSummaryNumber(summary map[string]any, key string) int64 {
	if summary == nil {
		return 0
	}
	switch value := summary[key].(type) {
	case int:
		return nonNegativeInt64(int64(value))
	case int64:
		return nonNegativeInt64(value)
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
			return 0
		}
		const maxInt64 = int64(1<<63 - 1)
		if value > float64(maxInt64) {
			return maxInt64
		}
		return int64(value)
	case json.Number:
		n, _ := value.Int64()
		return nonNegativeInt64(n)
	default:
		return 0
	}
}

func nonNegativeInt64(value int64) int64 {
	if value < 0 {
		return 0
	}
	return value
}

func knowledgeShareSafeHref(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "#"
	}
	if strings.HasPrefix(trimmed, "/") && !strings.HasPrefix(trimmed, "//") {
		return trimmed
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return "#"
	}
	switch strings.ToLower(parsed.Scheme) {
	case "http", "https":
		if parsed.Host != "" {
			return trimmed
		}
	}
	return "#"
}

func firstNonEmptyKnowledgeShare(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return "Knowledge Share"
}
