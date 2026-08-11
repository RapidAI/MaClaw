package corelib

import (
	"encoding/json"
	"testing"
)

func TestApplyLansengerMultiBotMigrationCopiesLegacySettings(t *testing.T) {
	mention := false
	cfg := AppConfig{
		LansengerEnabled: true, LansengerAppID: "app", LansengerAppSecret: "secret",
		LansengerGroupPolicy: "allowlist", LansengerAllowedGroupIDs: []string{"g1"}, LansengerRequireMention: &mention,
		LansengerAutoMentionReply: false, LansengerGroupFileMaxBytes: map[string]int64{"g1": 42},
		AnswerCache: AnswerCacheConfig{Enabled: true, TTLDays: 7},
	}
	if !ApplyLansengerMultiBotMigration(&cfg) {
		t.Fatal("migration did not report change")
	}
	if !cfg.LansengerBotsMigrated || len(cfg.LansengerBots) != 1 {
		t.Fatalf("migration result: %#v", cfg)
	}
	bot := cfg.LansengerBots[0]
	if bot.ID != DefaultLansengerBotProfileID || bot.EffectiveAutoMentionReply() || bot.RequireMention == nil || *bot.RequireMention {
		t.Fatalf("legacy flags not preserved: %#v", bot)
	}
	if bot.GroupFileMaxBytes["g1"] != 42 {
		t.Fatalf("limits not copied: %#v", bot.GroupFileMaxBytes)
	}
	if got := bot.EffectiveAnswerCache(); !got.CanReuseAnswers() || got.TTLDays != 7 {
		t.Fatalf("reply-cache policy not copied: %#v", got)
	}
	if ApplyLansengerMultiBotMigration(&cfg) {
		t.Fatal("second migration must be a no-op")
	}
}

func TestLansengerBotProfileDefaultsToMentionReply(t *testing.T) {
	if !(LansengerBotProfile{}).EffectiveAutoMentionReply() {
		t.Fatal("new profile should mention the asker by default")
	}
}

func TestLansengerBotProfileReplyCacheKeepsExplicitDisabledZeroTTL(t *testing.T) {
	legacy := LansengerBotProfile{}
	if got := legacy.EffectiveAnswerCache(); !got.Enabled || got.TTLDays != 0 {
		t.Fatalf("legacy profile default cache = %#v, want enabled zero TTL", got)
	}

	disabled := AnswerCacheConfig{Enabled: false, TTLDays: 0}
	encoded, err := json.Marshal(LansengerBotProfile{ID: "support", AnswerCache: &disabled})
	if err != nil {
		t.Fatal(err)
	}
	var restored LansengerBotProfile
	if err := json.Unmarshal(encoded, &restored); err != nil {
		t.Fatal(err)
	}
	if restored.AnswerCache == nil {
		t.Fatal("explicit disabled reply-cache policy was omitted during round trip")
	}
	if got := restored.EffectiveAnswerCache(); got.Enabled || got.TTLDays != 0 {
		t.Fatalf("restored cache policy = %#v, want explicit disabled zero TTL", got)
	}
}
