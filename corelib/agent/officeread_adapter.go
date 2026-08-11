package agent

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	officeread "github.com/RapidAI/OfficeRead"
	"github.com/richardlehane/mscfb"
)

// OfficeExtractEngine selects the Office document text extractor. PDF and
// plain-text formats intentionally remain outside this switch.
type OfficeExtractEngine string

const (
	// OfficeExtractEngineLegacy preserves the pre-OfficeRead parser behavior.
	OfficeExtractEngineLegacy OfficeExtractEngine = "legacy"
	// OfficeExtractEngineDual returns the legacy result while running OfficeRead
	// as a shadow read. It is intended for migration verification.
	OfficeExtractEngineDual OfficeExtractEngine = "dual"
	// OfficeExtractEngineOfficeRead uses OfficeRead as the primary parser.
	OfficeExtractEngineOfficeRead OfficeExtractEngine = "officeread"
)

var errOfficeReadUnavailable = errors.New("OfficeRead extractor is not linked")

// errOfficeReadSourceChanged means an Office source changed after a shared
// container decision was made. Retrying another parser on different bytes
// would invalidate that decision, so callers must fail closed instead.
var errOfficeReadSourceChanged = errors.New("OfficeRead source changed during extraction")

// errOfficeReadTimedOut is a stable, content-free result for a parser call
// that did not complete within MaClaw's host responsiveness budget. OfficeRead
// currently exposes no context-aware extraction API, so the underlying call
// cannot be forcefully stopped; the bounded call gate below makes that
// limitation explicit without allowing timed-out goroutines to accumulate.
var errOfficeReadTimedOut = errors.New("OfficeRead extraction timed out")

// errOfficeReadExtractionFailed is deliberately generic. OfficeRead and the
// filesystem can include paths, relationship names, or other document-derived
// details in errors. Rich-content callers persist failure state in the
// knowledge base, so this boundary must never pass those messages through.
var errOfficeReadExtractionFailed = errors.New("OfficeRead content extraction failed")

// errOfficeReadFormatMismatch is a stable, content-free result for the rich
// content boundary. Unlike the text-only entry point, rich knowledge imports
// carry a caller-supplied source kind into node metadata, so allowing a ZIP
// whose reliable OOXML signature disagrees with its extension would label the
// extracted Markdown and images as the wrong document type.
var errOfficeReadFormatMismatch = errors.New("OfficeRead content format does not match file extension")

// errOfficeReadInputTooLarge is deliberately a stable, content-free error. It
// protects future callers of ExtractOfficeTextWithFormat as well as today's
// tool and auto-injection paths, which already enforce the same ceiling.
var errOfficeReadInputTooLarge = errors.New("OfficeRead input exceeds 32 MiB limit")

// errOfficeReadOutputTooLarge bounds the text/Markdown retained by MaClaw
// after OfficeRead returns. A small compressed container can legitimately
// expand into a very large amount of text; input-size checks alone do not
// protect the paging cache or knowledge import from that case.
var errOfficeReadOutputTooLarge = errors.New("OfficeRead output exceeds retained-content limit")

// errOfficeReadUnsafeContainer is intentionally content-free.  The detailed
// ZIP metadata that caused a rejection is neither useful to the chat caller
// nor safe to put in rollout telemetry.
var errOfficeReadUnsafeContainer = errors.New("OfficeRead input failed container safety checks")

// errOfficeReadEncryptedContainer is distinct from a malformed container so
// rollout diagnostics can separate documents that need credentials from files
// that should be quarantined or repaired. Neither kind is eligible for a
// legacy fallback: reopening the same untrusted container defeats preflight.
var errOfficeReadEncryptedContainer = errors.New("OfficeRead input is encrypted")

// ErrOfficeReadUnsafeContainer is the stable, content-free result of MaClaw's
// OfficeRead boundary checks. Consumers should not inspect third-party parser
// messages for security decisions or telemetry.
var ErrOfficeReadUnsafeContainer = errOfficeReadUnsafeContainer

// ErrOfficeReadEncryptedContainer is the stable, content-free result when
// MaClaw observes an encrypted OOXML ZIP entry before extraction.
var ErrOfficeReadEncryptedContainer = errOfficeReadEncryptedContainer

// ErrOfficeReadExtractionFailed is the content-free rich-content failure
// returned for errors whose detailed cause is not safe to persist or display.
var ErrOfficeReadExtractionFailed = errOfficeReadExtractionFailed

// ErrOfficeReadFormatMismatch is returned by the rich-content boundary when
// a reliable file signature disagrees with the caller's extension-led kind.
// Consumers must not retry the same file through an extension-specific legacy
// parser: that would reopen a container which cannot be labelled correctly.
var ErrOfficeReadFormatMismatch = errOfficeReadFormatMismatch

// ErrOfficeReadInputTooLarge and ErrOfficeReadOutputTooLarge are stable
// resource-policy results. Rich-content consumers must not circumvent either
// ceiling by retrying the same document through an unrestricted legacy path.
var (
	// ErrOfficeReadTimedOut is emitted when the host response budget expires.
	// It intentionally carries neither a path nor a parser error.
	ErrOfficeReadTimedOut = errOfficeReadTimedOut
	// ErrOfficeReadSourceChanged prevents a preflight result for one version
	// from authorizing extraction of a replacement at the same path.
	ErrOfficeReadSourceChanged = errOfficeReadSourceChanged

	ErrOfficeReadInputTooLarge  = errOfficeReadInputTooLarge
	ErrOfficeReadOutputTooLarge = errOfficeReadOutputTooLarge
)

// IsOfficeReadContainerSafetyError reports errors for which callers must not
// retry another parser on the same input container.
func IsOfficeReadContainerSafetyError(err error) bool {
	return errors.Is(err, errOfficeReadUnsafeContainer) || errors.Is(err, errOfficeReadEncryptedContainer) ||
		errors.Is(err, errOfficeReadSourceChanged)
}

// IsOfficeReadRichContentBlocked reports errors for which a rich-content
// consumer must not reopen the same input with a legacy parser. In addition
// to unsafe or encrypted containers, this includes reliably mislabelled
// OOXML/PDF content and adapter-enforced input/output ceilings. Retrying the
// latter through a legacy parser would bypass the resource boundary that the
// explicit rich-content rollout promised to knowledge imports.
func IsOfficeReadRichContentBlocked(err error) bool {
	return IsOfficeReadContainerSafetyError(err) || errors.Is(err, errOfficeReadFormatMismatch) ||
		errors.Is(err, errOfficeReadInputTooLarge) || errors.Is(err, errOfficeReadOutputTooLarge)
}

// PreflightOfficeReadInput applies MaClaw's bounded Office container policy
// without selecting an extraction engine or reading document text. It is safe
// for callers that retain their own output protocol (for example knowledge
// nodes or structured Excel/PPTX JSON), so none can reopen an encrypted or
// malformed Office package merely because OfficeRead rich content is disabled.
//
// It deliberately accepts only Office formats; PDF and plain-text callers
// have their own parsers and resource rules. Returned errors are the stable,
// content-free adapter errors exported by this package.
func PreflightOfficeReadInput(filePath, format string) error {
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	if !isOfficeReadFormat(format) {
		return nil
	}
	info, err := os.Stat(filePath)
	if err != nil || info.IsDir() {
		return errOfficeReadUnsafeContainer
	}
	if info.Size() > MaxOfficeReadFileBytes {
		return errOfficeReadInputTooLarge
	}
	_, err = preflightOfficeReadSource(filePath, format)
	return err
}

// SnapshotOfficeReadInput creates a private, preflighted copy for a caller
// that needs to make more than one path-based Office parser call (for example
// knowledge text nodes plus embedded images).  The caller must invoke cleanup
// once every parser has released the returned pathname.  This is deliberately
// separate from ExtractOfficeText: it preserves an existing caller's output
// protocol while binding all of its subsequent reads to one verified version.
func SnapshotOfficeReadInput(filePath, format string) (snapshot string, cleanup func(), err error) {
	format = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	if !isOfficeReadFormat(format) {
		return "", nil, errOfficeReadFormatMismatch
	}
	snapshot, cleanup, err = SnapshotBoundedDocumentInput(filePath, "."+format)
	if err != nil {
		return "", nil, err
	}
	if _, err = preflightOfficeReadSource(snapshot, format); err != nil {
		cleanup()
		return "", nil, err
	}
	return snapshot, cleanup, nil
}

// SnapshotBoundedDocumentInput creates an owned, bounded copy of a local
// document after checking that the source did not change while it was copied.
// It is for callers whose own parser accepts only a pathname but is not an
// Office container parser. Callers must still apply their type-specific input
// validation to the returned copy. The extension is normalized to the small
// parser-relevant allowlist before creating the temporary file; it is never
// derived from document contents.
func SnapshotBoundedDocumentInput(filePath, extension string) (snapshot string, cleanup func(), err error) {
	snapshot, err = snapshotBoundedDocumentSourceWithExtension(filePath, extension)
	if err != nil {
		return "", nil, err
	}
	var once sync.Once
	cleanup = func() { once.Do(func() { _ = os.Remove(snapshot) }) }
	return snapshot, cleanup, nil
}

// SnapshotCSVInput creates the private, bounded snapshot required by an
// extension-led CSV parser and verifies that it is not an Office/PDF container
// relabelled as CSV. Encryption and malformed-container identities are
// preserved by ValidateCSVInput. All CSV consumers should use this helper
// instead of open-coding SnapshotBoundedDocumentInput plus a later probe.
func SnapshotCSVInput(filePath string) (snapshot string, cleanup func(), err error) {
	snapshot, cleanup, err = SnapshotBoundedDocumentInput(filePath, ".csv")
	if err != nil {
		return "", nil, err
	}
	if err := ValidateCSVInput(snapshot); err != nil {
		cleanup()
		return "", nil, err
	}
	return snapshot, cleanup, nil
}

func sanitizeOfficeReadRichContentError(err error) error {
	if err == nil {
		return nil
	}
	// These errors are constructed by this adapter and are intentionally
	// content-free. Keep their identities so callers can preserve the
	// fail-closed decision and distinguish operator-actionable conditions.
	for _, safe := range []error{
		errOfficeReadUnavailable,
		errOfficeReadTimedOut,
		errOfficeReadSourceChanged,
		errOfficeReadFormatMismatch,
		errOfficeReadInputTooLarge,
		errOfficeReadOutputTooLarge,
		errOfficeReadUnsafeContainer,
		errOfficeReadEncryptedContainer,
		errOfficeReadExtractionFailed,
	} {
		if errors.Is(err, safe) {
			return safe
		}
	}
	return errOfficeReadExtractionFailed
}

