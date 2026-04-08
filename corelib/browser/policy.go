package browser

import (
	"fmt"
	"net/url"
	"strings"
)

func validateNavigationPolicy(policy BrowserPolicy, targetURL, currentDomain string) error {
	parsed, err := url.Parse(strings.TrimSpace(targetURL))
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	host := strings.ToLower(strings.TrimSpace(parsed.Hostname()))
	if host == "" {
		return nil
	}
	for _, blocked := range policy.BlockedDomains {
		if domainMatches(host, blocked) {
			return fmt.Errorf("browser policy blocked domain: %s", host)
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
			return fmt.Errorf("browser policy disallowed domain: %s", host)
		}
	}
	if !policy.AllowCrossOriginNavigation && currentDomain != "" && !strings.EqualFold(currentDomain, host) {
		return fmt.Errorf("browser policy blocked cross-origin navigation from %s to %s", currentDomain, host)
	}
	return nil
}

func domainMatches(host, candidate string) bool {
	host = strings.ToLower(strings.TrimSpace(host))
	candidate = strings.ToLower(strings.TrimSpace(candidate))
	if host == "" || candidate == "" {
		return false
	}
	return host == candidate || strings.HasSuffix(host, "."+candidate)
}

func currentDomainFromSession(s *BrowserAgentSession) string {
	if s == nil || s.session == nil {
		return ""
	}
	info, err := s.session.Info()
	if err != nil || info == nil {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(info.URL))
	if err != nil {
		return ""
	}
	return strings.ToLower(parsed.Hostname())
}
