package guiapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecodeGitHubReleaseSummariesAcceptsLargeCatalogue(t *testing.T) {
	asset := strings.Repeat("x", 512)
	var b bytes.Buffer
	b.WriteString("[{\"tag_name\":\"V1\",\"published_at\":\"2026-08-28T00:00:00Z\",\"assets\":[")
	for i := 0; i < 12000; i++ {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "{\"name\":\"asset-%d\",\"browser_download_url\":\"https://example.invalid/%s\"}", i, asset)
	}
	b.WriteString("]}]")
	got, err := decodeGitHubReleaseSummaries(&b)
	if err != nil {
		t.Fatalf("decodeGitHubReleaseSummaries() error: %v", err)
	}
	if len(got) != 1 || len(got[0].Assets) != 12000 {
		t.Fatalf("decoded catalogue size = %d/%d", len(got), len(got[0].Assets))
	}
}

type prefixThenFailReader struct {
	data []byte
	off  int
	fail error
}

func (r *prefixThenFailReader) Read(p []byte) (int, error) {
	if r.off >= len(r.data) {
		return 0, r.fail
	}
	n := copy(p, r.data[r.off:])
	r.off += n
	return n, nil
}

func TestDecodeGitHubReleaseSummariesKeepsACompletePrefixWhenTheReadFails(t *testing.T) {
	sha := "sha256:" + strings.Repeat("a", 64)
	object := `{"tag_name":"V2","published_at":"2026-10-02T00:00:00Z","assets":[{"name":"TigerClaw-Setup.exe","browser_download_url":"https://github.com/RapidAI/MaClaw/releases/download/V2/TigerClaw-Setup.exe","digest":"` + sha + `"}]}`
	got, err := decodeGitHubReleaseSummaries(&prefixThenFailReader{data: []byte("[" + object + ","), fail: context.DeadlineExceeded})
	if !errors.Is(err, errGitHubReleasePageTruncated) {
		t.Fatalf("decodeGitHubReleaseSummaries() error = %v", err)
	}
	if len(got) != 1 || got[0].TagName != "V2" || len(got[0].Assets) != 1 {
		t.Fatalf("prefix = %+v", got)
	}
	if _, err := decodeGitHubReleaseSummaries(&prefixThenFailReader{data: []byte("["), fail: context.DeadlineExceeded}); errors.Is(err, errGitHubReleasePageTruncated) {
		t.Fatal("a cut-off array with no complete release was kept")
	}
}

func TestDecodeGitHubReleaseSummariesKeepsACompletePrefixWhenTheJSONEndsEarly(t *testing.T) {
	sha := "sha256:" + strings.Repeat("a", 64)
	object := `{"tag_name":"V2","published_at":"2026-10-02T00:00:00Z","assets":[{"name":"MetaStaff-Setup.exe","browser_download_url":"https://github.com/RapidAI/MaClaw/releases/download/V2/MetaStaff-Setup.exe","digest":"` + sha + `"}]}`
	got, err := decodeGitHubReleaseSummaries(strings.NewReader("[" + object + ","))
	if !errors.Is(err, errGitHubReleasePageTruncated) {
		t.Fatalf("decodeGitHubReleaseSummaries() error = %v", err)
	}
	if len(got) != 1 || got[0].TagName != "V2" || got[0].Assets[0].Name != "MetaStaff-Setup.exe" {
		t.Fatalf("prefix = %+v", got)
	}
	if _, err := decodeGitHubReleaseSummaries(strings.NewReader(`{"message":"no"}`)); errors.Is(err, errGitHubReleasePageTruncated) {
		t.Fatal("a non-array body was treated as a truncated release page")
	}
}

func TestCollectGitHubRollbackPagesRetriesATruncatedPageOnce(t *testing.T) {
	sha := "sha256:" + strings.Repeat("d", 64)
	calls := 0
	got, err := collectGitHubRollbackSummaries(context.Background(), func(ctx context.Context, page int) ([]githubReleaseSummary, error) {
		calls++
		if calls == 1 {
			return []githubReleaseSummary{rollbackGitHubSummary("V1", "2026-10-01T00:00:00Z", "TigerClaw-Setup.exe", sha, false)}, errGitHubReleasePageTruncated
		}
		summaries := make([]githubReleaseSummary, githubRollbackPageSize)
		for i := range summaries {
			summaries[i] = rollbackGitHubSummary(fmt.Sprintf("V%d", i+1), fmt.Sprintf("2026-10-%02dT00:00:00Z", i+1), "TigerClaw-Setup.exe", sha, false)
		}
		return summaries, nil
	}, "TigerClaw-Setup.exe")
	if err != nil || calls != 2 || len(got) != githubRollbackPageSize || got[0].TagName != "V1" {
		t.Fatalf("err=%v calls=%d summaries=%d", err, calls, len(got))
	}
}

func TestCollectGitHubRollbackPagesKeepsATruncatedPrefixWhenTheRetryFails(t *testing.T) {
	sha := "sha256:" + strings.Repeat("e", 64)
	calls := 0
	got, err := collectGitHubRollbackSummaries(context.Background(), func(ctx context.Context, page int) ([]githubReleaseSummary, error) {
		calls++
		if calls == 1 {
			return []githubReleaseSummary{
				rollbackGitHubSummary("V9", "2026-10-09T00:00:00Z", "TigerClaw-Setup.exe", sha, false),
				rollbackGitHubSummary("V8", "2026-10-08T00:00:00Z", "TigerClaw-Setup.exe", sha, false),
			}, errGitHubReleasePageTruncated
		}
		if calls == 2 {
			return nil, context.DeadlineExceeded
		}
		t.Fatalf("fetched page %d after the truncated prefix was already kept", page)
		return nil, nil
	}, "TigerClaw-Setup.exe")
	if err != nil || calls != 2 || len(got) != 2 || got[0].TagName != "V9" || got[1].TagName != "V8" {
		t.Fatalf("err=%v calls=%d got=%+v", err, calls, got)
	}
}

