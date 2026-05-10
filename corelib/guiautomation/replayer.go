package guiautomation

import (
	"fmt"
	"strings"
	"time"
)

// GUIReplayer replays a GUIRecordedFlow through the GUITaskSupervisor.
type GUIReplayer struct {
	supervisor *GUITaskSupervisor
	locator    *ElementLocator
	input      InputSimulator
}

// NewGUIReplayer creates a replayer.
func NewGUIReplayer(
	supervisor *GUITaskSupervisor,
	locator *ElementLocator,
	input InputSimulator,
) *GUIReplayer {
	return &GUIReplayer{
		supervisor: supervisor,
		locator:    locator,
		input:      input,
	}
}

// Replay converts a GUIRecordedFlow into a GUITaskSpec and executes it.
// overrides can replace text field values in steps (e.g. {"username": "admin"}).
func (r *GUIReplayer) Replay(flow *GUIRecordedFlow, overrides map[string]string) (*GUITaskState, error) {
	if flow == nil || len(flow.Steps) == 0 {
		return nil, fmt.Errorf("empty flow")
	}

	spec := r.flowToTaskSpec(flow, overrides)
	return r.supervisor.Execute(spec)
}

// CancelAll cancels all running tasks in the underlying supervisor.
// Safe to call from external packages (e.g. gui) that cannot access
// the unexported supervisor field directly.
func (r *GUIReplayer) CancelAll() {
	r.supervisor.CancelAll()
}

// flowToTaskSpec converts a recorded flow into an executable task spec.
func (r *GUIReplayer) flowToTaskSpec(flow *GUIRecordedFlow, overrides map[string]string) GUITaskSpec {
	var steps []GUIStepSpec
	for i := range flow.Steps {
		steps = append(steps, r.stepToSpec(&flow.Steps[i], overrides))
	}

	return GUITaskSpec{
		Description:     fmt.Sprintf("replay: %s", flow.Name),
		Steps:           steps,
		SuccessCriteria: flow.SuccessCriteria,
		MaxRetries:      3,
		StepTimeout:     30 * time.Second,
	}
}

// stepToSpec converts a single GUIRecordedStep to a GUIStepSpec, applying overrides.
func (r *GUIReplayer) stepToSpec(rs *GUIRecordedStep, overrides map[string]string) GUIStepSpec {
	params := make(map[string]string)

	switch rs.Action {
	case "click", "right_click", "double_click":
		params["x"] = fmt.Sprintf("%d", rs.Coords[0])
		params["y"] = fmt.Sprintf("%d", rs.Coords[1])

	case "type":
		text := rs.Text
		// Apply overrides:
		// 1. Exact match: if text equals an override key, replace entirely
		// 2. Placeholder: replace {{key}} patterns in text with override values
		for k, v := range overrides {
			if text == k {
				text = v
				break
			}
			placeholder := "{{" + k + "}}"
			if strings.Contains(text, placeholder) {
				text = strings.ReplaceAll(text, placeholder, v)
			}
		}
		params["text"] = text
		params["x"] = fmt.Sprintf("%d", rs.Coords[0])
		params["y"] = fmt.Sprintf("%d", rs.Coords[1])

	case "keypress":
		keys := ""
		for i, k := range rs.Keys {
			if i > 0 {
				keys += ","
			}
			keys += k
		}
		params["keys"] = keys
		params["x"] = fmt.Sprintf("%d", rs.Coords[0])
		params["y"] = fmt.Sprintf("%d", rs.Coords[1])

	case "scroll":
		params["scroll_dy"] = fmt.Sprintf("%d", rs.ScrollDY)
		params["x"] = fmt.Sprintf("%d", rs.Coords[0])
		params["y"] = fmt.Sprintf("%d", rs.Coords[1])

	case "drag":
		params["x"] = fmt.Sprintf("%d", rs.Coords[0])
		params["y"] = fmt.Sprintf("%d", rs.Coords[1])
		params["drag_to_x"] = fmt.Sprintf("%d", rs.DragTo[0])
		params["drag_to_y"] = fmt.Sprintf("%d", rs.DragTo[1])
	}

	return GUIStepSpec{
		Action:   rs.Action,
		Params:   params,
		OrigStep: rs,
	}
}
