package database

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/excel"
)

// readTableAction implements the host-neutral portion of read_table. Hosts
// may still provide a richer office adapter, but the core/TUI path now has
// identical bounded and typed behavior for local spreadsheets.
func readTableAction(ctx context.Context, manager *Manager, args map[string]interface{}) (interface{}, error) {
	filePath := stringArg(args, "file_path")
	trustedProfilePath := false
	sheet := stringArg(args, "sheet")
	if sheet == "" {
		sheet = stringArg(args, "table")
	}
	rangeText := stringArg(args, "range")
	limit := intArg(args, "limit", 100)
	if limit < 1 || limit > 5000 {
		return nil, fmt.Errorf("quota_exceeded: limit must be between 1 and 5000")
	}
	if filePath == "" {
		id := stringArg(args, "connection_id")
		adapter, ok := manager.AdapterFor(id, stringArg(args, "_owner_id"), stringArg(args, "_session_id"))
		if !ok {
			return nil, fmt.Errorf("connection_not_found")
		}
		if profileID := manager.ProfileIDForConnection(id); profileID != "" && !manager.operationAllowed(profileID, "read_table") && !manager.operationAllowed(profileID, "query") {
			return nil, fmt.Errorf("permission: operation is not allowed by profile")
		}
		if ea, ok := adapter.(*excelAdapter); ok {
			if profileID := manager.ProfileIDForConnection(id); profileID != "" && !manager.operationAllowed(profileID, "read_table") && !manager.operationAllowed(profileID, "query") {
				return nil, fmt.Errorf("permission: operation is not allowed by profile")
			}
			filePath, sheet = ea.profile.FilePath, firstNonEmpty(sheet, ea.profile.Sheet)
			trustedProfilePath = true
		} else {
			if !safeTableName(stringArg(args, "table")) {
				return nil, fmt.Errorf("syntax: table is required")
			}
			result, err := adapter.Query(ctx, QueryRequest{SQL: "SELECT * FROM " + quoteIdentifier(stringArg(args, "table")), Limit: limit, Timeout: intArg(args, "timeout_seconds", 30)})
			return result, err
		}
	}
	if manager != nil {
		resolved, err := manager.resolvePath(filePath, trustedProfilePath)
		if err != nil {
			return nil, err
		}
		filePath = resolved
	} else if !trustedProfilePath {
		if err := validateLocalDataPath(filePath); err != nil {
			return nil, err
		}
	}
	password := ""
	if manager != nil {
		if id := stringArg(args, "connection_id"); id != "" {
			if adapter, ok := manager.AdapterFor(id, stringArg(args, "_owner_id"), stringArg(args, "_session_id")); ok {
				if ea, ok := adapter.(*excelAdapter); ok {
					password = ea.secret
				}
			}
		}
	}
	r, err := excel.ReadFile(filePath, excel.ReadOptions{SheetName: sheet, Range: rangeText, MaxRows: limit, Password: password})
	if err != nil {
		return nil, fmt.Errorf("connection: %w", err)
	}
	rows := make([][]interface{}, 0, len(r.Rows))
	for _, row := range r.Rows {
		values := make([]interface{}, len(row))
		for i, cell := range row {
			values[i] = cell.Value
		}
		rows = append(rows, values)
	}
	rows, byteLimited, err := boundTableRows(rows, maxResultBytes)
	if err != nil {
		return nil, err
	}
	truncated := r.Truncated || byteLimited
	result := map[string]interface{}{"contract_version": ContractVersion, "sheet": r.SheetName, "rows": rows, "row_count": len(rows), "truncated": truncated}
	if hash, hashErr := fileSHA256(filePath); hashErr == nil {
		result["source_sha256"] = hash
	}
	if r.Truncated || byteLimited {
		warnings := make([]string, 0, 2)
		if r.Truncated {
			warnings = append(warnings, "source_row_limit")
		}
		if byteLimited {
			warnings = append(warnings, "result_byte_limit", "pagination_unavailable")
		}
		result["warnings"] = warnings
	}
	return result, nil
}

