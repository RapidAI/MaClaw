package agentservice

// Phase 2 ToolDispatcher convergence pilot on the agentservice host
// (docs/design/tool-routing-improvement-plan-zh.md Phase 2): a NameDispatcher
// registers read-only/lookup and mutate-class first-party names, and
// coreAgentCallbacks publishes it via agent.ToolDispatcherProvider. The
// RunLoop consults a dispatcher BEFORE the legacy executor chain, so once
// published these names are owned outright by the dispatcher.
//
// Pilot discipline:
//   - The kill switch MACLAW_TOOL_DISPATCHER must be on/1/true/yes for the
//     provider to publish anything; unset or any other value keeps the
//     provider nil and the legacy chain fully authoritative (default OFF, so
//     production behavior is untouched in this slice).
//   - Every handler enters through executeToolCallLegacy — the exact body of
//     ExecuteToolCall — so a dispatched call runs byte-identical logic to the
//     legacy path, for read-only AND mutate-class names:
//     CanonicalizeToolCallJSON, the runtime-tool invoker branch, the managed
//     semantic surface (callID-bound replay, governed marking, replan
//     recovery), and the ExecuteToolStructured preamble guards
//     (hardwareExpert/policy/mutation-scope/clientsecurity/adaptiveRetry)
//     all stay on the path. Loop-level gates (authorizeLoopTool admission,
//     replan skip) run upstream of the dispatcher consultation for both
//     paths. The only intentional difference is precedence: a handled name is
//     owned by the dispatcher and never reaches the legacy chain below it.
//   - Registration is ID-keyed: handlers register under their canonical
//     toolid.ToolID (core:<exact name>, matching CoreToolRegistry's ID
//     derivation) and a rendered-name → ID resolver maps dispatch input to
//     the ID. Do NOT add name-keyed registrations to this pilot — they would
//     only fire when the resolver misses, which is exactly the ambiguous
//     double-path the convergence removes.
//   - Registration asserts every pilot name exists in the legacy switch's
//     name domain; unknown names are skipped with a log, never a crash.

import (
	"log"
	"os"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/toolid"
)

// toolDispatcherEnvKey is the pilot kill switch: MACLAW_TOOL_DISPATCHER=on
// (or 1/true/yes) publishes the dispatcher; unset or off keeps it nil.
const toolDispatcherEnvKey = "MACLAW_TOOL_DISPATCHER"

func toolDispatcherPilotEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(toolDispatcherEnvKey))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// coreAgentStructuredSwitchNames is the name domain of the legacy execution
// path: the cases of executeToolStructuredUnguarded's main switch plus the
// cases of executeSharedHostTool's switch (the default branch of the former).
// The pilot registration asserts every pilot name against this set so a
// renamed or removed case is caught at registration time instead of silently
// falling through to legacy. Names are exact-case — the surface renders and
// the switches match "Glob"/"ripgrep"/"FileRead", never lowercase aliases.
var coreAgentStructuredSwitchNames = map[string]bool{
	"record_audio":                     true,
	"bash":                             true,
	"ssh":                              true,
	"ask_user":                         true,
	"task":                             true,
	"manage_schedule":                  true,
	"im_message":                       true,
	"send_file":                        true,
	"send_to_im":                       true,
	"memory":                           true,
	"read_file":                        true,
	"read_document":                    true,
	"read_tool_result":                 true,
	"write_file":                       true,
	"edit_file":                        true,
	"list_directory":                   true,
	"manage_skill":                     true,
	"web_search":                       true,
	"web_fetch":                        true,
	"knowledge_search":                 true,
	"knowledge_image_search":           true,
	"knowledge_context_pack":           true,
	"knowledge_import_share":           true,
	"knowledge_import_hub_share":       true,
	"knowledge_list_sources":           true,
	"knowledge_source_detail":          true,
	"knowledge_stats":                  true,
	"knowledge_list_source_labels":     true,
	"knowledge_update_source_labels":   true,
	"knowledge_update_source_metadata": true,
	"knowledge_enable_source":          true,
	"knowledge_disable_source":         true,
	"knowledge_delete_source":          true,
	"knowledge_refresh_source":         true,
	"knowledge_list_import_batches":    true,
	"knowledge_list_import_items":      true,
	"knowledge_retry_import_batch":     true,
	"knowledge_delete_import_batch":    true,
	"knowledge_save_urls":              true,
	"knowledge_import_package":         true,
	"knowledge_export":                 true,
	"knowledge_import_directory":       true,
	"knowledge_import_files":           true,
	"knowledge_save_url":               true,
	"knowledge_save_text":              true,
	// executeSharedHostTool switch (shared_tool_surface.go).
	"Glob":     true,
	"ripgrep":  true,
	"FileRead": true,
}

