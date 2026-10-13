package guiapp

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/RapidAI/CodeClaw/corelib/brand"
)

func TestArxivIDFromInput(t *testing.T) {
	cases := map[string]string{
		"2510.16079":                              "2510.16079",
		" 2510.16079 ":                            "2510.16079",
		"2510.16079v2":                            "2510.16079",
		"arXiv:2510.16079":                        "2510.16079",
		"arxiv:2510.16079v11":                     "2510.16079",
		"https://arxiv.org/abs/2510.16079":        "2510.16079",
		"https://arxiv.org/abs/2510.16079v3":      "2510.16079",
		"https://arxiv.org/pdf/2510.16079":        "2510.16079",
		"https://arxiv.org/pdf/2510.16079v1":      "2510.16079",
		"https://arxiv.org/pdf/2510.16079.pdf":    "2510.16079",
		"arxiv.org/abs/2510.16079":                "2510.16079",
		"https://arxiv.org/abs/2510.16079?ctx=1":  "2510.16079",
		"https://export.arxiv.org/abs/2510.16079": "2510.16079",
		"cs/0701001":                              "cs/0701001",
		"https://arxiv.org/abs/cs/0701001":        "cs/0701001",
		"math.GT/0309136":                         "math.GT/0309136",
		"Math.GT/0309136":                         "Math.GT/0309136",
		"https://arxiv.org/abs/math.GT/0309136":   "math.GT/0309136",
		"https://arxiv.org:443/abs/2510.16079":    "2510.16079",
		"<https://arxiv.org/abs/2510.16079>":      "2510.16079",
		"2510.16079.pdf":                          "2510.16079",
		// A link copied out of a chat or a note arrives as Markdown.
		"[Paper](https://arxiv.org/abs/2510.16079)":               "2510.16079",
		"see [cs/0701001](https://arxiv.org/abs/cs/0701001) here": "cs/0701001",
		"[x](https://arxiv.org/pdf/2510.16079v3)":                 "2510.16079",
		// Copied out of running text, the reference keeps its punctuation.
		"详见 arXiv:2510.16079。":                  "2510.16079",
		"See https://arxiv.org/abs/2510.16079.": "2510.16079",
		"参考 https://arxiv.org/abs/2510.16079，":  "2510.16079",
		"（2510.16079）":                          "2510.16079",
		"《https://arxiv.org/abs/2510.16079》":    "2510.16079",
		"“2510.16079”":                          "2510.16079",
		"arXiv:2510.16079!":                     "2510.16079",
		"2510.16079...":                         "2510.16079",
		// arXiv prints this exact shape on every abstract page ("Cite as:"), so
		// it is the most likely thing a user copies.
		"arXiv:2510.16079 [cs.CL]":                   "2510.16079",
		"Cite as: arXiv:2510.16079 [cs.CL]":          "2510.16079",
		"arXiv:2510.16079v2 [cs.CL]":                 "2510.16079",
		"arXiv:cs/0701001 [cs.CL]":                   "cs/0701001",
		"2510.16079 (2025)":                          "2510.16079",
		"cited 2510.16079, see above":                "2510.16079",
		"见 https://arxiv.org/abs/2510.16079 [cs.CL]": "2510.16079",
	}
	for in, want := range cases {
		if got := arxivIDFromInput(in); got != want {
			t.Fatalf("arxivIDFromInput(%q)=%q want %q", in, got, want)
		}
	}
}

