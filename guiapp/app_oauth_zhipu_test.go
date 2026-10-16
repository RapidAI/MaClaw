package guiapp

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

// zhipuCtxCancelled reports whether ctx has been cancelled without blocking.
func zhipuCtxCancelled(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	default:
		return false
	}
}

// The stored-flow cancel: Start published a flow the frontend has not picked
// up with Wait yet. Cancel must drop it, clear the owned slot, and abort the
// flow context so a late Wait finds nothing.
func TestCancelZhipuCodingOAuthDropsStoredFlow(t *testing.T) {
	app := &App{}
	ctx, cancel := context.WithCancel(context.Background())
	app.oauthGeneration = 1
	app.oauthCancel = cancel
	app.zhipuOwnedGen = 1
	app.zhipuLogin = &zhipuLoginFlow{ctx: ctx, generation: 1, finish: cancel}

	app.CancelZhipuCodingOAuth()

	if app.zhipuLogin != nil {
		t.Error("zhipuLogin not cleared")
	}
	if app.zhipuOwnedGen != 0 {
		t.Errorf("zhipuOwnedGen = %d, want 0", app.zhipuOwnedGen)
	}
	if !zhipuCtxCancelled(ctx) {
		t.Error("flow context not cancelled")
	}
}

// The Wait-owned cancel: Wait already popped the flow and is polling. Cancel
// must abort the global flow context and bump the generation so the late
// claim in Wait cannot persist a cancelled login's key.
func TestCancelZhipuCodingOAuthAbortsWaitOwnedFlow(t *testing.T) {
	app := &App{}
	ctx, cancel := context.WithCancel(context.Background())
	app.oauthGeneration = 1
	app.oauthCancel = cancel
	app.zhipuOwnedGen = 1
	app.zhipuLogin = nil // Wait owns the flow now

	app.CancelZhipuCodingOAuth()

	if app.oauthGeneration != 2 {
		t.Errorf("oauthGeneration = %d, want bumped to 2", app.oauthGeneration)
	}
	if app.oauthCancel != nil {
		t.Error("oauthCancel not cleared")
	}
	if app.zhipuOwnedGen != 0 {
		t.Errorf("zhipuOwnedGen = %d, want 0", app.zhipuOwnedGen)
	}
	if !zhipuCtxCancelled(ctx) {
		t.Error("global flow context not cancelled")
	}
}

// A flow left over from a superseded start must not hide the newer start's
// slot: Cancel drops the stale flow and aborts the in-flight newer login.
func TestCancelZhipuCodingOAuthDropsStaleFlowWithoutMaskingNewStart(t *testing.T) {
	app := &App{}
	staleCtx, staleCancel := context.WithCancel(context.Background())
	startCtx, startCancel := context.WithCancel(context.Background())
	// Start2 bumped the generation and claimed the slot; Start1's flow is
	// still stored under the old generation.
	app.oauthGeneration = 2
	app.oauthCancel = startCancel
	app.zhipuOwnedGen = 2
	app.zhipuLogin = &zhipuLoginFlow{ctx: staleCtx, generation: 1, finish: staleCancel}

	app.CancelZhipuCodingOAuth()

	if app.zhipuLogin != nil {
		t.Error("stale flow not dropped")
	}
	if app.oauthGeneration != 3 {
		t.Errorf("oauthGeneration = %d, want bumped to 3", app.oauthGeneration)
	}
	if !zhipuCtxCancelled(startCtx) {
		t.Error("in-flight newer start not cancelled")
	}
	if app.zhipuOwnedGen != 0 {
		t.Errorf("zhipuOwnedGen = %d, want 0", app.zhipuOwnedGen)
	}
}

// Cancelling with nothing in flight must be a no-op.
func TestCancelZhipuCodingOAuthNoopWhenIdle(t *testing.T) {
	app := &App{}
	app.CancelZhipuCodingOAuth()
	if app.oauthGeneration != 0 || app.zhipuOwnedGen != 0 || app.zhipuLogin != nil {
		t.Fatalf("idle cancel mutated state: gen=%d ownedGen=%d login=%v",
			app.oauthGeneration, app.zhipuOwnedGen, app.zhipuLogin)
	}
}

// Waiting without a Start must fail fast instead of blocking forever.
func TestWaitZhipuCodingOAuthWithoutStartFails(t *testing.T) {
	app := &App{}
	if _, err := app.WaitZhipuCodingOAuth(); err == nil || !strings.Contains(err.Error(), "没有正在进行的智谱登录") {
		t.Fatalf("WaitZhipuCodingOAuth err = %v, want 没有正在进行的智谱登录", err)
	}
}