// coreAgentPilotNames is the pilot registration set: read-only / lookup-style
// and mutate-class first-party names. Batch 1 was the five knowledge lookups;
// batch 2 added web/file/document lookups from the main switch plus the
// shared-host search tools (Glob/ripgrep/FileRead, exact case) from
// executeSharedHostTool's switch; batch 3 adds the mutate-class switch names
// write_file/edit_file/memory. All reach the legacy entry through
// executeToolCallLegacy.
var coreAgentPilotNames = []string{
	"knowledge_search",
	"knowledge_list_sources",
	"knowledge_stats",
	"knowledge_list_source_labels",
	"knowledge_source_detail",
	"knowledge_context_pack",
	"knowledge_image_search",
	"web_search",
	"web_fetch",
	"ask_user",
	"read_file",
	"read_tool_result",
	"read_document",
	"list_directory",
	"Glob",
	"ripgrep",
	"FileRead",
	"write_file",
	"edit_file",
	"memory",
}

// coreAgentPilotHandlers returns the pilot handler table keyed by pilot name.
// Every handler is the same one-liner: enter through executeToolCallLegacy —
// the exact body of ExecuteToolCall — so a dispatched call is byte-identical
// to the legacy path by construction, with the model/provider callID forwarded
// for idempotent replay.
func coreAgentPilotHandlers(c *coreAgentCallbacks) map[string]agent.ToolHandlerFunc {
	handlers := make(map[string]agent.ToolHandlerFunc, len(coreAgentPilotNames))
	for _, name := range coreAgentPilotNames {
		handlers[name] = func(name, argsJSON, callID string, _ agent.ToolCallExecutionContext) (agent.ToolExecutionResult, error) {
			return c.executeToolCallLegacy(name, argsJSON, callID), nil
		}
	}
	return handlers
}

// coreAgentPilotToolID returns the canonical tool ID for a pilot name. The
// agentservice host has no CoreToolRegistry (coreToolSpecs is hand-assembled),
// so the ID is derived with the registry's own rule — "core:" + exact rendered
// name — which matches CoreToolRegistry's zero-value-ID derivation
// (tool_register_core.go). Exact case is preserved ("core:Glob", not
// "core:glob").
func coreAgentPilotToolID(name string) toolid.ToolID {
	return toolid.MustParse("core:" + name)
}

// buildCoreAgentPilotDispatcher registers the pilot handlers under their
// canonical tool IDs and installs the rendered-name → ID resolver. The
// resolver is built from the pilot's own name list (option b): only pilot
// names resolve; everything else falls through to the legacy chain — correct
// and sufficient here because no name-keyed handlers exist. Any name outside
// the legacy switch domain or any duplicate/invalid registration is skipped
// with a log, never a crash. It returns the dispatcher and the skipped names
// so tests can assert the filtering.
func buildCoreAgentPilotDispatcher(c *coreAgentCallbacks, handlers map[string]agent.ToolHandlerFunc) (*agent.NameDispatcher, []string) {
	d := agent.NewNameDispatcher()
	names := make([]string, 0, len(handlers))
	for name := range handlers {
		names = append(names, name)
	}
	sort.Strings(names)
	resolvable := make(map[string]string, len(names))
	var skipped []string
	for _, name := range names {
		if !coreAgentStructuredSwitchNames[name] {
			log.Printf("[agentservice] tool dispatcher pilot: %q is not in the legacy switch domain; skipping registration", name)
			skipped = append(skipped, name)
			continue
		}
		if err := d.RegisterID(coreAgentPilotToolID(name), handlers[name]); err != nil {
			log.Printf("[agentservice] tool dispatcher pilot: register %q failed: %v", name, err)
			skipped = append(skipped, name)
			continue
		}
		resolvable[name] = coreAgentPilotToolID(name).String()
	}
	d.SetIDResolver(func(name string) (string, bool) {
		id, ok := resolvable[strings.TrimSpace(name)]
		return id, ok
	})
	return d, skipped
}

// ToolDispatcher implements agent.ToolDispatcherProvider (Phase 2 pilot).
// Default OFF: unless MACLAW_TOOL_DISPATCHER=on, it returns nil and the core
// loop keeps using the legacy executor chain unchanged.
func (c *coreAgentCallbacks) ToolDispatcher() agent.ToolDispatcher {
	if c == nil || !toolDispatcherPilotEnabled() {
		return nil
	}
	c.dispatcherOnce.Do(func() {
		d, skipped := buildCoreAgentPilotDispatcher(c, coreAgentPilotHandlers(c))
		if len(skipped) > 0 {
			log.Printf("[agentservice] tool dispatcher pilot: skipped names=%v", skipped)
		}
		log.Printf("[agentservice] tool dispatcher pilot: registered handlers=%v", d.Handlers())
		c.dispatcher = d
	})
	return c.dispatcher
}

var _ agent.ToolDispatcherProvider = (*coreAgentCallbacks)(nil)
