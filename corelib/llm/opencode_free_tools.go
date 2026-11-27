package llm

import (
	"encoding/json"
	"math"
	"strings"
)

// openCodeFreeToolTargets maps official OpenCode tools onto compatible Maclaw
// executors. execute and official names without a Maclaw equivalent stay unchanged.
var openCodeFreeToolTargets = map[string]string{
	"edit":      "edit_file",
	"glob":      "Glob",
	"grep":      "ripgrep",
	"shell":     "bash",
	"read":      "read_file",
	"write":     "write_file",
	"webfetch":  "web_fetch",
	"websearch": "web_search",
}

// PresentOpenCodeFreeMessage rewrites one free-model assistant message for
// the client that sent the request. HubCenter and CodexProxy both return
// this shape. A Maclaw caller receives the corresponding local executor for
// shell/read/write/edit/glob/grep/webfetch/websearch. A caller that already
// declared the official name, such as Codex shell, keeps that name. Thought text that the model also copied
// into content is left in reasoning_content when a tool call is present.
func PresentOpenCodeFreeMessage(callerTools []string, msg Message) Message {
	msg.ToolCalls = AdaptOpenCodeFreeToolCalls(callerTools, msg.ToolCalls)
	return normalizeOpenCodeFreeText(msg)
}

// NormalizeOpenCodeFreeChoice applies the text and finish-reason policy
// without renaming tools. Responses-API models keep the tool names they
// were called with.
func NormalizeOpenCodeFreeChoice(choice Choice) Choice {
	choice.Message = normalizeOpenCodeFreeText(choice.Message)
	choice.FinishReason = OpenCodeFreeFinishReason(choice.FinishReason, choice.Message.ToolCalls)
	return choice
}

func normalizeOpenCodeFreeText(msg Message) Message {
	if len(msg.ToolCalls) > 0 && strings.TrimSpace(msg.Content) == strings.TrimSpace(msg.ReasoningContent) {
		msg.Content = ""
	}
	msg.Content, msg.ReasoningContent = OpenCodeFreeVisibleText(msg.Content, msg.ReasoningContent, msg.ToolCalls)
	return msg
}

// PresentOpenCodeFreeChoice is the proxy response boundary. A stop finish
// with tool calls is not runnable for an OpenAI client; those calls need
// finish_reason tool_calls.
func PresentOpenCodeFreeChoice(callerTools []string, choice Choice) Choice {
	choice.Message = PresentOpenCodeFreeMessage(callerTools, choice.Message)
	choice.FinishReason = OpenCodeFreeFinishReason(choice.FinishReason, choice.Message.ToolCalls)
	return choice
}

// OpenCodeFreeFinishReason reports tool_calls when a stop/empty finish still
// carries tool calls. length and content_filter stay as the upstream sent them.
func OpenCodeFreeFinishReason(finish string, calls []ToolCall) string {
	finish = strings.TrimSpace(finish)
	if len(calls) > 0 && (finish == "" || finish == "stop") {
		return "tool_calls"
	}
	if finish == "" {
		return "stop"
	}
	return finish
}

// OpenCodeFreeVisibleText keeps a text answer in content. The same paragraph
// is not also copied into reasoning_content. A tool-call turn keeps the
// thought in reasoning_content and leaves content empty.
func OpenCodeFreeVisibleText(content, reasoning string, calls []ToolCall) (string, string) {
	if strings.TrimSpace(content) == "" && len(calls) == 0 {
		content = reasoning
	}
	if strings.TrimSpace(reasoning) == strings.TrimSpace(content) {
		reasoning = ""
	}
	return content, reasoning
}

// openCodeFreeHostToOfficialToolTargets is the inverse of
// openCodeFreeToolTargets for replaying a stored host call to the free
// upstream. The upstream model only has the official OpenCode names in its
// function catalog. Replaying the post-adaptation Maclaw names would teach
// it to imitate names that are not callable, which is how tool syntax leaks
// into visible content on the next turn.
var openCodeFreeHostToOfficialToolTargets = func() map[string]string {
	inverse := make(map[string]string, len(openCodeFreeToolTargets))
	for official, host := range openCodeFreeToolTargets {
		inverse[strings.ToLower(host)] = official
	}
	return inverse
}()

