// Transcript capture: converting REAL persisted sessions into taskeval
// datasets. The chosen source is the corelib/agent ConversationMemory
// snapshot file — the durable per-host conversation store written by the TUI
// (~/.maclaw/data/tui_conversation.json, tui/app.go:183-187), the rpc/pipe
// TUI modes, guiapp and the ACP bridge, all sharing one format:
//
//	{"sessions": {"<sessionID>": {"entries": [
//	  {"role": "user", "content": "..."},
//	  {"role": "assistant", "content": "...",
//	   "tool_calls": [{"id": "...", "type": "function",
//	                   "function": {"name": "bash", "arguments": "..."}}]},
//	  {"role": "tool", "tool_call_id": "...", "content": "..."},
//	  ...
//	], "last_access": "..."}}}
//
// (schema evidence: corelib/agent/conversation_memory.go:52-67
// ConversationEntry, :185-198 persistedSession, :213-215 memorySnapshot;
// corelib/agent/loop.go:2155-2161 commits assistant entries with
// choice.Message.ToolCalls; corelib/llm/types.go:56-65 ToolCall JSON shape;
// corelib/agent/conversation_memory_redaction_test.go:40 shows the exact
// persisted JSON.) Assistant entries durably record the tool names the model
// called, paired with the user entry that precedes them — the honest
// (user_text, [called tool names]) source this harness needs.
//
// The store types below are local mirrors of that schema rather than imports
// of corelib/agent: corelib/agent imports corelib/tool, so taskeval (inside
// corelib/tool) importing corelib/agent would create an import cycle. The
// mirrors decode only the fields capture needs.
//
// Usage (no CLI convention exists for the eval harnesses — they are library
// + test packages — so capture is a plain function):
//
//	stats, err := taskeval.ExportFile(
//	    "~/.maclaw/data/tui_conversation.json", // srcPath
//	    "",                                     // sessionID: "" = the only session
//	    "out.json",                             // dstPath: taskeval dataset
//	    nil)                                    // catalog: nil = DefaultCatalog
//	// then: report, err := taskeval.RunFile("out.json")
//
// Mapping rules (pinned):
//
//   - role "user" entries start a new Turn; their text content becomes
//     Turn.UserText. Entries with empty or non-text content (multimodal
//     parts carrying no text, whitespace-only strings) are skipped.
//   - Assistant entries following a user entry contribute their
//     tool_calls[].function.name to that turn's CalledTools, deduplicated,
//     order preserved. Assistant entries before the first user entry are
//     dropped.
//   - Called tool names not present in the catalog are OMITTED from
//     CalledTools and counted in ExportStats.UnknownCallsOmitted — taskeval
//     validation rejects unknown names, and a high omitted-call count in
//     real data is itself a signal (dynamic MCP adapter names, model
//     hallucinations, catalog drift).
//   - simulated_intent is never emitted: real sessions carry no ground-truth
//     intent labels, so exported conditional-tool turns grade fail-closed.
//   - Injected user-role recovery/nudge prompts are persisted indistinguish-
//     able from real user text and are exported as turns; that is honest to
//     what the router saw at request time.
package taskeval

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool/surfaceeval"
)

// ConversationStore is a parsed corelib/agent ConversationMemory snapshot
// file: one map of session ID to its persisted conversation entries.
type ConversationStore struct {
	Sessions map[string]ConversationSession `json:"sessions"`
}

// ConversationSession is one persisted session of a ConversationStore.
type ConversationSession struct {
	Entries []ConversationEntry `json:"entries"`
}

// ConversationEntry mirrors the persisted fields of
// corelib/agent.ConversationEntry that capture needs. Content stays raw so
// both plain-string and multimodal-array payloads decode.
type ConversationEntry struct {
	Role      string                 `json:"role"`
	Content   json.RawMessage        `json:"content"`
	ToolCalls []ConversationToolCall `json:"tool_calls,omitempty"`
	ToolName  string                 `json:"tool_name,omitempty"`
}

// ConversationToolCall mirrors the persisted OpenAI-style tool_calls shape
// (corelib/llm/types.go:56-65); only the function name is captured.
type ConversationToolCall struct {
	Function ConversationToolCallFunction `json:"function"`
}

// ConversationToolCallFunction is the function payload of one persisted
// tool call.
type ConversationToolCallFunction struct {
	Name string `json:"name"`
}

// LoadConversationStore reads and parses a ConversationMemory snapshot file
// (e.g. ~/.maclaw/data/tui_conversation.json).
func LoadConversationStore(path string) (*ConversationStore, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("taskeval: read conversation store %q: %w", path, err)
	}
	var store ConversationStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("taskeval: parse conversation store %q: %w", path, err)
	}
	if len(store.Sessions) == 0 {
		return nil, fmt.Errorf("taskeval: conversation store %q has no sessions", path)
	}
	return &store, nil
}

