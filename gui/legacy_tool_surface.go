package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

// legacyToolSurface is the short-lived admission record for an unmanaged
// legacy turn. It deliberately contains only the definitions selected for the
// current request: it is neither a task-continuity store nor a grant, and it
// must never be rebuilt from a previous round's tools.
//
// The semantic path has durable selection/grant admission. This adapter gives
// the remaining name-router path the corresponding minimum invariant while it
// is migrated: a model may execute only a function that was in the exact
// replacement surface sent with its request.
type legacyToolSurface struct {
	names             map[string]struct{}
	contracts         map[string]legacyToolArgumentContract
	provisionRequired map[string]bool
	// clientToolNames is an explicit, request-scoped dispatch binding. It is
	// deliberately not inferred from LoopContext.ClientTools: a host tool wins
	// a name collision and must never be redirected to a client declaration.
	clientToolNames map[string]struct{}
	// requireLiveProvision is set only for a surface rendered from a
	// LegacyAdapterPlan. Older compatibility snapshots remain protected by the
	// exact request definitions and parameter contract, but cannot claim they
	// are provision-backed selections until their family is migrated.
	requireLiveProvision bool
	epochs               *legacyToolSurfaceEpoch
}

// legacyToolArgumentContract is extracted from the exact definition snapshot
// sent to one legacy model request. It is intentionally request-local: a
// registry schema update or a later replacement must not broaden a response
// that was produced against an older surface.
//
// This is the P0 migration floor, not semantic ParameterAuthorization. It
// rejects object fields the model was not shown, closing the common legacy
// escape where a visible generic function receives a hidden provider/target or
// command-like argument.
type legacyToolArgumentContract struct {
	allowedFields map[string]struct{}
	enforce       bool
}

type legacyToolSurfaceEpoch struct {
	mu      sync.RWMutex
	version uint64
	active  string
}

var legacyToolSurfaceEpochFallback atomic.Uint64

func newLegacyToolSurface(definitions []map[string]interface{}) legacyToolSurface {
	return newLegacyToolSurfaceWithOptions(definitions, false, nil)
}

func newLegacyToolSurfaceFromPlan(definitions []map[string]interface{}) legacyToolSurface {
	return newLegacyToolSurfaceWithOptions(definitions, true, nil)
}

// newLegacyToolSurfaceWithClientTools receives only names whose definitions
// were appended by clientToolDefinitionsForAgent after host replacement. The
// caller must not pass all LoopContext.ClientTools, because that would turn a
// hidden or colliding declaration into an executable binding.
func newLegacyToolSurfaceWithClientTools(definitions []map[string]interface{}, clientToolNames []string) legacyToolSurface {
	return newLegacyToolSurfaceWithOptions(definitions, false, clientToolNames)
}

// replaceDefinitions returns a new immutable authorization snapshot while
// retaining this logical request stream's epoch holder. RunLoop creates the
// epoch immediately before it asks a compatibility callback to render the
// request surface. Replacing the snapshot must therefore not also replace the
// epoch holder: doing so would make the response from the just-rendered request
// look stale merely because its exact definitions were rebound.
//
// The caller must use the returned value as the complete replacement surface;
// it must never merge definitions from the predecessor snapshot.
func (s legacyToolSurface) replaceDefinitions(definitions []map[string]interface{}, clientToolNames []string) legacyToolSurface {
	replacement := newLegacyToolSurfaceWithClientTools(definitions, clientToolNames)
	if s.epochs != nil {
		replacement.epochs = s.epochs
	}
	return replacement
}

