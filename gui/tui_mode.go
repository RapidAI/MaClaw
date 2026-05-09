package main

// tui_mode.go provides a terminal-based interactive mode that uses the same
// IMMessageHandler as the desktop GUI and IM channels. Launched via `maclaw tui`.
//
// This is the canonical TUI implementation. It uses the exact same agent code
// path as desktop. The independent maclaw-tui binary (tui/) is a legacy path
// that will be deprecated in favor of this unified implementation.

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/tui/views"
	tea "github.com/charmbracelet/bubbletea"
)

const (
	guiTUIAppNotInitializedMsg       = "app is not initialized"
	guiTUIOfficialServiceInactiveMsg = "official service is not active"
)

// runTUIMode starts the terminal-based interactive mode.
func runTUIMode(app *App) {
	if app == nil {
		app = NewApp()
	}

	cfg, cfgErr := app.LoadConfig()
	lang := strings.TrimSpace(cfg.Language)
	if lang == "" {
		lang = "zh"
	}
	root := views.NewRootModel(lang)
	root.Config.LoadFromAppConfig(cfg)
	root.Onboarding.LoadFromAppConfig(cfg)
	root.Service.LoadFromAppConfig(cfg)
	loadGUITUIToolData(&root, cfg)
	configureGUITUIInitialTab(&root, cfg)
	if cfgErr != nil {
		root.StatusBar.SetMessage(fmt.Sprintf("config load failed: %v", cfgErr))
	}

	tuiApp := &tuiModeApp{
		app:  app,
		root: root,
	}
	p := tea.NewProgram(tuiApp, tea.WithAltScreen())
	tuiApp.program = p

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "TUI error: %v\n", err)
		os.Exit(1)
	}

	if tuiApp.handler != nil && tuiApp.handler.memory != nil {
		tuiApp.handler.memory.Stop()
	}
}

type tuiModeApp struct {
	handler *IMMessageHandler
	app     *App
	program *tea.Program
	root    views.RootModel
}

func (a *tuiModeApp) Init() tea.Cmd {
	return nil
}

type guiTUIOnboardingFinishedMsg struct {
	Config corelib.AppConfig
	Error  string
}

