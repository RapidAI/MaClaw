package browser

import (
	"encoding/json"
	"regexp"
	"strconv"
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
	// reading: answers of finished writes still being read.
	reading int
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
	// path is the URL without query; kind classifies the write (see
	// netWriteKind); done/ok: the request finished, with a 2xx/3xx answer.
	path string
	kind string
	done bool
	ok   bool
	// postID/postURL: an id or link the JSON answer carries (evidence of a
	// created post when the write is a publish, see evidenceSince).
	postID, postURL string
}

// Write kinds. upload: a file/picture going to the site; draft: a draft or
// autosave; write: anything else (a publish, a settings change).
const (
	netKindUpload = "upload"
	netKindDraft  = "draft"
	netKindWrite  = "write"
)

var (
	netUploadRe = regexp.MustCompile(`(?i)upload|(^|[/_.-])(images?|imgs?|pics?|photos?|pictures?|files?|media|bfs)([/_.-]|$)`)
	netDraftRe  = regexp.MustCompile(`(?i)draft|auto_?-?save|save_?cache`)
)

// netWriteKind classifies a writing request from its URL and headers. A
// multipart body is an upload; a URL naming a draft or autosave is a draft.
func netWriteKind(u string, headers map[string]any) string {
	ct := ""
	for k, v := range headers {
		if strings.EqualFold(k, "content-type") {
			ct, _ = v.(string)
		}
	}
	path := u
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	switch {
	case strings.Contains(strings.ToLower(ct), "multipart/form-data"), strings.HasPrefix(strings.ToLower(ct), "image/"):
		return netKindUpload
	case netDraftRe.MatchString(path):
		return netKindDraft
	case netUploadRe.MatchString(path):
		return netKindUpload
	}
	return netKindWrite
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
			Method  string         `json:"method"`
			URL     string         `json:"url"`
			Headers map[string]any `json:"headers"`
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
			path := p.Request.URL
			if i := strings.IndexAny(path, "?#"); i >= 0 {
				path = path[:i]
			}
			t.log = append(t.log, netWrite{at: time.Now(), id: p.RequestID, what: t.open[p.RequestID], answer: "no answer yet", path: path, kind: netWriteKind(p.Request.URL, p.Request.Headers)})
			t.last = time.Now()
		}
	case "Network.responseReceived":
		t.answer(p.RequestID, "HTTP "+itoa(p.Response.Status))
		if w := t.find(p.RequestID); w != nil {
			w.ok = p.Response.Status > 0 && p.Response.Status < 400
		}
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
			if w := t.find(p.RequestID); w != nil {
				w.done = true
			}
			if t.json[p.RequestID] && t.d != nil {
				delete(t.json, p.RequestID)
				t.reading++
				go func(id, what string) {
					t.readAnswer(id, what)
					t.mu.Lock()
					t.reading--
					t.mu.Unlock()
				}(p.RequestID, what)
			}
		}
	case "Network.loadingFailed":
		if what, ok := t.open[p.RequestID]; ok {
			delete(t.open, p.RequestID)
			t.last = time.Now()
			t.answer(p.RequestID, "failed: "+p.ErrorText)
			if w := t.find(p.RequestID); w != nil {
				w.done, w.ok = true, false
			}
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

// find is the log entry of a request (nil when it is not a tracked write).
func (t *netTracker) find(id string) *netWrite {
	for i := len(t.log) - 1; i >= 0; i-- {
		if t.log[i].id == id {
			return &t.log[i]
		}
	}
	return nil
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
		return
	}
	pid, purl := jsonPostRef(r.Body)
	if pid == "" && purl == "" {
		return
	}
	t.mu.Lock()
	if w := t.find(id); w != nil {
		w.postID, w.postURL = pid, purl
		w.answer += " (id " + pid + " " + purl + ")"
	}
	t.mu.Unlock()
}

// jsonPostRefKeys: field names that carry a created object's id or link in
// a JSON answer, in order of preference (generic, no site names).
var (
	jsonPostIDKeys  = []string{"article_id", "articleId", "post_id", "postId", "note_id", "noteId", "aid", "cvid", "id"}
	jsonPostURLKeys = []string{"url", "link", "article_url", "articleUrl", "share_url", "shareUrl", "post_url", "postUrl"}
	jsonPostIDRe    = regexp.MustCompile(`^[0-9A-Za-z_-]{4,40}$`)
)

