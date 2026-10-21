package guiapp

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"unicode"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/agentservice"
	"github.com/RapidAI/CodeClaw/corelib/intent"
	"github.com/RapidAI/CodeClaw/corelib/skill"
)

// Skill admission is how a complex task uses an installed skill without
// reopening the manage_skill gateway.
//
// The managed turn never shows the skill catalog, and a skill without a
// reviewed contract is quarantined, so the model cannot find or run one.
// The host therefore decides before the request:
//
//  1. On a complex task, score installed skills in this experience pool.
//     A match is used only when it is the right skill (its name or a trigger
//     is in the request) and it is complete (the runner can execute it, or
//     it is an agent-guided workflow with its instructions).
//  2. A runnable match is published as a closed skill_* function. The model
//     supplies business arguments. The host keeps the skill identity.
//  3. An agent-guided match injects its instructions and the host tools those
//     instructions need. It is not a manage_skill run.
//  4. When nothing qualifies, the request lists search_and_install_skill.
//     That call searches, installs, and runs the installed skill.
//
// Listing the tool is not use. The call authorizer accepts the admitted
// name, refuses the generic tools that would finish the task instead, and
// the loop reminds the model once if it stops without calling. After the
// admitted call has run, generic tools are available again. An agent-guided
// workflow keeps its file tools and keeps generate_pdf, office, and
// craft_tool closed for the turn.
//
// Lookups (weather, search, mail, a single file delete) are not complex
// tasks, so they neither bind a skill nor search the market.

const (
	skillAdmissionMinScore = 10
	skillAdmissionMaxBound = 3
)

type skillAdmissionMode int

const (
	skillAdmissionNone skillAdmissionMode = iota
	skillAdmissionBound
	skillAdmissionGuided
	skillAdmissionRemote
)

type admittedSkillBinding struct {
	Alias         string
	Name          string
	StableID      string
	Version       string
	ContentDigest string
	Description   string
}

type skillAdmissionDecision struct {
	Mode   skillAdmissionMode
	Bound  []admittedSkillBinding
	Guided []NLSkillDefinition
}

func (h *IMMessageHandler) skillAdmissionForTurn(ctx *LoopContext, userText string) skillAdmissionDecision {
	if h == nil || ctx != nil && (ctx.WorkflowAgentLoop || ctx.Platform == "ve_group_executor") {
		return skillAdmissionDecision{}
	}
	if goalContinuationPhase(userText) == "plan" {
		return skillAdmissionDecision{}
	}
	text := skillAdmissionTaskText(userText)
	if text == "" {
		return skillAdmissionDecision{}
	}
	if !complexSkillTask(ctx, text) {
		return skillAdmissionDecision{}
	}
	exec := h.getSkillExecutor()
	if exec == nil {
		return skillAdmissionDecision{Mode: skillAdmissionRemote}
	}
	owner := ""
	if ctx != nil {
		owner = ctx.UserID
	}
	bound, guided := admitLocalSkills(exec, text, h.skillExperienceDomainForOwner(owner))
	switch {
	case len(guided) > 0:
		log.Printf("[skill-admission] mode=guided task=%q skill=%s", truncateLogText(text, 60), guided[0].Name)
		return skillAdmissionDecision{Mode: skillAdmissionGuided, Guided: guided}
	case len(bound) > 0:
		names := make([]string, len(bound))
		for i, item := range bound {
			names[i] = item.Name
		}
		log.Printf("[skill-admission] mode=bound task=%q skills=%s", truncateLogText(text, 60), strings.Join(names, ", "))
		return skillAdmissionDecision{Mode: skillAdmissionBound, Bound: bound}
	default:
		log.Printf("[skill-admission] mode=remote task=%q", truncateLogText(text, 60))
		return skillAdmissionDecision{Mode: skillAdmissionRemote}
	}
}

