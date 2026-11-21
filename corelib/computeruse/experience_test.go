package computeruse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExperiencePromptRemembersLanxinAndDropsFreeText(t *testing.T) {
	SetExperienceDir(t.TempDir())
	t.Cleanup(func() { SetExperienceDir("") })

	got := ExperiencePrompt("向蓝信中的龙军用户问好", nil)
	if !strings.Contains(got, "lanxin") || !strings.Contains(got, "search box") || !strings.Contains(got, "hint:") {
		t.Fatalf("lanxin hint missing: %s", got)
	}
	if strings.Contains(got, "龙军") || strings.Contains(got, "lessons from earlier") {
		t.Fatal(got)
	}
	if !strings.Contains(got, "generic, not this machine's history") {
		t.Fatal(got)
	}
	notepad := ExperiencePrompt("打开记事本", nil)
	if strings.Contains(notepad, "lanxin") {
		t.Fatal("an unrelated app must not pull the lanxin lesson")
	}
	if !strings.Contains(notepad, "screen_index=-1") {
		t.Fatal("desktop stitch hint should travel with every computer-use prompt")
	}

	if ExperienceRecord("lanxin", "typed secret 998877 to 龙军", true) != "" {
		t.Fatal("free text was stored")
	}
	if ExperienceRecord("lanxin", "13800138000", true) != "" {
		t.Fatal("a phone number was stored")
	}
	if strings.Contains(ExperiencePrompt("蓝信", nil), "998877") || strings.Contains(ExperiencePrompt("蓝信", nil), "龙军") {
		t.Fatal("free text was stored")
	}
	if ExperienceRecord("", NoteSearchFind, true) != "" {
		t.Fatal("an empty app stored a search note on desktop")
	}
	if strings.Contains(ExperiencePrompt("继续", nil), "lanxin") {
		t.Fatal("a follow-up with no app name must not resurrect lanxin")
	}

	first := ExperienceRecord("lanxin", NoteSearchFind, true)
	second := ExperienceRecord("lanxin", NoteSearchFind, true)
	if !strings.Contains(first, "worked:") || strings.Contains(first, "(x") {
		t.Fatalf("first line: %s", first)
	}
	if !strings.Contains(second, "(x2)") {
		t.Fatalf("second line: %s", second)
	}
	again := ExperiencePrompt("继续", nil)
	if strings.Contains(again, "lanxin") || strings.Contains(again, "worked:") {
		t.Fatalf("last app leaked into an unrelated follow-up: %s", again)
	}
	named := ExperiencePrompt("向蓝信中的龙军用户问好", nil)
	if !strings.Contains(named, "lanxin") || !strings.Contains(named, "worked:") || !strings.Contains(named, "(x2)") {
		t.Fatalf("learned find note missing: %s", named)
	}
	if !strings.Contains(named, "hint:") || !strings.Contains(named, "search box") {
		t.Fatalf("generic hint missing beside the learned note: %s", named)
	}
	turned := ExperiencePrompt("继续", []string{"lanxin"})
	if !strings.Contains(turned, "lanxin") || !strings.Contains(turned, "worked:") {
		t.Fatalf("explicit turn app missing: %s", turned)
	}
	if strings.Contains(ExperiencePrompt("继续", []string{"not-a-window"}), "not-a-window") {
		t.Fatal("unknown turn app was rendered")
	}
}

func TestExperienceAvoidHidesTheGenericHint(t *testing.T) {
	dir := t.TempDir()
	SetExperienceDir(dir)
	t.Cleanup(func() { SetExperienceDir("") })

	line := ExperienceRecord("lanxin", NoteWindowChanged, false)
	if !strings.Contains(line, "avoid:") {
		t.Fatalf("line: %s", line)
	}
	got := ExperiencePrompt("给蓝信发消息", nil)
	if strings.Contains(got, "contact list") {
		t.Fatalf("avoid should hide the IM hint: %s", got)
	}
	if !strings.Contains(got, "avoid:") || !strings.Contains(got, "computer_focus the app") {
		t.Fatalf("learned avoid missing: %s", got)
	}
	if !strings.Contains(got, "hint:") || !strings.Contains(got, "screen_index=-1") {
		t.Fatalf("desktop hint should remain: %s", got)
	}

	stitch := ExperienceRecord("", NoteStitchRefused, false)
	if !strings.Contains(stitch, "avoid:") || !strings.Contains(stitch, "screen_index=-1") {
		t.Fatalf("stitch line: %s", stitch)
	}
	desktop := ExperiencePrompt("打开记事本", nil)
	if strings.Contains(desktop, "hint:") {
		t.Fatalf("learned stitch should replace the same hint: %s", desktop)
	}
	if strings.Count(desktop, "screen_index=-1") != 1 || !strings.Contains(desktop, "learned on this machine") {
		t.Fatalf("stitch avoid: %s", desktop)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "lessons.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "last_app") {
		t.Fatalf("LastApp was written: %s", raw)
	}
	SetExperienceDir("")
	if ExperiencePrompt("蓝信", []string{"lanxin"}) != "" {
		t.Fatal("empty dir should not prompt")
	}
}

func TestExperienceSearchProven(t *testing.T) {
	note, ok := ExperienceSearchProven(true, true, false, true, 3)
	if !ok || note != NoteSearchFind {
		t.Fatalf("vision focus+type: %q %v", note, ok)
	}
	if _, ok := ExperienceSearchProven(true, true, false, true, 4); ok {
		t.Fatal("observe count above the max should not record success")
	}
	if _, ok := ExperienceSearchProven(true, false, false, true, 0); ok {
		t.Fatal("vision without a type is not a search path")
	}
	if _, ok := ExperienceSearchProven(false, true, true, false, 1); ok {
		t.Fatal("text find without focus is not a search path")
	}
	note, ok = ExperienceSearchProven(true, true, true, false, 3)
	if !ok || note != NoteSearchFind {
		t.Fatalf("text focus+type+find: %q %v", note, ok)
	}
	if _, ok := ExperienceSearchProven(true, true, false, false, 1); ok {
		t.Fatal("text mode still needs a find hit")
	}
}

func TestExperienceDisabled(t *testing.T) {
	SetExperienceDir(t.TempDir())
	t.Cleanup(func() { SetExperienceDir("") })
	t.Setenv("MACLAW_COMPUTER_USE_EXPERIENCE", "off")
	if ExperienceRecord("lanxin", NoteSearchFind, true) != "" {
		t.Fatal("disabled store wrote a note")
	}
	if ExperiencePrompt("蓝信", nil) != "" {
		t.Fatal("disabled store returned a prompt")
	}
}

func TestExperienceAppIDUsesTheWindowSuffix(t *testing.T) {
	if got := ExperienceAppID("蓝信 - 张三"); got != "lanxin" {
		t.Fatalf("got %q", got)
	}
	if got := ExperienceAppID("简历.docx - Word"); got != "word" {
		t.Fatalf("got %q", got)
	}
	if ExperienceAppsIn("打开微信开发者工具")[0] != "wechat-dev" {
		t.Fatalf("got %#v", ExperienceAppsIn("打开微信开发者工具"))
	}
	if ExperienceAppID("完全未知的窗口") != "" {
		t.Fatal("unknown title should be empty")
	}
}
