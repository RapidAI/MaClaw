package main

func workflowDocPhaseIDSchemaDescription() string {
	return "Optional workflow phase enum: requirements, design, or tasks. When set, workflow document writes use the stable ASCII phase filename."
}

func workflowDocTypeSchemaDescription() string {
	return "Optional workflow document type enum: requirements, design, or task_plan. When set, workflow document writes use the stable ASCII phase filename."
}

func workflowDocDeliveryPhaseIDSchemaDescription() string {
	return "Optional workflow phase enum: requirements, design, or tasks. When set, display file_name is normalized to the stable ASCII phase filename."
}

func workflowDocDeliveryTypeSchemaDescription() string {
	return "Optional workflow document type enum: requirements, design, or task_plan. When set, display file_name is normalized to the stable ASCII phase filename."
}

func workflowDocGeneratePDFPhaseIDSchemaDescription() string {
	return "Optional workflow phase enum: requirements, design, or tasks. When set, it selects the workflow document type and stable ASCII filename prefix."
}
