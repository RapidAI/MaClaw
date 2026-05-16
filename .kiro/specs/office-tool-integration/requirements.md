# Requirements Document

## Introduction

Merge the existing `generate_pdf` built-in tool with new Excel (XLSX/CSV) reading/writing and PPTX reading capabilities into a unified `office(action=...)` tool. All three underlying libraries are from Vantagics and are pure Go: GoPDF2 (PDF generation, already integrated), GoExcel (XLSX/CSV read/write), and GoPPT (PPTX read). The unified tool follows the tool-merge pattern established in improvement #14 (13 tools → 3 tools), reducing LLM tool-selection overhead while preserving backward compatibility for `generate_pdf`.

## Glossary

- **Office_Tool**: The unified `office` tool registered in the tool registry, dispatching to sub-actions via an `action` parameter.
- **GoExcel**: The `github.com/VantageDataChat/GoExcel` pure-Go library for XLSX/CSV read/write operations.
- **GoPPT**: The `github.com/VantageDataChat/GoPPT` pure-Go library for PPTX create/read/write operations (only read is in scope).
- **GoPDF2**: The `github.com/VantageDataChat/GoPDF2` library already integrated for PDF generation via `corelib/docgen`.
- **GUI_Handler**: The `IMMessageHandler` in `gui/im_tools_misc.go` that processes tool calls in the desktop/IM GUI.
- **TUI_Handler**: The `TUIAgentHandler` in `tui/agent_handler.go` and `tui/agent_tools_missing.go` that processes tool calls in the terminal UI.
- **Tool_Registry**: The `ToolRegistry` in `gui/tool_registry_builtin.go` where built-in tools are registered with definitions and handlers.
- **Router**: The `Router` in `corelib/tool/router.go` that selects relevant tools per user message via conditional keep rules, BM25 scoring, and session pinning.
- **Coding_Tool_Gate**: The gate in `gui/coding_tool_gate.go` that controls which tools are available during coding workflow phases.
- **DocOnlyAllowedTools**: The canonical set in `corelib/workflow/types.go` listing tools permitted during doc-only workflow phases.
- **Cell_Range**: A string in A1 notation (e.g. `A1:D10`) specifying a rectangular region of cells in a spreadsheet.
- **Structured_JSON**: A JSON representation of slide data returned by `read_pptx`, containing slides, shapes, text runs, charts, tables, and speaker notes.

## Requirements

### Requirement 1: Unified Office Tool Registration

**User Story:** As a developer, I want a single `office` tool registered in the tool registry, so that the LLM has one entry point for all office document operations instead of separate tools.

#### Acceptance Criteria

1. THE Tool_Registry SHALL register a tool named `office` with an `action` parameter accepting values `generate_pdf`, `read_excel`, `write_excel`, and `read_pptx`.
2. THE Tool_Registry SHALL register `generate_pdf` as a backward-compatible alias that delegates to `office(action="generate_pdf", ...)`.
3. WHEN the Office_Tool receives an unknown action value, THE Office_Tool SHALL return an error message listing the supported actions.
4. THE Office_Tool SHALL include tags `["office", "pdf", "excel", "xlsx", "csv", "pptx", "document", "spreadsheet", "presentation"]` for BM25 routing.
5. THE Office_Tool description SHALL mention all four supported actions and their purposes in both Chinese and English keywords for routing accuracy.

### Requirement 2: PDF Generation (Existing Functionality)

**User Story:** As a user, I want the existing PDF generation to work identically through the new `office` tool, so that no existing workflows break.

#### Acceptance Criteria

1. WHEN `office(action="generate_pdf", content=..., title=..., doc_type=...)` is called, THE Office_Tool SHALL produce identical output to the current `generate_pdf` tool.
2. WHEN `generate_pdf(content=..., title=..., doc_type=...)` is called via the backward-compatible alias, THE Office_Tool SHALL produce identical output to the current `generate_pdf` tool.
3. THE Office_Tool SHALL preserve all existing parameter names and semantics: `content` (Markdown string), `title` (document title), `doc_type` (requirements/design/task_plan).
4. IF the `content` parameter is empty or missing, THEN THE Office_Tool SHALL return the same error message as the current implementation.

### Requirement 3: Excel Reading

**User Story:** As a user, I want to read data from XLSX and CSV files, so that I can extract spreadsheet information for analysis or processing.

#### Acceptance Criteria

