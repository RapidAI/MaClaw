// Package archiveutil provides safe archive inspection, extraction, and ZIP
// creation primitives. Embedded handlers use pure Go. External extraction is
// opt-in and only invokes an already-installed program through fixed argument
// arrays after the host has recorded the required approval.
package archiveutil

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
)

type Action string

const (
	ActionInspect Action = "inspect"
	ActionExtract Action = "extract"
	// ActionExtractExternal invokes a controlled, already-installed external
	// archiver only when AllowExternal is set by the host after authorization.
	ActionExtractExternal Action = "extract_external"
	ActionCreateZIP       Action = "create_zip"
)

type Format string

const (
	FormatUnknown Format = "unknown"
	FormatZIP     Format = "zip"
	FormatTAR     Format = "tar"
	FormatGZIP    Format = "gz"
	FormatTarGZIP Format = "tar.gz"
	FormatBZIP2   Format = "bz2"
	FormatTarBZ2  Format = "tar.bz2"
	FormatRAR     Format = "rar"
	Format7Z      Format = "7z"
	FormatXZ      Format = "xz"
	FormatZSTD    Format = "zst"
)

const (
	CodeFormatUnrecognized       = "FORMAT_UNRECOGNIZED"
	CodeFormatUnsupported        = "FORMAT_UNSUPPORTED"
	CodeExternalFallbackRequired = "EXTERNAL_FALLBACK_REQUIRED"
	// CodeExternalApprovalRequired means an external executable was selected
	// but the host has not yet recorded a one-time user approval for it.
	CodeExternalApprovalRequired = "EXTERNAL_APPROVAL_REQUIRED"
	CodeExternalToolNotFound     = "EXTERNAL_TOOL_NOT_FOUND"
	CodeExternalToolUnusable     = "EXTERNAL_TOOL_UNUSABLE"
	CodeExternalExecutionFailed  = "EXTERNAL_EXECUTION_FAILED"
	CodeDestinationExists        = "DESTINATION_EXISTS"
	CodeLimitExceeded            = "LIMIT_EXCEEDED"
	CodeUnsafeEntry              = "UNSAFE_ENTRY"
	CodeCorruptArchive           = "CORRUPT_ARCHIVE"
	CodeSourceChanged            = "SOURCE_CHANGED"
	CodeEncryptedArchive         = "ENCRYPTED_ARCHIVE"
	CodeMultiVolumeUnsupported   = "MULTIVOLUME_UNSUPPORTED"
	CodeInvalidArgument          = "INVALID_ARGUMENT"
	CodeIO                       = "IO_ERROR"
)

// Limits bound CPU, disk and directory-tree consumption. Zero values use the
// conservative defaults below.
type Limits struct {
	MaxInputBytes     int64
	MaxFiles          int
	MaxFileBytes      int64
	MaxTotalBytes     int64
	MaxDirectoryDepth int
	MaxEntryNameBytes int
	MaxListedEntries  int
	// MaxCompressionRatio defaults to 200 and only generates a warning when
	// exceeded. Set it to a negative value to suppress that diagnostic.
	// Extraction does not reject an archive solely for crossing it: actual
	// per-file and total expanded-size limits remain the hard safety boundary.
	MaxCompressionRatio int64
}

func DefaultLimits() Limits {
	return Limits{
		MaxInputBytes:       2 << 30,
		MaxFiles:            10_000,
		MaxFileBytes:        1 << 30,
		MaxTotalBytes:       4 << 30,
		MaxDirectoryDepth:   64,
		MaxEntryNameBytes:   4096,
		MaxListedEntries:    100,
		MaxCompressionRatio: 200,
	}
}

func (l Limits) normalized() Limits {
	d := DefaultLimits()
	if l.MaxInputBytes > 0 {
		d.MaxInputBytes = l.MaxInputBytes
	}
	if l.MaxFiles > 0 {
		d.MaxFiles = l.MaxFiles
	}
	if l.MaxFileBytes > 0 {
		d.MaxFileBytes = l.MaxFileBytes
	}
	if l.MaxTotalBytes > 0 {
		d.MaxTotalBytes = l.MaxTotalBytes
	}
	if l.MaxDirectoryDepth > 0 {
		d.MaxDirectoryDepth = l.MaxDirectoryDepth
	}
	if l.MaxEntryNameBytes > 0 {
		d.MaxEntryNameBytes = l.MaxEntryNameBytes
	}
	if l.MaxListedEntries > 0 {
		d.MaxListedEntries = l.MaxListedEntries
	}
	if l.MaxCompressionRatio != 0 {
		d.MaxCompressionRatio = l.MaxCompressionRatio
	}
	return d
}