func complexSkillTask(ctx *LoopContext, userText string) bool {
	if intent.ExplicitSkillInvocation(userText) {
		return true
	}
	if ctx == nil || !executionSurfaceIsFull(executionProfileFromLoop(ctx)) {
		return false
	}
	if ctx.Runtime.SemanticIntent == nil {
		return true
	}
	return skillTaskLabel(ctx.Runtime.SemanticIntent.Primary)
}

// skillAdmissionTaskText is the task the prompt and the tool list both score.
// Image notes are not the request. An attached document body is not the user
// naming a skill, and it must not be sent to the skill market either.
func skillAdmissionTaskText(userText string) string {
	return strings.TrimSpace(agent.CompactQueryForEmbedding(semanticUserIntentText(userText)))
}

func skillTaskLabel(primary intent.IntentLabel) bool {
	if primary.IsNonCapabilityLabel() {
		return false
	}
	switch primary {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelLiveDataVisual, intent.LabelWebFetch, intent.LabelCurrentTime,
		intent.LabelAttachmentDelivery, intent.LabelDocumentDelivery, intent.LabelDocumentOpen, intent.LabelDocumentRead,
		intent.LabelAppLaunch, intent.LabelFileDownload, intent.LabelFileDelete,
		intent.LabelScheduleManage, intent.LabelScheduleDispatch,
		intent.LabelConfigManage, intent.LabelSessionManage, intent.LabelTemplateManage,
		intent.LabelKnowledgeAdmin, intent.LabelKnowledgeWrite,
		intent.LabelAudioDeliver, intent.LabelAudioSynthesize, intent.LabelAudioRecord, intent.LabelAudioTranscribe,
		intent.LabelScreenshot, intent.LabelComputerUse,
		intent.LabelMailboxRead, intent.LabelMailboxSend, intent.LabelMailboxConfigure, intent.LabelMailboxDelete,
		intent.LabelWorklogRead, intent.LabelWorklogUpdate:
		return false
	default:
		return true
	}
}

// semanticLookupPlanningLabel is a label that can finish the turn by answering.
// An explicit skill request must not keep that plan: web_search would satisfy
// the classifier and the skill the user named would never run.
func semanticLookupPlanningLabel(label intent.IntentLabel) bool {
	switch label {
	case intent.LabelSearch, intent.LabelLiveData, intent.LabelLiveDataVisual, intent.LabelWebFetch, intent.LabelCurrentTime:
		return true
	default:
		return false
	}
}

// semanticExplicitSkillPlanningRewrite is the capability plan for a sentence
// that names a skill. A lookup label publishes web_search, and calling that
// tool is enough for the model to stop. Any other capability already on the
// verdict stays, including mailbox, knowledge write, and computer use; the
// lookup labels beside it are removed. A lookup-only verdict becomes shell,
// which is the capability an agent-guided workflow actually runs. Replacing
// those other capabilities with shell would drop the tools the sentence
// already asked for, and would undo the knowledge-write and mailbox-delete
// retargets that run before this one.
func semanticExplicitSkillPlanningRewrite(current *intent.ClassificationResult, userText string) *intent.ClassificationResult {
	if current == nil || !explicitSkillInvocationForProfile(userText) {
		return nil
	}
	kept := make([]intent.IntentLabel, 0, len(current.Labels()))
	hasLookup := false
	for _, label := range current.Labels() {
		if semanticLookupPlanningLabel(label) {
			hasLookup = true
			continue
		}
		if label.IsNonCapabilityLabel() {
			continue
		}
		kept = append(kept, label)
	}
	if len(kept) > 0 {
		if !hasLookup {
			return nil
		}
		out := *current
		out.Primary = kept[0]
		if len(kept) == 1 {
			out.Secondary = nil
		} else {
			out.Secondary = kept[1:]
		}
		out.ToolNames = nil
		return semanticExplicitSkillPlanningNote(&out)
	}
	if !hasLookup {
		return nil
	}
	if !imSemanticIntentIsManaged(*current) && !semanticLookupHalf(*current) {
		return nil
	}
	out := semanticLookupClassificationForPlanning(*current)
	out.Primary = intent.LabelShellCommand
	out.Secondary = nil
	out.ToolNames = nil
	out.WorkflowType = ""
	out.Degraded = false
	out.ControlPlaneFailure = false
	out.CreationOriented = false
	out.RunnerUp = ""
	out.RunnerUpScore = 0
	if out.Layer != 23 && out.Layer < 3 {
		out.Layer = 3
	}
	return semanticExplicitSkillPlanningNote(&out)
}

