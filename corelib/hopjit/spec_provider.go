package hopjit

import (
	"context"
	"fmt"
)

type SpecProvider interface {
	LoadSpec(context.Context, string) (string, error)
}

type SpecProviderFunc func(context.Context, string) (string, error)

func (f SpecProviderFunc) LoadSpec(ctx context.Context, id string) (string, error) {
	return f(ctx, id)
}

type MapSpecProvider map[string]string

func (p MapSpecProvider) LoadSpec(_ context.Context, id string) (string, error) {
	if spec, ok := p[id]; ok {
		return spec, nil
	}
	return "", fmt.Errorf("SPEC_NOT_FOUND: %s", id)
}

func (e *ExecutionEngine) loadCallSpecLocked(ctx context.Context, id string) (string, error) {
	if markdown, ok := e.snapshot.ResolvedSpecs[id]; ok {
		return markdown, nil
	}
	if e.specs == nil {
		return "", fmt.Errorf("SPEC_PROVIDER_UNAVAILABLE")
	}
	markdown, err := e.specs.LoadSpec(ctx, id)
	if err != nil {
		return "", err
	}
	e.snapshot.ResolvedSpecs[id] = markdown
	return markdown, nil
}

func WithSpecProvider(provider SpecProvider) EngineOption {
	return func(engine *ExecutionEngine) {
		engine.specs = provider
	}
}
