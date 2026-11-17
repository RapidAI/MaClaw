package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
	"github.com/RapidAI/CodeClaw/corelib/desktop"
	"github.com/RapidAI/CodeClaw/corelib/desktop/computeruse"
	"github.com/RapidAI/CodeClaw/corelib/desktop/inputcorrect"
)

// Exact input correction runs inside the existing verify-after-act path.
// The desktop control service and the browser session are relays: they
// observe and perform one allow-listed action. The strategy, the comparison,
// and the stored procedure stay in this process.
//
// The follow-up program is docs/desktop-bot-improvement-program.md.

const (
	desktopExactInputMarker      = "(read back: exact text match; input verified; send, publish, and payment are not verified)"
	desktopInputUnverifiedMarker = "(read back denied or incomplete: unverified, not a success)"
	desktopInputUnsettledMarker  = "(editor or upload has not settled: unverified, not a success)"
	desktopInputSecretMarker     = "(a secret field was not read back: unverified, not a success)"
)

// desktopInputProcedureRoot overrides the procedure directory in tests.
var desktopInputProcedureRoot string

func desktopInputStore(scope agentruntime.Scope) *inputcorrect.Store {
	root := desktopInputProcedureRoot
	if root == "" {
		root = filepath.Join(corelib.MaclawDataDir(), "desktop-input-procedures")
	}
	owner, bot := desktop.SplitDesktopUser(scope.UserID)
	if strings.TrimSpace(bot) == "" {
		bot = strings.TrimSpace(scope.InstanceID)
	}
	return inputcorrect.Open(root, inputcorrect.Scope{
		Tenant: strings.TrimSpace(scope.TenantID),
		Owner:  strings.TrimSpace(owner),
		Bot:    strings.TrimSpace(bot),
	})
}

func desktopInsertedStrategy(raw any) string {
	steps, err := computeruse.ParseSteps(raw)
	if err != nil {
		return ""
	}
	last := ""
	for _, step := range steps {
		switch step.Kind {
		case "paste":
			last = inputcorrect.StrategyClipboard
		case "set_text":
			last = inputcorrect.StrategySemantic
		case "type":
			last = inputcorrect.StrategySegmented
		}
	}
	return last
}

func desktopCorrectDesktopSteps(ctx context.Context, scope agentruntime.Scope, display string, raw any, base string, err error) (string, error, bool) {
	inserted := desktopStepsInsertedText(raw)
	if strings.TrimSpace(inserted) == "" || (err != nil && !inputcorrect.RecoverableInputError(err)) {
		return "", nil, false
	}
	return desktopApplyInputCorrection(ctx, scope, desktopNativeInputRelay{scope: scope, display: display}, inputcorrect.Request{
		Expected:     inserted,
		Effect:       inputcorrect.EffectInput,
		Environment:  runtime.GOOS,
		UsedStrategy: desktopInsertedStrategy(raw),
	}, base, err)
}

func desktopCorrectBrowserType(ctx context.Context, scope agentruntime.Scope, session *browser.BrowserAgentSession, typed, base string, err error) (string, error, bool) {
	if strings.TrimSpace(typed) == "" || (err != nil && !inputcorrect.RecoverableInputError(err)) {
		return "", nil, false
	}
	return desktopApplyInputCorrection(ctx, scope, desktopBrowserInputRelay{session: session}, inputcorrect.Request{
		Expected:     typed,
		Effect:       inputcorrect.EffectInput,
		Environment:  runtime.GOOS,
		UsedStrategy: inputcorrect.StrategySemantic,
	}, base, err)
}

func desktopApplyInputCorrection(ctx context.Context, scope agentruntime.Scope, relay inputcorrect.Relay, req inputcorrect.Request, base string, prior error) (string, error, bool) {
	out := inputcorrect.Run(ctx, req, relay, desktopInputStore(scope))
	log.Printf("[desktop-input] status=%s strategy=%s reason=%s attempts=%d images=%d", out.Status, out.Strategy, out.Reason, out.Attempts, out.Images)
	if out.Status == inputcorrect.StatusSkipped {
		return "", nil, false
	}
	text, oerr := desktopCorrectionResult(base, out)
	if prior != nil && (oerr != nil || !strings.Contains(text, desktopExactInputMarker)) {
		return base, prior, true
	}
	return text, oerr, true
}

func desktopCorrectionResult(base string, out inputcorrect.Outcome) (string, error) {
	switch out.Status {
	case inputcorrect.StatusVerified:
		return strings.TrimRight(base, "\n") + "\n" + desktopExactInputMarker + "\n" + desktopAppliedMarker, nil
	case inputcorrect.StatusUnverified:
		if out.Reason == "secret_not_read" {
			return desktopInputSecretMarker, nil
		}
		marker := desktopInputUnverifiedMarker
		if out.Reason == "editor_unsettled" {
			marker = desktopInputUnsettledMarker
		}
		return strings.TrimRight(base, "\n") + "\n" + marker, nil
	case inputcorrect.StatusFailed:
		return strings.TrimRight(base, "\n") + "\n" + desktopNotAppliedMarker + "\n" + out.Reason, nil
	case inputcorrect.StatusRefused:
		return "", fmt.Errorf("refused: %s", out.Reason)
	default:
		return base, nil
	}
}