type Request struct {
	Action      Action
	ArchivePath string
	Destination string
	SourcePaths []string
	OutputPath  string
	// ConflictPolicy and RootMode make the P0 safety contract explicit. Only
	// "fail" and "preserve" respectively are accepted; empty uses those
	// defaults. Merge/overwrite extraction is intentionally not implemented.
	ConflictPolicy string
	RootMode       string
	// AllowExternal is deliberately opt-in. A caller must obtain any required
	// user/host approval before requesting an external program invocation.
	AllowExternal bool
	// BasePath is the worktree boundary used to derive stable, non-absolute ZIP
	// entry names for create_zip. Sources must reside underneath this path.
	BasePath string
	Limits   Limits
}

// ExtractionPolicy controls narrowly scoped deviations from the default
// fail-closed extraction behaviour.  Public archive-tool calls always use the
// zero policy, which rejects links and special entries.
type ExtractionPolicy struct {
	// AllowSymlinks is reserved for trusted internal runtime bundles that need
	// relative symlinks. The core still validates that the link target stays
	// within the extraction root and never allows a symlink as a later parent.
	AllowSymlinks bool
	// Filter is evaluated only after the core has canonicalized the entry path
	// and rejected encrypted, link, and special entries. Returning false skips
	// an otherwise safe entry; returning an error aborts extraction. This lets
	// callers retain narrow business rules (such as denylisted file extensions)
	// without duplicating archive parsing or path-safety code.
	Filter EntryFilter
}

// EntryFilter decides whether an otherwise valid archive entry is materialized.
// It must be deterministic and must not write outside the extraction root.
type EntryFilter func(Entry) (include bool, err error)

// ExtractToDirectory extracts an embedded-supported archive directly into an
// existing, caller-owned empty directory.  It is intended for internal flows
// that already own a private temporary directory and need the same validated
// archive readers without another staging/publish transaction.
func ExtractToDirectory(archivePath, destination string, limits Limits) Result {
	return ExtractToDirectoryWithPolicy(archivePath, destination, limits, ExtractionPolicy{})
}

// ExtractToDirectoryWithPolicy is the internal variant of ExtractToDirectory.
// Callers should use a non-zero policy only for a verified, trusted bundle.
func ExtractToDirectoryWithPolicy(archivePath, destination string, limits Limits, policy ExtractionPolicy) Result {
	if archivePath == "" || destination == "" {
		return failure(ActionExtract, FormatUnknown, errorf(CodeInvalidArgument, "archive_path and destination are required"))
	}
	before, err := captureSourceState(archivePath)
	if err != nil {
		return failure(ActionExtract, FormatUnknown, errorf(CodeIO, "stat archive: %v", err))
	}
	limits = limits.normalized()
	if before.size > limits.MaxInputBytes {
		return failure(ActionExtract, FormatUnknown, errorf(CodeLimitExceeded, "archive input exceeds size limit"))
	}
	format, err := Detect(archivePath)
	if err != nil {
		return failure(ActionExtract, format, err)
	}
	if !embedded(format) {
		r := externalFallback(format)
		r.Action, r.InputPath = ActionExtract, archivePath
		return r
	}
	// Check before MkdirAll: otherwise an existing symlink ancestor could make
	// directory creation itself escape the caller-owned extraction root.
	if err := rejectSymlinkAncestors(destination); err != nil {
		return failure(ActionExtract, format, err)
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return failure(ActionExtract, format, errorf(CodeIO, "create extraction directory: %v", err))
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return failure(ActionExtract, format, errorf(CodeIO, "read extraction directory: %v", err))
	}
	if len(entries) != 0 {
		return failure(ActionExtract, format, errorf(CodeDestinationExists, "extraction directory must be empty: %s", destination))
	}
	// Repeat after creation to narrow the check/create race and to reject a
	// destination that was swapped for a link while it was being prepared.
	if err := rejectSymlinkAncestors(destination); err != nil {
		return failure(ActionExtract, format, err)
	}
	warnings := []string(nil)
	c := &counters{limits: limits, allowSymlinks: policy.AllowSymlinks, filter: policy.Filter, root: destination, inputBytes: before.size, warnings: &warnings}
	if err := extractInto(archivePath, format, destination, c); err != nil {
		return failure(ActionExtract, format, err)
	}
	if err := ensureSourceUnchanged(archivePath, before); err != nil {
		return failure(ActionExtract, format, err)
	}
	warnings = append(warnings, archiveFormatNameWarnings(archivePath, format)...)
	return Result{OK: true, Action: ActionExtract, Format: format, InputPath: archivePath, OutputPath: destination, Files: c.files, Directories: c.dirs, WrittenBytes: c.written, Warnings: uniqueWarnings(warnings)}
}

