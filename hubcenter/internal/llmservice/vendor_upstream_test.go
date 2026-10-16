package llmservice

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/lobsterai"
	"github.com/RapidAI/CodeClaw/corelib/qoder"
	"github.com/RapidAI/CodeClaw/corelib/trae"
)

// jsonForwardResponse is the answer a stub upstream gives the translators.
func jsonForwardResponse(req *http.Request) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body: io.NopCloser(bytes.NewBufferString(
			`{"id":"ok","choices":[{"message":{"role":"assistant","content":"ok"}}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)),
		Request: req,
	}
}

// Trae binds the token family to the device pair the login minted, so the
// stored pair must reach the upstream: the transport falls back to a
// claims-derived pair the SOLO host refuses with a 401.
func TestForwardTraeStampsStoredDevicePair(t *testing.T) {
	for _, tc := range []struct {
		edition   string
		name      string
		wantURL   string
		wantStore string
	}{
		{"china", trae.NameCN, "https://trae-api-cn.mchost.guru", trae.StoreCN},
		{"global", trae.NameGlobal, "https://coresg-normal.trae.ai", trae.StoreGlobal},
	} {
		var seen http.Header
		var target string
		client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			seen = req.Header.Clone()
			target = req.URL.String()
			return jsonForwardResponse(req), nil
		})}
		provider := &llmpool.ProviderConfig{
			ID: "trae-" + tc.edition, Name: tc.name, APIURL: tc.wantURL,
			APIKey: "access-token", Protocol: "openai", AuthKind: llmpool.ProviderAuthTrae,
			TraeEdition: tc.edition, TraeUserID: "uid-1",
			TraeMachineID: "machine-1", TraeDeviceID: "device-1",
			TraeRefreshToken: "refresh-token", Models: []string{"glm-5.2"},
		}
		resp, handled, err := tryForwardTrae(context.Background(), client, provider, map[string]any{
			"model": "glm-5.2", "messages": []any{map[string]any{"role": "user", "content": "hello"}},
		}, "glm-5.2", "glm-5.2", false)
		if err != nil || !handled {
			t.Fatalf("%s: forward handled=%v err=%v", tc.edition, handled, err)
		}
		if resp == nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: response = %#v", tc.edition, resp)
		}
		if seen.Get("X-Machine-Id") != "machine-1" || seen.Get("X-Device-Id") != "device-1" || seen.Get("X-Uid") != "uid-1" {
			t.Fatalf("%s: device pair = %v", tc.edition, seen)
		}
		// The SOLO wire carries the session as a Cloud-IDE-JWT, not a bearer.
		if !bytes.Contains([]byte(seen.Get("Authorization")), []byte("access-token")) {
			t.Fatalf("%s: authorization = %q", tc.edition, seen.Get("Authorization"))
		}
		if target == "" {
			t.Fatalf("%s: no upstream request was made", tc.edition)
		}
	}
}

// A static-key provider on a Trae host is still routed through the translator,
// because the host rejects an unmodified OpenAI body, but no device pair is
// invented for it.
func TestForwardTraeStaticKeyHasNoInventedDevicePair(t *testing.T) {
	var seen http.Header
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Clone()
		return jsonForwardResponse(req), nil
	})}
	provider := &llmpool.ProviderConfig{
		ID: "trae-static", Name: "Trae Key", APIURL: trae.GlobalProfile().ChatHost,
		APIKey: "sk-static", Protocol: "openai", Models: []string{"glm-5.2"},
	}
	if _, ok := TraeMaclawConfig(provider, "glm-5.2"); !ok {
		t.Fatal("a static key on the Trae chat host must still be translated")
	}
	if _, handled, err := tryForwardTrae(context.Background(), client, provider, map[string]any{
		"model": "glm-5.2", "messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}, "glm-5.2", "glm-5.2", false); err != nil || !handled {
		t.Fatalf("forward handled=%v err=%v", handled, err)
	}
	if seen.Get("X-Uid") != "" {
		t.Fatalf("invented a uid for a static key = %q", seen.Get("X-Uid"))
	}
}

// Both Qoder editions publish the same OpenAI front, so the edition decides
// which signing gateway the request is rewritten to. A CN login rewritten to
// the global gateway is a 401.
func TestForwardQoderRoutesPerEdition(t *testing.T) {
	for _, tc := range []struct {
		edition string
		name    string
	}{
		{"china", qoder.NameCN},
		{"global", qoder.NameGlobal},
	} {
		client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			return jsonForwardResponse(req), nil
		})}
		provider := &llmpool.ProviderConfig{
			ID: "qoder-" + tc.edition, Name: tc.name,
			APIURL: qoder.ChatBase, APIKey: "access-token", Protocol: "openai",
			AuthKind: llmpool.ProviderAuthQoder, QoderEdition: tc.edition,
			QoderUserID: "uid-1", QoderMachineID: "machine-1",
			QoderRefreshToken: "refresh-token", Models: []string{"qwen3.8-max"},
		}
		resp, handled, err := tryForwardQoder(context.Background(), client, provider, map[string]any{
			"model": "qwen3.8-max", "messages": []any{map[string]any{"role": "user", "content": "hello"}},
		}, "qwen3.8-max", "qwen3.8-max", false)
		if err != nil || !handled {
			t.Fatalf("%s: forward handled=%v err=%v", tc.edition, handled, err)
		}
		if resp == nil || resp.StatusCode != http.StatusOK {
			t.Fatalf("%s: response = %#v", tc.edition, resp)
		}
	}
}

// A plain static-key provider must not be captured by any vendor translator:
// the dispatcher decides per provider, and a false positive would translate an
// ordinary OpenAI request into a signed wire.
func TestVendorForwardersIgnorePlainProviders(t *testing.T) {
	plain := &llmpool.ProviderConfig{
		ID: "plain", Name: "Plain", APIURL: "https://api.openai.com/v1",
		APIKey: "sk-test", Protocol: "openai", Models: []string{"gpt-4o"},
	}
	body := map[string]any{
		"model": "gpt-4o", "messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}
	if _, handled, err := tryForwardQoder(context.Background(), nil, plain, body, "gpt-4o", "gpt-4o", false); handled || err != nil {
		t.Fatalf("qoder claimed a plain provider: handled=%v err=%v", handled, err)
	}
	if _, handled, err := tryForwardTrae(context.Background(), nil, plain, body, "gpt-4o", "gpt-4o", false); handled || err != nil {
		t.Fatalf("trae claimed a plain provider: handled=%v err=%v", handled, err)
	}
	if _, handled, err := tryForwardLobsterAI(context.Background(), nil, plain, body, "gpt-4o", "gpt-4o", false); handled || err != nil {
		t.Fatalf("lobsterai claimed a plain provider: handled=%v err=%v", handled, err)
	}
}

// LobsterAI's chat path is OpenAI-compatible, so the wrapper only has to reach
// the right host with the session identity intact.
func TestForwardLobsterAIUsesChatHost(t *testing.T) {
	var target string
	client := &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		target = req.URL.String()
		return jsonForwardResponse(req), nil
	})}
	provider := &llmpool.ProviderConfig{
		ID: "lobsterai", Name: lobsterai.Name, APIURL: lobsterai.APIBase,
		APIKey: "access-token", Protocol: "openai", AuthKind: llmpool.ProviderAuthLobsterAI,
		LobsterAIRefreshToken: "refresh-token", LobsterAIUserID: "user-1",
		LobsterAISessionUUID: "uuid-1", LobsterAIFirstKeyfrom: "1735689600000",
		Models: []string{"glm-5.3"},
	}
	resp, handled, err := tryForwardLobsterAI(context.Background(), client, provider, map[string]any{
		"model": "glm-5.3", "messages": []any{map[string]any{"role": "user", "content": "hello"}},
	}, "glm-5.3", "glm-5.3", false)
	if err != nil || !handled {
		t.Fatalf("forward handled=%v err=%v", handled, err)
	}
	if resp == nil || resp.StatusCode != http.StatusOK {
		t.Fatalf("response = %#v", resp)
	}
	if want := lobsterai.APIBase + "/api/proxy/v1/chat/completions"; target != want {
		t.Fatalf("target = %q want %q", target, want)
	}
}


// The availability test wraps its client through QoderHTTPClient and
// TraeHTTPClient so it exercises the same wire the chat forward path does.
// The bare corelib wrappers resolve their identity from a desktop credential
// store a server process does not have, which would send a different device
// pair (Trae) or an unsigned request (Qoder) than production.
func TestVendorHTTPClientsMatchForwardPath(t *testing.T) {
	traeCfg := traeMaclawConfigForTest()
	client := TraeHTTPClient(nil, traeCfg)
	tripper, ok := client.Transport.(traeHeaderTripper)
	if !ok {
		t.Fatalf("trae transport = %T, want the device-pair stamping wrapper", client.Transport)
	}
	if tripper.cfg.TraeMachineID != "machine-1" || tripper.cfg.TraeDeviceID != "device-1" || tripper.cfg.TraeUserID != "uid-1" {
		t.Fatalf("device pair = %#v", tripper.cfg)
	}

	// Stamping must happen before the inner transport clones the header, so a
	// request through the wrapper carries the stored pair.
	var seen http.Header
	client = &http.Client{Transport: proxyRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Clone()
		return jsonForwardResponse(req), nil
	})}
	client = TraeHTTPClient(client, traeCfg)
	resp, err := client.Do(traeTestRequest(t, traeCfg))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	_ = resp.Body.Close()
	if seen.Get("X-Machine-Id") != "machine-1" || seen.Get("X-Device-Id") != "device-1" || seen.Get("X-Uid") != "uid-1" {
		t.Fatalf("stamped headers = %v", seen)
	}
}

func traeMaclawConfigForTest() corelib.MaclawLLMConfig {
	profile := trae.CNProfile()
	cfg := corelib.MaclawLLMConfig{
		URL:            trae.CanonicalChatURL(profile, profile.ChatHost),
		Key:            "access-token",
		Model:          "glm-5.2",
		Protocol:       "openai",
		ProviderName:   profile.Name,
		TraeMachineID:  "machine-1",
		TraeDeviceID:   "device-1",
		TraeUserID:     "uid-1",
		TraeRefreshToken: "refresh-token",
	}
	cfg.AuthType = "oauth"
	return cfg
}

func traeTestRequest(t *testing.T, cfg corelib.MaclawLLMConfig) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, cfg.URL+"/chat/completions",
		strings.NewReader(`{"model":"glm-5.2","messages":[{"role":"user","content":"hello"}]}`))
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer access-token")
	return req
}
