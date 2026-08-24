package longhorizon

import (
	"regexp"
	"strings"
	"unicode"
)

var nextSuffixSplit = regexp.MustCompile("(?:\u2014|\u2013|--|//|#|[\uFF08(])")

func ParseManagerNext(text string) NextStep {
	for _, line := range strings.Split(text, "\n") {
		normalized := compactNextLine(line)
		normalized = nextSuffixSplit.Split(normalized, 2)[0]
		switch normalized {
		case "next:gui", "\u4e0b\u4e00\u6b65:gui\u4efb\u52a1", "\u4e0b\u4e00\u6b65\uff1agui\u4efb\u52a1":
			return NextGUI
		case "next:cli", "\u4e0b\u4e00\u6b65:cli\u4efb\u52a1", "\u4e0b\u4e00\u6b65\uff1acli\u4efb\u52a1":
			return NextCLI
		case "next:browser", "\u4e0b\u4e00\u6b65:browser", "\u4e0b\u4e00\u6b65\uff1abrowser", "\u4e0b\u4e00\u6b65:\u6d4f\u89c8\u5668", "\u4e0b\u4e00\u6b65\uff1a\u6d4f\u89c8\u5668":
			return NextBrowser
		case "next:ask", "\u4e0b\u4e00\u6b65:\u8bf7\u793a\u7528\u6237", "\u4e0b\u4e00\u6b65\uff1a\u8bf7\u793a\u7528\u6237", "\u4e0b\u4e00\u6b65:\u8bf7\u793a", "\u4e0b\u4e00\u6b65\uff1a\u8bf7\u793a", "\u4e0b\u4e00\u6b65:\u8be2\u95ee\u7528\u6237", "\u4e0b\u4e00\u6b65\uff1a\u8be2\u95ee\u7528\u6237":
			return NextAsk
		case "next:done", "next:complete", "\u4e0b\u4e00\u6b65:\u5b8c\u6210", "\u4e0b\u4e00\u6b65\uff1a\u5b8c\u6210":
			return NextDone
		case "next:blocked", "\u4e0b\u4e00\u6b65:\u963b\u585e", "\u4e0b\u4e00\u6b65\uff1a\u963b\u585e":
			return NextBlocked
		}
	}
	return NextInvalid
}

func ParseManagerPlan(text string) ManagerPlan {
	plan := ManagerPlan{
		Next: ParseManagerNext(text),
		Raw:  text,
	}
	plan.Goal = clipRunes(extractLabeledBlock(text, "goal", "\u76ee\u6807", "\u8ba1\u5212"), GoalCap)
	plan.Acceptance = clipRunes(extractLabeledBlock(text, "acceptance", "\u9a8c\u6536", "\u9a8c\u6536\u6807\u51c6"), AcceptanceCap)
	plan.Question = clipRunes(extractLabeledBlock(text, "question", "\u95ee\u9898", "\u8bf7\u793a"), GoalCap)
	refs := extractLabeledBlock(text, "relatedaudits", "related", "\u5f15\u7528\u5ba1\u8ba1")
	if refs != "" {
		for _, part := range strings.Split(refs, "\n") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			plan.RelatedAudits = append(plan.RelatedAudits, clipRunes(part, RelatedAuditEachCap))
			if len(plan.RelatedAudits) >= RelatedAuditMax {
				break
			}
		}
	}
	if plan.Next == NextInvalid {
		plan.Next = NextAsk
		if strings.TrimSpace(plan.Question) == "" {
			plan.Question = "I could not parse a valid Next route (cli/gui/browser/ask/blocked/done)."
		}
	}
	if (plan.Next == NextCLI || plan.Next == NextGUI || plan.Next == NextBrowser) && strings.TrimSpace(plan.Goal) == "" {
		plan.Next = NextAsk
		plan.Question = "Executor routes require a Goal field."
	}
	return plan
}

