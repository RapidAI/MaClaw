package tool

import (
	"fmt"
	"testing"
)

func benchmarkRegistry(b *testing.B, n int) *Registry {
	b.Helper()
	reg := NewRegistry()
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("tool_%d", i)
		err := reg.Register(RegisteredTool{
			Name:        name,
			Description: "reads config, talks to servers and writes output for " + name,
			Tags:        []string{"file", "server", "config"},
			Status:      StatusAvailable,
			Priority:    i % 5,
		})
		if err != nil {
			b.Fatal(err)
		}
	}
	return reg
}

// BenchmarkDynamicToolBuilderBuild measures one routing decision's tool
// selection with the usage tracker attached (the production default).
func BenchmarkDynamicToolBuilderBuild(b *testing.B) {
	reg := benchmarkRegistry(b, 100)
	bld := NewDynamicToolBuilder(reg)
	tr, err := NewUsageTracker("")
	if err != nil {
		b.Fatal(err)
	}
	for i := 0; i < 400; i++ {
		tr.RecordOutcome(fmt.Sprintf("tool_%d", i%100), []string{"read", "config"}, i%3 != 0, "")
	}
	bld.SetUsageTracker(tr)
	msg := "read the config file and show me the database settings"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bld.Build(msg)
	}
}

// BenchmarkDynamicToolBuilderBuildNoTracker covers the path without usage
// history, where the query does not need tokenising at all.
func BenchmarkDynamicToolBuilderBuildNoTracker(b *testing.B) {
	bld := NewDynamicToolBuilder(benchmarkRegistry(b, 100))
	msg := "read the config file and show me the database settings"
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = bld.Build(msg)
	}
}
