package main

import (
	"github.com/RapidAI/CodeClaw/corelib/codingruntime"
)

// admittedChildExecutionRegistry keeps only live, in-process cancellation
// handles for read-only children that were durably admitted by a GUI Runtime
// parent. The Ledger remains the source of truth: this registry exists solely
// to interrupt an already-blocking model/tool/SSH wait promptly after the
// parent task has been cancelled.
//
// A child must not inherit the parent LoopContext directly. Admission ends the
// parent Attempt normally (waiting_child), and cancelling that normal handoff
// context would make every detached child fail immediately. Instead each child
// receives its own context and the parent's explicit cancellation hook closes
// it together with the durable task subtree.
var guiAdmittedChildExecutions codingruntime.ChildExecutionRegistry

func cancelGUIAdmittedChildExecutions(parentTaskID string) {
	guiAdmittedChildExecutions.CancelParent(parentTaskID)
}
