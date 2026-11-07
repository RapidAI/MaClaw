package agent

import (
	"strings"
	"testing"
)

func TestFillHandoffMemoTailKeepsEndingNumbers(t *testing.T) {
	measurement := strings.Repeat("噪声", 900) + " p50=12.5ms"
	memo := FillHandoffMemo(HandoffFill{
		Measurement: measurement,
		Modified:    []string{"D:/workprj/aicoder/corelib/agent/handoff_memo.go"},
		Verify:      "go test ./corelib/agent exit 0",
	})
	if runeLen(memo.Progress) > 1400 {
		t.Fatalf("progress runes=%d", runeLen(memo.Progress))
	}
	if !strings.Contains(memo.Progress, "p50=12.5ms") {
		t.Fatalf("progress dropped the ending measurement: %q", memo.Progress)
	}
	if !strings.Contains(memo.Progress, "handoff_memo.go") {
		t.Fatalf("progress dropped the path: %q", memo.Progress)
	}
	if !strings.Contains(memo.Progress, "exit 0") {
		t.Fatalf("progress dropped the verify line: %q", memo.Progress)
	}
	if strings.Contains(memo.Progress, HandoffRecoveryMarker) || strings.Contains(memo.Progress, HandoffInlineCutMarker) {
		t.Fatalf("progress gained a cut marker: %q", memo.Progress)
	}
	if !strings.HasPrefix(memo.Progress, "噪声") && strings.Contains(memo.Progress, strings.Repeat("噪声", 900)) {
		t.Fatalf("a 1400 tail cut kept the whole 1800-rune prefix")
	}
}

func TestHandoffTextEmptyBeforeExtraGroup(t *testing.T) {
	if !HandoffTextEmpty("") || !HandoffTextEmpty(HandoffRecoveryMarker) || !HandoffTextEmpty(HandoffInlineCutMarker) {
		t.Fatal("marker-only fields must count empty")
	}
	if !HandoffTextEmpty(HandoffRecoveryMarker + HandoffRecoveryMarker) {
		t.Fatal("a field that is only the recovery marker twice must count empty")
	}
	if !HandoffTextEmpty("  " + HandoffRecoveryMarker + "  ") {
		t.Fatal("trimmed marker-only field must count empty")
	}
	path := "foo.go" + HandoffRecoveryMarker
	if HandoffTextEmpty(path) {
		t.Fatal("a path bullet that still names a file is not empty")
	}
	empty := HandoffMemo{Task: HandoffRecoveryMarker, Progress: HandoffInlineCutMarker, Next: HandoffRecoveryMarker}
	if empty.Usable() {
		t.Fatal("marker-only memo must be unusable before keeping an extra group")
	}
	onlyShelf := HandoffMemo{ShelfHandles: []ShelfHandle{{Kind: "tool_result", ID: "h1"}}}
	if onlyShelf.Usable() {
		t.Fatal("shelf alone must not make a memo usable")
	}
	withPath := HandoffMemo{FilesRead: []string{path}}
	if !withPath.Usable() {
		t.Fatal("a real path must make the memo usable")
	}
}

func TestFillHandoffMemoPathBulletMayKeepMarker(t *testing.T) {
	long := strings.Repeat("p", 240) + ".go"
	memo := FillHandoffMemo(HandoffFill{Read: []string{long}})
	if len(memo.FilesRead) != 1 {
		t.Fatalf("paths=%v", memo.FilesRead)
	}
	got := memo.FilesRead[0]
	if runeLen(got) > 180 {
		t.Fatalf("path runes=%d", runeLen(got))
	}
	if !strings.HasSuffix(got, HandoffRecoveryMarker) {
		t.Fatalf("over-long path must keep the recovery marker: %q", got)
	}
	if HandoffTextEmpty(got) || !memo.Usable() {
		t.Fatalf("truncated path must still count: %q", got)
	}
}

