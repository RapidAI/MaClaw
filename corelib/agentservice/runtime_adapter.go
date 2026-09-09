package agentservice

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

// RuntimeExecutor is the thin service adapter between the shared
// agentruntime contract and the existing service Executor. It deliberately
// performs no host-specific Agent logic: shared module prompt/tool/policy
// contributions are assembled here and passed to the wrapped executor, so GUI
// and srv converge on one implementation while the migration is in progress.
type RuntimeExecutor struct {
	inner   Executor
	modules *agentruntime.ModuleRegistry
}

func NewRuntimeExecutor(inner Executor) *RuntimeExecutor {
	return NewRuntimeExecutorWithModules(inner, nil)
}

func NewRuntimeExecutorWithModules(inner Executor, modules *agentruntime.ModuleRegistry) *RuntimeExecutor {
	return &RuntimeExecutor{inner: inner, modules: modules}
}

func (r *RuntimeExecutor) Execute(ctx context.Context, req agentruntime.TurnRequest) (agentruntime.TurnResult, error) {
	if r == nil || r.inner == nil {
		return agentruntime.TurnResult{}, fmt.Errorf("agent runtime executor is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return agentruntime.TurnResult{}, err
	}
	// A nil host is never a reason to fall back to an implicit GUI/default
	// surface. Treat it as an explicit headless host so every module sees the
	// same capability contract regardless of which transport omitted injection.
	req.Host = normalizedRuntimeHost(req.Host)
	concrete, ok := req.Input.(ExecuteRequest)
	if !ok {
		if pointer, pointerOK := req.Input.(*ExecuteRequest); pointerOK && pointer != nil {
			concrete = *pointer
			ok = true
		}
	}
	if !ok {
		return agentruntime.TurnResult{}, fmt.Errorf("agent runtime request payload must be agentservice.ExecuteRequest")
	}
	concrete.Host = req.Host
	// Runtime-owned fields must not be accepted from an in-process caller. They
	// are populated exclusively from the composition-root registry below; this
	// prevents stale or forged invokers from crossing the Runtime boundary.
	concrete.RuntimePrompt = ""
	concrete.RuntimeTools = nil
	concrete.RuntimeExecutableTools = nil
	concrete.RuntimeToolInvoker = nil
	req.Input = concrete
	if r.modules != nil {
		if err := r.modules.EvaluatePolicies(ctx, req); err != nil {
			return agentruntime.TurnResult{}, err
		}
		prompt, err := r.modules.ContributePrompt(ctx, req)
		if err != nil {
			return agentruntime.TurnResult{}, err
		}
		tools, executableNames, err := r.modules.ToolsWithExecutability(ctx, req)
		if err != nil {
			return agentruntime.TurnResult{}, err
		}
		executableTools := make([]agentruntime.ToolDefinition, 0, len(tools))
		for _, definition := range tools {
			if executableNames[definition.Name] {
				executableTools = append(executableTools, definition)
			}
		}
		concrete.RuntimePrompt = prompt
		concrete.RuntimeTools = tools
		concrete.RuntimeExecutableTools = executableTools
		req.Input = concrete
		concrete.RuntimeToolInvoker = runtimeModuleToolInvoker{registry: r.modules, request: req}
		req.Input = concrete
	}
	if req.Events != nil {
		var eventMu sync.Mutex
		var eventErr error
		emitEvent := func(event agentruntime.Event) {
			if err := req.Events.Emit(ctx, event); err != nil {
				// Callback signatures are intentionally fire-and-forget for
				// compatibility with the legacy executor.  Retain the first sink
				// failure and surface it after Execute so a durable outbox failure
				// cannot be mistaken for a successful run.
				eventMu.Lock()
				if eventErr == nil {
					eventErr = err
				}
				eventMu.Unlock()
			}
		}
		previousToken, previousCall, previousResult := concrete.OnToken, concrete.OnToolCall, concrete.OnToolResult
		concrete.OnToken = func(delta string) {
			if previousToken != nil {
				previousToken(delta)
			}
			emitEvent(agentruntime.Event{SchemaVersion: agentruntime.ContractVersion, Type: agentruntime.EventAssistantDelta, Scope: req.Scope, RunID: req.Scope.RunID, Payload: map[string]any{"delta": delta}})
		}
		concrete.OnToolCall = func(name string) {
			if previousCall != nil {
				previousCall(name)
			}
			emitEvent(agentruntime.Event{SchemaVersion: agentruntime.ContractVersion, Type: agentruntime.EventToolCall, Scope: req.Scope, RunID: req.Scope.RunID, Payload: map[string]any{"name": name}})
		}
		concrete.OnToolResult = func(name, result string) {
			if previousResult != nil {
				previousResult(name, result)
			}
			emitEvent(agentruntime.Event{SchemaVersion: agentruntime.ContractVersion, Type: agentruntime.EventToolResult, Scope: req.Scope, RunID: req.Scope.RunID, Payload: map[string]any{"name": name, "result": result}})
		}
		result, err := r.inner.Execute(ctx, concrete)
		eventMu.Lock()
		persistErr := eventErr
		eventMu.Unlock()
		if persistErr != nil {
			wrapped := fmt.Errorf("runtime event sink: %w", persistErr)
			if err != nil {
				// A failed executor can still have emitted token/tool events. Keep
				// both failures visible so callers can distinguish model failure
				// from an outbox durability failure and choose a safe retry policy.
				return agentruntime.TurnResult{}, errors.Join(err, wrapped)
			}
			return agentruntime.TurnResult{}, wrapped
		}
		if err != nil {
			return agentruntime.TurnResult{}, err
		}
		if result == nil {
			return agentruntime.TurnResult{}, nil
		}
		return agentruntime.TurnResult{Output: result, Metadata: result.Metadata}, nil
	}
	result, err := r.inner.Execute(ctx, concrete)
	if err != nil {
		return agentruntime.TurnResult{}, err
	}
	if result == nil {
		return agentruntime.TurnResult{}, nil
	}
	return agentruntime.TurnResult{Output: result, Metadata: result.Metadata}, nil
}

type runtimeModuleToolInvoker struct {
	registry *agentruntime.ModuleRegistry
	request  agentruntime.TurnRequest
}

func (i runtimeModuleToolInvoker) InvokeRuntimeTool(ctx context.Context, name string, args map[string]any) (string, bool, error) {
	if i.registry == nil {
		return "", false, nil
	}
	return i.registry.InvokeTool(ctx, i.request, name, args)
}

func (r *RuntimeExecutor) DescribeCapabilities(ctx context.Context, req agentruntime.CapabilityRequest) (agentruntime.CapabilitySnapshot, error) {
	if r == nil || r.inner == nil {
		return agentruntime.CapabilitySnapshot{}, fmt.Errorf("agent runtime executor is unavailable")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return agentruntime.CapabilitySnapshot{}, err
	}
	req.Host = normalizedRuntimeHost(req.Host)
	neutralPrompt := runtimeTurnInputPrompt(req.Input)
	snapshot := agentruntime.CapabilitySnapshot{
		ContractVersion: agentruntime.ContractVersion,
		Profile:         req.Host.Profile(),
		PromptDigest:    agentruntime.DigestPrompt(""),
	}
	concrete, concreteOK := req.Input.(ExecuteRequest)
	if !concreteOK {
		if pointer, pointerOK := req.Input.(*ExecuteRequest); pointerOK && pointer != nil {
			concrete, concreteOK = *pointer, true
		}
	}
	if concreteOK {
		concrete.Host = req.Host
		concrete.RuntimePrompt = ""
		concrete.RuntimeTools = nil
		concrete.RuntimeExecutableTools = nil
		concrete.RuntimeToolInvoker = nil
		req.Input = concrete
	}
	if describer, ok := r.inner.(CapabilityDescriber); ok && concreteOK {
		caps, err := describer.DescribeCapabilities(ctx, concrete)
		if err != nil {
			return agentruntime.CapabilitySnapshot{}, err
		}
		if caps != nil {
			snapshot.Profile.Name = caps.Executor
			// Capability snapshots are returned directly by the new endpoint;
			// keep only transport-safe policy flags and never leak workspace,
			// data-root, tenant, or user paths/identifiers.
			snapshot.Metadata = safeRuntimeCapabilityMetadata(caps.Metadata)
			// Preserve host-advertised capabilities (for example HTTP or
			// desktop ports) while overlaying executor-derived Agent flags.
			// Replacing the map here made a GUI/headless profile appear to lose
			// capabilities whenever a legacy executor described itself.
			capabilities := make(map[string]bool, len(snapshot.Profile.Capabilities)+4)
			for name, enabled := range snapshot.Profile.Capabilities {
				capabilities[name] = enabled
			}
			capabilities["sessions"] = caps.SupportsSessions
			capabilities["ask_user"] = caps.SupportsAskUser
			capabilities["ssh"] = caps.SupportsSSH
			capabilities["local_bash"] = caps.SupportsLocalBash
			snapshot.Profile.Capabilities = capabilities
			for _, tool := range caps.Tools {
				name := strings.TrimSpace(tool.Name)
				if name == "" {
					continue
				}
				snapshot.Modules = append(snapshot.Modules, agentruntime.ModuleDescriptor{ModuleID: name, Version: "legacy", HeadlessSupport: tool.Enabled})
				snapshot.Tools = append(snapshot.Tools, agentruntime.CapabilityTool{Name: name, Description: tool.Description, Parameters: tool.Parameters, Enabled: tool.Enabled, DisabledReason: tool.DisabledReason})
			}
		}
	}
	if r.modules != nil {
		turnRequest := agentruntime.TurnRequest{Scope: req.Scope, Host: req.Host, Input: req.Input}
		prompt, err := r.modules.ContributePrompt(ctx, turnRequest)
		if err != nil {
			return agentruntime.CapabilitySnapshot{}, err
		}
		snapshot.PromptDigest = agentruntime.DigestPrompt(prompt)
		moduleTools, executableNames, err := r.modules.ToolsWithExecutability(ctx, turnRequest)
		if err != nil {
			return agentruntime.CapabilitySnapshot{}, err
		}
		// Legacy/built-in definitions remain authoritative for a colliding name;
		// this mirrors execution-time precedence so the snapshot never advertises
		// a schema different from the tool that will actually run.
		toolByName := make(map[string]agentruntime.CapabilityTool, len(snapshot.Tools)+len(moduleTools))
		for _, tool := range snapshot.Tools {
			if name := strings.TrimSpace(tool.Name); name != "" {
				toolByName[name] = tool
			}
		}
		for _, tool := range moduleTools {
			if name := strings.TrimSpace(tool.Name); name != "" {
				if _, legacyExists := toolByName[name]; legacyExists {
					continue
				}
				toolByName[name] = agentruntime.CapabilityTool{Name: name, Description: tool.Description, Parameters: tool.Parameters, Enabled: executableNames[name]}
			}
		}
		snapshot.Tools = snapshot.Tools[:0]
		for _, tool := range toolByName {
			snapshot.Tools = append(snapshot.Tools, tool)
		}
		sort.Slice(snapshot.Tools, func(i, j int) bool { return snapshot.Tools[i].Name < snapshot.Tools[j].Name })
		merged := make(map[string]agentruntime.ModuleDescriptor, len(snapshot.Modules))
		for _, descriptor := range snapshot.Modules {
			if descriptor.ModuleID != "" {
				merged[descriptor.ModuleID] = descriptor
			}
		}
		// Composition-root modules are authoritative when they intentionally
		// replace a legacy tool descriptor with the shared contract.
		for _, descriptor := range r.modules.SnapshotForHost(req.Host) {
			if descriptor.ModuleID != "" {
				merged[descriptor.ModuleID] = descriptor
			}
		}
		snapshot.Modules = make([]agentruntime.ModuleDescriptor, 0, len(merged))
		for _, descriptor := range merged {
			snapshot.Modules = append(snapshot.Modules, descriptor)
		}
		sort.Slice(snapshot.Modules, func(i, j int) bool {
			if snapshot.Modules[i].ModuleID == snapshot.Modules[j].ModuleID {
				return snapshot.Modules[i].Version < snapshot.Modules[j].Version
			}
			return snapshot.Modules[i].ModuleID < snapshot.Modules[j].ModuleID
		})
	}
	if strings.TrimSpace(neutralPrompt) != "" {
		// A host may ask for a capability snapshot using the same neutral turn
		// input it will execute. Prefer that effective prompt over a registry-only
		// digest so GUI and headless adapters compare the identical turn surface.
		snapshot.PromptDigest = agentruntime.DigestPrompt(neutralPrompt)
	}
	// Compute the parity digest only after legacy and composition-root module
	// surfaces have been merged. Metadata is intentionally excluded by the
	// shared helper, so persistence/rate-limit differences do not create false
	// GUI-vs-headless behaviour drift.
	snapshot.SurfaceDigest = agentruntime.DigestCapabilitySurface(snapshot)
	return snapshot, nil
}

func runtimeTurnInputPrompt(input any) string {
	if value, ok := agentruntime.DecodeTurnInput(input); ok {
		return value.SystemPrompt
	}
	return ""
}

func normalizedRuntimeHost(host agentruntime.HostCapabilities) agentruntime.HostCapabilities {
	if host == nil {
		return agentruntime.HeadlessHostCapabilities{}
	}
	return host
}

func safeRuntimeCapabilityMetadata(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	allowed := map[string]bool{
		"bash_enabled": true, "bash_trusted_single_user": true,
		"ssh_direct_connect_enabled": true, "ssh_file_transfer_enabled": true,
		"tool_policy": true, "mutation_scope": true,
	}
	out := make(map[string]string, len(allowed))
	for key, value := range in {
		if allowed[key] {
			value = strings.TrimSpace(value)
			// Capability metadata is transport-facing and should remain bounded
			// even when a custom executor returns an unexpectedly large value.
			if len([]rune(value)) > 256 {
				value = string([]rune(value)[:256])
			}
			if value != "" {
				out[key] = value
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func (r *RuntimeExecutor) Close() error {
	if r == nil || r.inner == nil {
		return nil
	}
	if closer, ok := r.inner.(interface{ Close() error }); ok {
		return closer.Close()
	}
	return nil
}

var _ agentruntime.Runtime = (*RuntimeExecutor)(nil)
