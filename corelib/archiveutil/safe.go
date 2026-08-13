package archiveutil

import (
	"crypto/sha256"
	"encoding/binary"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

type counters struct {
	files, dirs   int
	entries       int
	written       int64
	limits        Limits
	allowSymlinks bool
	filter        EntryFilter
	root          string
	inputBytes    int64
	warnings      *[]string
}

// reserveEntry accounts for every materialized archive object, including a
// directory. Limiting only regular files leaves a cheap inode-exhaustion path
// through a ZIP/TAR containing millions of empty directories.
func (c *counters) reserveEntry() error {
	c.entries++
	if c.entries > c.limits.MaxFiles {
		return errorf(CodeLimitExceeded, "archive exceeds file count limit")
	}
	return nil
}

type sourceState struct {
	size    int64
	modTime int64
}

func captureSourceState(path string) (sourceState, error) {
	info, err := os.Stat(path)
	if err != nil {
		return sourceState{}, errorf(CodeIO, "stat archive source: %v", err)
	}
	if info.IsDir() {
		return sourceState{}, errorf(CodeInvalidArgument, "archive_path is a directory")
	}
	return sourceState{size: info.Size(), modTime: info.ModTime().UnixNano()}, nil
}

func ensureSourceUnchanged(path string, before sourceState) error {
	after, err := captureSourceState(path)
	if err != nil {
		return err
	}
	if after != before {
		return errorf(CodeSourceChanged, "archive source changed while it was being processed")
	}
	return nil
}

// entrySet rejects both direct duplicates and file/directory shape conflicts
// (for example an entry "a" followed by "a/b.txt"). Such archives otherwise
// have order-dependent extraction behaviour.
type entrySet struct {
	files map[string]bool
	dirs  map[string]bool
}

func newEntrySet() *entrySet { return &entrySet{files: map[string]bool{}, dirs: map[string]bool{}} }

func (s *entrySet) add(entry string, isDir bool) error {
	// Archive paths are compared case-insensitively so an archive cannot have
	// platform-dependent overwrite behaviour on Windows/macOS volumes.
	key := strings.ToLower(entry)
	if s.files[key] || s.dirs[key] {
		return errorf(CodeUnsafeEntry, "duplicate archive entry: %s", entry)
	}
	for parent := path.Dir(key); parent != "."; parent = path.Dir(parent) {
		if s.files[parent] {
			return errorf(CodeUnsafeEntry, "archive entry has a file parent: %s", entry)
		}
	}
	if !isDir {
		prefix := key + "/"
		for existing := range s.files {
			if strings.HasPrefix(existing, prefix) {
				return errorf(CodeUnsafeEntry, "archive file conflicts with child entry: %s", entry)
			}
		}
		for existing := range s.dirs {
			if strings.HasPrefix(existing, prefix) {
				return errorf(CodeUnsafeEntry, "archive file conflicts with child directory: %s", entry)
			}
		}
	}
	if isDir {
		s.dirs[key] = true
	} else {
		s.files[key] = true
	}
	return nil
}

func canonicalEntry(name string, limits Limits) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	if name == "" || strings.IndexByte(name, 0) >= 0 || len(name) > limits.MaxEntryNameBytes {
		return "", errorf(CodeUnsafeEntry, "invalid archive entry path")
	}
	if strings.HasPrefix(name, "/") || strings.HasPrefix(name, "//") || (len(name) >= 2 && name[1] == ':') {
		return "", errorf(CodeUnsafeEntry, "archive entry is absolute: %q", name)
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", errorf(CodeUnsafeEntry, "archive entry escapes destination: %q", name)
	}
	if depth := strings.Count(clean, "/") + 1; depth > limits.MaxDirectoryDepth {
		return "", errorf(CodeLimitExceeded, "archive entry exceeds directory depth")
	}
	for _, segment := range strings.Split(clean, "/") {
		if unsafeWindowsPathSegment(segment) {
			return "", errorf(CodeUnsafeEntry, "archive entry is not portable or safe: %q", name)
		}
	}
	return clean, nil
}