// officeReadExtractFunc is a narrow test seam around the text-only adapter.
// Production keeps its default implementation, which snapshots the source
// before it reaches the path-only third-party API.
type officeReadExtractFunc func(filePath string) (text, format string, err error)

// OfficeReadObservation contains migration diagnostics only. It deliberately
// excludes document text, image data, and the source path; callers may attach
// their own request-scoped identifiers if operational correlation is needed.
type OfficeReadObservation struct {
	Format           string
	Engine           OfficeExtractEngine
	SourceBytes      int64
	Elapsed          time.Duration
	OfficeReadOK     bool
	OfficeReadSize   int
	OfficeReadTokens int
	LegacyOK         bool
	LegacySize       int
	LegacyTokens     int
	SharedTokens     int
	FallbackUsed     bool
	ErrorClass       string
}

// OfficeReadResourceSnapshot is an optional, content-free host diagnostic for
// a single extraction. It lets the GUI observe parser resource deltas during
// dual-read rollout without exposing source paths, document contents, images,
// or Go runtime details outside the host.
type OfficeReadResourceSnapshot struct {
	HeapAllocBytes uint64
	TotalAlloc     uint64
	SysBytes       uint64
	NumGC          uint32
}

// OfficeReadResourceObservation is emitted after each eligible OfficeRead
// extraction when a host installs a sampler. Values are aggregate process
// counters, not a hard per-document memory limit.
type OfficeReadResourceObservation struct {
	Format      string
	Engine      OfficeExtractEngine
	SourceBytes int64
	Elapsed     time.Duration
	Before      OfficeReadResourceSnapshot
	After       OfficeReadResourceSnapshot
}

var (
	officeReadObserveMu sync.RWMutex
	// officeReadObserve is optional so the core package has no logging policy
	// and no risk of writing document contents into diagnostics.
	officeReadObserve         func(OfficeReadObservation)
	officeReadResourceObserve func(OfficeReadResourceObservation)
	officeReadResourceSample  func() OfficeReadResourceSnapshot
)

// SetOfficeReadObservationHandler registers a migration diagnostic sink. Its
// payload deliberately excludes source paths, document text, image data and
// raw parser errors. The returned function restores the previous handler.
func SetOfficeReadObservationHandler(handler func(OfficeReadObservation)) func() {
	officeReadObserveMu.Lock()
	previous := officeReadObserve
	officeReadObserve = handler
	officeReadObserveMu.Unlock()
	return func() {
		officeReadObserveMu.Lock()
		officeReadObserve = previous
		officeReadObserveMu.Unlock()
	}
}

// SetOfficeReadResourceObserver registers optional, content-free resource
// diagnostics. The sampler and observer are installed together so the agent
// package remains independent of a particular host's runtime/logging policy.
func SetOfficeReadResourceObserver(sample func() OfficeReadResourceSnapshot, handler func(OfficeReadResourceObservation)) func() {
	officeReadObserveMu.Lock()
	previousSample := officeReadResourceSample
	previousHandler := officeReadResourceObserve
	officeReadResourceSample = sample
	officeReadResourceObserve = handler
	officeReadObserveMu.Unlock()
	return func() {
		officeReadObserveMu.Lock()
		officeReadResourceSample = previousSample
		officeReadResourceObserve = previousHandler
		officeReadObserveMu.Unlock()
	}
}

// officeReadExtract is nil in production. Focused package tests may install a
// text-only stub to model ordinary parser outcomes without manufacturing full
// Office fixtures; the production branch below keeps the source snapshot alive
// for both OfficeRead and a possible legacy fallback.
var officeReadExtract officeReadExtractFunc

// officeReadResultExtract keeps the direct third-party call behind the same
// boundary as the container preflight. Tests use it only to prove unsafe ZIPs
// do not reach OfficeRead.
var officeReadResultExtract = officeread.Extract

const (
	// OfficeRead has a synchronous public API without cancellation. Two calls
	// are enough to preserve interactive throughput while bounding memory and
	// goroutines when a malformed input makes the dependency stall. A timed-out
	// call keeps its slot until it actually exits, so the bound remains valid.
	maxConcurrentOfficeReadExtractions = 2
)

var (
	officeReadExtractionSlots   = make(chan struct{}, maxConcurrentOfficeReadExtractions)
	officeReadExtractionTimeout = 30 * time.Second
)

// officeReadPreflight is an internal seam used to verify that a rich-content
// request performs the shared container traversal once, before the signature
// consistency decision and parser call.
var officeReadPreflight = preflightOfficeReadContainer

// officeReadVersionToken identifies the exact source bytes accepted by a
// shared preflight. It intentionally uses a complete digest rather than just
// size/mtime: sync clients can replace a document while retaining both pieces
// of metadata.
type officeReadVersionToken struct {
	size   int64
	mod    time.Time
	digest [sha256.Size]byte
}

func officeReadSourceVersion(filePath string) (officeReadVersionToken, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return officeReadVersionToken{}, errOfficeReadUnsafeContainer
	}
	defer f.Close()
	before, err := f.Stat()
	if err != nil || before.IsDir() {
		return officeReadVersionToken{}, errOfficeReadUnsafeContainer
	}
	if before.Size() > MaxOfficeReadFileBytes {
		return officeReadVersionToken{}, errOfficeReadInputTooLarge
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, MaxOfficeReadFileBytes+1))
	if err != nil {
		return officeReadVersionToken{}, errOfficeReadUnsafeContainer
	}
	if n > MaxOfficeReadFileBytes {
		return officeReadVersionToken{}, errOfficeReadInputTooLarge
	}
	after, err := f.Stat()
	if err != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return officeReadVersionToken{}, errOfficeReadSourceChanged
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return officeReadVersionToken{size: after.Size(), mod: after.ModTime(), digest: digest}, nil
}

// snapshotOfficeReadSource copies the opened source descriptor into a private
// file for the third-party path-only API.  A version check immediately before
// OfficeRead.Extract is insufficient: OfficeRead reopens its path internally,
// so another process could replace the original after that check.  Parsing a
// private snapshot instead binds preflight and parsing to the same bytes.
//
// The returned path retains the original extension because OfficeRead selects
// portions of its legacy parsing behavior from filepath.Ext.  The caller owns
// removal of the returned snapshot, including the late-worker timeout case.
func snapshotOfficeReadSource(filePath string) (snapshot string, err error) {
	return snapshotBoundedDocumentSource(filePath)
}

// snapshotBoundedDocumentSource is the byte-identity boundary for every
// path-based document parser, including the legacy fallback and exported
// compatibility helpers.  They all reopen a filename internally, so merely
// preflighting the user-controlled pathname would leave a TOCTOU window.
// Keep only known document suffixes on the temporary file: parsers use the
// suffix for dispatch, while arbitrary caller-provided suffixes do not need
// to be propagated into the system temporary directory.
func snapshotBoundedDocumentSource(filePath string) (snapshot string, err error) {
	return snapshotBoundedDocumentSourceWithExtension(filePath, documentSnapshotExtension(filepath.Ext(filePath)))
}

// snapshotDocumentSourceBeforeFinalCheck is a narrow test seam around the
// final pathname-to-snapshot identity check. Production leaves it as a no-op.
var snapshotDocumentSourceBeforeFinalCheck = func(string) {}

// snapshotBoundedDocumentSourceWithExtension lets an extension/signature
// router preserve its original suffix while it probes the private bytes. The
// extension must come from the caller's already-normalized dispatch context,
// never from untrusted archive metadata.
func snapshotBoundedDocumentSourceWithExtension(filePath, ext string) (snapshot string, err error) {
	source, err := os.Open(filePath)
	if err != nil {
		return "", errOfficeReadUnsafeContainer
	}
	defer source.Close()

	before, err := source.Stat()
	if err != nil || before.IsDir() {
		return "", errOfficeReadUnsafeContainer
	}
	if before.Size() > MaxOfficeReadFileBytes {
		return "", errOfficeReadInputTooLarge
	}

	ext = documentSnapshotExtension(ext)
	temporary, err := os.CreateTemp("", "maclaw-officeread-*"+ext)
	if err != nil {
		return "", errOfficeReadUnsafeContainer
	}
	snapshot = temporary.Name()
	defer func() {
		if err != nil {
			_ = temporary.Close()
			_ = os.Remove(snapshot)
			snapshot = ""
		}
	}()

	hash := sha256.New()
	limited := io.LimitReader(source, MaxOfficeReadFileBytes+1)
	n, copyErr := io.Copy(io.MultiWriter(temporary, hash), limited)
	if closeErr := temporary.Close(); copyErr != nil || closeErr != nil {
		return "", errOfficeReadUnsafeContainer
	}
	if n > MaxOfficeReadFileBytes {
		return "", errOfficeReadInputTooLarge
	}
	after, statErr := source.Stat()
	if statErr != nil || before.Size() != after.Size() || !before.ModTime().Equal(after.ModTime()) {
		return "", errOfficeReadSourceChanged
	}
	var sourceDigest [sha256.Size]byte
	copy(sourceDigest[:], hash.Sum(nil))
	snapshotVersion, versionErr := snapshotDocumentVersion(snapshot)
	if versionErr != nil || snapshotVersion.size != n || snapshotVersion.digest != sourceDigest {
		// A successful copy is hashed and compared here deliberately.  It keeps
		// this routine as the byte-identity boundary and prevents a future
		// simplification from regressing to metadata-only checks.
		return "", errOfficeReadUnsafeContainer
	}
	// Release the original handle before the final pathname identity check.
	// Besides avoiding a needless descriptor while parsers consume the private
	// copy, this lets Windows report an atomic rename/replacement rather than
	// turning it into a sharing violation solely because this verifier kept the
	// old handle open.
	if err := source.Close(); err != nil {
		return "", errOfficeReadUnsafeContainer
	}
	snapshotDocumentSourceBeforeFinalCheck(filePath)
	if err := verifySnapshotSourceStillCurrent(filePath, before, n, sourceDigest); err != nil {
		return "", err
	}
	return snapshot, nil
}

// verifySnapshotSourceStillCurrent confirms that the pathname still denotes
// the exact bytes copied into a private snapshot. source.Stat only describes
// the already-open handle: it cannot detect an atomic replacement at the
// original pathname, and size/mtime can be deliberately restored after an
// in-place rewrite. Re-read the bounded source digest after copying so no
// path-based parser is authorized by a snapshot of an earlier version.
func verifySnapshotSourceStillCurrent(filePath string, opened os.FileInfo, expectedSize int64, expectedDigest [sha256.Size]byte) error {
	current, err := os.Stat(filePath)
	if err != nil || current.IsDir() || !os.SameFile(opened, current) {
		return errOfficeReadSourceChanged
	}
	version, err := officeReadSourceVersion(filePath)
	if err != nil {
		// The source was accepted and copied already. Any inability to establish
		// one stable, bounded version at this final check is a version failure,
		// not permission to parse the earlier snapshot.
		return errOfficeReadSourceChanged
	}
	if version.size != expectedSize || version.digest != expectedDigest {
		return errOfficeReadSourceChanged
	}
	return nil
}

