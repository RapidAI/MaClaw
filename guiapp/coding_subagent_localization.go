package guiapp

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

const (
	codeNavigationToolName     = "code_navigation"
	reportLocalizationToolName = "report_localization"
	codegraphNPMPackage        = "@colbymchenry/codegraph"
)

// navigationRemoteFileSymbolPattern keeps definitions. A bare call ends in ';'.
const navigationRemoteFileSymbolPattern = `^[[:space:]]*(#define|typedef|struct|class|enum)[[:space:]]|^[[:space:]]*(static[[:space:]]+|const[[:space:]]+|inline[[:space:]]+|virtual[[:space:]]+)*([A-Za-z_][[:alnum:]_:]*[[:space:]]+)+[~A-Za-z_][[:alnum:]_:]*[[:space:]]*\([^;]*\)[[:space:]]*(const[[:space:]]*)?\{[[:space:]]*$`

var codegraphScorePattern = regexp.MustCompile(`\((\d+)%\)`)
var codegraphANSIPattern = regexp.MustCompile("\x1b\\[[0-9;]*[A-Za-z]")

// CodingSubAgentBugSignal is the normalized, machine-auditable form of a bug report.
type CodingSubAgentBugSignal struct {
	ErrorStrings     []string `json:"error_strings,omitempty"`
	StackFrames      []string `json:"stack_frames,omitempty"`
	EntryPoints      []string `json:"entry_points,omitempty"`
	ExpectedBehavior string   `json:"expected_behavior,omitempty"`
	ActualBehavior   string   `json:"actual_behavior,omitempty"`
	Reproduction     string   `json:"reproduction,omitempty"`
}

// CodingSubAgentLocalizationCandidate is a ranked suspect with explainable evidence.
type CodingSubAgentLocalizationCandidate struct {
	File                  string   `json:"file"`
	Symbol                string   `json:"symbol,omitempty"`
	Score                 float64  `json:"score"`
	SupportingEvidence    []string `json:"supporting_evidence,omitempty"`
	ContradictingEvidence []string `json:"contradicting_evidence,omitempty"`
	NextProbe             string   `json:"next_probe,omitempty"`
}

// CodingSubAgentLocalizationEvidence is shared by local, remote, worker, and explorer agents.
type CodingSubAgentLocalizationEvidence struct {
	Signal             CodingSubAgentBugSignal               `json:"signal"`
	Candidates         []CodingSubAgentLocalizationCandidate `json:"candidates,omitempty"`
	RootCauseFile      string                                `json:"root_cause_file"`
	RootCauseSymbol    string                                `json:"root_cause_symbol,omitempty"`
	CausalPath         []string                              `json:"causal_path"`
	Reproduction       string                                `json:"reproduction"`
	SupportingEvidence []string                              `json:"supporting_evidence"`
	RejectedHypotheses []string                              `json:"rejected_hypotheses,omitempty"`
	FocusedTests       []string                              `json:"focused_tests,omitempty"`
	ResearchDecision   string                                `json:"research_decision"`
	ResearchReason     string                                `json:"research_reason"`
	ExternalSources    []string                              `json:"external_sources,omitempty"`
	Confidence         float64                               `json:"confidence"`
	ReportedAt         time.Time                             `json:"reported_at"`
}

type codingSubAgentLocalizationState struct {
	mu       sync.Mutex
	evidence *CodingSubAgentLocalizationEvidence
	// revision is a callback-local control-plane generation. It is deliberately
	// not a provider response/tool-call identity and is never persisted or used
	// as a grant/journal key. It only prevents evidence accepted for an older
	// rendered static surface from authorizing an edit after replacement.
	revision uint64
}

func (s *codingSubAgentLocalizationState) set(e CodingSubAgentLocalizationEvidence) {
	s.setForRevision(e, 0)
}

func (s *codingSubAgentLocalizationState) setForRevision(e CodingSubAgentLocalizationEvidence, revision uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := normalizeLocalizationEvidence(e)
	if len(copy.FocusedTests) == 0 {
		copy.FocusedTests = localizationFocusedTestSuggestions(copy.RootCauseFile, copy.RootCauseSymbol)
	}
	copy.Candidates = rankLocalizationCandidates(copy.Candidates)
	copy.ReportedAt = time.Now().UTC()
	s.evidence = &copy
	s.revision = revision
}

func localizationCandidatesFromOutput(output string) []CodingSubAgentLocalizationCandidate {
	seen := map[string]*CodingSubAgentLocalizationCandidate{}
	for _, line := range strings.Split(output, "\n") {
		for _, m := range localizationStackFrameRE.FindAllStringSubmatch(line, -1) {
			if len(m) < 2 {
				continue
			}
			file := filepath.ToSlash(strings.TrimSpace(m[1]))
			candidate := seen[file]
			if candidate == nil {
				candidate = &CodingSubAgentLocalizationCandidate{File: file, Score: 1, NextProbe: "read symbol context and trace references/callers"}
				seen[file] = candidate
			}
			candidate.Score += 1
			candidate.SupportingEvidence = append(candidate.SupportingEvidence, truncateRunesForSubAgent(strings.TrimSpace(line), 240))
		}
	}
	out := make([]CodingSubAgentLocalizationCandidate, 0, len(seen))
	for _, c := range seen {
		out = append(out, *c)
	}
	return rankLocalizationCandidates(out)
}

func (s *codingSubAgentLocalizationState) snapshot() *CodingSubAgentLocalizationEvidence {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.evidence == nil {
		return nil
	}
	out := normalizeLocalizationEvidence(*s.evidence)
	out.ReportedAt = s.evidence.ReportedAt
	return &out
}

// snapshotForRevision returns evidence only when it was accepted for the same
// callback-local rendered-surface generation. Revision zero is the explicit
// direct-host compatibility path; it never becomes evidence for a later model
// request because the first rendered surface is revision one.
func (s *codingSubAgentLocalizationState) snapshotForRevision(revision uint64) *CodingSubAgentLocalizationEvidence {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.evidence == nil || s.revision != revision {
		return nil
	}
	out := normalizeLocalizationEvidence(*s.evidence)
	out.ReportedAt = s.evidence.ReportedAt
	return &out
}

func rankLocalizationCandidates(in []CodingSubAgentLocalizationCandidate) []CodingSubAgentLocalizationCandidate {
	out := make([]CodingSubAgentLocalizationCandidate, len(in))
	copy(out, in)
	for i := range out {
		out[i] = normalizeLocalizationCandidate(out[i])
	}
	// Score remains the model/tool's base score. Compute evidence weight only in
	// the comparator so ranking the same candidates repeatedly is idempotent.
	effectiveScore := func(c CodingSubAgentLocalizationCandidate) float64 {
		score := c.Score + float64(len(c.SupportingEvidence))*2 - float64(len(c.ContradictingEvidence))*3
		if c.Symbol != "" {
			score += 2
		}
		return score
	}
	sort.SliceStable(out, func(i, j int) bool {
		left, right := effectiveScore(out[i]), effectiveScore(out[j])
		if left == right {
			return out[i].File < out[j].File
		}
		return left > right
	})
	if len(out) > 10 {
		out = out[:10]
	}
	return out
}

func normalizeLocalizationCandidate(c CodingSubAgentLocalizationCandidate) CodingSubAgentLocalizationCandidate {
	c.File = strings.TrimSpace(c.File)
	c.Symbol = strings.TrimSpace(c.Symbol)
	c.NextProbe = strings.TrimSpace(c.NextProbe)
	c.SupportingEvidence = normalizeLocalizationStrings(c.SupportingEvidence)
	c.ContradictingEvidence = normalizeLocalizationStrings(c.ContradictingEvidence)
	return c
}

func normalizeLocalizationEvidence(e CodingSubAgentLocalizationEvidence) CodingSubAgentLocalizationEvidence {
	e.Signal.StackFrames = normalizeLocalizationStrings(e.Signal.StackFrames)
	e.Signal.ErrorStrings = normalizeLocalizationStrings(e.Signal.ErrorStrings)
	e.Signal.EntryPoints = normalizeLocalizationStrings(e.Signal.EntryPoints)
	e.RootCauseFile = strings.TrimSpace(e.RootCauseFile)
	e.RootCauseSymbol = strings.TrimSpace(e.RootCauseSymbol)
	e.Reproduction = strings.TrimSpace(e.Reproduction)
	e.ResearchDecision = strings.ToLower(strings.TrimSpace(e.ResearchDecision))
	e.ResearchReason = strings.TrimSpace(e.ResearchReason)
	e.CausalPath = normalizeLocalizationStrings(e.CausalPath)
	e.SupportingEvidence = normalizeLocalizationStrings(e.SupportingEvidence)
	e.RejectedHypotheses = normalizeLocalizationStrings(e.RejectedHypotheses)
	e.FocusedTests = normalizeLocalizationStrings(e.FocusedTests)
	e.ExternalSources = normalizeLocalizationStrings(e.ExternalSources)
	candidates := e.Candidates
	e.Candidates = make([]CodingSubAgentLocalizationCandidate, len(candidates))
	for i, candidate := range candidates {
		e.Candidates[i] = normalizeLocalizationCandidate(candidate)
	}
	return e
}

