package guiapp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticToolsSearchName is the read-only discovery meta-tool rendered on
// every governed surface. The closed surface assumes the model can guess the
// exact stable spelling of an unlisted tool; weaker models cannot (the
// 2026-08-26 PPT turn hallucinated "generate_ppt" and stalled). Discovery
// turns that guess into a lookup: the model asks in natural language, gets
// back exact names and whether each is listed or petitionable. Discovery is
// not authorization — grants still come only from the plan or a petition.
const semanticToolsSearchName = "tools_search"

func semanticToolsSearchDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticToolsSearchName,
			"description": "Query the task-scoped capability catalog. Read-only; the query text is explanatory and never selects tools.",
			"parameters": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query":          map[string]interface{}{"type": "string"},
					"scope_id":       map[string]interface{}{"type": "string"},
					"catalog_digest": map[string]interface{}{"type": "string"},
					"needs":          map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"page_token":     map[string]interface{}{"type": "string"},
				},
				"required":             []string{},
				"additionalProperties": false,
			},
		},
	}
}

// semanticToolsSearchInventory is display metadata only. It is deliberately
// not a retrieval index: no user text is compared with these entries. The
// live scope/plan is the authority for executable names; this stable metadata
// keeps unavailable and petitionable capabilities explainable to the model
// instead of making them disappear when a provider is temporarily absent.
type semanticToolsSearchEntry struct {
	name       string
	summary    string
	capability tool.CapabilityID
}

var semanticToolsSearchInventory = []semanticToolsSearchEntry{
	{"web_search", "Search the public web.", "information.search.web"},
	{"web_fetch", "Fetch the content of one web page.", tool.CapabilityInformationFetchWeb},
	{"generate_pdf", "Render Markdown content into a PDF and deliver it.", "document.generate.file"},
	{"office", "Write a spreadsheet (.xlsx) or presentation (.pptx) into the workspace.", tool.CapabilityDocumentWriteOffice},
	{"bash", "Run one local command in the bound workspace.", tool.CapabilityShellExecuteLocal},
	{"delegate_task", "Delegate one self-contained subtask to the coding agent.", tool.CapabilityAgentDelegateSubtask},
	{"send_file", "Deliver the produced file to the current channel.", "artifact.deliver.current_channel"},
	{"read_file", "Read a local file.", tool.CapabilityFSReadLocal},
	{"write_file", "Create or modify a local file.", tool.CapabilityFSWriteLocal},
	{"edit_file", "Edit a local file.", tool.CapabilityFSWriteLocal},
	{"list_directory", "List a local directory.", tool.CapabilityFSReadLocal},
	{"search_files", "Search local files.", tool.CapabilityFSReadLocal},
	{"download_file", "Download a remote resource into a local artifact.", tool.CapabilityArtifactAcquireRemote},
	{"open", "Open a file, application, or URL with the system handler.", "system.open"},
	{"screenshot", "Capture the desktop screenshot and deliver it.", "visual.capture.desktop"},
	{"current_datetime", "Read the current date and time.", "information.current_time"},
	{"git_status", "Inspect repository status and diffs.", "coding.repository.inspect"},
	{"git_commit", "Commit and push repository changes.", "coding.repository.mutate"},
	{"build_verify", "Run a reviewed build, test, or lint task.", "coding.build.verify"},
	{"browser", "Drive a web browser session.", "computer.browser"},
	{"computer_use", "Observe or drive the local desktop.", "computer.desktop"},
	{"ssh", "Run a command on a remote host over SSH.", "remote.ssh"},
	{"memory_recall", "Recall agent memory.", tool.CapabilityMemoryRecallAgent},
	{"memory", "Read or update agent memory.", tool.CapabilityMemoryManageAgent},
	{"knowledge_search", "Search the local knowledge base.", tool.CapabilityKnowledgeReadLocal},
	{"knowledge_save_text", "Save text into the knowledge base.", "knowledge.write.local"},
	{"knowledge_maintain", "Administer knowledge-base sources and maintenance.", "knowledge.maintain"},
	{"manage_schedule", "Administer local schedules and reminders.", "schedule.manage"},
	{"task", "Track local tasks.", "task.manage"},
	{"goal", "Manage long-running goals.", "goal.manage"},
	{"record_audio", "Record microphone audio.", "audio.record"},
	{"asr", "Transcribe speech audio.", "audio.transcribe"},
	{"session_search", "Search session history and audit records.", "session.search"},
	{"manage_template", "Manage session templates.", "template.manage"},
	{"manage_config", "Read or update assistant configuration.", "config.manage"},
	{"send_to_im", "Deliver a file to a specified IM target.", "artifact.deliver.specified_target"},
	{"send_im_text", "Send a text message to a specified IM target.", tool.CapabilityMessageSendIM},
	{"list_sessions", "List sessions.", "session.list"},
	{"mis_data", "Query or administer business data.", "business.mis"},
	{"database", "Connect and query a configured database.", "database.connect"},
	{"database_query", "Read-only SQL against a configured database profile.", "database.query"},
}

