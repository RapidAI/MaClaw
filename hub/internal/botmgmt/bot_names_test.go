package botmgmt

import (
	"errors"
	"testing"
)

func TestNormalizeBotName(t *testing.T) {
	for in, want := range map[string]string{
		"Bot 5": "bot5", "bot5": "bot5", "5号bot": "bot5", "５号ｂｏｔ": "bot5",
		"机器人5": "bot5", "BOT 05": "bot5", "Ａｌｉｃｅ": "alice", " 小 助手 ": "小助手",
	} {
		if got := normalizeBotName(in); got != want {
			t.Errorf("normalizeBotName(%q)=%q want %q", in, got, want)
		}
	}
	for _, in := range []string{"5", "5号", "#5", "５号"} {
		if got := normalizeBotQuery(in); got != "bot5" {
			t.Errorf("normalizeBotQuery(%q)=%q", in, got)
		}
	}
}

func TestMatchBotName(t *testing.T) {
	c := []nameCandidate{
		{"b1", "Bot 5"}, {"b2", "Bot 12"}, {"b3", "写作助手"}, {"b4", "翻译助手"},
		{"b5", "Researcher"}, {"b6", "Coder Pro"},
	}
	ok := map[string]string{
		"b1": "b1", "bot 5": "b1", "5": "b1", "5号bot": "b1", "５号": "b1", "12": "b2",
		"写作": "b3", "翻译助手bot": "b4", "research": "b5", "reseacher": "b5",
		"coder": "b6", "Coder pr0": "b6",
	}
	for in, want := range ok {
		got, err := matchBotName(c, in)
		if err != nil || got != want {
			t.Errorf("match(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	if _, err := matchBotName(c, "助手"); !errors.Is(err, ErrAmbiguousBot) {
		t.Errorf("助手: want ambiguous, got %v", err)
	}
	if _, err := matchBotName(c, "bot"); !errors.Is(err, ErrAmbiguousBot) {
		t.Errorf("bot: want ambiguous, got %v", err)
	}
	_, err := matchBotName(c, "厨师长")
	var ne *BotNameError
	if !errors.Is(err, ErrNoSuchBot) || !errors.As(err, &ne) || len(ne.Candidates) != len(c) {
		t.Errorf("厨师长: want no-such with candidates, got %v", err)
	}
}

func TestBotNameConflict(t *testing.T) {
	rec := record{
		Bots:      []Bot{{ID: "h1", Name: "Bot 5", OwnerUserID: "u1"}},
		LocalBots: []LocalBot{{ID: "l1", Name: "小助手", Owner: "u1"}},
	}
	if err := botNameConflict(rec, "u1", "bot5", ""); !errors.Is(err, ErrNameTaken) {
		t.Errorf("bot5 should clash: %v", err)
	}
	if err := botNameConflict(rec, "u1", "小 助手", ""); !errors.Is(err, ErrNameTaken) {
		t.Errorf("小 助手 should clash: %v", err)
	}
	if err := botNameConflict(rec, "u1", "Bot 5", "h1"); err != nil {
		t.Errorf("rename to self: %v", err)
	}
	if err := botNameConflict(rec, "u2", "Bot 5", ""); err != nil {
		t.Errorf("other user: %v", err)
	}
}