func semanticExplicitSkillPlanningNote(out *intent.ClassificationResult) *intent.ClassificationResult {
	const note = "explicit skill invocation planning"
	if out.Reason == "" {
		out.Reason = note
	} else if !strings.Contains(out.Reason, note) {
		out.Reason = strings.TrimSpace(out.Reason + "; " + note)
	}
	return out
}

func admitLocalSkills(exec *SkillExecutor, userText, domain string) ([]admittedSkillBinding, []NLSkillDefinition) {
	if exec == nil {
		return nil, nil
	}
	msg := strings.ToLower(strings.TrimSpace(userText))
	if msg == "" {
		return nil, nil
	}
	// Score the loaded entries directly. List() also stats every skill
	// directory for app metadata this decision does not use, and a second
	// load can pair one skill's description with another skill's digest
	// when two entries share a name.
	entries := exec.loadSkills()
	agentDomain := corelib.NormalizeSkillExperienceDomain(domain)
	type scored struct {
		def           NLSkillDefinition
		entry         corelib.NLSkillEntry
		score         int
		identityScore int
		guided        bool
	}
	var matches []scored
	considered := 0
	var hidden []string
	hiddenCount := 0
	for _, entry := range entries {
		if strings.TrimSpace(entry.Name) == "" {
			continue
		}
		considered++
		view := admissionSkillView(entry)
		if agentDomain != corelib.SkillDomainUniversal && !corelib.SkillVisibleInExperienceDomain(view.ExperienceDomain, agentDomain) {
			hiddenCount++
			if len(hidden) < experienceDomainLogSampleSize {
				hidden = append(hidden, strings.TrimSpace(entry.Name))
			}
			continue
		}
		guided, runnable := skillCompleteForAdmission(entry)
		if !guided && !runnable {
			continue
		}
		score, identity, identityScore := preferredLocalSkillScore(view, msg)
		if !identity && score < skillAdmissionMinScore {
			continue
		}
		if score <= 0 {
			continue
		}
		matches = append(matches, scored{
			def: view, entry: entry, score: score, identityScore: identityScore, guided: guided,
		})
	}
	if hiddenCount > 0 {
		log.Printf("[skill-domain] hid %d/%d skill(s) outside the %q experience pool (e.g. %s)",
			hiddenCount, considered, agentDomain, strings.Join(hidden, ", "))
	}
	if len(matches) == 0 {
		return nil, nil
	}
	best := matches[0]
	for _, item := range matches[1:] {
		if skillMatchOutranks(item.identityScore, item.score, item.guided, best.identityScore, best.score, best.guided) {
			best = item
		}
	}
	// An agent-guided workflow is the execution plan. A weaker runnable
	// recipe must not run beside it or send the turn to the market.
	if best.guided {
		return nil, []NLSkillDefinition{best.def}
	}
	// The host already picked an identity weight. A weaker name, or a skill
	// that only shares document words, must not be offered as another skill_*
	// the model can call and count as having used the selected skill.
	floor := best.identityScore
	bound := make([]admittedSkillBinding, 0, skillAdmissionMaxBound)
	used := map[string]bool{}
	for len(bound) < skillAdmissionMaxBound {
		var pick *scored
		for i := range matches {
			item := &matches[i]
			if item.guided || item.identityScore != floor || used[item.def.Name] {
				continue
			}
			if pick == nil || item.score > pick.score {
				pick = item
			}
		}
		if pick == nil {
			break
		}
		used[pick.def.Name] = true
		alias := skillAdmissionAlias(pick.entry, usedAliases(bound))
		binding := admittedSkillBinding{
			Alias:         alias,
			Name:          strings.TrimSpace(pick.entry.Name),
			StableID:      agentservice.DynamicSkillStableID(pick.entry),
			Version:       strings.TrimSpace(pick.entry.Version),
			ContentDigest: agentservice.DynamicSkillContentDigest(pick.entry),
			Description:   strings.TrimSpace(pick.def.Description),
		}
		bound = append(bound, binding)
	}
	return bound, nil
}

