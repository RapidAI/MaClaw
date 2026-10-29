package qoder

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/oauth"
	"github.com/RapidAI/CodeClaw/corelib/qoder/cosy"
)

// Live probe for the international agent_router rejection. Not part of the
// default suite: it talks to the real gateway with the local credential.
func TestLiveGlobalChatBodies(t *testing.T) {
	if testing.Short() {
		t.Skip("live")
	}
	store := oauth.NewFileCredentialStore(oauth.DefaultCredentialStorePath())
	cred, err := store.Read(StoreGlobal)
	if err != nil || cred == nil || strings.TrimSpace(cred.AccessToken) == "" {
		t.Fatalf("global credential: %v", err)
	}
	profile := GlobalProfile()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	models, def, err := ListModels(ctx, profile, cred.UserID, cred.AccessToken)
	if err != nil {
		t.Logf("catalog: %v", err)
	} else {
		keys := make([]string, 0, 8)
		for _, m := range models {
			if len(keys) == 8 {
				break
			}
			keys = append(keys, m.Key)
		}
		t.Logf("catalog n=%d default=%s sample=%v", len(models), def, keys)
	}

	rawEntry, err := rawCatalogEntry(ctx, profile, cred.UserID, cred.AccessToken, "dfmodel")
	if err != nil {
		t.Fatalf("raw catalog: %v", err)
	}
	t.Logf("dfmodel raw len=%d", len(rawEntry))
	status, _, snippet, err := postSignedChat(ctx, profile, cred.UserID, cred.AccessToken, ClientVersion, "dfmodel", enrichedWithRawConfig("dfmodel", rawEntry))
	t.Logf("global-raw status=%d err=%v snippet=%s", status, err, snippet)
}

func modelsIn(snippet string) []string {
	var out []string
	rest := snippet
	for {
		i := strings.Index(rest, `"model":"`)
		if i < 0 {
			break
		}
		rest = rest[i+len(`"model":"`):]
		end := strings.Index(rest, `"`)
		if end < 0 {
			break
		}
		out = append(out, rest[:end])
		rest = rest[end+1:]
	}
	return out
}

func enrichedProbeBody(model string, catalog Model) string {
	sessionID := newUUID()
	recordID := newUUID()
	modelConfig := map[string]any{"key": model, "source": "system", "is_reasoning": catalog.Reasoning}
	if catalog.MaxInput > 0 {
		modelConfig["max_input_tokens"] = catalog.MaxInput
	}
	if catalog.MaxOutput > 0 {
		modelConfig["max_output_tokens"] = catalog.MaxOutput
	}
	body := map[string]any{
		"model":          model,
		"stream":         true,
		"max_tokens":     16,
		"request_id":     newUUID(),
		"request_set_id": recordID,
		"chat_record_id": recordID,
		"session_id":     sessionID,
		"chat_task":      "FREE_INPUT",
		"is_reply":       true,
		"is_retry":       false,
		"source":         1,
		"version":        "3",
		"session_type":   "qodercli",
		"agent_id":       "agent_common",
		"task_id":        "common",
		"messages":       []ChatMessage{{Role: "user", Content: "reply with ok"}},
		"model_config":   modelConfig,
		"business": map[string]any{
			"product":  "cli",
			"version":  "1.0.0",
			"type":     "agent",
			"scene":    "cli",
			"stage":    "start",
			"id":       newUUID(),
			"begin_at": time.Now().UnixMilli(),
		},
		"data_policy_agreed": true,
	}
	raw, err := marshalAgentChatJSON(body)
	if err != nil {
		return ""
	}
	return string(raw)
}

