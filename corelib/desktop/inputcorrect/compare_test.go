package inputcorrect

import (
	"context"
	"errors"
	"strings"
	"testing"

	"golang.org/x/text/unicode/norm"
)

func TestCompareExactNewlineAndUnicode(t *testing.T) {
	nfd := norm.NFD.String("é")
	nfc := norm.NFC.String("é")
	if nfd == nfc {
		t.Fatal("fixture did not produce distinct Unicode forms")
	}
	cases := []struct {
		name     string
		expected string
		observed string
		match    bool
		kind     string
	}{
		{"crlf", "甲\n乙", "甲\r\n乙", true, Match},
		{"cr", "甲\n乙", "甲\r乙", true, Match},
		{"nfc", "前缀" + nfc, "前缀" + nfd, true, Match},
		{"truncated", "hello", "hell", false, Truncated},
		{"duplicate", "hello", "hellohello", false, Duplicate},
		{"newline", "甲\n乙", "甲\n\n乙", false, Newline},
		{"case", "Hello", "hello", false, Mismatch},
		{"space", "a b", "ab", false, Mismatch},
		{"empty", "  ", "x", false, EmptyExpected},
	}
	for _, tc := range cases {
		ok, kind := CompareExact(tc.expected, tc.observed)
		if ok != tc.match || kind != tc.kind {
			t.Errorf("%s: got %v %s", tc.name, ok, kind)
		}
	}
}

func TestRecoverableInputErrorIgnoresForegroundLock(t *testing.T) {
	if RecoverableInputError(errors.New("foreground_unavailable")) {
		t.Fatal("foreground_unavailable is not an unattended recovery")
	}
	if !RecoverableInputError(nil) {
		t.Fatal("a nil tool error can still be checked")
	}
	if !RecoverableInputError(errors.New("ref @e1 is stale")) {
		t.Fatal("stale ref is recoverable")
	}
}

func TestDecideEffectNeverReplaysOrInheritsApproval(t *testing.T) {
	verified := Procedure{Strategy: StrategyClipboard, Successes: 4, Confidence: 1}
	if GrantsApproval(verified) {
		t.Fatal("a verified procedure granted approval")
	}
	for _, kind := range []string{EffectSend, EffectPublish, EffectPay} {
		for _, ambiguous := range []bool{true, false} {
			d := DecideEffect(kind, ambiguous, verified)
			if d.Replay {
				t.Fatalf("%s ambiguous=%v replayed", kind, ambiguous)
			}
		}
	}
	if DecideEffect(EffectInput, true, verified).Replay {
		t.Fatal("input is not an external effect replay")
	}
}

func TestRunSecretDoesNotTouchRelayOrStore(t *testing.T) {
	relay := &scriptRelay{fail: t.Fatal}
	dir := t.TempDir()
	store := Open(dir, Scope{Tenant: "t", Owner: "o", Bot: "b"})
	out := Run(t.Context(), Request{Expected: "p@ss-secret-value", Secret: true, Effect: EffectInput, Environment: "linux"}, relay, store)
	if out.Status != StatusUnverified || out.InputVerified || out.Reason != "secret_not_read" {
		t.Fatalf("%+v", out)
	}
	if relay.observes != 0 || len(relay.acts) != 0 {
		t.Fatalf("secret was observed or typed: %+v", relay)
	}
	raw := readStore(t, dir)
	if strings.Contains(raw, "p@ss") || strings.Contains(raw, "secret-value") {
		t.Fatalf("secret stored: %s", raw)
	}
}

func TestRunRefusesExternalEffectWithoutActing(t *testing.T) {
	relay := &scriptRelay{}
	out := Run(t.Context(), Request{Expected: "hello", Effect: EffectPublish}, relay, nil)
	if out.Status != StatusRefused || out.EffectVerified || len(relay.acts) != 0 || relay.observes != 0 {
		t.Fatalf("%+v acts=%v", out, relay.acts)
	}
}

