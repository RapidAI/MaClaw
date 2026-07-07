package views

import (
	"net/http"
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func init() {
	onboardingTigerClawDefault = false
}

func TestOnboardingHubCenterDefaultsAndLoadsConfig(t *testing.T) {
	m := NewOnboardingModel("zh")
	if got := m.hubCenterInput.Value(); got != remote.DefaultRemoteHubCenterURL {
		t.Fatalf("default HubCenter = %q, want %q", got, remote.DefaultRemoteHubCenterURL)
	}

	cfg := corelib.AppConfig{
		RemoteEmail:         "user@example.com",
		RemoteHubCenterURL:  "https://center.example/",
		RemoteHubCenterURLs: []string{"https://backup.example/"},
	}
	m.LoadFromAppConfig(cfg)
	if got := m.emailInput.Value(); got != "user@example.com" {
		t.Fatalf("email input = %q", got)
	}
	if got := m.hubCenterInput.Value(); got != "https://center.example" {
		t.Fatalf("HubCenter input = %q", got)
	}
	for _, want := range []string{"https://center.example", "https://backup.example", remote.DefaultRemoteHubCenterURLs[0]} {
		if !containsString(m.hubCenterOptions, want) {
			t.Fatalf("HubCenter options missing %q: %#v", want, m.hubCenterOptions)
		}
	}
}

func TestOnboardingPrefillsEmailAndHubCenterFromEnv(t *testing.T) {
	t.Setenv("MACLAW_REMOTE_EMAIL", "env-user@example.com")
	t.Setenv("MACLAW_REMOTE_HUBCENTER_URL", "https://env-center.example/")

	m := NewOnboardingModel("en")
	if got := m.emailInput.Value(); got != "env-user@example.com" {
		t.Fatalf("email input = %q", got)
	}
	if got := m.hubCenterInput.Value(); got != "https://env-center.example" {
		t.Fatalf("HubCenter input = %q", got)
	}
	if m.cursor != onboardingRowActivate {
		t.Fatalf("cursor = %d, want activate row when env email is present", m.cursor)
	}
	if m.emailInput.Focused() || m.hubCenterInput.Focused() {
		t.Fatal("prefilled setup should focus the activate action instead of a text field")
	}
}

func TestOnboardingSetInitialEmailFocusesActivate(t *testing.T) {
	m := NewOnboardingModel("en")
	m.SetInitialEmail(" USER@Example.COM ")
	if got := m.EmailValueForTest(); got != "user@example.com" {
		t.Fatalf("email input = %q", got)
	}
	if m.cursor != onboardingRowActivate {
		t.Fatalf("cursor = %d, want activate row", m.cursor)
	}
	if m.emailInput.Focused() {
		t.Fatal("prefilled setup should focus the activate action")
	}
}

func TestOnboardingIgnoresInvalidEnvHubCenter(t *testing.T) {
	t.Setenv("MACLAW_HUBCENTER_URL", "center.example")

	m := NewOnboardingModel("en")
	if got := m.hubCenterInput.Value(); got != remote.DefaultRemoteHubCenterURL {
		t.Fatalf("invalid env HubCenter should keep default, got %q", got)
	}
}

func TestOnboardingLanguageRowCyclesAndEmitsSaveMessage(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.cursor = onboardingRowLanguage

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if cmd == nil {
		t.Fatal("expected language change command")
	}
	msg, ok := cmd().(OnboardingLanguageChangedMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	if msg.Language != "en" || m.lang != "en" {
		t.Fatalf("language change = %#v, model lang=%q", msg, m.lang)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Language") || !strings.Contains(view, "English") {
		t.Fatalf("language row should switch immediately:\n%s", view)
	}
}

func TestOnboardingLoadConfigAppliesLanguage(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{Language: "en"})
	if m.lang != "en" {
		t.Fatalf("lang = %q, want en", m.lang)
	}
	if got := onboardingText(m.lang, "title"); got != "First-run Setup" {
		t.Fatalf("title after load = %q", got)
	}
}

func TestOnboardingSpaceCyclesHubCenter(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubCenterURL:  "https://center.example",
		RemoteHubCenterURLs: []string{"https://backup.example"},
	})
	m.cursor = onboardingRowHubCenter
	m.focusCursor()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if got := m.hubCenterInput.Value(); got != "https://backup.example" {
		t.Fatalf("Space cycled HubCenter to %q, want backup", got)
	}
}

func TestOnboardingActivateMessageIncludesHubCenter(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.emailInput.SetValue("user@example.com")
	m.hubCenterInput.SetValue("https://center.example/")
	m.cursor = onboardingRowActivate
	m.focusCursor()

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected resolve command")
	}
	if !m.remoteBusy {
		t.Fatal("expected remoteBusy after activation")
	}
	msg, ok := cmd().(OnboardingResolveIdentityMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	if msg.Identity != "user@example.com" {
		t.Fatalf("Identity = %q", msg.Identity)
	}
	if msg.HubCenterURL != "https://center.example" {
		t.Fatalf("HubCenterURL = %q", msg.HubCenterURL)
	}
}

func TestOnboardingLoadSavedEmailFocusesActivate(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{RemoteEmail: "user@example.com"})
	if m.cursor != onboardingRowActivate {
		t.Fatalf("cursor = %d, want activate row", m.cursor)
	}
	if got := m.remoteStatus; got != onboardingText("zh", "emailSaved") {
		t.Fatalf("remote status = %q", got)
	}
}

func TestOnboardingLoadSavedEmailClearsTextFocus(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{RemoteEmail: "user@example.com"})
	if m.emailInput.Focused() || m.hubCenterInput.Focused() {
		t.Fatalf("text input should not stay focused when cursor moves to activate row")
	}
}

func TestOnboardingEnterOnEmailStartsActivation(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.emailInput.SetValue(" user@example.com ")
	m.cursor = onboardingRowEmail
	m.focusCursor()

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected resolve command")
	}
	if !m.remoteBusy {
		t.Fatal("expected remoteBusy after activation")
	}
	msg, ok := cmd().(OnboardingResolveIdentityMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	if msg.Identity != "user@example.com" {
		t.Fatalf("Identity = %q", msg.Identity)
	}
}

func TestOnboardingEnterOnHubCenterOpensSelector(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.emailInput.SetValue("user@example.com")
	m.hubCenterInput.SetValue("https://center.example/")
	m.cursor = onboardingRowHubCenter
	m.focusCursor()

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatalf("opening HubCenter selector should not start activation: %v", cmd)
	}
	if !m.hubCenterSelect || m.remoteBusy {
		t.Fatalf("expected HubCenter selector without activation, selector=%v remoteBusy=%v", m.hubCenterSelect, m.remoteBusy)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "手动输入") {
		t.Fatalf("HubCenter selector should include manual fallback:\n%s", view)
	}
}

