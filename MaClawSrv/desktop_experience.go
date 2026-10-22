package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/agentruntime"
	"github.com/RapidAI/CodeClaw/corelib/browser"
)

// Per-site experience (MACLAW_DESKTOP_EXPERIENCE, default on). Notes are
// learned only from validated outcomes: a method whose text was read back in
// place (verify-after-act) becomes a "worked" note; a method that failed with
// a classified error (not_applied, cross_origin_block, ...) becomes an
// "avoid" counter-example. Notes are built from fixed templates (method and
// error class names), never from typed text, URLs with paths, cookies or
// arguments. One small JSON file per tenant+user (a bot has its own user),
// capped per site and in total. When a site is visited (navigate) or was the
// last site of this user, its notes go into the result / tool guidance.

const desktopExperienceEnv = "MACLAW_DESKTOP_EXPERIENCE"

const (
	desktopExperienceMaxSites    = 40
	desktopExperienceMaxPerKind  = 6
	desktopExperienceMaxFileSize = 32 << 10
)

// desktopExperienceRoot overrides the store directory in tests.
var desktopExperienceRoot = ""

var desktopExperienceMu sync.Mutex

type desktopSiteNote struct {
	Note  string `json:"note"`
	Count int    `json:"count"`
	Last  int64  `json:"last"`
}

type desktopSiteExperience struct {
	Worked []desktopSiteNote `json:"worked,omitempty"`
	Avoid  []desktopSiteNote `json:"avoid,omitempty"`
	Seen   int64             `json:"seen"`
}

type desktopExperienceFile struct {
	LastSite string                            `json:"last_site,omitempty"`
	Sites    map[string]*desktopSiteExperience `json:"sites"`
}

func desktopExperienceEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(desktopExperienceEnv))) {
	case "0", "off", "false", "no":
		return false
	}
	return true
}

func desktopExperiencePath(scope agentruntime.Scope) string {
	root := desktopExperienceRoot
	if root == "" {
		root = filepath.Join(corelib.MaclawDataDir(), "desktop-site-experience")
	}
	sum := sha256.Sum256([]byte(scope.TenantID + "\x00" + scope.UserID))
	return filepath.Join(root, hex.EncodeToString(sum[:12])+".json")
}

func desktopExperienceLoad(path string) *desktopExperienceFile {
	f := &desktopExperienceFile{Sites: map[string]*desktopSiteExperience{}}
	raw, err := os.ReadFile(path)
	if err == nil && len(raw) <= desktopExperienceMaxFileSize*2 {
		_ = json.Unmarshal(raw, f)
	}
	if f.Sites == nil {
		f.Sites = map[string]*desktopSiteExperience{}
	}
	return f
}

func desktopExperienceSave(path string, f *desktopExperienceFile) {
	// Oldest sites go first until the file fits.
	for {
		raw, _ := json.Marshal(f)
		if len(raw) <= desktopExperienceMaxFileSize && len(f.Sites) <= desktopExperienceMaxSites {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return
			}
			tmp := path + ".tmp"
			if os.WriteFile(tmp, raw, 0o600) == nil {
				_ = os.Rename(tmp, path)
			}
			return
		}
		oldest, at := "", int64(1<<62)
		for site, e := range f.Sites {
			if e.Seen < at {
				oldest, at = site, e.Seen
			}
		}
		if oldest == "" {
			return
		}
		delete(f.Sites, oldest)
	}
}

func desktopAddNote(list []desktopSiteNote, note string, now int64) []desktopSiteNote {
	for i := range list {
		if list[i].Note == note {
			list[i].Count++
			list[i].Last = now
			return list
		}
	}
	list = append(list, desktopSiteNote{Note: note, Count: 1, Last: now})
	sort.SliceStable(list, func(i, j int) bool { return list[i].Last > list[j].Last })
	if len(list) > desktopExperienceMaxPerKind {
		list = list[:desktopExperienceMaxPerKind]
	}
	return list
}

// desktopExperienceOutcome is the validated lesson of one step, if any:
// worked=true for a read-back-verified method, false for a classified failure.
func desktopExperienceOutcome(intent, out string, err error) (note string, worked bool, ok bool) {
	if err == nil && strings.Contains(out, desktopAppliedMarker) {
		return fmt.Sprintf("%s entered text and it was verified by read-back", desktopIntentLabel(intent)), true, true
	}
	if class := desktopErrorClass(out, err); class != "" {
		return fmt.Sprintf("%s failed with %s", desktopIntentLabel(intent), class), false, true
	}
	return "", false, false
}

