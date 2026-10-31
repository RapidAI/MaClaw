package tool

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/RapidAI/CodeClaw/corelib/maclawpath"
)

type WriteMode string

const (
	WriteModeOverwrite WriteMode = "overwrite"
	WriteModeAppend    WriteMode = "append"
)

const DefaultWriteMode = string(WriteModeOverwrite)

// TextFileMutated runs after a text file is written or edited successfully.
// Hosts use it to refresh indexes. It must return quickly.
var TextFileMutated func(path string)

func noteTextFileMutated(path string) {
	if TextFileMutated == nil || path == "" {
		return
	}
	TextFileMutated(path)
}

// textFileWriteLocks serializes read-modify-write of one path. Different
// paths do not share a mutex, so two tasks can edit two files at once.
// Callers of WriteTextFile, EditTextFile, EditFileByLine, and RewriteTextFile
// take this lock before they read and release it after the write. A shell
// command that rewrites the same path does not take this lock. The key is the
// file the write will replace: a Windows directory junction is followed to
// its target, an extended-length prefix is folded onto that same path, and
// an existing final symlink is followed to its target. A trailing dot or
// space stays on the extended spelling, and junctions and 8.3 names after
// that component still resolve.
var textFileWriteLocks sync.Map

// textFileMutationHook runs after the per-path lock is held and before the
// read or write. Tests use it to prove the critical section sees the latest
// bytes. Production leaves it empty.
var textFileMutationHook atomic.Value // func(string)

func textFileWriteLock(path string) *sync.Mutex {
	key := textFileWriteLockKey(path)
	if key == "" {
		key = "\x00"
	}
	actual, _ := textFileWriteLocks.LoadOrStore(key, &sync.Mutex{})
	return actual.(*sync.Mutex)
}