func TestOnboardingHubCenterSelectorChoosesOption(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubCenterURL:  "https://center.example",
		RemoteHubCenterURLs: []string{"https://backup.example"},
	})
	m.cursor = onboardingRowHubCenter
	m.focusCursor()

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.hubCenterCursor = 1
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.hubCenterSelect {
		t.Fatal("selector should close after choosing an option")
	}
	if got := m.hubCenterInput.Value(); got != "https://backup.example" {
		t.Fatalf("HubCenter input = %q, want backup option", got)
	}
	if m.hubCenterInput.Focused() {
		t.Fatal("choosing a HubCenter preset should keep the row read-only")
	}
	before := m.hubCenterInput.Value()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("typed")})
	if got := m.hubCenterInput.Value(); got != before {
		t.Fatalf("preset HubCenter row should ignore typing, got %q want %q", got, before)
	}
}

func TestOnboardingHubCenterSelectorManualFallbackFocusesInput(t *testing.T) {
	m := NewOnboardingModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{RemoteHubCenterURL: "https://center.example"})
	m.cursor = onboardingRowHubCenter
	m.focusCursor()

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.hubCenterCursor = len(m.hubCenterOptions)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.hubCenterSelect {
		t.Fatal("selector should close after choosing manual input")
	}
	if !m.hubCenterInput.Focused() {
		t.Fatal("manual fallback should return focus to the HubCenter text input")
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if !strings.Contains(m.hubCenterInput.Value(), "x") {
		t.Fatalf("manual HubCenter input should accept typing, got %q", m.hubCenterInput.Value())
	}
}

func TestOnboardingRemoteResultShowsSelectedHub(t *testing.T) {
	m := NewOnboardingModel("en")
	m, _ = m.Update(OnboardingRemoteResultMsg{Success: true, HubURL: "https://hub.example/", MachineID: "machine-1", MachineReady: true})
	if m.hubURL != "https://hub.example" {
		t.Fatalf("hubURL = %q, want https://hub.example", m.hubURL)
	}
	if !m.remoteDone {
		t.Fatal("expected remoteDone after successful activation")
	}
}

func TestOnboardingRemoteFailureGuidesRetry(t *testing.T) {
	m := NewOnboardingModel("en")
	m.emailInput.SetValue("user@example.com")
	m.cursor = onboardingRowActivate
	m.focusCursor()

	m, _ = m.Update(OnboardingRemoteResultMsg{Success: false, Message: "network timeout"})
	if m.remoteDone {
		t.Fatal("failed activation should not be marked done")
	}
	if !m.remoteFailed {
		t.Fatal("failed activation should set retry state")
	}
	if m.cursor != onboardingRowActivate {
		t.Fatalf("cursor = %d, want activate row", m.cursor)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "network timeout") || !strings.Contains(view, "Check your input") || !strings.Contains(view, "press Enter to retry") {
		t.Fatalf("failed activation should show error and retry guidance:\n%s", view)
	}

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected retry activation command")
	}
	if m.remoteFailed {
		t.Fatal("starting retry should clear failure state")
	}
	if _, ok := cmd().(OnboardingResolveIdentityMsg); !ok {
		t.Fatalf("command returned %T, want OnboardingResolveIdentityMsg", cmd())
	}
}

func TestOnboardingRemoteFailureClearsAfterEditingEmail(t *testing.T) {
	m := NewOnboardingModel("en")
	m.emailInput.SetValue("user@example.com")
	m.cursor = onboardingRowActivate
	m.focusCursor()
	m, _ = m.Update(OnboardingRemoteResultMsg{Success: false, Message: "network timeout"})

	m.cursor = onboardingRowEmail
	m.focusCursor()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")})
	if m.remoteFailed {
		t.Fatal("editing email should clear stale activation failure")
	}
	if got := m.remoteStatus; got != onboardingText("en", "emailSaved") {
		t.Fatalf("remote status = %q", got)
	}
	view := stripANSIForTest(m.View())
	if strings.Contains(view, "Check your input") {
		t.Fatalf("stale failure retry hint should clear after editing email:\n%s", view)
	}
}

func TestOnboardingRemoteFailureClearsAfterCyclingHubCenter(t *testing.T) {
	m := NewOnboardingModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubCenterURL:  "https://center.example",
		RemoteHubCenterURLs: []string{"https://backup.example"},
	})
	m.emailInput.SetValue("user@example.com")
	m.cursor = onboardingRowActivate
	m.focusCursor()
	m, _ = m.Update(OnboardingRemoteResultMsg{Success: false, Message: "network timeout"})

	m.cursor = onboardingRowHubCenter
	m.focusCursor()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	if m.remoteFailed {
		t.Fatal("cycling HubCenter should clear stale activation failure")
	}
	if got := m.remoteStatus; got != onboardingText("en", "emailSaved") {
		t.Fatalf("remote status = %q", got)
	}
}

func TestOnboardingRemoteFailureClearsAfterSelectingHubCenter(t *testing.T) {
	m := NewOnboardingModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubCenterURL:  "https://center.example",
		RemoteHubCenterURLs: []string{"https://backup.example"},
	})
	m.emailInput.SetValue("user@example.com")
	m.cursor = onboardingRowActivate
	m.focusCursor()
	m, _ = m.Update(OnboardingRemoteResultMsg{Success: false, Message: "network timeout"})

	m.cursor = onboardingRowHubCenter
	m.focusCursor()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m.hubCenterCursor = 1
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.remoteFailed {
		t.Fatal("selecting a HubCenter preset should clear stale activation failure")
	}
	if got := m.hubCenterInput.Value(); got != "https://backup.example" {
		t.Fatalf("HubCenter input = %q, want backup option", got)
	}
}

func TestOnboardingViewFitsNarrowTerminal(t *testing.T) {
	m := NewOnboardingModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})
	m.emailInput.SetValue("user@example.com")
	m.hubCenterInput.SetValue("https://very-long-private-center.example.com")
	m, _ = m.Update(OnboardingRemoteResultMsg{Success: true, HubURL: "https://hub.example/some/long/path", MachineID: "machine-1", MachineReady: true})

	view := stripANSIForTest(m.View())
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > 44 {
			t.Fatalf("line width = %d, want <= 44: %q\nview:\n%s", got, line, view)
		}
	}
}

func TestOnboardingCompactViewKeepsFocusedActionVisible(t *testing.T) {
	m := NewOnboardingModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 12})
	m.remoteDone = true
	m.cursor = onboardingRowFinish
	m.focusCursor()

	view := stripANSIForTest(m.View())
	if strings.Contains(view, "local LLM settings") {
		t.Fatalf("compact onboarding should drop the subtitle to keep actions visible:\n%s", view)
	}
	if !strings.Contains(view, "Done") || !strings.Contains(view, "continue to Service Redeem") {
		t.Fatalf("compact onboarding should keep the focused finish action visible:\n%s", view)
	}
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) > 8 {
		t.Fatalf("compact onboarding lines = %d, want <= root content height 8:\n%s", len(lines), view)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > 44 {
			t.Fatalf("line width = %d, want <= 44: %q\nview:\n%s", got, line, view)
		}
	}
}

