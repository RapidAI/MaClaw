package desktopbot_test

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/desktopbot"
)

func TestPermitIgnoresCallIdentityAndBlocksRestart(t *testing.T) {
	path := effectDBPath(t)
	db := openEffectDB(t, path)
	permit := testPermit("run-1")
	var calls atomic.Int32
	release := make(chan struct{})
	started := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _, _ = desktopbot.RunAuthorized(db, permit, "git push origin main", func() (string, error) {
			calls.Add(1)
			close(started)
			<-release
			return "已推送", nil
		})
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the claim")
	}
	restarted := openEffectDB(t, path)
	var again atomic.Int32
	_, _, err := desktopbot.RunAuthorized(restarted, permit, "git push origin main", func() (string, error) {
		again.Add(1)
		return "第二次", nil
	})
	if again.Load() != 0 || !errors.As(err, &desktopbot.ErrEffectUncertain{}) {
		t.Fatalf("restart calls=%d err=%v", again.Load(), err)
	}
	close(release)
	<-done
	if calls.Load() != 1 {
		t.Fatalf("calls=%d", calls.Load())
	}
	text, replayed, err := desktopbot.RunAuthorized(restarted, permit, "git push origin main", func() (string, error) {
		again.Add(1)
		return "第三次", nil
	})
	if err != nil || !replayed || text != "已推送" || again.Load() != 0 {
		t.Fatalf("replay text=%q replayed=%v again=%d err=%v", text, replayed, again.Load(), err)
	}
}

func TestPermitParameterChangeIsStaleAndFreshIsANewOperation(t *testing.T) {
	db := openEffectDB(t, effectDBPath(t))
	permit := testPermit("run-1")
	if _, _, err := desktopbot.RunAuthorized(db, permit, "git push origin main", func() (string, error) {
		return "已推送", nil
	}); err != nil {
		t.Fatal(err)
	}
	changed := permit
	changed.Args = `{"command":"git push origin other","to":"b@example.com"}`
	changed.Target = "b@example.com"
	var calls atomic.Int32
	_, _, err := desktopbot.RunAuthorized(db, changed, "git push origin other", func() (string, error) {
		calls.Add(1)
		return "另一条", nil
	})
	if calls.Load() != 0 || !errors.As(err, &desktopbot.ErrConsentStale{}) {
		t.Fatalf("changed calls=%d err=%v", calls.Load(), err)
	}
	fresh := permit
	fresh.Fresh = true
	text, replayed, err := desktopbot.RunAuthorized(db, fresh, "git push origin main", func() (string, error) {
		calls.Add(1)
		return "用户要求再做一次", nil
	})
	if err != nil || replayed || calls.Load() != 1 || text != "用户要求再做一次" {
		t.Fatalf("fresh text=%q replayed=%v calls=%d err=%v", text, replayed, calls.Load(), err)
	}
}

func TestPermitUnknownBlocksANewRunUntilTheUserAuthorizes(t *testing.T) {
	db := openEffectDB(t, effectDBPath(t))
	permit := testPermit("run-1")
	if _, _, err := desktopbot.RunAuthorized(db, permit, "git push origin main", func() (string, error) {
		return "", uncertainTestError{}
	}); err == nil {
		t.Fatal("uncertain effect returned success")
	}
	next := testPermit("run-2")
	var calls atomic.Int32
	_, _, err := desktopbot.RunAuthorized(db, next, "git push origin main", func() (string, error) {
		calls.Add(1)
		return "新一轮", nil
	})
	if calls.Load() != 0 || !errors.As(err, &desktopbot.ErrEffectUncertain{}) {
		t.Fatalf("new run calls=%d err=%v", calls.Load(), err)
	}
	next.Fresh = true
	text, _, err := desktopbot.RunAuthorized(db, next, "git push origin main", func() (string, error) {
		calls.Add(1)
		return "用户明确再授权", nil
	})
	if err != nil || calls.Load() != 1 || text != "用户明确再授权" {
		t.Fatalf("authorized text=%q calls=%d err=%v", text, calls.Load(), err)
	}
}

func TestPermitMissingIdentityDoesNotRun(t *testing.T) {
	db := openEffectDB(t, effectDBPath(t))
	permit := testPermit("run-1")
	permit.Owner = ""
	var calls atomic.Int32
	_, _, err := desktopbot.RunAuthorized(db, permit, "git push origin main", func() (string, error) {
		calls.Add(1)
		return "不应执行", nil
	})
	if calls.Load() != 0 || !errors.As(err, &desktopbot.ErrEffectIdentity{}) {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}
	if _, err := desktopbot.CanonicalEffectArgs("", map[string]any{"action": "install"}); err == nil {
		t.Fatal("action alone was accepted as effect parameters")
	}
	args, err := desktopbot.CanonicalEffectArgs("git push origin main", map[string]any{
		"to": "a@example.com", "url": "https://example.test", "text": "正文", "password": "secret-value",
	})
	if err != nil || !containsAll(args, "a@example.com", "https://example.test", "正文") || containsAll(args, "secret-value") {
		t.Fatalf("args=%s err=%v", args, err)
	}
}

func TestPermitOneOwner(t *testing.T) {
	db := openEffectDB(t, effectDBPath(t))
	permit := testPermit("run-1")
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	var finished atomic.Int32
	var wg sync.WaitGroup
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func() {
			defer wg.Done()
			_, _, _ = desktopbot.RunAuthorized(db, permit, "git push origin main", func() (string, error) {
				if calls.Add(1) == 1 {
					close(started)
				}
				<-release
				return "已推送", nil
			})
			finished.Add(1)
		}()
	}
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("neither owner started")
	}
	deadline := time.Now().Add(5 * time.Second)
	for finished.Load() < 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if calls.Load() != 1 || finished.Load() != 1 {
		t.Fatalf("owners=%d finished=%d", calls.Load(), finished.Load())
	}
	close(release)
	wg.Wait()
}

func testPermit(runID string) desktopbot.EffectPermit {
	return desktopbot.EffectPermit{
		RunID:   runID,
		Owner:   "tenant/alice",
		Account: "bot",
		Host:    "git.example",
		Target:  "a@example.com",
		Tool:    "bash",
		Args:    `{"command":"git push origin main","to":"a@example.com"}`,
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if !containsPart(text, part) {
			return false
		}
	}
	return true
}

func containsPart(text, part string) bool {
	return len(part) > 0 && len(text) >= len(part) && (text == part || indexPart(text, part) >= 0)
}

func indexPart(text, part string) int {
	for i := 0; i+len(part) <= len(text); i++ {
		if text[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}
