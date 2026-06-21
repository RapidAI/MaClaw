package v2

import (
	"regexp"
	"sort"
	"strings"
	"unicode"
)

// extractFieldFromContext tries to extract a value for the given field from
// the combined context text. Uses rule-based extraction only — no LLM inference.
//
// Strategy: for each field, we build a set of "anchor patterns" based on
// the field's Label and known synonyms, then look for values adjacent to
// those anchors in the text.
func extractFieldFromContext(field PhaseInputField, context string) *PrefilledValue {
	// Select extraction strategy based on field type and name
	switch {
	case field.Type == "select":
		return extractSelectField(field, context)
	case field.Type == "boolean":
		return nil // boolean fields should never be auto-prefilled — user must make explicit choice
	case isPersonNameField(field.Name):
		return extractPersonName(field, context)
	case isInstitutionField(field.Name):
		return extractInstitution(field, context)
	case isTitleField(field.Name):
		return extractAcademicTitle(field, context)
	case isDisciplineField(field.Name):
		return extractDiscipline(field, context)
	case isEmailField(field.Name):
		return extractEmail(field, context)
	case isPhoneField(field.Name):
		return extractPhone(field, context)
	case isDateField(field.Name, field.Type):
		return extractDate(field, context)
	case isNumericField(field.Name, field.Type):
		return extractNumericByLabel(field, context)
	default:
		return extractByLabelAnchor(field, context)
	}
}

// --- Field category detectors ---

func isPersonNameField(name string) bool {
	return name == "name" || name == "applicant_name"
}

func isInstitutionField(name string) bool {
	return name == "institution" || name == "organization"
}

func isDisciplineField(name string) bool {
	return name == "discipline" || name == "research_field" || name == "subject_area"
}

func isTitleField(name string) bool {
	return name == "title"
}

func isEmailField(name string) bool {
	return name == "email" || strings.HasSuffix(name, "_email")
}

func isPhoneField(name string) bool {
	return name == "phone" || name == "mobile" || name == "tel" ||
		strings.HasSuffix(name, "_phone") || strings.HasSuffix(name, "_mobile")
}

func isDateField(name, fieldType string) bool {
	if fieldType == "date" {
		return true
	}
	return dateFieldNames[name]
}

// dateFieldNames are known date field names that use type="text" in templates.
var dateFieldNames = map[string]bool{
	"birth_date": true, "start_date": true, "end_date": true,
	"graduation_date": true, "phd_date": true,
}

func isNumericField(name, fieldType string) bool {
	if fieldType == "number" {
		return true
	}
	return numericFieldNames[name]
}

// numericFieldNames are known numeric field names that use type="text" in templates.
var numericFieldNames = map[string]bool{
	"h_index": true, "total_citations": true, "total_papers": true,
	"phd_year": true, "funding_amount": true, "duration": true,
}

// --- Extractors ---

// extractSelectField checks if any of the field's predefined options appear in context.
// For short option values (≤2 runes), requires adjacency to the field's label to avoid
// false positives (e.g. "男" appearing in "男生宿舍" when the field is "性别").
func extractSelectField(field PhaseInputField, context string) *PrefilledValue {
	if len(field.Options) == 0 {
		return nil
	}

	// First pass: try to find an option value near the field's label (high confidence)
	if field.Label != "" {
		for _, anchor := range []string{field.Label + "：", field.Label + ":", field.Label + "是", field.Label + "为"} {
			idx := strings.Index(context, anchor)
			if idx < 0 {
				continue
			}
			afterAnchor := context[idx+len(anchor):]
			afterAnchor = strings.TrimLeftFunc(afterAnchor, unicode.IsSpace)
			for _, opt := range field.Options {
				if opt.Value != "" && strings.HasPrefix(afterAnchor, opt.Value) {
					return &PrefilledValue{
						Value:        opt.Value,
						Source:       "context",
						SourceDetail: "匹配到选项值: " + opt.Value,
						Confidence:   0.90,
					}
				}
			}
		}
	}

	// Second pass: bare substring match — only for options with 3+ runes
	// (short values like "男"/"女" are too ambiguous without label context)
	for _, opt := range field.Options {
		if opt.Value == "" {
			continue
		}
		optRunes := len([]rune(opt.Value))
		if optRunes < 3 {
			// For 1-2 rune options, check if they appear in specific patterns:
			// "我是男" / "为男" / "是男性" — the option value must appear after an identity marker
			// or be immediately followed by "性" (gender indicator)
			identityMarkers := []string{"我是" + opt.Value, "性别" + opt.Value, "为" + opt.Value}
			matched := false
			for _, marker := range identityMarkers {
				if strings.Contains(context, marker) {
					matched = true
					break
				}
			}
			// Also check "X性" pattern (e.g. "男性" contains option "男")
			if !matched && strings.Contains(context, opt.Value+"性") {
				matched = true
			}
			if !matched {
				continue
			}
		} else {
			if !strings.Contains(context, opt.Value) {
				continue
			}
		}
		return &PrefilledValue{
			Value:        opt.Value,
			Source:       "context",
			SourceDetail: "匹配到选项值: " + opt.Value,
			Confidence:   0.80,
		}
	}
	return nil
}

