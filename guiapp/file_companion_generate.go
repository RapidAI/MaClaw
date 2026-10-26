package guiapp

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/docgen"
)

// The companion can write exactly one kind of file: a PDF of its own answer.
// It goes into the session's own results directory, next to the searchable PDF
// that pdf_restore writes, and never touches the document the reader opened.
//
// Everything about the destination is decided here. The tool takes no path
// argument: the document is untrusted input, and a document that says "write
// the notes to C:\Users\...\Desktop" must not be able to steer the writer.
// The user still chooses where the file ends up — the window's 产出物 card
// offers 保存, which asks.

const (
	// fileCompanionGenerateMaxRunes caps the Markdown one call may hand to
	// the writer. A note longer than this is a document, not a note. The
	// bound is in runes because that is what the reader sees; the writer's own
	// byte limit is a little higher still.
	fileCompanionGenerateMaxRunes = 40000
	// fileCompanionGenerateMaxPerTurn caps how many PDFs one turn may leave
	// behind. A turn that keeps asking for another file is not answering.
	fileCompanionGenerateMaxPerTurn = 3
	// fileCompanionGenerateMaxFigures caps how many original drawings one
	// notes PDF may carry. A textbook has dozens; a note has room for a few.
	fileCompanionGenerateMaxFigures = 8
	// fileCompanionGenerateFooter is the line every generated PDF carries.
	fileCompanionGenerateFooter = "由 MaClaw 伴读生成"
)

// FileCompanionFile is one file the companion produced in this turn. The
// window shows it as a 产出物 card: the reader opens it or saves it wherever
// they want it.
type FileCompanionFile struct {
	Label string `json:"label"`
	Path  string `json:"path"`
	Size  int64  `json:"size,omitempty"`
}

// fileCompanionGeneratedManifest remembers the files one session produced, so
// a reopened window can still show them. Answer is the 1-based ordinal of the
// answer bubble the file belongs to.
type fileCompanionGeneratedManifest struct {
	Files []fileCompanionGeneratedEntry `json:"files"`
}

type fileCompanionGeneratedEntry struct {
	Label  string `json:"label"`
	Path   string `json:"path"`
	Size   int64  `json:"size"`
	Answer int    `json:"answer"`
}

// fileCompanionToolGenerate writes one PDF from the model's own Markdown.
// The file lands in the session results directory and is handed back to the
// window as a 产出物 card, so the answer can promise a file that exists.
func (a *App) fileCompanionToolGenerate(userID, canonical string, args map[string]interface{}) (string, bool) {
	body, err := fileCompanionGenerateBody(args)
	if err != nil {
		return err.Error(), false
	}
	sessionID := fileCompanionSessionOf(userID)
	if sessionID == "" {
		return "生成 PDF 失败：找不到这次会话", false
	}
	if room := a.fileCompanionGenerateRoom(userID); room <= 0 {
		return fmt.Sprintf("本轮已经生成 %d 个文件，无法继续生成。请先把已经生成的文件告诉用户。", fileCompanionGenerateMaxPerTurn), false
	}
	title, subtitle := fileCompanionGenerateTitle(args, canonical)
	// The drawings the notes point at ride along: a note about a picture is
	// not a note without the picture.
	if !fileCompanionMarkdownOpenFence(body) {
		withFigures, count := a.fileCompanionGenerateFigures(userID, body)
		if count > 0 {
			logFileCompanion("tool generate figures=%d user=%s", count, userID)
			body = withFigures
		}
	}
	dest, err := a.fileCompanionGeneratePath(sessionID, fileCompanionGenerateFileName(args, canonical))
	if err != nil {
		logFileCompanion("tool generate refused user=%s: %v", userID, err)
		return "生成 PDF 失败：" + err.Error(), false
	}
	// The open document must never become the output, whatever the name says.
	// The writer only ever names a file that does not exist yet, so the open
	// document cannot be the target; the check stays as a cheap guard in case
	// the naming rules ever change.
	if sameFileCompanionExportPath(canonical, dest) {
		return "生成 PDF 失败：不能覆盖当前文件", false
	}
	written, err := docgen.GenerateToFile(docgen.Spec{
		Title:          title,
		Subtitle:       subtitle,
		Content:        body,
		Brand:          fileCompanionExportBrand,
		FooterHint:     fileCompanionGenerateFooter,
		FileNamePrefix: "companion",
		PaperSize:      "A4",
		Colorful:       true,
	}, dest)
	if err != nil {
		logFileCompanion("tool generate failed user=%s: %v", userID, err)
		return "生成 PDF 失败：" + err.Error(), false
	}
	file := FileCompanionFile{Label: filepath.Base(written), Path: written}
	if info, statErr := os.Stat(written); statErr == nil {
		file.Size = info.Size()
	}
	a.appendFileCompanionTurnFiles(userID, file)
	a.fileCompanionRecordGeneratedFile(sessionID, file, a.fileCompanionAnswerOrdinal(userID))
	logFileCompanion("tool generate user=%s file=%s", userID, written)
	return fmt.Sprintf("已生成 PDF：%s\n宿主会把它作为「产出物」放在回答下面，用户可以点「查看文档」，也可以点「保存」存到任意目录。"+
		"回答里说明文件已经生成，并提示用户用产出物卡片里的「保存」选择保存位置；不要把上面的路径贴进回答，也不要声称已经保存到某个目录。", written), true
}

