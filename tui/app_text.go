package main

import (
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/i18n"
)

func tuiConfigLang(cfg corelib.AppConfig) string {
	lang := cfg.Language
	if lang == "" {
		lang = "zh"
	}
	return i18n.NormalizeLang(lang)
}

func (m *tuiModel) uiLang() string {
	if m != nil {
		if lang := m.root.Lang(); lang != "" {
			return i18n.NormalizeLang(lang)
		}
		if m.app != nil {
			return tuiConfigLang(m.app.appConfig)
		}
	}
	return "zh"
}

func tuiText(lang, key string) string {
	if i18n.NormalizeLang(lang) == "en" {
		texts := map[string]string{
			"incompleteRemoteHint":         "Hint: remote setup is incomplete (email exists, but Hub credentials are missing).",
			"incompleteRemoteActivate":     "Open Setup to finish registration: run %s-tui setup, or press F1 in the TUI.",
			"incompleteRemoteSetup":        "Remote setup is incomplete. Open Setup to refresh Hub credentials.",
			"llmNotConfigured":             "LLM is not configured: complete Setup, Service Redeem, or Config first.",
			"llmNotConfiguredChat":         "LLM is not configured yet. I opened the next setup page; finish Setup, Service Redeem, or Config first.",
			"defaultRoleDescription":       "AI coding assistant",
			"restoredHistory":              "Restored %d history messages (/new clears them)",
			"serviceRedeemPrompt":          "Use a service code to enable MaClaw Official LLM.",
			"serviceOpenSetup":             "Open Setup and activate Hub before redeeming service codes.",
			"configOpenSetup":              "Opened Setup. Enter email and activate Hub.",
			"configOpenRedeem":             "Opened Service Redeem. Enter a service code or refresh service status.",
			"configOpenTools":              "Opened MCP templates. Left/Right choose local; Enter opens; A remote.",
			"mcpOptionalReady":             "Chat is ready. Optional: press F3 to add MCP capabilities from templates.",
			"mcpAddedReady":                "%s Chat is ready; press F2 to return.",
			"cancelled":                    "Cancelled",
			"configSaveFailed":             "Config save failed: %s: %s",
			"configSaveFailedPlain":        "config save failed: %s",
			"configLoadFailed":             "config load failed: %s",
			"configLoadWarning":            "Config load failed; started with defaults. Error: %s",
			"configSaved":                  "Saved %s",
			"onboardingCheckService":       "Checking MaClaw official service; enter a service code if it is not active.",
			"onboardingNeedConfig":         "Configure an LLM or complete Hub activation first.",
			"llmKeyMissing":                "LLM provider selected; enter its API key, choose a local provider, or redeem official service.",
			"onboardingComplete":           "Setup complete. You can start using the assistant.",
			"onboardingCompleteMCP":        "Setup complete. Chat is ready; press F3 anytime to add MCP templates.",
			"hubActivationSuccess":         "Hub activation succeeded. Continue to Service Redeem.",
			"hubActivationFailed":          "Hub activation failed: %s",
			"weixinBound":                  "WeChat binding succeeded.",
			"qqbotBound":                   "QQ Bot bound.",
			"qqbotWaitingScan":             "Waiting for QQ scan...",
			"qqbotScannedConfirm":          "Scanned. Confirm on your phone.",
			"qqbotQRExpired":               "QQ QR code expired. Press Enter to get a new one.",
			"qqbotQRFailed":                "QQ binding failed. Press Enter to try again.",
			"qqbotQREmpty":                 "QQ QR response is incomplete. Press Enter to try again.",
			"codeGenSSOSuccess":            "CodeGen SSO login succeeded.",
			"serviceRefreshReadyChat":      "Detected available MaClaw official service. Default LLM has switched.",
			"serviceRedeemSuccessChat":     "Service redeem succeeded. Default LLM has switched to MaClaw Official.",
			"loadConfigFailed":             "load config: %s",
			"saveConfigFailed":             "save config: %s",
			"saveHubCenterFailed":          "save HubCenter: %s",
			"hubURLMissing":                "Hub is not activated. Complete Setup; Hub URL is selected automatically from HubCenter and email.",
			"viewerTokenMissing":           "Hub viewer token is missing. Re-run Setup activation first.",
			"serviceStatusFailed":          "service status check failed: %s",
			"officialServiceMissing":       "No active MaClaw official service was found. Enter a service code.",
			"officialServiceReady":         "MaClaw official service is active. Default LLM has switched.",
			"redeemFailed":                 "redeem failed: %s",
			"redeemNoActiveService":        "service redeemed, but no active LLM service was returned",
			"redeemSuccessStatus":          "Redeem succeeded. Default LLM has switched to MaClaw Official.",
			"activated":                    "Hub activated",
			"regAuthFailed":                "Failed to connect to service. Please retry.",
			"regRequiresEmail":             "This service requires email registration. Please enter an email address.",
			"regRequiresPhone":             "This service requires phone registration. Please enter a phone number.",
			"regInvalidPhone":              "Invalid phone number.",
			"regHubNotResolved":            "Service not available. Please retry.",
			"regSMSSendFailed":             "Failed to send verification code. Please retry.",
			"regActivationFailed":          "Activation failed: %s",
			"weixinBoundShort":             "WeChat bound",
			"weixinWaitingScan":            "Waiting for WeChat scan...",
			"weixinScannedConfirm":         "Scanned. Confirm on your phone.",
			"weixinQRExpired":              "WeChat QR code expired. Press Enter to get a new one.",
			"weixinQRFailed":               "WeChat binding failed. Press Enter to try again.",
			"weixinQREmpty":                "WeChat QR response is incomplete. Press Enter to try again.",
			"ssoQREmpty":                   "SSO QR response is incomplete. Press Enter to try again.",
			"ssoSessionEmpty":              "SSO session is missing. Press Enter to try again.",
			"ssoInputEmpty":                "Paste the returned URL or token first.",
			"ssoWaitingScan":               "Waiting for SSO scan…",
			"chatCleared":                  "Chat cleared.",
			"modelInfoFull":                "Current model: %s\n   Provider: %s\n   Protocol: %s\n   Context: %d tokens",
			"modelInfoBasic":               "Current model: %s\n   Provider: %s",
			"btwUsage":                     "Usage: /btw <quick question>",
			"btwFailed":                    "/btw failed: %s",
			"moaUsage":                     "Usage:\n  /moa <question>              one-shot multi-model (default preset)\n  /moa @preset <question>      one-shot with named preset\n  /moa sticky on [preset]|off|status\n  /moa stats",
			"moaAtPresetUsage":             "Usage: /moa @preset <question>\nExample: /moa @review compare two designs",
			"moaUnavailable":               "Multi-model council unavailable: %s",
			"moaStickyOn":                  "Multi-model council sticky ON for this session. Use /moa sticky off to disable.",
			"moaStickyOnNamed":             "Multi-model council sticky ON (preset=%s). Use /moa sticky off to disable.",
			"moaStickyOff":                 "Multi-model council sticky OFF.",
			"moaStickyUsage":               "Usage: /moa sticky on [preset]|off|status",
			"moaStickyStatus":              "MoA sticky=%v one_shot_pending=%v preset=%s",
			"moaStatsEmpty":                "No MoA fan-outs recorded yet this day.",
			"btwNoInfo":                    "No extra information found.",
			"btwHeader":                    "Side query result:\n\n%s",
			"btwMemoryReadOnly":            "/btw can only use read-only memory actions (recall/themes/scenes/trace/candidates/derived); it cannot write memory.",
			"unknownBtwTool":               "unknown /btw tool: %s",
			"toolArgParseFailed":           "tool argument parse failed: %s",
			"agentStatusSSH":               "SSH sessions:",
			"agentStatusNoActive":          "No active TUI runtime sessions.",
			"skillNoMatch":                 "No matching Skill found.",
			"skillAlreadyInstalled":        "Skill already installed: %s",
			"githubSkillMissingRef":        "GitHub skill install metadata is missing.",
			"skillBuiltInDownloadGuidance": "For generic HTTP/PDF downloads, prefer the built-in download_file or web_fetch(save_path=...).",
			"skillGenericDownloadCaution":  "This is a generic download Skill; prefer the built-in download_file for simple HTTP/PDF downloads.",
			"saveFailed":                   "save failed: %s",
			"installedFrom":                "Installed %s from %s.",
			"added":                        "Added: %s",
			"memoryNotInitialized":         "Memory store is not initialized",
			"memoryEmpty":                  "Memory store is empty",
			"memoryHeader":                 "Memory store (%d entries):\n",
			"memoryMore":                   "  ... %d more\n",
			"memorySummary":                "Memory is active (%d entries). It is maintained automatically in the simplified TUI.",
			"memoryTUISimplified":          "Memory runs in the background in the simplified TUI. There is no separate memory page to browse.",
			"slashOpenSetup":               "Opened Setup. Enter email and choose HubCenter to activate Hub.",
			"slashOpenRedeem":              "Opened Service Redeem. Paste a service code; input is masked.",
			"slashOpenChat":                "Opened Chat. Type a message or /help for commands.",
			"slashOpenTools":               "Opened Tools. Use 1/2 to switch Skill and MCP.",
			"slashOpenMCP":                 "Opened MCP templates. Press Enter to add this preset, or Space to choose another.",
			"slashOpenMCPList":             "Opened MCP list. Press a/A to add local or remote templates.",
			"slashOpenTasks":               "Opened Tasks. Use 1/2/3 to switch task lists.",
			"slashOpenConfig":              "Opened Config. Use Enter/Space to choose values.",
			"slashOpenStatus":              "Opened Status. Enter on setup status jumps to the next useful page.",
			"slashOpenHelp":                "Opened Help. Esc closes; Up/Down or PgUp/PgDn scrolls.",
			"slashHelp": `Available commands:

Chat:
  /new /clear    Clear chat history and start fresh
  /btw <query>   Side query without interrupting the current task context
  /moa [@preset] <q> multi-model one-shot; /moa sticky|stats
  /loop <cmd> <goal>  Goal-driven verification loop (like Claude Code /loop)
                      e.g. /loop "go test ./..." make all tests pass
                      Options: --max N, --timeout N, --dir path
  /goal <objective>  Persistent long-running goal (auto-tracked across turns)
                      e.g. /goal implement user login with JWT auth
                      Sub-commands: status, pause, resume, cancel
Navigation:
  /setup [email] Open first-run Setup, optionally prefilled
  /redeem [code] Open Service Redeem, optionally prefilled
  /chat          Open Chat
  /tools [mcp]   Open Tools; mcp shows the MCP list or template choices when empty
  /mcp [remote]  Open MCP; template choices when empty, remote opens remote templates
  /skill         Open Skill tools
  /tasks [schedule] Open Tasks, optionally Schedule directly
  /schedule      Open scheduled tasks
  /config [llm|security|proxy|im|advanced] Open settings directly
  /status /doctor /health Show setup status and next action
  /status [user-id]  Include sticky canary membership for user-id
  /canary <user-id>  Preview shared-loop canary (IN/OUT · bucket · percent)
  /prompt-export Write adaptive-prompt stats JSON under ~/.maclaw/stats/exports/
  /llm /security Open the matching Config sub-page
Info:
  /model         Show current LLM model info
  /help          Show this help
Shortcuts:
  F1-F6          Jump tabs
  ?              Open help
  Esc            Cancel running request / leave input
  i              Focus input
  c              Clear chat when input is not focused
  Up/Down or jk  Scroll messages
  g/G            Jump top/bottom`,
			"unknownCommand": "Unknown command: %s (type /help to see available commands)",
			"initializing":   "Initializing...",
		}
		if text, ok := texts[key]; ok {
			return text
		}
	}
	texts := map[string]string{
		"incompleteRemoteHint":         "提示: 远程初始化不完整（已有邮箱，但缺少 Hub 凭据）。",
		"incompleteRemoteActivate":     "请打开初始化完成注册：运行 %s-tui setup，或在 TUI 中按 F1。",
		"incompleteRemoteSetup":        "远程初始化未完成，请在初始化页刷新 Hub 凭据。",
		"llmNotConfigured":             "LLM 未配置：请先在 初始化/服务兑换/设置 中完成配置",
		"llmNotConfiguredChat":         "LLM 还未配置。已为你打开下一步页面，请先完成 初始化、服务兑换 或 设置。",
		"defaultRoleDescription":       "AI 编程助手",
		"restoredHistory":              "已恢复 %d 条历史消息（/new 清除）",
		"serviceRedeemPrompt":          "请使用服务兑换码启用 MaClaw 官方 LLM",
		"serviceOpenSetup":             "请先打开初始化并激活 Hub，然后再兑换服务码。",
		"configOpenSetup":              "已打开初始化。请输入邮箱并激活 Hub。",
		"configOpenRedeem":             "已打开服务兑换。请输入服务兑换码，或刷新服务状态。",
		"configOpenTools":              "已打开 MCP 模板。左右键选择本地，Enter 打开，A 打开远程。",
		"mcpOptionalReady":             "聊天已可用。可选：按 F3 从模板添加 MCP 能力。",
		"mcpAddedReady":                "%s 聊天已可用；按 F2 返回。",
		"cancelled":                    "已取消",
		"configSaveFailed":             "配置保存失败: %s: %s",
		"configSaveFailedPlain":        "配置保存失败: %s",
		"configLoadFailed":             "配置读取失败: %s",
		"configLoadWarning":            "配置加载失败，已使用默认值启动。错误: %s",
		"configSaved":                  "已保存 %s",
		"onboardingCheckService":       "正在检查 MaClaw 官方服务；如未开通请输入服务兑换码",
		"onboardingNeedConfig":         "请先配置 LLM 或完成 Hub 激活",
		"llmKeyMissing":                "已选择 LLM 服务商；请填写密钥、切换本地服务商，或兑换官方服务。",
		"onboardingComplete":           "初始化完成，可以开始使用",
		"onboardingCompleteMCP":        "初始化完成，聊天已可用；随时可按 F3 添加 MCP 模板。",
		"hubActivationSuccess":         "Hub 激活成功，可以继续服务兑换",
		"hubActivationFailed":          "Hub 激活失败: %s",
		"weixinBound":                  "微信绑定成功",
		"qqbotBound":                   "QQ 机器人已绑定",
		"qqbotWaitingScan":             "等待 QQ 扫码...",
		"qqbotScannedConfirm":          "已扫码，请在手机上确认。",
		"qqbotQRExpired":               "QQ 二维码已过期，请按 Enter 重新获取。",
		"qqbotQRFailed":                "QQ 绑定失败，请按 Enter 重试。",
		"qqbotQREmpty":                 "QQ 二维码响应不完整，请按 Enter 重试。",
		"codeGenSSOSuccess":            "CodeGen SSO 登录成功",
		"serviceRefreshReadyChat":      "检测到可用的 MaClaw 官方服务，默认 LLM 已切换。",
		"serviceRedeemSuccessChat":     "服务兑换成功，默认 LLM 已切换到 MaClaw 官方。",
		"loadConfigFailed":             "读取配置失败: %s",
		"saveConfigFailed":             "保存配置失败: %s",
		"saveHubCenterFailed":          "保存 HubCenter 失败: %s",
		"hubURLMissing":                "Hub 未激活，请先完成初始化；Hub URL 会根据 HubCenter 和邮箱自动选择。",
		"viewerTokenMissing":           "缺少 Hub viewer token，请重新执行初始化激活。",
		"serviceStatusFailed":          "服务状态检查失败: %s",
		"officialServiceMissing":       "未发现可用的 MaClaw 官方服务，请输入服务兑换码",
		"officialServiceReady":         "MaClaw 官方服务已生效，默认 LLM 已切换",
		"redeemFailed":                 "兑换失败: %s",
		"redeemNoActiveService":        "服务已兑换，但未返回可用的 LLM 服务",
		"redeemSuccessStatus":          "兑换成功，默认 LLM 已切换到 MaClaw 官方",
		"activated":                    "Hub 已激活",
		"regAuthFailed":                "连接服务失败，请重试。",
		"regRequiresEmail":             "当前服务要求邮箱注册，请输入邮箱地址。",
		"regRequiresPhone":             "当前服务要求手机号注册，请输入手机号。",
		"regInvalidPhone":              "手机号格式无效。",
		"regHubNotResolved":            "服务暂不可用，请重试。",
		"regSMSSendFailed":             "发送验证码失败，请重试。",
		"regActivationFailed":          "激活失败: %s",
		"weixinBoundShort":             "微信已绑定",
		"weixinWaitingScan":            "等待微信扫码...",
		"weixinScannedConfirm":         "已扫码，请在手机上确认。",
		"weixinQRExpired":              "微信二维码已过期，请按 Enter 重新获取。",
		"weixinQRFailed":               "微信绑定失败，请按 Enter 重试。",
		"weixinQREmpty":                "微信二维码响应不完整，请按 Enter 重试。",
		"ssoQREmpty":                   "SSO 二维码响应不完整，请按 Enter 重试。",
		"ssoSessionEmpty":              "SSO 会话缺失，请按 Enter 重试。",
		"ssoInputEmpty":                "请先粘贴返回 URL 或 token。",
		"ssoWaitingScan":               "等待 SSO 扫码…",
		"chatCleared":                  "对话已清除",
		"modelInfoFull":                "当前模型: %s\n   服务商: %s\n   协议: %s\n   上下文: %d tokens",
		"modelInfoBasic":               "当前模型: %s\n   服务商: %s",
		"btwUsage":                     "用法: /btw <快速问题>",
		"btwFailed":                    "/btw 查询失败: %s",
		"moaUsage":                     "用法:\n  /moa <问题>                 单次多模型会诊（默认方案）\n  /moa @方案名 <问题>         指定方案单次会诊\n  /moa sticky on [方案]|off|status\n  /moa stats",
		"moaAtPresetUsage":             "用法: /moa @方案名 <问题>\n示例: /moa @review 对比两种设计",
		"moaUnavailable":               "多模型会诊不可用: %s",
		"moaStickyOn":                  "已开启本会话多模型会诊。使用 /moa sticky off 关闭。",
		"moaStickyOnNamed":             "已开启本会话多模型会诊（预设=%s）。使用 /moa sticky off 关闭。",
		"moaStickyOff":                 "已关闭本会话多模型会诊。",
		"moaStickyUsage":               "用法: /moa sticky on [预设名]|off|status",
		"moaStickyStatus":              "多模型会诊 sticky=%v 待单次=%v 预设=%s",
		"moaStatsEmpty":                "今日尚未记录多模型会诊 fan-out。",
		"btwNoInfo":                    "没有找到额外信息。",
		"btwHeader":                    "侧查询结果:\n\n%s",
		"btwMemoryReadOnly":            "/btw 只能使用只读记忆操作（recall/themes/scenes/trace/candidates/derived），不能写入记忆。",
		"unknownBtwTool":               "未知 /btw 工具: %s",
		"toolArgParseFailed":           "工具参数解析失败: %s",
		"agentStatusSSH":               "SSH 会话:",
		"agentStatusNoActive":          "当前没有活跃的 TUI 运行时会话。",
		"skillNoMatch":                 "未找到匹配的 Skill。",
		"skillAlreadyInstalled":        "Skill 已安装: %s",
		"githubSkillMissingRef":        "缺少 GitHub Skill 安装元数据。",
		"skillBuiltInDownloadGuidance": "通用 HTTP/PDF 下载优先使用内置 download_file 或 web_fetch(save_path=...)。",
		"skillGenericDownloadCaution":  "提示：这是通用下载 Skill；简单 HTTP/PDF 下载优先使用内置 download_file。",
		"saveFailed":                   "保存失败: %s",
		"installedFrom":                "已从 %[2]s 安装 %[1]s。",
		"added":                        "已添加: %s",
		"memoryNotInitialized":         "记忆存储未初始化",
		"memoryEmpty":                  "记忆库为空",
		"memoryHeader":                 "记忆库（共 %d 条）:\n",
		"memoryMore":                   "  ... 还有 %d 条\n",
		"memorySummary":                "记忆已启用（共 %d 条），简化 TUI 会在后台自动维护。",
		"memoryTUISimplified":          "简化 TUI 中记忆会在后台自动维护，不再提供单独的记忆浏览页。",
		"slashOpenSetup":               "已打开初始化。输入邮箱并选择 HubCenter 来激活 Hub。",
		"slashOpenRedeem":              "已打开服务兑换。粘贴服务兑换码；输入会被掩码。",
		"slashOpenChat":                "已打开聊天。输入消息，或输入 /help 查看命令。",
		"slashOpenTools":               "已打开工具。使用 1/2 切换 Skill 与 MCP。",
		"slashOpenMCP":                 "已打开 MCP 模板。按 Enter 添加当前预设，或按 Space 切换其它预设。",
		"slashOpenMCPList":             "已打开 MCP 列表。按 a/A 可添加本地或远程模板。",
		"slashOpenTasks":               "已打开任务。使用 1/2/3 切换任务列表。",
		"slashOpenConfig":              "已打开设置。使用 Enter/Space 选择配置值。",
		"slashOpenStatus":              "已打开状态总览。可在初始化状态行按 Enter 跳到下一步。",
		"slashOpenHelp":                "已打开帮助。Esc 关闭，↑↓ 或 PgUp/PgDn 滚动。",
		"unknownCommand":               "未知命令: %s（输入 /help 查看可用命令）",
		"initializing":                 "正在初始化...",
	}
	texts["slashHelp"] = `可用命令:

对话管理:
  /new /clear    清除对话历史，开始新对话
  /btw <查询>    侧查询（不打断当前任务上下文）
  /moa [@方案] <问题> 多模型会诊；/moa sticky|stats
  /loop <命令> <目标> 目标驱动验证循环；选项: --max 轮数, --timeout 秒, --dir 路径
  /goal <目标描述> 持久化长时间自主目标（跨轮次自动跟踪）
                    例: /goal 实现用户登录功能，包含JWT认证
                    子命令: status, pause, resume, cancel
页面跳转:
  /setup [邮箱]  打开首次设置，可自动填入
  /redeem [兑换码] 打开服务兑换，可自动填入
  /chat          打开聊天
  /tools [mcp]   打开工具；mcp 显示 MCP 列表，未配置时显示模板选择
  /mcp [remote]  打开 MCP；未配置时显示模板选择，remote 打开远程模板
  /skill         打开 Skill 工具
  /tasks [schedule] 打开任务，可直接进入定时任务
  /schedule      打开定时任务
  /config [llm|security|proxy|im|advanced] 直达设置子页
  /status /doctor /health 显示初始化状态和下一步动作
  /status [user-id]  附加该用户 sticky canary 归属预览
  /canary <user-id>  预览 shared-loop canary（IN/OUT · bucket · percent）
  /prompt-export 导出 adaptive-prompt 统计 JSON 到 ~/.maclaw/stats/exports/
  /llm /security 打开对应设置子页
信息查看:
  /model         显示当前 LLM 模型信息
  /help          显示此帮助
快捷键:
  F1-F6          直接跳转标签
  ?              打开帮助
  Esc            取消正在执行的请求 / 退出输入框
  i              聚焦输入框
  c              清除对话（非输入状态）
  ↑↓/jk          滚动消息
  g/G            跳到顶部/底部`
	if text, ok := texts[key]; ok {
		return text
	}
	return key
}

func tuiFormat(lang, key string, args ...interface{}) string {
	return fmt.Sprintf(tuiText(lang, key), args...)
}

func tuiProviderDisplayName(lang, name string) string {
	if name == tuiHubServiceProviderName {
		if i18n.NormalizeLang(lang) == "en" {
			return "MaClaw Official"
		}
		return "MaClaw 官方"
	}
	return name
}

func tuiModelDisplayLabel(lang, provider, model string) string {
	if provider == "" {
		return model
	}
	if model == "" {
		return tuiProviderDisplayName(lang, provider)
	}
	return tuiProviderDisplayName(lang, provider) + " / " + model
}