func (a *tuiModeApp) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}

	case views.ChatSendMsg:
		return a, a.sendMessage(msg.Text)

	case views.OnboardingActivateRemoteMsg:
		return a, a.activateRemote(msg.Email, msg.HubCenterURL)

	case views.OnboardingStartWeixinMsg:
		return a, a.startWeixinQR()

	case views.OnboardingPollWeixinMsg:
		return a, a.pollWeixinQR(msg.Token)

	case views.OnboardingRemoteResultMsg:
		if msg.Success {
			a.reloadConfigBackedViews()
		}
		var cmd tea.Cmd
		a.root, cmd = a.root.Update(msg)
		a.root.StatusBar.SetMessage(msg.Message)
		return a, cmd

	case views.OnboardingFinishMsg:
		return a, a.finishOnboarding()

	case guiTUIOnboardingFinishedMsg:
		if msg.Error != "" {
			a.root.StatusBar.SetMessage(msg.Error)
			return a, nil
		}
		a.reloadConfigBackedViews()
		switch views.ConfigSetupStatus(msg.Config) {
		case views.ConfigSetupNeedsRedeem:
			a.root.SetTab(views.TabServiceRedeem)
			a.root.StatusBar.SetMessage("Hub is ready; redeem or refresh service")
			return a, nil
		case views.ConfigSetupLLMReady, views.ConfigSetupOfficialReady, views.ConfigSetupMCPOptional:
			a.root.SetTab(views.TabChat)
			a.root.StatusBar.SetMessage("setup complete")
			return a, nil
		}
		a.root.SetTab(views.TabConfig)
		a.root.Config.FocusLLMConfig()
		a.root.StatusBar.SetMessage("setup complete; configure an LLM provider")
		return a, nil

	case views.OnboardingLanguageChangedMsg:
		return a, a.saveOnboardingLanguage(msg.Language)

	case views.OnboardingWeixinQRMsg, views.OnboardingWeixinPollResultMsg:
		var cmd tea.Cmd
		a.root, cmd = a.root.Update(msg)
		if result, ok := msg.(views.OnboardingWeixinPollResultMsg); ok && result.Success {
			a.reloadConfigBackedViews()
			a.root.StatusBar.SetMessage("WeChat bound")
		}
		return a, cmd

	case views.ConfigSaveMsg:
		return a, a.saveConfig(msg)

	case views.ConfigSaveFailedMsg:
		a.root.StatusBar.SetMessage(fmt.Sprintf("save config failed: %s", msg.Error))
		return a, nil

	case views.ConfigSavedMsg:
		if msg.Key == "language" && strings.TrimSpace(msg.Value) != "" {
			a.root.SetLang(msg.Value)
		}
		a.reloadConfigBackedViews()
		a.root.StatusBar.SetMessage(fmt.Sprintf("saved %s", views.ConfigDisplayNameForLang(msg.Key, a.root.Lang())))
		return a, nil

	case views.ConfigOpenSetupMsg:
		a.root.SetTab(views.TabOnboarding)
		a.root.StatusBar.SetMessage("Open setup")
		return a, nil

	case views.ConfigOpenServiceRedeemMsg:
		a.root.SetTab(views.TabServiceRedeem)
		a.root.StatusBar.SetMessage("Open service redeem")
		return a, nil

	case views.ConfigOpenToolsMsg:
		a.root.SetTab(views.TabTools)
		a.root.Tools.FocusMCP()
		a.root.StatusBar.SetMessage("Open tools")
		return a, nil

	case views.ServiceRedeemOpenSetupMsg:
		a.root.SetTab(views.TabOnboarding)
		a.root.StatusBar.SetMessage("Open setup")
		return a, nil

	case views.ServiceRedeemRefreshMsg:
		return a, a.refreshServiceStatus()

	case views.ServiceRedeemSubmitMsg:
		return a, a.redeemServiceCode(msg.Code)

	case views.ServiceRedeemResultMsg:
		if msg.Success && msg.HasConfig {
			a.root.Config.LoadFromAppConfig(msg.Config)
			a.root.Onboarding.LoadFromAppConfig(msg.Config)
			a.root.Service.LoadFromAppConfig(msg.Config)
			loadGUITUIToolData(&a.root, msg.Config)
		}
		var cmd tea.Cmd
		a.root, cmd = a.root.Update(msg)
		a.root.StatusBar.SetMessage(msg.Message)
		return a, cmd

	case views.ToolMCPAddMsg:
		return a, a.addLocalMCP(msg.Entry)

	case views.ToolMCPAddRemoteMsg:
		return a, a.addRemoteMCP(msg.Entry)

	case views.ToolOperationResultMsg:
		if msg.Success {
			a.reloadConfigBackedViews()
		}
		var cmd tea.Cmd
		a.root, cmd = a.root.Update(msg)
		a.root.StatusBar.SetMessage(msg.Message)
		return a, cmd

	case views.ToolRefreshMsg:
		a.reloadConfigBackedViews()
		a.root.StatusBar.SetMessage("tools refreshed")
		return a, nil

	case views.ToolSkillSearchMsg:
		return a, func() tea.Msg {
			return views.ToolSkillSearchResultMsg{Error: "Skill search is not available in maclaw tui yet"}
		}

	case views.ToolSkillInstallMsg:
		return a, func() tea.Msg {
			return views.ToolOperationResultMsg{Tab: views.ToolSubSkill, Success: false, Message: "Skill install is not available in maclaw tui yet"}
		}
	}

	var cmd tea.Cmd
	a.root, cmd = a.root.Update(msg)
	return a, cmd
}

func (a *tuiModeApp) View() string {
	return a.root.View()
}

