package browser

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

const (
	documentChallengeChecking = "checking"
	documentChallengeCaptcha  = "captcha"
)

var documentChallengeWaitBudget = 15 * time.Second

type documentChallenge struct {
	DocumentID string
	URL        string
	RequestURL string
	Kind       string
	Vendor     string
	FirstSeen  time.Time
}

type documentIdentity struct {
	ID  string
	URL string
}

func (s *Session) documentIdentity() documentIdentity {
	if s == nil {
		return documentIdentity{}
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return documentIdentity{}
	}
	raw, err := client.Send("Page.getFrameTree", map[string]interface{}{}, 3*time.Second)
	if err != nil {
		return documentIdentity{}
	}
	var payload struct {
		FrameTree struct {
			Frame struct {
				LoaderID string `json:"loaderId"`
				URL      string `json:"url"`
			} `json:"frame"`
		} `json:"frameTree"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return documentIdentity{}
	}
	identity := documentIdentity{ID: payload.FrameTree.Frame.LoaderID, URL: payload.FrameTree.Frame.URL}
	if identity.ID == "" && identity.URL != "" {
		identity.ID = "url:" + identity.URL
	}
	return identity
}

func (s *Session) noteMainFrame(frameID string) {
	if s == nil || strings.TrimSpace(frameID) == "" {
		return
	}
	s.mu.Lock()
	s.mainFrameID = strings.TrimSpace(frameID)
	s.mu.Unlock()
}

func (s *Session) noteDocumentResponse(frameID, rawURL string, status int) {
	if s == nil || strings.TrimSpace(rawURL) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.mainFrameID != "" && frameID != "" && s.mainFrameID != frameID {
		return
	}
	s.documentStatusURL = strings.TrimSpace(rawURL)
	s.documentStatusCode = status
	s.documentStatusFrame = frameID
}

func (s *Session) documentRateLimit(pageURL string) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	rawURL, status := s.documentStatusURL, s.documentStatusCode
	statusFrame, mainFrame := s.documentStatusFrame, s.mainFrameID
	s.mu.Unlock()
	if mainFrame != "" && statusFrame != "" && mainFrame != statusFrame {
		return false
	}
	return status == 429 && documentPathKey(rawURL) != "" && documentPathKey(rawURL) == documentPathKey(pageURL)
}

func (s *Session) historyEntryURL(offset int) (string, error) {
	if s == nil {
		return "", fmt.Errorf("browser session not connected")
	}
	s.mu.Lock()
	client := s.client
	s.mu.Unlock()
	if client == nil {
		return "", fmt.Errorf("browser session not connected")
	}
	raw, err := client.Send("Page.getNavigationHistory", map[string]interface{}{}, 3*time.Second)
	if err != nil {
		return "", err
	}
	var payload struct {
		CurrentIndex int `json:"currentIndex"`
		Entries      []struct {
			URL string `json:"url"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", err
	}
	index := payload.CurrentIndex + offset
	if index < 0 || index >= len(payload.Entries) {
		return "", fmt.Errorf("browser history entry unavailable")
	}
	return payload.Entries[index].URL, nil
}

func (s *Session) beginDocumentChallenge(identity documentIdentity, kind, vendor string) time.Time {
	if s == nil {
		return time.Now()
	}
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.challenge != nil && s.challenge.DocumentID == identity.ID {
		return s.challenge.FirstSeen
	}
	s.challenge = &documentChallenge{
		DocumentID: identity.ID,
		URL:        identity.URL,
		Kind:       kind,
		Vendor:     vendor,
		FirstSeen:  now,
	}
	return now
}

func (s *Session) finishDocumentChallenge(identity documentIdentity, kind, vendor string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind == "" {
		s.challenge = nil
		return
	}
	firstSeen := time.Now()
	requestURL := ""
	if s.challenge != nil && s.challenge.DocumentID == identity.ID {
		firstSeen = s.challenge.FirstSeen
		requestURL = s.challenge.RequestURL
	}
	s.challenge = &documentChallenge{DocumentID: identity.ID, URL: identity.URL, RequestURL: requestURL, Kind: kind, Vendor: vendor, FirstSeen: firstSeen}
}

func (s *Session) noteChallengeRequest(raw string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.challenge != nil && s.challenge.RequestURL == "" {
		s.challenge.RequestURL = strings.TrimSpace(raw)
	}
}