type scriptRelay struct {
	obs             []Observation
	text            string
	acts            []Action
	images          int
	fail            func(...any)
	observes        int
	firstActObserve int
	stuck           bool
	badSeg          bool
}

func (r *scriptRelay) Observe(context.Context) (Observation, error) {
	return r.observe()
}

func (r *scriptRelay) observe() (Observation, error) {
	r.observes++
	var base Observation
	if len(r.obs) == 0 {
		base = Observation{Unavailable: true}
	} else if r.observes-1 < len(r.obs) {
		base = r.obs[r.observes-1]
	} else {
		base = r.obs[len(r.obs)-1]
	}
	if len(r.acts) > 0 && base.TextTrusted && !base.ReadbackDenied && !base.Secret {
		base.FieldText = r.text
	}
	if r.images > 0 {
		base.ImageCount = r.images
	}
	return base, nil
}

func (r *scriptRelay) Act(_ context.Context, action Action) error {
	if r.fail != nil && action.Kind == EffectPublish {
		r.fail("publish act")
	}
	if !AllowedKind(action.Kind) {
		return errors.New("rejected")
	}
	r.acts = append(r.acts, action)
	if r.firstActObserve == 0 && action.Kind != ActionClearField {
		r.firstActObserve = r.observes
	}
	if r.stuck {
		return nil
	}
	switch action.Kind {
	case ActionClearField:
		r.text = ""
		if r.images > 0 {
			r.images = 0
		}
	case StrategyEditorReplace:
		r.text = action.Text
	case StrategySegmented:
		if r.badSeg {
			r.text = action.Text + action.Text
			return nil
		}
		r.text = action.Text
	default:
		r.text = action.Text
	}
	return nil
}

func plain(text string) Observation {
	return Observation{
		FrameID: "surface", RefEpoch: 1, LayoutScale: 100,
		ElementID: 4, ElementKey: "entry:body", Identified: true,
		FieldText: text, TextTrusted: true,
		Capability: Capability{Semantic: true, Clipboard: true, Keys: true},
	}
}

func TestRunWrongElementDoesNotType(t *testing.T) {
	wrong := plain("hello")
	wrong.ElementKey = "entry:title"
	relay := &scriptRelay{obs: []Observation{wrong, wrong}}
	out := Run(t.Context(), Request{Expected: "hello", ElementKey: "entry:body", Effect: EffectInput, Environment: "linux"}, relay, nil)
	if out.Status != StatusFailed || out.Reason != "wrong_element" || len(relay.acts) != 0 {
		t.Fatalf("%+v acts=%d", out, len(relay.acts))
	}
}

func TestRunStaleLayoutReobservesBeforeTyping(t *testing.T) {
	first := plain("hel")
	second := plain("hel")
	second.LayoutScale = 125
	second.RefEpoch = 2
	relay := &scriptRelay{obs: []Observation{first, second, second, second}}
	out := Run(t.Context(), Request{Expected: "hello", Effect: EffectInput, Environment: "linux"}, relay, nil)
	if !out.InputVerified || out.EffectVerified {
		t.Fatalf("%+v", out)
	}
	if len(relay.acts) == 0 || relay.acts[0].ElementID != second.ElementID {
		t.Fatalf("acts=%+v", relay.acts)
	}
	if relay.firstActObserve < 4 {
		t.Fatalf("typed before the new layout was stable: observe=%d", relay.firstActObserve)
	}
}

