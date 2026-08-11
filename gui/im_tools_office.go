package main

import (
	"fmt"
	"path/filepath"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// toolOffice dispatches office tool calls by action parameter.
// Read paths are shared with corelib/agent (native multi-format extractors).
func (h *IMMessageHandler) toolOffice(args map[string]interface{}) string {
	if args == nil {
		args = map[string]interface{}{}
	}
	ownerID, hasRuntimeOwner := h.consumeRuntimePolicyOwnerIDFromToolArgsOrCurrentState(args)
	if hasRuntimeOwner && ownerID == "" {
		return "office failed: runtime owner is missing; isolated runtime will not fall back to desktop working directory"
	}
	action := stringVal(args, "action")
	// Normalize path alias before dispatch so shared handlers see file_path.
	if stringVal(args, "file_path") == "" {
		if p := stringVal(args, "path"); p != "" {
			args["file_path"] = p
		}
	}
	// Resolve paths against the *session* workdir (task tab →
	// …/tasks/<slug>-<id>/workspace), not the global EffectiveWorkspaceDir.
	// The shared file resolver also validates bot-profile directory boundaries
	// after absolute and home-relative paths have been normalized.
	if p := stringVal(args, "file_path"); p != "" {
		resolved, err := h.resolveOfficeFilePathForOwner(p, ownerID)
		if err != nil {
			return "office failed: " + err.Error()
		}
		args["file_path"] = resolved
	}

	switch action {
	case "generate_pdf":
		return h.toolGeneratePDF(args)
	case "read_document", "read", "read_doc", "read_docx", "read_pdf", "read_word":
		return agent.ToolReadDocument(args)
	case "read_excel":
		return agent.ToolReadExcel(args)
	case "write_excel":
		return agent.ToolWriteExcel(args)
	case "read_pptx":
		return agent.ToolReadPPTX(args)
	default:
		return fmt.Sprintf("未知的 office action: %q。支持的 action: generate_pdf, read_document, read_doc, read_docx, read_pdf, read_excel, write_excel, read_pptx", action)
	}
}

// resolveOfficeFilePathForOwner resolves and authorizes an Office path using
// the same owner-scoped boundary as read_file/write_file.  Office reads and
// write_excel must not become an alternate route around a bot profile's local
// directory restrictions.
func (h *IMMessageHandler) resolveOfficeFilePathForOwner(p, ownerID string) (string, error) {
	return h.resolveFileToolPathForOwner(p, ownerID)
}

// officeResolvedPath is a test helper: returns how office would resolve path
// for a given session owner without invoking document parsers.
func (h *IMMessageHandler) officeResolvedPathForOwner(path, ownerID string) string {
	resolved, err := h.resolveOfficeFilePathForOwner(path, ownerID)
	if err != nil {
		return ""
	}
	return filepath.Clean(resolved)
}