// semanticToolsSearchPlanCapabilities maps display names onto the capability
// identity used by the planner. It is a compatibility map, never a text
// index: a name is selected only when it is present in the current scope/plan
// or when the caller explicitly asks for its exact capability id in needs.
var semanticToolsSearchPlanCapabilities = map[string]tool.CapabilityID{
	"web_search":    "information.search.web",
	"web_fetch":     tool.CapabilityInformationFetchWeb,
	"bash":          tool.CapabilityShellExecuteLocal,
	"delegate_task": tool.CapabilityAgentDelegateSubtask,
	"download_file": tool.CapabilityArtifactAcquireRemote,
	"office":        tool.CapabilityDocumentWriteOffice,
	"generate_pdf":  "document.generate.file",
	"send_file":     "artifact.deliver.current_channel",
	// Legacy aliases for the trusted file adapters (never rendered managed).
	"list_directory": tool.CapabilityFSReadLocal,
	"search_files":   tool.CapabilityFSReadLocal,
	"edit_file":      tool.CapabilityFSWriteLocal,
	// Capability-backed but never rule-routed on a managed chat surface.
	"memory_recall": tool.CapabilityMemoryRecallAgent,
	"build_verify":  tool.CapabilityBuildVerifyLocal,
	"send_im_text":  tool.CapabilityMessageSendIM,
}

// semanticToolsSearchNameCapability resolves the capability a managed surface
// would plan or petition for an inventory name, from whichever map backs it.
func semanticToolsSearchNameCapability(name string) (tool.CapabilityID, bool) {
	if capability, ok := semanticPetitionableCapabilities[name]; ok {
		return capability, true
	}
	capability, ok := semanticToolsSearchPlanCapabilities[name]
	return capability, ok
}

