package guiapp

import (
	"context"
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// semanticSessionResidue is the host-owned record of what this desktop
// conversation still has to do. It stores capability needs only. Grants,
// tool names, parameters, and route revisions stay on the per-turn plan.
type semanticSessionResidue struct {
	Generation uint64
	Status     semanticResidueStatus
	Needs      []tool.CapabilityNeed
	Summary    string
	// Remaining is how many further invocations of each capability this
	// session may still plan. LookupFacts records a successful web lookup.
	Remaining   map[string]int
	LookupFacts bool
}

type semanticResidueStatus string

const (
	semanticResidueOpen      semanticResidueStatus = "open"
	semanticResidueCompleted semanticResidueStatus = "completed"
)

type semanticResidueRelation string

const (
	semanticResidueNone     semanticResidueRelation = ""
	semanticResidueContinue semanticResidueRelation = "continue"
	semanticResidueSwitch   semanticResidueRelation = "switch"
	semanticResidueUnclear  semanticResidueRelation = "unclear"
)

func semanticResidueToPersisted(residue semanticSessionResidue) agent.SemanticSessionResidue {
	out := agent.SemanticSessionResidue{
		Generation:  residue.Generation,
		Status:      string(residue.Status),
		Summary:     residue.Summary,
		LookupFacts: residue.LookupFacts,
		Remaining:   cloneResidueRemaining(residue.Remaining),
	}
	for _, need := range residue.Needs {
		out.Needs = append(out.Needs, agent.SemanticSessionResidueNeed{
			ID:         need.ID,
			Capability: string(need.Capability),
			Required:   need.Required,
			Qualifiers: tool.CloneNeedQualifiers(need.Qualifiers),
		})
	}
	return out
}

func semanticResidueFromPersisted(residue agent.SemanticSessionResidue) semanticSessionResidue {
	out := semanticSessionResidue{
		Generation:  residue.Generation,
		Status:      semanticResidueStatus(residue.Status),
		Summary:     residue.Summary,
		LookupFacts: residue.LookupFacts,
		Remaining:   cloneResidueRemaining(residue.Remaining),
	}
	for _, need := range residue.Needs {
		out.Needs = append(out.Needs, tool.CapabilityNeed{
			ID:         need.ID,
			Capability: tool.CapabilityID(need.Capability),
			Required:   need.Required,
			Qualifiers: tool.CloneNeedQualifiers(need.Qualifiers),
		})
	}
	return out
}

func semanticResidueSessionKey(msg IMUserMessage) (string, bool) {
	if !strings.EqualFold(strings.TrimSpace(msg.Platform), "desktop") {
		return "", false
	}
	key := strings.TrimSpace(msg.UserID)
	if key == "" {
		return "", false
	}
	return key, true
}

func (h *IMMessageHandler) loadSemanticSessionResidue(key string) (semanticSessionResidue, bool) {
	if h == nil || strings.TrimSpace(key) == "" {
		return semanticSessionResidue{}, false
	}
	value, ok := h.semanticSessionResidues.Load(key)
	if !ok {
		return semanticSessionResidue{}, false
	}
	residue, ok := value.(semanticSessionResidue)
	if !ok {
		return semanticSessionResidue{}, false
	}
	residue.Needs = cloneSessionGovernedNeeds(residue.Needs)
	residue.Remaining = cloneResidueRemaining(residue.Remaining)
	return residue, true
}

func (h *IMMessageHandler) loadOpenSemanticSessionResidue(key string) (semanticSessionResidue, bool) {
	residue, ok := h.loadSemanticSessionResidue(key)
	if !ok || residue.Status != semanticResidueOpen || len(residue.Needs) == 0 {
		return semanticSessionResidue{}, false
	}
	return residue, true
}

// loadDesktopTurnResidue returns the open task, or a completed residue that
// still carries web-lookup facts. factsOnly means the task is finished: the
// next follow-up may skip a fresh search, and must not reopen the old tools.
func (h *IMMessageHandler) loadDesktopTurnResidue(msg IMUserMessage, workflow bool, attachments []MessageAttachment) (semanticSessionResidue, bool, bool) {
	key, desktop := semanticResidueSessionKey(msg)
	if h == nil || !desktop || workflow || hostTurnSelectedLocalImage(msg.Text, attachments) {
		return semanticSessionResidue{}, false, false
	}
	residue, ok := h.loadSemanticSessionResidue(key)
	if !ok && h.memory != nil {
		if persisted, found := h.memory.SemanticSessionResidue(key); found {
			residue = semanticResidueFromPersisted(persisted)
			h.semanticSessionResidues.Store(key, residue)
			ok = true
		}
	}
	if !ok {
		return semanticSessionResidue{}, false, false
	}
	if residue.Status == semanticResidueOpen && len(residue.Needs) > 0 {
		return residue, true, false
	}
	if residue.LookupFacts {
		return residue, false, true
	}
	return semanticSessionResidue{}, false, false
}

func (h *IMMessageHandler) storeSemanticSessionResidue(key string, residue semanticSessionResidue) {
	if h == nil || strings.TrimSpace(key) == "" {
		return
	}
	residue.Needs = cloneSessionGovernedNeeds(residue.Needs)
	residue.Remaining = cloneResidueRemaining(residue.Remaining)
	residue.Summary = strings.TrimSpace(residue.Summary)
	h.semanticSessionResidues.Store(key, residue)
	if h.memory != nil {
		h.memory.SetSemanticSessionResidue(key, semanticResidueToPersisted(residue))
	}
}

func markOpenTaskAnswerOnly(ctx *LoopContext, residueOpen bool, userText string) bool {
	if ctx == nil || !residueOpen || !semanticSocialNoToolText(userText) {
		return false
	}
	ctx.semanticTurnAnswerOnly = true
	return true
}

// semanticSocialNoToolText is a whole-utterance greeting or thanks. A task
// glued on ("谢谢，帮我改周报") is not one: the remainder must be a particle.
func semanticSocialNoToolText(text string) bool {
	if isTaskAnchorGreetingText(text) {
		return true
	}
	compact := compactSocialUtterance(text)
	switch compact {
	case "谢谢", "谢谢你", "感谢", "多谢", "辛苦了", "辛苦", "麻烦了", "麻烦你了",
		"thanks", "thankyou", "thx", "ty",
		"再见", "拜拜", "bye", "goodbye", "byebye",
		"哈哈", "哈哈哈", "呵呵", "嘿嘿":
		return true
	}
	for _, base := range []string{"谢谢", "感谢", "多谢", "辛苦", "哈哈", "呵呵", "thanks", "bye"} {
		if !strings.HasPrefix(compact, base) || len(compact) == len(base) {
			continue
		}
		switch compact[len(base):] {
		case "啊", "呀", "哦", "哟", "哈", "呢", "哇", "啦", "there", "ya":
			return true
		}
	}
	return false
}

// resetSemanticTurnLocalState drops residue bookkeeping that belongs to one
// inbound turn. A reused LoopContext otherwise keeps a greeting closure or a
// spent ceiling, and the next message cannot plan tools.
func resetSemanticTurnLocalState(ctx *LoopContext) {
	if ctx == nil {
		return
	}
	ctx.mu.Lock()
	defer ctx.mu.Unlock()
	ctx.semanticTurnAnswerOnly = false
	ctx.semanticSessionCeilingSpent = false
	ctx.semanticResidueCandidateNeeds = nil
	ctx.semanticResidueCandidateText = ""
	ctx.semanticResidueRemaining = nil
	ctx.semanticResidueLookupFacts = false
	ctx.semanticResidueLookupUsed = false
	ctx.semanticResidueUsed = nil
}

func (h *IMMessageHandler) clearSemanticSessionResidue(key string) {
	if h == nil || strings.TrimSpace(key) == "" {
		return
	}
	h.semanticSessionResidues.Delete(key)
	if h.memory != nil {
		h.memory.ClearSemanticSessionResidue(key)
	}
}

// semanticUtteranceIsTaskFollowUp reports a short continuation of the open
// task. A high-confidence new request does not match just because it is short.
func semanticUtteranceIsTaskFollowUp(text string) bool {
	if isTaskAnchorContinuationText(text) {
		return true
	}
	if utf8.RuneCountInString(strings.TrimSpace(text)) > 120 {
		return false
	}
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), " ", ""))
	for _, cue := range []string{"继续", "再改", "发给我", "然后", "再出一版", "再来一版", "continue"} {
		if strings.Contains(compact, strings.ReplaceAll(cue, " ", "")) {
			return true
		}
	}
	return false
}

