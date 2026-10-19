package memory

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// Desktop four-layer memory. The hot copy is this book. The same bytes are
// rendered into memory/profile.md, memory/notes.md, and log/YYYY-MM.md when a
// root is set. Writing the book does not start a machine.
//
// The prompt excerpt is rendered once. Later writes change the files and the
// hot copy, not that excerpt. A summary refresh is a later PR.

const (
	LayerProfile = "profile"
	LayerLog     = "log"
	LayerNote    = "note"
	LayerFile    = "file"

	ProfileMaxItems   = 100
	ProfileMaxRunes   = 4000
	LogPromptLimit    = 30
	NoteSnapshotRunes = 2000
	SearchResultLimit = 8
	CosineReplaceAt   = 0.85

	ProfileFullError   = "画像已有 100 条 / 4000 字。指出要合并的 id，或先 memory_forget。"
	ForgetMissingError = "没有这条。先 memory_search 核对原文再忘记。"
	HelperProfileError = "帮手不写画像。"
	HelperPrivateError = "不读别的帮手。"
	RejectWriteError   = "拒绝写入"
	LayerNameError     = "层要是 profile、log、note 或 file。"
	NeedContentError   = "要写内容。"
	NeedQueryError     = "要写查询。"
)

var (
	bearerSecret   = regexp.MustCompile(`(?i)Bearer\s+\S+`)
	userinfoSecret = regexp.MustCompile(`://[^/\s:]+:[^/\s@]+@`)

	desktopBooks sync.Map // owner -> *LayerBook
)

// DesktopMemoryWakesMachine is false. A memory write does not Ensure a machine.
func DesktopMemoryWakesMachine() bool { return false }

// DesktopBook returns the process-local hot copy for one person. The same
// owner always gets the same book, so every task of that person renders the
// same excerpt until a summary.
func DesktopBook(owner string) *LayerBook {
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = "desktop-user"
	}
	if v, ok := desktopBooks.Load(owner); ok {
		return v.(*LayerBook)
	}
	b := NewLayerBook(owner)
	actual, _ := desktopBooks.LoadOrStore(owner, b)
	return actual.(*LayerBook)
}

// ResetDesktopBooks drops every hot copy. Tests use it. A restart does the same.
func ResetDesktopBooks() {
	desktopBooks.Range(func(key, _ any) bool {
		desktopBooks.Delete(key)
		return true
	})
}

type memLine struct {
	ID   string
	Text string
	Hash string
	At   time.Time
}

type replacedLine struct {
	At   time.Time
	ID   string
	Turn string
	Text string
}

// WriteRequest is one memory_write or one end-of-turn candidate.
// Cosine, when set, is the similarity to the closest existing line and
// replaces the local bigram score. Nil means compute that score here.
// A fenced source (tool, email, web, webhook) cannot change the profile.
type WriteRequest struct {
	Layer   string
	Content string
	Path    string
	Source  string
	Turn    string
	Cosine  *float64
	At      time.Time
}

// WriteResult is the hot-copy outcome. Error is the sentence the model reads.
type WriteResult struct {
	ID       string
	Error    string
	Count    int
	Replaced bool
}

// TurnInput is the end-of-turn update. The same ID a second time changes nothing.
type TurnInput struct {
	ID        string
	UserLines []string
	Fences    []string
}

// LayerBook is one person's four layers, or one helper's notes and log.
type LayerBook struct {
	mu       sync.Mutex
	owner    string
	helperID string
	parent   *LayerBook
	root     string
	nowFn    func() time.Time
	seq      int

	profile  []memLine
	logs     []memLine
	notes    []memLine
	replaced []replacedLine
	files    map[string]string
	turns    map[string]struct{}
	helpers  map[string]*LayerBook

	seeded       bool
	excerpt      string
	excerptReady bool
}

// NewLayerBook builds a book that is not yet in the process map.
func NewLayerBook(owner string) *LayerBook {
	return &LayerBook{
		owner:   strings.TrimSpace(owner),
		files:   map[string]string{},
		turns:   map[string]struct{}{},
		helpers: map[string]*LayerBook{},
	}
}

// SetRoot is the home directory the files are written under. Empty keeps the
// hot copy only. It does not start a machine.
func (b *LayerBook) SetRoot(dir string) {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.root = dir
	for _, h := range b.helpers {
		h.mu.Lock()
		h.root = dir
		h.mu.Unlock()
	}
}

