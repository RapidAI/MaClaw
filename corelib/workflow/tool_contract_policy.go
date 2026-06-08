package workflow

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/tooldef"
)

var artifactWriteExtensions = map[string]bool{
	".csv":      true,
	".doc":      true,
	".docx":     true,
	".html":     true,
	".jpeg":     true,
	".jpg":      true,
	".json":     true,
	".md":       true,
	".markdown": true,
	".pdf":      true,
	".png":      true,
	".ppt":      true,
	".pptx":     true,
	".svg":      true,
	".tsv":      true,
	".txt":      true,
	".xls":      true,
	".xlsx":     true,
}

var projectCodeExtensions = map[string]bool{
	".c":     true,
	".cc":    true,
	".cpp":   true,
	".cs":    true,
	".css":   true,
	".go":    true,
	".h":     true,
	".hpp":   true,
	".java":  true,
	".js":    true,
	".jsx":   true,
	".kt":    true,
	".mjs":   true,
	".php":   true,
	".ps1":   true,
	".py":    true,
	".rb":    true,
	".rs":    true,
	".scss":  true,
	".sh":    true,
	".sql":   true,
	".swift": true,
	".ts":    true,
	".tsx":   true,
	".vue":   true,
}

var projectControlFileNames = map[string]bool{
	".dockerignore":     true,
	".editorconfig":     true,
	".env":              true,
	".env.local":        true,
	".gitignore":        true,
	"Cargo.lock":        true,
	"Cargo.toml":        true,
	"CMakeLists.txt":    true,
	"Dockerfile":        true,
	"Gemfile":           true,
	"Gemfile.lock":      true,
	"Makefile":          true,
	"go.mod":            true,
	"go.sum":            true,
	"package-lock.json": true,
	"package.json":      true,
	"pnpm-lock.yaml":    true,
	"pyproject.toml":    true,
	"requirements.txt":  true,
	"tsconfig.json":     true,
	"vite.config.js":    true,
	"vite.config.ts":    true,
	"yarn.lock":         true,
}

var projectMutationToolNames = map[string]bool{
	"create_session": true,
	"delegate_task":  true,
	"edit_file":      true,
	"edit_lines":     true,
	"send_input":     true,
	"ssh":            true,
	"task":           true,
}

// DefaultMutationScopeForPolicy gives legacy policy-only callers a conservative
// scope. Full defaults to project for backward compatibility; template-aware
// callers should pass the real PhaseContract so artifact/full can be separated.
func DefaultMutationScopeForPolicy(policy ToolFilterPolicy) MutationScope {
	switch policy {
	case ToolFilterDocOnly, ToolFilterPlanning:
		return MutationScopeWorkflowDoc
	case ToolFilterOpsControlled:
		return MutationScopeOps
	case ToolFilterFull:
		return MutationScopeProject
	default:
		return MutationScopeUnknown
	}
}

// PhaseContractFromPolicy builds a minimal contract for legacy surfaces that
// only carry policy/scope metadata instead of a full template-derived contract.
func PhaseContractFromPolicy(policy ToolFilterPolicy, scope MutationScope) PhaseContract {
	if scope == MutationScopeUnknown {
		scope = DefaultMutationScopeForPolicy(policy)
	}
	return PhaseContract{
		ToolPolicy:    policy,
		MutationScope: scope,
	}
}

// IsToolAllowedByContract applies both the tool policy and the mutation scope.
func IsToolAllowedByContract(contract PhaseContract, name string) bool {
	name = strings.TrimSpace(name)
	if !IsToolAllowedByPolicy(contract.ToolPolicy, name) {
		return false
	}
	switch contract.MutationScope {
	case MutationScopeArtifact:
		return !projectMutationToolNames[name]
	case MutationScopeNone, MutationScopeWorkflowDoc:
		if name == "write_file" || projectMutationToolNames[name] {
			return false
		}
	}
	return true
}

