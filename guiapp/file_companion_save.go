package guiapp

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// fileCompanionGrant is the in-memory open-file grant. It is not persisted.
// Equality is the canonical path, not a directory prefix.
type fileCompanionGrant struct {
	UserPath   string
	Canonical  string
	Editable   bool
	ReadOnly   bool
	CanExport  bool
	LoadedSize int64
	LoadedMod  int64
	LoadedHash string
	Selection  string
	// SelectionStart is a UTF-16 offset into the LF-normalized file.
	// -1 means the client did not report one. Zero would prefer the first character.
	SelectionStart int
}

// FileCompanionDocument is one opened tab.
type FileCompanionDocument struct {
	Path           string                 `json:"path"`
	Name           string                 `json:"name"`
	Editable       bool                   `json:"editable"`
	ReadOnly       bool                   `json:"readOnly"`
	CanExport      bool                   `json:"canExport"`
	Content        string                 `json:"content,omitempty"`
	LoadedHash     string                 `json:"loadedHash,omitempty"`
	SessionID      string                 `json:"sessionId,omitempty"`
	Size           int64                  `json:"size,omitempty"`
	Error          string                 `json:"error,omitempty"`
	ReadOnlyReason string                 `json:"readOnlyReason,omitempty"`
	Messages       []FileCompanionMessage `json:"messages,omitempty"`
	// The mark fields are set when this open unpacked a companion project. A
	// normal open leaves them empty so the reader keeps what it already stored.
	Highlights  json.RawMessage `json:"highlights,omitempty"`
	ChatMarks   json.RawMessage `json:"chatMarks,omitempty"`
	Annotations json.RawMessage `json:"annotations,omitempty"`
	Drawings    json.RawMessage `json:"drawings,omitempty"`
	Bookmarks   json.RawMessage `json:"bookmarks,omitempty"`
	// Imported is set when Path is the document taken out of a project zip.
	Imported bool `json:"imported,omitempty"`
}

// FileCompanionMessage is one restored chat bubble. Roles are user or assistant.
// Reasoning is the model's thinking trail, separate from the answer text.
type FileCompanionMessage struct {
	Role       string `json:"role"`
	Text       string `json:"text"`
	Reasoning  string `json:"reasoning,omitempty"`
	ResultPath string `json:"resultPath,omitempty"`
	// Paper marks the button turn. A person typing the same label is not one.
	Paper bool `json:"paper,omitempty"`
	// Figures are drawings from the open file that this answer refers to.
	Figures []FileCompanionFigure `json:"figures,omitempty"`
	// Files are the files this answer produced, shown as 产出物 cards.
	Files []FileCompanionFile `json:"files,omitempty"`
	// NoteCardPath is the PDF for a video or audio note. The bubble text stays
	// Markdown. ResultPath stays empty so the bubble is not replaced by the
	// paper sentence.
	NoteCardPath string `json:"noteCardPath,omitempty"`
	// VideoNote is the button or closed phrase, not a typed paper turn.
	VideoNote bool `json:"videoNote,omitempty"`
	// WebNote is a web page note. Its timestamps do not seek the preview.
	WebNote bool `json:"webNote,omitempty"`
}

// FileCompanionSaveResult reports a text write. Conflict stops autosave.
type FileCompanionSaveResult struct {
	Saved    bool   `json:"saved"`
	Conflict bool   `json:"conflict"`
	DiskHash string `json:"diskHash,omitempty"`
	Error    string `json:"error,omitempty"`
}

func (a *App) FileCompanionOpen(path string) (FileCompanionDocument, error) {
	return a.fileCompanionOpen(path, true)
}

