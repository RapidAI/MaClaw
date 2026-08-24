package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	semanticTrustedOfficeWriteAdapter        = "semantic_write_trusted_office"
	semanticTrustedOfficeWriteImplementation = "trusted-office-write-v1"
)

func semanticUnpublishedLegacyOfficeProvider(registered RegisteredTool) bool {
	for _, provision := range registered.CapabilityProvisions {
		if provision.Capability == tool.CapabilityDocumentWriteOffice {
			return true
		}
	}
	return false
}

func semanticTrustedOfficeWriteDefinition() map[string]interface{} {
	return map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        semanticTrustedOfficeWriteAdapter,
			"description": "Write a spreadsheet into a workspace path. Only path and table data are accepted.",
			"parameters":  semanticTrustedOfficeWriteInvocationSchema(),
		},
	}
}

func semanticTrustedOfficeWriteInvocationSchema() map[string]interface{} {
	return map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{"type": "string"},
			"sheets": map[string]interface{}{
				"type": "array",
				"items": map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"name": map[string]interface{}{"type": "string"},
						"rows": map[string]interface{}{
							"type": "array",
							"items": map[string]interface{}{
								"type": "array",
								"items": map[string]interface{}{"type": "string"},
							},
						},
					},
					"required":             []string{"name", "rows"},
					"additionalProperties": false,
				},
			},
		},
		"required":             []string{"path", "sheets"},
		"additionalProperties": false,
	}
}

func semanticTrustedOfficeWriteArgsAllowed(args map[string]interface{}) (path string, data map[string]interface{}, err error) {
	if len(args) > 2 {
		return "", nil, fmt.Errorf("trusted_office_write_arguments_rejected")
	}
	hasPath, hasSheets := false, false
	var sheets interface{}
	for key, raw := range args {
		switch key {
		case "path":
			value, ok := raw.(string)
			if !ok {
				return "", nil, fmt.Errorf("trusted_office_write_arguments_rejected")
			}
			path, hasPath = value, true
		case "sheets":
			sheets, hasSheets = raw, true
		default:
			return "", nil, fmt.Errorf("trusted_office_write_arguments_rejected")
		}
	}
	if !hasPath || !hasSheets {
		return "", nil, fmt.Errorf("trusted_office_write_arguments_rejected")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", nil, fmt.Errorf("trusted_office_write_path_required")
	}
	return path, map[string]interface{}{"sheets": sheets}, nil
}

func (h *IMMessageHandler) writeTrustedOffice(principalID, path string, data map[string]interface{}) (string, error) {
	if h == nil {
		return "", fmt.Errorf("trusted_office_write_unavailable")
	}
	principalID = strings.TrimSpace(principalID)
	if principalID == "" {
		return "", fmt.Errorf("trusted_office_write_principal_required")
	}
	if h.semanticTrustedOfficeWrite != nil {
		return h.semanticTrustedOfficeWrite(principalID, path, data)
	}
	workspace := trustedPrincipalBoundWorkspace(h, principalID)
	absPath, err := trustedFileWriteResolvePath(workspace, path)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(absPath); statErr == nil && info.IsDir() {
		return "", fmt.Errorf("trusted_office_write_path_is_directory")
	}
	if _, err := agent.WriteExcelDetailed(map[string]interface{}{"path": absPath, "data": data}); err != nil {
		return "", fmt.Errorf("trusted_office_write_failed: %w", err)
	}
	display := trustedFileWriteDisplayPath(workspace, absPath, path)
	return fmt.Sprintf("Wrote spreadsheet %s", display), nil
}

func semanticTrustedOfficeWriteResultProjection(text string) (string, error) {
	if strings.Contains(text, "[voice_base64") || strings.Contains(text, "[file_base64") {
		return "", fmt.Errorf("trusted_office_write_delivery_token")
	}
	if strings.Contains(text, "\"action\"") || strings.Contains(text, "write_excel") {
		return "", fmt.Errorf("trusted_office_write_legacy_name")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("trusted_office_write_empty")
	}
	return text, nil
}
