package agent

import (
	"archive/zip"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	officeread "github.com/RapidAI/OfficeRead"
	"github.com/richardlehane/mscfb"
)

func withOfficeReadExtractionGateForTest(t *testing.T, slots int, timeout time.Duration) {
	t.Helper()
	previousSlots := officeReadExtractionSlots
	previousTimeout := officeReadExtractionTimeout
	officeReadExtractionSlots = make(chan struct{}, slots)
	officeReadExtractionTimeout = timeout
	t.Cleanup(func() {
		officeReadExtractionSlots = previousSlots
		officeReadExtractionTimeout = previousTimeout
	})
}

func TestOfficeReadExplicitConfigOverridesProviderWithoutChangingIt(t *testing.T) {
	clearOfficeReadEnvironment(t)
	providerFallback := false
	restore := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "legacy", Formats: []string{"doc"}, Fallback: &providerFallback}
	})
	defer restore()

	explicitFallback := true
	explicit := OfficeReadConfig{
		Engine:       "officeread",
		Formats:      []string{"docx"},
		Fallback:     &explicitFallback,
		EmitMarkdown: &explicitFallback,
	}
	settings := officeReadSettingsForConfig(explicit)
	if settings.engine != OfficeExtractEngineOfficeRead || !settings.enabledFor("docx") || settings.enabledFor("doc") || !settings.fallback || !settings.emitMarkdown {
		t.Fatalf("explicit settings = %#v", settings)
	}
	if current := currentOfficeReadSettings(); current.engine != OfficeExtractEngineLegacy || len(current.formats) != 1 || current.fallback || current.emitMarkdown {
		t.Fatalf("explicit policy changed provider-backed settings: %#v", current)
	}
}

func TestCloneOfficeReadConfigOwnsMutableFields(t *testing.T) {
	fallback := true
	emitMarkdown := false
	original := OfficeReadConfig{
		Engine:       "officeread",
		Formats:      []string{"docx", "xlsx"},
		Fallback:     &fallback,
		EmitMarkdown: &emitMarkdown,
	}
	clone := CloneOfficeReadConfig(original)
	original.Formats[0] = "pptx"
	fallback = false
	emitMarkdown = true
	if clone.Engine != "officeread" || !reflect.DeepEqual(clone.Formats, []string{"docx", "xlsx"}) || clone.Fallback == nil || !*clone.Fallback || clone.EmitMarkdown == nil || *clone.EmitMarkdown {
		t.Fatalf("clone must be independent from original mutations: %#v", clone)
	}
	if CloneOfficeReadConfigPtr(nil) != nil {
		t.Fatal("nil policy must remain nil")
	}
}

func TestOfficeReadProviderPolicyIsCopiedBeforeResolution(t *testing.T) {
	clearOfficeReadEnvironment(t)
	fallback := true
	emitMarkdown := true
	formats := []string{"docx"}
	restore := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: formats, Fallback: &fallback, EmitMarkdown: &emitMarkdown}
	})
	defer restore()

	snapshot := readOfficeReadConfig()
	formats[0] = "pptx"
	fallback = false
	emitMarkdown = false
	if !reflect.DeepEqual(snapshot.Formats, []string{"docx"}) || snapshot.Fallback == nil || !*snapshot.Fallback || snapshot.EmitMarkdown == nil || !*snapshot.EmitMarkdown {
		t.Fatalf("provider snapshot changed after provider-owned mutation: %#v", snapshot)
	}
}

func TestToolReadDocumentWithOfficeReadConfigSeparatesCacheByPolicy(t *testing.T) {
	clearOfficeReadEnvironment(t)
	path := filepath.Join(t.TempDir(), "per-policy.docx")
	writeMinimalDOCX(t, path, "legacy document body")
	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "OfficeRead document body", "docx", nil
	})
	defer restoreExtract()

	legacy := ToolReadDocumentWithOfficeReadConfig(map[string]interface{}{"file_path": path}, OfficeReadConfig{Engine: "legacy"})
	if !strings.Contains(legacy, "legacy document body") || strings.Contains(legacy, "OfficeRead document body") {
		t.Fatalf("legacy explicit route = %q", legacy)
	}
	office := ToolReadDocumentWithOfficeReadConfig(map[string]interface{}{"file_path": path}, OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}})
	if !strings.Contains(office, "OfficeRead document body") || strings.Contains(office, "legacy document body") {
		t.Fatalf("OfficeRead explicit route reused wrong cache = %q", office)
	}
}
func TestOfficeReadSettings_DefaultsToAllSupportedFormats(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "")
	t.Setenv("MACLAW_OFFICE_READ_EMIT_MARKDOWN", "")

	settings := currentOfficeReadSettings()
	if settings.engine != OfficeExtractEngineOfficeRead {
		t.Fatalf("engine = %q, want officeread", settings.engine)
	}
	for _, format := range []string{"doc", "docx", "ppt", "pptx", "xls", "xlsx"} {
		if !settings.enabledFor(format) {
			t.Fatalf("%s must be enabled by default", format)
		}
	}
	if !settings.fallback {
		t.Fatal("fallback must default to true during migration")
	}
	if settings.emitMarkdown {
		t.Fatal("structured Markdown must remain disabled by default")
	}
}

func TestCurrentOfficeReadRuntimePolicyReturnsCanonicalResolvedSnapshot(t *testing.T) {
	clearOfficeReadEnvironment(t)
	fallback := false
	emitMarkdown := true
	restore := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{
			Engine:       "dual",
			Formats:      []string{".XLS", "doc", "xls"},
			Fallback:     &fallback,
			EmitMarkdown: &emitMarkdown,
		}
	})
	defer restore()

	policy := CurrentOfficeReadRuntimePolicy()
	if policy.Engine != OfficeExtractEngineDual || !reflect.DeepEqual(policy.Formats, []string{"doc", "xls"}) || policy.Fallback || !policy.EmitMarkdown {
		t.Fatalf("runtime policy = %#v", policy)
	}
	policy.Formats[0] = "mutated"
	if again := CurrentOfficeReadRuntimePolicy(); !reflect.DeepEqual(again.Formats, []string{"doc", "xls"}) {
		t.Fatalf("runtime policy formats must be a caller-owned snapshot: %#v", again)
	}
}