func TestFillHandoffMemoRenderFitsSummaryCap(t *testing.T) {
	reads := make([]string, 40)
	for i := range reads {
		reads[i] = strings.Repeat("r", 40) + "/read_" + strings.Repeat("x", i%9) + ".go"
	}
	modified := make([]string, 40)
	for i := range modified {
		modified[i] = strings.Repeat("m", 40) + "/mod_" + strings.Repeat("y", i%9) + ".go"
	}
	created := make([]string, 25)
	for i := range created {
		created[i] = strings.Repeat("c", 40) + "/new_" + strings.Repeat("z", i%9) + ".go"
	}
	commands := make([]HandoffCommand, 12)
	for i := range commands {
		commands[i] = HandoffCommand{
			Tool:      "bash",
			Path:      "main.go",
			Command:   "go test ./pkg" + strings.Repeat("a", i),
			Succeeded: false,
			Outcome:   strings.Repeat("compiler log ", 80),
		}
	}
	memo := FillHandoffMemo(HandoffFill{
		UserTexts:   []string{strings.Repeat("任务", 500)},
		Read:        reads,
		Modified:    modified,
		Created:     created,
		Commands:    commands,
		Next:        strings.Repeat("下一步", 200),
		Measurement: strings.Repeat("测", 2000) + " p50=1",
		Handles: []ShelfHandle{
			{Kind: "tool_result", ID: "a", Note: strings.Repeat("n", 80)},
			{Kind: "file_range", ID: "b", Path: "a.go", Note: strings.Repeat("n", 80)},
			{Kind: "checkpoint", ID: "c", Note: strings.Repeat("n", 40)},
			{Kind: "document", ID: "d", Note: strings.Repeat("n", 40)},
			{Kind: "web", ID: "e", Note: strings.Repeat("n", 40)},
			{Kind: "tool_result", ID: "f", Note: strings.Repeat("n", 40)},
			{Kind: "tool_result", ID: "g", Note: strings.Repeat("n", 40)},
			{Kind: "tool_result", ID: "h", Note: strings.Repeat("n", 40)},
			{Kind: "tool_result", ID: "i", Note: strings.Repeat("n", 40)},
		},
	})
	rendered := memo.Render()
	if runeLen(rendered) > 5000 {
		t.Fatalf("render runes=%d", runeLen(rendered))
	}
	if strings.Contains(rendered, HandoffInlineCutMarker) {
		t.Fatalf("render contains the inline cut marker:\n%s", rendered)
	}
	note := memo.LengthNote()
	if strings.Contains(note, "p50=1") || strings.Contains(note, memo.Progress) && memo.Progress != "" {
		t.Fatalf("length note leaked progress: %s", note)
	}
}

func TestReplaceHandoffBlockOneBlock(t *testing.T) {
	inner := "task line"
	first := ReplaceHandoffBlock("do the work", inner)
	if strings.Count(first, handoffOpen) != 1 || strings.Count(first, handoffClose) != 1 {
		t.Fatalf("first replace: %q", first)
	}
	if !strings.HasPrefix(first, "do the work\n\n"+handoffOpen) {
		t.Fatalf("append shape: %q", first)
	}

	missingClose := "keep-me\n" + handoffOpen + "\nold body that never closed"
	second := ReplaceHandoffBlock(missingClose, "fresh")
	if strings.Count(second, handoffOpen) != 1 || strings.Count(second, handoffClose) != 1 {
		t.Fatalf("missing close appended a second block: %q", second)
	}
	if !strings.HasPrefix(second, "keep-me\n"+handoffOpen) {
		t.Fatalf("prefix before the open tag was dropped: %q", second)
	}
	if strings.Contains(second, "old body") {
		t.Fatalf("old unclosed body remained: %q", second)
	}

	third := ReplaceHandoffBlock(second, "newer")
	again := ReplaceHandoffBlock(third, "newest")
	if strings.Count(again, handoffOpen) != 1 || strings.Count(again, handoffClose) != 1 {
		t.Fatalf("third replace left more than one block: %q", again)
	}
	if !strings.Contains(again, "newest") || strings.Contains(again, "fresh") || strings.Contains(again, "newer") {
		t.Fatalf("latest inner was not the only body: %q", again)
	}

	withTail := "pre\n" + handoffOpen + "\nold\n" + handoffClose + "\npost"
	kept := ReplaceHandoffBlock(withTail, "mid")
	if !strings.HasPrefix(kept, "pre\n"+handoffOpen) || !strings.HasSuffix(kept, handoffClose+"\npost") {
		t.Fatalf("text outside the block moved: %q", kept)
	}
	if strings.Count(kept, handoffOpen) != 1 {
		t.Fatalf("tail replace duplicated the block: %q", kept)
	}
}

