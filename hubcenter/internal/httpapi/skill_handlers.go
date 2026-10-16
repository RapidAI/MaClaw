package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skillmarket"
)

const (
	// Align with skill.MaxSkillPackageDownloadBytes so multi-asset packages
	// can be published/accepted (base64 file maps exceed the old 5 MiB cap).
	maxSkillPublishJSONBytes = coreskill.MaxSkillPackageDownloadBytes
	maxSkillSmallJSONBytes   = 4096
)

type SkillHandlers struct {
	store       *skill.SkillStore
	searchSvc   skillSearchRemover
	authSvc     *skillmarket.AuthService
	marketStore *skillmarket.Store
}

// skillSearchRemover is the subset of SearchService needed by SkillHandlers.
type skillSearchRemover interface {
	RemoveSkill(ctx context.Context, id string) error
	ReIndexSkill(ctx context.Context, id string) error
}

func NewSkillHandlers(store *skill.SkillStore, searchSvc skillSearchRemover, authSvc *skillmarket.AuthService, marketStore ...*skillmarket.Store) *SkillHandlers {
	var ms *skillmarket.Store
	if len(marketStore) > 0 {
		ms = marketStore[0]
	}
	return &SkillHandlers{store: store, searchSvc: searchSvc, authSvc: authSvc, marketStore: ms}
}

func skillError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

func decodeSkillJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	if err := decodeLimitedJSON(w, r, dst, limit); err != nil {
		if errors.Is(err, errRequestBodyTooLarge) {
			skillError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return false
		}
		skillError(w, http.StatusBadRequest, "invalid JSON body")
		return false
	}
	return true
}

func (h *SkillHandlers) SearchSkills(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	tagsRaw := r.URL.Query().Get("tags")
	pageStr := r.URL.Query().Get("page")

	var tags []string
	if tagsRaw != "" {
		for _, t := range strings.Split(tagsRaw, ",") {
			t = strings.TrimSpace(t)
			if t != "" {
				tags = append(tags, t)
			}
		}
	}

	page := 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	result := h.store.Search(q, tags, page)
	writeJSON(w, http.StatusOK, result)
}

func (h *SkillHandlers) GetSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		skillError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	s, err := h.store.GetVisible(id)
	if err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *SkillHandlers) DownloadSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		skillError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	s, err := h.store.GetCurrentVisible(id)
	if err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// DownloadBySkillID handles GET /api/v1/skills/by-skill-id/{skill_id}/download.
// Looks up a skill by its publisher.name skill_id (not the internal UUID).
// TODO: support ?version= and ?constraint= query params when multi-version
// storage is implemented (currently returns the single latest version).
func (h *SkillHandlers) DownloadBySkillID(w http.ResponseWriter, r *http.Request) {
	skillID := r.PathValue("skill_id")
	if skillID == "" {
		skillError(w, http.StatusBadRequest, "skill_id is required")
		return
	}
	meta := h.store.FindBySkillID(skillID)
	if meta == nil {
		skillError(w, http.StatusNotFound, "skill_id not found: "+skillID)
		return
	}
	// Return the current public skill (same format as DownloadSkill by UUID).
	// GetCurrentVisible also prevents a legacy alias from resolving to a
	// superseded revision after version groups have been merged.
	s, err := h.store.GetCurrentVisible(meta.ID)
	if err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

// GetSuite returns published Suite metadata. Complete member Skill payloads
// are intentionally exposed only by the purchase-aware download endpoint.
func (h *SkillHandlers) GetSuite(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		skillError(w, http.StatusBadRequest, "suite id is required")
		return
	}
	su, err := h.store.GetSuite(id)
	if err != nil || !su.Visible {
		skillError(w, http.StatusNotFound, "suite not found")
		return
	}
	if su.Version != "" {
		w.Header().Set("X-Suite-Version", su.Version)
	}
	// Details are metadata-only; complete member payloads are gated by the
	// download endpoint (and therefore by Suite purchase for paid packages).
	su.Skills = nil
	writeJSON(w, http.StatusOK, su)
}

