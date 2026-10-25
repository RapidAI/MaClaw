package guiapp

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// A companion project is a zip the reader can open again. It holds the
// document bytes, the highlight list, and the chat. The PDF inside is a copy.
// Highlights are not written into that PDF.

const (
	fileCompanionProjectKind         = "maclaw-file-companion"
	fileCompanionProjectVersion      = 1
	fileCompanionProjectManifestName = "maclaw-companion.json"
	fileCompanionProjectPaperEntry   = "sidecars/paper-result.pdf"
	fileCompanionProjectMaxMarks     = 8 << 20
	fileCompanionProjectMaxDoc       = 8 << 30
	fileCompanionProjectMaxSidecar   = 512 << 20
)

var errFileCompanionNotProject = errors.New("不是伴读项目")

type fileCompanionProjectManifest struct {
	Kind        string                 `json:"kind"`
	Version     int                    `json:"version"`
	Name        string                 `json:"name"`
	Highlights  json.RawMessage        `json:"highlights"`
	Messages    []FileCompanionMessage `json:"messages"`
	PaperResult string                 `json:"paperResult,omitempty"`
	PaperTurn   int                    `json:"paperTurn,omitempty"`
}

type fileCompanionProjectMark struct {
	ID    string                     `json:"id"`
	Page  int                        `json:"page"`
	Color string                     `json:"color"`
	Rects []fileCompanionProjectRect `json:"rects"`
	Text  string                     `json:"text,omitempty"`
}

type fileCompanionProjectRect struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
	W float64 `json:"w"`
	H float64 `json:"h"`
}

type fileCompanionProjectEntry struct {
	Role             string `json:"role"`
	Content          string `json:"content"`
	ReasoningContent string `json:"reasoning_content,omitempty"`
}

// Overridable so tests can pick a destination without a desktop dialog.
var fileCompanionSaveProjectDialog = func(a *App, defaultFilename string) (string, error) {
	if a == nil || a.ctx == nil {
		return "", nil
	}
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           "导出项目",
		DefaultFilename: defaultFilename,
		Filters: []runtime.FileFilter{
			{DisplayName: "伴读项目 (*.zip)", Pattern: "*.zip"},
		},
	})
}

// FileCompanionExportProject packs the open document, its highlights, and the
// chat into a zip. An empty dialog result is a cancel, not an error. The open
// file is only read.
func (a *App) FileCompanionExportProject(path, highlightsJSON string) (string, error) {
	if a == nil {
		return "", errors.New("应用不可用")
	}
	grant, canonical, err := a.fileCompanionGrant(path)
	if err != nil || grant == nil {
		return "", errFileCompanionNotGranted
	}
	source := strings.TrimSpace(grant.Canonical)
	if source == "" {
		source = canonical
	}
	info, err := os.Stat(source)
	if err != nil || info.IsDir() {
		return "", errors.New("文件无法读取")
	}
	highlights, err := fileCompanionCleanHighlights(highlightsJSON)
	if err != nil {
		return "", err
	}
	messages := a.fileCompanionHistory(fileCompanionSessionID(source))
	paperPath, paperTurn := fileCompanionProjectPaper(messages)
	dest, err := fileCompanionSaveProjectDialog(a, fileCompanionProjectFileName(source))
	if err != nil {
		return "", err
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		return "", nil
	}
	if abs, absErr := filepath.Abs(dest); absErr == nil {
		dest = abs
	}
	// The dialog can return the open document itself. Refuse that spelling
	// before a missing .zip suffix turns it into a different path.
	if sameFileCompanionExportPath(source, dest) {
		return "", errors.New("不能覆盖当前文件")
	}
	if !strings.EqualFold(filepath.Ext(dest), ".zip") {
		dest += ".zip"
	}
	if stat, statErr := os.Stat(dest); statErr == nil && stat.IsDir() {
		return "", errors.New("无法保存到该位置")
	}
	if sameFileCompanionExportPath(source, dest) {
		return "", errors.New("不能覆盖当前文件")
	}
	if err := writeFileCompanionProject(dest, source, highlights, messages, paperPath, paperTurn); err != nil {
		return "", err
	}
	logFileCompanion("export project")
	return dest, nil
}

func fileCompanionProjectFileName(source string) string {
	base := strings.TrimSuffix(filepath.Base(source), filepath.Ext(source))
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == ".." {
		base = "document"
	}
	runes := []rune(base)
	if len(runes) > 60 {
		base = string(runes[:60])
	}
	return base + "-伴读项目.zip"
}

