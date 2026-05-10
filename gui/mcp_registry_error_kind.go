package main

import "strings"

type mcpRegistryErrorKind int

const (
	mcpRegistryErrorUnknown mcpRegistryErrorKind = iota
	mcpRegistryErrorAlreadyExists
)

func classifyMCPRegistryError(err error) mcpRegistryErrorKind {
	if err == nil {
		return mcpRegistryErrorUnknown
	}
	if strings.Contains(err.Error(), "already exists") {
		return mcpRegistryErrorAlreadyExists
	}
	return mcpRegistryErrorUnknown
}

func (k mcpRegistryErrorKind) IsDuplicate() bool {
	return k == mcpRegistryErrorAlreadyExists
}