func TestArxivIDFromInputRejects(t *testing.T) {
	// A look-alike host must not be able to steer the PDF download, and a
	// non-arXiv string must be reported as bad input instead of fetched.
	for _, in := range []string{
		"",
		"   ",
		"hello world",
		"2510.160",
		"251.16079",
		"2510.160790",
		"https://arxiv.org.evil.com/abs/2510.16079",
		"https://example.com/abs/2510.16079",
		"https://example.com/2510.16079",
		"https://arxiv.org/abs/",
		"https://arxiv.org/",
		"/abs/2510.16079",
		"abs/2510.16079",
		// Markdown wrappers are only unwrapped for a real http(s) URL: a
		// non-arXiv target must not be rewritten into a fetch.
		"[Paper](https://example.com/abs/2510.16079)",
		"[Paper](arxiv.org/abs/2510.16079)",
		"[Paper]()",
		"](",
		// Trailing punctuation trimming must never eat its way back into the
		// host check: these stay outside the whitelist either way.
		"https://arxiv.org.evil.com/abs/2510.16079.",
		"（https://example.com/abs/2510.16079）",
		// Punctuation-only input must not be trimmed down to empty and then
		// reported as a confusing "bad input" for a blank box.
		"。。。",
		"!!!",
		// Dropping leading words must not resurrect a rejected host: the
		// look-alike stays rejected at every prefix offset.
		"see https://arxiv.org.evil.com/abs/2510.16079 please",
		"参考 https://example.com/abs/2510.16079 这篇",
		"something https://arxiv.org.evil.com/abs/2510.16079 trailing words",
		// Trying each word separately must not launder a host either, and must
		// not accept an annotation on its own.
		"cite https://arxiv.org.evil.com/abs/2510.16079 [cs.CL]",
		"arXiv: [cs.CL]",
		"cite [cs.CL] 2510.16079x",
	} {
		if got := arxivIDFromInput(in); got != "" {
			t.Fatalf("arxivIDFromInput(%q)=%q want empty", in, got)
		}
	}
}

func TestArxivHostAllowed(t *testing.T) {
	for _, host := range []string{"arxiv.org", "export.arxiv.org", "ARXIV.ORG", "arxiv.org."} {
		if !arxivHostAllowed(host) {
			t.Fatalf("expected allowed: %s", host)
		}
	}
	for _, host := range []string{"", "arxiv.org.evil.com", "example.com", "notarxiv.org.example"} {
		if arxivHostAllowed(host) {
			t.Fatalf("expected rejected: %s", host)
		}
	}
}

// The folder carries the build's brand, so a TigerClaw or MetaStaff build does
// not file papers under another product's name. Composing it from a parameter
// keeps the empty-brand fallback testable in every build.
func TestArxivPaperFolderNameUsesBrand(t *testing.T) {
	if got := arxivPaperFolderNameFor("TigerClaw"); got != "TigerClaw 伴读" {
		t.Fatalf("got %q", got)
	}
	if got := arxivPaperFolderNameFor(""); got != "MaClaw 伴读" {
		t.Fatalf("empty brand must fall back to MaClaw, got %q", got)
	}
	if got := arxivPaperFolderNameFor("   "); got != "MaClaw 伴读" {
		t.Fatalf("blank brand must fall back to MaClaw, got %q", got)
	}
	// The shipped default build must keep the folder where earlier downloads
	// already are, and any build must agree with its own brand name.
	if brand.Current().ID == "maclaw" {
		if got := arxivPaperFolderName(); got != "MaClaw 伴读" {
			t.Fatalf("default build folder moved to %q", got)
		}
	}
	if got := arxivPaperFolderName(); !strings.HasPrefix(got, brand.Current().DisplayName) {
		t.Fatalf("folder %q does not carry the brand %q", got, brand.Current().DisplayName)
	}

	// The call site matters, not just the helper: the download folder must
	// actually be filed under the brand name. Reverting the call site to a
	// literal used to slip past this test, which only checked the helper.
	base := t.TempDir()
	dir, err := arxivPaperDirUnder(base)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(dir) != base {
		t.Fatalf("dir %q is not directly under %q", dir, base)
	}
	if filepath.Base(dir) != arxivPaperFolderName() {
		t.Fatalf("dir %q does not use the brand folder name %q", dir, arxivPaperFolderName())
	}
	if info, statErr := os.Stat(dir); statErr != nil || !info.IsDir() {
		t.Fatalf("the folder was not created: %v", statErr)
	}
}