func newLegacyToolSurfaceWithOptions(definitions []map[string]interface{}, requireLiveProvision bool, clientToolNames []string) legacyToolSurface {
	if definitions == nil {
		return legacyToolSurface{}
	}
	names := make(map[string]struct{}, len(definitions))
	contracts := make(map[string]legacyToolArgumentContract, len(definitions))
	provisionRequired := make(map[string]bool, len(definitions))
	for _, definition := range definitions {
		if name := strings.TrimSpace(extractToolName(definition)); name != "" {
			names[name] = struct{}{}
			contracts[name] = legacyToolArgumentContractForDefinition(definition)
			provisionRequired[name] = requireLiveProvision || legacyDefinitionHasLiveProvision(definition)
		}
	}
	clients := make(map[string]struct{}, len(clientToolNames))
	for _, name := range clientToolNames {
		name = strings.TrimSpace(name)
		if _, exposed := names[name]; name != "" && exposed {
			clients[name] = struct{}{}
		}
	}
	return legacyToolSurface{names: names, contracts: contracts, provisionRequired: provisionRequired, clientToolNames: clients, requireLiveProvision: requireLiveProvision, epochs: &legacyToolSurfaceEpoch{}}
}

// Allows is fail-closed when a surface exists. A nil surface represents a
// direct/test host invocation that did not issue a model tool surface; it is
// intentionally not used by the production agent-loop call path.
func (s legacyToolSurface) Allows(name string) bool {
	if s.names == nil {
		return true
	}
	_, ok := s.names[strings.TrimSpace(name)]
	return ok
}

func (s legacyToolSurface) HasSnapshot() bool {
	return s.names != nil
}

func (s legacyToolSurface) Clone() legacyToolSurface {
	if s.names == nil {
		return legacyToolSurface{}
	}
	clone := make(map[string]struct{}, len(s.names))
	for name := range s.names {
		clone[name] = struct{}{}
	}
	contracts := make(map[string]legacyToolArgumentContract, len(s.contracts))
	for name, contract := range s.contracts {
		copied := legacyToolArgumentContract{enforce: contract.enforce}
		if contract.allowedFields != nil {
			copied.allowedFields = make(map[string]struct{}, len(contract.allowedFields))
			for field := range contract.allowedFields {
				copied.allowedFields[field] = struct{}{}
			}
		}
		contracts[name] = copied
	}
	provisionRequired := make(map[string]bool, len(s.provisionRequired))
	for name, required := range s.provisionRequired {
		provisionRequired[name] = required
	}
	clientToolNames := make(map[string]struct{}, len(s.clientToolNames))
	for name := range s.clientToolNames {
		clientToolNames[name] = struct{}{}
	}
	return legacyToolSurface{names: clone, contracts: contracts, provisionRequired: provisionRequired, clientToolNames: clientToolNames, requireLiveProvision: s.requireLiveProvision, epochs: &legacyToolSurfaceEpoch{}}
}

func legacyToolArgumentContractForDefinition(definition map[string]interface{}) legacyToolArgumentContract {
	function, _ := definition["function"].(map[string]interface{})
	parameters, _ := function["parameters"].(map[string]interface{})
	if len(parameters) == 0 {
		return legacyToolArgumentContract{}
	}
	properties, _ := parameters["properties"].(map[string]interface{})
	if len(properties) == 0 {
		return legacyToolArgumentContract{}
	}
	allowed := make(map[string]struct{}, len(properties))
	for name := range properties {
		if name = strings.TrimSpace(name); name != "" {
			allowed[name] = struct{}{}
		}
	}
	return legacyToolArgumentContract{allowedFields: allowed, enforce: len(allowed) > 0}
}

// AllowsArguments validates the top-level argument envelope against the exact
// function schema sent to the model. Nested field validation stays with the
// handler/schema validator; this boundary only prevents hidden top-level
// switches from crossing an old legacy adapter contract.
func (s legacyToolSurface) AllowsArguments(name, rawJSON string) error {
	if !s.HasSnapshot() {
		return nil
	}
	contract, ok := s.contracts[strings.TrimSpace(name)]
	if !ok || !contract.enforce {
		return nil
	}
	var args map[string]json.RawMessage
	decoder := json.NewDecoder(strings.NewReader(rawJSON))
	decoder.UseNumber()
	if err := decoder.Decode(&args); err != nil {
		// Normal argument decoding reports malformed payloads elsewhere. This
		// check is only authoritative once a JSON object was decoded.
		return nil
	}
	for field := range args {
		if _, allowed := contract.allowedFields[field]; !allowed {
			return fmt.Errorf("field %q is outside the request tool contract", field)
		}
	}
	return nil
}