// semanticToolsSearchRun executes one deterministic discovery query against
// the host inventory. Status is derived from the live surface and the turn's
// petition budget: listed names are callable now, petitionable names are
// reachable by calling them once, planned names unlock as earlier steps
// complete, and everything else must be stated as unavailable — an ambiguous
// status invites the model to call names the petition gate will reject
// (production 2026-08-27: "按计划路由提供" read as "call it", eight wasted
// iterations on office while the turn rode the coding surface).
func semanticToolsSearchRun(cb *sharedAgentLoopCallbacks, argsJSON string) string {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args == nil {
		return "[system rejected] tools_search_arguments_invalid"
	}
	if err := validateSemanticToolsSearchArgumentKeys(args); err != nil {
		return "[system rejected] tools_search_arguments_invalid"
	}
	query, _, err := semanticToolsSearchStringArg(args, "query")
	if err != nil {
		return "[system rejected] tools_search_arguments_invalid"
	}
	scopeID, _, err := semanticToolsSearchStringArg(args, "scope_id")
	if err != nil {
		return "[system rejected] tools_search_arguments_invalid"
	}
	catalogDigest, _, err := semanticToolsSearchStringArg(args, "catalog_digest")
	if err != nil {
		return "[system rejected] tools_search_arguments_invalid"
	}
	identityProvided := scopeID != "" || catalogDigest != ""
	explicitIdentity := scopeID != "" && catalogDigest != ""
	if (scopeID == "") != (catalogDigest == "") {
		return "[system rejected] tools_search_scope_identity_required"
	}
	// A governed execution entry supplies the identity automatically for
	// legacy clients that still send only query. This keeps compatibility with
	// old model schemas without leaving a production discovery call detached
	// from the immutable surface.
	if cb != nil && cb.semanticSurface != nil && scopeID == "" && catalogDigest == "" {
		scopeID = semanticSurfaceScopeID(cb.semanticSurface)
		catalogDigest = strings.TrimSpace(cb.semanticSurface.plan.CatalogDigest)
	}
	// Recompute this after host-side binding.  The previous value described the
	// untrusted request before the surface supplied its identity, so a valid
	// empty-query directory page was rejected as if it were an unbound legacy
	// lookup.
	identityProvided = scopeID != "" && catalogDigest != ""
	// A caller may never use an arbitrary scope/catalog pair to enumerate the
	// process-wide inventory.  There is no external catalog provider on this
	// execution path; identity is meaningful only when it is verified against
	// the live governed surface below.
	if !identityProvided || cb == nil || cb.semanticSurface == nil {
		return "[system rejected] tools_search_scope_unavailable"
	}
	// Compatibility clients still send query. A scope-bound page request may
	// omit it because discovery is a directory read, not text retrieval. Apply
	// this check after deriving the governed identity so an empty-query page
	// continuation is accepted only on a bound surface.
	// Query-only compatibility calls still need a non-empty explanation on the
	// first page. An explicitly bound directory request (or a continuation
	// carrying a page token) may omit it because the immutable scope already
	// determines the result set.
	pageToken, pageTokenPresent, err := semanticToolsSearchStringArg(args, "page_token")
	if err != nil {
		return "[system rejected] tools_search_arguments_invalid"
	}
	if query == "" && !explicitIdentity && strings.TrimSpace(pageToken) == "" {
		return "[system rejected] tools_search_query_required"
	}
	if cb != nil && cb.semanticSurface != nil && scopeID != "" {
		expectedScope := semanticSurfaceScopeID(cb.semanticSurface)
		expectedDigest := strings.TrimSpace(cb.semanticSurface.plan.CatalogDigest)
		if expectedScope == "" || expectedDigest == "" {
			return "[system rejected] tools_search_scope_unavailable"
		}
		if scopeID != expectedScope || catalogDigest != expectedDigest {
			return "[system rejected] tools_search_scope_mismatch"
		}
	}
	needsRaw, needsPresent := args["needs"]
	if !needsPresent {
		needsRaw = nil
	}
	if needsPresent && needsRaw == nil {
		return "[system rejected] tools_search_needs_invalid"
	}
	needs, needsErr := parseExactSearchNeeds(needsRaw)
	if needsErr != nil {
		// A malformed capability filter must never degrade into an unfiltered
		// directory.  Treating it as empty would silently broaden the
		// task-scoped surface and makes a caller's invalid request look valid.
		return "[system rejected] tools_search_needs_invalid"
	}
	entries := semanticToolsSearchCatalog(cb)
	if len(needs) > 0 {
		filtered := make([]semanticToolsSearchEntry, 0, len(entries))
		for _, entry := range entries {
			if needs[entry.capability] {
				filtered = append(filtered, entry)
			}
		}
		entries = filtered
	}
	collectionDigest := semanticToolsSearchCollectionDigest(entries)
	// A continuation token is bound to the immutable scope, catalog and exact
	// capability filter.  A bare offset is unsafe: it can be copied from one
	// task (or catalog generation) into another and silently skip or expose a
	// different directory.  Keep numeric offsets only for truly unbound legacy
	// callers; governed surfaces always have an identity and therefore require
	// the bound form.
	var pageArg interface{}
	if pageTokenPresent {
		pageArg = pageToken
	}
	pageStart, err := searchPageStartForScope(pageArg, scopeID, catalogDigest, needs, collectionDigest)
	if err != nil {
		return "[system rejected] tools_search_page_token_invalid"
	}
	if pageStart > len(entries) {
		return "[system rejected] tools_search_page_token_invalid"
	}
	// Keep the first page large enough to contain the complete built-in
	// directory (including dynamically published adapters). Pagination remains
	// available for unusually large catalogs, but a normal discovery call must
	// never hide a stable entry merely because it sorted after an arbitrary
	// top-K boundary.
	const pageSize = 256
	pageEnd := pageStart + pageSize
	if pageEnd > len(entries) {
		pageEnd = len(entries)
	}
	decisionID := semanticToolsSearchDecisionIDForCollection(scopeID, catalogDigest, needs, pageStart, collectionDigest)
	var out strings.Builder
	fmt.Fprintf(&out, "tools_search results for %q:\n", query)
	fmt.Fprintf(&out, "scope_id=%s catalog_digest=%s decision_id=%s\n", scopeID, catalogDigest, decisionID)
	if len(entries) == 0 {
		out.WriteString("(no capability in this task scope)\n")
	}
	for _, entry := range entries[pageStart:pageEnd] {
		fmt.Fprintf(&out, "- %s — %s %s\n", entry.name, entry.summary, semanticToolsSearchStatus(cb, entry.name))
	}
	if pageEnd < len(entries) {
		nextToken := strconv.Itoa(pageEnd)
		if scopeID != "" && catalogDigest != "" {
			nextToken = semanticToolsSearchPageToken(scopeID, catalogDigest, needs, pageEnd, collectionDigest)
		}
		fmt.Fprintf(&out, "next_page_token=%s\n", nextToken)
	}
	out.WriteString("只有标记「可请愿」的未列出名字才可以直接调用一次请愿（每轮每类限一次）；标记「已列入本轮计划/已用尽/不可用」的名字调用必被拒绝，不要尝试。查询文字不会改变本轮工具集合。")
	return out.String()
}

