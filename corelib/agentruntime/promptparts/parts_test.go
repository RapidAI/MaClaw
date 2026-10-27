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