func TestExtractOfficeReadResultBounded_TimeoutKeepsWorkerGateBounded(t *testing.T) {
	withOfficeReadExtractionGateForTest(t, 1, 20*time.Millisecond)
	previous := officeReadResultExtract
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		once.Do(func() { close(started) })
		<-release
		return &officeread.Result{Text: "late result"}, nil
	}
	defer func() { officeReadResultExtract = previous }()

	firstResult := make(chan error, 1)
	go func() {
		_, err := extractOfficeReadResultBounded("first.docx")
		firstResult <- err
	}()
	<-started
	select {
	case err := <-firstResult:
		if !errors.Is(err, ErrOfficeReadTimedOut) {
			t.Fatalf("first timeout err = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("first extraction did not time out")
	}

	startedAt := time.Now()
	if _, err := extractOfficeReadResultBounded("second.docx"); !errors.Is(err, ErrOfficeReadTimedOut) {
		t.Fatalf("saturated gate err = %v", err)
	} else if elapsed := time.Since(startedAt); elapsed > 500*time.Millisecond {
		t.Fatalf("saturated gate did not honor timeout: %v", elapsed)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for len(officeReadExtractionSlots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := len(officeReadExtractionSlots); got != 0 {
		t.Fatalf("late worker retained extraction slot: %d", got)
	}
}

func TestExtractOfficeReadResultBounded_RecoversLateWorkerPanic(t *testing.T) {
	withOfficeReadExtractionGateForTest(t, 1, 20*time.Millisecond)
	previous := officeReadResultExtract
	started := make(chan struct{})
	release := make(chan struct{})
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		close(started)
		<-release
		panic("test-only delayed parser panic")
	}
	defer func() { officeReadResultExtract = previous }()

	result := make(chan error, 1)
	go func() {
		_, err := extractOfficeReadResultBounded("panic.docx")
		result <- err
	}()
	<-started
	if err := <-result; !errors.Is(err, ErrOfficeReadTimedOut) {
		t.Fatalf("timeout err = %v", err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for len(officeReadExtractionSlots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if got := len(officeReadExtractionSlots); got != 0 {
		t.Fatalf("panic worker retained extraction slot: %d", got)
	}
}

func TestOfficeReadErrorClass_TimeoutIsContentFree(t *testing.T) {
	if got := officeReadErrorClass(ErrOfficeReadTimedOut, "sensitive text must not affect classification"); got != "timeout" {
		t.Fatalf("timeout class = %q", got)
	}
}

func TestExtractOfficeReadResult_RejectsReplacementAfterPreflightBeforeParser(t *testing.T) {
	path := filepath.Join(t.TempDir(), "replace-after-preflight.docx")
	writeMinimalDOCX(t, path, "validated source")

	previousPreflight := officeReadPreflight
	var preflightOnce sync.Once
	officeReadPreflight = func(filePath, format string) error {
		err := preflightOfficeReadContainer(filePath, format)
		preflightOnce.Do(func() {
			// This replacement occurs after the container inspection but before
			// the adapter's post-preflight version check. It need not preserve
			// mtime/size: the digest is the decisive identity.
			writeMinimalDOCX(t, filePath, "replacement source")
		})
		return err
	}
	defer func() { officeReadPreflight = previousPreflight }()
	previousExtract := officeReadResultExtract
	called := false
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		called = true
		return &officeread.Result{Text: "must not parse replacement"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()

	if _, err := extractOfficeReadResult(path); !errors.Is(err, ErrOfficeReadSourceChanged) {
		t.Fatalf("replacement err = %v, want source-change rejection", err)
	}
	if called {
		t.Fatal("OfficeRead parser received a source that replaced the preflighted file")
	}
}

func TestExtractOfficeReadResult_ParserReadsPrivateValidatedSnapshot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "snapshot-source.docx")
	writeMinimalDOCX(t, path, "validated source")

	previousPreflight := officeReadPreflight
	var replaced sync.Once
	officeReadPreflight = func(filePath, format string) error {
		err := preflightOfficeReadContainer(filePath, format)
		replaced.Do(func() {
			// This alters the original name after the snapshot preflight. The
			// parser must receive the immutable private copy instead.
			writeMinimalDOCX(t, path, "replacement source")
		})
		return err
	}
	defer func() { officeReadPreflight = previousPreflight }()

	previousExtract := officeReadResultExtract
	var parserPath string
	officeReadResultExtract = func(received string, _ officeread.Options) (*officeread.Result, error) {
		parserPath = received
		text, err := extractDocxText(received)
		if err != nil {
			return nil, err
		}
		return &officeread.Result{Text: text}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()

	result, err := extractOfficeReadResult(path)
	if err != nil {
		t.Fatalf("extract snapshot: %v", err)
	}
	if result == nil || !strings.Contains(result.Text, "validated source") || strings.Contains(result.Text, "replacement source") {
		t.Fatalf("parser text = %#v, want validated snapshot only", result)
	}
	if parserPath == "" || filepath.Clean(parserPath) == filepath.Clean(path) {
		t.Fatalf("parser path = %q, want private snapshot distinct from source", parserPath)
	}
	if _, err := os.Stat(parserPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot %q still exists after synchronous parse: %v", parserPath, err)
	}
}

func TestSnapshotOfficeReadInput_PreservesVerifiedBytesAndCleansUp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "knowledge-source.docx")
	writeMinimalDOCX(t, path, "verified knowledge source")

	previousPreflight := officeReadPreflight
	var replaced sync.Once
	officeReadPreflight = func(filePath, format string) error {
		err := preflightOfficeReadContainer(filePath, format)
		replaced.Do(func() { writeMinimalDOCX(t, path, "replacement knowledge source") })
		return err
	}
	defer func() { officeReadPreflight = previousPreflight }()

	snapshot, cleanup, err := SnapshotOfficeReadInput(path, "docx")
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if cleanup == nil || filepath.Clean(snapshot) == filepath.Clean(path) {
		t.Fatalf("snapshot = %q cleanup=%v, want private path and cleanup", snapshot, cleanup != nil)
	}
	text, err := extractDocxText(snapshot)
	if err != nil || !strings.Contains(text, "verified knowledge source") || strings.Contains(text, "replacement knowledge source") {
		t.Fatalf("snapshot text=%q err=%v, want verified version", text, err)
	}
	cleanup()
	if _, err := os.Stat(snapshot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("snapshot %q still exists after cleanup: %v", snapshot, err)
	}
}

func TestSnapshotBoundedDocumentInputRejectsAtomicReplacementAfterCopy(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "replace-after-copy.txt")
	if err := os.WriteFile(path, []byte("validated snapshot bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	replacement := filepath.Join(dir, "replacement.txt")
	if err := os.WriteFile(replacement, []byte("replacement bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	previousHook := snapshotDocumentSourceBeforeFinalCheck
	snapshotDocumentSourceBeforeFinalCheck = func(source string) {
		if source != path {
			t.Fatalf("snapshot source = %q, want %q", source, path)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(replacement, path); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { snapshotDocumentSourceBeforeFinalCheck = previousHook })

	snapshot, cleanup, err := SnapshotBoundedDocumentInput(path, ".txt")
	if !errors.Is(err, ErrOfficeReadSourceChanged) || snapshot != "" || cleanup != nil {
		t.Fatalf("snapshot result = path=%q cleanup=%v err=%v, want source-changed rejection", snapshot, cleanup != nil, err)
	}
}

func TestSnapshotBoundedDocumentInputRejectsMetadataPreservingRewriteAfterCopy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "rewrite-after-copy.txt")
	const original = "snapshot byte version A"
	const replacement = "snapshot byte version B"
	if len(original) != len(replacement) {
		t.Fatal("test fixture must preserve source size")
	}
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}

	previousHook := snapshotDocumentSourceBeforeFinalCheck
	snapshotDocumentSourceBeforeFinalCheck = func(source string) {
		if source != path {
			t.Fatalf("snapshot source = %q, want %q", source, path)
		}
		if err := os.WriteFile(path, []byte(replacement), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, before.ModTime(), before.ModTime()); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { snapshotDocumentSourceBeforeFinalCheck = previousHook })

	snapshot, cleanup, err := SnapshotBoundedDocumentInput(path, ".txt")
	if !errors.Is(err, ErrOfficeReadSourceChanged) || snapshot != "" || cleanup != nil {
		t.Fatalf("snapshot result = path=%q cleanup=%v err=%v, want source-changed rejection", snapshot, cleanup != nil, err)
	}
}

func TestSnapshotCSVInputRejectsDisguisedDocumentContainers(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(*testing.T, string)
		want  error
	}{
		{
			name: "docx",
			write: func(t *testing.T, path string) {
				writeMinimalDOCX(t, path, "must not become CSV")
			},
			want: ErrOfficeReadFormatMismatch,
		},
		{
			name: "pdf",
			write: func(t *testing.T, path string) {
				if err := os.WriteFile(path, []byte("%PDF-1.4\n% CSV disguise\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
			want: ErrOfficeReadFormatMismatch,
		},
		{
			name: "encrypted ooxml",
			write: func(t *testing.T, path string) {
				writeEncryptedOfficeReadZIP(t, path)
			},
			want: ErrOfficeReadEncryptedContainer,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "disguised.csv")
			tc.write(t, path)
			snapshot, cleanup, err := SnapshotCSVInput(path)
			if snapshot != "" || cleanup != nil || !errors.Is(err, tc.want) {
				t.Fatalf("SnapshotCSVInput = path=%q cleanup=%t err=%v, want %v", snapshot, cleanup != nil, err, tc.want)
			}
		})
	}
}

func TestExtractOfficeReadResult_TimeoutRetainsSnapshotUntilWorkerExits(t *testing.T) {
	withOfficeReadExtractionGateForTest(t, 1, 20*time.Millisecond)
	path := filepath.Join(t.TempDir(), "late-snapshot.docx")
	writeMinimalDOCX(t, path, "late snapshot source")

	previousExtract := officeReadResultExtract
	started := make(chan string, 1)
	release := make(chan struct{})
	officeReadResultExtract = func(received string, _ officeread.Options) (*officeread.Result, error) {
		started <- received
		<-release
		return &officeread.Result{Text: "late result"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()

	if _, err := extractOfficeReadResult(path); !errors.Is(err, ErrOfficeReadTimedOut) {
		t.Fatalf("timeout err = %v", err)
	}
	snapshot := <-started
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("late worker lost snapshot before it exited: %v", err)
	}
	close(release)
	deadline := time.Now().Add(time.Second)
	for {
		if _, err := os.Stat(snapshot); errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("snapshot %q remained after late worker exit", snapshot)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestExtractOfficeText_FallbackReadsSameSnapshotAsOfficeRead(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "true")
	path := filepath.Join(t.TempDir(), "fallback-snapshot.docx")
	writeMinimalDOCX(t, path, "validated source")

	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		// OfficeRead has reached its verified private input. Altering the user
		// pathname now must not change the legacy fallback's bytes.
		writeMinimalDOCX(t, path, "replacement source")
		return nil, errors.New("test parser failure")
	}
	defer func() { officeReadResultExtract = previousExtract }()

	text, format, err := ExtractOfficeTextWithFormat(path, "docx")
	if err != nil || format != "docx" || !strings.Contains(text, "validated source") || strings.Contains(text, "replacement source") {
		t.Fatalf("fallback = text=%q format=%q err=%v, want validated snapshot", text, format, err)
	}
}

func TestExtractOfficeText_DualReadsSameSnapshotAcrossReplacement(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "dual-snapshot.docx")
	writeMinimalDOCX(t, path, "validated source")

	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(received string, _ officeread.Options) (*officeread.Result, error) {
		text, err := extractDocxText(received)
		if err != nil {
			return nil, err
		}
		writeMinimalDOCX(t, path, "replacement source")
		return &officeread.Result{Text: text}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()

	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	text, format, err := ExtractOfficeTextWithFormat(path, "docx")
	if err != nil || format != "docx" || !strings.Contains(text, "validated source") || strings.Contains(text, "replacement source") {
		t.Fatalf("dual result = text=%q format=%q err=%v, want validated snapshot", text, format, err)
	}
	if !got.OfficeReadOK || !got.LegacyOK || got.SharedTokens == 0 {
		t.Fatalf("dual observation did not compare one validated source: %#v", got)
	}
}

func TestExtractOfficeText_LegacyReadsPrivateSnapshotAcrossReplacement(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	path := filepath.Join(t.TempDir(), "legacy-snapshot.docx")
	writeMinimalDOCX(t, path, "validated legacy source")

	previousPreflight := officeReadPreflight
	var replaced sync.Once
	officeReadPreflight = func(filePath, format string) error {
		err := preflightOfficeReadContainer(filePath, format)
		replaced.Do(func() { writeMinimalDOCX(t, path, "replacement legacy source") })
		return err
	}
	defer func() { officeReadPreflight = previousPreflight }()

	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "docx" || !strings.Contains(text, "validated legacy source") || strings.Contains(text, "replacement legacy source") {
		t.Fatalf("legacy result = text=%q format=%q err=%v, want validated snapshot", text, format, err)
	}
}

func TestExtractOfficeTextWithFormat_LegacyFailsClosedWhenPreflightSourceChanges(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	path := filepath.Join(t.TempDir(), "legacy-explicit-snapshot.docx")
	writeMinimalDOCX(t, path, "validated explicit legacy source")

	previousPreflight := officeReadPreflight
	var replaced sync.Once
	officeReadPreflight = func(filePath, format string) error {
		err := preflightOfficeReadContainer(filePath, format)
		replaced.Do(func() { writeMinimalDOCX(t, path, "replacement explicit legacy source") })
		return err
	}
	defer func() { officeReadPreflight = previousPreflight }()

	text, format, err := ExtractOfficeTextWithFormat(path, "docx")
	if !errors.Is(err, ErrOfficeReadSourceChanged) || format != "docx" || text != "" {
		t.Fatalf("explicit legacy result = text=%q format=%q err=%v, want source-change rejection", text, format, err)
	}
}

func TestExtractOfficeText_TimeoutFallsBackAndEmitsStableObservation(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "true")
	withOfficeReadExtractionGateForTest(t, 1, 20*time.Millisecond)
	path := filepath.Join(t.TempDir(), "timeout-fallback.docx")
	writeMinimalDOCX(t, path, "legacy fallback after timeout")

	previous := officeReadResultExtract
	started := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		once.Do(func() { close(started) })
		<-release
		return &officeread.Result{Text: "late OfficeRead body"}, nil
	}
	defer func() { officeReadResultExtract = previous }()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "docx" || !strings.Contains(text, "legacy fallback after timeout") {
		t.Fatalf("timeout fallback = text=%q format=%q err=%v", text, format, err)
	}
	if got.ErrorClass != "timeout" || !got.FallbackUsed || got.OfficeReadOK || !got.LegacyOK || got.OfficeReadSize != 0 || got.OfficeReadTokens != 0 {
		t.Fatalf("timeout observation = %#v", got)
	}
	<-started
	close(release)
	deadline := time.Now().Add(time.Second)
	for len(officeReadExtractionSlots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if gotSlots := len(officeReadExtractionSlots); gotSlots != 0 {
		t.Fatalf("late timeout worker retained gate: %d", gotSlots)
	}
}

func TestOfficeReadSettings_RichContentRequiresExplicitOptIn(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restore := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"doc"}, EmitMarkdown: &enabled}
	})
	defer restore()
	if settings := currentOfficeReadSettings(); !settings.emitMarkdown || !settings.enabledFor("doc") {
		t.Fatalf("persisted rich-content opt-in ignored: %#v", settings)
	}
	t.Setenv("MACLAW_OFFICE_READ_EMIT_MARKDOWN", "false")
	if settings := currentOfficeReadSettings(); settings.emitMarkdown {
		t.Fatalf("environment rich-content disable override ignored: %#v", settings)
	}
}

func TestOfficeReadSettings_InvalidBooleanEnvironmentOverridesDoNotEnableRichContent(t *testing.T) {
	clearOfficeReadEnvironment(t)
	fallback := false
	emitMarkdown := false
	restore := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, Fallback: &fallback, EmitMarkdown: &emitMarkdown}
	})
	defer restore()

	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "definitely")
	t.Setenv("MACLAW_OFFICE_READ_EMIT_MARKDOWN", "enable-now")
	settings := currentOfficeReadSettings()
	if settings.fallback || settings.emitMarkdown || OfficeReadRichContentEnabledForFormat("docx") {
		t.Fatalf("invalid boolean environment values must retain disabled persisted policy: %#v", settings)
	}

	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "1")
	t.Setenv("MACLAW_OFFICE_READ_EMIT_MARKDOWN", "true")
	settings = currentOfficeReadSettings()
	if !settings.fallback || !settings.emitMarkdown || !OfficeReadRichContentEnabledForFormat("docx") {
		t.Fatalf("explicit true boolean environment values were not applied: %#v", settings)
	}
}

func TestOfficeReadSettings_RichContentStaysOffDuringDualSampling(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restore := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "dual", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restore()

	if settings := currentOfficeReadSettings(); !settings.emitMarkdown || !settings.enabledFor("docx") {
		t.Fatalf("dual test setup did not preserve policy: %#v", settings)
	}
	if OfficeReadRichContentEnabledForFormat("docx") {
		t.Fatal("dual sampling must not make structured Markdown user-visible")
	}
	path := filepath.Join(t.TempDir(), "dual-rich.docx")
	writeMinimalDOCX(t, path, "dual shadow content")
	if content, active, err := ExtractOfficeReadRichContent(path); err != nil || active || content.Markdown != "" || len(content.Images) != 0 {
		t.Fatalf("dual rich extraction = content=%#v active=%t err=%v", content, active, err)
	}
}

func TestOfficeReadSettings_ExplicitLegacyDisablesDefaultFormats(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "")

	settings := currentOfficeReadSettings()
	if settings.engine != OfficeExtractEngineLegacy {
		t.Fatalf("legacy kill switch must disable OfficeRead, got %#v", settings)
	}
	for _, format := range []string{"doc", "docx", "ppt", "pptx", "xls", "xlsx"} {
		if settings.enabledFor(format) {
			t.Fatalf("legacy kill switch left %s enabled: %#v", format, settings)
		}
	}
}

func TestOfficeReadSettings_UsesPersistedConfigUnlessEnvironmentOverrides(t *testing.T) {
	clearOfficeReadEnvironment(t)
	fallback := false
	restore := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "dual", Formats: []string{".doc", "xls"}, Fallback: &fallback}
	})
	defer restore()

	settings := currentOfficeReadSettings()
	if settings.engine != OfficeExtractEngineDual || !settings.enabledFor("doc") || !settings.enabledFor("xls") || settings.enabledFor("ppt") || settings.fallback {
		t.Fatalf("persisted settings not applied: %#v", settings)
	}

	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	settings = currentOfficeReadSettings()
	if settings.engine != OfficeExtractEngineLegacy || settings.enabledFor("doc") {
		t.Fatalf("environment kill switch must override persisted config: %#v", settings)
	}
}

