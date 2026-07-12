package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

// TestVEKnowledgeAutoRecallPolicyMatchesSharedAgentConstants guards against
// VE drifting back to hard-coded inject counts / thresholds that diverge from
// IM, TUI, and agentservice (see KnowledgeAutoRecallMaxInject).
func TestVEKnowledgeAutoRecallPolicyMatchesSharedAgentConstants(t *testing.T) {
	t.Parallel()

	if agent.KnowledgeAutoRecallScoreThreshold != 0.3 {
		t.Fatalf("score threshold = %v, want 0.3 (shared policy)", agent.KnowledgeAutoRecallScoreThreshold)
	}
	if agent.KnowledgeAutoRecallSearchLimit < 5 {
		t.Fatalf("search limit = %d, want >= 5", agent.KnowledgeAutoRecallSearchLimit)
	}
	if agent.KnowledgeAutoRecallSnippetMaxRunes < 200 {
		t.Fatalf("snippet max = %d, want >= 200", agent.KnowledgeAutoRecallSnippetMaxRunes)
	}

	// Shared ladder: strong / medium / weak / none
	cases := []struct {
		top  float64
		want int
	}{
		{3.5, 5},
		{1.5, 3},
		{0.5, 2},
		{0.1, 0},
	}
	for _, tc := range cases {
		if got := agent.KnowledgeAutoRecallMaxInject(tc.top); got != tc.want {
			t.Fatalf("MaxInject(%.1f) = %d, want %d", tc.top, got, tc.want)
		}
	}
}

func TestKnowledgeAutoRecallSnippetUsesBestContentText(t *testing.T) {
	t.Parallel()
	r := knowledge.SearchResult{
		ResultType: "card",
		Claim:      "shared claim text",
		Summary:    "summary should lose",
		Snippet:    "snippet should lose",
	}
	got := knowledgeAutoRecallSnippet(r)
	if got != "shared claim text" {
		t.Fatalf("snippet = %q, want claim", got)
	}
}

func TestKnowledgeAutoRecallHeaderAndNoMatchHintNonEmpty(t *testing.T) {
	t.Parallel()
	if !strings.Contains(agent.KnowledgeAutoRecallHeader, "知识库") {
		t.Fatalf("header missing knowledge section: %q", agent.KnowledgeAutoRecallHeader)
	}
	if !strings.Contains(agent.KnowledgeAutoRecallNoMatchHint, "knowledge_search") {
		t.Fatalf("no-match hint should point at knowledge tools: %q", agent.KnowledgeAutoRecallNoMatchHint)
	}
}