func desktopIntentLabel(intent string) string {
	switch intent {
	case "type":
		return "browser type (DOM ref)"
	case "script":
		return "page script DOM write"
	case "app_run:text_input", "computer:text_input":
		return "real keyboard/clipboard input (" + strings.SplitN(intent, ":", 2)[0] + ")"
	}
	return intent
}

// desktopExperienceRecord stores the lesson of one step for the site.
func desktopExperienceRecord(scope agentruntime.Scope, site, action string, args map[string]any, out string, err error) {
	if !desktopExperienceEnabled() || site == "" || desktopRepeatExempt[action] {
		return
	}
	note, worked, ok := desktopExperienceOutcome(desktopIntentClass(action, args), out, err)
	if !ok {
		return
	}
	desktopExperienceMu.Lock()
	defer desktopExperienceMu.Unlock()
	path := desktopExperiencePath(scope)
	f := desktopExperienceLoad(path)
	e := f.Sites[site]
	if e == nil {
		e = &desktopSiteExperience{}
		f.Sites[site] = e
	}
	now := time.Now().Unix()
	e.Seen = now
	if worked {
		e.Worked = desktopAddNote(e.Worked, note, now)
	} else {
		e.Avoid = desktopAddNote(e.Avoid, note, now)
	}
	f.LastSite = site
	desktopExperienceSave(path, f)
	log.Printf("[desktop-experience] write site=%s worked=%t note=%q", site, worked, note)
	kind := "avoid"
	if worked {
		kind = "worked"
	}
	desktopEmit(scope, desktopEvent{Event: "experience_write", Site: site, Action: action, Result: kind, Reason: desktopStepClip(note, 160)})
}

// desktopExperienceNoteSite remembers the last site this user worked on.
func desktopExperienceNoteSite(scope agentruntime.Scope, site string) {
	if !desktopExperienceEnabled() || site == "" {
		return
	}
	desktopExperienceMu.Lock()
	defer desktopExperienceMu.Unlock()
	path := desktopExperiencePath(scope)
	f := desktopExperienceLoad(path)
	if f.LastSite == site {
		return
	}
	if _, known := f.Sites[site]; !known {
		// Only sites with lessons are kept; the last-site pointer alone is
		// not worth a write.
		return
	}
	f.LastSite = site
	desktopExperienceSave(path, f)
}

// desktopExperienceText is the guidance for site ("" = this user's last
// site), or "" when nothing is known. Seeded site playbooks (desktopSeedNotes)
// come first, then notes learned from verified runs. Every hit is logged and
// written to the run event stream as experience_hit.
func desktopExperienceText(scope agentruntime.Scope, site string) string {
	if !desktopExperienceEnabled() {
		return ""
	}
	desktopExperienceMu.Lock()
	f := desktopExperienceLoad(desktopExperiencePath(scope))
	desktopExperienceMu.Unlock()
	if site == "" {
		site = f.LastSite
	}
	seed := desktopSeedNotesFor(site)
	e := f.Sites[site]
	learned := e != nil && len(e.Worked)+len(e.Avoid) > 0
	if !learned && len(seed) == 0 {
		return ""
	}
	var b strings.Builder
	if len(seed) > 0 {
		fmt.Fprintf(&b, "Site playbook for %s:", site)
		for _, n := range seed {
			fmt.Fprintf(&b, " %s;", n)
		}
		b.WriteString(" ")
	}
	if learned {
		fmt.Fprintf(&b, "Experience on %s from earlier verified runs:", site)
		for _, n := range e.Worked {
			fmt.Fprintf(&b, " worked: %s (x%d);", n.Note, n.Count)
		}
		for _, n := range e.Avoid {
			fmt.Fprintf(&b, " avoid: %s (x%d);", n.Note, n.Count)
		}
	}
	source := "learned"
	switch {
	case len(seed) > 0 && learned:
		source = "seed+learned"
	case len(seed) > 0:
		source = "seed"
	}
	nLearned := 0
	if learned {
		nLearned = len(e.Worked) + len(e.Avoid)
	}
	log.Printf("[desktop-experience] hit site=%s source=%s seed=%d learned=%d", site, source, len(seed), nLearned)
	desktopEmit(scope, desktopEvent{Event: "experience_hit", Site: site, Result: source, Reason: fmt.Sprintf("seed=%d learned=%d", len(seed), nLearned)})
	return strings.TrimSuffix(strings.TrimSpace(b.String()), ";") + ". "
}

