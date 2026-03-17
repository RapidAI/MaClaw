package main

import "fmt"

// KiloSDKExecutionStrategy is a stub for Kilo HTTP+SSE mode.
// Full implementation is in build/deploy/stage/remote_execution_kilo.go.
type KiloSDKExecutionStrategy struct{}

func NewKiloSDKExecutionStrategy() *KiloSDKExecutionStrategy {
	return &KiloSDKExecutionStrategy{}
}

func (s *KiloSDKExecutionStrategy) Start(cmd CommandSpec) (ExecutionHandle, error) {
	return nil, fmt.Errorf("Kilo SDK mode not yet implemented")
}
