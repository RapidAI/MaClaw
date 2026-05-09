package views

import (
	"strings"
	"testing"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestServiceRedeemLoadsHubStateFromConfig(t *testing.T) {
	m := NewServiceRedeemModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteEmail:              "user@example.com",
		RemoteHubURL:             "https://hub.example/",
		RemoteHubCenterURL:       "https://center.example/",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
	})

	if !m.hubReady {
		t.Fatal("expected hubReady after Hub URL and viewer token are loaded")
	}
	if m.email != "user@example.com" {
		t.Fatalf("email = %q", m.email)
	}
	if m.hubURL != "https://hub.example" {
		t.Fatalf("hubURL = %q", m.hubURL)
	}
	if m.hubCenter != "https://center.example" {
		t.Fatalf("hubCenter = %q", m.hubCenter)
	}
	if m.provider != serviceRedeemOfficialProviderName {
		t.Fatalf("provider = %q", m.provider)
	}
}

func TestServiceRedeemBlocksSubmitBeforeHubActivation(t *testing.T) {
	m := NewServiceRedeemModel("zh")
	m.input.SetValue("SERVICE-CODE")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected setup navigation command before Hub activation")
	}
	if _, ok := cmd().(ServiceRedeemOpenSetupMsg); !ok {
		t.Fatalf("command returned %T, want ServiceRedeemOpenSetupMsg", cmd())
	}
	if m.busy {
		t.Fatal("redeem should not become busy before Hub activation")
	}
	if m.status != serviceRedeemText("zh", "setupRequired") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestServiceRedeemSubmitsWhenHubReady(t *testing.T) {
	m := NewServiceRedeemModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	})
	m.input.SetValue("SERVICE-CODE")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected submit command when Hub is ready")
	}
	if !m.busy {
		t.Fatal("expected busy state after submit")
	}
	msg, ok := cmd().(ServiceRedeemSubmitMsg)
	if !ok {
		t.Fatalf("command returned %T", cmd())
	}
	if msg.Code != "SERVICE-CODE" {
		t.Fatalf("code = %q", msg.Code)
	}
}

func TestServiceRedeemEnterRefreshesWhenOfficialReadyAndCodeEmpty(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:             "https://hub.example",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         serviceRedeemOfficialProviderName,
			IsHubService: true,
		}},
	})

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected refresh command when official service is already ready")
	}
	if !m.busy {
		t.Fatal("expected busy state while refreshing official service")
	}
	if _, ok := cmd().(ServiceRedeemRefreshMsg); !ok {
		t.Fatalf("command returned %T, want ServiceRedeemRefreshMsg", cmd())
	}
	if m.status != serviceRedeemText("en", "checking") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestServiceRedeemCodeInputIsMasked(t *testing.T) {
	m := NewServiceRedeemModel("en")
	if m.input.EchoMode != textinput.EchoPassword {
		t.Fatalf("redeem code input should use password echo, got %v", m.input.EchoMode)
	}
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	})
	m.input.SetValue("SERVICE-CODE")
	view := stripANSIForTest(m.View())
	if strings.Contains(view, "SERVICE-CODE") {
		t.Fatalf("redeem code should not be visible in the TUI:\n%s", view)
	}
	if !strings.Contains(view, "********") {
		t.Fatalf("redeem code input should show a masked value:\n%s", view)
	}
}

func TestServiceRedeemViewHidesCodeInputUntilHubReady(t *testing.T) {
	m := NewServiceRedeemModel("zh")
	view := stripANSIForTest(m.View())
	if strings.Contains(view, "兑换码  ") {
		t.Fatalf("redeem code input should be hidden until Hub is ready: %s", view)
	}
	if !strings.Contains(view, "初始化") {
		t.Fatalf("missing setup guidance before Hub activation: %s", view)
	}

	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	})
	view = stripANSIForTest(m.View())
	if !strings.Contains(view, "兑换码  ") {
		t.Fatalf("redeem code input should be visible after Hub is ready: %s", view)
	}
}

func TestServiceRedeemRefreshesWhenHubReady(t *testing.T) {
	m := NewServiceRedeemModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	})

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	if cmd == nil {
		t.Fatal("expected refresh command when Hub is ready")
	}
	if !m.busy {
		t.Fatal("expected busy state while refreshing")
	}
	if _, ok := cmd().(ServiceRedeemRefreshMsg); !ok {
		t.Fatalf("command returned %T, want ServiceRedeemRefreshMsg", cmd())
	}
}

