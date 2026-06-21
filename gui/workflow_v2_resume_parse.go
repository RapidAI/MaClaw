package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	gopdf2 "github.com/VantageDataChat/GoPDF2"
	legacydoc "github.com/shakinm/xlsReader/doc"
	"github.com/RapidAI/CodeClaw/corelib"
	v2 "github.com/RapidAI/CodeClaw/corelib/workflow/v2"
)

// ParseResumeForWorkflowForm is a Wails binding called by the frontend when the user
// uploads a resume/CV file in a workflow form that declares AcceptsResume=true.
//
// It reads the file, extracts text content, sends it to the LLM for structured
// extraction against the current phase's InputSchema, and returns the prefilled
// field values that the frontend can populate into the form.
//
// Parameters:
//   - filePath: absolute path to the uploaded resume file (.pdf, .docx, .md, .txt)
//   - phaseID: the workflow phase ID (to look up the InputSchema)
//
// Returns JSON-serialized map of field name → {value, source, confidence, ...}
func (a *App) ParseResumeForWorkflowForm(filePath, phaseID string) string {
	userID := desktopUserID

	// Get the current workflow state and phase schema
	if a.workflowV2 == nil {
		return marshalResumeError("工作流引擎未初始化")
	}

	state := a.workflowV2.machine.GetActive(userID)
	if state == nil {
		return marshalResumeError("没有活跃的工作流")
	}

	// Find the target phase's InputSchema
	var schema *v2.PhaseInputSchema
	for i := range state.Phases {
		if state.Phases[i].ID == phaseID {
			schema = state.Phases[i].InputSchema
			break
		}
	}
	if schema == nil {
		return marshalResumeError("找不到阶段 " + phaseID + " 的表单定义")
	}

	// Read and extract text from the file
	resumeText, err := extractTextFromFile(filePath)
	if err != nil {
		return marshalResumeError(fmt.Sprintf("读取文件失败: %v", err))
	}
	// Sanitize: remove null bytes and control characters that corrupt JSON/LLM prompts
	resumeText = sanitizeExtractedText(resumeText)
	if strings.TrimSpace(resumeText) == "" {
		return marshalResumeError("文件内容为空，无法提取信息")
	}

	log.Printf("[workflow-v2-resume] parsing resume for phase=%s file=%s text_len=%d",
		phaseID, filepath.Base(filePath), len([]rune(resumeText)))

	// Call LLM to extract structured fields from resume
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	caller := &appResumeLLMCaller{app: a}
	result, err := v2.ParseResumeForSchema(ctx, v2.ResumeParseRequest{
		ResumeText: resumeText,
		Schema:     schema,
	}, caller)
	if err != nil {
		log.Printf("[workflow-v2-resume] LLM parse failed: %v", err)
		return marshalResumeError(classifyResumeParseError(err))
	}

	// Convert to prefilled values format (same as memory prefill)
	prefilled := v2.ResumeParseResultToPrefilled(result, schema)
	if len(prefilled) == 0 {
		return marshalResumeError("未能从简历中提取到有效信息")
	}

	log.Printf("[workflow-v2-resume] extracted %d fields from resume for phase=%s",
		len(prefilled), phaseID)

	// Also sediment the extracted data to memory for future use
	// (so next time, even without uploading resume, memory recall can prefill)
	// Capture phaseID and schema locally — state pointer may be mutated concurrently.
	sedimentPhaseID := phaseID
	sedimentState := state
	go func() {
		formData := make(map[string]interface{}, len(prefilled))
		for name, pv := range prefilled {
			formData[name] = pv.Value
		}
		hubClient := a.ensureHubClient()
		if hubClient != nil {
			handler := hubClient.ensureIMHandler()
			if handler != nil {
				handler.sedimentFormDataToMemory(userID, sedimentPhaseID, formData, sedimentState)
			}
		}
	}()

	// Return as JSON for frontend consumption — unified response structure
	resp := resumeParseResponse{Data: prefilled}
	data, err := json.Marshal(resp)
	if err != nil {
		return marshalResumeError("序列化结果失败")
	}
	return string(data)
}

// --- LLM caller implementation ---

// appResumeLLMCaller implements v2.ResumeLLMCaller using the app's configured LLM.
type appResumeLLMCaller struct {
	app *App
}

func (c *appResumeLLMCaller) CallLLMForResumeParse(ctx context.Context, systemPrompt, userPrompt string) (string, error) {
	if c.app == nil {
		return "", fmt.Errorf("app is nil")
	}

	hubClient := c.app.hubClient()
	if hubClient == nil {
		return "", fmt.Errorf("no LLM configured")
	}
	handler := hubClient.ensureIMHandler()
	if handler == nil {
		return "", fmt.Errorf("no LLM configured")
	}

	req := LLMClassifyRequest{
		SystemPrompt:      systemPrompt,
		UserMessage:       userPrompt,
		TimeoutSec:        30,
		Tag:               "workflow-resume-parse",
		PreferLightweight: false, // resume parsing needs full capability model
	}

	result, err := handler.LLMClassify(ctx, req)
	if err != nil {
		return "", err
	}
	return result.Text, nil
}