// snapshotDocumentVersion hashes a private snapshot without requiring an
// Office suffix. The public version helper intentionally uses the stricter
// Office format contract; this lower-level verification is also used for
// text-seam tests and for signature-routed unknown extensions.
func snapshotDocumentVersion(filePath string) (officeReadVersionToken, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return officeReadVersionToken{}, errOfficeReadUnsafeContainer
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() || info.Size() > MaxOfficeReadFileBytes {
		return officeReadVersionToken{}, errOfficeReadUnsafeContainer
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(f, MaxOfficeReadFileBytes+1))
	if err != nil || n > MaxOfficeReadFileBytes {
		return officeReadVersionToken{}, errOfficeReadUnsafeContainer
	}
	var digest [sha256.Size]byte
	copy(digest[:], hash.Sum(nil))
	return officeReadVersionToken{size: n, mod: info.ModTime(), digest: digest}, nil
}

// documentSnapshotExtension returns a parser-relevant suffix for the temporary
// copy. Unknown suffixes stay suffix-free, but callers that need content
// sniffing can preserve the original extension through the explicit variant.
func documentSnapshotExtension(value string) string {
	ext := "." + strings.TrimPrefix(strings.ToLower(strings.TrimSpace(value)), ".")
	switch strings.TrimPrefix(ext, ".") {
	case "doc", "docx", "xls", "xlsx", "ppt", "pptx", "pdf", "csv", "txt", "text", "md", "markdown":
		// Preserve only parser-relevant suffixes.
	default:
		ext = ""
	}
	return ext
}

func preflightOfficeReadSource(filePath, format string) (officeReadVersionToken, error) {
	version, err := officeReadSourceVersion(filePath)
	if err != nil {
		return officeReadVersionToken{}, err
	}
	if err := officeReadPreflight(filePath, format); err != nil {
		return officeReadVersionToken{}, err
	}
	verified, err := officeReadSourceVersion(filePath)
	if err != nil {
		return officeReadVersionToken{}, err
	}
	if verified != version {
		return officeReadVersionToken{}, errOfficeReadSourceChanged
	}
	return version, nil
}

func verifyOfficeReadSourceVersion(filePath string, expected officeReadVersionToken) error {
	actual, err := officeReadSourceVersion(filePath)
	if err != nil {
		return err
	}
	if actual != expected {
		return errOfficeReadSourceChanged
	}
	return nil
}

// OfficeReadConfig is the persisted host policy supplied by the GUI. The
// agent package intentionally owns this small value type so hosts do not need
// to import OfficeRead or leak its API through their configuration model.
// A nil Fallback means the migration-safe default (true).
type OfficeReadConfig struct {
	Engine       string
	Formats      []string
	Fallback     *bool
	EmitMarkdown *bool
}

// CloneOfficeReadConfig returns an independent, immutable-by-convention policy
// snapshot. Hosts pass these policies across asynchronous import and refresh
// boundaries, so copying pointer-backed booleans as well as the format slice is
// required: a later configuration mutation must not change an extraction that
// has already begun.
func CloneOfficeReadConfig(config OfficeReadConfig) OfficeReadConfig {
	clone := OfficeReadConfig{
		Engine:  config.Engine,
		Formats: append([]string(nil), config.Formats...),
	}
	if config.Fallback != nil {
		value := *config.Fallback
		clone.Fallback = &value
	}
	if config.EmitMarkdown != nil {
		value := *config.EmitMarkdown
		clone.EmitMarkdown = &value
	}
	return clone
}

// CloneOfficeReadConfigPtr returns nil for nil input and otherwise an
// independent policy snapshot. It is useful for request structs whose nil
// value deliberately preserves the process-level desktop provider behavior.
func CloneOfficeReadConfigPtr(config *OfficeReadConfig) *OfficeReadConfig {
	if config == nil {
		return nil
	}
	clone := CloneOfficeReadConfig(*config)
	return &clone
}

// OfficeReadRuntimePolicy is the fully resolved, content-free extractor
// policy after persisted host configuration and operational environment
// overrides have been applied. It is intended for rollout tooling which must
// prove that the policy it reports is the same policy used by extraction.
// Formats is a canonical, sorted copy and may be empty when a non-empty
// configured allowlist is malformed (the extractor fails closed in that case).
type OfficeReadRuntimePolicy struct {
	Engine       OfficeExtractEngine
	Formats      []string
	Fallback     bool
	EmitMarkdown bool
}

// OfficeReadImage is an image emitted by OfficeRead for a controlled consumer
// such as the knowledge-base importer. It is deliberately not used by the
// chat attachment or read_document paths.
type OfficeReadImage struct {
	Name string
	Alt  string
	Ext  string
	Data []byte
}

// OfficeReadRichContent is the opt-in structured part of an OfficeRead
// extraction. It deliberately has no source path or OfficeRead result object,
// so consumers cannot accidentally use parser options or leak parser state.
type OfficeReadRichContent struct {
	Format   string
	Markdown string
	Images   []OfficeReadImage
}

// OfficeReadConfigProvider reads the current host configuration. It is called
// at extraction time, allowing a persisted setting change to take effect
// immediately without rebuilding the tool registry.
type OfficeReadConfigProvider func() OfficeReadConfig

var (
	officeReadConfigProviderMu sync.RWMutex
	officeReadConfigProvider   OfficeReadConfigProvider
)

// SetOfficeReadConfigProvider installs the host's persisted configuration
// provider and returns a restore function useful for lifecycle ownership and
// focused tests. Environment variables always take precedence over it.
func SetOfficeReadConfigProvider(provider OfficeReadConfigProvider) func() {
	officeReadConfigProviderMu.Lock()
	previous := officeReadConfigProvider
	officeReadConfigProvider = provider
	officeReadConfigProviderMu.Unlock()
	return func() {
		officeReadConfigProviderMu.Lock()
		officeReadConfigProvider = previous
		officeReadConfigProviderMu.Unlock()
	}
}

func readOfficeReadConfig() OfficeReadConfig {
	officeReadConfigProviderMu.RLock()
	provider := officeReadConfigProvider
	officeReadConfigProviderMu.RUnlock()
	if provider == nil {
		return OfficeReadConfig{}
	}
	// The persisted policy provider belongs to the host (normally the GUI).
	// It is intentionally outside extraction's correctness boundary: a broken
	// config read must not turn opening a user-selected document into a process
	// panic. Fall back to the conservative built-in policy, while environment
	// variables continue to provide the emergency kill switch.
	defer func() {
		_ = recover()
	}()
	return CloneOfficeReadConfig(provider())
}

const (
	// OfficeRead loads the source container before parsing it.  The input-size
	// ceiling alone therefore does not bound a highly-compressible OOXML ZIP.
	// These limits apply before OfficeRead (and before an OfficeRead-triggered
	// legacy fallback) can inflate any part.
	maxOfficeReadZIPEntries             = 4096
	maxOfficeReadZIPEntryBytes    int64 = 32 * 1024 * 1024
	maxOfficeReadZIPExpandedBytes       = 96 * 1024 * 1024
	maxOfficeReadZIPPathDepth           = 64
	// FILEPASS appears in the early BIFF workbook record sequence. Limiting the
	// check to this prefix avoids turning a directory-only OLE preflight into an
	// unbounded stream read while still covering the defined encryption marker.
	maxOfficeReadBIFFEncryptionPrefixBytes int64 = 1 * 1024 * 1024
	// The OLE reader needs to allocate directory/FAT bookkeeping from header
	// counts. Bound those counts by the actual 32 MiB input size before giving
	// it the file so forged uint32 values cannot cause disproportionate
	// allocation during a preflight intended to be defensive.
	minOfficeReadOLEHeaderBytes int64 = 512
	// mscfb.New walks CFBF metadata through random-access reads. Header bounds
	// alone do not bound the amount of I/O caused by a malicious directory or
	// mini-stream chain, so the preflight has a separate budget. The limits are
	// intentionally far above ordinary Office metadata, but finite: this is a
	// container classifier, not a full document reader.
	maxOfficeReadOLEPreflightReadBytes    int64 = 48 * 1024 * 1024
	maxOfficeReadOLEPreflightReadRequests       = 128 * 1024
	// In CFBF version 3 each full-sector read made by mscfb.New can append
	// four directory entries. Limiting these reads bounds directory objects
	// even when the header's directory-sector count is reserved as zero.
	maxOfficeReadOLEPreflightSectorReads = 8 * 1024
)

var errOfficeReadPreflightBudgetExceeded = errors.New("OfficeRead OLE preflight read budget exceeded")

// officeReadPreflightReaderAt places an independent upper bound on the
// random reads made while mscfb constructs its directory view. It accounts
// requested bytes (rather than bytes successfully returned) so a short or
// failing underlying read cannot be retried without spending budget.
//
// It is intentionally used only for preflight. OfficeRead and legacy parsers
// receive their normal file handles only after the container has been allowed.
type officeReadPreflightReaderAt struct {
	reader          io.ReaderAt
	maxBytes        int64
	maxRequests     int
	maxSectorReads  int
	readBytes       int64
	readRequests    int
	readSectorReads int
}

func (r *officeReadPreflightReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	if offset < 0 {
		return 0, errOfficeReadPreflightBudgetExceeded
	}
	if len(p) == 0 {
		return 0, nil
	}
	requestBytes := int64(len(p))
	if r.readRequests >= r.maxRequests || requestBytes > r.maxBytes-r.readBytes {
		return 0, errOfficeReadPreflightBudgetExceeded
	}
	if requestBytes >= minOfficeReadOLEHeaderBytes && r.readSectorReads >= r.maxSectorReads {
		return 0, errOfficeReadPreflightBudgetExceeded
	}
	r.readRequests++
	r.readBytes += requestBytes
	if requestBytes >= minOfficeReadOLEHeaderBytes {
		r.readSectorReads++
	}
	return r.reader.ReadAt(p, offset)
}

// extractOfficeReadResult is the normal production call into OfficeRead. Keep
// its preflight and panic containment at this boundary so text-only consumers
// receive the shared handling of malformed containers.
func extractOfficeReadResult(filePath string) (result *officeread.Result, err error) {
	snapshot, err := snapshotOfficeReadSource(filePath)
	if err != nil {
		return nil, err
	}
	return extractOfficeReadSnapshotResult(snapshot, officeReadFormat(filePath))
}