func TestCollectGitHubRollbackPagesKeepsATruncatedPrefixWhenTheDeadlineWins(t *testing.T) {
	sha := "sha256:" + strings.Repeat("9", 64)
	ctx := &errAfter{Context: context.Background(), after: 3}
	calls := 0
	got, err := collectGitHubRollbackSummaries(ctx, func(context.Context, int) ([]githubReleaseSummary, error) {
		calls++
		return []githubReleaseSummary{
			rollbackGitHubSummary("V9", "2026-10-09T00:00:00Z", "MetaStaff-Universal.pkg", sha, false),
		}, errGitHubReleasePageTruncated
	}, "MetaStaff-Universal.pkg")
	if err != nil || calls != 1 || len(got) != 1 || got[0].TagName != "V9" {
		t.Fatalf("err=%v calls=%d got=%+v", err, calls, got)
	}
}

// errAfter fails Err after after calls. collectGitHubRollbackSummaries checks
// it at the start of a page and once more before retrying a truncated page,
// so the third check is a deadline that fires before that retry starts.
type errAfter struct {
	context.Context
	checks int
	after  int
}

func (c *errAfter) Err() error {
	c.checks++
	if c.checks >= c.after {
		return context.DeadlineExceeded
	}
	return c.Context.Err()
}

func TestCollectGitHubRollbackPagesKeepsTheLongerTruncatedPrefix(t *testing.T) {
	sha := "sha256:" + strings.Repeat("f", 64)
	calls := 0
	longer := []githubReleaseSummary{
		rollbackGitHubSummary("V9", "2026-10-09T00:00:00Z", "TigerClaw-Setup.exe", sha, false),
		rollbackGitHubSummary("V8", "2026-10-08T00:00:00Z", "TigerClaw-Setup.exe", sha, false),
	}
	got, err := collectGitHubRollbackSummaries(context.Background(), func(ctx context.Context, page int) ([]githubReleaseSummary, error) {
		calls++
		if calls == 1 {
			return longer, errGitHubReleasePageTruncated
		}
		if calls == 2 {
			return longer[:1], errGitHubReleasePageTruncated
		}
		t.Fatalf("fetched page %d after a truncated page was kept", page)
		return nil, nil
	}, "TigerClaw-Setup.exe")
	if err != nil || calls != 2 || len(got) != 2 || got[0].TagName != "V9" || got[1].TagName != "V8" {
		t.Fatalf("err=%v calls=%d got=%+v", err, calls, got)
	}
}

func TestDecodeGitHubReleaseSummariesRejectsOversizedResponse(t *testing.T) {
	data := bytes.Repeat([]byte("x"), githubReleaseListMaxBytes+1)
	if _, err := decodeGitHubReleaseSummaries(bytes.NewReader(data)); err == nil {
		t.Fatal("expected oversized response to be rejected")
	}
}

func rollbackGitHubSummary(tag, published, name, digest string, draft bool) githubReleaseSummary {
	return githubReleaseSummary{
		TagName:     tag,
		PublishedAt: published,
		Draft:       draft,
		Assets: []struct {
			Name               string `json:"name"`
			BrowserDownloadURL string `json:"browser_download_url"`
			Digest             string `json:"digest"`
		}{{
			Name:               name,
			BrowserDownloadURL: "https://github.com/RapidAI/MaClaw/releases/download/" + tag + "/" + name,
			Digest:             digest,
		}},
	}
}

func TestGitHubRollbackReleasesURLUsesAShortPage(t *testing.T) {
	got := githubRollbackReleasesURL(2)
	want := "https://api.github.com/repos/RapidAI/MaClaw/releases?per_page=10&page=2"
	if got != want {
		t.Fatalf("githubRollbackReleasesURL(2) = %q", got)
	}
}

func TestCollectGitHubRollbackPagesStopsOnceFiveDigestBuildsArePresent(t *testing.T) {
	sha := "sha256:" + strings.Repeat("a", 64)
	calls := 0
	got, err := collectGitHubRollbackSummaries(context.Background(), func(ctx context.Context, page int) ([]githubReleaseSummary, error) {
		calls++
		if page != 1 {
			t.Fatalf("fetched page %d after five digest builds were already available", page)
		}
		summaries := make([]githubReleaseSummary, 0, githubRollbackPageSize)
		for i := 0; i < githubRollbackPageSize; i++ {
			tag := fmt.Sprintf("V%02d", i)
			summaries = append(summaries, rollbackGitHubSummary(tag, fmt.Sprintf("2026-10-%02dT00:00:00Z", i+1), "TigerClaw-Setup.exe", sha, false))
		}
		return summaries, nil
	}, "TigerClaw-Setup.exe")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 || len(got) != githubRollbackPageSize {
		t.Fatalf("calls=%d summaries=%d", calls, len(got))
	}
}

func TestCollectGitHubRollbackPagesReadsPastAFullPageOfDrafts(t *testing.T) {
	sha := "sha256:" + strings.Repeat("b", 64)
	calls := 0
	got, err := collectGitHubRollbackSummaries(context.Background(), func(ctx context.Context, page int) ([]githubReleaseSummary, error) {
		calls++
		if page == 1 {
			summaries := make([]githubReleaseSummary, githubRollbackPageSize)
			for i := range summaries {
				summaries[i] = rollbackGitHubSummary(fmt.Sprintf("D%d", i), "2026-10-20T00:00:00Z", "TigerClaw-Setup.exe", sha, true)
			}
			return summaries, nil
		}
		if page != 2 {
			t.Fatalf("fetched page %d", page)
		}
		summaries := make([]githubReleaseSummary, 5)
		for i := range summaries {
			summaries[i] = rollbackGitHubSummary(fmt.Sprintf("V%d", i), fmt.Sprintf("2026-10-%02dT00:00:00Z", i+1), "TigerClaw-Setup.exe", sha, false)
		}
		return summaries, nil
	}, "TigerClaw-Setup.exe")
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 || len(got) != githubRollbackPageSize+5 {
		t.Fatalf("calls=%d summaries=%d", calls, len(got))
	}
}

