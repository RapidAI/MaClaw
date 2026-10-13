package guiapp

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/brand"
	"github.com/RapidAI/CodeClaw/corelib/websearch"
)

// The 打开 menu's arXiv branch: an ID or any arXiv link is turned into the
// canonical PDF URL, downloaded once into a local folder, and opened as a
// normal companion tab.
const (
	// arxivDownloadTimeoutS bounds one PDF download. Papers can be tens of
	// megabytes, so this is far above the text-fetch default.
	arxivDownloadTimeoutS = 90
	// arxivDownloadSlack is the extra context budget over the fetch timeout so
	// the wrapper reports its own error instead of a bare context deadline.
	arxivDownloadSlack = 30 * time.Second
	// arxivKeptStageSuffix names the staging file kept after a failed rename.
	// The name is deterministic, unlike the per-attempt ones used while
	// downloading, so a later attempt for the same paper can find and reuse the
	// bytes instead of fetching them again.
	arxivKeptStageSuffix = ".part"
	// arxivStaleStageGrace is how old a staging file must be before it counts
	// as abandoned. A live download is bounded by arxivDownloadTimeoutS plus
	// slack, so nothing older can still belong to a running attempt.
	arxivStaleStageGrace = 15 * time.Minute
	// arxivWrapChars surround a pasted reference: angle brackets from an
	// autolink, quotes from copying out of prose, and their full-width forms.
	arxivWrapChars = "<>\"'“”‘’《》「」（）() \t\r\n"
	// arxivTrailChars are sentence punctuation left behind when a single
	// reference is copied out of running text: "see arXiv:2510.16079."
	arxivTrailChars = ".,;:!?。、，；：！？…)]）】》」"
)

// arxivPDFBaseURL is the canonical arXiv PDF endpoint. A version-less ID
// resolves to the latest revision. Tests point it at a local server.
var arxivPDFBaseURL = "https://arxiv.org/pdf/"

// arxivPaperDirHook replaces the download folder in tests.
var arxivPaperDirHook func() (string, error)

// arxivSavePathHook observes the staging file each attempt hands to the
// fetcher, so a test can prove two concurrent downloads never share one.
var arxivSavePathHook func(path string)

var (
	// 2510.16079 — the current arXiv identifier shape.
	arxivNewIDRe = regexp.MustCompile(`^\d{4}\.\d{4,5}$`)
	// cs/0701001 and math.GT/0309136 — pre-2007 identifiers still download.
	// arXiv writes the subject class in mixed case, so this is matched
	// case-insensitively; the ID is forwarded exactly as it was resolved.
	arxivOldIDRe = regexp.MustCompile(`(?i)^[a-z][a-z0-9-]*(\.[a-z]{2})?/\d{7}$`)
	// 2510.16079v2 — the version suffix is dropped: the reader wants the paper,
	// not one frozen revision, and the file name must stay stable across them.
	arxivVersionRe = regexp.MustCompile(`^(.+?)[vV]\d+$`)
)

// arxivIDFromInput accepts an arXiv ID ("2510.16079", "arXiv:2510.16079v2",
// "cs/0701001") or any arXiv link (abs / pdf / html) and returns the
// version-less canonical ID. An empty string means the input is not an arXiv
// reference, which the menu reports as a bad input rather than a failed
// download.
func arxivIDFromInput(input string) string {
	raw := arxivNormalizeInput(input)
	if raw == "" {
		return ""
	}
	if id := arxivParseCandidate(raw); id != "" {
		return id
	}
	// A reference pasted out of running text carries its surroundings along:
	// "详见 arXiv:2510.16079。" or arXiv's own "Cite as: arXiv:2510.16079
	// [cs.CL]", which is what every abstract page prints. Rejecting those would
	// tell the user their valid ID is bad input, which is the same wrong answer
	// the Markdown wrapper used to produce. Each word is therefore tried on its
	// own, so a label before the reference or a category annotation after it is
	// simply not part of any word that parses. Every candidate still goes
	// through the host whitelist, so a non-arXiv target never becomes a
	// download.
	for _, word := range strings.Fields(raw) {
		candidate := arxivNormalizeInput(word)
		if candidate == "" {
			continue
		}
		if id := arxivParseCandidate(candidate); id != "" {
			return id
		}
	}
	return ""
}

