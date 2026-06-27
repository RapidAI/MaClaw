package structureddata

import "time"

type DataEventInput struct {
	Source         string         `json:"source"`
	EventType      string         `json:"event_type"`
	Operation      string         `json:"operation,omitempty"`
	BusinessAction string         `json:"business_action_id,omitempty"`
	DatasetID      string         `json:"dataset_id"`
	RecordID       string         `json:"record_id,omitempty"`
	IdempotencyKey string         `json:"idempotency_key,omitempty"`
	Title          string         `json:"title,omitempty"`
	Tags           []string       `json:"tags,omitempty"`
	Data           map[string]any `json:"data,omitempty"`
	OccurredAt     string         `json:"occurred_at,omitempty"`
	DryRun         bool           `json:"dry_run,omitempty"`
}

type DataEventResult struct {
	Status         string                `json:"status"`
	Duplicate      bool                  `json:"duplicate,omitempty"`
	OriginalStatus string                `json:"original_status,omitempty"`
	DryRun         bool                  `json:"dry_run,omitempty"`
	Valid          bool                  `json:"valid,omitempty"`
	Validation     *ValidateRecordResult `json:"validation,omitempty"`
	Preview        map[string]any        `json:"preview,omitempty"`
	Source         string                `json:"source"`
	EventType      string                `json:"event_type"`
	Operation      string                `json:"operation"`
	BusinessAction string                `json:"business_action_id,omitempty"`
	DatasetID      string                `json:"dataset_id"`
	RecordID       string                `json:"record_id,omitempty"`
	IdempotencyKey string                `json:"idempotency_key,omitempty"`
	Record         *Record               `json:"record,omitempty"`
	AppliedAt      time.Time             `json:"applied_at"`
}

type QueryEventContractsInput struct {
	Domain   string `json:"domain,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type EventContract struct {
	ID                  string                 `json:"id"`
	Domain              string                 `json:"domain"`
	Title               string                 `json:"title"`
	Description         string                 `json:"description,omitempty"`
	SourceHint          string                 `json:"source_hint,omitempty"`
	Endpoint            string                 `json:"endpoint"`
	ConnectorEndpoint   string                 `json:"connector_endpoint_template,omitempty"`
	Method              string                 `json:"method"`
	BusinessAction      string                 `json:"business_action_id"`
	EventType           string                 `json:"event_type"`
	Operation           string                 `json:"operation"`
	DatasetID           string                 `json:"dataset_id"`
	RequiredFields      []string               `json:"required_fields,omitempty"`
	InputFields         []DatasetTemplateField `json:"input_fields,omitempty"`
	SuggestedTags       []string               `json:"suggested_tags,omitempty"`
	DataTemplate        map[string]any         `json:"data_template,omitempty"`
	DryRunBodyTemplate  map[string]any         `json:"dry_run_body_template"`
	CommitBodyTemplate  map[string]any         `json:"commit_body_template"`
	IdempotencyTemplate string                 `json:"idempotency_template"`
	RecommendedFlow     []string               `json:"recommended_flow"`
}

type QueryReportsInput struct {
	Limit    int    `json:"limit,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type ReportDefinition struct {
	ID          string         `json:"id"`
	Domain      string         `json:"domain"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	DatasetID   string         `json:"dataset_id"`
	Aggregate   AggregateInput `json:"aggregate"`
}

type AggregateInput struct {
	Filter    map[string]any    `json:"filter,omitempty"`
	GroupBy   []string          `json:"group_by,omitempty"`
	Metrics   []AggregateMetric `json:"metrics,omitempty"`
	Sort      []SortSpec        `json:"sort,omitempty"`
	Limit     int               `json:"limit,omitempty"`
	ScanLimit int               `json:"scan_limit,omitempty"`
}

type AggregateMetric struct {
	Name  string `json:"name,omitempty"`
	As    string `json:"as,omitempty"`
	Op    string `json:"op"`
	Field string `json:"field,omitempty"`
}

type AggregateResult struct {
	DatasetID string            `json:"dataset_id"`
	GroupBy   []string          `json:"group_by,omitempty"`
	Metrics   []AggregateMetric `json:"metrics"`
	Rows      []map[string]any  `json:"rows"`
	Scanned   int               `json:"scanned"`
	Limit     int               `json:"limit"`
	ScanLimit int               `json:"scan_limit"`
	Truncated bool              `json:"truncated,omitempty"`
}

type ReportResult struct {
	Report         ReportDefinition `json:"report"`
	Result         AggregateResult  `json:"result"`
	PrimaryResult  string           `json:"primary_result,omitempty"`
	BusinessStatus string           `json:"business_status,omitempty"`
	ResultStatus   string           `json:"result_status,omitempty"`
	ResultPayload  map[string]any   `json:"result_payload,omitempty"`
	Outputs        []map[string]any `json:"outputs,omitempty"`
	Artifacts      []map[string]any `json:"artifacts,omitempty"`
}
