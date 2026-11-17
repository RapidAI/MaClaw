package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktop/inputcorrect"
)

type guiFix struct {
	mu            sync.Mutex
	text          string
	role          string
	name          string
	elementID     int
	className     string
	epoch         int
	scale         int
	images        int
	pending       bool
	editorReplace bool
	denied        bool
	secret        bool
	multi         bool
	windowErr     string
	stuck         bool
	badPaste      bool
	badType       bool
	observeN      int
	acts          []string
	setIDs        []int
	onObserve     func(*guiFix)
}

func newGUIFix(text string) *guiFix {
	return &guiFix{
		text: text, role: "entry", name: "body", elementID: 7,
		className: "composer", epoch: 1, scale: 100,
	}
}

func installGUI(t *testing.T, f *guiFix) {
	t.Helper()
	prevCtl, prevApp, prevRoot := desktopRemoteCtl, desktopRemoteApp, desktopInputProcedureRoot
	desktopInputProcedureRoot = t.TempDir()
	desktopRemoteCtl = func(_ context.Context, _, _, _ string, req map[string]any) (map[string]any, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch ctlString(req["op"]) {
		case desktop.CtlA11yProbe:
			f.observeN++
			if f.onObserve != nil {
				f.onObserve(f)
			}
			if f.secret {
				return map[string]any{"ok": true, "role": "password text", "focused_text": "p@ss-secret-value"}, nil
			}
			if f.denied {
				return map[string]any{"ok": true, "readback": "denied"}, nil
			}
			out := map[string]any{
				"ok": true, "focused_text": f.text, "role": f.role, "name": f.name,
				"id": f.elementID, "images": f.images, "upload_pending": f.pending,
				"editor_busy": false, "ref_epoch": f.epoch, "editor_replace": f.editorReplace,
			}
			if f.multi {
				out["focused_texts"] = []any{f.text, "other field"}
			}
			return out, nil
		case desktop.CtlScreen:
			return map[string]any{"ok": true, "scale": float64(f.scale) / 100}, nil
		case desktop.CtlWindows:
			if f.windowErr != "" {
				return nil, errors.New(f.windowErr)
			}
			return map[string]any{"ok": true, "windows": []any{map[string]any{
				"class": f.className, "active": true, "title": "must-not-be-stored",
			}}}, nil
		case desktop.CtlClipboardSet:
			f.acts = append(f.acts, "clipboard")
			if !f.stuck {
				if f.badPaste {
					f.text = ctlString(req["text"]) + ctlString(req["text"])
				} else {
					f.text = ctlString(req["text"])
				}
			}
			return map[string]any{"ok": true}, nil
		case desktop.CtlA11yAct:
			f.acts = append(f.acts, "semantic")
			f.setIDs = append(f.setIDs, ctlInt(req["id"]))
			if !f.stuck {
				f.text = ctlString(req["text"])
			}
			return map[string]any{"ok": true, "method": "atspi"}, nil
		default:
			return nil, errors.New("unexpected op")
		}
	}
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		joined := strings.Join(args, " ")
		f.acts = append(f.acts, joined)
		if f.stuck {
			return "", nil
		}
		if strings.Contains(joined, "ctrl+a") {
			f.text = ""
		}
		if len(args) > 0 && args[0] == "type" {
			chunk := args[len(args)-1]
			if f.badType {
				f.text = chunk + chunk
			} else {
				f.text += chunk
			}
		}
		return "", nil
	}
	t.Cleanup(func() {
		desktopRemoteCtl, desktopRemoteApp, desktopInputProcedureRoot = prevCtl, prevApp, prevRoot
	})
}

func inputScope(user, bot string) agentruntime.Scope {
	return agentruntime.Scope{TenantID: "tenant", UserID: user, InstanceID: bot, RunID: "run-" + bot}
}

func inputSteps(kind, text string) any {
	return []any{map[string]any{"action": kind, "text": text}}
}