func TestRunTruncationDuplicateAndNewline(t *testing.T) {
	relay := &scriptRelay{obs: []Observation{plain("hell")}}
	out := Run(t.Context(), Request{Expected: "hello", Effect: EffectInput, Environment: "linux"}, relay, nil)
	if !out.InputVerified || relay.text != "hello" {
		t.Fatalf("%+v text=%q", out, relay.text)
	}
	dup := plain("hellohello")
	dup.ElementID = 0
	dup.Capability.Semantic = false
	relay = &scriptRelay{obs: []Observation{dup, dup, dup, dup}}
	out = Run(t.Context(), Request{Expected: "hello", Effect: EffectInput, Environment: "linux"}, relay, nil)
	if !out.InputVerified || relay.acts[0].Kind != ActionClearField {
		t.Fatalf("%+v acts=%+v", out, relay.acts)
	}
	nl := plain("甲\r\n乙")
	relay = &scriptRelay{obs: []Observation{nl}}
	out = Run(t.Context(), Request{Expected: "甲\n乙", Effect: EffectInput, Environment: "linux"}, relay, nil)
	if !out.InputVerified || len(relay.acts) != 0 {
		t.Fatalf("newline fold typed again: %+v acts=%d", out, len(relay.acts))
	}
}

func TestRunDeniedReadbackIsNotSuccess(t *testing.T) {
	obs := plain("hello")
	obs.ReadbackDenied = true
	obs.TextTrusted = false
	relay := &scriptRelay{obs: []Observation{obs}}
	out := Run(t.Context(), Request{Expected: "hello", Effect: EffectInput, Environment: "linux"}, relay, nil)
	if out.Status != StatusUnverified || out.InputVerified || len(relay.acts) != 0 {
		t.Fatalf("%+v", out)
	}
}

func TestRunBoundedFailureChangesStrategyThenStops(t *testing.T) {
	obs := plain("hel")
	relay := &scriptRelay{obs: []Observation{obs}, stuck: true, text: "hel"}
	out := Run(t.Context(), Request{Expected: "hello", Effect: EffectInput, Environment: "linux"}, relay, nil)
	if out.Status != StatusFailed || !out.Escalated || out.InputVerified {
		t.Fatalf("%+v", out)
	}
	typed := 0
	for _, act := range relay.acts {
		if act.Kind != ActionClearField {
			typed++
		}
	}
	if typed != MaxActs {
		t.Fatalf("typing acts=%d %+v", typed, relay.acts)
	}
}

func TestRunPreservesEditorImagesAndWaitsForUpload(t *testing.T) {
	busy := plain("hello")
	busy.UploadPending = true
	busy.ImageCount = 0
	ready := plain("hello")
	ready.ImageCount = 1
	ready.Capability.EditorReplace = true
	relay := &scriptRelay{obs: []Observation{busy, busy, ready}}
	out := Run(t.Context(), Request{Expected: "hello", MinImages: 1, Effect: EffectInput, Environment: "linux"}, relay, nil)
	if !out.InputVerified || !out.EditorSettled || out.EffectVerified || out.Images != 1 || len(relay.acts) != 0 {
		t.Fatalf("%+v acts=%d", out, len(relay.acts))
	}
	partial := plain("hel")
	partial.ImageCount = 1
	partial.Capability.EditorReplace = true
	relay = &scriptRelay{obs: []Observation{partial, partial, partial}, images: 1}
	out = Run(t.Context(), Request{Expected: "hello", Effect: EffectInput, Environment: "linux"}, relay, nil)
	if !out.InputVerified || relay.images != 1 {
		t.Fatalf("%+v images=%d acts=%+v", out, relay.images, relay.acts)
	}
	for _, act := range relay.acts {
		if act.Kind == ActionClearField {
			t.Fatal("clear removed editor content")
		}
	}
	never := plain("hello")
	never.UploadPending = true
	relay = &scriptRelay{obs: []Observation{never}}
	out = Run(t.Context(), Request{Expected: "hello", Effect: EffectInput, Environment: "linux"}, relay, nil)
	if out.Status != StatusUnverified || out.Reason != "editor_unsettled" || out.InputVerified {
		t.Fatalf("%+v", out)
	}
}

