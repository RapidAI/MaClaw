package structureddata

import "time"

type QueryAccessPolicyPresetsInput struct {
	Limit    int    `json:"limit,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type QueryDashboardsInput struct {
	Limit    int    `json:"limit,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type QueryQualityChecksInput struct {
	Limit    int    `json:"limit,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type SchemaProposalInput struct {
	SampleData map[string]any `json:"sample_data"`
	Reason     string         `json:"reason,omitempty"`
}

type ListSchemaProposalsInput struct {
	Status   string `json:"status,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Before   string `json:"before,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type SchemaProposal struct {
	ID             string                 `json:"id,omitempty"`
	TenantID       string                 `json:"tenant_id,omitempty"`
	DatasetID      string                 `json:"dataset_id"`
	Reason         string                 `json:"reason,omitempty"`
	ExistingFields []FieldDefinition      `json:"existing_fields"`
	Suggested      []FieldDefinition      `json:"suggested"`
	Ignored        []string               `json:"ignored,omitempty"`
	Impact         map[string]interface{} `json:"impact"`
	Status         string                 `json:"status,omitempty"`
	CreatedBy      string                 `json:"created_by,omitempty"`
	AppliedBy      string                 `json:"applied_by,omitempty"`
	AppliedAt      *time.Time             `json:"applied_at,omitempty"`
	CreatedAt      time.Time              `json:"created_at,omitempty"`
	UpdatedAt      time.Time              `json:"updated_at,omitempty"`
}

type ApplySchemaProposalInput struct {
	ProposalID string            `json:"proposal_id,omitempty"`
	Fields     []FieldDefinition `json:"fields"`
	Confirm    bool              `json:"confirm"`
	Reason     string            `json:"reason,omitempty"`
}

type ApplySchemaProposalResult struct {
	DatasetID string            `json:"dataset_id"`
	Applied   []FieldDefinition `json:"applied"`
	Reason    string            `json:"reason,omitempty"`
}