func (s *Session) currentDocumentChallenge() *documentChallenge {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	tracked := s.challenge
	s.mu.Unlock()
	if tracked == nil {
		return nil
	}
	copy := *tracked
	identity := s.documentIdentity()
	if identity.ID != "" && copy.DocumentID != "" && identity.ID != copy.DocumentID {
		s.mu.Lock()
		if s.challenge == tracked {
			s.challenge = nil
		}
		s.mu.Unlock()
		return nil
	}
	if identity.URL != "" {
		copy.URL = identity.URL
	}
	return &copy
}

func (s *Session) challengeForNavigation(target string) *documentChallenge {
	tracked := s.currentDocumentChallenge()
	if tracked == nil {
		return nil
	}
	targetKey := documentPathKey(target)
	currentKey := documentPathKey(tracked.URL)
	requestKey := documentPathKey(tracked.RequestURL)
	if targetKey == "" || (targetKey != currentKey && (requestKey == "" || targetKey != requestKey)) {
		return nil
	}
	return tracked
}

func (s *Session) waitForChallengeSnapshot() string {
	tracked := s.currentDocumentChallenge()
	if tracked == nil {
		return ""
	}
	report, ok := s.waitPageReady()
	if !ok || (!report.ChallengeCleared && report.Wall != documentChallengeChecking && report.Wall != documentChallengeCaptcha) {
		return ""
	}
	return strings.TrimSpace(report.Note())
}

func browserNavigationSent(result *BrowserActionResult) bool {
	if result == nil || result.Data == nil {
		return true
	}
	sent, ok := result.Data["navigation_sent"].(bool)
	return !ok || sent
}

func (s *BrowserAgentSession) challengeMutationResult(action string) *BrowserActionResult {
	if s == nil || s.session == nil || !isCaptchaMutatingAction(action) {
		return nil
	}
	return s.documentChallengeResult(action, s.session.currentDocumentChallenge())
}

func (s *BrowserAgentSession) documentChallengeResult(action string, tracked *documentChallenge) *BrowserActionResult {
	if tracked == nil {
		return nil
	}
	switch tracked.Kind {
	case documentChallengeChecking:
		return &BrowserActionResult{
			SessionID: s.ID,
			Action:    "browser_" + strings.TrimPrefix(action, "browser_"),
			Status:    "ok",
			Detail:    "browser_challenge_checking",
			Display:   "站点正在当前文档内自动进行安全检查，这次操作未发送。不要刷新、重新打开或点击；probe 会复用当前文档剩余的等待时间。",
			Data: map[string]interface{}{
				"reason": "browser_challenge_checking",
				"vendor": tracked.Vendor,
			},
			batchStop:       true,
			batchStopReason: "browser_challenge",
		}
	case documentChallengeCaptcha:
		return s.documentChallengeAskResult(action, tracked)
	case "rate_limit":
		return s.frequencyHoldResult("browser_"+strings.TrimPrefix(action, "browser_"), frequencyRefusalStopText(true))
	default:
		return nil
	}
}

func (s *BrowserAgentSession) documentChallengeAskResult(action string, tracked *documentChallenge) *BrowserActionResult {
	pageURL := ""
	vendor := ""
	if tracked != nil {
		pageURL = tracked.URL
		vendor = tracked.Vendor
	}
	action = "browser_" + strings.TrimPrefix(action, "browser_")
	if err := s.handPageToUser("captcha", pageURL, ""); err != nil && previewHandoffEnabled() {
		display := "用户已在预览中处理安全检查。先 probe，再继续。不要刷新或重复提交。"
		if !errors.Is(err, errUserReleased) {
			display = err.Error()
		}
		return &BrowserActionResult{
			SessionID: s.ID,
			Action:    action,
			Status:    "blocked",
			Display:   display,
			Detail:    "browser_challenge",
			Data:      map[string]interface{}{"reason": "browser_challenge", "vendor": vendor, "url": pageURL},
		}
	}
	request := captchaAskUserRequest(pageURL)
	request.Question = "页面的安全检查没有自动完成。请在右侧预览窗点「接管」完成检查；不要刷新页面。继续之后必须先 probe，再 click/type。"
	return &BrowserActionResult{
		SessionID: s.ID,
		Action:    action,
		Status:    "ask",
		Display:   "page needs the person to complete its security check in the MaClaw preview; do not reload, then probe before acting",
		Detail:    "browser_challenge",
		AskUser:   request,
		Data:      map[string]interface{}{"reason": "browser_challenge", "vendor": vendor, "url": pageURL},
	}
}
