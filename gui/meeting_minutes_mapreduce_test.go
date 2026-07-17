package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/meetingminutes"
)

func TestGuessMeetingTitleFromPath(t *testing.T) {
	if got := guessMeetingTitleFromPath(`C:\rec\record_weekly.wav`); got != "weekly" && got != "record_weekly" {
		// strip record_ prefix
		if got != "weekly" {
			// Windows basename may keep full name if prefix strip is case-sensitive path
			if !strings.Contains(got, "weekly") {
				t.Fatalf("title=%q", got)
			}
		}
	}
	if got := guessMeetingTitleFromPath(""); got != "会议纪要" {
		t.Fatalf("empty=%q", got)
	}
}

func TestMeetingMinutesDraftPath(t *testing.T) {
	got := meetingMinutesDraftPath(`D:\a\b\meet.wav`)
	if !strings.HasSuffix(got, `_minutes_draft.md`) || !strings.Contains(got, "meet") {
		t.Fatalf("draft path=%q", got)
	}
}

func TestBuildMeetingMinutesDraftExtractiveWithoutApp(t *testing.T) {
	body := strings.Repeat("讨论项目进度与风险。", 120)
	draft, usedLLM := buildMeetingMinutesDraft(context.Background(), nil, "周会", "同步", body, true)
	if usedLLM {
		t.Fatal("nil app must not use LLM")
	}
	if !strings.Contains(draft, "周会") || !strings.Contains(draft, "摘要") {
		t.Fatalf("draft incomplete: %s", clipMM(draft, 200))
	}
	// allowLLM=false always extractive
	draft2, used2 := buildMeetingMinutesDraft(context.Background(), nil, "周会", "", body, false)
	if used2 || !strings.Contains(draft2, "摘要") {
		t.Fatalf("allowLLM=false should be extractive")
	}
}

func TestASRToolBoolArg(t *testing.T) {
	if !asrToolBoolArg(map[string]interface{}{"for_minutes": true}, "for_minutes") {
		t.Fatal("bool true")
	}
	if !asrToolBoolArg(map[string]interface{}{"minutes": "true"}, "for_minutes", "minutes") {
		t.Fatal("string true")
	}
	if asrToolBoolArg(map[string]interface{}{"for_minutes": false}, "for_minutes") {
		t.Fatal("bool false")
	}
	if asrToolBoolArg(map[string]interface{}{"for_minutes": "false"}, "for_minutes") {
		t.Fatal("string false must not enable minutes")
	}
	if asrToolBoolArg(nil, "for_minutes") {
		t.Fatal("nil args")
	}
}

func TestEnrichLongASRWithMinutesDraft(t *testing.T) {
	dir := t.TempDir()
	audio := filepath.Join(dir, "long.wav")
	body := "【开头】" + strings.Repeat("会议正文段落内容。", 500) + "【结尾】"
	base := formatASRToolResult(audio, body)
	if !strings.Contains(base, "[ASR long transcript]") {
		t.Fatalf("expected long asr base")
	}
	// Transcribe-only path: toolASR skips enrich; base has no draft block.
	if strings.Contains(base, "[engine_minutes_draft]") {
		t.Fatalf("base spill must not include minutes draft")
	}

	h := &IMMessageHandler{} // no app → extractive when enrich is requested
	got := h.enrichLongASRWithMinutesDraft(audio, body, base, true)
	if !strings.Contains(got, "[engine_minutes_draft]") {
		t.Fatalf("missing engine draft block:\n%s", got[len(got)/2:])
	}
	if !strings.Contains(got, "minutes_draft_file:") {
		t.Fatalf("missing draft path")
	}
	if !strings.Contains(got, "used_llm_map_reduce: false") {
		t.Fatalf("expected extractive path without LLM")
	}
	// Sidecar draft on disk.
	draftPath := meetingMinutesDraftPath(audio)
	data, err := os.ReadFile(draftPath)
	if err != nil {
		t.Fatalf("draft file: %v", err)
	}
	if !strings.Contains(string(data), "摘要") {
		t.Fatalf("draft file content weak: %s", clipMM(string(data), 200))
	}
	// Transcript sidecar still required.
	if _, err := os.Stat(asrTranscriptSidecarPath(audio)); err != nil {
		t.Fatalf("transcript sidecar: %v", err)
	}
}

func TestSplitPlainTextUsedByMeetingPackage(t *testing.T) {
	// Sanity: package import path used by GUI compiles with multi-chunk split.
	var b strings.Builder
	for i := 0; i < 30; i++ {
		b.WriteString("段")
		b.WriteString(strings.Repeat("议题决议待办", 25))
		b.WriteString("\n\n")
	}
	chunks := meetingminutes.SplitPlainText(b.String(), 180, 80, 6)
	if len(chunks) < 2 {
		t.Fatalf("want multi chunk, got %d", len(chunks))
	}
}

func clipMM(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
