package structureddata

type QueryBusinessActionsInput struct {
	Limit    int    `json:"limit,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type BusinessAction struct {
	ID             string                 `json:"id"`
	Domain         string                 `json:"domain"`
	Title          string                 `json:"title"`
	Description    string                 `json:"description,omitempty"`
	DatasetID      string                 `json:"dataset_id"`
	EventType      string                 `json:"event_type"`
	Operation      string                 `json:"operation"`
	RequiredFields []string               `json:"required_fields,omitempty"`
	SuggestedTags  []string               `json:"suggested_tags,omitempty"`
	InputFields    []DatasetTemplateField `json:"input_fields,omitempty"`
}

type ExecuteBusinessActionInput struct {
	RecordID       string         `json:"record_id,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Title          string         `json:"title,omitempty"`
	Tags           []string       `json:"tags,omitempty"`
	Data           map[string]any `json:"data"`
	OccurredAt     string         `json:"occurred_at,omitempty"`
	DryRun         bool           `json:"dry_run,omitempty"`
}

type ExecuteBusinessActionResult struct {
	Action     BusinessAction          `json:"action"`
	DryRun     bool                    `json:"dry_run,omitempty"`
	Valid      bool                    `json:"valid"`
	Validation *ValidateRecordResult   `json:"validation,omitempty"`
	Preview    map[string]interface{}  `json:"preview,omitempty"`
	Event      *DataEventResult        `json:"event,omitempty"`
	Rules      *BusinessRuleEvaluation `json:"rules,omitempty"`
}

type BusinessViewDefinition struct {
	ID            string         `json:"id"`
	Domain        string         `json:"domain"`
	Title         string         `json:"title"`
	Description   string         `json:"description,omitempty"`
	DatasetID     string         `json:"dataset_id"`
	Fields        []string       `json:"fields"`
	DefaultFilter map[string]any `json:"default_filter,omitempty"`
	DefaultSort   []SortSpec     `json:"default_sort,omitempty"`
	DefaultLimit  int            `json:"default_limit,omitempty"`
}

type QueryBusinessViewsInput struct {
	Limit    int    `json:"limit,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type BusinessViewResult struct {
	View         BusinessViewDefinition `json:"view"`
	Records      []Record               `json:"records"`
	Limit        int                    `json:"limit"`
	HasMore      bool                   `json:"has_more"`
	NextBefore   string                 `json:"next_before,omitempty"`
	NextBeforeID string                 `json:"next_before_id,omitempty"`
}

type BusinessDomainCatalog struct {
	Domain           string                   `json:"domain"`
	Title            string                   `json:"title"`
	Initialized      bool                     `json:"initialized"`
	UseCases         []BusinessDomainUseCase  `json:"use_cases,omitempty"`
	Datasets         []DatasetCapability      `json:"datasets,omitempty"`
	Templates        []DatasetTemplate        `json:"templates,omitempty"`
	MissingTemplates []string                 `json:"missing_templates,omitempty"`
	BusinessActions  []BusinessAction         `json:"business_actions,omitempty"`
	BusinessViews    []BusinessViewDefinition `json:"business_views,omitempty"`
	Dashboards       []DashboardDefinition    `json:"dashboards,omitempty"`
	Reports          []ReportDefinition       `json:"reports,omitempty"`
}

type QueryBusinessDomainsInput struct {
	Limit    int    `json:"limit,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type BusinessDomainUseCase struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Description        string   `json:"description,omitempty"`
	IntentHints        []string `json:"intent_hints,omitempty"`
	PreferredAction    string   `json:"preferred_action,omitempty"`
	PreferredView      string   `json:"preferred_view,omitempty"`
	PreferredReport    string   `json:"preferred_report,omitempty"`
	PreferredDashboard string   `json:"preferred_dashboard,omitempty"`
	RequiresAdmin      bool     `json:"requires_admin,omitempty"`
	DryRunRecommended  bool     `json:"dry_run_recommended,omitempty"`
}

type ResolveBusinessIntentInput struct {
	Query  string `json:"query"`
	Domain string `json:"domain,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type BusinessIntentMatch struct {
	Domain           string                   `json:"domain"`
	Title            string                   `json:"title"`
	UseCase          BusinessDomainUseCase    `json:"use_case"`
	Score            int                      `json:"score"`
	Confidence       float64                  `json:"confidence,omitempty"`
	Decision         string                   `json:"decision,omitempty"`
	BusinessObjectID string                   `json:"business_object_id,omitempty"`
	BusinessActionID string                   `json:"business_action_id,omitempty"`
	IntentSignals    []string                 `json:"intent_signals,omitempty"`
	Matched          []string                 `json:"matched,omitempty"`
	NextSteps        []BusinessIntentNextStep `json:"next_steps,omitempty"`
}

type BusinessIntentNextStep struct {
	Order            int                    `json:"order"`
	Action           string                 `json:"action"`
	Purpose          string                 `json:"purpose"`
	Description      string                 `json:"description,omitempty"`
	AdminOnly        bool                   `json:"admin_only,omitempty"`
	DryRun           bool                   `json:"dry_run,omitempty"`
	RequiredFields   []string               `json:"required_fields,omitempty"`
	InputFields      []DatasetTemplateField `json:"input_fields,omitempty"`
	DataTemplate     map[string]any         `json:"data_template,omitempty"`
	BodyTemplate     map[string]any         `json:"body_template,omitempty"`
	ToolCallTemplate map[string]any         `json:"tool_call_template,omitempty"`
	Params           map[string]any         `json:"params,omitempty"`
}

type ResolveBusinessIntentResult struct {
	Query   string                `json:"query"`
	Domain  string                `json:"domain,omitempty"`
	Matches []BusinessIntentMatch `json:"matches"`
}

type QueryRelationshipsInput struct {
	DatasetID string `json:"dataset_id,omitempty"`
	Limit     int    `json:"limit,omitempty"`
	BeforeID  string `json:"before_id,omitempty"`
}

type DatasetRelationship struct {
	SourceDatasetID string `json:"source_dataset_id"`
	SourceField     string `json:"source_field"`
	SourceTitle     string `json:"source_title,omitempty"`
	TargetDatasetID string `json:"target_dataset_id,omitempty"`
	FieldType       string `json:"field_type"`
	FromTemplate    bool   `json:"from_template,omitempty"`
	Initialized     bool   `json:"initialized,omitempty"`
}

type QueryRelatedRecordsInput struct {
	Limit    int    `json:"limit,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type RelatedRecordLink struct {
	Direction    string              `json:"direction"`
	Relationship DatasetRelationship `json:"relationship"`
	Record       *Record             `json:"record,omitempty"`
	Missing      bool                `json:"missing,omitempty"`
	Message      string              `json:"message,omitempty"`
}

type RelatedRecordsResult struct {
	DatasetID    string              `json:"dataset_id"`
	RecordID     string              `json:"record_id"`
	Record       *Record             `json:"record,omitempty"`
	Links        []RelatedRecordLink `json:"links"`
	Limit        int                 `json:"limit"`
	HasMore      bool                `json:"has_more"`
	NextBeforeID string              `json:"next_before_id,omitempty"`
}