func unsafeWindowsPathSegment(segment string) bool {
	if segment == "" || strings.Contains(segment, ":") || strings.HasSuffix(segment, ".") || strings.HasSuffix(segment, " ") {
		return true
	}
	base := strings.ToUpper(strings.SplitN(segment, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return true
	}
	return len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9'
}

func safeJoin(root, entry string) (string, error) {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	target := filepath.Join(rootAbs, filepath.FromSlash(entry))
	rel, err := filepath.Rel(rootAbs, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return "", errorf(CodeUnsafeEntry, "archive entry escapes destination")
	}
	return target, nil
}

func writeEntry(dst string, src io.Reader, declared int64, c *counters) (int64, error) {
	if declared > c.limits.MaxFileBytes || (declared >= 0 && declared > c.limits.MaxTotalBytes-c.written) {
		return 0, errorf(CodeLimitExceeded, "archive entry exceeds size limit")
	}
	if c.root == "" {
		return 0, errorf(CodeIO, "archive extraction root is missing")
	}
	if err := ensureNoSymlinkParent(c.root, filepath.Dir(dst)); err != nil {
		return 0, err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return 0, errorf(CodeIO, "create archive directory: %v", err)
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, errorf(CodeIO, "create extracted file: %v", err)
	}
	limit := c.limits.MaxFileBytes
	if remaining := c.limits.MaxTotalBytes - c.written; remaining < limit {
		limit = remaining
	}
	n, copyErr := io.Copy(out, io.LimitReader(src, limit+1))
	closeErr := out.Close()
	if copyErr != nil {
		if isCorruptStreamError(copyErr) {
			return n, errorf(CodeCorruptArchive, "read archive entry: %v", copyErr)
		}
		return n, errorf(CodeIO, "write archive entry: %v", copyErr)
	}
	if closeErr != nil {
		return n, errorf(CodeIO, "close extracted file: %v", closeErr)
	}
	if n > limit || (declared >= 0 && n != declared) {
		return n, errorf(CodeLimitExceeded, "archive entry exceeds size limit")
	}
	c.files++
	c.written += n
	c.warnCompressionRatio()
	return n, nil
}

func isCorruptStreamError(err error) bool {
	if err == io.ErrUnexpectedEOF {
		return true
	}
	text := strings.ToLower(err.Error())
	return strings.Contains(text, "checksum") || strings.Contains(text, "crc") || strings.Contains(text, "unexpected end") ||
		strings.Contains(text, "gzip") || strings.Contains(text, "bzip2") || strings.Contains(text, "invalid tar")
}

func (c *counters) warnCompressionRatio() {
	if c.warnings == nil || c.limits.MaxCompressionRatio <= 0 || c.inputBytes <= 0 || c.written <= c.inputBytes*c.limits.MaxCompressionRatio {
		return
	}
	for _, warning := range *c.warnings {
		if warning == "archive expansion ratio exceeds warning threshold" {
			return
		}
	}
	*c.warnings = append(*c.warnings, "archive expansion ratio exceeds warning threshold")
}

func (c *counters) include(entry Entry) (bool, error) {
	if c.filter == nil {
		return true, nil
	}
	include, err := c.filter(entry)
	if err != nil {
		return false, errorf(CodeInvalidArgument, "archive entry filter: %v", err)
	}
	return include, nil
}

func addDirectory(root, entry string, c *counters) error {
	dst, err := safeJoin(root, entry)
	if err != nil {
		return err
	}
	if c.root == "" {
		return errorf(CodeIO, "archive extraction root is missing")
	}
	if err := ensureNoSymlinkParent(c.root, filepath.Dir(dst)); err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o700); err != nil {
		return errorf(CodeIO, "create archive directory: %v", err)
	}
	c.dirs++
	return nil
}