// SetNow pins the clock. Tests move it. Production leaves it nil.
func (b *LayerBook) SetNow(fn func() time.Time) {
	if b == nil {
		return
	}
	b.mu.Lock()
	b.nowFn = fn
	b.mu.Unlock()
}

// Helper returns that helper's notes and log. It can read this profile and
// cannot write it, and it cannot read another helper.
func (b *LayerBook) Helper(id string) *LayerBook {
	if b == nil {
		return nil
	}
	if b.parent != nil {
		return b.parent.Helper(id)
	}
	id = strings.TrimSpace(id)
	if id == "" {
		id = "helper"
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if h, ok := b.helpers[id]; ok {
		return h
	}
	h := NewLayerBook(b.owner)
	h.helperID = id
	h.parent = b
	h.root = b.root
	b.helpers[id] = h
	return h
}

// Excerpt is the third prefix segment. The first call renders it. Later
// calls return those same bytes.
func (b *LayerBook) Excerpt() string {
	if b == nil {
		return ""
	}
	if b.parent != nil {
		return b.helperExcerpt()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.excerptReady {
		b.excerpt = WrapDesktopMemory(b.bodyLocked(b.now()))
		b.excerptReady = true
	}
	return b.excerpt
}

func (b *LayerBook) helperExcerpt() string {
	b.mu.Lock()
	if b.excerptReady {
		out := b.excerpt
		b.mu.Unlock()
		return out
	}
	b.mu.Unlock()
	profile := b.parent.copyProfile()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.excerptReady {
		return b.excerpt
	}
	b.profile = profile
	b.excerpt = WrapDesktopMemory(b.bodyLocked(b.now()))
	b.excerptReady = true
	return b.excerpt
}

// Render is the current hot copy. It does not move a frozen excerpt.
func (b *LayerBook) Render() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return WrapDesktopMemory(b.bodyLocked(b.now()))
}

// RefreshExcerpt replaces the frozen excerpt with the current hot copy.
// Only a committed idle summary calls it. A write does not.
func (b *LayerBook) RefreshExcerpt() string {
	if b == nil {
		return ""
	}
	if b.parent != nil {
		return b.helperRefreshExcerpt()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.excerpt = WrapDesktopMemory(b.bodyLocked(b.now()))
	b.excerptReady = true
	return b.excerpt
}

func (b *LayerBook) helperRefreshExcerpt() string {
	profile := b.parent.copyProfile()
	b.mu.Lock()
	defer b.mu.Unlock()
	b.profile = profile
	b.excerpt = WrapDesktopMemory(b.bodyLocked(b.now()))
	b.excerptReady = true
	return b.excerpt
}

// ProfileTexts are the current profile lines, in order.
func (b *LayerBook) ProfileTexts() []string {
	if b == nil {
		return nil
	}
	if b.helperID != "" && b.parent != nil {
		return b.parent.ProfileTexts()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.profile))
	for i, line := range b.profile {
		out[i] = line.Text
	}
	return out
}

// ReplacedTexts are the lines moved under ## 已替换.
func (b *LayerBook) ReplacedTexts() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.replaced))
	for i, line := range b.replaced {
		out[i] = line.Text
	}
	return out
}

// NoteTexts are the notes still in the hot copy.
func (b *LayerBook) NoteTexts() []string {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]string, len(b.notes))
	for i, line := range b.notes {
		out[i] = line.Text
	}
	return out
}

// FileBody is the hot-copy bytes of one relative path, such as memory/profile.md.
func (b *LayerBook) FileBody(rel string) string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.files[filepath.ToSlash(rel)]
}

