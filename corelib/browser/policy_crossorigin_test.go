package browser

import "testing"

func TestCrossOriginDenialIsMarkedSeparately(t *testing.T) {
	err := validateNavigationPolicy(BrowserPolicy{}, "https://zhuanlan.zhihu.com/write", "www.youtube.com")
	if err == nil || !isCrossOriginDenied(err) || !isPolicyDenied(err) {
		t.Fatalf("cross-origin refusal not marked: %v", err)
	}
	err = validateNavigationPolicy(BrowserPolicy{BlockedDomains: []string{"zhihu.com"}}, "https://zhuanlan.zhihu.com/write", "www.youtube.com")
	if err == nil || isCrossOriginDenied(err) {
		t.Fatalf("blocked domain must stay a hard refusal: %v", err)
	}
	err = validateNavigationPolicy(BrowserPolicy{AllowedDomains: []string{"youtube.com"}}, "https://zhuanlan.zhihu.com/write", "www.youtube.com")
	if err == nil || isCrossOriginDenied(err) {
		t.Fatalf("allow-list miss must stay a hard refusal: %v", err)
	}
}
