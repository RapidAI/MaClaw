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
			LabelSSH:    {"ssh"},
			LabelSearch: {"web_search"},
			LabelDocumentDelivery: {"send_file", "open", "craft_tool"},
			LabelBrowser: {
				"browser_navigate", "browser_click", "browser_type",
				"browser_screenshot", "browser_scroll", "browser_wait",
				"browser_execute_js", "browser_select", "browser_hover",
				"browser_drag", "browser_upload", "browser_download",
				"browser_tab_new", "browser_tab_close", "browser_tab_switch",
				"browser_cookie_get", "browser_cookie_set", "browser_dialog_handle",
				"browser_pdf", "browser_network_intercept", "browser_evaluate",
				"browser_close", "browser_back", "browser_forward",
				"browser_refresh",
				"gui_record_start", "gui_record_stop",
			},
			LabelOffice:       {"office"},
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
