package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/hub/internal/im"
	"github.com/RapidAI/CodeClaw/hub/internal/llmservice"
	"github.com/RapidAI/CodeClaw/hub/internal/store"
)

type systemFreeUpdateRequest struct {
	Name        string                        `json:"name"`
	Description string                        `json:"description"`
	Models      []llmservice.ModelServiceModel `json:"models"`
}

func configuredProviderIDSet(providerReg *im.LLMProviderRegistry) map[string]struct{} {
	out := map[string]struct{}{}
	if providerReg == nil {
		return out
	}
	for _, p := range providerReg.Providers {
		id := strings.ToLower(strings.TrimSpace(p.ID))
		if id != "" {
			out[id] = struct{}{}
		}
	}
	return out
}

func loadSystemFreeRegistry(r *http.Request, system store.SystemSettingsRepository) (*llmservice.Registry, error) {
	reg, err := llmservice.LoadRegistry(r.Context(), system)
	if err != nil {
		return nil, err
	}
	if llmservice.EnsureSystemFreeServiceGroup(reg) {
		if saveErr := llmservice.SaveRegistry(r.Context(), system, reg); saveErr != nil {
			log.Printf("[system-free] ensure save failed: %v", saveErr)
		} else {
			invalidateLLMRuntimeCaches(system)
		}
	}
	return reg, nil
}

// GetSystemFreeLLMHandler returns the reserved system-free group status.
func GetSystemFreeLLMHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		reg, err := loadSystemFreeRegistry(r, system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		status := llmservice.EvaluateSystemFreeStatus(reg, configuredProviderIDSet(providerReg))
		writeJSON(w, http.StatusOK, status)
	}
}

// UpdateSystemFreeLLMHandler updates editable fields of system-free (providers/models).
// Cannot delete the group or change access_policy away from free.
func UpdateSystemFreeLLMHandler(system store.SystemSettingsRepository, audits ...store.AdminAuditRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		audit := firstAdminAuditRepo(audits...)
		var req systemFreeUpdateRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "Invalid request body")
			return
		}
		reg, err := loadSystemFreeRegistry(r, system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		old := *reg
		group := reg.FindModelServiceGroup(llmservice.SystemFreeServiceGroupID)
		if group == nil {
			writeError(w, http.StatusInternalServerError, "SYSTEM_FREE_MISSING", "system-free service group missing")
			return
		}
		if name := strings.TrimSpace(req.Name); name != "" {
			group.Name = name
		}
		if desc := strings.TrimSpace(req.Description); desc != "" {
			group.Description = desc
		}
		if req.Models != nil {
			group.Models = append([]llmservice.ModelServiceModel(nil), req.Models...)
		}
		group.ID = llmservice.SystemFreeServiceGroupID
		group.AccessPolicy = llmservice.AccessPolicyFree
		// Write group back into registry slice.
		for i := range reg.ModelServiceGroups {
			if llmservice.IsSystemFreeServiceGroup(reg.ModelServiceGroups[i].ID) {
				reg.ModelServiceGroups[i] = *group
				break
			}
		}
		llmservice.ProtectSystemFreeOnSave(reg, &old)
		reg.Normalize()

		providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		if issues := collectLLMServiceProviderReferenceIssues(reg, providerReg); len(issues) > 0 {
			// Only fail if system-free itself has unknown providers.
			for _, issue := range issues {
				if strings.Contains(issue, `service group "system-free"`) || strings.Contains(issue, `service group "System-free"`) {
					writeError(w, http.StatusBadRequest, "LLM_SERVICE_PROVIDER_NOT_FOUND", issue)
					return
				}
			}
			// Also match case-insensitive group id in issue string.
			for _, issue := range issues {
				if strings.Contains(strings.ToLower(issue), `service group "system-free"`) {
					writeError(w, http.StatusBadRequest, "LLM_SERVICE_PROVIDER_NOT_FOUND", issue)
					return
				}
			}
		}
		if err := llmservice.SaveRegistry(r.Context(), system, reg); err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_SAVE_FAILED", err.Error())
			return
		}
		writeAdminAuditLog(r.Context(), audit, adminAuditUserID(r), "llm.system_free.update", map[string]any{
			"service_group_id": llmservice.SystemFreeServiceGroupID,
		})
		invalidateLLMRuntimeCaches(system)
		status := llmservice.EvaluateSystemFreeStatus(reg, configuredProviderIDSet(providerReg))
		writeJSON(w, http.StatusOK, status)
	}
}

