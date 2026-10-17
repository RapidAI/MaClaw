package app

import (
	"runtime/debug"
	"testing"
)

func TestApplySoftMemoryLimitUsesMiB(t *testing.T) {
	prev := applySoftMemoryLimit(256)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	got := debug.SetMemoryLimit(256 << 20)
	if got != 256<<20 {
		t.Fatalf("limit = %d, want %d", got, 256<<20)
	}
}

func TestApplySoftMemoryLimitDefault(t *testing.T) {
	prev := applySoftMemoryLimit(0)
	t.Cleanup(func() { debug.SetMemoryLimit(prev) })
	got := debug.SetMemoryLimit(int64(defaultSoftMemoryLimitMB) << 20)
	if got != int64(defaultSoftMemoryLimitMB)<<20 {
		t.Fatalf("limit = %d, want default %d MiB", got, defaultSoftMemoryLimitMB)
	}
}
