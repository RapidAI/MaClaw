package workbuddy

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestProfilesAndAllowlist(t *testing.T) {
	china, ok := ProfileByName(NameChina)
	if !ok || china.ChatURL != "https://copilot.tencent.com/v2" || china.Origin != "https://www.codebuddy.cn" {
		t.Fatalf("china profile = %#v", china)
	}
	global, ok := ProfileByName(NameGlobal)
	if !ok || global.ChatURL != "https://www.workbuddy.ai/v2" || global.DefaultModel != "gpt-5.4" {
		t.Fatalf("global profile = %#v", global)
	}
	if _, ok := ProfileByURL("https://copilot.tencent.com/v2/"); !ok {
		t.Fatal("domestic chat URL was not recognized")
	}
	if ModelsForURL("https://api.example.com/v1") != nil {
		t.Fatal("unrelated URL returned a catalog")
	}
	var sawGLM, sawGPT bool
	for _, spec := range ModelsForURL(china.ChatURL) {
		if spec.ID == "glm-5.3" {
			sawGLM = true
		}
	}
	for _, spec := range ModelsForURL(global.ChatURL) {
		if spec.ID == "gpt-5.4" {
			sawGPT = true
		}
	}
	if !sawGLM || !sawGPT {
		t.Fatalf("catalog missing defaults glm=%v gpt=%v", sawGLM, sawGPT)
	}
	if china.DefaultContext() != 1000000 || global.DefaultContext() != 272000 {
		t.Fatalf("context china=%d global=%d", china.DefaultContext(), global.DefaultContext())
	}
}

func TestApplyHeadersSkipsStaticAPIKey(t *testing.T) {
	staticKey := make(http.Header)
	ApplyHeaders(staticKey, corelib.MaclawLLMConfig{
		ProviderName: NameGlobal,
		URL:          GlobalProfile().ChatURL,
		Key:          "shared-key",
	})
	if staticKey.Get("X-Product") != "" || staticKey.Get("User-Agent") != "" || staticKey.Get("X-No-User-Id") != "" || staticKey.Get("X-No-Enterprise-Id") != "" || staticKey.Get("X-No-Department-Info") != "" {
		t.Fatalf("static key gained account headers: %v", staticKey)
	}

	login := make(http.Header)
	ApplyHeaders(login, corelib.MaclawLLMConfig{
		ProviderName: NameGlobal,
		URL:          GlobalProfile().ChatURL,
		AuthType:     "oauth",
	})
	if login.Get("X-Product") != Product || login.Get("User-Agent") != UserAgent || login.Get("X-No-User-Id") != "1" || login.Get("X-No-Enterprise-Id") != "1" || login.Get("X-No-Department-Info") != "1" {
		t.Fatalf("empty login headers = %v", login)
	}
}

func TestPrepareBodySkipsRewriteWhenAlreadyCompatible(t *testing.T) {
	original := []byte(`{"model":"glm-5.3","stream":true,"messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}]}`)
	got, stream, ok := PrepareBody(original)
	if !ok || !stream || string(got) != string(original) {
		t.Fatalf("rewrote compatible body: %s", got)
	}
}

func TestPrepareBodyFlattensObjectToolChoice(t *testing.T) {
	named, _, ok := PrepareBody([]byte(`{"model":"hy3","stream":true,"reasoning_effort":"high","messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`))
	if !ok {
		t.Fatal("named prepare failed")
	}
	var namedObj map[string]any
	if err := json.Unmarshal(named, &namedObj); err != nil {
		t.Fatal(err)
	}
	if namedObj["tool_choice"] != "required" {
		t.Fatalf("named tool_choice = %#v, want required", namedObj["tool_choice"])
	}
	flat, _, ok := PrepareBody([]byte(`{"model":"hy3","stream":true,"reasoning_effort":"high","messages":[{"role":"system","content":"s"}],"tool_choice":{"type":"function","name":"lookup"}}`))
	if !ok {
		t.Fatal("flat prepare failed")
	}
	var flatObj map[string]any
	if err := json.Unmarshal(flat, &flatObj); err != nil {
		t.Fatal(err)
	}
	if flatObj["tool_choice"] != "required" {
		t.Fatalf("flat tool_choice = %#v, want required", flatObj["tool_choice"])
	}
	other, _, ok := PrepareBody([]byte(`{"model":"hy3","stream":true,"reasoning_effort":"high","messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}],"tool_choice":{"type":"auto"}}`))
	if !ok {
		t.Fatal("typeless prepare failed")
	}
	var otherObj map[string]any
	if err := json.Unmarshal(other, &otherObj); err != nil {
		t.Fatal(err)
	}
	if otherObj["tool_choice"] != "auto" {
		t.Fatalf("unnamed tool_choice = %#v, want auto", otherObj["tool_choice"])
	}
	noneChoice, _, ok := PrepareBody([]byte(`{"model":"hy3","stream":true,"reasoning_effort":"high","messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}],"tool_choice":{"type":"none"}}`))
	if !ok {
		t.Fatal("none prepare failed")
	}
	var noneObj map[string]any
	if err := json.Unmarshal(noneChoice, &noneObj); err != nil {
		t.Fatal(err)
	}
	if noneObj["tool_choice"] != "none" {
		t.Fatalf("none tool_choice = %#v, want none", noneObj["tool_choice"])
	}
	kept := []byte(`{"model":"glm-5.3","stream":true,"messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}],"tool_choice":"auto"}`)
	got, _, ok := PrepareBody(kept)
	if !ok || string(got) != string(kept) {
		t.Fatalf("string tool_choice was rewritten: %s", got)
	}
	keptObject := []byte(`{"model":"deepseek-v4.1-flash","stream":true,"messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}],"tool_choice":{"type":"function","function":{"name":"lookup"}}}`)
	got, _, ok = PrepareBody(keptObject)
	if !ok || string(got) != string(keptObject) {
		t.Fatalf("non-hy3 object tool_choice was rewritten: %s", got)
	}
}

