package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestTaskDeliveryValidate(t *testing.T) {
	t.Parallel()
	if err := (*TaskDelivery)(nil).Validate(); err != nil {
		t.Fatalf("nil validate: %v", err)
	}
	disabled := &TaskDelivery{Enabled: false}
	if err := disabled.Validate(); err != nil {
		t.Fatalf("disabled: %v", err)
	}
	bad := &TaskDelivery{Enabled: true, Channel: "lansenger"}
	if err := bad.Validate(); err == nil {
		t.Fatal("expected error for no targets")
	}
	groupMissing := &TaskDelivery{
		Enabled: true,
		Channel: DeliveryChannelLansenger,
		Targets: []DeliveryTarget{{Kind: DeliveryKindGroup}},
	}
	if err := groupMissing.Validate(); err == nil {
		t.Fatal("expected group_id required")
	}
	ok := &TaskDelivery{
		Enabled: true,
		Targets: []DeliveryTarget{
			{Kind: DeliveryKindGroup, GroupID: "g1", MentionUserIDs: []string{" a ", "a", "b"}},
			{Kind: DeliveryKindUser, UserID: "u1"},
		},
	}
	if err := ok.Validate(); err != nil {
		t.Fatalf("ok: %v", err)
	}
	if ok.Channel != DeliveryChannelLansenger {
		t.Fatalf("default channel = %q", ok.Channel)
	}
	if ok.On != DeliveryOnSuccess {
		t.Fatalf("default on = %q", ok.On)
	}
	if len(ok.Targets[0].MentionUserIDs) != 2 {
		t.Fatalf("mention dedupe: %#v", ok.Targets[0].MentionUserIDs)
	}
}

func TestShouldDeliver(t *testing.T) {
	t.Parallel()
	d := &TaskDelivery{
		Enabled: true,
		Channel: DeliveryChannelLansenger,
		Targets: []DeliveryTarget{{Kind: DeliveryKindGroup, GroupID: "g"}},
		On:      DeliveryOnSuccess,
	}
	if !d.ShouldDeliver(nil, "hello") {
		t.Fatal("success+text should deliver")
	}
	if d.ShouldDeliver(nil, "  ") {
		t.Fatal("empty result should not deliver on success")
	}
	if d.ShouldDeliver(errors.New("x"), "hello") {
		t.Fatal("error should not deliver on success mode")
	}
	if !d.ShouldDeliver(context.DeadlineExceeded, "partial") {
		t.Fatal("timeout with partial text should deliver")
	}
	if d.ShouldDeliver(context.DeadlineExceeded, "") {
		t.Fatal("timeout without text should not deliver on success")
	}
	d.On = DeliveryOnError
	if !d.ShouldDeliver(errors.New("x"), "") {
		t.Fatal("error_only should deliver on error")
	}
	if d.ShouldDeliver(nil, "ok") {
		t.Fatal("error_only should not deliver on success")
	}
	d.On = DeliveryOnAlways
	if !d.ShouldDeliver(errors.New("x"), "") {
		t.Fatal("always should deliver on error even without body")
	}
	if !d.ShouldDeliver(nil, "") {
		t.Fatal("always should deliver even when result is empty")
	}
	if !d.ShouldDeliver(nil, "ok") {
		t.Fatal("always should deliver on success with body")
	}
}