func ParseAuditReport(text string, probe ProbeResult) AuditReport {
	report := AuditReport{
		Status:    "incomplete",
		Integrity: "suspect",
		Alignment: "drifted",
		Summary:   clipRunes(strings.TrimSpace(text), AuditorOutputCap),
		HasProbe:  strings.TrimSpace(probe.Digest) != "",
	}
	status := strings.ToLower(extractLabeledBlock(text, "status", "\u72b6\u6001"))
	switch firstToken(status) {
	case "complete", "\u5b8c\u6210":
		report.Status = "complete"
	case "blocked", "\u963b\u585e":
		report.Status = "blocked"
	case "incomplete", "\u672a\u5b8c\u6210":
		report.Status = "incomplete"
	}
	integrity := strings.ToLower(extractLabeledBlock(text, "integrity", "\u5b8c\u6574\u6027"))
	switch firstToken(integrity) {
	case "clean", "\u5e72\u51c0":
		report.Integrity = "clean"
	case "violation", "\u8fdd\u89c4":
		report.Integrity = "violation"
	case "suspect", "\u53ef\u7591":
		report.Integrity = "suspect"
	}
	alignment := strings.ToLower(extractLabeledBlock(text, "alignment", "\u5bf9\u9f50"))
	switch firstToken(alignment) {
	case "aligned", "\u5bf9\u9f50":
		report.Alignment = "aligned"
	case "drifted", "\u504f\u79bb":
		report.Alignment = "drifted"
	}
	if summary := extractLabeledBlock(text, "summary", "\u6458\u8981"); summary != "" {
		report.Summary = clipRunes(summary, AuditorOutputCap)
	}
	if !report.HasProbe && report.Integrity == "clean" {
		report.Integrity = "suspect"
	}
	if ProbeShowsUserChallenge(probe.Digest) {
		if report.Integrity == "clean" {
			report.Integrity = "suspect"
		}
		if report.Status == "complete" {
			report.Status = "incomplete"
		}
	}
	return report
}

func ProbeShowsUserChallenge(digest string) bool {
	return strings.Contains(strings.ToLower(digest), "flags=captcha_or_login")
}

func compactNextLine(line string) string {
	line = strings.TrimSpace(line)
	line = strings.Trim(line, "*")
	var b strings.Builder
	for _, r := range strings.ToLower(line) {
		if unicode.IsSpace(r) || r == '\u3000' {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func extractLabeledBlock(text string, labels ...string) string {
	lines := strings.Split(text, "\n")
	want := make([]string, 0, len(labels))
	for _, label := range labels {
		want = append(want, compactNextLine(label+":"), compactNextLine(label+"\uff1a"))
	}
	collecting := false
	var body []string
	for _, line := range lines {
		head := compactNextLine(line)
		matched := false
		for _, label := range want {
			if head == label || strings.HasPrefix(head, label) {
				matched = true
				rest := strings.TrimSpace(line)
				colon := strings.IndexAny(rest, ":\uff1a")
				if colon >= 0 {
					rest = strings.TrimSpace(rest[colon+1:])
				}
				if rest != "" {
					body = append(body, rest)
				}
				collecting = true
				break
			}
		}
		if matched {
			continue
		}
		if collecting {
			if looksLikeLabel(line) {
				break
			}
			body = append(body, strings.TrimSpace(line))
		}
	}
	return strings.TrimSpace(strings.Join(body, "\n"))
}

func looksLikeLabel(line string) bool {
	head := compactNextLine(line)
	for _, prefix := range []string{
		"next:", "\u4e0b\u4e00\u6b65:", "goal:", "\u76ee\u6807:", "acceptance:", "\u9a8c\u6536:", "\u9a8c\u6536\u6807\u51c6:",
		"question:", "\u95ee\u9898:", "related:", "relatedaudits:", "status:", "\u72b6\u6001:",
		"integrity:", "\u5b8c\u6574\u6027:", "alignment:", "\u5bf9\u9f50:",
		"summary:", "\u6458\u8981:",
	} {
		if strings.HasPrefix(head, compactNextLine(prefix)) {
			return true
		}
	}
	return false
}

func firstToken(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	fields := strings.FieldsFunc(s, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == ';' || r == '/' || r == '|'
	})
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
