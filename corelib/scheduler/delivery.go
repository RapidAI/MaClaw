package scheduler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// Delivery channel identifiers.
const (
	DeliveryChannelLansenger = "lansenger"
	DeliveryChannelWeixin    = "weixin"
	DeliveryChannelTelegram  = "telegram"
	DeliveryChannelQQ        = "qq"
)

// Delivery target kinds.
const (
	DeliveryKindGroup = "group" // channel group chat
	DeliveryKindUser  = "user"  // private / direct message
)

// When to push the agent result.
const (
	DeliveryOnSuccess = "success"    // default: only on successful run with non-empty result
	DeliveryOnAlways  = "always"     // success or failure (if text available)
	DeliveryOnError   = "error_only" // only when the run fails
)

// TaskDelivery is optional push configuration for a scheduled task result.
// Empty / disabled means legacy behaviour (desktop notification / owner proactive only).
type TaskDelivery struct {
	Enabled bool   `json:"enabled"`
	Channel string `json:"channel,omitempty"` // e.g. "lansenger", "weixin"
	// BotProfileID identifies the Lansenger bot that owns this delivery. It is
	// runtime-injected by a profile-bound IM handler, never supplied by an LLM.
	// Empty preserves legacy/default-channel behaviour and is intentionally
	// omitted from old persisted task files.
	BotProfileID string           `json:"bot_profile_id,omitempty"`
	Targets      []DeliveryTarget `json:"targets,omitempty"`
	On           string           `json:"on,omitempty"` // success | always | error_only
	// Prefix is prepended to the body (task name is applied by the sender if empty).
	Prefix string `json:"prefix,omitempty"`
	// FailOnError when true makes delivery failures fail the scheduled task
	// (LastError). Default false: agent work success is preserved; delivery
	// issues are logged and optionally annotated onto the result text.
	FailOnError bool `json:"fail_on_error,omitempty"`
}

// DeliveryTarget is one destination under a channel.
type DeliveryTarget struct {
	// Kind is "group" or "user".
	Kind string `json:"kind"`
	// GroupID is required when Kind is group.
	GroupID string `json:"group_id,omitempty"`
	// GroupName is optional display metadata (not used for routing).
	GroupName string `json:"group_name,omitempty"`
	// UserID is the private recipient when Kind is user (e.g. Lansenger staffId).
	UserID string `json:"user_id,omitempty"`
	// MentionUserIDs optionally @-mentions people in a group message (Lansenger Reminder).
	MentionUserIDs []string `json:"mention_user_ids,omitempty"`
	// MentionAll @-all in a group message when true.
	MentionAll bool `json:"mention_all,omitempty"`
}

// Active reports whether delivery should be attempted (enabled with at least one target).
func (d *TaskDelivery) Active() bool {
	if d == nil || !d.Enabled {
		return false
	}
	return len(d.Targets) > 0
}

// PeerIDFromTarget returns the primary routing id for memory / self resolution.
func PeerIDFromTarget(t DeliveryTarget) string {
	if id := strings.TrimSpace(t.UserID); id != "" {
		return id
	}
	return strings.TrimSpace(t.GroupID)
}

// CanRememberAsSelfPeer reports whether a successful target should update
// DeliveryStateStore last-peer memory used by user_id=self.
// Group destinations must never be remembered — a group_id would break
// subsequent private self resolution on the same channel.
func CanRememberAsSelfPeer(t DeliveryTarget) bool {
	kind := strings.TrimSpace(strings.ToLower(t.Kind))
	switch kind {
	case DeliveryKindGroup:
		return false
	case DeliveryKindUser:
		return true
	default:
		// Empty/unknown kind: only when an explicit user_id is present.
		return strings.TrimSpace(t.UserID) != ""
	}
}

