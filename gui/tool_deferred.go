package main

// DeferredToolNames lists tools that are excluded from the initial prompt
// and only discoverable via the discover_tool tool. This implements
// "progressive tool discovery" — keeping the initial tool list lean
// (reducing context and model decision cost) while preserving full capability.
//
// Criteria for deferral:
// - Low frequency: rarely used in typical conversations
// - Self-contained: can be fully described when discovered (no ambient context needed)
// - Not time-critical: a one-round discover_tool detour is acceptable
var DeferredToolNames = []string{
	// Config management (merged into manage_config)
	"manage_config",
	// Schedule management (merged into manage_schedule)
	"manage_schedule",
	// Template management (merged into manage_template)
	"manage_template",
	// AgentNet P2P knowledge network
	"agentnet_search",
	"agentnet_publish",
	// Audit log query
	"query_audit_log",
	// Session templates (legacy names, kept for backward compat dispatch only)
	"create_template",
	"list_templates",
	"launch_template",
	// Config (legacy names)
	"get_config",
	"update_config",
	"batch_update_config",
	"list_config_schema",
	"export_config",
	"import_config",
	// Schedule (legacy names)
	"create_scheduled_task",
	"list_scheduled_tasks",
	"delete_scheduled_task",
	"update_scheduled_task",
	// Parallel execution
	"parallel_execute",
	// Tool recommendation
	"recommend_tool",
	// LLM provider switch
	"switch_llm_provider",
	// Structured question tool — deferred because LLM tends to misuse it
	// for coding workflow phase confirmations (popping button UI instead of
	// plain text). Available via discover_tool if genuinely needed.
	"ask_user",
}