// semanticToolsSearchStringArg validates optional string arguments instead of
// silently converting malformed JSON values to an empty string.  Silent
// conversion is dangerous here: a malformed scope/catalog value could be
// treated as an omitted compatibility field and broaden a directory read.
func semanticToolsSearchStringArg(args map[string]interface{}, key string) (string, bool, error) {
	raw, present := args[key]
	if !present {
		return "", false, nil
	}
	value, ok := raw.(string)
	if !ok {
		return "", true, fmt.Errorf("%s must be a string", key)
	}
	return strings.TrimSpace(value), true, nil
}

func validateSemanticToolsSearchArgumentKeys(args map[string]interface{}) error {
	for key := range args {
		switch key {
		case "query", "scope_id", "catalog_digest", "needs", "page_token":
		default:
			return fmt.Errorf("unknown tools_search argument %q", key)
		}
	}
	return nil
}

func exactSearchNeeds(raw interface{}) map[tool.CapabilityID]bool {
	needs, _ := parseExactSearchNeeds(raw)
	return needs
}

// parseExactSearchNeeds parses the exact capability filter accepted by the
// discovery contract.  It deliberately accepts both JSON's []interface{}
// representation and []string values used by in-process callers, while
// rejecting every other shape and every non-string element.  The query is a
// display hint only; capability IDs are the sole filter authority.
func parseExactSearchNeeds(raw interface{}) (map[tool.CapabilityID]bool, error) {
	needs := make(map[tool.CapabilityID]bool)
	if raw == nil {
		return needs, nil
	}
	var values []interface{}
	switch typed := raw.(type) {
	case []interface{}:
		values = typed
	case []string:
		values = make([]interface{}, len(typed))
		for index, value := range typed {
			values[index] = value
		}
	default:
		return nil, fmt.Errorf("needs must be an array of strings")
	}
	for _, value := range values {
		name, ok := value.(string)
		if !ok || strings.TrimSpace(name) == "" {
			return nil, fmt.Errorf("needs must contain non-empty strings")
		}
		needs[tool.CapabilityID(strings.TrimSpace(name))] = true
	}
	return needs, nil
}