// arxivParseCandidate resolves one normalized string, trying the plain form
// first and then the Markdown "[text](url)" wrapper.
func arxivParseCandidate(raw string) string {
	if id := arxivParseID(raw); id != "" {
		return id
	}
	if unwrapped := arxivUnwrapMarkdownLink(raw); unwrapped != raw {
		return arxivParseID(unwrapped)
	}
	return ""
}

// arxivNormalizeInput strips what surrounds a reference without changing what
// it points at: sentence punctuation from a copy out of running text, then the
// brackets and quotes around it.
func arxivNormalizeInput(input string) string {
	raw := strings.TrimSpace(input)
	for strings.TrimSpace(raw) != "" {
		next := strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), arxivTrailChars))
		if next == raw {
			break
		}
		raw = next
	}
	return strings.Trim(raw, arxivWrapChars)
}

// arxivUnwrapMarkdownLink pulls the URL out of a "[text](url)" wrapper. The
// input is returned unchanged when there is no such wrapper or when it does
// not hold an http(s) URL, so a plain string is never rewritten.
func arxivUnwrapMarkdownLink(raw string) string {
	open := strings.Index(raw, "](")
	if open < 0 {
		return raw
	}
	inner := raw[open+2:]
	if end := strings.IndexByte(inner, ')'); end >= 0 {
		inner = inner[:end]
	}
	inner = strings.TrimSpace(inner)
	lower := strings.ToLower(inner)
	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return raw
	}
	return inner
}

func arxivParseID(raw string) string {
	// A pasted link often carries ?context= or #section.
	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	lower := strings.ToLower(raw)
	isURL := false
	switch {
	case strings.HasPrefix(lower, "http://"), strings.HasPrefix(lower, "https://"):
		parsed, err := url.Parse(raw)
		if err != nil || parsed == nil {
			return ""
		}
		// Only arXiv itself: a look-alike host must not steer the download.
		// Hostname (not Host) so an explicit port stays part of the identity.
		if !arxivHostAllowed(parsed.Hostname()) {
			return ""
		}
		raw = parsed.Path
		isURL = true
	case strings.HasPrefix(lower, "arxiv.org/"), strings.HasPrefix(lower, "www.arxiv.org/"), strings.HasPrefix(lower, "export.arxiv.org/"):
		if i := strings.Index(raw, "/"); i >= 0 {
			raw = raw[i+1:]
		}
		isURL = true
	case strings.HasPrefix(lower, "arxiv:"):
		raw = raw[len("arxiv:"):]
	}
	// Path prefixes only belong to a link. A hand-typed "abs/2510.16079" is
	// not an ID and must stay rejected.
	if isURL {
		raw = strings.TrimPrefix(raw, "/")
		for _, prefix := range []string{"abs/", "pdf/", "html/", "format/", "browse/"} {
			if len(raw) > len(prefix) && strings.HasPrefix(strings.ToLower(raw), prefix) {
				raw = raw[len(prefix):]
				break
			}
		}
	}
	raw = strings.Trim(raw, "/")
	if strings.HasSuffix(strings.ToLower(raw), ".pdf") {
		raw = raw[:len(raw)-len(".pdf")]
	}
	raw = strings.TrimSpace(raw)
	if match := arxivVersionRe.FindStringSubmatch(raw); match != nil {
		raw = match[1]
	}
	if !arxivNewIDRe.MatchString(raw) && !arxivOldIDRe.MatchString(raw) {
		return ""
	}
	// A bare ID typed by hand is fine; a link to some other site is not.
	if !isURL && strings.Contains(raw, "/") && !arxivOldIDRe.MatchString(raw) {
		return ""
	}
	return raw
}

func arxivHostAllowed(host string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	host = strings.TrimSuffix(host, ".")
	if host == "" {
		return false
	}
	return host == "arxiv.org" || strings.HasSuffix(host, ".arxiv.org")
}

