package guiapp

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

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

// discoveryPictureCreationUtterance is a request to create a picture. The
// directory has no image tool. Search, download, and a document renderer are
// not that tool, and loading one of them used to close the catalog miss.
// A PDF, a diagram, a screenshot, and a request to find or download an
// existing picture keep the tools those tasks already use.
func discoveryPictureCreationUtterance(text string) bool {
	text = strings.TrimSpace(text)
	if text == "" || discoveryPictureCreationExcluded(text) || discoveryPictureIsDiagram(text) {
		return false
	}
	// 不画图 / 不要文生图 name the verb and cancel it. A later affirmed
	// phrase in the same sentence still counts.
	if discoveryHasAffirmed(text, "文生图") || discoveryHasAffirmed(text, "画图") {
		return true
	}
	for _, phrase := range []string{"画一张", "画一幅", "画一只"} {
		if discoveryHasAffirmed(text, phrase) {
			return true
		}
	}
	if discoveryLooseDrawIsPicture(text) {
		return true
	}
	folded := strings.ToLower(text)
	if discoveryAffirmedFoldedPhrase(folded, "text-to-image") || discoveryAffirmedFoldedPhrase(folded, "text to image") {
		return true
	}
	if discoveryEnglishPictureCreation(folded) {
		return true
	}
	return discoveryGeneratedPictureObject(text)
}

func discoveryPictureCreationExcluded(text string) bool {
	folded := strings.ToLower(text)
	// ASCII format names are whole words. "word" inside "password" and
	// "excel" inside "excellent" are not a document request.
	for _, word := range []string{
		"pdf", "ppt", "pptx", "word", "excel", "powerpoint",
		"download", "screenshot",
	} {
		if discoveryASCIIWord(folded, word) {
			return true
		}
	}
	for _, word := range []string{
		"文档", "报告", "幻灯", "表格", "演示文稿",
		"下载",
		"截图", "截屏", "截一",
		"找一张", "找一幅", "找个图", "搜一张", "搜索图片", "寻找", "查找图片", "现成",
		"find a picture", "find an image", "search for a picture", "search for an image",
	} {
		if strings.Contains(folded, word) {
			return true
		}
	}
	return false
}