func (a *App) fileCompanionOpen(path string, acceptProject bool) (FileCompanionDocument, error) {
	doc := FileCompanionDocument{Path: path, Name: filepath.Base(path)}
	if a == nil {
		return doc, errors.New("app unavailable")
	}
	if strings.ContainsRune(path, 0) {
		doc.Error = "path contains NUL"
		return doc, nil
	}
	path = strings.TrimSpace(path)
	doc.Path = path
	doc.Name = filepath.Base(path)
	if path == "" {
		doc.Error = "empty path"
		return doc, nil
	}
	// The tab identity is the canonical path. A second spelling of the same
	// file, including Windows case, must not open another tab. The display
	// path is that same walk, before case folding, so the tab keeps the
	// on-disk name.
	identity, display, canonErr := fileCompanionPathIdentity(path)
	if canonErr == nil {
		doc.Path = identity
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			doc.Error = "file not found"
			return doc, nil
		}
		doc.Error = "file is not readable"
		return doc, nil
	}
	if info.IsDir() {
		doc.Error = "directories are not opened as tabs"
		return doc, nil
	}
	if canonErr != nil {
		doc.Error = "path is not readable"
		return doc, nil
	}
	if base := filepath.Base(display); base != "" && base != "." && base != ".." {
		doc.Name = base
	}
	canonical := identity
	if acceptProject && strings.EqualFold(filepath.Ext(canonical), ".zip") {
		return a.fileCompanionOpenProjectZip(display)
	}
	fileLock := a.fileCompanionFileLock(canonical)
	fileLock.Lock()
	unlocked := false
	unlockFile := func() {
		if !unlocked {
			unlocked = true
			fileLock.Unlock()
		}
	}
	defer unlockFile()
	grant := &fileCompanionGrant{
		UserPath:       path,
		Canonical:      canonical,
		CanExport:      true,
		SelectionStart: -1,
	}
	file, err := os.Open(path)
	if err != nil {
		doc.Error = "file is not readable"
		doc.CanExport = false
		return doc, nil
	}
	// Preview kinds are never saved from this buffer. Skip the body, the hash,
	// and the writable probe so a PDF or image is not read up to the text cap.
	if !fileCompanionTextExtension(path) {
		_ = file.Close()
		grant.ReadOnly = true
		grant.LoadedSize = info.Size()
		grant.LoadedMod = info.ModTime().UnixNano()
		doc.ReadOnly = true
		doc.CanExport = true
		doc.SessionID = fileCompanionSessionID(canonical)
		doc.Size = info.Size()
		st := a.companionRuntime()
		st.mu.Lock()
		st.grants[canonical] = grant
		st.mu.Unlock()
		sid := doc.SessionID
		unlockFile()
		doc.Messages = a.fileCompanionHistory(sid)
		a.rememberFileCompanionDocument(doc)
		return doc, nil
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, int64(codingWorkbenchBrowserMaxReadBytes)+1))
	if err != nil {
		doc.Error = "file is not readable"
		doc.CanExport = false
		return doc, nil
	}
	// The read cap can split a rune. tooBig stays tied to the raw read so
	// trimming that partial rune cannot make a capped file editable.
	capped := len(payload) > codingWorkbenchBrowserMaxReadBytes
	payload = fileCompanionTrimPartialRune(payload, capped)
	textKind := fileCompanionTextEditable(path, payload)
	tooBig := capped || utf8.RuneCount(payload) > codingWorkbenchBrowserMaxRunes
	writable := fileCompanionWritable(path)
	if refreshed, statErr := os.Stat(path); statErr == nil {
		info = refreshed
	}
	if textKind {
		if tooBig {
			runes := []rune(string(payload))
			if len(runes) > codingWorkbenchBrowserMaxRunes {
				runes = runes[:codingWorkbenchBrowserMaxRunes]
			}
			doc.Content = string(runes)
		} else {
			doc.Content = string(payload)
		}
	}
	sum := sha256.Sum256(payload)
	grant.Editable = textKind && !tooBig && writable
	grant.ReadOnly = !grant.Editable
	grant.LoadedSize = info.Size()
	grant.LoadedMod = info.ModTime().UnixNano()
	grant.LoadedHash = hex.EncodeToString(sum[:])
	doc.Editable = grant.Editable
	doc.ReadOnly = !doc.Editable
	doc.CanExport = true
	doc.LoadedHash = grant.LoadedHash
	doc.SessionID = fileCompanionSessionID(canonical)
	doc.Size = info.Size()
	if textKind && tooBig {
		doc.ReadOnlyReason = "too-large"
	} else if textKind && !writable {
		doc.ReadOnlyReason = "permission"
	}
	st := a.companionRuntime()
	st.mu.Lock()
	st.grants[canonical] = grant
	st.mu.Unlock()
	sid := doc.SessionID
	unlockFile()
	doc.Messages = a.fileCompanionHistory(sid)
	a.rememberFileCompanionDocument(doc)
	return doc, nil
}