// Write applies one memory_write. The same normalized text updates the
// existing line. Cosine at or above 0.85 replaces that line and does not
// insert a second one.
func (b *LayerBook) Write(req WriteRequest) WriteResult {
	if b == nil {
		return WriteResult{Error: NeedContentError}
	}
	if b.helperID != "" && strings.TrimSpace(req.Layer) == LayerProfile {
		return WriteResult{Error: HelperProfileError}
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.writeLocked(req)
}

// Forget removes one current line by id or by its text.
func (b *LayerBook) Forget(layer, id, text string) string {
	if b == nil {
		return ForgetMissingError
	}
	layer = strings.TrimSpace(layer)
	if b.helperID != "" && layer == LayerProfile {
		return HelperProfileError
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked()
	id = strings.TrimSpace(id)
	text = normalizeMemoryText(stripMemorySecrets(text))
	switch layer {
	case LayerProfile:
		if !b.dropLocked(&b.profile, id, text) {
			return ForgetMissingError
		}
	case LayerLog:
		if !b.dropLocked(&b.logs, id, text) {
			return ForgetMissingError
		}
	case LayerNote:
		if !b.dropLocked(&b.notes, id, text) {
			return ForgetMissingError
		}
	default:
		return LayerNameError
	}
	b.writeFilesLocked()
	return ""
}

// Search reads the hot copy. It returns at most eight fenced lines and does
// not change the excerpt. A hit resets that note's clock.
func (b *LayerBook) Search(query, layer string) string {
	if b == nil {
		return NeedQueryError
	}
	query = strings.TrimSpace(query)
	if query == "" {
		return NeedQueryError
	}
	if b.helperID != "" && helperPathOfOther(query, b.helperID) {
		return HelperPrivateError
	}
	var shared []memLine
	if b.parent != nil {
		shared = b.parent.copyProfile()
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked()
	now := b.now()
	hits := b.searchLocked(query, strings.TrimSpace(layer), now, shared)
	b.writeFilesLocked()
	if len(hits) == 0 {
		return WrapDesktopMemory("（无）")
	}
	return WrapDesktopMemory(strings.Join(hits, "\n"))
}

// ReadHelper reports another helper's notes. A helper cannot read them.
func (b *LayerBook) ReadHelper(otherID string) string {
	if b == nil {
		return HelperPrivateError
	}
	otherID = strings.TrimSpace(otherID)
	if b.helperID != "" && otherID != "" && otherID != b.helperID {
		return HelperPrivateError
	}
	return ""
}

// EndTurn updates files after the reply is out. It does not move the excerpt
// and it does not send anything. The same turn id is a no-op.
func (b *LayerBook) EndTurn(in TurnInput) string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if id := strings.TrimSpace(in.ID); id != "" {
		if _, ok := b.turns[id]; ok {
			return ""
		}
		b.turns[id] = struct{}{}
	}
	if b.helperID != "" {
		for _, line := range in.UserLines {
			b.writeLocked(WriteRequest{Layer: LayerNote, Content: line, Source: "user", Turn: in.ID})
		}
	} else {
		for _, line := range in.UserLines {
			b.writeLocked(WriteRequest{Layer: LayerProfile, Content: line, Source: "user", Turn: in.ID})
		}
	}
	if len(in.Fences) > 0 {
		b.addLogLocked(RejectWriteError, b.now())
	}
	b.writeFilesLocked()
	return ""
}

// Sweep drops notes that have been quiet for 24 hours.
func (b *LayerBook) Sweep() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	b.sweepLocked()
	b.writeFilesLocked()
}

// Call is memory_write, memory_forget, or memory_search.
func (b *LayerBook) Call(name string, args map[string]interface{}) string {
	if b == nil {
		return NeedContentError
	}
	switch strings.TrimSpace(name) {
	case "memory_write":
		res := b.Write(writeFromArgs(args))
		if res.Error != "" {
			return res.Error
		}
		return "已写入 " + res.ID
	case "memory_forget":
		errText := b.Forget(stringArg(args, "layer"), stringArg(args, "id"), stringArg(args, "content"))
		if errText != "" {
			return errText
		}
		return "已忘记"
	case "memory_search":
		return b.Search(stringArg(args, "query"), stringArg(args, "layer"))
	default:
		return "没有这个动作。"
	}
}

// SeedFromStore copies existing entries into the layers once, before the
// excerpt is frozen. A later save in the old store does not move the pin.
func (b *LayerBook) SeedFromStore(s *Store, owner string) {
	if b == nil || s == nil || b.parent != nil {
		return
	}
	b.mu.Lock()
	if b.seeded || b.excerptReady {
		b.mu.Unlock()
		return
	}
	b.mu.Unlock()

	s.RLock()
	src := append([]Entry(nil), s.entries...)
	s.RUnlock()

	b.mu.Lock()
	defer b.mu.Unlock()
	if b.seeded || b.excerptReady {
		return
	}
	b.seeded = true
	owner = strings.TrimSpace(owner)
	for _, e := range src {
		if e.Status == StatusSuperseded || e.Status == StatusDormant {
			continue
		}
		if owner != "" && e.OwnerID != "" && e.OwnerID != owner {
			continue
		}
		text := normalizeMemoryText(stripMemorySecrets(e.Content))
		if text == "" {
			continue
		}
		at := e.UpdatedAt
		if at.IsZero() {
			at = e.CreatedAt
		}
		switch LayerOf(e.Category) {
		case LayerProfile:
			b.profile = append(b.profile, memLine{ID: b.keepID(e.ID, "p"), Text: text, Hash: memoryTextHash(text), At: at})
		case LayerLog:
			if utf8.RuneCountInString(text) > 200 {
				b.files["memory/artifact-"+b.keepID(e.ID, "f")+".txt"] = text
				continue
			}
			b.logs = append(b.logs, memLine{ID: b.keepID(e.ID, "l"), Text: text, Hash: memoryTextHash(text), At: at})
		default:
			b.files["memory/file-"+b.keepID(e.ID, "f")+".txt"] = text
		}
	}
	b.writeFilesLocked()
}

func (b *LayerBook) keepID(id, prefix string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return b.allocIDLocked(prefix)
	}
	return id
}

