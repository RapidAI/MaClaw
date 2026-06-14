package v2

import (
	"regexp"
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
	case isPersonNameField(field.Name):
		return extractPersonName(field, context)
	case isInstitutionField(field.Name):
		return extractInstitution(field, context)
	case isTitleField(field.Name):
		return extractAcademicTitle(field, context)
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

func isTitleField(name string) bool {
	return name == "title"
}

// --- Extractors ---

// extractSelectField checks if any of the field's predefined options appear in context.
func extractSelectField(field PhaseInputField, context string) *PrefilledValue {
	if len(field.Options) == 0 {
		return nil
	}
	for _, opt := range field.Options {
		if opt.Value != "" && strings.Contains(context, opt.Value) {
			return &PrefilledValue{
				Value:        opt.Value,
				Source:       "context",
				SourceDetail: "匹配到选项值: " + opt.Value,
				Confidence:   0.85,
			}
		}
	}
	return nil
}

// extractPersonName tries to find a Chinese person name near identity anchors.
// Chinese names are 2-4 characters, typically preceded by "我是"/"我叫"/"姓名"/"申请人".
var personNameAnchors = regexp.MustCompile(`(?:我(?:是|叫)|姓名[：:]\s*|申请人[：:]\s*|(?:我|本人).*?(?:叫|是))([` + "\u4e00-\u9fff" + `]{2,4})`)

// personNamePossessive matches "我是X的Y" where Y is the person name.
// Handles: "我是北京大学计算机学院的张伟教授" → "张伟"
// The name capture is non-greedy ({2,3}?) then followed by a title suffix or delimiter.
var personNamePossessive = regexp.MustCompile(`(?:我是|我叫)[` + "\u4e00-\u9fff" + `]+的([` + "\u4e00-\u9fff" + `]{2,3}?)(?:教授|研究员|副教授|讲师|博士|老师|同学|[，,。；;、\s]|$)`)

func extractPersonName(field PhaseInputField, context string) *PrefilledValue {
	// First try the possessive pattern ("我是XX的张三") — more specific
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

	// Fallback to direct anchors ("我是张三"、"我叫李明")
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
	return nil
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

// extractByLabelAnchor is the generic fallback: looks for "Label：Value" or
// "Label: Value" patterns in the context text.
func extractByLabelAnchor(field PhaseInputField, context string) *PrefilledValue {
	// Build anchor patterns from the field's Label
	label := field.Label
	if label == "" {
		return nil
	}

	// Pattern: "Label：value" or "Label: value" (Chinese/English colon)
	anchors := []string{label + "：", label + ":", label + "是", label + "为"}
	for _, anchor := range anchors {
		idx := strings.Index(context, anchor)
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
	return value
}
