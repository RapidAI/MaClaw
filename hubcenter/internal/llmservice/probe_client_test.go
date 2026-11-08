package llmservice

import (
	"context"
	"net/http"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/llmpool"
	"github.com/RapidAI/CodeClaw/corelib/qoder"
	"github.com/RapidAI/CodeClaw/corelib/windsurf"
)

func TestMemberProbeConfigAndWrapMatchLocalProbe(t *testing.T) {
	base := &http.Client{}
	plain := MemberProbeConfig(corelib.MaclawLLMConfig{
		URL: "https://api.example.com/v1", Model: "m", TimeoutSec: 9,
	}, &llmpool.ProviderConfig{
		Name: "Plain", APIURL: "https://api.example.com/v1",
		MaxOutputTokensPerRequest: 32, AgentType: "code",
	})
	if plain.URL != "https://api.example.com/v1" || plain.TimeoutSec != 9 || plain.MemberOutputTokenCap != 32 {
		t.Fatalf("plain=%#v", plain)
	}
	if got := WrapMemberProbeClient(base, plain); got != base {
		t.Fatal("plain client was wrapped")
	}
	if got := WrapMemberProbeClient(nil, plain); got != http.DefaultClient {
		t.Fatal("nil client was not the default client")
	}

	qoderProvider := &llmpool.ProviderConfig{
		Name: qoder.NameCN, APIURL: qoder.ChatBase, APIKey: "access-token",
		AuthKind: llmpool.ProviderAuthQoder, QoderEdition: "china",
		QoderUserID: "uid-1", QoderMachineID: "machine-1",
		UpstreamTimeoutSec: 3, MaxOutputTokensPerRequest: 16384,
	}
	want, ok := QoderMaclawConfig(qoderProvider, "qwen")
	if !ok {
		t.Fatal("qoder config was not recognized")
	}
	got := MemberProbeConfig(corelib.MaclawLLMConfig{
		URL: "http://ignored", Model: "qwen", TimeoutSec: 15, Key: "caller-key",
	}, qoderProvider)
	if got.URL != want.URL || got.ProviderName != want.ProviderName || got.QoderUserID != "uid-1" || got.TimeoutSec != 15 || got.MemberOutputTokenCap != 16384 {
		t.Fatalf("qoder cfg=%#v want url=%s name=%s", got, want.URL, want.ProviderName)
	}
	wrapped := WrapMemberProbeClient(base, got)
	if wrapped == base {
		t.Fatal("qoder client was not wrapped")
	}
	if _, ok := wrapped.Transport.(*qoder.Transport); !ok {
		t.Fatalf("qoder transport=%T", wrapped.Transport)
	}

	devin := MemberProbeConfig(corelib.MaclawLLMConfig{
		URL: "http://127.0.0.1:9/v1", Model: "swe", TimeoutSec: 4,
	}, &llmpool.ProviderConfig{Name: "Devin", APIURL: "http://127.0.0.1:9/v1"})
	if devin.ProviderName != "Devin" || devin.TimeoutSec != 4 {
		t.Fatalf("devin cfg=%#v", devin)
	}
	wrapped = WrapMemberProbeClient(base, devin)
	if _, ok := wrapped.Transport.(*windsurf.Transport); !ok {
		t.Fatalf("devin transport=%T", wrapped.Transport)
	}
}

func TestOpenCodeProbeStreamRequestSetsAccept(t *testing.T) {
	provider := &llmpool.ProviderConfig{
		APIURL: "https://opencode.ai/zen/go/v1", APIKey: "sk-live", WireAPI: "responses",
	}
	cfg := corelib.MaclawLLMConfig{
		URL: provider.APIURL, Key: provider.APIKey, Model: "muse-spark-1.2-contributor-free", WireAPI: "responses",
	}
	req, _, kind, err := openCodeProbeStreamRequest(context.Background(), cfg, provider, ProviderChatTest{Model: cfg.Model}, cfg.Model)
	if err != nil {
		t.Fatal(err)
	}
	if req != nil && req.Body != nil {
		_ = req.Body.Close()
	}
	if kind != "responses" || req == nil || req.Header.Get("Accept") != "text/event-stream" {
		t.Fatalf("responses kind=%s accept=%q", kind, headerAccept(req))
	}

	chatProvider := &llmpool.ProviderConfig{APIURL: "https://opencode.ai/zen/go/v1", APIKey: "sk-live"}
	chatCfg := corelib.MaclawLLMConfig{URL: chatProvider.APIURL, Key: "sk-live", Model: "space-bunny-free"}
	req, _, kind, err = openCodeProbeStreamRequest(context.Background(), chatCfg, chatProvider, ProviderChatTest{Model: chatCfg.Model}, chatCfg.Model)
	if err != nil {
		t.Fatal(err)
	}
	if req != nil && req.Body != nil {
		_ = req.Body.Close()
	}
	if kind != "openai" || req == nil || req.Header.Get("Accept") != "text/event-stream" {
		t.Fatalf("chat kind=%s accept=%q", kind, headerAccept(req))
	}
}

func headerAccept(req *http.Request) string {
	if req == nil {
		return ""
	}
	return req.Header.Get("Accept")
}