func fileCompanionCleanHighlights(raw string) (json.RawMessage, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "null" {
		return json.RawMessage("[]"), nil
	}
	if len(raw) > fileCompanionProjectMaxMarks {
		return nil, errors.New("高亮无法保存")
	}
	var incoming []json.RawMessage
	if err := json.Unmarshal([]byte(raw), &incoming); err != nil {
		return nil, errors.New("高亮无法保存")
	}
	cleaned := make([]fileCompanionProjectMark, 0, len(incoming))
	for _, item := range incoming {
		if len(cleaned) >= 2000 {
			break
		}
		var mark fileCompanionProjectMark
		if err := json.Unmarshal(item, &mark); err != nil {
			continue
		}
		next, ok := fileCompanionCleanMark(mark)
		if ok {
			cleaned = append(cleaned, next)
		}
	}
	encoded, err := json.Marshal(cleaned)
	if err != nil {
		return nil, errors.New("高亮无法保存")
	}
	return encoded, nil
}

func fileCompanionCleanMark(mark fileCompanionProjectMark) (fileCompanionProjectMark, bool) {
	switch mark.Color {
	case "yellow", "green", "blue", "pink":
	default:
		return fileCompanionProjectMark{}, false
	}
	if mark.Page < 1 || mark.Page > 100000 {
		return fileCompanionProjectMark{}, false
	}
	id := strings.TrimSpace(mark.ID)
	if id == "" || len(id) > 80 {
		return fileCompanionProjectMark{}, false
	}
	rects := make([]fileCompanionProjectRect, 0, len(mark.Rects))
	for _, rect := range mark.Rects {
		if len(rects) >= 40 {
			break
		}
		if !fileCompanionRectOK(rect.X) || !fileCompanionRectOK(rect.Y) || !fileCompanionRectOK(rect.W) || !fileCompanionRectOK(rect.H) {
			continue
		}
		if rect.W < 0.001 || rect.H < 0.001 || rect.X+rect.W > 1.001 || rect.Y+rect.H > 1.001 {
			continue
		}
		rects = append(rects, rect)
	}
	if len(rects) == 0 {
		return fileCompanionProjectMark{}, false
	}
	text := strings.TrimSpace(mark.Text)
	if len([]rune(text)) > 500 {
		text = string([]rune(text)[:500])
	}
	return fileCompanionProjectMark{ID: id, Page: mark.Page, Color: mark.Color, Rects: rects, Text: text}, true
}

func fileCompanionRectOK(value float64) bool {
	return !((value != value) || value < 0 || value > 1)
}

func fileCompanionProjectPaper(messages []FileCompanionMessage) (string, int) {
	seen := 0
	path := ""
	turn := 0
	for i := 0; i < len(messages)-1; i++ {
		if messages[i].Role != "user" || !messages[i].Paper || messages[i+1].Role != "assistant" {
			continue
		}
		seen++
		candidate := strings.TrimSpace(messages[i+1].ResultPath)
		if candidate == "" {
			continue
		}
		info, err := os.Stat(candidate)
		if err != nil || info.IsDir() {
			continue
		}
		path = candidate
		turn = seen
	}
	return path, turn
}

func fileCompanionProjectSafeName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "document"
	}
	cleaned := strings.Map(func(r rune) rune {
		if r < 32 || r == '/' || r == '\\' || r == ':' {
			return '_'
		}
		return r
	}, name)
	runes := []rune(cleaned)
	if len(runes) > 180 {
		cleaned = string(runes[:180])
	}
	if cleaned == "" {
		return "document"
	}
	return cleaned
}

