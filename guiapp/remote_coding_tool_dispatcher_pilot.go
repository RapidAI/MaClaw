package guiapp

// Phase 2 ToolDispatcher convergence pilot on the remote coding-workbench
// subagent host, mirroring the local coding pilot: remote turns run
// RemoteCodingSubAgent's task path → codingagent.Run → agent.RunLoop
// (remote_coding_subagent.go:617), so the core loop's dispatcher-first chain
// is live here. remoteCodingCallbacks implements
// agent.ToolCallContextExecutor (ExecuteToolCallWithContext,
// remote_coding_subagent.go:2493) — the highest-priority legacy executor —
// behind its own remote static-compatibility fences (rendered-name,
// epoch, response correlation) and the dynamic-gateway/alias rejections.
//
// Pilot discipline:
//   - The kill switch MACLAW_TOOL_DISPATCHER must be on/1/true/yes for the
//     provider to publish anything; unset or any other value keeps the
//     provider nil and the legacy chain fully authoritative (default OFF).
//   - Every handler enters through ExecuteToolCallWithContext — the exact
//     boundary RunLoop calls — with the loop's ToolCallExecutionContext
//     forwarded verbatim, so the remote admission fences stay intact. The
//     only intentional difference is precedence.
//   - Registration is ID-keyed, with canonical IDs and the domain assertion
//     both sourced from the IM handler's ToolRegistry (registry-backed
//     resolver). Only the allowlisted pilot names get handlers; do NOT add
//     name-keyed registrations here.
//   - Names the registry does not serve (or that carry no tool ID) are
//     skipped with a log, never a crash.

import (
	"log"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// remoteCodingPilotEnvKey is the pilot kill switch, shared with the other
// host pilots: MACLAW_TOOL_DISPATCHER=on publishes the dispatcher; unset or
// off keeps it nil.
const remoteCodingPilotEnvKey = "MACLAW_TOOL_DISPATCHER"

func remoteCodingDispatcherPilotEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(remoteCodingPilotEnvKey))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// remoteCodingPilotToolNames is the pilot's handler-ownership allowlist:
// read-only/lookup names the remote surface serves through LOCAL execution
// (cases of executeRemoteTool, not the ssh_* relay names). Each MUST also
// resolve in the host ToolRegistry (the domain authority and canonical-ID
// source). Deliberately absent: "current_datetime" (remote-served and
// registered, but its clock output is not deterministic across two
// sequential parity calls, violating the pilot's same-input→same-output
// contract). ssh_* names, spawn, todo, and dynamic aliases stay excluded.
var remoteCodingPilotToolNames = []string{
	"web_search",
	"web_fetch",
	"knowledge_search",
	"knowledge_image_search",
	"coding_knowledge_search",
}

// buildRemoteCodingPilotDispatcher registers the pilot handlers under the
// canonical tool IDs carried by the host ToolRegistry (registry-backed
// resolver): Get(name) both asserts the name is in the domain (skipped with a
// log when absent) and yields the registry-assigned ID. Only pilot names get
// handlers, so everything else still falls through to the legacy chain. A
// nil registry registers nothing. It returns the dispatcher and the skipped
// names.
func buildRemoteCodingPilotDispatcher(c *remoteCodingCallbacks, reg *ToolRegistry, names []string) (*agent.NameDispatcher, []string) {
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
			log.Printf("[remote-coding-dispatcher-pilot] %q is not registered (or has no tool ID) in the host ToolRegistry; skipping registration", name)
			skipped = append(skipped, name)
			continue
		}
		id := entry.ID.String()
		if err := d.RegisterID(entry.ID, func(name, argsJSON, callID string, execution agent.ToolCallExecutionContext) (agent.ToolExecutionResult, error) {
			return c.ExecuteToolCallWithContext(name, argsJSON, callID, execution), nil
		}); err != nil {
			log.Printf("[remote-coding-dispatcher-pilot] register %q failed: %v", name, err)
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
func (c *remoteCodingCallbacks) ToolDispatcher() agent.ToolDispatcher {
	if c == nil || !remoteCodingDispatcherPilotEnabled() {
		return nil
	}
	c.dispatcherOnce.Do(func() {
		var reg *ToolRegistry
		if c.agent != nil && c.agent.handler != nil {
			reg = c.agent.handler.registry
		}
		if reg == nil {
			return
		}
		d, skipped := buildRemoteCodingPilotDispatcher(c, reg, remoteCodingPilotToolNames)
		if len(skipped) > 0 {
			log.Printf("[remote-coding-dispatcher-pilot] skipped names=%v", skipped)
		}
		log.Printf("[remote-coding-dispatcher-pilot] registered handlers=%v", d.Handlers())
		c.dispatcher = d
	})
	return c.dispatcher
}

var _ agent.ToolDispatcherProvider = (*remoteCodingCallbacks)(nil)