// FilterToolDefinitionsByContract applies the workflow phase contract to
// LLM-facing tool definitions.
func FilterToolDefinitionsByContract(contract PhaseContract, tools []map[string]interface{}) []map[string]interface{} {
	if len(tools) == 0 {
		return tools
	}
	filtered := make([]map[string]interface{}, 0, len(tools))
	for _, def := range tools {
		if IsToolAllowedByContract(contract, tooldef.Name(def)) {
			filtered = append(filtered, def)
		}
	}
	return filtered
}

// RequiredToolNamesForContract returns required tools for the specific phase
// contract. Artifact phases should not pin local coding tools just because their
// policy is full.
func RequiredToolNamesForContract(contract PhaseContract) []string {
	if contract.ToolPolicy == ToolFilterFull && contract.MutationScope == MutationScopeArtifact {
		return []string{"read_file", "list_directory", "write_file", "send_file", "office", "generate_pdf"}
	}
	return RequiredToolNamesForPolicy(contract.ToolPolicy)
}

// ValidateToolCallByContract validates one concrete tool call against both
// tool policy and mutation scope.
func ValidateToolCallByContract(contract PhaseContract, name string, args map[string]interface{}) error {
	return ValidateToolCallByContractWithApproval(contract, name, args, nil)
}

// ValidateToolCallByContractWithApproval validates one concrete tool call and
// optional ops approval manifest against the complete phase contract.
func ValidateToolCallByContractWithApproval(contract PhaseContract, name string, args map[string]interface{}, approved []OpsApprovedCommand) error {
	name = strings.TrimSpace(name)
	if err := ValidateToolCallByPolicyWithApproval(contract.ToolPolicy, name, args, approved); err != nil {
		return err
	}
	return validateToolCallByMutationScope(contract.MutationScope, name, args)
}

func validateToolCallByMutationScope(scope MutationScope, name string, args map[string]interface{}) error {
	name = strings.TrimSpace(name)
	switch scope {
	case MutationScopeArtifact:
		return validateArtifactScopeToolCall(name, args)
	case MutationScopeNone, MutationScopeWorkflowDoc:
		return validateNonProjectScopeToolCall(scope, name, args)
	default:
		return nil
	}
}

func validateArtifactScopeToolCall(name string, args map[string]interface{}) error {
	if projectMutationToolNames[name] {
		return fmt.Errorf("%s is not allowed by mutation scope artifact", name)
	}
	switch name {
	case "write_file":
		path := strings.TrimSpace(stringArg(args, "path"))
		if path == "" {
			return fmt.Errorf("write_file path is required by mutation scope artifact")
		}
		if !isArtifactWritePath(path) {
			return fmt.Errorf("write_file path %q looks like project/source mutation, not an artifact deliverable", path)
		}
	case "bash":
		command := stringArg(args, "command")
		if strings.TrimSpace(command) == "" {
			return fmt.Errorf("bash command is required by mutation scope artifact")
		}
		if err := validateReadOnlyOpsCommand(command); err != nil {
			return fmt.Errorf("bash command is not allowed by mutation scope artifact: %w", err)
		}
	}
	return nil
}

func validateNonProjectScopeToolCall(scope MutationScope, name string, args map[string]interface{}) error {
	if name == "write_file" || projectMutationToolNames[name] {
		return fmt.Errorf("%s is not allowed by mutation scope %s", name, scope)
	}
	if name == "bash" {
		command := stringArg(args, "command")
		if strings.TrimSpace(command) == "" {
			return fmt.Errorf("bash command is required by mutation scope %s", scope)
		}
		if err := validateReadOnlyOpsCommand(command); err != nil {
			return fmt.Errorf("bash command is not allowed by mutation scope %s: %w", scope, err)
		}
	}
	return nil
}

func isArtifactWritePath(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	cleaned := filepath.ToSlash(filepath.Clean(path))
	base := filepath.Base(cleaned)
	if projectControlFileNames[base] {
		return false
	}
	ext := strings.ToLower(filepath.Ext(base))
	if projectCodeExtensions[ext] {
		return false
	}
	return artifactWriteExtensions[ext]
}