// CloneTaskDelivery returns a deep copy of d (including targets/mentions).
// Safe on nil. Used when firing tasks so Normalize/ShouldDeliver cannot mutate
// the persisted task config under concurrent reads/updates.
func CloneTaskDelivery(d *TaskDelivery) *TaskDelivery {
	if d == nil {
		return nil
	}
	cp := *d
	if len(d.Targets) > 0 {
		cp.Targets = make([]DeliveryTarget, len(d.Targets))
		for i, t := range d.Targets {
			cp.Targets[i] = t
			if len(t.MentionUserIDs) > 0 {
				cp.Targets[i].MentionUserIDs = append([]string(nil), t.MentionUserIDs...)
			}
		}
	}
	return &cp
}

// Normalize fills defaults and trims strings. Safe on nil.
func (d *TaskDelivery) Normalize() {
	if d == nil {
		return
	}
	// Canonicalize aliases (蓝信/微信/wechat/…) before empty-default.
	d.Channel = DefaultDeliveryChannel(d.Channel)
	d.BotProfileID = strings.TrimSpace(d.BotProfileID)
	on := strings.TrimSpace(strings.ToLower(d.On))
	switch on {
	case DeliveryOnAlways, DeliveryOnError, DeliveryOnSuccess:
		d.On = on
	default:
		d.On = DeliveryOnSuccess
	}
	d.Prefix = strings.TrimSpace(d.Prefix)

	out := make([]DeliveryTarget, 0, len(d.Targets))
	for _, t := range d.Targets {
		t.Kind = strings.TrimSpace(strings.ToLower(t.Kind))
		t.GroupID = strings.TrimSpace(t.GroupID)
		t.GroupName = strings.TrimSpace(t.GroupName)
		t.UserID = strings.TrimSpace(t.UserID)
		mentions := make([]string, 0, len(t.MentionUserIDs))
		seen := map[string]struct{}{}
		for _, id := range t.MentionUserIDs {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			mentions = append(mentions, id)
		}
		t.MentionUserIDs = mentions
		if t.Kind == "" {
			if t.GroupID != "" || t.GroupName != "" {
				t.Kind = DeliveryKindGroup
			} else if t.UserID != "" {
				t.Kind = DeliveryKindUser
			}
		}
		// Drop completely empty target slots (agent noise).
		if t.Kind == "" && t.GroupID == "" && t.GroupName == "" && t.UserID == "" {
			continue
		}
		out = append(out, t)
	}
	d.Targets = out
}

// Validate checks a delivery config. Disabled / empty is always valid.
// Group targets may temporarily have only group_name (agent path before resolve);
// call EnsureResolved / ResolveDeliveryGroupNames before Manager.Add/Update persist.
// Channel is not hard-whitelisted here so new IM platforms only need a runtime catalog/sender.
func (d *TaskDelivery) Validate() error {
	if d == nil || !d.Enabled {
		return nil
	}
	d.Normalize()
	if d.Channel == "" {
		return fmt.Errorf("scheduler: delivery channel is required")
	}
	if len(d.Targets) == 0 {
		return fmt.Errorf("scheduler: delivery enabled but no targets")
	}
	for i, t := range d.Targets {
		switch t.Kind {
		case DeliveryKindGroup:
			if t.GroupID == "" && t.GroupName == "" {
				return fmt.Errorf("scheduler: delivery targets[%d]: group_id or group_name is required for kind=group", i)
			}
		case DeliveryKindUser:
			if t.UserID == "" {
				return fmt.Errorf("scheduler: delivery targets[%d]: user_id is required for kind=user", i)
			}
		default:
			return fmt.Errorf("scheduler: delivery targets[%d]: kind must be %q or %q", i, DeliveryKindGroup, DeliveryKindUser)
		}
	}
	return nil
}

// EnsureResolved fails if any group target still lacks group_id (name not resolved).
func (d *TaskDelivery) EnsureResolved() error {
	if d == nil || !d.Enabled {
		return nil
	}
	d.Normalize()
	for i, t := range d.Targets {
		if t.Kind == DeliveryKindGroup && t.GroupID == "" {
			name := t.GroupName
			if name == "" {
				name = "(unknown)"
			}
			return fmt.Errorf("scheduler: delivery targets[%d]: group %q has no group_id — resolve group name first (im_message/manage_schedule action=list_targets)", i, name)
		}
	}
	return nil
}

