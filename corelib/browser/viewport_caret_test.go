package browser

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/gorilla/websocket"
)

func acceptViewportInput(t *testing.T, err error) {
	t.Helper()
	if err != nil && !errors.Is(err, errViewportNewCaret) {
		t.Fatal(err)
	}
}

func TestViewportNextValueKeepsTheFieldPrefix(t *testing.T) {
	if got := viewportNextValue("", "", "13"); got != "13" {
		t.Fatalf("empty field: %q", got)
	}
	if got := viewportNextValue("13", "13", "1"); got != "1" {
		t.Fatalf("backspace: %q", got)
	}
	if got := viewportNextValue("+86", "", "1"); got != "1" {
		t.Fatalf("empty mirror replaces: %q", got)
	}
	if got := viewportNextValue("+8613", "13", "139"); got != "+86139" {
		t.Fatalf("edit with prefix: %q", got)
	}
	if got := viewportNextValue("other", "13", "9"); got != "9" {
		t.Fatalf("lost mirror: %q", got)
	}
}

func TestViewportTextUsesTheClickedField(t *testing.T) {
	fake := newViewportCDP(t)
	session := &Session{client: fake.client}
	t.Cleanup(func() { fake.client.Close() })

	for _, in := range []ViewportInput{
		{Kind: "down", X: 0.5, Y: 0.4, Button: "left"},
		{Kind: "text", Text: "13", Gen: 1},
		{Kind: "text", Text: "1", Gen: 1},
		{Kind: "down", X: 0.52, Y: 0.41, Button: "left"},
		{Kind: "text", Text: "9", Gen: 2},
	} {
		if err := session.ApplyViewportInput(in); err != nil && !errors.Is(err, errViewportNewCaret) {
			t.Fatalf("%s: %v", in.Kind, err)
		}
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if strings.Join(fake.setValues, ",") != "13,1,9" {
		t.Fatalf("field writes: %v", fake.setValues)
	}
	joined := strings.Join(fake.methods, "\n")
	focusAt := strings.Index(joined, "Emulation.setFocusEmulationEnabled")
	evalAt := strings.Index(joined, "Runtime.evaluate")
	if focusAt < 0 || evalAt < 0 || focusAt > evalAt {
		t.Fatalf("focus emulation should precede the caret script:\n%s", joined)
	}
	if strings.Count(joined, "Emulation.setFocusEmulationEnabled") < 2 {
		t.Fatalf("focus emulation stayed latched across the second click:\n%s", joined)
	}
	if !strings.Contains(joined, "elementsFromPoint") {
		t.Fatalf("click did not resolve an element:\n%s", joined)
	}
	if strings.Contains(joined, "Input.insertText") {
		t.Fatalf("text bypassed the field setter:\n%s", joined)
	}
}

func TestViewportTextErrorOmitsTheTypedValue(t *testing.T) {
	fake := newViewportCDP(t)
	fake.refuseRead = true
	session := &Session{client: fake.client}
	t.Cleanup(func() { fake.client.Close() })
	acceptViewportInput(t, session.ApplyViewportInput(ViewportInput{Kind: "down", X: 0.2, Y: 0.2, Button: "left"}))
	const typed = "13900001111"
	err := session.ApplyViewportInput(ViewportInput{Kind: "text", Text: typed, Gen: 1})
	if err == nil {
		t.Fatal("expected the write to fail when the field does not keep the text")
	}
	if strings.Contains(err.Error(), typed) {
		t.Fatalf("error echoes the typed text: %s", err.Error())
	}
}

func TestViewportUnchangedTextRequiresACaret(t *testing.T) {
	fake := newViewportCDP(t)
	fake.noCaret = true
	session := &Session{client: fake.client}
	t.Cleanup(func() { fake.client.Close() })
	acceptViewportInput(t, session.ApplyViewportInput(ViewportInput{Kind: "down", X: 0.2, Y: 0.2, Button: "left"}))
	err := session.ApplyViewportInput(ViewportInput{Kind: "text", Text: "", Gen: 1})
	if err != errViewportNoCaret {
		t.Fatalf("empty bar without a field: %v", err)
	}
}

func TestViewportEnterDoesNotInsertACharacter(t *testing.T) {
	fake := newViewportCDP(t)
	session := &Session{client: fake.client}
	t.Cleanup(func() { fake.client.Close() })
	for _, in := range []ViewportInput{
		{Kind: "down", X: 0.5, Y: 0.4, Button: "left"},
		{Kind: "text", Text: "13", Gen: 1},
		{Kind: "keydown", Key: "Enter"},
		{Kind: "keyup", Key: "Enter"},
	} {
		acceptViewportInput(t, session.ApplyViewportInput(in))
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if strings.Join(fake.setValues, ",") != "13" {
		t.Fatalf("field writes: %v", fake.setValues)
	}
	var down string
	for _, raw := range fake.keyEvents {
		if strings.Contains(raw, `"keyDown"`) {
			down = raw
			break
		}
	}
	if down == "" {
		t.Fatalf("missing keydown: %v", fake.keyEvents)
	}
	if strings.Contains(down, "\r") || strings.Contains(down, "\\r") || strings.Contains(down, `"text"`) {
		t.Fatalf("Enter inserted a character: %s", down)
	}
	if !strings.Contains(down, `"key":"Enter"`) || !strings.Contains(down, `"code":"Enter"`) || !strings.Contains(down, `"windowsVirtualKeyCode":13`) {
		t.Fatalf("Enter lost its key identity: %s", down)
	}
}

func TestViewportCaretScriptSkipsDocumentBody(t *testing.T) {
	js := viewportFocusCaretJS(10, 20) + viewportSetValueJS("x") + viewportReadValueJS
	if !strings.Contains(js, `tag === "BODY"`) || !strings.Contains(js, `tag === "HTML"`) {
		t.Fatal("caret script accepts the document body")
	}
	if !strings.Contains(js, `querySelector("input,textarea")`) {
		t.Fatal("caret script accepts a wrapper around inputs")
	}
	if strings.Contains(js, "el.isContentEditable)") {
		t.Fatal("caret script still treats every contenteditable ancestor as the field")
	}
	if !strings.Contains(js, `t === "tel"`) || !strings.Contains(js, `t === "password"`) {
		t.Fatal("caret script dropped text-like input types")
	}
	setJS := viewportSetValueJS("x")
	if strings.Contains(setJS, `Event("change"`) {
		t.Fatal("each keystroke commits a change event")
	}
	focusJS := viewportFocusCaretJS(10, 20)
	miss := strings.Index(focusJS, "if (!el) return")
	wipe := strings.Index(focusJS, "removeAttribute")
	if miss < 0 || wipe < 0 || miss > wipe {
		t.Fatal("a missed click clears the caret")
	}
	if !strings.Contains(focusJS, "same: same") {
		t.Fatal("focus result does not say whether the field changed")
	}
}

func TestViewportNewCaretDropsThePreviousBar(t *testing.T) {
	fake := newViewportCDP(t)
	session := &Session{client: fake.client}
	t.Cleanup(func() { fake.client.Close() })
	acceptViewportInput(t, session.ApplyViewportInput(ViewportInput{Kind: "down", X: 0.5, Y: 0.4, Button: "left"}))
	acceptViewportInput(t, session.ApplyViewportInput(ViewportInput{Kind: "text", Text: "1", Gen: 1}))
	err := session.ApplyViewportInput(ViewportInput{Kind: "down", X: 0.5, Y: 0.8, Button: "left"})
	if !errors.Is(err, errViewportNewCaret) {
		t.Fatalf("new field: %v", err)
	}
	const carried = "12"
	err = session.ApplyViewportInput(ViewportInput{Kind: "text", Text: carried, Gen: 1})
	if !errors.Is(err, errViewportNewCaret) {
		t.Fatalf("carried bar: %v", err)
	}
	if strings.Contains(err.Error(), carried) {
		t.Fatalf("error echoes the bar: %s", err.Error())
	}
	// The new field's own "1" starts like the previous number and must land.
	acceptViewportInput(t, session.ApplyViewportInput(ViewportInput{Kind: "text", Text: "1", Gen: 2}))
	acceptViewportInput(t, session.ApplyViewportInput(ViewportInput{Kind: "text", Text: "12", Gen: 2}))
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if strings.Join(fake.setValues, ",") != "1,1,12" {
		t.Fatalf("field writes: %v", fake.setValues)
	}
}

func TestViewportSameCaretKeepsTheBar(t *testing.T) {
	fake := newViewportCDP(t)
	fake.sameCaret = true
	session := &Session{client: fake.client}
	t.Cleanup(func() { fake.client.Close() })
	for _, in := range []ViewportInput{
		{Kind: "down", X: 0.5, Y: 0.4, Button: "left"},
		{Kind: "text", Text: "13"},
		{Kind: "down", X: 0.9, Y: 0.9, Button: "left"},
		{Kind: "text", Text: "139"},
	} {
		if err := session.ApplyViewportInput(in); err != nil {
			t.Fatalf("%s: %v", in.Kind, err)
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if strings.Join(fake.setValues, ",") != "13,139" {
		t.Fatalf("field writes: %v", fake.setValues)
	}
}

type viewportCDP struct {
	client     *CDPClient
	mu         sync.Mutex
	methods    []string
	setValues  []string
	keyEvents  []string
	field      string
	refuseRead bool
	noCaret    bool
	sameCaret  bool
}

func newViewportCDP(t *testing.T) *viewportCDP {
	t.Helper()
	fake := &viewportCDP{}
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			var req struct {
				ID     int64           `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
			}
			if err := conn.ReadJSON(&req); err != nil {
				return
			}
			result := map[string]interface{}{}
			switch req.Method {
			case "Page.getLayoutMetrics":
				result = map[string]interface{}{
					"cssVisualViewport": map[string]float64{"clientWidth": 1280, "clientHeight": 800},
					"layoutViewport":    map[string]float64{"clientWidth": 1280, "clientHeight": 800},
				}
			case "Runtime.evaluate":
				var p struct {
					Expression string `json:"expression"`
				}
				_ = json.Unmarshal(req.Params, &p)
				value := `{"ok":true}`
				if strings.Contains(p.Expression, "elementsFromPoint") {
					fake.mu.Lock()
					same := fake.sameCaret
					fake.mu.Unlock()
					if same {
						value = `{"ok":true,"same":true}`
					} else {
						value = `{"ok":true,"same":false}`
					}
				}
				switch {
				case strings.Contains(p.Expression, "maclaw-viewport-read"):
					fake.mu.Lock()
					current := fake.field
					refuse := fake.refuseRead
					noCaret := fake.noCaret
					fake.mu.Unlock()
					if noCaret {
						value = `{"ok":false}`
						break
					}
					if refuse {
						current = "nope"
					}
					raw, _ := json.Marshal(map[string]interface{}{"ok": true, "value": current})
					value = string(raw)
				case strings.Contains(p.Expression, "_valueTracker"):
					const needle = "const text = "
					if i := strings.Index(p.Expression, needle); i >= 0 {
						var text string
						if err := json.NewDecoder(strings.NewReader(p.Expression[i+len(needle):])).Decode(&text); err == nil {
							fake.mu.Lock()
							fake.field = text
							fake.setValues = append(fake.setValues, text)
							fake.mu.Unlock()
						}
					}
				}
				fake.mu.Lock()
				fake.methods = append(fake.methods, p.Expression)
				fake.mu.Unlock()
				result = map[string]interface{}{
					"result": map[string]interface{}{"type": "string", "value": value},
				}
			case "Input.dispatchKeyEvent":
				fake.mu.Lock()
				fake.keyEvents = append(fake.keyEvents, string(req.Params))
				fake.mu.Unlock()
			}
			fake.mu.Lock()
			fake.methods = append(fake.methods, req.Method)
			fake.mu.Unlock()
			_ = conn.WriteJSON(map[string]interface{}{"id": req.ID, "result": result})
		}
	}))
	t.Cleanup(srv.Close)
	client, err := ConnectCDP("ws" + strings.TrimPrefix(srv.URL, "http"))
	if err != nil {
		t.Fatal(err)
	}
	fake.client = client
	return fake
}
