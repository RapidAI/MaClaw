package guiapp

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/agent"
)

// fileCompanionOpenSet is the companion's document session. It is the list of
// files the window had open, active file first. It is not the process argv:
// the open-file verb never belongs here.
type fileCompanionOpenSet struct {
	Paths []string `json:"paths"`
}

func fileCompanionOpenSetPath(a *App) string {
	if a == nil {
		return ""
	}
	return filepath.Join(a.GetDataDir(), "file-companion", "open-set.json")
}

// fileCompanionExistingFiles keeps documents. A file that is confirmed gone,
// and a directory, are not documents.
// A stat that fails for another reason stays: a volume that is not ready
// should be tried again, not forgotten.
func fileCompanionExistingFiles(paths []string) []string {
	out := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		kept, ok := fileCompanionDocumentPath(path)
		if !ok {
			continue
		}
		key := fileCompanionDedupeKey(kept)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, kept)
	}
	return out
}

func fileCompanionDocumentPath(path string) (string, bool) {
	path = strings.TrimSpace(path)
	if path == "" || strings.ContainsRune(path, 0) {
		return "", false
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false
		}
		return path, true
	}
	if info.IsDir() {
		return "", false
	}
	return path, true
}

func (a *App) loadFileCompanionOpenSet() (fileCompanionOpenSet, bool) {
	path := fileCompanionOpenSetPath(a)
	if path == "" {
		return fileCompanionOpenSet{}, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return fileCompanionOpenSet{}, false
	}
	var set fileCompanionOpenSet
	if err := json.Unmarshal(raw, &set); err != nil {
		return fileCompanionOpenSet{}, false
	}
	return set, true
}

func (a *App) writeFileCompanionOpenSet(paths []string) error {
	path := fileCompanionOpenSetPath(a)
	if path == "" {
		return os.ErrInvalid
	}
	if paths == nil {
		paths = []string{}
	}
	body, err := json.Marshal(fileCompanionOpenSet{Paths: paths})
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, body) {
		return nil
	}
	return writeReplacingFile(path, body)
}

// writeReplacingFile replaces path with data. The previous file stays in place
// until the new one is ready: a missing open set means "migrate every
// transcript", so deleting it first would resurrect tabs the user closed.
func writeReplacingFile(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".open-set-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	if err := renameReplacing(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// SetFileCompanionOpenSet stores the window's current tabs. The active file
// is first. The window does not send error tabs. An empty list is stored:
// the user closed the last file, and the next open must ask instead of
// resurrecting an old transcript.
func (a *App) SetFileCompanionOpenSet(paths []string) error {
	if a == nil {
		return os.ErrInvalid
	}
	kept := make([]string, 0, len(paths))
	seen := map[string]bool{}
	for _, path := range paths {
		path = strings.TrimSpace(path)
		if path == "" || strings.ContainsRune(path, 0) {
			continue
		}
		key := fileCompanionDedupeKey(path)
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		kept = append(kept, path)
	}
	return a.writeFileCompanionOpenSet(kept)
}

// FileCompanionResumePaths is what a companion with no document arguments opens.
// A saved open set is authoritative, including an empty one. Sessions are read
// only when that file has never been written, so closing the last tab is not
// undone by an old transcript.
func (a *App) FileCompanionResumePaths() []string {
	return a.fileCompanionResumePaths()
}

func (a *App) fileCompanionResumePaths() []string {
	if a == nil {
		return nil
	}
	if set, ok := a.loadFileCompanionOpenSet(); ok {
		// Reading does not rewrite. The window is the writer after this, and
		// the main process also reads on the icon click while that window is
		// saving the tabs the user still has open.
		return fileCompanionExistingFiles(set.Paths)
	}
	migrated, ok := a.fileCompanionPathsFromSessions()
	if ok {
		_ = a.writeFileCompanionOpenSet(migrated)
	}
	return migrated
}

func (a *App) fileCompanionPathsFromSessions() ([]string, bool) {
	dir := filepath.Join(a.GetDataDir(), "file-companion", "sessions")
	entries, err := os.ReadDir(dir)
	if err != nil {
		// A missing directory is an empty history. Any other error leaves the
		// set unwritten so the next click can try again.
		return nil, os.IsNotExist(err)
	}
	type found struct {
		path string
		rank int64
	}
	var founds []found
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		path := pathFromCompanionSessionFile(filepath.Join(dir, entry.Name()))
		if path == "" {
			continue
		}
		founds = append(founds, found{path: path, rank: info.ModTime().UnixNano()})
	}
	sort.SliceStable(founds, func(i, j int) bool {
		return founds[i].rank > founds[j].rank
	})
	paths := make([]string, 0, len(founds))
	for _, item := range founds {
		paths = append(paths, item.path)
	}
	return fileCompanionExistingFiles(paths), true
}

