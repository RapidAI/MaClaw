package guiapp

// Phase 2 ToolDispatcher convergence pilot on the guiapp IM host, mirroring
// the agentservice pilot (corelib/agentservice/tool_dispatcher_pilot.go):
// a NameDispatcher registers a set of read-only first-party names by their
// canonical tool IDs, and sharedAgentLoopCallbacks publishes it via
// agent.ToolDispatcherProvider. The shared RunLoop consults a dispatcher
// BEFORE the legacy ToolCallContextExecutor chain, so once published these
// names are owned outright by the dispatcher.
//
// Pilot discipline:
//   - The kill switch MACLAW_TOOL_DISPATCHER must be on/1/true/yes for the
//     provider to publish anything; unset or any other value keeps the
//     provider nil and the legacy chain fully authoritative (default OFF).
//   - Every handler enters through ExecuteToolCallWithContext — the exact
//     boundary the core loop calls via ToolCallContextExecutor — including
//     the surface-epoch gate and the loop-supplied ToolCallExecutionContext,
//     so a dispatched call is byte-identical to the legacy path by
//     construction, for read-only AND mutate-class names. Write-time gates
//     (credential fence, ACP permission, argument-size limit, group-policy)
//     all sit downstream of that entry, and loop-level admission
//     (authorizeLoopTool, replan skip) runs upstream of the dispatcher
//     consultation for both paths. The only intentional difference is
//     precedence: a handled name is owned by the dispatcher and never reaches
//     the legacy chain.
//   - Registration is ID-keyed, with the canonical IDs and the domain
//     assertion both sourced from the IM handler's ToolRegistry
//     (registry-backed resolver): only the allowlisted pilot names get
//     handlers, but resolution and identity come from the registry, including
//     explicitly assigned non-core IDs. Do NOT add name-keyed registrations
//     here.
//   - Names the registry does not serve (or that carry no tool ID) are
//     skipped with a log, never a crash.

import (
	"log"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// imDispatcherEnvKey is the pilot kill switch, shared with the agentservice
// pilot: MACLAW_TOOL_DISPATCHER=on publishes the dispatcher; unset or off
// keeps it nil.
const imDispatcherEnvKey = "MACLAW_TOOL_DISPATCHER"

func imDispatcherPilotEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(imDispatcherEnvKey))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// imPilotToolNames is the pilot's handler-ownership allowlist: read-only /
// lookup AND mutate-class first-party names the IM host serves, each of which
// MUST also be registered in the IM handler's ToolRegistry — the registry is
// the domain authority and the source of canonical tool IDs (see
// buildIMPilotDispatcher). Exact case: the registry renders and matches
// "Glob"/"ripgrep"/"FileRead", never lowercase aliases. Execute-kind
// (bash/ssh), task/delegate, and MCP/skill adapter names stay excluded.
//
// Mutate-class inclusion (write_file/edit_file/memory) is safe by the same
// construction as the read-only names: every handler enters through
// ExecuteToolCallWithContext, and loop-level gates (authorizeLoopTool
// admission, replan skip) run upstream of the dispatcher consultation for
// both paths — the dispatcher only replaces the executor-chain segment.
var imPilotToolNames = []string{
	"web_search",
	"web_fetch",
	"read_file",
	"FileRead",
	"ripgrep",
	"Glob",
	"ask_user",
	"current_datetime",
	"write_file",
	"edit_file",
	"memory",
}

// buildIMPilotDispatcher registers the pilot handlers under the canonical
// tool IDs carried by the IM handler's ToolRegistry (registry-backed
// resolver): Get(name) both asserts the name is in the live IM domain
// (skipped with a log when absent) and yields the registry-assigned ID —
// including explicitly assigned non-core IDs. Resolving a non-pilot name is
// harmless: only pilot names get handlers, so everything else still falls
// through to the legacy chain. A nil registry registers nothing (the
// dispatcher would be inert). It returns the dispatcher and the skipped
// names so tests can assert the filtering.
func buildIMPilotDispatcher(c *sharedAgentLoopCallbacks, reg *ToolRegistry, names []string) (*agent.NameDispatcher, []string) {
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
			log.Printf("[im-dispatcher-pilot] %q is not registered (or has no tool ID) in the IM ToolRegistry; skipping registration", name)
			skipped = append(skipped, name)
			continue
		}
		id := entry.ID.String()
		if err := d.RegisterID(entry.ID, func(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) (agent.ToolExecutionResult, error) {
			return c.ExecuteToolCallWithContext(name, argsJSON, callID, execution), nil
		}); err != nil {
			log.Printf("[im-dispatcher-pilot] register %q failed: %v", name, err)
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
// loop keeps using the legacy ToolCallContextExecutor chain unchanged. The
// dispatcher is registry-backed: without the IM handler's ToolRegistry there
// is no domain or canonical ID to resolve against, so nothing is published.
func (c *sharedAgentLoopCallbacks) ToolDispatcher() agent.ToolDispatcher {
	if c == nil || !imDispatcherPilotEnabled() {
		return nil
	}
	c.dispatcherOnce.Do(func() {
		var reg *ToolRegistry
		if c.handler != nil {
			reg = c.handler.registry
		}
		if reg == nil {
			return
		}
		d, skipped := buildIMPilotDispatcher(c, reg, imPilotToolNames)
		if len(skipped) > 0 {
			log.Printf("[im-dispatcher-pilot] skipped names=%v", skipped)
		}
		log.Printf("[im-dispatcher-pilot] registered handlers=%v", d.Handlers())
		c.dispatcher = d
	})
	return c.dispatcher
}

var _ agent.ToolDispatcherProvider = (*sharedAgentLoopCallbacks)(nil)