// extractOfficeReadSnapshotResult owns a snapshot whose pathname is private to
// this adapter.  It performs the container decision on those exact bytes and
// transfers deletion to the parser worker, so a response timeout cannot remove
// a file that a late OfficeRead call is still reading.
func extractOfficeReadSnapshotResult(snapshot, format string) (*officeread.Result, error) {
	version, err := preflightOfficeReadSource(snapshot, format)
	if err != nil {
		_ = os.Remove(snapshot)
		return nil, err
	}
	return extractOfficeReadSnapshotResultAfterPreflight(snapshot, version)
}

// extractOfficeReadSnapshotResultAfterPreflight consumes a previously
// preflighted private snapshot.  Unlike the original user path, this pathname
// cannot be switched by a rename between the version check and OfficeRead's
// internal os.ReadFile call.
func extractOfficeReadSnapshotResultAfterPreflight(snapshot string, version officeReadVersionToken) (*officeread.Result, error) {
	if err := verifyOfficeReadSourceVersion(snapshot, version); err != nil {
		_ = os.Remove(snapshot)
		return nil, err
	}
	return extractOfficeReadResultBoundedWithCleanup(snapshot, func() { _ = os.Remove(snapshot) })
}

type officeReadResultCall struct {
	result *officeread.Result
	err    error
}

// extractOfficeReadResultBounded gives a synchronous, context-less third-party
// parser a finite host-side response budget. The slot belongs to the worker,
// not the caller: a timed-out call remains counted until it really returns.
// That avoids the common but unsafe "spawn and forget" timeout pattern where
// repeated malformed documents create unbounded goroutines and allocations.
func extractOfficeReadResultBounded(filePath string) (*officeread.Result, error) {
	return extractOfficeReadResultBoundedWithCleanup(filePath, nil)
}

func extractOfficeReadResultBoundedWithCleanup(filePath string, cleanup func()) (*officeread.Result, error) {
	// Capture the currently configured gate before starting the worker. Runtime
	// policy is deliberately static in production; capturing also means a test
	// or future host reconfiguration cannot make a late worker release a newer
	// gate than the one it acquired.
	slots := officeReadExtractionSlots
	timeout := officeReadExtractionTimeout
	select {
	case slots <- struct{}{}:
	case <-time.After(timeout):
		// No worker owns this snapshot when admission itself times out.
		if cleanup != nil {
			cleanup()
		}
		return nil, errOfficeReadTimedOut
	}

	done := make(chan officeReadResultCall, 1)
	go func() {
		defer func() { <-slots }()
		result, err := safeOfficeReadResultExtract(filePath)
		// A synchronous caller can immediately inspect cleanup-sensitive state
		// after receiving its result. Remove the private snapshot before that
		// notification, while a timed-out caller still returns before this point
		// and therefore leaves cleanup correctly owned by the late worker.
		if cleanup != nil {
			cleanup()
		}
		done <- officeReadResultCall{result: result, err: err}
	}()

	select {
	case call := <-done:
		return call.result, call.err
	case <-time.After(timeout):
		return nil, errOfficeReadTimedOut
	}
}

// safeOfficeReadResultExtract keeps the panic boundary inside the worker. The
// outer caller can time out before the dependency returns, so recovering only
// in the caller goroutine would leave a later parser panic able to terminate
// the process.
func safeOfficeReadResultExtract(filePath string) (result *officeread.Result, err error) {
	defer func() {
		if recover() != nil {
			result = nil
			err = errOfficeReadUnsafeContainer
		}
	}()
	return officeReadResultExtract(filePath, officeread.Options{})
}

// preflightOfficeReadContainer validates OOXML central-directory metadata
// without inflating entries. It detects ZIP from the actual file signature so
// a mislabelled OOXML file cannot bypass the check. For actual legacy OLE
// containers it also validates the compound-file directory and rejects the
// explicit encrypted-package markers that can be identified without reading
// document streams. This is intentionally not a claim to detect every legacy
// Office encryption scheme (for example, an XLS FILEPASS record).
func preflightOfficeReadContainer(filePath, format string) error {
	expectedOOXML := false
	switch strings.ToLower(strings.TrimPrefix(format, ".")) {
	case "docx", "xlsx", "pptx":
		expectedOOXML = true
	}

	f, err := os.Open(filePath)
	if err != nil {
		return errOfficeReadUnsafeContainer
	}
	header := make([]byte, 8)
	_, readErr := io.ReadFull(f, header)
	closeErr := f.Close()
	if readErr != nil && !errors.Is(readErr, io.ErrUnexpectedEOF) {
		return errOfficeReadUnsafeContainer
	}
	if closeErr != nil {
		return errOfficeReadUnsafeContainer
	}
	isZIP := len(header) >= 4 && header[0] == 'P' && header[1] == 'K' && (header[2] == 3 || header[2] == 5 || header[2] == 7) && (header[3] == 4 || header[3] == 6 || header[3] == 8)
	isOLE := len(header) == 8 && string(header) == "\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"
	// PDF is not an Office container and continues through ExtractOfficeText's
	// established signature routing. In particular, a PDF saved with a .doc
	// suffix must reach the existing GoPDF2 route rather than being reported as
	// a malformed legacy Office container. This exception is deliberately
	// signature-specific: arbitrary bytes with a legacy Office suffix remain
	// fail-closed below.
	isPDF := len(header) >= 4 && string(header[:4]) == "%PDF"
	if isOLE {
		return preflightOfficeReadOLE(filePath, format)
	}
	if !expectedOOXML && !isZIP && !isPDF {
		// A declared legacy Office format must be a real compound-file
		// container.  OfficeRead otherwise accepts arbitrary bytes for some
		// legacy suffixes as plain text, which would turn a corrupt `.doc` into
		// a seemingly successful document read after the six-format rollout.
		// Keep extension-mismatch support: an actual ZIP above remains eligible
		// for OOXML signature routing, while callers that pass an empty format
		// (the generic container probe) have already established a container
		// signature before reaching this branch.
		if isOfficeReadFormat(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")) {
			return errOfficeReadUnsafeContainer
		}
		return nil
	}

	reader, err := zip.OpenReader(filePath)
	if err != nil {
		return errOfficeReadUnsafeContainer
	}
	defer reader.Close()
	if len(reader.File) > maxOfficeReadZIPEntries {
		return errOfficeReadUnsafeContainer
	}

	seen := make(map[string]struct{}, len(reader.File))
	// A normal Office Open XML package has one top-level document family.  A
	// ZIP containing two or three primary families is ambiguous: OfficeRead's
	// internal part map is intentionally format-agnostic, so accepting it
	// would make the selected parser depend on container iteration instead of
	// the user's file. Embedded documents live below the owning family's
	// directory (for example word/embeddings/) and do not trip this check.
	primaryFamilies := make(map[string]struct{}, 1)
	// The family directory alone is not sufficient evidence of an Office
	// package: arbitrary ZIPs can contain a word/, xl/, or ppt/ folder. Keep a
	// separate record of the required main document part for the selected
	// family, so those lookalikes fail before either parser can open them.
	primaryDocumentParts := make(map[string]bool, 1)
	var expanded int64
	for _, entry := range reader.File {
		name := strings.ReplaceAll(entry.Name, "\\", "/")
		isDir := strings.HasSuffix(name, "/")
		clean := path.Clean(strings.TrimSuffix(name, "/"))
		if name == "" || clean == "." || strings.HasPrefix(clean, "/") || clean != strings.TrimSuffix(name, "/") || strings.Count(clean, "/") > maxOfficeReadZIPPathDepth {
			return errOfficeReadUnsafeContainer
		}
		key := strings.ToLower(clean) + ":file"
		if isDir {
			key = strings.ToLower(clean) + ":dir"
		}
		if _, duplicate := seen[key]; duplicate {
			return errOfficeReadUnsafeContainer
		}
		seen[key] = struct{}{}
		if family := officeReadOOXMLPrimaryFamily(clean); family != "" {
			primaryFamilies[family] = struct{}{}
			if len(primaryFamilies) > 1 {
				return errOfficeReadUnsafeContainer
			}
			if officeReadOOXMLMainDocumentPart(clean, family) {
				primaryDocumentParts[family] = true
			}
		}
		if entry.Flags&0x1 != 0 {
			return errOfficeReadEncryptedContainer
		}
		if entry.UncompressedSize64 > uint64(maxOfficeReadZIPEntryBytes) {
			return errOfficeReadUnsafeContainer
		}
		if entry.UncompressedSize64 > uint64(maxOfficeReadZIPExpandedBytes-expanded) {
			return errOfficeReadUnsafeContainer
		}
		expanded += int64(entry.UncompressedSize64)
	}
	// A ZIP signature, or even an Office-looking top-level directory, does not
	// make a file an Office document. In particular, legacy-looking names such
	// as "report.doc" are otherwise able to carry an arbitrary ZIP through the
	// legacy-format branch and into OfficeRead. Every supported OOXML family has
	// a required primary document part, so reject packages without exactly that
	// part after checking every entry for encryption first (an encrypted package
	// should retain its more actionable error class).
	if len(primaryFamilies) != 1 {
		return errOfficeReadUnsafeContainer
	}
	for family := range primaryFamilies {
		// A caller that has explicitly selected an OOXML parser (including
		// SnapshotOfficeReadInput users such as structured read_excel/read_pptx
		// and knowledge import) must not be handed a different OOXML family.
		// The generic container probe intentionally has no expected family: it
		// only establishes that a later signature router may inspect the same
		// safe package.  Legacy formats remain extension-led because their OLE
		// signature cannot identify a Word/Excel/PowerPoint family.
		if expectedOOXML && family != officeReadExpectedOOXMLFamily(format) {
			return errOfficeReadFormatMismatch
		}
		if !primaryDocumentParts[family] {
			return errOfficeReadUnsafeContainer
		}
	}
	return nil
}

func officeReadExpectedOOXMLFamily(format string) string {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), ".")) {
	case "docx":
		return "word"
	case "xlsx":
		return "xl"
	case "pptx":
		return "ppt"
	default:
		return ""
	}
}

func officeReadOOXMLPrimaryFamily(cleanName string) string {
	cleanName = strings.ToLower(strings.TrimPrefix(cleanName, "./"))
	for _, family := range []string{"word", "xl", "ppt"} {
		if strings.HasPrefix(cleanName, family+"/") {
			return family
		}
	}
	return ""
}

func officeReadOOXMLMainDocumentPart(cleanName, family string) bool {
	cleanName = strings.ToLower(strings.TrimPrefix(cleanName, "./"))
	switch family {
	case "word":
		return cleanName == "word/document.xml"
	case "xl":
		return cleanName == "xl/workbook.xml"
	case "ppt":
		return cleanName == "ppt/presentation.xml"
	default:
		return false
	}
}

