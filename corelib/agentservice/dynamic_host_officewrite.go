package agentservice

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/excel"
	coretool "github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	reviewedHostOfficeWriteProviderID     = "core-officewrite"
	reviewedHostOfficeWriteImplementation = "local"
	reviewedHostOfficeWriteAdapterName    = "host_document_write_office"
	reviewedHostOfficeWriteMaxSheets      = 32
	reviewedHostOfficeWriteMaxRows        = 1000
	reviewedHostOfficeWriteMaxCols        = 256
)

type reviewedHostOfficeWriter interface {
	WriteReviewedHostOffice(ctx context.Context, principal Principal, path string, data excel.WriteData) (string, error)
}

func reviewedHostOfficeWriteInvocationSchema() map[string]interface{} {
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
								"type":  "array",
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

func reviewedHostOfficeWriteContractDigest() string {
	return coretool.SchemaDigest([]byte("document.write.office:v1:host-officewrite-spreadsheet"))
}

// ProjectReviewedHostOfficeWriteProvider projects the host-owned workspace
// spreadsheet write. It is not a Skill/MCP discovery entry and must not
// import GUI office / write_excel. The closed schema accepts path and table
// data only. Channel, destination, group_name, action, and file_path are
// rejected. Word and presentation stay unpublished. The host process
// observes the write, so the handler result is the local completion receipt.
func ProjectReviewedHostOfficeWriteProvider(writer reviewedHostOfficeWriter) (coretool.ProviderSpec, map[string]interface{}, hostOwnedRuntimeBinding, error) {
	if writer == nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("host office writer is unavailable")
	}
	parameters := reviewedHostOfficeWriteInvocationSchema()
	authorization, err := coretool.NewParameterAuthorization(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, fmt.Errorf("authorize host office write schema: %w", err)
	}
	invocationDigest, err := dynamicHostInvocationDigest(parameters)
	if err != nil {
		return coretool.ProviderSpec{}, nil, hostOwnedRuntimeBinding{}, err
	}
	contractDigest := reviewedHostOfficeWriteContractDigest()
	bindingSchemaDigest := coretool.SchemaDigest([]byte(strings.Join([]string{
		"host-officewrite-path-sheets-v1", contractDigest, invocationDigest,
	}, "\x00")))
	provider := coretool.ProviderSpec{
		AdapterName: reviewedHostOfficeWriteAdapterName,
		Binding: coretool.ProviderBinding{
			Kind:             reviewedHostProviderKind,
			ProviderID:       reviewedHostOfficeWriteProviderID,
			ImplementationID: reviewedHostOfficeWriteImplementation,
			SchemaDigest:     bindingSchemaDigest,
		},
		ParameterAuthorization: authorization,
		Provides: []coretool.CapabilityProvision{{
			Capability: CapabilityOfficeWrite,
			Qualifiers: map[string]string{QualifierDocumentFormat: DocumentFormatSpreadsheet},
			Quality:    1,
		}},
		Effects: []coretool.EffectClass{coretool.EffectSensitive},
		Ready:   true,
	}
	definition := map[string]interface{}{
		"type": "function",
		"function": map[string]interface{}{
			"name":        "dynamic_provider",
			"description": "",
			"parameters":  parameters,
		},
	}
	return provider, definition, hostOwnedRuntimeBinding{execute: executeReviewedHostOfficeWrite(writer)}, nil
}

func AttachReviewedHostOfficeWriteProvider(catalog DynamicSemanticCatalog, writer reviewedHostOfficeWriter) (DynamicSemanticCatalog, error) {
	provider, definition, host, err := ProjectReviewedHostOfficeWriteProvider(writer)
	if err != nil {
		return DynamicSemanticCatalog{}, err
	}
	if err := catalog.add(provider, definition, dynamicSemanticRuntimeBinding{
		provider: provider.Binding,
		host:     &host,
	}); err != nil {
		return DynamicSemanticCatalog{}, err
	}
	return catalog, nil
}

func executeReviewedHostOfficeWrite(writer reviewedHostOfficeWriter) func(context.Context, Principal, map[string]interface{}) (string, error) {
	return func(ctx context.Context, principal Principal, args map[string]interface{}) (string, error) {
		if writer == nil {
			return "", fmt.Errorf("host_office_write_unavailable")
		}
		path, data, err := reviewedHostOfficeWriteArgsAllowed(args)
		if err != nil {
			return "", err
		}
		return writer.WriteReviewedHostOffice(ctx, principal, path, data)
	}
}