func TestServiceRedeemShowsOfficialProviderReady(t *testing.T) {
	m := NewServiceRedeemModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:             "https://hub.example",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         serviceRedeemOfficialProviderName,
			IsHubService: true,
		}},
	})
	if !m.officialReady {
		t.Fatal("expected officialReady for MaClaw official provider")
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "MaClaw 官方 LLM 已配置") {
		t.Fatalf("expected official-ready status in view: %s", view)
	}
}

func TestServiceRedeemEnglishDisplaysOfficialProviderName(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:             "https://hub.example",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         serviceRedeemOfficialProviderName,
			IsHubService: true,
		}},
	})
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "LLM provider: MaClaw Official") {
		t.Fatalf("official provider should be displayed in English:\n%s", view)
	}
	if strings.Contains(view, serviceRedeemOfficialProviderName) {
		t.Fatalf("internal provider name should not leak in English UI:\n%s", view)
	}
}

func TestServiceRedeemRecognizesEnglishOfficialProviderName(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:             "https://hub.example",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: "MaClaw Official",
		MaclawLLMUrl:             "https://hub.example/api/llm",
		MaclawLLMModel:           "qwen-max",
	})
	if !m.officialReady {
		t.Fatal("expected English MaClaw Official provider name to be treated as official service")
	}
	if got := m.status; got != serviceRedeemText("en", "officialReady") {
		t.Fatalf("status = %q", got)
	}
}

func TestServiceRedeemViewFitsNarrowTerminal(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 18})
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteEmail:              "very-long-user-name@example.com",
		RemoteHubURL:             "https://hub.example/some/long/path",
		RemoteHubCenterURL:       "https://center.example/some/long/path",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         serviceRedeemOfficialProviderName,
			IsHubService: true,
		}},
	})
	m.status = "a very long service status message that should fit the terminal"

	view := stripANSIForTest(m.View())
	for _, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > 44 {
			t.Fatalf("line width = %d, want <= 44: %q\nview:\n%s", got, line, view)
		}
	}
}

func TestServiceRedeemHubURLLabelFollowsLanguage(t *testing.T) {
	m := NewServiceRedeemModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:      "https://hub.example/some/long/path",
		RemoteViewerToken: "viewer-token",
	})

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "已选择 Hub: https://hub.example/some/long/path") {
		t.Fatalf("Chinese service redeem view should localize the Hub URL label:\n%s", view)
	}
	if strings.Contains(view, "\n  Hub: https://hub.example/some/long/path") {
		t.Fatalf("Chinese service redeem view should not keep the English Hub URL label:\n%s", view)
	}
}

func TestServiceRedeemCompactViewKeepsCodeAndStatusVisible(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m, _ = m.Update(tea.WindowSizeMsg{Width: 44, Height: 12})
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:             "https://hub.example/some/long/path",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         serviceRedeemOfficialProviderName,
			IsHubService: true,
		}},
		LocalMCPServers: []corelib.LocalMCPServerEntry{{Name: "filesystem"}},
	})
	m.status = "a very long service status message that should fit the terminal"
	m.input.SetValue("SERVICE-CODE")

	view := stripANSIForTest(m.View())
	if strings.Contains(view, "Redeem a MaClaw official service code") {
		t.Fatalf("compact service view should drop the subtitle to keep actions visible:\n%s", view)
	}
	if !strings.Contains(view, "Code") || !strings.Contains(view, "Status") || !strings.Contains(view, "F2 Chat") || !strings.Contains(view, "Enter refreshes") {
		t.Fatalf("compact service view should keep redeem controls visible:\n%s", view)
	}
	if strings.Contains(view, "SERVICE-CODE") || !strings.Contains(view, "********") {
		t.Fatalf("compact service view should keep service code masked:\n%s", view)
	}
	lines := strings.Split(strings.TrimRight(view, "\n"), "\n")
	if len(lines) > 8 {
		t.Fatalf("compact service lines = %d, want <= root content height 8:\n%s", len(lines), view)
	}
	for _, line := range lines {
		if got := lipgloss.Width(line); got > 44 {
			t.Fatalf("line width = %d, want <= 44: %q\nview:\n%s", got, line, view)
		}
	}
}

func TestServiceRedeemEnterBeforeHubOpensSetup(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.input.SetValue("SERVICE-CODE")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected setup navigation command")
	}
	if _, ok := cmd().(ServiceRedeemOpenSetupMsg); !ok {
		t.Fatalf("command returned %T, want ServiceRedeemOpenSetupMsg", cmd())
	}
	if m.status != serviceRedeemText("en", "setupRequired") {
		t.Fatalf("status = %q", m.status)
	}
}