func TestMergeCatalogKeepsAllowlistAndAppendsNewModels(t *testing.T) {
	merged := MergeCatalog("codebuddy", []UpstreamModel{
		{ID: "glm-5.3", Name: "GLM Live", MaxInputTokens: 111, MaxOutputTokens: 222},
		{ID: "brand-new", Name: "Brand New"},
	})
	if len(merged) < 2 || merged[0].ID == "brand-new" {
		t.Fatalf("catalog order = %#v", merged[:2])
	}
	var sawLive, sawNew bool
	for _, spec := range merged {
		if spec.ID == "glm-5.3" {
			sawLive = spec.Name == "GLM Live" && spec.ContextLength == 111 && spec.MaxOutput == 222
		}
		if spec.ID == "brand-new" && spec.Name == "Brand New" && spec.MaxOutput == 8192 {
			sawNew = true
		}
	}
	if !sawLive || !sawNew {
		t.Fatalf("merged catalog missing overlay live=%v new=%v", sawLive, sawNew)
	}
}

func TestPrepareBodyAddsSystemAndForcesStream(t *testing.T) {
	body, originalStream, ok := PrepareBody([]byte(`{"model":"hy3","stream":false,"messages":[{"role":"user","content":"You are Claude Code, Anthropic's official CLI for Claude."}]}`))
	if !ok || originalStream {
		t.Fatalf("prepare ok=%v originalStream=%v", ok, originalStream)
	}
	text := string(body)
	if !strings.Contains(text, `"stream":true`) || !strings.Contains(text, defaultSystemPrompt) {
		t.Fatalf("body = %s", text)
	}
	if strings.Contains(text, "Anthropic's official CLI for Claude.") && !strings.Contains(text, "official CLI tool") {
		t.Fatalf("template was not rewritten: %s", text)
	}
	if !strings.Contains(text, `"reasoning_effort":"high"`) {
		t.Fatalf("hy3 thinking was not forced: %s", text)
	}
}

