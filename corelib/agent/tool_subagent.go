package agent

import (
	"fmt"
	"strings"
)

// SubAgentSpec defines a specialized sub-agent with its own system prompt.
type SubAgentSpec struct {
	Name        string
	Description string
	Prompt      string
}

// BuiltinSubAgents defines legacy prompt-injection sub-agents. Coding work is
// intentionally not listed here because it needs a real host CodingSubAgent
// executor that can create files and verify results.
var BuiltinSubAgents = map[string]SubAgentSpec{
	"help": {
		Name:        "help",
		Description: "MaClaw usage help for features, configuration, and tool usage.",
		Prompt:      "You are a MaClaw usage help specialist. Answer questions about MaClaw features, configuration, and tools with concise practical steps.",
	},
}

// ToolDelegateTask handles the delegate_task tool call.
func ToolDelegateTask(args map[string]interface{}) string {
	agentName, _ := args["agent"].(string)
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		return ListSubAgents()
	}
	if strings.EqualFold(agentName, "coding_workflow") {
		return "[system rejected] delegate_task(coding_workflow) requires a host CodingSubAgent executor. Core context injection is disabled for coding_workflow because it cannot create or verify files."
	}

	spec, ok := BuiltinSubAgents[agentName]
	if !ok {
		return fmt.Sprintf("Unknown sub-agent: %s\n\n%s", agentName, ListSubAgents())
	}

	userRequest, _ := args["request"].(string)
	if strings.TrimSpace(userRequest) == "" {
		return fmt.Sprintf("Error: missing request parameter for %s.", spec.Name)
	}

	return fmt.Sprintf("__SUBAGENT_CONTEXT__\n[%s sub-agent context]\n\nDomain: %s\n\nGuidance:\n%s\n\nUser request: %s\n\nHandle the request using the guidance above.",
		spec.Name, spec.Description, spec.Prompt, userRequest)
}

// ListSubAgents returns a formatted list of available legacy sub-agents.
func ListSubAgents() string {
	var b strings.Builder
	b.WriteString("Available sub-agents:\n")
	for name, spec := range BuiltinSubAgents {
		b.WriteString(fmt.Sprintf("\n- **%s**: %s", name, spec.Description))
	}
	b.WriteString("\n\nUsage: delegate_task(agent=\"help\", request=\"question\")")
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
