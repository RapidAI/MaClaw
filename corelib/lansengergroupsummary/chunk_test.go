package lansengergroupsummary

import (
	"strings"
	"testing"
	"time"
)

func TestIsSummaryCommand(t *testing.T) {
	cases := map[string]bool{
		"/summary":   true,
		" /summary ": true,
		"/SUMMARY":   true,
		"/摘要":        true,
		"／summary":   true, // fullwidth slash
		"/summary x": false,
		"/summary start": false, // start is not bare run
		"summary":    false,
		"":           false,
	}
	for in, want := range cases {
		if got := IsSummaryCommand(in); got != want {
			t.Errorf("IsSummaryCommand(%q)=%v want %v", in, got, want)
		}
	}
}

func TestParseSummaryCommand(t *testing.T) {
	cases := map[string]SummaryCommandKind{
		"":                 SummaryCmdNone,
		"hello":            SummaryCmdNone,
		"/summary":         SummaryCmdRun,
		" /SUMMARY ":       SummaryCmdRun,
		"／summary":         SummaryCmdRun,
		"/摘要":              SummaryCmdRun,
		"/summary start":   SummaryCmdStart,
		"/summary  start":  SummaryCmdStart,
		"/summary\tstart":  SummaryCmdStart,
		"/SUMMARY START":   SummaryCmdStart,
		"/summary 开始":      SummaryCmdStart,
		"/summary 起点":      SummaryCmdStart,
		"/摘要 开始":           SummaryCmdStart,
		"/摘要 start":        SummaryCmdStart,
		"/summary foo":     SummaryCmdUnknown,
		"/summary start x": SummaryCmdUnknown,
		"/summarystart":    SummaryCmdNone, // glued — not a command
		"/摘要开始":            SummaryCmdNone,
		"@Bot /summary":    SummaryCmdNone, // caller must strip @ first
	}
	for in, want := range cases {
		if got := ParseSummaryCommand(in); got != want {
			t.Errorf("ParseSummaryCommand(%q)=%v want %v", in, got, want)
		}
	}
	if !IsSummaryControlLine("/summary start") || !IsSummaryControlLine("@Bot /summary start") {
		t.Fatal("expected control lines")
	}
	if IsSummaryControlLine("hello") {
		t.Fatal("plain text is not a control line")
	}
	// Parser also strips /cmd@BotName so buffered residual lines still match.
	if ParseSummaryCommand("/summary@Bot") != SummaryCmdRun {
		t.Fatal("expected /summary@Bot → run after postfix strip")
	}
	if ParseSummaryCommand("/summary@Bot start") != SummaryCmdStart {
		t.Fatal("expected /summary@Bot start → start")
	}
}

func TestFilterSummaryCommands(t *testing.T) {
	msgs := []Message{
		{Seq: 1, Text: "hello"},
		{Seq: 2, Text: "/summary"},
		{Seq: 3, Text: "world"},
		{Seq: 4, Text: "@Bot /summary"},
		{Seq: 5, Text: "/summary start"},
		{Seq: 6, Text: "@Bot /summary 开始"},
	}
	out := FilterSummaryCommands(msgs)
	if len(out) != 2 || out[0].Text != "hello" || out[1].Text != "world" {
		t.Fatalf("got %#v", out)
	}
}

func TestBuildChunksSinglePass(t *testing.T) {
	msgs := []Message{
		{Seq: 1, SpeakerName: "A", Text: "hi", At: time.Now()},
		{Seq: 2, SpeakerName: "B", Text: "there", At: time.Now()},
	}
	chunks := BuildChunks(msgs, 6000, 5500, 800)
	if len(chunks) != 1 {
		t.Fatalf("chunks=%d want 1", len(chunks))
	}
	if !strings.Contains(chunks[0].Formatted, "A:") || !strings.Contains(chunks[0].Formatted, "hi") {
		t.Fatalf("formatted=%q", chunks[0].Formatted)
	}
}

func TestBuildChunksMapReduce(t *testing.T) {
	// Force many small chunks with a tiny budget.
	var msgs []Message
	for i := 0; i < 20; i++ {
		msgs = append(msgs, Message{
			Seq:         int64(i + 1),
			SpeakerName: "User",
			Text:        strings.Repeat("讨论内容很长", 30) + string(rune('A'+i%26)),
			At:          time.Now(),
		})
	}
	chunks := BuildChunksCapped(msgs, 200, 100, 800, 12)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	if len(chunks) > 12 {
		t.Fatalf("chunk cap exceeded: %d", len(chunks))
	}
}

