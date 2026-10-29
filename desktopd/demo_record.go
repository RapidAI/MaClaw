package desktopd

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/demoskill"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
)

// Demonstrate-and-narrate recording (MACLAW_DEMO_SKILL, stage 3).
// The Hub opens a session for one bot display; while it is open desktopd
// polls the focused window, takes a screenshot when the focus changes (and
// every demoShotEvery), keeps the narration the Hub sends, and on end merges
// the person's input the supervisor logged into one demoskill.Timeline.
// Screenshots stay on this host under demoShotRoot()/<id>/; the timeline
// refers to them by file name.
var (
	demoPollEvery = 2 * time.Second
	demoShotEvery = 20 * time.Second
	demoMaxLength = 30 * time.Minute
	demoMaxShots  = 120
	demoKeepShots = 24 * time.Hour
)

func demoShotRoot() string { return filepath.Join(os.TempDir(), "maclaw-demo") }

type demoDeps struct {
	ctl  func(ctx context.Context, request map[string]any) (map[string]any, error)
	shot func(ctx context.Context) ([]byte, error)
}

type demoSession struct {
	mu        sync.Mutex
	id        string
	title     string
	started   time.Time
	dir       string
	events    []demoskill.Event
	shots     int
	lastFocus string
	lastShot  time.Time
	cancel    context.CancelFunc
	done      chan struct{}
}

type demoTable struct {
	mu       sync.Mutex
	sessions map[string]*demoSession
	now      func() time.Time
}

var demoSessions = &demoTable{sessions: map[string]*demoSession{}, now: time.Now}

func (t *demoTable) handle(ctx context.Context, key, op string, request map[string]any, deps demoDeps) (map[string]any, error) {
	if !demoskill.Enabled() {
		return nil, fmt.Errorf("%w: demo recording is off (%s)", ErrInvalid, demoskill.EnvFlag)
	}
	switch op {
	case desktop.CtlDemoBegin:
		title, _ := request["title"].(string)
		return t.begin(ctx, key, strings.TrimSpace(title), deps)
	case desktop.CtlDemoNarrate:
		text, _ := request["text"].(string)
		return t.narrate(key, text)
	case desktop.CtlDemoEnd:
		return t.end(ctx, key, deps)
	case desktop.CtlDemoShot:
		return t.shot(request)
	}
	return nil, fmt.Errorf("%w: unknown demo op %s", ErrInvalid, op)
}

// shot returns one stored PNG by timeline id and Ref (file name). The
// files stay under demoShotRoot after end so Hub can caption and the GUI
// can show thumbnails.
func (t *demoTable) shot(request map[string]any) (map[string]any, error) {
	id, _ := request["id"].(string)
	ref, _ := request["ref"].(string)
	id = strings.TrimSpace(id)
	ref = filepath.Base(strings.TrimSpace(ref))
	if id == "" || !strings.HasPrefix(id, "demo-") || ref == "" || ref != filepath.Base(ref) || !strings.HasSuffix(strings.ToLower(ref), ".png") {
		return nil, fmt.Errorf("%w: demo shot id/ref is invalid", ErrInvalid)
	}
	data, err := os.ReadFile(filepath.Join(demoShotRoot(), id, ref))
	if err != nil || len(data) == 0 {
		return nil, fmt.Errorf("%w: demo shot not found", ErrInvalid)
	}
	if len(data) > 8<<20 {
		return nil, fmt.Errorf("%w: demo shot is too large", ErrInvalid)
	}
	return map[string]any{"ok": true, "mime": "image/png", "data": base64.StdEncoding.EncodeToString(data)}, nil
}

func (t *demoTable) begin(ctx context.Context, key, title string, deps demoDeps) (map[string]any, error) {
	t.mu.Lock()
	if old := t.sessions[key]; old != nil {
		t.mu.Unlock()
		return map[string]any{"ok": true, "id": old.id, "already": true}, nil
	}
	t.mu.Unlock()
	if _, err := deps.ctl(ctx, map[string]any{"op": desktop.CtlDemoInputBegin}); err != nil {
		return nil, err
	}
	pruneDemoShots(t.now())
	var raw [8]byte
	_, _ = rand.Read(raw[:])
	id := "demo-" + hex.EncodeToString(raw[:])
	dir := filepath.Join(demoShotRoot(), id)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	runCtx, cancel := context.WithTimeout(context.Background(), demoMaxLength)
	s := &demoSession{id: id, title: title, started: t.now(), dir: dir, cancel: cancel, done: make(chan struct{})}
	t.mu.Lock()
	if old := t.sessions[key]; old != nil {
		t.mu.Unlock()
		cancel()
		_ = os.RemoveAll(dir)
		return map[string]any{"ok": true, "id": old.id, "already": true}, nil
	}
	t.sessions[key] = s
	t.mu.Unlock()
	go t.watch(runCtx, s, deps)
	return map[string]any{"ok": true, "id": id, "started_at": s.started.Unix()}, nil
}

func (t *demoTable) narrate(key, text string) (map[string]any, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("%w: narration text is empty", ErrInvalid)
	}
	if len([]rune(text)) > 2000 {
		text = string([]rune(text)[:2000])
	}
	t.mu.Lock()
	s := t.sessions[key]
	t.mu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("%w: no demo recording is open", ErrInvalid)
	}
	s.mu.Lock()
	s.events = append(s.events, demoskill.Event{At: t.now(), Kind: demoskill.KindNarration, Text: text})
	s.mu.Unlock()
	return map[string]any{"ok": true, "id": s.id}, nil
}