func configureGUITUIInitialTab(root *views.RootModel, cfg corelib.AppConfig) {
	switch views.ConfigSetupStatus(cfg) {
	case views.ConfigSetupNeedsLLMKey:
		root.SetTab(views.TabConfig)
		root.Config.FocusLLMKey()
		root.StatusBar.SetMessage("LLM key is required")
	case views.ConfigSetupLLMReady, views.ConfigSetupOfficialReady, views.ConfigSetupMCPOptional:
		root.SetTab(views.TabChat)
	case views.ConfigSetupNeedsRedeem:
		root.SetTab(views.TabServiceRedeem)
		root.StatusBar.SetMessage("Redeem or refresh MaClaw official service")
	case views.ConfigSetupNeedsSetup:
		if !cfg.OnboardingDone {
			root.SetTab(views.TabOnboarding)
			root.StatusBar.SetMessage("Open setup to finish first-run configuration")
			return
		}
		root.SetTab(views.TabConfig)
		root.Config.FocusLLMConfig()
		root.StatusBar.SetMessage("Configure an LLM provider before chat")
	default:
		root.SetTab(views.TabConfig)
		root.Config.FocusLLMConfig()
		root.StatusBar.SetMessage("Configure an LLM provider before chat")
	}
}

func (a *tuiModeApp) reloadConfigBackedViews() {
	if a == nil || a.app == nil {
		return
	}
	cfg, err := a.app.LoadConfig()
	if err != nil {
		a.root.StatusBar.SetMessage(fmt.Sprintf("config load failed: %v", err))
		return
	}
	a.root.Config.LoadFromAppConfig(cfg)
	a.root.Onboarding.LoadFromAppConfig(cfg)
	a.root.Service.LoadFromAppConfig(cfg)
	loadGUITUIToolData(&a.root, cfg)
}

func (a *tuiModeApp) activateRemote(email, hubCenterURL string) tea.Cmd {
	return func() tea.Msg {
		if a.app == nil {
			return views.OnboardingRemoteResultMsg{Success: false, Message: guiTUIAppNotInitializedMsg}
		}
		hubCenterURL = strings.TrimRight(strings.TrimSpace(hubCenterURL), "/")
		if hubCenterURL != "" {
			if err := a.app.PatchConfig(func(cfg *corelib.AppConfig) {
				cfg.RemoteHubCenterURL = hubCenterURL
			}); err != nil {
				return views.OnboardingRemoteResultMsg{Success: false, Message: err.Error()}
			}
		}
		result, err := a.app.ActivateRemote(email, "", "")
		if err != nil {
			return views.OnboardingRemoteResultMsg{Success: false, Message: err.Error()}
		}
		cfg, loadErr := a.app.LoadConfig()
		if loadErr != nil {
			return views.OnboardingRemoteResultMsg{Success: false, Message: loadErr.Error()}
		}
		hubServiceReady := strings.TrimSpace(cfg.RemoteHubURL) != "" && strings.TrimSpace(cfg.RemoteViewerToken) != ""
		machineReady := strings.TrimSpace(cfg.RemoteMachineID) != "" &&
			strings.TrimSpace(cfg.RemoteMachineToken) != "" &&
			strings.TrimSpace(cfg.RemoteViewerToken) != ""
		message := strings.TrimSpace(result.Message)
		if message == "" {
			message = "activated"
		}
		return views.OnboardingRemoteResultMsg{
			Success:         true,
			Message:         message,
			HubURL:          cfg.RemoteHubURL,
			MachineID:       result.MachineID,
			HubServiceReady: hubServiceReady,
			MachineReady:    machineReady,
		}
	}
}

