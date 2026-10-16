package skill

import (
	"strings"
	"sync"

	"github.com/RapidAI/CodeClaw/corelib/experience/lifecycle"
)

// repairEventSink is the process-wide lifecycle sink for skill repair
// evidence. Repair entry points (AttemptRepair/ApplyRepair/RepairGate.Verify)
// are free functions and shared components called from GUI, TUI, hub, and
// tests; threading a sink through every caller would be intrusive, so the
// sink follows the same package-level opt-in pattern as the evolution audit
// log. Hosts wire it next to UsageTracker.SetExperienceEventSink.
var repairEventSinkState struct {
	sync.RWMutex
	sink lifecycle.EventSink
}

// SetRepairEventSink connects skill self-repair and gate verification to the
// shared experience lifecycle trail (repair_attempted / repair_applied).
// Pass nil to disconnect. The repair path remains fully usable without a sink.
func SetRepairEventSink(sink lifecycle.EventSink) {
	repairEventSinkState.Lock()
	repairEventSinkState.sink = sink
	repairEventSinkState.Unlock()
}

// emitRepairEvent records one repair lifecycle event when a sink is wired.
// Repair events are evidence for the experience lifecycle (comparative
// distiller input), not user-facing notifications; recording is best-effort.
func emitRepairEvent(event lifecycle.Event) {
	repairEventSinkState.RLock()
	sink := repairEventSinkState.sink
	repairEventSinkState.RUnlock()
	if sink == nil || event.EventType == "" {
		return
	}
	sink.RecordExperienceEvent(event)
}

func repairEventReason(explanation string) string {
	explanation = strings.Join(strings.Fields(strings.TrimSpace(explanation)), " ")
	if len([]rune(explanation)) > 200 {
		explanation = string([]rune(explanation)[:197]) + "..."
	}
	return explanation
}
