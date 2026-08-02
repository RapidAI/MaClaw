//go:build livesmoke

package browser

// Live smoke for the automation primitives against the real managed browser.
// Verifies the injected observe/click/type JS end-to-end (multi-candidate
// selectors, data-qa attribute naming, occlusion fallback, iframe offset,
// CJK input) — things Go unit tests cannot cover.
// Run: go test ./corelib/browser/ -tags livesmoke -run TestLiveAutomationPrimitives -v

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func liveTestPage(mux *http.ServeMux) {
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>live-test</title></head><body>
<h1>automation live test</h1>
<button id="b1" data-qa="launch" onclick="document.title='clicked-b1'">Launch</button>
<span data-testid="launch">decoy with same value under a different attribute</span>
<input name="title" type="text" placeholder="title here">
<div style="position:relative;width:200px;height:40px;margin-top:8px">
  <button id="b2" style="position:absolute;inset:0" onclick="document.title='clicked-b2'">Covered</button>
  <div style="position:absolute;inset:0;z-index:10;background:transparent"></div>
</div>
<iframe src="/frame" style="width:300px;height:80px;margin-top:8px"></iframe>
<div style="height:2000px"></div>
</body></html>`)
	})
	mux.HandleFunc("/frame", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><body><button id="b3" onclick="parent.document.title='clicked-b3'">Inner</button></body></html>`)
	})
}

