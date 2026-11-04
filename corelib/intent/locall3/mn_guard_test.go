package locall3

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/embedding"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

// pooledEmb: Qwen3-family fake whose vector depends on the text: "файл" → e0,
// "сервер" → e1, anything else → e2.
type pooledEmb struct{ fakeEmb }

func (p pooledEmb) vec(text string) []float32 {
	v := make([]float32, p.dim)
	switch t := strings.ToLower(text); {
	case strings.Contains(t, "файл"):
		v[0] = 1
	case strings.Contains(t, "сервер"):
		v[1] = 1
	default:
		v[2] = 1
	}
	return v
}
func (p pooledEmb) EmbedPooledWithRole(text string, _ embedding.Role) ([]float32, []float32, error) {
	return p.vec(text), p.vec(text), nil
}

// writeMNGuard: file_write (side effect) for e0 p≈.9999, ssh (sensitive) for e1,
// search below τ for e2.
func writeMNGuard(t *testing.T, dir string, dim int) {
	labels := []string{"file_write", "ssh", "search", "unknown"}
	w2 := make([][]float64, len(labels))
	for i := range w2 {
		w2[i] = make([]float64, 2*dim)
	}
	w2[0][0], w2[1][1], w2[2][2] = 12, 12, 1
	m := map[string]any{"labels": labels, "w2": w2, "b2": []float64{0, 0, 0, 0}, "temperature": 1.0,
		"tau": intent.MNGuardTau, "tau_s": 2.0, "l2norm": true, "sensitive": []string{"ssh"},
		"input_blocks": []string{"last", "mean"}, "embed_role": "classification", "embed_model": "qwen3"}
	b, _ := json.Marshal(m)
	if err := os.WriteFile(filepath.Join(dir, intent.MNGuardHeadName), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func mnRuntime(t *testing.T, mode string) (*Runtime, string) {
	t.Helper()
	t.Setenv(intent.LocalHeadEnv, "")
	t.Setenv(intent.MNLocalEnv, mode)
	logp := filepath.Join(t.TempDir(), "mn.jsonl")
	t.Setenv(MNShadowLogEnv, logp)
	dir := t.TempDir()
	writeHead(t, dir, intent.ShippedLocalHeadName, "qwen3", 1024, nil)
	writeMNGuard(t, dir, 1024)
	rt := NewWithMode(dir, intent.LocalL3On)
	rt.Logf = func(string, ...any) {}
	if _, err := rt.Attach(pooledEmb{qwen}); err != nil {
		t.Fatal(err)
	}
	return rt, logp
}

func readMNLog(t *testing.T, p string) []mnShadowRecord {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	var out []mnShadowRecord
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		var r mnShadowRecord
		if err := json.Unmarshal([]byte(l), &r); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func TestMNModeDefaultIsShadow(t *testing.T) {
	t.Setenv(intent.MNLocalEnv, "")
	if intent.MNLocalMode() != intent.MNLocalShadow {
		t.Fatal("default must be shadow")
	}
}

// Shadow (default): remote decides every Cyrillic turn; local verdict logged.
func TestMNShadowLogsVerdictRemoteDecides(t *testing.T) {
	rt, logp := mnRuntime(t, "")
	calls := 0
	fn := rt.ContextFunc(remoteSpy(&calls))
	ctx := context.Background()
	for i, q := range []string{"Энэ файлд хадгална уу", "Серверт ssh холбогдоорой", "Мэдээлэл хайгаарай"} {
		out, err := fn(ctx, ctx, "", q)
		if err != nil || calls != i+1 || !strings.Contains(out, "web_search") {
			t.Fatalf("%q: out=%q err=%v calls=%d", q, out, err, calls)
		}
	}
	if out, _ := fn(ctx, ctx, "", "tidy up this folder"); calls != 3 || !strings.Contains(out, "file_ops") {
		t.Fatal("en must stay on the local head")
	}
	rt.mnWG.Wait()
	recs := readMNLog(t, logp)
	if len(recs) != 3 {
		t.Fatalf("records %d", len(recs))
	}
	// background writes may land in any order: key by local label.
	want := map[string][2]bool{"file_write": {true, false}, "ssh": {false, true}, "search": {false, false}}
	for i, r := range recs {
		w, ok := want[r.LocalLabel]
		delete(want, r.LocalLabel)
		if !ok || r.Mode != "shadow" || r.DecidedBy != "remote" || r.WouldAccept != w[0] ||
			r.Sensitive != w[1] || r.RemoteTop != "web_search" || r.Agree == nil || *r.Agree || r.TextHash == "" || r.Text != "" {
			t.Fatalf("rec %d: %+v", i, r)
		}
	}
	if fi, err := os.Stat(logp); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("log perm %v %v", fi, err)
	}
}

func TestMNOffNoHeadNoLog(t *testing.T) {
	rt, logp := mnRuntime(t, "off")
	calls := 0
	fn := rt.ContextFunc(remoteSpy(&calls))
	if _, err := fn(context.Background(), context.Background(), "", "Энэ файлд хадгална уу"); err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
	rt.mnWG.Wait()
	if recs := readMNLog(t, logp); len(recs) != 0 {
		t.Fatalf("off must not log: %d", len(recs))
	}
}

func TestMNGuardRouting(t *testing.T) {
	t.Setenv(intent.LocalHeadEnv, "")
	t.Setenv(intent.MNLocalEnv, "on")
	t.Setenv(MNShadowLogEnv, filepath.Join(t.TempDir(), "mn.jsonl"))
	dir := t.TempDir()
	writeHead(t, dir, intent.ShippedLocalHeadName, "qwen3", 1024, nil)
	writeMNGuard(t, dir, 1024)
	rt := NewWithMode(dir, intent.LocalL3On)
	rt.Logf = func(string, ...any) {}
	emb := pooledEmb{qwen}
	if _, err := rt.Attach(emb); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fn := rt.ContextFunc(remoteSpy(&calls))
	ctx := context.Background()
	// Confident non-sensitive MN → local (no MN-only confirmation; permissions as zh/en).
	q := "Энэ файлд хадгална уу"
	out, err := fn(ctx, ctx, "", q)
	if err != nil || calls != 0 || !strings.Contains(out, "file_write") {
		t.Fatalf("mn confident: out=%q err=%v calls=%d", out, err, calls)
	}
	// Confident sensitive MN → remote L3.
	if out, err := fn(ctx, ctx, "", "Серверт ssh холбогдоорой"); err != nil || calls != 1 || !strings.Contains(out, "web_search") {
		t.Fatalf("mn sensitive: out=%q err=%v calls=%d", out, err, calls)
	}
	// Low-confidence MN → remote L3.
	if _, err := fn(ctx, ctx, "", "Мэдээлэл хайгаарай"); err != nil || calls != 2 {
		t.Fatalf("mn low conf: err=%v calls=%d", err, calls)
	}
	// zh / en unchanged: main local head decides, remote untouched.
	for _, q := range []string{"整理一下这个文件夹", "整理一下這個資料夾", "tidy up this folder"} {
		out, err := fn(ctx, ctx, "", q)
		if err != nil || calls != 2 || !strings.Contains(out, "file_ops") {
			t.Fatalf("%q: out=%q err=%v calls=%d", q, out, err, calls)
		}
	}
	// Probe and tool-head bypass for Cyrillic stay as before.
	if _, _, _, ok := rt.ProbeFunc()(ctx, q); ok {
		t.Fatal("probe must still skip Cyrillic")
	}
}

func TestMNGuardAbsentKeepsBypass(t *testing.T) {
	rt := confidentRuntime(t, intent.LocalL3On)
	calls := 0
	fn := rt.ContextFunc(remoteSpy(&calls))
	if _, err := fn(context.Background(), context.Background(), "", "Энэ файлд хадгална уу"); err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}

// Head absent at first attach (still downloading) → Cyrillic goes remote, no
// log; once the file lands a re-Attach on the same embedder picks it up.
func TestMNGuardHotAttachAfterDownload(t *testing.T) {
	t.Setenv(intent.LocalHeadEnv, "")
	t.Setenv(intent.MNLocalEnv, "on")
	t.Setenv(MNShadowLogEnv, filepath.Join(t.TempDir(), "mn.jsonl"))
	dir := t.TempDir()
	writeHead(t, dir, intent.ShippedLocalHeadName, "qwen3", 1024, nil)
	rt := NewWithMode(dir, intent.LocalL3On)
	rt.Logf = func(string, ...any) {}
	emb := pooledEmb{qwen}
	if _, err := rt.Attach(emb); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fn := rt.ContextFunc(remoteSpy(&calls))
	ctx := context.Background()
	if _, err := fn(ctx, ctx, "", "Энэ файлд хадгална уу"); err != nil || calls != 1 {
		t.Fatalf("absent head must fall back to remote: err=%v calls=%d", err, calls)
	}
	writeMNGuard(t, dir, 1024)
	if _, err := rt.Attach(emb); err != nil {
		t.Fatal(err)
	}
	if out, err := fn(ctx, ctx, "", "Энэ файлд хадгална уу"); err != nil || calls != 1 || !strings.Contains(out, "file_write") {
		t.Fatalf("after download: out=%q err=%v calls=%d", out, err, calls)
	}
}

// A corrupt / wrong head file is refused: Cyrillic stays on remote L3.
func TestMNGuardBadFileFallsBack(t *testing.T) {
	t.Setenv(intent.LocalHeadEnv, "")
	t.Setenv(intent.MNLocalEnv, "on")
	dir := t.TempDir()
	writeHead(t, dir, intent.ShippedLocalHeadName, "qwen3", 1024, nil)
	if err := os.WriteFile(filepath.Join(dir, intent.MNGuardHeadName), []byte("{truncated"), 0o644); err != nil {
		t.Fatal(err)
	}
	rt := NewWithMode(dir, intent.LocalL3On)
	rt.Logf = func(string, ...any) {}
	if _, err := rt.Attach(pooledEmb{qwen}); err != nil {
		t.Fatal(err)
	}
	calls := 0
	fn := rt.ContextFunc(remoteSpy(&calls))
	if _, err := fn(context.Background(), context.Background(), "", "Энэ файлд хадгална уу"); err != nil || calls != 1 {
		t.Fatalf("err=%v calls=%d", err, calls)
	}
}
