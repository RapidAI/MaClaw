package guiapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib/websearch"
	"github.com/wailsapp/wails/v2/pkg/options"
)

func TestCompanionPaperURLDoesNotHandoffOrDownload(t *testing.T) {
	raw := "maclaw://companion-paper?url=https://arxiv.org/abs/2510.16079"
	args := []string{"MaClaw", raw}
	if !companionPaperFromArgs(args) {
		t.Fatal("expected companion-paper")
	}
	if creditGiftFromArgs(args).Code != "" {
		t.Fatal("companion-paper produced a credit code")
	}
	if referralHandoffFromArgs(args).Handoff != "" {
		t.Fatal("companion-paper produced an onboarding handoff")
	}
	if cloudWorkspaceShareFromArgs(args).Token != "" {
		t.Fatal("companion-paper produced a share token")
	}
	called := false
	prev := companionPaperExecHook
	companionPaperExecHook = func(argv []string) error {
		called = true
		return nil
	}
	t.Cleanup(func() { companionPaperExecHook = prev })
	if called {
		t.Fatal("parsing the URL downloaded a paper")
	}
	for _, other := range []string{
		"maclaw://credit/ABCDEF2345",
		"maclaw://onboarding?referral_handoff=0123456789abcdef",
		"maclaw://cloud-workspace-share?token=tok",
		"maclaw://import?token=cd_x",
	} {
		if companionPaperFromArgs([]string{"MaClaw", other}) {
			t.Fatalf("%s matched companion-paper", other)
		}
	}
	text := readRepoFile(t, filepath.Join("build", "windows", "installer", "multiarch.nsi"))
	openAt := strings.Index(text, `Software\Classes\maclaw\shell\open\command`)
	if openAt < 0 {
		t.Fatal("protocol command missing")
	}
	line := text[openAt:]
	if i := strings.Index(line, "\n"); i >= 0 {
		line = line[:i]
	}
	if strings.Contains(line, "open-file") || strings.Contains(line, "companion-paper") {
		t.Fatalf("protocol command changed: %s", line)
	}
}

func TestClassifyCompanionPaperNotice(t *testing.T) {
	got := classifyDesktopLaunchForOS([]string{"MaClaw", "companion-paper-notice", "no-pdf"}, "windows")
	if got.Mode != desktopLaunchFileCompanion || got.Notice != "no-pdf" || len(got.Paths) != 0 || got.StartHidden {
		t.Fatalf("notice launch = %#v", got)
	}
	unknown := classifyDesktopLaunchForOS([]string{"MaClaw", "companion-paper-notice", "try again"}, "darwin")
	if unknown.Mode != desktopLaunchMain || unknown.StartHidden || unknown.Notice != "" {
		t.Fatalf("unknown notice = %#v", unknown)
	}
	app := &App{}
	app.onFileCompanionSecondInstance(optionsSecond("companion-paper-notice", "no-pdf"))
	if app.fileCompanion == nil || len(app.fileCompanion.notices) != 1 || app.fileCompanion.notices[0] != "no-pdf" {
		t.Fatalf("notices = %#v", app.fileCompanion)
	}
	if app.fileCompanion.shows != 0 {
		t.Fatal("notice opened the 打开 panel")
	}
	boot := app.GetFileCompanionBoot()
	if boot.Notice != "no-pdf" || len(boot.Paths) != 0 || boot.Mode != desktopLaunchFileCompanion {
		t.Fatalf("boot = %#v", boot)
	}
	app.fileCompanion.uiReady = true
	app.onFileCompanionSecondInstance(optionsSecond("companion-paper-notice", "timeout"))
	if app.fileCompanion.shows != 0 {
		t.Fatal("ready notice still opened the panel")
	}
	if again := app.GetFileCompanionBoot(); again.Notice != "" {
		t.Fatalf("delivered notice stayed on boot: %#v", again)
	}
}

func optionsSecond(args ...string) options.SecondInstanceData {
	return options.SecondInstanceData{Args: args}
}

func TestCompanionPaperNoticeSecondInstance(t *testing.T) {
	// Replaced by TestClassifyCompanionPaperNotice. Kept so the options import stays used
	// through the real second-instance call above.
}