func textFileWriteLockKey(path string) string {
	// Trim the left side only. A trailing space on \\?\C:\dir\file is part of
	// the file name; TrimSpace would delete it and fold the key onto file.
	path = strings.TrimLeft(path, " \t")
	if runtime.GOOS == "windows" {
		if spelled, ok := spellWindowsExtendedLockPath(path); ok {
			// CreateFile rejects a forward slash after \\?\. Rewriting it onto
			// the backslash file would lock a file the write never opens.
			if extendedSlashMissesDistinctFile(path, spelled) {
				return strings.ToLower(path)
			}
			// CreateFile rejects ".", "..", and empty components in an
			// extended path. Clean would lock the parent instead.
			if extendedComponentsRejected(spelled) {
				return strings.ToLower(spelled)
			}
			if kept := windowsExtendedDistinctLockPath(spelled); kept != "" {
				return strings.ToLower(resolveExtendedDistinctLockPath(kept))
			}
			if _, stripped := stripWindowsExtendedLockPrefix(spelled); stripped {
				path = spelled
			}
		}
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	cleaned := filepath.Clean(path)
	if abs, err := filepath.Abs(cleaned); err == nil {
		cleaned = abs
	}
	// Fold the extended prefix before the walk. Lstat and Readlink accept
	// \\?\ , but EvalSymlinks keeps that prefix, so the drive-letter path and
	// the extended path would otherwise stay on two locks.
	cleaned = foldWindowsLockPath(cleaned)
	cleaned = filepath.Clean(resolveTextFileLockPath(cleaned))
	cleaned = foldWindowsLockPath(cleaned)
	if runtime.GOOS == "windows" {
		return strings.ToLower(cleaned)
	}
	return cleaned
}

// spellWindowsExtendedLockPath rewrites //?/ and mixed slashes into a \\?\
// path. It keeps ".", "..", empty components, and trailing dots and spaces
// so the caller can classify them. Clean would name a different file.
func spellWindowsExtendedLockPath(path string) (string, bool) {
	if !hasAnyWindowsExtendedPrefix(path) {
		return "", false
	}
	if !strings.Contains(path, "/") {
		return path, true
	}
	return strings.ReplaceAll(path, "/", `\`), true
}

func hasAnyWindowsExtendedPrefix(path string) bool {
	if len(path) < 4 {
		return false
	}
	if (path[0] != '\\' && path[0] != '/') || (path[1] != '\\' && path[1] != '/') {
		return false
	}
	if path[2] != '?' && path[2] != '.' {
		return false
	}
	return path[3] == '\\' || path[3] == '/'
}

// extendedSlashMissesDistinctFile reports a \\?\ path whose Win32 spelling
// would name a different file, written with a forward slash. CreateFile
// rejects that slash, so the key must not become the backslash file.
func extendedSlashMissesDistinctFile(original, spelled string) bool {
	if !strings.Contains(original, "/") {
		return false
	}
	return windowsLockPathNeedsExtendedForm(spelled)
}

// extendedComponentsRejected reports ".", "..", or an empty component.
// CreateFile on \\?\ returns ERROR_INVALID_NAME for those. A lexical Clean
// would take the lock of the parent file instead.
func extendedComponentsRejected(spelled string) bool {
	rest := spelled
	if hasWindowsExtendedLockPrefix(spelled) {
		rest = spelled[4:]
	}
	rest = strings.TrimRight(rest, `\`)
	if rest == "" {
		return false
	}
	for _, part := range strings.Split(rest, `\`) {
		if part == "" || part == "." || part == ".." {
			return true
		}
	}
	return false
}

// windowsExtendedDistinctLockPath returns the extended path when its Win32
// spelling would name a different file. An empty result means the caller can
// fold the prefix away. The path must already be slash-normalized and free
// of components CreateFile rejects.
func windowsExtendedDistinctLockPath(path string) string {
	if runtime.GOOS != "windows" {
		return ""
	}
	folded, ok := stripWindowsExtendedLockPrefix(path)
	if !ok || !windowsLockPathNeedsExtendedForm(folded) {
		return ""
	}
	return path
}

// resolveExtendedDistinctLockPath follows junctions and 8.3 names on an
// extended path whose Win32 spelling is a different file. EvalSymlinks keeps
// a trailing dot or space when the path still has \\?\, and strips it on the
// Win32 spelling, so that prefix is put back before each step. A junction
// that leaves the extended file lands on the Win32 key of the target.
func resolveExtendedDistinctLockPath(path string) string {
	const maxHops = 32
	current := reprefixExtendedLockPath(path)
	for hop := 0; hop < maxHops; hop++ {
		stepped := false
		if linkDir := nearestTextFileReparseDir(filepath.Dir(current)); linkDir != "" {
			if next, ok := retargetTextFileThroughReparse(current, linkDir); ok {
				next = reprefixExtendedLockPath(next)
				if !textFileLockPathEqual(next, current) {
					current = next
					stepped = true
				}
			}
		}
		if canonical := canonicalizeTextFileLockPath(current); canonical != "" {
			canonical = reprefixExtendedLockPath(canonical)
			if !textFileLockPathEqual(canonical, current) {
				current = canonical
				stepped = true
			}
		}
		if followed := followTextFileFinalSymlink(current); followed != "" {
			followed = reprefixExtendedLockPath(followed)
			if !textFileLockPathEqual(followed, current) {
				current = followed
				stepped = true
			}
		}
		if !stepped {
			break
		}
	}
	return finishExtendedLockKey(current)
}

// reprefixExtendedLockPath puts \\?\ back when a trailing dot or space is
// still in the path, and drops it when the path now names the Win32 file.
func reprefixExtendedLockPath(path string) string {
	if path == "" {
		return path
	}
	if hasWindowsExtendedLockPrefix(path) {
		folded, ok := stripWindowsExtendedLockPrefix(path)
		if ok && !windowsLockPathNeedsExtendedForm(folded) {
			return folded
		}
		return path
	}
	if windowsLockPathNeedsExtendedForm(path) {
		return withWindowsExtendedLockPrefix(path)
	}
	return path
}

func finishExtendedLockKey(path string) string {
	path = reprefixExtendedLockPath(path)
	if hasWindowsExtendedLockPrefix(path) {
		folded, ok := stripWindowsExtendedLockPrefix(path)
		if !ok || windowsLockPathNeedsExtendedForm(folded) {
			return path
		}
		path = folded
	}
	if windowsLockPathNeedsExtendedForm(path) {
		return withWindowsExtendedLockPrefix(path)
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	path = foldWindowsLockPath(path)
	path = filepath.Clean(resolveTextFileLockPath(path))
	path = foldWindowsLockPath(path)
	return path
}

func withWindowsExtendedLockPrefix(path string) string {
	if path == "" || hasWindowsExtendedLockPrefix(path) {
		return path
	}
	if strings.HasPrefix(path, `\\`) {
		return `\\?\UNC\` + strings.TrimPrefix(path, `\\`)
	}
	return `\\?\` + path
}

// foldWindowsLockPath makes a Windows lock key name the file CreateFile opens.
// \\?\C:\ and \\?\UNC\ fold onto the Win32 path. A component that ends in a
// dot or a space does not: the extended path keeps that character and the
// Win32 path strips it, so those are different files. The Win32 spelling then
// drops trailing dots and spaces, matching the file WriteFile replaces.
func foldWindowsLockPath(path string) string {
	if runtime.GOOS != "windows" || path == "" {
		return path
	}
	if folded, ok := stripWindowsExtendedLockPrefix(path); ok {
		if windowsLockPathNeedsExtendedForm(folded) {
			return filepath.Clean(path)
		}
		path = folded
	}
	if hasWindowsExtendedLockPrefix(path) {
		return filepath.Clean(path)
	}
	return stripWindowsTrailingDotSpace(path)
}

func hasWindowsExtendedLockPrefix(path string) bool {
	return len(path) >= 4 && path[0] == '\\' && path[1] == '\\' && (path[2] == '?' || path[2] == '.') && path[3] == '\\'
}

func stripWindowsExtendedLockPrefix(path string) (string, bool) {
	if !hasWindowsExtendedLockPrefix(path) {
		return path, false
	}
	rest := path[4:]
	if len(rest) >= 4 && strings.EqualFold(rest[:4], `UNC\`) {
		return `\\` + rest[4:], true
	}
	if len(rest) >= 7 && strings.EqualFold(rest[:7], `Volume{`) {
		return path, false
	}
	if len(rest) >= 2 && rest[1] == ':' {
		return rest, true
	}
	return path, false
}

func windowsLockPathNeedsExtendedForm(path string) bool {
	for _, part := range strings.Split(path, `\`) {
		if part == "" || part == "." || part == ".." || strings.Contains(part, ":") {
			continue
		}
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return true
		}
	}
	return false
}

func stripWindowsTrailingDotSpace(path string) string {
	parts := strings.Split(path, `\`)
	changed := false
	for i, part := range parts {
		if part == "" || part == "." || part == ".." || strings.Contains(part, ":") {
			continue
		}
		trimmed := strings.TrimRight(part, ". ")
		if trimmed == "" || trimmed == part {
			continue
		}
		parts[i] = trimmed
		changed = true
	}
	if !changed {
		return path
	}
	return filepath.Clean(strings.Join(parts, `\`))
}

// resolveTextFileLockPath returns the path whose bytes a write replaces.
// A Windows directory junction is ModeIrregular. EvalSymlinks leaves that
// path in place, or fails even after the directory exists, while WriteFile
// follows the junction onto the target. Readlink is the hop that makes the
// junction name and the target name one lock. The hop still works when
// directories under the junction do not exist yet, so creating them does not
// change the key. After those hops, existing components are canonicalized so
// an 8.3 short name and the long name share the lock. Components that do not
// exist yet keep the caller's spelling, so creating them does not change the
// key. A final symlink is followed only when that link already exists, and
// its target is resolved again. The lock is not re-entrant and this walk
// must not write the file.
func resolveTextFileLockPath(path string) string {
	const maxHops = 32
	current := filepath.Clean(path)
	for hop := 0; hop < maxHops; hop++ {
		stepped := false
		if linkDir := nearestTextFileReparseDir(filepath.Dir(current)); linkDir != "" {
			if next, ok := retargetTextFileThroughReparse(current, linkDir); ok && !textFileLockPathEqual(next, current) {
				current = next
				stepped = true
			}
		}
		if canonical := canonicalizeTextFileLockPath(current); !textFileLockPathEqual(canonical, current) {
			current = canonical
			stepped = true
		}
		if followed := followTextFileFinalSymlink(current); !textFileLockPathEqual(followed, current) {
			current = followed
			stepped = true
		}
		if !stepped {
			return current
		}
	}
	return current
}

// canonicalizeTextFileLockPath expands existing 8.3 names to the long path.
// EvalSymlinks fails while a suffix does not exist, so that suffix stays as
// written and the key is the same before and after the file is created.
func canonicalizeTextFileLockPath(path string) string {
	var missing []string
	current := path
	for {
		if resolved, err := filepath.EvalSymlinks(current); err == nil && resolved != "" {
			for i := len(missing) - 1; i >= 0; i-- {
				resolved = filepath.Join(resolved, missing[i])
			}
			return filepath.Clean(resolved)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		missing = append(missing, filepath.Base(current))
		current = parent
	}
}

// nearestTextFileReparseDir finds the closest directory symlink or Windows
// junction at dir or above it. Missing intermediate directories are skipped.
// A normal directory is not a stop: the junction may be one of its parents.
func nearestTextFileReparseDir(dir string) string {
	current := dir
	for {
		info, err := os.Lstat(current)
		if err == nil && textFileDirReparse(current, info) {
			return current
		}
		// A missing directory, or a directory this process cannot stat, is
		// not proof that a junction above it is absent. Stopping here would
		// leave the junction path and the target path on different locks.
		next := filepath.Dir(current)
		if next == current {
			return ""
		}
		current = next
	}
}

// textFileDirReparse reports a directory symlink or a Windows junction.
// Lstat does not set ModeDir for either. A junction is ModeIrregular, and
// Stat follows it to a directory. A cloud placeholder stays a file.
func textFileDirReparse(path string, info os.FileInfo) bool {
	if info == nil || info.IsDir() {
		return false
	}
	if info.Mode()&(os.ModeSymlink|os.ModeIrregular) == 0 {
		return false
	}
	followed, err := os.Stat(path)
	return err == nil && followed.IsDir()
}

func retargetTextFileThroughReparse(path, linkDir string) (string, bool) {
	target, err := os.Readlink(linkDir)
	if err != nil || target == "" {
		resolved, evalErr := filepath.EvalSymlinks(linkDir)
		if evalErr != nil || resolved == "" || textFileLockPathEqual(resolved, linkDir) {
			return "", false
		}
		target = resolved
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(linkDir), target)
	}
	rel, err := filepath.Rel(linkDir, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	resolved := filepath.Clean(filepath.Join(target, rel))
	if resolved == "" || textFileLockPathEqual(resolved, path) {
		return "", false
	}
	return resolved, true
}

func followTextFileFinalSymlink(path string) string {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return path
	}
	if resolved, evalErr := filepath.EvalSymlinks(path); evalErr == nil && resolved != "" {
		return filepath.Clean(resolved)
	}
	target, readErr := os.Readlink(path)
	if readErr != nil || target == "" {
		return path
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(filepath.Dir(path), target)
	}
	return filepath.Clean(target)
}

func textFileLockPathEqual(left, right string) bool {
	if runtime.GOOS == "windows" {
		return strings.EqualFold(filepath.Clean(left), filepath.Clean(right))
	}
	return filepath.Clean(left) == filepath.Clean(right)
}

func callTextFileMutationHook(path string) {
	hook, _ := textFileMutationHook.Load().(func(string))
	if hook != nil {
		hook(path)
	}
}

func withTextFileWriteLock(path string, fn func() error) error {
	mu := textFileWriteLock(path)
	mu.Lock()
	defer mu.Unlock()
	callTextFileMutationHook(path)
	return fn()
}

// RewriteTextFile reads path under the per-path lock and, when fn asks to
// write, replaces the file before the lock is released. fn sees the bytes the
// write will replace. A passage edit that reads first and then calls
// WriteTextFile drops a concurrent edit; this is the locked form of that
// read-modify-write. fn must not call WriteTextFile, EditTextFile,
// EditFileByLine, or RewriteTextFile: the lock is not re-entrant.
func RewriteTextFile(path string, fn func(current string) (next string, write bool, err error)) (int64, error) {
	var size int64
	err := withTextFileWriteLock(path, func() error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		next, write, err := fn(string(data))
		if err != nil || !write {
			return err
		}
		if err := os.WriteFile(path, []byte(next), 0o644); err != nil {
			return fmt.Errorf("写入文件失败 %s: %w", path, err)
		}
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("写入验证失败 %s: %w", path, err)
		}
		size = info.Size()
		noteTextFileMutated(path)
		return nil
	})
	return size, err
}

// WriteModeOptions is the closed set of write modes the file-write capability
// advertises in its invocation schema, derived from the canonical constants so
// a host cannot render one set while NormalizeWriteModeKind enforces another.
// The schema declares mode optional, and the empty reading (overwrite) lives
// in NormalizeWriteModeKind, not in the advertised set.
func WriteModeOptions() []string {
	return []string{string(WriteModeOverwrite), string(WriteModeAppend)}
}

// WriteModeHint renders the canonical vocabulary for a rejection message, so
// a model that ignored the enum still learns the valid names from its first
// failure and can retry inside the turn.
func WriteModeHint() string {
	return "allowed: " + strings.Join(WriteModeOptions(), ", ")
}

type PathResolver func() string

func ResolveFileToolPath(path string, projectDirResolver PathResolver) (string, error) {
	p := strings.TrimSpace(path)
	// Empty path → current Project directory / workspace (not user home).
	if p == "" {
		if projectDirResolver != nil {
			if base := strings.TrimSpace(projectDirResolver()); base != "" {
				return filepath.Clean(base), nil
			}
		}
		return "", fmt.Errorf("缺少 path 参数")
	}
	p = maclawpath.ExpandHomePath(p)
	if filepath.IsAbs(p) {
		return filepath.Clean(p), nil
	}
	if projectDirResolver != nil {
		if base := strings.TrimSpace(projectDirResolver()); base != "" {
			return filepath.Clean(filepath.Join(base, p)), nil
		}
	}
	cwd, err := os.Getwd()
	if err == nil && strings.TrimSpace(cwd) != "" {
		return filepath.Clean(filepath.Join(cwd, p)), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Clean(p), nil
	}
	return filepath.Clean(filepath.Join(home, p)), nil
}

func NormalizeWriteModeKind(mode string) (WriteMode, error) {
	m := strings.ToLower(strings.TrimSpace(mode))
	if m == "" {
		return WriteModeOverwrite, nil
	}
	switch m {
	case string(WriteModeOverwrite):
		return WriteModeOverwrite, nil
	case string(WriteModeAppend):
		return WriteModeAppend, nil
	default:
		return "", fmt.Errorf("不支持的 mode: %s", mode)
	}
}

func NormalizeWriteMode(mode string) (string, error) {
	m, err := NormalizeWriteModeKind(mode)
	if err != nil {
		return "", err
	}
	return string(m), nil
}

func WriteTextFile(path, content, mode string) (int64, error) {
	m, err := NormalizeWriteModeKind(mode)
	if err != nil {
		return 0, err
	}
	var size int64
	err = withTextFileWriteLock(path, func() error {
		dir := filepath.Dir(path)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录失败 %s: %w", dir, err)
		}

		if m == WriteModeAppend {
			f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
			if err != nil {
				return fmt.Errorf("打开文件失败 %s: %w", path, err)
			}
			defer f.Close()
			if _, err := f.WriteString(content); err != nil {
				return fmt.Errorf("写入内容失败: %w", err)
			}
		} else {
			if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
				return fmt.Errorf("写入文件失败 %s: %w", path, err)
			}
		}

		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("写入验证失败 %s: %w", path, err)
		}
		size = info.Size()
		noteTextFileMutated(path)
		return nil
	})
	return size, err
}

type EditTextFileResult struct {
	Count int
	Size  int64
	Path  string
}

func EditTextFile(path, oldString, newString string, replaceAll bool) (*EditTextFileResult, error) {
	if oldString == "" {
		return nil, fmt.Errorf("缺少 old_string 参数")
	}
	var result *EditTextFileResult
	err := withTextFileWriteLock(path, func() error {
		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("读取文件失败 %s: %w", path, err)
		}
		replaced, err := ReplaceTextPassage(string(data), oldString, newString, replaceAll)
		if err != nil {
			if errors.Is(err, ErrTextPassageNotFound) {
				return fmt.Errorf("未找到要替换的内容")
			}
			return err
		}
		if err := os.WriteFile(path, []byte(replaced.Text), 0o644); err != nil {
			return fmt.Errorf("写入文件失败 %s: %w", path, err)
		}

		// Verify write succeeded.
		info, err := os.Stat(path)
		if err != nil {
			return fmt.Errorf("写入验证失败 %s: %w", path, err)
		}
		noteTextFileMutated(path)
		result = &EditTextFileResult{Count: replaced.Applied, Size: info.Size(), Path: path}
		return nil
	})
	return result, err
}

// EditLineResult is the outcome of a line-level edit operation.
type EditLineResult struct {
	Path         string
	LinesChanged int   // number of lines affected
	TotalLines   int   // total lines after edit
	Size         int64 // file size after edit
}

// EditLineOperation specifies what kind of line edit to perform.
type EditLineOperation string

const (
	// EditLineReplace replaces lines [startLine, endLine] with new content.
	EditLineReplace EditLineOperation = "replace"
	// EditLineInsert inserts new content after the specified line (startLine).
	// endLine is ignored.
	EditLineInsert EditLineOperation = "insert"
	// EditLineDelete deletes lines [startLine, endLine].
	EditLineDelete EditLineOperation = "delete"
)

// EditFileByLine performs a line-level edit on a file.
//
// Line numbers are 1-indexed. For replace and delete, both startLine and
// endLine are inclusive: [startLine, endLine].
//
// Operations:
//   - replace: replaces lines startLine..endLine with newContent
//   - insert:  inserts newContent after startLine (0 = insert at beginning)
//   - delete:  removes lines startLine..endLine
//
// This is more precise than EditTextFile (substring match) because it uses
// line numbers for addressing — no ambiguity from duplicate content.
func EditFileByLine(path string, op EditLineOperation, startLine, endLine int, newContent string) (*EditLineResult, error) {
	var result *EditLineResult
	err := withTextFileWriteLock(path, func() error {
		edited, lockErr := editFileByLineLocked(path, op, startLine, endLine, newContent)
		result = edited
		return lockErr
	})
	return result, err
}

func editFileByLineLocked(path string, op EditLineOperation, startLine, endLine int, newContent string) (*EditLineResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败 %s: %w", path, err)
	}

	// Detect and preserve the file's line ending style.
	// This prevents mixing \r\n and \n after editing.
	text := string(data)
	lineEnding := "\n"
	if strings.Contains(text, "\r\n") {
		lineEnding = "\r\n"
		text = strings.ReplaceAll(text, "\r\n", "\n")
	}

	lines := strings.Split(text, "\n")
	totalLines := len(lines)

	// Validate line numbers.
	switch op {
	case EditLineInsert:
		if startLine < 0 || startLine > totalLines {
			return nil, fmt.Errorf("行号越界: start_line=%d（文件共 %d 行，insert 允许 0..%d）", startLine, totalLines, totalLines)
		}
	case EditLineReplace, EditLineDelete:
		if startLine < 1 || startLine > totalLines {
			return nil, fmt.Errorf("行号越界: start_line=%d（文件共 %d 行）", startLine, totalLines)
		}
		if endLine < startLine {
			return nil, fmt.Errorf("end_line=%d 不能小于 start_line=%d", endLine, startLine)
		}
		if endLine > totalLines {
			return nil, fmt.Errorf("行号越界: end_line=%d（文件共 %d 行）", endLine, totalLines)
		}
	default:
		return nil, fmt.Errorf("未知操作: %s（支持 replace/insert/delete）", op)
	}

	var result []string
	var linesChanged int

	switch op {
	case EditLineReplace:
		// Replace lines [startLine, endLine] with newContent.
		// Normalize newContent line endings to match internal \n representation.
		normalizedContent := strings.ReplaceAll(newContent, "\r\n", "\n")
		newLines := strings.Split(normalizedContent, "\n")
		result = append(result, lines[:startLine-1]...)
		result = append(result, newLines...)
		result = append(result, lines[endLine:]...)
		linesChanged = endLine - startLine + 1

	case EditLineInsert:
		// Insert newContent after startLine. startLine=0 means insert at top.
		normalizedContent := strings.ReplaceAll(newContent, "\r\n", "\n")
		newLines := strings.Split(normalizedContent, "\n")
		result = append(result, lines[:startLine]...)
		result = append(result, newLines...)
		result = append(result, lines[startLine:]...)
		linesChanged = len(newLines)

	case EditLineDelete:
		// Delete lines [startLine, endLine].
		result = append(result, lines[:startLine-1]...)
		result = append(result, lines[endLine:]...)
		linesChanged = endLine - startLine + 1
	}

	output := strings.Join(result, lineEnding)
	if err := os.WriteFile(path, []byte(output), 0o644); err != nil {
		return nil, fmt.Errorf("写入文件失败 %s: %w", path, err)
	}

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("写入验证失败 %s: %w", path, err)
	}
	noteTextFileMutated(path)

	return &EditLineResult{
		Path:         path,
		LinesChanged: linesChanged,
		TotalLines:   len(result),
		Size:         info.Size(),
	}, nil
}