func (a *tuiModeApp) finishOnboarding() tea.Cmd {
	return func() tea.Msg {
		if a.app == nil {
			return guiTUIOnboardingFinishedMsg{Error: guiTUIAppNotInitializedMsg}
		}
		if err := a.app.PatchConfig(func(cfg *corelib.AppConfig) {
			cfg.OnboardingDone = true
		}); err != nil {
			return guiTUIOnboardingFinishedMsg{Error: err.Error()}
		}
		cfg, err := a.app.LoadConfig()
		if err != nil {
			return guiTUIOnboardingFinishedMsg{Error: err.Error()}
		}
		return guiTUIOnboardingFinishedMsg{Config: cfg}
	}
}

func (a *tuiModeApp) saveConfig(msg views.ConfigSaveMsg) tea.Cmd {
	return func() tea.Msg {
		if a.app == nil {
			return views.ConfigSaveFailedMsg{Key: msg.Key, Error: guiTUIAppNotInitializedMsg}
		}
		cfg := msg.Config
		if !msg.HasConfig {
			var err error
			cfg, err = a.app.LoadConfig()
			if err != nil {
				return views.ConfigSaveFailedMsg{Key: msg.Key, Error: err.Error()}
			}
			views.ApplyConfigValue(&cfg, msg.Key, msg.Value)
		}
		if err := a.app.SaveConfig(cfg); err != nil {
			return views.ConfigSaveFailedMsg{Key: msg.Key, Error: err.Error()}
		}
		return views.ConfigSavedMsg{Key: msg.Key, Value: msg.Value}
	}
}

func (a *tuiModeApp) addLocalMCP(entry corelib.LocalMCPServerEntry) tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		entry.Name = strings.TrimSpace(entry.Name)
		entry.Command = strings.TrimSpace(entry.Command)
		if entry.Name == "" || entry.Command == "" {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: "MCP name and command are required"}
		}
		if a.app == nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: guiTUIAppNotInitializedMsg}
		}
		entry.ID = guiTUIMCPID("local", entry.Name, now)
		entry.CreatedAt = now.Format(time.RFC3339)
		if err := a.app.PatchConfig(func(cfg *corelib.AppConfig) {
			cfg.LocalMCPServers = append(cfg.LocalMCPServers, entry)
		}); err != nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: err.Error()}
		}
		return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: true, Message: fmt.Sprintf("added %s", entry.Name)}
	}
}

func (a *tuiModeApp) addRemoteMCP(entry corelib.MCPServerEntry) tea.Cmd {
	return func() tea.Msg {
		now := time.Now()
		entry.Name = strings.TrimSpace(entry.Name)
		entry.EndpointURL = strings.TrimSpace(entry.EndpointURL)
		entry.AuthType = strings.TrimSpace(entry.AuthType)
		entry.AuthSecret = strings.TrimSpace(entry.AuthSecret)
		if entry.Name == "" || entry.EndpointURL == "" {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: "MCP name and endpoint URL are required"}
		}
		if a.app == nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: guiTUIAppNotInitializedMsg}
		}
		entry.ID = guiTUIMCPID("remote", entry.Name, now)
		entry.CreatedAt = now.Format(time.RFC3339)
		entry.Source = corelib.MCPSourceManual
		if err := a.app.PatchConfig(func(cfg *corelib.AppConfig) {
			cfg.MCPServers = append(cfg.MCPServers, entry)
		}); err != nil {
			return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: false, Message: err.Error()}
		}
		return views.ToolOperationResultMsg{Tab: views.ToolSubMCP, Success: true, Message: fmt.Sprintf("added %s", entry.Name)}
	}
}

func guiTUIMCPID(kind, name string, now time.Time) string {
	name = strings.TrimSpace(name)
	name = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '-' || r == '_':
			return r
		case r == ' ':
			return '_'
		default:
			return -1
		}
	}, name)
	name = strings.Trim(name, "_-")
	if name == "" {
		name = "server"
	}
	return fmt.Sprintf("%s_%s_%d", kind, name, now.UnixMilli())
}