func TestOnboardingRejectsInvalidEmail(t *testing.T) {
	m := NewOnboardingModel("en")
	m.emailInput.SetValue("not-an-email")
	m.cursor = onboardingRowEmail
	m.focusCursor()

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("invalid email should not start activation")
	}
	if m.cursor != onboardingRowEmail {
		t.Fatalf("cursor = %d, want email row", m.cursor)
	}
	if got := m.remoteStatus; got != onboardingText("en", "emailInvalid") {
		t.Fatalf("remote status = %q", got)
	}
}

func TestOnboardingBlankHubCenterUsesDefault(t *testing.T) {
	m := NewOnboardingModel("en")
	m.emailInput.SetValue("user@example.com")
	m.hubCenterInput.SetValue("  ")
	m.cursor = onboardingRowActivate
	m.focusCursor()

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected resolve command")
	}
	msg := cmd().(OnboardingResolveIdentityMsg)
	if msg.HubCenterURL != remote.DefaultRemoteHubCenterURL {
		t.Fatalf("HubCenterURL = %q, want default", msg.HubCenterURL)
	}
}

func TestOnboardingInvalidHubCenterStillSendsResolve(t *testing.T) {
	// With the identity-first flow, HubCenter is hidden from the user.
	// Even an invalid value proceeds — the backend resolve will handle errors.
	m := NewOnboardingModel("en")
	m.emailInput.SetValue("user@example.com")
	m.hubCenterInput.SetValue("center.example")
	m.cursor = onboardingRowActivate
	m.focusCursor()

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected resolve command even with unusual HubCenter value")
	}
	if !m.remoteBusy {
		t.Fatal("expected remoteBusy")
	}
}

func TestOnboardingSetLangRelocalizesStatuses(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.SetLang("en")
	if got := m.remoteStatus; got != onboardingText("en", "notActivated") {
		t.Fatalf("remote status = %q", got)
	}
	if got := m.weixinStatus; got != onboardingText("en", "notBound") {
		t.Fatalf("weixin status = %q", got)
	}
}

func TestOnboardingSetLangPreservesActivatedMachineID(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token",
		RemoteViewerToken:  "viewer-token",
	})
	m.SetLang("en")
	want := onboardingText("en", "activated") + ": machine-1"
	if got := m.remoteStatus; got != want {
		t.Fatalf("remote status = %q, want %q", got, want)
	}
}

func TestOnboardingLoadTreatsHubViewerAsServiceReady(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.cursor = onboardingRowActivate
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteEmail:       "user@example.com",
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	})
	if !m.remoteDone {
		t.Fatal("Hub URL + viewer token should be enough to continue to service redeem")
	}
	if m.remoteStatus != onboardingText("zh", "serviceReady") {
		t.Fatalf("remote status = %q, want service ready", m.remoteStatus)
	}
	if m.cursor != onboardingRowFinish {
		t.Fatalf("cursor = %d, want finish row", m.cursor)
	}
	if got := m.nextStepText(); got != onboardingText("zh", "nextRedeemOptionalWeixin") {
		t.Fatalf("next step = %q", got)
	}
	m.SetLang("en")
	if got := m.remoteStatus; got != onboardingText("en", "serviceReady") {
		t.Fatalf("English service-ready status = %q", got)
	}
}

func TestOnboardingQRUsesCompactTerminalRendering(t *testing.T) {
	rendered := renderOnboardingQR("https://example.com/weixin-login?token=test", 120)
	lines := strings.Split(strings.TrimSpace(stripANSIForTest(rendered)), "\n")
	if len(lines) > 20 {
		t.Fatalf("compact QR rendered too many lines: %d", len(lines))
	}
}

func TestOnboardingQRViewHidesSetupRowsToFitTerminal(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.weixinQR = "https://example.com/weixin-login?token=test"
	view := stripANSIForTest(m.View())
	if strings.Contains(view, "HubCenter") || strings.Contains(view, "Hub 激活") {
		t.Fatalf("QR view should prioritize the scan panel instead of full setup rows:\n%s", view)
	}
	if strings.Contains(view, "payload:") || strings.Contains(view, "token=test") {
		t.Fatalf("QR view should not expose payload when the QR fits:\n%s", view)
	}
}

func TestOnboardingQRViewHidesPayloadWhenQRFits(t *testing.T) {
	m := NewOnboardingModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})
	m.weixinQR = "https://example.com/weixin-login?token=test"

	view := stripANSIForTest(m.View())
	if strings.Contains(view, "payload:") || strings.Contains(view, "token=test") {
		t.Fatalf("QR payload should stay hidden when rendered QR fits:\n%s", view)
	}
	if !strings.Contains(view, "Payload appears only when QR cannot fit") {
		t.Fatalf("scan footer should explain payload fallback:\n%s", view)
	}
}

func TestOnboardingQRPayloadFitsNarrowTerminal(t *testing.T) {
	m := NewOnboardingModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 24, Height: 8})
	m.weixinQR = "https://example.com/weixin-login?token=test"

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "payload:") {
		t.Fatalf("narrow QR view should expose payload fallback:\n%s", view)
	}
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > 24 {
			t.Fatalf("line width = %d, want <= 24: %q\nview:\n%s", got, line, view)
		}
	}
}

func TestOnboardingQRPayloadPrefixFollowsLanguage(t *testing.T) {
	m := NewOnboardingModel("zh")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 24, Height: 8})
	m.weixinQR = "https://example.com/weixin-login?token=test"

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "载荷:") {
		t.Fatalf("Chinese QR fallback should localize payload prefix:\n%s", view)
	}
	if strings.Contains(view, "payload:") {
		t.Fatalf("Chinese QR fallback should not keep the English payload prefix:\n%s", view)
	}
}

func TestFitOnboardingRespectsTinyWidths(t *testing.T) {
	for _, width := range []int{0, 1, 2, 3} {
		got := fitOnboarding("abcdef", width)
		if lipgloss.Width(got) > max(0, width) {
			t.Fatalf("fitOnboarding width %d produced %q with display width %d", width, got, lipgloss.Width(got))
		}
	}
}

func TestOnboardingQRViewEscReturnsToSetupRows(t *testing.T) {
	m := NewOnboardingModel("en")
	m.weixinQR = "https://example.com/weixin-login?token=test"
	m.weixinToken = "token-test"

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	view := stripANSIForTest(m.View())
	if m.weixinQR != "" || m.weixinToken != "" {
		t.Fatalf("QR state should be cleared after Esc, qr=%q token=%q", m.weixinQR, m.weixinToken)
	}
	if m.weixinStatus != onboardingText("en", "notBound") {
		t.Fatalf("status after Esc = %q, want not bound", m.weixinStatus)
	}
	if !strings.Contains(view, "First-run Setup") || !strings.Contains(view, "HubCenter") {
		t.Fatalf("Esc should return to setup rows:\n%s", view)
	}
}