func TestPackTaskNewestConstraintsSurvive(t *testing.T) {
	newest := strings.Repeat("前", 700) + "KEEP-42"
	memo := FillHandoffMemo(HandoffFill{UserTexts: []string{
		strings.Repeat("旧", 400) + " OLD-END",
		"[上下文恢复] 不要把恢复条当任务",
		"[对话历史摘要] 也不要",
		"[系统通知] synthetic",
		handoffOpen + " already a memo",
		newest,
	}})
	if runeLen(memo.Task) > 600 {
		t.Fatalf("task runes=%d", runeLen(memo.Task))
	}
	if !strings.HasSuffix(memo.Task, "KEEP-42") {
		t.Fatalf("newest constraint was head-cut away: %q", memo.Task)
	}
	if strings.Contains(memo.Task, "OLD-END") || strings.Contains(memo.Task, "上下文恢复") || strings.Contains(memo.Task, "系统通知") {
		t.Fatalf("older or synthetic lines leaked into a full newest line: %q", memo.Task)
	}

	packed := FillHandoffMemo(HandoffFill{UserTexts: []string{
		"oldest ask",
		"middle ask",
		"newest ask stays",
	}})
	if !strings.HasSuffix(packed.Task, "newest ask stays") || !strings.Contains(packed.Task, "oldest ask") {
		t.Fatalf("short lines should stay newest-last: %q", packed.Task)
	}
}

func TestPackRemotePlannedKeepsVerifyPastNaiveHead(t *testing.T) {
	var b strings.Builder
	b.WriteString(strings.Repeat("测", 450))
	b.WriteString("\n")
	b.WriteString("D:\\workprj\\aicoder\\bench_out.txt\n")
	b.WriteString(strings.Repeat("compiler warning line\n", 30))
	b.WriteString("median 17.4 ms\n")
	b.WriteString(strings.Repeat("more bench log\n", 20))
	b.WriteString("go test ./corelib/agent exit 0\n")
	sum := b.String()
	naive := headCut(sum, 400)
	if strings.Contains(naive, "exit 0") || strings.Contains(naive, "17.4") {
		t.Fatal("fixture no longer puts the verify line past rune 400")
	}
	body := PackRemotePlannedFromSummary(sum)
	if runeLen(body) > 400 {
		t.Fatalf("body runes=%d", runeLen(body))
	}
	for _, want := range []string{"exit 0", "17.4", "bench_out.txt"} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %q", want, body)
		}
	}
	if strings.Contains(body, "compiler warning") || strings.Contains(body, "more bench log") {
		t.Fatalf("body kept the raw log: %q", body)
	}
	if strings.Contains(body, HandoffRecoveryMarker) || strings.Contains(body, HandoffInlineCutMarker) {
		t.Fatalf("body gained a cut marker: %q", body)
	}

	noAnchor := strings.Repeat("测", 500) + "TAILMARK"
	tail := PackRemotePlannedFromSummary(noAnchor)
	if runeLen(tail) > 400 || !strings.HasSuffix(tail, "TAILMARK") {
		t.Fatalf("no-anchor summary must tail-cut: %q", tail)
	}
}