// admissionSkillView is the slice of an entry the scorer and the workflow
// prompt read. It is not the management-list projection.
func admissionSkillView(entry corelib.NLSkillEntry) NLSkillDefinition {
	triggers := entry.Triggers
	if triggers == nil {
		triggers = []string{}
	}
	steps := entry.Steps
	if steps == nil {
		steps = []corelib.NLSkillStep{}
	}
	return NLSkillDefinition{
		Name:             strings.TrimSpace(entry.Name),
		Description:      entry.Description,
		Triggers:         triggers,
		Steps:            steps,
		Status:           entry.Status,
		Source:           entry.Source,
		Type:             entry.Type,
		Content:          entry.Content,
		SkillDir:         entry.SkillDir,
		Params:           entry.Params,
		RequiredArgs:     entry.RequiredArgs,
		ExperienceDomain: corelib.NormalizeSkillExperienceDomain(entry.ExperienceDomain),
	}
}

func usedAliases(bound []admittedSkillBinding) map[string]bool {
	out := make(map[string]bool, len(bound))
	for _, item := range bound {
		out[item.Alias] = true
	}
	return out
}

func skillCompleteForAdmission(entry corelib.NLSkillEntry) (guided, runnable bool) {
	switch normalizeSkillEntryStatus(entry.Status) {
	case skillEntryStatusActive, skillEntryStatusUnknown:
	default:
		return false, false
	}
	if isShellBrowserAutomationSkillEntry(entry) {
		return false, false
	}
	if skill.IsAgentGuidedWorkflowSkill(&entry) && strings.TrimSpace(skill.AgentGuidedWorkflowInstructions(&entry)) != "" {
		return true, false
	}
	if skill.IsKnowledgeSkillType(entry.Type) || skill.IsInstructionOnlySkillType(entry.Type) {
		return false, false
	}
	report := skill.AssessRunnerCompatibility(&entry, skill.RunnerBackendGUI)
	return false, report.Runnable && !report.AgentGuidedOnly
}

func skillAdmissionAlias(entry corelib.NLSkillEntry, used map[string]bool) string {
	var b strings.Builder
	lastBreak := false
	for _, r := range strings.ToLower(strings.TrimSpace(entry.Name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastBreak = false
		case unicode.Is(unicode.Han, r):
			// Keep the alias ASCII. A Han name is distinguished by the stable id.
		default:
			if b.Len() > 0 && !lastBreak {
				b.WriteByte('_')
				lastBreak = true
			}
		}
	}
	slug := strings.Trim(b.String(), "_")
	if slug == "" {
		slug = strings.TrimPrefix(agentservice.DynamicSkillStableID(entry), "legacy:")
		slug = strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, strings.ToLower(slug))
	}
	if slug == "" {
		slug = "run"
	}
	alias := "skill_" + slug
	if !used[alias] {
		return alias
	}
	for n := 2; ; n++ {
		next := fmt.Sprintf("%s_%d", alias, n)
		if !used[next] {
			return next
		}
	}
}

func skillAdmissionToolDefinition(binding admittedSkillBinding) map[string]interface{} {
	desc := "执行宿主已选定的本机 Skill「" + binding.Name + "」。这个函数只运行这一个 Skill，不要传 Skill 名称，不要再搜索或安装，也不要改叫 manage_skill。"
	if binding.Description != "" {
		desc += " " + truncateTraceText(binding.Description, 160)
	}
	return toolDef(binding.Alias, desc, map[string]interface{}{
		"args": map[string]interface{}{
			"type":        "object",
			"description": "Skill 的业务参数。",
		},
	}, nil)
}

