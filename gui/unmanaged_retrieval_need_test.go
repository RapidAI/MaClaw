package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

func TestMergeAmbientRetrievalToolsFollowsNeedsNotNamePin(t *testing.T) {
	knowledge := agent.ToolDef("knowledge_search", "search kb", map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, []string{"query"})
	memory := agent.ToolDef("memory", "memory", map[string]interface{}{"action": map[string]interface{}{"type": "string"}}, nil)
	codingKB := agent.ToolDef("coding_knowledge_search", "coding kb", map[string]interface{}{"query": map[string]interface{}{"type": "string"}}, []string{"query"})
	clock := agent.ToolDef("current_datetime", "clock", nil, nil)
	allTools := []map[string]interface{}{knowledge, memory, codingKB, clock}

	got := mergeAmbientRetrievalTools([]map[string]interface{}{clock}, allTools)
	names := make(map[string]bool, len(got))
	for _, item := range got {
		names[extractToolName(item)] = true
	}
	if !names["current_datetime"] || !names["knowledge_search"] || !names["memory"] {
		t.Fatalf("Need path must add warehouse tools: %#v", names)
	}
	if names["coding_knowledge_search"] {
		t.Fatal("coding_knowledge_search is not an ambient Need")
	}
}

func TestUnmanagedRetrievalToolForNeedDoesNotPinCodingKnowledge(t *testing.T) {
	if unmanagedRetrievalToolForNeed(tool.CapabilityKnowledgeReadLocal) != "knowledge_search" {
		t.Fatalf("knowledge need mapped to %q", unmanagedRetrievalToolForNeed(tool.CapabilityKnowledgeReadLocal))
	}
	if unmanagedRetrievalToolForNeed(tool.CapabilityMemoryRecallAgent) != "memory" {
		t.Fatalf("recall need mapped to %q", unmanagedRetrievalToolForNeed(tool.CapabilityMemoryRecallAgent))
	}
	if unmanagedRetrievalToolForNeed("repo.inspect.vcs") != "" {
		t.Fatal("non-warehouse capability must not map to a pin")
	}
}

func TestBtwSystemPromptDoesNotDumpProactiveRecall(t *testing.T) {
	prompt := buildBtwSystemPrompt(&IMMessageHandler{}, "what did we decide about the gateway")
	if strings.Contains(prompt, "自动召回") || strings.Contains(prompt, agent.KnowledgeAutoRecallHeader) {
		t.Fatalf("/btw must not dump warehouse bodies:\n%s", prompt)
	}
	if !strings.Contains(prompt, `memory(action="recall")`) {
		t.Fatalf("/btw must still tell the model to recall via tools:\n%s", prompt)
	}
}
