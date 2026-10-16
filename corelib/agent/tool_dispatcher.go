package agent

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/toolid"
)

// ToolDispatcher is the Phase 2 convergence target for host-side tool
// dispatch (docs/design/tool-routing-improvement-plan-zh.md Phase 2): hosts
// register handlers under a name instead of maintaining per-host name
// switches. The RunLoop execution-preference chain
// (executeAuthorizedLoopToolCallWithContext) consults a dispatcher before
// falling back to the legacy executor interfaces, so a host can migrate
// handler-by-handler without a big-bang rewrite.
//
// Scope note: dispatchers handle FIRST-PARTY names. Opaque MCP/skill adapter
// names keep their existing binding path (R1/R2) and must not be registered
// here.
type ToolDispatcher interface {
	// Dispatch runs the handler registered for name. callID is the
	// model/provider tool-call identifier and execution is the loop-supplied
	// tool-call context (surface epoch, response binding); both let handlers
	// enter the same idempotent-replay and surface-correlation paths as the
	// legacy executor chain. handled=false means no handler owns the name and
	// the caller must fall back to legacy executors — a dispatcher never
	// invents a denial for unknown names.
	Dispatch(name, argsJSON, callID string, execution ToolCallExecutionContext) (result ToolExecutionResult, handled bool, err error)
}

// ToolDispatcherProvider lets callbacks publish a dispatcher to the core
// loop. Optional: hosts that never register handlers keep the legacy chain
// untouched.
type ToolDispatcherProvider interface {
	ToolDispatcher() ToolDispatcher
}

// ToolHandlerFunc is the handler shape for NameDispatcher. It receives the
// canonical tool name, the raw arguments JSON, the model/provider tool-call
// identifier, and the loop-supplied tool-call context (same inputs as the
// legacy ToolCallContextExecutor chain).
type ToolHandlerFunc func(name, argsJSON, callID string, execution ToolCallExecutionContext) (ToolExecutionResult, error)

// NameDispatcher is a map-based ToolDispatcher: one handler per exact name.
// It can also register handlers under canonical toolid.ToolID values and
// resolve rendered names to IDs at dispatch time (Phase 2: registration and
// dispatch speak in canonical IDs, not bare names). Zero value is ready;
// copies share the handler maps (construct once, then treat as immutable).
type NameDispatcher struct {
	handlers   map[string]ToolHandlerFunc
	idHandlers map[string]ToolHandlerFunc
	// resolveID maps a rendered/legacy name to its canonical ToolID string.
	// Nil means ID-based dispatch is inactive (name-only mode).
	resolveID func(name string) (string, bool)
}

// NewNameDispatcher returns an empty dispatcher.
func NewNameDispatcher() *NameDispatcher {
	return &NameDispatcher{handlers: map[string]ToolHandlerFunc{}}
}

// Register adds a handler for name. Empty/whitespace names and duplicates
// are rejected — silently overwriting a registration would let one host
// switch shadow another's handler, which is exactly the bug this
// convergence removes.
func (d *NameDispatcher) Register(name string, fn ToolHandlerFunc) error {
	n := strings.TrimSpace(name)
	if n == "" {
		return fmt.Errorf("tool dispatcher: empty handler name")
	}
	if fn == nil {
		return fmt.Errorf("tool dispatcher: nil handler for %q", n)
	}
	if d.handlers == nil {
		d.handlers = map[string]ToolHandlerFunc{}
	}
	if _, exists := d.handlers[n]; exists {
		return fmt.Errorf("tool dispatcher: duplicate handler for %q", n)
	}
	d.handlers[n] = fn
	return nil
}

// RegisterID adds a handler under a canonical toolid.ToolID (e.g.
// toolid.MustParse("core:bash")). ID-keyed handlers take precedence over
// name-keyed ones at dispatch when the name resolves to an ID.
func (d *NameDispatcher) RegisterID(id toolid.ToolID, fn ToolHandlerFunc) error {
	if id.IsZero() {
		return fmt.Errorf("tool dispatcher: zero tool id")
	}
	if fn == nil {
		return fmt.Errorf("tool dispatcher: nil handler for %q", id)
	}
	if d.idHandlers == nil {
		d.idHandlers = map[string]ToolHandlerFunc{}
	}
	key := id.String()
	if _, exists := d.idHandlers[key]; exists {
		return fmt.Errorf("tool dispatcher: duplicate handler for %q", key)
	}
	d.idHandlers[key] = fn
	return nil
}

// SetIDResolver installs the rendered-name → canonical-ID mapping used at
// dispatch time. Typically backed by CoreToolRegistry.FindByID /
// CoreToolRegistry ID derivation (core:<name>). Call once before serving.
func (d *NameDispatcher) SetIDResolver(resolve func(name string) (string, bool)) {
	d.resolveID = resolve
}

// Dispatch implements ToolDispatcher. Resolution order: name → ID (when a
// resolver is installed and an ID-keyed handler exists) → name-keyed handler
// → not handled.
func (d *NameDispatcher) Dispatch(name, argsJSON, callID string, execution ToolCallExecutionContext) (ToolExecutionResult, bool, error) {
	if d == nil {
		return ToolExecutionResult{}, false, nil
	}
	if d.resolveID != nil && len(d.idHandlers) > 0 {
		if idStr, ok := d.resolveID(name); ok {
			if fn, exists := d.idHandlers[idStr]; exists {
				res, err := fn(name, argsJSON, callID, execution)
				return res, true, err
			}
		}
	}
	fn, ok := d.handlers[name]
	if !ok {
		return ToolExecutionResult{}, false, nil
	}
	res, err := fn(name, argsJSON, callID, execution)
	return res, true, err
}

// Handlers returns the registered names in sorted order (diagnostics,
// dual-run logging, /status surfaces). ID-keyed handlers render as their ID
// string.
func (d *NameDispatcher) Handlers() []string {
	if d == nil {
		return nil
	}
	names := make([]string, 0, len(d.handlers)+len(d.idHandlers))
	for n := range d.handlers {
		names = append(names, n)
	}
	for id := range d.idHandlers {
		names = append(names, id)
	}
	sort.Strings(names)
	return names
}