type desktopNativeInputRelay struct {
	scope   agentruntime.Scope
	display string
}

func (r desktopNativeInputRelay) Observe(ctx context.Context) (inputcorrect.Observation, error) {
	ax, err := desktopCtlCall(ctx, r.scope, r.display, map[string]any{"op": desktop.CtlA11yProbe, "focused_text": true})
	if err != nil || ax == nil {
		return inputcorrect.Observation{Unavailable: true}, nil
	}
	if ok, _ := ax["ok"].(bool); !ok {
		return inputcorrect.Observation{Unavailable: true}, nil
	}
	screen, _ := desktopCtlCall(ctx, r.scope, r.display, map[string]any{"op": desktop.CtlScreen})
	wins, _ := desktopCtlCall(ctx, r.scope, r.display, map[string]any{"op": desktop.CtlWindows})
	return desktopObservationFromCtl(ax, screen, wins), nil
}

func (r desktopNativeInputRelay) Act(ctx context.Context, action inputcorrect.Action) error {
	if !inputcorrect.AllowedKind(action.Kind) || inputcorrect.ExternalEffect(action.Kind) {
		return fmt.Errorf("refused action")
	}
	switch action.Kind {
	case inputcorrect.ActionClearField:
		return r.keys(ctx, "ctrl+a", "Delete")
	case inputcorrect.StrategySemantic:
		if action.ElementID <= 0 {
			return fmt.Errorf("semantic input needs an element id")
		}
		_, err := desktopCtlCall(ctx, r.scope, r.display, map[string]any{
			"op": desktop.CtlA11yAct, "id": action.ElementID, "do": "set_text", "text": action.Text,
		})
		return err
	case inputcorrect.StrategyClipboard:
		if _, err := desktopCtlCall(ctx, r.scope, r.display, map[string]any{"op": desktop.CtlClipboardSet, "text": action.Text}); err != nil {
			return err
		}
		return r.keys(ctx, "ctrl+v")
	case inputcorrect.StrategySegmented:
		for _, chunk := range chunkRunes(action.Text, 32) {
			argv, err := computeruse.StepArgv(computeruse.Step{Kind: "type", Text: chunk}, desktop.Shot{Scale: 1})
			if err != nil {
				return err
			}
			if err := desktopDispatchApp(ctx, r.scope, r.display, argv); err != nil {
				return err
			}
		}
		return nil
	case inputcorrect.StrategyEditorReplace:
		return fmt.Errorf("editor replace is not available from desktop control")
	default:
		return fmt.Errorf("refused action")
	}
}

func (r desktopNativeInputRelay) keys(ctx context.Context, chords ...string) error {
	for _, chord := range chords {
		combo, err := computeruse.KeyCombo([]string{chord})
		if err != nil {
			return err
		}
		argv, err := computeruse.StepArgv(computeruse.Step{Kind: "key", Keys: []string{combo}}, desktop.Shot{Scale: 1})
		if err != nil {
			return err
		}
		if err := desktopDispatchApp(ctx, r.scope, r.display, argv); err != nil {
			return err
		}
	}
	return nil
}

func desktopDispatchApp(ctx context.Context, scope agentruntime.Scope, display string, argv []string) error {
	if desktopRemoteApp != nil {
		_, err := desktopRemoteApp(ctx, scope.TenantID, scope.UserID, display, argv)
		return err
	}
	if client := desktopHubClientFromEnv(); client != nil {
		_, err := client.App(ctx, scope.TenantID, scope.UserID, display, argv)
		return err
	}
	if err := refuseLocalDesktop(ctx); err != nil {
		return err
	}
	_, err := desktopAppCommand(display, argv...)
	return err
}

func desktopObservationFromCtl(ax, screen, wins map[string]any) inputcorrect.Observation {
	obs := inputcorrect.Observation{
		FrameID:     desktopActiveWindowClass(wins),
		LayoutScale: desktopLayoutScale(screen),
		RefEpoch:    ctlInt(ax["ref_epoch"]),
		ImageCount:  ctlInt(ax["images"]),
		Capability: inputcorrect.Capability{
			Clipboard:     true,
			Keys:          true,
			EditorReplace: ctlBool(ax["editor_replace"]),
		},
	}
	role := strings.TrimSpace(ctlString(ax["role"]))
	if ctlBool(ax["secret"]) || strings.EqualFold(role, "password text") {
		obs.Secret = true
		obs.ReadbackDenied = true
		return obs
	}
	if ctlString(ax["readback"]) == "denied" {
		obs.ReadbackDenied = true
		return obs
	}
	if texts, ok := ax["focused_texts"].([]any); ok && len(texts) != 1 {
		obs.Identified = false
		obs.ElementKey = "ambiguous"
		obs.TextTrusted = true
		return obs
	}
	text := ctlString(ax["focused_text"])
	if strings.Contains(text, "\u241e") {
		obs.Clipped = true
		return obs
	}
	name := strings.TrimSpace(ctlString(ax["name"]))
	if role == "" {
		role = "field"
	}
	obs.ElementKey = role + ":" + name
	obs.ElementID = ctlInt(ax["id"])
	obs.Identified = role != ""
	obs.FieldText = text
	obs.TextTrusted = true
	obs.EdgeStripped = true
	obs.UploadPending = ctlBool(ax["upload_pending"])
	obs.EditorBusy = ctlBool(ax["editor_busy"])
	obs.Capability.Semantic = obs.ElementID > 0 && obs.ImageCount == 0
	return obs
}

