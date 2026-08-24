package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/hubcenter/internal/llmservice"
)

// adminClassHeadGroupID is always empty. HubCenter serves one official head;
// query group_id must not write a per-group store that classify never loads.
func adminClassHeadGroupID(_ *http.Request) string {
	return ""
}

func adminClassHeadQueryGroupID(r *http.Request) string {
	if r == nil || r.URL == nil {
		return ""
	}
	return strings.TrimSpace(r.URL.Query().Get("group_id"))
}

func writeLLMUnavailable(w http.ResponseWriter, llmSvc *llmservice.Service) bool {
	if llmSvc != nil {
		return false
	}
	writeJSONResp(w, http.StatusServiceUnavailable, map[string]string{"error": "llm service unavailable"})
	return true
}

func officialClassHeadPublishHandler(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeLLMUnavailable(w, llmSvc) {
			return
		}
		writeJSONResp(w, http.StatusOK, llmSvc.PublishedOfficialHead())
	}
}

func adminClassHeadSamplePage(r *http.Request) int {
	if r == nil || r.URL == nil {
		return 1
	}
	raw := strings.TrimSpace(r.URL.Query().Get("page"))
	if raw == "" {
		return 1
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 1
	}
	return n
}

func adminLLMClassHeadHandler(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeLLMUnavailable(w, llmSvc) {
			return
		}
		writeJSONResp(w, http.StatusOK, llmSvc.OfficialClassHeadViewPage(adminClassHeadGroupID(r), adminClassHeadSamplePage(r)))
	}
}

func adminLLMClassHeadTrainHandler(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeLLMUnavailable(w, llmSvc) {
			return
		}
		if err := llmSvc.EnqueueOfficialClassHeadTrainFor(adminClassHeadGroupID(r)); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusAccepted, map[string]any{"queued": true, "status": llmpool.HeadStatusTraining})
	}
}

func adminLLMClassHeadPipelineHandler(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeLLMUnavailable(w, llmSvc) {
			return
		}
		var req struct {
			Mode     string `json:"mode"`
			Override string `json:"override"`
			Reason   string `json:"reason"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		view, err := llmSvc.SetOfficialClassHeadPipelineFor(adminClassHeadGroupID(r), req.Mode, req.Override, req.Reason)
		if err != nil {
			code := http.StatusConflict
			if llmpool.IsPromoteBlocked(err) {
				code = http.StatusConflict
			}
			writeJSONResp(w, code, map[string]any{"error": err.Error(), "view": view})
			return
		}
		writeJSONResp(w, http.StatusOK, view)
	}
}

func adminLLMClassHeadReviewHandler(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeLLMUnavailable(w, llmSvc) {
			return
		}
		var req struct {
			SampleID  string `json:"sample_id"`
			GoldClass string `json:"gold_class"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		view, err := llmSvc.ReviewOfficialClassHeadFor(adminClassHeadGroupID(r), req.SampleID, req.GoldClass)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, view)
	}
}

func adminLLMClassHeadSampleDeleteHandler(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeLLMUnavailable(w, llmSvc) {
			return
		}
		var req struct {
			SampleID string `json:"sample_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		view, err := llmSvc.DeleteOfficialClassHeadSampleFor(adminClassHeadGroupID(r), req.SampleID)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, view)
	}
}

func adminLLMClassHeadRollbackHandler(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeLLMUnavailable(w, llmSvc) {
			return
		}
		view, err := llmSvc.RollbackOfficialClassHeadFor(adminClassHeadGroupID(r))
		if err != nil {
			code := http.StatusInternalServerError
			if llmpool.IsPipelineRuleBlocked(err) {
				code = http.StatusConflict
			}
			writeJSONResp(w, code, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, view)
	}
}

func adminLLMClassHeadPullOfficialHandler(_ *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSONResp(w, http.StatusBadRequest, map[string]string{
			"error": "HubCenter has one official head; groups do not pull it",
		})
	}
}

func adminLLMClassHeadTrainerHandler(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeLLMUnavailable(w, llmSvc) {
			return
		}
		var req struct {
			NodeID string `json:"node_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		view, err := llmSvc.SetOfficialClassHeadTrainerFor(adminClassHeadGroupID(r), req.NodeID)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, view)
	}
}

func adminLLMClassHeadScoreHandler(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeLLMUnavailable(w, llmSvc) {
			return
		}
		var req struct {
			Slot    string            `json:"slot"`
			Text    string            `json:"text"`
			GroupID string            `json:"group_id"`
			Headers map[string]string `json:"headers"`
			Body    map[string]any    `json:"body"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		body := req.Body
		if body == nil {
			body = map[string]any{}
		}
		if strings.TrimSpace(req.Text) != "" {
			if _, ok := body["messages"]; !ok {
				body["messages"] = []any{map[string]any{"role": "user", "content": req.Text}}
			}
			if _, ok := body["model"]; !ok {
				body["model"] = "auto"
			}
		}
		header := http.Header{}
		for key, value := range req.Headers {
			header.Set(key, value)
		}
		report, err := llmSvc.ScoreOfficialClassHeadFor(r.Context(), req.GroupID, req.Slot, header, body)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, report)
	}
}

func adminLLMClassHeadDistributeHandler(llmSvc *llmservice.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if writeLLMUnavailable(w, llmSvc) {
			return
		}
		nodeID := r.URL.Query().Get("node_id")
		view, err := llmSvc.DistributeOfficialClassHeadFor(adminClassHeadGroupID(r), nodeID)
		if err != nil {
			writeJSONResp(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		writeJSONResp(w, http.StatusOK, view)
	}
}