func addSymlink(root, entry, linkName string, c *counters) error {
	if !c.allowSymlinks {
		return errorf(CodeUnsafeEntry, "symlink entry is not supported: %s", entry)
	}
	if strings.IndexByte(linkName, 0) >= 0 {
		return errorf(CodeUnsafeEntry, "symlink target contains NUL: %s", entry)
	}
	dst, err := safeJoin(root, entry)
	if err != nil {
		return err
	}
	linkName = filepath.FromSlash(strings.ReplaceAll(linkName, "\\", "/"))
	if filepath.IsAbs(linkName) || (len(linkName) >= 2 && linkName[1] == ':') {
		return errorf(CodeUnsafeEntry, "symlink target is absolute: %s", entry)
	}
	target := filepath.Join(filepath.Dir(dst), linkName)
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return errorf(CodeIO, "resolve extraction root: %v", err)
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return errorf(CodeIO, "resolve symlink target: %v", err)
	}
	rel, err := filepath.Rel(rootAbs, targetAbs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return errorf(CodeUnsafeEntry, "symlink target escapes extraction root: %s", entry)
	}
	if err := ensureNoSymlinkParent(rootAbs, filepath.Dir(dst)); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o700); err != nil {
		return errorf(CodeIO, "create symlink parent: %v", err)
	}
	if err := os.Symlink(linkName, dst); err != nil {
		return errorf(CodeIO, "create archive symlink: %v", err)
	}
	c.files++
	return nil
}

// ensureNoSymlinkParent prohibits later archive entries from writing through a
// previously extracted link, even for the small trusted-bundle exception.
func ensureNoSymlinkParent(root, directory string) error {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return errorf(CodeIO, "resolve extraction root: %v", err)
	}
	for current := directory; ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return errorf(CodeUnsafeEntry, "archive output parent is a symlink: %s", current)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return errorf(CodeIO, "inspect archive output parent: %v", statErr)
		}
		if filepath.Clean(current) == rootAbs {
			return nil
		}
		parent := filepath.Dir(current)
		if parent == current {
			return errorf(CodeUnsafeEntry, "archive output parent escapes extraction root")
		}
	}
}

func makeStaging(destination string) (string, error) {
	parent := filepath.Dir(destination)
	if err := rejectSymlinkAncestors(parent); err != nil {
		return "", err
	}
	if info, err := os.Stat(destination); err == nil && info != nil {
		return "", errorf(CodeDestinationExists, "destination already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return "", errorf(CodeIO, "check destination: %v", err)
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", errorf(CodeIO, "create destination parent: %v", err)
	}
	if err := rejectSymlinkAncestors(parent); err != nil {
		return "", err
	}
	return os.MkdirTemp(parent, ".archive-staging-")
}

// PrepareMergeStage creates a private sibling staging directory for a later
// merge into destination. Unlike PrepareExternalStage, destination may already
// exist. It centralizes the parent creation and link checks so callers do not
// create a staging directory through a destination-parent symlink before the
// archive safety envelope runs.
func PrepareMergeStage(destination string) (string, error) {
	if destination == "" {
		return "", errorf(CodeInvalidArgument, "merge destination is required")
	}
	parent := filepath.Dir(destination)
	if err := rejectSymlinkAncestors(parent); err != nil {
		return "", err
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return "", errorf(CodeIO, "create merge destination parent: %v", err)
	}
	if err := rejectSymlinkAncestors(parent); err != nil {
		return "", err
	}
	stage, err := os.MkdirTemp(parent, ".archive-merge-stage-")
	if err != nil {
		return "", errorf(CodeIO, "create merge staging directory: %v", err)
	}
	return stage, nil
}

// PrepareExternalStage reserves a new sibling staging directory. It never
// runs a program; hosts use it only after their command/approval policy allows
// a particular external adapter.
func PrepareExternalStage(destination string) (ExternalStage, error) {
	stage, err := makeStaging(destination)
	if err != nil {
		return ExternalStage{}, err
	}
	return ExternalStage{Path: stage, Destination: destination}, nil
}

func (s ExternalStage) Publish() error {
	if s.Path == "" || s.Destination == "" {
		return errorf(CodeInvalidArgument, "invalid external archive staging transaction")
	}
	stageInfo, err := os.Lstat(s.Path)
	if err != nil {
		return errorf(CodeIO, "inspect external archive staging: %v", err)
	}
	if stageInfo.Mode()&os.ModeSymlink != 0 || !stageInfo.IsDir() {
		return errorf(CodeUnsafeEntry, "external archive staging is not a regular directory")
	}
	if s.validated == nil {
		return errorf(CodeInvalidArgument, "external archive staging must be validated before publish")
	}
	current, err := stampValidatedDirectory(s.Path)
	if err != nil {
		return err
	}
	if current != s.validated.stamp {
		return errorf(CodeSourceChanged, "external archive staging changed after validation")
	}
	if err := rejectSymlinkAncestors(filepath.Dir(s.Destination)); err != nil {
		return err
	}
	if _, err := os.Stat(s.Destination); err == nil {
		return errorf(CodeDestinationExists, "destination already exists: %s", s.Destination)
	} else if !os.IsNotExist(err) {
		return errorf(CodeIO, "check destination: %v", err)
	}
	if err := renameNoReplace(s.Path, s.Destination); err != nil {
		if isNoReplaceCollision(err) {
			return errorf(CodeDestinationExists, "destination already exists: %s", s.Destination)
		}
		return errorf(CodeIO, "publish extracted archive: %v", err)
	}
	return nil
}

// Validate scans an external program's output and binds the successful result
// to this staging transaction. Callers must use the returned stage for Publish.
func (s ExternalStage) Validate(limits Limits) (stage ExternalStage, files, dirs int, written int64, err error) {
	stage = s
	files, dirs, written, err = ValidateExtractedDirectory(s.Path, limits)
	if err != nil {
		return stage, files, dirs, written, err
	}
	stamp, err := stampValidatedDirectory(s.Path)
	if err != nil {
		return stage, files, dirs, written, err
	}
	stage.validated = &validatedDirectoryState{files: files, dirs: dirs, written: written, stamp: stamp}
	return stage, files, dirs, written, nil
}

// publishNewFile atomically publishes a newly created file. It mirrors the
// directory transaction's final safety checks so a newly introduced output
// ancestor link or a racing creator cannot turn create_zip into an overwrite.
func publishNewFile(stagePath, destination string) error {
	if stagePath == "" || destination == "" {
		return errorf(CodeInvalidArgument, "invalid archive output transaction")
	}
	if err := rejectSymlinkAncestors(filepath.Dir(destination)); err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return errorf(CodeDestinationExists, "output already exists: %s", destination)
	} else if !os.IsNotExist(err) {
		return errorf(CodeIO, "check output: %v", err)
	}
	if err := renameNoReplace(stagePath, destination); err != nil {
		if isNoReplaceCollision(err) {
			return errorf(CodeDestinationExists, "output already exists: %s", destination)
		}
		return errorf(CodeIO, "publish zip: %v", err)
	}
	return nil
}

