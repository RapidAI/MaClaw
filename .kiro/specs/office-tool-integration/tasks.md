# Implementation Plan: Office Tool Integration

## Overview

Merge the existing `generate_pdf` tool with new Excel (XLSX/CSV) and PPTX reading capabilities into a unified `office(action=...)` tool. Implementation follows a bottom-up approach: core library packages first, then handler layers, then registration/routing/gate wiring, and finally system prompt updates. All code is Go. Dependencies: `github.com/VantageDataChat/GoExcel` and `github.com/VantageDataChat/GoPPT`.

## Tasks

- [x] 1. Add vendor dependencies and create `corelib/excel/` package
  - [x] 1.1 Add GoExcel and GoPPT dependencies
    - Run `go get github.com/VantageDataChat/GoExcel` and `go get github.com/VantageDataChat/GoPPT`
    - Verify `go.mod` and `go.sum` are updated
    - _Requirements: 6.1, 6.2_

  - [x] 1.2 Implement `corelib/excel/` types and `ParseRange`
    - Create `corelib/excel/excel.go` with `CellValue`, `CellType` constants, `ReadOptions`, `ReadResult`, `SheetStyle`, `WriteCell`, `WriteSheet`, `WriteData` types as specified in the design
    - Create `corelib/excel/range.go` with `ParseRange(rangeStr string) (int, int, int, int, error)` implementing A1-notation parsing (column letters A-ZZ → 1-based int, 1-based row numbers)
    - Error messages in Chinese per design: `范围格式错误: "{range}"。请使用 A1 表示法，例如 A1:D10`
    - _Requirements: 3.4, 3.9, 6.1, 6.3_

  - [ ]* 1.3 Write unit tests for `ParseRange`
    - Create `corelib/excel/range_test.go`
    - `TestParseRange_ValidRanges` — table-driven: `A1` → (1,1,1,1), `B2:D5` → (2,2,4,5), `AA1:AB10` → (27,1,28,10)
    - `TestParseRange_InvalidRanges` — table-driven: empty string, missing colon, non-alpha column, zero row, reversed bounds
    - _Requirements: 3.9_

  - [ ]* 1.4 Write property test for malformed range rejection
    - **Property 7: Malformed range rejection**
    - Create property test in `corelib/excel/range_test.go` using `github.com/leanovate/gopter`
    - Generate random strings that don't match valid A1 notation; assert `ParseRange` returns non-nil error mentioning "A1" or expected format
    - Min 100 iterations
    - **Validates: Requirements 3.9**

  - [x] 1.5 Implement `excel.ReadFile` for XLSX and CSV
    - In `corelib/excel/excel.go`, implement `ReadFile(filePath string, opts ReadOptions) (*ReadResult, error)`
    - Auto-detect CSV by `.csv` extension; use GoExcel (`gospreadsheet`) for XLSX
    - Support `SheetName` option (empty = first sheet), `Range` option (empty = all non-empty cells)
    - Map GoExcel cell types to `CellType` constants; extract formula strings for formula cells
    - Error messages: file not found → `文件不存在: {path}`, sheet not found → `工作表 "{name}" 不存在。可用的工作表: ...`, CSV parse error → `CSV 解析失败: {err}`
    - Implement `ListSheets(filePath string) ([]string, error)`
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9, 3.10, 6.1, 6.3_

  - [x] 1.6 Implement `excel.WriteFile`
    - In `corelib/excel/excel.go`, implement `WriteFile(filePath string, data WriteData) error`
    - Create directories if needed (`os.MkdirAll`)
    - Write each sheet: string cells as text (formula if starts with `=`), float64 as numeric, bool as boolean
    - Apply `SheetStyle` formatting (bold, font size, background color, number format) via GoExcel API
    - Validate: empty `Sheets` slice → `data.sheets 不能为空`
    - _Requirements: 4.1, 4.2, 4.3, 4.4, 4.5, 4.6, 4.7, 4.8, 4.9, 4.10, 6.1, 6.3_

  - [ ]* 1.7 Write unit tests for `corelib/excel/`
    - Create `corelib/excel/excel_test.go`
    - `TestReadFile_XLSX_BasicData`, `TestReadFile_CSV_BasicData`, `TestReadFile_FileNotFound`, `TestReadFile_SheetNotFound`
    - `TestWriteFile_CreatesDirectories`, `TestWriteFile_OverwritesExisting`, `TestWriteFile_MalformedData`, `TestWriteFile_StyleApplication`
    - `TestListSheets`
    - Use temp directories for file I/O tests
    - _Requirements: 6.5_

  - [ ]* 1.8 Write property tests for `corelib/excel/`
    - **Property 3: Excel write-then-read round-trip** — generate random `WriteData` with mixed types, write then read, assert equivalence
    - **Property 4: Sheet selection by name** — generate multi-sheet workbooks, read by name, assert correct sheet returned
    - **Property 5: Range filtering returns correct subset** — generate data + valid range, assert subset matches full read
    - **Property 6: CSV read fidelity** — generate tabular data, write CSV, read back, assert string equivalence
    - **Property 8: Cell type classification** — generate random values, write then read, assert type preserved
    - All in `corelib/excel/excel_test.go`, min 100 iterations each
    - **Validates: Requirements 3.1, 3.3, 3.4, 3.6, 3.10, 4.1, 4.3, 4.4, 4.5, 4.10**

