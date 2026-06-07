const zh = {
  "adminConsole": "管理控制台",
  "logout": "退出",
  "overview": "总览",
  "sandbox": "沙箱",
  "logs": "日志",
  "config": "服务配置",
  "tenants": "租户与用户",
  "tenantList": "租户",
  "accounts": "管理员",
  "knowledge": "知识与技能来源",
  "ops": "运维",
  "setup": "初始化管理员",
  "login": "管理员登录",
  "refresh": "刷新",
  "save": "保存",
  "saved": "保存成功",
  "savedDraft": "草稿已保存，当前生效需应用 .env/systemd 并重启服务",
  "run": "运行",
  "validate": "校验",
  "exportPlan": "导出方案",
  "detect": "检测",
  "smoke": "冒烟检测",
  "diagnose": "诊断并保存报告",
  "installPlan": "安装方案",
  "changePassword": "修改密码",
  "sessions": "会话",
  "users": "用户",
  "status": "状态",
  "action": "操作",
  "confirm": "确认",
  "raw": "原始数据",
  "password": "密码",
  "username": "用户名",
  "setupToken": "初始化令牌",
  "displayName": "显示名",
  "language": "语言",
  "adminSecret": "Admin Secret 或会话 Token",
  "accountLogin": "账号登录",
  "secretLogin": "Admin Secret",
  "secretLoginHint": "Admin Secret 来自服务启动环境变量 MACLAW_ADMIN_SECRET；通常用于自动化或紧急管理，不是管理员密码。",
  "showSecret": "显示",
  "hideSecret": "隐藏",
  "requiredField": "请先填写必填项。",
  "loginHint": "登录后所有操作都通过 Admin API 完成。",
  "setupHint": "首次启动需要创建 owner 管理员；如服务设置了 MACLAW_ADMIN_SETUP_TOKEN，请输入初始化令牌。",
  "authLoginKicker": "安全管理员入口",
  "authAccessKicker": "管理员访问",
  "authSetupKicker": "首次初始化",
  "authSetupAccessKicker": "管理员初始化",
  "setupTokenEnvHint": "需要填写服务器环境变量 MACLAW_ADMIN_SETUP_TOKEN。",
  "noSetupTokenRequired": "不需要初始化令牌",
  "minPassword": "至少 {min} 位",
  "setupTokenRequiredMessage": "请输入初始化令牌。",
  "usernameRequiredMessage": "请输入用户名。",
  "passwordMinMessage": "密码至少需要 {min} 位。",
  "initializedMessage": "初始化成功，请登录。",
  "noToken": "未登录",
  "loaded": "已加载",
  "failed": "请求失败",
  "dangerous": "危险操作",
  "suspend": "暂停",
  "activate": "启用",
  "revoke": "吊销",
  "effective": "当前生效",
  "environment": "环境变量",
  "draft": "草稿",
  "report": "报告",
  "sandboxNonePrompt": "输入 DISABLE SANDBOX 确认关闭沙箱",
  "useSecret": "使用 Secret",
  "cancelJobConfirm": "确认取消这个任务？",
  "revokeCredentialConfirm": "确认吊销这个凭据？",
  "deleteBlocked": "删除被阻止",
  "deleteTenantConfirm": "确认删除租户并清理其数据？",
  "deleteUserConfirm": "确认删除用户并清理其数据？",
  "forceDeleteTenantPrompt": "此租户有删除保护。强制删除会同时删除租户下全部 {users} 个用户及其数据。请输入管理员密码或 Admin Secret：",
  "forceDeleteTenantConfirm": "确认强制删除此受保护租户、全部用户及其数据？此操作不可恢复。",
  "forceDeleteUserPrompt": "此用户有删除保护。如需强制删除，请输入管理员密码或 Admin Secret：",
  "forceDeleteUserConfirm": "确认强制删除此受保护用户并清理其数据？此操作不可恢复。",
  "revokeSessionConfirm": "确认吊销这个会话？",
  "clearKnowledgeConfirm": "清理此租户的所有知识数据？",
  "tenantIDRequired": "需要 tenant_id",
  "deleteKnowledgeAccessConfirm": "确认删除此知识访问覆盖？",
  "deleteSkillTenantConfirm": "确认删除租户技能源覆盖？",
  "deleteSkillUserConfirm": "确认删除用户技能源覆盖？",
  "deleteSnapshotConfirm": "确认删除此快照？",
  "installRun": "执行安装",
  "installRunPrompt": "输入 INSTALL SANDBOX 确认执行安装命令",
  "deleteSandboxReportConfirm": "确认删除此沙箱报告？",
  "rollback": "回滚",
  "sandboxRollbackConfirm": "确认清除运行时沙箱覆盖并回到环境配置？",
  "deleteSandboxProfileConfirm": "确认删除此沙箱 profile？",
  "profileName": "Profile 名称",
  "editProfile": "编辑 Profile",
  "profileJSON": "Profile JSON",
  "eventStatus": "事件状态",
  "eventBackend": "事件 Backend",
  "supportBundle": "支持包",
  "serviceSupportBundle": "服务支持包",
  "importRunPrompt": "输入 IMPORT STATE 确认执行导入",
  "restoreRunPrompt": "输入 RESTORE SNAPSHOT 确认恢复快照",
  "exportSecretPrompt": "输入 EXPORT SECRETS 确认导出敏感密钥",
  "snapshotSecretPrompt": "输入 SNAPSHOT SECRETS 确认创建含密钥快照",
  "securityRisks": "安全风险",
  "riskEvents": "风险事件",
  "bySeverity": "按严重程度",
  "byKind": "按类型",
  "all": "全部",
  "clear": "清除",
  "total": "总数",
  "allRisks": "全部风险",
  "generatedAt": "生成时间",
  "invalidRiskLimit": "风险条数必须在 1 到 500 之间",
  "empty": "空",
  "view": "查看",
  "delete": "删除",
  "cancel": "取消",
  "noReports": "暂无报告",
  "redactions": "脱敏项",
  "dataRoot": "数据根目录",
  "overviewHint": "运行状态、就绪状态、调度器和异步任务",
  "sandboxHint": "检测、切换、诊断和安装指引",
  "logsHint": "已脱敏的服务日志尾部，支持来源和文本过滤",
  "runtimeLabel": "运行时",
  "runGC": "运行 GC",
  "goroutines": "Goroutine",
  "downloadGoroutines": "下载 Goroutine",
  "heapProfile": "Heap Profile",
  "downloadHeapProfile": "下载 Heap",
  "readiness": "就绪",
  "scheduler": "调度器",
  "asyncJobs": "异步任务",
  "jobs": "任务",
  "security": "安全",
  "mode": "模式",
  "effectiveBackend": "生效后端",
  "strict": "严格模式",
  "backends": "后端",
  "capabilities": "能力",
  "profiles": "Profiles",
  "events": "事件",
  "sources": "来源",
  "open": "打开",
  "search": "搜索",
  "level": "级别",
  "text": "文本",
  "lines": "行",
  "truncated": "已截断",
  "reason": "原因",
  "profile": "Profile",
  "audit": "审计",
  "export": "导出",
  "import": "导入",
  "snapshots": "快照",
  "create": "创建",
  "createTenant": "创建租户",
  "createUser": "创建用户",
  "createAdmin": "创建管理员",
  "credentials": "凭据",
  "apiCredentials": "API 凭据",
  "credentialHelp": "用于外部程序以这个用户身份调用 MaClawSrv API。普通后台管理不需要创建；如果怀疑泄露，可以暂停、轮换或吊销。",
  "credentialResult": "最近操作结果",
  "knowledgeStats": "知识统计",
  "knowledgeSources": "知识来源",
  "knowledgeAccess": "知识访问",
  "skillSources": "技能来源",
  "global": "全局",
  "tenantOverride": "租户覆盖",
  "userOverride": "用户覆盖",
  "oldPassword": "旧密码",
  "newPassword": "新密码",
  "deleteProtected": "删除保护",
  "protectionReason": "保护原因",
  "allowCrossTenant": "允许跨租户范围",
  "clearTenantKnowledge": "清理租户知识",
  "restartRequired": "需要重启",
  "runtimeOnly": "仅运行时",
  "warnings": "警告",
  "errors": "错误",
  "notes": "备注",
  "recommendations": "建议",
  "dryRun": "预演",
  "restore": "恢复",
  "overwrite": "覆盖",
  "secrets": "密钥",
  "messages": "消息",
  "runs": "运行",
  "name": "名称",
  "email": "邮箱",
  "configHint": "Schema 驱动的草稿、校验和导出方案",
  "tenantsHint": "创建、暂停、凭据、删除检查和清理",
  "accountsHint": "管理员操作员和会话",
  "knowledgeHint": "知识访问、租户清理和技能源策略",
  "opsHint": "审计、导出/导入、风险事件和服务快照",
  "serviceConfig": "服务配置",
  "manualApply": "手动应用",
  "willExecute": "将执行",
  "howToApplyConfig": "如何修改配置",
  "configApplyHint": "勾选草稿页里的配置项后修改值，点击保存写入草稿；再校验并导出方案。需要重启的配置要按导出的 .env/systemd 内容应用并重启服务。",
  "configEffectiveHint": "这里显示当前进程正在使用的值。保存草稿不会立即改变本页；导出方案、应用到服务环境并重启后才会更新。",
  "configDiffHint": "差异显示已保存草稿和当前生效值的对比。保存后会自动刷新本页。",
  "revealAdminSecret": "查看 Root Admin API Secret",
  "revealAdminSecretHint": "仅 owner 管理员账号可用。需重新输入当前管理员密码，操作会写入审计日志。",
  "adminPassword": "管理员密码",
  "copy": "复制",
  "copied": "已复制",
  "cfgTabDraft": "草稿",
  "cfgTabEffective": "当前生效",
  "cfgTabEnvironment": "环境变量",
  "cfgTabDiff": "差异",
  "cfgTabSecrets": "密钥",
  "cfgTabRaw": "原始数据",
  "valid": "有效",
  "invalid": "无效",
  "configuredState": "已配置",
  "changed": "已变更",
  "current": "当前值",
  "desired": "目标值",
  "defaultValue": "默认值",
  "source": "来源",
  "envKey": "环境变量",
  "createCredential": "创建凭据",
  "listCredentials": "列出凭据",
  "crossTenant": "跨租户",
  "skillSourcePolicy": "技能源策略",
  "inherit": "继承",
  "enabledState": "启用",
  "disabledState": "停用",
  "activeState": "活跃",
  "inactiveState": "未活跃",
  "activeStatus": "启用",
  "suspendedStatus": "已暂停",
  "yes": "是",
  "no": "否",
  "role": "角色",
  "lastLoginAt": "最近登录",
  "createdAt": "创建时间",
  "expiresAt": "过期时间",
  "remoteIP": "远端 IP",
  "genericID": "ID",
  "configKey": "配置项",
  "sourceCount": "来源数",
  "distilledSources": "已提炼来源",
  "latestSourceAt": "最近来源时间",
  "kind": "类型",
  "nextRunAt": "下次运行",
  "lastError": "最近错误",
  "reportID": "报告 ID",
  "effectiveBackend": "生效后端",
  "resourceType": "资源类型",
  "resourceID": "资源 ID",
  "backend": "后端",
  "network": "网络",
  "available": "可用",
  "smokeStatus": "冒烟状态",
  "version": "版本",
  "detail": "详情",
  "severity": "严重程度",
  "expected": "预期",
  "actual": "实际",
  "exists": "存在",
  "sizeBytes": "大小",
  "modifiedAt": "修改时间",
  "deleteProtectedColumn": "删除保护",
  "apiKeyHint": "API Key 提示",
  "scope": "范围",
  "jobPending": "等待中",
  "jobRunning": "运行中",
  "jobSucceeded": "成功",
  "jobFailed": "失败",
  "jobCanceled": "已取消",
  "statusOK": "正常",
  "statusReady": "就绪",
  "statusPass": "通过",
  "statusWarn": "警告",
  "statusFail": "失败",
  "severityHigh": "高",
  "severityMedium": "中",
  "severityLow": "低",
  "riskTimeRangeInvalid": "开始时间必须早于或等于结束时间",
  "modeAuto": "自动",
  "modeNone": "关闭",
  "defaultOption": "默认",
  "strictEnabled": "强制启用",
  "strictDisabled": "强制关闭",
  "networkHost": "宿主网络",
  "logError": "错误",
  "logInfo": "信息",
  "requiresPrivilege": "需要权限",
  "redactedState": "已脱敏",
  "unknown": "未知",
  "noReport": "暂无报告",
  "highMedium": "高 {high} / 中 {medium}",
  "allOption": "全部",
  "selectTenant": "请选择租户",
  "selectUser": "请选择用户",
  "addScope": "添加范围",
  "scopeName": "范围名称",
  "kindPlaceholder": "类型",
  "tenantID": "tenant_id",
  "tenantName": "租户名称",
  "userID": "user_id",
  "knowledgeOwner": "知识归属用户",
  "knowledgeSourceDisplayHint": "优先显示租户名称和用户名；括号中保留原始 ID，方便核对。",
  "knowledgeAccessTargetHint": "先选择要配置知识权限的用户；保存后只影响这个用户的可读知识范围。",
  "knowledgeScopeBuilderHint": "添加可读知识来源时优先选择租户名称和用户名；预览会保留原始 ID 方便核对。",
  "actionName": "操作",
  "apiKeyOptional": "API key（可选）",
  "apiSecretOptional": "API secret（可选）",
  "userEmail": "用户邮箱",
  "pasteExportedJSON": "粘贴导出的 JSON",
  "invalidJSON": "JSON 格式无效",
  "invalidLimit": "条数必须在 {min} 到 {max} 之间",
  "resolve": "解析",
  "restart": "重启",
  "runtime": "运行时",
  "sensitive": "敏感",
  "owner": "所有者",
  "operator": "操作员",
  "ownerOnly": "仅所有者可操作",
  "summary": "摘要",
  "deleteCheck": "删除检查",
  "retirePlan": "退役方案",
  "profileJSONInvalid": "Profile JSON 格式无效",
  "recentErrors": "最近错误",
  "includeWarnings": "包含警告",
  "searchAllLogs": "搜索全部日志",
  "download": "下载",
  "rotateLog": "\u8f6c\u50a8\u65e5\u5fd7",
  "rotateLogConfirm": "\u786e\u8ba4\u8f6c\u50a8\u5f53\u524d\u65e5\u5fd7\u5e76\u521b\u5efa\u65b0\u6587\u4ef6\uff1f",
  "rotateSecret": "轮换密钥",
  "rotateKey": "轮换公钥",
  "publicKnowledgeBases": "公用知识库",
  "publicKnowledgeHint": "管理共享知识库，可导入文本、文档/压缩包和抓取 URL。",
  "publicKnowledgeName": "公用知识库名称",
  "createPublicKnowledgeBase": "创建公用知识库",
  "addPublicKnowledgeScope": "加入公用知识库",
  "configuredKnowledgeScopes": "已配置知识范围",
  "effectiveKnowledgeAccess": "实际生效知识范围",
  "ownKnowledge": "自有知识",
  "sharedKnowledge": "他人知识",
  "selectedUserForPublicKnowledge": "先选择要配置的用户，再挂载或移除公用知识库。",
  "attachPublicKnowledge": "挂载",
  "detachPublicKnowledge": "移除",
  "importText": "导入文本",
  "importDocumentArchive": "导入文档/压缩包",
  "importURL": "导入 URL",
  "importing": "导入中...",
  "importStarted": "知识库导入已开始",
  "importedKnowledge": "知识库导入已完成",
  "importCompleted": "知识库导入已完成",
  "importStillRunning": "知识库导入仍在运行",
  "importJob": "导入任务",
  "importStatus": "状态",
  "importSource": "来源",
  "importTitle": "标题",
  "importKind": "类型",
  "importFiles": "文件",
  "importUrls": "URL",
  "importProcessed": "已处理",
  "importImported": "已导入",
  "importFailed": "失败",
  "importSkipped": "跳过",
  "importDuplicates": "重复",
  "importWarnings": "警告",
  "sameDomainOnly": "仅同域名",
  "crawlDepth": "枚举深度",
  "labels": "标签",
  "urlPlaceholder": "https://example.com/docs",
  "deletePublicKnowledgeConfirm": "删除此公用知识库及其中知识来源？",
  "importTextRequired": "请输入要导入的文本。",
  "importURLRequired": "请输入要导入的 URL。",
  "skillSourcesHintDetailed": "技能来源是助手搜索和安装技能的渠道，不是知识库。生效优先级：用户单独策略 > 租户单独策略 > 全局默认策略。未启用的层级会继承上一级。",
  "skillPolicyPriorityTitle": "生效顺序",
  "globalPolicyShort": "全局默认",
  "tenantOverrideHintDetailed": "租户覆盖：只影响该租户内所有用户；启用后覆盖全局默认。",
  "userOverrideHintDetailed": "用户覆盖：只影响选中的单个用户；启用后优先级最高。",
  "globalSkillPolicyTitle": "全局默认策略",
  "tenantSkillPolicyTitle": "租户单独策略",
  "userSkillPolicyTitle": "用户单独策略",
  "globalSkillPolicyHint": "所有租户和用户的默认技能来源；没有租户/用户单独策略时按这里执行。",
  "tenantSkillPolicyHint": "给选中的租户设置技能来源；启用后，该租户内用户默认按这里执行。",
  "userSkillPolicyHint": "给选中的用户设置技能来源；启用后优先级最高，只影响这个用户。",
  "skillPolicyEnabledHint": "启用此层策略；不启用则继承上一级策略。",
  "skillhubDesc": "官方 SkillHub / SkillMarket，优先使用的受控技能市场。",
  "localDesc": "本地 ZIP / 导入上传，不访问远程技能市场。",
  "clawhubDesc": "社区 ClawHub 镜像，用于发现社区共享技能。",
  "githubDesc": "GitHub 开源仓库，范围更广但需注意来源可信度。",
  "enterpriseHubDesc": "企业内部能力市场，适合只允许企业已审批技能。"
};
const en = {
  "adminConsole": "Admin Console",
  "logout": "Logout",
  "overview": "Overview",
  "sandbox": "Sandbox",
  "logs": "Logs",
  "config": "Service Config",
  "clientConfig": "Client Config",
  "clientConfigHint": "Shared user-side search, proxy, network security, MCP, SSH, skill source, and interface config",
  "clientConfigDefaults": "Shared client config",
  "clientConfigDefaultsHint": "Saved values are shared by all MaClawSrv users and take effect through the effective runtime config.",
  "webSearch": "Web Search",
  "webSearchProviders": "Search providers JSON",
  "currentProvider": "Current provider",
  "proxyConfig": "Proxy Service",
  "sharedTools": "Shared Tool Resources",
  "securityDefaults": "Security & Network Defaults",
  "experienceDefaults": "User Interface",
  "advancedJSON": "Advanced JSON",
  "advancedJSONHint": "Configure shared arrays such as MCP Servers, Local MCP, SSH Hosts, SkillHub URLs, and external skill directories.",
  "applyToNewUsers": "Apply to all users",
  "validateOnly": "Validate only",
  "defaultClientConfigSaved": "Shared client config saved",
  "providerType": "Provider type",
  "providerKey": "API Key",
  "providerBaseURL": "Base URL",
  "networkLevel": "Network level",
  "allowlist": "Allowlist",
  "tenants": "Tenants & Users",
  "tenantList": "Tenants",
  "accounts": "Admins",
  "knowledge": "Knowledge & Skill Sources",
  "ops": "Ops",
  "setup": "Initialize Admin",
  "login": "Admin Login",
  "refresh": "Refresh",
  "save": "Save",
  "saved": "Saved",
  "savedDraft": "Draft saved. Effective values update after applying .env/systemd and restarting the service.",
  "run": "Run",
  "validate": "Validate",
  "exportPlan": "Export Plan",
  "detect": "Detect",
  "smoke": "Smoke Test",
  "diagnose": "Diagnose and Save",
  "installPlan": "Install Plan",
  "changePassword": "Change Password",
  "sessions": "Sessions",
  "users": "Users",
  "status": "Status",
  "action": "Action",
  "confirm": "Confirm",
  "raw": "Raw",
  "password": "Password",
  "username": "Username",
  "setupToken": "Setup Token",
  "displayName": "Display Name",
  "language": "Language",
  "adminSecret": "Admin Secret or Session Token",
  "accountLogin": "Account Login",
  "secretLogin": "Admin Secret",
  "secretLoginHint": "Admin Secret comes from MACLAW_ADMIN_SECRET at service startup. It is for automation or emergency admin access, not the admin password.",
  "showSecret": "Show",
  "hideSecret": "Hide",
  "requiredField": "Fill required fields first.",
  "loginHint": "After login, every operation goes through Admin API.",
  "setupHint": "First boot creates an owner admin. Enter the setup token when MACLAW_ADMIN_SETUP_TOKEN is configured.",
  "authLoginKicker": "Secure admin operations",
  "authAccessKicker": "Admin access",
  "authSetupKicker": "First run setup",
  "authSetupAccessKicker": "Admin setup",
  "setupTokenEnvHint": "Enter the server environment variable MACLAW_ADMIN_SETUP_TOKEN.",
  "noSetupTokenRequired": "No setup token required",
  "minPassword": "At least {min} characters",
  "setupTokenRequiredMessage": "Enter the setup token.",
  "usernameRequiredMessage": "Enter the username.",
  "passwordMinMessage": "Password must be at least {min} characters.",
  "initializedMessage": "Initialized. Please log in.",
  "noToken": "Not signed in",
  "loaded": "Loaded",
  "failed": "Request failed",
  "dangerous": "Dangerous",
  "suspend": "Suspend",
  "activate": "Activate",
  "revoke": "Revoke",
  "effective": "Effective",
  "environment": "Environment",
  "draftDiff": "Draft diff",
  "clearDraft": "Clear draft",
  "clearDraftConfirm": "Clear the service config draft?",
  "draft": "Draft",
  "report": "Report",
  "sandboxNonePrompt": "Type DISABLE SANDBOX to confirm disabling sandbox",
  "useSecret": "Use Secret",
  "cancelJobConfirm": "Cancel this job?",
  "revokeCredentialConfirm": "Revoke this credential?",
  "deleteBlocked": "Delete blocked",
  "deleteTenantConfirm": "Delete tenant and cleanup data?",
  "deleteUserConfirm": "Delete user and cleanup data?",
  "forceDeleteTenantPrompt": "This tenant is delete-protected. Force delete also deletes all {users} users and their data. Enter admin password or Admin Secret:",
  "forceDeleteTenantConfirm": "Force delete this protected tenant, all users, and all data? This cannot be undone.",
  "forceDeleteUserPrompt": "This user is delete-protected. Enter admin password or Admin Secret to force delete:",
  "forceDeleteUserConfirm": "Force delete this protected user and cleanup data? This cannot be undone.",
  "revokeSessionConfirm": "Revoke this session?",
  "clearKnowledgeConfirm": "Clear all knowledge for this tenant?",
  "tenantIDRequired": "tenant_id required",
  "deleteKnowledgeAccessConfirm": "Delete this knowledge access override?",
  "deleteSkillTenantConfirm": "Delete tenant skill source override?",
  "deleteSkillUserConfirm": "Delete user skill source override?",
  "deleteSnapshotConfirm": "Delete this snapshot?",
  "installRun": "Run Install",
  "installRunPrompt": "Type INSTALL SANDBOX to confirm running install commands",
  "deleteSandboxReportConfirm": "Delete this sandbox report?",
  "rollback": "Rollback",
  "sandboxRollbackConfirm": "Clear runtime sandbox overrides and return to environment config?",
  "deleteSandboxProfileConfirm": "Delete this sandbox profile?",
  "profileName": "Profile Name",
  "editProfile": "Edit Profile",
  "profileJSON": "Profile JSON",
  "eventStatus": "Event Status",
  "eventBackend": "Event Backend",
  "supportBundle": "Support Bundle",
  "serviceSupportBundle": "Service Bundle",
  "importRunPrompt": "Type IMPORT STATE to confirm importing state",
  "restoreRunPrompt": "Type RESTORE SNAPSHOT to confirm restoring snapshot",
  "exportSecretPrompt": "Type EXPORT SECRETS to confirm exporting sensitive secrets",
  "snapshotSecretPrompt": "Type SNAPSHOT SECRETS to confirm creating a snapshot with secrets",
  "securityRisks": "Security Risks",
  "riskEvents": "Risk Events",
  "bySeverity": "By severity",
  "byKind": "By kind",
  "all": "all",
  "clear": "Clear",
  "total": "total",
  "allRisks": "all risks",
  "generatedAt": "Generated",
  "invalidRiskLimit": "Risk limit must be between 1 and 500",
  "empty": "Empty",
  "view": "View",
  "delete": "Delete",
  "cancel": "Cancel",
  "noReports": "No reports",
  "redactions": "Redactions",
  "dataRoot": "Data Root",
  "overviewHint": "Runtime, readiness, scheduler and async jobs",
  "sandboxHint": "Detect, switch, diagnose and install guidance",
  "logsHint": "Redacted service log tail with source and text filters",
  "runtimeLabel": "Runtime",
  "runGC": "Run GC",
  "goroutines": "Goroutines",
  "downloadGoroutines": "Download goroutines",
  "heapProfile": "Heap profile",
  "downloadHeapProfile": "Download heap",
  "readiness": "Readiness",
  "scheduler": "Scheduler",
  "asyncJobs": "Async Jobs",
  "jobs": "Jobs",
  "security": "Security",
  "mode": "Mode",
  "effectiveBackend": "Effective",
  "strict": "Strict",
  "backends": "Backends",
  "capabilities": "Capabilities",
  "profiles": "Profiles",
  "events": "Events",
  "sources": "Sources",
  "open": "Open",
  "search": "Search",
  "level": "level",
  "text": "text",
  "lines": "lines",
  "truncated": "truncated",
  "reason": "reason",
  "profile": "Profile",
  "audit": "Audit",
  "export": "Export",
  "import": "Import",
  "snapshots": "Snapshots",
  "create": "Create",
  "createTenant": "Create Tenant",
  "createUser": "Create User",
  "createAdmin": "Create Admin",
  "credentials": "Credentials",
  "apiCredentials": "API credentials",
  "credentialHelp": "Used by external programs to call the MaClawSrv API as this user. Normal admin work does not need one. If exposed, suspend, rotate, or revoke it.",
  "credentialResult": "Latest operation result",
  "knowledgeStats": "Knowledge Stats",
  "knowledgeSources": "Knowledge Sources",
  "knowledgeAccess": "Knowledge Access",
  "skillSources": "Skill Sources",
  "global": "Global",
  "tenantOverride": "Tenant Override",
  "userOverride": "User Override",
  "oldPassword": "Old password",
  "newPassword": "New password",
  "deleteProtected": "delete protected",
  "protectionReason": "Protection reason",
  "allowCrossTenant": "allow cross-tenant scopes",
  "clearTenantKnowledge": "Clear tenant knowledge",
  "restartRequired": "Restart required",
  "runtimeOnly": "Runtime only",
  "warnings": "Warnings",
  "errors": "Errors",
  "notes": "Notes",
  "recommendations": "Recommendations",
  "dryRun": "dry run",
  "restore": "Restore",
  "overwrite": "overwrite",
  "secrets": "secrets",
  "messages": "messages",
  "runs": "runs",
  "name": "Name",
  "email": "Email",
  "configHint": "Schema-driven draft, validation and export plan",
  "tenantsHint": "Create, pause, credentials, delete-check and cleanup",
  "accountsHint": "Admin operators and sessions",
  "knowledgeHint": "Knowledge access, tenant cleanup and skill source policy",
  "opsHint": "Audit, export/import, risk events and service snapshots",
  "serviceConfig": "Service Config",
  "manualApply": "manual apply",
  "willExecute": "will execute",
  "howToApplyConfig": "How to change config",
  "configApplyHint": "Select fields in the Draft tab, edit values, then save a draft. Validate and export a plan. Restart-required values must be applied with the exported .env/systemd content and service restart.",
  "configEffectiveHint": "This tab shows values currently used by the running process. Saving a draft does not change this page immediately. Export a plan, apply it to the service environment, then restart to update it.",
  "configDiffHint": "Diff compares the saved draft with current effective values. It refreshes automatically after Save.",
  "revealAdminSecret": "Reveal Root Admin API Secret",
  "revealAdminSecretHint": "Owner account sessions only. Re-enter your current admin password. The action is written to the audit log.",
  "adminPassword": "Admin password",
  "copy": "Copy",
  "copied": "Copied",
  "cfgTabDraft": "Draft",
  "cfgTabEffective": "Effective",
  "cfgTabEnvironment": "Environment",
  "cfgTabDiff": "Diff",
  "cfgTabSecrets": "Secrets",
  "cfgTabRaw": "Raw",
  "valid": "Valid",
  "invalid": "Invalid",
  "configuredState": "Configured",
  "changed": "Changed",
  "current": "Current",
  "desired": "Desired",
  "defaultValue": "Default",
  "source": "Source",
  "envKey": "Env Key",
  "createCredential": "Create credential",
  "listCredentials": "List credentials",
  "crossTenant": "Cross Tenant",
  "skillSourcePolicy": "Skill Source Policy",
  "inherit": "Inherit",
  "enabledState": "Enabled",
  "disabledState": "Disabled",
  "activeState": "Active",
  "inactiveState": "Inactive",
  "activeStatus": "Active",
  "suspendedStatus": "Suspended",
  "yes": "Yes",
  "no": "No",
  "role": "Role",
  "lastLoginAt": "Last login",
  "createdAt": "Created at",
  "expiresAt": "Expires at",
  "remoteIP": "Remote IP",
  "genericID": "ID",
  "configKey": "Config key",
  "sourceCount": "Source count",
  "distilledSources": "Distilled sources",
  "latestSourceAt": "Latest source",
  "kind": "Kind",
  "nextRunAt": "Next run",
  "lastError": "Last error",
  "reportID": "Report ID",
  "effectiveBackend": "Effective backend",
  "resourceType": "Resource type",
  "resourceID": "Resource ID",
  "backend": "Backend",
  "network": "Network",
  "available": "Available",
  "smokeStatus": "Smoke status",
  "version": "Version",
  "detail": "Detail",
  "severity": "Severity",
  "expected": "Expected",
  "actual": "Actual",
  "exists": "Exists",
  "sizeBytes": "Size",
  "modifiedAt": "Modified at",
  "deleteProtectedColumn": "Delete protected",
  "apiKeyHint": "API key hint",
  "scope": "Scope",
  "jobPending": "Pending",
  "jobRunning": "Running",
  "jobSucceeded": "Succeeded",
  "jobFailed": "Failed",
  "jobCanceled": "Canceled",
  "statusOK": "OK",
  "statusReady": "Ready",
  "statusPass": "Pass",
  "statusWarn": "Warn",
  "statusFail": "Fail",
  "severityHigh": "High",
  "severityMedium": "Medium",
  "severityLow": "Low",
  "riskTimeRangeInvalid": "Since must be before or equal to until",
  "modeAuto": "Auto",
  "modeNone": "Off",
  "defaultOption": "Default",
  "strictEnabled": "Force on",
  "strictDisabled": "Force off",
  "networkHost": "Host network",
  "logError": "Error",
  "logInfo": "Info",
  "requiresPrivilege": "Requires privilege",
  "redactedState": "Redacted",
  "unknown": "Unknown",
  "noReport": "No report",
  "highMedium": "High {high} / medium {medium}",
  "allOption": "All",
  "selectTenant": "Select tenant",
  "selectUser": "Select user",
  "addScope": "Add scope",
  "scopeName": "Scope name",
  "kindPlaceholder": "Kind",
  "tenantID": "tenant_id",
  "tenantName": "Tenant name",
  "userID": "user_id",
  "knowledgeOwner": "Knowledge owner",
  "knowledgeSourceDisplayHint": "Shows tenant/user names first, with raw IDs kept in parentheses for verification.",
  "knowledgeAccessTargetHint": "Select the user whose knowledge permissions you are configuring. Saving affects only this user's readable knowledge scopes.",
  "knowledgeScopeBuilderHint": "Choose tenant and user names when adding readable knowledge sources. Preview keeps raw IDs for verification.",
  "actionName": "Action",
  "apiKeyOptional": "API key (optional)",
  "apiSecretOptional": "API secret (optional)",
  "userEmail": "User email",
  "pasteExportedJSON": "Paste exported JSON",
  "invalidJSON": "Invalid JSON",
  "invalidLimit": "Limit must be between {min} and {max}",
  "resolve": "Resolve",
  "restart": "Restart",
  "runtime": "Runtime",
  "sensitive": "Sensitive",
  "owner": "owner",
  "operator": "operator",
  "ownerOnly": "Owner only",
  "summary": "Summary",
  "deleteCheck": "Delete check",
  "retirePlan": "Retire plan",
  "profileJSONInvalid": "Invalid profile JSON",
  "recentErrors": "Recent errors",
  "includeWarnings": "Include warnings",
  "searchAllLogs": "Search all logs",
  "download": "Download",
  "rotateLog": "Rotate log",
  "rotateLogConfirm": "Rotate the current log and create a new file?",
  "rotateSecret": "Rotate secret",
  "rotateKey": "Rotate key",
  "publicKnowledgeBases": "Public Knowledge Bases",
  "publicKnowledgeHint": "Manage named shared knowledge bases. Import text, documents, archives, and crawled URLs.",
  "publicKnowledgeName": "Public knowledge base name",
  "createPublicKnowledgeBase": "Create public knowledge base",
  "addPublicKnowledgeScope": "Add public knowledge base",
  "configuredKnowledgeScopes": "Configured knowledge scopes",
  "effectiveKnowledgeAccess": "Effective knowledge access",
  "ownKnowledge": "Own knowledge",
  "sharedKnowledge": "Shared user knowledge",
  "selectedUserForPublicKnowledge": "Select the target user first, then attach or detach public knowledge bases.",
  "attachPublicKnowledge": "Attach",
  "detachPublicKnowledge": "Detach",
  "importText": "Import text",
  "importDocumentArchive": "Import document/archive",
  "importURL": "Import URL",
  "importing": "Importing...",
  "importStarted": "Knowledge import started",
  "importedKnowledge": "Knowledge import completed",
  "importCompleted": "Knowledge import completed",
  "importStillRunning": "Knowledge import still running",
  "importJob": "Import job",
  "importStatus": "Status",
  "importSource": "Source",
  "importTitle": "Title",
  "importKind": "Kind",
  "importFiles": "Files",
  "importUrls": "URLs",
  "importProcessed": "Processed",
  "importImported": "Imported",
  "importFailed": "Failed",
  "importSkipped": "Skipped",
  "importDuplicates": "Duplicates",
  "importWarnings": "Warnings",
  "sameDomainOnly": "Same domain only",
  "crawlDepth": "Crawl depth",
  "labels": "labels",
  "urlPlaceholder": "https://example.com/docs",
  "deletePublicKnowledgeConfirm": "Delete this public knowledge base and its knowledge sources?",
  "importTextRequired": "Enter text to import.",
  "importURLRequired": "Enter at least one URL to import.",
  "skillSourcesHintDetailed": "Skill sources are channels for finding and installing skills, not knowledge bases. Priority: user-specific policy > tenant-specific policy > global default policy. Disabled layers inherit from the parent layer.",
  "skillPolicyPriorityTitle": "Effective order",
  "globalPolicyShort": "Global default",
  "tenantOverrideHintDetailed": "Tenant override: affects all users in the selected tenant and overrides the global default when enabled.",
  "userOverrideHintDetailed": "User override: affects only the selected user and has the highest priority when enabled.",
  "globalSkillPolicyTitle": "Global default policy",
  "tenantSkillPolicyTitle": "Tenant-specific policy",
  "userSkillPolicyTitle": "User-specific policy",
  "globalSkillPolicyHint": "Default skill sources for all tenants and users unless tenant/user-specific policies are enabled.",
  "tenantSkillPolicyHint": "Set skill sources for the selected tenant. When enabled, users in this tenant use this by default.",
  "userSkillPolicyHint": "Set skill sources for the selected user. When enabled, this has highest priority and affects only that user.",
  "skillPolicyEnabledHint": "Enable this policy layer. When disabled, it inherits from the parent layer.",
  "skillhubDesc": "Official SkillHub / SkillMarket, the preferred controlled skill marketplace.",
  "localDesc": "Local ZIP / import upload without remote skill marketplace access.",
  "clawhubDesc": "Community ClawHub mirror for shared community skills.",
  "githubDesc": "GitHub open source repositories, broader but requiring source trust review.",
  "enterpriseHubDesc": "Enterprise internal capability market for approved company skills."
};
const dicts = {"zh-CN": zh, "en-US": en};
const fallbackLabels = {
  "clientConfig": "客户端配置",
  "clientConfigHint": "所有用户共用的搜索、代理、安全网络、MCP、SSH、技能源和界面配置",
  "clientConfigDefaults": "共享客户端配置",
  "clientConfigDefaultsHint": "保存后由所有 MaClawSrv 用户共用，并通过运行时生效配置立即生效。",
  "webSearch": "联网搜索",
  "webSearchProviders": "搜索服务 JSON",
  "currentProvider": "当前搜索服务",
  "proxyConfig": "代理服务",
  "sharedTools": "公用工具资源",
  "securityDefaults": "安全与网络默认",
  "experienceDefaults": "用户界面",
  "advancedJSON": "高级 JSON",
  "advancedJSONHint": "可配置 MCP Servers、Local MCP、SSH Hosts、SkillHub URLs、外部技能目录等公用数组。",
  "applyToNewUsers": "应用到所有用户",
  "validateOnly": "仅校验",
  "defaultClientConfigSaved": "共享客户端配置已保存",
  "providerType": "服务类型",
  "providerKey": "API Key",
  "providerBaseURL": "Base URL",
  "networkLevel": "网络级别",
  "allowlist": "允许名单"
};
Object.assign(en, {
  searchProviderHelp: "Choose which search engine MaClawSrv users use for web search. Brave, Serper, TinyFish, and Tavily can use API keys; DuckDuckGo needs no key.",
  freeNoKey: "Free, no key needed",
  apiKeySupported: "API key supported",
  searchProviderBraveHint: "Uses Brave Search API. Without an API key, runtime falls back to the default direct web search.",
  searchProviderSerperHint: "Uses Serper Search API. Without an API key, runtime falls back to the default direct web search.",
  searchProviderTinyfishHint: "Uses TinyFish Search & Fetch API for web search and content extraction. Without an API key, runtime falls back to default direct web search.",
  searchProviderTavilyHint: "Uses Tavily Search API (1000 free searches/month). Without an API key, runtime falls back to default direct web search.",
  searchProviderDuckduckgoHint: "DuckDuckGo is the free option and requires no API key.",
  searchProviderGenericHint: "Configure this provider's API key and endpoint.",
  enterApiKey: "Enter API Key",
  noExtraConfig: "No extra configuration is needed for this provider."
});
Object.assign(zh, {
  searchProviderHelp: "选择 MaClawSrv 用户网页搜索使用的搜索服务。Brave、Serper、TinyFish、Tavily 可配置 API Key；DuckDuckGo 无需 Key。",
  freeNoKey: "免费，无需 Key",
  apiKeySupported: "可配置 API Key",
  searchProviderBraveHint: "使用 Brave Search API。未填写 API Key 时，运行时会回退到默认联网搜索。",
  searchProviderSerperHint: "使用 Serper Search API。未填写 API Key 时，运行时会回退到默认联网搜索。",
  searchProviderTinyfishHint: "使用 TinyFish Search & Fetch API，支持网页搜索和内容提取。未填写 API Key 时，运行时会回退到默认联网搜索。",
  searchProviderTavilyHint: "使用 Tavily Search API（每月 1000 次免费搜索）。未填写 API Key 时，运行时会回退到默认联网搜索。",
  searchProviderDuckduckgoHint: "DuckDuckGo 是免费选项，无需 API Key。",
  searchProviderGenericHint: "配置此搜索服务的 API Key 和接入地址。",
  enterApiKey: "输入 API Key",
  noExtraConfig: "当前服务商无需额外配置。"
});
Object.assign(en, { experienceDefaults: "User Interface" });
Object.assign(zh, { experienceDefaults: "用户界面" });
Object.assign(en, {
  securityGuardrails: "Safety guardrails",
  securityModeDefault: "Default",
  securityModeRelaxed: "Relaxed",
  securityModeStandard: "Standard",
  securityModeStrict: "Strict",
  securityModeDeveloper: "Developer"
});
Object.assign(zh, {
  securityGuardrails: "安全护栏",
  securityModeDefault: "默认",
  securityModeRelaxed: "宽松",
  securityModeStandard: "标准",
  securityModeStrict: "严格",
  securityModeDeveloper: "开发者"
});
const sections = ["overview","sandbox","logs","config","clientConfig","tenants","accounts","knowledge","ops"];
const initialSection = sections.includes(location.hash.slice(1)) ? location.hash.slice(1) : (sections.includes(localStorage.getItem("maclaw.admin.section")) ? localStorage.getItem("maclaw.admin.section") : "overview");
const state = { locale: localStorage.getItem("maclaw.admin.locale") || "zh-CN", token: localStorage.getItem("maclaw.admin.token") || "", section: initialSection, sectionChanged: false, me: null, pendingRiskFilter: null, pendingRequests: 0, knowledgeTenantNames: {}, knowledgeUserNames: {}, locales: [{locale:"zh-CN",label:"zh-CN"},{locale:"en-US",label:"English"}] };
const $ = (id) => document.getElementById(id);
const t = (k, vars) => { let out=(dicts[state.locale] || zh)[k] || fallbackLabels[k] || k; for(const [name,value] of Object.entries(vars||{})) out=out.replaceAll(`{${name}}`, String(value)); return out; };
const colLabels={id:"genericID",name:"name",email:"email",username:"username",display_name:"displayName",role:"role",status:"status",locale:"language",last_login_at:"lastLoginAt",active:"activeState",created_at:"createdAt",expires_at:"expiresAt",remote_ip:"remoteIP",tenant_id:"tenantID",tenant_name:"tenantName",user_id:"userID",owner_id:"userID",owner_name:"knowledgeOwner",title:"name",updated_at:"generatedAt",generated_at:"generatedAt",latest_source_at:"latestSourceAt",source_count:"sourceCount",distilled_sources:"distilledSources",kind:"kind",next_run_at:"nextRunAt",last_error:"lastError",report_id:"reportID",effective_backend:"effectiveBackend",resource_type:"resourceType",resource_id:"resourceID",backend:"backend",network:"network",available:"available",smoke_status:"smokeStatus",version:"version",detail:"detail",severity:"severity",expected:"expected",actual:"actual",exists:"exists",size_bytes:"sizeBytes",modified_at:"modifiedAt",delete_protected:"deleteProtectedColumn",api_key_hint:"apiKeyHint",scope:"scope",profile:"profile",raw:"raw",reason:"reason",summary:"summary",key:"configKey",value:"current",env_key:"envKey",configured:"configuredState",default:"defaultValue",source:"source",restart_required:"restartRequired",mutable_at_runtime:"runtimeOnly",changed:"changed",current:"current",desired:"desired",sensitive:"sensitive",action:"action"};
const tableCol=(c)=>colLabels[c]?t(colLabels[c]):c;
const esc = (v) => String(v ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;","\"":"&quot;","'":"&#39;"}[c]));
const pretty = (v) => JSON.stringify(v, null, 2);
function emptyState(label){ return `<div class="empty-state" role="status"><span aria-hidden="true"></span><p>${esc(label||t("empty"))}</p></div>`; }
function optionText(parts){ return parts.filter(Boolean).join(" / "); }
function displayWithID(label,id){ const text=String(label||"").trim(); const raw=String(id||"").trim(); if(!text||text===raw) return raw||"-"; return `${text} (${raw})`; }
function tenantSelect(id,items,selected="",blank=true,blankLabel="allOption"){ const opts=blank?`<option value="">${t(blankLabel)}</option>`:""; return `<select id="${esc(id)}">${opts}${(items||[]).map(x=>`<option value="${esc(x.id)}" ${selected===x.id?"selected":""}>${esc(optionText([x.name,x.id]))}</option>`).join("")}</select>`; }
function userSelect(id,items,selected="",mode="id",blank=true,blankLabel="allOption"){ const opts=blank?`<option value="">${t(blankLabel)}</option>`:""; return `<select id="${esc(id)}">${opts}${(items||[]).filter(x=>mode!=="email"||x.email).map(x=>{ const value=mode==="tenantUser"?`${x.tenant_id||""}:${x.id||""}`:(mode==="email"?x.email:x.id); return `<option value="${esc(value)}" ${selected===value?"selected":""}>${esc(optionText([x.name,x.email,x.tenant_id,x.id]))}</option>`; }).join("")}</select>`; }
function adminSelect(id,items,selected="",blank=true,blankLabel="allOption"){ const opts=blank?`<option value="">${t(blankLabel)}</option>`:""; return `<select id="${esc(id)}">${opts}${(items||[]).map(x=>`<option value="${esc(x.id)}" ${selected===x.id?"selected":""}>${esc(optionText([x.username,x.display_name,cellText("role",x.role),x.id]))}</option>`).join("")}</select>`; }
function optionLabel(value,col="status"){ const v=String(value??""); const labels={auto:"modeAuto",none:"modeNone",default:"defaultOption",host:"networkHost",error:"logError",info:"logInfo"}; if(col==="strict"&&v==="true") return t("strictEnabled"); if(col==="strict"&&v==="false") return t("strictDisabled"); return labels[v]?t(labels[v]):cellText(col,v); }
function localizedOptions(values,col="status"){ return (values||[]).map(v=>`<option value="${esc(v)}">${esc(optionLabel(v,col))}</option>`).join(""); }
function splitTenantUser(value){ const [tenant,user]=String(value||"").split(":"); return {tenant:tenant||"",user:user||""}; }
function tenantUserValue(tenantId,userId){ const selected=splitTenantUser($(userId)?.value||""); return {tenant:selected.tenant||($(tenantId)?.value||""),user:selected.user||($(userId)?.value||"")}; }
function syncTenantFromUser(userId,tenantId){ const user=$(userId); const tenant=$(tenantId); if(!user||!tenant) return; user.onchange=()=>{ const ids=splitTenantUser(user.value); if(ids.tenant) tenant.value=ids.tenant; }; tenant.onchange=()=>{ const ids=splitTenantUser(user.value); if(ids.tenant&&tenant.value&&ids.tenant!==tenant.value) user.value=""; }; }
function toast(msg){ const el=$("toast"); const text=String(msg||""); const lower=text.toLowerCase(); const kind=lower.includes("failed")||lower.includes("error")||lower.includes("失败")?"bad":(lower.includes("loaded")||lower.includes("saved")||lower.includes("成功")||lower.includes("已")?"ok":"info"); el.textContent=text; el.setAttribute("role","status"); el.setAttribute("aria-live","polite"); if(kind==="bad"){ el.setAttribute("role","alert"); el.setAttribute("aria-live","assertive"); } el.className=`toast toast-${kind}`; clearTimeout(window.toastTimer); window.toastTimer=setTimeout(()=>el.classList.add("hidden"),3500); }
function setNetworkBusy(delta){ state.pendingRequests=Math.max(0,(state.pendingRequests||0)+delta); document.body.classList.toggle("is-fetching",state.pendingRequests>0); }
function headers(json=true){ const h={}; if(json) h["Content-Type"]="application/json"; if(state.token) h["X-MaClaw-Admin-Secret"]=state.token; return h; }
async function api(path, opts={}){ setNetworkBusy(1); try{ const res=await fetch(path,{...opts,headers:{...headers(opts.body!==undefined),...(opts.headers||{})}}); const text=await res.text(); let body=text; try{ body=text?JSON.parse(text):{} }catch{} if(!res.ok){ const err=new Error(body?.error || text || res.statusText); err.status=res.status; err.body=body; throw err; } return body; } finally { setNetworkBusy(-1); } }
async function downloadAdmin(path, fallbackName="maclaw-admin-download"){ setNetworkBusy(1); try{ const res=await fetch(path,{headers:headers(false)}); const blob=await res.blob(); if(!res.ok){ let msg=res.statusText; try{msg=(await blob.text())||msg}catch{} throw new Error(msg); } const cd=res.headers.get("content-disposition")||""; const m=cd.match(/filename="?([^";]+)"?/i); const a=document.createElement("a"); a.href=URL.createObjectURL(blob); a.download=m?.[1]||fallbackName; document.body.appendChild(a); a.click(); setTimeout(()=>{URL.revokeObjectURL(a.href); a.remove();},1000); } finally { setNetworkBusy(-1); } }
async function textAdmin(path){ setNetworkBusy(1); try{ const res=await fetch(path,{headers:headers(false)}); const text=await res.text(); if(!res.ok) throw new Error(text || res.statusText); return text; } finally { setNetworkBusy(-1); } }
function setTitle(title,hint=""){ $("pageTitle").textContent=title; $("pageHint").textContent=hint; document.title=`${title} · MaClawSrv Admin`; const main=$("main"); if(main) main.setAttribute("aria-labelledby","pageTitle"); const content=$("content"); if(content) content.setAttribute("aria-describedby", hint ? "pageHint" : ""); }
function isOwner(){ return state.me?.auth_type==="admin_secret" || state.me?.admin?.role==="owner"; }
function applyOwnerGuards(){ if(isOwner()) return; const tip=t("ownerOnly"); ["runRuntimeGC","rotateLog","saveSandbox","rollbackSandbox","installSandboxRun","saveSandboxProfile","createTenant","createTenantUser","createCredential","saveCfg","exportCfg","clearCfgDraft","clearTenantKnowledge","saveKnowledgeCrossTenant","saveKnowledgeAccess","deleteKnowledgeAccess","createPublicKnowledge","publicKnowledgeImportText","publicKnowledgeImportFile","publicKnowledgeImportURLs","saveSkillGlobal","saveSkillTenant","deleteSkillTenant","saveSkillUser","deleteSkillUser","runImport","createSnapshot","pruneSnapshots"].forEach(id=>{ const el=$(id); if(el){ el.disabled=true; el.title=tip; }}); document.querySelectorAll("[data-job-cancel],[data-tenant-status],[data-tenant-delete],[data-user-status],[data-user-delete],[data-credential-status],[data-credential-secret],[data-credential-key],[data-credential-revoke],[data-admin-toggle],[data-session-revoke],[data-sandbox-report-delete],[data-sandbox-profile-delete],[data-snapshot-restore-run],[data-snapshot-delete],[data-public-kb-add],[data-public-kb-remove],[data-public-kb-delete]").forEach(el=>{ el.disabled=true; el.title=tip; }); }
function bindNavKeyboard(){ const buttons=[...$("nav").querySelectorAll("button")]; buttons.forEach((b,i)=>b.onkeydown=(e)=>{ const keys={ArrowDown:1,ArrowRight:1,ArrowUp:-1,ArrowLeft:-1}; if(e.key==="Home"||e.key==="End"||keys[e.key]){ e.preventDefault(); const next=e.key==="Home"?0:e.key==="End"?buttons.length-1:(i+keys[e.key]+buttons.length)%buttons.length; buttons[next]?.focus(); } if(e.key==="Enter"||e.key===" "){ e.preventDefault(); b.click(); } }); document.onkeydown=(e)=>{ if(e.altKey||e.ctrlKey||e.metaKey||e.shiftKey) return; const target=e.target; if(target&&["INPUT","TEXTAREA","SELECT"].includes(target.tagName)) return; const n=Number(e.key); if(n>=1&&n<=buttons.length){ e.preventDefault(); buttons[n-1]?.click(); buttons[n-1]?.focus(); } }; }
function setSection(id, updateHash=true){ if(!sections.includes(id)) id="overview"; state.sectionChanged=state.section!==id; state.section=id; localStorage.setItem("maclaw.admin.section",id); if(updateHash&&location.hash!==`#${id}`) history.replaceState(null,"",`#${id}`); }
function setAuthShell(on,target="loginPanel"){ const active=!!on; document.body.classList.toggle("auth-screen",active); $("app")?.classList.toggle("auth-only",active); $("skipLink")?.setAttribute("href",active?`#${target}`:"#content"); }
function renderShell(){ document.documentElement.lang=state.locale; const localeSelect=$("localeSelect"); if(localeSelect){ localeSelect.innerHTML=(state.locales||[]).map(x=>`<option value="${esc(x.locale)}">${esc(x.label||x.locale)}</option>`).join(""); localeSelect.value=state.locale; } document.querySelectorAll("[data-i18n]").forEach(n=>n.textContent=t(n.dataset.i18n)); $("nav").innerHTML=sections.map((id,i)=>`<button type="button" class="${state.section===id?"active":""}" data-section="${id}" data-shortcut="${i+1}" title="${esc(i+1)} · ${esc(t(id))}" aria-current="${state.section===id?"page":"false"}">${t(id)}</button>`).join(""); $("nav").querySelectorAll("button").forEach(b=>b.onclick=()=>{setSection(b.dataset.section); render();}); bindNavKeyboard(); const badge=$("authBadge"); badge.textContent=state.me?.admin?.username || state.me?.auth_type || (state.token ? "admin" : t("noToken")); badge.className=`badge ${state.token?"badge-on":"badge-off"}`; }
function focusPrimaryInput(scope){ const root=scope||document; const el=root.querySelector("input:not([type='hidden']):not(:disabled), select:not(:disabled), textarea:not(:disabled), button:not(:disabled)"); if(el&&window.matchMedia("(pointer: fine)").matches) setTimeout(()=>el.focus({preventScroll:true}),0); }
function enhanceA11y(){ const content=$("content"); if(content){ content.setAttribute("role","region"); content.setAttribute("aria-live","polite"); content.setAttribute("aria-busy","false"); } document.querySelectorAll("input,select,textarea").forEach(el=>{ const hasName=el.getAttribute("aria-label")||el.getAttribute("aria-labelledby")||el.closest("label")||document.querySelector(`label[for="${CSS.escape(el.id||"")}"]`); if(hasName) return; const label=el.placeholder||el.name||el.id||el.type; if(label) el.setAttribute("aria-label",label); }); document.querySelectorAll("th").forEach(th=>th.setAttribute("scope","col")); document.querySelectorAll(".card,.panel").forEach((box,i)=>{ const h=box.querySelector(":scope > h2"); if(!h) return; if(!h.id) h.id=`section-title-${state.section}-${i}`; box.setAttribute("aria-labelledby",h.id); }); document.querySelectorAll(".table-wrap").forEach((wrap,i)=>{ wrap.setAttribute("tabindex","0"); wrap.setAttribute("role","region"); if(!wrap.getAttribute("aria-label")) wrap.setAttribute("aria-label",`${t("raw")} ${i+1}`); }); document.querySelectorAll(".code, pre").forEach((block,i)=>{ block.setAttribute("tabindex","0"); block.setAttribute("role","region"); if(!block.getAttribute("aria-label")) block.setAttribute("aria-label",`${t("raw")} ${i+1}`); }); document.querySelectorAll("button").forEach(btn=>{ if(!btn.getAttribute("aria-label")&&btn.title&&!btn.textContent.trim()) btn.setAttribute("aria-label",btn.title); }); }
function authLocaleControl(){ return `<label class="auth-locale"><span>${t("language")}</span><select id="authLocaleSelect" aria-label="${esc(t("language"))}">${localeOptions()}</select></label>`; }
function bindAuthLocale(onChange){ const el=$("authLocaleSelect"); if(!el) return; el.value=state.locale; el.onchange=()=>{ state.locale=el.value; localStorage.setItem("maclaw.admin.locale",state.locale); onChange?.(); }; }
function renderLogin(focusMode){ setAuthShell(true); setTitle(t("login"), t("loginHint")); const loginMode=localStorage.getItem("maclaw.admin.loginMode")==="secret"?"secret":"account"; const accountActive=loginMode==="account"; $("loginPanel").classList.remove("hidden"); $("loginPanel").innerHTML=`<div class="admin-auth-shell"><section class="admin-auth-hero"><div class="auth-brand-row"><div class="auth-brand-mark">M</div><div><strong>MaClawSrv</strong><span>${t("adminConsole")}</span></div></div><div class="auth-hero-copy"><span class="auth-kicker">${t("authLoginKicker")}</span><h2>${t("login")}</h2><p>${t("loginHint")}</p></div><div class="auth-points"><div><strong>${t("security")}</strong><span>${t("audit")}</span></div><div><strong>${t("tenants")}</strong><span>${t("users")}</span></div><div><strong>${t("config")}</strong><span>${t("effective")}</span></div></div></section><section class="admin-auth-card"><div class="auth-card-head"><span class="auth-kicker">${t("authAccessKicker")}</span><h2>${t("login")}</h2><p>${t("loginHint")}</p></div>${authLocaleControl()}<div class="segmented auth-tabs" role="tablist" aria-label="${esc(t("login"))}"><button type="button" role="tab" id="loginTabAccount" aria-controls="loginPanelAccount" aria-selected="${accountActive}" tabindex="${accountActive?"0":"-1"}" class="${accountActive?"active":""}" data-login-mode="account">${t("accountLogin")}</button><button type="button" role="tab" id="loginTabSecret" aria-controls="loginPanelSecret" aria-selected="${!accountActive}" tabindex="${!accountActive?"0":"-1"}" class="${!accountActive?"active":""}" data-login-mode="secret">${t("secretLogin")}</button></div><div id="loginPanelAccount" class="login-method-panel ${accountActive?"":"hidden"}" role="tabpanel" aria-labelledby="loginTabAccount" aria-hidden="${!accountActive}"><div class="form-grid auth-form-grid"><div class="field"><label for="loginUser">${t("username")}</label><input id="loginUser" autocomplete="username" required></div><div class="field"><label for="loginPass">${t("password")}</label><input id="loginPass" type="password" autocomplete="current-password" required></div></div><div class="row mt-12 auth-actions"><button id="loginSubmit">${t("login")}</button></div></div><div id="loginPanelSecret" class="login-method-panel ${accountActive?"hidden":""}" role="tabpanel" aria-labelledby="loginTabSecret" aria-hidden="${accountActive}"><div class="field"><label for="secretToken">${t("adminSecret")}</label><div class="secret-field-row"><input id="secretToken" type="password" value="${esc(state.token)}" autocomplete="off" spellcheck="false" required><button type="button" class="secondary w-auto" id="toggleSecret" aria-controls="secretToken" aria-pressed="false">${t("showSecret")}</button></div><p class="helper-text">${t("secretLoginHint")}</p></div><div class="row mt-12 auth-actions"><button id="secretSubmit">${t("useSecret")}</button></div></div></section></div>`; const switchLoginMode=(mode,keepFocus=false)=>{localStorage.setItem("maclaw.admin.loginMode",mode); renderLogin(keepFocus?mode:"");}; $("loginPanel").querySelectorAll("[data-login-mode]").forEach(btn=>{btn.onclick=()=>switchLoginMode(btn.dataset.loginMode); btn.onkeydown=e=>{ if(["ArrowLeft","ArrowRight","Home","End"].includes(e.key)){ e.preventDefault(); switchLoginMode(e.key==="ArrowLeft"||e.key==="Home"?"account":"secret",true); } };}); const requireFields=(ids)=>{for(const id of ids){const el=$(id); if(!el?.value.trim()){toast(t("requiredField")); el?.focus({preventScroll:true}); return false; }} return true;}; const toggleSecret=$("toggleSecret"); if(toggleSecret) toggleSecret.onclick=()=>{const input=$("secretToken"); const show=input.type==="password"; input.type=show?"text":"password"; toggleSecret.textContent=show?t("hideSecret"):t("showSecret"); toggleSecret.setAttribute("aria-pressed",String(show)); input.focus({preventScroll:true});}; const loginSubmit=$("loginSubmit"); if(loginSubmit) loginSubmit.onclick=async()=>{if(!requireFields(["loginUser","loginPass"])) return; try{const out=await api("/api/v1/admin/auth/login",{method:"POST",body:JSON.stringify({username:$("loginUser").value.trim(),password:$("loginPass").value})}); state.token=out.token; localStorage.setItem("maclaw.admin.token",state.token); await loadMe(); await loadLocales().catch(()=>{}); render();}catch(e){toast(`${t("failed")}: ${e.message}`)}}; const secretSubmit=$("secretSubmit"); if(secretSubmit) secretSubmit.onclick=async()=>{if(!requireFields(["secretToken"])) return; state.token=$("secretToken").value.trim(); localStorage.setItem("maclaw.admin.token",state.token); await loadMe().catch(()=>{}); await loadLocales().catch(()=>{}); render();}; ["loginUser","loginPass"].forEach(id=>{const el=$(id); if(el) el.onkeydown=e=>{ if(e.key==="Enter") $("loginSubmit").click(); };}); const secretToken=$("secretToken"); if(secretToken) secretToken.onkeydown=e=>{ if(e.key==="Enter") $("secretSubmit").click(); }; bindAuthLocale(()=>renderLogin(focusMode)); enhanceA11y(); if(focusMode){ $(`loginTab${focusMode==="secret"?"Secret":"Account"}`)?.focus({preventScroll:true}); }else{ focusPrimaryInput($("loginPanel")); } }
function renderBootstrap(status){ setAuthShell(true,"bootstrapPanel"); setTitle(t("setup"), t("setupHint")); const minPass=status.password_policy?.min_length||12; const tokenRequired=!!status.setup_token_required; $("bootstrapPanel").classList.remove("hidden"); $("bootstrapPanel").innerHTML=`<div class="admin-auth-shell"><section class="admin-auth-hero"><div class="auth-brand-row"><div class="auth-brand-mark">M</div><div><strong>MaClawSrv</strong><span>${t("adminConsole")}</span></div></div><div class="auth-hero-copy"><span class="auth-kicker">${t("authSetupKicker")}</span><h2>${t("setup")}</h2><p>${t("setupHint")}</p></div><div class="auth-points"><div><strong>${t("owner")}</strong><span>${t("createAdmin")}</span></div><div><strong>${t("security")}</strong><span>${t("setupToken")}</span></div><div><strong>${t("language")}</strong><span>${t("locales")}</span></div></div></section><section class="admin-auth-card"><div class="auth-card-head"><span class="auth-kicker">${t("authSetupAccessKicker")}</span><h2>${t("setup")}</h2><p>${t("setupHint")}</p></div>${authLocaleControl()}<div id="setupError" class="error-panel hidden" role="alert"></div><div class="form-grid auth-form-grid">${tokenRequired?`<div class="field"><label for="setupToken">${t("setupToken")}</label><input id="setupToken" autocomplete="off" required><p class="helper-text">${t("setupTokenEnvHint")}</p></div>`:`<input id="setupToken" type="hidden" value="">`}<div class="field"><label for="setupUser">${t("username")}</label><input id="setupUser" value="admin" autocomplete="username" required></div><div class="field"><label for="setupPass">${t("password")}</label><input id="setupPass" type="password" autocomplete="new-password" minlength="${minPass}" required><p class="helper-text">${t("minPassword",{min:minPass})}</p></div><div class="field"><label for="setupName">${t("displayName")}</label><input id="setupName" value="Owner"></div><div class="field"><label for="setupLocale">${t("language")}</label><select id="setupLocale">${localeOptions()}</select></div></div><div class="row mt-12 auth-actions"><button id="setupSubmit">${t("setup")}</button><span class="muted">${tokenRequired?t("setupTokenEnvHint"):t("noSetupTokenRequired")} · ${t("minPassword",{min:minPass})}</span></div></section></div>`; const fail=(msg)=>{const box=$("setupError"); box.textContent=msg; box.classList.remove("hidden"); toast(msg);}; $("setupSubmit").onclick=async()=>{const btn=$("setupSubmit"); const user=$("setupUser").value.trim(); const pass=$("setupPass").value; if(tokenRequired&&!$("setupToken").value.trim()) return fail(t("setupTokenRequiredMessage")); if(!user) return fail(t("usernameRequiredMessage")); if(pass.length<minPass) return fail(t("passwordMinMessage",{min:minPass})); btn.disabled=true; $("setupError").classList.add("hidden"); try{await api("/api/v1/admin/bootstrap/initialize",{method:"POST",body:JSON.stringify({setup_token:$("setupToken").value.trim(),username:user,password:pass,display_name:$("setupName").value.trim(),locale:$("setupLocale").value})}); toast(t("initializedMessage")); state.token=""; localStorage.removeItem("maclaw.admin.token"); render();}catch(e){fail(`${t("failed")}: ${e.message}`)}finally{btn.disabled=false;}}; ["setupToken","setupUser","setupPass","setupName"].forEach(id=>{const el=$(id); if(el) el.onkeydown=e=>{ if(e.key==="Enter") $("setupSubmit").click(); };}); bindAuthLocale(()=>renderBootstrap(status)); enhanceA11y(); focusPrimaryInput($("bootstrapPanel")); }
async function loadMe(){ if(!state.token){state.me=null; return;} state.me=await api("/api/v1/admin/auth/me"); if(!localStorage.getItem("maclaw.admin.locale")&&state.me?.admin?.locale){ state.locale=state.me.admin.locale; } }
function localeOptions(selected=state.locale){ return (state.locales||[]).map(x=>`<option value="${esc(x.locale)}" ${selected===x.locale?"selected":""}>${esc(x.label||x.locale)}</option>`).join(""); }
function applyLocaleMetadata(out){ if(out?.locales) state.locales=out.locales; if(!localStorage.getItem("maclaw.admin.locale")){ state.locale=state.me?.admin?.locale||(out&&out.default_locale)||state.locale; } }
async function loadLocales(){ if(!state.token) return; const out=await api("/api/v1/admin/i18n/locales"); applyLocaleMetadata(out); }
async function startup(){ renderShell(); try{ const bs=await api("/api/v1/admin/bootstrap/status",{headers:{}}); applyLocaleMetadata(bs); if(!bs.initialized){ hideMain(true); renderBootstrap(bs); return; } }catch(e){} if(state.token){ await loadMe().catch(()=>{state.me=null; state.token=""; localStorage.removeItem("maclaw.admin.token");}); await loadLocales().catch(()=>{}); } render(); }
function hideMain(authOnly=false){ setAuthShell(authOnly); $("content").innerHTML=""; $("content").className="content"; $("loginPanel").classList.add("hidden"); $("bootstrapPanel").classList.add("hidden"); renderShell(); }
function clearTransientAdminModals(){ document.querySelectorAll("#credentialModalBackdrop,.tenant-users-modal,.admin-result-modal").forEach(el=>{ const backdrop=el.id==="credentialModalBackdrop"?el:el.closest(".modal-backdrop"); backdrop?.remove(); }); }
async function render(){ clearTransientAdminModals(); hideMain(!state.token); if(!state.token){ renderLogin(); return; } renderShell(); $("content").setAttribute("aria-busy","true"); $("content").innerHTML=`<div class="card loading-card"><div class="skeleton skeleton-title"></div><div class="skeleton skeleton-line"></div><div class="skeleton skeleton-line short"></div></div>`; const f={overview,sandbox,logs,config,clientConfig,tenants,accounts,knowledge,ops}[state.section] || overview; try{ await f(); applyOwnerGuards(); enhanceA11y(); if(state.sectionChanged){ $("main")?.focus({preventScroll:true}); state.sectionChanged=false; } }catch(e){ if(e.status===401){state.token="";localStorage.removeItem("maclaw.admin.token");hideMain(true);renderLogin();return;} $("content").innerHTML=`<div class="panel error-panel"><h2>${t("failed")}</h2><pre class="code">${esc(e.message)}\n${esc(pretty(e.body||{}))}</pre></div>`; enhanceA11y(); if(state.sectionChanged){ $("main")?.focus({preventScroll:true}); state.sectionChanged=false; } } }
async function overview(){ setTitle(t("overview"), t("overviewHint")); const [rt,dash,ready,scheduler,jobs,security,tenantsResp,usersResp]=await Promise.all([api("/api/v1/admin/runtime/status"),api("/api/v1/admin/dashboard").catch(e=>({error:e.message})),api("/api/v1/admin/system/readiness"),api("/api/v1/admin/scheduler/status").catch(e=>({error:e.message,recent_tasks:[]})),api("/api/v1/admin/jobs?limit=50").catch(e=>({error:e.message,items:[]})),api("/api/v1/admin/security/summary").catch(e=>({error:e.message,status:"unknown",counts:{},recent:[]})),api("/api/v1/admin/tenants?limit=500").catch(e=>({error:e.message,items:[]})),api("/api/v1/admin/users?limit=500").catch(e=>({error:e.message,items:[]}))]); const tenantItems=tenantsResp.items||[]; const userItems=usersResp.items||[]; $("content").innerHTML=`<div class="grid"><div class="card metric"><span>${t("runtimeLabel")}</span><b class="${rt.readiness?.status==='ok'||rt.ready?"status-ok":"status-warn"}">${esc(cellText("status",rt.readiness?.status||rt.status||rt.ready))}</b></div><div class="card metric"><span>${t("sandbox")}</span><b>${esc(rt.sandbox?.effective_backend||rt.sandbox?.mode||"-")}</b><span>${esc(cellText("status",rt.last_sandbox_report?.status||t("noReport")))}</span></div><div class="card metric"><span>${t("jobs")}</span><b>${esc(Object.values(rt.jobs||{}).reduce((a,b)=>a+b,0))}</b></div><div class="card metric"><span>${t("scheduler")}</span><b>${esc(scheduler.enabled?t("enabledState"):t("disabledState"))}</b></div><div class="card metric"><span>${t("security")}</span><b class="${security.status==='ok'?'status-ok':'status-warn'}">${esc(cellText("status",security.status||t("unknown")))}</b><span>${esc(t("highMedium",{high:security.counts?.high||0,medium:security.counts?.medium||0}))}</span></div></div><div class="card"><h2>${t("securityRisks")}</h2>${riskFilterSummary(security)}<h3>${t("bySeverity")}</h3>${riskCountChips(security.counts,"severity")}<h3>${t("byKind")}</h3>${riskCountChips(security.kind_counts)}${riskEventsTable(security.recent||[])}</div><div class="card"><h2>${t("asyncJobs")}</h2><div class="row"><select id="jobStatus"><option value="">${t("allOption")}</option>${localizedOptions(["pending","running","succeeded","failed","canceled"])}</select><input id="jobKind" placeholder="${t("kindPlaceholder")}">${tenantSelect("jobTenant",tenantItems)}${userSelect("jobUser",userItems,"","tenantUser")}<button id="loadJobs">${t("refresh")}</button></div><div id="jobsTable">${jobTable(jobs.items||[])}</div></div><div class="card"><h2>${t("scheduler")}</h2>${table(scheduler.recent_tasks||[],["id","name","status","next_run_at","last_error"],null)}</div><div class="card"><h2>${t("raw")}</h2><button class="secondary" id="runRuntimeGC">${t("runGC")}</button><button class="secondary" id="viewGoroutines">${t("goroutines")}</button><button class="secondary" id="downloadGoroutines">${t("downloadGoroutines")}</button><button class="secondary" id="viewHeapProfile">${t("heapProfile")}</button><button class="secondary" id="downloadHeapProfile">${t("downloadHeapProfile")}</button><button class="secondary" id="serviceSupportBundle">${t("serviceSupportBundle")}</button><button class="secondary" id="serviceSupportBundleDownload">${t("download")}</button><pre id="overviewOut" class="code">${esc(pretty({runtime:rt,dashboard:dash,readiness:ready,scheduler,jobs,security,tenants:tenantsResp,users:usersResp}))}</pre></div>`; syncTenantFromUser("jobUser","jobTenant"); bindJobActions(); bindRiskCountChips(); bindRiskEventActions(security.recent||[]); $("runRuntimeGC").onclick=async()=>{$("overviewOut").textContent=pretty(await api("/api/v1/admin/runtime/gc",{method:"POST"}));}; $("viewGoroutines").onclick=async()=>{try{$("overviewOut").textContent=await textAdmin("/api/v1/admin/runtime/goroutines?debug=1");}catch(e){$("overviewOut").textContent=`${t("failed")}: ${e.message}`; toast(`${t("failed")}: ${e.message}`);}}; $("downloadGoroutines").onclick=()=>downloadAdmin("/api/v1/admin/runtime/goroutines?debug=2&download=true","goroutines.txt").catch(e=>toast(`${t("failed")}: ${e.message}`)); $("viewHeapProfile").onclick=async()=>{try{$("overviewOut").textContent=await textAdmin("/api/v1/admin/runtime/profiles/heap?debug=1&gc=true");}catch(e){$("overviewOut").textContent=`${t("failed")}: ${e.message}`; toast(`${t("failed")}: ${e.message}`);}}; $("downloadHeapProfile").onclick=()=>downloadAdmin("/api/v1/admin/runtime/profiles/heap?debug=1&gc=true&download=true","heap.txt").catch(e=>toast(`${t("failed")}: ${e.message}`)); $("serviceSupportBundle").onclick=async()=>{$("overviewOut").textContent=pretty(await api("/api/v1/admin/support-bundle"));}; $("serviceSupportBundleDownload").onclick=()=>downloadAdmin("/api/v1/admin/support-bundle?download=true","maclaw-support-bundle.json").catch(e=>toast(`${t("failed")}: ${e.message}`)); $("loadJobs").onclick=async()=>{const q=new URLSearchParams({limit:"50"}); if($("jobStatus").value) q.set("status",$("jobStatus").value); if($("jobKind").value) q.set("kind",$("jobKind").value); const ids=tenantUserValue("jobTenant","jobUser"); if(ids.tenant) q.set("tenant_id",ids.tenant); if(ids.user) q.set("user_id",ids.user); const out=await api(`/api/v1/admin/jobs?${q}`); $("jobsTable").innerHTML=jobTable(out.items||[]); $("overviewOut").textContent=pretty(out); bindJobActions(); applyOwnerGuards();}; }
function jobTable(items){ return table(items,["id","kind","status","tenant_id","user_id","created_at"],x=>`<button class="warn" data-job-cancel="${esc(x.id)}" ${x.status==='pending'||x.status==='running'?"":"disabled"}>${t("cancel")}</button>`); }
function bindJobActions(){ document.querySelectorAll("[data-job-cancel]").forEach(b=>b.onclick=async()=>{if(b.disabled) return; if(await confirmDanger(t("cancelJobConfirm"))){const out=await api(`/api/v1/admin/jobs/${encodeURIComponent(b.dataset.jobCancel)}/cancel`,{method:"POST"}); $("overviewOut").textContent=pretty(out); $("loadJobs")?.click();}}); }
async function sandbox(){ setTitle(t("sandbox"), t("sandboxHint")); const [st,cfg,reports,events,profiles]=await Promise.all([api("/api/v1/admin/sandbox/status"),api("/api/v1/admin/sandbox/config"),api("/api/v1/admin/sandbox/reports").catch(()=>({items:[]})),api("/api/v1/admin/sandbox/events?limit=20").catch(()=>({items:[]})),api("/api/v1/admin/sandbox/profiles").catch(()=>({items:[]}))]); $("content").innerHTML=`<div class="split"><div class="card stack"><h2>${t("status")}</h2><div class="grid"><div class="metric"><span>${t("mode")}</span><b>${esc(st.mode)}</b><span>${esc(st.mode_source||"")}</span></div><div class="metric"><span>${t("effectiveBackend")}</span><b>${esc(st.effective_backend)}</b><span>${esc(st.fallback_reason||"")}</span></div><div class="metric"><span>${t("strict")}</span><b>${esc(st.strict)}</b><span>${esc(st.strict_source||"")}</span></div></div><label>${t("mode")}</label><select id="sandboxMode">${localizedOptions(["auto","landlock","bwrap","nsjail","none"],"mode")}</select><label>${t("strict")}</label><select id="sandboxStrict"><option value="">${t("defaultOption")}</option>${localizedOptions(["true","false"],"strict")}</select><label>${t("profile")}</label><input id="sandboxProfile" placeholder="${t("defaultOption")}" list="sandboxProfileList"><datalist id="sandboxProfileList">${(profiles.items||[]).map(p=>`<option value="${esc(p.name)}"></option>`).join("")}</datalist><input id="sandboxReason" placeholder="${t("reason")}"><div class="row"><button id="saveSandbox">${t("save")}</button><button class="secondary" id="detectSandbox">${t("detect")}</button><button class="secondary" id="smokeSandbox">${t("smoke")}</button><button class="warn" id="diagnoseSandbox">${t("diagnose")}</button><button class="secondary" id="rollbackSandbox">${t("rollback")}</button><button class="secondary" id="installSandbox">${t("installPlan")}</button><button class="secondary" id="sandboxSupportBundle">${t("supportBundle")}</button><button class="secondary" id="sandboxSupportBundleDownload">${t("download")}</button><button class="danger" id="installSandboxRun">${t("installRun")}</button></div><h2>${t("backends")}</h2>${table(st.backends||[],["name","available","smoke_status","reason","version"],null)}<h2>${t("capabilities")}</h2>${table(st.capabilities||[],["name","status","detail","raw"],null)}<h2>${t("profiles")}</h2>${sandboxProfilesTable(profiles.items||[])}${sandboxProfileEditor()}</div><div class="card"><h2>${t("report")}</h2>${sandboxReportsTable(reports.items||[])}<h2>${t("events")}</h2><div class="row"><select id="sandboxEventStatus"><option value="">${t("allOption")}</option>${localizedOptions(["pass","warn","fail"])}</select><input id="sandboxEventBackend" placeholder="${t("eventBackend")}"><button class="secondary" id="loadSandboxEvents">${t("refresh")}</button></div><div id="sandboxEventsTable">${sandboxEventsTable(events.items||[])}</div><div id="sandboxOut" class="code">${esc(pretty({config:cfg,events,profiles}))}</div></div></div>`; $("sandboxMode").value=cfg.mode?.value||cfg.mode||"auto"; $("sandboxStrict").value=cfg.strict?.value===undefined?"":String(cfg.strict.value); $("sandboxReason").value=cfg.reason||""; $("sandboxProfile").value="default"; bindSandboxReportActions(); bindSandboxEventActions(events.items||[]); bindSandboxProfileActions(profiles.items||[]); $("saveSandbox").onclick=async()=>{const mode=$("sandboxMode").value; const strict=$("sandboxStrict").value; const body={mode,reason:$("sandboxReason").value}; if(mode==="none"){if(!await confirmPhrase(t("sandboxNonePrompt"),"DISABLE SANDBOX")) return; body.confirm_unsafe=true;} if(strict!=="") body.strict=strict==="true"; $("sandboxOut").innerHTML=`<pre>${esc(pretty(await api("/api/v1/admin/sandbox/switch",{method:"POST",body:JSON.stringify(body)})))}</pre>`;}; $("detectSandbox").onclick=async()=>{$("sandboxOut").innerHTML=sandboxStatusView(await api("/api/v1/admin/sandbox/detect",{method:"POST",body:"{}"}));}; $("smokeSandbox").onclick=async()=>{$("sandboxOut").innerHTML=sandboxReportView(await api("/api/v1/admin/sandbox/smoke-test",{method:"POST",body:"{}"}));}; $("diagnoseSandbox").onclick=async()=>{$("sandboxOut").innerHTML=sandboxReportView(await api("/api/v1/admin/sandbox/diagnose",{method:"POST",body:JSON.stringify({write_report:true,include_mcp_stdio_test:true,profile:$("sandboxProfile").value.trim()||"default"})}));}; $("rollbackSandbox").onclick=async()=>{if(await confirmDanger(t("sandboxRollbackConfirm"))){$("sandboxOut").innerHTML=`<pre>${esc(pretty(await api("/api/v1/admin/sandbox/rollback",{method:"POST",body:JSON.stringify({reason:$("sandboxReason").value})})))}</pre>`; render();}}; $("installSandbox").onclick=async()=>{$("sandboxOut").innerHTML=sandboxInstallView(await api(`/api/v1/admin/sandbox/install-plan?backend=${encodeURIComponent($("sandboxMode").value)}`));}; $("sandboxSupportBundle").onclick=async()=>{const bundle=await api("/api/v1/admin/sandbox/support-bundle"); $("sandboxOut").innerHTML=sandboxSupportBundleView(bundle); bindRiskEventActions(bundle.security_risks?.recent||[]); bindSandboxReportActions(); applyOwnerGuards();}; $("sandboxSupportBundleDownload").onclick=()=>downloadAdmin("/api/v1/admin/sandbox/support-bundle?download=true","maclaw-sandbox-support-bundle.json").catch(e=>toast(`${t("failed")}: ${e.message}`)); $("installSandboxRun").onclick=async()=>{if(!await confirmPhrase(t("installRunPrompt"),"INSTALL SANDBOX")) return; try{$("sandboxOut").innerHTML=sandboxInstallView(await api("/api/v1/admin/sandbox/install",{method:"POST",body:JSON.stringify({backend:$("sandboxMode").value,mode:"run",confirm:true})}));}catch(e){$("sandboxOut").innerHTML=`<pre>${esc(t("failed"))}: ${esc(e.message)}\n${esc(pretty(e.body||{}))}</pre>`;}}; }
function sandboxReportsTable(items){ if(!items.length) return emptyState(t("noReports")); return table(items,["report_id","status","effective_backend","profile","generated_at"],x=>`<button class="secondary" data-sandbox-report="${esc(x.report_id)}">${t("view")}</button> <button class="danger" data-sandbox-report-delete="${esc(x.report_id)}">${t("delete")}</button>`); }
function bindSandboxReportActions(){ document.querySelectorAll("[data-sandbox-report]").forEach(b=>b.onclick=async()=>{$("sandboxOut").innerHTML=sandboxReportView(await api(`/api/v1/admin/sandbox/reports/${encodeURIComponent(b.dataset.sandboxReport)}`));}); document.querySelectorAll("[data-sandbox-report-delete]").forEach(b=>b.onclick=async()=>{if(await confirmDanger(t("deleteSandboxReportConfirm"))){$("sandboxOut").innerHTML=`<pre>${esc(pretty(await api(`/api/v1/admin/sandbox/reports/${encodeURIComponent(b.dataset.sandboxReportDelete)}?confirm=true`,{method:"DELETE"})))}</pre>`; render();}}); }
function riskCountChips(counts,type="kind"){ const entries=Object.entries(counts||{}).filter(([,v])=>v>0).sort((a,b)=>b[1]-a[1]); if(!entries.length) return emptyState(t("empty")); const attr=type==="severity"?"data-risk-severity":"data-risk-kind"; return `<div class="row riskCountChips">${entries.map(([k,v])=>`<button class="secondary" ${attr}="${esc(k)}">${esc(type==="severity"?cellText("severity",k):k)} ${esc(v)}</button>`).join("")}</div>`; }
function riskKindOptions(counts){ return Object.keys(counts||{}).sort().map(k=>`<option value="${esc(k)}"></option>`).join(""); }
function goToRiskOps(filter){ state.pendingRiskFilter=filter; setSection("ops"); render(); }
function bindRiskCountChips(){ document.querySelectorAll("[data-risk-kind]").forEach(btn=>btn.onclick=()=>{ if(!$("riskKind")) return goToRiskOps({kind:btn.dataset.riskKind}); $("riskKind").value=btn.dataset.riskKind; loadRiskEventsFromFilters(); }); document.querySelectorAll("[data-risk-severity]").forEach(btn=>btn.onclick=()=>{ if(!$("riskSeverity")) return goToRiskOps({severity:btn.dataset.riskSeverity}); $("riskSeverity").value=btn.dataset.riskSeverity; loadRiskEventsFromFilters(); }); }
function riskEventsTable(items){ if(!items.length) return emptyState(t("empty")); return table(items,["severity","kind","summary","created_at"],x=>`<button class="secondary" data-risk-event="${esc(x.id)}">${t("view")}</button>`); }
function riskDetailTarget(){ return $("opsOut")||$("overviewOut")||$("sandboxOut"); }
function bindRiskEventActions(items){ const byId=new Map((items||[]).map(x=>[x.id,x])); document.querySelectorAll("[data-risk-event]").forEach(btn=>btn.onclick=()=>{ const out=riskDetailTarget(); if(out) out.textContent=pretty(byId.get(btn.dataset.riskEvent)||{}); }); }
function validRiskTimeRange(){ const since=$("riskSince")?.value; const until=$("riskUntil")?.value; if(since&&until&&new Date(since)>new Date(until)){ toast(t("riskTimeRangeInvalid")); return false; } return true; }
function validRiskLimit(){ const el=$("riskLimit"); if(!el) return true; const n=Number(el.value||50); if(!Number.isInteger(n)||n<1||n>500){ toast(t("invalidRiskLimit")); return false; } return true; }
function toLocalDateTimeInput(d){ const pad=n=>String(n).padStart(2,"0"); return `${d.getFullYear()}-${pad(d.getMonth()+1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`; }
function setRiskTimePreset(hours){ if(!$("riskSince")||!$("riskUntil")) return; if(!hours){ $("riskSince").value=""; $("riskUntil").value=""; loadRiskEventsFromFilters(); return; } const until=new Date(); const since=new Date(until.getTime()-hours*60*60*1000); $("riskSince").value=toLocalDateTimeInput(since); $("riskUntil").value=toLocalDateTimeInput(until); loadRiskEventsFromFilters(); }
async function loadRiskEventsFromFilters(){ if(!validRiskTimeRange()||!validRiskLimit()) return; const q=new URLSearchParams(); if($("riskSeverity")?.value) q.set("severity",$("riskSeverity").value); if($("riskKind")?.value) q.set("kind",$("riskKind").value.trim()); if($("riskSince")?.value) q.set("since",new Date($("riskSince").value).toISOString()); if($("riskUntil")?.value) q.set("until",new Date($("riskUntil").value).toISOString()); q.set("limit",$("riskLimit")?.value||"50"); const risks=await api(`/api/v1/admin/security/risk-events?${q}`); $("riskSeverityCounts").innerHTML=riskCountChips(risks.counts,"severity"); $("riskKindCounts").innerHTML=riskCountChips(risks.kind_counts); if($("riskKindOptions")) $("riskKindOptions").innerHTML=riskKindOptions(risks.kind_counts); bindRiskCountChips(); if($("riskFilterStatus")) $("riskFilterStatus").innerHTML=riskFilterSummary(risks); $("riskEventsList").innerHTML=riskEventsTable(risks.items||[]); bindRiskEventActions(risks.items||[]); $("opsOut").textContent=pretty(risks); }
function clearRiskFilters(){ ["riskSeverity","riskKind","riskSince","riskUntil"].forEach(id=>{ if($(id)) $(id).value=""; }); if($("riskLimit")) $("riskLimit").value="50"; loadRiskEventsFromFilters(); }
function riskFilterSummary(risks){ const f=risks.filters||{}; const parts=[]; const severity=f.severity||$("riskSeverity")?.value; const kind=f.kind||$("riskKind")?.value?.trim(); const since=f.since||$("riskSince")?.value; const until=f.until||$("riskUntil")?.value; const limit=f.limit||$("riskLimit")?.value; if(severity) parts.push(`severity=${severity}`); if(kind) parts.push(`kind=${kind}`); if(since) parts.push(`since=${since}`); if(until) parts.push(`until=${until}`); if(limit) parts.push(`limit=${limit}`); return `<p class="muted">${t("total")} ${esc(risks.total??(risks.items||[]).length)}${risks.generated_at?" · "+t("generatedAt")+" "+esc(risks.generated_at):""}${parts.length?" · "+esc(parts.join(" · ")):" · "+t("allRisks")}</p>`; }
function applyPendingRiskFilter(){ const f=state.pendingRiskFilter; if(!f) return false; state.pendingRiskFilter=null; if(f.severity&&$("riskSeverity")) $("riskSeverity").value=f.severity; if(f.kind&&$("riskKind")) $("riskKind").value=f.kind; loadRiskEventsFromFilters(); return true; }
function sandboxEventsTable(items){ if(!items.length) return emptyState(t("empty")); return table(items,["action","resource_type","resource_id","created_at"],x=>`<button class="secondary" data-sandbox-event="${esc(x.id)}">${t("view")}</button>`); }
function sandboxProfilesTable(items){ if(!items.length) return emptyState(t("empty")); return table(items,["name","backend","network","updated_at"],x=>`<button class="secondary" data-sandbox-profile="${esc(x.name)}">${t("view")}</button> <button class="danger" data-sandbox-profile-delete="${esc(x.name)}">${t("delete")}</button>`); }
function sandboxProfileEditor(){ return `<div class="panel stack"><h2>${t("editProfile")}</h2><div class="form-grid"><input id="sandboxProfileName" placeholder="${t("profileName")}"><select id="sandboxProfileBackend">${localizedOptions(["bwrap","landlock","nsjail"],"backend")}</select><select id="sandboxProfileNetwork">${localizedOptions(["default","disabled","host"],"network")}</select></div><textarea id="sandboxProfileJSON" placeholder="${t("profileJSON")}">{
  "readonly_paths": [],
  "writable_paths": [],
  "env_allowlist": []
}</textarea><div class="row"><button class="secondary" id="validateSandboxProfile">${t("validate")}</button><button id="saveSandboxProfile">${t("save")}</button></div></div>`; }
function sandboxStatusView(st){ return `<h2>${esc(st.effective_backend||st.mode)}</h2><p class="muted">${esc(st.fallback_reason||"")}</p>${table(st.backends||[],["name","available","smoke_status","reason","version"],null)}${table(st.capabilities||[],["name","status","detail","raw"],null)}<pre>${esc(pretty(st))}</pre>`; }
function sandboxReportView(r){ return `<h2 class="${r.status==='fail'?'status-bad':r.status==='warn'?'status-warn':'status-ok'}">${esc(r.status)} · ${esc(r.effective_backend||r.mode)}</h2><p>${esc(r.summary||"")}</p>${table(r.checks||[],["id","title","status","severity","expected","actual"],null)}${(r.warnings||[]).length?`<h2>${t("warnings")}</h2><pre>${esc((r.warnings||[]).join("\n"))}</pre>`:""}${(r.recommendations||[]).length?`<h2>${t("recommendations")}</h2><pre>${esc((r.recommendations||[]).join("\n"))}</pre>`:""}<pre>${esc(pretty(r))}</pre>`; }
function sandboxInstallView(p){ return `<h2>${esc(p.backend)} · ${esc(p.platform)}</h2><p class="muted">${t("requiresPrivilege")}: ${esc(cellText("sensitive",p.requires_privilege))} · ${t("willExecute")}: ${esc(cellText("status",p.will_execute))}</p><pre>${esc((p.commands||[]).join("\n"))}</pre>${(p.notes||[]).length?`<h2>${t("notes")}</h2><pre>${esc((p.notes||[]).join("\n"))}</pre>`:""}`; }
function sandboxSupportBundleView(b){ const risks=b.security_risks||{}; const redactions=(b.redactions||[]).map(x=>`<code>${esc(x)}</code>`).join(" ")||t("empty"); const dataRoot=`${esc(b.data_root_name||"")}${b.data_root_redacted?" · "+t("redactedState"):""}`; return `<h2>${t("supportBundle")}</h2><p class="muted">${esc(b.generated_at||"")} · ${t("report")} ${esc(b.report_count||0)} · ${t("events")} ${esc(b.event_count||0)} · ${t("profiles")} ${esc(b.profile_count||0)}</p><h2>${t("dataRoot")}</h2><p class="muted">${dataRoot}</p><h2>${t("redactions")}</h2><p class="muted">${redactions}</p><h2>${t("recentErrors")}</h2>${recentLogTable(b.recent_log_errors||[])}<h2>${t("securityRisks")}</h2>${riskFilterSummary(risks)}<h3>${t("bySeverity")}</h3>${riskCountChips(risks.counts,"severity")}<h3>${t("byKind")}</h3>${riskCountChips(risks.kind_counts)}${riskEventsTable(risks.recent||[])}<h2>${t("report")}</h2>${sandboxReportsTable(b.reports||[])}<pre>${esc(pretty(b))}</pre>`; }
async function logs(){ setTitle(t("logs"), t("logsHint")); const [src,recent]=await Promise.all([api("/api/v1/admin/logs/sources"),api("/api/v1/admin/logs/errors/recent?limit=20").catch(e=>({error:e.message,items:[]}))]); $("content").innerHTML=`<div class="card"><h2>${t("sources")}</h2>${table(src.items||[],["id","name","exists","size_bytes","modified_at"],x=>`<button class="secondary" data-log-source="${esc(x.id)}">${t("open")}</button>`)}</div><div class="card stack"><h2>${t("recentErrors")}</h2><label class="row"><input id="logIncludeWarn" type="checkbox" class="w-auto"> ${t("includeWarnings")}</label><button class="secondary" id="loadRecentErrors">${t("refresh")}</button><div id="recentErrors">${recentLogTable(recent.items||[])}</div></div><div class="card stack"><div class="row"><select id="logSource">${(src.items||[]).map(s=>`<option value="${esc(s.id)}">${esc(s.name||s.id)}</option>`).join("")}</select><select id="logLevel"><option value="">${t("allOption")}</option>${localizedOptions(["error","warn","info"],"level")}</select><input id="logQuery" placeholder="${t("search")}"><input id="logTail" type="number" value="200" class="w-narrow"><button id="loadLog">${t("refresh")}</button><button class="secondary" id="searchLogs">${t("searchAllLogs")}</button><button class="secondary" id="downloadLog">${t("download")}</button><button class="warn" id="rotateLog">${t("rotateLog")}</button></div><div id="logMeta" class="muted"></div><div id="logLines"></div><pre id="logOut" class="code hidden"></pre></div>`; document.querySelectorAll("[data-log-source]").forEach(b=>b.onclick=()=>{$("logSource").value=b.dataset.logSource; $("loadLog").click();}); $("loadRecentErrors").onclick=async()=>{const q=new URLSearchParams({limit:"20",include_warn:String($("logIncludeWarn").checked)}); const out=await api(`/api/v1/admin/logs/errors/recent?${q}`); $("recentErrors").innerHTML=recentLogTable(out.items||[]); $("logOut").textContent=pretty(out);}; $("rotateLog").onclick=async()=>{const id=$("logSource").value; if(!await confirmDanger(t("rotateLogConfirm"))) return; const out=await api(`/api/v1/admin/logs/${encodeURIComponent(id)}/rotate?confirm=true`,{method:"POST"}); $("logOut").textContent=pretty(out); await logs();}; $("downloadLog").onclick=()=>{let tail; try{tail=numberInRange("logTail",200,1,2000);}catch{return;} const id=$("logSource").value; const q=new URLSearchParams({tail:String(tail)}); if($("logLevel").value) q.set("level",$("logLevel").value); if($("logQuery").value) q.set("q",$("logQuery").value); downloadAdmin(`/api/v1/admin/logs/${encodeURIComponent(id)}/download?${q}`,`${id}.log`).catch(e=>toast(`${t("failed")}: ${e.message}`));}; $("searchLogs").onclick=async()=>{let tail; try{tail=numberInRange("logTail",200,1,2000);}catch{return;} const body={q:$("logQuery").value,level:$("logLevel").value,tail,limit:100}; const out=await api("/api/v1/admin/logs/search",{method:"POST",body:JSON.stringify(body)}); $("logMeta").textContent=`${out.items?.length||0} ${t("lines")}`; $("logLines").innerHTML=recentLogTable(out.items||[]); $("logOut").textContent=pretty(out);}; $("loadLog").onclick=async()=>{const id=$("logSource").value; let tail; try{tail=numberInRange("logTail",200,1,2000);}catch{return;} const q=new URLSearchParams({tail:String(tail)}); if($("logLevel").value) q.set("level",$("logLevel").value); if($("logQuery").value) q.set("q",$("logQuery").value); const out=await api(`/api/v1/admin/logs/${encodeURIComponent(id)}?${q}`); $("logMeta").textContent=`${out.source?.path||id} ? ${out.lines?.length||0} ${t("lines")}${out.truncated?" ? "+t("truncated"):""}`; $("logLines").innerHTML=logLineTable(out.lines||[]); $("logOut").textContent=pretty(out);}; $("loadLog").click(); }
function recentLogTable(items){ if(!items.length) return emptyState(t("empty")); return `<div class="table-wrap log-table-wrap" tabindex="0" role="region" aria-label="${t("recentErrors")}"><table class="log-table"><thead><tr><th scope="col">${t("source")}</th><th scope="col">#</th><th scope="col">${t("level")}</th><th scope="col">${t("text")}</th></tr></thead><tbody>${items.map(x=>{const line=x.line||x; return `<tr><td class="cell-code">${esc(x.source_name||x.source_id||"")}</td><td class="cell-code">${esc(line.number||"")}</td><td>${cellValue("status",line.level||"")}</td><td><code>${esc(line.text||x.text||"")}</code></td></tr>`;}).join("")}</tbody></table></div>`; }
function logLineTable(lines){ if(!lines.length) return emptyState(t("empty")); return `<div class="table-wrap log-table-wrap" tabindex="0" role="region" aria-label="${t("logs")}"><table class="log-table"><thead><tr><th scope="col">#</th><th scope="col">${t("level")}</th><th scope="col">${t("text")}</th></tr></thead><tbody>${lines.map(x=>`<tr><td class="cell-code">${esc(x.number)}</td><td>${cellValue("status",x.level)}</td><td><code>${esc(x.text)}</code></td></tr>`).join("")}</tbody></table></div>`; }
async function config(){
  setTitle(t("config"), t("configHint"));
  const [eff,draftResp,schemaResp,envResp,diffResp]=await Promise.all([
    api("/api/v1/admin/service-config/effective"),
    api("/api/v1/admin/service-config/draft"),
    api("/api/v1/admin/service-config/schema"),
    api("/api/v1/admin/service-config/environment"),
    api("/api/v1/admin/service-config/diff")
  ]);
  const draft=draftResp.draft||{values:{}};
  const schema=schemaResp.items||[];
  const tabs=["draft","effective","environment","diff","secrets","raw"];
  const saved=localStorage.getItem("maclaw.admin.configTab")||"draft";
  const active=tabs.includes(saved)?saved:"draft";
  const tabLabel=(id)=>t(`cfgTab${id[0].toUpperCase()+id.slice(1)}`);
  const tabBtn=(id)=>`<button type="button" role="tab" class="${active===id?"active":""}" aria-selected="${active===id}" data-config-tab="${id}">${tabLabel(id)}</button>`;
  const pane=(id,html)=>`<section class="config-tab-panel ${active===id?"":"hidden"}" data-config-pane="${id}" role="tabpanel">${html}</section>`;
  const effectiveRows=Object.entries(eff.fields||{}).map(([key,v])=>({key,value:JSON.stringify(v.value),source:v.source,restart_required:v.restart_required,mutable_at_runtime:v.mutable_at_runtime}));
  const diffTable=(items)=>table(items||[],["key","env_key","configured","changed","current","desired","restart_required"],null);
  $("content").innerHTML=`<div class="card stack config-card"><div class="segmented config-tabs" role="tablist" aria-label="${t("config")}">${tabs.map(tabBtn).join("")}</div>${pane("draft",`<div class="stack"><h2>${t("draft")}</h2><div class="panel"><h2>${t("howToApplyConfig")}</h2><p class="helper-text">${t("configApplyHint")}</p></div><div id="cfgFields" class="stack">${configFields(schema,draft.values||{},eff.fields||{})}</div><label>${t("reason")}</label><input id="cfgReason" value="${esc(draft.reason||"")}" placeholder="${t("reason")}"><div class="row"><button id="saveCfg">${t("save")}</button><button class="secondary" id="validateCfg">${t("validate")}</button><button class="secondary" id="exportCfg">${t("exportPlan")}</button><button class="danger" id="clearCfgDraft">${t("clearDraft")}</button></div><pre id="cfgOut" class="code">${esc(pretty(draftResp))}</pre></div>`)}${pane("effective",`<div class="panel"><h2>${t("effective")}</h2><p class="helper-text">${t("configEffectiveHint")}</p></div>${table(effectiveRows,["key","value","source","restart_required","mutable_at_runtime"],null)}`)}${pane("environment",`<h2>${t("environment")}</h2>${table(envResp.items||[],["key","env_key","configured","value","default","source"],null)}`)}${pane("diff",`<div class="panel"><h2>${t("draftDiff")}</h2><p class="helper-text">${t("configDiffHint")}</p></div><div id="cfgDiffTable">${diffTable(diffResp.items)}</div>`)}${pane("secrets",`<div class="panel stack"><h2>${t("revealAdminSecret")}</h2><p class="helper-text">${t("revealAdminSecretHint")}</p><div class="secret-field-row"><input id="revealSecretPassword" type="password" autocomplete="current-password" placeholder="${t("adminPassword")}"><button id="revealAdminSecret" class="warn">${t("revealAdminSecret")}</button></div><div id="adminSecretResult" class="code hidden"></div></div>`)}${pane("raw",`<h2>${t("raw")}</h2><pre id="cfgRawOut" class="code">${esc(pretty({draft:draftResp, schema:schemaResp, environment:envResp, diff:diffResp}))}</pre>`)}</div>`;
  const switchConfigTab=(id)=>{localStorage.setItem("maclaw.admin.configTab",id); document.querySelectorAll("[data-config-tab]").forEach(b=>{const on=b.dataset.configTab===id; b.classList.toggle("active",on); b.setAttribute("aria-selected",String(on));}); document.querySelectorAll("[data-config-pane]").forEach(p=>p.classList.toggle("hidden",p.dataset.configPane!==id));};
  document.querySelectorAll("[data-config-tab]").forEach(btn=>btn.onclick=()=>switchConfigTab(btn.dataset.configTab));
  const values=()=>buildConfigValues(schema);
  const out=()=>$("cfgOut")||$("cfgRawOut");
  if($("saveCfg")) $("saveCfg").onclick=async()=>{const btn=$("saveCfg"); try{btn.disabled=true; const result=await api("/api/v1/admin/service-config/draft",{method:"PATCH",body:JSON.stringify({values:values(),reason:$("cfgReason").value})}); out().innerHTML=configValidationView(result); const diff=await api("/api/v1/admin/service-config/diff"); if($("cfgDiffTable")) $("cfgDiffTable").innerHTML=diffTable(diff.items); switchConfigTab("diff"); toast(t("savedDraft"));}catch(e){toast(`${t("failed")}: ${e.message}`);}finally{btn.disabled=false;}};
  if($("validateCfg")) $("validateCfg").onclick=async()=>{try{out().innerHTML=configValidationView(await api("/api/v1/admin/service-config/validate",{method:"POST",body:JSON.stringify({values:values()})}));}catch(e){toast(`${t("failed")}: ${e.message}`);}};
  if($("exportCfg")) $("exportCfg").onclick=async()=>{try{out().innerHTML=configExportView(await api("/api/v1/admin/service-config/export-plan",{method:"POST",body:JSON.stringify({values:values()})}));}catch(e){toast(`${t("failed")}: ${e.message}`);}};
  if($("clearCfgDraft")) $("clearCfgDraft").onclick=async()=>{if(!await confirmDanger(t("clearDraftConfirm"))) return; try{out().textContent=pretty(await api("/api/v1/admin/service-config/draft?confirm=true",{method:"DELETE"})); toast(t("loaded")); render();}catch(e){toast(`${t("failed")}: ${e.message}`);}};
  if($("revealAdminSecret")) $("revealAdminSecret").onclick=async()=>{try{const result=await api("/api/v1/admin/auth/reveal-admin-secret",{method:"POST",body:JSON.stringify({password:$("revealSecretPassword").value})}); const box=$("adminSecretResult"); box.classList.remove("hidden"); box.innerHTML=`<div class="row"><code>${esc(result.secret)}</code><button class="secondary" id="copyAdminSecret">${t("copy")}</button></div>`; $("copyAdminSecret").onclick=async()=>{await navigator.clipboard.writeText(result.secret); toast(t("copied"));};}catch(e){toast(`${t("failed")}: ${e.message}`);}};
}
function configFields(schema,draft,effective){ return schema.map(f=>{const writable=!!f.writable; const hasDraft=Object.prototype.hasOwnProperty.call(draft,f.key); const effectiveValue=effective[f.key]?.value; const current=hasDraft?draft[f.key]:(f.sensitive?"":(effectiveValue ?? f.default ?? "")); const checked=hasDraft?"checked":""; const disabled=writable?"":"disabled"; let input=""; if(f.type==="bool"){ input=`<label class="row"><input id="cfg_${esc(f.key)}" type="checkbox" ${current===true?"checked":""} ${disabled} class="w-auto"> ${esc(f.env_key||f.key)}</label>`; } else if(f.type==="enum"){ input=`<select id="cfg_${esc(f.key)}" ${disabled}>${(f.allowed_values||[]).map(v=>`<option value="${esc(v)}" ${String(current)===v?"selected":""}>${esc(v)}</option>`).join("")}</select>`; } else { input=`<input id="cfg_${esc(f.key)}" value="${esc(current)}" ${disabled} ${f.sensitive?'type="password"':''}>`; } return `<div class="panel"><div class="row"><label class="row"><input id="cfgUse_${esc(f.key)}" type="checkbox" ${checked} ${writable?"":"disabled"} class="w-auto"> <strong>${esc(f.key)}</strong></label><span class="badge">${esc(f.type)}</span>${f.restart_required?`<span class="badge">${t("restart")}</span>`:""}${f.mutable_at_runtime?`<span class="badge">${t("runtime")}</span>`:""}${f.sensitive?`<span class="badge">${t("sensitive")}</span>`:""}</div><p class="muted">${esc(f.description||"")}</p>${input}</div>`; }).join(""); }
function buildConfigValues(schema){ const out={}; for(const f of schema){ const use=$(`cfgUse_${f.key}`); if(!use||!use.checked) continue; const el=$(`cfg_${f.key}`); if(!el) continue; out[f.key]=f.type==="bool"?!!el.checked:el.value; } return out; }
function configValidationView(x){ const validation=x.validation||x; return `<h2 class="${validation.valid?'status-ok':'status-bad'}">${validation.valid?t('valid'):t('invalid')}</h2>${(validation.errors||[]).length?`<h2>${t("errors")}</h2><pre>${esc((validation.errors||[]).join("\n"))}</pre>`:""}${(validation.warnings||[]).length?`<h2>${t("warnings")}</h2><pre>${esc((validation.warnings||[]).join("\n"))}</pre>`:""}${table(validation.env_plan||[],["key","env_key","value","sensitive","action"],null)}<pre>${esc(pretty(x))}</pre>`; }
function configExportView(p){ return `<h2>${p.restart_required?t("restartRequired"):t("runtimeOnly")}</h2><p class="muted">${t("manualApply")}: ${esc(p.requires_manual_apply)} · ${t("willExecute")}: ${esc(p.will_execute)}</p><h2>.env</h2><pre>${esc(p.dotenv_content||"")}</pre><h2>systemd</h2><pre>${esc(p.systemd_dropin_content||"")}</pre>${configValidationView(p.validation||{})}`; }
function tenantDisplayNameByID(items){ const names=new Map(); (items||[]).forEach(x=>names.set(String(x.id||""),String(x.name||x.id||""))); return names; }
function userTenantRows(users,tenants){ const names=tenantDisplayNameByID(tenants); return (users||[]).map(x=>{ const tenantID=String(x.tenant_id||""); return {...x,tenant_name:displayWithID(names.get(tenantID)||tenantID,tenantID)}; }); }
function actionKey(...parts){ return parts.map(x=>encodeURIComponent(String(x??""))).join(":"); }
function actionParts(value){ return String(value||"").split(":").map(x=>decodeURIComponent(x)); }
function userSearchColumns(){ return ["tenant_name","name","email","status","delete_protected","id"]; }
function tenantUserColumns(){ return ["name","email","status","delete_protected","id"]; }
function tenantUserSearchRows(rows){ const tenant=String($("userSearchTenant")?.value||""); const q=String($("userSearchQuery")?.value||"").trim().toLowerCase(); return (rows||[]).filter(x=>{ const tenantOK=!tenant||String(x.tenant_id||"")===tenant; const hay=[x.tenant_name,x.tenant_id,x.name,x.email,x.id,x.status].map(v=>String(v||"").toLowerCase()).join(" "); return tenantOK&&(!q||hay.includes(q)); }); }
function renderTenantUserSearch(rows){ const filtered=tenantUserSearchRows(rows); $("tenantUserSearchResults").innerHTML=filtered.length?table(filtered,userSearchColumns(), userActions):emptyState(t("empty")); $("tenantUserSearchCount").textContent=String(filtered.length); bindTenantActions(); applyOwnerGuards(); }
function tenantUsersTitle(tenant,rows){ const name=displayWithID(tenant.name||tenant.id,tenant.id); return `${name} / ${rows.length} ${t("users")}`; }
function showTenantUsersModal(tenant,rows){ const prior=document.activeElement; const overlay=document.createElement("div"); overlay.className="modal-backdrop"; overlay.innerHTML=`<div class="modal tenant-users-modal" role="dialog" aria-modal="true" aria-labelledby="tenantUsersTitle"><div class="modal-head"><h2 id="tenantUsersTitle">${esc(tenantUsersTitle(tenant,rows))}</h2><button type="button" class="secondary w-auto" id="tenantUsersClose" aria-label="${esc(t("cancel"))}">&times;</button></div>${rows.length?table(rows,tenantUserColumns(), userActions):emptyState(t("empty"))}</div>`; document.body.appendChild(overlay); const close=()=>{overlay.remove(); prior?.focus?.({preventScroll:true});}; overlay.querySelector("#tenantUsersClose").onclick=close; overlay.onkeydown=e=>{if(e.key==="Escape") close();}; overlay.addEventListener("click",e=>{if(e.target===overlay) close();}); bindTenantActions(); applyOwnerGuards(); overlay.querySelector("#tenantUsersClose")?.focus({preventScroll:true}); }
function bindTenantPageActions(tenants,users){ document.querySelectorAll("[data-tenant-users]").forEach(b=>b.onclick=()=>{ const id=b.dataset.tenantUsers; const tenant=(tenants||[]).find(x=>String(x.id)===String(id))||{id}; showTenantUsersModal(tenant,(users||[]).filter(x=>String(x.tenant_id||"")===String(id))); }); const refresh=()=>renderTenantUserSearch(users); $("userSearchTenant").onchange=refresh; $("userSearchQuery").oninput=refresh; $("clearUserSearch").onclick=()=>{$("userSearchTenant").value=""; $("userSearchQuery").value=""; refresh();}; refresh(); }
async function tenants(){ setTitle(t("tenants"), t("tenantsHint")); const [ten,user]=await Promise.all([api("/api/v1/admin/tenants?limit=100"),api("/api/v1/admin/users?limit=100").catch(e=>({error:e.message,items:[]}))]); const tenantItems=ten.items||[]; const userRows=userTenantRows(user.items||[],tenantItems); const tenantOptions=tenantItems.map(x=>`<option value="${esc(x.id)}">${esc(x.name||x.id)}</option>`).join(""); const searchTenantOptions=`<option value="">${t("allOption")}</option>${tenantOptions}`; $("content").innerHTML=`<div class="tenant-admin-layout"><div class="tenant-forms"><div class="card stack"><h2>${t("createTenant")}</h2><div class="form-grid"><input id="tenantName" placeholder="${t("name")}"><label class="row"><input id="tenantProtected" type="checkbox" class="w-auto"> ${t("deleteProtected")}</label><input id="tenantProtectReason" placeholder="${t("protectionReason")}"></div><button id="createTenant">${t("create")}</button></div><div class="card stack"><h2>${t("createUser")}</h2><div class="form-grid"><select id="userTenant">${tenantOptions}</select><input id="userName" placeholder="${t("name")}"><input id="userEmail" placeholder="${t("email")}"><label class="row"><input id="userProtected" type="checkbox" class="w-auto"> ${t("deleteProtected")}</label><input id="userProtectReason" placeholder="${t("protectionReason")}"></div><button id="createTenantUser">${t("create")}</button></div></div><div class="tenant-main"><div class="card"><h2>${t("tenantList")}</h2>${table(tenantItems,["id","name","status","delete_protected","created_at"], tenantActions)}</div><div class="card stack tenant-user-search"><div class="section-head"><div><h2>${t("users")} ${t("search")}</h2><p class="muted"><span id="tenantUserSearchCount">0</span> ${t("users")}</p></div></div><div class="row"><select id="userSearchTenant">${searchTenantOptions}</select><input id="userSearchQuery" placeholder="${t("name")} / ${t("email")} / ID"><button type="button" class="secondary" id="clearUserSearch">${t("clear")}</button></div><div id="tenantUserSearchResults"></div></div></div></div>`; bindTenantActions(); bindTenantPageActions(tenantItems,userRows); $("createTenant").onclick=async()=>{await api("/api/v1/admin/tenants",{method:"POST",body:JSON.stringify({name:$("tenantName").value,delete_protected:$("tenantProtected").checked,delete_protection_reason:$("tenantProtectReason").value})}); render();}; $("createTenantUser").onclick=async()=>{const tenant=$("userTenant").value; await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users`,{method:"POST",body:JSON.stringify({name:$("userName").value,email:$("userEmail").value,delete_protected:$("userProtected").checked,delete_protection_reason:$("userProtectReason").value})}); render();}; }
function tenantActions(x){ return `<button class="secondary" data-tenant-users="${esc(x.id)}">${t("view")} ${t("users")}</button> <button class="secondary" data-tenant-summary="${esc(x.id)}">${t("summary")}</button> <button data-tenant-status="${esc(actionKey(x.id,x.status==='active'?'disabled':'active'))}">${x.status==='active'?t("suspend"):t("activate")}</button> <button class="secondary" data-tenant-audit="${esc(x.id)}">${t("audit")}</button> <button class="secondary" data-tenant-check="${esc(x.id)}">${t("deleteCheck")}</button> <button class="danger" data-tenant-delete="${esc(x.id)}">${t("delete")}</button>`; }
function userActions(x){ return `<button data-user-status="${esc(actionKey(x.tenant_id,x.id,x.status==='active'?'disabled':'active'))}">${x.status==='active'?t("suspend"):t("activate")}</button> <button class="secondary" data-user-credentials="${esc(actionKey(x.tenant_id,x.id))}">${t("apiCredentials")}</button> <button class="secondary" data-user-audit="${esc(actionKey(x.tenant_id,x.id))}">${t("audit")}</button> <button class="secondary" data-user-check="${esc(actionKey(x.tenant_id,x.id))}">${t("deleteCheck")}</button> <button class="secondary" data-user-plan="${esc(actionKey(x.tenant_id,x.id))}">${t("retirePlan")}</button> <button class="danger" data-user-delete="${esc(actionKey(x.tenant_id,x.id))}">${t("delete")}</button>`; }
function credentialActions(tenant,userId,x){ return `<button data-credential-status="${esc(actionKey(tenant,userId,x.id,x.status==='active'?'suspended':'active'))}">${x.status==='active'?t("suspend"):t("activate")}</button> <button class="secondary" data-credential-secret="${esc(actionKey(tenant,userId,x.id))}">${t("rotateSecret")}</button> <button class="secondary" data-credential-key="${esc(actionKey(tenant,userId,x.id))}">${t("rotateKey")}</button> <button class="danger" data-credential-revoke="${esc(actionKey(tenant,userId,x.id))}">${t("revoke")}</button>`; }
function credentialModalBody(tenant,userId,creds){ return `<div class="modal-head"><h2 id="credentialModalTitle">${esc(t("apiCredentials"))}</h2><button type="button" class="secondary w-auto" id="credentialClose" aria-label="${esc(t("cancel"))}">&times;</button></div><p class="helper-text">${esc(t("credentialHelp"))}</p><div class="panel stack credential-create-panel"><h2>${t("createCredential")}</h2><div class="form-grid compact-form"><input id="credentialName" placeholder="${t("name")}"><input id="credentialKey" placeholder="${t("apiKeyOptional")}"><input id="credentialSecret" placeholder="${t("apiSecretOptional")}"><input id="credentialExpires" type="datetime-local"></div><button id="createCredential">${t("createCredential")}</button></div><div class="credential-list">${table(creds.items||[],["id","name","status","api_key_hint","expires_at","created_at"], x=>credentialActions(tenant,userId,x))}</div><details class="credential-result"><summary>${t("credentialResult")}</summary><pre id="credentialModalOut" class="code"></pre></details>`; }
async function showCredentials(tenant,userId){ const prior=document.activeElement; const creds=await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(userId)}/credentials?limit=100`); let overlay=$("credentialModalBackdrop"); const fresh=!overlay; if(!overlay){ overlay=document.createElement("div"); overlay.id="credentialModalBackdrop"; overlay.className="modal-backdrop"; document.body.appendChild(overlay); } overlay.innerHTML=`<div class="modal credential-modal" role="dialog" aria-modal="true" aria-labelledby="credentialModalTitle">${credentialModalBody(tenant,userId,creds)}</div>`; const close=()=>{overlay.remove(); prior?.focus?.({preventScroll:true});}; overlay.querySelector("#credentialClose").onclick=close; overlay.onkeydown=e=>{if(e.key==="Escape") close();}; overlay.onclick=e=>{if(e.target===overlay) close();}; $("createCredential").onclick=async()=>{const body={name:$("credentialName").value,api_key:$("credentialKey").value,api_secret:$("credentialSecret").value}; if($("credentialExpires").value) body.expires_at=new Date($("credentialExpires").value).toISOString(); const out=await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(userId)}/credentials`,{method:"POST",body:JSON.stringify(body)}); await showCredentials(tenant,userId); const outBox=$("credentialModalOut"); if(outBox){ outBox.textContent=pretty(out); outBox.closest("details").open=true; } toast(t("loaded"));}; bindCredentialActions(); applyOwnerGuards(); if(fresh) overlay.querySelector("#credentialClose")?.focus({preventScroll:true}); }
function credentialOutput(value){ const out=$("credentialModalOut"); if(out){ out.textContent=pretty(value); out.closest("details")?.setAttribute("open",""); } }
function bindCredentialActions(){ document.querySelectorAll("[data-credential-status]").forEach(b=>b.onclick=async()=>{const [tenant,userId,id,status]=actionParts(b.dataset.credentialStatus); await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(userId)}/credentials/${encodeURIComponent(id)}`,{method:"PATCH",body:JSON.stringify({status})}); await showCredentials(tenant,userId);}); document.querySelectorAll("[data-credential-secret]").forEach(b=>b.onclick=async()=>{const [tenant,userId,id]=actionParts(b.dataset.credentialSecret); credentialOutput(await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(userId)}/credentials/${encodeURIComponent(id)}/rotate-secret`,{method:"POST",body:"{}"}));}); document.querySelectorAll("[data-credential-key]").forEach(b=>b.onclick=async()=>{const [tenant,userId,id]=actionParts(b.dataset.credentialKey); credentialOutput(await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(userId)}/credentials/${encodeURIComponent(id)}/rotate-key`,{method:"POST",body:"{}"}));}); document.querySelectorAll("[data-credential-revoke]").forEach(b=>b.onclick=async()=>{const [tenant,userId,id]=actionParts(b.dataset.credentialRevoke); if(await confirmDanger(t("revokeCredentialConfirm"))){await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(userId)}/credentials/${encodeURIComponent(id)}`,{method:"DELETE"}); await showCredentials(tenant,userId);}}); }
function showAdminResult(title,value){ const prior=document.activeElement; const overlay=document.createElement("div"); overlay.className="modal-backdrop"; overlay.innerHTML=`<div class="modal admin-result-modal" role="dialog" aria-modal="true" aria-labelledby="adminResultTitle"><div class="modal-head"><h2 id="adminResultTitle">${esc(title)}</h2><button type="button" class="secondary w-auto" id="adminResultClose" aria-label="${esc(t("cancel"))}">&times;</button></div><pre class="code">${esc(typeof value==="string"?value:pretty(value))}</pre></div>`; document.body.appendChild(overlay); const close=()=>{overlay.remove(); prior?.focus?.({preventScroll:true});}; overlay.querySelector("#adminResultClose").onclick=close; overlay.onkeydown=e=>{if(e.key==="Escape") close();}; overlay.addEventListener("click",e=>{if(e.target===overlay) close();}); overlay.querySelector("#adminResultClose")?.focus({preventScroll:true}); }
async function showTenantAudit(tenant,user=""){ const q=new URLSearchParams({tenant_id:tenant,limit:"100"}); if(user) q.set("user_id",user); showAdminResult(t("audit"),await api(`/api/v1/admin/audit-events?${q}`)); }
function modalDecision({title=t("confirm"),message="",confirmText=t("confirm"),danger=false,secret=false}={}){return new Promise(resolve=>{const prior=document.activeElement; const overlay=document.createElement("div"); overlay.className="modal-backdrop"; overlay.innerHTML=`<div class="modal" role="dialog" aria-modal="true" aria-labelledby="modalTitle"><h2 id="modalTitle">${esc(title)}</h2><p>${esc(message)}</p>${secret?`<input id="modalSecret" type="password" autocomplete="current-password" aria-label="${esc(t("password"))}">`:""}<div class="row modal-actions"><button type="button" class="secondary" id="modalCancel">${t("cancel")}</button><button type="button" class="${danger?"danger":"primary"}" id="modalOk">${esc(confirmText)}</button></div></div>`; document.body.appendChild(overlay); const close=(ok)=>{const value=secret?overlay.querySelector("#modalSecret")?.value||"":""; overlay.remove(); prior?.focus?.({preventScroll:true}); resolve(ok?{ok:true,value}:{ok:false,value:""});}; overlay.querySelector("#modalCancel").onclick=()=>close(false); overlay.querySelector("#modalOk").onclick=()=>close(true); overlay.onkeydown=e=>{if(e.key==="Escape") close(false); if(e.key==="Enter"&&(secret?document.activeElement?.id==="modalSecret":true)) close(true);}; (secret?overlay.querySelector("#modalSecret"):overlay.querySelector("#modalOk"))?.focus({preventScroll:true});});}
async function confirmDanger(message){return (await modalDecision({title:t("dangerous"),message,confirmText:t("confirm"),danger:true})).ok;}
async function confirmPhrase(message,phrase){const typed=await modalDecision({title:t("dangerous"),message,confirmText:t("confirm"),danger:true,secret:true}); return typed.ok&&typed.value===phrase;}
async function forceDeleteSecret(promptText,confirmText){const first=await modalDecision({title:t("dangerous"),message:promptText,confirmText:t("confirm"),danger:true,secret:true}); if(!first.ok||!first.value) return ""; const second=await modalDecision({title:t("dangerous"),message:confirmText,confirmText:t("delete"),danger:true}); return second.ok?first.value:"";}
function protectedOnly(check){const blockers=check.blockers||[]; return blockers.length>0&&blockers.every(x=>x.kind==="delete_protected");}
function bindTenantActions(){
  document.querySelectorAll("[data-tenant-summary]").forEach(b=>b.onclick=async()=>showAdminResult(t("summary"),await api(`/api/v1/admin/tenants/${encodeURIComponent(b.dataset.tenantSummary)}/summary`)));
  document.querySelectorAll("[data-tenant-status]").forEach(b=>b.onclick=async()=>{const [tenant,status]=actionParts(b.dataset.tenantStatus); const action=status==="disabled"?"pause":"resume"; await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/${action}`,{method:"POST",body:"{}"}); render();});
  document.querySelectorAll("[data-tenant-audit]").forEach(b=>b.onclick=async()=>showTenantAudit(b.dataset.tenantAudit));
  document.querySelectorAll("[data-tenant-check]").forEach(b=>b.onclick=async()=>showAdminResult(t("deleteCheck"),await api(`/api/v1/admin/tenants/${encodeURIComponent(b.dataset.tenantCheck)}/delete-check`)));
  document.querySelectorAll("[data-tenant-delete]").forEach(b=>b.onclick=async()=>{try{const id=b.dataset.tenantDelete; const check=await api(`/api/v1/admin/tenants/${encodeURIComponent(id)}/delete-check`); if(check.can_delete===false){showAdminResult(t("deleteCheck"),check); if(protectedOnly(check)){const secret=await forceDeleteSecret(t("forceDeleteTenantPrompt",{users:check.users||0}),t("forceDeleteTenantConfirm")); if(!secret) return; await api(`/api/v1/admin/tenants/${encodeURIComponent(id)}?confirm=true&force=true`,{method:"DELETE",body:JSON.stringify({password:secret,admin_secret:secret})}); toast(t("loaded")); render(); return;} toast(t("deleteBlocked")); return;} if(await confirmDanger(t("deleteTenantConfirm"))){await api(`/api/v1/admin/tenants/${encodeURIComponent(id)}?confirm=true`,{method:"DELETE"}); toast(t("loaded")); render();}}catch(e){toast(`${t("failed")}: ${e.message}`);}});
  document.querySelectorAll("[data-user-status]").forEach(b=>b.onclick=async()=>{const [tenant,user,status]=actionParts(b.dataset.userStatus); const action=status==="disabled"?"pause":"resume"; await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(user)}/${action}`,{method:"POST",body:"{}"}); render();});
  document.querySelectorAll("[data-user-credentials]").forEach(b=>b.onclick=async()=>{const [tenant,user]=actionParts(b.dataset.userCredentials); await showCredentials(tenant,user);});
  document.querySelectorAll("[data-user-audit]").forEach(b=>b.onclick=async()=>{const [tenant,user]=actionParts(b.dataset.userAudit); await showTenantAudit(tenant,user);});
  document.querySelectorAll("[data-user-check]").forEach(b=>b.onclick=async()=>{const [tenant,user]=actionParts(b.dataset.userCheck); showAdminResult(t("deleteCheck"),await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(user)}/delete-check`));});
  document.querySelectorAll("[data-user-plan]").forEach(b=>b.onclick=async()=>{const [tenant,user]=actionParts(b.dataset.userPlan); showAdminResult(t("retirePlan"),await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(user)}/retire-plan`));});
  document.querySelectorAll("[data-user-delete]").forEach(b=>b.onclick=async()=>{try{const [tenant,user]=actionParts(b.dataset.userDelete); const check=await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(user)}/delete-check`); if(check.can_delete===false){showAdminResult(t("deleteCheck"),check); if(protectedOnly(check)){const secret=await forceDeleteSecret(t("forceDeleteUserPrompt"),t("forceDeleteUserConfirm")); if(!secret) return; await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(user)}?confirm=true&force=true`,{method:"DELETE",body:JSON.stringify({password:secret,admin_secret:secret})}); toast(t("loaded")); render(); return;} toast(t("deleteBlocked")); return;} if(await confirmDanger(t("deleteUserConfirm"))){await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/users/${encodeURIComponent(user)}?confirm=true`,{method:"DELETE"}); toast(t("loaded")); render();}}catch(e){toast(`${t("failed")}: ${e.message}`);}});
}
async function accounts(){ setTitle(t("accounts"), t("accountsHint")); if(!isOwner()){ $("content").innerHTML=`<div class="card stack"><h2>${t("changePassword")}</h2><p class="muted">${t("ownerOnly")}: ${t("users")} / ${t("sessions")}</p><div class="form-grid"><input id="oldPass" type="password" placeholder="${t("oldPassword")}"><input id="newPass" type="password" placeholder="${t("newPassword")}"></div><button id="changePass">${t("save")}</button></div>`; $("changePass").onclick=async()=>{await api("/api/v1/admin/auth/change-password",{method:"POST",body:JSON.stringify({old_password:$("oldPass").value,new_password:$("newPass").value})}); toast(t("loaded"));}; return; } const [users,sessions]=await Promise.all([api("/api/v1/admin/auth/users"),api("/api/v1/admin/auth/sessions")]); $("content").innerHTML=`<div class="card"><h2>${t("users")}</h2>${table(users.items||[],["username","display_name","role","status","locale","last_login_at"], x=>`<button data-admin-toggle="${esc(x.id)}:${x.status==='active'?'suspended':'active'}">${x.status==='active'?t("suspend"):t("activate")}</button>`)}</div><div class="card stack"><h2>${t("createAdmin")}</h2><div class="form-grid"><input id="newAdminUser" placeholder="${t("username")}"><input id="newAdminPass" type="password" placeholder="${t("password")}"><input id="newAdminName" placeholder="${t("displayName")}"><select id="newAdminRole"><option value="operator">${t("operator")}</option><option value="owner">${t("owner")}</option></select><select id="newAdminLocale">${localeOptions()}</select></div><button id="createAdmin">${t("create")}</button></div><div class="card"><h2>${t("sessions")}</h2>${table(sessions.items||[],["username","active","created_at","expires_at","remote_ip"], x=>`<button class="danger" data-session-revoke="${esc(x.id)}">${t("revoke")}</button>`)}</div><div class="card stack"><h2>${t("changePassword")}</h2><div class="form-grid"><input id="oldPass" type="password" placeholder="${t("oldPassword")}"><input id="newPass" type="password" placeholder="${t("newPassword")}"></div><button id="changePass">${t("save")}</button></div>`; document.querySelectorAll("[data-admin-toggle]").forEach(b=>b.onclick=async()=>{const [id,status]=b.dataset.adminToggle.split(":"); await api(`/api/v1/admin/auth/users/${encodeURIComponent(id)}`,{method:"PATCH",body:JSON.stringify({status,confirm_unsafe:true})}); render();}); document.querySelectorAll("[data-session-revoke]").forEach(b=>b.onclick=async()=>{if(await confirmDanger(t("revokeSessionConfirm"))){await api(`/api/v1/admin/auth/sessions/${encodeURIComponent(b.dataset.sessionRevoke)}?confirm=true`,{method:"DELETE"}); render();}}); $("createAdmin").onclick=async()=>{await api("/api/v1/admin/auth/users",{method:"POST",body:JSON.stringify({username:$("newAdminUser").value,password:$("newAdminPass").value,display_name:$("newAdminName").value,role:$("newAdminRole").value,locale:$("newAdminLocale").value})}); render();}; $("changePass").onclick=async()=>{await api("/api/v1/admin/auth/change-password",{method:"POST",body:JSON.stringify({old_password:$("oldPass").value,new_password:$("newPass").value})}); toast(t("loaded"));}; }
async function knowledge(){
  setTitle(t("knowledge"), t("knowledgeHint"));
  const [stats,sources,cross,available,globalCfg,tenantsResp,usersResp,publicResp]=await Promise.all([
    api("/api/v1/admin/knowledge/stats").catch(e=>({error:e.message})),
    api("/api/v1/admin/knowledge/sources").catch(e=>({error:e.message,sources:[]})),
    api("/api/v1/admin/knowledge-access/cross-tenant").catch(e=>({error:e.message,enabled:false})),
    api("/api/v1/admin/skill-sources/available").catch(e=>({error:e.message,sources:["skillhub","clawhub","github"]})),
    api("/api/v1/admin/skill-sources/global").catch(e=>({error:e.message,enabled:false,allowed_sources:[]})),
    api("/api/v1/admin/tenants?limit=500").catch(e=>({error:e.message,items:[]})),
    api("/api/v1/admin/users?limit=500").catch(e=>({error:e.message,items:[]})),
    api("/api/v1/admin/public-knowledge-libraries").catch(e=>({error:e.message,libraries:[]}))
  ]);
  const sourceItems=sources.sources||sources.items||[];
  const availableSources=available.sources||[];
  const sourceDescriptions=available.description||{};
  const tenantItems=tenantsResp.items||[];
  const userItems=usersResp.items||[];
  const publicLibraries=publicResp.libraries||[];
  const tenantNameByID=new Map(tenantItems.map(x=>[String(x.id||""),String(x.name||x.id||"")]));
  const userNameByKey=new Map();
  userItems.forEach(x=>{ const label=String(x.name||x.email||x.username||x.id||""); userNameByKey.set(`${x.tenant_id||""}:${x.id||""}`,label); if(x.id&&!userNameByKey.has(`:${x.id}`)) userNameByKey.set(`:${x.id}`,label); });
  state.knowledgeTenantNames=Object.fromEntries(tenantNameByID);
  state.knowledgeUserNames=Object.fromEntries(userNameByKey);
  const knowledgeSourceRows=sourceItems.map(x=>{ const tenantID=String(x.tenant_id||""); const ownerID=String(x.owner_id||""); const ownerLabel=userNameByKey.get(`${tenantID}:${ownerID}`)||userNameByKey.get(`:${ownerID}`)||ownerID; return {...x,tenant_name:displayWithID(tenantNameByID.get(tenantID)||tenantID,tenantID),owner_name:displayWithID(ownerLabel,ownerID)}; });
  const publicKnowledgeRows=publicLibraries.map(x=>({...x,tenant_name:displayWithID(tenantNameByID.get(String(x.tenant_id||""))||x.tenant_id,x.tenant_id)}));
  $("content").innerHTML=`<section class="knowledge-page">
    <div class="knowledge-summary">
      <div class="card metric knowledge-metric"><span>${t("knowledgeSources")}</span><b>${esc(sources.total ?? sourceItems.length ?? "-")}</b><small>${esc(sourceItems.length)} ${t("sources")}</small></div>
      <div class="card metric knowledge-metric"><span>${t("crossTenant")}</span><b class="${cross.enabled?"status-warn":"status-ok"}">${esc(cross.enabled?t("enabledState"):t("disabledState"))}</b><small>${t("allowCrossTenant")}</small></div>
      <div class="card metric knowledge-metric"><span>${t("skillSourcePolicy")}</span><b>${esc(globalCfg.enabled?t("global"):t("inherit"))}</b><small>${esc((globalCfg.allowed_sources||availableSources).join(" / ")||t("empty"))}</small></div>
    </div>
    <div class="knowledge-layout">
      <div class="knowledge-main stack">
        <div class="card knowledge-panel"><div class="section-head"><div><h2>${t("knowledgeSources")}</h2><p class="muted">${t("knowledgeSourceDisplayHint")}</p></div></div>${table(knowledgeSourceRows,["id","tenant_name","owner_name","title","updated_at"],null)}</div>
        <div class="card knowledge-panel"><div class="section-head"><div><h2>${t("publicKnowledgeBases")}</h2><p class="muted">${t("publicKnowledgeHint")}</p></div></div>${publicKnowledgePanel(publicKnowledgeRows,tenantItems)}</div>
        <div class="card knowledge-panel"><div class="section-head"><div><h2>${t("knowledgeStats")}</h2><p class="muted">${t("raw")}</p></div></div><pre class="code compact-code">${esc(pretty(stats))}</pre></div>
      </div>
      <aside class="knowledge-side stack">
        <div class="card stack knowledge-panel"><div class="section-head"><div><h2>${t("knowledgeAccess")}</h2><p class="muted">${t("knowledgeAccessTargetHint")}</p></div></div><label class="switch-row"><input id="knowledgeCrossTenant" type="checkbox" ${cross.enabled?"checked":""}> <span>${t("allowCrossTenant")}</span></label><button id="saveKnowledgeCrossTenant">${t("save")}</button><div class="form-grid compact-form">${tenantSelect("knowledgeTenant",tenantItems,"",true,"selectTenant")}${userSelect("knowledgeUser",userItems,"","tenantUser",true,"selectUser")}</div><p class="muted">${t("knowledgeScopeBuilderHint")}</p><div class="form-grid compact-form scope-builder">${tenantSelect("knowledgeScopeTenant",tenantItems,"",true,"selectTenant")}${userSelect("knowledgeScopeUser",userItems,"","tenantUser",true,"selectUser")}<input id="knowledgeScopeName" placeholder="${t("scopeName")}"><button type="button" class="secondary" id="addKnowledgeScope">${t("addScope")}</button></div>${publicKnowledgeScopeSelect(publicLibraries)}<textarea id="knowledgeScopes" class="policy-textarea" placeholder='[{"tenant_id":"tenant-a","owner_id":"user-a","name":"team"}]'></textarea><div id="knowledgeScopePreview" class="knowledge-scope-preview" aria-live="polite"></div><div class="action-bar"><label class="switch-row inline"><input id="knowledgeEnabled" type="checkbox" checked> <span>${t("enabledState")}</span></label><button id="getKnowledgeAccess" class="secondary">${t("view")}</button><button id="saveKnowledgeAccess">${t("save")}</button><button class="secondary" id="resolveKnowledgeAccess">${t("resolve")}</button><button class="danger" id="deleteKnowledgeAccess">${t("delete")}</button></div></div>
        <div class="card stack knowledge-panel"><div class="section-head"><div><h2>${t("skillSources")}</h2><p class="muted">${t("skillSourcesHintDetailed")}</p></div></div><div class="policy-flow" aria-label="${esc(t("skillPolicyPriorityTitle"))}"><strong>${t("skillPolicyPriorityTitle")}</strong><span>${t("globalPolicyShort")}</span><span>${t("tenantOverride")}</span><span>${t("userOverride")}</span><small>${t("tenantOverrideHintDetailed")}</small><small>${t("userOverrideHintDetailed")}</small></div><div class="policy-grid"><section class="policy-box"><h3>${t("globalSkillPolicyTitle")}</h3><p class="muted">${t("globalSkillPolicyHint")}</p>${skillSourceChecks("skillGlobal",availableSources,globalCfg,sourceDescriptions)}<button id="saveSkillGlobal">${t("save")}</button></section><section class="policy-box"><h3>${t("tenantSkillPolicyTitle")}</h3><p class="muted">${t("tenantSkillPolicyHint")}</p>${tenantSelect("skillTenant",tenantItems,"",true,"selectTenant")}${skillSourceChecks("skillTenantCfg",availableSources,{},sourceDescriptions)}<div class="action-bar"><button id="loadSkillTenant" class="secondary">${t("view")}</button><button id="saveSkillTenant">${t("save")}</button><button class="danger" id="deleteSkillTenant">${t("delete")}</button></div></section><section class="policy-box"><h3>${t("userSkillPolicyTitle")}</h3><p class="muted">${t("userSkillPolicyHint")}</p>${userSelect("skillUser",userItems,"","tenantUser",true,"selectUser")}${skillSourceChecks("skillUserCfg",availableSources,{},sourceDescriptions)}<div class="action-bar"><button id="loadSkillUser" class="secondary">${t("view")}</button><button id="saveSkillUser">${t("save")}</button><button class="secondary" id="resolveSkillUser">${t("resolve")}</button><button class="danger" id="deleteSkillUser">${t("delete")}</button></div></section></div></div>
        <div class="card stack danger-zone knowledge-panel"><div class="section-head"><div><h2>${t("dangerous")}</h2><p class="muted">${t("clearTenantKnowledge")}</p></div></div><div class="form-grid compact-form">${tenantSelect("knowledgeClearTenant",tenantItems,"",true,"selectTenant")}</div><button class="danger" id="clearTenantKnowledge">${t("clearTenantKnowledge")}</button></div>
      </aside>
    </div>
    <details class="card raw-details"><summary>${t("raw")}</summary><pre id="knowledgeOut" class="code">${esc(pretty({stats,sources,cross,available,global:globalCfg,tenants:tenantsResp,users:usersResp,public_libraries:publicResp}))}</pre></details>
  </section>`;
  syncTenantFromUser("knowledgeUser","knowledgeTenant");
  syncTenantFromUser("knowledgeScopeUser","knowledgeScopeTenant");
  bindKnowledgeActions(availableSources,publicLibraries);
}
function publicKnowledgeOptionLabel(x){ return `${x.name} / ${x.tenant_name||displayWithID(state.knowledgeTenantNames?.[String(x.tenant_id||"")]||x.tenant_id,x.tenant_id)}`; }
function publicKnowledgeScopeSelect(libraries){ return `<div class="form-grid compact-form scope-builder public-scope-builder"><select id="knowledgePublicScopeLibrary"><option value="">${t("publicKnowledgeBases")}</option>${(libraries||[]).map(x=>`<option value="${esc(x.id)}">${esc(publicKnowledgeOptionLabel(x))}</option>`).join("")}</select><button type="button" class="secondary" id="addPublicKnowledgeScope">${t("addPublicKnowledgeScope")}</button><p class="muted span-2">${t("selectedUserForPublicKnowledge")}</p></div>`; }
function publicKnowledgePanel(libraries,tenants){ return `<div class="stack"><div class="form-grid compact-form">${tenantSelect("publicKnowledgeTenant",tenants,"",true,"selectTenant")}<input id="publicKnowledgeName" placeholder="${t("publicKnowledgeName")}"><button id="createPublicKnowledge" type="button">${t("createPublicKnowledgeBase")}</button></div><div>${table(libraries||[],["name","tenant_name","source_count","distilled_sources","latest_source_at"],x=>`<button class="secondary" data-public-kb-add="${esc(x.id)}">${t("attachPublicKnowledge")}</button> <button class="secondary" data-public-kb-remove="${esc(x.id)}">${t("detachPublicKnowledge")}</button> <button class="secondary" data-public-kb-select="${esc(x.id)}">${t("view")}</button> <button class="danger" data-public-kb-delete="${esc(x.id)}">${t("delete")}</button>`)}</div><div class="form-grid compact-form"><select id="publicKnowledgeImportTarget"><option value="">${t("allOption")}</option>${(libraries||[]).map(x=>`<option value="${esc(x.id)}">${esc(publicKnowledgeOptionLabel(x))}</option>`).join("")}</select><input id="publicKnowledgeTopic" placeholder="${t("scopeName")}"><input id="publicKnowledgeLabels" placeholder="${t("labels")}"></div><div class="form-grid compact-form"><textarea id="publicKnowledgeText" class="policy-textarea" placeholder="${t("importText")}"></textarea><button id="publicKnowledgeImportText" type="button" class="secondary import-action import-action-wide">${t("importText")}</button></div><div class="form-grid compact-form"><input id="publicKnowledgeFile" type="file" multiple accept=".doc,.docx,.pdf,.pptx,.xlsx,.xls,.csv,.md,.markdown,.txt,.text,.zip,.rar"><button id="publicKnowledgeImportFile" type="button" class="secondary import-action import-action-wide">${t("importDocumentArchive")}</button></div><div class="form-grid compact-form"><textarea id="publicKnowledgeURLs" class="policy-textarea" placeholder="${t("urlPlaceholder")}"></textarea><label class="field-inline" for="publicKnowledgeDepth"><span>${t("crawlDepth")}</span><input id="publicKnowledgeDepth" type="number" min="0" max="5" step="1" value="0"></label><label class="switch-row"><input id="publicKnowledgeSameDomain" type="checkbox" checked> <span>${t("sameDomainOnly")}</span></label><button id="publicKnowledgeImportURLs" type="button" class="secondary import-action import-action-wide">${t("importURL")}</button></div></div>`; }
function skillSourceDescription(source,descriptions){ const fallback={skillhub:t("skillhubDesc"),clawhub:t("clawhubDesc"),github:t("githubDesc"),enterprise_hub:t("enterpriseHubDesc"),local:t("localDesc")}; return fallback[source]||(descriptions&&descriptions[source])||""; }
function skillSourceChecks(prefix,sources,cfg,descriptions){ const allowed=new Set(cfg.allowed_sources||sources||[]); return `<label class="switch-row"><input id="${prefix}_enabled" type="checkbox" ${cfg.enabled?"checked":""}> <span>${t("enabledState")}</span><small>${t("skillPolicyEnabledHint")}</small></label><div class="source-checks">${(sources||[]).map(s=>{const desc=skillSourceDescription(s,descriptions); return `<label><input id="${prefix}_${esc(s)}" type="checkbox" ${allowed.has(s)?"checked":""}> <span><strong>${esc(s)}</strong>${desc?`<small>${esc(desc)}</small>`:""}</span></label>`;}).join("")}</div>`; }
function skillSourceBody(prefix,sources){ return {enabled:!!$(`${prefix}_enabled`)?.checked, allowed_sources:(sources||[]).filter(s=>$(`${prefix}_${s}`)?.checked)}; }
function applySkillSource(prefix,sources,cfg){ if($(`${prefix}_enabled`)) $(`${prefix}_enabled`).checked=!!cfg.enabled; for(const s of sources||[]){ const el=$(`${prefix}_${s}`); if(el) el.checked=(cfg.allowed_sources||[]).includes(s); } }
function knowledgeIds(){ const selected=splitTenantUser($('knowledgeUser').value.trim()); return {tenant:selected.tenant||$('knowledgeTenant').value.trim(), user:selected.user||$('knowledgeUser').value.trim()}; }
function requireKnowledgeIds(){ const ids=knowledgeIds(); if(!ids.tenant){ toast(t("tenantIDRequired")); return null; } if(!ids.user){ toast(t("requiredField")); return null; } return ids; }
function requireInput(id,labelKey){ const value=$(id).value.trim(); if(!value){ toast(t(labelKey||"requiredField")); return ""; } return value; }
function requireSkillUserIds(){ const ids=tenantUserValue('skillTenant','skillUser'); if(!ids.tenant){ toast(t("tenantIDRequired")); return null; } if(!ids.user){ toast(t("requiredField")); return null; } return ids; }
function appendKnowledgeScope(){ const ids=tenantUserValue('knowledgeScopeTenant','knowledgeScopeUser'); if(!ids.tenant){ toast(t("tenantIDRequired")); return; } if(!ids.user){ toast(t("requiredField")); return; } let scopes; try{scopes=parseJSONField("knowledgeScopes","[]");}catch{return;} scopes.push({tenant_id:ids.tenant,owner_id:ids.user,name:$('knowledgeScopeName').value.trim()||ids.user}); $('knowledgeScopes').value=pretty(scopes); renderKnowledgeScopePreview(scopes,t("configuredKnowledgeScopes")); }
function appendPublicKnowledgeScope(libraries){ const id=$('knowledgePublicScopeLibrary')?.value||""; const lib=(libraries||[]).find(x=>x.id===id); if(!lib){ toast(t("requiredField")); return; } let scopes; try{scopes=parseJSONField("knowledgeScopes","[]");}catch{return;} scopes.push({tenant_id:lib.tenant_id,owner_id:lib.owner_id,name:lib.name}); $('knowledgeScopes').value=pretty(scopes); renderKnowledgeScopePreview(scopes,t("configuredKnowledgeScopes")); }
function knowledgeScopeType(scope){ if(String(scope.name||"")==="self") return t("ownKnowledge"); if(String(scope.owner_id||"").startsWith("public:")) return t("publicKnowledgeBases"); return t("sharedKnowledge"); }
function knowledgeScopeDisplay(scope){ const tenantID=String(scope.tenant_id||""); const ownerID=String(scope.owner_id||""); const tenantLabel=state.knowledgeTenantNames?.[tenantID]||tenantID; const ownerLabel=state.knowledgeUserNames?.[`${tenantID}:${ownerID}`]||state.knowledgeUserNames?.[`:${ownerID}`]||ownerID; return {tenant:displayWithID(tenantLabel,tenantID), owner:displayWithID(ownerLabel,ownerID)}; }
function renderKnowledgeScopePreview(scopes,title=t("configuredKnowledgeScopes")){ const box=$('knowledgeScopePreview'); if(!box) return; const items=Array.isArray(scopes)?scopes:[]; if(!items.length){ box.innerHTML=`<p class="muted">${esc(title)}: ${esc(t("empty"))}</p>`; return; } box.innerHTML=`<strong>${esc(title)}</strong><div>${items.map(s=>{const d=knowledgeScopeDisplay(s); return `<span class="knowledge-scope-chip"><b>${esc(s.name||knowledgeScopeType(s))}</b><small>${esc(knowledgeScopeType(s))}</small><code>${esc(d.tenant)} / ${esc(d.owner)}</code></span>`;}).join("")}</div>`; }
function setKnowledgeAccessFields(cfg){ if($('knowledgeEnabled')) $('knowledgeEnabled').checked=!!cfg.enabled; if($('knowledgeScopes')) $('knowledgeScopes').value=pretty(cfg.read_scopes||[]); renderKnowledgeScopePreview(cfg.read_scopes||[],t("configuredKnowledgeScopes")); }
function showKnowledgeOut(value,open=true){ const out=$('knowledgeOut'); if(out) out.textContent=formatKnowledgeOut(value); const raw=document.querySelector('.raw-details'); if(open&&raw) raw.open=true; }
function formatKnowledgeOut(value){ if(typeof value==="string") return value; if(!value||typeof value!=="object") return pretty(value); const result=value.result&&typeof value.result==="object"?value.result:value; const lines=[]; if(value.job_id||value.id) lines.push(`${t("importJob")}: ${value.job_id||value.id}`); if(value.status) lines.push(`${t("importStatus")}: ${value.status}`); if(value.source_id) lines.push(`${t("importSource")}: ${value.source_id}`); if(value.title) lines.push(`${t("importTitle")}: ${value.title}`); if(value.kind) lines.push(`${t("importKind")}: ${value.kind}`); if(Number.isFinite(value.file_count)) lines.push(`${t("importFiles")}: ${value.file_count}`); if(Array.isArray(value.filenames)&&value.filenames.length) lines.push(`${t("importFiles")}: ${value.filenames.join(", ")}`); if(Number.isFinite(value.url_count)) lines.push(`${t("importUrls")}: ${value.url_count}`); [["processed_files","importProcessed"],["imported_files","importImported"],["failed_files","importFailed"],["skipped_files","importSkipped"],["duplicate_files","importDuplicates"]].forEach(([key,label])=>{ if(Number.isFinite(result[key])) lines.push(`${t(label)}: ${result[key]}`); }); if(Array.isArray(result.warnings)&&result.warnings.length) lines.push(`${t("importWarnings")}: ${result.warnings.length}`); if(value.error||result.error) lines.push(`${t("failed")}: ${value.error||result.error}`); return lines.length?`${lines.join("\n")}\n\n${pretty(value)}`:pretty(value); }
async function watchAdminJob(jobID){ if(!jobID) return null; let latest=null; for(let i=0;i<60;i++){ latest=await api(`/api/v1/admin/jobs/${encodeURIComponent(jobID)}`); showKnowledgeOut(latest); if(["succeeded","failed","canceled"].includes(String(latest.status||""))) return latest; await new Promise(resolve=>setTimeout(resolve,1000)); } return latest; }
function toastKnowledgeJobResult(job){ const status=String(job?.status||"").toLowerCase(); if(status==="succeeded") toast(t("importCompleted")); else if(status==="failed"||status==="canceled") toast(`${t("failed")}: ${job?.error||status}`); else toast(t("importStillRunning")); }
async function withPublicKnowledgeButton(btn,fn){ if(!btn) return fn(); const text=btn.textContent; btn.disabled=true; btn.textContent="..."; try{return await fn();}catch(e){toast(`${t("failed")}: ${e.message}`);}finally{btn.disabled=false; btn.textContent=text; applyOwnerGuards();} }
async function updatePublicKnowledgeAccess(library,method,btn){ try{ await withPublicKnowledgeButton(btn,async()=>{ if(!library){ toast(t("requiredField")); return; } const ids=requireKnowledgeIds(); if(!ids) return; const cfg=await api(`/api/v1/admin/knowledge-access/tenants/${encodeURIComponent(ids.tenant)}/users/${encodeURIComponent(ids.user)}/public-libraries/${encodeURIComponent(library.id)}`,{method}); setKnowledgeAccessFields(cfg); showKnowledgeOut(cfg); toast(t('saved')); }); }catch{} }
function bindPublicKnowledgeActions(libraries){
  const selectedLibrary=()=>{ const id=$('publicKnowledgeImportTarget')?.value||""; const library=(libraries||[]).find(x=>x.id===id); if(!library) toast(t("requiredField")); return library; };
  $('createPublicKnowledge').onclick=async()=>{ await withPublicKnowledgeButton($('createPublicKnowledge'),async()=>{ const tenant=$('publicKnowledgeTenant').value.trim(); const name=$('publicKnowledgeName').value.trim(); if(!tenant||!name){ toast(t('requiredField')); return; } showKnowledgeOut(await api('/api/v1/admin/public-knowledge-libraries',{method:'POST',body:JSON.stringify({tenant_id:tenant,name})})); toast(t('saved')); render(); }); };
  document.querySelectorAll('[data-public-kb-add]').forEach(b=>b.onclick=async()=>updatePublicKnowledgeAccess((libraries||[]).find(x=>x.id===b.dataset.publicKbAdd),'POST',b));
  document.querySelectorAll('[data-public-kb-remove]').forEach(b=>b.onclick=async()=>updatePublicKnowledgeAccess((libraries||[]).find(x=>x.id===b.dataset.publicKbRemove),'DELETE',b));
  document.querySelectorAll('[data-public-kb-select]').forEach(b=>b.onclick=async()=>{ await withPublicKnowledgeButton(b,async()=>{ if($('publicKnowledgeImportTarget')) $('publicKnowledgeImportTarget').value=b.dataset.publicKbSelect; showKnowledgeOut(await api(`/api/v1/admin/public-knowledge-libraries/${encodeURIComponent(b.dataset.publicKbSelect)}/sources`)); }); });
  document.querySelectorAll('[data-public-kb-delete]').forEach(b=>b.onclick=async()=>{ if(!await confirmDanger(t('deletePublicKnowledgeConfirm'))) return; await withPublicKnowledgeButton(b,async()=>{ showKnowledgeOut(await api(`/api/v1/admin/public-knowledge-libraries/${encodeURIComponent(b.dataset.publicKbDelete)}`,{method:'DELETE'})); toast(t('saved')); render(); }); });
  $('publicKnowledgeImportText').onclick=async()=>{ await withPublicKnowledgeButton($('publicKnowledgeImportText'),async()=>{ const lib=selectedLibrary(); if(!lib) return; const text=$('publicKnowledgeText').value.trim(); if(!text){ toast(t('importTextRequired')); return; } showKnowledgeOut(await api(`/api/v1/admin/public-knowledge-libraries/${encodeURIComponent(lib.id)}/import/text`,{method:'POST',body:JSON.stringify({text,topic_hint:$('publicKnowledgeTopic').value,labels:$('publicKnowledgeLabels').value})})); toast(t('saved')); }); };
  $('publicKnowledgeImportURLs').onclick=async()=>{ await withPublicKnowledgeButton($('publicKnowledgeImportURLs'),async()=>{ const lib=selectedLibrary(); if(!lib) return; const text=$('publicKnowledgeURLs').value.trim(); if(!text){ toast(t('importURLRequired')); return; } showKnowledgeOut(t('importing')); const out=await api(`/api/v1/admin/public-knowledge-libraries/${encodeURIComponent(lib.id)}/import/urls`,{method:'POST',body:JSON.stringify({text,max_depth:Number($('publicKnowledgeDepth').value||0),same_domain_only:$('publicKnowledgeSameDomain').checked,topic_hint:$('publicKnowledgeTopic').value,labels:$('publicKnowledgeLabels').value})}); showKnowledgeOut(out); toast(t('importStarted')); toastKnowledgeJobResult(await watchAdminJob(out.job_id)); }); };
  $('publicKnowledgeImportFile').onclick=async()=>{ await withPublicKnowledgeButton($('publicKnowledgeImportFile'),async()=>{ const lib=selectedLibrary(); const files=[...($('publicKnowledgeFile')?.files||[])]; if(!lib||!files.length){ toast(t('requiredField')); return; } const form=new FormData(); files.forEach(file=>form.append('file',file)); form.append('topic_hint',$('publicKnowledgeTopic').value); form.append('labels',$('publicKnowledgeLabels').value); setNetworkBusy(1); try{ showKnowledgeOut(t('importing')); const resp=await fetch(`/api/v1/admin/public-knowledge-libraries/${encodeURIComponent(lib.id)}/import/file`,{method:'POST',headers:headers(false),body:form}); const text=await resp.text(); let out={}; try{out=text?JSON.parse(text):{};}catch{out={raw:text};} if(!resp.ok) throw new Error(out.error||text||resp.statusText); showKnowledgeOut(out); toast(t('importStarted')); toastKnowledgeJobResult(await watchAdminJob(out.job_id)); } finally { setNetworkBusy(-1); } }); };
}
function bindKnowledgeActions(sources,publicLibraries){
  const out=()=>$('knowledgeOut');
  const show=x=>showKnowledgeOut(x,false);
  const saveOK=x=>{ showKnowledgeOut(x); toast(t("saved")); };
  const run=fn=>async()=>{ try{ await fn(); }catch(e){ toast(`${t("failed")}: ${e.message}`); } };
  $('addKnowledgeScope').onclick=appendKnowledgeScope;
  $('addPublicKnowledgeScope').onclick=()=>appendPublicKnowledgeScope(publicLibraries);
  $('knowledgeScopes').oninput=()=>{ try{renderKnowledgeScopePreview(parseJSONField("knowledgeScopes","[]"),t("configuredKnowledgeScopes"));}catch{} };
  bindPublicKnowledgeActions(publicLibraries);
  $('saveKnowledgeCrossTenant').onclick=run(async()=>saveOK(await api('/api/v1/admin/knowledge-access/cross-tenant',{method:'PUT',body:JSON.stringify({enabled:$('knowledgeCrossTenant').checked})})));
  $('clearTenantKnowledge').onclick=run(async()=>{ const tenant=requireInput('knowledgeClearTenant','tenantIDRequired'); if(!tenant) return; if(await confirmDanger(t("clearKnowledgeConfirm"))) saveOK(await api(`/api/v1/admin/tenants/${encodeURIComponent(tenant)}/knowledge?confirm=true`,{method:'DELETE'})); });
  $('getKnowledgeAccess').onclick=run(async()=>{ const ids=requireKnowledgeIds(); if(!ids) return; const cfg=await api(`/api/v1/admin/knowledge-access/tenants/${encodeURIComponent(ids.tenant)}/users/${encodeURIComponent(ids.user)}`); setKnowledgeAccessFields(cfg); show(cfg); });
  $('saveKnowledgeAccess').onclick=run(async()=>{ const ids=requireKnowledgeIds(); if(!ids) return; let scopes; try{scopes=parseJSONField("knowledgeScopes","[]");}catch{return;} saveOK(await api(`/api/v1/admin/knowledge-access/tenants/${encodeURIComponent(ids.tenant)}/users/${encodeURIComponent(ids.user)}`,{method:'PUT',body:JSON.stringify({enabled:$('knowledgeEnabled').checked,read_scopes:scopes})})); });
  $('resolveKnowledgeAccess').onclick=run(async()=>{ const ids=requireKnowledgeIds(); if(!ids) return; const resolved=await api(`/api/v1/admin/knowledge-access/tenants/${encodeURIComponent(ids.tenant)}/users/${encodeURIComponent(ids.user)}/resolve`); renderKnowledgeScopePreview(resolved.scopes||[],t("effectiveKnowledgeAccess")); show(resolved); });
  $('deleteKnowledgeAccess').onclick=run(async()=>{ const ids=requireKnowledgeIds(); if(!ids) return; if(await confirmDanger(t("deleteKnowledgeAccessConfirm"))) saveOK(await api(`/api/v1/admin/knowledge-access/tenants/${encodeURIComponent(ids.tenant)}/users/${encodeURIComponent(ids.user)}`,{method:'DELETE'})); });
  $('saveSkillGlobal').onclick=run(async()=>saveOK(await api('/api/v1/admin/skill-sources/global',{method:'PUT',body:JSON.stringify(skillSourceBody('skillGlobal',sources))})));
  $('loadSkillTenant').onclick=run(async()=>{ const tenant=requireInput('skillTenant','tenantIDRequired'); if(!tenant) return; const cfg=await api(`/api/v1/admin/skill-sources/tenant/${encodeURIComponent(tenant)}`); applySkillSource('skillTenantCfg',sources,cfg); show(cfg); });
  $('saveSkillTenant').onclick=run(async()=>{ const tenant=requireInput('skillTenant','tenantIDRequired'); if(!tenant) return; saveOK(await api(`/api/v1/admin/skill-sources/tenant/${encodeURIComponent(tenant)}`,{method:'PUT',body:JSON.stringify(skillSourceBody('skillTenantCfg',sources))})); });
  $('deleteSkillTenant').onclick=run(async()=>{ const tenant=requireInput('skillTenant','tenantIDRequired'); if(!tenant) return; if(await confirmDanger(t("deleteSkillTenantConfirm"))) saveOK(await api(`/api/v1/admin/skill-sources/tenant/${encodeURIComponent(tenant)}`,{method:'DELETE'})); });
  $('loadSkillUser').onclick=run(async()=>{ const ids=requireSkillUserIds(); if(!ids) return; const cfg=await api(`/api/v1/admin/skill-sources/tenants/${encodeURIComponent(ids.tenant)}/users/${encodeURIComponent(ids.user)}`); applySkillSource('skillUserCfg',sources,cfg); show(cfg); });
  $('saveSkillUser').onclick=run(async()=>{ const ids=requireSkillUserIds(); if(!ids) return; saveOK(await api(`/api/v1/admin/skill-sources/tenants/${encodeURIComponent(ids.tenant)}/users/${encodeURIComponent(ids.user)}`,{method:'PUT',body:JSON.stringify(skillSourceBody('skillUserCfg',sources))})); });
  $('resolveSkillUser').onclick=run(async()=>{ const ids=requireSkillUserIds(); if(!ids) return; show(await api(`/api/v1/admin/skill-sources/tenants/${encodeURIComponent(ids.tenant)}/users/${encodeURIComponent(ids.user)}/resolve`)); });
  $('deleteSkillUser').onclick=run(async()=>{ const ids=requireSkillUserIds(); if(!ids) return; if(await confirmDanger(t("deleteSkillUserConfirm"))) saveOK(await api(`/api/v1/admin/skill-sources/tenants/${encodeURIComponent(ids.tenant)}/users/${encodeURIComponent(ids.user)}`,{method:'DELETE'})); });
}
async function ops(){ setTitle(t("ops"), t("opsHint")); const [audit,snaps,risks,tenantsResp,usersResp,adminsResp]=await Promise.all([api("/api/v1/admin/audit-events?limit=50").catch(e=>({error:e.message,items:[]})),api("/api/v1/admin/snapshots?limit=50").catch(e=>({error:e.message,items:[]})),api("/api/v1/admin/security/risk-events?limit=50").catch(e=>({error:e.message,items:[]})),api("/api/v1/admin/tenants?limit=500").catch(e=>({error:e.message,items:[]})),api("/api/v1/admin/users?limit=500").catch(e=>({error:e.message,items:[]})),api("/api/v1/admin/auth/users").catch(e=>({error:e.message,items:[]}))]); const tenantItems=tenantsResp.items||[]; const userItems=usersResp.items||[]; const adminItems=adminsResp.items||[]; $("content").innerHTML=`<div class="grid"><div class="card stack"><h2>${t("audit")}</h2><div class="form-grid"><input id="auditAction" placeholder="${t("actionName")}">${tenantSelect("auditTenant",tenantItems)}${userSelect("auditUser",userItems,"","tenantUser")}${adminSelect("auditActorUser",adminItems)}<input id="auditLimit" type="number" value="50"></div><button id="loadAudit">${t("refresh")}</button></div><div class="card stack"><h2>${t("export")}</h2><div class="form-grid">${tenantSelect("exportTenant",tenantItems)}${userSelect("exportUser",userItems,"","tenantUser")}<label class="row"><input id="exportMessages" type="checkbox" checked class="w-auto"> ${t("messages")}</label><label class="row"><input id="exportRuns" type="checkbox" checked class="w-auto"> ${t("runs")}</label><label class="row"><input id="exportAudit" type="checkbox" checked class="w-auto"> ${t("audit")}</label><label class="row"><input id="exportSecrets" type="checkbox" class="w-auto"> ${t("secrets")}</label></div><button id="runExport">${t("export")}</button></div><div class="card stack"><h2>${t("import")}</h2><textarea id="importText" placeholder="${t("pasteExportedJSON")}"></textarea><div class="row"><label class="row"><input id="importOverwrite" type="checkbox" class="w-auto"> ${t("overwrite")}</label><label class="row"><input id="importDryRun" type="checkbox" checked class="w-auto"> ${t("dryRun")}</label><button id="runImport">${t("import")}</button></div></div><div class="card stack"><h2>${t("snapshots")}</h2><div class="form-grid"><input id="snapshotName" placeholder="${t("name")}">${tenantSelect("snapshotTenant",tenantItems)}${userSelect("snapshotUser",userItems,"","tenantUser")}<label class="row"><input id="snapshotMessages" type="checkbox" checked class="w-auto"> ${t("messages")}</label><label class="row"><input id="snapshotRuns" type="checkbox" checked class="w-auto"> ${t("runs")}</label><label class="row"><input id="snapshotAudit" type="checkbox" checked class="w-auto"> ${t("audit")}</label><label class="row"><input id="snapshotSecrets" type="checkbox" class="w-auto"> ${t("secrets")}</label></div><div class="row"><button id="createSnapshot">${t("create")}</button><button class="secondary" id="refreshSnapshots">${t("refresh")}</button><input id="pruneKeep" type="number" value="10" class="w-narrow"><button class="warn" id="pruneSnapshots">${t("dryRun")}</button></div></div></div><div class="card"><h2>${t("riskEvents")}</h2><div class="row"><select id="riskSeverity"><option value="">${t("all")}</option>${localizedOptions(["high","medium","low"],"severity")}</select><input id="riskKind" placeholder="${t("kindPlaceholder")}" list="riskKindOptions"><datalist id="riskKindOptions">${riskKindOptions(risks.kind_counts)}</datalist><input id="riskSince" type="datetime-local"><input id="riskUntil" type="datetime-local"><input id="riskLimit" type="number" min="1" max="500" value="50" class="w-narrow"><button class="secondary" id="loadRisks">${t("refresh")}</button><button class="secondary" id="clearRiskFilters">${t("clear")}</button></div><div class="row"><button class="secondary" data-risk-preset="1">1h</button><button class="secondary" data-risk-preset="24">24h</button><button class="secondary" data-risk-preset="168">7d</button><button class="secondary" data-risk-preset="all">${t("all")}</button></div><div id="riskFilterStatus">${riskFilterSummary(risks)}</div><h3>${t("bySeverity")}</h3><div id="riskSeverityCounts">${riskCountChips(risks.counts,"severity")}</div><h3>${t("byKind")}</h3><div id="riskKindCounts">${riskCountChips(risks.kind_counts)}</div><div id="riskEventsList">${riskEventsTable(risks.items||[])}</div></div><div class="card"><h2>${t("snapshots")}</h2>${snapshotTable(snaps.items||[])}</div><div class="card"><h2>${t("raw")}</h2><pre id="opsOut" class="code">${esc(pretty({audit,snapshots:snaps,security_risks:risks,tenants:tenantsResp,users:usersResp,admins:adminsResp}))}</pre></div>`; bindOpsSnapshotActions(); bindRiskEventActions(risks.items||[]); bindRiskCountChips(); $("loadRisks").onclick=loadRiskEventsFromFilters; $("clearRiskFilters").onclick=clearRiskFilters; document.querySelectorAll("[data-risk-preset]").forEach(btn=>btn.onclick=()=>setRiskTimePreset(btn.dataset.riskPreset==="all"?0:Number(btn.dataset.riskPreset))); syncTenantFromUser("auditUser","auditTenant"); syncTenantFromUser("exportUser","exportTenant"); syncTenantFromUser("snapshotUser","snapshotTenant"); applyPendingRiskFilter(); $("loadAudit").onclick=async()=>{const q=new URLSearchParams(); if($("auditAction").value) q.set("action",$("auditAction").value); const ids=tenantUserValue("auditTenant","auditUser"); if(ids.tenant) q.set("tenant_id",ids.tenant); if(ids.user) q.set("user_id",ids.user); if($("auditActorUser").value) q.set("actor_user_id",$("auditActorUser").value); let auditLimit; try{auditLimit=numberInRange("auditLimit",50,1,500);}catch{return;} q.set("limit",String(auditLimit)); $("opsOut").textContent=pretty(await api(`/api/v1/admin/audit-events?${q}`));}; $("runExport").onclick=async()=>{const q=exportParams("export"); if($("exportSecrets").checked){if(!await confirmPhrase(t("exportSecretPrompt"),"EXPORT SECRETS")) return; q.set("confirm","true");} $("opsOut").textContent=pretty(await api(`/api/v1/admin/export?${q}`));}; $("runImport").onclick=async()=>{let data; try{data=parseJSONField("importText","{}");}catch{return;} const q=new URLSearchParams({overwrite:String($("importOverwrite").checked),dry_run:String($("importDryRun").checked)}); if(!$("importDryRun").checked){if(!await confirmPhrase(t("importRunPrompt"),"IMPORT STATE")) return; q.set("confirm","true");} $("opsOut").textContent=pretty(await api(`/api/v1/admin/import?${q}`,{method:"POST",body:JSON.stringify({data,overwrite:$("importOverwrite").checked,dry_run:$("importDryRun").checked})}));}; $("createSnapshot").onclick=async()=>{const ids=tenantUserValue("snapshotTenant","snapshotUser"); const body={name:$("snapshotName").value,tenant_id:ids.tenant,user_id:ids.user,include_messages:$("snapshotMessages").checked,include_runs:$("snapshotRuns").checked,include_audit:$("snapshotAudit").checked,include_secrets:$("snapshotSecrets").checked}; let path="/api/v1/admin/snapshots"; if(body.include_secrets){if(!await confirmPhrase(t("snapshotSecretPrompt"),"SNAPSHOT SECRETS")) return; path+="?confirm=true";} $("opsOut").textContent=pretty(await api(path,{method:"POST",body:JSON.stringify(body)})); render();}; $("refreshSnapshots").onclick=()=>render(); $("pruneSnapshots").onclick=async()=>{$("opsOut").textContent=pretty(await api("/api/v1/admin/snapshots/prune",{method:"POST",body:JSON.stringify({keep_latest:numberInRange("pruneKeep",10,1,1000),dry_run:true})}));}; }
function exportParams(prefix){ const q=new URLSearchParams(); const ids=tenantUserValue(prefix+"Tenant",prefix+"User"); if(ids.tenant) q.set("tenant_id",ids.tenant); if(ids.user) q.set("user_id",ids.user); q.set("include_messages",String($(prefix+"Messages").checked)); q.set("include_runs",String($(prefix+"Runs").checked)); q.set("include_audit",String($(prefix+"Audit").checked)); q.set("include_secrets",String($(prefix+"Secrets").checked)); return q; }
function snapshotTable(items){ return table(items,["id","name","scope","tenant_id","user_id","size_bytes","created_at"],x=>`<button class="secondary" data-snapshot-get="${esc(x.id)}">${t("view")}</button> <button class="warn" data-snapshot-restore="${esc(x.id)}">${t("restore")} ${t("dryRun")}</button> <button class="warn" data-snapshot-restore-run="${esc(x.id)}">${t("restore")}</button> <button class="danger" data-snapshot-delete="${esc(x.id)}">${t("delete")}</button>`); }
function defaultClientSearchProviders(){ return [{name:"Brave",type:"brave",base_url:"https://api.search.brave.com/res/v1/web/search"},{name:"Serper",type:"serper",base_url:"https://google.serper.dev/search"},{name:"TinyFish",type:"tinyfish",base_url:"https://api.search.tinyfish.ai"},{name:"Tavily",type:"tavily",base_url:"https://api.tavily.com/search"},{name:"DuckDuckGo",type:"duckduckgo"}]; }
function normalizeClientSearchProviders(providers){ const existing=Array.isArray(providers)?providers.filter(p=>p&&String(p.type||p.name||"").trim()):[]; const byType=new Map(existing.map(p=>[String(p.type||p.name||"").trim().toLowerCase(),p])); const out=defaultClientSearchProviders().map(def=>({...def,...(byType.get(def.type)||{})})); existing.forEach(p=>{ const key=String(p.type||p.name||"").trim().toLowerCase(); if(key&&!out.some(x=>String(x.type||x.name||"").trim().toLowerCase()===key)) out.push(p); }); return out; }
function clientSearchProviderID(provider,index){ return String(provider.type||provider.name||`provider-${index}`).trim()||`provider-${index}`; }
function clientSearchDOMID(id){ return String(id||"provider").replace(/[^a-zA-Z0-9_-]/g,"_"); }
function clientSearchProviderTitle(provider){ return provider.name||provider.type||t("currentProvider"); }
function clientSearchProviderHint(type){ switch(String(type||"").toLowerCase()){ case "brave": return t("searchProviderBraveHint"); case "serper": return t("searchProviderSerperHint"); case "tinyfish": return t("searchProviderTinyfishHint"); case "tavily": return t("searchProviderTavilyHint"); case "duckduckgo": return t("searchProviderDuckduckgoHint"); default: return t("searchProviderGenericHint"); } }
function renderClientSearchProviderPicker(providers,current){ return `<div class="client-search-config"><p class="helper-text">${esc(t("searchProviderHelp"))}</p><div class="client-search-layout"><div class="client-search-provider-list" role="listbox" aria-label="${esc(t("currentProvider"))}">${providers.map((p,i)=>{ const id=clientSearchProviderID(p,i); const active=id===current; const meta=String(p.type||"").toLowerCase()==="duckduckgo"?t("freeNoKey"):t("apiKeySupported"); return `<button type="button" class="client-search-provider ${active?"active":""}" data-client-search-provider="${esc(id)}" role="option" aria-selected="${active?"true":"false"}"><strong>${esc(clientSearchProviderTitle(p))}</strong><span>${esc(meta)}</span></button>`; }).join("")}</div><div class="client-search-details">${providers.map((p,i)=>{ const id=clientSearchProviderID(p,i); const domID=clientSearchDOMID(id); const type=String(p.type||"").toLowerCase(); const noKey=type==="duckduckgo"; return `<section class="client-search-detail" data-client-search-detail="${esc(id)}" ${id===current?"":"hidden"}><div class="split"><div><h3>${esc(clientSearchProviderTitle(p))}</h3><p class="helper-text">${esc(clientSearchProviderHint(type))}</p></div><span class="state-pill state-${noKey?"ok":"warn"}">${esc(noKey?t("freeNoKey"):t("apiKeySupported"))}</span></div><input type="hidden" data-client-search-name="${esc(id)}" value="${esc(p.name||"")}"><input type="hidden" data-client-search-type="${esc(id)}" value="${esc(p.type||"")}"><div class="form-grid"><div class="field"><label for="clientSearchKey_${esc(domID)}">${t("providerKey")}</label>${noKey?`<div class="empty-state compact">${esc(t("noExtraConfig"))}</div><input id="clientSearchKey_${esc(domID)}" data-client-search-key="${esc(id)}" type="hidden" value="${esc(p.key||"")}">`:`<input id="clientSearchKey_${esc(domID)}" data-client-search-key="${esc(id)}" type="password" value="${esc(p.key||"")}" placeholder="${esc(t("enterApiKey"))}" autocomplete="new-password">`}</div><div class="field"><label for="clientSearchBase_${esc(domID)}">${t("providerBaseURL")}</label><input id="clientSearchBase_${esc(domID)}" data-client-search-base="${esc(id)}" value="${esc(p.base_url||"")}"></div></div></section>`; }).join("")}</div></div></div>`; }
function bindClientSearchProviderSelector(){ document.querySelectorAll("[data-client-search-provider]").forEach(btn=>btn.onclick=()=>{ const id=btn.dataset.clientSearchProvider; $("clientSearchCurrent").value=id; document.querySelectorAll("[data-client-search-provider]").forEach(item=>{ const active=item.dataset.clientSearchProvider===id; item.classList.toggle("active",active); item.setAttribute("aria-selected",active?"true":"false"); }); document.querySelectorAll("[data-client-search-detail]").forEach(panel=>{ panel.hidden=panel.dataset.clientSearchDetail!==id; }); }); }
function readClientSearchProviders(){ return Array.from(document.querySelectorAll("[data-client-search-detail]")).map(panel=>{ const id=panel.dataset.clientSearchDetail; return {name:panel.querySelector("[data-client-search-name]")?.value?.trim()||id,type:panel.querySelector("[data-client-search-type]")?.value?.trim()||id,key:panel.querySelector("[data-client-search-key]")?.value||"",base_url:panel.querySelector("[data-client-search-base]")?.value?.trim()||""}; }); }
function securityModeLabel(value){ const key=String(value||"default").toLowerCase(); const labels={default:"securityModeDefault",relaxed:"securityModeRelaxed",standard:"securityModeStandard",strict:"securityModeStrict",developer:"securityModeDeveloper"}; return t(labels[key]||"securityModeDefault"); }
function renderClientSecurityModeTabs(current){ const value=String(current||""); const modes=["","relaxed","standard","strict","developer"]; return `<div class="field wide"><label>${t("securityGuardrails")}</label><input id="clientSecurityMode" type="hidden" value="${esc(value)}"><div class="client-mode-tabs" role="tablist" aria-label="${esc(t("securityGuardrails"))}">${modes.map(mode=>`<button type="button" role="tab" class="client-mode-tab ${mode===value?"active":""}" aria-selected="${mode===value?"true":"false"}" data-client-security-mode="${esc(mode)}">${esc(securityModeLabel(mode))}</button>`).join("")}</div></div>`; }
function bindClientSecurityModeTabs(){ document.querySelectorAll("[data-client-security-mode]").forEach(btn=>btn.onclick=()=>{ const mode=btn.dataset.clientSecurityMode||""; $("clientSecurityMode").value=mode; document.querySelectorAll("[data-client-security-mode]").forEach(item=>{ const active=(item.dataset.clientSecurityMode||"")===mode; item.classList.toggle("active",active); item.setAttribute("aria-selected",active?"true":"false"); }); }); }
function bindOpsSnapshotActions(){ document.querySelectorAll("[data-snapshot-get]").forEach(b=>b.onclick=async()=>$("opsOut").textContent=pretty(await api(`/api/v1/admin/snapshots/${encodeURIComponent(b.dataset.snapshotGet)}`))); document.querySelectorAll("[data-snapshot-restore]").forEach(b=>b.onclick=async()=>$("opsOut").textContent=pretty(await api(`/api/v1/admin/snapshots/${encodeURIComponent(b.dataset.snapshotRestore)}/restore?dry_run=true`,{method:"POST",body:JSON.stringify({dry_run:true})}))); document.querySelectorAll("[data-snapshot-restore-run]").forEach(b=>b.onclick=async()=>{if(!await confirmPhrase(t("restoreRunPrompt"),"RESTORE SNAPSHOT")) return; $("opsOut").textContent=pretty(await api(`/api/v1/admin/snapshots/${encodeURIComponent(b.dataset.snapshotRestoreRun)}/restore?confirm=true`,{method:"POST",body:JSON.stringify({dry_run:false})}));}); document.querySelectorAll("[data-snapshot-delete]").forEach(b=>b.onclick=async()=>{if(await confirmDanger(t("deleteSnapshotConfirm"))){ $("opsOut").textContent=pretty(await api(`/api/v1/admin/snapshots/${encodeURIComponent(b.dataset.snapshotDelete)}?confirm=true`,{method:"DELETE"})); render(); }}); }
function cellClass(col,value){ const name=String(col||"").toLowerCase(); const v=String(value??"").toLowerCase(); if(["status","active","available","enabled","valid","configured","delete_protected","sensitive","restart_required","mutable_at_runtime","strict"].includes(name)) return "cell-state"; if(name.includes("at")||name.includes("time")||name==="created"||name==="modified") return "cell-time"; if(name.includes("id")||name.includes("key")) return "cell-code"; if(v==="fail"||v==="failed"||v==="error"||v==="false") return "cell-state"; return ""; }
function cellText(col,raw){ const name=String(col||"").toLowerCase(); const v=String(raw??"").toLowerCase(); const labels={owner:"owner",operator:"operator",active:name==="active"?"activeState":"activeStatus",suspended:"suspendedStatus",enabled:"enabledState",disabled:"disabledState",inactive:"inactiveState",true:name==="active"?"activeState":"yes",false:name==="active"?"inactiveState":"no",pending:"jobPending",running:"jobRunning",succeeded:"jobSucceeded",failed:"jobFailed",canceled:"jobCanceled",ok:"statusOK",ready:"statusReady",pass:"statusPass",warn:"statusWarn",warning:"statusWarn",fail:"statusFail",error:"statusFail",high:"severityHigh",medium:"severityMedium",low:"severityLow"}; return labels[v]?t(labels[v]):String(raw??""); }
function cellValue(col,value){ const raw=value??""; const text=cellText(col,raw); const v=String(raw).toLowerCase(); const stateMap={ok:"ok",pass:"ok",ready:"ok",active:"ok",enabled:"ok",succeeded:"ok",true:"ok",warn:"warn",warning:"warn",pending:"warn",running:"warn",false:"muted",inactive:"muted",disabled:"muted",suspended:"bad",failed:"bad",fail:"bad",error:"bad",canceled:"bad"}; const cls=stateMap[v]; if(cellClass(col,raw)==="cell-state"&&cls) return `<span class="state-pill state-${cls}">${esc(text)}</span>`; if(typeof raw==="boolean") return `<span class="state-pill state-${raw?"ok":"muted"}">${esc(text)}</span>`; return esc(text); }
function table(items, cols, action){ if(!items.length) return emptyState(t("empty")); return `<div class="table-wrap" tabindex="0" role="region" aria-label="${esc(cols.map(tableCol).join(", "))}"><table><thead><tr>${cols.map(c=>`<th>${esc(tableCol(c))}</th>`).join("")}<th>${t("action")}</th></tr></thead><tbody>${items.map(x=>`<tr>${cols.map(c=>`<td class="${cellClass(c,x[c])}">${cellValue(c,x[c])}</td>`).join("")}<td class="table-actions">${action?action(x):""}</td></tr>`).join("")}</tbody></table></div>`; }
async function clientConfig(){
  setTitle(t("clientConfig"), t("clientConfigHint"));
  const [cfgResp,schemaResp]=await Promise.all([api("/api/v1/admin/client-config/default"),api("/api/v1/admin/client-config/schema")]);
  const cfg=cfgResp.app_config||{};
  const providers=normalizeClientSearchProviders(cfg.web_search_providers);
  const advancedKeys=["mcp_servers","local_mcp_servers","ssh_hosts","skill_hub_urls","external_skill_dirs","skill_sources_allowed"];
  const advanced={}; advancedKeys.forEach(k=>advanced[k]=cfg[k]??[]);
  const currentSearchRaw=cfg.web_search_current_provider||""; const currentSearchProvider=providers.find(p=>(p.name||"")===currentSearchRaw||(p.type||"")===currentSearchRaw); const currentSearch=clientSearchProviderID(currentSearchProvider||providers[0]||{type:"duckduckgo"},0);
  $("content").innerHTML=`<div class="card stack client-config-card"><div class="panel"><h2>${t("clientConfigDefaults")}</h2><p class="helper-text">${t("clientConfigDefaultsHint")}</p></div><div class="client-config-layout"><section class="panel stack client-search-panel"><div class="split"><div><h2>${t("webSearch")}</h2><p class="helper-text">${t("searchProviderHelp")}</p></div><input id="clientSearchCurrent" type="hidden" value="${esc(currentSearch)}"></div>${renderClientSearchProviderPicker(providers,currentSearch)}</section><section class="panel stack"><h2>${t("proxyConfig")}</h2><div class="form-grid"><label class="row"><input id="clientProxyEnabled" type="checkbox" class="w-auto" ${cfg.default_proxy_enabled?"checked":""}> ${t("enabledState")}</label><div class="field"><label for="clientProxyProtocol">${t("providerType")}</label><select id="clientProxyProtocol"><option value="http">HTTP</option><option value="https">HTTPS</option><option value="socks5">SOCKS5</option></select></div><div class="field"><label for="clientProxyHost">Host</label><input id="clientProxyHost" value="${esc(cfg.default_proxy_host||"")}"></div><div class="field"><label for="clientProxyPort">Port</label><input id="clientProxyPort" value="${esc(cfg.default_proxy_port||"")}"></div><div class="field"><label for="clientProxyUsername">Username</label><input id="clientProxyUsername" value="${esc(cfg.default_proxy_username||"")}"></div><div class="field"><label for="clientProxyPassword">Password</label><input id="clientProxyPassword" type="password" value="${esc(cfg.default_proxy_password||"")}"></div><div class="field wide"><label for="clientProxyBypass">Bypass</label><input id="clientProxyBypass" value="${esc(cfg.default_proxy_bypass||"localhost;127.0.0.1;::1;*.local")}"></div><label class="row"><input id="clientProxyMaclaw" type="checkbox" class="w-auto" ${cfg.default_proxy_scope_maclaw?"checked":""}> MaClaw</label><label class="row"><input id="clientProxyAgent" type="checkbox" class="w-auto" ${cfg.default_proxy_scope_agent?"checked":""}> Agent</label></div></section><section class="panel stack"><h2>${t("securityDefaults")}</h2><div class="form-grid"><label class="row"><input id="clientHubSecurity" type="checkbox" class="w-auto" ${cfg.hub_security_centralized?"checked":""}> Hub managed</label>${renderClientSecurityModeTabs(cfg.security_policy_mode||"")}<div class="field"><label for="clientNetworkLevel">${t("networkLevel")}</label><select id="clientNetworkLevel"><option value="">${t("defaultOption")}</option><option value="full">full</option><option value="allowlist">allowlist</option><option value="intranet">intranet</option><option value="none">none</option></select></div><div class="field wide"><label for="clientNetworkAllowlist">${t("allowlist")}</label><input id="clientNetworkAllowlist" value="${esc((cfg.network_allowlist||[]).join(", "))}" placeholder="api.example.com, *.corp.local"></div></div></section><section class="panel stack"><h2>${t("experienceDefaults")}</h2><div class="form-grid"><div class="field wide"><label for="clientLanguage">${t("language")}</label><select id="clientLanguage"><option value="">${t("defaultOption")}</option><option value="zh-CN">zh-CN</option><option value="en-US">en-US</option></select></div></div></section></div><section class="panel stack"><h2>${t("advancedJSON")}</h2><p class="helper-text">${t("advancedJSONHint")}</p><textarea id="clientAdvancedJSON" class="json-editor json-editor-tall" spellcheck="false">${esc(pretty(advanced))}</textarea></section><div class="row"><button id="saveClientCfg">${t("save")}</button><button class="secondary" id="validateClientCfg">${t("validateOnly")}</button><button class="secondary" id="reloadClientCfg">${t("refresh")}</button></div><pre id="clientCfgOut" class="code">${esc(pretty({config:cfgResp,schema:schemaResp}))}</pre></div>`;
  bindClientSearchProviderSelector();
  const proxyAgent=$("clientProxyAgent"); if(proxyAgent&&!$("clientProxyCodingTools")) proxyAgent.closest("label")?.insertAdjacentHTML("beforebegin",`<label class="row"><input id="clientProxyCodingTools" type="checkbox" class="w-auto" ${cfg.default_proxy_scope_coding_tools?"checked":""}> Coding tools</label>`);
  bindClientSecurityModeTabs();
  $("clientProxyProtocol").value=cfg.default_proxy_protocol||"http"; $("clientNetworkLevel").value=cfg.network_level||""; $("clientLanguage").value=cfg.language||"";
  const body=()=>({app_config:readClientConfigForm(cfg)});
  $("saveClientCfg").onclick=async()=>{try{const result=await api("/api/v1/admin/client-config/default",{method:"PUT",body:JSON.stringify(body())}); $("clientCfgOut").textContent=pretty(result); toast(t("defaultClientConfigSaved"));}catch(e){toast(`${t("failed")}: ${e.message}`);}};
  $("validateClientCfg").onclick=async()=>{try{$("clientCfgOut").textContent=pretty(await api("/api/v1/admin/client-config/default/validate",{method:"POST",body:JSON.stringify(body())}));}catch(e){toast(`${t("failed")}: ${e.message}`);}};
  $("reloadClientCfg").onclick=()=>render();
}
function readClientConfigForm(base){ let advanced; try{advanced=JSON.parse($("clientAdvancedJSON").value||"{}");}catch(e){toast(`${t("invalidJSON")}: ${t("advancedJSON")}`); throw e;} const cfg={...(base||{}),...advanced}; const providers=readClientSearchProviders(); cfg.web_search_providers=providers; const selectedSearch=$("clientSearchCurrent").value.trim(); const selectedProvider=(providers||[]).find(p=>(p?.name||"")===selectedSearch||(p?.type||"")===selectedSearch); cfg.web_search_current_provider=selectedProvider?.type||selectedProvider?.name||selectedSearch; cfg.default_proxy_enabled=$("clientProxyEnabled").checked; cfg.default_proxy_protocol=$("clientProxyProtocol").value; cfg.default_proxy_host=$("clientProxyHost").value.trim(); cfg.default_proxy_port=$("clientProxyPort").value.trim(); cfg.default_proxy_username=$("clientProxyUsername").value.trim(); cfg.default_proxy_password=$("clientProxyPassword").value; cfg.default_proxy_bypass=$("clientProxyBypass").value.trim(); cfg.default_proxy_scope_maclaw=$("clientProxyMaclaw").checked; cfg.default_proxy_scope_agent=$("clientProxyAgent").checked; cfg.hub_security_centralized=$("clientHubSecurity").checked; cfg.security_policy_mode=$("clientSecurityMode").value; cfg.network_level=$("clientNetworkLevel").value; cfg.network_allowlist=$("clientNetworkAllowlist").value.split(/[\n,]+/).map(x=>x.trim()).filter(Boolean); cfg.language=$("clientLanguage").value; return cfg; }
const readClientConfigFormBase=readClientConfigForm; readClientConfigForm=(base)=>{const cfg=readClientConfigFormBase(base); cfg.default_proxy_scope_coding_tools=!!$("clientProxyCodingTools")?.checked; return cfg;};
$("localeSelect").onchange=()=>{state.locale=$("localeSelect").value; localStorage.setItem("maclaw.admin.locale",state.locale); render();};
$("logoutBtn").onclick=async()=>{try{ if(state.token) await api("/api/v1/admin/auth/logout",{method:"POST"}); }catch{} state.token=""; state.me=null; localStorage.removeItem("maclaw.admin.token"); render();};
window.addEventListener("hashchange",()=>{ const next=location.hash.slice(1); if(sections.includes(next)&&next!==state.section){ setSection(next,false); render(); } });
startup();