func desktopActiveWindowClass(wins map[string]any) string {
	raw, _ := wins["windows"].([]any)
	for _, item := range raw {
		w, _ := item.(map[string]any)
		if !ctlBool(w["active"]) {
			continue
		}
		return strings.TrimSpace(ctlString(w["class"]))
	}
	return ""
}

func desktopLayoutScale(screen map[string]any) int {
	switch n := screen["scale"].(type) {
	case float64:
		if n <= 0 {
			return 0
		}
		return int(math.Round(n * 100))
	default:
		return 0
	}
}

func ctlString(v any) string {
	s, _ := v.(string)
	return s
}

func ctlBool(v any) bool {
	b, _ := v.(bool)
	return b
}

func ctlInt(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	default:
		return 0
	}
}

func chunkRunes(s string, n int) []string {
	if n < 1 {
		n = 32
	}
	rs := []rune(s)
	var out []string
	for len(rs) > 0 {
		if len(rs) <= n {
			out = append(out, string(rs))
			break
		}
		out = append(out, string(rs[:n]))
		rs = rs[n:]
	}
	return out
}

// desktopPageEval is the browser relay. Tests replace it; production reads
// the attached page through the session's existing CDP evaluate.
var desktopPageEval = func(session *browser.BrowserAgentSession, expr string) (string, error) {
	if session == nil {
		return "", fmt.Errorf("no browser session")
	}
	return session.EvalValue(expr)
}

type desktopBrowserInputRelay struct {
	session *browser.BrowserAgentSession
}

func (r desktopBrowserInputRelay) Observe(context.Context) (inputcorrect.Observation, error) {
	raw, err := desktopPageEval(r.session, desktopInputObserveExpr)
	if err != nil {
		return inputcorrect.Observation{Unavailable: true}, nil
	}
	var page desktopPageField
	if json.Unmarshal([]byte(raw), &page) != nil {
		return inputcorrect.Observation{Unavailable: true}, nil
	}
	if page.Secret {
		return inputcorrect.Observation{Secret: true, ReadbackDenied: true}, nil
	}
	obs := inputcorrect.Observation{
		FrameID:        page.FrameID,
		RefEpoch:       page.RefEpoch,
		LayoutScale:    page.LayoutScale,
		ElementID:      page.ElementID,
		ElementKey:     page.ElementKey,
		Identified:     page.Identified,
		FieldText:      page.Text,
		TextTrusted:    page.Trusted && !page.Denied && !page.Clipped,
		ReadbackDenied: page.Denied,
		Clipped:        page.Clipped,
		ImageCount:     page.Images,
		UploadPending:  page.UploadPending,
		EditorBusy:     page.EditorBusy,
		Capability: inputcorrect.Capability{
			Semantic:      page.Semantic && page.ElementID > 0 && page.Images == 0,
			Clipboard:     page.Clipboard,
			Keys:          page.Keys,
			EditorReplace: page.EditorReplace,
		},
	}
	if page.Denied || page.Clipped {
		obs.FieldText = ""
	}
	return obs, nil
}

func (r desktopBrowserInputRelay) Act(_ context.Context, action inputcorrect.Action) error {
	if !inputcorrect.AllowedKind(action.Kind) || inputcorrect.ExternalEffect(action.Kind) {
		return fmt.Errorf("refused action")
	}
	raw, err := desktopPageEval(r.session, desktopInputActExpr(action.Kind, action.Text, action.ElementID))
	if err != nil {
		return err
	}
	var res struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	if json.Unmarshal([]byte(raw), &res) != nil || !res.OK {
		if res.Error == "" {
			res.Error = "input action failed"
		}
		return fmt.Errorf("%s", res.Error)
	}
	return nil
}

type desktopPageField struct {
	Text          string `json:"text"`
	Trusted       bool   `json:"trusted"`
	Denied        bool   `json:"denied"`
	Secret        bool   `json:"secret"`
	Clipped       bool   `json:"clipped"`
	Images        int    `json:"images"`
	UploadPending bool   `json:"upload_pending"`
	EditorBusy    bool   `json:"editor_busy"`
	ElementKey    string `json:"element_key"`
	Identified    bool   `json:"identified"`
	ElementID     int    `json:"element_id"`
	FrameID       string `json:"frame_id"`
	RefEpoch      int    `json:"ref_epoch"`
	LayoutScale   int    `json:"layout_scale"`
	Semantic      bool   `json:"semantic"`
	Clipboard     bool   `json:"clipboard"`
	Keys          bool   `json:"keys"`
	EditorReplace bool   `json:"editor_replace"`
}