func TestCollectGitHubRollbackPagesKeepsAnEarlierPageWhenTheNextFails(t *testing.T) {
	sha := "sha256:" + strings.Repeat("c", 64)
	got, err := collectGitHubRollbackSummaries(context.Background(), func(ctx context.Context, page int) ([]githubReleaseSummary, error) {
		if page != 1 {
			return nil, fmt.Errorf("page failed")
		}
		summaries := make([]githubReleaseSummary, githubRollbackPageSize)
		for i := range summaries {
			name, digest := "notes.txt", ""
			if i < 2 {
				name, digest = "TigerClaw-Setup.exe", sha
			}
			summaries[i] = rollbackGitHubSummary(fmt.Sprintf("V%d", i), fmt.Sprintf("2026-10-%02dT00:00:00Z", i+1), name, digest, false)
		}
		return summaries, nil
	}, "TigerClaw-Setup.exe")
	if err != nil || len(got) != githubRollbackPageSize {
		t.Fatalf("err=%v summaries=%d", err, len(got))
	}
}

func TestCollectGitHubRollbackPagesStopsWhenTheDeadlineExpires(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	got, err := collectGitHubRollbackSummaries(ctx, func(context.Context, int) ([]githubReleaseSummary, error) {
		calls++
		summaries := make([]githubReleaseSummary, githubRollbackPageSize)
		for i := range summaries {
			summaries[i] = rollbackGitHubSummary(fmt.Sprintf("V%d", i), fmt.Sprintf("2026-10-%02dT00:00:00Z", i+1), "notes.txt", "", false)
		}
		cancel()
		return summaries, nil
	}, "TigerClaw-Setup.exe")
	if err != nil || calls != 1 || len(got) != githubRollbackPageSize {
		t.Fatalf("err=%v calls=%d summaries=%d", err, calls, len(got))
	}
}

func TestFetchGitHubRollbackPageRejectsNonOK(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Accept") != "application/vnd.github+json" {
			t.Errorf("Accept = %q", r.Header.Get("Accept"))
		}
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	_, err := fetchGitHubRollbackPage(context.Background(), updateHTTPClient(time.Second), server.URL)
	if err == nil || !strings.Contains(err.Error(), "status 403") {
		t.Fatalf("fetchGitHubRollbackPage() error = %v", err)
	}
}

func TestCompareVersions_NumericOnly(t *testing.T) {
	cases := []struct {
		v1, v2 string
		want   int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.2.0", "1.1.9", 1},
		{"2.0.0", "1.99.99", 1},
		{"1.0", "1.0.0", 0},
		{"1.0.0.1", "1.0.0", 1},
	}
	for _, c := range cases {
		got := compareVersions(c.v1, c.v2)
		if got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.v1, c.v2, got, c.want)
		}
	}
}

func TestCompareVersions_PreRelease(t *testing.T) {
	cases := []struct {
		v1, v2 string
		want   int
	}{
		// beta < stable (same numeric)
		{"1.3.0-beta.1", "1.3.0", -1},
		{"1.3.0", "1.3.0-beta.1", 1},
		// beta < rc < stable
		{"1.3.0-beta.1", "1.3.0-rc.1", -1},
		{"1.3.0-rc.1", "1.3.0", -1},
		// alpha < beta
		{"1.3.0-alpha.1", "1.3.0-beta.1", -1},
		// same type, compare numbers
		{"1.3.0-beta.1", "1.3.0-beta.2", -1},
		{"1.3.0-beta.2", "1.3.0-beta.1", 1},
		{"1.3.0-beta.1", "1.3.0-beta.1", 0},
		// rc numbering
		{"1.3.0-rc.1", "1.3.0-rc.2", -1},
		// higher numeric always wins regardless of pre-release
		{"1.4.0-beta.1", "1.3.0", 1},
		{"1.3.0", "1.4.0-beta.1", -1},
		// beta with no number vs beta.1
		{"1.3.0-beta", "1.3.0-beta.1", -1},
		// "rc1" format (no dot separator)
		{"1.3.0-rc1", "1.3.0-rc2", -1},
	}
	for _, c := range cases {
		got := compareVersions(c.v1, c.v2)
		if got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.v1, c.v2, got, c.want)
		}
	}
}

func TestCompareVersions_WithVPrefix(t *testing.T) {
	cases := []struct {
		v1, v2 string
		want   int
	}{
		{"V1.3.0", "v1.3.0", 0},
		{"v1.3.0-beta.1", "V1.3.0", -1},
		{"V1.4.0", "v1.3.0-rc.1", 1},
	}
	for _, c := range cases {
		got := compareVersions(c.v1, c.v2)
		if got != c.want {
			t.Errorf("compareVersions(%q, %q) = %d, want %d", c.v1, c.v2, got, c.want)
		}
	}
}

func TestSplitVersionPreRelease(t *testing.T) {
	cases := []struct {
		input   string
		wantNum string
		wantPre string
	}{
		{"1.3.0", "1.3.0", ""},
		{"1.3.0-beta.1", "1.3.0", "beta.1"},
		{"1.3.0-rc1", "1.3.0", "rc1"},
		{"1.3.0-alpha", "1.3.0", "alpha"},
		{"1.3.0-beta.10", "1.3.0", "beta.10"},
		{"", "", ""},
	}
	for _, c := range cases {
		gotNum, gotPre := splitVersionPreRelease(c.input)
		if gotNum != c.wantNum || gotPre != c.wantPre {
			t.Errorf("splitVersionPreRelease(%q) = (%q, %q), want (%q, %q)",
				c.input, gotNum, gotPre, c.wantNum, c.wantPre)
		}
	}
}

