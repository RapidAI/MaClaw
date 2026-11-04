package hopjit

import "context"

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