// beginEpoch binds one model request to this exact legacy replacement surface.
// It must be called only at the model-request boundary; a later replacement
// gets a new epoch holder and therefore invalidates every prior response.
func (s legacyToolSurface) beginEpoch() string {
	if !s.HasSnapshot() || s.epochs == nil {
		return ""
	}
	s.epochs.mu.Lock()
	defer s.epochs.mu.Unlock()
	s.epochs.version++
	s.epochs.active = "legacy-surface:" + legacyToolSurfaceEpochNonce(s.epochs.version)
	return s.epochs.active
}

func (s legacyToolSurface) epochIsCurrent(epoch string) bool {
	if !s.HasSnapshot() || s.epochs == nil || strings.TrimSpace(epoch) == "" {
		return false
	}
	s.epochs.mu.RLock()
	defer s.epochs.mu.RUnlock()
	return epoch == s.epochs.active
}

func legacyToolSurfaceEpochNonce(version uint64) string {
	var random [16]byte
	if _, err := rand.Read(random[:]); err == nil {
		return hex.EncodeToString(random[:])
	}
	// crypto/rand failure is exceptional, but surface correlation must remain
	// fail-closed and unique within this process rather than silently disabling.
	return "fallback-" + strconv.FormatUint(version, 10) + "-" + strconv.FormatUint(legacyToolSurfaceEpochFallback.Add(1), 10)
}

// legacyToolSurfaceDeniedText is deliberately stable: model responses that
// belong to an earlier replacement surface must receive a clear retry/replan
// signal without learning about hidden registry capabilities.
func legacyToolSurfaceDeniedText(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "(unknown)"
	}
	return "[system rejected] " + name + " is not available on this request's tool surface. Request a replan or use an exposed tool."
}

func legacyAdapterCatalogDeniedText(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "(unknown)"
	}
	return "[system rejected] catalog_incomplete: " + name + " has no live reviewed legacy adapter provision. Request a replan."
}

func legacyToolArgumentDeniedText(name string, err error) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = "(unknown)"
	}
	return "[system rejected] parameter_contract_denied: " + name + " " + err.Error() + ". Request a replan."
}

// call_mcp_tool is intentionally retained as a host-controlled transport for
// explicit AgentView submissions and the CodingSubAgent's matched-set path.
// It is never an authority a legacy model may use to select server_id and
// tool_name. Managed MCP calls bind those values outside model arguments.
func isLegacyModelMCPGateway(name string) bool {
	return strings.TrimSpace(name) == "call_mcp_tool"
}

func legacyModelMCPGatewayDeniedText() string {
	return "[system rejected] dynamic_mcp_requires_managed_surface: legacy model calls cannot select MCP server_id or tool_name. Request a managed semantic replan."
}

// legacyAdapterCatalogAllows is separate from the name snapshot: a surface
// tells us what the model saw, while the provision catalog tells us whether a
// legacy name may still stand for a reviewed capability at all.
func legacyAdapterCatalogAllows(name string) bool {
	return !coretool.LegacyAdapterCatalogIncomplete(strings.TrimSpace(name), time.Now().UTC())
}

func (s legacyToolSurface) AllowsLiveProvision(name string) bool {
	name = strings.TrimSpace(name)
	if !s.requireLiveProvision && !s.provisionRequired[name] {
		return legacyAdapterCatalogAllows(name)
	}
	_, ok := coretool.LegacyAdapterProvisionForTool(name, time.Now().UTC())
	return ok
}

func (s legacyToolSurface) RequiresLiveProvision() bool {
	return s.requireLiveProvision
}

// IsClientTool reports an exposed request-local binding, rather than merely a
// name advertised by the connected client. It prevents client declarations
// from overriding host dispatch or surviving a replacement surface.
func (s legacyToolSurface) IsClientTool(name string) bool {
	_, ok := s.clientToolNames[strings.TrimSpace(name)]
	return ok
}