func TestPreReleaseWeight(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"", 100},
		{"alpha.1", 10},
		{"beta.1", 20},
		{"rc.1", 30},
		{"Beta.2", 20},
		{"RC1", 30},
		{"dev.1", 15},
	}
	for _, c := range cases {
		got := preReleaseWeight(c.input)
		if got != c.want {
			t.Errorf("preReleaseWeight(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestPreReleaseNumber(t *testing.T) {
	cases := []struct {
		input string
		want  int
	}{
		{"beta", 0},
		{"beta.1", 1},
		{"beta.10", 10},
		{"rc1", 1},
		{"rc12", 12},
		{"alpha-3", 3},
	}
	for _, c := range cases {
		got := preReleaseNumber(c.input)
		if got != c.want {
			t.Errorf("preReleaseNumber(%q) = %d, want %d", c.input, got, c.want)
		}
	}
}

func TestVerifySHA256File_EmptyExpected(t *testing.T) {
	// Empty expected = no verification = always passes
	if err := verifySHA256File("/nonexistent/path", ""); err != nil {
		t.Errorf("verifySHA256File with empty expected should return nil, got %v", err)
	}
}

func TestVerifySHA256File_InvalidLength(t *testing.T) {
	err := verifySHA256File("/some/path", "tooshort")
	if err == nil {
		t.Error("verifySHA256File with short digest should return error")
	}
}

func TestVerifySHA256File_CorrectHash(t *testing.T) {
	// Write a temp file and verify its known SHA256
	content := []byte("hello world\n")
	tmpFile := filepath.Join(t.TempDir(), "test.bin")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatal(err)
	}
	// sha256("hello world\n") = a948904f2f0f479b8f8564e9d7d4e6fc80d4f85e3d5fda5b7f6c5fc4b2cb4f2b ... nope
	// Compute expected: sha256sum of "hello world\n"
	h := sha256.New()
	h.Write(content)
	expected := fmt.Sprintf("%x", h.Sum(nil))

	if err := verifySHA256File(tmpFile, expected); err != nil {
		t.Errorf("verifySHA256File with correct hash should pass, got: %v", err)
	}
}

func TestVerifySHA256File_WrongHash(t *testing.T) {
	content := []byte("hello world\n")
	tmpFile := filepath.Join(t.TempDir(), "test.bin")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatal(err)
	}
	wrongHash := "0000000000000000000000000000000000000000000000000000000000000000"
	err := verifySHA256File(tmpFile, wrongHash)
	if err == nil {
		t.Error("verifySHA256File with wrong hash should return error")
	}
	if !strings.Contains(err.Error(), "integrity verification failed") {
		t.Errorf("error should mention integrity verification, got: %v", err)
	}
}

func TestVerifySHA256File_UppercaseExpected(t *testing.T) {
	content := []byte("test data")
	tmpFile := filepath.Join(t.TempDir(), "test.bin")
	if err := os.WriteFile(tmpFile, content, 0644); err != nil {
		t.Fatal(err)
	}
	h := sha256.New()
	h.Write(content)
	expected := strings.ToUpper(fmt.Sprintf("%x", h.Sum(nil)))

	// Should still pass — function lowercases the expected hash internally
	if err := verifySHA256File(tmpFile, expected); err != nil {
		t.Errorf("verifySHA256File with uppercase expected should pass, got: %v", err)
	}
}

func TestPickPreferredUpdateResult(t *testing.T) {
	betaOnly := UpdateResult{HasUpdate: true, LatestVersion: "V1.4.0-beta.1", TagName: "1.4.0-beta.1", Channel: "beta"}
	stableOnly := UpdateResult{HasUpdate: true, LatestVersion: "V1.3.0", TagName: "1.3.0", Channel: "stable"}
	olderBeta := UpdateResult{HasUpdate: false, LatestVersion: "V1.3.0-beta.2", TagName: "1.3.0-beta.2", Channel: "beta"}
	newerStable := UpdateResult{HasUpdate: true, LatestVersion: "V1.3.0", TagName: "1.3.0", Channel: "stable"}
	sameBeta := UpdateResult{HasUpdate: true, LatestVersion: "V1.3.0-beta.1", TagName: "1.3.0-beta.1", Channel: "beta"}
	sameStable := UpdateResult{HasUpdate: false, LatestVersion: "V1.2.0", TagName: "1.2.0", Channel: "stable"}
	// Same numeric tag on both channels should prefer beta (user opted in).
	equalBeta := UpdateResult{HasUpdate: true, LatestVersion: "V1.5.0", TagName: "1.5.0", Channel: "beta"}
	equalStable := UpdateResult{HasUpdate: true, LatestVersion: "V1.5.0", TagName: "1.5.0", Channel: "stable"}
	// TagName should win over display-only LatestVersion for comparison.
	tagWinsBeta := UpdateResult{HasUpdate: true, LatestVersion: "V1.0.0", TagName: "2.0.0-beta.1", Channel: "beta"}
	tagWinsStable := UpdateResult{HasUpdate: true, LatestVersion: "V9.0.0", TagName: "1.9.0", Channel: "stable"}

	t.Run("both fail", func(t *testing.T) {
		_, err := pickPreferredUpdateResult(UpdateResult{}, fmt.Errorf("beta down"), UpdateResult{}, fmt.Errorf("stable down"))
		if err == nil {
			t.Fatal("expected error when both channels fail")
		}
	})
	t.Run("beta fails falls back to stable", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(UpdateResult{}, fmt.Errorf("beta down"), stableOnly, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "stable" || !got.HasUpdate {
			t.Fatalf("got %+v, want stable update", got)
		}
	})
	t.Run("stable fails falls back to beta", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(betaOnly, nil, UpdateResult{}, fmt.Errorf("stable down"))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "beta" || !got.HasUpdate {
			t.Fatalf("got %+v, want beta update", got)
		}
	})
	t.Run("newer beta preferred over older stable", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(betaOnly, nil, stableOnly, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "beta" || got.LatestVersion != "V1.4.0-beta.1" {
			t.Fatalf("got %+v, want beta V1.4.0-beta.1", got)
		}
	})
	t.Run("newer stable not masked by older beta", func(t *testing.T) {
		// User on a beta build; formal 1.3.0 is out but beta.json still points at 1.3.0-beta.2.
		got, err := pickPreferredUpdateResult(olderBeta, nil, newerStable, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "stable" || !got.HasUpdate || got.LatestVersion != "V1.3.0" {
			t.Fatalf("got %+v, want stable V1.3.0 with has_update", got)
		}
	})
	t.Run("newer beta preferred when stable has no update path", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(sameBeta, nil, sameStable, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "beta" {
			t.Fatalf("got %+v, want beta", got)
		}
	})
	t.Run("equal versions prefer beta", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(equalBeta, nil, equalStable, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got.Channel != "beta" {
			t.Fatalf("got %+v, want beta on version tie", got)
		}
	})
	t.Run("compare TagName over display LatestVersion", func(t *testing.T) {
		got, err := pickPreferredUpdateResult(tagWinsBeta, nil, tagWinsStable, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// Tag 2.0.0-beta.1 > 1.9.0 even though display LatestVersion on stable is V9.0.0.
		if got.Channel != "beta" || got.TagName != "2.0.0-beta.1" {
			t.Fatalf("got %+v, want beta by TagName", got)
		}
	})
}

func TestRollbackReleasesFromManifestUsesOnlyStableMirrorURLs(t *testing.T) {
	manifest := stableHistoryManifest{Releases: []stableHistoryRelease{
		{Build: "11970", PublishedAt: "2026-08-28T10:00:00Z", Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {URLs: []string{
				"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/releases/11970/MaClaw-Setup.exe",
			}, SHA256: strings.Repeat("a", 64)},
		}},
		{Build: "11969", PublishedAt: "2026-08-27T10:00:00Z", Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {URL: "https://untrusted.example/installer.exe"},
		}},
	}}

	releases := rollbackReleasesFromManifest(manifest, "MaClaw-Setup.exe")
	if len(releases) != 1 {
		t.Fatalf("got %d releases, want 1", len(releases))
	}
	if releases[0].Build != "11970" || releases[0].PublishedAt != "2026-08-28T10:00:00Z" || releases[0].SHA256 != strings.Repeat("a", 64) {
		t.Fatalf("unexpected rollback release: %+v", releases[0])
	}
	urls := splitDownloadURLs(releases[0].DownloadUrl)
	if len(urls) != 2 || urls[0] != "https://github.com/RapidAI/MaClaw/releases/download/11970/MaClaw-Setup.exe" || !strings.Contains(urls[1], "/releases/11970/MaClaw-Setup.exe") {
		t.Fatalf("rollback URLs = %#v, want GitHub then immutable R2 path", urls)
	}
}