func TestOnboardingQRPollAfterEscIsIgnored(t *testing.T) {
	m := NewOnboardingModel("en")
	m.weixinQR = "https://example.com/weixin-login?token=test"
	m.weixinToken = "token-test"

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m, _ = m.Update(OnboardingWeixinPollResultMsg{Token: "token-test", Status: "confirmed", Message: "bound", Success: true, Completed: true})
	if m.weixinDone || m.weixinStatus != onboardingText("en", "notBound") {
		t.Fatalf("late poll after Esc should be ignored, done=%v status=%q", m.weixinDone, m.weixinStatus)
	}
}

func TestOnboardingQRStalePollResultIsIgnored(t *testing.T) {
	m := NewOnboardingModel("en")
	m.weixinQR = "https://example.com/weixin-login?token=new"
	m.weixinToken = "new-token"
	m.weixinStatus = "waiting"

	m, _ = m.Update(OnboardingWeixinPollResultMsg{Token: "old-token", Status: "expired", Message: "expired", Completed: true})
	if m.weixinQR == "" || m.weixinToken != "new-token" || m.weixinStatus != "waiting" {
		t.Fatalf("stale poll should not mutate current QR state: qr=%q token=%q status=%q", m.weixinQR, m.weixinToken, m.weixinStatus)
	}
}

func TestOnboardingQREmptyTokenPollDuringActiveQRIsIgnored(t *testing.T) {
	m := NewOnboardingModel("en")
	m.weixinQR = "https://example.com/weixin-login?token=new"
	m.weixinToken = "new-token"
	m.weixinStatus = "waiting"

	m, _ = m.Update(OnboardingWeixinPollResultMsg{Status: "error", Message: "empty token", Completed: true})
	if m.weixinQR == "" || m.weixinToken != "new-token" || m.weixinStatus != "waiting" {
		t.Fatalf("empty-token poll should not mutate active QR state: qr=%q token=%q status=%q", m.weixinQR, m.weixinToken, m.weixinStatus)
	}
}

func TestOnboardingQRMessageTrimsTokenForPolling(t *testing.T) {
	m := NewOnboardingModel("en")
	m.weixinBusy = true

	m, cmd := m.Update(OnboardingWeixinQRMsg{Success: true, QR: " https://example.com/qr ", Token: " token-test "})
	if m.weixinQR != "https://example.com/qr" || m.weixinToken != "token-test" {
		t.Fatalf("QR state not trimmed, qr=%q token=%q", m.weixinQR, m.weixinToken)
	}
	msg, ok := onboardingPollMsgFromCmdForTest(cmd)
	if !ok || msg.Token != "token-test" {
		t.Fatalf("poll cmd = %#v, want trimmed token", msg)
	}
}

func TestOnboardingQRTickShowsProgress(t *testing.T) {
	m := NewOnboardingModel("en")
	m.weixinQR = "https://example.com/qr"
	m.weixinToken = "token-test"
	m.weixinStatus = onboardingText("en", "waitingScan")

	m, cmd := m.Update(OnboardingWeixinTickMsg{Token: "token-test"})
	if m.weixinElapsed != 1 || cmd == nil {
		t.Fatalf("tick should advance and keep ticking, elapsed=%d cmdNil=%v", m.weixinElapsed, cmd == nil)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "(1s)") {
		t.Fatalf("view should show elapsed polling progress:\n%s", view)
	}
}

func TestOnboardingQRExpiredAutoRefreshes(t *testing.T) {
	m := NewOnboardingModel("en")
	m.weixinQR = "https://example.com/weixin-login?token=test"
	m.weixinToken = "token-test"

	m, cmd := m.Update(OnboardingWeixinPollResultMsg{Token: "token-test", Status: "expired", Message: "expired", Completed: true})
	if cmd == nil {
		t.Fatal("expired QR should request a fresh QR automatically")
	}
	if m.weixinRefreshes != 1 || !m.weixinBusy || m.weixinQR != "" || m.weixinToken != "" {
		t.Fatalf("refresh state = refreshes=%d busy=%v qr=%q token=%q", m.weixinRefreshes, m.weixinBusy, m.weixinQR, m.weixinToken)
	}
	if !onboardingStartMsgFromCmdForTest(cmd) {
		t.Fatalf("command = %#v, want OnboardingStartWeixinMsg", cmd())
	}
}

func TestOnboardingQRBusyTickShowsProgress(t *testing.T) {
	m := NewOnboardingModel("en")
	m.weixinBusy = true
	m.weixinStatus = onboardingText("en", "requestingQR")

	m, cmd := m.Update(OnboardingWeixinTickMsg{})
	if m.weixinElapsed != 1 || cmd == nil {
		t.Fatalf("busy tick should advance and keep ticking, elapsed=%d cmdNil=%v", m.weixinElapsed, cmd == nil)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "(1s)") {
		t.Fatalf("busy view should show elapsed progress:\n%s", view)
	}
}

func TestOnboardingQRAutoRefreshCanBeCancelled(t *testing.T) {
	m := NewOnboardingModel("en")
	m.weixinBusy = true
	m.weixinStatus = onboardingText("en", "refreshingQR")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if cmd != nil {
		t.Fatalf("cancel command = %#v, want nil", cmd)
	}
	if m.weixinBusy || m.weixinQR != "" || m.weixinToken != "" || m.weixinStatus != onboardingText("en", "notBound") {
		t.Fatalf("cancel state busy=%v qr=%q token=%q status=%q", m.weixinBusy, m.weixinQR, m.weixinToken, m.weixinStatus)
	}

	m, _ = m.Update(OnboardingWeixinQRMsg{Success: true, QR: "https://example.com/stale", Token: "stale-token"})
	if m.weixinQR != "" || m.weixinToken != "" {
		t.Fatalf("stale QR response should be ignored after cancel, qr=%q token=%q", m.weixinQR, m.weixinToken)
	}
}

func TestTigerClawOnboardingShowsSSOFlowOnly(t *testing.T) {
	m := NewOnboardingModel("en")
	m.tigerClaw = true
	m.cursor = onboardingRowSSO
	m.focusCursor()

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Enterprise SSO") || strings.Contains(view, "Hub activation") || strings.Contains(view, "Email") {
		t.Fatalf("TigerClaw onboarding should show SSO without Hub/email rows:\n%s", view)
	}
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil || !m.ssoBusy || m.ssoFlowID == "" {
		t.Fatalf("enter on SSO should start async flow, busy=%v flow=%q cmdNil=%v", m.ssoBusy, m.ssoFlowID, cmd == nil)
	}
	if !m.ssoTicking {
		t.Fatal("enter on SSO should start one progress ticker")
	}
	if !onboardingStartSSOMsgFromCmdForTest(cmd) {
		t.Fatalf("command = %#v, want OnboardingStartSSOMsg", cmd())
	}
}

