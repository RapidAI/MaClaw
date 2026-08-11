package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
)

func TestLansengerBotProfileCRUDRedactsAndPreservesSecret(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	first, err := app.SaveLansengerBot(corelib.LansengerBotProfile{
		ID: "support", Name: "Support", Enabled: false, AppID: "app-1", AppSecret: "keep-secret",
		DocumentDirectories: []string{" docs ", "docs"}, GroupPolicy: "allow",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !first.SecretConfigured {
		t.Fatalf("unsafe bot view: %#v", first)
	}
	if got := first.DocumentDirectories; len(got) != 1 || !filepath.IsAbs(got[0]) || filepath.Base(got[0]) != "docs" || first.GroupPolicy != "allowlist" {
		t.Fatalf("profile was not normalized: %#v", first)
	}

	updated, err := app.SaveLansengerBot(corelib.LansengerBotProfile{ID: "support", Name: "Renamed", Enabled: false, AppID: "app-2"})
	if err != nil {
		t.Fatal(err)
	}
	if !updated.SecretConfigured || updated.Name != "Renamed" {
		t.Fatalf("update view = %#v", updated)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.LansengerBots) != 1 || cfg.LansengerBots[0].AppSecret != "keep-secret" {
		t.Fatalf("secret was not retained: %#v", cfg.LansengerBots)
	}
	bots, err := app.ListLansengerBots()
	if err != nil || len(bots) != 1 || bots[0].ID != "support" || !bots[0].SecretConfigured {
		t.Fatalf("list = %#v err=%v", bots, err)
	}
	encoded, err := json.Marshal(bots)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "keep-secret") {
		t.Fatalf("list leaked secret: %#v", bots)
	}
	if err := app.DeleteLansengerBot("support"); err != nil {
		t.Fatal(err)
	}
	bots, err = app.ListLansengerBots()
	if err != nil || len(bots) != 0 {
		t.Fatalf("delete list = %#v err=%v", bots, err)
	}
}

func TestSaveLansengerBotKeepsReplyCachePolicyOnItsOwnProfile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	supportCache := corelib.AnswerCacheConfig{Enabled: true, TTLDays: 7}
	first, err := app.SaveLansengerBot(corelib.LansengerBotProfile{
		ID: "support", AnswerCache: &supportCache,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := first.AnswerCache; !got.CanReuseAnswers() || got.TTLDays != 7 {
		t.Fatalf("saved cache policy = %#v", got)
	}
	salesCache := corelib.AnswerCacheConfig{Enabled: true, TTLDays: 0}
	second, err := app.SaveLansengerBot(corelib.LansengerBotProfile{
		ID: "sales", AnswerCache: &salesCache,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := second.AnswerCache; got.CanReuseAnswers() || got.TTLDays != 0 {
		t.Fatalf("second profile cache policy = %#v", got)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.LansengerBots) != 2 || cfg.LansengerBots[0].EffectiveAnswerCache().TTLDays != 7 || cfg.LansengerBots[1].EffectiveAnswerCache().TTLDays != 0 {
		t.Fatalf("bot cache policies were not stored independently: %#v", cfg.LansengerBots)
	}
}

func TestSaveLansengerBotPreservesCachePolicyWhenOlderCallerOmitsIt(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	policy := corelib.AnswerCacheConfig{Enabled: false, TTLDays: 7}
	if _, err := app.SaveLansengerBot(corelib.LansengerBotProfile{ID: "support", Name: "Support", AnswerCache: &policy}); err != nil {
		t.Fatal(err)
	}
	updated, err := app.SaveLansengerBot(corelib.LansengerBotProfile{ID: "support", Name: "Renamed"})
	if err != nil {
		t.Fatal(err)
	}
	if got := updated.AnswerCache; got.Enabled || got.TTLDays != 7 {
		t.Fatalf("cache policy was lost on partial bot save: %#v", got)
	}
}

func TestSaveLansengerBotNormalizesAndAuthorizesConfiguredDirectories(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	root := t.TempDir()
	work := filepath.Join(root, "work")
	docs := filepath.Join(root, "docs")
	assets := filepath.Join(root, "assets")
	for _, dir := range []string{work, docs, assets} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	saved, err := app.SaveLansengerBot(corelib.LansengerBotProfile{
		ID: "support", WorkingDirectory: work,
		DocumentDirectories: []string{docs, filepath.Join(docs, ".")},
		AllowedDirectories:  []string{assets},
	})
	if err != nil {
		t.Fatal(err)
	}
	wantDocuments := []string{filepath.Clean(docs)}
	wantAllowed := []string{filepath.Clean(work), filepath.Clean(assets), filepath.Clean(docs)}
	if !reflect.DeepEqual(saved.DocumentDirectories, wantDocuments) || !reflect.DeepEqual(saved.AllowedDirectories, wantAllowed) {
		t.Fatalf("saved profile directories = documents=%#v allowed=%#v, want documents=%#v allowed=%#v", saved.DocumentDirectories, saved.AllowedDirectories, wantDocuments, wantAllowed)
	}
	if !filepath.IsAbs(saved.WorkingDirectory) {
		t.Fatalf("working directory must be absolute: %q", saved.WorkingDirectory)
	}
}

func TestSaveLansengerBotRejectsIncompleteEnabledProfile(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	_, err := app.SaveLansengerBot(corelib.LansengerBotProfile{ID: "missing-credentials", Enabled: true})
	if err == nil || !strings.Contains(err.Error(), "App ID and App Secret") {
		t.Fatalf("err = %v, want incomplete credentials error", err)
	}
}

func TestSaveLansengerBotRejectsDuplicateEnabledAppID(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	if _, err := app.SaveLansengerBot(corelib.LansengerBotProfile{ID: "first", Enabled: true, AppID: "app-id", AppSecret: "first-secret"}); err != nil {
		t.Fatal(err)
	}
	_, err := app.SaveLansengerBot(corelib.LansengerBotProfile{ID: "second", Enabled: true, AppID: "app-id", AppSecret: "second-secret"})
	if err == nil || !strings.Contains(err.Error(), "already uses this App ID") {
		t.Fatalf("err = %v, want duplicate App ID error", err)
	}
}

func TestLansengerBotViewMarksDeletedExpertAsDegraded(t *testing.T) {
	swapExpertStoreForTest(t)
	const expertID = "support-expert"
	if err := defaultExpertStore.Save(ExpertDefinition{ID: expertID, Name: "Support"}); err != nil {
		t.Fatal(err)
	}
	profile := corelib.LansengerBotProfile{ID: "support", AssistantMode: corelib.LansengerAssistantModeExpert, ExpertID: expertID}
	if view := lansengerBotProfileView(profile); view.Status != "" || view.StatusReason != "" {
		t.Fatalf("available expert status = %#v", view)
	}
	if err := defaultExpertStore.Delete(expertID, false); err != nil {
		t.Fatal(err)
	}
	view := lansengerBotProfileView(profile)
	if view.Status != "degraded" || view.StatusReason != unavailableAssistantBindingExpertMessage {
		t.Fatalf("deleted expert status = %#v", view)
	}
}

func TestUpdateLansengerBotProfileGroupStateIsScoped(t *testing.T) {
	app := &App{testHomeDir: t.TempDir()}
	for _, id := range []string{"alpha", "beta"} {
		if _, err := app.SaveLansengerBot(corelib.LansengerBotProfile{ID: id}); err != nil {
			t.Fatal(err)
		}
	}
	if err := app.SetLansengerBotGroupIgnored("alpha", "group-1", true); err != nil {
		t.Fatal(err)
	}
	if err := app.SetLansengerBotGroupAllowed("alpha", "group-2", true); err != nil {
		t.Fatal(err)
	}
	if err := app.SetLansengerBotGroupFileMaxBytes("alpha", "group-3", 123); err != nil {
		t.Fatal(err)
	}
	cfg, err := app.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	alpha, ok := lansengerBotProfileFromConfig(cfg, "alpha")
	if !ok || !lansengerBotProfileGroupIgnored(alpha, "group-1") || !lansengerBotProfileGroupAllowed(alpha, "group-2") || lansengerBotProfileGroupFileLimit(alpha, "group-3") != 123 {
		t.Fatalf("alpha profile state = %#v", alpha)
	}
	beta, ok := lansengerBotProfileFromConfig(cfg, "beta")
	if !ok || lansengerBotProfileGroupIgnored(beta, "group-1") || lansengerBotProfileGroupAllowed(beta, "group-2") || lansengerBotProfileGroupFileLimit(beta, "group-3") != 0 {
		t.Fatalf("beta profile leaked alpha group state: %#v", beta)
	}
}