// semanticFollowUpIsBareCue reports a continuation that does not name a new
// task. "继续" and "然后再执行一下" stay on the open surface. "然后连上服务器
// 跑一遍检查" does not.
func semanticFollowUpIsBareCue(text string) bool {
	if isTaskAnchorContinuationText(text) {
		return true
	}
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), " ", ""))
	compact = strings.Trim(compact, "。.!！?？~～啊呀吧呢哦哟")
	for _, cue := range []string{"然后再", "然后", "再改一版", "再来一版", "再出一版", "再改", "发给我", "继续", "continue"} {
		if !strings.Contains(compact, cue) {
			continue
		}
		rest := strings.Replace(compact, cue, "", 1)
		rest = strings.Trim(rest, "。.!！?？~～啊呀吧呢哦哟请")
		switch rest {
		case "", "一下", "下", "执行", "执行一下", "再执行", "再执行一下", "跑一下", "做一下", "发给我":
			return true
		}
		return false
	}
	return false
}

func semanticThenRest(text string) (string, bool) {
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), " ", ""))
	compact = strings.Trim(compact, "。.!！?？~～啊呀吧呢哦哟")
	var rest string
	switch {
	case strings.HasPrefix(compact, "然后再"):
		rest = strings.TrimPrefix(compact, "然后再")
	case strings.HasPrefix(compact, "然后"):
		rest = strings.TrimPrefix(compact, "然后")
	default:
		return "", false
	}
	rest = strings.Trim(rest, "。.!！?？~～啊呀吧呢哦哟")
	return rest, rest != ""
}