func TestDesktopInputWrongElementAndStaleLayout(t *testing.T) {
	f := newGUIFix("hello")
	f.multi = true
	installGUI(t, f)
	_, err := desktopVerifySteps(context.Background(), inputScope("owner", "bot-a"), ":9", inputSteps("type", "hello"), nil, "ok", nil)
	if err == nil || !strings.Contains(err.Error(), "unverified") || strings.Contains(err.Error(), desktopExactInputMarker) {
		t.Fatalf("err=%v acts=%v", err, f.acts)
	}
	if len(f.acts) != 0 {
		t.Fatalf("typed into an ambiguous field: %v", f.acts)
	}

	f2 := newGUIFix("hel")
	f2.onObserve = func(g *guiFix) {
		if g.observeN == 2 {
			g.scale = 125
			g.epoch = 2
			g.elementID = 9
		}
	}
	installGUI(t, f2)
	started := time.Now()
	out, err := desktopVerifySteps(context.Background(), inputScope("owner", "bot-layout"), ":9", inputSteps("type", "hello"), nil, "ok", nil)
	if err != nil || !strings.Contains(out, desktopExactInputMarker) || strings.Contains(out, desktopInputUnverifiedMarker) {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("layout change waited")
	}
	if len(f2.setIDs) == 0 || f2.setIDs[0] != 9 {
		t.Fatalf("acted on the stale element: ids=%v acts=%v", f2.setIDs, f2.acts)
	}
}

func TestDesktopInputTruncationDuplicateAndNewline(t *testing.T) {
	const draft = "给同事的普通正文\n第二行"
	f := newGUIFix("给同事的普通正文\n第二")
	installGUI(t, f)
	out, err := desktopVerifySteps(context.Background(), inputScope("owner", "bot-text"), ":9", inputSteps("type", draft), nil, "ok", nil)
	if err != nil || !strings.Contains(out, desktopExactInputMarker) || f.text != draft {
		t.Fatalf("out=%q err=%v text=%q acts=%v", out, err, f.text, f.acts)
	}

	dup := newGUIFix(draft + draft)
	installGUI(t, dup)
	out, err = desktopVerifySteps(context.Background(), inputScope("owner", "bot-dup"), ":9", inputSteps("paste", draft), nil, "ok", nil)
	if err != nil || !strings.Contains(out, desktopExactInputMarker) || dup.text != draft {
		t.Fatalf("out=%q err=%v text=%q acts=%v", out, err, dup.text, dup.acts)
	}
	if strings.Contains(strings.Join(dup.acts, " "), "ctrl+a") {
		t.Fatalf("duplicate was cleared with a generic key: %v", dup.acts)
	}

	nl := newGUIFix("甲\r\n乙")
	installGUI(t, nl)
	out, err = desktopVerifySteps(context.Background(), inputScope("owner", "bot-nl"), ":9", inputSteps("type", "甲\n乙"), nil, "ok", nil)
	if err != nil || !strings.Contains(out, desktopExactInputMarker) || len(nl.acts) != 0 {
		t.Fatalf("out=%q err=%v acts=%v", out, err, nl.acts)
	}
}

