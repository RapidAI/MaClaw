package modelmanager

import (
	"sync/atomic"
	"testing"
	"time"
)

func TestReleaseMemoryRunsReleasers(t *testing.T) {
	var n atomic.Int32
	RegisterReleaser(func() { n.Add(1) })
	t.Setenv("MACLAW_MODEL_FREE_OS_MEMORY", "off")
	ReleaseMemory()
	if n.Load() < 1 {
		t.Fatalf("releaser ran %d times", n.Load())
	}
}

func TestUnloadTriggersRelease(t *testing.T) {
	orig := releaseAfterUnload
	defer func() { releaseAfterUnload = orig }()
	calls := 0
	releaseAfterUnload = func() { calls++ }
	m := New(Config[int]{Name: "t", Load: func() (int, error) { return 1, nil }})
	_, done, _ := m.Acquire()
	done()
	m.Unload()
	if calls != 1 || m.Loaded() {
		t.Fatalf("calls=%d loaded=%v", calls, m.Loaded())
	}
}

func TestIdleUnloadDelayEnv(t *testing.T) {
	t.Setenv("MACLAW_MODEL_IDLE_UNLOAD_SEC", "")
	if IdleUnloadDelay(5*time.Minute) != 5*time.Minute {
		t.Fatal("default")
	}
	t.Setenv("MACLAW_MODEL_IDLE_UNLOAD_SEC", "60")
	if IdleUnloadDelay(5*time.Minute) != time.Minute {
		t.Fatal("override")
	}
	if New(Config[int]{UnloadDelay: time.Hour}).unloadDelay != time.Minute {
		t.Fatal("New must apply env")
	}
}