func (a *tuiModeApp) startWeixinQR() tea.Cmd {
	return func() tea.Msg {
		if a.app == nil {
			return views.OnboardingWeixinQRMsg{Success: false, Message: guiTUIAppNotInitializedMsg}
		}
		result := a.app.StartWeixinQRLogin()
		if errText := strings.TrimSpace(result["error"]); errText != "" {
			return views.OnboardingWeixinQRMsg{Success: false, Message: errText}
		}
		qr := strings.TrimSpace(result["qrcode_url"])
		token := strings.TrimSpace(result["qrcode_token"])
		if qr == "" || token == "" {
			return views.OnboardingWeixinQRMsg{Success: false, Message: "empty WeChat QR response"}
		}
		return views.OnboardingWeixinQRMsg{Success: true, QR: qr, Token: token}
	}
}

func (a *tuiModeApp) pollWeixinQR(token string) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(1 * time.Second)
		if a.app == nil {
			return views.OnboardingWeixinPollResultMsg{Status: "error", Message: guiTUIAppNotInitializedMsg, Completed: true}
		}
		result := a.app.PollWeixinQRStatus(token)
		status := strings.TrimSpace(result["status"])
		message := strings.TrimSpace(result["message"])
		if errText := strings.TrimSpace(result["error"]); errText != "" {
			if message == "" {
				message = errText
			}
			return views.OnboardingWeixinPollResultMsg{Status: status, Message: message, Completed: true}
		}
		switch status {
		case "confirmed":
			return views.OnboardingWeixinPollResultMsg{Status: status, Message: message, Success: true, Completed: true, AccountID: result["account_id"]}
		case "expired", "error":
			return views.OnboardingWeixinPollResultMsg{Status: status, Message: message, Completed: true}
		default:
			return views.OnboardingWeixinPollResultMsg{Status: status, Message: message}
		}
	}
}

func (a *tuiModeApp) refreshServiceStatus() tea.Cmd {
	return func() tea.Msg {
		if a.app == nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: guiTUIAppNotInitializedMsg, FromRefresh: true}
		}
		status, err := a.app.GetHubLLMServiceStatus()
		if err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: err.Error(), FromRefresh: true}
		}
		if !guiTUIHubServiceReady(status) {
			return views.ServiceRedeemResultMsg{Success: false, Message: guiTUIOfficialServiceInactiveMsg, FromRefresh: true}
		}
		cfg, cfgErr := a.app.LoadConfig()
		if cfgErr != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: cfgErr.Error(), FromRefresh: true}
		}
		return guiTUIServiceSuccessResult("official service ready", status, cfg, true)
	}
}

func (a *tuiModeApp) redeemServiceCode(code string) tea.Cmd {
	return func() tea.Msg {
		if a.app == nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: guiTUIAppNotInitializedMsg}
		}
		status, err := a.app.RedeemHubLLMService(code)
		if err != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: err.Error()}
		}
		if !guiTUIHubServiceReady(status) {
			return views.ServiceRedeemResultMsg{Success: false, Message: guiTUIOfficialServiceInactiveMsg}
		}
		cfg, cfgErr := a.app.LoadConfig()
		if cfgErr != nil {
			return views.ServiceRedeemResultMsg{Success: false, Message: cfgErr.Error()}
		}
		return guiTUIServiceSuccessResult("redeem succeeded", status, cfg, false)
	}
}

func guiTUIServiceSuccessResult(message string, status HubLLMServiceStatus, cfg corelib.AppConfig, fromRefresh bool) views.ServiceRedeemResultMsg {
	expires, credits := guiTUIServiceExpiryAndCredits(status)
	return views.ServiceRedeemResultMsg{
		Success:          true,
		Message:          message,
		ProviderName:     hubServiceProviderName,
		CreditsRemaining: credits,
		ExpiresAt:        expires,
		FromRefresh:      fromRefresh,
		Config:           cfg,
		HasConfig:        true,
	}
}

