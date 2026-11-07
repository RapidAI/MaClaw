package guiapp

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/llm"
)

// codingAgentResumeFileName sits beside .coding_workbench.json in the task
// directory. It is the last root coding loop, not the chat transcript.
const codingAgentResumeFileName = ".coding_agent_state.json"

// codingAgentResume is the previous root coding loop. History is what the
// executor saw (user turn, tool calls, tool results, final answer). Snapshots
// are the files that loop had already read.
type codingAgentResume struct {
	History   []agent.ConversationEntry
	Snapshots map[string]codingFileSnapshot
	Memo      string
}

type codingAgentResumeDisk struct {
	ProjectPath   string                      `json:"project_path"`
	UpdatedAtUnix int64                       `json:"updated_at_unix"`
	History       []agent.ConversationEntry   `json:"history,omitempty"`
	Snapshots     []codingAgentResumeSnapDisk `json:"snapshots,omitempty"`
	Memo          string                      `json:"memo,omitempty"`
}

type codingAgentResumeSnapDisk struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Hash string `json:"hash"`
}

func codingAgentResumeStatePath(userID string) string {
	sticky := stickyCodingMemoryFilePath(userID)
	if sticky == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(sticky), codingAgentResumeFileName)
}

// loadCodingAgentResume reads the last root loop for this task. A different
// project, an expired file, or a missing file yields an empty resume. The
// caller still checks snapshot bytes before treating a file as read.
func loadCodingAgentResume(userID, projectPath string) codingAgentResume {
	userID = strings.TrimSpace(userID)
	projectPath = normalizeProjectSessionPath(projectPath)
	if userID == "" || projectPath == "" {
		return codingAgentResume{}
	}
	path := codingAgentResumeStatePath(userID)
	if path == "" {
		return codingAgentResume{}
	}
	var disk codingAgentResumeDisk
	var readErr error
	withStickyCodingUserLock(userID, func() {
		data, err := os.ReadFile(path)
		if err != nil {
			if !os.IsNotExist(err) {
				readErr = err
			}
			return
		}
		if err := json.Unmarshal(data, &disk); err != nil {
			readErr = err
			return
		}
		if disk.UpdatedAtUnix > 0 && time.Since(time.Unix(disk.UpdatedAtUnix, 0)) > stickyCodingMemoryMaxAge {
			disk = codingAgentResumeDisk{}
			if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
				readErr = err
			}
		}
	})
	if readErr != nil {
		log.Printf("[coding-env] load coding agent state failed path=%s err=%v", path, readErr)
		return codingAgentResume{}
	}
	if normalizeProjectSessionPath(disk.ProjectPath) != projectPath {
		return codingAgentResume{}
	}
	history := codingAgentResumeReady(disk.History)
	snaps := make(map[string]codingFileSnapshot, len(disk.Snapshots))
	for _, snap := range disk.Snapshots {
		key := canonicalCodingPath(snap.Path)
		if key == "" || snap.Hash == "" {
			continue
		}
		if !navigationWithinRoot(projectPath, key) {
			continue
		}
		snaps[key] = codingFileSnapshot{Size: snap.Size, Hash: snap.Hash}
	}
	if len(snaps) == 0 {
		snaps = nil
	}
	return codingAgentResume{History: history, Snapshots: snaps, Memo: disk.Memo}
}

// codingModelHistory is the transcript passed into codingagent.Run.
// Flag coding/all retires that replay. Nested and horizon turns never load
// a resume, so their history is already empty.
func codingModelHistory(resume codingAgentResume) []agent.ConversationEntry {
	if agent.HandoffMemoCoding() {
		return nil
	}
	return resume.History
}

func codingHistoryAsMessages(entries []agent.ConversationEntry) []interface{} {
	out := make([]interface{}, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.ToMessage())
	}
	return out
}

// codingResumeStatusStrip is what the next coding call sees instead of the
// stored tool transcript. A missing memo is projected from that transcript.
func codingResumeStatusStrip(resume codingAgentResume) string {
	if strings.TrimSpace(resume.Memo) != "" {
		if memo, ok := agent.ParseHandoffMemo(resume.Memo); ok {
			return agent.FillHandoffMemo(agent.HandoffFill{Prior: memo, Strip: true}).Render()
		}
		return agent.HandoffStripFromText(resume.Memo)
	}
	if len(resume.History) == 0 {
		return ""
	}
	projected := agent.MemoFromDroppedProjects(codingHistoryAsMessages(resume.History), agent.HandoffMemo{}, true)
	if !projected.Usable() {
		return ""
	}
	return projected.Render()
}