1. WHEN `office(action="read_excel", file_path=..., sheet=..., range=...)` is called with a valid XLSX file path, THE Office_Tool SHALL return the cell data as a JSON array of rows.
2. WHEN the `sheet` parameter is omitted, THE Office_Tool SHALL read from the first sheet in the workbook.
3. WHEN the `sheet` parameter is provided, THE Office_Tool SHALL read from the sheet matching that name.
4. WHEN the `range` parameter is provided in A1 notation (e.g. `A1:D10`), THE Office_Tool SHALL return only cells within that rectangular region.
5. WHEN the `range` parameter is omitted, THE Office_Tool SHALL return all non-empty cells in the sheet.
6. WHEN `office(action="read_excel", file_path=..., ...)` is called with a valid CSV file path, THE Office_Tool SHALL parse the CSV and return the data as a JSON array of rows.
7. IF the file does not exist, THEN THE Office_Tool SHALL return a descriptive error including the file path.
8. IF the sheet name does not exist in the workbook, THEN THE Office_Tool SHALL return an error listing the available sheet names.
9. IF the range string is malformed, THEN THE Office_Tool SHALL return an error describing the expected A1 notation format.
10. FOR ALL valid XLSX files, reading then formatting the cell data as JSON then parsing the JSON SHALL produce values equivalent to the original cell data (round-trip property for data fidelity).

### Requirement 4: Excel Writing

**User Story:** As a user, I want to create and modify XLSX files with cell values, formulas, and styles, so that I can generate spreadsheet reports programmatically.

#### Acceptance Criteria

1. WHEN `office(action="write_excel", file_path=..., data=...)` is called, THE Office_Tool SHALL create or overwrite an XLSX file at the specified path.
2. THE `data` parameter SHALL accept a JSON object with the following structure: `{"sheets": [{"name": "Sheet1", "rows": [[cell, cell, ...], ...]}]}`.
3. WHEN a cell value is a string starting with `=`, THE Office_Tool SHALL write it as a formula.
4. WHEN a cell value is a number, THE Office_Tool SHALL write it as a numeric cell.
5. WHEN a cell value is a string (not starting with `=`), THE Office_Tool SHALL write it as a text cell.
6. WHEN the `data` parameter contains a `styles` field for a cell, THE Office_Tool SHALL apply the specified formatting (bold, font size, background color, number format).
7. IF the `file_path` directory does not exist, THEN THE Office_Tool SHALL create the necessary directories.
8. IF the `data` parameter is missing or malformed, THEN THE Office_Tool SHALL return a descriptive error.
9. WHEN writing to an existing file, THE Office_Tool SHALL overwrite the file completely (not merge with existing content).
10. FOR ALL valid write data, writing an XLSX file then reading it back SHALL produce cell values equivalent to the original input data (round-trip property).

### Requirement 5: PPTX Reading

**User Story:** As a user, I want to read the contents of PPTX files, so that I can extract presentation structure, text, and metadata for analysis.

#### Acceptance Criteria

1. WHEN `office(action="read_pptx", file_path=...)` is called with a valid PPTX file, THE Office_Tool SHALL return a Structured_JSON representation of the presentation.
2. THE Structured_JSON SHALL include: total slide count, and for each slide: slide number, shapes (with type, position, dimensions), text content (with formatting runs), tables (with cell data), charts (with type and data series), and speaker notes.
3. WHEN a slide contains no shapes, THE Structured_JSON SHALL represent it as an empty shapes array.
4. WHEN a shape contains text, THE Structured_JSON SHALL include the text content with paragraph and run-level formatting (bold, italic, font size, color).
5. IF the file does not exist, THEN THE Office_Tool SHALL return a descriptive error including the file path.
6. IF the file is not a valid PPTX file, THEN THE Office_Tool SHALL return a descriptive error indicating the file format is invalid.
7. WHEN the PPTX file contains embedded charts, THE Structured_JSON SHALL include the chart type and data series labels.

### Requirement 6: Excel/PPTX Package Layer

**User Story:** As a developer, I want the GoExcel and GoPPT library integrations encapsulated in dedicated packages under `corelib/`, so that the tool handlers remain thin and the logic is testable independently.

#### Acceptance Criteria

1. THE system SHALL provide a `corelib/excel/` package that wraps GoExcel operations (read XLSX, read CSV, write XLSX).
2. THE system SHALL provide a `corelib/pptx/` package that wraps GoPPT read operations.
3. THE `corelib/excel/` package SHALL expose functions with clear input/output types, not raw library types, to decouple the handler from the library.
4. THE `corelib/pptx/` package SHALL expose a `Read(filePath string) (*Presentation, error)` function returning a structured Go type.
5. THE `corelib/excel/` and `corelib/pptx/` packages SHALL each have unit tests covering success and error paths.

