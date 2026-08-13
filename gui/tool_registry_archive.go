package main

// registerArchiveBuiltinTool keeps the archive schema and its GUI-local
// handler together without coupling corelib/archiveutil to the GUI registry.
func registerArchiveBuiltinTool(registry *ToolRegistry, h *IMMessageHandler) {
	if registry == nil || h == nil {
		return
	}
	_ = registry.Register(RegisteredTool{
		Name:        "archive",
		Description: "Inspect and extract common archives using embedded pure-Go handlers, or create a ZIP file. Actions: inspect, extract, extract_external, create_zip. ZIP, TAR, GZ, TAR.GZ, BZ2, TAR.BZ2, and RAR are handled without shell commands. Unsupported formats return a structured external fallback plan. extract_external asks the user to approve the exact request, then only runs an already-installed external program; it never installs software. All write actions create new outputs and fail if the destination already exists.",
		Category:    ToolCategoryBuiltin,
		Tags:        []string{"file", "archive", "compress", "extract", "zip"},
		Status:      RegToolAvailable,
		Source:      "builtin",
		InputSchema: map[string]interface{}{
			"action":          map[string]string{"type": "string", "description": "inspect | extract | extract_external | create_zip"},
			"archive_path":    map[string]string{"type": "string", "description": "Archive path for inspect or extract."},
			"destination":     map[string]string{"type": "string", "description": "New destination directory for extract."},
			"conflict_policy": map[string]string{"type": "string", "description": "Optional; only fail is supported (the default)."},
			"root_mode":       map[string]string{"type": "string", "description": "Optional for create_zip; only preserve is supported (the default)."},
			"source_paths":    map[string]interface{}{"type": "array", "description": "File or directory paths for create_zip.", "items": map[string]string{"type": "string"}},
			"output_path":     map[string]string{"type": "string", "description": "New .zip output path for create_zip."},
		},
		Required:          []string{"action"},
		ExecutionContract: defaultExplicitExecutionContractMetadata("archive"),
		Handler:           h.toolArchive,
	})
}
