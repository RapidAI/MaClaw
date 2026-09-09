// Package agentruntime defines the transport-neutral contract shared by the
// GUI and MaClawSrv hosts. The package intentionally contains no Wails, HTTP,
// or agentservice imports; host-specific state is supplied through TurnRequest
// and narrow capability ports.
package agentruntime

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"
)

const ContractVersion = "maclaw.agentruntime/v1"
const CapabilityUnavailableCode = "capability_unavailable"

// CapabilityUnavailableError is the stable error shape hosts should return
// when a GUI-only side effect is requested from srv/headless runtime.
type CapabilityUnavailableError struct {
	Capability string
}

// Module is the common descriptor contract for builtin and tenant-projected
// runtime modules. Implementations must be stateless; request state belongs in
// TurnRequest and host adapters.
type Module interface {
	Descriptor() ModuleDescriptor
}

type ToolModule interface {
	Module
	Tools(context.Context, TurnRequest) ([]ToolDefinition, error)
}

// ToolHandler is an optional execution contract for modules that contribute
// callable tools. A module may contribute descriptors only (for capability
// discovery); hosts must expose a tool to the model only when a handler is
// available, preventing an advertised-but-unknown invocation.
type ToolHandler interface {
	ToolModule
	InvokeTool(context.Context, TurnRequest, string, map[string]any) (string, error)
}

type PromptContributor interface {
	Module
	ContributePrompt(context.Context, TurnRequest) (string, error)
}

type PolicyModule interface {
	Module
	EvaluatePolicy(context.Context, TurnRequest) error
}

type ToolDefinition struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
}

// ModuleRegistry is the single deterministic registration point shared by GUI
// and srv composition roots. Hosts may filter descriptors by capabilities, but
// they must not maintain a second hand-written module list.
type ModuleRegistry struct {
	modules map[string]Module
}

func NewModuleRegistry(modules ...Module) (*ModuleRegistry, error) {
	registry := &ModuleRegistry{modules: make(map[string]Module)}
	for _, module := range modules {
		if isNilModule(module) {
			continue
		}
		descriptor := module.Descriptor()
		id := strings.TrimSpace(descriptor.ModuleID)
		if id == "" {
			return nil, fmt.Errorf("runtime module id is required")
		}
		if strings.TrimSpace(descriptor.Version) == "" {
			return nil, fmt.Errorf("runtime module %q version is required", id)
		}
		if _, exists := registry.modules[id]; exists {
			return nil, fmt.Errorf("runtime module %q is registered more than once", id)
		}
		registry.modules[id] = module
	}
	return registry, nil
}

// isNilModule also catches an interface containing a typed nil pointer. Such
// values are common when optional host integrations are assembled and would
// otherwise panic when Descriptor is called during registry construction.
func isNilModule(module Module) bool {
	if module == nil {
		return true
	}
	value := reflect.ValueOf(module)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (r *ModuleRegistry) Snapshot() []ModuleDescriptor {
	return r.SnapshotForHost(nil)
}

// SnapshotForHost returns only modules that can run on the supplied host.
// Passing nil intentionally returns the complete registry for process-level
// diagnostics; Runtime adapters normalize omitted hosts before requesting a
// turn-scoped snapshot.
func (r *ModuleRegistry) SnapshotForHost(host HostCapabilities) []ModuleDescriptor {
	if r == nil {
		return nil
	}
	descriptors := make([]ModuleDescriptor, 0, len(r.modules))
	for _, module := range r.modules {
		if !moduleSupportedByHost(module, host) {
			continue
		}
		descriptor := module.Descriptor()
		// Registry identity is normalized at registration time; normalize the
		// exported descriptor as well so a module returning " foo " cannot be
		// dropped by sorted lookup or create whitespace-only drift in snapshots.
		descriptor.ModuleID = strings.TrimSpace(descriptor.ModuleID)
		descriptor.Version = strings.TrimSpace(descriptor.Version)
		descriptors = append(descriptors, descriptor)
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].ModuleID < descriptors[j].ModuleID })
	return descriptors
}