// extractPersonName tries to find a person name from identity anchors.
// Supports both Chinese names (2-4 CJK chars) and English names (capitalized words).
var personNameAnchors = regexp.MustCompile(`(?:我(?:是|叫)|姓名[：:]\s*|申请人[：:]\s*|(?:我|本人).*?(?:叫|是))([` + "\u4e00-\u9fff" + `]{2,4})`)

// personNamePossessive matches "我是X的Y" where Y is the person name.
var personNamePossessive = regexp.MustCompile(`(?:我是|我叫)[` + "\u4e00-\u9fff" + `]+的([` + "\u4e00-\u9fff" + `]{2,3}?)(?:教授|研究员|副教授|讲师|博士|老师|同学|[，,。；;、\s]|$)`)

// englishNameAnchors matches common English name patterns with identity markers.
// Captures "My name is John Smith" / "I'm Jane Doe" / "Name: Alice Johnson"
// Note: uses a two-step approach — first find the anchor case-insensitively,
// then extract the capitalized name after it.
var englishNameAnchorPrefixes = []string{"my name is ", "i'm ", "i am "}

// englishNameCapRe captures 1-3 capitalized words at the start of a string.
var englishNameCapRe = regexp.MustCompile(`^([A-Z][a-z]+(?:\s+[A-Z][a-z]+){0,2})`)

// englishNameLabelAnchors matches "姓名：John Smith" or "Name: John Smith"
var englishNameLabelRe = regexp.MustCompile(`(?:姓名|名字|[Nn]ame)[：:]\s*([A-Z][a-z]+(?:\s+[A-Z][a-z]+){0,2})`)

func extractPersonName(field PhaseInputField, context string) *PrefilledValue {
	// First try the possessive pattern ("我是XX的张三") — most specific for Chinese
	if m := personNamePossessive.FindStringSubmatch(context); m != nil {
		name := strings.TrimSpace(m[1])
		if name != "" && !isCommonNonName(name) {
			return &PrefilledValue{
				Value:        name,
				Source:       "context",
				SourceDetail: "提取自: " + m[0],
				Confidence:   0.90,
			}
		}
	}

	// Try Chinese name direct anchors ("我是张三"、"我叫李明")
	matches := personNameAnchors.FindAllStringSubmatch(context, -1)
	for _, m := range matches {
		name := strings.TrimSpace(m[1])
		if name != "" && !isCommonNonName(name) {
			return &PrefilledValue{
				Value:        name,
				Source:       "context",
				SourceDetail: "提取自: " + m[0],
				Confidence:   0.90,
			}
		}
	}

	// Try English name with label anchor ("姓名：John Smith" / "Name: Alice")
	if m := englishNameLabelRe.FindStringSubmatch(context); m != nil {
		name := strings.TrimSpace(m[1])
		if name != "" && !isCommonEnglishNonName(name) {
			return &PrefilledValue{
				Value:        name,
				Source:       "context",
				SourceDetail: "提取自: " + m[0],
				Confidence:   0.85,
			}
		}
	}

	// Try English name with identity markers ("My name is X" / "I'm X")
	lowerCtx := strings.ToLower(context)
	for _, prefix := range englishNameAnchorPrefixes {
		idx := strings.Index(lowerCtx, prefix)
		if idx < 0 {
			continue
		}
		// Extract from the ORIGINAL (case-preserved) context after the prefix
		afterPrefix := context[idx+len(prefix):]
		afterPrefix = strings.TrimLeftFunc(afterPrefix, unicode.IsSpace)
		// Capture capitalized words (1-3 words)
		if m := englishNameCapRe.FindString(afterPrefix); m != "" {
			name := strings.TrimSpace(m)
			if name != "" && !isCommonEnglishNonName(name) {
				return &PrefilledValue{
					Value:        name,
					Source:       "context",
					SourceDetail: "提取自英文自我介绍",
					Confidence:   0.80,
				}
			}
		}
	}

	return nil
}