func TestAcquireCompanionPaperRejectsPrivateURLs(t *testing.T) {
	dir := t.TempDir()
	restorePaperHooks(t, dir, func(ctx context.Context, rawURL string, opts *websearch.FetchOptions) (*websearch.FetchResult, error) {
		t.Errorf("fetch called for %s", rawURL)
		return nil, errors.New("fetched")
	})
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		t.Error("loopback handler was called")
	}))
	defer srv.Close()
	cases := []string{
		srv.URL + "/paper",
		srv.URL + "/paper.pdf",
		"http://10.0.0.1/x.pdf",
		"http://169.254.169.254/x.pdf",
		"http://user:pw@example.org/x.pdf",
		"file:///etc/passwd",
		"javascript:alert(1)",
	}
	var execs [][]string
	prevExec := companionPaperExecHook
	companionPaperExecHook = func(argv []string) error {
		execs = append(execs, append([]string(nil), argv...))
		return nil
	}
	t.Cleanup(func() { companionPaperExecHook = prevExec })
	for _, raw := range cases {
		execs = nil
		if code := acceptCompanionPaper(raw); code != companionPaperCodeBadScheme {
			t.Fatalf("%s code=%s", raw, code)
		}
		if len(execs) != 1 || execs[0][1] != companionPaperNoticeArg || execs[0][2] != companionPaperCodeBadScheme {
			t.Fatalf("%s exec=%#v", raw, execs)
		}
	}
	if hits != 0 {
		t.Fatalf("loopback hits=%d", hits)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("paper dir gained files: %v", entries)
	}
}

func TestAcquireCompanionPaperRequestURLKeepsOrder(t *testing.T) {
	raw := "https://User:pw@CDN.Example.ORG/dl/paper.pdf?b=1&utm_source=x&a=hello%20w&token=secret&key=k#frag"
	got := paperRequestURL(raw)
	want := "https://cdn.example.org/dl/paper.pdf?b=1&a=hello%20w"
	if got != want {
		t.Fatalf("request %s", got)
	}
	canon := canonicalPaperURL(raw)
	if canon != "https://cdn.example.org/dl/paper.pdf?a=hello+w&b=1" {
		t.Fatalf("canonical %s", canon)
	}
	if companionPaperFileName(canon) != companionPaperFileName(canonicalPaperURL("https://cdn.example.org/dl/paper.pdf?a=hello%20w&b=1")) {
		t.Fatal("filename drifted")
	}
}

func TestAcquireCompanionPaperSavesPublicPDF(t *testing.T) {
	dir := t.TempDir()
	const page = "https://aclanthology.org/2024.acl-long.1.pdf?utm_source=x&token=secret#frag"
	restorePaperHooks(t, dir, func(ctx context.Context, rawURL string, opts *websearch.FetchOptions) (*websearch.FetchResult, error) {
		if !opts.PublicNetworkOnly || opts.HubDownload != nil {
			t.Fatalf("opts=%+v", opts)
		}
		if opts.TimeoutS != companionPaperPDFTimeoutS || opts.MaxBytes != companionPaperPDFMaxBytes {
			t.Fatalf("limits timeout=%d max=%d", opts.TimeoutS, opts.MaxBytes)
		}
		if rawURL != "https://aclanthology.org/2024.acl-long.1.pdf" {
			t.Fatalf("url=%s", rawURL)
		}
		if err := os.WriteFile(opts.SavePath, []byte("%PDF-1.7 paper"), 0o644); err != nil {
			return nil, err
		}
		return &websearch.FetchResult{URL: rawURL, SavedTo: opts.SavePath, ContentType: "application/pdf"}, nil
	})
	var execs [][]string
	prev := companionPaperExecHook
	companionPaperExecHook = func(argv []string) error {
		execs = append(execs, append([]string(nil), argv...))
		return nil
	}
	t.Cleanup(func() { companionPaperExecHook = prev })
	if code := acceptCompanionPaper(page); code != "" {
		t.Fatal(code)
	}
	name := companionPaperFileName(canonicalPaperURL(page))
	if !strings.HasPrefix(name, "paper-") || !strings.HasSuffix(name, ".pdf") || len(name) != len("paper-")+16+len(".pdf") {
		t.Fatalf("name=%s", name)
	}
	same := companionPaperFileName(canonicalPaperURL("https://aclanthology.org/2024.acl-long.1.pdf"))
	if name != same {
		t.Fatalf("canonical name %s != %s", name, same)
	}
	if len(execs) != 1 || execs[0][1] != "open-file" || !strings.HasSuffix(execs[0][2], name) {
		t.Fatalf("exec=%#v", execs)
	}
	if !fileIsPDF(filepath.Join(dir, name)) {
		t.Fatal("pdf was not saved")
	}
}