async function loadSandboxEventsFromFilters(){ const q=new URLSearchParams({limit:"20"}); if($("sandboxEventStatus")?.value) q.set("status",$("sandboxEventStatus").value); if($("sandboxEventBackend")?.value) q.set("backend",$("sandboxEventBackend").value.trim()); const events=await api(`/api/v1/admin/sandbox/events?${q}`); $("sandboxEventsTable").innerHTML=sandboxEventsTable(events.items||[]); bindSandboxEventActions(events.items||[]); $("sandboxOut").innerHTML=`<pre>${esc(pretty(events))}</pre>`; }
function bindSandboxEventActions(items){ const byId=new Map((items||[]).map(x=>[x.id,x])); document.querySelectorAll("[data-sandbox-event]").forEach(b=>b.onclick=()=>{$("sandboxOut").innerHTML=`<pre>${esc(pretty(byId.get(b.dataset.sandboxEvent)||{}))}</pre>`;}); if($("loadSandboxEvents")) $("loadSandboxEvents").onclick=loadSandboxEventsFromFilters; }

function readSandboxProfileEditor(){ let extra; try{extra=parseJSONField("sandboxProfileJSON","{}");}catch(e){ toast(t("profileJSONInvalid")); throw e; } return {...extra,name:$("sandboxProfileName").value.trim(),backend:$("sandboxProfileBackend").value,network:$("sandboxProfileNetwork").value}; }
function fillSandboxProfileEditor(profile){ if(!profile) profile={backend:"bwrap",network:"default",readonly_paths:[],writable_paths:[],env_allowlist:[]}; $("sandboxProfileName").value=profile.name||""; $("sandboxProfileBackend").value=profile.backend||"bwrap"; $("sandboxProfileNetwork").value=profile.network||"default"; const {name,backend,network,updated_at,...extra}=profile; $("sandboxProfileJSON").value=pretty(extra); }
function bindSandboxProfileActions(items){ const byName=new Map((items||[]).map(x=>[x.name,x])); document.querySelectorAll("[data-sandbox-profile]").forEach(b=>b.onclick=async()=>{const out=await api(`/api/v1/admin/sandbox/profiles/${encodeURIComponent(b.dataset.sandboxProfile)}`); const profile=out.profile||out; fillSandboxProfileEditor(profile); $("sandboxProfile").value=profile.name||"default"; $("sandboxOut").innerHTML=`<pre>${esc(pretty(out))}</pre>`;}); document.querySelectorAll("[data-sandbox-profile-delete]").forEach(b=>b.onclick=async()=>{if(await confirmDanger(t("deleteSandboxProfileConfirm"))){$("sandboxOut").innerHTML=`<pre>${esc(pretty(await api(`/api/v1/admin/sandbox/profiles/${encodeURIComponent(b.dataset.sandboxProfileDelete)}?confirm=true`,{method:"DELETE"})))}</pre>`; render();}}); $("validateSandboxProfile").onclick=async()=>{const body=readSandboxProfileEditor(); if(!body.name) return toast(t("profileName")); $("sandboxOut").innerHTML=`<pre>${esc(pretty(await api(`/api/v1/admin/sandbox/profiles/${encodeURIComponent(body.name)}/validate`,{method:"POST",body:JSON.stringify(body)})))}</pre>`;}; $("saveSandboxProfile").onclick=async()=>{const body=readSandboxProfileEditor(); if(!body.name) return toast(t("profileName")); $("sandboxOut").innerHTML=`<pre>${esc(pretty(await api(`/api/v1/admin/sandbox/profiles/${encodeURIComponent(body.name)}`,{method:"PUT",body:JSON.stringify(body)})))}</pre>`; render();}; }