func TestServiceRedeemMissingSetupHintMentionsEnter(t *testing.T) {
	view := stripANSIForTest(NewServiceRedeemModel("en").View())
	if !strings.Contains(view, "Press Enter to open Setup") || !strings.Contains(view, "Enter opens Setup") {
		t.Fatalf("missing setup hint should mention Enter navigation:\n%s", view)
	}
}

func TestServiceRedeemMissingSetupKeepsPrefilledCodeMasked(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.SetInitialCode("SERVICE-CODE")

	view := stripANSIForTest(m.View())
	if strings.Contains(view, "SERVICE-CODE") {
		t.Fatalf("prefilled code should remain masked before setup:\n%s", view)
	}
	if !strings.Contains(view, "********") || !strings.Contains(view, "Code saved") {
		t.Fatalf("missing setup view should show masked saved-code guidance:\n%s", view)
	}
}

func TestServiceRedeemFailureResultMarksFailure(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	})
	m, _ = m.Update(ServiceRedeemResultMsg{Success: false, Message: "quota exhausted"})
	if !m.lastFailure {
		t.Fatal("expected failed result to mark lastFailure")
	}
	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "quota exhausted") || !strings.Contains(view, "Check the code") || !strings.Contains(view, "Ctrl+U clears") {
		t.Fatalf("failed redeem should show error and retry guidance:\n%s", view)
	}
	m, _ = m.Update(ServiceRedeemResultMsg{Success: true, Message: "ok"})
	if m.lastFailure {
		t.Fatal("successful result should clear lastFailure")
	}
}

func TestServiceRedeemFailureClearsAfterCodeEditOrClear(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	})
	m.SetInitialCode("OLD-CODE")
	m, _ = m.Update(ServiceRedeemResultMsg{Success: false, Message: "bad code"})

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	if m.lastFailure {
		t.Fatal("editing code should clear stale failure state")
	}
	if got := m.status; got != serviceRedeemText("en", "codeReady") {
		t.Fatalf("status after edit = %q", got)
	}

	m, _ = m.Update(ServiceRedeemResultMsg{Success: false, Message: "bad code"})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	if m.lastFailure || m.lastSuccess {
		t.Fatalf("Ctrl+U should clear result flags, success=%v failure=%v", m.lastSuccess, m.lastFailure)
	}
	if got := m.CodeValueForTest(); got != "" {
		t.Fatalf("Ctrl+U should clear code, got %q", got)
	}
	if got := m.status; got != serviceRedeemText("en", "ready") {
		t.Fatalf("status after clear = %q", got)
	}
}

func TestServiceRedeemRefreshResultDoesNotClearPrefilledCode(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.SetInitialCode("SERVICE-CODE")

	m, _ = m.Update(ServiceRedeemResultMsg{Success: true, Message: "official ready", FromRefresh: true})
	if got := m.CodeValueForTest(); got != "SERVICE-CODE" {
		t.Fatalf("refresh result should preserve prefilled code, got %q", got)
	}

	m, _ = m.Update(ServiceRedeemResultMsg{Success: true, Message: "redeemed"})
	if got := m.CodeValueForTest(); got != "" {
		t.Fatalf("redeem success should clear submitted code, got %q", got)
	}
}

func TestServiceRedeemSetLangRelocalizesSetupRequired(t *testing.T) {
	m := NewServiceRedeemModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{})
	m.SetLang("en")
	if got := m.status; got != serviceRedeemText("en", "setupRequired") {
		t.Fatalf("status = %q", got)
	}
}

func TestServiceRedeemSetLangRelocalizesOfficialReady(t *testing.T) {
	m := NewServiceRedeemModel("zh")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:             "https://hub.example",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         serviceRedeemOfficialProviderName,
			IsHubService: true,
		}},
	})
	m.SetLang("en")
	if got := m.status; got != serviceRedeemText("en", "officialReady") {
		t.Fatalf("status = %q", got)
	}
}

func TestServiceRedeemNormalizesPastedCode(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:      "https://hub.example",
		RemoteViewerToken: "viewer-token",
	})
	m.input.SetValue("  SERVICE  CODE\n")

	m, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected submit command")
	}
	msg := cmd().(ServiceRedeemSubmitMsg)
	if msg.Code != "SERVICECODE" {
		t.Fatalf("code = %q", msg.Code)
	}
	if got := m.input.Value(); got != "SERVICECODE" {
		t.Fatalf("input value = %q", got)
	}
}

func TestServiceRedeemInitialCodeIsMaskedAndReady(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.SetInitialCode("  SERVICE  CODE\n")
	if got := m.CodeValueForTest(); got != "SERVICECODE" {
		t.Fatalf("initial code = %q", got)
	}
	if m.status != serviceRedeemText("en", "codeReady") {
		t.Fatalf("status = %q, want codeReady", m.status)
	}
	view := stripANSIForTest(m.View())
	if strings.Contains(view, "SERVICECODE") {
		t.Fatalf("view should keep initial code masked:\n%s", view)
	}
}