func semanticLeadingThenAside(text string) bool {
	rest, ok := semanticThenRest(text)
	// "然后查一下这份表" is about the open document, not a web search.
	return ok && semanticAsideRestIsExternalLookup(rest) && !semanticAsideRestIsDocumentWork(rest) && !semanticAsideRestIsAboutOpenDocument(rest)
}

func semanticAsideRestIsAboutOpenDocument(rest string) bool {
	for _, cue := range []string{"这份", "这张表", "这个表", "这个文件", "文档里", "表里", "报告里", "表格里", "上面的", "刚才的"} {
		if strings.Contains(rest, cue) {
			return true
		}
	}
	return false
}

func semanticAsideRestIsExternalLookup(rest string) bool {
	for _, cue := range []string{"天气", "气温", "预报", "新闻", "股价", "汇率", "搜索", "查一下", "查询", "最新", "几点"} {
		if strings.Contains(rest, cue) {
			return true
		}
	}
	return false
}

func semanticUtteranceNamesExternalLookup(userText string) bool {
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(userText), " ", ""))
	return semanticAsideRestIsExternalLookup(compact) || lexicalWebSearchRequest(userText) || lexicalFreshLookupRequest(userText)
}

func semanticLeadingThenDocumentEdit(text string) bool {
	rest, ok := semanticThenRest(text)
	return ok && semanticAsideRestIsDocumentWork(rest)
}

// semanticResidueShortDocumentEdit is a residue continuation that only asks
// to edit the open document. It keeps that document tool and does not grow
// download, read, bash, or write_file around it.
func semanticResidueShortDocumentEdit(result intent.ClassificationResult, userText string) bool {
	if !strings.Contains(result.Reason, "session residue") {
		return false
	}
	switch result.Primary {
	case intent.LabelOffice, intent.LabelDocumentGenerate, intent.LabelFileWrite:
	default:
		return false
	}
	if utf8.RuneCountInString(strings.TrimSpace(userText)) > 24 {
		return false
	}
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(userText), " ", ""))
	compact = strings.Trim(compact, "。.!！?？~～啊呀吧呢哦哟")
	if strings.HasPrefix(compact, "然后") && !strings.HasPrefix(compact, "然后再") {
		compact = strings.TrimPrefix(compact, "然后")
		compact = strings.Trim(compact, "。.!！?？~～啊呀吧呢哦哟")
	}
	return semanticAsideRestIsDocumentWork(compact)
}

// semanticResidueSlimOfficeTurn is a short residue turn about the open
// document. "改短一点" and "然后结论是什么" keep the document tool.
// "继续" and "然后再执行一下" still get the workspace tools.
func semanticResidueSlimOfficeTurn(result intent.ClassificationResult, userText string) bool {
	// A replan or petition calls the planner with an empty utterance. That
	// must not look like a short question and strip the published office tools.
	if strings.TrimSpace(userText) == "" {
		return false
	}
	if semanticResidueShortDocumentEdit(result, userText) {
		return true
	}
	if !strings.Contains(result.Reason, "session residue") {
		return false
	}
	switch result.Primary {
	case intent.LabelOffice, intent.LabelDocumentGenerate, intent.LabelFileWrite:
	default:
		return false
	}
	if utf8.RuneCountInString(strings.TrimSpace(userText)) > 24 {
		return false
	}
	return !semanticNeedsWorkspaceContinuation(userText)
}

func semanticNeedsWorkspaceContinuation(userText string) bool {
	if !semanticFollowUpIsBareCue(userText) {
		return false
	}
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(userText), " ", ""))
	compact = strings.Trim(compact, "。.!！?？~～啊呀吧呢哦哟")
	if semanticAsideRestIsDocumentWork(compact) {
		return false
	}
	return true
}

func semanticAsideRestIsDocumentWork(rest string) bool {
	// Single characters collide with place names: 长 is in 长沙, 加 is in 加拿大.
	for _, cue := range []string{
		"改", "写", "删", "标题", "封面", "页边", "发给", "发我",
		"加上", "加一段", "加一节", "长一点", "短一点", "这一版", "再版",
	} {
		if strings.Contains(rest, cue) {
			return true
		}
	}
	return false
}

