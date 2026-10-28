package guiapp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
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
			"description": "Query the task-scoped capability catalog. The query text is explanatory and does not pick tools; user-declared capabilities such as ssh can join the current scope. bash, read_file, and write_file are baseline workspace tools when listed.",
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
	{"generate_pdf", "Render current facts as a PDF and deliver it.", "document.generate.file"},
	{"office", "Write a spreadsheet (.xlsx) or presentation (.pptx) into the workspace.", tool.CapabilityDocumentWriteOffice},
	{"bash", "Run one local command in the bound workspace.", tool.CapabilityShellExecuteLocal},
	{"delegate_task", "Delegate one self-contained subtask to the coding agent.", tool.CapabilityAgentDelegateSubtask},
	{"send_file", "Deliver the produced file to the current channel.", "artifact.deliver.current_channel"},
	{"read_file", "Read a local file.", tool.CapabilityFSReadLocal},
	{"write_file", "Create or modify a local file.", tool.CapabilityFSWriteLocal},
	{"delete_file", "Remove one file inside the bound workspace.", tool.CapabilityFSDeleteLocal},
	{"edit_file", "Edit a local file.", tool.CapabilityFSWriteLocal},
	{"list_directory", "List a local directory.", tool.CapabilityFSReadLocal},
	{"search_files", "Search local files.", tool.CapabilityFSReadLocal},
	{"download_file", "Download a remote resource into a local artifact.", tool.CapabilityArtifactAcquireRemote},
	{"open", "Open a file, application, or URL with the system handler.", "system.open"},
	{"screenshot", "Capture the desktop screenshot and deliver it.", "visual.capture.desktop"},
	{"current_datetime", "Read the current date and time.", "information.current_time"},
	{"git_status", "Inspect repository status and diffs.", "coding.repository.inspect"},
	{"git_commit", "Commit and push repository changes.", "coding.repository.mutate"},
	{"build_verify", "Run a reviewed build, test, or lint task. A LaTeX directory is compiled by the host TeX engine.", "coding.build.verify"},
	{"browser", "Drive a web browser session.", "computer.browser"},
	{"ssh", "Run a command on a remote host over SSH.", "remote.ssh"},
	{"memory_recall", "Recall agent memory.", tool.CapabilityMemoryRecallAgent},
	{"memory", "Read or update agent memory.", tool.CapabilityMemoryManageAgent},
	{"knowledge_search", "Search the local knowledge base.", tool.CapabilityKnowledgeReadLocal},
	{"knowledge_save_text", "Save text into the knowledge base.", tool.CapabilityKnowledgeIngestLocal},
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
	{"mailbox_query", "Read a configured mailbox. Does not send.", tool.CapabilityMessageReadMailbox},
	{"mailbox_send", "Send, reply, or forward through a configured mailbox after confirmation.", tool.CapabilityMessageSendMailbox},
	{"mailbox_configure", "Save a mailbox from the connection details the user provided. Does not send.", tool.CapabilityMessageConfigureMailbox},
	{"mailbox_delete", "Delete mail in a configured mailbox after confirmation.", tool.CapabilityMessageDeleteMailbox},
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
	"memory_recall":     tool.CapabilityMemoryRecallAgent,
	"build_verify":      tool.CapabilityBuildVerifyLocal,
	"send_im_text":      tool.CapabilityMessageSendIM,
	"mailbox_send":      tool.CapabilityMessageSendMailbox,
	"mailbox_configure": tool.CapabilityMessageConfigureMailbox,
	"mailbox_delete":    tool.CapabilityMessageDeleteMailbox,
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

// semanticToolsSearchListedDirectivePrefix is the last line of a result that
// already contains a callable name. runSemanticToolsSearch uses it to tell a
// focused answer apart from a repeat.
const semanticToolsSearchListedDirectivePrefix = "立即调用已在当前工具面的名字："

