package main

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

func TestKnowledgeAutoRecallHeaderSentinelNonEmpty(t *testing.T) {
	t.Parallel()
	if !strings.Contains(agent.KnowledgeAutoRecallHeader, "知识库参考（自动检索）") {
		t.Fatalf("header sentinel missing: %q", agent.KnowledgeAutoRecallHeader)
	}
	if !strings.Contains(agent.EnterpriseKnowledgeAutoRecallHeader, "企业知识库参考（自动检索）") {
		t.Fatalf("enterprise header sentinel missing: %q", agent.EnterpriseKnowledgeAutoRecallHeader)
	}
}

func TestKnowledgeSearchSnippetUsesBestContentText(t *testing.T) {
	t.Parallel()
	r := knowledge.SearchResult{
		ResultType: "card",
		Claim:      "shared claim text",
		Summary:    "summary should lose",
		Snippet:    "snippet should lose",
	}
	got := knowledge.BestContentText(r)
	if got != "shared claim text" {
		t.Fatalf("snippet = %q, want claim", got)
	}
}
