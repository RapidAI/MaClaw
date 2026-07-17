package commands

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/corelib/weixin"
	qrcode "github.com/skip2/go-qrcode"
)

// RunOnboarding guides first-time users through the settings that are painful
// to type by hand in a pure terminal: Hub email activation and WeChat binding.
func RunOnboarding(args []string) error {
	fs := flag.NewFlagSet("onboarding", flag.ExitOnError)
	emailFlag := fs.String("email", "", "Hub account email")
	hubCenterFlag := fs.String("hubcenter", "", "HubCenter URL used to select Hub automatically")
	invitationCode := fs.String("invitation-code", "", "Hub invitation code, if required")
	skipRemote := fs.Bool("skip-remote", false, "skip Hub remote activation")
	skipWeixin := fs.Bool("skip-weixin", false, "skip WeChat binding")
	showQRPayload := fs.Bool("show-qr-payload", false, "print raw WeChat QR payload on trusted terminals")
	yes := fs.Bool("yes", false, "accept recommended onboarding steps")
	fs.Parse(args)

	store := NewFileConfigStore(ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	reader := bufio.NewReader(os.Stdin)
	Println("MaClaw TUI onboarding")
	Println("This wizard can activate Hub remote mode and bind WeChat for terminal-only environments.")
	Println()

	email := strings.TrimSpace(*emailFlag)
	if email == "" {
		email = strings.TrimSpace(cfg.RemoteEmail)
	}
	if !*skipRemote && email == "" {
		prompted, err := promptLine(reader, "Email for Hub activation (leave blank to skip): ")
		if err != nil {
			return err
		}
		email = strings.TrimSpace(prompted)
	}
	if !*skipRemote && email != "" && !validOnboardingEmailForCLI(email) {
		return fmt.Errorf("invalid Hub activation email: %s", email)
	}

	if !*skipRemote && email != "" {
		hubCenterURL := selectedHubCenterForOnboarding(cfg, strings.TrimSpace(*hubCenterFlag))
		if !*yes && strings.TrimSpace(*hubCenterFlag) == "" {
			chosen, err := promptHubCenter(reader, hubCenterOnboardingChoices(cfg, hubCenterURL), hubCenterURL)
			if err != nil {
				return err
			}
			hubCenterURL = chosen
		}
		if !validOnboardingHubCenterForCLI(hubCenterURL) {
			return fmt.Errorf("HubCenter must be a valid http(s) URL: %s", hubCenterURL)
		}
		cfg.RemoteEmail = email
		cfg.RemoteHubCenterURL = hubCenterURL
		if err := store.SaveConfig(cfg); err != nil {
			return fmt.Errorf("save email and HubCenter: %w", err)
		}
		activated := remoteActivationComplete(cfg)
		if activated {
			Printf("Hub remote mode is already activated for %s.\n", email)
			Printf("HubCenter: %s\n", orDefault(cfg.RemoteHubCenterURL, remote.DefaultRemoteHubCenterURL))
		} else if *yes || promptYesNo(reader, "Activate this machine with Hub now?", true) {
			if err := activateRemoteForOnboarding(context.Background(), store, cfg, email, strings.TrimSpace(*invitationCode)); err != nil {
				return err
			}
			cfg, _ = store.LoadConfig()
		}
	}

	if !*skipWeixin {
		bound := cfg.WeixinEnabled && cfg.WeixinToken != ""
		if bound {
			Printf("WeChat is already bound (account_id=%s).\n", orDefault(cfg.WeixinAccountID, "unknown"))
		} else if *yes || promptYesNo(reader, "Bind WeChat by scanning a QR code?", true) {
			if err := bindWeixinForOnboarding(context.Background(), store, cfg, *showQRPayload); err != nil {
				return err
			}
			cfg, _ = store.LoadConfig()
		}
	}

	cfg.OnboardingDone = true
	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("save onboarding state: %w", err)
	}
	Println()
	Println("Onboarding complete. Run `maclaw-tui status` to review the next step, or run `maclaw-tui` to continue.")
	return nil
}

func validOnboardingEmailForCLI(email string) bool {
	email = strings.TrimSpace(email)
	if email == "" || strings.ContainsAny(email, " \t\r\n") {
		return false
	}
	at := strings.LastIndex(email, "@")
	if at <= 0 || at >= len(email)-1 {
		return false
	}
	domain := email[at+1:]
	return strings.Contains(domain, ".") && !strings.HasPrefix(domain, ".") && !strings.HasSuffix(domain, ".")
}

func validOnboardingHubCenterForCLI(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != ""
}

func selectedHubCenterForOnboarding(cfg corelib.AppConfig, flagValue string) string {
	if flagValue != "" {
		return strings.TrimRight(flagValue, "/")
	}
	if cfg.RemoteHubCenterURL != "" {
		return strings.TrimRight(strings.TrimSpace(cfg.RemoteHubCenterURL), "/")
	}
	return remote.DefaultRemoteHubCenterURL
}

