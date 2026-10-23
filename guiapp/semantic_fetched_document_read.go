package guiapp

import (
	"strings"
	"unicode/utf8"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/intent"
)

const fetchedDocumentReadNote = "fetched document read"

// fetchedDocumentFormatTokens are the non-plaintext extensions the document
// reader parses. Longer tokens come first so "docx" is not read as "doc".
// txt/md/json/log stay out: those are paged text reads, not this grant.
var fetchedDocumentFormatTokens = []string{
	"asciidoc", "docx", "xlsx", "pptx", "dxfb",
	"dwg", "dxf", "pdf", "doc", "xls", "ppt", "csv",
	"epub", "gltf", "glb", "adoc", "eml",
}

// fetchedDocumentBodyReadCues are requests to read the body of a named file.
// A format token alone ("dwg 是什么", "pdf 转 word", "生成pdf") is not one.
var fetchedDocumentBodyReadCues = []string{
	"理解其中",
	"其中的内容",
	"读出内容",
	"读取内容",
	"读一下内容",
	"读一读内容",
	"读懂内容",
	"看看内容",
	"文件内容",
	"图纸内容",
	"里面写了",
	"里面有什么",
	"真读",
	"read its content",
	"read the content",
	"understand its content",
	"understand the content",
	"what's inside",
	"whats inside",
	"what is inside",
}

// fetchedDocumentNegationPhrases reject a clause. Longer phrases come first.
// A later clause can still ask for the file ("不要读 pdf，还是搜索 dwg 理解其中").
var fetchedDocumentNegationPhrases = []string{
	"无需读取", "不要读取", "不用读取", "别读取",
	"不要下载", "不用下载", "别下载", "无需下载",
	"不要搜索", "不用搜索", "别搜索", "不搜索",
	"不要读", "不用读", "别读",
	"do not download", "don't download", "dont download",
	"do not search", "don't search", "dont search",
	"do not read", "don't read", "dont read",
}

// queryWantsFetchedDocumentRead is true when the utterance asks to fetch a
// reader format and read its body. Picker paths and OCR notes are not the
// request. A negated clause is dropped, so a spaced "不要 读" does not match
// and a later positive clause still can.
func queryWantsFetchedDocumentRead(text string) bool {
	text = fetchedDocumentActionText(fetchedDocumentReadUserText(text))
	return fetchedDocumentNamesFormat(text) &&
		fetchedDocumentBodyReadCue(text) &&
		utteranceAsksToFetchDocument(text)
}

// fetchedDocumentReadUserText is the same lexical view the planner uses:
// the ACP user-request body, without host file-picker or OCR annotations.
func fetchedDocumentReadUserText(text string) string {
	// The picker block is host-appended. A .dwg path there is a file the user
	// already has, not a format they asked to fetch.
	return strings.TrimSpace(semanticUserIntentText(stripHostAttachmentSections(acpInnerUserRequest(text))))
}

// fetchedDocumentActionText keeps clauses that do not reject the read,
// the download, or the search.
func fetchedDocumentActionText(text string) string {
	parts := splitFetchedDocumentClauses(text)
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" || fetchedDocumentClauseNegated(part) {
			continue
		}
		kept = append(kept, part)
	}
	return strings.Join(kept, " ")
}

func splitFetchedDocumentClauses(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		switch r {
		case '，', '。', '；', ';', '！', '？', '!', '?', '\n', '、':
			return true
		default:
			return false
		}
	})
}

func fetchedDocumentClauseNegated(clause string) bool {
	lower := strings.ToLower(clause)
	compact := compactFetchedDocumentSpaces(lower)
	for _, phrase := range fetchedDocumentNegationPhrases {
		if strings.Contains(compact, compactFetchedDocumentSpaces(phrase)) {
			return true
		}
	}
	return false
}