// PublishSuiteVersion publishes a new Suite revision (upgrade). The request
// body is a SkillSuiteFull; the path ID is authoritative.
func (h *SkillHandlers) PublishSuiteVersion(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		skillError(w, http.StatusBadRequest, "suite id is required")
		return
	}
	if _, err := h.store.GetSuite(id); err != nil {
		skillError(w, http.StatusNotFound, "suite not found")
		return
	}
	var su skill.SkillSuiteFull
	if err := json.NewDecoder(io.LimitReader(r.Body, maxSkillPublishJSONBytes+1)).Decode(&su); err != nil {
		skillError(w, http.StatusBadRequest, "invalid suite: "+err.Error())
		return
	}
	su.ID = id
	if strings.TrimSpace(su.Version) == "" {
		skillError(w, http.StatusBadRequest, "suite version is required")
		return
	}
	if !validSuiteSemver(su.Version) {
		skillError(w, http.StatusBadRequest, "suite version must be semantic version (e.g. 1.2.3)")
		return
	}
	if err := h.store.PublishSuite(su); err != nil {
		skillError(w, http.StatusBadRequest, err.Error())
		return
	}
	result, _ := h.store.GetSuite(id)
	if h.marketStore != nil && result != nil {
		_ = h.marketStore.RecordSuiteAuditEvent(r.Context(), id, "publish", strings.TrimSpace(r.Header.Get("X-User-ID")), "", suiteMemberIDsForAudit(result))
	}
	writeJSON(w, http.StatusOK, map[string]any{"suite": result, "status": "published"})
}

func validSuiteSemver(v string) bool {
	v = strings.TrimSpace(strings.TrimPrefix(v, "v"))
	core := v
	suffix := ""
	if i := strings.IndexAny(core, "+-"); i >= 0 {
		suffix, core = core[i:], core[:i]
	}
	parts := strings.Split(core, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		if len(p) > 1 && p[0] == '0' {
			return false
		}
		for _, r := range p {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	if suffix != "" {
		if suffix[0] != '-' && suffix[0] != '+' {
			return false
		}
		for _, r := range suffix[1:] {
			if !(r == '.' || r == '-' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z') {
				return false
			}
		}
		if strings.HasSuffix(suffix, ".") || strings.HasSuffix(suffix, "-") || strings.HasSuffix(suffix, "+") {
			return false
		}
	}
	return true
}

// validSuiteSlug mirrors the SkillStore identifier contract so malformed
// Suite/member IDs are rejected at the HTTP boundary with a client error
// instead of surfacing later as an internal persistence failure.
func validSuiteSlug(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	for i, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.') {
			return false
		}
		if i == 0 && (r == '-' || r == '_' || r == '.') {
			return false
		}
	}
	return true
}

// RollbackSuite restores an exact historical Suite revision.
func (h *SkillHandlers) RollbackSuite(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		skillError(w, http.StatusBadRequest, "suite id is required")
		return
	}
	var req struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		skillError(w, http.StatusBadRequest, "invalid request")
		return
	}
	result, err := h.store.RollbackSuite(id, req.Version)
	if err != nil {
		skillError(w, http.StatusBadRequest, err.Error())
		return
	}
	if h.marketStore != nil {
		_ = h.marketStore.RecordSuiteAuditEvent(r.Context(), id, "rollback", strings.TrimSpace(r.Header.Get("X-User-ID")), "", suiteMemberIDsForAudit(result))
	}
	writeJSON(w, http.StatusOK, map[string]any{"suite": result, "status": "rolled_back"})
}