- [x] 2. Create `corelib/pptx/` package
  - [x] 2.1 Implement `corelib/pptx/` types and `Read` function
    - Create `corelib/pptx/pptx.go` with all types from design: `Presentation`, `DocumentProperties`, `Slide`, `Shape`, `ShapeType` constants, `Position`, `Dimensions`, `TextBody`, `Paragraph`, `TextRun`, `TableData`, `TableRow`, `TableCell`, `ChartData`, `DataSeries`
    - Implement `Read(filePath string) (*Presentation, error)` wrapping GoPPT library
    - Extract: slide count, document properties, per-slide shapes (text with formatting runs, tables, charts, images), speaker notes
    - Error messages: file not found → `文件不存在: {path}`, invalid format → `文件格式无效，不是有效的 PPTX 文件: {path}`, read error → `读取 PPTX 失败: {err}`
    - _Requirements: 5.1, 5.2, 5.3, 5.4, 5.5, 5.6, 5.7, 6.2, 6.4_

  - [ ]* 2.2 Write unit tests for `corelib/pptx/`
    - Create `corelib/pptx/pptx_test.go`
    - `TestRead_BasicPresentation`, `TestRead_TextFormatting`, `TestRead_Tables`, `TestRead_Charts`, `TestRead_EmptySlide`, `TestRead_SpeakerNotes`, `TestRead_FileNotFound`, `TestRead_InvalidFormat`
    - Use fixture PPTX files in `corelib/pptx/testdata/`
    - _Requirements: 6.5_

- [x] 3. Checkpoint - Ensure core library tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 4. Implement GUI handler (`gui/im_tools_office.go`)
  - [x] 4.1 Create `gui/im_tools_office.go` with action dispatcher
    - Implement `toolOffice(args map[string]interface{}) string` on `IMMessageHandler`
    - Read `action` parameter via `stringVal(args, "action")`
    - Dispatch: `generate_pdf` → `h.toolGeneratePDF(args)`, `read_excel` → `h.handleReadExcel(args)`, `write_excel` → `h.handleWriteExcel(args)`, `read_pptx` → `h.handleReadPPTX(args)`
    - Unknown action → `未知的 office action: %q。支持的 action: generate_pdf, read_excel, write_excel, read_pptx`
    - _Requirements: 1.1, 1.3, 2.1_

  - [x] 4.2 Implement `handleReadExcel` in GUI handler
    - Extract `file_path`, `sheet`, `range` from args
    - Call `excel.ReadFile(filePath, excel.ReadOptions{SheetName: sheet, Range: rangeStr})`
    - Serialize `ReadResult` to JSON and return
    - _Requirements: 3.1, 3.2, 3.3, 3.4, 3.5, 3.6, 3.7, 3.8, 3.9_

  - [x] 4.3 Implement `handleWriteExcel` in GUI handler
    - Extract `file_path` and `data` from args
    - Parse `data` JSON into `excel.WriteData`; handle missing/malformed data with Chinese error messages
    - Call `excel.WriteFile(filePath, writeData)`
    - Return success message with file path
    - _Requirements: 4.1, 4.2, 4.7, 4.8_

  - [x] 4.4 Implement `handleReadPPTX` in GUI handler
    - Extract `file_path` from args
    - Call `pptx.Read(filePath)`
    - Serialize `Presentation` to JSON and return
    - _Requirements: 5.1, 5.5, 5.6_

  - [ ]* 4.5 Write unit tests for GUI office handler
    - Create `gui/im_tools_office_test.go`
    - `TestToolOffice_Dispatch` — verify each action routes correctly
    - `TestToolOffice_UnknownAction` — verify error message lists all 4 actions
    - `TestToolOffice_ReadExcel_Integration` — end-to-end with temp XLSX file
    - `TestToolOffice_WriteThenReadExcel` — end-to-end round-trip
    - _Requirements: 1.3, 2.1, 3.1, 4.1_

  - [ ]* 4.6 Write property tests for GUI office handler
    - **Property 1: Unknown action returns supported action list** — generate random non-valid action strings, assert error contains all 4 action names
    - **Property 2: PDF alias equivalence** — generate random content/title/doc_type, assert `office(action="generate_pdf")` and `generate_pdf` produce identical output
    - Both in `gui/im_tools_office_test.go`, min 100 iterations each
    - **Validates: Requirements 1.3, 2.1, 2.2**