func TestDesktopInputDeniedReadbackIsNotSuccess(t *testing.T) {
	f := newGUIFix("hello")
	f.denied = true
	installGUI(t, f)
	out, err := desktopVerifySteps(context.Background(), inputScope("owner", "bot-deny"), ":9", inputSteps("type", "hello"), nil, "ok", nil)
	if err == nil || !strings.Contains(err.Error(), "unverified") || strings.Contains(out, desktopExactInputMarker) || strings.Contains(out, desktopAppliedMarker) {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if len(f.acts) != 0 {
		t.Fatalf("denied read-back still typed: %v", f.acts)
	}
}

func TestDesktopInputBoundedFailureStops(t *testing.T) {
	f := newGUIFix("hel")
	f.stuck = true
	installGUI(t, f)
	out, err := desktopVerifySteps(context.Background(), inputScope("owner", "bot-bound"), ":9", inputSteps("type", "hello"), nil, "ok", nil)
	if err == nil || strings.Contains(err.Error(), "verified") && !strings.Contains(err.Error(), "unverified") || strings.Contains(out, desktopExactInputMarker) {
		t.Fatalf("out=%q err=%v acts=%v", out, err, f.acts)
	}
	if len(f.acts) == 0 || len(f.acts) > inputcorrect.MaxActs {
		t.Fatalf("acts=%v", f.acts)
	}
}

func TestDesktopInputPreservesEditorImages(t *testing.T) {
	native := newGUIFix("hel")
	native.images = 1
	installGUI(t, native)
	out, err := desktopVerifySteps(context.Background(), inputScope("owner", "bot-img"), ":9", inputSteps("type", "hello"), nil, "ok", nil)
	if err == nil || strings.Contains(out, desktopExactInputMarker) || strings.Contains(strings.Join(native.acts, " "), "ctrl+a") {
		t.Fatalf("native clear out=%q err=%v acts=%v", out, err, native.acts)
	}

	prevEval, prevRoot := desktopPageEval, desktopInputProcedureRoot
	desktopInputProcedureRoot = t.TempDir()
	page := &pageFix{text: "hel", images: 1}
	desktopPageEval = page.eval
	t.Cleanup(func() { desktopPageEval, desktopInputProcedureRoot = prevEval, prevRoot })
	session := &browser.BrowserAgentSession{}
	out, err = desktopVerifyBrowserType(context.Background(), inputScope("owner", "bot-editor"), session, "hello", "ok", nil)
	if err != nil || !strings.Contains(out, desktopExactInputMarker) || page.images != 1 || page.text != "hello" {
		t.Fatalf("out=%q err=%v page=%+v", out, err, page)
	}
	for _, kind := range page.kinds {
		if kind == inputcorrect.ActionClearField {
			t.Fatal("editor field was cleared")
		}
	}
	if strings.Contains(desktopInputActJS, "innerHTML") || strings.Contains(desktopInputObserveExpr, "csdn") {
		t.Fatal("page script is a site-specific or wholesale document replace")
	}
}

func TestDesktopInputUploadPendingIsNotSuccessUntilSettled(t *testing.T) {
	f := newGUIFix("hello")
	f.pending = true
	f.onObserve = func(g *guiFix) {
		if g.observeN >= 3 {
			g.pending = false
			g.images = 1
		}
	}
	installGUI(t, f)
	out, err := desktopVerifySteps(context.Background(), inputScope("owner", "bot-up"), ":9", inputSteps("type", "hello"), nil, "ok", nil)
	if err != nil || !strings.Contains(out, desktopExactInputMarker) {
		t.Fatalf("out=%q err=%v", out, err)
	}
	if !strings.Contains(out, "send, publish, and payment are not verified") {
		t.Fatalf("effect was not separated: %q", out)
	}
	if f.observeN < 3 || len(f.acts) != 0 {
		t.Fatalf("observe=%d acts=%v", f.observeN, f.acts)
	}
}

func TestDesktopInputAmbiguousEffectIsNotReplayed(t *testing.T) {
	scope := inputScope("owner", "bot-pay")
	desktopNoteEffectOutcome(scope.RunID, "publish", nil, "", errors.New("publish timed out and the outcome is unknown"))
	t.Cleanup(func() { desktopAmbiguousEffects.Delete(desktopAmbiguousKey(scope.RunID, inputcorrect.EffectPublish)) })
	approved := context.WithValue(context.Background(), desktopPublishApprovedKey{}, true)
	if desktopPublishAllowed(approved) != true {
		t.Fatal("approval fixture")
	}
	if err := desktopRejectAmbiguousReplay(scope, "publish", nil); err == nil || !strings.Contains(err.Error(), "not replayed") {
		t.Fatal(err)
	}
	if err := desktopRejectAmbiguousReplay(scope, "type", map[string]any{"text": "hello"}); err != nil {
		t.Fatal(err)
	}
	if inputcorrect.DecideEffect(inputcorrect.EffectPay, true, inputcorrect.Procedure{Successes: 3}).Replay {
		t.Fatal("pay replayed")
	}
}

func TestDesktopInputProcedureDoesNotGrantApproval(t *testing.T) {
	f := newGUIFix("hello")
	installGUI(t, f)
	scope := inputScope("owner", "bot-approval")
	if _, err := desktopVerifySteps(context.Background(), scope, ":9", inputSteps("paste", "hello"), nil, "ok", nil); err != nil {
		t.Fatal(err)
	}
	store := desktopInputStore(scope)
	got, ok := store.Best(inputcorrect.Conditions{Environment: runtime.GOOS, FieldClass: inputcorrect.FieldPlain, LayoutScale: 100, Clipboard: true, Keys: true, Semantic: true})
	if !ok || inputcorrect.GrantsApproval(got) {
		t.Fatalf("procedure=%+v ok=%v", got, ok)
	}
	if desktopPublishAllowed(context.Background()) {
		t.Fatal("remembered input approved publish")
	}
}

func TestDesktopInputSecretIsExcluded(t *testing.T) {
	f := newGUIFix("p@ss-secret-value")
	f.secret = true
	installGUI(t, f)
	out, err := desktopVerifySteps(context.Background(), inputScope("owner", "bot-secret"), ":9", inputSteps("type", "p@ss-secret-value"), nil, "typed p@ss-secret-value", nil)
	if err == nil || strings.Contains(out, "p@ss") || strings.Contains(err.Error(), "p@ss") || !strings.Contains(err.Error(), "secret_not_read") {
		t.Fatalf("out=%q err=%v", out, err)
	}
	raw := readInputStore(t)
	if strings.Contains(raw, "p@ss") || strings.Contains(raw, "secret-value") {
		t.Fatalf("stored secret: %s", raw)
	}
}

func TestDesktopInputScopeRestartReuseAndDemotion(t *testing.T) {
	const draft = "普通正文hello"
	root := t.TempDir()
	prevRoot := desktopInputProcedureRoot
	desktopInputProcedureRoot = root
	t.Cleanup(func() { desktopInputProcedureRoot = prevRoot })

	a := newGUIFix(draft)
	installReuse(t, a)
	scopeA := inputScope("owner", "bot-a")
	out, err := desktopVerifySteps(context.Background(), scopeA, ":9", inputSteps("type", draft), nil, "ok", nil)
	if err != nil || !strings.Contains(out, desktopExactInputMarker) {
		t.Fatalf("seed %q %v", out, err)
	}

	again := newGUIFix("普通正文")
	again.badType = true
	installReuse(t, again)
	out, err = desktopVerifySteps(context.Background(), scopeA, ":9", inputSteps("paste", draft), nil, "ok", nil)
	if err != nil || !strings.Contains(out, desktopExactInputMarker) || strings.Contains(strings.Join(again.acts, "\n"), "type") || strings.Contains(strings.Join(again.acts, " "), "ctrl+a") {
		t.Fatalf("recover %q %v acts=%v", out, err, again.acts)
	}
	if !containsAct(again.acts, "semantic") {
		t.Fatalf("identified field was not set: %v", again.acts)
	}

	other := newGUIFix("普")
	installReuse(t, other)
	scopeB := inputScope("owner", "bot-b")
	if _, err = desktopVerifySteps(context.Background(), scopeB, ":9", inputSteps("paste", draft), nil, "ok", nil); err != nil {
		t.Fatal(err)
	}
	if firstTyped(other.acts) == "type" {
		t.Fatalf("bot A strategy leaked to bot B: %v", other.acts)
	}
	raw := readInputStore(t)
	for _, leaked := range []string{draft, "must-not-be-stored", "cookie"} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("leaked %q in %s", leaked, raw)
		}
	}
}

