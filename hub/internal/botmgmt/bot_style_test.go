package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime/promptparts"
)

func TestBotStyleVersionsRollbackAndMeta(t *testing.T) {
	ctx := context.Background()
	settings := &memSettings{}
	raw, _ := json.Marshal(record{
		Bots:   []Bot{{ID: "bot_a", Name: "a", InstanceID: "i", OwnerUserID: "alice"}, {ID: "bot_b", Name: "b", InstanceID: "j", OwnerUserID: "bob"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	_ = settings.Set(ctx, storageKey("t"), string(raw))
	svc := NewService(settings)

	v, err := svc.GetBotStyleForUser(ctx, "t", "alice", "bot_a")
	if err != nil || v.Current.Version != 0 || v.DefaultSoul == "" || v.DefaultParts["honesty"] == "" {
		t.Fatalf("%v %+v", err, v)
	}
	soul, mem := "叫我小龙", "主人写知乎"
	if v, err = svc.SetBotStyleForUser(ctx, "t", "alice", "bot_a", BotStyleInput{Soul: &soul, Memory: &mem}); err != nil || v.Current.Version != 1 {
		t.Fatalf("%v %+v", err, v)
	}
	parts := map[string]string{"voice": "更短"}
	if v, _ = svc.SetBotStyleForUser(ctx, "t", "alice", "bot_a", BotStyleInput{Parts: &parts}); v.Current.Version != 2 || v.Current.Soul != soul || v.Current.Parts["voice"] != "更短" || len(v.History) != 1 {
		t.Fatalf("partial update: %+v", v)
	}
	bad := map[string]string{"nope": "x"}
	if _, err := svc.SetBotStyleForUser(ctx, "t", "alice", "bot_a", BotStyleInput{Parts: &bad}); !errors.Is(err, ErrInvalidInput) {
		t.Fatal(err)
	}
	if _, err := svc.SetBotStyleForUser(ctx, "t", "alice", "bot_b", BotStyleInput{Soul: &soul}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner: %v", err)
	}
	if v, err = svc.RollbackBotStyleForUser(ctx, "t", "alice", "bot_a", 1); err != nil || v.Current.Version != 3 || len(v.Current.Parts) != 0 || v.Current.Soul != soul {
		t.Fatalf("rollback: %v %+v", err, v)
	}
	rec, _ := svc.load(ctx, "t")
	st := promptparts.StyleFromMeta(botStyleMeta(&rec, "bot_a"))
	if st.Soul != soul || st.Memory != mem {
		t.Fatalf("meta %+v", st)
	}
	t.Setenv("MACLAW_STYLE_LAYER", "off")
	if botStyleMeta(&rec, "bot_a") != nil {
		t.Fatal("kill switch ignored")
	}
}