// fileCompanionTrimPartialRune drops an incomplete sequence left at the end of
// a capped read. A short invalid file is unchanged so it is not treated as text.
func fileCompanionTrimPartialRune(payload []byte, capped bool) []byte {
	if utf8.Valid(payload) || !capped || len(payload) == 0 {
		return payload
	}
	for n := 1; n < utf8.UTFMax && n < len(payload); n++ {
		head := payload[:len(payload)-n]
		if utf8.Valid(head) {
			return head
		}
	}
	return payload
}

func fileCompanionTextExtension(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".html", ".htm", ".txt", ".text", ".log", "":
		return true
	default:
		return false
	}
}

func fileCompanionTextEditable(path string, payload []byte) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".md", ".markdown", ".html", ".htm", ".txt", ".text", ".log":
		return utf8.Valid(payload)
	case "":
		return isCodePreviewTextContent(payload)
	default:
		return false
	}
}

func fileCompanionWritable(path string) bool {
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		return false
	}
	_ = file.Close()
	return true
}

func (a *App) fileCompanionGrant(path string) (*fileCompanionGrant, string, error) {
	if strings.ContainsRune(path, 0) {
		return nil, "", errFileCompanionNULPath
	}
	canonical, err := fileCompanionCanonicalPath(path)
	if err != nil {
		return nil, "", err
	}
	st := a.companionRuntime()
	st.mu.Lock()
	grant := st.grants[canonical]
	st.mu.Unlock()
	if grant == nil || grant.Canonical != canonical {
		if note := a.fileCompanionNoteResultGrant(canonical); note != nil {
			return note, canonical, nil
		}
		return nil, canonical, errFileCompanionNotGranted
	}
	return grant, canonical, nil
}