### Requirement 7: Router and Conditional Keep Rules

**User Story:** As a developer, I want the router to correctly include the `office` tool when the user message mentions spreadsheets, presentations, or PDF generation, so that the LLM can access it when needed.

#### Acceptance Criteria

1. THE Router conditional keep rules SHALL include a rule that matches Excel-related keywords (xlsx, csv, spreadsheet, 表格, 电子表格, Excel) and keeps the `office` tool.
2. THE Router conditional keep rules SHALL include a rule that matches PPTX-related keywords (pptx, 幻灯片, 演示文稿, PowerPoint, PPT, 读取PPT) and keeps the `office` tool.
3. THE existing conditional keep rule for `generate_pdf` matching `codingWorkflowDocKeywords` SHALL be updated to keep `office` instead.
4. THE `generate_pdf` backward-compatible alias SHALL remain in `noPinConditionalTools` to prevent session pinning.
5. THE `office` tool SHALL be added to `noPinConditionalTools` to prevent session pinning (it should only appear when contextually relevant).

### Requirement 8: Coding Tool Gate and Workflow Integration

**User Story:** As a developer, I want the coding tool gate and workflow engine to recognize the `office` tool as a delivery/document tool, so that it is not blocked during coding workflow phases.

#### Acceptance Criteria

1. THE Coding_Tool_Gate `deliveryToolAllowlist` SHALL include `office` alongside the existing `generate_pdf` entry.
2. THE DocOnlyAllowedTools set in `corelib/workflow/types.go` SHALL include `office` alongside the existing `generate_pdf` entry.
3. WHILE a coding workflow is in a doc-only phase, THE system SHALL allow `office` tool calls to pass through the tool filter.

### Requirement 9: System Prompt Updates

**User Story:** As a developer, I want the system prompts to reference the `office` tool for PDF generation and mention the new Excel/PPTX capabilities, so that the LLM knows how to use them.

#### Acceptance Criteria

1. THE GUI system prompt SHALL replace references to `generate_pdf` with `office(action="generate_pdf", ...)` in the coding workflow documentation sections.
2. THE GUI system prompt SHALL add a section describing the `office` tool's Excel reading, Excel writing, and PPTX reading capabilities.
3. THE TUI system prompt SHALL include a description of the `office` tool capabilities.
4. THE desktop workflow doc override SHALL reference `office` instead of `generate_pdf` where applicable.

### Requirement 10: TUI Support

**User Story:** As a user of the TUI interface, I want the `office` tool to be available with appropriate stubs or full implementations, so that I can use office document operations from the terminal.

#### Acceptance Criteria

1. THE TUI_Handler SHALL dispatch `office` tool calls to a handler that routes by `action` parameter.
2. WHEN `office(action="generate_pdf", ...)` is called in TUI, THE TUI_Handler SHALL produce the same Markdown-fallback behavior as the current `generate_pdf` TUI stub.
3. WHEN `office(action="read_excel", ...)` is called in TUI, THE TUI_Handler SHALL read the file and return cell data (full implementation, not a stub).
4. WHEN `office(action="write_excel", ...)` is called in TUI, THE TUI_Handler SHALL write the XLSX file (full implementation, not a stub).
5. WHEN `office(action="read_pptx", ...)` is called in TUI, THE TUI_Handler SHALL read the file and return structured JSON (full implementation, not a stub).
6. THE TUI_Handler SHALL maintain the `generate_pdf` backward-compatible alias dispatching to the office handler.

### Requirement 11: Backward Compatibility

**User Story:** As a developer, I want all existing references to `generate_pdf` to continue working without modification, so that no existing workflows, tests, or configurations break.

#### Acceptance Criteria

1. THE `generate_pdf` tool name SHALL remain functional as a backward-compatible alias in both GUI and TUI.
2. THE `generate_pdf` entry in `codingWorkflowDocKeywords` conditional keep rules SHALL continue to trigger tool inclusion.
3. THE `generate_pdf` entry in `deliveryToolAllowlist` SHALL be preserved alongside the new `office` entry.
4. THE `generate_pdf` entry in `DocOnlyAllowedTools` SHALL be preserved alongside the new `office` entry.
5. THE `generate_pdf` entry in `noPinConditionalTools` SHALL be preserved.
6. ALL existing tests referencing `generate_pdf` SHALL continue to pass without modification.
7. THE SteeringWorkflowDetector `interceptToolCall` SHALL handle both `generate_pdf` and `office(action="generate_pdf")` tool calls for doc preview emission.