func TestDeadEndRepeatIsStructural(t *testing.T) {
	log := "exit status 1\n" + strings.Repeat("compiler error detail\n", 40)
	memo := FillHandoffMemo(HandoffFill{
		UserTexts: []string{"不要重跑基准"},
		Commands: []HandoffCommand{
			{Tool: "bash", Path: "bench_test.go", Command: "go test ./bench", Succeeded: false, Outcome: log},
			{Tool: "bash", Path: "bench_test.go", Command: "go test ./bench -count=1", Succeeded: false, Outcome: log},
			{Tool: "bash", Path: "ok_test.go", Command: "go test ./corelib/agent", Succeeded: true, Outcome: "ok"},
		},
	})
	rendered := memo.renderDeadEnds()
	if !strings.Contains(rendered, "不要用同一方式再试") {
		t.Fatalf("repeated failure missing the structural prefix: %q", rendered)
	}
	if strings.Contains(rendered, "compiler error detail") {
		t.Fatalf("dead end kept the compiler log: %q", rendered)
	}
	if strings.Contains(rendered, "不要重跑基准") {
		t.Fatalf("user text became a dead end: %q", rendered)
	}
	if !strings.Contains(memo.Task, "不要重跑基准") {
		t.Fatalf("user text should stay on the task: %q", memo.Task)
	}
	if !strings.Contains(memo.Progress, "go test ./corelib/agent exit 0") {
		t.Fatalf("successful verify missing from progress: %q", memo.Progress)
	}
	if strings.Contains(memo.Progress, "compiler error") {
		t.Fatalf("progress kept the compiler log: %q", memo.Progress)
	}
	for _, end := range memo.DeadEnds {
		if strings.Contains(end.Attempt, "go test ./corelib/agent") {
			t.Fatalf("successful verify entered dead ends: %+v", end)
		}
	}
}

func TestUserTextDoesNotCreateDeadEnd(t *testing.T) {
	memo := FillHandoffMemo(HandoffFill{UserTexts: []string{"不要重跑基准，数字保持 17.4"}})
	if len(memo.DeadEnds) != 0 {
		t.Fatalf("dead ends=%+v", memo.DeadEnds)
	}
	fromDrop := MemoFromDroppedProjects([]interface{}{
		map[string]interface{}{"role": "user", "content": "不要重跑基准"},
		map[string]interface{}{"role": "tool", "name": "bash", "content": "ok"},
	}, HandoffMemo{}, false)
	if len(fromDrop.DeadEnds) != 0 {
		t.Fatalf("user sentence or a passing tool created a dead end: %+v", fromDrop.DeadEnds)
	}
}

func TestHandoffMemoMode(t *testing.T) {
	t.Setenv("MACLAW_HANDOFF_MEMO", "")
	t.Setenv("MACLAW_CODING_MEMO_PER_ROUND", "")
	if HandoffMemoMode() != "off" || HandoffMemoCoding() || HandoffMemoAll() || HandoffMemoPerRound() {
		t.Fatalf("unset mode=%s", HandoffMemoMode())
	}
	t.Setenv("MACLAW_HANDOFF_MEMO", "coding")
	t.Setenv("MACLAW_CODING_MEMO_PER_ROUND", "1")
	if !HandoffMemoCoding() || HandoffMemoAll() || !HandoffMemoPerRound() {
		t.Fatal("coding flag should enable the loop and the per-round switch")
	}
	t.Setenv("MACLAW_CODING_MEMO_PER_ROUND", "no")
	if HandoffMemoPerRound() {
		t.Fatal("per-round must stay off unless the value is 1, on, or true")
	}
	t.Setenv("MACLAW_HANDOFF_MEMO", "ALL")
	t.Setenv("MACLAW_CODING_MEMO_PER_ROUND", "true")
	if HandoffMemoMode() != "all" || !HandoffMemoCoding() || !HandoffMemoAll() || !HandoffMemoPerRound() {
		t.Fatalf("all mode=%s", HandoffMemoMode())
	}
	t.Setenv("MACLAW_HANDOFF_MEMO", "yes")
	if HandoffMemoMode() != "off" || HandoffMemoPerRound() {
		t.Fatal("unknown flag values are off, including per-round")
	}
	t.Setenv("MACLAW_HANDOFF_MEMO", "off")
	t.Setenv("MACLAW_CODING_MEMO_PER_ROUND", "1")
	if HandoffMemoPerRound() {
		t.Fatal("per-round must not fire when the main flag is off")
	}
}

