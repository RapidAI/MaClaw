package hopjit

import (
	"context"
	"errors"
)

type ToolProvider interface {
	Execute(context.Context, string, map[string]any) (any, error)
}

type ToolProviderFunc func(context.Context, string, map[string]any) (any, error)

func (f ToolProviderFunc) Execute(ctx context.Context, name string, args map[string]any) (any, error) {
	return f(ctx, name, args)
}

type EngineOption func(*ExecutionEngine)

func WithToolProvider(provider ToolProvider) EngineOption {
	return func(engine *ExecutionEngine) {
		engine.tools = provider
	}
}

func WithRunOwner(ownerID string) EngineOption {
	return func(engine *ExecutionEngine) {
		engine.runOwner = ownerID
	}
}

const maxCallDepth = 32

func (e *ExecutionEngine) newChildEngine() *ExecutionEngine {
	child := NewExecutionEngine(e.store, WithToolProvider(e.tools), WithSpecProvider(e.specs), WithRunOwner(e.runOwner))
	child.callDepth = e.callDepth
	return child
}

func (e *ExecutionEngine) newCallEngine() (*ExecutionEngine, error) {
	if e.callDepth >= maxCallDepth {
		return nil, errors.New("CALL_DEPTH_LIMIT_EXCEEDED")
	}
	child := e.newChildEngine()
	child.callDepth++
	return child, nil
}
