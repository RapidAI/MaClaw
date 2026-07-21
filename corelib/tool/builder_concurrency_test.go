package tool

import (
	"fmt"
	"sync"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
)

// TestDynamicToolBuilderSerializesConfigurationAndBuild exercises the startup
// path where embedding activation swaps routing configuration while IM requests
// are already building tool definitions. Run with -race to catch regressions.
func TestDynamicToolBuilderSerializesConfigurationAndBuild(t *testing.T) {
	makeRegistry := func(prefix string) *Registry {
		reg := NewRegistry()
		for i := 0; i < 24; i++ {
			if err := reg.Register(RegisteredTool{
				Name:        fmt.Sprintf("%s_%d", prefix, i),
				Description: "concurrent builder routing test",
				Category:    CategoryNonCode,
			}); err != nil {
				t.Fatal(err)
			}
		}
		return reg
	}

	registries := []*Registry{makeRegistry("first"), makeRegistry("second")}
	builder := NewDynamicToolBuilder(registries[0])
	var wg sync.WaitGroup

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for n := 0; n < 40; n++ {
				if defs := builder.Build("route the current IM message"); len(defs) == 0 {
					t.Error("Build returned no definitions")
					return
				}
				_ = builder.BuildAll()
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for n := 0; n < 80; n++ {
			builder.SetRegistry(registries[n%len(registries)])
			builder.SetEmbedder(embedding.NoopEmbedder{})
			builder.SetEnrichmentStore(nil)
			builder.SetUsageTracker(nil)
			builder.SetReranker(nil)
		}
	}()
	wg.Wait()
}