func TestParseHandoffMemoRoundTrip(t *testing.T) {
	memo := FillHandoffMemo(HandoffFill{
		UserTexts: []string{"ship the memo"},
		Read:      []string{"a.go"},
		Modified:  []string{"b.go"},
		Created:   []string{"c.go"},
		Next:      "write tests",
		Commands: []HandoffCommand{{
			Tool: "bash", Command: "go test", Succeeded: false, Outcome: "exit status 1",
		}},
		Handles: []ShelfHandle{{Kind: "tool_result", ID: "h1", Path: "a.go", Offset: 10, Limit: 20, Note: "preview"}},
	})
	parsed, ok := ParseHandoffMemo(memo.RenderBlock())
	if !ok {
		t.Fatal("parse missed the block")
	}
	if parsed.Task != memo.Task || parsed.Next != memo.Next || parsed.Progress != memo.Progress {
		t.Fatalf("parsed=%+v want=%+v", parsed, memo)
	}
	if len(parsed.FilesRead) != 1 || parsed.FilesRead[0] != "a.go" || parsed.FilesModified[0] != "b.go" || parsed.FilesCreated[0] != "c.go" {
		t.Fatalf("files=%+v", parsed)
	}
	if len(parsed.DeadEnds) != 1 || !strings.Contains(parsed.DeadEnds[0].Outcome, "exit status 1") {
		t.Fatalf("dead=%+v", parsed.DeadEnds)
	}
	if len(parsed.ShelfHandles) != 1 || parsed.ShelfHandles[0].ID != "h1" || parsed.ShelfHandles[0].Offset != 10 || parsed.ShelfHandles[0].Limit != 20 {
		t.Fatalf("shelf=%+v", parsed.ShelfHandles)
	}
	if _, ok := ParseHandoffMemo("no tag here"); ok {
		t.Fatal("missing open tag should not parse")
	}
}

func TestFillHandoffMemoKeepsPriorAndDropsOldestFiles(t *testing.T) {
	prior := HandoffMemo{Task: "prior task", Progress: "prior progress 9", Next: "prior next"}
	kept := FillHandoffMemo(HandoffFill{Prior: prior})
	if kept.Task != "prior task" || kept.Progress != "prior progress 9" || kept.Next != "prior next" {
		t.Fatalf("empty delta dropped the prior memo: %+v", kept)
	}
	replaced := FillHandoffMemo(HandoffFill{Prior: prior, UserTexts: []string{"new task"}, Next: "new next", Measurement: "p50=3"})
	if replaced.Task != "new task" || !strings.Contains(replaced.Progress, "p50=3") || replaced.Next != "new next" {
		t.Fatalf("new anchors should replace prior fields: %+v", replaced)
	}

	pad := func(prefix string, n int) string {
		extra := n - runeLen(prefix) - len(".go")
		if extra < 0 {
			t.Fatalf("prefix %q longer than %d", prefix, n)
		}
		return prefix + strings.Repeat("x", extra) + ".go"
	}
	reads := make([]string, 5)
	for i := range reads {
		reads[i] = pad("read"+string(rune('A'+i)), 100)
	}
	modified := make([]string, 5)
	for i := range modified {
		modified[i] = pad("mod"+string(rune('A'+i)), 100)
	}
	created := []string{pad("old", 100), pad("mid", 100)}
	budget := FillHandoffMemo(HandoffFill{Read: reads, Modified: modified, Created: created})
	if budget.fileRunes() > 900 {
		t.Fatalf("file runes=%d", budget.fileRunes())
	}
	if len(budget.FilesRead) == 0 || budget.FilesRead[0] == reads[0] {
		t.Fatalf("oldest read should drop first: %v", budget.FilesRead)
	}
	if len(budget.FilesModified) != len(modified) {
		t.Fatalf("modified should stay while reads remain to drop: %v", budget.FilesModified)
	}
	longCreated := []string{pad("old", 400), pad("mid", 400), pad("new", 400)}
	onlyCreated := FillHandoffMemo(HandoffFill{Created: longCreated})
	if len(onlyCreated.FilesCreated) < 2 || onlyCreated.FilesCreated[0] == longCreated[0] {
		t.Fatalf("created should keep the newer paths: %v", onlyCreated.FilesCreated)
	}
}

