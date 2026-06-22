package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/hub/internal/security"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type workflowDraftGenerateRequest struct {
	Description string `json:"description"`
	Language    string `json:"language"`
}

const workflowDraftDescriptionMaxBytes = 4000

func WorkflowDraftLLMHandler(identity veMachineAuthenticator, system store.SystemSettingsRepository, securitySvc *security.SecurityService) http.HandlerFunc {
	_ = system
	_ = securitySvc
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			writeError(w, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "use POST")
			return
		}
		if _, ok := authenticateVEMachine(w, r, identity); !ok {
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, workflowDraftDescriptionMaxBytes*2)
		var req workflowDraftGenerateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			var maxBytesErr *http.MaxBytesError
			if errors.As(err, &maxBytesErr) {
				writeError(w, http.StatusRequestEntityTooLarge, "REQUEST_TOO_LARGE", "workflow draft request is too large")
				return
			}
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "invalid JSON")
			return
		}
		description, ok := normalizeWorkflowDraftDescription(req.Description)
		if !ok {
			writeError(w, http.StatusBadRequest, "DESCRIPTION_REQUIRED", "description is required")
			return
		}
		writeJSON(w, http.StatusOK, buildFallbackWorkflowDraft(description, req.Language))
	}
}

func normalizeWorkflowDraftDescription(value string) (string, bool) {
	description := strings.TrimSpace(value)
	if description == "" {
		return "", false
	}
	if len(description) > workflowDraftDescriptionMaxBytes {
		var clipped strings.Builder
		clipped.Grow(workflowDraftDescriptionMaxBytes)
		for _, r := range description {
			runeLen := utf8.RuneLen(r)
			if runeLen < 0 {
				runeLen = len("\ufffd")
			}
			if clipped.Len()+runeLen > workflowDraftDescriptionMaxBytes {
				break
			}
			clipped.WriteRune(r)
		}
		description = clipped.String()
	}
	return description, true
}

func buildFallbackWorkflowDraft(description, language string) map[string]any {
	zh := strings.HasPrefix(strings.ToLower(strings.TrimSpace(language)), "zh")
	name := "Approval workflow draft"
	if zh {
		name = "\u5ba1\u6279\u6d41\u7a0b\u8349\u7a3f"
	}
	return map[string]any{
		"name":        name,
		"description": description,
		"graph": map[string]any{
			"nodes": []map[string]any{
				{
					"id":       "node1",
					"type":     "trigger",
					"label":    fallbackWorkflowLabel(zh, "Start", "\u5f00\u59cb"),
					"position": map[string]any{"x": 80, "y": 80},
					"config":   map[string]any{"trigger_type": "manual", "description": description},
				},
				{
					"id":       "node2",
					"type":     "form",
					"label":    fallbackWorkflowLabel(zh, "Submit request", "\u63d0\u4ea4\u7533\u8bf7"),
					"position": map[string]any{"x": 300, "y": 80},
					"config": map[string]any{
						"fields":      []any{},
						"description": description,
					},
				},
				{
					"id":       "node3",
					"type":     "approval",
					"label":    fallbackWorkflowLabel(zh, "Approval", "\u5ba1\u6279"),
					"position": map[string]any{"x": 520, "y": 80},
					"config": map[string]any{
						"approver_ids":      []any{},
						"mode":              "single",
						"min_approvals":     1,
						"approver_order":    []any{},
						"timeout_hours":     24,
						"fallback_approver": "",
					},
				},
				{
					"id":       "node4",
					"type":     "terminal",
					"label":    fallbackWorkflowLabel(zh, "Complete", "\u5b8c\u6210"),
					"position": map[string]any{"x": 740, "y": 80},
					"config": map[string]any{
						"result_executors": []any{},
						"notifiers":        []any{},
					},
				},
			},
			"edges": []map[string]any{
				{"id": "edge1", "source_id": "node1", "target_id": "node2"},
				{"id": "edge2", "source_id": "node2", "target_id": "node3"},
				{"id": "edge3", "source_id": "node3", "target_id": "node4"},
			},
		},
	}
}

func fallbackWorkflowLabel(zh bool, en, zhHans string) string {
	if zh {
		return zhHans
	}
	return en
}
