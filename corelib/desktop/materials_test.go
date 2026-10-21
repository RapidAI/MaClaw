package desktop

import (
	"strings"
	"testing"
)

func TestSafeMaterialName(t *testing.T) {
	cases := map[string]string{
		"  封面.jpg ":       "封面.jpg",
		"../etc/passwd":   "_etc_passwd",
		"a/b\\c:d":        "a_b_c_d",
		"..":              "",
		"   ":             "",
		"///":             "",
		".hidden":         "hidden",
		"line\nbreak.txt": "line_break.txt",
	}
	for in, want := range cases {
		if got := SafeMaterialName(in); got != want {
			t.Errorf("SafeMaterialName(%q)=%q want %q", in, got, want)
		}
	}
	long := strings.Repeat("长", 300) + ".mp4"
	got := SafeMaterialName(long)
	if !strings.HasSuffix(got, ".mp4") || len([]rune(got)) != 120 {
		t.Fatalf("long name: %d runes %q", len([]rune(got)), got[len(got)-8:])
	}
}

func TestMaterialsFolder(t *testing.T) {
	if got := MaterialsFolder("bot-1", "小红书助手", []MaterialPeer{{ID: "bot-2", Name: "写作"}}); got != "小红书助手" {
		t.Fatalf("got %q", got)
	}
	if got := MaterialsFolder("bot-1", "小红书助手", []MaterialPeer{{ID: "bot-2", Name: "小红书助手"}}); got != "bot-1" {
		t.Fatalf("collision: %q", got)
	}
	if got := MaterialsFolder("bot-2", "小红书助手", []MaterialPeer{{ID: "bot-1", Name: "小红书助手"}}); got != "bot-2" {
		t.Fatalf("other side of a shared name: %q", got)
	}
	if got := MaterialsFolder("bot-1", "../", nil); got != "bot-1" {
		t.Fatalf("invalid: %q", got)
	}
	if got := MaterialsFolder("bot-1", "a/b", nil); got != "a_b" {
		t.Fatalf("sanitized: %q", got)
	}
	// An unusable name occupies the id folder. A later bot must not reuse it
	// just because the first bot's display name sanitizes to empty.
	if got := MaterialsFolder("bot_b", "bot_a", []MaterialPeer{{ID: "bot_a", Name: "..."}}); got != "bot_b" {
		t.Fatalf("id folder taken by a nameless bot: %q", got)
	}
	if got := MaterialsFolder("bot_a", "...", []MaterialPeer{{ID: "bot_b", Name: "bot_a"}}); got != "bot_a" {
		t.Fatalf("nameless bot lost its id folder: %q", got)
	}
}

func TestMaterialStoredNameKeepsTheUploadID(t *testing.T) {
	a := MaterialStoredName("image.png", "up-aaaaaaaa")
	b := MaterialStoredName("image.png", "up-bbbbbbbb")
	if a == b || a == "image.png" || !strings.HasSuffix(a, "-up-aaaaaaaa.png") {
		t.Fatalf("stored names %q %q", a, b)
	}
	long := MaterialStoredName(strings.Repeat("长", 300)+".mp4", "up-12345678")
	if !strings.HasSuffix(long, "-up-12345678.mp4") || len([]rune(long)) > 120 {
		t.Fatalf("long stored name %d %q", len([]rune(long)), long)
	}
}

func TestMaterialPath(t *testing.T) {
	p, err := MaterialPath("小红书助手", "../../x.png")
	if err != nil || p != "/home/desktop/素材/小红书助手/_.._x.png" && p != ContainerHome+"/素材/小红书助手/_.._x.png" {
		t.Fatalf("path %q %v", p, err)
	}
	if !strings.HasPrefix(p, MaterialsRoot+"/") {
		t.Fatalf("escaped root: %q", p)
	}
	if MaterialDisplayPath(p) != "~/素材/小红书助手/_.._x.png" {
		t.Fatalf("display %q", MaterialDisplayPath(p))
	}
	if _, err := MaterialPath("", "x"); err == nil {
		t.Fatal("empty folder accepted")
	}
}