func TestFileIsPDF(t *testing.T) {
	dir := t.TempDir()
	pdf := filepath.Join(dir, "a.pdf")
	if err := os.WriteFile(pdf, []byte("%PDF-1.7\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileIsPDF(pdf) {
		t.Fatal("expected PDF magic detected")
	}
	html := filepath.Join(dir, "b.pdf")
	if err := os.WriteFile(html, []byte("<html>not found</html>"), 0o644); err != nil {
		t.Fatal(err)
	}
	if fileIsPDF(html) {
		t.Fatal("an arXiv error page saved as .pdf must not pass")
	}
	if fileIsPDF(filepath.Join(dir, "missing.pdf")) {
		t.Fatal("missing file is not a PDF")
	}
	empty := filepath.Join(dir, "empty.pdf")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if fileIsPDF(empty) {
		t.Fatal("an empty file is not a PDF")
	}
	if fileIsPDF(dir) {
		t.Fatal("a directory is not a PDF")
	}
	// The spec tells readers to look for the header within the first 1024
	// bytes, so a producer that emits a blank line first must not be reported
	// as "no PDF, the paper may not exist".
	lead := filepath.Join(dir, "lead.pdf")
	if err := os.WriteFile(lead, []byte("\n\n%PDF-1.4 rest"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !fileIsPDF(lead) {
		t.Fatal("a PDF with a leading blank line was rejected")
	}
	// Tolerating a prefix must not mean accepting the header anywhere: a file
	// that only mentions %PDF- past the window is not a PDF.
	late := filepath.Join(dir, "late.pdf")
	pad := make([]byte, arxivPDFHeaderScan)
	for i := range pad {
		pad[i] = ' '
	}
	if err := os.WriteFile(late, append(pad, []byte("%PDF-1.4")...), 0o644); err != nil {
		t.Fatal(err)
	}
	if fileIsPDF(late) {
		t.Fatal("the header past the scan window must not count")
	}
}

func TestFileCompanionOpenArxivDownloadsAndReuses(t *testing.T) {
	body := []byte("%PDF-1.7 fake paper body")
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if !strings.HasSuffix(r.URL.Path, "/2510.16079") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	prevURL := arxivPDFBaseURL
	arxivPDFBaseURL = srv.URL + "/pdf/"
	t.Cleanup(func() { arxivPDFBaseURL = prevURL })

	dir := t.TempDir()
	prevHook := arxivPaperDirHook
	arxivPaperDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { arxivPaperDirHook = prevHook })

	app := &App{}
	got, err := app.FileCompanionOpenArxiv("https://arxiv.org/abs/2510.16079v2")
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	want := filepath.Join(dir, "2510.16079.pdf")
	if filepath.Clean(got) != filepath.Clean(want) {
		t.Fatalf("path=%q want %q", got, want)
	}
	onDisk, err := os.ReadFile(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(onDisk) != string(body) {
		t.Fatalf("body=%q", onDisk)
	}
	// No .part left behind.
	if _, err := os.Stat(want + ".part"); !os.IsNotExist(err) {
		t.Fatalf("staged download left behind: %v", err)
	}
	// A second request for the same paper must not hit the network again.
	if _, err := app.FileCompanionOpenArxiv("2510.16079"); err != nil {
		t.Fatalf("reuse: %v", err)
	}
	if hits != 1 {
		t.Fatalf("server hits=%d want 1 (cached paper must not re-download)", hits)
	}
}

// Two old-style IDs share the same numeric tail. If the on-disk name dropped
// the archive prefix they would collide and the reuse check would return the
// wrong paper.
func TestFileCompanionOpenArxivOldIDsDoNotCollide(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_, _ = w.Write([]byte("%PDF-1.7 " + r.URL.Path))
	}))
	defer srv.Close()
	prevURL := arxivPDFBaseURL
	arxivPDFBaseURL = srv.URL + "/pdf/"
	t.Cleanup(func() { arxivPDFBaseURL = prevURL })
	dir := t.TempDir()
	prevHook := arxivPaperDirHook
	arxivPaperDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { arxivPaperDirHook = prevHook })

	app := &App{}
	cs, err := app.FileCompanionOpenArxiv("cs/0701001")
	if err != nil {
		t.Fatalf("cs: %v", err)
	}
	math, err := app.FileCompanionOpenArxiv("math/0701001")
	if err != nil {
		t.Fatalf("math: %v", err)
	}
	if cs == math {
		t.Fatalf("old-style IDs collided on %q", cs)
	}
	if filepath.Base(cs) != "cs_0701001.pdf" || filepath.Base(math) != "math_0701001.pdf" {
		t.Fatalf("names=%q %q", filepath.Base(cs), filepath.Base(math))
	}
	// Both must have been fetched; the second must not reuse the first.
	if hits != 2 {
		t.Fatalf("server hits=%d want 2", hits)
	}
	// And each must still be reusable without another fetch.
	if _, err := app.FileCompanionOpenArxiv("cs/0701001"); err != nil {
		t.Fatalf("reuse cs: %v", err)
	}
	if hits != 2 {
		t.Fatalf("server hits=%d want 2 after reuse", hits)
	}
}

