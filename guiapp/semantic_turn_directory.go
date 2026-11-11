package guiapp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const semanticDirectoryPetitionDenied = "[system] 这个名字不在本回合能力目录里。调用目录中标记可请愿的名字，或使用当前工具列表。"

// semanticTurnDirectoryEntry is one row of the projection shared by the
// system prompt and tools_search. Role is grant or petition.
type semanticTurnDirectoryEntry struct {
	Name    string
	Summary string
	Role    string
	// CatalogKind is set only on a discovery catalog row: mcp or skill.
	// The grant follows this, not a "__" in the rendered name. A long MCP
	// name is rewritten without that separator, and a skill alias can
	// contain it.
	CatalogKind string
	// ServerID and ToolName are the MCP bind this row observed. The grant
	// uses them. A later inventory can fail or drop the tool, and must not
	// be required to recover an identity the row already has.
	ServerID string
	ToolName string
	// StableID is the skill this row observed. The grant loads that skill
	// and binds the row's name. A later scan can give the alias to a
	// different skill; the alias is not the identity.
	StableID string
}

// semanticOrdinaryQA is a turn whose labels carry no capability. The legacy
// name router must not publish a second tool list for it.
func semanticOrdinaryQA(result intent.ClassificationResult) bool {
	if result.Primary == "" {
		return false
	}
	for _, label := range result.Labels() {
		if !label.IsNonCapabilityLabel() {
			return false
		}
	}
	for _, label := range result.Offered {
		if label != "" && !label.IsNonCapabilityLabel() {
			return false
		}
	}
	return true
}

func semanticCallbackClassification(cb *sharedAgentLoopCallbacks) (intent.ClassificationResult, bool) {
	if cb == nil {
		return intent.ClassificationResult{}, false
	}
	// The surface record is the classification that minted this turn's grants.
	// A loop intent from another pass must not hide a neighbor the plan already
	// published into the directory.
	if cb.semanticSurface != nil && cb.semanticSurface.replan != nil && cb.semanticSurface.replan.Classification.Primary != "" {
		return cb.semanticSurface.replan.Classification, true
	}
	if cb.loopCtx != nil && cb.loopCtx.Runtime.SemanticIntent != nil {
		return *cb.loopCtx.Runtime.SemanticIntent, true
	}
	return intent.ClassificationResult{}, false
}

func semanticSurfaceHasGrant(surface *semanticCallSurface, name string) bool {
	if surface == nil {
		return false
	}
	_, ok := surface.grants[name]
	return ok
}

// semanticTurnDirectory is the names this turn may know about. The primary
// label's tools are the grant rows. Offered and secondary neighbors are
// petitionable. ssh, mailbox send, and desktop control stay out unless the
// primary is that family and cleared the write floor. bash and write_file
// stay out of an ordinary turn.
func semanticTurnDirectory(result intent.ClassificationResult) []semanticTurnDirectoryEntry {
	if intent.ClassificationIsModelDiscovery(result) {
		return modelDiscoveryDirectory()
	}
	if semanticOrdinaryQA(result) {
		return nil
	}
	var entries []semanticTurnDirectoryEntry
	seen := map[string]bool{}
	add := func(name, summary, role string) {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			return
		}
		if semanticDirectoryClosed(result, name) {
			return
		}
		seen[name] = true
		entries = append(entries, semanticTurnDirectoryEntry{Name: name, Summary: summary, Role: role})
	}
	for _, name := range semanticDirectoryNamesForLabel(result.Primary) {
		add(name, semanticDirectorySummary(name), "grant")
	}
	for _, label := range result.Offered {
		for _, name := range semanticDirectoryNamesForLabel(label) {
			add(name, semanticDirectorySummary(name), "petition")
		}
	}
	for _, label := range result.Secondary {
		if label == result.Primary {
			continue
		}
		for _, name := range semanticDirectoryNamesForLabel(label) {
			add(name, semanticDirectorySummary(name), "petition")
		}
	}
	// Archetype neighbors stay out of the first grant wave. The directory
	// is how the model reaches them: web_fetch on a search, screenshot on
	// a browser turn. A primary the directory itself closed (a weak
	// computer_use) does not open its companions as a side door.
	if !semanticDirectoryPrimaryClosed(result) {
		for _, label := range agentservice.SemanticArchetypeBundles()[result.Primary] {
			for _, name := range semanticDirectoryNamesForLabel(label) {
				add(name, semanticDirectorySummary(name), "petition")
			}
		}
		// Family neighbors are directory rows only. A mailbox read can
		// petition delete or configure after the user authorizes it; send
		// stays closed unless that label is the primary.
		for _, label := range semanticDirectoryFamilyCompanions(result.Primary) {
			for _, name := range semanticDirectoryNamesForLabel(label) {
				add(name, semanticDirectorySummary(name), "petition")
			}
		}
	}
	return entries
}