func TestOfficeReadSettings_RejectsMalformedNonEmptyFormatScope(t *testing.T) {
	clearOfficeReadEnvironment(t)
	fallback := true
	restore := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"doc", "pdf"}, Fallback: &fallback}
	})
	defer restore()

	settings := currentOfficeReadSettings()
	if settings.enabledFor("doc") || settings.enabledFor("ppt") || len(settings.formats) != 0 {
		t.Fatalf("malformed persisted scope must not partially enable formats: %#v", settings)
	}

	// The environment is an emergency override, but it receives the same
	// fail-closed parsing: one malformed item must not leave another promoted
	// format enabled.
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc,pdf")
	if settings = currentOfficeReadSettings(); settings.enabledFor("doc") || settings.enabledFor("ppt") || len(settings.formats) != 0 {
		t.Fatalf("malformed environment scope must not partially enable formats: %#v", settings)
	}

	t.Setenv("MACLAW_OFFICE_READ_FORMATS", ".DOC, ppt")
	if settings = currentOfficeReadSettings(); !settings.enabledFor("doc") || !settings.enabledFor("ppt") || len(settings.formats) != 2 {
		t.Fatalf("valid environment scope was not canonicalized: %#v", settings)
	}
}

func TestOfficeReadSettings_PanickingConfigProviderFallsBackToDefaultScope(t *testing.T) {
	clearOfficeReadEnvironment(t)
	restore := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		panic("host config provider failed")
	})
	defer restore()

	settings := currentOfficeReadSettings()
	if settings.engine != OfficeExtractEngineOfficeRead || !settings.enabledFor("doc") || !settings.enabledFor("docx") || !settings.enabledFor("ppt") || !settings.enabledFor("pptx") || !settings.enabledFor("xls") || !settings.enabledFor("xlsx") || !settings.fallback || settings.emitMarkdown {
		t.Fatalf("panic fallback settings = %#v", settings)
	}

	// A host-provider failure must not interfere with the operational kill
	// switch. Operators can still disable OfficeRead before a broken GUI config
	// layer is repaired.
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	if settings = currentOfficeReadSettings(); settings.engine != OfficeExtractEngineLegacy || settings.enabledFor("ppt") {
		t.Fatalf("environment kill switch ignored after provider panic: %#v", settings)
	}
}

func TestExtractOfficeTextWithEngine_OfficeReadPrimary(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", ".docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "true")

	restore := stubOfficeReadExtract(t, func(path string) (string, string, error) {
		return "OfficeRead primary body", "docx", nil
	})
	defer restore()

	text, format, err := ExtractOfficeTextWithFormat(filepath.Join(t.TempDir(), "not-read.docx"), "docx")
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	if format != "docx" || text != "OfficeRead primary body" {
		t.Fatalf("got text=%q format=%q", text, format)
	}
}

func TestExtractOfficeTextWithEngine_RealOfficeReadDOCX(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")

	path := filepath.Join(t.TempDir(), "real-officeread.docx")
	writeMinimalDOCX(t, path, "real OfficeRead DOCX body")

	text, format, err := ExtractOfficeTextWithFormat(path, "docx")
	if err != nil {
		t.Fatalf("OfficeRead extract: %v", err)
	}
	if format != "docx" || !strings.Contains(text, "real OfficeRead DOCX body") {
		t.Fatalf("got text=%q format=%q", text, format)
	}
}

// Keep a production-path smoke test for every OOXML family.  Most adapter
// tests deliberately install a seam so they can isolate error and timeout
// behavior; these fixtures instead exercise the pinned OfficeRead dependency
// through its real ZIP-family dispatch before the format is enabled by
// default in a release.
func TestExtractOfficeTextWithEngine_RealOfficeReadOOXMLFamilies(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx,xlsx,pptx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")

	for _, tc := range []struct {
		name     string
		filename string
		write    func(*testing.T, string)
		want     string
	}{
		{name: "docx", filename: "real.docx", write: func(t *testing.T, path string) { writeMinimalDOCX(t, path, "real OfficeRead DOCX body") }, want: "real OfficeRead DOCX body"},
		{name: "xlsx", filename: "real.xlsx", write: func(t *testing.T, path string) { writeMinimalOfficeReadXLSX(t, path, "real OfficeRead XLSX cell") }, want: "real OfficeRead XLSX cell"},
		{name: "pptx", filename: "real.pptx", write: func(t *testing.T, path string) { writeMinimalOfficeReadPPTX(t, path, "real OfficeRead PPTX body") }, want: "real OfficeRead PPTX body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.filename)
			tc.write(t, path)
			text, format, err := ExtractOfficeTextWithFormat(path, strings.TrimPrefix(filepath.Ext(path), "."))
			if err != nil || !strings.Contains(text, tc.want) || format != strings.TrimPrefix(filepath.Ext(path), ".") {
				t.Fatalf("real OfficeRead result = text=%q format=%q err=%v", text, format, err)
			}
		})
	}
}

func TestExtractOfficeTextWithFormat_RejectsMismatchedOOXMLBeforeParser(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx,pptx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "true")

	path := filepath.Join(t.TempDir(), "actual-presentation.pptx")
	writeMinimalOfficeReadOOXMLFixture(t, path, "ppt/presentation.xml", "presentation body")

	called := false
	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		called = true
		return &officeread.Result{Text: "must not parse"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()

	text, format, err := ExtractOfficeTextWithFormat(path, "docx")
	if !errors.Is(err, ErrOfficeReadFormatMismatch) || text != "" || format != "docx" {
		t.Fatalf("explicit mismatched extraction = text=%q format=%q err=%v", text, format, err)
	}
	if called {
		t.Fatal("mismatched OOXML must not reach OfficeRead or legacy fallback")
	}
}

func TestExtractOfficeText_AutoRoutesMismatchedOOXMLAcrossAllModernFamilies(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx,xlsx,pptx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")

	for _, tc := range []struct {
		name         string
		filename     string
		documentPart string
		wantFormat   string
	}{
		{name: "docx named presentation", filename: "actual.pptx", documentPart: "word/document.xml", wantFormat: "docx"},
		{name: "xlsx named document", filename: "actual.docx", documentPart: "xl/workbook.xml", wantFormat: "xlsx"},
		{name: "pptx named workbook", filename: "actual.xlsx", documentPart: "ppt/presentation.xml", wantFormat: "pptx"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.filename)
			writeMinimalOfficeReadOOXMLFixture(t, path, tc.documentPart, "signature-routed body")

			previousExtract := officeReadResultExtract
			defer func() { officeReadResultExtract = previousExtract }()
			called := false
			officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
				called = true
				return &officeread.Result{Text: "signature-routed body"}, nil
			}

			text, format, err := ExtractOfficeText(path)
			if err != nil || !called || format != tc.wantFormat || text != "signature-routed body" {
				t.Fatalf("auto route = text=%q format=%q called=%t err=%v", text, format, called, err)
			}
		})
	}
}

func TestExtractOfficeText_ReportsSniffedFormatWhenOfficeReadIsPrimary(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc,docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")
	path := filepath.Join(t.TempDir(), "misnamed.doc")
	writeMinimalDOCX(t, path, "actual DOCX body")

	text, format, err := ExtractOfficeText(path)
	if err != nil {
		t.Fatalf("OfficeRead extract: %v", err)
	}
	if format != "docx" || !strings.Contains(text, "actual DOCX body") {
		t.Fatalf("got text=%q format=%q, want sniffed docx", text, format)
	}
}

func TestExtractOfficeText_PrimaryRouteRevalidatesBeforeParser(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")
	path := filepath.Join(t.TempDir(), "single-text-preflight.docx")
	writeMinimalDOCX(t, path, "single text preflight body")

	previousPreflight := officeReadPreflight
	preflightCalls := 0
	officeReadPreflight = func(filePath, format string) error {
		preflightCalls++
		return preflightOfficeReadContainer(filePath, format)
	}
	defer func() { officeReadPreflight = previousPreflight }()

	text, format, err := ExtractOfficeText(path)
	if err != nil || format != "docx" || !strings.Contains(text, "single text preflight body") {
		t.Fatalf("extract: text=%q format=%q err=%v", text, format, err)
	}
	// The first inspection protects extension/signature routing; the second
	// verifies the exact bytes immediately before OfficeRead opens the source.
	// Two passes are intentional: a path can be replaced between them.
	if preflightCalls != 2 {
		t.Fatalf("text extraction preflight calls = %d, want 2", preflightCalls)
	}
}

func TestExtractOfficeText_RejectsOversizedEnabledSourceBeforePreflight(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "oversized-before-preflight.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxOfficeReadRichContentBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	previousPreflight := officeReadPreflight
	preflightCalls := 0
	officeReadPreflight = func(string, string) error {
		preflightCalls++
		return nil
	}
	defer func() { officeReadPreflight = previousPreflight }()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, format, err := ExtractOfficeText(path)
	if !errors.Is(err, ErrOfficeReadInputTooLarge) || format != "docx" {
		t.Fatalf("extract err=%v format=%q, want input-size rejection for docx", err, format)
	}
	if preflightCalls != 0 {
		t.Fatalf("oversized input preflight calls = %d, want 0", preflightCalls)
	}
	if got.Format != "docx" || got.Engine != OfficeExtractEngineOfficeRead || got.SourceBytes != maxOfficeReadRichContentBytes+1 || got.ErrorClass != "input_too_large" || got.OfficeReadOK || got.LegacyOK || got.OfficeReadSize != 0 || got.LegacySize != 0 || got.SharedTokens != 0 {
		t.Fatalf("oversized source must emit zero-metric observation without preflight: %#v", got)
	}
}

func TestExtractOfficeText_DualOversizedSourcePreservesLegacyWithoutPreflight(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "ppt")
	path := filepath.Join(t.TempDir(), "oversized-dual-before-preflight.ppt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxOfficeReadRichContentBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	previousPreflight := officeReadPreflight
	preflightCalls := 0
	officeReadPreflight = func(string, string) error {
		preflightCalls++
		return nil
	}
	defer func() { officeReadPreflight = previousPreflight }()
	called := false
	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		called = true
		return "must not parse", "ppt", nil
	})
	defer restoreExtract()

	text, format, err := ExtractOfficeText(path)
	if err == nil || format != "ppt" || text != "" {
		t.Fatalf("dual oversized extract: text=%q format=%q err=%v", text, format, err)
	}
	if preflightCalls != 0 || called {
		t.Fatalf("dual oversized source must skip preflight and OfficeRead shadow: preflight=%d officeRead=%t", preflightCalls, called)
	}
}

func TestSniffOOXMLKindRejectsMultiplePrimaryFamilies(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ambiguous.zip")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	for _, name := range []string{"word/document.xml", "xl/workbook.xml"} {
		part, err := archive.Create(name)
		if err != nil {
			_ = archive.Close()
			_ = file.Close()
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("fixture")); err != nil {
			_ = archive.Close()
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if got := sniffOOXMLKind(path); got != "" {
		t.Fatalf("ambiguous OOXML package must not be signature-routed as %q", got)
	}
}

func TestExtractOfficeTextWithEngine_FallsBackToLegacy(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "true")

	path := filepath.Join(t.TempDir(), "fallback.docx")
	writeMinimalDOCX(t, path, "legacy fallback body")
	restore := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "", "docx", errors.New("OfficeRead parser failure")
	})
	defer restore()

	text, format, err := ExtractOfficeTextWithFormat(path, "docx")
	if err != nil {
		t.Fatalf("fallback extract: %v", err)
	}
	if format != "docx" || !strings.Contains(text, "legacy fallback body") {
		t.Fatalf("got text=%q format=%q", text, format)
	}
}

func TestExtractOfficeTextWithEngine_NoFallbackReturnsOfficeReadFailure(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")

	path := filepath.Join(t.TempDir(), "no-fallback.docx")
	writeMinimalDOCX(t, path, "legacy body must not leak through disabled fallback")
	restore := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "", "docx", errors.New("OfficeRead parser failure")
	})
	defer restore()

	_, _, err := ExtractOfficeTextWithFormat(path, "docx")
	if err == nil || !strings.Contains(err.Error(), "OfficeRead parser failure") {
		t.Fatalf("err = %v, want OfficeRead failure", err)
	}
}