// openCodeFreeOfficialReplayCall converts a host-side tool call back into
// the official upstream namespace before it is flattened into the OpenCode
// transcript. The adapter rewrites official calls to Maclaw executors for
// local execution; the wire replay must cross the same boundary in the
// opposite direction so the model sees one consistent tool namespace.
func openCodeFreeOfficialReplayCall(name, argsJSON string) (string, string, bool) {
	official, ok := openCodeFreeHostToOfficialToolTargets[strings.ToLower(strings.TrimSpace(name))]
	if !ok {
		return name, argsJSON, false
	}
	return official, rewriteOpenCodeFreeReplayArguments(official, argsJSON), true
}

var openCodeFreeOfficialReplayArgKeys = loadOpenCodeFreeOfficialReplayArgKeys()

func loadOpenCodeFreeOfficialReplayArgKeys() map[string]map[string]struct{} {
	var tools []struct {
		Function struct {
			Name       string `json:"name"`
			Parameters struct {
				Properties map[string]json.RawMessage `json:"properties"`
			} `json:"parameters"`
		} `json:"function"`
	}
	if err := json.Unmarshal([]byte(openCodeFreeToolsJSON), &tools); err != nil {
		return nil
	}
	out := make(map[string]map[string]struct{}, len(tools))
	for _, tool := range tools {
		name := strings.TrimSpace(tool.Function.Name)
		if name == "" {
			continue
		}
		keys := make(map[string]struct{}, len(tool.Function.Parameters.Properties))
		for key := range tool.Function.Parameters.Properties {
			keys[key] = struct{}{}
		}
		out[name] = keys
	}
	return out
}

func filterOpenCodeFreeOfficialReplayArgs(official string, args map[string]interface{}) {
	allowed, ok := openCodeFreeOfficialReplayArgKeys[official]
	if !ok {
		return
	}
	for key := range args {
		if _, keep := allowed[key]; !keep {
			delete(args, key)
		}
	}
}

func rewriteOpenCodeFreeReplayArguments(official, argsJSON string) string {
	raw := strings.TrimSpace(argsJSON)
	if raw == "" {
		return argsJSON
	}
	var args map[string]interface{}
	if json.Unmarshal([]byte(raw), &args) != nil || args == nil {
		return argsJSON
	}
	switch official {
	case "shell":
		renameOpenCodeArg(args, "working_dir", "workdir")
		if timeout, ok := openCodeFreeNumericArg(args["timeout"]); ok && timeout > 0 {
			milliseconds := timeout * 1000
			if milliseconds >= 1<<62 {
				delete(args, "timeout")
			} else {
				args["timeout"] = int64(math.Round(milliseconds))
			}
		}
	case "read":
		// Maclaw read_file also has an offset key, but there it means
		// "tail the last N lines"; official read.offset is the 1-based
		// start row. Only a host start_line may cross the boundary. A
		// host-native tail offset cannot be represented and must not be
		// replayed with the opposite meaning.
		if startLine, exists := args["start_line"]; exists {
			args["offset"] = startLine
			delete(args, "start_line")
		} else {
			delete(args, "offset")
		}
		renameOpenCodeArg(args, "lines", "limit")
	case "edit":
		renameOpenCodeArg(args, "old_string", "oldString")
		renameOpenCodeArg(args, "new_string", "newString")
		renameOpenCodeArg(args, "replace_all", "replaceAll")
	case "glob":
		renameOpenCodeArg(args, "include_hidden", "hidden")
		renameOpenCodeArg(args, "max_results", "limit")
	case "grep":
		renameOpenCodeArg(args, "glob", "include")
		renameOpenCodeArg(args, "fixed_string", "literal")
		renameOpenCodeArg(args, "case_sensitive", "caseSensitive")
		renameOpenCodeArg(args, "max_results", "limit")
		if _, exists := args["caseSensitive"]; !exists {
			// Maclaw ripgrep defaults to case-insensitive while official grep
			// defaults to case-sensitive. Replay the actual host behavior.
			args["caseSensitive"] = false
		}
	case "webfetch":
		// Host web_fetch historically accepts up to 600s, while the official
		// schema caps timeout at 120s. Replaying a larger host value would teach
		// the model an argument its own catalog declares invalid.
		if timeout, ok := openCodeFreeNumericArg(args["timeout"]); ok {
			if timeout <= 0 {
				delete(args, "timeout")
			} else if timeout > 120 {
				args["timeout"] = 120
			}
		}
	}
	filterOpenCodeFreeOfficialReplayArgs(official, args)
	encoded, err := json.Marshal(args)
	if err != nil {
		return argsJSON
	}
	return string(encoded)
}

func openCodeFreeNumericArg(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		n, err := v.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}

