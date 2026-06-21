package main

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"

	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// UploadSupplementaryDocs is a Wails binding called by the frontend when the user
// uploads supplementary reference documents in a workflow form that declares
// AcceptsSupplementary. Unlike resume parsing (which fills form fields), supplementary
// docs are stored as raw text content and injected into subsequent phase prompts as
// reference material for LLM generation.
//
// Parameters:
//   - filePaths: JSON-encoded array of absolute file paths selected by the user
//
// Returns JSON: { "data": { "count": N, "files": ["name1.pdf", ...] }, "error": null }
//
//	or:   { "data": null, "error": "error message" }
func (a *App) UploadSupplementaryDocs(filePaths string) string {
	userID := desktopUserID

	// Parse file paths
	var paths []string
	if err := json.Unmarshal([]byte(filePaths), &paths); err != nil {
		return marshalSupplementaryError("文件路径解析失败: " + err.Error())
	}
	if len(paths) == 0 {
		return marshalSupplementaryError("未选择任何文件")
	}

	// Get active workflow state
	if a.workflowV2 == nil {
		return marshalSupplementaryError("工作流引擎未初始化")
	}
	state := a.workflowV2.machine.GetActive(userID)
	if state == nil {
		return marshalSupplementaryError("没有活跃的工作流")
	}

	// Check max files limit from the active phase's schema
	activePhase := state.ActivePhase()
	if activePhase != nil && activePhase.InputSchema != nil && activePhase.InputSchema.AcceptsSupplementary != nil {
		maxFiles := activePhase.InputSchema.AcceptsSupplementary.MaxFiles
		if maxFiles <= 0 {
			maxFiles = 5 // default
		}
		existingCount := len(state.SupplementaryDocs)
		if existingCount+len(paths) > maxFiles {
			return marshalSupplementaryError(fmt.Sprintf("最多上传 %d 份文件（已有 %d 份，本次选择 %d 份）", maxFiles, existingCount, len(paths)))
		}
	}

	// Extract text from each file (done outside of any lock — I/O intensive)
	var docs []v2.SupplementaryDocEntry
	for _, path := range paths {
		fileName := filepath.Base(path)

		text, err := extractTextFromFile(path)
		if err != nil {
			log.Printf("[workflow-v2-supplementary] failed to extract text from %s: %v", fileName, err)
			continue
		}
		text = sanitizeExtractedText(text)
		if strings.TrimSpace(text) == "" {
			log.Printf("[workflow-v2-supplementary] empty content from %s, skipping", fileName)
			continue
		}
		docs = append(docs, v2.SupplementaryDocEntry{FileName: fileName, Text: text})
	}

	if len(docs) == 0 {
		return marshalSupplementaryError("所有文件均无法提取文本内容")
	}

	// Atomically update the workflow state via StateMachine's mutex-protected path.
	// This prevents race conditions with concurrent SubmitForm calls.
	processedFiles, err := a.workflowV2.machine.AddSupplementaryDocs(userID, docs)
	if err != nil {
		log.Printf("[workflow-v2-supplementary] AddSupplementaryDocs failed: %v", err)
		return marshalSupplementaryError(err.Error())
	}

	for _, f := range processedFiles {
		log.Printf("[workflow-v2-supplementary] stored %s", f)
	}

	// Return success with file list
	result := map[string]interface{}{
		"data": map[string]interface{}{
			"count": len(processedFiles),
			"files": processedFiles,
		},
		"error": nil,
	}
	bs, _ := json.Marshal(result)
	return string(bs)
}

// RemoveSupplementaryDoc removes a single supplementary document from the workflow state.
func (a *App) RemoveSupplementaryDoc(fileName string) string {
	userID := desktopUserID

	if a.workflowV2 == nil {
		return marshalSupplementaryError("工作流引擎未初始化")
	}

	if err := a.workflowV2.machine.RemoveSupplementaryDoc(userID, fileName); err != nil {
		return marshalSupplementaryError(err.Error())
	}

	result := map[string]interface{}{
		"data":  map[string]interface{}{"removed": fileName},
		"error": nil,
	}
	bs, _ := json.Marshal(result)
	return string(bs)
}

func marshalSupplementaryError(msg string) string {
	result := map[string]interface{}{
		"data":  nil,
		"error": msg,
	}
	bs, _ := json.Marshal(result)
	return string(bs)
}
