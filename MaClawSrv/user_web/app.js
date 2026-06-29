const $ = (id) => document.getElementById(id);
const I18N = {
  en: {
    userWorkspace: "User Workspace", assistantNav: "AI Assistant", settingsNav: "System Settings", skipToMain: "Skip to main content", appSections: "User app sections", userViews: "User views", notSignedIn: "Not signed in", logout: "Log Out", ready: "Ready", busy: "Busy",
    loginRequired: "Login required", loginHint: "Open this page from VE Platform MaClawSrv user entry.", cannotStart: "Cannot start user app", missingToken: "Missing short-lived access token.", rawSecretRejected: "Raw secrets in URLs are not accepted. Open this page again from VE Platform.", sessionExpired: "Session expired. Open this page again from VE Platform.", loadFailed: "Load failed", retry: "Retry",
    assistantTitle: "AI Assistant", assistantHint: "Instances share user-level config, tools, knowledge, memory, and security policy.", instancesTitle: "Assistant instances", instancesHint: "Runtime state and sessions stay per instance. Configuration stays shared by user.", new: "New", noInstances: "No instances", unknown: "unknown", readyState: "ready", notReady: "not ready", instanceName: "Instance name", instanceCreated: "Instance created",
    sessions: "Sessions", noSessions: "No sessions", firstMessage: "Send the first message to create a session.", selectedMissing: "Selected assistant instance was not found or is unavailable. Open it again from VE Platform or select another instance.", createFirst: "No instance yet. Create an assistant instance first.", noMessages: "No messages", typeMessage: "Type a message...", message: "Message", send: "Send", webSession: "Web session", run: "Run", waitingUser: "waiting for user", continueWaiting: "Continue waiting", cancel: "Cancel", confirm: "Confirm", runCancelled: "Run cancelled", sent: "Sent", runStatus: "Run status: {status}", llmManagedByHub: "LLM is not fully configured. Ask VE Platform to pass the Hub LLM endpoint and viewer token, or fill in System Settings.",
    settingsTitle: "System Settings", settingsHint: "User-scoped settings shared by all assistant instances.", sharedConfig: "Shared config", sharedConfigHint: "LLM, MCP, tools, skills, knowledge, and security policy are shared at user scope.", configResponse: "Config response", secretHint: "Masked value keeps the existing secret. Enter a new value only when rotating it.", valid: "Valid", needsAttention: "Needs attention", currentConfigOk: "Current shared config can start instances.", save: "Save", validate: "Validate", test: "Test", saved: "Saved", validated: "Validated", testPassed: "Test passed", testFailed: "Test failed", unset: "Unset", trueValue: "True", falseValue: "False",
    groupLLM: "LLM", groupLLMHint: "Primary model providers and legacy fallback fields.", groupTools: "MCP & Tools", groupToolsHint: "MCP capability install, compact add, and search providers shared by every instance.", groupSkills: "Skills", groupSkillsHint: "Search, install, and view skills. Source details stay managed by the service.", installedSkills: "Installed skills", noSkills: "No skills installed", skillMarketSearch: "SkillMarket search", search: "Search", install: "Install", installed: "Installed", searchSkillsPlaceholder: "Search SkillMarket...", skillInstalled: "Skill installed", previous: "Previous", nextPage: "Next", pageStatus: "Page {page} / {pages}", groupMemory: "Knowledge & Memory", groupMemoryHint: "Memory compression and knowledge context budget.", groupSecurity: "Security", groupSecurityHint: "User-level execution boundary and network policy.", groupIM: "IM", groupIMHint: "User-scoped QQ, WeChat, Telegram, third-party integration, monitor, and history.", numberInvalid: "{key} must be a valid {type}", jsonInvalid: "{key} must be valid JSON"
  },
  zh: {
    userWorkspace: "用户工作台", assistantNav: "AI 助手", settingsNav: "系统设置", skipToMain: "跳到主要内容", appSections: "用户应用区域", userViews: "用户视图", notSignedIn: "未登录", logout: "退出", ready: "就绪", busy: "忙碌",
    loginRequired: "需要登录", loginHint: "请从 VE Platform 的 MaClawSrv 用户入口打开本页。", cannotStart: "无法启动用户应用", missingToken: "缺少短期访问令牌。", rawSecretRejected: "URL 中不接受原始密钥。请从 VE Platform 重新打开本页。", sessionExpired: "会话已过期，请从 VE Platform 重新打开本页。", loadFailed: "加载失败", retry: "重试",
    assistantTitle: "AI 助手", assistantHint: "多个实例共享用户级配置、工具、知识、记忆和安全策略。", instancesTitle: "助手实例", instancesHint: "运行状态和会话按实例保留，配置按用户共享。", new: "新建", noInstances: "暂无实例", unknown: "未知", readyState: "就绪", notReady: "未就绪", instanceName: "实例名称", instanceCreated: "实例已创建",
    sessions: "会话", noSessions: "暂无会话", firstMessage: "发送第一条消息后会自动创建会话。", selectedMissing: "选中的助手实例不存在或不可用。请从 VE Platform 重新打开，或选择其它实例。", createFirst: "还没有实例，请先创建助手实例。", noMessages: "暂无消息", typeMessage: "输入消息...", message: "消息", send: "发送", webSession: "网页会话", run: "运行", waitingUser: "等待用户", continueWaiting: "继续等待", cancel: "取消", confirm: "确认", runCancelled: "运行已取消", sent: "已发送", runStatus: "运行状态：{status}", llmManagedByHub: "LLM 未完成配置。请让 VE Platform 传入 Hub LLM 地址和 viewer token，或在系统设置里填写可用配置。",
    settingsTitle: "系统设置", settingsHint: "这些用户级设置会被所有助手实例共享。", sharedConfig: "共享配置", sharedConfigHint: "LLM、MCP、工具、技能、知识和安全策略按用户范围共享。", configResponse: "配置响应", secretHint: "显示为掩码时会保留现有密钥；只有需要轮换时才输入新值。", valid: "有效", needsAttention: "需要处理", currentConfigOk: "当前共享配置可以启动实例。", save: "保存", validate: "校验", test: "测试", saved: "已保存", validated: "已校验", testPassed: "测试通过", testFailed: "测试失败", unset: "未设置", trueValue: "是", falseValue: "否",
    groupLLM: "LLM", groupLLMHint: "主模型服务商和旧版兜底字段。", groupTools: "MCP 与工具", groupToolsHint: "所有实例共享的远程/本地工具服务器和搜索服务商。", groupSkills: "技能", groupSkillsHint: "已安装技能、技能中心、外部目录和来源白名单。", groupMemory: "知识与记忆", groupMemoryHint: "记忆压缩和知识上下文预算。", groupSecurity: "安全", groupSecurityHint: "用户级执行边界和网络策略。", numberInvalid: "{key} 必须是有效的{type}", jsonInvalid: "{key} 必须是有效 JSON"
  }
};
const params = new URLSearchParams(location.search);
Object.assign(I18N.en, {
  groupInterface: "User Interface",
  groupInterfaceHint: "Preferred display language.",
  groupProxy: "Proxy",
  groupProxyHint: "Default proxy endpoint, credentials, bypass list, and proxy scopes."
});
Object.assign(I18N.zh, {
  groupInterface: "\u7528\u6237\u754c\u9762",
  groupInterfaceHint: "\u9996\u9009\u754c\u9762\u8bed\u8a00\u3002",
  groupProxy: "\u4ee3\u7406",
  groupProxyHint: "\u9ed8\u8ba4\u4ee3\u7406\u5730\u5740\u3001\u51ed\u636e\u3001\u8df3\u8fc7\u5217\u8868\u548c\u4ee3\u7406\u8303\u56f4\u3002"
});
Object.assign(I18N.en, {
  groupMigration: "Move Out & In",
  groupMigrationHint: "Move this user's memory and local knowledge base through the current Hub tenant.",
  migrationTitle: "Move Out & In",
  migrationHint: "The Hub only stores one encrypted relay package for this tenant user.",
  migrationNotReady: "Hub login and machine registration are required.",
  migrationCurrentHub: "Hub",
  migrationCurrentTenant: "Tenant",
  migrationCurrentMachine: "Current machine",
  migrationLimit: "Package limit",
  migrationNoExport: "No export package on Hub.",
  migrationHasExport: "Existing export package",
  migrationOverwriteWarning: "Only one export is kept. Creating a new export overwrites the previous one, even if the password is different.",
  migrationExportPassword: "Export password",
  migrationExportPasswordConfirm: "Confirm password",
  migrationStartExport: "Move out",
  migrationRefresh: "Refresh",
  migrationImportSource: "Import source",
  migrationSelectSource: "Select a machine",
  migrationImportPassword: "Import password",
  migrationStartImport: "Move in",
  migrationPasswordMismatch: "Passwords do not match.",
  migrationPasswordRequired: "Password is required.",
  migrationExportStarted: "Export started.",
  migrationImportStarted: "Import started.",
  migrationCompleted: "Migration completed.",
  migrationFailed: "Migration failed.",
  migrationLoading: "Loading migration state...",
  migrationProgress: "Progress",
  migrationReadyOnly: "Ready packages can be imported. Packages already claimed by this machine can be resumed or cleaned up.",
  migrationMachineName: "Machine"
});
Object.assign(I18N.zh, {
  groupMigration: "\u8fc1\u51fa\u4e0e\u8fc1\u5165",
  groupMigrationHint: "\u901a\u8fc7\u5f53\u524d\u767b\u5f55\u7684 Hub \u79df\u6237\u642c\u8fc1\u6b64\u7528\u6237\u7684\u8bb0\u5fc6\u548c\u672c\u5730\u77e5\u8bc6\u5e93\u3002",
  migrationTitle: "\u8fc1\u51fa\u4e0e\u8fc1\u5165",
  migrationHint: "Hub \u53ea\u4fdd\u7559\u6b64\u79df\u6237\u7528\u6237\u7684\u4e00\u4efd\u52a0\u5bc6\u4e2d\u8f6c\u5305\u3002",
  migrationNotReady: "\u9700\u8981\u5b8c\u6210 Hub \u767b\u5f55\u548c\u672c\u673a\u6ce8\u518c\u3002",
  migrationCurrentHub: "Hub",
  migrationCurrentTenant: "\u79df\u6237",
  migrationCurrentMachine: "\u5f53\u524d\u673a\u5668",
  migrationLimit: "\u8fc1\u79fb\u5305\u4e0a\u9650",
  migrationNoExport: "Hub \u4e0a\u6682\u65e0\u8fc1\u51fa\u5305\u3002",
  migrationHasExport: "\u5df2\u6709\u8fc1\u51fa\u5305",
  migrationOverwriteWarning: "\u53ea\u4fdd\u7559\u4e00\u4efd\u8fc1\u51fa\u6570\u636e\u3002\u65b0\u8fc1\u51fa\u4f1a\u8986\u76d6\u65e7\u6570\u636e\uff0c\u5373\u4f7f\u5bc6\u7801\u4e0d\u540c\u4e5f\u4e00\u6837\u3002",
  migrationExportPassword: "\u8fc1\u51fa\u5bc6\u7801",
  migrationExportPasswordConfirm: "\u786e\u8ba4\u5bc6\u7801",
  migrationStartExport: "\u6267\u884c\u8fc1\u51fa",
  migrationRefresh: "\u5237\u65b0",
  migrationImportSource: "\u8fc1\u5165\u6765\u6e90",
  migrationSelectSource: "\u9009\u62e9\u673a\u5668",
  migrationImportPassword: "\u8fc1\u5165\u5bc6\u7801",
  migrationStartImport: "\u6267\u884c\u8fc1\u5165",
  migrationPasswordMismatch: "\u4e24\u6b21\u5bc6\u7801\u4e0d\u4e00\u81f4\u3002",
  migrationPasswordRequired: "\u9700\u8981\u8f93\u5165\u5bc6\u7801\u3002",
  migrationExportStarted: "\u8fc1\u51fa\u5df2\u5f00\u59cb\u3002",
  migrationImportStarted: "\u8fc1\u5165\u5df2\u5f00\u59cb\u3002",
  migrationCompleted: "\u8fc1\u79fb\u5b8c\u6210\u3002",
  migrationFailed: "\u8fc1\u79fb\u5931\u8d25\u3002",
  migrationLoading: "\u6b63\u5728\u52a0\u8f7d\u8fc1\u79fb\u72b6\u6001...",
  migrationProgress: "\u8fdb\u5ea6",
  migrationReadyOnly: "\u53ef\u8fc1\u5165\u5df2\u5c31\u7eea\u7684\u8fc1\u51fa\u5305\uff0c\u4e5f\u53ef\u7ee7\u7eed\u6216\u6e05\u7406\u7531\u5f53\u524d\u673a\u5668\u8ba4\u9886\u7684\u8fc1\u79fb\u5305\u3002",
  migrationMachineName: "\u673a\u5668"
});
Object.assign(I18N.zh, {
  userWorkspace: "用户工作台", assistantNav: "AI 助手", settingsNav: "系统设置", skipToMain: "跳到主要内容", appSections: "用户应用区域", userViews: "用户视图", notSignedIn: "未登录", logout: "退出", ready: "就绪", busy: "忙碌",
  loginRequired: "需要登录", loginHint: "请从 VE Platform 的 MaClawSrv 用户入口打开本页。", cannotStart: "无法启动用户应用", missingToken: "缺少短期访问令牌。", rawSecretRejected: "URL 中不接受原始密钥。请从 VE Platform 重新打开本页。", sessionExpired: "会话已过期，请从 VE Platform 重新打开本页。", loadFailed: "加载失败", retry: "重试",
  assistantTitle: "AI 助手", assistantHint: "多个实例共享用户级配置、工具、知识、记忆和安全策略。", instancesTitle: "助手实例", instancesHint: "运行状态和会话按实例保留，配置按用户共享。", new: "新建", noInstances: "暂无实例", unknown: "未知", readyState: "就绪", notReady: "未就绪", instanceName: "实例名称", instanceCreated: "实例已创建",
  sessions: "会话", noSessions: "暂无会话", firstMessage: "发送第一条消息后会自动创建会话。", selectedMissing: "选中的助手实例不存在或不可用。请从 VE Platform 重新打开，或选择其它实例。", createFirst: "还没有实例，请先创建助手实例。", noMessages: "暂无消息", typeMessage: "输入消息...", message: "消息", send: "发送", webSession: "网页会话", run: "运行", waitingUser: "等待用户", continueWaiting: "继续等待", cancel: "取消", runCancelled: "运行已取消", sent: "已发送", runStatus: "运行状态：{status}", llmManagedByHub: "LLM 未完成配置。请让 VE Platform 传入 Hub LLM 地址和 viewer token，或在系统设置里填写可用配置。",
  settingsTitle: "系统设置", settingsHint: "这些用户级设置会被所有助手实例共享。", sharedConfig: "共享配置", sharedConfigHint: "LLM、MCP、工具、技能、知识和安全策略按用户范围共享。", configResponse: "配置响应", secretHint: "显示为掩码时会保留现有密钥；只有需要轮换时才输入新值。", valid: "有效", needsAttention: "需要处理", currentConfigOk: "当前共享配置可以启动实例。", save: "保存", validate: "校验", test: "测试", saved: "已保存", validated: "已校验", testPassed: "测试通过", testFailed: "测试失败", unset: "未设置", trueValue: "是", falseValue: "否",
  groupLLM: "LLM", groupLLMHint: "主模型服务商和旧版兜底字段。", groupTools: "MCP 与工具", groupToolsHint: "安装 MCP 能力、精简添加 MCP，并管理所有实例共享的搜索服务商。", groupSkills: "技能", groupSkillsHint: "搜索、安装、查看技能；来源细节由服务端管理。", groupMemory: "知识与记忆", groupMemoryHint: "记忆压缩和知识上下文预算。", groupSecurity: "安全", groupSecurityHint: "用户级执行边界和网络策略。", groupIM: "IM", groupIMHint: "当前用户隔离的 QQ、微信、Telegram、第三方接入、监看和历史交流。", numberInvalid: "{key} 必须是有效的{type}", jsonInvalid: "{key} 必须是有效 JSON"
});
Object.assign(I18N.zh, {
  installedSkills: "\u5df2\u5b89\u88c5\u6280\u80fd",
  noSkills: "\u6682\u65e0\u5df2\u5b89\u88c5\u6280\u80fd",
  skillMarketSearch: "SkillMarket \u641c\u7d22",
  search: "\u641c\u7d22",
  install: "\u5b89\u88c5",
  installed: "\u5df2\u5b89\u88c5",
  searchSkillsPlaceholder: "\u641c\u7d22 SkillMarket...",
  skillInstalled: "\u6280\u80fd\u5df2\u5b89\u88c5",
  previous: "\u4e0a\u4e00\u9875",
  nextPage: "\u4e0b\u4e00\u9875",
  pageStatus: "\u7b2c {page} / {pages} \u9875"
});
Object.assign(I18N.en, {
  channelOverview: "IM",
  channelOverviewHint: "Configure this user's QQ, WeChat, Telegram, Lansenger, and third-party IM access.",
  channelCredentialHint: "Secrets are masked after saving. Leave masked values unchanged unless rotating credentials.",
  channelQQ: "QQ Bot",
  channelTelegram: "Telegram Bot",
  channelWeixin: "Personal WeChat / iLink",
  channelLansenger: "Lansenger",
  channelThirdParty: "MaClaw Third-party Integration Protocol",
  channelProtocolEndpoint: "Protocol endpoint",
  channelCopyEndpoint: "Copy endpoint",
  channelGenerateToken: "Generate token",
  showSecret: "Show",
  hideSecret: "Hide",
  channelTokenGenerated: "Token generated. Save settings to apply it.",
  weixinQRBind: "Scan to bind",
  weixinQRHint: "Use WeChat to scan this QR code. Credentials are saved to this MaClawSrv user after confirmation.",
  weixinQRLoading: "Generating QR code...",
  weixinQRWaiting: "Waiting for scan...",
  weixinQRScanned: "Scanned. Confirm on phone.",
  weixinQRConfirmed: "WeChat bound.",
  weixinQRExpired: "QR code expired. Generate a new one.",
  weixinQRError: "WeChat QR login failed.",
  weixinBoundAccount: "Bound account",
  weixinRuntimeStatus: "Runtime status",
  weixinRuntimeStarting: "Starting",
  weixinRuntimeConnected: "Connected",
  weixinRuntimeDisconnected: "Disconnected",
  weixinRuntimeError: "Runtime error",
  weixinRebind: "Rebind",
  weixinNotBound: "No WeChat account bound",
  generateSecret: "Generate secret",
  channelTokenUnavailable: "Browser crypto API is unavailable.",
  channelEnabled: "Enabled",
  channelDisabled: "Disabled",
  channelAuto: "Auto",
  channelRunning: "Enabled",
  channelReady: "Configured",
  channelIncomplete: "Needs config",
  channelSaveStart: "Save and start",
  channelDisconnect: "Disconnect",
  channelRestart: "Restart channel",
  channelSavedStarted: "Saved. Channel is enabled.",
  channelDisconnected: "Disconnected.",
  imChannelTabQQ: "QQ Bot",
  imChannelTabTelegram: "Telegram Bot",
  imChannelTabWeixin: "WeChat",
  imChannelTabLansenger: "Lansenger",
  imChannelTabThirdParty: "Third-party Access",
  imWatchHistory: "Watch history",
  imQQDescription: "Configure this user's own QQ Bot credentials and start the QQ channel.",
  imTelegramDescription: "Configure this user's Telegram Bot token and start the Telegram channel.",
  imWeixinDescription: "Scan to bind personal WeChat. After confirmation, MaClawSrv saves credentials and enables the channel.",
  imLansengerDescription: "Configure this user's Lansenger app credentials and start the Lansenger channel.",
  imThirdPartyDescription: "Expose this user's HTTP message gateway for third-party software.",
  imMissingRequired: "Complete required fields before starting.",
  imWeixinStartScan: "Scan WeChat first, then startup is saved after confirmation.",
  imCancelQR: "Cancel QR login",
  imGetAppID: "Get AppID",
  imTutorial: "Tutorial",
  imOpenDocs: "Open docs",
  imProgressHints: "Show progress hints",
  imProgressHintsDesc: "When off, IM only shows the first progress note and the final result.",
  customValue: "Custom",
  selectAll: "Select all",
  clearSelection: "Clear"
});
Object.assign(I18N.en, {
  imAuditTitle: "IM monitor and history",
  imAuditHint: "View historical conversations for this MaClawSrv user only.",
  imAuditPlatformAll: "All platforms",
  imAuditKeyword: "Keyword",
  imAuditContact: "Contact",
  imAuditRefresh: "Refresh",
  imAuditExport: "Export CSV",
  imAuditCleanup: "Clean",
  imAuditCleanupBefore: "Clean before",
  imAuditCleanupDays: "days",
  imAuditCleanupConfirm: "Delete IM history before {before}?",
  imAuditCleaned: "Deleted {deleted} IM messages",
  imAuditEmpty: "No IM history found",
  imAuditLoadOlder: "Load older",
  imAuditOpenSession: "Session",
  imAuditOpen: "Open",
  imAuditStats: "{messages} messages / {contacts} contacts / {platforms} platforms",
  imAuditLoading: "Loading IM history..."
});
Object.assign(I18N.en, {
  groupIM: "IM",
  groupIMHint: "User-scoped QQ, WeChat, Telegram, Lansenger, third-party integration, monitor, and history.",
  channelOverview: "IM",
  channelOverviewHint: "Configure this user's QQ, WeChat, Telegram, Lansenger, and third-party IM access."
});
Object.assign(I18N.zh, {
  groupIM: "\u0049\u004d",
  groupIMHint: "\u5f53\u524d\u7528\u6237\u9694\u79bb\u7684 QQ\u3001\u5fae\u4fe1\u3001Telegram\u3001\u84dd\u4fe1\u3001\u7b2c\u4e09\u65b9\u63a5\u5165\u3001\u76d1\u770b\u548c\u5386\u53f2\u4ea4\u6d41\u3002",
  channelOverview: "\u0049\u004d",
  channelOverviewHint: "\u914d\u7f6e\u5f53\u524d\u7528\u6237\u7684 QQ\u3001\u5fae\u4fe1\u3001Telegram\u3001\u84dd\u4fe1\u548c\u7b2c\u4e09\u65b9 IM \u63a5\u5165\u3002"
});
Object.assign(I18N.zh, {
  imAuditTitle: "\u0049\u004d \u76d1\u770b\u4e0e\u5386\u53f2\u4ea4\u6d41",
  imAuditHint: "\u4ec5\u67e5\u770b\u5f53\u524d MaClawSrv \u7528\u6237\u9694\u79bb\u8303\u56f4\u5185\u7684\u5386\u53f2\u4ea4\u6d41\u3002",
  imAuditPlatformAll: "\u5168\u90e8\u5e73\u53f0",
  imAuditKeyword: "\u5173\u952e\u8bcd",
  imAuditContact: "\u8054\u7cfb\u4eba",
  imAuditRefresh: "\u5237\u65b0",
  imAuditExport: "\u5bfc\u51fa CSV",
  imAuditCleanup: "\u6e05\u7406",
  imAuditCleanupBefore: "\u6e05\u7406\u65e9\u4e8e",
  imAuditCleanupDays: "\u5929",
  imAuditCleanupConfirm: "\u5220\u9664 {before} \u4e4b\u524d\u7684 IM \u5386\u53f2\uff1f",
  imAuditCleaned: "\u5df2\u5220\u9664 {deleted} \u6761 IM \u6d88\u606f",
  imAuditEmpty: "\u6682\u65e0 IM \u5386\u53f2",
  imAuditLoadOlder: "\u52a0\u8f7d\u66f4\u65e9",
  imAuditOpenSession: "\u4f1a\u8bdd",
  imAuditOpen: "\u6253\u5f00",
  imAuditStats: "{messages} \u6761\u6d88\u606f / {contacts} \u4e2a\u8054\u7cfb\u4eba / {platforms} \u4e2a\u5e73\u53f0",
  imAuditLoading: "\u6b63\u5728\u52a0\u8f7d IM \u5386\u53f2..."
});
Object.assign(I18N.zh, {
  channelOverview: "\u0049\u004d",
  channelOverviewHint: "\u914d\u7f6e\u5f53\u524d\u7528\u6237\u7684 QQ\u3001\u5fae\u4fe1\u3001Telegram\u3001\u84dd\u4fe1\u548c\u7b2c\u4e09\u65b9 IM \u63a5\u5165\u3002",
  channelCredentialHint: "\u5bc6\u94a5\u4fdd\u5b58\u540e\u4f1a\u8131\u654f\u663e\u793a\u3002\u663e\u793a\u4e3a\u63a9\u7801\u65f6\u8bf7\u4fdd\u6301\u4e0d\u53d8\uff0c\u53ea\u6709\u8f6e\u6362\u51ed\u636e\u65f6\u624d\u91cd\u65b0\u586b\u5199\u3002",
  channelQQ: "QQ Bot",
  channelTelegram: "Telegram Bot",
  channelWeixin: "\u4e2a\u4eba\u5fae\u4fe1 / iLink",
  channelLansenger: "\u84dd\u4fe1",
  channelThirdParty: "Maclaw \u7b2c\u4e09\u65b9\u63a5\u5165\u534f\u8bae",
  channelProtocolEndpoint: "\u534f\u8bae\u63a5\u5165\u5730\u5740",
  channelCopyEndpoint: "\u590d\u5236\u63a5\u5165\u5730\u5740",
  channelGenerateToken: "\u751f\u6210 Token",
  showSecret: "\u67e5\u770b",
  hideSecret: "\u9690\u85cf",
  channelTokenGenerated: "Token \u5df2\u751f\u6210\uff0c\u4fdd\u5b58\u8bbe\u7f6e\u540e\u751f\u6548\u3002",
  weixinQRBind: "\u626b\u7801\u7ed1\u5b9a",
  weixinQRHint: "\u4f7f\u7528\u5fae\u4fe1\u626b\u63cf\u4e8c\u7ef4\u7801\uff0c\u786e\u8ba4\u540e\u51ed\u636e\u4fdd\u5b58\u5230\u5f53\u524d MaClawSrv \u7528\u6237\u3002",
  weixinQRLoading: "\u6b63\u5728\u751f\u6210\u4e8c\u7ef4\u7801...",
  weixinQRWaiting: "\u7b49\u5f85\u626b\u7801...",
  weixinQRScanned: "\u5df2\u626b\u7801\uff0c\u8bf7\u5728\u624b\u673a\u4e0a\u786e\u8ba4\u3002",
  weixinQRConfirmed: "\u5fae\u4fe1\u5df2\u7ed1\u5b9a\u3002",
  weixinQRExpired: "\u4e8c\u7ef4\u7801\u5df2\u8fc7\u671f\uff0c\u8bf7\u91cd\u65b0\u751f\u6210\u3002",
  weixinQRError: "\u5fae\u4fe1\u626b\u7801\u767b\u5f55\u5931\u8d25\u3002",
  weixinBoundAccount: "\u5df2\u7ed1\u5b9a\u8d26\u53f7",
  weixinRuntimeStatus: "\u8fd0\u884c\u72b6\u6001",
  weixinRuntimeStarting: "\u542f\u52a8\u4e2d",
  weixinRuntimeConnected: "\u5df2\u8fde\u63a5",
  weixinRuntimeDisconnected: "\u672a\u8fde\u63a5",
  weixinRuntimeError: "\u8fd0\u884c\u9519\u8bef",
  weixinRebind: "\u91cd\u65b0\u7ed1\u5b9a",
  weixinNotBound: "\u5c1a\u672a\u7ed1\u5b9a\u5fae\u4fe1\u8d26\u53f7",
  generateSecret: "\u751f\u6210\u5bc6\u94a5",
  channelTokenUnavailable: "\u5f53\u524d\u6d4f\u89c8\u5668\u4e0d\u53ef\u7528\u5b89\u5168\u968f\u673a\u6570\u3002",
  channelEnabled: "\u5df2\u542f\u7528",
  channelDisabled: "\u672a\u542f\u7528",
  channelAuto: "\u81ea\u52a8",
  channelRunning: "\u5df2\u542f\u7528",
  channelReady: "\u5df2\u914d\u7f6e",
  channelIncomplete: "\u5f85\u914d\u7f6e",
  channelSaveStart: "\u4fdd\u5b58\u5e76\u542f\u52a8",
  channelDisconnect: "\u65ad\u5f00",
  channelRestart: "\u91cd\u542f\u901a\u9053",
  channelSavedStarted: "\u5df2\u4fdd\u5b58\uff0c\u901a\u9053\u5df2\u542f\u7528\u3002",
  channelDisconnected: "\u5df2\u65ad\u5f00\u3002",
  imChannelTabQQ: "QQ Bot",
  imChannelTabTelegram: "Telegram Bot",
  imChannelTabWeixin: "\u5fae\u4fe1",
  imChannelTabLansenger: "\u84dd\u4fe1",
  imChannelTabThirdParty: "\u7b2c\u4e09\u65b9\u63a5\u5165",
  imWatchHistory: "\u76d1\u770b\u5386\u53f2",
  imQQDescription: "\u914d\u7f6e\u5f53\u524d\u7528\u6237\u81ea\u5df1\u7684 QQ Bot \u51ed\u636e\uff0c\u5e76\u542f\u52a8 QQ \u901a\u9053\u3002",
  imTelegramDescription: "\u914d\u7f6e\u5f53\u524d\u7528\u6237\u7684 Telegram Bot Token\uff0c\u5e76\u542f\u52a8 Telegram \u901a\u9053\u3002",
  imWeixinDescription: "\u626b\u7801\u7ed1\u5b9a\u4e2a\u4eba\u5fae\u4fe1\uff0c\u786e\u8ba4\u540e MaClawSrv \u4fdd\u5b58\u51ed\u636e\u5e76\u542f\u7528\u901a\u9053\u3002",
  imLansengerDescription: "\u914d\u7f6e\u5f53\u524d\u7528\u6237\u7684\u84dd\u4fe1\u5e94\u7528\u51ed\u636e\uff0c\u5e76\u542f\u52a8\u84dd\u4fe1\u901a\u9053\u3002",
  imThirdPartyDescription: "\u4e3a\u5f53\u524d\u7528\u6237\u5f00\u653e HTTP \u6d88\u606f\u63a5\u5165\u7aef\u53e3\uff0c\u4f9b\u7b2c\u4e09\u65b9\u8f6f\u4ef6\u8fde\u63a5\u3002",
  imMissingRequired: "\u8bf7\u5148\u8865\u5168\u5fc5\u586b\u9879\uff0c\u518d\u542f\u52a8\u3002",
  imWeixinStartScan: "\u8bf7\u5148\u626b\u7801\u7ed1\u5b9a\u5fae\u4fe1\uff0c\u786e\u8ba4\u540e\u4f1a\u81ea\u52a8\u4fdd\u5b58\u5e76\u542f\u7528\u3002",
  imCancelQR: "\u53d6\u6d88\u626b\u7801",
  imGetAppID: "\u83b7\u53d6 AppID",
  imTutorial: "\u6559\u7a0b",
  imOpenDocs: "\u6253\u5f00\u63a5\u5165\u6587\u6863",
  imProgressHints: "\u663e\u793a\u8fdb\u5ea6\u63d0\u793a",
  imProgressHintsDesc: "\u5173\u95ed\u540e\uff0cIM \u53ea\u663e\u793a\u7b2c\u4e00\u6761\u8fdb\u5ea6\u63d0\u793a\u548c\u6700\u7ec8\u7ed3\u679c\u3002",
  customValue: "\u81ea\u5b9a\u4e49",
  selectAll: "\u5168\u9009",
  clearSelection: "\u6e05\u7a7a"
});
Object.assign(I18N.en, { loading: "Loading...", knowledgeImport: "Knowledge import", knowledgeImportHint: "Add text, documents, document archives, or crawled URLs to this user's knowledge base.", connectedKnowledge: "Accessible knowledge bases", connectedKnowledgeHint: "Knowledge bases this user can read. Import only writes to the user's own knowledge base.", noConnectedKnowledge: "No accessible knowledge bases", selfKnowledge: "Own knowledge base", publicKnowledge: "Public knowledge base", otherUserKnowledge: "Other user's knowledge base", knowledgeOwner: "Owner", knowledgeTenant: "Tenant", knowledgeScopeIDs: "Raw IDs", knowledgeCurrentUser: "current user", knowledgePublicOwner: "public", importText: "Text", importFile: "File or archive", importURL: "URL crawl", title: "Title", topicHint: "Topic hint", labels: "Labels", textToImport: "Text to import", chooseFiles: "Choose documents, ZIP, or RAR", urlsToImport: "URLs to import", crawlDepth: "Crawl depth", sameDomainOnly: "Same domain only", import: "Import", importing: "Importing...", importStarted: "Knowledge import started", importedKnowledge: "Knowledge import completed", importCompleted: "Knowledge import completed", importStillRunning: "Knowledge import still running", importTextPlaceholder: "Paste text...", importURLPlaceholder: "https://example.com/docs", knowledgeTemplate: "Template", insertTemplate: "Insert template", urlExample: "URL example", addURL: "Add URL", importJob: "Import job", importStatus: "Status", importSource: "Source", importTitle: "Title", importKind: "Kind", importFiles: "Files", importUrls: "URLs", importProcessed: "Processed", importImported: "Imported", importFailed: "Failed", importSkipped: "Skipped", importDuplicates: "Duplicates", importWarnings: "Warnings" });
Object.assign(I18N.en, { knowledgeExport: "Knowledge export", knowledgeExportHint: "Export all or selected own knowledge as editable JSON for machine-to-machine exchange or later Hub sharing.", exportTitle: "Export title", exportDescription: "Knowledge description", exportDescriptionPlaceholder: "Describe this export, its use case, and any caveats.", exportSourceIDs: "Selected Source IDs", exportSourceIDsPlaceholder: "Leave empty for all sources. Separate IDs with commas or new lines.", includeDisabledSources: "Include disabled sources", exportFile: "Export file", viewShares: "View shares", exportDescriptionRequired: "Enter the knowledge description first.", exportStarted: "Preparing export file...", exportCompleted: "Knowledge export file is ready.", knowledgePackageImport: "Whole-library import", knowledgePackageImportHint: "Import a MaClaw knowledge JSON package. URL items can be rebuilt; text items import when content is present.", choosePackage: "Choose package", importPackage: "Import package", choosePackageFirst: "Choose a knowledge export JSON file first.", knowledgeShareImport: "Import by knowledge ID or share link", knowledgeShareImportHint: "Paste a human-readable, agent-importable share link, or enter the knowledge ID directly.", knowledgeID: "Knowledge ID", shareLink: "Share link", hubURL: "Hub URL", hubToken: "Hub Token", hubTokenPlaceholder: "Optional, for private/tenant/user-list shares", importShare: "Import share", shareInputRequired: "Enter a knowledge ID or share link." });
Object.assign(I18N.en, { clearOwnKnowledge: "Clear", clearOwnKnowledgeConfirm: "All data in your own knowledge base will be cleared and cannot be recovered. Continue?", clearOwnKnowledgePasswordPrompt: "Enter administrator password or Admin Secret to clear this knowledge base:", clearOwnKnowledgeDone: "Knowledge base cleared. Deleted sources: {count}", clearOwnKnowledgeAuthRequired: "Administrator credential is required." });
Object.assign(I18N.zh, {
  groupIM: "\u0049\u004d",
  groupIMHint: "\u5f53\u524d\u7528\u6237\u9694\u79bb\u7684 QQ\u3001\u5fae\u4fe1\u3001Telegram\u3001\u7b2c\u4e09\u65b9\u63a5\u5165\u3001\u76d1\u770b\u548c\u5386\u53f2\u4ea4\u6d41\u3002",
  channelOverview: "\u0049\u004d",
  channelOverviewHint: "\u914d\u7f6e\u5f53\u524d\u7528\u6237\u7684 QQ\u3001\u5fae\u4fe1\u3001Telegram \u548c\u7b2c\u4e09\u65b9 IM \u63a5\u5165\u3002"
});
Object.assign(I18N.zh, { loading: "\u52a0\u8f7d\u4e2d...", knowledgeImport: "\u77e5\u8bc6\u5e93\u5bfc\u5165", knowledgeImportHint: "\u5c06\u6587\u672c\u3001\u5355\u6587\u6863\u3001\u6587\u6863\u538b\u7f29\u5305\u6216\u6307\u5b9a\u6df1\u5ea6\u7684 URL \u6293\u53d6\u7ed3\u679c\u5bfc\u5165\u5f53\u524d\u7528\u6237\u77e5\u8bc6\u5e93\u3002", connectedKnowledge: "\u53ef\u8bbf\u95ee\u77e5\u8bc6\u5e93", connectedKnowledgeHint: "\u5f53\u524d\u7528\u6237\u53ef\u8bfb\u7684\u77e5\u8bc6\u5e93\u5217\u8868\u3002\u5bfc\u5165\u53ea\u5199\u5165\u81ea\u5df1\u7684\u77e5\u8bc6\u5e93\u3002", noConnectedKnowledge: "\u6682\u65e0\u53ef\u8bbf\u95ee\u77e5\u8bc6\u5e93", selfKnowledge: "\u81ea\u5df1\u7684\u77e5\u8bc6\u5e93", publicKnowledge: "\u516c\u5171\u77e5\u8bc6\u5e93", otherUserKnowledge: "\u5176\u5b83\u7528\u6237\u7684\u77e5\u8bc6\u5e93", knowledgeOwner: "\u5c5e\u4e3b", knowledgeTenant: "\u79df\u6237", knowledgeScopeIDs: "\u539f\u59cb ID", knowledgeCurrentUser: "\u5f53\u524d\u7528\u6237", knowledgePublicOwner: "\u516c\u5171", importText: "\u6587\u672c", importFile: "\u6587\u4ef6/\u538b\u7f29\u5305", importURL: "URL \u679a\u4e3e", title: "\u6807\u9898", topicHint: "\u4e3b\u9898\u63d0\u793a", labels: "\u6807\u7b7e", textToImport: "\u5bfc\u5165\u6587\u672c", chooseFiles: "\u9009\u62e9\u6587\u6863\u3001ZIP \u6216 RAR", urlsToImport: "\u5bfc\u5165 URL", crawlDepth: "\u679a\u4e3e\u6df1\u5ea6", sameDomainOnly: "\u4ec5\u540c\u57df\u540d", import: "\u5bfc\u5165", importing: "\u5bfc\u5165\u4e2d...", importStarted: "\u77e5\u8bc6\u5e93\u5bfc\u5165\u5df2\u5f00\u59cb", importedKnowledge: "\u77e5\u8bc6\u5e93\u5bfc\u5165\u5df2\u5b8c\u6210", importCompleted: "\u77e5\u8bc6\u5e93\u5bfc\u5165\u5df2\u5b8c\u6210", importStillRunning: "\u77e5\u8bc6\u5e93\u5bfc\u5165\u4ecd\u5728\u8fd0\u884c", importTextPlaceholder: "\u7c98\u8d34\u8981\u5bfc\u5165\u7684\u6587\u672c...", importURLPlaceholder: "https://example.com/docs", knowledgeTemplate: "\u6a21\u677f", insertTemplate: "\u63d2\u5165\u6a21\u677f", urlExample: "URL \u793a\u4f8b", addURL: "\u6dfb\u52a0 URL", importJob: "\u5bfc\u5165\u4efb\u52a1", importStatus: "\u72b6\u6001", importSource: "\u6765\u6e90", importTitle: "\u6807\u9898", importKind: "\u7c7b\u578b", importFiles: "\u6587\u4ef6", importUrls: "URL", importProcessed: "\u5df2\u5904\u7406", importImported: "\u5df2\u5bfc\u5165", importFailed: "\u5931\u8d25", importSkipped: "\u8df3\u8fc7", importDuplicates: "\u91cd\u590d", importWarnings: "\u8b66\u544a" });
Object.assign(I18N.zh, { knowledgeExport: "知识库导出", knowledgeExportHint: "导出当前用户自己的全部或部分知识为可编辑 JSON 文件，可用于跨机器交换或后续上传 Hub。", exportTitle: "导出标题", exportDescription: "知识描述", exportDescriptionPlaceholder: "说明这次导出的内容、适用场景和注意事项。", exportSourceIDs: "部分导出 Source IDs", exportSourceIDsPlaceholder: "留空表示导出全部；多个 ID 用逗号或换行分隔。", includeDisabledSources: "包含禁用来源", exportFile: "导出文件", viewShares: "查看分享", exportDescriptionRequired: "请先填写知识描述。", exportStarted: "正在生成导出文件...", exportCompleted: "知识库导出文件已生成。", knowledgePackageImport: "整库导入", knowledgePackageImportHint: "导入 MaClaw 知识导出 JSON 包。当前可重建 URL 项；包含 content 字段的文本项也可导入。", choosePackage: "选择导出包", importPackage: "导入包", choosePackageFirst: "请先选择知识导出 JSON 文件。", knowledgeShareImport: "按知识 ID / 分享链接导入", knowledgeShareImportHint: "粘贴人可阅读、agent 可导入的分享链接，或直接输入知识 ID。", knowledgeID: "知识 ID", shareLink: "分享链接", hubURL: "Hub 地址", hubToken: "Hub Token", hubTokenPlaceholder: "私有/租户/用户列表可见时可填写", importShare: "导入分享", shareInputRequired: "请填写知识 ID 或分享链接。" });
Object.assign(I18N.en, { ownKnowledgeImports: "Own import batches", ownKnowledgeImportsHint: "Batches imported into this user's own knowledge base.", knowledgeBatchName: "File or folder", knowledgeBatchFiles: "Files", knowledgeBatchUpdated: "Updated", knowledgeBatchEmpty: "No import batches", knowledgeBatchSamples: "Samples", knowledgeBatchMeta: "{imported} imported / {skipped} skipped / {failed} failed" });
Object.assign(I18N.zh, { ownKnowledgeImports: "\u81ea\u5df1\u7684\u77e5\u8bc6\u5e93\u5217\u8868", ownKnowledgeImportsHint: "\u6309\u5bfc\u5165\u6279\u6b21\u5217\u51fa\u5f53\u524d\u7528\u6237\u81ea\u5df1\u7684\u77e5\u8bc6\u5e93\u5185\u5bb9\u3002", knowledgeBatchName: "\u6587\u4ef6\u540d/\u76ee\u5f55\u540d", knowledgeBatchFiles: "\u6587\u4ef6\u6570", knowledgeBatchUpdated: "\u66f4\u65b0\u65f6\u95f4", knowledgeBatchEmpty: "\u6682\u65e0\u5bfc\u5165\u6279\u6b21", knowledgeBatchSamples: "\u793a\u4f8b\u6587\u4ef6", knowledgeBatchMeta: "\u5df2\u5bfc\u5165 {imported} / \u8df3\u8fc7 {skipped} / \u5931\u8d25 {failed}" });
Object.assign(I18N.en, { deleteKnowledgeBatch: "Delete", deleteKnowledgeBatchTitle: "Delete import batch", deleteKnowledgeBatchConfirm: "Delete import batch \"{name}\" and its imported knowledge? This cannot be undone.", deleteKnowledgeBatchDone: "Deleted import batch. Sources deleted: {count}" });
Object.assign(I18N.zh, { deleteKnowledgeBatch: "\u5220\u9664", deleteKnowledgeBatchTitle: "\u5220\u9664\u5bfc\u5165\u6279\u6b21", deleteKnowledgeBatchConfirm: "\u786e\u8ba4\u5220\u9664\u5bfc\u5165\u6279\u6b21\u201c{name}\u201d\u53ca\u5176\u5bfc\u5165\u7684\u77e5\u8bc6\u5185\u5bb9\uff1f\u6b64\u64cd\u4f5c\u4e0d\u53ef\u6062\u590d\u3002", deleteKnowledgeBatchDone: "\u5df2\u5220\u9664\u5bfc\u5165\u6279\u6b21\uff0c\u5220\u9664\u6765\u6e90\u6570\uff1a{count}" });
Object.assign(I18N.zh, { clearOwnKnowledge: "\u6e05\u9664", clearOwnKnowledgeConfirm: "\u81ea\u5df1\u7684\u77e5\u8bc6\u5e93\u4e2d\u7684\u6240\u6709\u6570\u636e\u5c06\u88ab\u6e05\u9664\uff0c\u4e14\u4e0d\u53ef\u6062\u590d\u3002\u786e\u8ba4\u7ee7\u7eed\uff1f", clearOwnKnowledgePasswordPrompt: "\u8bf7\u8f93\u5165\u7ba1\u7406\u5458\u5bc6\u7801\u6216 Admin Secret\uff0c\u7528\u4e8e\u6e05\u9664\u8be5\u77e5\u8bc6\u5e93\uff1a", clearOwnKnowledgeDone: "\u77e5\u8bc6\u5e93\u5df2\u6e05\u9664\uff0c\u5220\u9664\u6765\u6e90\u6570\uff1a{count}", clearOwnKnowledgeAuthRequired: "\u9700\u8981\u8f93\u5165\u7ba1\u7406\u5458\u51ed\u636e\u3002" });
Object.assign(I18N.en, { enterTextFirst: "Enter text before importing.", chooseFileFirst: "Choose at least one document or archive.", enterURLFirst: "Enter at least one URL.", customTopicRequired: "Enter the custom topic hint.", customLabelRequired: "Enter the custom labels.", customTitleRequired: "Enter the custom title.", importQueued: "Import submitted. Checking progress...", importProgress: "Checking progress {current}/{total}..." });
Object.assign(I18N.zh, { enterTextFirst: "\u8bf7\u5148\u8f93\u5165\u8981\u5bfc\u5165\u7684\u6587\u672c\u3002", chooseFileFirst: "\u8bf7\u5148\u9009\u62e9\u81f3\u5c11\u4e00\u4e2a\u6587\u6863\u6216\u538b\u7f29\u5305\u3002", enterURLFirst: "\u8bf7\u5148\u8f93\u5165\u81f3\u5c11\u4e00\u4e2a URL\u3002", customTopicRequired: "\u8bf7\u8f93\u5165\u81ea\u5b9a\u4e49\u4e3b\u9898\u63d0\u793a\u3002", customLabelRequired: "\u8bf7\u8f93\u5165\u81ea\u5b9a\u4e49\u6807\u7b7e\u3002", customTitleRequired: "\u8bf7\u8f93\u5165\u81ea\u5b9a\u4e49\u6807\u9898\u3002", importQueued: "\u5bfc\u5165\u4efb\u52a1\u5df2\u63d0\u4ea4\uff0c\u6b63\u5728\u67e5\u8be2\u8fdb\u5ea6...", importProgress: "\u6b63\u5728\u67e5\u8be2\u8fdb\u5ea6 {current}/{total}..." });
Object.assign(I18N.en, { knowledgeNav: "Knowledge", knowledgeTitle: "Knowledge Base", knowledgeHint: "Import into your own knowledge base and search readable knowledge scopes.", knowledgeQuery: "Knowledge query", knowledgeQueryHint: "Search your own knowledge plus connected readable scopes. Other users' private knowledge is not queried.", knowledgeQueryPlaceholder: "Search knowledge...", knowledgeLimit: "Results", knowledgeNoResults: "No matching knowledge", knowledgeQueryFailed: "Knowledge query failed", knowledgeResultType: "Type", knowledgeResultScore: "Score" });
Object.assign(I18N.zh, { knowledgeNav: "\u77e5\u8bc6\u5e93", knowledgeTitle: "\u77e5\u8bc6\u5e93", knowledgeHint: "\u5bfc\u5165\u5230\u81ea\u5df1\u7684\u77e5\u8bc6\u5e93\uff0c\u5e76\u67e5\u8be2\u81ea\u5df1\u53ef\u8bfb\u7684\u77e5\u8bc6\u8303\u56f4\u3002", knowledgeQuery: "\u77e5\u8bc6\u67e5\u8be2", knowledgeQueryHint: "\u4ec5\u641c\u7d22\u81ea\u6709\u77e5\u8bc6\u548c\u5df2\u5173\u8054\u7684\u53ef\u8bfb\u8303\u56f4\uff0c\u4e0d\u67e5\u5176\u5b83\u7528\u6237\u7684\u79c1\u6709\u77e5\u8bc6\u3002", knowledgeQueryPlaceholder: "\u641c\u7d22\u77e5\u8bc6...", knowledgeLimit: "\u7ed3\u679c\u6570", knowledgeNoResults: "\u672a\u627e\u5230\u5339\u914d\u77e5\u8bc6", knowledgeQueryFailed: "\u77e5\u8bc6\u67e5\u8be2\u5931\u8d25", knowledgeResultType: "\u7c7b\u578b", knowledgeResultScore: "\u5206\u6570" });
Object.assign(I18N.en, { memoryManager: "Memory management", memoryManagerHint: "View, search, add, edit, and delete this user's long-term memory.", memorySearch: "Search memory", memoryCategory: "Category", memoryContent: "Memory content", memoryContentRequired: "Enter memory content.", memoryContentTooLong: "Memory content must be {max} characters or fewer.", memoryTags: "Tags", memoryTagsHint: "Comma or newline separated", memoryTagsTooMany: "Use {max} tags or fewer.", memoryTagTooLong: "Each tag must be {max} characters or fewer.", memoryRefresh: "Refresh", memoryClear: "Clear", memoryAdd: "Add memory", memoryUpdate: "Update memory", memoryCancelEdit: "Cancel edit", memoryEmpty: "No memory entries", memorySaved: "Memory saved", memoryDeleted: "Memory deleted", memoryUpdated: "Memory updated", memoryEdit: "Edit", memoryDelete: "Delete", memoryAllCategories: "All categories", memoryTotal: "Total", memoryAccessCount: "Access", memoryUpdatedAt: "Updated", memoryLoadMore: "Load more" });
Object.assign(I18N.zh, { memoryManager: "\u8bb0\u5fc6\u7ba1\u7406", memoryManagerHint: "\u67e5\u770b\u3001\u641c\u7d22\u3001\u65b0\u589e\u3001\u7f16\u8f91\u548c\u5220\u9664\u5f53\u524d\u7528\u6237\u7684\u957f\u671f\u8bb0\u5fc6\u3002", memorySearch: "\u641c\u7d22\u8bb0\u5fc6", memoryCategory: "\u5206\u7c7b", memoryContent: "\u8bb0\u5fc6\u5185\u5bb9", memoryContentRequired: "\u8bf7\u8f93\u5165\u8bb0\u5fc6\u5185\u5bb9\u3002", memoryContentTooLong: "\u8bb0\u5fc6\u5185\u5bb9\u4e0d\u80fd\u8d85\u8fc7 {max} \u4e2a\u5b57\u7b26\u3002", memoryTags: "\u6807\u7b7e", memoryTagsHint: "\u9017\u53f7\u6216\u6362\u884c\u5206\u9694", memoryTagsTooMany: "\u6807\u7b7e\u4e0d\u80fd\u8d85\u8fc7 {max} \u4e2a\u3002", memoryTagTooLong: "\u5355\u4e2a\u6807\u7b7e\u4e0d\u80fd\u8d85\u8fc7 {max} \u4e2a\u5b57\u7b26\u3002", memoryRefresh: "\u5237\u65b0", memoryClear: "\u6e05\u9664", memoryAdd: "\u6dfb\u52a0\u8bb0\u5fc6", memoryUpdate: "\u66f4\u65b0\u8bb0\u5fc6", memoryCancelEdit: "\u53d6\u6d88\u7f16\u8f91", memoryEmpty: "\u6682\u65e0\u8bb0\u5fc6\u6761\u76ee", memorySaved: "\u8bb0\u5fc6\u5df2\u4fdd\u5b58", memoryDeleted: "\u8bb0\u5fc6\u5df2\u5220\u9664", memoryUpdated: "\u8bb0\u5fc6\u5df2\u66f4\u65b0", memoryEdit: "\u7f16\u8f91", memoryDelete: "\u5220\u9664", memoryAllCategories: "\u5168\u90e8\u5206\u7c7b", memoryTotal: "\u603b\u6570", memoryAccessCount: "\u8bbf\u95ee", memoryUpdatedAt: "\u66f4\u65b0", memoryLoadMore: "\u52a0\u8f7d\u66f4\u591a" });
Object.assign(I18N.en, { mcpManager: "MCP", mcpManagerHint: "Use capability marketplace first. Add manually by JSON or compact editor only when needed.", mcpMarketplace: "Capability Marketplace", mcpMarketplaceHint: "Search and install MCP capabilities from Hub/HubCenter. Manual config stays compact.", mcpInstalled: "Installed MCP", mcpNoServers: "No MCP servers", mcpManualAdd: "Add MCP", mcpModeMarket: "Marketplace", mcpModeRemote: "Remote HTTP", mcpModeLocal: "Local stdio", mcpModeJson: "JSON import", mcpName: "Name", mcpEndpoint: "Endpoint", mcpCommand: "Command", mcpArgs: "Args", mcpEnv: "Env", mcpHeaders: "Headers", mcpAuthType: "Auth", mcpSecret: "Secret", mcpAutoStart: "Auto start", mcpDisabled: "Disabled", mcpAdd: "Add", mcpEdit: "Edit", mcpSave: "Save MCP", mcpClose: "Close", mcpAddParam: "Add param", mcpParamName: "Param", mcpParamValue: "Value", mcpStart: "Start", mcpStop: "Stop", mcpCheck: "Check", mcpDelete: "Delete", mcpAdded: "MCP added", mcpUpdated: "MCP updated", mcpDeleted: "MCP deleted", mcpJson: "MCP JSON", mcpJsonHint: "Paste Claude-style mcpServers JSON or an array/object of MaClaw MCP entries.", mcpOpenGui: "Open MaClaw GUI > MCP > Marketplace for market install." });
Object.assign(I18N.zh, { mcpManager: "MCP", mcpManagerHint: "\u4f18\u5148\u4ece\u80fd\u529b\u5e02\u573a\u9009\uff1b\u53ea\u6709\u5fc5\u8981\u65f6\u518d\u7528 JSON \u6216\u7cbe\u7b80\u7f16\u8f91\u754c\u9762\u6dfb\u52a0 MCP \u914d\u7f6e\u3002", mcpMarketplace: "\u80fd\u529b\u5e02\u573a", mcpMarketplaceHint: "\u641c\u7d22\u5e76\u5b89\u88c5 Hub/HubCenter MCP \u80fd\u529b\uff1b\u624b\u52a8\u914d\u7f6e\u4fdd\u6301\u7cbe\u7b80\u3002", mcpInstalled: "\u5df2\u5b89\u88c5 MCP", mcpNoServers: "\u6682\u65e0 MCP \u670d\u52a1", mcpManualAdd: "\u6dfb\u52a0 MCP", mcpModeMarket: "\u80fd\u529b\u5e02\u573a", mcpModeRemote: "\u8fdc\u7a0b HTTP", mcpModeLocal: "\u672c\u5730 stdio", mcpModeJson: "JSON \u5bfc\u5165", mcpName: "\u540d\u79f0", mcpEndpoint: "\u63a5\u5165\u5730\u5740", mcpCommand: "\u547d\u4ee4", mcpArgs: "\u53c2\u6570", mcpEnv: "\u73af\u5883\u53d8\u91cf", mcpHeaders: "Headers", mcpAuthType: "\u8ba4\u8bc1", mcpSecret: "\u5bc6\u94a5", mcpAutoStart: "\u81ea\u52a8\u542f\u52a8", mcpDisabled: "\u7981\u7528", mcpAdd: "\u6dfb\u52a0", mcpEdit: "\u7f16\u8f91", mcpSave: "\u4fdd\u5b58 MCP", mcpClose: "\u6536\u8d77", mcpAddParam: "\u6dfb\u52a0\u53c2\u6570", mcpParamName: "\u53c2\u6570", mcpParamValue: "\u503c", mcpStart: "\u542f\u52a8", mcpStop: "\u505c\u6b62", mcpCheck: "\u68c0\u67e5", mcpDelete: "\u5220\u9664", mcpAdded: "MCP \u5df2\u6dfb\u52a0", mcpUpdated: "MCP \u5df2\u66f4\u65b0", mcpDeleted: "MCP \u5df2\u5220\u9664", mcpJson: "MCP JSON", mcpJsonHint: "\u7c98\u8d34 Claude \u98ce\u683c mcpServers JSON\uff0c\u6216 MaClaw MCP \u6761\u76ee\u6570\u7ec4/\u5bf9\u8c61\u3002", mcpOpenGui: "\u8bf7\u5728 MaClaw GUI > MCP > \u80fd\u529b\u5e02\u573a\u5b8c\u6210\u5e02\u573a\u5b89\u88c5\u3002" });
Object.assign(I18N.en, { webSearchManager: "Web search", webSearchHint: "Search service is managed by the administrator in Admin Web Client Config. This page only shows the effective provider.", webSearchNoProviders: "No search provider", webSearchCurrent: "Current search service", webSearchManagedByAdmin: "Managed by administrator", webSearchProviderCount: "{count} configured", webSearchProviderType: "Type" });
Object.assign(I18N.zh, { webSearchManager: "\u8054\u7f51\u641c\u7d22\u670d\u52a1", webSearchHint: "\u641c\u7d22\u670d\u52a1\u7531\u7ba1\u7406\u5458\u5728\u540e\u53f0\u300c\u5ba2\u6237\u7aef\u914d\u7f6e\u300d\u7edf\u4e00\u8bbe\u7f6e\uff0c\u6b64\u5904\u4ec5\u663e\u793a\u5f53\u524d\u751f\u6548 provider\u3002", webSearchNoProviders: "\u6682\u65e0\u641c\u7d22\u670d\u52a1", webSearchCurrent: "\u5f53\u524d\u641c\u7d22\u670d\u52a1", webSearchManagedByAdmin: "\u7531\u7ba1\u7406\u5458\u7edf\u4e00\u914d\u7f6e", webSearchProviderCount: "\u5df2\u914d\u7f6e {count} \u4e2a", webSearchProviderType: "\u7c7b\u578b" });
const FIELD_I18N = {
  en: {
    maclaw_llm_url: ["LLM URL", "Legacy flat LLM endpoint URL."], maclaw_llm_key: ["LLM Access Token", "Provider access token entered manually. This field does not generate a key."], maclaw_llm_model: ["LLM Model", "Legacy flat default model. Use auto for VE Platform Hub LLM endpoints; service groups are platform metadata, not model names."], maclaw_llm_current_provider: ["Current Provider", "Selected provider name from maclaw_llm_providers."], maclaw_llm_providers: ["LLM Providers", "Provider list. When configured, MaClawSrv prefers the selected provider over legacy flat fields."],
    mcp_servers: ["Remote MCP Servers", "Remote MCP server registry shared by all user assistant instances."], local_mcp_servers: ["Local MCP Servers", "Local MCP stdio server registry shared by all user assistant instances."], ssh_hosts: ["SSH Hosts", "Preconfigured SSH host labels available to user assistant instances."], web_search_providers: ["Web Search Providers", "Search provider configuration shared by user assistant instances."], web_search_current_provider: ["Current Web Search Provider", "Selected provider name from web_search_providers."],
    nl_skills: ["Installed Skills", "User-level skill entries available to assistant instances."], skill_hub_urls: ["Skill Hubs", "Skill discovery sources for this user."], external_skill_dirs: ["External Skill Directories", "Additional user skill directories."], skill_sources_allowed: ["Allowed Skill Sources", "Optional allow-list for skill sources. Empty allows all configured sources."],
    memory_auto_compress: ["Memory Auto Compress", "Enable automatic conversation and memory compression."], memory_max_backups: ["Memory Max Backups", "Maximum memory backup count. Zero uses service default."], knowledge_skill_token_budget: ["Knowledge Skill Token Budget", "Token budget for knowledge skill context packs. Zero uses service default."],
    security_policy_mode: ["Security Policy Mode", "User-level security policy mode for tool and agent execution."], sandbox_mode: ["Sandbox Mode", "Execution sandbox preference for this user."], network_level: ["Network Level", "Network access level for user tools and agents."], yolo_mode_allowed: ["YOLO Mode Allowed", "Allow this user to enable broad tool execution mode."]
  },
  zh: {
    maclaw_llm_url: ["LLM 服务地址", "旧版平铺 LLM 服务端点地址。由 VE Platform 托管时通常自动填入。"], maclaw_llm_key: ["LLM 访问令牌", "这里填写服务商访问令牌，不需要生成密钥。"], maclaw_llm_model: ["LLM 模型", "旧版默认模型；接入 VE Platform Hub 时使用 auto，服务组由平台元数据管理，不填在这里。"], maclaw_llm_current_provider: ["当前服务商", "从 maclaw_llm_providers 中选择的服务商名称。"], maclaw_llm_providers: ["LLM 服务商列表", "服务商列表。配置后会优先使用选中的服务商，而不是旧版平铺字段。"],
    mcp_servers: ["远程 MCP 服务", "所有助手实例共享的远程 MCP 服务注册表。"], local_mcp_servers: ["本地 MCP 服务", "所有助手实例共享的本地 stdio MCP 服务注册表。"], ssh_hosts: ["SSH 主机", "预配置给用户助手实例使用的 SSH 主机标签。"], web_search_providers: ["联网搜索服务", "用户助手实例共享的搜索服务配置。"], web_search_current_provider: ["当前搜索服务", "从 web_search_providers 中选择的搜索服务名称。"],
    nl_skills: ["已安装技能", "可供助手实例使用的用户级技能条目。"], skill_hub_urls: ["技能中心", "此用户的技能发现来源。"], external_skill_dirs: ["外部技能目录", "额外的用户技能目录。"], skill_sources_allowed: ["允许的技能来源", "可选的技能来源白名单。留空表示允许所有已配置来源。"],
    memory_auto_compress: ["自动压缩记忆", "启用会话与记忆的自动压缩。"], memory_max_backups: ["记忆备份上限", "最大记忆备份数量。0 表示使用服务默认值。"], knowledge_skill_token_budget: ["知识技能 Token 预算", "知识技能上下文包的 Token 预算。0 表示使用服务默认值。"],
    security_policy_mode: ["安全策略模式", "用户级工具和 Agent 执行安全策略模式。"], sandbox_mode: ["沙箱模式", "此用户的执行沙箱偏好。"], network_level: ["网络访问级别", "用户工具和 Agent 的网络访问级别。"], yolo_mode_allowed: ["允许 YOLO 模式", "允许此用户启用宽松工具执行模式。"]
  }
};
const HIDDEN_CONFIG_KEYS = new Set([
  "claude", "codex", "opencode", "codebuddy", "iflow", "kilo",
  "projects", "current_project", "active_tool", "default_tool", "default_tool_provider",
  "show_codex", "show_opencode", "show_codebuddy", "show_iflow", "show_kilo",
  "extra_tool_configs", "use_windows_terminal", "nl_skills", "llm_token_usage",
  "mcp_servers", "local_mcp_servers", "ssh_hosts", "skill_hub_urls", "external_skill_dirs", "skill_sources_allowed", "web_search_providers", "web_search_current_provider",
  "default_proxy_enabled", "default_proxy_protocol", "default_proxy_host", "default_proxy_port", "default_proxy_username", "default_proxy_password", "default_proxy_bypass", "default_proxy_scope_maclaw", "default_proxy_scope_coding_tools", "default_proxy_scope_agent",
  "hub_security_centralized", "security_policy_mode", "network_level", "network_allowlist",
  "language", "ui_mode", "working_directory", "vector_search_enabled", "tts_enabled", "asr_enabled", "im_progress_nudge_enabled",
  "thirdparty_gateway_host", "thirdparty_gateway_port",
  "maclaw_llm_protocol", "maclaw_llm_context_length", "maclaw_llm_timeout_sec", "skill_runner_timeout_sec", "maclaw_llm_current_provider", "maclaw_llm_providers", "llm_prompt_cache", "auxiliary_llm", "model_routes",
  "remote_user_id", "remote_tenant_id", "remote_tenant_name", "remote_machine_id", "remote_machine_name",
  "remote_machine_token", "remote_viewer_token", "skill_market_session_token", "remote_client_id", "remote_sn",
  "env_check_done", "last_env_check_time", "onboarding_done", "floating_btn_x", "floating_btn_y",
  "floating_btn_position_set", "noise_floor_calibrated", "speech_level_calibrated"
]);
const ADMIN_MANAGED_CONFIG_KEYS = [
  "web_search_providers", "web_search_current_provider",
  "default_proxy_enabled", "default_proxy_protocol", "default_proxy_host", "default_proxy_port", "default_proxy_username", "default_proxy_password", "default_proxy_bypass", "default_proxy_scope_maclaw", "default_proxy_scope_coding_tools", "default_proxy_scope_agent",
  "mcp_servers", "local_mcp_servers", "ssh_hosts", "skill_hub_urls", "external_skill_dirs", "skill_sources_allowed",
  "hub_security_centralized", "security_policy_mode", "network_level", "network_allowlist",
  "language", "ui_mode", "working_directory", "vector_search_enabled", "tts_enabled", "asr_enabled", "im_progress_nudge_enabled"
];
function userConfigDraft(config) {
  return { ...(config || {}) };
}
function stripAdminManagedConfig(config) {
  const out = { ...(config || {}) };
  ADMIN_MANAGED_CONFIG_KEYS.forEach((key) => delete out[key]);
  return out;
}
const requestedLocale = (params.get("lang") || localStorage.getItem("maclaw.user.lang") || document.documentElement.lang || navigator.language || "zh-CN").toLowerCase();
Object.assign(FIELD_I18N.zh, {
  maclaw_llm_url: ["LLM 服务地址", "旧版平铺 LLM 服务端点地址。由 VE Platform 托管时通常自动填入。"], maclaw_llm_key: ["LLM 访问令牌", "这里填写服务商访问令牌，不需要生成密钥。"], maclaw_llm_model: ["LLM 模型", "旧版默认模型；接入 VE Platform Hub 时使用 auto，服务组由平台元数据管理，不填在这里。"], maclaw_llm_current_provider: ["当前服务商", "从 maclaw_llm_providers 中选择的服务商名称。"], maclaw_llm_providers: ["LLM 服务商列表", "服务商列表。配置后会优先使用选中的服务商，而不是旧版平铺字段。"],
  mcp_servers: ["远程 MCP 服务", "所有助手实例共享的远程 MCP 服务注册表。"], local_mcp_servers: ["本地 MCP 服务", "所有助手实例共享的本地 stdio MCP 服务注册表。"], ssh_hosts: ["SSH 主机", "预配置给用户助手实例使用的 SSH 主机标签。"], web_search_providers: ["联网搜索服务", "用户助手实例共享的搜索服务配置。"], web_search_current_provider: ["当前搜索服务", "从 web_search_providers 中选择的搜索服务名称。"],
  nl_skills: ["已安装技能", "可供助手实例使用的用户级技能条目。"], skill_hub_urls: ["技能中心", "此用户的技能发现来源。"], external_skill_dirs: ["外部技能目录", "额外的用户技能目录。"], skill_sources_allowed: ["允许的技能来源", "可选的技能来源白名单。留空表示允许所有已配置来源。"],
  memory_auto_compress: ["自动压缩记忆", "启用会话与记忆的自动压缩。"], memory_max_backups: ["记忆备份上限", "最大记忆备份数量。0 表示使用服务默认值。"], knowledge_skill_token_budget: ["知识技能 Token 预算", "知识技能上下文包的 Token 预算。0 表示使用服务默认值。"],
  security_policy_mode: ["安全策略模式", "用户级工具和 Agent 执行安全策略模式。"], sandbox_mode: ["沙箱模式", "此用户的执行沙箱偏好。"], network_level: ["网络访问级别", "用户工具和 Agent 的网络访问级别。"], yolo_mode_allowed: ["允许 YOLO 模式", "允许此用户启用宽松工具执行模式。"]
});
Object.assign(FIELD_I18N.en, {
  qqbot_enabled: ["Enable QQ Bot", "Enable this user's QQ Bot binding."], qqbot_app_id: ["QQ Bot App ID", "QQ Bot application ID."], qqbot_app_secret: ["QQ Bot App Secret", "QQ Bot application secret."],
  telegram_bot_enabled: ["Enable Telegram Bot", "Enable this user's Telegram Bot binding."], telegram_bot_token: ["Telegram Bot Token", "BotFather token used by the Telegram Bot channel."],
  weixin_enabled: ["Enable personal WeChat", "Enable this user's iLink/personal WeChat binding."], weixin_token: ["Personal WeChat Token", "iLink/personal WeChat session token."], weixin_account_id: ["Personal WeChat Account ID", "Bound personal WeChat account ID."],
  lansenger_enabled: ["Enable Lansenger", "Enable this user's Lansenger binding."], lansenger_app_id: ["Lansenger App ID", "Lansenger application ID."], lansenger_app_secret: ["Lansenger App Secret", "Lansenger application secret."], lansenger_gateway_url: ["Lansenger API Gateway", "Lansenger API gateway base URL."], lansenger_wss_url: ["Lansenger WebSocket URL", "Optional Lansenger WebSocket gateway override."],
  thirdparty_gateway_enabled: ["Enable third-party access", "Enable MaClaw third-party IM access for this user."], thirdparty_gateway_token: ["Access Token", "Unique bearer token used to route third-party messages to this user."], thirdparty_gateway_host: ["Gateway Host", "Service-level MaClawSrv gateway host."], thirdparty_gateway_port: ["Gateway Port", "Service-level MaClawSrv gateway port."]
});
Object.assign(FIELD_I18N.zh, {
  qqbot_enabled: ["\u542f\u7528 QQ Bot", "\u542f\u7528\u6b64\u7528\u6237\u7684 QQ Bot \u7ed1\u5b9a\u3002"], qqbot_app_id: ["QQ Bot App ID", "QQ Bot \u5e94\u7528 ID\u3002"], qqbot_app_secret: ["QQ Bot App Secret", "QQ Bot \u5e94\u7528\u5bc6\u94a5\u3002"],
  telegram_bot_enabled: ["\u542f\u7528 Telegram Bot", "\u542f\u7528\u6b64\u7528\u6237\u7684 Telegram Bot \u7ed1\u5b9a\u3002"], telegram_bot_token: ["Telegram Bot Token", "Telegram BotFather \u9881\u53d1\u7684\u673a\u5668\u4eba Token\u3002"],
  weixin_enabled: ["\u542f\u7528\u4e2a\u4eba\u5fae\u4fe1", "\u542f\u7528\u6b64\u7528\u6237\u7684 iLink/\u4e2a\u4eba\u5fae\u4fe1\u7ed1\u5b9a\u3002"], weixin_token: ["\u4e2a\u4eba\u5fae\u4fe1 Token", "iLink/\u4e2a\u4eba\u5fae\u4fe1\u4f1a\u8bdd Token\u3002"], weixin_account_id: ["\u4e2a\u4eba\u5fae\u4fe1\u8d26\u53f7 ID", "\u5df2\u7ed1\u5b9a\u4e2a\u4eba\u5fae\u4fe1\u8d26\u53f7 ID\u3002"],
  lansenger_enabled: ["\u542f\u7528\u84dd\u4fe1", "\u542f\u7528\u6b64\u7528\u6237\u7684\u84dd\u4fe1\u7ed1\u5b9a\u3002"], lansenger_app_id: ["\u84dd\u4fe1 App ID", "\u84dd\u4fe1\u5e94\u7528 ID\u3002"], lansenger_app_secret: ["\u84dd\u4fe1 App Secret", "\u84dd\u4fe1\u5e94\u7528\u5bc6\u94a5\u3002"], lansenger_gateway_url: ["\u84dd\u4fe1 API Gateway", "\u84dd\u4fe1 API \u7f51\u5173\u5730\u5740\u3002"], lansenger_wss_url: ["\u84dd\u4fe1 WebSocket \u5730\u5740", "\u53ef\u9009\u7684\u84dd\u4fe1 WebSocket \u7f51\u5173\u5730\u5740\u3002"],
  thirdparty_gateway_enabled: ["\u542f\u7528\u7b2c\u4e09\u65b9\u63a5\u5165", "\u542f\u7528\u6b64\u7528\u6237\u7684 MaClaw \u7b2c\u4e09\u65b9 IM \u63a5\u5165\u3002"], thirdparty_gateway_token: ["\u63a5\u5165 Token", "\u6b64\u7528\u6237\u4e13\u5c5e Bearer Token\uff0c\u7528\u4e8e\u5c06\u7b2c\u4e09\u65b9\u6d88\u606f\u8def\u7531\u5230\u8be5\u7528\u6237\u3002"], thirdparty_gateway_host: ["\u7f51\u5173\u5730\u5740", "MaClawSrv \u670d\u52a1\u7ea7\u7b2c\u4e09\u65b9\u7f51\u5173\u5730\u5740\u3002"], thirdparty_gateway_port: ["\u7f51\u5173\u7aef\u53e3", "MaClawSrv \u670d\u52a1\u7ea7\u7b2c\u4e09\u65b9\u7f51\u5173\u7aef\u53e3\u3002"]
});
Object.assign(FIELD_I18N.en, {
  im_progress_nudge_enabled: ["Show progress hints", "When off, IM only shows the first progress note and the final result."]
});
Object.assign(FIELD_I18N.zh, {
  im_progress_nudge_enabled: ["\u663e\u793a\u8fdb\u5ea6\u63d0\u793a", "\u5173\u95ed\u540e\uff0cIM \u53ea\u663e\u793a\u7b2c\u4e00\u6761\u8fdb\u5ea6\u63d0\u793a\u548c\u6700\u7ec8\u7ed3\u679c\u3002"]
});
Object.assign(FIELD_I18N.en, {
  audio_input_device_id: ["Audio input device", "Preferred microphone/input device."],
  audio_output_device_id: ["Audio output device", "Preferred speaker/output device."]
});
Object.assign(FIELD_I18N.zh, {
  audio_input_device_id: ["\u97f3\u9891\u8f93\u5165\u8bbe\u5907", "\u4f18\u5148\u4f7f\u7528\u7684\u9ea6\u514b\u98ce/\u8f93\u5165\u8bbe\u5907\u3002"],
  audio_output_device_id: ["\u97f3\u9891\u8f93\u51fa\u8bbe\u5907", "\u4f18\u5148\u4f7f\u7528\u7684\u626c\u58f0\u5668/\u8f93\u51fa\u8bbe\u5907\u3002"]
});
Object.assign(FIELD_I18N.en, {
  language: ["Language", "Preferred interface language."],
  screen_dim_timeout_min: ["Screen dim timeout", "Minutes before the screen dim preference applies. Zero disables it."],
  ui_mode: ["Interface mode", "Preferred web interface density."],
  ui_zoom_factor: ["Interface zoom", "Preferred interface scale."],
  show_tray_icon: ["Show tray icon", "Show MaClaw in the system tray when supported."],
  default_proxy_enabled: ["Default proxy", "Use the configured proxy for supported outbound traffic."],
  default_proxy_protocol: ["Proxy protocol", "Protocol used by the default proxy."],
  default_proxy_host: ["Proxy host", "Host or IP address of the default proxy."],
  default_proxy_port: ["Proxy port", "Port of the default proxy."],
  default_proxy_username: ["Proxy username", "Optional proxy username."],
  default_proxy_password: ["Proxy password", "Optional proxy password. Masked values keep the current secret."],
  default_proxy_bypass: ["Proxy bypass", "Hosts or patterns that should not use the proxy."],
  default_proxy_scope_maclaw: ["Proxy MaClaw", "Whether MaClaw app requests use the default proxy."],
  default_proxy_scope_agent: ["Proxy agents", "Whether agent/tool requests use the default proxy."]
});
Object.assign(FIELD_I18N.zh, {
  language: ["\u8bed\u8a00", "\u9996\u9009\u754c\u9762\u8bed\u8a00\u3002"],
  screen_dim_timeout_min: ["\u5c4f\u5e55\u53d8\u6697\u65f6\u95f4", "\u5c4f\u5e55\u53d8\u6697\u504f\u597d\u7684\u5206\u949f\u6570\uff0c0 \u8868\u793a\u5173\u95ed\u3002"],
  ui_mode: ["\u754c\u9762\u6a21\u5f0f", "\u7f51\u9875\u754c\u9762\u7684\u663e\u793a\u5bc6\u5ea6\u3002"],
  ui_zoom_factor: ["\u754c\u9762\u7f29\u653e", "\u754c\u9762\u663e\u793a\u6bd4\u4f8b\u3002"],
  show_tray_icon: ["\u663e\u793a\u6258\u76d8\u56fe\u6807", "\u652f\u6301\u65f6\u5728\u7cfb\u7edf\u6258\u76d8\u663e\u793a MaClaw\u3002"],
  default_proxy_enabled: ["\u9ed8\u8ba4\u4ee3\u7406", "\u652f\u6301\u7684\u51fa\u7ad9\u8bf7\u6c42\u4f7f\u7528\u4ee3\u7406\u3002"],
  default_proxy_protocol: ["\u4ee3\u7406\u534f\u8bae", "\u9ed8\u8ba4\u4ee3\u7406\u4f7f\u7528\u7684\u534f\u8bae\u3002"],
  default_proxy_host: ["\u4ee3\u7406\u5730\u5740", "\u9ed8\u8ba4\u4ee3\u7406\u7684 Host \u6216 IP\u3002"],
  default_proxy_port: ["\u4ee3\u7406\u7aef\u53e3", "\u9ed8\u8ba4\u4ee3\u7406\u7684\u7aef\u53e3\u3002"],
  default_proxy_username: ["\u4ee3\u7406\u7528\u6237\u540d", "\u53ef\u9009\u7684\u4ee3\u7406\u7528\u6237\u540d\u3002"],
  default_proxy_password: ["\u4ee3\u7406\u5bc6\u7801", "\u53ef\u9009\u7684\u4ee3\u7406\u5bc6\u7801\uff0c\u63a9\u7801\u503c\u4f1a\u4fdd\u7559\u73b0\u6709\u5bc6\u7801\u3002"],
  default_proxy_bypass: ["\u4ee3\u7406\u8df3\u8fc7\u5217\u8868", "\u4e0d\u8d70\u4ee3\u7406\u7684\u4e3b\u673a\u6216\u5339\u914d\u89c4\u5219\u3002"],
  default_proxy_scope_maclaw: ["MaClaw \u4f7f\u7528\u4ee3\u7406", "MaClaw \u5e94\u7528\u8bf7\u6c42\u662f\u5426\u4f7f\u7528\u9ed8\u8ba4\u4ee3\u7406\u3002"],
  default_proxy_scope_agent: ["Agent \u4f7f\u7528\u4ee3\u7406", "Agent/\u5de5\u5177\u8bf7\u6c42\u662f\u5426\u4f7f\u7528\u9ed8\u8ba4\u4ee3\u7406\u3002"]
});
const locale = requestedLocale.startsWith("en") ? "en" : "zh";
const t = (key, vars = {}) => Object.entries(vars).reduce((s, [k, v]) => s.replaceAll(`{${k}}`, String(v)), (I18N[locale] || I18N.zh)[key] || key);
function fieldMeta(def = {}) { const tr = FIELD_I18N[locale]?.[def.key]; return tr ? { ...def, title: tr[0], description: tr[1] } : def; }
function configTypeName(type) { if (locale !== "zh") return type; return type === "integer" ? "整数" : type === "number" ? "数字" : type; }
function configIssueLabel(issue = {}) { const key = String(issue.key || ""); const base = key.split(".")[0]; const meta = fieldMeta({ key: base, title: base }); const suffix = key.includes(".") ? ` / ${key.split(".").slice(1).join(".")}` : ""; return `${meta.title || key}${suffix}`; }
function configIssueMessage(issue = {}) { const msg = String(issue.message || ""); if (locale !== "zh") return msg; const key = issue.key || ""; if (msg.includes("managed-by-hub")) return "仍然使用 VE Platform managed-by-hub 占位符，请从 VE Platform 重新打开并传入 Hub LLM 地址和 viewer token。"; if (msg.includes("Selected provider URL is required") || msg.includes("URL is required")) return "必须填写 LLM 服务地址。"; if (msg.includes("API key is required") || msg.includes("credential is required")) return "必须填写 LLM 访问令牌。"; if (msg.includes("Selected provider model is required") || msg.includes("model is required")) return "必须填写 LLM 模型；接入 VE Platform Hub 时填写 auto。"; if (msg.includes("selected provider") && msg.includes("was not found")) return "当前服务商不在 LLM 服务商列表中。"; if (key === "maclaw_llm_current_provider") return msg.replace("maclaw_llm_current_provider is required when multiple providers are configured", "配置多个服务商时必须选择当前服务商"); return msg; }
const TOKEN_REFRESH_GRACE_MS = 2 * 60 * 1000;
const TOKEN_REFRESH_ACTIVITY_WINDOW_MS = 10 * 60 * 1000;
const TOKEN_REFRESH_RECHECK_MS = 60 * 1000;
const state = { token: "", tokenExpiresAt: 0, tokenRefreshTimer: 0, tokenRefreshPromise: null, lastActivityAt: 0, me: null, instances: [], sessions: [], messages: [], view: "assistant", instanceId: "", sessionId: "", config: null, schema: [], skills: [], skillResults: [], skillQuery: "", skillPage: 1, mcpServers: [], mcpMarketResults: [], mcpMarketQuery: "", mcpAddMode: "market", mcpEditingID: "", memoryItems: [], memoryNextOffset: 0, memoryHasMore: false, memoryLoading: false, memorySaving: false, memoryReloadPending: false, memorySearchTimer: 0, migrationStatus: null, migrationInstances: [], migrationLoading: false, migrationJob: null, migrationJobTimer: 0, knowledgeBatchPage: 1, knowledgeBatchTotal: 0, imAuditItems: [], imAuditContacts: [], imAuditStats: null, imAuditCleanupBefore: "", imAuditContactsPlatform: null, imAuditHasMore: false, imAuditNextBefore: "", imAuditLoading: false, imAuditLoaded: false, imAuditPlatform: "", imAuditQuery: "", imAuditContact: "", imAuditOpen: false, imSubTab: "qq", imRuntimes: {}, weixinRuntime: null, weixinQRCodeURL: "", weixinQRToken: "", weixinQRStatus: "", weixinQRMessage: "", weixinQRPollTimer: 0, imStartingKey: "", settingsTab: "", busy: false, currentRun: null, runStream: null, copySnippets: [], hiddenMessages: {} };
const saved = sessionStorage.getItem("maclaw.user.token") || "";
const savedExpiry = sessionStorage.getItem("maclaw.user.token_expires_at") || "";
const launchToken = params.get("launch_token") || "";
const hasLaunchToken = params.has("launch_token");
const secretURLKeys = ["token", "access_token", "api_key", "api_secret"];
const rawURLSecret = secretURLKeys.some((key) => params.has(key) || location.hash.toLowerCase().includes(`${key}=`));
state.token = hasLaunchToken || rawURLSecret ? "" : saved;
state.view = ["assistant", "knowledge", "settings"].includes(params.get("view")) ? params.get("view") : "assistant";
state.instanceId = params.get("instance_id") || "";
function normalizeSettingsTab(tab) {
  tab = String(tab || "").trim().toLowerCase();
  if (tab === "channels" || tab === "channels_more") return "im";
  if (tab === "ui") return "interface";
  if (tab === "runtime" || tab === "pet" || tab === "startup" || tab === "local_runtime") return "";
  if (tab === "advanced") return "";
  return ["llm", "tools", "skills", "memory", "migration", "security", "im", "interface", "proxy"].includes(tab) ? tab : "";
}
state.settingsTab = normalizeSettingsTab(params.get("settings_tab"));
if (rawURLSecret || hasLaunchToken) {
  secretURLKeys.forEach((key) => params.delete(key));
  params.delete("launch_token");
  const next = location.pathname + (params.toString() ? `?${params}` : "");
  history.replaceState(null, "", next);
}

function esc(v) { return String(v ?? "").replace(/[&<>'"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", "'": "&#39;", '"': "&quot;" }[c])); }
function pretty(v) { return JSON.stringify(v, null, 2); }
function setBusy(on) { state.busy = on; document.body.classList.toggle("is-fetching", on); $("statusBadge").textContent = on ? t("busy") : t("ready"); }
function toast(msg) { const el = $("toast"); el.textContent = msg; el.classList.remove("hidden"); clearTimeout(toast.timer); toast.timer = setTimeout(() => el.classList.add("hidden"), 3600); }
function stopTokenRefreshTimer() { if (state.tokenRefreshTimer) { clearTimeout(state.tokenRefreshTimer); state.tokenRefreshTimer = 0; } }
function parseTokenExpiry(expiresAt) { const ms = Date.parse(String(expiresAt || "")); return Number.isFinite(ms) ? ms : 0; }
if (state.token) state.tokenExpiresAt = parseTokenExpiry(savedExpiry);
function tokenExpiresInMs() { return state.tokenExpiresAt ? state.tokenExpiresAt - Date.now() : 0; }
function hasRecentUserActivity() { return !!state.lastActivityAt && (Date.now() - state.lastActivityAt) <= TOKEN_REFRESH_ACTIVITY_WINDOW_MS; }
function onTokenRefreshTimer() {
  state.tokenRefreshTimer = 0;
  if (!state.token || !state.tokenExpiresAt) return;
  const untilExpiry = tokenExpiresInMs();
  if (untilExpiry <= 0) return;
  if (!hasRecentUserActivity()) {
    scheduleTokenRefresh(Math.min(Math.max(untilExpiry - 1000, 1000), TOKEN_REFRESH_RECHECK_MS));
    return;
  }
  void refreshAccessToken("timer").catch((e) => {
    if (e?.status !== 401 && state.token) {
      scheduleTokenRefresh(Math.min(Math.max(tokenExpiresInMs() - 1000, 1000), TOKEN_REFRESH_RECHECK_MS));
    }
  });
}
function scheduleTokenRefresh(delayMs) {
  stopTokenRefreshTimer();
  if (!state.token || !state.tokenExpiresAt) return;
  const untilExpiry = tokenExpiresInMs();
  if (untilExpiry <= 0) return;
  const delay = typeof delayMs === "number" ? delayMs : Math.max(untilExpiry - TOKEN_REFRESH_GRACE_MS, 1000);
  state.tokenRefreshTimer = window.setTimeout(onTokenRefreshTimer, delay);
}
function applyAccessToken(token, expiresAt) {
  state.token = String(token || "").trim();
  state.tokenExpiresAt = parseTokenExpiry(expiresAt);
  if (state.token) {
    sessionStorage.setItem("maclaw.user.token", state.token);
    if (state.tokenExpiresAt) sessionStorage.setItem("maclaw.user.token_expires_at", new Date(state.tokenExpiresAt).toISOString());
    else sessionStorage.removeItem("maclaw.user.token_expires_at");
  } else {
    sessionStorage.removeItem("maclaw.user.token");
    sessionStorage.removeItem("maclaw.user.token_expires_at");
  }
  scheduleTokenRefresh();
}
function clearAccessToken() {
  resetRunState(); resetWeixinQRLogin(); sessionStorage.removeItem("maclaw.user.token"); sessionStorage.removeItem("maclaw.user.token_expires_at");
  stopTokenRefreshTimer();
  state.tokenRefreshPromise = null;
  state.token = "";
  state.tokenExpiresAt = 0;
  state.lastActivityAt = 0;
}
async function refreshAccessToken(reason = "activity", force = false) {
  if (!state.token) throw new Error("missing launch token");
  if (state.tokenRefreshPromise) return state.tokenRefreshPromise;
  if (!force && state.tokenExpiresAt && tokenExpiresInMs() > TOKEN_REFRESH_GRACE_MS) return { skipped: true, reason };
  state.tokenRefreshPromise = (async () => {
    const resp = await fetch("/api/v1/web/refresh", { method: "POST", headers: headers(false) });
    const text = await resp.text();
    let data = {};
    if (text) { try { data = JSON.parse(text); } catch { data = { raw: text }; } }
    if (!resp.ok) {
      const err = new Error(apiErrorMessage(data, `${resp.status} ${resp.statusText}`));
      err.status = resp.status;
      if (resp.status === 401) clearAccessToken();
      throw err;
    }
    if (!data.access_token) throw new Error("missing refreshed access token");
    applyAccessToken(data.access_token || "", data.expires_at);
    return data;
  })();
  try {
    return await state.tokenRefreshPromise;
  } finally {
    state.tokenRefreshPromise = null;
  }
}
function markUserActivity() {
  state.lastActivityAt = Date.now();
  if (state.token && state.tokenExpiresAt && tokenExpiresInMs() <= TOKEN_REFRESH_GRACE_MS) void refreshAccessToken("activity");
}
function headers(json = true) { const h = { Authorization: `Bearer ${state.token}` }; if (json) h["Content-Type"] = "application/json"; return h; }
function apiErrorMessage(data, fallback) {
  const msg = data.error || data.message || fallback;
  const text = `${msg} ${data.raw || ""}`.toLowerCase();
  if (text.includes("qrcode token is not active")) return t("weixinQRExpired");
  return text.includes("managed-by-hub") || text.includes("viewer authentication failed") || text.includes("unauthorized") ? t("llmManagedByHub") : msg;
}
async function api(path, opt = {}) {
  if (!state.token) throw new Error("missing launch token");
  const resp = await fetch(path, { ...opt, headers: { ...headers(opt.body !== undefined), ...(opt.headers || {}) } });
  const text = await resp.text();
  let data = {};
  if (text) { try { data = JSON.parse(text); } catch { data = { raw: text }; } }
  if (!resp.ok) {
    const err = new Error(apiErrorMessage(data, `${resp.status} ${resp.statusText}`));
    err.status = resp.status;
    if (resp.status === 401) clearAccessToken();
    throw err;
  }
  return data;
}
async function authorizedObjectURL(path) {
  if (!state.token) throw new Error("missing launch token");
  const resp = await fetch(path, { headers: headers(false) });
  if (!resp.ok) throw new Error(`${resp.status} ${resp.statusText}`);
  return URL.createObjectURL(await resp.blob());
}
function closeRunStream() { if (state.runStream) { state.runStream.abort(); state.runStream = null; } }
function resetRunState() { closeRunStream(); state.currentRun = null; }
async function exchangeLaunchToken() {
  if (state.token || !launchToken) return;
  const resp = await fetch("/api/v1/web/exchange", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ launch_token: launchToken }) });
  const text = await resp.text();
  let data = {};
  if (text) { try { data = JSON.parse(text); } catch { data = { raw: text }; } }
  if (!resp.ok) { const err = new Error(data.error || data.message || `${resp.status} ${resp.statusText}`); err.status = resp.status; throw err; }
  if (!data.access_token) throw new Error("missing exchanged access token");
  applyAccessToken(data.access_token, data.expires_at);
}
function items(resp) { return Array.isArray(resp?.items) ? resp.items : Array.isArray(resp) ? resp : []; }
function activeInstance() { return state.instanceId ? (state.instances.find((x) => x.id === state.instanceId) || null) : (state.instances[0] || null); }
function selectedInstanceMissing() { return !!state.instanceId && !state.instances.some((x) => x.id === state.instanceId); }
function panelMessageKey() { return `${state.instanceId || "default"}:${state.sessionId || "new"}`; }
function messageIdentity(m) { return String(m?.id || "").trim(); }
function hiddenMessageSet(key = panelMessageKey()) { return state.hiddenMessages[key] || (state.hiddenMessages[key] = new Set()); }
function visibleMessages(messages) { const hidden = hiddenMessageSet(); return items(messages).filter((m) => !hidden.has(messageIdentity(m))); }
function setTitle(title, hint) { $("pageTitle").textContent = title; $("pageHint").textContent = hint; document.title = `${title} - MaClawSrv`; }
function initChrome() { document.documentElement.lang = locale === "zh" ? "zh-CN" : "en"; document.querySelector(".skip-link").textContent = t("skipToMain"); document.querySelector(".sidebar").setAttribute("aria-label", t("appSections")); document.querySelector(".nav").setAttribute("aria-label", t("userViews")); $("brandSubtitle").textContent = t("userWorkspace"); document.querySelector('[data-view="assistant"]').textContent = t("assistantNav"); document.querySelector('[data-view="knowledge"]').textContent = t("knowledgeNav"); document.querySelector('[data-view="settings"]').textContent = t("settingsNav"); $("logoutBtn").textContent = t("logout"); if (!state.me) $("identity").textContent = t("notSignedIn"); setBusy(state.busy); }
function updateNav() { document.querySelectorAll("[data-view]").forEach((b) => { const on = b.dataset.view === state.view; b.classList.toggle("active", on); b.setAttribute("aria-current", on ? "page" : "false"); }); }

async function bootstrap() {
  try {
    initChrome();
    setBusy(true);
    if (rawURLSecret) {
      clearAccessToken();
      renderMissingToken(t("rawSecretRejected"));
      return;
    }
    await exchangeLaunchToken();
    if (!state.token) { renderMissingToken(); return; }
    await refreshAccessToken("bootstrap", !state.tokenExpiresAt);
    const [me, inst] = await Promise.all([api("/api/v1/me"), api("/api/v1/instances")]);
    state.me = me;
    state.instances = items(inst);
    if (!state.instanceId && state.instances[0]) state.instanceId = state.instances[0].id;
    $("identity").textContent = `${me.email || me.user_id || "user"}`;
    await render();
  } catch (e) { if (e.status === 401) renderMissingToken(t("sessionExpired")); else renderError(e); }
  finally { setBusy(false); }
}
async function render() { updateNav(); if (state.view === "settings") return renderSettings(); if (state.view === "knowledge") return renderKnowledge(); return renderAssistant(); }
function renderMissingToken(message = t("missingToken")) { setTitle(t("loginRequired"), t("loginHint")); $("content").innerHTML = `<section class="panel stack"><h2>${t("cannotStart")}</h2><p class="error">${esc(message)}</p></section>`; }
function renderError(e) { $("content").innerHTML = `<section class="panel stack"><h2>${t("loadFailed")}</h2><p class="error">${esc(e.message)}</p><button id="retryBtn" type="button" class="primary">${t("retry")}</button></section>`; $("retryBtn").onclick = bootstrap; }
function handleAPIError(e) { if (e && e.status === 401) { renderMissingToken(t("sessionExpired")); return true; } return false; }
async function refreshInstances() {
  const out = await api("/api/v1/instances");
  state.instances = items(out);
  if (state.instanceId && !state.instances.some((x) => x.id === state.instanceId)) state.instanceId = state.instances[0]?.id || "";
  if (!state.instanceId && state.instances[0]) state.instanceId = state.instances[0].id;
  return state.instances;
}

async function renderAssistant() {
  setTitle(t("assistantTitle"), t("assistantHint"));
  const inst = activeInstance();
  if (inst) state.instanceId = inst.id;
  $("content").innerHTML = `<div class="assistant-layout"><section class="panel stack assistant-rail"><div class="split"><div><h2>${t("instancesTitle")}</h2><p class="helper">${t("instancesHint")}</p></div><button id="newInst" type="button" class="primary">${t("new")}</button></div><div id="instanceList" class="list"></div><div id="sessionList" class="list"></div></section><section class="card chat"><div id="runPanel" class="run-panel hidden"></div><div class="chat-toolbar"><span class="muted">${t("webSession")}</span><button id="clearPanel" type="button" class="secondary clear-panel-btn">${clearContentLabel()}</button></div><div class="messages-wrap"><div id="messages" class="messages"></div><button id="jumpLatest" type="button" class="jump-latest hidden">${latestLabel()}</button></div><form id="composer" class="composer"><textarea id="prompt" placeholder="${t("typeMessage")}" aria-label="${t("message")}"></textarea><div class="composer-actions"><input id="voiceFile" type="file" accept="audio/wav,audio/ogg,audio/opus,audio/mpeg,audio/aac,audio/mp4,.wav,.ogg,.opus,.oga,.silk,.mp3,.aac,.m4a" hidden><button id="voiceBtn" type="button" class="secondary">${transcribeLabel()}</button><button id="sendBtn" type="submit" class="primary">${t("send")}</button></div></form></section></div>`;
  renderInstanceList();
  $("newInst").onclick = createInstance;
  $("clearPanel").onclick = clearPanelContent;
  $("composer").onsubmit = sendMessage;
  bindVoiceComposer();
  bindComposerKeys();
  if (inst) await loadSessionsAndMessages();
  else if (selectedInstanceMissing()) {
    $("prompt").disabled = true;
    $("sendBtn").disabled = true;
    renderEmptyChat(t("selectedMissing"), true);
  } else renderEmptyChat(t("createFirst"));
}
async function renderKnowledge() {
  setTitle(t("knowledgeTitle"), t("knowledgeHint"));
  $("content").innerHTML = `<section class="panel stack knowledge-page">${renderKnowledgeQuery()}${renderKnowledgeImporter()}</section>`;
  bindKnowledgeQuery();
  bindKnowledgeImporter();
  loadKnowledgeAccessSummary();
  loadKnowledgeImportBatches();
}
function renderKnowledgeQuery() {
  return `<div class="knowledge-search" role="search" aria-label="${esc(t("knowledgeQuery"))}"><div class="knowledge-section-head"><strong>${esc(t("knowledgeQuery"))}</strong><span class="helper">${esc(t("knowledgeQueryHint"))}</span></div><form id="knowledgeSearchForm" class="knowledge-search-form"><label class="knowledge-search-main" for="knowledgeQueryText">${esc(t("knowledgeQuery"))}<input id="knowledgeQueryText" type="search" placeholder="${esc(t("knowledgeQueryPlaceholder"))}" autocomplete="off"></label><label class="knowledge-search-limit" for="knowledgeQueryLimit">${esc(t("knowledgeLimit"))}<select id="knowledgeQueryLimit"><option value="5">5</option><option value="8" selected>8</option><option value="12">12</option><option value="20">20</option></select></label><button id="knowledgeSearchBtn" type="submit" class="primary">${esc(t("search"))}</button></form><div id="knowledgeSearchResults" class="knowledge-result-list" aria-live="polite"></div></div>`;
}
function bindKnowledgeQuery() {
  const form = $("knowledgeSearchForm");
  if (!form) return;
  form.onsubmit = async (e) => {
    e.preventDefault();
    await searchKnowledge();
  };
}
async function searchKnowledge() {
  const query = $("knowledgeQueryText")?.value?.trim() || "";
  const out = $("knowledgeSearchResults");
  if (!query) return showKnowledgeFieldError("knowledgeQueryText", t("knowledgeQueryPlaceholder"));
  clearKnowledgeFieldError("knowledgeQueryText");
  try {
    $("knowledgeSearchBtn").disabled = true;
    out.innerHTML = `<div class="muted">${esc(t("loading"))}</div>`;
    const rawLimit = Number($("knowledgeQueryLimit")?.value || 8);
    const limit = [5, 8, 12, 20].includes(rawLimit) ? rawLimit : 8;
    const resp = await api("/api/v1/knowledge/search", { method: "POST", body: JSON.stringify({ query, limit }) });
    out.innerHTML = renderKnowledgeResults(items(resp.results || resp));
  } catch (e) {
    if (!handleAPIError(e)) out.innerHTML = `<div class="error">${esc(t("knowledgeQueryFailed"))}: ${esc(e.message)}</div>`;
  } finally {
    const btn = $("knowledgeSearchBtn");
    if (btn) btn.disabled = false;
  }
}
function renderKnowledgeResults(results) {
  if (!results.length) return `<div class="muted">${esc(t("knowledgeNoResults"))}</div>`;
  return results.map((r) => {
    const source = r.source || {};
    const title = r.node_title || r.card_title || source.title || r.subject || r.claim || r.citation || source.id || r.node_id || r.card_id || r.fact_id || t("unknown");
    const text = r.snippet || r.summary || r.claim || [r.subject, r.predicate, r.object].filter(Boolean).join(" ");
    const hasScore = r.score !== undefined && r.score !== null && String(r.score).trim() !== "";
    const score = Number(r.score);
    const meta = [
      source.kind || r.result_type ? `${t("knowledgeResultType")}: ${source.kind || r.result_type}` : "",
      hasScore && Number.isFinite(score) ? `${t("knowledgeResultScore")}: ${score.toFixed(2)}` : "",
      source.id ? `${t("importSource")}: ${source.id}` : "",
      r.citation || ""
    ].filter(Boolean).join(" / ");
    return `<article class="knowledge-result"><h3>${esc(title)}</h3>${text ? `<p>${esc(text)}</p>` : ""}${meta ? `<small>${esc(meta)}</small>` : ""}</article>`;
  }).join("");
}
function bindComposerKeys() {
  const prompt = $("prompt");
  if (!prompt) return;
  const sync = () => { autoResizePrompt(); updateSendButtonState(); };
  prompt.oninput = sync;
  prompt.onkeydown = (e) => {
    if (e.key !== "Enter" || e.shiftKey || e.ctrlKey || e.altKey || e.metaKey || e.isComposing) return;
    e.preventDefault();
    $("composer")?.requestSubmit();
  };
  sync();
}
function bindVoiceComposer() {
  const input = $("voiceFile");
  const btn = $("voiceBtn");
  if (!input || !btn) return;
  btn.onclick = () => input.click();
  input.onchange = async () => {
    const file = input.files?.[0];
    input.value = "";
    if (file) await transcribeVoiceFile(file, btn);
  };
}
async function transcribeVoiceFile(file, btn) {
  if (!file) return;
  const prev = btn?.textContent || transcribeLabel();
  if (btn) { btn.disabled = true; btn.textContent = transcribingLabel(); }
  try {
    const audioFormat = voiceUploadFormat(file);
    const resp = await fetch("/api/v1/ai-models/asr/transcribe", { method: "POST", headers: { Authorization: `Bearer ${state.token}`, "Content-Type": voiceUploadContentType(file), "X-MaClaw-Audio-Format": audioFormat }, body: file });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) {
      const err = new Error(apiErrorMessage(data, `${resp.status} ${resp.statusText}`));
      err.status = resp.status;
      throw err;
    }
    const text = String(data.text || "").trim();
    const prompt = $("prompt");
    if (prompt && text) {
      prompt.value = [prompt.value.trim(), text].filter(Boolean).join(prompt.value.trim() ? "\n" : "");
      prompt.dispatchEvent(new Event("input", { bubbles: true }));
      prompt.focus();
      toast(voiceReadyLabel());
    }
  } catch (e) {
    if (!handleAPIError(e)) toast(e.message);
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = prev; }
  }
}
function voiceUploadContentType(file) {
  const inferred = voiceUploadFormat(file);
  const explicit = String(file?.type || "").trim().toLowerCase();
  if (explicit && explicit !== "application/octet-stream" && explicit !== "binary/octet-stream") return explicit;
  if (inferred === "mp3") return "audio/mpeg";
  if (inferred === "ogg") return "audio/ogg";
  if (inferred === "wav") return "audio/wav";
  if (inferred === "aac") return "audio/aac";
  if (inferred === "m4a") return "audio/mp4";
  if (inferred === "silk") return "application/octet-stream";
  return explicit || "application/octet-stream";
}
function voiceUploadFormat(file) {
  const name = String(file?.name || "").toLowerCase();
  if (name.endsWith(".mp3")) return "mp3";
  if (name.endsWith(".ogg") || name.endsWith(".oga") || name.endsWith(".opus")) return "ogg";
  if (name.endsWith(".wav")) return "wav";
  if (name.endsWith(".silk")) return "silk";
  if (name.endsWith(".aac")) return "aac";
  if (name.endsWith(".m4a")) return "m4a";
  return "";
}
function autoResizePrompt() { const el = $("prompt"); if (!el) return; el.style.height = "auto"; el.style.height = `${Math.min(el.scrollHeight, 180)}px`; }
function updateSendButtonState() { const btn = $("sendBtn"); const prompt = $("prompt"); if (btn && prompt) btn.disabled = !prompt.value.trim() || prompt.disabled; }
function renderInstanceList() {
  const box = $("instanceList");
  box.innerHTML = state.instances.map((i) => `<button type="button" class="instance ${i.id === state.instanceId ? "active" : ""}" data-instance="${esc(i.id)}"><strong>${esc(i.name || i.id)}</strong><span class="muted">${esc(i.status || t("unknown"))} · ${i.ready ? t("readyState") : t("notReady")}</span><span class="pill">${esc(i.id)}</span></button>`).join("") || `<div class="muted">${t("noInstances")}</div>`;
  box.querySelectorAll("[data-instance]").forEach((b) => b.onclick = async () => { if (state.instanceId !== b.dataset.instance) resetRunState(); state.instanceId = b.dataset.instance; state.sessionId = ""; renderInstanceList(); await loadSessionsAndMessages(); });
}
async function createInstance() {
  const name = `web-assistant-${String(state.instances.length + 1).padStart(2, "0")}`;
  try {
    setBusy(true);
    const inst = await api("/api/v1/instances", { method: "POST", body: JSON.stringify({ name, description: "VE Platform user web assistant", metadata: { channel: "ve-platform-web" } }) });
    state.instances.unshift(inst); state.instanceId = inst.id; state.sessionId = "";
    await renderAssistant(); toast(t("instanceCreated"));
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
  finally { setBusy(false); }
}
async function loadSessionsAndMessages() {
  const inst = activeInstance();
  if (!inst) return;
  try {
    const sessions = await api(`/api/v1/instances/${encodeURIComponent(inst.id)}/sessions?limit=20`);
    state.sessions = items(sessions);
    if (!state.sessionId && state.sessions[0]) state.sessionId = state.sessions[0].id;
    renderSessions();
    if (state.sessionId) {
      const msgs = await api(`/api/v1/instances/${encodeURIComponent(inst.id)}/sessions/${encodeURIComponent(state.sessionId)}/messages?limit=100`);
      state.messages = visibleMessages(msgs); renderMessages();
    } else { renderEmptyChat(t("firstMessage")); }
  } catch (e) { if (!handleAPIError(e)) renderEmptyChat(e.message, true); }
}
function renderSessions() {
  const box = $("sessionList");
  box.innerHTML = `<h3>${t("sessions")}</h3>` + (state.sessions.map((s) => `<button type="button" class="instance ${s.id === state.sessionId ? "active" : ""}" data-session="${esc(s.id)}"><strong>${esc(s.title || s.id)}</strong><span class="muted">${esc(s.status || "active")}</span></button>`).join("") || `<div class="muted">${t("noSessions")}</div>`);
  box.querySelectorAll("[data-session]").forEach((b) => b.onclick = async () => { if (state.sessionId !== b.dataset.session) resetRunState(); state.sessionId = b.dataset.session; await loadSessionsAndMessages(); });
}
function upsertSession(session) {
  if (!session?.id) return;
  const idx = state.sessions.findIndex((s) => s.id === session.id);
  if (idx >= 0) state.sessions[idx] = session; else state.sessions.unshift(session);
  renderSessions();
}
function renderEmptyChat(text, isError = false) { state.messages = []; state.copySnippets = []; updateJumpLatestButton(false); $("messages").innerHTML = `<div class="message assistant ${isError ? "error" : ""}">${esc(text)}</div>`; }
function clearPanelContent() {
  resetRunState();
  const hidden = hiddenMessageSet();
  state.messages.map(messageIdentity).filter(Boolean).forEach((id) => hidden.add(id));
  state.messages = [];
  state.copySnippets = [];
  renderRunPanel(null);
  updateJumpLatestButton(false);
  const box = $("messages");
  if (box) box.innerHTML = `<div class="message assistant">${t("noMessages")}</div>`;
  toast(contentClearedLabel());
}
function addThinkingPlaceholder(runId = "") {
  removeThinkingPlaceholders();
  state.messages.push({ id: `local-thinking-${Date.now()}`, role: "assistant", content: thinkingLabel(), created_at: new Date().toISOString(), local_thinking: true, run_id: runId });
  renderMessages(true);
}
function removeThinkingPlaceholders() {
  state.messages = state.messages.filter((m) => !m.local_thinking);
}
function removeThinkingPlaceholdersAndRender() {
  const before = state.messages.length;
  removeThinkingPlaceholders();
  if (state.messages.length !== before) renderMessages();
}
function replaceLocalMessage(localId, message) {
  if (!message?.id) return;
  const idx = state.messages.findIndex((m) => m.id === localId);
  if (idx >= 0) state.messages[idx] = message;
  else if (!state.messages.some((m) => m.id === message.id)) state.messages.push(message);
  renderMessages();
}
function messageDetails(m) {
  const meta = m.metadata || {};
  const hasTool = meta.tool_name || meta.tool_call || meta.tool_result || meta.tool_call_id || (m.role === "system" && Object.keys(meta).length);
  if (!hasTool) return "";
  return `<details class="tool-detail"><summary>${esc(meta.tool_name || meta.tool_call || m.role || "tool")}</summary><pre>${esc(pretty(meta))}</pre></details>`;
}
function splitURLTrailingPunctuation(url) {
  let body = String(url || "");
  let tail = "";
  const open = { ")": "(", "]": "[", "}": "{" };
  const count = (s, ch) => (s.match(new RegExp(`\\${ch}`, "g")) || []).length;
  while (body) {
    const ch = body.at(-1);
    if (/[.,;:!?，。；：！？]/.test(ch)) { tail = ch + tail; body = body.slice(0, -1); continue; }
    if (open[ch] && count(body, ch) > count(body, open[ch])) { tail = ch + tail; body = body.slice(0, -1); continue; }
    break;
  }
  return [body, tail];
}
function renderExternalLink(href, label) {
  const safeHref = esc(href);
  return `<a href="${safeHref}" target="_blank" rel="noopener noreferrer">${label}</a>`;
}
function restoreInlineTokens(html, tokens) {
  return tokens.reduce((out, token, idx) => out.replace(`\u0000${idx}\u0000`, token), html);
}
function renderInlineMarkdown(text) {
  const tokens = [];
  let html = esc(text).replace(/`([^`]+)`/g, (_m, code) => {
    const key = `\u0000${tokens.length}\u0000`;
    tokens.push(`<code>${code}</code>`);
    return key;
  });
  html = html.replace(/\[([^\]]+)\]\((https?:\/\/[^\s)]+)\)/g, (_m, label, href) => {
    const key = `\u0000${tokens.length}\u0000`;
    tokens.push(renderExternalLink(href, label));
    return key;
  });
  html = html
    .replace(/(^|\s)(https?:\/\/[^\s<]+)/g, (_m, prefix, rawURL) => { const [url, tail] = splitURLTrailingPunctuation(rawURL); return `${prefix}${renderExternalLink(url, url)}${tail}`; })
    .replace(/\*\*([^*]+)\*\*/g, `<strong>$1</strong>`)
    .replace(/(^|[^*])\*([^*\s][^*]*?)\*/g, `$1<em>$2</em>`);
  return restoreInlineTokens(html, tokens);
}
function renderMarkdownParagraph(lines) {
  return `<p>${renderInlineMarkdown(lines.join("\n"))}</p>`;
}
function splitMarkdownTableRow(line) {
  const trimmed = line.trim().replace(/^\|/, "").replace(/\|$/, "");
  const cells = [];
  let cell = "";
  let escaped = false;
  for (const ch of trimmed) {
    if (escaped) { cell += ch === "|" ? "|" : `\\${ch}`; escaped = false; continue; }
    if (ch === "\\") { escaped = true; continue; }
    if (ch === "|") { cells.push(cell.trim()); cell = ""; continue; }
    cell += ch;
  }
  if (escaped) cell += "\\";
  cells.push(cell.trim());
  return cells;
}
function isMarkdownTableRow(line) {
  if (!line.includes("|")) return false;
  if (isMarkdownTableDivider(line)) return false;
  const cells = splitMarkdownTableRow(line);
  return cells.length >= 3 && cells.some(Boolean) && cells.filter((cell) => cell.length).length >= 2;
}
function splitFlattenedMarkdownTable(line) {
  if (!isMarkdownTableRow(line)) return [];
  const cells = splitMarkdownTableRow(line);
  const headerEnd = cells.findIndex((cell, idx) => idx > 0 && /^\d+$/.test(cell));
  if (headerEnd < 3 || !/^(#|\u5e8f\u53f7|\u7f16\u53f7)$/i.test(cells[0])) return [];
  const header = cells.slice(0, headerEnd);
  const data = cells.slice(headerEnd);
  if (!data.length || data.length % header.length !== 0) return [];
  const rows = [header];
  for (let i = 0; i < data.length; i += header.length) rows.push(data.slice(i, i + header.length));
  return rows;
}
function isMarkdownTableDivider(line) {
  return /^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)+\|?\s*$/.test(line);
}
function renderMarkdownTable(rows) {
  const cellsForRow = (row) => Array.isArray(row) ? row : splitMarkdownTableRow(row);
  const header = cellsForRow(rows[0]);
  const hasDivider = rows.length > 1 && isMarkdownTableDivider(rows[1]);
  const body = rows.slice(hasDivider ? 2 : 1).map(cellsForRow);
  const head = `<thead><tr>${header.map((cell) => `<th>${renderInlineMarkdown(cell)}</th>`).join("")}</tr></thead>`;
  const bodyHtml = body.length ? `<tbody>${body.map((row) => `<tr>${header.map((_cell, idx) => `<td>${renderInlineMarkdown(row[idx] || "")}</td>`).join("")}</tr>`).join("")}</tbody>` : "";
  return `<div class="md-table-wrap"><table>${head}${bodyHtml}</table></div>`;
}
function renderMarkdown(text) {
  const snippets = arguments[1] || [];
  const src = String(text || "").replace(/\r\n/g, "\n").replace(/\r/g, "\n");
  const lines = src.split("\n");
  const out = [];
  let paragraph = [];
  const flushParagraph = () => { if (paragraph.length) { out.push(renderMarkdownParagraph(paragraph)); paragraph = []; } };
  for (let i = 0; i < lines.length; i++) {
    const line = lines[i];
    const fence = line.match(/^```\s*([^`]*)\s*$/);
    if (fence) {
      flushParagraph();
      const code = [];
      const lang = fence[1].trim().split(/\s+/)[0] || "code";
      i++;
      while (i < lines.length && !/^```\s*$/.test(lines[i])) code.push(lines[i++]);
      const copyIdx = snippets.push(code.join("\n")) - 1;
      out.push(`<div class="md-code-shell"><div class="md-code-head"><span>${esc(lang)}</span><button type="button" class="copy-btn" data-copy-code="${copyIdx}" aria-label="${esc(copyLabel())}">${esc(copyLabel())}</button></div><pre class="md-code"><code>${esc(code.join("\n"))}</code></pre></div>`);
      continue;
    }
    if (!line.trim()) { flushParagraph(); continue; }
    const heading = line.match(/^(#{1,3})\s+(.+)$/);
    if (heading) { flushParagraph(); out.push(`<h${heading[1].length}>${renderInlineMarkdown(heading[2])}</h${heading[1].length}>`); continue; }
    const flattenedRows = splitFlattenedMarkdownTable(line);
    if (flattenedRows.length) { flushParagraph(); out.push(renderMarkdownTable(flattenedRows)); continue; }
    if (isMarkdownTableRow(line) && i + 1 < lines.length && isMarkdownTableDivider(lines[i + 1])) {
      flushParagraph();
      const rows = [line, lines[i + 1]];
      i += 2;
      while (i < lines.length && isMarkdownTableRow(lines[i])) rows.push(lines[i++]);
      i--;
      out.push(renderMarkdownTable(rows));
      continue;
    }
    if (isMarkdownTableRow(line) && i + 1 < lines.length && isMarkdownTableRow(lines[i + 1])) {
      flushParagraph();
      const rows = [line];
      i++;
      while (i < lines.length && isMarkdownTableRow(lines[i])) rows.push(lines[i++]);
      i--;
      out.push(renderMarkdownTable(rows));
      continue;
    }
    if (/^---+$/.test(line.trim())) { flushParagraph(); out.push(`<hr>`); continue; }
    const quote = line.match(/^>\s?(.+)$/);
    if (quote) {
      flushParagraph();
      const items = [quote[1]];
      while (i + 1 < lines.length) {
        const next = lines[i + 1].match(/^>\s?(.+)$/);
        if (!next) break;
        items.push(next[1]); i++;
      }
      out.push(`<blockquote>${renderInlineMarkdown(items.join("\n"))}</blockquote>`);
      continue;
    }
    const bullet = line.match(/^\s*[-*+]\s+(.+)$/);
    if (bullet) {
      flushParagraph();
      const items = [bullet[1]];
      while (i + 1 < lines.length) {
        const next = lines[i + 1].match(/^\s*[-*+]\s+(.+)$/);
        if (!next) break;
        items.push(next[1]); i++;
      }
      out.push(`<ul>${items.map((item) => {
        const task = item.match(/^\[( |x|X)\]\s+(.+)$/);
        return task ? `<li class="task-list-item"><input type="checkbox" disabled ${task[1].toLowerCase() === "x" ? "checked" : ""}> ${renderInlineMarkdown(task[2])}</li>` : `<li>${renderInlineMarkdown(item)}</li>`;
      }).join("")}</ul>`);
      continue;
    }
    const ordered = line.match(/^\s*\d+[.)]\s+(.+)$/);
    if (ordered) {
      flushParagraph();
      const items = [ordered[1]];
      while (i + 1 < lines.length) {
        const next = lines[i + 1].match(/^\s*\d+[.)]\s+(.+)$/);
        if (!next) break;
        items.push(next[1]); i++;
      }
      out.push(`<ol>${items.map((item) => `<li>${renderInlineMarkdown(item)}</li>`).join("")}</ol>`);
      continue;
    }
    paragraph.push(line);
  }
  flushParagraph();
  return out.join("");
}
function messageCreatedAt(m) { const raw = m.created_at || m.createdAt || m.timestamp || ""; if (typeof raw === "number") return Number.isFinite(raw) ? raw : 0; const ts = Date.parse(raw); return Number.isFinite(ts) ? ts : 0; }
function messageRoleClass(role) { return ["user", "assistant", "system", "tool", "error", "progress"].includes(role) ? role : "assistant"; }
function orderedMessages() { return state.messages.map((item, idx) => ({ item, idx, ts: messageCreatedAt(item) })).sort((a, b) => (a.ts && b.ts && a.ts !== b.ts) ? a.ts - b.ts : a.idx - b.idx).map((x) => x.item); }
function formatMessageTime(m) { const ts = messageCreatedAt(m); return ts ? new Date(ts).toLocaleString(locale === "en" ? "en-US" : "zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit" }) : ""; }
function messageMetaHTML(m) { const when = formatMessageTime(m); return when ? `<span class="message-time">${esc(when)}</span>` : ""; }
function copyLabel() { return locale === "en" ? "Copy" : "复制"; }
function copiedLabel() { return locale === "en" ? "Copied" : "已复制"; }
function latestLabel() { return locale === "en" ? "Latest" : "最新消息"; }
function clearContentLabel() { return locale === "en" ? "Clear" : "清空内容"; }
function contentClearedLabel() { return locale === "en" ? "Panel cleared" : "面板内容已清空"; }
function sendingLabel() { return locale === "en" ? "Sending..." : "发送中..."; }
function thinkingLabel() { return locale === "en" ? "Thinking" : "思考中"; }
function copyFailedLabel() { return locale === "en" ? "Copy failed" : "复制失败"; }
function fallbackCopyText(value) {
  const area = document.createElement("textarea");
  area.value = value;
  area.setAttribute("readonly", "");
  area.className = "sr-copy-area";
  document.body.appendChild(area);
  area.select();
  let copied = false;
  try { copied = document.execCommand("copy"); }
  finally { area.remove(); }
  if (!copied) throw new Error(copyFailedLabel());
}
async function copyTextToClipboard(value) {
  if (navigator.clipboard?.writeText) {
    try { await navigator.clipboard.writeText(value); return; }
    catch { /* Fall back for denied clipboard permissions or insecure contexts. */ }
  }
  fallbackCopyText(value);
}
async function copyTextImproved(text, btn) {
  const value = String(text || "");
  if (!value) return;
  try {
    await copyTextToClipboard(value);
    if (btn) {
      const prev = btn.textContent;
      btn.textContent = copiedLabel();
      setTimeout(() => { btn.textContent = prev || copyLabel(); }, 1200);
    }
    toast(copiedLabel());
  } catch (e) { toast(e?.message || copyFailedLabel()); }
}
function bindMessageCopyButtons(msgs) {
  document.querySelectorAll("[data-copy-message]").forEach((btn) => btn.onclick = () => copyTextImproved(String(msgs[Number(btn.dataset.copyMessage)]?.content || msgs[Number(btn.dataset.copyMessage)]?.text || ""), btn));
  document.querySelectorAll("[data-copy-code]").forEach((btn) => btn.onclick = () => copyTextImproved(state.copySnippets[Number(btn.dataset.copyCode)] || "", btn));
}
function bindMessageSpeakButtons(msgs) {
  document.querySelectorAll("[data-speak-message]").forEach((btn) => btn.onclick = () => speakMessage(msgs[Number(btn.dataset.speakMessage)], btn));
}
async function speakMessage(message, btn) {
  const text = String(message?.content || message?.text || "").trim();
  const endpoint = safeTTSEndpoint(message?.metadata?.tts_endpoint);
  if (!text || message?.metadata?.tts_available !== "true") return toast(ttsUnavailableLabel());
  const prev = btn?.textContent || speakLabel();
  if (btn) { btn.disabled = true; btn.textContent = speakingLabel(); }
  try {
    const resp = await fetch(endpoint, { method: "POST", headers: headers(true), body: JSON.stringify({ text, voice_id: message?.metadata?.tts_voice_id || "", format: "mp3" }) });
    if (!resp.ok) {
      let data = {};
      try { data = await resp.json(); } catch { data = {}; }
      const err = new Error(apiErrorMessage(data, `${resp.status} ${resp.statusText}`));
      err.status = resp.status;
      throw err;
    }
    const blob = await resp.blob();
    const url = URL.createObjectURL(blob);
    const audio = new Audio(url);
    const sinkID = String(state.config?.audio_output_device_id || "").trim();
    if (sinkID && typeof audio.setSinkId === "function") {
      try { await audio.setSinkId(sinkID); } catch { /* Browser may reject unavailable output device IDs. */ }
    }
    audio.onended = () => URL.revokeObjectURL(url);
    audio.onerror = () => URL.revokeObjectURL(url);
    await audio.play();
  } catch (e) {
    if (!handleAPIError(e)) toast(e.message || ttsUnavailableLabel());
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = prev; }
  }
}
function safeTTSEndpoint(value) {
  const endpoint = String(value || "").trim();
  if (endpoint.startsWith("/api/") && !endpoint.startsWith("//")) return endpoint;
  return "/api/v1/ai-models/tts/synthesize";
}
function shouldStickMessagesToBottom(el) { return !el || el.scrollHeight <= el.clientHeight || el.scrollHeight - el.scrollTop - el.clientHeight < 80; }
function updateJumpLatestButton(show) { const btn = $("jumpLatest"); if (btn) btn.classList.toggle("hidden", !show); }
function bindJumpLatestButton() { const btn = $("jumpLatest"); const box = $("messages"); if (!btn || !box) return; btn.onclick = () => { box.scrollTop = box.scrollHeight; updateJumpLatestButton(false); }; box.onscroll = () => { if (shouldStickMessagesToBottom(box)) updateJumpLatestButton(false); }; }
function messageCopyButtonHTML(m, idx) { return m.local_thinking || !String(m.content || m.text || "").trim() ? "" : `<button type="button" class="copy-btn" data-copy-message="${idx}" aria-label="${esc(copyLabel())}">${esc(copyLabel())}</button>`; }
function speakLabel() { return locale === "en" ? "Speak" : "\u6717\u8bfb"; }
function speakingLabel() { return locale === "en" ? "Speaking..." : "\u6717\u8bfb\u4e2d..."; }
function transcribeLabel() { return locale === "en" ? "Voice" : "\u8bed\u97f3"; }
function transcribingLabel() { return locale === "en" ? "Transcribing..." : "\u8bc6\u522b\u4e2d..."; }
function voiceReadyLabel() { return locale === "en" ? "Voice text inserted" : "\u8bed\u97f3\u6587\u672c\u5df2\u63d2\u5165"; }
function ttsUnavailableLabel() { return locale === "en" ? "Voice playback is not ready" : "\u8bed\u97f3\u64ad\u653e\u6682\u672a\u5c31\u7eea"; }
function messageSpeakButtonHTML(m, idx) {
  const meta = m.metadata || {};
  const text = String(m.content || m.text || "").trim();
  if ((m.role || "assistant") !== "assistant" || !text || m.local_thinking || meta.tts_available !== "true") return "";
  return `<button type="button" class="copy-btn" data-speak-message="${idx}" aria-label="${esc(speakLabel())}">${esc(speakLabel())}</button>`;
}
function messageActionsHTML(m, idx) {
  const actions = [messageSpeakButtonHTML(m, idx), messageCopyButtonHTML(m, idx)].filter(Boolean).join("");
  return actions ? `<div class="message-actions">${actions}</div>` : "";
}
function renderMessages(forceStick = false) { const box = $("messages"); const stick = forceStick || shouldStickMessagesToBottom(box); const msgs = orderedMessages(); state.copySnippets = []; box.innerHTML = msgs.map((m, idx) => `<article class="message ${messageRoleClass(m.role || "assistant")} ${m.local_pending || m.local_thinking ? "pending" : ""}"><div class="message-head"><div class="message-meta"><strong>${esc(m.role || "assistant")}</strong>${messageMetaHTML(m)}${m.local_pending ? `<span class="message-time">${sendingLabel()}</span>` : ""}</div>${messageActionsHTML(m, idx)}</div><div class="md-content ${m.local_thinking ? "thinking" : ""}">${renderMarkdown(m.content || m.text || "", state.copySnippets)}</div>${messageDetails(m)}</article>`).join("") || `<div class="message assistant">${t("noMessages")}</div>`; bindMessageCopyButtons(msgs); bindMessageSpeakButtons(msgs); bindJumpLatestButton(); if (stick) { box.scrollTop = box.scrollHeight; updateJumpLatestButton(false); } else { updateJumpLatestButton(true); } }
function renderRunPanel(run) {
  const panel = $("runPanel"); if (!panel) return;
  if (run === null) state.currentRun = null; else state.currentRun = run || state.currentRun;
  const r = state.currentRun;
  if (!r || !r.id) { panel.classList.add("hidden"); panel.innerHTML = ""; return; }
  const running = r.status === "running";
  const waiting = r.waiting_for_user || r.response_source === "ask_user";
  panel.classList.remove("hidden");
  panel.innerHTML = `<div><strong>${t("run")} ${esc(r.status || t("unknown"))}</strong><span class="muted">${esc(r.id)}</span>${waiting ? `<span class="pill">${t("waitingUser")}</span>` : ""}</div><div class="row"><button id="waitRun" type="button" class="secondary">${t("continueWaiting")}</button><button id="cancelRun" type="button" class="danger" ${running ? "" : "disabled"}>${t("cancel")}</button></div>`;
  $("waitRun").onclick = () => watchRun(r);
  $("cancelRun").onclick = () => cancelCurrentRun();
}
function handleRunEvent(env) {
  const snap = env?.snapshot || {};
  if (snap.run) renderRunPanel(snap.run);
  if (snap.session?.id) state.sessionId = snap.session.id;
  if (snap.assistant_message) {
    removeThinkingPlaceholders();
    const idx = state.messages.findIndex((m) => m.id === snap.assistant_message.id);
    if (idx >= 0) state.messages[idx] = snap.assistant_message; else state.messages.push(snap.assistant_message);
    renderMessages();
  }
}
function parseSSEFrame(part) {
  const lines = String(part || "").split("\n");
  const event = (lines.find((line) => line.startsWith("event:")) || "event: message").slice(6).trim() || "message";
  const data = lines.filter((line) => line.startsWith("data:")).map((line) => line.slice(5).trimStart()).join("\n");
  return { event, data };
}
function splitSSEBuffer(buffer) {
  const frames = [];
  let rest = String(buffer || "");
  while (rest) {
    const lf = rest.indexOf("\n\n");
    const crlf = rest.indexOf("\r\n\r\n");
    const useCRLF = crlf >= 0 && (lf < 0 || crlf < lf);
    const idx = useCRLF ? crlf : lf;
    if (idx < 0) break;
    frames.push(rest.slice(0, idx));
    rest = rest.slice(idx + (useCRLF ? 4 : 2));
  }
  return { frames, rest };
}
function parseSSEJSON(frame) {
  if (!frame.data) return null;
  try { return JSON.parse(frame.data); } catch { return null; }
}
function handleRunFrame(part) {
  const frame = parseSSEFrame(part);
  const payload = parseSSEJSON(frame);
  if (frame.event === "error") throw new Error(payload?.error || "stream error");
  if (payload) handleRunEvent(payload);
  if (frame.event === "done" && !payload?.snapshot?.assistant_message) removeThinkingPlaceholdersAndRender();
}
async function watchRun(run) {
  if (!run?.id) return;
  const inst = activeInstance(); if (!inst) return;
  closeRunStream();
  const controller = new AbortController(); state.runStream = controller;
  try {
    const resp = await fetch(`/api/v1/instances/${encodeURIComponent(inst.id)}/runs/${encodeURIComponent(run.id)}/events`, { headers: headers(false), signal: controller.signal });
    if (!resp.ok) { const err = new Error(`${resp.status} ${resp.statusText}`); err.status = resp.status; throw err; }
    const reader = resp.body.getReader(); const decoder = new TextDecoder(); let buf = "";
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      buf += decoder.decode(value, { stream: true });
      const split = splitSSEBuffer(buf); buf = split.rest;
      split.frames.forEach(handleRunFrame);
    }
    buf += decoder.decode();
    if (buf.trim()) handleRunFrame(buf);
    await loadSessionsAndMessages();
  } catch (e) { if (e.name !== "AbortError") { removeThinkingPlaceholders(); renderMessages(); if (!handleAPIError(e)) toast(e.message); } }
  finally { if (state.runStream === controller) state.runStream = null; }
}
async function cancelCurrentRun() {
  const inst = activeInstance(); const run = state.currentRun;
  if (!inst || !run?.id) return;
  try {
    const out = await api(`/api/v1/instances/${encodeURIComponent(inst.id)}/runs/${encodeURIComponent(run.id)}/cancel`, { method: "POST", body: JSON.stringify({}) });
    renderRunPanel(out); closeRunStream(); await loadSessionsAndMessages(); toast(t("runCancelled"));
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
}
async function sendMessage(e) {
  e.preventDefault();
  const promptEl = $("prompt"); const sendBtn = $("sendBtn");
  if (!promptEl || !sendBtn || sendBtn.disabled) return;
  const inst = activeInstance(); const content = promptEl.value.trim();
  if (!inst || !content) return;
  const optimisticId = `local-user-${Date.now()}-${Math.random().toString(36).slice(2)}`;
  closeRunStream();
  state.currentRun = null;
  sendBtn.disabled = true;
  promptEl.value = "";
  autoResizePrompt();
  state.messages.push({ id: optimisticId, role: "user", content, created_at: new Date().toISOString(), local_pending: true });
  addThinkingPlaceholder();
  try {
    setBusy(true);
    const body = { content, input_type: "text", title: t("webSession") };
    if (state.sessionId) body.session_id = state.sessionId;
    const out = await api(`/api/v1/instances/${encodeURIComponent(inst.id)}/messages`, { method: "POST", body: JSON.stringify(body) });
    state.sessionId = out.session?.id || state.sessionId;
    upsertSession(out.session);
    replaceLocalMessage(optimisticId, out.message);
    updateSendButtonState();
    renderRunPanel(out.run);
    if (out.run?.id) { addThinkingPlaceholder(out.run.id); watchRun(out.run); }
    else await loadSessionsAndMessages();
    toast(out.run?.status ? t("runStatus", { status: out.run.status }) : t("sent"));
  } catch (e2) { state.messages = state.messages.filter((m) => m.id !== optimisticId && !m.local_thinking); renderMessages(); if (!promptEl.value.trim()) promptEl.value = content; autoResizePrompt(); if (!handleAPIError(e2)) toast(e2.message); }
  finally { updateSendButtonState(); setBusy(false); }
}

async function renderSettings() {
  resetRunState();
  setTitle(t("settingsTitle"), t("settingsHint"));
  try {
    setBusy(true);
    const [schema, cfgResp, skillsResp, mcpResp, wxStatus, imStatus] = await Promise.all([api("/api/v1/config/schema"), api("/api/v1/config"), api("/api/v1/skills"), api("/api/v1/mcp/servers"), api("/api/v1/im/weixin/status").catch(() => null), api("/api/v1/im/status").catch(() => null)]);
    state.schema = items(schema);
    state.config = userConfigDraft(cfgResp.app_config);
    state.skills = items(skillsResp);
    state.mcpServers = items(mcpResp);
    state.weixinRuntime = wxStatus;
    state.imRuntimes = imStatus?.items || {};
    const validation = await api("/api/v1/config/validate", { method: "POST", body: JSON.stringify({ app_config: state.config }) });
    const valid = validation.valid ? "ok" : "error";
    $("content").innerHTML = `<section class="panel stack settings-panel"><div class="settings-head"><div><h2>${t("sharedConfig")}</h2><p class="helper">${t("sharedConfigHint")}</p></div><div class="settings-actions"><span id="cfgStatus" class="badge ${valid}">${validation.valid ? t("valid") : t("needsAttention")}</span><button id="saveCfg" type="button" class="primary">${t("save")}</button><button id="validateCfg" type="button" class="secondary">${t("validate")}</button><button id="testCfg" type="button" class="secondary">${t("test")}</button></div></div><div id="issues" class="stack"></div><div id="cfgTabs" class="cfg-tabs" role="tablist" aria-label="${esc(t("sharedConfig"))}"></div><form id="cfgForm" class="fields"></form><details class="cfg-output"><summary>${t("configResponse")}</summary><pre id="cfgOut" class="code"></pre></details></section>`;
    renderIssues(validation); renderConfigFields();
    $("saveCfg").onclick = saveConfig; $("validateCfg").onclick = validateConfig; $("testCfg").onclick = testConfig;
    setConfigOutput({ me: state.me, app_config: state.config });
  } catch (e) { if (!handleAPIError(e)) renderError(e); }
  finally { setBusy(false); }
}
function skillMarketURL() { return String(state.config?.remote_hubcenter_url || items(state.config?.remote_hubcenter_urls)[0] || "").trim(); }
const SKILL_PAGE_SIZE = 20;
function skillName(s) { return s.name || s.Name || t("unknown"); }
function skillDescription(s) { return s.description || s.Description || ""; }
function skillSource(s) { return s.source || s.Source || "local"; }
function renderSkillCard(s, options = {}) {
  const name = skillName(s);
  const desc = skillDescription(s);
  const status = s.status || s.Status || "active";
  const action = options.action || `<span class="pill">${esc(status)}</span>`;
  const source = options.source ? `<span class="skill-source">${esc(options.source)}</span>` : "";
  return `<article class="skill-card"><div class="skill-card-head"><strong title="${esc(name)}">${esc(name)}</strong>${action}</div>${source}<p>${esc(desc || name)}</p></article>`;
}
function renderSkillManager() {
  const skills = items(state.skills);
  const totalPages = Math.max(1, Math.ceil(skills.length / SKILL_PAGE_SIZE));
  state.skillPage = Math.min(Math.max(1, Number(state.skillPage || 1)), totalPages);
  const start = (state.skillPage - 1) * SKILL_PAGE_SIZE;
  const installed = skills.slice(start, start + SKILL_PAGE_SIZE).map((s) => renderSkillCard(s)).join("") || `<p class="helper">${t("noSkills")}</p>`;
  const pager = skills.length > SKILL_PAGE_SIZE ? `<div class="skill-pager"><button type="button" class="secondary" data-skill-page="prev" ${state.skillPage <= 1 ? "disabled" : ""}>${esc(t("previous"))}</button><span class="helper">${esc(t("pageStatus", { page: state.skillPage, pages: totalPages }))}</span><button type="button" class="secondary" data-skill-page="next" ${state.skillPage >= totalPages ? "disabled" : ""}>${esc(t("nextPage"))}</button></div>` : "";
  const installedNames = new Set(skills.map((s) => String(skillName(s)).toLowerCase()).filter(Boolean));
  const results = items(state.skillResults).map((s) => {
    const alreadyInstalled = Boolean(s.installed) || installedNames.has(String(skillName(s)).toLowerCase());
    const installAttrs = alreadyInstalled ? "disabled aria-disabled=\"true\"" : `data-install-skill="${esc(s.id || "")}" data-install-source="${esc(s.source || "skillmarket")}"`;
    const action = `<button type="button" class="secondary" ${installAttrs}>${alreadyInstalled ? esc(t("installed")) : esc(t("install"))}</button>`;
    return renderSkillCard(s, { action, source: skillSource(s) });
  }).join("");
  return `<div class="skill-manager"><div class="split skill-manager-head"><div><strong>${t("installedSkills")}</strong><span class="helper">/api/v1/skills</span></div><span class="pill">${skills.length}</span></div><div class="skill-grid">${installed}</div>${pager}<div id="skillSearchForm" class="skill-search" role="search"><input id="skillSearchInput" type="search" value="${esc(state.skillQuery)}" placeholder="${t("searchSkillsPlaceholder")}" aria-label="${t("skillMarketSearch")}"><button id="skillSearchBtn" type="button" class="secondary">${t("search")}</button></div><div id="skillSearchResults" class="skill-grid skill-result-grid">${results}</div></div>`;
}
function bindSkillManager() {
  const form = $("skillSearchForm");
  if (!form) return;
  const input = $("skillSearchInput");
  $("skillSearchBtn").onclick = searchSkills;
  input.onkeydown = (e) => { if (e.key === "Enter" && !e.isComposing) { e.preventDefault(); searchSkills(e); } };
  document.querySelectorAll("[data-skill-page]").forEach((b) => {
    b.onclick = () => {
      state.skillPage += b.dataset.skillPage === "next" ? 1 : -1;
      renderConfigFields();
    };
  });
  document.querySelectorAll("[data-install-skill]").forEach((b) => { b.onclick = () => installSkill(b.dataset.installSkill, b.dataset.installSource); });
}
async function searchSkills(e) {
  e?.preventDefault();
  const input = $("skillSearchInput");
  state.skillQuery = input?.value?.trim() || "";
  if (!state.skillQuery) return;
  try {
    setBusy(true);
    const out = await api("/api/v1/skills/search", { method: "POST", body: JSON.stringify({ query: state.skillQuery, sources: ["skillmarket"], skill_market_url: skillMarketURL(), include_installed: true }) });
    state.skillResults = items(out);
    renderConfigFields();
  } catch (e2) { if (!handleAPIError(e2)) toast(e2.message); }
  finally { setBusy(false); }
}
async function installSkill(skillID, source) {
  if (!skillID) return;
  try {
    setBusy(true);
    await api("/api/v1/skills/install", { method: "POST", body: JSON.stringify({ source: source || "skillmarket", skill_market_url: skillMarketURL(), skill_id: skillID, overwrite: true }) });
    state.skills = items(await api("/api/v1/skills"));
    state.skillResults = state.skillResults.map((x) => x.id === skillID ? { ...x, installed: true } : x);
    renderConfigFields();
    toast(t("skillInstalled"));
  } catch (e2) { if (!handleAPIError(e2)) toast(e2.message); }
  finally { setBusy(false); }
}
function mcpServerLabel(s) { return s.name || s.Name || s.id || s.ID || t("unknown"); }
function mcpServerID(s) { return s.id || s.ID || ""; }
function mcpServerKind(s) { return s.kind || s.Kind || (s.command || s.Command ? "local" : "remote"); }
function normalizedMCPKey(value) { return String(value || "").trim().toLowerCase(); }
function mcpServerMarketKeys(s) {
  const cap = s.capability || s.Capability || {};
  return [cap.capability_id, cap.CapabilityID, cap.global_key, cap.GlobalKey, s.id, s.ID].map(normalizedMCPKey).filter(Boolean);
}
function mcpMarketItemKeys(item) { return [item.capability_id, item.CapabilityID, item.global_key, item.GlobalKey, item.id, item.ID].map(normalizedMCPKey).filter(Boolean); }
function isMCPMarketInstalled(item) {
  const keys = new Set(items(state.mcpServers).flatMap(mcpServerMarketKeys));
  return mcpMarketItemKeys(item).some((key) => keys.has(key));
}
function renderMCPManager() {
  const servers = items(state.mcpServers);
  const rows = servers.map((s) => {
    const id = mcpServerID(s);
    const kind = mcpServerKind(s);
    const status = s.health_status || s.HealthStatus || (s.disabled || s.Disabled ? t("mcpDisabled") : "unknown");
    const detail = kind === "local" ? [s.command || s.Command, ...(s.args || s.Args || [])].filter(Boolean).join(" ") : (s.endpoint_url || s.EndpointURL || "");
    return `<div class="mcp-row"><div><strong>${esc(mcpServerLabel(s))}</strong><span class="helper">${esc([kind, status, detail].filter(Boolean).join(" - "))}</span></div><div class="row"><button type="button" class="secondary" data-mcp-edit="${esc(id)}">${t("mcpEdit")}</button><button type="button" class="secondary" data-mcp-action="health-check" data-mcp-id="${esc(id)}">${t("mcpCheck")}</button>${kind === "local" ? `<button type="button" class="secondary" data-mcp-action="start" data-mcp-id="${esc(id)}">${t("mcpStart")}</button><button type="button" class="secondary" data-mcp-action="stop" data-mcp-id="${esc(id)}">${t("mcpStop")}</button>` : ""}<button type="button" class="secondary" data-mcp-action="delete" data-mcp-id="${esc(id)}">${t("mcpDelete")}</button></div></div>${state.mcpEditingID === id ? renderMCPEditor(s) : ""}`;
  }).join("") || `<p class="helper">${t("mcpNoServers")}</p>`;
  return `<div class="mcp-manager"><div class="split"><div><strong>${t("mcpManager")}</strong><span class="helper">${t("mcpManagerHint")}</span></div><span class="pill">${servers.length}</span></div><div class="list">${rows}</div><div class="mcp-add"><div class="mcp-mode-row"><label for="mcpAddMode">${t("mcpManualAdd")}</label><select id="mcpAddMode"><option value="market" ${state.mcpAddMode === "market" ? "selected" : ""}>${t("mcpModeMarket")}</option><option value="remote" ${state.mcpAddMode === "remote" ? "selected" : ""}>${t("mcpModeRemote")}</option><option value="local" ${state.mcpAddMode === "local" ? "selected" : ""}>${t("mcpModeLocal")}</option><option value="json" ${state.mcpAddMode === "json" ? "selected" : ""}>${t("mcpModeJson")}</option></select></div><div id="mcpAddBody">${renderMCPAddBody()}</div></div></div>`;
}
function renderMCPParamRows(names = []) {
  const rows = names.length ? names : [""];
  return rows.map((name) => `<div class="mcp-param-row"><input data-mcp-param-key value="${esc(name)}" placeholder="${esc(t("mcpParamName"))}"><input data-mcp-param-value type="password" autocomplete="new-password" value="${name ? "******" : ""}" placeholder="${esc(t("mcpParamValue"))}"><button type="button" class="secondary" data-mcp-param-remove>${t("mcpDelete")}</button></div>`).join("");
}
function renderMCPEditor(s) {
  const id = mcpServerID(s);
  const kind = mcpServerKind(s);
  if (kind === "local") {
    const args = items(s.args || s.Args).join("\n");
    const envKeys = items(s.env_keys || s.EnvKeys);
    return `<div class="mcp-editor mcp-inline-editor" data-mcp-editor="${esc(id)}"><label>${t("mcpName")}<input id="mcpEditName" value="${esc(mcpServerLabel(s))}"></label><label>${t("mcpCommand")}<input id="mcpEditCommand" list="mcpEditCommandChoices" value="${esc(s.command || s.Command || "")}"><datalist id="mcpEditCommandChoices"><option value="npx"></option><option value="uvx"></option><option value="python"></option><option value="node"></option><option value="cmd"></option></datalist></label><label>${t("mcpArgs")}<textarea id="mcpEditArgs">${esc(args)}</textarea></label><label class="inline-check"><input id="mcpEditAutoStart" type="checkbox" ${s.auto_start || s.AutoStart ? "checked" : ""}>${t("mcpAutoStart")}</label><label class="inline-check"><input id="mcpEditDisabled" type="checkbox" ${s.disabled || s.Disabled ? "checked" : ""}>${t("mcpDisabled")}</label><div class="mcp-param-list"><div class="split"><strong>${t("mcpEnv")}</strong><button type="button" class="secondary" data-mcp-param-add>${t("mcpAddParam")}</button></div><div data-mcp-param-list>${renderMCPParamRows(envKeys)}</div></div><div class="row mcp-editor-actions"><button type="button" class="primary" data-mcp-save="${esc(id)}">${t("mcpSave")}</button><button type="button" class="secondary" data-mcp-close>${t("mcpClose")}</button></div></div>`;
  }
  const headers = items(s.header_names || s.HeaderNames);
  return `<div class="mcp-editor mcp-inline-editor" data-mcp-editor="${esc(id)}"><label>${t("mcpName")}<input id="mcpEditName" value="${esc(mcpServerLabel(s))}"></label><label>${t("mcpEndpoint")}<input id="mcpEditEndpoint" type="url" value="${esc(s.endpoint_url || s.EndpointURL || "")}"></label><label>${t("mcpAuthType")}<select id="mcpEditAuth"><option value="none" ${(s.auth_type || s.AuthType) === "none" ? "selected" : ""}>none</option><option value="bearer" ${(s.auth_type || s.AuthType) === "bearer" ? "selected" : ""}>bearer</option><option value="api_key" ${(s.auth_type || s.AuthType) === "api_key" ? "selected" : ""}>api_key</option></select></label><label>${t("mcpSecret")}<input id="mcpEditSecret" type="password" autocomplete="new-password" value="${s.has_auth_secret || s.HasAuthSecret ? "******" : ""}" placeholder="${esc(t("secretHint"))}"></label><div class="mcp-param-list"><div class="split"><strong>${t("mcpHeaders")}</strong><button type="button" class="secondary" data-mcp-param-add>${t("mcpAddParam")}</button></div><div data-mcp-param-list>${renderMCPParamRows(headers)}</div></div><div class="row mcp-editor-actions"><button type="button" class="primary" data-mcp-save="${esc(id)}">${t("mcpSave")}</button><button type="button" class="secondary" data-mcp-close>${t("mcpClose")}</button></div></div>`;
}
function renderMCPAddBody() {
  if (state.mcpAddMode === "remote") return `<div class="mcp-editor"><label>${t("mcpName")}<input id="mcpRemoteName" type="text" value=""></label><label>${t("mcpEndpoint")}<input id="mcpRemoteEndpoint" type="url" list="mcpEndpointChoices"><datalist id="mcpEndpointChoices"><option value="http://localhost:3000/mcp"></option><option value="http://localhost:8000/mcp"></option><option value="https://mcp.example.com/mcp"></option></datalist></label><label>${t("mcpAuthType")}<select id="mcpRemoteAuth"><option value="none">none</option><option value="bearer">bearer</option><option value="api_key">api_key</option></select></label><label>${t("mcpSecret")}<input id="mcpRemoteSecret" type="password" autocomplete="new-password"></label><button id="mcpAddRemoteBtn" type="button" class="primary">${t("mcpAdd")}</button></div>`;
  if (state.mcpAddMode === "local") return `<div class="mcp-editor"><label>${t("mcpName")}<input id="mcpLocalName" type="text" value=""></label><label>${t("mcpCommand")}<select id="mcpLocalCommand"><option value="npx">npx</option><option value="uvx">uvx</option><option value="python">python</option><option value="node">node</option><option value="cmd">cmd</option></select></label><label>${t("mcpArgs")}<textarea id="mcpLocalArgs" placeholder="--transport\nstdio"></textarea></label><label class="inline-check"><input id="mcpLocalAutoStart" type="checkbox">${t("mcpAutoStart")}</label><button id="mcpAddLocalBtn" type="button" class="primary">${t("mcpAdd")}</button></div>`;
  if (state.mcpAddMode === "json") return `<div class="mcp-editor"><span class="helper">${t("mcpJsonHint")}</span><textarea id="mcpJsonImport" placeholder='{"mcpServers":{"filesystem":{"command":"npx","args":["-y","@modelcontextprotocol/server-filesystem"]}}}'></textarea><button id="mcpImportJsonBtn" type="button" class="primary">${t("mcpAdd")}</button></div>`;
  const results = items(state.mcpMarketResults).map((item, idx) => {
    const installed = isMCPMarketInstalled(item);
    const attrs = installed ? "disabled aria-disabled=\"true\"" : `data-mcp-market-install="${idx}"`;
    return `<div class="mcp-row"><div><strong>${esc(item.display_name || item.capability_id || item.id || t("unknown"))}</strong><span class="helper">${esc([item.source, item.description].filter(Boolean).join(" - "))}</span></div><button type="button" class="primary" ${attrs}>${installed ? t("installed") : t("install")}</button></div>`;
  }).join("") || `<p class="helper">${t("mcpMarketplaceHint")}</p>`;
  return `<div class="mcp-market"><div class="skill-search" role="search"><input id="mcpMarketSearchInput" type="search" value="${esc(state.mcpMarketQuery)}" placeholder="${t("mcpMarketplace")}" aria-label="${t("mcpMarketplace")}"><button id="mcpMarketSearchBtn" type="button" class="secondary">${t("search")}</button></div><div class="list">${results}</div></div>`;
}
function bindMCPManager() {
  const mode = $("mcpAddMode");
  if (!mode) return;
  mode.onchange = () => { state.mcpAddMode = mode.value; $("mcpAddBody").innerHTML = renderMCPAddBody(); bindMCPManager(); };
  document.querySelectorAll("[data-mcp-edit]").forEach((button) => { button.onclick = () => { state.mcpEditingID = button.dataset.mcpEdit || ""; renderConfigFields(); }; });
  document.querySelectorAll("[data-mcp-close]").forEach((button) => { button.onclick = () => { state.mcpEditingID = ""; renderConfigFields(); }; });
  document.querySelectorAll("[data-mcp-save]").forEach((button) => { button.onclick = () => updateMCPServer(button.dataset.mcpSave || "", button.closest("[data-mcp-editor]")); });
  document.querySelectorAll("[data-mcp-param-add]").forEach((button) => { button.onclick = () => addMCPParamRow(button); });
  document.querySelectorAll("[data-mcp-param-remove]").forEach((button) => { button.onclick = () => button.closest(".mcp-param-row")?.remove(); });
  document.querySelectorAll("[data-mcp-action]").forEach((button) => { button.onclick = () => runMCPAction(button.dataset.mcpId, button.dataset.mcpAction); });
  const remoteBtn = $("mcpAddRemoteBtn"); if (remoteBtn) remoteBtn.onclick = addRemoteMCP;
  const localBtn = $("mcpAddLocalBtn"); if (localBtn) localBtn.onclick = addLocalMCP;
  const jsonBtn = $("mcpImportJsonBtn"); if (jsonBtn) jsonBtn.onclick = importMCPJSON;
  const marketBtn = $("mcpMarketSearchBtn"); if (marketBtn) marketBtn.onclick = searchMCPMarket;
  const marketInput = $("mcpMarketSearchInput"); if (marketInput) marketInput.onkeydown = (e) => { if (e.key === "Enter" && !e.isComposing) { e.preventDefault(); searchMCPMarket(); } };
  document.querySelectorAll("[data-mcp-market-install]").forEach((button) => { button.onclick = () => installMCPMarket(Number(button.dataset.mcpMarketInstall || -1)); });
}
async function refreshMCPServers() {
  state.mcpServers = items(await api("/api/v1/mcp/servers"));
  renderConfigFields();
}
async function runMCPAction(id, action) {
  if (!id || !action) return;
  try {
    setBusy(true);
    if (action === "delete") await api(`/api/v1/mcp/servers/${encodeURIComponent(id)}`, { method: "DELETE" });
    else await api(`/api/v1/mcp/servers/${encodeURIComponent(id)}/${action}`, { method: "POST" });
    await refreshMCPServers();
    toast(action === "delete" ? t("mcpDeleted") : t("mcpUpdated"));
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
  finally { setBusy(false); }
}
function addMCPParamRow(button) {
  const list = button.closest(".mcp-param-list")?.querySelector("[data-mcp-param-list]");
  if (!list) return;
  list.insertAdjacentHTML("beforeend", renderMCPParamRows([""]));
  const remove = list.lastElementChild?.querySelector("[data-mcp-param-remove]");
  if (remove) remove.onclick = () => remove.closest(".mcp-param-row")?.remove();
}
function mcpEditorParamMap(editor) {
  const out = {};
  editor.querySelectorAll(".mcp-param-row").forEach((row) => {
    const key = String(row.querySelector("[data-mcp-param-key]")?.value || "").trim();
    const value = String(row.querySelector("[data-mcp-param-value]")?.value || "").trim();
    if (key && value) out[key] = value;
  });
  return out;
}
async function updateMCPServer(id, editor) {
  editor = editor || [...document.querySelectorAll("[data-mcp-editor]")].find((el) => el.dataset.mcpEditor === id);
  const server = items(state.mcpServers).find((s) => mcpServerID(s) === id);
  if (!editor || !server) return;
  const kind = mcpServerKind(server);
  const body = { name: editor.querySelector("#mcpEditName")?.value?.trim() || mcpServerLabel(server) };
  if (kind === "local") {
    body.command = editor.querySelector("#mcpEditCommand")?.value || "npx";
    body.args = (editor.querySelector("#mcpEditArgs")?.value || "").split(/\r?\n/).map((x) => x.trim()).filter(Boolean);
    body.env = mcpEditorParamMap(editor);
    body.auto_start = editor.querySelector("#mcpEditAutoStart")?.checked === true;
    body.disabled = editor.querySelector("#mcpEditDisabled")?.checked === true;
  } else {
    body.endpoint_url = editor.querySelector("#mcpEditEndpoint")?.value?.trim() || "";
    body.auth_type = editor.querySelector("#mcpEditAuth")?.value || "none";
    body.auth_secret = editor.querySelector("#mcpEditSecret")?.value || "";
    body.headers = mcpEditorParamMap(editor);
  }
  try {
    setBusy(true);
    await api(`/api/v1/mcp/servers/${encodeURIComponent(id)}`, { method: "PATCH", body: JSON.stringify(body) });
    state.mcpServers = items(await api("/api/v1/mcp/servers"));
    state.mcpEditingID = id;
    renderConfigFields();
    toast(t("mcpUpdated"));
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
  finally { setBusy(false); }
}
async function addRemoteMCP() {
  const body = { kind: "remote", name: $("mcpRemoteName")?.value?.trim() || "Remote MCP", endpoint_url: $("mcpRemoteEndpoint")?.value?.trim() || "", auth_type: $("mcpRemoteAuth")?.value || "none", auth_secret: $("mcpRemoteSecret")?.value || "" };
  if (!body.endpoint_url) return;
  await createMCPServer(body);
}
async function addLocalMCP() {
  const body = { kind: "local", name: $("mcpLocalName")?.value?.trim() || "Local MCP", command: $("mcpLocalCommand")?.value || "npx", args: ($("mcpLocalArgs")?.value || "").split(/\r?\n/).map((x) => x.trim()).filter(Boolean), auto_start: $("mcpLocalAutoStart")?.checked === true };
  await createMCPServer(body);
}
async function createMCPServer(body) {
  try { setBusy(true); await api("/api/v1/mcp/servers", { method: "POST", body: JSON.stringify(body) }); await refreshMCPServers(); toast(t("mcpAdded")); }
  catch (e) { if (!handleAPIError(e)) toast(e.message); }
  finally { setBusy(false); }
}
async function searchMCPMarket() {
  state.mcpMarketQuery = $("mcpMarketSearchInput")?.value?.trim() || "";
  try { setBusy(true); state.mcpMarketResults = items(await api(`/api/v1/mcp/market?q=${encodeURIComponent(state.mcpMarketQuery)}`)); renderConfigFields(); }
  catch (e) { if (!handleAPIError(e)) toast(e.message); }
  finally { setBusy(false); }
}
async function installMCPMarket(index) {
  const item = state.mcpMarketResults[index];
  if (!item) return;
  try { setBusy(true); await api("/api/v1/mcp/market/install", { method: "POST", body: JSON.stringify(item) }); state.mcpServers = items(await api("/api/v1/mcp/servers")); renderConfigFields(); toast(t("mcpAdded")); }
  catch (e) { if (!handleAPIError(e)) toast(e.message); }
  finally { setBusy(false); }
}
function mcpEntriesFromJSON(raw) {
  const parsed = JSON.parse(raw || "{}");
  if (Array.isArray(parsed)) return parsed;
  const source = parsed.mcpServers && typeof parsed.mcpServers === "object" ? parsed.mcpServers : parsed;
  return Object.entries(source).map(([name, cfg]) => ({ kind: cfg.endpoint_url || cfg.url ? "remote" : "local", name, endpoint_url: cfg.endpoint_url || cfg.url || "", command: cfg.command || "npx", args: Array.isArray(cfg.args) ? cfg.args : [], env: cfg.env || {}, disabled: cfg.disabled === true, auto_start: cfg.auto_start === true }));
}
async function importMCPJSON() {
  try {
    setBusy(true);
    for (const entry of mcpEntriesFromJSON($("mcpJsonImport")?.value || "")) await api("/api/v1/mcp/servers", { method: "POST", body: JSON.stringify(entry) });
    await refreshMCPServers(); toast(t("mcpAdded"));
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
  finally { setBusy(false); }
}
function webSearchProviders() { return items(state.config?.web_search_providers).map((p) => p && typeof p === "object" ? p : {}); }
function webSearchProviderLabel(provider) { return String(provider?.name || provider?.type || "").trim(); }
function currentWebSearchProvider() {
  const providers = webSearchProviders();
  const current = String(state.config?.web_search_current_provider || "").trim();
  return providers.find((p) => webSearchProviderLabel(p) === current || String(p.type || "").trim() === current) || providers[0] || null;
}
function renderWebSearchManager() {
  const providers = webSearchProviders();
  const provider = currentWebSearchProvider();
  const label = webSearchProviderLabel(provider) || t("webSearchNoProviders");
  const type = provider?.type ? `<span class="helper">${esc(t("webSearchProviderType"))}: ${esc(provider.type)}</span>` : "";
  return `<div class="mcp-manager web-search-manager" data-web-search-manager data-web-search-readonly="true"><div class="split"><div><strong>${t("webSearchManager")}</strong><span class="helper">${t("webSearchHint")}</span></div><span class="pill">${esc(t("webSearchProviderCount", { count: providers.length }))}</span></div><div class="mcp-row web-search-readonly"><div><span class="helper">${esc(t("webSearchCurrent"))}</span><strong>${esc(label)}</strong>${type}</div><span class="pill">${esc(t("webSearchManagedByAdmin"))}</span></div></div>`;
}
function bindWebSearchManager() {
  const manager = document.querySelector("[data-web-search-manager]");
  if (!manager) return;
}
const KNOWLEDGE_TOPIC_SUGGESTIONS = ["project", "runbook", "api", "policy", "security", "troubleshooting", "design", "meeting-notes"];
const KNOWLEDGE_LABEL_SUGGESTIONS = ["docs", "ops", "security", "product", "engineering", "faq", "reference", "archive"];
const KNOWLEDGE_TITLE_SUGGESTIONS = ["Project notes", "Runbook", "API reference", "Troubleshooting guide", "Security policy", "Meeting notes"];
const KNOWLEDGE_TEXT_TEMPLATES = [
  { id: "project-notes", label: "Project notes", title: "Project notes", topic: "project", labels: "docs", text: "# Project notes\n\n## Goal\n- \n\n## Current status\n- \n\n## Key decisions\n- \n\n## Links\n- " },
  { id: "runbook", label: "Runbook", title: "Runbook", topic: "runbook", labels: "ops", text: "# Runbook\n\n## Service\n\n## Start / stop\n- \n\n## Health checks\n- \n\n## Common fixes\n- " },
  { id: "api-reference", label: "API reference", title: "API reference", topic: "api", labels: "reference", text: "# API reference\n\n## Endpoint\n- Method: GET\n- Path: /api/v1/...\n\n## Parameters\n- \n\n## Response\n- \n\n## Notes\n- " },
  { id: "troubleshooting", label: "Troubleshooting", title: "Troubleshooting guide", topic: "troubleshooting", labels: "faq", text: "# Troubleshooting\n\n## Symptom\n\n## Likely causes\n- \n\n## Checks\n- \n\n## Resolution\n- " },
  { id: "meeting-notes", label: "Meeting notes", title: "Meeting notes", topic: "meeting-notes", labels: "product", text: "# Meeting notes\n\n## Date\n\n## Attendees\n- \n\n## Decisions\n- \n\n## Action items\n- " }
];
const KNOWLEDGE_URL_SUGGESTIONS = [
  { url: "https://example.com/docs", topic: "project", labels: "docs" },
  { url: "https://example.com/api", topic: "api", labels: "reference" },
  { url: "https://example.com/runbook", topic: "runbook", labels: "ops" },
  { url: "https://example.com/troubleshooting", topic: "troubleshooting", labels: "faq" },
  { url: "https://example.com/faq", topic: "troubleshooting", labels: "faq" }
];
function datalistTextInput(id, suggestions, value = "") {
  const current = String(value || "");
  const all = current && !suggestions.includes(current) ? [...suggestions, current] : suggestions;
  const selected = all.includes(current) ? current : "";
  const custom = selected ? "" : current;
  return `<div class="choice-custom knowledge-choice ${custom ? "custom-active" : ""}"><select id="${esc(id)}" data-choice-suggest><option value="" ${selected === "" && !custom ? "selected" : ""}>${t("unset")}</option>${all.map((opt) => `<option value="${esc(opt)}" ${selected === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}<option value="__custom__" ${custom ? "selected" : ""}>${t("customValue")}</option></select><input id="${esc(id)}Custom" type="text" data-choice-custom value="${esc(custom)}" aria-label="${esc(id)} custom"></div>`;
}
function formChoiceValue(id) {
  const selectValue = String($(id)?.value || "");
  if (selectValue === "__custom__") return String($(`${id}Custom`)?.value || "").trim();
  return selectValue.trim();
}
function clearKnowledgeFieldError(id) {
  const el = $(id);
  if (!el) return;
  el.classList.remove("knowledge-field-error");
  el.removeAttribute("aria-invalid");
  el.removeAttribute("aria-errormessage");
  $(`${id}Error`)?.remove();
}
function showKnowledgeFieldError(id, message) {
  const el = $(id);
  if (!el) { toast(message); return false; }
  clearKnowledgeFieldError(id);
  el.classList.add("knowledge-field-error");
  el.setAttribute("aria-invalid", "true");
  const msg = document.createElement("p");
  msg.id = `${id}Error`;
  msg.className = "knowledge-field-help error";
  msg.textContent = message;
  el.setAttribute("aria-errormessage", msg.id);
  (el.closest(".choice-custom") || el).insertAdjacentElement("afterend", msg);
  toast(message);
  el.focus();
  return false;
}
function clearKnowledgeImportErrors() {
  document.querySelectorAll(".knowledge-field-error").forEach((el) => clearKnowledgeFieldError(el.id));
  document.querySelectorAll(".knowledge-field-help.error").forEach((el) => el.remove());
}
function clearKnowledgeChoiceError(el) {
  if (!el?.id) return;
  clearKnowledgeFieldError(el.id);
  if (el.matches("[data-choice-suggest]")) clearKnowledgeFieldError(`${el.id}Custom`);
  if (el.matches("[data-choice-custom]") && el.id.endsWith("Custom")) clearKnowledgeFieldError(el.id.slice(0, -6));
}
function requireKnowledgeChoiceValue(id, emptyMessage) {
  const select = $(id);
  if (select?.value !== "__custom__") return formChoiceValue(id);
  const customID = `${id}Custom`;
  const value = formChoiceValue(id);
  if (!value) { showKnowledgeFieldError(customID, emptyMessage); return false; }
  clearKnowledgeFieldError(customID);
  return value;
}
function setFormChoiceValue(id, value) {
  const select = $(id);
  if (!select || !value) return;
  if ([...select.options].some((opt) => opt.value === value)) {
    select.value = value;
    select.closest(".choice-custom")?.classList.remove("custom-active");
    return;
  }
  select.value = "__custom__";
  if ($(`${id}Custom`)) $(`${id}Custom`).value = value;
  select.closest(".choice-custom")?.classList.add("custom-active");
}
function knowledgeDepthInput() {
  return `<select id="knowledgeURLDepth">${[0, 1, 2, 3, 4, 5].map((n) => `<option value="${n}" ${n === 0 ? "selected" : ""}>${n}</option>`).join("")}</select>`;
}
function knowledgeTemplateInput() {
  return `<div class="knowledge-picker-row"><select id="knowledgeTextTemplate" aria-label="${esc(t("knowledgeTemplate"))}"><option value="">${esc(t("unset"))}</option>${KNOWLEDGE_TEXT_TEMPLATES.map((tpl) => `<option value="${esc(tpl.id)}">${esc(tpl.label)}</option>`).join("")}</select><button id="insertKnowledgeTemplateBtn" type="button" class="secondary">${esc(t("insertTemplate"))}</button></div>`;
}
function knowledgeURLExampleInput() {
  return `<div class="knowledge-picker-row"><select id="knowledgeURLExample" aria-label="${esc(t("urlExample"))}"><option value="">${esc(t("unset"))}</option>${KNOWLEDGE_URL_SUGGESTIONS.map((item) => `<option value="${esc(item.url)}">${esc(item.url)}</option>`).join("")}</select><button id="addKnowledgeURLBtn" type="button" class="secondary">${esc(t("addURL"))}</button></div>`;
}
function knowledgeField(forID, label, control, extraClass = "") {
  return `<div class="knowledge-field ${esc(extraClass)}"><label for="${esc(forID)}">${esc(label)}</label>${control}</div>`;
}
function knowledgeGlyph(name) {
  const icons = {
    access: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><path d="M4 7.5A2.5 2.5 0 0 1 6.5 5h11A2.5 2.5 0 0 1 20 7.5v9A2.5 2.5 0 0 1 17.5 19h-11A2.5 2.5 0 0 1 4 16.5z"></path><path d="M8 9.5h8M8 13h8M8 16.5h5"></path></svg>`,
    import: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><path d="M12 4v10"></path><path d="m8.5 10.5 3.5 3.5 3.5-3.5"></path><path d="M5 18.5A1.5 1.5 0 0 0 6.5 20h11a1.5 1.5 0 0 0 1.5-1.5"></path></svg>`,
    text: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><path d="M6 5.5h12"></path><path d="M9 5.5v13"></path><path d="M15 5.5v13"></path><path d="M6 18.5h12"></path></svg>`,
    file: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><path d="M6 3.5h7l5 5V19a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V5.5a2 2 0 0 1 2-2z"></path><path d="M13 3.5v5h5"></path></svg>`,
    url: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><path d="M10 14 8 16a3 3 0 0 1-4.2-4.2l3-3A3 3 0 0 1 11 9"></path><path d="M14 10l2-2a3 3 0 1 1 4.2 4.2l-3 3A3 3 0 0 1 13 15"></path><path d="M9 15l6-6"></path></svg>`,
    export: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><path d="M12 20V10"></path><path d="m15.5 13.5-3.5-3.5-3.5 3.5"></path><path d="M5 5.5A1.5 1.5 0 0 1 6.5 4h11A1.5 1.5 0 0 1 19 5.5"></path></svg>`,
    package: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><path d="M4 7.5 12 4l8 3.5-8 3.5z"></path><path d="M4 7.5V16l8 4 8-4V7.5"></path><path d="M12 11v9"></path></svg>`,
    share: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><circle cx="6" cy="12" r="2"></circle><circle cx="18" cy="6" r="2"></circle><circle cx="18" cy="18" r="2"></circle><path d="m7.7 11 8-4"></path><path d="m7.7 13 8 4"></path></svg>`,
    batch: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><rect x="4" y="5" width="16" height="5" rx="1.5"></rect><rect x="4" y="14" width="16" height="5" rx="1.5"></rect></svg>`,
    open: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><path d="M14 5h5v5"></path><path d="M10 14 19 5"></path><path d="M19 13v5a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V7a2 2 0 0 1 2-2h5"></path></svg>`,
    upload: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.9" aria-hidden="true"><path d="M12 20V9"></path><path d="m8.5 12.5 3.5-3.5 3.5 3.5"></path><path d="M5 5.5A1.5 1.5 0 0 1 6.5 4h11A1.5 1.5 0 0 1 19 5.5"></path></svg>`
  };
  return `<span class="knowledge-icon knowledge-icon--${esc(name)}">${icons[name] || icons.import}</span>`;
}
function knowledgeHeading(icon, label) {
  return `${knowledgeGlyph(icon)}<span>${esc(label)}</span>`;
}
function knowledgeButton(id, icon, label, extraClass = "", extraAttrs = "") {
  const cls = ["secondary", "knowledge-icon-button", extraClass].filter(Boolean).join(" ");
  return `<button id="${esc(id)}" type="button" class="${esc(cls)}" ${extraAttrs}>${knowledgeGlyph(icon)}<span>${esc(label)}</span></button>`;
}
function renderKnowledgeImporter() {
  const textFields = [
    knowledgeField("knowledgeTextTitle", t("title"), datalistTextInput("knowledgeTextTitle", KNOWLEDGE_TITLE_SUGGESTIONS)),
    knowledgeField("knowledgeTextTopic", t("topicHint"), datalistTextInput("knowledgeTextTopic", KNOWLEDGE_TOPIC_SUGGESTIONS)),
    knowledgeField("knowledgeTextLabels", t("labels"), datalistTextInput("knowledgeTextLabels", KNOWLEDGE_LABEL_SUGGESTIONS)),
    knowledgeField("knowledgeTextTemplate", t("knowledgeTemplate"), knowledgeTemplateInput(), "knowledge-span-2"),
    knowledgeField("knowledgeTextBody", t("textToImport"), `<textarea id="knowledgeTextBody" placeholder="${esc(t("importTextPlaceholder"))}"></textarea>`, "knowledge-span-2"),
    `${knowledgeButton("knowledgeTextImportBtn", "upload", t("import"), "knowledge-span-2")}`
  ].join("");
  const fileFields = [
    knowledgeField("knowledgeFileTopic", t("topicHint"), datalistTextInput("knowledgeFileTopic", KNOWLEDGE_TOPIC_SUGGESTIONS)),
    knowledgeField("knowledgeFileLabels", t("labels"), datalistTextInput("knowledgeFileLabels", KNOWLEDGE_LABEL_SUGGESTIONS)),
    knowledgeField("knowledgeFileInput", t("chooseFiles"), `<input id="knowledgeFileInput" type="file" multiple accept=".doc,.docx,.pdf,.pptx,.xlsx,.xls,.csv,.md,.markdown,.txt,.text,.zip,.rar">`, "knowledge-span-2"),
    `${knowledgeButton("knowledgeFileImportBtn", "upload", t("import"), "knowledge-span-2")}`
  ].join("");
  const urlFields = [
    knowledgeField("knowledgeURLTopic", t("topicHint"), datalistTextInput("knowledgeURLTopic", KNOWLEDGE_TOPIC_SUGGESTIONS)),
    knowledgeField("knowledgeURLLabels", t("labels"), datalistTextInput("knowledgeURLLabels", KNOWLEDGE_LABEL_SUGGESTIONS)),
    knowledgeField("knowledgeURLExample", t("urlExample"), knowledgeURLExampleInput(), "knowledge-span-2"),
    knowledgeField("knowledgeURLText", t("urlsToImport"), `<textarea id="knowledgeURLText" placeholder="${esc(t("importURLPlaceholder"))}"></textarea>`, "knowledge-span-2"),
    knowledgeField("knowledgeURLDepth", t("crawlDepth"), knowledgeDepthInput()),
    `<label class="inline-check knowledge-check"><input id="knowledgeSameDomain" type="checkbox" checked>${esc(t("sameDomainOnly"))}</label>`,
    `${knowledgeButton("knowledgeURLImportBtn", "upload", t("import"), "knowledge-span-2")}`
  ].join("");
  const exportFields = [
    knowledgeField("knowledgeExportTitle", t("exportTitle"), `<input id="knowledgeExportTitle" type="text" maxlength="160">`),
    knowledgeField("knowledgeExportDescription", t("exportDescription"), `<textarea id="knowledgeExportDescription" placeholder="${esc(t("exportDescriptionPlaceholder"))}" maxlength="2000"></textarea>`, "knowledge-span-2"),
    knowledgeField("knowledgeExportSourceIDs", t("exportSourceIDs"), `<textarea id="knowledgeExportSourceIDs" placeholder="${esc(t("exportSourceIDsPlaceholder"))}"></textarea>`, "knowledge-span-2"),
    `<label class="inline-check knowledge-check"><input id="knowledgeExportIncludeDisabled" type="checkbox">${esc(t("includeDisabledSources"))}</label>`,
    `<div class="knowledge-action-row knowledge-span-2">${knowledgeButton("knowledgeExportBtn", "export", t("exportFile"))}${knowledgeButton("knowledgeViewSharesBtn", "open", t("viewShares"))}</div>`
  ].join("");
  const packageFields = [
    knowledgeField("knowledgePackageFile", t("choosePackage"), `<input id="knowledgePackageFile" type="file" accept=".json,application/json">`, "knowledge-span-2"),
    `${knowledgeButton("knowledgePackageImportBtn", "package", t("importPackage"), "knowledge-span-2")}`
  ].join("");
  const shareFields = [
    knowledgeField("knowledgeShareID", t("knowledgeID"), `<input id="knowledgeShareID" type="text" autocomplete="off">`),
    knowledgeField("knowledgeHubURL", t("hubURL"), `<input id="knowledgeHubURL" type="url" placeholder="https://hub.example.com">`),
    knowledgeField("knowledgeShareLink", t("shareLink"), `<textarea id="knowledgeShareLink" placeholder="https://hub.example.com/knowledge/..."></textarea>`, "knowledge-span-2"),
    knowledgeField("knowledgeHubToken", t("hubToken"), `<input id="knowledgeHubToken" type="password" autocomplete="off" placeholder="${esc(t("hubTokenPlaceholder"))}">`, "knowledge-span-2"),
    `${knowledgeButton("knowledgeShareImportBtn", "share", t("importShare"), "knowledge-span-2")}`
  ].join("");
  return `<div class="knowledge-access-summary" role="group" aria-label="${esc(t("connectedKnowledge"))}"><div class="knowledge-section-head"><strong class="knowledge-icon-title">${knowledgeHeading("access", t("connectedKnowledge"))}</strong><span class="helper">${esc(t("connectedKnowledgeHint"))}</span></div><div class="knowledge-access-layout"><div id="knowledgeAccessSummary" class="knowledge-scope-list" aria-live="polite">${esc(t("loading"))}</div><section class="knowledge-batch-panel" aria-label="${esc(t("ownKnowledgeImports"))}"><div class="knowledge-batch-head"><div><strong class="knowledge-icon-title">${knowledgeHeading("batch", t("ownKnowledgeImports"))}</strong><span class="helper">${esc(t("ownKnowledgeImportsHint"))}</span></div><span id="knowledgeBatchCount" class="badge">0</span></div><div id="knowledgeBatchList" class="knowledge-batch-list" aria-live="polite">${esc(t("loading"))}</div><div id="knowledgeBatchPager" class="knowledge-batch-pager"></div></section></div></div><div class="knowledge-importer" role="group" aria-label="${esc(t("knowledgeImport"))}"><div class="knowledge-section-head"><strong class="knowledge-icon-title">${knowledgeHeading("import", t("knowledgeImport"))}</strong><span class="helper">${esc(t("knowledgeImportHint"))}</span></div><div class="knowledge-import-grid"><section><h3 class="knowledge-subhead">${knowledgeHeading("text", t("importText"))}</h3><div class="knowledge-import-fields">${textFields}</div></section><section><h3 class="knowledge-subhead">${knowledgeHeading("file", t("importFile"))}</h3><div class="knowledge-import-fields">${fileFields}</div></section><section><h3 class="knowledge-subhead">${knowledgeHeading("url", t("importURL"))}</h3><div class="knowledge-import-fields">${urlFields}</div></section><section><h3 class="knowledge-subhead">${knowledgeHeading("export", t("knowledgeExport"))}</h3><p class="helper">${esc(t("knowledgeExportHint"))}</p><div class="knowledge-import-fields">${exportFields}</div></section><section><h3 class="knowledge-subhead">${knowledgeHeading("package", t("knowledgePackageImport"))}</h3><p class="helper">${esc(t("knowledgePackageImportHint"))}</p><div class="knowledge-import-fields">${packageFields}</div></section><section><h3 class="knowledge-subhead">${knowledgeHeading("share", t("knowledgeShareImport"))}</h3><p class="helper">${esc(t("knowledgeShareImportHint"))}</p><div class="knowledge-import-fields">${shareFields}</div></section></div><div id="knowledgeImportProgress" class="knowledge-progress" role="status" aria-live="polite"></div><pre id="knowledgeImportStatus" class="code" aria-live="polite"></pre></div>`;
}
function bindKnowledgeImporter() {
  if (!$('knowledgeTextImportBtn')) return;
  bindChoiceCustomControls();
  document.querySelectorAll(".knowledge-importer input, .knowledge-importer textarea, .knowledge-importer select").forEach((el) => {
    el.addEventListener("input", () => clearKnowledgeChoiceError(el));
    el.addEventListener("change", () => clearKnowledgeChoiceError(el));
  });
  $('insertKnowledgeTemplateBtn').onclick = insertKnowledgeTemplate;
  $('addKnowledgeURLBtn').onclick = addKnowledgeURLExample;
  $('knowledgeTextImportBtn').onclick = importKnowledgeText;
  $('knowledgeFileImportBtn').onclick = importKnowledgeFiles;
  $('knowledgeURLImportBtn').onclick = importKnowledgeURLs;
  $('knowledgeExportBtn').onclick = exportKnowledgePackage;
  $('knowledgeViewSharesBtn').onclick = viewKnowledgeShares;
  $('knowledgePackageImportBtn').onclick = importKnowledgePackage;
  $('knowledgeShareImportBtn').onclick = importKnowledgeShare;
}
function insertKnowledgeTemplate() {
  const target = $('knowledgeTextBody');
  if (!target) return;
  const tpl = KNOWLEDGE_TEXT_TEMPLATES.find((item) => item.id === $('knowledgeTextTemplate')?.value);
  if (!tpl) return;
  setFormChoiceValue("knowledgeTextTitle", tpl.title);
  setFormChoiceValue("knowledgeTextTopic", tpl.topic);
  setFormChoiceValue("knowledgeTextLabels", tpl.labels);
  target.value = target.value.trim() ? `${target.value.trim()}\n\n${tpl.text}` : tpl.text;
  target.focus();
}
function addKnowledgeURLExample() {
  const target = $('knowledgeURLText');
  const url = $('knowledgeURLExample')?.value || "";
  if (!target || !url) return;
  const item = KNOWLEDGE_URL_SUGGESTIONS.find((x) => x.url === url);
  if (item) {
    setFormChoiceValue("knowledgeURLTopic", item.topic);
    setFormChoiceValue("knowledgeURLLabels", item.labels);
  }
  const lines = target.value.split(/\r?\n/).map((line) => line.trim()).filter(Boolean);
  if (!lines.includes(url)) lines.push(url);
  target.value = lines.join("\n");
  target.focus();
}
function setKnowledgeImportStatus(value, reveal = false) {
  const el = $('knowledgeImportStatus');
  const progress = $('knowledgeImportProgress');
  if (el) {
    el.textContent = formatKnowledgeImportStatus(value);
    if (reveal && el.textContent.trim()) el.scrollIntoView({ block: "nearest" });
  }
  if (progress) progress.textContent = knowledgeProgressText(value);
  setConfigOutput(value);
}
function knowledgeProgressText(value) {
  if (typeof value === "string") return value;
  if (!value || typeof value !== "object") return "";
  if (value.progress_text) return value.progress_text;
  const status = String(value.status || "").toLowerCase();
  if (["pending", "queued", "running"].includes(status)) return t("importQueued");
  if (status === "succeeded" || status === "completed") return t("importCompleted");
  if (status === "failed" || status === "canceled") return `${t("importFailed")}: ${value.error || status}`;
  return "";
}
function formatKnowledgeImportStatus(value) {
  if (typeof value === "string") return value;
  if (!value || typeof value !== "object") return pretty(value);
  const result = value.result && typeof value.result === "object" ? value.result : value;
  const lines = [];
  if (value.job_id || value.id) lines.push(`${t("importJob")}: ${value.job_id || value.id}`);
  if (value.status) lines.push(`${t("importStatus")}: ${value.status}`);
  if (value.source_id) lines.push(`${t("importSource")}: ${value.source_id}`);
  if (value.title) lines.push(`${t("importTitle")}: ${value.title}`);
  if (value.kind) lines.push(`${t("importKind")}: ${value.kind}`);
  if (Number.isFinite(value.file_count)) lines.push(`${t("importFiles")}: ${value.file_count}`);
  if (Array.isArray(value.filenames) && value.filenames.length) lines.push(`${t("importFiles")}: ${value.filenames.join(", ")}`);
  if (Number.isFinite(value.url_count)) lines.push(`${t("importUrls")}: ${value.url_count}`);
  const stats = [["processed_files", "importProcessed"], ["imported_files", "importImported"], ["failed_files", "importFailed"], ["skipped_files", "importSkipped"], ["duplicate_files", "importDuplicates"]];
  stats.forEach(([key, label]) => { if (Number.isFinite(result[key])) lines.push(`${t(label)}: ${result[key]}`); });
  if (Array.isArray(result.warnings) && result.warnings.length) lines.push(`${t("importWarnings")}: ${result.warnings.length}`);
  if (value.error || result.error) lines.push(`${t("importFailed")}: ${value.error || result.error}`);
  return lines.length ? lines.join("\n") : pretty(value);
}
function displayWithID(label, id) {
  const text = String(label || "").trim();
  const raw = String(id || "").trim();
  if (!text || text === raw) return raw || "-";
  return `${text} (${raw})`;
}
function userKnowledgeScopeKind(scope, access) {
  const raw = String(scope.scope_type || "").toLowerCase();
  if (["self", "public", "user"].includes(raw)) return raw;
  if (String(scope.owner_id || "").startsWith("public:")) return "public";
  if (String(scope.name || "") === "self") return "self";
  if (String(scope.tenant_id || "") === String(access?.tenant_id || "") && String(scope.owner_id || "") === String(access?.user_id || "")) return "self";
  return "user";
}
function userKnowledgeScopeTypeLabel(kind) {
  if (kind === "self") return t("selfKnowledge");
  if (kind === "public") return t("publicKnowledge");
  return t("otherUserKnowledge");
}
function userKnowledgeTenantLabel(scope, tenantID) {
  const selfTenantID = String(state.me?.tenant_id || state.me?.remote_tenant_id || "");
  if (scope.tenant_name || scope.tenant) return scope.tenant_name || scope.tenant;
  if (tenantID && tenantID === selfTenantID) return state.me?.tenant_name || state.me?.remote_tenant_name || tenantID;
  return tenantID;
}
function userKnowledgeScopeDisplay(scope, kind) {
  const tenantID = String(scope.tenant_id || "");
  const ownerID = String(scope.owner_id || "");
  const tenantLabel = userKnowledgeTenantLabel(scope, tenantID);
  const ownerName = String(scope.owner_name || scope.user_name || "").trim();
  const selfName = state.me?.name || state.me?.user_name || "";
  const selfEmail = state.me?.email || "";
  const selfOwner = selfName && selfEmail ? `${selfName} / ${selfEmail}` : selfName || selfEmail || state.me?.user_id || ownerID;
  const publicOwner = scope.owner_name || scope.name || t("knowledgePublicOwner");
  const userOwner = ownerName || ownerID;
  const ownerLabel = kind === "self" ? selfOwner : kind === "public" ? publicOwner : userOwner;
  return { tenant: displayWithID(tenantLabel, tenantID), owner: displayWithID(ownerLabel, ownerID) };
}
function renderKnowledgeAccessScopes(access) {
  const scopes = items(access?.scopes || []);
  if (!scopes.length) return `<p class="helper">${esc(t("noConnectedKnowledge"))}</p>`;
  return scopes.map((scope) => {
    const kind = userKnowledgeScopeKind(scope, access);
    const type = userKnowledgeScopeTypeLabel(kind);
    const label = String(scope.name || "").trim();
    const title = label && label !== "self" ? label : type;
    const display = userKnowledgeScopeDisplay(scope, kind);
    const clearAction = kind === "self" ? `<button type="button" class="danger knowledge-clear-btn" data-clear-own-knowledge>${esc(t("clearOwnKnowledge"))}</button>` : "";
    return `<div class="knowledge-scope-chip knowledge-scope-${esc(kind)}"><div class="knowledge-scope-head"><strong>${esc(title)}</strong><div class="knowledge-scope-actions"><span class="knowledge-scope-badge">${esc(type)}</span>${clearAction}</div></div><dl class="knowledge-scope-meta"><div><dt>${esc(t("knowledgeOwner"))}</dt><dd>${esc(display.owner)}</dd></div><div><dt>${esc(t("knowledgeTenant"))}</dt><dd>${esc(display.tenant)}</dd></div></dl><small class="knowledge-scope-ids"><span>${esc(t("knowledgeScopeIDs"))}</span><code>${esc(scope.tenant_id || "-")}</code><code>${esc(scope.owner_id || "-")}</code></small></div>`;
  }).join("");
}
async function loadKnowledgeAccessSummary() {
  const el = $('knowledgeAccessSummary');
  if (!el) return;
  try { el.innerHTML = renderKnowledgeAccessScopes(await api("/api/v1/knowledge/access")); bindKnowledgeAccessActions(); }
  catch (e) { el.innerHTML = `<p class="error">${esc(e.message || t("loadFailed"))}</p>`; }
}
function bindKnowledgeAccessActions() {
  document.querySelectorAll("[data-clear-own-knowledge]").forEach((btn) => {
    btn.onclick = (event) => clearOwnKnowledgeBase(event.currentTarget);
  });
}
function renderKnowledgeImportBatches(data) {
  const batches = items(data?.items || []);
  const total = Number(data?.total || 0);
  state.knowledgeBatchTotal = total;
  const count = $("knowledgeBatchCount");
  if (count) count.textContent = String(total);
  if (!batches.length) return `<p class="helper">${esc(t("knowledgeBatchEmpty"))}</p>`;
  return batches.map((batch) => {
    const samples = items(batch.sample_files || []).slice(0, 3);
    const meta = t("knowledgeBatchMeta", { imported: batch.imported_files || 0, skipped: batch.skipped_files || 0, failed: batch.failed_files || 0 });
    const sampleText = samples.length ? `${t("knowledgeBatchSamples")}: ${samples.join(", ")}` : "";
    const name = batch.display_name || batch.root_name || batch.id || "-";
    const topic = batch.topic_hint && batch.topic_hint !== name ? `<span class="knowledge-batch-topic">${esc(batch.topic_hint)}</span>` : "";
    return `<article class="knowledge-batch-row" data-knowledge-batch-id="${esc(batch.id || "")}" data-knowledge-batch-name="${esc(name)}"><div class="knowledge-batch-title"><strong title="${esc(name)}">${esc(name)}</strong><div class="knowledge-batch-actions">${topic}<button type="button" class="danger knowledge-batch-delete" data-delete-knowledge-batch>${esc(t("deleteKnowledgeBatch"))}</button></div></div><div class="knowledge-batch-meta"><span>${esc(t("importStatus"))}: ${esc(batch.status || "-")}</span><span>${esc(t("knowledgeBatchFiles"))}: ${esc(String(batch.total_files || 0))}</span><span>${esc(t("knowledgeBatchUpdated"))}: ${esc(fmtMemoryDate(batch.updated_at || batch.created_at || ""))}</span><span>${esc(meta)}</span>${sampleText ? `<span title="${esc(sampleText)}">${esc(sampleText)}</span>` : ""}</div></article>`;
  }).join("");
}
function renderKnowledgeBatchPager() {
  const el = $("knowledgeBatchPager");
  if (!el) return;
  const totalPages = Math.max(1, Math.ceil((state.knowledgeBatchTotal || 0) / 10));
  el.innerHTML = state.knowledgeBatchTotal > 10 ? `<button type="button" class="secondary" data-knowledge-batch-page="prev" ${state.knowledgeBatchPage <= 1 ? "disabled" : ""}>${esc(t("previous"))}</button><span class="helper">${esc(t("pageStatus", { page: state.knowledgeBatchPage, pages: totalPages }))}</span><button type="button" class="secondary" data-knowledge-batch-page="next" ${state.knowledgeBatchPage >= totalPages ? "disabled" : ""}>${esc(t("nextPage"))}</button>` : "";
  el.querySelectorAll("[data-knowledge-batch-page]").forEach((btn) => {
    btn.onclick = async () => {
      const totalPages = Math.max(1, Math.ceil((state.knowledgeBatchTotal || 0) / 10));
      state.knowledgeBatchPage = btn.dataset.knowledgeBatchPage === "next" ? Math.min(totalPages, state.knowledgeBatchPage + 1) : Math.max(1, state.knowledgeBatchPage - 1);
      await loadKnowledgeImportBatches();
    };
  });
}
async function loadKnowledgeImportBatches() {
  const list = $("knowledgeBatchList");
  if (!list) return;
  try {
    list.innerHTML = `<p class="helper">${esc(t("loading"))}</p>`;
    const out = await api(`/api/v1/knowledge/import/batches?page=${state.knowledgeBatchPage}&limit=10`);
    const total = Number(out?.total || 0);
    const totalPages = Math.max(1, Math.ceil(total / 10));
    if (!items(out?.items || []).length && total > 0 && state.knowledgeBatchPage > totalPages) {
      state.knowledgeBatchPage = totalPages;
      await loadKnowledgeImportBatches();
      return;
    }
    list.innerHTML = renderKnowledgeImportBatches(out);
    bindKnowledgeBatchActions();
    renderKnowledgeBatchPager();
  } catch (e) {
    if (!handleAPIError(e)) list.innerHTML = `<p class="error">${esc(e.message || t("loadFailed"))}</p>`;
  }
}
function bindKnowledgeBatchActions() {
  document.querySelectorAll("[data-delete-knowledge-batch]").forEach((btn) => {
    btn.onclick = (event) => deleteKnowledgeBatch(event.currentTarget);
  });
}
async function deleteKnowledgeBatch(btn) {
  const row = btn?.closest?.("[data-knowledge-batch-id]");
  const batchID = row?.dataset?.knowledgeBatchId || "";
  const name = row?.dataset?.knowledgeBatchName || batchID;
  if (!batchID || btn?.disabled) return;
  const ok = await requestDangerConfirm({ title: t("deleteKnowledgeBatchTitle"), message: t("deleteKnowledgeBatchConfirm", { name }) });
  if (!ok) return;
  const oldText = btn.textContent || "";
  try {
    btn.disabled = true;
    btn.textContent = t("busy");
    const out = await api(`/api/v1/knowledge/import/batches/${encodeURIComponent(batchID)}`, { method: "DELETE" });
    toast(t("deleteKnowledgeBatchDone", { count: out.deleted_sources || 0 }));
    await loadKnowledgeImportBatches();
  } catch (e) {
    if (!handleAPIError(e)) toast(e.message || t("loadFailed"));
  } finally {
    btn.disabled = false;
    btn.textContent = oldText || t("deleteKnowledgeBatch");
  }
}
function requestDangerConfirm({ title, message }) {
  return new Promise((resolve) => {
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    let closed = false;
    const dialogID = `dangerConfirm${Date.now()}${Math.floor(Math.random() * 100000)}`;
    const titleID = `${dialogID}Title`;
    const messageID = `${dialogID}Message`;
    const backdrop = document.createElement("div");
    backdrop.className = "danger-modal-backdrop";
    backdrop.innerHTML = `<section class="danger-modal" role="dialog" aria-modal="true" aria-labelledby="${esc(titleID)}" aria-describedby="${esc(messageID)}"><div class="danger-modal-head"><h2 id="${esc(titleID)}">${esc(title)}</h2><button type="button" class="ghost danger-modal-close" aria-label="${esc(t("cancel"))}">x</button></div><p id="${esc(messageID)}">${esc(message)}</p><div class="danger-modal-actions"><button type="button" class="secondary" data-danger-cancel>${esc(t("cancel"))}</button><button type="button" class="danger" data-danger-confirm>${esc(t("confirm"))}</button></div></section>`;
    const cleanup = (value) => {
      if (closed) return;
      closed = true;
      document.removeEventListener("keydown", onKey);
      backdrop.remove();
      previousFocus?.focus?.({ preventScroll: true });
      resolve(value);
    };
    const onKey = (event) => {
      if (event.key === "Escape") cleanup(false);
      if (event.key === "Enter") cleanup(true);
    };
    backdrop.querySelector("[data-danger-cancel]").onclick = () => cleanup(false);
    backdrop.querySelector(".danger-modal-close").onclick = () => cleanup(false);
    backdrop.querySelector("[data-danger-confirm]").onclick = () => cleanup(true);
    document.addEventListener("keydown", onKey);
    document.body.appendChild(backdrop);
    backdrop.querySelector("[data-danger-confirm]")?.focus({ preventScroll: true });
  });
}
function requestDangerPassword({ title, message, passwordLabel }) {
  return new Promise((resolve) => {
    const previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    let closed = false;
    const dialogID = `dangerModal${Date.now()}${Math.floor(Math.random() * 100000)}`;
    const titleID = `${dialogID}Title`;
    const messageID = `${dialogID}Message`;
    const backdrop = document.createElement("div");
    backdrop.className = "danger-modal-backdrop";
    backdrop.innerHTML = `<section class="danger-modal" role="dialog" aria-modal="true" aria-labelledby="${esc(titleID)}" aria-describedby="${esc(messageID)}"><div class="danger-modal-head"><h2 id="${esc(titleID)}">${esc(title)}</h2><button type="button" class="ghost danger-modal-close" aria-label="${esc(t("cancel"))}">x</button></div><p id="${esc(messageID)}">${esc(message)}</p><label>${esc(passwordLabel)}<input data-danger-password type="password" autocomplete="current-password"></label><div class="danger-modal-actions"><button type="button" class="secondary" data-danger-cancel>${esc(t("cancel"))}</button><button type="button" class="danger" data-danger-confirm>${esc(t("confirm"))}</button></div></section>`;
    const input = backdrop.querySelector("[data-danger-password]");
    const cleanup = (value) => {
      if (closed) return;
      closed = true;
      document.removeEventListener("keydown", onKey);
      backdrop.remove();
      previousFocus?.focus?.({ preventScroll: true });
      resolve(value);
    };
    const onKey = (event) => {
      if (event.key === "Escape") cleanup(null);
      if (event.key === "Enter" && document.activeElement === input) cleanup(input?.value || "");
    };
    backdrop.querySelector("[data-danger-cancel]").onclick = () => cleanup(null);
    backdrop.querySelector(".danger-modal-close").onclick = () => cleanup(null);
    backdrop.querySelector("[data-danger-confirm]").onclick = () => cleanup(input?.value || "");
    document.addEventListener("keydown", onKey);
    document.body.appendChild(backdrop);
    input?.focus({ preventScroll: true });
  });
}
async function clearOwnKnowledgeBase(btn) {
  if (btn?.disabled) return;
  const oldText = btn?.textContent || "";
  if (btn) { btn.disabled = true; btn.textContent = t("busy"); }
  const adminCredential = await requestDangerPassword({ title: t("clearOwnKnowledge"), message: t("clearOwnKnowledgeConfirm"), passwordLabel: t("clearOwnKnowledgePasswordPrompt") });
  if (adminCredential === null) {
    if (btn) { btn.disabled = false; btn.textContent = oldText || t("clearOwnKnowledge"); }
    return;
  }
  if (adminCredential === "") {
    if (btn) { btn.disabled = false; btn.textContent = oldText || t("clearOwnKnowledge"); }
    toast(t("clearOwnKnowledgeAuthRequired"));
    return;
  }
  try {
    const out = await api("/api/v1/knowledge?confirm=true", { method: "DELETE", body: JSON.stringify({ admin_password: adminCredential }) });
    const count = Number.isFinite(Number(out.deleted)) ? Number(out.deleted) : 0;
    toast(t("clearOwnKnowledgeDone", { count }));
    setKnowledgeImportStatus(out, true);
    await loadKnowledgeAccessSummary();
    state.knowledgeBatchPage = 1;
    await loadKnowledgeImportBatches();
  } catch (e) {
    if (!handleAPIError(e)) toast(e.message);
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = oldText || t("clearOwnKnowledge"); }
  }
}
async function watchKnowledgeImportJob(jobID) {
  if (!jobID) return null;
  let latest = null;
  setKnowledgeImportStatus({ id: jobID, status: "queued", progress_text: t("importQueued") }, true);
  for (let i = 0; i < 60; i++) {
    setKnowledgeImportStatus({ id: jobID, status: latest?.status || "queued", progress_text: t("importProgress", { current: i + 1, total: 60 }), result: latest?.result });
    await new Promise((resolve) => setTimeout(resolve, i === 0 ? 400 : 1200));
    latest = await api(`/api/v1/knowledge/import/jobs/${encodeURIComponent(jobID)}`);
    setKnowledgeImportStatus(latest);
    if (!["pending", "queued", "running"].includes(String(latest.status || "").toLowerCase())) return latest;
  }
  return latest;
}
async function runKnowledgeImport(buttonID, task) {
  const btn = $(buttonID);
  const oldText = btn?.textContent || "";
  try {
    if (btn) { btn.disabled = true; btn.textContent = t("importing"); }
    setBusy(true);
    return await task();
  } finally {
    if (btn) { btn.disabled = false; btn.textContent = oldText || t("import"); }
    setBusy(false);
  }
}
function toastKnowledgeImportResult(job) {
  const status = String(job?.status || "").toLowerCase();
  if (status === "succeeded" || status === "completed") toast(t("importCompleted"));
  else if (status === "failed" || status === "canceled") toast(`${t("importFailed")}: ${job?.error || status}`);
  else toast(t("importStillRunning"));
}
async function finishKnowledgeImport(out) {
  setKnowledgeImportStatus(out, true);
  const jobID = String(out?.job_id || out?.id || "").trim();
  if (!jobID) { toastKnowledgeImportResult(out); await loadKnowledgeImportBatches(); return out; }
  toast(t("importStarted"));
  const finalJob = await watchKnowledgeImportJob(jobID);
  toastKnowledgeImportResult(finalJob || out);
  await loadKnowledgeImportBatches();
  return finalJob || out;
}
function parseKnowledgeSourceIDs(value) {
  return String(value || "").split(/[\n,]/).map((item) => item.trim()).filter(Boolean);
}
function knowledgeExportFilename(resp) {
  const header = resp.headers.get("Content-Disposition") || "";
  const match = header.match(/filename="?([^";]+)"?/i);
  return match?.[1] || `maclaw-knowledge-${Date.now()}.json`;
}
async function exportKnowledgePackage() {
  const description = $('knowledgeExportDescription')?.value?.trim() || "";
  if (!description) return showKnowledgeFieldError("knowledgeExportDescription", t("exportDescriptionRequired"));
  try {
    await runKnowledgeImport("knowledgeExportBtn", async () => {
      setKnowledgeImportStatus(t("exportStarted"), true);
      const payload = {
        title: $('knowledgeExportTitle')?.value?.trim() || "",
        description,
        source_ids: parseKnowledgeSourceIDs($('knowledgeExportSourceIDs')?.value || ""),
        include_disabled: $('knowledgeExportIncludeDisabled')?.checked === true
      };
      const resp = await fetch("/api/v1/knowledge/export", { method: "POST", headers: headers(true), body: JSON.stringify(payload) });
      if (!resp.ok) {
        const out = await resp.json().catch(() => ({}));
        throw new Error(apiErrorMessage(out, `${resp.status} ${resp.statusText}`));
      }
      const blob = await resp.blob();
      const a = document.createElement("a");
      a.href = URL.createObjectURL(blob);
      a.download = knowledgeExportFilename(resp);
      document.body.appendChild(a);
      a.click();
      setTimeout(() => { URL.revokeObjectURL(a.href); a.remove(); }, 1200);
      setKnowledgeImportStatus(t("exportCompleted"), true);
      toast(t("exportCompleted"));
    });
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
}
function viewKnowledgeShares() {
  const base = String(state.config?.remote_hub_url || "").trim().replace(/\/+$/, "");
  const viewerToken = String(state.config?.remote_viewer_token || "").trim();
  const tokenHash = viewerToken ? `#token=${encodeURIComponent(viewerToken)}` : "";
  const target = `${base || ""}/hub/knowledge/shares/mine${tokenHash}`;
  window.open(target, "_blank", "noopener");
}
async function importKnowledgePackage() {
  const file = $('knowledgePackageFile')?.files?.[0];
  if (!file) return showKnowledgeFieldError("knowledgePackageFile", t("choosePackageFirst"));
  try {
    await runKnowledgeImport("knowledgePackageImportBtn", async () => {
      setKnowledgeImportStatus(t("importing"), true);
      const text = await file.text();
      const payload = JSON.parse(text);
      const out = await api("/api/v1/knowledge/import/package", { method: "POST", body: JSON.stringify(payload) });
      await finishKnowledgeImport(out);
    });
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
}
async function importKnowledgeShare() {
  const knowledgeID = $('knowledgeShareID')?.value?.trim() || "";
  const shareLink = $('knowledgeShareLink')?.value?.trim() || "";
  if (!knowledgeID && !shareLink) return showKnowledgeFieldError("knowledgeShareID", t("shareInputRequired"));
  try {
    await runKnowledgeImport("knowledgeShareImportBtn", async () => {
      setKnowledgeImportStatus(t("importing"), true);
      const out = await api("/api/v1/knowledge/import/share", { method: "POST", body: JSON.stringify({ knowledge_id: knowledgeID, share_link: shareLink, hub_url: $('knowledgeHubURL')?.value?.trim() || "", hub_token: $('knowledgeHubToken')?.value?.trim() || "" }) });
      await finishKnowledgeImport(out);
    });
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
}
async function importKnowledgeText() {
  clearKnowledgeImportErrors();
  const text = $('knowledgeTextBody')?.value?.trim() || "";
  if (!text) return showKnowledgeFieldError("knowledgeTextBody", t("enterTextFirst"));
  const title = requireKnowledgeChoiceValue("knowledgeTextTitle", t("customTitleRequired"));
  if (title === false) return;
  const topic = requireKnowledgeChoiceValue("knowledgeTextTopic", t("customTopicRequired"));
  if (topic === false) return;
  const labels = requireKnowledgeChoiceValue("knowledgeTextLabels", t("customLabelRequired"));
  if (labels === false) return;
  try {
    await runKnowledgeImport("knowledgeTextImportBtn", async () => {
      setKnowledgeImportStatus(t("importing"), true);
      const out = await api("/api/v1/knowledge/import/text", { method: "POST", body: JSON.stringify({ text, title, topic_hint: topic, labels }) });
      await finishKnowledgeImport(out);
    });
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
}
async function importKnowledgeFiles() {
  clearKnowledgeImportErrors();
  const files = [...($('knowledgeFileInput')?.files || [])];
  if (!files.length) return showKnowledgeFieldError("knowledgeFileInput", t("chooseFileFirst"));
  const topic = requireKnowledgeChoiceValue("knowledgeFileTopic", t("customTopicRequired"));
  if (topic === false) return;
  const labels = requireKnowledgeChoiceValue("knowledgeFileLabels", t("customLabelRequired"));
  if (labels === false) return;
  try {
    await runKnowledgeImport("knowledgeFileImportBtn", async () => {
      setKnowledgeImportStatus(t("importing"), true);
      const form = new FormData();
      files.forEach((file) => form.append("file", file));
      form.append("topic_hint", topic);
      form.append("labels", labels);
      const resp = await fetch("/api/v1/knowledge/import/file", { method: "POST", headers: headers(false), body: form });
      const out = await resp.json().catch(() => ({}));
      if (!resp.ok) throw new Error(apiErrorMessage(out, `${resp.status} ${resp.statusText}`));
      await finishKnowledgeImport(out);
    });
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
}
async function importKnowledgeURLs() {
  clearKnowledgeImportErrors();
  const text = $('knowledgeURLText')?.value?.trim() || "";
  if (!text) return showKnowledgeFieldError("knowledgeURLText", t("enterURLFirst"));
  const topic = requireKnowledgeChoiceValue("knowledgeURLTopic", t("customTopicRequired"));
  if (topic === false) return;
  const labels = requireKnowledgeChoiceValue("knowledgeURLLabels", t("customLabelRequired"));
  if (labels === false) return;
  try {
    await runKnowledgeImport("knowledgeURLImportBtn", async () => {
      setKnowledgeImportStatus(t("importing"), true);
      const rawDepth = Number($('knowledgeURLDepth')?.value || 0);
      const maxDepth = [0, 1, 2, 3, 4, 5].includes(rawDepth) ? rawDepth : 0;
      const out = await api("/api/v1/knowledge/import/urls", { method: "POST", body: JSON.stringify({ text, max_depth: maxDepth, same_domain_only: $('knowledgeSameDomain')?.checked !== false, topic_hint: topic, labels }) });
      await finishKnowledgeImport(out);
    });
  } catch (e) { if (!handleAPIError(e)) toast(e.message); }
}
const MEMORY_CATEGORIES = ["", "self_identity", "user_fact", "preference", "project_knowledge", "instruction", "conversation_summary", "session_checkpoint", "task_artifact", "profile"];
const MEMORY_EDITABLE_CATEGORIES = MEMORY_CATEGORIES.filter((cat) => cat && cat !== "self_identity");
const MEMORY_MAX_CONTENT_CHARS = 20000;
const MEMORY_MAX_TAGS = 32;
const MEMORY_MAX_TAG_CHARS = 80;
const MEMORY_PAGE_LIMIT = 50;
function isProtectedMemory(item) { return item?.read_only === true || item?.protected === true || String(item?.category || "") === "self_identity"; }
function renderMemoryManager() {
  const options = MEMORY_CATEGORIES.map((cat) => `<option value="${esc(cat)}">${esc(cat ? configChoiceLabel("memory_category", cat) : t("memoryAllCategories"))}</option>`).join("");
  return `<section class="memory-manager" data-memory-manager><div class="split"><div><h3>${esc(t("memoryManager"))}</h3><p class="helper">${esc(t("memoryManagerHint"))}</p></div><span id="memoryCount" class="badge">${esc(t("memoryTotal"))}: 0</span></div><div class="memory-toolbar" role="search"><input id="memorySearchInput" type="search" placeholder="${esc(t("memorySearch"))}"><select id="memoryCategoryFilter">${options}</select><button id="memoryRefreshBtn" type="button" class="secondary">${esc(t("memoryRefresh"))}</button><button id="memoryClearBtn" type="button" class="secondary">${esc(t("memoryClear"))}</button></div><div id="memorySummary" class="memory-summary"></div><div class="memory-editor"><input id="memoryEditID" type="hidden"><label>${esc(t("memoryContent"))}<textarea id="memoryContentInput" rows="3" maxlength="20000"></textarea></label><label>${esc(t("memoryCategory"))}<select id="memoryCategoryInput">${MEMORY_EDITABLE_CATEGORIES.map((cat) => `<option value="${esc(cat)}">${esc(configChoiceLabel("memory_category", cat))}</option>`).join("")}</select></label><label>${esc(t("memoryTags"))}<textarea id="memoryTagsInput" rows="2" maxlength="3000" placeholder="${esc(t("memoryTagsHint"))}"></textarea></label><div class="row"><button id="memorySaveBtn" type="button" class="primary">${esc(t("memoryAdd"))}</button><button id="memoryCancelEditBtn" type="button" class="secondary" hidden>${esc(t("memoryCancelEdit"))}</button></div></div><div id="memoryList" class="memory-list"></div><button id="memoryLoadMoreBtn" type="button" class="secondary memory-load-more" hidden>${esc(t("memoryLoadMore"))}</button></section>`;
}
function memoryTagsValue() {
  const seen = new Set();
  return String($("memoryTagsInput")?.value || "").split(/[\n,]/).map((x) => x.trim()).filter(Boolean).filter((tag) => { if (seen.has(tag)) return false; seen.add(tag); return true; });
}
function memoryPayload() { return { content: String($("memoryContentInput")?.value || "").trim(), category: String($("memoryCategoryInput")?.value || "user_fact"), tags: memoryTagsValue() }; }
function validateMemoryPayload(payload) {
  if (!payload.content) return toast(t("memoryContentRequired"));
  if ([...payload.content].length > MEMORY_MAX_CONTENT_CHARS) return toast(t("memoryContentTooLong", { max: MEMORY_MAX_CONTENT_CHARS }));
  if (items(payload.tags).length > MEMORY_MAX_TAGS) return toast(t("memoryTagsTooMany", { max: MEMORY_MAX_TAGS }));
  if (items(payload.tags).some((tag) => [...String(tag)].length > MEMORY_MAX_TAG_CHARS)) return toast(t("memoryTagTooLong", { max: MEMORY_MAX_TAG_CHARS }));
  return true;
}
function fmtMemoryDate(value) { if (!value) return "-"; try { return new Date(value).toLocaleString(locale === "en" ? "en-US" : "zh-CN"); } catch { return value; } }
function resetMemoryEditor() { $("memoryEditID").value = ""; $("memoryContentInput").value = ""; $("memoryTagsInput").value = ""; $("memoryCategoryInput").value = "user_fact"; $("memorySaveBtn").textContent = t("memoryAdd"); $("memoryCancelEditBtn").hidden = true; }
function renderMemoryList(itemsList) {
  const list = $("memoryList");
  if (!list) return;
  if (!itemsList.length) { list.innerHTML = `<p class="helper">${esc(t("memoryEmpty"))}</p>`; return; }
  list.innerHTML = itemsList.map((item) => `<article class="memory-entry" data-memory-id="${esc(item.id)}"><div class="split"><span class="badge">${esc(configChoiceLabel("memory_category", item.category || ""))}</span><small>${esc(t("memoryUpdatedAt"))}: ${esc(fmtMemoryDate(item.updated_at || item.created_at || ""))} \u00b7 ${esc(t("memoryAccessCount"))}: ${esc(String(item.access_count || 0))}</small></div><p>${esc(item.content || "")}</p>${items(item.tags).length ? `<div class="memory-tags">${items(item.tags).map((tag) => `<span>${esc(tag)}</span>`).join("")}</div>` : ""}${isProtectedMemory(item) ? "" : `<div class="row"><button type="button" class="secondary" data-memory-edit>${esc(t("memoryEdit"))}</button><button type="button" class="danger" data-memory-delete>${esc(t("memoryDelete"))}</button></div>`}</article>`).join("");
  list.querySelectorAll("[data-memory-edit]").forEach((btn) => { btn.onclick = () => { const entry = itemsList.find((x) => x.id === btn.closest("[data-memory-id]")?.dataset.memoryId); if (!entry) return; $("memoryEditID").value = entry.id || ""; $("memoryContentInput").value = entry.content || ""; $("memoryCategoryInput").value = entry.category || "user_fact"; $("memoryTagsInput").value = items(entry.tags).join("\n"); $("memorySaveBtn").textContent = t("memoryUpdate"); $("memoryCancelEditBtn").hidden = false; $("memoryContentInput").focus(); }; });
  list.querySelectorAll("[data-memory-delete]").forEach((btn) => { btn.onclick = async () => { const id = btn.closest("[data-memory-id]")?.dataset.memoryId; if (state.memorySaving || !id || !confirm(t("memoryDelete") + "?")) return; setMemorySaving(true); try { await api(`/api/v1/memory/${encodeURIComponent(id)}`, { method: "DELETE" }); if ($("memoryEditID")?.value === id) resetMemoryEditor(); toast(t("memoryDeleted")); await loadMemoryEntries(); } catch (e) { if (!handleAPIError(e)) toast(e.message); } finally { setMemorySaving(false); } }; });
}
function renderMemorySummary(counts) {
  const el = $("memorySummary");
  if (!el) return;
  const chips = MEMORY_CATEGORIES.filter(Boolean).map((cat) => ({ cat, count: Number(counts?.[cat] || 0) })).filter((item) => item.count > 0);
  el.innerHTML = chips.map((item) => `<button type="button" class="memory-chip" data-memory-category-chip="${esc(item.cat)}"><span>${esc(configChoiceLabel("memory_category", item.cat))}</span><strong>${esc(String(item.count))}</strong></button>`).join("");
  el.querySelectorAll("[data-memory-category-chip]").forEach((btn) => { btn.onclick = () => { $("memoryCategoryFilter").value = btn.dataset.memoryCategoryChip || ""; loadMemoryEntries(false); }; });
}
function clearMemoryFilters() { clearTimeout(state.memorySearchTimer); $("memorySearchInput").value = ""; $("memoryCategoryFilter").value = ""; loadMemoryEntries(false); }
function scheduleMemorySearch() { clearTimeout(state.memorySearchTimer); state.memorySearchTimer = setTimeout(() => loadMemoryEntries(false), 300); }
function setMemoryLoading(on, append) {
  ["memoryRefreshBtn", "memoryClearBtn", "memoryLoadMoreBtn", "memorySaveBtn"].forEach((id) => { const el = $(id); if (el) el.disabled = on || state.memorySaving; });
  document.querySelectorAll("[data-memory-edit], [data-memory-delete]").forEach((el) => { el.disabled = on || state.memorySaving; });
  if (on && !append && !items(state.memoryItems).length && $("memoryList")) $("memoryList").innerHTML = `<p class="helper">${esc(t("loading"))}</p>`;
}
function setMemorySaving(on) {
  state.memorySaving = on;
  ["memorySaveBtn", "memoryCancelEditBtn", "memoryRefreshBtn", "memoryClearBtn", "memoryLoadMoreBtn"].forEach((id) => { const el = $(id); if (el) el.disabled = on || state.memoryLoading; });
  document.querySelectorAll("[data-memory-edit], [data-memory-delete]").forEach((el) => { el.disabled = on || state.memoryLoading; });
}
async function loadMemoryEntries() {
  const append = arguments[0] === true;
  if (state.memoryLoading) { if (!append) state.memoryReloadPending = true; return; }
  state.memoryReloadPending = false;
  const q = encodeURIComponent(String($("memorySearchInput")?.value || "").trim());
  const category = encodeURIComponent(String($("memoryCategoryFilter")?.value || ""));
  const offset = append ? state.memoryNextOffset || 0 : 0;
  state.memoryLoading = true;
  setMemoryLoading(true, append);
  try { const out = await api(`/api/v1/memory?q=${q}&category=${category}&limit=${MEMORY_PAGE_LIMIT}&offset=${offset}`); state.memoryItems = append ? state.memoryItems.concat(items(out.items)) : items(out.items); state.memoryNextOffset = Number(out.next_offset || state.memoryItems.length); state.memoryHasMore = !!out.has_more; $("memoryCount").textContent = `${t("memoryTotal")}: ${out.total || 0}`; renderMemorySummary(out.category_counts || {}); renderMemoryList(state.memoryItems); const more = $("memoryLoadMoreBtn"); if (more) more.hidden = !state.memoryHasMore; }
  catch (e) { if (!handleAPIError(e)) $("memoryList").innerHTML = `<p class="error">${esc(e.message || t("loadFailed"))}</p>`; }
  finally { state.memoryLoading = false; setMemoryLoading(false, append); if (state.memoryReloadPending) loadMemoryEntries(false); }
}
function bindMemoryManager() {
  if (!$("memoryList")) return;
  $("memoryRefreshBtn").onclick = () => loadMemoryEntries(false);
  $("memoryClearBtn").onclick = clearMemoryFilters;
  $("memoryLoadMoreBtn").onclick = () => loadMemoryEntries(true);
  $("memorySearchInput").onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); clearTimeout(state.memorySearchTimer); loadMemoryEntries(false); } };
  $("memorySearchInput").oninput = scheduleMemorySearch;
  $("memoryCategoryFilter").onchange = () => loadMemoryEntries(false);
  $("memoryCancelEditBtn").onclick = resetMemoryEditor;
  $("memorySaveBtn").onclick = async () => { if (state.memorySaving) return; const payload = memoryPayload(); if (validateMemoryPayload(payload) !== true) return; const id = String($("memoryEditID")?.value || ""); setMemorySaving(true); try { const path = id ? `/api/v1/memory/${encodeURIComponent(id)}` : "/api/v1/memory"; await api(path, { method: id ? "PUT" : "POST", body: JSON.stringify(payload) }); toast(id ? t("memoryUpdated") : t("memorySaved")); resetMemoryEditor(); await loadMemoryEntries(); } catch (e) { if (!handleAPIError(e)) toast(e.message); } finally { setMemorySaving(false); } };
  if (state.settingsTab === "memory") loadMemoryEntries(false);
}
function renderIssues(validation) { $("issues").innerHTML = (validation.issues || []).map((i) => `<p class="error"><strong>${esc(configIssueLabel(i))}</strong><span>${esc(configIssueMessage(i))}</span></p>`).join("") || `<p class="ok">${t("currentConfigOk")}</p>`; }
function updateConfigStatus(validation) { const el = $("cfgStatus"); if (!el) return; const valid = validation.valid ? "ok" : "error"; el.className = `badge ${valid}`; el.textContent = validation.valid ? t("valid") : t("needsAttention"); }
function setConfigOutput(value) { const el = $("cfgOut"); if (el) el.textContent = pretty(value); }
function setSettingsActionsDisabled(on) {
  ["saveCfg", "validateCfg", "testCfg"].forEach((id) => { const el = $(id); if (el) el.disabled = on; });
  document.querySelectorAll("[data-save-start-im], [data-disconnect-im]").forEach((el) => { el.disabled = on; });
}
const CHANNEL_CONFIG_KEYS = [
  "im_progress_nudge_enabled",
  "qqbot_enabled", "qqbot_app_id", "qqbot_app_secret",
  "telegram_bot_enabled", "telegram_bot_token",
  "weixin_enabled", "weixin_token", "weixin_account_id",
  "lansenger_enabled", "lansenger_app_id", "lansenger_app_secret", "lansenger_gateway_url", "lansenger_wss_url",
  "thirdparty_gateway_enabled", "thirdparty_gateway_token"
];
const IM_ENABLED_KEYS = new Set(["qqbot_enabled", "telegram_bot_enabled", "weixin_enabled", "lansenger_enabled", "thirdparty_gateway_enabled"]);
const PLAIN_TEXT_CONFIG_FIELDS = new Set(["qqbot_app_id", "lansenger_app_id"]);
const IM_REQUIRED_FIELDS = {
  qqbot_enabled: ["qqbot_app_id", "qqbot_app_secret"],
  telegram_bot_enabled: ["telegram_bot_token"],
  weixin_enabled: ["weixin_token", "weixin_account_id"],
  lansenger_enabled: ["lansenger_app_id", "lansenger_app_secret", "lansenger_gateway_url"],
  thirdparty_gateway_enabled: ["thirdparty_gateway_token"]
};
const NON_GENERATABLE_SECRET_KEYS = new Set(["maclaw_llm_key", "qqbot_app_secret", "lansenger_app_secret"]);
function canGenerateSecretForKey(key) {
  return !NON_GENERATABLE_SECRET_KEYS.has(String(key || ""));
}
const IM_RUNTIME_PLATFORMS = {
  qqbot_enabled: "qq",
  telegram_bot_enabled: "telegram",
  lansenger_enabled: "lansenger",
  thirdparty_gateway_enabled: "thirdparty"
};
const IM_DOC_LINKS = {
  qq: "https://q.qq.com/qqbot/openclaw/login.html",
  telegram: "https://open-claw.bot/docs/channels/telegram/"
};
const CONFIG_CHOICE_FIELDS = {
  default_proxy_protocol: ["http", "https", "socks5"],
  language: ["zh-CN", "en-US"],
  skill_purchase_mode: ["auto", "free_only"],
  ui_mode: ["lite", "pro"],
  security_policy_mode: ["none", "standard", "relaxed", "strict", "developer"],
  sandbox_mode: ["none", "os", "docker"],
  network_level: ["none", "intranet", "allowlist", "full"]
};
const GENERIC_CHOICE_FIELDS = {
  mode: ["auto", "local", "remote", "enabled", "disabled", "standard", "strict", "relaxed"],
  policy: ["auto", "allow", "confirm", "reject", "notify_admin", "disabled"],
  type: ["standard", "custom", "local", "remote", "http", "stdio"],
  status: ["active", "inactive", "enabled", "disabled", "pending"],
  source: ["enterprise_hub", "hubcenter", "skillhub", "clawhub", "github", "local"]
};
const CONFIG_NUMBER_CHOICE_FIELDS = {
  agent_response_timeout_sec: [60, 120, 300, 480, 900],
  maclaw_agent_max_iterations: [30, 60, 100, 150, 300],
  subagent_concurrency: [1, 2, 3, 4],
  memory_max_backups: [0, 5, 10, 20, 50],
  knowledge_skill_token_budget: [0, 4000, 8000, 12000, 20000, 32000],
  screen_dim_timeout_min: [0, 3, 5, 10, 15, 30, 60],
  ui_zoom_factor: [0, 0.8, 0.9, 1, 1.1, 1.25, 1.5, 2],
  chat_font_size: [12, 13, 14, 16, 18, 20, 24],
  auto_fetch_interval_min: [0, 5, 10, 20, 30, 60],
  thirdparty_gateway_port: [0, 8080, 18080, 28080, 38080],
  daily_llm_budget_usd: [0, 1, 3, 5, 10, 20, 50]
};
const CONFIG_ARRAY_CHOICE_FIELDS = {};
const CONFIG_LINE_ARRAY_FIELDS = new Set([
  "network_allowlist",
  "auto_fetch_rss_feeds",
  "auto_fetch_watch_dirs",
  "favorite_employees",
  "ve_allowed_directories"
]);
const CONFIG_STRING_LINE_FIELDS = new Set([
  "default_proxy_bypass"
]);
const CONFIG_LINE_ARRAY_SUGGESTION_FIELDS = {
  network_allowlist: ["localhost", "127.0.0.1", "::1", "*.local", "*.corp.example.com", "api.openai.com", "api.anthropic.com"],
  auto_fetch_rss_feeds: ["https://github.blog/feed/", "https://openai.com/news/rss.xml"],
  auto_fetch_watch_dirs: ["~/Downloads", "~/Documents", "D:/workprj"],
  ve_allowed_directories: ["~/workspace", "~/Documents", "D:/workprj", "D:/workprj/aicoder"]
};
const CONFIG_STRING_LINE_SUGGESTION_FIELDS = {
  default_proxy_bypass: ["localhost", "127.0.0.1", "::1", "*.local", "*.corp.example.com"]
};
const COMMON_SKILL_SUGGESTIONS = ["coding", "code_review", "security", "data_analysis", "ops", "docs", "testing", "planning"];
const COMMON_ROLE_SUGGESTIONS = ["user", "developer", "operator", "admin", "security_admin", "auditor", "approver"];
const COMMON_COMMAND_ARG_SUGGESTIONS = ["--help", "--version", "--transport", "stdio", "--port", "8080", "--config", "config.json"];
const COMMON_TEXT_FALLBACK_SUGGESTIONS = ["default", "local", "remote", "auto", "enabled", "disabled"];
const COMMON_LINE_FALLBACK_SUGGESTIONS = ["default", "local", "remote", "project", "ops", "security"];
const COMMON_NUMBER_FALLBACK_SUGGESTIONS = [0, 1, 2, 3, 5, 10, 30, 60, 120, 300];
const ROLE_NAME_SUGGESTIONS = ["developer", "security engineer", "ops engineer", "product manager", "data analyst", "support engineer"];
const ROLE_DESCRIPTION_SUGGESTIONS = [
  "Help with coding, debugging, testing, and technical planning.",
  "Review security risk, permissions, network access, and operational changes.",
  "Assist with operations, diagnostics, deployment checks, and incident response.",
  "Support product analysis, documentation, research, and decision preparation."
];
const LLM_URL_SUGGESTIONS = [
  "https://api.openai.com/v1",
  "https://api.anthropic.com",
  "https://open.bigmodel.cn/api/coding/paas/v4",
  "https://open.bigmodel.cn/api/anthropic",
  "https://api.minimaxi.com/v1",
  "https://api.kimi.com/coding/v1",
  "http://localhost:11434/v1",
  "http://localhost:1234/v1"
];
const LLM_MODEL_SUGGESTIONS = ["auto", "gpt-4o", "claude-sonnet-4-20250514", "glm-5-turbo", "glm-5.1", "kimi-for-coding", "MiniMax-M2.7", "qwen2.5-coder:32b", "deepseek-coder-v2", "llama3.1"];
const CONFIG_OBJECT_LIST_FIELDS = {};
const CONFIG_OBJECT_FIELDS = {
  mis_data: {
    fields: [
      { key: "enabled", kind: "bool" },
      { key: "endpoint", kind: "text", suggestions: ["https://mis.example.com/api", "http://localhost:8080/api"] },
      { key: "token", kind: "password" },
      { key: "tenant_id", kind: "text" },
      { key: "user_id", kind: "text" },
      { key: "role", kind: "select", options: ["", "data_user", "data_admin"] }
    ]
  },
  group_discussion: {
    fields: [
      { key: "enabled", kind: "bool" },
      { key: "discoverable", kind: "bool" },
      { key: "availability", kind: "select", options: ["", "available", "busy", "dnd", "offline"] },
      { key: "suggest_consultation", kind: "bool" },
      { key: "confirm_before_start", kind: "bool" },
      { key: "display_name", kind: "text" },
      { key: "security_group_id", kind: "text" },
      { key: "skills", kind: "lines", suggestions: COMMON_SKILL_SUGGESTIONS },
      { key: "description", kind: "longtext", suggestions: ROLE_DESCRIPTION_SUGGESTIONS },
      { key: "model_visibility", kind: "select", options: ["", "public", "security_group", "private"] },
      { key: "languages", kind: "multi", options: ["zh-CN", "en-US", "ja-JP", "ko-KR"] },
      { key: "invite_policy", kind: "select", options: ["", "auto_accept", "confirm", "reject"] },
      { key: "allow_security_group_free_discussion", kind: "bool" },
      { key: "use_cross_agent_experience", kind: "bool" },
      { key: "allowed_roles", kind: "multi", options: COMMON_ROLE_SUGGESTIONS },
      { key: "max_risk_level", kind: "select", options: ["", "low", "medium", "high"] },
      { key: "context_policy", kind: "select", options: ["", "summary", "full", "minimal"] },
      { key: "reject_when_dnd", kind: "bool" },
      { key: "max_rounds", kind: "number", options: [1, 2, 3, 5, 8] },
      { key: "timeout_seconds", kind: "number", options: [30, 60, 120, 300, 600] },
      { key: "concurrent_limit", kind: "number", options: [1, 2, 3, 5, 10] },
      { key: "contribution_score", kind: "number", options: [0, 0.5, 1, 2, 5] },
      { key: "contribution_evidence", kind: "number", options: [0, 1, 3, 5, 10] },
      { key: "sensitive_query_policy", kind: "select", options: ["", "allow", "confirm", "reject"] }
    ]
  },
  capability_market_policy: {
    fields: [
      { key: "view_mode", kind: "select", options: ["", "merged", "enterprise_only", "hubcenter_only"] },
      { key: "enterprise_only_install", kind: "bool" },
      { key: "enterprise_only_search", kind: "bool" },
      { key: "managed_deployment.enabled", kind: "bool" },
      { key: "managed_deployment.retry_interval_minutes", kind: "number", options: [15, 30, 60, 120, 240, 1440] },
      { key: "managed_deployment.reinstall_if_removed", kind: "bool" },
      { key: "recommended_capability.enabled", kind: "bool" },
      { key: "recommended_capability.allow_user_dismiss", kind: "bool" },
      { key: "update_policy.enterprise_hub.default", kind: "select", options: ["", "auto_update_approved", "auto_update", "auto_update_disabled", "notify_admin", "auto_update_patch_only", "auto_update_trusted_publisher"] },
      { key: "update_policy.enterprise_hub.apply_to", kind: "multi", options: ["managed_deployments", "installed_enterprise_capabilities", "recommended_capabilities_installed_by_user"] },
      { key: "update_policy.hubcenter.free_capability", kind: "select", options: ["", "auto_update", "auto_update_disabled", "notify_admin", "auto_import_pending_review", "auto_update_patch_only", "auto_update_trusted_publisher"] },
      { key: "update_policy.hubcenter.paid_capability", kind: "select", options: ["", "require_license_and_purchase_policy", "notify_admin", "auto_import_pending_review", "auto_update_disabled"] },
      { key: "update_policy.hubcenter.license_or_price_changed", kind: "select", options: ["", "require_admin_or_purchase_policy", "notify_admin", "auto_update_disabled"] },
      { key: "source_priority.enterprise_hub", kind: "number", options: [0, 20, 40, 60, 80, 100] },
      { key: "source_priority.hubcenter", kind: "number", options: [0, 20, 40, 60, 80, 100] },
      { key: "source_priority.clawhub", kind: "number", options: [0, 20, 40, 60, 80, 100] },
      { key: "source_priority.github", kind: "number", options: [0, 20, 40, 60, 80, 100] },
      { key: "resource_types.skill.allowed_sources", kind: "multi", options: ["enterprise_hub", "hubcenter", "clawhub", "github", "local"] },
      { key: "resource_types.skill.default_sources", kind: "multi", options: ["enterprise_hub", "hubcenter", "clawhub", "github"] },
      { key: "resource_types.skill.user_configurable_sources", kind: "multi", options: ["enterprise_hub", "hubcenter", "clawhub", "github"] },
      { key: "resource_types.mcp.allowed_sources", kind: "multi", options: ["enterprise_hub", "hubcenter", "clawhub", "github"] },
      { key: "resource_types.mcp.default_sources", kind: "multi", options: ["enterprise_hub", "hubcenter", "clawhub", "github"] },
      { key: "resource_types.mcp.user_configurable_sources", kind: "multi", options: ["enterprise_hub", "hubcenter", "clawhub", "github"] }
    ]
  }
};
const CONFIG_OBJECT_MAP_FIELDS = {};
const CONFIG_JSON_STRING_OBJECT_FIELDS = {
  ve_approval_config: {
    fields: [
      { key: "enabled", kind: "bool" },
      { key: "acl.mode", kind: "select", options: ["", "whitelist", "blacklist"] },
      { key: "acl.departments", kind: "lines", suggestions: ["engineering", "product", "security", "ops", "finance", "legal", "hr"] },
      { key: "acl.roles", kind: "lines", suggestions: COMMON_ROLE_SUGGESTIONS },
      { key: "acl.skills", kind: "lines", suggestions: COMMON_SKILL_SUGGESTIONS },
      { key: "acl.entities", kind: "lines", suggestions: ["all", "tenant", "department", "team", "user"] },
      { key: "max_queue_size", kind: "number", options: [10, 50, 100, 200, 500, 1000] },
      { key: "timeout_hours", kind: "number", options: [1, 4, 8, 24, 72, 168, 720] },
      { key: "daily_quota", kind: "number", options: [10, 50, 100, 500, 1000, 5000, 10000] },
      { key: "fallback_approver", kind: "text" }
    ]
  }
};
const CONFIG_SUGGESTION_FIELDS = {
  maclaw_llm_url: LLM_URL_SUGGESTIONS,
  maclaw_llm_model: LLM_MODEL_SUGGESTIONS,
  tts_voice_id: ["default", "zf_xiaobei", "zf_xiaoni", "zm_yunjian", "zm_yunxi"],
  default_proxy_host: ["127.0.0.1", "localhost"],
  default_proxy_port: ["7890", "7897", "1080", "3128", "8080"],
  lansenger_gateway_url: ["https://apigw.lx.qianxin.com"],
  lansenger_wss_url: ["wss://apigw.lx.qianxin.com/ws", "wss://gateway.example.com/ws"],
  weixin_base_url: ["https://api.weixin.qq.com"],
  weixin_cdn_url: ["https://res.wx.qq.com", "https://cdn.example.com/weixin"],
  skill_market_url: ["https://hubcenter.example.com", "https://hub.example.com"],
  thirdparty_gateway_host: ["127.0.0.1", "0.0.0.0", "localhost"],
  maclaw_role_name: ROLE_NAME_SUGGESTIONS,
  maclaw_role_description: ROLE_DESCRIPTION_SUGGESTIONS
};
const GENERIC_TEXT_SUGGESTIONS = {
  url: ["https://example.com", "http://localhost:8080", "http://localhost:3000"],
  wss: ["wss://gateway.example.com/ws", "ws://localhost:8080/ws"],
  host: ["127.0.0.1", "0.0.0.0", "localhost"],
  path: ["~/.maclaw", "~/.maclaw/workspace", "~/workspace", "D:/workprj"],
  name: ["default", "local", "development", "production", "security", "ops"],
  id: ["default", "local", "hub", "dev", "prod"],
  user: ["root", "ubuntu", "ec2-user", "deploy", "admin"],
  email: ["user@example.com", "admin@example.com"],
  mobile: ["+8613800138000", "+12025550123"],
  nickname: ["developer", "operator", "assistant", "reviewer"],
  model: LLM_MODEL_SUGGESTIONS
};
const GENERIC_OBJECT_KEY_SUGGESTIONS = ["name", "url", "endpoint", "enabled", "type", "mode", "token", "key", "secret", "path", "role", "source"];
function userProfileSuggestions(keys) {
  const out = [];
  keys.forEach((key) => {
    const value = state.me?.[key];
    if (value !== undefined && value !== null && String(value).trim()) out.push(String(value).trim());
  });
  return out;
}
function genericSuggestionOptions(key) {
  const k = String(key || "").toLowerCase();
  if (k.includes("email")) return [...new Set([...userProfileSuggestions(["email", "remote_email", "user_email"]), ...GENERIC_TEXT_SUGGESTIONS.email])];
  if (k.includes("mobile") || k.includes("phone")) return [...new Set([...userProfileSuggestions(["mobile", "phone", "remote_mobile"]), ...GENERIC_TEXT_SUGGESTIONS.mobile])];
  if (k.includes("nickname") || k.includes("display_name")) return [...new Set([...userProfileSuggestions(["name", "display_name", "user_name", "email"]), ...GENERIC_TEXT_SUGGESTIONS.nickname])];
  if (k.includes("model")) return GENERIC_TEXT_SUGGESTIONS.model;
  if (k.includes("wss") || k.includes("websocket")) return GENERIC_TEXT_SUGGESTIONS.wss;
  if (k.endsWith("url") || k.includes("_url") || k.includes("endpoint")) return GENERIC_TEXT_SUGGESTIONS.url;
  if (k.endsWith("host") || k.includes("_host")) return GENERIC_TEXT_SUGGESTIONS.host;
  if (k.includes("path") || k.includes("dir") || k.includes("directory")) return GENERIC_TEXT_SUGGESTIONS.path;
  if (k.endsWith("user") || k.includes("_user")) return GENERIC_TEXT_SUGGESTIONS.user;
  if (k.endsWith("name") || k.includes("display_name") || k.endsWith("label") || k.endsWith("title")) return GENERIC_TEXT_SUGGESTIONS.name;
  if (k.endsWith("id") && !k.includes("token") && !k.includes("secret") && !k.includes("key")) return GENERIC_TEXT_SUGGESTIONS.id;
  return GENERIC_TEXT_SUGGESTIONS.name;
}
function genericLineSuggestions(key) {
  const k = String(key || "").toLowerCase();
  if (k.includes("skill")) return COMMON_SKILL_SUGGESTIONS;
  if (k.includes("role")) return COMMON_ROLE_SUGGESTIONS;
  if (k.includes("source")) return ["enterprise_hub", "hubcenter", "skillhub", "clawhub", "github", "local"];
  if (k.includes("employee") || k.includes("user") || k.includes("member") || k.includes("approver")) {
    return [...new Set([...userProfileSuggestions(["name", "display_name", "user_name", "email", "remote_email"]), "admin", "operator", "developer", "approver"])];
  }
  if (k.includes("url") || k.includes("feed") || k.includes("endpoint")) return GENERIC_TEXT_SUGGESTIONS.url;
  if (k.includes("host") || k.includes("allowlist")) return GENERIC_TEXT_SUGGESTIONS.host;
  if (k.includes("path") || k.includes("dir") || k.includes("directory")) return GENERIC_TEXT_SUGGESTIONS.path;
  return GENERIC_TEXT_SUGGESTIONS.name;
}
function genericNumberOptions(key) {
  const k = String(key || "").toLowerCase();
  if (k.includes("port")) return [0, 22, 80, 443, 7890, 8080, 18080];
  if (k.includes("timeout") || k.includes("interval")) return [0, 5, 10, 30, 60, 120, 300, 600];
  if (k.includes("limit") || k.includes("max")) return [0, 1, 3, 5, 10, 50, 100, 500, 1000];
  if (k.includes("score") || k.includes("confidence")) return [0, 0.5, 0.7, 0.8, 0.9, 0.95, 1];
  return [0, 1, 2, 5, 10, 20, 50, 100];
}
const CONFIG_CHOICE_LABELS = {
  en: {
    default_proxy_protocol: { http: "HTTP", https: "HTTPS", socks5: "SOCKS5" },
    language: { "zh-CN": "Simplified Chinese", "en-US": "English" },
    skill_purchase_mode: { auto: "Auto", free_only: "Free only" },
    ui_mode: { lite: "Lite", pro: "Pro" },
    security_policy_mode: { none: "Off", standard: "Standard", relaxed: "Relaxed", strict: "Strict", developer: "Developer" },
    sandbox_mode: { none: "Off", os: "OS sandbox", docker: "Docker" },
    network_level: { none: "Offline", intranet: "Intranet only", allowlist: "Allowlist", full: "Full network" },
    memory_category: { self_identity: "Self identity", user_fact: "User fact", preference: "Preference", project_knowledge: "Project knowledge", instruction: "Instruction", conversation_summary: "Conversation summary", session_checkpoint: "Session checkpoint", task_artifact: "Task artifact", profile: "Profile" }
  },
  zh: {
    default_proxy_protocol: { http: "HTTP", https: "HTTPS", socks5: "SOCKS5" },
    language: { "zh-CN": "\u7b80\u4f53\u4e2d\u6587", "en-US": "\u82f1\u6587" },
    skill_purchase_mode: { auto: "\u81ea\u52a8", free_only: "\u4ec5\u514d\u8d39" },
    ui_mode: { lite: "\u7b80\u6d01", pro: "\u4e13\u4e1a" },
    security_policy_mode: { none: "\u5173\u95ed", standard: "\u6807\u51c6", relaxed: "\u5bbd\u677e", strict: "\u4e25\u683c", developer: "\u5f00\u53d1\u8005" },
    sandbox_mode: { none: "\u5173\u95ed", os: "\u7cfb\u7edf\u6c99\u7bb1", docker: "Docker" },
    network_level: { none: "\u79bb\u7ebf", intranet: "\u4ec5\u5185\u7f51", allowlist: "\u5141\u8bb8\u5217\u8868", full: "\u5b8c\u6574\u7f51\u7edc" },
    memory_category: { self_identity: "\u81ea\u6211\u8ba4\u77e5", user_fact: "\u7528\u6237\u4e8b\u5b9e", preference: "\u504f\u597d", project_knowledge: "\u9879\u76ee\u77e5\u8bc6", instruction: "\u6307\u4ee4", conversation_summary: "\u5bf9\u8bdd\u6458\u8981", session_checkpoint: "\u4f1a\u8bdd\u68c0\u67e5\u70b9", task_artifact: "\u4efb\u52a1\u4ea7\u7269", profile: "\u7528\u6237\u753b\u50cf" }
  }
};
function configBoolLabel(value) {
  if (value === true) return t("channelEnabled");
  if (value === false) return t("channelDisabled");
  return t("channelAuto");
}
function thirdPartyProtocolEndpoint() {
  return `${thirdPartyEndpointBase()}/api/im-gateway/v1`;
}
function thirdPartyEndpointBase() {
  const origin = String(window.location.origin || "").trim().replace(/\/+$/, "");
  if (origin && origin !== "null") return origin;
  const protocol = String(window.location.protocol || "http:").replace(/:+$/, "") || "http";
  const host = String(window.location.host || "").trim();
  return host ? `${protocol}://${host}` : "http://127.0.0.1";
}
function updateThirdPartyEndpoint() {
  const el = $("thirdPartyProtocolEndpoint");
  if (el) el.textContent = thirdPartyProtocolEndpoint();
}
function bindChannelTools() {
  document.querySelectorAll("[data-im-subtab]").forEach((button) => {
    button.onclick = () => setActiveIMSubTab(button.dataset.imSubtab || "");
  });
  document.querySelectorAll("[data-im-watch-history]").forEach((button) => {
    button.onclick = () => {
      state.imAuditPlatform = String(button.dataset.imWatchHistory || "").trim();
      state.imAuditContact = "";
      state.imAuditQuery = "";
      resetIMAuditPagination();
      state.imAuditOpen = true;
      const shell = document.querySelector(".im-audit-shell");
      if (shell) shell.open = true;
      loadIMAuditMessages();
    };
  });
  const copy = $("copyThirdPartyEndpoint");
  if (copy) copy.onclick = () => copyTextImproved(thirdPartyProtocolEndpoint(), copy);
  updateThirdPartyEndpoint();
  document.querySelectorAll("[data-save-start-im]").forEach((button) => {
    button.onclick = () => saveAndStartIM(button.dataset.saveStartIm || "");
  });
  document.querySelectorAll("[data-disconnect-im]").forEach((button) => {
    button.onclick = () => disconnectIM(button.dataset.disconnectIm || "");
  });
}
function randomSecretValue() {
  if (!window.crypto?.getRandomValues) return "";
  const bytes = new Uint8Array(32);
  window.crypto.getRandomValues(bytes);
  return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
}
function isLikelySecretKey(key) {
  return /(^|_)(api_?key|secret|token|password|passwd|pwd)(_|$)/i.test(String(key || ""));
}
function bindSecretGenerators() {
  document.querySelectorAll("[data-generate-secret]").forEach((button) => {
    button.onclick = () => {
      const target = $(button.dataset.generateSecret || "");
      const value = randomSecretValue();
      if (!target || !value) { toast(t("channelTokenUnavailable")); return; }
      target.value = value;
      target.dispatchEvent(new Event("input", { bubbles: true }));
      toast(t("channelTokenGenerated"));
    };
  });
  document.querySelectorAll("[data-toggle-secret]").forEach((button) => {
    button.onclick = () => {
      const input = $(button.dataset.toggleSecret || "");
      if (!input) return;
      const show = input.type === "password";
      input.type = show ? "text" : "password";
      button.textContent = show ? t("hideSecret") : t("showSecret");
    };
  });
}
function secretGenerateLabel(id) {
  return /token/i.test(String(id || "")) ? t("channelGenerateToken") : t("generateSecret");
}
function secretFieldMarkup(id, attrs, current, extra = "", canGenerate = true) {
  const key = String(id || "").replace(/^cfg_/, "");
  const generate = canGenerate && canGenerateSecretForKey(key) ? `<button type="button" class="secondary" data-generate-secret="${esc(id)}">${esc(secretGenerateLabel(id))}</button>` : "";
  const reveal = key === "thirdparty_gateway_token" ? `<button type="button" class="secondary" data-toggle-secret="${esc(id)}">${esc(t("showSecret"))}</button>` : "";
  return `<div class="secret-input"><input id="${esc(id)}" ${attrs} type="password" autocomplete="new-password" spellcheck="false" value="${esc(current)}" ${extra}>${generate}${reveal}</div>`;
}
function configFieldMarkup(key, defs) {
  const d = fieldMeta(defs[key] || { key, title: key, type: Array.isArray(state.config[key]) ? "array" : typeof state.config[key] === "boolean" ? "bool" : typeof state.config[key] === "number" ? "number" : "string" });
  const label = `${esc(d.title || key)}${d.required ? " *" : ""}`;
  return `<div class="field"><label for="cfg_${key}">${label}</label>${fieldInput(key, d)}<span class="helper">${esc(fieldHelper(d))}</span></div>`;
}
function renderIMToggleField(enabledKey, defs) {
  const d = fieldMeta(defs[enabledKey] || { key: enabledKey, title: enabledKey, type: "bool" });
  return `<div class="im-enable-row"><div><strong>${esc(d.title || enabledKey)}</strong><span class="helper">${esc(fieldHelper(d))}</span></div>${boolRadioInput(enabledKey, esc(fieldValue(enabledKey, d)))}</div>`;
}
function hasConfigValue(key) {
  return String(state.config?.[key] ?? "").trim() !== "";
}
function imStatus(enabledKey) {
  const enabled = state.config?.[enabledKey];
  const configured = (IM_REQUIRED_FIELDS[enabledKey] || []).every(hasConfigValue);
  if (enabledKey === "weixin_enabled" && enabled === true && configured) return weixinRuntimeBadge();
  const platform = IM_RUNTIME_PLATFORMS[enabledKey];
  if (enabled === true && configured && platform) return imRuntimeBadge(platform);
  if (enabled === true && configured) return { label: t("channelRunning"), cls: "ok" };
  if (enabled === true) return { label: t("channelIncomplete"), cls: "warn" };
  return { label: t("channelDisabled"), cls: "" };
}
function imRuntimeBadge(platform) {
  const info = state.imRuntimes?.[platform] || {};
  const runtime = String(info.status || "").trim();
  const detail = String(info.last_error || "").trim();
  const withDetail = (label) => detail ? `${label}: ${detail}` : label;
  if (runtime === "connected") return { label: t("weixinRuntimeConnected"), cls: "ok" };
  if (runtime === "connecting" || runtime === "reconnecting") return { label: t("weixinRuntimeStarting"), cls: "warn" };
  if (runtime === "error" || runtime === "session_expired") return { label: withDetail(t("weixinRuntimeError")), cls: "warn" };
  if (!runtime || runtime === "disabled" || runtime === "disconnected") return { label: withDetail(t("weixinRuntimeDisconnected")), cls: "warn" };
  return { label: withDetail(t("channelRunning")), cls: "ok" };
}
function weixinRuntimeBadge() {
  const runtime = String(state.weixinRuntime?.runtime || state.weixinRuntime?.status || "").trim();
  if (runtime === "connected") return { label: t("weixinRuntimeConnected"), cls: "ok" };
  if (runtime === "connecting" || runtime === "reconnecting") return { label: t("weixinRuntimeStarting"), cls: "warn" };
  if (runtime === "error" || runtime === "session_expired") return { label: t("weixinRuntimeError"), cls: "warn" };
  return { label: t("weixinRuntimeDisconnected"), cls: "warn" };
}
function renderIMCardStatus(enabledKey) {
  const status = imStatus(enabledKey);
  return `<span class="badge ${esc(status.cls)}">${esc(status.label)}</span>`;
}
function renderIMCardActions(enabledKey) {
  const busy = state.imStartingKey === enabledKey ? "disabled" : "";
  if (state.config?.[enabledKey] === true && imRequiredFieldsReady(enabledKey)) {
    return `<div class="im-card-actions"><button type="button" class="secondary danger" data-disconnect-im="${esc(enabledKey)}" ${busy}>${esc(t("channelDisconnect"))}</button></div>`;
  }
  return `<div class="im-card-actions"><button type="button" class="primary" data-save-start-im="${esc(enabledKey)}" ${busy}>${esc(t("channelSaveStart"))}</button></div>`;
}
function renderIMLinkAction(href, label) {
  return `<a class="secondary im-link-action" href="${esc(href)}" target="_blank" rel="noopener noreferrer">${esc(label)}</a>`;
}
function thirdPartyDocsURL() {
  const base = String(state.config?.remote_hub_url || "").trim().replace(/\/+$/, "");
  return base ? `${base}/connector` : "/connector";
}
function thirdPartyProtocolTools() {
  return `<div class="channel-protocol"><div><strong>${esc(t("channelProtocolEndpoint"))}</strong><code id="thirdPartyProtocolEndpoint">${esc(thirdPartyProtocolEndpoint())}</code></div><div class="channel-protocol-actions"><button type="button" class="secondary" id="copyThirdPartyEndpoint">${esc(t("channelCopyEndpoint"))}</button></div></div>`;
}
function normalizeIMSubTab(tab) {
  tab = String(tab || "").trim().toLowerCase();
  return ["qq", "telegram", "weixin", "lansenger", "thirdparty"].includes(tab) ? tab : "qq";
}
function setActiveIMSubTab(tab) {
  state.imSubTab = normalizeIMSubTab(tab);
  renderConfigFields();
}
function imSubTabs() {
  return [
    ["qq", t("imChannelTabQQ")],
    ["telegram", t("imChannelTabTelegram")],
    ["weixin", t("imChannelTabWeixin")],
    ["lansenger", t("imChannelTabLansenger")],
    ["thirdparty", t("imChannelTabThirdParty")]
  ];
}
function renderIMSubTabs() {
  return `<div class="im-subtabs" role="tablist" aria-label="${esc(t("channelOverview"))}">${imSubTabs().map(([id, label]) => {
    const on = normalizeIMSubTab(state.imSubTab) === id;
    return `<button type="button" role="tab" class="im-subtab ${on ? "active" : ""}" data-im-subtab="${esc(id)}" aria-selected="${on ? "true" : "false"}">${esc(label)}</button>`;
  }).join("")}</div>`;
}
function renderIMProgressHintSettings(defs) {
  const key = "im_progress_nudge_enabled";
  const d = fieldMeta(defs[key] || { key, title: t("imProgressHints"), description: t("imProgressHintsDesc"), type: "bool" });
  const enabled = state.config?.[key] !== false;
  const name = "cfg_im_progress_nudge_enabled";
  const control = `<div id="${esc(name)}" class="bool-radio bool-radio-two" data-bool-group="true" data-key="${esc(key)}" data-type="bool" role="radiogroup" aria-label="${esc(d.title || key)}"><label><input type="radio" name="${esc(name)}" value="true" ${enabled ? "checked" : ""}><span>${esc(t("trueValue"))}</span></label><label><input type="radio" name="${esc(name)}" value="false" ${enabled ? "" : "checked"}><span>${esc(t("falseValue"))}</span></label></div>`;
  return `<section class="im-progress-settings"><div><strong>${esc(d.title || t("imProgressHints"))}</strong><span class="helper">${esc(d.description || t("imProgressHintsDesc"))}</span></div>${control}</section>`;
}
function renderIMChannelShell(title, description, enabledKey, platform, defs, body, extraActions = "", actionOverride = null) {
  const actions = actionOverride === null ? renderIMCardActions(enabledKey) : actionOverride;
  return `<section class="im-channel-panel" aria-label="${esc(title)}"><div class="im-channel-toolbar"><div><h3>${esc(title)}</h3><p>${esc(description)}</p></div><div class="im-channel-actions">${renderIMCardStatus(enabledKey)}${extraActions}<button type="button" class="secondary" data-im-watch-history="${esc(platform)}">${esc(t("imWatchHistory"))}</button>${actions}</div></div>${renderIMToggleField(enabledKey, defs)}${body}</section>`;
}
function renderQQIMPanel(defs) {
  return renderIMChannelShell(t("channelQQ"), t("imQQDescription"), "qqbot_enabled", "qq", defs, `<div class="im-field-grid im-field-grid-two">${["qqbot_app_id", "qqbot_app_secret"].map((key) => configFieldMarkup(key, defs)).join("")}</div>`, renderIMLinkAction(IM_DOC_LINKS.qq, t("imGetAppID")));
}
function renderTelegramIMPanel(defs) {
  return renderIMChannelShell(t("channelTelegram"), t("imTelegramDescription"), "telegram_bot_enabled", "telegram", defs, `<div class="im-field-grid">${configFieldMarkup("telegram_bot_token", defs)}</div>`, renderIMLinkAction(IM_DOC_LINKS.telegram, t("imTutorial")));
}
function renderWeixinIMPanel(defs) {
  const account = String(state.config?.weixin_account_id || "").trim();
  const runtime = weixinRuntimeBadge();
  const runtimeText = state.weixinRuntime?.last_error ? `${runtime.label}: ${state.weixinRuntime.last_error}` : runtime.label;
  const accountLine = account ? `<div class="weixin-account-status"><div><strong>${esc(t("weixinBoundAccount"))}</strong><code>${esc(account)}</code></div><div><strong>${esc(t("weixinRuntimeStatus"))}</strong><span class="badge ${esc(runtime.cls)}">${esc(runtimeText)}</span></div></div>` : `<span class="helper">${esc(t("weixinNotBound"))}</span>`;
  return renderIMChannelShell(t("channelWeixin"), t("imWeixinDescription"), "weixin_enabled", "weixin", defs, `${accountLine}${renderWeixinQRTools()}`, "", renderWeixinActions(account));
}
function renderThirdPartyIMPanel(defs) {
  return renderIMChannelShell(t("channelThirdParty"), t("imThirdPartyDescription"), "thirdparty_gateway_enabled", "thirdparty", defs, `<div class="im-field-grid">${configFieldMarkup("thirdparty_gateway_token", defs)}</div>${thirdPartyProtocolTools()}`, renderIMLinkAction(thirdPartyDocsURL(), t("imOpenDocs")));
}
function renderLansengerIMPanel(defs) {
  return renderIMChannelShell(t("channelLansenger"), t("imLansengerDescription"), "lansenger_enabled", "lansenger", defs, `<div class="im-field-grid im-field-grid-two">${["lansenger_app_id", "lansenger_app_secret", "lansenger_gateway_url", "lansenger_wss_url"].map((key) => configFieldMarkup(key, defs)).join("")}</div>`);
}
function renderActiveIMPanel(defs) {
  const tab = normalizeIMSubTab(state.imSubTab);
  if (tab === "telegram") return renderTelegramIMPanel(defs);
  if (tab === "weixin") return renderWeixinIMPanel(defs);
  if (tab === "lansenger") return renderLansengerIMPanel(defs);
  if (tab === "thirdparty") return renderThirdPartyIMPanel(defs);
  return renderQQIMPanel(defs);
}
function renderWeixinQRTools() {
  const status = normalizeWeixinQRStatus(state.weixinQRStatus);
  const bound = Boolean(String(state.config?.weixin_account_id || "").trim()) && !state.weixinQRToken && !state.weixinQRCodeURL;
  const statusText = status === "confirmed" ? t("weixinQRConfirmed") : status === "scaned" ? t("weixinQRScanned") : status === "expired" ? t("weixinQRExpired") : status === "error" ? t("weixinQRError") : state.weixinQRToken ? t("weixinQRWaiting") : t("weixinQRHint");
  const message = normalizeWeixinQRMessage(status, state.weixinQRMessage, statusText);
  const qr = state.weixinQRCodeURL ? `<div class="weixin-qr-box"><img src="${esc(state.weixinQRCodeURL)}" alt="${esc(t("weixinQRBind"))}"><span class="helper">${esc(message)}</span></div>` : `<span class="helper">${esc(message)}</span>`;
  const busy = status === "loading" ? "disabled" : "";
  const cancel = state.weixinQRToken || state.weixinQRCodeURL ? `<button type="button" class="secondary danger" id="cancelWeixinQRLogin">${esc(t("imCancelQR"))}</button>` : "";
  if (bound) return "";
  return `<div class="weixin-qr-tools"><div><strong>${esc(t("weixinQRBind"))}</strong>${qr}</div><div class="weixin-qr-actions"><button type="button" class="secondary" id="startWeixinQRLogin" ${busy}>${esc(status === "loading" ? t("weixinQRLoading") : t("weixinQRBind"))}</button>${cancel}</div></div>`;
}
function normalizeWeixinQRStatus(status) {
  const s = String(status || "").trim().toLowerCase();
  if (["wait", "waiting", "pending", "polling", "timeout"].includes(s)) return "wait";
  if (["scaned", "scanned", "scan"].includes(s)) return "scaned";
  if (["confirmed", "confirm", "success", "succeeded", "connected", "done", "ok"].includes(s)) return "confirmed";
  if (["expired", "expire"].includes(s)) return "expired";
  if (["error", "failed", "fail"].includes(s)) return "error";
  return s;
}
function normalizeWeixinQRMessage(status, message, fallback) {
  const text = String(message || "").trim();
  if (!text) return fallback || "";
  const normalizedStatus = normalizeWeixinQRStatus(status);
  if ((normalizedStatus === "wait" || !normalizedStatus) && /^timeout$/i.test(text)) return t("weixinQRWaiting");
  return text;
}
function renderWeixinActions(boundAccount) {
  const busy = state.imStartingKey === "weixin_enabled" ? "disabled" : "";
  if (!boundAccount) return "";
  if (state.config?.weixin_enabled === true) {
    return `<div class="im-card-actions"><button type="button" class="secondary danger" data-disconnect-im="weixin_enabled" ${busy}>${esc(t("channelDisconnect"))}</button></div>`;
  }
  return `<div class="im-card-actions"><button type="button" class="primary" data-save-start-im="weixin_enabled" ${busy}>${esc(t("channelSaveStart"))}</button></div>`;
}
function renderIMConfigEditor(defs) {
  state.imSubTab = normalizeIMSubTab(state.imSubTab);
  return `<div class="channel-overview im-config-editor"><div class="im-section-head"><div><strong>${t("channelOverview")}</strong><span class="helper">${t("channelOverviewHint")}</span></div></div>${renderIMProgressHintSettings(defs)}${renderIMSubTabs()}${renderActiveIMPanel(defs)}<details class="im-audit-shell" ${state.imAuditOpen ? "open" : ""}><summary>${esc(t("imAuditTitle"))}</summary>${renderIMAuditPanel()}</details><p class="helper">${t("channelCredentialHint")}</p></div>`;
}
function renderIMAuditPanel() {
  const platforms = [["", t("imAuditPlatformAll")], ["qq", "QQ"], ["weixin", "WeChat"], ["telegram", "Telegram"], ["lansenger", "Lansenger"], ["thirdparty", "Third-party"]];
  const contactOptions = state.imAuditContacts.map((item) => {
    const label = [item.display_name || item.contact_id, item.platform, item.message_count ? String(item.message_count) : ""].filter(Boolean).join(" / ");
    return `<option value="${esc(item.contact_id || "")}" label="${esc(label)}"></option>`;
  }).join("");
  const displayItems = orderedIMAuditItems();
  const rows = displayItems.length
    ? `${displayItems.map(renderIMAuditRow).join("")}${state.imAuditLoading ? `<p class="helper">${esc(t("imAuditLoading"))}</p>` : ""}`
    : state.imAuditLoading
      ? `<p class="helper">${esc(t("imAuditLoading"))}</p>`
      : `<p class="helper">${esc(t("imAuditEmpty"))}</p>`;
  const stats = state.imAuditStats ? `<div class="memory-summary"><span class="memory-chip"><strong>${esc(t("imAuditStats", { messages: state.imAuditStats.messages || 0, contacts: state.imAuditStats.contacts || 0, platforms: state.imAuditStats.platforms || 0 }))}</strong></span></div>` : "";
  const loadOlder = state.imAuditHasMore ? `<button type="button" class="secondary memory-load-more" id="imAuditLoadOlder" ${state.imAuditLoading ? "disabled" : ""}>${esc(t("imAuditLoadOlder"))}</button>` : "";
  const cleanupDays = [7, 14, 30, 60, 90].map((days) => `<button type="button" class="secondary" data-im-audit-days="${days}">${days} ${esc(t("imAuditCleanupDays"))}</button>`).join("");
  const busy = state.imAuditLoading ? "disabled" : "";
  return `<section class="im-audit"><div class="split"><div><strong>${esc(t("imAuditTitle"))}</strong><span class="helper">${esc(t("imAuditHint"))}</span></div><div class="row"><button type="button" class="secondary" id="imAuditExport" ${busy}>${esc(t("imAuditExport"))}</button><button type="button" class="secondary" id="imAuditRefresh" ${busy}>${esc(t("imAuditRefresh"))}</button></div></div><div class="im-audit-filters"><select id="imAuditPlatform" aria-label="${esc(t("imAuditPlatformAll"))}">${platforms.map(([value, label]) => `<option value="${esc(value)}" ${value === state.imAuditPlatform ? "selected" : ""}>${esc(label)}</option>`).join("")}</select><input id="imAuditContact" type="search" list="imAuditContactOptions" placeholder="${esc(t("imAuditContact"))}" value="${esc(state.imAuditContact)}"><datalist id="imAuditContactOptions">${contactOptions}</datalist><input id="imAuditQuery" type="search" placeholder="${esc(t("imAuditKeyword"))}" value="${esc(state.imAuditQuery)}"></div><div class="im-audit-cleanup"><div class="row">${cleanupDays}</div><label for="imAuditCleanupBefore">${esc(t("imAuditCleanupBefore"))}<input id="imAuditCleanupBefore" type="datetime-local" value="${esc(state.imAuditCleanupBefore)}"></label><button type="button" class="secondary danger" id="imAuditCleanup" ${busy}>${esc(t("imAuditCleanup"))}</button></div>${stats}<div id="imAuditList" class="im-audit-list">${rows}</div>${loadOlder}</section>`;
}
function imAuditCreatedAtMs(item) {
  const msg = item?.message || item?.Message || {};
  const raw = msg.created_at || item?.created_at || item?.CreatedAt || "";
  const ms = raw ? Date.parse(raw) : NaN;
  return Number.isFinite(ms) ? ms : 0;
}
function orderedIMAuditItems() {
  return state.imAuditItems.map((item, idx) => ({ item, idx, ts: imAuditCreatedAtMs(item) })).sort((a, b) => a.ts !== b.ts ? a.ts - b.ts : a.idx - b.idx).map((x) => x.item);
}
function renderIMAuditRow(item) {
  const msg = item.message || item.Message || {};
  const role = msg.role || "assistant";
  const content = msg.content || msg.text || "";
  const platform = item.platform || item.Platform || "";
  const contact = item.contact_id || item.ContactID || "";
  const sessionTitle = item.session_title || item.SessionTitle || item.session_id || item.SessionID || "";
  const instanceName = item.instance_name || item.InstanceName || item.instance_id || item.InstanceID || "";
  const created = msg.created_at || item.created_at || item.CreatedAt || "";
  const when = created ? new Date(created).toLocaleString(locale === "en" ? "en-US" : "zh-CN", { month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit" }) : "";
  const idx = state.imAuditItems.indexOf(item);
  return `<article class="im-audit-row"><div class="message-head"><div class="message-meta"><strong>${esc(role)}</strong>${platform ? `<span class="pill">${esc(platform)}</span>` : ""}${contact ? `<span class="message-time">${esc(contact)}</span>` : ""}${when ? `<span class="message-time">${esc(when)}</span>` : ""}</div>${idx >= 0 ? `<button type="button" class="secondary" data-im-audit-open="${idx}">${esc(t("imAuditOpen"))}</button>` : ""}</div><div class="md-content">${renderMarkdown(content, state.copySnippets)}</div><div class="message-details"><span>${esc(instanceName)}</span>${sessionTitle ? `<span>${esc(t("imAuditOpenSession"))}: ${esc(sessionTitle)}</span>` : ""}</div></article>`;
}
async function loadIMAuditMessages(append = false) {
  if (state.imAuditLoading || (append && !state.imAuditNextBefore)) return;
  state.imAuditLoading = true;
  if (state.view === "settings" && $("cfgTabs")) renderConfigFields();
  try {
    await loadIMAuditContacts(false);
    const qs = new URLSearchParams({ limit: "100" });
    if (state.imAuditPlatform) qs.set("platform", state.imAuditPlatform);
    if (state.imAuditQuery) qs.set("q", state.imAuditQuery);
    if (state.imAuditContact) qs.set("contact", state.imAuditContact);
    if (append) qs.set("before", state.imAuditNextBefore);
    const [out, stats] = await Promise.all([
      api(`/api/v1/im-audit/messages?${qs.toString()}`),
      api(`/api/v1/im-audit/stats?${imAuditQueryString()}`)
    ]);
    const nextItems = items(out);
    state.imAuditItems = append ? state.imAuditItems.concat(nextItems) : nextItems;
    state.imAuditHasMore = Boolean(out?.has_more);
    state.imAuditNextBefore = String(out?.next_before || "");
    state.imAuditStats = stats;
    state.imAuditLoaded = true;
  } catch (e) {
    state.imAuditLoaded = false;
    if (!handleAPIError(e)) toast(e.message);
  } finally {
    state.imAuditLoading = false;
    if (state.view === "settings" && $("cfgTabs")) renderConfigFields();
  }
}
function imAuditQueryString() {
  const qs = new URLSearchParams();
  if (state.imAuditPlatform) qs.set("platform", state.imAuditPlatform);
  if (state.imAuditQuery) qs.set("q", state.imAuditQuery);
  if (state.imAuditContact) qs.set("contact", state.imAuditContact);
  return qs.toString();
}
async function exportIMAuditCSV() {
  const query = imAuditQueryString();
  const url = `/api/v1/im-audit/export.csv${query ? `?${query}` : ""}`;
  try {
    const resp = await fetch(url, { headers: headers(false) });
    if (!resp.ok) throw new Error(`${resp.status} ${resp.statusText}`);
    const blob = await resp.blob();
    const href = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = href;
    a.download = `im-audit-${new Date().toISOString().slice(0, 10)}.csv`;
    document.body.appendChild(a);
    a.click();
    a.remove();
    URL.revokeObjectURL(href);
  } catch (e) {
    if (!handleAPIError(e)) toast(e.message);
  }
}
async function cleanupIMAuditMessages() {
  const raw = String($("imAuditCleanupBefore")?.value || "").trim();
  if (!raw) return;
  state.imAuditCleanupBefore = raw;
  const before = new Date(raw);
  if (!Number.isFinite(before.getTime())) return toast(t("loadFailed"));
  const label = before.toLocaleString(locale === "en" ? "en-US" : "zh-CN");
  if (!confirm(t("imAuditCleanupConfirm", { before: label }))) return;
  try {
    const qs = imAuditQueryString();
    const sep = qs ? `${qs}&` : "";
    const out = await api(`/api/v1/im-audit/messages?${sep}before=${encodeURIComponent(before.toISOString())}&confirm=true`, { method: "DELETE" });
    toast(t("imAuditCleaned", { deleted: out.deleted || 0 }));
    state.imAuditLoaded = false;
    state.imAuditContactsPlatform = null;
    resetIMAuditPagination();
    await loadIMAuditMessages();
  } catch (e) {
    if (!handleAPIError(e)) toast(e.message);
  }
}
async function loadIMAuditContacts(force = false) {
  if (!force && state.imAuditContactsPlatform === state.imAuditPlatform) return;
  const qs = new URLSearchParams();
  if (state.imAuditPlatform) qs.set("platform", state.imAuditPlatform);
  const out = await api(`/api/v1/im-audit/contacts${qs.toString() ? `?${qs.toString()}` : ""}`);
  state.imAuditContacts = items(out);
  state.imAuditContactsPlatform = state.imAuditPlatform;
}
function resetIMAuditPagination() {
  state.imAuditHasMore = false;
  state.imAuditNextBefore = "";
}
function setIMAuditCleanupDays(days) {
  const n = Number(days);
  if (!Number.isFinite(n) || n <= 0) return;
  const d = new Date(Date.now() - n * 24 * 60 * 60 * 1000);
  const pad = (v) => String(v).padStart(2, "0");
  state.imAuditCleanupBefore = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
  const el = $("imAuditCleanupBefore");
  if (el) el.value = state.imAuditCleanupBefore;
}
function clearWeixinQRPoll() {
  if (state.weixinQRPollTimer) clearTimeout(state.weixinQRPollTimer);
  state.weixinQRPollTimer = 0;
}
function revokeWeixinQRCodeURL() {
  if (state.weixinQRCodeURL && state.weixinQRCodeURL.startsWith("blob:")) URL.revokeObjectURL(state.weixinQRCodeURL);
  state.weixinQRCodeURL = "";
}
function resetWeixinQRLogin() {
  clearWeixinQRPoll();
  revokeWeixinQRCodeURL();
  state.weixinQRToken = "";
  state.weixinQRStatus = "";
  state.weixinQRMessage = "";
}
function scheduleWeixinQRPoll() {
  clearWeixinQRPoll();
  if (!state.weixinQRToken) return;
  state.weixinQRPollTimer = setTimeout(() => pollWeixinQRLogin(), 1600);
}
async function startWeixinQRLogin() {
  try {
    clearWeixinQRPoll();
    state.weixinQRStatus = "loading";
    state.weixinQRMessage = t("weixinQRLoading");
    renderConfigFields();
    const out = await api("/api/v1/im/weixin/qr/start", { method: "POST", body: "{}" });
    revokeWeixinQRCodeURL();
    const imageURL = String(out.qrcode_image_url || "");
    state.weixinQRCodeURL = imageURL ? await authorizedObjectURL(imageURL) : String(out.qrcode_url || "");
    state.weixinQRToken = String(out.qrcode_token || "");
    state.weixinQRStatus = "wait";
    state.weixinQRMessage = t("weixinQRWaiting");
    renderConfigFields();
    scheduleWeixinQRPoll();
  } catch (e) {
    state.weixinQRStatus = "error";
    state.weixinQRMessage = e.message || t("weixinQRError");
    renderConfigFields();
    if (!handleAPIError(e)) toast(state.weixinQRMessage);
  }
}
async function pollWeixinQRLogin() {
  if (!state.weixinQRToken) return;
  try {
    const out = await api("/api/v1/im/weixin/qr/poll", { method: "POST", body: JSON.stringify({ qrcode_token: state.weixinQRToken }) });
    state.weixinQRStatus = normalizeWeixinQRStatus(out.status);
    state.weixinQRMessage = normalizeWeixinQRMessage(state.weixinQRStatus, out.message || out.error, "");
    if (out.retryable && (state.weixinQRStatus === "wait" || state.weixinQRStatus === "error" || !state.weixinQRStatus)) {
      state.weixinQRStatus = "wait";
      state.weixinQRMessage = t("weixinQRWaiting");
      renderConfigFields();
      scheduleWeixinQRPoll();
      return;
    }
    if (state.weixinQRStatus === "confirmed") {
      state.weixinQRMessage = out.account_id ? `${t("weixinQRConfirmed")} ${out.account_id}` : t("weixinQRConfirmed");
      state.weixinQRToken = "";
      revokeWeixinQRCodeURL();
      clearWeixinQRPoll();
      const cfgResp = await api("/api/v1/config");
      state.config = userConfigDraft(cfgResp.app_config);
      await loadWeixinRuntimeStatus();
      toast(t("weixinQRConfirmed"));
      renderConfigFields();
      return;
    }
    if (state.weixinQRStatus === "expired" || state.weixinQRStatus === "error") {
      state.weixinQRToken = "";
      revokeWeixinQRCodeURL();
      clearWeixinQRPoll();
      renderConfigFields();
      return;
    }
    renderConfigFields();
    scheduleWeixinQRPoll();
  } catch (e) {
    state.weixinQRStatus = "error";
    state.weixinQRMessage = e.message || t("weixinQRError");
    state.weixinQRToken = "";
    revokeWeixinQRCodeURL();
    clearWeixinQRPoll();
    renderConfigFields();
    if (!handleAPIError(e)) toast(state.weixinQRMessage);
  }
}
async function openIMAuditSession(item) {
  const instanceID = item?.instance_id || item?.InstanceID || "";
  const sessionID = item?.session_id || item?.SessionID || "";
  if (!instanceID || !sessionID) return;
  resetRunState();
  state.view = "assistant";
  state.instanceId = instanceID;
  state.sessionId = sessionID;
  history.replaceState(null, "", `/app/?view=assistant&instance_id=${encodeURIComponent(instanceID)}`);
  await refreshInstances();
  await renderAssistant();
}
function bindIMAuditPanel() {
  const shell = document.querySelector(".im-audit-shell");
  if (shell) shell.ontoggle = () => { state.imAuditOpen = shell.open; };
  const refresh = $("imAuditRefresh");
  if (!refresh) return;
  const syncFilters = () => {
    state.imAuditPlatform = $("imAuditPlatform")?.value || "";
    state.imAuditContact = String($("imAuditContact")?.value || "").trim();
    state.imAuditQuery = String($("imAuditQuery")?.value || "").trim();
  };
  refresh.onclick = () => { syncFilters(); resetIMAuditPagination(); loadIMAuditMessages(); };
  const loadOlder = $("imAuditLoadOlder");
  if (loadOlder) loadOlder.onclick = () => loadIMAuditMessages(true);
  const exportBtn = $("imAuditExport");
  if (exportBtn) exportBtn.onclick = () => { syncFilters(); exportIMAuditCSV(); };
  const cleanupBtn = $("imAuditCleanup");
  if (cleanupBtn) cleanupBtn.onclick = () => { syncFilters(); cleanupIMAuditMessages(); };
  const cleanupBefore = $("imAuditCleanupBefore");
  if (cleanupBefore) cleanupBefore.onchange = () => { state.imAuditCleanupBefore = cleanupBefore.value; };
  document.querySelectorAll("[data-im-audit-days]").forEach((btn) => {
    btn.onclick = () => setIMAuditCleanupDays(btn.dataset.imAuditDays);
  });
  ["imAuditPlatform", "imAuditContact", "imAuditQuery"].forEach((id) => {
    const el = $(id);
    if (!el) return;
    el.onchange = () => { syncFilters(); resetIMAuditPagination(); if (id === "imAuditPlatform") state.imAuditContactsPlatform = null; loadIMAuditMessages(); };
    el.onkeydown = (e) => { if (e.key === "Enter") { e.preventDefault(); syncFilters(); resetIMAuditPagination(); loadIMAuditMessages(); } };
  });
  document.querySelectorAll("[data-im-audit-open]").forEach((btn) => {
    btn.onclick = () => openIMAuditSession(state.imAuditItems[Number(btn.dataset.imAuditOpen)]);
  });
  if (!state.imAuditLoaded && state.settingsTab === "im") loadIMAuditMessages();
}
function bindWeixinQRTools() {
  const btn = $("startWeixinQRLogin");
  if (btn) btn.onclick = startWeixinQRLogin;
  const cancel = $("cancelWeixinQRLogin");
  if (cancel) cancel.onclick = () => { resetWeixinQRLogin(); renderConfigFields(); };
}
async function loadWeixinRuntimeStatus() {
  try {
    state.weixinRuntime = await api("/api/v1/im/weixin/status");
  } catch (e) {
    if (e?.status === 401) throw e;
  }
  return state.weixinRuntime;
}
async function loadIMRuntimeStatuses() {
  try {
    const out = await api("/api/v1/im/status");
    state.imRuntimes = out?.items || {};
  } catch (e) {
    if (e?.status === 401) throw e;
  }
  return state.imRuntimes;
}
function setBoolControlValue(key, value) {
  const box = $(`cfg_${key}`);
  if (box?.dataset?.boolGroup === "true") {
    const radio = box.querySelector(`input[value="${value ? "true" : "false"}"]`);
    if (radio) radio.checked = true;
    return;
  }
  const el = $(`cfg_${key}`);
  if (el) el.value = value ? "true" : "false";
}
async function saveAndStartIM(enabledKey) {
  if (!enabledKey || state.imStartingKey) return;
  if (enabledKey === "weixin_enabled" && !imRequiredFieldsReady(enabledKey)) {
    toast(t("imWeixinStartScan"));
    await startWeixinQRLogin();
    return;
  }
  if (enabledKey === "thirdparty_gateway_enabled" && !String($("cfg_thirdparty_gateway_token")?.value || state.config?.thirdparty_gateway_token || "").trim()) {
    const token = randomSecretValue();
    const input = $("cfg_thirdparty_gateway_token");
    if (input && token) input.value = token;
  }
  const missing = missingIMRequiredFields(enabledKey);
  if (missing.length) {
    toast(t("imMissingRequired"));
    const first = $(`cfg_${missing[0]}`);
    if (first?.focus) first.focus();
    return;
  }
  state.imStartingKey = enabledKey;
  setBoolControlValue(enabledKey, true);
  try {
    const saved = await saveConfig({ toastMessage: t("channelSavedStarted") });
    if (!saved) return;
    if (enabledKey === "weixin_enabled" && imRequiredFieldsReady(enabledKey)) {
      await api("/api/v1/im/weixin/restart", { method: "POST", body: "{}" });
      await loadWeixinRuntimeStatus();
      renderConfigFields();
    }
  } finally {
    state.imStartingKey = "";
    renderConfigFields();
  }
}
async function disconnectIM(enabledKey) {
  if (!enabledKey || state.imStartingKey) return;
  state.imStartingKey = enabledKey;
  setBoolControlValue(enabledKey, false);
  try {
    await saveConfig({ toastMessage: t("channelDisconnected") });
  } finally {
    state.imStartingKey = "";
    renderConfigFields();
  }
}
function imCurrentFieldValue(key) {
  const el = $(`cfg_${key}`);
  if (el?.dataset?.boolGroup === "true") return el.querySelector("input:checked")?.value || "";
  return String(el?.value ?? state.config?.[key] ?? "").trim();
}
function missingIMRequiredFields(enabledKey) {
  return (IM_REQUIRED_FIELDS[enabledKey] || []).filter((key) => !imCurrentFieldValue(key));
}
function imRequiredFieldsReady(enabledKey) {
  return missingIMRequiredFields(enabledKey).length === 0;
}
function migrationBytes(n) {
  const v = Number(n || 0);
  if (!Number.isFinite(v) || v <= 0) return "-";
  if (v >= 1024 * 1024 * 1024) return `${(v / (1024 * 1024 * 1024)).toFixed(1)}G`;
  if (v >= 1024 * 1024) return `${(v / (1024 * 1024)).toFixed(1)}M`;
  if (v >= 1024) return `${(v / 1024).toFixed(1)}K`;
  return `${v}B`;
}
function migrationExportFromStatus() {
  return state.migrationStatus?.current_export || state.migrationStatus?.export || null;
}
function migrationPackageRows() {
  const rows = [...items(state.migrationInstances)];
  const exp = migrationExportFromStatus();
  const exportID = String(exp?.export_id || "");
  if (exportID && !rows.some((item) => String(item.export_id || "") === exportID)) {
    rows.push({
      instance_id: exp.source_instance_id || exp.source_machine_id || exportID,
      machine_id: exp.source_machine_id || exp.source_instance_id || "",
      machine_name: exp.source_machine_name || "",
      has_export: true,
      export_id: exportID,
      export_status: exp.status || "",
      export_claimed_by_machine_id: exp.claimed_by_machine_id || "",
      export_size: exp.compressed_size || exp.export_size || 0
    });
  }
  return rows.filter((item) => item.has_export && item.export_id);
}
function migrationImportableInstances() {
  const machineID = String(state.migrationStatus?.machine_id || "");
  return migrationPackageRows().filter((item) => {
    const status = String(item.export_status || "").toLowerCase();
    if (status === "ready") return true;
    const claimedByCurrent = machineID && String(item.export_claimed_by_machine_id || "") === machineID;
    return claimedByCurrent && ["importing", "imported", "deleting"].includes(status);
  });
}
function migrationSelectedInstance(exportID) {
  return migrationImportableInstances().find((item) => String(item.export_id || "") === String(exportID || ""));
}
function migrationIsCleanupRetry(item) {
  return ["imported", "deleting"].includes(String(item?.export_status || "").toLowerCase());
}
function renderMigrationManager() {
  const status = state.migrationStatus;
  const configured = status?.configured;
  const exp = migrationExportFromStatus();
  const importableInstances = migrationImportableInstances();
  const pct = Math.round(Number(state.migrationJob?.progress || 0) * 100);
  const jobText = state.migrationJob?.progress_text || state.migrationJob?.error || "";
  const machine = [status?.machine_name, status?.machine_id].filter(Boolean).join(" / ") || "-";
  const sourceOptions = importableInstances.map((item) => {
    const itemStatus = String(item.export_status || "").toLowerCase();
    const label = `${item.machine_name || item.instance_name || item.machine_id || item.instance_id} / ${migrationBytes(item.export_size)} / ${itemStatus || "ready"}`;
    return `<option value="${esc(item.export_id || "")}">${esc(label)}</option>`;
  }).join("");
  const exportSummary = exp && exp.status ? `${t("migrationHasExport")}: ${exp.source_machine_name || exp.source_machine_id || "-"} / ${migrationBytes(exp.compressed_size || exp.export_size)} / ${exp.status}` : t("migrationNoExport");
  const disabled = state.migrationLoading || state.migrationJob && ["pending", "running"].includes(String(state.migrationJob.status || ""));
  const disabledAttr = disabled || !configured ? "disabled" : "";
  const progress = state.migrationJob ? `<div class="migration-progress"><div class="split"><strong>${esc(t("migrationProgress"))}</strong><span>${esc(String(pct))}%</span></div><progress max="100" value="${esc(String(pct))}"></progress><span class="${state.migrationJob.status === "failed" ? "error" : "helper"}">${esc(jobText || state.migrationJob.status || "")}</span></div>` : "";
  const notReady = state.migrationLoading && !status ? `<p class="helper">${esc(t("migrationLoading"))}</p>` : configured ? "" : `<p class="error">${esc(status?.configuration_reason || t("migrationNotReady"))}</p>`;
  return `<section class="migration-manager" data-migration-manager><div class="split"><div><h3>${esc(t("migrationTitle"))}</h3><p class="helper">${esc(t("migrationHint"))}</p></div><button type="button" class="secondary" id="migrationRefreshBtn">${esc(t("migrationRefresh"))}</button></div>${notReady}<div class="migration-kv"><div><span>${esc(t("migrationCurrentHub"))}</span><strong>${esc(status?.hub_url || "-")}</strong></div><div><span>${esc(t("migrationCurrentTenant"))}</span><strong>${esc(status?.tenant_id || "-")}</strong></div><div><span>${esc(t("migrationCurrentMachine"))}</span><strong>${esc(machine)}</strong></div><div><span>${esc(t("migrationLimit"))}</span><strong>${esc(migrationBytes(status?.max_compressed_bytes))}</strong></div></div><div class="migration-warning">${esc(t("migrationOverwriteWarning"))}</div><div class="migration-grid"><section><h4>${esc(t("migrationStartExport"))}</h4><p class="helper">${esc(exportSummary)}</p><label>${esc(t("migrationExportPassword"))}<input id="migrationExportPassword" type="password" autocomplete="new-password" ${disabledAttr}></label><label>${esc(t("migrationExportPasswordConfirm"))}<input id="migrationExportPasswordConfirm" type="password" autocomplete="new-password" ${disabledAttr}></label><button type="button" class="primary" id="migrationExportBtn" ${disabledAttr}>${esc(t("migrationStartExport"))}</button></section><section><h4>${esc(t("migrationStartImport"))}</h4><label>${esc(t("migrationImportSource"))}<select id="migrationImportExportID" ${disabledAttr}><option value="">${esc(t("migrationSelectSource"))}</option>${sourceOptions}</select></label><label>${esc(t("migrationImportPassword"))}<input id="migrationImportPassword" type="password" autocomplete="current-password" ${disabledAttr}></label><button type="button" class="primary" id="migrationImportBtn" ${disabledAttr}>${esc(t("migrationStartImport"))}</button><p class="helper">${esc(t("migrationReadyOnly"))}</p></section></div>${progress}</section>`;
}
async function loadMigrationState() {
  if (state.migrationLoading) return;
  state.migrationLoading = true;
  try {
    const [status, list] = await Promise.all([
      api("/api/v1/migration/status"),
      api("/api/v1/migration/instances").catch(() => ({ instances: [] }))
    ]);
    state.migrationStatus = status;
    state.migrationInstances = items(list.instances || list.items || list);
  } catch (e) {
    state.migrationStatus = { configured: false, configuration_reason: e.message };
    state.migrationInstances = [];
    if (!handleAPIError(e)) toast(e.message);
  } finally {
    state.migrationLoading = false;
    if (state.view === "settings" && state.settingsTab === "migration" && $("cfgTabs")) renderConfigFields();
  }
}
function bindMigrationManager() {
  const root = document.querySelector("[data-migration-manager]");
  if (!root) return;
  const refresh = $("migrationRefreshBtn");
  if (refresh) refresh.onclick = () => loadMigrationState();
  const exportBtn = $("migrationExportBtn");
  if (exportBtn) exportBtn.onclick = startMigrationExport;
  const importBtn = $("migrationImportBtn");
  if (importBtn) importBtn.onclick = startMigrationImport;
  if (!state.migrationStatus && !state.migrationLoading) loadMigrationState();
}
async function startMigrationExport() {
  const password = String($("migrationExportPassword")?.value || "");
  const confirmPassword = String($("migrationExportPasswordConfirm")?.value || "");
  if (!password) return toast(t("migrationPasswordRequired"));
  if (password !== confirmPassword) return toast(t("migrationPasswordMismatch"));
  try {
    const out = await api("/api/v1/migration/export", { method: "POST", body: JSON.stringify({ password, password_confirm: confirmPassword, confirm_overwrite: true }) });
    toast(t("migrationExportStarted"));
    watchMigrationJob(out.job_id);
  } catch (e) {
    if (!handleAPIError(e)) toast(e.message);
  } finally {
    if ($("migrationExportPassword")) $("migrationExportPassword").value = "";
    if ($("migrationExportPasswordConfirm")) $("migrationExportPasswordConfirm").value = "";
  }
}
async function startMigrationImport() {
  const exportID = String($("migrationImportExportID")?.value || "").trim();
  const password = String($("migrationImportPassword")?.value || "");
  const cleanupRetry = migrationIsCleanupRetry(migrationSelectedInstance(exportID));
  if (!exportID) return toast(t("migrationSelectSource"));
  if (!password && !cleanupRetry) return toast(t("migrationPasswordRequired"));
  try {
    const out = await api("/api/v1/migration/import", { method: "POST", body: JSON.stringify({ export_id: exportID, password, cleanup_retry: cleanupRetry }) });
    toast(t("migrationImportStarted"));
    watchMigrationJob(out.job_id);
  } catch (e) {
    if (!handleAPIError(e)) toast(e.message);
  } finally {
    if ($("migrationImportPassword")) $("migrationImportPassword").value = "";
  }
}
async function watchMigrationJob(jobID) {
  if (!jobID) return;
  if (state.migrationJobTimer) clearTimeout(state.migrationJobTimer);
  try {
    const job = await api(`/api/v1/jobs/${encodeURIComponent(jobID)}`);
    state.migrationJob = job;
    if (state.view === "settings" && state.settingsTab === "migration" && $("cfgTabs")) renderConfigFields();
    if (["pending", "running"].includes(String(job.status || ""))) {
      state.migrationJobTimer = setTimeout(() => watchMigrationJob(jobID), 1200);
      return;
    }
    toast(job.status === "succeeded" ? t("migrationCompleted") : t("migrationFailed"));
    await loadMigrationState();
  } catch (e) {
    if (!handleAPIError(e)) toast(e.message);
  }
}
function configGroups(defs) {
  const visibleDefs = defs.filter((x) => !HIDDEN_CONFIG_KEYS.has(x.key));
  const byKey = Object.fromEntries(visibleDefs.map((x) => [x.key, x]));
  const allKeys = [...new Set([...visibleDefs.map((x) => x.key), ...Object.keys(state.config || {}).filter((key) => !HIDDEN_CONFIG_KEYS.has(key))])];
  const groups = [
    { id: "llm", title: t("groupLLM"), hint: t("groupLLMHint"), keys: ["maclaw_llm_url", "maclaw_llm_key", "maclaw_llm_model"] },
    { id: "tools", title: t("groupTools"), hint: t("groupToolsHint"), keys: ["web_search_providers", "web_search_current_provider"] },
    { id: "skills", title: t("groupSkills"), hint: t("groupSkillsHint"), keys: [] },
    { id: "memory", title: t("groupMemory"), hint: t("groupMemoryHint"), keys: ["memory_auto_compress", "memory_max_backups", "knowledge_skill_token_budget"] },
    { id: "migration", title: t("groupMigration"), hint: t("groupMigrationHint"), keys: [] },
    { id: "security", title: t("groupSecurity"), hint: t("groupSecurityHint"), keys: ["security_policy_mode", "sandbox_mode", "network_level", "yolo_mode_allowed"] },
    { id: "im", title: t("groupIM"), hint: t("groupIMHint"), keys: CHANNEL_CONFIG_KEYS },
  ];
  const used = new Set(groups.flatMap((g) => g.keys));
  const rest = allKeys.filter((key) => !used.has(key));
  const pick = (pred) => rest.filter(pred);
  const interfaceKeys = pick((key) => key === "language");
  const proxyKeys = pick((key) => /^(default_proxy_)/.test(key));
  groups.push(
    { id: "interface", title: t("groupInterface"), hint: t("groupInterfaceHint"), keys: interfaceKeys },
    { id: "proxy", title: t("groupProxy"), hint: t("groupProxyHint"), keys: proxyKeys }
  );
  return groups.map((g) => ({ ...g, keys: g.keys.filter((key) => !HIDDEN_CONFIG_KEYS.has(key) && (byKey[key] || Object.prototype.hasOwnProperty.call(state.config || {}, key))) })).filter((g) => g.keys.length || ["tools", "skills", "memory", "migration", "im"].includes(g.id));
}
function setActiveConfigTab(tab) {
  tab = normalizeSettingsTab(tab);
  state.settingsTab = tab;
  document.querySelectorAll("[data-cfg-tab]").forEach((b) => { const on = b.dataset.cfgTab === tab; b.classList.toggle("active", on); b.setAttribute("aria-selected", on ? "true" : "false"); });
  document.querySelectorAll("[data-cfg-panel]").forEach((p) => { const off = p.dataset.cfgPanel !== tab; p.hidden = off; p.setAttribute("aria-hidden", off ? "true" : "false"); });
  if (tab === "memory" && $("memoryList")) loadMemoryEntries(false);
  if (tab === "migration" && $("migrationRefreshBtn")) loadMigrationState();
  if (tab === "im" && $("imAuditList") && !state.imAuditLoaded) loadIMAuditMessages();
}
function bindChoiceCustomControls() {
  document.querySelectorAll(".choice-custom").forEach((box) => {
    const select = box.querySelector("[data-choice-suggest]");
    const input = box.querySelector("[data-choice-custom]");
    if (!select || !input) return;
    const sync = () => {
      const custom = select.value === "__custom__";
      box.classList.toggle("custom-active", custom);
      input.disabled = !custom;
      if (custom) input.removeAttribute("aria-hidden"); else input.setAttribute("aria-hidden", "true");
    };
    select.onchange = sync;
    sync();
  });
  document.querySelectorAll("[data-list-kind='longtext-choice']").forEach((box) => {
    const select = box.querySelector("[data-longtext-suggest]");
    const detail = box.querySelector(".custom-lines");
    if (!select || !detail) return;
    const sync = () => { if (select.value === "__custom__") detail.open = true; };
    select.onchange = sync;
    sync();
  });
  document.querySelectorAll("[data-choice-lines-action]").forEach((button) => {
    button.onclick = () => {
      const box = button.closest("[data-list-kind='choice-lines']");
      const select = box?.querySelector("[data-array-suggest]");
      if (!select) return;
      const on = button.dataset.choiceLinesAction === "all";
      [...select.options].forEach((opt) => { opt.selected = on; });
      select.dispatchEvent(new Event("change", { bubbles: true }));
    };
  });
  document.querySelectorAll(".kv-pair").forEach((pair) => {
    const bind = (selectName, inputName, className) => {
      const select = pair.querySelector(`[${selectName}]`);
      const input = pair.querySelector(`[${inputName}]`);
      if (!select || !input) return;
      const sync = () => {
        const custom = select.value === "__custom__";
        pair.classList.toggle(className, custom);
        input.disabled = !custom;
        if (custom) input.removeAttribute("aria-hidden"); else input.setAttribute("aria-hidden", "true");
      };
      select.onchange = sync;
      sync();
    };
    bind("data-kv-key", "data-kv-key-custom", "custom-key-active");
    bind("data-kv-value", "data-kv-value-custom", "custom-value-active");
    bind("data-generic-object-key", "data-generic-object-key-custom", "custom-key-active");
    bind("data-generic-object-value", "data-generic-object-value-custom", "custom-value-active");
  });
}
function moveConfigTab(current, delta) {
  const tabs = [...document.querySelectorAll("[data-cfg-tab]")];
  const idx = tabs.indexOf(current);
  if (idx < 0 || tabs.length === 0) return;
  const next = tabs[(idx + delta + tabs.length) % tabs.length];
  setActiveConfigTab(next.dataset.cfgTab);
  next.focus();
}
function fieldValue(key, def = {}) {
  const value = state.config[key];
  if (def.type === "array" || def.type === "object") return pretty(value || (def.type === "array" ? [] : {}));
  if (def.type === "bool") return value === true ? "true" : value === false ? "false" : "";
  return value ?? "";
}
function configChoiceLabel(key, value) {
  return CONFIG_CHOICE_LABELS[locale]?.[key]?.[value] || CONFIG_CHOICE_LABELS.en[key]?.[value] || value;
}
function genericChoiceOptions(key) {
  const k = String(key || "").toLowerCase();
  if (k.endsWith("mode") || k.endsWith("_mode")) return GENERIC_CHOICE_FIELDS.mode;
  if (k.endsWith("policy") || k.endsWith("_policy")) return GENERIC_CHOICE_FIELDS.policy;
  if (k.endsWith("type") || k.endsWith("_type")) return GENERIC_CHOICE_FIELDS.type;
  if (k.endsWith("status") || k.endsWith("_status")) return GENERIC_CHOICE_FIELDS.status;
  if (k.endsWith("source") || k.endsWith("_source")) return GENERIC_CHOICE_FIELDS.source;
  return [];
}
function providerChoiceOptions(key) {
  const listKey = key === "web_search_current_provider" ? "web_search_providers" : "";
  if (!listKey) return null;
  return items(state.config?.[listKey]).map((x) => String(x.name || x.Name || "").trim()).filter(Boolean);
}
function stringChoiceInput(key, value) {
  const options = CONFIG_CHOICE_FIELDS[key] || providerChoiceOptions(key) || genericChoiceOptions(key) || [];
  const current = String(value || "");
  const all = current && !options.includes(current) ? [...options, current] : options;
  return `<select id="cfg_${key}" data-key="${esc(key)}" data-type="string" data-unset-empty="true"><option value="" ${current === "" ? "selected" : ""}>${t("unset")}</option>${all.map((opt) => `<option value="${esc(opt)}" ${current === opt ? "selected" : ""}>${esc(configChoiceLabel(key, opt))}${options.includes(opt) ? "" : ` (${esc(opt)})`}</option>`).join("")}</select>`;
}
function numberChoiceInput(key, value, type) {
  const options = CONFIG_NUMBER_CHOICE_FIELDS[key] || genericNumberOptions(key) || [];
  const current = String(value ?? "");
  const normalized = options.map((opt) => String(opt));
  const all = current && !normalized.includes(current) ? [...normalized, current] : normalized;
  return `<select id="cfg_${key}" data-key="${esc(key)}" data-type="${esc(type)}" data-unset-empty="true"><option value="" ${current === "" ? "selected" : ""}>${t("unset")}</option>${all.map((opt) => `<option value="${esc(opt)}" ${current === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}</select>`;
}
function numberChoiceCustomMarkup(id, attrs, current, suggestions) {
  const value = String(current ?? "").trim();
  const options = suggestions.map((opt) => String(opt));
  const selected = options.includes(value) ? value : "";
  const custom = selected ? "" : value;
  return `<div id="${esc(id)}" ${attrs} data-list-kind="choice-custom" class="choice-custom ${custom ? "custom-active" : ""}"><select data-choice-suggest><option value="" ${selected === "" && !custom ? "selected" : ""}>${t("unset")}</option>${options.map((opt) => `<option value="${esc(opt)}" ${selected === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}<option value="__custom__" ${custom ? "selected" : ""}>${t("customValue")}</option></select><input type="number" data-choice-custom value="${esc(custom)}" aria-label="${esc(id)} custom"></div>`;
}
function arrayChoiceInput(key) {
  const options = CONFIG_ARRAY_CHOICE_FIELDS[key] || [];
  const current = items(state.config?.[key]).map((v) => String(v).trim()).filter(Boolean);
  const all = [...new Set([...options, ...current])];
  return `<select id="cfg_${key}" data-key="${esc(key)}" data-type="array-choice" multiple size="${Math.min(Math.max(all.length, 3), 6)}">${all.map((opt) => `<option value="${esc(opt)}" ${current.includes(opt) ? "selected" : ""}>${esc(configChoiceLabel(key, opt))}${options.includes(opt) ? "" : ` (${esc(opt)})`}</option>`).join("")}</select>`;
}
function choiceLinesMarkup(id, attrs, currentValues, suggestions, stringMode = false) {
  const current = currentValues.map((v) => String(v).trim()).filter(Boolean);
  const selected = current.filter((v) => suggestions.includes(v));
  const custom = current.filter((v) => !suggestions.includes(v)).join("\n");
  const type = stringMode ? "string-choice-lines" : "array-choice-lines";
  return `<div id="${esc(id)}" ${attrs} data-type="${type}" data-list-kind="choice-lines" class="choice-lines"><div class="choice-select-stack"><select data-array-suggest multiple size="${Math.min(Math.max(suggestions.length, 3), 7)}">${suggestions.map((opt) => `<option value="${esc(opt)}" ${selected.includes(opt) ? "selected" : ""}>${esc(opt)}</option>`).join("")}</select><div class="choice-actions"><button type="button" class="secondary" data-choice-lines-action="all">${t("selectAll")}</button><button type="button" class="secondary" data-choice-lines-action="clear">${t("clearSelection")}</button></div></div><details class="custom-lines" ${custom ? "open" : ""}><summary>${t("customValue")}</summary><textarea data-array-custom>${esc(custom)}</textarea></details></div>`;
}
function choiceCustomMarkup(id, attrs, current, suggestions) {
  const value = String(current ?? "").trim();
  const selected = suggestions.includes(value) ? value : "";
  const custom = selected ? "" : value;
  return `<div id="${esc(id)}" ${attrs} data-list-kind="choice-custom" class="choice-custom ${custom ? "custom-active" : ""}"><select data-choice-suggest><option value="" ${selected === "" && !custom ? "selected" : ""}>${t("unset")}</option>${suggestions.map((opt) => `<option value="${esc(opt)}" ${selected === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}<option value="__custom__" ${custom ? "selected" : ""}>${t("customValue")}</option></select><input type="text" data-choice-custom value="${esc(custom)}" aria-label="${esc(id)} custom"></div>`;
}
function kvChoiceSelect(attr, customAttr, current, suggestions, ariaLabel) {
  const value = String(current ?? "").trim();
  const options = [...new Set(suggestions.map((x) => String(x)).filter(Boolean))];
  const selected = options.includes(value) ? value : "";
  const custom = selected ? "" : value;
  return `<select ${attr} aria-label="${esc(ariaLabel)}"><option value="" ${selected === "" && !custom ? "selected" : ""}>${t("unset")}</option>${options.map((opt) => `<option value="${esc(opt)}" ${selected === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}<option value="__custom__" ${custom ? "selected" : ""}>${t("customValue")}</option></select><input type="text" ${customAttr} value="${esc(custom)}" aria-label="${esc(ariaLabel)} custom">`;
}
function longTextChoiceMarkup(id, attrs, current, suggestions) {
  const selected = suggestions.includes(current) ? current : "";
  const custom = selected ? "" : current;
  return `<div id="${esc(id)}" ${attrs} data-list-kind="longtext-choice" class="choice-lines"><select data-longtext-suggest><option value="" ${selected === "" && !custom ? "selected" : ""}>${t("unset")}</option>${suggestions.map((opt) => `<option value="${esc(opt)}" ${selected === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}<option value="__custom__" ${custom ? "selected" : ""}>${t("customValue")}</option></select><details class="custom-lines" ${custom ? "open" : ""}><summary>${t("customValue")}</summary><textarea data-longtext-custom>${esc(custom)}</textarea></details></div>`;
}
function lineArrayInput(key) {
  const value = items(state.config?.[key]).map((v) => String(v)).join("\n");
  const suggestions = CONFIG_LINE_ARRAY_SUGGESTION_FIELDS[key] || genericLineSuggestions(key);
  if (suggestions.length) {
    return choiceLinesMarkup(`cfg_${key}`, `data-key="${esc(key)}"`, items(state.config?.[key]), suggestions);
  }
  return choiceLinesMarkup(`cfg_${key}`, `data-key="${esc(key)}"`, value.split(/\r?\n/), COMMON_LINE_FALLBACK_SUGGESTIONS);
}
function stringLineInput(key) {
  const value = String(state.config?.[key] || "").split(/[;\n]/).map((v) => v.trim()).filter(Boolean).join("\n");
  const suggestions = CONFIG_STRING_LINE_SUGGESTION_FIELDS[key] || genericLineSuggestions(key);
  if (suggestions.length) {
    return choiceLinesMarkup(`cfg_${key}`, `data-key="${esc(key)}"`, value.split(/\r?\n/), suggestions, true);
  }
  return choiceLinesMarkup(`cfg_${key}`, `data-key="${esc(key)}"`, value.split(/\r?\n/), COMMON_LINE_FALLBACK_SUGGESTIONS, true);
}
function genericArrayInput(key) {
  const value = items(state.config?.[key]).map((v) => String(v)).join("\n");
  const suggestions = genericLineSuggestions(key);
  if (suggestions.length) return choiceLinesMarkup(`cfg_${key}`, `data-key="${esc(key)}"`, items(state.config?.[key]), suggestions);
  return choiceLinesMarkup(`cfg_${key}`, `data-key="${esc(key)}"`, value.split(/\r?\n/), COMMON_LINE_FALLBACK_SUGGESTIONS);
}
function shallowObject(value) {
  return value && typeof value === "object" && !Array.isArray(value) && Object.values(value).every((v) => v === null || typeof v !== "object");
}
function scalarLeafObject(value) {
  if (!value || typeof value !== "object" || Array.isArray(value)) return false;
  return Object.values(value).every((v) => v === null || typeof v !== "object" || scalarLeafObject(v));
}
function flattenObjectLeaves(value, prefix = "") {
  const out = [];
  Object.entries(value || {}).forEach(([key, val]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    if (val && typeof val === "object" && !Array.isArray(val)) out.push(...flattenObjectLeaves(val, path));
    else out.push([path, String(val ?? "")]);
  });
  return out;
}
function setPlainObjectPath(target, field, value) {
  const parts = String(field || "").split(".").filter(Boolean);
  if (!parts.length) return;
  let cur = target;
  parts.slice(0, -1).forEach((part) => {
    if (!cur[part] || typeof cur[part] !== "object" || Array.isArray(cur[part])) cur[part] = {};
    cur = cur[part];
  });
  cur[parts[parts.length - 1]] = value;
}
function plainObjectPathValue(target, field) {
  const parts = String(field || "").split(".").filter(Boolean);
  let cur = target;
  for (const part of parts) {
    if (!cur || typeof cur !== "object" || Array.isArray(cur)) return undefined;
    cur = cur[part];
  }
  return cur;
}
function coercePlainObjectValue(current, value) {
  const raw = String(value ?? "").trim();
  if (typeof current === "boolean") return raw === "true" ? true : raw === "false" ? false : raw;
  if (typeof current === "number") {
    const parsed = Number(raw);
    return Number.isFinite(parsed) ? parsed : raw;
  }
  if (current === null && raw === "null") return null;
  return raw;
}
function genericObjectInput(key) {
  const raw = state.config?.[key];
  const source = raw === undefined || raw === null ? {} : scalarLeafObject(raw) ? raw : null;
  if (!source) return `<details class="raw-json-editor" open><summary>${t("customValue")}</summary><textarea id="cfg_${key}" data-key="${esc(key)}" data-type="object">${esc(fieldValue(key, { type: "object" }))}</textarea></details>`;
  const pairs = shallowObject(source) ? Object.entries(source).map(([k, v]) => [String(k), String(v ?? "")]) : flattenObjectLeaves(source);
  const rowCount = Math.max(4, pairs.length + 2);
  const keyChoices = [...GENERIC_OBJECT_KEY_SUGGESTIONS, ...pairs.map(([k]) => k)];
  const valueChoices = genericLineSuggestions(key).concat(genericSuggestionOptions(key));
  const controls = Array.from({ length: rowCount }, (_, idx) => {
    const [k, v] = pairs[idx] || ["", ""];
    return `<div class="kv-pair ${keyChoices.includes(k) ? "" : k ? "custom-key-active" : ""} ${valueChoices.includes(v) ? "" : v ? "custom-value-active" : ""}">${kvChoiceSelect("data-generic-object-key", "data-generic-object-key-custom", k, keyChoices, `${key} key`)}${kvChoiceSelect("data-generic-object-value", "data-generic-object-value-custom", v, valueChoices, `${key} value`)}</div>`;
  }).join("");
  const deep = shallowObject(source) ? "" : ' data-deep-object="true"';
  return `<div id="cfg_${key}" data-key="${esc(key)}" data-type="object-kv"${deep} class="kv-list">${controls}</div>`;
}
function objectFieldValue(item, field) {
  if (!item || typeof item !== "object") return "";
  if (field.includes(".")) return field.split(".").reduce((cur, part) => cur && typeof cur === "object" ? cur[part] : undefined, item) ?? "";
  const camel = field.replace(/_([a-z])/g, (_, ch) => ch.toUpperCase());
  const pascal = camel ? camel[0].toUpperCase() + camel.slice(1) : field;
  return item[field] ?? item[camel] ?? item[pascal] ?? "";
}
function objectSubInput(scope, row, f, raw) {
  const id = `cfg_${scope}_${row}_${f.key}`;
  const current = Array.isArray(raw) ? raw.join("\n") : String(raw ?? "");
  const attrs = `data-list-key="${esc(scope)}" data-list-row="${row}" data-list-field="${esc(f.key)}"`;
  if (f.kind === "select") return `<label for="${esc(id)}">${esc(f.key)}<select id="${esc(id)}" ${attrs}><option value="" ${current === "" ? "selected" : ""}>${t("unset")}</option>${f.options.filter(Boolean).map((opt) => `<option value="${esc(opt)}" ${current === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}</select></label>`;
  if (f.kind === "text" && genericChoiceOptions(f.key).length) {
    const options = genericChoiceOptions(f.key);
    const all = current && !options.includes(current) ? [...options, current] : options;
    return `<label for="${esc(id)}">${esc(f.key)}<select id="${esc(id)}" ${attrs}><option value="" ${current === "" ? "selected" : ""}>${t("unset")}</option>${all.map((opt) => `<option value="${esc(opt)}" ${current === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}</select></label>`;
  }
  if (f.kind === "provider") {
    const options = providerChoiceOptions("maclaw_llm_current_provider") || [];
    const all = current && !options.includes(current) ? [...options, current] : options;
    return `<label for="${esc(id)}">${esc(f.key)}<select id="${esc(id)}" ${attrs}><option value="" ${current === "" ? "selected" : ""}>${t("unset")}</option>${all.map((opt) => `<option value="${esc(opt)}" ${current === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}</select></label>`;
  }
  if (f.kind === "number" && f.options?.length) {
    const normalized = f.options.map((opt) => String(opt));
    const all = current && !normalized.includes(current) ? [...normalized, current] : normalized;
    return `<label for="${esc(id)}">${esc(f.key)}<select id="${esc(id)}" ${attrs}><option value="" ${current === "" ? "selected" : ""}>${t("unset")}</option>${all.map((opt) => `<option value="${esc(opt)}" ${current === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}</select></label>`;
  }
  if (f.kind === "number" && genericNumberOptions(f.key).length) {
    const options = genericNumberOptions(f.key).map((opt) => String(opt));
    const all = current && !options.includes(current) ? [...options, current] : options;
    return `<label for="${esc(id)}">${esc(f.key)}<select id="${esc(id)}" ${attrs}><option value="" ${current === "" ? "selected" : ""}>${t("unset")}</option>${all.map((opt) => `<option value="${esc(opt)}" ${current === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}</select></label>`;
  }
  if (f.kind === "number") return `<label>${esc(f.key)}${numberChoiceCustomMarkup(id, attrs, current, COMMON_NUMBER_FALLBACK_SUGGESTIONS)}</label>`;
  if (f.kind === "bool") return `<label for="${esc(id)}">${esc(f.key)}<select id="${esc(id)}" ${attrs}><option value="" ${current === "" ? "selected" : ""}>${t("unset")}</option><option value="true" ${current === "true" ? "selected" : ""}>${t("trueValue")}</option><option value="false" ${current === "false" ? "selected" : ""}>${t("falseValue")}</option></select></label>`;
  if (f.kind === "multi") {
    const selected = Array.isArray(raw) ? raw.map((x) => String(x)) : current.split(/\r?\n/).map((x) => x.trim()).filter(Boolean);
    const all = [...new Set([...(f.options || []), ...selected])];
    return `<label for="${esc(id)}">${esc(f.key)}<select id="${esc(id)}" ${attrs} multiple size="${Math.min(Math.max(all.length, 3), 6)}">${all.map((opt) => `<option value="${esc(opt)}" ${selected.includes(opt) ? "selected" : ""}>${esc(opt)}</option>`).join("")}</select></label>`;
  }
  if (f.kind === "kv") {
    const source = raw && typeof raw === "object" && !Array.isArray(raw) ? raw : {};
    const pairs = Object.entries(source).map(([k, v]) => [String(k), String(v ?? "")]);
    const rowCount = Math.max(3, pairs.length + 1);
    const keyChoices = [...(f.keySuggestions || []), ...pairs.map(([k]) => k)];
    const valueChoices = [...(f.valueSuggestions || []), ...pairs.map(([, v]) => v)];
    const controls = Array.from({ length: rowCount }, (_, idx) => {
      const [k, v] = pairs[idx] || ["", ""];
      return `<div class="kv-pair ${keyChoices.includes(k) ? "" : k ? "custom-key-active" : ""} ${valueChoices.includes(v) ? "" : v ? "custom-value-active" : ""}">${kvChoiceSelect("data-kv-key", "data-kv-key-custom", k, keyChoices, `${f.key} key`)}${kvChoiceSelect("data-kv-value", "data-kv-value-custom", v, valueChoices, `${f.key} value`)}</div>`;
    }).join("");
    return `<label>${esc(f.key)}<div id="${esc(id)}" ${attrs} data-list-kind="kv" class="kv-list">${controls}</div></label>`;
  }
  if (f.kind === "lines") {
    const suggestions = f.suggestions?.length ? f.suggestions : genericLineSuggestions(f.key);
    if (suggestions.length) return `<label>${esc(f.key)}${choiceLinesMarkup(id, attrs, Array.isArray(raw) ? raw : current.split(/\r?\n/), suggestions)}</label>`;
    return `<label>${esc(f.key)}${choiceLinesMarkup(id, attrs, Array.isArray(raw) ? raw : current.split(/\r?\n/), COMMON_LINE_FALLBACK_SUGGESTIONS)}</label>`;
  }
  if (f.kind === "longtext" && f.suggestions?.length) return `<label>${esc(f.key)}${longTextChoiceMarkup(id, attrs, current, f.suggestions)}</label>`;
  if (f.kind === "longtext") return `<label>${esc(f.key)}${longTextChoiceMarkup(id, attrs, current, ROLE_DESCRIPTION_SUGGESTIONS)}</label>`;
  const suggestions = f.suggestions?.length ? f.suggestions : genericSuggestionOptions(f.key);
  if (suggestions.length) {
    const all = current && !suggestions.includes(current) ? [...suggestions, current] : suggestions;
    return `<label>${esc(f.key)}${choiceCustomMarkup(id, attrs, current, all)}</label>`;
  }
  if (f.kind === "password") return `<label for="${esc(id)}">${esc(f.key)}${secretFieldMarkup(id, attrs, current)}</label>`;
  return `<label>${esc(f.key)}${choiceCustomMarkup(id, attrs, current, COMMON_TEXT_FALLBACK_SUGGESTIONS)}</label>`;
}
function objectListInput(key) {
  const def = CONFIG_OBJECT_LIST_FIELDS[key];
  const existing = items(state.config?.[key]).map((v) => v && typeof v === "object" ? v : {});
  const rowCount = Math.max(def.rows || 1, existing.length + 1);
  const rows = Array.from({ length: rowCount }, (_, row) => {
    const item = existing[row] || {};
    return `<div class="object-row">${def.fields.map((f) => objectSubInput(key, row, f, objectFieldValue(item, f.key))).join("")}</div>`;
  }).join("");
  return `<div id="cfg_${key}" data-object-list="${esc(key)}" class="object-list">${rows}</div>`;
}
function objectFormInput(key) {
  const def = CONFIG_OBJECT_FIELDS[key];
  const item = state.config?.[key] && typeof state.config[key] === "object" ? state.config[key] : {};
  return `<div id="cfg_${key}" data-object-form="${esc(key)}" class="object-list"><div class="object-row">${def.fields.map((f) => objectSubInput(key, 0, f, objectFieldValue(item, f.key))).join("")}</div></div>`;
}
function objectMapInput(key) {
  const def = CONFIG_OBJECT_MAP_FIELDS[key];
  const existing = state.config?.[key] && typeof state.config[key] === "object" ? state.config[key] : {};
  const routeKeys = [...new Set([...def.keyOptions, ...Object.keys(existing)])];
  const rowCount = Math.max(def.rows || 1, routeKeys.length + 1);
  const rows = Array.from({ length: rowCount }, (_, row) => {
    const route = routeKeys[row] || "";
    const item = route ? existing[route] || {} : {};
    const allRoutes = route && !def.keyOptions.includes(route) ? [...def.keyOptions, route] : def.keyOptions;
    const routeInput = `<label for="cfg_${key}_${row}_route">${esc(def.keyName)}<select id="cfg_${key}_${row}_route" data-map-key="${esc(key)}" data-map-row="${row}"><option value="" ${route === "" ? "selected" : ""}>${t("unset")}</option>${allRoutes.map((opt) => `<option value="${esc(opt)}" ${route === opt ? "selected" : ""}>${esc(opt)}</option>`).join("")}</select></label>`;
    return `<div class="object-row">${routeInput}${def.fields.map((f) => objectSubInput(key, row, f, objectFieldValue(item, f.key))).join("")}</div>`;
  }).join("");
  return `<div id="cfg_${key}" data-object-map="${esc(key)}" class="object-list">${rows}</div>`;
}
function jsonStringObjectInput(key) {
  const def = CONFIG_JSON_STRING_OBJECT_FIELDS[key];
  let item = {};
  try { item = JSON.parse(state.config?.[key] || "{}"); } catch { item = {}; }
  return `<div id="cfg_${key}" data-json-string-object="${esc(key)}" class="object-list"><div class="object-row">${def.fields.map((f) => objectSubInput(key, 0, f, objectFieldValue(item, f.key))).join("")}</div></div>`;
}
function suggestionInput(key, def, value) {
  const options = CONFIG_SUGGESTION_FIELDS[key] || genericSuggestionOptions(key) || [];
  const current = String(value ?? "").trim();
  const all = current && !options.includes(current) ? [...options, current] : options;
  const listID = `cfg_${key}_choices`;
  if (def.secret) return `${secretFieldMarkup(`cfg_${key}`, `data-key="${esc(key)}" data-type="string"`, value, `list="${esc(listID)}"`)}<datalist id="${esc(listID)}">${all.map((opt) => `<option value="${esc(opt)}"></option>`).join("")}</datalist>`;
  return choiceCustomMarkup(`cfg_${key}`, `data-key="${esc(key)}" data-type="string-choice-custom"`, current, all);
}
function audioDeviceInput(key, value) {
  const current = String(value || "");
  return `<select id="cfg_${key}" data-key="${esc(key)}" data-type="string" data-audio-device="${key === "audio_output_device_id" ? "audiooutput" : "audioinput"}"><option value="" ${current === "" ? "selected" : ""}>${t("unset")}</option>${current ? `<option value="${esc(current)}" selected>${esc(current)}</option>` : ""}</select>`;
}
async function bindAudioDeviceInputs() {
  const selects = [...document.querySelectorAll("[data-audio-device]")];
  if (!selects.length || !navigator.mediaDevices?.enumerateDevices) return;
  try {
    const devices = await navigator.mediaDevices.enumerateDevices();
    selects.forEach((select) => {
      const kind = select.dataset.audioDevice;
      const current = select.value;
      const matches = devices.filter((device) => device.kind === kind);
      const opts = [`<option value="" ${current === "" ? "selected" : ""}>${t("unset")}</option>`].concat(matches.map((device, idx) => {
        const label = device.label || `${kind} ${idx + 1}`;
        return `<option value="${esc(device.deviceId)}" ${current === device.deviceId ? "selected" : ""}>${esc(label)}</option>`;
      }));
      if (current && !matches.some((device) => device.deviceId === current)) opts.push(`<option value="${esc(current)}" selected>${esc(current)}</option>`);
      select.innerHTML = opts.join("");
    });
  } catch {}
}
function boolRadioInput(key, value) {
  const name = `cfg_${key}`;
  const on = value === "true";
  const options = [["true", t("trueValue")], ["false", t("falseValue")]];
  return `<div id="${esc(name)}" class="bool-radio bool-radio-two" data-bool-group="true" data-key="${esc(key)}" data-type="bool" role="radiogroup" aria-label="${esc(fieldMeta({ key, title: key }).title || key)}">${options.map(([val, label]) => `<label><input type="radio" name="${esc(name)}" value="${esc(val)}" ${on === (val === "true") ? "checked" : ""}><span>${esc(label)}</span></label>`).join("")}</div>`;
}
function fieldInput(key, def) {
  const value = esc(fieldValue(key, def));
  const secret = def.secret || isLikelySecretKey(key);
  if (def.type === "array" && CONFIG_OBJECT_LIST_FIELDS[key]) return objectListInput(key);
  if (def.type === "object" && CONFIG_OBJECT_FIELDS[key]) return objectFormInput(key);
  if (def.type === "object" && CONFIG_OBJECT_MAP_FIELDS[key]) return objectMapInput(key);
  if (def.type === "array" && CONFIG_ARRAY_CHOICE_FIELDS[key]) return arrayChoiceInput(key);
  if (def.type === "array" && CONFIG_LINE_ARRAY_FIELDS.has(key)) return lineArrayInput(key);
  if (def.type === "array") return genericArrayInput(key);
  if (def.type === "object") return genericObjectInput(key);
  if (def.type === "bool" && IM_ENABLED_KEYS.has(key)) return boolRadioInput(key, value);
  if (def.type === "bool") return `<select id="cfg_${key}" data-key="${esc(key)}" data-type="bool"><option value="" ${value === "" ? "selected" : ""}>${t("unset")}</option><option value="true" ${value === "true" ? "selected" : ""}>${t("trueValue")}</option><option value="false" ${value === "false" ? "selected" : ""}>${t("falseValue")}</option></select>`;
  if ((def.type === "integer" || def.type === "number") && CONFIG_NUMBER_CHOICE_FIELDS[key]) return numberChoiceInput(key, fieldValue(key, def), def.type);
  if ((def.type === "integer" || def.type === "number") && genericNumberOptions(key).length) return numberChoiceInput(key, fieldValue(key, def), def.type);
  if (def.type === "integer" || def.type === "number") return numberChoiceCustomMarkup(`cfg_${key}`, `data-key="${esc(key)}" data-type="${esc(def.type)}"`, fieldValue(key, def), COMMON_NUMBER_FALLBACK_SUGGESTIONS);
  if (PLAIN_TEXT_CONFIG_FIELDS.has(key)) return `<input id="cfg_${key}" data-key="${esc(key)}" data-type="string" value="${value}" autocomplete="off" spellcheck="false">`;
  if (CONFIG_CHOICE_FIELDS[key] || providerChoiceOptions(key) || genericChoiceOptions(key).length) return stringChoiceInput(key, fieldValue(key, def));
  if (CONFIG_JSON_STRING_OBJECT_FIELDS[key]) return jsonStringObjectInput(key);
  if (CONFIG_STRING_LINE_FIELDS.has(key)) return stringLineInput(key);
  if (key === "audio_input_device_id" || key === "audio_output_device_id") return audioDeviceInput(key, fieldValue(key, def));
  if (secret) return secretFieldMarkup(`cfg_${key}`, `data-key="${esc(key)}" data-type="string"`, fieldValue(key, def));
  if (CONFIG_SUGGESTION_FIELDS[key] || genericSuggestionOptions(key).length) return suggestionInput(key, def, fieldValue(key, def));
  return choiceCustomMarkup(`cfg_${key}`, `data-key="${esc(key)}" data-type="string-choice-custom"`, fieldValue(key, def), COMMON_TEXT_FALLBACK_SUGGESTIONS);
}
function fieldHelper(d) {
  const text = d.description || d.example || "";
  const extra = d.secret ? fieldSecretHint(d) : "";
  return [text, extra].filter(Boolean).join(" ");
}
function fieldSecretHint(d) {
  if (d.key === "maclaw_llm_key") {
    return locale === "en"
      ? "Masked value keeps the existing access token. Enter a new value only when replacing it."
      : "显示为掩码时会保留现有访问令牌；只有需要替换时才输入新值。";
  }
  return t("secretHint");
}
function parseConfigNumber(key, value, integer) {
  const raw = String(value || "").trim();
  if (raw === "") return undefined;
  const next = Number(raw);
  if (!Number.isFinite(next) || (integer && !Number.isInteger(next))) throw new Error(t("numberInvalid", { key: configIssueLabel({ key }), type: configTypeName(integer ? "integer" : "number") }));
  return next;
}
function objectElementValue(el) {
  if (el?.dataset?.boolGroup === "true") {
    return String(el.querySelector("input[type='radio']:checked")?.value || "").trim();
  }
  if (el?.dataset?.listKind === "kv") {
    const out = {};
    el.querySelectorAll(".kv-pair").forEach((pair) => {
      const keySelect = String(pair.querySelector("[data-kv-key]")?.value || "");
      const valueSelect = String(pair.querySelector("[data-kv-value]")?.value || "");
      const key = String(keySelect === "__custom__" ? pair.querySelector("[data-kv-key-custom]")?.value || "" : keySelect).trim();
      const value = String(valueSelect === "__custom__" ? pair.querySelector("[data-kv-value-custom]")?.value || "" : valueSelect).trim();
      if (key && value) out[key] = value;
    });
    return Object.keys(out).length ? out : null;
  }
  if (el?.dataset?.listKind === "choice-lines") {
    const selected = [...(el.querySelector("[data-array-suggest]")?.selectedOptions || [])].map((x) => x.value).filter(Boolean);
    const custom = String(el.querySelector("[data-array-custom]")?.value || "").split(/\r?\n/).map((x) => x.trim()).filter(Boolean);
    return [...new Set([...selected, ...custom])].join("\n");
  }
  if (el?.dataset?.listKind === "choice-custom") {
    const selectValue = String(el.querySelector("[data-choice-suggest]")?.value || "");
    if (selectValue === "__custom__") return String(el.querySelector("[data-choice-custom]")?.value || "").trim();
    return selectValue.trim();
  }
  if (el?.dataset?.listKind === "longtext-choice") {
    const selectValue = String(el.querySelector("[data-longtext-suggest]")?.value || "");
    if (selectValue === "__custom__") return String(el.querySelector("[data-longtext-custom]")?.value || "").trim();
    return selectValue.trim();
  }
  if (el?.multiple) return [...el.selectedOptions].map((x) => x.value).filter(Boolean).join("\n");
  return String(el?.value || "").trim();
}
function renderConfigFields() {
  const defs = Object.fromEntries(state.schema.map((x) => [x.key, x]));
  const groups = configGroups(state.schema);
  if (!groups.some((g) => g.id === state.settingsTab)) state.settingsTab = groups[0]?.id || "";
  $("cfgTabs").innerHTML = groups.map((group) => `<button id="cfg_tab_${esc(group.id)}" type="button" role="tab" class="cfg-tab ${group.id === state.settingsTab ? "active" : ""}" data-cfg-tab="${esc(group.id)}" aria-controls="cfg_panel_${esc(group.id)}" aria-selected="${group.id === state.settingsTab ? "true" : "false"}">${esc(group.title)}</button>`).join("");
  $("cfgForm").innerHTML = groups.map((group) => {
    const special = group.id === "tools" ? renderMCPManager() + renderWebSearchManager() : group.id === "skills" ? renderSkillManager() : group.id === "memory" ? renderMemoryManager() : group.id === "migration" ? renderMigrationManager() : group.id === "im" ? renderIMConfigEditor(defs) : "";
    const fields = group.id === "im" ? "" : group.keys.map((key) => configFieldMarkup(key, defs)).join("");
    return `<fieldset id="cfg_panel_${esc(group.id)}" class="cfg-group" data-cfg-panel="${esc(group.id)}" role="tabpanel" aria-labelledby="cfg_tab_${esc(group.id)}" aria-hidden="${group.id === state.settingsTab ? "false" : "true"}" ${group.id === state.settingsTab ? "" : "hidden"}><legend>${esc(group.title)}</legend><p class="helper">${esc(group.hint)}</p>${special}${fields}</fieldset>`;
  }).join("");
  bindSkillManager();
  bindMCPManager();
  bindWebSearchManager();
  bindMemoryManager();
  bindMigrationManager();
  bindIMAuditPanel();
  bindWeixinQRTools();
  bindChoiceCustomControls();
  bindChannelTools();
  bindSecretGenerators();
  bindAudioDeviceInputs();
  document.querySelectorAll("[data-cfg-tab]").forEach((b) => {
    b.onclick = () => setActiveConfigTab(b.dataset.cfgTab);
    b.onkeydown = (e) => { if (e.key === "ArrowRight") { e.preventDefault(); moveConfigTab(b, 1); } else if (e.key === "ArrowLeft") { e.preventDefault(); moveConfigTab(b, -1); } };
  });
}
function collectConfig() {
  const next = stripAdminManagedConfig(userConfigDraft(state.config));
  const setObjectPath = (target, field, value) => {
    if (!field.includes(".")) { target[field] = value; return; }
    const parts = field.split(".");
    let cur = target;
    parts.slice(0, -1).forEach((part) => {
      if (!cur[part] || typeof cur[part] !== "object" || Array.isArray(cur[part])) cur[part] = {};
      cur = cur[part];
    });
    cur[parts[parts.length - 1]] = value;
  };
  const deleteObjectPath = (target, field) => {
    if (!field.includes(".")) { delete target[field]; return; }
    const parts = field.split(".");
    let cur = target;
    for (const part of parts.slice(0, -1)) {
      if (!cur || typeof cur !== "object") return;
      cur = cur[part];
    }
    if (cur && typeof cur === "object") delete cur[parts[parts.length - 1]];
  };
  const assignObjectField = (target, key, field, value) => {
    const meta = (CONFIG_OBJECT_FIELDS[key]?.fields || CONFIG_OBJECT_MAP_FIELDS[key]?.fields || CONFIG_OBJECT_LIST_FIELDS[key]?.fields || CONFIG_JSON_STRING_OBJECT_FIELDS[key]?.fields || []).find((f) => f.key === field) || {};
    if (meta.kind === "kv") {
      if (value && typeof value === "object" && Object.keys(value).length) setObjectPath(target, field, value); else deleteObjectPath(target, field);
      return;
    }
    if (!value) { deleteObjectPath(target, field); return; }
    if (meta.kind === "number") {
      const parsed = Number(value);
      if (!Number.isFinite(parsed)) throw new Error(t("numberInvalid", { key: configIssueLabel({ key: `${key}.${field}` }), type: configTypeName("number") }));
      setObjectPath(target, field, parsed);
      return;
    }
    if (meta.kind === "bool") { setObjectPath(target, field, value === "true"); return; }
    if (meta.kind === "lines" || meta.kind === "multi") {
      const lines = value.split(/\r?\n/).map((x) => x.trim()).filter(Boolean);
      if (lines.length) setObjectPath(target, field, lines);
      return;
    }
    setObjectPath(target, field, value);
  };
  document.querySelectorAll("[data-json-string-object]").forEach((box) => {
    const key = box.dataset.jsonStringObject;
    let out = {};
    try { out = JSON.parse(state.config?.[key] || "{}"); } catch { out = {}; }
    box.querySelectorAll("[data-list-key]").forEach((el) => assignObjectField(out, key, el.dataset.listField || "", objectElementValue(el)));
    if (Object.keys(out).length) next[key] = JSON.stringify(out); else delete next[key];
  });
  document.querySelectorAll("[data-object-form]").forEach((box) => {
    const key = box.dataset.objectForm;
    const current = state.config?.[key] && typeof state.config[key] === "object" ? state.config[key] : {};
    const out = { ...current };
    box.querySelectorAll("[data-list-key]").forEach((el) => assignObjectField(out, key, el.dataset.listField || "", objectElementValue(el)));
    if (Object.keys(out).length) next[key] = out; else delete next[key];
  });
  document.querySelectorAll("[data-object-map]").forEach((box) => {
    const key = box.dataset.objectMap;
    const current = state.config?.[key] && typeof state.config[key] === "object" ? state.config[key] : {};
    const rows = new Map();
    box.querySelectorAll("[data-map-key]").forEach((el) => {
      const row = Number(el.dataset.mapRow || 0);
      const route = String(el.value || "").trim();
      if (route) rows.set(row, { route, value: { ...(current[route] || {}) } });
    });
    box.querySelectorAll("[data-list-key]").forEach((el) => {
      const row = Number(el.dataset.listRow || 0);
      const entry = rows.get(row);
      if (!entry) return;
      assignObjectField(entry.value, key, el.dataset.listField || "", objectElementValue(el));
    });
    const out = {};
    [...rows.keys()].sort((a, b) => a - b).forEach((row) => {
      const entry = rows.get(row);
      if (Object.keys(entry.value).length) out[entry.route] = entry.value;
    });
    if (Object.keys(out).length) next[key] = out; else delete next[key];
  });
  document.querySelectorAll("[data-object-list]").forEach((box) => {
    const key = box.dataset.objectList;
    const def = CONFIG_OBJECT_LIST_FIELDS[key];
    const existing = items(state.config?.[key]).map((v) => v && typeof v === "object" ? v : {});
    const rows = new Map();
    const visibleRows = new Set();
    box.querySelectorAll("[data-list-key]").forEach((el) => {
      const row = Number(el.dataset.listRow || 0);
      const field = el.dataset.listField || "";
      const value = objectElementValue(el);
      if (!rows.has(row)) rows.set(row, { ...(existing[row] || {}) });
      if (value) visibleRows.add(row);
      assignObjectField(rows.get(row), key, field, value);
    });
    const out = [...rows.keys()].sort((a, b) => a - b).filter((row) => visibleRows.has(row)).map((row) => rows.get(row)).filter((item) => Object.keys(item).length);
    if (out.length) next[key] = out; else delete next[key];
  });
  ADMIN_MANAGED_CONFIG_KEYS.forEach((key) => delete next[key]);
  document.querySelectorAll("[data-key]").forEach((el) => {
    const key = el.dataset.key; const type = el.dataset.type || "string";
    if (type === "array" || type === "object") {
      try { next[key] = JSON.parse(el.value || (type === "array" ? "[]" : "{}")); } catch { throw new Error(t("jsonInvalid", { key: configIssueLabel({ key }) })); }
    } else if (type === "object-kv") {
      const out = {};
      const current = state.config?.[key] && typeof state.config[key] === "object" && !Array.isArray(state.config[key]) ? state.config[key] : {};
      el.querySelectorAll(".kv-pair").forEach((pair) => {
        const keySelect = String(pair.querySelector("[data-generic-object-key]")?.value || "");
        const valueSelect = String(pair.querySelector("[data-generic-object-value]")?.value || "");
        const pairKey = String(keySelect === "__custom__" ? pair.querySelector("[data-generic-object-key-custom]")?.value || "" : keySelect).trim();
        const pairValue = String(valueSelect === "__custom__" ? pair.querySelector("[data-generic-object-value-custom]")?.value || "" : valueSelect).trim();
        if (pairKey && pairValue) {
          const typedValue = coercePlainObjectValue(plainObjectPathValue(current, pairKey), pairValue);
          if (el.dataset.deepObject === "true") setPlainObjectPath(out, pairKey, typedValue); else out[pairKey] = typedValue;
        }
      });
      if (Object.keys(out).length) next[key] = out; else delete next[key];
    } else if (type === "array-choice") {
      const selected = [...el.selectedOptions].map((x) => x.value).filter(Boolean);
      if (selected.length) next[key] = selected; else delete next[key];
    } else if (type === "array-lines") {
      const lines = el.value.split(/\r?\n/).map((x) => x.trim()).filter(Boolean);
      if (lines.length) next[key] = lines; else delete next[key];
    } else if (type === "array-choice-lines") {
      const lines = objectElementValue(el).split(/\r?\n/).map((x) => x.trim()).filter(Boolean);
      if (lines.length) next[key] = lines; else delete next[key];
    } else if (type === "string-lines") {
      next[key] = el.value.split(/[;\r\n]+/).map((x) => x.trim()).filter(Boolean).join(";");
    } else if (type === "string-choice-lines") {
      next[key] = objectElementValue(el).split(/[;\r\n]+/).map((x) => x.trim()).filter(Boolean).join(";");
    } else if (type === "string-choice-custom") {
      const value = objectElementValue(el);
      if (el.dataset.unsetEmpty === "true" && value === "") delete next[key]; else next[key] = value;
    } else if (type === "bool") {
      const value = objectElementValue(el);
      if (value === "") delete next[key]; else next[key] = value === "true";
    } else if (type === "integer") {
      const parsed = parseConfigNumber(key, objectElementValue(el), true); if (parsed === undefined) delete next[key]; else next[key] = parsed;
    } else if (type === "number") {
      const parsed = parseConfigNumber(key, objectElementValue(el), false); if (parsed === undefined) delete next[key]; else next[key] = parsed;
    } else if (el.dataset.unsetEmpty === "true" && el.value === "") { delete next[key];
    } else { next[key] = el.value; }
  });
  return next;
}
function refreshSettingsAfterSave() {
  const savedConfig = state.config;
  Promise.all([
    api("/api/v1/config/validate", { method: "POST", body: JSON.stringify({ app_config: savedConfig }) }),
    refreshInstances(),
    loadWeixinRuntimeStatus(),
    loadIMRuntimeStatuses()
  ]).then(([validation]) => {
    if (state.view !== "settings" || state.config !== savedConfig) return;
    updateConfigStatus(validation);
    renderIssues(validation);
    renderConfigFields();
  }).catch((e) => {
    if (e?.status === 401) handleAPIError(e);
  });
}
async function saveConfig(options = {}) {
  try {
    setBusy(true);
    setSettingsActionsDisabled(true);
    const next = collectConfig();
    const out = await api("/api/v1/config", { method: "PUT", body: JSON.stringify({ app_config: next }) });
    state.config = out.app_config || next;
    updateConfigStatus({ valid: true });
    renderIssues({ valid: true, issues: [] });
    renderConfigFields();
    setConfigOutput(out);
    toast(options.toastMessage || t("saved"));
    refreshSettingsAfterSave();
    return true;
  } catch (e) {
    if (!handleAPIError(e)) toast(e.message);
    return false;
  } finally {
    setSettingsActionsDisabled(false);
    setBusy(false);
  }
}
async function validateConfig() { try { setBusy(true); setSettingsActionsDisabled(true); const out = await api("/api/v1/config/validate", { method: "POST", body: JSON.stringify({ app_config: collectConfig() }) }); updateConfigStatus(out); renderIssues(out); setConfigOutput(out); toast(t("validated")); } catch (e) { if (!handleAPIError(e)) toast(e.message); } finally { setSettingsActionsDisabled(false); setBusy(false); } }
async function testConfig() { try { setBusy(true); setSettingsActionsDisabled(true); const out = await api("/api/v1/config/test", { method: "POST", body: JSON.stringify({ app_config: collectConfig() }) }); setConfigOutput(out); toast(out.success ? t("testPassed") : t("testFailed")); } catch (e) { if (!handleAPIError(e)) toast(e.message); } finally { setSettingsActionsDisabled(false); setBusy(false); } }

document.querySelectorAll("[data-view]").forEach((b) => b.onclick = () => { if (state.view !== b.dataset.view) { resetRunState(); if (state.view === "settings") resetWeixinQRLogin(); } state.view = b.dataset.view; history.replaceState(null, "", `/app/?view=${state.view}${state.instanceId ? `&instance_id=${encodeURIComponent(state.instanceId)}` : ""}`); render(); });
$("logoutBtn").onclick = () => { clearAccessToken(); renderMissingToken(); };
["pointerdown", "keydown", "input", "focus"].forEach((eventName) => document.addEventListener(eventName, markUserActivity, true));
document.addEventListener("visibilitychange", () => { if (document.visibilityState === "visible") markUserActivity(); });
window.addEventListener("beforeunload", clearWeixinQRPoll);
markUserActivity();
bootstrap();
