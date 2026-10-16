// Transcript capture source 2: the guiapp IM session persistence.
//
// Durable store: the IM session search SQLite DB at <dataDir>/
// session_search.db (guiapp/app.go:3928-3931 sessionSearchDBPath). After
// every IM agent-loop turn, persistSessionTranscriptAsync
// (guiapp/im_history_persistence.go:583-637) converts the conversation
// history to []session.TranscriptEntry via conversationToTranscriptEntries
// (:641-675) — extracting ToolCalls → session.ToolCallMeta with the called
// tool NAME via extractToolCallMeta (:679-713) — serializes it with
// session.Serialize (corelib/session/serializer.go:29-80) and stores it in
// the `sessions` table (corelib/session/store.go:71-77:
//
//	sessions(session_id TEXT PRIMARY KEY, timestamp TEXT, platform TEXT,
//	         topic TEXT, full_text TEXT)
//
// with an FTS5 shadow table for search. In full_text the serialized format
// records, per turn, the [user] text block and one [tool_call:ID name:NAME]
// block per assistant tool call, so the (user_text, [called tool names])
// pair this harness needs is durably present — session.Deserialize
// (corelib/session/serializer.go:83-106) reconstructs the entry stream.
//
// Layer decision: this converter lives in package guiapp, NOT in
// corelib/tool/taskeval — guiapp imports corelib/tool (e.g.
// acp_permission.go), so taskeval importing guiapp would create an import
// cycle. It reuses taskeval's public Dataset/Turn/ExportStats types and
// Validate, and decodes the store through corelib/session's own
// Serialize/Deserialize pair (no schema mirror is needed: the session
// package owns this format and is importable from here).
//
// Mapping rules are IDENTICAL to taskeval's source 1
// (corelib/tool/taskeval/export.go ExportFromConversationStore):
//
//   - role "user" entries start a new Turn; their text content becomes
//     Turn.UserText. Entries with empty or whitespace-only text are skipped
//     (still counted in ExportStats.UserTurnsSeen).
//   - Assistant entries following a user entry contribute their
//     tool_calls[].name to that turn's CalledTools, deduplicated, order
//     preserved. Assistant entries before the first user entry are dropped.
//   - Called tool names not present in the catalog are OMITTED from
//     CalledTools and counted in ExportStats.UnknownCallsOmitted.
//   - simulated_intent is never emitted (real sessions carry no ground-truth
//     intent labels), and a session yielding zero turns is an error.
package guiapp

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/session"
	"github.com/RapidAI/CodeClaw/corelib/tool/surfaceeval"
	"github.com/RapidAI/CodeClaw/corelib/tool/taskeval"
)

// IMSession is one persisted IM session of an IMSessionStore: the
// deserialized transcript entries of one `sessions` row.
type IMSession struct {
	Entries []session.TranscriptEntry
}

// IMSessionStore is the parsed content of the IM session search DB: one map
// of session ID to its deserialized transcript entries. It mirrors
// taskeval.ConversationStore for source 1.
type IMSessionStore struct {
	Sessions map[string]IMSession
}

// LoadIMSessionStore reads and parses the IM session search DB at dbPath
// (e.g. <dataDir>/session_search.db). Every stored session's full_text is
// deserialized with the same session.Deserialize the search UI path uses.
func LoadIMSessionStore(dbPath string) (*IMSessionStore, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("guiapp: read im session store %q: %w", dbPath, err)
	}
	store, err := session.NewStore(dbPath)
	if err != nil {
		return nil, fmt.Errorf("guiapp: open im session store %q: %w", dbPath, err)
	}
	fullTexts, err := store.AllFullTexts()
	_ = store.Close()
	if err != nil {
		return nil, fmt.Errorf("guiapp: read im session store %q: %w", dbPath, err)
	}
	if len(fullTexts) == 0 {
		return nil, fmt.Errorf("guiapp: im session store %q has no sessions", dbPath)
	}
	out := &IMSessionStore{Sessions: make(map[string]IMSession, len(fullTexts))}
	for id, text := range fullTexts {
		entries, err := session.Deserialize(text)
		if err != nil {
			return nil, fmt.Errorf("guiapp: parse im session %q in %q: %w", id, dbPath, err)
		}
		out.Sessions[id] = IMSession{Entries: entries}
	}
	return out, nil
}