func TestAcquireCompanionPaperMapsDownloadFailures(t *testing.T) {
	dir := t.TempDir()
	mode := "pdf"
	restorePaperHooks(t, dir, func(ctx context.Context, rawURL string, opts *websearch.FetchOptions) (*websearch.FetchResult, error) {
		switch mode {
		case "html":
			return &websearch.FetchResult{URL: rawURL, ContentType: "text/html", Content: "<html>no pdf</html>"}, nil
		case "not-pdf":
			if err := os.WriteFile(opts.SavePath, []byte("not a pdf"), 0o644); err != nil {
				return nil, err
			}
			return &websearch.FetchResult{SavedTo: opts.SavePath, ContentType: "application/pdf"}, nil
		case "big":
			return nil, errors.New("file exceeds max_bytes limit (26214400 bytes)")
		case "timeout":
			return nil, context.DeadlineExceeded
		default:
			return nil, errors.New("fetched")
		}
	})
	var execs [][]string
	prev := companionPaperExecHook
	companionPaperExecHook = func(argv []string) error {
		execs = append(execs, append([]string(nil), argv...))
		return nil
	}
	t.Cleanup(func() { companionPaperExecHook = prev })
	checks := []struct {
		raw  string
		set  string
		code string
	}{
		{"https://example.org/notes", "html", companionPaperCodeNoPDF},
		{"https://aclanthology.org/2024.acl-long.1.pdf", "not-pdf", companionPaperCodeNotPDF},
		{"https://aclanthology.org/P19-1478.pdf", "big", companionPaperCodeTooBig},
		{"https://aclanthology.org/P19-1479.pdf", "timeout", companionPaperCodeTimeout},
	}
	for _, check := range checks {
		mode = check.set
		execs = nil
		if code := acceptCompanionPaper(check.raw); code != check.code {
			t.Fatalf("%s code=%s want %s", check.raw, code, check.code)
		}
		if len(execs) != 1 || execs[0][1] != companionPaperNoticeArg || execs[0][2] != check.code {
			t.Fatalf("%s exec=%#v", check.raw, execs)
		}
	}
}

func TestAcquireCompanionPaperSkipsLoopbackCitation(t *testing.T) {
	dir := t.TempDir()
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hits++
		t.Error("citation host was called")
	}))
	defer srv.Close()
	restorePaperHooks(t, dir, func(ctx context.Context, rawURL string, opts *websearch.FetchOptions) (*websearch.FetchResult, error) {
		if strings.Contains(rawURL, "127.0.0.1") {
			t.Fatalf("dialed %s", rawURL)
		}
		body := `<html><head><meta name="citation_pdf_url" content="` + srv.URL + `/paper.pdf"></head></html>`
		return &websearch.FetchResult{URL: rawURL, ContentType: "text/html; charset=utf-8", Content: body}, nil
	})
	prev := companionPaperExecHook
	companionPaperExecHook = func(argv []string) error { return nil }
	t.Cleanup(func() { companionPaperExecHook = prev })
	if code := acceptCompanionPaper("https://example.org/abs/1"); code != companionPaperCodeBadScheme {
		t.Fatalf("code=%s", code)
	}
	if hits != 0 {
		t.Fatalf("hits=%d", hits)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("files=%v", namesOf(entries))
	}
}

func TestAcquireCompanionPaperUsesCitationAndArxivName(t *testing.T) {
	dir := t.TempDir()
	restorePaperHooks(t, dir, func(ctx context.Context, rawURL string, opts *websearch.FetchOptions) (*websearch.FetchResult, error) {
		if strings.TrimSpace(opts.SavePath) == "" {
			if !opts.RawBody || !opts.PublicNetworkOnly || opts.HubDownload != nil {
				t.Fatalf("probe opts=%+v", opts)
			}
			body := `<html><meta property="citation_pdf_url" content="https://aclanthology.org/2024.acl-long.1.pdf"><link rel="alternate" type="application/pdf" href="/local.pdf"></html>`
			return &websearch.FetchResult{URL: rawURL, ContentType: "text/html", Content: body}, nil
		}
		if opts.RawBody {
			t.Fatalf("download opts=%+v", opts)
		}
		if !strings.Contains(rawURL, "2024.acl-long.1.pdf") {
			t.Fatalf("candidate=%s", rawURL)
		}
		if err := os.WriteFile(opts.SavePath, []byte("%PDF-1.4 cited"), 0o644); err != nil {
			return nil, err
		}
		return &websearch.FetchResult{SavedTo: opts.SavePath, ContentType: "application/pdf"}, nil
	})
	path, code := acquireCompanionPaper("https://example.org/paper")
	if code != "" {
		t.Fatal(code)
	}
	if !strings.HasSuffix(path, companionPaperFileName(canonicalPaperURL("https://example.org/paper"))) {
		t.Fatalf("path=%s", path)
	}
	if !fileIsPDF(path) {
		t.Fatal("saved file is not a pdf")
	}
}