func validateLocalizationEvidencePath(field, value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return fmt.Errorf("%s is required", field)
	}
	// Evidence can describe a remote path whose separator differs from the
	// coordinator's, so validate components without host-dependent filepath.Clean.
	normalized := strings.ReplaceAll(value, `\`, "/")
	if normalized == "." || normalized == "/" {
		return fmt.Errorf("%s must identify a source file", field)
	}
	if strings.HasSuffix(normalized, "/") {
		return fmt.Errorf("%s must identify a source file, not a directory", field)
	}
	for _, component := range strings.Split(normalized, "/") {
		if component == ".." {
			return fmt.Errorf("%s must not contain unresolved parent traversal", field)
		}
	}
	return nil
}

func normalizeLocalizationStrings(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, value := range in {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

var (
	localizationStackFrameRE = regexp.MustCompile(`(?m)((?:[A-Za-z]:)?[A-Za-z0-9_./\\-]+\.(?:go|py|js|jsx|ts|tsx|rs|java|kt|cpp|cc|c|h|cs|rb|php))(?::|\()(\d+)`)
	localizationQuotedRE     = regexp.MustCompile(`["'“”]([^"'“”\r\n]{4,180})["'“”]`)
)

func extractCodingSubAgentBugSignal(text string) CodingSubAgentBugSignal {
	text = strings.TrimSpace(text)
	signal := CodingSubAgentBugSignal{}
	for _, m := range localizationStackFrameRE.FindAllStringSubmatch(text, 12) {
		signal.StackFrames = append(signal.StackFrames, m[1]+":"+m[2])
	}
	for _, m := range localizationQuotedRE.FindAllStringSubmatch(text, 12) {
		v := strings.TrimSpace(m[1])
		lowerV := strings.ToLower(v)
		if strings.Contains(lowerV, "error") || strings.Contains(lowerV, "fail") || strings.Contains(lowerV, "panic") || strings.Contains(lowerV, "crash") || strings.Contains(lowerV, "exception") || strings.ContainsAny(v, "错误异常失败崩溃") {
			signal.ErrorStrings = append(signal.ErrorStrings, v)
		}
	}
	lower := strings.ToLower(text)
	if i := strings.Index(lower, "expected"); i >= 0 {
		signal.ExpectedBehavior = truncateRunesForSubAgent(text[i:], 240)
	}
	if i := strings.Index(lower, "actual"); i >= 0 {
		signal.ActualBehavior = truncateRunesForSubAgent(text[i:], 240)
	}
	if strings.ContainsAny(text, "复现重现") || strings.Contains(lower, "repro") {
		signal.Reproduction = truncateRunesForSubAgent(text, 500)
	}
	signal.StackFrames = uniqueSortedSubAgentStrings(signal.StackFrames)
	signal.ErrorStrings = uniqueSortedSubAgentStrings(signal.ErrorStrings)
	return signal
}

func codingTaskNeedsLocalization(text string) bool {
	lower := strings.ToLower(codingTaskLocalizationIntent(text))
	if lower == "" {
		return false
	}
	for _, marker := range []string{
		"bug", "fix", "broken", "regression", "crash", "panic", "exception",
		"error", "fault", "incorrect", "fail", "failed", "fails", "failing", "failure",
		"hang", "hangs", "hanging", "hung", "timeout",
	} {
		if localizationContainsWord(lower, marker) {
			return true
		}
	}
	for _, marker := range []string{"错误", "异常", "崩溃", "故障", "修复", "不正确", "失效", "失败", "根因"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	// Bare "定位" matches tool/meta prose ("定位报告", "先定位相关路径") and then
	// poisons a feature task into the bug-fix edit gate. Require a bug-shaped
	// collocation instead.
	for _, marker := range []string{"故障定位", "根因定位", "问题定位", "定位根因", "定位崩溃", "定位故障", "定位问题", "定位错误"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// codingTaskLocalizationIntent drops machine carry-over (prior-step status,
// last-turn summaries, file lists). Those blocks often contain "failed" /
// "error" / "定位报告" from the previous gate and would otherwise reclassify a
// feature step as a bug-fix, forcing report_localization in a retry loop.
func codingTaskLocalizationIntent(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lower := strings.ToLower(text)
	cut := len(text)
	for _, marker := range []string{
		"previous turn summary:",
		"files modified earlier:",
		"prior plan step ",
	} {
		if i := strings.Index(lower, marker); i >= 0 && i < cut {
			cut = i
		}
	}
	return strings.TrimSpace(text[:cut])
}

// codingTaskNeedsExternalResearch identifies bug reports whose answer depends
// on unfamiliar, version-sensitive, or third-party facts that cannot be proven
// from the repository alone. Keep this deliberately narrower than the general
// bug detector so ordinary local logic bugs do not incur mandatory network use.
func codingTaskNeedsExternalResearch(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))
	if lower == "" {
		return false
	}
	strongWordMarkers := []string{
		"unknown", "unfamiliar", "latest", "upgrade", "upgraded", "deprecated", "deprecation",
		"dependency", "compatibility", "sdk", "protocol", "driver", "version", "vendor",
	}
	for _, marker := range strongWordMarkers {
		if localizationContainsWord(lower, marker) {
			return true
		}
	}
	phraseMarkers := []string{
		"never seen", "don't know", "do not know", "current version", "new version",
		"third-party", "third party", "breaking change", "api changed", "vendor error",
		"不知道", "不清楚", "不熟悉", "没见过", "陌生", "未知",
		"最新版", "新版本", "升级后", "更新后", "弃用", "第三方", "依赖", "兼容性", "协议",
		"错误码", "官方文档", "外部服务", "云服务商", "供应商",
	}
	for _, marker := range phraseMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	if localizationResearchHasNamedExternalSurface(text) {
		return true
	}
	// Generic engineering nouns also describe repository-owned abstractions
	// ("internal API", "local provider", "package parser"). Treat them as an
	// external signal only when the report does not explicitly scope the failure
	// to repository-owned code. This keeps research proactive without forcing a
	// network dependency for ordinary local implementation bugs.
	for _, marker := range []string{"package", "library", "framework", "api", "plugin", "extension", "provider"} {
		if localizationContainsWord(lower, marker) && !localizationResearchGenericSurfaceIsLocallyQualified(lower, marker) {
			return true
		}
	}
	return false
}

func localizationResearchGenericSurfaceIsLocallyQualified(text, surface string) bool {
	for _, phrase := range []string{
		"仓内 " + surface, "仓库内 " + surface, "本地 " + surface,
		"内部 " + surface, "自研 " + surface,
	} {
		if strings.Contains(text, phrase) {
			return true
		}
	}
	localQualifiers := map[string]bool{
		"internal": true, "local": true, "repository": true, "repo": true,
		"private": true, "custom": true, "in-house": true, "our": true,
	}
	tokens := strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-'
	})
	for index, token := range tokens {
		if token != surface {
			continue
		}
		for offset := 1; offset <= 2 && index-offset >= 0; offset++ {
			if localQualifiers[tokens[index-offset]] {
				return true
			}
		}
		return false
	}
	return false
}

var localizationNamedExternalSurfaceRE = regexp.MustCompile(`\b([A-Z][A-Za-z0-9.+_-]{2,})\s+(?i:API|framework|library|plugin|extension|provider)\b`)

func localizationResearchHasNamedExternalSurface(text string) bool {
	for _, indexes := range localizationNamedExternalSurfaceRE.FindAllStringSubmatchIndex(text, -1) {
		if len(indexes) < 4 {
			continue
		}
		match := text[indexes[2]:indexes[3]]
		prefixStart := indexes[0] - 32
		if prefixStart < 0 {
			prefixStart = 0
		}
		prefix := strings.ToLower(text[prefixStart:indexes[0]])
		if localizationResearchNamedSurfaceIsLocallyQualified(prefix) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(match)) {
		case "internal", "local", "repository", "private", "custom":
			continue
		default:
			return true
		}
	}
	return false
}

func localizationResearchNamedSurfaceIsLocallyQualified(prefix string) bool {
	tokens := strings.FieldsFunc(prefix, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && r != '-'
	})
	if len(tokens) == 0 {
		return false
	}
	start := len(tokens) - 3
	if start < 0 {
		start = 0
	}
	for _, token := range tokens[start:] {
		switch token {
		case "internal", "local", "private", "custom", "in-house", "our", "自研", "内部", "本地":
			return true
		}
	}
	return false
}

func localizationContainsWord(text, word string) bool {
	for start := 0; ; {
		index := strings.Index(text[start:], word)
		if index < 0 {
			return false
		}
		index += start
		end := index + len(word)
		leftOK := index == 0 || !localizationASCIIWordByte(text[index-1])
		rightOK := end == len(text) || !localizationASCIIWordByte(text[end])
		if leftOK && rightOK {
			return true
		}
		start = index + 1
		if start >= len(text) {
			return false
		}
	}
}

func localizationASCIIWordByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9' || b == '_'
}
func codingWebResearchResultLooksFailed(result string) bool {
	lower := strings.ToLower(strings.TrimSpace(result))
	if lower == "" {
		return true
	}
	// toolWebSearch emits this stable header only after receiving a non-empty
	// result slice. Check it before scanning snippets: a legitimate article may
	// itself discuss "no results" or "search failed" and must not turn the whole
	// audited search into a false failure.
	if codingWebResearchHasSuccessfulResultHeader(lower) {
		return false
	}
	for _, marker := range []string{
		"web_search unavailable", "search unavailable", "search failed", "request failed",
		"no search provider", "provider not configured", "timed out", "timeout",
		"no results", "no relevant results", "nothing found",
		"搜索不可用", "搜索失败", "未配置", "超时", "未找到相关结果", "没有找到相关结果",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func codingWebFetchResultLooksFailed(result string) bool {
	if remoteHostDownloadSaved(result) || remoteWebFetchPageRetrieved(result) {
		return false
	}
	if remoteWebFetchHostFailure(result) || remoteWebFetchToolEnvelopeFailed(result) {
		return true
	}
	trimmed := strings.TrimSpace(result)
	lower := strings.ToLower(trimmed)
	if lower == "" {
		return true
	}
	if codingWebFetchEnvelopeHasFailureMarker(trimmed) {
		return true
	}
	// A successful page extraction contains enough title/body material to audit.
	// Tiny acknowledgements such as "ok" or a bare URL do not prove that the
	// declared source was actually read.
	if utf8.RuneCountInString(trimmed) < 24 {
		return true
	}
	return false
}

// codingWebFetchEnvelopeHasFailureMarker matches role refusals in the opening
// line. The phrases are the tool's own wording, not a general "read-only" sentence.
func codingWebFetchEnvelopeHasFailureMarker(result string) bool {
	head := result
	if i := strings.IndexAny(head, "\r\n"); i >= 0 {
		head = head[:i]
	}
	runes := []rune(head)
	if len(runes) > 120 {
		head = string(runes[:120])
	}
	lower := strings.ToLower(head)
	for _, marker := range []string{
		"is not allowed for a read-only coding child",
		"is not allowed for this coding-agent role",
		"is not available for nested role",
		"is unavailable for a read-only repository inquiry",
		"is unavailable for a run/build/demo request",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

var codingWebResearchSuccessHeaderRE = regexp.MustCompile(`(?m)(?:搜索\s+.+?找到|search(?:ed)?\s+.+?found)\s+([1-9][0-9]*)\s+(?:条结果|results?)`)

const localizationWebAuditMaxRunes = 20000

func truncateLocalizationWebAudit(result string) string {
	result = strings.TrimSpace(result)
	runes := []rune(result)
	if len(runes) <= localizationWebAuditMaxRunes {
		return result
	}
	// Search sources and fetch metadata commonly appear near the tail. Preserve
	// both ends so validation does not silently lose a declared URL just because
	// a provider returned a long body or many snippets.
	const marker = "\n…（研究审计内容已截断）…\n"
	markerRunes := []rune(marker)
	available := localizationWebAuditMaxRunes - len(markerRunes)
	head := available * 3 / 4
	tail := available - head
	return string(runes[:head]) + marker + string(runes[len(runes)-tail:])
}

func codingWebResearchHasSuccessfulResultHeader(result string) bool {
	return codingWebResearchSuccessHeaderRE.MatchString(strings.ToLower(strings.TrimSpace(result)))
}

func codingWebResearchSourceCoversAudit(source string, searches []CodingSubAgentSearchResult) bool {
	source = strings.TrimSpace(source)
	if source == "" {
		return false
	}
	if strings.Contains(source, "://") && !localizationResearchSourceIsHTTPURL(source) {
		return false
	}
	for _, search := range searches {
		if !strings.EqualFold(strings.TrimSpace(search.Tool), "web_search") ||
			!search.Succeeded || codingWebResearchResultLooksFailed(search.Summary) {
			continue
		}
		// toolWebSearch repeats the query in its success header. Match sources only
		// against the result body so an echoed query cannot validate itself, while
		// still allowing a genuine result whose title happens to equal the query.
		if localizationAuditContainsSource(localizationWebSearchResultBody(search.Summary), source) {
			return true
		}
	}
	return false
}

func localizationWebSearchResultBody(summary string) string {
	lines := strings.Split(strings.ReplaceAll(summary, "\r\n", "\n"), "\n")
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if codingWebResearchHasSuccessfulResultHeader(line) {
			return strings.Join(lines[i+1:], "\n")
		}
		break
	}
	return summary
}

func localizationResearchSourceIsHTTPURL(source string) bool {
	parsed, ok := localizationResearchParsedHTTPURL(source)
	return ok && parsed.User == nil
}

func localizationResearchParsedHTTPURL(raw string) (*url.URL, bool) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" || parsed.Hostname() == "" {
		return nil, false
	}
	parsed.Scheme = strings.ToLower(parsed.Scheme)
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, false
	}
	if strings.Contains(parsed.Hostname(), "%") || !localizationResearchValidPort(parsed.Port()) {
		return nil, false
	}
	return parsed, true
}

func localizationResearchValidPort(port string) bool {
	if port == "" {
		return true
	}
	value, err := strconv.Atoi(port)
	return err == nil && value >= 1 && value <= 65535
}

func localizationAuditContainsSource(summary, source string) bool {
	summaryLower := strings.ToLower(summary)
	sourceLower := strings.ToLower(strings.TrimSpace(source))
	if sourceLower == "" {
		return false
	}
	// URLs are strong source identities. Match them as bounded URL tokens so a
	// prefix such as https://vendor.example cannot validate a different page.
	if strings.Contains(sourceLower, "://") {
		for _, token := range localizationHTTPURLTokens(summaryLower) {
			if sameLocalizationResearchURL(token, sourceLower) {
				return true
			}
		}
		return false
	}
	// Non-URL source labels need a meaningful title-sized value and bounded
	// occurrence. Tiny fragments such as "docs" are too easy to fabricate.
	if utf8.RuneCountInString(sourceLower) < 8 {
		return false
	}
	for start := 0; ; {
		index := strings.Index(summaryLower[start:], sourceLower)
		if index < 0 {
			return false
		}
		index += start
		end := index + len(sourceLower)
		leftOK := index == 0 || !localizationASCIIWordByte(summaryLower[index-1])
		rightOK := end == len(summaryLower) || !localizationASCIIWordByte(summaryLower[end])
		if leftOK && rightOK {
			return true
		}
		start = index + 1
	}
}

var localizationHTTPURLTokenRE = regexp.MustCompile(`https?://[^\s<>"'` + "`" + `()\[\]{}]+`)

func localizationHTTPURLTokens(text string) []string {
	matches := localizationHTTPURLTokenRE.FindAllString(text, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		match = strings.TrimRight(match, ".,;:!?")
		if match != "" {
			out = append(out, match)
		}
	}
	return out
}

const localizationIncompleteReportGuidance = ` If this is exploration or new-feature work, stop calling report_localization and continue with read/search/edit. Bug-fix reports need ALL of: root_cause_file, causal_path (string array), reproduction, supporting_evidence (string array), research_decision (searched|not_needed|unavailable), research_reason, confidence (number in (0,1]). Example: {"root_cause_file":"src/foo.cpp","causal_path":["caller","foo.bar"],"reproduction":"command X fails with Y","supporting_evidence":["src/foo.cpp:12 takes the failing branch"],"research_decision":"not_needed","research_reason":"failure is fully visible in repository control flow","confidence":0.8}`

const localizationReportNotRequiredMessage = "report_localization skipped: this task is not a bug-fix of existing code. Do not retry this tool. Continue exploring or implementing with read/search/edit tools."

const localizationReportOptionalMessage = "report_localization skipped: a localization report is not required for this turn. Do not retry this tool. Continue with the edit."

func localizationInvalidEvidenceMessage(err error) string {
	if err == nil {
		return "invalid localization evidence"
	}
	msg := "invalid localization evidence: " + err.Error()
	if strings.Contains(err.Error(), "missing required fields") {
		return msg + localizationIncompleteReportGuidance
	}
	return msg
}

func localizationMissingRequiredFields(e CodingSubAgentLocalizationEvidence) []string {
	var missing []string
	if strings.TrimSpace(e.RootCauseFile) == "" {
		missing = append(missing, "root_cause_file")
	}
	if len(e.CausalPath) == 0 {
		missing = append(missing, "causal_path")
	}
	if strings.TrimSpace(e.Reproduction) == "" {
		missing = append(missing, "reproduction")
	}
	if len(e.SupportingEvidence) == 0 {
		missing = append(missing, "supporting_evidence")
	}
	if strings.TrimSpace(e.ResearchDecision) == "" {
		missing = append(missing, "research_decision")
	}
	if strings.TrimSpace(e.ResearchReason) == "" {
		missing = append(missing, "research_reason")
	}
	if e.Confidence == 0 {
		missing = append(missing, "confidence")
	}
	return missing
}

func (s *codingSubAgentLocalizationState) evidenceRevision() (uint64, bool) {
	if s == nil {
		return 0, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.evidence == nil {
		return 0, false
	}
	return s.revision, true
}

func localizationStaleControlPlaneEvidenceDetail(state *codingSubAgentLocalizationState, currentRevision uint64) string {
	stored, ok := state.evidenceRevision()
	if !ok {
		return "missing localization evidence"
	}
	return fmt.Sprintf("previous report is bound to an older control-plane surface (evidence_revision=%d current_revision=%d); re-submit the full report_localization including causal_path, reproduction, supporting_evidence, research_decision, research_reason, and confidence", stored, currentRevision)
}

func localizationEvidenceAcceptedText(revision uint64, evidenceJSON string) string {
	return fmt.Sprintf("localization evidence accepted (control_plane_revision=%d). Matching edits stay authorized on later turns until the tool surface actually changes.\n%s", revision, evidenceJSON)
}

func localizationBugFixEditBlocked(remote bool, detail string) string {
	target := "an existing file"
	if remote {
		target = "remote code"
	}
	return "bug-fix edit blocked: submit report_localization with root-cause evidence before modifying " + target + ": " + detail
}

func validateLocalizationEvidence(e *CodingSubAgentLocalizationEvidence, expectedPath string) error {
	if e == nil {
		return fmt.Errorf("missing localization evidence")
	}
	normalized := normalizeLocalizationEvidence(*e)
	e = &normalized
	if missing := localizationMissingRequiredFields(*e); len(missing) > 0 {
		return fmt.Errorf("missing required fields: %s", strings.Join(missing, ", "))
	}
	if err := validateLocalizationEvidencePath("root_cause_file", e.RootCauseFile); err != nil {
		return err
	}
	if expectedPath != "" && !localizationEvidenceCoversPath(e, expectedPath) {
		return fmt.Errorf("root cause/candidates do not cover edit target %q", expectedPath)
	}
	for i, candidate := range e.Candidates {
		candidate = normalizeLocalizationCandidate(candidate)
		if err := validateLocalizationEvidencePath(fmt.Sprintf("candidates[%d].file", i), candidate.File); err != nil {
			return err
		}
		for previous := 0; previous < i; previous++ {
			if sameLocalizationPath(e.Candidates[previous].File, candidate.File) {
				return fmt.Errorf("candidates[%d].file duplicates candidates[%d].file", i, previous)
			}
		}
		if math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) {
			return fmt.Errorf("candidates[%d].score must be finite", i)
		}
		if len(candidate.SupportingEvidence) == 0 && len(candidate.ContradictingEvidence) == 0 {
			return fmt.Errorf("candidates[%d] requires supporting_evidence or contradicting_evidence", i)
		}
		if len(candidate.SupportingEvidence) > 0 && candidate.Score <= 0 {
			return fmt.Errorf("candidates[%d].score must be greater than 0 when supporting_evidence is present", i)
		}
	}
	decision := strings.ToLower(strings.TrimSpace(e.ResearchDecision))
	if decision != "searched" && decision != "not_needed" && decision != "unavailable" {
		return fmt.Errorf("research_decision must be searched, not_needed, or unavailable")
	}
	if decision == "searched" && len(e.ExternalSources) == 0 {
		return fmt.Errorf("external_sources is required when research_decision is searched")
	}
	if math.IsNaN(e.Confidence) || math.IsInf(e.Confidence, 0) || e.Confidence <= 0 || e.Confidence > 1 {
		return fmt.Errorf("confidence must be greater than 0 and at most 1")
	}
	return nil
}

func validateLocalizationResearchEvidence(taskText string, e *CodingSubAgentLocalizationEvidence, searches []CodingSubAgentSearchResult) error {
	if e == nil {
		return fmt.Errorf("missing localization evidence")
	}
	researchContext := localizationResearchContext(taskText, e)
	decision := strings.ToLower(strings.TrimSpace(e.ResearchDecision))
	successful, attempted := false, false
	relevantSearches := make([]CodingSubAgentSearchResult, 0, len(searches))
	for _, search := range searches {
		if strings.EqualFold(strings.TrimSpace(search.Tool), "web_search") {
			if !localizationResearchQueryRelevant(researchContext, search.Query) {
				continue
			}
			attempted = true
			relevantSearches = append(relevantSearches, search)
			if search.Succeeded && !codingWebResearchResultLooksFailed(search.Summary) {
				successful = true
			}
		}
	}
	if decision == "searched" && !successful {
		return fmt.Errorf("research_decision=searched requires a successful audited web_search query relevant to the reported bug")
	}
	if decision == "searched" {
		for _, source := range e.ExternalSources {
			if !codingWebResearchSourceCoversAudit(source, relevantSearches) {
				return fmt.Errorf("external source %q is not present in relevant audited web_search evidence", source)
			}
		}
		if codingTaskNeedsExternalResearch(researchContext) && !localizationResearchHasFetchedDeclaredSource(researchContext, e.ExternalSources, relevantSearches, searches) {
			return fmt.Errorf("external/version-sensitive research requires a successful audited web_fetch of at least one declared HTTP source")
		}
	}
	if decision == "unavailable" {
		if !attempted {
			return fmt.Errorf("research_decision=unavailable requires an audited web_search attempt")
		}
		if successful {
			return fmt.Errorf("research_decision=unavailable is invalid because web_search returned usable results")
		}
		if !localizationResearchUnavailableAttemptSufficient(researchContext, relevantSearches) {
			return fmt.Errorf("research_decision=unavailable requires two distinct relevant web_search attempts when the first attempt merely returns no results")
		}
	}
	if codingTaskNeedsExternalResearch(researchContext) && decision == "not_needed" {
		return fmt.Errorf("task contains unfamiliar/current/third-party signals; use web_search, or attempt it and report unavailable")
	}
	return nil
}

func localizationResearchDebugSummary(taskText string, e *CodingSubAgentLocalizationEvidence, searches []CodingSubAgentSearchResult) string {
	decision := "missing"
	sources := 0
	if e != nil {
		decision = strings.ToLower(strings.TrimSpace(e.ResearchDecision))
		sources = len(e.ExternalSources)
	}
	researchContext := localizationResearchContext(taskText, e)
	relevant, successful, failed, fetches, auditableFetches := 0, 0, 0, 0, 0
	lastSeq := uint64(0)
	for _, search := range searches {
		if search.seq > lastSeq {
			lastSeq = search.seq
		}
		switch strings.ToLower(strings.TrimSpace(search.Tool)) {
		case "web_search":
			if !localizationResearchQueryRelevant(researchContext, search.Query) {
				continue
			}
			relevant++
			if search.Succeeded && !codingWebResearchResultLooksFailed(search.Summary) {
				successful++
			} else {
				failed++
			}
		case "web_fetch":
			fetches++
			if search.Succeeded && !codingWebFetchResultLooksFailed(search.Summary) &&
				(!search.FetchAuditKnown || search.FetchRangeKnown) {
				auditableFetches++
			}
		}
	}
	return fmt.Sprintf(
		"decision=%s needs_external=%t sources=%d searches_total=%d relevant=%d successful=%d failed=%d fetches=%d auditable_fetches=%d last_seq=%d",
		decision, codingTaskNeedsExternalResearch(researchContext), sources, len(searches), relevant, successful, failed, fetches, auditableFetches, lastSeq,
	)
}

func localizationResearchToolDebugSummary(search CodingSubAgentSearchResult) string {
	tool := strings.ToLower(strings.TrimSpace(search.Tool))
	if tool == "web_search" {
		fingerprint := localizationResearchQueryFingerprint(search.Query)
		queryHash := sha256.Sum256([]byte(fingerprint))
		failedLooking := codingWebResearchResultLooksFailed(search.Summary)
		return fmt.Sprintf("tool=web_search seq=%d success=%t failed_looking=%t failure_kind=%s query_terms=%d query_hash=%x result_runes=%d",
			search.seq, search.Succeeded, failedLooking, localizationResearchSearchDebugFailureKind(search, failedLooking),
			len(strings.Fields(fingerprint)), queryHash[:8], utf8.RuneCountInString(search.Summary))
	}
	if tool == "web_fetch" {
		host := ""
		if parsed, ok := localizationResearchParsedHTTPURL(search.FetchResolvedURL); ok {
			host = strings.ToLower(parsed.Hostname())
		}
		failedLooking := codingWebFetchResultLooksFailed(search.Summary)
		return fmt.Sprintf("tool=web_fetch seq=%d success=%t failed_looking=%t failure_kind=%s host=%q audit_known=%t range_known=%t range=%d-%d/%d has_more=%t result_runes=%d",
			search.seq, search.Succeeded, failedLooking, localizationResearchFetchDebugFailureKind(search, failedLooking), host,
			search.FetchAuditKnown, search.FetchRangeKnown, search.FetchOffset, search.FetchNextOffset,
			search.FetchTotalChars, search.FetchHasMore, utf8.RuneCountInString(search.Summary))
	}
	return fmt.Sprintf("tool=%s seq=%d success=%t", tool, search.seq, search.Succeeded)
}

func localizationResearchSearchDebugFailureKind(search CodingSubAgentSearchResult, failedLooking bool) string {
	if search.Succeeded && !failedLooking {
		return "none"
	}
	if localizationResearchSearchNoResults(search.Summary) {
		return "no_results"
	}
	if localizationResearchSearchTerminalFailure(search.Summary) {
		return "unavailable"
	}
	if !search.Succeeded {
		return "tool_failure"
	}
	return "ambiguous"
}

func localizationResearchFetchDebugFailureKind(search CodingSubAgentSearchResult, failedLooking bool) string {
	if !search.Succeeded {
		return "tool_failure"
	}
	if failedLooking {
		return "invalid_content"
	}
	if search.FetchAuditKnown && !search.FetchRangeKnown {
		return "missing_range_audit"
	}
	return "none"
}

func remoteLocalizationLogProject(c *remoteCodingCallbacks) string {
	if c == nil || c.agent == nil {
		return ""
	}
	return compactCodingSubAgentLogText(c.agent.projectDir, 300)
}

func localizationResearchUnavailableAttemptSufficient(researchContext string, searches []CodingSubAgentSearchResult) bool {
	seenQueries := make(map[string]bool)
	retryableFailures := 0
	for _, search := range searches {
		if !strings.EqualFold(strings.TrimSpace(search.Tool), "web_search") ||
			!localizationResearchQueryRelevant(researchContext, search.Query) ||
			(search.Succeeded && !codingWebResearchResultLooksFailed(search.Summary)) {
			continue
		}
		query := localizationResearchQueryFingerprint(search.Query)
		if query == "" || seenQueries[query] {
			continue
		}
		seenQueries[query] = true
		if localizationResearchSearchNoResults(search.Summary) {
			retryableFailures++
			continue
		}
		if localizationResearchSearchTerminalFailure(search.Summary) {
			// Provider/network/configuration failures are conclusive for the current
			// tool invocation; repeating the same request only wastes time.
			return true
		}
		// Empty, truncated, or otherwise ambiguous failures need a distinct retry.
		retryableFailures++
	}
	return retryableFailures >= 2
}

func localizationResearchQueryFingerprint(query string) string {
	normalized := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '.' || r == '+' || r == '#' {
			return unicode.ToLower(r)
		}
		return ' '
	}, query)
	tokens := strings.Fields(normalized)
	if len(tokens) == 0 {
		return ""
	}
	ignored := map[string]bool{
		"a": true, "an": true, "and": true, "error": true, "for": true, "in": true,
		"of": true, "on": true, "or": true, "please": true, "search": true,
		"the": true, "to": true, "with": true,
	}
	filtered := tokens[:0]
	for _, token := range tokens {
		if !ignored[token] {
			filtered = append(filtered, token)
		}
	}
	tokens = filtered
	if len(tokens) == 0 {
		return ""
	}
	sort.Strings(tokens)
	unique := tokens[:0]
	for _, token := range tokens {
		if len(unique) == 0 || unique[len(unique)-1] != token {
			unique = append(unique, token)
		}
	}
	return strings.Join(unique, " ")
}