func TestPreferOldestKeepsHead(t *testing.T) {
	var msgs []Message
	for i := 0; i < 50; i++ {
		msgs = append(msgs, Message{
			Seq:         int64(i + 1),
			SpeakerName: "U",
			Text:        strings.Repeat("内容", 40) + string(rune('0'+i%10)),
			At:          time.Now(),
		})
	}
	kept, dropped := PreferOldest(msgs, 300, 800)
	if dropped == 0 {
		t.Fatal("expected some messages dropped")
	}
	if len(kept)+dropped != len(msgs) {
		t.Fatalf("kept=%d dropped=%d total=%d", len(kept), dropped, len(msgs))
	}
	// Kept should be the leading (oldest) seqs — safe for cursor MaxSeq(kept).
	if kept[0].Seq != msgs[0].Seq {
		t.Fatalf("expected oldest head, first seq=%d", kept[0].Seq)
	}
	if MaxSeq(kept) >= msgs[len(msgs)-1].Seq {
		t.Fatalf("expected tail left for next summary, max kept=%d last=%d", MaxSeq(kept), msgs[len(msgs)-1].Seq)
	}
}

func TestSplitWavesChronological(t *testing.T) {
	var msgs []Message
	for i := 0; i < 30; i++ {
		msgs = append(msgs, Message{
			Seq:         int64(i + 1),
			SpeakerName: "U",
			Text:        strings.Repeat("讨论内容", 50),
			At:          time.Now(),
		})
	}
	waves := SplitWaves(msgs, 400, 800, 4)
	if len(waves) < 2 {
		t.Fatalf("expected multiple waves, got %d", len(waves))
	}
	// Chronological: first wave starts at seq 1.
	if waves[0][0].Seq != 1 {
		t.Fatalf("wave0 first seq=%d", waves[0][0].Seq)
	}
	// Contiguous coverage.
	var n int
	for _, w := range waves {
		n += len(w)
	}
	if n != len(msgs) {
		t.Fatalf("covered %d of %d", n, len(msgs))
	}
}

func TestPreferOldestCursorSafeWithMaxSeq(t *testing.T) {
	// Simulates: unsummarized 1..20, budget only fits 1..8.
	// MaxSeq(kept)=8 must leave 9..20 as still-new (not skipped).
	var msgs []Message
	for i := 1; i <= 20; i++ {
		msgs = append(msgs, Message{
			Seq:         int64(i),
			SpeakerName: "U",
			Text:        strings.Repeat("内容", 30),
			At:          time.Now(),
		})
	}
	kept, dropped := PreferOldest(msgs, 200, 800)
	if dropped == 0 {
		t.Fatal("expected drop")
	}
	mark := MaxSeq(kept)
	for _, m := range msgs {
		if m.Seq <= mark {
			// would be "summarized"
			continue
		}
		// must be in the dropped tail only
		found := false
		for _, k := range kept {
			if k.Seq == m.Seq {
				found = true
			}
		}
		if found {
			t.Fatalf("seq %d kept but > mark %d", m.Seq, mark)
		}
	}
	if mark >= msgs[len(msgs)-1].Seq {
		t.Fatal("mark advanced past all content")
	}
}

func TestTruncateToTokenBudget(t *testing.T) {
	long := strings.Repeat("abcdefghij", 500)
	out := TruncateToTokenBudget(long, 50)
	if out == long {
		t.Fatal("expected truncation")
	}
	if !strings.Contains(out, "前文已省略") {
		t.Fatalf("missing ellipsis marker: %q", out[:min(40, len(out))])
	}
}

func TestMaxSeqAndTimeRange(t *testing.T) {
	t0 := time.Date(2026, 7, 16, 10, 0, 0, 0, time.Local)
	msgs := []Message{
		{Seq: 3, At: t0},
		{Seq: 7, At: t0.Add(time.Hour)},
		{Seq: 5, At: t0.Add(30 * time.Minute)},
	}
	if MaxSeq(msgs) != 7 {
		t.Fatalf("MaxSeq=%d", MaxSeq(msgs))
	}
	label := TimeRangeLabel(msgs)
	if label == "" || !strings.Contains(label, "10:00") {
		t.Fatalf("label=%q", label)
	}
}