// TestSystemFreeLLMHandler probes system-free availability with a minimal LLM call.
func TestSystemFreeLLMHandler(system store.SystemSettingsRepository) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		system := scopedSystemSettingsForRequest(r, system)
		reg, err := loadSystemFreeRegistry(r, system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_SERVICE_LOAD_FAILED", err.Error())
			return
		}
		providerReg, err := im.LoadLLMProviderRegistry(r.Context(), system)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "LLM_PROVIDER_LOAD_FAILED", err.Error())
			return
		}
		status := llmservice.EvaluateSystemFreeStatus(reg, configuredProviderIDSet(providerReg))
		if !status.Present {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": false, "success": false, "error": "system-free service group missing",
				"service_group_id": llmservice.SystemFreeServiceGroupID, "status": status,
			})
			return
		}
		models, _ := llmservice.BuildAuthorizedModelsForServiceGroups(reg, []string{llmservice.SystemFreeServiceGroupID})
		models = filterAuthorizedModelsForConfiguredProviders(models, providerReg)
		if len(models) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": false, "success": false, "error": "system-free has no routable models",
				"service_group_id": llmservice.SystemFreeServiceGroupID, "status": status,
			})
			return
		}
		body := map[string]any{
			"model": "auto",
			"messages": []map[string]string{
				{"role": "user", "content": "Reply with exactly: pong"},
			},
		}
		model, externalModel, err := resolveAuthorizedModel(body, models)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": false, "success": false, "error": err.Error(),
				"service_group_id": llmservice.SystemFreeServiceGroupID, "status": status,
			})
			return
		}

		// Prefer local provider direct test when first provider is local; otherwise
		// use the same authorized forward path as production server-side calls.
		start := time.Now()
		providerID := ""
		if len(model.ProviderIDs) > 0 {
			providerID = strings.TrimSpace(model.ProviderIDs[0])
		}
		if llmservice.IsBuiltinProvider(providerID) {
			respBody, statusCode, usedProviderID, serviceGroupIDs, fwdErr := forwardAuthorizedModelRequest(r, providerReg, model, body, externalModel)
			elapsed := time.Since(start)
			if fwdErr != nil {
				writeJSON(w, http.StatusOK, map[string]any{
					"ok": false, "success": false, "error": fwdErr.Error(),
					"service_group_id": llmservice.SystemFreeServiceGroupID,
					"provider_id":      usedProviderID,
					"model":            model.Name,
					"service_groups":   serviceGroupIDs,
					"status_code":      statusCode,
					"latency_ms":       elapsed.Milliseconds(),
					"status":           status,
				})
				return
			}
			if statusCode < http.StatusOK || statusCode >= http.StatusMultipleChoices {
				snippet := workflowDraftProviderResponseSnippet(respBody)
				writeJSON(w, http.StatusOK, map[string]any{
					"ok": false, "success": false,
					"error":            "upstream returned HTTP " + http.StatusText(statusCode),
					"service_group_id": llmservice.SystemFreeServiceGroupID,
					"provider_id":      usedProviderID,
					"model":            model.Name,
					"status_code":      statusCode,
					"response":         snippet,
					"latency_ms":       elapsed.Milliseconds(),
					"status":           status,
				})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": true, "success": true, "message": "system-free is available",
				"service_group_id": llmservice.SystemFreeServiceGroupID,
				"provider_id":      usedProviderID,
				"model":            model.Name,
				"latency_ms":       elapsed.Milliseconds(),
				"status":           status,
			})
			return
		}

		// Local provider path: direct ping when credentials exist.
		local := providerReg.FindProvider(providerID)
		if local == nil || strings.TrimSpace(local.APIURL) == "" || strings.TrimSpace(local.APIKey) == "" {
			// Fall back to authorized forward (may still resolve multi-provider).
			respBody, statusCode, usedProviderID, serviceGroupIDs, fwdErr := forwardAuthorizedModelRequest(r, providerReg, model, body, externalModel)
			elapsed := time.Since(start)
			if fwdErr != nil || statusCode < 200 || statusCode >= 300 {
				errMsg := "provider not fully configured"
				if fwdErr != nil {
					errMsg = fwdErr.Error()
				} else {
					errMsg = "upstream returned HTTP " + http.StatusText(statusCode) + ": " + workflowDraftProviderResponseSnippet(respBody)
				}
				writeJSON(w, http.StatusOK, map[string]any{
					"ok": false, "success": false, "error": errMsg,
					"service_group_id": llmservice.SystemFreeServiceGroupID,
					"provider_id":      usedProviderID,
					"model":            model.Name,
					"service_groups":   serviceGroupIDs,
					"status_code":      statusCode,
					"latency_ms":       elapsed.Milliseconds(),
					"status":           status,
				})
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": true, "success": true, "message": "system-free is available",
				"service_group_id": llmservice.SystemFreeServiceGroupID,
				"provider_id":      usedProviderID,
				"model":            model.Name,
				"latency_ms":       elapsed.Milliseconds(),
				"status":           status,
			})
			return
		}
		cfg := corelib.MaclawLLMConfig{
			URL:       local.APIURL,
			Key:       local.APIKey,
			Model:     systemFreeFirstNonEmpty(local.Model, model.Name, "auto"),
			Protocol:  normalizeProviderProtocol(local.Protocol),
			WireAPI:   normalizeProviderWireAPI(local.WireAPI),
			AgentType: strings.TrimSpace(local.AgentType),
		}
		messages := []interface{}{map[string]string{"role": "user", "content": "Reply with exactly: pong"}}
		resp, err := agent.DoSimpleLLMRequest(cfg, messages, http.DefaultClient, 15*time.Second)
		elapsed := time.Since(start)
		if err != nil {
			writeJSON(w, http.StatusOK, map[string]any{
				"ok": false, "success": false, "error": err.Error(),
				"service_group_id": llmservice.SystemFreeServiceGroupID,
				"provider_id":      providerID,
				"model":            cfg.Model,
				"latency_ms":       elapsed.Milliseconds(),
				"status":           status,
			})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"ok": true, "success": true, "message": "system-free is available",
			"service_group_id": llmservice.SystemFreeServiceGroupID,
			"provider_id":      providerID,
			"model":            cfg.Model,
			"reply":            resp.Content,
			"latency_ms":       elapsed.Milliseconds(),
			"status":           status,
		})
	}
}

func systemFreeFirstNonEmpty(values ...string) string {
	for _, v := range values {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}
