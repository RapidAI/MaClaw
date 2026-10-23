package main

import (
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func TestRunMediaTrackedForChooser(t *testing.T) {
	sc := agentruntime.Scope{RunID: "run-media-1"}
	desktopNoteMedia(sc, "bash", map[string]any{"command": "python3 make.py -o /home/desktop/Desktop/xhs_cover.png"}, "ok 1080x1440", nil)
	desktopNoteMedia(sc, "bash", map[string]any{"command": "echo hi"}, "-rw-r--r-- 1 root root 486 /tmp/dateband.png", nil)
	m := desktopPendingMedia("run-media-1")
	if len(m) != 2 || m[1] != "/tmp/dateband.png" || m[0] != "/home/desktop/Desktop/xhs_cover.png" {
		t.Fatalf("media = %v", m)
	}
	desktopNoteMedia(sc, "upload", map[string]any{"files": []any{"/home/desktop/Desktop/xhs_cover.png"}}, "", nil)
	if m = desktopPendingMedia("run-media-1"); m[len(m)-1] != "/home/desktop/Desktop/xhs_cover.png" {
		t.Fatalf("newest = %v", m)
	}
	if f := desktopArmChooserForRun(sc, "navigate", nil); f == nil {
		t.Fatal("nil disarm")
	}
}

func TestUploadJudge(t *testing.T) {
	// XHS clears input.files at once; the editor and previews appear instead.
	if ok, _ := desktopUploadJudge("0,0,12,0,900", "0,10,22,0,1400"); !ok {
		t.Fatal("preview not seen")
	}
	if ok, _ := desktopUploadJudge("0,0,12,0,900", "0,0,12,0,905"); ok {
		t.Fatal("no change judged ok")
	}
	if ok, _ := desktopUploadJudge("0,0,0,0,10", "1,0,0,0,10"); !ok {
		t.Fatal("input files not seen")
	}
}