func decideSemanticResidueRelation(current intent.ClassificationResult, userText string, residue semanticSessionResidue) semanticResidueRelation {
	if residue.Status != semanticResidueOpen || len(residue.Needs) == 0 {
		return semanticResidueNone
	}
	if semanticSocialNoToolText(userText) {
		return semanticResidueNone
	}
	// "然后改到本机执行" is a follow-up phrase and an explicit machine change.
	// The machine change wins; a bare "然后再执行一下" stays on the open host.
	if semanticExplicitSurfaceChange(current, residue.Needs, userText) {
		return semanticResidueSwitch
	}
	// "然后现在几点" is a clock question. The 然后 does not pull the open
	// office tools into this turn.
	if semanticExplicitReadOnlySideQuestion(current, userText) {
		return semanticResidueNone
	}
	// "所以，多的脂肪去哪了？" is unknown on its own. Merging the open summary
	// ("北京天气") can score live_data at 0.85 and look like a new lookup.
	// That merge stays on the open task unless the utterance itself names a
	// lookup ("崇州天气") or a different mutating task ("连上服务器").
	if semanticTaskContextMerged(current) && !semanticUtteranceNamesExternalLookup(userText) {
		if current.Confidence >= 0.85 && semanticClassificationHasMutatingFamily(current) && semanticResidueMutatingDisjoint(current, residue.Needs) && !semanticKeepsOpenWorkSurface(current, residue.Needs, userText) && !semanticFollowUpIsBareCue(userText) {
			return semanticResidueSwitch
		}
		return semanticResidueUnclear
	}
	// "崇州天气" after "北京天气", and "重庆天气，生成格式化pdf" after a
	// spent weather PDF, are new lookups. Continuing would clamp the spent
	// search and generate grants and reuse the previous city's facts.
	if semanticFreshLookupAgainstLookupResidue(current, userText, residue) {
		return semanticResidueNone
	}
	// "然后连上服务器跑一遍检查" names a different task. A bare "继续" or
	// "发给我" stays on the open tools and does not adopt a jittered shell.
	if current.Confidence >= 0.85 && semanticClassificationHasMutatingFamily(current) && semanticResidueMutatingDisjoint(current, residue.Needs) {
		if semanticKeepsOpenWorkSurface(current, residue.Needs, userText) || semanticFollowUpIsBareCue(userText) {
			return semanticResidueUnclear
		}
		return semanticResidueSwitch
	}
	// "再改一版" or "然后改短一点" labeled search keeps the open document and
	// does not add web_search. "然后北京天气怎么样" is only the weather question.
	if current.Confidence >= 0.85 && imSemanticIntentIsManaged(current) && !semanticClassificationHasMutatingFamily(current) {
		if semanticFollowUpIsBareCue(userText) || semanticLeadingThenDocumentEdit(userText) {
			return semanticResidueUnclear
		}
		if semanticLeadingThenAside(userText) {
			return semanticResidueNone
		}
		// "然后结论是什么" is about the open document. Keep its tools and do
		// not adopt a search label. Weather and news stay outside, above.
		if _, ok := semanticThenRest(userText); ok {
			return semanticResidueUnclear
		}
	}
	if semanticUtteranceIsTaskFollowUp(userText) || isGenericContinuationPrimary(current) {
		if semanticKeepsOpenWorkSurface(current, residue.Needs, userText) {
			return semanticResidueUnclear
		}
		return semanticResidueContinue
	}
	if !imSemanticIntentIsManaged(current) || current.Primary.IsNonCapabilityLabel() {
		return semanticResidueUnclear
	}
	// A read-only side question does not inherit the open task and does not
	// close it. Settle keeps the mutating residue.
	if current.Confidence >= 0.85 && !semanticClassificationHasMutatingFamily(current) {
		return semanticResidueNone
	}
	// A short edit of the open delivery stays on that grant. Everything else
	// that already classified as its own managed request is a new delivery:
	// the previous PDF or lookup being spent is not a reason to refuse the
	// next one. Follow-ups returned above.
	if semanticKeepsOpenWorkSurface(current, residue.Needs, userText) {
		return semanticResidueUnclear
	}
	return semanticResidueNone
}

func semanticKeepsOpenWorkSurface(current intent.ClassificationResult, needs []tool.CapabilityNeed, userText string) bool {
	if semanticPureInteractiveClassification(current) && semanticResidueHasInteractiveSurface(needs) {
		return true
	}
	if semanticKeepsOpenShell(current, needs, userText) {
		return true
	}
	// A short edit of the file already open ("改短一点") is often labeled
	// file_write or document_generate. A longer request still switches.
	if utf8.RuneCountInString(strings.TrimSpace(userText)) > 24 {
		return false
	}
	return semanticPureDocumentEditClassification(current) && semanticResidueHasDocumentEdit(needs)
}

func semanticTaskContextMerged(result intent.ClassificationResult) bool {
	return strings.Contains(result.Reason, "task-context merge")
}