func enrichedWithRawConfig(model string, rawEntry []byte) string {
	sessionID := newUUID()
	recordID := newUUID()
	var modelConfig any
	if jsonUnmarshal(rawEntry, &modelConfig) != nil {
		modelConfig = map[string]any{"key": model, "source": "system"}
	}
	body := map[string]any{
		"model":              model,
		"stream":             true,
		"max_tokens":         16,
		"request_id":         newUUID(),
		"request_set_id":     recordID,
		"chat_record_id":     recordID,
		"session_id":         sessionID,
		"chat_task":          "FREE_INPUT",
		"is_reply":           true,
		"is_retry":           false,
		"source":             1,
		"version":            "3",
		"session_type":       "qodercli",
		"agent_id":           "agent_common",
		"task_id":            "common",
		"messages":           []ChatMessage{{Role: "user", Content: "reply with ok"}},
		"model_config":       modelConfig,
		"data_policy_agreed": true,
		"business": map[string]any{
			"product": "cli", "version": "1.0.0", "type": "agent", "scene": "cli",
			"stage": "start", "id": newUUID(), "begin_at": time.Now().UnixMilli(),
		},
	}
	raw, err := marshalAgentChatJSON(body)
	if err != nil {
		return ""
	}
	return string(raw)
}

func jsonUnmarshal(raw []byte, dest any) error {
	return json.Unmarshal(raw, dest)
}

func rawCatalogEntry(ctx context.Context, profile Profile, uid, token, key string) ([]byte, error) {
	signed, err := cosy.New(MachineID(), ClientVersion, strings.TrimSpace(uid))
	if err != nil {
		return nil, err
	}
	defer signed.Close()
	if err := signed.RefreshAuthFields(strings.TrimSpace(token)); err != nil {
		return nil, err
	}
	prepared, err := signed.PrepareRequest(strings.TrimRight(profile.InferBase, "/"), "/algo/api/v2/model/list", http.MethodGet, "auth", "", "")
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, prepared.URL, nil)
	if err != nil {
		return nil, err
	}
	for k, value := range prepared.Headers {
		req.Header.Set(k, value)
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := oauth.DoNoFollow(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	var scenes map[string][]json.RawMessage
	if err := json.Unmarshal(body, &scenes); err != nil {
		return nil, err
	}
	for _, entries := range scenes {
		for _, entry := range entries {
			var probe struct {
				Key      string `json:"key"`
				ModelKey string `json:"model_key"`
			}
			if json.Unmarshal(entry, &probe) != nil {
				continue
			}
			if probe.Key == key || probe.ModelKey == key {
				return entry, nil
			}
		}
	}
	return nil, fmt.Errorf("catalog entry %s missing", key)
}

func postSignedChat(ctx context.Context, profile Profile, uid, token, version, model, bodyJSON string) (int, string, string, error) {
	signing, err := cosy.New(MachineID(), version, strings.TrimSpace(uid))
	if err != nil {
		return 0, "", "", err
	}
	defer signing.Close()
	if err := signing.RefreshAuthFields(strings.TrimSpace(token)); err != nil {
		return 0, "", "", err
	}
	prepared, err := signing.PrepareInferRequest(strings.TrimRight(profile.ChatOrigin, "/"), bodyJSON, model, "system")
	if err != nil {
		return 0, "", "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, prepared.URL, strings.NewReader(prepared.Body))
	if err != nil {
		return 0, prepared.URL, "", err
	}
	for key, value := range prepared.Headers {
		req.Header.Set(key, value)
	}
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("User-Agent", "qoder/"+version)
	resp, err := oauth.DoNoFollowWithin(req, 20*time.Second, 30*time.Second)
	if err != nil {
		return 0, prepared.URL, "", err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 2500))
	text := strings.ReplaceAll(string(raw), "\n", " ")
	if len(text) > 900 {
		text = text[:900]
	}
	return resp.StatusCode, prepared.URL, text, nil
}

func redactURL(raw string) string {
	if i := strings.Index(raw, "?"); i >= 0 {
		return raw[:i] + "?" + fmt.Sprintf("qlen=%d", len(raw)-i)
	}
	return raw
}