func TestImmutableReleaseAssetURLUsesPathEscapedOpaqueBuild(t *testing.T) {
	got := immutableReleaseAssetURL(r2PublicBaseURL, "V7+candidate:1@host/unsafe", "MaClaw Setup.exe")
	want := r2PublicBaseURL + "/releases/V7+candidate:1@host%2Funsafe/MaClaw%20Setup.exe"
	if got != want {
		t.Fatalf("immutableReleaseAssetURL() = %q, want %q", got, want)
	}
}

func TestGitHubReleaseAssetURLUsesPathEscapedOpaqueBuild(t *testing.T) {
	got := githubReleaseAssetURL("V7+candidate:1@host/unsafe", "MaClaw Setup.exe")
	want := "https://github.com/RapidAI/MaClaw/releases/download/V7+candidate:1@host%2Funsafe/MaClaw%20Setup.exe"
	if got != want {
		t.Fatalf("githubReleaseAssetURL() = %q, want %q", got, want)
	}
}

func TestBuildUpdateResultPathEscapesGitHubTag(t *testing.T) {
	app := &App{}
	result, err := app.buildUpdateResult("V7.0.0.1", latestReleaseInfo{TagName: "V7.1.0/unsafe"}, false)
	if err != nil {
		t.Fatalf("buildUpdateResult() error = %v", err)
	}
	if got := splitDownloadURLs(result.DownloadUrl)[1]; got != "https://github.com/RapidAI/MaClaw/releases/download/V7.1.0%2Funsafe/MaClaw-Setup.exe" {
		t.Fatalf("GitHub update URL = %q, want escaped tag segment", got)
	}
}
func TestIsReleaseMirrorURLRejectsLookalikeHost(t *testing.T) {
	lookalike := "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev.evil.example/latest/MaClaw-Setup.exe"
	if isReleaseMirrorURL(lookalike, "MaClaw-Setup.exe", false) {
		t.Fatalf("isReleaseMirrorURL accepted lookalike host %q", lookalike)
	}
}

func TestRollbackReleasesFromManifestRequiresSHA256(t *testing.T) {
	manifest := stableHistoryManifest{Releases: []stableHistoryRelease{
		{Build: "11970", PublishedAt: "2026-08-28T10:00:00Z", Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {URLs: []string{"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/releases/11970/MaClaw-Setup.exe"}, SHA256: ""},
		}},
	}}
	if releases := rollbackReleasesFromManifest(manifest, "MaClaw-Setup.exe"); len(releases) != 0 {
		t.Fatalf("got %+v, want no release without a checksum", releases)
	}
}

func TestRollbackReleasesFromManifestRequiresRFC3339PublicationDate(t *testing.T) {
	manifest := stableHistoryManifest{Releases: []stableHistoryRelease{
		{Build: "11970", PublishedAt: "not-a-date", Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {URLs: []string{"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/releases/11970/MaClaw-Setup.exe"}, SHA256: strings.Repeat("a", 64)},
		}},
	}}
	if releases := rollbackReleasesFromManifest(manifest, "MaClaw-Setup.exe"); len(releases) != 0 {
		t.Fatalf("got %+v, want no release with a malformed publication date", releases)
	}
}

func TestFetchStableHistoryFallsBackWhenFirstMirrorHasNoValidRelease(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(stableHistoryManifest{Releases: []stableHistoryRelease{{
			Build:       "11970",
			PublishedAt: "2026-08-28T10:00:00Z",
			Assets: map[string]updateManifestAsset{
				"MaClaw-Setup.exe": {URLs: []string{"https://untrusted.example/installer.exe"}, SHA256: strings.Repeat("a", 64)},
			},
		}}})
	}))
	defer server.Close()

	if _, err := fetchStableHistory("test", server.URL, time.Second); err == nil || !strings.Contains(err.Error(), "no valid rollback releases") {
		t.Fatalf("fetchStableHistory() error = %v, want invalid-history failure", err)
	}
}

func TestRollbackReleasesFromManifestLimitsAndDeduplicates(t *testing.T) {
	assets := func(build string) map[string]updateManifestAsset {
		return map[string]updateManifestAsset{"MaClaw-Setup.exe": {URLs: []string{
			"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/releases/" + build + "/MaClaw-Setup.exe",
		}, SHA256: strings.Repeat("b", 64)}}
	}
	manifest := stableHistoryManifest{Releases: []stableHistoryRelease{
		{Build: "6", PublishedAt: "2026-08-06T00:00:00Z", Assets: assets("6")},
		{Build: "6", PublishedAt: "2026-08-05T00:00:00Z", Assets: assets("6")},
		{Build: "5", PublishedAt: "2026-08-05T00:00:00Z", Assets: assets("5")},
		{Build: "4", PublishedAt: "2026-08-04T00:00:00Z", Assets: assets("4")},
		{Build: "3", PublishedAt: "2026-08-03T00:00:00Z", Assets: assets("3")},
		{Build: "2", PublishedAt: "2026-08-02T00:00:00Z", Assets: assets("2")},
		{Build: "1", PublishedAt: "2026-08-01T00:00:00Z", Assets: assets("1")},
	}}
	releases := rollbackReleasesFromManifest(manifest, "MaClaw-Setup.exe")
	if len(releases) != 5 {
		t.Fatalf("got %d releases, want 5", len(releases))
	}
	if releases[0].Build != "6" || releases[4].Build != "2" {
		t.Fatalf("unexpected retained builds: %+v", releases)
	}
}