func TestExtractOfficeTextWithEngine_RejectsOversizedOfficeReadInput(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")

	path := filepath.Join(t.TempDir(), "oversized.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxOfficeReadRichContentBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	called := false
	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		called = true
		return "must not parse", "docx", nil
	})
	defer restoreExtract()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, _, err = ExtractOfficeTextWithFormat(path, "docx")
	if !errors.Is(err, errOfficeReadInputTooLarge) {
		t.Fatalf("err = %v, want 32 MiB OfficeRead limit", err)
	}
	if called {
		t.Fatal("OfficeRead must not receive an oversized input")
	}
	if got.ErrorClass != "input_too_large" || got.SourceBytes != maxOfficeReadRichContentBytes+1 || got.OfficeReadOK {
		t.Fatalf("unexpected oversized observation: %#v", got)
	}
}

func TestExtractOfficeTextWithEngine_DualSkipsOversizedShadowRead(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "ppt")

	path := filepath.Join(t.TempDir(), "oversized.ppt")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxOfficeReadRichContentBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	called := false
	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		called = true
		return "must not parse", "ppt", nil
	})
	defer restoreExtract()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, _, _ = ExtractOfficeTextWithFormat(path, "ppt")
	if called {
		t.Fatal("dual mode must skip an oversized OfficeRead shadow parse")
	}
	if got.ErrorClass != "input_too_large" || got.SourceBytes != maxOfficeReadRichContentBytes+1 {
		t.Fatalf("unexpected oversized observation: %#v", got)
	}
	if got.LegacyOK || got.LegacySize != 0 || got.LegacyTokens != 0 || got.SharedTokens != 0 {
		t.Fatalf("oversized dual fallback without readable legacy text must not produce evidence: %#v", got)
	}
}

