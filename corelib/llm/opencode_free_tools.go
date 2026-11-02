package llm

import (
	"encoding/json"
	"strings"
)

// openCodeFreeToolTargets is the set proven by the live weather-PDF run.
// execute and the other official names stay unchanged so the model switches.
var openCodeFreeToolTargets = map[string]string{
	"shell":     "bash",
	"read":      "read_file",
	"write":     "write_file",
	"webfetch":  "web_fetch",
	"websearch": "web_search",
}

// PresentOpenCodeFreeMessage rewrites one free-model assistant message for
// the client that sent the request. HubCenter and CodexProxy both return
// this shape. A Maclaw caller receives bash/read_file/write_file/web_fetch/
// web_search. A caller that already declared the official name, such as
// Codex shell, keeps that name. Thought text that the model also copied
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

// AdaptOpenCodeFreeToolCalls maps official OpenCode tool calls onto the
// caller's executors. An empty caller list uses the Maclaw names, which is
// what the GUI runs when the planner never rendered those tools.
func AdaptOpenCodeFreeToolCalls(callerTools []string, calls []ToolCall) []ToolCall {
	if len(calls) == 0 {
		return calls
	}
	out := make([]ToolCall, len(calls))
	copy(out, calls)
	for i := range out {
		name, args, ok := adaptOpenCodeFreeToolCall(callerTools, out[i].Function.Name, out[i].Function.Arguments)
		if !ok {
			continue
		}
		out[i].Function.Name = name
		out[i].Function.Arguments = args
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
	if _, ok := have[maclaw]; ok {
		return maclaw
	}
	if _, ok := have[official]; ok {
		return official
	}
	return maclaw
}

func openCodeFreeMaclawArgs(target string) bool {
	switch target {
	case "bash", "read_file", "write_file":
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
		if timeout, ok := args["timeout"].(float64); ok && timeout > 0 {
			seconds := timeout / 1000
			if seconds < 1 {
				delete(args, "timeout")
			} else {
				args["timeout"] = seconds
			}
		}
	case "read_file":
		renameOpenCodeArg(args, "offset", "start_line")
		renameOpenCodeArg(args, "limit", "lines")
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