// isCommonEnglishNonName filters out common English words that look like capitalized names
// but are actually common nouns/adjectives that appear after "I'm".
var commonEnglishNonNames = map[string]bool{
	"sorry": true, "happy": true, "fine": true, "good": true,
	"here": true, "sure": true, "ready": true, "done": true,
	"not": true, "also": true, "very": true,
	"professor": true, "doctor": true, "student": true,
}

func isCommonEnglishNonName(s string) bool {
	return commonEnglishNonNames[strings.ToLower(s)]
}

// isCommonNonName filters out common Chinese words that are not person names
// but might be captured by the 2-4 character pattern.
func isCommonNonName(s string) bool {
	nonNames := []string{"教授", "研究员", "副教授", "讲师", "助理", "博士", "硕士", "学者", "同学", "老师"}
	for _, n := range nonNames {
		if s == n {
			return true
		}
	}
	// Also reject if the captured text ends with an institution suffix
	// (e.g. "北京大学" captured by "我是[CJK]{2,4}" pattern)
	instSuffixes := []string{"大学", "学院", "研究所", "研究院", "公司", "集团", "中心"}
	for _, suffix := range instSuffixes {
		if strings.HasSuffix(s, suffix) {
			return true
		}
	}
	return false
}

// extractInstitution tries to find an institution name (typically ending in 大学/研究所/学院/公司).
// The regex captures 2-20 CJK characters ending with a known institution suffix.
// We then strip common prefix verbs/particles that are not part of the name.
var institutionRe = regexp.MustCompile(`([` + "\u4e00-\u9fff" + `]{2,20}(?:科技大学|工业大学|师范大学|理工大学|医科大学|大学|研究所|研究院|学院))`)

// institutionPrefixStrip removes common Chinese verb/particle prefixes from institution matches.
var institutionPrefixStrip = regexp.MustCompile(`^(?:我在|我是|来自|在于|就读|毕业于|工作于|属于|来|在|到|去|是)`)

func extractInstitution(field PhaseInputField, context string) *PrefilledValue {
	if m := institutionRe.FindStringSubmatch(context); m != nil {
		inst := strings.TrimSpace(m[1])
		// Strip common prefix verbs that aren't part of the institution name
		inst = institutionPrefixStrip.ReplaceAllString(inst, "")
		inst = strings.TrimSpace(inst)
		if inst != "" && len([]rune(inst)) >= 2 {
			return &PrefilledValue{
				Value:        inst,
				Source:       "context",
				SourceDetail: "提取自文本中的机构名称",
				Confidence:   0.85,
			}
		}
	}
	return nil
}

// extractAcademicTitle looks for academic title keywords.
// Ordered longest-first to prefer "副教授" over "教授" when both substrings match.
var titleKeywords = []string{"助理研究员", "助理教授", "副研究员", "副教授", "研究员", "教授", "讲师"}

func extractAcademicTitle(field PhaseInputField, context string) *PrefilledValue {
	for _, kw := range titleKeywords {
		if strings.Contains(context, kw) {
			return &PrefilledValue{
				Value:        kw,
				Source:       "context",
				SourceDetail: "匹配到职称关键词: " + kw,
				Confidence:   0.80,
			}
		}
	}
	return nil
}

// extractDiscipline looks for academic discipline/research field keywords in free text.
// Matches patterns like "研究方向为X"、"学科：X"、"研究领域：X"
// as well as known discipline names appearing in the text.
//
// Strategy:
// 1. Label-anchor extraction with colon separators (highest precision)
// 2. Direct matching of known discipline names (longest match first)
// 3. Generic label-anchor fallback
var disciplineAnchors = []string{
	"学科领域：", "学科领域:", "研究领域：", "研究领域:",
	"学科：", "学科:", "专业：", "专业:",
	"研究方向：", "研究方向:", "一级学科：", "一级学科:",
	"研究方向为", "研究方向是", "学科领域为", "学科领域是",
}

// knownDisciplines contains common Chinese academic discipline names for direct matching.
// MUST be sorted by rune length descending — longest match wins to avoid
// "计算机科学" matching when "计算机科学与技术" is present.
var knownDisciplines []string