func writeFileCompanionProject(dest, source string, highlights json.RawMessage, messages []FileCompanionMessage, paperPath string, paperTurn int) error {
	src, err := os.Open(source)
	if err != nil {
		return errors.New("文件无法读取")
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil || info.IsDir() {
		return errors.New("文件无法读取")
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return errors.New("无法保存到该位置")
	}
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return errors.New("无法保存到该位置")
	}
	failed := true
	defer func() {
		if failed {
			_ = os.Remove(tmp)
		}
	}()
	zw := zip.NewWriter(out)
	manifest := fileCompanionProjectManifest{
		Kind:       fileCompanionProjectKind,
		Version:    fileCompanionProjectVersion,
		Name:       filepath.Base(source),
		Highlights: highlights,
		Messages:   messages,
	}
	if paperPath != "" && paperTurn > 0 {
		manifest.PaperResult = fileCompanionProjectPaperEntry
		manifest.PaperTurn = paperTurn
	}
	if err := writeProjectJSON(zw, fileCompanionProjectManifestName, manifest); err != nil {
		_ = zw.Close()
		_ = out.Close()
		return err
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		return errors.New("无法保存项目")
	}
	header.Name = "document/" + fileCompanionProjectSafeName(filepath.Base(source))
	header.Method = zip.Store
	dw, err := zw.CreateHeader(header)
	if err != nil {
		_ = zw.Close()
		_ = out.Close()
		return errors.New("无法保存项目")
	}
	copied, err := io.Copy(dw, io.LimitReader(src, fileCompanionProjectMaxDoc+1))
	if err != nil || copied > fileCompanionProjectMaxDoc {
		_ = zw.Close()
		_ = out.Close()
		return errors.New("文件过大")
	}
	if manifest.PaperResult != "" {
		if err := copyProjectFile(zw, manifest.PaperResult, paperPath, fileCompanionProjectMaxSidecar); err != nil {
			_ = zw.Close()
			_ = out.Close()
			return err
		}
	}
	if err := zw.Close(); err != nil {
		_ = out.Close()
		return errors.New("无法保存项目")
	}
	if err := out.Close(); err != nil {
		return errors.New("无法保存项目")
	}
	if err := replaceFileCompanionProject(tmp, dest); err != nil {
		return err
	}
	failed = false
	return nil
}

func writeProjectJSON(zw *zip.Writer, name string, value any) error {
	w, err := zw.Create(name)
	if err != nil {
		return errors.New("无法保存项目")
	}
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return errors.New("无法保存项目")
	}
	return nil
}

func copyProjectFile(zw *zip.Writer, name, source string, limit int64) error {
	file, err := os.Open(source)
	if err != nil {
		return errors.New("无法保存项目")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.IsDir() || info.Size() > limit {
		return errors.New("文件过大")
	}
	header, err := zip.FileInfoHeader(info)
	if err != nil {
		return errors.New("无法保存项目")
	}
	header.Name = name
	header.Method = zip.Store
	w, err := zw.CreateHeader(header)
	if err != nil {
		return errors.New("无法保存项目")
	}
	n, err := io.Copy(w, io.LimitReader(file, limit+1))
	if err != nil || n > limit {
		return errors.New("文件过大")
	}
	return nil
}

func replaceFileCompanionProject(tmp, dest string) error {
	if err := os.Rename(tmp, dest); err == nil {
		return nil
	}
	_ = os.Remove(dest)
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return errors.New("无法保存到该位置")
	}
	return nil
}

func (a *App) fileCompanionOpenProjectZip(path string) (FileCompanionDocument, error) {
	doc := FileCompanionDocument{Path: path, Name: filepath.Base(path)}
	extracted, highlights, err := a.importFileCompanionProject(path)
	if err != nil {
		if errors.Is(err, errFileCompanionNotProject) {
			doc.Error = errFileCompanionNotProject.Error()
		} else {
			doc.Error = err.Error()
		}
		return doc, nil
	}
	opened, err := a.fileCompanionOpen(extracted, false)
	if err != nil {
		return opened, err
	}
	if opened.Error != "" {
		return opened, nil
	}
	opened.Highlights = highlights
	opened.Imported = true
	logFileCompanion("import project")
	return opened, nil
}

