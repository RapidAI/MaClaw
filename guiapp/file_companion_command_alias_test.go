package guiapp

import "testing"

func TestFileCompanionCommandAliases(t *testing.T) {
	notes := []string{"generate notes", "Take Notes", "make a note.", "тэмдэглэл үүсгэх", "Тэмдэглэл хийх"}
	for _, text := range notes {
		if !fileCompanionVideoNotePhraseMatches(text) {
			t.Fatalf("note alias %q", text)
		}
		if !fileCompanionWantsVideoNote("lecture.mp4", FileCompanionTurnRequest{Text: text}) {
			t.Fatalf("note path %q", text)
		}
		if fileCompanionWantsVideoNote("paper.pdf", FileCompanionTurnRequest{Text: text}) {
			t.Fatalf("note alias entered a pdf %q", text)
		}
	}
	if fileCompanionVideoNotePhraseMatches("please generate notes later") {
		t.Fatal("a longer sentence matched the note alias")
	}
	if fileCompanionPaperAlias("论文解读") {
		t.Fatal("typed Chinese paper label was promoted")
	}
	papers := []string{"read the paper", "Paper Briefing", "өгүүллийн тайлбар"}
	for _, text := range papers {
		if !fileCompanionPaperAlias(text) {
			t.Fatalf("paper alias %q", text)
		}
		got := fileCompanionWithCommandAlias(FileCompanionTurnRequest{Text: text})
		if got.Purpose != fileCompanionPurposePaper {
			t.Fatalf("purpose for %q = %q", text, got.Purpose)
		}
	}
	kept := fileCompanionWithCommandAlias(FileCompanionTurnRequest{Text: "read the paper", Purpose: "video-note"})
	if kept.Purpose != "video-note" {
		t.Fatalf("existing purpose changed to %q", kept.Purpose)
	}
	for _, zh := range []string{"正在整理笔记", "正在记录画面", "正在识别语音（第 1/4 段）"} {
		if got := pickUI("mn", zh, zh, ""); got != zh {
			t.Fatalf("pickUI rewrote %q to %q", zh, got)
		}
	}
}