// ExtractZIPBytesToDirectory applies the same safe ZIP extraction rules to an
// in-memory ZIP payload. It is intended for internal upload and rollback
// paths which receive an already-buffered ZIP and must not reimplement ZIP
// traversal, path validation, or expansion limits.
//
// destination must be an existing or new empty directory. Unlike file-backed
// extraction, no source-change check is possible for an immutable byte slice.
func ExtractZIPBytesToDirectory(data []byte, destination string, limits Limits, policy ExtractionPolicy) Result {
	if destination == "" {
		return failure(ActionExtract, FormatZIP, errorf(CodeInvalidArgument, "destination is required"))
	}
	limits = limits.normalized()
	if int64(len(data)) > limits.MaxInputBytes {
		return failure(ActionExtract, FormatZIP, errorf(CodeLimitExceeded, "archive input exceeds size limit"))
	}
	if err := rejectSymlinkAncestors(destination); err != nil {
		return failure(ActionExtract, FormatZIP, err)
	}
	if err := os.MkdirAll(destination, 0o700); err != nil {
		return failure(ActionExtract, FormatZIP, errorf(CodeIO, "create extraction directory: %v", err))
	}
	entries, err := os.ReadDir(destination)
	if err != nil {
		return failure(ActionExtract, FormatZIP, errorf(CodeIO, "read extraction directory: %v", err))
	}
	if len(entries) != 0 {
		return failure(ActionExtract, FormatZIP, errorf(CodeDestinationExists, "extraction directory must be empty: %s", destination))
	}
	if err := rejectSymlinkAncestors(destination); err != nil {
		return failure(ActionExtract, FormatZIP, err)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return failure(ActionExtract, FormatZIP, errorf(CodeCorruptArchive, "open zip: %v", err))
	}
	warnings := []string(nil)
	c := &counters{limits: limits, allowSymlinks: policy.AllowSymlinks, filter: policy.Filter, root: destination, inputBytes: int64(len(data)), warnings: &warnings}
	if err := extractZIPFiles(zr.File, destination, c); err != nil {
		return failure(ActionExtract, FormatZIP, err)
	}
	return Result{OK: true, Action: ActionExtract, Format: FormatZIP, OutputPath: destination, Files: c.files, Directories: c.dirs, WrittenBytes: c.written, Warnings: uniqueWarnings(warnings)}
}

// CanonicalEntry normalizes and validates a portable archive entry path. It
// is exposed for archive-consuming business code that needs to inspect ZIP
// metadata without extracting it; all actual writes still go through the
// extraction APIs above.
func CanonicalEntry(name string, limits Limits) (string, error) {
	return canonicalEntry(name, limits.normalized())
}

type Entry struct {
	Path string `json:"path"`
	// OriginalPath is the archive-provided path before separator normalization.
	// Filters can use it for stricter domain rules; write operations always use
	// the canonical Path field.
	OriginalPath string `json:"-"`
	Dir          bool   `json:"dir"`
	Size         int64  `json:"size,omitempty"`
}

type Fallback struct {
	RecommendedPrograms []string `json:"recommended_programs,omitempty"`
	AvailablePrograms   []string `json:"available_programs,omitempty"`
	CraftToolAllowed    bool     `json:"craft_tool_allowed,omitempty"`
	UserActionRequired  bool     `json:"user_action_required,omitempty"`
}

type Result struct {
	OK           bool      `json:"ok"`
	Action       Action    `json:"action"`
	Format       Format    `json:"format,omitempty"`
	Code         string    `json:"code,omitempty"`
	Message      string    `json:"message,omitempty"`
	InputPath    string    `json:"input_path,omitempty"`
	OutputPath   string    `json:"output_path,omitempty"`
	Files        int       `json:"files,omitempty"`
	Directories  int       `json:"directories,omitempty"`
	WrittenBytes int64     `json:"written_bytes,omitempty"`
	Entries      []Entry   `json:"entries,omitempty"`
	Truncated    bool      `json:"truncated,omitempty"`
	Warnings     []string  `json:"warnings,omitempty"`
	Fallback     *Fallback `json:"fallback,omitempty"`
}

// ExternalStage is a host-facing staging transaction for an approved external
// program. Call Validate before Publish, and always Cleanup when Publish did
// not succeed. A successful validation is bound to the staged directory's
// metadata and Publish rejects any later mutation.
type ExternalStage struct {
	Path        string
	Destination string
	validated   *validatedDirectoryState
}

type validatedDirectoryState struct {
	files, dirs int
	written     int64
	stamp       directoryStamp
}

type directoryStamp struct {
	digest [32]byte
}

type Error struct {
	Code    string
	Message string
	Err     error
}

func (e *Error) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s: %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

func errorf(code, format string, args ...interface{}) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

func failure(action Action, format Format, err error) Result {
	r := Result{OK: false, Action: action, Format: format, Code: CodeIO, Message: err.Error()}
	if typed, ok := err.(*Error); ok {
		r.Code, r.Message = typed.Code, typed.Message
	}
	return r
}