func TestRollbackReleasesFromManifestKeepsNewestWhenCatalogueIsOldestFirst(t *testing.T) {
	assets := func(build string) map[string]updateManifestAsset {
		return map[string]updateManifestAsset{"TigerClaw-Setup.exe": {URLs: []string{
			"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/releases/" + build + "/TigerClaw-Setup.exe",
		}, SHA256: strings.Repeat("b", 64)}}
	}
	manifest := stableHistoryManifest{Releases: []stableHistoryRelease{
		{Build: "1", PublishedAt: "2026-08-01T00:00:00Z", Assets: assets("1")},
		{Build: "6", PublishedAt: "2026-08-01T00:00:00Z", Assets: assets("6")},
		{Build: "2", PublishedAt: "2026-08-02T00:00:00Z", Assets: assets("2")},
		{Build: "3", PublishedAt: "2026-08-03T00:00:00Z", Assets: assets("3")},
		{Build: "4", PublishedAt: "2026-08-04T00:00:00Z", Assets: assets("4")},
		{Build: "5", PublishedAt: "2026-08-05T00:00:00Z", Assets: assets("5")},
		{Build: "6", PublishedAt: "2026-08-06T00:00:00Z", Assets: assets("6")},
	}}
	releases := rollbackReleasesFromManifest(manifest, "TigerClaw-Setup.exe")
	if len(releases) != 5 || releases[0].Build != "6" || releases[0].PublishedAt != "2026-08-06T00:00:00Z" || releases[4].Build != "2" {
		t.Fatalf("oldest-first catalogue retained %+v", releases)
	}
}

func TestGitHubDigestSHA256(t *testing.T) {
	want := strings.Repeat("ab", 32)
	got, ok := githubDigestSHA256("sha256:" + want)
	if !ok || got != want {
		t.Fatalf("githubDigestSHA256() = %q %v", got, ok)
	}
	if _, ok := githubDigestSHA256("sha256:" + strings.Repeat("a", 63)); ok {
		t.Fatal("accepted a short digest")
	}
	if _, ok := githubDigestSHA256(want); ok {
		t.Fatal("accepted a digest without the sha256 prefix")
	}
}

func TestRollbackReleasesFromGitHubSummariesKeepsOlderOEMInstallers(t *testing.T) {
	sha := strings.Repeat("c", 64)
	older := strings.Repeat("d", 64)
	summaries := []githubReleaseSummary{
		{
			TagName: "V8.1.0.3", PublishedAt: "2026-10-19T15:37:47Z",
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
				Digest             string `json:"digest"`
			}{
				{Name: "MaClaw-Setup.exe", BrowserDownloadURL: "https://github.com/RapidAI/MaClaw/releases/download/V8.1.0.3/MaClaw-Setup.exe", Digest: "sha256:" + sha},
				{Name: "TigerClaw-Setup.exe", BrowserDownloadURL: "https://github.com/RapidAI/MaClaw/releases/download/V8.1.0.3/TigerClaw-Setup.exe", Digest: "sha256:" + sha},
			},
		},
		{
			TagName: "V8.1.0.2", PublishedAt: "2026-10-18T15:37:47Z", Draft: true,
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
				Digest             string `json:"digest"`
			}{
				{Name: "TigerClaw-Setup.exe", BrowserDownloadURL: "https://github.com/RapidAI/MaClaw/releases/download/V8.1.0.2/TigerClaw-Setup.exe", Digest: "sha256:" + older},
			},
		},
		{
			TagName: "V8.1.0.1", PublishedAt: "2026-10-17T15:37:47Z",
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
				Digest             string `json:"digest"`
			}{
				{Name: "TigerClaw-Setup.exe", BrowserDownloadURL: "https://github.com/RapidAI/MaClaw/releases/download/V8.1.0.1/TigerClaw-Setup.exe", Digest: "sha256:" + older},
			},
		},
		{
			TagName: "V8.0.0.9", PublishedAt: "2026-10-16T15:37:47Z",
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
				Digest             string `json:"digest"`
			}{
				{Name: "MaClaw-Setup.exe", BrowserDownloadURL: "https://github.com/RapidAI/MaClaw/releases/download/V8.0.0.9/MaClaw-Setup.exe", Digest: "sha256:" + sha},
			},
		},
	}
	releases := rollbackReleasesFromGitHubSummaries(summaries, "TigerClaw-Setup.exe")
	if len(releases) != 2 || releases[0].Build != "V8.1.0.3" || releases[1].Build != "V8.1.0.1" {
		t.Fatalf("OEM rollback releases = %+v, want current and older TigerClaw builds", releases)
	}
	if releases[1].SHA256 != older || !strings.Contains(releases[1].DownloadUrl, "/V8.1.0.1/TigerClaw-Setup.exe") {
		t.Fatalf("older OEM release was not downloadable: %+v", releases[1])
	}
	maclaw := rollbackReleasesFromGitHubSummaries(summaries, "MaClaw-Setup.exe")
	if len(maclaw) != 2 || maclaw[0].Build != "V8.1.0.3" || maclaw[1].Build != "V8.0.0.9" {
		t.Fatalf("MaClaw rollback releases = %+v", maclaw)
	}
}