func init() {
	raw := []string{
		"计算机科学与技术", "信息与通信工程", "电子科学与技术", "控制科学与工程",
		"材料科学与工程", "化学工程与技术", "环境科学与工程", "航空宇航科学与技术",
		"公共卫生与预防医学", "管理科学与工程", "自然语言处理",
		"人工智能", "软件工程", "机械工程", "土木工程", "电气工程", "生物医学工程",
		"计算机视觉", "机器学习", "深度学习", "数据挖掘", "网络安全",
		"新闻传播学", "中国语言文学", "外国语言文学",
		"工商管理", "公共管理",
		"基础医学", "临床医学", "地球科学",
		"物联网", "大数据", "云计算", "量子计算",
		"教育学", "心理学", "社会学", "经济学",
		"物理学", "生物学", "天文学",
		"数学", "化学", "法学", "药学",
	}
	// Sort by rune length descending to guarantee longest-match-first semantics.
	// This is a correctness invariant — DO NOT remove this sort.
	sort.Slice(raw, func(i, j int) bool {
		return len([]rune(raw[i])) > len([]rune(raw[j]))
	})
	knownDisciplines = raw
}

// maxDisciplineValueRunes limits extracted discipline values. Real discipline names
// are at most ~10 CJK characters. Anything longer is likely a sentence fragment.
const maxDisciplineValueRunes = 15

func extractDiscipline(field PhaseInputField, context string) *PrefilledValue {
	// First try label-anchor patterns (most reliable — requires colon/copula separator)
	for _, anchor := range disciplineAnchors {
		idx := strings.LastIndex(context, anchor)
		if idx < 0 {
			continue
		}
		afterAnchor := context[idx+len(anchor):]
		value := extractValueAfterAnchor(afterAnchor)
		if value == "" {
			continue
		}
		// Validate: discipline names are short (2-15 CJK chars)
		if runeLen := len([]rune(value)); runeLen < 2 || runeLen > maxDisciplineValueRunes {
			continue
		}
		return &PrefilledValue{
			Value:        value,
			Source:       "context",
			SourceDetail: "提取自: " + anchor + value,
			Confidence:   0.80,
		}
	}

	// Second: match known discipline names directly in text.
	// List is sorted longest-first, so first hit is the most specific match.
	// For short names (≤3 runes like "数学"), require them to NOT be part of a
	// longer compound word (e.g. "数学模型" should not match "数学" as the discipline).
	for _, disc := range knownDisciplines {
		idx := strings.Index(context, disc)
		if idx < 0 {
			continue
		}
		// For short discipline names, check that they are not a prefix of a longer word
		if len([]rune(disc)) <= 3 {
			afterEnd := context[idx+len(disc):]
			if afterEnd != "" {
				nextRune, _ := nextCJKRune(afterEnd)
				// If followed by another CJK character that forms a compound word, skip
				if nextRune != 0 && !isDisciplineSuffix(nextRune) {
					continue
				}
			}
		}
		return &PrefilledValue{
			Value:        disc,
			Source:       "context",
			SourceDetail: "匹配到学科名称: " + disc,
			Confidence:   0.75,
		}
	}

	// Fallback to generic label-anchor extraction
	return extractByLabelAnchor(field, context)
}

// nextCJKRune returns the first rune from s if it's a CJK character, or 0 otherwise.
func nextCJKRune(s string) (rune, int) {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return r, len(string(r))
		}
		return 0, 0
	}
	return 0, 0
}

// isDisciplineSuffix returns true if the rune is a common suffix that still
// indicates a discipline context (e.g. "学" in "数学学科", "系" in "化学系").
func isDisciplineSuffix(r rune) bool {
	switch r {
	case '学', '系', '院', '科', '类', '门':
		return true
	}
	return false
}

// extractByLabelAnchor is the generic fallback: looks for "Label：Value" or
// "Label: Value" patterns in the context text.
// Prefers the LAST occurrence of the anchor — in concatenated context, the most
// recent (bottom) entries are the most relevant to the current task.
func extractByLabelAnchor(field PhaseInputField, context string) *PrefilledValue {
	// Build anchor patterns from the field's Label
	label := field.Label
	if label == "" {
		return nil
	}

	// Pattern: "Label：value" or "Label: value" (Chinese/English colon)
	anchors := []string{label + "：", label + ":", label + "是", label + "为"}
	for _, anchor := range anchors {
		idx := strings.LastIndex(context, anchor)
		if idx < 0 {
			continue
		}
		// Extract the value after the anchor until end-of-line or next delimiter
		afterAnchor := context[idx+len(anchor):]
		value := extractValueAfterAnchor(afterAnchor)
		if value != "" {
			return &PrefilledValue{
				Value:        value,
				Source:       "context",
				SourceDetail: "提取自: " + anchor + value,
				Confidence:   0.75,
			}
		}
	}
	return nil
}