// MergeValidatedDirectory copies a directory tree that has already passed
// archive validation into an existing destination. It is deliberately not an
// archive extractor: callers use it only after ExtractToDirectory has created
// a private staging tree and when their established product semantics require
// merging/replacing existing files. Both source and destination links and
// special files are rejected.
func MergeValidatedDirectory(sourceRoot, destinationRoot string) error {
	if sourceRoot == "" || destinationRoot == "" {
		return errorf(CodeInvalidArgument, "source and destination directories are required")
	}
	sourceAbs, err := filepath.Abs(sourceRoot)
	if err != nil {
		return errorf(CodeIO, "resolve staged source: %v", err)
	}
	destinationAbs, err := filepath.Abs(destinationRoot)
	if err != nil {
		return errorf(CodeIO, "resolve staged destination: %v", err)
	}
	if directoryRootsOverlap(sourceAbs, destinationAbs) {
		return errorf(CodeInvalidArgument, "staged source and merge destination must not overlap")
	}
	if err := validateValidatedDirectory(sourceAbs); err != nil {
		return err
	}
	if err := rejectSymlinkAncestors(destinationAbs); err != nil {
		return err
	}
	if err := os.MkdirAll(destinationAbs, 0o700); err != nil {
		return errorf(CodeIO, "create merge destination: %v", err)
	}
	if err := rejectSymlinkAncestors(destinationAbs); err != nil {
		return err
	}
	return filepath.WalkDir(sourceAbs, func(source string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errorf(CodeIO, "walk staged source: %v", walkErr)
		}
		rel, err := filepath.Rel(sourceAbs, source)
		if err != nil {
			return errorf(CodeIO, "resolve staged entry: %v", err)
		}
		if rel == "." {
			return nil
		}
		canonical, err := canonicalEntry(filepath.ToSlash(rel), DefaultLimits())
		if err != nil {
			return err
		}
		target, err := safeJoin(destinationAbs, canonical)
		if err != nil {
			return err
		}
		entryInfo, err := os.Lstat(source)
		if err != nil {
			return errorf(CodeIO, "inspect staged entry: %v", err)
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			return errorf(CodeUnsafeEntry, "staged source contains a symlink: %s", canonical)
		}
		if entry.IsDir() {
			if err := ensureNoSymlinkParent(destinationAbs, filepath.Dir(target)); err != nil {
				return err
			}
			if existing, statErr := os.Lstat(target); statErr == nil {
				if existing.Mode()&os.ModeSymlink != 0 || !existing.IsDir() {
					return errorf(CodeUnsafeEntry, "merge destination conflicts with directory: %s", canonical)
				}
			} else if !os.IsNotExist(statErr) {
				return errorf(CodeIO, "inspect merge destination: %v", statErr)
			}
			if err := os.MkdirAll(target, 0o700); err != nil {
				return errorf(CodeIO, "create merge directory: %v", err)
			}
			return nil
		}
		if !entryInfo.Mode().IsRegular() {
			return errorf(CodeUnsafeEntry, "staged source contains a special file: %s", canonical)
		}
		return copyMergedFile(source, target, destinationAbs, canonical, entryInfo)
	})
}

