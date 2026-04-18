package main

import (
	"encoding/json"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/excel"
	"github.com/RapidAI/CodeClaw/corelib/pptx"
)

// toolOffice dispatches office tool calls by action parameter.
func (h *TUIAgentHandler) toolOffice(args map[string]interface{}) string {
	action := stringArg(args, "action")
	switch action {
	case "generate_pdf":
		return h.toolGeneratePDF(args)
	case "read_excel":
		return h.handleReadExcel(args)
	case "write_excel":
		return h.handleWriteExcel(args)
	case "read_pptx":
		return h.handleReadPPTX(args)
	default:
		return fmt.Sprintf("未知的 office action: %q。支持的 action: generate_pdf, read_excel, write_excel, read_pptx", action)
	}
}

// handleReadExcel reads an Excel (XLSX/CSV) file and returns the data as JSON.
func (h *TUIAgentHandler) handleReadExcel(args map[string]interface{}) string {
	filePath := stringArg(args, "file_path")
	if filePath == "" {
		return "缺少 file_path 参数"
	}
	sheet := stringArg(args, "sheet")
	rangeStr := stringArg(args, "range")

	filePath = resolvePath(filePath)

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

// handleWriteExcel writes data to an XLSX file.
func (h *TUIAgentHandler) handleWriteExcel(args map[string]interface{}) string {
	filePath := stringArg(args, "file_path")
	if filePath == "" {
		return "缺少 file_path 参数"
	}
	filePath = resolvePath(filePath)

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

// handleReadPPTX reads a PPTX file and returns the structured data as JSON.
func (h *TUIAgentHandler) handleReadPPTX(args map[string]interface{}) string {
	filePath := stringArg(args, "file_path")
	if filePath == "" {
		return "缺少 file_path 参数"
	}
	filePath = resolvePath(filePath)

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