// jsonPostRef finds an id and an http(s) link in a JSON answer: top level
// or under data/result/article/post (two levels).
func jsonPostRef(body string) (string, string) {
	var v any
	if json.Unmarshal([]byte(body), &v) != nil {
		return "", ""
	}
	var id, link string
	var walk func(x any, depth int)
	walk = func(x any, depth int) {
		m, ok := x.(map[string]any)
		if !ok || depth > 2 {
			return
		}
		for _, k := range jsonPostIDKeys {
			if id != "" {
				break
			}
			switch val := m[k].(type) {
			case string:
				if jsonPostIDRe.MatchString(val) && strings.Trim(val, "0") != "" {
					id = val
				}
			case float64:
				if val >= 1000 {
					id = strconv.FormatInt(int64(val), 10)
				}
			}
		}
		for _, k := range jsonPostURLKeys {
			if s, ok := m[k].(string); ok && link == "" && (strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") || strings.HasPrefix(s, "//")) {
				link = s
			}
		}
		for _, k := range []string{"data", "result", "article", "post", "note"} {
			walk(m[k], depth+1)
		}
	}
	walk(v, 0)
	return id, link
}

// uploadStats counts upload-kind writes started after since: started,
// finished with a good answer, failed, and still open.
func (t *netTracker) uploadStats(since time.Time) (started, ok, failed, open int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, w := range t.log {
		if w.kind != netKindUpload || !w.at.After(since) {
			continue
		}
		started++
		switch {
		case !w.done:
			open++
		case w.ok:
			ok++
		default:
			failed++
		}
	}
	return
}

// pathsBefore: URL paths of writes sent before t (the editor's own saves).
func (t *netTracker) pathsBefore(at time.Time) map[string]bool {
	out := map[string]bool{}
	if t == nil {
		return out
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, w := range t.log {
		if !w.at.After(at) {
			out[w.path] = true
		}
	}
	return out
}

// submitWritesSince lists the writes after since that can be a submission:
// not an upload, not a draft/autosave, and not to an endpoint the editor
// was already writing to before since (its periodic draft save).
func (t *netTracker) submitWritesSince(since time.Time) []netWrite {
	if t == nil {
		return nil
	}
	before := t.pathsBefore(since)
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []netWrite
	for _, w := range t.log {
		if w.at.After(since) && w.kind == netKindWrite && !before[w.path] {
			out = append(out, w)
		}
	}
	return out
}

// draftWritesSince counts writes after since that only saved a draft.
func (t *netTracker) draftWritesSince(since time.Time) int {
	if t == nil {
		return 0
	}
	before := t.pathsBefore(since)
	t.mu.Lock()
	defer t.mu.Unlock()
	n := 0
	for _, w := range t.log {
		if w.at.After(since) && (w.kind == netKindDraft || (w.kind == netKindWrite && before[w.path])) {
			n++
		}
	}
	return n
}

// settleAnswers waits up to max for submission writes since since to finish
// and for their answers to be read (the page reacts to an answer before
// the tracker has read its body).
func (t *netTracker) settleAnswers(since time.Time, max time.Duration) {
	if t == nil {
		return
	}
	end := time.Now().Add(max)
	for time.Now().Before(end) {
		busy := false
		for _, w := range t.submitWritesSince(since) {
			if !w.done {
				busy = true
			}
		}
		t.mu.Lock()
		if t.reading > 0 {
			busy = true
		}
		t.mu.Unlock()
		if !busy {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// evidenceSince: the id or link a submission write's answer carried.
func (t *netTracker) evidenceSince(since time.Time) (string, string) {
	for _, w := range t.submitWritesSince(since) {
		if w.ok && (w.postID != "" || w.postURL != "") {
			return w.postID, w.postURL
		}
	}
	return "", ""
}

// logLines is the write log for the publish record, newest last, clipped.
func (t *netTracker) logLines(max int) []string {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	start := 0
	if len(t.log) > max {
		start = len(t.log) - max
	}
	var out []string
	for _, w := range t.log[start:] {
		out = append(out, w.at.Format("15:04:05")+" "+w.kind+" "+clipRunes(w.what+" → "+w.answer, 200))
	}
	return out
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
