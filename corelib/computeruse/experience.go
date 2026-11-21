package computeruse

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Computer Use lessons. Notes are fixed templates keyed by app id (lanxin,
// wechat, …) plus a desktop bucket that only holds the stitch refusal.
// Typed text, contact names, passwords, and raw window titles are never
// stored. A later turn reads the apps named in that task, not the previous
// task's window.

const (
	experienceDesktop    = "desktop"
	experienceMaxPerKind = 6
	experienceMaxApps    = 24
	experienceMaxBytes   = 32 << 10
	// ExperienceSuccessObserveMax is the most computer_observe calls a round
	// may make and still record a short search path as something that worked.
	ExperienceSuccessObserveMax = 3
)

const (
	// NoteStitchRefused is recorded when a virtual-desktop stitch is too
	// large to click reliably. It is the only note stored on the desktop bucket.
	NoteStitchRefused = "do not pass screen_index=-1 on a large desktop; that stitch is refused and the foreground window is captured. Pass window=<app title>."
	// NoteSearchFind is recorded only after a short, proven search path.
	NoteSearchFind = "the search box plus computer_find reached a row. Do not scroll a long list to hunt for a name."
	// NoteFoundText is recorded when computer_find matched on-screen text.
	NoteFoundText = "computer_find matched on-screen text. Click that ref."
	// NoteFindMissed is recorded when computer_find returned no match.
	NoteFindMissed = "computer_find returned no match. Try the app search box, or pass window=<app title>."
	// NoteWindowChanged is recorded when the foreground window moved under an action.
	NoteWindowChanged = "the foreground window changed under the action. computer_focus the app before the next click."
	// NoteRefreshFailed is recorded when the post-action crop could not be taken.
	NoteRefreshFailed = "the foreground refresh failed. computer_observe before the next click."
)

const noteIMSearch = "computer_focus the window, use its search box, computer_type the name, then computer_find the row. Do not scroll the contact list. A successful action already includes the next foreground crop."

// experienceRecordable is what ExperienceRecord may write. The IM seed is a
// generic hint and is not a learned note.
var experienceRecordable = map[string]struct{}{
	NoteStitchRefused: {},
	NoteSearchFind:    {},
	NoteFoundText:     {},
	NoteFindMissed:    {},
	NoteWindowChanged: {},
	NoteRefreshFailed: {},
}

// experienceSeeds are generic hints, not this machine's history. They show
// until the app has any learned avoid note.
var experienceSeeds = map[string][]experienceNote{
	experienceDesktop: {{Note: NoteStitchRefused}},
}

func init() {
	for _, g := range aliasGroups {
		if g.kind != AdapterIM || g.id == "" {
			continue
		}
		experienceSeeds[g.id] = []experienceNote{{Note: noteIMSearch}}
	}
}

type experienceNote struct {
	Note  string `json:"note"`
	Count int    `json:"count"`
	Last  int64  `json:"last"`
}

type experienceApp struct {
	Worked []experienceNote `json:"worked,omitempty"`
	Avoid  []experienceNote `json:"avoid,omitempty"`
	Seen   int64            `json:"seen"`
}

type experienceFile struct {
	// LastApp is ignored. Older files may still contain it.
	LastApp string                    `json:"last_app,omitempty"`
	Apps    map[string]*experienceApp `json:"apps"`
}

var (
	experienceMu  sync.Mutex
	experienceDir string
)

// SetExperienceDir chooses where lessons are stored. An empty dir disables
// persistence. Tests set a temp directory; the GUI sets its data directory.
func SetExperienceDir(dir string) {
	experienceMu.Lock()
	experienceDir = strings.TrimSpace(dir)
	experienceMu.Unlock()
}

func experienceEnabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MACLAW_COMPUTER_USE_EXPERIENCE"))) {
	case "0", "off", "false", "no":
		return false
	}
	experienceMu.Lock()
	dir := experienceDir
	experienceMu.Unlock()
	return dir != ""
}

func experiencePath() string {
	experienceMu.Lock()
	dir := experienceDir
	experienceMu.Unlock()
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "lessons.json")
}

