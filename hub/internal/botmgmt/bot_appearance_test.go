package botmgmt

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestBotAppearanceStoredPerOwner(t *testing.T) {
	settings := &memSettings{}
	raw, _ := json.Marshal(record{
		Bots:   []Bot{{ID: "bot_a", Name: "a", InstanceID: "i", OwnerUserID: "alice"}, {ID: "bot_b", Name: "b", InstanceID: "j", OwnerUserID: "bob"}},
		Grants: []Grant{{ID: "g1", Scope: ScopeUser, TargetID: "alice"}},
	})
	_ = settings.Set(context.Background(), storageKey("t"), string(raw))
	svc := NewService(settings)
	c, sh := "#FF8800", "cloud"
	bot, err := svc.SetBotAppearanceForUser(context.Background(), "t", "alice", "bot_a", &c, &sh)
	if err != nil || bot.Color != "#ff8800" || bot.Shape != "cloud" {
		t.Fatalf("%v %+v", err, bot)
	}
	// Only shape changes; color stays.
	sq := "square"
	bot, _ = svc.SetBotAppearanceForUser(context.Background(), "t", "alice", "bot_a", nil, &sq)
	if bot.Color != "#ff8800" || bot.Shape != "square" {
		t.Fatalf("partial: %+v", bot)
	}
	rec, _ := svc.load(context.Background(), "t")
	if rec.Bots[0].Color != "#ff8800" || rec.Bots[0].Shape != "square" {
		t.Fatalf("not persisted: %+v", rec.Bots[0])
	}
	if _, err := svc.SetBotAppearanceForUser(context.Background(), "t", "alice", "bot_b", &c, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("other owner: %v", err)
	}
	bad := "red"
	if _, err := svc.SetBotAppearanceForUser(context.Background(), "t", "alice", "bot_a", &bad, nil); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad color: %v", err)
	}
	badShape := "<svg>"
	if _, err := svc.SetBotAppearanceForUser(context.Background(), "t", "alice", "bot_a", nil, &badShape); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("bad shape: %v", err)
	}
	empty := ""
	bot, _ = svc.SetBotAppearanceForUser(context.Background(), "t", "alice", "bot_a", &empty, nil)
	if bot.Color != "" || bot.Shape != "square" {
		t.Fatalf("clear: %+v", bot)
	}
}
