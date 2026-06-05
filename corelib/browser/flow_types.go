package browser

import "time"

// RecordedFlow is a recorded browser operation sequence.
type RecordedFlow struct {
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	RecordedAt      time.Time       `json:"recorded_at"`
	StartURL        string          `json:"start_url"`
	Steps           []RecordedStep  `json:"steps"`
	SuccessCriteria []CriterionSpec `json:"success_criteria,omitempty"`
}

// RecordedStep is a single recorded browser operation.
type RecordedStep struct {
	Action        string            `json:"action"`                   // navigate, click, type, scroll
	Selector      string            `json:"selector,omitempty"`       // inferred CSS selector
	Coords        [2]int            `json:"coords,omitempty"`         // original click coordinates [x, y]
	Text          string            `json:"text,omitempty"`           // input text (type action)
	TextLen       int               `json:"text_len,omitempty"`       // length of redacted input text
	ContentFormat string            `json:"content_format,omitempty"` // plain or markdown for type action
	Redacted      bool              `json:"redacted,omitempty"`       // true when sensitive fields were not persisted
	URL           string            `json:"url,omitempty"`            // navigation URL
	Timestamp     time.Duration     `json:"timestamp"`                // offset from recording start
	Snapshot      *RecordedSnapshot `json:"snapshot,omitempty"`       // page state after action
}

// RecordedSnapshot captures page state at recording time.
type RecordedSnapshot struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}
