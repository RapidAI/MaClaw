package guiapp

import (
	"errors"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticCapabilityExtension reports that a managed-plan failure is a missing
// built-in, not a safety refusal. The turn stays closed: the model may search
// and install a skill, or craft a one-off program, and nothing else is
// published. Policy denials, stale documents, ambiguous native documents, the
// workflow entry, and catalog-not-ready failures stay rejected.
func semanticCapabilityExtension(err error, userText string, attachments []MessageAttachment) (reason string, paths []string, ok bool) {
	if err == nil || errors.Is(err, errSemanticSessionCeilingSpent) || errors.Is(err, errSemanticAwaitingConfirmation) {
		return "", nil, false
	}
	if semanticCapabilityExtensionSafety(err) {
		return "", nil, false
	}
	if tool.IsTrustedInputMissingOrAmbiguous(err) {
		paths = semanticUnsupportedSelectedPaths(userText, attachments)
		if len(paths) == 0 {
			return "", nil, false
		}
		return "内置阅读器不支持这些文件", paths, true
	}
	var unmapped semanticUnmappedCapabilityError
	if errors.As(err, &unmapped) {
		return "内置工具没有「" + string(unmapped.Label) + "」这项能力", nil, true
	}
	if strings.Contains(err.Error(), "no governed capability needs") {
		reason = "内置工具没有覆盖这项请求"
		if strings.Contains(err.Error(), intent.CatalogGapReasonMark) {
			reason += "。不要用幻灯片、Word 或 PDF 顶替。用户要的是文字就直接回答；用户要的是内置工具做不出的文件时，再搜索 Skill，最后才编写程序，产物必须是用户要的那种文件"
		}
		return reason, nil, true
	}
	var unmet semanticUnmetNeedsError
	if errors.As(err, &unmet) {
		if semanticUnmetOnlyBudget(unmet.Unmet) {
			return "", nil, false
		}
		return "内置工具没有覆盖这项请求", nil, true
	}
	return "", nil, false
}

// semanticCapabilityExtensionSafety is the refusal set that must not grow a
// skill or a crafted program. A missing built-in is not in this set.
func semanticCapabilityExtensionSafety(err error) bool {
	if semanticTrustedDocumentContextError(err) || semanticProducedDocumentError(err) {
		return true
	}
	if errors.Is(err, errSemanticGenerateDeliveryConflict) {
		return true
	}
	if semanticUnmetHasReason(err, "policy_denied") || semanticUnmetHasReason(err, "artifact_dependency_missing") {
		return true
	}
	var unmapped semanticUnmappedCapabilityError
	if errors.As(err, &unmapped) && unmapped.Label == intent.LabelWorkflowTask {
		return true
	}
	var pinned semanticPinnedProviderUnavailableError
	if errors.As(err, &pinned) {
		return true
	}
	if semanticUnmetHasReason(err, tool.UnmetReasonPinnedProviderUnavailable) {
		return true
	}
	var unmet semanticUnmetNeedsError
	if errors.As(err, &unmet) && semanticUnmetOnlyReasons(unmet.Unmet,
		tool.CatalogCoverageReasonNotReady,
		tool.CatalogCoverageReasonIncomplete,
		tool.CatalogCoverageReasonStale) {
		return true
	}
	return false
}

// semanticUnsupportedSelectedPaths is the current selection when every file
// is outside the native document reader. A native document in the same
// selection stays on the document plan. An image is not an unsupported book.
func semanticUnsupportedSelectedPaths(userText string, attachments []MessageAttachment) []string {
	paths := agent.SelectedLocalFilePathsFromPrompt(userText)
	if len(paths) > 0 {
		unsupported := make([]string, 0, len(paths))
		for _, path := range paths {
			if agent.IsDocumentFilePath(path) || agent.IsImageFilePath(path) {
				continue
			}
			unsupported = append(unsupported, path)
		}
		if len(unsupported) == 0 || len(unsupported) != len(paths) {
			return nil
		}
		return unsupported
	}
	var names []string
	sawNative := false
	for _, attachment := range attachments {
		name := strings.TrimSpace(attachment.FileName)
		if attachment.Type == "image" || agent.IsImageFilePath(name) {
			continue
		}
		if name == "" {
			continue
		}
		if _, _, ok := agent.DocumentAttachmentFormat(name, attachment.MimeType); ok || agent.IsDocumentFilePath(name) {
			sawNative = true
			continue
		}
		names = append(names, name)
	}
	if sawNative || len(names) == 0 {
		return nil
	}
	return names
}

func semanticSurfaceIsModelDiscovery(surface *semanticCallSurface) bool {
	return surface != nil && surface.replan != nil && intent.ClassificationIsModelDiscovery(surface.replan.Classification)
}

// modelDiscoveryHidesGrant is the clock stand-in kept on the plan so a
// discovery turn can be published. It is not a function the model can call,
// and tools_search must not describe it as the settled surface.
func modelDiscoveryHidesGrant(name string) bool {
	return strings.TrimSpace(name) == "current_datetime"
}

func modelDiscoveryExtensionName(name string) bool {
	switch strings.TrimSpace(name) {
	case "search_and_install_skill", "craft_tool":
		return true
	default:
		return false
	}
}

// discoveryUnmatchedExtensionMessage is the host rejection while a discovery
// catalog miss has not yet run search_and_install_skill (and craft_tool, when
// that search installs nothing). selection_not_authorized is the wrong text:
// it tells the model to answer from evidence it does not have, which is how
// the picture turn dispatched a download.
const discoveryUnmatchedExtensionMessage = "所查的名字不在目录里。先调用 search_and_install_skill，query 用用户的原话；没有可用 Skill 时再调用 craft_tool。不要派后台任务，也不要用 web_search、download_file、generate_pdf、office 或 browser 顶替。"

// discoveryUnmatchedSubstituteMessage is the same turn after the extension
// path has actually run. Local tools can carry out the program it produced.
// Search, download, and a document renderer are still a different capability.
const discoveryUnmatchedSubstituteMessage = "这次目录没有对得上的能力，搜索和下载不能当作结果。用已经执行的 search_and_install_skill 或 craft_tool 的结果完成。需要在本机落盘时请愿 bash、write_file 或 read_file。不要派后台任务，也不要调用 web_search、download_file、generate_pdf、office 或 browser。"

// discoveryDispatchBeforeProbeMessage refuses dispatch while this assistant
// message still has a tools_search that has not run. The worker is not
// started, and the handoff fence stays open so that search can still record
// a catalog miss.
const discoveryDispatchBeforeProbeMessage = "这一条回复里的 tools_search 还没执行，后台任务没有开始。先看目录结果。对得上再派后台任务；对不上就调用 search_and_install_skill，没有可用 Skill 时再调用 craft_tool。"

// discoverySubstituteTool is a capability that can imitate a catalog miss by
// fetching or rendering something else. bash and write_file are not in this
// set: they are how a crafted program lands a file.
func discoverySubstituteTool(name string) bool {
	switch strings.TrimSpace(name) {
	case "web_search", "web_fetch", "download_file", "generate_pdf", "office", "browser", chatTaskToolDispatch:
		return true
	default:
		return false
	}
}

func (c *sharedAgentLoopCallbacks) discoveryExtensionSettled() bool {
	return c != nil && (c.discoveryCraftRan || c.discoverySkillInstalled)
}

// discoveryPetitionBlocked reports that this discovery turn's catalog miss
// has not authorized name. Before the extension path runs, every other name
// is closed. After it runs, only the substitute set stays closed.
func (c *sharedAgentLoopCallbacks) discoveryPetitionBlocked(name string) (bool, string) {
	if c == nil || !c.discoveryUnmatchedObligation {
		return false, ""
	}
	name = strings.TrimSpace(name)
	if name == "" || name == semanticToolsSearchName || modelDiscoveryExtensionName(name) {
		return false, ""
	}
	if !c.discoveryExtensionSettled() {
		return true, discoveryUnmatchedExtensionMessage
	}
	if discoverySubstituteTool(name) {
		return true, discoveryUnmatchedSubstituteMessage
	}
	return false, ""
}

// discoverySubstitutionBlocks is a needs list whose every name the miss gate
// refuses. The page must stay on the extension step. A mixed list that still
// contains a local tool is loaded name by name.
func (c *sharedAgentLoopCallbacks) discoverySubstitutionBlocks(names map[string]bool) bool {
	if c == nil || !c.discoveryUnmatchedObligation || len(names) == 0 {
		return false
	}
	for name := range names {
		if blocked, _ := c.discoveryPetitionBlocked(name); !blocked {
			return false
		}
	}
	return true
}

func (c *sharedAgentLoopCallbacks) noteDiscoveryCatalogMiss() {
	if c == nil || !semanticSurfaceIsModelDiscovery(c.semanticSurface) {
		return
	}
	// Freeze the acquisition list once. A fetch that already succeeded under
	// a matched needs list stays on the card. A fetch after this point is
	// the substitution the miss is closing.
	if !c.discoveryUnmatchedObligation {
		c.discoveryMissAcquisitionBase = len(c.remoteAcquisitionPaths)
	}
	c.discoveryUnmatchedObligation = true
	if c.loopCtx != nil {
		c.loopCtx.setDiscoveryCatalogMiss()
		if c.discoveryExtensionSettled() {
			c.loopCtx.markDiscoveryCatalogMissSettled()
		}
	}
}

func discoveryExtensionActuallyRan(result string) bool {
	result = strings.TrimSpace(result)
	if result == "" || result == "handler unavailable" {
		return false
	}
	if strings.Contains(result, "selection_not_authorized") || strings.HasPrefix(result, "[system rejected]") {
		return false
	}
	return true
}

// noteDiscoveryExtensionHost records an extension the executor actually
// entered. ok is the host outcome. Skill prose is not that bit: an import
// or download failure still returns a sentence, and treating every sentence
// that is not on the miss list as an install opened local tools while
// craft_tool was still required.
func (c *sharedAgentLoopCallbacks) noteDiscoveryExtensionHost(name, result string, ok bool) {
	if c == nil || !discoveryExtensionActuallyRan(result) {
		return
	}
	switch strings.TrimSpace(name) {
	case "craft_tool":
		if ok {
			c.discoveryCraftRan = true
		}
	case "search_and_install_skill":
		c.discoverySkillSearchRan = true
		if ok {
			c.discoverySkillInstalled = true
		}
	}
	c.noteDiscoveryMissSettled()
}

func (c *sharedAgentLoopCallbacks) noteDiscoveryMissSettled() {
	if c == nil || c.loopCtx == nil || !c.discoveryExtensionSettled() {
		return
	}
	c.loopCtx.markDiscoveryCatalogMissSettled()
}

func (c *sharedAgentLoopCallbacks) discoveryDispatchRejection(name string) string {
	if strings.TrimSpace(name) != chatTaskToolDispatch {
		return ""
	}
	// A miss already on the books uses that sentence. The probe hold only
	// covers the window before this batch's tools_search has executed.
	if c != nil && c.loopCtx != nil && c.loopCtx.discoveryBatchPendingSearchCount() > 0 && !c.discoveryUnmatchedObligation {
		return "[system rejected] " + discoveryDispatchBeforeProbeMessage
	}
	blocked, message := c.discoveryPetitionBlocked(name)
	if !blocked || strings.TrimSpace(message) == "" {
		return ""
	}
	return "[system rejected] " + message
}

// modelDiscoveryGrantPromptMark keeps the discovery fence idempotent. The
// directory sentence also says the turn did not preselect a tool, so the
// mark has to be a string that sentence does not contain.
const modelDiscoveryGrantPromptMark = "【发现模式】"

// modelDiscoveryGrantPromptFence is the contract for a turn that skipped the
// intent classifier. The ordinary grant fence names office as an unlisted
// name to call, which is how a picture request became a deck: every
// directory row is petitionable, and that sentence tells the model to pick
// one. This fence keeps the safety lines and refuses that substitution.
// A directory name is loaded by tools_search needs. The provider cannot
// emit a function that is absent from this request.
const modelDiscoveryGrantPromptFence = "\n【发现模式】预置函数是 tools_search、search_and_install_skill 和 craft_tool。目录里标了「可请愿」的名字，就是用户要的那种能力时，调用 tools_search 并把那个名字放进 needs。当前这一请求的函数列表里没有的名字不能调用，加载成功后它们出现在下一请求。一次 needs 可以写多个工作区工具。需要 bash、write_file、office 或 generate_pdf 时，把那个名字放进 needs。不要调用 office、generate_pdf、write_file 或 bash 来顶替另一个已经对得上的目录名字。没有哪种名字能做出用户要的文件时，先调用 search_and_install_skill，query 用用户的原话，再用 craft_tool。文字问题直接回答，不要为此做文件。标了等待用户确认的名字不要调用，也不要改用别的工具完成。不要发明 previous_turn_tool。auto_extract 的 error_class 若是 encrypted、malformed、source_changed、input_too_large 或 output_too_large，该文件已关闭，不要用 bash、pip、python 或其它 OCR 处理它。\n"

func ensureTurnGrantPromptFence(prompt string, result intent.ClassificationResult) string {
	if intent.ClassificationIsModelDiscovery(result) {
		return ensureModelDiscoveryGrantPromptFence(prompt)
	}
	return ensureSemanticGrantPromptFence(prompt)
}

func ensureModelDiscoveryGrantPromptFence(prompt string) string {
	if strings.Contains(prompt, modelDiscoveryGrantPromptMark) {
		return prompt
	}
	return prompt + modelDiscoveryGrantPromptFence
}

func turnGrantClassification(surface *semanticCallSurface) intent.ClassificationResult {
	if surface == nil || surface.replan == nil {
		return intent.ClassificationResult{}
	}
	return surface.replan.Classification
}

// modelDiscoveryVisibleTools is what the chat model may call before it
// petitions. The clock grant stays on the surface so the plan can be
// published, and stays off this list. Skill search and craft_tool are the
// last resort when the directory has no name for the request.
func modelDiscoveryVisibleTools(h *IMMessageHandler, grants []map[string]interface{}) []map[string]interface{} {
	kept := make([]map[string]interface{}, 0, len(grants)+3)
	for _, def := range grants {
		if modelDiscoveryHidesGrant(extractToolName(def)) {
			continue
		}
		kept = append(kept, def)
	}
	extra := append([]map[string]interface{}{semanticToolsSearchDefinition()}, capabilityExtensionToolDefs(h)...)
	return appendUniqueToolDefs(kept, extra)
}

func capabilityExtensionToolDefs(h *IMMessageHandler) []map[string]interface{} {
	if h == nil || h.registry == nil {
		return nil
	}
	// Discovery reads the registry directly. The legacy tool cache is the
	// only other place that used to register skill search, and a discovery
	// turn never rebuilds that cache, so the preset was craft_tool alone.
	if h.app != nil {
		h.syncSkillHubTools()
	}
	out := make([]map[string]interface{}, 0, 2)
	for _, name := range []string{"search_and_install_skill", "craft_tool"} {
		registered, ok := h.registry.Get(name)
		if !ok || registered == nil || registered.Status != RegToolAvailable {
			continue
		}
		out = append(out, registeredToolToDef(*registered))
	}
	return out
}

func capabilityExtensionPrompt(reason string, paths []string, tools []map[string]interface{}) string {
	have := map[string]bool{}
	for _, def := range tools {
		have[extractToolName(def)] = true
	}
	var b strings.Builder
	b.WriteString("## 内置能力未覆盖\n")
	if strings.Contains(reason, "不要用幻灯片、Word 或 PDF 顶替") {
		b.WriteString("最接近的内置工具做不了这件事。不要改用幻灯片、Word 或 PDF 交差。\n")
		b.WriteString("用户要文字内容时直接回答，不要为此做文件。\n")
		b.WriteString("用户要一种内置工具做不出的文件时，按下面的顺序。产物必须是用户要的那种文件。\n")
	} else {
		b.WriteString("这一轮的内置工具不能直接完成请求。不要拒绝，也不要让用户改格式或换软件。\n")
	}
	step := 1
	catalogGap := strings.Contains(reason, "不要用幻灯片、Word 或 PDF 顶替")
	if have["search_and_install_skill"] {
		if catalogGap {
			fmt.Fprintf(&b, "%d. 用户要一种内置工具做不出的文件时，调用 search_and_install_skill，query 用用户的原话。文字请求不要调用它。\n", step)
		} else {
			fmt.Fprintf(&b, "%d. 先调用 search_and_install_skill，query 用用户的原话，搜索并安装能完成这件事的 Skill。\n", step)
		}
		step++
	}
	if have["craft_tool"] {
		if catalogGap {
			fmt.Fprintf(&b, "%d. 没有可用 Skill，或 Skill 明确失败时，调用 craft_tool。产物必须是用户要的那种文件，不要生成 pptx、docx 或 pdf 来顶替。\n", step)
		} else {
			fmt.Fprintf(&b, "%d. 没有可用 Skill，或 Skill 明确失败时，调用 craft_tool 编写一次性程序完成。task 里写清目标；如果下面列了文件，写上完整路径。\n", step)
		}
	}
	b.WriteString("不要用脚本绕过 encrypted、malformed、source_changed、input_too_large、output_too_large。\n")
	if strings.TrimSpace(reason) != "" {
		b.WriteString("原因：")
		b.WriteString(reason)
		b.WriteString("\n")
	}
	if len(paths) > 0 {
		b.WriteString("已选文件：\n")
		for _, path := range paths {
			b.WriteString("- ")
			b.WriteString(path)
			b.WriteString("\n")
		}
	}
	return strings.TrimSpace(b.String())
}