func (a *App) importFileCompanionProject(path string) (string, json.RawMessage, error) {
	if a == nil {
		return "", nil, errors.New("应用不可用")
	}
	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", nil, errFileCompanionNotProject
	}
	defer reader.Close()
	var manifestFile *zip.File
	var documentFile *zip.File
	var paperFile *zip.File
	for _, file := range reader.File {
		kind, ok := fileCompanionProjectEntryKind(file.Name)
		if !ok {
			if strings.HasSuffix(file.Name, "/") {
				continue
			}
			return "", nil, errFileCompanionNotProject
		}
		switch kind {
		case "manifest":
			if manifestFile != nil {
				return "", nil, errFileCompanionNotProject
			}
			manifestFile = file
		case "document":
			if documentFile != nil {
				return "", nil, errFileCompanionNotProject
			}
			documentFile = file
		case "paper":
			paperFile = file
		default:
			return "", nil, errFileCompanionNotProject
		}
	}
	if manifestFile == nil || documentFile == nil {
		return "", nil, errFileCompanionNotProject
	}
	if manifestFile.UncompressedSize64 > fileCompanionProjectMaxMarks || documentFile.UncompressedSize64 > fileCompanionProjectMaxDoc {
		return "", nil, errors.New("文件过大")
	}
	manifest, err := readProjectManifest(manifestFile)
	if err != nil {
		return "", nil, err
	}
	if manifest.PaperResult != "" && (paperFile == nil || paperFile.Name != fileCompanionProjectPaperEntry) {
		return "", nil, errFileCompanionNotProject
	}
	root := filepath.Join(a.GetDataDir(), "file-companion", "projects")
	tmpDir, err := os.MkdirTemp(root, "incoming-")
	if err != nil {
		if mkErr := os.MkdirAll(root, 0o755); mkErr != nil {
			return "", nil, errors.New("无法打开项目")
		}
		tmpDir, err = os.MkdirTemp(root, "incoming-")
		if err != nil {
			return "", nil, errors.New("无法打开项目")
		}
	}
	defer os.RemoveAll(tmpDir)
	safeName := fileCompanionProjectSafeName(manifest.Name)
	if safeName == "document" {
		safeName = fileCompanionProjectSafeName(strings.TrimPrefix(strings.ReplaceAll(documentFile.Name, "\\", "/"), "document/"))
	}
	tmpFile := filepath.Join(tmpDir, safeName)
	sum, err := extractProjectFile(documentFile, tmpFile, fileCompanionProjectMaxDoc)
	if err != nil {
		return "", nil, err
	}
	finalDir := filepath.Join(root, hex.EncodeToString(sum[:16]))
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		return "", nil, errors.New("无法打开项目")
	}
	finalPath := filepath.Join(finalDir, safeName)
	_ = os.Remove(finalPath)
	if err := os.Rename(tmpFile, finalPath); err != nil {
		if copyErr := copyPlainFile(tmpFile, finalPath); copyErr != nil {
			return "", nil, errors.New("无法打开项目")
		}
	}
	identity, _, idErr := fileCompanionPathIdentity(finalPath)
	if idErr != nil {
		return "", nil, errors.New("无法打开项目")
	}
	sessionID := fileCompanionSessionID(identity)
	if err := a.writeFileCompanionProjectSession(sessionID, manifest.Messages); err != nil {
		return "", nil, err
	}
	if manifest.PaperResult != "" && paperFile != nil && manifest.PaperTurn > 0 {
		if err := a.installFileCompanionProjectPaper(sessionID, paperFile, manifest.PaperTurn); err != nil {
			return "", nil, err
		}
	}
	highlights := manifest.Highlights
	if len(highlights) == 0 {
		highlights = json.RawMessage("[]")
	}
	return identity, highlights, nil
}

func fileCompanionProjectEntryKind(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.Contains(name, "../") {
		return "", false
	}
	if strings.HasSuffix(name, "/") {
		return "", false
	}
	if name == fileCompanionProjectManifestName {
		return "manifest", true
	}
	if name == fileCompanionProjectPaperEntry {
		return "paper", true
	}
	if strings.HasPrefix(name, "document/") {
		rest := strings.TrimPrefix(name, "document/")
		if rest == "" || strings.Contains(rest, "/") || rest == "." || rest == ".." {
			return "", false
		}
		return "document", true
	}
	return "", false
}

func readProjectManifest(file *zip.File) (fileCompanionProjectManifest, error) {
	var manifest fileCompanionProjectManifest
	rc, err := file.Open()
	if err != nil {
		return manifest, errFileCompanionNotProject
	}
	defer rc.Close()
	raw, err := io.ReadAll(io.LimitReader(rc, fileCompanionProjectMaxMarks+1))
	if err != nil || int64(len(raw)) > fileCompanionProjectMaxMarks {
		return manifest, errFileCompanionNotProject
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return manifest, errFileCompanionNotProject
	}
	if manifest.Kind != fileCompanionProjectKind || manifest.Version != fileCompanionProjectVersion {
		return manifest, errFileCompanionNotProject
	}
	if len(manifest.Highlights) == 0 {
		manifest.Highlights = json.RawMessage("[]")
	}
	cleaned, err := fileCompanionCleanHighlights(string(manifest.Highlights))
	if err != nil {
		return manifest, errFileCompanionNotProject
	}
	manifest.Highlights = cleaned
	return manifest, nil
}

