package guiapp

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
	"github.com/RapidAI/CodeClaw/corelib/llm"
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

// semanticToolsSearchNeedsMissNote is the header line of a page whose needs
// filter matched nothing and fell back to the full directory. The repeat
// guard reads it: that page shares every row with an earlier unfiltered
// directory but carries the miss step as its new tail.
const semanticToolsSearchNeedsMissNote = "needs filter matched nothing; showing the task directory\n"

func semanticToolsSearchDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticToolsSearchName,
			"description": "Query the task-scoped capability catalog. The query text does not select tools. The catalog is paged: a cut page still lists the remaining names verbatim — petition those names through needs, or pass page_token to read on. Never declare a capability absent before the whole directory is seen. Pass directory names in needs to load them onto the next request. A name absent from this request's function list cannot be called until that load succeeds. One needs list can name several workspace tools. ssh, browser, delete_file, and record_audio load only when that name is the sole needs entry. computer_use is not loaded onto this request.",
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
	{"read_attachment", "Read the approved document attachment. No path.", "document.read.local"},
	{"write_file", "Create or modify a local file.", tool.CapabilityFSWriteLocal},
	{"delete_file", "Remove one file inside the bound workspace.", tool.CapabilityFSDeleteLocal},
	{"edit_file", "Edit a local file.", tool.CapabilityFSWriteLocal},
	{"list_directory", "List a local directory.", tool.CapabilityFSReadLocal},
	{"search_files", "Search local files.", tool.CapabilityFSReadLocal},
	{"download_file", "Download a remote resource into a local artifact.", tool.CapabilityArtifactAcquireRemote},
	{"open", "Open a file, application, or URL with the system handler.", "system.open"},
	{"screenshot", "Capture the desktop screenshot and deliver it.", "visual.capture.desktop"},
	{semanticTrustedLiveDataVisualAdapter, "Render the current trusted lookup as an image.", "visual.render.live_data"},
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
	{"schedule_dispatch", "Register a due-time delivery for the current trusted destination. Does not send now.", tool.CapabilityScheduleDispatchChannel},
	{"task", "Track local tasks.", "task.manage"},
	{"goal", "Manage long-running goals.", "goal.manage"},
	{"record_audio", "Record microphone audio.", "audio.record"},
	{"tts_render", "Render text as a speech artifact. Does not play or send.", tool.CapabilityAudioRenderSpeech},
	{"tts_local", "Play text as speech on this machine. Does not send.", tool.CapabilityAudioSynthesizeLocal},
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
	// Bind failures belong to the plan this search expands. A later search
	// starts clean, so a document producer added since the last failure can
	// publish the name instead of repeating a closed refusal.
	if cb != nil {
		cb.petitionInputUnbound = nil
		cb.discoveryAdmitUnbound = nil
	}
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
	// and the confirmation line still has to be on that page. It is the
	// last line only when this search named nothing else.
	catalog := semanticToolsSearchBuiltinFirst(semanticToolsSearchCatalog(cb))
	entries := catalog
	filterNote := ""
	var requestedNames map[string]bool
	discoverySurface := cb != nil && semanticSurfaceIsModelDiscovery(cb.semanticSurface)
	if cb != nil {
		// A previous search's guided-skill note is not this page's row.
		cb.discoverySearchBodies = nil
	}
	if discoverySurface {
		// Installed skills and ready MCP tools are petitionable catalog
		// rows. Leaving them out of the result listing made an installed
		// server invisible: the model could not guess the folded
		// server__tool spelling and had no name to put in needs.
		entries = appendDiscoveryCatalogSearchEntries(entries, cb.discoveryCatalogRows())
	}
	var resolveNote string
	var unservedNote string
	var nearMissCandidates []string
	if len(needs) > 0 {
		// needTokens snapshots the petition as sent. needs is rewritten by
		// the resolutions below; the unserved report still owes the caller
		// an answer for every token it asked for.
		needTokens := make([]string, 0, len(needs))
		for token := range needs {
			if raw := strings.TrimSpace(string(token)); raw != "" {
				needTokens = append(needTokens, raw)
			}
		}
		served := map[string]bool{}
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
				// A capability id is not itself a tool name. On a discovery
				// turn the selected row is the name the next request loads.
				if discoverySurface && capabilityOnly[entry.capability] {
					if requestedNames == nil {
						requestedNames = map[string]bool{}
					}
					requestedNames[entry.name] = true
				}
			}
		}
		if len(filtered) == 0 && len(requestedNames) == 0 && discoverySurface {
			// A loose token ("weather", "web search", "search engine") is
			// usually a spelling or vocabulary slip, not a missing
			// capability. Resolving it to the one loadable row it means
			// keeps a wrong guess from arming the unmatched gate, which
			// closes web_search and every fetch/render tool for the rest
			// of the turn (production 2026-11-11: 北京天气 passed
			// needs=["weather"], the miss armed the gate, and the turn
			// finished through search_and_install_skill and a craft_tool
			// urllib scraper while web_search sat in the directory).
			loose := resolveDiscoveryNeedsLoose(needs, entries, cb)
			switch {
			case len(loose.resolved) > 0:
				requestedNames = loose.resolved
				for _, entry := range entries {
					if loose.resolved[entry.name] {
						filtered = append(filtered, entry)
					}
				}
				needs = loose.caps
				resolveNote = "needs 里不匹配目录的名字按意图解析为：" + strings.Join(loose.pairs, "、") + "。\n"
				for token := range loose.served {
					served[token] = true
				}
			case len(loose.candidates) > 0:
				// A token that lands near directory rows is still a
				// vocabulary problem. The page stays a miss but the
				// unmatched gate must not arm: the model may petition an
				// exact row, or take the extension path when none fits.
				nearMissCandidates = loose.candidates
			}
		}
		// Installed skills and MCP tools are catalog rows, not inventory
		// lines. A needs token that spells one is the petition. Leaving it
		// out makes the page a miss and points at search_and_install_skill.
		if discoverySurface {
			have := map[string]bool{}
			for _, entry := range filtered {
				have[entry.name] = true
			}
			for token := range needs {
				name := strings.TrimSpace(string(token))
				if name == "" || have[name] {
					continue
				}
				// A bare tool name or a server:tool spelling resolves to the
				// one installed-MCP row it means. The exact server__tool
				// spelling is not guessable, so a close token that names one
				// row is still that row's petition.
				row, ok := cb.discoveryCatalogResolveName(name)
				if !ok {
					continue
				}
				served[name] = true
				summary := strings.TrimSpace(row.Summary)
				if summary == "" {
					summary = "已安装。"
				}
				filtered = append(filtered, semanticToolsSearchEntry{name: row.Name, summary: summary})
				have[row.Name] = true
				if requestedNames == nil {
					requestedNames = map[string]bool{}
				}
				requestedNames[row.Name] = true
			}
		}
		if len(filtered) == 0 {
			// A petitionable name with no directory row is still a load.
			// computer_use is that name: the main conversation cannot publish
			// it, and treating the token as unknown used to open the catalog
			// miss and close every other tool. Unknown tokens still miss.
			// A loose-resolved name already proved it is loadable or
			// callable; it keeps the row-less slot too, so a resolution is
			// never rewritten back into a miss.
			loadableTokens := discoveryNeedsAreLoadableNames(cb, needs)
			if discoverySurface && (loadableTokens || (resolveNote != "" && len(requestedNames) > 0)) {
				if requestedNames == nil {
					requestedNames = map[string]bool{}
				}
				if resolveNote == "" {
					for token := range needs {
						name := strings.TrimSpace(string(token))
						if name != "" {
							requestedNames[name] = true
						}
					}
				}
			} else {
				filterNote = semanticToolsSearchNeedsMissNote
				needs = map[tool.CapabilityID]bool{}
				requestedNames = nil
			}
		} else {
			entries = filtered
			// A loose resolution already rewrote needs to the matched
			// capabilities; only exact matches take the resolved map.
			if resolveNote == "" {
				needs = resolved
			}
		}
		// A mixed petition answers the matched tokens and would drop the
		// rest in silence: the model then reads a partial page as the full
		// answer. Every token gets a verdict — matched names carry their
		// row, unmatched ones are named here.
		if filterNote == "" {
			var dropped []string
			for _, raw := range needTokens {
				if served[raw] || requestedNames[raw] || capabilityOnly[tool.CapabilityID(raw)] {
					continue
				}
				dropped = append(dropped, raw)
			}
			if len(dropped) > 0 {
				sort.Strings(dropped)
				unservedNote = "needs 里这些名字没有匹配到目录条目，不会装载：" + strings.Join(dropped, "、") + "。\n"
			}
		}
	}
	// A later needs list can name web_search after the catalog already missed.
	// Loading it would make the download the fulfillment. Keep the miss page
	// until the extension path has run, and keep substitute names off even
	// after that.
	if discoverySurface && cb.discoverySubstitutionBlocks(requestedNames) {
		filterNote = semanticToolsSearchNeedsMissNote
		needs = map[tool.CapabilityID]bool{}
		requestedNames = nil
		entries = catalog
		// The blocked names were already resolved for nothing: naming the
		// substitute beside the extension order reads like it may still load.
		resolveNote = ""
		unservedNote = ""
		nearMissCandidates = nil
	}
	collectionDigest := semanticToolsSearchCollectionDigest(entries)
	// A continuation token is bound to the immutable scope, catalog and exact
	// capability filter.  A bare offset is unsafe: it can be copied from one
	// task (or catalog generation) into another and silently skip or expose a
	// different directory.  Keep numeric offsets only for truly unbound legacy
	// callers; governed surfaces always have an identity and therefore require
	// the bound form.
	// Validate before loading. A copied token from the previous directory
	// does not match this needs filter. Loading first and then rejecting
	// grants the tools and tells the model the lookup failed.
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
	// The model names a tool in needs. That name is not on this request's
	// function list, so the provider keeps emitting tools_search. Load the
	// selected rows here; the next request is what can call a function.
	// A guided skill returns its body instead of a function.
	loadedNotes := ""
	unloadedPetition := false
	admitted := false
	// held means a requested name is already in the plan and waiting: the
	// predecessor call is this page's action. exclusiveAlone is set when
	// this search loaded nothing, held nothing, and skipped at least one
	// fail-closed family. That sentence is the page action. The catalog
	// miss stays closed, so a later search that names one of them alone
	// can still load it.
	held := false
	exclusiveAlone := ""
	if filterNote == "" && discoverySurface && len(needs) > 0 && len(requestedNames) > 0 {
		needNames := make([]string, 0, len(requestedNames))
		for name := range requestedNames {
			needNames = append(needNames, name)
		}
		sort.Strings(needNames)
		var exclusiveNote string
		loadedNotes, admitted, exclusiveNote, held = cb.admitDiscoverySearchNeeds(needNames)
		// Names were in the directory, and this call loaded none of them.
		// The clock plan's "现在调用" must not become the answer.
		unloadedPetition = !admitted && cb != nil && cb.discoveryUnmatchedObligation
		if exclusiveNote != "" && (admitted || held) {
			// A workspace name, or the predecessor of a held one, is the
			// action. The skipped family is a fact in the header.
			if loadedNotes != "" && !strings.HasSuffix(loadedNotes, "\n") {
				loadedNotes += "\n"
			}
			loadedNotes += exclusiveNote
		}
		if exclusiveNote != "" && !admitted && !held && !unloadedPetition {
			exclusiveAlone = exclusiveNote
		}
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
	// An unmatched needs token is not a question about the planned successor.
	// "现在调用 office，不要告诉用户工具缺失" answered a picture search with
	// the spreadsheet plan (2026-11-05). The directory stays so a bad token
	// on a real task can still be read. The decision says the name is absent.
	var decision semanticToolsSearchDecision
	if unloadedPetition {
		decision = semanticToolsSearchDecision{
			text:    semanticToolsSearchDiscoveryUnloaded,
			closing: semanticToolsSearchDiscoveryNextStep,
		}
	} else if filterNote != "" {
		switch {
		case len(nearMissCandidates) > 0:
			// A near miss is a vocabulary slip, not a capability gap. The
			// unmatched gate stays disarmed so the model can still petition
			// an exact row; the extension path is named as the fallback.
			decision = semanticToolsSearchNearMissDecision(nearMissCandidates)
		default:
			if discoverySurface {
				cb.noteDiscoveryCatalogMiss()
			}
			decision = semanticToolsSearchUnmatchedDecision(cb)
		}
	} else if exclusiveAlone != "" {
		decision = semanticToolsSearchDecision{text: exclusiveAlone + "\n", closing: exclusiveAlone}
	} else {
		deferCallNow := len(semanticToolsSearchListedNeeds(catalog, requestedNames, statusOf)) > 0
		decision = semanticToolsSearchSurfaceDecision(cb, catalog, statusOf, deferCallNow)
	}
	// A guided skill has no function to call. Its body is this result.
	// It stays in the head. The preview keeps the head of an over-budget
	// page and the tail call, and a suffix cut would drop the procedure.
	// The call, when a function was also loaded, stays the tail.
	if note, only := soleDiscoveryDesktopDoorNote(loadedNotes); only && !unloadedPetition && !admitted && !held && exclusiveAlone == "" {
		// This search asked for desktop control and loaded nothing else.
		// The plan's own 「现在调用」 is a second imperative: a picture search
		// used to finish by calling office. The refusal is the whole action,
		// at the head and as the last line. An already-listed workspace
		// name, a tool the plan is already holding, a skipped exclusive
		// family, or a petition that closed the turn each keep their own
		// action. The refusal stays in the header.
		decision.callNow = nil
		decision.nextAppear = nil
		decision.text = note + "\n"
		decision.closing = note
		loadedNotes = ""
	} else if note, only := discoveryAdmitUnboundAction(cb, loadedNotes); only && !unloadedPetition && !admitted && !held && exclusiveAlone == "" {
		// The names are in the directory. Their templates did not bind a
		// trusted document. Each name's fact stays in the head. The last
		// line is one follow-up for the whole list: a per-name sentence
		// let whichever note sorted last hide the delivery name's producer
		// step, or order that step for a read the producer does not unlock.
		decision.callNow = nil
		decision.nextAppear = nil
		decision.text = note + "\n"
		decision.closing = cb.discoveryUnboundFollowUp(note)
		loadedNotes = ""
	} else if loadedNotes != "" {
		if decision.text != "" && !strings.HasSuffix(decision.text, "\n") {
			decision.text += "\n"
		}
		decision.text += loadedNotes
		if !strings.HasSuffix(decision.text, "\n") {
			decision.text += "\n"
		}
		loadedNotes = ""
	}
	// A miss already carries the skill-search step on its own decision.
	// An empty decision here is a directory whose named tools were just
	// loaded, or one that still lists those rows. The listed-needs line is
	// that page's action. Stamping the miss step onto it told a matched
	// lookup that nothing corresponded and to install a skill.
	decisionID := semanticToolsSearchDecisionIDForCollection(scopeID, catalogDigest, needs, pageStart, collectionDigest)
	var header strings.Builder
	fmt.Fprintf(&header, "tools_search results for %q:\n", query)
	fmt.Fprintf(&header, "scope_id=%s catalog_digest=%s decision_id=%s\n", scopeID, catalogDigest, decisionID)
	if filterNote != "" {
		header.WriteString(filterNote)
	}
	if resolveNote != "" {
		header.WriteString(resolveNote)
	}
	if unservedNote != "" {
		header.WriteString(unservedNote)
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
		rows[i] = semanticToolsSearchRenderRow(entry, semanticToolsSearchRowStatusForAction(statusOf(entry.name), decision), softenPetition)
	}
	// The continuation token and the action line are known only after the
	// page is chosen. FitEnd is an estimate; the assembled page is what the
	// model sees, so rows are dropped until it fits and then added back
	// while the inline budget still holds.
	waiting := semanticToolsSearchConfirmationWaiting(catalog, statusOf)
	assemble := func(end int) string {
		return semanticToolsSearchAssemblePage(header.String(), rows, entries, pageStart, end, len(entries), scopeID, catalogDigest, needs, collectionDigest, query, requestedNames, waiting, statusOf, decision, discoverySurface)
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

func semanticToolsSearchAssemblePage(header string, rows []string, entries []semanticToolsSearchEntry, pageStart, pageEnd, entryCount int, scopeID, catalogDigest string, needs map[tool.CapabilityID]bool, collectionDigest string, query string, requestedNames map[string]bool, waiting []string, statusOf func(string) string, decision semanticToolsSearchDecision, discoverySurface bool) string {
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
		// A cut page must still publish every hidden name. The model only
		// paginates when it knows something is missing; naming the rest is
		// what keeps a later-page write tool from being declared absent
		// (production 2026-11-11: post_work_log_add sat on page 2 while the
		// turn reported 腾云没有写接口 and fell back to the browser).
		out.WriteString(semanticToolsSearchUnshownNamesLine(entries[pageEnd:], query, discoverySurface))
	}
	// The action line is last. A listed name the caller already asked for is
	// the only imperative on that page (production 2026-09-27: the petition
	// sermon sent the model after web_fetch instead of the authorized save).
	// Otherwise the line repeats the surface decision, so a model that reads
	// the tail follows the same call the head named. Row status still says
	// which other names are petitionable.
	// A draft waiting for the user is a fact about the catalog, not that
	// action. The footer is last only when this search named nothing else:
	// leaving it last on every page made the tail follower, and the repeat
	// check, treat a later miss as the directory already shown.
	page := entries[pageStart:pageEnd]
	confirmationOnly := semanticToolsSearchRequestedConfirmationOnly(requestedNames, statusOf)
	writeConfirmation := func() {
		if len(waiting) == 0 {
			return
		}
		fmt.Fprintf(&out, "\n等待用户确认：%s。不要再次调用，也不要用其它工具完成该动作。\n", strings.Join(waiting, "、"))
	}
	if !confirmationOnly {
		writeConfirmation()
	}
	if listed := semanticToolsSearchListedNeeds(page, requestedNames, statusOf); len(listed) > 0 {
		fmt.Fprintf(&out, "%s%s。不要再查询这些名字。\n", semanticToolsSearchListedDirectivePrefix, strings.Join(listed, ", "))
	} else if line := semanticToolsSearchClosingLine(decision); line != "" {
		out.WriteString(line)
		out.WriteByte('\n')
	} else if discoverySurface {
		// The classified sermon says to call the name. That name is not on
		// this request's function list. needs is what loads it. A search
		// that only named a draft waiting for the user does not get this
		// line: it would send the model back to petition that same name.
		// An unfiltered directory still gets the line when some other row
		// is waiting, and that line stays the tail.
		if !confirmationOnly {
			out.WriteString(semanticToolsSearchDiscoveryLoadLine)
			out.WriteByte('\n')
		}
	} else if !discoverySurface {
		// This sentence is the classified turn's rule: one petition per
		// class, and bash / write_file stay off that turn. A discovery
		// turn has no class budget, and those names are ordinary
		// petitions. The row status is the eligibility mark.
		out.WriteString("只有本回合能力目录里标记「可请愿」的名字才可以直接调用一次请愿（每轮每类限一次）。bash 和 write_file 不在普通回合里。ssh、mailbox_send、computer_use 只有主标签就是该能力时才出现。")
	}
	if confirmationOnly {
		writeConfirmation()
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

// semanticToolsSearchRowStatusForAction keeps a planned row from ordering a
// call the page action does not name. The header clause already drops
// 「先调用」 when this result names a listed tool. The row was still saying
// it, so a save lookup followed that predecessor instead of the listed tool.
// The sentence stays only when callNow is the page action.
func semanticToolsSearchRowStatusForAction(status string, decision semanticToolsSearchDecision) string {
	if len(decision.callNow) > 0 || !strings.Contains(status, "先调用") {
		return status
	}
	return "[已列入本轮计划：不要直接调用，也不要请愿]"
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

// discoveryQueryStrongWords is the search query's vocabulary minus the
// words too common to rank by. A cut page orders its hidden-name index by
// these; an empty set keeps the catalog order.
func discoveryQueryStrongWords(query string) map[string]bool {
	words := map[string]bool{}
	for _, word := range discoveryNeedWords(query) {
		if !semanticToolsSearchWeakWords[word] {
			words[word] = true
		}
	}
	return words
}

// semanticToolsSearchUnshownNamesBudget caps the hidden-name index. Names
// are the discovery payload — summaries stay behind page_token — so the
// index outranks a few summary rows, but it must never become the page.
const semanticToolsSearchUnshownNamesBudget = 1200

// semanticToolsSearchUnshownNamesLine lists the names a cut page cannot
// show, so existence never depends on reaching the next page. Status and
// summaries still do: the line also says how to read them. When the index
// itself overflows its budget, names sharing a non-weak word with the
// query go first — the row the caller was looking for is the one that
// must not end up behind "等共 N 个".
func semanticToolsSearchUnshownNamesLine(rest []semanticToolsSearchEntry, query string, discoverySurface bool) string {
	if len(rest) == 0 {
		return ""
	}
	// The index copies before it orders: rest aliases entries, and
	// reordering the backing slice would corrupt the next page.
	ordered := append([]semanticToolsSearchEntry(nil), rest...)
	if scored := discoveryQueryStrongWords(query); len(scored) > 0 {
		score := func(name string) int {
			nameWords := map[string]bool{}
			for _, word := range discoveryNeedWords(name) {
				nameWords[word] = true
			}
			n := 0
			for word := range scored {
				if nameWords[word] {
					n++
				}
			}
			return n
		}
		sort.SliceStable(ordered, func(i, j int) bool {
			return score(ordered[i].name) > score(ordered[j].name)
		})
	}
	var b strings.Builder
	fmt.Fprintf(&b, "本页未列出的名字还有 %d 个：", len(rest))
	listed := 0
	for _, entry := range ordered {
		name := strings.TrimSpace(entry.name)
		if name == "" {
			continue
		}
		extra := len(name)
		if listed > 0 {
			extra += len("、")
		}
		if listed > 0 && b.Len()+extra > semanticToolsSearchUnshownNamesBudget {
			break
		}
		if listed > 0 {
			b.WriteString("、")
		}
		b.WriteString(name)
		listed++
	}
	if listed < len(ordered) {
		fmt.Fprintf(&b, " 等共 %d 个", len(ordered))
	}
	if discoverySurface {
		b.WriteString("。状态和描述用 page_token 翻页查看；其中标记「可请愿」的名字直接写进 needs 也能装载。没见到目标名字时不要断言该能力不存在。\n")
	} else {
		b.WriteString("。状态和描述用 page_token 翻页查看。没见到目标名字时不要断言该能力不存在。\n")
	}
	return b.String()
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
	closing    string
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
	if line := strings.TrimSpace(decision.closing); line != "" {
		return line
	}
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

const semanticToolsSearchUnmatchedNeedsDecision = "所查的名字不在本回合目录里。标记「可请愿」的名字可以单独调用一次来申请。没有对应名字就说明做不到，不要改用已经列出的其它工具。\n"

// semanticToolsSearchDiscoveryUnmatched is the same miss on a discovery
// turn. A missing directory row is not "the capability is impossible":
// the next call is search_and_install_skill, then craft_tool.
const semanticToolsSearchDiscoveryUnmatched = "所查的名字不在本回合目录里，没有对得上的能力。先调用 search_and_install_skill，query 用用户的原话；没有可用 Skill 时再调用 craft_tool。在这两步有结果之前，不要派后台任务，也不要改请愿别的名字。\n"

const semanticToolsSearchDiscoveryNextStep = "先调用 search_and_install_skill，query 用用户的原话；没有可用 Skill 时再调用 craft_tool。"

// semanticToolsSearchDiscoveryUnloaded is a needs list whose names are in
// the directory and none of them loaded. That is the same closed turn as a
// name the directory does not have: the next call is the extension path,
// and dispatch is not how a failed petition becomes a download.
const semanticToolsSearchDiscoveryUnloaded = "这些名字这一步没有加载。先调用 search_and_install_skill，query 用用户的原话；没有可用 Skill 时再调用 craft_tool。在这两步有结果之前，不要派后台任务，也不要改请愿别的名字。\n"

// semanticToolsSearchDiscoveryLoadLine is the action on a discovery page
// that has no plan decision and no listed-needs directive. The name is not
// on this request's function list. tools_search needs is what loads it.
const semanticToolsSearchDiscoveryLoadLine = "要加载标了「可请愿」的名字，再调用 tools_search，把那个名字放进 needs。当前函数列表里没有的名字不能调用，加载成功后它出现在下一请求。一次 needs 可以写多个工作区工具。"

// discoveryMainLoopComputerUseNote is the page fact when the main
// conversation was asked to load desktop control. The computer helper
// operates the desktop. This request does not gain that function.
const discoveryMainLoopComputerUseNote = "这个对话不操作桌面。computer_use 不会加载到下一请求。桌面操作由电脑助手执行。"

// soleDiscoveryDesktopDoorNote reports that this search loaded nothing and
// its only sentence is a desktop-control refusal. That sentence is the
// page's last line. A grant beside it stays in the header.
func soleDiscoveryDesktopDoorNote(notes string) (string, bool) {
	notes = strings.TrimSpace(notes)
	if notes == "" || strings.Contains(notes, "\n") || !strings.Contains(notes, "computer_use 不会加载到下一请求") {
		return "", false
	}
	return notes, true
}

const (
	// petitionInputUnboundMissing is a delivery template with no trusted
	// document and no in-turn producer.
	petitionInputUnboundMissing = "missing"
	// petitionInputUnboundAmbiguous is a delivery template with more than
	// one document that could bind.
	petitionInputUnboundAmbiguous = "ambiguous"
)

// discoveryTrustedInputUnboundNote is the page fact for one unbound name.
// code is petitionInputUnboundMissing or petitionInputUnboundAmbiguous.
// It does not order the next needs entry. discoveryUnboundFollowUp is the
// one last line for every unbound name in this search, so a second fact
// cannot hide the delivery step or aim that step at a read.
func (c *sharedAgentLoopCallbacks) discoveryTrustedInputUnboundNote(name, code string) string {
	name = strings.TrimSpace(name)
	if code == petitionInputUnboundAmbiguous {
		return name + " 在目录里。当前计划有多份可绑定的文档，不能选定，这一步不会加载。"
	}
	return name + " 在目录里。当前计划没有可绑定的受信任文档，这一步不会加载。"
}

// discoveryUnboundFollowUp is the last line when every load note is an
// unbound fact. A current-channel file delivery names the producers this
// admission layer would load, then that delivery name. A read keeps its
// fact in the head: a producer does not satisfy document.read.local.
// With no such delivery name, the facts themselves are the line and the
// page does not send the model to install a skill.
func (c *sharedAgentLoopCallbacks) discoveryUnboundFollowUp(facts string) string {
	facts = strings.TrimSpace(facts)
	unlocked := c.discoveryUnboundUnlockedNames()
	producers := c.discoveryAdmissibleDocumentProducers()
	if c == nil || len(unlocked) == 0 || len(producers) == 0 {
		if facts == "" {
			return ""
		}
		return facts + "不要调用 search_and_install_skill 或 craft_tool。"
	}
	return "再调用 tools_search，needs 里只写 " + discoveryJoinOr(producers) + "。加载成功后，再把 " + discoveryJoinOr(unlocked) + " 放进 needs。不要调用 search_and_install_skill 或 craft_tool。"
}

func (c *sharedAgentLoopCallbacks) discoveryUnboundUnlockedNames() []string {
	if c == nil || len(c.petitionInputUnbound) == 0 {
		return nil
	}
	names := make([]string, 0, len(c.petitionInputUnbound))
	for name, code := range c.petitionInputUnbound {
		name = strings.TrimSpace(name)
		if name == "" || code != petitionInputUnboundMissing || !discoveryInTurnProducerUnlocks(name) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// discoveryInTurnProducerUnlocks reports that a producer already in the
// plan satisfies this name. semanticNeedsForTrustedDocumentInputs skips
// the lookup only for current-channel delivery whose format is file.
// The petition label's template carries that format. document.read.local
// stays on the lookup, so a producer is not a follow-up for it.
func discoveryInTurnProducerUnlocks(name string) bool {
	capability, ok := semanticToolsSearchNameCapability(name)
	if !ok || capability != agentservice.CapabilityArtifactDeliverCurrent {
		return false
	}
	label, ok := semanticPetitionLabelForCapability(capability)
	if !ok {
		return false
	}
	for _, tmpl := range imSemanticIntentRuleSet[label] {
		if tmpl.Capability != capability {
			continue
		}
		need := tool.CapabilityNeed{Capability: tmpl.Capability, Qualifiers: tmpl.Qualifiers}
		if tool.CurrentChannelDeliverAccepts(need, agentservice.ArtifactFormatFile) {
			return true
		}
	}
	return false
}

// discoveryAdmissibleDocumentProducers is the directory names that satisfy
// the delivery template's in-turn producer check and that
// admitDiscoverySearchNeeds would load. The page's last line is the order
// the model follows, so a name this layer would count as a failed load
// stays off that line.
func (c *sharedAgentLoopCallbacks) discoveryAdmissibleDocumentProducers() []string {
	if c == nil {
		return nil
	}
	var names []string
	seen := map[string]bool{}
	for _, entry := range semanticToolsSearchInventory {
		name := strings.TrimSpace(entry.name)
		if name == "" || seen[name] || !discoveryDocumentProducerCapability(entry.capability) {
			continue
		}
		if !c.discoveryDocumentProducerPetitionLoads(name) {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// discoveryDocumentProducerCapability is a capability that makes
// current-channel file delivery skip the trusted-document lookup. It is
// the same pair the GUI bind passes to InTurnArtifactProducerPresent:
// document generate always counts, and office write is the host extra.
func discoveryDocumentProducerCapability(id tool.CapabilityID) bool {
	return tool.IsDocumentGenerateFile(id) || id == tool.CapabilityDocumentWriteOffice
}

// discoveryDocumentProducerPetitionLoads reports that a later tools_search
// whose only needs entry is this name would reach PetitionToolCall. These
// are the gates admitDiscoverySearchNeeds applies before that call. A
// producer already on the surface is already in the plan; this bind
// failure is not waiting on another petition of that name.
func (c *sharedAgentLoopCallbacks) discoveryDocumentProducerPetitionLoads(name string) bool {
	if c == nil || !c.discoverySearchNeedLoadable(name) || c.discoverySearchNeedCallable(name) {
		return false
	}
	if discoveryPictureCreationUtterance(c.userText) && discoverySubstituteTool(name) {
		return false
	}
	return true
}

func discoveryJoinOr(names []string) string {
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return strings.Join(names[:len(names)-1], "、") + " 或 " + names[len(names)-1]
	}
}

// discoveryAdmitUnboundAction reports that this search's only load notes are
// the bind results recorded on the callback. The page uses that block as its
// action. A grant or another sentence beside it stays in the header.
func discoveryAdmitUnboundAction(c *sharedAgentLoopCallbacks, loadedNotes string) (string, bool) {
	if c == nil || len(c.discoveryAdmitUnbound) == 0 {
		return "", false
	}
	block := strings.Join(c.discoveryAdmitUnbound, "\n")
	if strings.TrimSpace(loadedNotes) != block {
		return "", false
	}
	return block, true
}

// discoveryNeedsAreLoadableNames reports that every needs token is a name
// this discovery turn can petition. A directory row is not required. An
// unknown token is not loadable, and that list stays a catalog miss.
func discoveryNeedsAreLoadableNames(cb *sharedAgentLoopCallbacks, needs map[tool.CapabilityID]bool) bool {
	if cb == nil || len(needs) == 0 {
		return false
	}
	for token := range needs {
		name := strings.TrimSpace(string(token))
		if name == "" || !cb.discoverySearchNeedLoadable(name) {
			return false
		}
	}
	return true
}

func semanticToolsSearchUnmatchedDecision(cb *sharedAgentLoopCallbacks) semanticToolsSearchDecision {
	if cb != nil && semanticSurfaceIsModelDiscovery(cb.semanticSurface) {
		return semanticToolsSearchDecision{
			text:    semanticToolsSearchDiscoveryUnmatched,
			closing: semanticToolsSearchDiscoveryNextStep,
		}
	}
	return semanticToolsSearchDecision{text: semanticToolsSearchUnmatchedNeedsDecision}
}

// semanticToolsSearchNearMissDecision is the decision of a miss whose tokens
// still landed near directory rows. It does not arm the unmatched gate: an
// exact row may still be petitioned, and the extension path is named only as
// the fallback for a real capability gap.
func semanticToolsSearchNearMissDecision(candidates []string) semanticToolsSearchDecision {
	candidates = append([]string(nil), candidates...)
	sort.Strings(candidates)
	if len(candidates) > 6 {
		candidates = candidates[:6]
	}
	return semanticToolsSearchDecision{
		text:    "needs 里的名字不在本回合目录里，但和这些名字接近：" + strings.Join(candidates, "、") + "。对得上就把确切名字放进 needs 再查一次，或直接调用一次请愿。确实没有对得上的能力时，先调用 search_and_install_skill，没有可用 Skill 时再调用 craft_tool。\n",
		closing: "对得上就请愿目录里的确切名字；确实没有对得上的能力时先 search_and_install_skill，再 craft_tool。",
	}
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

// semanticToolsSearchRequestedConfirmationOnly reports that this search
// named only tools waiting for the user. The generic load line would tell
// the model to petition those names again. An empty request is a directory
// page, not that search, and keeps the load line.
func semanticToolsSearchRequestedConfirmationOnly(requested map[string]bool, statusOf func(string) string) bool {
	if len(requested) == 0 || statusOf == nil {
		return false
	}
	for name := range requested {
		name = strings.TrimSpace(name)
		if name == "" || statusOf(name) != "[等待用户确认：不要再次调用]" {
			return false
		}
	}
	return true
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
// the user said "用ssh访问" after a search-shaped turn. An explicit rejection
// is a user constraint and cannot be overridden by the model's search query.
func semanticToolsSearchMaybeExpandScope(cb *sharedAgentLoopCallbacks, query string) {
	if cb == nil || cb.semanticSurface == nil {
		return
	}
	userDeclaresSSH, userRejectsSSH := semanticUserTextSSHDirectives(cb.userText)
	if userRejectsSSH {
		return
	}
	queryDeclaresSSH, _ := semanticUserTextSSHDirectives(query)
	if !userDeclaresSSH && !queryDeclaresSSH {
		return
	}
	if !userDeclaresSSH && !semanticUserTextSupportsRemote(cb.userText) {
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
	declared, _ := semanticUserTextSSHDirectives(text)
	return declared
}

func semanticUserTextSSHDirectives(text string) (declared, rejected bool) {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false, false
	}
	for _, needle := range []string{"用ssh", "ssh访问", "ssh登录", "登录服务器", "ssh"} {
		for start := 0; start < len(lower); {
			at := strings.Index(lower[start:], needle)
			if at < 0 {
				break
			}
			at += start
			start = at + len(needle)
			if needle == "ssh" && ((at > 0 && isASCIILetterOrDigit(lower[at-1])) || (start < len(lower) && isASCIILetterOrDigit(lower[start]))) {
				continue
			}
			if semanticSSHMentionNegated(lower, at) {
				rejected = true
			} else {
				declared = true
			}
		}
	}
	return declared, rejected
}

func semanticSSHMentionNegated(lower string, at int) bool {
	if at <= 0 || at > len(lower) {
		return false
	}
	clause := lower[:at]
	if boundary := strings.LastIndexAny(clause, "。！？；,.;!?\n"); boundary >= 0 {
		clause = clause[boundary+1:]
	}
	for _, marker := range []string{"不要", "不用", "别用", "别改", "别通过", "别连接", "别登录", "禁止", "无需", "勿", "不可", "不能", "不许", "不使用", "不通过", "不采用", "不改用"} {
		if strings.Contains(clause, marker) {
			return true
		}
	}
	for _, marker := range []string{"do not", "don't", "dont", "never", "must not", "should not", "shouldn't", "not ", "no ", "without ", "avoid "} {
		if strings.Contains(clause, marker) {
			return true
		}
	}
	return false
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

// appendDiscoveryCatalogSearchEntries folds the installed skill/MCP
// catalog rows into the searchable listing. They carry no capability id:
// their petition resolves through the catalog row, not a capability match.
func appendDiscoveryCatalogSearchEntries(entries []semanticToolsSearchEntry, rows []semanticTurnDirectoryEntry) []semanticToolsSearchEntry {
	have := make(map[string]bool, len(entries)+len(rows))
	for _, entry := range entries {
		have[entry.name] = true
	}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" || have[name] {
			continue
		}
		have[name] = true
		summary := strings.TrimSpace(row.Summary)
		if summary == "" {
			summary = "已安装。"
		}
		entries = append(entries, semanticToolsSearchEntry{name: name, summary: summary})
	}
	return entries
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

// semanticToolsSearchNeedSynonyms maps a whole normalized needs token onto
// the one directory name it obviously means. These are intents a model
// spells naturally instead of the tool name; every entry is a deliberate,
// unambiguous pick. Multi-word spellings go through normalization, so
// "web search" and "web-search" never need entries here.
var semanticToolsSearchNeedSynonyms = map[string]string{
	"weather":       "web_search",
	"forecast":      "web_search",
	"news":          "web_search",
	"google":        "web_search",
	"bing":          "web_search",
	"baidu":         "web_search",
	"internet":      "web_search",
	"online":        "web_search",
	"searchengine":  "web_search",
	"stockprice":    "web_search",
	"天气":            "web_search",
	"天气预报":          "web_search",
	"新闻":            "web_search",
	"股价":            "web_search",
	"汇率":            "web_search",
	"knowledgebase": "knowledge_search",
	"kb":            "knowledge_search",
	"知识库":           "knowledge_search",
	"time":          "current_datetime",
	"date":          "current_datetime",
	"clock":         "current_datetime",
	"时间":            "current_datetime",
	"日期":            "current_datetime",
	"excel":         "office",
	"xlsx":          "office",
	"spreadsheet":   "office",
	"ppt":           "office",
	"pptx":          "office",
	"powerpoint":    "office",
	"slides":        "office",
	"表格":            "office",
	"pdf":           "generate_pdf",
	"download":      "download_file",
	"下载":            "download_file",
	"fetch":         "web_fetch",
	"crawl":         "web_fetch",
	"scrape":        "web_fetch",
	"抓取":            "web_fetch",
	"mic":           "record_audio",
	"microphone":    "record_audio",
	"record":        "record_audio",
	"录音":            "record_audio",
	"transcribe":    "asr",
	"transcription": "asr",
	"stt":           "asr",
	"转写":            "asr",
	"speak":         "tts_local",
	"voice":         "tts_local",
	"朗读":            "tts_local",
	"screen":        "screenshot",
	"截屏":            "screenshot",
	"截图":            "screenshot",
	"sql":           "database_query",
	"reminder":      "manage_schedule",
	"alarm":         "manage_schedule",
	"schedule":      "manage_schedule",
	"日程":            "manage_schedule",
	"提醒":            "manage_schedule",
	"memo":          "memory",
	"remember":      "memory",
	"记住":            "memory",
	"history":       "session_search",
	"audit":         "session_search",
	"历史":            "session_search",
	"commit":        "git_commit",
	"push":          "git_commit",
	"cmd":           "bash",
	"shell":         "bash",
	"terminal":      "bash",
	"命令行":           "bash",
	"delete":        "delete_file",
	"删除":            "delete_file",
	"sendmail":      "mailbox_send",
	"sendemail":     "mailbox_send",
	"发邮件":           "mailbox_send",
	"browse":        "browser",
	"website":       "browser",
	"浏览器":           "browser",
	"网页":            "browser",
	"email":         "mailbox_query",
	"mail":          "mailbox_query",
	"邮件":            "mailbox_query",
}

// semanticToolsSearchNeedWordSynonyms is the per-word form used when a token
// carries several words and none of the stronger matches fires ("search
// engine", "weather forecast"). A word maps only onto an unambiguous intent.
var semanticToolsSearchNeedWordSynonyms = map[string]string{
	"weather":  "web_search",
	"forecast": "web_search",
	"news":     "web_search",
	"engine":   "web_search",
}

// semanticToolsSearchWeakWords are vocabulary shared by so many directory
// names that a lone overlap is not evidence the token meant that row.
// "image.generate" sharing "generate" with generate_pdf must stay a genuine
// catalog miss, not a near miss for the document renderer.
var semanticToolsSearchWeakWords = map[string]bool{
	"generate": true, "manage": true, "search": true, "file": true,
	"files": true, "send": true, "deliver": true, "query": true,
	"data": true, "read": true, "write": true, "run": true, "save": true,
	"list": true, "render": true, "play": true, "open": true,
	"current": true, "local": true, "tool": true, "tools": true,
	"config": true, "session": true, "text": true, "record": true,
	"connect": true, "admin": true, "capture": true, "inspect": true,
	"launch": true, "track": true, "make": true, "create": true,
	"get": true, "use": true, "check": true, "new": true, "do": true,
}

// discoveryNeedStopwords are dropped before a token is compared. They carry
// no intent and would otherwise keep word sets unequal.
var discoveryNeedStopwords = map[string]bool{
	"the": true, "a": true, "an": true, "to": true, "of": true,
	"for": true, "and": true, "or": true, "in": true, "on": true,
	"at": true, "by": true, "does": true, "please": true, "me": true,
	"my": true, "i": true, "with": true, "via": true, "from": true,
}

// discoveryNeedWords splits a needs token or a directory name into the
// vocabulary loose matching compares. Naive singular-stripping is enough:
// the tokens are spellings, not free text.
func discoveryNeedWords(token string) []string {
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(token)), func(r rune) bool {
		return r == '_' || r == '-' || r == ' ' || r == '\t' || r == '/' || r == '.' || r == ':' || r == ',' || r == ';' || r == '(' || r == ')'
	})
	words := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || discoveryNeedStopwords[part] {
			continue
		}
		if len(part) > 3 && strings.HasSuffix(part, "s") && !strings.HasSuffix(part, "ss") {
			part = strings.TrimSuffix(part, "s")
		}
		words = append(words, part)
	}
	return words
}

// discoveryNeedNormalize is the joined vocabulary used for spelling-level
// equality: "web search", "web-search", and "web_search" all fold to the
// same string.
func discoveryNeedNormalize(token string) string {
	return strings.Join(discoveryNeedWords(token), "")
}

// resolveDiscoveryNeedsLoose maps needs tokens that matched nothing exactly
// onto the loadable directory rows they mean. Resolution is deliberately
// conservative — a synonym, a normalized spelling, an equal word set, or a
// single unambiguous concept target all point at one name. Tokens that only
// land near rows are candidates: the caller renders them as a near miss
// instead of arming the unmatched gate.
// discoveryNeedsLooseResolution is the outcome of resolving one needs list
// loosely. served marks the raw tokens that resolved: a petition that
// resolves some tokens still owes the caller a verdict on the rest.
type discoveryNeedsLooseResolution struct {
	resolved   map[string]bool
	caps       map[tool.CapabilityID]bool
	pairs      []string
	candidates []string
	served     map[string]bool
}

func resolveDiscoveryNeedsLoose(needs map[tool.CapabilityID]bool, entries []semanticToolsSearchEntry, cb *sharedAgentLoopCallbacks) discoveryNeedsLooseResolution {
	out := discoveryNeedsLooseResolution{
		resolved: map[string]bool{},
		caps:     map[tool.CapabilityID]bool{},
		served:   map[string]bool{},
	}
	candidates := map[string]bool{}
	for token := range needs {
		raw := strings.TrimSpace(string(token))
		if raw == "" {
			continue
		}
		name, near := discoveryNeedLooseMatch(raw, entries, cb)
		switch {
		case name != "":
			if !out.resolved[name] {
				out.pairs = append(out.pairs, raw+"→"+name)
			}
			out.resolved[name] = true
			out.served[raw] = true
			if capability, ok := semanticToolsSearchNameCapability(name); ok && capability != "" {
				out.caps[capability] = true
			} else {
				out.caps[tool.CapabilityID(name)] = true
			}
		default:
			for _, candidate := range near {
				candidates[candidate] = true
			}
		}
	}
	if len(out.resolved) == 0 {
		out.resolved = nil
		out.caps = nil
		out.served = nil
		out.candidates = make([]string, 0, len(candidates))
		for name := range candidates {
			out.candidates = append(out.candidates, name)
		}
		sort.Strings(out.candidates)
		return out
	}
	sort.Strings(out.pairs)
	return out
}

// discoveryNeedLooseMatch resolves one unmatched needs token. Only names the
// turn could actually load are eligible: a synonym that points at a denied
// or blocked name is not a resolution, and an ambiguous candidate set would
// only send the next search after the same wrong guess.
func discoveryNeedLooseMatch(token string, entries []semanticToolsSearchEntry, cb *sharedAgentLoopCallbacks) (string, []string) {
	norm := discoveryNeedNormalize(token)
	if norm == "" {
		return "", nil
	}
	// resolvable names a row the turn can actually load or call. A granted
	// name qualifies: the page's listed-needs directive then tells the model
	// to call it now. The clock stand-in does not — it is never a callable
	// function, so resolving to it would end at a dead petition.
	resolvable := func(name string) bool {
		if cb == nil {
			return false
		}
		// A name the unmatched gate closed is not a target: resolving to it
		// would only print a promise the substitution block then retracts.
		if blocked, _ := cb.discoveryPetitionBlocked(name); blocked {
			return false
		}
		// A denied petition counts as a failed load and arms the gate.
		// A loose slip must not route the turn back into one; the name can
		// still be a near candidate whose row carries the denial mark.
		if _, denied := cb.semanticPetitionDenied[name]; denied {
			return false
		}
		if cb.discoverySearchNeedLoadable(name) {
			return true
		}
		return cb.discoverySearchNeedCallable(name) && !modelDiscoveryHidesGrant(name)
	}
	// nearable is the wider candidate set for a near miss. A hidden stand-in
	// may be named as a look-alike; it just cannot be the resolution. A
	// petitionable name whose backing service is down (an empty knowledge
	// base) still belongs in the list: its row then carries the honest
	// unavailable mark, which is what stops the retry — not silence. A
	// name the unmatched gate already closed stays out.
	nearable := func(name string) bool {
		if cb == nil {
			return false
		}
		if blocked, _ := cb.discoveryPetitionBlocked(name); blocked {
			return false
		}
		if resolvable(name) || cb.discoverySearchNeedCallable(name) {
			return true
		}
		_, petitionable := semanticPetitionableCapabilities[name]
		return petitionable
	}
	if synonym, ok := semanticToolsSearchNeedSynonyms[norm]; ok {
		if resolvable(synonym) {
			return synonym, nil
		}
		// The token names a real capability the turn cannot serve (an
		// empty knowledge base, the hidden clock). It is still the best
		// near candidate: its row carries the reason, and a soft miss
		// keeps the gate open for the names the turn can load.
		if nearable(synonym) {
			return "", []string{synonym}
		}
	}
	words := discoveryNeedWords(token)
	var exact, near []string
	seen := map[string]bool{}
	consider := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] || !nearable(name) {
			return
		}
		seen[name] = true
		nameNorm := discoveryNeedNormalize(name)
		switch {
		case nameNorm == norm, discoveryNeedWordSetEqual(words, name):
			if resolvable(name) {
				exact = append(exact, name)
			} else {
				near = append(near, name)
			}
		case strings.Contains(nameNorm, norm) || strings.Contains(norm, nameNorm),
			discoveryNeedSharesStrongWord(words, name):
			near = append(near, name)
		}
	}
	for _, entry := range entries {
		consider(entry.name)
	}
	for name := range semanticPetitionableCapabilities {
		consider(name)
	}
	if cb != nil {
		for _, row := range cb.discoveryCatalogRows() {
			consider(row.Name)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		return "", sortedUniqueStrings(exact)
	}
	// A concept token carries no directory vocabulary ("search engine",
	// "weather forecast"). Per-word synonyms resolve it only when every
	// mapped word agrees on one target and no name evidence competes.
	if len(near) == 0 {
		targets := map[string]bool{}
		mapped := false
		for _, word := range words {
			if target, ok := semanticToolsSearchNeedWordSynonyms[word]; ok {
				mapped = true
				targets[target] = true
			}
		}
		if mapped && len(targets) == 1 {
			for target := range targets {
				if resolvable(target) {
					return target, nil
				}
				if nearable(target) {
					return "", []string{target}
				}
			}
		}
		if len(targets) > 1 {
			list := make([]string, 0, len(targets))
			for target := range targets {
				if nearable(target) {
					list = append(list, target)
				}
			}
			return "", sortedUniqueStrings(list)
		}
		return "", nil
	}
	return "", sortedUniqueStrings(near)
}

// discoveryNeedWordSetEqual reports that a needs token and a directory name
// carry exactly the same vocabulary ("file search" is search_files).
func discoveryNeedWordSetEqual(words []string, name string) bool {
	nameWords := discoveryNeedWords(name)
	if len(words) == 0 || len(words) != len(nameWords) {
		return false
	}
	set := make(map[string]int, len(words))
	for _, word := range words {
		set[word]++
	}
	for _, word := range nameWords {
		set[word]--
		if set[word] < 0 {
			return false
		}
	}
	for _, count := range set {
		if count != 0 {
			return false
		}
	}
	return true
}

// discoveryNeedSharesStrongWord reports a shared non-weak vocabulary word —
// evidence the token reached for this row but spelled it differently.
func discoveryNeedSharesStrongWord(words []string, name string) bool {
	nameWords := map[string]bool{}
	for _, word := range discoveryNeedWords(name) {
		nameWords[word] = true
	}
	for _, word := range words {
		if !semanticToolsSearchWeakWords[word] && nameWords[word] {
			return true
		}
	}
	return false
}

func sortedUniqueStrings(values []string) []string {
	set := map[string]bool{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || set[value] {
			continue
		}
		set[value] = true
		out = append(out, value)
	}
	sort.Strings(out)
	return out
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
// the exact model-visible name bound by the current surface. Stable adapters
// and installed MCP tools (server__tool) resolve before a grant. Other dynamic
// adapters are added only from their materialized opaque grant token. An
// internal dynamic AdapterName without a rendered grant is never advertised
// as if it were callable, which keeps discovery and the actual surface identical.
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
			// Stable host adapters and installed MCP tools have a model name
			// before the first grant. Other dynamic adapters stay on the
			// grant token and are added from the grant table below.
			if name := semanticStableRenderedName(selection.AdapterName); name != "" {
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
		// model-visible name. They enrich a row the plan or a grant already
		// added. A dynamic schema alone must not advertise a tool.
		for adapter, definition := range surface.schemas {
			adapter = strings.TrimSpace(adapter)
			if strings.HasPrefix(adapter, "installed-mcp:") {
				if rendered := semanticStableRenderedName(adapter); rendered != "" {
					if entry, ok := byName[rendered]; ok {
						if description := strings.TrimSpace(tool.ExtractToolDescription(definition)); description != "" {
							entry.summary = description
							byName[rendered] = entry
						}
					}
				}
			}
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
	if result, ok := semanticCallbackClassification(cb); ok {
		return semanticToolsSearchApplyDirectory(entries, result)
	}
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

// semanticKnowledgeBaseUnavailable reports that the local knowledge store
// cannot serve a search this turn: it failed to open or holds no sources.
// The directory advertises that fact instead of offering a petition that can
// only fail — production 2026-11-11 petitioned knowledge_search first, got
// "知识库不可用", and the detour cost the turn its recovery path.
func (c *sharedAgentLoopCallbacks) semanticKnowledgeBaseUnavailable() bool {
	if c == nil {
		return false
	}
	if c.knowledgeUnavailable != nil {
		return *c.knowledgeUnavailable
	}
	unavailable := false
	if c.handler != nil && c.handler.app != nil {
		unavailable = !veHasKnowledgeSources(c.handler.app)
	}
	c.knowledgeUnavailable = &unavailable
	return unavailable
}

func semanticToolsSearchStatus(cb *sharedAgentLoopCallbacks, name string) string {
	if cb == nil {
		return "[本轮不可用：不要调用，用已列出的工具完成]"
	}
	surface := cb.semanticSurface
	hidden := cb != nil && semanticSurfaceIsModelDiscovery(surface) && modelDiscoveryHidesGrant(name)
	if hidden {
		// The clock stand-in stays on the plan so the turn can be published.
		// It is not a function the model can call, so the row must not read
		// like a petition target or the settled surface.
		return "[本轮不可用：不要调用，用已列出的工具完成]"
	}
	if surface != nil {
		if _, ok := surface.grants[name]; ok {
			return "[已在当前工具面]"
		}
	}
	// The sidecar grant is a second tools/call while the managed write is
	// still awaiting its receipt. Do not describe that name as callable.
	if cb.managedInstalledMCPSpentThisRequest(name) && surface != nil {
		if grant, ok := semanticRetiredGrantForName(surface, name); ok {
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
	// A discovery search loads a runnable skill onto the next request.
	// The binding is that function. It is not a semantic grant.
	if _, ok := cb.admittedSkillBindings[name]; ok {
		return "[已在当前工具面]"
	}
	if _, ok := cb.discoverySearchBodies[name]; ok {
		return "[已装载]"
	}
	if surface != nil {
		if grant, ok := semanticRetiredGrantForName(surface, name); ok {
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
		if note := semanticPlannedWaitNote(cb, name, semanticPetitionableCapabilities[name]); note != "" {
			return "[" + note + "]"
		}
		return "[已列入本轮计划：不要直接调用，也不要请愿]"
	}
	if _, ok := semanticPetitionableCapabilities[name]; ok {
		if name == "knowledge_search" && cb.semanticKnowledgeBaseUnavailable() {
			return "[本机知识库为空或未配置，请愿它也没有结果]"
		}
		if _, has := semanticCallbackClassification(cb); has && !cb.semanticTurnAdmits(name) {
			return "[本轮不可用：不要调用，用已列出的工具完成]"
		}
		if _, denied := cb.semanticPetitionDenied[name]; denied {
			return "[此名请愿未通过，不要重试；同类其它「可请愿」名字仍可调用]"
		}
		switch cb.petitionInputUnbound[name] {
		case petitionInputUnboundAmbiguous:
			return "[已在目录：当前计划有多份可绑定的文档]"
		case petitionInputUnboundMissing:
			return "[已在目录：当前计划没有可绑定的受信任文档]"
		}
		consumed := cb.semanticPetitionConsumed
		if semanticPetitionIsEffectful(name) {
			consumed = cb.semanticEffectfulPetitionConsumed
		}
		if consumed {
			return "[本轮请愿机会已用完，不要调用]"
		}
		// Discovery does not spend the one-petition budget, and this
		// provider does not emit a name that is absent from the function
		// list. "直接调用一次" is that budget and that emit. The row keeps
		// the eligibility mark. Naming the tool in needs is what loads it.
		if semanticSurfaceIsModelDiscovery(surface) {
			return "[可请愿]"
		}
		return "[可请愿：直接调用一次]"
	}
	// An installed skill or ready MCP tool is petitionable on a discovery
	// surface even though it is not in the closed petition map: its grant
	// resolves through the catalog row, not a rule label.
	if cb != nil && semanticSurfaceIsModelDiscovery(surface) && cb.discoveryCatalogHas(name) {
		return "[可请愿]"
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
		if adapter == name || semanticStableRenderedName(adapter) == name {
			return true
		}
	}
	return false
}

// LiftExpiredDirectoryHolds is the outbound copy of a tools_search page.
// A hold such as "下一请求出现：generate_pdf。现在不要调用。" is true only for
// the request that produced it. The closer names the next step; a later step
// such as send_file is only in the "尚未列出" clause, and that clause is the
// same hold once the wire list actually contains the name. tools is the slice
// this request will send, which can be narrower than the callback's snapshot.
// A name already called after that page loses the prohibition and is not
// ordered again. A petition denial is the same request-scoped fact in another
// tool result: the denied name, and any other name that sentence says is
// still absent. The stored tool result is not rewritten.
func (c *sharedAgentLoopCallbacks) LiftExpiredDirectoryHolds(conversation []interface{}, tools []map[string]interface{}) []interface{} {
	if c == nil {
		return nil
	}
	next, ok := liftExpiredToolsSearchHolds(conversation, semanticVisibleToolNames(tools))
	if !ok {
		return nil
	}
	return next
}

func liftExpiredToolsSearchHolds(conversation []interface{}, listed map[string]bool) ([]interface{}, bool) {
	if len(conversation) == 0 || len(listed) == 0 {
		return nil, false
	}
	// A hold page is the next-request closer or the planned-hold decision.
	// The predecessor page ends in "现在调用 web_search" and never uses the
	// closer, but its "不要直接调用这些名字" is the same stale prohibition
	// once generate_pdf is on the wire list.
	lastHold := -1
	lastDenial := map[string]int{}
	for i, item := range conversation {
		content, ok := toolResultContent(item)
		if !ok {
			continue
		}
		if holdContent, hold := directoryHoldToolContent(item); hold && directoryHoldPage(holdContent) {
			lastHold = i
		}
		if name, ok := rejectedCallName(content); ok {
			lastDenial[name] = i
		}
	}
	if lastHold < 0 && len(lastDenial) == 0 {
		return nil, false
	}
	// A rejection names the tool that did not run. That call is not a
	// completed use. Every other assistant call in the conversation is.
	used := namesUsed(conversation)
	// Only the latest hold page may order a call. Earlier pages lose a
	// prohibition that the wire list has already lifted.
	var out []interface{}
	changed := false
	for i, item := range conversation {
		content, isTool := toolResultContent(item)
		var next string
		var ok bool
		if isTool && strings.Contains(content, "tools_search results for ") && directoryHoldPage(content) {
			// Calls after this page, plus the assistant message that produced it.
			// web_search is usually in that message, before the result exists.
			// A planned denial in that batch means the call did not run.
			after := namesCalledSince(conversation, i)
			issuing := issuingAssistantToolNames(conversation, i)
			next, ok = rewriteExpiredToolsSearchHold(content, listed, after, issuing, i == lastHold)
		}
		if !ok && isTool {
			if _, denial := plannedDenialToolName(content); denial {
				next, ok = rewriteExpiredPlannedDenial(content, listed, namesCalledSince(conversation, i), used, lastHold, lastDenial, i)
			} else if _, rejection := surfaceRejectionName(content); rejection {
				next, ok = rewriteExpiredSurfaceRejection(content, listed, namesCalledSince(conversation, i), lastHold, lastDenial, i)
			} else if _, rejected := argumentRejectionName(content); rejected {
				next, ok = rewriteExpiredArgumentRejection(content, listed, namesCalledSince(conversation, i))
			}
		}
		if !ok {
			if changed {
				out = append(out, item)
			}
			continue
		}
		if !changed {
			out = make([]interface{}, 0, len(conversation))
			out = append(out, conversation[:i]...)
			changed = true
		}
		out = append(out, directoryHoldWithContent(item, next))
	}
	if !changed {
		return nil, false
	}
	return out, true
}

// namesCalledSince is the set of tool names that ran after index. A
// rejection in that call's batch means the call did not execute, so the
// name stays uncalled and can still be ordered once it is listed. A
// consumed-grant result stays a call: that name already ran. Any other
// result counts as the call, because the tool message does not carry the
// tool name.
func namesCalledSince(conversation []interface{}, index int) map[string]bool {
	var called map[string]bool
	for i := index + 1; i < len(conversation); i++ {
		names, assistant := assistantToolNames(conversation[i])
		if !assistant {
			continue
		}
		denied := rejectedNamesInBatch(conversation, i)
		for _, name := range names {
			if denied[name] {
				continue
			}
			if called == nil {
				called = map[string]bool{}
			}
			called[name] = true
		}
	}
	return called
}

// rejectedNamesInBatch is the set of names whose result in this assistant
// batch says the call did not execute. The result may sit before or after
// the tools_search page from the same message.
func rejectedNamesInBatch(conversation []interface{}, assistantAt int) map[string]bool {
	var denied map[string]bool
	for i := assistantAt + 1; i < len(conversation); i++ {
		if _, assistant := assistantToolNames(conversation[i]); assistant {
			break
		}
		content, isTool := toolResultContent(conversation[i])
		if !isTool {
			continue
		}
		name, ok := rejectedCallName(content)
		if !ok {
			continue
		}
		if denied == nil {
			denied = map[string]bool{}
		}
		denied[name] = true
	}
	return denied
}

func messageToolNames(item interface{}) []string {
	m, ok := item.(map[string]interface{})
	if !ok {
		return nil
	}
	role, _ := m["role"].(string)
	if role != "assistant" {
		return nil
	}
	return toolCallNames(m["tool_calls"])
}

func toolCallNames(raw interface{}) []string {
	switch calls := raw.(type) {
	case nil:
		return nil
	case []llm.ToolCall:
		names := make([]string, 0, len(calls))
		for _, call := range calls {
			name := strings.TrimSpace(call.Function.Name)
			if name != "" {
				names = append(names, name)
			}
		}
		return names
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil
	}
	var loose []struct {
		Function struct {
			Name string `json:"name"`
		} `json:"function"`
	}
	if json.Unmarshal(data, &loose) != nil {
		return nil
	}
	names := make([]string, 0, len(loose))
	for _, call := range loose {
		name := strings.TrimSpace(call.Function.Name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func toolResultContent(item interface{}) (string, bool) {
	var role, content string
	switch m := item.(type) {
	case map[string]interface{}:
		role, _ = m["role"].(string)
		content, _ = m["content"].(string)
	case map[string]string:
		role = m["role"]
		content = m["content"]
	default:
		return "", false
	}
	if role != "tool" {
		return "", false
	}
	return content, true
}

func directoryHoldToolContent(item interface{}) (string, bool) {
	content, ok := toolResultContent(item)
	if !ok || !strings.Contains(content, "tools_search results for ") {
		return "", false
	}
	return content, true
}

// plannedDenialToolName is the tool named by a petition denial that is true
// only while that tool is absent from the request. The call that produced
// the denial sits before this result, so it is not evidence the tool was used.
func plannedDenialToolName(content string) (string, bool) {
	const lead = "Error: tool \""
	const mid = "\" is planned for this turn but "
	if !strings.HasPrefix(content, lead) {
		return "", false
	}
	rest := content[len(lead):]
	end := strings.Index(rest, mid)
	if end <= 0 {
		return "", false
	}
	name := rest[:end]
	if strings.Contains(name, "\n") || strings.Contains(name, "\"") {
		return "", false
	}
	return name, true
}

// rejectedCallName is the tool whose result says this invocation did not
// execute. A planned denial, an unrendered-surface denial, a repeat-wave
// continuation, and an argument rejection are the same fact. A consumed-grant
// result is not: that name already ran.
func rejectedCallName(content string) (string, bool) {
	if name, ok := plannedDenialToolName(content); ok {
		return name, true
	}
	if name, ok := surfaceRejectionName(content); ok {
		return name, true
	}
	return argumentRejectionName(content)
}

// argumentRejectionName is a call the loop refused before execution because
// the arguments were not a JSON object. The sentence tells the model to
// send the call again. That attempt is not a completed use.
func argumentRejectionName(content string) (string, bool) {
	const lead = "Error: tool call \""
	const mid = "\" has invalid JSON object arguments. The tool was not executed."
	if !strings.HasPrefix(content, lead) {
		return "", false
	}
	rest := content[len(lead):]
	end := strings.Index(rest, mid)
	if end <= 0 {
		return "", false
	}
	name := rest[:end]
	if name == "" || strings.Contains(name, "\n") || strings.Contains(name, "\"") {
		return "", false
	}
	return name, true
}

// surfaceRejectionName is a non-execution result that is not a planned
// denial. The call stayed off this request, or it was deferred to the next
// one. The name is the quoted tool at the start of the sentence.
func surfaceRejectionName(content string) (string, bool) {
	const lead = "Error: tool \""
	if !strings.HasPrefix(content, lead) {
		return "", false
	}
	rest := content[len(lead):]
	for _, mid := range []string{
		"\" was not available in this request's rendered tool surface.",
		"\" is listed on the next request in this same reply.",
	} {
		end := strings.Index(rest, mid)
		if end <= 0 {
			continue
		}
		name := rest[:end]
		if name == "" || strings.Contains(name, "\n") || strings.Contains(name, "\"") {
			return "", false
		}
		return name, true
	}
	return "", false
}

// namesUsed is every assistant tool call that was not rejected. A successful
// result does not name the tool, so a call with no rejection counts as use.
// The rejected call stays unused, including when it is the only call in that
// assistant message.
func namesUsed(conversation []interface{}) map[string]bool {
	used := map[string]bool{}
	for i, item := range conversation {
		names, assistant := assistantToolNames(item)
		if !assistant {
			continue
		}
		denied := rejectedNamesInBatch(conversation, i)
		for _, name := range names {
			if denied[name] {
				continue
			}
			used[name] = true
		}
	}
	return used
}

func assistantToolNames(item interface{}) ([]string, bool) {
	m, ok := item.(map[string]interface{})
	if !ok {
		return nil, false
	}
	role, _ := m["role"].(string)
	if role != "assistant" {
		return nil, false
	}
	return toolCallNames(m["tool_calls"]), true
}

// rewriteExpiredPlannedDenial drops "not listed yet" / "do not call it again
// before it is listed" once the wire list contains the name. A denial whose
// own name is still absent can still name another tool ("generate_pdf appears
// on the next request", "Call web_search now"). Those clauses are the same
// request-scoped fact. Only the latest denial of the rejected name, and only
// when no later hold page is the action, orders a call. The denied call
// itself is not a completed use.
func rewriteExpiredPlannedDenial(content string, listed, after, used map[string]bool, lastHold int, lastDenial map[string]int, index int) (string, bool) {
	name, ok := plannedDenialToolName(content)
	if !ok {
		return content, false
	}
	latest := index == lastDenial[name]
	allowOrder := latest && (lastHold < 0 || index >= lastHold)
	if listed[name] {
		if allowOrder && !after[name] {
			return fmt.Sprintf("Error: tool %q is listed on this request. Call it now.", name), true
		}
		return fmt.Sprintf("Error: tool %q is listed on this request.", name), true
	}
	// The held denial says "It appears on the next request" and names nobody.
	// The subject's own quoted appear-clause is also left as stored. A later
	// call that actually ran spends that promise. The call that produced this
	// denial sits before it and is not in after.
	if after[name] {
		return fmt.Sprintf("Error: tool %q has already been called.", name), true
	}
	return projectUnlistedPlannedDenial(content, name, listed, used, allowOrder)
}

// rewriteExpiredSurfaceRejection lifts an unrendered-surface denial or a
// repeat-wave continuation once that fact is false. The call did not run.
// Only the latest rejection of the name orders it, and only when no later
// hold page is the action. A name that later ran, and is absent from this
// wire list, is already called. The stored result is left as it was.
func rewriteExpiredSurfaceRejection(content string, listed, after map[string]bool, lastHold int, lastDenial map[string]int, index int) (string, bool) {
	name, ok := surfaceRejectionName(content)
	if !ok {
		return content, false
	}
	latest := index == lastDenial[name]
	allowOrder := latest && (lastHold < 0 || index >= lastHold)
	if listed[name] {
		if allowOrder && !after[name] {
			return fmt.Sprintf("Error: tool %q is listed on this request. Call it now.", name), true
		}
		return fmt.Sprintf("Error: tool %q is listed on this request.", name), true
	}
	if after[name] {
		return fmt.Sprintf("Error: tool %q has already been called.", name), true
	}
	return content, false
}

// rewriteExpiredArgumentRejection leaves the regenerate sentence while this
// attempt is still the outstanding call. The sentence already says to send
// the call again, and it is the only place the model is told the arguments
// were rejected. A later call that actually ran spends that instruction.
func rewriteExpiredArgumentRejection(content string, listed, after map[string]bool) (string, bool) {
	name, ok := argumentRejectionName(content)
	if !ok || !after[name] {
		return content, false
	}
	if listed[name] {
		return fmt.Sprintf("Error: tool %q is listed on this request.", name), true
	}
	return fmt.Sprintf("Error: tool %q has already been called.", name), true
}

// projectUnlistedPlannedDenial rewrites a denial that still correctly holds
// its own name. Quoted "Call X now" and "X appears on the next request"
// clauses move with the wire list. The subject's prohibition stays.
func projectUnlistedPlannedDenial(content, subject string, listed, used map[string]bool, allowOrder bool) (string, bool) {
	callNow := quotedNamesBefore(content, " now.")
	appear := quotedNamesBefore(content, " appears on the next request")
	if len(callNow) == 0 && len(appear) == 0 {
		return content, false
	}
	var orderNow, listedOnly, stillNext []string
	seen := map[string]bool{}
	changed := false
	for _, name := range callNow {
		if name == subject || seen[name] {
			continue
		}
		seen[name] = true
		if used[name] || !listed[name] {
			changed = true
			continue
		}
		orderNow = append(orderNow, name)
	}
	for _, name := range appear {
		if name == subject || seen[name] {
			continue
		}
		seen[name] = true
		if !listed[name] {
			if used[name] {
				changed = true
				continue
			}
			stillNext = append(stillNext, name)
			continue
		}
		changed = true
		if used[name] || !allowOrder {
			listedOnly = append(listedOnly, name)
			continue
		}
		orderNow = append(orderNow, name)
	}
	if !changed {
		return content, false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Error: tool %q is planned for this turn but is not listed yet.", subject)
	if len(orderNow) > 0 {
		b.WriteString(" Call " + joinListedNames(orderNow) + " now.")
	}
	if len(listedOnly) > 0 {
		b.WriteString(" " + joinListedNames(listedOnly) + " is listed on this request.")
	}
	if len(stillNext) > 0 {
		b.WriteString(" " + joinListedNames(stillNext) + " appears on the next request; call it then.")
	}
	fmt.Fprintf(&b, " Call %q only after %q is listed.", subject, subject)
	b.WriteString(" Do not tell the user the tool is missing and do not ask the user to re-authorize tools.")
	next := b.String()
	if next == content {
		return content, false
	}
	return next, true
}

// quotedNamesBefore parses the joinListedNames list immediately before
// marker. "Call a tool that is listed now" and "It appears on the next
// request" name nobody, and stay unmatched.
func quotedNamesBefore(text, marker string) []string {
	at := strings.Index(text, marker)
	if at < 0 {
		return nil
	}
	return trailingQuotedNames(text[:at])
}

func trailingQuotedNames(head string) []string {
	head = strings.TrimRight(head, " ")
	var names []string
	for strings.HasSuffix(head, `"`) {
		head = head[:len(head)-1]
		quote := strings.LastIndex(head, `"`)
		if quote < 0 {
			return nil
		}
		name := strings.TrimSpace(head[quote+1:])
		if name == "" || strings.ContainsAny(name, "\"\n") {
			return nil
		}
		names = append(names, name)
		head = strings.TrimRight(head[:quote], " ")
		switch {
		case strings.HasSuffix(head, " and"):
			head = strings.TrimRight(head[:len(head)-len(" and")], " ")
		case strings.HasSuffix(head, ","):
			head = strings.TrimRight(head[:len(head)-1], " ")
		default:
			head = ""
		}
	}
	if len(names) == 0 {
		return nil
	}
	for i, j := 0, len(names)-1; i < j; i, j = i+1, j-1 {
		names[i], names[j] = names[j], names[i]
	}
	return names
}

func directoryHoldWithContent(item interface{}, content string) interface{} {
	switch m := item.(type) {
	case map[string]interface{}:
		cp := make(map[string]interface{}, len(m))
		for k, v := range m {
			cp[k] = v
		}
		cp["content"] = content
		return cp
	case map[string]string:
		cp := make(map[string]string, len(m))
		for k, v := range m {
			cp[k] = v
		}
		cp["content"] = content
		return cp
	default:
		return item
	}
}

func directoryHoldPage(page string) bool {
	if _, _, ok := directoryHoldCloser(page); ok {
		return true
	}
	return len(plannedHoldClauses(page)) > 0
}

// issuingAssistantToolNames is the assistant message that requested this
// tool result, minus calls that batch rejected. Those calls sit before the
// result, so a scan that starts after the page never sees them. A rejection
// anywhere in the batch is proof the name did not run, including when the
// result is stored before this page. A consumed-grant result stays.
func issuingAssistantToolNames(conversation []interface{}, index int) map[string]bool {
	assistantAt := -1
	var names []string
	for i := index - 1; i >= 0; i-- {
		found, ok := assistantToolNames(conversation[i])
		if !ok {
			continue
		}
		assistantAt = i
		names = found
		break
	}
	if assistantAt < 0 {
		return nil
	}
	denied := rejectedNamesInBatch(conversation, assistantAt)
	seen := map[string]bool{}
	for _, name := range names {
		if denied[name] {
			continue
		}
		seen[name] = true
	}
	if len(seen) == 0 {
		return nil
	}
	return seen
}

func rewriteExpiredToolsSearchHold(page string, listed, after, issuing map[string]bool, order bool) (string, bool) {
	closerNames, closerPhrase, hasCloser := directoryHoldCloser(page)
	orderNames, orderPhrase, hasOrderTail := tailCallLine(page)
	holds := directoryHoldSubjectNames(page, closerNames)
	if len(holds) == 0 && !hasCloser {
		return page, false
	}
	holdSet := map[string]bool{}
	for _, name := range holds {
		holdSet[name] = true
	}
	// Predecessor orders ("现在调用 web_search") are not holds. A hold named
	// in the same assistant message was rejected. An order there ran only
	// when that batch did not deny it.
	var keptOrders []string
	seenOrder := map[string]bool{}
	addOrder := func(name string) {
		if name == "" || holdSet[name] || seenOrder[name] || !listed[name] {
			return
		}
		if after[name] || issuing[name] {
			return
		}
		seenOrder[name] = true
		keptOrders = append(keptOrders, name)
	}
	for _, name := range decisionCallNames(page) {
		addOrder(name)
	}
	for _, name := range orderNames {
		addOrder(name)
	}
	var priorOrders []string
	seenPrior := map[string]bool{}
	notePrior := func(name string) {
		if name == "" || holdSet[name] || seenPrior[name] {
			return
		}
		seenPrior[name] = true
		priorOrders = append(priorOrders, name)
	}
	for _, name := range decisionCallNames(page) {
		notePrior(name)
	}
	for _, name := range orderNames {
		notePrior(name)
	}
	// A hold called after this page is finished even when this request no
	// longer lists it. The one-shot grant is spent, so "出现在下一请求" is
	// false and keeps the model waiting on a tool that will not return.
	// Say 已调用. The wire list is the only source of 已在当前工具列表.
	// A hold in the issuing message was denied and is not finished.
	called := map[string]bool{}
	for name := range after {
		called[name] = true
	}
	for name := range issuing {
		if !holdSet[name] {
			called[name] = true
		}
	}
	listedHold := false
	finishedHold := false
	var callNow, released, still, finished []string
	for _, name := range holds {
		switch {
		case listed[name] && order && !after[name]:
			listedHold = true
			callNow = append(callNow, name)
		case listed[name]:
			listedHold = true
			released = append(released, name)
		case after[name]:
			finished = append(finished, name)
			finishedHold = true
		default:
			still = append(still, name)
		}
	}
	expiredOrder := len(keptOrders) != len(priorOrders)
	if !listedHold && !expiredOrder && !finishedHold {
		return page, false
	}
	original := page
	if order {
		callNow = append(keptOrders, callNow...)
	}
	// Drop a hold from 尚未列出 once it is on the wire list or already called.
	// called matches after for holds: the issuing denial stays out.
	drop := map[string]bool{}
	for _, name := range holds {
		if listed[name] || called[name] {
			drop[name] = true
		}
	}
	callSentence := ""
	if order && len(callNow) > 0 {
		callSentence = "立刻调用 " + strings.Join(callNow, "、") + "。"
	} else if !order && len(keptOrders) > 0 {
		callSentence = "现在调用 " + strings.Join(keptOrders, "、") + "。"
	}
	page = rewriteHeldDecisionLine(page, callSentence, drop, listed, called)
	page = rewriteHeldDirectoryRows(page, callNow, released, finished)
	// 先调用 in a row can name a predecessor the decision line already
	// shortened away. Issuing names reach called only when that batch let
	// them run. A planned denial keeps the lead.
	page = relaxCalledPredecessorLeads(page, called)
	// listed, not drop: a finished name absent from the wire list must not
	// become 已在当前工具列表.
	page = relaxListedAppearance(page, listed, called)
	// A row ban with no name belongs to that row. Once another tool in the
	// same bracket is listed or already called, the ban reads as forbidding
	// that tool.
	page = bindAnonymousRowBan(page, listed, called)
	var tail string
	if order {
		if len(callNow) > 0 {
			tail = "现在调用 " + strings.Join(callNow, "、") + "。"
		}
		if len(still) > 0 {
			tail += "仍未列出、现在不要调用：" + strings.Join(still, "、") + "。"
		}
	} else if len(keptOrders) > 0 && hasOrderTail {
		tail = "现在调用 " + strings.Join(keptOrders, "、") + "。"
	}
	switch {
	case hasCloser:
		// An older page drops the closer. Putting a predecessor order there
		// would make that old page the action again.
		if !order {
			tail = ""
		}
		page = replaceLast(page, closerPhrase, tail)
	case hasOrderTail:
		page = replaceLast(page, orderPhrase, tail)
	default:
		page = replaceLast(page, semanticToolsSearchDeferredCloser, tail)
	}
	if page == original {
		return page, false
	}
	return page, true
}

func decisionCallNames(page string) []string {
	const mark = "本轮决定："
	const lead = "现在调用 "
	at := strings.LastIndex(page, mark)
	if at < 0 {
		return nil
	}
	rest := page[at+len(mark):]
	if !strings.HasPrefix(rest, lead) {
		return nil
	}
	rest = rest[len(lead):]
	end := strings.Index(rest, "。")
	if end < 0 || strings.Contains(rest[:end], "\n") {
		return nil
	}
	return splitHoldNames(rest[:end])
}

func tailCallLine(page string) (names []string, phrase string, ok bool) {
	trimmed := strings.TrimRight(page, "\n")
	at := strings.LastIndex(trimmed, "\n")
	line := trimmed
	if at >= 0 {
		line = trimmed[at+1:]
	}
	const lead = "现在调用 "
	if !strings.HasPrefix(line, lead) || !strings.HasSuffix(line, "。") {
		return nil, "", false
	}
	body := line[len(lead) : len(line)-len("。")]
	if body == "" || strings.Contains(body, "。") {
		return nil, "", false
	}
	names = splitHoldNames(body)
	if len(names) == 0 {
		return nil, "", false
	}
	return names, line, true
}

func splitHoldNames(body string) []string {
	var names []string
	for _, name := range strings.Split(body, "、") {
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

func stripDecisionCallNow(head string) string {
	const mark = "本轮决定："
	const lead = "现在调用 "
	at := strings.LastIndex(head, mark)
	if at < 0 {
		return head
	}
	tail := head[at+len(mark):]
	if !strings.HasPrefix(tail, lead) {
		return head
	}
	rest := tail[len(lead):]
	end := strings.Index(rest, "。")
	if end < 0 || strings.Contains(rest[:end], "\n") {
		return head
	}
	return head[:at+len(mark)] + rest[end+len("。"):]
}

func directoryHoldCloser(page string) (names []string, phrase string, ok bool) {
	const lead = "下一请求出现："
	const tail = "。现在不要调用。"
	at := strings.LastIndex(page, lead)
	if at < 0 {
		return nil, "", false
	}
	rest := page[at+len(lead):]
	end := strings.Index(rest, tail)
	if end < 0 || strings.Contains(rest[:end], "\n") {
		return nil, "", false
	}
	for _, name := range strings.Split(rest[:end], "、") {
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil, "", false
	}
	return names, page[at : at+len(lead)+end+len(tail)], true
}

// directoryHoldSubjectNames is the closer plus every "尚未列出" clause name.
// Clause order follows the closer, then the decision line.
func directoryHoldSubjectNames(page string, closerNames []string) []string {
	names := make([]string, 0, len(closerNames)+2)
	seen := map[string]bool{}
	add := func(name string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		seen[name] = true
		names = append(names, name)
	}
	for _, name := range closerNames {
		add(name)
	}
	for _, clause := range plannedHoldClauses(page) {
		add(plannedHoldClauseName(clause))
	}
	return names
}

func plannedHoldClauses(page string) []string {
	const prefix = "已计划但本请求尚未列出："
	const banLead = "。不要直接调用这些名字"
	at := strings.Index(page, prefix)
	if at < 0 {
		return nil
	}
	rest := page[at+len(prefix):]
	banAt := strings.Index(rest, banLead)
	if banAt < 0 {
		return nil
	}
	return splitPlannedHoldClauses(rest[:banAt])
}

func replaceLast(page, phrase, replacement string) string {
	at := strings.LastIndex(page, phrase)
	if at < 0 {
		return page
	}
	if replacement == "" && at > 0 && page[at-1] == '\n' {
		return page[:at-1] + page[at+len(phrase):]
	}
	return page[:at] + replacement + page[at+len(phrase):]
}

func rewriteHeldDecisionLine(page, callSentence string, drop, listed, called map[string]bool) string {
	const prefix = "已计划但本请求尚未列出："
	const banLead = "。不要直接调用这些名字"
	at := strings.Index(page, prefix)
	if at < 0 {
		return page
	}
	rest := page[at+len(prefix):]
	banAt := strings.Index(rest, banLead)
	if banAt < 0 {
		return page
	}
	clauses := splitPlannedHoldClauses(rest[:banAt])
	var kept []string
	for _, clause := range clauses {
		if drop[plannedHoldClauseName(clause)] {
			continue
		}
		// A kept clause can still say a listed predecessor "出现在下一请求".
		// That fact is false on this request and talks the model out of the
		// call this request is ordering. The wire list is the authority, not
		// only the names this page was holding.
		kept = append(kept, relaxStaleNextRequestFact(clause, listed, called))
	}
	ban := rest[banAt:]
	banEnd := strings.Index(ban[len(banLead):], "。")
	if banEnd < 0 {
		return page
	}
	originalBan := ban[:len(banLead)+banEnd+len("。")]
	after := ban[len(originalBan):]
	// "这些名字" used to cover every held clause. After a listed name is
	// removed, name the clauses that are still held so the ban cannot be
	// read as covering the name this request is supposed to call.
	banSentence := originalBan
	if len(kept) > 0 {
		keptNames := make([]string, len(kept))
		for i, clause := range kept {
			keptNames[i] = plannedHoldClauseName(clause)
		}
		banSentence = "。不要直接调用 " + strings.Join(keptNames, "、") + originalBan[len(banLead):]
	}
	head := stripDecisionCallNow(page[:at])
	if len(kept) == 0 {
		if callSentence == "" {
			head = strings.TrimSuffix(head, "本轮决定：")
		}
		return head + callSentence + after
	}
	return head + callSentence + prefix + strings.Join(kept, "；") + banSentence + after
}

func splitPlannedHoldClauses(body string) []string {
	var clauses []string
	depth := 0
	start := 0
	for i, r := range body {
		switch r {
		case '（':
			depth++
		case '）':
			if depth > 0 {
				depth--
			}
		case '；':
			if depth == 0 {
				if clause := strings.TrimSpace(body[start:i]); clause != "" {
					clauses = append(clauses, clause)
				}
				start = i + len("；")
			}
		}
	}
	if clause := strings.TrimSpace(body[start:]); clause != "" {
		clauses = append(clauses, clause)
	}
	return clauses
}

func plannedHoldClauseName(clause string) string {
	name, _, _ := strings.Cut(clause, "（")
	return strings.TrimSpace(name)
}

// relaxStaleNextRequestFact rewrites a predecessor fact inside a clause that
// stays held. The clause's own name is not listed. A listed name must not
// stay "absent until the next request", including when it shares a 、 list
// with a name that is still absent. A name already called and absent from
// this wire list is 已调用, not 已在当前工具列表. "后再调用" names nobody,
// so a settled name does not keep it.
func relaxStaleNextRequestFact(clause string, listed, called map[string]bool) string {
	if clause == "" || (len(listed) == 0 && len(called) == 0) {
		return clause
	}
	clause = relaxNextRequestSuffix(clause, "出现在下一请求后再调用", listed, called)
	return relaxNextRequestSuffix(clause, "出现在下一请求", listed, called)
}

// relaxNextRequestSuffix rewrites the 、-separated names immediately before
// one "出现在下一请求" suffix. A single-name replace only sees the name glued
// to the suffix, so an earlier listed name stays absent and an earlier
// unlisted name gets pulled into "已在当前工具列表".
func relaxNextRequestSuffix(clause, suffix string, listed, called map[string]bool) string {
	var b strings.Builder
	rest := clause
	for {
		at := strings.Index(rest, suffix)
		if at < 0 {
			b.WriteString(rest)
			return b.String()
		}
		names, start, ok := namesBeforeSuffix(rest, at)
		end := at + len(suffix)
		if !ok || !anySettledHoldName(names, listed, called) {
			b.WriteString(rest[:end])
			rest = rest[end:]
			continue
		}
		b.WriteString(rest[:start])
		b.WriteString(joinNextRequestFacts(names, suffix, listed, called))
		rest = rest[end:]
	}
}

func anySettledHoldName(names []string, listed, called map[string]bool) bool {
	for _, name := range names {
		if listed[name] || called[name] {
			return true
		}
	}
	return false
}

func joinNextRequestFacts(names []string, suffix string, listed, called map[string]bool) string {
	var parts []string
	var pending []string
	flush := func() {
		if len(pending) == 0 {
			return
		}
		parts = append(parts, strings.Join(pending, "、")+" "+suffix)
		pending = nil
	}
	for _, name := range names {
		switch {
		case listed[name]:
			flush()
			parts = append(parts, name+" 已在当前工具列表")
		case called[name]:
			flush()
			parts = append(parts, name+" 已调用")
		default:
			pending = append(pending, name)
		}
	}
	flush()
	return strings.Join(parts, "，")
}

// namesBeforeSuffix is the tool-name list that ends at at. Spaces around
// the names and the 、 separators belong to the list. The separator before
// the first name stays outside it.
func namesBeforeSuffix(s string, at int) (names []string, start int, ok bool) {
	i := at
	for i > 0 && s[i-1] == ' ' {
		i--
	}
	var rev []string
	nameStart := i
	for {
		nameEnd := i
		for i > 0 && isHoldNameByte(s[i-1]) {
			i--
		}
		if i == nameEnd {
			break
		}
		rev = append(rev, s[i:nameEnd])
		nameStart = i
		for i > 0 && s[i-1] == ' ' {
			i--
		}
		if strings.HasSuffix(s[:i], "、") {
			i -= len("、")
			for i > 0 && s[i-1] == ' ' {
				i--
			}
			continue
		}
		break
	}
	if len(rev) == 0 {
		return nil, 0, false
	}
	for a, b := 0, len(rev)-1; a < b; a, b = a+1, b-1 {
		rev[a], rev[b] = rev[b], rev[a]
	}
	return rev, nameStart, true
}

func isHoldNameByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '_'
}

// relaxCalledPredecessorLeads rewrites "先调用 web_search、web_fetch" once
// those calls have already been issued. A single-name replace leaves the
// second name attached to a lead that was removed with the first.
func relaxCalledPredecessorLeads(page string, called map[string]bool) string {
	if page == "" || len(called) == 0 || !strings.Contains(page, "先调用 ") {
		return page
	}
	return relaxDelimitedLead(page, "先调用 ", func(name string) (string, bool) {
		if called[name] {
			return name + " 已调用", false
		}
		return "", true
	})
}

// relaxListedAppearance rewrites "之后会出现 generate_pdf" and "先等
// generate_pdf" once that absence is false. A name on the wire list is
// 已在当前工具列表. A name already called and absent from this wire list is
// 已调用. Passing the drop set here would describe that spent name as listed.
func relaxListedAppearance(page string, listed, called map[string]bool) string {
	if page == "" || (len(listed) == 0 && len(called) == 0) {
		return page
	}
	appear := func(name string) (string, bool) {
		if listed[name] {
			return name + " 已在当前工具列表", false
		}
		if called[name] {
			return name + " 已调用", false
		}
		return "", true
	}
	if strings.Contains(page, "之后会出现 ") {
		page = relaxDelimitedLead(page, "之后会出现 ", appear)
	}
	if strings.Contains(page, "先等 ") {
		page = relaxDelimitedLead(page, "先等 ", appear)
	}
	return page
}

// relaxDelimitedLead rewrites one lead and the 、-separated tool names that
// follow it. keep false removes that name from the lead; replacement is the
// fact that takes its place. The bytes after the list, including 。， stay.
func relaxDelimitedLead(page, lead string, mapName func(name string) (replacement string, keep bool)) string {
	var b strings.Builder
	rest := page
	for {
		at := strings.Index(rest, lead)
		if at < 0 {
			b.WriteString(rest)
			return b.String()
		}
		names, end, ok := parseHoldNameList(rest[at+len(lead):])
		if !ok {
			b.WriteString(rest[:at+len(lead)])
			rest = rest[at+len(lead):]
			continue
		}
		var kept, extra []string
		changed := false
		for _, name := range names {
			replacement, keep := mapName(name)
			if keep {
				kept = append(kept, name)
				continue
			}
			changed = true
			if replacement != "" {
				extra = append(extra, replacement)
			}
		}
		span := at + len(lead) + end
		if !changed {
			b.WriteString(rest[:span])
			rest = rest[span:]
			continue
		}
		b.WriteString(rest[:at])
		if len(extra) > 0 {
			b.WriteString(strings.Join(extra, "，"))
		}
		if len(kept) > 0 {
			if len(extra) > 0 {
				b.WriteString("，")
			}
			b.WriteString(lead)
			b.WriteString(strings.Join(kept, "、"))
		}
		rest = rest[span:]
	}
}

func parseHoldNameList(s string) (names []string, end int, ok bool) {
	i := 0
	for {
		for i < len(s) && s[i] == ' ' {
			i++
		}
		start := i
		for i < len(s) && isHoldNameByte(s[i]) {
			i++
		}
		if i == start {
			break
		}
		names = append(names, s[start:i])
		for i < len(s) && s[i] == ' ' {
			i++
		}
		if i < len(s) && strings.HasPrefix(s[i:], "、") {
			i += len("、")
			continue
		}
		break
	}
	if len(names) == 0 {
		return nil, 0, false
	}
	return names, i, true
}

func rewriteHeldDirectoryRows(page string, callNow, released, finished []string) string {
	status := map[string]string{}
	for _, name := range callNow {
		status[name] = "[已在当前工具列表，立刻调用。]"
	}
	for _, name := range released {
		if _, ok := status[name]; ok {
			continue
		}
		status[name] = "[已在当前工具列表。]"
	}
	// A listed name already won the row. 已调用 is only for a hold that ran
	// and is absent from this wire list. The row matcher still requires
	// 不要直接调用, so a spent-grant row stays as it was stored.
	for _, name := range finished {
		if _, ok := status[name]; ok {
			continue
		}
		status[name] = "[已调用。]"
	}
	if len(status) == 0 {
		return page
	}
	lines := strings.Split(page, "\n")
	changed := false
	for i, line := range lines {
		next := rewriteHeldDirectoryRow(line, status)
		if next != line {
			lines[i] = next
			changed = true
		}
	}
	if !changed {
		return page
	}
	return strings.Join(lines, "\n")
}

// bindAnonymousRowBan names the row when its "不要直接调用" has no subject
// and the same bracket already names a different tool that is listed now
// or already called. "generate_pdf 已调用" beside an unnamed ban reads as
// forbidding that finished tool. The decision ban was given the same
// treatment. A live page never reaches here: the hold is still true, and
// the anonymous ban still means the row.
func bindAnonymousRowBan(page string, listed, called map[string]bool) string {
	if page == "" || (len(listed) == 0 && len(called) == 0) || !strings.Contains(page, "不要直接调用，也不要请愿") {
		return page
	}
	lines := strings.Split(page, "\n")
	changed := false
	for i, line := range lines {
		if !strings.HasPrefix(line, "- ") || !strings.Contains(line, "不要直接调用，也不要请愿") {
			continue
		}
		rest := strings.TrimPrefix(line, "- ")
		name, detail, ok := strings.Cut(rest, " — ")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if name == "" || !rowBracketMentionsOtherListed(detail, name, listed, called) {
			continue
		}
		lines[i] = strings.Replace(line, "不要直接调用，也不要请愿", "不要直接调用 "+name+"，也不要请愿", 1)
		changed = true
	}
	if !changed {
		return page
	}
	return strings.Join(lines, "\n")
}

func rowBracketMentionsOtherListed(detail, row string, listed, called map[string]bool) bool {
	scope := detail
	if open := strings.LastIndex(detail, "["); open >= 0 {
		scope = detail[open:]
	}
	for i := 0; i < len(scope); {
		if !isHoldNameByte(scope[i]) {
			i++
			continue
		}
		start := i
		for i < len(scope) && isHoldNameByte(scope[i]) {
			i++
		}
		token := scope[start:i]
		if token != row && (listed[token] || called[token]) {
			return true
		}
	}
	return false
}

func rewriteHeldDirectoryRow(line string, status map[string]string) string {
	if !strings.HasPrefix(line, "- ") || !strings.Contains(line, "不要直接调用") {
		return line
	}
	rest := strings.TrimPrefix(line, "- ")
	name, _, ok := strings.Cut(rest, " — ")
	if !ok {
		return line
	}
	next, ok := status[strings.TrimSpace(name)]
	if !ok {
		return line
	}
	open := strings.LastIndex(line, "[")
	if open < 0 {
		return line
	}
	return line[:open] + next
}
