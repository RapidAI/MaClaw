package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// ---------------------------------------------------------------------------
// Expert session policy: resolve an expert persona from a session userID and
// apply its tool/skill allow-lists.
//
// Expert sessions use userID "desktop-user:expert:<expertId>" (see
// SendAIAssistantMessage). The persona swap itself lives in im_system_prompt.go;
// this file owns the reverse lookup plus the allow-list filters.
// ---------------------------------------------------------------------------

// expertSessionUserIDPrefix prefixes expert session userIDs.
const expertSessionUserIDPrefix = desktopUserID + ":expert:"

// expertSessionUserID builds the session userID for an expert tab.
func expertSessionUserID(expertID string) string {
	return expertSessionUserIDPrefix + strings.TrimSpace(expertID)
}

// expertTabSessionID mirrors the frontend expertTabId() (expertTypes.ts):
// expert tab session files (tab_<id>.json) are keyed by this id.
func expertTabSessionID(expertID string) string {
	return "expert-" + strings.TrimSpace(expertID)
}

// expertIDFromUserID extracts the expert id from an expert session userID;
// returns "" for non-expert userIDs.
func expertIDFromUserID(userID string) string {
	userID = strings.TrimSpace(userID)
	if !strings.HasPrefix(userID, expertSessionUserIDPrefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(userID, expertSessionUserIDPrefix))
}

// expertDefCacheTTL bounds how long a resolved expert definition is reused.
// Edits invalidate explicitly; the TTL is only a safety net.
const expertDefCacheTTL = 10 * time.Second

type expertDefCacheEntry struct {
	def       *ExpertDefinition
	expiresAt time.Time
}

var expertDefCache sync.Map           // userID -> expertDefCacheEntry
var assistantBindingByUserID sync.Map // scoped userID -> *assistantBindingTurnScope

// assistantBindingTurnScope holds immutable, turn-local binding policy. In
// particular, ExpertDef is resolved before the loop starts. If an administrator
// deletes that expert during a running turn, the rest of the turn retains the
// same restrictive tool/skill policy instead of becoming an unrestricted
// general session after cache invalidation.
type assistantBindingTurnScope struct {
	binding   agent.AssistantBinding
	expertDef *ExpertDefinition
}

// cloneAssistantBinding makes transport-owned binding metadata safe to carry
// into an asynchronous follow-up. The policy must never borrow mutable slices
// from the inbound message or allow a synthetic command to mutate its parent.
func cloneAssistantBinding(binding *agent.AssistantBinding) *agent.AssistantBinding {
	if binding == nil {
		return nil
	}
	clone := *binding
	clone.DocumentDirectories = append([]string(nil), binding.DocumentDirectories...)
	clone.AllowedDirectories = append([]string(nil), binding.AllowedDirectories...)
	return &clone
}

// prepareAssistantBindingTurn validates and snapshots a binding without making
// it visible to policy lookups yet. Normal IM turns acquire their per-session
// serialization lock later in the entry path; publishing earlier would let a
// queued same-session turn temporarily replace the active turn's policy.
func prepareAssistantBindingTurn(msg IMUserMessage) (*assistantBindingTurnScope, string) {
	if msg.AssistantBinding == nil || strings.TrimSpace(msg.UserID) == "" {
		return nil, ""
	}
	binding := *cloneAssistantBinding(msg.AssistantBinding)
	scope := assistantBindingTurnScope{binding: binding}
	if strings.EqualFold(strings.TrimSpace(binding.Mode), corelib.LansengerAssistantModeExpert) {
		expertID := strings.TrimSpace(binding.ExpertID)
		if expertID == "" {
			return nil, unavailableAssistantBindingExpertMessage
		}
		scope.expertDef = loadExpertDefByID(expertID)
		if scope.expertDef == nil {
			return nil, unavailableAssistantBindingExpertMessage
		}
	}
	return &scope, ""
}

func activateAssistantBindingForTurn(userID string, scope *assistantBindingTurnScope) func() {
	userID = strings.TrimSpace(userID)
	if scope == nil || userID == "" {
		return func() {}
	}
	// Store the scope pointer rather than the value: a binding contains directory
	// slices and is therefore not comparable. Pointer identity also ensures a
	// stale cleanup can never remove a newer turn's scope.
	assistantBindingByUserID.Store(userID, scope)
	return func() { assistantBindingByUserID.CompareAndDelete(userID, scope) }
}

// bindAssistantForTurn is retained for direct callers and tests. The normal
// IM entry path uses prepare + activate after acquiring session serialization.
func bindAssistantForTurn(msg IMUserMessage) (func(), string) {
	scope, errText := prepareAssistantBindingTurn(msg)
	if errText != "" {
		return func() {}, errText
	}
	return activateAssistantBindingForTurn(msg.UserID, scope), ""
}

