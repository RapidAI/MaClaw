package browser

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type policyDeniedError struct {
	msg string
}

func (e *policyDeniedError) Error() string {
	return e.msg
}

func policyDenied(format string, args ...interface{}) error {
	return &policyDeniedError{msg: fmt.Sprintf(format, args...)}
}

func isPolicyDenied(err error) bool {
	var denied *policyDeniedError
	return errors.As(err, &denied)
}

func validateNavigationPolicy(policy BrowserPolicy, targetURL, currentDomain string) error {
	parsed, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	scheme := strings.ToLower(strings.TrimSpace(parsed.Scheme))
	switch scheme {
	case "http", "https":
	case "about":
		return nil
	case "":
	default:
		return policyDenied("browser policy blocked URL scheme: %s", scheme)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		if scheme == "http" || scheme == "https" {
			return fmt.Errorf("invalid url: missing host")
		}
		return nil
	}
	for _, blocked := range policy.BlockedDomains {
		if domainMatches(host, blocked) {
			return policyDenied("browser policy blocked domain: %s", host)
		}
	}
	if len(policy.AllowedDomains) > 0 {
		allowed := false
		for _, item := range policy.AllowedDomains {
			if domainMatches(host, item) {
				allowed = true
				break
			}
		}
		if !allowed {
			return policyDenied("browser policy disallowed domain: %s", host)
		}
	}
	if !policy.AllowCrossOriginNavigation && currentDomain != "" && !strings.EqualFold(currentDomain, host) {
		return policyDenied("browser policy blocked cross-origin navigation from %s to %s", currentDomain, host)
	}
	return nil
}

func validateUploadPolicy(policy BrowserPolicy) error {
	if !policy.AllowUpload {
		return policyDenied("browser policy blocked upload; pass allow_upload=true on session_start")
	}
	return nil
}

func validateDownloadPolicy(policy BrowserPolicy) error {
	if !policy.AllowDownload {
		return policyDenied("browser policy blocked download; pass allow_download=true on session_start")
	}
	return nil
}

func validatePopupPolicy(policy BrowserPolicy) error {
	if !policy.AllowPopup {
		return policyDenied("browser policy blocked popup; pass allow_popup=true on session_start")
	}
	return nil
}

func blockedActionResult(action, display string) *BrowserActionResult {
	return &BrowserActionResult{
		Action:  action,
		Status:  "blocked",
		Display: display,
		Data:    map[string]interface{}{"reason": "blocked"},
	}
}

// BrowserActionExecuted reports whether a result describes an action the
// browser actually carried out. Only a policy refusal describes one it did
// not: an "unchanged" page was still navigated, a captcha prompt sits on a
// page that was already loaded, and a failed expectation is a verdict about
// an action that has already happened.
//
// This lives beside the code that assigns the statuses because a caller in
// another package cannot enumerate them safely. One that guesses will
// eventually call a completed action refused, which tells the model nothing
// happened and invites it to repeat an effect that already holds.
func BrowserActionExecuted(result *BrowserActionResult) bool {
	if result == nil {
		return false
	}
	switch strings.TrimSpace(result.Status) {
	case "blocked":
		return false
	case "", "ok", "unchanged", "ask", "expect_failed":
		return true
	default:
		// A status added without revisiting this list. Reporting it as
		// refused would be the more dangerous of the two wrong answers, so
		// the unknown case errs toward "something may have happened".
		return true
	}
}

func (s *BrowserAgentSession) blockedResult(action, display string) *BrowserActionResult {
	result := blockedActionResult(action, display)
	if s != nil && s.ID != "" {
		result.SessionID = s.ID
		result.Data["session_id"] = s.ID
	}
	return result
}

func policyBlockResult(s *BrowserAgentSession, action string, err error) (*BrowserActionResult, error) {
	if err == nil {
		return nil, nil
	}
	if isPolicyDenied(err) {
		return s.blockedResult(action, err.Error()), nil
	}
	return nil, err
}

func actionError(result *BrowserActionResult, err error) error {
	if err != nil {
		return err
	}
	if result == nil {
		return fmt.Errorf("empty browser action result")
	}
	switch result.Status {
	case "ok", "", "unchanged":
		return nil
	case "blocked":
		return policyDenied("%s", firstNonEmpty(result.Display, "blocked"))
	default:
		return fmt.Errorf("%s", firstNonEmpty(result.Display, result.Status))
	}
}

func (s *BrowserAgentSession) OpenURL(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	return actionError(s.Navigate(raw))
}

func shouldDenyManagedDownloads(policy BrowserPolicy, mode SessionMode) bool {
	return !policy.AllowDownload && !browserAgentModeUsesUserChrome(mode)
}

func domainMatches(host, candidate string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	if host == "" || candidate == "" {
		return false
	}
	return host == candidate || strings.HasSuffix(host, "."+candidate)
}

func hostFromURL(raw string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(parsed.Hostname()))
}

func currentDomainFromSession(s *BrowserAgentSession) string {
	if s == nil {
		return ""
	}
	s.mu.RLock()
	var snapURL string
	if s.lastSnapshotID != "" && s.snapshots != nil {
		if snap := s.snapshots[s.lastSnapshotID]; snap != nil {
			snapURL = snap.URL
		}
	}
	sess := s.session
	s.mu.RUnlock()
	if host := hostFromURL(snapURL); host != "" {
		return host
	}
	if sess == nil {
		return ""
	}
	info, err := sess.Info()
	if err != nil || info == nil {
		return ""
	}
	return hostFromURL(info.URL)
}