func TestDesktopInputEmptyFieldIsUnidentified(t *testing.T) {
	f := newGUIFix("hello")
	f.elementID = 0
	installGUI(t, f)
	out, err := desktopVerifySteps(context.Background(), inputScope("owner", "bot-empty"), ":9", inputSteps("type", "hello"), nil, "ok", nil)
	if err == nil || !strings.Contains(err.Error(), "unverified") || len(f.acts) != 0 || strings.Contains(out, desktopAppliedMarker) {
		t.Fatalf("out=%q err=%v acts=%v", out, err, f.acts)
	}
}

func TestDesktopInputScriptIsNotPrefixSuccess(t *testing.T) {
	desktopInputProcedureRoot = t.TempDir()
	session := &browser.BrowserAgentSession{}
	out, err := desktopVerifyBrowserScript(context.Background(), inputScope("owner", "bot-script"), session, `el.value = "hello world"`, "ok", nil)
	if err == nil || strings.Contains(out, desktopAppliedMarker) || strings.Contains(err.Error(), "exact") {
		t.Fatalf("prefix fallback out=%q err=%v", out, err)
	}
	prev := desktopPageEval
	desktopPageEval = func(*browser.BrowserAgentSession, string) (string, error) {
		return `{"text":"hello","trusted":true,"identified":true,"element_id":4,"element_key":"INPUT:body:0","frame_id":"top:1","ref_epoch":2,"layout_scale":100,"semantic":true}`, nil
	}
	t.Cleanup(func() { desktopPageEval = prev })
	out, err = desktopVerifyBrowserScript(context.Background(), inputScope("owner", "bot-script2"), session, `el.value = "hello world"`, "ok", nil)
	if err == nil || strings.Contains(out, desktopAppliedMarker) {
		t.Fatalf("partial read counted as success out=%q err=%v", out, err)
	}
}

func TestDesktopInputForegroundLockIsNotARecovery(t *testing.T) {
	f := newGUIFix("hello")
	f.windowErr = "foreground_unavailable"
	installGUI(t, f)
	started := time.Now()
	out, err := desktopVerifySteps(context.Background(), inputScope("owner", "bot-fg"), ":9", inputSteps("type", "hello"), nil, "ok", nil)
	if err != nil || !strings.Contains(out, desktopExactInputMarker) {
		t.Fatalf("a window-class miss must not block an exact field: out=%q err=%v", out, err)
	}
	if time.Since(started) > 2*time.Second || len(f.acts) != 0 {
		t.Fatalf("waited or retyped: %v %s", f.acts, time.Since(started))
	}
	_, err = desktopVerifySteps(context.Background(), inputScope("owner", "bot-fg2"), ":9", inputSteps("type", "hello"), nil, "ok", errors.New("foreground_unavailable"))
	if err == nil || !strings.Contains(err.Error(), "foreground_unavailable") {
		t.Fatal(err)
	}
}