func TestRollbackReleasesFromGitHubSummariesKeepsNewestFiveWhenPageIsOldestFirst(t *testing.T) {
	sha := strings.Repeat("a", 64)
	summaries := make([]githubReleaseSummary, 0, 6)
	for i := 1; i <= 6; i++ {
		tag := fmt.Sprintf("V8.0.0.%d", i)
		summaries = append(summaries, githubReleaseSummary{
			TagName:     tag,
			PublishedAt: fmt.Sprintf("2026-10-%02dT00:00:00Z", i),
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
				Digest             string `json:"digest"`
			}{{
				Name:               "TigerClaw-Setup.exe",
				BrowserDownloadURL: "https://github.com/RapidAI/MaClaw/releases/download/" + tag + "/TigerClaw-Setup.exe",
				Digest:             "sha256:" + sha,
			}},
		})
	}
	releases := rollbackReleasesFromGitHubSummaries(summaries, "TigerClaw-Setup.exe")
	if len(releases) != 5 || releases[0].Build != "V8.0.0.6" || releases[4].Build != "V8.0.0.2" {
		t.Fatalf("oldest-first GitHub page retained %+v", releases)
	}
}

func TestRollbackReleaseCanEnterLeavesRoomForANewerBuild(t *testing.T) {
	sha := strings.Repeat("a", 64)
	accepted := make([]RollbackRelease, 0, 5)
	for i := 5; i >= 1; i-- {
		accepted = append(accepted, RollbackRelease{
			Build:       fmt.Sprintf("%d", i),
			PublishedAt: fmt.Sprintf("2026-10-%02dT00:00:00Z", i),
			DownloadUrl: "https://example.invalid",
			SHA256:      sha,
		})
	}
	if !rollbackReleaseCanEnter(accepted, "2026-10-06T00:00:00Z") {
		t.Fatal("a newer build must be able to enter a full catalogue")
	}
	if rollbackReleaseCanEnter(accepted, "2026-09-01T00:00:00Z") {
		t.Fatal("an older build must not displace the five newest")
	}
	if !rollbackReleaseCanEnter(accepted[:3], "2026-09-01T00:00:00Z") {
		t.Fatal("fewer than five builds still have room")
	}
}

func TestRollbackManifestCandidatesWalkNewestFirst(t *testing.T) {
	sha := strings.Repeat("ab", 32)
	summary := func(tag string, day int, draft, withDigest bool) githubReleaseSummary {
		digest := ""
		if withDigest {
			digest = "sha256:" + sha
		}
		return githubReleaseSummary{
			TagName:     tag,
			PublishedAt: fmt.Sprintf("2026-10-%02dT00:00:00Z", day),
			Draft:       draft,
			Assets: []struct {
				Name               string `json:"name"`
				BrowserDownloadURL string `json:"browser_download_url"`
				Digest             string `json:"digest"`
			}{
				{Name: "TigerClaw-Setup.exe", BrowserDownloadURL: "https://github.com/RapidAI/MaClaw/releases/download/" + tag + "/TigerClaw-Setup.exe", Digest: digest},
				{Name: "latest.json", BrowserDownloadURL: "https://github.com/RapidAI/MaClaw/releases/download/" + tag + "/latest.json"},
			},
		}
	}
	summaries := []githubReleaseSummary{
		summary("V1", 1, false, false),
		summary("V2", 2, true, false),
		summary("V3", 3, false, false),
		summary("V4", 4, false, true),
		summary("V5", 5, false, false),
	}
	got := rollbackManifestCandidates(summaries, "TigerClaw-Setup.exe", []RollbackRelease{{
		Build: "V3", PublishedAt: "2026-10-03T00:00:00Z",
	}})
	if len(got) != 2 || got[0].TagName != "V5" || got[1].TagName != "V1" {
		t.Fatalf("manifest candidates = %+v, want newest digest-less builds only", got)
	}
}

func TestMergeRollbackReleasesKeepsOlderMirrorBuilds(t *testing.T) {
	sha := strings.Repeat("a", 64)
	release := func(build, published string) RollbackRelease {
		return RollbackRelease{Build: build, PublishedAt: published, DownloadUrl: "https://github.com/RapidAI/MaClaw/releases/download/" + build + "/TigerClaw-Setup.exe", SHA256: sha}
	}
	merged := mergeRollbackReleases(
		[]RollbackRelease{release("V8.1.0.3", "2026-10-19T00:00:00Z")},
		[]RollbackRelease{
			release("V8.1.0.3", "2026-10-19T00:00:00Z"),
			release("V8.1.0.2", "2026-10-18T00:00:00Z"),
			release("V8.1.0.1", "2026-10-17T00:00:00Z"),
		},
		5,
	)
	if len(merged) != 3 || merged[0].Build != "V8.1.0.3" || merged[2].Build != "V8.1.0.1" {
		t.Fatalf("merged rollback releases = %+v", merged)
	}
}
func TestUpdateResultVersionKey(t *testing.T) {
	if got := updateResultVersionKey(UpdateResult{TagName: "1.2.3", LatestVersion: "V9.9.9"}); got != "1.2.3" {
		t.Fatalf("prefer TagName, got %q", got)
	}
	if got := updateResultVersionKey(UpdateResult{LatestVersion: "V1.0.0"}); got != "V1.0.0" {
		t.Fatalf("fallback LatestVersion, got %q", got)
	}
	if got := updateResultVersionKey(UpdateResult{TagName: "  v1.0.0  "}); got != "v1.0.0" {
		t.Fatalf("trim TagName, got %q", got)
	}
}

func TestUpdateTargetFileNameFor_BrandPackages(t *testing.T) {
	cases := []struct {
		name      string
		brandName string
		goos      string
		want      string
	}{
		{name: "windows default", brandName: "MaClaw", goos: "windows", want: "MaClaw-Setup.exe"},
		{name: "darwin default", brandName: "MaClaw", goos: "darwin", want: "MaClaw-Universal.pkg"},
		{name: "windows metastaff", brandName: "MetaStaff", goos: "windows", want: "MetaStaff-Setup.exe"},
		{name: "darwin metastaff", brandName: "MetaStaff", goos: "darwin", want: "MetaStaff-Universal.pkg"},
		{name: "fallback", brandName: "", goos: "linux", want: "MaClaw-Setup.exe"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := updateTargetFileNameFor(tc.brandName, tc.goos); got != tc.want {
				t.Fatalf("updateTargetFileNameFor(%q, %q) = %q, want %q", tc.brandName, tc.goos, got, tc.want)
			}
		})
	}
}