func skillRemoteSearchToolDefinition() map[string]interface{} {
	return toolDef("search_and_install_skill",
		"本机没有与当前任务匹配的完整 Skill。先调用这个工具，query 用当前任务原文。它会搜索、安装并运行最匹配的 Skill。不要先用 generate_pdf、office 或 bash 把任务做完，也不要改叫 manage_skill。天气、百科这类查询不要用这个工具。",
		map[string]interface{}{
			"query": map[string]interface{}{
				"type":        "string",
				"description": "当前任务原文。不要改写成搜索引擎问句。",
			},
		}, []string{"query"})
}

func (h *IMMessageHandler) appendAdmittedAgentGuidedSkillDocs(b *strings.Builder, userMessage string, loopCtx *LoopContext) {
	decision := h.skillAdmissionForTurn(loopCtx, userMessage)
	if decision.Mode != skillAdmissionGuided || len(decision.Guided) == 0 {
		return
	}
	matched := make([]matchedKnowledgeSkill, 0, len(decision.Guided))
	for _, def := range decision.Guided {
		content := skillDocContentForInjection(def)
		if content == "" {
			continue
		}
		matched = append(matched, matchedKnowledgeSkill{
			Name: def.Name, Content: content, ParamSchema: buildParamSchemaForSkill(def), Score: 1, AgentGuided: true,
		})
	}
	writeMatchedSkillDocs(b, matched, defaultKnowledgeSkillTokenBudget)
}

func (c *sharedAgentLoopCallbacks) clearSkillAdmission() {
	if c == nil {
		return
	}
	c.admittedSkillBindings = nil
	c.skillRemoteSearchOffered = false
	c.agentGuidedHostTools = false
}

func (c *sharedAgentLoopCallbacks) publishAdmittedSkillTools(userText string) {
	c.clearSkillAdmission()
	if c == nil || c.handler == nil || c.foregroundHandoffOnly() {
		return
	}
	decision := c.handler.skillAdmissionForTurn(c.loopCtx, userText)
	var defs []map[string]interface{}
	switch decision.Mode {
	case skillAdmissionBound:
		c.admittedSkillBindings = make(map[string]admittedSkillBinding, len(decision.Bound))
		for _, binding := range decision.Bound {
			if binding.Alias == "" {
				continue
			}
			c.admittedSkillBindings[binding.Alias] = binding
			defs = append(defs, skillAdmissionToolDefinition(binding))
		}
	case skillAdmissionGuided:
		c.agentGuidedHostTools = true
		defs = append(defs, c.handler.agentGuidedHostToolDefinitions()...)
	case skillAdmissionRemote:
		c.skillRemoteSearchOffered = true
		defs = append(defs, skillRemoteSearchToolDefinition())
	default:
		return
	}
	c.appendDiscoveredMCPDefs(defs)
}

func (h *IMMessageHandler) agentGuidedHostToolDefinitions() []map[string]interface{} {
	if h == nil || h.registry == nil {
		return nil
	}
	out := make([]map[string]interface{}, 0, len(agentGuidedHostToolNames))
	for _, name := range agentGuidedHostToolNames {
		registered, ok := h.registry.Get(name)
		if !ok || registered == nil || registered.Status != RegToolAvailable {
			continue
		}
		out = append(out, registeredToolToDef(*registered))
	}
	return out
}

func (c *sharedAgentLoopCallbacks) admittedSkillHostCall(name string) bool {
	if c == nil {
		return false
	}
	name = strings.TrimSpace(name)
	if _, ok := c.admittedSkillBindings[name]; ok {
		return true
	}
	if c.skillRemoteSearchOffered && name == "search_and_install_skill" {
		return true
	}
	if c.agentGuidedHostTools && isAgentGuidedHostToolName(name) {
		if c.semanticSurface != nil {
			if _, granted := c.semanticSurface.grants[name]; granted {
				// The semantic executor owns a name it already granted.
				// Visibility on a light prompt is skillAdmissionToolVisible.
				return false
			}
		}
		return true
	}
	return false
}