func hubCenterOnboardingChoices(cfg corelib.AppConfig, current string) []string {
	values := []string{current, cfg.RemoteHubCenterURL}
	values = append(values, cfg.RemoteHubCenterURLs...)
	values = append(values, remote.DefaultRemoteHubCenterURLs...)
	return uniqueOnboardingHubCenters(values...)
}

func uniqueOnboardingHubCenters(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimRight(strings.TrimSpace(value), "/")
		if value == "" {
			continue
		}
		found := false
		for _, existing := range out {
			if existing == value {
				found = true
				break
			}
		}
		if !found {
			out = append(out, value)
		}
	}
	return out
}

func promptHubCenter(reader *bufio.Reader, choices []string, current string) (string, error) {
	if len(choices) == 0 {
		choices = []string{remote.DefaultRemoteHubCenterURL}
	}
	Println("HubCenter choices:")
	for i, choice := range choices {
		marker := " "
		if choice == current {
			marker = "*"
		}
		Printf("  %s%d) %s\n", marker, i+1, choice)
	}
	Println("  m) Manual input (private HubCenter)")
	answer, err := promptLine(reader, "HubCenter choice (number, Enter keeps current, m for manual): ")
	if err != nil {
		return "", err
	}
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return current, nil
	}
	if idx, err := strconv.Atoi(answer); err == nil && idx >= 1 && idx <= len(choices) {
		return choices[idx-1], nil
	}
	if answer == "m" || answer == "M" || strings.EqualFold(answer, "manual") {
		manual, err := promptLine(reader, "Private HubCenter URL: ")
		if err != nil {
			return "", err
		}
		manual = strings.TrimRight(strings.TrimSpace(manual), "/")
		if manual == "" {
			return current, nil
		}
		return manual, nil
	}
	if strings.HasPrefix(answer, "http://") || strings.HasPrefix(answer, "https://") {
		return strings.TrimRight(answer, "/"), nil
	}
	Printf("Unknown HubCenter choice %q; keeping current.\n", answer)
	return current, nil
}

func activateRemoteForOnboarding(ctx context.Context, store *FileConfigStore, cfg corelib.AppConfig, email, invCode string) error {
	Printf("Activating Hub remote mode for %s ...\n", email)
	profile := buildRemoteEnrollmentProfile(cfg, email, invCode)

	result, err := remote.NewEnrollmentClient().Enroll(ctx, profile)
	if err != nil {
		return fmt.Errorf("Hub activation failed: %w", err)
	}

	cfg.RemoteEmail = result.Email
	cfg.RemoteSN = result.SN
	cfg.RemoteUserID = result.UserID
	cfg.RemoteMachineID = result.MachineID
	cfg.RemoteMachineToken = result.MachineToken
	cfg.RemoteHubURL = result.HubURL
	cfg.RemoteEnabled = true
	cfg.DefaultLaunchMode = "remote"
	if result.ViewerToken != "" {
		cfg.RemoteViewerToken = result.ViewerToken
	}
	if result.ClientID != "" && cfg.RemoteClientID == "" {
		cfg.RemoteClientID = result.ClientID
	}
	if result.HubCenterURL != "" && !remote.IsLoopbackURL(result.HubCenterURL) {
		cfg.RemoteHubCenterURL = result.HubCenterURL
	}
	if len(result.DiscoveredURLs) > 0 {
		cfg.RemoteHubCenterURLs = remote.NormalizeHubCenterURLs(result.DiscoveredURLs)
	}
	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("save Hub credentials: %w", err)
	}

	Println("Hub activation succeeded.")
	Printf("  Hub URL:    %s\n", result.HubURL)
	Printf("  Machine ID: %s\n", result.MachineID)
	if result.SN != "" {
		Printf("  SN:         %s\n", result.SN)
	}
	return nil
}

