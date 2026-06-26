package structureddata

type BusinessErrorAction struct {
	Label  string         `json:"label"`
	Action string         `json:"action"`
	Args   map[string]any `json:"args,omitempty"`
}

type BusinessError struct {
	Code        string                `json:"code"`
	Message     string                `json:"message"`
	Actor       string                `json:"actor,omitempty"`
	Target      string                `json:"target,omitempty"`
	Required    string                `json:"required,omitempty"`
	Actual      string                `json:"actual,omitempty"`
	NextActions []BusinessErrorAction `json:"next_actions,omitempty"`
	Metadata    map[string]any        `json:"metadata,omitempty"`
}