func assistantBindingForUserID(userID string) *agent.AssistantBinding {
	if value, ok := assistantBindingByUserID.Load(strings.TrimSpace(userID)); ok {
		if scope, ok := value.(*assistantBindingTurnScope); ok && scope != nil {
			binding := scope.binding
			return &binding
		}
	}
	return nil
}

func assistantBindingExpertDefForUserID(userID string) *ExpertDefinition {
	if value, ok := assistantBindingByUserID.Load(strings.TrimSpace(userID)); ok {
		if scope, ok := value.(*assistantBindingTurnScope); ok && scope != nil {
			return scope.expertDef
		}
	}
	return nil
}

const unavailableAssistantBindingExpertMessage = "绑定的 AI 专家已不可用，请在蓝信机器人设置中重新选择。"

// expertDefForUserID resolves the effective expert definition for a session
// userID (user override copy first, then builtin). Returns nil for non-expert
// sessions. Results are cached briefly to keep prompt/tool hot paths cheap.
func expertDefForUserID(userID string) *ExpertDefinition {
	id := expertIDFromUserID(userID)
	if id == "" {
		if binding := assistantBindingForUserID(userID); binding != nil && strings.EqualFold(binding.Mode, corelib.LansengerAssistantModeExpert) {
			// Binding turns use the definition snapshotted at admission. This is
			// deliberate: it retains restrictions through a concurrent delete.
			return assistantBindingExpertDefForUserID(userID)
		}
	}
	if id == "" {
		return nil
	}
	cacheKey := userID + "\x00" + id
	now := time.Now()
	if v, ok := expertDefCache.Load(cacheKey); ok {
		if entry, ok := v.(expertDefCacheEntry); ok && now.Before(entry.expiresAt) {
			return entry.def
		}
	}
	def := loadExpertDefByID(id)
	expertDefCache.Store(cacheKey, expertDefCacheEntry{def: def, expiresAt: now.Add(expertDefCacheTTL)})
	return def
}

// invalidateExpertDefCache drops every cached definition for one expert id.
// Cache keys are scoped by session userID so one expert can be used by several
// independent bot profiles. Deleting only the desktop session key would leave
// those profile caches live until their TTL expires.
func invalidateExpertDefCache(expertID string) {
	expertID = strings.TrimSpace(expertID)
	if expertID == "" {
		return
	}
	suffix := "\x00" + expertID
	expertDefCache.Range(func(key, _ interface{}) bool {
		if cacheKey, ok := key.(string); ok && strings.HasSuffix(cacheKey, suffix) {
			expertDefCache.Delete(key)
		}
		return true
	})
}

// loadExpertDefByID reads the store, falling back to the builtin definition.
func loadExpertDefByID(id string) *ExpertDefinition {
	if isManagedIndustryExpert(id) && !isActiveManagedIndustryExpert(id) {
		return nil
	}
	if def, ok, err := defaultExpertStore.Get(id); err == nil && ok {
		cp := def
		return &cp
	} else if err != nil {
		log.Printf("[expert] read store for %q failed: %v", id, err)
	}
	return builtinExpertByID(id)
}

// expertAlwaysKeptTools are never filtered out of an expert session: without
// ask_user the agent cannot interact, without manage_skill bound skills cannot
// run, and without discover_tool deferred tools stay unreachable.
var expertAlwaysKeptTools = map[string]bool{
	"manage_skill":  true,
	"ask_user":      true,
	"discover_tool": true,
}

// expertToolAllowSet builds the effective tool allow-set for an expert:
// its whitelist plus the always-kept interaction/skill tools.
func expertToolAllowSet(def *ExpertDefinition) map[string]bool {
	allow := make(map[string]bool, len(def.Tools)+len(expertAlwaysKeptTools))
	for _, name := range def.Tools {
		if n := strings.TrimSpace(name); n != "" {
			allow[n] = true
		}
	}
	for name := range expertAlwaysKeptTools {
		allow[name] = true
	}
	return allow
}

// expertSkillAllowed reports whether a skill name is inside the expert's
// skill allow-list (empty list = all skills allowed).
func expertSkillAllowed(def *ExpertDefinition, skillName string) bool {
	if def == nil || len(def.Skills) == 0 {
		return true
	}
	for _, name := range def.Skills {
		if strings.TrimSpace(name) == skillName {
			return true
		}
	}
	return false
}

// expertToolNameAllowListAppliesToManagedSemantic is false because
// ExpertDefinition.Tools is a legacy function-name list. There is no
// reviewed control-plane name→capability mapping, so a managed turn must
// ignore that list: the model-visible surface comes only from ToolPlanner
// plus CatalogRenderer. The name filter remains for unmigrated legacy loops.
func expertToolNameAllowListAppliesToManagedSemantic() bool {
	return false
}