func (t *demoTable) end(ctx context.Context, key string, deps demoDeps) (map[string]any, error) {
	t.mu.Lock()
	s := t.sessions[key]
	delete(t.sessions, key)
	t.mu.Unlock()
	if s == nil {
		return nil, fmt.Errorf("%w: no demo recording is open", ErrInvalid)
	}
	s.cancel()
	<-s.done
	// One last look so the timeline ends on the final state of the screen.
	t.capture(ctx, s, deps, true)
	var input []any
	if out, err := deps.ctl(ctx, map[string]any{"op": desktop.CtlDemoInputEnd}); err == nil {
		input, _ = out["events"].([]any)
	}
	s.mu.Lock()
	events := append(append([]demoskill.Event(nil), s.events...), demoInputEvents(input)...)
	s.mu.Unlock()
	sort.SliceStable(events, func(i, j int) bool { return events[i].At.Before(events[j].At) })
	tl := demoskill.Timeline{ID: s.id, Title: s.title, StartedAt: s.started, Events: events}
	return map[string]any{"ok": true, "timeline": tl, "shot_dir": s.dir}, nil
}

func (t *demoTable) watch(ctx context.Context, s *demoSession, deps demoDeps) {
	defer close(s.done)
	t.capture(ctx, s, deps, true)
	tick := time.NewTicker(demoPollEvery)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			t.capture(ctx, s, deps, false)
		}
	}
}

// capture records a focus event when the active window changed and a
// screenshot when the focus changed, when force is set, or every demoShotEvery.
func (t *demoTable) capture(ctx context.Context, s *demoSession, deps demoDeps, force bool) {
	now := t.now()
	app, title := "", ""
	if out, err := deps.ctl(ctx, map[string]any{"op": "windows"}); err == nil {
		app, title = activeWindow(out)
	}
	focus := app + "\x00" + title
	s.mu.Lock()
	changed := focus != s.lastFocus && (app != "" || title != "")
	if changed {
		s.lastFocus = focus
		s.events = append(s.events, demoskill.Event{At: now, Kind: demoskill.KindFocus, App: app, Window: title})
	}
	wantShot := (changed || force || now.Sub(s.lastShot) >= demoShotEvery) && s.shots < demoMaxShots
	s.mu.Unlock()
	if !wantShot || deps.shot == nil {
		return
	}
	data, err := deps.shot(ctx)
	if err != nil || len(data) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shots >= demoMaxShots {
		return
	}
	s.shots++
	s.lastShot = now
	name := fmt.Sprintf("%03d.png", s.shots)
	if os.WriteFile(filepath.Join(s.dir, name), data, 0o600) != nil {
		return
	}
	s.events = append(s.events, demoskill.Event{At: now, Kind: demoskill.KindScreenshot, App: app, Window: title, Ref: name})
}

func activeWindow(out map[string]any) (string, string) {
	list, _ := out["windows"].([]any)
	for _, item := range list {
		w, _ := item.(map[string]any)
		if active, _ := w["active"].(bool); !active {
			continue
		}
		app, _ := w["class"].(string)
		title, _ := w["title"].(string)
		return strings.TrimSpace(app), strings.TrimSpace(title)
	}
	return "", ""
}

// demoInputEvents turns the supervisor's input log into timeline events.
// Coordinates stay normalized, scaled to 0..1000. Runs of printable keys
// become one "type" event that says how many keys, never what was typed.
func demoInputEvents(raw []any) []demoskill.Event {
	var out []demoskill.Event
	chars := 0
	var charAt time.Time
	flush := func() {
		if chars > 0 {
			out = append(out, demoskill.Event{At: charAt, Kind: demoskill.KindInput, Action: "type", Text: fmt.Sprintf("输入了 %d 个字符（内容未记录，见旁白）", chars)})
			chars = 0
		}
	}
	for _, item := range raw {
		e, _ := item.(map[string]any)
		if e == nil {
			continue
		}
		atF, _ := e["at"].(float64)
		at := time.Unix(0, int64(atF*float64(time.Second)))
		kind, _ := e["t"].(string)
		if kind == "key" {
			if code, _ := e["code"].(string); code == "char" {
				if chars == 0 {
					charAt = at
				}
				chars++
				continue
			} else if code != "" {
				flush()
				out = append(out, demoskill.Event{At: at, Kind: demoskill.KindInput, Action: "key", Text: code})
			}
			continue
		}
		flush()
		switch kind {
		case "down":
			x, _ := e["x"].(float64)
			y, _ := e["y"].(float64)
			action := "click"
			switch b, _ := e["b"].(float64); b {
			case 3:
				action = "right_click"
			case 2:
				action = "middle_click"
			}
			out = append(out, demoskill.Event{At: at, Kind: demoskill.KindInput, Action: action, X: int(x * 1000), Y: int(y * 1000)})
		case "wheel":
			out = append(out, demoskill.Event{At: at, Kind: demoskill.KindInput, Action: "scroll"})
		}
	}
	flush()
	return out
}

func pruneDemoShots(now time.Time) {
	entries, err := os.ReadDir(demoShotRoot())
	if err != nil {
		return
	}
	for _, e := range entries {
		info, err := e.Info()
		if err == nil && strings.HasPrefix(e.Name(), "demo-") && now.Sub(info.ModTime()) > demoKeepShots {
			_ = os.RemoveAll(filepath.Join(demoShotRoot(), e.Name()))
		}
	}
}