// SessionIDs returns the sorted session IDs of the store.
func (s *IMSessionStore) SessionIDs() []string {
	ids := make([]string, 0, len(s.Sessions))
	for id := range s.Sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ExportFromIMSessionStore converts one session of a parsed IM session
// store into a taskeval Dataset. sessionID selects the session; an empty
// sessionID resolves to the store's only session (an error lists the
// available IDs when there is more than one). A nil or empty catalog uses
// surfaceeval.DefaultCatalog. A session that yields zero turns is an error:
// the dataset would fail taskeval validation, so the exporter refuses to
// produce it. Stats semantics are identical to taskeval's
// ExportFromConversationStore (source 1).
func ExportFromIMSessionStore(store *IMSessionStore, sessionID string, catalog []surfaceeval.CatalogTool) (*taskeval.Dataset, taskeval.ExportStats, error) {
	var stats taskeval.ExportStats
	if store == nil || len(store.Sessions) == 0 {
		return nil, stats, fmt.Errorf("guiapp: im session store is empty")
	}
	if sessionID == "" {
		ids := store.SessionIDs()
		if len(ids) != 1 {
			return nil, stats, fmt.Errorf("guiapp: im session store has %d sessions, specify one of: %s", len(ids), strings.Join(ids, ", "))
		}
		sessionID = ids[0]
	}
	imSession, ok := store.Sessions[sessionID]
	if !ok {
		return nil, stats, fmt.Errorf("guiapp: session %q not in im session store (have: %s)", sessionID, strings.Join(store.SessionIDs(), ", "))
	}
	stats.SessionID = sessionID

	if len(catalog) == 0 {
		catalog = surfaceeval.DefaultCatalog()
	}
	known := make(map[string]bool, len(catalog))
	for _, t := range catalog {
		known[t.Name] = true
	}

	var turns []taskeval.Turn
	var pending *taskeval.Turn
	flush := func() {
		if pending != nil {
			turns = append(turns, *pending)
			pending = nil
		}
	}
	for _, entry := range imSession.Entries {
		switch entry.Role {
		case "user":
			flush()
			stats.UserTurnsSeen++
			text, ok := imEntryText(entry.Content)
			if !ok {
				continue
			}
			pending = &taskeval.Turn{UserText: text}
		case "assistant":
			if pending == nil {
				continue
			}
			for _, tc := range entry.ToolCalls {
				name := strings.TrimSpace(tc.Name)
				if name == "" {
					continue
				}
				stats.CallsTotal++
				if !known[name] {
					stats.UnknownCallsOmitted++
					continue
				}
				seen := false
				for _, existing := range pending.CalledTools {
					if existing == name {
						seen = true
						break
					}
				}
				if !seen {
					pending.CalledTools = append(pending.CalledTools, name)
				}
			}
		default:
			// role "tool", "system", ...: no turn semantics of their own.
		}
	}
	flush()

	if len(turns) == 0 {
		return nil, stats, fmt.Errorf("guiapp: session %q exported 0 turns (no user turns with non-empty text)", sessionID)
	}
	stats.TurnsExported = len(turns)
	ds := &taskeval.Dataset{Version: taskeval.DatasetVersion, Transcript: turns}
	if err := taskeval.Validate(ds); err != nil {
		return nil, stats, fmt.Errorf("guiapp: exported dataset fails validation: %w", err)
	}
	return ds, stats, nil
}

// imEntryText extracts the textual content of a persisted IM transcript
// entry. The IM writer (conversationToTranscriptEntries,
// im_history_persistence.go:641-675) stores string content verbatim and
// marshals multimodal content to a JSON part array, so a payload that
// decodes as a part array with typed elements is read as parts (only
// "text" parts contribute); anything else is plain user text. The second
// return value is false when nothing textual remains after trimming —
// identical semantics to taskeval's entryText for source 1.
func imEntryText(content string) (string, bool) {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "", false
	}
	if strings.HasPrefix(trimmed, "[") {
		var parts []map[string]interface{}
		if err := json.Unmarshal([]byte(trimmed), &parts); err == nil && imLooksLikeParts(parts) {
			var b strings.Builder
			first := true
			for _, part := range parts {
				if typ, _ := part["type"].(string); typ != "text" {
					continue
				}
				text, _ := part["text"].(string)
				if !first {
					b.WriteString("\n")
				}
				first = false
				b.WriteString(text)
			}
			out := strings.TrimSpace(b.String())
			return out, out != ""
		}
	}
	return trimmed, true
}

// imLooksLikeParts reports whether a decoded JSON array has the shape of a
// multimodal content-part array (every element an object carrying a string
// "type"), distinguishing a marshaled part array from user text that merely
// happens to start with "[".
func imLooksLikeParts(parts []map[string]interface{}) bool {
	if len(parts) == 0 {
		return false
	}
	for _, part := range parts {
		if part == nil {
			return false
		}
		if _, ok := part["type"].(string); !ok {
			return false
		}
	}
	return true
}

// ExportIMFile is the CLI-friendly capture entry point for source 2: it
// reads the IM session search DB (e.g. <dataDir>/session_search.db), exports
// one session to a taskeval dataset JSON at dstPath and returns the export
// statistics. sessionID "" resolves to the only session. The written file
// is validated before return, so it is guaranteed to load with
// taskeval.RunFile. The file is written 0600: it contains real user text.
func ExportIMFile(srcPath, sessionID, dstPath string, catalog []surfaceeval.CatalogTool) (*taskeval.ExportStats, error) {
	store, err := LoadIMSessionStore(srcPath)
	if err != nil {
		return nil, err
	}
	ds, stats, err := ExportFromIMSessionStore(store, sessionID, catalog)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("guiapp: encode dataset: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(dstPath, data, 0600); err != nil {
		return nil, fmt.Errorf("guiapp: write dataset %q: %w", dstPath, err)
	}
	return &stats, nil
}