func bindWeixinForOnboarding(ctx context.Context, store *FileConfigStore, cfg corelib.AppConfig, showPayload bool) error {
	baseURL := strings.TrimSpace(cfg.WeixinBaseURL)
	if baseURL == "" {
		baseURL = weixin.DefaultBaseURL
	}

	Println("Requesting WeChat QR code ...")
	qrContent, qrToken, err := weixin.StartQRLogin(ctx, baseURL, weixin.DefaultBotType)
	if err != nil {
		return fmt.Errorf("request WeChat QR code: %w", err)
	}
	if qrContent == "" || qrToken == "" {
		return fmt.Errorf("WeChat QR response is incomplete")
	}

	Println()
	if rendered, err := renderTerminalQR(qrContent); err == nil {
		Println(rendered)
	} else {
		Printf("QR render failed: %v\n", err)
	}
	Println("Scan this QR code with WeChat, then confirm login on the phone.")
	Println(weixinQRPayloadLine(qrContent, showPayload))
	Println("Waiting for confirmation, press Ctrl+C to cancel.")

	waitCtx, cancel := context.WithTimeout(ctx, 8*time.Minute)
	defer cancel()
	lastStatus := weixin.QRLoginStatusUnknown
	for {
		result, status, err := weixin.PollQRStatus(waitCtx, baseURL, qrToken)
		if err != nil {
			if weixin.IsQRLoginRetryableError(err) {
				Printf("WeChat QR status check failed; retrying: %v\n", err)
				select {
				case <-waitCtx.Done():
					return fmt.Errorf("WeChat binding timed out")
				case <-time.After(2 * time.Second):
				}
				continue
			}
			return fmt.Errorf("poll WeChat QR status: %w", err)
		}
		if status != lastStatus {
			Printf("WeChat QR status: %s\n", weixinQRStatusLabel(status))
			lastStatus = status
		}
		switch status {
		case weixin.QRLoginStatusConfirmed:
			if result == nil || !result.Connected {
				msg := "login was not confirmed"
				if result != nil && result.Message != "" {
					msg = result.Message
				}
				return fmt.Errorf("WeChat binding failed: %s", msg)
			}
			return saveWeixinLoginResult(store, cfg, result)
		case weixin.QRLoginStatusExpired:
			return fmt.Errorf("WeChat QR code expired; run onboarding again to get a fresh code")
		}

		select {
		case <-waitCtx.Done():
			return fmt.Errorf("WeChat binding timed out")
		case <-time.After(1 * time.Second):
		}
	}
}

func weixinQRPayloadLine(content string, show bool) string {
	if show {
		return "QR payload: " + content
	}
	return "QR payload hidden. Re-run onboarding with --show-qr-payload only on a trusted terminal if QR scanning fails."
}

func saveWeixinLoginResult(store *FileConfigStore, cfg corelib.AppConfig, result *weixin.QRLoginResult) error {
	cfg.WeixinEnabled = true
	cfg.WeixinToken = result.BotToken
	cfg.WeixinAccountID = result.AccountID
	if result.BaseURL != "" {
		cfg.WeixinBaseURL = result.BaseURL
	}
	if cfg.WeixinLocalMode == nil {
		local := true
		cfg.WeixinLocalMode = &local
	}
	if err := store.SaveConfig(cfg); err != nil {
		return fmt.Errorf("save WeChat credentials: %w", err)
	}
	Println("WeChat binding succeeded.")
	if result.AccountID != "" {
		Printf("  Account ID: %s\n", result.AccountID)
	}
	return nil
}

func renderTerminalQR(content string) (string, error) {
	qr, err := qrcode.New(content, qrcode.Low)
	if err != nil {
		return "", err
	}
	bitmap := qr.Bitmap()
	if len(bitmap) == 0 {
		return "", fmt.Errorf("empty QR bitmap")
	}

	const quiet = 2
	size := len(bitmap) + quiet*2
	var b strings.Builder
	for y := 0; y < size; y += 2 {
		for x := 0; x < size; x++ {
			top := terminalQRModule(bitmap, x, y, quiet)
			bottom := false
			if y+1 < size {
				bottom = terminalQRModule(bitmap, x, y+1, quiet)
			}
			b.WriteString(terminalQRHalfBlock(top, bottom))
		}
		b.WriteByte('\n')
	}
	return b.String(), nil
}

func terminalQRModule(bitmap [][]bool, x, y, quiet int) bool {
	yy := y - quiet
	xx := x - quiet
	if yy < 0 || yy >= len(bitmap) || xx < 0 || len(bitmap) == 0 || xx >= len(bitmap[yy]) {
		return false
	}
	return bitmap[yy][xx]
}

func terminalQRHalfBlock(topBlack, bottomBlack bool) string {
	fg := 37
	if topBlack {
		fg = 30
	}
	bg := 47
	if bottomBlack {
		bg = 40
	}
	return fmt.Sprintf("\x1b[%d;%dm▀▀\x1b[0m", fg, bg)
}

func weixinQRStatusLabel(status weixin.QRLoginStatus) string {
	switch status {
	case weixin.QRLoginStatusWait:
		return "waiting for scan"
	case weixin.QRLoginStatusScanned:
		return "scanned, waiting for phone confirmation"
	case weixin.QRLoginStatusConfirmed:
		return "confirmed"
	case weixin.QRLoginStatusExpired:
		return "expired"
	default:
		return status.String()
	}
}

func promptLine(reader *bufio.Reader, label string) (string, error) {
	Print(label)
	line, err := reader.ReadString('\n')
	if err != nil && len(line) == 0 {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func promptYesNo(reader *bufio.Reader, label string, def bool) bool {
	suffix := " [Y/n]: "
	if !def {
		suffix = " [y/N]: "
	}
	answer, err := promptLine(reader, label+suffix)
	if err != nil {
		return def
	}
	answer = strings.ToLower(strings.TrimSpace(answer))
	if answer == "" {
		return def
	}
	return answer == "y" || answer == "yes" || answer == "1"
}