func suiteMemberIDsForAudit(suite *skill.SkillSuiteFull) []string {
	if suite == nil {
		return nil
	}
	ids := make([]string, 0, len(suite.Members))
	for _, member := range suite.Members {
		if id := firstNonEmpty(member.SkillID, member.SkillRef); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func (h *SkillHandlers) ListSuiteVersions(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	su, err := h.store.GetSuite(id)
	if err != nil || !su.Visible {
		skillError(w, http.StatusNotFound, "suite not found")
		return
	}
	versions := make([]skill.SuiteVersionSummary, 0, len(su.VersionHistory)+1)
	versions = append(versions, skill.SuiteVersionSummary{Version: su.Version, SourceRevision: su.SourceRevision, UpdatedAt: su.UpdatedAt})
	versions = append(versions, su.VersionHistory...)
	writeJSON(w, http.StatusOK, map[string]any{"suite_id": id, "current_version": su.Version, "versions": versions})
}

// DownloadSuite is the public download endpoint. The Suite JSON contains all
// member Skill definitions so clients can install them atomically.
func (h *SkillHandlers) DownloadSuite(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	purchaseID := strings.TrimSpace(r.URL.Query().Get("purchase_id"))
	// When a client supplies a purchase ID, enforce that the entitlement is
	// still active. Public/free Suite downloads remain backwards compatible.
	if h.marketStore != nil {
		if purchaseID != "" {
			rec, err := h.marketStore.GetSuitePurchaseByID(r.Context(), purchaseID)
			if err != nil || rec.SuiteID != id {
				skillError(w, http.StatusForbidden, "invalid suite purchase")
				return
			}
			if rec.Status != "active" {
				skillError(w, http.StatusPaymentRequired, "suite purchase is not active")
				return
			}
			if h.authSvc != nil {
				token := extractSessionToken(r)
				if token == "" {
					skillError(w, http.StatusUnauthorized, "session token required")
					return
				}
				sess, authErr := h.authSvc.ValidateSession(r.Context(), token)
				if authErr != nil || (rec.BuyerID != "" && sess.UserID != rec.BuyerID) {
					skillError(w, http.StatusForbidden, "suite purchase ownership required")
					return
				}
			}
		}
	}
	su, err := h.store.GetSuite(id)
	if err != nil || !su.Visible {
		skillError(w, http.StatusNotFound, "suite not found")
		return
	}
	// Paid Suites require an active entitlement. Free Suites retain the public
	// download behaviour used by legacy clients.
	if h.marketStore != nil && su.Price > 0 && purchaseID == "" {
		skillError(w, http.StatusPaymentRequired, "suite purchase is required")
		return
	}
	if su.Version != "" {
		w.Header().Set("X-Suite-Version", su.Version)
	}
	// ETag must represent the complete response, not only the embedded Skill
	// bytes: a Suite version can change metadata/permissions while retaining
	// identical member payloads.
	raw, _ := json.Marshal(su)
	etag := sha256Hex(raw)
	w.Header().Set("ETag", `"`+etag+`"`)
	if strings.TrimSpace(r.Header.Get("If-None-Match")) == `"`+etag+`"` {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("format")), "zip") {
		data, err := suiteZipBytes(su)
		if err != nil {
			skillError(w, http.StatusInternalServerError, err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/zip")
		w.Header().Set("Content-Disposition", `attachment; filename="`+su.ID+`.zip"`)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(data)
		h.recordSuiteDownloadAudit(r, su)
		return
	}
	h.recordSuiteDownloadAudit(r, su)
	writeJSON(w, http.StatusOK, su)
}

func (h *SkillHandlers) recordSuiteDownloadAudit(r *http.Request, su *skill.SkillSuiteFull) {
	if h.store != nil {
		_ = h.store.IncrementSuiteDownloadCount(su.ID)
	}
	if h.marketStore == nil {
		return
	}
	// Prefer the authenticated identity header. Query-string email is retained
	// only for legacy unauthenticated/free downloads and must never override a
	// server-provided actor identity in the audit trail.
	actor := strings.TrimSpace(r.Header.Get("X-User-ID"))
	if actor == "" {
		actor = strings.TrimSpace(r.URL.Query().Get("email"))
	}
	members := make([]string, 0, len(su.Members))
	for _, m := range su.Members {
		if id := firstNonEmpty(m.SkillID, m.SkillRef); id != "" {
			members = append(members, id)
		}
	}
	_ = h.marketStore.RecordSuiteAuditEvent(r.Context(), su.ID, "download", actor, strings.TrimSpace(r.URL.Query().Get("purchase_id")), members)
}

func suiteZipBytes(su *skill.SkillSuiteFull) ([]byte, error) {
	if su == nil {
		return nil, errors.New("suite is nil")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	manifest, _ := json.MarshalIndent(su, "", "  ")
	f, err := zw.Create("suite.json")
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(manifest); err != nil {
		return nil, err
	}
	integrity := map[string]string{"suite.json": sha256Hex(manifest)}
	archiveEntries := map[string]struct{}{strings.ToLower("suite.json"): {}}
	usedRoots := make(map[string]struct{}, len(su.Skills))
	memberRoots := make(map[string]string, len(su.Skills))
	for _, member := range su.Skills {
		root := strings.TrimSpace(member.Name)
		if root == "" {
			root = member.ID
		}
		if root == "" {
			continue
		}
		// Keep archive entry names confined to skills/<member>/ even when a
		// malformed upstream member name contains separators or dot segments.
		root = path.Base(strings.Trim(strings.ReplaceAll(root, "\\", "/"), "/"))
		if root == "" || root == "." || root == ".." {
			continue
		}
		if _, exists := usedRoots[root]; exists {
			fallback := root + "-" + sha256Hex([]byte(member.ID))[:8]
			for n := 2; ; n++ {
				if _, taken := usedRoots[fallback]; !taken {
					break
				}
				fallback = fmt.Sprintf("%s-%d", root, n)
			}
			root = fallback
		}
		usedRoots[root] = struct{}{}
		memberRoots[strings.ToLower(firstNonEmpty(member.ID, member.SkillID, member.Name))] = root
		wroteDefinition := false
		for name, encoded := range member.Files {
			clean, ok := safeSuiteArchivePath(name)
			if !ok {
				continue
			}
			archiveName := "skills/" + root + "/" + clean
			if _, duplicate := archiveEntries[strings.ToLower(archiveName)]; duplicate {
				return nil, fmt.Errorf("duplicate suite archive entry: %s", archiveName)
			}
			archiveEntries[strings.ToLower(archiveName)] = struct{}{}
			data, err := base64.StdEncoding.DecodeString(encoded)
			if err != nil {
				continue
			}
			entry, err := zw.Create(archiveName)
			if err != nil {
				return nil, err
			}
			if _, err = entry.Write(data); err != nil {
				return nil, err
			}
			integrity["skills/"+root+"/"+clean] = sha256Hex(data)
			if strings.EqualFold(clean, "skill.md") || strings.EqualFold(clean, "skill.yaml") {
				wroteDefinition = true
			}
		}
		if strings.TrimSpace(member.AgentSkillMD) != "" && !wroteDefinition {
			archiveName := "skills/" + root + "/skill.md"
			if _, duplicate := archiveEntries[strings.ToLower(archiveName)]; duplicate {
				return nil, fmt.Errorf("duplicate suite archive entry: %s", archiveName)
			}
			archiveEntries[strings.ToLower(archiveName)] = struct{}{}
			entry, err := zw.Create(archiveName)
			if err != nil {
				return nil, err
			}
			if _, err = entry.Write([]byte(member.AgentSkillMD)); err != nil {
				return nil, err
			}
			integrity["skills/"+root+"/skill.md"] = sha256Hex([]byte(member.AgentSkillMD))
		}
	}
	suiteYAML := fmt.Sprintf("id: %s\nname: %s\nversion: %s\nmembers:\n", yamlQuote(su.ID), yamlQuote(su.Name), yamlQuote(su.Version))
	for _, member := range su.Members {
		root := memberRoots[strings.ToLower(firstNonEmpty(member.SkillID, member.SkillRef, member.Name))]
		if root == "" {
			root = path.Base(strings.Trim(strings.ReplaceAll(firstNonEmpty(member.Name, member.SkillID), "\\", "/"), "/"))
		}
		suiteYAML += fmt.Sprintf("  - skill_id: %s\n    name: %s\n    path: %s\n    required: %t\n", yamlQuote(member.SkillID), yamlQuote(member.Name), yamlQuote("skills/"+root), member.Required)
	}
	suiteYAMLBytes := []byte(suiteYAML)
	f, err = zw.Create("suite.yaml")
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(suiteYAMLBytes); err != nil {
		return nil, err
	}
	integrity["suite.yaml"] = sha256Hex(suiteYAMLBytes)
	// Emit the portable manifest names documented for Suite archives. Keep the
	// JSON envelope above for backwards compatibility with existing clients.
	manifestJSON, _ := json.MarshalIndent(map[string]any{"suite_id": su.ID, "files": integrity}, "", "  ")
	f, err = zw.Create("suite_integrity_manifest.json")
	if err != nil {
		return nil, err
	}
	if _, err = f.Write(manifestJSON); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func safeSuiteArchivePath(name string) (string, bool) {
	n := strings.TrimRight(strings.ReplaceAll(name, "\\", "/"), "/")
	if n == "" || strings.HasPrefix(n, "/") {
		return "", false
	}
	clean := path.Clean(n)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") || (len(clean) > 1 && clean[1] == ':') {
		return "", false
	}
	return clean, true
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func yamlQuote(value string) string {
	return "\"" + strings.ReplaceAll(strings.ReplaceAll(value, "\\", "\\\\"), "\"", "\\\"") + "\""
}

func (h *SkillHandlers) ListSuites(w http.ResponseWriter, r *http.Request) {
	if suites := h.store.ListSuites(); suites != nil {
		writeJSON(w, http.StatusOK, map[string]any{"suites": suites, "total": len(suites)})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"suites": []skill.SkillSuiteFull{}, "total": 0})
}

func (h *SkillHandlers) SearchSuites(w http.ResponseWriter, r *http.Request) {
	page := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("page")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			page = parsed
		}
	}
	writeJSON(w, http.StatusOK, h.store.SearchSuites(r.URL.Query().Get("q"), page))
}

// SetSuiteVisibility allows administrators to review, publish or withdraw a
// Suite without mutating its member Skill identities.
func (h *SkillHandlers) SetSuiteVisibility(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		skillError(w, http.StatusBadRequest, "suite id is required")
		return
	}
	var req struct {
		Visible *bool  `json:"visible"`
		Status  string `json:"status"`
	}
	if !decodeSkillJSON(w, r, &req, 16<<10) || req.Visible == nil {
		skillError(w, http.StatusBadRequest, "visible is required")
		return
	}
	su, err := h.store.GetSuite(id)
	if err != nil {
		skillError(w, http.StatusNotFound, "suite not found")
		return
	}
	status := strings.TrimSpace(req.Status)
	if status != "" {
		su.Status = status
	} else if !*req.Visible {
		su.Status = "withdrawn"
	} else {
		su.Status = "published"
	}
	if err := h.store.SetSuiteVisibility(id, *req.Visible, su.Status); err != nil {
		skillError(w, http.StatusInternalServerError, err.Error())
		return
	}
	su, _ = h.store.GetSuite(id)
	if h.marketStore != nil {
		eventType := "publish"
		if !*req.Visible {
			eventType = "withdraw"
		} else if strings.TrimSpace(req.Status) != "" && !strings.EqualFold(strings.TrimSpace(req.Status), "published") {
			eventType = "review"
		}
		_ = h.marketStore.RecordSuiteAuditEvent(r.Context(), id, eventType, strings.TrimSpace(r.Header.Get("X-User-ID")), "", suiteMemberIDsForAudit(su))
	}
	writeJSON(w, http.StatusOK, map[string]any{"suite": su, "status": su.Status})
}

// AdminGetSkill exposes a complete revision to authorized catalog managers,
// including revisions intentionally hidden from the public catalog.
func (h *SkillHandlers) AdminGetSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		skillError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	s, err := h.store.Get(id)
	if err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
}