// arxivPaperFileName is the on-disk name for one ID. The ID is already
// whitelisted by arxivIDFromInput, so only the old-style slash is folded. A
// basename would be wrong here: cs/0701001 and math/0701001 would collapse
// onto 0701001.pdf and the reuse check would hand back the wrong paper.
func arxivPaperFileName(id string) string {
	name := strings.ReplaceAll(strings.TrimSpace(id), "/", "_")
	if name == "" {
		name = "paper"
	}
	return name + ".pdf"
}

// arxivPaperFolderName is the subfolder downloaded papers land in, under the
// user's Downloads folder. The prefix is the build's brand so a TigerClaw or
// MetaStaff build does not file its papers under another product's name; the
// MaClaw default keeps existing folders exactly where they are.
func arxivPaperFolderName() string {
	return arxivPaperFolderNameFor(brand.Current().DisplayName)
}

func arxivPaperFolderNameFor(displayName string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = "MaClaw"
	}
	return name + " 伴读"
}

// arxivPaperDir is the folder downloaded papers are written to: the user's
// Downloads folder (resolved through the platform helper, so a redirected
// OneDrive folder on Windows is honoured rather than bypassed) with a
// companion subfolder, otherwise the home directory. The folder is created
// on first use.
func (a *App) arxivPaperDir() (string, error) {
	if arxivPaperDirHook != nil {
		return arxivPaperDirHook()
	}
	base := ""
	if a != nil {
		if dir, err := a.GetDownloadsFolder(); err == nil {
			base = strings.TrimSpace(dir)
		}
	}
	home := ""
	if dir, err := os.UserHomeDir(); err == nil {
		home = strings.TrimSpace(dir)
	}
	if base == "" {
		base = home
	}
	if base == "" {
		return "", errors.New("找不到用户目录，无法保存论文")
	}
	// A Downloads folder that is missing or is a file must not be created:
	// papers would land somewhere the user never opens.
	if info, statErr := os.Stat(base); statErr != nil || !info.IsDir() {
		if home == "" || home == base {
			return "", errors.New("找不到下载文件夹，无法保存论文")
		}
		base = home
	}
	return arxivPaperDirUnder(base)
}

// arxivPaperDirUnder creates and returns the companion subfolder inside base.
// It is split out from the base lookup so a test can prove the folder really is
// filed under the build's brand, rather than only that the name helper agrees
// with the brand.
func arxivPaperDirUnder(base string) (string, error) {
	dir := filepath.Join(base, arxivPaperFolderName())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("创建目录失败：%s", err.Error())
	}
	return dir, nil
}

// arxivPDFHeaderScan is how far into a file the PDF header is looked for. The
// PDF specification puts "%PDF-" on the first line but tells readers to search
// within the first 1024 bytes, because some producers emit a blank line or a
// byte-order mark ahead of it.
const arxivPDFHeaderScan = 1024

// fileIsPDF reports whether the file exists and carries the PDF header, so a
// saved arXiv error page is never handed to the reader as a paper.
func fileIsPDF(path string) bool {
	file, err := os.Open(path)
	if err != nil {
		return false
	}
	defer file.Close()
	head, err := io.ReadAll(io.LimitReader(file, arxivPDFHeaderScan))
	if err != nil || len(head) == 0 {
		return false
	}
	// Matching anywhere in the scan window rather than at byte zero keeps a
	// valid paper from being reported as "no PDF, the paper may not exist" just
	// because its producer left a leading blank line.
	return bytes.Contains(head, []byte("%PDF-"))
}