func appendCodingResumeStrip(userText, strip string) string {
	strip = strings.TrimSpace(strip)
	if strip == "" || strings.HasPrefix(strings.TrimSpace(userText), "[系统续接]") {
		return userText
	}
	return strings.TrimRight(userText, "\n") + "\n\n" + codingCompletedStepResultPrefix + strip
}

func codingMemoFromTurn(taskText string, read, modified, created []string, commands []CodingSubAgentCommandResult) string {
	handoff := make([]agent.HandoffCommand, 0, len(commands))
	for _, cmd := range commands {
		handoff = append(handoff, agent.HandoffCommand{
			Tool:      "bash",
			Path:      cmd.WorkingDir,
			Command:   cmd.Command,
			Succeeded: cmd.Succeeded,
			Outcome:   cmd.Summary,
		})
	}
	memo := agent.FillHandoffMemo(agent.HandoffFill{
		UserTexts: []string{taskText},
		Read:      read,
		Modified:  modified,
		Created:   created,
		Commands:  handoff,
	})
	if !memo.Usable() {
		return ""
	}
	return memo.RenderBlock()
}

// recordCodingAgentResume appends this loop to the task's agent transcript.
// An empty delta leaves the previous file in place, so a start failure
// cannot wipe a resume that a later reopen still needs.
func recordCodingAgentResume(userID, projectPath string, delta []agent.ConversationEntry, snaps map[string]codingFileSnapshot) {
	userID = strings.TrimSpace(userID)
	projectPath = normalizeProjectSessionPath(projectPath)
	if userID == "" || projectPath == "" || len(delta) == 0 {
		return
	}
	path := codingAgentResumeStatePath(userID)
	if path == "" {
		return
	}
	withStickyCodingUserLock(userID, func() {
		// HistoryDelta is only this loop. A later short turn must not replace
		// the tool transcript the next reopen still needs.
		var prior codingAgentResumeDisk
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &prior)
		}
		sameProject := normalizeProjectSessionPath(prior.ProjectPath) == projectPath
		freshEnough := prior.UpdatedAtUnix <= 0 || time.Since(time.Unix(prior.UpdatedAtUnix, 0)) <= stickyCodingMemoryMaxAge
		var base []agent.ConversationEntry
		if sameProject && freshEnough {
			base = prior.History
		}
		history := codingAgentResumeReady(append(append([]agent.ConversationEntry{}, base...), delta...))
		if len(history) == 0 {
			return
		}
		storedSnaps := snaps
		if len(storedSnaps) == 0 && sameProject && freshEnough {
			storedSnaps = codingAgentResumeSnapMap(prior.Snapshots)
		}
		memo := ""
		if sameProject && freshEnough {
			memo = prior.Memo
		}
		disk := codingAgentResumeDisk{
			ProjectPath:   projectPath,
			UpdatedAtUnix: time.Now().Unix(),
			History:       history,
			Memo:          memo,
		}
		for key, snap := range storedSnaps {
			key = canonicalCodingPath(key)
			if key == "" || snap.Hash == "" || !navigationWithinRoot(projectPath, key) {
				continue
			}
			disk.Snapshots = append(disk.Snapshots, codingAgentResumeSnapDisk{
				Path: key,
				Size: snap.Size,
				Hash: snap.Hash,
			})
		}
		data, err := json.Marshal(disk)
		if err != nil {
			log.Printf("[coding-env] encode coding agent state failed user=%s err=%v", userID, err)
			return
		}
		if err := atomicWriteFile(path, data); err != nil {
			log.Printf("[coding-env] persist coding agent state failed path=%s err=%v", path, err)
			return
		}
		log.Printf("[coding-env] stored coding agent state user=%s history=%d snapshots=%d", userID, len(history), len(disk.Snapshots))
	})
}

