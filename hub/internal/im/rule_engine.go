package im

import "strings"

// RouteAction is the outcome of a rule engine evaluation.
type RouteAction string

const (
	ActionRouteToTarget      RouteAction = "route_to_target"
	ActionBroadcast          RouteAction = "broadcast"
	ActionNeedClassification RouteAction = "need_classification"
	ActionPassthrough        RouteAction = "passthrough"
)

// RouteDecision is the result returned by RuleEngine.Evaluate.
type RouteDecision struct {
	Action   RouteAction
	TargetID string // valid when Action == ActionRouteToTarget
	Reason   string
}

// RuleEngine makes deterministic routing decisions using pure in-memory
// logic — zero I/O, zero latency. It is the first stage of the Coordinator
// pipeline; only when no rule matches does the message go to the LLM
// IntentClassifier.
type RuleEngine struct{}

// Evaluate returns a routing decision for the given message context.
//
// Rule priority (first match wins):
//  1. @name prefix → route to that specific device
//  2. Single device + smart_route_single_device=false → route to it
//  3. User has a selected device (not broadcast) → route to it
//  4. User is in broadcast mode → broadcast
//  5. None matched + LLM enabled → need_classification
//  6. None matched + no LLM → passthrough (prompt user to /call)
func (e *RuleEngine) Evaluate(
	text string,
	machines []OnlineMachineInfo,
	selectedMachine string,
	llmEnabled bool,
	smartRouteSingle bool,
) RouteDecision {
	// Rule 1: @name prefix — find the targeted device.
	if strings.HasPrefix(text, "@") {
		if idx := strings.IndexByte(text, ' '); idx > 1 {
			name := text[1:idx]
			for _, m := range machines {
				if strings.EqualFold(m.Name, name) {
					return RouteDecision{
						Action:   ActionRouteToTarget,
						TargetID: m.MachineID,
						Reason:   "@ 指定设备",
					}
				}
			}
		}
	}

	// Rule 2: Single device online + switch off → direct forward.
	if len(machines) == 1 && !smartRouteSingle {
		return RouteDecision{
			Action:   ActionRouteToTarget,
			TargetID: machines[0].MachineID,
			Reason:   "单设备直接转发",
		}
	}

	// Rule 3: User has explicitly selected a device (not broadcast).
	if selectedMachine != "" && selectedMachine != broadcastMachineID {
		// Verify the selected machine is still in the online list.
		for _, m := range machines {
			if m.MachineID == selectedMachine {
				return RouteDecision{
					Action:   ActionRouteToTarget,
					TargetID: selectedMachine,
					Reason:   "已选定设备",
				}
			}
		}
		// Selected machine went offline — fall through to classification.
	}

	// Rule 4: Broadcast mode.
	if selectedMachine == broadcastMachineID {
		return RouteDecision{
			Action: ActionBroadcast,
			Reason: "广播模式",
		}
	}

	// Rule 5/6: No rule matched.
	if llmEnabled {
		return RouteDecision{
			Action: ActionNeedClassification,
			Reason: "需要 LLM 意图分类",
		}
	}
	return RouteDecision{
		Action: ActionPassthrough,
		Reason: "无 LLM，降级为现有逻辑",
	}
}
