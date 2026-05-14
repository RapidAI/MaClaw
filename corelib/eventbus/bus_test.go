package eventbus

import (
	"sync"
	"testing"
	"time"
)

func TestBus_PublishSubscribe(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe("memory")
	bus.Publish(MemoryEvent{Kind: "saved", EntryID: "e1"})

	select {
	case evt := <-ch:
		if evt.Domain() != "memory" || evt.Type() != "saved" {
			t.Errorf("wrong event: %s/%s", evt.Domain(), evt.Type())
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timeout waiting for event")
	}
}

func TestBus_DomainIsolation(t *testing.T) {
	bus := New()
	defer bus.Close()

	memCh := bus.Subscribe("memory")
	toolCh := bus.Subscribe("tool")

	bus.Publish(MemoryEvent{Kind: "saved"})

	select {
	case <-memCh:
		// expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("memory subscriber should receive")
	}

	select {
	case <-toolCh:
		t.Fatal("tool subscriber should NOT receive memory events")
	case <-time.After(50 * time.Millisecond):
		// expected — no event
	}
}

func TestBus_WildcardSubscriber(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe("*")
	bus.Publish(MemoryEvent{Kind: "saved"})
	bus.Publish(ToolEvent{Kind: "executed", ToolName: "bash"})

	received := 0
	for i := 0; i < 2; i++ {
		select {
		case <-ch:
			received++
		case <-time.After(100 * time.Millisecond):
			break
		}
	}
	if received != 2 {
		t.Errorf("wildcard should receive all events, got %d", received)
	}
}

func TestBus_NonBlockingPublish(t *testing.T) {
	bus := NewWithBuffer(1) // tiny buffer
	defer bus.Close()

	bus.Subscribe("memory") // subscribe but don't read

	// Publish more than buffer size — should not block
	done := make(chan bool)
	go func() {
		for i := 0; i < 100; i++ {
			bus.Publish(MemoryEvent{Kind: "saved"})
		}
		done <- true
	}()

	select {
	case <-done:
		// good — didn't block
	case <-time.After(1 * time.Second):
		t.Fatal("publish should not block even with full buffer")
	}
}

func TestBus_Unsubscribe(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe("memory")
	bus.Unsubscribe("memory", ch)

	// Channel should be closed
	_, ok := <-ch
	if ok {
		t.Error("channel should be closed after unsubscribe")
	}

	if bus.SubscriberCount("memory") != 0 {
		t.Error("subscriber count should be 0 after unsubscribe")
	}
}

func TestBus_MultipleSubscribers(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch1 := bus.Subscribe("tool")
	ch2 := bus.Subscribe("tool")

	bus.Publish(ToolEvent{Kind: "executed", ToolName: "bash"})

	for _, ch := range []<-chan Event{ch1, ch2} {
		select {
		case evt := <-ch:
			if evt.Type() != "executed" {
				t.Error("wrong event type")
			}
		case <-time.After(100 * time.Millisecond):
			t.Fatal("both subscribers should receive")
		}
	}
}

func TestBus_ConcurrentPublish(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := bus.Subscribe("tool")
	var wg sync.WaitGroup
	n := 100

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			bus.Publish(ToolEvent{Kind: "executed"})
		}()
	}
	wg.Wait()

	received := 0
	for {
		select {
		case <-ch:
			received++
		case <-time.After(50 * time.Millisecond):
			goto done
		}
	}
done:
	if received < n/2 {
		t.Errorf("expected most events delivered, got %d/%d", received, n)
	}
}

func TestBus_NilSafe(t *testing.T) {
	var bus *Bus
	bus.Publish(MemoryEvent{Kind: "saved"}) // should not panic
	ch := bus.Subscribe("memory")
	_, ok := <-ch
	if ok {
		t.Error("nil bus subscribe should return closed channel")
	}
	bus.Unsubscribe("memory", ch) // should not panic
	bus.Close()                   // should not panic
}

func TestBus_SubscriberCount(t *testing.T) {
	bus := New()
	defer bus.Close()

	if bus.SubscriberCount("memory") != 0 {
		t.Error("initial count should be 0")
	}
	bus.Subscribe("memory")
	bus.Subscribe("memory")
	if bus.SubscriberCount("memory") != 2 {
		t.Errorf("expected 2, got %d", bus.SubscriberCount("memory"))
	}
}

func TestGenericEvent(t *testing.T) {
	evt := GenericEvent{EventDomain: "custom", EventType: "test", Payload: "data"}
	if evt.Domain() != "custom" || evt.Type() != "test" {
		t.Error("generic event fields wrong")
	}
}