func (b *LayerBook) writeLocked(req WriteRequest) WriteResult {
	req.Layer = strings.TrimSpace(req.Layer)
	raw := stripMemorySecrets(req.Content)
	text := normalizeMemoryText(raw)
	now := req.At
	if now.IsZero() {
		now = b.now()
	}
	if fencedSource(req.Source) {
		b.addLogLocked(RejectWriteError, now)
		b.writeFilesLocked()
		return WriteResult{Error: RejectWriteError}
	}
	if req.Layer == LayerFile {
		return b.writeFileLocked(req.Path, raw)
	}
	if text == "" {
		return WriteResult{Error: NeedContentError}
	}
	if req.Layer == LayerProfile || req.Layer == LayerNote || req.Layer == LayerLog {
		if err := ScanForInjection(text); err != nil {
			b.addLogLocked(RejectWriteError, now)
			b.writeFilesLocked()
			return WriteResult{Error: RejectWriteError}
		}
	}
	switch req.Layer {
	case LayerProfile:
		return b.writeLinesLocked(&b.profile, "p", text, now, req, true)
	case LayerLog:
		return b.writeLinesLocked(&b.logs, "l", text, now, req, false)
	case LayerNote:
		res := b.writeLinesLocked(&b.notes, "n", text, now, req, false)
		b.sweepLocked()
		b.writeFilesLocked()
		return res
	default:
		return WriteResult{Error: LayerNameError}
	}
}

func (b *LayerBook) writeLinesLocked(dst *[]memLine, prefix, text string, now time.Time, req WriteRequest, profile bool) WriteResult {
	lines := *dst
	idx, score, same := bestLine(lines, text, req.Cosine)
	if same && idx >= 0 {
		lines[idx].At = now
		*dst = lines
		b.writeFilesLocked()
		return WriteResult{ID: lines[idx].ID, Count: len(lines)}
	}
	if idx >= 0 && score >= CosineReplaceAt {
		old := lines[idx]
		if profile {
			next := profileRunes(lines) - utf8.RuneCountInString(old.Text) + utf8.RuneCountInString(text)
			if next > ProfileMaxRunes {
				return WriteResult{Error: ProfileFullError, Count: len(lines)}
			}
		}
		lines[idx].Text = text
		lines[idx].Hash = memoryTextHash(text)
		lines[idx].At = now
		*dst = lines
		if profile {
			b.replaced = append(b.replaced, replacedLine{At: now, ID: old.ID, Turn: strings.TrimSpace(req.Turn), Text: old.Text})
			b.addLogLocked("替换了画像 "+old.ID, now)
		}
		b.writeFilesLocked()
		return WriteResult{ID: old.ID, Count: len(lines), Replaced: true}
	}
	if profile && (len(lines) >= ProfileMaxItems || profileRunes(lines)+utf8.RuneCountInString(text) > ProfileMaxRunes) {
		return WriteResult{Error: ProfileFullError, Count: len(lines)}
	}
	id := b.allocIDLocked(prefix)
	lines = append(lines, memLine{ID: id, Text: text, Hash: memoryTextHash(text), At: now})
	*dst = lines
	b.writeFilesLocked()
	return WriteResult{ID: id, Count: len(lines)}
}