func localizationResearchSearchNoResults(summary string) bool {
	lower := strings.ToLower(strings.TrimSpace(summary))
	for _, marker := range []string{"no results", "no relevant results", "nothing found", "未找到相关结果", "没有找到相关结果"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func localizationResearchSearchTerminalFailure(summary string) bool {
	lower := strings.ToLower(strings.TrimSpace(summary))
	if lower == "" {
		return false
	}
	for _, marker := range []string{
		"unavailable", "provider not configured", "no search provider", "invalid api key",
		"unauthorized", "forbidden", "timed out", "timeout", "network error", "connection refused",
		"搜索不可用", "未配置", "无可用搜索", "鉴权失败", "认证失败", "超时", "网络错误", "连接被拒绝",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func localizationResearchHasFetchedDeclaredSource(researchContext string, sources []string, relevantSearches, searches []CodingSubAgentSearchResult) bool {
	declared := make([]string, 0, len(sources))
	for _, source := range sources {
		if localizationResearchSourceIsHTTPURL(source) {
			declared = append(declared, strings.TrimSpace(source))
		}
	}
	for _, source := range declared {
		coveringSearches := make([]CodingSubAgentSearchResult, 0, len(relevantSearches))
		for _, search := range relevantSearches {
			if !strings.EqualFold(strings.TrimSpace(search.Tool), "web_search") ||
				!search.Succeeded || codingWebResearchResultLooksFailed(search.Summary) ||
				!localizationAuditContainsSource(localizationWebSearchResultBody(search.Summary), source) {
				continue
			}
			coveringSearches = append(coveringSearches, search)
		}
		if len(coveringSearches) == 0 {
			continue
		}
		for _, discovery := range coveringSearches {
			var fetches []CodingSubAgentSearchResult
			for _, fetch := range searches {
				if !strings.EqualFold(strings.TrimSpace(fetch.Tool), "web_fetch") ||
					!fetch.Succeeded || codingWebFetchResultLooksFailed(fetch.Summary) ||
					!sameLocalizationResearchURL(fetch.Query, source) {
					continue
				}
				// seq=0 is retained for legacy/unsequenced audit records and unit
				// fixtures. Live local and remote callbacks always assign a sequence.
				if discovery.seq != 0 && fetch.seq != 0 && fetch.seq <= discovery.seq {
					continue
				}
				// Live callbacks mark every web_fetch audit. A live result without the
				// stable read-range metadata is a download/save result or malformed
				// output, not proof that the page body was inspected. Legacy fixtures
				// remain compatible because their audit provenance is unknown.
				if fetch.FetchAuditKnown && !fetch.FetchRangeKnown {
					continue
				}
				if fetch.FetchAuditKnown &&
					!sameLocalizationResearchURL(fetch.FetchResolvedURL, source) &&
					!sameLocalizationResearchOrigin(fetch.FetchResolvedURL, source) {
					continue
				}
				body := localizationWebFetchResultBody(fetch.Summary)
				// Do not force pagination when one page already proves every required
				// precision token. This keeps research fast for large documents.
				if localizationResearchFetchedBodyRelevant(researchContext, body) {
					return true
				}
				fetches = append(fetches, fetch)
			}
			if localizationResearchContinuousFetchesRelevant(researchContext, fetches) {
				return true
			}
		}
	}
	return false
}

var (
	localizationWebFetchRangeRE      = regexp.MustCompile(`(?m)^已读取:\s*(\d+)\s*-\s*(\d+)\s*/\s*(\d+)\s*字符\s*$`)
	localizationWebFetchPaginationRE = regexp.MustCompile(`(?im)^truncated:\s*(true|false)\s*\|\s*has_more:\s*(true|false)\s*\|\s*next_offset:\s*(\d+)\s*$`)
)

func localizationWebFetchPagination(tool string, args map[string]interface{}, result string) (offset, nextOffset, totalChars int, hasMore, known bool) {
	if !strings.EqualFold(strings.TrimSpace(tool), "web_fetch") {
		return 0, 0, 0, false, false
	}
	requestedOffset := localizationIntArg(args, "offset", 0)
	header := localizationWebFetchHeader(result)
	rangeMatch := localizationWebFetchRangeRE.FindStringSubmatch(header)
	pageMatch := localizationWebFetchPaginationRE.FindStringSubmatch(header)
	if len(rangeMatch) != 4 || len(pageMatch) != 4 {
		return requestedOffset, 0, 0, false, false
	}
	start, startErr := strconv.Atoi(rangeMatch[1])
	end, endErr := strconv.Atoi(rangeMatch[2])
	total, totalErr := strconv.Atoi(rangeMatch[3])
	nextOffset, nextErr := strconv.Atoi(pageMatch[3])
	truncated := strings.EqualFold(pageMatch[1], "true")
	hasMore = strings.EqualFold(pageMatch[2], "true")
	if startErr != nil || endErr != nil || totalErr != nil || nextErr != nil ||
		start < 0 || end < start || total < end || nextOffset != end ||
		truncated != hasMore ||
		(hasMore && end >= total) || (!hasMore && end != total) {
		return requestedOffset, 0, 0, false, false
	}
	// The returned range is authoritative. This also handles string-valued
	// offsets accepted by the host's permissive argument decoder without letting
	// an audit-side type mismatch relabel a later page as offset zero.
	return start, nextOffset, total, hasMore, true
}

func localizationWebFetchHeader(result string) string {
	normalized := strings.ReplaceAll(result, "\r\n", "\n")
	if split := strings.Index(normalized, "\n\n"); split >= 0 {
		return normalized[:split]
	}
	return normalized
}

func localizationWebFetchResolvedURL(result string) string {
	for _, line := range strings.Split(localizationWebFetchHeader(result), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(strings.ToLower(trimmed), "url:") {
			return strings.TrimSpace(trimmed[len("url:"):])
		}
	}
	return ""
}

func localizationIntArg(args map[string]interface{}, key string, fallback int) int {
	if args == nil {
		return fallback
	}
	switch value := args[key].(type) {
	case int:
		if value >= 0 {
			return value
		}
	case int64:
		if value >= 0 && value <= math.MaxInt {
			return int(value)
		}
	case float64:
		if value >= 0 && value <= math.MaxInt && value == math.Trunc(value) {
			return int(value)
		}
	case json.Number:
		if parsed, err := strconv.Atoi(string(value)); err == nil && parsed >= 0 {
			return parsed
		}
	case string:
		if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil && parsed >= 0 {
			return parsed
		}
	}
	return fallback
}

// localizationResearchContinuousFetchesRelevant combines only a continuous,
// non-overlapping chain beginning at offset zero. Duplicate offsets are ignored
// after the first observation, so repeated first-page reads cannot manufacture
// evidence. Reading to EOF is unnecessary once the accumulated chain proves all
// required tokens.
func localizationResearchContinuousFetchesRelevant(researchContext string, fetches []CodingSubAgentSearchResult) bool {
	known := make([]CodingSubAgentSearchResult, 0, len(fetches))
	for _, fetch := range fetches {
		if fetch.FetchRangeKnown {
			known = append(known, fetch)
		}
	}
	sort.SliceStable(known, func(i, j int) bool {
		if known[i].FetchOffset == known[j].FetchOffset {
			return known[i].seq < known[j].seq
		}
		return known[i].FetchOffset < known[j].FetchOffset
	})
	expected := 0
	expectedTotal := -1
	seenOffsets := make(map[int]bool, len(known))
	var bodies []string
	for _, fetch := range known {
		if seenOffsets[fetch.FetchOffset] {
			continue
		}
		seenOffsets[fetch.FetchOffset] = true
		if fetch.FetchOffset != expected {
			break
		}
		if expectedTotal >= 0 && fetch.FetchTotalChars != expectedTotal {
			break
		}
		expectedTotal = fetch.FetchTotalChars
		bodies = append(bodies, localizationWebFetchResultBody(fetch.Summary))
		expected = fetch.FetchNextOffset
		if localizationResearchFetchedBodyRelevant(researchContext, strings.Join(bodies, "\n")) {
			return true
		}
		if !fetch.FetchHasMore {
			break
		}
	}
	return false
}

func localizationWebFetchResultBody(summary string) string {
	normalized := strings.ReplaceAll(summary, "\r\n", "\n")
	lines := strings.Split(normalized, "\n")
	sawFetchMetadata := false
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		lower := strings.ToLower(trimmed)
		if strings.HasPrefix(trimmed, "标题:") || strings.HasPrefix(trimmed, "URL:") ||
			strings.HasPrefix(trimmed, "类型:") || strings.HasPrefix(trimmed, "已读取:") ||
			strings.HasPrefix(lower, "truncated:") {
			sawFetchMetadata = true
			continue
		}
		if sawFetchMetadata && trimmed == "" {
			return strings.Join(lines[i+1:], "\n")
		}
		if !sawFetchMetadata && trimmed != "" {
			return summary
		}
	}
	if sawFetchMetadata {
		return ""
	}
	return summary
}

func localizationResearchFetchedBodyRelevant(researchContext, summary string) bool {
	contextTokens := localizationResearchTokenRE.FindAllString(strings.ToLower(researchContext), -1)
	summaryTokens := localizationResearchTokenRE.FindAllString(strings.ToLower(summary), -1)
	if len(contextTokens) == 0 || len(summaryTokens) == 0 {
		return false
	}
	// Precise versions and diagnostics are the strongest evidence that the page
	// actually addresses the concrete failure, rather than merely being a fetched
	// URL with generic boilerplate.
	precision := localizationResearchRequiredPrecisionTokens(contextTokens)
	if len(precision) > 0 {
		for _, token := range precision {
			if !localizationResearchTokensContain(summaryTokens, token) {
				return false
			}
		}
		return true
	}
	generic := map[string]bool{
		"api": true, "bug": true, "client": true, "compatibility": true,
		"dependency": true, "docs": true, "documentation": true, "error": true,
		"failure": true, "fix": true, "guide": true, "official": true,
		"request": true, "response": true, "sdk": true, "version": true,
	}
	for _, token := range contextTokens {
		if !localizationResearchIdentityToken(token, generic) {
			continue
		}
		if localizationResearchTokensContain(summaryTokens, token) {
			return true
		}
	}
	return false
}

func sameLocalizationResearchURL(a, b string) bool {
	normalize := func(raw string) string {
		parsed, ok := localizationResearchParsedHTTPURL(raw)
		if !ok || parsed.User != nil {
			return ""
		}
		host := strings.ToLower(parsed.Hostname())
		port := parsed.Port()
		if port == "" || parsed.Scheme == "https" && port == "443" || parsed.Scheme == "http" && port == "80" {
			parsed.Host = host
		} else {
			parsed.Host = host + ":" + port
		}
		parsed.Fragment = ""
		if parsed.Path != "/" {
			parsed.Path = strings.TrimSuffix(parsed.Path, "/")
		}
		return parsed.String()
	}
	left, right := normalize(a), normalize(b)
	return left != "" && left == right
}

func sameLocalizationResearchOrigin(a, b string) bool {
	type origin struct {
		scheme string
		host   string
		port   string
	}
	parse := func(raw string) (origin, bool) {
		parsed, ok := localizationResearchParsedHTTPURL(raw)
		if !ok || parsed.User != nil {
			return origin{}, false
		}
		scheme := parsed.Scheme
		host := strings.ToLower(parsed.Hostname())
		port := parsed.Port()
		if port == "" {
			if scheme == "https" {
				port = "443"
			} else {
				port = "80"
			}
		}
		return origin{scheme: scheme, host: host, port: port}, host != ""
	}
	left, leftOK := parse(a)
	right, rightOK := parse(b)
	if !leftOK || !rightOK || left.host != right.host {
		return false
	}
	if left.scheme == right.scheme {
		return left.port == right.port
	}
	return left.scheme == "https" && right.scheme == "http" && left.port == "443" && right.port == "80"
}

func localizationResearchContext(taskText string, e *CodingSubAgentLocalizationEvidence) string {
	if e == nil {
		return taskText
	}
	parts := []string{
		taskText,
		e.Reproduction,
		e.Signal.ExpectedBehavior,
		e.Signal.ActualBehavior,
		e.Signal.Reproduction,
	}
	parts = append(parts, e.Signal.ErrorStrings...)
	parts = append(parts, e.Signal.StackFrames...)
	parts = append(parts, e.Signal.EntryPoints...)
	parts = append(parts, e.CausalPath...)
	parts = append(parts, e.SupportingEvidence...)
	return strings.Join(parts, "\n")
}

var localizationResearchTokenRE = regexp.MustCompile(`[a-z0-9][a-z0-9._+/#-]*`)

func localizationResearchQueryRelevant(taskText, query string) bool {
	taskText = strings.ToLower(strings.TrimSpace(taskText))
	query = strings.ToLower(strings.TrimSpace(query))
	if taskText == "" || query == "" {
		return false
	}
	// Product names, package names, versions, error codes, and API identifiers
	// survive this tokenization. Generic debugging words do not count: otherwise
	// a query such as "how to fix error" could satisfy every task.
	generic := map[string]bool{
		"a": true, "after": true, "an": true, "and": true, "api": true, "bug": true,
		"compatibility": true, "crash": true, "dependency": true, "driver": true,
		"error": true, "exception": true, "fail": true, "failed": true, "failing": true,
		"failure": true, "fix": true, "for": true, "framework": true, "guide": true,
		"how": true, "in": true, "issue": true, "latest": true, "library": true,
		"module": true, "of": true, "on": true, "package": true, "problem": true,
		"party": true, "protocol": true, "sdk": true, "the": true, "third": true,
		"third-party": true, "to": true,
		"unknown": true, "upgrade": true, "upgraded": true, "vendor": true,
		"version": true, "with": true,
	}
	rawTaskTokens := localizationResearchTokenRE.FindAllString(taskText, -1)
	rawQueryTokens := localizationResearchTokenRE.FindAllString(query, -1)
	// When localization has already surfaced a precise version or diagnostic
	// code, retain it in the query. A component-name-only search is too broad to
	// verify the concrete failure the agent is about to fix.
	for _, required := range localizationResearchRequiredPrecisionTokens(rawTaskTokens) {
		if !localizationResearchTokensContain(rawQueryTokens, required) {
			return false
		}
	}
	taskTokens := map[string]bool{}
	for _, token := range rawTaskTokens {
		if localizationResearchIdentityToken(token, generic) {
			taskTokens[token] = true
		}
	}
	for _, token := range rawQueryTokens {
		if taskTokens[token] {
			return true
		}
	}
	// A fully non-ASCII report may legitimately be searched using an English
	// translation. Require a substantive query, while tasks containing product
	// identifiers must retain at least one of them in the search.
	if len(taskTokens) == 0 {
		if len(rawTaskTokens) > 0 {
			taskDomains := localizationResearchDomainTokens(rawTaskTokens)
			for domain := range localizationResearchDomainTokens(rawQueryTokens) {
				if taskDomains[domain] {
					return true
				}
			}
			return false
		}
		for _, token := range rawQueryTokens {
			if len(token) >= 3 && !generic[token] {
				return true
			}
		}
		return len(localizationResearchDomainTokens(rawQueryTokens)) > 0
	}
	return false
}

func localizationResearchRequiredPrecisionTokens(tokens []string) []string {
	// Reproduction logs may contain dates, hashes and incidental identifiers.
	// Bound mandatory precision so the resulting search remains practical.
	versions := make([]string, 0, 2)
	canonicalDiagnostics := make([]string, 0, 2)
	vendorDiagnostics := make([]string, 0, 2)
	seen := map[string]bool{}
	for index, token := range tokens {
		token = strings.ToLower(strings.TrimSpace(token))
		if token == "" || seen[token] {
			continue
		}
		if localizationResearchVersionToken(token) && len(versions) < 2 {
			seen[token] = true
			versions = append(versions, token)
			continue
		}
		if localizationResearchCanonicalDiagnosticToken(token) && len(canonicalDiagnostics) < 2 {
			seen[token] = true
			canonicalDiagnostics = append(canonicalDiagnostics, token)
			continue
		}
		if localizationResearchHTTPStatusToken(tokens, index) && len(canonicalDiagnostics) < 2 {
			seen[token] = true
			canonicalDiagnostics = append(canonicalDiagnostics, token)
			continue
		}
		if localizationResearchDiagnosticToken(token) && len(vendorDiagnostics) < 2 {
			seen[token] = true
			vendorDiagnostics = append(vendorDiagnostics, token)
		}
	}
	diagnostics := append(canonicalDiagnostics, vendorDiagnostics...)
	if len(diagnostics) > 2 {
		diagnostics = diagnostics[:2]
	}
	return append(versions, diagnostics...)
}

func localizationResearchHTTPStatusToken(tokens []string, index int) bool {
	if index < 0 || index >= len(tokens) {
		return false
	}
	token := strings.TrimSpace(tokens[index])
	if len(token) != 3 {
		return false
	}
	status, err := strconv.Atoi(token)
	if err != nil || status < 400 || status > 599 {
		return false
	}
	// A bare three-digit number may be a line, port fragment, or issue number.
	// Require nearby protocol/status language before treating it as an exact
	// external diagnostic that every research query must retain.
	for offset := 1; offset <= 2; offset++ {
		for _, neighbor := range []int{index - offset, index + offset} {
			if neighbor < 0 || neighbor >= len(tokens) {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(tokens[neighbor])) {
			case "http", "https", "status", "statuscode", "response", "responsecode":
				return true
			}
		}
	}
	return false
}

func localizationResearchTokensContain(tokens []string, want string) bool {
	for _, token := range tokens {
		if strings.EqualFold(strings.TrimSpace(token), want) {
			return true
		}
	}
	return false
}

func localizationResearchVersionToken(token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	prefixed := strings.HasPrefix(token, "v")
	trimmed := strings.TrimPrefix(token, "v")
	if trimmed == "" || !prefixed && !strings.Contains(trimmed, ".") {
		return false
	}
	for _, r := range trimmed {
		if (r < '0' || r > '9') && r != '.' && r != '-' && r != '_' {
			return false
		}
	}
	parts := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '.' || r == '-' || r == '_' })
	if len(parts) == 0 || len(parts) > 4 {
		return false
	}
	if !prefixed && len(parts) >= 3 && len(parts[0]) == 4 {
		if year, err := strconv.Atoi(parts[0]); err == nil && year >= 1900 && year <= 2100 {
			return false
		}
	}
	return true
}

func localizationResearchDiagnosticToken(token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if len(token) < 4 || len(token) > 32 || localizationResearchVersionToken(token) {
		return false
	}
	if localizationResearchCanonicalDiagnosticToken(token) {
		return true
	}
	letters, digits := 0, 0
	for _, r := range token {
		if r >= 'a' && r <= 'z' {
			letters++
		} else if r >= '0' && r <= '9' {
			digits++
		} else {
			return false
		}
	}
	return letters >= 1 && letters <= 3 && digits >= 3 && digits <= 6
}

func localizationResearchCanonicalDiagnosticToken(token string) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	for _, prefix := range []string{"err_", "error_", "cve-"} {
		if strings.HasPrefix(token, prefix) {
			return true
		}
	}
	return false
}

func localizationResearchDomainTokens(tokens []string) map[string]bool {
	out := map[string]bool{}
	for _, token := range tokens {
		switch strings.ToLower(strings.TrimSpace(token)) {
		case "api", "compatibility", "dependency", "driver", "framework", "library", "module", "package", "protocol", "sdk", "vendor", "version":
			out[strings.ToLower(strings.TrimSpace(token))] = true
		}
	}
	return out
}

func localizationResearchIdentityToken(token string, generic map[string]bool) bool {
	token = strings.ToLower(strings.TrimSpace(token))
	if len(token) < 3 || generic[token] {
		return false
	}
	// A version number is valuable search context but not an identity. Matching
	// only "v2" or "4.2" can connect an unrelated result to any versioned bug.
	trimmed := strings.TrimPrefix(token, "v")
	if trimmed != "" {
		versionLike := true
		for _, r := range trimmed {
			if (r < '0' || r > '9') && r != '.' && r != '-' && r != '_' {
				versionLike = false
				break
			}
		}
		if versionLike {
			return false
		}
	}
	return true
}

func localizationEvidenceCoversPath(e *CodingSubAgentLocalizationEvidence, path string) bool {
	if e == nil {
		return false
	}
	if sameLocalizationPath(e.RootCauseFile, path) {
		return true
	}
	for _, candidate := range e.Candidates {
		if sameLocalizationPath(candidate.File, path) && localizationCandidateAuthorizesEdit(candidate) {
			return true
		}
	}
	// Focused regression tests are legitimate secondary edits when explicitly
	// named in the evidence report.
	for _, test := range e.FocusedTests {
		if localizationPathLooksLikeTestSource(path) && localizationFocusedTestNamesPath(test, path) {
			return true
		}
	}
	return false
}

func localizationCandidateAuthorizesEdit(candidate CodingSubAgentLocalizationCandidate) bool {
	candidate = normalizeLocalizationCandidate(candidate)
	return candidate.Score > 0 && !math.IsNaN(candidate.Score) && !math.IsInf(candidate.Score, 0) && len(candidate.SupportingEvidence) > 0
}

func localizationPathLooksLikeTestSource(path string) bool {
	path = strings.ToLower(filepath.ToSlash(strings.TrimSpace(path)))
	if path == "" {
		return false
	}
	base := filepath.Base(path)
	return strings.Contains(path, "/test/") || strings.Contains(path, "/tests/") ||
		strings.Contains(path, "/__tests__/") || strings.HasPrefix(path, "test/") ||
		strings.HasPrefix(path, "tests/") || strings.HasPrefix(base, "test_") ||
		strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, "_test.py") ||
		strings.Contains(base, ".test.") || strings.Contains(base, ".spec.")
}

func localizationFocusedTestNamesPath(test, path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	for _, token := range strings.Fields(test) {
		token = strings.Trim(token, "\"'`[](){},;:")
		if token != "" && sameLocalizationPath(token, path) {
			return true
		}
	}
	return false
}

func summarizeLocalizationQuality(taskText string, existingFilesModified []string, e *CodingSubAgentLocalizationEvidence, searches []CodingSubAgentSearchResult) string {
	if len(existingFilesModified) == 0 || !codingTaskNeedsLocalization(taskText) {
		return ""
	}
	if err := validateLocalizationEvidence(e, ""); err != nil {
		return "bug localization quality gate failed: " + err.Error()
	}
	if err := validateLocalizationResearchEvidence(taskText, e, searches); err != nil {
		return "bug localization research gate failed: " + err.Error()
	}
	rootCauseModified := false
	for _, file := range existingFilesModified {
		if sameLocalizationPath(e.RootCauseFile, file) {
			rootCauseModified = true
			continue
		}
		if !localizationEvidenceCoversPath(e, file) {
			return fmt.Sprintf("bug localization quality gate failed: modified existing file %q is not covered by root cause, supported candidates, or an explicitly named focused test", file)
		}
	}
	if rootCauseModified {
		return ""
	}
	return fmt.Sprintf("bug localization quality gate failed: root cause %q does not match any modified existing file", e.RootCauseFile)
}

func sameLocalizationPath(a, b string) bool {
	rawA, rawB := strings.TrimSpace(a), strings.TrimSpace(b)
	a = filepath.ToSlash(filepath.Clean(rawA))
	b = filepath.ToSlash(filepath.Clean(rawB))
	windowsPath := localizationPathIsWindowsAbs(rawA) || localizationPathIsWindowsAbs(rawB)
	equal := func(left, right string) bool {
		if windowsPath {
			return strings.EqualFold(left, right)
		}
		return left == right
	}
	hasSuffix := func(value, suffix string) bool {
		if windowsPath {
			return strings.HasSuffix(strings.ToLower(value), strings.ToLower(suffix))
		}
		return strings.HasSuffix(value, suffix)
	}
	if equal(a, b) {
		return true
	}
	// Suffix matching is only for an absolute-vs-relative comparison. Treating
	// two different relative paths (or a bare basename and a nested path) as the
	// same could authorize an edit to the wrong same-named file.
	absA, absB := localizationPathIsAbs(rawA), localizationPathIsAbs(rawB)
	if absA == absB {
		return false
	}
	if absA {
		return hasSuffix(a, "/"+b)
	}
	return hasSuffix(b, "/"+a)
}

func localizationPathIsAbs(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	// Remote paths are POSIX paths even when the coordinator runs on Windows,
	// where filepath.IsAbs("/repo/file.go") is false.
	return filepath.IsAbs(path) || strings.HasPrefix(filepath.ToSlash(path), "/")
}

func localizationPathIsWindowsAbs(path string) bool {
	path = strings.TrimSpace(filepath.ToSlash(path))
	driveAbs := len(path) >= 3 && ((path[0] >= 'A' && path[0] <= 'Z') || (path[0] >= 'a' && path[0] <= 'z')) && path[1] == ':' && path[2] == '/'
	return driveAbs || strings.HasPrefix(path, "//")
}

func buildCodeNavigationToolDefinition() map[string]interface{} {
	return buildRemoteToolDef(codeNavigationToolName,
		"Explore source structure with codegraph. Use node for one symbol, file_symbols for a file, callers or callees for call edges, and explore for an area overview. A returned hit is evidence. A different symbol, file, or call edge is a new code_navigation call. A text search that already returned lines also counts. If the codegraph command is missing, tries npm install -g @colbymchenry/codegraph once; if the project has no .codegraph/ index, tries codegraph init.",
		map[string]interface{}{
			"operation": map[string]interface{}{"type": "string", "enum": []string{"explore", "node", "workspace_symbol", "definition", "references", "implementation", "callers", "callees", "file_symbols"}},
			"query":     map[string]interface{}{"type": "string", "description": "Symbol, file, or code question"},
			"file":      map[string]interface{}{"type": "string", "description": "Optional context file"},
			"line":      map[string]interface{}{"type": "number", "description": "Optional 1-based line"},
			"column":    map[string]interface{}{"type": "number", "description": "Optional 1-based column"},
			"depth":     map[string]interface{}{"type": "number", "description": "Traversal depth, default 2, max 5"},
		}, []string{"operation", "query"})
}

func buildReportLocalizationToolDefinition() map[string]interface{} {
	return buildRemoteToolDef(reportLocalizationToolName,
		"Submit structured root-cause evidence before editing an existing file for a bug fix. Do not call this for exploration or new-feature work. The report becomes an enforced audit gate.",
		map[string]interface{}{
			"root_cause_file":     map[string]interface{}{"type": "string", "minLength": 1},
			"root_cause_symbol":   map[string]interface{}{"type": "string"},
			"causal_path":         map[string]interface{}{"type": "array", "minItems": 1, "items": map[string]interface{}{"type": "string", "minLength": 1}},
			"reproduction":        map[string]interface{}{"type": "string", "minLength": 1},
			"supporting_evidence": map[string]interface{}{"type": "array", "minItems": 1, "items": map[string]interface{}{"type": "string", "minLength": 1}},
			"rejected_hypotheses": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"focused_tests":       map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
			"research_decision":   map[string]interface{}{"type": "string", "enum": []string{"searched", "not_needed", "unavailable"}, "description": "searched after web_search; not_needed only for repository-internal facts; unavailable only after a failed search attempt"},
			"research_reason":     map[string]interface{}{"type": "string", "minLength": 1, "description": "Why external research was needed or safely unnecessary/unavailable"},
			"external_sources":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": "URLs/titles actually found; required for searched"},
			"confidence":          map[string]interface{}{"type": "number", "exclusiveMinimum": 0, "maximum": 1},
			"candidates": map[string]interface{}{"type": "array", "items": map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"file":                   map[string]interface{}{"type": "string", "minLength": 1},
					"symbol":                 map[string]interface{}{"type": "string"},
					"score":                  map[string]interface{}{"type": "number"},
					"supporting_evidence":    map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"contradicting_evidence": map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}},
					"next_probe":             map[string]interface{}{"type": "string"},
				},
				"required": []string{"file", "score"},
			}},
		}, []string{"root_cause_file", "causal_path", "reproduction", "supporting_evidence", "research_decision", "research_reason", "confidence"})
}