func (r *ModuleRegistry) sortedModules() []Module {
	if r == nil {
		return nil
	}
	ids := make([]string, 0, len(r.modules))
	for id := range r.modules {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	modules := make([]Module, 0, len(ids))
	for _, id := range ids {
		modules = append(modules, r.modules[id])
	}
	return modules
}

// ContributePrompt deterministically combines all registered prompt
// contributors. The registry, rather than each host, owns ordering so GUI
// and headless executions cannot drift when a module is added.
func (r *ModuleRegistry) ContributePrompt(ctx context.Context, request TurnRequest) (string, error) {
	var sections []string
	for _, module := range r.sortedModules() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if !moduleSupportedByHost(module, request.Host) {
			continue
		}
		contributor, ok := module.(PromptContributor)
		if !ok {
			continue
		}
		section, err := contributor.ContributePrompt(ctx, request)
		if err != nil {
			return "", fmt.Errorf("runtime module %q prompt: %w", module.Descriptor().ModuleID, err)
		}
		if section = strings.TrimSpace(section); section != "" {
			sections = append(sections, section)
		}
	}
	return strings.Join(sections, "\n\n"), nil
}

// Tools returns the deterministic tool surface contributed by registered
// modules and rejects duplicate names before a host can expose an ambiguous
// schema to a model.
func (r *ModuleRegistry) Tools(ctx context.Context, request TurnRequest) ([]ToolDefinition, error) {
	tools, _, err := r.collectTools(ctx, request)
	return tools, err
}

// ToolsWithExecutability aggregates definitions once and returns the names
// that have a ToolHandler. Adapters should prefer this method when they need
// both the advertised and executable surfaces, avoiding duplicate provider
// calls for stateful integrations.
func (r *ModuleRegistry) ToolsWithExecutability(ctx context.Context, request TurnRequest) ([]ToolDefinition, map[string]bool, error) {
	return r.collectTools(ctx, request)
}

func (r *ModuleRegistry) collectTools(ctx context.Context, request TurnRequest) ([]ToolDefinition, map[string]bool, error) {
	var tools []ToolDefinition
	seen := make(map[string]string)
	executable := make(map[string]bool)
	for _, module := range r.sortedModules() {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		if !moduleSupportedByHost(module, request.Host) {
			continue
		}
		provider, ok := module.(ToolModule)
		if !ok {
			continue
		}
		definitions, err := provider.Tools(ctx, request)
		if err != nil {
			return nil, nil, fmt.Errorf("runtime module %q tools: %w", module.Descriptor().ModuleID, err)
		}
		for _, definition := range definitions {
			definition.Name = strings.TrimSpace(definition.Name)
			if definition.Name == "" {
				return nil, nil, fmt.Errorf("runtime module %q returned a tool without a name", module.Descriptor().ModuleID)
			}
			if previous, exists := seen[definition.Name]; exists {
				return nil, nil, fmt.Errorf("runtime tool %q is contributed by both %q and %q", definition.Name, previous, module.Descriptor().ModuleID)
			}
			seen[definition.Name] = module.Descriptor().ModuleID
			tools = append(tools, definition)
			if _, executableModule := module.(ToolHandler); executableModule {
				executable[definition.Name] = true
			}
		}
	}
	return tools, executable, nil
}