// NeedsGroupNameResolution reports whether any group target needs catalog lookup.
func (d *TaskDelivery) NeedsGroupNameResolution() bool {
	if d == nil || !d.Enabled {
		return false
	}
	for _, t := range d.Targets {
		if t.Kind == DeliveryKindGroup && strings.TrimSpace(t.GroupID) == "" && strings.TrimSpace(t.GroupName) != "" {
			return true
		}
	}
	return false
}

// MaxDeliveryBodyRunes caps outbound IM text (most bots chunk or reject very long posts).
const MaxDeliveryBodyRunes = 3500

// TruncateDeliveryBody trims and caps text for proactive / scheduled IM delivery.
func TruncateDeliveryBody(s string) string {
	return TruncateStr(strings.TrimSpace(s), MaxDeliveryBodyRunes)
}

// ShouldDeliver decides whether to push based on run outcome.
// Read-only: does not mutate d (avoids touching persisted config via shared pointers).
func (d *TaskDelivery) ShouldDeliver(runErr error, resultText string) bool {
	if !d.Active() {
		return false
	}
	// Local On normalize — same defaults as Normalize, without rewriting d.
	on := strings.TrimSpace(strings.ToLower(d.On))
	hasText := strings.TrimSpace(resultText) != ""
	switch on {
	case DeliveryOnAlways:
		// Always push when configured; FormatBody supplies a placeholder if empty.
		return true
	case DeliveryOnError:
		return runErr != nil
	default: // success (including empty/unknown On)
		if runErr == nil && hasText {
			return true
		}
		// Timeout/cancel with partial agent output: still push what we have
		// (useful for long process-type jobs that filled some content).
		if hasText && isAbortError(runErr) {
			return true
		}
		return false
	}
}

func isAbortError(err error) bool {
	if err == nil {
		return false
	}
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}

// AnnotateRunErrWithContext folds scheduler timeout/cancel into runErr so
// ShouldDeliver can still push partial agent text when the agent also returned
// a string error (fmt.Errorf("%s", ...) does not wrap context errors).
func AnnotateRunErrWithContext(ctx context.Context, runErr error) error {
	if ctx == nil {
		return runErr
	}
	cerr := ctx.Err()
	if cerr == nil {
		return runErr
	}
	if runErr == nil {
		return cerr
	}
	if isAbortError(runErr) {
		return runErr
	}
	return fmt.Errorf("%w: %v", cerr, runErr)
}

// FormatBody builds the message body for delivery.
func (d *TaskDelivery) FormatBody(taskName, resultText string, runErr error) string {
	name := strings.TrimSpace(taskName)
	body := strings.TrimSpace(resultText)
	var b strings.Builder
	if d != nil {
		if p := strings.TrimSpace(d.Prefix); p != "" {
			b.WriteString(p)
			if !strings.HasSuffix(p, "\n") {
				b.WriteString("\n")
			}
		}
	}
	if name != "" {
		b.WriteString("【定时任务】")
		b.WriteString(name)
		b.WriteString("\n")
	}
	if runErr != nil {
		if isAbortError(runErr) {
			b.WriteString("状态: 超时/中断\n")
		} else {
			b.WriteString("状态: 失败\n")
		}
		if body != "" {
			b.WriteString(body)
			b.WriteString("\n")
		}
		b.WriteString("错误: ")
		b.WriteString(runErr.Error())
	} else {
		if body == "" {
			body = "(无输出)"
		}
		b.WriteString(body)
	}
	return TruncateDeliveryBody(b.String())
}