func TestArxivPaperFileName(t *testing.T) {
	if got := arxivPaperFileName("2510.16079"); got != "2510.16079.pdf" {
		t.Fatalf("new id=%q", got)
	}
	if got := arxivPaperFileName("cs/0701001"); got != "cs_0701001.pdf" {
		t.Fatalf("old id=%q", got)
	}
	if got := arxivPaperFileName(""); got != "paper.pdf" {
		t.Fatalf("empty=%q", got)
	}
}

func TestFileCompanionOpenArxivRejectsBadInput(t *testing.T) {
	if _, err := (&App{}).FileCompanionOpenArxiv("not a paper"); err == nil {
		t.Fatal("expected rejection")
	} else if !strings.Contains(err.Error(), "Arxiv") {
		t.Fatalf("error should name the expected input: %v", err)
	}
}

func TestFileCompanionOpenArxivReportsMissingPaper(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()
	prevURL := arxivPDFBaseURL
	arxivPDFBaseURL = srv.URL + "/pdf/"
	t.Cleanup(func() { arxivPDFBaseURL = prevURL })
	prevHook := arxivPaperDirHook
	arxivPaperDirHook = func() (string, error) { return t.TempDir(), nil }
	t.Cleanup(func() { arxivPaperDirHook = prevHook })

	_, err := (&App{}).FileCompanionOpenArxiv("2510.16079")
	if err == nil {
		t.Fatal("expected failure")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("404 should be reported: %v", err)
	}
}

func TestFileCompanionOpenArxivRejectsNonPDFBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("<html>Article identifier not recognized</html>"))
	}))
	defer srv.Close()
	prevURL := arxivPDFBaseURL
	arxivPDFBaseURL = srv.URL + "/pdf/"
	t.Cleanup(func() { arxivPDFBaseURL = prevURL })
	dir := t.TempDir()
	prevHook := arxivPaperDirHook
	arxivPaperDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { arxivPaperDirHook = prevHook })

	if _, err := (&App{}).FileCompanionOpenArxiv("2510.16079"); err == nil {
		t.Fatal("an HTML error page must not be opened as a paper")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("failed download left files behind: %v", entries)
	}
}

// A rename failure must not delete the paper that was just downloaded: the
// bytes are good, only the bookkeeping failed, and the next attempt reuses the
// staged file instead of fetching tens of megabytes again.
func TestFileCompanionOpenArxivKeepsDownloadWhenRenameFails(t *testing.T) {
	body := []byte("%PDF-1.7 keep me")
	// The counter lives in the handler from the start: Serve captures the
	// handler when it starts, so swapping srv.Config.Handler later would not
	// take effect and the "did it download again" check would silently pass.
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	prevURL := arxivPDFBaseURL
	arxivPDFBaseURL = srv.URL + "/pdf/"
	t.Cleanup(func() { arxivPDFBaseURL = prevURL })
	dir := t.TempDir()
	prevHook := arxivPaperDirHook
	arxivPaperDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { arxivPaperDirHook = prevHook })

	// A directory occupying the target name makes os.Rename fail while the
	// download itself succeeds.
	if err := os.Mkdir(filepath.Join(dir, "2510.16079.pdf"), 0o755); err != nil {
		t.Fatal(err)
	}

	_, err := (&App{}).FileCompanionOpenArxiv("2510.16079")
	if err == nil {
		t.Fatal("expected a rename failure")
	}
	if !strings.Contains(err.Error(), "保存失败") {
		t.Fatalf("error=%v", err)
	}
	// The staging file gets a unique name, so look for whatever survived
	// rather than hard-coding one.
	entries, listErr := os.ReadDir(dir)
	if listErr != nil {
		t.Fatal(listErr)
	}
	var kept []byte
	var keptName string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".part") {
			continue
		}
		if kept != nil {
			t.Fatalf("expected one surviving staging file, found another: %s", entry.Name())
		}
		keptName = entry.Name()
		var readErr error
		kept, readErr = os.ReadFile(filepath.Join(dir, entry.Name()))
		if readErr != nil {
			t.Fatalf("the downloaded paper was discarded: %v", readErr)
		}
	}
	if kept == nil {
		t.Fatalf("the downloaded paper was discarded; dir contents=%v", names(entries))
	}
	if string(kept) != string(body) {
		t.Fatalf("staged file %s body=%q", keptName, kept)
	}
	// The kept name must be deterministic: a random one would never be found
	// again, so the bytes would sit there forever and be re-downloaded anyway.
	if keptName != "2510.16079.pdf"+arxivKeptStageSuffix {
		t.Fatalf("kept staging name=%q, want the deterministic one", keptName)
	}

	// The point of keeping the bytes is that the retry is cheap: the second
	// attempt must file the parked paper without going back to the network.
	// Freeing the name is what the user would do by removing the blocker.
	if err := os.Remove(filepath.Join(dir, "2510.16079.pdf")); err != nil {
		t.Fatal(err)
	}
	hits = 0
	got, err := (&App{}).FileCompanionOpenArxiv("2510.16079")
	if err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if hits != 0 {
		t.Fatalf("the retry downloaded again (%d requests) instead of reusing the kept bytes", hits)
	}
	if fileIsPDF(got) == false {
		t.Fatalf("retry returned %q which is not the paper", got)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "2510.16079.pdf"+arxivKeptStageSuffix)); !os.IsNotExist(statErr) {
		t.Fatalf("the kept file was not consumed by the retry")
	}
}

