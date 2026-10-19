package desktop

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// ScreenStatus is one screen as the in-container file service reports it.
// CDP is the address that service already bound. Display is not a port formula.
type ScreenStatus struct {
	Display string
	CDP     string
	Profile string
	Token   string
}

// ParseScreen reads the file service's one-line screen answer.
// A missing CDP address is an error. This function does not invent one
// from the display, and the error does not include the gate token.
func ParseScreen(out string) (ScreenStatus, error) {
	var status ScreenStatus
	found := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var payload struct {
			Display string `json:"display"`
			CDP     string `json:"cdp"`
			Profile string `json:"profile"`
			Token   string `json:"token"`
		}
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			return ScreenStatus{}, fmt.Errorf("%w: desktop supervisor returned invalid screen", ErrInvalid)
		}
		status = ScreenStatus{
			Display: payload.Display,
			CDP:     payload.CDP,
			Profile: payload.Profile,
			Token:   payload.Token,
		}
		found = true
	}
	if !found {
		return ScreenStatus{}, fmt.Errorf("%w: desktop supervisor returned no screen", ErrInvalid)
	}
	if err := checkScreen(status); err != nil {
		return ScreenStatus{}, err
	}
	return status, nil
}

func checkScreen(status ScreenStatus) error {
	display := strings.TrimSpace(status.Display)
	if !ValidDisplay(display) {
		return fmt.Errorf("%w: desktop supervisor returned no screen", ErrInvalid)
	}
	number, err := strconv.Atoi(strings.TrimPrefix(display, ":"))
	if err != nil || number <= 0 {
		return fmt.Errorf("%w: desktop supervisor returned no screen", ErrInvalid)
	}
	if err := checkScreenCDP(status.CDP); err != nil {
		return err
	}
	if err := checkScreenProfile(number, status.Profile); err != nil {
		return err
	}
	if !ValidGateToken(strings.TrimSpace(status.Token)) {
		return fmt.Errorf("%w: desktop supervisor returned an invalid token", ErrInvalid)
	}
	return nil
}

func checkScreenCDP(raw string) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme != "http" || parsed.User != nil {
		return fmt.Errorf("%w: desktop supervisor returned no cdp address", ErrInvalid)
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.EscapedPath() != "" && parsed.EscapedPath() != "/") {
		return fmt.Errorf("%w: desktop supervisor returned no cdp address", ErrInvalid)
	}
	host, port, err := net.SplitHostPort(parsed.Host)
	if err != nil || host != "127.0.0.1" {
		return fmt.Errorf("%w: desktop supervisor returned no cdp address", ErrInvalid)
	}
	n, err := strconv.Atoi(port)
	if err != nil || n <= 0 || n > 65535 {
		return fmt.Errorf("%w: desktop supervisor returned no cdp address", ErrInvalid)
	}
	return nil
}

func checkScreenProfile(display int, profile string) error {
	profile = strings.TrimSpace(profile)
	if profile == "" || strings.Contains(profile, `\`) || strings.Contains(profile, "..") || path.Clean(profile) != profile {
		return fmt.Errorf("%w: desktop supervisor returned no profile", ErrInvalid)
	}
	parts := strings.Split(profile, "/")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "desktops" || parts[3] != "screens" || parts[5] != "profile" {
		return fmt.Errorf("%w: desktop supervisor returned no profile", ErrInvalid)
	}
	if !ValidUserKey(parts[2]) || parts[4] != strconv.Itoa(display) {
		return fmt.Errorf("%w: desktop supervisor returned no profile", ErrInvalid)
	}
	return nil
}