// FileCompanionOpenArxiv downloads one arXiv paper and returns its local path.
// The companion window opens that path as a tab. A paper already in the folder
// is reused instead of downloaded again.
func (a *App) FileCompanionOpenArxiv(input string) (string, error) {
	id := arxivIDFromInput(input)
	if id == "" {
		return "", errors.New("请填写 Arxiv ID 或论文链接")
	}
	dir, err := a.arxivPaperDir()
	if err != nil {
		return "", err
	}
	target := filepath.Join(dir, arxivPaperFileName(id))
	if fileIsPDF(target) {
		logFileCompanion("arxiv reuse id=%s", id)
		return target, nil
	}
	// A previous attempt may have downloaded this paper but failed to file it
	// under its own name. Those bytes are parked at a deterministic name, so
	// retry the rename rather than fetching tens of megabytes again.
	kept := target + arxivKeptStageSuffix
	if info, statErr := os.Stat(kept); statErr == nil && !info.IsDir() {
		if fileIsPDF(kept) {
			if renameErr := os.Rename(kept, target); renameErr != nil {
				// The name is still occupied, so the bytes cannot be delivered
				// however often they are fetched. Fail without burning bandwidth.
				return "", fmt.Errorf("保存失败：%s", renameErr.Error())
			}
			logFileCompanion("arxiv reuse staged id=%s", id)
			return target, nil
		}
		// Only a valid PDF is ever parked here, so anything else is unusable
		// debris from an older build or a stray file. Drop it rather than let
		// it shadow the paper forever.
		_ = os.Remove(kept)
	}
	// Staging files from attempts that died with the process are unreachable:
	// their names are random, so nothing would ever clean them up again.
	arxivReapStaleStaging(dir, arxivPaperFileName(id))
	// Download beside the target and rename, so a killed download leaves no
	// half PDF under the paper's own name. The staging file gets a unique name
	// per attempt: the fetch writes straight to the path it is given, so two
	// downloads of one ID sharing a fixed name would interleave their bytes
	// into a file whose header still reads %PDF- and would then pass the magic
	// check as a silently corrupt paper.
	stageFile, err := os.CreateTemp(dir, arxivPaperFileName(id)+".*.part")
	if err != nil {
		return "", fmt.Errorf("创建暂存文件失败：%s", err.Error())
	}
	staged := stageFile.Name()
	_ = stageFile.Close()
	// Handing this back to the fetcher as a SavePath means the file it creates
	// replaces ours; either way the name is ours alone until it is renamed.
	_ = os.Remove(staged)
	stagedGone := false
	// Cleanup is unconditional except when the download itself has to be kept:
	// see the rename failure below.
	defer func() {
		if stagedGone {
			return
		}
		if _, statErr := os.Stat(staged); statErr == nil {
			_ = os.Remove(staged)
		}
	}()
	parent := context.Background()
	if a != nil && a.ctx != nil {
		parent = a.ctx
	}
	ctx, cancel := context.WithTimeout(parent, arxivDownloadTimeoutS*time.Second+arxivDownloadSlack)
	defer cancel()
	opts := &websearch.FetchOptions{
		SavePath: staged,
		MaxBytes: maxWebFetchDownloadBytes,
		TimeoutS: arxivDownloadTimeoutS,
	}
	// Tests observe the staging path each attempt hands to the fetcher: two
	// concurrent downloads must never be given the same one.
	if arxivSavePathHook != nil {
		arxivSavePathHook(staged)
	}
	var strategy corelib.WebSearchStrategy
	if a != nil && a.imHandler != nil {
		strategy = a.imHandler.getWebSearchStrategy()
	}
	result, err := websearch.FetchWithStrategyCtx(ctx, arxivPDFBaseURL+id, opts, strategy)
	if err != nil {
		return "", errors.New(arxivDownloadErrorText(err))
	}
	saved := ""
	if result != nil {
		saved = strings.TrimSpace(result.SavedTo)
	}
	if saved == "" {
		saved = staged
	}
	if !fileIsPDF(saved) {
		return "", errors.New("下载失败：没有拿到 PDF，可能这篇论文不存在")
	}
	if err := os.Rename(saved, target); err != nil {
		// The paper itself is already on disk and valid; deleting it here
		// would throw away a good download over a bookkeeping failure. Park it
		// at the deterministic name so the next attempt for this ID finds it
		// and only has to retry the rename. If even that fails, the unique
		// staging name is left for the deferred cleanup: nothing would ever
		// look for it again.
		if parkErr := os.Rename(saved, kept); parkErr == nil {
			stagedGone = true
		}
		return "", fmt.Errorf("保存失败：%s", err.Error())
	}
	stagedGone = true
	logFileCompanion("arxiv download id=%s bytes_path=%s", id, target)
	return target, nil
}

