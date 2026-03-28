package browser

import (
	"fmt"
	"strings"
	"time"
)

// FlowReplayer replays a RecordedFlow through the BrowserTaskSupervisor,
// with LLM-assisted adaptation when the page has changed since recording.
type FlowReplayer struct {
	supervisor *BrowserTaskSupervisor
	ocr        OCRProvider
	// llmDecide sends context to the LLM and returns the decision/action text.
	// May be nil (replay without LLM adaptation).
	llmDecide func(context string) (string, error)
}

// NewFlowReplayer creates a replayer.
func NewFlowReplayer(
	supervisor *BrowserTaskSupervisor,
	ocr OCRProvider,
	llmDecide func(string) (string, error),
) *FlowReplayer {
	return &FlowReplayer{
		supervisor: supervisor,
		ocr:        ocr,
		llmDecide:  llmDecide,
	}
}

// Replay converts a RecordedFlow into a TaskSpec and executes it.
// overrides can replace placeholder values in step params (e.g. {"username": "admin"}).
func (r *FlowReplayer) Replay(flow *RecordedFlow, overrides map[string]string) (*TaskState, error) {
	if flow == nil || len(flow.Steps) == 0 {
		return nil, fmt.Errorf("empty flow")
	}

	spec := r.flowToTaskSpec(flow, overrides)
	return r.supervisor.Execute(spec)
}

func (r *FlowReplayer) flowToTaskSpec(flow *RecordedFlow, overrides map[string]string) TaskSpec {
	var steps []StepSpec

	for _, rs := range flow.Steps {
		step := r.recordedStepToStepSpec(rs, overrides)
		steps = append(steps, step)
	}

	return TaskSpec{
		Description:     fmt.Sprintf("replay: %s", flow.Name),
		Steps:           steps,
		SuccessCriteria: flow.SuccessCriteria,
		MaxRetries:      3,
		StepTimeout:     30 * time.Second,
	}
}

func (r *FlowReplayer) recordedStepToStepSpec(rs RecordedStep, overrides map[string]string) StepSpec {
	params := make(map[string]string)

	switch rs.Action {
	case "navigate":
		url := rs.URL
		if v, ok := overrides["url"]; ok {
			url = v
		}
		params["url"] = url

	case "click", "click_at":
		params["selector"] = rs.Selector
		// Store coords as fallback hint (RetryStrategy can use them)
		if rs.Coords[0] > 0 || rs.Coords[1] > 0 {
			params["fallback_x"] = fmt.Sprintf("%d", rs.Coords[0])
			params["fallback_y"] = fmt.Sprintf("%d", rs.Coords[1])
		}

	case "type":
		params["selector"] = rs.Selector
		text := rs.Text
		// Apply overrides for common field names
		if rs.Selector != "" {
			for k, v := range overrides {
				if containsFieldHint(rs.Selector, k) {
					text = v
					break
				}
			}
		}
		params["text"] = text

	case "scroll":
		params["delta_y"] = "500" // default

	case "wait":
		params["selector"] = rs.Selector
		params["timeout"] = "10"

	default:
		params["selector"] = rs.Selector
	}

	// Add step-level verification if we have a snapshot to compare against
	var verify *CriterionSpec
	if rs.Snapshot != nil && rs.Snapshot.URL != "" {
		verify = &CriterionSpec{
			Type:    "url_contains",
			Pattern: extractURLPath(rs.Snapshot.URL),
		}
	}

	return StepSpec{
		Action: rs.Action,
		Params: params,
		Verify: verify,
	}
}

// containsFieldHint checks if a CSS selector hints at a specific field name.
func containsFieldHint(selector, fieldName string) bool {
	if fieldName == "" || selector == "" {
		return false
	}
	return strings.Contains(selector, fieldName) ||
		strings.Contains(selector, "name="+fieldName) ||
		strings.Contains(selector, "id="+fieldName)
}

// extractURLPath extracts the path portion of a URL for loose matching.
func extractURLPath(url string) string {
	idx := 0
	slashCount := 0
	for i, c := range url {
		if c == '/' {
			slashCount++
			if slashCount == 3 {
				idx = i
				break
			}
		}
	}
	if idx > 0 && idx < len(url) {
		return url[idx:]
	}
	return url
}
