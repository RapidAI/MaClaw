package browser

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"
	"time"
)

// PublishEventSource is a PublishDriver that also streams the page's CDP
// events. With it the publish run judges uploads, draft saves and the
// submit by the requests the page actually makes instead of by time.
type PublishEventSource interface {
	Subscribe() (events <-chan CDPEvent, cancel func())
}

// netTracker follows the page's writing requests (POST/PUT/PATCH/DELETE):
// how many are in flight, when the last one changed, and which failed.
type netTracker struct {
	mu       sync.Mutex
	open     map[string]string // request id -> method+url
	last     time.Time
	failures []netFailure
	writes   int
	// fetched: last XHR/fetch of any method (an editor polls an upload's
	// processing state with reads after the write finished).
	fetched time.Time
	starts  []time.Time
	log     []netWrite // every write and its answer, in order
	cancel  func()
	// d reads a write's answer body; json marks writes answered with JSON.
	d    PublishDriver
	json map[string]bool
	// refusals: writes answered 2xx whose JSON body says they failed
	// (GraphQL errors, success:false); only the submit consults them.
	refusals []netFailure
}

type netWrite struct {
	at     time.Time
	id     string
	what   string
	answer string
}

type netFailure struct {
	at   time.Time
	what string
}

var netWriteMethods = map[string]bool{"POST": true, "PUT": true, "PATCH": true, "DELETE": true}

// startNetTracker enables the Network domain and starts following events;
// nil when the driver cannot stream events (callers fall back to the DOM).
func startNetTracker(d PublishDriver) *netTracker {
	src, ok := d.(PublishEventSource)
	if !ok {
		return nil
	}
	if _, err := d.Send("Network.enable", map[string]interface{}{}); err != nil {
		return nil
	}
	ch, cancel := src.Subscribe()
	t := &netTracker{open: map[string]string{}, last: time.Now(), cancel: cancel, d: d, json: map[string]bool{}}
	go func() {
		for ev := range ch {
			t.on(ev)
		}
	}()
	return t
}

func (t *netTracker) stop() {
	if t != nil && t.cancel != nil {
		t.cancel()
	}
}

func (t *netTracker) on(ev CDPEvent) {
	var p struct {
		RequestID string `json:"requestId"`
		Request   struct {
			Method string `json:"method"`
			URL    string `json:"url"`
		} `json:"request"`
		Response struct {
			Status   int    `json:"status"`
			MimeType string `json:"mimeType"`
		} `json:"response"`
		Type      string `json:"type"`
		ErrorText string `json:"errorText"`
		Canceled  bool   `json:"canceled"`
	}
	if !strings.HasPrefix(ev.Method, "Network.") || json.Unmarshal(ev.Params, &p) != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if ev.Method == "Network.requestWillBeSent" && (p.Type == "XHR" || p.Type == "Fetch") {
		t.fetched = time.Now()
	}
	switch ev.Method {
	case "Network.requestWillBeSent":
		if netWriteMethods[strings.ToUpper(p.Request.Method)] && !netBeacon(p.Type, p.Request.URL) {
			t.open[p.RequestID] = p.Request.Method + " " + clipRunes(p.Request.URL, 120)
			t.writes++
			t.starts = append(t.starts, time.Now())
			t.log = append(t.log, netWrite{at: time.Now(), id: p.RequestID, what: t.open[p.RequestID], answer: "no answer yet"})
			t.last = time.Now()
		}
	case "Network.responseReceived":
		t.answer(p.RequestID, "HTTP "+itoa(p.Response.Status))
		if _, ok := t.open[p.RequestID]; ok && strings.Contains(p.Response.MimeType, "json") && p.Response.Status < 400 {
			t.json[p.RequestID] = true
		}
		if what, ok := t.open[p.RequestID]; ok && p.Response.Status >= 400 {
			t.failures = append(t.failures, netFailure{time.Now(), what + " → HTTP " + itoa(p.Response.Status)})
		}
	case "Network.loadingFinished":
		if what, ok := t.open[p.RequestID]; ok {
			delete(t.open, p.RequestID)
			t.last = time.Now()
			if t.json[p.RequestID] && t.d != nil {
				delete(t.json, p.RequestID)
				go t.readAnswer(p.RequestID, what)
			}
		}
	case "Network.loadingFailed":
		if what, ok := t.open[p.RequestID]; ok {
			delete(t.open, p.RequestID)
			t.last = time.Now()
			t.answer(p.RequestID, "failed: "+p.ErrorText)
			if !p.Canceled {
				t.failures = append(t.failures, netFailure{time.Now(), what + " → " + p.ErrorText})
			}
		}
	}
}

