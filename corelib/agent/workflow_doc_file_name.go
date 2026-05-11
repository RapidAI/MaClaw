package agent

import (
	"encoding/base64"
	"path/filepath"
	"strings"
	"unicode"
)

type workflowDocPhaseKind string

const (
	workflowDocPhaseUnknown      workflowDocPhaseKind = ""
	workflowDocPhaseRequirements workflowDocPhaseKind = "requirements"
	workflowDocPhaseDesign       workflowDocPhaseKind = "design"
	workflowDocPhaseTasks        workflowDocPhaseKind = "tasks"
)

func normalizeWorkflowDocPhaseKind(value string) workflowDocPhaseKind {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "requirements", "requirement", "requirements_doc":
		return workflowDocPhaseRequirements
	case "design", "tech_design", "technical_design":
		return workflowDocPhaseDesign
	case "tasks", "task", "task_breakdown", "task_list", "task_plan":
		return workflowDocPhaseTasks
	default:
		return workflowDocPhaseUnknown
	}
}

func (k workflowDocPhaseKind) String() string {
	return string(k)
}

func workflowDocPhaseFromMetadata(phaseID, docType string) workflowDocPhaseKind {
	if phase := normalizeWorkflowDocPhaseKind(phaseID); phase != workflowDocPhaseUnknown {
		return phase
	}
	return normalizeWorkflowDocPhaseKind(docType)
}

func workflowDocStableFileName(phase workflowDocPhaseKind) string {
	switch phase {
	case workflowDocPhaseRequirements:
		return "01-requirements.md"
	case workflowDocPhaseDesign:
		return "02-technical-design.md"
	case workflowDocPhaseTasks:
		return "03-task-breakdown.md"
	default:
		return "workflow-phase.md"
	}
}

func workflowDocStableFileNameWithExt(phase workflowDocPhaseKind, ext string) string {
	base := strings.TrimSuffix(workflowDocStableFileName(phase), ".md")
	ext = stableWorkflowDocExt(ext)
	if ext == "" {
		ext = ".md"
	}
	return base + ext
}

func stableWorkflowDocExt(ext string) string {
	ext = strings.TrimSpace(ext)
	if ext == "" {
		return ""
	}
	if !strings.HasPrefix(ext, ".") {
		ext = "." + ext
	}
	ext = strings.ToLower(ext)
	for _, r := range ext {
		if r > unicode.MaxASCII || !(r == '.' || r == '_' || r == '-' || r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	return ext
}

func workflowDocWritePath(path string, args map[string]interface{}) string {
	phase := workflowDocPhaseFromMetadata(StringArg(args, "phase_id"), StringArg(args, "doc_type"))
	if phase == workflowDocPhaseUnknown {
		return path
	}
	dir := workflowDocWriteDir(strings.TrimSpace(path))
	return filepath.Join(dir, workflowDocStableFileName(phase))
}

func workflowDocWriteDir(path string) string {
	dir := filepath.Dir(path)
	if dir == "." || dir == "" {
		return "."
	}
	if isLocalizedWorkflowDocDir(filepath.Base(dir)) {
		if parent := filepath.Dir(dir); parent != "" {
			return parent
		}
		return "."
	}
	return dir
}

func workflowDocDeliveryFileNameWithFallbackExt(fileName string, args map[string]interface{}, fallbackExt string) string {
	phase := workflowDocPhaseFromMetadata(StringArg(args, "phase_id"), StringArg(args, "doc_type"))
	if phase == workflowDocPhaseUnknown {
		return fileName
	}
	ext := stableWorkflowDocExt(filepath.Ext(strings.TrimSpace(fileName)))
	if ext == "" {
		ext = stableWorkflowDocExt(fallbackExt)
	}
	return workflowDocStableFileNameWithExt(phase, ext)
}

func workflowDocDeliveryMessagePayloadFlag(args map[string]interface{}) string {
	phase := workflowDocPhaseFromMetadata(StringArg(args, "phase_id"), StringArg(args, "doc_type"))
	if phase == workflowDocPhaseUnknown {
		return ""
	}
	message := strings.TrimSpace(InferFileDeliveryMessageForDocType(phase.String(), ""))
	if message == "" {
		return ""
	}
	return "msg64:" + base64.RawURLEncoding.EncodeToString([]byte(message))
}

func workflowDocSchemaPhaseIDDescription() string {
	return "Workflow document phase ID for stable filenames. Use requirements, design/tech_design, or tasks/task_breakdown/task_plan. When set, generated workflow document filenames are stable ASCII; display titles may be localized."
}

func workflowDocSchemaDocTypeDescription() string {
	return "Workflow document type alias for stable filenames: requirements, design, or task_plan. Prefer phase_id when available."
}

func isLocalizedWorkflowDocDir(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, r := range name {
		if r > unicode.MaxASCII {
			return true
		}
	}
	return false
}