func TestOnboardingSSOQRMessageStartsPollingAndTicks(t *testing.T) {
	m := NewOnboardingModel("en")
	m.tigerClaw = true
	m.ssoBusy = true
	m.ssoFlowID = "flow-1"

	m, cmd := m.Update(OnboardingSSOQRMsg{FlowID: "flow-1", Success: true, QR: " https://example.com/sso ", LoginURL: " https://login.example ", PollClient: &http.Client{}})
	if m.ssoQR != "https://example.com/sso" || m.ssoLoginURL != "https://login.example" {
		t.Fatalf("SSO QR state not trimmed, qr=%q login=%q", m.ssoQR, m.ssoLoginURL)
	}
	msg, ok := onboardingPollSSOMsgFromCmdForTest(cmd)
	if !ok || msg.FlowID != "flow-1" {
		t.Fatalf("poll cmd = %#v, want SSO poll", msg)
	}
	if !m.ssoTicking {
		t.Fatal("SSO QR should keep a single progress ticker active")
	}

	m, cmd = m.Update(OnboardingSSOTickMsg{FlowID: "flow-1"})
	if m.ssoElapsed != 1 || cmd == nil {
		t.Fatalf("SSO tick should advance, elapsed=%d cmdNil=%v", m.ssoElapsed, cmd == nil)
	}
	if !m.ssoTicking {
		t.Fatal("SSO tick should schedule the next ticker")
	}
	if !strings.Contains(stripANSIForTest(m.View()), "(1s)") {
		t.Fatalf("SSO QR view should show elapsed progress:\n%s", stripANSIForTest(m.View()))
	}
}

func TestOnboardingSSOManualFallbackDoesNotStartPolling(t *testing.T) {
	m := NewOnboardingModel("en")
	m.tigerClaw = true
	m.ssoBusy = true
	m.ssoFlowID = "flow-1"

	m, cmd := m.Update(OnboardingSSOQRMsg{FlowID: "flow-1", Success: true, QR: "https://codegen.example/login", LoginURL: "https://codegen.example/login", Message: "open manually"})
	if cmd != nil {
		t.Fatalf("manual fallback should not start polling without session client: %#v", cmd())
	}
	if m.ssoBusy || m.ssoQR == "" || !m.ssoInput.Focused() || m.ssoStatus != "open manually" {
		t.Fatalf("manual fallback state busy=%v qr=%q focused=%v status=%q", m.ssoBusy, m.ssoQR, m.ssoInput.Focused(), m.ssoStatus)
	}
}

func TestOnboardingSSOTerminalFailureClearsFlow(t *testing.T) {
	m := NewOnboardingModel("en")
	m.tigerClaw = true
	m.ssoQR = "https://example.com/sso"
	m.ssoFlowID = "flow-1"
	m.ssoInput.Focus()
	m.ssoInput.SetValue("stale")

	m, _ = m.Update(OnboardingSSOResultMsg{FlowID: "flow-1", Success: false, Message: "fatal"})
	if m.ssoQR != "" || m.ssoFlowID != "" || m.ssoInput.Focused() || m.ssoInput.Value() != "" {
		t.Fatalf("terminal failure should clear SSO flow, qr=%q flow=%q focused=%v input=%q", m.ssoQR, m.ssoFlowID, m.ssoInput.Focused(), m.ssoInput.Value())
	}

	m, _ = m.Update(OnboardingSSOResultMsg{FlowID: "flow-1", Success: true, Email: "stale@example.com"})
	if m.ssoDone {
		t.Fatal("late success after terminal failure should be ignored")
	}
}

func TestOnboardingSSOCancelIgnoresStaleResults(t *testing.T) {
	m := NewOnboardingModel("en")
	m.tigerClaw = true
	m.ssoBusy = true
	m.ssoFlowID = "flow-1"
	m.ssoStatus = onboardingText("en", "ssoRequestingQR")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if msg, ok := onboardingCancelSSOMsgFromCmdForTest(cmd); !ok || msg.FlowID != "flow-1" {
		t.Fatalf("cancel command = %#v, want cancel flow-1", msg)
	}
	if m.ssoBusy || m.ssoQR != "" || m.ssoFlowID != "" || m.ssoStatus != onboardingText("en", "ssoNotSignedIn") {
		t.Fatalf("cancel state busy=%v qr=%q flow=%q status=%q", m.ssoBusy, m.ssoQR, m.ssoFlowID, m.ssoStatus)
	}
	m, _ = m.Update(OnboardingSSOQRMsg{FlowID: "flow-1", Success: true, QR: "https://example.com/stale"})
	m, _ = m.Update(OnboardingSSOResultMsg{FlowID: "flow-1", Success: true, Email: "user@example.com"})
	if m.ssoDone || m.ssoQR != "" {
		t.Fatalf("stale SSO result should be ignored, done=%v qr=%q", m.ssoDone, m.ssoQR)
	}
}

func TestOnboardingSSOQRAcceptsPastedReturnURL(t *testing.T) {
	m := NewOnboardingModel("en")
	m.tigerClaw = true
	m.ssoQR = "https://example.com/sso"
	m.ssoFlowID = "flow-1"
	m.ssoInput.Focus()

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("https://callback.example/?token=abc")})
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected pasted SSO input submit command")
	}
	msg, ok := onboardingSubmitSSOMsgFromCmdForTest(cmd)
	if !ok || msg.FlowID != "flow-1" || !strings.Contains(msg.Input, "token=abc") {
		t.Fatalf("submit cmd = %#v", msg)
	}
	if !m.ssoBusy || m.ssoStatus != onboardingText("en", "ssoValidating") {
		t.Fatalf("manual validation state busy=%v status=%q", m.ssoBusy, m.ssoStatus)
	}

	_, duplicateCmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if duplicateCmd != nil {
		t.Fatalf("enter during manual validation should not submit again: %#v", duplicateCmd())
	}
}

func TestOnboardingSSOQRAllowsManualSubmitWhilePolling(t *testing.T) {
	m := NewOnboardingModel("en")
	m.tigerClaw = true
	m.ssoBusy = true
	m.ssoPolling = true
	m.ssoQR = "https://example.com/sso"
	m.ssoFlowID = "flow-1"
	m.ssoInput.Focus()
	m.ssoInput.SetValue("https://callback.example/?token=manual")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	msg, ok := onboardingSubmitSSOMsgFromCmdForTest(cmd)
	if !ok || msg.FlowID != "flow-1" || !strings.Contains(msg.Input, "token=manual") {
		t.Fatalf("submit cmd = %#v", msg)
	}
	if !m.ssoBusy || !m.ssoPolling || !m.ssoSubmitting || m.ssoStatus != onboardingText("en", "ssoValidating") {
		t.Fatalf("manual submit during poll state busy=%v polling=%v submitting=%v status=%q", m.ssoBusy, m.ssoPolling, m.ssoSubmitting, m.ssoStatus)
	}
}