// preflightOfficeReadOLE opens only the CFBF directory. mscfb.New verifies
// header, FAT, directory and traversal consistency without reading a document
// stream. Any failure is an unsafe container: the caller must not then reopen
// the same bytes through a legacy fallback.
func preflightOfficeReadOLE(filePath, format string) (err error) {
	defer func() {
		if recover() != nil {
			err = errOfficeReadUnsafeContainer
		}
	}()
	f, err := os.Open(filePath)
	if err != nil {
		return errOfficeReadUnsafeContainer
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !validateOfficeReadOLEHeaderBounds(f, info.Size()) {
		return errOfficeReadUnsafeContainer
	}
	limitedReader := &officeReadPreflightReaderAt{
		reader:         f,
		maxBytes:       maxOfficeReadOLEPreflightReadBytes,
		maxRequests:    maxOfficeReadOLEPreflightReadRequests,
		maxSectorReads: maxOfficeReadOLEPreflightSectorReads,
	}
	doc, err := mscfb.New(limitedReader)
	if err != nil {
		return errOfficeReadUnsafeContainer
	}

	hasEncryptedPackage := false
	hasEncryptionMetadata := false
	// A legacy OLE magic number by itself cannot name a document family, so
	// generic ExtractOfficeText intentionally remains extension-led. Explicit
	// DOC/XLS/PPT callers, however, must not hand a container with a definitive
	// top-level application stream to a different parser. Track only reliable,
	// mutually exclusive stream names; generic Compound Files without one keep
	// their compatibility behavior.
	detectedFamily := ""
	for _, entry := range doc.File {
		if entry == nil || entry.FileInfo().IsDir() {
			continue
		}
		// An embedded OLE object has an independent set of document and
		// encryption streams. Those signals do not encrypt or otherwise change
		// the outer document, whose parser will decide how to handle the embedded
		// payload. This preflight governs only the outer CFBF container, so all
		// document-family and encryption checks below are root-level only.
		if len(entry.Path) != 0 {
			continue
		}
		name := strings.ToLower(strings.Join(append(append([]string(nil), entry.Path...), entry.Name), "/"))
		if strings.Contains(name, "encryptedpackage") {
			hasEncryptedPackage = true
		}
		if strings.Contains(name, "encryptioninfo") ||
			strings.Contains(name, "dataspaces") ||
			strings.Contains(name, "strongencryption") ||
			strings.Contains(name, "encryptiontransform") {
			hasEncryptionMetadata = true
		}
		// The EncryptedSummary stream is a defined container-level signal for
		// RC4-protected legacy PowerPoint files. Unlike content heuristics, it
		// is available from the directory alone.
		if strings.EqualFold(entry.Name, "EncryptedSummary") {
			return errOfficeReadEncryptedContainer
		}
		if family := officeReadOLEDocumentFamily(entry.Name); family != "" {
			if detectedFamily != "" && detectedFamily != family {
				return errOfficeReadUnsafeContainer
			}
			detectedFamily = family
		}
		if strings.EqualFold(entry.Name, "Workbook") || strings.EqualFold(entry.Name, "Book") {
			hasFilePass, readErr := oleBIFFPrefixHasFilePass(entry)
			if readErr != nil {
				// A directory can be valid while the declared Workbook stream
				// points at a broken chain. Do not turn that stream-read failure
				// into a false "not encrypted" result and then hand the same OLE
				// container to another parser.
				return errOfficeReadUnsafeContainer
			}
			if hasFilePass {
				return errOfficeReadEncryptedContainer
			}
		}
		if strings.EqualFold(entry.Name, "WordDocument") {
			encrypted, readErr := oleWordPrefixEncrypted(entry)
			if readErr != nil {
				return errOfficeReadUnsafeContainer
			}
			if encrypted {
				return errOfficeReadEncryptedContainer
			}
		}
	}
	if hasEncryptedPackage && hasEncryptionMetadata {
		return errOfficeReadEncryptedContainer
	}
	if expectedFamily := officeReadExpectedOLEFamily(format); expectedFamily != "" && detectedFamily != "" && detectedFamily != expectedFamily {
		return errOfficeReadFormatMismatch
	}
	return nil
}

func officeReadExpectedOLEFamily(format string) string {
	switch strings.ToLower(strings.TrimPrefix(strings.TrimSpace(format), ".")) {
	case "doc":
		return "word"
	case "xls":
		return "excel"
	case "ppt":
		return "powerpoint"
	default:
		return ""
	}
}

func officeReadOLEDocumentFamily(streamName string) string {
	switch {
	case strings.EqualFold(streamName, "WordDocument"):
		return "word"
	case strings.EqualFold(streamName, "Workbook"), strings.EqualFold(streamName, "Book"):
		return "excel"
	case strings.EqualFold(streamName, "PowerPoint Document"):
		return "powerpoint"
	default:
		return ""
	}
}

func oleBIFFPrefixHasFilePass(entry *mscfb.File) (bool, error) {
	if entry == nil || entry.Size <= 0 {
		return false, nil
	}
	limit := entry.Size
	if limit > maxOfficeReadBIFFEncryptionPrefixBytes {
		limit = maxOfficeReadBIFFEncryptionPrefixBytes
	}
	// The preflight limits source files to 32 MiB at every production OfficeRead
	// entry point. Still guard the conversion so malformed version-4 sizes can
	// never influence allocation through a negative or overflowing int.
	if limit <= 0 || limit > int64(int(^uint(0)>>1)) {
		return false, errOfficeReadUnsafeContainer
	}
	data, err := io.ReadAll(io.LimitReader(entry, limit))
	if err != nil {
		return false, err
	}
	return biffPrefixHasFilePass(data), nil
}

// biffPrefixHasFilePass parses only complete BIFF records in a bounded prefix.
// A truncated or malformed tail is deliberately not inferred as encryption;
// the OLE directory and parser containment still apply to that input.
func biffPrefixHasFilePass(data []byte) bool {
	for offset := 0; offset+4 <= len(data); {
		recordID := binary.LittleEndian.Uint16(data[offset:])
		recordSize := int(binary.LittleEndian.Uint16(data[offset+2:]))
		offset += 4
		if recordSize > len(data)-offset {
			return false
		}
		if recordID == 0x002f { // FILEPASS
			return true
		}
		offset += recordSize
	}
	return false
}

// oleWordPrefixEncrypted recognizes the fEncrypted bit in the 32-byte FIB
// base carried at the beginning of a legacy WordDocument stream. The check is
// gated by the FIB identifier so arbitrary OLE stream bytes cannot be
// classified as an encrypted Word document based on a coincidental bit.
func oleWordPrefixEncrypted(entry *mscfb.File) (bool, error) {
	const fibBaseBytes = 32
	if entry == nil || entry.Size == 0 {
		return false, nil
	}
	if entry.Size < fibBaseBytes {
		return false, errOfficeReadUnsafeContainer
	}
	header := make([]byte, fibBaseBytes)
	if _, err := io.ReadFull(io.LimitReader(entry, fibBaseBytes), header); err != nil {
		return false, err
	}
	return binary.LittleEndian.Uint16(header[0:2]) == 0xa5ec &&
		binary.LittleEndian.Uint16(header[10:12])&0x0100 != 0, nil
}

func validateOfficeReadOLEHeaderBounds(file *os.File, fileSize int64) bool {
	if fileSize < minOfficeReadOLEHeaderBytes {
		return false
	}
	header := make([]byte, minOfficeReadOLEHeaderBytes)
	if _, err := file.ReadAt(header, 0); err != nil {
		return false
	}
	majorVersion := binary.LittleEndian.Uint16(header[26:28])
	sectorShift := binary.LittleEndian.Uint16(header[30:32])
	if (majorVersion != 3 && majorVersion != 4) || (sectorShift != 9 && sectorShift != 12) {
		return false
	}
	sectorSize := int64(1) << sectorShift
	if fileSize < minOfficeReadOLEHeaderBytes+sectorSize {
		return false
	}
	availableSectors := uint64((fileSize - minOfficeReadOLEHeaderBytes) / sectorSize)
	if availableSectors == 0 {
		return false
	}
	// These are the header counts that mscfb may use to size slices before it
	// has walked every referenced sector. Their upper bound must come from the
	// physical file, never from attacker-controlled metadata alone.
	for _, offset := range []int{40, 44, 64, 72} {
		if uint64(binary.LittleEndian.Uint32(header[offset:offset+4])) > availableSectors {
			return false
		}
	}
	if majorVersion == 3 && binary.LittleEndian.Uint32(header[40:44]) != 0 {
		return false
	}
	return true
}

func officeReadFormat(filePath string) string {
	return strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), ".")
}

// officeReadSettings merges the persisted host policy with operational
// environment overrides. The environment is deliberately highest priority:
// it provides an immediate global kill switch even if the GUI cannot start.
//
//	MACLAW_OFFICE_READ_ENGINE=legacy|dual|officeread
//	MACLAW_OFFICE_READ_FORMATS=.ppt,.doc,.xls
//	MACLAW_OFFICE_READ_FALLBACK=true|false
//
// With no environment override, every supported Office format is enabled.
// An explicit engine of legacy remains the global kill switch, and an
// explicit non-empty allowlist remains the narrow, format-level rollback.
type officeReadSettings struct {
	engine       OfficeExtractEngine
	formats      map[string]struct{}
	fallback     bool
	emitMarkdown bool
}

func currentOfficeReadSettings() officeReadSettings {
	return officeReadSettingsForConfig(readOfficeReadConfig())
}