// pathFromCompanionSessionFile reads the document path recorded in a companion
// transcript. The path is on the first marked line, so the chat body is not
// decoded. JSON escapes on that line are decoded with encoding/json.
func pathFromCompanionSessionFile(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	return pathFromCompanionSessionReader(f)
}

// pathFromCompanionSessionReader reads until it has a finished path line.
// A short read is not the end of the file unless it reports EOF: stopping
// there would miss a path that sits just past the bytes already in hand.
// No marker in the bytes so far is not final either; the marker can sit
// later in the file.
func pathFromCompanionSessionReader(r io.Reader) string {
	marker := []byte(agent.FilePathPromptPrefix)
	buf := make([]byte, 0, 64*1024)
	tmp := make([]byte, 64*1024)
	for {
		n, readErr := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			got, done := pathFromCompanionSessionBytes(buf)
			if got != "" {
				return got
			}
			// A finished scan has no path in these bytes. Keep only a tail so
			// a marker split across reads is still visible, and drop the chat
			// body already ruled out. An unfinished line stays intact.
			if done {
				keep := len(marker) - 1
				if len(buf) > keep {
					buf = append([]byte(nil), buf[len(buf)-keep:]...)
				}
			} else if i := bytes.Index(buf, marker); i > 0 {
				buf = append([]byte(nil), buf[i:]...)
			}
		}
		if readErr == io.EOF {
			return ""
		}
		if readErr != nil || n == 0 {
			return ""
		}
	}
}

func pathFromCompanionSessionBytes(raw []byte) (string, bool) {
	marker := []byte(agent.FilePathPromptPrefix)
	for len(raw) > 0 {
		i := bytes.Index(raw, marker)
		if i < 0 {
			return "", true
		}
		raw = raw[i+len(marker):]
		line, ended := jsonStringLine(raw)
		if !ended {
			return "", false
		}
		if line == "" {
			continue
		}
		paths := agent.SelectedLocalFilePathsFromPrompt(agent.FilePathPromptPrefix + "\n" + line)
		if len(paths) > 0 {
			return paths[0], true
		}
	}
	return "", true
}

// jsonStringLine decodes the next JSON string line. The bool is false when
// the line runs off the end of raw: a short read must not turn that fragment
// into a path. Leading newline escapes are skipped. \u escapes are left for
// encoding/json.
func jsonStringLine(raw []byte) (string, bool) {
	var b bytes.Buffer
	b.WriteByte('"')
	ended := false
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if c == '\\' && i+1 < len(raw) {
			esc := raw[i+1]
			if esc == 'n' || esc == 'r' {
				if b.Len() > 1 {
					ended = true
					break
				}
				i++
				continue
			}
			b.WriteByte('\\')
			b.WriteByte(esc)
			i++
			if esc == 'u' {
				for k := 0; k < 4 && i+1 < len(raw); k++ {
					i++
					b.WriteByte(raw[i])
				}
			}
			continue
		}
		if c == '"' || c == '\n' || c == '\r' {
			ended = true
			break
		}
		b.WriteByte(c)
	}
	if !ended || b.Len() == 1 {
		return "", false
	}
	b.WriteByte('"')
	var line string
	if err := json.Unmarshal(b.Bytes(), &line); err != nil {
		return "", true
	}
	return strings.TrimSpace(line), true
}