// fileCompanionGenerateFigures appends the drawings the notes talk about. The
// numbering is the one the model saw this turn (图1, 图2…), and the match is
// the same one that decides which pictures the window shows under the answer —
// so the PDF carries exactly the drawings the reader can already see.
//
// A picture that is not on disk is skipped: a notes PDF with one missing
// drawing is better than no PDF at all.
func (a *App) fileCompanionGenerateFigures(userID, body string) (string, int) {
	figures := a.fileCompanionTurnFigures(userID)
	if len(figures) == 0 {
		return body, 0
	}
	matched := fileCompanionFiguresForReply(figures, "", body)
	if len(matched) == 0 {
		return body, 0
	}
	var b strings.Builder
	b.WriteString(body)
	b.WriteString("\n\n## 原文配图\n\n")
	added := 0
	for _, fig := range matched {
		if added >= fileCompanionGenerateMaxFigures {
			break
		}
		if info, err := os.Stat(fig.Path); err != nil || info.IsDir() || info.Size() == 0 {
			continue
		}
		alt := fileCompanionGenerateAltText(fig.Label, fig.Caption)
		b.WriteString(fmt.Sprintf("![%s](<%s>)\n\n", alt, filepath.ToSlash(fig.Path)))
		added++
	}
	if added == 0 {
		return body, 0
	}
	return b.String(), added
}

// fileCompanionGenerateAltText builds the image caption of one drawing. A
// caption comes from the document, and a document caption may hold "]" or a
// newline — either one would end the Markdown image early and leave the rest
// of the line as a broken link.
func fileCompanionGenerateAltText(label, caption string) string {
	clean := func(text string) string {
		var b strings.Builder
		for _, r := range text {
			switch r {
			case '[', ']', '<', '>', '\n', '\r', '\t':
				b.WriteRune(' ')
			default:
				b.WriteRune(r)
			}
		}
		return strings.Join(strings.Fields(b.String()), " ")
	}
	alt := clean(label)
	if caption = clean(caption); caption != "" {
		alt = strings.TrimSpace(alt + " " + caption)
	}
	return alt
}

// fileCompanionMarkdownOpenFence reports whether the markdown ends inside an
// unclosed code fence. Appending after it would put the drawings inside the
// code block.
func fileCompanionMarkdownOpenFence(body string) bool {
	fences := 0
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fences++
		}
	}
	return fences%2 == 1
}

// fileCompanionSessionOf returns the session id behind a companion user id.
// The id is the only name the writer uses, so a caller never has to guess
// which spelling of a document path belongs to the session.
func fileCompanionSessionOf(userID string) string {
	userID = strings.TrimSpace(userID)
	if !strings.HasPrefix(userID, "file-companion:") {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(userID, "file-companion:"))
}

// fileCompanionGenerateBody reads the Markdown the writer is asked to render.
// The model writes the body under one of a few names, and a missing body is
// the model's mistake to retry.
func fileCompanionGenerateBody(args map[string]interface{}) (string, error) {
	body := ""
	for _, name := range []string{"markdown", "content", "body"} {
		if value := fileCompanionStringArg(args, name); value != "" {
			body = value
			break
		}
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", errors.New("markdown 缺失：请把要写进 PDF 的正文放在 markdown 里")
	}
	if runes := utf8.RuneCountInString(body); runes > fileCompanionGenerateMaxRunes {
		return "", fmt.Errorf("正文太长：一次最多 %d 字，当前 %d 字。请分多次生成，或先精简正文。", fileCompanionGenerateMaxRunes, runes)
	}
	return body, nil
}

// fileCompanionGenerateTitle reads the document title. Without one the file
// is named after the document it was made from, and there is no second line
// to repeat it.
func fileCompanionGenerateTitle(args map[string]interface{}, canonical string) (title, subtitle string) {
	source := fileCompanionPaperSourceName(canonical)
	given := strings.TrimSpace(fileCompanionStringArg(args, "title"))
	if given == "" {
		return source + " 笔记", ""
	}
	if runes := []rune(given); len(runes) > 80 {
		given = string(runes[:80])
	}
	return given, source
}