// SessionIDs returns the sorted session IDs of the store.
func (s *ConversationStore) SessionIDs() []string {
	ids := make([]string, 0, len(s.Sessions))
	for id := range s.Sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ExportStats reports what the converter saw and what it dropped.
type ExportStats struct {
	// SessionID is the session that was exported (resolved when the caller
	// passed "").
	SessionID string `json:"session_id"`
	// UserTurnsSeen is the number of user-role entries encountered.
	UserTurnsSeen int `json:"user_turns_seen"`
	// TurnsExported is the number of turns written to the dataset (user
	// turns with extractable non-empty text).
	TurnsExported int `json:"turns_exported"`
	// CallsTotal is every tool call observed across exported turns,
	// including the omitted ones: kept = CallsTotal - UnknownCallsOmitted.
	CallsTotal int `json:"calls_total"`
	// UnknownCallsOmitted is the number of called tool names absent from
	// the catalog and therefore dropped from CalledTools. A high value in
	// real data is a signal in itself (dynamic MCP adapter names, model
	// hallucinations, catalog drift).
	UnknownCallsOmitted int `json:"unknown_calls_omitted"`
}

// ExportFromConversationStore converts one session of a parsed conversation
// store into a taskeval Dataset. sessionID selects the session; an empty
// sessionID resolves to the store's only session (an error lists the
// available IDs when there is more than one). A nil or empty catalog uses
// surfaceeval.DefaultCatalog. A session that yields zero turns is an error:
// the dataset would fail taskeval validation, so the exporter refuses to
// write a file that cannot be replayed.
func ExportFromConversationStore(store *ConversationStore, sessionID string, catalog []surfaceeval.CatalogTool) (*Dataset, ExportStats, error) {
	var stats ExportStats
	if store == nil || len(store.Sessions) == 0 {
		return nil, stats, fmt.Errorf("taskeval: conversation store is empty")
	}
	if sessionID == "" {
		ids := store.SessionIDs()
		if len(ids) != 1 {
			return nil, stats, fmt.Errorf("taskeval: store has %d sessions, specify one of: %s", len(ids), strings.Join(ids, ", "))
		}
		sessionID = ids[0]
	}
	session, ok := store.Sessions[sessionID]
	if !ok {
		return nil, stats, fmt.Errorf("taskeval: session %q not in store (have: %s)", sessionID, strings.Join(store.SessionIDs(), ", "))
	}
	stats.SessionID = sessionID

	if len(catalog) == 0 {
		catalog = surfaceeval.DefaultCatalog()
	}
	known := make(map[string]bool, len(catalog))
	for _, t := range catalog {
		known[t.Name] = true
	}

	var turns []Turn
	var pending *Turn
	flush := func() {
		if pending != nil {
			turns = append(turns, *pending)
			pending = nil
		}
	}
	for _, entry := range session.Entries {
		switch entry.Role {
		case "user":
			flush()
			stats.UserTurnsSeen++
			text, ok := entryText(entry.Content)
			if !ok {
				continue
			}
			pending = &Turn{UserText: text}
		case "assistant":
			if pending == nil {
				continue
			}
			for _, tc := range entry.ToolCalls {
				name := strings.TrimSpace(tc.Function.Name)
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
		return nil, stats, fmt.Errorf("taskeval: session %q exported 0 turns (no user turns with non-empty text)", sessionID)
	}
	stats.TurnsExported = len(turns)
	ds := &Dataset{Version: DatasetVersion, Transcript: turns}
	if err := validate(ds); err != nil {
		return nil, stats, fmt.Errorf("taskeval: exported dataset fails validation: %w", err)
	}
	return ds, stats, nil
}

// entryText extracts the textual content of a persisted conversation entry.
// Content is persisted either as a plain string or as a multimodal part
// array; parts of type "text" contribute their text. The second return
// value is false when nothing textual remains after trimming.
func entryText(raw json.RawMessage) (string, bool) {
	if len(raw) == 0 {
		return "", false
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		s = strings.TrimSpace(s)
		return s, s != ""
	}
	var parts []map[string]interface{}
	if err := json.Unmarshal(raw, &parts); err != nil {
		return "", false
	}
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

// ExportFile is the CLI-friendly capture entry point: it reads a
// ConversationMemory snapshot (e.g. ~/.maclaw/data/tui_conversation.json),
// exports one session to a taskeval dataset JSON at dstPath and returns the
// export statistics. sessionID "" resolves to the only session. The written
// file is validated before return, so it is guaranteed to load with RunFile.
// The file is written 0600: it contains real user text.
func ExportFile(srcPath, sessionID, dstPath string, catalog []surfaceeval.CatalogTool) (*ExportStats, error) {
	store, err := LoadConversationStore(srcPath)
	if err != nil {
		return nil, err
	}
	ds, stats, err := ExportFromConversationStore(store, sessionID, catalog)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(ds, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("taskeval: encode dataset: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(dstPath, data, 0600); err != nil {
		return nil, fmt.Errorf("taskeval: write dataset %q: %w", dstPath, err)
	}
	return &stats, nil
}