// ExperienceAppID is the alias id of a window title, such as lanxin for
// "蓝信 - 张三". An unknown title is empty.
func ExperienceAppID(title string) string {
	n := NormalizeWindowTitle(title)
	if n == "" {
		return ""
	}
	if id, ok := groupByName[n]; ok {
		return id
	}
	pre, suf, split := splitWindowTitle(n)
	if id, ok := groupByName[suf]; ok {
		return id
	}
	if split {
		if id, ok := groupByName[pre]; ok {
			return id
		}
	}
	ids := ExperienceAppsIn(title)
	if len(ids) == 1 {
		return ids[0]
	}
	return ""
}

// ExperienceAppsIn lists app ids named in text, longest alias first so
// 微信开发者工具 does not also count as 微信.
func ExperienceAppsIn(text string) []string {
	s := NormalizeWindowTitle(text)
	if s == "" {
		return nil
	}
	rs := []rune(s)
	var ids []string
	seen := map[string]bool{}
	for i := 0; i < len(rs); {
		alias, ok := longestMentionAlias(string(rs[i:]))
		if !ok {
			i++
			continue
		}
		if id, ok := groupByName[alias.name]; ok && id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
		n := utf8.RuneCountInString(alias.name)
		if n < 1 {
			n = 1
		}
		i += n
	}
	return ids
}

// ExperienceSearchProven reports the one success note a short search path
// may store. Vision has no computer_find, so focus then type is the evidence.
// Text mode needs focus, then type, then a find hit. A round that already
// called computer_observe more than ExperienceSuccessObserveMax times does
// not record that path.
func ExperienceSearchProven(focused, typed, found, vision bool, observes int) (string, bool) {
	if observes > ExperienceSuccessObserveMax {
		return "", false
	}
	if vision && focused && typed {
		return NoteSearchFind, true
	}
	if !vision && focused && typed && found {
		return NoteSearchFind, true
	}
	return "", false
}

// ExperienceRecord stores one allowed lesson. The desktop bucket accepts
// only the stitch refusal. An empty or unknown app stores nothing, so a
// missed window title cannot attach an app sentence to every later task.
// The returned line is the sentence just stored, or empty when nothing was
// written. LastApp is not updated.
func ExperienceRecord(app, note string, worked bool) string {
	note = strings.TrimSpace(note)
	if note == "" || !experienceEnabled() {
		return ""
	}
	if _, ok := experienceRecordable[note]; !ok {
		return ""
	}
	app = strings.TrimSpace(app)
	if note == NoteStitchRefused {
		app = experienceDesktop
	} else if !experienceKnownApp(app) {
		return ""
	}
	experienceMu.Lock()
	defer experienceMu.Unlock()
	path := ""
	if experienceDir != "" {
		path = filepath.Join(experienceDir, "lessons.json")
	}
	if path == "" {
		return ""
	}
	f := experienceLoad(path)
	f.LastApp = ""
	e := f.Apps[app]
	if e == nil {
		e = &experienceApp{}
		f.Apps[app] = e
	}
	now := time.Now().Unix()
	e.Seen = now
	if worked {
		e.Worked = experienceAddNote(e.Worked, note, now)
	} else {
		e.Avoid = experienceAddNote(e.Avoid, note, now)
	}
	count := experienceNoteCount(e.Worked, note)
	if !worked {
		count = experienceNoteCount(e.Avoid, note)
	}
	experienceSave(path, f)
	line := experienceOneLine(worked, note, count)
	log.Printf("[computer-use] experience app=%s worked=%t note=%q", app, worked, note)
	return line
}

// ExperiencePrompt is the playbook addendum for this request. Desktop hints
// are always included. App sections come from names in the request plus
// turnApps, which are app ids for the window this turn is already driving.
// A previous task's app is not added. Empty when persistence is off.
func ExperiencePrompt(request string, turnApps []string) string {
	if !experienceEnabled() {
		return ""
	}
	path := experiencePath()
	experienceMu.Lock()
	f := experienceLoad(path)
	experienceMu.Unlock()
	ids := []string{experienceDesktop}
	seen := map[string]bool{experienceDesktop: true}
	add := func(id string) {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] || !experienceKnownApp(id) {
			return
		}
		seen[id] = true
		ids = append(ids, id)
	}
	for _, id := range ExperienceAppsIn(request) {
		add(id)
	}
	for _, id := range turnApps {
		add(id)
	}
	var hints, learned strings.Builder
	for _, id := range ids {
		hint, learnedLine := experienceSplit(id, f.Apps[id])
		if hint != "" {
			hints.WriteString(hint)
			hints.WriteByte('\n')
		}
		if learnedLine != "" {
			learned.WriteString(learnedLine)
			learned.WriteByte('\n')
		}
	}
	var b strings.Builder
	if hints.Len() > 0 {
		b.WriteString("Computer Use hints (generic, not this machine's history):\n")
		b.WriteString(hints.String())
	}
	if learned.Len() > 0 {
		b.WriteString("Computer Use experience (learned on this machine):\n")
		b.WriteString(learned.String())
	}
	return b.String()
}

