package tool

import (
	"reflect"
	"sync"
	"testing"
)

func TestAugmentCommandEnvWithoutHost(t *testing.T) {
	env := []string{"Path=C:\\Windows", "OS=Windows_NT"}
	if got := AugmentCommandEnv(env); !reflect.DeepEqual(got, env) {
		t.Fatalf("unset augmenter changed the env: %v", got)
	}
}

func TestSetCommandEnvAugmenter(t *testing.T) {
	commandEnvAugmenterMu.Lock()
	prev := commandEnvAugmenter
	commandEnvAugmenterMu.Unlock()
	t.Cleanup(func() {
		commandEnvAugmenterMu.Lock()
		commandEnvAugmenter = prev
		commandEnvAugmenterMu.Unlock()
	})

	SetCommandEnvAugmenter(func(env []string) []string {
		return append([]string{"Path=C:\\Tools"}, env...)
	})
	got := AugmentCommandEnv([]string{"Path=C:\\Windows"})
	if len(got) != 2 || got[0] != "Path=C:\\Tools" {
		t.Fatalf("augmenter not applied: %v", got)
	}
}

// Concurrent AugmentCommandEnv callers must never race the installer.
func TestAugmentCommandEnvConcurrent(t *testing.T) {
	commandEnvAugmenterMu.Lock()
	prev := commandEnvAugmenter
	commandEnvAugmenterMu.Unlock()
	t.Cleanup(func() {
		commandEnvAugmenterMu.Lock()
		commandEnvAugmenter = prev
		commandEnvAugmenterMu.Unlock()
	})

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 50; j++ {
				SetCommandEnvAugmenter(func(env []string) []string { return env })
				_ = AugmentCommandEnv([]string{"A=1"})
			}
		}()
	}
	wg.Wait()
}
