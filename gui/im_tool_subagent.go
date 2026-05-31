package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// SubAgentSpec defines a legacy prompt-injection sub-agent.
type SubAgentSpec struct {
	Name        string
	Description string
	Prompt      string
}

// builtinSubAgents are only legacy context helpers. coding_workflow is handled
// by executeCodingWorkflowDelegateArgs and must never fall back to context-only
// fake activation.
var builtinSubAgents = map[string]SubAgentSpec{
	"help": {
		Name:        "help",
		Description: "MaClaw usage help for features, configuration, and tool usage.",
		Prompt:      "You are a MaClaw usage help specialist. Answer questions about MaClaw features, configuration, and tools with concise practical steps.",
	},
}

// toolDelegateTask handles the delegate_task tool call.
func (h *IMMessageHandler) toolDelegateTask(args map[string]interface{}) string {
	agentName, _ := args["agent"].(string)
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return h.listSubAgents()
	}

	userRequest, _ := args["request"].(string)
	if strings.TrimSpace(userRequest) == "" {
		return fmt.Sprintf("Error: missing request parameter for %s.", agentName)
	}

	ownerID, explicitRuntime := h.consumeRuntimePolicyOwnerIDFromToolArgsOrCurrentState(args)
	if ownerID == "" && explicitRuntime && strings.EqualFold(agentName, "coding_workflow") {
		return "Error: runtime owner is missing; isolated runtime will not fall back to desktop workflow owner."
	}
	if result, handled := h.executeCodingWorkflowDelegateArgs(args, agentLoopToolExecutionOptions{UserID: ownerID}); handled {
		return result.Text
	}

	if registry := h.getAgentRegistry(); registry != nil {
		if def := registry.Get(agentName); def != nil {
			return subAgentContextText(def.Name, def.Description, def.SystemPrompt, userRequest)
		}
	}

	spec, ok := builtinSubAgents[agentName]
	if !ok {
		return fmt.Sprintf("Unknown sub-agent: %s\n\n%s", agentName, h.listSubAgents())
	}
	return subAgentContextText(spec.Name, spec.Description, spec.Prompt, userRequest)
}

func subAgentContextText(name, description, prompt, request string) string {
	return fmt.Sprintf("__SUBAGENT_CONTEXT__\n[%s sub-agent context]\n\nDomain: %s\n\nGuidance:\n%s\n\nUser request: %s\n\nHandle the request using the guidance above.",
		name, description, prompt, request)
}

// getAgentRegistry returns the lazily-initialized agent registry.
// Scans ~/.maclaw/agents/ and <project>/.maclaw/agents/ for YAML definitions.
func (h *IMMessageHandler) getAgentRegistry() *agent.AgentRegistry {
	if h.app == nil {
		return nil
	}
	h.app.agentRegistryOnce.Do(func() {
		userDir := filepath.Join(corelib.MaclawBaseDir(), "agents")
		dirs := []string{userDir}
		if wd := corelib.EffectiveWorkspaceDir(); wd != "" {
			projectDir := filepath.Join(wd, ".maclaw", "agents")
			if info, err := os.Stat(projectDir); err == nil && info.IsDir() {
				dirs = append(dirs, projectDir)
			}
		}
		h.app.agentRegistry = agent.NewAgentRegistry(dirs...)
		_ = h.app.agentRegistry.Load()
	})
	return h.app.agentRegistry
}

func (h *IMMessageHandler) listSubAgents() string {
	var b strings.Builder
	b.WriteString("Available sub-agents:\n")
	b.WriteString("\n- **coding_workflow**: runs the internal CodingSubAgent for coding tasks")
	if registry := h.getAgentRegistry(); registry != nil {
		for _, def := range registry.List() {
			if strings.EqualFold(strings.TrimSpace(def.Name), "coding_workflow") {
				continue
			}
			b.WriteString(fmt.Sprintf("\n- **%s**: %s", def.Name, def.Description))
		}
	} else {
		for name, spec := range builtinSubAgents {
			b.WriteString(fmt.Sprintf("\n- **%s**: %s", name, spec.Description))
		}
	}
	b.WriteString("\n\nUsage: delegate_task(agent=\"coding_workflow\", request=\"coding task\")")
	return b.String()
}

// IsSubAgentContext checks if a tool result contains sub-agent context injection.
func IsSubAgentContext(result string) bool {
	return strings.HasPrefix(result, "__SUBAGENT_CONTEXT__")
}

// ExtractSubAgentContext extracts the context from a sub-agent result.
func ExtractSubAgentContext(result string) string {
	return strings.TrimPrefix(result, "__SUBAGENT_CONTEXT__\n")
}
