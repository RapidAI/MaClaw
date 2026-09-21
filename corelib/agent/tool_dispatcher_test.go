package agent

import (
	"errors"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/toolid"
)

func TestNameDispatcherRegisterAndDispatch(t *testing.T) {
	d := NewNameDispatcher()
	called := ""
	if err := d.Register("bash", func(name, args, callID string, _ ToolCallExecutionContext) (ToolExecutionResult, error) {
		called = name + "|" + args + "|" + callID
		return ToolExecutionResult{Result: "ok"}, nil
	}); err != nil {
		t.Fatalf("register: %v", err)
	}
	res, handled, err := d.Dispatch("bash", "{}", "call-9", ToolCallExecutionContext{})
	if err != nil || !handled || res.Result != "ok" {
		t.Fatalf("dispatch: %+v %v %v", res, handled, err)
	}
	if called != "bash|{}|call-9" {
		t.Fatalf("handler saw %q", called)
	}
}

func TestNameDispatcherUnknownNameFallsThrough(t *testing.T) {
	d := NewNameDispatcher()
	res, handled, err := d.Dispatch("nope", "{}", "call-1", ToolCallExecutionContext{})
	if handled || err != nil || res.Result != "" {
		t.Fatalf("unknown name must fall through silently: %+v %v %v", res, handled, err)
	}
	var nilD *NameDispatcher
	if _, handled, _ := nilD.Dispatch("bash", "{}", "call-1", ToolCallExecutionContext{}); handled {
		t.Fatalf("nil dispatcher must not handle")
	}
}

func TestNameDispatcherRejectsBadRegistrations(t *testing.T) {
	d := NewNameDispatcher()
	if err := d.Register("  ", func(string, string, string, ToolCallExecutionContext) (ToolExecutionResult, error) {
		return ToolExecutionResult{}, nil
	}); err == nil {
		t.Fatalf("empty name accepted")
	}
	if err := d.Register("bash", nil); err == nil {
		t.Fatalf("nil handler accepted")
	}
	fn := func(string, string, string, ToolCallExecutionContext) (ToolExecutionResult, error) {
		return ToolExecutionResult{}, nil
	}
	if err := d.Register("bash", fn); err != nil {
		t.Fatalf("first register: %v", err)
	}
	if err := d.Register("bash", fn); err == nil {
		t.Fatalf("duplicate registration accepted — shadowing must be rejected")
	}
	if got := d.Handlers(); len(got) != 1 || got[0] != "bash" {
		t.Fatalf("handlers=%v", got)
	}
}

func TestNameDispatcherPropagatesHandlerError(t *testing.T) {
	d := NewNameDispatcher()
	_ = d.Register("write_file", func(string, string, string, ToolCallExecutionContext) (ToolExecutionResult, error) {
		return ToolExecutionResult{}, errors.New("disk full")
	})
	res, handled, err := d.Dispatch("write_file", "{}", "call-3", ToolCallExecutionContext{})
	if !handled || err == nil || err.Error() != "disk full" {
		t.Fatalf("err propagation: %v", err)
	}
	_ = res
}

type dispatcherProviderCallbacks struct {
	LoopCallbacks
	d ToolDispatcher
}

func (c *dispatcherProviderCallbacks) ToolDispatcher() ToolDispatcher { return c.d }

type legacyExecutorCallbacks struct {
	LoopCallbacks
	executed string
}

func (c *legacyExecutorCallbacks) ExecuteTool(name, argsJSON string) string {
	c.executed = name
	return "legacy:" + name
}