func (h *SkillHandlers) PopularSkills(w http.ResponseWriter, r *http.Request) {
	skills := h.store.TopByDownloads(20)
	if skills == nil {
		skills = []skill.HubSkillMeta{}
	}
	writeJSON(w, http.StatusOK, skills)
}

func (h *SkillHandlers) PublishSkill(w http.ResponseWriter, r *http.Request) {
	if h.authSvc == nil {
		skillError(w, http.StatusServiceUnavailable, "skill publish auth service not configured")
		return
	}
	token := extractSessionToken(r)
	if token == "" {
		skillError(w, http.StatusUnauthorized, "session token required")
		return
	}
	if _, err := h.authSvc.ValidateSession(r.Context(), token); err != nil {
		skillError(w, http.StatusUnauthorized, "session expired or invalid")
		return
	}
	var s skill.HubSkillFull
	if !decodeSkillJSON(w, r, &s, maxSkillPublishJSONBytes) {
		return
	}
	if s.ID == "" || s.Name == "" {
		skillError(w, http.StatusBadRequest, "id and name are required")
		return
	}
	// Uploader self-reported elevated trust levels are never honored: only
	// admins may grant builtin/official/trusted (via the admin endpoints).
	switch s.TrustLevel {
	case "community", "agent-created":
	default:
		s.TrustLevel = "community"
	}
	if err := h.store.Publish(s); err != nil {
		skillError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, s)
}

