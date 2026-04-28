package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"

	marketschema "github.com/RapidAI/CodeClaw/corelib/skillmarket"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/license"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/skillmarket"
	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/store"
)

type SkillMarketHandler struct {
	centerAuth centerAuthenticator
	licenses   *license.Service
	skills     *skillmarket.Service
}

type CloudSkill = marketschema.Skill

func NewSkillMarketHandler(centerAuth centerAuthenticator, licenses *license.Service, skills *skillmarket.Service) *SkillMarketHandler {
	return &SkillMarketHandler{centerAuth: centerAuth, licenses: licenses, skills: skills}
}

func (h *SkillMarketHandler) ListAdminSkills() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, err := h.skills.List(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LIST_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, marketschema.CatalogResponse{Skills: convertSkills(items, true)})
	}
}

func (h *SkillMarketHandler) CreateAdminSkill() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input skillmarket.SkillInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		skill, err := h.skills.Create(r.Context(), input)
		if err != nil {
			writeError(w, http.StatusBadRequest, "CREATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusCreated, convertSkill(skill, true))
	}
}

func (h *SkillMarketHandler) UpdateAdminSkill() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var input skillmarket.SkillInput
		if err := decodeJSON(r, &input); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid request body")
			return
		}
		skill, err := h.skills.Update(r.Context(), r.PathValue("skill_id"), input)
		if err != nil {
			writeError(w, http.StatusBadRequest, "UPDATE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, convertSkill(skill, true))
	}
}

func (h *SkillMarketHandler) DeleteAdminSkill() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if err := h.skills.Delete(r.Context(), r.PathValue("skill_id")); err != nil {
			writeError(w, http.StatusInternalServerError, "DELETE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	}
}

func (h *SkillMarketHandler) SearchCenterSkills() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if _, ok := h.authorize(w, r, centerID); !ok {
			return
		}

		items, err := h.skills.SearchActive(r.Context(), r.URL.Query().Get("q"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, "SEARCH_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, marketschema.SearchResponse{Results: convertSkills(items, false)})
	}
}

func (h *SkillMarketHandler) GetCenterSkill() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		centerID := r.PathValue("id")
		if _, ok := h.authorize(w, r, centerID); !ok {
			return
		}

		skill, err := h.skills.GetActive(r.Context(), r.PathValue("skill_id"))
		if err != nil {
			writeError(w, http.StatusNotFound, "SKILL_NOT_FOUND", "skill not found")
			return
		}
		writeJSON(w, http.StatusOK, convertSkill(skill, false))
	}
}

func (h *SkillMarketHandler) authorize(w http.ResponseWriter, r *http.Request, centerID string) (*store.Center, bool) {
	center, ok := authenticateCenterRequest(w, r, h.centerAuth, centerID)
	if !ok {
		return nil, false
	}
	if h.licenses == nil {
		writeError(w, http.StatusInternalServerError, "LICENSE_UNAVAILABLE", "license service is unavailable")
		return nil, false
	}
	lic, err := h.licenses.GetActive(r.Context(), centerID)
	if err != nil || lic == nil {
		writeError(w, http.StatusForbidden, "NO_ACTIVE_LICENSE", "active license is required")
		return nil, false
	}
	if !licenseAllowsSkillMarket(lic.Modules) {
		writeError(w, http.StatusForbidden, "SKILL_MARKET_NOT_LICENSED", "skill market module is not licensed")
		return nil, false
	}
	return center, true
}

func licenseAllowsSkillMarket(modulesJSON string) bool {
	var modules []string
	if err := json.Unmarshal([]byte(modulesJSON), &modules); err != nil {
		return false
	}
	for _, module := range modules {
		switch strings.ToLower(strings.TrimSpace(module)) {
		case "skill_market", "skills", "skill", "all":
			return true
		}
	}
	return false
}

func convertSkills(items []*store.Skill, includeAdminFields bool) []marketschema.Skill {
	out := make([]marketschema.Skill, 0, len(items))
	for _, item := range items {
		out = append(out, convertSkill(item, includeAdminFields))
	}
	return out
}

func convertSkill(item *store.Skill, includeAdminFields bool) marketschema.Skill {
	if item == nil {
		return marketschema.Skill{}
	}
	var tags []string
	_ = json.Unmarshal([]byte(item.Tags), &tags)
	result := marketschema.Skill{
		ID:            item.ID,
		Name:          item.Name,
		Description:   item.Description,
		Category:      item.Category,
		Version:       item.Version,
		Tags:          tags,
		RiskLevel:     item.RiskLevel,
		Status:        item.Status,
		Price:         item.Price,
		Author:        item.Author,
		AvgRating:     item.AvgRating,
		DownloadCount: item.DownloadCount,
		Downloads:     item.DownloadCount,
	}
	if includeAdminFields {
		result.CreatedAt = marketschema.FormatTime(item.CreatedAt)
		result.UpdatedAt = marketschema.FormatTime(item.UpdatedAt)
	}
	return result
}