func TestOnboardingSSOPollFailureDoesNotCancelManualSubmit(t *testing.T) {
	m := NewOnboardingModel("en")
	m.tigerClaw = true
	m.ssoBusy = true
	m.ssoPolling = true
	m.ssoSubmitting = true
	m.ssoQR = "https://example.com/sso"
	m.ssoFlowID = "flow-1"
	m.ssoStatus = onboardingText("en", "ssoValidating")

	m, _ = m.Update(OnboardingSSOResultMsg{FlowID: "flow-1", Success: false, Message: "poll timeout", KeepOpen: true})
	if !m.ssoBusy || m.ssoPolling || !m.ssoSubmitting || m.ssoStatus != onboardingText("en", "ssoValidating") {
		t.Fatalf("poll failure should leave manual validation active, busy=%v polling=%v submitting=%v status=%q", m.ssoBusy, m.ssoPolling, m.ssoSubmitting, m.ssoStatus)
	}

	m, _ = m.Update(OnboardingSSOResultMsg{FlowID: "flow-1", Success: false, Message: "bad token", KeepOpen: true, FromManual: true})
	if m.ssoBusy || m.ssoSubmitting || m.ssoStatus != "bad token" || m.ssoQR == "" {
		t.Fatalf("manual failure should stop validation and keep QR open, busy=%v submitting=%v status=%q qr=%q", m.ssoBusy, m.ssoSubmitting, m.ssoStatus, m.ssoQR)
	}
}

func TestOnboardingSSOPollFailureKeepsManualInputOpen(t *testing.T) {
	m := NewOnboardingModel("en")
	m.tigerClaw = true
	m.ssoQR = "https://example.com/sso"
	m.ssoFlowID = "flow-1"
	m.ssoInput.Focus()
	m.ssoInput.SetValue("https://callback.example/?token=abc")

	m, _ = m.Update(OnboardingSSOResultMsg{FlowID: "flow-1", Success: false, Message: "network timeout", KeepOpen: true})
	if m.ssoQR == "" || !m.ssoInput.Focused() || m.ssoInput.Value() == "" {
		t.Fatalf("poll failure should keep manual input open, qr=%q focused=%v input=%q", m.ssoQR, m.ssoInput.Focused(), m.ssoInput.Value())
	}
	m, cmd := m.Update(OnboardingSSOTickMsg{FlowID: "flow-1"})
	if m.ssoElapsed != 0 || cmd != nil {
		t.Fatalf("tick after poll failure should stop, elapsed=%d cmd=%v", m.ssoElapsed, cmd)
	}
}

func TestOnboardingQRViewEnterRefreshesPollWhileBusy(t *testing.T) {
	m := NewOnboardingModel("en")
	m.weixinQR = "https://example.com/qr"
	m.weixinToken = "token-test"
	m.weixinBusy = true

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("Enter during active QR should trigger a status refresh")
	}
	msg, ok := cmd().(OnboardingPollWeixinMsg)
	if !ok || msg.Token != "token-test" {
		t.Fatalf("command = %#v, want poll for token-test", msg)
	}
}

func TestOnboardingQRCompletedFailureReturnsToSetupRows(t *testing.T) {
	m := NewOnboardingModel("en")
	m.weixinQR = "https://example.com/weixin-login?token=test"
	m.weixinToken = "token-test"
	m.weixinRefreshes = maxOnboardingWeixinRefreshes

	m, _ = m.Update(OnboardingWeixinPollResultMsg{Token: "token-test", Status: "expired", Message: "expired", Completed: true})
	if m.weixinQR != "" || m.weixinToken != "" {
		t.Fatalf("QR state should clear after terminal poll failure, qr=%q token=%q", m.weixinQR, m.weixinToken)
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "First-run Setup") || !strings.Contains(view, "expired") {
		t.Fatalf("completed failure should return to setup rows with status:\n%s", view)
	}
}

func TestOnboardingQRViewUsesPayloadOnShortTerminal(t *testing.T) {
	m := NewOnboardingModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 10})
	m.weixinQR = "https://example.com/weixin-login?token=test"

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "Terminal is too small for QR") {
		t.Fatalf("short terminal should show QR height fallback:\n%s", view)
	}
	if !strings.Contains(view, "payload:") {
		t.Fatalf("short terminal should keep payload fallback visible:\n%s", view)
	}
}

func onboardingPollMsgFromCmdForTest(cmd tea.Cmd) (OnboardingPollWeixinMsg, bool) {
	if cmd == nil {
		return OnboardingPollWeixinMsg{}, false
	}
	msg := cmd()
	if poll, ok := msg.(OnboardingPollWeixinMsg); ok {
		return poll, true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return OnboardingPollWeixinMsg{}, false
	}
	for _, item := range batch {
		if item == nil {
			continue
		}
		if poll, ok := item().(OnboardingPollWeixinMsg); ok {
			return poll, true
		}
	}
	return OnboardingPollWeixinMsg{}, false
}

func onboardingStartMsgFromCmdForTest(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if _, ok := msg.(OnboardingStartWeixinMsg); ok {
		return true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return false
	}
	for _, item := range batch {
		if item == nil {
			continue
		}
		if _, ok := item().(OnboardingStartWeixinMsg); ok {
			return true
		}
	}
	return false
}

func onboardingPollSSOMsgFromCmdForTest(cmd tea.Cmd) (OnboardingPollSSOMsg, bool) {
	if cmd == nil {
		return OnboardingPollSSOMsg{}, false
	}
	msg := cmd()
	if poll, ok := msg.(OnboardingPollSSOMsg); ok {
		return poll, true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return OnboardingPollSSOMsg{}, false
	}
	for _, item := range batch {
		if item == nil {
			continue
		}
		if poll, ok := item().(OnboardingPollSSOMsg); ok {
			return poll, true
		}
	}
	return OnboardingPollSSOMsg{}, false
}

func onboardingStartSSOMsgFromCmdForTest(cmd tea.Cmd) bool {
	if cmd == nil {
		return false
	}
	msg := cmd()
	if _, ok := msg.(OnboardingStartSSOMsg); ok {
		return true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return false
	}
	for _, item := range batch {
		if item == nil {
			continue
		}
		if _, ok := item().(OnboardingStartSSOMsg); ok {
			return true
		}
	}
	return false
}

func onboardingSubmitSSOMsgFromCmdForTest(cmd tea.Cmd) (OnboardingSubmitSSOInputMsg, bool) {
	if cmd == nil {
		return OnboardingSubmitSSOInputMsg{}, false
	}
	msg := cmd()
	if submit, ok := msg.(OnboardingSubmitSSOInputMsg); ok {
		return submit, true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return OnboardingSubmitSSOInputMsg{}, false
	}
	for _, item := range batch {
		if item == nil {
			continue
		}
		if submit, ok := item().(OnboardingSubmitSSOInputMsg); ok {
			return submit, true
		}
	}
	return OnboardingSubmitSSOInputMsg{}, false
}

