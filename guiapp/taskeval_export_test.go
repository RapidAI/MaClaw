package guiapp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/session"
	"github.com/RapidAI/CodeClaw/corelib/tool/taskeval"
)

// canonicalIMConversation is the one logical conversation both capture
// sources are fed in TestExportFromIMSessionStoreParity. It exercises every
// mapping rule: a pre-first-user assistant call (dropped), duplicate tool
// names (deduped, order preserved), a catalog-unknown MCP adapter name
// (omitted + counted), a whitespace-only user turn (seen, skipped), a
// multimodal user message (text part only), and a final cost-only turn.
var canonicalIMConversation = []struct {
	role      string
	content   string
	toolCalls []string
}{
	{role: "assistant", toolCalls: []string{"bash"}}, // before first user: dropped
	{role: "user", content: "帮我看一下 corelib/tool/router.go 的评分逻辑"},
	{role: "assistant", toolCalls: []string{"bash", "bash"}},
	{role: "assistant", toolCalls: []string{"read_file"}},
	{role: "tool", content: "file contents here"},
	{role: "assistant", content: "评分逻辑在 routeWithOptions。"},
	{role: "user", content: "   "}, // whitespace-only: seen, not exported
	{role: "user", content: `[{"type":"text","text":"查一下 Go 1.24 的 release notes"},{"type":"image_url","image_url":{}}]`},
	{role: "assistant", toolCalls: []string{"web_search", "mcp_adapter_x"}},
	{role: "user", content: "现在几点了"},
	{role: "assistant", content: "现在是下午三点。"},
}

// canonicalIMStats is the ExportStats both sources must produce for
// canonicalIMConversation. CallsTotal counts every non-empty tool-call name
// attached to an exported turn (bash×2 + read_file + web_search +
// mcp_adapter_x = 5); the pre-first-user call and the whitespace turn's
// (nonexistent) calls are not counted.
var canonicalIMStats = taskeval.ExportStats{
	SessionID:           "im-user",
	UserTurnsSeen:       4,
	TurnsExported:       3,
	CallsTotal:          5,
	UnknownCallsOmitted: 1,
}