func newZhipuLoginTestApp(t *testing.T, providers []corelib.MaclawLLMProvider) *App {
	t.Helper()
	tmpHome := t.TempDir()
	t.Setenv("USERPROFILE", tmpHome)
	t.Setenv("HOME", tmpHome)
	app := &App{testHomeDir: tmpHome}
	if err := app.SaveConfig(corelib.AppConfig{
		MaclawLLMProviders: providers,
	}); err != nil {
		t.Fatalf("SaveConfig() error = %v", err)
	}
	return app
}

func findZhipuProvider(t *testing.T, providers []corelib.MaclawLLMProvider) *corelib.MaclawLLMProvider {
	t.Helper()
	for i := range providers {
		if corelib.IsZhipuCodingProviderName(providers[i].Name) {
			return &providers[i]
		}
	}
	t.Fatalf("智谱编程 provider not in list: %+v", providers)
	return nil
}

func TestSaveZhipuCodingLoginFillsKeyAndKeepsConfig(t *testing.T) {
	app := newZhipuLoginTestApp(t, []corelib.MaclawLLMProvider{
		{Name: "智谱编程", URL: "https://open.bigmodel.cn/api/anthropic", Model: "glm-5.3-flash", Protocol: "anthropic", ContextLength: 400000},
		{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash", Key: "deepseek-key"},
	})
	name, err := app.saveZhipuCodingLogin("key-id.key-secret")
	if err != nil {
		t.Fatalf("saveZhipuCodingLogin() error = %v", err)
	}
	if name != "智谱编程" {
		t.Errorf("matched provider name = %q, want 智谱编程", name)
	}
	data := app.GetMaclawLLMProviders()
	zhipu := findZhipuProvider(t, data.Providers)
	if zhipu.Key != "key-id.key-secret" {
		t.Errorf("Key = %q, want resolved coding-plan key", zhipu.Key)
	}
	if zhipu.ConnectionTestPassed {
		t.Error("fresh login must not count as connection-tested")
	}
	// A login must not rewrite fields the user configured.
	if zhipu.Model != "glm-5.3-flash" || zhipu.URL != "https://open.bigmodel.cn/api/anthropic" || zhipu.Protocol != "anthropic" {
		t.Errorf("existing config changed: %+v", zhipu)
	}
	for _, provider := range data.Providers {
		if provider.Name == "DeepSeek" && provider.Key != "deepseek-key" {
			t.Errorf("DeepSeek key changed: %+v", provider)
		}
	}
}

func TestSaveZhipuCodingLoginBackfillsPresetDefaults(t *testing.T) {
	app := newZhipuLoginTestApp(t, []corelib.MaclawLLMProvider{
		{Name: "智谱编程"},
	})
	if _, err := app.saveZhipuCodingLogin("k.s"); err != nil {
		t.Fatalf("saveZhipuCodingLogin() error = %v", err)
	}
	zhipu := findZhipuProvider(t, app.GetMaclawLLMProviders().Providers)
	if zhipu.URL != "https://open.bigmodel.cn/api/anthropic" {
		t.Errorf("URL = %q, want preset anthropic endpoint", zhipu.URL)
	}
	if zhipu.Protocol != "anthropic" {
		t.Errorf("Protocol = %q, want anthropic", zhipu.Protocol)
	}
	if zhipu.Model != zhipuCodingDefaultModel {
		t.Errorf("Model = %q, want %q", zhipu.Model, zhipuCodingDefaultModel)
	}
	if zhipu.ContextLength != 400000 {
		t.Errorf("ContextLength = %d, want 400000", zhipu.ContextLength)
	}
}

func TestSaveZhipuCodingLoginLandsOnSynthesizedDefault(t *testing.T) {
	// GetMaclawLLMProviders always ensures the built-in presets exist, so a
	// login lands on the synthesized 智谱编程 default even when the user never
	// saved that provider explicitly.
	app := newZhipuLoginTestApp(t, []corelib.MaclawLLMProvider{
		{Name: "DeepSeek", URL: "https://api.deepseek.com/v1", Model: "deepseek-v4-flash"},
	})
	if _, err := app.saveZhipuCodingLogin("k.s"); err != nil {
		t.Fatalf("saveZhipuCodingLogin() error = %v", err)
	}
	zhipu := findZhipuProvider(t, app.GetMaclawLLMProviders().Providers)
	if zhipu.Key != "k.s" {
		t.Errorf("Key = %q, want k.s on the synthesized default provider", zhipu.Key)
	}
}

func TestSaveZhipuCodingLoginRejectsEmptyKey(t *testing.T) {
	app := newZhipuLoginTestApp(t, []corelib.MaclawLLMProvider{
		{Name: "智谱编程"},
	})
	if _, err := app.saveZhipuCodingLogin("   "); err == nil {
		t.Fatal("expected error for empty API key")
	}
	zhipu := findZhipuProvider(t, app.GetMaclawLLMProviders().Providers)
	if zhipu.Key != "" {
		t.Errorf("Key = %q, want unchanged on failure", zhipu.Key)
	}
}