// With the bytes already parked, a still-blocked target cannot be fixed by
// fetching them again: the attempt must fail immediately instead of spending
// the user's bandwidth on a download that cannot land.
func TestFileCompanionOpenArxivDoesNotRedownloadWhileTargetIsBlocked(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits++
		_, _ = w.Write([]byte("%PDF-1.7 parked"))
	}))
	defer srv.Close()
	prevURL := arxivPDFBaseURL
	arxivPDFBaseURL = srv.URL + "/pdf/"
	t.Cleanup(func() { arxivPDFBaseURL = prevURL })
	dir := t.TempDir()
	prevHook := arxivPaperDirHook
	arxivPaperDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { arxivPaperDirHook = prevHook })

	if err := os.Mkdir(filepath.Join(dir, "2510.16079.pdf"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "2510.16079.pdf"+arxivKeptStageSuffix), []byte("%PDF-1.7 parked"), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := (&App{}).FileCompanionOpenArxiv("2510.16079")
	if err == nil || !strings.Contains(err.Error(), "保存失败") {
		t.Fatalf("err=%v", err)
	}
	if hits != 0 {
		t.Fatalf("re-downloaded %d time(s) although the paper could not be filed", hits)
	}
}

// A process killed mid-download leaves a staging file whose name is random, so
// nothing would ever collect it. Attempts must reap what only a dead download
// could have left, without touching a file a live attempt still owns.
func TestFileCompanionOpenArxivReapsStaleStaging(t *testing.T) {
	body := []byte("%PDF-1.7 fresh")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	prevURL := arxivPDFBaseURL
	arxivPDFBaseURL = srv.URL + "/pdf/"
	t.Cleanup(func() { arxivPDFBaseURL = prevURL })
	dir := t.TempDir()
	prevHook := arxivPaperDirHook
	arxivPaperDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { arxivPaperDirHook = prevHook })

	stale := filepath.Join(dir, "2510.16079.pdf.111.part")
	fresh := filepath.Join(dir, "2510.16079.pdf.222.part")
	// Only a valid PDF is ever parked under the deterministic name, so a
	// non-PDF there is debris and must not shadow the paper forever.
	debris := filepath.Join(dir, "2510.16079.pdf"+arxivKeptStageSuffix)
	for _, path := range []string{stale, fresh, debris} {
		if err := os.WriteFile(path, []byte("%PDF-1.7 half"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(debris, []byte("not a pdf at all"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * arxivStaleStageGrace)
	for _, path := range []string{stale, debris} {
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}

	if _, err := (&App{}).FileCompanionOpenArxiv("2510.16079"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatalf("an abandoned staging file survived: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("a staging file a live attempt could still own was removed: %v", err)
	}
	if _, err := os.Stat(debris); !os.IsNotExist(err) {
		t.Fatalf("non-PDF debris under the parked name survived: %v", err)
	}
}

// The fetcher writes straight to the SavePath it is given, so two downloads of
// one ID sharing a fixed staging name would interleave into a file whose 5-byte
// header still reads %PDF- — a silently corrupt paper that passes fileIsPDF.
// Each attempt must therefore stage under its own name.
func TestFileCompanionOpenArxivStagingNameIsUniquePerAttempt(t *testing.T) {
	// Record the SavePath each attempt hands to the fetcher: those must never
	// collide, because os.Create on a shared path interleaves two writers into
	// one file that still starts with the PDF magic.
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(150 * time.Millisecond)
		_, _ = w.Write([]byte("%PDF-1.7 ok"))
	}))
	defer slow.Close()
	dir := t.TempDir()
	prevHook := arxivPaperDirHook
	arxivPaperDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { arxivPaperDirHook = prevHook })
	prevURL := arxivPDFBaseURL
	arxivPDFBaseURL = slow.URL + "/pdf/"
	t.Cleanup(func() { arxivPDFBaseURL = prevURL })

	// A directory under the target name blocks every rename, so each attempt
	// keeps its staging file — exactly the state this test needs to inspect.
	if err := os.Mkdir(filepath.Join(dir, "2510.16079.pdf"), 0o755); err != nil {
		t.Fatal(err)
	}

	const parallel = 4
	// Every attempt records the staging path it was given. Sharing one would
	// mean two writers on one file, which is exactly the corruption this guards
	// against; asserting on the file system afterwards cannot see it, because
	// a successful download deletes its staging file before the next starts.
	var mu sync.Mutex
	seen := map[string]int{}
	prevSavePath := arxivSavePathHook
	arxivSavePathHook = func(path string) {
		mu.Lock()
		defer mu.Unlock()
		seen[path]++
	}
	t.Cleanup(func() { arxivSavePathHook = prevSavePath })

	var wg sync.WaitGroup
	for i := 0; i < parallel; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := (&App{}).FileCompanionOpenArxiv("2510.16079"); err == nil {
				t.Error("expected the blocked rename to fail")
			}
		}()
	}
	wg.Wait()

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != parallel {
		t.Fatalf("%d concurrent attempts shared %d staging file(s): %v", parallel, len(seen), seen)
	}
	for path, count := range seen {
		if count != 1 {
			t.Fatalf("staging file %s was handed to %d attempts", path, count)
		}
		if filepath.Ext(path) != ".part" {
			t.Fatalf("unexpected staging name %s", path)
		}
	}
}