// persistIMFixtureDB writes sessions to a real SQLite session-search DB
// through the same corelib/session writer path the IM post-conversation
// hook uses (session.Serialize + Store.Persist).
func persistIMFixtureDB(t *testing.T, sessions map[string][]session.TranscriptEntry) string {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "session_search.db")
	store, err := session.NewStore(dbPath)
	if err != nil {
		t.Fatalf("session.NewStore: %v", err)
	}
	for id, entries := range sessions {
		doc := session.SessionDocument{
			SessionID: id,
			Timestamp: time.Now(),
			Platform:  "gui",
			Topic:     session.ExtractTopic(session.Serialize(entries)),
			FullText:  session.Serialize(entries),
		}
		if err := store.Persist(doc); err != nil {
			t.Fatalf("Persist(%s): %v", id, err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	return dbPath
}

// canonicalIMEntries renders canonicalIMConversation as IM transcript
// entries (the shape conversationToTranscriptEntries produces).
func canonicalIMEntries() []session.TranscriptEntry {
	var out []session.TranscriptEntry
	callIdx := 0
	for _, e := range canonicalIMConversation {
		te := session.TranscriptEntry{Role: e.role, Content: e.content}
		for _, name := range e.toolCalls {
			callIdx++
			te.ToolCalls = append(te.ToolCalls, session.ToolCallMeta{
				ID:   "call_" + string(rune('0'+callIdx)),
				Name: name,
			})
		}
		out = append(out, te)
	}
	return out
}

// canonicalConversationStore renders canonicalIMConversation as a source-1
// taskeval ConversationStore (OpenAI-style persisted entries).
func canonicalConversationStore() *taskeval.ConversationStore {
	type tc struct {
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	}
	entries := make([]taskeval.ConversationEntry, 0, len(canonicalIMConversation))
	callIdx := 0
	for _, e := range canonicalIMConversation {
		ce := taskeval.ConversationEntry{Role: e.role}
		if e.role == "user" && e.content != "   " && e.content != "" {
			// Plain user text is a JSON string; the multimodal message stays
			// a raw part array, exactly as ConversationMemory persists it.
			if e.content[0] == '[' {
				ce.Content = json.RawMessage(e.content)
			} else {
				raw, _ := json.Marshal(e.content)
				ce.Content = json.RawMessage(raw)
			}
		} else if e.content != "" {
			raw, _ := json.Marshal(e.content)
			ce.Content = json.RawMessage(raw)
		}
		for _, name := range e.toolCalls {
			callIdx++
			var raw json.RawMessage
			b, _ := json.Marshal(tc{
				ID:   "call_" + string(rune('0'+callIdx)),
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: name, Arguments: "{}"},
			})
			raw = b
			var convTC taskeval.ConversationToolCall
			if err := json.Unmarshal(raw, &convTC); err != nil {
				panic(err)
			}
			ce.ToolCalls = append(ce.ToolCalls, convTC)
		}
		entries = append(entries, ce)
	}
	return &taskeval.ConversationStore{
		Sessions: map[string]taskeval.ConversationSession{
			"im-user": {Entries: entries},
		},
	}
}

// TestExportFromIMSessionStoreParity proves the second capture source has
// IDENTICAL mapping semantics to source 1: the same logical conversation,
// persisted once through the real IM writer path (Serialize + SQLite) and
// once as a ConversationMemory snapshot, yields identical ExportStats and
// an identical transcript, both validating and replaying.
func TestExportFromIMSessionStoreParity(t *testing.T) {
	dbPath := persistIMFixtureDB(t, map[string][]session.TranscriptEntry{
		"im-user": canonicalIMEntries(),
	})
	store, err := LoadIMSessionStore(dbPath)
	if err != nil {
		t.Fatalf("LoadIMSessionStore: %v", err)
	}
	if ids := store.SessionIDs(); len(ids) != 1 || ids[0] != "im-user" {
		t.Fatalf("session ids=%v, want [im-user]", ids)
	}

	ds2, stats2, err := ExportFromIMSessionStore(store, "im-user", nil)
	if err != nil {
		t.Fatalf("ExportFromIMSessionStore: %v", err)
	}
	if stats2 != canonicalIMStats {
		t.Fatalf("source-2 stats=%+v, want %+v", stats2, canonicalIMStats)
	}

	// Source 1 on the same conversation.
	ds1, stats1, err := taskeval.ExportFromConversationStore(canonicalConversationStore(), "im-user", nil)
	if err != nil {
		t.Fatalf("ExportFromConversationStore: %v", err)
	}
	if stats1 != canonicalIMStats {
		t.Fatalf("source-1 stats=%+v, want %+v", stats1, canonicalIMStats)
	}
	if !reflect.DeepEqual(ds1.Transcript, ds2.Transcript) {
		t.Fatalf("transcripts differ:\nsource1=%+v\nsource2=%+v", ds1.Transcript, ds2.Transcript)
	}

	// The exporter never emits simulated_intent.
	for _, turn := range ds2.Transcript {
		if turn.SimulatedIntent != nil {
			t.Fatalf("exporter emitted simulated_intent: %+v", turn.SimulatedIntent)
		}
	}
	// Round-trip: validate + replay through the real router.
	if err := taskeval.Validate(ds2); err != nil {
		t.Fatalf("exported dataset fails validation: %v", err)
	}
	res := taskeval.RunWithSelector(ds2, nil, "im-fixture")
	if res.Turns != 3 {
		t.Fatalf("replay turns=%d, want 3", res.Turns)
	}

	// Mapping details worth pinning explicitly.
	turn1 := ds2.Transcript[0]
	if turn1.UserText != "帮我看一下 corelib/tool/router.go 的评分逻辑" {
		t.Fatalf("turn1 user_text=%q", turn1.UserText)
	}
	if !reflect.DeepEqual(turn1.CalledTools, []string{"bash", "read_file"}) {
		t.Fatalf("turn1 called_tools=%v, want [bash read_file] (deduped, order preserved)", turn1.CalledTools)
	}
	turn2 := ds2.Transcript[1]
	if turn2.UserText != "查一下 Go 1.24 的 release notes" || !reflect.DeepEqual(turn2.CalledTools, []string{"web_search"}) {
		t.Fatalf("turn2=%+v, want multimodal text + [web_search] (unknown MCP name omitted)", turn2)
	}
	turn3 := ds2.Transcript[2]
	if turn3.UserText != "现在几点了" || len(turn3.CalledTools) != 0 {
		t.Fatalf("turn3=%+v, want cost-only 现在几点了 turn", turn3)
	}
}

// TestExportIMSessionSelection covers the session-selection and degenerate
// session rules, mirroring source 1: an empty sessionID resolves only a
// single-session store, multi-session stores require an explicit ID, unknown
// IDs are rejected with the available list, and a session with no usable
// user turns is an error.
func TestExportIMSessionSelection(t *testing.T) {
	dbPath := persistIMFixtureDB(t, map[string][]session.TranscriptEntry{
		"im-user":  canonicalIMEntries(),
		"im-user2": {{Role: "assistant", Content: "hi"}},
	})
	store, err := LoadIMSessionStore(dbPath)
	if err != nil {
		t.Fatalf("LoadIMSessionStore: %v", err)
	}
	if _, _, err := ExportFromIMSessionStore(store, "", nil); err == nil {
		t.Fatal("empty sessionID on a 2-session store: want error")
	}
	ds, stats, err := ExportFromIMSessionStore(store, "im-user", nil)
	if err != nil {
		t.Fatalf("explicit session: %v", err)
	}
	if stats != canonicalIMStats {
		t.Fatalf("stats=%+v, want %+v", stats, canonicalIMStats)
	}
	if _, _, err := ExportFromIMSessionStore(store, "nope", nil); err == nil {
		t.Fatal("unknown sessionID: want error")
	}
	if _, _, err := ExportFromIMSessionStore(store, "im-user2", nil); err == nil {
		t.Fatal("session with no user turns: want error (dataset would fail validation)")
	}
	if ds.Version != taskeval.DatasetVersion {
		t.Fatalf("dataset version=%d, want %d", ds.Version, taskeval.DatasetVersion)
	}

	if _, err := LoadIMSessionStore(filepath.Join(t.TempDir(), "missing.db")); err == nil {
		t.Fatal("missing DB path: want error")
	}
}

// TestExportIMFile exercises the CLI-friendly entry point end to end:
// capture a real-shaped IM session DB to a dataset file, verify it is
// written 0600, loads with taskeval.RunFile, and reports stats.
func TestExportIMFile(t *testing.T) {
	dbPath := persistIMFixtureDB(t, map[string][]session.TranscriptEntry{
		"im-user": canonicalIMEntries(),
	})
	dst := filepath.Join(t.TempDir(), "out.json")
	stats, err := ExportIMFile(dbPath, "", dst, nil)
	if err != nil {
		t.Fatalf("ExportIMFile: %v", err)
	}
	if *stats != canonicalIMStats {
		t.Fatalf("stats=%+v, want %+v", *stats, canonicalIMStats)
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
	res, err := taskeval.RunFile(dst)
	if err != nil {
		t.Fatalf("RunFile on exported dataset: %v", err)
	}
	if res.Turns != 3 {
		t.Fatalf("replay turns=%d, want 3", res.Turns)
	}
}

// TestImEntryText pins the content-extraction rules, mirroring source 1's
// TestEntryText plus the IM-store ambiguity case (user text that merely
// starts with "[" must survive verbatim).
func TestImEntryText(t *testing.T) {
	cases := []struct {
		content string
		want    string
		ok      bool
	}{
		{"hello", "hello", true},
		{"  ", "", false},
		{"[{\"type\":\"text\",\"text\":\"a\"},{\"type\":\"image_url\",\"image_url\":{}}]", "a", true},
		{"[{\"type\":\"text\",\"text\":\"第一段\"},{\"type\":\"text\",\"text\":\"第二段\"}]", "第一段\n第二段", true},
		{"[{\"type\":\"text\",\"text\":\"\"},{\"type\":\"text\",\"text\":\"b\"}]", "b", true},
		{"[{\"type\":\"image_url\",\"image_url\":{}}]", "", false},
		{"[1,2,3]", "[1,2,3]", true}, // not a part array: plain user text
		{"[]", "[]", true},           // empty array: plain user text
		{"[not json", "[not json", true},
	}
	for i, c := range cases {
		got, ok := imEntryText(c.content)
		if got != c.want || ok != c.ok {
			t.Fatalf("case %d: imEntryText(%q)=%q,%v want %q,%v", i, c.content, got, ok, c.want, c.ok)
		}
	}
}
