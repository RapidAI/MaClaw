package browser

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestFillPasswordFieldUsesOneInsertText(t *testing.T) {
	const secret = "s3cret-value"
	var method string
	var got map[string]any
	calls := 0
	err := FillPasswordField(true, "password", secret, func(name string, params map[string]any) error {
		calls++
		method = name
		got = params
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || method != "Input.insertText" || got["text"] != secret {
		t.Fatalf("calls=%d method=%s params=%v", calls, method, got)
	}
}

func TestFillPasswordFieldRefusesTheWrongControl(t *testing.T) {
	const secret = "s3cret-value"
	send := func(string, map[string]any) error {
		t.Fatal("insert was called")
		return nil
	}
	cases := []struct {
		focused bool
		kind    string
		want    string
	}{
		{false, "password", "no_focus"},
		{false, "cross-origin", "not_password_field"},
		{false, "closed-shadow", "not_password_field"},
		{true, "text", "not_password_field"},
		{true, "cross-origin", "not_password_field"},
		{true, "closed-shadow", "not_password_field"},
		{true, "unreadable", "unavailable"},
		{false, "unreadable", "unavailable"},
		{true, "password", "unavailable"},
	}
	for _, tc := range cases {
		err := FillPasswordField(tc.focused, tc.kind, secret, nil)
		if tc.kind == "password" && tc.focused {
			err = FillPasswordField(true, "password", "", send)
		}
		if err == nil || err.Error() != tc.want || strings.Contains(err.Error(), secret) {
			t.Fatalf("focused=%v kind=%s err=%v", tc.focused, tc.kind, err)
		}
	}
	err := FillPasswordField(true, "password", secret, func(string, map[string]any) error {
		return errFill("boom " + secret)
	})
	if err == nil || err.Error() != "unavailable" || strings.Contains(err.Error(), secret) {
		t.Fatalf("send error leaked the value: %v", err)
	}
}

func TestClassifyFocusedKindRefusesOpaqueControls(t *testing.T) {
	if ClassifyFocusedKind("  Password ") != "password" {
		t.Fatal("password kind was not normalized")
	}
	if ClassifyFocusedKind("") != "" || ClassifyFocusedKind("   ") != "" {
		t.Fatal("an empty focus was classified as a control")
	}
	for _, kind := range []string{"password", "cross-origin", "closed-shadow", "unreadable"} {
		if !FocusBlocksModelType(kind) {
			t.Fatalf("%s did not block a model type", kind)
		}
	}
	focused, kind := focusKindFromEval(" password ", nil)
	if !focused || kind != "password" {
		t.Fatalf("focus eval focused=%v kind=%q", focused, kind)
	}
	if focused, kind = focusKindFromEval("  ", nil); focused || kind != "" {
		t.Fatalf("empty eval focused=%v kind=%q", focused, kind)
	}
	if focused, kind = focusKindFromEval("", errors.New("cdp")); !focused || kind != "unreadable" {
		t.Fatalf("failed eval focused=%v kind=%q", focused, kind)
	}
	for _, kind := range []string{"", "text", "email", "textarea"} {
		if FocusBlocksModelType(kind) {
			t.Fatalf("%s blocked a model type", kind)
		}
	}
	if strings.Contains(focusedInputKindJS, "确认支付") {
		t.Fatal("the focus walk mentions payment copy")
	}
	for _, needle := range []string{"shadowRoot.activeElement", "contentDocument", "cross-origin", "closed-shadow"} {
		if !strings.Contains(focusedInputKindJS, needle) {
			t.Fatalf("focus walk lost %s", needle)
		}
	}
}

func TestKeyInsertsTextIgnoresNavigation(t *testing.T) {
	for _, key := range []string{
		"p", "A", "shift+a", "Shift_L+a", "shift+shift+a", "space",
		"ctrl+v", "control+v", "Control_L+v", "Control_R+v",
		"ctrl+shift+v", "shift+ctrl+v", "ctrl+Shift_L+v", "Shift_L+Insert",
		"plus", "period", "minus", "shift+plus", "Shift_R+plus", "KP_Add",
		"ISO_Level3_Shift+a", "U00E9",
	} {
		if !KeyInsertsText(key) {
			t.Fatalf("%s was not treated as typing", key)
		}
	}
	for _, key := range []string{
		"", "Return", "Enter", "Tab", "BackSpace", "ctrl+a", "Control_L+a",
		"Escape", "Delete", "F5", "Page_Up", "shift+Tab", "Shift_L+Tab", "Up",
		"alt+Left", "Shift_L", "Control_L", "ISO_Left_Tab", "Caps_Lock",
		"KP_Enter", "KP_Left",
	} {
		if KeyInsertsText(key) {
			t.Fatalf("%s was treated as typing", key)
		}
	}
}

func TestModelTypeRefAllowedLeavesOrdinaryFieldsOpen(t *testing.T) {
	if modelTypeRefAllowed(nil) {
		t.Fatal("a missing ref was treated as a text field")
	}
	for _, ref := range []*BrowserElementRef{
		{Tag: "input", InputType: "email"},
		{Tag: "input", InputType: "text"},
		{Tag: "textarea"},
		{Tag: "div", Role: "textbox"},
	} {
		if !modelTypeRefAllowed(ref) {
			t.Fatalf("%+v was refused", ref)
		}
	}
	for _, ref := range []*BrowserElementRef{
		{Tag: "input"},
		{Tag: "input", InputType: "password"},
		{Tag: "input", InputType: "Password"},
		{Tag: "div"},
		{Tag: "fancy-password", InputType: "password"},
		{Tag: "input", InputType: "checkbox"},
	} {
		if modelTypeRefAllowed(ref) {
			t.Fatalf("%+v was allowed", ref)
		}
	}
}

func TestModelTypeRefForInsertDropsUnprovenNodes(t *testing.T) {
	email := &BrowserElementRef{Tag: "input", InputType: "email", BackendNodeID: 7}
	if got := modelTypeRefForInsert(true, email); got == nil || got.BackendNodeID != 7 {
		t.Fatalf("email node = %+v", got)
	}
	box := &BrowserElementRef{Tag: "textbox", Role: "textbox", BackendNodeID: 9}
	got := modelTypeRefForInsert(true, box)
	if got == nil || got.BackendNodeID != 0 || box.BackendNodeID != 9 {
		t.Fatalf("textbox node = %+v original = %d", got, box.BackendNodeID)
	}
	if got := modelTypeRefForInsert(false, box); got == nil || got.BackendNodeID != 9 {
		t.Fatalf("unblocked textbox node = %+v", got)
	}
	password := &BrowserElementRef{Tag: "input", InputType: "password", BackendNodeID: 4}
	if backendNodeTypeAllowed(password) || backendNodeTypeAllowed(&BrowserElementRef{Tag: "input", BackendNodeID: 3}) {
		t.Fatal("an unproven or password node was treated as a text field")
	}
	if !backendNodeTypeAllowed(&BrowserElementRef{Tag: "textarea", BackendNodeID: 2}) {
		t.Fatal("a textarea node was dropped")
	}
}

func TestModelTypeCandidateBlockedBeforePrepare(t *testing.T) {
	const secret = "s3cret-value"
	reads := []string{}
	read := func(selector string) (string, error) {
		reads = append(reads, selector)
		switch selector {
		case "input[name=absent]":
			return "missing", nil
		case "div.box":
			return "password", nil
		case "form":
			return "password", errors.New("eval failed " + secret)
		case "div.email", "input[type=email]":
			return "email", nil
		case "input":
			return "password", nil
		default:
			t.Fatalf("unexpected selector %s", selector)
			return "", nil
		}
	}
	focusCalls := 0
	focus := func() string {
		focusCalls++
		return "password"
	}
	email := &BrowserElementRef{Tag: "input", InputType: "email"}
	if err := modelTypeCandidateBlocked(email, []string{"input"}, focus, read); err == nil || err.Error() != "not_password_field" || focusCalls != 0 || strings.Join(reads, ",") != "input" {
		t.Fatalf("generic email candidate was typed: err=%v reads=%v focus=%d", err, reads, focusCalls)
	}
	reads = nil
	if err := modelTypeCandidateBlocked(email, []string{"input[type=email]"}, focus, read); err != nil || focusCalls != 0 || strings.Join(reads, ",") != "input[type=email]" {
		t.Fatalf("email selector was blocked by the focused password: err=%v reads=%v focus=%d", err, reads, focusCalls)
	}
	reads = nil
	password := &BrowserElementRef{Tag: "input", InputType: "Password"}
	if err := modelTypeCandidateBlocked(password, []string{"input[type=password]"}, focus, read); err == nil || err.Error() != "not_password_field" || len(reads) != 0 || strings.Contains(err.Error(), secret) {
		t.Fatalf("password ref reached a candidate: %v reads=%v", err, reads)
	}
	box := &BrowserElementRef{Tag: "div", Role: "div"}
	err := modelTypeCandidateBlocked(box, []string{"input[name=absent]", "div.box"}, func() string {
		t.Fatal("focus was read after a password candidate")
		return ""
	}, read)
	if err == nil || err.Error() != "not_password_field" || strings.Join(reads, ",") != "input[name=absent],div.box" {
		t.Fatalf("wrapper candidates: err=%v reads=%v", err, reads)
	}
	reads = nil
	err = modelTypeCandidateBlocked(nil, []string{"form"}, func() string {
		t.Fatal("selector-only type read the focused control")
		return "password"
	}, read)
	if err == nil || err.Error() != "not_password_field" || strings.Contains(err.Error(), secret) || strings.Join(reads, ",") != "form" {
		t.Fatalf("selector read error: err=%v reads=%v", err, reads)
	}
	reads = nil
	focusCalls = 0
	err = modelTypeCandidateBlocked(box, []string{" ", "div.email"}, focus, read)
	if err == nil || err.Error() != "not_password_field" || focusCalls != 1 || strings.Join(reads, ",") != "div.email" {
		t.Fatalf("email wrapper with a focused password: err=%v reads=%v focus=%d", err, reads, focusCalls)
	}
	quiet := func() string { return "" }
	if err := modelTypeCandidateBlocked(box, []string{"div.email"}, quiet, read); err != nil {
		t.Fatalf("email wrapper with nothing focused: %v", err)
	}
	if err := modelTypeCandidateBlocked(&BrowserElementRef{Tag: "div"}, []string{"div.box"}, nil, nil); err == nil || err.Error() != "not_password_field" {
		t.Fatalf("unreadable candidate was typed: %v", err)
	}
}

func TestReceiveTargetKindFollowsTheEditablePrepare(t *testing.T) {
	if !receiveKindBlocks("password") || !receiveKindBlocks("unreadable") || !receiveKindBlocks("closed-shadow") {
		t.Fatal("a password or unreadable target was allowed")
	}
	for _, kind := range []string{"", "missing", "email", "text", "textarea", "submit"} {
		if receiveKindBlocks(kind) {
			t.Fatalf("%s was refused", kind)
		}
	}
	jsdom, err := filepath.Abs(filepath.Join("..", "..", "guiapp", "frontend", "node_modules", "jsdom"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jsdom); err != nil {
		t.Fatalf("jsdom is required to run the receive classifier: %v", err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	names := []string{"activePassword", "activeEmail", "boxPassword", "boxEmail", "selPassword", "formPassword", "formEmailFirst", "selEmail", "missing", "framePassword", "shadowPassword", "selBoxPassword"}
	selectors := map[string]string{
		"activePassword": "",
		"activeEmail":    "",
		"boxPassword":    "",
		"boxEmail":       "",
		"selPassword":    `input[type="password"]`,
		"formPassword":   "form",
		"formEmailFirst": "form",
		"selEmail":       `input[type="email"]`,
		"missing":        `input[name="absent"]`,
		"framePassword":  `input[type="password"]`,
		"shadowPassword": `input[type="password"]`,
		"selBoxPassword": "div.box",
	}
	for _, name := range names {
		expr := receiveTargetKindJS(selectors[name])
		if strings.Contains(expr, "s3cret") {
			t.Fatal("receive script contains a secret")
		}
		if err := os.WriteFile(filepath.Join(dir, name+".js"), []byte(expr), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "receive-test.js")
	program := `
const fs = require('fs');
const path = require('path');
const {JSDOM} = require(process.argv[2]);
const dir = process.argv[3];
function run(name, setup) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { pretendToBeVisual: true, runScripts: 'dangerously', url: 'https://example.test/' });
  setup(dom.window);
  const expr = fs.readFileSync(path.join(dir, name + '.js'), 'utf8');
  return JSON.parse(dom.window.eval(expr)).kind;
}
function focus(el) { el.tabIndex = 0; el.focus(); }
function input(doc, type) {
  const el = doc.createElement('input');
  el.type = type;
  return el;
}
const report = {
  activePassword: run('activePassword', (window) => {
    const el = input(window.document, 'password');
    window.document.body.appendChild(el);
    focus(el);
  }),
  activeEmail: run('activeEmail', (window) => {
    const el = input(window.document, 'email');
    window.document.body.appendChild(el);
    focus(el);
  }),
  boxPassword: run('boxPassword', (window) => {
    const box = window.document.createElement('div');
    box.appendChild(input(window.document, 'password'));
    window.document.body.appendChild(box);
    focus(box);
  }),
  boxEmail: run('boxEmail', (window) => {
    const box = window.document.createElement('div');
    box.appendChild(input(window.document, 'email'));
    window.document.body.appendChild(box);
    focus(box);
  }),
  selPassword: run('selPassword', (window) => {
    window.document.body.appendChild(input(window.document, 'password'));
  }),
  formPassword: run('formPassword', (window) => {
    const form = window.document.createElement('form');
    form.appendChild(input(window.document, 'password'));
    window.document.body.appendChild(form);
  }),
  formEmailFirst: run('formEmailFirst', (window) => {
    const form = window.document.createElement('form');
    form.appendChild(input(window.document, 'email'));
    form.appendChild(input(window.document, 'password'));
    window.document.body.appendChild(form);
  }),
  selEmail: run('selEmail', (window) => {
    window.document.body.appendChild(input(window.document, 'email'));
  }),
  missing: run('missing', () => {}),
  framePassword: run('framePassword', (window) => {
    const frame = window.document.createElement('iframe');
    window.document.body.appendChild(frame);
    frame.contentDocument.body.appendChild(input(frame.contentDocument, 'password'));
  }),
  shadowPassword: run('shadowPassword', (window) => {
    const host = window.document.createElement('div');
    window.document.body.appendChild(host);
    host.attachShadow({mode: 'open'}).appendChild(input(window.document, 'password'));
  }),
  selBoxPassword: run('selBoxPassword', (window) => {
    const box = window.document.createElement('div');
    box.className = 'box';
    box.appendChild(input(window.document, 'password'));
    window.document.body.appendChild(box);
    const email = input(window.document, 'email');
    window.document.body.appendChild(email);
    focus(email);
  })
};
const expect = {
  activePassword: 'password', activeEmail: 'email', boxPassword: 'password', boxEmail: 'email',
  selPassword: 'password', formPassword: 'password', formEmailFirst: 'email', selEmail: 'email',
  missing: 'missing', framePassword: 'password', shadowPassword: 'password', selBoxPassword: 'password'
};
const failures = [];
for (const name of Object.keys(expect)) {
  if (report[name] !== expect[name]) failures.push(name + '=' + JSON.stringify(report[name]));
}
if (failures.length) throw new Error(failures.join('; ') + ' report=' + JSON.stringify(report));
console.log(JSON.stringify(report));
`
	if err := os.WriteFile(script, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script, jsdom, dir).CombinedOutput()
	if err != nil {
		t.Fatalf("receive kind: %v\n%s", err, out)
	}
	for _, kind := range []string{`"boxPassword":"password"`, `"formPassword":"password"`, `"formEmailFirst":"email"`, `"framePassword":"password"`, `"shadowPassword":"password"`, `"missing":"missing"`, `"selBoxPassword":"password"`} {
		if !strings.Contains(string(out), kind) {
			t.Fatalf("receive report missing %s: %s", kind, out)
		}
	}
}

func TestReceiveTargetKindStaysInTheNamedFrame(t *testing.T) {
	scoped := receiveTargetKindJSScoped("form", frameScope{Name: "login"}, true)
	plain := receiveTargetKindJS("form")
	if strings.Contains(scoped, "s3cret") || strings.Contains(plain, "s3cret") {
		t.Fatal("receive script contains a secret")
	}
	if !strings.Contains(scoped, `"login"`) || !strings.Contains(scoped, ",true)") {
		t.Fatalf("scoped receive did not limit the frame: %s", scoped)
	}
	if strings.Contains(plain, ",true)") {
		t.Fatal("unscoped receive limited the frame")
	}
	jsdom, err := filepath.Abs(filepath.Join("..", "..", "guiapp", "frontend", "node_modules", "jsdom"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jsdom); err != nil {
		t.Fatalf("jsdom is required to run the receive classifier: %v", err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, expr := range map[string]string{
		"parentEmail":   plain,
		"framePassword": scoped,
		"frameEmail":    scoped,
		"frameMissing":  receiveTargetKindJSScoped(`input[type="password"]`, frameScope{Name: "login"}, true),
		"frameGone":     receiveTargetKindJSScoped("form", frameScope{Name: "gone"}, true),
		"frameClosed":   receiveTargetKindJSScoped("form", frameScope{Name: "closed"}, true),
	} {
		if err := os.WriteFile(filepath.Join(dir, name+".js"), []byte(expr), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "frame-receive.js")
	program := `
const fs = require('fs');
const path = require('path');
const {JSDOM} = require(process.argv[2]);
const dir = process.argv[3];
function run(name, setup) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { pretendToBeVisual: true, runScripts: 'dangerously', url: 'https://example.test/' });
  setup(dom.window);
  const expr = fs.readFileSync(path.join(dir, name + '.js'), 'utf8');
  return JSON.parse(dom.window.eval(expr)).kind;
}
function input(doc, type) {
  const el = doc.createElement('input');
  el.type = type;
  return el;
}
function frame(doc, name) {
  const frame = doc.createElement('iframe');
  frame.name = name;
  doc.body.appendChild(frame);
  return frame;
}
const report = {
  parentEmail: run('parentEmail', (window) => {
    const doc = window.document;
    const outer = doc.createElement('form');
    outer.appendChild(input(doc, 'email'));
    doc.body.appendChild(outer);
    const child = frame(doc, 'login');
    const inner = child.contentDocument.createElement('form');
    inner.appendChild(input(child.contentDocument, 'password'));
    child.contentDocument.body.appendChild(inner);
  }),
  framePassword: run('framePassword', (window) => {
    const doc = window.document;
    const outer = doc.createElement('form');
    outer.appendChild(input(doc, 'email'));
    doc.body.appendChild(outer);
    const child = frame(doc, 'login');
    const inner = child.contentDocument.createElement('form');
    inner.appendChild(input(child.contentDocument, 'password'));
    child.contentDocument.body.appendChild(inner);
  }),
  frameEmail: run('frameEmail', (window) => {
    const doc = window.document;
    const outer = doc.createElement('form');
    outer.appendChild(input(doc, 'password'));
    doc.body.appendChild(outer);
    const child = frame(doc, 'login');
    const inner = child.contentDocument.createElement('form');
    inner.appendChild(input(child.contentDocument, 'email'));
    child.contentDocument.body.appendChild(inner);
  }),
  frameMissing: run('frameMissing', (window) => {
    const child = frame(window.document, 'login');
    child.contentDocument.body.appendChild(input(child.contentDocument, 'email'));
  }),
  frameGone: run('frameGone', () => {}),
  frameClosed: run('frameClosed', (window) => {
    const child = frame(window.document, 'closed');
    Object.defineProperty(child, 'contentDocument', { get() { throw new Error('cross-origin'); } });
    const outer = window.document.createElement('form');
    outer.appendChild(input(window.document, 'email'));
    window.document.body.appendChild(outer);
  })
};
const expect = {
  parentEmail: 'email', framePassword: 'password', frameEmail: 'email',
  frameMissing: 'missing', frameGone: 'cross-origin', frameClosed: 'cross-origin'
};
const failures = [];
for (const name of Object.keys(expect)) {
  if (report[name] !== expect[name]) failures.push(name + '=' + JSON.stringify(report[name]));
}
if (failures.length) throw new Error(failures.join('; ') + ' report=' + JSON.stringify(report));
console.log(JSON.stringify(report));
`
	if err := os.WriteFile(script, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script, jsdom, dir).CombinedOutput()
	if err != nil {
		t.Fatalf("frame receive kind: %v\n%s", err, out)
	}
	for _, kind := range []string{`"parentEmail":"email"`, `"framePassword":"password"`, `"frameEmail":"email"`, `"frameMissing":"missing"`, `"frameGone":"cross-origin"`, `"frameClosed":"cross-origin"`} {
		if !strings.Contains(string(out), kind) {
			t.Fatalf("frame receive report missing %s: %s", kind, out)
		}
	}
}

func TestFocusedInputWalkReadsNestedFramesAndShadows(t *testing.T) {
	// The walk script runs from a temp directory, so Node resolves this module
	// from that file, not from the package directory. An absolute path is required.
	jsdom, err := filepath.Abs(filepath.Join("..", "..", "guiapp", "frontend", "node_modules", "jsdom"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(jsdom); err != nil {
		t.Fatalf("jsdom is required to run the shipped focus walk: %v", err)
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "walk.js")
	source := filepath.Join(dir, "kind.js")
	if err := os.WriteFile(source, []byte(focusedInputKindJS), 0o644); err != nil {
		t.Fatal(err)
	}
	program := `
const fs = require('fs');
const {JSDOM} = require(process.argv[2]);
const source = fs.readFileSync(process.argv[3], 'utf8');
function kind(setup) {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', { pretendToBeVisual: true, runScripts: 'dangerously', url: 'https://example.test/' });
  setup(dom.window);
  return JSON.parse(dom.window.eval(source)).kind;
}
function focus(el) { el.tabIndex = 0; el.focus(); }
const nested = kind((window) => {
  const doc = window.document;
  const outer = doc.createElement('iframe');
  doc.body.appendChild(outer);
  const mid = outer.contentDocument.createElement('iframe');
  outer.contentDocument.body.appendChild(mid);
  const input = mid.contentDocument.createElement('input');
  input.type = 'password';
  mid.contentDocument.body.appendChild(input);
  // jsdom moves only the parent frame's activeElement on each focus() call.
  // Focusing from the outside in leaves the same public chain a browser leaves
  // after the inner password field is focused: outer iframe, inner iframe, input.
  focus(outer);
  focus(mid);
  focus(input);
});
const shadowed = kind((window) => {
  const doc = window.document;
  const frame = doc.createElement('iframe');
  doc.body.appendChild(frame);
  const host = frame.contentDocument.createElement('div');
  frame.contentDocument.body.appendChild(host);
  const root = host.attachShadow({mode: 'open'});
  const input = frame.contentDocument.createElement('input');
  input.type = 'password';
  root.appendChild(input);
  focus(input);
});
const blocked = kind((window) => {
  const doc = window.document;
  const frame = doc.createElement('iframe');
  frame.tabIndex = 0;
  doc.body.appendChild(frame);
  Object.defineProperty(frame, 'contentDocument', { get() { throw new Error('blocked'); } });
  focus(frame);
});
const closed = kind((window) => {
  const doc = window.document;
  class FancyPassword extends window.HTMLElement {}
  window.customElements.define('fancy-password', FancyPassword);
  const host = doc.createElement('fancy-password');
  doc.body.appendChild(host);
  const root = host.attachShadow({mode: 'closed'});
  const input = doc.createElement('input');
  input.type = 'password';
  root.appendChild(input);
  focus(input);
});
const lightCustom = kind((window) => {
  const doc = window.document;
  class LiteButton extends window.HTMLElement {}
  window.customElements.define('lite-button', LiteButton);
  const el = doc.createElement('lite-button');
  doc.body.appendChild(el);
  focus(el);
});
const plain = kind((window) => {
  const input = window.document.createElement('input');
  input.type = 'email';
  window.document.body.appendChild(input);
  focus(input);
});
const closedDiv = kind((window) => {
  const doc = window.document;
  const host = doc.createElement('div');
  doc.body.appendChild(host);
  const root = host.attachShadow({mode: 'closed'});
  const input = doc.createElement('input');
  input.type = 'password';
  root.appendChild(input);
  focus(input);
});
const nestedShadow = kind((window) => {
  const doc = window.document;
  const outer = doc.createElement('div');
  doc.body.appendChild(outer);
  const first = outer.attachShadow({mode: 'open'});
  const inner = doc.createElement('div');
  first.appendChild(inner);
  const second = inner.attachShadow({mode: 'open'});
  const input = doc.createElement('input');
  input.type = 'password';
  second.appendChild(input);
  focus(input);
});
const plainDiv = kind((window) => {
  const el = window.document.createElement('div');
  el.tabIndex = 0;
  window.document.body.appendChild(el);
  focus(el);
});
const deep = kind((window) => {
  let doc = window.document;
  const frames = [];
  for (let i = 0; i < 17; i++) {
    const frame = doc.createElement('iframe');
    doc.body.appendChild(frame);
    frames.push(frame);
    doc = frame.contentDocument;
  }
  const input = doc.createElement('input');
  input.type = 'password';
  doc.body.appendChild(input);
  frames.forEach(focus);
  focus(input);
});
const report = {nested, shadowed, blocked, closed, plain, closedDiv, nestedShadow, plainDiv, deep, lightCustom};
const failures = [];
if (nested !== 'password') failures.push('nested=' + JSON.stringify(nested));
if (shadowed !== 'password') failures.push('shadow=' + JSON.stringify(shadowed));
if (blocked !== 'cross-origin') failures.push('cross=' + JSON.stringify(blocked));
if (closed !== 'closed-shadow') failures.push('closed=' + JSON.stringify(closed));
if (plain !== 'email') failures.push('email=' + JSON.stringify(plain));
if (lightCustom !== '') failures.push('lightCustom=' + JSON.stringify(lightCustom));
if (closedDiv !== 'closed-shadow') failures.push('closedDiv=' + JSON.stringify(closedDiv));
if (nestedShadow !== 'password') failures.push('nestedShadow=' + JSON.stringify(nestedShadow));
if (plainDiv !== '') failures.push('plainDiv=' + JSON.stringify(plainDiv));
if (deep !== 'closed-shadow') failures.push('deep=' + JSON.stringify(deep));
if (failures.length) throw new Error(failures.join('; ') + ' report=' + JSON.stringify(report));
console.log(JSON.stringify(report));
`
	if err := os.WriteFile(script, []byte(program), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(node, script, jsdom, source).CombinedOutput()
	if err != nil {
		t.Fatalf("focus walk: %v\n%s", err, out)
	}
	text := string(out)
	for _, kind := range []string{`"nested":"password"`, `"shadowed":"password"`, `"blocked":"cross-origin"`, `"closed":"closed-shadow"`, `"plain":"email"`, `"closedDiv":"closed-shadow"`, `"nestedShadow":"password"`, `"plainDiv":""`, `"deep":"closed-shadow"`, `"lightCustom":""`} {
		if !strings.Contains(text, kind) {
			t.Fatalf("walk report missing %s: %s", kind, text)
		}
	}
}

func TestPaymentAndConsentNeedARealControl(t *testing.T) {
	if paymentConfirmFromSignals(false, false) {
		t.Fatal("a page without a card or checkout was treated as payment")
	}
	if !paymentConfirmFromSignals(true, false) || !paymentConfirmFromSignals(false, true) {
		t.Fatal("a card field or checkout frame was not payment")
	}
	if consentDialogFromSignals(true, true) || consentDialogFromSignals(true, false) {
		t.Fatal("a cookie accept was treated as a consent dialog")
	}
	if !consentDialogFromSignals(false, true) {
		t.Fatal("a consent dialog was ignored")
	}
	if !strings.Contains(browserPageFlagsCollectJS, `cc-number`) || !strings.Contains(browserPageFlagsCollectJS, "js.stripe.com") || !strings.Contains(browserPageFlagsCollectJS, "paypal.com") {
		t.Fatal("page flag script lost the card or checkout signal")
	}
	if strings.Contains(browserPageFlagsCollectJS, "确认支付") {
		t.Fatal("a pay confirmation alone is a page flag")
	}
}