func reviewedHostOfficeWriteArgsAllowed(args map[string]interface{}) (string, excel.WriteData, error) {
	if len(args) > 2 {
		return "", excel.WriteData{}, fmt.Errorf("host_office_write_arguments_rejected")
	}
	path := ""
	var rawSheets interface{}
	hasPath, hasSheets := false, false
	for key, raw := range args {
		switch key {
		case "path":
			value, ok := raw.(string)
			if !ok {
				return "", excel.WriteData{}, fmt.Errorf("host_office_write_arguments_rejected")
			}
			path, hasPath = value, true
		case "sheets":
			rawSheets, hasSheets = raw, true
		default:
			return "", excel.WriteData{}, fmt.Errorf("host_office_write_arguments_rejected")
		}
	}
	if !hasPath || !hasSheets {
		return "", excel.WriteData{}, fmt.Errorf("host_office_write_arguments_rejected")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", excel.WriteData{}, fmt.Errorf("host_office_write_path_required")
	}
	data, err := reviewedHostOfficeSheetsToWriteData(rawSheets)
	if err != nil {
		return "", excel.WriteData{}, err
	}
	return path, data, nil
}

func reviewedHostOfficeSheetsToWriteData(raw interface{}) (excel.WriteData, error) {
	items, ok := raw.([]interface{})
	if !ok || len(items) == 0 || len(items) > reviewedHostOfficeWriteMaxSheets {
		return excel.WriteData{}, fmt.Errorf("host_office_write_sheets_rejected")
	}
	out := excel.WriteData{Sheets: make([]excel.WriteSheet, 0, len(items))}
	for _, item := range items {
		sheet, ok := item.(map[string]interface{})
		if !ok {
			return excel.WriteData{}, fmt.Errorf("host_office_write_sheets_rejected")
		}
		name := ""
		var rawRows interface{}
		hasName, hasRows := false, false
		for key, value := range sheet {
			switch key {
			case "name":
				text, ok := value.(string)
				if !ok {
					return excel.WriteData{}, fmt.Errorf("host_office_write_sheets_rejected")
				}
				name, hasName = strings.TrimSpace(text), true
			case "rows":
				rawRows, hasRows = value, true
			default:
				return excel.WriteData{}, fmt.Errorf("host_office_write_sheets_rejected")
			}
		}
		if !hasName || !hasRows || name == "" {
			return excel.WriteData{}, fmt.Errorf("host_office_write_sheets_rejected")
		}
		rows, err := reviewedHostOfficeRowsToWriteCells(rawRows)
		if err != nil {
			return excel.WriteData{}, err
		}
		out.Sheets = append(out.Sheets, excel.WriteSheet{Name: name, Rows: rows})
	}
	return out, nil
}

func reviewedHostOfficeRowsToWriteCells(raw interface{}) ([][]excel.WriteCell, error) {
	items, ok := raw.([]interface{})
	if !ok || len(items) > reviewedHostOfficeWriteMaxRows {
		return nil, fmt.Errorf("host_office_write_rows_rejected")
	}
	out := make([][]excel.WriteCell, 0, len(items))
	for _, item := range items {
		cells, ok := item.([]interface{})
		if !ok || len(cells) > reviewedHostOfficeWriteMaxCols {
			return nil, fmt.Errorf("host_office_write_rows_rejected")
		}
		row := make([]excel.WriteCell, 0, len(cells))
		for _, cell := range cells {
			text, ok := cell.(string)
			if !ok {
				return nil, fmt.Errorf("host_office_write_rows_rejected")
			}
			row = append(row, excel.WriteCell{Value: text})
		}
		out = append(out, row)
	}
	return out, nil
}

func (c *coreAgentCallbacks) WriteReviewedHostOffice(ctx context.Context, principal Principal, path string, data excel.WriteData) (string, error) {
	if c == nil || strings.TrimSpace(c.workspace) == "" {
		return "", fmt.Errorf("host_office_write_unavailable")
	}
	if strings.TrimSpace(principal.TenantID) != strings.TrimSpace(c.principal.TenantID) ||
		strings.TrimSpace(principal.UserID) != strings.TrimSpace(c.principal.UserID) {
		return "", fmt.Errorf("host_office_write_principal_mismatch")
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("host_office_write_path_required")
	}
	if len(data.Sheets) == 0 {
		return "", fmt.Errorf("host_office_write_sheets_rejected")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		default:
		}
	}
	absPath, err := c.resolveWorkspacePath(path)
	if err != nil {
		return "", err
	}
	if info, statErr := os.Stat(absPath); statErr == nil && info.IsDir() {
		return "", fmt.Errorf("host_office_write_path_is_directory")
	}
	if err := excel.WriteFile(absPath, data); err != nil {
		return "", err
	}
	display := reviewedHostWorkspaceRelative(c.workspace, absPath, path)
	return fmt.Sprintf("Wrote spreadsheet %s", display), nil
}