// semanticFreshLookupSubjectMinConfidence admits a verified lookup that sits
// just under the 0.85 relation gate. "重庆天气，生成格式化pdf" scored
// live_data 0.839 and otherwise continued the previous city's spent PDF grant.
const semanticFreshLookupSubjectMinConfidence = 0.82

func semanticDeliveryOfFreshLookup(compact string) bool {
	if !semanticUtteranceNamesFreshLookupSubject(compact) {
		return false
	}
	stripped := compact
	for _, cue := range []string{"发给我", "发给", "发我"} {
		stripped = strings.ReplaceAll(stripped, cue, "")
	}
	return !semanticAsideRestIsDocumentWork(stripped)
}

// semanticFollowUpStaysOnOpenTask is a continuation of the open delivery.
// "发给我" and "然后" can introduce a new subject, so they do not.
func semanticFollowUpStaysOnOpenTask(text string) bool {
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), " ", ""))
	for _, cue := range []string{"继续", "再改", "再出一版", "再来一版", "continue"} {
		if strings.Contains(compact, cue) {
			return true
		}
	}
	return false
}

func semanticUtteranceNamesFreshLookupSubject(userText string) bool {
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(userText), " ", ""))
	for _, cue := range []string{"天气", "气温", "预报", "新闻", "股价", "汇率"} {
		if strings.Contains(compact, cue) {
			return true
		}
	}
	return false
}

func semanticFreshLookupAgainstLookupResidue(current intent.ClassificationResult, userText string, residue semanticSessionResidue) bool {
	if current.Degraded || semanticFollowUpIsBareCue(userText) {
		return false
	}
	switch current.Primary {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelLiveDataVisual, intent.LabelWebFetch:
	case intent.LabelDocumentGenerate:
		// "把重庆天气发给我" can be labeled as the PDF itself. A new
		// lookup subject still needs a fresh grant. "生成pdf报告" with
		// no new subject stays on the open facts.
		if !semanticUtteranceNamesFreshLookupSubject(userText) {
			return false
		}
	default:
		return false
	}
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(userText), " ", ""))
	// "然后写成周报" can share a live-data label with the previous weather
	// card. That is a document request, not another city lookup. "发给我"
	// only delivers, so "然后把重庆天气发给我" stays a new lookup.
	if (semanticLeadingThenDocumentEdit(userText) || semanticAsideRestIsDocumentWork(compact)) && !semanticDeliveryOfFreshLookup(compact) {
		return false
	}
	// "继续查一下天气" while a report is open keeps that report. "然后重庆天气，
	// 生成格式化pdf" after a weather PDF is a new delivery; 然后 alone must
	// not spend the previous city's grant.
	namedSubject := semanticUtteranceNamesFreshLookupSubject(userText)
	if namedSubject && semanticUtteranceIsTaskFollowUp(userText) {
		if semanticKeepsOpenWorkSurface(current, residue.Needs, userText) {
			return false
		}
		// A previous weather card is spent by "然后把重庆天气发给我" even
		// when this sentence is only a lookup. "把重庆天气发给我" while a
		// report is open is a delivery of that lookup, not a revision.
		// "继续查一下天气" stays with the report.
		if semanticResidueIsLookupVisual(residue.Needs) || (semanticDeliveryOfFreshLookup(compact) && !semanticFollowUpStaysOnOpenTask(userText)) {
			return true
		}
		if semanticClassificationHasMutatingFamily(current) && semanticResidueHasDocumentWork(residue.Needs) {
			return current.Confidence >= semanticFreshLookupSubjectMinConfidence
		}
		return false
	}
	if namedSubject {
		return current.Confidence >= semanticFreshLookupSubjectMinConfidence
	}
	if current.Confidence < 0.85 {
		return false
	}
	return semanticResidueIsLookupVisual(residue.Needs) && !semanticResidueHasDocumentWork(residue.Needs)
}

func semanticResidueIsLookupVisual(needs []tool.CapabilityNeed) bool {
	saw := false
	for _, need := range needs {
		switch strings.TrimSpace(string(need.Capability)) {
		case "information.search.web", "information.fetch.web", "visual.render.live_data":
			saw = true
		}
	}
	return saw
}

func semanticResidueHasDocumentWork(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		switch need.Capability {
		case tool.CapabilityDocumentWriteOffice, agentservice.CapabilityDocumentGenerate, tool.CapabilityFSWriteLocal:
			return true
		}
	}
	return false
}

func semanticExplicitReadOnlySideQuestion(current intent.ClassificationResult, userText string) bool {
	if current.Degraded || current.Confidence < 0.85 || semanticClassificationHasMutatingFamily(current) || !imSemanticIntentIsManaged(current) {
		return false
	}
	if semanticPureLabel(current, intent.LabelCurrentTime) && isLocalCurrentTimeQuery(userText) {
		return true
	}
	if semanticPureLabel(current, intent.LabelSearch) || semanticPureLabel(current, intent.LabelLiveData) || semanticPureLabel(current, intent.LabelWebFetch) {
		return lexicalWebSearchRequest(userText) || lexicalFreshLookupRequest(userText)
	}
	return false
}