// AdaptOpenCodeFreeToolCalls maps official OpenCode tool calls onto the
// caller's executors. An empty caller list uses the Maclaw names, which is
// what the GUI runs when the planner never rendered those tools.
func AdaptOpenCodeFreeToolCalls(callerTools []string, calls []ToolCall) []ToolCall {
	return adaptOpenCodeFreeToolCalls(callerTools, calls, "")
}

// AdaptOpenCodeFreeToolCallsForTurn applies the same rewrite, using the
// host phase already written into this turn's user text. A goal action from
// the other phase is not a delivery of this turn.
func AdaptOpenCodeFreeToolCallsForTurn(userText string, callerTools []string, calls []ToolCall) []ToolCall {
	phase, _ := openCodeFreeGoalPhaseFromText(userText)
	return adaptOpenCodeFreeToolCalls(callerTools, calls, phase)
}

func adaptOpenCodeFreeToolCalls(callerTools []string, calls []ToolCall, phase string) []ToolCall {
	if len(calls) == 0 {
		return calls
	}
	out := make([]ToolCall, 0, len(calls))
	for _, call := range calls {
		if hostCalls, ok := expandOpenCodeFreeHostCalls(call, phase); ok {
			// shell/read/write/edit/glob/grep/webfetch/websearch inside MACLAW_CALL
			// are the same official calls as a direct function. Leaving the official
			// name in place is denied as an unrendered tool and closes it.
			for i := range hostCalls {
				name, args, adapted := adaptOpenCodeFreeToolCall(callerTools, hostCalls[i].Function.Name, hostCalls[i].Function.Arguments)
				if !adapted {
					continue
				}
				hostCalls[i].Function.Name = name
				hostCalls[i].Function.Arguments = args
			}
			out = append(out, hostCalls...)
			continue
		}
		name, args, ok := adaptOpenCodeFreeToolCall(callerTools, call.Function.Name, call.Function.Arguments)
		if ok {
			call.Function.Name = name
			call.Function.Arguments = args
		}
		out = append(out, call)
	}
	return out
}

func adaptOpenCodeFreeToolCall(callerTools []string, name, argsJSON string) (string, string, bool) {
	official := strings.ToLower(strings.TrimSpace(name))
	maclaw, ok := openCodeFreeToolTargets[official]
	if !ok {
		return name, argsJSON, false
	}
	target := chooseOpenCodeFreeTool(callerTools, official, maclaw)
	if target == strings.TrimSpace(name) && !openCodeFreeMaclawArgs(target) {
		return name, argsJSON, false
	}
	return target, rewriteOpenCodeFreeToolArguments(target, argsJSON), true
}

func chooseOpenCodeFreeTool(callerTools []string, official, maclaw string) string {
	if len(callerTools) == 0 {
		return maclaw
	}
	have := make(map[string]struct{}, len(callerTools))
	for _, name := range callerTools {
		name = strings.ToLower(strings.TrimSpace(name))
		if name != "" {
			have[name] = struct{}{}
		}
	}
	if _, ok := have[strings.ToLower(maclaw)]; ok {
		return maclaw
	}
	if _, ok := have[official]; ok {
		return official
	}
	return maclaw
}

func openCodeFreeMaclawArgs(target string) bool {
	switch target {
	case "bash", "read_file", "write_file", "edit_file", "Glob", "ripgrep", "web_fetch":
		return true
	default:
		return false
	}
}