// extractValueAfterAnchor extracts a value string after an anchor pattern.
// Stops at newline, comma, semicolon, or common Chinese delimiters.
func extractValueAfterAnchor(s string) string {
	s = strings.TrimLeftFunc(s, unicode.IsSpace)
	if s == "" {
		return ""
	}

	// Find the end of the value
	end := len(s)
	for i, r := range s {
		if r == '\n' || r == '\r' || r == '，' || r == '；' || r == '。' || r == '、' {
			end = i
			break
		}
		// Also stop at English delimiters when followed by space (avoid cutting URLs)
		if (r == ',' || r == ';') && i+1 < len(s) && s[i+1] == ' ' {
			end = i
			break
		}
	}

	value := strings.TrimSpace(s[:end])
	// Sanity: don't return overly long values (likely paragraph text, not a field value)
	if len([]rune(value)) > 100 {
		return ""
	}
	// Reject values that are clearly not actual data — question words, instruction fragments
	if isQuestionOrInstructionFragment(value) {
		return ""
	}
	return value
}

// isQuestionOrInstructionFragment detects values that were extracted from
// question/instruction context rather than actual data. When the "是"/"为" anchor
// matches "研究领域是什么" or "研究领域是必填的", the extracted "什么"/"必填的" should be rejected.
func isQuestionOrInstructionFragment(value string) bool {
	if value == "" {
		return false
	}
	// Exact matches of common question/instruction words
	questionWords := []string{
		"什么", "哪个", "哪些", "几", "多少", "怎么", "怎样", "如何", "为何",
		"谁", "何时", "何地", "吗", "呢", "吧",
	}
	for _, qw := range questionWords {
		if value == qw || strings.HasPrefix(value, qw) {
			return true
		}
	}
	// Common instruction fragments
	instructionPrefixes := []string{
		"必填", "必需", "可选", "选填", "请填", "请输入", "请提供",
		"不能为空", "不可为空",
	}
	for _, ip := range instructionPrefixes {
		if strings.HasPrefix(value, ip) {
			return true
		}
	}
	return false
}

// --- Email extraction ---

