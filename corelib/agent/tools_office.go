package agent

// tools_office.go implements office tool handlers (read_excel, write_excel,
// read_pptx) as standalone functions.
//
// Document/PDF reads live in tools_office_read.go (read_document / read_doc /
// read_docx / read_pdf). generate_pdf stays in gui/ because it depends on
// GUI-specific document generation logic.
//
// Migrated from gui/im_tools_office.go as part of the agent-unification plan.

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/excel"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
)

// Structured Office tools return JSON rather than read_document's paged text.
// Keep their payload bounded independently so an otherwise valid 32 MiB input
// cannot create an unbounded tool result in the agent context.
const (
	defaultStructuredOfficeMaxRows     = 1000
	maxStructuredOfficeMaxRows         = 5000
	defaultStructuredOfficeMaxSlides   = 100
	maxStructuredOfficeMaxSlides       = 500
	defaultStructuredOfficeSlideOffset = 0
	maxStructuredOfficeJSONBytes       = 3 * 1024 * 1024
)

// structuredOfficeToolSnapshot is a narrow seam that proves the structured
// tool parsers receive the owned snapshot rather than reopening the original
// pathname after policy validation.
var structuredOfficeToolSnapshot = snapshotStructuredOfficeToolInput

// structuredCSVInputProbe keeps CSV's lightweight type boundary independently
// testable. It must never parse the complete CSV: read_excel owns row and JSON
// limits, while this probe only rejects containers masquerading as CSV.
var structuredCSVInputProbe = probeStructuredCSVInput

// ToolReadExcel reads an Excel file and returns the data as JSON.
// Supports modern .xlsx/.csv and legacy .xls (BIFF), with the same sheet,
// A1 range, and bounded-row contract for every supported spreadsheet type.
func ToolReadExcel(args map[string]interface{}) string {
	filePath := officeFilePathArg(args)
	if filePath == "" {
		return "缺少 file_path 参数（也可用 path）"
	}
	sheet := StringArg(args, "sheet")
	rangeStr := StringArg(args, "range")
	maxRows := structuredOfficeMaxRows(args)

	filePath = resolveOfficeToolPath(filePath)
	info, err := os.Stat(filePath)
	if err != nil {
		return formatOfficeReadUnavailable(filePath)
	}
	if info.IsDir() {
		return formatOfficeReadUnavailable(filePath)
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	format := strings.TrimPrefix(ext, ".")
	// read_excel is a format-specific structured API, not a second generic
	// document router.  In particular, a valid PPTX named .pptx used to pass
	// its own PPTX preflight and then reach the XLSX parser, whose ordinary
	// failure misleadingly offered a recovery parser for the same input.
	// Reject every non-Excel suffix before creating a parser snapshot so only
	// read_document owns cross-format signature routing.
	if ext != ".xls" && ext != ".xlsx" && ext != ".csv" {
		return formatOfficeReadFailure(filePath, format, ErrOfficeReadFormatMismatch)
	}
	parsePath, cleanup, err := structuredOfficeToolSnapshot(filePath, format)
	if err != nil {
		return formatOfficeReadFailure(filePath, format, err)
	}
	defer cleanup()
	result, err := excel.ReadFile(parsePath, excel.ReadOptions{
		SheetName: sheet,
		Range:     rangeStr,
		MaxRows:   maxRows,
	})
	if err != nil {
		kind := strings.TrimPrefix(ext, ".")
		if kind == "" {
			kind = "xlsx"
		}
		return formatOfficeReadFailure(filePath, kind, err)
	}

	data, err := marshalStructuredOfficeResult(result)
	if err != nil {
		return formatOfficeReadFailure(filePath, strings.TrimPrefix(ext, "."), err)
	}
	return string(data)
}

// ToolWriteExcel writes data to an XLSX file.
func ToolWriteExcel(args map[string]interface{}) string {
	text, _ := WriteExcelDetailed(args)
	return text
}

// WriteExcelDetailed is ToolWriteExcel with the verdict kept in the error
// instead of only in the prose. The legacy registry surface has room for a
// string and nothing else, but a caller that must know whether the file was
// actually written cannot recover that by searching the prose: excel.WriteFile
// reports an empty sheet set as "data.sheets 不能为空", which shares no word
// with the other failures, and the success line echoes a path that may itself
// contain one.
func WriteExcelDetailed(args map[string]interface{}) (string, error) {
	filePath := officeFilePathArg(args)
	if filePath == "" {
		return "缺少 file_path 参数（也可用 path）", fmt.Errorf("office_write_path_required")
	}
	filePath = resolveOfficeToolPath(filePath)

	rawData, ok := args["data"]
	if !ok || rawData == nil {
		return "缺少 data 参数", fmt.Errorf("office_write_data_required")
	}

	var jsonBytes []byte

	switch v := rawData.(type) {
	case string:
		jsonBytes = []byte(v)
	default:
		var err error
		jsonBytes, err = json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("data 参数格式错误: %v", err), fmt.Errorf("office_write_data_malformed: %v", err)
		}
	}

	var writeData excel.WriteData
	if err := json.Unmarshal(jsonBytes, &writeData); err != nil {
		return fmt.Sprintf("data 参数格式错误: %v", err), fmt.Errorf("office_write_data_malformed: %v", err)
	}

	if err := excel.WriteFile(filePath, writeData); err != nil {
		return err.Error(), fmt.Errorf("office_write_failed: %v", err)
	}

	return fmt.Sprintf("已成功写入 XLSX 文件: %s", filePath), nil
}