// recordCodingAgentResumeMemo stores a handoff memo without replacing the
// transcript or the read snapshots. An empty memo does not rewrite the file.
func recordCodingAgentResumeMemo(userID, projectPath, memo string) {
	userID = strings.TrimSpace(userID)
	projectPath = normalizeProjectSessionPath(projectPath)
	memo = strings.TrimSpace(memo)
	if userID == "" || projectPath == "" || memo == "" {
		return
	}
	path := codingAgentResumeStatePath(userID)
	if path == "" {
		return
	}
	withStickyCodingUserLock(userID, func() {
		var prior codingAgentResumeDisk
		if data, err := os.ReadFile(path); err == nil {
			_ = json.Unmarshal(data, &prior)
		}
		sameProject := normalizeProjectSessionPath(prior.ProjectPath) == projectPath
		freshEnough := prior.UpdatedAtUnix <= 0 || time.Since(time.Unix(prior.UpdatedAtUnix, 0)) <= stickyCodingMemoryMaxAge
		if !sameProject || !freshEnough {
			prior = codingAgentResumeDisk{ProjectPath: projectPath}
		}
		prior.ProjectPath = projectPath
		prior.UpdatedAtUnix = time.Now().Unix()
		prior.Memo = memo
		data, err := json.Marshal(prior)
		if err != nil {
			log.Printf("[coding-env] encode coding agent memo failed user=%s err=%v", userID, err)
			return
		}
		if err := atomicWriteFile(path, data); err != nil {
			log.Printf("[coding-env] persist coding agent memo failed path=%s err=%v", path, err)
		}
	})
}

