package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SearchHit is one organic result extracted from a real search-engine page.
type SearchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// browserSearchMu serializes browser-side searches (tab creation in the
// shared persistent browser).
var browserSearchMu sync.Mutex

// SearchViaBrowser searches the web with the managed real browser as the
// last-resort path when every HTTP-level endpoint fails: the page is loaded
// by a real Chrome (cookies, TLS fingerprint, JS execution), so engines that
// block plain HTTP clients (Google, and captcha'd Bing/Baidu) still work.
// Results are extracted from the live DOM, not from raw HTML.
//
// Engines are tried in order: Bing (cn.bing.com, reachable everywhere), then
// Google (needs a reachable route; better coverage when available).
func SearchViaBrowser(ctx context.Context, query string, maxResults int) ([]SearchHit, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return nil, fmt.Errorf("query is empty")
	}
	if maxResults <= 0 {
		maxResults = 8
	}
	if ctx == nil {
		ctx = context.Background()
	}

	addr := ""
	if sess := mostRecentLiveAgentSession(); sess != nil {
		addr = sess.Addr
	}
	if addr == "" {
		var err error
		addr, err = DiscoverOrLaunchPersistent()
		if err != nil {
			return nil, fmt.Errorf("no usable browser: %w", err)
		}
	}

	browserSearchMu.Lock()
	defer browserSearchMu.Unlock()

	engines := []struct {
		name string
		url  string
	}{
		{"bing", "https://cn.bing.com/search?q=" + url.QueryEscape(query) + fmt.Sprintf("&count=%d", maxResults)},
		{"google", "https://www.google.com/search?q=" + url.QueryEscape(query) + fmt.Sprintf("&num=%d&hl=zh-CN", maxResults)},
	}
	var failures []string
	for _, e := range engines {
		hits, err := searchEngineViaBrowser(ctx, addr, e.name, e.url, maxResults)
		if err == nil && len(hits) > 0 {
			// Do not log the query text: search terms are user data and the
			// download log is 0644 on disk.
			downloadLogf("[browser-search] engine=%s hits=%d", e.name, len(hits))
			return hits, nil
		}
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: %v", e.name, err))
		} else {
			failures = append(failures, fmt.Sprintf("%s: no results", e.name))
		}
		if ctx.Err() != nil {
			break
		}
	}
	return nil, fmt.Errorf("browser search failed (%s)", strings.Join(failures, "; "))
}

// searchEngineViaBrowser opens one tab on the given engine results page and
// polls the DOM until organic results appear.
func searchEngineViaBrowser(ctx context.Context, cdpAddr, engine, searchURL string, maxResults int) ([]SearchHit, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()

	bwsURL, err := browserWebSocketURL(cdpAddr)
	if err != nil {
		return nil, err
	}
	bws, err := ConnectCDP(bwsURL)
	if err != nil {
		return nil, fmt.Errorf("connect browser endpoint: %w", err)
	}
	defer bws.Close()

	created, err := bws.Send("Target.createTarget", map[string]interface{}{"url": searchURL}, DefaultCmdTimeout)
	if err != nil {
		return nil, fmt.Errorf("Target.createTarget: %w", err)
	}
	var tgt struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(created, &tgt); err != nil || tgt.TargetID == "" {
		return nil, fmt.Errorf("parse createTarget result: %v body_len=%d", err, len(created))
	}
	defer func() {
		_, _ = bws.Send("Target.closeTarget", map[string]interface{}{"targetId": tgt.TargetID}, 5*time.Second)
	}()

	pageWS, err := pageWSURLForTarget(cdpAddr, tgt.TargetID, 5*time.Second)
	if err != nil {
		return nil, err
	}
	pws, err := ConnectCDP(pageWS)
	if err != nil {
		return nil, fmt.Errorf("connect page target: %w", err)
	}
	defer pws.Close()

	expr := searchExtractionJS(engine, maxResults)
	deadline := time.Now().Add(25 * time.Second)
	for {
		raw, err := pws.Send("Runtime.evaluate", map[string]interface{}{
			"expression":    expr,
			"returnByValue": true,
		}, DefaultCmdTimeout)
		if err == nil {
			if hits := parseSearchEvalResult(raw); len(hits) > 0 {
				return hits, nil
			}
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("no organic results on the results page")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(700 * time.Millisecond):
		}
	}
}

// parseSearchEvalResult decodes the Runtime.evaluate response: the JS
// expression returns a JSON string of [{title,url,snippet}].
func parseSearchEvalResult(raw json.RawMessage) []SearchHit {
	var envelope struct {
		Result struct {
			Value json.RawMessage `json:"value"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil || len(envelope.Result.Value) == 0 {
		return nil
	}
	var payload string
	if err := json.Unmarshal(envelope.Result.Value, &payload); err != nil || payload == "" {
		return nil
	}
	var hits []SearchHit
	if err := json.Unmarshal([]byte(payload), &hits); err != nil {
		return nil
	}
	out := hits[:0]
	for _, h := range hits {
		if h.Title != "" && strings.HasPrefix(h.URL, "http") {
			out = append(out, h)
		}
	}
	return out
}

// searchExtractionJS returns the DOM extraction expression per engine. It
// returns a JSON string of [{title,url,snippet}].
func searchExtractionJS(engine string, maxResults int) string {
	switch engine {
	case "google":
		return fmt.Sprintf(`(() => {
  const seen = new Set(); const out = [];
  for (const h3 of document.querySelectorAll('a h3')) {
    const a = h3.closest('a');
    if (!a || !a.href || !a.href.startsWith('http')) continue;
    const u = a.href;
    if (/google\.(com|com\.cn)\/(search|maps|preferences|intl)|accounts\.google|support\.google|policies\.google|webcache/.test(u)) continue;
    const title = (h3.textContent || '').trim();
    if (!title || seen.has(u)) continue;
    seen.add(u);
    let snippet = '';
    const block = a.closest('.MjjYud,.g,.tF2Cxc,.Gx5Zad,.hlcw0c') || (a.parentElement && a.parentElement.parentElement);
    if (block) {
      const s = block.querySelector('.VwiC3b,[data-sncf],.lEBKkf');
      if (s) snippet = (s.textContent || '').trim();
    }
    out.push({title, url: u, snippet});
    if (out.length >= %d) break;
  }
  return JSON.stringify(out);
})()`, maxResults)
	default: // bing
		return fmt.Sprintf(`(() => {
  const out = [];
  for (const li of document.querySelectorAll('li.b_algo')) {
    const a = li.querySelector('h2 a');
    if (!a || !a.href) continue;
    const p = li.querySelector('.b_caption p, .b_lineclamp2, .b_lineclamp3, .b_lineclamp4');
    out.push({title: (a.textContent || '').trim(), url: a.href, snippet: p ? (p.textContent || '').trim() : ''});
    if (out.length >= %d) break;
  }
  return JSON.stringify(out);
})()`, maxResults)
	}
}