func TestPublicArxivSkipsStrategyAndKeepsIDFileName(t *testing.T) {
	dir := t.TempDir()
	prevDir := arxivPaperDirHook
	arxivPaperDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { arxivPaperDirHook = prevDir })
	probes := 0
	prevProbe := arxivStrategyProbe
	arxivStrategyProbe = func() { probes++ }
	t.Cleanup(func() { arxivStrategyProbe = prevProbe })
	prevFetch := paperFetchHook
	paperFetchHook = func(ctx context.Context, rawURL string, opts *websearch.FetchOptions) (*websearch.FetchResult, error) {
		if !opts.PublicNetworkOnly || opts.HubDownload != nil {
			t.Fatalf("opts=%+v", opts)
		}
		if !strings.Contains(rawURL, "2510.16079") {
			t.Fatalf("url=%s", rawURL)
		}
		if err := os.WriteFile(opts.SavePath, []byte("%PDF-1.4 arxiv"), 0o644); err != nil {
			return nil, err
		}
		return &websearch.FetchResult{SavedTo: opts.SavePath, ContentType: "application/pdf"}, nil
	}
	t.Cleanup(func() { paperFetchHook = prevFetch })
	app := &App{imHandler: &IMMessageHandler{}}
	got, err := app.openArxivPaper("https://arxiv.org/pdf/2510.16079.pdf", arxivOpenOptions{PublicNetworkOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	if probes != 0 {
		t.Fatalf("strategy probes=%d", probes)
	}
	if !strings.HasSuffix(got, "2510.16079.pdf") {
		t.Fatalf("path=%s", got)
	}
	if err := os.Remove(got); err != nil {
		t.Fatal(err)
	}
	body := []byte("%PDF-1.4 in-app")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	prevURL := arxivPDFBaseURL
	arxivPDFBaseURL = srv.URL + "/pdf/"
	defer func() { arxivPDFBaseURL = prevURL }()
	probes = 0
	app.FileCompanionOpenArxiv("2510.16079")
	if probes != 1 {
		t.Fatalf("in-app strategy probes=%d", probes)
	}
}

func TestArxivRenameFailureRechecksExistingPDF(t *testing.T) {
	body := []byte("%PDF-1.7 fresh")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(body)
	}))
	defer srv.Close()
	prevURL := arxivPDFBaseURL
	arxivPDFBaseURL = srv.URL + "/pdf/"
	t.Cleanup(func() { arxivPDFBaseURL = prevURL })
	dir := t.TempDir()
	prevDir := arxivPaperDirHook
	arxivPaperDirHook = func() (string, error) { return dir, nil }
	t.Cleanup(func() { arxivPaperDirHook = prevDir })
	target := filepath.Join(dir, "2510.16079.pdf")
	prevRename := arxivRenameHook
	arxivRenameHook = func(oldPath, newPath string) error {
		if newPath == target {
			if err := os.WriteFile(newPath, []byte("%PDF-1.4 winner"), 0o644); err != nil {
				return err
			}
			return errors.New("busy")
		}
		return os.Rename(oldPath, newPath)
	}
	t.Cleanup(func() { arxivRenameHook = prevRename })
	got, err := (&App{}).FileCompanionOpenArxiv("2510.16079")
	if err != nil {
		t.Fatal(err)
	}
	if got != target || !fileIsPDF(got) {
		t.Fatalf("got=%s err=%v", got, err)
	}
}

func restorePaperHooks(t *testing.T, dir string, fetch func(context.Context, string, *websearch.FetchOptions) (*websearch.FetchResult, error)) {
	t.Helper()
	prevDir := arxivPaperDirHook
	prevFetch := paperFetchHook
	arxivPaperDirHook = func() (string, error) { return dir, nil }
	paperFetchHook = fetch
	t.Cleanup(func() {
		arxivPaperDirHook = prevDir
		paperFetchHook = prevFetch
	})
}

func namesOf(entries []os.DirEntry) []string {
	out := make([]string, 0, len(entries))
	for _, entry := range entries {
		out = append(out, entry.Name())
	}
	return out
}