func semanticFollowUpAllowsTaskMerge(result *intent.ClassificationResult, userText string) bool {
	if result == nil || result.Degraded {
		return true
	}
	// Relation already inherits, switches, or stands aside. Merging the open
	// task summary here would erase a shell switch or a weather question.
	if result.Confidence >= 0.85 && imSemanticIntentIsManaged(*result) {
		return false
	}
	return !semanticExplicitReadOnlySideQuestion(*result, userText)
}

func semanticExplicitSurfaceChange(current intent.ClassificationResult, needs []tool.CapabilityNeed, userText string) bool {
	text := strings.ToLower(userText)
	if semanticResidueHasCapability(needs, tool.CapabilityShellExecuteRemoteHost) && semanticPureLabel(current, intent.LabelShellCommand) && semanticExplicitLocalShell(text) {
		return true
	}
	if semanticResidueHasCapability(needs, tool.CapabilityShellExecuteLocal) && semanticPureLabel(current, intent.LabelSSH) && semanticExplicitRemoteShell(text) {
		return true
	}
	return false
}

func semanticKeepsOpenShell(current intent.ClassificationResult, needs []tool.CapabilityNeed, userText string) bool {
	if utf8.RuneCountInString(strings.TrimSpace(userText)) > 24 {
		return false
	}
	openRemote := semanticResidueHasCapability(needs, tool.CapabilityShellExecuteRemoteHost)
	openLocal := semanticResidueHasCapability(needs, tool.CapabilityShellExecuteLocal)
	if openRemote == openLocal {
		return false
	}
	text := strings.ToLower(userText)
	if openRemote && semanticPureLabel(current, intent.LabelShellCommand) && !semanticExplicitLocalShell(text) {
		return true
	}
	if openLocal && semanticPureLabel(current, intent.LabelSSH) && !semanticExplicitRemoteShell(text) {
		return true
	}
	return false
}