// skillAdmissionToolVisible is the prompt-profile view of an admitted skill.
// admittedSkillHostCall returns false for a guided file tool the planner
// already granted, so the semantic executor owns that call. The light
// allowlist would then hide bash and write_file. This predicate keeps those
// workflow tools callable. Bound and remote admission do not open them.
// generate_pdf stays a substitute.
func (c *sharedAgentLoopCallbacks) skillAdmissionToolVisible(name string) bool {
	if c == nil {
		return false
	}
	if c.admittedSkillHostCall(name) {
		return true
	}
	return c.skillAdmissionIsGuided() && isAgentGuidedHostToolName(name)
}

func (c *sharedAgentLoopCallbacks) executeAdmittedSkillHostCall(name, argsJSON string) (string, bool, bool) {
	if !c.admittedSkillHostCall(name) {
		return "", false, false
	}
	if c.handler == nil {
		return "[system rejected] skill runner is unavailable", true, false
	}
	name = strings.TrimSpace(name)
	if binding, ok := c.admittedSkillBindings[name]; ok {
		text, ran, dispatched := c.executeAdmittedSkillBinding(binding, argsJSON)
		if dispatched {
			c.admittedSkillSettled = true
		}
		return text, true, ran
	}
	if c.skillRemoteSearchOffered && name == "search_and_install_skill" {
		var args map[string]interface{}
		if strings.TrimSpace(argsJSON) != "" {
			_ = json.Unmarshal([]byte(argsJSON), &args)
		}
		if args == nil {
			args = map[string]interface{}{}
		}
		query, _ := args["query"].(string)
		if strings.TrimSpace(query) == "" {
			return "Missing query parameter.", true, false
		}
		// The host already decided which task has no local skill. A query
		// the model writes can be a lookup or a different job. Search the
		// admitted task. host_admitted only lets that task past the lookup
		// filter. It does not authorize whatever string arrived in args.
		if task := skillAdmissionTaskText(c.userText); task != "" {
			args["query"] = task
		}
		result := c.handler.executeSkillSearchInstallAdmitted(args, nil, true)
		c.admittedSkillSettled = true
		return result.Text, true, result.Success
	}
	text := c.executeToolWithoutSemanticSurface(name, argsJSON)
	rejected := skillHostResultRejected(text)
	if !rejected {
		c.admittedSkillSettled = true
	}
	return text, true, !rejected
}

// skillHostResultRejected reports a result the host refused before the tool
// did the work. A rejection quoted inside a file the tool actually read is
// not one of those, and must still count as using the workflow.
func skillHostResultRejected(text string) bool {
	trimmed := strings.TrimSpace(text)
	return strings.HasPrefix(trimmed, "[system rejected]") || strings.HasPrefix(trimmed, "Error")
}

func (c *sharedAgentLoopCallbacks) executeAdmittedSkillBinding(binding admittedSkillBinding, argsJSON string) (string, bool, bool) {
	exec := c.handler.getSkillExecutor()
	if exec == nil {
		return "[system rejected] skill runner is unavailable", false, false
	}
	arguments := map[string]interface{}{}
	if strings.TrimSpace(argsJSON) != "" {
		var parsed map[string]interface{}
		// JSON null unmarshals to a nil map without an error. Indexing it
		// panics, and it is not a skill argument object.
		if err := json.Unmarshal([]byte(argsJSON), &parsed); err != nil || parsed == nil {
			return "[system rejected] skill arguments are not an object", false, false
		}
		if nested, ok := parsed["args"].(map[string]interface{}); ok {
			arguments = nested
		} else {
			delete(parsed, "name")
			arguments = parsed
		}
	}
	output, err := exec.executeBoundSkill(binding.StableID, binding.Name, binding.Version, binding.ContentDigest, arguments)
	if err != nil {
		if strings.TrimSpace(output) == "" {
			return err.Error(), false, true
		}
		return output + "\n" + err.Error(), false, true
	}
	if strings.TrimSpace(output) == "" {
		return "Skill " + binding.Name + " finished.", true, true
	}
	return output, true, true
}