func TestParseDeliveryFromAny(t *testing.T) {
	t.Parallel()
	d, err := ParseDeliveryFromAny(map[string]interface{}{
		"enabled": true,
		"channel": "lansenger",
		"targets": []interface{}{
			map[string]interface{}{"kind": "group", "group_id": "g1", "group_name": "产品群"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if d == nil || !d.Active() || d.Targets[0].GroupID != "g1" {
		t.Fatalf("got %#v", d)
	}
	cleared, err := ParseDeliveryFromAny(nil)
	if err != nil || cleared != nil {
		t.Fatalf("nil parse: %v %#v", err, cleared)
	}
	fromJSON, err := ParseDeliveryFromAny(`{"enabled":true,"targets":[{"kind":"user","user_id":"s1"}]}`)
	if err != nil || fromJSON == nil || fromJSON.Targets[0].UserID != "s1" {
		t.Fatalf("json: %v %#v", err, fromJSON)
	}

	// Agent often omits enabled — auto-enable when targets present.
	auto, err := ParseDeliveryFromAny(map[string]interface{}{
		"channel": "lansenger",
		"targets": []interface{}{
			map[string]interface{}{"kind": "group", "group_id": "g9"},
		},
	})
	if err != nil || auto == nil || !auto.Enabled {
		t.Fatalf("auto-enable: %v %#v", err, auto)
	}
	// Explicit false must not re-enable.
	off, err := ParseDeliveryFromAny(map[string]interface{}{
		"enabled": false,
		"targets": []interface{}{
			map[string]interface{}{"kind": "group", "group_id": "g9"},
		},
	})
	if err != nil || off != nil {
		t.Fatalf("explicit false: %v %#v", err, off)
	}
}

func TestMergeDeliveryOutcome(t *testing.T) {
	t.Parallel()
	soft := &TaskDelivery{Enabled: true, FailOnError: false}
	text, err := MergeDeliveryOutcome(soft, "ok body", nil, errors.New("push fail"))
	if err != nil {
		t.Fatalf("soft should not fail task: %v", err)
	}
	if !strings.Contains(text, "ok body") || !strings.Contains(text, "投递警告") {
		t.Fatalf("text=%q", text)
	}
	// Soft merge is idempotent: do not stack multiple warning lines.
	again, err := MergeDeliveryOutcome(soft, text, nil, errors.New("push fail 2"))
	if err != nil {
		t.Fatalf("soft dedupe err: %v", err)
	}
	if again != text {
		t.Fatalf("expected no second warning, got %q", again)
	}
	strict := &TaskDelivery{Enabled: true, FailOnError: true}
	_, err = MergeDeliveryOutcome(strict, "ok", nil, errors.New("push fail"))
	if err == nil || !strings.Contains(err.Error(), "agent ok") {
		t.Fatalf("strict err=%v", err)
	}
	agentErr := fmt.Errorf("%w: agent boom", context.DeadlineExceeded)
	combined, err := MergeDeliveryOutcome(soft, "x", agentErr, errors.New("push fail"))
	if err == nil || !strings.Contains(err.Error(), "agent boom") {
		t.Fatalf("combined err=%v", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("runErr wrap lost: %v", err)
	}
	_ = combined
}

func TestTruncateStrNoOvershoot(t *testing.T) {
	t.Parallel()
	s := strings.Repeat("a", 50)
	got := TruncateStr(s, 10)
	if n := len([]rune(got)); n > 10 {
		t.Fatalf("overshoot %d: %q", n, got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("want ellipsis: %q", got)
	}
}

func TestCanRememberAsSelfPeer(t *testing.T) {
	t.Parallel()
	if CanRememberAsSelfPeer(DeliveryTarget{Kind: DeliveryKindGroup, GroupID: "g1"}) {
		t.Fatal("group must not feed self memory")
	}
	if !CanRememberAsSelfPeer(DeliveryTarget{Kind: DeliveryKindUser, UserID: "u1"}) {
		t.Fatal("user should feed self memory")
	}
	if !CanRememberAsSelfPeer(DeliveryTarget{UserID: "u2"}) {
		t.Fatal("empty kind with user_id should feed self memory")
	}
	if CanRememberAsSelfPeer(DeliveryTarget{GroupID: "g2"}) {
		t.Fatal("group_id alone must not feed self memory")
	}
}

func TestCloneTaskDeliveryDeep(t *testing.T) {
	t.Parallel()
	if CloneTaskDelivery(nil) != nil {
		t.Fatal("nil")
	}
	src := &TaskDelivery{
		Enabled: true,
		Channel: DeliveryChannelLansenger,
		Targets: []DeliveryTarget{{
			Kind: DeliveryKindGroup, GroupID: "g1",
			MentionUserIDs: []string{"a", "b"},
		}},
	}
	cp := CloneTaskDelivery(src)
	if cp == src || &cp.Targets[0] == &src.Targets[0] {
		t.Fatal("expected new pointers")
	}
	cp.Targets[0].MentionUserIDs[0] = "mutated"
	if src.Targets[0].MentionUserIDs[0] != "a" {
		t.Fatal("mention slice shared")
	}
	cp.Normalize() // must not affect src channel defaults if already set
	if src.Channel != DeliveryChannelLansenger {
		t.Fatalf("src mutated: %q", src.Channel)
	}
}

func TestTruncateLastResultKeepsDeliveryWarning(t *testing.T) {
	t.Parallel()
	// Build a body longer than maxLastResultRunes with a trailing soft warning.
	body := strings.Repeat("字", maxLastResultRunes)
	full := body + "\n\n" + DeliveryWarningPrefix + "push fail"
	got := TruncateLastResult(full)
	if !HasDeliveryWarning(got) {
		t.Fatalf("warning lost: %q", got)
	}
	if !strings.Contains(got, "push fail") {
		t.Fatalf("warn detail lost: %q", got)
	}
	if n := len([]rune(got)); n > maxLastResultRunes {
		t.Fatalf("overshoot: %d runes > %d", n, maxLastResultRunes)
	}
	// Plain TruncateStr drops the suffix for this input.
	plain := TruncateStr(full, maxLastResultRunes)
	if HasDeliveryWarning(plain) {
		t.Fatalf("expected plain TruncateStr to drop warning for long body, got %q", plain)
	}
	// Short toast budget still keeps the warning.
	toast := TruncatePreservingDeliveryWarning(full, 80)
	if !HasDeliveryWarning(toast) {
		t.Fatalf("toast lost warning: %q", toast)
	}
	if n := len([]rune(toast)); n > 80 {
		t.Fatalf("toast overshoot %d", n)
	}
	if TruncateLastResult("short") != "short" {
		t.Fatal("short")
	}
	if TruncateLastResult("") != "" {
		t.Fatal("empty")
	}
}

func TestAnnotateExecErrStr(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := annotateExecErrStr(ctx, "", time.Second); got != "execution cancelled (shutdown)" {
		t.Fatalf("empty: %q", got)
	}
	if got := annotateExecErrStr(ctx, "agent boom", time.Second); !strings.Contains(got, "agent boom") || !strings.Contains(got, "cancelled") {
		t.Fatalf("augment: %q", got)
	}
	if got := annotateExecErrStr(ctx, "context canceled", time.Second); got != "context canceled" {
		t.Fatalf("dedupe: %q", got)
	}
	// Go's context.DeadlineExceeded.Error() is "context deadline exceeded".
	dctx, dcancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer dcancel()
	<-dctx.Done()
	if got := annotateExecErrStr(dctx, context.DeadlineExceeded.Error(), time.Minute); got != context.DeadlineExceeded.Error() {
		t.Fatalf("deadline dedupe: %q", got)
	}
}

func TestShouldDeliverAndSummarizeDoNotMutate(t *testing.T) {
	t.Parallel()
	d := &TaskDelivery{
		Enabled: true,
		// empty channel/on → Normalize would fill defaults if it mutated d
		Targets: []DeliveryTarget{{Kind: DeliveryKindGroup, GroupID: "g"}},
	}
	_ = d.ShouldDeliver(nil, "hi")
	_ = SummarizeDelivery(d)
	if d.Channel != "" || d.On != "" {
		t.Fatalf("read paths mutated delivery: channel=%q on=%q", d.Channel, d.On)
	}
}

func TestGetListCloneDelivery(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir + "/tasks.json")
	if err != nil {
		t.Fatal(err)
	}
	id, err := m.Add(ScheduledTask{
		Name: "n", Action: "a", Hour: 9,
		Delivery: &TaskDelivery{
			Enabled: true,
			Targets: []DeliveryTarget{{Kind: DeliveryKindUser, UserID: "u1"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := m.Get(id)
	if got == nil || got.Delivery == nil {
		t.Fatal("missing delivery")
	}
	// Mutate returned delivery; store must stay intact.
	got.Delivery.Channel = "mutated"
	got.Delivery.Targets[0].UserID = "mutated"
	again := m.Get(id)
	if again.Delivery.Channel == "mutated" || again.Delivery.Targets[0].UserID == "mutated" {
		t.Fatal("Get returned live delivery pointer")
	}
	list := m.List()
	if len(list) != 1 {
		t.Fatal(len(list))
	}
	list[0].Delivery.Channel = "mutated-list"
	again = m.Get(id)
	if again.Delivery.Channel == "mutated-list" {
		t.Fatal("List returned live delivery pointer")
	}
}

func TestAnnotateRunErrWithContext(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got := AnnotateRunErrWithContext(ctx, nil); !errors.Is(got, context.Canceled) {
		t.Fatalf("nil runErr: %v", got)
	}
	got := AnnotateRunErrWithContext(ctx, errors.New("agent boom"))
	if !errors.Is(got, context.Canceled) {
		t.Fatalf("wrapped: %v", got)
	}
	if !strings.Contains(got.Error(), "agent boom") {
		t.Fatalf("message: %v", got)
	}
	// With partial text + annotated abort, ShouldDeliver succeeds.
	d := &TaskDelivery{
		Enabled: true,
		Channel: DeliveryChannelLansenger,
		Targets: []DeliveryTarget{{Kind: DeliveryKindGroup, GroupID: "g"}},
		On:      DeliveryOnSuccess,
	}
	if !d.ShouldDeliver(got, "partial") {
		t.Fatal("partial+abort should deliver")
	}
	body := d.FormatBody("t", "partial", got)
	if !strings.Contains(body, "超时/中断") {
		t.Fatalf("body status: %q", body)
	}
}

func TestFanOutDeliveryTargets(t *testing.T) {
	t.Parallel()
	targets := []DeliveryTarget{
		{Kind: DeliveryKindGroup, GroupID: "g1"},
		{Kind: DeliveryKindGroup, GroupID: "g2"},
		{Kind: DeliveryKindUser, UserID: "u1"},
	}
	ok, err := FanOutDeliveryTargets(targets, func(i int, t DeliveryTarget) error {
		if t.GroupID == "g2" {
			return errors.New("boom")
		}
		return nil
	})
	if ok != 2 || err == nil || !strings.Contains(err.Error(), "partial") {
		t.Fatalf("ok=%d err=%v", ok, err)
	}
}

func TestFormatBodyAndSummarize(t *testing.T) {
	t.Parallel()
	d := &TaskDelivery{Enabled: true, Prefix: "日报", Targets: []DeliveryTarget{
		{Kind: DeliveryKindGroup, GroupID: "g", GroupName: "研发"},
		{Kind: DeliveryKindUser, UserID: "u1"},
	}}
	d.Normalize()
	body := d.FormatBody("新闻摘要", "1. hello", nil)
	for _, part := range []string{"新闻摘要", "1. hello", "日报"} {
		if !strings.Contains(body, part) {
			t.Fatalf("body missing %q: %q", part, body)
		}
	}
	sum := SummarizeDelivery(d)
	for _, part := range []string{"lansenger", "研发", "私聊"} {
		if !strings.Contains(sum, part) {
			t.Fatalf("sum missing %q: %q", part, sum)
		}
	}
}

func TestAddAndUpdateDelivery(t *testing.T) {
	dir := t.TempDir()
	m, err := NewManager(dir + "/tasks.json")
	if err != nil {
		t.Fatal(err)
	}
	id, err := m.Add(ScheduledTask{
		Name:   "news",
		Action: "search news",
		Hour:   9,
		Minute: 0,
		Delivery: &TaskDelivery{
			Enabled: true,
			Channel: DeliveryChannelLansenger,
			Targets: []DeliveryTarget{{Kind: DeliveryKindGroup, GroupID: "g1", GroupName: "G"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := m.Get(id)
	if got == nil || got.Delivery == nil || !got.Delivery.Active() {
		t.Fatalf("delivery not persisted: %#v", got)
	}
	// int hour (not float64) must apply.
	if err := m.Update(id, map[string]interface{}{"hour": 10}); err != nil {
		t.Fatal(err)
	}
	if got = m.Get(id); got == nil || got.Hour != 10 {
		t.Fatalf("hour int coerce: %#v", got)
	}
	if err := m.Update(id, map[string]interface{}{"delivery": nil}); err != nil {
		t.Fatal(err)
	}
	got = m.Get(id)
	if got == nil || got.Delivery != nil {
		t.Fatalf("expected cleared delivery, got %#v", got.Delivery)
	}
}