func TestTransportAggregatesNonStreamAndPassesSSE(t *testing.T) {
	var sawStream, sawMarker, sawUser, sawProduct bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		sawStream = strings.Contains(string(raw), `"stream":true`)
		sawMarker = r.Header.Get(MarkerHeader) != ""
		sawUser = r.Header.Get("X-User-Id") == "user-1"
		sawProduct = r.Header.Get("X-Product") == Product
		if strings.Contains(r.Header.Get("Accept"), "text/event-stream") {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\ndata: [DONE]\n")
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"id\":\"c1\",\"model\":\"glm-5.3\",\"choices\":[{\"delta\":{\"role\":\"assistant\",\"content\":\"你\"}}]}\n\n"+
			"data: {\"choices\":[{\"delta\":{\"content\":\"好\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n")
	}))
	defer upstream.Close()

	cfg := corelib.MaclawLLMConfig{
		ProviderName:          NameChina,
		URL:                   ChinaProfile().ChatURL,
		WorkBuddyUserID:       "user-1",
		WorkBuddyEnterpriseID: "ent-1",
		WorkBuddyOrigin:       ChinaProfile().Origin,
	}
	client := WrapClient(upstream.Client())
	req, err := http.NewRequest(http.MethodPost, upstream.URL+"/v2/chat/completions", strings.NewReader(`{"model":"glm-5.3","stream":false,"messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ApplyHeaders(req.Header, cfg)
	req.Header.Set(MarkerHeader, "1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !sawStream || sawMarker || !sawUser || !sawProduct {
		t.Fatalf("upstream flags stream=%v marker=%v user=%v product=%v", sawStream, sawMarker, sawUser, sawProduct)
	}
	if !strings.Contains(string(body), `"content":"你好"`) || strings.Contains(string(body), "data:") {
		t.Fatalf("aggregated body = %s", body)
	}

	streamReq, err := http.NewRequest(http.MethodPost, upstream.URL+"/v2/chat/completions", strings.NewReader(`{"model":"glm-5.3","stream":true,"messages":[{"role":"system","content":"s"},{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	streamReq.Header.Set("Accept", "text/event-stream")
	streamReq.Header.Set(MarkerHeader, "1")
	streamResp, err := client.Do(streamReq)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResp.Body.Close()
	streamBody, _ := io.ReadAll(streamResp.Body)
	if !strings.Contains(string(streamBody), "data:") {
		t.Fatalf("stream body = %s", streamBody)
	}
}

func TestRunLoginPollsUntilToken(t *testing.T) {
	var polls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/auth/state"):
			_, _ = io.WriteString(w, `{"code":0,"data":{"state":"st","authUrl":"https://example.test/login"}}`)
		case strings.Contains(r.URL.Path, "/auth/token"):
			polls++
			if polls < 2 {
				_, _ = io.WriteString(w, `{"code":11217,"msg":"login ing"}`)
				return
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"accessToken":"access","refreshToken":"refresh","expiresIn":3600,"domain":"d"}}`)
		case strings.Contains(r.URL.Path, "/login/account"):
			_, _ = io.WriteString(w, `{"code":0,"data":{"uid":"u1","enterpriseId":"e1","nickname":"Ada"}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	profile := ChinaProfile()
	profile.APIRoot = srv.URL
	opened := ""
	prev := openBrowser
	openBrowser = func(raw string) error {
		opened = raw
		return nil
	}
	t.Cleanup(func() { openBrowser = prev })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cred, err := RunLogin(ctx, profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	if opened != "https://example.test/login" {
		t.Fatalf("opened %q", opened)
	}
	if cred.AccessToken != "access" || cred.RefreshToken != "refresh" || cred.UserID != "u1" || cred.EnterpriseID != "e1" {
		t.Fatalf("cred = %#v", cred)
	}
	if cred.ExpiresAt <= time.Now().Unix() {
		t.Fatalf("expiresAt = %d", cred.ExpiresAt)
	}
}

func TestRunLoginKeepsPollingOnUnauthorized(t *testing.T) {
	var polls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "/auth/state"):
			_, _ = io.WriteString(w, `{"code":0,"data":{"state":"st","authUrl":"https://example.test/login"}}`)
		case strings.Contains(r.URL.Path, "/auth/token"):
			polls++
			if polls == 1 {
				http.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}
			_, _ = io.WriteString(w, `{"code":0,"data":{"accessToken":"access","refreshToken":"refresh","expiresIn":60}}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	profile := ChinaProfile()
	profile.APIRoot = srv.URL
	prev := openBrowser
	openBrowser = func(string) error { return nil }
	t.Cleanup(func() { openBrowser = prev })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cred, err := RunLogin(ctx, profile, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cred.AccessToken != "access" || cred.UserID != "" {
		t.Fatalf("cred = %#v", cred)
	}
}

func TestFetchModelsReadsLiveCatalog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer access" || r.Header.Get("X-Product") != Product {
			http.Error(w, "missing auth", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":0,"data":{"models":[{"id":"brand-new","name":"Brand New","maxInputTokens":10,"maxOutputTokens":20}]}}`)
	}))
	defer srv.Close()
	profile := GlobalProfile()
	profile.APIRoot = srv.URL
	models, err := FetchModels(context.Background(), profile, AccountCredential{AccessToken: "access", UserID: "u"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ID != "brand-new" || models[0].MaxOutputTokens != 20 {
		t.Fatalf("models = %#v", models)
	}
}

func TestRunLoginFailsFastOnHTTPStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/auth/state") {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"code":0,"data":{"state":"st","authUrl":"https://example.test/login"}}`)
			return
		}
		http.Error(w, "missing", http.StatusNotFound)
	}))
	defer srv.Close()
	profile := GlobalProfile()
	profile.APIRoot = srv.URL
	prev := openBrowser
	openBrowser = func(string) error { return nil }
	t.Cleanup(func() { openBrowser = prev })

	start := time.Now()
	_, err := RunLogin(context.Background(), profile, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 404") {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("login waited %s on HTTP 404", time.Since(start))
	}
}

func TestTransportSurfacesBusinessError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get(MarkerHeader) != "" {
			t.Errorf("internal marker reached upstream")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"code":11128,"msg":"first message is not system prompt"}`)
	}))
	defer upstream.Close()
	client := WrapClient(upstream.Client())
	req, err := http.NewRequest(http.MethodPost, upstream.URL+"/v2/chat/completions", strings.NewReader(`{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(MarkerHeader, "1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "first message is not system prompt") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}

func TestTransportSurfacesUpstreamStreamError(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "data: {\"error\":{\"message\":\"quota exceeded\"}}\n\ndata: [DONE]\n")
	}))
	defer upstream.Close()
	client := WrapClient(upstream.Client())
	req, err := http.NewRequest(http.MethodPost, upstream.URL+"/v2/chat/completions", strings.NewReader(`{"model":"glm-5.3","messages":[{"role":"user","content":"hi"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(MarkerHeader, "1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusBadGateway || !strings.Contains(string(body), "quota exceeded") {
		t.Fatalf("status=%d body=%s", resp.StatusCode, body)
	}
}
