package agent

// tools_office.go implements office tool handlers (read_excel, write_excel,
// read_pptx) as standalone functions.
//
// generate_pdf stays in gui/ because it depends on GUI-specific document
// generation logic.
//
// Migrated from gui/im_tools_office.go as part of the agent-unification plan.

import (
	"encoding/json"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/excel"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
)

// ToolReadExcel reads an Excel (XLSX/CSV) file and returns the data as JSON.
func ToolReadExcel(args map[string]interface{}) string {
	filePath := StringArg(args, "file_path")
	if filePath == "" {
		return "缺少 file_path 参数"
	}
	sheet := StringArg(args, "sheet")
	rangeStr := StringArg(args, "range")

	filePath = ResolvePath(filePath)

	result, err := excel.ReadFile(filePath, excel.ReadOptions{
		SheetName: sheet,
		Range:     rangeStr,
	})
	if err != nil {
		return err.Error()
	}

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("序列化结果失败: %v", err)
	}
	return string(data)
}

// ToolWriteExcel writes data to an XLSX file.
func ToolWriteExcel(args map[string]interface{}) string {
	filePath := StringArg(args, "file_path")
	if filePath == "" {
		return "缺少 file_path 参数"
	}
	filePath = ResolvePath(filePath)

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
func ToolReadPPTX(args map[string]interface{}) string {
	filePath := StringArg(args, "file_path")
	if filePath == "" {
		return "缺少 file_path 参数"
	}
	filePath = ResolvePath(filePath)

	result, err := pptx.Read(filePath)
	if err != nil {
		return err.Error()
	}

	data, err := json.Marshal(result)
	if err != nil {
		return fmt.Sprintf("序列化结果失败: %v", err)
	}
	return string(data)
}
