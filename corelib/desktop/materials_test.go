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
	if got := MaterialsFolder("bot-1", "小红书助手", []string{"写作"}); got != "小红书助手" {
		t.Fatalf("got %q", got)
	}
	if got := MaterialsFolder("bot-1", "小红书助手", []string{"小红书助手"}); got != "bot-1" {
		t.Fatalf("collision: %q", got)
	}
	if got := MaterialsFolder("bot-1", "../", nil); got != "bot-1" {
		t.Fatalf("invalid: %q", got)
	}
	if got := MaterialsFolder("bot-1", "a/b", nil); got != "a_b" {
		t.Fatalf("sanitized: %q", got)
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