func semanticExplicitLocalShell(text string) bool {
	for _, cue := range []string{"本地", "本机", "local"} {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func semanticExplicitRemoteShell(text string) bool {
	for _, cue := range []string{"服务器", "远端", "远程", "ssh"} {
		if strings.Contains(text, cue) {
			return true
		}
	}
	return false
}

func semanticPureLabel(current intent.ClassificationResult, want intent.IntentLabel) bool {
	saw := false
	for _, label := range current.Labels() {
		if label == want {
			saw = true
			continue
		}
		if !label.IsNonCapabilityLabel() && len(imSemanticIntentRuleSet[label]) > 0 {
			return false
		}
	}
	return saw
}

func semanticResidueHasCapability(needs []tool.CapabilityNeed, capability tool.CapabilityID) bool {
	for _, need := range needs {
		if need.Capability == capability {
			return true
		}
	}
	return false
}

func semanticPureDocumentEditClassification(current intent.ClassificationResult) bool {
	saw := false
	for _, label := range current.Labels() {
		switch label {
		case intent.LabelOffice, intent.LabelDocumentGenerate, intent.LabelFileWrite:
			saw = true
		default:
			if !label.IsNonCapabilityLabel() && len(imSemanticIntentRuleSet[label]) > 0 {
				return false
			}
		}
	}
	return saw
}

func semanticResidueHasDocumentEdit(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		switch need.Capability {
		case tool.CapabilityDocumentWriteOffice, tool.CapabilityFSWriteLocal, agentservice.CapabilityDocumentGenerate:
			return true
		}
	}
	return false
}

func semanticPureInteractiveClassification(current intent.ClassificationResult) bool {
	saw := false
	for _, label := range current.Labels() {
		switch label {
		case intent.LabelBrowser, intent.LabelComputerUse:
			saw = true
		default:
			if !label.IsNonCapabilityLabel() && len(imSemanticIntentRuleSet[label]) > 0 {
				return false
			}
		}
	}
	return saw
}

func semanticResidueHasInteractiveSurface(needs []tool.CapabilityNeed) bool {
	for _, need := range needs {
		switch need.Capability {
		case tool.CapabilityBrowserControlWeb, tool.CapabilityComputerControlDesktop:
			return true
		}
	}
	return false
}

func semanticClassificationHasMutatingFamily(result intent.ClassificationResult) bool {
	for _, capability := range semanticRuleCapabilities(result) {
		if sessionGovernedNeedHasSideEffect(tool.CapabilityNeed{Capability: capability}) {
			return true
		}
	}
	return false
}

func semanticRuleCapabilities(result intent.ClassificationResult) []tool.CapabilityID {
	var capabilities []tool.CapabilityID
	seen := map[tool.CapabilityID]bool{}
	for _, label := range result.Labels() {
		for _, template := range imSemanticIntentRuleSet[label] {
			if template.Capability == "" || seen[template.Capability] {
				continue
			}
			seen[template.Capability] = true
			capabilities = append(capabilities, template.Capability)
		}
	}
	return capabilities
}

func semanticResidueMutatingDisjoint(current intent.ClassificationResult, needs []tool.CapabilityNeed) bool {
	open := map[tool.CapabilityID]bool{}
	for _, need := range needs {
		if sessionGovernedNeedHasSideEffect(need) {
			open[need.Capability] = true
		}
	}
	if len(open) == 0 {
		return true
	}
	for _, capability := range semanticRuleCapabilities(current) {
		if sessionGovernedNeedHasSideEffect(tool.CapabilityNeed{Capability: capability}) && open[capability] {
			return false
		}
	}
	return true
}

// semanticClassificationWithOpenResidue folds an open task's granted needs
// into this turn's classification. Continue unions them with a new managed
// label. Unclear, continuation, and unknown keep the open needs and do not
// take a new mutating family from the bare sentence.
func semanticClassificationWithOpenResidue(current intent.ClassificationResult, needs []tool.CapabilityNeed, relation semanticResidueRelation) (intent.ClassificationResult, bool) {
	switch relation {
	case semanticResidueContinue, semanticResidueUnclear:
	default:
		return current, false
	}
	inherited := agentservice.ClassificationFromGrantedNeeds(needs, imSemanticIntentRuleSet)
	if !imSemanticIntentIsManaged(inherited) {
		return current, false
	}
	if relation == semanticResidueUnclear || !imSemanticIntentIsManaged(current) || current.Primary.IsNonCapabilityLabel() {
		inherited.Reason = "session residue continuation"
		return inherited, true
	}
	out := current
	for _, label := range inherited.Labels() {
		if !out.HasLabel(label) {
			out.Secondary = append(append([]intent.IntentLabel(nil), out.Secondary...), label)
		}
	}
	out.Reason = strings.TrimSpace(out.Reason + "; session residue")
	return out, true
}

func (h *IMMessageHandler) noteSemanticSessionResidueCandidate(ctx *LoopContext, userID, channel, userText string, plan tool.ToolPlan) {
	if h == nil || ctx == nil || ctx.WorkflowAgentLoop {
		return
	}
	if !strings.EqualFold(strings.TrimSpace(channel), "desktop") || h.isPureCodingWorkbenchSession(userID) {
		return
	}
	needs := grantedNeedsFromPlan(plan)
	if len(needs) == 0 {
		return
	}
	ctx.semanticResidueCandidateNeeds = cloneSessionGovernedNeeds(needs)
	ctx.semanticResidueCandidateText = strings.TrimSpace(userText)
}

func (h *IMMessageHandler) settleSemanticSessionResidue(msg IMUserMessage, loopCtx *LoopContext, resp *IMAgentResponse) {
	key, ok := semanticResidueSessionKey(msg)
	if !ok || h == nil || loopCtx == nil || resp == nil {
		return
	}
	if strings.TrimSpace(resp.Error) != "" || len(loopCtx.semanticResidueCandidateNeeds) == 0 {
		return
	}
	previous, _ := h.loadSemanticSessionResidue(key)
	// A read-only plan must not close an open mutating task, including when
	// the sentence contains 然后 or 再查. Those cues do not finish the task.
	if previous.Status == semanticResidueOpen && sessionGovernedNeedsHaveSideEffect(previous.Needs) && !sessionGovernedNeedsHaveSideEffect(loopCtx.semanticResidueCandidateNeeds) {
		return
	}
	summary := semanticResidueSummary(msg.Text, previous.Summary)
	if strings.TrimSpace(loopCtx.semanticResidueCandidateText) != "" && !semanticUtteranceIsTaskFollowUp(msg.Text) {
		summary = semanticResidueSummary(loopCtx.semanticResidueCandidateText, "")
	}
	status := semanticResidueOpen
	if !sessionGovernedNeedsHaveSideEffect(loopCtx.semanticResidueCandidateNeeds) {
		status = semanticResidueCompleted
	}
	used, lookupUsed := loopCtx.semanticResidueUsage()
	lookupFacts := lookupUsed
	if semanticUtteranceIsTaskFollowUp(msg.Text) {
		lookupFacts = lookupFacts || previous.LookupFacts
	}
	h.storeSemanticSessionResidue(key, semanticSessionResidue{
		Generation:  previous.Generation + 1,
		Status:      status,
		Needs:       loopCtx.semanticResidueCandidateNeeds,
		Summary:     summary,
		Remaining:   residueRemainingAfterUse(loopCtx.semanticResidueCandidateNeeds, used),
		LookupFacts: lookupFacts,
	})
}

func residueRemainingAfterUse(needs []tool.CapabilityNeed, used map[string]int) map[string]int {
	counts := map[string]int{}
	for _, need := range needs {
		counts[string(need.Capability)]++
	}
	if len(counts) == 0 {
		return nil
	}
	remaining := make(map[string]int, len(counts))
	for capability, count := range counts {
		left := count - used[capability]
		if left < 0 {
			left = 0
		}
		remaining[capability] = left
	}
	return remaining
}

// semanticFollowUpRenewsDownloads reports a continuation that asks for
// another wave of files. "继续补图" must not inherit a spent download
// ceiling, or the extra photos the user just asked for never start.
func semanticFollowUpRenewsDownloads(text string) bool {
	compact := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(text), " ", ""))
	for _, cue := range []string{"不要下载", "别下载", "不用下载", "不要补图"} {
		if strings.Contains(compact, cue) {
			return false
		}
	}
	// "继续下" also matches "继续下周的报告", and "再下" matches "再下结论".
	// Only phrases that ask for more files renew the ceiling.
	for _, cue := range []string{"补图", "补几张", "补照片", "继续下载", "再下载", "再下几", "再下一张", "下几张", "下载图片", "下载照片", "下载剩下", "接着下载"} {
		if strings.Contains(compact, cue) {
			return true
		}
	}
	return false
}

