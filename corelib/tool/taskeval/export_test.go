package taskeval

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// testdataPath resolves a file in testdata/ relative to this test file.
func testdataPath(t *testing.T, name string) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

// TestExportFromConversationStore exercises the full capture mapping on a
// fixture shaped exactly like a persisted corelib/agent ConversationMemory
// snapshot (see testdata/conversation-store.json and the export.go package
// doc for the schema evidence): user turns become Turn.UserText, following
// assistant tool_calls become deduped CalledTools, catalog-unknown names
// are omitted and counted, and the result passes taskeval validation.
func TestExportFromConversationStore(t *testing.T) {
	store, err := LoadConversationStore(testdataPath(t, "conversation-store.json"))
	if err != nil {
		t.Fatalf("LoadConversationStore: %v", err)
	}
	if ids := store.SessionIDs(); len(ids) != 2 || ids[0] != "desktop-user" || ids[1] != "proj-tab" {
		t.Fatalf("session ids=%v, want [desktop-user proj-tab]", ids)
	}

	ds, stats, err := ExportFromConversationStore(store, "desktop-user", nil)
	if err != nil {
		t.Fatalf("ExportFromConversationStore: %v", err)
	}
	if stats.SessionID != "desktop-user" {
		t.Fatalf("stats.SessionID=%q, want desktop-user", stats.SessionID)
	}
	if stats.UserTurnsSeen != 4 {
		t.Fatalf("user_turns_seen=%d, want 4 (whitespace-only turn still seen)", stats.UserTurnsSeen)
	}
	if stats.TurnsExported != 3 {
		t.Fatalf("turns_exported=%d, want 3 (whitespace-only turn skipped)", stats.TurnsExported)
	}
	if stats.CallsTotal != 6 {
		t.Fatalf("calls_total=%d, want 6", stats.CallsTotal)
	}
	if stats.UnknownCallsOmitted != 1 {
		t.Fatalf("unknown_calls_omitted=%d, want 1 (the dynamic MCP adapter name)", stats.UnknownCallsOmitted)
	}

	if len(ds.Transcript) != 3 {
		t.Fatalf("transcript turns=%d, want 3", len(ds.Transcript))
	}
	turn1 := ds.Transcript[0]
	if turn1.UserText != "帮我看一下 corelib/tool/router.go 的评分逻辑" {
		t.Fatalf("turn1 user_text=%q", turn1.UserText)
	}
	// bash appeared twice in the fixture and read_file across two assistant
	// entries: deduped, order preserved.
	if len(turn1.CalledTools) != 2 || turn1.CalledTools[0] != "bash" || turn1.CalledTools[1] != "read_file" {
		t.Fatalf("turn1 called_tools=%v, want [bash read_file]", turn1.CalledTools)
	}
	// Multimodal user content: only the text part is captured.
	turn2 := ds.Transcript[1]
	if turn2.UserText != "查一下 Go 1.24 的 release notes" {
		t.Fatalf("turn2 user_text=%q, want the text part only", turn2.UserText)
	}
	if len(turn2.CalledTools) != 1 || turn2.CalledTools[0] != "web_search" {
		t.Fatalf("turn2 called_tools=%v, want [web_search] (unknown MCP name omitted)", turn2.CalledTools)
	}
	// Final turn: assistant answered without tool calls → cost-only turn.
	turn3 := ds.Transcript[2]
	if turn3.UserText != "现在几点了" || len(turn3.CalledTools) != 0 {
		t.Fatalf("turn3=%+v, want cost-only 现在几点了 turn", turn3)
	}
	// The exporter never emits simulated_intent.
	for _, turn := range ds.Transcript {
		if turn.SimulatedIntent != nil {
			t.Fatalf("exporter emitted simulated_intent: %+v", turn.SimulatedIntent)
		}
	}
	// Round-trip: the exported dataset must pass taskeval validation and be
	// replayable.
	if err := validate(ds); err != nil {
		t.Fatalf("exported dataset fails validation: %v", err)
	}
	res := RunWithSelector(ds, nil, "fixture")
	if res.Turns != 3 {
		t.Fatalf("replay turns=%d, want 3", res.Turns)
	}
}

// TestExportSessionSelection covers the session-selection rules: an empty
// sessionID resolves only a single-session store, multi-session stores
// require an explicit ID, and unknown IDs are rejected with the available
// list.
func TestExportSessionSelection(t *testing.T) {
	store, err := LoadConversationStore(testdataPath(t, "conversation-store.json"))
	if err != nil {
		t.Fatalf("LoadConversationStore: %v", err)
	}
	if _, _, err := ExportFromConversationStore(store, "", nil); err == nil {
		t.Fatal("empty sessionID on a 2-session store: want error")
	}
	ds, stats, err := ExportFromConversationStore(store, "proj-tab", nil)
	if err != nil {
		t.Fatalf("explicit session: %v", err)
	}
	if stats.TurnsExported != 1 || ds.Transcript[0].CalledTools[0] != "bash" {
		t.Fatalf("proj-tab export=%+v / %+v", stats, ds.Transcript)
	}
	if _, _, err := ExportFromConversationStore(store, "nope", nil); err == nil {
		t.Fatal("unknown sessionID: want error")
	}

	single, err := LoadConversationStore(testdataPath(t, "conversation-empty.json"))
	if err != nil {
		t.Fatalf("LoadConversationStore: %v", err)
	}
	if _, _, err := ExportFromConversationStore(single, "", nil); err == nil {
		t.Fatal("session with no user turns: want error (dataset would fail validation)")
	}
}

// TestExportFile exercises the CLI-friendly entry point end to end: capture
// a real-shaped store file to a dataset file, verify it is written 0600,
// loads with RunFile, and reports stats.
func TestExportFile(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "out.json")
	stats, err := ExportFile(testdataPath(t, "conversation-store.json"), "desktop-user", dst, nil)
	if err != nil {
		t.Fatalf("ExportFile: %v", err)
	}
	if stats.TurnsExported != 3 || stats.CallsTotal != 6 || stats.UnknownCallsOmitted != 1 {
		t.Fatalf("stats=%+v", stats)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("stat exported file: %v", err)
	}
	// POSIX mode bits are not enforced on Windows; check them where they
	// exist (the file holds real user text, so 0600 is requested).
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Fatalf("exported file mode=%v, want 0600 (real user text inside)", info.Mode().Perm())
	}
	res, err := RunFile(dst)
	if err != nil {
		t.Fatalf("RunFile on exported dataset: %v", err)
	}
	if res.Turns != 3 {
		t.Fatalf("replay turns=%d, want 3", res.Turns)
	}
}

// TestEntryText pins the content-extraction rules directly.
func TestEntryText(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		ok   bool
	}{
		{`"hello"`, "hello", true},
		{`"  "`, "", false},
		{`[{"type":"text","text":"a"},{"type":"image_url","image_url":{}}]`, "a", true},
		{`[{"type":"text","text":"第一段"},{"type":"text","text":"第二段"}]`, "第一段\n第二段", true},
		{`[{"type":"text","text":""},{"type":"text","text":"b"}]`, "b", true},
		{`[{"type":"image_url","image_url":{}}]`, "", false},
		{`not json`, "", false},
		{``, "", false},
	}
	for i, c := range cases {
		got, ok := entryText([]byte(c.raw))
		if got != c.want || ok != c.ok {
			t.Fatalf("case %d: entryText(%s)=%q,%v want %q,%v", i, c.raw, got, ok, c.want, c.ok)
		}
	}
}