func searchPageStart(raw interface{}) (int, error) {
	if raw == nil {
		return 0, nil
	}
	token, ok := raw.(string)
	if !ok {
		return 0, fmt.Errorf("page token must be a string")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, nil
	}
	start, err := strconv.Atoi(token)
	if err != nil || start < 0 {
		return 0, fmt.Errorf("invalid page token")
	}
	return start, nil
}

// semanticToolsSearchPageToken is an opaque, deterministic continuation
// token.  The digest covers every input that determines the ordered result
// set except query text, so changing wording cannot alter or reuse a page.
func semanticToolsSearchPageToken(scopeID, catalogDigest string, needs map[tool.CapabilityID]bool, pageStart int, collectionDigest ...string) string {
	return "v1:" + strconv.Itoa(pageStart) + ":" + semanticToolsSearchPageTokenDigest(scopeID, catalogDigest, needs, pageStart, collectionDigest...)
}

func semanticToolsSearchPageTokenDigest(scopeID, catalogDigest string, needs map[tool.CapabilityID]bool, pageStart int, collectionDigest ...string) string {
	decision := semanticToolsSearchDecisionIDForCollection(scopeID, catalogDigest, needs, pageStart, collectionDigest...)
	return strings.TrimPrefix(decision, "decision:")
}

// searchPageStartForScope accepts an offset only when no immutable identity is
// available.  Once a caller is bound to a surface, a continuation must carry
// the scope/catalog/filter digest generated above.  This closes cross-scope,
// stale-catalog and changed-filter pagination confusion.
func searchPageStartForScope(raw interface{}, scopeID, catalogDigest string, needs map[tool.CapabilityID]bool, collectionDigest ...string) (int, error) {
	if raw == nil {
		return 0, nil
	}
	token, ok := raw.(string)
	if !ok {
		return 0, fmt.Errorf("page token must be a string")
	}
	token = strings.TrimSpace(token)
	if token == "" {
		return 0, nil
	}
	if strings.TrimSpace(scopeID) == "" && strings.TrimSpace(catalogDigest) == "" {
		return searchPageStart(token)
	}
	parts := strings.Split(token, ":")
	if len(parts) != 3 || parts[0] != "v1" {
		return 0, fmt.Errorf("unbound page token")
	}
	start, err := strconv.Atoi(parts[1])
	if err != nil || start < 0 {
		return 0, fmt.Errorf("invalid page token offset")
	}
	expected := semanticToolsSearchPageTokenDigest(strings.TrimSpace(scopeID), strings.TrimSpace(catalogDigest), needs, start, collectionDigest...)
	if parts[2] != expected {
		return 0, fmt.Errorf("page token scope mismatch")
	}
	return start, nil
}