// officeReadSettingsForConfig resolves an explicit host policy using the same
// environment overrides as the default provider-backed path. It is kept
// package-private because callers must use the typed per-request APIs rather
// than pass parser settings through untrusted tool arguments.
func officeReadSettingsForConfig(config OfficeReadConfig) officeReadSettings {
	rawEngine := strings.ToLower(strings.TrimSpace(config.Engine))
	if raw, ok := os.LookupEnv("MACLAW_OFFICE_READ_ENGINE"); ok && strings.TrimSpace(raw) != "" {
		rawEngine = strings.ToLower(strings.TrimSpace(raw))
	}
	engine := OfficeExtractEngine(rawEngine)
	switch engine {
	case OfficeExtractEngineDual, OfficeExtractEngineOfficeRead:
	default:
		if rawEngine == "" {
			engine = OfficeExtractEngineOfficeRead
		} else {
			engine = OfficeExtractEngineLegacy
		}
	}

	formats := make(map[string]struct{})
	configuredFormats := config.Formats
	if rawFormats, ok := os.LookupEnv("MACLAW_OFFICE_READ_FORMATS"); ok && strings.TrimSpace(rawFormats) != "" {
		configuredFormats = strings.Split(rawFormats, ",")
	}
	if len(configuredFormats) == 0 {
		for _, format := range []string{"doc", "docx", "ppt", "pptx", "xls", "xlsx"} {
			formats[format] = struct{}{}
		}
	} else {
		// The configured allowlist is a promotion boundary. Ignoring one bad
		// value while accepting the rest could silently enable a format after a
		// typo or externally edited config. GUI writes are validated, but the
		// environment and persisted file are operational inputs, so parse the
		// whole non-empty list fail-closed instead.
		validFormats := make(map[string]struct{}, len(configuredFormats))
		valid := true
		for _, item := range configuredFormats {
			format := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(item)), ".")
			if format == "" || !isOfficeReadFormat(format) {
				valid = false
				break
			}
			validFormats[format] = struct{}{}
		}
		if valid {
			formats = validFormats
		}
	}

	fallback := true
	if config.Fallback != nil {
		fallback = *config.Fallback
	}
	if raw, ok := os.LookupEnv("MACLAW_OFFICE_READ_FALLBACK"); ok {
		if value, valid := parseOfficeReadBooleanOverride(raw); valid {
			fallback = value
		}
	}
	emitMarkdown := false
	if config.EmitMarkdown != nil {
		emitMarkdown = *config.EmitMarkdown
	}
	if raw, ok := os.LookupEnv("MACLAW_OFFICE_READ_EMIT_MARKDOWN"); ok {
		if value, valid := parseOfficeReadBooleanOverride(raw); valid {
			emitMarkdown = value
		}
	}
	return officeReadSettings{engine: engine, formats: formats, fallback: fallback, emitMarkdown: emitMarkdown}
}

// CurrentOfficeReadRuntimePolicy returns the same effective policy snapshot
// used by the next extraction. It exposes no source, document, or parser
// state, so callers such as the dual-read evidence tool can bind a report to
// the active rollout scope without duplicating policy parsing.
func CurrentOfficeReadRuntimePolicy() OfficeReadRuntimePolicy {
	settings := currentOfficeReadSettings()
	formats := make([]string, 0, len(settings.formats))
	for format := range settings.formats {
		formats = append(formats, format)
	}
	sort.Strings(formats)
	return OfficeReadRuntimePolicy{
		Engine:       settings.engine,
		Formats:      formats,
		Fallback:     settings.fallback,
		EmitMarkdown: settings.emitMarkdown,
	}
}

// parseOfficeReadBooleanOverride accepts only explicit true/false operational
// values. Environment variables can be edited outside the GUI; treating an
// arbitrary non-empty value as true could silently enable rich Markdown/image
// consumption before a format has cleared its promotion gates. Invalid or
// empty overrides therefore leave the persisted policy intact.
func parseOfficeReadBooleanOverride(raw string) (value, valid bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "true", "1":
		return true, true
	case "false", "0":
		return false, true
	default:
		return false, false
	}
}

func isOfficeReadFormat(format string) bool {
	switch strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".") {
	case "doc", "docx", "xls", "xlsx", "ppt", "pptx":
		return true
	default:
		return false
	}
}

// OfficeReadRichContentEnabledForFormat reports whether the explicit
// knowledge/preview rich-content rollout is active for one Office format. It
// deliberately requires the OfficeRead primary engine, not merely a dual-read
// shadow. In dual mode legacy text/nodes remain authoritative by contract;
// allowing StructuredMarkdown or image assets to enter knowledge storage there
// would silently make the shadow extractor user-visible before its format has
// cleared the promotion gates. It exposes policy only; no parser result, file
// path, or document data crosses this package boundary.
func OfficeReadRichContentEnabledForFormat(format string) bool {
	settings := currentOfficeReadSettings()
	return settings.richContentEnabledFor(format)
}

// OfficeReadRichContentEnabledForFormatWithOfficeReadConfig reports whether
// rich Office content is enabled under an explicit trusted host policy. It is
// intended for request-scoped hosts; callers must never derive config from
// model-controlled tool arguments.
func OfficeReadRichContentEnabledForFormatWithOfficeReadConfig(format string, config OfficeReadConfig) bool {
	return officeReadSettingsForConfig(config).richContentEnabledFor(format)
}

// richContentEnabledFor evaluates the rich-output policy from one settings
// snapshot.
func (s officeReadSettings) richContentEnabledFor(format string) bool {
	return s.emitMarkdown && s.engine == OfficeExtractEngineOfficeRead && s.enabledFor(format)
}

func (s officeReadSettings) enabledFor(format string) bool {
	if s.engine == OfficeExtractEngineLegacy || !isOfficeReadFormat(format) {
		return false
	}
	_, ok := s.formats[strings.TrimPrefix(strings.ToLower(format), ".")]
	return ok
}

// officeReadCacheKeySuffix prevents a result produced with one engine setting
// being returned after the operator switches engine or format flags.
func officeReadCacheKeySuffix() string {
	return officeReadCacheKeySuffixForSettings(currentOfficeReadSettings())
}

func officeReadCacheKeySuffixForSettings(s officeReadSettings) string {
	formats := make([]string, 0, len(s.formats))
	for format := range s.formats {
		formats = append(formats, format)
	}
	sort.Strings(formats)
	return string(s.engine) + ":fallback=" + strconv.FormatBool(s.fallback) + ":markdown=" + strconv.FormatBool(s.emitMarkdown) + ":formats=" + strings.Join(formats, ",")
}

const (
	maxOfficeReadRichContentBytes           int64 = 32 * 1024 * 1024
	maxOfficeReadTextRunes                        = 1_000_000
	maxOfficeReadStructuredMarkdownRunes          = 1_000_000
	maxOfficeReadRichContentImageBytes            = 20 * 1024 * 1024
	maxOfficeReadRichContentTotalImageBytes       = 32 * 1024 * 1024
)

// MaxOfficeReadRichContentImageBytes is the largest individual image exposed
// to an opt-in rich-content consumer. It aligns with the asset manager's
// safe decode ceiling.
const MaxOfficeReadRichContentImageBytes = maxOfficeReadRichContentImageBytes

// MaxOfficeReadTextRunes is the largest OfficeRead text result accepted by
// the adapter. Tool paging may return smaller chunks but never caches an
// unbounded whole-document string.
const MaxOfficeReadTextRunes = maxOfficeReadTextRunes

// ExtractOfficeReadRichContent returns OfficeRead's Markdown and embedded
// image data only for the explicit rich-content rollout. It is intentionally
// separate from ExtractOfficeTextWithFormat: the latter remains text-only so
// Markdown or binary image data can never enter automatic chat injection.
//
// The bool reports whether rich content was enabled for this file. Callers
// should silently retain their existing parser when it is false or an error is
// returned. This makes the feature safe for format-level rollback.
func ExtractOfficeReadRichContent(filePath string) (OfficeReadRichContent, bool, error) {
	return extractOfficeReadRichContentWithSettings(filePath, currentOfficeReadSettings())
}

// ExtractOfficeReadRichContentWithOfficeReadConfig extracts rich Office
// content under an explicit trusted host policy. This keeps a multi-tenant
// host from falling back to the process-wide desktop configuration while
// retaining environment overrides for emergency rollback.
func ExtractOfficeReadRichContentWithOfficeReadConfig(filePath string, config OfficeReadConfig) (OfficeReadRichContent, bool, error) {
	return extractOfficeReadRichContentWithSettings(filePath, officeReadSettingsForConfig(config))
}

func extractOfficeReadRichContentWithSettings(filePath string, settings officeReadSettings) (OfficeReadRichContent, bool, error) {
	format := officeReadFormat(filePath)
	if !settings.richContentEnabledFor(format) {
		return OfficeReadRichContent{}, false, nil
	}
	started := time.Now()
	sourceBytes := int64(-1)
	resourceSample, resourceObserve := officeReadResourceHooks()
	resourceBefore, resourceSampled := safeOfficeReadResourceSample(resourceSample)
	emitResource := func() {
		if !resourceSampled || resourceObserve == nil {
			return
		}
		resourceAfter, ok := safeOfficeReadResourceSample(resourceSample)
		if !ok {
			return
		}
		emitOfficeReadResourceObservation(resourceObserve, OfficeReadResourceObservation{
			Format: format, Engine: settings.engine, SourceBytes: sourceBytes, Elapsed: time.Since(started),
			Before: resourceBefore, After: resourceAfter,
		})
	}
	info, err := os.Stat(filePath)
	if err != nil {
		emitResource()
		return OfficeReadRichContent{}, true, sanitizeOfficeReadRichContentError(err)
	}
	if info.IsDir() {
		emitResource()
		return OfficeReadRichContent{}, true, errors.New("OfficeRead rich content requires a file")
	}
	sourceBytes = info.Size()
	if info.Size() > maxOfficeReadRichContentBytes {
		emitResource()
		return OfficeReadRichContent{}, true, errOfficeReadInputTooLarge
	}
	// Copy before preflight so both signature routing and OfficeRead consume one
	// immutable private source.  The third-party API accepts only a pathname and
	// reopens it itself, so preflighting the user path followed by parsing that
	// path would leave a replacement window even with a digest check.
	snapshot, err := snapshotOfficeReadSource(filePath)
	if err != nil {
		emitResource()
		return OfficeReadRichContent{}, true, sanitizeOfficeReadRichContentError(err)
	}
	cleanupSnapshot := true
	defer func() {
		if cleanupSnapshot {
			_ = os.Remove(snapshot)
		}
	}()
	// Preserve the shared fail-closed container decision before considering a
	// format mismatch. Otherwise an encrypted ZIP named .doc could be returned
	// as a harmless mismatch and a knowledge fallback might reopen it through a
	// legacy DOC parser.
	version, err := preflightOfficeReadSource(snapshot, format)
	if err != nil {
		emitResource()
		return OfficeReadRichContent{}, true, err
	}
	// The rich-content APIs do not perform the text entry point's automatic
	// signature routing: their callers retain the supplied source kind in
	// knowledge-node metadata and image lifecycle records. A reliable ZIP/PDF
	// signature mismatch must therefore stop here rather than yielding DOCX
	// Markdown tagged as DOC (or equivalent). OLE remains extension-led because
	// its magic bytes cannot distinguish Word, Excel, and PowerPoint safely.
	if sniffed := sniffOfficeFormat(snapshot); sniffed != "" && sniffed != format {
		emitResource()
		return OfficeReadRichContent{}, true, errOfficeReadFormatMismatch
	}
	result, err := extractOfficeReadSnapshotResultAfterPreflight(snapshot, version)
	// The worker owns deletion once it has started, including after its caller
	// returns a timeout.  On an early version failure it removed the snapshot
	// itself; Remove is idempotent for this cleanup purpose.
	cleanupSnapshot = false
	if err != nil {
		emitResource()
		return OfficeReadRichContent{}, true, sanitizeOfficeReadRichContentError(err)
	}
	if result == nil {
		emitResource()
		return OfficeReadRichContent{}, true, errOfficeReadUnavailable
	}
	content := OfficeReadRichContent{
		Format: format,
		Images: make([]OfficeReadImage, 0, len(result.Images)),
	}
	if len([]rune(result.StructuredMarkdown)) > maxOfficeReadStructuredMarkdownRunes {
		emitResource()
		return OfficeReadRichContent{}, true, errOfficeReadOutputTooLarge
	}
	content.Markdown = result.StructuredMarkdown
	imageBytes := 0
	for _, image := range result.Images {
		if !shouldKeepOfficeReadRichImage(image.Data, imageBytes) {
			continue
		}
		content.Images = append(content.Images, OfficeReadImage{
			Name: image.Name,
			Alt:  image.Alt,
			Ext:  image.Ext,
			// result is private to this call and becomes unreachable when this
			// function returns. Reusing its immutable byte slice avoids doubling
			// peak memory for every embedded image before the controlled knowledge
			// asset pipeline persists it.
			Data: image.Data,
		})
		imageBytes += len(image.Data)
	}
	emitResource()
	return content, true, nil
}