// ReplaceValidatedTopLevelDirectories applies a validated staging tree to an
// existing destination, replacing only its top-level directories while merging
// top-level files. It first assembles every incoming top-level directory in a
// private sibling transaction, then swaps that prepared directory into place.
// An individual directory is therefore never left absent or partially copied
// if copying it fails. The staging tree must be private output of
// ExtractToDirectory; this function never follows source or destination links.
func ReplaceValidatedTopLevelDirectories(sourceRoot, destinationRoot string) error {
	if sourceRoot == "" || destinationRoot == "" {
		return errorf(CodeInvalidArgument, "source and destination directories are required")
	}
	sourceAbs, err := filepath.Abs(sourceRoot)
	if err != nil {
		return errorf(CodeIO, "resolve staged source: %v", err)
	}
	destinationAbs, err := filepath.Abs(destinationRoot)
	if err != nil {
		return errorf(CodeIO, "resolve staged destination: %v", err)
	}
	if directoryRootsOverlap(sourceAbs, destinationAbs) {
		return errorf(CodeInvalidArgument, "staged source and replacement destination must not overlap")
	}
	// Validate the complete tree before removing any existing top-level
	// directory. This preserves an existing installation when a staging tree
	// was unexpectedly tampered with after extraction but before publication.
	if err := validateValidatedDirectory(sourceAbs); err != nil {
		return err
	}
	if err := rejectSymlinkAncestors(destinationAbs); err != nil {
		return err
	}
	if err := os.MkdirAll(destinationAbs, 0o700); err != nil {
		return errorf(CodeIO, "create replacement destination: %v", err)
	}
	if err := rejectSymlinkAncestors(destinationAbs); err != nil {
		return err
	}
	entries, err := os.ReadDir(sourceAbs)
	if err != nil {
		return errorf(CodeIO, "read staged source: %v", err)
	}
	for _, entry := range entries {
		canonical, err := canonicalEntry(entry.Name(), DefaultLimits())
		if err != nil {
			return err
		}
		source := filepath.Join(sourceAbs, entry.Name())
		info, err := os.Lstat(source)
		if err != nil {
			return errorf(CodeIO, "inspect staged entry: %v", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errorf(CodeUnsafeEntry, "staged source contains a symlink: %s", canonical)
		}
		target, err := safeJoin(destinationAbs, canonical)
		if err != nil {
			return err
		}
		if info.IsDir() {
			if err := ensureNoSymlinkParent(destinationAbs, filepath.Dir(target)); err != nil {
				return err
			}
			if err := replaceValidatedDirectory(source, target, destinationAbs, canonical); err != nil {
				return err
			}
			continue
		}
		if !info.Mode().IsRegular() {
			return errorf(CodeUnsafeEntry, "staged source contains a special file: %s", canonical)
		}
		if err := copyMergedFile(source, target, destinationAbs, canonical, info); err != nil {
			return err
		}
	}
	return nil
}

func replaceValidatedDirectory(source, target, destinationRoot, canonical string) error {
	parent := filepath.Dir(target)
	if err := ensureNoSymlinkParent(destinationRoot, parent); err != nil {
		return err
	}
	prepared, err := os.MkdirTemp(parent, ".archive-replace-*")
	if err != nil {
		return errorf(CodeIO, "create replacement staging directory: %v", err)
	}
	defer func() { _ = os.RemoveAll(prepared) }()
	if err := MergeValidatedDirectory(source, prepared); err != nil {
		return err
	}
	if err := ensureNoSymlinkParent(destinationRoot, parent); err != nil {
		return err
	}
	backup := ""
	cleanupBackup := false
	if existing, err := os.Lstat(target); err == nil {
		if existing.Mode()&os.ModeSymlink != 0 || !existing.IsDir() {
			return errorf(CodeUnsafeEntry, "replacement destination conflicts with directory: %s", canonical)
		}
		backup, err = reserveReplacementBackup(parent)
		if err != nil {
			return err
		}
		// Retain the old directory if rollback cannot restore it. Removing it in
		// a deferred cleanup after a failed restore would turn a recoverable
		// concurrency conflict into silent data loss.
		defer func() {
			if cleanupBackup {
				_ = os.RemoveAll(backup)
			}
		}()
		if err := renameNoReplace(target, backup); err != nil {
			if isNoReplaceCollision(err) {
				return errorf(CodeIO, "reserve replacement backup collided: %s", canonical)
			}
			return errorf(CodeIO, "move replaced directory aside: %v", err)
		}
	} else if !os.IsNotExist(err) {
		return errorf(CodeIO, "inspect replacement destination: %v", err)
	}
	if err := renameNoReplace(prepared, target); err != nil {
		if backup != "" {
			if restoreErr := renameNoReplace(backup, target); restoreErr != nil {
				return errorf(CodeIO, "publish replacement directory: %v; prior directory retained at %s because restore failed: %v", err, backup, restoreErr)
			}
		}
		if isNoReplaceCollision(err) {
			return errorf(CodeDestinationExists, "replacement destination appeared concurrently: %s", canonical)
		}
		return errorf(CodeIO, "publish replacement directory: %v", err)
	}
	cleanupBackup = true
	return nil
}

// reserveReplacementBackup obtains an unused sibling path without retaining
// the directory itself. The subsequent no-replace rename moves the old tree
// there atomically, so a failed final publish can restore the prior version.
func reserveReplacementBackup(parent string) (string, error) {
	backup, err := os.MkdirTemp(parent, ".archive-replace-backup-*")
	if err != nil {
		return "", errorf(CodeIO, "create replacement backup reservation: %v", err)
	}
	if err := os.Remove(backup); err != nil {
		return "", errorf(CodeIO, "release replacement backup reservation: %v", err)
	}
	return backup, nil
}

// validateValidatedDirectory checks the source-side invariant shared by the
// merge helpers. Keeping this preflight separate from the copy walk means
// replacement callers do not delete a previous package before discovering an
// invalid sibling elsewhere in staging.
func validateValidatedDirectory(root string) error {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return errorf(CodeIO, "inspect staged source: %v", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return errorf(CodeUnsafeEntry, "staged source is not a regular directory")
	}
	return filepath.WalkDir(root, func(source string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errorf(CodeIO, "walk staged source: %v", walkErr)
		}
		if source == root {
			return nil
		}
		rel, err := filepath.Rel(root, source)
		if err != nil {
			return errorf(CodeIO, "resolve staged entry: %v", err)
		}
		canonical, err := canonicalEntry(filepath.ToSlash(rel), DefaultLimits())
		if err != nil {
			return err
		}
		info, err := os.Lstat(source)
		if err != nil {
			return errorf(CodeIO, "inspect staged entry: %v", err)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errorf(CodeUnsafeEntry, "staged source contains a symlink: %s", canonical)
		}
		if info.IsDir() || info.Mode().IsRegular() {
			return nil
		}
		return errorf(CodeUnsafeEntry, "staged source contains a special file: %s", canonical)
	})
}

