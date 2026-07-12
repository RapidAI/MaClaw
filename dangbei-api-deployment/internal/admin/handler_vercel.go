package admin

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func (h *Handler) syncVercel(w http.ResponseWriter, r *http.Request) {
	var req map[string]any
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "invalid json"})
		return
	}
	vercelToken, _ := req["vercel_token"].(string)
	projectID, _ := req["project_id"].(string)
	teamID, _ := req["team_id"].(string)
	autoValidate := true
	if v, ok := req["auto_validate"].(bool); ok {
		autoValidate = v
	}
	saveCreds := true
	if v, ok := req["save_credentials"].(bool); ok {
		saveCreds = v
	}
	usePreconfig := vercelToken == "__USE_PRECONFIG__" || strings.TrimSpace(vercelToken) == ""
	if usePreconfig {
		vercelToken = strings.TrimSpace(os.Getenv("VERCEL_TOKEN"))
	}
	if strings.TrimSpace(projectID) == "" {
		projectID = strings.TrimSpace(os.Getenv("VERCEL_PROJECT_ID"))
	}
	if strings.TrimSpace(teamID) == "" {
		teamID = strings.TrimSpace(os.Getenv("VERCEL_TEAM_ID"))
	}
	if vercelToken == "" || projectID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"detail": "需要 Vercel Token 和 Project ID"})
		return
	}
	validated, failed := 0, []string{}
	if autoValidate {
		for _, acc := range h.Store.Snapshot().Accounts {
			if strings.TrimSpace(acc.Token) != "" {
				continue
			}
			token, err := h.DS.Login(r.Context(), acc)
			if err != nil {
				failed = append(failed, acc.Identifier())
			} else {
				validated++
				_ = h.Store.UpdateAccountToken(acc.Identifier(), token)
			}
			time.Sleep(500 * time.Millisecond)
		}
	}

	cfgJSON, _, err := h.Store.ExportJSONAndBase64()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"detail": err.Error()})
		return
	}
	cfgB64 := base64.StdEncoding.EncodeToString([]byte(cfgJSON))
	client := &http.Client{Timeout: 30 * time.Second}
	params := url.Values{}
	if teamID != "" {
		params.Set("teamId", teamID)
	}
	headers := map[string]string{"Authorization": "Bearer " + vercelToken}
	envResp, status, err := vercelRequest(r.Context(), client, http.MethodGet, "https://api.vercel.com/v9/projects/"+projectID+"/env", params, headers, nil)
	if err != nil || status != http.StatusOK {
		writeJSON(w, statusOr(status, http.StatusInternalServerError), map[string]any{"detail": "获取环境变量失败"})
		return
	}
	envs, _ := envResp["envs"].([]any)
	existingEnvID := findEnvID(envs, "DS2API_CONFIG_JSON")
	if existingEnvID != "" {
		_, status, err = vercelRequest(r.Context(), client, http.MethodPatch, "https://api.vercel.com/v9/projects/"+projectID+"/env/"+existingEnvID, params, headers, map[string]any{"value": cfgB64})
	} else {
		_, status, err = vercelRequest(r.Context(), client, http.MethodPost, "https://api.vercel.com/v10/projects/"+projectID+"/env", params, headers, map[string]any{"key": "DS2API_CONFIG_JSON", "value": cfgB64, "type": "encrypted", "target": []string{"production", "preview"}})
	}
	if err != nil || (status != http.StatusOK && status != http.StatusCreated) {
		writeJSON(w, statusOr(status, http.StatusInternalServerError), map[string]any{"detail": "更新环境变量失败"})
		return
	}
	savedCreds := []string{}
	if saveCreds && !usePreconfig {
		creds := [][2]string{{"VERCEL_TOKEN", vercelToken}, {"VERCEL_PROJECT_ID", projectID}}
		if teamID != "" {
			creds = append(creds, [2]string{"VERCEL_TEAM_ID", teamID})
		}
		for _, kv := range creds {
			id := findEnvID(envs, kv[0])
			if id != "" {
				_, status, _ = vercelRequest(r.Context(), client, http.MethodPatch, "https://api.vercel.com/v9/projects/"+projectID+"/env/"+id, params, headers, map[string]any{"value": kv[1]})
			} else {
				_, status, _ = vercelRequest(r.Context(), client, http.MethodPost, "https://api.vercel.com/v10/projects/"+projectID+"/env", params, headers, map[string]any{"key": kv[0], "value": kv[1], "type": "encrypted", "target": []string{"production", "preview"}})
			}
			if status == http.StatusOK || status == http.StatusCreated {
				savedCreds = append(savedCreds, kv[0])
			}
		}
	}
	projectResp, status, _ := vercelRequest(r.Context(), client, http.MethodGet, "https://api.vercel.com/v9/projects/"+projectID, params, headers, nil)
	manual := true
	deployURL := ""
	if status == http.StatusOK {
		if link, ok := projectResp["link"].(map[string]any); ok {
			if linkType, _ := link["type"].(string); linkType == "github" {
				repoID := intFrom(link["repoId"])
				ref, _ := link["productionBranch"].(string)
				if ref == "" {
					ref = "main"
				}
				depResp, depStatus, _ := vercelRequest(r.Context(), client, http.MethodPost, "https://api.vercel.com/v13/deployments", params, headers, map[string]any{"name": projectID, "project": projectID, "target": "production", "gitSource": map[string]any{"type": "github", "repoId": repoID, "ref": ref}})
				if depStatus == http.StatusOK || depStatus == http.StatusCreated {
					deployURL, _ = depResp["url"].(string)
					manual = false
				}
			}
		}
	}
	h.Store.SetVercelSync(h.computeSyncHash(), time.Now().Unix())
	result := map[string]any{"success": true, "validated_accounts": validated}
	if manual {
		result["message"] = "配置已同步到 Vercel，请手动触发重新部署"
		result["manual_deploy_required"] = true
	} else {
		result["message"] = "配置已同步，正在重新部署..."
		result["deployment_url"] = deployURL
	}
	if len(failed) > 0 {
		result["failed_accounts"] = failed
	}
	if len(savedCreds) > 0 {
		result["saved_credentials"] = savedCreds
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) vercelStatus(w http.ResponseWriter, _ *http.Request) {
	snap := h.Store.Snapshot()
	current := h.computeSyncHash()
	synced := snap.VercelSyncHash != "" && snap.VercelSyncHash == current
	writeJSON(w, http.StatusOK, map[string]any{"synced": synced, "last_sync_time": nilIfZero(snap.VercelSyncTime), "has_synced_before": snap.VercelSyncHash != ""})
}

func vercelRequest(ctx context.Context, client *http.Client, method, endpoint string, params url.Values, headers map[string]string, body any) (map[string]any, int, error) {
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}
	var reader io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return nil, 0, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	parsed := map[string]any{}
	_ = json.Unmarshal(b, &parsed)
	if len(parsed) == 0 {
		parsed["raw"] = string(b)
	}
	return parsed, resp.StatusCode, nil
}

func findEnvID(envs []any, key string) string {
	for _, item := range envs {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if k, _ := m["key"].(string); k == key {
			id, _ := m["id"].(string)
			return id
		}
	}
	return ""
}
