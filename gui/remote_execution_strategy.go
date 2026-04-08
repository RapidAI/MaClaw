package main

// ExecutionHandle represents a running remote execution instance.
type ExecutionHandle interface {
	PID() int
	Write(data []byte) error
	Interrupt() error
	Kill() error
	Output() <-chan []byte
	Exit() <-chan PTYExit
	Close() error
}

// AskUserQuestionResponder is implemented by structured execution handles
// that can answer a pending AskUserQuestion tool_use via protocol-native
// continuation messages instead of raw stdin text.
type AskUserQuestionResponder interface {
	WriteAskUserQuestionAnswer(pending *PendingToolUse, text string) error
}

// ExecutionStrategy describes how a remote command is started and hosted.
type ExecutionStrategy interface {
	Start(cmd CommandSpec) (ExecutionHandle, error)
}
