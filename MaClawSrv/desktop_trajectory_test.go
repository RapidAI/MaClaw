package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
)

func testPNG(t *testing.T, w, h int) string {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h))); err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(buf.Bytes())
}

func TestDesktopMaxBatchStepsEnv(t *testing.T) {
	for raw, want := range map[string]int{"": 16, "8": 8, "999": 64, "0": 16, "x": 16} {
		t.Setenv(desktopMaxStepsEnv, raw)
		if got := desktopMaxBatchSteps(); got != want {
			t.Fatalf("%q: got %d want %d", raw, got, want)
		}
	}
}

func TestDesktopTrajectoryDir(t *testing.T) {
	scope := agentruntime.Scope{UserID: "alice", RunID: "run/1 x"}
	if got := desktopTrajectoryDir(scope, ""); got != "~/素材/default/runs/run_1_x" {
		t.Fatalf("got %q", got)
	}
	if got := desktopTrajectoryDir(scope, "写作助手"); got != "~/素材/写作助手/runs/run_1_x" {
		t.Fatalf("materials folder not used: %q", got)
	}
	if got := desktopTrajectoryDir(scope, "../etc"); got != "~/素材/default/runs/run_1_x" {
		t.Fatalf("unsafe folder must be ignored: %q", got)
	}
	if got := desktopTrajectoryDir(agentruntime.Scope{UserID: "alice"}, ""); got != "" {
		t.Fatalf("no run id must not record, got %q", got)
	}
}

func TestDesktopRunBudgetStopsActions(t *testing.T) {
	t.Setenv(desktopTrajectoryEnv, "off")
	t.Setenv(desktopRunMaxActionsEnv, "2")
	scope := agentruntime.Scope{TenantID: "t", UserID: "budget-user", RunID: "budget-run-" + t.Name()}
	args := map[string]any{"action": "screenshot"}
	for i := 0; i < 2; i++ {
		if err := desktopBudgetCheck(scope, "screenshot"); err != nil {
			t.Fatalf("step %d refused: %v", i, err)
		}
		desktopRecordStep(context.Background(), scope, args, "ok", nil, time.Millisecond)
	}
	err := desktopBudgetCheck(scope, "screenshot")
	if err == nil || !strings.Contains(err.Error(), "budget spent") {
		t.Fatalf("third action must be refused, got %v", err)
	}
	if err := desktopBudgetCheck(scope, "request_takeover"); err != nil {
		t.Fatalf("takeover must stay possible: %v", err)
	}
}

func TestDesktopRunBudgetCountsImageTokens(t *testing.T) {
	t.Setenv(desktopTrajectoryEnv, "off")
	t.Setenv(desktopRunMaxImageTokEnv, "1000")
	scope := agentruntime.Scope{TenantID: "t", UserID: "img-user", RunID: "img-run-" + t.Name()}
	out := agentruntime.AttachModelImage("shot", "image/png", testPNG(t, 1000, 750))
	desktopRecordStep(context.Background(), scope, map[string]any{"action": "screenshot"}, out, nil, time.Millisecond)
	if err := desktopBudgetCheck(scope, "screenshot"); err == nil {
		t.Fatal("image token budget must stop the run")
	}
}

func TestDesktopTrajectoryWritesStepAndImage(t *testing.T) {
	t.Setenv(desktopTrajectoryEnv, "")
	var mu sync.Mutex
	files := map[string]string{}
	var cmds []string
	done := make(chan struct{}, 4)
	oldFile, oldBash := desktopTrajectoryFile, desktopTrajectoryBash
	desktopTrajectoryFile = func(_ context.Context, _ agentruntime.Scope, p, c string) error {
		mu.Lock()
		files[p] = c
		mu.Unlock()
		return nil
	}
	desktopTrajectoryBash = func(_ context.Context, _ agentruntime.Scope, c string) error {
		mu.Lock()
		cmds = append(cmds, c)
		mu.Unlock()
		done <- struct{}{}
		return nil
	}
	defer func() { desktopTrajectoryFile, desktopTrajectoryBash = oldFile, oldBash }()

	scope := agentruntime.Scope{TenantID: "t", UserID: "alice", RunID: "write-run"}
	img := testPNG(t, 40, 30)
	out := agentruntime.AttachModelImage("clicked", "image/png", img)
	desktopRecordStep(context.Background(), scope, map[string]any{"action": "type", "text": "secret-password"}, out, nil, 5*time.Millisecond)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not run")
	}
	mu.Lock()
	defer mu.Unlock()
	meta, ok := files["~/素材/default/runs/write-run/step-001.json"]
	if !ok {
		t.Fatalf("step json missing: %v", files)
	}
	if strings.Contains(meta, "secret-password") {
		t.Fatal("typed text must not be stored")
	}
	var step desktopTrajectoryStep
	if err := json.Unmarshal([]byte(meta), &step); err != nil {
		t.Fatal(err)
	}
	if step.Action != "type" || step.Image != "step-001.png" || !step.OK {
		t.Fatalf("step=%+v", step)
	}
	part := files["~/素材/default/runs/write-run/.step-001.b64.000"]
	if part != img {
		t.Fatal("image base64 part not written")
	}
	if len(cmds) != 1 || !strings.Contains(cmds[0], "base64 -d > 'step-001.png'") {
		t.Fatalf("cmds=%v", cmds)
	}
}