// --- File text extraction ---

// extractTextFromFile reads a file and extracts its text content.
// Supports: .txt, .md (direct read), .pdf (via pymupdf subprocess), .docx (via python-docx subprocess).
// For unsupported formats, attempts to read as plain text.
func extractTextFromFile(filePath string) (string, error) {
	ext := strings.ToLower(filepath.Ext(filePath))

	switch ext {
	case ".txt", ".md", ".markdown":
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", err
		}
		return string(data), nil

	case ".pdf":
		// Primary: Go-native PDF text extraction (no Python required)
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("读取 PDF 文件失败: %v", err)
		}
		text, err := extractPDFTextNative(data)
		if err == nil && strings.TrimSpace(text) != "" {
			return text, nil
		}
		// Fallback: Python pymupdf (better quality for complex PDFs)
		return extractTextViaPython(filePath, "import fitz; doc=fitz.open(r'%s'); print('\\n'.join(page.get_text() for page in doc))")

	case ".docx":
		// Go-native DOCX extraction via zip + XML parsing (same as knowledge/parse.go)
		return extractDocxTextNative(filePath)

	case ".doc":
		// Legacy Word 97-2003 format — Go-native via LegacyOfficeReader
		return extractDocTextNative(filePath)

	default:
		// Try reading as plain text
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", fmt.Errorf("unsupported file type %s: %v", ext, err)
		}
		return string(data), nil
	}
}

// extractTextViaPython runs a Python one-liner to extract text from a file.
// Falls back to error message with installation instructions if the library is missing.
func extractTextViaPython(filePath, pythonTemplate string) (string, error) {
	// Escape for Python raw string: backslashes doubled, single quotes escaped.
	// We use the path as a Python argument via sys.argv to avoid injection entirely.
	// Template uses sys.argv[1] instead of hardcoded path.
	safeTemplate := strings.ReplaceAll(pythonTemplate, "r'%s'", "sys.argv[1]")
	script := "import sys; " + safeTemplate

	// Build list of Python executables to try:
	// 1. maclaw's bundled uv venv Python ({MaclawBaseDir}/python/venv/Scripts/python.exe)
	// 2. maclaw's bundled uv install Python ({MaclawBaseDir}/python/install/python.exe)
	// 3. System python / python3
	pyCmds := []string{"python", "python3"}
	maclawBase := corelib.MaclawBaseDir()
	// uv venv has installed packages (pymupdf etc.) — prefer it
	for _, relPath := range []string{
		filepath.Join("python", "venv", "Scripts", "python.exe"), // Windows uv venv
		filepath.Join("python", "venv", "bin", "python"),         // Linux/Mac uv venv
		filepath.Join("python", "install", "python.exe"),         // Windows uv install (bare)
		filepath.Join("python", "install", "bin", "python"),      // Linux/Mac uv install
	} {
		candidate := filepath.Join(maclawBase, relPath)
		if _, err := os.Stat(candidate); err == nil {
			pyCmds = append([]string{candidate}, pyCmds...)
			break // use the first found
		}
	}

	// Try each Python executable
	for _, pyCmd := range pyCmds {
		// Use a 30s timeout to prevent hanging on corrupted files
		pyCtx, pyCancel := context.WithTimeout(context.Background(), 30*time.Second)
		cmd := exec.CommandContext(pyCtx, pyCmd, "-c", script, filePath)
		cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8", "PYTHONUTF8=1")
		output, err := cmd.Output()
		pyCancel()
		if err == nil {
			text := strings.TrimSpace(string(output))
			if text != "" {
				return text, nil
			}
		}
	}

	return "", fmt.Errorf("无法解析文件 %s：需要安装 Python 及相关库（pymupdf/python-docx）", filepath.Base(filePath))
}

// extractPDFTextNative uses the Go-native GoPDF2 library to extract text from PDF bytes.
// No Python dependency required. Falls back gracefully if extraction fails.
func extractPDFTextNative(pdfData []byte) (string, error) {
	text, err := gopdf2.ExtractAllPagesText(pdfData)
	if err != nil {
		return "", err
	}
	return text, nil
}