- [x] 5. Implement TUI handler (`tui/agent_tools_office.go`)
  - [x] 5.1 Create `tui/agent_tools_office.go` with action dispatcher
    - Implement `toolOffice(args map[string]interface{}) string` on `TUIAgentHandler`
    - Same dispatch pattern as GUI: `generate_pdf` → `h.toolGeneratePDF(args)` (existing Markdown fallback), `read_excel`/`write_excel`/`read_pptx` → full implementations calling `corelib/excel` and `corelib/pptx`
    - Unknown action → same error message as GUI
    - _Requirements: 10.1, 10.2, 10.3, 10.4, 10.5_

  - [x] 5.2 Wire TUI `office` and `generate_pdf` dispatch
    - In `tui/agent_handler.go` `dispatchTool`, add case for `"office"` → `h.toolOffice(args)`
    - Update existing `"generate_pdf"` case to inject `action` and delegate to `h.toolOffice(args)`
    - _Requirements: 10.1, 10.6_

- [x] 6. Tool registration and backward-compatible alias
  - [x] 6.1 Register `office` tool in `gui/tool_registry_builtin.go`
    - Define `officeToolParams` map with `action`, `content`, `title`, `doc_type`, `file_path`, `sheet`, `range`, `data` parameters
    - Define `officeToolRequired` as `[]string{"action"}`
    - Register with `reg("office", ...)` using bilingual description and tags `["office", "pdf", "excel", "xlsx", "csv", "pptx", "document", "spreadsheet", "presentation"]`
    - _Requirements: 1.1, 1.4, 1.5_

  - [x] 6.2 Update `generate_pdf` registration as backward-compatible alias
    - Modify existing `generate_pdf` registration handler to inject `args["action"] = "generate_pdf"` and call `h.toolOffice(args)`
    - Preserve existing parameter definitions and `[]string{"content"}` required params
    - _Requirements: 1.2, 2.2, 11.1_

  - [ ]* 6.3 Write test for backward-compatible alias
    - `TestToolOffice_GeneratePDFAlias` — verify calling through alias produces same output as direct office call
    - _Requirements: 2.2, 11.1_

- [x] 7. Checkpoint - Ensure handler tests pass
  - Ensure all tests pass, ask the user if questions arise.

