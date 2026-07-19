package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"
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

var expertDefCache sync.Map // userID -> expertDefCacheEntry

// expertDefForUserID resolves the effective expert definition for a session
// userID (user override copy first, then builtin). Returns nil for non-expert
// sessions. Results are cached briefly to keep prompt/tool hot paths cheap.
func expertDefForUserID(userID string) *ExpertDefinition {
	id := expertIDFromUserID(userID)
	if id == "" {
		return nil
	}
	now := time.Now()
	if v, ok := expertDefCache.Load(userID); ok {
		if entry, ok := v.(expertDefCacheEntry); ok && now.Before(entry.expiresAt) {
			return entry.def
		}
	}
	def := loadExpertDefByID(id)
	expertDefCache.Store(userID, expertDefCacheEntry{def: def, expiresAt: now.Add(expertDefCacheTTL)})
	return def
}

// invalidateExpertDefCache drops the cached definition for one expert id.
func invalidateExpertDefCache(expertID string) {
	expertDefCache.Delete(expertSessionUserID(expertID))
}

// loadExpertDefByID reads the store, falling back to the builtin definition.
func loadExpertDefByID(id string) *ExpertDefinition {
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

// filterToolsForExpert applies the expert's tool allow-list. A nil definition
// or an empty Tools list means "all tools" and the input is returned as-is.
// Tool defs without an extractable name are kept (fail-open for unknown shapes).
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