// fileCompanionGenerateFileName reads the wanted file name. A name is a name,
// never a path: the directory part is dropped, because the destination is the
// host's decision.
func fileCompanionGenerateFileName(args map[string]interface{}, canonical string) string {
	raw := ""
	for _, key := range []string{"filename", "file_name", "name"} {
		if value := fileCompanionStringArg(args, key); value != "" {
			raw = value
			break
		}
	}
	raw = strings.TrimSpace(raw)
	if raw != "" {
		// Only the last path element survives, and the extension is added by
		// the host.
		raw = filepath.Base(strings.ReplaceAll(raw, "\\", "/"))
		if ext := filepath.Ext(raw); strings.EqualFold(ext, ".pdf") {
			raw = strings.TrimSuffix(raw, ext)
		}
	}
	base := strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return -1
		}
		return r
	}, raw)
	base = strings.Trim(base, " .")
	if runes := []rune(base); len(runes) > 60 {
		base = strings.TrimRight(string(runes[:60]), " .")
	}
	if strings.TrimSpace(base) == "" {
		base = fileCompanionPaperSourceName(canonical) + "-笔记"
	}
	return base
}

// fileCompanionGeneratePath is where the file goes: the session's own results
// directory. An existing file is never overwritten — the call gets the next
// free name instead, so two notes in one session are two files.
//
// The session id is the only name this uses. It never derives one from a
// document path: Windows lowercases the canonical path, so a hash taken from
// the caller's spelling would land in a different directory than the one the
// session reads.
func (a *App) fileCompanionGeneratePath(sessionID, name string) (string, error) {
	if a == nil {
		return "", errors.New("app unavailable")
	}
	dir := filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", errors.New("生成目录创建失败")
	}
	candidate := filepath.Join(dir, name+".pdf")
	for i := 2; i < 1000; i++ {
		if _, err := os.Stat(candidate); err != nil {
			break
		}
		candidate = filepath.Join(dir, fmt.Sprintf("%s-%d.pdf", name, i))
	}
	// The name ran out of free numbers: refuse instead of writing over a file
	// that is already there.
	if _, err := os.Stat(candidate); err == nil {
		return "", errors.New("同名文件太多，无法再生成新的文件。请换一个 filename。")
	}
	return candidate, nil
}