func (b *LayerBook) writeFileLocked(path, body string) WriteResult {
	rel, errText := cleanMemoryPath(path, b.helperID)
	if errText != "" {
		return WriteResult{Error: errText}
	}
	if b.files == nil {
		b.files = map[string]string{}
	}
	b.files[rel] = body
	b.writeFilesLocked()
	return WriteResult{ID: rel, Count: 1, Replaced: true}
}

func (b *LayerBook) addLogLocked(text string, at time.Time) {
	text = normalizeMemoryText(text)
	if text == "" {
		return
	}
	id := b.allocIDLocked("l")
	b.logs = append(b.logs, memLine{ID: id, Text: text, Hash: memoryTextHash(text), At: at})
}

func (b *LayerBook) dropLocked(dst *[]memLine, id, text string) bool {
	lines := *dst
	hash := ""
	if text != "" {
		hash = memoryTextHash(text)
	}
	for i, line := range lines {
		if (id != "" && line.ID == id) || (hash != "" && line.Hash == hash) {
			*dst = append(lines[:i], lines[i+1:]...)
			return true
		}
	}
	return false
}

func (b *LayerBook) sweepLocked() {
	now := b.now()
	kept := make([]memLine, 0, len(b.notes))
	for _, line := range b.notes {
		if NoteForgotten(line.At, now) {
			continue
		}
		kept = append(kept, line)
	}
	b.notes = kept
}

func (b *LayerBook) searchLocked(query, layer string, now time.Time, shared []memLine) []string {
	type hit struct {
		path string
		line int
		text string
	}
	var hits []hit
	add := func(path string, n int, text string) {
		if layer != "" && layerOfPath(path) != layer {
			return
		}
		if !strings.Contains(text, query) {
			return
		}
		hits = append(hits, hit{path: path, line: n, text: text})
	}
	profile := b.profile
	if b.parent != nil {
		profile = shared
	}
	base := b.homeBase()
	for i, line := range profile {
		add(base+"memory/profile.md", i+1, line.ID+" "+line.Text)
	}
	for i, line := range b.replaced {
		add(base+"memory/profile.md", len(profile)+2+i, line.ID+" "+line.Text)
	}
	for i, line := range b.logs {
		add(base+"log/"+line.At.Format("2006-01")+".md", i+1, line.Text)
	}
	for i, line := range b.notes {
		if strings.Contains(line.Text, query) {
			line.At = now
			b.notes[i] = line
		}
		add(b.notePath(), i+1, line.Text)
	}
	if len(hits) > SearchResultLimit {
		hits = hits[:SearchResultLimit]
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = fmt.Sprintf("%s:%d:%s", h.path, h.line, h.text)
	}
	return out
}

func (b *LayerBook) bodyLocked(now time.Time) string {
	var buf strings.Builder
	buf.WriteString("画像\n")
	for _, line := range b.profile {
		fmt.Fprintf(&buf, "- %s %s\n", line.ID, line.Text)
	}
	buf.WriteString("\n日志\n")
	for _, line := range rankLogs(b.logs, b.profile, now) {
		fmt.Fprintf(&buf, "- %s %s %s\n", line.At.UTC().Format("2006-01-02"), line.ID, line.Text)
	}
	buf.WriteString("\n")
	notes := fitNotes(b.liveNotes(now))
	if len(notes) == 0 {
		buf.WriteString("笔记: 无\n")
	} else {
		buf.WriteString("笔记\n")
		for _, line := range notes {
			fmt.Fprintf(&buf, "- %s %s\n", line.ID, line.Text)
		}
	}
	return buf.String()
}

func (b *LayerBook) liveNotes(now time.Time) []memLine {
	out := make([]memLine, 0, len(b.notes))
	for _, line := range b.notes {
		if NoteForgotten(line.At, now) {
			continue
		}
		out = append(out, line)
	}
	return out
}

func (b *LayerBook) writeFilesLocked() {
	if b.helperID == "" {
		b.files["memory/profile.md"] = b.profileMarkdown()
	}
	b.files[b.noteRel()] = b.notesMarkdown()
	for month, body := range b.logsByMonth() {
		b.files[b.logRel(month)] = body
	}
	if strings.TrimSpace(b.root) == "" {
		return
	}
	for rel, body := range b.files {
		path := filepath.Join(b.root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			continue
		}
		_ = os.WriteFile(path, []byte(body), 0o644)
	}
}

