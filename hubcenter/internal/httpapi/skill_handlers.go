package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	coreskill "github.com/RapidAI/CodeClaw/corelib/skill"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/skill"
)

const (
	// Align with skill.MaxSkillPackageDownloadBytes so multi-asset packages
	// can be published/accepted (base64 file maps exceed the old 5 MiB cap).
	maxSkillPublishJSONBytes = coreskill.MaxSkillPackageDownloadBytes
	maxSkillSmallJSONBytes   = 4096
)

type SkillHandlers struct {
	store     *skill.SkillStore
	searchSvc skillSearchRemover
}

// skillSearchRemover is the subset of SearchService needed by SkillHandlers.
type skillSearchRemover interface {
	RemoveSkill(ctx context.Context, id string) error
	ReIndexSkill(ctx context.Context, id string) error
}

func NewSkillHandlers(store *skill.SkillStore, searchSvc skillSearchRemover) *SkillHandlers {
	return &SkillHandlers{store: store, searchSvc: searchSvc}
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
	s, err := h.store.GetVisible(meta.ID)
	if err != nil {
		skillError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s)
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
	var s skill.HubSkillFull
	if !decodeSkillJSON(w, r, &s, maxSkillPublishJSONBytes) {
		return
	}
	if s.ID == "" || s.Name == "" {
		skillError(w, http.StatusBadRequest, "id and name are required")
		return
	}
	s.TrustLevel = "trusted"
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
		URL string `json:"url"`
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

	// 发布每个 skill，重复的按 source_url+name 覆盖
	var published []string
	for _, sk := range result.Skills {
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
		}
		sk.Price = 0
		sk.Visible = true
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
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"published": published,
		"errors":    result.Errors,
		"total":     len(result.Skills),
	})
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
	writeJSON(w, http.StatusOK, result)
}