// filterToolsForExpert applies the expert's tool allow-list. A nil definition
// or an empty Tools list means "all tools" and the input is returned as-is.
// Tool defs without an extractable name are kept (fail-open for unknown shapes).
// Do not call this on a managed semantic surface.
func filterToolsForExpert(tools []map[string]interface{}, def *ExpertDefinition) []map[string]interface{} {
	if def == nil || len(def.Tools) == 0 || len(tools) == 0 {
		return tools
	}
	allow := expertToolAllowSet(def)
	out := make([]map[string]interface{}, 0, len(tools))
	for _, td := range tools {
		name := extractToolName(td)
		if name == "" || allow[name] {
			out = append(out, td)
		}
	}
	return out
}

// ---------------------------------------------------------------------------
// Execution-layer enforcement. The visibility-layer filters above only hide
// tools from the LLM's tool list; a determined model can still emit a call by
// name. expertToolCallRejectionWithDef is the hard gate evaluated at dispatch
// time (see executeAgentLoopToolCall / executeBonusRoundTool).
// ---------------------------------------------------------------------------

// expertToolCallRejectionWithDef returns a non-empty rejection text when the
// tool call falls outside the expert's allow-lists; "" means allowed.
// Pure function — the def resolution lives in expertToolExecutionRejection.
func expertToolCallRejectionWithDef(def *ExpertDefinition, toolName, argsJSON string) string {
	if def == nil {
		return ""
	}
	toolName = strings.TrimSpace(toolName)
	if len(def.Tools) > 0 && !expertToolAllowSet(def)[toolName] {
		return fmt.Sprintf("[system rejected] 该专家未授权工具 %s，本次调用已被拦截。如需该能力，请在专家设置中将其加入工具白名单。", toolName)
	}
	// manage_skill is always tool-allowed (skills are the expert's execution
	// entry point), so skill gating lives here on the run branch instead.
	if len(def.Skills) > 0 && toolName == "manage_skill" {
		var args struct {
			Action string `json:"action"`
			Name   string `json:"name"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err == nil {
			if strings.EqualFold(strings.TrimSpace(args.Action), "run") {
				if skill := strings.TrimSpace(args.Name); skill != "" && !expertSkillAllowed(def, skill) {
					return fmt.Sprintf("[system rejected] 该专家未授权技能 %s，本次调用已被拦截。如需该技能，请在专家设置中将其加入技能白名单。", skill)
				}
			}
		}
	}
	return ""
}

// expertToolExecutionRejection resolves the expert for userID and evaluates
// the dispatch-time gate. Returns "" for non-expert sessions.
func expertToolExecutionRejection(userID, toolName, argsJSON string) string {
	return expertToolCallRejectionWithDef(expertDefForUserID(userID), toolName, argsJSON)
}

// filterToolsForExpertUser resolves the expert for userID and filters tools.
func (h *IMMessageHandler) filterToolsForExpertUser(userID string, tools []map[string]interface{}) []map[string]interface{} {
	def := expertDefForUserID(userID)
	if def == nil || len(def.Tools) == 0 {
		return tools
	}
	before := len(tools)
	out := filterToolsForExpert(tools, def)
	if len(out) != before {
		log.Printf("[expert] tool allow-list user=%q expert=%q tools=%d->%d", userID, def.ID, before, len(out))
	}
	return out
}

// filterSkillsForExpert applies the expert's skill allow-list. A nil
// definition or an empty Skills list means "all skills".
func filterSkillsForExpert(skills []NLSkillDefinition, def *ExpertDefinition) []NLSkillDefinition {
	if def == nil || len(def.Skills) == 0 || len(skills) == 0 {
		return skills
	}
	allow := make(map[string]bool, len(def.Skills))
	for _, name := range def.Skills {
		if n := strings.TrimSpace(name); n != "" {
			allow[n] = true
		}
	}
	out := make([]NLSkillDefinition, 0, len(skills))
	for _, s := range skills {
		if allow[s.Name] {
			out = append(out, s)
		}
	}
	return out
}

// filterSkillsForExpertUser resolves the expert for userID and filters skills.
func (h *IMMessageHandler) filterSkillsForExpertUser(userID string, skills []NLSkillDefinition) []NLSkillDefinition {
	def := expertDefForUserID(userID)
	if def == nil || len(def.Skills) == 0 {
		return skills
	}
	before := len(skills)
	out := filterSkillsForExpert(skills, def)
	if len(out) != before {
		log.Printf("[expert] skill allow-list user=%q expert=%q skills=%d->%d", userID, def.ID, before, len(out))
	}
	return out
}