func localizationEvidenceFromArgs(args map[string]interface{}, signal CodingSubAgentBugSignal) (CodingSubAgentLocalizationEvidence, error) {
	raw, err := json.Marshal(args)
	if err != nil {
		return CodingSubAgentLocalizationEvidence{}, err
	}
	var e CodingSubAgentLocalizationEvidence
	if err := json.Unmarshal(raw, &e); err != nil {
		return e, err
	}
	e.Signal = signal
	e = normalizeLocalizationEvidence(e)
	if err := validateLocalizationEvidence(&e, ""); err != nil {
		return e, err
	}
	return e, nil
}

func (c *codingSubAgentCallbacks) executeReportLocalization(args map[string]interface{}) codingToolExecutionResult {
	if c == nil {
		return codingToolExecutionResult{Text: "localization state unavailable", Outcome: codingToolOutcomeFailed}
	}
	if !c.codingQualityGateOn() {
		log.Printf("[coding-localization] report skipped stage=quality_gate_off task=%d", taskDisplayNumber(c.task))
		return codingToolExecutionResult{Text: localizationReportOptionalMessage, Outcome: codingToolOutcomeSuccess}
	}
	text := ""
	if c.task != nil {
		text = c.task.Title + "\n" + c.task.Description
	}
	if strings.TrimSpace(text) != "" && !codingTaskNeedsLocalization(text) {
		log.Printf("[coding-localization] report skipped stage=not_bug task=%d", taskDisplayNumber(c.task))
		return codingToolExecutionResult{Text: localizationReportNotRequiredMessage, Outcome: codingToolOutcomeSuccess}
	}
	e, err := localizationEvidenceFromArgs(args, extractCodingSubAgentBugSignal(text))
	if err != nil {
		log.Printf("[coding-localization] report rejected stage=evidence task=%d error=%q", taskDisplayNumber(c.task), compactCodingSubAgentLogText(err.Error(), 500))
		return codingToolExecutionResult{Text: localizationInvalidEvidenceMessage(err), Outcome: codingToolOutcomeFailed}
	}
	searches := c.getSearchesRun()
	if err := validateLocalizationResearchEvidence(text, &e, searches); err != nil {
		log.Printf("[coding-localization] report rejected stage=research task=%d root=%q confidence=%.2f %s error=%q",
			taskDisplayNumber(c.task), compactCodingSubAgentLogText(e.RootCauseFile, 240), e.Confidence,
			localizationResearchDebugSummary(text, &e, searches), compactCodingSubAgentLogText(err.Error(), 500))
		return codingToolExecutionResult{Text: "invalid localization research evidence: " + err.Error(), Outcome: codingToolOutcomeFailed}
	}
	revision := c.storeLocalizationForCurrentControlPlaneRevision(e)
	log.Printf("[coding-localization] report accepted task=%d root=%q symbol=%q candidates=%d confidence=%.2f %s",
		taskDisplayNumber(c.task), compactCodingSubAgentLogText(e.RootCauseFile, 240), compactCodingSubAgentLogText(e.RootCauseSymbol, 160),
		len(e.Candidates), e.Confidence, localizationResearchDebugSummary(text, &e, searches))
	accepted := c.localization.snapshotForRevision(revision)
	out, _ := json.MarshalIndent(accepted, "", "  ")
	return codingToolExecutionResult{Text: localizationEvidenceAcceptedText(revision, string(out)), Outcome: codingToolOutcomeSuccess}
}

func (c *codingSubAgentCallbacks) requireLocalizationBeforeExistingBugEdit(path string, created bool) string {
	if c == nil || created || (c.subagent != nil && c.subagent.horizonPosture) || !c.codingQualityGateOn() || c.task == nil || !codingTaskNeedsLocalization(c.task.Title+"\n"+c.task.Description) {
		return ""
	}
	e := c.localizationForCurrentControlPlaneRevision()
	if e == nil {
		detail := localizationStaleControlPlaneEvidenceDetail(&c.localization, c.currentControlPlaneRevision())
		log.Printf("[coding-localization] edit blocked stage=evidence task=%d path=%q error=%q",
			taskDisplayNumber(c.task), compactCodingSubAgentLogText(c.displayProjectPath(path), 300), compactCodingSubAgentLogText(detail, 500))
		return localizationBugFixEditBlocked(false, detail)
	}
	if err := validateLocalizationEvidence(e, c.displayProjectPath(path)); err != nil {
		log.Printf("[coding-localization] edit blocked stage=evidence task=%d path=%q error=%q",
			taskDisplayNumber(c.task), compactCodingSubAgentLogText(c.displayProjectPath(path), 300), compactCodingSubAgentLogText(err.Error(), 500))
		return localizationBugFixEditBlocked(false, err.Error())
	}
	searches := c.getSearchesRun()
	if err := validateLocalizationResearchEvidence(c.task.Title+"\n"+c.task.Description, e, searches); err != nil {
		log.Printf("[coding-localization] edit blocked stage=research task=%d path=%q %s error=%q",
			taskDisplayNumber(c.task), compactCodingSubAgentLogText(c.displayProjectPath(path), 300),
			localizationResearchDebugSummary(c.task.Title+"\n"+c.task.Description, e, searches), compactCodingSubAgentLogText(err.Error(), 500))
		return "bug-fix edit blocked: localization research evidence is no longer valid: " + err.Error()
	}
	log.Printf("[coding-localization] edit authorized task=%d path=%q root=%q",
		taskDisplayNumber(c.task), compactCodingSubAgentLogText(c.displayProjectPath(path), 300), compactCodingSubAgentLogText(e.RootCauseFile, 240))
	return ""
}

func (c *remoteCodingCallbacks) executeRemoteReportLocalization(args map[string]interface{}) string {
	if c == nil {
		return "localization state unavailable"
	}
	if !c.remoteQualityGateEnabled() {
		log.Printf("[remote-localization] report skipped stage=quality_gate_off project=%q", remoteLocalizationLogProject(c))
		return localizationReportOptionalMessage
	}
	taskText := c.task + "\n" + c.taskContext
	if strings.TrimSpace(taskText) != "" && !codingTaskNeedsLocalization(taskText) {
		log.Printf("[remote-localization] report skipped stage=not_bug project=%q", remoteLocalizationLogProject(c))
		return localizationReportNotRequiredMessage
	}
	e, err := localizationEvidenceFromArgs(args, extractCodingSubAgentBugSignal(taskText))
	if err != nil {
		log.Printf("[remote-localization] report rejected stage=evidence project=%q error=%q",
			remoteLocalizationLogProject(c), compactCodingSubAgentLogText(err.Error(), 500))
		return localizationInvalidEvidenceMessage(err)
	}
	if err := validateLocalizationResearchEvidence(c.task+"\n"+c.taskContext, &e, c.searchesRun); err != nil {
		log.Printf("[remote-localization] report rejected stage=research project=%q root=%q confidence=%.2f %s error=%q",
			remoteLocalizationLogProject(c), compactCodingSubAgentLogText(e.RootCauseFile, 240), e.Confidence,
			localizationResearchDebugSummary(c.task+"\n"+c.taskContext, &e, c.searchesRun), compactCodingSubAgentLogText(err.Error(), 500))
		return "invalid localization research evidence: " + err.Error()
	}
	revision := c.storeLocalizationForCurrentControlPlaneRevision(e)
	log.Printf("[remote-localization] report accepted project=%q root=%q symbol=%q candidates=%d confidence=%.2f %s",
		remoteLocalizationLogProject(c), compactCodingSubAgentLogText(e.RootCauseFile, 240),
		compactCodingSubAgentLogText(e.RootCauseSymbol, 160), len(e.Candidates), e.Confidence,
		localizationResearchDebugSummary(c.task+"\n"+c.taskContext, &e, c.searchesRun))
	accepted := c.localization.snapshotForRevision(revision)
	out, _ := json.MarshalIndent(accepted, "", "  ")
	return localizationEvidenceAcceptedText(revision, string(out))
}

func (c *remoteCodingCallbacks) requireRemoteLocalizationBeforeBugEdit(args map[string]interface{}, definitelyExisting bool) string {
	if c == nil || !c.remoteQualityGateEnabled() || !codingTaskNeedsLocalization(c.task+"\n"+c.taskContext) {
		return ""
	}
	path := remoteArgStr(args, "path")
	if path == "" {
		return ""
	}
	// ssh_edit_file always targets an existing file. ssh_write_file may create a
	// new file, but when localization already points elsewhere it must not bypass
	// the root-cause gate by rewriting that existing target wholesale.
	e := c.localizationForCurrentControlPlaneRevision()
	if !definitelyExisting {
		resolved := remoteCleanPath(c.resolvePath(path))
		exists, tracked := c.knownExisting[resolved]
		if !tracked {
			log.Printf("[remote-localization] edit blocked stage=existence project=%q path=%q error=%q",
				remoteLocalizationLogProject(c), compactCodingSubAgentLogText(path, 300), "target existence is unknown")
			return "bug-fix write blocked: use ssh_read_file/code_navigation to determine whether the target exists, then submit report_localization before editing existing code"
		}
		if !exists {
			log.Printf("[remote-localization] edit exempt reason=new_file project=%q path=%q",
				remoteLocalizationLogProject(c), compactCodingSubAgentLogText(path, 300))
			return ""
		}
	}
	if e == nil {
		detail := localizationStaleControlPlaneEvidenceDetail(&c.localization, c.currentControlPlaneRevision())
		log.Printf("[remote-localization] edit blocked stage=evidence project=%q path=%q error=%q",
			remoteLocalizationLogProject(c), compactCodingSubAgentLogText(path, 300), compactCodingSubAgentLogText(detail, 500))
		return localizationBugFixEditBlocked(true, detail)
	}
	if err := validateLocalizationEvidence(e, path); err != nil {
		log.Printf("[remote-localization] edit blocked stage=evidence project=%q path=%q error=%q",
			remoteLocalizationLogProject(c), compactCodingSubAgentLogText(path, 300), compactCodingSubAgentLogText(err.Error(), 500))
		return localizationBugFixEditBlocked(true, err.Error())
	}
	if err := validateLocalizationResearchEvidence(c.task+"\n"+c.taskContext, e, c.searchesRun); err != nil {
		log.Printf("[remote-localization] edit blocked stage=research project=%q path=%q %s error=%q",
			remoteLocalizationLogProject(c), compactCodingSubAgentLogText(path, 300),
			localizationResearchDebugSummary(c.task+"\n"+c.taskContext, e, c.searchesRun), compactCodingSubAgentLogText(err.Error(), 500))
		return "bug-fix edit blocked: localization research evidence is no longer valid: " + err.Error()
	}
	log.Printf("[remote-localization] edit authorized project=%q path=%q root=%q",
		remoteLocalizationLogProject(c), compactCodingSubAgentLogText(path, 300), compactCodingSubAgentLogText(e.RootCauseFile, 240))
	return ""
}

type codeNavigationOutput struct {
	Backend      string `json:"backend"`
	Operation    string `json:"operation"`
	Query        string `json:"query"`
	Output       string `json:"output"`
	Truncated    bool   `json:"truncated,omitempty"`
	ContinueWith string `json:"continue_with,omitempty"`
}

// codegraphKeepUsingHint is attached to a real codegraph hit. The hit is
// evidence. Naming the next lookup must not retract this one: "an earlier
// miss is stale" was read as "the search that just returned did not happen."
const codegraphKeepUsingHint = "This codegraph lookup succeeded; the lines above are evidence. Do not repeat this search. The next symbol, file, caller, or callee lookup should call code_navigation (node, file_symbols, callers, or callees). read_file may use a span named in this result."

// codegraphSearchAgainHint is attached after a text search that already ran.
// It must not say the search is stale, or that code_navigation had to run
// before ripgrep. That wording discards the result the tool just returned.
const codegraphSearchAgainHint = "This text search already ran and counts. Use these lines. Do not repeat this search."

// codegraphSearchPageHint is the same evidence rule for a result that stopped
// at the match cap. "Do not repeat" there hides every later page.
const codegraphSearchPageHint = "This text search already ran and counts. Use these lines. The result stopped at max_results, so a later page of this same pattern may pass offset."

// codegraphSearchWalkHint is for a walk that hit the time limit before the
// tree was finished. "Do not repeat" there hides every file the walk never opened.
const codegraphSearchWalkHint = "This text search already ran and counts. Use these lines. The walk stopped before the tree was finished, so a narrower path may follow."

const remoteCodegraphHitMarker = "MACLAW_CODEGRAPH_HIT"

// takeRemoteCodegraphHitMarker reports whether codegraph itself produced a hit.
// Only a line that is exactly the marker counts, so a source file that contains
// the same word stays in the output and does not flip the hint.
func takeRemoteCodegraphHitMarker(result string) (string, bool) {
	if !strings.Contains(result, remoteCodegraphHitMarker) {
		return result, false
	}
	lines := strings.Split(result, "\n")
	hit := false
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.TrimSpace(line) == remoteCodegraphHitMarker {
			hit = true
			continue
		}
		kept = append(kept, line)
	}
	return strings.Join(kept, "\n"), hit
}

func codegraphContinueHint(backend string) string {
	if backend == "codegraph" {
		return codegraphKeepUsingHint
	}
	// "before ripgrep" told the model the lookup it just made does not count.
	return "This lookup did not return a codegraph hit. The output above still counts. A literal or regular-expression search may follow it."
}

// appendCodegraphSearchHint is added only after a text search that returned
// match lines, and only when this project or a parent already has an index.
// A miss stays unmarked: "do not repeat" on "no matches" tells the model the
// empty result is final and the next pattern must not run.
func (c *codingSubAgentCallbacks) appendCodegraphSearchHint(text string) string {
	if c == nil || !codegraphSearchResultHasLines(text) || codegraphSearchHintAlreadyPresent(text) {
		return text
	}
	if codegraphNearestIndexRoot(c.projectPath()) == "" {
		return text
	}
	hint := codegraphSearchAgainHint
	if codegraphSearchStoppedAtCap(text) {
		hint = codegraphSearchPageHint
	} else if codegraphSearchWalkStopped(text) {
		hint = codegraphSearchWalkHint
	}
	return strings.TrimSpace(text) + "\n" + hint
}

// codegraphSearchStoppedAtCap is true only for the tool footer. A source
// line that mentions max_results is a match, not a sign that paging remains.
func codegraphSearchStoppedAtCap(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "... reached max_results=") {
			return true
		}
	}
	return false
}

func codegraphSearchWalkStopped(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "... search stopped before the tree was finished" {
			return true
		}
	}
	return false
}

func codegraphSearchHintAlreadyPresent(text string) bool {
	lines := strings.Split(text, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	return last == codegraphSearchAgainHint || last == codegraphSearchPageHint || last == codegraphSearchWalkHint
}

func codegraphSearchResultHasLines(text string) bool {
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "... reached max_results=") || line == "... search stopped before the tree was finished" || codegraphSearchCommandStatusLine(line) {
			continue
		}
		lower := strings.ToLower(line)
		if strings.HasPrefix(lower, "no matches") || strings.HasPrefix(lower, "no match") {
			return false
		}
		return true
	}
	return false
}

// codegraphSearchCommandStatusLine is the shell wrapper around a search, not
// a hit. Sealing "command exited with code 1" told the model an empty rg or
// Select-String had already been searched.
func codegraphSearchCommandStatusLine(line string) bool {
	lower := strings.ToLower(strings.TrimSpace(line))
	return strings.HasPrefix(lower, "command exited with code ") ||
		strings.HasPrefix(lower, "(command completed with no output)") ||
		strings.HasPrefix(lower, "command timed out") ||
		strings.HasPrefix(lower, "command cancelled") ||
		strings.HasPrefix(lower, "a match was found; quiet mode printed nothing")
}

func normalizeCodeNavigationArgs(args map[string]interface{}) (operation, query string, depth int, err error) {
	operation = strings.ToLower(strings.TrimSpace(fmt.Sprint(args["operation"])))
	query = strings.TrimSpace(fmt.Sprint(args["query"]))
	if query == "" || query == "<nil>" {
		return "", "", 0, fmt.Errorf("query is required")
	}
	allowed := map[string]bool{"explore": true, "node": true, "workspace_symbol": true, "definition": true, "references": true, "implementation": true, "callers": true, "callees": true, "file_symbols": true}
	if !allowed[operation] {
		return "", "", 0, fmt.Errorf("unsupported operation %q", operation)
	}
	depth = 2
	if n, ok := codingSubAgentArgumentIntegerValue(args["depth"]); ok {
		depth = int(n)
	}
	if depth < 1 {
		depth = 1
	}
	if depth > 5 {
		depth = 5
	}
	return operation, query, depth, nil
}

func navigationQuestion(operation, query string, depth int) string {
	switch operation {
	case "node", "file_symbols":
		return query
	case "definition":
		return fmt.Sprintf("Find the definition of %s and show its source", query)
	case "references":
		return fmt.Sprintf("Find references and usages of %s", query)
	case "implementation":
		return fmt.Sprintf("Find implementations of %s", query)
	case "callers":
		return fmt.Sprintf("Find callers of %s and trace incoming call paths to depth %d", query, depth)
	case "callees":
		return fmt.Sprintf("Find functions called by %s and trace outgoing call paths to depth %d", query, depth)
	default:
		return query
	}
}

// codegraphCommandList picks narrow CodeGraph commands. A prose question
// becomes a few location queries, including short tokens such as bmp, instead
// of one fuzzy explore that prints source.
func codegraphCommandList(operation, query, question, file string) [][]string {
	file = codegraphPinFile(file)
	switch operation {
	case "file_symbols":
		return [][]string{codegraphNodeArgs(query, true)}
	case "node", "definition":
		if file != "" && !codegraphTargetIsFile(query) {
			return [][]string{{"node", "--file", file, query}}
		}
		return [][]string{codegraphNodeArgs(query, codegraphTargetIsFile(query))}
	case "workspace_symbol", "implementation":
		return [][]string{{"query", query}}
	case "references", "callers":
		if file != "" {
			return [][]string{{"callers", query, "--file", file}}
		}
		return [][]string{{"callers", query}}
	case "callees":
		if file != "" {
			return [][]string{{"callees", query, "--file", file}}
		}
		return [][]string{{"callees", query}}
	default:
		target := strings.TrimSpace(question)
		if target == "" {
			target = strings.TrimSpace(query)
		}
		if codegraphTargetIsFile(target) {
			return [][]string{codegraphNodeArgs(target, true)}
		}
		if codegraphTargetIsSymbol(target) {
			return [][]string{{"node", target}}
		}
		if tokens := navigationSearchTokens(target, 3); len(tokens) > 0 {
			cmds := make([][]string, len(tokens))
			for i, token := range tokens {
				cmds[i] = []string{"query", "--limit", "4", token}
			}
			return cmds
		}
		return [][]string{{"explore", "--max-files", "8", target}}
	}
}

// codegraphPinFile is a project-relative file the CLI may use to pick one
// overload. callers itself cannot take a file, and a path outside the project
// must not be passed through.
func codegraphPinFile(file string) string {
	file = strings.TrimSpace(file)
	if file == "" || file == "<nil>" || navigationQueryEscapesRoot(file) || strings.ContainsAny(file, "\r\n'\"") {
		return ""
	}
	return strings.ReplaceAll(file, `\`, "/")
}

// codegraphNodeSymbolArg is the symbol of a node lookup. File mode and
// --symbols-only are not symbol names.
func codegraphNodeSymbolArg(args []string) string {
	if len(args) < 2 || args[0] != "node" {
		return ""
	}
	for _, arg := range args {
		if arg == "--symbols-only" {
			return ""
		}
	}
	symbol := args[len(args)-1]
	if codegraphTargetIsFile(symbol) {
		return ""
	}
	return symbol
}

func codegraphArgs(operation, query, question string) []string {
	cmds := codegraphCommandList(operation, query, question, "")
	if len(cmds) == 0 {
		return []string{"explore", question}
	}
	return cmds[0]
}

// codegraphNodeArgs reads one symbol in full. A file path uses the symbol map
// only; the numbered full-file view is the same cost as read_file.
func codegraphNodeArgs(target string, symbolsOnly bool) []string {
	if symbolsOnly {
		return []string{"node", "--symbols-only", target}
	}
	return []string{"node", target}
}

func codegraphTargetIsFile(target string) bool {
	target = strings.TrimSpace(target)
	if target == "" || strings.ContainsAny(target, " \t\n") {
		return false
	}
	if strings.ContainsAny(target, `/\`) {
		return true
	}
	switch strings.ToLower(filepath.Ext(target)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".rb", ".php", ".vue", ".svelte", ".kt", ".swift":
		return true
	default:
		return false
	}
}

func codegraphTargetIsSymbol(target string) bool {
	if target == "" || strings.ContainsAny(target, " \t\n\"'`，。！？、；：（）") {
		return false
	}
	for _, r := range target {
		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r) {
			return false
		}
	}
	return true
}

func formatCodegraphRemoteArgs(args []string) string {
	var b strings.Builder
	for i, arg := range args {
		if i > 0 {
			b.WriteByte(' ')
		}
		if i == 0 {
			b.WriteString(arg)
			continue
		}
		b.WriteString(remoteShellQuote(arg))
	}
	return b.String()
}

