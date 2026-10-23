package botregistry

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func TestBusDedupeRateLoop(t *testing.T) {
	now := time.Date(2026, 10, 23, 10, 0, 0, 0, time.UTC)
	b := NewBus(BusLimits{PerMinute: 3, PerHour: 5, PairMax: 4, MaxHops: 2})
	b.SetClock(func() time.Time { return now })
	m1, err := b.Submit(Message{From: "a", To: "main", Kind: MsgDone, Text: "published"})
	if err != nil || m1.ID == "" {
		t.Fatalf("first: %v", err)
	}
	if got, err := b.Submit(Message{From: "a", To: "main", Kind: MsgDone, Text: "  published "}); !errors.Is(err, ErrDuplicate) || got.ID != m1.ID {
		t.Fatalf("dup: %v %v", got.ID, err)
	}
	if _, err := b.Submit(Message{From: "a", To: "main", Text: "x", ExternalID: "n1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Submit(Message{From: "a", To: "main", Text: "other text", ExternalID: "n1"}); !errors.Is(err, ErrDuplicate) {
		t.Fatalf("ext dup: %v", err)
	}
	b.Submit(Message{From: "a", To: "main", Text: "three"})
	if _, err := b.Submit(Message{From: "a", To: "main", Text: "four"}); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("rate: %v", err)
	}
	now = now.Add(11 * time.Minute)
	// pair guard: a<->main already 3; one more allowed then loop
	if _, err := b.Submit(Message{From: "main", To: "a", Text: "ok"}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Submit(Message{From: "main", To: "a", Text: "again"}); !errors.Is(err, ErrLoop) {
		t.Fatalf("pair: %v", err)
	}
	b.PersonSpoke()
	if _, err := b.Submit(Message{From: "main", To: "a", Text: "again"}); err != nil {
		t.Fatalf("after person: %v", err)
	}
	// hops
	r1, _ := b.Submit(Message{From: "c", To: "d", Text: "q"})
	r2, _ := b.Submit(Message{From: "d", To: "c", Text: "a1", ReplyTo: r1.ID})
	r3, err := b.Submit(Message{From: "c", To: "d", Text: "a2", ReplyTo: r2.ID})
	if err != nil || r3.Hops != 2 {
		t.Fatalf("hops %d %v", r3.Hops, err)
	}
	if _, err := b.Submit(Message{From: "d", To: "c", Text: "a3", ReplyTo: r3.ID}); !errors.Is(err, ErrLoop) {
		t.Fatalf("hop limit: %v", err)
	}
	if _, err := b.Submit(Message{From: "x", To: "x", Text: "self"}); !errors.Is(err, ErrBadMessage) {
		t.Fatalf("self: %v", err)
	}
}

func TestInboxWakesWhenIdleQueuesWhenBusy(t *testing.T) {
	var woke [][]Message
	in := NewInbox(func(ms []Message) { woke = append(woke, ms) })
	if !in.Deliver(Message{ID: "1", Kind: MsgDone, Text: "a"}) || len(woke) != 1 {
		t.Fatal("idle should wake")
	}
	if in.Deliver(Message{ID: "p", Kind: MsgProgress, Text: "50%"}) || len(woke) != 1 {
		t.Fatal("progress must not wake")
	}
	end := in.Begin()
	if in.Deliver(Message{ID: "2", Kind: MsgQuestion, Text: "b"}) {
		t.Fatal("busy should queue")
	}
	end()
	end() // idempotent
	if len(woke) != 2 || len(woke[1]) != 2 || woke[1][0].ID != "p" || woke[1][1].ID != "2" {
		t.Fatalf("flush on turn end: %+v", woke)
	}
	end2 := in.Begin()
	in.Deliver(Message{ID: "3", Kind: MsgProgress, Text: "c"})
	end2()
	if len(woke) != 2 || len(in.Pending(false)) != 1 {
		t.Fatal("progress alone stays pending")
	}
}

func TestWakePromptFramesAsData(t *testing.T) {
	p := WakePrompt([]Message{{ID: "m1", From: "b1", FromName: "发布员", Kind: MsgArtifact, Text: "截图好了", Artifacts: []string{"/d/s.png"}}})
	for _, want := range []string{"not from the person", "发布员", "kind=artifact", "/d/s.png", "reply_to=id"} {
		if !strings.Contains(p, want) {
			t.Fatalf("missing %q in %s", want, p)
		}
	}
	if d := WakeDisplay([]Message{{From: "b1", FromName: "发布员", Kind: MsgDone, Text: "ok"}}); !strings.Contains(d, "发布员（完成）") {
		t.Fatal(d)
	}
}

func TestCheckMessageAnyBot(t *testing.T) {
	if d := Check(Bot{ID: "sub"}, Bot{ID: "main"}, ActionMessage, "hi"); !d.Allowed {
		t.Fatal("sub may message main")
	}
	if d := Check(Bot{ID: "sub"}, Bot{ID: "peer"}, ActionNotify, "hi"); d.Allowed {
		t.Fatal("notify (control) stays primary-only")
	}
}
