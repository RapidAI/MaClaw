package botmgmt

import "testing"

func TestAnswersOwnReportStopsWakePingPong(t *testing.T) {
	notes := []BotCrossNote{
		{ID: "n1", Kind: "report", From: "bot-a", Target: "bot-b"},
		{ID: "n2", Kind: "report", From: "bot-b", Target: "bot-a", ReplyTo: "n1"},
		{ID: "n3", Kind: "delegate", From: "bot-a", Target: "bot-b"},
		{ID: "n4", Kind: "report", From: "bot-b", Target: "bot-a", ReplyTo: "n3"},
		{ID: "n5", Kind: "report", From: "bot-c", Target: "bot-b"},
	}
	if !answersOwnReport(notes, notes[1]) {
		t.Fatal("a reply to bot-a's own report must not wake bot-a")
	}
	if answersOwnReport(notes, notes[3]) {
		t.Fatal("a result for a delegate must still wake")
	}
	if answersOwnReport(notes, notes[4]) {
		t.Fatal("a fresh report must still wake")
	}
}
