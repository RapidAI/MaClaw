package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// SearchHit is one organic result extracted from a real search-engine page.
type SearchHit struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

type SearchOptions struct {
	// HumanAssist keeps a detected verification page open so the user can
	// complete a CAPTCHA or slider manually. It never automates the challenge.
	HumanAssist bool
}

// browserSearchGate serializes browser-side searches (tab creation in the
// shared persistent browser) while still allowing queued callers to cancel.
var browserSearchGate = make(chan struct{}, 1)

// browserSearchCommandTimeout caps each CDP command by both the command's
// ordinary timeout and the caller's remaining search budget. Without this, an
// 8-second browser search could still block for a 15-second CDP timeout.
func browserSearchCommandTimeout(ctx context.Context, fallback time.Duration) time.Duration {
	if ctx == nil {
		return fallback
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		return fallback
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return time.Millisecond
	}
	if remaining < fallback {
		return remaining
	}
	return fallback
}

// SearchViaBrowser searches the web with the managed real browser as the
// last-resort path when every HTTP-level endpoint fails: the page is loaded
// by a real Chrome (cookies, TLS fingerprint, JS execution), so engines that
// block plain HTTP clients (Google, and captcha'd Bing/Baidu) still work.
// Results are extracted from the live DOM, not from raw HTML.
//
// SearchViaBrowser searches one explicitly selected engine. The caller owns
// ordering and fallback policy; this layer must never silently substitute a
// different engine.
func SearchViaBrowser(ctx context.Context, engineID, query string, maxResults int) ([]SearchHit, error) {
	return SearchViaBrowserWithOptions(ctx, engineID, query, maxResults, SearchOptions{})
}