func installReuse(t *testing.T, f *guiFix) {
	t.Helper()
	prevCtl, prevApp := desktopRemoteCtl, desktopRemoteApp
	desktopRemoteCtl = func(_ context.Context, _, _, _ string, req map[string]any) (map[string]any, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		switch ctlString(req["op"]) {
		case desktop.CtlA11yProbe:
			f.observeN++
			return map[string]any{
				"ok": true, "focused_text": f.text, "role": f.role, "name": f.name,
				"id": f.elementID, "ref_epoch": f.epoch,
			}, nil
		case desktop.CtlScreen:
			return map[string]any{"ok": true, "scale": 1.0}, nil
		case desktop.CtlWindows:
			return map[string]any{"ok": true, "windows": []any{map[string]any{"class": "composer", "active": true}}}, nil
		case desktop.CtlClipboardSet:
			f.acts = append(f.acts, "clipboard")
			f.text = ctlString(req["text"])
			return map[string]any{"ok": true}, nil
		case desktop.CtlA11yAct:
			f.acts = append(f.acts, "semantic")
			f.text = ctlString(req["text"])
			return map[string]any{"ok": true}, nil
		default:
			return nil, errors.New("unexpected")
		}
	}
	desktopRemoteApp = func(_ context.Context, _, _, _ string, args []string) (string, error) {
		f.mu.Lock()
		defer f.mu.Unlock()
		joined := strings.Join(args, " ")
		if strings.Contains(joined, "ctrl+a") {
			f.acts = append(f.acts, "clear")
			f.text = ""
			return "", nil
		}
		if len(args) > 0 && args[0] == "type" {
			f.acts = append(f.acts, "type")
			chunk := args[len(args)-1]
			if f.badType {
				f.text = chunk + chunk
			} else {
				f.text += chunk
			}
		}
		return "", nil
	}
	t.Cleanup(func() { desktopRemoteCtl, desktopRemoteApp = prevCtl, prevApp })
}

func containsAct(acts []string, kind string) bool {
	for _, act := range acts {
		if act == kind {
			return true
		}
	}
	return false
}

func firstTyped(acts []string) string {
	for _, act := range acts {
		if act == "clear" || strings.Contains(act, "ctrl+a") {
			continue
		}
		if act == "semantic" || act == "clipboard" || act == "type" || strings.HasPrefix(act, "type ") {
			if strings.HasPrefix(act, "type") {
				return "type"
			}
			return act
		}
	}
	return ""
}

func readInputStore(t *testing.T) string {
	t.Helper()
	entries, err := jsonFiles(desktopInputProcedureRoot)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Join(entries, "\n")
}

type pageFix struct {
	mu     sync.Mutex
	text   string
	images int
	kinds  []string
	busy   int
}

func (p *pageFix) eval(session *browser.BrowserAgentSession, expr string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if strings.Contains(expr, "maclaw-input-observe") {
		raw, _ := json.Marshal(map[string]any{
			"text": p.text, "trusted": true, "denied": false, "secret": false,
			"images": p.images, "upload_pending": p.busy > 0, "editor_busy": p.busy > 0,
			"element_key": "textbox:body", "identified": true, "element_id": 1,
			"frame_id": "f1", "ref_epoch": 1, "layout_scale": 100,
			"semantic": p.images == 0, "clipboard": true, "keys": true, "editor_replace": p.images > 0,
		})
		return string(raw), nil
	}
	if strings.Contains(expr, "MACLAW_INPUT_ACT") {
		payload := expr[strings.Index(expr, "{"):]
		if i := strings.Index(payload, ";"); i >= 0 {
			payload = payload[:i]
		}
		var act struct {
			Kind string `json:"kind"`
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(payload), &act); err != nil {
			return "", err
		}
		p.kinds = append(p.kinds, act.Kind)
		if act.Kind == inputcorrect.ActionClearField {
			return `{"ok":false,"error":"images_present"}`, nil
		}
		p.text = act.Text
		raw, _ := json.Marshal(map[string]any{"ok": true, "images": p.images, "images_before": p.images})
		return string(raw), nil
	}
	return "", errors.New("unknown eval")
}

func jsonFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, string(raw))
	}
	return out, nil
}