// state: writing requests in flight and the time since the last change.
func (t *netTracker) state() (int, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.open), time.Since(t.last)
}

// idleFor: time since the page last wrote or made any XHR/fetch.
func (t *netTracker) idleFor() (int, time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	last := t.last
	if t.fetched.After(last) {
		last = t.fetched
	}
	return len(t.open), time.Since(last)
}

// failedSince lists writing requests that failed after since.
func (t *netTracker) failedSince(since time.Time) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for _, f := range t.failures {
		if f.at.After(since) {
			out = append(out, f.what)
		}
	}
	return out
}

// waitQuiet returns once no writing request is in flight and none changed
// for quiet, or at max (false). This is the editor's own "saved" signal:
// uploads and draft saves are writing requests.
func (t *netTracker) waitQuiet(quiet, max time.Duration) bool {
	if t == nil {
		return true
	}
	end := time.Now().Add(max)
	for {
		n, since := t.state()
		if n == 0 && since >= quiet {
			return true
		}
		if time.Now().After(end) {
			return false
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func itoa(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

// wroteSince: a writing request started after since.
func (t *netTracker) wroteSince(since time.Time) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, s := range t.starts {
		if s.After(since) {
			return true
		}
	}
	return false
}

func (t *netTracker) answer(id, a string) {
	for i := len(t.log) - 1; i >= 0; i-- {
		if t.log[i].id == id {
			t.log[i].answer = a
			return
		}
	}
}

// writesSince lists the writes sent after since with their answers.
func (t *netTracker) writesSince(since time.Time) []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for _, w := range t.log {
		if w.at.After(since) {
			out = append(out, w.what+" → "+w.answer)
		}
	}
	return out
}

// netBeaconRe: analytics and logging endpoints; their writes are not the
// page saving or posting anything.
var netBeaconRe = regexp.MustCompile(`(?i)/(log|logs|logging|track|tracking|tracker|collect|beacon|metric|metrics|stat|stats|apm|sentry|analytics|telemetry|rum|perf)(/|\?|\.|$)`)

func netBeacon(typ, u string) bool {
	if typ == "Ping" || typ == "CSPViolationReport" {
		return true
	}
	if i := strings.Index(u, "?"); i >= 0 {
		u = u[:i]
	}
	return netBeaconRe.MatchString(u)
}

// readAnswer reads a JSON answer and notes a refusal the status code hid.
func (t *netTracker) readAnswer(id, what string) {
	raw, err := t.d.Send("Network.getResponseBody", map[string]interface{}{"requestId": id})
	if err != nil {
		return
	}
	var r struct {
		Body string `json:"body"`
		B64  bool   `json:"base64Encoded"`
	}
	if json.Unmarshal(raw, &r) != nil || r.B64 || len(r.Body) > 1<<16 {
		return
	}
	if why := jsonRefusal(r.Body); why != "" {
		t.mu.Lock()
		t.refusals = append(t.refusals, netFailure{time.Now(), what + " → " + why})
		t.answer(id, "refused: "+why)
		t.mu.Unlock()
	}
}

// jsonRefusal: a JSON answer that says the write failed ("" otherwise).
func jsonRefusal(body string) string {
	var m map[string]json.RawMessage
	if json.Unmarshal([]byte(body), &m) != nil {
		return ""
	}
	if e, ok := m["errors"]; ok {
		var list []map[string]any
		if json.Unmarshal(e, &list) == nil && len(list) > 0 {
			msg, _ := list[0]["message"].(string)
			return "errors: " + clipRunes(msg, 120)
		}
	}
	if v, ok := m["success"]; ok && string(v) == "false" {
		for _, k := range []string{"msg", "message", "error"} {
			if x, ok := m[k]; ok {
				var s string
				_ = json.Unmarshal(x, &s)
				return "success=false: " + clipRunes(s, 120)
			}
		}
		return "success=false"
	}
	return ""
}

// refusedSince lists writes after since whose JSON answer refused them.
func (t *netTracker) refusedSince(since time.Time) []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []string
	for _, f := range t.refusals {
		if f.at.After(since) {
			out = append(out, f.what)
		}
	}
	return out
}