func directoryRootsOverlap(first, second string) bool {
	for _, pair := range [][2]string{{first, second}, {second, first}} {
		rel, err := filepath.Rel(pair[0], pair[1])
		if err != nil {
			return true
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))) {
			return true
		}
	}
	return false
}

func copyMergedFile(source, target, destinationRoot, canonical string, sourceInfo os.FileInfo) error {
	parent := filepath.Dir(target)
	if err := ensureNoSymlinkParent(destinationRoot, parent); err != nil {
		return err
	}
	if err := os.MkdirAll(parent, 0o700); err != nil {
		return errorf(CodeIO, "create merge file parent: %v", err)
	}
	if err := ensureNoSymlinkParent(destinationRoot, parent); err != nil {
		return err
	}
	if existing, err := os.Lstat(target); err == nil {
		if existing.Mode()&os.ModeSymlink != 0 || existing.IsDir() || !existing.Mode().IsRegular() {
			return errorf(CodeUnsafeEntry, "merge destination conflicts with file: %s", canonical)
		}
	} else if !os.IsNotExist(err) {
		return errorf(CodeIO, "inspect merge destination file: %v", err)
	}
	in, err := os.Open(source)
	if err != nil {
		return errorf(CodeIO, "open staged file: %v", err)
	}
	defer in.Close()
	openedInfo, err := in.Stat()
	if err != nil {
		return errorf(CodeIO, "inspect opened staged file: %v", err)
	}
	if !openedInfo.Mode().IsRegular() || !os.SameFile(sourceInfo, openedInfo) {
		return errorf(CodeUnsafeEntry, "staged source changed while merging: %s", canonical)
	}
	tmp, err := os.CreateTemp(parent, ".archive-merge-*")
	if err != nil {
		return errorf(CodeIO, "create merge file staging: %v", err)
	}
	tmpPath := tmp.Name()
	defer func() { _ = os.Remove(tmpPath) }()
	if err := tmp.Chmod(sourceInfo.Mode().Perm()); err != nil {
		_ = tmp.Close()
		return errorf(CodeIO, "set merge file mode: %v", err)
	}
	written, err := io.Copy(tmp, io.LimitReader(in, sourceInfo.Size()+1))
	if err != nil {
		_ = tmp.Close()
		return errorf(CodeIO, "copy staged file: %v", err)
	}
	if written != sourceInfo.Size() {
		_ = tmp.Close()
		return errorf(CodeUnsafeEntry, "staged source changed while merging: %s", canonical)
	}
	finalSourceInfo, err := in.Stat()
	if err != nil {
		_ = tmp.Close()
		return errorf(CodeIO, "reinspect staged file: %v", err)
	}
	if !finalSourceInfo.Mode().IsRegular() || !os.SameFile(sourceInfo, finalSourceInfo) || finalSourceInfo.Size() != sourceInfo.Size() || !finalSourceInfo.ModTime().Equal(sourceInfo.ModTime()) {
		_ = tmp.Close()
		return errorf(CodeUnsafeEntry, "staged source changed while merging: %s", canonical)
	}
	if err := tmp.Close(); err != nil {
		return errorf(CodeIO, "close merge file staging: %v", err)
	}
	if err := ensureNoSymlinkParent(destinationRoot, parent); err != nil {
		return err
	}
	if err := renameReplace(tmpPath, target); err != nil {
		return errorf(CodeIO, "publish merged file: %v", err)
	}
	return nil
}