func TestHandoffMemoStripCaps(t *testing.T) {
	commands := make([]HandoffCommand, 4)
	for i := range commands {
		commands[i] = HandoffCommand{Tool: "bash", Command: "fail" + string(rune('a'+i)), Succeeded: false, Outcome: "exit status 1"}
	}
	memo := FillHandoffMemo(HandoffFill{
		Strip:       true,
		UserTexts:   []string{strings.Repeat("任", 400) + "END"},
		Next:        strings.Repeat("下", 300),
		Measurement: strings.Repeat("进", 1200) + "n=9",
		Commands:    commands,
		Handles: []ShelfHandle{
			{Kind: "tool_result", ID: "1"},
			{Kind: "tool_result", ID: "2"},
			{Kind: "tool_result", ID: "3"},
			{Kind: "tool_result", ID: "4"},
		},
	})
	if runeLen(memo.Task) > 200 || !strings.HasSuffix(memo.Task, "END") {
		t.Fatalf("strip task=%d %q", runeLen(memo.Task), memo.Task)
	}
	if runeLen(memo.Progress) > 900 || !strings.Contains(memo.Progress, "n=9") {
		t.Fatalf("strip progress=%d %q", runeLen(memo.Progress), memo.Progress)
	}
	if runeLen(memo.Next) > 160 {
		t.Fatalf("strip next=%d", runeLen(memo.Next))
	}
	if len(memo.DeadEnds) > 2 || len(memo.ShelfHandles) > 3 {
		t.Fatalf("strip dead=%d shelf=%d", len(memo.DeadEnds), len(memo.ShelfHandles))
	}
}

func TestConversationHasPendingAskExport(t *testing.T) {
	pending := []interface{}{
		map[string]interface{}{"role": "user", "content": "do it"},
		map[string]interface{}{"role": "assistant", "content": "asking"},
		map[string]interface{}{"role": "tool", "content": "Asked user: which file?"},
	}
	if !ConversationHasPendingAsk(pending) {
		t.Fatal("pending ask should block a rewrite")
	}
	answered := append(pending, map[string]interface{}{"role": "user", "content": "the first one"})
	if ConversationHasPendingAsk(answered) {
		t.Fatal("a later user turn clears the pending ask")
	}
}

func TestHandoffCompactionPromptAsksForOneMemo(t *testing.T) {
	initial := HandoffCompactionPrompt("", "普通对话")
	for _, want := range []string{"<handoff-memo>", "## task", "## progress", "## files", "## next", "## dead_ends", "## shelf_handles", "普通对话"} {
		if !strings.Contains(initial, want) {
			t.Fatalf("initial prompt missing %q:\n%s", want, initial)
		}
	}
	if strings.Contains(initial, "保留上一份摘要") || strings.Contains(initial, "四个部分") || strings.Contains(initial, "当前进度") {
		t.Fatalf("initial prompt kept the old sections:\n%s", initial)
	}
	update := HandoffCompactionPrompt("median 17.4", "新进展")
	if !strings.Contains(update, "保留上一份摘要") || !strings.Contains(update, "median 17.4") || !strings.Contains(update, "<handoff-memo>") || strings.Contains(update, "仍使用这四个部分") {
		t.Fatalf("update prompt:\n%s", update)
	}
	parsed, ok := ParseHandoffMemo("<handoff-memo>\n## task\nship the report\n## progress\nmedian 17.4\n## files\nread:\n- D:\\work\\pkg\\a.go\n## next\nrun the suite\n## dead_ends\n- [ERR] go test: exit != 0\n## shelf_handles\n- checkpoint id=abc note=context_checkpoint\n</handoff-memo>")
	if !ok || !parsed.Usable() || !strings.Contains(parsed.Progress, "17.4") || len(parsed.FilesRead) != 1 || parsed.FilesRead[0] != `D:\work\pkg\a.go` {
		t.Fatalf("compliant memo rejected: ok=%v %+v", ok, parsed)
	}
}

