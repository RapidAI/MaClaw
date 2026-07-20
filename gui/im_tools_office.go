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
	action := stringVal(args, "action")
	// Normalize path alias before dispatch so shared handlers see file_path.
	if stringVal(args, "file_path") == "" {
		if p := stringVal(args, "path"); p != "" {
			args["file_path"] = p
		}
	}
	// Resolve relative paths against the *session* workdir (task tab →
	// …/tasks/<slug>-<id>/workspace), not the global EffectiveWorkspaceDir.
	// Absolute paths and ~ expansion are handled by resolvePathWithBase.
	if p := stringVal(args, "file_path"); p != "" {
		args["file_path"] = h.resolveOfficeFilePath(p)
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

// resolveOfficeFilePath resolves office tool file_path against the active
// session workdir (project/task tab), matching bash/read_file path semantics.
func (h *IMMessageHandler) resolveOfficeFilePath(p string) string {
	base := ""
	if h != nil {
		base = h.resolveToolWorkDirForOwner("", h.currentRuntimeOrLegacyPolicyOwnerID())
	}
	return resolvePathWithBase(p, base)
}

// officeResolvedPath is a test helper: returns how office would resolve path
// for a given session owner without invoking document parsers.
func (h *IMMessageHandler) officeResolvedPathForOwner(path, ownerID string) string {
	base := ""
	if h != nil {
		base = h.resolveToolWorkDirForOwner("", ownerID)
	}
	return filepath.Clean(resolvePathWithBase(path, base))
}
