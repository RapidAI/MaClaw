package main

import "strings"

type knowledgeToolActionKind string

const (
	knowledgeToolActionUnknown knowledgeToolActionKind = ""
	knowledgeToolActionScan    knowledgeToolActionKind = "scan"
	knowledgeToolActionReplace knowledgeToolActionKind = "replace"
	knowledgeToolActionCheck   knowledgeToolActionKind = "check"
)

func normalizeKnowledgeToolActionKind(action string) knowledgeToolActionKind {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "scan", "dry_run", "preview":
		return knowledgeToolActionScan
	case "replace", "set", "update":
		return knowledgeToolActionReplace
	case "check", "test":
		return knowledgeToolActionCheck
	default:
		return knowledgeToolActionUnknown
	}
}