func (h *SkillHandlers) RateSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		skillError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	var req struct {
		MaclawID string `json:"maclaw_id"`
		Score    int    `json:"score"`
	}
	if !decodeSkillJSON(w, r, &req, maxSkillSmallJSONBytes) {
		return
	}
	if req.MaclawID == "" {
		skillError(w, http.StatusBadRequest, "maclaw_id is required")
		return
	}
	if req.Score < 1 || req.Score > 5 {
		skillError(w, http.StatusBadRequest, "score must be between 1 and 5")
		return
	}
	if err := h.store.Rate(id, req.MaclawID, req.Score); err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SkillHandlers) AdminSetVisibility(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID      string `json:"id"`
		Visible bool   `json:"visible"`
	}
	if !decodeSkillJSON(w, r, &req, maxSkillSmallJSONBytes) {
		return
	}
	if req.ID == "" {
		skillError(w, http.StatusBadRequest, "id is required")
		return
	}
	if err := h.store.SetVisibility(req.ID, req.Visible); err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	// 设为不可见时从搜索索引中移除，设为可见时重新索引
	if h.searchSvc != nil {
		if !req.Visible {
			_ = h.searchSvc.RemoveSkill(r.Context(), req.ID)
		} else {
			_ = h.searchSvc.ReIndexSkill(r.Context(), req.ID)
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// AdminSetTrustLevel allows admins to set the trust level of a skill.
func (h *SkillHandlers) AdminSetTrustLevel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ID         string `json:"id"`
		TrustLevel string `json:"trust_level"`
	}
	if !decodeSkillJSON(w, r, &req, maxSkillSmallJSONBytes) {
		return
	}
	if req.ID == "" {
		skillError(w, http.StatusBadRequest, "id is required")
		return
	}
	validLevels := map[string]bool{"builtin": true, "official": true, "trusted": true, "community": true, "agent-created": true}
	if !validLevels[req.TrustLevel] {
		skillError(w, http.StatusBadRequest, "trust_level must be one of: builtin, official, trusted, community, agent-created")
		return
	}
	if err := h.store.SetTrustLevel(req.ID, req.TrustLevel); err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SkillHandlers) AdminDeleteSkill(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		skillError(w, http.StatusBadRequest, "skill id is required")
		return
	}
	if err := h.store.DeleteSkill(id); err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	// 从搜索索引中移除，防止已删除的 Skill 仍出现在搜索结果中
	if h.searchSvc != nil {
		_ = h.searchSvc.RemoveSkill(r.Context(), id)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *SkillHandlers) AdminImportFromURL(w http.ResponseWriter, r *http.Request) {
	var req struct {
		URL            string `json:"url"`
		Kind           string `json:"kind"`
		SuiteID        string `json:"suite_id"`
		PublishMembers *bool  `json:"publish_members"`
	}
	if !decodeSkillJSON(w, r, &req, maxSkillSmallJSONBytes) {
		return
	}
	if req.URL == "" {
		skillError(w, http.StatusBadRequest, "url is required")
		return
	}

	importer := skill.NewRemoteImporter()
	result, err := importer.ImportFromURL(req.URL)
	if err != nil {
		skillError(w, http.StatusBadRequest, err.Error())
		return
	}
	if strings.EqualFold(strings.TrimSpace(req.Kind), "suite") && result.Suite == nil && len(result.Skills) > 0 {
		first := result.Skills[0]
		suiteID := strings.TrimSpace(req.SuiteID)
		if suiteID == "" {
			suiteID = "github-import-suite"
		}
		su := &skill.SkillSuiteFull{SkillSuiteMeta: skill.SkillSuiteMeta{ID: suiteID, Name: first.Name, Description: first.Description, Version: first.Version, SourceURL: first.SourceURL, Visible: true, Status: "published"}, Skills: append([]skill.HubSkillFull(nil), result.Skills...), Manifest: skill.SuiteManifest{Format: "skill-suite.v1"}}
		for i, member := range result.Skills {
			su.Members = append(su.Members, skill.SkillSuiteMember{SkillID: member.ID, SkillRef: member.SkillID, Name: member.Name, Version: member.Version, Required: true, Order: i})
		}
		result.Suite = su
		result.PackageKind = "suite"
	}

	// 发布每个 skill，重复的按 source_url+name 覆盖
	var published []string
	publishedByName := make(map[string]skill.HubSkillFull)
	publishMembers := req.PublishMembers == nil || *req.PublishMembers
	for _, sk := range result.Skills {
		if !publishMembers {
			continue
		}
		// 检查是否已存在同 source_url + name 的 skill
		existing := h.store.FindBySourceURL(sk.SourceURL, sk.Name)
		if existing != nil {
			// 覆盖更新：复用旧 ID，保留统计数据
			sk.ID = existing.ID
			sk.CreatedAt = existing.CreatedAt
			sk.UpdatedAt = time.Now().Format(time.RFC3339)
			sk.Downloads = existing.Downloads
			sk.DownloadCount = existing.DownloadCount
			sk.RatingSum = existing.RatingSum
			sk.RatingCount = existing.RatingCount
			sk.AvgRating = existing.AvgRating
			sk.Visible = existing.Visible
			sk.Status = existing.Status
			sk.TrustLevel = existing.TrustLevel
		}
		sk.Price = 0
		if existing == nil {
			sk.Visible = true
		}
		// Admin-imported skills default to "trusted" (official store content).
		// User-submitted skills via PublishSkill remain "community".
		if sk.TrustLevel == "" || sk.TrustLevel == "community" {
			sk.TrustLevel = "trusted"
		}
		if err := h.store.Publish(sk); err != nil {
			result.Errors = append(result.Errors, "publish "+sk.Name+": "+err.Error())
			continue
		}
		published = append(published, sk.Name)
		publishedByName[strings.ToLower(strings.TrimSpace(sk.Name))] = sk
	}
	if result.Suite != nil && (len(published) > 0 || !publishMembers) {
		// Keep Suite member references aligned with reused Skill IDs when an
		// import updates an existing source/name pair.
		for i := range result.Suite.Skills {
			if sk, ok := publishedByName[strings.ToLower(strings.TrimSpace(result.Suite.Skills[i].Name))]; ok {
				result.Suite.Skills[i] = sk
			}
		}
		for i := range result.Suite.Members {
			if sk, ok := publishedByName[strings.ToLower(strings.TrimSpace(result.Suite.Members[i].Name))]; ok {
				result.Suite.Members[i].SkillID = sk.ID
				if result.Suite.Members[i].SkillRef == "" {
					result.Suite.Members[i].SkillRef = sk.SkillID
				}
			}
		}
		if existingSuite, getErr := h.store.GetSuite(result.Suite.ID); getErr != nil || existingSuite == nil {
			result.Suite.Visible = true
			result.Suite.Status = "published"
			result.Suite.TrustLevel = "trusted"
		} else {
			result.Suite.Visible = existingSuite.Visible
			result.Suite.Status = existingSuite.Status
			result.Suite.TrustLevel = existingSuite.TrustLevel
			result.Suite.Downloads = existingSuite.Downloads
		}
		if len(result.Errors) > 0 {
			result.Suite.Status = "needs_review"
			// A partially imported Suite must not be discoverable or
			// downloadable until an administrator completes review.
			result.Suite.Visible = false
		}
		if raw, marshalErr := json.Marshal(result.Suite.Skills); marshalErr == nil {
			sum := sha256.Sum256(raw)
			result.Suite.PackageSHA256 = hex.EncodeToString(sum[:])
		}
		if err := h.store.PublishSuite(*result.Suite); err != nil {
			result.Errors = append(result.Errors, "publish suite: "+err.Error())
		} else if h.marketStore != nil {
			actor := strings.TrimSpace(r.Header.Get("X-User-ID"))
			if actor == "" {
				actor = strings.TrimSpace(r.URL.Query().Get("email"))
			}
			members := make([]string, 0, len(result.Suite.Members))
			for _, member := range result.Suite.Members {
				if id := firstNonEmpty(member.SkillID, member.SkillRef); id != "" {
					members = append(members, id)
				}
			}
			_ = h.marketStore.RecordSuiteAuditEvent(r.Context(), result.Suite.ID, "publish", actor, "", members)
		}
	}

	response := map[string]interface{}{
		"published": published,
		"errors":    result.Errors,
		"total":     len(result.Skills),
	}
	if result.Suite != nil {
		response["package_kind"] = "suite"
		response["suite"] = result.Suite
		response["suite_id"] = result.Suite.ID
	}
	writeJSON(w, http.StatusOK, response)
}

func (h *SkillHandlers) AdminListSkills(w http.ResponseWriter, r *http.Request) {
	pageStr := r.URL.Query().Get("page")
	pageSizeStr := r.URL.Query().Get("page_size")
	page := 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}
	perPage := 0
	if pageSizeStr != "" {
		if ps, err := strconv.Atoi(pageSizeStr); err == nil && ps > 0 {
			perPage = ps
		}
	}
	var result skill.SkillSearchResult
	if perPage > 0 {
		result = h.store.ListAllPaged(page, perPage)
	} else {
		result = h.store.ListAll(page)
	}
	suites := h.store.ListSuitesAll()
	result.Suites = make([]skill.SkillSuiteMeta, 0, len(suites))
	for _, suite := range suites {
		result.Suites = append(result.Suites, suite.SkillSuiteMeta)
	}
	writeJSON(w, http.StatusOK, result)
}