// formatCodegraphRemoteInvocation runs one command, or several location
// queries when a question has more than one distinctive word.
func formatCodegraphRemoteInvocation(cmds [][]string) string {
	if len(cmds) == 0 {
		return ":"
	}
	if len(cmds) == 1 && len(cmds[0]) >= 2 && (cmds[0][0] == "callers" || cmds[0][0] == "callees") && (len(cmds[0]) == 2 || (len(cmds[0]) == 4 && cmds[0][2] == "--file")) {
		file := ""
		if len(cmds[0]) == 4 {
			file = cmds[0][3]
		}
		return codegraphRemoteEdgeAfterExactNode(cmds[0][0], cmds[0][1], file)
	}
	if len(cmds) == 1 {
		accept := codegraphRemoteAcceptHit
		if symbol := codegraphNodeSymbolArg(cmds[0]); symbol != "" {
			accept = codegraphRemoteAcceptNamedNode(symbol)
		}
		script := "if _cg_out=$(\"$_cg_bin\" " + formatCodegraphRemoteArgs(cmds[0]) + " 2>&1); then " + accept + "; fi"
		if cmds[0][0] == "query" {
			script += "; " + codegraphRemoteSymbolMapFrom("_cg_out")
		}
		return script
	}
	var b strings.Builder
	b.WriteString("_cg_blob=; for _cg_term in")
	for _, cmd := range cmds {
		b.WriteByte(' ')
		b.WriteString(remoteShellQuote(cmd[len(cmd)-1]))
	}
	b.WriteString("; do _cg_term_hit=0; if _cg_out=$(\"$_cg_bin\" query --limit 4 \"$_cg_term\" 2>&1); then " + codegraphRemoteAcceptHit + "; fi; if [ \"$_cg_term_hit\" -eq 1 ]; then _cg_blob=\"${_cg_blob}\n${_cg_out}\"; fi; if [ \"$_cg_term_hit\" -eq 0 ]; then _cg_miss=\"${_cg_miss} ${_cg_term}\"; fi; done; " + codegraphRemoteSymbolMapFrom("_cg_blob") + "; if [ \"$_cg_hit\" -eq 1 ] && [ -n \"$_cg_miss\" ]; then for _cg_term in $_cg_miss; do if _cg_found=$(command -v rg 2>&1); then rg -n --no-heading --color never -m 20 -- \"$_cg_term\" .; else grep -RIn -m 20 -- \"$_cg_term\" .; fi; done; fi")
	return b.String()
}

// codegraphSymbolMapNote tells the model the names in a file map are the next
// lookups. The prose query that found the file does not include those bodies.
const codegraphSymbolMapNote = "Symbol map for %s. A name below is a symbol: call code_navigation with operation node and that name for its body. read_file may use the line after the dash."

// codegraphDominantFileAwk prints score:file for each source file named by a
// query location. The score is the one on the immediately previous line. The
// shell keeps the highest score whose file is still on disk.
const codegraphDominantFileAwk = `{
  s=$0
  gsub(/\033\[[0-9;]*[A-Za-z]/, "", s)
  sub(/\r$/, "", s)
  sub(/^[ \t]+/, "", s)
  sub(/[ \t]+$/, "", s)
  cur=0
  got=0
  if (match(s, /\(([0-9]+)%\)/)) {
    sc=substr(s, RSTART, RLENGTH)
    gsub(/[^0-9]/, "", sc)
    cur=sc+0
    got=1
  }
  if (s ~ /^[A-Za-z0-9_.\/\\-]+:[0-9]+$/) {
    f=s
    sub(/:[0-9]+$/, "", f)
    ext=f
    sub(/^.*\./, "", ext)
    use=0
    if (had) use=prev
    if (f ~ /\.[A-Za-z0-9]+$/ && index(f, "..") == 0 && index(f, "/") != 1 && index(f, ".codegraph") == 0 && ext ~ /^(go|ts|tsx|js|jsx|py|rs|java|c|h|cc|cpp|hpp|cs|rb|php|vue|svelte|kt|swift)$/) {
      if (!(f in seen) || bestsc[f] < use) bestsc[f]=use
      seen[f]=1
    }
    had=0
    prev=0
  } else {
    had=got
    prev=cur
  }
}
END { for (f in seen) print bestsc[f] ":" f }`

// codegraphSymbolMapFilterAwk drops the CLI footer that tells the model to
// read the whole file. With hit line numbers it keeps the dozen symbols on
// either side of the one that contains each hit. A fixed span of source lines
// drops the next function when that function starts far below the hit.
const codegraphSymbolMapFilterAwk = `BEGIN { nh=0; if (hits != "") nh=split(hits, hv, ",") }
{
  if (index($0, "like Read") != 0) next
  n++
  a[n]=$0
}
END {
  if (0 < nh && 90 < n) {
    scount=0
    matched=0
    for (j=1; j<=n; j++) {
      t=a[j]
      sub(/\r$/, "", t)
      sub(/^[ \t]+/, "", t)
      sym=0
      ln=0
      if (t ~ /^- /) {
        sym=1
        if (match(t, /:[0-9]+$/)) ln=substr(t, RSTART+1)+0
      }
      if (sym) {
        scount++
        sj[scount]=j
        sl[scount]=ln
      } else {
        m++
        b[m]=a[j]
        gap=0
      }
    }
    for (k=1; k<=scount; k++) {
      if (k <= 15) keep[k]=1
    }
    for (i=1; i<=nh; i++) {
      chosen=0
      for (k=1; k<=scount; k++) {
        if (sl[k] == hv[i]) chosen=k
      }
      if (chosen == 0) continue
      matched=1
      lo=chosen-12
      if (lo < 1) lo=1
      hi=chosen+12
      if (scount < hi) hi=scount
      for (k=lo; k<=hi; k++) keep[k]=1
    }
    if (matched == 1) {
    gap=0
    m=0
    seen=0
    for (j=1; j<=n; j++) {
      t=a[j]
      sub(/\r$/, "", t)
      sub(/^[ \t]+/, "", t)
      if (t ~ /^- /) {
        seen++
        if (keep[seen]) { m++; b[m]=a[j]; gap=0 }
        else if (0 < m && gap == 0) { m++; b[m]="..."; gap=1 }
      } else {
        m++
        b[m]=a[j]
        gap=0
      }
    }
    for (i=1; i<=m; i++) print b[i]
    exit
    }
  }
  if (n <= 90) { for (i=1; i<=n; i++) print a[i]; exit }
  for (i=1; i<=45; i++) print a[i]
  print "..."
  s=n-44
  for (i=s; i<=n; i++) print a[i]
}`

// codegraphFileHitLinesAwk prints the comma-separated line numbers of one
// source file in a query result. The symbol map uses them to keep the
// surrounding names.
const codegraphFileHitLinesAwk = `{
  s=$0
  gsub(/\033\[[0-9;]*[A-Za-z]/, "", s)
  sub(/\r$/, "", s)
  sub(/^[ \t]+/, "", s)
  sub(/[ \t]+$/, "", s)
  if (s ~ /^[A-Za-z0-9_.\/\\-]+:[0-9]+$/) {
    path=s
    sub(/:[0-9]+$/, "", path)
    if (path == f) {
      ln=s
      sub(/^.*:/, "", ln)
      k++
      lv[k]=ln+0
    }
  }
}
END {
  out=""
  for (i=1; i<=k; i++) {
    if (1 < i) out=out ","
    out=out lv[i]
  }
  if (out != "") print out
}`

func codegraphRemoteSymbolMapFrom(varName string) string {
	quoted := "\"$" + varName + "\""
	return "if [ \"$_cg_hit\" -eq 1 ]; then _cg_map_file=; _cg_best=-1; for _cg_row in $(printf '%s\\n' " + quoted + " | awk " + remoteShellQuote(codegraphDominantFileAwk) + "); do _cg_sc=${_cg_row%%:*}; _cg_f=${_cg_row#*:}; if [ -n \"$_cg_f\" ] && [ -f \"$_cg_f\" ] && [ \"$_cg_sc\" -gt \"$_cg_best\" ]; then _cg_best=$_cg_sc; _cg_map_file=$_cg_f; fi; done; if [ -n \"$_cg_map_file\" ]; then " + codegraphRemoteHitBodies(quoted) + "; if _cg_map=$(\"$_cg_bin\" node --symbols-only \"$_cg_map_file\" 2>&1); then if printf '%s\\n' \"$_cg_map\" | grep -F -q '### Symbols'; then printf '%s\\n' \"" + fmt.Sprintf(codegraphSymbolMapNote, "${_cg_map_file}") + "\"; _cg_hits=$(printf '%s\\n' " + quoted + " | awk -v f=\"$_cg_map_file\" " + remoteShellQuote(codegraphFileHitLinesAwk) + "); printf '%s\\n' \"$_cg_map\" | awk -v hits=\"$_cg_hits\" " + remoteShellQuote(codegraphSymbolMapFilterAwk) + "; fi; fi; fi; fi"
}

// codegraphNamedHitAwk prints score:name for each query hit in one file.
// The name is the token before the score, and only the following location
// line belongs to that score.
const codegraphNamedHitAwk = `{
  s=$0
  gsub(/\033\[[0-9;]*[A-Za-z]/, "", s)
  sub(/\r$/, "", s)
  sub(/^[ \t]+/, "", s)
  sub(/[ \t]+$/, "", s)
  if (match(s, /\(([0-9]+)%\)/)) {
    sc=substr(s, RSTART, RLENGTH)
    gsub(/[^0-9]/, "", sc)
    name=s
    sub(/ *\([0-9][0-9]*%\).*/, "", name)
    sub(/^[^ ]+ +/, "", name)
    if (name ~ /^[A-Za-z_][A-Za-z0-9_:.]*$/) { pending=name; pscore=sc+0; got=1 }
  }
  if (s ~ /^[A-Za-z0-9_.\/\\-]+:[0-9]+$/) {
    path=s
    sub(/:[0-9]+$/, "", path)
    if (got && path == f && 2000 <= pscore) print pscore ":" pending
    got=0
  }
}`

// codegraphSourceBodyAwk prints the current definition of one symbol.
// codegraph node --file omits the source, and node without --file prints the
// indexed byte range, which stays on the old line after the file changes.
const codegraphSourceBodyAwk = `{
  s = $0
  sub(/\r$/, "", s)
  if (taking == 0) {
    t = s
    sub(/^[ \t]+/, "", t)
    if (pending == 0 && (t ~ /^\/\// || t ~ /^\/\*/ || t ~ /^\*/ || t ~ /^#/)) next
    if (pending == 0) {
      key = name "("
      p = index(s, key)
      if (p == 0) next
      if (1 < p) {
        ch = substr(s, p - 1, 1)
        if (ch ~ /[A-Za-z0-9_]/) next
        q = p - 1
        while (0 < q && (substr(s, q, 1) == " " || substr(s, q, 1) == "\t")) q = q - 1
        ch = substr(s, q, 1)
        if (ch == "(" || ch == ".") next
      }
      cmt = index(s, "//")
      if (0 < cmt && cmt < p) next
      rest = substr(s, p)
      br = index(rest, "{")
      sm = index(rest, ";")
      if (0 < br && (sm == 0 || br < sm)) taking = 1
      else if (sm == 0 && br == 0) { pending = 1; held = NR "\t" s; heldn = 1; next }
      else next
    } else {
      if (6 < heldn) { pending = 0; held = ""; next }
      br = index(s, "{")
      sm = index(s, ";")
      if (0 < br && (sm == 0 || br < sm)) { taking = 1; pending = 0; n = heldn; print held }
      else if (0 < sm && (br == 0 || sm < br)) { pending = 0; held = ""; next }
      else { held = held "\n" NR "\t" s; heldn++; next }
    }
  }
  n++
  print NR "\t" s
  cut = s
  cmt = index(cut, "//")
  if (0 < cmt) cut = substr(cut, 1, cmt - 1)
  tmp = cut
  o = gsub(/{/, "{", tmp)
  tmp = cut
  c = gsub(/}/, "}", tmp)
  depth = depth + o - c
  if (0 < o) seen = 1
  if (seen == 1 && depth < 1) exit
  if (400 < n) { print "..."; exit }
}`

func codegraphRemoteHitBodies(quoted string) string {
	return "_cg_b1=; _cg_s1=-1; _cg_b2=; _cg_s2=-1; for _cg_row in $(printf '%s\\n' " + quoted + " | awk -v f=\"$_cg_map_file\" " + remoteShellQuote(codegraphNamedHitAwk) + "); do _cg_sc=${_cg_row%%:*}; _cg_nm=${_cg_row#*:}; if [ -n \"$_cg_nm\" ] && [ \"$_cg_sc\" -gt \"$_cg_s1\" ]; then if [ \"$_cg_nm\" != \"$_cg_b1\" ]; then _cg_s2=$_cg_s1; _cg_b2=$_cg_b1; fi; _cg_s1=$_cg_sc; _cg_b1=$_cg_nm; elif [ -n \"$_cg_nm\" ] && [ \"$_cg_nm\" != \"$_cg_b1\" ] && [ \"$_cg_nm\" != \"$_cg_b2\" ] && [ \"$_cg_sc\" -gt \"$_cg_s2\" ]; then _cg_s2=$_cg_sc; _cg_b2=$_cg_nm; fi; done; for _cg_nm in $_cg_b1 $_cg_b2; do if [ -n \"$_cg_nm\" ] && [ -f \"$_cg_map_file\" ]; then _cg_src=$(awk -v name=\"$_cg_nm\" " + remoteShellQuote(codegraphSourceBodyAwk) + " \"$_cg_map_file\"); if [ -n \"$_cg_src\" ]; then printf '%s\\n' \"Body for ${_cg_nm} in ${_cg_map_file}.\"; printf '%s\\n' \"$_cg_src\"; fi; fi; done"
}

var codegraphHitLocationPattern = regexp.MustCompile(`^[ \t]*([A-Za-z0-9_./\\-]+\.[A-Za-z0-9]+):([0-9]+)[ \t]*$`)

// codegraphSourceBodyLines is the longest function text attached to a query
// hit. drawScene in the snake program is 127 lines, so a shorter cap drops
// its closing brace. A function that never balances stops here.
const codegraphSourceBodyLines = 400

func codegraphQueryBatch(cmds [][]string) bool {
	if len(cmds) == 0 {
		return false
	}
	for _, cmd := range cmds {
		if len(cmd) == 0 || cmd[0] != "query" {
			return false
		}
	}
	return true
}

func codegraphIndexedSourcePath(rel string) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".rs", ".java", ".c", ".h", ".cc", ".cpp", ".hpp", ".cs", ".rb", ".php", ".vue", ".svelte", ".kt", ".swift":
		return true
	default:
		return false
	}
}

// codegraphDominantHitFile is the source file of the highest-scoring query
// hit that is still on disk. A missing top hit must not hide the next file.
func codegraphDominantHitFile(output, root string) string {
	plain := codegraphANSIPattern.ReplaceAllString(output, "")
	best, bestScore := "", -1
	prevScore, prevHad := 0, false
	for _, line := range strings.Split(plain, "\n") {
		line = strings.TrimRight(line, "\r")
		thisScore, thisHad := 0, false
		if match := codegraphScorePattern.FindStringSubmatch(line); match != nil {
			if score, err := strconv.Atoi(match[1]); err == nil {
				thisScore, thisHad = score, true
			}
		}
		match := codegraphHitLocationPattern.FindStringSubmatch(line)
		if match == nil {
			prevScore, prevHad = thisScore, thisHad
			continue
		}
		rel := filepath.ToSlash(strings.TrimSpace(match[1]))
		score := 0
		if prevHad {
			score = prevScore
		}
		prevScore, prevHad = 0, false
		if rel == "" || strings.Contains(rel, "..") || strings.HasPrefix(rel, "/") || strings.Contains(rel, ".codegraph") || filepath.IsAbs(rel) || !codegraphIndexedSourcePath(rel) {
			continue
		}
		if root != "" {
			info, err := os.Stat(filepath.Join(root, filepath.FromSlash(rel)))
			if err != nil || info.IsDir() {
				continue
			}
		}
		if score > bestScore {
			best, bestScore = rel, score
		}
	}
	return best
}

var codegraphQueryScoreLine = regexp.MustCompile(`^(\S+)\s+(\S+)\s+\((\d+)%\)\s*$`)

// codegraphBodyTargets is the highest-scoring symbol names in one file.
// A prose query only lists those names. The model then ripgreps guesses and
// reads the whole file unless the tool also returns the bodies.
func codegraphBodyTargets(output, file string) []string {
	plain := codegraphANSIPattern.ReplaceAllString(output, "")
	file = filepath.ToSlash(strings.TrimSpace(file))
	type namedHit struct {
		name  string
		score int
	}
	var hits []namedHit
	pending, pendingScore := "", -1
	for _, line := range strings.Split(plain, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		if match := codegraphQueryScoreLine.FindStringSubmatch(line); match != nil {
			score, err := strconv.Atoi(match[3])
			if err != nil || !codegraphRemoteNodeSymbol(match[2]) {
				pending, pendingScore = "", -1
				continue
			}
			pending, pendingScore = match[2], score
			continue
		}
		loc := codegraphHitLocationPattern.FindStringSubmatch(line)
		if loc == nil || pending == "" {
			continue
		}
		if filepath.ToSlash(strings.TrimSpace(loc[1])) == file && pendingScore >= 2000 {
			hits = append(hits, namedHit{name: pending, score: pendingScore})
		}
		pending, pendingScore = "", -1
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return hits[i].score > hits[j].score
	})
	var names []string
	seen := map[string]bool{}
	for _, hit := range hits {
		if seen[hit.name] {
			continue
		}
		seen[hit.name] = true
		names = append(names, hit.name)
		if len(names) == 2 {
			break
		}
	}
	return names
}

