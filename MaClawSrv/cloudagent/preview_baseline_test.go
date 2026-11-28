package cloudagent

import (
	"path/filepath"
	"testing"
)

func TestNoteFileBaselineRemembersPreviewOriginal(t *testing.T) {
	root := t.TempDir()
	inst := &instance{}
	host := &agentHost{
		root:            root,
		inst:            inst,
		fileWriteCounts: map[string]int{"a.txt": 1},
	}
	abs := filepath.Join(root, "a.txt")
	host.noteFileBaseline(abs, "before\n", true)
	got, ok := inst.previewBaseline("a.txt")
	if !ok || got != "before\n" {
		t.Fatalf("baseline=%q ok=%v", got, ok)
	}
	if host.fileBaselines["a.txt"] != "before\n" {
		t.Fatalf("host baseline=%q", host.fileBaselines["a.txt"])
	}

	host.noteFileBaseline(abs, "later\n", true)
	got, ok = inst.previewBaseline("a.txt")
	if !ok || got != "before\n" {
		t.Fatalf("replaced baseline=%q ok=%v", got, ok)
	}

	empty := &instance{}
	blank := &agentHost{
		root:            root,
		inst:            empty,
		fileWriteCounts: map[string]int{"b.txt": 1},
	}
	blank.noteFileBaseline(filepath.Join(root, "b.txt"), "", true)
	if _, ok := empty.previewBaseline("b.txt"); ok {
		t.Fatal("empty baseline stored")
	}

	second := &instance{}
	again := &agentHost{
		root:            root,
		inst:            second,
		fileWriteCounts: map[string]int{"c.txt": 2},
	}
	again.noteFileBaseline(filepath.Join(root, "c.txt"), "nope\n", true)
	if _, ok := second.previewBaseline("c.txt"); ok {
		t.Fatal("a later write replaced the missing first snapshot")
	}
}
