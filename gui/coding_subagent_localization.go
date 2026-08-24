package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/knowledge"
)

const (
	codeNavigationToolName     = "code_navigation"
	reportLocalizationToolName = "report_localization"
)

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
	lower := strings.ToLower(strings.TrimSpace(text))
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
	for _, marker := range []string{"错误", "异常", "崩溃", "故障", "修复", "不正确", "失效", "失败", "定位", "根因"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
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
	trimmed := strings.TrimSpace(result)
	lower := strings.ToLower(trimmed)
	if lower == "" {
		return true
	}
	for _, marker := range []string{
		"web_fetch unavailable", "web_fetch failed", "fetch failed", "request failed",
		"timed out", "timeout", "抓取失败", "缺少 url 参数",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	// A successful page extraction contains enough title/body material to audit.
	// Tiny acknowledgements such as "ok" or a bare URL do not prove that the
	// declared source was actually read.
	if utf8.RuneCountInString(trimmed) < 24 {
		return true
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

func validateLocalizationEvidence(e *CodingSubAgentLocalizationEvidence, expectedPath string) error {
	if e == nil {
		return fmt.Errorf("missing localization evidence")
	}
	normalized := normalizeLocalizationEvidence(*e)
	e = &normalized
	if err := validateLocalizationEvidencePath("root_cause_file", e.RootCauseFile); err != nil {
		return err
	}
	if expectedPath != "" && !localizationEvidenceCoversPath(e, expectedPath) {
		return fmt.Errorf("root cause/candidates do not cover edit target %q", expectedPath)
	}
	if len(e.CausalPath) == 0 {
		return fmt.Errorf("causal_path must contain at least one symptom-to-cause step")
	}
	if strings.TrimSpace(e.Reproduction) == "" {
		return fmt.Errorf("reproduction evidence or an explicit reason it is unavailable is required")
	}
	if len(e.SupportingEvidence) == 0 {
		return fmt.Errorf("supporting_evidence is required")
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
	switch strings.ToLower(strings.TrimSpace(e.ResearchDecision)) {
	case "searched":
		if strings.TrimSpace(e.ResearchReason) == "" {
			return fmt.Errorf("research_reason is required when external research was used")
		}
		if len(e.ExternalSources) == 0 {
			return fmt.Errorf("external_sources is required when research_decision is searched")
		}
	case "not_needed", "unavailable":
		if strings.TrimSpace(e.ResearchReason) == "" {
			return fmt.Errorf("research_reason must explain why web research was not used")
		}
	default:
		return fmt.Errorf("research_decision must be searched, not_needed, or unavailable")
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
		"Structured code navigation. Prefer this over text search for symbols, definitions, references, callers, callees, implementations, and file symbols. Uses CodeGraph first and falls back to indexed text search.",
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
		"Submit structured root-cause evidence before editing an existing file for a bug fix. The report becomes an enforced audit gate.",
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
	text := ""
	if c.task != nil {
		text = c.task.Title + "\n" + c.task.Description
	}
	e, err := localizationEvidenceFromArgs(args, extractCodingSubAgentBugSignal(text))
	if err != nil {
		log.Printf("[coding-localization] report rejected stage=evidence task=%d error=%q", taskDisplayNumber(c.task), compactCodingSubAgentLogText(err.Error(), 500))
		return codingToolExecutionResult{Text: "invalid localization evidence: " + err.Error(), Outcome: codingToolOutcomeFailed}
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
	return codingToolExecutionResult{Text: fmt.Sprintf("localization evidence accepted (control_plane_revision=%d)\n%s", revision, string(out)), Outcome: codingToolOutcomeSuccess}
}

func (c *codingSubAgentCallbacks) requireLocalizationBeforeExistingBugEdit(path string, created bool) string {
	if c == nil || created || c.task == nil || (c.subagent != nil && c.subagent.horizonPosture) || !codingTaskNeedsLocalization(c.task.Title+"\n"+c.task.Description) {
		return ""
	}
	e := c.localizationForCurrentControlPlaneRevision()
	if err := validateLocalizationEvidence(e, c.displayProjectPath(path)); err != nil {
		log.Printf("[coding-localization] edit blocked stage=evidence task=%d path=%q error=%q",
			taskDisplayNumber(c.task), compactCodingSubAgentLogText(c.displayProjectPath(path), 300), compactCodingSubAgentLogText(err.Error(), 500))
		return "bug-fix edit blocked: submit report_localization with root-cause evidence before modifying an existing file: " + err.Error()
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
	e, err := localizationEvidenceFromArgs(args, extractCodingSubAgentBugSignal(c.task+"\n"+c.taskContext))
	if err != nil {
		log.Printf("[remote-localization] report rejected stage=evidence project=%q error=%q",
			remoteLocalizationLogProject(c), compactCodingSubAgentLogText(err.Error(), 500))
		return "invalid localization evidence: " + err.Error()
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
	return fmt.Sprintf("localization evidence accepted (control_plane_revision=%d)\n%s", revision, string(out))
}

func (c *remoteCodingCallbacks) requireRemoteLocalizationBeforeBugEdit(args map[string]interface{}, definitelyExisting bool) string {
	if c == nil || !codingTaskNeedsLocalization(c.task+"\n"+c.taskContext) {
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
	if !definitelyExisting && c.knownExisting != nil && !c.knownExisting[remoteCleanPath(c.resolvePath(path))] {
		log.Printf("[remote-localization] edit exempt reason=new_file project=%q path=%q",
			remoteLocalizationLogProject(c), compactCodingSubAgentLogText(path, 300))
		return ""
	}
	if !definitelyExisting && e == nil {
		log.Printf("[remote-localization] edit blocked stage=existence project=%q path=%q error=%q",
			remoteLocalizationLogProject(c), compactCodingSubAgentLogText(path, 300), "target existence is unknown")
		return "bug-fix write blocked: use ssh_read_file/code_navigation to determine whether the target exists, then submit report_localization before rewriting existing code"
	}
	if err := validateLocalizationEvidence(e, path); err != nil {
		log.Printf("[remote-localization] edit blocked stage=evidence project=%q path=%q error=%q",
			remoteLocalizationLogProject(c), compactCodingSubAgentLogText(path, 300), compactCodingSubAgentLogText(err.Error(), 500))
		return "bug-fix edit blocked: submit report_localization with root-cause evidence before modifying remote code: " + err.Error()
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
	Backend   string `json:"backend"`
	Operation string `json:"operation"`
	Query     string `json:"query"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated,omitempty"`
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
	backend, output, runErr := runLocalCodeNavigationWithPosition(root, operation, query, file, int(line), int(column), depth)
	if runErr != nil {
		return codingToolExecutionResult{Text: runErr.Error(), Outcome: codingToolOutcomeFailed}
	}
	truncated := false
	if len(output) > 24000 {
		output = output[:24000]
		truncated = true
	}
	payload, _ := json.Marshal(codeNavigationOutput{Backend: backend, Operation: operation, Query: query, Output: output, Truncated: truncated})
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

func runLocalCodeNavigation(root, operation, query string, depth int) (string, string, error) {
	return runLocalCodeNavigationWithPosition(root, operation, query, "", 0, 0, depth)
}

func runLocalCodeNavigationWithPosition(root, operation, query, file string, line, column, depth int) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if info, err := os.Stat(filepath.Join(root, ".codegraph")); err == nil && info.IsDir() {
		bin := "codegraph"
		if _, err := exec.LookPath("codegraph.cmd"); err == nil {
			bin = "codegraph.cmd"
		}
		verb := "explore"
		arg := navigationQuestion(operation, query, depth)
		if operation == "node" || operation == "file_symbols" {
			verb = "node"
			arg = query
		}
		cmd := exec.CommandContext(ctx, bin, verb, arg)
		cmd.Dir = root
		hideCommandWindow(cmd)
		data, err := cmd.CombinedOutput()
		if err == nil && len(strings.TrimSpace(string(data))) > 0 {
			return "codegraph", string(data), nil
		}
	}
	if backend, output, ok := runLocalLSPNavigation(ctx, root, operation, query, file, line, column); ok {
		return backend, output, nil
	}
	return runLocalNavigationTextFallback(ctx, root, operation, query)
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
	rg := "rg"
	if _, err := exec.LookPath(rg); err != nil {
		return "", "", fmt.Errorf("code navigation unavailable: no CodeGraph index and rg is not installed")
	}
	pattern := regexp.QuoteMeta(query)
	if operation == "definition" || operation == "workspace_symbol" || operation == "file_symbols" {
		pattern = `(?i)(func|function|class|type|interface|struct|def|const|var|let)\s+.*` + pattern
	}
	cmd := exec.CommandContext(ctx, rg, "-n", "--no-heading", "--color", "never", "-m", "80", pattern, ".")
	cmd.Dir = root
	hideCommandWindow(cmd)
	data, err := cmd.CombinedOutput()
	if err != nil && len(data) == 0 {
		return "ripgrep", "", fmt.Errorf("no navigation matches for %q", query)
	}
	return "ripgrep", string(data), nil
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
	command := "if [ -d .codegraph ] && command -v codegraph >/dev/null 2>&1; then "
	if operation == "node" || operation == "file_symbols" {
		command += "codegraph node " + remoteShellQuote(query)
	} else {
		command += "codegraph explore " + remoteShellQuote(question)
	}
	if file != "" && (operation == "definition" || operation == "references" || operation == "implementation") {
		command += "; elif command -v gopls >/dev/null 2>&1 && [ " + remoteShellQuote(strings.ToLower(filepath.Ext(file))) + " = .go ]; then gopls " + operation + " " + remoteShellQuote(fmt.Sprintf("%s:%d:%d", file, maxLocalizationInt(int(line), 1), maxLocalizationInt(int(column), 1)))
		command += "; elif command -v lsp-cli >/dev/null 2>&1; then lsp-cli " + remoteShellQuote(operation) + " --file " + remoteShellQuote(file) + " --line " + strconv.Itoa(maxLocalizationInt(int(line), 1)) + " --column " + strconv.Itoa(maxLocalizationInt(int(column), 1)) + " --query " + remoteShellQuote(query)
	}
	command += "; elif command -v rg >/dev/null 2>&1; then rg -n --no-heading --color never -m 80 -- " + remoteShellQuote(query) + " .; else grep -RIn -m 80 -- " + remoteShellQuote(query) + " .; fi"
	result := c.sshBash(map[string]interface{}{"command": command, "working_dir": c.agent.projectDir})
	if remoteCodingToolOutcome(result) == "success" {
		c.trackRemoteSearch(codeNavigationToolName, question, c.agent.projectDir, result, true)
	}
	payload, _ := json.Marshal(map[string]interface{}{"backend": "remote:auto", "operation": operation, "query": query, "output": result, "candidates": localizationCandidatesFromOutput(result)})
	return string(payload)
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