func (b *LayerBook) profileMarkdown() string {
	var buf strings.Builder
	for _, line := range b.profile {
		fmt.Fprintf(&buf, "- %s %s\n", line.ID, line.Text)
	}
	if len(b.replaced) == 0 {
		return buf.String()
	}
	if buf.Len() > 0 {
		buf.WriteByte('\n')
	}
	buf.WriteString("## 已替换\n")
	for _, line := range b.replaced {
		turn := ""
		if strings.TrimSpace(line.Turn) != "" {
			turn = " turn=" + strings.TrimSpace(line.Turn)
		}
		fmt.Fprintf(&buf, "- %s %s%s %s\n", line.At.UTC().Format("2006-01-02"), line.ID, turn, line.Text)
	}
	return buf.String()
}

func (b *LayerBook) notesMarkdown() string {
	var buf strings.Builder
	for _, line := range b.notes {
		fmt.Fprintf(&buf, "- %s %s\n", line.ID, line.Text)
	}
	return buf.String()
}

func (b *LayerBook) logsByMonth() map[string]string {
	grouped := map[string][]memLine{}
	for _, line := range b.logs {
		month := line.At.UTC().Format("2006-01")
		grouped[month] = append(grouped[month], line)
	}
	out := make(map[string]string, len(grouped))
	for month, lines := range grouped {
		var buf strings.Builder
		for _, line := range lines {
			fmt.Fprintf(&buf, "- %s %s %s\n", line.At.UTC().Format("2006-01-02"), line.ID, line.Text)
		}
		out[month] = buf.String()
	}
	return out
}

func (b *LayerBook) noteRel() string {
	if b.helperID != "" {
		return "helpers/" + b.helperID + "/notes.md"
	}
	return "memory/notes.md"
}

func (b *LayerBook) logRel(month string) string {
	if b.helperID != "" {
		return "helpers/" + b.helperID + "/log/" + month + ".md"
	}
	return "log/" + month + ".md"
}

func (b *LayerBook) notePath() string {
	return b.homeBase() + b.noteRel()
}

func (b *LayerBook) homeBase() string {
	if b.helperID != "" {
		return "/home/desktop/"
	}
	return "/home/desktop/"
}

func (b *LayerBook) allocIDLocked(prefix string) string {
	b.seq++
	return fmt.Sprintf("%s-%02d", prefix, b.seq)
}

func (b *LayerBook) now() time.Time {
	if b.nowFn != nil {
		return b.nowFn()
	}
	return time.Now().UTC()
}

func (b *LayerBook) copyProfile() []memLine {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]memLine, len(b.profile))
	copy(out, b.profile)
	return out
}

func bestLine(lines []memLine, text string, cosine *float64) (idx int, score float64, same bool) {
	hash := memoryTextHash(text)
	best := -1
	bestBigram := -1.0
	for i, line := range lines {
		if line.Hash == hash {
			return i, 1, true
		}
		s := bigramCosine(line.Text, text)
		if s > bestBigram {
			bestBigram = s
			best = i
		}
	}
	score = bestBigram
	if cosine != nil {
		score = *cosine
	}
	return best, score, false
}

func rankLogs(lines, profile []memLine, now time.Time) []memLine {
	if len(lines) == 0 {
		return nil
	}
	profileText := ""
	for _, line := range profile {
		profileText += line.Text
	}
	type scored struct {
		line  memLine
		score float64
	}
	items := make([]scored, len(lines))
	for i, line := range lines {
		at := line.At
		if at.IsZero() {
			at = now
		}
		ageDays := now.Sub(at).Hours() / 24
		if ageDays < 0 {
			ageDays = 0
		}
		score := math.Exp(-ageDays / 14)
		if entityOverlap(profileText, line.Text) {
			score += 1
		}
		if now.Sub(at) <= 48*time.Hour {
			score += 2
		}
		items[i] = scored{line: line, score: score}
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].score != items[j].score {
			return items[i].score > items[j].score
		}
		if !items[i].line.At.Equal(items[j].line.At) {
			return items[i].line.At.After(items[j].line.At)
		}
		return items[i].line.ID < items[j].line.ID
	})
	if len(items) > LogPromptLimit {
		items = items[:LogPromptLimit]
	}
	out := make([]memLine, len(items))
	for i := range items {
		out[i] = items[i].line
	}
	return out
}

