//go:build windows

package guiapp

import "testing"

// Regression test for the AI-assistant GUI crash: the badge renderer used to
// resolve SetBkMode/SetTextColor from user32.dll, where those GDI functions do
// not exist, so the first badge push panicked syscall.LazyProc and killed the
// whole process. Rendered icons must come back non-zero on every call now,
// and repeated renders must not lose the procs' lookup cache.
func TestTaskbarBadgeRenderIconCreatesIcon(t *testing.T) {
	for _, count := range []int{1, 2, 100} {
		icon := taskbarBadgeRenderIcon(count)
		if icon == 0 {
			t.Fatalf("taskbarBadgeRenderIcon(%d) returned 0", count)
		}
		procBadgeDestroyIcon.Call(icon)
	}
	icon := taskbarBadgeRenderIcon(0)
	if icon != 0 {
		procBadgeDestroyIcon.Call(icon)
		t.Fatalf("taskbarBadgeRenderIcon(0) should return 0 (no badge)")
	}
}