// InvokeTool routes a previously aggregated tool name to its owning module.
// The boolean distinguishes an unknown/non-executable tool from a handler
// failure, allowing adapters to preserve the stable unknown-tool outcome.
func (r *ModuleRegistry) InvokeTool(ctx context.Context, request TurnRequest, name string, args map[string]any) (string, bool, error) {
	name = strings.TrimSpace(name)
	if err := ctx.Err(); err != nil {
		return "", true, err
	}
	for _, module := range r.sortedModules() {
		if err := ctx.Err(); err != nil {
			return "", true, err
		}
		provider, ok := module.(ToolModule)
		if !ok {
			continue
		}
		definitions, err := provider.Tools(ctx, request)
		if err != nil {
			return "", true, fmt.Errorf("runtime module %q tools: %w", module.Descriptor().ModuleID, err)
		}
		for _, definition := range definitions {
			if strings.TrimSpace(definition.Name) != name {
				continue
			}
			if reason := moduleUnavailableReason(module, request.Host); reason != "" {
				// Keep a known-but-unsupported tool distinguishable from an
				// unknown tool. This is important for stale GUI surfaces and
				// headless clients replaying a capability snapshot from another
				// host: callers can present a stable remediation instead of
				// silently treating the request as a typo.
				return "", true, CapabilityUnavailableError{Capability: reason}
			}
			handler, executable := module.(ToolHandler)
			if !executable {
				return "", false, nil
			}
			if err := ctx.Err(); err != nil {
				return "", true, err
			}
			result, err := handler.InvokeTool(ctx, request, name, args)
			return result, true, err
		}
	}
	return "", false, nil
}

// ExecutableToolNames returns the subset of the aggregated tool surface that
// has an invocation handler. Descriptors without handlers remain visible to
// capability discovery but are never exposed to the model by a legacy host.
func (r *ModuleRegistry) ExecutableToolNames(ctx context.Context, request TurnRequest) (map[string]bool, error) {
	_, names, err := r.collectTools(ctx, request)
	return names, err
}