// The closing line is the page's one action, and it is the same fact as the
// surface decision. Petition eligibility stays on the row. A closing line
// that invites a petition, or that tells the model to call every grant, is
// what gets quoted (production 2026-10-19: "再用可请愿的工具生成 PDF" while
// web_search was the listed step). Baselines such as bash are grants too, so
// "call every listed name" is not a next step.
const semanticToolsSearchListedCloser = "没有未列出的计划步骤。"
const semanticToolsSearchPlannedIdleCloser = "已计划的名字要等更早的步骤完成。"
const semanticToolsSearchDeferredCloser = "已计划的名字不要直接调用。"

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
	explicitIdentity := scopeID != "" && catalogDigest != ""
	// A caller that copied only one field of the previous result header is
	// still bound to this surface: the live governed surface supplies the
	// missing half and every supplied value is verified below. Rejecting the
	// partial pair stranded a production model after a petition (2026-10-27:
	// scope_id alone and catalog_digest alone both returned
	// tools_search_scope_identity_required, and the turn retried the lookup
	// past its budget trying to re-read the surface it had just been given).
	hostBoundPartial := false
	if (scopeID == "") != (catalogDigest == "") && cb != nil && cb.semanticSurface != nil {
		if scopeID == "" {
			scopeID = semanticSurfaceScopeID(cb.semanticSurface)
		}
		if catalogDigest == "" {
			catalogDigest = strings.TrimSpace(cb.semanticSurface.plan.CatalogDigest)
		}
		hostBoundPartial = true
	}
	// A governed execution entry supplies the identity automatically for
	// legacy clients that still send only query. This keeps compatibility with
	// old model schemas without leaving a production discovery call detached
	// from the immutable surface.
	if cb != nil && cb.semanticSurface != nil && scopeID == "" && catalogDigest == "" {
		scopeID = semanticSurfaceScopeID(cb.semanticSurface)
		catalogDigest = strings.TrimSpace(cb.semanticSurface.plan.CatalogDigest)
	}
	// Derived, not trusted: an unbound legacy call receives the live identity
	// here, and every supplied value is compared with that identity below.
	// Computing this after binding is what lets an empty-query directory read
	// be accepted on a fully bound surface.
	identityProvided := scopeID != "" && catalogDigest != ""
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
	// first page. An explicitly bound directory request (both fields supplied,
	// one field plus the host-bound half, or a continuation carrying a page
	// token) may omit it because the immutable scope already determines the
	// result set.
	pageToken, pageTokenPresent, err := semanticToolsSearchStringArg(args, "page_token")
	if err != nil {
		return "[system rejected] tools_search_arguments_invalid"
	}
	if query == "" && !explicitIdentity && !hostBoundPartial && strings.TrimSpace(pageToken) == "" {
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
	semanticToolsSearchMaybeExpandScope(cb, query)
	// Keep the unfiltered directory. A needs filter may hide mailbox_send,
	// and the confirmation line has to survive that page: the tail is what
	// a shortened result keeps.
	catalog := semanticToolsSearchBuiltinFirst(semanticToolsSearchCatalog(cb))
	entries := catalog
	filterNote := ""
	var requestedNames map[string]bool
	if len(needs) > 0 {
		// Models pass tool names ("web_search") in needs. Those are not
		// capability ids. A filter that matches nothing must not erase the
		// directory: production 2026-09-22 崇州天气 heard "no capability in
		// this task scope" and could not find web_search.
		resolved := resolveSearchNeedTokens(needs, entries)
		// Tool names select that row only. Capability ids select every row of
		// that capability. Name matching keeps a row when its display id and
		// the petition id differ (production 2026-09-27 dropped
		// knowledge_save_text beside web_fetch).
		requestedNames = searchNeedNames(needs, entries)
		capabilityOnly := capabilityNeedsExcludingNames(needs, requestedNames, entries)
		filtered := make([]semanticToolsSearchEntry, 0, len(entries))
		for _, entry := range entries {
			if requestedNames[entry.name] || capabilityOnly[entry.capability] {
				filtered = append(filtered, entry)
			}
		}
		if len(filtered) == 0 {
			filterNote = "needs filter matched nothing; showing the task directory\n"
			needs = map[tool.CapabilityID]bool{}
			requestedNames = nil
		} else {
			entries = filtered
			needs = resolved
		}
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
	// The surface decision is a property of the plan, not of alphabetical
	// position and not of the query. It is written before the directory so a
	// later byte budget cannot hide a planned name behind tools that sort
	// earlier. Production 2026-10-19: generate_pdf was planned behind
	// web_search, the alphabetical dump was cut at database_query, and the
	// surviving tail was the petition sermon.
	statusCache := map[string]string{}
	statusOf := func(name string) string {
		if status, ok := statusCache[name]; ok {
			return status
		}
		status := semanticToolsSearchStatus(cb, name)
		statusCache[name] = status
		return status
	}
	// A lookup that already names a listed tool ends with that directive.
	// The plan's own "现在调用" / "先调用" would be a second imperative, which
	// is how a save lookup was sent after a different listed tool.
	deferCallNow := len(semanticToolsSearchListedNeeds(catalog, requestedNames, statusOf)) > 0
	decision := semanticToolsSearchSurfaceDecision(cb, catalog, statusOf, deferCallNow)
	decisionID := semanticToolsSearchDecisionIDForCollection(scopeID, catalogDigest, needs, pageStart, collectionDigest)
	var header strings.Builder
	fmt.Fprintf(&header, "tools_search results for %q:\n", query)
	fmt.Fprintf(&header, "scope_id=%s catalog_digest=%s decision_id=%s\n", scopeID, catalogDigest, decisionID)
	if filterNote != "" {
		header.WriteString(filterNote)
	}
	if decision.text != "" {
		header.WriteString(decision.text)
	}
	if len(entries) == 0 {
		header.WriteString("(no capability in this task scope)\n")
	}
	rows := make([]string, len(entries))
	// A planned step already names the action. "直接调用一次" on every other
	// row is a second action, and it is the sentence the model quotes
	// (production 2026-10-19). The gate status stays [可请愿]. The call verb
	// stays only when nothing is planned, which is when that call is the action
	// (production 2026-08-27).
	softenPetition := decision.planned
	for i, entry := range entries {
		rows[i] = semanticToolsSearchRenderRow(entry, statusOf(entry.name), softenPetition)
	}
	// The continuation token and the action line are known only after the
	// page is chosen. FitEnd is an estimate; the assembled page is what the
	// model sees, so rows are dropped until it fits and then added back
	// while the inline budget still holds.
	waiting := semanticToolsSearchConfirmationWaiting(catalog, statusOf)
	assemble := func(end int) string {
		return semanticToolsSearchAssemblePage(header.String(), rows, entries, pageStart, end, len(entries), scopeID, catalogDigest, needs, collectionDigest, requestedNames, waiting, statusOf, decision)
	}
	pageEnd := pageStart
	if pageStart < len(rows) {
		pageEnd = semanticToolsSearchFitEnd(header.String(), rows, pageStart, agent.ToolsSearchMaxToolResult)
	}
	rendered := assemble(pageEnd)
	for len(rendered) > agent.ToolsSearchMaxToolResult && pageEnd > pageStart {
		pageEnd--
		rendered = assemble(pageEnd)
	}
	for pageEnd < len(rows) {
		next := assemble(pageEnd + 1)
		if len(next) > agent.ToolsSearchMaxToolResult {
			break
		}
		pageEnd++
		rendered = next
	}
	return rendered
}

func semanticToolsSearchAssemblePage(header string, rows []string, entries []semanticToolsSearchEntry, pageStart, pageEnd, entryCount int, scopeID, catalogDigest string, needs map[tool.CapabilityID]bool, collectionDigest string, requestedNames map[string]bool, waiting []string, statusOf func(string) string, decision semanticToolsSearchDecision) string {
	if pageEnd < pageStart {
		pageEnd = pageStart
	}
	if pageEnd > len(rows) {
		pageEnd = len(rows)
	}
	var out strings.Builder
	out.WriteString(header)
	for _, row := range rows[pageStart:pageEnd] {
		out.WriteString(row)
		out.WriteByte('\n')
	}
	// A page that could not take a single row must not hand back its own
	// offset. That token replays the same header forever.
	if pageEnd > pageStart && pageEnd < entryCount {
		nextToken := strconv.Itoa(pageEnd)
		if scopeID != "" && catalogDigest != "" {
			nextToken = semanticToolsSearchPageToken(scopeID, catalogDigest, needs, pageEnd, collectionDigest)
		}
		fmt.Fprintf(&out, "next_page_token=%s\n", nextToken)
	}
	// The action line is last. A listed name the caller already asked for is
	// the only imperative on that page (production 2026-09-27: the petition
	// sermon sent the model after web_fetch instead of the authorized save).
	// Otherwise the line repeats the surface decision, so a model that reads
	// the tail follows the same call the head named. Row status still says
	// which other names are petitionable.
	page := entries[pageStart:pageEnd]
	if listed := semanticToolsSearchListedNeeds(page, requestedNames, statusOf); len(listed) > 0 {
		fmt.Fprintf(&out, "%s%s。不要再查询这些名字。\n", semanticToolsSearchListedDirectivePrefix, strings.Join(listed, ", "))
	} else if line := semanticToolsSearchClosingLine(decision); line != "" {
		out.WriteString(line)
		out.WriteByte('\n')
	} else {
		out.WriteString("只有标记「可请愿」的未列出名字才可以直接调用一次请愿（每轮每类限一次）。「此名请愿未通过」只拒绝这一个名字，不关掉同类其它可请愿名字。bash、read_file、write_file 是保底工具，已列出即可反复调用。用户已声明的能力（如 ssh）会扩进当前范围；查询文字本身不会挑选工具。")
	}
	if len(waiting) > 0 {
		fmt.Fprintf(&out, "\n等待用户确认：%s。不要再次调用，也不要用其它工具完成该动作。\n", strings.Join(waiting, "、"))
	}
	return out.String()
}

// semanticToolsSearchBuiltinFirst keeps every stable inventory name ahead of
// dynamic adapters. Dynamic descriptions sort before generate_pdf and used to
// push the planned row off the first page.
func semanticToolsSearchBuiltinFirst(entries []semanticToolsSearchEntry) []semanticToolsSearchEntry {
	if len(entries) == 0 {
		return entries
	}
	builtin := make(map[string]bool, len(semanticToolsSearchInventory))
	for _, entry := range semanticToolsSearchInventory {
		builtin[entry.name] = true
	}
	head := make([]semanticToolsSearchEntry, 0, len(entries))
	tail := make([]semanticToolsSearchEntry, 0)
	for _, entry := range entries {
		if builtin[entry.name] {
			head = append(head, entry)
		} else {
			tail = append(tail, entry)
		}
	}
	return append(head, tail...)
}

func semanticToolsSearchRenderRow(entry semanticToolsSearchEntry, status string, softenPetition bool) string {
	if softenPetition && status == "[可请愿：直接调用一次]" {
		status = "[可请愿]"
	}
	return fmt.Sprintf("- %s — %s %s", entry.name, semanticToolsSearchOneLine(entry.summary), status)
}

// semanticToolsSearchOneLine keeps a directory row an index line. Full tool
// schemas belong on the granted tool, not in the catalog dump.
func semanticToolsSearchOneLine(summary string) string {
	summary = strings.Join(strings.Fields(summary), " ")
	const maxRunes = 48
	if utf8.RuneCountInString(summary) <= maxRunes {
		return summary
	}
	runes := []rune(summary)
	return string(runes[:maxRunes-1]) + "…"
}

// semanticToolsSearchFitEnd estimates the exclusive end of a page so the
// header and those rows fit in budget, leaving room for the continuation
// token and the action line. The caller checks the assembled page: this
// estimate can both overflow and leave unused budget.
func semanticToolsSearchFitEnd(prefix string, rows []string, start, budget int) int {
	if start >= len(rows) {
		return start
	}
	if budget <= 0 {
		budget = agent.ToolsSearchMaxToolResult
	}
	const reserve = 512
	room := budget - len(prefix) - reserve
	end := start
	used := 0
	maxEnd := start + 256
	if maxEnd > len(rows) {
		maxEnd = len(rows)
	}
	for end < maxEnd {
		need := len(rows[end]) + 1
		if end > start && used+need > room {
			break
		}
		used += need
		end++
	}
	if end == start {
		end = start + 1
	}
	return end
}

// semanticToolsSearchDecisionBudget keeps the surface decision on the first
// lines. A decision that names every grant pushes the planned clause out of
// the header the projection preserves.
const semanticToolsSearchDecisionBudget = 1500

// semanticToolsSearchDecision is the plan fact written at the head of every
// page and repeated, in shorter form, as the page's last action.
type semanticToolsSearchDecision struct {
	text       string
	callNow    []string
	nextAppear []string
	planned    bool
	listed     bool
	deferred   bool
}

// semanticToolsSearchSurfaceDecision names the predecessor the model can call
// now and the tools already planned but not listed. It does not read the
// query. "现在调用" is only the callNow set from predecessorNamesFor — the
// same walk a petition denial uses. Baseline grants such as bash stay
// directory rows unless they are that predecessor. deferCallNow is set when
// this result already names a listed tool: that directive is the imperative.
// The planned clause then states the successor as a fact and does not say
// "先调用", which would be a second imperative. A partial page still has a
// continuation token, so the decision does not forbid another directory read.
func semanticToolsSearchSurfaceDecision(cb *sharedAgentLoopCallbacks, entries []semanticToolsSearchEntry, statusOf func(string) string, deferCallNow bool) semanticToolsSearchDecision {
	if cb == nil || cb.semanticSurface == nil || statusOf == nil {
		return semanticToolsSearchDecision{}
	}
	listed := make([]string, 0, 4)
	planned := make([]semanticToolsSearchPlanned, 0, 4)
	seen := map[string]bool{}
	for _, entry := range entries {
		name := strings.TrimSpace(entry.name)
		if name == "" || name == semanticToolsSearchName || seen[name] {
			continue
		}
		status := statusOf(name)
		switch {
		case status == "[已在当前工具面]":
			seen[name] = true
			listed = append(listed, name)
		case strings.Contains(status, "已列入本轮计划"):
			seen[name] = true
			planned = append(planned, semanticToolsSearchPlanned{
				entry: entry,
				names: cb.predecessorNamesFor(entry.capability),
				wait:  cb.plannedSuccessorWait(entry.capability),
			})
		}
	}
	if len(listed) == 0 && len(planned) == 0 {
		return semanticToolsSearchDecision{}
	}
	if len(planned) == 0 {
		if deferCallNow {
			return semanticToolsSearchDecision{}
		}
		text, _ := semanticToolsSearchFitDecision(listed, nil, semanticToolsSearchListedDecisionText)
		return semanticToolsSearchDecision{text: text, listed: true}
	}
	callNow := semanticToolsSearchCallNow(planned)
	if deferCallNow {
		callNow = nil
	}
	clauses := make([]string, len(planned))
	for i, item := range planned {
		clauses[i] = semanticToolsSearchPlannedClause(item.entry, item.names, item.wait, deferCallNow)
	}
	text, fitted := semanticToolsSearchFitDecision(callNow, clauses, semanticToolsSearchPlannedDecisionText)
	if deferCallNow {
		fitted = nil
	}
	return semanticToolsSearchDecision{
		text:       text,
		callNow:    fitted,
		nextAppear: semanticToolsSearchNextAppear(planned),
		planned:    true,
		deferred:   deferCallNow,
	}
}

type semanticToolsSearchPlanned struct {
	entry semanticToolsSearchEntry
	names predecessorNames
	wait  successorWait
}

// semanticToolsSearchClosingLine is the action a page ends with when the
// caller did not already name a listed tool. callNow is that action. A held
// plan names the tool that appears on the next request and does not end on
// 请愿. A directory with no unlisted plan step does not order every grant.
func semanticToolsSearchClosingLine(decision semanticToolsSearchDecision) string {
	switch {
	case len(decision.callNow) > 0:
		return "现在调用 " + strings.Join(decision.callNow, "、") + "。"
	case decision.deferred && decision.planned:
		return semanticToolsSearchDeferredCloser
	case len(decision.nextAppear) > 0:
		return "下一请求出现：" + strings.Join(decision.nextAppear, "、") + "。现在不要调用。"
	case decision.planned:
		return semanticToolsSearchPlannedIdleCloser
	case decision.listed:
		return semanticToolsSearchListedCloser
	default:
		return ""
	}
}

// semanticToolsSearchNextAppear names the planned tool the next request can
// list. A held generate_pdf is that tool. send_file's nextRequest is
// generate_pdf, so delivery is not named here while generate is still unlisted.
func semanticToolsSearchNextAppear(planned []semanticToolsSearchPlanned) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(planned))
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		out = append(out, name)
	}
	for _, item := range planned {
		switch item.wait {
		case successorWaitHeld:
			add(item.entry.name)
		case successorWaitImmediate:
			if len(item.names.callNow) == 0 {
				add(item.entry.name)
			}
		case successorWaitLater:
			if len(item.names.callNow) == 0 {
				for _, name := range item.names.nextRequest {
					add(name)
				}
			}
		}
	}
	sort.Strings(out)
	return out
}

