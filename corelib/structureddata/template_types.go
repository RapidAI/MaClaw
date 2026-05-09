package structureddata

type DatasetTemplate struct {
	ID          string                   `json:"id"`
	Domain      string                   `json:"domain"`
	Name        string                   `json:"name"`
	Title       string                   `json:"title"`
	Description string                   `json:"description,omitempty"`
	Fields      []DatasetTemplateField   `json:"fields"`
	SampleData  []map[string]interface{} `json:"sample_data,omitempty"`
}

type QueryTemplatesInput struct {
	Limit    int    `json:"limit,omitempty"`
	BeforeID string `json:"before_id,omitempty"`
}

type DatasetTemplateField struct {
	Key       string         `json:"key"`
	Type      string         `json:"type"`
	Title     string         `json:"title,omitempty"`
	Required  bool           `json:"required,omitempty"`
	Indexed   bool           `json:"indexed,omitempty"`
	Sensitive bool           `json:"sensitive,omitempty"`
	Config    map[string]any `json:"config,omitempty"`
}

type CreateFromTemplateInput struct {
	ID          string `json:"id,omitempty"`
	Domain      string `json:"domain,omitempty"`
	Name        string `json:"name,omitempty"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
}

type CreateFromTemplateResult struct {
	Dataset *Dataset          `json:"dataset"`
	Fields  []FieldDefinition `json:"fields"`
}

type BootstrapTemplatesInput struct {
	TemplateIDs  []string `json:"template_ids,omitempty"`
	Domains      []string `json:"domains,omitempty"`
	SkipExisting bool     `json:"skip_existing,omitempty"`
	DryRun       bool     `json:"dry_run,omitempty"`
}

type BootstrapTemplatesResult struct {
	Created     []CreateFromTemplateResult `json:"created"`
	WouldCreate []DatasetTemplate          `json:"would_create,omitempty"`
	Skipped     []string                   `json:"skipped,omitempty"`
	Errors      map[string]string          `json:"errors,omitempty"`
}