func TestExtractOfficeTextWithEngine_DualOversizedEmptyLegacyHasNoEvidence(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")

	path := filepath.Join(t.TempDir(), "oversized-empty-legacy.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	document, err := zw.Create("word/document.xml")
	if err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	_, _ = document.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t></w:t></w:r></w:p></w:body></w:document>`))
	padding, err := zw.CreateHeader(&zip.FileHeader{Name: "word/media/padding.bin", Method: zip.Store})
	if err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	chunk := make([]byte, 1024*1024)
	for written := int64(0); written <= maxOfficeReadRichContentBytes; written += int64(len(chunk)) {
		if _, err := padding.Write(chunk); err != nil {
			_ = f.Close()
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	called := false
	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		called = true
		return "must not parse", "docx", nil
	})
	defer restoreExtract()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, _, _ = ExtractOfficeTextWithFormat(path, "docx")
	if called {
		t.Fatal("dual mode must skip an oversized OfficeRead shadow parse")
	}
	if got.ErrorClass != "input_too_large" || got.SourceBytes <= maxOfficeReadRichContentBytes {
		t.Fatalf("unexpected oversized observation: %#v", got)
	}
	if got.LegacyOK || got.LegacySize != 0 || got.LegacyTokens != 0 || got.SharedTokens != 0 {
		t.Fatalf("empty legacy result must not be credited in oversized dual evidence: %#v", got)
	}
}

func TestExtractOfficeReadRichContent_RejectsOversizedInput(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restoreConfig := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restoreConfig()

	path := filepath.Join(t.TempDir(), "oversized.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxOfficeReadRichContentBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	_, active, err := ExtractOfficeReadRichContent(path)
	if !active || !errors.Is(err, ErrOfficeReadInputTooLarge) || !IsOfficeReadRichContentBlocked(err) {
		t.Fatalf("rich oversized input = active=%t err=%v", active, err)
	}
}

func TestExtractOfficeReadRichContent_RealOfficeReadOOXMLFamilies(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restoreConfig := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"docx", "xlsx", "pptx"}, EmitMarkdown: &enabled}
	})
	defer restoreConfig()

	for _, tc := range []struct {
		name     string
		filename string
		write    func(*testing.T, string)
		want     string
	}{
		{name: "docx", filename: "rich.docx", write: func(t *testing.T, path string) { writeMinimalDOCX(t, path, "real rich DOCX body") }, want: "real rich DOCX body"},
		{name: "xlsx", filename: "rich.xlsx", write: func(t *testing.T, path string) { writeMinimalOfficeReadXLSX(t, path, "real rich XLSX cell") }, want: "real rich XLSX cell"},
		{name: "pptx", filename: "rich.pptx", write: func(t *testing.T, path string) { writeMinimalOfficeReadPPTX(t, path, "real rich PPTX body") }, want: "real rich PPTX body"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.filename)
			tc.write(t, path)
			content, active, err := ExtractOfficeReadRichContent(path)
			if err != nil || !active || content.Format != strings.TrimPrefix(filepath.Ext(path), ".") || !strings.Contains(content.Markdown, tc.want) {
				t.Fatalf("real rich result = content=%#v active=%t err=%v", content, active, err)
			}
		})
	}
}

func TestExtractOfficeReadRichContent_EmitsContentFreeResourceObservation(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restoreConfig := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restoreConfig()
	path := filepath.Join(t.TempDir(), "rich-resource-sensitive.docx")
	writeMinimalDOCX(t, path, "rich resource body")

	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		return &officeread.Result{StructuredMarkdown: "# rich resource body"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()
	var samples int
	var got OfficeReadResourceObservation
	restoreResources := SetOfficeReadResourceObserver(func() OfficeReadResourceSnapshot {
		samples++
		return OfficeReadResourceSnapshot{HeapAllocBytes: uint64(samples * 10), TotalAlloc: uint64(samples * 20), SysBytes: uint64(samples * 30), NumGC: uint32(samples)}
	}, func(observation OfficeReadResourceObservation) { got = observation })
	defer restoreResources()

	content, active, err := ExtractOfficeReadRichContent(path)
	if err != nil || !active || content.Markdown == "" {
		t.Fatalf("rich extraction: active=%t content=%#v err=%v", active, content, err)
	}
	if samples != 2 || got.Format != "docx" || got.Engine != OfficeExtractEngineOfficeRead || got.SourceBytes <= 0 || got.Before.HeapAllocBytes != 10 || got.After.HeapAllocBytes != 20 || got.Elapsed < 0 {
		t.Fatalf("unexpected rich resource observation: samples=%d observation=%#v", samples, got)
	}
}

func TestExtractOfficeReadRichContent_IgnoresPanickingDiagnostics(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restoreConfig := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restoreConfig()
	path := filepath.Join(t.TempDir(), "rich-diagnostics.docx")
	writeMinimalDOCX(t, path, "safe rich body")

	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		return &officeread.Result{StructuredMarkdown: "# safe rich body"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()
	restoreResources := SetOfficeReadResourceObserver(
		func() OfficeReadResourceSnapshot { panic("test rich resource sampler panic") },
		func(OfficeReadResourceObservation) { panic("test rich resource observer panic") },
	)
	defer restoreResources()

	content, active, err := ExtractOfficeReadRichContent(path)
	if err != nil || !active || content.Format != "docx" || content.Markdown != "# safe rich body" {
		t.Fatalf("diagnostic panic changed rich extraction: content=%#v active=%t err=%v", content, active, err)
	}
}

func TestExtractOfficeReadRichContent_PreflightAndVersionCheckBeforeExtraction(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restoreConfig := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restoreConfig()
	path := filepath.Join(t.TempDir(), "single-preflight.docx")
	writeMinimalDOCX(t, path, "single preflight body")

	previousPreflight := officeReadPreflight
	preflightCalls := 0
	officeReadPreflight = func(filePath, format string) error {
		preflightCalls++
		return preflightOfficeReadContainer(filePath, format)
	}
	defer func() { officeReadPreflight = previousPreflight }()
	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		return &officeread.Result{StructuredMarkdown: "# single preflight body"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()

	content, active, err := ExtractOfficeReadRichContent(path)
	if err != nil || !active || content.Markdown == "" {
		t.Fatalf("rich extraction: active=%t content=%#v err=%v", active, content, err)
	}
	if preflightCalls != 1 {
		t.Fatalf("rich extraction preflight calls = %d, want 1", preflightCalls)
	}
}

func TestExtractOfficeReadRichContent_EmitsResourceObservationForRejectedInput(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restoreConfig := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restoreConfig()
	path := filepath.Join(t.TempDir(), "rich-too-large.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxOfficeReadRichContentBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	var samples int
	var got OfficeReadResourceObservation
	restoreResources := SetOfficeReadResourceObserver(func() OfficeReadResourceSnapshot {
		samples++
		return OfficeReadResourceSnapshot{}
	}, func(observation OfficeReadResourceObservation) { got = observation })
	defer restoreResources()

	_, active, err := ExtractOfficeReadRichContent(path)
	if !active || !errors.Is(err, ErrOfficeReadInputTooLarge) {
		t.Fatalf("rich oversized input = active=%t err=%v", active, err)
	}
	if samples != 2 || got.Format != "docx" || got.SourceBytes != maxOfficeReadRichContentBytes+1 || got.Elapsed < 0 {
		t.Fatalf("rejected rich resource observation: samples=%d observation=%#v", samples, got)
	}
}

func TestExtractOfficeTextWithEngine_RejectsOversizedOfficeReadText(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")

	restore := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return strings.Repeat("字", maxOfficeReadTextRunes+1), "docx", nil
	})
	defer restore()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, _, err := ExtractOfficeTextWithFormat(filepath.Join(t.TempDir(), "large-output.docx"), "docx")
	if !errors.Is(err, errOfficeReadOutputTooLarge) {
		t.Fatalf("err = %v, want output limit", err)
	}
	if got.ErrorClass != "output_too_large" || got.OfficeReadOK || got.OfficeReadSize != 0 || got.OfficeReadTokens != 0 {
		t.Fatalf("unexpected output-limit observation: %#v", got)
	}
}

func TestExtractOfficeTextWithEngine_DoesNotFallbackWhenOfficeReadTextIsTooLarge(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "true")

	path := filepath.Join(t.TempDir(), "large-output-fallback.docx")
	writeMinimalDOCX(t, path, "legacy body must not bypass output limit")
	restore := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return strings.Repeat("字", maxOfficeReadTextRunes+1), "docx", nil
	})
	defer restore()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	text, format, err := ExtractOfficeTextWithFormat(path, "docx")
	if !errors.Is(err, ErrOfficeReadOutputTooLarge) || text != "" || format != "docx" {
		t.Fatalf("output-limit fallback result = text=%q format=%q err=%v", text, format, err)
	}
	if got.ErrorClass != "output_too_large" || got.FallbackUsed || got.LegacyOK || got.LegacySize != 0 || got.LegacyTokens != 0 {
		t.Fatalf("output-limit fallback must not reopen legacy parser: %#v", got)
	}
}

func TestExtractOfficeTextWithEngine_DualPreservesLegacyWhenOfficeReadTextIsTooLarge(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "large-output.docx")
	writeMinimalDOCX(t, path, "legacy bounded body")
	restore := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return strings.Repeat("字", maxOfficeReadTextRunes+1), "docx", nil
	})
	defer restore()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	text, _, err := ExtractOfficeTextWithFormat(path, "docx")
	if err != nil || !strings.Contains(text, "legacy bounded body") {
		t.Fatalf("dual text=%q err=%v", text, err)
	}
	if got.ErrorClass != "output_too_large" || got.OfficeReadOK || !got.LegacyOK {
		t.Fatalf("unexpected dual output-limit observation: %#v", got)
	}
}

func TestExtractOfficeReadRichContent_RejectsOversizedMarkdown(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restoreConfig := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restoreConfig()
	path := filepath.Join(t.TempDir(), "markdown.docx")
	writeMinimalDOCX(t, path, "body")
	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		return &officeread.Result{StructuredMarkdown: strings.Repeat("#", maxOfficeReadStructuredMarkdownRunes+1)}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()

	_, active, err := ExtractOfficeReadRichContent(path)
	if !active || !errors.Is(err, errOfficeReadOutputTooLarge) {
		t.Fatalf("rich output limit active=%t err=%v", active, err)
	}
}

func TestExtractOfficeReadRichContent_RejectsMisnamedOOXMLBeforeExtraction(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restoreConfig := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"doc", "docx"}, EmitMarkdown: &enabled}
	})
	defer restoreConfig()
	path := filepath.Join(t.TempDir(), "misnamed.doc")
	writeMinimalDOCX(t, path, "actual DOCX body")

	called := false
	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		called = true
		return &officeread.Result{StructuredMarkdown: "# must not parse"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()

	_, active, err := ExtractOfficeReadRichContent(path)
	if !active || !errors.Is(err, errOfficeReadFormatMismatch) {
		t.Fatalf("rich mismatch = active=%t err=%v", active, err)
	}
	if called {
		t.Fatal("misnamed OOXML must not reach rich OfficeRead extraction")
	}
}

func TestExtractOfficeReadRichContent_PreservesContainerSafetyErrorBeforeMismatch(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restoreConfig := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"doc"}, EmitMarkdown: &enabled}
	})
	defer restoreConfig()
	path := filepath.Join(t.TempDir(), "encrypted.doc")
	writeEncryptedOfficeReadZIP(t, path)

	_, active, err := ExtractOfficeReadRichContent(path)
	if !active || !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("rich encrypted mismatch = active=%t err=%v", active, err)
	}
}

func TestExtractOfficeReadRichContent_SanitizesFilesystemError(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restoreConfig := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restoreConfig()

	path := filepath.Join(t.TempDir(), "customer-secret-plan.docx")
	_, active, err := ExtractOfficeReadRichContent(path)
	if !active || !errors.Is(err, ErrOfficeReadExtractionFailed) {
		t.Fatalf("rich missing-file error = active=%t err=%v", active, err)
	}
	if strings.Contains(err.Error(), "customer-secret-plan") || strings.Contains(err.Error(), path) {
		t.Fatalf("rich error leaked source path: %q", err)
	}
}

func TestExtractOfficeReadRichContent_SanitizesThirdPartyError(t *testing.T) {
	clearOfficeReadEnvironment(t)
	enabled := true
	restoreConfig := SetOfficeReadConfigProvider(func() OfficeReadConfig {
		return OfficeReadConfig{Engine: "officeread", Formats: []string{"docx"}, EmitMarkdown: &enabled}
	})
	defer restoreConfig()
	path := filepath.Join(t.TempDir(), "content.docx")
	writeMinimalDOCX(t, path, "body")

	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		return nil, errors.New("parser detail for customer-secret-plan.docx")
	}
	defer func() { officeReadResultExtract = previousExtract }()

	_, active, err := ExtractOfficeReadRichContent(path)
	if !active || !errors.Is(err, ErrOfficeReadExtractionFailed) {
		t.Fatalf("rich third-party error = active=%t err=%v", active, err)
	}
	if strings.Contains(err.Error(), "customer-secret-plan") {
		t.Fatalf("rich error leaked parser detail: %q", err)
	}
}

func TestShouldKeepOfficeReadRichImageBoundsIndividualAndAggregatePayload(t *testing.T) {
	if shouldKeepOfficeReadRichImage(nil, 0) {
		t.Fatal("empty rich image must be discarded")
	}
	if shouldKeepOfficeReadRichImage(make([]byte, maxOfficeReadRichContentImageBytes+1), 0) {
		t.Fatal("per-image rich content limit must be enforced")
	}
	if !shouldKeepOfficeReadRichImage(make([]byte, 12*1024*1024), 20*1024*1024) {
		t.Fatal("image fitting aggregate budget must be retained")
	}
	if shouldKeepOfficeReadRichImage(make([]byte, 12*1024*1024), 21*1024*1024) {
		t.Fatal("aggregate rich image budget must be enforced")
	}
	if shouldKeepOfficeReadRichImage([]byte{1}, -1) {
		t.Fatal("invalid aggregate total must fail closed")
	}
}

func TestExtractOfficeTextWithEngine_CorruptOOXMLDoesNotPanic(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")

	path := filepath.Join(t.TempDir(), "corrupt.docx")
	if err := os.WriteFile(path, []byte("not a ZIP Office document"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Some OfficeRead compatibility modes intentionally return no text rather
	// than an error for an invalid OOXML payload. The migration invariant is
	// that the malformed input remains contained and never panics the GUI.
	_, _, _ = ExtractOfficeTextWithFormat(path, "docx")
}

func TestExtractOfficeTextWithEngine_RejectsUnsafeOOXMLBeforeOfficeRead(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")

	for _, test := range []struct {
		name  string
		write func(*zip.Writer) error
	}{
		{
			name: "duplicate entries",
			write: func(w *zip.Writer) error {
				for range 2 {
					out, err := w.Create("word/document.xml")
					if err != nil {
						return err
					}
					if _, err := out.Write([]byte("<doc/>")); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			name: "case colliding entries",
			write: func(w *zip.Writer) error {
				for _, name := range []string{"word/document.xml", "WORD/DOCUMENT.XML"} {
					out, err := w.Create(name)
					if err != nil {
						return err
					}
					if _, err := out.Write([]byte("<doc/>")); err != nil {
						return err
					}
				}
				return nil
			},
		},
		{
			name: "mixed primary Office families",
			write: func(w *zip.Writer) error {
				for _, name := range []string{"word/document.xml", "xl/workbook.xml"} {
					out, err := w.Create(name)
					if err != nil {
						return err
					}
					if _, err := out.Write([]byte("<root/>")); err != nil {
						return err
					}
				}
				return nil
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "unsafe.docx")
			f, err := os.Create(path)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(f)
			if err := test.write(zw); err != nil {
				_ = zw.Close()
				_ = f.Close()
				t.Fatal(err)
			}
			if err := zw.Close(); err != nil {
				_ = f.Close()
				t.Fatal(err)
			}
			if err := f.Close(); err != nil {
				t.Fatal(err)
			}

			called := false
			previousExtract := officeReadResultExtract
			officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
				called = true
				return &officeread.Result{Text: "must not parse"}, nil
			}
			defer func() { officeReadResultExtract = previousExtract }()
			var got OfficeReadObservation
			restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
			defer restoreObserver()

			_, _, err = ExtractOfficeTextWithFormat(path, "docx")
			if !errors.Is(err, errOfficeReadUnsafeContainer) {
				t.Fatalf("err = %v, want unsafe-container rejection", err)
			}
			if called {
				t.Fatal("OfficeRead must not receive an unsafe OOXML container")
			}
			if got.ErrorClass != "malformed" || got.OfficeReadOK {
				t.Fatalf("unexpected safety observation: %#v", got)
			}
		})
	}
}

func TestExtractOfficeTextWithEngine_RejectsEncryptedOOXMLBeforeBothParsers(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "true")
	path := filepath.Join(t.TempDir(), "encrypted.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry := &zip.FileHeader{Name: "word/document.xml", Method: zip.Deflate, Flags: 1}
	part, err := archive.CreateHeader(entry)
	if err != nil {
		_ = archive.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := part.Write([]byte("<w:document/>")); err != nil {
		_ = archive.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	called := false
	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		called = true
		return &officeread.Result{Text: "must not parse"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, _, err = ExtractOfficeTextWithFormat(path, "docx")
	if !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want encrypted container rejection", err)
	}
	if called || got.ErrorClass != "encrypted" || got.LegacyOK || got.FallbackUsed {
		t.Fatalf("encrypted input reached parser or fallback: called=%t observation=%#v", called, got)
	}
}

func TestExtractOfficeText_LegacyModeRejectsEncryptedOfficeContainer(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	path := filepath.Join(t.TempDir(), "encrypted.docx")
	writeEncryptedOfficeReadZIP(t, path)

	var observations int
	restoreObserver := SetOfficeReadObservationHandler(func(OfficeReadObservation) { observations++ })
	defer restoreObserver()
	if _, _, err := ExtractOfficeText(path); !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("legacy extract err = %v, want encrypted-container rejection", err)
	}
	if observations != 0 {
		t.Fatalf("legacy container rejection must not create OfficeRead migration observations: %d", observations)
	}
}

func TestExtractOfficeText_DisabledOfficeFormatRejectsOversizedInput(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "ppt")
	path := filepath.Join(t.TempDir(), "oversized.docx")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxOfficeReadFileBytes + 1); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ExtractOfficeText(path); !errors.Is(err, ErrOfficeReadInputTooLarge) {
		t.Fatalf("disabled OfficeRead format must retain shared input boundary: %v", err)
	}
}

func TestExtractOfficeTextWithFormat_LegacyModeRejectsEncryptedOfficeContainer(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	path := filepath.Join(t.TempDir(), "encrypted.docx")
	writeEncryptedOfficeReadZIP(t, path)
	if _, _, err := ExtractOfficeTextWithFormat(path, "docx"); !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("legacy explicit-format extract err = %v, want encrypted-container rejection", err)
	}
}

func TestExtractOfficeText_DoesNotSniffRetryRejectedEncryptedZIP(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc")
	path := filepath.Join(t.TempDir(), "encrypted.doc")
	writeEncryptedOfficeReadZIP(t, path)
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, _, err := ExtractOfficeText(path)
	if !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want original encrypted-container rejection", err)
	}
	if got.Format != "doc" || got.Engine != OfficeExtractEngineOfficeRead || got.ErrorClass != "encrypted" || got.SourceBytes <= 0 || got.OfficeReadOK || got.LegacyOK || got.OfficeReadSize != 0 || got.LegacySize != 0 || got.SharedTokens != 0 {
		t.Fatalf("sniff-boundary rejection must emit zero-metric encrypted observation: %#v", got)
	}
}

func TestExtractOfficeText_DoesNotSniffBypassRejectedEncryptedZIPWhenTargetFormatDisabled(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	// The misleading filename extension is enabled, but the sniffed DOCX
	// target is not. The source-format preflight must still reject it.
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc")
	path := filepath.Join(t.TempDir(), "encrypted.doc")
	writeEncryptedOfficeReadZIP(t, path)
	var samples int
	var got OfficeReadResourceObservation
	restoreResources := SetOfficeReadResourceObserver(func() OfficeReadResourceSnapshot {
		samples++
		return OfficeReadResourceSnapshot{HeapAllocBytes: uint64(samples)}
	}, func(observation OfficeReadResourceObservation) { got = observation })
	defer restoreResources()

	_, _, err := ExtractOfficeText(path)
	if !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want original encrypted-container rejection", err)
	}
	if samples != 2 || got.Format != "doc" || got.Engine != OfficeExtractEngineOfficeRead || got.SourceBytes <= 0 || got.Elapsed < 0 || got.Before.HeapAllocBytes != 1 || got.After.HeapAllocBytes != 2 {
		t.Fatalf("sniff-boundary rejection must emit content-free resource observation: samples=%d observation=%#v", samples, got)
	}
}

func TestExtractOfficeTextWithEngine_RejectsEncryptedOLEBeforeBothParsers(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "ppt")

	path := filepath.Join(t.TempDir(), "encrypted.ppt")
	writeMinimalOLE(t, path, "EncryptedSummary")
	called := false
	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		called = true
		return &officeread.Result{Text: "must not parse"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, _, err := ExtractOfficeTextWithFormat(path, "ppt")
	if !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want encrypted container rejection", err)
	}
	if called || got.ErrorClass != "encrypted" || got.LegacyOK || got.FallbackUsed {
		t.Fatalf("encrypted OLE reached parser or fallback: called=%t observation=%#v", called, got)
	}
}

func TestExtractOfficeTextWithEngine_SafetyRejectionDoesNotRetainPartialOfficeMetrics(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "unsafe-partial.docx")
	writeMinimalDOCX(t, path, "legacy text must not be reached")

	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "partial OfficeRead content", "docx", ErrOfficeReadUnsafeContainer
	})
	defer restoreExtract()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	if _, _, err := ExtractOfficeTextWithFormat(path, "docx"); !errors.Is(err, ErrOfficeReadUnsafeContainer) {
		t.Fatalf("err = %v, want container safety rejection", err)
	}
	if got.ErrorClass != "malformed" || got.OfficeReadOK || got.OfficeReadSize != 0 || got.OfficeReadTokens != 0 || got.LegacyOK || got.LegacySize != 0 || got.LegacyTokens != 0 || got.SharedTokens != 0 {
		t.Fatalf("safety rejection must not retain partial parser metrics: %#v", got)
	}
}

func TestExtractOfficeText_DoesNotSniffRetryRejectedEncryptedOLE(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "encrypted.docx")
	writeMinimalOLE(t, path, "EncryptedSummary")

	_, _, err := ExtractOfficeText(path)
	if !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want original encrypted-container rejection", err)
	}
}

func TestPreflightOfficeReadContainer_RejectsMalformedOLE(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.doc")
	if err := os.WriteFile(path, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1not-a-compound-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflightOfficeReadContainer(path, "doc"); !errors.Is(err, ErrOfficeReadUnsafeContainer) {
		t.Fatalf("err = %v, want malformed OLE rejection", err)
	}
}

func TestPreflightOfficeReadContainer_RejectsEncryptedPackageOLE(t *testing.T) {
	path := filepath.Join(t.TempDir(), "encrypted.doc")
	writeMinimalOLE(t, path, "EncryptedPackage", "EncryptionInfo")
	if err := preflightOfficeReadContainer(path, "doc"); !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want encrypted OLE rejection", err)
	}
}

func TestPreflightOfficeReadContainer_AllowsValidUnencryptedOLE(t *testing.T) {
	path := filepath.Join(t.TempDir(), "normal.ppt")
	writeMinimalOLE(t, path, "PowerPoint Document")
	if err := preflightOfficeReadContainer(path, "ppt"); err != nil {
		t.Fatalf("valid unencrypted OLE was rejected: %v", err)
	}
}

func TestPreflightOfficeReadContainer_RejectsMismatchedOLEFamilyForExplicitFormat(t *testing.T) {
	for _, tc := range []struct {
		name   string
		format string
		stream string
		want   error
	}{
		{name: "word as xls", format: "xls", stream: "WordDocument", want: ErrOfficeReadFormatMismatch},
		{name: "excel as ppt", format: "ppt", stream: "Workbook", want: ErrOfficeReadFormatMismatch},
		{name: "powerpoint as doc", format: "doc", stream: "PowerPoint Document", want: ErrOfficeReadFormatMismatch},
		{name: "word correct family", format: "doc", stream: "WordDocument", want: nil},
		{name: "excel correct family", format: "xls", stream: "Workbook", want: nil},
		{name: "powerpoint correct family", format: "ppt", stream: "PowerPoint Document", want: nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "legacy."+tc.format)
			writeMinimalOLE(t, path, tc.stream)
			err := preflightOfficeReadContainer(path, tc.format)
			if !errors.Is(err, tc.want) {
				t.Fatalf("preflight error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestPreflightOfficeReadContainer_GenericOLEProbeRetainsExtensionLedRouting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.bin")
	writeMinimalOLE(t, path, "PowerPoint Document")
	if err := preflightOfficeReadContainer(path, ""); err != nil {
		t.Fatalf("generic OLE probe must not impose a caller family: %v", err)
	}
}

func TestPreflightOfficeReadContainer_IgnoresEmbeddedOLEFamilyStreams(t *testing.T) {
	for _, tc := range []struct {
		name       string
		format     string
		rootStream string
		embedded   string
	}{
		{name: "word embeds workbook", format: "doc", rootStream: "WordDocument", embedded: "Workbook"},
		{name: "excel embeds presentation", format: "xls", rootStream: "Workbook", embedded: "PowerPoint Document"},
		{name: "powerpoint embeds word", format: "ppt", rootStream: "PowerPoint Document", embedded: "WordDocument"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "embedded."+tc.format)
			writeOLEWithEmbeddedStream(t, path, tc.rootStream, "ObjectPool", tc.embedded)
			if err := preflightOfficeReadContainer(path, tc.format); err != nil {
				t.Fatalf("embedded %s stream changed outer %s classification: %v", tc.embedded, tc.format, err)
			}
		})
	}
}

func TestPreflightOfficeReadContainer_IgnoresEmbeddedOLEEncryptionSignals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embedded-encryption.doc")
	writeOLEWithEmbeddedStream(t, path, "WordDocument", "ObjectPool", "EncryptedSummary")
	if err := preflightOfficeReadContainer(path, "doc"); err != nil {
		t.Fatalf("embedded encryption signal must not reject unencrypted outer document: %v", err)
	}
}

// A minimal BIFF Workbook inside a valid CFBF container is sufficient to
// prove the real pinned OfficeRead XLS path runs after MaClaw's OLE preflight.
// DOC/PPT need complete FIB/PPT record graphs, so their meaningful coverage
// remains in the authorized compatibility corpus rather than a fabricated
// adapter fixture.
func TestExtractOfficeTextWithEngine_RealOfficeReadBIFFXLS(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc,xls,ppt")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")

	for _, tc := range []struct {
		name       string
		filename   string
		streamName string
		payload    string
	}{
		// A small BIFF Workbook stream is structurally sufficient for the
		// pinned extractor's legacy spreadsheet path. Faithful DOC/PPT fixtures
		// require complete FIB/PPT record graphs and belong to the authorized
		// compatibility corpus rather than a hand-written adapter unit fixture.
		{name: "xls", filename: "real.xls", streamName: "Workbook", payload: "real OfficeRead XLS cell"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), tc.filename)
			writeOLEWithStream(t, path, tc.streamName, []byte(tc.payload))
			text, format, err := ExtractOfficeTextWithFormat(path, strings.TrimPrefix(filepath.Ext(path), "."))
			if err != nil || format != strings.TrimPrefix(filepath.Ext(path), ".") || !strings.Contains(text, tc.payload) {
				t.Fatalf("real legacy OfficeRead result = text=%q format=%q err=%v", text, format, err)
			}
		})
	}
}

func TestPreflightOfficeReadContainer_RejectsUnreadableWorkbookStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken-workbook.xls")
	writeOLEWithWorkbook(t, path, []byte{0x09, 0x08, 0x00, 0x00})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// The Workbook occupies sectors 2..9. End its chain after sector 2 while
	// retaining its declared 4096-byte size: directory parsing succeeds, but
	// the bounded encryption-prefix read must fail closed.
	binary.LittleEndian.PutUint32(data[512+2*4:512+3*4], 0xfffffffe)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflightOfficeReadContainer(path, "xls"); !errors.Is(err, ErrOfficeReadUnsafeContainer) {
		t.Fatalf("err = %v, want unreadable OLE stream rejection", err)
	}
}

func TestPreflightOfficeReadContainer_RejectsTruncatedWordDocumentStream(t *testing.T) {
	path := filepath.Join(t.TempDir(), "truncated-word.doc")
	writeOLEWithStream(t, path, "WordDocument", make([]byte, 32))
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Legacy Word's FIB base is 32 bytes. Keep the container otherwise valid
	// but make the declared stream shorter, which must not be treated as an
	// ordinary unencrypted document.
	const directorySecondEntryStreamSizeOffset = 512*2 + 128 + 120
	binary.LittleEndian.PutUint32(data[directorySecondEntryStreamSizeOffset:directorySecondEntryStreamSizeOffset+4], 16)
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := preflightOfficeReadContainer(path, "doc"); !errors.Is(err, ErrOfficeReadUnsafeContainer) {
		t.Fatalf("err = %v, want truncated Word stream rejection", err)
	}
}

func TestOfficeReadPreflightReaderAt_BoundsRequestedBytesAndReads(t *testing.T) {
	backing := strings.NewReader(strings.Repeat("x", 64))
	reader := &officeReadPreflightReaderAt{
		reader:      backing,
		maxBytes:    8,
		maxRequests: 2,
	}
	buf := make([]byte, 4)
	if n, err := reader.ReadAt(buf, 0); err != nil || n != 4 {
		t.Fatalf("first read = n=%d err=%v", n, err)
	}
	if n, err := reader.ReadAt(buf, 4); err != nil || n != 4 {
		t.Fatalf("second read = n=%d err=%v", n, err)
	}
	if _, err := reader.ReadAt(make([]byte, 1), 8); !errors.Is(err, errOfficeReadPreflightBudgetExceeded) {
		t.Fatalf("third read err = %v, want budget rejection", err)
	}
	if reader.readBytes != 8 || reader.readRequests != 2 {
		t.Fatalf("usage = bytes=%d requests=%d, want 8 and 2", reader.readBytes, reader.readRequests)
	}
}

func TestOfficeReadPreflightReaderAt_RejectsOversizedOrInvalidRequestBeforeRead(t *testing.T) {
	backing := strings.NewReader(strings.Repeat("x", 64))
	reader := &officeReadPreflightReaderAt{
		reader:      backing,
		maxBytes:    4,
		maxRequests: 1,
	}
	if _, err := reader.ReadAt(make([]byte, 5), 0); !errors.Is(err, errOfficeReadPreflightBudgetExceeded) {
		t.Fatalf("oversized read err = %v, want budget rejection", err)
	}
	if reader.readBytes != 0 || reader.readRequests != 0 {
		t.Fatalf("oversized read changed usage: bytes=%d requests=%d", reader.readBytes, reader.readRequests)
	}
	if _, err := reader.ReadAt(make([]byte, 1), -1); !errors.Is(err, errOfficeReadPreflightBudgetExceeded) {
		t.Fatalf("negative-offset read err = %v, want budget rejection", err)
	}
}

func TestOfficeReadPreflightReaderAt_BoundsSectorReads(t *testing.T) {
	backing := strings.NewReader(strings.Repeat("x", 2048))
	reader := &officeReadPreflightReaderAt{
		reader:         backing,
		maxBytes:       2048,
		maxRequests:    4,
		maxSectorReads: 1,
	}
	if _, err := reader.ReadAt(make([]byte, int(minOfficeReadOLEHeaderBytes)), 0); err != nil {
		t.Fatalf("first sector-sized read err = %v", err)
	}
	if _, err := reader.ReadAt(make([]byte, int(minOfficeReadOLEHeaderBytes)), minOfficeReadOLEHeaderBytes); !errors.Is(err, errOfficeReadPreflightBudgetExceeded) {
		t.Fatalf("second sector-sized read err = %v, want budget rejection", err)
	}
	if reader.readSectorReads != 1 || reader.readRequests != 1 {
		t.Fatalf("usage = sector reads=%d requests=%d, want 1 and 1", reader.readSectorReads, reader.readRequests)
	}
}

func TestPreflightOfficeReadContainer_RejectsOLEHeaderCountsBeyondFile(t *testing.T) {
	for _, test := range []struct {
		name   string
		offset int
	}{
		{name: "directory", offset: 40},
		{name: "fat", offset: 44},
		{name: "mini fat", offset: 64},
		{name: "difat", offset: 72},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "header-count.doc")
			writeMinimalOLE(t, path, "WordDocument")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			binary.LittleEndian.PutUint32(data[test.offset:test.offset+4], ^uint32(0))
			if err := os.WriteFile(path, data, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := preflightOfficeReadContainer(path, "doc"); !errors.Is(err, ErrOfficeReadUnsafeContainer) {
				t.Fatalf("err = %v, want malformed OLE rejection", err)
			}
		})
	}
}

func TestBIFFPrefixHasFilePass(t *testing.T) {
	if biffPrefixHasFilePass([]byte{0x09, 0x08, 0x00, 0x00, 0x2f, 0x00, 0x00, 0x00}) != true {
		t.Fatal("FILEPASS record was not detected")
	}
	if biffPrefixHasFilePass([]byte{0x09, 0x08, 0x04, 0x00, 0x00}) {
		t.Fatal("truncated BIFF record must not be inferred as FILEPASS")
	}
	if biffPrefixHasFilePass([]byte{0x09, 0x08, 0x00, 0x00}) {
		t.Fatal("ordinary BIFF record must not be treated as encrypted")
	}
}

func TestOLEWordPrefixEncrypted(t *testing.T) {
	path := filepath.Join(t.TempDir(), "worddocument.doc")
	encryptedFIB := make([]byte, 32)
	binary.LittleEndian.PutUint16(encryptedFIB[0:2], 0xa5ec)
	binary.LittleEndian.PutUint16(encryptedFIB[10:12], 0x0100)
	writeOLEWithStream(t, path, "WordDocument", encryptedFIB)
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	doc, err := mscfb.New(file)
	if err != nil {
		t.Fatal(err)
	}
	var entry *mscfb.File
	for _, candidate := range doc.File {
		if candidate != nil && strings.EqualFold(candidate.Name, "WordDocument") {
			entry = candidate
			break
		}
	}
	encrypted, err := oleWordPrefixEncrypted(entry)
	if err != nil {
		t.Fatalf("Word FIB preflight: %v", err)
	}
	if !encrypted {
		t.Fatal("Word FIB fEncrypted bit was not detected")
	}
}

func TestExtractOfficeTextWithEngine_RejectsEncryptedWordFIBBeforeBothParsers(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc")
	path := filepath.Join(t.TempDir(), "encrypted.doc")
	encryptedFIB := make([]byte, 32)
	binary.LittleEndian.PutUint16(encryptedFIB[0:2], 0xa5ec)
	binary.LittleEndian.PutUint16(encryptedFIB[10:12], 0x0100)
	writeOLEWithStream(t, path, "WordDocument", encryptedFIB)
	called := false
	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		called = true
		return &officeread.Result{Text: "must not parse"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, _, err := ExtractOfficeTextWithFormat(path, "doc")
	if !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want encrypted container rejection", err)
	}
	if called || got.ErrorClass != "encrypted" || got.LegacyOK || got.FallbackUsed {
		t.Fatalf("encrypted Word FIB reached parser or fallback: called=%t observation=%#v", called, got)
	}
}

func TestExtractOfficeTextWithEngine_RejectsBIFFFilePassBeforeBothParsers(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "xls")
	path := filepath.Join(t.TempDir(), "encrypted.xls")
	writeOLEWithWorkbook(t, path, []byte{0x09, 0x08, 0x00, 0x00, 0x2f, 0x00, 0x00, 0x00})
	called := false
	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		called = true
		return &officeread.Result{Text: "must not parse"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, _, err := ExtractOfficeTextWithFormat(path, "xls")
	if !errors.Is(err, ErrOfficeReadEncryptedContainer) {
		t.Fatalf("err = %v, want encrypted container rejection", err)
	}
	if called || got.ErrorClass != "encrypted" || got.LegacyOK || got.FallbackUsed {
		t.Fatalf("FILEPASS input reached parser or fallback: called=%t observation=%#v", called, got)
	}
}

func TestExtractOfficeTextWithEngine_RejectsMalformedOLEBeforeBothParsers(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "true")
	path := filepath.Join(t.TempDir(), "corrupt.doc")
	if err := os.WriteFile(path, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1not-a-compound-file"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := false
	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		called = true
		return &officeread.Result{Text: "must not parse"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, _, err := ExtractOfficeTextWithFormat(path, "doc")
	if !errors.Is(err, ErrOfficeReadUnsafeContainer) {
		t.Fatalf("err = %v, want malformed OLE rejection", err)
	}
	if called || got.ErrorClass != "malformed" || got.LegacyOK || got.FallbackUsed {
		t.Fatalf("malformed OLE reached parser or fallback: called=%t observation=%#v", called, got)
	}
}

func TestExtractOfficeTextWithEngine_RejectsNonOfficeZIPWithLegacySuffixBeforeBothParsers(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "doc")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "true")

	path := filepath.Join(t.TempDir(), "not-an-office-document.doc")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	payload, err := archive.Create("payload.txt")
	if err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := payload.Write([]byte("not an Office package")); err != nil {
		_ = archive.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	called := false
	previousExtract := officeReadResultExtract
	officeReadResultExtract = func(string, officeread.Options) (*officeread.Result, error) {
		called = true
		return &officeread.Result{Text: "must not parse"}, nil
	}
	defer func() { officeReadResultExtract = previousExtract }()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	_, _, err = ExtractOfficeTextWithFormat(path, "doc")
	if !errors.Is(err, ErrOfficeReadUnsafeContainer) {
		t.Fatalf("err = %v, want malformed ZIP rejection", err)
	}
	if called || got.ErrorClass != "malformed" || got.LegacyOK || got.FallbackUsed {
		t.Fatalf("non-Office ZIP reached parser or fallback: called=%t observation=%#v", called, got)
	}
}

func TestPreflightOfficeReadContainer_AllowsLegitimateDirectoryEntries(t *testing.T) {
	path := filepath.Join(t.TempDir(), "normal.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"word/", "word/document.xml"} {
		out, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatal(err)
		}
		if name != "word/" {
			if _, err := out.Write([]byte("<doc/>")); err != nil {
				_ = zw.Close()
				_ = f.Close()
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := preflightOfficeReadContainer(path, "docx"); err != nil {
		t.Fatalf("directory entry should be accepted: %v", err)
	}
}

func TestPreflightOfficeReadContainer_RejectsOOXMLFamilyWithoutMainDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lookalike.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"word/", "word/styles.xml"} {
		out, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatal(err)
		}
		if name != "word/" {
			if _, err := out.Write([]byte("<w:styles/>")); err != nil {
				_ = zw.Close()
				_ = f.Close()
				t.Fatal(err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := preflightOfficeReadContainer(path, "docx"); !errors.Is(err, ErrOfficeReadUnsafeContainer) {
		t.Fatalf("err = %v, want malformed OOXML lookalike rejection", err)
	}
}

func TestPreflightOfficeReadContainer_AllowsEmbeddedOOXMLPackageWithinOneFamily(t *testing.T) {
	path := filepath.Join(t.TempDir(), "embedded.docx")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(f)
	for _, name := range []string{"word/document.xml", "word/embeddings/embedded.xlsx"} {
		part, err := zw.Create(name)
		if err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatal(err)
		}
		if _, err := part.Write([]byte("placeholder")); err != nil {
			_ = zw.Close()
			_ = f.Close()
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := preflightOfficeReadContainer(path, "docx"); err != nil {
		t.Fatalf("embedded OOXML payload must remain valid in its owning family: %v", err)
	}
}

func TestExtractOfficeTextWithEngine_DualKeepsLegacyResult(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")

	path := filepath.Join(t.TempDir(), "dual.docx")
	writeMinimalDOCX(t, path, "legacy body")
	called := false
	restore := stubOfficeReadExtract(t, func(string) (string, string, error) {
		called = true
		return "OfficeRead shadow body", "docx", nil
	})
	defer restore()

	text, _, err := ExtractOfficeTextWithFormat(path, "docx")
	if err != nil {
		t.Fatalf("dual extract: %v", err)
	}
	if !called {
		t.Fatal("dual mode did not execute OfficeRead")
	}
	if !strings.Contains(text, "legacy body") || strings.Contains(text, "OfficeRead shadow body") {
		t.Fatalf("dual mode returned the wrong result: %q", text)
	}
}

func TestExtractOfficeTextWithEngine_DualDoesNotCreditEmptyLegacyText(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")

	path := filepath.Join(t.TempDir(), "empty-legacy.docx")
	writeMinimalDOCX(t, path, "")
	restore := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "OfficeRead shadow body", "docx", nil
	})
	defer restore()

	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	if _, _, err := ExtractOfficeTextWithFormat(path, "docx"); err == nil {
		t.Fatal("empty legacy result must retain its legacy extraction failure")
	}
	if !got.OfficeReadOK || got.LegacyOK || got.LegacySize != 0 || got.LegacyTokens != 0 || got.SharedTokens != 0 {
		t.Fatalf("empty legacy result must not be credited in dual evidence: %#v", got)
	}
}

func TestExtractOfficeTextWithEngine_ObservationContainsNoContentOrPath(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "dual")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")

	path := filepath.Join(t.TempDir(), "sensitive-name.docx")
	writeMinimalDOCX(t, path, "legacy observable body")
	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "OfficeRead observable body", "docx", nil
	})
	defer restoreExtract()

	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	if _, _, err := ExtractOfficeTextWithFormat(path, "docx"); err != nil {
		t.Fatalf("dual extract: %v", err)
	}
	if got.Format != "docx" || !got.OfficeReadOK || got.OfficeReadSize == 0 || got.OfficeReadTokens == 0 || !got.LegacyOK || got.LegacySize == 0 || got.LegacyTokens == 0 || got.SharedTokens == 0 || got.SourceBytes <= 0 || got.Elapsed < 0 {
		t.Fatalf("unexpected observation: %#v", got)
	}
	if strings.Contains(got.Format, "sensitive") {
		t.Fatalf("observation must not include a path: %#v", got)
	}
}

func TestExtractOfficeTextWithEngine_EmitsContentFreeResourceObservation(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "resource-sensitive.docx")
	writeMinimalDOCX(t, path, "resource observation body")

	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "resource observation body", "docx", nil
	})
	defer restoreExtract()
	var samples int
	var got OfficeReadResourceObservation
	restoreResources := SetOfficeReadResourceObserver(func() OfficeReadResourceSnapshot {
		samples++
		return OfficeReadResourceSnapshot{HeapAllocBytes: uint64(samples * 10), TotalAlloc: uint64(samples * 20), SysBytes: uint64(samples * 30), NumGC: uint32(samples)}
	}, func(observation OfficeReadResourceObservation) { got = observation })
	defer restoreResources()

	if _, _, err := ExtractOfficeTextWithFormat(path, "docx"); err != nil {
		t.Fatalf("extract: %v", err)
	}
	if samples != 2 || got.Format != "docx" || got.Engine != OfficeExtractEngineOfficeRead || got.SourceBytes <= 0 || got.Before.HeapAllocBytes != 10 || got.After.HeapAllocBytes != 20 || got.Elapsed < 0 {
		t.Fatalf("unexpected resource observation: samples=%d observation=%#v", samples, got)
	}
}

func TestExtractOfficeTextWithEngine_IgnoresPanickingDiagnostics(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "diagnostics.docx")
	writeMinimalDOCX(t, path, "safe extraction body")

	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "safe extraction body", "docx", nil
	})
	defer restoreExtract()
	restoreObserver := SetOfficeReadObservationHandler(func(OfficeReadObservation) { panic("test observation sink panic") })
	defer restoreObserver()
	restoreResources := SetOfficeReadResourceObserver(
		func() OfficeReadResourceSnapshot { panic("test resource sampler panic") },
		func(OfficeReadResourceObservation) { panic("test resource sink panic") },
	)
	defer restoreResources()

	text, format, err := ExtractOfficeTextWithFormat(path, "docx")
	if err != nil || format != "docx" || text != "safe extraction body" {
		t.Fatalf("diagnostic panic changed extraction: text=%q format=%q err=%v", text, format, err)
	}
}

func TestExtractOfficeTextWithEngine_IgnoresPanickingResourceObserver(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	path := filepath.Join(t.TempDir(), "resource-observer.docx")
	writeMinimalDOCX(t, path, "safe resource body")

	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "safe resource body", "docx", nil
	})
	defer restoreExtract()
	restoreResources := SetOfficeReadResourceObserver(
		func() OfficeReadResourceSnapshot { return OfficeReadResourceSnapshot{} },
		func(OfficeReadResourceObservation) { panic("test resource observer panic") },
	)
	defer restoreResources()

	text, _, err := ExtractOfficeTextWithFormat(path, "docx")
	if err != nil || text != "safe resource body" {
		t.Fatalf("resource observer panic changed extraction: text=%q err=%v", text, err)
	}
}

func TestOfficeReadTokenHistogramSupportsCJKAndWords(t *testing.T) {
	left := officeReadTokenHistogram("项目 Alpha alpha 2026")
	right := officeReadTokenHistogram("项目 alpha beta 2026")
	if officeReadTokenCount(left) != 5 || officeReadTokenCount(right) != 5 {
		t.Fatalf("unexpected token totals left=%d right=%d", officeReadTokenCount(left), officeReadTokenCount(right))
	}
	if shared := officeReadSharedTokenCount(left, right); shared != 4 {
		t.Fatalf("shared tokens = %d, want 4", shared)
	}
}

func TestOfficeReadErrorClassUsesStableCategories(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		want string
	}{
		{name: "encrypted", err: errors.New("document is password protected"), want: "encrypted"},
		{name: "encrypted container", err: ErrOfficeReadEncryptedContainer, want: "encrypted"},
		{name: "source changed", err: ErrOfficeReadSourceChanged, want: "source_changed"},
		{name: "unreadable", err: errors.New("permission denied"), want: "unreadable"},
		{name: "malformed", err: errors.New("zip: not a valid archive"), want: "malformed"},
		{name: "other", err: errors.New("implementation detail must not leak"), want: "extract_error"},
		{name: "empty", want: "empty_text"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := officeReadErrorClass(test.err, ""); got != test.want {
				t.Fatalf("officeReadErrorClass(%v) = %q, want %q", test.err, got, test.want)
			}
		})
	}
}

func TestExtractOfficeTextWithEngine_RecordsFailureClassWithoutRawError(t *testing.T) {
	clearOfficeReadEnvironment(t)
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "docx")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "false")

	path := filepath.Join(t.TempDir(), "observation.docx")
	writeMinimalDOCX(t, path, "legacy body")
	restoreExtract := stubOfficeReadExtract(t, func(string) (string, string, error) {
		return "", "docx", errors.New("sensitive parser implementation detail")
	})
	defer restoreExtract()
	var got OfficeReadObservation
	restoreObserver := SetOfficeReadObservationHandler(func(observation OfficeReadObservation) { got = observation })
	defer restoreObserver()

	if _, _, err := ExtractOfficeTextWithFormat(path, "docx"); err == nil {
		t.Fatal("expected OfficeRead failure")
	}
	if got.ErrorClass != "extract_error" || got.OfficeReadOK || got.SourceBytes <= 0 || got.Elapsed < 0 {
		t.Fatalf("unexpected failed observation: %#v", got)
	}
}

func TestOfficeReadCacheKeySuffixChangesWithSettings(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "legacy")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "")
	legacyKey := officeReadCacheKeySuffix()

	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "officeread")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", ".ppt,.doc")
	newKey := officeReadCacheKeySuffix()
	if legacyKey == newKey {
		t.Fatalf("cache key did not change: %q", legacyKey)
	}
	if !strings.Contains(newKey, "formats=doc,ppt") {
		t.Fatalf("format allowlist must be stable and sorted: %q", newKey)
	}
}

func TestToolReadDocument_DefaultsToOfficeReadForAllSupportedFormats(t *testing.T) {
	t.Setenv("MACLAW_OFFICE_READ_ENGINE", "")
	t.Setenv("MACLAW_OFFICE_READ_FORMATS", "")
	t.Setenv("MACLAW_OFFICE_READ_FALLBACK", "")

	restore := stubOfficeReadExtract(t, func(path string) (string, string, error) {
		return "default OfficeRead route", strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."), nil
	})
	defer restore()

	for _, format := range []string{"doc", "docx", "ppt", "pptx", "xls", "xlsx"} {
		t.Run(format, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "document."+format)
			writeValidOfficeDefaultRouteFixture(t, path, format)
			out := ToolReadDocument(map[string]interface{}{"file_path": path})
			if strings.Contains(out, "读取失败") || !strings.Contains(out, "default OfficeRead route") || !strings.Contains(out, "# format: "+format) {
				t.Fatalf("default %s route failed: %s", format, out)
			}
		})
	}
}

func writeValidOfficeDefaultRouteFixture(t *testing.T, path, format string) {
	t.Helper()
	switch format {
	case "docx":
		writeMinimalDOCX(t, path, "default OfficeRead route")
	case "xlsx":
		writeStructuredOfficeTestXLSX(t, path, "default OfficeRead route")
	case "pptx":
		writeMinimalOfficeReadOOXMLFixture(t, path, "ppt/presentation.xml", "default OfficeRead route")
	default:
		// Legacy OLE preflight accepts a valid CFBF container before the
		// test seam intercepts the primary OfficeRead extraction.
		writeMinimalOLE(t, path, "SummaryInformation")
	}
}

func writeMinimalOfficeReadOOXMLFixture(t *testing.T, path, documentPart, text string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create OOXML fixture: %v", err)
	}
	zw := zip.NewWriter(file)
	part, err := zw.Create(documentPart)
	if err != nil {
		_ = file.Close()
		t.Fatalf("create OOXML part: %v", err)
	}
	if _, err := part.Write([]byte(text)); err != nil {
		_ = zw.Close()
		_ = file.Close()
		t.Fatalf("write OOXML part: %v", err)
	}
	if err := zw.Close(); err != nil {
		_ = file.Close()
		t.Fatalf("close OOXML fixture: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close OOXML fixture: %v", err)
	}
}

func writeMinimalOfficeReadXLSX(t *testing.T, path, value string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	parts := map[string]string{
		"xl/workbook.xml":            `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Data" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   `<?xml version="1.0" encoding="UTF-8"?><worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>` + value + `</t></is></c></row></sheetData></worksheet>`,
	}
	for name, body := range parts {
		entry, err := zw.Create(name)
		if err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			_ = zw.Close()
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeMinimalOfficeReadPPTX(t *testing.T, path, text string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(file)
	parts := map[string]string{
		"ppt/presentation.xml":            `<?xml version="1.0" encoding="UTF-8"?><p:presentation xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><p:sldIdLst><p:sldId id="256" r:id="rId1"/></p:sldIdLst></p:presentation>`,
		"ppt/_rels/presentation.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/slide" Target="slides/slide1.xml"/></Relationships>`,
		"ppt/slides/slide1.xml":           `<?xml version="1.0" encoding="UTF-8"?><p:sld xmlns:p="http://schemas.openxmlformats.org/presentationml/2006/main" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r><a:t>` + text + `</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`,
	}
	for name, body := range parts {
		entry, err := zw.Create(name)
		if err != nil {
			_ = file.Close()
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			_ = zw.Close()
			_ = file.Close()
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func clearOfficeReadEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"MACLAW_OFFICE_READ_ENGINE",
		"MACLAW_OFFICE_READ_FORMATS",
		"MACLAW_OFFICE_READ_FALLBACK",
		"MACLAW_OFFICE_READ_EMIT_MARKDOWN",
	} {
		// t.Setenv registers restoration of the caller's environment; unsetting
		// afterwards models the truly absent state rather than an empty value.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatalf("unset %s: %v", key, err)
		}
	}
}

func stubOfficeReadExtract(t *testing.T, stub officeReadExtractFunc) func() {
	t.Helper()
	previous := officeReadExtract
	officeReadExtract = stub
	return func() { officeReadExtract = previous }
}

// writeMinimalOLE writes a valid CFBF directory with empty named streams. It
// exercises the preflight directory walk without adding binary fixtures to the
// repository. The stream bytes are deliberately absent: encryption markers
// tested here are container-directory signals.
func writeMinimalOLE(t *testing.T, filePath string, streams ...string) {
	t.Helper()
	const (
		sectorSize = 512
		endOfChain = 0xfffffffe
		noStream   = 0xffffffff
		fatSector  = 0xfffffffd
	)
	data := make([]byte, sectorSize*3)
	copy(data, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"))
	binary.LittleEndian.PutUint16(data[24:26], 0x003e)
	binary.LittleEndian.PutUint16(data[26:28], 3)
	binary.LittleEndian.PutUint16(data[28:30], 0xfffe)
	binary.LittleEndian.PutUint16(data[30:32], 9)
	binary.LittleEndian.PutUint16(data[32:34], 6)
	binary.LittleEndian.PutUint32(data[44:48], 1) // one FAT sector
	binary.LittleEndian.PutUint32(data[48:52], 1) // directory sector
	binary.LittleEndian.PutUint32(data[60:64], endOfChain)
	binary.LittleEndian.PutUint32(data[68:72], endOfChain)
	for off := 76; off < 512; off += 4 {
		binary.LittleEndian.PutUint32(data[off:off+4], noStream)
	}
	binary.LittleEndian.PutUint32(data[76:80], 0)
	for off := sectorSize; off < sectorSize*2; off += 4 {
		binary.LittleEndian.PutUint32(data[off:off+4], noStream)
	}
	binary.LittleEndian.PutUint32(data[sectorSize:sectorSize+4], fatSector)
	binary.LittleEndian.PutUint32(data[sectorSize+4:sectorSize+8], endOfChain)

	directory := data[sectorSize*2:]
	writeOLEDirectoryEntry(directory[:128], "Root Entry", 5, noStream)
	if len(streams) > 0 {
		binary.LittleEndian.PutUint32(directory[76:80], 1)
	}
	for i, name := range streams {
		entry := directory[(i+1)*128 : (i+2)*128]
		writeOLEDirectoryEntry(entry, name, 2, noStream)
		if i+1 < len(streams) {
			binary.LittleEndian.PutUint32(entry[72:76], uint32(i+2))
		}
	}
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeEncryptedOfficeReadZIP(t *testing.T, filePath string) {
	t.Helper()
	file, err := os.Create(filePath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	entry, err := archive.CreateHeader(&zip.FileHeader{Name: "word/document.xml", Method: zip.Deflate, Flags: 1})
	if err != nil {
		_ = archive.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("<w:document/>")); err != nil {
		_ = archive.Close()
		_ = file.Close()
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeOLEDirectoryEntry(entry []byte, name string, objectType byte, childID uint32) {
	encoded := []rune(name)
	for i, r := range encoded {
		binary.LittleEndian.PutUint16(entry[i*2:i*2+2], uint16(r))
	}
	binary.LittleEndian.PutUint16(entry[len(encoded)*2:len(encoded)*2+2], 0)
	binary.LittleEndian.PutUint16(entry[64:66], uint16((len(encoded)+1)*2))
	entry[66] = objectType
	entry[67] = 1
	binary.LittleEndian.PutUint32(entry[68:72], 0xffffffff)
	binary.LittleEndian.PutUint32(entry[72:76], 0xffffffff)
	binary.LittleEndian.PutUint32(entry[76:80], childID)
	binary.LittleEndian.PutUint32(entry[116:120], 0xfffffffe)
}

func writeOLEWithWorkbook(t *testing.T, filePath string, workbookPrefix []byte) {
	writeOLEWithStream(t, filePath, "Workbook", workbookPrefix)
}

func writeOLEWithStream(t *testing.T, filePath, name string, prefix []byte) {
	t.Helper()
	const (
		sectorSize    = 512
		endOfChain    = 0xfffffffe
		noStream      = 0xffffffff
		fatSector     = 0xfffffffd
		streamSectors = 8 // a 4096-byte stream uses normal, not mini, sectors
	)
	data := make([]byte, sectorSize*(1+2+streamSectors))
	copy(data, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"))
	binary.LittleEndian.PutUint16(data[24:26], 0x003e)
	binary.LittleEndian.PutUint16(data[26:28], 3)
	binary.LittleEndian.PutUint16(data[28:30], 0xfffe)
	binary.LittleEndian.PutUint16(data[30:32], 9)
	binary.LittleEndian.PutUint16(data[32:34], 6)
	binary.LittleEndian.PutUint32(data[44:48], 1)
	binary.LittleEndian.PutUint32(data[48:52], 1)
	binary.LittleEndian.PutUint32(data[60:64], endOfChain)
	binary.LittleEndian.PutUint32(data[68:72], endOfChain)
	for offset := 76; offset < 512; offset += 4 {
		binary.LittleEndian.PutUint32(data[offset:offset+4], noStream)
	}
	binary.LittleEndian.PutUint32(data[76:80], 0)
	fat := data[sectorSize : sectorSize*2]
	for offset := 0; offset < len(fat); offset += 4 {
		binary.LittleEndian.PutUint32(fat[offset:offset+4], noStream)
	}
	binary.LittleEndian.PutUint32(fat[0:4], fatSector)
	binary.LittleEndian.PutUint32(fat[4:8], endOfChain)
	for sector := 2; sector < 2+streamSectors; sector++ {
		next := uint32(endOfChain)
		if sector+1 < 2+streamSectors {
			next = uint32(sector + 1)
		}
		binary.LittleEndian.PutUint32(fat[sector*4:sector*4+4], next)
	}
	directory := data[sectorSize*2 : sectorSize*3]
	writeOLEDirectoryEntry(directory[:128], "Root Entry", 5, 1)
	writeOLEDirectoryEntry(directory[128:256], name, 2, noStream)
	binary.LittleEndian.PutUint32(directory[128+116:128+120], 2)
	binary.LittleEndian.PutUint32(directory[128+120:128+124], sectorSize*streamSectors)
	copy(data[sectorSize*3:], prefix)
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeOLEWithEmbeddedStream creates a valid CFBF with one root-level stream
// and one nested stream below a storage object. It exercises the distinction
// between the outer document family and an embedded OLE payload without
// committing binary fixtures.
func writeOLEWithEmbeddedStream(t *testing.T, filePath, rootStream, storage, embeddedStream string) {
	t.Helper()
	const (
		sectorSize = 512
		endOfChain = 0xfffffffe
		noStream   = 0xffffffff
		fatSector  = 0xfffffffd
	)
	data := make([]byte, sectorSize*3)
	copy(data, []byte("\xd0\xcf\x11\xe0\xa1\xb1\x1a\xe1"))
	binary.LittleEndian.PutUint16(data[24:26], 0x003e)
	binary.LittleEndian.PutUint16(data[26:28], 3)
	binary.LittleEndian.PutUint16(data[28:30], 0xfffe)
	binary.LittleEndian.PutUint16(data[30:32], 9)
	binary.LittleEndian.PutUint16(data[32:34], 6)
	binary.LittleEndian.PutUint32(data[44:48], 1)
	binary.LittleEndian.PutUint32(data[48:52], 1)
	binary.LittleEndian.PutUint32(data[60:64], endOfChain)
	binary.LittleEndian.PutUint32(data[68:72], endOfChain)
	for offset := 76; offset < sectorSize; offset += 4 {
		binary.LittleEndian.PutUint32(data[offset:offset+4], noStream)
	}
	binary.LittleEndian.PutUint32(data[76:80], 0)
	fat := data[sectorSize : sectorSize*2]
	for offset := 0; offset < len(fat); offset += 4 {
		binary.LittleEndian.PutUint32(fat[offset:offset+4], noStream)
	}
	binary.LittleEndian.PutUint32(fat[0:4], fatSector)
	binary.LittleEndian.PutUint32(fat[4:8], endOfChain)

	directory := data[sectorSize*2:]
	writeOLEDirectoryEntry(directory[:128], "Root Entry", 5, 1)
	writeOLEDirectoryEntry(directory[128:256], rootStream, 2, noStream)
	writeOLEDirectoryEntry(directory[256:384], storage, 1, 3)
	writeOLEDirectoryEntry(directory[384:512], embeddedStream, 2, noStream)
	// Root child tree: rootStream with storage as its right sibling. Storage's
	// child is embeddedStream, producing Path=[storage] for that stream.
	binary.LittleEndian.PutUint32(directory[128+72:128+76], 2)
	if err := os.WriteFile(filePath, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
