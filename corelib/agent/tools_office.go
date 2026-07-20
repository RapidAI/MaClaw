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

// ToolReadExcel reads an Excel file and returns the data as JSON.
// Supports modern .xlsx/.csv and legacy .xls (BIFF).
func ToolReadExcel(args map[string]interface{}) string {
	filePath := officeFilePathArg(args)
	if filePath == "" {
		return "缺少 file_path 参数（也可用 path）"
	}
	sheet := StringArg(args, "sheet")
	rangeStr := StringArg(args, "range")

	filePath = resolveOfficeToolPath(filePath)
	if _, err := os.Stat(filePath); err != nil {
		return fmt.Sprintf("文件不存在或无法访问: %v", err)
	}
	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".xls" {
		if rangeStr != "" {
			return formatOfficeReadFailure(filePath, "xls", fmt.Errorf("旧版 .xls 暂不支持 range 参数；请省略 range 或先另存为 .xlsx"))
		}
		out, err := readXLSAsExcelJSON(filePath, sheet)
		if err != nil {
			return formatOfficeReadFailure(filePath, "xls", err)
		}
		return out
	}

	result, err := excel.ReadFile(filePath, excel.ReadOptions{
		SheetName: sheet,
		Range:     rangeStr,
	})
	if err != nil {
		kind := strings.TrimPrefix(ext, ".")
		if kind == "" {
			kind = "xlsx"
		}
		return formatOfficeReadFailure(filePath, kind, err)
	}

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("序列化结果失败: %v", err)
	}
	return string(data)
}

// ToolWriteExcel writes data to an XLSX file.
func ToolWriteExcel(args map[string]interface{}) string {
	filePath := officeFilePathArg(args)
	if filePath == "" {
		return "缺少 file_path 参数（也可用 path）"
	}
	filePath = resolveOfficeToolPath(filePath)

	rawData, ok := args["data"]
	if !ok || rawData == nil {
		return "缺少 data 参数"
	}

	var jsonBytes []byte

	switch v := rawData.(type) {
	case string:
		jsonBytes = []byte(v)
	default:
		var err error
		jsonBytes, err = json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("data 参数格式错误: %v", err)
		}
	}

	var writeData excel.WriteData
	if err := json.Unmarshal(jsonBytes, &writeData); err != nil {
		return fmt.Sprintf("data 参数格式错误: %v", err)
	}

	if err := excel.WriteFile(filePath, writeData); err != nil {
		return err.Error()
	}

	return fmt.Sprintf("已成功写入 XLSX 文件: %s", filePath)
}

// ToolReadPPTX reads a PPTX file and returns the structured data as JSON.
// Legacy .ppt is not supported natively; failure text steers agents to craft_tool.
func ToolReadPPTX(args map[string]interface{}) string {
	filePath := officeFilePathArg(args)
	if filePath == "" {
		return "缺少 file_path 参数（也可用 path）"
	}
	filePath = resolveOfficeToolPath(filePath)
	if _, err := os.Stat(filePath); err != nil {
		return fmt.Sprintf("文件不存在或无法访问: %v", err)
	}
	if strings.EqualFold(filepath.Ext(filePath), ".ppt") {
		return formatOfficeReadFailure(filePath, "ppt", fmt.Errorf("原生解析暂不支持旧版 PowerPoint .ppt"))
	}

	result, err := pptx.Read(filePath)
	if err != nil {
		return formatOfficeReadFailure(filePath, "pptx", err)
	}

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("序列化结果失败: %v", err)
	}
	return string(data)
}