// arxivReapStaleStaging removes staging files for one paper that no running
// download can still own. Live attempts hold unique names with fresh
// timestamps; only a process that died mid-download leaves one behind, and its
// random name means nothing else would ever collect it. The deterministic name
// used to park a paper is left alone: it is the one a retry looks for.
func arxivReapStaleStaging(dir, paperName string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	prefix := paperName + "."
	parked := paperName + arxivKeptStageSuffix
	cutoff := time.Now().Add(-arxivStaleStageGrace)
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || name == parked {
			continue
		}
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".part") {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || info.ModTime().After(cutoff) {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}

// arxivDownloadErrorText turns a fetch failure into a reader-facing sentence.
// The raw error can name internal transports, so only the recognizable cases
// are translated.
//
// The URL is stripped before matching. A transport error embeds the whole URL
// — Get "https://arxiv.org/pdf/2404.12345": dial tcp ... — and arXiv IDs are
// just digit runs, so 2404.12345 or 1403.2040 would otherwise be read as HTTP
// 404 / HTTP 403. That misdiagnosed a DNS failure as "no such paper" and sent
// users off to correct an ID that was right all along.
func arxivDownloadErrorText(err error) string {
	if err == nil {
		return "下载失败"
	}
	msg := strings.TrimSpace(err.Error())
	what := strings.ToLower(arxivStripURL(msg))
	switch {
	case arxivStatusLooksNotFound(what):
		return "下载失败：找不到这篇论文（404）"
	case arxivStatusLooksForbidden(what):
		return "下载失败：arXiv 拒绝了这次请求，请稍后再试"
	case strings.Contains(what, "timed out"), strings.Contains(what, "deadline exceeded"), strings.Contains(what, "context deadline"):
		return "下载失败：连接 arXiv 超时，请稍后再试"
	}
	if msg == "" {
		return "下载失败"
	}
	return arxivShortFailure(msg)
}

// arxivStripURL removes every quoted URL from a transport error so only the
// failure itself is left to classify.
func arxivStripURL(msg string) string {
	out := msg
	for {
		start := strings.IndexByte(out, '"')
		if start < 0 {
			return out
		}
		end := strings.IndexByte(out[start+1:], '"')
		if end < 0 {
			return out[:start]
		}
		out = out[:start] + out[start+1+end+1:]
	}
}

func arxivStatusLooksNotFound(what string) bool {
	for _, token := range []string{"status 404", "status code 404", "404 not found", "http 404"} {
		if strings.Contains(what, token) {
			return true
		}
	}
	return arxivLemmaIsStatus(what, "404")
}

func arxivStatusLooksForbidden(what string) bool {
	for _, token := range []string{"status 403", "status code 403", "403 forbidden", "http 403", "blocked"} {
		if strings.Contains(what, token) {
			return true
		}
	}
	return arxivLemmaIsStatus(what, "403")
}

// arxivStatusCodeRe matches a status code only as a standalone token: codes
// are fixed strings and compiling them per failure buys nothing.
var arxivStatusCodeRe = map[string]*regexp.Regexp{
	"404": regexp.MustCompile(`(^|[^0-9a-z.])404([^0-9a-z]|$)`),
	"403": regexp.MustCompile(`(^|[^0-9a-z.])403([^0-9a-z]|$)`),
}

// arxivLemmaIsStatus recognizes a bare status code as a standalone token. The
// old code matched the bare digits anywhere, which is how an ID inside the URL
// got read as a status. A lone "404" with nothing around it is still accepted
// because that is how several transports report it.
func arxivLemmaIsStatus(what string, code string) bool {
	re, ok := arxivStatusCodeRe[code]
	return ok && re.MatchString(what)
}

// arxivShortFailure caps a raw error so one long diagnostic line cannot push
// the rest of the panel out of view.
func arxivShortFailure(msg string) string {
	runes := []rune(msg)
	if len(runes) > 80 {
		msg = string(runes[:80]) + "…"
	}
	return "下载失败：" + msg
}