func fitNotes(lines []memLine) []memLine {
	total := 0
	for _, line := range lines {
		total += utf8.RuneCountInString(line.Text)
	}
	i := 0
	for total > NoteSnapshotRunes && i < len(lines) {
		total -= utf8.RuneCountInString(lines[i].Text)
		i++
	}
	return lines[i:]
}

func entityOverlap(profile, log string) bool {
	rs := []rune(profile)
	if len(rs) < 2 || log == "" {
		return false
	}
	for i := 0; i+1 < len(rs); i++ {
		if strings.Contains(log, string(rs[i:i+2])) {
			return true
		}
	}
	return false
}

func profileRunes(lines []memLine) int {
	n := 0
	for _, line := range lines {
		n += utf8.RuneCountInString(line.Text)
	}
	return n
}

func memoryTextHash(text string) string {
	sum := sha256.Sum256([]byte(normalizeMemoryText(text)))
	return hex.EncodeToString(sum[:])
}

func normalizeMemoryText(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

func stripMemorySecrets(s string) string {
	s = bearerSecret.ReplaceAllString(s, "Bearer [redacted]")
	s = userinfoSecret.ReplaceAllString(s, "://[redacted]@")
	return s
}

func fencedSource(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "tool", "email", "web", "webhook", "fence":
		return true
	default:
		return false
	}
}

func bigramCosine(a, b string) float64 {
	ga := bigramCounts(normalizeMemoryText(a))
	gb := bigramCounts(normalizeMemoryText(b))
	if len(ga) == 0 || len(gb) == 0 {
		return 0
	}
	var dot, na, nb float64
	for g, c := range ga {
		na += float64(c * c)
		if d, ok := gb[g]; ok {
			dot += float64(c * d)
		}
	}
	for _, c := range gb {
		nb += float64(c * c)
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func bigramCounts(s string) map[string]int {
	rs := []rune(s)
	out := map[string]int{}
	if len(rs) < 2 {
		if len(rs) == 1 {
			out[string(rs)] = 1
		}
		return out
	}
	for i := 0; i+1 < len(rs); i++ {
		out[string(rs[i:i+2])]++
	}
	return out
}

func cleanMemoryPath(path, helperID string) (string, string) {
	path = filepath.ToSlash(strings.TrimSpace(path))
	path = strings.TrimPrefix(path, "/home/desktop/")
	path = strings.TrimPrefix(path, "/")
	if path == "" || strings.Contains(path, "..") || strings.Contains(path, ":") {
		return "", pathDenied(helperID)
	}
	switch {
	case helperID != "":
		prefix := "helpers/" + helperID + "/"
		if !strings.HasPrefix(path, prefix) {
			return "", pathDenied(helperID)
		}
	case strings.HasPrefix(path, "memory/") || strings.HasPrefix(path, "log/") || strings.HasPrefix(path, "skills/") || strings.HasPrefix(path, "routines/"):
	default:
		return "", pathDenied(helperID)
	}
	return path, ""
}

func pathDenied(helperID string) string {
	id := strings.TrimSpace(helperID)
	if id == "" {
		id = "<id>"
	}
	return "路径不在记忆目录里。换到 memory/、log/、skills/、routines/ 或这个帮手自己的 helpers/" + id + "/。"
}

func helperPathOfOther(query, self string) bool {
	q := filepath.ToSlash(query)
	if !strings.Contains(q, "helpers/") {
		return false
	}
	selfMark := "helpers/" + self + "/"
	return !strings.Contains(q, selfMark)
}

func layerOfPath(path string) string {
	switch {
	case strings.Contains(path, "profile.md"):
		return LayerProfile
	case strings.Contains(path, "/log/"):
		return LayerLog
	case strings.Contains(path, "notes.md"):
		return LayerNote
	default:
		return LayerFile
	}
}

func stringArg(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	default:
		return fmt.Sprint(t)
	}
}

func writeFromArgs(args map[string]interface{}) WriteRequest {
	req := WriteRequest{
		Layer:   stringArg(args, "layer"),
		Content: stringArg(args, "content"),
		Path:    stringArg(args, "path"),
		Source:  stringArg(args, "source"),
		Turn:    stringArg(args, "turn"),
	}
	if args != nil {
		if raw, ok := args["cosine"]; ok {
			switch n := raw.(type) {
			case float64:
				v := n
				req.Cosine = &v
			case int:
				v := float64(n)
				req.Cosine = &v
			}
		}
	}
	return req
}