// semanticToolsSearchCatalog returns a deterministic directory. Static rows
// remain visible as honest unavailable/petitionable metadata, while the live
// plan and trusted definitions are merged so a new provider cannot disappear
// before a display inventory refresh.
// semanticToolsSearchCatalog returns a deterministic directory. Static rows
// remain visible as honest unavailable/petitionable metadata. Live rows use
// the exact model-visible name bound by the current surface: stable adapters
// resolve through SemanticModelFunctionName, while dynamic adapters are added
// only from their materialized opaque grant token. An internal dynamic
// AdapterName without a rendered grant is never advertised as if it were
// callable, which keeps discovery and the actual surface identical.
func semanticToolsSearchCatalog(cb *sharedAgentLoopCallbacks) []semanticToolsSearchEntry {
	byName := make(map[string]semanticToolsSearchEntry, len(semanticToolsSearchInventory))
	for _, entry := range semanticToolsSearchInventory {
		if strings.TrimSpace(entry.name) != "" {
			byName[entry.name] = entry
		}
	}
	if cb != nil && cb.semanticSurface != nil {
		surface := cb.semanticSurface
		selectionByID := make(map[string]tool.PlannedSelection, len(surface.plan.Selections))
		for _, selection := range surface.plan.Selections {
			selectionByID[selection.ID] = selection
			// Stable host adapters have a deterministic model name even before
			// their first grant is materialized. Dynamic adapters deliberately do
			// not: their model name is the opaque token in surface.grants.
			if name := strings.TrimSpace(tool.SemanticModelFunctionName(selection.AdapterName)); name != "" {
				semanticToolsSearchUpsertSelection(byName, surface, name, selection)
			}
		}
		// Grants are the authoritative mapping for dynamic model names. Include
		// retired names as well so tools_search can explain why a previously
		// rendered opaque name must not be retried.
		for name, grant := range surface.grants {
			selection, ok := selectionByID[grant.SelectionID]
			if !ok {
				continue
			}
			semanticToolsSearchUpsertSelection(byName, surface, strings.TrimSpace(name), selection)
		}
		for name, grant := range surface.retiredGrants {
			selection, ok := selectionByID[grant.SelectionID]
			if !ok {
				continue
			}
			semanticToolsSearchUpsertSelection(byName, surface, strings.TrimSpace(name), selection)
		}
		// Schemas are keyed by trusted AdapterName, not necessarily by the
		// model-visible name. They enrich stable inventory rows only; a dynamic
		// schema without a corresponding grant is intentionally omitted.
		for adapter, definition := range surface.schemas {
			adapter = strings.TrimSpace(adapter)
			name := strings.TrimSpace(tool.SemanticModelFunctionName(adapter))
			if name == "" {
				// Dynamic catalog definitions intentionally carry the fixed
				// source name "dynamic_provider"; their actual model name is
				// the opaque grant key handled above. A test/local static schema
				// may instead be self-named and can remain display metadata.
				if strings.HasPrefix(adapter, "dynamic_mcp_") || strings.HasPrefix(adapter, "dynamic_skill_") || strings.TrimSpace(tool.ExtractToolName(definition)) != adapter {
					continue
				}
				name = adapter
			}
			entry := byName[name]
			entry.name = name
			if description := strings.TrimSpace(tool.ExtractToolDescription(definition)); description != "" {
				entry.summary = description
			}
			if entry.summary == "" {
				entry.summary = "Provider admitted by the current task scope."
			}
			byName[name] = entry
		}
	}
	entries := make([]semanticToolsSearchEntry, 0, len(byName))
	for _, entry := range byName {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].name < entries[j].name })
	return entries
}

func semanticToolsSearchUpsertSelection(byName map[string]semanticToolsSearchEntry, surface *semanticCallSurface, name string, selection tool.PlannedSelection) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	entry := byName[name]
	entry.name = name
	if entry.summary == "" && surface != nil && surface.registry != nil {
		if descriptor, ok := surface.registry.Lookup(selection.FitProof.MatchedCapability); ok {
			entry.summary = strings.TrimSpace(descriptor.Summary)
		}
	}
	if entry.summary == "" {
		entry.summary = "Provider admitted by the current task scope."
	}
	if entry.capability == "" {
		entry.capability = selection.FitProof.MatchedCapability
	}
	byName[name] = entry
}
func semanticToolsSearchDecisionID(scopeID, catalogDigest string, needs map[tool.CapabilityID]bool, pageStart int) string {
	return semanticToolsSearchDecisionIDForCollection(scopeID, catalogDigest, needs, pageStart)
}

