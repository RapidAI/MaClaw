package intent

// ToolAffinityRegistry maps IntentLabels to tool name sets.
// It is the single source of truth for which tools should be activated
// when a given intent is detected.
type ToolAffinityRegistry struct {
	mapping map[IntentLabel][]string
}

// NewToolAffinityRegistry creates the default registry with all intent-to-tool mappings.
func NewToolAffinityRegistry() *ToolAffinityRegistry {
	return &ToolAffinityRegistry{
		mapping: map[IntentLabel][]string{
			LabelSSH:              {"ssh"},
			LabelSearch:           {"web_search", "web_fetch", "download_file"},
			LabelLiveData:         {"web_search", "web_fetch", "download_file"},
			LabelDocumentDelivery: {"send_file", "send_to_im", "im_message"},
			LabelDocumentGenerate: {},
			LabelBusinessData:     {"mis_data"},
			LabelBrowser: {
				"browser",
			},
			LabelOffice:      {"office"},
			LabelScreenshot:  {"screenshot"},
			LabelAudioRecord: {"record_audio"},
			LabelCurrentTime: {"current_datetime"},
			LabelKnowledgeWrite: {
				"knowledge_save_text",
				"knowledge_save_url", "knowledge_save_urls",
				"knowledge_import_files", "knowledge_import_directory",
			},
			LabelCoding:       {"generate_pdf", "office"},
			LabelMaintenance:  {"generate_pdf", "office"},
			LabelBugFix:       {},
			LabelContinuation: {},
			LabelNonCoding:    {},
			LabelAmbiguous:    {},
			LabelUnknown:      {},
		},
	}
}

// ToolsFor returns the tool names associated with the given label.
// Returns nil if the label has no mapped tools.
func (r *ToolAffinityRegistry) ToolsFor(label IntentLabel) []string {
	return r.mapping[label]
}

// Resolve returns the union of tool names for primary + secondary labels.
// Duplicates are removed.
func (r *ToolAffinityRegistry) Resolve(primary IntentLabel, secondary []IntentLabel) []string {
	seen := make(map[string]bool)
	var result []string

	for _, name := range r.mapping[primary] {
		if !seen[name] {
			seen[name] = true
			result = append(result, name)
		}
	}

	for _, label := range secondary {
		for _, name := range r.mapping[label] {
			if !seen[name] {
				seen[name] = true
				result = append(result, name)
			}
		}
	}

	return result
}