func TestServiceRedeemShowsMCPNextStepAfterOfficialReady(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:             "https://hub.example",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
		MaclawLLMUrl:             "https://hub.example/api/llm",
		MaclawLLMModel:           "auto",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         serviceRedeemOfficialProviderName,
			IsHubService: true,
		}},
	})

	view := stripANSIForTest(m.View())
	if !strings.Contains(view, "F3 opens Tools/MCP templates") || !strings.Contains(view, "F2 opens Chat") {
		t.Fatalf("official-ready redeem page should guide next MCP/chat actions:\n%s", view)
	}
}

func TestServiceRedeemHidesMCPNextStepWhenMCPConfigured(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m.LoadFromAppConfig(corelib.AppConfig{
		RemoteHubURL:             "https://hub.example",
		RemoteViewerToken:        "viewer-token",
		MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
		MaclawLLMUrl:             "https://hub.example/api/llm",
		MaclawLLMModel:           "auto",
		MaclawLLMProviders: []corelib.MaclawLLMProvider{{
			Name:         serviceRedeemOfficialProviderName,
			IsHubService: true,
		}},
		LocalMCPServers: []corelib.LocalMCPServerEntry{{Name: "filesystem"}},
	})

	view := stripANSIForTest(m.View())
	if strings.Contains(view, "F3 opens Tools/MCP templates") {
		t.Fatalf("MCP next-step hint should be hidden once MCP is configured:\n%s", view)
	}
	if !strings.Contains(view, "press F2 to start chatting") || !strings.Contains(view, "F2 opens Chat") {
		t.Fatalf("official-ready page with MCP configured should guide directly to chat:\n%s", view)
	}
}

func TestServiceRedeemResultConfigUpdatesMCPNextStepState(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m, _ = m.Update(ServiceRedeemResultMsg{
		Success:   true,
		Message:   "official ready",
		HasConfig: true,
		Config: corelib.AppConfig{
			RemoteHubURL:             "https://hub.example",
			RemoteViewerToken:        "viewer-token",
			MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
			MaclawLLMUrl:             "https://hub.example/api/llm",
			MaclawLLMModel:           "auto",
			MaclawLLMProviders: []corelib.MaclawLLMProvider{{
				Name:         serviceRedeemOfficialProviderName,
				IsHubService: true,
			}},
		},
	})
	if !m.NeedsMCPNextStep() {
		t.Fatal("expected successful official-service config without MCP to enable the MCP next-step hint")
	}

	m, _ = m.Update(ServiceRedeemResultMsg{
		Success:   true,
		Message:   "official ready",
		HasConfig: true,
		Config: corelib.AppConfig{
			RemoteHubURL:             "https://hub.example",
			RemoteViewerToken:        "viewer-token",
			MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
			MaclawLLMUrl:             "https://hub.example/api/llm",
			MaclawLLMModel:           "auto",
			MaclawLLMProviders: []corelib.MaclawLLMProvider{{
				Name:         serviceRedeemOfficialProviderName,
				IsHubService: true,
			}},
			MCPServers: []corelib.MCPServerEntry{{Name: "remote"}},
		},
	})
	if m.NeedsMCPNextStep() {
		t.Fatal("MCP next-step hint should turn off after MCP servers are present")
	}
}

func TestServiceRedeemResultKeepsReadableStatusWhenMessageEmpty(t *testing.T) {
	m := NewServiceRedeemModel("en")
	m, _ = m.Update(ServiceRedeemResultMsg{
		Success:   true,
		HasConfig: true,
		Config: corelib.AppConfig{
			RemoteHubURL:             "https://hub.example",
			RemoteViewerToken:        "viewer-token",
			MaclawLLMCurrentProvider: serviceRedeemOfficialProviderName,
			MaclawLLMUrl:             "https://hub.example/api/llm",
			MaclawLLMModel:           "auto",
			MaclawLLMProviders: []corelib.MaclawLLMProvider{{
				Name:         serviceRedeemOfficialProviderName,
				IsHubService: true,
			}},
		},
	})

	if got := m.status; got != serviceRedeemText("en", "officialReady") {
		t.Fatalf("status = %q, want official-ready fallback", got)
	}
}

func TestNormalizeServiceRedeemCodeForInput(t *testing.T) {
	if got := NormalizeServiceRedeemCodeForInput(" A B\tC\n"); got != "ABC" {
		t.Fatalf("normalized code = %q", got)
	}
}
