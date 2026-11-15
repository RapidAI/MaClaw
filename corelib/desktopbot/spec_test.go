package desktopbot

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHidePlanModeDropsTheLabel(t *testing.T) {
	got := HidePlanMode("我们进入规划模式，发给谁？")
	if got != "我们发给谁？" {
		t.Fatalf("hidden = %q", got)
	}
	if HidePlanMode("规划模式") != "" {
		t.Fatal("a label-only sentence should be empty")
	}
	if HidePlanMode("发给谁？") != "发给谁？" {
		t.Fatal("an ordinary question stays")
	}
}

func TestSpecStaysUntilCleared(t *testing.T) {
	ResetSpecMemory()
	t.Cleanup(ResetSpecMemory)
	NoteSpec("t", "u", "bot", "周报，收件人未定")
	if SpecOf("t", "u", "bot") != "周报，收件人未定" {
		t.Fatal("spec was not stored")
	}
	if SpecOf("t", "u", "other") != "" {
		t.Fatal("spec leaked to another bot")
	}
	NoteSpec("t", "u", "bot", "周报发给老王")
	if SpecOf("t", "u", "bot") != "周报发给老王" {
		t.Fatal("spec was not replaced")
	}
	ClearSpec("t", "u", "bot")
	if SpecOf("t", "u", "bot") != "" {
		t.Fatal("spec stayed after clear")
	}
}

func TestReplaceSpecIgnoresAStaleGeneration(t *testing.T) {
	ResetSpecMemory()
	t.Cleanup(ResetSpecMemory)
	gen := NoteSpec("t", "u", "bot", "周报，收件人未定")
	if gen == 0 || SpecGen("t", "u", "bot") != gen {
		t.Fatalf("gen = %d", gen)
	}
	if !ReplaceSpec("t", "u", "bot", gen, "目标：周报。约束：发给指定的人。完成标准：老王收到。还缺：发给谁") {
		t.Fatal("the current generation was rejected")
	}
	next := NoteSpec("t", "u", "bot", "周报发给老王")
	if ReplaceSpec("t", "u", "bot", gen, "过期的规范") {
		t.Fatal("a stale background write landed")
	}
	if SpecOf("t", "u", "bot") != "周报发给老王" || SpecGen("t", "u", "bot") != next {
		t.Fatalf("spec = %q gen %d", SpecOf("t", "u", "bot"), SpecGen("t", "u", "bot"))
	}
	ClearSpec("t", "u", "bot")
	if ReplaceSpec("t", "u", "bot", next, "清掉之后又写回") {
		t.Fatal("a cleared spec was restored")
	}
}

func TestSpecFileSurvivesReload(t *testing.T) {
	ResetSpecMemory()
	t.Cleanup(func() {
		detachSpecFile()
		ResetSpecMemory()
	})
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := BindSpecFile(bad); err == nil {
		t.Fatal("an unreadable spec file was accepted")
	}
	left, err := os.ReadFile(bad)
	if err != nil || string(left) != "{" {
		t.Fatalf("corrupt file = %q %v", left, err)
	}

	path := filepath.Join(dir, "specs.json")
	if err := BindSpecFile(path); err != nil {
		t.Fatal(err)
	}
	gen := NoteSpec("t", "u", "bot", "周报，收件人未定")
	const card = "目标：周报。完成标准：老王收到。还缺：发给谁"
	if !ReplaceSpec("t", "u", "bot", gen, card) {
		t.Fatal("the current generation was rejected")
	}
	NoteSpec("t", "u", "other", "另一件事")
	ResetSpecMemory()
	if err := BindSpecFile(path); err != nil {
		t.Fatal(err)
	}
	if SpecOf("t", "u", "bot") != card || SpecGen("t", "u", "bot") != gen {
		t.Fatalf("reloaded = %q gen %d", SpecOf("t", "u", "bot"), SpecGen("t", "u", "bot"))
	}
	if SpecOf("t", "u", "other") != "另一件事" {
		t.Fatal("the other bot's spec was dropped")
	}
	if ReplaceSpec("t", "u", "bot", gen+1, "过期的规范") {
		t.Fatal("a stale generation landed after reload")
	}
	ClearSpec("t", "u", "bot")
	ResetSpecMemory()
	if err := BindSpecFile(path); err != nil {
		t.Fatal(err)
	}
	if SpecOf("t", "u", "bot") != "" {
		t.Fatal("a cleared spec came back")
	}
	if SpecOf("t", "u", "other") != "另一件事" {
		t.Fatal("clearing one bot removed the other")
	}
}
