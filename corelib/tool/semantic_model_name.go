package tool

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

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
	if name, ok := reviewedDynamicPromptName(adapter); ok {
		return name
	}
	if semanticStableHostAdapter(adapter) {
		return adapter
	}
	return ""
}

// reviewedDynamicPromptPrefix marks an internal dynamic adapter whose
// reviewed binding implements a capability that already has a stable prompt
// spelling. The suffix is the ordinary dynamic adapter id. Vendor tool names
// never appear in this prefix.
const reviewedDynamicPromptPrefix = "reviewed-prompt:"

// AllowReviewedDynamicPrompt is the sealed set of prompt spellings a
// code-reviewed dynamic implementation may render as. It is not a vendor
// tool allowlist.
//
// The set is registry-driven rather than a literal switch so a second reviewed
// binding does not have to come back here to edit a case arm — but it stays
// sealed in the only way that matters: registration is accepted only for a
// spelling the host surface already exposes as its own stable name. A vendor
// name can never be registered, so a reviewed binding can alias itself onto
// an existing prompt contract and nothing else.
//
// The mutex is not optional. AllowReviewedDynamicPrompt is on the planner's
// read path and planning runs concurrently across turns and sessions, while
// registration mutates the same map. An unsynchronized map with a concurrent
// writer does not degrade gracefully — the runtime throws a fatal, unrecoverable
// error rather than returning a wrong answer.
var (
	reviewedDynamicPromptMu    sync.RWMutex
	reviewedDynamicPromptNames = map[string]struct{}{"web_search": {}}
)

// RegisterReviewedDynamicPrompt admits a prompt spelling that a code-reviewed
// dynamic implementation may render as. The name must already be a stable host
// spelling; anything else is rejected rather than registered, so the set cannot
// grow a new model-visible name by this route.
func RegisterReviewedDynamicPrompt(name string) error {
	spelling := strings.TrimSpace(name)
	if spelling == "" {
		return fmt.Errorf("reviewed dynamic prompt name is empty")
	}
	if !semanticStableHostAdapter(spelling) {
		return fmt.Errorf("reviewed dynamic prompt %q is not a stable host spelling", spelling)
	}
	reviewedDynamicPromptMu.Lock()
	defer reviewedDynamicPromptMu.Unlock()
	reviewedDynamicPromptNames[spelling] = struct{}{}
	return nil
}