// codegraphSourceBody is the function as it exists in the file now.
// The brace may be on the same line or on one of the next few lines.
// A call such as drawScene(dc); or if (drawScene(dc)) { is not a definition.
func codegraphSourceBody(root, file, name string) string {
	if root == "" || !codegraphRemoteNodeSymbol(name) || navigationQueryEscapesRoot(file) {
		return ""
	}
	abs := filepath.Join(root, filepath.FromSlash(file))
	if !navigationWithinRoot(root, abs) {
		return ""
	}
	data, err := os.ReadFile(abs)
	if err != nil || len(data) > 2<<20 {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	start := codegraphDefinitionStart(lines, name)
	if start < 0 {
		return ""
	}
	var b strings.Builder
	depth, seen := 0, false
	for i := start; i < len(lines) && i < start+codegraphSourceBodyLines; i++ {
		line := strings.TrimRight(lines[i], "\r")
		fmt.Fprintf(&b, "%d\t%s\n", i+1, line)
		opens, closes := codegraphBraceDelta(line)
		depth += opens - closes
		if opens > 0 {
			seen = true
		}
		if seen && depth < 1 {
			break
		}
	}
	if !seen {
		return ""
	}
	text := strings.TrimSpace(b.String())
	if start+codegraphSourceBodyLines < len(lines) && depth > 0 {
		text += "\n..."
	}
	return text
}

func codegraphDefinitionStart(lines []string, name string) int {
	for i, line := range lines {
		rest, ok := codegraphNameCall(strings.TrimRight(line, "\r"), name)
		if !ok {
			continue
		}
		if codegraphBraceOpens(rest) {
			return i
		}
		if codegraphSemiEnds(rest) {
			continue
		}
		for j := i + 1; j < len(lines) && j <= i+6; j++ {
			part := strings.TrimRight(lines[j], "\r")
			if codegraphBraceOpens(part) {
				return i
			}
			if codegraphSemiEnds(part) {
				break
			}
		}
	}
	return -1
}

func codegraphNameCall(line, name string) (string, bool) {
	trim := strings.TrimSpace(line)
	if trim == "" || strings.HasPrefix(trim, "//") || strings.HasPrefix(trim, "/*") || strings.HasPrefix(trim, "*") || strings.HasPrefix(trim, "#") {
		return "", false
	}
	key := name + "("
	p := strings.Index(line, key)
	if p < 0 {
		return "", false
	}
	if p > 0 {
		prev, _ := utf8.DecodeLastRuneInString(line[:p])
		if prev == '_' || unicode.IsLetter(prev) || unicode.IsDigit(prev) {
			return "", false
		}
		// `if (drawScene(dc)) {` and `obj.drawScene() {` are calls. A definition
		// is preceded by a type or by `::`.
		head := strings.TrimRight(line[:p], " \t")
		if head != "" {
			r, _ := utf8.DecodeLastRuneInString(head)
			if r == '(' || r == '.' {
				return "", false
			}
		}
	}
	if cmt := strings.Index(line, "//"); cmt >= 0 && cmt < p {
		return "", false
	}
	return line[p:], true
}

func codegraphCodeHead(line string) string {
	if cmt := strings.Index(line, "//"); cmt >= 0 {
		return line[:cmt]
	}
	return line
}

func codegraphBraceOpens(line string) bool {
	line = codegraphCodeHead(line)
	brace := strings.Index(line, "{")
	semi := strings.Index(line, ";")
	return brace >= 0 && (semi < 0 || brace < semi)
}

func codegraphSemiEnds(line string) bool {
	line = codegraphCodeHead(line)
	brace := strings.Index(line, "{")
	semi := strings.Index(line, ";")
	return semi >= 0 && (brace < 0 || semi < brace)
}

func codegraphBraceDelta(line string) (opens, closes int) {
	if cmt := strings.Index(line, "//"); cmt >= 0 {
		line = line[:cmt]
	}
	var quote byte
	for i := 0; i < len(line); i++ {
		c := line[i]
		if quote != 0 {
			if c == '\\' && i+1 < len(line) {
				i++
				continue
			}
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'':
			quote = c
		case '{':
			opens++
		case '}':
			closes++
		}
	}
	return opens, closes
}

func codegraphCapText(text string, max int) string {
	text = strings.TrimSpace(text)
	if len(text) <= max {
		return text
	}
	cut := max
	for cut > 0 && !utf8.RuneStart(text[cut]) {
		cut--
	}
	return strings.TrimSpace(text[:cut]) + "\n..."
}

// codegraphTrimSymbolMap removes the CLI footer. Hit line numbers keep the
// symbols around those lines, including one that sits in the middle of a long
// file. Without hit lines, both ends are kept so the list is not only its first page.
func codegraphTrimSymbolMap(data string, hits []int) string {
	var lines []string
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, ">") || strings.Contains(trim, "like Read") {
			continue
		}
		if trim == "" && len(lines) == 0 {
			continue
		}
		lines = append(lines, line)
	}
	if len(hits) > 0 && len(lines) > 90 {
		type mapRow struct {
			text   string
			symbol int
		}
		var rows []mapRow
		var starts []int
		symbols := 0
		for _, line := range lines {
			lineNo, isSymbol := codegraphMapSymbolLine(line)
			if !isSymbol {
				rows = append(rows, mapRow{text: line})
				continue
			}
			symbols++
			rows = append(rows, mapRow{text: line, symbol: symbols})
			starts = append(starts, lineNo)
		}
		keep := make([]bool, symbols+1)
		headN := symbols
		if headN > 15 {
			headN = 15
		}
		for i := 1; i <= headN; i++ {
			keep[i] = true
		}
		matched := false
		for _, hit := range hits {
			chosen := 0
			for i, start := range starts {
				if start == hit {
					chosen = i + 1
				}
			}
			if chosen == 0 {
				continue
			}
			matched = true
			lo, hi := chosen-12, chosen+12
			if lo < 1 {
				lo = 1
			}
			if hi > symbols {
				hi = symbols
			}
			for i := lo; i <= hi; i++ {
				keep[i] = true
			}
		}
		if matched {
			var kept []string
			gap := false
			for _, row := range rows {
				if row.symbol == 0 {
					kept = append(kept, row.text)
					gap = false
					continue
				}
				if keep[row.symbol] {
					kept = append(kept, row.text)
					gap = false
					continue
				}
				if len(kept) > 0 && !gap {
					kept = append(kept, "...")
					gap = true
				}
			}
			return strings.TrimSpace(strings.Join(kept, "\n"))
		}
	}
	const head, tail = 45, 45
	if len(lines) > head+tail {
		kept := make([]string, 0, head+tail+1)
		kept = append(kept, lines[:head]...)
		kept = append(kept, "...")
		kept = append(kept, lines[len(lines)-tail:]...)
		lines = kept
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func codegraphMapSymbolLine(line string) (int, bool) {
	trim := strings.TrimSpace(line)
	if !strings.HasPrefix(trim, "- ") {
		return 0, false
	}
	colon := strings.LastIndex(trim, ":")
	if colon < 0 {
		return 0, true
	}
	n, err := strconv.Atoi(strings.TrimSpace(trim[colon+1:]))
	if err != nil || n <= 0 {
		return 0, true
	}
	return n, true
}

func codegraphHitLinesForFile(output, file string) []int {
	plain := codegraphANSIPattern.ReplaceAllString(output, "")
	file = filepath.ToSlash(strings.TrimSpace(file))
	var lines []int
	for _, line := range strings.Split(plain, "\n") {
		line = strings.TrimRight(line, "\r")
		match := codegraphHitLocationPattern.FindStringSubmatch(line)
		if match == nil || filepath.ToSlash(strings.TrimSpace(match[1])) != file {
			continue
		}
		n, err := strconv.Atoi(match[2])
		if err != nil || n <= 0 {
			continue
		}
		lines = append(lines, n)
	}
	return lines
}

func attachCodegraphSymbolMap(bin, root, output string, cmds [][]string) string {
	if bin == "" || root == "" || !codegraphQueryBatch(cmds) {
		return output
	}
	file := codegraphDominantHitFile(output, root)
	if file == "" {
		return output
	}
	var bodies strings.Builder
	for _, name := range codegraphBodyTargets(output, file) {
		// node --file returns the trail only. node without --file returns the
		// indexed range, which can be an older line of a different function.
		body := codegraphSourceBody(root, file, name)
		if body == "" {
			continue
		}
		if bodies.Len() > 0 {
			bodies.WriteString("\n\n")
		}
		bodies.WriteString("Body for " + name + " in " + file + ".\n")
		bodies.WriteString(codegraphCapText(body, 8000))
	}
	ctx, cancel := codegraphStepContext()
	defer cancel()
	data, err := runCodegraphCommand(ctx, bin, root, "node", "--symbols-only", file)
	if err != nil || !strings.Contains(data, "### Symbols") {
		if bodies.Len() == 0 {
			return output
		}
		return strings.TrimSpace(output) + "\n\n" + bodies.String()
	}
	data = codegraphTrimSymbolMap(data, codegraphHitLinesForFile(output, file))
	result := strings.TrimSpace(output)
	if bodies.Len() > 0 {
		result += "\n\n" + bodies.String()
	}
	return result + "\n\n" + fmt.Sprintf(codegraphSymbolMapNote, file) + "\n" + data
}

// codegraphRemoteAcceptHit keeps a codegraph result. A one-line miss, or a
// query score below 2000, falls through to text search.
const codegraphRemoteAcceptHit = "_cg_esc=$(printf '\\033'); _cg_plain=$(printf '%s\\n' \"$_cg_out\" | sed \"s/${_cg_esc}\\\\[[0-9;]*[A-Za-z]//g\"); _cg_first=$(printf '%s\\n' \"$_cg_plain\" | sed -n '/[[:alpha:]]/p' | head -n 1); _cg_score=$(printf '%s\\n' \"$_cg_plain\" | sed -n 's/.*(\\([0-9][0-9]*\\)%).*/\\1/p' | head -n 1); case \"$_cg_first\" in *'Symbol \"'*' not found'*) ;; *'No results found'*) ;; *'No relevant code found'*) ;; *'No files indexed'*) ;; *'No indexed file matches'*) ;; *'No callers found'*) ;; *'No callees found'*) ;; *'Search Results'*) if [ -z \"$_cg_score\" ] || [ \"$_cg_score\" -ge 2000 ]; then printf '%s\\n' \"$_cg_out\"; _cg_hit=1; _cg_term_hit=1; fi ;; *[![:space:]]*) printf '%s\\n' \"$_cg_out\"; _cg_hit=1; _cg_term_hit=1 ;; esac"

// codegraphRemoteAcceptNamedNode is the node accept path. A misspelled name
// still prints the nearest symbol and exits 0; that body is not the symbol
// the caller asked for, so the text search has to run.
func codegraphRemoteAcceptNamedNode(symbol string) string {
	if !codegraphRemoteNodeSymbol(symbol) {
		return codegraphRemoteAcceptHit
	}
	const printed = "printf '%s\\n' \"$_cg_out\"; _cg_hit=1; _cg_term_hit=1"
	var heads []string
	for _, name := range codegraphNodeHeadingNames(symbol) {
		heads = append(heads, "'## "+name+" '*|'## "+name+"'|'## '*'::"+name+" '*|'## '*'."+name+" '*")
	}
	head := strings.Join(heads, "|")
	named := "*'definitions named \"" + symbol + "\"'*"
	replacement := "_cg_head=$(printf '%s\\n' \"$_cg_plain\" | sed -n '/^## /p' | head -n 1); case \"$_cg_head\" in " + head + ") " + printed + " ;; *) case \"$_cg_plain\" in " + named + ") " + printed + " ;; esac ;; esac ;; esac"
	needle := "*[![:space:]]*) " + printed + " ;; esac"
	if !strings.Contains(codegraphRemoteAcceptHit, needle) {
		return codegraphRemoteAcceptHit
	}
	return strings.Replace(codegraphRemoteAcceptHit, needle, "*[![:space:]]*) "+replacement, 1)
}

// codegraphRemoteEdgeAfterExactNode runs callers or callees only after node
// names that symbol. Those commands otherwise return the nearest symbol's
// edges and label them with the name that was asked.
func codegraphRemoteEdgeAfterExactNode(verb, symbol, file string) string {
	if verb != "callers" && verb != "callees" {
		return ":"
	}
	if codegraphRemoteNodeSymbol(symbol) && (file != "" || codegraphSymbolIsQualified(symbol)) {
		// callers compares the whole string with node.name and ignores --file.
		// A qualified name misses, and a file pin would still mix in every
		// other file's edges. The node trail is already scoped.
		return codegraphRemoteQualifiedTrail(verb, symbol, file)
	}
	edge := "if _cg_out=$(\"$_cg_bin\" " + formatCodegraphRemoteArgs([]string{verb, symbol}) + " 2>&1); then " + codegraphRemoteAcceptHit + "; fi"
	if !codegraphRemoteNodeSymbol(symbol) {
		return edge
	}
	return "_cg_named=0; if _cg_out=$(\"$_cg_bin\" " + formatCodegraphRemoteArgs([]string{"node", symbol}) + " 2>&1); then " + codegraphRemoteNodeNameGate(symbol) + "; fi; if [ \"$_cg_named\" -eq 1 ]; then " + edge + "; fi"
}

// codegraphQualifiedAwk prints the heading, location, and one trail line for
// each section whose heading names the symbol. The program is single-quoted
// into a command substitution, so it must not contain a single quote.
const codegraphQualifiedAwk = `function ends(s, suf) { return length(s) >= length(suf) && substr(s, length(s) - length(suf) + 1) == suf } function want(h) { if (h == a || (b != "" && h == b)) return 1; if (a != "" && (ends(h, "." a) || ends(h, "::" a))) return 1; if (b != "" && (ends(h, "." b) || ends(h, "::" b))) return 1; return 0 } function flush() { if (keep && trail != "") { print head; if (loc != "") print loc; print trail } } { sub(/\r$/, ""); sub(/^[ \t]+/, "") } /^## / { flush(); head = $0; loc = ""; trail = ""; h = $0; sub(/^## /, "", h); sub(/ \(.*/, "", h); keep = want(h); next } keep && /^\*\*Location:/ && loc == "" { loc = $0; next } keep && index($0, label) == 1 { trail = $0; next } END { flush() }`

// codegraphRemoteQualifiedTrail keeps the resolved symbol's location and
// trail. callers compares the whole qualified string with node.name, misses,
// and then attaches the top neighbor's edges.
func codegraphRemoteQualifiedTrail(verb, symbol, file string) string {
	label := "**Called by \u2190"
	if verb == "callees" {
		label = "**Calls \u2192"
	}
	names := codegraphNodeHeadingNames(symbol)
	a, b := "", ""
	if len(names) > 0 {
		a = names[0]
	}
	if len(names) > 1 {
		b = names[1]
	}
	nodeArgs := []string{"node", symbol}
	if file != "" {
		nodeArgs = []string{"node", "--file", file, symbol}
	}
	return "_cg_named=0; if _cg_out=$(\"$_cg_bin\" " + formatCodegraphRemoteArgs(nodeArgs) + " 2>&1); then " + codegraphRemoteNodeNameGate(symbol) + "; fi; if [ \"$_cg_named\" -eq 1 ]; then _cg_edges=$(printf '%s\\n' \"$_cg_plain\" | awk -v a=" + remoteShellQuote(a) + " -v b=" + remoteShellQuote(b) + " -v label=" + remoteShellQuote(label) + " " + remoteShellQuote(codegraphQualifiedAwk) + "); if [ -n \"$_cg_edges\" ]; then printf '%s\\n' \"$_cg_edges\"; _cg_hit=1; _cg_term_hit=1; fi; fi"
}

// codegraphRemoteNodeNameGate sets _cg_named when the node heading is the
// requested symbol. It does not print the definition.
func codegraphRemoteNodeNameGate(symbol string) string {
	var heads []string
	for _, name := range codegraphNodeHeadingNames(symbol) {
		heads = append(heads, "'## "+name+" '*|'## "+name+"'|'## '*'::"+name+" '*|'## '*'."+name+" '*")
	}
	named := "*'definitions named \"" + symbol + "\"'*"
	return "_cg_esc=$(printf '\\033'); _cg_plain=$(printf '%s\\n' \"$_cg_out\" | sed \"s/${_cg_esc}\\\\[[0-9;]*[A-Za-z]//g\"); _cg_head=$(printf '%s\\n' \"$_cg_plain\" | sed -n '/^## /p' | head -n 1); case \"$_cg_head\" in " + strings.Join(heads, "|") + ") _cg_named=1 ;; *) case \"$_cg_plain\" in " + named + ") _cg_named=1 ;; esac ;; esac"
}

func codegraphRemoteNodeSymbol(symbol string) bool {
	if symbol == "" || len(symbol) > 200 {
		return false
	}
	for _, r := range symbol {
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '_' && r != ':' && r != '.' {
			return false
		}
	}
	return true
}

// codegraphOutputUseful reports whether CodeGraph returned a real hit.
// A miss still exits 0 and prints a one-line "not found" message; treating
// that as success would skip the text-search fallback.
func codegraphOutputUseful(output string) bool {
	text := strings.TrimSpace(output)
	if text == "" {
		return false
	}
	// Query output starts with a color-only line. The ranking header is the
	// next line that contains a letter. Source that merely mentions that
	// header later is still a hit.
	first := codegraphFirstLetterLine(text)
	if strings.Contains(first, "No results found") || strings.Contains(first, "No relevant code found") || strings.Contains(first, "No files indexed") || strings.Contains(first, "No indexed file matches") || strings.Contains(first, "No callers found") || strings.Contains(first, "No callees found") {
		return false
	}
	if strings.Contains(first, "Symbol \"") && strings.Contains(first, " not found") {
		return false
	}
	if strings.Contains(first, "Search Results") {
		if score, ok := codegraphBestScore(text); ok && score < 2000 {
			return false
		}
	}
	return true
}

// codegraphFirstLetterLine skips the color-only line codegraph prints before
// a query header.
func codegraphFirstLetterLine(output string) string {
	output = codegraphANSIPattern.ReplaceAllString(output, "")
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, r := range line {
			if unicode.IsLetter(r) {
				return line
			}
		}
	}
	return ""
}

// codegraphBestScore reads the first "(NNN%)" relevance codegraph query prints.
// A weak score is a fuzzy name, not a symbol the caller asked for.
func codegraphBestScore(output string) (int, bool) {
	match := codegraphScorePattern.FindStringSubmatch(output)
	if match == nil {
		return 0, false
	}
	score, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, false
	}
	return score, true
}

// codegraphCommandAccepted reports a usable codegraph result. node prints the
// nearest symbol when the exact name is missing, and that source must not
// count as a hit for the name that was asked.
func codegraphCommandAccepted(args []string, data string) bool {
	if symbol := codegraphNodeSymbolArg(args); symbol != "" {
		return codegraphNodeNamesSymbol(data, symbol)
	}
	return codegraphOutputUseful(data)
}

// codegraphNodeNamesSymbol is true when the node heading is the requested
// symbol. A heading for a different symbol is a fuzzy neighbor.
func codegraphNodeNamesSymbol(output, symbol string) bool {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" || !codegraphOutputUseful(output) {
		return false
	}
	plain := codegraphANSIPattern.ReplaceAllString(output, "")
	if strings.Contains(plain, "definitions named \""+symbol+"\"") {
		return true
	}
	for _, line := range strings.Split(plain, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "## ") {
			continue
		}
		// The first heading is the symbol. A later ## line is source or a
		// following section, and must not make a neighbor count as a hit.
		name := strings.TrimSpace(strings.TrimPrefix(line, "## "))
		if i := strings.Index(name, " ("); i >= 0 {
			name = strings.TrimSpace(name[:i])
		}
		return codegraphHeadingNameMatches(name, symbol)
	}
	return false
}

// codegraphSymbolIsQualified reports a lookup whose CLI heading is the short
// name. callers and callees do not resolve that qualifier.
func codegraphSymbolIsQualified(symbol string) bool {
	symbol = strings.TrimSpace(symbol)
	if strings.Contains(symbol, "::") {
		return true
	}
	dot := strings.LastIndex(symbol, ".")
	return dot > 0 && dot < len(symbol)-1
}

// codegraphQualifiedEdges keeps the heading and location of each section that
// names the symbol, plus that section's trail. A later heading is a different
// symbol, and a prose line that merely starts with "Calls" is not an edge.
func codegraphQualifiedEdges(output, symbol, verb string) string {
	label := "**Called by \u2190"
	if verb == "callees" {
		label = "**Calls \u2192"
	}
	plain := codegraphANSIPattern.ReplaceAllString(output, "")
	var b strings.Builder
	var heading, location, trail string
	match := false
	flush := func() {
		if !match || trail == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(heading)
		if location != "" {
			b.WriteByte('\n')
			b.WriteString(location)
		}
		b.WriteByte('\n')
		b.WriteString(trail)
	}
	for _, line := range strings.Split(plain, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## ") {
			flush()
			heading = line
			location = ""
			trail = ""
			name := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			if i := strings.Index(name, " ("); i >= 0 {
				name = strings.TrimSpace(name[:i])
			}
			match = codegraphHeadingNameMatches(name, symbol)
			continue
		}
		if !match {
			continue
		}
		if location == "" && strings.HasPrefix(line, "**Location:") {
			location = line
			continue
		}
		if trail == "" && strings.HasPrefix(line, label) {
			trail = line
		}
	}
	flush()
	return b.String()
}

// codegraphNodeHeadingNames includes the short name of a qualified lookup.
// node prints ## playTone for Audio::playTone; a bare misspelling has no
// shorter form, so playTon still does not match playTone.
func codegraphNodeHeadingNames(symbol string) []string {
	symbol = strings.TrimSpace(symbol)
	if symbol == "" {
		return nil
	}
	names := []string{symbol}
	tail := ""
	if i := strings.LastIndex(symbol, "::"); i >= 0 {
		tail = symbol[i+2:]
	} else if i := strings.LastIndex(symbol, "."); i > 0 && i < len(symbol)-1 {
		tail = symbol[i+1:]
	}
	if tail != "" && tail != symbol {
		names = append(names, tail)
	}
	return names
}

func codegraphHeadingNameMatches(name, symbol string) bool {
	for _, candidate := range codegraphNodeHeadingNames(symbol) {
		if name == candidate || strings.HasSuffix(name, "."+candidate) || strings.HasSuffix(name, "::"+candidate) {
			return true
		}
	}
	return false
}

func (c *codingSubAgentCallbacks) executeLocalCodeNavigation(args map[string]interface{}) codingToolExecutionResult {
	operation, query, depth, err := normalizeCodeNavigationArgs(args)
	if err != nil {
		return codingToolExecutionResult{Text: err.Error(), Outcome: codingToolOutcomeFailed}
	}
	root := c.projectPath()
	if root == "" {
		return codingToolExecutionResult{Text: "project path unavailable", Outcome: codingToolOutcomeFailed}
	}
	file := strings.TrimSpace(fmt.Sprint(args["file"]))
	if file == "<nil>" {
		file = ""
	}
	line, _ := codingSubAgentArgumentIntegerValue(args["line"])
	column, _ := codingSubAgentArgumentIntegerValue(args["column"])
	bootstrap := c.codegraphBootstrapAllowed()
	backend, output, runErr := runLocalCodeNavigationWithPosition(root, operation, query, file, int(line), int(column), depth, bootstrap)
	if runErr != nil {
		return codingToolExecutionResult{Text: runErr.Error(), Outcome: codingToolOutcomeFailed}
	}
	output, truncated := capCodeNavigationOutput(output)
	payload, _ := json.Marshal(codeNavigationOutput{Backend: backend, Operation: operation, Query: query, Output: output, Truncated: truncated, ContinueWith: codegraphContinueHint(backend)})
	// Candidate ranking is included in the structured output so the model can
	// compare suspects without rereading every raw match.
	candidates := localizationCandidatesFromOutput(output)
	if len(candidates) > 0 {
		var wrapper map[string]interface{}
		_ = json.Unmarshal(payload, &wrapper)
		wrapper["candidates"] = candidates
		payload, _ = json.Marshal(wrapper)
	}
	return codingToolExecutionResult{Text: string(payload), Outcome: codingToolOutcomeSuccess}
}

// codegraphBootstrapAllowed is the write gate for install and codegraph init.
// A missing index is not a reason to skip the tool, but creating that index
// is a project write. Inquiry, launch/host commands, read-only nested roles,
// and the compatibility surface must not do it.
func (c *codingSubAgentCallbacks) codegraphBootstrapAllowed() bool {
	if c == nil || codingTaskLooksInquiry(c.task) || codingTaskLooksOperational(c.task) {
		return false
	}
	if c.usesUncorrelatedStaticCompatibilityModelSurface() {
		return false
	}
	if c.subagent != nil && c.subagent.nestDepth > 0 &&
		(c.subagent.role == codingRoleExplorer || c.subagent.role == codingRoleReviewer) {
		return false
	}
	return true
}

func runLocalCodeNavigation(root, operation, query string, depth int) (string, string, error) {
	return runLocalCodeNavigationWithPosition(root, operation, query, "", 0, 0, depth, true)
}