// ParseDeliveryFromAny converts JSON-decoded values (map / []byte / string / TaskDelivery) into TaskDelivery.
// Returns (nil, nil) when v is nil — caller may clear delivery.
//
// Agent convenience: when targets are present but "enabled" is omitted, delivery
// is treated as enabled. Explicit enabled:false still clears/disables.
func ParseDeliveryFromAny(v interface{}) (*TaskDelivery, error) {
	if v == nil {
		return nil, nil
	}
	switch x := v.(type) {
	case *TaskDelivery:
		if x == nil {
			return nil, nil
		}
		cp := *x
		// Typed values already chose Enabled; do not re-enable.
		return finalizeParsedDelivery(&cp, false)
	case TaskDelivery:
		cp := x
		return finalizeParsedDelivery(&cp, false)
	case string:
		s := strings.TrimSpace(x)
		if s == "" || s == "null" {
			return nil, nil
		}
		var d TaskDelivery
		if err := json.Unmarshal([]byte(s), &d); err != nil {
			return nil, fmt.Errorf("scheduler: invalid delivery json: %w", err)
		}
		// JSON string: if "enabled" key missing, auto-enable when targets exist.
		return finalizeParsedDelivery(&d, !jsonObjectHasEnabledKey([]byte(s)))
	case []byte:
		if len(x) == 0 {
			return nil, nil
		}
		var d TaskDelivery
		if err := json.Unmarshal(x, &d); err != nil {
			return nil, fmt.Errorf("scheduler: invalid delivery json: %w", err)
		}
		return finalizeParsedDelivery(&d, !jsonObjectHasEnabledKey(x))
	case map[string]interface{}:
		_, hasEnabled := x["enabled"]
		raw, err := json.Marshal(x)
		if err != nil {
			return nil, fmt.Errorf("scheduler: marshal delivery: %w", err)
		}
		var d TaskDelivery
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("scheduler: invalid delivery object: %w", err)
		}
		return finalizeParsedDelivery(&d, !hasEnabled)
	default:
		raw, err := json.Marshal(x)
		if err != nil {
			return nil, fmt.Errorf("scheduler: unsupported delivery type %T", v)
		}
		var d TaskDelivery
		if err := json.Unmarshal(raw, &d); err != nil {
			return nil, fmt.Errorf("scheduler: invalid delivery value: %w", err)
		}
		return finalizeParsedDelivery(&d, !jsonObjectHasEnabledKey(raw))
	}
}

func jsonObjectHasEnabledKey(raw []byte) bool {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	_, ok := probe["enabled"]
	return ok
}

func finalizeParsedDelivery(d *TaskDelivery, autoEnableIfTargets bool) (*TaskDelivery, error) {
	if d == nil {
		return nil, nil
	}
	d.Normalize()
	if autoEnableIfTargets && !d.Enabled && len(d.Targets) > 0 {
		d.Enabled = true
	}
	if err := d.Validate(); err != nil {
		return nil, err
	}
	if !d.Enabled {
		return nil, nil
	}
	return d, nil
}

// DeliveryWarningPrefix marks soft delivery failures embedded in LastResult.
const DeliveryWarningPrefix = "[投递警告] "

// maxLastResultRunes caps persisted LastResult (UI + JSON size).
const maxLastResultRunes = 500

// HasDeliveryWarning reports whether text already contains a soft delivery note.
// Matches both the canonical prefix ("[投递警告] ") and a bare marker for UI/legacy.
func HasDeliveryWarning(text string) bool {
	return strings.Contains(text, DeliveryWarningPrefix) || strings.Contains(text, "[投递警告]")
}

// TruncateLastResult caps LastResult for storage while preserving a trailing
// soft delivery warning when present (so UI badges / HasDeliveryWarning keep working).
func TruncateLastResult(result string) string {
	return TruncatePreservingDeliveryWarning(result, maxLastResultRunes)
}