func SearchViaBrowserWithOptions(ctx context.Context, engineID, query string, maxResults int, opts SearchOptions) ([]SearchHit, error) {
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
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	engineID = strings.ToLower(strings.TrimSpace(engineID))
	engineName := ""
	searchURL := ""
	switch engineID {
	case "bing", "bing_cn":
		engineName = "bing"
		searchURL = "https://cn.bing.com/search?q=" + url.QueryEscape(query) + fmt.Sprintf("&count=%d", maxResults)
	case "google":
		engineName = "google"
		searchURL = "https://www.google.com/search?q=" + url.QueryEscape(query) + fmt.Sprintf("&num=%d&hl=zh-CN", maxResults)
	default:
		return nil, fmt.Errorf("unsupported browser search engine %q", engineID)
	}

	select {
	case browserSearchGate <- struct{}{}:
		defer func() { <-browserSearchGate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	addr := ""
	if sess := mostRecentLiveAgentSession(); sess != nil {
		addr = sess.Addr
	}
	if addr == "" {
		var err error
		addr, err = DiscoverOrLaunchPersistentCtx(ctx)
		if err != nil {
			return nil, fmt.Errorf("no usable browser: %w", err)
		}
	}

	hits, err := searchEngineViaBrowserWithOptions(ctx, addr, engineName, searchURL, maxResults, opts)
	if err != nil {
		return nil, fmt.Errorf("browser search failed (%s: %w)", engineName, err)
	}
	if len(hits) == 0 {
		return nil, fmt.Errorf("browser search failed (%s: no results)", engineName)
	}
	// Do not log the query text: search terms are user data and the download log
	// may be readable by local diagnostic tooling.
	downloadLogf("[browser-search] engine=%s hits=%d", engineName, len(hits))
	return hits, nil
}

// searchEngineViaBrowser opens one tab on the given engine results page and
// polls the DOM until organic results appear.
func searchEngineViaBrowser(ctx context.Context, cdpAddr, engine, searchURL string, maxResults int) ([]SearchHit, error) {
	return searchEngineViaBrowserWithOptions(ctx, cdpAddr, engine, searchURL, maxResults, SearchOptions{})
}

func searchEngineViaBrowserWithOptions(ctx context.Context, cdpAddr, engine, searchURL string, maxResults int, opts SearchOptions) ([]SearchHit, error) {
	timeout := 15 * time.Second
	if opts.HumanAssist {
		timeout = 90 * time.Second
	}
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline); remaining > 0 && remaining < timeout {
			timeout = remaining
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	bwsURL, err := browserWebSocketURLCtx(ctx, cdpAddr)
	if err != nil {
		return nil, err
	}
	bws, err := ConnectCDP(bwsURL)
	if err != nil {
		return nil, fmt.Errorf("connect browser endpoint: %w", err)
	}
	defer bws.Close()
	contextRaw, err := bws.Send("Target.createBrowserContext", map[string]interface{}{}, browserSearchCommandTimeout(ctx, DefaultCmdTimeout))
	if err != nil {
		return nil, fmt.Errorf("Target.createBrowserContext: %w", err)
	}
	var browserContext struct {
		BrowserContextID string `json:"browserContextId"`
	}
	if err := json.Unmarshal(contextRaw, &browserContext); err != nil || browserContext.BrowserContextID == "" {
		return nil, fmt.Errorf("parse browser context result: %v", err)
	}
	defer func() {
		// Disposing the isolated context closes every target created inside it.
		// Keep cleanup best-effort and short so an expired search budget does not
		// turn into several extra seconds of user-visible latency.
		_, _ = bws.Send("Target.disposeBrowserContext", map[string]interface{}{"browserContextId": browserContext.BrowserContextID}, 500*time.Millisecond)
	}()

	created, err := bws.Send("Target.createTarget", map[string]interface{}{"url": searchURL, "browserContextId": browserContext.BrowserContextID}, browserSearchCommandTimeout(ctx, DefaultCmdTimeout))
	if err != nil {
		return nil, fmt.Errorf("Target.createTarget: %w", err)
	}
	var tgt struct {
		TargetID string `json:"targetId"`
	}
	if err := json.Unmarshal(created, &tgt); err != nil || tgt.TargetID == "" {
		return nil, fmt.Errorf("parse createTarget result: %v body_len=%d", err, len(created))
	}
	pageWS, err := pageWSURLForTargetCtx(ctx, cdpAddr, tgt.TargetID, 5*time.Second)
	if err != nil {
		return nil, err
	}
	pws, err := ConnectCDP(pageWS)
	if err != nil {
		return nil, fmt.Errorf("connect page target: %w", err)
	}
	defer pws.Close()

	expr := searchExtractionJS(engine, maxResults)
	verificationSeen := false
	for {
		raw, err := pws.Send("Runtime.evaluate", map[string]interface{}{
			"expression":    expr,
			"returnByValue": true,
		}, browserSearchCommandTimeout(ctx, DefaultCmdTimeout))
		if err == nil {
			if hits := parseSearchEvalResult(raw); len(hits) > 0 {
				return hits, nil
			}
			if opts.HumanAssist && !verificationSeen && searchPageNeedsHumanVerification(ctx, pws) {
				verificationSeen = true
				// Bring the real browser tab to the foreground so the user can
				// complete the challenge. The page stays alive while this loop
				// polls for results, then is cleaned up on success or timeout.
				if _, activateErr := bws.Send("Target.activateTarget", map[string]interface{}{"targetId": tgt.TargetID}, browserSearchCommandTimeout(ctx, DefaultCmdTimeout)); activateErr != nil {
					downloadLogf("[browser-search] could not foreground verification page engine=%s", engine)
				} else {
					downloadLogf("[browser-search] waiting for manual verification engine=%s", engine)
				}
			}
		}
		select {
		case <-ctx.Done():
			if verificationSeen {
				return nil, fmt.Errorf("verification challenge was not completed")
			}
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				return nil, fmt.Errorf("no organic results on the results page: %w", ctx.Err())
			}
			return nil, ctx.Err()
		case <-time.After(700 * time.Millisecond):
		}
	}
}

func searchPageNeedsHumanVerification(ctx context.Context, pws *CDPClient) bool {
	raw, err := pws.Send("Runtime.evaluate", map[string]interface{}{
		"expression":    `(() => { function textFrom(doc){ let t=''; try { t += String(doc.body ? (doc.body.innerText || doc.body.textContent || '') : ''); } catch(e){} try { if (doc.querySelector('[class*="captcha"], [id*="captcha"]')) t += ' captcha'; } catch(e){} let all=[]; try { all = doc.querySelectorAll('*'); } catch(e){ return t; } for (const el of all){ if (el.shadowRoot){ try { t += ' ' + (el.shadowRoot.innerText || el.shadowRoot.textContent || ''); if (el.shadowRoot.querySelector('[class*="captcha"], [id*="captcha"]')) t += ' captcha'; } catch(e){} } } let frames=[]; try { frames = doc.querySelectorAll('iframe,frame'); } catch(e){ return t; } for (const f of frames){ try { if (/captcha/i.test(f.src||'')) return 'captcha'; if (f.contentDocument) t += ' ' + textFrom(f.contentDocument); } catch(e){} } return t; } const t = textFrom(document).toLowerCase(); return /captcha|verify you are human|unusual traffic|安全验证|人机验证|拖动滑块|验证码/.test(t); })()`,
		"returnByValue": true,
	}, browserSearchCommandTimeout(ctx, DefaultCmdTimeout))
	if err != nil {
		return false
	}
	var envelope struct {
		Result struct {
			Value bool `json:"value"`
		} `json:"result"`
	}
	return json.Unmarshal(raw, &envelope) == nil && envelope.Result.Value
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
//
// Each engine expression is layered: primary (current known markup) →
// secondary (structural, class-agnostic anchors like #b_results) → generic
// (engine-agnostic link heuristics). The first layer that yields results
// wins, so an engine renaming its CSS classes degrades gracefully instead
// of returning zero results.
func searchExtractionJS(engine string, maxResults int) string {
	switch engine {
	case "google":
		return fmt.Sprintf(`(() => {
  const MAX = %d;
  function primary() {
    const seen = new Set(); const out = [];
    for (const h3 of document.querySelectorAll('a h3')) {
      const a = h3.closest('a');
      if (!a || !a.href || !a.href.startsWith('http')) continue;
      const u = a.href;
      if (/google\.[a-z.]+\/(search|maps|preferences|intl)|accounts\.google|support\.google|policies\.google|webcache/.test(u)) continue;
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
      if (out.length >= MAX) break;
    }
    return out;
  }
  %s
  const p = primary();
  if (p.length) return JSON.stringify(p);
  return JSON.stringify(genericExtract());
})()`, maxResults, genericSearchExtractJS(
			`/google\.[a-z.]+\/(search|maps|preferences|intl|accounts)|accounts\.google|support\.google|policies\.google|webcache/`))
	default: // bing
		return fmt.Sprintf(`(() => {
  const MAX = %d;
  function primary() {
    const out = [];
    for (const li of document.querySelectorAll('li.b_algo')) {
      const a = li.querySelector('h2 a');
      if (!a || !a.href) continue;
      const p = li.querySelector('.b_caption p, .b_lineclamp2, .b_lineclamp3, .b_lineclamp4');
      out.push({title: (a.textContent || '').trim(), url: a.href, snippet: p ? (p.textContent || '').trim() : ''});
      if (out.length >= MAX) break;
    }
    return out;
  }
  function secondary() {
    // Class-agnostic: the #b_results list id is stabler than b_algo classes.
    const out = [];
    const list = document.querySelector('ol#b_results');
    if (!list) return out;
    for (const li of list.querySelectorAll(':scope > li')) {
      const a = li.querySelector('h2 a');
      if (!a || !a.href || !a.href.startsWith('http')) continue;
      const title = (a.textContent || '').trim();
      if (!title) continue;
      const p = li.querySelector('p');
      out.push({title, url: a.href, snippet: p ? (p.textContent || '').trim() : ''});
      if (out.length >= MAX) break;
    }
    return out;
  }
  %s
  const p = primary();
  if (p.length) return JSON.stringify(p);
  const s = secondary();
  if (s.length) return JSON.stringify(s);
  return JSON.stringify(genericExtract());
})()`, maxResults, genericSearchExtractJS(
			`/\/\/([^/]+\.)?bing\.com\/(search|images|videos|maps|news)|\/\/(login\.live|account\.microsoft|support\.microsoft)\.com/`))
	}
}

// genericSearchExtractJS is the engine-agnostic structural fallback used
// when every engine-specific selector layer yields nothing (e.g. the engine
// renamed all its classes). It scans content-area links and keeps those
// that look like organic results: http(s) href, outside nav/header/footer,
// not pointing back at the engine itself, and with either a heading or a
// reasonably long anchor text. The returned JS references MAX from the
// enclosing IIFE.
func genericSearchExtractJS(excludeRe string) string {
	return fmt.Sprintf(`function genericExtract() {
  const exclude = %s;
  const seen = new Set(); const out = [];
  const root = document.querySelector('main') || document.body;
  if (!root) return out;
  for (const a of root.querySelectorAll('a[href^="http"]')) {
    if (out.length >= MAX) break;
    if (a.closest('nav,header,footer,[role="navigation"],[role="banner"],[role="contentinfo"]')) continue;
    const u = a.href;
    if (exclude.test(u)) continue;
    const heading = a.querySelector('h1,h2,h3') || a.closest('h1,h2,h3');
    let title = (heading ? heading.textContent : (a.innerText || a.textContent || '')).trim().replace(/\s+/g, ' ');
    if (title.length < 8) continue;
    if (title.length > 300) title = title.slice(0, 300);
    if (seen.has(u)) continue;
    seen.add(u);
    let snippet = '';
    const block = a.closest('li,article,div');
    if (block) {
      const p = block.querySelector('p');
      if (p) snippet = (p.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 500);
    }
    out.push({title, url: u, snippet});
  }
  return out;
}`, excludeRe)
}