func (s ExternalStage) Cleanup() {
	if s.Path != "" {
		_ = os.RemoveAll(s.Path)
	}
}

// ValidateExtractedDirectory applies the post-extraction safety envelope to
// content produced by an external archiver. It rejects links and special files
// and applies the same file-count/size limits as embedded extraction.
func ValidateExtractedDirectory(root string, limits Limits) (files, dirs int, written int64, err error) {
	if root == "" {
		err = errorf(CodeInvalidArgument, "external output root is required")
		return
	}
	rootInfo, statErr := os.Lstat(root)
	if statErr != nil {
		err = errorf(CodeIO, "inspect external output root: %v", statErr)
		return
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		err = errorf(CodeUnsafeEntry, "external output root is not a regular directory")
		return
	}
	limits = limits.normalized()
	entries := 0
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errorf(CodeIO, "scan external output: %v", walkErr)
		}
		if p == root {
			return nil
		}
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return errorf(CodeIO, "resolve external output path: %v", relErr)
		}
		// WalkDir gives us names below root, but normalize them through the same
		// entry validator as embedded readers so deeply nested or invalid names
		// cannot bypass the archive safety envelope.
		if _, canonicalErr := canonicalEntry(filepath.ToSlash(rel), limits); canonicalErr != nil {
			return canonicalErr
		}
		if d.Type()&os.ModeSymlink != 0 {
			return errorf(CodeUnsafeEntry, "external output contains a symlink: %s", p)
		}
		entries++
		if entries > limits.MaxFiles {
			return errorf(CodeLimitExceeded, "external output exceeds entry count limit")
		}
		if d.IsDir() {
			dirs++
			return nil
		}
		info, infoErr := d.Info()
		if infoErr != nil {
			return errorf(CodeIO, "inspect external output: %v", infoErr)
		}
		if !info.Mode().IsRegular() {
			return errorf(CodeUnsafeEntry, "external output contains a special file: %s", p)
		}
		if info.Size() > limits.MaxFileBytes || info.Size() > limits.MaxTotalBytes-written {
			return errorf(CodeLimitExceeded, "external output exceeds size limit")
		}
		files++
		written += info.Size()
		return nil
	})
	return
}