// TruncatePreservingDeliveryWarning shortens result to at most max runes while
// keeping a trailing soft delivery warning when present.
func TruncatePreservingDeliveryWarning(result string, max int) string {
	if result == "" || max <= 0 {
		return ""
	}
	if !HasDeliveryWarning(result) {
		return truncateRunes(result, max)
	}
	idx := strings.LastIndex(result, "[投递警告]")
	if idx < 0 {
		return truncateRunes(result, max)
	}
	warn := strings.TrimSpace(result[idx:])
	warnRunes := []rune(warn)
	if len(warnRunes) >= max {
		// Prefer keeping the warning marker over agent body.
		return string(warnRunes[:max])
	}
	body := strings.TrimSpace(result[:idx])
	// Reserve room for "\n\n" + warning.
	budget := max - len(warnRunes) - 2
	if budget <= 0 {
		return string(warnRunes)
	}
	body = truncateRunes(body, budget)
	if body == "" {
		return warn
	}
	return body + "\n\n" + warn
}

// truncateRunes shortens s to at most max runes, appending "..." when truncated
// such that the result length never exceeds max.
func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 3 {
		return string(r[:max])
	}
	return string(r[:max-3]) + "..."
}

// MergeDeliveryOutcome combines agent runErr with delivery err according to policy.
// When FailOnError is false (default) and the agent succeeded, delivery errors are
// soft: they annotate resultText and do not fail the task.
func MergeDeliveryOutcome(d *TaskDelivery, resultText string, runErr, delErr error) (string, error) {
	if delErr == nil {
		return resultText, runErr
	}
	strict := d != nil && d.FailOnError
	if runErr != nil {
		// Keep runErr in the wrap chain (timeout/cancel detection via errors.Is).
		return resultText, fmt.Errorf("%w; %v", runErr, delErr)
	}
	if strict {
		return resultText, fmt.Errorf("agent ok; %w", delErr)
	}
	// Soft: keep task success, surface warning in the stored result (once).
	if HasDeliveryWarning(resultText) {
		return resultText, nil
	}
	note := DeliveryWarningPrefix + delErr.Error()
	if strings.TrimSpace(resultText) == "" {
		return note, nil
	}
	return strings.TrimSpace(resultText) + "\n\n" + note, nil
}

// FanOutDeliveryTargets runs fn for each target, collecting partial failures.
// okCount is how many targets succeeded.
func FanOutDeliveryTargets(targets []DeliveryTarget, fn func(i int, t DeliveryTarget) error) (okCount int, err error) {
	if len(targets) == 0 {
		return 0, nil
	}
	var failures []string
	for i, t := range targets {
		if e := fn(i, t); e != nil {
			failures = append(failures, fmt.Sprintf("targets[%d]: %v", i, e))
			continue
		}
		okCount++
	}
	if len(failures) == 0 {
		return okCount, nil
	}
	if okCount > 0 {
		return okCount, fmt.Errorf("delivery partial (%d ok, %d failed): %s", okCount, len(failures), strings.Join(failures, "; "))
	}
	return 0, fmt.Errorf("delivery: %s", strings.Join(failures, "; "))
}

// SummarizeDelivery returns a short human-readable delivery description for lists/tools.
// Does not mutate d (normalizes a clone).
func SummarizeDelivery(d *TaskDelivery) string {
	if d == nil || !d.Enabled {
		return ""
	}
	snap := CloneTaskDelivery(d)
	snap.Normalize()
	if len(snap.Targets) == 0 {
		return snap.Channel + " (no targets)"
	}
	parts := make([]string, 0, len(snap.Targets))
	for _, t := range snap.Targets {
		switch t.Kind {
		case DeliveryKindGroup:
			label := t.GroupName
			if label == "" {
				label = t.GroupID
			}
			s := "群:" + label
			if t.MentionAll {
				s += " @all"
			} else if len(t.MentionUserIDs) > 0 {
				s += fmt.Sprintf(" @%d人", len(t.MentionUserIDs))
			}
			parts = append(parts, s)
		case DeliveryKindUser:
			parts = append(parts, "私聊:"+t.UserID)
		}
	}
	return snap.Channel + " → " + strings.Join(parts, ", ")
}