func rewriteOpenCodeFreeToolArguments(target, argsJSON string) string {
	raw := strings.TrimSpace(argsJSON)
	if raw == "" || !openCodeFreeMaclawArgs(target) {
		return argsJSON
	}
	var args map[string]interface{}
	if json.Unmarshal([]byte(raw), &args) != nil || args == nil {
		return argsJSON
	}
	renameOpenCodeArg(args, "filePath", "path")
	switch target {
	case "bash":
		// Official shell uses workdir plus a millisecond timeout. Maclaw bash
		// reads working_dir and treats timeout as seconds. A raw 120000 would
		// wait for more than a day.
		renameOpenCodeArg(args, "workdir", "working_dir")
		if _, exists := args["timeout"]; !exists {
			// Official foreground shell defaults to 120000ms. Maclaw bash
			// cannot represent a shorter foreground timeout: both its schema
			// and executor floor are 240s. Materialize that effective value
			// instead of silently widening the omission to the 600s host
			// default. Official background commands are different: they have
			// no timeout unless the call explicitly sets one.
			if background, _ := args["background"].(bool); !background {
				args["timeout"] = int64(240)
			}
		} else if timeout, ok := args["timeout"].(float64); ok {
			if timeout <= 0 {
				// Official zero disables the timeout. Maclaw bash has no
				// no-timeout representation; dropping the field uses its
				// bounded default instead of failing schema validation.
				delete(args, "timeout")
			} else {
				// Maclaw bash validates timeout as an integer number of seconds
				// in [240,600]. Convert milliseconds upward, then materialize
				// the same bounded value the host executor can actually apply.
				switch {
				case timeout/1000 > 600:
					args["timeout"] = int64(600)
				case timeout/1000 <= 240:
					args["timeout"] = int64(240)
				default:
					args["timeout"] = int64(math.Ceil(timeout / 1000))
				}
			}
		}
	case "read_file":
		renameOpenCodeArg(args, "offset", "start_line")
		renameOpenCodeArg(args, "limit", "lines")
		// Official read is a numbered 1-based page. Materialize the default
		// start explicitly; host line_numbers is an internal executor mode and
		// is filtered back out when the call is replayed upstream.
		if _, exists := args["start_line"]; !exists {
			args["start_line"] = int64(1)
		}
		args["line_numbers"] = true
		// Official read defaults to and caps one page at 2000 lines. Maclaw's
		// omitted-lines behavior is an adaptive outline rather than a page, so
		// materialize the official page length explicitly.
		if lines, exists := args["lines"]; !exists {
			args["lines"] = int64(2000)
		} else if n, ok := lines.(float64); ok {
			if n <= 0 || n > 2000 {
				args["lines"] = int64(2000)
			}
		}
	case "edit_file":
		renameOpenCodeArg(args, "oldString", "old_string")
		renameOpenCodeArg(args, "newString", "new_string")
		renameOpenCodeArg(args, "replaceAll", "replace_all")
		if replaceAll, _ := args["replace_all"].(bool); !replaceAll {
			// Official edit requires oldString to identify one unique location
			// unless replaceAll is true. Maclaw historically edits the first
			// match, so carry the stricter contract through explicitly.
			args["require_unique"] = true
		}
	case "Glob":
		renameOpenCodeArg(args, "hidden", "include_hidden")
		renameOpenCodeArg(args, "limit", "max_results")
		// Official glob defaults to 100 matches; Maclaw Glob defaults to 200.
		if limit, exists := args["max_results"]; !exists {
			args["max_results"] = int64(100)
		} else if n, ok := limit.(float64); ok {
			if n <= 0 {
				args["max_results"] = int64(100)
			} else if n > 2000 {
				args["max_results"] = int64(2000)
			}
		}
	case "ripgrep":
		renameOpenCodeArg(args, "include", "glob")
		renameOpenCodeArg(args, "literal", "fixed_string")
		renameOpenCodeArg(args, "caseSensitive", "case_sensitive")
		renameOpenCodeArg(args, "limit", "max_results")
		if _, exists := args["case_sensitive"]; !exists {
			// Official grep is case-sensitive unless the call says otherwise;
			// Maclaw ripgrep is case-insensitive by default.
			args["case_sensitive"] = true
		}
	case "web_fetch":
		// Official webfetch accepts fractional seconds, defaults to 30s, and
		// caps execution at 120s. Maclaw web_fetch uses integer seconds and a
		// much wider host-native default, so the bridge must supply the
		// official default rather than inherit the host policy.
		if _, exists := args["timeout"]; !exists {
			args["timeout"] = int64(30)
		} else if timeout, ok := args["timeout"].(float64); ok {
			switch {
			case timeout <= 0:
				args["timeout"] = int64(30)
			case timeout > 120:
				args["timeout"] = int64(120)
			default:
				args["timeout"] = int64(math.Ceil(timeout))
			}
		}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return argsJSON
	}
	return string(encoded)
}

func renameOpenCodeArg(args map[string]interface{}, from, to string) {
	if _, exists := args[to]; exists {
		return
	}
	value, ok := args[from]
	if !ok {
		return
	}
	args[to] = value
	delete(args, from)
}

// OpenAIDeclaredToolNames lists function names from a chat-completions tools
// array. Built-in tools without a function name are skipped.
func OpenAIDeclaredToolNames(raw any) []string {
	var names []string
	for _, tool := range openAIChatTools(raw) {
		if fn, _ := tool["function"].(map[string]interface{}); fn != nil {
			if name := strings.TrimSpace(stringValue(fn["name"])); name != "" {
				names = append(names, name)
				continue
			}
		}
		if name := strings.TrimSpace(stringValue(tool["name"])); name != "" {
			names = append(names, name)
		}
	}
	return names
}