func compactFetchedDocumentSpaces(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch r {
		case ' ', '\t', '\n', '\r', '\v', '\f', '\u00a0', '\u3000':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func fetchedDocumentNamesFormat(text string) bool {
	folded := foldFetchedDocumentScan(text)
	for _, token := range fetchedDocumentFormatTokens {
		if foldedHasBoundedToken(folded, token) {
			return true
		}
	}
	return false
}

func fetchedDocumentBodyReadCue(text string) bool {
	lower := strings.ToLower(text)
	for _, cue := range fetchedDocumentBodyReadCues {
		if cue == "真读" {
			if containsZhenDu(text) {
				return true
			}
			continue
		}
		if strings.Contains(text, cue) || strings.Contains(lower, cue) {
			return true
		}
	}
	return false
}

// containsZhenDu matches 真读 without treating 认真读书 as a read request.
func containsZhenDu(text string) bool {
	const cue = "真读"
	rest := text
	for {
		i := strings.Index(rest, cue)
		if i < 0 {
			return false
		}
		if i == 0 {
			return true
		}
		prev, _ := utf8.DecodeLastRuneInString(rest[:i])
		if prev != '认' {
			return true
		}
		rest = rest[i+len(cue):]
	}
}

// shortFetchedDocumentReadCue is a follow-up such as 「真读内容」 that only
// means "read it" once earlier user turns name the file.
func shortFetchedDocumentReadCue(text string) bool {
	text = fetchedDocumentActionText(strings.TrimSpace(text))
	if text == "" || utf8.RuneCountInString(text) > 24 {
		return false
	}
	if fetchedDocumentNamesFormat(text) {
		return false
	}
	return fetchedDocumentBodyReadCue(text)
}

func fetchedDocumentReadEvidence(userText string, history []agent.ConversationEntry) string {
	current := fetchedDocumentReadUserText(userText)
	if queryWantsFetchedDocumentRead(current) {
		return current
	}
	if !shortFetchedDocumentReadCue(current) {
		return ""
	}
	priors := recentUserTaskTexts(history, 2)
	cleaned := make([]string, 0, len(priors))
	for _, prior := range priors {
		if text := fetchedDocumentReadUserText(prior); text != "" {
			cleaned = append(cleaned, text)
		}
	}
	merged := mergeTaskContextText(current, cleaned)
	if merged == "" || !queryWantsFetchedDocumentRead(merged) {
		return ""
	}
	return merged
}

// utteranceAsksToFetchDocument is the web-search or download half. A rejected
// "不搜索" / "不要下载" is removed first, so the leftover word does not count.
func utteranceAsksToFetchDocument(text string) bool {
	compact := compactFetchedDocumentSpaces(text)
	for _, phrase := range []string{
		"不要搜索", "不用搜索", "别搜索", "不搜索",
		"不要下载", "不用下载", "别下载", "无需下载",
	} {
		compact = strings.ReplaceAll(compact, phrase, "")
	}
	if strings.Contains(compact, "搜索") || strings.Contains(compact, "下载") {
		return true
	}
	// Keep spaces. Compacting first would glue "search" onto the next word
	// and the boundary check would miss it.
	lower := strings.ToLower(text)
	for _, phrase := range []string{
		"do not search", "don't search", "dont search",
		"do not download", "don't download", "dont download",
	} {
		lower = strings.ReplaceAll(lower, phrase, " ")
	}
	folded := foldFetchedDocumentScan(lower)
	return foldedHasBoundedToken(folded, "search") || foldedHasBoundedToken(folded, "download")
}

// semanticFetchedDocumentReadPlanning adds download_file and read_file to a
// lookup whose evidence, including a short follow-up merged with the two
// prior user turns, asks to fetch a document and read its body.
func semanticFetchedDocumentReadPlanning(current *intent.ClassificationResult, userText string, history []agent.ConversationEntry) *intent.ClassificationResult {
	evidence := fetchedDocumentReadEvidence(userText, history)
	if evidence == "" {
		return nil
	}
	return semanticFetchedDocumentReadRewrite(current, evidence)
}

// semanticFetchedDocumentReadRewrite keeps search as the primary and appends
// file_download plus file_read. It does not grant document_read, write, or
// shell. Nil means the caller must leave the classification alone.
// A retarget is returned even when the two labels were already present:
// dropping that clone left file_read as primary, and its archetype bundle
// is file_write.
func semanticFetchedDocumentReadRewrite(current *intent.ClassificationResult, evidence string) *intent.ClassificationResult {
	if !queryWantsFetchedDocumentRead(evidence) {
		return nil
	}
	base := cloneFetchedDocumentClassification(current)
	if base.WorkflowType != "" || base.ControlPlaneFailure {
		return nil
	}
	before := base.Primary
	hadAttachmentRead := base.HasLabel(intent.LabelDocumentRead)
	if !allowFetchedDocumentReadPrimary(&base, evidence) {
		return nil
	}
	stripFetchedDocumentAttachmentRead(&base)
	if fetchedDocumentReadAlreadySelected(base) && base.Primary == before && !hadAttachmentRead {
		return nil
	}
	if !fetchedDocumentReadAlreadySelected(base) {
		appendIntentSecondary(&base, intent.LabelFileDownload)
		appendIntentSecondary(&base, intent.LabelFileRead)
		if base.Degraded || base.Confidence < imSemanticMinimumConfidence {
			base.Degraded = false
			if base.Confidence < imSemanticMinimumConfidence {
				base.Confidence = imSemanticMinimumConfidence
			}
		}
	}
	if base.Reason == "" {
		base.Reason = fetchedDocumentReadNote
	} else if !strings.Contains(base.Reason, fetchedDocumentReadNote) {
		base.Reason = strings.TrimSpace(base.Reason + "; " + fetchedDocumentReadNote)
	}
	return &base
}

func allowFetchedDocumentReadPrimary(result *intent.ClassificationResult, evidence string) bool {
	// Same lexical view as queryWants. A picker path must not count as the
	// fetch, and a local "read report.dwg" classified as search must not
	// become a remote download.
	if !utteranceAsksToFetchDocument(fetchedDocumentActionText(fetchedDocumentReadUserText(evidence))) {
		return false
	}
	switch result.Primary {
	case intent.LabelSearch, intent.LabelWebFetch:
		return true
	case intent.LabelFileRead, intent.LabelFileDownload, intent.LabelDocumentRead:
		// file_read's bundle is file_write. file_download as primary makes
		// the workspace baseline add bash once file_read is attached.
		// document_read is removed after the retarget: it reads one uploaded
		// attachment and fail-closes when the turn has none.
		retargetFetchedDocumentPrimary(result)
		return true
	default:
		if !result.Primary.IsNonCapabilityLabel() {
			return false
		}
		result.Primary = intent.LabelSearch
		dedupeIntentSecondaries(result)
		return true
	}
}

// stripFetchedDocumentAttachmentRead drops document.read.local. The fetched
// file is a workspace path from the download receipt, not an ingress attachment.
func stripFetchedDocumentAttachmentRead(result *intent.ClassificationResult) {
	if result == nil || !result.HasLabel(intent.LabelDocumentRead) {
		return
	}
	if result.Primary == intent.LabelDocumentRead {
		result.Primary = intent.LabelSearch
	}
	kept := make([]intent.IntentLabel, 0, len(result.Secondary))
	for _, label := range result.Secondary {
		if label == intent.LabelDocumentRead {
			continue
		}
		kept = append(kept, label)
	}
	result.Secondary = kept
	dedupeIntentSecondaries(result)
}

func retargetFetchedDocumentPrimary(result *intent.ClassificationResult) {
	previous := result.Primary
	result.Primary = intent.LabelSearch
	if previous != "" && previous != intent.LabelSearch && !previous.IsNonCapabilityLabel() {
		appendIntentSecondary(result, previous)
	}
	dedupeIntentSecondaries(result)
}

func fetchedDocumentReadAlreadySelected(result intent.ClassificationResult) bool {
	if result.Primary != intent.LabelSearch && result.Primary != intent.LabelWebFetch {
		return false
	}
	return !result.Degraded &&
		result.Confidence >= imSemanticMinimumConfidence &&
		result.HasLabel(intent.LabelFileDownload) &&
		result.HasLabel(intent.LabelFileRead)
}

func appendIntentSecondary(result *intent.ClassificationResult, label intent.IntentLabel) {
	if result == nil || label == "" || result.HasLabel(label) {
		return
	}
	result.Secondary = append(result.Secondary, label)
}

func dedupeIntentSecondaries(result *intent.ClassificationResult) {
	if result == nil || len(result.Secondary) == 0 {
		return
	}
	seen := map[intent.IntentLabel]bool{result.Primary: true}
	kept := make([]intent.IntentLabel, 0, len(result.Secondary))
	for _, label := range result.Secondary {
		if label == "" || seen[label] {
			continue
		}
		seen[label] = true
		kept = append(kept, label)
	}
	result.Secondary = kept
}

func cloneFetchedDocumentClassification(current *intent.ClassificationResult) intent.ClassificationResult {
	if current == nil {
		return intent.ClassificationResult{}
	}
	base := *current
	if len(current.Secondary) > 0 {
		base.Secondary = append([]intent.IntentLabel(nil), current.Secondary...)
	}
	return base
}

// semanticFetchedDocumentReadProfile is a lookup that also downloads and reads
// the file. The full profile is required: a light search budget of 1 omits
// the reader, and the light prompt tells the model those tools are absent.
func semanticFetchedDocumentReadProfile(result intent.ClassificationResult) bool {
	switch result.Primary {
	case intent.LabelSearch, intent.LabelWebFetch, intent.LabelFileRead:
	default:
		return false
	}
	return result.HasLabel(intent.LabelFileDownload) && result.HasLabel(intent.LabelFileRead)
}

// foldFetchedDocumentScan maps fullwidth letters onto ASCII and every other
// character onto a boundary, matching utteranceNamesPDF.
func foldFetchedDocumentScan(text string) string {
	var b strings.Builder
	b.Grow(len(text))
	for _, r := range text {
		switch {
		case r >= 'A' && r <= 'Z':
			b.WriteByte(byte(r - 'A' + 'a'))
		case r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.':
			b.WriteByte(byte(r))
		case r >= 'Ａ' && r <= 'Ｚ':
			b.WriteByte(byte(r - 'Ａ' + 'a'))
		case r >= 'ａ' && r <= 'ｚ':
			b.WriteByte(byte(r - 'ａ' + 'a'))
		case r >= '０' && r <= '９':
			b.WriteByte(byte(r - '０' + '0'))
		case r == '．':
			b.WriteByte('.')
		default:
			b.WriteByte(' ')
		}
	}
	return b.String()
}

func foldedHasBoundedToken(folded, token string) bool {
	if token == "" {
		return false
	}
	for i := 0; i+len(token) <= len(folded); {
		if folded[i:i+len(token)] != token {
			i++
			continue
		}
		beforeOK := i == 0 || !fetchedDocumentASCIIAlphaNum(folded[i-1])
		after := i + len(token)
		afterOK := after == len(folded) || !fetchedDocumentASCIIAlphaNum(folded[after])
		if beforeOK && afterOK {
			return true
		}
		i = after
	}
	return false
}

func fetchedDocumentASCIIAlphaNum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9')
}
