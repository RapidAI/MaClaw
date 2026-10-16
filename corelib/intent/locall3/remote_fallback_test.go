package locall3

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// abstainingRuntime attaches a head whose probs are uniform (top-1 ≈ 0.33 <
// τ 0.80), so every call abstains.
func abstainingRuntime(t *testing.T, mode string) (*Runtime, *[]string) {
	t.Setenv(intent.LocalHeadEnv, "")
	t.Helper()
	dir := t.TempDir()
	w2 := [][]float64{make([]float64, 1024), make([]float64, 1024), make([]float64, 1024)}
	writeHead(t, dir, intent.ShippedLocalHeadName, "qwen3", 1024, map[string]any{"w2": w2})
	rt := NewWithMode(dir, mode)
	var logs []string
	rt.Logf = func(f string, a ...any) { logs = append(logs, f) }
	if _, err := rt.Attach(qwen); err != nil {
		t.Fatal(err)
	}
	return rt, &logs
}

func TestAbstainFallsBackToRemote(t *testing.T) {
	rt, logs := abstainingRuntime(t, intent.LocalL3On)
	calls := 0
	fn := rt.ContextFunc(func(context.Context, context.Context, string, string) (string, error) {
		calls++
		return `{"top":[{"skill":"web_search","score":0.9}]}`, nil
	})
	out, err := fn(context.Background(), context.Background(), "", "x")
	if err != nil || calls != 1 || !strings.Contains(out, "web_search") {
		t.Fatalf("out=%q err=%v calls=%d", out, err, calls)
	}
	if len(*logs) == 0 || !strings.Contains((*logs)[len(*logs)-1], "remote fallback") {
		t.Fatalf("logs=%v", *logs)
	}
}

func TestAbstainRemoteFailureKeepsHeadUncertainError(t *testing.T) {
	rt, _ := abstainingRuntime(t, intent.LocalL3On)
	fn := rt.ContextFunc(func(context.Context, context.Context, string, string) (string, error) {
		return "", errors.New("503")
	})
	_, err := fn(context.Background(), context.Background(), "", "x")
	if _, _, ok := intent.LocalHeadUncertainTop(err); !ok {
		t.Fatalf("want head-uncertain error, got %v", err)
	}
}

func TestStrictAbstainNeverCallsRemote(t *testing.T) {
	rt, _ := abstainingRuntime(t, intent.LocalL3Strict)
	calls := 0
	fn := rt.ContextFunc(func(context.Context, context.Context, string, string) (string, error) {
		calls++
		return "", nil
	})
	if _, err := fn(context.Background(), context.Background(), "", "x"); err == nil || calls != 0 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

func TestConfidentLocalNeverCallsRemote(t *testing.T) {
	dir := t.TempDir()
	writeHead(t, dir, intent.ShippedLocalHeadName, "qwen3", 1024, nil)
	rt := NewWithMode(dir, intent.LocalL3On)
	rt.Logf = func(string, ...any) {}
	if _, err := rt.Attach(qwen); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fn := rt.ContextFunc(func(context.Context, context.Context, string, string) (string, error) { calls++; return "", nil })
	out, err := fn(context.Background(), context.Background(), "", "x")
	if err != nil || calls != 0 || !strings.Contains(out, "file_ops") {
		t.Fatalf("out=%q err=%v calls=%d", out, err, calls)
	}
}