func runLocalCodeNavigationWithPosition(root, operation, query, file string, line, column, depth int, bootstrap bool) (string, string, error) {
	// Install and index use their own budgets. The 30s query clock starts
	// afterward, so a slow bootstrap cannot cancel the codegraph call or the
	// ripgrep fallback that follows it.
	bin := lookCodegraphBinary()
	if bin == "" && bootstrap {
		bin = ensureLocalCodegraphBinary()
	}
	// A parent project can already own the index. CodeGraph queries walk up to
	// it, so a subdirectory must not build a second database beside that one.
	haveIndex := bin != "" && (codegraphNearestIndexRoot(root) != "" || (bootstrap && ensureLocalCodegraphIndex(root, bin)))
	if haveIndex {
		if text, mcpErr := codegraphMCPNavigate(root, bin, operation, query, navigationQuestion(operation, query, depth), file, line); mcpErr == nil && strings.TrimSpace(text) != "" {
			return "codegraph", text, nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		argsList := codegraphCommandList(operation, query, navigationQuestion(operation, query, depth), file)
		output, missed := runCodegraphCommands(ctx, bin, root, argsList)
		cancel()
		if strings.TrimSpace(output) != "" {
			output = attachCodegraphSymbolMap(bin, root, output, argsList)
			if len(missed) > 0 {
				extraCtx, extraCancel := context.WithTimeout(context.Background(), 30*time.Second)
				extra, _, extraErr := runLocalNavigationTextFallback(extraCtx, root, "explore", strings.Join(missed, " "))
				extraCancel()
				if extraErr == nil && strings.TrimSpace(extra) != "" {
					output = strings.TrimSpace(output) + "\n" + extra
				}
			}
			return "codegraph", output, nil
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if backend, output, ok := runLocalLSPNavigation(ctx, root, operation, query, file, line, column); ok {
		return backend, output, nil
	}
	return runLocalNavigationTextFallback(ctx, root, operation, query)
}

func runCodegraphCommands(ctx context.Context, bin, root string, cmds [][]string) (string, []string) {
	if len(cmds) == 0 {
		return "", nil
	}
	outs := make([]string, len(cmds))
	var wg sync.WaitGroup
	for i, args := range cmds {
		wg.Add(1)
		go func(i int, args []string) {
			defer wg.Done()
			if len(args) >= 2 && (args[0] == "callers" || args[0] == "callees") && (len(args) == 2 || (len(args) == 4 && args[2] == "--file")) {
				symbol := args[1]
				pin := ""
				if len(args) == 4 {
					pin = args[3]
				}
				// The name check and the edge list each get a full query
				// budget. One shared clock let a slow node cancel the callers
				// command, and the text search then ran for a symbol that exists.
				probeCtx, probeCancel := codegraphStepContext()
				nodeArgs := []string{"node", symbol}
				if pin != "" {
					nodeArgs = []string{"node", "--file", pin, symbol}
				}
				probe, _ := runCodegraphCommand(probeCtx, bin, root, nodeArgs...)
				probeCancel()
				if !codegraphNodeNamesSymbol(probe, symbol) {
					return
				}
				if pin != "" || codegraphSymbolIsQualified(symbol) {
					if edges := codegraphQualifiedEdges(probe, symbol, args[0]); edges != "" {
						outs[i] = edges
					}
					return
				}
				edgeCtx, edgeCancel := codegraphStepContext()
				data, err := runCodegraphCommand(edgeCtx, bin, root, args...)
				edgeCancel()
				if err == nil && codegraphOutputUseful(data) {
					outs[i] = data
				}
				return
			}
			data, err := runCodegraphCommand(ctx, bin, root, args...)
			if err == nil && codegraphCommandAccepted(args, data) {
				outs[i] = data
			}
		}(i, args)
	}
	wg.Wait()
	var b strings.Builder
	var missed []string
	for i, out := range outs {
		out = strings.TrimSpace(out)
		if out == "" {
			if len(cmds) > 1 && len(cmds[i]) > 0 && (cmds[i][0] == "query" || cmds[i][0] == "node") {
				missed = append(missed, cmds[i][len(cmds[i])-1])
			}
			continue
		}
		if b.Len() > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(out)
	}
	return b.String(), missed
}

// codegraphStepContext is one query budget. The navigation clock that
// started the name check must not become the callers deadline: once that
// clock is done, callers would be skipped for a symbol that was found.
func codegraphStepContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func runLocalLSPNavigation(ctx context.Context, root, operation, query, file string, line, column int) (string, string, bool) {
	if file == "" {
		return "", "", false
	}
	abs := file
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, file)
	}
	if strings.EqualFold(filepath.Ext(abs), ".go") {
		if _, err := exec.LookPath("gopls"); err != nil {
			return "", "", false
		}
		verb := map[string]string{"definition": "definition", "references": "references", "implementation": "implementation"}[operation]
		if verb == "" {
			return "", "", false
		}
		if line < 1 {
			line = 1
		}
		if column < 1 {
			column = 1
		}
		cmd := exec.CommandContext(ctx, "gopls", verb, fmt.Sprintf("%s:%d:%d", abs, line, column))
		cmd.Dir = root
		hideCommandWindow(cmd)
		data, err := cmd.CombinedOutput()
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return "gopls", string(data), true
		}
	}
	// Generic LSP clients can be provided by the environment through lsp-cli.
	// Its stable command form lets non-Go projects participate without coupling
	// this harness to every language-server protocol implementation.
	if _, err := exec.LookPath("lsp-cli"); err == nil {
		cmd := exec.CommandContext(ctx, "lsp-cli", operation, "--file", abs, "--line", strconv.Itoa(line), "--column", strconv.Itoa(column), "--query", query)
		cmd.Dir = root
		hideCommandWindow(cmd)
		data, err := cmd.CombinedOutput()
		if err == nil && strings.TrimSpace(string(data)) != "" {
			return "lsp-cli", string(data), true
		}
	}
	return "", "", false
}

func runLocalNavigationTextFallback(ctx context.Context, root, operation, query string) (string, string, error) {
	// A filename is not text inside the file. Searching the tree for
	// "snake.cpp" misses the file the caller asked to outline.
	if navigationShouldOutlineFile(operation, query) {
		if outline, ok := navigationFileSymbolOutline(root, query); ok {
			return "file", outline, nil
		}
	}
	rg := "rg"
	if _, err := exec.LookPath(rg); err != nil {
		return "", "", fmt.Errorf("code navigation unavailable: codegraph produced no result and rg is not installed")
	}
	pattern := navigationFallbackPattern(operation, query)
	cmd := exec.CommandContext(ctx, rg, "-n", "--no-heading", "--color", "never", "-m", "80", pattern, ".")
	cmd.Dir = root
	hideCommandWindow(cmd)
	data, err := cmd.CombinedOutput()
	if err != nil && len(data) == 0 {
		return "ripgrep", "", fmt.Errorf("no navigation matches for %q", query)
	}
	return "ripgrep", string(data), nil
}

func navigationFallbackPattern(operation, query string) string {
	quoted := regexp.QuoteMeta(strings.TrimSpace(query))
	switch operation {
	case "definition":
		// C and C++ definitions look like `void playTone(...) {` or `class Game`.
		// A call such as `playTone(440);` has a semicolon before any brace.
		return `(?i)((^|[^[:alnum:]_])` + quoted + `[[:space:]]*\([^;\n]*\{|(^|[^[:alnum:]_])(class|struct|enum|interface|func|function|def|type|typedef)\s+` + quoted + `([^[:alnum:]_]|$))`
	case "workspace_symbol":
		return `(?i)(^|[^[:alnum:]_])` + quoted + `([^[:alnum:]_]|$)`
	}
	fields := strings.Fields(query)
	if len(fields) < 2 {
		return quoted
	}
	var parts []string
	for _, field := range fields {
		field = strings.Trim(field, ".,;:!?\"'`()[]{}")
		if len([]rune(field)) < 3 || navigationFallbackStopWord(field) {
			continue
		}
		parts = append(parts, regexp.QuoteMeta(field))
		if len(parts) >= 6 {
			break
		}
	}
	if len(parts) == 0 {
		return quoted
	}
	return "(?i)(" + strings.Join(parts, "|") + ")"
}

func navigationFallbackStopWord(word string) bool {
	switch strings.ToLower(word) {
	case "the", "and", "for", "with", "from", "that", "this", "how", "what", "when", "where", "which", "into", "over", "code", "file", "data", "test", "tests", "check", "checks", "works", "using", "based",
		"int", "str", "err", "ctx", "buf", "tmp", "val", "ptr", "nil", "new", "var", "let", "get", "set", "len", "cap", "idx", "obj", "arg", "ret", "fmt", "raw", "out", "end", "max", "min", "pos", "num",
		"fix", "run", "use", "try", "put", "can", "may", "not", "but", "all", "one", "two", "its", "our", "you", "are", "was", "has", "had", "did", "own", "per", "via", "too", "now":
		return true
	default:
		return false
	}
}