func stampValidatedDirectory(root string) (directoryStamp, error) {
	rootInfo, err := os.Lstat(root)
	if err != nil {
		return directoryStamp{}, errorf(CodeIO, "inspect external output root: %v", err)
	}
	if rootInfo.Mode()&os.ModeSymlink != 0 || !rootInfo.IsDir() {
		return directoryStamp{}, errorf(CodeUnsafeEntry, "external output root is not a regular directory")
	}
	hash := sha256.New()
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return errorf(CodeIO, "scan external output: %v", walkErr)
		}
		if path == root {
			return nil
		}
		info, err := os.Lstat(path)
		if err != nil {
			return errorf(CodeIO, "inspect external output: %v", err)
		}
		if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
			return errorf(CodeUnsafeEntry, "external output changed after validation: %s", path)
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return errorf(CodeIO, "resolve external output path: %v", err)
		}
		writeDirectoryStampField(hash, filepath.ToSlash(rel))
		writeDirectoryStampField(hash, info.Mode().String())
		writeDirectoryStampInt64(hash, info.Size())
		writeDirectoryStampInt64(hash, info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			file, err := os.Open(path)
			if err != nil {
				return errorf(CodeIO, "open external output: %v", err)
			}
			_, copyErr := io.Copy(hash, file)
			closeErr := file.Close()
			if copyErr != nil {
				return errorf(CodeIO, "read external output: %v", copyErr)
			}
			if closeErr != nil {
				return errorf(CodeIO, "close external output: %v", closeErr)
			}
		}
		return nil
	})
	if err != nil {
		return directoryStamp{}, err
	}
	var stamp directoryStamp
	copy(stamp.digest[:], hash.Sum(nil))
	return stamp, nil
}

func writeDirectoryStampField(hash io.Writer, value string) {
	writeDirectoryStampInt64(hash, int64(len(value)))
	_, _ = io.WriteString(hash, value)
}

func writeDirectoryStampInt64(hash io.Writer, value int64) {
	var data [8]byte
	binary.LittleEndian.PutUint64(data[:], uint64(value))
	_, _ = hash.Write(data[:])
}

// rejectSymlinkAncestors keeps archive writes from escaping through an
// existing symlink in a requested output path. Hosts may allow a resolved
// workspace root separately, but the archive primitive never follows a new
// link while creating a staging tree or output file.
func rejectSymlinkAncestors(directory string) error {
	directory, err := filepath.Abs(directory)
	if err != nil {
		return errorf(CodeIO, "resolve output parent: %v", err)
	}
	for current := directory; ; current = filepath.Dir(current) {
		info, statErr := os.Lstat(current)
		if statErr == nil && info.Mode()&os.ModeSymlink != 0 {
			return errorf(CodeUnsafeEntry, "output parent contains a symlink: %s", current)
		}
		if statErr != nil && !os.IsNotExist(statErr) {
			return errorf(CodeIO, "inspect output parent: %v", statErr)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return nil
		}
	}
}
