package main

import (
	"encoding/json"
	"log"
	"strings"
)

type officeToolAction string

const (
	officeToolActionUnknown     officeToolAction = ""
	officeToolActionGeneratePDF officeToolAction = "generate_pdf"
)

func normalizeOfficeToolAction(action string) officeToolAction {
	switch officeToolAction(strings.TrimSpace(action)) {
	case officeToolActionGeneratePDF:
		return officeToolActionGeneratePDF
	default:
		return officeToolActionUnknown
	}
}

// SteeringWorkflowDetector tracks steering-driven coding workflow state
// within a single agent loop invocation.
type SteeringWorkflowDetector struct {
	detected               bool              // whether a coding workflow has been detected
	suggestMaximizeEmitted bool              // whether suggest_maximize event has been emitted
	phaseDocuments         map[string]string // detected phase documents (phaseID -> content)
	userID                 string            // current user ID
}

// NewSteeringWorkflowDetector creates a new detector for the given user.
func NewSteeringWorkflowDetector(userID string) *SteeringWorkflowDetector {
	return &SteeringWorkflowDetector{
		detected:       true,
		phaseDocuments: make(map[string]string),
		userID:         userID,
	}
}

func (h *IMMessageHandler) emitAgentLoopSteeringSuggestMaximize(userID string, detector *SteeringWorkflowDetector, gateConfig codingToolGateConfig) {
	if detector == nil || detector.suggestMaximizeEmitted || !gateConfig.active {
		return
	}
	detector.suggestMaximizeEmitted = true
	if h.getWorkflowEngine() == nil {
		return
	}
	if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
		adapter.EmitSuggestMaximize(userID, "coding")
		log.Printf("[SteeringWorkflow] emitted suggest_maximize for user=%s (gate active, first tool call)", userID)
	}
}

func (h *IMMessageHandler) emitAgentLoopSteeringDocUpdate(userID string, detector *SteeringWorkflowDetector, toolName string, argsJSON string) {
	if detector == nil {
		return
	}
	detector.interceptToolCall(toolName, argsJSON, func(phaseID, content string) {
		if h.getWorkflowEngine() == nil {
			return
		}
		if adapter, ok := h.getWorkflowEngine().GetCallbacks().(*GUIWorkflowAdapter); ok {
			_ = adapter.EmitDocUpdate(userID, phaseID, content)
			log.Printf("[SteeringWorkflow] emitted doc_update for user=%s phase=%s len=%d", userID, phaseID, len(content))
		}
	})
}

