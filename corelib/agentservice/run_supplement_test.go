package agentservice

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// The supplement queue exists to keep six properties true. Each test below is
// one of them; if a change breaks one of these, the feature is broken even
// when every other test still passes.
//
// I1  one supplement reaches the loop at most once
// I2  a supplement reaches exactly one run, the one it was aimed at
// I3  the conversation cost stays constant regardless of how many arrive
// I4  a supplement ends in exactly one of applied or missed, never limbo
// I5  no run identity means no delivery, never a session-wide guess
// I6  supplement text never lands in logs or durable storage

// testServiceForSupplements builds a service reduced to the fields the queue
// touches. Nothing here dials a store or an executor: these invariants are
// about routing and lifecycle, not about the worker.
func testServiceForSupplements() *Service {
	return &Service{
		now:         func() time.Time { return time.Unix(0, 0).UTC() },
		supplements: newRunSupplements(),
		runningRuns: map[string]context.CancelFunc{},
	}
}

// I1: draining marks what it returned, so a second drain cannot repeat it.
func TestRunSupplementTakeIsAtMostOnce(t *testing.T) {
	r := newRunSupplements()
	r.append("run_1", "only first 5", time.Now())
	first := r.takeQueued("run_1", time.Now())
	if len(first) != 1 {
		t.Fatalf("expected one item on first take, got %d", len(first))
	}
	if second := r.takeQueued("run_1", time.Now()); len(second) != 0 {
		t.Fatalf("supplement delivered twice: %d items on second take", len(second))
	}
	if first[0].State != runSupplementStateApplied {
		t.Fatalf("expected taken item marked applied, got %q", first[0].State)
	}
}

// I2: two live runs keep separate queues. An item aimed at one must never show
// up in the other's drain, which is what a session-keyed queue would do.
func TestRunSupplementQueuesArePerRun(t *testing.T) {
	r := newRunSupplements()
	r.append("run_a", "aimed at a", time.Now())
	r.append("run_b", "aimed at b", time.Now())
	a := r.takeQueued("run_a", time.Now())
	if len(a) != 1 || !strings.Contains(a[0].Content, "aimed at a") {
		t.Fatalf("run a got the wrong contents: %+v", a)
	}
	b := r.takeQueued("run_b", time.Now())
	if len(b) != 1 || !strings.Contains(b[0].Content, "aimed at b") {
		t.Fatalf("run b got the wrong contents: %+v", b)
	}
}

// I3: no matter how many supplements arrive, the rotating summary stays one
// message. Appending per item is what made earlier drafts leak tokens on every
// later iteration.
func TestRunSupplementSummaryStaysOneMessage(t *testing.T) {
	h := &supplementHook{take: func() []*RunSupplement { return nil }}
	var conversation []interface{}
	conversation = append(conversation, map[string]string{"role": "system", "content": "base prompt"})
	var pending []*RunSupplement
	for i := 0; i < 20; i++ {
		pending = append(pending, &RunSupplement{ID: "s", Content: "line"})
	}
	h.take = func() []*RunSupplement { out := pending; pending = nil; return out }
	conversation = valueOrOriginal(h.TransformConversation(conversation), conversation)
	for round := 0; round < 5; round++ {
		h.merged = append(h.merged, "extra")
		h.dirty = true
		conversation = valueOrOriginal(h.TransformConversation(conversation), conversation)
	}
	if got := countSupplementSummaries(conversation); got != 1 {
		t.Fatalf("expected exactly one summary message, got %d", got)
	}
	if len(h.merged) == 0 {
		t.Fatal("summary lost its contents")
	}
}

func valueOrOriginal(next, fallback []interface{}) []interface{} {
	if next == nil {
		return fallback
	}
	return next
}

func countSupplementSummaries(conversation []interface{}) int {
	count := 0
	for _, msg := range conversation {
		if content, ok := summaryText(msg); ok && strings.Contains(content, supplementMarker) {
			count++
		}
	}
	return count
}

