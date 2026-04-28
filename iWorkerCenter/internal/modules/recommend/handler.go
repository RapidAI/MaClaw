package recommend

import (
	"encoding/json"
	"net/http"

	colleagueRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/colleagues/repo"
	roleRepo "github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/roles/repo"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/modules/tenant"
	"github.com/RapidAI/CodeClaw/iWorkerCenter/internal/shared/response"
)

// Handler exposes the recommendation API.
type Handler struct {
	colleagueRp *colleagueRepo.ColleagueRepo
	roleRp      *roleRepo.RoleRepo
}

// NewHandler creates a Handler.
func NewHandler(colleagueRp *colleagueRepo.ColleagueRepo, roleRp *roleRepo.RoleRepo) *Handler {
	return &Handler{colleagueRp: colleagueRp, roleRp: roleRp}
}

// RegisterClientRoutes registers client-facing routes.
func (h *Handler) RegisterClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/client/recommend", h.handleRecommend)
}

func (h *Handler) handleRecommend(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		response.Error(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
		return
	}

	var req struct {
		TaskDescription string `json:"task_description"`
		TopN            int    `json:"top_n"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "INVALID_BODY", "invalid JSON")
		return
	}

	if req.TaskDescription == "" {
		response.BadRequest(w, "MISSING_TASK", "task_description is required")
		return
	}

	// Build colleague profiles from DB
	tid := tenant.RequestTenantID(r)
	colleagues, err := h.colleagueRp.ListActive(tid)
	if err != nil {
		response.Internal(w, err.Error())
		return
	}

	profiles := make([]ColleagueProfile, 0, len(colleagues))
	for _, c := range colleagues {
		roleName := ""
		roleCode := ""
		if c.RoleID != "" {
			if role, err := h.roleRp.GetByID(tid, c.RoleID); err == nil {
				roleName = role.Name
				roleCode = role.Code
			}
		}
		profiles = append(profiles, ColleagueProfile{
			ID:        c.ID,
			Name:      c.Name,
			RoleCode:  roleCode,
			RoleName:  roleName,
			Strengths: c.Strengths,
			Tasks:     c.Tasks,
		})
	}

	recs := Recommend(req.TaskDescription, profiles, req.TopN)
	if recs == nil {
		recs = []Recommendation{}
	}

	response.OK(w, map[string]any{"recommendations": recs})
}
