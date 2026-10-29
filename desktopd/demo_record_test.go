package desktopd

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/demoskill"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

func TestDemoRecordingOffByDefault(t *testing.T) {
	t.Setenv(demoskill.EnvFlag, "")
	tbl := &demoTable{sessions: map[string]*demoSession{}, now: time.Now}
	if _, err := tbl.handle(context.Background(), "k", desktop.CtlDemoBegin, nil, demoDeps{}); err == nil {
		t.Fatal("recording must be off without the flag")
	}
}

func TestDemoRecordingBuildsTimeline(t *testing.T) {
	t.Setenv(demoskill.EnvFlag, "1")
	t.Setenv("TMPDIR", t.TempDir())
	oldPoll := demoPollEvery
	demoPollEvery = 10 * time.Millisecond
	defer func() { demoPollEvery = oldPoll }()

	var mu sync.Mutex
	title := "收件箱 - Mail"
	var ops []string
	base := float64(time.Now().Unix())
	deps := demoDeps{
		ctl: func(_ context.Context, req map[string]any) (map[string]any, error) {
			op, _ := req["op"].(string)
			mu.Lock()
			defer mu.Unlock()
			ops = append(ops, op)
			switch op {
			case "windows":
				return map[string]any{"windows": []any{
					map[string]any{"active": false, "class": "Xfce4-terminal", "title": "term"},
					map[string]any{"active": true, "class": "Thunderbird", "title": title},
				}}, nil
			case desktop.CtlDemoInputEnd:
				return map[string]any{"events": []any{
					map[string]any{"t": "down", "b": float64(1), "x": 0.5, "y": 0.25, "at": base + 1},
					map[string]any{"t": "key", "code": "char", "at": base + 2},
					map[string]any{"t": "key", "code": "char", "at": base + 2.1},
					map[string]any{"t": "key", "code": "Enter", "at": base + 3},
				}}, nil
			}
			return map[string]any{"ok": true}, nil
		},
		shot: func(context.Context) ([]byte, error) { return []byte("png"), nil },
	}
	tbl := &demoTable{sessions: map[string]*demoSession{}, now: time.Now}
	ctx := context.Background()
	if _, err := tbl.handle(ctx, "k", desktop.CtlDemoBegin, map[string]any{"title": "发邮件"}, deps); err != nil {
		t.Fatal(err)
	}
	if _, err := tbl.handle(ctx, "k", desktop.CtlDemoNarrate, map[string]any{"text": "把周报发给老板"}, deps); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	title = "写邮件"
	mu.Unlock()
	time.Sleep(50 * time.Millisecond)
	out, err := tbl.handle(ctx, "k", desktop.CtlDemoEnd, nil, deps)
	if err != nil {
		t.Fatal(err)
	}
	tl := out["timeline"].(demoskill.Timeline)
	kinds := map[string]int{}
	var typed, click string
	for _, e := range tl.Events {
		kinds[e.Kind]++
		if e.Action == "type" {
			typed = e.Text
		}
		if e.Action == "click" && e.X == 500 && e.Y == 250 {
			click = "ok"
		}
	}
	if kinds[demoskill.KindFocus] != 2 || kinds[demoskill.KindNarration] != 1 || kinds[demoskill.KindScreenshot] < 2 || click == "" {
		t.Fatalf("timeline = %+v", tl.Events)
	}
	if !strings.Contains(typed, "2 个字符") {
		t.Fatalf("typed = %q", typed)
	}
	if _, err := os.Stat(filepath.Join(out["shot_dir"].(string), "001.png")); err != nil {
		t.Fatal(err)
	}
	if ops[0] != desktop.CtlDemoInputBegin {
		t.Fatalf("ops = %v", ops)
	}
	if _, err := tbl.handle(ctx, "k", desktop.CtlDemoNarrate, map[string]any{"text": "x"}, deps); err == nil {
		t.Fatal("narration after end must fail")
	}
}

func TestDemoOpsAreHubOnly(t *testing.T) {
	for _, op := range []string{desktop.CtlDemoBegin, desktop.CtlDemoEnd, desktop.CtlDemoInputBegin} {
		if !desktop.IsCtlHubOnlyOp(op) || !desktop.IsCtlBotOp(op) {
			t.Fatalf("%s must be a Hub-only bot op", op)
		}
	}
}

func TestDemoShotFetch(t *testing.T) {
	t.Setenv(demoskill.EnvFlag, "1")
	t.Setenv("TMPDIR", t.TempDir())
	tbl := &demoTable{sessions: map[string]*demoSession{}, now: time.Now}
	deps := demoDeps{
		ctl:  func(context.Context, map[string]any) (map[string]any, error) { return map[string]any{"ok": true}, nil },
		shot: func(context.Context) ([]byte, error) { return []byte("png-bytes"), nil },
	}
	ctx := context.Background()
	begin, err := tbl.handle(ctx, "k", desktop.CtlDemoBegin, nil, deps)
	if err != nil {
		t.Fatal(err)
	}
	id := begin["id"].(string)
	time.Sleep(30 * time.Millisecond)
	end, err := tbl.handle(ctx, "k", desktop.CtlDemoEnd, nil, deps)
	if err != nil {
		t.Fatal(err)
	}
	_ = end
	out, err := tbl.handle(ctx, "k", desktop.CtlDemoShot, map[string]any{"id": id, "ref": "001.png"}, deps)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := decodeB64(out["data"].(string))
	if err != nil || string(raw) != "png-bytes" {
		t.Fatalf("data=%v err=%v", out["data"], err)
	}
	if _, err := tbl.handle(ctx, "k", desktop.CtlDemoShot, map[string]any{"id": id, "ref": "../x.png"}, deps); err == nil {
		t.Fatal("path escape must fail")
	}
}

func decodeB64(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