// ToolReadPPTX reads a PPTX file and returns structured data as JSON.
// Legacy .ppt is text-readable through read_document, but it has no compatible
// structured-slide JSON contract and is therefore rejected here.
func ToolReadPPTX(args map[string]interface{}) string {
	filePath := officeFilePathArg(args)
	if filePath == "" {
		return "缺少 file_path 参数（也可用 path）"
	}
	filePath = resolveOfficeToolPath(filePath)
	info, err := os.Stat(filePath)
	if err != nil {
		return formatOfficeReadUnavailable(filePath)
	}
	if info.IsDir() {
		return formatOfficeReadUnavailable(filePath)
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	// Like read_excel, this structured API must not become an extension-agnostic
	// parser probe.  Legacy .ppt remains available through read_document's
	// six-format text route, but structured slide JSON is PPTX-only.
	if ext != ".pptx" {
		return formatOfficeReadFailure(filePath, strings.TrimPrefix(ext, "."), ErrOfficeReadFormatMismatch)
	}
	parsePath, cleanup, err := structuredOfficeToolSnapshot(filePath, "pptx")
	if err != nil {
		return formatOfficeReadFailure(filePath, "pptx", err)
	}
	defer cleanup()

	maxSlides := structuredOfficeMaxSlides(args)
	slideOffset := structuredOfficeSlideOffset(args)
	result, err := pptx.ReadWithOptions(parsePath, pptx.ReadOptions{Offset: slideOffset, MaxSlides: maxSlides})
	if err != nil {
		return formatOfficeReadFailure(filePath, "pptx", err)
	}

	data, err := marshalStructuredOfficeResult(result)
	if err != nil {
		return formatOfficeReadFailure(filePath, "pptx", err)
	}
	return string(data)
}

func structuredOfficeMaxSlides(args map[string]interface{}) int {
	maxSlides := intArg(args, "max_slides", defaultStructuredOfficeMaxSlides)
	if maxSlides <= 0 {
		return defaultStructuredOfficeMaxSlides
	}
	if maxSlides > maxStructuredOfficeMaxSlides {
		return maxStructuredOfficeMaxSlides
	}
	return maxSlides
}

func structuredOfficeSlideOffset(args map[string]interface{}) int {
	offset := intArg(args, "slide_offset", defaultStructuredOfficeSlideOffset)
	if offset < 0 {
		return 0
	}
	return offset
}

func structuredOfficeMaxRows(args map[string]interface{}) int {
	maxRows := intArg(args, "max_rows", defaultStructuredOfficeMaxRows)
	if maxRows <= 0 {
		return defaultStructuredOfficeMaxRows
	}
	if maxRows > maxStructuredOfficeMaxRows {
		return maxStructuredOfficeMaxRows
	}
	return maxRows
}

func marshalStructuredOfficeResult(result any) ([]byte, error) {
	data, err := json.Marshal(result)
	if err != nil {
		return nil, err
	}
	if len(data) > maxStructuredOfficeJSONBytes {
		return nil, ErrOfficeReadOutputTooLarge
	}
	return data, nil
}

// snapshotStructuredOfficeToolInput applies the shared bounded container
// policy and binds every path-based structured parser to its verified private
// copy. The tools intentionally retain their JSON protocol, but cannot use it
// as a second route around OfficeRead preflight or source-version checks.
func snapshotStructuredOfficeToolInput(filePath, format string) (snapshot string, cleanup func(), err error) {
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	// Office parsers reopen their supplied pathname. Bind their parse to the
	// same byte sequence that passed ZIP/OLE preflight instead of preflighting
	// a mutable user path and then opening it again below. CSV has no Office
	// container semantics, but it still receives the same bounded/versioned
	// private copy before encoding/csv materializes its grid.
	switch format {
	case "xls", "xlsx", "pptx":
		return SnapshotOfficeReadInput(filePath, format)
	case "csv":
		// CSV is not an Office container, but callers can rename an Office or
		// PDF payload to .csv. Before handing its private copy to a CSV parser,
		// perform only the shared container preflight and signature probe. Calling
		// ExtractOfficeText here would fully extract ordinary CSV files before the
		// structured reader applies its own max_rows and JSON bounds.
		snapshot, cleanup, err = SnapshotBoundedDocumentInput(filePath, ".csv")
		if err != nil {
			return "", nil, err
		}
		if err := structuredCSVInputProbe(snapshot); err != nil {
			cleanup()
			return "", nil, err
		}
		return snapshot, cleanup, nil
	default:
		// Keep the historical extension-led parser behavior for unsupported
		// suffixes, while still avoiding a post-stat TOCTOU read.
		return SnapshotBoundedDocumentInput(filePath, filepath.Ext(filePath))
	}
}

// probeStructuredCSVInput rejects a ZIP/OLE/PDF document relabelled as CSV
// without routing a normal CSV through a full document extractor. The
// container preflight is intentionally first so encryption remains the stable,
// actionable failure class rather than being flattened into a mismatch.
func probeStructuredCSVInput(filePath string) error {
	if _, err := preflightOfficeReadContainerIfPresent(filePath); err != nil {
		return err
	}
	if sniffOfficeFormat(filePath) != "" {
		return ErrOfficeReadFormatMismatch
	}
	return nil
}

// ValidateCSVInput is the shared, lightweight type boundary for pathname-based
// CSV consumers. It rejects Office/PDF containers relabelled as CSV without
// extracting normal CSV text; callers must invoke it on their owned snapshot.
func ValidateCSVInput(filePath string) error {
	return probeStructuredCSVInput(filePath)
}