// fileCompanionGenerateRoom is how many more files this turn may leave behind.
func (a *App) fileCompanionGenerateRoom(userID string) int {
	if a == nil {
		return 0
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	room := fileCompanionGenerateMaxPerTurn - len(st.turnFiles[userID])
	if room < 0 {
		return 0
	}
	return room
}

// seedFileCompanionTurnFiles starts one turn's produced-file list empty. A
// file an earlier turn produced travels on that turn's reply; it must not
// reappear under this turn's answer.
func (a *App) seedFileCompanionTurnFiles(userID string) {
	if a == nil {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.turnFiles == nil {
		st.turnFiles = map[string][]FileCompanionFile{}
	}
	st.turnFiles[userID] = nil
}

// appendFileCompanionTurnFiles registers one produced file for this turn.
func (a *App) appendFileCompanionTurnFiles(userID string, file FileCompanionFile) {
	if a == nil || strings.TrimSpace(file.Path) == "" {
		return
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.turnFiles == nil {
		st.turnFiles = map[string][]FileCompanionFile{}
	}
	for _, existing := range st.turnFiles[userID] {
		if fileCompanionFigureKey(existing.Path) == fileCompanionFigureKey(file.Path) {
			return
		}
	}
	st.turnFiles[userID] = append(st.turnFiles[userID], file)
}

// fileCompanionTurnFiles is the list of files this turn produced.
func (a *App) fileCompanionTurnFiles(userID string) []FileCompanionFile {
	if a == nil {
		return nil
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]FileCompanionFile(nil), st.turnFiles[userID]...)
}

// fileCompanionAnswerOrdinal is the 1-based number of the answer bubble the
// file belongs to. It counts the answer bubbles the way the restored history
// counts them, so the same file lands on the same bubble after a restart.
func (a *App) fileCompanionAnswerOrdinal(userID string) int {
	if a == nil || a.imHandler == nil || a.imHandler.memory == nil {
		return 1
	}
	n := 0
	for _, entry := range a.imHandler.memory.Load(userID) {
		if !strings.EqualFold(strings.TrimSpace(entry.Role), "assistant") {
			continue
		}
		if fileCompanionPlanningOnly(entry) {
			continue
		}
		if strings.TrimSpace(fileCompanionEntryText(entry.Content)) == "" {
			continue
		}
		n++
	}
	return n + 1
}

// fileCompanionGeneratedMu serializes the manifest. The file is read, changed
// and written back; two turns that finish together must not lose an entry.
var fileCompanionGeneratedMu sync.Mutex

// fileCompanionRecordGeneratedFile remembers one produced file for the
// session, so a reopened window can show it again.
func (a *App) fileCompanionRecordGeneratedFile(sessionID string, file FileCompanionFile, answer int) {
	if a == nil || strings.TrimSpace(file.Path) == "" {
		return
	}
	path := a.fileCompanionGeneratedManifestPathBySession(sessionID)
	if path == "" {
		return
	}
	fileCompanionGeneratedMu.Lock()
	defer fileCompanionGeneratedMu.Unlock()
	manifest := a.fileCompanionGeneratedManifestBySession(sessionID)
	manifest.Files = append(manifest.Files, fileCompanionGeneratedEntry{
		Label:  file.Label,
		Path:   file.Path,
		Size:   file.Size,
		Answer: answer,
	})
	if len(manifest.Files) > fileCompanionGenerateMaxPerTurn*8 {
		manifest.Files = manifest.Files[len(manifest.Files)-fileCompanionGenerateMaxPerTurn*8:]
	}
	if err := fileCompanionWriteGeneratedManifest(path, manifest); err != nil {
		logFileCompanion("generated record failed")
	}
}

// fileCompanionWriteGeneratedManifest writes the manifest in one step. A
// plain write truncates first, and a reader that arrives in that window sees
// half a file and believes the session produced nothing.
func fileCompanionWriteGeneratedManifest(path string, manifest fileCompanionGeneratedManifest) error {
	raw, err := json.Marshal(manifest)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".generated-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer func() { _ = os.Remove(name) }()
	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// A rename inside one directory replaces the old file in one step, so a
	// reader never sees a half-written manifest.
	return os.Rename(name, path)
}

// fileCompanionGeneratedManifestPathBySession is the manifest of one session,
// named by the session id alone so /clear can drop it without the document.
func (a *App) fileCompanionGeneratedManifestPathBySession(sessionID string) string {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" || strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\`) {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID, "generated.json")
}

// fileCompanionGeneratedManifestBySession reads the files one session
// produced, named by the session id.
func (a *App) fileCompanionGeneratedManifestBySession(sessionID string) fileCompanionGeneratedManifest {
	var manifest fileCompanionGeneratedManifest
	if a == nil {
		return manifest
	}
	path := a.fileCompanionGeneratedManifestPathBySession(sessionID)
	if path == "" {
		return manifest
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return manifest
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fileCompanionGeneratedManifest{}
	}
	return manifest
}

// fileCompanionAttachGeneratedFiles puts the produced files back on the
// answers they belong to. The manifest records the answer ordinal, so a
// reopened window shows the same card under the same bubble.
func (a *App) fileCompanionAttachGeneratedFiles(sessionID string, messages []FileCompanionMessage) []FileCompanionMessage {
	if a == nil || len(messages) == 0 {
		return messages
	}
	manifest := a.fileCompanionGeneratedManifestBySession(sessionID)
	if len(manifest.Files) == 0 {
		return messages
	}
	byAnswer := map[int][]FileCompanionFile{}
	for _, entry := range manifest.Files {
		if strings.TrimSpace(entry.Path) == "" {
			continue
		}
		if _, err := os.Stat(entry.Path); err != nil {
			continue
		}
		answer := entry.Answer
		if answer < 1 {
			answer = 1
		}
		byAnswer[answer] = append(byAnswer[answer], FileCompanionFile{
			Label: entry.Label,
			Path:  entry.Path,
			Size:  entry.Size,
		})
	}
	if len(byAnswer) == 0 {
		return messages
	}
	seen := 0
	for i := range messages {
		// Only a bubble that carries text is an answer. The restored list also
		// holds assistant bubbles made of a pending plan alone (Reasoning, no
		// Text); counting those would shift every file one bubble down.
		if messages[i].Role != "assistant" || strings.TrimSpace(messages[i].Text) == "" {
			continue
		}
		seen++
		files := byAnswer[seen]
		if len(files) == 0 {
			continue
		}
		messages[i].Files = append(messages[i].Files, files...)
	}
	return messages
}

// fileCompanionStringArg reads one string argument. Models quote numbers and
// numbers where strings belong often enough that both spellings are read.
func fileCompanionStringArg(args map[string]interface{}, name string) string {
	if args == nil {
		return ""
	}
	raw, ok := args[name]
	if !ok || raw == nil {
		return ""
	}
	switch value := raw.(type) {
	case string:
		return strings.TrimSpace(value)
	case float64:
		return strings.TrimSpace(fmt.Sprintf("%v", value))
	case json.Number:
		return strings.TrimSpace(value.String())
	case bool:
		return ""
	default:
		return ""
	}
}