func discoveryPictureIsDiagram(text string) bool {
	for _, word := range []string{"流程图", "架构图", "示意图", "思维导图", "框图", "时序图", "拓扑图", "结构图", "类图"} {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func discoveryEnglishPictureCreation(folded string) bool {
	if !discoveryEnglishPictureNoun(folded) {
		return false
	}
	from := 0
	for from < len(folded) {
		verbAt, verbLen := discoveryNextEnglishPictureVerb(folded[from:])
		if verbAt < 0 {
			return false
		}
		abs := from + verbAt
		// A negated verb does not cancel a later affirmed one:
		// "do not generate a table, generate an image".
		if !discoveryEnglishVerbNegated(folded[:abs]) {
			return true
		}
		from = abs + verbLen
	}
	return false
}

func discoveryEnglishPictureNoun(folded string) bool {
	for _, noun := range []string{"image", "picture", "illustration"} {
		if discoveryASCIIWord(folded, noun) {
			return true
		}
	}
	return false
}

func discoveryNextEnglishPictureVerb(s string) (int, int) {
	best, bestLen := -1, 0
	if i := strings.Index(s, "generat"); i >= 0 {
		best, bestLen = i, len("generat")
	}
	for _, word := range []string{"draw", "paint"} {
		if i := discoveryASCIIWordIndex(s, word); i >= 0 && (best < 0 || i < best) {
			best, bestLen = i, len(word)
		}
	}
	return best, bestLen
}

func discoveryAffirmedFoldedPhrase(folded, phrase string) bool {
	from := 0
	for from < len(folded) {
		rel := strings.Index(folded[from:], phrase)
		if rel < 0 {
			return false
		}
		at := from + rel
		if !discoveryEnglishVerbNegated(folded[:at]) {
			return true
		}
		from = at + len(phrase)
	}
	return false
}

// discoveryEnglishVerbNegated is "do not / don't / not" immediately before
// the picture verb. "cannot" stays a request; the "not" there is not a word.
func discoveryEnglishVerbNegated(prefix string) bool {
	folded := strings.TrimRight(prefix, " \t")
	for _, neg := range []string{"do not", "don't", "dont", "not"} {
		if !strings.HasSuffix(folded, neg) {
			continue
		}
		at := len(folded) - len(neg)
		if at == 0 || !discoveryASCIILetter(folded[at-1]) {
			return true
		}
	}
	return false
}

func discoveryASCIIWord(folded, word string) bool {
	rest := folded
	for {
		i := strings.Index(rest, word)
		if i < 0 {
			return false
		}
		before := i == 0 || !discoveryASCIILetter(rest[i-1])
		after := i + len(word)
		if before && (after == len(rest) || !discoveryASCIILetter(rest[after])) {
			return true
		}
		base := len(folded) - len(rest)
		rest = folded[base+i+1:]
	}
}

func discoveryASCIIWordIndex(folded, word string) int {
	rest := folded
	for {
		i := strings.Index(rest, word)
		if i < 0 {
			return -1
		}
		at := len(folded) - len(rest) + i
		before := at == 0 || !discoveryASCIILetter(folded[at-1])
		after := at + len(word)
		if before && (after == len(folded) || !discoveryASCIILetter(folded[after])) {
			return at
		}
		rest = folded[at+1:]
	}
}

func discoveryASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}

// discoveryLooseDrawIsPicture is 画个 / 画一个 when that verb's object is not
// a plan. 画个猫 and 画个图 stay. 画一个方案 and 画一个方案图 do not. A plan
// word after a clause break belongs to another clause: 画个猫，方案另说
// still draws. 画 inside 动画 is not this verb.
func discoveryLooseDrawIsPicture(text string) bool {
	for _, phrase := range []string{"画一个", "画个"} {
		if discoveryDrawObjectIsPicture(text, phrase) {
			return true
		}
	}
	return false
}

func discoveryDrawObjectIsPicture(text, phrase string) bool {
	from := 0
	for from < len(text) {
		rel := strings.Index(text[from:], phrase)
		if rel < 0 {
			return false
		}
		at := from + rel
		objFrom := at + len(phrase)
		from = objFrom
		if discoveryVerbNegated(text[:at]) || discoveryAnimatedDraw(text, at) {
			continue
		}
		object := text[objFrom:]
		if i := discoveryClauseBreakIndex(object); i >= 0 {
			object = object[:i]
		}
		if !discoveryDrawPlanWord(object) {
			return true
		}
	}
	return false
}

func discoveryDrawPlanWord(object string) bool {
	for _, word := range []string{"方案", "计划", "清单", "名单", "步骤", "大纲", "原型", "流程"} {
		if strings.Contains(object, word) {
			return true
		}
	}
	return false
}

func discoveryAnimatedDraw(text string, at int) bool {
	prev, size := utf8.DecodeLastRuneInString(text[:at])
	return size > 0 && prev == '动'
}

func discoveryClauseBreakIndex(s string) int {
	return strings.IndexAny(s, "，,。！？!?:：；;\n")
}

// discoveryGeneratedPictureObject is a picture object of 生成.
// After the verb, the nearest created 图 within 16 runes counts, so a later
// 图 does not hide 生成老鼠戏猫图, and a 图 that only shows up after a long
// gap is not a picture. 一张 and 一幅 alone are not enough: 生成一张清单
// still searches. 图片 or 图像 with nothing between counts. A 图 that only
// points at an existing figure (如图, 见图, 图中) does not.
// Before the verb, only the head of the 把/将 phrase that actually introduces
// it counts, and only when 生成 does not already take some other object
// (把图片生成出来, not 参考图片生成说明).
func discoveryGeneratedPictureObject(text string) bool {
	const window = 16
	rest := text
	for len(rest) > 0 {
		i := strings.Index(rest, "生成")
		if i < 0 {
			return false
		}
		at := len(text) - len(rest) + i
		if discoveryVerbNegated(text[:at]) {
			rest = rest[i+len("生成"):]
			continue
		}
		suffix := text[at+len("生成"):]
		if discoveryPictureNounAfter(suffix, window) || discoveryPictureNounBefore(text[:at], suffix, window) {
			return true
		}
		rest = rest[i+len("生成"):]
	}
	return false
}

func discoveryPictureNounAfter(suffix string, window int) bool {
	offset := 0
	for offset < len(suffix) {
		rel := strings.Index(suffix[offset:], "图")
		if rel < 0 {
			return false
		}
		at := offset + rel
		n := utf8.RuneCountInString(suffix[:at])
		if n > window {
			return false
		}
		// 图片的说明 is a modifier. 生成图片交给我 is the object.
		if !discoveryPictureModifies(suffix, at) {
			if n == 0 && (strings.HasPrefix(suffix, "图片") || strings.HasPrefix(suffix, "图像")) {
				return true
			}
			if n > 0 && !discoveryReferencePicture(suffix, at) {
				return true
			}
		}
		offset = at + len("图")
	}
	return false
}

// discoveryPictureModifies is 图/图片/图像 followed by 的. The noun
// modifies whatever comes next, so it is not the object being created.
func discoveryPictureModifies(text string, at int) bool {
	rest := text[at+len("图"):]
	return strings.HasPrefix(rest, "的") || strings.HasPrefix(rest, "片的") || strings.HasPrefix(rest, "像的")
}

// discoveryReferencePicture is a 图 that points at an existing figure.
// 如图 and 见图 are references even at the end of the clause. 图中, 图里,
// 图示, and 图所 are modifiers. 上图 and 下图 are references too: 生成下图
// stays a search. 一下图 is not that 下图: 一下 is the softener.
func discoveryReferencePicture(text string, at int) bool {
	prev, prevSize := utf8.DecodeLastRuneInString(text[:at])
	// 一下图 is the softener plus 图. 下图 without that 一 is the figure below.
	if prev == '下' && prevSize > 0 {
		earlier, _ := utf8.DecodeLastRuneInString(text[:at-prevSize])
		if earlier != '一' {
			return true
		}
	} else {
		switch prev {
		case '如', '见', '上', '左', '右', '附':
			return true
		}
	}
	next, size := utf8.DecodeRuneInString(text[at+len("图"):])
	if size == 0 {
		return false
	}
	switch next {
	case '中', '里', '示', '所':
		return true
	}
	return false
}

// discoveryPictureNounBefore is the preverbal object of 生成.
// The picture noun has to be the head of the 把/将 phrase against the verb
// (把图片生成出来, 将一幅图生成出来, 把这张图生成出来). An earlier 把 in
// another clause does not count, and neither does 将 inside 将来 or 即将.
// The phrase counts only when what follows 生成 is empty or a complement
// such as 出来发给我. 参考图片生成说明 keeps its postverbal object.
func discoveryPictureNounBefore(prefix, suffix string, window int) bool {
	if window < utf8.RuneCountInString("图片") || !discoveryPictureSuffixContinues(suffix) {
		return false
	}
	trimmed := strings.TrimRight(prefix, " \t\r\n，,。！？!?:：；;、")
	nounAt, ok := discoveryPreverbalPictureHead(trimmed)
	if !ok {
		return false
	}
	beforeHead := trimmed[:nounAt]
	marker := discoveryDisposalMarker(beforeHead)
	if marker < 0 {
		return false
	}
	object := beforeHead[marker+len("把"):]
	return !discoveryClauseBreak(object)
}

func discoveryDisposalMarker(beforeHead string) int {
	best := -1
	for _, marker := range []string{"把", "将"} {
		from := 0
		for from < len(beforeHead) {
			rel := strings.Index(beforeHead[from:], marker)
			if rel < 0 {
				break
			}
			at := from + rel
			if marker == "把" || !discoveryLexicalizedJiang(beforeHead, at) {
				if at > best {
					best = at
				}
			}
			from = at + len(marker)
		}
	}
	return best
}

// discoveryLexicalizedJiang is 将 that does not take the following noun:
// 将来, 将要, 将近, and 即将.
func discoveryLexicalizedJiang(text string, at int) bool {
	prev, _ := utf8.DecodeLastRuneInString(text[:at])
	if prev == '即' {
		return true
	}
	next, _ := utf8.DecodeRuneInString(text[at+len("将"):])
	switch next {
	case '来', '要', '近':
		return true
	default:
		return false
	}
}

func discoveryClauseBreak(object string) bool {
	return strings.ContainsAny(object, "，,。！？!?:：；;\n")
}

// discoveryPictureSuffixContinues reports that 生成 has no separate
// postverbal object. A complement (出来, 一下) and a delivery tail
// (出来发给我) still leave the preverbal noun as the object. 了一个说明
// and 说明 do not.
func discoveryPictureSuffixContinues(suffix string) bool {
	s := strings.TrimLeft(suffix, " \t\r\n，,。！？!?:：；;、")
	if s == "" {
		return true
	}
	for _, comp := range []string{"出来", "起来", "一下"} {
		if strings.HasPrefix(s, comp) {
			s = strings.TrimLeft(s[len(comp):], " \t\r\n，,。！？!?:：；;、")
			break
		}
	}
	if s == "" {
		return true
	}
	switch s {
	case "好", "好了", "好吧", "完", "完了", "吧", "了", "啊", "呀", "呢", "吗":
		return true
	}
	for _, tail := range []string{"发给我", "发给", "发我", "给我", "保存"} {
		if strings.HasPrefix(s, tail) {
			// 给我一段说明 is a new object. 出来发给我 is only the tail.
			rest := strings.TrimLeft(s[len(tail):], " \t\r\n，,。！？!?:：；;、")
			return discoveryPictureTailOnly(rest)
		}
	}
	return false
}

func discoveryPictureTailOnly(s string) bool {
	switch s {
	case "", "好", "好了", "好吧", "完", "完了", "吧", "了", "啊", "呀", "呢", "吗", "一下":
		return true
	}
	return false
}

// discoveryHasAffirmed reports phrase occurs and is not cancelled by 不/别/勿
// or by 不要/不用/不必/不是/别再/不再 immediately in front of it.
// 画 inside 动画 is not the verb.
func discoveryHasAffirmed(text, phrase string) bool {
	from := 0
	for from < len(text) {
		rel := strings.Index(text[from:], phrase)
		if rel < 0 {
			return false
		}
		at := from + rel
		// 动画图 contains 画图, but that 画 is not the verb.
		if strings.HasPrefix(phrase, "画") && discoveryAnimatedDraw(text, at) {
			from = at + len(phrase)
			continue
		}
		if !discoveryVerbNegated(text[:at]) {
			return true
		}
		from = at + len(phrase)
	}
	return false
}

func discoveryVerbNegated(prefix string) bool {
	runes := []rune(strings.TrimRight(prefix, " \t\r\n"))
	if len(runes) == 0 {
		return false
	}
	switch runes[len(runes)-1] {
	case '不', '别', '勿', '非':
		return true
	}
	if len(runes) >= 2 {
		switch string(runes[len(runes)-2:]) {
		case "不要", "不用", "不必", "不是", "别再", "不再":
			return true
		}
	}
	return false
}

// discoveryPreverbalPictureHead reports the byte offset of a picture-object
// head at the end of phrase: 图片, 图像, 张图, or 幅图.
func discoveryPreverbalPictureHead(phrase string) (int, bool) {
	for _, head := range []string{"图片", "图像", "张图", "幅图"} {
		if strings.HasSuffix(phrase, head) {
			return len(phrase) - len(head), true
		}
	}
	return 0, false
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

// notePetitionInputUnbound records a discovery expansion that failed
// because the capability's trusted-input contract is unmet, and that
// failure is not a missing built-in. An unsupported selected file still
// extends, so this returns false and the caller keeps the catalog miss.
// The stored note is what a later search of the same name repeats.
func (c *sharedAgentLoopCallbacks) notePetitionInputUnbound(name string, err error) bool {
	if c == nil || !tool.IsTrustedInputMissingOrAmbiguous(err) || c.discoveryTrustedInputExtends(err) {
		return false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	if c.petitionInputUnbound == nil {
		c.petitionInputUnbound = map[string]string{}
	}
	code := petitionInputUnboundMissing
	if strings.Contains(err.Error(), tool.TrustedInputAmbiguous) {
		code = petitionInputUnboundAmbiguous
	}
	c.petitionInputUnbound[name] = code
	return true
}

// discoveryTrustedInputExtends reports that this trusted-input failure is
// the planner's missing-built-in case: the turn selected files the native
// reader does not accept. An empty selection, and an image, are not that
// case. semanticCapabilityExtension is the one classification.
func (c *sharedAgentLoopCallbacks) discoveryTrustedInputExtends(err error) bool {
	if c == nil || !tool.IsTrustedInputMissingOrAmbiguous(err) {
		return false
	}
	var attachments []MessageAttachment
	if c.semanticSurface != nil && c.semanticSurface.replan != nil {
		attachments = c.semanticSurface.replan.Attachments
	}
	_, _, ok := semanticCapabilityExtension(err, c.userText, attachments)
	return ok
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