func onboardingCancelSSOMsgFromCmdForTest(cmd tea.Cmd) (OnboardingCancelSSOMsg, bool) {
	if cmd == nil {
		return OnboardingCancelSSOMsg{}, false
	}
	msg := cmd()
	if cancel, ok := msg.(OnboardingCancelSSOMsg); ok {
		return cancel, true
	}
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		return OnboardingCancelSSOMsg{}, false
	}
	for _, item := range batch {
		if item == nil {
			continue
		}
		if cancel, ok := item().(OnboardingCancelSSOMsg); ok {
			return cancel, true
		}
	}
	return OnboardingCancelSSOMsg{}, false
}

func stripANSIForTest(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		if r == 0x1b {
			inEsc = true
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func TestOnboardingNextStepGuidance(t *testing.T) {
	m := NewOnboardingModel("zh")
	if got := m.nextStepText(); got != onboardingText("zh", "nextEmail") {
		t.Fatalf("empty email next step = %q", got)
	}

	en := NewOnboardingModel("en")
	if got := en.nextStepText(); !strings.Contains(got, "Done") || !strings.Contains(got, "local LLM") {
		t.Fatalf("empty email guidance should offer a local LLM path, got %q", got)
	}

	m.emailInput.SetValue("user@example.com")
	if got := m.nextStepText(); got != onboardingText("zh", "nextActivate") {
		t.Fatalf("email next step = %q", got)
	}

	m.remoteDone = true
	if got := m.nextStepText(); got != onboardingText("zh", "nextRedeemOptionalWeixin") {
		t.Fatalf("remote done next step = %q", got)
	}

	m.weixinDone = true
	if got := m.nextStepText(); got != onboardingText("zh", "nextRedeem") {
		t.Fatalf("all done next step = %q", got)
	}
}

func TestOnboardingLabelsWeixinOptional(t *testing.T) {
	view := stripANSIForTest(NewOnboardingModel("zh").View())
	if !strings.Contains(view, "微信绑定（可选）") {
		t.Fatalf("onboarding should label WeChat as optional: %s", view)
	}
}

func TestOnboardingActivationSuccessFocusesFinish(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.cursor = onboardingRowActivate
	m.focusCursor()

	m, _ = m.Update(OnboardingRemoteResultMsg{Success: true, MachineID: "machine-1", MachineReady: true})
	if m.cursor != onboardingRowFinish {
		t.Fatalf("cursor = %d, want finish row", m.cursor)
	}
	if !m.remoteDone {
		t.Fatal("remote should be marked done after success")
	}
	if got := m.finishHint(); got != onboardingText("zh", "finishHintRedeem") {
		t.Fatalf("finish hint = %q", got)
	}
}

func TestOnboardingActivationIncompleteKeepsActivateFocused(t *testing.T) {
	m := NewOnboardingModel("en")
	m.cursor = onboardingRowActivate
	m.focusCursor()

	m, _ = m.Update(OnboardingRemoteResultMsg{Success: true, HubURL: "https://hub.example", MachineID: "machine-1"})
	if m.remoteDone {
		t.Fatal("remote should not be marked done without service or machine credentials")
	}
	if m.cursor != onboardingRowActivate {
		t.Fatalf("cursor = %d, want activate row", m.cursor)
	}
	if got := m.remoteStatus; got != onboardingText("en", "serviceIncomplete") {
		t.Fatalf("remote status = %q, want incomplete guidance", got)
	}
}

func TestOnboardingLoadRemoteDoneFocusesFinishBeforeDone(t *testing.T) {
	m := NewOnboardingModel("zh")
	m.cursor = onboardingRowActivate
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token",
		RemoteViewerToken:  "viewer-token",
	})
	if m.cursor != onboardingRowFinish {
		t.Fatalf("cursor = %d, want finish row", m.cursor)
	}
}

func TestOnboardingMissingViewerTokenRequiresReactivation(t *testing.T) {
	m := NewOnboardingModel("en")
	m.cursor = onboardingRowLanguage
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteEmail:        "user@example.com",
		RemoteMachineID:    "machine-1",
		RemoteMachineToken: "token",
	})
	if m.remoteDone {
		t.Fatal("remote setup should not be complete without viewer token")
	}
	if m.cursor != onboardingRowActivate {
		t.Fatalf("cursor = %d, want activate row", m.cursor)
	}
	if got := m.remoteStatus; got != onboardingText("en", "emailSaved") {
		t.Fatalf("remote status = %q, want email saved guidance", got)
	}
}

// --- Identity-first registration flow tests ---

