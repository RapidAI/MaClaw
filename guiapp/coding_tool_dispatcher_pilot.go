package guiapp

// Phase 2 ToolDispatcher convergence pilot on the coding-workbench subagent
// host, mirroring the IM/TUI pilots: coding turns run CodingSubAgent.
// ExecuteTask → codingagent.Run → agent.RunLoop in
// corelib/codingagent/codingagent.go, so the core loop's dispatcher-first
// chain is live here.
// codingSubAgentCallbacks implements agent.ToolCallContextExecutor
// (ExecuteToolCallWithContext, coding_dynamic_surface.go:412) — the
// highest-priority legacy executor — and a published dispatcher takes
// precedence over it while handlers route through it verbatim.
//
// Pilot discipline:
//   - The kill switch MACLAW_TOOL_DISPATCHER must be on/1/true/yes for the
//     provider to publish anything; unset or any other value keeps the
//     provider nil and the legacy chain fully authoritative (default OFF).
//   - Every handler enters through ExecuteToolCallWithContext — the exact
//     boundary RunLoop calls — including the legacy-dynamic-gateway fence,
//     the static-compatibility rendered-name fence, and the surface-epoch /
//     response-correlation fences, with the loop's ToolCallExecutionContext
//     forwarded verbatim. The only intentional difference is precedence.
//   - Registration is ID-keyed, with canonical IDs and the domain assertion
//     both sourced from the IM handler's ToolRegistry (registry-backed
//     resolver) — the same registry family the coding workbench tools are
//     registered in. Only the allowlisted pilot names get handlers; do NOT
//     add name-keyed registrations here.
//   - Names the registry does not serve (or that carry no tool ID) are
//     skipped with a log, never a crash.

import (
	"log"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// codingPilotEnvKey is the pilot kill switch, shared with the other host
// pilots: MACLAW_TOOL_DISPATCHER=on publishes the dispatcher; unset or off
// keeps it nil.
const codingPilotEnvKey = "MACLAW_TOOL_DISPATCHER"

func codingDispatcherPilotEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(codingPilotEnvKey))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// codingPilotToolNames is the pilot's handler-ownership allowlist:
// read-only/lookup names the coding worker surface serves (cases of
// executeToolWithOutcome). Each MUST also resolve in the host ToolRegistry
// (the domain authority and canonical-ID source). Execute-kind (bash),
// mutate-kind (write_file/edit_file), spawn, and dynamic Skill/MCP aliases
// stay excluded.
var codingPilotToolNames = []string{
	"web_search",
	"web_fetch",
	"read_file",
	"list_directory",
	"Glob",
	"ripgrep",
	"knowledge_search",
	"current_datetime",
}

// buildCodingPilotDispatcher registers the pilot handlers under the canonical
// tool IDs carried by the host ToolRegistry (registry-backed resolver):
// Get(name) both asserts the name is in the domain (skipped with a log when
// absent) and yields the registry-assigned ID. Only pilot names get handlers,
// so everything else still falls through to the legacy chain. A nil registry
// registers nothing. It returns the dispatcher and the skipped names.
func buildCodingPilotDispatcher(c *codingSubAgentCallbacks, reg *ToolRegistry, names []string) (*agent.NameDispatcher, []string) {
	d := agent.NewNameDispatcher()
	resolvable := make(map[string]string, len(names))
	var skipped []string
	for _, name := range names {
		if reg == nil {
			skipped = append(skipped, name)
			continue
		}
		entry, ok := reg.Get(name)
		if !ok || entry.ID.IsZero() {
			log.Printf("[coding-dispatcher-pilot] %q is not registered (or has no tool ID) in the host ToolRegistry; skipping registration", name)
			skipped = append(skipped, name)
			continue
		}
		id := entry.ID.String()
		if err := d.RegisterID(entry.ID, func(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) (agent.ToolExecutionResult, error) {
			return c.ExecuteToolCallWithContext(name, argsJSON, callID, execution), nil
		}); err != nil {
			log.Printf("[coding-dispatcher-pilot] register %q failed: %v", name, err)
			skipped = append(skipped, name)
			continue
		}
		resolvable[name] = id
	}
	d.SetIDResolver(func(name string) (string, bool) {
		id, ok := resolvable[strings.TrimSpace(name)]
		return id, ok
	})
	return d, skipped
}

// ToolDispatcher implements agent.ToolDispatcherProvider (Phase 2 pilot).
// Default OFF: unless MACLAW_TOOL_DISPATCHER=on, it returns nil and the core
// loop keeps using the legacy ToolCallContextExecutor chain unchanged. Built
// lazily per callback; without a host registry there is no domain or
// canonical ID to resolve against, so nothing is published.
func (c *codingSubAgentCallbacks) ToolDispatcher() agent.ToolDispatcher {
	if c == nil || !codingDispatcherPilotEnabled() {
		return nil
	}
	c.dispatcherOnce.Do(func() {
		var reg *ToolRegistry
		if c.subagent != nil && c.subagent.handler != nil {
			reg = c.subagent.handler.registry
		}
		if reg == nil {
			return
		}
		d, skipped := buildCodingPilotDispatcher(c, reg, codingPilotToolNames)
		if len(skipped) > 0 {
			log.Printf("[coding-dispatcher-pilot] skipped names=%v", skipped)
		}
		log.Printf("[coding-dispatcher-pilot] registered handlers=%v", d.Handlers())
		c.dispatcher = d
	})
	return c.dispatcher
}

var _ agent.ToolDispatcherProvider = (*codingSubAgentCallbacks)(nil)