func TestLiveAutomationPrimitives(t *testing.T) {
	mux := http.NewServeMux()
	liveTestPage(mux)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr, err := DiscoverOrLaunchPersistent()
	if err != nil {
		t.Skipf("managed browser unavailable: %v", err)
	}
	// GetSession's fast path ignores addr when a global session is already
	// alive — close it so we actually drive the managed persistent browser.
	CloseSession()
	session, err := GetSession(addr)
	if err != nil {
		t.Skipf("CDP session unavailable: %v", err)
	}
	// Leave the managed browser on a blank tab instead of a dead test URL.
	t.Cleanup(func() {
		_, _ = session.Navigate("about:blank")
	})
	if _, err := session.Navigate(srv.URL + "/"); err != nil {
		t.Fatalf("Navigate: %v", err)
	}

	// ── 1. observe: multi-candidate refs, correct attribute naming ──
	raw, err := session.Eval(browserObserveScript)
	if err != nil {
		t.Fatalf("observe eval: %v", err)
	}
	var payload struct {
		Refs []BrowserElementRef `json:"refs"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("parse observe payload: %v", err)
	}
	if len(payload.Refs) == 0 {
		t.Fatal("observe returned no refs")
	}
	var b1 *BrowserElementRef
	for i := range payload.Refs {
		ref := &payload.Refs[i]
		if len(ref.SelectorCandidates) == 0 {
			t.Fatalf("ref %s (%s %s) has no selector candidates", ref.Ref, ref.Tag, ref.Name)
		}
		if ref.Selector == "" {
			t.Fatalf("ref %s has empty primary selector", ref.Ref)
		}
		if strings.Contains(ref.Selector, "#b1") || strings.Contains(strings.Join(ref.SelectorCandidates, " "), "#b1") {
			b1 = ref
		}
	}
	if b1 == nil {
		t.Fatal("observe did not find button #b1")
	}
	joined := strings.Join(b1.SelectorCandidates, " ")
	if !strings.Contains(joined, `[data-qa="launch"]`) {
		t.Fatalf("b1 candidates missing data-qa selector: %v", b1.SelectorCandidates)
	}
	if strings.Contains(joined, `[data-testid="launch"]`) {
		t.Fatalf("b1 candidates must not attribute data-qa value to data-testid: %v", b1.SelectorCandidates)
	}
	// Non-legacy candidates must be unique on the page.
	for _, cand := range b1.SelectorCandidates[:len(b1.SelectorCandidates)-1] {
		countRaw, err := session.Eval(fmt.Sprintf(`String(document.querySelectorAll(%q).length)`, cand))
		if err != nil {
			t.Fatalf("count eval for %q: %v", cand, err)
		}
		if countRaw != "1" {
			t.Fatalf("candidate %q matches %s elements, want exactly 1", cand, countRaw)
		}
	}

	resetTitle := func() {
		if _, err := session.Eval(`document.title=''`); err != nil {
			t.Fatalf("reset title: %v", err)
		}
	}
	title := func() string {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			v, err := session.Eval(`document.title`)
			if err == nil && v != "" {
				return v
			}
			time.Sleep(100 * time.Millisecond)
		}
		v, _ := session.Eval(`document.title`)
		return v
	}

	// ── 2. plain coordinate click ──
	resetTitle()
	if err := session.Click("#b1"); err != nil {
		t.Fatalf("click #b1: %v", err)
	}
	if got := title(); got != "clicked-b1" {
		t.Fatalf("after click #b1 title = %q, want clicked-b1", got)
	}

	// ── 3. occluded element → JS click fallback ──
	resetTitle()
	if err := session.Click("#b2"); err != nil {
		t.Fatalf("click #b2 (occluded): %v", err)
	}
	if got := title(); got != "clicked-b2" {
		t.Fatalf("after click #b2 title = %q, want clicked-b2 (occlusion fallback)", got)
	}

	// ── 4. iframe element: frame search + offset accumulation ──
	resetTitle()
	if err := session.Click("#b3"); err != nil {
		t.Fatalf("click #b3 (iframe): %v", err)
	}
	if got := title(); got != "clicked-b3" {
		t.Fatalf("after click #b3 title = %q, want clicked-b3 (iframe offset)", got)
	}

	// ── 5. CJK input lands and verifies ──
	const want = "你好，世界 Hello"
	if err := session.Type("input[name=title]", want); err != nil {
		t.Fatalf("type CJK: %v", err)
	}
	got, err := session.Eval(`document.querySelector('input[name=title]').value`)
	if err != nil {
		t.Fatalf("read input value: %v", err)
	}
	if got != want {
		t.Fatalf("input value = %q, want %q", got, want)
	}

	// ── 6. scroll actually moves the page ──
	if _, err := session.Eval(`window.scrollTo(0, 0), 'ok'`); err != nil {
		t.Fatalf("reset scroll: %v", err)
	}
	if err := session.Scroll(0, 600); err != nil {
		t.Fatalf("scroll: %v", err)
	}
	time.Sleep(300 * time.Millisecond)
	sy, err := session.Eval(`String(Math.round(window.scrollY))`)
	if err != nil {
		t.Fatalf("read scrollY: %v", err)
	}
	if sy == "0" {
		t.Fatal("scroll did not move the page (scrollY still 0)")
	}
}

// TestLiveSearchExtractionResilience runs the layered search extraction JS
// against real Chrome with local fixture pages: current markup, renamed
// classes (secondary layer), and unrecognizable markup (generic fallback).
func TestLiveSearchExtractionResilience(t *testing.T) {
	mux := http.NewServeMux()
	page := func(body string) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprint(w, "<!doctype html><html><head><title>fixture</title></head><body>"+body+"</body></html>")
		}
	}
	mux.HandleFunc("/bing-full", page(`
<ol id="b_results">
<li class="b_algo"><h2><a href="https://example.com/one">Example One Result</a></h2><div class="b_caption"><p>Snippet one</p></div></li>
<li class="b_algo"><h2><a href="https://example.com/two">Example Two Result</a></h2><p class="b_lineclamp2">Snippet two</p></li>
</ol>`))
	mux.HandleFunc("/bing-renamed", page(`
<ol id="b_results">
<li class="zx_q9"><h2><a href="https://example.com/renamed">Renamed Classes Result</a></h2><p>Renamed snippet</p></li>
</ol>`))
	mux.HandleFunc("/weird", page(`
<main>
<div class="card"><a href="https://example.com/alpha"><span>A fairly long organic result title</span></a><p>Alpha description</p></div>
<div class="card"><a href="https://example.com/beta"><h3>Beta result heading</h3></a><p>Beta description</p></div>
<nav><a href="https://example.com/nav">Navigation link that is long enough</a></nav>
</main>`))
	// No h3-in-anchor structure, so google's primary layer yields nothing and
	// the generic fallback must take over (first non-empty layer wins).
	mux.HandleFunc("/weird-noh3", page(`
<main>
<div class="card"><a href="https://example.com/alpha"><span>A fairly long organic result title</span></a><p>Alpha description</p></div>
<div class="card"><a href="https://example.com/beta"><span class="t">Another organic result title here</span></a><p>Beta description</p></div>
</main>`))
	mux.HandleFunc("/google-full", page(`
<div class="MjjYud"><a href="https://example.com/g1"><h3>Google result one</h3></a><div class="VwiC3b">Google snippet one</div></div>
<a href="https://www.google.com/search?q=self"><h3>Google self link must be excluded</h3></a>`))
	srv := httptest.NewServer(mux)
	defer srv.Close()

	addr, err := DiscoverOrLaunchPersistent()
	if err != nil {
		t.Skipf("managed browser unavailable: %v", err)
	}
	CloseSession()
	session, err := GetSession(addr)
	if err != nil {
		t.Skipf("CDP session unavailable: %v", err)
	}
	t.Cleanup(func() {
		_, _ = session.Navigate("about:blank")
	})

	extract := func(engine, path string) []SearchHit {
		t.Helper()
		if _, err := session.Navigate(srv.URL + path); err != nil {
			t.Fatalf("Navigate %s: %v", path, err)
		}
		raw, err := session.Eval(searchExtractionJS(engine, 5))
		if err != nil {
			t.Fatalf("extraction eval %s %s: %v", engine, path, err)
		}
		var hits []SearchHit
		if err := json.Unmarshal([]byte(raw), &hits); err != nil {
			t.Fatalf("parse extraction %s %s: %v (raw=%q)", engine, path, err, raw)
		}
		return hits
	}

	// Primary layer: current bing markup.
	hits := extract("bing", "/bing-full")
	if len(hits) != 2 || hits[0].URL != "https://example.com/one" || hits[0].Snippet != "Snippet one" {
		t.Fatalf("bing primary: %+v", hits)
	}
	// Secondary layer: b_algo renamed, #b_results survives.
	hits = extract("bing", "/bing-renamed")
	if len(hits) != 1 || hits[0].URL != "https://example.com/renamed" {
		t.Fatalf("bing secondary (renamed classes): %+v", hits)
	}
	// Generic fallback: no recognizable markup at all; nav link excluded.
	hits = extract("bing", "/weird")
	if len(hits) != 2 {
		t.Fatalf("bing generic fallback: %+v", hits)
	}
	if hits[0].URL != "https://example.com/alpha" || hits[1].URL != "https://example.com/beta" {
		t.Fatalf("bing generic fallback urls: %+v", hits)
	}
	// Google primary + self-link exclusion.
	hits = extract("google", "/google-full")
	if len(hits) != 1 || hits[0].URL != "https://example.com/g1" || hits[0].Snippet != "Google snippet one" {
		t.Fatalf("google primary: %+v", hits)
	}
	// Google generic fallback on markup its primary layer can't match.
	hits = extract("google", "/weird-noh3")
	if len(hits) != 2 {
		t.Fatalf("google generic fallback: %+v", hits)
	}
}