func TestOnboardingResolveIdentityPhoneShowsCodeInput(t *testing.T) {
	m := NewOnboardingModel("en")
	m.emailInput.SetValue("13800138000")
	m.cursor = onboardingRowEmail
	m.focusCursor()

	// Simulate resolve result: phone method, code sent
	m, cmd := m.Update(OnboardingResolveIdentityResultMsg{
		Success:  true,
		Identity: "13800138000",
		Method:   "phone",
		HubURL:   "https://hub.example",
		TenantID: "tenant-1",
		CodeLength: 6,
	})
	if cmd == nil {
		t.Fatal("expected SMS tick command")
	}
	if !m.codeSent {
		t.Fatal("codeSent should be true after phone resolve")
	}
	if m.codeLength != 6 {
		t.Fatalf("codeLength = %d, want 6", m.codeLength)
	}
	if m.cursor != onboardingRowSMSCode {
		t.Fatalf("cursor = %d, want SMS code row", m.cursor)
	}
	if m.resolvedMethod != "phone" {
		t.Fatalf("resolvedMethod = %q", m.resolvedMethod)
	}
	// SMS code row should be in active rows
	rows := m.activeRows()
	found := false
	for _, r := range rows {
		if r == onboardingRowSMSCode {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("SMS code row not in activeRows: %v", rows)
	}
}

func TestOnboardingResolveIdentityEmailAutoDispatches(t *testing.T) {
	m := NewOnboardingModel("en")
	m.emailInput.SetValue("user@example.com")

	// Simulate resolve result: email method
	m, cmd := m.Update(OnboardingResolveIdentityResultMsg{
		Success:  true,
		Identity: "user@example.com",
		Method:   "email",
		HubURL:   "https://hub.example",
		TenantID: "tenant-1",
	})
	if cmd == nil {
		t.Fatal("expected auto-dispatch verify command for email method")
	}
	if !m.remoteBusy {
		t.Fatal("remoteBusy should stay true during email auto-enroll")
	}
	if m.codeSent {
		t.Fatal("codeSent should be false for email method")
	}
	// The cmd should produce an OnboardingVerifyCodeMsg
	msg := cmd()
	verifyMsg, ok := msg.(OnboardingVerifyCodeMsg)
	if !ok {
		t.Fatalf("auto-dispatch cmd returned %T, want OnboardingVerifyCodeMsg", msg)
	}
	if verifyMsg.Method != "email" {
		t.Fatalf("verify method = %q, want email", verifyMsg.Method)
	}
	if verifyMsg.HubURL != "https://hub.example" {
		t.Fatalf("verify hubURL = %q", verifyMsg.HubURL)
	}
}

func TestOnboardingVerifyCodeSuccess(t *testing.T) {
	m := NewOnboardingModel("en")
	m.codeSent = true
	m.resolvedMethod = "phone"
	m.remoteBusy = true

	m, _ = m.Update(OnboardingVerifyCodeResultMsg{
		Success:     true,
		Message:     "activated",
		HubURL:      "https://hub.example",
		MachineID:   "machine-123",
		MachineReady: true,
	})
	if !m.remoteDone {
		t.Fatal("remoteDone should be true after success")
	}
	if m.remoteFailed {
		t.Fatal("remoteFailed should be false after success")
	}
	if m.cursor != onboardingRowFinish {
		t.Fatalf("cursor = %d, want finish row", m.cursor)
	}
}

func TestOnboardingVerifyCodeFailurePhoneFocusesSMSCode(t *testing.T) {
	m := NewOnboardingModel("en")
	m.codeSent = true
	m.resolvedMethod = "phone"
	m.remoteBusy = true

	m, _ = m.Update(OnboardingVerifyCodeResultMsg{
		Success: false,
		Message: "invalid code",
	})
	if m.remoteDone {
		t.Fatal("remoteDone should be false after failure")
	}
	if !m.remoteFailed {
		t.Fatal("remoteFailed should be true")
	}
	if m.cursor != onboardingRowSMSCode {
		t.Fatalf("cursor = %d, want SMS code row for phone failure", m.cursor)
	}
}

func TestOnboardingVerifyCodeFailureEmailFocusesEmail(t *testing.T) {
	m := NewOnboardingModel("en")
	m.codeSent = false
	m.resolvedMethod = "email"
	m.remoteBusy = true

	m, _ = m.Update(OnboardingVerifyCodeResultMsg{
		Success: false,
		Message: "enrollment failed",
	})
	if m.cursor != onboardingRowEmail {
		t.Fatalf("cursor = %d, want email row for email failure", m.cursor)
	}
}

func TestOnboardingIdentityChangeResetsSMSState(t *testing.T) {
	m := NewOnboardingModel("en")
	m.emailInput.SetValue("13800138000")
	m.codeSent = true
	m.resolvedMethod = "phone"
	m.resolvedHubURL = "https://hub.example"
	m.smsCodeInput.SetValue("123456")
	m.cursor = onboardingRowEmail
	m.focusCursor()

	// Type a character — identity changes
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	if m.codeSent {
		t.Fatal("codeSent should reset when identity changes")
	}
	if m.resolvedMethod != "" {
		t.Fatalf("resolvedMethod = %q, should be cleared", m.resolvedMethod)
	}
	if m.smsCodeInput.Value() != "" {
		t.Fatalf("smsCodeInput = %q, should be cleared", m.smsCodeInput.Value())
	}
}

func TestOnboardingSMSCountdownExpiredAllowsResend(t *testing.T) {
	m := NewOnboardingModel("en")
	m.emailInput.SetValue("13800138000")
	m.codeSent = true
	m.codeCountdown = 0
	m.resolvedMethod = "phone"
	m.resolvedHubURL = "https://hub.example"
	m.cursor = onboardingRowEmail
	m.focusCursor()

	// Enter on email row when countdown=0 should re-resolve (resend)
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected re-resolve command after countdown expired")
	}
	if m.codeSent {
		t.Fatal("codeSent should be reset for re-resolve")
	}
	if !m.remoteBusy {
		t.Fatal("expected remoteBusy during re-resolve")
	}
	msg := cmd()
	if _, ok := msg.(OnboardingResolveIdentityMsg); !ok {
		t.Fatalf("command returned %T, want OnboardingResolveIdentityMsg", msg)
	}
}

func TestOnboardingSMSCountdownActiveVerifiesCode(t *testing.T) {
	m := NewOnboardingModel("en")
	m.emailInput.SetValue("13800138000")
	m.codeSent = true
	m.codeCountdown = 30
	m.resolvedMethod = "phone"
	m.resolvedHubURL = "https://hub.example"
	m.resolvedTenantID = "tenant-1"
	m.smsCodeInput.SetValue("123456")
	m.cursor = onboardingRowEmail
	m.focusCursor()

	// Enter on email row when countdown>0 and code sent should verify
	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected verify command")
	}
	msg := cmd()
	verifyMsg, ok := msg.(OnboardingVerifyCodeMsg)
	if !ok {
		t.Fatalf("command returned %T, want OnboardingVerifyCodeMsg", msg)
	}
	if verifyMsg.VerifyCode != "123456" {
		t.Fatalf("VerifyCode = %q", verifyMsg.VerifyCode)
	}
	if verifyMsg.Method != "phone" {
		t.Fatalf("Method = %q", verifyMsg.Method)
	}
}

func TestOnboardingLoadMobileFallback(t *testing.T) {
	m := NewOnboardingModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{RemoteMobile: "13800138000"})
	if got := m.emailInput.Value(); got != "13800138000" {
		t.Fatalf("email input = %q, want phone fallback from RemoteMobile", got)
	}
}

func TestOnboardingEmptyIdentityShowsError(t *testing.T) {
	m := NewOnboardingModel("en")
	m.emailInput.SetValue("")
	m.cursor = onboardingRowEmail
	m.focusCursor()

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Fatal("empty identity should not dispatch command")
	}
	if got := m.remoteStatus; got != onboardingText("en", "identityRequired") {
		t.Fatalf("remote status = %q", got)
	}
}

func TestOnboardingSMSTickCountdown(t *testing.T) {
	m := NewOnboardingModel("en")
	m.codeSent = true
	m.codeCountdown = 3

	m, cmd := m.Update(OnboardingSMSTickMsg{})
	if m.codeCountdown != 2 {
		t.Fatalf("countdown = %d, want 2", m.codeCountdown)
	}
	if cmd == nil {
		t.Fatal("expected next tick command while countdown > 0")
	}

	m.codeCountdown = 1
	m, cmd = m.Update(OnboardingSMSTickMsg{})
	if m.codeCountdown != 0 {
		t.Fatalf("countdown = %d, want 0", m.codeCountdown)
	}
	if cmd != nil {
		t.Fatal("expected no tick command at countdown = 0")
	}
}

func TestOnboardingHubCenterRowNotVisibleToUser(t *testing.T) {
	m := NewOnboardingModel("en")
	rows := m.activeRows()
	for _, r := range rows {
		if r == onboardingRowHubCenter {
			t.Fatal("HubCenter row should not be in activeRows for user-facing navigation")
		}
	}
}
