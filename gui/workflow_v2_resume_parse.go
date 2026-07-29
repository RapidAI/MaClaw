package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	cskill "github.com/RapidAI/CodeClaw/corelib/skill"
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
		// Primary: shared native extractor (no Python required)
		if text, err := agent.ExtractPDFText(filePath); err == nil && strings.TrimSpace(text) != "" {
			return text, nil
		}
		// Fallback: Python pymupdf (better quality for complex PDFs)
		return extractTextViaPython(filePath, "import fitz; doc=fitz.open(r'%s'); print('\\n'.join(page.get_text() for page in doc))")

	case ".docx":
		return agent.ExtractDocxText(filePath)

	case ".doc":
		return agent.ExtractDocText(filePath)

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

	// Build list of Python executables to try.
	// Use the unified Python resolution from corelib/skill (checks system PATH
	// then bundled install Python), plus system fallbacks.
	var pyCmds []string
	if primary := cskill.FindPython(); primary != "" {
		pyCmds = append(pyCmds, primary)
	}
	// Always include system names as fallback (FindPython may return absolute path,
	// but user could have installed Python after app startup without cache refresh).
	for _, name := range []string{"python", "python3"} {
		found := false
		for _, existing := range pyCmds {
			if existing == name {
				found = true
				break
			}
		}
		if !found {
			pyCmds = append(pyCmds, name)
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
	Error string                        `json:"error,omitempty"`
	Data  map[string]*v2.PrefilledValue `json:"data,omitempty"`
}

func marshalResumeError(msg string) string {
	resp := resumeParseResponse{Error: msg}
	data, _ := json.Marshal(resp)
	return string(data)
}
