package inputcorrect

import (
	"context"
	"strings"
)

// Strategies are general input methods. A strategy is not a site rule, a
// coordinate, or a shell command.
const (
	StrategySemantic      = "semantic_set"
	StrategyClipboard     = "clipboard_paste"
	StrategySegmented     = "segmented_keys"
	StrategyEditorReplace = "editor_replace"
	ActionClearField      = "clear_field"
)

// Effects separate typing from something that leaves the machine.
const (
	EffectInput   = "input"
	EffectSend    = "send"
	EffectPublish = "publish"
	EffectPay     = "pay"
)

// Outcomes of one correction.
const (
	StatusSkipped    = "skipped"
	StatusVerified   = "verified"
	StatusUnverified = "unverified"
	StatusFailed     = "failed"
	StatusRefused    = "refused"
)

// Field classes. A rich field already holds non-text content.
const (
	FieldPlain = "plain"
	FieldRich  = "rich"
)

// Capability is what the desktop relay can do on this observation.
// The relay reports it. The model does not.
type Capability struct {
	Semantic      bool
	Clipboard     bool
	Keys          bool
	EditorReplace bool
}

// Observation is one trusted read inside an isolated bot desktop. Nobody
// else is using that keyboard. A mismatch is the wrong element, a stale
// frame, or an editor that has not settled — not a person taking the
// foreground. Coordinates, window titles, and secrets are not included.
type Observation struct {
	Unavailable    bool
	FrameID        string
	RefEpoch       int
	LayoutScale    int
	ElementID      int
	ElementKey     string
	Identified     bool
	FieldText      string
	TextTrusted    bool
	ReadbackDenied bool
	Secret         bool
	Clipped        bool
	EdgeStripped   bool
	ImageCount     int
	UploadPending  bool
	EditorBusy     bool
	Capability     Capability
}

// Action is one relay step. Kind is allow-listed. Text is the ordinary
// field contents for this attempt and is not stored.
type Action struct {
	Kind        string
	Text        string
	ElementID   int
	ElementKey  string
	FrameID     string
	RefEpoch    int
	LayoutScale int
}

// Request is the server's decision input.
type Request struct {
	Expected    string
	Secret      bool
	Effect      string
	Environment string
	// FrameID and ElementKey are the surface and field this attempt is
	// for. Empty means the loop must still see one stable identified field.
	FrameID    string
	ElementKey string
	// MinImages is an upload postcondition. Zero does not wait for images.
	MinImages int
	// UsedStrategy is the method that already ran, when this call is the
	// read-back of that method. It is recorded only after an exact match.
	UsedStrategy string
}

// Outcome is the decision. EffectVerified is always false: matching the
// field is not proof that a message was sent, a page was published, or a
// payment was made.
type Outcome struct {
	Status         string
	InputVerified  bool
	EffectVerified bool
	Escalated      bool
	Strategy       string
	Mismatch       string
	Attempts       int
	Reason         string
	Images         int
	EditorSettled  bool
}

// Relay is the desktop. It observes and performs one allow-listed action.
// It does not choose the strategy.
type Relay interface {
	Observe(ctx context.Context) (Observation, error)
	Act(ctx context.Context, action Action) error
}

// AllowedKind reports whether kind may be sent to the relay.
func AllowedKind(kind string) bool {
	switch kind {
	case StrategySemantic, StrategyClipboard, StrategySegmented, StrategyEditorReplace, ActionClearField:
		return true
	default:
		return false
	}
}

// ExternalEffect reports send, publish, or pay.
func ExternalEffect(kind string) bool {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case EffectSend, EffectPublish, EffectPay:
		return true
	default:
		return false
	}
}

// GrantsApproval is always false. A remembered input procedure does not
// authorize send, publish, or pay.
func GrantsApproval(Procedure) bool { return false }

// EffectDecision is whether an external effect may be issued again.
type EffectDecision struct {
	Replay bool
	Reason string
}

// DecideEffect refuses another send, publish, or pay. An ambiguous outcome
// is not replayed. A verified input procedure does not change that.
func DecideEffect(kind string, ambiguous bool, _ Procedure) EffectDecision {
	if !ExternalEffect(kind) {
		return EffectDecision{Replay: false, Reason: "not_an_external_effect"}
	}
	if ambiguous {
		return EffectDecision{Replay: false, Reason: "ambiguous_outcome_not_replayed"}
	}
	return EffectDecision{Replay: false, Reason: "remembered_input_does_not_authorize"}
}

// RecoverableInputError reports a stale element or frame the loop may
// read again. A nil error is recoverable because the tool's own "ok" is
// not the postcondition. foreground_unavailable is not: an isolated bot
// desktop has no competing pointer, and this loop does not treat that
// string as a reason to wait or to switch to a shell workaround.
func RecoverableInputError(err error) bool {
	if err == nil {
		return true
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "foreground_unavailable") {
		return false
	}
	return strings.Contains(msg, "stale") || strings.Contains(msg, "target closed") || strings.Contains(msg, "no such target")
}