func TestManifestAssetDownloadURLsPreservesPublishedMirrors(t *testing.T) {
	manifest := updateManifest{
		Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {
				URLs: []string{
					"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/MaClaw-Setup.exe",
				},
			},
		},
	}
	urls := manifestAssetDownloadURLs(manifest, "MaClaw-Setup.exe", "V7.1.0.11864", false)
	if len(urls) != 1 || urls[0] != "https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/MaClaw-Setup.exe" {
		t.Fatalf("manifestAssetDownloadURLs() = %#v, want the published R2 URL", urls)
	}
}

func TestManifestAssetDownloadURLsRejectsUntrustedOrWrongPathURLs(t *testing.T) {
	manifest := updateManifest{
		Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {
				URLs: []string{
					"https://evil.example/latest/MaClaw-Setup.exe",
					"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/other.exe",
					"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/MaClaw-Setup.exe?token=untrusted",
				},
			},
		},
	}
	urls := manifestAssetDownloadURLs(manifest, "MaClaw-Setup.exe", "", false)
	if len(urls) != 0 {
		t.Fatalf("manifestAssetDownloadURLs() = %#v, want no untrusted URLs", urls)
	}
}

func TestValidGitHubReleaseAssetURL(t *testing.T) {
	build, asset := "V7.1.0.11876", "MaClaw-Setup.exe"
	want := githubReleaseAssetURL(build, asset)
	if !validGitHubReleaseAssetURL(want, build, asset) {
		t.Fatalf("expected canonical GitHub URL to validate: %s", want)
	}
	for _, bad := range []string{
		"https://evil.example/RapidAI/MaClaw/releases/download/V7.1.0.11876/MaClaw-Setup.exe",
		want + "?download=1",
		"https://github.com/RapidAI/MaClaw/releases/tag/" + build,
	} {
		if validGitHubReleaseAssetURL(bad, build, asset) {
			t.Fatalf("unexpectedly accepted untrusted GitHub URL: %s", bad)
		}
	}
}

func TestManifestDownloadOrderUsesR2ThenGitHub(t *testing.T) {
	manifest := updateManifest{
		Tag: "V7.1.0.11864",
		Assets: map[string]updateManifestAsset{
			"MaClaw-Setup.exe": {
				URLs: []string{
					"https://pub-c837069cbe31469590a5fea6235b436b.r2.dev/latest/MaClaw-Setup.exe",
				},
			},
		},
	}
	urls := manifestAssetDownloadURLs(manifest, "MaClaw-Setup.exe", manifest.Tag, false)
	downloads := combineDownloadURLList(append([]string{r2ReleaseAssetURL("MaClaw-Setup.exe", false), "https://github.com/RapidAI/MaClaw/releases/download/V7.1.0.11864/MaClaw-Setup.exe"}, urls...)...)
	got := splitDownloadURLs(downloads)
	if len(got) != 2 || got[0] != r2ReleaseAssetURL("MaClaw-Setup.exe", false) || !strings.Contains(got[1], "github.com/") {
		t.Fatalf("download candidate order = %#v, want R2, GitHub", got)
	}
}

func TestBuildUpdateResultUsesR2AndGitHubWhenNoManifestURLsExist(t *testing.T) {
	app := &App{}
	result, err := app.buildUpdateResult("V7.0.0.1", latestReleaseInfo{TagName: "V7.1.0.11864"}, false)
	if err != nil {
		t.Fatalf("buildUpdateResult() error = %v", err)
	}
	urls := splitDownloadURLs(result.DownloadUrl)
	if len(urls) != 2 || urls[0] != r2ReleaseAssetURL("MaClaw-Setup.exe", false) || !strings.Contains(urls[1], "github.com/") {
		t.Fatalf("fallback download candidate order = %#v, want R2, GitHub", urls)
	}
}

func TestBuildUpdateResultDoesNotOfferOlderRelease(t *testing.T) {
	app := &App{}
	result, err := app.buildUpdateResult("7.5.0.11973", latestReleaseInfo{TagName: "V7.1.0.11876"}, false)
	if err != nil {
		t.Fatalf("buildUpdateResult() error = %v", err)
	}
	if result.HasUpdate {
		t.Fatalf("older release was offered as update: current=7.5.0.11973 latest=%s", result.LatestVersion)
	}
}

func TestDownloadUpdateRemovesPartialInstallerAndReportsAllFailedSources(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "6")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("bad"))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusForbidden)
	}))
	defer second.Close()

	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, "Downloads"), 0o755); err != nil {
		t.Fatalf("create Downloads directory: %v", err)
	}
	app := &App{testHomeDir: home, downloadCancelers: make(map[string]*downloadCanceler)}
	_, err := app.DownloadUpdate(first.URL+"\n"+second.URL, "MaClaw-Setup.exe")
	if err == nil || !strings.Contains(err.Error(), "all download sources failed") || !strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("DownloadUpdate() error = %v, want aggregated source failures", err)
	}
	downloads := filepath.Join(home, "Downloads")
	if _, err := os.Stat(filepath.Join(downloads, "MaClaw-Setup.exe")); !os.IsNotExist(err) {
		t.Fatalf("partial installer remains after failed fallbacks: %v", err)
	}
}

func TestDownloadUpdateReportsChecksumFailureBeforeTryingFallback(t *testing.T) {
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", "5242880")
		_, _ = w.Write(make([]byte, 5*1024*1024))
	}))
	defer first.Close()
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusForbidden)
	}))
	defer second.Close()

	home := t.TempDir()
	t.Setenv("USERPROFILE", home)
	if err := os.MkdirAll(filepath.Join(home, "Downloads"), 0o755); err != nil {
		t.Fatalf("create Downloads directory: %v", err)
	}
	app := &App{testHomeDir: home, downloadCancelers: make(map[string]*downloadCanceler)}
	_, err := app.DownloadUpdateWithSHA256(first.URL+"\n"+second.URL, "MaClaw-Setup.exe", strings.Repeat("0", 64))
	if err == nil || !strings.Contains(err.Error(), "integrity verification failed") || !strings.Contains(err.Error(), "403 Forbidden") {
		t.Fatalf("DownloadUpdateWithSHA256() error = %v, want checksum and fallback failures", err)
	}
	if _, err := os.Stat(filepath.Join(home, "Downloads", "MaClaw-Setup.exe")); !os.IsNotExist(err) {
		t.Fatalf("installer remains after checksum failure and failed fallback: %v", err)
	}
}