// modelDiscoveryDirectory is the catalog the chat model may petition.
// The clock anchor is not a choice. ssh, mailbox send, and desktop control
// are petition rows. They are not on the first-wave function list, and a
// sentence does not grant them.
func modelDiscoveryDirectory() []semanticTurnDirectoryEntry {
	names := make([]string, 0, len(semanticPetitionableCapabilities))
	for name := range semanticPetitionableCapabilities {
		if name == "current_datetime" {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	entries := make([]semanticTurnDirectoryEntry, 0, len(names))
	for _, name := range names {
		entries = append(entries, semanticTurnDirectoryEntry{
			Name: name, Summary: semanticDirectorySummary(name), Role: "petition",
		})
	}
	return entries
}

func semanticDirectoryFamilyCompanions(primary intent.IntentLabel) []intent.IntentLabel {
	switch primary {
	case intent.LabelMailboxRead:
		return []intent.IntentLabel{intent.LabelMailboxDelete, intent.LabelMailboxConfigure}
	default:
		return nil
	}
}

func semanticDirectoryPrimaryClosed(result intent.ClassificationResult) bool {
	names := semanticDirectoryNamesForLabel(result.Primary)
	if len(names) == 0 {
		return false
	}
	for _, name := range names {
		if !semanticDirectoryClosed(result, name) {
			return false
		}
	}
	return true
}

func semanticDirectoryAdmitsPetition(result intent.ClassificationResult, name string) bool {
	name = strings.TrimSpace(name)
	for _, entry := range semanticTurnDirectory(result) {
		if entry.Name == name {
			return true
		}
	}
	return false
}

func (c *sharedAgentLoopCallbacks) semanticTurnAdmits(name string) bool {
	if c == nil {
		return false
	}
	name = strings.TrimSpace(name)
	key := semanticDirectoryAdmitKey(c)
	if c.directoryAdmitKey != key || c.directoryAdmit == nil {
		names := map[string]bool{}
		if result, ok := semanticCallbackClassification(c); ok {
			for _, entry := range semanticTurnDirectory(result) {
				names[entry.Name] = true
			}
		}
		c.directoryAdmitKey = key
		c.directoryAdmit = names
	}
	if c.directoryAdmit[name] {
		return true
	}
	// Installed skill and MCP names are not in the static directory cache.
	// A discovery turn admits them by the current catalog.
	if result, ok := semanticCallbackClassification(c); ok && intent.ClassificationIsModelDiscovery(result) {
		return c.discoveryCatalogHas(name)
	}
	return false
}

func semanticDirectoryAdmitKey(c *sharedAgentLoopCallbacks) string {
	if c == nil {
		return ""
	}
	result, ok := semanticCallbackClassification(c)
	if !ok {
		return fmt.Sprintf("none|%p", c.semanticSurface)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%p|%s|%t|%.4f", c.semanticSurface, result.Primary, result.Degraded, result.Confidence)
	for _, label := range result.Secondary {
		b.WriteByte(',')
		b.WriteString(string(label))
	}
	b.WriteByte('|')
	for _, label := range result.Offered {
		b.WriteByte(',')
		b.WriteString(string(label))
	}
	return b.String()
}

func semanticDirectoryClosed(result intent.ClassificationResult, name string) bool {
	switch name {
	case "ssh":
		return result.Degraded || result.Confidence < imSemanticMinimumConfidence || result.Primary != intent.LabelSSH
	case "mailbox_send":
		return result.Degraded || result.Confidence < imSemanticMinimumConfidence || result.Primary != intent.LabelMailboxSend
	case "computer_use":
		return result.Degraded || result.Confidence < imSemanticMinimumConfidence || result.Primary != intent.LabelComputerUse
	case "bash":
		return result.Primary != intent.LabelShellCommand
	case "write_file":
		switch result.Primary {
		case intent.LabelFileWrite, intent.LabelCoding, intent.LabelBugFix, intent.LabelMaintenance:
			return false
		default:
			return true
		}
	default:
		return false
	}
}

func semanticDirectoryNamesForLabel(label intent.IntentLabel) []string {
	switch label {
	case intent.LabelSearch, intent.LabelLiveData:
		return []string{"web_search"}
	case intent.LabelWebFetch:
		return []string{"web_fetch"}
	case intent.LabelOffice:
		return []string{"office"}
	case intent.LabelDocumentGenerate:
		return []string{"generate_pdf"}
	case intent.LabelLiveDataVisual:
		return []string{"web_search", semanticTrustedLiveDataVisualAdapter}
	case intent.LabelFileRead:
		return []string{"read_file"}
	case intent.LabelDocumentRead:
		return []string{"read_attachment"}
	case intent.LabelFileWrite:
		return []string{"write_file"}
	case intent.LabelCoding, intent.LabelBugFix, intent.LabelMaintenance:
		return []string{"read_file", "write_file", "git_status", "build_verify"}
	case intent.LabelShellCommand:
		return []string{"bash"}
	case intent.LabelSSH:
		return []string{"ssh"}
	case intent.LabelMailboxSend:
		return []string{"mailbox_send"}
	case intent.LabelComputerUse:
		return []string{"computer_use"}
	case intent.LabelBrowser:
		return []string{"browser"}
	case intent.LabelScreenshot:
		return []string{"screenshot"}
	case intent.LabelAppLaunch, intent.LabelDocumentOpen:
		return []string{"open"}
	case intent.LabelAttachmentDelivery:
		return []string{"send_file"}
	case intent.LabelDocumentDelivery:
		return []string{"send_to_im"}
	case intent.LabelCurrentTime:
		return []string{"current_datetime"}
	case intent.LabelFileDownload:
		return []string{"download_file"}
	case intent.LabelDatabase:
		return []string{"database_query"}
	case intent.LabelBusinessData:
		return []string{"database", "database_query"}
	case intent.LabelDelegateTask:
		return []string{"delegate_task"}
	case intent.LabelGitInspect:
		return []string{"git_status"}
	case intent.LabelGitMutate:
		return []string{"git_commit"}
	case intent.LabelKnowledgeRead:
		return []string{"knowledge_search"}
	case intent.LabelKnowledgeWrite:
		return []string{"knowledge_save_text"}
	case intent.LabelKnowledgeAdmin:
		return []string{"knowledge_maintain"}
	case intent.LabelAudioTranscribe:
		return []string{"asr"}
	case intent.LabelAudioRecord:
		return []string{"record_audio"}
	case intent.LabelAudioDeliver:
		return []string{"tts_render"}
	case intent.LabelAudioSynthesize:
		return []string{"tts_local"}
	case intent.LabelAuditRead:
		return []string{"session_search"}
	case intent.LabelFileDelete:
		return []string{"delete_file"}
	case intent.LabelMemoryManage:
		return []string{"memory"}
	case intent.LabelTaskTrack:
		return []string{"task"}
	case intent.LabelGoalManage:
		return []string{"goal"}
	case intent.LabelTemplateManage:
		return []string{"manage_template"}
	case intent.LabelSessionManage:
		return []string{"list_sessions"}
	case intent.LabelScheduleManage:
		return []string{"manage_schedule"}
	case intent.LabelScheduleDispatch:
		return []string{"manage_schedule", "schedule_dispatch"}
	case intent.LabelConfigManage:
		return []string{"manage_config"}
	case intent.LabelMailboxRead:
		return []string{"mailbox_query"}
	case intent.LabelMailboxDelete:
		return []string{"mailbox_delete"}
	case intent.LabelMailboxConfigure:
		return []string{"mailbox_configure"}
	default:
		return nil
	}
}

func semanticDirectorySummary(name string) string {
	if summary, ok := semanticDirectorySummaryByName[name]; ok {
		return summary
	}
	switch name {
	case semanticTrustedLiveDataVisualAdapter:
		return "Render the current trusted lookup as an image."
	case "send_image":
		return "Deliver the approved image to the current channel."
	case "send_voice":
		return "Deliver the approved speech to the current channel."
	}
	if strings.Contains(name, "__") {
		return "Installed tool for this turn."
	}
	return "Capability admitted for this turn."
}

var semanticDirectorySummaryByName = semanticDirectorySummaries()

func semanticDirectorySummaries() map[string]string {
	summaries := make(map[string]string, len(semanticToolsSearchInventory))
	for _, entry := range semanticToolsSearchInventory {
		summaries[entry.name] = entry.summary
	}
	return summaries
}

const semanticTurnDirectoryHeading = "## 本回合能力目录\n"
const semanticTurnDirectoryClose = "[本回合能力目录结束]"
const semanticDiscoveryDirectoryPreface = "本轮不预选工具。标了「可请愿」的名字，直接调用一次来申请。没有对应名字时，先 search_and_install_skill，再用 craft_tool。不要用另一种工具顶替；工具报告不可用或失败后，改请愿目录里真正对得上的名字不算顶替。文字问题直接回答。\n"

func appendSemanticTurnDirectoryRules(b *strings.Builder, result intent.ClassificationResult) {
	if b == nil {
		return
	}
	writeSemanticTurnDirectory(b, result, semanticDirectoryExposure{})
}

// semanticDirectoryExposure is the live surface state the directory sentence
// is allowed to describe. A spent grant must not stay worded as callable.
type semanticDirectoryExposure struct {
	Granted                map[string]bool
	Planned                map[string]bool
	Retired                map[string]bool
	AwaitingConfirm        map[string]bool
	AwaitingReceipt        map[string]bool
	Wait                   map[string]string
	Denied                 map[string]bool
	ReadPetitionSpent      bool
	EffectfulPetitionSpent bool
	// Unavailable is a petitionable name whose backing service cannot serve
	// this turn at all (an empty knowledge base). The row stays visible as
	// honest metadata instead of offering a petition that can only fail.
	Unavailable map[string]string
	// Catalog is the discovery-only list of installed skills and ready MCP
	// tools. Those names are petition rows. They are not grants.
	Catalog []semanticTurnDirectoryEntry
}

func semanticSurfaceExposure(surface *semanticCallSurface) semanticDirectoryExposure {
	exposure := semanticDirectoryExposure{
		Granted: semanticSurfaceGrantNames(surface),
		Planned: semanticSurfacePlanNames(surface),
		Retired: semanticSurfaceRetiredNames(surface),
	}
	exposure.AwaitingConfirm, exposure.AwaitingReceipt = semanticSurfaceAwaitingNames(surface)
	return exposure
}

func writeSemanticTurnDirectory(b *strings.Builder, result intent.ClassificationResult, exposure semanticDirectoryExposure) {
	entries := semanticTurnDirectory(result)
	b.WriteString("\n")
	b.WriteString(semanticTurnDirectoryHeading)
	if intent.ClassificationIsModelDiscovery(result) {
		b.WriteString(semanticDiscoveryDirectoryPreface)
	}
	if len(entries) == 0 {
		b.WriteString("- 本回合没有 bash，也没有 write_file。不要调用它们。\n")
	} else {
		for _, entry := range entries {
			switch {
			case exposure.Granted[entry.Name]:
				fmt.Fprintf(b, "- %s — %s 已在当前函数列表里。\n", entry.Name, entry.Summary)
			case exposure.AwaitingConfirm[entry.Name]:
				// A frozen mailbox draft is not a spent tool. "已用尽" sends
				// the model to finish the send with something else.
				fmt.Fprintf(b, "- %s — %s 等待用户确认，不要再次调用，也不要用其它工具完成该动作。\n", entry.Name, entry.Summary)
			case exposure.AwaitingReceipt[entry.Name]:
				fmt.Fprintf(b, "- %s — %s 结果未定，不要再次调用。\n", entry.Name, entry.Summary)
			case exposure.Retired[entry.Name]:
				fmt.Fprintf(b, "- %s — %s 本轮授权已用尽，不要调用。\n", entry.Name, entry.Summary)
			case exposure.Wait[entry.Name] != "":
				fmt.Fprintf(b, "- %s — %s %s\n", entry.Name, entry.Summary, exposure.Wait[entry.Name])
			case entry.Role != "grant" && exposure.Planned[entry.Name]:
				// A held successor is already signed. Asking for another
				// petition spends the turn on a name that is waiting.
				fmt.Fprintf(b, "- %s — %s 已列入本轮计划，不要直接调用，也不要请愿。\n", entry.Name, entry.Summary)
			case entry.Role == "grant":
				fmt.Fprintf(b, "- %s — %s 主标签授权。\n", entry.Name, entry.Summary)
			case exposure.Denied[entry.Name]:
				fmt.Fprintf(b, "- %s — %s 此名请愿未通过，不要重试。\n", entry.Name, entry.Summary)
			case exposure.petitionSpent(entry.Name):
				fmt.Fprintf(b, "- %s — %s 本轮请愿机会已用完，不要调用。\n", entry.Name, entry.Summary)
			case exposure.Unavailable[entry.Name] != "":
				fmt.Fprintf(b, "- %s — %s %s\n", entry.Name, entry.Summary, exposure.Unavailable[entry.Name])
			default:
				fmt.Fprintf(b, "- %s — %s 可请愿，不在当前函数列表里。\n", entry.Name, entry.Summary)
			}
		}
	}
	writeDiscoveryCatalogRows(b, entries, exposure)
	// Ambient and host-issued grants (memory_recall, a delivered image) are
	// already callable. Leaving them out of this section makes the directory
	// look like the whole menu. They stay out of the petition set. A catalog
	// row already states that name, so the extra writers must not print it again.
	listed := entriesAlreadyInDirectory(entries, exposure.Catalog)
	writeGrantedNamesOutsideDirectory(b, listed, exposure.Granted)
	writePlannedInstalledNames(b, listed, exposure)
	b.WriteString(semanticTurnDirectoryClose)
	b.WriteString("\n")
}

// entriesAlreadyInDirectory marks builtin rows and discovery catalog rows as
// already printed. The grant and plan extras then skip those names.
func entriesAlreadyInDirectory(entries, catalog []semanticTurnDirectoryEntry) []semanticTurnDirectoryEntry {
	if len(catalog) == 0 {
		return entries
	}
	listed := make([]semanticTurnDirectoryEntry, 0, len(entries)+len(catalog))
	listed = append(listed, entries...)
	return append(listed, catalog...)
}

// semanticStableRenderedName is the model spelling that does not depend on a
// grant token. Installed MCP keeps server__tool. An opaque dynamic adapter
// stays empty until its grant is bound.
func semanticStableRenderedName(adapter string) string {
	return strings.TrimSpace(tool.RenderedSemanticFunctionName(adapter, ""))
}

func writePlannedInstalledNames(b *strings.Builder, entries []semanticTurnDirectoryEntry, exposure semanticDirectoryExposure) {
	if b == nil || len(exposure.Planned) == 0 {
		return
	}
	listed := map[string]bool{}
	for _, entry := range entries {
		listed[entry.Name] = true
	}
	extra := make([]string, 0, len(exposure.Planned))
	status := make(map[string]string, len(exposure.Planned))
	for name := range exposure.Planned {
		name = strings.TrimSpace(name)
		if !semanticInstalledPromptName(name) || listed[name] || exposure.Granted[name] {
			continue
		}
		switch {
		case exposure.AwaitingConfirm[name]:
			status[name] = "等待用户确认，不要再次调用，也不要用其它工具完成该动作。"
		case exposure.AwaitingReceipt[name]:
			status[name] = "结果未定，不要再次调用。"
		case exposure.Retired[name]:
			// tools_search keeps this spelling callable after the managed
			// one-shot moves to retired. "不要调用" would contradict that.
			status[name] = "已安装的 MCP 工具，继续用这个名字调用。"
		default:
			// No managed grant and no sidecar grant. The name is not in this
			// request's function list. tools_search says the same.
			status[name] = "已列入本轮计划，不要直接调用，也不要请愿。"
		}
		extra = append(extra, name)
	}
	sort.Strings(extra)
	for _, name := range extra {
		fmt.Fprintf(b, "- %s — %s %s\n", name, semanticDirectorySummary(name), status[name])
	}
}

// semanticInstalledPromptName is the server__tool spelling, not the
// installed-mcp adapter that contains that spelling.
func semanticInstalledPromptName(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" || strings.Contains(name, ":") || !strings.Contains(name, "__") {
		return false
	}
	return semanticStableRenderedName("installed-mcp:"+name+":dynamic_mcp_") == name
}

func writeGrantedNamesOutsideDirectory(b *strings.Builder, entries []semanticTurnDirectoryEntry, granted map[string]bool) {
	if b == nil || len(granted) == 0 {
		return
	}
	listed := map[string]bool{}
	for _, entry := range entries {
		listed[entry.Name] = true
	}
	extra := make([]string, 0, len(granted))
	for name := range granted {
		name = strings.TrimSpace(name)
		if name == "" || name == semanticToolsSearchName || listed[name] {
			continue
		}
		extra = append(extra, name)
	}
	sort.Strings(extra)
	for _, name := range extra {
		fmt.Fprintf(b, "- %s — %s 已在当前函数列表里。\n", name, semanticDirectorySummary(name))
	}
}

// petitionSpent reports that this name's petition class already used its one
// turn. Read-only and effectful names have separate budgets, matching
// tools_search. A name outside the petition map is left unchanged.
func semanticDirectoryWaitNotes(c *sharedAgentLoopCallbacks) map[string]string {
	if c == nil || c.semanticSurface == nil {
		return nil
	}
	notes := map[string]string{}
	for name, capability := range semanticPetitionableCapabilities {
		if _, live := c.semanticSurface.grants[name]; live {
			continue
		}
		if !semanticSurfacePlansTool(c.semanticSurface, name) {
			continue
		}
		if note := semanticPlannedWaitNote(c, name, capability); note != "" {
			notes[name] = note
		}
	}
	if len(notes) == 0 {
		return nil
	}
	return notes
}

// semanticPlannedWaitNote names the predecessor a planned tool is waiting on.
// An empty note keeps the generic "already planned" sentence.
func semanticPlannedWaitNote(c *sharedAgentLoopCallbacks, name string, capability tool.CapabilityID) string {
	if c == nil || capability == "" {
		return ""
	}
	switch c.plannedSuccessorWait(capability) {
	case successorWaitHeld:
		return "已列入本轮计划。本响应的其它工具调用结束后，下一请求会出现。不要直接调用，也不要请愿。"
	case successorWaitImmediate:
		now := c.predecessorNamesFor(capability).callNow
		if len(now) == 0 {
			return ""
		}
		return "已列入本轮计划，先调用 " + strings.Join(now, "、") + "。成功后下一请求会出现。不要直接调用，也不要请愿。"
	case successorWaitLater:
		names := c.predecessorNamesFor(capability)
		switch {
		case len(names.callNow) > 0 && len(names.nextRequest) > 0:
			return "已列入本轮计划，先调用 " + strings.Join(names.callNow, "、") + "。之后会出现 " + strings.Join(names.nextRequest, "、") + "。不要直接调用，也不要请愿。"
		case len(names.callNow) > 0:
			return "已列入本轮计划，先调用 " + strings.Join(names.callNow, "、") + "。不要直接调用，也不要请愿。"
		case len(names.nextRequest) > 0:
			return "已列入本轮计划，先等 " + strings.Join(names.nextRequest, "、") + "。不要直接调用，也不要请愿。"
		default:
			return ""
		}
	default:
		return ""
	}
}

func (exposure semanticDirectoryExposure) petitionSpent(name string) bool {
	if _, ok := semanticPetitionableCapabilities[name]; !ok {
		return false
	}
	if semanticPetitionIsEffectful(name) {
		return exposure.EffectfulPetitionSpent
	}
	return exposure.ReadPetitionSpent
}

func semanticSurfaceRetiredNames(surface *semanticCallSurface) map[string]bool {
	if surface == nil {
		return nil
	}
	names := make(map[string]bool, len(surface.retiredGrants))
	for name, grant := range surface.retiredGrants {
		addSemanticDirectoryNames(names, name, grant)
	}
	return names
}

func semanticSurfaceAwaitingNames(surface *semanticCallSurface) (confirm, receipt map[string]bool) {
	if surface == nil {
		return nil, nil
	}
	for name, grant := range surface.retiredGrants {
		awaiting, reason := semanticRetiredAwaitingReceipt(surface, grant.SelectionID)
		if !awaiting {
			continue
		}
		if mailboxDraftAwaitingConfirmation(reason) {
			if confirm == nil {
				confirm = map[string]bool{}
			}
			addSemanticDirectoryNames(confirm, name, grant)
			continue
		}
		if receipt == nil {
			receipt = map[string]bool{}
		}
		addSemanticDirectoryNames(receipt, name, grant)
	}
	return confirm, receipt
}

// addSemanticDirectoryNames records the model spelling. A retired grant can
// be keyed by the host adapter (host_document_generate_file) while the
// directory row is generate_pdf.
func addSemanticDirectoryNames(names map[string]bool, name string, grant tool.InvocationGrant) {
	if names == nil {
		return
	}
	for _, candidate := range semanticGrantNameCandidates(name, grant) {
		if candidate != "" {
			names[candidate] = true
		}
	}
}

// semanticGrantNameCandidates is the model spellings of one grant. The table
// key and AdapterName may each be the host adapter or the stable name.
func semanticGrantNameCandidates(key string, grant tool.InvocationGrant) []string {
	return []string{
		strings.TrimSpace(key),
		semanticStableRenderedName(key),
		strings.TrimSpace(grant.AdapterName),
		semanticStableRenderedName(grant.AdapterName),
	}
}

// semanticRetiredGrantForName returns the retired grant the model names.
// An exact table key wins. Otherwise the directory candidates match, so a
// grant stored as host_document_generate_file answers generate_pdf.
func semanticRetiredGrantForName(surface *semanticCallSurface, name string) (tool.InvocationGrant, bool) {
	if surface == nil {
		return tool.InvocationGrant{}, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return tool.InvocationGrant{}, false
	}
	if grant, ok := surface.retiredGrants[name]; ok {
		return grant, true
	}
	for key, grant := range surface.retiredGrants {
		for _, candidate := range semanticGrantNameCandidates(key, grant) {
			if candidate == name {
				return grant, true
			}
		}
	}
	return tool.InvocationGrant{}, false
}

func semanticSurfaceGrantNames(surface *semanticCallSurface) map[string]bool {
	if surface == nil {
		return nil
	}
	names := make(map[string]bool, len(surface.grants))
	for name := range surface.grants {
		name = strings.TrimSpace(name)
		if name != "" {
			names[name] = true
		}
	}
	return names
}

// projectSemanticDirectoryPrompt rewrites the directory from the published
// classification and the grants that are actually listed. A petition used to
// leave the old sentence "不在当前函数列表里" on a name the next request renders.
func (c *sharedAgentLoopCallbacks) projectSemanticDirectoryPrompt() {
	if c == nil || strings.TrimSpace(c.systemPrompt) == "" || c.semanticSurface == nil || c.semanticSurface.replan == nil || c.semanticSurface.replan.Classification.Primary == "" {
		return
	}
	// semanticDirectoryExposure already filled Catalog from the same
	// observation that marked granted names. A second lookup here can see
	// a server finish tools/list and print a different set.
	exposure := c.semanticDirectoryExposure()
	c.systemPrompt = semanticPromptWithTurnDirectory(c.systemPrompt, c.semanticSurface.replan.Classification, exposure)
}

// writeDiscoveryCatalogRows appends installed skills and ready MCP tools.
// A name the builtin directory already lists is not repeated.
func writeDiscoveryCatalogRows(b *strings.Builder, entries []semanticTurnDirectoryEntry, exposure semanticDirectoryExposure) {
	if b == nil || len(exposure.Catalog) == 0 {
		return
	}
	listed := map[string]bool{}
	for _, entry := range entries {
		listed[entry.Name] = true
	}
	for _, entry := range exposure.Catalog {
		name := strings.TrimSpace(entry.Name)
		if name == "" || listed[name] {
			continue
		}
		listed[name] = true
		summary := directorySingleLine(entry.Summary)
		if summary == "" {
			summary = semanticDirectorySummary(name)
		}
		if exposure.Granted[name] {
			fmt.Fprintf(b, "- %s — %s 已在当前函数列表里。\n", name, summary)
			continue
		}
		fmt.Fprintf(b, "- %s — %s 可请愿，不在当前函数列表里。\n", name, summary)
	}
}

func (c *sharedAgentLoopCallbacks) semanticDirectoryExposure() semanticDirectoryExposure {
	if c == nil {
		return semanticDirectoryExposure{}
	}
	exposure := semanticSurfaceExposure(c.semanticSurface)
	// server__tool is listed only when this request will actually send that
	// definition. A managed or sidecar grant left behind after a failed
	// render, a spent call, or an authorizer drop is not a callable name.
	// Unplanned sidecar tools stay out of this section.
	visible := semanticVisibleToolNames(c.tools)
	if c.discoveredMCPSurface != nil && len(exposure.Planned) > 0 {
		if exposure.Granted == nil {
			exposure.Granted = map[string]bool{}
		}
		for name := range c.discoveredMCPSurface.grants {
			name = strings.TrimSpace(name)
			if name == "" || !exposure.Planned[name] || !semanticInstalledToolCallable(c, name, visible) {
				continue
			}
			exposure.Granted[name] = true
		}
	}
	for name := range exposure.Granted {
		if semanticInstalledPromptName(name) && !semanticInstalledToolCallable(c, name, visible) {
			delete(exposure.Granted, name)
		}
	}
	c.noteVisibleDiscoveryCatalog(&exposure, visible)
	exposure.ReadPetitionSpent = c.semanticPetitionConsumed
	exposure.EffectfulPetitionSpent = c.semanticEffectfulPetitionConsumed
	exposure.Wait = semanticDirectoryWaitNotes(c)
	if c.semanticKnowledgeBaseUnavailable() {
		exposure.Unavailable = map[string]string{
			"knowledge_search": "本机知识库为空或未配置，请愿它也没有结果。",
		}
	}
	if len(c.semanticPetitionDenied) == 0 {
		return exposure
	}
	exposure.Denied = make(map[string]bool, len(c.semanticPetitionDenied))
	for name := range c.semanticPetitionDenied {
		exposure.Denied[strings.TrimSpace(name)] = true
	}
	return exposure
}

// noteVisibleDiscoveryCatalog marks a petitioned skill or MCP name as
// already on the function list once this request actually sends it.
// A sidecar grant that is not in the visible list stays a petition row.
// Catalog is that same observation, so the printed rows cannot be a
// later tools/list than the granted marks.
func (c *sharedAgentLoopCallbacks) noteVisibleDiscoveryCatalog(exposure *semanticDirectoryExposure, visible map[string]bool) {
	if c == nil || exposure == nil || !semanticSurfaceIsModelDiscovery(c.semanticSurface) {
		return
	}
	if exposure.Granted == nil {
		exposure.Granted = map[string]bool{}
	}
	rows := c.discoveryCatalogRows()
	exposure.Catalog = rows
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if name == "" || !visible[name] || !semanticInstalledToolCallable(c, name, visible) {
			continue
		}
		exposure.Granted[name] = true
	}
}

func semanticInstalledToolCallable(c *sharedAgentLoopCallbacks, name string, visible map[string]bool) bool {
	if c == nil || !visible[name] || c.managedInstalledMCPSpentThisRequest(name) {
		return false
	}
	return c.IsToolAllowed(name)
}

func semanticVisibleToolNames(defs []map[string]interface{}) map[string]bool {
	names := make(map[string]bool, len(defs))
	for _, def := range defs {
		if name := strings.TrimSpace(extractToolName(def)); name != "" {
			names[name] = true
		}
	}
	return names
}

func semanticSurfacePlanNames(surface *semanticCallSurface) map[string]bool {
	if surface == nil {
		return nil
	}
	names := make(map[string]bool)
	for _, selection := range surface.plan.Selections {
		adapter := strings.TrimSpace(selection.AdapterName)
		if adapter == "" {
			continue
		}
		model := semanticStableRenderedName(adapter)
		if model != "" && model != adapter {
			names[model] = true
			continue
		}
		names[adapter] = true
	}
	return names
}

// semanticPromptWithTurnDirectory projects result into an already built
// system prompt. Entry builds that prompt before a late tree verdict can
// replace a timeout, so the first model request has to take the directory
// from the classification that actually minted the surface. A light prompt
// has no directory section; it is inserted ahead of the grant fence so the
// page-read rule stays last.
func semanticPromptWithTurnDirectory(prompt string, result intent.ClassificationResult, exposure semanticDirectoryExposure) string {
	var section strings.Builder
	writeSemanticTurnDirectory(&section, result, exposure)
	body := strings.Trim(section.String(), "\n")
	if body == "" {
		return prompt
	}
	if start := strings.Index(prompt, semanticTurnDirectoryHeading); start >= 0 {
		// The discovery preface sits between the heading and the bullets.
		// It stays inside the section. The next non-bullet, or the closing
		// line, is the end, so a later copy of the marker cannot pull the
		// grant fence into the replacement.
		end := directorySectionEnd(prompt, start)
		return prompt[:start] + body + "\n" + prompt[end:]
	}
	const fence = "Governed tools:"
	if at := strings.Index(prompt, fence); at >= 0 {
		return prompt[:at] + body + "\n" + prompt[at:]
	}
	return prompt + "\n" + body + "\n"
}

// directorySingleLine keeps one directory row on one line. A newline in a
// tool description would otherwise put the status on a following line.
func directorySingleLine(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// directorySectionEnd is the index just after the directory that begins at
// start. The closing marker ends the section only as its own line. The
// discovery preface is part of the section; any other non-bullet ends it.
// A later copy of the marker must not pull the grant fence in with it.
func directorySectionEnd(prompt string, start int) int {
	end := start + len(semanticTurnDirectoryHeading)
	if end > len(prompt) {
		return len(prompt)
	}
	for end < len(prompt) {
		line, next := directoryLineAt(prompt, end)
		line = strings.TrimRight(line, "\r")
		if line == semanticTurnDirectoryClose {
			if next < 0 {
				return len(prompt)
			}
			return next
		}
		if !directoryLineStaysInside(line) {
			return end
		}
		if next < 0 {
			return len(prompt)
		}
		end = next
	}
	return end
}

func directoryLineStaysInside(line string) bool {
	if line == "" || strings.HasPrefix(line, "- ") {
		return true
	}
	return line == strings.TrimSuffix(semanticDiscoveryDirectoryPreface, "\n")
}

func directoryLineAt(prompt string, at int) (line string, next int) {
	rest := prompt[at:]
	if i := strings.IndexByte(rest, '\n'); i >= 0 {
		return rest[:i], at + i + 1
	}
	return rest, -1
}

// semanticToolsSearchApplyDirectory pins the turn directory at the front of
// a tools_search page and drops names the directory closed. Alphabetical
// order no longer hides a planned neighbor behind database_query.
func semanticToolsSearchApplyDirectory(entries []semanticToolsSearchEntry, result intent.ClassificationResult) []semanticToolsSearchEntry {
	directory := semanticTurnDirectory(result)
	if len(directory) == 0 {
		return entries
	}
	byName := make(map[string]semanticToolsSearchEntry, len(entries))
	for _, entry := range entries {
		byName[entry.name] = entry
	}
	pinned := make([]semanticToolsSearchEntry, 0, len(directory))
	seen := map[string]bool{}
	for _, item := range directory {
		entry, ok := byName[item.Name]
		if !ok {
			entry = semanticToolsSearchEntry{name: item.Name, summary: item.Summary}
		}
		pinned = append(pinned, entry)
		seen[item.Name] = true
	}
	rest := entries
	for _, entry := range rest {
		if seen[entry.name] {
			continue
		}
		pinned = append(pinned, entry)
	}
	return pinned
}
