package tool

import "strings"

// SemanticModelFunctionName is the host-owned name the model sees for a
// catalog adapter. It is stable across turns and matches the ordinary tool
// names the rest of the prompt already teaches.
//
// Grant.Token stays the one-shot consume/replay identity and the durable
// route-state key. It is never the LLM-visible function name: rotating
// invoke_* spellings made prompt, history, and the live tool list disagree.
func SemanticModelFunctionName(adapter string) string {
	adapter = strings.TrimSpace(adapter)
	if adapter == "" {
		return ""
	}
	if name, ok := semanticModelFunctionNames[adapter]; ok {
		return name
	}
	if semanticStableHostAdapter(adapter) {
		return adapter
	}
	return ""
}

// RenderedSemanticFunctionName is the model-visible function for a selection.
// Known host adapters use a stable prompt name. Dynamic MCP/Skill adapters
// keep the rotating grant token so provider identity does not leak.
func RenderedSemanticFunctionName(adapter, grantToken string) string {
	if name := SemanticModelFunctionName(adapter); name != "" {
		return name
	}
	return strings.TrimSpace(grantToken)
}

func semanticStableHostAdapter(adapter string) bool {
	_, ok := semanticStableHostNames[adapter]
	return ok
}

func init() {
	semanticStableHostNames = make(map[string]struct{}, len(semanticIdentityHostAdapters)+len(semanticModelFunctionNames))
	for name := range semanticIdentityHostAdapters {
		semanticStableHostNames[name] = struct{}{}
	}
	seen := make(map[string]string, len(semanticModelFunctionNames))
	for adapter, name := range semanticModelFunctionNames {
		if prev, ok := seen[name]; ok {
			panic("semantic model function name " + name + " mapped from both " + prev + " and " + adapter)
		}
		seen[name] = adapter
		semanticStableHostNames[name] = struct{}{}
	}
}

// semanticIdentityHostAdapters are registered tools whose own name is already
// the prompt spelling. Mapped values from semanticModelFunctionNames are
// merged in init() so the allowlist cannot drift from the remap table.
var semanticIdentityHostAdapters = map[string]struct{}{
	"generate_pdf": {}, "screenshot": {}, "write_file": {}, "read_file": {},
	"edit_file": {}, "list_directory": {}, "search_files": {}, "bash": {},
	"office": {}, "memory": {}, "task": {}, "goal": {}, "manage_template": {},
	"list_sessions": {}, "manage_schedule": {}, "manage_config": {},
	"send_file": {}, "send_to_im": {}, "asr": {}, "record_audio": {},
	"open": {}, "download_file": {}, "web_search": {}, "web_fetch": {},
	"current_datetime": {}, "git_status": {}, "ssh": {}, "browser": {},
	"computer_use": {}, "delegate_task": {}, "knowledge_search": {},
	"knowledge_save_text": {}, "knowledge_maintain": {}, "session_search": {},
	"mis_data": {}, "git_commit": {}, "build_verify": {}, "memory_recall": {},
}

var semanticStableHostNames map[string]struct{}

// semanticModelFunctionNames maps internal host adapters onto the names the
// prompt and conversation history already use. Adapters whose registered
// name is already that stable spelling (generate_pdf, screenshot, write_file)
// are omitted and returned unchanged. Each prompt name is used at most once:
// two ready adapters that share a name cannot both be rendered.
var semanticModelFunctionNames = map[string]string{
	"semantic_search_trusted_web":           "web_search",
	"semantic_fetch_trusted_web":            "web_fetch",
	"semantic_read_trusted_clock":           "current_datetime",
	"semantic_read_trusted_file":            "read_file",
	"semantic_write_trusted_file":           "write_file",
	"semantic_inspect_trusted_repo":         "git_status",
	"semantic_mutate_trusted_repo":          "git_commit",
	"semantic_execute_trusted_shell":        "bash",
	"semantic_execute_trusted_ssh":          "ssh",
	"semantic_control_trusted_browser":      "browser",
	"semantic_control_trusted_desktop":      "computer_use",
	"semantic_run_trusted_build_verify":     "build_verify",
	"semantic_acquire_trusted_remote":       "download_file",
	"semantic_delegate_trusted_subtask":     "delegate_task",
	"semantic_ingest_trusted_knowledge":     "knowledge_save_text",
	"semantic_read_trusted_knowledge":       "knowledge_search",
	"semantic_administer_trusted_knowledge": "knowledge_maintain",
	"semantic_administer_trusted_memory":    "memory",
	"semantic_recall_trusted_memory":        "memory_recall",
	"semantic_administer_trusted_task":      "task",
	"semantic_administer_trusted_goal":      "goal",
	"semantic_administer_trusted_template":  "manage_template",
	"semantic_inspect_trusted_session":      "list_sessions",
	"semantic_administer_trusted_schedule":  "manage_schedule",
	"semantic_administer_trusted_config":    "manage_config",
	"semantic_send_trusted_im":              "send_im_text",
	"semantic_transcribe_trusted_audio":     "asr",
	"semantic_read_trusted_audit":           "session_search",
	"semantic_write_trusted_office":         "office",
	"semantic_deliver_current_file":         "send_file",
	"semantic_deliver_current_image":        "send_image",
	"semantic_deliver_current_voice":        "send_voice",
	"semantic_deliver_specified_target":     "send_to_im",
}