func guiTUIServiceExpiryAndCredits(status HubLLMServiceStatus) (string, float64) {
	expires := status.EffectiveExpiresAt
	if expires == "" {
		expires = status.NearestExpiresAt
	}
	credits := status.CreditsRemaining
	if credits == 0 {
		credits = status.CreditsAvailable
	}
	return expires, credits
}

func guiTUIHubServiceReady(status HubLLMServiceStatus) bool {
	return status.Active && strings.TrimSpace(status.HubLLMBaseURL) != ""
}

func loadGUITUIToolData(root *views.RootModel, cfg corelib.AppConfig) {
	if root == nil {
		return
	}
	var skills []views.SkillItem
	for _, sk := range cfg.NLSkills {
		skills = append(skills, views.SkillItem{
			Name:        sk.Name,
			Description: sk.Description,
			Status:      sk.Status,
			Source:      sk.Source,
			Publisher:   sk.Publisher,
		})
	}
	root.Tools.SetSkills(skills)

	var servers []views.MCPItem
	for _, srv := range cfg.LocalMCPServers {
		status := "stopped"
		if !srv.Disabled {
			status = "ready"
		}
		servers = append(servers, views.MCPItem{
			ID:       srv.ID,
			Name:     srv.Name,
			Type:     "local",
			Status:   status,
			Endpoint: srv.Command + " " + strings.Join(srv.Args, " "),
		})
	}
	for _, srv := range cfg.MCPServers {
		servers = append(servers, views.MCPItem{
			ID:       srv.ID,
			Name:     srv.Name,
			Type:     "remote",
			Status:   "ready",
			Endpoint: srv.EndpointURL,
		})
	}
	root.Tools.SetMCPServers(servers)
}

func (a *tuiModeApp) saveOnboardingLanguage(lang string) tea.Cmd {
	lang = strings.TrimSpace(lang)
	if lang == "" {
		return nil
	}
	a.root.SetLang(lang)
	return func() tea.Msg {
		if a.app == nil {
			return nil
		}
		if err := a.app.PatchConfig(func(cfg *corelib.AppConfig) {
			cfg.Language = lang
		}); err != nil {
			return views.ConfigSaveFailedMsg{Key: "language", Error: err.Error()}
		}
		return nil
	}
}

func (a *tuiModeApp) ensureHandler() (*IMMessageHandler, error) {
	if a.handler != nil {
		return a.handler, nil
	}
	if a.app == nil {
		return nil, fmt.Errorf(guiTUIAppNotInitializedMsg)
	}
	if a.app.workflowEngine == nil {
		a.app.initWorkflowEngine()
	}
	if a.app.steeringStore == nil {
		a.app.initSteeringStore()
	}
	a.app.ensureInteractionInfra()
	manager := &RemoteSessionManager{
		app:      a.app,
		sessions: make(map[string]*RemoteSession),
	}
	a.handler = NewIMMessageHandler(a.app, manager)
	return a.handler, nil
}

func (a *tuiModeApp) sendMessage(text string) tea.Cmd {
	handler, err := a.ensureHandler()
	if err != nil {
		return func() tea.Msg {
			return views.ChatResponseMsg{Error: err.Error()}
		}
	}
	prog := a.program
	lang := a.root.Lang()

	return func() tea.Msg {
		resp := handler.HandleIMMessageWithProgressAndStream(
			IMUserMessage{
				UserID:   "tui-user",
				Platform: "tui",
				Text:     text,
				Lang:     lang,
			},
			func(progressText string) {
				if prog != nil {
					prog.Send(views.ChatStreamMsg{Type: "progress", Content: progressText})
				}
			},
			func(delta string) {
				if prog != nil {
					prog.Send(views.ChatStreamMsg{Type: "token", Content: delta})
				}
			},
			nil, nil,
		)

		if resp == nil {
			return views.ChatResponseMsg{Error: "no response"}
		}
		if resp.Error != "" {
			return views.ChatResponseMsg{Error: resp.Error}
		}
		return views.ChatResponseMsg{Text: resp.Text}
	}
}
