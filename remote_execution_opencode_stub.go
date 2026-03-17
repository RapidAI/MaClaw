package main

import "fmt"

// OpenCodeSDKExecutionStrategy is a stub for OpenCode HTTP+SSE mode.
// Full implementation is in build/deploy/stage/remote_execution_opencode.go.
type OpenCodeSDKExecutionStrategy struct{}

func NewOpenCodeSDKExecutionStrategy() *OpenCodeSDKExecutionStrategy {
	return &OpenCodeSDKExecutionStrategy{}
}

func (s *OpenCodeSDKExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	return nil, fmt.Errorf("OpenCode SDK mode not yet implemented")
}