func semanticToolsSearchCallNow(planned []semanticToolsSearchPlanned) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(planned))
	for _, item := range planned {
		for _, name := range item.names.callNow {
			name = strings.TrimSpace(name)
			if name == "" || seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// semanticToolsSearchFitDecision drops extra planned clauses first, then
// extra head names, until the line fits. Builtin-first catalog order already
// put the stable planned tool ahead of dynamic ones, so the kept clause is
// that tool. The last remaining clause and the last remaining head name stay
// even when the line is still long: an empty decision would hide the plan.
func semanticToolsSearchFitDecision(head, tail []string, render func(head, tail []string) string) (string, []string) {
	text := render(head, tail)
	for len(text) > semanticToolsSearchDecisionBudget && len(tail) > 1 {
		tail = tail[:len(tail)-1]
		text = render(head, tail)
	}
	fitted := head
	for len(text) > semanticToolsSearchDecisionBudget && len(fitted) > 1 {
		fitted = fitted[:len(fitted)-1]
		text = render(fitted, tail)
	}
	return text, fitted
}

func semanticToolsSearchListedDecisionText(listed, _ []string) string {
	return "本轮决定：已在当前工具面：" + strings.Join(listed, "、") + "。\n"
}

func semanticToolsSearchPlannedDecisionText(callNow, clauses []string) string {
	var b strings.Builder
	b.WriteString("本轮决定：")
	if len(callNow) > 0 {
		fmt.Fprintf(&b, "现在调用 %s。", strings.Join(callNow, "、"))
	}
	fmt.Fprintf(&b, "已计划但本请求尚未列出：%s。不要直接调用这些名字，不要请愿，不要告诉用户工具缺失。\n", strings.Join(clauses, "；"))
	return b.String()
}

// semanticToolsSearchPlannedClause uses the same wait the petition denial
// uses. A listed predecessor does not mean this name appears next:
// send_file's callNow is web_search, and the name that appears after that
// search is generate_pdf. Promising send_file itself is what the denial
// wording was written to avoid. factsOnly drops "先调用": the page already
// ends by naming a different listed tool, so the clause stays a fact.
func semanticToolsSearchPlannedClause(entry semanticToolsSearchEntry, names predecessorNames, wait successorWait, factsOnly bool) string {
	switch wait {
	case successorWaitHeld:
		return fmt.Sprintf("%s（本响应的其它调用结束后出现在下一请求）", entry.name)
	case successorWaitImmediate:
		if len(names.callNow) > 0 {
			if factsOnly {
				return fmt.Sprintf("%s（%s 成功后，%s 出现在下一请求）", entry.name, strings.Join(names.callNow, "、"), entry.name)
			}
			return fmt.Sprintf("%s（先调用 %s，成功后它出现在下一请求）", entry.name, strings.Join(names.callNow, "、"))
		}
		return entry.name + "（当前步骤成功后出现在下一请求）"
	case successorWaitLater:
		return semanticToolsSearchLaterClause(entry.name, names, factsOnly)
	default:
		return entry.name + "（前置步骤完成后出现在列表）"
	}
}

func semanticToolsSearchLaterClause(name string, names predecessorNames, factsOnly bool) string {
	callNow := strings.Join(names.callNow, "、")
	nextRequest := strings.Join(names.nextRequest, "、")
	switch {
	case len(names.callNow) > 0 && len(names.nextRequest) > 0:
		if factsOnly {
			return fmt.Sprintf("%s（%s 成功后 %s 出现在下一请求。%s 要等它自己出现在列表后再调用）", name, callNow, nextRequest, name)
		}
		return fmt.Sprintf("%s（先调用 %s；成功后 %s 出现在下一请求。%s 要等它自己出现在列表后再调用）", name, callNow, nextRequest, name)
	case len(names.callNow) > 0:
		if factsOnly {
			return fmt.Sprintf("%s（%s 完成后，%s 要等它自己出现在列表后再调用）", name, callNow, name)
		}
		return fmt.Sprintf("%s（先调用 %s。%s 要等它自己出现在列表后再调用）", name, callNow, name)
	case len(names.nextRequest) > 0:
		return fmt.Sprintf("%s（%s 出现在下一请求后再调用；%s 要等它自己出现在列表）", name, nextRequest, name)
	default:
		return name + "（更早的计划步骤完成后才出现在列表）"
	}
}

func semanticToolsSearchConfirmationWaiting(entries []semanticToolsSearchEntry, statusOf func(string) string) []string {
	if statusOf == nil {
		return nil
	}
	names := make([]string, 0, 1)
	seen := map[string]bool{}
	for _, entry := range entries {
		name := strings.TrimSpace(entry.name)
		if name == "" || seen[name] {
			continue
		}
		if statusOf(name) == "[等待用户确认：不要再次调用]" {
			seen[name] = true
			names = append(names, name)
		}
	}
	return names
}

// semanticToolsSearchMaybeExpandScope widens the live plan when the current
// user text already declared a missing petitionable capability — for example
// the user said "用ssh访问" after a search-shaped turn. Discovery still does
// not select tools from query wording alone.
func semanticToolsSearchMaybeExpandScope(cb *sharedAgentLoopCallbacks, query string) {
	if cb == nil || cb.semanticSurface == nil {
		return
	}
	if !semanticUserTextDeclaresSSH(cb.userText) && !semanticUserTextDeclaresSSH(query) {
		return
	}
	if !semanticUserTextDeclaresSSH(cb.userText) && !semanticUserTextSupportsRemote(cb.userText) {
		return
	}
	if semanticPlanHasCapability(cb.semanticSurface.plan, tool.CapabilityShellExecuteRemoteHost) {
		return
	}
	if _, ok := cb.semanticSurface.grants["ssh"]; ok {
		return
	}
	// Without either mode published the capability has no provider and
	// petitioning would spend the effectful budget on a guaranteed expansion
	// failure. Connect mode (session-less) is petitionable: its provider can
	// resolve the expansion and open a session on the re-issued call.
	if cb.handler == nil || (!semanticTrustedSSHPublished(cb.handler, cb.semanticPrincipalID()) && !semanticTrustedSSHConnectPublished(cb.handler, cb.semanticPrincipalID())) {
		return
	}
	cb.PetitionToolCall("ssh")
}

func semanticUserTextDeclaresSSH(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, needle := range []string{"用ssh", "ssh访问", "ssh登录", "登录服务器"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	return hasASCIIToken(lower, "ssh")
}

func semanticUserTextSupportsRemote(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	for _, needle := range []string{"服务器", "远程", "主机"} {
		if strings.Contains(lower, needle) {
			return true
		}
	}
	for _, needle := range []string{"server", "remote"} {
		if hasASCIIToken(lower, needle) {
			return true
		}
	}
	return false
}

func hasASCIIToken(lower, token string) bool {
	token = strings.TrimSpace(token)
	if token == "" || !strings.Contains(lower, token) {
		return false
	}
	start := 0
	for {
		idx := strings.Index(lower[start:], token)
		if idx < 0 {
			return false
		}
		idx += start
		beforeOK := idx == 0 || !isASCIILetterOrDigit(lower[idx-1])
		after := idx + len(token)
		afterOK := after >= len(lower) || !isASCIILetterOrDigit(lower[after])
		if beforeOK && afterOK {
			return true
		}
		start = idx + len(token)
	}
}

func isASCIILetterOrDigit(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
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

// capabilityNeedsExcludingNames returns need tokens that are capability
// ids. Tokens that are tool names are omitted so they cannot drag in every
// alias of that capability.
func capabilityNeedsExcludingNames(needs map[tool.CapabilityID]bool, names map[string]bool, entries []semanticToolsSearchEntry) map[tool.CapabilityID]bool {
	if len(needs) == 0 {
		return nil
	}
	known := make(map[tool.CapabilityID]bool, len(entries)+len(semanticPetitionableCapabilities))
	for _, entry := range entries {
		if entry.capability != "" {
			known[entry.capability] = true
		}
	}
	for _, capability := range semanticPetitionableCapabilities {
		if capability != "" {
			known[capability] = true
		}
	}
	out := make(map[tool.CapabilityID]bool)
	for token := range needs {
		if names[string(token)] {
			continue
		}
		if known[token] {
			out[token] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// searchNeedNames returns need tokens that are exact tool names. Capability
// id drift must not hide a name the caller asked for by spelling.
func searchNeedNames(needs map[tool.CapabilityID]bool, entries []semanticToolsSearchEntry) map[string]bool {
	if len(needs) == 0 {
		return nil
	}
	known := make(map[string]bool, len(entries)+len(semanticPetitionableCapabilities))
	for _, entry := range entries {
		if entry.name != "" {
			known[entry.name] = true
		}
	}
	for name := range semanticPetitionableCapabilities {
		known[name] = true
	}
	out := make(map[string]bool)
	for token := range needs {
		if known[string(token)] {
			out[string(token)] = true
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// semanticToolsSearchListedNeeds names requested tools that are already on
// this turn's surface. The directive is part of the tool result, so a model
// that asked for a listed name is told to call it instead of searching again.
func semanticToolsSearchListedNeeds(page []semanticToolsSearchEntry, requested map[string]bool, statusOf func(string) string) []string {
	if len(requested) == 0 || statusOf == nil {
		return nil
	}
	names := make([]string, 0, len(requested))
	for _, entry := range page {
		if !requested[entry.name] {
			continue
		}
		if statusOf(entry.name) != "[已在当前工具面]" {
			continue
		}
		names = append(names, entry.name)
	}
	sort.Strings(names)
	return names
}

func resolveSearchNeedTokens(needs map[tool.CapabilityID]bool, entries []semanticToolsSearchEntry) map[tool.CapabilityID]bool {
	known := make(map[tool.CapabilityID]bool, len(entries))
	byName := make(map[string]tool.CapabilityID, len(entries)+len(semanticPetitionableCapabilities))
	for _, entry := range entries {
		if entry.capability != "" {
			known[entry.capability] = true
		}
		if entry.name != "" && entry.capability != "" {
			byName[entry.name] = entry.capability
		}
	}
	for name, capability := range semanticPetitionableCapabilities {
		if capability == "" {
			continue
		}
		known[capability] = true
		byName[name] = capability
	}
	resolved := make(map[tool.CapabilityID]bool, len(needs))
	for token := range needs {
		if known[token] {
			resolved[token] = true
			continue
		}
		if capability, ok := byName[string(token)]; ok {
			resolved[capability] = true
			continue
		}
		resolved[token] = true
	}
	return resolved
}

// parseExactSearchNeeds parses the exact capability filter accepted by the
// discovery contract.  It deliberately accepts both JSON's []interface{}
// representation and []string values used by in-process callers, while
// rejecting every other shape and every non-string element.  The query is a
// display hint only. needs filters by capability id or exact tool name.
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
		name := strings.TrimSpace(entry.name)
		if name == "" {
			continue
		}
		// Display ids in the static table drifted from the petition map
		// (remote.ssh versus shell.execute.remote_host). The row the filter
		// sees uses the id a needs token actually resolves to.
		if capability, ok := semanticToolsSearchNameCapability(name); ok && capability != "" {
			entry.capability = capability
		}
		byName[name] = entry
	}
	if cb != nil && cb.semanticSurface != nil {
		surface := cb.semanticSurface
		selectionByID := make(map[string]tool.PlannedSelection, len(surface.plan.Selections))
		for _, selection := range surface.plan.Selections {
			selectionByID[selection.ID] = selection
			// Stable host adapters have a deterministic model name even before
			// their first grant. Installed MCP uses server__tool once its
			// grant is in the table below; other dynamic adapters stay on
			// the grant token.
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
	mainAgent := cb == nil || !loopKeepsDrivingTools(cb.loopCtx)
	for _, entry := range byName {
		if mainAgent && (desktopbot.MainDropsTool(entry.name) || strings.Contains(strings.ToLower(entry.summary), "drive the local desktop")) {
			continue
		}
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
	}
	// The sidecar grant is a second tools/call while the managed write is
	// still awaiting its receipt. Do not describe that name as callable.
	if cb.managedInstalledMCPSpentThisRequest(name) && surface != nil {
		if grant, ok := surface.retiredGrants[name]; ok {
			if awaiting, reason := semanticRetiredAwaitingReceipt(surface, grant.SelectionID); awaiting {
				if mailboxDraftAwaitingConfirmation(reason) {
					return "[等待用户确认：不要再次调用]"
				}
				return "[结果未定：不要再次调用]"
			}
		}
	}
	// An installed server stays callable under server__tool. A retired
	// managed token with the same spelling must not hide the live sidecar.
	if cb.discoveredMCPSurface != nil {
		if _, ok := cb.discoveredMCPSurface.grants[name]; ok {
			return "[已在当前工具面]"
		}
	}
	if surface != nil {
		if grant, ok := surface.retiredGrants[name]; ok {
			if strings.HasPrefix(strings.TrimSpace(grant.AdapterName), "installed-mcp:") {
				return "[已安装的 MCP 工具，继续用这个名字调用]"
			}
			if awaiting, reason := semanticRetiredAwaitingReceipt(surface, grant.SelectionID); awaiting {
				if mailboxDraftAwaitingConfirmation(reason) {
					return "[等待用户确认：不要再次调用]"
				}
				return "[结果未定：不要再次调用]"
			}
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
		// Timing lives in the surface decision. One sentence cannot say
		// "appears automatically" for both generate_pdf (next request after
		// web_search) and send_file (only after generate_pdf is itself listed).
		return "[已列入本轮计划：不要直接调用，也不要请愿]"
	}
	if _, ok := semanticPetitionableCapabilities[name]; ok {
		if _, denied := cb.semanticPetitionDenied[name]; denied {
			return "[此名请愿未通过，不要重试；同类其它「可请愿」名字仍可调用]"
		}
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
		adapter := strings.TrimSpace(selection.AdapterName)
		if adapter == "" {
			continue
		}
		// Match the name the model will actually see. Capability equality is
		// still wrong: list_directory and read_file share a capability, and
		// only the selected adapter is planned. The host adapter id
		// host_document_generate_file is the same plan row as generate_pdf.
		if adapter == name || tool.SemanticModelFunctionName(adapter) == name {
			return true
		}
	}
	return false
}