// desktopSeedNotes are fixed playbooks for sites whose editors defeat the
// generic methods. Keyed by registrable domain; subdomains inherit.
var desktopSeedNotes = map[string][]string{
	"zhihu.com": {
		"write articles at https://zhuanlan.zhihu.com/write; if it shows a sign-in form, 安全验证 or stays blank, call request_takeover at once (do not wait)",
		"title FIRST, before the body: the title is a plain <textarea placeholder=\"请输入标题（最多 100 个字）\"> (not the Draft.js body). Fill it with one page script: await type('textarea[placeholder^=\"请输入标题\"]', TITLE); return document.querySelector('textarea[placeholder^=\"请输入标题\"]').value — and check the returned value equals the title. Do not click it by pixels, OCR it, or paste into it with app_run",
		"decide the final title before you fill it: when the person named a date (e.g. 10.18), the title carries that date as they wrote it; if you used an earlier day's data, say so in the body's first paragraph, not by putting the other date in the title. A title once published is final: never go back to /edit to reword it, change its date or re-publish (that is an update, which only the person can ask for)",
		"topics: set them with one page script per topic, never with app_run keys or paste: await type('input[placeholder*=\"搜索话题\"]', TOPIC); (page scripts refuse execCommand/insertText — use the type() helper); then (after the suggestions appear, a second script) click the suggestion element whose text is exactly TOPIC and return the chosen topic list; two topics are enough — once two topic chips show, stop adding topics; if a topic does not stick after two tries, skip it: topics never hold up 发布. Right before 发布 re-read document.querySelector('textarea[placeholder^=\"请输入标题\"]').value; if it is not exactly the title (e.g. a topic name got appended), set it again with the title script",
		"body: the editor is Draft.js — fill it with ONE action=rich_fill whose text is the whole article (no x,y, no ref): it focuses the editor, pastes with a real Ctrl+V and returns 字数; do not type the body in chunks, with type(), or key by key, and never DOM-set or script the body",
		"after paste/import, probe the body and check its text length is close to the source length before going on",
		"the body is a rich-text editor: never write it with a page script or DOM (innerHTML/textContent/dispatchEvent paste are dropped and refused); only rich_fill (real paste); if 字数 stays 0, call rich_fill once more with replace=true, never append a second copy",
		"the editor footer shows 字数: N — 字数 0 after a paste means it did not land; do not go on to topics or publishing",
		"topics (添加话题): choose topics that match what the article is about (for AI/ML paper digests: 人工智能, 机器学习, 深度学习, 大语言模型, 计算机视觉, 论文); never pick a topic because it shares a literal token (e.g. Hf → 铪 is wrong)",
		"publishing is public: when the person asked in this task to publish it (发到知乎/发布到知乎/直接发布), click 发布 yourself once 字数 > 0 and the title is set — then, if a 发布设置/话题 dialog appears, finish it and confirm; otherwise (no such request) call request_takeover so the person checks and presses 发布",
		"after 发布: the page moves to zhuanlan.zhihu.com/p/<id> (not /edit); open that URL again and probe that the title and the full body text are there before you report the link as published",
		"verify the published article in the browser tab (navigate to /p/<id>, then a page script that returns document.title, the h1 text and the article body text length) — never with curl/wget/python from the shell: zhihu answers those with a ~650-byte anti-bot page, which says nothing about the article; make that browser read your last desktop action before the reply, and quote its title, body length and URL",
		"once /p/<id> shows the title and body, the article is published: do not open /edit, select-all or paste again, and do not press 更新",
		"finish right after that check: once the /p/<id> read-back shows the title and body, your next step is the final answer (one reply: what was published, the title, the /p/<id> link, the date used) — no screenshot, no closing the 发布成功 popup, no docx/export/deliver and no further tool calls unless the person asked for a file",
		"each request to publish (发到知乎/发布到知乎) means a NEW article made in this task at /write, even if a tab, the 创作中心 list or your notes already show a published article with the same or a similar title: that page is from an earlier task, not this one — never report it as this task's result; only open an existing article's /edit when the person asks to update/修改/更新 that article",
	},
}

func desktopSeedNotesFor(site string) []string {
	site = strings.ToLower(strings.TrimSpace(site))
	for site != "" {
		if n, ok := desktopSeedNotes[site]; ok {
			return n
		}
		i := strings.IndexByte(site, '.')
		if i < 0 {
			return nil
		}
		site = site[i+1:]
	}
	return nil
}

// desktopSiteOf is the host a step is about: a navigate's URL, else the
// attached page.
func desktopSiteOf(action string, args map[string]any, session *browser.BrowserAgentSession) string {
	if action == "navigate" {
		if h := browser.HostOfURL(desktopArg(args, "url")); h != "" {
			return h
		}
	}
	if session == nil || !desktopSessionConnected(session) {
		return ""
	}
	return session.PageHost()
}