// The loop's execution-preference chain must consult the dispatcher before
// legacy executors, and fall through when the dispatcher declines.
func TestExecuteChainDispatcherPrecedesLegacy(t *testing.T) {
	d := NewNameDispatcher()
	_ = d.Register("bash", func(name, args, callID string, _ ToolCallExecutionContext) (ToolExecutionResult, error) {
		return ToolExecutionResult{Result: "dispatched:" + name + ":" + callID}, nil
	})
	legacy := &legacyExecutorCallbacks{}
	cb := &dispatcherProviderCallbacks{d: d, LoopCallbacks: legacy}

	res := executeAuthorizedLoopToolCallWithContext(cb, "bash", "{}", "call-1", ToolCallExecutionContext{})
	if res.Result != "dispatched:bash:call-1" {
		t.Fatalf("dispatcher should win and receive the call ID: %q", res.Result)
	}
	if legacy.executed != "" {
		t.Fatalf("legacy executor must not run for dispatched name")
	}
	if res.Outcome != ToolExecutionOutcomeOK {
		t.Fatalf("outcome must be classified: %q", res.Outcome)
	}

	// Unknown name falls through to the legacy chain.
	res = executeAuthorizedLoopToolCallWithContext(cb, "read_file", "{}", "call-2", ToolCallExecutionContext{})
	if res.Result != "legacy:read_file" || legacy.executed != "read_file" {
		t.Fatalf("fallback broken: %q executed=%q", res.Result, legacy.executed)
	}
}

func TestExecuteChainWithoutProviderUnchanged(t *testing.T) {
	legacy := &legacyExecutorCallbacks{}
	res := executeAuthorizedLoopToolCallWithContext(legacy, "bash", "{}", "call-1", ToolCallExecutionContext{})
	if res.Result != "legacy:bash" {
		t.Fatalf("no-provider path changed: %q", res.Result)
	}
}

func TestNameDispatcherIDKeyedDispatch(t *testing.T) {
	d := NewNameDispatcher()
	var gotID, gotName string
	if err := d.RegisterID(toolid.MustParse("core:bash"), func(name, args, callID string, _ ToolCallExecutionContext) (ToolExecutionResult, error) {
		gotID, gotName = "core:bash", name
		return ToolExecutionResult{Result: "by-id"}, nil
	}); err != nil {
		t.Fatalf("register id: %v", err)
	}
	if err := d.Register("bash", func(name, args, callID string, _ ToolCallExecutionContext) (ToolExecutionResult, error) {
		return ToolExecutionResult{Result: "by-name"}, nil
	}); err != nil {
		t.Fatalf("register name: %v", err)
	}
	// Without a resolver, name lookup wins (name-only mode).
	res, handled, _ := d.Dispatch("bash", "{}", "c1", ToolCallExecutionContext{})
	if !handled || res.Result != "by-name" {
		t.Fatalf("name-only mode: %+v", res)
	}
	// With a resolver mapping the rendered name to the canonical ID, the
	// ID-keyed handler wins even though a name handler also exists.
	d.SetIDResolver(func(name string) (string, bool) {
		if name == "bash" {
			return "core:bash", true
		}
		return "", false
	})
	res, handled, _ = d.Dispatch("bash", "{}", "c1", ToolCallExecutionContext{})
	if !handled || res.Result != "by-id" || gotID != "core:bash" || gotName != "bash" {
		t.Fatalf("id mode: %+v id=%q name=%q", res, gotID, gotName)
	}
	// Unresolvable name falls back to name-keyed handler.
	res, handled, _ = d.Dispatch("web_search", "{}", "c2", ToolCallExecutionContext{})
	if handled {
		t.Fatalf("unregistered name must not handle: %+v", res)
	}
}

func TestNameDispatcherRegisterIDValidation(t *testing.T) {
	d := NewNameDispatcher()
	fn := func(string, string, string, ToolCallExecutionContext) (ToolExecutionResult, error) {
		return ToolExecutionResult{}, nil
	}
	if err := d.RegisterID(toolid.ToolID{}, fn); err == nil {
		t.Fatalf("zero id accepted")
	}
	if err := d.RegisterID(toolid.MustParse("core:bash"), nil); err == nil {
		t.Fatalf("nil handler accepted")
	}
	if err := d.RegisterID(toolid.MustParse("core:bash"), fn); err != nil {
		t.Fatalf("first: %v", err)
	}
	if err := d.RegisterID(toolid.MustParse("core:bash"), fn); err == nil {
		t.Fatalf("duplicate id accepted")
	}
	h := d.Handlers()
	if len(h) != 1 || h[0] != "core:bash" {
		t.Fatalf("handlers=%v", h)
	}
}