// fileCompanionNoteResultGrant allows the note Markdown and PDF under this
// session's results directory. It does not grant the open media, a subtitle,
// or a path that climbs out of that directory.
func (a *App) fileCompanionNoteResultGrant(canonical string) *fileCompanionGrant {
	if a == nil {
		return nil
	}
	root := filepath.Join(a.GetDataDir(), "file-companion", "results")
	rel, err := filepath.Rel(root, canonical)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
		return nil
	}
	parts := strings.Split(rel, string(os.PathSeparator))
	if len(parts) != 2 {
		return nil
	}
	sessionID, base := parts[0], parts[1]
	if sessionID == "" || strings.Contains(sessionID, "..") || strings.ContainsAny(sessionID, `/\`) {
		return nil
	}
	if !strings.HasSuffix(base, "-笔记.md") && !strings.HasSuffix(base, "-笔记.pdf") {
		return nil
	}
	if !a.fileCompanionSessionKnown(sessionID) {
		return nil
	}
	return &fileCompanionGrant{
		UserPath:  canonical,
		Canonical: canonical,
		ReadOnly:  true,
		CanExport: true,
	}
}

func (a *App) fileCompanionSessionKnown(sessionID string) bool {
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	if st.webSessions != nil && st.webSessions[sessionID] != "" {
		return true
	}
	for path := range st.grants {
		if fileCompanionSessionID(path) == sessionID {
			return true
		}
	}
	return false
}

var errFileCompanionNotGranted = errors.New("path is outside the open file")

func (a *App) FileCompanionSaveText(path, content, loadedHash string) (FileCompanionSaveResult, error) {
	return a.fileCompanionWriteText(path, loadedHash, "", func(string) (string, error) {
		return content, nil
	})
}

func (a *App) FileCompanionAppendText(path, insertion, loadedHash string) (FileCompanionSaveResult, error) {
	return a.fileCompanionWriteText(path, loadedHash, "", func(current string) (string, error) {
		selection, prefer := a.fileCompanionSelection(path)
		return fileCompanionAppendAfterSelection(current, selection, insertion, prefer), nil
	})
}

func (a *App) rememberFileCompanionSelection(path, selection string, start int) {
	grant, _, err := a.fileCompanionGrant(path)
	if err != nil || grant == nil {
		return
	}
	if start < 0 {
		start = -1
	}
	st := a.companionRuntime()
	st.mu.Lock()
	grant.Selection = selection
	grant.SelectionStart = start
	st.mu.Unlock()
}

func (a *App) fileCompanionSelection(path string) (string, int) {
	grant, _, err := a.fileCompanionGrant(path)
	if err != nil || grant == nil {
		return "", -1
	}
	st := a.companionRuntime()
	st.mu.Lock()
	defer st.mu.Unlock()
	start := grant.SelectionStart
	if start < 0 {
		start = -1
	}
	return grant.Selection, start
}

func fileCompanionAppendAfterSelection(content, selection, insertion string, prefer int) string {
	insertion = strings.TrimRight(insertion, "\r\n")
	if strings.TrimSpace(selection) == "" || strings.TrimSpace(content) == "" {
		return fileCompanionWithBlankLine(content, insertion)
	}
	_, end, ok := fileCompanionSelectionSpan(content, selection, prefer)
	if !ok {
		return fileCompanionWithBlankLine(content, insertion)
	}
	nl := fileCompanionBreak(content)
	return content[:end] + nl + nl + insertion + content[end:]
}

func fileCompanionWithBlankLine(content, insertion string) string {
	insertion = strings.TrimRight(insertion, "\r\n")
	if content == "" {
		return insertion
	}
	nl := fileCompanionBreak(content)
	if strings.HasSuffix(content, nl) {
		return content + nl + insertion
	}
	return content + nl + nl + insertion
}

func fileCompanionBreak(content string) string {
	if strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

// fileCompanionSelectionSpan finds selection even when the editor uses LF and
// the file still uses CRLF. prefer is a UTF-16 offset into the LF-normalized
// file; values below zero keep the first match. Returned indexes are into content.
func fileCompanionSelectionSpan(content, selection string, prefer int) (start, end int, ok bool) {
	normContent := strings.ReplaceAll(content, "\r\n", "\n")
	normSelection := strings.ReplaceAll(selection, "\r\n", "\n")
	if prefer < 0 {
		if idx := strings.Index(content, selection); idx >= 0 {
			return idx, idx + len(selection), true
		}
		nidx := strings.Index(normContent, normSelection)
		if nidx < 0 || normSelection == "" {
			return 0, 0, false
		}
		return fileCompanionMapNormalized(content, nidx), fileCompanionMapNormalized(content, nidx+len(normSelection)), true
	}
	if normSelection == "" {
		return 0, 0, false
	}
	nidx, found := fileCompanionPreferredIndex(normContent, normSelection, fileCompanionUTF16ToByte(normContent, prefer))
	if !found {
		return 0, 0, false
	}
	return fileCompanionMapNormalized(content, nidx), fileCompanionMapNormalized(content, nidx+len(normSelection)), true
}

// fileCompanionUTF16ToByte maps a JavaScript selection offset onto a Go string.
func fileCompanionUTF16ToByte(s string, units int) int {
	if units <= 0 {
		return 0
	}
	seen := 0
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		width := 1
		if r > 0xFFFF {
			width = 2
		}
		if seen+width > units {
			return i
		}
		seen += width
		i += size
		if seen == units {
			return i
		}
	}
	return len(s)
}

func fileCompanionPreferredIndex(normContent, normSelection string, bytePrefer int) (int, bool) {
	if bytePrefer+len(normSelection) <= len(normContent) && normContent[bytePrefer:bytePrefer+len(normSelection)] == normSelection {
		return bytePrefer, true
	}
	best := -1
	bestDist := 0
	from := 0
	limit := len(normContent) - len(normSelection)
	for from <= limit {
		rel := strings.Index(normContent[from:], normSelection)
		if rel < 0 {
			break
		}
		abs := from + rel
		dist := abs - bytePrefer
		if dist < 0 {
			dist = -dist
		}
		if best < 0 || dist < bestDist {
			best = abs
			bestDist = dist
		}
		if abs >= bytePrefer {
			break
		}
		from = abs + 1
	}
	if best < 0 {
		return 0, false
	}
	return best, true
}

func fileCompanionMapNormalized(content string, normOffset int) int {
	norm := 0
	for i := 0; i < len(content); {
		if norm == normOffset {
			return i
		}
		if i+1 < len(content) && content[i] == '\r' && content[i+1] == '\n' {
			i += 2
			norm++
			continue
		}
		i++
		norm++
	}
	return len(content)
}

func (a *App) fileCompanionWriteText(path, loadedHash, requestID string, mutate func(current string) (string, error)) (FileCompanionSaveResult, error) {
	var zero FileCompanionSaveResult
	if a == nil {
		return zero, errors.New("app unavailable")
	}
	grant, canonical, err := a.fileCompanionGrant(path)
	if err != nil {
		return FileCompanionSaveResult{Error: "path is outside the open file"}, err
	}
	fileLock := a.fileCompanionFileLock(canonical)
	fileLock.Lock()
	defer fileLock.Unlock()
	grant, canonical, err = a.fileCompanionGrant(path)
	if err != nil {
		return FileCompanionSaveResult{Error: "path is outside the open file"}, err
	}
	if !grant.Editable || grant.ReadOnly {
		return FileCompanionSaveResult{Error: "file is read only"}, errors.New("file is read only")
	}
	resolved, err := fileCompanionCanonicalPath(grant.UserPath)
	if err != nil || resolved != canonical || resolved != grant.Canonical {
		return FileCompanionSaveResult{Conflict: true, Error: "file changed"}, nil
	}
	info, err := os.Stat(grant.UserPath)
	if err != nil {
		return FileCompanionSaveResult{Error: "file is not writable"}, err
	}
	if info.IsDir() {
		return FileCompanionSaveResult{Error: "path is outside the open file"}, errFileCompanionNotGranted
	}
	diskChanged := info.Size() != grant.LoadedSize || info.ModTime().UnixNano() != grant.LoadedMod
	var currentBytes []byte
	diskHash := grant.LoadedHash
	if diskChanged {
		payload, readErr := os.ReadFile(grant.UserPath)
		if readErr != nil {
			return FileCompanionSaveResult{Error: "file is not readable"}, readErr
		}
		currentBytes = payload
		sum := sha256.Sum256(payload)
		diskHash = hex.EncodeToString(sum[:])
		// Reject only when the file itself differs from the grant. A client
		// hash left behind by an overlapping autosave is not an external edit.
		if diskHash != grant.LoadedHash && loadedHash != diskHash {
			return FileCompanionSaveResult{Conflict: true, DiskHash: diskHash, Error: "file changed"}, nil
		}
	}
	if currentBytes == nil {
		var readErr error
		currentBytes, readErr = os.ReadFile(grant.UserPath)
		if readErr != nil {
			return FileCompanionSaveResult{Error: "file is not readable"}, readErr
		}
	}
	current := string(currentBytes)
	next, err := mutate(current)
	if err != nil {
		return FileCompanionSaveResult{Error: err.Error()}, err
	}
	if next == current {
		sum := sha256.Sum256(currentBytes)
		hash := hex.EncodeToString(sum[:])
		st := a.companionRuntime()
		st.mu.Lock()
		grant.LoadedHash = hash
		grant.LoadedSize = info.Size()
		grant.LoadedMod = info.ModTime().UnixNano()
		st.mu.Unlock()
		return FileCompanionSaveResult{Saved: true, DiskHash: hash}, nil
	}
	writeNext := func() error {
		file, openErr := os.OpenFile(grant.UserPath, os.O_WRONLY|os.O_TRUNC, 0)
		if openErr != nil {
			return openErr
		}
		written, writeErr := file.Write([]byte(next))
		if writeErr != nil || written != len(next) {
			_ = file.Close()
			if writeErr == nil {
				writeErr = io.ErrShortWrite
			}
			return writeErr
		}
		return file.Close()
	}
	// A model append shares chatMu with the progress-button cancel. A reader
	// save has an empty request id and writes immediately.
	if strings.TrimSpace(requestID) != "" {
		if pubErr := a.fileCompanionPublishTurn(requestID, writeNext); pubErr != nil {
			if errors.Is(pubErr, errFileCompanionTurnCancelled) {
				return zero, pubErr
			}
			return FileCompanionSaveResult{Error: "file is not writable"}, pubErr
		}
	} else if writeErr := writeNext(); writeErr != nil {
		return FileCompanionSaveResult{Error: "file is not writable"}, writeErr
	}
	sum := sha256.Sum256([]byte(next))
	hash := hex.EncodeToString(sum[:])
	st := a.companionRuntime()
	st.mu.Lock()
	grant.LoadedHash = hash
	if updated, statErr := os.Stat(grant.UserPath); statErr == nil {
		grant.LoadedSize = updated.Size()
		grant.LoadedMod = updated.ModTime().UnixNano()
	}
	st.mu.Unlock()
	return FileCompanionSaveResult{Saved: true, DiskHash: hash}, nil
}

func (a *App) fileCompanionGranted(path string) bool {
	_, _, err := a.fileCompanionGrant(path)
	return err == nil
}
