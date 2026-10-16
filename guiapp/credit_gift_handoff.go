package guiapp

import (
	"net/url"
	"strings"
)

// CreditGiftLaunch is a maclaw://credit/<code> open. Only the code is kept.
// The desktop claims against the HubCenter already configured in the app.
type CreditGiftLaunch struct {
	Code string `json:"code"`
}

func (a *App) setPendingCreditGift(launch CreditGiftLaunch) {
	code := canonicalGiftCode(launch.Code)
	if a == nil || code == "" {
		return
	}
	a.creditGiftMu.Lock()
	a.pendingCreditGift = CreditGiftLaunch{Code: code}
	ready := a.ctx != nil && a.hasWailsEventsContext()
	a.creditGiftMu.Unlock()
	if ready {
		a.emitEvent(EventTokenBankCredit, CreditGiftLaunch{Code: code})
	}
}

func (a *App) peekPendingCreditGift() CreditGiftLaunch {
	if a == nil {
		return CreditGiftLaunch{}
	}
	a.creditGiftMu.Lock()
	defer a.creditGiftMu.Unlock()
	return a.pendingCreditGift
}

func (a *App) takePendingCreditGift() CreditGiftLaunch {
	if a == nil {
		return CreditGiftLaunch{}
	}
	a.creditGiftMu.Lock()
	defer a.creditGiftMu.Unlock()
	launch := a.pendingCreditGift
	a.pendingCreditGift = CreditGiftLaunch{}
	return launch
}

// ConsumeCreditGiftHandoff returns a deep link that arrived before the React
// listener was installed, then forgets it.
func (a *App) ConsumeCreditGiftHandoff() CreditGiftLaunch {
	return a.takePendingCreditGift()
}

func creditGiftFromArgs(args []string) CreditGiftLaunch {
	for _, arg := range args {
		if code := giftCodeFromLaunchArg(arg); code != "" {
			return CreditGiftLaunch{Code: code}
		}
	}
	return CreditGiftLaunch{}
}

func giftCodeFromLaunchArg(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed == nil {
		return ""
	}
	path := strings.Trim(parsed.EscapedPath(), "/")
	if decoded, decodeErr := url.PathUnescape(path); decodeErr == nil {
		path = decoded
	}
	switch {
	case strings.EqualFold(parsed.Scheme, "maclaw") && strings.EqualFold(parsed.Host, "credit"):
		if i := strings.LastIndex(path, "/"); i >= 0 {
			path = path[i+1:]
		}
		return canonicalGiftCode(path)
	case strings.EqualFold(parsed.Scheme, "https") || strings.EqualFold(parsed.Scheme, "http"):
		const marker = "/c/"
		idx := strings.Index(strings.ToLower(parsed.Path), marker)
		if idx < 0 {
			return ""
		}
		rest := parsed.Path[idx+len(marker):]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			rest = rest[:slash]
		}
		if decoded, decodeErr := url.PathUnescape(rest); decodeErr == nil {
			rest = decoded
		}
		return canonicalGiftCode(rest)
	default:
		return ""
	}
}

func canonicalGiftCode(raw string) string {
	code := strings.ToUpper(strings.TrimSpace(raw))
	if len(code) != 10 {
		return ""
	}
	for _, r := range code {
		switch {
		case r >= 'A' && r <= 'Z':
		case r >= '2' && r <= '7':
		default:
			return ""
		}
	}
	return code
}