// navigationSearchTokens picks a few search words from a prose question.
// A 3-letter token such as bmp is kept, but a 4-letter word does not outrank
// a longer specific word.
func navigationSearchTokens(query string, limit int) []string {
	if limit < 1 {
		return nil
	}
	type scoredToken struct {
		token string
		score int
		index int
	}
	var acronyms []string
	var rest []scoredToken
	for i, field := range strings.Fields(query) {
		field = strings.Trim(field, ".,;:!?\"'`()[]{}")
		if len([]rune(field)) < 3 || navigationFallbackStopWord(field) {
			continue
		}
		if len([]rune(field)) == 3 && navigationTokenIsAcronym(field) && len(acronyms) < 2 {
			acronyms = append(acronyms, field)
			continue
		}
		rest = append(rest, scoredToken{token: field, score: len([]rune(field)), index: i})
	}
	sort.SliceStable(rest, func(i, j int) bool {
		// Vague words such as sound/audio must not crowd out the project name.
		gi, gj := navigationTokenIsGeneric(rest[i].token), navigationTokenIsGeneric(rest[j].token)
		if gi != gj {
			return !gi
		}
		if rest[i].score != rest[j].score {
			return rest[i].score > rest[j].score
		}
		return rest[i].index < rest[j].index
	})
	out := make([]string, 0, limit)
	out = append(out, acronyms...)
	for _, item := range rest {
		if len(out) >= limit {
			break
		}
		out = append(out, item.token)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

func navigationTokenIsGeneric(token string) bool {
	switch strings.ToLower(token) {
	case "sound", "audio", "beep", "game", "tone", "noise", "music":
		return true
	default:
		return false
	}
}

func navigationTokenIsAcronym(token string) bool {
	if token == "" {
		return false
	}
	for _, r := range token {
		if !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func navigationResolveFile(root, query string) string {
	query = strings.TrimSpace(query)
	if query == "" || !codegraphTargetIsFile(query) {
		return ""
	}
	candidate := query
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(root, filepath.FromSlash(query))
	}
	if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() && navigationWithinRoot(root, candidate) {
		return candidate
	}
	base := filepath.Base(query)
	bestDepth := -1
	var found string
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		name := d.Name()
		if d.IsDir() {
			if path != root && (name == ".git" || name == "build" || name == "node_modules" || codegraphIsDataDir(name)) {
				return fs.SkipDir
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr == nil && rel != "." && strings.Count(rel, string(filepath.Separator)) > 3 {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.EqualFold(name, base) || !navigationWithinRoot(root, path) {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		depth := strings.Count(rel, string(filepath.Separator))
		if found == "" || depth < bestDepth {
			found = path
			bestDepth = depth
		}
		if bestDepth == 0 {
			return fs.SkipAll
		}
		return nil
	})
	return found
}

func navigationWithinRoot(root, path string) bool {
	root = filepath.Clean(strings.TrimSpace(root))
	path = filepath.Clean(strings.TrimSpace(path))
	if root == "" || path == "" {
		return false
	}
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func navigationFileSymbolOutline(root, query string) (string, bool) {
	path := navigationResolveFile(root, query)
	if path == "" {
		return "", false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 512<<10))
	if err != nil && len(data) == 0 {
		return "", false
	}
	rel := path
	if r, err := filepath.Rel(root, path); err == nil {
		rel = r
	}
	var preview strings.Builder
	var symbolLines []string
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lineNo := 0
	previewCount := 0
	for sc.Scan() {
		lineNo++
		raw := strings.TrimRight(sc.Text(), "\r")
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if previewCount < 40 {
			fmt.Fprintf(&preview, "%d: %s\n", lineNo, navigationTrimLine(trimmed, 180))
			previewCount++
		}
		if navigationLineLooksLikeSymbol(trimmed) {
			symbolLines = append(symbolLines, fmt.Sprintf("%d: %s", lineNo, navigationTrimLine(trimmed, 180)))
		}
	}
	if len(symbolLines) > 0 {
		shown := symbolLines
		if len(shown) > 80 {
			head := append([]string{}, shown[:40]...)
			head = append(head, "...")
			shown = append(head, shown[len(shown)-40:]...)
		}
		return filepath.ToSlash(rel) + "\n" + strings.Join(shown, "\n") + "\n", true
	}
	if previewCount == 0 {
		return "", false
	}
	return filepath.ToSlash(rel) + "\n" + preview.String(), true
}

func navigationTrimLine(line string, max int) string {
	if len([]rune(line)) <= max {
		return line
	}
	return string([]rune(line)[:max])
}

func navigationShouldOutlineFile(operation, query string) bool {
	switch operation {
	case "file_symbols", "node", "explore", "definition":
		return codegraphTargetIsFile(query)
	default:
		return false
	}
}

func navigationLineLooksLikeSymbol(line string) bool {
	trimmed := strings.TrimSpace(line)
	lower := strings.ToLower(trimmed)
	if trimmed == "" || strings.HasPrefix(lower, "//") || strings.HasPrefix(lower, "/*") || strings.HasPrefix(lower, "*") || strings.HasPrefix(lower, "#include") {
		return false
	}
	for _, prefix := range []string{"if ", "if(", "for ", "for(", "while ", "while(", "switch ", "switch(", "return ", "else "} {
		if strings.HasPrefix(lower, prefix) {
			return false
		}
	}
	if fields := strings.Fields(lower); len(fields) > 0 {
		switch fields[0] {
		case "func", "function", "class", "struct", "enum", "interface", "typedef", "#define", "def", "fn", "type":
			return true
		}
	}
	// Calls such as playTone(440); share parentheses with definitions.
	// A definition has a return type before the name and is not a statement.
	if strings.HasSuffix(lower, ";") || !strings.Contains(trimmed, "(") || !strings.Contains(trimmed, ")") {
		return false
	}
	open := strings.Index(trimmed, "(")
	head := strings.TrimSpace(trimmed[:open])
	if head == "" || strings.Contains(head, ".") || len(strings.Fields(head)) < 2 {
		return false
	}
	r, _ := utf8.DecodeRuneInString(trimmed)
	return unicode.IsLetter(r) || r == '_' || r == '~'
}

func (c *remoteCodingCallbacks) executeRemoteCodeNavigation(args map[string]interface{}) string {
	operation, query, depth, err := normalizeCodeNavigationArgs(args)
	if err != nil {
		return err.Error()
	}
	if c == nil || c.agent == nil || c.agent.handler == nil {
		return "remote code navigation unavailable"
	}
	question := navigationQuestion(operation, query, depth)
	file := strings.TrimSpace(fmt.Sprint(args["file"]))
	if file == "<nil>" {
		file = ""
	}
	line, _ := codingSubAgentArgumentIntegerValue(args["line"])
	column, _ := codingSubAgentArgumentIntegerValue(args["column"])
	// Claim the one-shot install before SSH. If the command never ran, release
	// the claim so a dropped connection can try again.
	bootstrap, releaseBootstrap := c.claimRemoteCodegraphBootstrap()
	// The call that is still running init owns a partial database. Another
	// call must not query that file until init has finished.
	allowQuery := remoteCodegraphQueryAllowed(c.agent.projectDir, bootstrap)
	var result string
	codegraphHit := false
	succeeded := false
	if allowQuery && c.remoteCodegraphIndexCachedOrProbe() {
		// The index is already usable. Release the init claim before the
		// daemon handshake, or every other lookup in this task skips
		// codegraph while this connection is opening.
		if bootstrap {
			releaseBootstrap(false)
			bootstrap = false
		}
		text, mcpErr := c.callRemoteCodegraphMCP(operation, query, question, file, int(line))
		if mcpErr == nil && strings.TrimSpace(text) != "" {
			result = text
			codegraphHit = true
			succeeded = true
		} else if mcpErr != nil {
			// A symbol miss is an empty result, not a dead index. Drop the
			// cache only when the proxy itself failed.
			remoteCodegraphForgetIndex(c.agent.projectDir)
		}
	}
	if result == "" {
		command := remoteCodeNavigationCommand(operation, query, question, file, int(line), int(column), bootstrap, allowQuery)
		result = c.sshBash(map[string]interface{}{"command": command, "working_dir": c.agent.projectDir})
		_, ran := remoteCodingParseAuthoritativeExitMarker(result)
		releaseBootstrap(ran)
		succeeded = remoteCodingToolOutcome(result) == "success"
		// The marker is its own line, checked before the output cap. A long result
		// must not lose it, and a source line that merely mentions the word must
		// not count as a hit. The status test after the marker keeps a failed
		// text search from being reported as success.
		result, codegraphHit = takeRemoteCodegraphHitMarker(result)
		if codegraphHit {
			remoteCodegraphRememberIndex(c.agent.projectDir)
		}
	}
	result, truncated := capCodeNavigationOutput(result)
	if succeeded {
		c.trackRemoteSearch(codeNavigationToolName, question, c.agent.projectDir, result, true)
	}
	hintBackend := "remote:auto"
	if codegraphHit {
		hintBackend = "codegraph"
	}
	payload, _ := json.Marshal(map[string]interface{}{"backend": "remote:auto", "operation": operation, "query": query, "output": result, "truncated": truncated, "continue_with": codegraphContinueHint(hintBackend), "candidates": localizationCandidatesFromOutput(result)})
	return string(payload)
}

// navigationQueryEscapesRoot reports a path code_navigation must not open.
// The remote shell's working directory is the project, so ".." and absolute
// paths would read outside it.
func navigationQueryEscapesRoot(query string) bool {
	query = strings.TrimSpace(query)
	if query == "" {
		return true
	}
	if strings.HasPrefix(query, "/") || strings.HasPrefix(query, `\`) || filepath.IsAbs(query) {
		return true
	}
	slash := strings.ReplaceAll(query, `\`, "/")
	for _, part := range strings.Split(slash, "/") {
		if part == ".." {
			return true
		}
	}
	return false
}

// remoteNavigationFileOutline locates a source file inside the project and
// prints its definitions. More than 80 symbols keep the first and last 40.
func remoteNavigationFileOutline(query string) string {
	base := remoteShellQuote(filepath.Base(filepath.FromSlash(strings.ReplaceAll(strings.TrimSpace(query), `\`, "/"))))
	locate := remoteShallowFileAssignment(base)
	if !navigationQueryEscapesRoot(query) {
		quoted := remoteShellQuote(query)
		locate = "if [ -f " + quoted + " ]; then _cg_file=" + quoted + "; else " + remoteShallowFileAssignment(base) + "; fi"
	}
	pattern := remoteShellQuote(navigationRemoteFileSymbolPattern)
	return locate + "; if [ -n \"$_cg_file\" ] && [ -f \"$_cg_file\" ]; then _cg_syms=$(grep -nE " + pattern + " \"$_cg_file\"); _cg_n=$(printf '%s\\n' \"$_cg_syms\" | grep -c '[^[:space:]]' || true); if [ \"$_cg_n\" -gt 80 ]; then _cg_head=$(printf '%s\\n' \"$_cg_syms\" | head -n 40); _cg_tail=$(printf '%s\\n' \"$_cg_syms\" | tail -n 40); printf '%s\\n...\\n%s\\n' \"$_cg_head\" \"$_cg_tail\"; elif [ -n \"$_cg_syms\" ]; then printf '%s\\n' \"$_cg_syms\"; else head -n 40 \"$_cg_file\"; fi; else "
}

func remoteShallowFileAssignment(quotedBase string) string {
	return "_cg_file=$(find . -maxdepth 5 -type f -iname " + quotedBase + " ! -path '*/.git/*' ! -path '*/node_modules/*' ! -path '*/build/*' ! -path '*/.codegraph/*' ! -path '*/.codegraph-*/*' ! -path \"*/${_cg_dir}/*\" | awk 'BEGIN{bestn=999}{n=split($0,a,\"/\"); if(n>0 && n<bestn){bestn=n; best=$0}} END{if(best!=\"\") print best}')"
}

// remoteCodeNavigationShell prefers codegraph explore/node. When
// allowBootstrap is set and the command is missing, it tries the global npm
// package once, then codegraph init if the project has no index. A missing
// binary, a failed command, or empty output falls through to gopls/lsp/rg/grep.
func remoteCodeNavigationShell(operation, query, question, file string, line, column int, allowBootstrap bool) string {
	return remoteCodeNavigationCommand(operation, query, question, file, line, column, allowBootstrap, true)
}

func remoteCodeNavigationCommand(operation, query, question, file string, line, column int, allowBootstrap, allowQuery bool) string {
	invocation := formatCodegraphRemoteInvocation(codegraphCommandList(operation, query, question, file))
	var b strings.Builder
	// Keep stdout in variables. File redirections such as >/dev/null are
	// rejected as shell writes before this command can run. npm's global bin
	// is often absent from a login PATH, so an existing install is also
	// resolved from `npm prefix -g` before any install is attempted.
	b.WriteString(remoteCodegraphDirAssignment())
	b.WriteString("_cg_hit=0; ")
	if allowQuery {
		b.WriteString("_cg_miss=; _cg_bin=; if _cg_found=$(command -v codegraph 2>&1); then _cg_bin=codegraph; elif _cg_found=$(command -v codegraph.cmd 2>&1); then _cg_bin=codegraph.cmd; elif _cg_found=$(command -v npm 2>&1); then _cg_prefix=$(npm prefix -g); if [ -x \"$_cg_prefix/bin/codegraph\" ]; then _cg_bin=\"$_cg_prefix/bin/codegraph\"; elif [ -f \"$_cg_prefix/codegraph.cmd\" ]; then _cg_bin=\"$_cg_prefix/codegraph.cmd\"; fi; ")
		if allowBootstrap {
			b.WriteString("if [ -z \"$_cg_bin\" ]; then _cg_install=$(npm install -g " + remoteShellQuote(codegraphNPMPackage) + " 2>&1) || true; if _cg_found=$(command -v codegraph 2>&1); then _cg_bin=codegraph; elif _cg_found=$(command -v codegraph.cmd 2>&1); then _cg_bin=codegraph.cmd; else _cg_prefix=$(npm prefix -g); if [ -x \"$_cg_prefix/bin/codegraph\" ]; then _cg_bin=\"$_cg_prefix/bin/codegraph\"; elif [ -f \"$_cg_prefix/codegraph.cmd\" ]; then _cg_bin=\"$_cg_prefix/codegraph.cmd\"; fi; fi; fi; ")
		}
		b.WriteString("fi; if [ -n \"$_cg_bin\" ]; then ")
		b.WriteString(remoteCodegraphHasDBFunc())
		if allowBootstrap {
			// A killed init leaves a non-empty database. status rejects that
			// fragment. A status check still running after 20s is not a
			// rejection: a large database can outlive the short check. The
			// background pid is a shell, and its node child holds the database,
			// so the whole process tree is stopped and the lock is cleared
			// before the index is queried. A parent index already covers this
			// directory; init runs only when nothing above it is indexed.
			b.WriteString(remoteCodegraphProcessHelpers())
			b.WriteString("if [ -s \"$_cg_dir/codegraph.db\" ]; then if _cg_db_ok; then :; else _cg_unlock=$(\"$_cg_bin\" unlock . 2>&1) || true; if _cg_db_ok; then :; else rm -f \"$_cg_dir/codegraph.db\" \"$_cg_dir/codegraph.db-wal\" \"$_cg_dir/codegraph.db-shm\" \"$_cg_dir/daemon.pid\"; fi; fi; fi; ")
			b.WriteString("if ! _cg_has_db; then _cg_init=$(\"$_cg_bin\" init . 2>&1) || true; fi; ")
		}
		b.WriteString("if _cg_has_db; then " + invocation + "; fi; fi; ")
	}
	b.WriteString("if [ \"$_cg_hit\" -eq 0 ]; then ")
	if codegraphTargetIsFile(query) && navigationShouldOutlineFile(operation, query) {
		// An exact relative path is outlined in place. A missing file is found
		// by basename inside the project. Absolute paths and ".." are not opened.
		b.WriteString(remoteNavigationFileOutline(query))
	}
	if file != "" && (operation == "definition" || operation == "references" || operation == "implementation") {
		b.WriteString("if _cg_found=$(command -v gopls 2>&1) && [ " + remoteShellQuote(strings.ToLower(filepath.Ext(file))) + " = .go ]; then gopls " + operation + " " + remoteShellQuote(fmt.Sprintf("%s:%d:%d", file, maxLocalizationInt(line, 1), maxLocalizationInt(column, 1))))
		b.WriteString("; elif _cg_found=$(command -v lsp-cli 2>&1); then lsp-cli " + remoteShellQuote(operation) + " --file " + remoteShellQuote(file) + " --line " + strconv.Itoa(maxLocalizationInt(line, 1)) + " --column " + strconv.Itoa(maxLocalizationInt(column, 1)) + " --query " + remoteShellQuote(query))
		b.WriteString("; elif ")
	} else {
		b.WriteString("if ")
	}
	rgPattern := query
	if !codegraphTargetIsFile(query) {
		rgPattern = navigationFallbackPattern(operation, query)
	}
	b.WriteString("_cg_found=$(command -v rg 2>&1); then rg -n --no-heading --color never -m 80 -- " + remoteShellQuote(rgPattern) + " .; else grep -RIn -m 80 -- " + remoteShellQuote(query) + " .; fi")
	if codegraphTargetIsFile(query) && navigationShouldOutlineFile(operation, query) {
		b.WriteString("; fi")
	}
	b.WriteString("; fi; _cg_rc=$?; if [ \"$_cg_hit\" -eq 1 ]; then printf '%s\\n' " + remoteCodegraphHitMarker + "; fi; [ \"$_cg_rc\" -eq 0 ]")
	return b.String()
}

var (
	codegraphBootstrapMu        sync.Mutex
	codegraphInstallFailed      bool
	codegraphIndexMu            sync.Mutex
	codegraphIndexLocks         = map[string]*sync.Mutex{}
	codegraphIndexBuilding      = map[string]bool{}
	codegraphIndexFailed        = map[string]bool{}
	remoteCodegraphBootstrapTry sync.Map
)

const remoteCodegraphBootstrapRunning = "running"

// claimRemoteCodegraphBootstrap lets the first writable call install and
// init. release(false) drops the claim when the remote command never ran.
func (c *remoteCodingCallbacks) claimRemoteCodegraphBootstrap() (bool, func(bool)) {
	noop := func(bool) {}
	if c == nil || c.agent == nil || !c.agent.codegraphBootstrapAllowed() {
		return false, noop
	}
	key := c.agent.projectDir
	if _, loaded := remoteCodegraphBootstrapTry.LoadOrStore(key, remoteCodegraphBootstrapRunning); loaded {
		return false, noop
	}
	return true, func(ran bool) {
		if !ran {
			remoteCodegraphBootstrapTry.Delete(key)
			return
		}
		remoteCodegraphBootstrapTry.Store(key, "done")
	}
}

// remoteCodegraphQueryAllowed reports whether this call may read the index.
// The call that claimed bootstrap runs init and then queries. Every other
// call waits until that init is no longer in progress, because the database
// file appears before it is complete.
func remoteCodegraphQueryAllowed(projectDir string, bootstrapClaimed bool) bool {
	if bootstrapClaimed {
		return true
	}
	state, ok := remoteCodegraphBootstrapTry.Load(projectDir)
	return !ok || state != remoteCodegraphBootstrapRunning
}

// codegraphBootstrapAllowed reports whether this remote turn may install
// codegraph or create .codegraph/. Read-only and command-only turns may query
// an index that already exists.
func (a *RemoteCodingSubAgent) codegraphBootstrapAllowed() bool {
	if a == nil || a.readOnlyInquiry || a.operationalRequest {
		return false
	}
	return a.nestDepth == 0 || (a.role != codingRoleExplorer && a.role != codingRoleReviewer)
}

func lookCodegraphBinary() string {
	if runtime.GOOS == "windows" {
		if bin := lookExecutable("codegraph.cmd", "codegraph.exe"); bin != "" {
			return bin
		}
	}
	return lookExecutable("codegraph", "codegraph.cmd", "codegraph.exe")
}

func lookExecutable(names ...string) string {
	for _, name := range names {
		if path, err := exec.LookPath(name); err == nil && path != "" {
			return path
		}
	}
	return ""
}

// ensureLocalCodegraphBinary installs @colbymchenry/codegraph when the command
// is missing and npm is available. A hard failure is not repeated. A timeout
// stops the npm process tree and can be retried; npm.cmd's node child would
// otherwise keep the install lock.
func ensureLocalCodegraphBinary() string {
	if bin := lookCodegraphBinary(); bin != "" {
		return bin
	}
	codegraphBootstrapMu.Lock()
	defer codegraphBootstrapMu.Unlock()
	if bin := lookCodegraphBinary(); bin != "" {
		return bin
	}
	npmBin := lookExecutable("npm", "npm.cmd")
	if npmBin != "" {
		if bin := codegraphBinaryBesideNPM(npmBin); bin != "" {
			return bin
		}
	}
	if codegraphInstallFailed {
		return ""
	}
	if npmBin == "" {
		codegraphInstallFailed = true
		log.Printf("[codegraph] install skipped: npm is not installed")
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := runCodegraphCommand(ctx, npmBin, "", "install", "-g", codegraphNPMPackage)
	if bin := lookCodegraphBinary(); bin != "" {
		return bin
	}
	if bin := codegraphBinaryBesideNPM(npmBin); bin != "" {
		return bin
	}
	if codegraphInstallFailureIsFinal(ctx.Err()) {
		codegraphInstallFailed = true
		log.Printf("[codegraph] npm install -g %s failed: %v %s", codegraphNPMPackage, err, compactCodingSubAgentLogText(out, 400))
	} else {
		log.Printf("[codegraph] npm install -g %s timed out; process tree stopped so the next call can retry: %s", codegraphNPMPackage, compactCodingSubAgentLogText(out, 200))
	}
	return ""
}

// codegraphInstallFailureIsFinal reports a finished npm error. A deadline
// means the install was interrupted and the next navigation call may try again.
func codegraphInstallFailureIsFinal(ctxErr error) bool {
	return !errors.Is(ctxErr, context.DeadlineExceeded)
}

func codegraphBinaryBesideNPM(npmPath string) string {
	dir := filepath.Dir(npmPath)
	names := []string{"codegraph", "codegraph.cmd", "codegraph.exe"}
	if runtime.GOOS == "windows" {
		names = []string{"codegraph.cmd", "codegraph.exe", "codegraph"}
	}
	for _, name := range names {
		candidate := filepath.Join(dir, name)
		info, err := os.Stat(candidate)
		if err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

// codegraphDataDirName is the index directory CodeGraph will use. CODEGRAPH_DIR
// may rename that single path segment (for example .codegraph-win). An unsafe
// value is ignored, matching the command's own rule.
func codegraphDataDirName() string {
	name := strings.TrimSpace(os.Getenv("CODEGRAPH_DIR"))
	if codegraphDataDirNameSafe(name) {
		return name
	}
	return ".codegraph"
}

func codegraphDataDirNameSafe(name string) bool {
	// Match codegraph's own rule: one path segment inside the project.
	// A quote is also rejected so the remote shell can quote the name.
	if name == "" || name == "." || strings.Contains(name, "..") || strings.ContainsAny(name, "/\\'\"\n\r") || filepath.IsAbs(name) {
		return false
	}
	return true
}

// codegraphIsDataDir reports an index directory. CodeGraph skips .codegraph,
// .codegraph-* siblings, and the active CODEGRAPH_DIR override.
func codegraphIsDataDir(name string) bool {
	if name == ".codegraph" || strings.HasPrefix(name, ".codegraph-") {
		return true
	}
	override := codegraphDataDirName()
	return override != ".codegraph" && name == override
}

// remoteCodegraphDirAssignment resolves the same directory name inside the
// remote shell. The value is a single safe path segment, never a path.
func remoteCodegraphDirAssignment() string {
	// Trim like the codegraph command, then keep one path segment. Spaces and
	// long names are valid. Quotes and separators are rejected through printf
	// so this script does not contain a raw quote the shell-write check would
	// treat as a redirection.
	return `_cg_dir=.codegraph; _cg_raw=${CODEGRAPH_DIR-}; while :; do case "$_cg_raw" in [[:space:]]*) _cg_raw=${_cg_raw#?} ;; *[[:space:]]) _cg_raw=${_cg_raw%?} ;; *) break ;; esac; done; case "$_cg_raw" in ''|.|..) ;; *..*) ;; *) if printf '%s' "$_cg_raw" | grep -q "$(printf '[\042\047/\134]')"; then :; else _cg_dir=$_cg_raw; fi ;; esac; `
}

// remoteCodegraphHasDBFunc is true when this directory or a parent already
// has a database. CodeGraph queries walk up to that parent, so a missing
// local file is not a reason to init a second index.
func remoteCodegraphHasDBFunc() string {
	return `_cg_has_db() { _cg_at=$(pwd); while :; do if [ -s "$_cg_at/$_cg_dir/codegraph.db" ]; then return 0; fi; _cg_up=$(dirname "$_cg_at"); if [ "$_cg_up" = "$_cg_at" ]; then return 1; fi; _cg_at=$_cg_up; done; }; `
}

// remoteCodegraphProcessHelpers stops a status check that outlives its budget.
// /proc sees the node child and a finished zombie; ps -o is only the fallback
// where /proc does not exist. kill -0 on a zombie stays true in some shells,
// which would ignore a failed status and keep a broken database.
func remoteCodegraphProcessHelpers() string {
	return `_cg_kill_tree() { if [ -d /proc ]; then for _cg_ent in /proc/[0-9]*; do _cg_ppid=$(sed -n 's/^PPid:[[:space:]]*//p' "$_cg_ent/status" 2>&1) || _cg_ppid=; if [ "$_cg_ppid" = "$1" ]; then _cg_kill_tree "${_cg_ent#/proc/}"; fi; done; else _cg_ps=$(ps -o pid=,ppid= 2>&1) || _cg_ps=; _cg_kids=$(printf '%s\n' "$_cg_ps" | awk -v p="$1" '$1 ~ /^[0-9]+$/ && $2 == p { print $1 }'); for _cg_kid in $_cg_kids; do _cg_kill_tree "$_cg_kid"; done; fi; _cg_sig=$(kill "$1" 2>&1) || true; }; ` +
		`_cg_running() { if [ -r "/proc/$1/status" ]; then _cg_state=$(sed -n 's/^State:[[:space:]]*\(.\).*/\1/p' "/proc/$1/status" 2>&1) || return 1; case "$_cg_state" in Z|z|"") return 1 ;; esac; return 0; fi; _cg_ping=$(kill -0 "$1" 2>&1); }; ` +
		`_cg_db_ok() { _cg_status=$("$_cg_bin" status 2>&1) & _cg_pid=$!; _cg_n=0; while [ "$_cg_n" -lt 20 ]; do if ! _cg_running "$_cg_pid"; then break; fi; sleep 1; _cg_n=$((_cg_n + 1)); done; if _cg_running "$_cg_pid"; then _cg_kill_tree "$_cg_pid"; _cg_wait=$(wait "$_cg_pid" 2>&1) || true; _cg_unlock=$("$_cg_bin" unlock . 2>&1) || true; return 0; fi; wait "$_cg_pid"; }; `
}

// codegraphIndexUsable reports a database that no init is currently writing.
func codegraphIndexUsable(root string) bool {
	if !codegraphIndexReady(root) {
		return false
	}
	key := codegraphIndexFailureKey(root)
	codegraphIndexMu.Lock()
	building := key != "" && codegraphIndexBuilding[key]
	codegraphIndexMu.Unlock()
	return !building
}

func codegraphMarkIndexBuilding(root string, building bool) {
	key := codegraphIndexFailureKey(root)
	if key == "" {
		return
	}
	codegraphIndexMu.Lock()
	if building {
		codegraphIndexBuilding[key] = true
	} else {
		delete(codegraphIndexBuilding, key)
	}
	codegraphIndexMu.Unlock()
}

// codegraphLockProject serializes init for one project. A different project
// can init at the same time.
func codegraphLockProject(root string) func() {
	key := codegraphIndexFailureKey(root)
	if key == "" {
		return func() {}
	}
	codegraphIndexMu.Lock()
	mu := codegraphIndexLocks[key]
	if mu == nil {
		mu = &sync.Mutex{}
		codegraphIndexLocks[key] = mu
	}
	codegraphIndexMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func codegraphIndexReady(root string) bool {
	root = strings.TrimSpace(root)
	if root == "" {
		return false
	}
	// A cloned tree can contain .codegraph/.gitignore with no database.
	// CodeGraph itself treats the project as initialized only when codegraph.db exists.
	info, err := os.Stat(filepath.Join(root, codegraphDataDirName(), "codegraph.db"))
	return err == nil && info.Mode().IsRegular() && info.Size() > 0
}

// codegraphNearestIndexRoot is the directory CodeGraph will query. The project
// itself wins. While this process is writing that database, there is no usable
// index yet. Otherwise the nearest parent database is the one queries walk up to.
func codegraphNearestIndexRoot(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	root = filepath.Clean(root)
	if codegraphIndexUsable(root) {
		return root
	}
	if !filepath.IsAbs(root) || codegraphIndexIsBuilding(root) {
		return ""
	}
	current := root
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return ""
		}
		if codegraphIndexReady(parent) {
			return parent
		}
		current = parent
	}
}

func codegraphIndexIsBuilding(root string) bool {
	key := codegraphIndexFailureKey(root)
	if key == "" {
		return false
	}
	codegraphIndexMu.Lock()
	building := codegraphIndexBuilding[key]
	codegraphIndexMu.Unlock()
	return building
}

// ensureLocalCodegraphIndex builds .codegraph/ once per project when the
// command is already installed. A failed init is not repeated for that root.
func ensureLocalCodegraphIndex(root, bin string) bool {
	root = strings.TrimSpace(root)
	if root == "" || strings.TrimSpace(bin) == "" {
		return false
	}
	if codegraphIndexUsable(root) {
		return true
	}
	unlockProject := codegraphLockProject(root)
	defer unlockProject()
	if codegraphIndexUsable(root) {
		return true
	}
	if codegraphIndexAttemptFailed(root) {
		return false
	}
	// Publish the build before init creates the file. A non-empty database
	// appears while node is still writing it; other calls must not query or
	// delete that file until this build releases it.
	codegraphMarkIndexBuilding(root, true)
	defer codegraphMarkIndexBuilding(root, false)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	out, err := runCodegraphCommand(ctx, bin, root, "init", ".")
	// A timeout can be reported after init has already committed the database.
	// Keep that index. Only a database status rejects is a fragment to delete.
	ready := codegraphIndexReady(root)
	statusOK := false
	if ready && err != nil {
		// A killed init leaves daemon.pid. status cannot accept a finished
		// database until that lock is gone, and a lock error must not delete it.
		codegraphUnlockPartialIndex(root, bin)
		statusOK = codegraphIndexAcceptsQueries(root, bin)
	}
	if codegraphInitResultUsable(err, ready, statusOK) {
		return true
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		// The process tree, including node started by codegraph.cmd, is already
		// stopped. Drop the fragment it was writing so the next call can init.
		codegraphRemovePartialIndex(root)
		log.Printf("[codegraph] init timed out for %s; partial index removed so the next call can retry: %s", root, compactCodingSubAgentLogText(out, 200))
		return false
	}
	codegraphRemovePartialIndex(root)
	codegraphRememberIndexFailure(root)
	log.Printf("[codegraph] init failed for %s: %v %s", root, err, compactCodingSubAgentLogText(out, 400))
	return false
}

// codegraphInitResultUsable reports whether the database left by init can be
// queried. A clean exit is enough. A cancelled exit still counts when status
// accepts the file, because the wait can lose the race with process exit.
func codegraphInitResultUsable(commandErr error, ready, statusOK bool) bool {
	if !ready {
		return false
	}
	return commandErr == nil || statusOK
}

// codegraphIndexAcceptsQueries asks codegraph status whether the database is
// readable. A non-empty fragment of a killed init still looks like a database
// file, and status is what rejects it.
func codegraphIndexAcceptsQueries(root, bin string) bool {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(bin) == "" || !codegraphIndexReady(root) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	_, err := runCodegraphCommand(ctx, bin, root, "status")
	return codegraphIndexStatusKeepsDatabase(err, ctx.Err())
}

// codegraphIndexStatusKeepsDatabase keeps the file when status accepts it.
// A status check that only runs out of time is not a rejection: a large
// database can outlive the short check, and deleting it would force another init.
func codegraphIndexStatusKeepsDatabase(statusErr, statusCtxErr error) bool {
	if statusErr == nil && statusCtxErr == nil {
		return true
	}
	return errors.Is(statusCtxErr, context.DeadlineExceeded)
}

// runCodegraphCommand runs one codegraph invocation and stops its process tree
// when ctx ends. codegraph.cmd starts node; killing only the batch host leaves
// that child holding the index lock.
func runCodegraphCommand(ctx context.Context, bin, root string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	cmd.Dir = root
	hideCommandWindow(cmd)
	tool.PrepareCommandForTreeKill(cmd)
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		return "", err
	}
	err := tool.WaitCommandWithContext(ctx, cmd)
	return output.String(), err
}

func codegraphUnlockPartialIndex(root, bin string) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _ = runCodegraphCommand(ctx, bin, root, "unlock", ".")
}

func codegraphRemovePartialIndex(root string) {
	base := filepath.Join(root, codegraphDataDirName())
	for _, name := range []string{"codegraph.db", "codegraph.db-wal", "codegraph.db-shm", "daemon.pid"} {
		_ = os.Remove(filepath.Join(base, name))
	}
}

func codegraphIndexFailureKey(root string) string {
	root = strings.TrimSpace(root)
	if root == "" {
		return ""
	}
	return filepath.Clean(root)
}

func codegraphIndexAttemptFailed(root string) bool {
	key := codegraphIndexFailureKey(root)
	if key == "" {
		return false
	}
	codegraphIndexMu.Lock()
	defer codegraphIndexMu.Unlock()
	return codegraphIndexFailed[key]
}

func codegraphRememberIndexFailure(root string) {
	key := codegraphIndexFailureKey(root)
	if key == "" {
		return
	}
	codegraphIndexMu.Lock()
	codegraphIndexFailed[key] = true
	codegraphIndexMu.Unlock()
}

func capCodeNavigationOutput(output string) (string, bool) {
	const maxBytes = 24000
	if len(output) <= maxBytes {
		return output, false
	}
	cut := maxBytes
	for cut > 0 && cut < len(output) && !utf8.RuneStart(output[cut]) {
		cut--
	}
	if cut <= 0 {
		cut = maxBytes
	}
	return output[:cut], true
}

func maxLocalizationInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func localizationFocusedTestSuggestions(rootCauseFile, symbol string) []string {
	file := filepath.ToSlash(strings.TrimSpace(rootCauseFile))
	if file == "" {
		return nil
	}
	ext := strings.ToLower(filepath.Ext(file))
	dir := filepath.ToSlash(filepath.Dir(file))
	base := strings.TrimSuffix(filepath.Base(file), filepath.Ext(file))
	var out []string
	switch ext {
	case ".go":
		if symbol != "" {
			out = append(out, "go test ./"+dir+" -run "+regexp.QuoteMeta(symbol))
		}
		out = append(out, "go test ./"+dir)
	case ".py":
		out = append(out, "pytest "+dir+" -k "+strconv.Quote(symbol))
	case ".ts", ".tsx", ".js", ".jsx":
		out = append(out, "npm test -- "+base)
	case ".rs":
		out = append(out, "cargo test "+symbol)
	}
	return uniqueSortedSubAgentStrings(out)
}

func persistLocalizationExperience(app *App, store *knowledge.CodingKnowledgeStore, projectPath, taskTitle string, e *CodingSubAgentLocalizationEvidence, commands []CodingSubAgentCommandResult, runtimeTaskID string) {
	if store == nil || e == nil || validateLocalizationEvidence(e, "") != nil {
		return
	}
	runtimeTaskID = strings.TrimSpace(runtimeTaskID)
	if strings.HasPrefix(runtimeTaskID, "horizon:") || strings.HasPrefix(runtimeTaskID, "horizon-") {
		return
	}
	if len(e.FocusedTests) == 0 {
		e.FocusedTests = localizationFocusedTestSuggestions(e.RootCauseFile, e.RootCauseSymbol)
	}
	failed := make([]string, 0)
	for _, c := range commands {
		if !c.Succeeded {
			failed = append(failed, truncateRunesForSubAgent(c.Command+": "+c.Summary, 300))
		}
	}
	content, _ := json.Marshal(e)
	exp := knowledge.CodingExperience{
		Title:            "Bug localization: " + truncateRunesForSubAgent(taskTitle, 120),
		Category:         knowledge.CodingCategoryPitfall,
		Scope:            knowledge.CodingScopeProject,
		ProjectPath:      projectPath,
		TriggerCondition: strings.Join(append(append([]string{}, e.Signal.ErrorStrings...), e.Signal.StackFrames...), " "),
		Content:          string(content),
		FailedAttempts:   append(failed, e.RejectedHypotheses...),
		Labels:           []string{"bug-localization", "root-cause", filepath.ToSlash(e.RootCauseFile)},
		SourceTaskTitle:  taskTitle,
		CreatedBy:        "runtime",
		// Agent-produced localization is reusable guidance, not a reviewed rule.
		// Keep it staged even when the execution evidence was strong.
		Status:     knowledge.CodingStatusCandidate,
		Confidence: 0.8 + e.Confidence*0.4,
	}
	if app == nil {
		// Keep legacy/direct callers from silently writing a model-generated
		// active rule when they do not execute under the durable Runtime.
		log.Printf("[coding-localization] skip automatic experience without runtime application binding")
		return
	}
	provenance, err := codingExperienceRuntimeProvenance(app, runtimeTaskID)
	if err != nil {
		log.Printf("[coding-localization] skip automatic experience without runtime provenance: %v", err)
		return
	}
	exp.SourceRuntimeTaskID = provenance.TaskID
	exp.SourceRuntimeAttemptID = provenance.AttemptID
	exp.EvidenceDigest = provenance.EvidenceDigest
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = store.SaveRuntimeExperience(ctx, exp)
}