- [x] 8. Router, gate, and allowlist updates
  - [x] 8.1 Add router conditional keep rules for Excel and PPTX keywords
    - In `corelib/tool/router.go`, define `excelKeywords` slice: `xlsx`, `csv`, `spreadsheet`, `表格`, `电子表格`, `Excel`, `excel`
    - Define `pptxReadKeywords` slice: `pptx`, `幻灯片`, `演示文稿`, `PowerPoint`, `PPT`, `读取PPT`, `ppt`
    - Add two new `conditionalKeepRule` entries keeping `"office"` for each keyword set
    - Update existing `codingWorkflowDocKeywords` rule to keep `[]string{"generate_pdf", "office"}`
    - _Requirements: 7.1, 7.2, 7.3_

  - [x] 8.2 Add `office` to `noPinConditionalTools` and `BuiltinToolNames`
    - In `corelib/tool/router.go`, add `"office": true` to `noPinConditionalTools`
    - Add `"office": true` to `BuiltinToolNames`
    - Preserve existing `"generate_pdf": true` in `noPinConditionalTools`
    - _Requirements: 7.4, 7.5_

  - [x] 8.3 Add `office` to `deliveryToolAllowlist` and `DocOnlyAllowedTools`
    - In `gui/coding_tool_gate.go`, add `"office": true` to `deliveryToolAllowlist` (keep existing `"generate_pdf": true`)
    - In `corelib/workflow/types.go`, add `"office": true` to `DocOnlyAllowedTools` (keep existing `"generate_pdf": true`)
    - _Requirements: 8.1, 8.2, 8.3, 11.3, 11.4_

  - [x] 8.4 Update `SteeringWorkflowDetector.interceptToolCall` for `office`
    - In `gui/im_message_handler.go`, add case `"office"` in `interceptToolCall`: check if `action == "generate_pdf"` in parsed args, then apply same doc preview emission logic as existing `"generate_pdf"` case
    - _Requirements: 11.7_

  - [ ]* 8.5 Write router and gate tests
    - `TestConditionalKeepRules_ExcelKeywords` — verify "office" kept for Excel keywords
    - `TestConditionalKeepRules_PPTXKeywords` — verify "office" kept for PPTX keywords
    - `TestConditionalKeepRules_CodingDocKeywords` — verify both "office" and "generate_pdf" kept
    - `TestNoPinConditionalTools_Office` — verify "office" in no-pin set
    - `TestDeliveryToolAllowlist_Office` — verify "office" in allowlist
    - `TestDocOnlyAllowedTools_Office` — verify "office" in doc-only set
    - `TestInterceptToolCall_OfficeGeneratePDF` — verify SteeringWorkflowDetector handles `office(action="generate_pdf")`
    - _Requirements: 7.1, 7.2, 7.3, 7.5, 8.1, 8.2, 11.7_

- [x] 9. System prompt updates
  - [x] 9.1 Update GUI system prompt
    - Replace `generate_pdf` references with `office(action="generate_pdf", ...)` in IM channel coding workflow documentation sections
    - Add new section describing `office` tool capabilities: Excel reading (XLSX/CSV with sheet selection and A1 range), Excel writing (XLSX with styles and formulas), PPTX reading (structured JSON with slides, shapes, text, tables, charts, notes)
    - Update desktop workflow doc override to reference `office` instead of `generate_pdf`
    - _Requirements: 9.1, 9.2, 9.4_

  - [x] 9.2 Update TUI system prompt
    - Add `office` tool description with all four actions and their parameters
    - _Requirements: 9.3_

- [x] 10. Checkpoint - Ensure all tests pass and backward compatibility is maintained
  - Ensure all tests pass, ask the user if questions arise.
  - Verify existing `generate_pdf` tests still pass without modification.
  - _Requirements: 11.6_

- [x] 11. Final integration verification
  - [x] 11.1 Verify backward compatibility end-to-end
    - Confirm `generate_pdf` alias works in both GUI and TUI
    - Confirm `generate_pdf` in `deliveryToolAllowlist`, `DocOnlyAllowedTools`, `noPinConditionalTools` all preserved
    - Confirm `codingWorkflowDocKeywords` rule keeps both `generate_pdf` and `office`
    - Run full test suite to verify no regressions
    - _Requirements: 11.1, 11.2, 11.3, 11.4, 11.5, 11.6_

- [x] 12. Final checkpoint - Ensure all tests pass
  - Ensure all tests pass, ask the user if questions arise.

## Notes

- Tasks marked with `*` are optional and can be skipped for faster MVP
- Each task references specific requirements for traceability
- Checkpoints ensure incremental validation
- Property tests validate universal correctness properties from the design document (8 properties total)
- Unit tests validate specific examples and edge cases (~30 tests specified in design)
- Backward compatibility for `generate_pdf` is maintained throughout — existing entries are preserved, not replaced
- The `corelib/excel/` and `corelib/pptx/` packages never expose raw GoExcel/GoPPT types to handlers