// noteGuidedSkillUse records that an agent-guided workflow actually ran a
// host file tool. A bash grant takes the semantic executor, so the admitted
// host-call path never sees it. A system rejection is not a use.
func (c *sharedAgentLoopCallbacks) noteGuidedSkillUse(name string) {
	c.noteGuidedSkillUseResult(name, "")
}

func (c *sharedAgentLoopCallbacks) noteGuidedSkillUseResult(name, text string) {
	if c == nil || c.admittedSkillSettled || !c.agentGuidedHostTools {
		return
	}
	if !isAgentGuidedHostToolName(name) || skillHostResultRejected(text) {
		return
	}
	c.admittedSkillSettled = true
}

func (c *sharedAgentLoopCallbacks) skillSurfaceAdmitted() bool {
	return c != nil && c.semanticSurface != nil &&
		(len(c.admittedSkillBindings) > 0 || c.agentGuidedHostTools || c.skillRemoteSearchOffered)
}

func (c *sharedAgentLoopCallbacks) skillAdmissionIsGuided() bool {
	return c != nil && c.agentGuidedHostTools && len(c.admittedSkillBindings) == 0 && !c.skillRemoteSearchOffered
}

// skillSubstituteBlocked refuses the generic tools that would finish a
// complex task without the skill the host already selected. File tools stay
// open for an agent-guided workflow, because those tools are how it runs.
// generate_pdf, office, and craft_tool stay closed for that whole turn.
func (c *sharedAgentLoopCallbacks) skillSubstituteBlocked(name string) (bool, string) {
	if !c.skillSurfaceAdmitted() {
		return false, ""
	}
	if skillAdmissionSubstitute(name, c.skillAdmissionIsGuided()) {
		if c.skillAdmissionIsGuided() || !c.admittedSkillSettled {
			return true, c.skillObligationReason()
		}
		return false, ""
	}
	if skillAdmissionLookupSubstitute(name) && !c.admittedSkillSettled {
		return true, c.skillObligationReason()
	}
	return false, ""
}

func skillAdmissionSubstitute(name string, guided bool) bool {
	switch strings.TrimSpace(name) {
	case "generate_pdf", "office", "craft_tool":
		return true
	case "bash", "write_file", "edit_file", "edit_lines":
		return !guided
	default:
		return false
	}
}

// skillAdmissionLookupSubstitute is a tool that can answer the turn without
// the skill. It stays closed until that skill or the market search has run,
// then opens, so a guided workflow can still look something up after it starts.
// generate_pdf stays closed for the whole guided turn; a lookup does not.
func skillAdmissionLookupSubstitute(name string) bool {
	switch strings.TrimSpace(name) {
	case "web_search", "web_fetch", "current_datetime":
		return true
	default:
		return false
	}
}

func (c *sharedAgentLoopCallbacks) skillObligationReason() string {
	if c.skillAdmissionIsGuided() {
		return "这个 Skill 要按使用文档用 bash、read_file、write_file、edit_file、list_directory 完成，不要改用 generate_pdf、office、craft_tool 或 web_search。"
	}
	if c.skillRemoteSearchOffered && len(c.admittedSkillBindings) == 0 {
		return "本机没有匹配的完整 Skill。请先调用 search_and_install_skill，query 用当前任务原文。不要先用 generate_pdf、office、bash 或 web_search 把任务做完。"
	}
	names := make([]string, 0, len(c.admittedSkillBindings))
	for alias := range c.admittedSkillBindings {
		names = append(names, alias)
	}
	sort.Strings(names)
	return "本机已有匹配的完整 Skill。请先调用 " + strings.Join(names, "、") + "，不要改用 generate_pdf、office、bash 或 web_search 完成同一件事。"
}

// SkillObligationPrompt is the one reminder when the model stops before using
// the skill the host selected. Empty after that call has been attempted.
func (c *sharedAgentLoopCallbacks) SkillObligationPrompt() string {
	if c == nil || c.admittedSkillSettled || !c.skillSurfaceAdmitted() {
		return ""
	}
	return "[系统] " + c.skillObligationReason()
}