func experienceKnownApp(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" || id == experienceDesktop {
		return false
	}
	for _, g := range aliasGroups {
		if g.id == id {
			return true
		}
	}
	return false
}

func experienceLoad(path string) *experienceFile {
	f := &experienceFile{Apps: map[string]*experienceApp{}}
	if path == "" {
		return f
	}
	raw, err := os.ReadFile(path)
	if err == nil && len(raw) <= experienceMaxBytes*2 {
		_ = json.Unmarshal(raw, f)
	}
	if f.Apps == nil {
		f.Apps = map[string]*experienceApp{}
	}
	return f
}

func experienceSave(path string, f *experienceFile) {
	if f != nil {
		f.LastApp = ""
	}
	for {
		raw, err := json.Marshal(f)
		if err != nil {
			return
		}
		if len(raw) <= experienceMaxBytes && len(f.Apps) <= experienceMaxApps {
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
		for app, e := range f.Apps {
			seen := int64(0)
			if e != nil {
				seen = e.Seen
			}
			if seen < at {
				oldest, at = app, seen
			}
		}
		if oldest == "" {
			return
		}
		delete(f.Apps, oldest)
	}
}

func experienceAddNote(list []experienceNote, note string, now int64) []experienceNote {
	for i := range list {
		if list[i].Note == note {
			list[i].Count++
			list[i].Last = now
			return list
		}
	}
	list = append(list, experienceNote{Note: note, Count: 1, Last: now})
	sort.SliceStable(list, func(i, j int) bool { return list[i].Last > list[j].Last })
	if len(list) > experienceMaxPerKind {
		list = list[:experienceMaxPerKind]
	}
	return list
}

func experienceNoteCount(list []experienceNote, note string) int {
	for _, n := range list {
		if n.Note == note {
			return n.Count
		}
	}
	return 0
}

func experienceOneLine(worked bool, note string, count int) string {
	kind := "avoid"
	if worked {
		kind = "worked"
	}
	if count > 1 {
		return fmt.Sprintf("%s: %s (x%d)", kind, note, count)
	}
	return kind + ": " + note
}

func experienceSplit(id string, learned *experienceApp) (hintLine, learnedLine string) {
	var worked, avoid []experienceNote
	if learned != nil {
		for _, n := range learned.Worked {
			if _, ok := experienceRecordable[n.Note]; ok && strings.TrimSpace(n.Note) != "" {
				worked = append(worked, n)
			}
		}
		for _, n := range learned.Avoid {
			if _, ok := experienceRecordable[n.Note]; ok && strings.TrimSpace(n.Note) != "" {
				avoid = append(avoid, n)
			}
		}
	}
	if len(avoid) == 0 {
		var b strings.Builder
		for _, n := range experienceSeeds[id] {
			note := strings.TrimSpace(n.Note)
			if note == "" || experienceListed(worked, note) {
				continue
			}
			if b.Len() == 0 {
				b.WriteString("- ")
				b.WriteString(id)
				b.WriteString(":")
			}
			b.WriteString(" hint: ")
			b.WriteString(note)
			b.WriteByte(';')
		}
		hintLine = b.String()
	}
	if len(worked) == 0 && len(avoid) == 0 {
		return hintLine, ""
	}
	var b strings.Builder
	b.WriteString("- ")
	b.WriteString(id)
	b.WriteString(":")
	for _, n := range worked {
		b.WriteByte(' ')
		b.WriteString(experienceOneLine(true, n.Note, n.Count))
		b.WriteByte(';')
	}
	for _, n := range avoid {
		b.WriteByte(' ')
		b.WriteString(experienceOneLine(false, n.Note, n.Count))
		b.WriteByte(';')
	}
	return hintLine, b.String()
}

func experienceListed(list []experienceNote, note string) bool {
	for _, n := range list {
		if n.Note == note {
			return true
		}
	}
	return false
}