// semanticResidueRemainingForFollowUp copies the open ceiling. A download
// continuation drops only the acquire count, so a new wave is published at
// DownloadRepeatBudget while the rest of the open grant stays spent.
func semanticResidueRemainingForFollowUp(remaining map[string]int, text string) map[string]int {
	out := cloneResidueRemaining(remaining)
	if len(out) == 0 || !semanticFollowUpRenewsDownloads(text) {
		return out
	}
	delete(out, string(tool.CapabilityArtifactAcquireRemote))
	return out
}

func cloneResidueRemaining(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

type semanticResidueRemainingKey struct{}

func withSemanticResidueRemaining(ctx context.Context, remaining map[string]int) context.Context {
	if len(remaining) == 0 {
		return ctx
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, semanticResidueRemainingKey{}, cloneResidueRemaining(remaining))
}

func semanticResidueRemaining(ctx context.Context) map[string]int {
	if ctx == nil {
		return nil
	}
	remaining, _ := ctx.Value(semanticResidueRemainingKey{}).(map[string]int)
	return remaining
}

// dropUnusedSessionCompanions removes optional baseline and archetype
// companions that this session planned and never called. A companion that
// was used, or that this turn declares for the first time, stays.
func dropUnusedSessionCompanions(needs []tool.CapabilityNeed, remaining map[string]int) []tool.CapabilityNeed {
	if len(needs) == 0 || len(remaining) == 0 {
		return needs
	}
	expanded := map[string]int{}
	for _, need := range needs {
		expanded[string(need.Capability)]++
	}
	out := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		if need.Required || !unusedSessionCompanion(need, remaining, expanded) {
			out = append(out, need)
		}
	}
	return out
}

func unusedSessionCompanion(need tool.CapabilityNeed, remaining, expanded map[string]int) bool {
	companion := strings.Contains(need.ID, "zz-baseline:")
	if !companion {
		for _, evidence := range need.EvidenceIDs {
			if evidence == "intent:baseline_workspace" || evidence == "intent:archetype_bundle" {
				companion = true
				break
			}
		}
	}
	if !companion {
		return false
	}
	limit, tracked := remaining[string(need.Capability)]
	if !tracked {
		return false
	}
	return limit >= expanded[string(need.Capability)]
}

func clampNeedsToResidueRemaining(needs []tool.CapabilityNeed, remaining map[string]int) []tool.CapabilityNeed {
	if len(needs) == 0 || len(remaining) == 0 {
		return needs
	}
	seen := map[string]int{}
	out := make([]tool.CapabilityNeed, 0, len(needs))
	for _, need := range needs {
		capability := string(need.Capability)
		limit, ok := remaining[capability]
		if !ok {
			out = append(out, need)
			continue
		}
		if seen[capability] >= limit {
			continue
		}
		seen[capability]++
		out = append(out, need)
	}
	return out
}

func semanticPlanHasBaseline(plan tool.ToolPlan) bool {
	for _, selection := range plan.Selections {
		if strings.Contains(selection.NeedID, "zz-baseline:") {
			return true
		}
	}
	return false
}

func semanticResidueSummary(current, previous string) string {
	current = truncateRunes(strings.TrimSpace(current), 120)
	previous = strings.TrimSpace(previous)
	if semanticUtteranceIsTaskFollowUp(current) && previous != "" {
		return previous
	}
	if current != "" {
		return current
	}
	return previous
}