// extractDocxTextNative extracts text from a .docx file using pure Go (zip + XML).
// Same approach as knowledge/parse.go:parseDOCXNodes but returns plain text instead of nodes.
func extractDocxTextNative(filePath string) (string, error) {
	r, err := zip.OpenReader(filePath)
	if err != nil {
		return "", fmt.Errorf("无法打开 DOCX 文件: %v", err)
	}
	defer r.Close()

	var documentXML []byte
	for _, f := range r.File {
		if f.Name == "word/document.xml" {
			rc, err := f.Open()
			if err != nil {
				return "", err
			}
			documentXML, err = io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return "", err
			}
			break
		}
	}
	if len(documentXML) == 0 {
		return "", fmt.Errorf("DOCX 文件中未找到 document.xml")
	}

	// Parse XML to extract paragraph text
	decoder := xml.NewDecoder(bytes.NewReader(documentXML))
	var paragraphs []string
	var paragraph strings.Builder
	inParagraph := false
	for {
		tok, err := decoder.Token()
		if err != nil {
			break // EOF or error — flush what we have
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "p":
				if inParagraph && paragraph.Len() > 0 {
					paragraphs = append(paragraphs, strings.TrimSpace(paragraph.String()))
					paragraph.Reset()
				}
				inParagraph = true
			case "tab":
				if inParagraph {
					paragraph.WriteString("\t")
				}
			case "br", "cr":
				if inParagraph {
					paragraph.WriteString("\n")
				}
			}
		case xml.EndElement:
			if t.Name.Local == "p" && inParagraph {
				if paragraph.Len() > 0 {
					paragraphs = append(paragraphs, strings.TrimSpace(paragraph.String()))
					paragraph.Reset()
				}
				inParagraph = false
			}
		case xml.CharData:
			if inParagraph {
				paragraph.Write(t)
			}
		}
	}
	if paragraph.Len() > 0 {
		paragraphs = append(paragraphs, strings.TrimSpace(paragraph.String()))
	}

	text := strings.Join(paragraphs, "\n")
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("DOCX 文件中没有可读取的文本内容")
	}
	return text, nil
}

// extractDocTextNative extracts text from a legacy .doc file (Word 97-2003)
// using the Go-native LegacyOfficeReader library. No Python required.
// Uses recover to handle panics from malformed .doc files gracefully.
func extractDocTextNative(filePath string) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("DOC 文件解析异常: %v", r)
		}
	}()
	document, openErr := legacydoc.OpenFile(filePath)
	if openErr != nil {
		return "", fmt.Errorf("无法打开 DOC 文件: %v", openErr)
	}
	text = document.GetText()
	if strings.TrimSpace(text) == "" {
		return "", fmt.Errorf("DOC 文件中没有可读取的文本内容")
	}
	return text, nil
}

// classifyResumeParseError converts technical LLM errors into user-friendly messages.
func classifyResumeParseError(err error) string {
	msg := err.Error()
	lower := strings.ToLower(msg)

	switch {
	case strings.Contains(lower, "context") && strings.Contains(lower, "exceeded"):
		return "简历内容过长，超出模型处理能力。请尝试缩短简历或仅保留关键信息部分。"
	case strings.Contains(lower, "timeout") || strings.Contains(lower, "deadline"):
		return "简历解析超时，请稍后重试。"
	case strings.Contains(lower, "rate limit") || strings.Contains(lower, "429"):
		return "API 调用频率受限，请稍后再试。"
	case strings.Contains(lower, "no llm configured"):
		return "未配置语言模型，无法解析简历。请先在设置中配置 LLM。"
	case strings.Contains(lower, "parse"):
		return "简历内容解析失败，模型返回格式异常。请确认简历内容完整后重试。"
	default:
		return fmt.Sprintf("简历解析失败: %v", err)
	}
}

// sanitizeExtractedText removes null bytes, control characters, and excessive whitespace
// from extracted document text. PDF/DOCX extractors sometimes include garbage characters
// that corrupt JSON encoding and waste LLM prompt tokens.
func sanitizeExtractedText(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	prevWasNewline := false
	for _, r := range s {
		// Remove null bytes and most control characters (keep \n, \t)
		if r == 0 || (r < 0x20 && r != '\n' && r != '\t' && r != '\r') {
			continue
		}
		// Collapse more than 2 consecutive newlines
		if r == '\n' {
			if prevWasNewline {
				continue // skip extra blank lines
			}
			prevWasNewline = true
		} else if r != '\r' {
			prevWasNewline = false
		}
		sb.WriteRune(r)
	}
	return strings.TrimSpace(sb.String())
}

// --- Response helpers ---

type resumeParseResponse struct {
	Error string                       `json:"error,omitempty"`
	Data  map[string]*v2.PrefilledValue `json:"data,omitempty"`
}

func marshalResumeError(msg string) string {
	resp := resumeParseResponse{Error: msg}
	data, _ := json.Marshal(resp)
	return string(data)
}