func TestSummarizerPromptForMode(t *testing.T) {
	system, user := SummarizerPromptForMode("hello", false)
	if strings.Contains(user, "<handoff-memo>") || !strings.HasPrefix(user, "请压缩以下对话历史") || strings.Contains(system, "<handoff-memo>") {
		t.Fatalf("flag-off compress prompt changed:\n%s\n%s", system, user)
	}
	system, user = SummarizerPromptForMode("hello", true)
	if !strings.Contains(user, "<handoff-memo>") || !strings.Contains(user, "## dead_ends") || !strings.Contains(user, "## shelf_handles") || !strings.Contains(system, "<handoff-memo>") {
		t.Fatalf("flag-all compress prompt:\n%s\n%s", system, user)
	}
}

func TestNoteCoveredMemoCountsDeterministicAsFallback(t *testing.T) {
	before := CurrentHandoffMemoStats()
	NoteCoveredMemo(HandoffMemo{Progress: "median 17.4"}, false, false)
	mid := CurrentHandoffMemoStats()
	if mid.Applied != before.Applied || mid.Fallback != before.Fallback+1 {
		t.Fatalf("deterministic fill applied=%d fallback=%d", mid.Applied-before.Applied, mid.Fallback-before.Fallback)
	}
	NoteCoveredMemo(HandoffMemo{Progress: "median 17.4"}, true, false)
	after := CurrentHandoffMemoStats()
	if after.Applied != mid.Applied+1 || after.Fallback != mid.Fallback {
		t.Fatalf("llm memo applied=%d fallback=%d", after.Applied-mid.Applied, after.Fallback-mid.Fallback)
	}
}

func TestHandoffMemoStatsDoNotShareABody(t *testing.T) {
	before := CurrentHandoffMemoStats()
	NoteHandoffApplied()
	NoteHandoffFallback()
	NoteHandoffKeptExtraGroup()
	after := CurrentHandoffMemoStats()
	if after.Applied != before.Applied+1 || after.Fallback != before.Fallback+1 || after.KeptExtraGroup != before.KeptExtraGroup+1 {
		t.Fatalf("before=%+v after=%+v", before, after)
	}
}

func TestHandoffStripFromTextDropsTheLog(t *testing.T) {
	var b strings.Builder
	b.WriteString(strings.Repeat("测", 300))
	b.WriteString("\nD:/work/bench_out.txt\n")
	b.WriteString(strings.Repeat("compiler warning line\n", 8))
	b.WriteString("median 17.4 ms\n")
	b.WriteString("go test ./corelib/agent exit 0\n")
	strip := HandoffStripFromText(b.String() + HandoffRecoveryMarker)
	if !strings.Contains(strip, "17.4") || !strings.Contains(strip, "exit 0") || !strings.Contains(strip, "bench_out.txt") {
		t.Fatalf("strip=%q", strip)
	}
	if strings.Contains(strip, "compiler warning") || strings.Contains(strip, HandoffRecoveryMarker) {
		t.Fatalf("strip kept the log or the marker: %q", strip)
	}
	if HandoffStripFromText(HandoffRecoveryMarker) != "" {
		t.Fatal("marker-only text is not a strip")
	}
}

func TestSessionRoundPrefixConstant(t *testing.T) {
	if SessionRoundPrefix != "[系统续轮]" {
		t.Fatalf("prefix=%q", SessionRoundPrefix)
	}
}