// shouldKeepOfficeReadRichImage bounds the aggregate binary payload handed to
// downstream consumers. OfficeRead itself must materialize the document to
// parse it, but retaining only this bounded slice prevents a document with
// many individually valid images from multiplying the knowledge import's
// memory and asset-processing work.
func shouldKeepOfficeReadRichImage(data []byte, currentTotal int) bool {
	if len(data) == 0 || len(data) > maxOfficeReadRichContentImageBytes || currentTotal < 0 {
		return false
	}
	return len(data) <= maxOfficeReadRichContentTotalImageBytes-currentTotal
}

func validateOfficeReadText(text string) error {
	if len([]rune(text)) > maxOfficeReadTextRunes {
		return errOfficeReadOutputTooLarge
	}
	return nil
}

// observeOfficeReadPreflightRejection closes the diagnostic gap at the
// extension/signature routing boundary. ExtractOfficeText must reject an
// unsafe configured source format before it can rewrite the format based on a
// content signature; that return happens before extractOfficeTextWithEngine
// would normally install its observations. Keep this telemetry content-free
// and best-effort, matching the later engine path without reopening the file.
func observeOfficeReadPreflightRejection(filePath, format string, preflightErr error) {
	settings := currentOfficeReadSettings()
	if !settings.enabledFor(format) {
		// A security preflight also protects legacy/disabled routes, but it is
		// not a migration attempt. Do not manufacture OfficeRead rollout samples
		// merely because a caller rejected a container before legacy parsing.
		return
	}
	started := time.Now()
	sourceBytes := int64(-1)
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		sourceBytes = info.Size()
	}

	resourceSample, resourceObserve := officeReadResourceHooks()
	resourceBefore, resourceSampled := safeOfficeReadResourceSample(resourceSample)
	emitOfficeReadObservation(OfficeReadObservation{
		Format:      format,
		Engine:      settings.engine,
		SourceBytes: sourceBytes,
		Elapsed:     time.Since(started),
		ErrorClass:  officeReadErrorClass(preflightErr, ""),
	})
	if !resourceSampled || resourceObserve == nil {
		return
	}
	resourceAfter, ok := safeOfficeReadResourceSample(resourceSample)
	if !ok {
		return
	}
	emitOfficeReadResourceObservation(resourceObserve, OfficeReadResourceObservation{
		Format: format, Engine: settings.engine, SourceBytes: sourceBytes, Elapsed: time.Since(started),
		Before: resourceBefore, After: resourceAfter,
	})
}

func observeOfficeReadInputTooLarge(filePath, format string, sourceBytes int64) {
	settings := currentOfficeReadSettings()
	started := time.Now()
	resourceSample, resourceObserve := officeReadResourceHooks()
	resourceBefore, resourceSampled := safeOfficeReadResourceSample(resourceSample)
	emitOfficeReadObservation(OfficeReadObservation{
		Format:      format,
		Engine:      settings.engine,
		SourceBytes: sourceBytes,
		Elapsed:     time.Since(started),
		ErrorClass:  "input_too_large",
	})
	if !resourceSampled || resourceObserve == nil {
		return
	}
	resourceAfter, ok := safeOfficeReadResourceSample(resourceSample)
	if !ok {
		return
	}
	emitOfficeReadResourceObservation(resourceObserve, OfficeReadResourceObservation{
		Format: format, Engine: settings.engine, SourceBytes: sourceBytes, Elapsed: time.Since(started),
		Before: resourceBefore, After: resourceAfter,
	})
}

// extractOfficeTextWithEngine uses OfficeRead only for explicitly enabled
// Office formats. In dual mode the legacy text remains authoritative. This
// preserves existing output, pagination, and auto-injection behavior while
// allowing callers to validate OfficeRead safely.
func extractOfficeTextWithEngine(filePath, format string) (string, string, error) {
	return extractOfficeTextWithEngineAfterPreflightWithSettings(filePath, format, false, currentOfficeReadSettings())
}

// extractOfficeTextWithEngineAfterPreflight is the internal form used by the
// unified extension/signature router. When preflightDone is true, the router
// has just completed the shared inspection of these exact bytes and this call
// must not repeat ZIP/OLE metadata traversal before invoking OfficeRead.
func extractOfficeTextWithEngineAfterPreflight(filePath, format string, preflightDone bool) (string, string, error) {
	return extractOfficeTextWithEngineAfterPreflightWithSettings(filePath, format, preflightDone, currentOfficeReadSettings())
}

