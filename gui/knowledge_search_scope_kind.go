package main

import (
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

type knowledgeSearchScopeKind string

const (
	knowledgeSearchScopeUnknown   knowledgeSearchScopeKind = ""
	knowledgeSearchScopeProject   knowledgeSearchScopeKind = knowledgeSearchScopeKind(knowledge.SaveScopeProject)
	knowledgeSearchScopePersonal  knowledgeSearchScopeKind = knowledgeSearchScopeKind(knowledge.SaveScopePersonal)
	knowledgeSearchScopeLocalOnly knowledgeSearchScopeKind = knowledgeSearchScopeKind(knowledge.SaveScopeLocalOnly)
	knowledgeSearchScopeLocal     knowledgeSearchScopeKind = "local"
	knowledgeSearchScopeAll       knowledgeSearchScopeKind = "all"
)

func normalizeKnowledgeSearchScopeKind(value string) knowledgeSearchScopeKind {
	switch knowledgeSearchScopeKind(strings.ToLower(strings.TrimSpace(value))) {
	case knowledgeSearchScopeProject:
		return knowledgeSearchScopeProject
	case knowledgeSearchScopePersonal:
		return knowledgeSearchScopePersonal
	case knowledgeSearchScopeLocalOnly:
		return knowledgeSearchScopeLocalOnly
	case knowledgeSearchScopeLocal:
		return knowledgeSearchScopeLocal
	case knowledgeSearchScopeAll:
		return knowledgeSearchScopeAll
	default:
		return knowledgeSearchScopeUnknown
	}
}

func (scope knowledgeSearchScopeKind) ClearsProjectPath() bool {
	switch scope {
	case knowledgeSearchScopePersonal, knowledgeSearchScopeLocalOnly, knowledgeSearchScopeLocal, knowledgeSearchScopeAll:
		return true
	default:
		return false
	}
}