// isCodingTask checks whether the message text matches coding task keywords
// from the steering workflow rules. This uses a focused keyword list aligned
// with the coding-workflow.md steering file.
func (d *SteeringWorkflowDetector) isCodingTask(message string) bool {
	lower := strings.ToLower(strings.TrimSpace(message))
	if lower == "" {
		return false
	}

	keywords := []string{
		"code", "coding", "implement", "create", "modify code", "refactor",
		"fix bug", "design architecture", "add feature", "new feature",
	}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

// matchPhaseID extracts a workflow phase ID from a file name by matching
// known patterns for requirements, design, and tasks documents.
func (d *SteeringWorkflowDetector) matchPhaseID(fileName string) string {
	lower := strings.ToLower(strings.TrimSpace(fileName))
	if lower == "" {
		return ""
	}
	switch {
	case strings.Contains(lower, "tasks") ||
		strings.Contains(lower, "task_breakdown") ||
		strings.Contains(lower, "task breakdown") ||
		strings.Contains(lower, "task_plan"):
		return workflowPhaseTasks
	case strings.Contains(lower, "requirements"):
		return workflowPhaseRequirements
	case strings.Contains(lower, "design"):
		return workflowPhaseDesign
	default:
		return ""
	}
}

// interceptToolCall checks a tool call for workflow phase documents and
// invokes the emit callback with (phaseID, content) when a match is found.
func (d *SteeringWorkflowDetector) interceptToolCall(toolName, argsJSON string, emit func(phaseID, content string)) {
	if !d.detected || emit == nil {
		return
	}
	switch classifyAgentToolKind(toolName) {
	case agentToolKindWriteFile:
		var args struct {
			Path    string `json:"path"`
			Content string `json:"content"`
			PhaseID string `json:"phase_id"`
			DocType string `json:"doc_type"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return
		}
		phaseID := workflowPhaseFromMetadata(args.PhaseID, args.DocType)
		if phaseID == "" {
			phaseID = d.matchPhaseID(args.Path)
		}
		if phaseID == "" || args.Content == "" {
			return
		}
		d.phaseDocuments[phaseID] = args.Content
		emit(phaseID, args.Content)

	case agentToolKindGeneratePDF:
		var args struct {
			MarkdownContent string `json:"markdown_content"`
			Content         string `json:"content"`
			PhaseID         string `json:"phase_id"`
			DocType         string `json:"doc_type"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return
		}
		content := args.MarkdownContent
		if content == "" {
			content = args.Content
		}
		if content == "" {
			return
		}
		phaseID := workflowPhaseFromMetadata(args.PhaseID, args.DocType)
		if phaseID == "" {
			return
		}
		d.phaseDocuments[phaseID] = content
		emit(phaseID, content)

	case agentToolKindOffice:
		var args struct {
			Action          string `json:"action"`
			MarkdownContent string `json:"markdown_content"`
			Content         string `json:"content"`
			PhaseID         string `json:"phase_id"`
			DocType         string `json:"doc_type"`
		}
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
			return
		}
		if normalizeOfficeToolAction(args.Action) != officeToolActionGeneratePDF {
			return
		}
		content := args.MarkdownContent
		if content == "" {
			content = args.Content
		}
		if content == "" {
			return
		}
		phaseID := workflowPhaseFromMetadata(args.PhaseID, args.DocType)
		if phaseID == "" {
			return
		}
		d.phaseDocuments[phaseID] = content
		emit(phaseID, content)
	}
}

// extractFencedDocument extracts document content between leading `---`
// delimiters, or falls back to the first Markdown heading.
func extractFencedDocument(text string) string {
	lines := strings.Split(text, "\n")
	firstIdx := -1
	secondIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if len(trimmed) < 3 || strings.Trim(trimmed, "-") != "" {
			continue
		}
		if firstIdx < 0 {
			if i > 4 {
				break
			}
			if i > 0 {
				prev := strings.TrimSpace(lines[i-1])
				if strings.HasPrefix(prev, "#") {
					continue
				}
			}
			firstIdx = i
		} else {
			secondIdx = i
			break
		}
	}
	if firstIdx >= 0 && secondIdx > firstIdx+1 {
		inner := strings.Join(lines[firstIdx+1:secondIdx], "\n")
		inner = strings.TrimSpace(inner)
		if len(inner) > 100 {
			return inner
		}
	}
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			stripped := strings.Join(lines[i:], "\n")
			stripped = strings.TrimSpace(stripped)
			if len(stripped) > 100 {
				return stripped
			}
			break
		}
	}
	return text
}

// interceptTextOutput checks the LLM's plain text output for workflow phase
// documents.
func (d *SteeringWorkflowDetector) interceptTextOutput(text string, emit func(phaseID, content string)) {
	if !d.detected || emit == nil || strings.TrimSpace(text) == "" {
		return
	}
	if len(text) < 200 {
		return
	}
	headingArea := text
	if len(headingArea) > 500 {
		headingArea = headingArea[:500]
	}
	phaseID := d.matchPhaseID(headingArea)
	if phaseID == "" {
		if len(d.phaseDocuments) == 0 {
			phaseID = workflowPhaseRequirements
		} else {
			return
		}
	}
	docContent := extractFencedDocument(text)
	if existing, ok := d.phaseDocuments[phaseID]; ok && existing == docContent {
		return
	}
	d.phaseDocuments[phaseID] = docContent
	emit(phaseID, docContent)
}
