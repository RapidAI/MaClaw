package browser

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"
)

type orderDriver struct {
	mu           sync.Mutex
	calls        []string
	failDisables int
	release      chan struct{}
	started      chan struct{}
	once         sync.Once
	subs         []chan CDPEvent
}

func (d *orderDriver) note(method string) {
	d.mu.Lock()
	d.calls = append(d.calls, method)
	d.mu.Unlock()
}

func (d *orderDriver) Send(method string, params interface{}) (json.RawMessage, error) {
	if method == "Network.getResponseBody" {
		d.note(method)
		d.once.Do(func() { close(d.started) })
		<-d.release
		return json.RawMessage(`{"body":"{\"ok\":true}","base64Encoded":false}`), nil
	}
	if method == "Network.disable" {
		d.mu.Lock()
		d.calls = append(d.calls, method)
		fail := d.failDisables > 0
		if fail {
			d.failDisables--
		}
		d.mu.Unlock()
		if fail {
			return nil, fmt.Errorf("disable failed")
		}
		return json.RawMessage(`{}`), nil
	}
	d.note(method)
	return json.RawMessage(`{}`), nil
}

func (d *orderDriver) ArmChooser([]string) error               { return nil }
func (d *orderDriver) WaitChooser(time.Duration) (bool, error) { return false, nil }
func (d *orderDriver) DisarmChooser()                          {}

func (d *orderDriver) Subscribe() (<-chan CDPEvent, func()) {
	ch := make(chan CDPEvent, 8)
	d.mu.Lock()
	d.subs = append(d.subs, ch)
	d.mu.Unlock()
	return ch, func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		for i, c := range d.subs {
			if c == ch {
				d.subs = append(d.subs[:i], d.subs[i+1:]...)
				close(ch)
				return
			}
		}
	}
}

func (d *orderDriver) emit(method, params string) {
	ev := CDPEvent{Method: method, Params: json.RawMessage(params)}
	d.mu.Lock()
	subs := append([]chan CDPEvent{}, d.subs...)
	d.mu.Unlock()
	for _, ch := range subs {
		ch <- ev
	}
}

func TestNetTrackerDisableWaitsForBody(t *testing.T) {
	d := &orderDriver{release: make(chan struct{}), started: make(chan struct{})}
	tr := startNetTracker(d)
	if tr == nil {
		t.Fatal("tracker was not started")
	}
	d.emit("Network.requestWillBeSent", `{"requestId":"r1","type":"XHR","request":{"method":"POST","url":"https://example.com/save","headers":{}}}`)
	d.emit("Network.responseReceived", `{"requestId":"r1","type":"XHR","response":{"status":200,"mimeType":"application/json"}}`)
	d.emit("Network.loadingFinished", `{"requestId":"r1"}`)
	select {
	case <-d.started:
	case <-time.After(2 * time.Second):
		t.Fatal("response body was not read")
	}
	stopped := make(chan struct{})
	go func() {
		tr.stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("stop turned Network off while the response body was still being read")
	case <-time.After(150 * time.Millisecond):
	}
	close(d.release)
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stop did not finish after the body was read")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	body, disable := -1, -1
	for i, c := range d.calls {
		if c == "Network.getResponseBody" && body < 0 {
			body = i
		}
		if c == "Network.disable" && disable < 0 {
			disable = i
		}
	}
	if body < 0 || disable < 0 || body > disable {
		t.Fatalf("call order = %v", d.calls)
	}
}

func TestNetTrackerDisableRetries(t *testing.T) {
	d := &orderDriver{release: make(chan struct{}), started: make(chan struct{}), failDisables: 1}
	tr := startNetTracker(d)
	if tr == nil {
		t.Fatal("tracker was not started")
	}
	tr.stop()
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.calls) != 3 || d.calls[0] != "Network.enable" || d.calls[1] != "Network.disable" || d.calls[2] != "Network.disable" {
		t.Fatalf("calls = %v", d.calls)
	}
}
