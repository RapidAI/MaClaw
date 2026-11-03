package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// Form restore (d79). A draft typed into a form field is kept in the
// desktop ledger with a stable locator of that field. When a run attaches
// to the same page again (after a hand-back, a restart or an offline
// stretch) and that field is empty, its text is put back. It never
// reloads, navigates, presses, submits or publishes, never touches a
// password, hidden or file field, and never overwrites what is there.

type desktopFieldLocator struct {
	Tag   string `json:"tag"`
	ID    string `json:"id,omitempty"`
	Name  string `json:"name,omitempty"`
	Aria  string `json:"aria,omitempty"`
	Ph    string `json:"ph,omitempty"`
	CE    bool   `json:"ce,omitempty"`
	Index int    `json:"idx"`
}

const desktopFocusedFieldJS = `(() => { let e = document.activeElement; if (!e || e === document.body) return "";
 while (e && e.shadowRoot && e.shadowRoot.activeElement) e = e.shadowRoot.activeElement;
 const t = (e.type || "").toLowerCase(); if (["password","hidden","file"].includes(t)) return "";
 const ce = !!e.isContentEditable; const tag = e.tagName.toLowerCase();
 if (!ce && tag !== "input" && tag !== "textarea") return "";
 const all = Array.from(document.querySelectorAll(ce ? "[contenteditable]" : tag));
 return JSON.stringify({tag, id: e.id || "", name: e.getAttribute("name") || "", aria: e.getAttribute("aria-label") || "",
  ph: e.getAttribute("placeholder") || "", ce, idx: all.indexOf(e)}); })()`

func desktopFocusedFieldLocator(session *browser.BrowserAgentSession) string {
	if session == nil {
		return ""
	}
	raw, err := session.EvalValue(desktopFocusedFieldJS)
	if err != nil {
		return ""
	}
	var loc desktopFieldLocator
	if json.Unmarshal([]byte(strings.TrimSpace(raw)), &loc) != nil || loc.Tag == "" {
		return ""
	}
	if loc.ID == "" && loc.Name == "" && loc.Aria == "" && loc.Ph == "" && loc.Index < 0 {
		return ""
	}
	return strings.TrimSpace(raw)
}

func desktopLedgerRememberField(scope agentruntime.Scope, url, body, field string) {
	key := desktopLedgerKey(scope)
	body = desktopLedgerCapBody(body)
	if key == "" || strings.TrimSpace(url) == "" || body == "" || field == "" {
		return
	}
	desktopLedgerAdd(key, desktopLedgerEntry{Kind: "draft", Host: desktopLedgerHost(url), URL: strings.TrimSpace(url), Body: body, Field: field, Run: scope.RunID, Detail: "draft"})
}

func desktopRestoreSamePage(a, b string) bool {
	norm := func(u string) string {
		u = strings.TrimSpace(u)
		if i := strings.IndexByte(u, '#'); i >= 0 {
			u = u[:i]
		}
		return strings.TrimRight(u, "/")
	}
	return a != "" && norm(a) == norm(b)
}

// desktopRestorePlan picks the latest draft per field for this page.
func desktopRestorePlan(entries []desktopLedgerEntry, pageURL string) []desktopLedgerEntry {
	now := desktopLedgerNow()
	seen := map[string]bool{}
	var out []desktopLedgerEntry
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Kind != "draft" || e.Field == "" || e.Body == "" || !desktopRestoreSamePage(e.URL, pageURL) || now.Sub(e.At) > desktopPageStateWindow {
			continue
		}
		if seen[e.Field] {
			continue
		}
		seen[e.Field] = true
		out = append(out, e)
	}
	return out
}

func desktopRestoreFieldJS(field, body string) string {
	f, _ := json.Marshal(field)
	b, _ := json.Marshal(body)
	return fmt.Sprintf(`(() => { const L = JSON.parse(%s), v = %s;
 const sel = L.ce ? "[contenteditable]" : L.tag; const all = Array.from(document.querySelectorAll(sel));
 let e = (L.id && document.getElementById(L.id)) || all.find(x => (L.name && x.getAttribute("name") === L.name) || (L.aria && x.getAttribute("aria-label") === L.aria) || (L.ph && x.getAttribute("placeholder") === L.ph)) || (!L.id && !L.name && !L.aria && !L.ph ? all[L.idx] : null);
 if (!e) return "missing";
 const t = (e.type || "").toLowerCase(); if (["password","hidden","file"].includes(t)) return "unsafe";
 const cur = L.ce ? (e.innerText || "").trim() : (e.value || "").trim();
 if (cur !== "") return cur === v.trim() ? "same" : "kept";
 e.focus();
 if (L.ce) { document.execCommand("insertText", false, v); }
 else { const p = e.tagName === "TEXTAREA" ? HTMLTextAreaElement.prototype : HTMLInputElement.prototype;
  Object.getOwnPropertyDescriptor(p, "value").set.call(e, v); }
 e.dispatchEvent(new Event("input", {bubbles: true})); e.dispatchEvent(new Event("change", {bubbles: true}));
 return "restored"; })()`, string(f), string(b))
}

// desktopRestoreFormFields puts persisted drafts back into empty fields of
// the page the session is on. Quiet when there is nothing to restore.
func desktopRestoreFormFields(scope agentruntime.Scope, session *browser.BrowserAgentSession) string {
	if session == nil || !desktopSessionConnected(session) {
		return ""
	}
	page := session.CurrentURL()
	plan := desktopRestorePlan(desktopLedgerEntries(desktopLedgerKey(scope)), page)
	n := 0
	for _, e := range plan {
		if got, err := session.EvalValue(desktopRestoreFieldJS(e.Field, e.Body)); err == nil && got == "restored" {
			n++
		}
	}
	if n == 0 {
		return ""
	}
	return fmt.Sprintf("RESTORED %d form field(s) on this page from the desktop ledger (empty fields only; nothing reloaded, pressed or submitted). Check them before going on.", n)
}