// EvaluatePolicies runs all policy modules in registration order before an
// executor is invoked. A denied request stops at the first deterministic
// failure, preventing hosts from implementing divergent policy sequencing.
func (r *ModuleRegistry) EvaluatePolicies(ctx context.Context, request TurnRequest) error {
	for _, module := range r.sortedModules() {
		policy, ok := module.(PolicyModule)
		if !ok {
			continue
		}
		if !moduleSupportedByHost(module, request.Host) {
			continue
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := policy.EvaluatePolicy(ctx, request); err != nil {
			return fmt.Errorf("runtime module %q policy: %w", module.Descriptor().ModuleID, err)
		}
	}
	return nil
}

// moduleSupportedByHost keeps host-specific modules out of a turn surface
// when the current host cannot provide their required side effects. A nil
// host is treated as unspecified for direct registry callers; RuntimeExecutor
// normalizes nil hosts to an explicit headless profile before reaching here.
func moduleSupportedByHost(module Module, host HostCapabilities) bool {
	return moduleUnavailableReason(module, host) == ""
}

func moduleUnavailableReason(module Module, host HostCapabilities) string {
	if module == nil || host == nil {
		return ""
	}
	descriptor := module.Descriptor()
	profile := host.Profile()
	if profile.Headless && !descriptor.HeadlessSupport {
		return "headless"
	}
	for _, required := range descriptor.RequiredHostCapabilities {
		required = strings.TrimSpace(required)
		if required == "" {
			continue
		}
		available := false
		switch strings.ToLower(required) {
		case "desktop_capture":
			_, available = host.DesktopCapture()
		case "document_launcher":
			_, available = host.DocumentLauncher()
		case "url_launcher":
			_, available = host.URLLauncher()
		case "speech_renderer":
			_, available = host.SpeechRenderer()
		}
		if !available {
			available = profile.Capabilities[required]
		}
		if !available {
			return required
		}
	}
	return ""
}

func (e CapabilityUnavailableError) Error() string {
	if e.Capability == "" {
		return CapabilityUnavailableCode
	}
	return fmt.Sprintf("%s: %s", CapabilityUnavailableCode, e.Capability)
}

// Runtime is the single entry point for a non-UI Agent turn. Implementations
// must be safe for concurrent calls and must not retain the current principal
// or session between turns.
type Runtime interface {
	Execute(context.Context, TurnRequest) (TurnResult, error)
	DescribeCapabilities(context.Context, CapabilityRequest) (CapabilitySnapshot, error)
	Close() error
}

// Scope identifies the authenticated execution boundary. Empty optional
// fields are allowed for process-level capability discovery but never widen a
// host adapter's authorization checks.
type Scope struct {
	TenantID   string `json:"tenant_id"`
	UserID     string `json:"user_id"`
	InstanceID string `json:"instance_id"`
	SessionID  string `json:"session_id"`
	RunID      string `json:"run_id,omitempty"`
}

// TurnRequest carries request-scoped input and host adapters. Input is kept as
// an opaque value at this boundary so agentruntime does not duplicate the
// service's persistence DTOs or create an import cycle. The service adapter
// validates and decodes the concrete request before execution.
type TurnRequest struct {
	Scope  Scope            `json:"scope"`
	Input  any              `json:"input,omitempty"`
	Host   HostCapabilities `json:"-"`
	Events EventSink        `json:"-"`
}

type TurnResult struct {
	Output   any               `json:"output,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type CapabilityRequest struct {
	Scope Scope            `json:"scope"`
	Host  HostCapabilities `json:"-"`
	// Input may carry a host/service-specific capability request while the
	// contract remains independent of the service package.
	Input any `json:"input,omitempty"`
}

type CapabilitySnapshot struct {
	ContractVersion string             `json:"contract_version"`
	Profile         CapabilityProfile  `json:"profile"`
	Modules         []ModuleDescriptor `json:"modules,omitempty"`
	Tools           []CapabilityTool   `json:"tools,omitempty"`
	Metadata        map[string]string  `json:"metadata,omitempty"`
	// PromptDigest lets GUI and headless hosts compare the effective shared
	// prompt without exposing prompt text in a capability response.
	PromptDigest string `json:"prompt_digest,omitempty"`
	// SurfaceDigest is a stable digest of the effective, transport-neutral
	// capability surface (host profile, modules, tools and prompt digest). It
	// deliberately excludes Metadata because metadata contains persistence and
	// operational details that may differ between hosts without changing Agent
	// behaviour. GUI, srv and TUI can compare this value before claiming parity.
	SurfaceDigest string `json:"surface_digest,omitempty"`
}

// Capability metadata keys shared by GUI, headless and future hosts. Keeping
// these identifiers in the Runtime contract avoids transport-specific string
// literals drifting when clients inspect persistence guarantees.
const (
	CapabilityMetadataLifecyclePersistenceMode = "lifecycle_persistence_mode"
	CapabilityMetadataDurableOutbox            = "durable_outbox"
	CapabilityMetadataAdmissionAtomic          = "admission_atomic"
	CapabilityMetadataAdmissionOutboxAtomic    = "admission_outbox_atomic"
	CapabilityMetadataCompletionAtomic         = "completion_atomic"
	CapabilityMetadataCompletionOutboxAtomic   = "completion_outbox_atomic"
	CapabilityMetadataTerminalOutboxAtomic     = "terminal_outbox_atomic"
	CapabilityMetadataRateLimitEnabled         = "rate_limit_enabled"
	CapabilityMetadataRateLimitRate            = "rate_limit_rate"
	CapabilityMetadataRateLimitBurst           = "rate_limit_burst"
	CapabilityMetadataRateLimitTenantLimit     = "rate_limit_tenant_limit"
)

type CapabilityTool struct {
	Name           string         `json:"name"`
	Description    string         `json:"description,omitempty"`
	Parameters     map[string]any `json:"parameters,omitempty"`
	Enabled        bool           `json:"enabled"`
	DisabledReason string         `json:"disabled_reason,omitempty"`
}

// DigestPrompt returns the stable SHA-256 digest used in capability parity
// checks. Empty prompts intentionally produce the digest of an empty string.
func DigestPrompt(prompt string) string {
	digest := sha256.Sum256([]byte(strings.TrimSpace(prompt)))
	return fmt.Sprintf("%x", digest[:])
}

// DigestCapabilitySurface returns a stable SHA-256 digest for the effective
// Agent capability surface. Inputs are copied and sorted so callers may pass
// map-backed or concurrently assembled slices without introducing ordering
// drift. The digest is intentionally limited to behaviour-affecting fields;
// transport metadata (paths, tenant identifiers, persistence flags) is not
// included and must never be used as a parity signal.
func DigestCapabilitySurface(snapshot CapabilitySnapshot) string {
	type canonicalModule struct {
		ModuleID                 string   `json:"module_id"`
		Version                  string   `json:"version"`
		HeadlessSupport          bool     `json:"headless_support"`
		RequiredHostCapabilities []string `json:"required_host_capabilities,omitempty"`
	}
	type canonicalTool struct {
		Name           string         `json:"name"`
		Description    string         `json:"description,omitempty"`
		Parameters     map[string]any `json:"parameters,omitempty"`
		Enabled        bool           `json:"enabled"`
		DisabledReason string         `json:"disabled_reason,omitempty"`
	}
	type canonicalSurface struct {
		ContractVersion string            `json:"contract_version"`
		Profile         CapabilityProfile `json:"profile"`
		Modules         []canonicalModule `json:"modules,omitempty"`
		Tools           []canonicalTool   `json:"tools,omitempty"`
		PromptDigest    string            `json:"prompt_digest,omitempty"`
	}

	profile := CapabilityProfile{
		Name:     strings.TrimSpace(snapshot.Profile.Name),
		Headless: snapshot.Profile.Headless,
	}
	if len(snapshot.Profile.Capabilities) > 0 {
		profile.Capabilities = make(map[string]bool, len(snapshot.Profile.Capabilities))
		for name, enabled := range snapshot.Profile.Capabilities {
			name = strings.TrimSpace(name)
			if name != "" {
				profile.Capabilities[name] = enabled
			}
		}
	}

	modules := make([]canonicalModule, 0, len(snapshot.Modules))
	for _, module := range snapshot.Modules {
		id := strings.TrimSpace(module.ModuleID)
		if id == "" {
			continue
		}
		required := make([]string, 0, len(module.RequiredHostCapabilities))
		for _, capability := range module.RequiredHostCapabilities {
			if capability = strings.TrimSpace(capability); capability != "" {
				required = append(required, capability)
			}
		}
		sort.Strings(required)
		modules = append(modules, canonicalModule{
			ModuleID: id, Version: strings.TrimSpace(module.Version),
			HeadlessSupport: module.HeadlessSupport, RequiredHostCapabilities: required,
		})
	}
	sort.Slice(modules, func(i, j int) bool {
		if modules[i].ModuleID != modules[j].ModuleID {
			return modules[i].ModuleID < modules[j].ModuleID
		}
		if modules[i].Version != modules[j].Version {
			return modules[i].Version < modules[j].Version
		}
		left, _ := json.Marshal(modules[i])
		right, _ := json.Marshal(modules[j])
		return string(left) < string(right)
	})

	tools := make([]canonicalTool, 0, len(snapshot.Tools))
	for _, tool := range snapshot.Tools {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			continue
		}
		parameters := cloneJSONMap(tool.Parameters)
		if len(tool.Parameters) > 0 && parameters == nil {
			// A non-empty schema that cannot survive JSON canonicalization is
			// invalid for a capability snapshot. Do not silently collapse it to
			// an empty parameter object: that would let GUI and srv publish the
			// same digest for materially different, non-serializable contracts.
			return DigestPrompt("invalid capability surface")
		}
		tools = append(tools, canonicalTool{
			Name: name, Description: strings.TrimSpace(tool.Description),
			Parameters: parameters, Enabled: tool.Enabled,
			DisabledReason: strings.TrimSpace(tool.DisabledReason),
		})
	}
	sort.Slice(tools, func(i, j int) bool {
		if tools[i].Name != tools[j].Name {
			return tools[i].Name < tools[j].Name
		}
		// Duplicate names should already be rejected by ModuleRegistry, but
		// capability snapshots may be assembled by legacy adapters. Tie-break
		// on the complete canonical JSON so even that defensive path remains
		// independent of provider iteration order.
		left, _ := json.Marshal(tools[i])
		right, _ := json.Marshal(tools[j])
		return string(left) < string(right)
	})

	canonical := canonicalSurface{
		ContractVersion: strings.TrimSpace(snapshot.ContractVersion),
		Profile:         profile,
		Modules:         modules,
		Tools:           tools,
		PromptDigest:    strings.TrimSpace(snapshot.PromptDigest),
	}
	payload, err := json.Marshal(canonical)
	if err != nil {
		// Capability parameters are expected to be JSON values. If an embedded
		// host violates that contract, return a deterministic failure digest
		// rather than leaking an error through a discovery endpoint.
		return DigestPrompt("invalid capability surface")
	}
	digest := sha256.Sum256(payload)
	return fmt.Sprintf("%x", digest[:])
}

// cloneJSONMap performs a JSON round-trip to detach nested parameter maps and
// slices from module-owned state. encoding/json also gives us deterministic
// map-key ordering for the digest canonicalization above.
func cloneJSONMap(in map[string]any) map[string]any {
	if len(in) == 0 {
		return nil
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(payload, &out); err != nil {
		return nil
	}
	return out
}

type CapabilityProfile struct {
	Name         string          `json:"name"`
	Headless     bool            `json:"headless"`
	Capabilities map[string]bool `json:"capabilities,omitempty"`
}

// HostCapabilities is deliberately typed and minimal. A missing capability is
// represented by (nil, false) and must result in a stable unavailable outcome,
// not an unknown-tool error.
type HostCapabilities interface {
	Profile() CapabilityProfile
	DesktopCapture() (DesktopCapturePort, bool)
	DocumentLauncher() (DocumentLauncherPort, bool)
	URLLauncher() (URLLauncherPort, bool)
	SpeechRenderer() (SpeechRendererPort, bool)
}

type DesktopCapturePort interface {
	Capture(context.Context, DesktopCaptureRequest) ([]byte, string, error)
}

type DocumentLauncherPort interface {
	OpenDocument(context.Context, string) error
}

type URLLauncherPort interface {
	OpenURL(context.Context, string) error
}

type SpeechRendererPort interface {
	RenderSpeech(context.Context, []byte, string) error
}

type EventSink interface {
	Emit(context.Context, Event) error
}

type Event struct {
	SchemaVersion string `json:"schema_version"`
	// EventID is an optional producer-supplied idempotency key. Runtime does
	// not generate a sequence from it: the durable outbox owns per-run
	// ordering. Hosts may leave it empty and let their repository assign an
	// id. When present, the id is carried through unchanged so a retried
	// callback can resolve to the canonical durable event.
	EventID string `json:"event_id,omitempty"`
	Type    string `json:"type"`
	Scope   Scope  `json:"scope"`
	RunID   string `json:"run_id,omitempty"`
	// OccurredAt is the producer observation time. It is advisory only; the
	// durable sink normalizes it to UTC and supplies a timestamp when omitted.
	OccurredAt time.Time      `json:"occurred_at,omitempty"`
	Sequence   uint64         `json:"sequence,omitempty"`
	Payload    map[string]any `json:"payload,omitempty"`
}

const MaxEventIDBytes = 256

// NormalizeEventEnvelope applies the transport-neutral event contract before
// a GUI memory sink or a headless durable sink accepts the event. It does not
// assign Sequence: ordering belongs to the sink/repository so Runtime cannot
// race lifecycle events such as run.started for the same cursor.
func NormalizeEventEnvelope(event Event) (Event, error) {
	event.SchemaVersion = strings.TrimSpace(event.SchemaVersion)
	if event.SchemaVersion == "" {
		event.SchemaVersion = ContractVersion
	}
	event.EventID = strings.TrimSpace(event.EventID)
	if len([]byte(event.EventID)) > MaxEventIDBytes {
		return Event{}, fmt.Errorf("agent runtime event id exceeds %d bytes", MaxEventIDBytes)
	}
	if strings.ContainsAny(event.EventID, "\r\n") {
		return Event{}, fmt.Errorf("agent runtime event id contains a line break")
	}
	event.Type = strings.TrimSpace(event.Type)
	if event.Type == "" {
		return Event{}, fmt.Errorf("agent runtime event type is required")
	}
	if strings.ContainsAny(event.Type, "\r\n") {
		return Event{}, fmt.Errorf("agent runtime event type contains a line break")
	}
	event.RunID = strings.TrimSpace(event.RunID)
	event.Scope.RunID = strings.TrimSpace(event.Scope.RunID)
	switch {
	case event.RunID == "":
		event.RunID = event.Scope.RunID
	case event.Scope.RunID == "":
		event.Scope.RunID = event.RunID
	case event.RunID != event.Scope.RunID:
		return Event{}, fmt.Errorf("agent runtime event run id does not match scope")
	}
	if !event.OccurredAt.IsZero() {
		event.OccurredAt = event.OccurredAt.UTC()
	}
	if event.Payload != nil {
		if _, err := json.Marshal(event.Payload); err != nil {
			return Event{}, fmt.Errorf("agent runtime event payload: %w", err)
		}
		event.Payload = cloneEventPayload(event.Payload)
	}
	return event, nil
}

func cloneEventPayload(payload map[string]any) map[string]any {
	if payload == nil {
		return nil
	}
	out := make(map[string]any, len(payload))
	for key, value := range payload {
		out[key] = cloneEventPayloadValue(value)
	}
	return out
}

func cloneEventPayloadValue(value any) any {
	switch current := value.(type) {
	case map[string]any:
		return cloneEventPayload(current)
	case []any:
		out := make([]any, len(current))
		for i, item := range current {
			out[i] = cloneEventPayloadValue(item)
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(current))
		for key, item := range current {
			out[key] = item
		}
		return out
	case []string:
		return append([]string(nil), current...)
	case []map[string]any:
		out := make([]map[string]any, len(current))
		for i, item := range current {
			out[i] = cloneEventPayload(item)
		}
		return out
	default:
		return value
	}
}

type ModuleDescriptor struct {
	ModuleID string `json:"module_id"`
	Version  string `json:"version"`
	// HeadlessSupport must be true for a module to participate in a headless
	// turn. GUI-only modules should set it to false and declare the concrete
	// required host capabilities below.
	HeadlessSupport          bool     `json:"headless_support"`
	RequiredHostCapabilities []string `json:"required_host_capabilities,omitempty"`
}

// HeadlessHostCapabilities is the explicit no-UI fallback used by srv when a
// host adapter does not expose desktop/audio side effects.
type HeadlessHostCapabilities struct {
	Capabilities map[string]bool
}

func (h HeadlessHostCapabilities) Profile() CapabilityProfile {
	capabilities := make(map[string]bool, len(h.Capabilities))
	for key, value := range h.Capabilities {
		capabilities[key] = value
	}
	return CapabilityProfile{Name: "headless", Headless: true, Capabilities: capabilities}
}

func (HeadlessHostCapabilities) DesktopCapture() (DesktopCapturePort, bool) {
	return nil, false
}
func (HeadlessHostCapabilities) DocumentLauncher() (DocumentLauncherPort, bool) {
	return nil, false
}
func (HeadlessHostCapabilities) URLLauncher() (URLLauncherPort, bool) { return nil, false }
func (HeadlessHostCapabilities) SpeechRenderer() (SpeechRendererPort, bool) {
	return nil, false
}
