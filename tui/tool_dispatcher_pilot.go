package main

// Phase 2 ToolDispatcher convergence pilot on the TUI host, mirroring the
// agentservice and guiapp IM pilots: a NameDispatcher registers a set of
// read-only first-party names by their canonical tool IDs, and tuiCallbacks
// publishes it via agent.ToolDispatcherProvider. Every TUI loop
// (agent.RunLoop in app/pipe/rpc/weixin/scheduler/loop-command) executes tool
// calls through the core loop's dispatcher-first chain, so once published
// these names are owned outright by the dispatcher.
//
// Pilot discipline:
//   - The kill switch MACLAW_TOOL_DISPATCHER must be on/1/true/yes for the
//     provider to publish anything; unset or any other value keeps the
//     provider nil and the legacy chain fully authoritative (default OFF).
//   - Every handler enters through tuiCallbacks.ExecuteTool — the exact
//     legacy entry (spawn-tool special case, argument parsing, read-only-child
//     guard, _ctx injection, toolRegistry.ExecuteCtx) — returning the raw
//     text and letting the core loop classify the outcome, exactly like the
//     legacy final rung, for read-only AND mutate-class names. The only
//     intentional difference is precedence.
//   - Registration is ID-keyed (core:<exact name>) with a resolver backed by
//     the host's real CoreToolRegistry (Lookup(name).ID) — the registry is
//     the domain assertion too: names it does not serve are skipped with a
//     log, never a crash. Do NOT add name-keyed registrations here.

import (
	"log"
	"os"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// tuiDispatcherEnvKey is the pilot kill switch, shared with the other host
// pilots: MACLAW_TOOL_DISPATCHER=on publishes the dispatcher; unset or off
// keeps it nil.
const tuiDispatcherEnvKey = "MACLAW_TOOL_DISPATCHER"

func tuiDispatcherPilotEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(tuiDispatcherEnvKey))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// tuiPilotToolNames is the pilot registration set: read-only/lookup AND
// mutate-class names the TUI host registers via RegisterCoreTools (exact
// case; the registry renders and matches "Glob", never a lowercase alias).
// All are registered unconditionally — memory's nil-store behavior is a
// runtime dependency, not a registration gate. Execute-kind (bash/ssh) and
// host-adapter names stay excluded.
//
// Mutate-class inclusion (write_file/edit_file/memory) is safe by the same
// construction as the read-only names: every handler enters through
// tuiCallbacks.ExecuteTool, whose prefix (spawn special case, argument parse,
// read-only-child guard, _ctx injection) and registry dispatch all sit
// INSIDE that entry, and loop-level gates (authorizeLoopTool admission,
// replan skip) run upstream of the dispatcher consultation for both paths —
// the dispatcher only replaces the executor-chain segment.
var tuiPilotToolNames = []string{
	"web_search",
	"web_fetch",
	"read_file",
	"Glob",
	"list_directory",
	"ask_user",
	"write_file",
	"edit_file",
	"memory",
}

// buildTUIPilotDispatcher registers the pilot handlers under their canonical
// tool IDs, with a resolver backed by the host's CoreToolRegistry:
// Lookup(name) both asserts the name is in the TUI domain (skipped with a
// log when absent) and yields the registry-assigned canonical ID. Resolving
// a non-pilot name is harmless — only pilot IDs have handlers, so everything
// else still falls through to the legacy chain. It returns the dispatcher
// and the skipped names so tests can assert the filtering.
func buildTUIPilotDispatcher(c *tuiCallbacks, names []string) (*agent.NameDispatcher, []string) {
	d := agent.NewNameDispatcher()
	resolvable := make(map[string]string, len(names))
	var skipped []string
	reg := c.app.toolRegistry
	for _, name := range names {
		entry, ok := reg.Lookup(name)
		if !ok || entry.ID.IsZero() {
			log.Printf("[tui-dispatcher-pilot] %q is not registered in the TUI CoreToolRegistry; skipping registration", name)
			skipped = append(skipped, name)
			continue
		}
		id := entry.ID.String()
		if err := d.RegisterID(entry.ID, func(name, argsJSON, _ string, _ agent.ToolCallExecutionContext) (agent.ToolExecutionResult, error) {
			return agent.ToolExecutionResult{Result: c.ExecuteTool(name, argsJSON)}, nil
		}); err != nil {
			log.Printf("[tui-dispatcher-pilot] register %q failed: %v", name, err)
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
// loop keeps using the legacy chain unchanged. Built lazily per callback;
// without an app/toolRegistry there is nothing to dispatch against.
func (c *tuiCallbacks) ToolDispatcher() agent.ToolDispatcher {
	if c == nil || !tuiDispatcherPilotEnabled() {
		return nil
	}
	c.dispatcherOnce.Do(func() {
		if c.app == nil || c.app.toolRegistry == nil {
			return
		}
		d, skipped := buildTUIPilotDispatcher(c, tuiPilotToolNames)
		if len(skipped) > 0 {
			log.Printf("[tui-dispatcher-pilot] skipped names=%v", skipped)
		}
		log.Printf("[tui-dispatcher-pilot] registered handlers=%v", d.Handlers())
		c.dispatcher = d
	})
	return c.dispatcher
}

var _ agent.ToolDispatcherProvider = (*tuiCallbacks)(nil)

// sortedTUIPilotNames is a deterministic copy for tests and logs.
func sortedTUIPilotNames() []string {
	names := append([]string(nil), tuiPilotToolNames...)
	sort.Strings(names)
	return names
}
