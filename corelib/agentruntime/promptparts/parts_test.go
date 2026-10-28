package promptparts

import (
	"strings"
	"testing"
)

func TestRenderOverridesAndSwitch(t *testing.T) {
	out := Render(Overrides{Quiet: "-", Voice: "be brief"}, Voice, Quiet, Honesty)
	if !strings.HasPrefix(out, "be brief\n") || strings.Contains(out, Default(Quiet)) || !strings.Contains(out, Default(Honesty)) {
		t.Fatalf("%q", out)
	}
	t.Setenv("MACLAW_STYLE_LAYER", "off")
	if Render(nil, Voice) != "" {
		t.Fatal("kill switch ignored")
	}
}

func TestEveryPartHasText(t *testing.T) {
	for _, id := range IDs() {
		if Default(id) == "" {
			t.Fatal(id)
		}
	}
	if len(BaseHash()) != 16 {
		t.Fatal("hash")
	}
	long := strings.Repeat("字", MaxPartRunes+10)
	if n := len([]rune(Text(Voice, Overrides{Voice: long}))); n != MaxPartRunes {
		t.Fatal(n)
	}
}

func TestFactRoundTrip(t *testing.T) {
	line := Fact("PUBLISH_RESULT", "state", "live_incomplete", "link", "https://x/p/1", "missing", `picture 7 "a"`, "notice", "")
	key, f := ParseFact(line)
	if key != "PUBLISH_RESULT" || f["state"] != "live_incomplete" || f["link"] != "https://x/p/1" || f["missing"] != "picture 7 'a'" {
		t.Fatalf("%s %v", line, f)
	}
	if _, ok := f["notice"]; ok {
		t.Fatal("empty field kept")
	}
}

func TestStyleMetaRoundTripAndCharacter(t *testing.T) {
	st := Style{Soul: "叫小龙", Memory: "主人用知乎", Parts: map[string]string{"voice": "简短", "bogus": "x"}}
	back := StyleFromMeta(st.Meta())
	if back.Soul != "叫小龙" || back.Memory != "主人用知乎" || back.Overrides()[Voice] != "简短" || len(back.Overrides()) != 1 {
		t.Fatalf("%+v", back)
	}
	if c := (Style{}).Character(); !strings.Contains(c, DefaultSoul) {
		t.Fatal(c)
	}
	if c := back.Character(); !strings.Contains(c, "叫小龙") || !strings.Contains(c, "主人用知乎") {
		t.Fatal(c)
	}
	if n := len([]rune(StyleFromMeta(map[string]string{MetaSoul: strings.Repeat("a", SoulMaxRunes+5)}).Soul)); n != SoulMaxRunes {
		t.Fatal(n)
	}
}

func TestDefaultStyleHasNoFillerOrBareConfirm(t *testing.T) {
	voice := Default(Voice)
	for _, want := range []string{"Lead with the answer", "Do not narrate a plan", "do not restate the request", "no boilerplate openers or closers", "Do not ask permission to proceed"} {
		if !strings.Contains(voice, want) {
			t.Fatalf("voice part lacks %q", want)
		}
	}
	for _, want := range []string{"capable friend", "No plan narration", "no restating the request", "no bare"} {
		if !strings.Contains(DefaultSoul, want) {
			t.Fatalf("DefaultSoul lacks %q", want)
		}
	}
	if strings.Contains(DefaultSoul, "wait for the owner's go-ahead") {
		t.Fatal("DefaultSoul still asks for a bare go-ahead")
	}
}