func TestRunReuseDemotionAndScope(t *testing.T) {
	dir := t.TempDir()
	a := Open(dir, Scope{Tenant: "t", Owner: "owner", Bot: "bot-a"})
	cond := Conditions{Environment: "linux", FieldClass: FieldPlain, LayoutScale: 100, Semantic: true, Clipboard: true, Keys: true}
	obs := plain("hello")
	relay := &scriptRelay{obs: []Observation{obs}}
	out := Run(t.Context(), Request{Expected: "hello", UsedStrategy: StrategySegmented, Effect: EffectInput, Environment: "linux"}, relay, a)
	if !out.InputVerified {
		t.Fatal(out.Reason)
	}
	restarted := Open(dir, Scope{Tenant: "t", Owner: "owner", Bot: "bot-a"})
	got, ok := restarted.Best(cond)
	if !ok || got.Strategy != StrategySegmented {
		t.Fatalf("restart best=%+v ok=%v", got, ok)
	}
	other := Open(dir, Scope{Tenant: "t", Owner: "owner", Bot: "bot-b"})
	if _, ok := other.Best(cond); ok {
		t.Fatal("procedure leaked across bots")
	}
	partial := plain("hel")
	relay = &scriptRelay{obs: []Observation{partial, partial, partial, partial}, badSeg: true}
	out = Run(t.Context(), Request{Expected: "hello", Effect: EffectInput, Environment: "linux"}, relay, restarted)
	first := ""
	for _, act := range relay.acts {
		if act.Kind != ActionClearField {
			first = act.Kind
			break
		}
	}
	if !out.InputVerified || first != StrategySegmented {
		t.Fatalf("%+v first=%s acts=%+v", out, first, relay.acts)
	}
	if !restarted.IsDemoted(StrategySegmented, cond) {
		t.Fatal("failed strategy was not demoted")
	}
	again := Open(dir, Scope{Tenant: "t", Owner: "owner", Bot: "bot-a"})
	best, ok := again.Best(cond)
	if !ok || best.Strategy == StrategySegmented {
		t.Fatalf("demotion did not survive reload: %+v", best)
	}
	shifted := cond
	shifted.LayoutScale = 150
	if _, ok := again.Best(shifted); ok {
		t.Fatal("a different layout scale reused the procedure")
	}
	raw := readStore(t, dir)
	if strings.Contains(raw, "hello") {
		t.Fatalf("typed text stored: %s", raw)
	}
}

func TestStoreConcurrentConfirmAndStripsForeignKeys(t *testing.T) {
	dir := t.TempDir()
	scope := Scope{Tenant: "t", Owner: "o", Bot: "b"}
	cond := Conditions{Environment: "linux", FieldClass: FieldPlain, LayoutScale: 100, Clipboard: true}
	store := Open(dir, scope)
	poison := []byte(`{"version":1,"procedures":[{"strategy":"clipboard_paste","environment":"linux","field_class":"plain","layout_scale":100,"needs_clipboard":true,"provenance":"exact_readback","successes":1,"confidence":1,"updated":1,"draft":"给张三","recipient":"zhang","cookie":"a=b","grants_approval":true,"x":12,"y":40}]}`)
	path := store.path
	if err := writeFile(path, poison); err != nil {
		t.Fatal(err)
	}
	if err := store.Rewrite(); err != nil {
		t.Fatal(err)
	}
	raw := readStore(t, dir)
	for _, leaked := range []string{"给张三", "zhang", "cookie", "grants_approval", "\"x\"", "\"y\""} {
		if strings.Contains(raw, leaked) {
			t.Fatalf("kept %s in %s", leaked, raw)
		}
	}
	done := make(chan struct{})
	for i := 0; i < 2; i++ {
		go func() {
			s := Open(dir, scope)
			for n := 0; n < 20; n++ {
				s.Confirm(StrategyClipboard, cond)
			}
			done <- struct{}{}
		}()
	}
	<-done
	<-done
	got, ok := Open(dir, scope).Best(cond)
	if !ok || got.Successes != 41 {
		t.Fatalf("successes=%d ok=%v", got.Successes, ok)
	}
}

func writeFile(path string, raw []byte) error {
	return osWrite(path, raw)
}