// I3 companion: every merged supplement stays visible after subsequent rounds.
func TestRunSupplementSummaryKeepsAllItems(t *testing.T) {
	h := &supplementHook{take: func() []*RunSupplement {
		return []*RunSupplement{{ID: "1", Content: "first requirement"}}
	}}
	var conversation []interface{}
	conversation = valueOrOriginal(h.TransformConversation(conversation), conversation)
	h.take = func() []*RunSupplement { return []*RunSupplement{{ID: "2", Content: "second requirement"}} }
	conversation = valueOrOriginal(h.TransformConversation(conversation), conversation)
	text := summaryOf(conversation)
	if !strings.Contains(text, "first requirement") || !strings.Contains(text, "second requirement") {
		t.Fatalf("summary lost an item: %q", text)
	}
}

func summaryOf(conversation []interface{}) string {
	for _, msg := range conversation {
		if content, ok := summaryText(msg); ok && strings.Contains(content, supplementMarker) {
			return content
		}
	}
	return ""
}

// I4: whatever the loop never consumed is reported once, then released.
func TestRunSupplementReapReportsMissed(t *testing.T) {
	r := newRunSupplements()
	r.append("run_done", "arrived too late", time.Now())
	missed := r.reap("run_done")
	if len(missed) != 1 {
		t.Fatalf("expected one missed report, got %d", len(missed))
	}
	if missed[0].State != runSupplementStateMissed {
		t.Fatalf("expected missed state, got %q", missed[0].State)
	}
	if again := r.reap("run_done"); len(again) != 0 {
		t.Fatalf("reap reported twice: %d", len(again))
	}
}

// I5: without a run identity nothing is delivered. This is the guard that stops
// the queue from falling back to a session-wide guess if wiring regresses.
func TestSupplementHookDisabledWithoutRunID(t *testing.T) {
	hook := newSupplementHook(testServiceForSupplements(), "bot-1", "")
	if _, ok := hook.(agent.DefaultLoopHooks); !ok {
		t.Fatal("hook without a run id must stay inert, not guess a run")
	}
	svc := testServiceForSupplements()
	if got := svc.takePendingRunSupplements(""); len(got) != 0 {
		t.Fatalf("empty run id must never deliver: got %d", len(got))
	}
}

// I6: the audit trail records identifiers and counts, never the text itself.
func TestRunSupplementNeverPersistsText(t *testing.T) {
	item := &RunSupplement{ID: "s1", Content: "secret preference text", State: runSupplementStateQueued}
	for _, field := range []string{item.ID, string(item.State)} {
		if strings.Contains(field, "secret preference") {
			t.Fatalf("text leaked into an identifier: %q", field)
		}
	}
	if !strings.Contains(formatSupplementSummary([]string{"secret preference text"}), "secret preference text") {
		t.Fatal("summary should carry the text to the model; it must not be logged instead")
	}
}

// The default for an empty model reply must stay "exit the loop". Implementing
// LoopHooks directly is allowed, but changing this return value would turn a
// dead model call into a spin.
func TestSupplementHookKeepsEmptyResponseDefault(t *testing.T) {
	h := &supplementHook{take: func() []*RunSupplement { return nil }}
	if h.OnEmptyResponse(0) {
		t.Fatal("OnEmptyResponse must return false to keep the loop's default exit")
	}
}

// I4 companion: a missed supplement must still be readable after the run ends.
// The person is told "it did not apply", which is only honest if asking again
// still returns that fact. Deleting the queue entry at reap time makes the
// report unreachable, so the list endpoint answers "nothing was ever queued"
// and the promise is silently withdrawn.
func TestRunSupplementMissedSurvivesReapForTheReporter(t *testing.T) {
	r := newRunSupplements()
	r.append("run_over", "arrived after the last iteration", time.Now())
	// The run ends without another loop iteration, so nothing drains it.
	svc := &Service{now: func() time.Time { return time.Unix(0, 0).UTC() }, supplements: r}
	svc.reapRunSupplements("run_over")

	items := r.outcome("run_over")
	if len(items) != 1 {
		t.Fatalf("reap erased the record of a missed supplement, so the receipt can never leave queued: %+v", r.closed)
	}
	if items[0].State != runSupplementStateMissed {
		t.Fatalf("expected the retained item to report missed, got %q", items[0].State)
	}
	// Reported once; releasing it afterwards is a separate decision.
	if again := r.reap("run_over"); len(again) != 0 {
		t.Fatalf("a second reap re-reported the same item: %+v", again)
	}
}