func extractProjectFile(file *zip.File, dest string, limit int64) ([32]byte, error) {
	var sum [32]byte
	if file.UncompressedSize64 > uint64(limit) {
		return sum, errors.New("文件过大")
	}
	rc, err := file.Open()
	if err != nil {
		return sum, errFileCompanionNotProject
	}
	defer rc.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return sum, errors.New("无法打开项目")
	}
	defer out.Close()
	hash := sha256.New()
	n, err := io.Copy(out, io.TeeReader(io.LimitReader(rc, limit+1), hash))
	if err != nil {
		return sum, errors.New("无法打开项目")
	}
	if n > limit {
		return sum, errors.New("文件过大")
	}
	copy(sum[:], hash.Sum(nil))
	return sum, nil
}

func copyPlainFile(source, dest string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}

func (a *App) writeFileCompanionProjectSession(sessionID string, messages []FileCompanionMessage) error {
	sessionID = strings.TrimSpace(sessionID)
	if a == nil || sessionID == "" {
		return errors.New("无法打开项目")
	}
	userID := "file-companion:" + sessionID
	// History reads the in-memory copy first. Clear would also delete the side
	// file and unbind it. The replacement bumps the save generation and writes
	// the sharer's transcript before it releases the lock, so a turn that
	// already loaded the old chat cannot write that chat back. The side file
	// is written by that save. Cancel comes after the bump: a save from the
	// turn being stopped then sees the new generation and is dropped.
	if err := a.ensureFileCompanionHandler(userID, sessionID); err != nil {
		return errors.New("无法打开项目")
	}
	stored := fileCompanionProjectEntries(messages)
	entries := make([]agent.ConversationEntry, 0, len(stored))
	for _, entry := range stored {
		entries = append(entries, agent.ConversationEntry{
			Role:             entry.Role,
			Content:          entry.Content,
			ReasoningContent: entry.ReasoningContent,
		})
	}
	if !a.fileCompanionReplaceChat(userID, entries) {
		return errors.New("无法打开项目")
	}
	if a.imHandler != nil {
		_, _ = a.imHandler.RequestCancelSessionForUser(userID)
	}
	return nil
}

func fileCompanionProjectEntries(messages []FileCompanionMessage) []fileCompanionProjectEntry {
	if len(messages) == 0 {
		return []fileCompanionProjectEntry{}
	}
	out := make([]fileCompanionProjectEntry, 0, len(messages))
	for _, message := range messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role != "user" && role != "assistant" {
			continue
		}
		content := message.Text
		if role == "user" && message.Paper && !strings.Contains(content, fileCompanionPaperMarker) {
			content = strings.TrimSpace(content) + "\n" + fileCompanionPaperMarker
		}
		reasoning := ""
		if role == "assistant" {
			reasoning = strings.TrimSpace(message.Reasoning)
		}
		if strings.TrimSpace(content) == "" && reasoning == "" {
			continue
		}
		out = append(out, fileCompanionProjectEntry{Role: role, Content: content, ReasoningContent: reasoning})
	}
	return out
}

func (a *App) installFileCompanionProjectPaper(sessionID string, file *zip.File, turn int) error {
	if file.UncompressedSize64 > fileCompanionProjectMaxSidecar {
		return errors.New("文件过大")
	}
	dir := filepath.Join(a.GetDataDir(), "file-companion", "results", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.New("无法打开项目")
	}
	pdfPath := filepath.Join(dir, "论文解读.pdf")
	if _, err := extractProjectFile(file, pdfPath, fileCompanionProjectMaxSidecar); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "latest.txt"), []byte(pdfPath), 0o644); err != nil {
		return errors.New("无法打开项目")
	}
	if turn < 1 {
		turn = 1
	}
	if err := os.WriteFile(filepath.Join(dir, "latest-turn.txt"), []byte(itoaPositive(turn)), 0o644); err != nil {
		return errors.New("无法打开项目")
	}
	return nil
}

func itoaPositive(n int) string {
	if n < 1 {
		return "1"
	}
	digits := make([]byte, 0, 8)
	for n > 0 {
		digits = append(digits, byte('0'+n%10))
		n /= 10
	}
	for i, j := 0, len(digits)-1; i < j; i, j = i+1, j-1 {
		digits[i], digits[j] = digits[j], digits[i]
	}
	return string(digits)
}

func marshalProjectJSON(value any) ([]byte, error) {
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(value); err != nil {
		return nil, err
	}
	return []byte(strings.TrimRight(buf.String(), "\n")), nil
}
