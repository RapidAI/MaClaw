package agent

import (
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// shouldNudgeStoredRetrieval is the one-shot gate for a full turn that was
// given a side-effecting tool and then stopped before using any tool. Memory
// or knowledge search is still listed and unused. Read-only surfaces, including
// file reads, are left alone. The reply text is not inspected.
func shouldNudgeStoredRetrieval(light bool, tools []map[string]interface{}, history []ConversationEntry, nudges int) bool {
	if light || nudges >= 1 || turnUsedAnyTool(history) || !surfaceHasExecutionTool(tools) {
		return false
	}
	return retrievalToolsStillPending(tools, history)
}

func surfaceHasExecutionTool(tools []map[string]interface{}) bool {
	now := time.Now().UTC()
	for _, def := range tools {
		name := toolDefName(def)
		if name == "" || IsLightTurnToolAllowed(name) {
			continue
		}
		provision, ok := tool.LegacyAdapterProvisionForTool(name, now)
		if !ok {
			continue
		}
		for _, effect := range provision.Effects {
			if effect != tool.EffectReadOnly {
				return true
			}
		}
	}
	return false
}

func turnUsedAnyTool(history []ConversationEntry) bool {
	for _, entry := range history {
		if strings.EqualFold(strings.TrimSpace(entry.Role), "tool") || strings.TrimSpace(entry.ToolName) != "" {
			return true
		}
	}
	return false
}

// StoredOperationalFactNudge tells the model to read stored facts before
// asking the user, then continue with the tools already listed.
func StoredOperationalFactNudge() string {
	return "[系统] 先查记忆（memory），再查知识库（knowledge_search）。两边都没有，才向用户索要还缺的事实。只有一方有结果，或两边一致，直接用查到的内容继续原任务，不要再问用户。两边冲突时，只问用户确认哪一份。"
}

// retrievalToolsStillPending is true when this request still lists memory or
// knowledge_search and that tool has not run yet. With neither tool on the
// surface there is nothing to force.
func retrievalToolsStillPending(tools []map[string]interface{}, history []ConversationEntry) bool {
	wanted := map[string]bool{}
	for _, def := range tools {
		switch toolDefName(def) {
		case "memory", "memory_recall", "knowledge_search":
			wanted[toolDefName(def)] = true
		}
	}
	if len(wanted) == 0 {
		return false
	}
	for _, entry := range history {
		delete(wanted, strings.TrimSpace(entry.ToolName))
	}
	return len(wanted) > 0
}