func semanticToolsSearchDecisionIDForCollection(scopeID, catalogDigest string, needs map[tool.CapabilityID]bool, pageStart int, collectionDigest ...string) string {
	needNames := make([]string, 0, len(needs))
	for need := range needs {
		needNames = append(needNames, string(need))
	}
	sort.Strings(needNames)
	collection := ""
	if len(collectionDigest) > 0 {
		collection = strings.TrimSpace(collectionDigest[0])
	}
	material := strings.Join([]string{scopeID, catalogDigest, collection, strings.Join(needNames, ","), strconv.Itoa(pageStart)}, "\x00")
	return "decision:" + tool.SchemaDigest([]byte(material))
}

// semanticToolsSearchCollectionDigest freezes the exact ordered directory
// represented by a page token.  CatalogDigest is the publisher identity;
// this second digest protects against a buggy mutable surface that changes its
// definitions without advancing that identity.
func semanticToolsSearchCollectionDigest(entries []semanticToolsSearchEntry) string {
	var material strings.Builder
	for _, entry := range entries {
		material.WriteString(strings.TrimSpace(entry.name))
		material.WriteByte(0)
		material.WriteString(strings.TrimSpace(string(entry.capability)))
		material.WriteByte(0)
		material.WriteString(strings.TrimSpace(entry.summary))
		material.WriteByte(0)
	}
	return tool.SchemaDigest([]byte(material.String()))
}

func semanticSurfaceScopeID(surface *semanticCallSurface) string {
	if surface == nil {
		return ""
	}
	if strings.TrimSpace(surface.scopePlan.ScopeID) != "" {
		return strings.TrimSpace(surface.scopePlan.ScopeID)
	}
	parts := []string{strings.TrimSpace(surface.scope.RootTaskID), strings.TrimSpace(surface.scope.SessionID), strings.TrimSpace(surface.scope.TurnID), strings.TrimSpace(surface.plan.ID), strings.TrimSpace(surface.plan.CatalogDigest)}
	for _, part := range parts {
		if part == "" {
			return ""
		}
	}
	return "semantic-scope:" + tool.SchemaDigest([]byte(strings.Join(parts, "\x00")))
}
func semanticToolsSearchStatus(cb *sharedAgentLoopCallbacks, name string) string {
	if cb == nil {
		return "[本轮不可用：不要调用，用已列出的工具完成]"
	}
	surface := cb.semanticSurface
	if surface != nil {
		if _, ok := surface.grants[name]; ok {
			return "[已在当前工具面]"
		}
		if _, ok := surface.retiredGrants[name]; ok {
			return "[本轮授权已用尽，不要调用]"
		}
	}
	if cb != nil && cb.legacyPetitionAllows(name) {
		return "[已在当前工具面]"
	}
	// Only the exact stable adapter name selected by this plan is "planned".
	// Capability equality alone is unsafe: multiple display aliases can map to
	// the same capability (for example read_file/list_directory). Marking every
	// alias as planned tells the model to wait for a tool that will never be
	// rendered and recreates the disappearing-tool failure in discovery.
	if semanticSurfacePlansTool(surface, name) {
		return "[已列入本轮计划：前置步骤完成后自动出现在列表，不要直接调用]"
	}
	if _, ok := semanticPetitionableCapabilities[name]; ok {
		consumed := cb.semanticPetitionConsumed
		if semanticPetitionIsEffectful(name) {
			consumed = cb.semanticEffectfulPetitionConsumed
		}
		if consumed {
			return "[本轮请愿机会已用完，不要调用]"
		}
		return "[可请愿：直接调用一次]"
	}
	return "[本轮不可用：不要调用，用已列出的工具完成]"
}

func semanticSurfacePlansTool(surface *semanticCallSurface, name string) bool {
	if surface == nil {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, selection := range surface.plan.Selections {
		if strings.TrimSpace(selection.AdapterName) == name {
			return true
		}
	}
	return false
}
