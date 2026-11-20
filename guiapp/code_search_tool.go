package guiapp

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/codesearch"
)

func warmCodeSearch(root string) {
	codesearch.Warm(root)
}

func keepCodeSearchWarm(root string) func() {
	if strings.TrimSpace(root) == "" {
		return nil
	}
	return codesearch.KeepWarm(root)
}

const codeSearchRouteZH = "不知道名字、问的是行为或模块在哪时用 code_search。已知符号和调用边仍用 code_navigation。精确文本用 ripgrep。"
const codeSearchRouteEN = "When the name is unknown and the question is about behavior or which module owns it, use code_search. Known symbols and call edges stay on code_navigation. Exact text stays on ripgrep."

func buildCodeSearchToolDefinition() map[string]interface{} {
	return buildRemoteToolDef(codeSearchToolName,
		"Search the project by behavior or module when you do not know the symbol name. Returns ranked code chunks with path and line range. Use code_navigation for a known symbol, definition, or call edge. Use ripgrep for an exact string or regex. A hit is a snippet, not a source read, and does not unlock edit_file.",
		map[string]interface{}{
			"query": map[string]interface{}{"type": "string", "description": "Behavior, module, or concept to find"},
			"path":  map[string]interface{}{"type": "string", "description": "Optional project directory or file to search"},
			"top_k": map[string]interface{}{"type": "number", "description": "Optional hit count, default 8, max 20"},
		}, []string{"query"})
}

func (c *codingSubAgentCallbacks) executeLocalCodeSearch(args map[string]interface{}, argsJSON string) codingToolExecutionResult {
	query := strings.TrimSpace(fmt.Sprint(args["query"]))
	if query == "" || query == "<nil>" {
		return codingToolExecutionResult{Text: "query is required", Outcome: codingToolOutcomeFailed}
	}
	root := c.projectPath()
	if root == "" {
		return codingToolExecutionResult{Text: "project path unavailable", Outcome: codingToolOutcomeFailed}
	}
	searchArgs := c.withDefaultProjectPath(args)
	p, _ := searchArgs["path"].(string)
	if strings.TrimSpace(p) != "" {
		if reply := c.requireProjectReadScope(p, codeSearchToolName); reply.rejected() {
			return c.rejectProjectScope(codeSearchToolName, searchArgs, reply)
		}
	}
	// A relative path joined onto the project can name a directory that is not
	// there (the project folder's own name is the usual case). Searching it
	// would be an indexed miss for the whole tree. Tell the caller to drop it.
	if raw, _ := args["path"].(string); strings.TrimSpace(raw) != "" {
		info, err := os.Stat(p)
		if err != nil || info == nil || (!info.IsDir() && !info.Mode().IsRegular()) {
			return codingToolExecutionResult{
				Text:    "path not found: " + strings.TrimSpace(raw) + ". Omit path to search the project.",
				Outcome: codingToolOutcomeFailed,
			}
		}
	}
	sub := ""
	if strings.TrimSpace(p) != "" {
		slash, ok := codeSearchRelative(root, p)
		if !ok {
			return codingToolExecutionResult{Text: c.rejectToolCall(codeSearchToolName, searchArgs, "path escapes the project"), Outcome: codingToolOutcomeBlocked}
		}
		if slash != "." {
			sub = slash
		}
	}
	topK := 0
	if n, ok := codingSubAgentArgumentIntegerValue(args["top_k"]); ok {
		topK = int(n)
	}
	res := codesearch.Search(codesearch.Request{
		Root:  root,
		Path:  sub,
		Query: query,
		TopK:  topK,
		Progress: func(phase string, done, total int) {
			c.emitCodeSearchCatchup(argsJSON, done, total)
		},
	})
	text := codesearch.Encode(res)
	c.trackSearchResult(codeSearchToolName, searchArgs, text, res.Freshness == "indexed")
	return codingToolExecutionResult{Text: text, Outcome: codingToolOutcomeSuccess}
}

// emitCodeSearchCatchup writes the open code_search row's note. It is not a
// second search. The row stays tool_started; the UI folds this into that row.
func (c *codingSubAgentCallbacks) emitCodeSearchCatchup(argsJSON string, done, total int) {
	if c == nil || c.subagent == nil || c.subagent.onProgress == nil {
		return
	}
	if done <= 0 || total <= 0 {
		return
	}
	if done != 1 && done != total && done%8 != 0 {
		return
	}
	title := ""
	if c.task != nil {
		title = compactSubAgentTaskTitle(c.task.Title)
	}
	event := newCodingAgentTaskEvent(codingAgentEventPhaseRunning, c.task, title, "")
	event.Event = codingAgentEventKindToolProgress.String()
	event.Detail = codeSearchToolName
	event.Command = codingToolEventCommand(codeSearchToolName, argsJSON)
	event.Target = codingToolEventTarget(codeSearchToolName, argsJSON, c.projectPath())
	event.Summary = fmt.Sprintf("已跟上 %d/%d 个改动", done, total)
	event.Count = done
	emitCodingAgentEvent(c.subagent.onProgress, event)
}

// codeSearchRelative returns the slash path of abs inside root. On Windows the
// scope gate resolves the real casing, then filepath.Rel still rejects a
// different-case copy of the same path. A case-insensitive prefix is the same
// containment check after Clean has removed "..".
func codeSearchRelative(root, abs string) (string, bool) {
	root = filepath.Clean(root)
	abs = filepath.Clean(abs)
	if rel, err := filepath.Rel(root, abs); err == nil {
		slash := filepath.ToSlash(rel)
		if slash == "." || (slash != ".." && !strings.HasPrefix(slash, "../")) {
			return slash, true
		}
	}
	if runtime.GOOS != "windows" {
		return "", false
	}
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	absSlash := filepath.ToSlash(abs)
	if strings.EqualFold(absSlash, rootSlash) {
		return ".", true
	}
	if len(absSlash) <= len(rootSlash) || absSlash[len(rootSlash)] != '/' || !strings.EqualFold(absSlash[:len(rootSlash)], rootSlash) {
		return "", false
	}
	rel := absSlash[len(rootSlash)+1:]
	if rel == "" || rel == "." {
		return ".", true
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false
	}
	return rel, true
}
