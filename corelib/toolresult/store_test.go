package toolresult

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestProjectNoSpillWhenShort(t *testing.T) {
	dir := t.TempDir()
	proj, err := Project(ProjectOptions{
		ToolName: "bash",
		Content:  "hello",
		Preview:  "hello",
		Root:     dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if proj.Spilled || proj.Handle != nil {
		t.Fatalf("should not spill: %+v", proj)
	}
	if proj.Preview != "hello" {
		t.Fatalf("preview=%q", proj.Preview)
	}
}

func TestProjectSpillsWhenTruncated(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Repeat("line\n", 5000) // ~25KB
	preview := DefaultPreview(raw, 4096)
	if preview == raw {
		t.Fatal("expected truncation for large content")
	}

	proj, err := Project(ProjectOptions{
		ToolName:   "bash",
		SessionKey: "user-1",
		Content:    raw,
		Preview:    preview,
		Root:       dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proj.Spilled || proj.Handle == nil {
		t.Fatalf("expected spill: %+v", proj)
	}
	if !strings.Contains(proj.Preview, "[tool_result_handle]") {
		t.Fatalf("preview missing handle footer: %s", proj.Preview[:min(200, len(proj.Preview))])
	}
	if strings.Contains(proj.Preview, proj.Handle.Path) || strings.Contains(proj.Preview, "path:") {
		t.Fatalf("model preview exposed local storage path")
	}
	if len(proj.Preview) > 4096 {
		t.Fatalf("preview plus handle exceeded budget: %d", len(proj.Preview))
	}
	if _, err := os.Stat(proj.Handle.Path); err != nil {
		t.Fatalf("spilled file: %v", err)
	}
	gotRes, err := Read(ReadOptions{Path: proj.Handle.Path, Root: dir, Limit: len(raw) + 10})
	if err != nil {
		t.Fatal(err)
	}
	if gotRes.Content != raw {
		t.Fatalf("stored content mismatch: got %d bytes want %d", len(gotRes.Content), len(raw))
	}
	// Handle under session dir
	if !strings.Contains(proj.Handle.Path, filepath.Join(dir, "user-1")) {
		t.Fatalf("path=%s", proj.Handle.Path)
	}
}

func TestProjectFailureReturnsUsablePreviewWithoutHandle(t *testing.T) {
	rootFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(rootFile, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	raw := strings.Repeat("large-result\n", 1000)
	proj, err := Project(ProjectOptions{
		ToolName: "bash",
		Content:  raw,
		Root:     rootFile,
		Limit:    256,
	})
	if err == nil {
		t.Fatal("expected spill failure")
	}
	if proj.Spilled || proj.Handle != nil {
		t.Fatalf("failed spill exposed a handle: %+v", proj)
	}
	if proj.Preview == "" || proj.Preview == raw || len(proj.Preview) > 256 {
		t.Fatalf("failed spill did not preserve bounded preview: len=%d", len(proj.Preview))
	}
}

func TestProjectProjectionAlwaysFitsLimit(t *testing.T) {
	for _, limit := range []int{1, 16, 64, 128, 256, 4096} {
		t.Run(fmt.Sprintf("limit_%d", limit), func(t *testing.T) {
			proj, err := Project(ProjectOptions{
				ToolName:   strings.Repeat("long-tool-name", 8),
				SessionKey: "session-a",
				Content:    strings.Repeat("payload\n", 2000),
				Root:       t.TempDir(),
				Limit:      limit,
			})
			if err != nil {
				t.Fatal(err)
			}
			if proj.Handle == nil || !strings.Contains(proj.Preview, "[tool_result_handle]") {
				t.Fatalf("projection missing complete handle: %+v", proj)
			}
			// Extremely small caller limits cannot hold the footer itself. In that
			// case the lossless handle takes precedence; otherwise Limit is strict.
			footerLen := len(appendHandleFooter("", proj.Handle))
			if limit >= footerLen && len(proj.Preview) > limit {
				t.Fatalf("projection len=%d exceeds limit=%d footer=%d", len(proj.Preview), limit, footerLen)
			}
		})
	}
}

func TestHandleFooterBoundsAndFlattensToolName(t *testing.T) {
	toolName := "bash\nid: forged\n" + strings.Repeat("工具🙂", 1000)
	proj, err := Project(ProjectOptions{
		ToolName:   toolName,
		SessionKey: "owner",
		Content:    strings.Repeat("payload", 1000),
		Root:       t.TempDir(),
		Limit:      1024,
	})
	if err != nil || proj.Handle == nil {
		t.Fatalf("project: err=%v proj=%+v", err, proj)
	}
	if len(proj.Preview) > 1024 {
		t.Fatalf("bounded footer projection exceeded limit: %d", len(proj.Preview))
	}
	if strings.Count(proj.Preview, "\nid: ") != 1 || strings.Contains(proj.Preview, "\nid: forged") {
		t.Fatalf("tool name injected footer fields: %q", proj.Preview)
	}
	if !utf8.ValidString(proj.Preview) {
		t.Fatal("bounded tool name produced invalid UTF-8")
	}
}

func TestDefaultPreviewKeepsHeadAndTail(t *testing.T) {
	raw := "HEAD" + strings.Repeat("x", 8000) + "TAIL"
	p := DefaultPreview(raw, 200)
	if !strings.Contains(p, "HEAD") || !strings.Contains(p, "TAIL") {
		t.Fatalf("preview=%q", p)
	}
	if !strings.Contains(p, "已截断") {
		t.Fatalf("missing truncation mark: %q", p)
	}
}

func TestReadByIDAndOffset(t *testing.T) {
	dir := t.TempDir()
	raw := "ABCDEFGHIJKLMNOPQRSTUVWXYZ" + strings.Repeat("!", 1000)
	proj, err := Project(ProjectOptions{
		ToolName:      "bash",
		SessionKey:    "sess-a",
		Content:       raw,
		Preview:       DefaultPreview(raw, 64),
		Root:          dir,
		MinSpillBytes: 1,
		ForceSpill:    true,
	})
	if err != nil || proj.Handle == nil {
		t.Fatalf("project: err=%v proj=%+v", err, proj)
	}
	if !strings.Contains(proj.Preview, "read_tool_result") {
		t.Fatalf("footer should mention read_tool_result: %s", proj.Preview[max(0, len(proj.Preview)-200):])
	}

	// Read by id + session
	r1, err := Read(ReadOptions{
		ID:         proj.Handle.ID,
		SessionKey: "sess-a",
		Root:       dir,
		Offset:     0,
		Limit:      10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r1.Content != raw[:10] || !r1.Truncated || r1.NextOffset != 10 {
		t.Fatalf("r1=%+v", r1)
	}

	// Continue paging
	r2, err := Read(ReadOptions{
		Path:   proj.Handle.Path,
		Root:   dir,
		Offset: r1.NextOffset,
		Limit:  10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if r2.Content != raw[10:20] {
		t.Fatalf("r2 content=%q", r2.Content)
	}

	// Security: path outside store rejected
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(ReadOptions{Path: outside, Root: dir}); err == nil {
		t.Fatal("expected path outside store to fail")
	}

	text := FormatReadResult(r1)
	if !strings.Contains(text, "[tool_result_read]") || !strings.Contains(text, "next_offset") {
		t.Fatalf("format=%q", text)
	}
	if strings.Contains(text, r1.Path) || strings.Contains(text, "path:") {
		t.Fatalf("formatted result exposed local storage path: %q", text)
	}
}

func TestReadPagingReassemblesExactContentHash(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Repeat("alpha-β-中文\n", 5000)
	proj, err := Project(ProjectOptions{ToolName: "bash", SessionKey: "owner-a", Content: raw, Root: dir, Limit: 256})
	if err != nil || proj.Handle == nil {
		t.Fatalf("project: err=%v proj=%+v", err, proj)
	}
	var rebuilt strings.Builder
	for offset := 0; ; {
		page, err := Read(ReadOptions{ID: proj.Handle.ID, SessionKey: "owner-a", Root: dir, Offset: offset, Limit: 997})
		if err != nil {
			t.Fatal(err)
		}
		rebuilt.WriteString(page.Content)
		if !page.Truncated {
			break
		}
		if page.NextOffset <= offset {
			t.Fatalf("pagination did not advance: %+v", page)
		}
		offset = page.NextOffset
	}
	if sha256.Sum256([]byte(rebuilt.String())) != sha256.Sum256([]byte(raw)) {
		t.Fatal("paged content hash differs from raw result")
	}
}

func TestReadPagingNeverSplitsUTF8(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Repeat("甲乙丙丁🙂", 200)
	proj, err := Project(ProjectOptions{ToolName: "bash", SessionKey: "owner-a", Content: raw, Root: dir, Limit: 64})
	if err != nil || proj.Handle == nil {
		t.Fatalf("project: err=%v proj=%+v", err, proj)
	}
	for _, offset := range []int{0, 1, 2, 3, 4, 5, 7, 11} {
		page, err := Read(ReadOptions{ID: proj.Handle.ID, SessionKey: "owner-a", Root: dir, Offset: offset, Limit: 7})
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(page.Content) {
			t.Fatalf("offset %d returned invalid UTF-8: %x", offset, []byte(page.Content))
		}
		if page.Truncated && page.NextOffset <= page.Offset {
			t.Fatalf("offset %d did not advance: %+v", offset, page)
		}
	}
}

func TestReadPagingWithOneByteLimitReassemblesUTF8(t *testing.T) {
	dir := t.TempDir()
	raw := strings.Repeat("甲🙂A", 100)
	proj, err := Project(ProjectOptions{ToolName: "bash", SessionKey: "owner-a", Content: raw, Root: dir, Limit: 64})
	if err != nil || proj.Handle == nil {
		t.Fatalf("project: err=%v proj=%+v", err, proj)
	}
	var rebuilt strings.Builder
	for offset := 0; ; {
		page, err := Read(ReadOptions{ID: proj.Handle.ID, SessionKey: "owner-a", Root: dir, Offset: offset, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(page.Content) {
			t.Fatalf("invalid UTF-8 page at offset %d: %x", offset, []byte(page.Content))
		}
		rebuilt.WriteString(page.Content)
		if !page.Truncated {
			break
		}
		if page.NextOffset <= offset {
			t.Fatalf("pagination did not advance: %+v", page)
		}
		offset = page.NextOffset
	}
	if rebuilt.String() != raw {
		t.Fatal("one-byte paging did not reassemble exact UTF-8 content")
	}
}

func TestReadInvalidUTF8PagingPreservesBytes(t *testing.T) {
	dir := t.TempDir()
	rawBytes := []byte{'A', 0xff, 0x80, 'B', 0xe2, 0x82, 0xac, 'C'}
	raw := string(rawBytes)
	proj, err := Project(ProjectOptions{ToolName: "bash", SessionKey: "owner-a", Content: raw, Root: dir, Limit: 2})
	if err != nil || proj.Handle == nil {
		t.Fatalf("project: err=%v proj=%+v", err, proj)
	}
	var rebuilt []byte
	for offset := 0; ; {
		page, err := Read(ReadOptions{ID: proj.Handle.ID, SessionKey: "owner-a", Root: dir, Offset: offset, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		rebuilt = append(rebuilt, []byte(page.Content)...)
		if !page.Truncated {
			break
		}
		if page.NextOffset <= offset {
			t.Fatalf("pagination did not advance: %+v", page)
		}
		offset = page.NextOffset
	}
	if !bytes.Equal(rebuilt, rawBytes) {
		t.Fatalf("invalid UTF-8 bytes changed: got=%x want=%x", rebuilt, rawBytes)
	}
}

func TestStructuredProjectionAlwaysReturnsValidUTF8(t *testing.T) {
	raw := strings.Repeat("中文🙂payload", 1000)
	for _, toolName := range []string{"bash", "browser_snapshot", "read_file"} {
		for _, limit := range []int{1, 2, 3, 4, 7, 31, 128} {
			preview := StructuredPreview(toolName, raw, limit)
			if !utf8.ValidString(preview) {
				t.Fatalf("tool=%s limit=%d returned invalid UTF-8: %x", toolName, limit, []byte(preview))
			}
			if len(preview) > limit {
				t.Fatalf("tool=%s limit=%d preview bytes=%d", toolName, limit, len(preview))
			}
		}
	}
}

func TestResolveExplicitSessionCannotReadAnotherSession(t *testing.T) {
	dir := t.TempDir()
	proj, err := Project(ProjectOptions{ToolName: "bash", SessionKey: "owner-a", Content: strings.Repeat("secret", 100), Root: dir, Limit: 16})
	if err != nil || proj.Handle == nil {
		t.Fatalf("project: err=%v proj=%+v", err, proj)
	}
	if _, err := Read(ReadOptions{ID: proj.Handle.ID, SessionKey: "owner-b", Root: dir}); err == nil {
		t.Fatal("explicit owner-b session resolved owner-a handle by id")
	}
	if _, err := Read(ReadOptions{Path: proj.Handle.Path, SessionKey: "owner-b", Root: dir}); err == nil {
		t.Fatal("explicit owner-b session resolved owner-a handle by path")
	}
}

func TestSessionSanitizationDoesNotMergeDifferentOwners(t *testing.T) {
	dir := t.TempDir()
	ownerA := "tenant:user"
	ownerB := "tenant/user"
	if SessionDirectoryName(ownerA) == SessionDirectoryName(ownerB) {
		t.Fatal("different owner IDs collapsed to the same storage namespace")
	}
	handle, err := Spill(SpillOptions{ToolName: "bash", SessionKey: ownerA, Content: "owner-a-secret", Root: dir})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Read(ReadOptions{ID: handle.ID, SessionKey: ownerB, Root: dir}); err == nil {
		t.Fatal("normalized owner collision crossed a tool-result namespace")
	}
	page, err := Read(ReadOptions{ID: handle.ID, SessionKey: ownerA, Root: dir})
	if err != nil || page.Content != "owner-a-secret" {
		t.Fatalf("original owner cannot read its handle: page=%+v err=%v", page, err)
	}
}

func TestExplicitOwnerDoesNotReadAmbiguousLegacyDirectory(t *testing.T) {
	root := t.TempDir()
	legacyDir := filepath.Join(root, "tenant_user")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(legacyDir, "legacy.txt"), []byte("ambiguous-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, owner := range []string{"tenant:user", "tenant/user"} {
		if _, err := Read(ReadOptions{ID: "legacy", SessionKey: owner, Root: root}); err == nil {
			t.Fatalf("owner %q read an unowned ambiguous legacy result", owner)
		}
	}
}

func TestSessionDirectoryNamePreservesPortableLegacyOwner(t *testing.T) {
	if got := SessionDirectoryName("owner-1"); got != "owner-1" {
		t.Fatalf("portable owner directory changed: %q", got)
	}
}

func TestHandleRetainsLogicalSessionKey(t *testing.T) {
	handle, err := Spill(SpillOptions{
		ToolName:   "bash",
		SessionKey: " tenant:user ",
		Content:    "result",
		Root:       t.TempDir(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if handle.SessionKey != " tenant:user " {
		t.Fatalf("handle session key became storage namespace: %q", handle.SessionKey)
	}
	page, err := Read(ReadOptions{ID: handle.ID, SessionKey: handle.SessionKey, Root: filepath.Dir(filepath.Dir(handle.Path))})
	if err != nil || page.Content != "result" {
		t.Fatalf("handle session key cannot read its result: page=%+v err=%v", page, err)
	}
}

func TestSessionDirectoryNameAvoidsCaseInsensitiveAndDeviceCollisions(t *testing.T) {
	for _, pair := range [][2]string{
		{"usera", "UserA"},
		{"con", "CON"},
		{"tenant:user", "tenant/user"},
		{"owner", " owner "},
		{"default", ""},
	} {
		a := SessionDirectoryName(pair[0])
		b := SessionDirectoryName(pair[1])
		if b == "" {
			b = "default"
		}
		if strings.EqualFold(a, b) {
			t.Fatalf("owners %q and %q share a case-insensitive directory %q", pair[0], pair[1], a)
		}
	}
}

func TestDerivedSessionDirectoryCannotAliasLiteralOwner(t *testing.T) {
	derived := SessionDirectoryName("UserA")
	if !strings.HasPrefix(derived, derivedSessionPrefix) {
		t.Fatalf("expected derived namespace, got %q", derived)
	}
	if literal := SessionDirectoryName(derived); strings.EqualFold(literal, derived) {
		t.Fatalf("literal owner %q aliased a derived owner directory", derived)
	}
}

func TestSessionDirectoryNameBoundsLongOwnersWithoutMerging(t *testing.T) {
	ownerA := strings.Repeat("tenant", 100) + "a"
	ownerB := strings.Repeat("tenant", 100) + "b"
	dirA := SessionDirectoryName(ownerA)
	dirB := SessionDirectoryName(ownerB)
	if len(dirA) > maxSessionSegmentBytes || len(dirB) > maxSessionSegmentBytes {
		t.Fatalf("session segment too long: %d, %d", len(dirA), len(dirB))
	}
	if dirA == dirB {
		t.Fatal("different long owners collapsed to one directory")
	}
	root := t.TempDir()
	handle, err := Spill(SpillOptions{ToolName: "bash", SessionKey: ownerA, Content: "long-owner-secret", Root: root})
	if err != nil {
		t.Fatal(err)
	}
	page, err := Read(ReadOptions{ID: handle.ID, SessionKey: ownerA, Root: root})
	if err != nil || page.Content != "long-owner-secret" {
		t.Fatalf("long owner read-back failed: page=%+v err=%v", page, err)
	}
	if _, err := Read(ReadOptions{ID: handle.ID, SessionKey: ownerB, Root: root}); err == nil {
		t.Fatal("different long owner read another owner's result")
	}
}

func TestHandleIDKeepsUTF8ToolNameValid(t *testing.T) {
	root := t.TempDir()
	handle, err := Spill(SpillOptions{
		ToolName:   strings.Repeat("工具🙂", 20),
		SessionKey: "owner",
		Content:    "完整结果",
		Root:       root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !utf8.ValidString(handle.ID) || !utf8.ValidString(filepath.Base(handle.Path)) {
		t.Fatalf("invalid UTF-8 handle: id=%x path=%x", []byte(handle.ID), []byte(filepath.Base(handle.Path)))
	}
	page, err := Read(ReadOptions{ID: handle.ID, SessionKey: "owner", Root: root})
	if err != nil || page.Content != "完整结果" {
		t.Fatalf("UTF-8 handle read-back failed: page=%+v err=%v", page, err)
	}
}

func TestResolveRejectsSymlinkEscape(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation normally requires elevated Windows privileges")
	}
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(outside, []byte("secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	sessionDir := filepath.Join(dir, "owner-a")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(sessionDir, "escape.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if _, err := Read(ReadOptions{Path: link, SessionKey: "owner-a", Root: dir}); err == nil {
		t.Fatal("read followed a symlink outside the tool-result store")
	}
	if _, err := Read(ReadOptions{ID: "escape", SessionKey: "owner-a", Root: dir}); err == nil {
		t.Fatal("id resolution followed a symlink outside the tool-result store")
	}
}

func TestGetStoreStatsReportsDurableHandles(t *testing.T) {
	dir := t.TempDir()
	rawA := strings.Repeat("a", 200)
	rawB := strings.Repeat("乙", 100)
	for i, raw := range []string{rawA, rawB} {
		proj, err := Project(ProjectOptions{
			ToolName:   "bash",
			SessionKey: fmt.Sprintf("owner-%d", i),
			Content:    raw,
			Root:       dir,
			Limit:      16,
		})
		if err != nil || proj.Handle == nil {
			t.Fatalf("project %d: err=%v proj=%+v", i, err, proj)
		}
	}
	stats := GetStoreStats(dir)
	if stats.Files != 2 || stats.Bytes != int64(len(rawA)+len(rawB)) {
		t.Fatalf("store stats=%+v", stats)
	}
}

func TestGetStoreStatsInvalidatesAfterSpill(t *testing.T) {
	dir := t.TempDir()
	if got := GetStoreStats(dir); got.Files != 0 || got.Bytes != 0 {
		t.Fatalf("initial stats=%+v", got)
	}
	raw := strings.Repeat("payload", 100)
	handle, err := Spill(SpillOptions{ToolName: "bash", SessionKey: "owner", Content: raw, Root: dir})
	if err != nil || handle == nil {
		t.Fatalf("spill: handle=%+v err=%v", handle, err)
	}
	if got := GetStoreStats(dir); got.Files != 1 || got.Bytes != int64(len(raw)) {
		t.Fatalf("stats remained stale after spill: %+v", got)
	}
}

func TestGetStoreStatsDoesNotCacheScanRacingWithSpill(t *testing.T) {
	root := t.TempDir()
	rootAbs, err := storeRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	storeStatsMu.Lock()
	delete(storeStatsCache, rootAbs)
	generation := storeStatsGeneration[rootAbs]
	storeStatsMu.Unlock()

	// Model the snapshot-install phase of a scan that began before Spill.
	handle, err := Spill(SpillOptions{ToolName: "bash", SessionKey: "owner", Content: "new-result", Root: root})
	if err != nil || handle == nil {
		t.Fatalf("spill: handle=%+v err=%v", handle, err)
	}
	storeStatsMu.Lock()
	if storeStatsGeneration[rootAbs] == generation {
		storeStatsCache[rootAbs] = storeStatsCacheEntry{stats: StoreStats{}, scannedAt: time.Now()}
	}
	_, staleCached := storeStatsCache[rootAbs]
	storeStatsMu.Unlock()
	if staleCached {
		t.Fatal("a scan predating Spill installed stale cache state")
	}
	if got := GetStoreStats(root); got.Files != 1 || got.Bytes != int64(len("new-result")) {
		t.Fatalf("post-race stats=%+v", got)
	}
}

func TestRefreshStoreStatsSeesExternalChange(t *testing.T) {
	dir := t.TempDir()
	_ = GetStoreStats(dir)
	sessionDir := filepath.Join(dir, "owner")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "external.txt"), []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	if cached := GetStoreStats(dir); cached.Files != 0 {
		t.Fatalf("expected cached snapshot before explicit refresh: %+v", cached)
	}
	if fresh := RefreshStoreStats(dir); fresh.Files != 1 || fresh.Bytes != int64(len("external")) {
		t.Fatalf("refresh did not rescan: %+v", fresh)
	}
}

func TestGetStoreStatsIgnoresAtomicTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "owner")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, ".tmp123"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, "complete.txt"), []byte("complete"), 0o600); err != nil {
		t.Fatal(err)
	}
	stats := RefreshStoreStats(root)
	if stats.Files != 1 || stats.Bytes != int64(len("complete")) {
		t.Fatalf("temporary file counted as durable handle: %+v", stats)
	}
}

func TestSpillConcurrentWritesAreCompleteAndUnique(t *testing.T) {
	dir := t.TempDir()
	const workers = 64
	handles := make(chan *Handle, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			content := fmt.Sprintf("worker-%d:%s", i, strings.Repeat("完整🙂", 1000))
			handle, err := Spill(SpillOptions{ToolName: "bash", SessionKey: "owner", Content: content, Root: dir})
			if err != nil {
				errs <- err
				return
			}
			page, err := Read(ReadOptions{ID: handle.ID, SessionKey: "owner", Root: dir, Limit: MaxReadLimit})
			if err != nil || page.Truncated || page.Content != content {
				errs <- fmt.Errorf("read %s: err=%v truncated=%v content_match=%v", handle.ID, err, page.Truncated, page.Content == content)
				return
			}
			handles <- handle
		}()
	}
	wg.Wait()
	close(errs)
	close(handles)
	for err := range errs {
		t.Fatal(err)
	}
	seen := make(map[string]bool, workers)
	for handle := range handles {
		if seen[handle.ID] {
			t.Fatalf("duplicate handle id %q", handle.ID)
		}
		seen[handle.ID] = true
	}
	if len(seen) != workers {
		t.Fatalf("handles=%d want=%d", len(seen), workers)
	}
	entries, err := os.ReadDir(filepath.Join(dir, "owner"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != workers {
		t.Fatalf("stored files=%d want=%d", len(entries), workers)
	}
}

func TestReadLargeSparseFileUsesBoundedPage(t *testing.T) {
	dir := t.TempDir()
	sessionDir := filepath.Join(dir, "owner")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sessionDir, "large.txt")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	const total = int64(2 << 30)
	if err := file.Truncate(total); err != nil {
		_ = file.Close()
		t.Skipf("large sparse files unavailable: %v", err)
	}
	marker := []byte("甲🙂END")
	offset := total - int64(len(marker))
	if _, err := file.WriteAt(marker, offset); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := Read(ReadOptions{ID: "large", SessionKey: "owner", Root: dir, Offset: int(offset), Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.Content != "甲" || page.ReturnedBytes != len("甲") || page.TotalBytes != int(total) {
		t.Fatalf("unexpected sparse page: %+v content=%q", page, page.Content)
	}
}

func TestReadUsesCurrentFileSizeAfterAppend(t *testing.T) {
	root := t.TempDir()
	handle, err := Spill(SpillOptions{ToolName: "bash", SessionKey: "owner", Content: strings.Repeat("x", 1024), Root: root})
	if err != nil {
		t.Fatal(err)
	}
	file, err := os.OpenFile(handle.Path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString("changed"); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	page, err := Read(ReadOptions{ID: handle.ID, SessionKey: "owner", Root: root, Limit: 10})
	if err != nil || page.TotalBytes != 1031 {
		t.Fatalf("read should use the stable post-change file: page=%+v err=%v", page, err)
	}
}

func TestResolveMissingHandle(t *testing.T) {
	dir := t.TempDir()
	if _, err := Resolve("no_such_handle", "", "sess", dir); err == nil {
		t.Fatal("expected missing handle error")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