func names(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}

func TestArxivDownloadErrorText(t *testing.T) {
	if got := arxivDownloadErrorText(nil); got != "下载失败" {
		t.Fatalf("nil err=%q", got)
	}
	cases := map[string]string{
		"HTTP 404: Not Found":       "404",
		"HTTP 403 forbidden":        "拒绝",
		"context deadline exceeded": "超时",
		"status code 404":           "404",
		"403 Forbidden":             "拒绝",
		"rate limited, blocked":     "拒绝",
	}
	for in, want := range cases {
		if got := arxivDownloadErrorText(errString(in)); !strings.Contains(got, want) {
			t.Fatalf("arxivDownloadErrorText(%q)=%q want contains %q", in, got, want)
		}
	}
}

// A transport error embeds the request URL, and an arXiv ID is just a digit
// run. 2404.12345 reads as HTTP 404 and 1403.2040 as HTTP 403 to a naive
// substring match, which reported a DNS failure as "no such paper" and sent
// users off to correct an ID that was already right.
func TestArxivDownloadErrorTextIgnoresTheIDInsideTheURL(t *testing.T) {
	cases := []struct {
		err  string
		want string
	}{
		// DNS and TLS failures must fall through to the raw message; before
		// the URL was stripped, each of these was reported as a 404/403.
		{`Get "https://arxiv.org/pdf/2404.12345": dial tcp: lookup arxiv.org: no such host`, `dial tcp`},
		{`Get "https://arxiv.org/pdf/2510.40499": dial tcp: lookup arxiv.org: no such host`, `dial tcp`},
		{`Get "https://arxiv.org/pdf/1403.2040": tls handshake failure`, `tls handshake`},
		// The real thing still classifies, whether or not a URL is present.
		{`unexpected status code 404`, "404"},
		{`Get "https://arxiv.org/pdf/2404.12345": unexpected status code 404`, "404"},
		{`Get "https://arxiv.org/pdf/1403.2040": 403 forbidden`, "拒绝"},
		{`Get "https://arxiv.org/pdf/2404.12345": context deadline exceeded`, "超时"},
	}
	for _, c := range cases {
		got := arxivDownloadErrorText(errString(c.err))
		if !strings.Contains(got, c.want) {
			t.Fatalf("arxivDownloadErrorText(%q)=%q want contains %q", c.err, got, c.want)
		}
		if strings.Contains(got, "找不到这篇论文") && !strings.Contains(c.err, "404") {
			t.Fatalf("arxivDownloadErrorText(%q)=%q wrongly claims a missing paper", c.err, got)
		}
	}
}
