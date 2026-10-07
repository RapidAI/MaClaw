package llmservice

import (
	"context"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/llmpool"
)

func TestProviderServeWindowsSaveAndRoundTrip(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	windows := []llmpool.TokenBankShareWindow{{Days: []int{1, 2, 3, 4, 5}, Start: "22:00", End: "24:00"}}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:           "limited",
		Name:         "limited",
		APIURL:       "https://api.limited.example/v1",
		ServeWindows: windows,
	}); err != nil {
		t.Fatalf("AddProvider: %v", err)
	}
	got, err := svc.GetProvider(ctx, "limited")
	if err != nil || got == nil {
		t.Fatalf("get provider: %#v err=%v", got, err)
	}
	if len(got.ServeWindows) != 1 || got.ServeWindows[0].Start != "22:00" || got.ServeWindows[0].End != "24:00" {
		t.Fatalf("ServeWindows = %#v", got.ServeWindows)
	}
	// Mutating the returned copy must not leak into the stored registry.
	got.ServeWindows[0].Days[0] = 9
	fresh, err := svc.GetProvider(ctx, "limited")
	if err != nil || fresh == nil || len(fresh.ServeWindows) != 1 || len(fresh.ServeWindows[0].Days) != 5 {
		t.Fatalf("serve windows mutated through copy: %#v err=%v", fresh, err)
	}

	// An update that omits serve_windows keeps the stored windows; the editor
	// and the import API clear them only with an explicit [].
	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID:     "limited",
		Name:   "limited",
		APIURL: "https://api.limited.example/v1",
	}); err != nil {
		t.Fatalf("keep update: %v", err)
	}
	fresh, err = svc.GetProvider(ctx, "limited")
	if err != nil || fresh == nil || len(fresh.ServeWindows) != 1 {
		t.Fatalf("omitted serve_windows should be preserved, got %#v err=%v", fresh, err)
	}
	// An explicit [] clears: the editor owns the window list.
	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID:           "limited",
		Name:         "limited",
		APIURL:       "https://api.limited.example/v1",
		ServeWindows: []llmpool.TokenBankShareWindow{},
	}); err != nil {
		t.Fatalf("clear update: %v", err)
	}
	fresh, err = svc.GetProvider(ctx, "limited")
	if err != nil || fresh == nil || len(fresh.ServeWindows) != 0 {
		t.Fatalf("serve windows should be cleared, got %#v err=%v", fresh, err)
	}
}

func TestProviderServeWindowsRejectBrokenClock(t *testing.T) {
	ctx := context.Background()
	svc := NewService(&mockSystemSettings{})
	broken := []llmpool.TokenBankShareWindow{{Start: "25:00", End: "26:00"}}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{
		ID:           "broken",
		Name:         "broken",
		APIURL:       "https://api.broken.example/v1",
		ServeWindows: broken,
	}); err == nil || !strings.Contains(err.Error(), "serve_windows") {
		t.Fatalf("AddProvider err = %v, want serve_windows rejection", err)
	}
	if err := svc.AddProvider(ctx, llmpool.ProviderConfig{ID: "ok", Name: "ok", APIURL: "https://api.ok.example/v1"}); err != nil {
		t.Fatalf("AddProvider ok: %v", err)
	}
	if err := svc.UpdateProvider(ctx, llmpool.ProviderConfig{
		ID:           "ok",
		Name:         "ok",
		APIURL:       "https://api.ok.example/v1",
		ServeWindows: []llmpool.TokenBankShareWindow{{Start: "08:00", End: "08:00"}},
	}); err == nil || !strings.Contains(err.Error(), "serve_windows") {
		t.Fatalf("UpdateProvider err = %v, want serve_windows rejection", err)
	}
}

func TestAcceptLiveProviderSkipsClosedServeWindows(t *testing.T) {
	open := &llmpool.ProviderConfig{ID: "open"}
	if !acceptLiveProvider(open) {
		t.Fatal("a provider without windows stays live")
	}
	closed := &llmpool.ProviderConfig{ID: "closed", ServeWindows: []llmpool.TokenBankShareWindow{{Start: "99:99"}}}
	if acceptLiveProvider(closed) {
		t.Fatal("a provider outside its serve window is skipped")
	}
	// The dispatch diagnostic names the window when every member is closed.
	reg := &Registry{Providers: []llmpool.ProviderConfig{{
		ID:           "closed",
		Name:         "closed",
		APIURL:       "https://api.closed.example/v1",
		ArrayID:      "closed",
		ServeWindows: []llmpool.TokenBankShareWindow{{Start: "99:99"}},
	}}}
	if !arrayRouteServeWindowClosed(reg, "closed", "m") {
		t.Fatal("every member outside its serve window should report the window as closed")
	}
	reg.Providers[0].ServeWindows = []llmpool.TokenBankShareWindow{{Start: "00:00", End: "24:00"}}
	if arrayRouteServeWindowClosed(reg, "closed", "m") {
		t.Fatal("an always-open window should not report the array as closed")
	}
}