// boundTableRows enforces the common JSON result budget for the read_table
// convenience action. It intentionally does not manufacture a cursor: unlike
// query results, read_table has no stable backend snapshot to resume from.
func boundTableRows(rows [][]interface{}, maxBytes int) ([][]interface{}, bool, error) {
	if maxBytes <= 0 {
		return rows, false, nil
	}
	bounded := make([][]interface{}, 0, len(rows))
	used := 0
	for _, row := range rows {
		encoded, _ := json.Marshal(row)
		if used+len(encoded) > maxBytes {
			if len(bounded) == 0 {
				return nil, false, fmt.Errorf("result_too_large: first Excel row exceeds result byte limit")
			}
			return bounded, true, nil
		}
		used += len(encoded)
		bounded = append(bounded, row)
	}
	return bounded, false, nil
}

func writeTableAction(args map[string]interface{}, export bool) (interface{}, error) {
	return writeTableActionWithContext(context.Background(), args, export)
}

func writeTableActionWithContext(ctx context.Context, args map[string]interface{}, export bool) (interface{}, error) {
	return writeTableActionWithManager(ctx, nil, args, export)
}

func writeTableActionWithManager(ctx context.Context, manager *Manager, args map[string]interface{}, export bool) (interface{}, error) {
	approval := approvalFromContext(ctx)
	filePath := stringArg(args, "file_path")
	if filePath == "" {
		return nil, fmt.Errorf("缺少 file_path 参数")
	}
	if manager != nil {
		resolved, err := manager.resolvePath(filePath, false)
		if err != nil {
			return nil, err
		}
		filePath = resolved
	} else if err := validateLocalDataPath(filePath); err != nil {
		return nil, err
	}
	if !strings.EqualFold(filepath.Ext(filePath), ".xlsx") {
		return nil, fmt.Errorf("unsupported_capability: only .xlsx writes are supported")
	}
	rangeText := strings.TrimSpace(stringArg(args, "range"))
	if !export && rangeText == "" {
		if _, statErr := os.Stat(filePath); statErr == nil {
			// WriteFile historically replaces the whole workbook. Requiring an
			// explicit rectangle for existing files prevents an omitted range
			// from silently deleting unrelated sheets/cells.
			return nil, fmt.Errorf("permission: range is required when updating an existing workbook")
		} else if !os.IsNotExist(statErr) {
			return nil, fmt.Errorf("path_denied: cannot stat target workbook")
		}
	}
	if !boolArg(args, "dry_run", true) && approval.Token == "" {
		return nil, fmt.Errorf("permission: approval token required")
	}
	rows, err := normalizeRows(args["rows"])
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		if data, ok := args["data"].(map[string]interface{}); ok {
			rows, err = normalizeRows(data["rows"])
		}
	}
	if err != nil {
		return nil, err
	}
	if rangeText != "" {
		startCol, startRow, endCol, endRow, parseErr := excel.ParseRange(rangeText)
		if parseErr != nil {
			return nil, fmt.Errorf("syntax: %w", parseErr)
		}
		maxRows, maxCols := endRow-startRow+1, endCol-startCol+1
		if len(rows) > maxRows {
			return nil, fmt.Errorf("quota_exceeded: rows exceed range height: got %d, max %d", len(rows), maxRows)
		}
		for i, row := range rows {
			if len(row) > maxCols {
				return nil, fmt.Errorf("quota_exceeded: rows[%d] exceed range width: got %d, max %d", i, len(row), maxCols)
			}
		}
	}
	sheet := firstNonEmpty(stringArg(args, "sheet"), "Sheet1")
	sourceHash := ""
	if hash, err := fileSHA256(filePath); err == nil {
		sourceHash = hash
	}
	// Range writes into an existing workbook round-trip through the GoExcel
	// reader, which does not map font/fill styles back onto cells: values and
	// merged regions outside the range survive, but their styles may not.
	// Surface that limitation instead of silently promising full fidelity.
	roundTripStyleLoss := rangeText != "" && sourceHash != "" && !strings.EqualFold(filepath.Ext(filePath), ".xlsx")
	styleWarnings := func() []string {
		if roundTripStyleLoss {
			return []string{"range_styles_not_preserved"}
		}
		return nil
	}
	// Dry-run returns a deterministic summary and never touches the file.
	if boolArg(args, "dry_run", true) {
		preview := map[string]interface{}{"contract_version": ContractVersion, "dry_run": true, "dry_run_guarantee": "policy_only", "file_path": filePath, "sheet": sheet, "row_count": len(rows), "source_sha256": sourceHash}
		if rangeText != "" {
			preview["range"] = rangeText
		}
		if warnings := styleWarnings(); warnings != nil {
			preview["warnings"] = warnings
		}
		return preview, nil
	}
	if export {
		// export_excel is intentionally create-only unless the caller explicitly
		// supplies approval; existing files are not silently overwritten.
		if _, statErr := os.Stat(filePath); statErr == nil {
			return nil, fmt.Errorf("permission: export target already exists")
		}
	}
	if expected := strings.TrimSpace(stringArg(args, "source_sha256")); expected != "" {
		actual, err := fileSHA256(filePath)
		if err != nil || !strings.EqualFold(expected, actual) {
			return nil, fmt.Errorf("permission: source file changed since preview")
		}
	} else if !export && rangeText != "" && sourceHash != "" {
		// A range write merges into an existing workbook. Without the preview
		// hash returned by dry_run, a concurrent modification since the
		// preview would be silently overwritten.
		return nil, fmt.Errorf("permission: source_sha256 is required when writing a range into an existing workbook")
	}
	escaped := 0
	data := excel.WriteData{Sheets: []excel.WriteSheet{{Name: sheet, Rows: make([][]excel.WriteCell, len(rows))}}}
	for i, row := range rows {
		data.Sheets[0].Rows[i] = make([]excel.WriteCell, len(row))
		for j, value := range row {
			sanitized, changed := sanitizeFormulaValueCount(value)
			if changed {
				escaped++
			}
			data.Sheets[0].Rows[i][j] = excel.WriteCell{Value: sanitized}
		}
	}
	if rangeText != "" {
		if err := excel.WriteFileRange(filePath, sheet, rangeText, data.Sheets[0].Rows); err != nil {
			if strings.Contains(err.Error(), "range overlaps") || strings.Contains(err.Error(), "sheet ") {
				return nil, fmt.Errorf("unsupported_capability: %w", err)
			}
			return nil, fmt.Errorf("path_denied: %w", err)
		}
	} else if err := excel.WriteFile(filePath, data); err != nil {
		return nil, fmt.Errorf("path_denied: %w", err)
	}
	if export && !boolArg(args, "dry_run", true) && manager != nil {
		profile, ok := manager.ProfileForConnection(stringArg(args, "connection_id"))
		if !ok {
			profile, ok = manager.ProfileByID(stringArg(args, "profile_id"))
		}
		if ok && BlocksExternalDelivery(profile.DataClassification) {
			NoteClassifiedExport(filePath, profile.DataClassification)
		}
	}
	result := map[string]interface{}{"contract_version": ContractVersion, "ok": true, "file_path": filePath, "sheet": sheet, "row_count": len(rows)}
	if rangeText != "" {
		result["range"] = rangeText
	}
	warnings := styleWarnings()
	if escaped > 0 {
		warnings = append(warnings, fmt.Sprintf("formula_cells_escaped:%d", escaped))
	}
	if len(warnings) > 0 {
		result["warnings"] = warnings
	}
	return result, nil
}

func fileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func normalizeRows(raw interface{}) ([][]interface{}, error) {
	if typed, ok := raw.([][]interface{}); ok {
		rows := make([][]interface{}, len(typed))
		for i, row := range typed {
			rows[i] = append([]interface{}(nil), row...)
		}
		return rows, nil
	}
	if typed, ok := raw.([][]string); ok {
		rows := make([][]interface{}, len(typed))
		for i, row := range typed {
			rows[i] = make([]interface{}, len(row))
			for j, value := range row {
				rows[i][j] = value
			}
		}
		return rows, nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("rows must be an array")
	}
	rows := make([][]interface{}, len(items))
	for i, item := range items {
		cells, ok := item.([]interface{})
		if !ok {
			return nil, fmt.Errorf("rows[%d] must be an array", i)
		}
		rows[i] = cells
	}
	return rows, nil
}

func sanitizeFormulaValue(value interface{}) interface{} {
	sanitized, _ := sanitizeFormulaValueCount(value)
	return sanitized
}

func sanitizeFormulaValueCount(value interface{}) (interface{}, bool) {
	s, ok := value.(string)
	if !ok || s == "" {
		return value, false
	}
	if strings.ContainsRune("=+-@", rune(s[0])) {
		return "'" + s, true
	}
	return value, false
}

func validateLocalDataPath(path string) error {
	if path == "" || filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return fmt.Errorf("path_denied: path must be relative to the owner workspace")
	}
	clean := filepath.Clean(path)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("path_denied: path escapes the owner workspace")
	}
	return nil
}

func quoteIdentifier(name string) string {
	return `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