// emailRe matches standard email addresses.
var emailRe = regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`)

func extractEmail(field PhaseInputField, context string) *PrefilledValue {
	if m := emailRe.FindString(context); m != "" {
		return &PrefilledValue{
			Value:        m,
			Source:       "context",
			SourceDetail: "提取到邮箱: " + m,
			Confidence:   0.85,
		}
	}
	return nil
}

// --- Phone number extraction ---

// phoneRe matches common phone number formats:
// - China mainland: 13x/14x/15x/16x/17x/18x/19x followed by 9 digits (11 total)
// - International: +86-xxx / +1-xxx style
// Uses negative lookbehind/lookahead simulation via non-digit boundaries to avoid
// matching inside longer digit sequences (e.g. ID card numbers).
var chinaPhoneRe = regexp.MustCompile(`(?:^|[^\d])((?:\+86|86)?[\s\-]?1[3-9]\d{9})(?:[^\d]|$)`)
var intlPhoneRe = regexp.MustCompile(`\+\d{1,3}[\s\-]?\d[\d\s\-]{6,14}\d`)

func extractPhone(field PhaseInputField, context string) *PrefilledValue {
	// Try China mainland phone first (most specific)
	if m := chinaPhoneRe.FindStringSubmatch(context); m != nil {
		phone := m[1] // capture group 1 is the 11-digit number
		return &PrefilledValue{
			Value:        normalizePhone(phone),
			Source:       "context",
			SourceDetail: "提取到手机号: " + phone,
			Confidence:   0.90,
		}
	}
	// Try international format
	if m := intlPhoneRe.FindString(context); m != "" {
		return &PrefilledValue{
			Value:        normalizePhone(m),
			Source:       "context",
			SourceDetail: "提取到电话: " + m,
			Confidence:   0.80,
		}
	}
	return nil
}

// normalizePhone removes spaces and dashes from phone numbers for storage.
func normalizePhone(phone string) string {
	var sb strings.Builder
	for _, r := range phone {
		if r == '+' || (r >= '0' && r <= '9') {
			sb.WriteRune(r)
		}
	}
	return sb.String()
}

// --- Date extraction ---

// datePatterns matches common date formats in Chinese and standard formats.
// Ordered most-specific first.
var datePatterns = []*regexp.Regexp{
	// "1980年5月15日" / "1980年5月"
	regexp.MustCompile(`\d{4}年\d{1,2}月(?:\d{1,2}日)?`),
	// "2023-05-15" / "2023/05/15" / "2023.05.15"
	regexp.MustCompile(`\d{4}[/\-.]\d{1,2}[/\-.]\d{1,2}`),
	// "2023-05" / "2023/05" (year-month only, no day follows)
	regexp.MustCompile(`\d{4}[/\-.]\d{1,2}`),
}

// birthDateRe matches "出生于1985年3月" or "出生1982年6月15日" patterns.
var birthDateRe = regexp.MustCompile(`出生于?\s*(\d{4}年\d{1,2}月(?:\d{1,2}日)?)`)

func extractDate(field PhaseInputField, context string) *PrefilledValue {
	label := field.Label
	if label == "" {
		label = field.Name
	}

	// Try to find date near the field's label anchor first
	anchors := []string{label + "：", label + ":"}
	// Also try the field Name as anchor (handles "birth_date: 1990-05-20")
	if field.Name != label {
		anchors = append(anchors, field.Name+"：", field.Name+":")
	}
	// Add verb-based anchors
	anchors = append(anchors, label+"是", label+"为")

	for _, anchor := range anchors {
		idx := strings.LastIndex(context, anchor)
		if idx < 0 {
			continue
		}
		afterAnchor := context[idx+len(anchor):]
		afterAnchor = strings.TrimLeftFunc(afterAnchor, unicode.IsSpace)
		// Take a limited window to avoid matching unrelated dates later in text
		window := afterAnchor
		if len([]rune(window)) > 30 {
			window = string([]rune(window)[:30])
		}
		if d := findDateInText(window); d != "" {
			return normalizeDatePrefillValue(field, d, "提取自: "+anchor+d)
		}
	}

	// For birth_date specifically, also try "出生于XXXX年" pattern
	if field.Name == "birth_date" {
		if m := birthDateRe.FindStringSubmatch(context); m != nil {
			return normalizeDatePrefillValue(field, m[1], "提取自: "+m[0])
		}
	}

	return nil
}

// normalizeDatePrefillValue creates a PrefilledValue for a date field.
// When the field type is "date" (rendered as <input type="date">),
// the value is normalized to ISO format (YYYY-MM-DD or YYYY-MM).
// For text-type date fields, the original format is preserved.
func normalizeDatePrefillValue(field PhaseInputField, value, sourceDetail string) *PrefilledValue {
	if field.Type == "date" {
		if iso := normalizeDateToISO(value); iso != "" {
			value = iso
		}
	}
	return &PrefilledValue{
		Value:        value,
		Source:       "context",
		SourceDetail: sourceDetail,
		Confidence:   0.85,
	}
}

// findDateInText finds the first date pattern in the given text.
func findDateInText(text string) string {
	for _, re := range datePatterns {
		if m := re.FindString(text); m != "" {
			return strings.TrimSpace(m)
		}
	}
	return ""
}

// --- Numeric field extraction ---

// numericAfterLabelRe matches digits (possibly with decimal) after an anchor.
var numericAfterLabelRe = regexp.MustCompile(`^(\d+(?:\.\d+)?)`)

// extractNumericByLabel extracts a numeric value using the field's label as anchor.
// More specific than extractByLabelAnchor — only captures digit sequences,
// reducing false positives for fields like h_index, total_citations.
// Uses LastIndex to prefer the most recent occurrence (consistent with extractByLabelAnchor).
func extractNumericByLabel(field PhaseInputField, context string) *PrefilledValue {
	label := field.Label
	if label == "" {
		return nil
	}

	// Try "Label：42" or "Label: 42" patterns
	anchors := []string{label + "：", label + ":", label + "是", label + "为"}
	for _, anchor := range anchors {
		idx := strings.LastIndex(context, anchor)
		if idx < 0 {
			continue
		}
		afterAnchor := context[idx+len(anchor):]
		afterAnchor = strings.TrimLeftFunc(afterAnchor, unicode.IsSpace)
		if m := numericAfterLabelRe.FindString(afterAnchor); m != "" {
			return &PrefilledValue{
				Value:        m,
				Source:       "context",
				SourceDetail: "提取自: " + anchor + m,
				Confidence:   0.80,
			}
		}
	}
	return nil
}