// ReviewedDynamicPromptNames lists the current sealed set so a host can assert
// its own surface rather than trust the registry to stay as small as its
// first author left it.
func ReviewedDynamicPromptNames() []string {
	reviewedDynamicPromptMu.RLock()
	defer reviewedDynamicPromptMu.RUnlock()
	out := make([]string, 0, len(reviewedDynamicPromptNames))
	for name := range reviewedDynamicPromptNames {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// AllowReviewedDynamicPrompt reports whether name is currently admissible as a
// reviewed dynamic prompt spelling.
func AllowReviewedDynamicPrompt(name string) bool {
	reviewedDynamicPromptMu.RLock()
	defer reviewedDynamicPromptMu.RUnlock()
	_, ok := reviewedDynamicPromptNames[strings.TrimSpace(name)]
	return ok
}

func reviewedDynamicPromptName(adapter string) (string, bool) {
	rest, ok := strings.CutPrefix(adapter, reviewedDynamicPromptPrefix)
	if !ok {
		return "", false
	}
	name, suffix, ok := strings.Cut(rest, ":")
	if !ok || !strings.HasPrefix(suffix, "dynamic_") || !AllowReviewedDynamicPrompt(name) {
		return "", false
	}
	return name, true
}

// installedMCPAdapterPrefix marks an installed MCP adapter whose model-visible
// name is the stable server__tool spelling. The suffix stays the internal
// dynamic adapter id. The grant token is still the one-shot consume identity;
// it is not the name the model calls.
const installedMCPAdapterPrefix = "installed-mcp:"

// InstalledMCPModelFunctionName is the stable function an installed MCP tool
// keeps across turns and across repeat calls. Rotating invoke_* tokens made
// the model call a name that had already left the list.
func InstalledMCPModelFunctionName(serverID, toolName string) string {
	server := sanitizeInstalledMCPNamePart(serverID)
	toolName = sanitizeInstalledMCPNamePart(toolName)
	if server == "" || toolName == "" {
		return ""
	}
	name := server + "__" + toolName
	if validRenderedFunctionName(name) {
		return name
	}
	sum := SchemaDigest([]byte(server + "\x00" + toolName))
	if len(sum) > 8 {
		sum = sum[:8]
	}
	keep := 64 - len(sum) - 1
	if keep < 1 {
		keep = 1
	}
	if len(name) > keep {
		name = name[:keep]
	}
	name = strings.Trim(name, "_-")
	if name == "" {
		name = "mcp"
	}
	candidate := name + "_" + sum
	if !validRenderedFunctionName(candidate) {
		return ""
	}
	return candidate
}

func sanitizeInstalledMCPNamePart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_-")
}

func installedMCPPromptName(adapter string) (string, bool) {
	rest, ok := strings.CutPrefix(strings.TrimSpace(adapter), installedMCPAdapterPrefix)
	if !ok {
		return "", false
	}
	name, suffix, ok := strings.Cut(rest, ":")
	if !ok || !strings.HasPrefix(suffix, "dynamic_mcp_") || !validRenderedFunctionName(name) {
		return "", false
	}
	return name, true
}

// RenderedSemanticFunctionName is the model-visible function for a selection.
// Known host adapters use a stable prompt name. An installed MCP adapter uses
// server__tool, which does not change when the one-shot grant is replaced.
// A reviewed dynamic binding uses its sealed prompt spelling. Every other
// dynamic adapter keeps the rotating grant token.
func RenderedSemanticFunctionName(adapter, grantToken string) string {
	if name := SemanticModelFunctionName(adapter); name != "" {
		return name
	}
	if name, ok := installedMCPPromptName(adapter); ok {
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
	for _, name := range semanticModelFunctionNames {
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
	"current_datetime": {}, "git_status": {}, "ssh": {}, "browser": {}, "mailbox_query": {},
	"computer_use": {}, "delegate_task": {}, "knowledge_search": {},
	"knowledge_save_text": {}, "knowledge_maintain": {}, "session_search": {},
	"mis_data": {}, "git_commit": {}, "build_verify": {}, "memory_recall": {},
}

var semanticStableHostNames map[string]struct{}

// semanticModelFunctionNames maps internal host adapters onto the names the
// prompt and conversation history already use. Adapters whose registered
// name is already that stable spelling (generate_pdf, screenshot, write_file)
// are omitted and returned unchanged.
//
// GUI (semantic_*) and MaClawSrv (host_*) are separate catalogs for the same
// outcome. They share a prompt name so the model calls web_search on both.
// They are never planned together; the renderer still rejects two selections
// in one plan that would render as the same function.
var semanticModelFunctionNames = map[string]string{
	"semantic_search_trusted_web":           "web_search",
	"host_information_search_web":           "web_search",
	"semantic_fetch_trusted_web":            "web_fetch",
	"host_information_fetch_web":            "web_fetch",
	"semantic_read_trusted_clock":           "current_datetime",
	"host_information_current_time":         "current_datetime",
	"semantic_read_trusted_file":            "read_file",
	"host_fs_read_local":                    "read_file",
	"semantic_write_trusted_file":           "write_file",
	"host_fs_write_local":                   "write_file",
	"semantic_delete_trusted_file":          "delete_file",
	"semantic_inspect_trusted_repo":         "git_status",
	"host_repo_inspect_vcs":                 "git_status",
	"semantic_mutate_trusted_repo":          "git_commit",
	"host_repo_mutate_vcs":                  "git_commit",
	"semantic_execute_trusted_shell":        "bash",
	"host_shell_execute_local":              "bash",
	"semantic_execute_trusted_ssh":          "ssh",
	"host_shell_execute_remote_host":        "ssh",
	"semantic_control_trusted_browser":      "browser",
	"host_browser_control_web":              "browser",
	"semantic_control_trusted_desktop":      "computer_use",
	"host_computer_control_desktop":         "computer_use",
	"semantic_run_trusted_build_verify":     "build_verify",
	"host_build_verify_local":               "build_verify",
	"semantic_acquire_trusted_remote":       "download_file",
	"host_artifact_acquire_remote":          "download_file",
	"semantic_delegate_trusted_subtask":     "delegate_task",
	"host_agent_delegate_subtask":           "delegate_task",
	"semantic_ingest_trusted_knowledge":     "knowledge_save_text",
	"host_knowledge_ingest_local":           "knowledge_save_text",
	"semantic_read_trusted_knowledge":       "knowledge_search",
	"host_knowledge_read_local":             "knowledge_search",
	"semantic_administer_trusted_knowledge": "knowledge_maintain",
	"host_knowledge_admin_maintenance":      "knowledge_maintain",
	"semantic_administer_trusted_memory":    "memory",
	"host_memory_manage_agent":              "memory",
	"semantic_recall_trusted_memory":        "memory_recall",
	"host_memory_recall_agent":              "memory_recall",
	"semantic_administer_trusted_task":      "task",
	"host_task_track_local":                 "task",
	"semantic_administer_trusted_goal":      "goal",
	"host_goal_manage_longrunning":          "goal",
	"semantic_administer_trusted_template":  "manage_template",
	"host_template_manage_session":          "manage_template",
	"semantic_inspect_trusted_session":      "list_sessions",
	"host_session_manage_coding":            "list_sessions",
	"semantic_administer_trusted_schedule":  "manage_schedule",
	"host_schedule_administer_local":        "manage_schedule",
	"semantic_administer_trusted_config":    "manage_config",
	"host_config_manage_self":               "manage_config",
	"semantic_send_trusted_im":              "send_im_text",
	"host_message_send_im":                  "send_im_text",
	"semantic_send_trusted_mailbox":         "mailbox_send",
	"semantic_configure_trusted_mailbox":    "mailbox_configure",
	"semantic_delete_trusted_mailbox":       "mailbox_delete",
	"semantic_transcribe_trusted_audio":     "asr",
	"host_audio_transcribe_speech":          "asr",
	"semantic_read_trusted_audit":           "session_search",
	"host_security_audit_read":              "session_search",
	"semantic_write_trusted_office":         "office",
	"host_document_write_office":            "office",
	"semantic_deliver_current_file":         "send_file",
	"semantic_deliver_current_image":        "send_image",
	"semantic_deliver_current_voice":        "send_voice",
	"semantic_deliver_specified_target":     "send_to_im",
	"host_artifact_deliver_specified":       "send_to_im",
	// These GUI tools already use the prompt spelling as their adapter name,
	// so only the srv host adapter needs an entry.
	"host_document_generate_file": "generate_pdf",
	"host_visual_capture_desktop": "screenshot",
}