func codingAgentResumeSnapMap(snaps []codingAgentResumeSnapDisk) map[string]codingFileSnapshot {
	if len(snaps) == 0 {
		return nil
	}
	out := make(map[string]codingFileSnapshot, len(snaps))
	for _, snap := range snaps {
		if snap.Path == "" || snap.Hash == "" {
			continue
		}
		out[snap.Path] = codingFileSnapshot{Size: snap.Size, Hash: snap.Hash}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func deleteCodingAgentResume(userID string) {
	userID = strings.TrimSpace(userID)
	path := codingAgentResumeStatePath(userID)
	if path == "" {
		return
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		log.Printf("[coding-env] delete coding agent state failed path=%s err=%v", path, err)
	}
}

// codingAgentResumeReady bounds one loop the same way chat history is bounded,
// and keeps a user turn at the front so the next request does not open on an
// assistant or tool row.
func codingAgentResumeReady(entries []agent.ConversationEntry) []agent.ConversationEntry {
	cleaned := make([]agent.ConversationEntry, 0, len(entries))
	for _, entry := range entries {
		// A host nudge is loop steering, not the task's agent state.
		if entry.HostContinuation {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(entry.Role)) {
		case "user", "assistant", "tool":
		default:
			continue
		}
		entry.Role = strings.ToLower(strings.TrimSpace(entry.Role))
		cleaned = append(cleaned, entry)
	}
	if len(cleaned) == 0 {
		return nil
	}
	// Host nudges are already gone, so a tool result that followed one is
	// adjacent to its assistant row and can stay with that call.
	cleaned = codingAgentResumeClosed(cleaned)
	if len(cleaned) == 0 {
		return nil
	}
	var firstUser string
	for _, entry := range cleaned {
		if entry.Role != "user" {
			continue
		}
		if text, ok := entry.Content.(string); ok {
			firstUser = strings.TrimSpace(text)
		}
		break
	}
	trimmed := agent.TrimHistory(cleaned)
	for len(trimmed) > 0 && trimmed[0].Role == "tool" {
		trimmed = trimmed[1:]
	}
	// A user row stays only when an assistant or tool row follows it before
	// the next user. The next root turn appends its own user message, and a
	// later model loop from this same turn also starts with a user message.
	// A loop that stored no assistant or tool output must not leave that
	// user row beside either of them.
	trimmed = codingAgentResumeDropBareUsers(trimmed)
	if len(trimmed) == 0 {
		return nil
	}
	if trimmed[0].Role != "user" {
		anchor := firstUser
		if anchor == "" {
			anchor = "Previous coding turn."
		}
		anchor = truncateRunesV2(anchor, 500)
		trimmed = append([]agent.ConversationEntry{{Role: "user", Content: anchor}}, trimmed...)
	}
	return trimmed
}

// codingAgentResumeAppendLoop attaches a later model loop from the same root
// turn. The host verification command is not a model loop and is not passed
// here. An empty extra leaves the base slice alone.
func codingAgentResumeAppendLoop(base, extra []agent.ConversationEntry) []agent.ConversationEntry {
	if len(extra) == 0 {
		return base
	}
	if len(base) == 0 {
		return extra
	}
	out := make([]agent.ConversationEntry, 0, len(base)+len(extra))
	out = append(out, base...)
	out = append(out, extra...)
	return out
}

// codingAgentResumeDropBareUsers removes a user row whose loop stored no
// assistant or tool output before the next user row.
func codingAgentResumeDropBareUsers(entries []agent.ConversationEntry) []agent.ConversationEntry {
	if len(entries) == 0 {
		return nil
	}
	out := make([]agent.ConversationEntry, 0, len(entries))
	for i, entry := range entries {
		if entry.Role == "user" && !codingAgentResumeUserHasOutput(entries, i) {
			continue
		}
		out = append(out, entry)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func codingAgentResumeUserHasOutput(entries []agent.ConversationEntry, i int) bool {
	for j := i + 1; j < len(entries); j++ {
		switch entries[j].Role {
		case "user":
			return false
		case "assistant", "tool":
			return true
		}
	}
	return false
}

// codingAgentResumeClosed keeps a tool call only when a tool result for that
// id was stored. The loop appends the assistant tool_calls row before tools
// run, and a pause or hard stop returns before the missing results are
// appended. Calls in the same batch that already have results stay. A tool
// row with no matching call is dropped.
func codingAgentResumeClosed(entries []agent.ConversationEntry) []agent.ConversationEntry {
	if len(entries) == 0 {
		return nil
	}
	groups := agent.BuildEntryGroups(entries)
	out := make([]agent.ConversationEntry, 0, len(entries))
	for _, g := range groups {
		chunk := entries[g.Start:g.End]
		head := chunk[0]
		if head.Role != "assistant" {
			if head.Role == "tool" {
				continue
			}
			out = append(out, head)
			continue
		}
		have := codingAgentResumeResultIDs(chunk[1:])
		keptCalls, keptIDs, recognized := codingAgentResumeMatchedToolCalls(head.ToolCalls, have)
		if !recognized || len(keptIDs) == 0 {
			if codingAgentResumeHasToolCalls(head.ToolCalls) {
				head.ToolCalls = nil
				if !codingAgentResumeAssistantVisible(head) {
					continue
				}
			}
			out = append(out, head)
			continue
		}
		head.ToolCalls = keptCalls
		out = append(out, head)
		seen := make(map[string]agent.ConversationEntry, len(keptIDs))
		for _, row := range chunk[1:] {
			id := strings.TrimSpace(row.ToolCallID)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = row
		}
		for _, id := range keptIDs {
			out = append(out, seen[id])
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func codingAgentResumeResultIDs(rows []agent.ConversationEntry) map[string]struct{} {
	if len(rows) == 0 {
		return nil
	}
	ids := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		id := strings.TrimSpace(row.ToolCallID)
		if id == "" {
			continue
		}
		ids[id] = struct{}{}
	}
	if len(ids) == 0 {
		return nil
	}
	return ids
}

func codingAgentResumeHasToolCalls(calls interface{}) bool {
	items, ok := codingAgentResumeToolCallItems(calls)
	if !ok {
		return calls != nil
	}
	return len(items) > 0
}

func codingAgentResumeAssistantVisible(entry agent.ConversationEntry) bool {
	switch v := entry.Content.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	default:
		return true
	}
}

func codingAgentResumeToolCallItems(calls interface{}) ([]interface{}, bool) {
	switch v := calls.(type) {
	case nil:
		return nil, true
	case []llm.ToolCall:
		items := make([]interface{}, len(v))
		for i := range v {
			items[i] = v[i]
		}
		return items, true
	case []interface{}:
		return v, true
	case []map[string]interface{}:
		items := make([]interface{}, len(v))
		for i := range v {
			items[i] = v[i]
		}
		return items, true
	default:
		data, err := json.Marshal(v)
		if err != nil || len(data) == 0 || data[0] != '[' {
			return nil, false
		}
		var items []interface{}
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, false
		}
		return items, true
	}
}

func codingAgentResumeToolCallID(item interface{}) string {
	switch v := item.(type) {
	case llm.ToolCall:
		return strings.TrimSpace(v.ID)
	case map[string]interface{}:
		id, _ := v["id"].(string)
		return strings.TrimSpace(id)
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		var m map[string]interface{}
		if err := json.Unmarshal(data, &m); err != nil {
			return ""
		}
		id, _ := m["id"].(string)
		return strings.TrimSpace(id)
	}
}

func codingAgentResumeToolCallIDs(calls interface{}) []string {
	items, ok := codingAgentResumeToolCallItems(calls)
	if !ok || len(items) == 0 {
		return nil
	}
	ids := make([]string, 0, len(items))
	for _, item := range items {
		if id := codingAgentResumeToolCallID(item); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

// codingAgentResumeMatchedToolCalls returns the calls whose ids have a stored
// result, in call order. The concrete slice type is preserved for the shapes
// the loop and a JSON reload actually produce.
func codingAgentResumeMatchedToolCalls(calls interface{}, have map[string]struct{}) (interface{}, []string, bool) {
	switch v := calls.(type) {
	case nil:
		return nil, nil, true
	case []llm.ToolCall:
		kept := make([]llm.ToolCall, 0, len(v))
		var ids []string
		seen := map[string]struct{}{}
		for _, tc := range v {
			id := strings.TrimSpace(tc.ID)
			if id == "" {
				continue
			}
			if _, ok := have[id]; !ok {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			kept = append(kept, tc)
			ids = append(ids, id)
		}
		if len(kept) == 0 {
			return nil, nil, true
		}
		return kept, ids, true
	case []interface{}:
		return codingAgentResumeFilterToolCallItems(v, have)
	case []map[string]interface{}:
		items := make([]interface{}, len(v))
		for i := range v {
			items[i] = v[i]
		}
		return codingAgentResumeFilterToolCallItems(items, have)
	default:
		items, ok := codingAgentResumeToolCallItems(calls)
		if !ok {
			return nil, nil, false
		}
		return codingAgentResumeFilterToolCallItems(items, have)
	}
}

func codingAgentResumeFilterToolCallItems(items []interface{}, have map[string]struct{}) (interface{}, []string, bool) {
	kept := make([]interface{}, 0, len(items))
	var ids []string
	seen := map[string]struct{}{}
	for _, item := range items {
		id := codingAgentResumeToolCallID(item)
		if id == "" {
			continue
		}
		if _, ok := have[id]; !ok {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		kept = append(kept, item)
		ids = append(ids, id)
	}
	if len(kept) == 0 {
		return nil, nil, true
	}
	return kept, ids, true
}

func (c *codingSubAgentCallbacks) exportCodingReadSnapshots() map[string]codingFileSnapshot {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.fileSnapshots) == 0 {
		return nil
	}
	out := make(map[string]codingFileSnapshot, len(c.fileSnapshots))
	for key, snap := range c.fileSnapshots {
		if snap.Hash == "" {
			continue
		}
		if c.filesRead != nil && !c.filesRead[key] {
			continue
		}
		out[key] = snap
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// installRestoredReadSnapshots marks files the previous loop already read,
// but only while the bytes are still the same. A changed file stays unread
// so the edit gate asks for a fresh lookup.
func (c *codingSubAgentCallbacks) installRestoredReadSnapshots(projectPath string, snaps map[string]codingFileSnapshot) int {
	if c == nil || len(snaps) == 0 {
		return 0
	}
	projectPath = normalizeProjectSessionPath(projectPath)
	kept := 0
	for path, prev := range snaps {
		if prev.Hash == "" || projectPath == "" || !navigationWithinRoot(projectPath, path) {
			continue
		}
		current, err := snapshotCodingFile(path)
		if err != nil || current != prev {
			continue
		}
		key := canonicalCodingPath(path)
		if key == "" {
			continue
		}
		c.mu.Lock()
		if c.filesRead == nil {
			c.filesRead = map[string]bool{}
		}
		if c.fileSnapshots == nil {
			c.fileSnapshots = map[string]codingFileSnapshot{}
		}
		c.filesRead[key] = true
		c.fileSnapshots[key] = current
		// The read happened in the previous process, before any edit this turn.
		if c.firstReadSeq == 0 {
			c.firstReadSeq = c.nextEventSeqLocked()
		}
		c.mu.Unlock()
		kept++
	}
	return kept
}