func extractOfficeTextWithEngineAfterPreflightWithSettings(filePath, format string, preflightDone bool, settings officeReadSettings) (string, string, error) {
	if !settings.enabledFor(format) {
		return extractLegacyOfficeTextWithFormat(filePath, format)
	}
	started := time.Now()
	sourceBytes := int64(-1)
	resourceSample, resourceObserve := officeReadResourceHooks()
	var resourceBefore OfficeReadResourceSnapshot
	resourceSampled := false
	if resourceSample != nil {
		resourceBefore, resourceSampled = safeOfficeReadResourceSample(resourceSample)
	}
	emitResource := func() {
		if !resourceSampled || resourceSample == nil || resourceObserve == nil {
			return
		}
		resourceAfter, ok := safeOfficeReadResourceSample(resourceSample)
		if !ok {
			return
		}
		emitOfficeReadResourceObservation(resourceObserve, OfficeReadResourceObservation{
			Format: format, Engine: settings.engine, SourceBytes: sourceBytes, Elapsed: time.Since(started),
			Before: resourceBefore, After: resourceAfter,
		})
	}
	if info, err := os.Stat(filePath); err == nil && !info.IsDir() {
		sourceBytes = info.Size()
	}
	if sourceBytes > maxOfficeReadRichContentBytes {
		observation := OfficeReadObservation{
			Format: format, Engine: settings.engine, SourceBytes: sourceBytes,
			Elapsed: time.Since(started), ErrorClass: "input_too_large",
		}
		if settings.engine == OfficeExtractEngineDual {
			// Dual mode must preserve the legacy result. Skip the unsafe shadow
			// parse but still make its omission visible in content-free telemetry.
			legacyText, legacyFormat, legacyErr := extractLegacyOfficeTextWithFormat(filePath, format)
			// Match the normal dual-read evidence invariant: a nil parser error
			// without a readable body is not a successful extraction, and an
			// unsuccessful side must not retain partial rune/token metrics.
			if legacyErr == nil && strings.TrimSpace(legacyText) != "" {
				legacyTokens := officeReadTokenHistogram(legacyText)
				observation.LegacyOK = true
				observation.LegacySize = len([]rune(legacyText))
				observation.LegacyTokens = officeReadTokenCount(legacyTokens)
			}
			observation.Elapsed = time.Since(started)
			emitOfficeReadObservation(observation)
			emitResource()
			return legacyText, legacyFormat, legacyErr
		}
		emitOfficeReadObservation(observation)
		emitResource()
		return "", format, errOfficeReadInputTooLarge
	}

	// The file can be replaced after the router's preflight (including while it
	// sniffs a misleading extension). In production, take a stable shared
	// snapshot before starting either parser. A possible legacy fallback must
	// read those same bytes rather than reopening a replacement at filePath.
	// OfficeRead receives its own child snapshot through extractOfficeReadResult,
	// so its timed-out worker can safely outlive this function and its shared
	// fallback snapshot. Focused package tests may replace the narrow text seam
	// below to model parser outcomes without manufacturing a full Office fixture.
	_ = preflightDone
	legacyPath := filePath
	var officeText string
	var officeErr error
	if officeReadExtract != nil {
		// The narrow test seam models parser outcomes independently of a real
		// Office fixture. Preserve that contract: snapshotting is required for
		// production path-based parsers, but a seam must not turn a synthetic
		// nonexistent pathname into a filesystem failure.
		officeText, _, officeErr = officeReadExtract(filePath)
	} else {
		sharedSnapshot, snapshotErr := snapshotOfficeReadSource(filePath)
		if snapshotErr != nil {
			observation := OfficeReadObservation{
				Format: format, Engine: settings.engine, SourceBytes: sourceBytes,
				Elapsed: time.Since(started), ErrorClass: officeReadErrorClass(snapshotErr, ""),
			}
			emitOfficeReadObservation(observation)
			emitResource()
			return "", format, snapshotErr
		}
		defer func() { _ = os.Remove(sharedSnapshot) }()
		if _, preflightErr := preflightOfficeReadSource(sharedSnapshot, format); preflightErr != nil {
			observation := OfficeReadObservation{
				Format: format, Engine: settings.engine, SourceBytes: sourceBytes,
				Elapsed: time.Since(started), ErrorClass: officeReadErrorClass(preflightErr, ""),
			}
			emitOfficeReadObservation(observation)
			emitResource()
			return "", format, preflightErr
		}
		sharedVersion, versionErr := officeReadSourceVersion(sharedSnapshot)
		if versionErr != nil {
			officeErr = versionErr
		} else {
			// Give the bounded worker a child snapshot. A timeout may outlive this
			// function while legacy fallback still needs sharedSnapshot, so the
			// worker must own a separate file and its cleanup lifecycle.
			parserSnapshot, parserSnapshotErr := snapshotOfficeReadSource(sharedSnapshot)
			if parserSnapshotErr != nil {
				officeErr = parserSnapshotErr
			} else {
				parserVersion, parserVersionErr := officeReadSourceVersion(parserSnapshot)
				if parserVersionErr != nil || parserVersion.size != sharedVersion.size || parserVersion.digest != sharedVersion.digest {
					_ = os.Remove(parserSnapshot)
					if parserVersionErr != nil {
						officeErr = parserVersionErr
					} else {
						officeErr = errOfficeReadSourceChanged
					}
				} else {
					result, resultErr := extractOfficeReadSnapshotResultAfterPreflight(parserSnapshot, parserVersion)
					officeErr = resultErr
					if officeErr == nil {
						if result == nil {
							officeErr = errOfficeReadUnavailable
						} else {
							officeText = result.Text
						}
					}
				}
			}
		}
		legacyPath = sharedSnapshot
	}
	// The caller has already selected the route (including ExtractOfficeText's
	// ZIP signature routing), so retain its authoritative format for return
	// values, diagnostics, and dual-report accounting. This prevents a DOCX
	// named .doc from being extracted through the DOCX route but reported as DOC.
	officeFormat := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(format)), ".")
	if officeFormat == "" {
		officeFormat = officeReadFormat(filePath)
	}
	if officeErr == nil {
		officeErr = validateOfficeReadText(officeText)
	}

	officeReadOK := officeErr == nil && strings.TrimSpace(officeText) != ""
	if settings.engine == OfficeExtractEngineOfficeRead && officeReadOK {
		officeTokens := officeReadTokenHistogram(officeText)
		emitOfficeReadObservation(OfficeReadObservation{
			Format: format, Engine: settings.engine, SourceBytes: sourceBytes, Elapsed: time.Since(started), OfficeReadOK: true, OfficeReadSize: len([]rune(officeText)), OfficeReadTokens: officeReadTokenCount(officeTokens),
		})
		emitResource()
		return officeText, officeFormat, nil
	}

	if IsOfficeReadContainerSafetyError(officeErr) {
		observation := OfficeReadObservation{
			Format:      format,
			Engine:      settings.engine,
			SourceBytes: sourceBytes,
			Elapsed:     time.Since(started),
			// A container safety rejection is never usable text evidence, even
			// if an upstream parser happened to return partial text before the
			// error. Keep all content counters zero so this early-return branch
			// matches the dual-report audit invariant for failed readers.
			ErrorClass: officeReadErrorClass(officeErr, officeText),
		}
		emitOfficeReadObservation(observation)
		emitResource()
		return "", officeFormat, officeErr
	}
	if settings.engine != OfficeExtractEngineDual && isOfficeReadTextRecoveryBlocked(officeErr) {
		// A retained-content limit is a MaClaw resource decision, not an
		// OfficeRead quality failure. Retrying the same bytes through legacy here
		// would bypass the output boundary and contradict the fail-closed tool
		// guidance. Dual remains intentionally separate below: it is a shadow
		// comparison mode whose public contract returns the legacy result.
		observation := OfficeReadObservation{
			Format:      format,
			Engine:      settings.engine,
			SourceBytes: sourceBytes,
			Elapsed:     time.Since(started),
			ErrorClass:  officeReadErrorClass(officeErr, officeText),
		}
		emitOfficeReadObservation(observation)
		emitResource()
		return "", officeFormat, officeErr
	}

	legacyText, legacyFormat, legacyErr := extractLegacyOfficeTextWithFormat(legacyPath, format)
	legacyOK := legacyErr == nil
	// A nil parser error alone is not dual-read evidence: both sides need a
	// readable body before their rune/token counters can influence migration
	// diagnostics. Keep legacyOK for the existing fallback return semantics,
	// but use the stricter observed value in the content-free observation.
	legacyObservedOK := legacyOK && strings.TrimSpace(legacyText) != ""
	officeTokens := officeReadTokenHistogram(officeText)
	legacyTokens := officeReadTokenHistogram(legacyText)
	officeRunes, officeTokenCount := 0, 0
	if officeReadOK {
		officeRunes = len([]rune(officeText))
		officeTokenCount = officeReadTokenCount(officeTokens)
	}
	legacyRunes, legacyTokenCount := 0, 0
	if legacyObservedOK {
		legacyRunes = len([]rune(legacyText))
		legacyTokenCount = officeReadTokenCount(legacyTokens)
	}
	sharedTokens := 0
	if officeReadOK && legacyObservedOK {
		sharedTokens = officeReadSharedTokenCount(officeTokens, legacyTokens)
	}
	observation := OfficeReadObservation{
		Format:           format,
		Engine:           settings.engine,
		SourceBytes:      sourceBytes,
		Elapsed:          time.Since(started),
		OfficeReadOK:     officeReadOK,
		OfficeReadSize:   officeRunes,
		OfficeReadTokens: officeTokenCount,
		LegacyOK:         legacyObservedOK,
		LegacySize:       legacyRunes,
		LegacyTokens:     legacyTokenCount,
		SharedTokens:     sharedTokens,
	}
	if settings.engine == OfficeExtractEngineDual {
		// The shadow result is intentionally not returned or logged here. The
		// host owns diagnostics so no document content leaks from this layer.
		observation.ErrorClass = officeReadErrorClass(officeErr, officeText)
		emitOfficeReadObservation(observation)
		emitResource()
		return legacyText, legacyFormat, legacyErr
	}
	if legacyOK && settings.fallback {
		observation.FallbackUsed = true
		observation.ErrorClass = officeReadErrorClass(officeErr, officeText)
		emitOfficeReadObservation(observation)
		emitResource()
		return legacyText, legacyFormat, nil
	}
	observation.ErrorClass = officeReadErrorClass(officeErr, officeText)
	emitOfficeReadObservation(observation)
	emitResource()
	if officeErr != nil {
		return "", officeFormat, officeErr
	}
	if strings.TrimSpace(officeText) == "" {
		return "", officeFormat, errors.New("OfficeRead returned no readable text")
	}
	return legacyText, legacyFormat, legacyErr
}

// isOfficeReadTextRecoveryBlocked identifies resource-policy failures for
// which an OfficeRead primary route must not reopen the same input through the
// legacy parser. Container/version safety errors are handled immediately above;
// this helper keeps that branch focused on the retained-content limit.
func isOfficeReadTextRecoveryBlocked(err error) bool {
	return errors.Is(err, errOfficeReadOutputTooLarge)
}

// officeReadTokenHistogram produces content-free comparison metrics for dual
// reads. It never leaves this package: callers receive only aggregate counts.
// CJK Han characters are individual terms because documents commonly have no
// whitespace separators; letters/numbers in other scripts remain word tokens.
func officeReadTokenHistogram(text string) map[string]int {
	if text == "" {
		return nil
	}
	counts := make(map[string]int)
	var word []rune
	flush := func() {
		if len(word) == 0 {
			return
		}
		counts[strings.ToLower(string(word))]++
		word = word[:0]
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			flush()
			counts[string(r)]++
			continue
		}
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			word = append(word, r)
			continue
		}
		flush()
	}
	flush()
	return counts
}

func officeReadTokenCount(tokens map[string]int) int {
	total := 0
	for _, n := range tokens {
		total += n
	}
	return total
}

func officeReadSharedTokenCount(left, right map[string]int) int {
	total := 0
	for token, leftCount := range left {
		if rightCount := right[token]; rightCount < leftCount {
			total += rightCount
		} else {
			total += leftCount
		}
	}
	return total
}

func officeReadErrorClass(err error, text string) string {
	if err != nil {
		// Classify only from the parser error locally; the error itself is never
		// emitted by OfficeReadObservation or GUI rollout logs. Stable categories
		// make encrypted, malformed and inaccessible samples actionable without
		// turning parser wording into telemetry.
		if errors.Is(err, errOfficeReadEncryptedContainer) {
			return "encrypted"
		}
		if errors.Is(err, errOfficeReadUnsafeContainer) {
			return "malformed"
		}
		if errors.Is(err, errOfficeReadInputTooLarge) {
			return "input_too_large"
		}
		if errors.Is(err, errOfficeReadOutputTooLarge) {
			return "output_too_large"
		}
		if errors.Is(err, errOfficeReadTimedOut) {
			return "timeout"
		}
		if errors.Is(err, errOfficeReadSourceChanged) {
			return "source_changed"
		}
		if errors.Is(err, errOfficeReadFormatMismatch) {
			// A declared Office kind disagrees with a reliable OOXML family.
			// Treat it as a malformed input at the agent/tool boundary: callers
			// must not receive recovery guidance that asks another parser to
			// reopen the same mislabelled container.
			return "malformed"
		}
		message := strings.ToLower(err.Error())
		switch {
		case strings.Contains(message, "encrypt"), strings.Contains(message, "password"), strings.Contains(message, "protected"):
			return "encrypted"
		case strings.Contains(message, "permission"), strings.Contains(message, "access is denied"), strings.Contains(message, "not permitted"):
			return "unreadable"
		case strings.Contains(message, "zip"), strings.Contains(message, "ole"), strings.Contains(message, "corrupt"), strings.Contains(message, "malformed"), strings.Contains(message, "invalid"):
			return "malformed"
		}
		return "extract_error"
	}
	if strings.TrimSpace(text) == "" {
		return "empty_text"
	}
	return ""
}

func emitOfficeReadObservation(observation OfficeReadObservation) {
	officeReadObserveMu.RLock()
	handler := officeReadObserve
	officeReadObserveMu.RUnlock()
	if handler != nil {
		// Diagnostics are strictly optional. A host logger, callback, or test
		// hook must not turn an otherwise successful document extraction into a
		// GUI-visible failure (or a process panic).
		func() {
			defer func() { _ = recover() }()
			handler(observation)
		}()
	}
}

func officeReadResourceHooks() (func() OfficeReadResourceSnapshot, func(OfficeReadResourceObservation)) {
	officeReadObserveMu.RLock()
	sample := officeReadResourceSample
	handler := officeReadResourceObserve
	officeReadObserveMu.RUnlock()
	return sample, handler
}

// safeOfficeReadResourceSample keeps optional process telemetry out of the
// parser's failure domain. It reports false when a host-provided sampler
// panics, so callers simply omit that resource record for this extraction.
func safeOfficeReadResourceSample(sample func() OfficeReadResourceSnapshot) (snapshot OfficeReadResourceSnapshot, ok bool) {
	if sample == nil {
		return OfficeReadResourceSnapshot{}, false
	}
	defer func() {
		if recover() != nil {
			snapshot = OfficeReadResourceSnapshot{}
			ok = false
		}
	}()
	return sample(), true
}

// emitOfficeReadResourceObservation makes resource diagnostics best-effort,
// matching the content-free migration observer above. A broken host telemetry
// sink must never change extraction, fallback, or pagination semantics.
func emitOfficeReadResourceObservation(handler func(OfficeReadResourceObservation), observation OfficeReadResourceObservation) {
	if handler == nil {
		return
	}
	func() {
		defer func() { _ = recover() }()
		handler(observation)
	}()
}
