package browser

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// TaskVerifier validates success criteria against the current browser page.
type TaskVerifier struct {
	ocr       OCRProvider
	sessionFn func() (*Session, error)
}

// NewTaskVerifier creates a verifier. OCR-backed criteria are disabled in the
// stable browser path because they depend on screenshots.
func NewTaskVerifier(ocr OCRProvider, sessionFn func() (*Session, error)) *TaskVerifier {
	return &TaskVerifier{ocr: ocr, sessionFn: sessionFn}
}

// Verify checks all criteria and returns a combined result.
func (v *TaskVerifier) Verify(criteria []CriterionSpec) (*VerifyResult, error) {
	sess, err := v.sessionFn()
	if err != nil {
		return nil, fmt.Errorf("browser session: %w", err)
	}

	result := &VerifyResult{Passed: true}
	for _, c := range criteria {
		cr := v.checkOne(sess, c)
		result.Details = append(result.Details, cr)
		if !cr.Passed {
			result.Passed = false
		}
	}
	return result, nil
}

func (v *TaskVerifier) checkOne(sess *Session, c CriterionSpec) CriterionResult {
	switch c.Type {
	case "dom_exists":
		return v.checkDOMExists(sess, c)
	case "dom_text":
		return v.checkDOMText(sess, c)
	case "url_contains":
		return v.checkURLContains(sess, c)
	case "url_matches":
		return v.checkURLMatches(sess, c)
	case "ocr_contains":
		return CriterionResult{Criterion: c, Passed: false, Error: "ocr_contains is disabled in the stable browser path; use dom_text/url/network/console criteria"}
	case "frame_url_contains":
		return v.checkFrameURLContains(sess, c)
	case "network_request":
		return v.checkNetworkRequest(sess, c)
	case "console_no_error":
		return v.checkConsoleNoError(sess, c)
	default:
		return CriterionResult{Criterion: c, Passed: false, Error: fmt.Sprintf("unknown criterion type: %s", c.Type)}
	}
}

func (v *TaskVerifier) checkDOMExists(sess *Session, c CriterionSpec) CriterionResult {
	err := sess.WaitForSelector(c.Selector, 5)
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false, Error: fmt.Sprintf("selector %q not found: %v", c.Selector, err)}
	}
	return CriterionResult{Criterion: c, Passed: true, Actual: "exists"}
}

func (v *TaskVerifier) checkDOMText(sess *Session, c CriterionSpec) CriterionResult {
	text, err := sess.GetText(c.Selector)
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false, Error: fmt.Sprintf("get text %q: %v", c.Selector, err)}
	}
	if strings.Contains(text, c.Pattern) {
		return CriterionResult{Criterion: c, Passed: true, Actual: truncate(text, 200)}
	}
	return CriterionResult{Criterion: c, Passed: false, Actual: truncate(text, 200),
		Error: fmt.Sprintf("text does not contain %q", c.Pattern)}
}

func (v *TaskVerifier) checkURLContains(sess *Session, c CriterionSpec) CriterionResult {
	info, err := sess.Info()
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false, Error: fmt.Sprintf("get info: %v", err)}
	}
	if strings.Contains(info.URL, c.Pattern) {
		return CriterionResult{Criterion: c, Passed: true, Actual: info.URL}
	}
	return CriterionResult{Criterion: c, Passed: false, Actual: info.URL,
		Error: fmt.Sprintf("URL does not contain %q", c.Pattern)}
}

func (v *TaskVerifier) checkURLMatches(sess *Session, c CriterionSpec) CriterionResult {
	info, err := sess.Info()
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false, Error: fmt.Sprintf("get info: %v", err)}
	}
	re, err := regexp.Compile(c.Pattern)
	if err != nil {
		return CriterionResult{Criterion: c, Passed: false, Error: fmt.Sprintf("invalid regex %q: %v", c.Pattern, err)}
	}
	if re.MatchString(info.URL) {
		return CriterionResult{Criterion: c, Passed: true, Actual: info.URL}
	}
	return CriterionResult{Criterion: c, Passed: false, Actual: info.URL,
		Error: fmt.Sprintf("URL does not match %q", c.Pattern)}
}

func (v *TaskVerifier) checkFrameURLContains(sess *Session, c CriterionSpec) CriterionResult {
	frames := sess.FrameSnapshots()
	frameIDFilter := strings.TrimSpace(c.FrameID)
	for _, frame := range frames {
		if frameIDFilter != "" && frame.FrameID != frameIDFilter {
			continue
		}
		if strings.Contains(frame.URL, c.Pattern) {
			return CriterionResult{Criterion: c, Passed: true, Actual: frame.URL}
		}
	}
	return CriterionResult{Criterion: c, Passed: false, Error: fmt.Sprintf("frame URL does not contain %q", c.Pattern)}
}

func (v *TaskVerifier) checkNetworkRequest(sess *Session, c CriterionSpec) CriterionResult {
	if strings.TrimSpace(c.Pattern) == "" {
		return CriterionResult{Criterion: c, Passed: false, Error: "missing network pattern"}
	}
	if strings.Contains(strings.Join(sess.lastNetworkLines(), " | "), c.Pattern) {
		return CriterionResult{Criterion: c, Passed: true, Actual: c.Pattern}
	}
	return CriterionResult{Criterion: c, Passed: false, Error: fmt.Sprintf("network requests do not contain %q", c.Pattern)}
}

func (v *TaskVerifier) checkConsoleNoError(sess *Session, c CriterionSpec) CriterionResult {
	if len(sess.lastErrorLines()) > 0 {
		return CriterionResult{Criterion: c, Passed: false, Actual: truncate(strings.Join(sess.lastErrorLines(), " | "), 200), Error: "console error present"}
	}
	return CriterionResult{Criterion: c, Passed: true, Actual: "no_error"}
}

// WaitForStable waits until the page has no new network activity for 1 second.
// Uses a simple heuristic: poll document.readyState + check for pending XHR.
func (v *TaskVerifier) WaitForStable(timeout time.Duration) error {
	sess, err := v.sessionFn()
	if err != nil {
		return err
	}
	return sess.WaitForStable(timeout, 300*time.Millisecond)
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-3] + "..."
}
