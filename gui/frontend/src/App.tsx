import { useEffect, useState, useRef, useMemo } from 'react';
import './App.css';
import { appVersion, buildNumber } from './version';
import appIcon from './assets/images/maclaw2.png';
import qianxinIcon from './assets/images/qianxin.png';
import claudecodeIcon from './assets/images/claudecode.png';
import codebuddyIcon from './assets/images/Codebuddy.png';
import codexIcon from './assets/images/Codex.png';
import geminiIcon from './assets/images/gemincli.png';
import iflowIcon from './assets/images/iflow.png';
import opencodeIcon from './assets/images/opencode.png';
import kiloIcon from './assets/images/KiloCode.png';
import cursorIcon from './assets/images/qodercli.png';
import lobsterOffline from './assets/images/lobster_offline.svg';
import lobsterHalf from './assets/images/lobster_half.svg';
import agentnetIcon from './assets/images/clawnet.svg';
import { CheckToolsStatus, InstallTool, InstallToolOnDemand, IsToolBeingInstalled, LoadConfig, SaveConfig, CheckEnvironment, ResizeWindow, WindowHide, LaunchTool, SelectProjectDir, SetLanguage, GetUserHomeDir, CheckUpdate, ShowMessage, ReadBBS, ReadTutorial, ReadThanks, ListPythonEnvironments, PackLog, ShowItemInFolder, OpenFileOrShowInFolder, GetSystemInfo, OpenSystemUrl, DownloadUpdate, CancelDownload, LaunchInstallerAndExit, ListSkills, ListSkillsWithInstallStatus, AddSkill, DeleteSkill, SelectSkillFile, GetSkillsDir, SetEnvCheckInterval, GetEnvCheckInterval, ShouldCheckEnvironment, UpdateLastEnvCheckTime, InstallDefaultMarketplace, InstallSkill, IsWindowsTerminalAvailable, ListRemoteHubs, PingMaclawLLM, AgentNetIsRunning, AgentNetEnsureDaemonWithDownload, AgentNetStopDaemon, GetQQBotStatus, RestartQQBot, GetTelegramStatus, RestartTelegram, GetWeixinStatus, RestartWeixin, StopWeixin, StartWeixinQRLogin, WaitWeixinQRLogin, GetWeixinLocalMode, SetWeixinLocalMode, GetQQBotLocalMode, SetQQBotLocalMode, GetTelegramLocalMode, SetTelegramLocalMode, GetLansengerStatus, RestartLansenger, StopLansenger, GetLansengerLocalMode, SetLansengerLocalMode, IsGossipAllowed, GetBrandInfo, GetUIZoomFactor, SetUIZoomFactor, GetAllLLMTokenUsage, GetMaclawLLMProviders, ListScheduledTasks, ListBackgroundLoops, MaximiseAndSaveGeometry, RestoreWindowGeometry, ListToolProviders } from "../wailsjs/go/main/App";
import { EventsOn, EventsOff, BrowserOpenURL, Quit, WindowFullscreen, WindowUnfullscreen } from "../wailsjs/runtime";
import { main } from "../wailsjs/go/models";
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';
import { RemoteSettingsPanel } from './components/remote/RemoteSettingsPanel';
import { SecurityPolicyPanel } from './components/remote/SecurityPolicyPanel';
import { RemoteSessionList } from './components/remote/RemoteSessionList';
import { useRemotePanel } from './components/remote/useRemotePanel';
import { TERMINAL_SESSION_STATUSES } from './components/remote/types';
import { SkillsManagementPanel } from './components/remote/SkillsManagementPanel';
import { MCPManagementPanel } from './components/remote/MCPManagementPanel';
import { LLMConfigPanel } from './components/remote/LLMConfigPanel';
import { EmbeddingConfigPanel } from './components/remote/EmbeddingConfigPanel';
import { ASRConfigPanel } from './components/remote/ASRConfigPanel';
import { MaclawRolePanel } from './components/remote/MaclawRolePanel';
import { MemoryManagementPanel } from './components/remote/MemoryManagementPanel';
import { HubServiceRedeemPanel } from './components/remote/HubServiceRedeemPanel';
import { WebSearchConfigPanel } from './components/remote/WebSearchConfigPanel';
import { AgentNetPanel } from './components/remote/AgentNetPanel';
import { AgentNetTabContainer } from './components/remote/AgentNetTabContainer';
import { OnboardingWizard } from './components/remote/OnboardingWizard';
import { AIAssistantPanel } from './components/ai/AIAssistantPanel';
import { AboutPanel } from './components/AboutPanel';
import { useAIAssistant } from './components/ai/useAIAssistant';
import { GossipPanel } from './components/gossip/GossipPanel';
import { useDialog } from './components/CustomDialog';
import { useToast } from './components/Toast';
import { QRCodeSVG } from 'qrcode.react';

const subscriptionUrls: { [key: string]: string } = {
    "GLM": "https://bigmodel.cn/glm-coding",
    "Kimi": "https://www.kimi.com/membership/pricing?from=upgrade_plan&track_id=1d2446f5-f45f-4ae5-961e-c0afe936a115",
    "Doubao": "https://www.volcengine.com/activity/codingplan",
    "腾讯云": "https://cloud.tencent.com/document/product/1772/128947",
    "讯飞星辰": "https://www.xfyun.cn/doc/spark/CodingPlan.html",
    "MiniMax": "https://platform.minimaxi.com/user-center/payment/coding-plan",
    "百度千帆": "https://cloud.baidu.com/product/codingplan.html",
    "Codex": "https://www.aicodemirror.com/register?invitecode=CZPPWZ",
    "Gemini": "https://www.aicodemirror.com/register?invitecode=CZPPWZ",
    "DeepSeek": "https://platform.deepseek.com/api_keys",
    "ChatFire": "https://api.chatfire.cn/register?aff=jira",
    "XiaoMi": "https://platform.xiaomimimo.com/#/console/api-keys",
    "摩尔线程": "https://code.mthreads.com/",
    "快手": "https://www.streamlake.com/marketing/coding-plan",
    "阿里云": "https://coding.dashscope.aliyuncs.com/"
};

// Known provider API endpoints database
// Organized by protocol type: anthropic (Claude), gemini, openai (Codex)
interface ProviderEndpoint {
    name: string;
    url: string;
    protocol: 'anthropic' | 'gemini' | 'openai';
    region: 'china' | 'global';
    description?: string;
}

interface RemoteCenterHubOption {
    hub_id: string;
    name: string;
    base_url: string;
    pwa_url?: string;
    visibility?: string;
    enrollment_mode?: string;
    status?: string;
}

interface SidebarTokenUsageStat {
    input_tokens?: number;
    output_tokens?: number;
    total_tokens?: number;
    InputTokens?: number;
    OutputTokens?: number;
    TotalTokens?: number;
}

const sidebarProviderAliases: Record<string, string[]> = {
    "智谱龙虾": ["智谱", "GLM(智谱)", "GLM (智谱)"],
    "智谱": ["智谱龙虾", "GLM(智谱)", "GLM (智谱)"],
    "GLM(智谱)": ["智谱", "智谱龙虾", "GLM (智谱)"],
    "GLM (智谱)": ["智谱", "智谱龙虾", "GLM(智谱)"],
};

const PROJECT_PAGE_SIZE = 5;

const knownProviderEndpoints: ProviderEndpoint[] = [
    // Anthropic Protocol (Claude)
    { name: "Claude Official", url: "https://api.anthropic.com/v1", protocol: "anthropic", region: "global", description: "Official Claude API" },
    { name: "MiniMax", url: "https://api.minimaxi.com/anthropic", protocol: "anthropic", region: "china" },
    { name: "DeepSeek", url: "https://api.deepseek.com/anthropic", protocol: "anthropic", region: "china" },
    { name: "腾讯云", url: "https://api.lkeap.cloud.tencent.com/coding/anthropic", protocol: "anthropic", region: "china", description: "Tencent Cloud Claude-compatible endpoint" },
    { name: "ChatFire", url: "https://api.chatfire.cn/v1", protocol: "anthropic", region: "china" },
    { name: "OpenRouter", url: "https://openrouter.ai/api", protocol: "anthropic", region: "global" },
    
    // Gemini Protocol
    { name: "Google Gemini Official", url: "https://generativelanguage.googleapis.com/v1beta", protocol: "gemini", region: "global", description: "Official Google Gemini API" },

    // OpenAI Protocol (Codex)
    { name: "OpenAI Official", url: "https://api.openai.com/v1", protocol: "openai", region: "global", description: "Official OpenAI API" },
    { name: "xAI (Grok)", url: "https://api.x.ai/v1", protocol: "openai", region: "global", description: "xAI Grok API" },
    { name: "智谱龙虾", url: "https://open.bigmodel.cn/api/coding/paas/v4", protocol: "openai", region: "china" },
    { name: "智谱编程", url: "https://open.bigmodel.cn/api/anthropic", protocol: "anthropic", region: "china" },
    { name: "Kimi", url: "https://api.kimi.com/coding/v1", protocol: "openai", region: "china" },
    { name: "Doubao", url: "https://ark.cn-beijing.volces.com/api/coding", protocol: "openai", region: "china" },
    { name: "腾讯云", url: "https://api.lkeap.cloud.tencent.com/coding/v3", protocol: "openai", region: "china", description: "Tencent Cloud OpenAI-compatible endpoint" },
    { name: "Doubao Codex", url: "https://ark.cn-beijing.volces.com/api/coding/v3", protocol: "openai", region: "china" },
    { name: "DeepSeek Codex", url: "https://api.aicodemirror.com/api/codex/backend-api/codex", protocol: "openai", region: "china" },
    { name: "OpenRouter", url: "https://openrouter.ai/api/v1", protocol: "openai", region: "global" },
    { name: "Together AI", url: "https://api.together.xyz/v1", protocol: "openai", region: "global" },
    { name: "Groq", url: "https://api.groq.com/openai/v1", protocol: "openai", region: "global" },
    { name: "Perplexity", url: "https://api.perplexity.ai", protocol: "openai", region: "global" },
];



// Recommended model IDs per provider (used for model name suggestions)
const recommendedModels: { [provider: string]: { id: string; note?: string }[] } = {
    "GLM": [{ id: "glm-5-turbo" }, { id: "glm-5.1" }, { id: "glm-4.7" }],
    "智谱龙虾": [{ id: "glm-5-turbo" }, { id: "glm-5.1" }, { id: "glm-4.7" }],
    "智谱编程": [{ id: "glm-5.1" }, { id: "glm-4.7" }],
    "Kimi": [{ id: "kimi-k2-thinking" }, { id: "kimi-for-coding" }],
    "Doubao": [{ id: "doubao-seed-code-preview-latest" }],
    "MiniMax": [{ id: "MiniMax-M2.1" }],
    "DeepSeek": [{ id: "deepseek-chat" }],
    "ChatFire": [{ id: "sonnet" }, { id: "gpt-5.1-codex-mini" }, { id: "gpt-4o" }, { id: "gemini-2.5-pro" }],
    "XiaoMi": [{ id: "mimo-v2-flash" }],
    "摩尔线程": [{ id: "GLM-4.7" }],
    "快手": [{ id: "kat-coder-pro-v1" }],
    "腾讯云": [
        { id: "glm-5", note: "默认" },
        { id: "tc-code-latest", note: "Auto" },
        { id: "hunyuan-2.0-instruct" },
        { id: "hunyuan-2.0-thinking" },
        { id: "hunyuan-t1" },
        { id: "hunyuan-turbos" },
        { id: "minimax-m2.5" },
        { id: "kimi-k2.5" },
    ],
    "阿里云": [
        { id: "qwen3.5-plus", note: "支持图片理解" },
        { id: "kimi-k2.5", note: "支持图片理解" },
        { id: "glm-5" },
        { id: "MiniMax-M2.5" },
        { id: "qwen3-max-2026-01-23" },
        { id: "qwen3-coder-next" },
        { id: "qwen3-coder-plus" },
        { id: "glm-4.7" },
    ],
};

// Tool name constants to avoid repeated string arrays
const TOOL_NAMES = ['claude', 'gemini', 'codex', 'opencode', 'codebuddy', 'cursor', 'iflow', 'kilo'] as const;
const SKILL_TOOLS = ['claude', 'gemini', 'codex'] as const;
const isToolTab = (tab: string): boolean => (TOOL_NAMES as readonly string[]).includes(tab);
const isSkillTool = (tab: string): boolean => (SKILL_TOOLS as readonly string[]).includes(tab);

// Shared badge style for model buttons
const badgeBaseStyle: React.CSSProperties = {
    position: 'absolute',
    top: '-8px',
    right: '0px',
    color: 'var(--theme-text-primary)',
    fontSize: '10px',
    padding: '1px 6px',
    borderRadius: '999px',
    fontWeight: 'bold',
    zIndex: 10,
    transform: 'scale(0.85)',
    boxShadow: '0 1px 4px rgba(0,0,0,0.15)',
    letterSpacing: '0.02em'
};

// Reusable markdown link component
const MarkdownLink = ({ node, ...props }: any) => (
    <a
        {...props}
        onClick={(e: React.MouseEvent) => {
            e.preventDefault();
            if (props.href) BrowserOpenURL(props.href);
        }}
        style={{ cursor: 'pointer', color: 'var(--theme-link-color)', textDecoration: 'underline' }}
    />
);

// Localized display names for providers that use non-English ModelName identifiers
const providerDisplayNames: { [lang: string]: { [key: string]: string } } = {
    "en": {
        "摩尔线程": "MooreThreads",
        "快手": "Kuaishou"
    },
    "zh-Hans": {
        "摩尔线程": "摩尔线程",
        "快手": "快手"
    },
    "zh-Hant": {
        "摩尔线程": "摩爾線程",
        "快手": "快手"
    }
};

// Get localized display name for a model, falling back to the raw name
const getModelDisplayName = (modelName: string, lang: string): string => {
    return providerDisplayNames[lang]?.[modelName] ?? providerDisplayNames["en"]?.[modelName] ?? modelName;
};

const translations: any = {
    "en": {
        "title": "MaClaw",
        "about": "About",
        "help": "Help",
        "cs146s": "Course",
        "introVideo": "Beginner",
        "thanks": "Thanks",
        "hide": "Hide",
        "launch": "Start Coding",
        "project": "Project",
        "projectDir": "Project Directory",
        "change": "Change",
        "browse": "Browse",
        "yoloMode": "Yolo Mode",
        "dangerouslySkip": "(Dangerously Skip Permissions)",
        "launchBtn": "Launch Tool",
        "modelSettings": "PROVIDER SETTINGS",
        "providerName": "Provider Name",
        "modelName": "Model ID",
        "apiKey": "API Key",
        "personalToken": "Personal Token",
        "getToken": "Get Token",
        "getKey": "Get API Key",
        "enterKey": "Enter API Key",
        "apiEndpoint": "API Endpoint",
        "saveChanges": "Save & Close",
        "saving": "Saving...",
        "saved": "Saved successfully!",
        "close": "Close",
        "manageProjects": "Projects",
        "projectManagement": "Project Management",
        "projectName": "Project Name",
        "delete": "Delete",
        "addNewProject": "+ Add New Project",
        "projectDirError": "Please set a valid Project Directory!",
        "initializing": "Initializing...",
        "loadingConfig": "Loading config...",
        "syncing": "Syncing...",
        "switched": "Provider switched & synced!",
        "projectSwitched": "Project switched!",
        "dirUpdated": "Directory updated!",
        "langName": "English",
        "custom": "Custom",
        "checkUpdate": "Check Update",
        "noUpdate": "No updates available",
        "updateAvailable": "Check for new version: ",
        "foundNewVersion": "Check for new version",
        "downloadNow": "Download Now",
        "paste": "Paste",
        "hideConfig": "Configure",
        "editConfig": "Configure",
        "settings": "Settings",
        "globalSettings": "Global Settings",
        "language": "Language",
        "runnerStatus": "Cur",
        "yoloModeLabel": "Yolo Mode",
        "adminModeLabel": "As Admin",
        "rootModeLabel": "As root",
        "teamModeLabel": "Team Mode",
        "pythonProjectLabel": "Python Project",
        "pythonEnvLabel": "Env",
        "customProviderPlaceholder": "Custom Provider Name",
        "addCustomProvider": "Add Custom Provider",
        "removeCustomProvider": "Remove This Provider",
        "maxCustomProviders": "Maximum 6 custom providers allowed",
        "cannotRemoveLastCustom": "Cannot remove the last custom provider",
        "version": "Version",
        "author": "Author",
        "aboutSectionTag": "ABOUT",
        "aboutProductName": "MaClaw Bedrock",
        "buildLabel": "Build",
        "quickActionsTitle": "Quick Actions",
        "quickActionsDesc": "Open official resources, check updates, or report issues.",
        "codeRepository": "Code Repository",
        "checkingUpdate": "Checking for updates...",
        "downloading": "Downloading...",
        "downloadCancelled": "Download cancelled",
        "downloadError": "Download error: {error}",
        "toolRepairTitle": "Installing Tool",
        "toolRepairInstalling": "Installing {tool}...",
        "toolRepairSuccess": "{tool} installed successfully!",
        "toolRepairFailed": "Failed to install {tool}",
        "toolRepairVersion": "Version: {version}",
        "installNow": "Install Now",
        "downloadAndUpdate": "Download and Update",
        "cancelDownload": "Cancel",
        "downloadComplete": "Download complete",
        "onlineUpdate": "Online Update",
        "retry": "Retry",
        "opencode": "OpenCode",
        "opencodeDesc": "OpenCode AI Programming Assistant",
        "codebuddy": "CodeBuddy",
        "codebuddyDesc": "CodeBuddy AI Assistant",
        "cursor": "Cursor Agent",
        "cursorDesc": "Cursor AI Programming Assistant",
        "iflow": "iFlow CLI",
        "iflowDesc": "iFlow AI Programming Assistant",
        "kilo": "Kilo Code CLI",
        "kiloDesc": "Kilo Code AI Programming Assistant",
        "bugReport": "Problem Feedback",
        "businessCooperation": "Contact: WeChat znsoft",
        "original": "Original",
        "message": "Message",
        "tutorial": "Tutorial",
        "gossip": "Gossip",
        "apiStore": "API Store",
        "relayService": "Relay",
        "getApiKey": "Get API Key",
        "subscription": "Monthly",
        "danger": "DANGER",
        "selectAll": "Select All",
        "copy": "Copy",
        "cut": "Cut",
        "contextPaste": "Paste",
        "forward": "Relay",
        "customized": "Custom",
        "originalFlag": "Native",
        "monthly": "Monthly",
        "premium": "Paid",
        "quickStart": "Tutorial",
        "manual": "Materials",
        "officialWebsite": "Official Website",
        "dontShowAgain": "Don't show again",
        "showWelcomePage": "Show Welcome Page",
        "refreshMessage": "Refresh",
        "refreshing": "🔄 Fetching latest messages...",
        "refreshSuccess": "✅ Refresh successful!",
        "refreshFailed": "❌ Refresh failed: ",
        "lastUpdate": "Last Update: ",
        "startupTitle": "Welcome to MaClaw",

        "showMore": "Show More",
        "showLess": "Show Less",
        "installLog": "View Log",
        "memoryHealth": "Memory Health",
        "securityEvents": "Security Events",
        "errorLog": "Error Log",
        "errorLogTitle": "Error Log",
        "errorLogEmpty": "No errors found in the log.",
        "memoryHealthTitle": "Memory Health Dashboard",
        "memHealthCapacity": "Capacity",
        "memHealthArchived": "Archived",
        "memHealthStale": "Stale",
        "memHealthOrphan": "Orphan (no links)",
        "memHealthNoEmbed": "No Embedding",
        "memHealthPinned": "Pinned",
        "memHealthVersioned": "With History",
        "memHealthEmbedder": "Embedder",
        "memHealthAvgAccess": "Avg Access",
        "memHealthOldest": "Oldest",
        "memHealthNewest": "Newest",
        "memHealthCategories": "Category Distribution",
        "memHealthUnavailable": "Memory system not initialized.",
        "loading": "Loading",
        "installLogTitle": "Installation Logs",
        "sendLog": "Send Log",
        "sendLogSubject": "MaClaw Environment Log",
        "confirmDelete": "Confirm Delete",
        "confirmDeleteMessage": "Are you sure you want to delete provider \"{name}\"?",
        "confirmSendLog": "Confirm Send",
        "confirmSendLogMessage": "No errors detected in logs. Send anyway?",
        "cancel": "Cancel",
        "confirm": "Confirm",
        "slogan": "Master your code, seize the machine.",
        "maclawLLMPopupTitle": "Let's get MaClaw ready!",
        "maclawLLMPopupDesc": "Two quick steps to unlock remote coding.",
        "maclawLLMStep1Title": "Configure LLM",
        "maclawLLMStep1Desc": "Connect an LLM provider so MaClaw can think.",
        "maclawLLMApplyLobster": "Get a free Lobster Plan",
        "maclawLLMGoSettings": "I have an API key",
        "maclawLLMStep2Title": "Register Remote & Bind Feishu",
        "maclawLLMStep2Desc": "Register your device and bind Feishu to enable remote control.",
        "maclawLLMGoRemote": "Go to Remote Settings",
        "maclawLLMReadyHint": "The AI assistant ring (left sidebar) lights up once you're all set.",
        "proxySettings": "Proxy",
        "proxyHost": "Proxy Host",
        "proxyPort": "Proxy Port",
        "proxyUsername": "Username (Optional)",
        "proxyPassword": "Password (Optional)",
        "proxyMode": "Proxy",
        "proxyNotConfigured": "Proxy not configured. Please configure proxy settings first.",
        "useDefaultProxy": "Use default proxy settings",
        "proxyHostPlaceholder": "e.g., 192.168.1.1 or proxy.company.com",
        "proxyPortPlaceholder": "e.g., 8080",
        "proxyProtocol": "Protocol",
        "proxyBypass": "Bypass List",
        "proxyBypassPlaceholder": "e.g., localhost;127.*;10.*;192.168.*",
        "proxyBypassHint": "Semicolon-separated. Addresses matching these patterns will bypass the proxy.",
        "proxyEnabled": "Enable Proxy",
        "proxyScopeMaclaw": "MacClaw (LLM API calls)",
        "proxyScopeCodingTools": "Coding Tools (macOS/Linux only)",
        "proxyScopeAgent": "Agent (web_search / web_fetch)",
        "proxyScopeTitle": "Proxy Scope",
        "remoteControl": "Remote Control",
        "remoteControlDesc": "Configure MaClaw remote diagnostics, Hub connection, and remote session control.",
        "remoteRefresh": "Refresh",
        "remoteRunReadiness": "Run Readiness",
        "remoteRunConpty": "Run ConPTY Probe",
        "remoteRunLaunchProbe": "Run {tool} Launch Probe",
        "remoteRunFullSmoke": "Run Full Smoke",
        "remoteActivation": "Registration",
        "remoteActivated": "Registered",
        "remoteNotActivated": "Not Registered",
        "remoteRegister": "Register",
        "remoteEmailNotConfigured": "Remote email not configured",
        "remoteHub": "Hub",
        "remoteConnected": "Connected",
        "remoteDisconnected": "Disconnected",
        "remoteNoHubUrl": "No hub URL",
        "remoteReadiness": "Readiness",
        "remoteReady": "Ready",
        "remoteNeedsAttention": "Needs Attention",
        "remoteNotRun": "Not Run",
        "remoteLaunch": "Launch",
        "remotePassed": "Passed",
        "remoteFailed": "Failed",
        "remoteSmoke": "Smoke",
        "remoteRouting": "Remote Routing",
        "remoteEnableLaunchPath": "Enable remote launch path",
        "remoteHubUrl": "Hub URL",
        "remoteHubCenterUrl": "Hub Center URL",
        "remoteEmail": "Remote Email",
        "remoteBindEmail": "Bind Email",
        "remoteNotInstalled": "Not installed",
        "remoteActivating": "Registering...",
        "remoteActivate": "Register Remote",
        "remoteStarting": "Starting...",
        "remoteStartTool": "Start Remote",
        "remoteStopTool": "Stop Remote",
        "remoteUnavailable": "Unavailable: {reason}",
        "remoteInstallingTool": "Installing {tool}...",
        "remoteInstallTool": "Install {tool}",
        "remoteReconnecting": "Reconnecting...",
        "remoteReconnectHub": "Reconnect Hub",
        "remoteClearing": "Clearing...",
        "remoteClearActivation": "Clear Registration",
        "remoteReRegister": "Re-register",
        "remoteToolPath": "Tool path",
        "remoteNextStep": "Next Step",
        "remoteLaunchProject": "Launch project",
        "remoteNoProjectSelected": "No project selected",
        "remoteReadinessWarnings": "Readiness Warnings",
        "remoteNoReadinessIssues": "No readiness issues detected.",
        "remoteProbeNotRun": "Probe not run yet.",
        "remoteConptyAvailable": "ConPTY available for remote {tool} sessions.",
        "remoteConptyUnavailable": "ConPTY unavailable.",
        "remoteLaunchProbeTitle": "{tool} Launch Probe",
        "remoteLaunchProbePending": "{tool} launch probe pending",
        "remoteCommandReady": "Command ready: {value}",
        "remoteLaunchProbeFailed": "Launch probe failed",
        "remoteFullSmoke": "Full Smoke",
        "remoteFullSmokeNotRun": "Full smoke has not been run yet.",
        "remoteTool": "Tool",
        "remoteProviderLabel": "Provider",
        "remoteProviderDefault": "Default",
        "remotePty": "PTY",
        "remoteSupported": "Supported",
        "remoteUnavailableShort": "Unavailable",
        "remoteSession": "Session",
        "remoteHubVisibility": "Hub Visibility",
        "remoteVerified": "Verified",
        "remoteNotVerified": "Not verified",
        "remoteNoImportantEvents": "No important events yet.",
        "remoteSendInstructionPlaceholder": "Send a remote instruction...",
        "remoteSend": "Send",
        "remoteDiagnosticsTitle": "Diagnostics",
        "remoteManagedSessions": "Managed Remote Sessions",
        "remoteManagedSessionsDesc": "Desktop-hosted remote sessions ready for Hub and PWA control.",
        "remoteActiveRecords": "{count} active record(s)",
        "remoteNoSessions": "No remote sessions yet. Start a remote tool with remote mode enabled to populate this panel.",
        "remoteCurrentTask": "Current Task",
        "remoteLastResult": "Last Result",
        "remoteProgress": "Progress",
        "remoteRecentActivity": "Recent Activity",
        "remoteEvent": "Event",
        "remoteStatusUnknown": "unknown",
        "remoteSeverityInfo": "info",
        "remoteToolInstalled": "{tool} installed",
        "remoteAlreadyInstalled": "{tool} is already installed",
        "remoteOpenAICompat": "OpenAI Compat",
        "remoteNativeProtocol": "Native Protocol",
        "remoteSessionConfig": "Session Config",
        "remoteStatelessLaunch": "Stateless Launch",
        "remoteProxyAware": "Proxy Aware",
        "remoteNoProxySupport": "No Proxy Support",
        "remoteInterrupt": "Interrupt",
        "remoteInterruptSent": "Interrupt sent",
        "remoteInterruptFailed": "Interrupt failed: {error}",
        "remoteKillSession": "Kill Session",
        "remoteKillSent": "Kill signal sent",
        "remoteKillFailed": "Kill failed: {error}",
        "remoteReadinessFailed": "Remote readiness failed: {error}",
        "remoteConptyFailed": "ConPTY probe failed: {error}",
        "remoteLaunchProbeFailedToast": "{tool} launch probe failed: {error}",
        "remoteSmokeCompleted": "Remote {tool} smoke completed",
        "remoteSmokeFailed": "Remote {tool} smoke failed: {error}",
        "remoteEmailRequired": "Remote email is required",
        "remoteServerRequired": "Remote server is required",
        "remoteActivateFirst": "Please register remote access first",
        "remoteActivationDialogTitle": "Register Remote",
        "remoteActivationDialogDesc": "Enter a Hub URL directly, or load your registered Hubs from HubCenter and choose one.",
        "remoteActivateAndLaunch": "Register and Launch",
        "remoteLoadRegisteredHubs": "Load Registered Hubs",
        "remoteLoadingRegisteredHubs": "Loading Hubs...",
        "remoteSelectRegisteredHub": "Registered Hub",
        "remoteNoRegisteredHubs": "No registered Hubs found",
        "remoteLoadHubListFailed": "Failed to load Hub list: {error}",
        "remoteHubManualOrSelect": "You can paste a Hub URL directly, or pick one from the HubCenter list above.",
        "remoteActivationCompleted": "Remote registration completed",
        "remoteActivationFailed": "Remote registration failed: {error}",
        "remoteReconnectFailed": "Reconnect failed: {error}",
        "remoteSelectProjectFirst": "Please select a launch project first",
        "remoteStartFailed": "Start failed: {error}",
        "remoteInstallFailed": "Install failed: {error}",
        "remoteSaveFailed": "Save failed: {error}",
        "remoteSendFailed": "Send failed: {error}",
        "remoteActivationCleared": "Remote registration cleared",
        "remoteClearFailed": "Clear failed: {error}",
        "remoteActivateStep": "Register Remote",
        "remoteActivateStepDesc": "Register the selected email and machine before starting remote sessions.",
        "remoteReconnectStep": "Reconnect Hub",
        "remoteReconnectStepDesc": "Hub URL is configured but the connection is currently offline.",
        "remoteConfigureModelStep": "Configure model",
        "remoteConfigureModelStepDesc": "Open the provider settings and save an API key plus model before launching remotely.",
        "remoteRunReadinessStep": "Run Readiness",
        "remoteRunReadinessStepDesc": "Collect the latest diagnostics for the selected tool and project.",
        "remoteRunReadinessAgain": "Run Readiness Again",
        "remoteRunReadinessAgainDesc": "Refresh diagnostics after fixing setup issues.",
        "remoteModeLabel": "Remote",
        "remoteModeDesc": "Start this tool through Hub for phone control",
        "localModeLabel": "Local",
        "launchModeLabel": "Mode",
        "defaultLaunchModeLabel": "Default Launch Mode",
        "defaultLaunchModeDesc": "Choose the default working mode for the tool launch area",
        "uiModeLabel": "Interface Mode",
        "uiModePro": "Pro",
        "uiModeLite": "Lite",
        "uiModeProDesc": "Full coding toolchain for developers",
        "uiModeLiteDesc": "AI assistant & skill extensions, coding tools hidden",
        "freeload": "Free",
        "bigSpender": "Big Spender",
        "skills": "Skills",
        "addSkill": "Add Skill",
        "skillName": "Skill Name",
        "skillDesc": "Description",
        "skillType": "Type",
        "skillAddress": "Skill ID",
        "skillZip": "Zip Package",
        "skillPath": "Path",
        "skillValue": "Value/Path",
        "skillAdded": "Skill added successfully",
        "skillDeleted": "Skill deleted successfully",
        "confirmDeleteSkill": "Are you sure you want to delete this skill?",
        "noSkills": "No skills added yet.",
        "installSkills": "Install Skills",
        "installLocation": "Install Location:",
        "userLocation": "User",
        "projectLocation": "Project",
        "selectSkillsToInstall": "Skill Installation",
        "installDefaultMarketplace": "Install Default Marketplace",
        "install": "Install",
        "installed": "Installed",
        "installing": "Installing...",
        "installNotImplemented": "Installation functionality is not yet implemented.",
        "pauseEnvCheck": "Skip Env Check",
        "useWindowsTerminal": "Use Windows Terminal",
        "envCheckIntervalPrefix": "Every",
        "envCheckIntervalSuffix": "days, remind to check environment",
        "envCheckDueTitle": "Environment Check Reminder",
        "envCheckDueMessage": "It has been {days} days since the last environment check. Would you like to check now?",
        "recheckEnv": "Manual Check & Update Environment",
        "skillRequiredError": "Name and Address/Path are required!",
        "skillZipOnlyError": "Gemini and Codex only support zip package skills.",
        "skillAddError": "Error adding skill: {error}",
        "skillDeleteError": "Error deleting skill: {error}",
        "copyLog": "Copy Log",
        "logsCopied": "Logs copied to clipboard",
        "currentVersion": "Current Version",
        "latestVersion": "Latest Version",
        "foundNewVersionMsg": "New version found. Download and update now?",
        "packageUnavailable": "The installer package for this version has not been published yet. Please visit the release page to check, or try again later.",
        "visitReleasePage": "Visit Release Page",
        "isLatestVersion": "Already up to date",
        "billing": "Billing",
        "placeholderName": "e.g., Frontend Design",
        "placeholderDesc": "Description...",
        "placeholderAddress": "@anthropics/...",
        "placeholderZip": "Select .zip file",
        "cannotDeleteSystemSkill": "System skill package cannot be deleted.",
        "systemDefault": "System Default",
        "envCheckTitle": "MaClaw Environment Setup",
        "envCheckExitWarningTitle": "Warning: Exit During Environment Setup",
        "envCheckExitWarningMessage": "Exiting now will result in incomplete environment setup, and the application may not function properly.\n\nOnly exit in extreme cases (such as infinite loops or unresponsive behavior).\n\nAre you sure you want to exit?",
        "envCheckExitConfirm": "Yes, Exit",
        "envCheckExitCancel": "No, Continue Setup",
        "selectProvider": "Select Provider",
        "knownProviders": "Known Providers",
        "providerList": "Provider List",
        "selectProviderTitle": "Select API Provider",
        "chinaProviders": "China Providers",
        "globalProviders": "Global Providers",
        "allProviders": "All Providers",
        "filterByRegion": "Filter by Region",
        "projectSearch": "Search",
        "projectSearchPlaceholder": "Search by name or path",
        "projectSortDefault": "Default order",
        "projectSortNameAsc": "Name A-Z",
        "projectSortNameDesc": "Name Z-A",
        "projectSortPathAsc": "Path A-Z",
        "projectSortPathDesc": "Path Z-A",
        "projectNoResults": "No projects matched",
        "projectShowing": "Showing",
        "projectTotal": "total",
        "prevPage": "Prev",
        "nextPage": "Next",
        "mcpTabLocal": "Local (Stdio)",
        "mcpTabRemote": "Remote (HTTP)",
        "mcpLocalCount": "local MCP server(s)",
        "mcpImportJson": "Import JSON",
        "mcpAdd": "+ Add",
        "mcpLoading": "Loading...",
        "mcpNoLocalServers": "No local MCP Servers. Click \"+ Add\" or \"Import JSON\" to configure.",
        "mcpDisabled": "Disabled",
        "mcpRunning": "Running",
        "mcpNotRunning": "Not running",
        "mcpEnable": "Enable",
        "mcpDisable": "Disable",
        "mcpEdit": "Edit",
        "mcpDelete": "Delete",
        "mcpEnvVars": "Env vars",
        "mcpConfirmDelete": "Confirm Delete",
        "mcpConfirmDeleteLocal": "Are you sure you want to delete local MCP Server \"{name}\"?",
        "mcpDeleting": "Deleting...",
        "mcpImportJsonTitle": "Import JSON Config",
        "mcpImportJsonDesc": "Paste standard MCP JSON config, format like:",
        "mcpImportJsonPlaceholder": "Paste JSON config...",
        "mcpJsonFormatError": "JSON format error",
        "mcpJsonStructureError": "Invalid format, expected { mcpServers: { name: { command, args, env } } }",
        "mcpImporting": "Importing...",
        "mcpImport": "Import",
        "mcpRemoteImportJsonTitle": "Import Remote MCP Config",
        "mcpRemoteImportJsonDesc": "Paste MCP JSON config (supports Kiro / Cursor / Claude Desktop format):",
        "mcpRemoteJsonStructureError": "Invalid format, expected { mcpServers: { name: { url, headers } } } or { name: { endpoint_url } }",
        "mcpRemoteJsonMissingUrl": "Server \"{name}\" is missing url or endpoint_url",
        "mcpEditLocalServer": "Edit Local MCP Server",
        "mcpAddLocalServer": "Add Local MCP Server",
        "mcpNameLabel": "Name",
        "mcpNameRequired": "Name is required",
        "mcpCommandLabel": "Command",
        "mcpCommandRequired": "Command is required",
        "mcpArgsLabel": "Args (one per line)",
        "mcpEnvLabel": "Environment Variables",
        "mcpAddEnvVar": "+ Add Env Var",
        "mcpSubmitting": "Submitting...",
        "mcpSave": "Save",
        "mcpAutoStartOn": "Auto-start On",
        "mcpAutoStartOff": "Auto-start Off",
        "mcpAutoStartStatus": "Startup",
        "mcpAutoStartEnabled": "Auto-start enabled",
        "mcpAutoStartDisabled": "Auto-start disabled",
        "mcpAutoStartDisabledHint": "Enable this server before changing auto-start",
        "mcpAutoStartCheckbox": "Start automatically when the app launches",
        "mcpServersRegistered": "registered MCP server(s)",
        "mcpRegisterServer": "+ Register MCP Server",
        "mcpNoRemoteServers": "No registered MCP Servers",
        "mcpColName": "Name",
        "mcpColEndpoint": "Endpoint URL",
        "mcpColHealth": "Health",
        "mcpColTools": "Tools",
        "mcpColActions": "Actions",
        "mcpHealthy": "Healthy",
        "mcpSlow": "Slow",
        "mcpUnavailable": "Unavailable",
        "mcpChecking": "Checking…",
        "mcpNotChecked": "Not checked",
        "mcpCollapse": "Collapse",
        "mcpTools": "Tools",
        "mcpConfirmDeleteRemote": "Are you sure you want to unregister MCP Server \"{name}\"? This cannot be undone.",
        "mcpEditServer": "Edit MCP Server",
        "mcpRegisterServerTitle": "Register MCP Server",
        "mcpEndpointLabel": "Endpoint URL",
        "mcpEndpointRequired": "Endpoint URL is required",
        "mcpAuthType": "Auth Type",
        "mcpAuthNone": "None",
        "mcpAuthApiKey": "API Key",
        "mcpAuthBearer": "Bearer Token",
        "mcpEnterApiKey": "Enter API Key",
        "mcpEnterBearer": "Enter Bearer Token",
        "mcpCustomHeaders": "Custom Headers",
        "mcpAddHeader": "Add",
        "mcpNoCustomHeaders": "No custom headers",
        "mcpRegister": "Register",
        "mcpHealthRecord": "Health Check Record",
        "mcpHealthStatus": "Status",
        "mcpFailCount": "Fail count",
        "mcpLastCheck": "Last check",
        "mcpCheckNow": "Check Now",
        "mcpLoadingTools": "Loading tools...",
        "mcpToolList": "Tool List",
        "mcpNoDescription": "No description",
        "mcpNoTools": "No tools"
    },
    "zh-Hans": {
        "title": "码卡龙",
        "about": "关于",
        "help": "帮助",
        "manual": "文档指南",
        "cs146s": "在线课程",
        "introVideo": "入门视频",
        "thanks": "鸣谢",
        "hide": "隐藏",
        "launch": "开始编程",
        "project": "项目",
        "projectDir": "项目目录",
        "change": "更改",
        "browse": "浏览",
        "yoloMode": "Yolo 模式",
        "dangerouslySkip": "(危险：跳过权限检查)",
        "launchBtn": "启动工具",
        "modelSettings": "服务商配置",
        "providerName": "服务商名称",
        "modelName": "模型名称/ID",
        "apiKey": "API Key",
        "personalToken": "个人令牌",
        "getToken": "获取令牌",
        "getKey": "获取 API Key",
        "enterKey": "输入 API Key",
        "apiEndpoint": "API 端点",
        "saveChanges": "保存并关闭",
        "saving": "保存中...",
        "saved": "保存成功！",
        "close": "关闭",
        "manageProjects": "项目管理",
        "projectManagement": "项目管理",
        "projectName": "项目名称",
        "delete": "删除",
        "addNewProject": "+ 添加新项目",
        "projectDirError": "请设置有效的项目目录！",
        "initializing": "初始化中...",
        "loadingConfig": "加载配置中...",
        "syncing": "正在同步...",
        "switched": "服务商已切换并同步！",
        "projectSwitched": "项目已切换！",
        "dirUpdated": "目录已更新！",
        "langName": "简体中文",
        "custom": "自定义",
        "checkUpdate": "检查更新",
        "noUpdate": "无可用更新",
        "updateAvailable": "检查新版本: ",
        "foundNewVersion": "检查新版本",
        "downloadNow": "立即下载",
        "paste": "粘贴",
        "hideConfig": "配置",
        "editConfig": "配置",
        "settings": "设置",
        "globalSettings": "全局设置",
        "language": "界面语言",
        "runnerStatus": "环境",
        "yoloModeLabel": "Yolo 模式",
        "adminModeLabel": "管理员权限",
        "rootModeLabel": "Root 权限",
        "teamModeLabel": "团队模式",
        "pythonProjectLabel": "Python 项目",
        "pythonEnvLabel": "环境",
        "customProviderPlaceholder": "自定义服务商名称",
        "addCustomProvider": "添加自定义服务商",
        "removeCustomProvider": "删除此服务商",
        "maxCustomProviders": "最多只能添加6个自定义服务商",
        "cannotRemoveLastCustom": "不能删除最后一个自定义服务商",
        "version": "版本",
        "author": "作者",
        "aboutSectionTag": "关于",
        "aboutProductName": "码卡龙·磐石 MaClaw",
        "buildLabel": "构建",
        "quickActionsTitle": "快捷操作",
        "quickActionsDesc": "打开官网资源、检查更新或反馈问题。",
        "codeRepository": "代码仓库",
        "checkingUpdate": "正在检查更新...",
        "downloading": "正在下载...",
        "downloadCancelled": "下载已取消",
        "downloadError": "下载错误: {error}",
        "toolRepairTitle": "安装工具",
        "toolRepairInstalling": "正在安装 {tool}...",
        "toolRepairSuccess": "{tool} 安装成功！",
        "toolRepairFailed": "安装 {tool} 失败",
        "toolRepairVersion": "版本: {version}",
        "installNow": "立即安装",
        "downloadAndUpdate": "下载并更新",
        "cancelDownload": "取消下载",
        "downloadComplete": "下载完成",
        "onlineUpdate": "在线更新",
        "retry": "重试",
        "opencode": "OpenCode",
        "opencodeDesc": "OpenCode AI 辅助编程",
        "codebuddy": "CodeBuddy",
        "codebuddyDesc": "CodeBuddy 编程助手",
        "cursor": "Cursor Agent",
        "cursorDesc": "Cursor AI 辅助编程",
        "iflow": "iFlow CLI",
        "iflowDesc": "iFlow AI 辅助编程",
        "kilo": "Kilo Code CLI",
        "kiloDesc": "Kilo Code AI 辅助编程",
        "bugReport": "问题反馈",
        "businessCooperation": "联系信息：微信 znsoft",
        "original": "原厂",
        "message": "消息",
        "tutorial": "教程",
        "gossip": "八卦",
        "apiStore": "API商店",
        "relayService": "转发",
        "getApiKey": "获取API Key",
        "subscription": "包月",
        "danger": "危险",
        "selectAll": "全选",
        "copy": "复制",
        "cut": "剪切",
        "contextPaste": "粘贴",
        "refreshMessage": "刷新",
        "refreshing": "🔄 正在从服务器获取最新消息...",
        "refreshSuccess": "✅ 获取新消息成功",
        "refreshFailed": "❌ 刷新失败：",
        "lastUpdate": "最后更新：",
        "forward": "转发",
        "customized": "定制",
        "originalFlag": "原生",
        "monthly": "包月",
        "premium": "氪金",
        "quickStart": "新手教学",
        "officialWebsite": "官方网站",
        "dontShowAgain": "下次不再显示",
        "showWelcomePage": "显示欢迎页",
        "startupTitle": "欢迎使用码卡龙",
        "showMore": "更多",
        "showLess": "收起",
        "installLog": "查看日志",
        "memoryHealth": "记忆健康",
        "securityEvents": "安全事件",
        "errorLog": "错误日志",
        "errorLogTitle": "错误日志",
        "errorLogEmpty": "日志中未发现错误信息。",
        "memoryHealthTitle": "记忆健康仪表盘",
        "memHealthCapacity": "容量",
        "memHealthArchived": "已归档",
        "memHealthStale": "可能过时",
        "memHealthOrphan": "孤立（无关联）",
        "memHealthNoEmbed": "无向量",
        "memHealthPinned": "已固定",
        "memHealthVersioned": "有历史版本",
        "memHealthEmbedder": "向量引擎",
        "memHealthAvgAccess": "平均访问",
        "memHealthOldest": "最早",
        "memHealthNewest": "最新",
        "memHealthCategories": "分类分布",
        "memHealthUnavailable": "记忆系统未初始化。",
        "loading": "加载中",
        "installLogTitle": "环境检查与安装日志",
        "sendLog": "发送日志",
        "sendLogSubject": "MaClaw环境安装日志",
        "confirmDelete": "确认删除",
        "confirmDeleteMessage": "确定要删除服务商 \"{name}\" 吗？",
        "confirmSendLog": "确认发送",
        "confirmSendLogMessage": "日志中没有检测到错误，是否仍要发送日志？",
        "cancel": "取消",
        "confirm": "确定",
        "slogan": "让远程编程像品尝甜点一样丝滑。",
        "maclawLLMPopupTitle": "来，配置一下 MaClaw 吧",
        "maclawLLMPopupDesc": "两步开启远程编程。",
        "maclawLLMStep1Title": "配置 LLM",
        "maclawLLMStep1Desc": "连接 LLM 服务商，让 MaClaw 能思考。",
        "maclawLLMApplyLobster": "免费领取龙虾套餐",
        "maclawLLMGoSettings": "我已有 API Key",
        "maclawLLMStep2Title": "移动端注册 & 绑定飞书",
        "maclawLLMStep2Desc": "注册设备并绑定飞书，即可通过移动端操控。",
        "maclawLLMGoRemote": "前往远程设置",
        "maclawLLMReadyHint": "左侧 AI 助手圆圈全亮，说明一切就绪。",
        "proxySettings": "代理设置",
        "proxyHost": "代理主机",
        "proxyPort": "代理端口",
        "proxyUsername": "用户名 (可选)",
        "proxyPassword": "密码 (可选)",
        "proxyMode": "代理",
        "proxyNotConfigured": "代理未配置。请先配置代理设置。",
        "useDefaultProxy": "使用默认代理设置",
        "proxyHostPlaceholder": "例如：192.168.1.1 或 proxy.company.com",
        "proxyPortPlaceholder": "例如：8080",
        "proxyProtocol": "协议",
        "proxyBypass": "绕过地址",
        "proxyBypassPlaceholder": "例如：localhost;127.*;10.*;192.168.*",
        "proxyBypassHint": "分号分隔。匹配的地址将绕过代理。",
        "proxyEnabled": "启用代理",
        "proxyScopeMaclaw": "MacClaw（大模型 API 调用）",
        "proxyScopeCodingTools": "编程工具（仅 macOS/Linux 生效）",
        "proxyScopeAgent": "智能体（web_search / web_fetch）",
        "proxyScopeTitle": "使用范围",
        "remoteControl": "移动端注册",
        "remoteControlDesc": "配置 MaClaw 远程诊断、Hub 连接和远程会话控制。",
        "remoteRefresh": "刷新",
        "remoteRunReadiness": "运行就绪检查",
        "remoteRunConpty": "运行 ConPTY 检测",
        "remoteRunLaunchProbe": "运行 {tool} 启动探测",
        "remoteRunFullSmoke": "运行完整冒烟测试",
        "remoteActivation": "注册状态",
        "remoteActivated": "已注册",
        "remoteNotActivated": "未注册",
        "remoteRegister": "注册",
        "remoteEmailNotConfigured": "尚未配置远程邮箱",
        "remoteHub": "Hub 连接",
        "remoteConnected": "已连接",
        "remoteDisconnected": "未连接",
        "remoteNoHubUrl": "未配置 Hub 地址",
        "remoteReadiness": "就绪状态",
        "remoteReady": "已就绪",
        "remoteNeedsAttention": "需要处理",
        "remoteNotRun": "未运行",
        "remoteLaunch": "启动探测",
        "remotePassed": "通过",
        "remoteFailed": "失败",
        "remoteSmoke": "冒烟测试",
        "remoteRouting": "远程路由",
        "remoteEnableLaunchPath": "启用远程启动路径",
        "remoteHubUrl": "Hub 地址",
        "remoteHubCenterUrl": "Hub Center 地址",
        "remoteEmail": "远程邮箱",
        "remoteBindEmail": "绑定邮件",
        "remoteNotInstalled": "未安装",
        "remoteActivating": "注册中...",
        "remoteActivate": "注册移动端",
        "remoteStarting": "启动中...",
        "remoteStartTool": "启动远程",
        "remoteStopTool": "停止远程",
        "remoteUnavailable": "不可用：{reason}",
        "remoteInstallingTool": "正在安装 {tool}...",
        "remoteInstallTool": "安装 {tool}",
        "remoteReconnecting": "重连中...",
        "remoteReconnectHub": "重连 Hub",
        "remoteClearing": "清除中...",
        "remoteClearActivation": "清除注册状态",
        "remoteReRegister": "重新注册",
        "remoteToolPath": "工具路径",
        "remoteNextStep": "下一步",
        "remoteLaunchProject": "启动项目",
        "remoteNoProjectSelected": "未选择项目",
        "remoteReadinessWarnings": "就绪检查提示",
        "remoteNoReadinessIssues": "未检测到就绪问题。",
        "remoteProbeNotRun": "尚未运行检测。",
        "remoteConptyAvailable": "{tool} 远程会话已支持 ConPTY。",
        "remoteConptyUnavailable": "ConPTY 不可用。",
        "remoteLaunchProbeTitle": "{tool} 启动探测",
        "remoteLaunchProbePending": "{tool} 启动探测尚未运行",
        "remoteCommandReady": "命令已就绪：{value}",
        "remoteLaunchProbeFailed": "启动探测失败",
        "remoteFullSmoke": "完整冒烟测试",
        "remoteFullSmokeNotRun": "尚未运行完整冒烟测试。",
        "remoteTool": "工具",
        "remoteProviderLabel": "服务商",
        "remoteProviderDefault": "默认",
        "remotePty": "PTY",
        "remoteSupported": "支持",
        "remoteUnavailableShort": "不可用",
        "remoteSession": "会话",
        "remoteHubVisibility": "Hub 可见性",
        "remoteVerified": "已验证",
        "remoteNotVerified": "未验证",
        "remoteNoImportantEvents": "暂时没有重要事件。",
        "remoteSendInstructionPlaceholder": "向远程会话发送指令...",
        "remoteSend": "发送",
        "remoteInterrupt": "中断",
        "remoteInterruptSent": "已发送中断",
        "remoteInterruptFailed": "中断失败：{error}",
        "remoteKillSession": "结束会话",
        "remoteKillSent": "已发送结束信号",
        "remoteKillFailed": "结束失败：{error}",
        "remoteReadinessFailed": "远程就绪检查失败：{error}",
        "remoteConptyFailed": "ConPTY 检测失败：{error}",
        "remoteLaunchProbeFailedToast": "{tool} 启动探测失败：{error}",
        "remoteSmokeCompleted": "远程 {tool} 冒烟测试已完成",
        "remoteSmokeFailed": "远程 {tool} 冒烟测试失败：{error}",
        "remoteEmailRequired": "必须填写远程邮箱",
        "remoteServerRequired": "必须先配置远程服务器地址",
        "remoteActivateFirst": "请先完成移动端注册",
        "remoteActivationDialogTitle": "移动端注册",
        "remoteActivationDialogDesc": "你可以直接输入 Hub 地址，或者先从 HubCenter 加载已注册的 Hub 再选择一个。",
        "remoteActivateAndLaunch": "注册并启动",
        "remoteLoadRegisteredHubs": "加载已注册 Hub",
        "remoteLoadingRegisteredHubs": "正在加载 Hub...",
        "remoteSelectRegisteredHub": "已注册 Hub",
        "remoteNoRegisteredHubs": "没有可用的已注册 Hub",
        "remoteLoadHubListFailed": "加载 Hub 列表失败：{error}",
        "remoteHubManualOrSelect": "你可以直接粘贴 Hub 地址，也可以从上面的 HubCenter 列表中选择。",
        "remoteActivationCompleted": "移动端注册已完成",
        "remoteActivationFailed": "移动端注册失败：{error}",
        "remoteReconnectFailed": "重连失败：{error}",
        "remoteSelectProjectFirst": "请先选择一个启动项目",
        "remoteStartFailed": "启动失败：{error}",
        "remoteInstallFailed": "安装失败：{error}",
        "remoteSaveFailed": "保存失败：{error}",
        "remoteSendFailed": "发送失败：{error}",
        "remoteActivationCleared": "移动端注册状态已清除",
        "remoteClearFailed": "清除失败：{error}",
        "remoteActivateStep": "移动端注册",
        "remoteActivateStepDesc": "启动远程会话前，先登记邮箱和设备信息。",
        "remoteReconnectStep": "重连 Hub",
        "remoteReconnectStepDesc": "已配置 Hub 地址，但当前连接处于离线状态。",
        "remoteConfigureModelStep": "配置模型",
        "remoteConfigureModelStepDesc": "先打开服务商配置，保存 API Key 和模型后再远程启动。",
        "remoteRunReadinessStep": "运行就绪检查",
        "remoteRunReadinessStepDesc": "为当前工具和项目采集最新诊断信息。",
        "remoteRunReadinessAgain": "重新运行就绪检查",
        "remoteRunReadinessAgainDesc": "修复问题后重新刷新诊断结果。",
        "remoteModeLabel": "远程",
        "remoteModeDesc": "通过 Hub 启动此工具，便于手机控制",
        "localModeLabel": "本地",
        "launchModeLabel": "方式",
        "defaultLaunchModeLabel": "默认启动模式",
        "defaultLaunchModeDesc": "选择工具启动区的默认工作模式",
        "uiModeLabel": "界面模式",
        "uiModePro": "专业模式",
        "uiModeLite": "简洁模式",
        "uiModeProDesc": "包含完整编程工具链，适合开发者",
        "uiModeLiteDesc": "专注 AI 助手与技能扩展，隐藏编程工具",
        "freeload": "白嫖中",
        "bigSpender": "大力氪金",
        "skills": "技能",
        "addSkill": "添加技能",
        "skillName": "技能名称",
        "skillDesc": "描述",
        "skillType": "类型",
        "skillAddress": "Skill ID",
        "skillZip": "Zip包",
        "skillPath": "路径",
        "skillValue": "值/路径",
        "skillAdded": "技能添加成功",
        "skillDeleted": "技能删除成功",
        "confirmDeleteSkill": "确定要删除此技能吗？",
        "noSkills": "暂无技能。",
        "installSkills": "安装技能",
        "installLocation": "安装位置:",
        "userLocation": "用户",
        "projectLocation": "项目",
        "selectSkillsToInstall": "技能安装",
        "installDefaultMarketplace": "安装默认市场",
        "install": "安装",
        "installed": "已安装",
        "installing": "正在安装...",
        "installNotImplemented": "安装功能暂未实现。",
        "pauseEnvCheck": "跳过环境检测",
        "useWindowsTerminal": "使用 Windows Terminal",
        "envCheckIntervalPrefix": "每隔",
        "envCheckIntervalSuffix": "日提醒检测环境",
        "envCheckDueTitle": "环境检测提醒",
        "envCheckDueMessage": "距离上次环境检测已过{days}天，是否现在检测？",
        "recheckEnv": "手动检测更新运行环境",
        "skillRequiredError": "名称和地址/路径是必填项！",
        "skillZipOnlyError": "Gemini 和 Codex 仅支持 Zip 包式技能。",
        "skillAddError": "添加技能出错: {error}",
        "skillDeleteError": "删除技能出错: {error}",
        "copyLog": "复制日志",
        "logsCopied": "日志已复制到剪贴板",
        "currentVersion": "当前版本",
        "latestVersion": "最新版本",
        "foundNewVersionMsg": "检查到新版本，是否立即下载更新？",
        "packageUnavailable": "该版本的安装包尚未发布，请前往发布页面查看，或稍后再试。",
        "visitReleasePage": "前往发布页面",
        "isLatestVersion": "已是最新版本",
        "billing": "计费",
        "placeholderName": "例如：前端设计",
        "placeholderDesc": "描述...",
        "placeholderAddress": "@anthropics/...",
        "placeholderZip": "选择 .zip 文件",
        "cannotDeleteSystemSkill": "系统技能包不能删除。",
        "systemDefault": "系统默认",
        "envCheckTitle": "MaClaw 运行环境检测安装",
        "envCheckExitWarningTitle": "警告：退出环境安装",
        "envCheckExitWarningMessage": "退出将导致环境安装不完整，程序无法正常运行。\n\n只有在程序死循环等极端情况下才建议退出。\n\n确定要退出吗？",
        "envCheckExitConfirm": "是的，退出",
        "envCheckExitCancel": "否，继续安装",
        "selectProvider": "选择服务商",
        "knownProviders": "已知服务商",
        "providerList": "服务商列表",
        "selectProviderTitle": "选择 API 服务商",
        "chinaProviders": "国内服务商",
        "globalProviders": "国外服务商",
        "allProviders": "全部服务商",
        "filterByRegion": "按地区筛选",
        "projectSearch": "搜索",
        "projectSearchPlaceholder": "按名称或路径搜索",
        "projectSortDefault": "默认顺序",
        "projectSortNameAsc": "名称 A-Z",
        "projectSortNameDesc": "名称 Z-A",
        "projectSortPathAsc": "路径 A-Z",
        "projectSortPathDesc": "路径 Z-A",
        "projectNoResults": "没有匹配的项目",
        "projectShowing": "显示",
        "projectTotal": "总计",
        "prevPage": "上一页",
        "nextPage": "下一页",
        "mcpTabLocal": "本地 (Stdio)",
        "mcpTabRemote": "远程 (HTTP)",
        "mcpLocalCount": "个本地 MCP Server",
        "mcpImportJson": "导入 JSON",
        "mcpAdd": "+ 添加",
        "mcpLoading": "加载中...",
        "mcpNoLocalServers": "暂无本地 MCP Server，点击「+ 添加」或「导入 JSON」来配置",
        "mcpDisabled": "已禁用",
        "mcpRunning": "运行中",
        "mcpNotRunning": "未运行",
        "mcpEnable": "启用",
        "mcpDisable": "禁用",
        "mcpEdit": "编辑",
        "mcpDelete": "删除",
        "mcpEnvVars": "环境变量",
        "mcpConfirmDelete": "确认删除",
        "mcpConfirmDeleteLocal": "确定要删除本地 MCP Server「{name}」吗？",
        "mcpDeleting": "删除中...",
        "mcpImportJsonTitle": "导入 JSON 配置",
        "mcpImportJsonDesc": "粘贴标准 MCP JSON 配置，支持格式如：",
        "mcpImportJsonPlaceholder": "粘贴 JSON 配置...",
        "mcpJsonFormatError": "JSON 格式错误",
        "mcpJsonStructureError": "格式不正确，需要 { mcpServers: { name: { command, args, env } } }",
        "mcpImporting": "导入中...",
        "mcpImport": "导入",
        "mcpRemoteImportJsonTitle": "导入远程 MCP 配置",
        "mcpRemoteImportJsonDesc": "粘贴 MCP JSON 配置代码（支持 Kiro / Cursor / Claude Desktop 格式）：",
        "mcpRemoteJsonStructureError": "格式不正确，需要 { mcpServers: { name: { url, headers } } } 或 { name: { endpoint_url } }",
        "mcpRemoteJsonMissingUrl": "服务器「{name}」缺少 url 或 endpoint_url 字段",
        "mcpEditLocalServer": "编辑本地 MCP Server",
        "mcpAddLocalServer": "添加本地 MCP Server",
        "mcpNameLabel": "名称",
        "mcpNameRequired": "名称不能为空",
        "mcpCommandLabel": "命令 (command)",
        "mcpCommandRequired": "命令不能为空",
        "mcpArgsLabel": "参数 (args)，每行一个",
        "mcpEnvLabel": "环境变量 (env)",
        "mcpAddEnvVar": "+ 添加环境变量",
        "mcpSubmitting": "提交中...",
        "mcpSave": "保存",
        "mcpAutoStartOn": "开机自启开",
        "mcpAutoStartOff": "开机自启关",
        "mcpAutoStartStatus": "启动时",
        "mcpAutoStartEnabled": "启动时自动启动",
        "mcpAutoStartDisabled": "启动时不自动启动",
        "mcpAutoStartDisabledHint": "请先启用该服务，再设置自动启动",
        "mcpAutoStartCheckbox": "程序启动时自动启动该服务",
        "mcpServersRegistered": "个已注册 MCP Server",
        "mcpRegisterServer": "+ 注册 MCP Server",
        "mcpNoRemoteServers": "暂无已注册的 MCP Server",
        "mcpColName": "名称",
        "mcpColEndpoint": "端点 URL",
        "mcpColHealth": "健康状态",
        "mcpColTools": "工具数",
        "mcpColActions": "操作",
        "mcpHealthy": "健康",
        "mcpSlow": "缓慢",
        "mcpUnavailable": "不可用",
        "mcpChecking": "检测中…",
        "mcpNotChecked": "未检测",
        "mcpCollapse": "收起",
        "mcpTools": "工具",
        "mcpConfirmDeleteRemote": "确定要注销 MCP Server「{name}」吗？此操作不可撤销。",
        "mcpEditServer": "编辑 MCP Server",
        "mcpRegisterServerTitle": "注册 MCP Server",
        "mcpEndpointLabel": "端点 URL",
        "mcpEndpointRequired": "端点 URL 不能为空",
        "mcpAuthType": "认证方式",
        "mcpAuthNone": "无认证",
        "mcpAuthApiKey": "API Key",
        "mcpAuthBearer": "Bearer Token",
        "mcpEnterApiKey": "输入 API Key",
        "mcpEnterBearer": "输入 Bearer Token",
        "mcpCustomHeaders": "自定义 Headers",
        "mcpAddHeader": "添加",
        "mcpNoCustomHeaders": "无自定义 Headers",
        "mcpRegister": "注册",
        "mcpHealthRecord": "健康检查记录",
        "mcpHealthStatus": "状态",
        "mcpFailCount": "失败次数",
        "mcpLastCheck": "最近检查",
        "mcpCheckNow": "立即检查",
        "mcpLoadingTools": "加载工具列表...",
        "mcpToolList": "工具列表",
        "mcpNoDescription": "无描述",
        "mcpNoTools": "暂无工具"
    },
    "zh-Hant": {
        "title": "碼卡龍",
        "about": "關於",
        "help": "幫助",
        "manual": "文檔指南",
        "cs146s": "線上課程",
        "introVideo": "入門視頻",
        "thanks": "鳴謝",
        "hide": "隱藏",
        "launch": "開始編程",
        "project": "專案",
        "projectDir": "專案目錄",
        "change": "變更",
        "browse": "瀏覽",
        "yoloMode": "Yolo 模式",
        "dangerouslySkip": "(危險：跳過權限檢查)",
        "launchBtn": "啟動工具",
        "modelSettings": "服務商設定",
        "providerName": "服務商名稱",
        "modelName": "模型名稱/ID",
        "apiKey": "API Key",
        "personalToken": "個人令牌",
        "getToken": "獲取令牌",
        "getKey": "獲取 API Key",
        "enterKey": "輸入 API Key",
        "apiEndpoint": "API 端點",
        "saveChanges": "儲存並關閉",
        "saving": "儲存中...",
        "saved": "儲存成功！",
        "close": "關閉",
        "manageProjects": "專案管理",
        "projectManagement": "專案管理",
        "projectName": "專案名稱",
        "delete": "刪除",
        "addNewProject": "+ 新增專案",
        "projectDirError": "請設置有效的專案目錄！",
        "initializing": "初始化中...",
        "loadingConfig": "載入設定中...",
        "syncing": "正在同步...",
        "switched": "服務商已切換並同步！",
        "langName": "繁體中文",
        "custom": "自定義",
        "checkUpdate": "檢查更新",
        "noUpdate": "無可用更新",
        "updateAvailable": "發現新版本: ",
        "foundNewVersion": "發現新版本",
        "downloadNow": "立即下載",
        "paste": "貼上",
        "hideConfig": "配置",
        "editConfig": "配置",
        "settings": "設置",
        "globalSettings": "全局設置",
        "language": "界面語言",
        "runnerStatus": "目前環境",
        "yoloModeLabel": "Yolo 模式",
        "adminModeLabel": "管理員權限",
        "rootModeLabel": "Root 權限",
        "teamModeLabel": "團隊模式",
        "pythonProjectLabel": "Python 項目",
        "pythonEnvLabel": "環境",
        "customProviderPlaceholder": "自定義服務商名稱",
        "addCustomProvider": "添加自定義服務商",
        "removeCustomProvider": "刪除此服務商",
        "maxCustomProviders": "最多只能添加6個自定義服務商",
        "cannotRemoveLastCustom": "不能刪除最後一個自定義服務商",
        "version": "版本",
        "author": "作者",
        "aboutSectionTag": "關於",
        "aboutProductName": "碼卡龍·磐石 MaClaw",
        "buildLabel": "構建",
        "quickActionsTitle": "快捷操作",
        "quickActionsDesc": "打開官網資源、檢查更新或反饋問題。",
        "codeRepository": "代碼倉庫",
        "checkingUpdate": "正在檢查更新...",
        "downloading": "正在下載...",
        "downloadCancelled": "下載已取消",
        "downloadError": "下載錯誤: {error}",
        "toolRepairTitle": "安裝工具",
        "toolRepairInstalling": "正在安裝 {tool}...",
        "toolRepairSuccess": "{tool} 安裝成功！",
        "toolRepairFailed": "安裝 {tool} 失敗",
        "toolRepairVersion": "版本: {version}",
        "installNow": "立即安裝",
        "downloadAndUpdate": "下載並更新",
        "cancelDownload": "取消下載",
        "downloadComplete": "下載完成",
        "onlineUpdate": "線上更新",
        "retry": "重試",
        "opencode": "OpenCode",
        "opencodeDesc": "OpenCode AI 輔助編程",
        "codebuddy": "CodeBuddy",
        "codebuddyDesc": "CodeBuddy 編程助手",
        "cursor": "Cursor Agent",
        "cursorDesc": "Cursor AI 輔助編程",
        "iflow": "iFlow CLI",
        "iflowDesc": "iFlow AI 輔助編程",
        "kilo": "Kilo Code CLI",
        "kiloDesc": "Kilo Code AI 輔助編程",
        "bugReport": "問題反饋",
        "businessCooperation": "聯繫信息：微信 znsoft",
        "original": "原廠",
        "message": "消息",
        "tutorial": "教程",
        "gossip": "八卦",
        "apiStore": "API商店",
        "relayService": "轉發",
        "getApiKey": "獲取API Key",
        "subscription": "包月",
        "danger": "危險",
        "selectAll": "全選",
        "copy": "複製",
        "cut": "剪切",
        "contextPaste": "粘貼",
        "refreshMessage": "刷新",
        "refreshing": "🔄 正在从服务器获取最新消息...",
        "refreshSuccess": "✅ 獲取新消息成功",
        "refreshFailed": "❌ 刷新失敗：",
        "lastUpdate": "最後更新：",
        "forward": "轉發",
        "customized": "定制",
        "originalFlag": "原生",
        "monthly": "包月",
        "premium": "氪金",
        "quickStart": "新手教學",
        "officialWebsite": "官方網站",
        "dontShowAgain": "下次不再顯示",
        "showWelcomePage": "顯示歡迎頁",
        "startupTitle": "歡迎使用碼卡龍",
        "showMore": "更多",
        "showLess": "收起",
        "installLog": "查看日誌",
        "memoryHealth": "記憶健康",
        "securityEvents": "安全事件",
        "errorLog": "錯誤日誌",
        "errorLogTitle": "錯誤日誌",
        "errorLogEmpty": "日誌中未發現錯誤信息。",
        "memoryHealthTitle": "記憶健康儀表盤",
        "memHealthCapacity": "容量",
        "memHealthArchived": "已歸檔",
        "memHealthStale": "可能過時",
        "memHealthOrphan": "孤立（無關聯）",
        "memHealthNoEmbed": "無向量",
        "memHealthPinned": "已固定",
        "memHealthVersioned": "有歷史版本",
        "memHealthEmbedder": "向量引擎",
        "memHealthAvgAccess": "平均訪問",
        "memHealthOldest": "最早",
        "memHealthNewest": "最新",
        "memHealthCategories": "分類分佈",
        "memHealthUnavailable": "記憶系統未初始化。",
        "loading": "載入中",
        "installLogTitle": "環境檢查與安裝日誌",
        "sendLog": "發送日誌",
        "sendLogSubject": "MaClaw環境安裝日誌",
        "confirmDelete": "確認刪除",
        "confirmDeleteMessage": "確定要刪除服務商 \"{name}\" 嗎？",
        "confirmSendLog": "確認發送",
        "confirmSendLogMessage": "日誌中沒有檢測到錯誤，是否仍要發送日誌？",
        "cancel": "取消",
        "confirm": "確定",
        "slogan": "讓遠程編程像品嚐甜點一樣絲滑。",
        "maclawLLMPopupTitle": "來，配置一下 MaClaw 吧",
        "maclawLLMPopupDesc": "兩步開啟遠端編程。",
        "maclawLLMStep1Title": "配置 LLM",
        "maclawLLMStep1Desc": "連接 LLM 服務商，讓 MaClaw 能思考。",
        "maclawLLMApplyLobster": "免費領取龍蝦套餐",
        "maclawLLMGoSettings": "我已有 API Key",
        "maclawLLMStep2Title": "行動端註冊 & 綁定飛書",
        "maclawLLMStep2Desc": "註冊裝置並綁定飛書，即可透過行動端操控。",
        "maclawLLMGoRemote": "前往遠端設定",
        "maclawLLMReadyHint": "左側 AI 助手圓圈全亮，說明一切就緒。",
        "proxySettings": "代理設置",
        "proxyHost": "代理主機",
        "proxyPort": "代理端口",
        "proxyUsername": "使用者名稱 (可選)",
        "proxyPassword": "密碼 (可選)",
        "proxyMode": "代理",
        "proxyNotConfigured": "代理未配置。請先配置代理設置。",
        "useDefaultProxy": "使用預設代理設置",
        "proxyHostPlaceholder": "例如：192.168.1.1 或 proxy.company.com",
        "proxyPortPlaceholder": "例如：8080",
        "proxyProtocol": "協議",
        "proxyBypass": "繞過地址",
        "proxyBypassPlaceholder": "例如：localhost;127.*;10.*;192.168.*",
        "proxyBypassHint": "分號分隔。匹配的地址將繞過代理。",
        "proxyEnabled": "啟用代理",
        "proxyScopeMaclaw": "MacClaw（大模型 API 調用）",
        "proxyScopeCodingTools": "編程工具（僅 macOS/Linux 生效）",
        "proxyScopeAgent": "智能體（web_search / web_fetch）",
        "proxyScopeTitle": "使用範圍",
        "remoteControl": "行動端註冊",
        "remoteControlDesc": "設定 MaClaw 遠端診斷、Hub 連線與遠端會話控制。",
        "remoteRefresh": "重新整理",
        "remoteRunReadiness": "執行就緒檢查",
        "remoteRunConpty": "執行 ConPTY 檢測",
        "remoteRunLaunchProbe": "執行 {tool} 啟動探測",
        "remoteRunFullSmoke": "執行完整冒煙測試",
        "remoteActivation": "註冊狀態",
        "remoteActivated": "已註冊",
        "remoteNotActivated": "未註冊",
        "remoteRegister": "註冊",
        "remoteEmailNotConfigured": "尚未設定遠端信箱",
        "remoteHub": "Hub 連線",
        "remoteConnected": "已連線",
        "remoteDisconnected": "未連線",
        "remoteNoHubUrl": "未設定 Hub 位址",
        "remoteReadiness": "就緒狀態",
        "remoteReady": "已就緒",
        "remoteNeedsAttention": "需要處理",
        "remoteNotRun": "未執行",
        "remoteLaunch": "啟動探測",
        "remotePassed": "通過",
        "remoteFailed": "失敗",
        "remoteSmoke": "冒煙測試",
        "remoteRouting": "遠端路由",
        "remoteEnableLaunchPath": "啟用遠端啟動路徑",
        "remoteHubUrl": "Hub 位址",
        "remoteHubCenterUrl": "Hub Center 位址",
        "remoteEmail": "遠端信箱",
        "remoteBindEmail": "綁定郵件",
        "remoteNotInstalled": "未安裝",
        "remoteActivating": "註冊中...",
        "remoteActivate": "註冊行動端",
        "remoteStarting": "啟動中...",
        "remoteStartTool": "啟動遠端",
        "remoteStopTool": "停止遠端",
        "remoteUnavailable": "不可用：{reason}",
        "remoteInstallingTool": "正在安裝 {tool}...",
        "remoteInstallTool": "安裝 {tool}",
        "remoteReconnecting": "重新連線中...",
        "remoteReconnectHub": "重新連線 Hub",
        "remoteClearing": "清除中...",
        "remoteClearActivation": "清除註冊狀態",
        "remoteReRegister": "重新註冊",
        "remoteToolPath": "工具路徑",
        "remoteNextStep": "下一步",
        "remoteLaunchProject": "啟動專案",
        "remoteNoProjectSelected": "未選擇專案",
        "remoteReadinessWarnings": "就緒檢查提示",
        "remoteNoReadinessIssues": "未檢測到就緒問題。",
        "remoteProbeNotRun": "尚未執行檢測。",
        "remoteConptyAvailable": "{tool} 遠端會話已支援 ConPTY。",
        "remoteConptyUnavailable": "ConPTY 不可用。",
        "remoteLaunchProbeTitle": "{tool} 啟動探測",
        "remoteLaunchProbePending": "{tool} 啟動探測尚未執行",
        "remoteCommandReady": "指令已就緒：{value}",
        "remoteLaunchProbeFailed": "啟動探測失敗",
        "remoteFullSmoke": "完整冒煙測試",
        "remoteFullSmokeNotRun": "尚未執行完整冒煙測試。",
        "remoteTool": "工具",
        "remoteProviderLabel": "服務商",
        "remoteProviderDefault": "預設",
        "remotePty": "PTY",
        "remoteSupported": "支援",
        "remoteUnavailableShort": "不可用",
        "remoteSession": "會話",
        "remoteHubVisibility": "Hub 可見性",
        "remoteVerified": "已驗證",
        "remoteNotVerified": "未驗證",
        "remoteNoImportantEvents": "暫時沒有重要事件。",
        "remoteSendInstructionPlaceholder": "向遠端會話傳送指令...",
        "remoteSend": "傳送",
        "remoteInterrupt": "中斷",
        "remoteInterruptSent": "已送出中斷",
        "remoteInterruptFailed": "中斷失敗：{error}",
        "remoteKillSession": "結束會話",
        "remoteKillSent": "已送出結束訊號",
        "remoteKillFailed": "結束失敗：{error}",
        "remoteReadinessFailed": "遠端就緒檢查失敗：{error}",
        "remoteConptyFailed": "ConPTY 檢測失敗：{error}",
        "remoteLaunchProbeFailedToast": "{tool} 啟動探測失敗：{error}",
        "remoteSmokeCompleted": "遠端 {tool} 冒煙測試已完成",
        "remoteSmokeFailed": "遠端 {tool} 冒煙測試失敗：{error}",
        "remoteEmailRequired": "必須填寫遠端信箱",
        "remoteServerRequired": "必須先設定遠端伺服器位址",
        "remoteActivateFirst": "請先完成行動端註冊",
        "remoteActivationDialogTitle": "行動端註冊",
        "remoteActivationDialogDesc": "你可以直接輸入 Hub 位址，或先從 HubCenter 載入已註冊的 Hub 再選擇一個。",
        "remoteActivateAndLaunch": "註冊並啟動",
        "remoteLoadRegisteredHubs": "載入已註冊 Hub",
        "remoteLoadingRegisteredHubs": "正在載入 Hub...",
        "remoteSelectRegisteredHub": "已註冊 Hub",
        "remoteNoRegisteredHubs": "沒有可用的已註冊 Hub",
        "remoteLoadHubListFailed": "載入 Hub 清單失敗：{error}",
        "remoteHubManualOrSelect": "你可以直接貼上 Hub 位址，也可以從上方的 HubCenter 清單中選擇。",
        "remoteActivationCompleted": "行動端註冊已完成",
        "remoteActivationFailed": "行動端註冊失敗：{error}",
        "remoteReconnectFailed": "重新連線失敗：{error}",
        "remoteSelectProjectFirst": "請先選擇一個啟動專案",
        "remoteStartFailed": "啟動失敗：{error}",
        "remoteInstallFailed": "安裝失敗：{error}",
        "remoteSaveFailed": "儲存失敗：{error}",
        "remoteSendFailed": "傳送失敗：{error}",
        "remoteActivationCleared": "行動端註冊狀態已清除",
        "remoteClearFailed": "清除失敗：{error}",
        "remoteActivateStep": "行動端註冊",
        "remoteActivateStepDesc": "啟動遠端會話前，先登記信箱與裝置資訊。",
        "remoteReconnectStep": "重新連線 Hub",
        "remoteReconnectStepDesc": "已設定 Hub 位址，但目前連線處於離線狀態。",
        "remoteConfigureModelStep": "設定模型",
        "remoteConfigureModelStepDesc": "先開啟服務商設定，儲存 API Key 與模型後再遠端啟動。",
        "remoteRunReadinessStep": "執行就緒檢查",
        "remoteRunReadinessStepDesc": "為目前工具與專案蒐集最新診斷資訊。",
        "remoteRunReadinessAgain": "重新執行就緒檢查",
        "remoteRunReadinessAgainDesc": "修復問題後重新整理診斷結果。",
        "remoteModeLabel": "遠端",
        "remoteModeDesc": "透過 Hub 啟動此工具，方便手機控制",
        "localModeLabel": "本機",
        "launchModeLabel": "方式",
        "defaultLaunchModeLabel": "預設啟動模式",
        "defaultLaunchModeDesc": "選擇工具啟動區的預設工作模式",
        "uiModeLabel": "介面模式",
        "uiModePro": "專業模式",
        "uiModeLite": "簡潔模式",
        "uiModeProDesc": "包含完整程式工具鏈，適合開發者",
        "uiModeLiteDesc": "專注 AI 助手與技能擴展，隱藏程式工具",
        "freeload": "白嫖中",
        "bigSpender": "大力氪金",
        "skills": "技能",
        "addSkill": "新增技能",
        "skillName": "技能名稱",
        "skillDesc": "描述",
        "skillType": "類型",
        "skillAddress": "Skill ID",
        "skillZip": "Zip包",
        "skillPath": "路徑",
        "skillValue": "值/路徑",
        "skillAdded": "技能新增成功",
        "skillDeleted": "技能刪除成功",
        "confirmDeleteSkill": "確定要刪除此技能嗎？",
        "noSkills": "暫無技能。",
        "installSkills": "安裝技能",
        "installLocation": "安裝位置:",
        "userLocation": "用戶",
        "projectLocation": "項目",
        "selectSkillsToInstall": "技能安裝",
        "installDefaultMarketplace": "安裝默認市場",
        "install": "安裝",
        "installed": "已安裝",
        "installing": "正在安裝...",
        "installNotImplemented": "安裝功能暫未實現。",
        "pauseEnvCheck": "跳過環境檢測",
        "useWindowsTerminal": "使用 Windows Terminal",
        "envCheckIntervalPrefix": "每隔",
        "envCheckIntervalSuffix": "日提醒檢測環境",
        "envCheckDueTitle": "環境檢測提醒",
        "envCheckDueMessage": "距離上次環境檢測已過{days}天，是否現在檢測？",
        "recheckEnv": "手動檢測更新運行環境",
        "skillRequiredError": "名稱和地址/路徑是必填項！",
        "skillZipOnlyError": "Gemini 和 Codex 僅支持 Zip 包式技能。",
        "skillAddError": "添加技能出錯: {error}",
        "skillDeleteError": "刪除技能出錯: {error}",
        "copyLog": "複製日誌",
        "logsCopied": "日誌已複製到剪貼板",
        "currentVersion": "當前版本",
        "latestVersion": "最新版本",
        "foundNewVersionMsg": "檢查到新版本，是否立即下載更新？",
        "packageUnavailable": "該版本的安裝包尚未發佈，請前往發佈頁面查看，或稍後再試。",
        "visitReleasePage": "前往發佈頁面",
        "isLatestVersion": "已是最新版本",
        "billing": "計費",
        "placeholderName": "例如：前端設計",
        "placeholderDesc": "描述...",
        "placeholderAddress": "@anthropics/...",
        "placeholderZip": "選擇 .zip 文件",
        "cannotDeleteSystemSkill": "系統技能包不能刪除。",
        "systemDefault": "系統默認",
        "envCheckTitle": "MaClaw 運行環境檢測安裝",
        "selectProvider": "選擇服務商",
        "knownProviders": "已知服務商",
        "providerList": "服務商列表",
        "selectProviderTitle": "選擇 API 服務商",
        "chinaProviders": "國內服務商",
        "globalProviders": "國外服務商",
        "allProviders": "全部服務商",
        "filterByRegion": "按地區篩選",
        "projectSearch": "搜尋",
        "projectSearchPlaceholder": "按名稱或路徑搜尋",
        "projectSortDefault": "預設順序",
        "projectSortNameAsc": "名稱 A-Z",
        "projectSortNameDesc": "名稱 Z-A",
        "projectSortPathAsc": "路徑 A-Z",
        "projectSortPathDesc": "路徑 Z-A",
        "projectNoResults": "沒有匹配的專案",
        "projectShowing": "顯示",
        "projectTotal": "總計",
        "prevPage": "上一頁",
        "nextPage": "下一頁",
        "mcpTabLocal": "本機 (Stdio)",
        "mcpTabRemote": "遠端 (HTTP)",
        "mcpLocalCount": "個本機 MCP Server",
        "mcpImportJson": "匯入 JSON",
        "mcpAdd": "+ 新增",
        "mcpLoading": "載入中...",
        "mcpNoLocalServers": "暫無本機 MCP Server，點擊「+ 新增」或「匯入 JSON」來設定",
        "mcpDisabled": "已停用",
        "mcpRunning": "執行中",
        "mcpNotRunning": "未執行",
        "mcpEnable": "啟用",
        "mcpDisable": "停用",
        "mcpEdit": "編輯",
        "mcpDelete": "刪除",
        "mcpEnvVars": "環境變數",
        "mcpConfirmDelete": "確認刪除",
        "mcpConfirmDeleteLocal": "確定要刪除本機 MCP Server「{name}」嗎？",
        "mcpDeleting": "刪除中...",
        "mcpImportJsonTitle": "匯入 JSON 設定",
        "mcpImportJsonDesc": "貼上標準 MCP JSON 設定，支援格式如：",
        "mcpImportJsonPlaceholder": "貼上 JSON 設定...",
        "mcpJsonFormatError": "JSON 格式錯誤",
        "mcpJsonStructureError": "格式不正確，需要 { mcpServers: { name: { command, args, env } } }",
        "mcpImporting": "匯入中...",
        "mcpImport": "匯入",
        "mcpRemoteImportJsonTitle": "匯入遠端 MCP 設定",
        "mcpRemoteImportJsonDesc": "貼上 MCP JSON 設定代碼（支援 Kiro / Cursor / Claude Desktop 格式）：",
        "mcpRemoteJsonStructureError": "格式不正確，需要 { mcpServers: { name: { url, headers } } } 或 { name: { endpoint_url } }",
        "mcpRemoteJsonMissingUrl": "伺服器「{name}」缺少 url 或 endpoint_url 欄位",
        "mcpEditLocalServer": "編輯本機 MCP Server",
        "mcpAddLocalServer": "新增本機 MCP Server",
        "mcpNameLabel": "名稱",
        "mcpNameRequired": "名稱不能為空",
        "mcpCommandLabel": "指令 (command)",
        "mcpCommandRequired": "指令不能為空",
        "mcpArgsLabel": "參數 (args)，每行一個",
        "mcpEnvLabel": "環境變數 (env)",
        "mcpAddEnvVar": "+ 新增環境變數",
        "mcpSubmitting": "提交中...",
        "mcpSave": "儲存",
        "mcpAutoStartOn": "開機自啟開",
        "mcpAutoStartOff": "開機自啟關",
        "mcpAutoStartStatus": "啟動時",
        "mcpAutoStartEnabled": "啟動時自動啟動",
        "mcpAutoStartDisabled": "啟動時不自動啟動",
        "mcpAutoStartDisabledHint": "請先啟用該服務，再設定自動啟動",
        "mcpAutoStartCheckbox": "程式啟動時自動啟動該服務",
        "mcpServersRegistered": "個已註冊 MCP Server",
        "mcpRegisterServer": "+ 註冊 MCP Server",
        "mcpNoRemoteServers": "暫無已註冊的 MCP Server",
        "mcpColName": "名稱",
        "mcpColEndpoint": "端點 URL",
        "mcpColHealth": "健康狀態",
        "mcpColTools": "工具數",
        "mcpColActions": "操作",
        "mcpHealthy": "健康",
        "mcpSlow": "緩慢",
        "mcpUnavailable": "不可用",
        "mcpChecking": "檢測中…",
        "mcpNotChecked": "未檢測",
        "mcpCollapse": "收起",
        "mcpTools": "工具",
        "mcpConfirmDeleteRemote": "確定要註銷 MCP Server「{name}」嗎？此操作不可撤銷。",
        "mcpEditServer": "編輯 MCP Server",
        "mcpRegisterServerTitle": "註冊 MCP Server",
        "mcpEndpointLabel": "端點 URL",
        "mcpEndpointRequired": "端點 URL 不能為空",
        "mcpAuthType": "認證方式",
        "mcpAuthNone": "無認證",
        "mcpAuthApiKey": "API Key",
        "mcpAuthBearer": "Bearer Token",
        "mcpEnterApiKey": "輸入 API Key",
        "mcpEnterBearer": "輸入 Bearer Token",
        "mcpCustomHeaders": "自訂 Headers",
        "mcpAddHeader": "新增",
        "mcpNoCustomHeaders": "無自訂 Headers",
        "mcpRegister": "註冊",
        "mcpHealthRecord": "健康檢查記錄",
        "mcpHealthStatus": "狀態",
        "mcpFailCount": "失敗次數",
        "mcpLastCheck": "最近檢查",
        "mcpCheckNow": "立即檢查",
        "mcpLoadingTools": "載入工具清單...",
        "mcpToolList": "工具清單",
        "mcpNoDescription": "無描述",
        "mcpNoTools": "暫無工具"
    }
};

interface ToolConfigurationProps {
    toolName: string;
    toolCfg: any;
    showModelSettings: boolean;
    setShowModelSettings: (show: boolean) => void;
    handleModelSwitch: (name: string) => void;
    t: (key: string) => string;
    lang: string;
}

const ToolConfiguration = ({
    toolName, toolCfg, showModelSettings, setShowModelSettings,
    handleModelSwitch, t, lang
}: ToolConfigurationProps) => {
    if (!toolCfg || !toolCfg.models) {
        return <div style={{ padding: '15px', color: 'var(--theme-text-secondary)' }}>Loading configuration...</div>;
    }

    const getBadge = (model: any): { bg: string; label: string } | null => {
        const name = model.model_name.toLowerCase();
        if (model.model_name === "Original") return { bg: 'var(--theme-primary)', label: t("originalFlag") };
        if (model.has_subscription) return { bg: 'var(--theme-danger)', label: t("subscription") };
        if (name.includes("glm") || name.includes("kimi") || name.includes("doubao") || name.includes("minimax"))
            return { bg: 'var(--theme-danger)', label: t("monthly") };
        if (name.includes("deepseek")) return { bg: 'var(--theme-warning)', label: t("premium") };
        if (name.includes("xiaomi")) return { bg: 'var(--theme-warning)', label: t("bigSpender") };
        if (model.is_custom) return { bg: 'var(--theme-text-muted)', label: t("customized") };
        if (["aicodemirror", "aigocode", "noin.ai", "gaccode", "chatfire", "coderelay"].some(p => name.includes(p)))
            return { bg: 'var(--theme-success)', label: t("forward") };
        return null;
    };

    return (
        <div style={{
            backgroundColor: 'var(--theme-surface-muted)',
            padding: '9px 12px',
            borderRadius: '12px',
            border: '1px solid var(--theme-border-subtle)',
            marginBottom: '10px'
        }}>
            <div className="model-switcher" style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(3, 1fr)',
                gap: '8px',
                width: '100%',
                paddingTop: '5px',
                paddingBottom: '5px',
                overflow: 'visible'
            }}>
                {toolCfg.models.map((model: any) => {
                    const badge = getBadge(model);
                    return (
                        <button
                            key={model.model_name}
                            className={`model-btn ${toolCfg.current_model === model.model_name ? 'selected' : ''}`}
                            onClick={() => handleModelSwitch(model.model_name)}
                            style={{
                                width: '100%',
                                padding: '5px 4px',
                                fontSize: '1.125rem',
                                borderBottom: (model.api_key && model.api_key.trim() !== "") ? '3px solid var(--primary-color)' : '1px solid var(--border-color)',
                                position: 'relative',
                                overflow: 'visible'
                            }}
                        >
                            {model.model_name === "Original" ? t("original") : getModelDisplayName(model.model_name, lang)}
                            {badge && (
                                <span style={{ ...badgeBaseStyle, backgroundColor: badge.bg }}>
                                    {badge.label}
                                </span>
                            )}
                        </button>
                    );
                })}
            </div>
        </div>
    );
};

function App() {
    const { showAlert } = useDialog();
    const [config, setConfig] = useState<main.AppConfig | null>(null);
    const [navTab, setNavTab] = useState<string>("ai");
    const [aiThemeMode, setAIThemeMode] = useState<'light' | 'dark'>(() => {
        if (typeof window === 'undefined') return 'light';
        try {
            return window.localStorage.getItem('ai_assistant_theme_mode') === 'dark' ? 'dark' : 'light';
        } catch {
            return 'light';
        }
    });
    const navTabRef = useRef(navTab);
    useEffect(() => { navTabRef.current = navTab; }, [navTab]);
    const [bbsContent, setBbsContent] = useState<string>("");
    const [tutorialContent, setTutorialContent] = useState<string>("");
    const [thanksContent, setThanksContent] = useState<string>("");
    const [refreshStatus, setRefreshStatus] = useState<string>("");
    const [lastUpdateTime, setLastUpdateTime] = useState<string>("");
    const [refreshKey, setRefreshKey] = useState<number>(0);
    const [activeTool, setActiveTool] = useState<string>("claude");
    const [status, setStatus] = useState("");
    const [activeTab, setActiveTab] = useState(0);
    const [tabStartIndex, setTabStartIndex] = useState(0);
    const [settingsTab, setSettingsTab] = useState<'general' | 'proxy' | 'ui' | 'display' | 'remote' | 'skills' | 'mcp' | 'llm' | 'serviceRedeem' | 'search' | 'embedding' | 'role' | 'memory' | 'agentnet' | 'security' | 'im' | 'system'>('general');
    const [imSubTab, setImSubTab] = useState<'qq' | 'telegram' | 'weixin' | 'lansenger'>('qq');
    const [qqBotStatus, setQQBotStatus] = useState<string>('disconnected');
    const [qqBotLocalMode, setQQBotLocalModeState] = useState<boolean>(true);
    const [telegramStatus, setTelegramStatus] = useState<string>('disconnected');
    const [telegramLocalMode, setTelegramLocalModeState] = useState<boolean>(true);
    const [weixinStatus, setWeixinStatus] = useState<string>('disconnected');
    const [weixinLocalMode, setWeixinLocalModeState] = useState<boolean>(true);
    const [lansengerStatus, setLansengerStatus] = useState<string>('disabled');
    const [lansengerLocalMode, setLansengerLocalModeState] = useState<boolean>(true);
    const [weixinQRCode, setWeixinQRCode] = useState<string>('');
    const [weixinQRLoading, setWeixinQRLoading] = useState<boolean>(false);
    const [weixinQRWaiting, setWeixinQRWaiting] = useState<boolean>(false);
    const [weixinQRError, setWeixinQRError] = useState<string>('');
    const [installLocation, setInstallLocation] = useState<'user' | 'project'>('user');
    const [installProject, setInstallProject] = useState<string>("");
    const [isBatchInstalling, setIsBatchInstalling] = useState(false);
    const [isMarketplaceInstalling, setIsMarketplaceInstalling] = useState(false);
    const [isLoading, setIsLoading] = useState(true);
    const [isManualCheck, setIsManualCheck] = useState(false);
    const [toolProviders, setToolProviders] = useState<Array<{ name: string; valid: boolean; builtin: boolean }>>([]);

    // Fetch provider list when the default tool selection changes
    useEffect(() => {
        const selectedTool = config?.default_tool || '';
        if (!selectedTool) {
            setToolProviders([]);
            return;
        }
        ListToolProviders(selectedTool).then((providers) => {
            setToolProviders(providers || []);
        }).catch(() => {
            setToolProviders([]);
        });
    }, [config?.default_tool]);

    const [showStartupPopup, setShowStartupPopup] = useState(false);
    const [showMaclawLLMPopup, setShowMaclawLLMPopup] = useState(false);
    const [aiPanelMaximized, setAiPanelMaximized] = useState(false);
    const [pythonEnvironments, setPythonEnvironments] = useState<any[]>([]);
    const [envCheckInterval, setEnvCheckInterval] = useState<number>(7);
    const [uiZoom, setUiZoom] = useState<number>(1.0);

    // Brand info from backend
    const [brandInfo, setBrandInfo] = useState<{id: string, displayName: string, displayNameCN: string, slogan: string, author: string, businessContact: string, websiteURL: string, githubURL: string, iconPath: string} | null>(null);
    const currentIcon = brandInfo?.id === 'qianxin' ? qianxinIcon : appIcon;
    const brandDisplayTitle = brandInfo ? `${brandInfo.displayNameCN} ${brandInfo.displayName}` : '码卡龙 MaClaw';
    const brandSidebarName = brandInfo?.displayName || 'MaClaw';

    // MaClaw LLM online status (lobster indicator)
    const [maclawLLMOnline, setMaclawLLMOnline] = useState<boolean>(false);
    const [maclawLLMConfigured, setMaclawLLMConfigured] = useState<boolean>(false);
    const [sidebarCurrentProviderTokenUsage, setSidebarCurrentProviderTokenUsage] = useState<{ provider: string; input: number; output: number; total: number }>({ provider: '', input: 0, output: 0, total: 0 });
    const maclawLLMFirstPingDone = useRef(false);

    // AgentNet P2P network running status (globe indicator)
    const [agentNetRunning, setAgentNetRunning] = useState<boolean>(false);
    const maclawLLMFirstPingResult = useRef<{online: boolean; configured: boolean} | null>(null);

    // Active background task count (for sidebar task icon badge)
    const [activeTaskCount, setActiveTaskCount] = useState<number>(0);

    // Ref to prevent multiple hide clicks
    const isHidingRef = useRef(false);

    useEffect(() => {
        // activeTab 0 is Original (hidden), so configurable models start at 1.
        // We map activeTab to a 0-based index for the configurable list.
        const localActiveIndex = activeTab > 0 ? activeTab - 1 : 0;

        if (localActiveIndex < tabStartIndex) {
            setTabStartIndex(localActiveIndex);
        } else if (localActiveIndex >= tabStartIndex + 4) {
            setTabStartIndex(localActiveIndex - 3);
        }
    }, [activeTab]);

    const [showModelSettings, setShowModelSettings] = useState(false);
    const [showProxySettings, setShowProxySettings] = useState(false);

    useEffect(() => {
        if (showModelSettings && activeTab === 0) {
            setActiveTab(1);
        }
    }, [showModelSettings, activeTab]);

    const [showInstallSkillModal, setShowInstallSkillModal] = useState(false);
    const [selectedSkillsToInstall, setSelectedSkillsToInstall] = useState<string[]>([]);

    // Load skills with install status when install modal is shown or location/project changes
    useEffect(() => {
        if (showInstallSkillModal && config) {
            // Get project path for project installations
            let targetProjectPath = "";
            if (installLocation === 'project' && installProject) {
                const p = config.projects?.find((proj: any) => proj.id === installProject);
                if (p) targetProjectPath = p.path;
            }

            // Load skills with install status
            ListSkillsWithInstallStatus(activeTool, installLocation, targetProjectPath)
                .then(list => setSkills(list || []))
                .catch(err => console.error('Failed to load skills:', err));
        }
    }, [showInstallSkillModal, installLocation, installProject, activeTool, config]);

    // Load skills when navigating to skills tab
    useEffect(() => {
        if (navTab === 'skills') {
            ListSkills(activeTool)
                .then(list => setSkills(list || []))
                .catch(err => console.error('Failed to load skills:', err));
        }
    }, [navTab, activeTool]);

    const [toolStatuses, setToolStatuses] = useState<any[]>([]);
    const [envLogs, setEnvLogs] = useState<string[]>([]);
    const [showLogs, setShowLogs] = useState(false);
    const [toolRepairStatus, setToolRepairStatus] = useState<{show: boolean, toolName: string, status: 'installing' | 'success' | 'failed', message: string}>({show: false, toolName: '', status: 'installing', message: ''});
    const [onDemandInstallingTool, setOnDemandInstallingTool] = useState<string>("");  // Track which tool is being installed on-demand
    const [backgroundInstallStatus, setBackgroundInstallStatus] = useState<string>("");
    const [backgroundInstallingTool, setBackgroundInstallingTool] = useState<string>("");  // Track which tool is being installed in background
    const [launchingTool, setLaunchingTool] = useState<string>("");  // Track which tool is being launched
    const [selectedProjectForLaunch, setSelectedProjectForLaunch] = useState<string>("");
    const [launchProjectKeyword, setLaunchProjectKeyword] = useState<string>("");
    const [projectSearchKeyword, setProjectSearchKeyword] = useState<string>("");
    const [projectSortMode, setProjectSortMode] = useState<'default' | 'name-asc' | 'name-desc' | 'path-asc' | 'path-desc'>('default');
    const [projectCurrentPage, setProjectCurrentPage] = useState<number>(1);
    const [showInstallLog, setShowInstallLog] = useState(false);
    const [showUpdateModal, setShowUpdateModal] = useState(false);
    const [updateResult, setUpdateResult] = useState<any>(null);
    const [isDownloading, setIsDownloading] = useState(false);
    const [downloadProgress, setDownloadProgress] = useState(0);
    const [downloadError, setDownloadError] = useState("");
    const [installerPath, setInstallerPath] = useState("");
    const [isStartupUpdateCheck, setIsStartupUpdateCheck] = useState(false);
    const isWindows = /window/i.test(navigator.userAgent);
    const [hasWindowsTerminal, setHasWindowsTerminal] = useState(false);
    const [lang, setLang] = useState("en");
    const translate = (key: string) => translations[lang][key] || translations["en"][key] || key;
    const formatText = (key: string, values: Record<string, string> = {}) => {
        return Object.entries(values).reduce((text, [name, value]) => text.replaceAll(`{${name}}`, value), translate(key));
    };
    const localizeText = (en: string, zhHans: string, zhHant: string) => (
        lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
    );
    const t = translate;
    const { showToast: showToastGlobal } = useToast();
    const [skills, setSkills] = useState<main.Skill[]>([]);
    const [showAddSkillModal, setShowAddSkillModal] = useState(false);
    const [gossipAllowed, setGossipAllowed] = useState(true);
    const [showRemoteActivationModal, setShowRemoteActivationModal] = useState(false);
    const [pendingRemoteLaunchTool, setPendingRemoteLaunchTool] = useState<string>("");
    const [remoteActivationDraft, setRemoteActivationDraft] = useState({ hub_url: "", hubcenter_url: "", email: "" });
    const [remoteCenterHubs, setRemoteCenterHubs] = useState<RemoteCenterHubOption[]>([]);
    const [loadingRemoteCenterHubs, setLoadingRemoteCenterHubs] = useState(false);
    const [newSkillName, setNewSkillName] = useState("");
    const [newSkillDesc, setNewSkillDesc] = useState("");
    const [newSkillType, setNewSkillType] = useState("address");
    const [newSkillValue, setNewSkillValue] = useState("");
    const [selectedSkill, setSelectedSkill] = useState<string | null>(null);
    const [skillContextMenu, setSkillContextMenu] = useState<{ x: number, y: number, visible: boolean, skillName: string | null }>({
        x: 0, y: 0, visible: false, skillName: null
    });

    const [confirmDialog, setConfirmDialog] = useState<{
        show: boolean;
        title: string;
        message: string;
        onConfirm: () => void;
        onCancel?: () => void;
    }>({
        show: false,
        title: "",
        message: "",
        onConfirm: () => { }
    });

    // Provider selector state
    const [showProviderSelector, setShowProviderSelector] = useState(false);
    const [showModelRecommend, setShowModelRecommend] = useState(false);
    const [providerFilter, setProviderFilter] = useState<'all' | 'china' | 'global'>('all');
    const [selectedProviderForUrl, setSelectedProviderForUrl] = useState<ProviderEndpoint | null>(null);
    const [hoveredProvider, setHoveredProvider] = useState<{ provider: ProviderEndpoint, x: number, y: number } | null>(null);

    const showToastMessage = (message: string, duration: number = 3000) => {
        showToastGlobal(message, 'info', duration);
    };


    const handleDeleteSkill = async (name: string) => {
        if (name === "Claude Official Documentation Skill Package" || name === "超能力技能包") {
            showToastMessage(t("cannotDeleteSystemSkill"));
            return;
        }

        setConfirmDialog({
            show: true,
            title: t("confirmDelete"),
            message: t("confirmDeleteSkill"),
            onConfirm: async () => {
                try {
                    await DeleteSkill(name, activeTool);
                    const list = await ListSkills(activeTool);
                    setSkills(list || []);
                    if (selectedSkill === name) setSelectedSkill(null);
                    showToastMessage(t("skillDeleted"));
                    setConfirmDialog(prev => ({ ...prev, show: false }));
                } catch (err) {
                    showToastMessage(t("skillDeleteError").replace("{error}", err as string));
                }
            }
        });
    };

    const handleDownload = async () => {
        if (!updateResult) return;
        // Use download_url if available (added in backend update), fallback to release_url
        const downloadUrl = updateResult.download_url || updateResult.release_url;
        if (!downloadUrl) return;

        setIsDownloading(true);
        setDownloadProgress(0);
        setDownloadError("");
        setInstallerPath("");

        const fileName = isWindows ? "MaClaw-Setup.exe" : "MaClaw-Universal.pkg";

        try {
            const path = await DownloadUpdate(downloadUrl, fileName);
            setInstallerPath(path);
        } catch (err: any) {
            console.error("Download error:", err);
            // Error is handled by the event listener
        }
    };

    const handleCancelDownload = () => {
        const fileName = isWindows ? "MaClaw-Setup.exe" : "MaClaw-Universal.pkg";
        CancelDownload(fileName);
    };

    const handleInstall = async () => {
        if (installerPath) {
            try {
                await LaunchInstallerAndExit(installerPath);
            } catch (err) {
                console.error("Install launch error:", err);
                showToastMessage(t("downloadError").replace("{error}", err as string));
            }
        }
    };

    const handleAIPanelMaximizeToggle = () => {
        if (aiPanelMaximized) {
            WindowUnfullscreen();
        } else {
            WindowFullscreen();
        }
        setAiPanelMaximized(prev => !prev);
    };

    const handleWindowHide = (e: React.MouseEvent) => {
        // Prevent event bubbling and default behavior
        e.preventDefault();
        e.stopPropagation();

        console.log("Hide button clicked"); // Debug log

        // Prevent multiple rapid clicks
        if (isHidingRef.current) {
            console.log("Already hiding, ignoring click");
            return;
        }
        isHidingRef.current = true;

        console.log("Calling WindowHide");
        WindowHide();

        // Reset flag after a short delay
        setTimeout(() => {
            isHidingRef.current = false;
        }, 1000);
    };

    useEffect(() => {
        const handleClick = () => {
            setSkillContextMenu(prev => ({ ...prev, visible: false }));
        };
        window.addEventListener('click', handleClick);
        return () => window.removeEventListener('click', handleClick);
    }, [skillContextMenu]);

    const logEndRef = useRef<HTMLTextAreaElement>(null);

    useEffect(() => {
        if (logEndRef.current) {
            logEndRef.current.scrollTop = logEndRef.current.scrollHeight;
        }
    }, [envLogs]);

    useEffect(() => {
        // Language detection
        const userLang = navigator.language;
        let initialLang = "en";
        if (userLang.startsWith("zh-TW") || userLang.startsWith("zh-HK")) {
            initialLang = "zh-Hant";
        } else if (userLang.startsWith("zh")) {
            initialLang = "zh-Hans";
        }
        setLang(initialLang);
        SetLanguage(initialLang);

        // Load brand info from backend
        GetBrandInfo().then((info: any) => {
            setBrandInfo(info);
        }).catch(() => {});

        // Detect OS from backend for Windows Terminal check
        GetSystemInfo().then(info => {
            if (info.os === "windows") {
                IsWindowsTerminalAvailable().then(available => {
                    setHasWindowsTerminal(available);
                }).catch(() => {
                    setHasWindowsTerminal(false);
                });
            }
        }).catch(() => {});

        // Environment Check Logic
        const logHandler = (msg: string) => {
            setEnvLogs(prev => [...prev, msg]);
            // Only show logs panel for serious errors (installation failures, download failures)
            const lowerMsg = msg.toLowerCase();
            const isSerialError = (lowerMsg.includes("failed") || lowerMsg.includes("error")) &&
                                  (lowerMsg.includes("install") || lowerMsg.includes("download") ||
                                   lowerMsg.includes("npm") || lowerMsg.includes("node"));
            if (isSerialError) {
                setShowLogs(true);
            }
        };
        const doneHandler = () => {
            ResizeWindow(788, 528);
            setIsLoading(false);
            setIsManualCheck(false);
        };

        EventsOn("env-log", logHandler);
        EventsOn("env-check-done", doneHandler);
        EventsOn("show-env-logs", () => {
            setEnvLogs([]);
            setShowLogs(true);
            setIsLoading(true);
            setIsManualCheck(true);
        });

        // Tool repair events
        EventsOn("tool-repair-start", (toolName: string) => {
            setToolRepairStatus({show: true, toolName, status: 'installing', message: ''});
        });
        EventsOn("tool-repair-success", (toolName: string, version: string) => {
            setToolRepairStatus({show: true, toolName, status: 'success', message: version});
            // Auto-close after 2 seconds on success
            setTimeout(() => {
                setToolRepairStatus(prev => ({...prev, show: false}));
            }, 2000);
        });
        EventsOn("tool-repair-failed", (toolName: string, error: string) => {
            setToolRepairStatus({show: true, toolName, status: 'failed', message: error});
        });

        EventsOn("download-progress", (data: any) => {
            console.log("Download progress event:", data);
            if (data.status === "downloading") {
                setDownloadProgress(Math.floor(data.percentage));
            } else if (data.status === "completed") {
                setDownloadProgress(100);
                setIsDownloading(false);
            } else if (data.status === "error") {
                setDownloadError(data.error);
                setIsDownloading(false);
            } else if (data.status === "cancelled") {
                setDownloadError(t("downloadCancelled"));
                setIsDownloading(false);
            }
        });

        CheckEnvironment(false); // Start checks

        // Load environment check interval and check if due
        GetEnvCheckInterval().then(val => setEnvCheckInterval(val));

        ShouldCheckEnvironment().then(due => {
            if (due) {
                // Fetch interval again to use in message
                GetEnvCheckInterval().then(days => {
                    const currentLang = initialLang;
                    const localT = (key: string) => translations[currentLang][key] || translations["en"][key] || key;

                    setConfirmDialog({
                        show: true,
                        title: localT("envCheckDueTitle"),
                        message: localT("envCheckDueMessage").replace("{days}", days.toString()),
                        onConfirm: () => {
                            setConfirmDialog(prev => ({ ...prev, show: false }));
                            UpdateLastEnvCheckTime();
                            setEnvLogs([]);
                            setShowLogs(true);
                            setIsLoading(true);
                            setIsManualCheck(true);
                            CheckEnvironment(true);
                        },
                        onCancel: () => {
                            setConfirmDialog(prev => ({ ...prev, show: false }));
                            UpdateLastEnvCheckTime(); // Reset timer even if cancelled
                        }
                    });
                });
            }
        });

        // Load Python environments
        ListPythonEnvironments().then((envs) => {
            setPythonEnvironments(envs);
        }).catch(err => {
            console.error("Failed to load Python environments:", err);
        });

        // Config Logic
        LoadConfig().then((cfg) => {
            // Apply default launch mode setting on startup
            if (cfg.default_launch_mode === 'remote') {
                cfg.remote_enabled = true;
            } else if (cfg.default_launch_mode === 'local') {
                cfg.remote_enabled = false;
            }
            setConfig(cfg);

            // Apply saved UI zoom factor
            GetUIZoomFactor().then((z) => {
                if (z > 0) {
                    setUiZoom(z);
                }
            }).catch(() => {});

            if (!cfg.pause_env_check) {
                checkTools();
            }

            if (cfg && cfg.language) {
                setLang(cfg.language);
                SetLanguage(cfg.language);
            }

            // Automatic update check on startup disabled - use "Online Update" button instead
            // Show welcome page if needed
            if (cfg && !cfg.hide_startup_popup) {
                setShowStartupPopup(true);
            }
            if (cfg && cfg.current_project) {
                setSelectedProjectForLaunch(cfg.current_project);
            } else if (cfg && cfg.projects && cfg.projects.length > 0) {
                setSelectedProjectForLaunch(cfg.projects[0].id);
            }
            if (cfg) {
                // Both modes default to AI assistant panel on startup
                setNavTab("ai");

                // Keep track of the last active tool for settings/launch logic
                const lastActiveTool = cfg.active_tool || "claude";
                if (isToolTab(lastActiveTool)) {
                    setActiveTool(lastActiveTool);
                }

                ReadBBS().then(content => setBbsContent(content)).catch(err => console.error(err));
                ReadThanks().then(content => setThanksContent(content)).catch(err => console.error(err));

                const toolCfg = (cfg as any)[lastActiveTool];
                if (toolCfg && toolCfg.models) {
                    const idx = toolCfg.models.findIndex((m: any) => m.model_name === toolCfg.current_model);
                    if (idx !== -1) setActiveTab(idx);

                    // NOTE: removed auto-popup of provider config when no API key is set.
                    // Users can open it manually via the "服务商配置" button.
                }
            }
        }).catch(err => {
            console.error("Failed to load config on startup:", err);
            setStatus("Error loading config: " + err);
            // Fallback: retry once after a short delay. If the config file was
            // being written by a concurrent SaveConfig, it should be ready now.
            setTimeout(() => {
                LoadConfig().then((cfg) => {
                    setConfig(cfg);
                    if (cfg && cfg.language) {
                        setLang(cfg.language);
                        SetLanguage(cfg.language);
                    }
                }).catch(err2 => {
                    console.error("Retry load config also failed:", err2);
                    // Last resort: set a minimal default config so the UI is not stuck
                    // on "加载配置中" forever. User can still use the app and reconfigure.
                    setConfig(new main.AppConfig({}));
                });
            }, 1500);
        });

        // Listen for external config changes (e.g. from Tray)
        const handleConfigChange = (cfg: main.AppConfig) => {
            setConfig(cfg);
            GetUIZoomFactor().then((z) => {
                if (z > 0) {
                    setUiZoom(z);
                }
            }).catch(() => {});
            // Sync with tray menu changes — but don't yank the user away from
            // the AI assistant panel.  'ai' is never persisted as active_tool,
            // so a config-changed event would always overwrite it.
            if (navTabRef.current !== 'ai') {
                const tool = cfg.active_tool || "ai";
                setNavTab(tool);
                if (tool === 'claude' || tool === 'gemini' || tool === 'codex' || tool === 'opencode' || tool === 'codebuddy' || tool === 'cursor' || tool === 'iflow' || tool === 'kilo') {
                    setActiveTool(tool);
                    const toolCfg = (cfg as any)[tool];
                    if (toolCfg && toolCfg.models) {
                        const idx = toolCfg.models.findIndex((m: any) => m.model_name === toolCfg.current_model);
                        if (idx !== -1) setActiveTab(idx);
                    }
                }
            }
        };
        EventsOn("config-changed", handleConfigChange);
        EventsOn("config-updated", handleConfigChange);

        // QQ Bot status listener
        EventsOn("qqbot-status-changed", (status: string) => {
            setQQBotStatus(status);
            if (status === 'error') {
                // Auto-rollback: disable the checkbox and save config
                LoadConfig().then((c: any) => {
                    if (c?.qqbot_enabled) {
                        const next = new main.AppConfig({ ...c, qqbot_enabled: false });
                        setConfig(next);
                        SaveConfig(next).then(() => RestartQQBot()).catch(() => {});
                    }
                }).catch(() => {});
            }
        });
        // Fetch initial QQ Bot status
        GetQQBotStatus().then(setQQBotStatus).catch(() => {});
        GetQQBotLocalMode().then(setQQBotLocalModeState).catch(() => {});

        // Telegram Bot status listener
        EventsOn("telegram-status-changed", (status: string) => {
            setTelegramStatus(status);
            if (status === 'error') {
                LoadConfig().then((c: any) => {
                    if (c?.telegram_bot_enabled) {
                        const next = new main.AppConfig({ ...c, telegram_bot_enabled: false });
                        setConfig(next);
                        SaveConfig(next).then(() => RestartTelegram()).catch(() => {});
                    }
                }).catch(() => {});
            }
        });
        GetTelegramStatus().then(setTelegramStatus).catch(() => {});
        GetTelegramLocalMode().then(setTelegramLocalModeState).catch(() => {});

        // WeChat status listener
        EventsOn("weixin-status-changed", (status: string) => {
            setWeixinStatus(status);
        });
        GetWeixinStatus().then(setWeixinStatus).catch(() => {});
        GetWeixinLocalMode().then(setWeixinLocalModeState).catch(() => {});

        // Lansenger status listener
        EventsOn("lansenger-status-changed", (payload: any) => {
            let status = '';
            if (typeof payload === 'string') {
                status = payload;
            } else if (payload?.Status) {
                status = payload.Status;
            } else if (payload?.status) {
                status = payload.status;
            }
            if (status) {
                setLansengerStatus(status);
                if (status === 'error') {
                    LoadConfig().then((c: any) => {
                        if (c?.lansenger_enabled) {
                            const next = new main.AppConfig({ ...c, lansenger_enabled: false });
                            setConfig(next);
                            SaveConfig(next).catch(() => {});
                            StopLansenger().catch(() => {});
                            setLansengerStatus('disabled');
                        }
                    }).catch(() => {});
                }
            }
        });
        GetLansengerStatus().then(setLansengerStatus).catch(() => {});
        GetLansengerLocalMode().then(setLansengerLocalModeState).catch(() => {});

        // Listen for background tool installation events
        EventsOn("tool-checking", (toolName: string) => {
            setBackgroundInstallStatus(lang === 'zh-Hans' ? `检查 ${toolName}...` : `Checking ${toolName}...`);
            setBackgroundInstallingTool("");  // Clear previous tool's installing state
        });

        EventsOn("tool-installing", (toolName: string) => {
            setBackgroundInstallStatus(lang === 'zh-Hans' ? `安装 ${toolName}...` : `Installing ${toolName}...`);
            setBackgroundInstallingTool(toolName);
        });

        EventsOn("tool-updating", (toolName: string) => {
            setBackgroundInstallStatus(lang === 'zh-Hans' ? `更新 ${toolName}...` : `Updating ${toolName}...`);
            setBackgroundInstallingTool(toolName);
        });

        EventsOn("tool-installed", (toolName: string) => {
            console.log("Tool installed in background:", toolName);
            setBackgroundInstallStatus(lang === 'zh-Hans' ? `✓ ${toolName} 安装完成` : `✓ ${toolName} installed`);
            setBackgroundInstallingTool("");
            setTimeout(() => setBackgroundInstallStatus(""), 3000);
            // Refresh tool statuses
            CheckToolsStatus().then(statuses => {
                setToolStatuses(statuses);
            });
        });

        EventsOn("tool-updated", (toolName: string) => {
            console.log("Tool updated in background:", toolName);
            setBackgroundInstallStatus(lang === 'zh-Hans' ? `✓ ${toolName} 已更新` : `✓ ${toolName} updated`);
            setBackgroundInstallingTool("");
            setTimeout(() => setBackgroundInstallStatus(""), 3000);
            // Refresh tool statuses
            CheckToolsStatus().then(statuses => {
                setToolStatuses(statuses);
            });
        });

        EventsOn("tools-install-done", () => {
            console.log("Background tool installation complete");
            setBackgroundInstallStatus("");
            setBackgroundInstallingTool("");
            // Final refresh of tool statuses
            CheckToolsStatus().then(statuses => {
                setToolStatuses(statuses);
            });
        });

        // Hub security policy: refresh gossip visibility when policy changes (Req 6.1)
        IsGossipAllowed().then(setGossipAllowed).catch(() => {});
        EventsOn("hub-security-policy-changed", () => {
            IsGossipAllowed().then(setGossipAllowed).catch(() => {});
        });

        return () => {
            EventsOff("env-log");
            EventsOff("env-check-done");
            EventsOff("download-progress");
            EventsOff("config-changed");
            EventsOff("config-updated");
            EventsOff("qqbot-status-changed");
            EventsOff("telegram-status-changed");
            EventsOff("weixin-status-changed");
            EventsOff("tool-checking");
            EventsOff("tool-installing");
            EventsOff("tool-updating");
            EventsOff("tool-installed");
            EventsOff("tool-updated");
            EventsOff("tools-install-done");
            EventsOff("hub-security-policy-changed");
        };
    }, []);

    // Poll AgentNet running status so the globe indicator lights up without
    // requiring the user to visit the settings panel first.
    // When the settings tab is active, AgentNetPanel also polls — but the
    // lightweight AgentNetIsRunning() call is idempotent, so the overlap is
    // harmless and keeps the globe indicator responsive on tab switches.
    // NOTE: Only poll when agentnet_enabled — if disabled, report as not running.
    const agentNetAutoStarted = useRef(false);
    const agentNetPrevUp = useRef(false);
    const agentNetEnabledRef = useRef(!!config?.agentnet_enabled);
    useEffect(() => { agentNetEnabledRef.current = !!config?.agentnet_enabled; }, [config?.agentnet_enabled]);
    useEffect(() => {
        let retryTimer: ReturnType<typeof setTimeout> | null = null;
        const clearRetry = () => {
            if (retryTimer) { clearTimeout(retryTimer); retryTimer = null; }
        };
        const checkAgentNet = () => {
            clearRetry();
            // When AgentNet is disabled in config, don't report as running
            // even if a residual daemon process happens to be alive.
            if (!agentNetEnabledRef.current) {
                agentNetPrevUp.current = false;
                setAgentNetRunning(false);
                return;
            }
            AgentNetIsRunning().then(up => {
                if (!up && agentNetPrevUp.current) {
                    // Was online, now looks offline — quick retry in 2s to
                    // avoid flashing the icon gray on a transient hiccup.
                    retryTimer = setTimeout(() => {
                        retryTimer = null;
                        AgentNetIsRunning()
                            .then(up2 => {
                                agentNetPrevUp.current = up2;
                                setAgentNetRunning(up2);
                            })
                            .catch(() => {
                                agentNetPrevUp.current = false;
                                setAgentNetRunning(false);
                            });
                    }, 2000);
                } else {
                    agentNetPrevUp.current = up;
                    setAgentNetRunning(up);
                }
            }).catch(() => {
                agentNetPrevUp.current = false;
                setAgentNetRunning(false);
            });
        };
        checkAgentNet();
        const timer = setInterval(checkAgentNet, 5000);
        return () => { clearInterval(timer); clearRetry(); };
    }, []);

    // Auto-start AgentNet daemon when enabled in config, so the user doesn't
    // have to visit the settings panel to light up the globe icon.
    // When disabled, actively stop any residual daemon.
    useEffect(() => {
        // Skip when config hasn't loaded yet — don't kill a daemon before
        // we know the user's preference.
        if (!config) return;
        if (!config.agentnet_enabled) {
            // Disabled — stop residual daemon if it's still running.
            AgentNetIsRunning().then(up => {
                if (up) {
                    AgentNetStopDaemon().catch(() => {});
                }
            }).catch(() => {});
            setAgentNetRunning(false);
            return;
        }
        if (agentNetAutoStarted.current) return;
        agentNetAutoStarted.current = true;
        AgentNetIsRunning().then(up => {
            if (!up) {
                AgentNetEnsureDaemonWithDownload()
                    .then(() => AgentNetIsRunning())
                    .then(up2 => setAgentNetRunning(up2))
                    .catch(() => {});
            } else {
                setAgentNetRunning(true);
            }
        }).catch(() => {});
    }, [config?.agentnet_enabled]);

    // Poll MaClaw LLM status every 60 seconds.
    // Also re-ping immediately when the user navigates to/from the LLM settings
    // tab (settingsTab changes), which covers the "just saved config" scenario.
    useEffect(() => {
        const pingLLM = () => {
            PingMaclawLLM().then((s: any) => {
                setMaclawLLMOnline(!!s.online);
                setMaclawLLMConfigured(!!s.configured);
                setMaclawLLMConfigured(!!s.configured);
                // Stash the first ping result; the separate config-aware effect will decide whether to show the popup.
                if (!maclawLLMFirstPingDone.current) {
                    maclawLLMFirstPingDone.current = true;
                    maclawLLMFirstPingResult.current = { online: !!s.online, configured: !!s.configured };
                }
            }).catch(() => {
                setMaclawLLMOnline(false);
                if (!maclawLLMFirstPingDone.current) {
                    maclawLLMFirstPingDone.current = true;
                    maclawLLMFirstPingResult.current = { online: false, configured: false };
                }
            });
        };
        pingLLM();
        const timer = setInterval(pingLLM, 60000);
        return () => clearInterval(timer);
    }, []);

    // Show the MaClaw LLM popup once both the first ping result AND config are available.
    // Only checks LLM status here; registration check is done in a separate effect
    // after useRemotePanel provides remoteActivationStatus.
    useEffect(() => {
        if (!config || !maclawLLMFirstPingResult.current) return;
        const { online } = maclawLLMFirstPingResult.current;
        if (!online) {
            setShowMaclawLLMPopup(true);
            // Suppress the startup welcome popup to avoid two overlapping modals
            setShowStartupPopup(false);
        }
        // Clear so this only fires once.
        maclawLLMFirstPingResult.current = null;
    }, [config, maclawLLMOnline]);

    const checkTools = async () => {
        try {
            const statuses = await CheckToolsStatus();
            setToolStatuses(statuses);
            // Tools are now installed in background by the backend
            // No need to install here - just update the status
        } catch (err) {
            console.error("Failed to check tools:", err);
        }
    };

    const handleLangChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
        const newLang = e.target.value;
        setLang(newLang);
        SetLanguage(newLang);
        if (config) {
            const newConfig = new main.AppConfig({ ...config, language: newLang });
            setConfig(newConfig);
            SaveConfig(newConfig);
        }
    };

    const switchTool = (tool: string) => {
        setNavTab(tool);
        if (isToolTab(tool)) {
            setActiveTool(tool);
            setActiveTab(0);
        }

        if (tool === 'message') {
            // message tab removed — redirect to AI assistant
            switchTool('ai');
            return;
        }

        if (tool === 'tutorial') {
            setShowModelSettings(false);
            ReadTutorial().then(content => setTutorialContent(content)).catch(err => console.error(err));
        }

        if (tool === 'skills') {
            setShowModelSettings(false);
            ListSkills(activeTool).then(list => setSkills(list || [])).catch(err => console.error(err));
        }

        if (config) {
            // Don't persist 'ai' as active_tool — it's a UI nav state, not a coding tool
            if (tool !== 'ai') {
                const newConfig = new main.AppConfig({ ...config, active_tool: tool });
                setConfig(newConfig);
                SaveConfig(newConfig);
            }

            const toolCfg = (config as any)[tool];
            if (toolCfg && toolCfg.models) {
                const idx = toolCfg.models.findIndex((m: any) => m.model_name === toolCfg.current_model);
                if (idx !== -1) setActiveTab(idx);
            }
        }
    };

    const handleSkillContext = (e: React.MouseEvent, skillName: string) => {
        e.preventDefault();
        e.stopPropagation();

        if (skillName === "Claude Official Documentation Skill Package" || skillName === "超能力技能包") {
             return;
        }

        setSelectedSkill(skillName);
        setSkillContextMenu({
            x: e.clientX,
            y: e.clientY,
            visible: true,
            skillName: skillName
        });
    };

    const allProjects = config?.projects || [];
    const normalizedProjectKeyword = projectSearchKeyword.trim().toLowerCase();
    const filteredAndSortedProjects = useMemo(() => {
        const filtered = normalizedProjectKeyword.length === 0
            ? allProjects
            : allProjects.filter((proj: any) =>
                (proj.name || "").toLowerCase().includes(normalizedProjectKeyword) ||
                (proj.path || "").toLowerCase().includes(normalizedProjectKeyword)
            );

        if (projectSortMode === 'default') return filtered;

        const sorted = [...filtered];
        sorted.sort((a: any, b: any) => {
            const nameA = (a.name || "").toLowerCase();
            const nameB = (b.name || "").toLowerCase();
            const pathA = (a.path || "").toLowerCase();
            const pathB = (b.path || "").toLowerCase();
            switch (projectSortMode) {
                case 'name-asc':
                    return nameA.localeCompare(nameB);
                case 'name-desc':
                    return nameB.localeCompare(nameA);
                case 'path-asc':
                    return pathA.localeCompare(pathB);
                case 'path-desc':
                    return pathB.localeCompare(pathA);
                default:
                    return 0;
            }
        });
        return sorted;
    }, [allProjects, normalizedProjectKeyword, projectSortMode]);

    const totalProjectPages = Math.max(1, Math.ceil(filteredAndSortedProjects.length / PROJECT_PAGE_SIZE));
    const safeProjectCurrentPage = Math.min(projectCurrentPage, totalProjectPages);
    const projectPageStartIndex = (safeProjectCurrentPage - 1) * PROJECT_PAGE_SIZE;
    const pagedProjects = useMemo(() => (
        filteredAndSortedProjects.slice(projectPageStartIndex, projectPageStartIndex + PROJECT_PAGE_SIZE)
    ), [filteredAndSortedProjects, projectPageStartIndex]);
    const normalizedLaunchProjectKeyword = launchProjectKeyword.trim().toLowerCase();
    const launchProjectOptions = useMemo(() => {
        const candidates = normalizedLaunchProjectKeyword.length === 0
            ? allProjects
            : allProjects.filter((proj: any) =>
                (proj.name || "").toLowerCase().includes(normalizedLaunchProjectKeyword) ||
                (proj.path || "").toLowerCase().includes(normalizedLaunchProjectKeyword)
            );
        return [...candidates].sort((a: any, b: any) => {
            if (a.id === config?.current_project) return -1;
            if (b.id === config?.current_project) return 1;
            return (a.name || "").localeCompare((b.name || ""));
        });
    }, [allProjects, normalizedLaunchProjectKeyword, config?.current_project]);
    const resolvedLaunchProject = useMemo(() => {
        if (!config?.projects || config.projects.length === 0) return null;
        return config.projects.find((p: any) => p.id === selectedProjectForLaunch)
            || config.projects.find((p: any) => p.id === config.current_project)
            || config.projects[0];
    }, [config, selectedProjectForLaunch]);
    const updateResolvedLaunchProject = (updater: (project: any) => any) => {
        if (!config?.projects || !resolvedLaunchProject) return;
        const newProjects = config.projects.map((project: any) =>
            project.id === resolvedLaunchProject.id ? updater(project) : project
        );
        const newConfig = new main.AppConfig({ ...config, projects: newProjects });
        setConfig(newConfig);
        SaveConfig(newConfig);
    };
    const getSelectedProjectForRemote = () => resolvedLaunchProject?.path || "";
    const launchProjectSelectOptions = useMemo(() => {
        if (!resolvedLaunchProject) return launchProjectOptions;
        if (launchProjectOptions.some((p: any) => p.id === resolvedLaunchProject.id)) {
            return launchProjectOptions;
        }
        return [resolvedLaunchProject, ...launchProjectOptions];
    }, [launchProjectOptions, resolvedLaunchProject]);
    const normalizeIssueItems = (items: any): string[] => {
        if (!Array.isArray(items)) return [];
        return items.map((item: any) => {
            if (typeof item === 'string') return item;
            if (item?.message) return item.message;
            if (item?.detail) return item.detail;
            return JSON.stringify(item);
        });
    };


    const {
        remoteActivationStatus,
        remoteConnectionStatus,
        remoteToolReadiness,
        remotePTYProbe,
        remoteToolLaunchProbe,
        remoteSmokeReport,
        remoteSessions,
        remoteInputDrafts,
        setRemoteInputDrafts,
        remoteBusy,
        selectedRemoteTool,
        setSelectedRemoteTool,
        remoteToolMetadata,
        visibleRemoteTools,
        selectedRemoteToolInfo,
        selectedRemoteToolCanStart,
        selectedRemoteToolUnavailableReason,
        selectedRemoteToolBadges,
        remoteSuggestedAction,
        getRemoteToolLabel,
        getRemoteToolConfigHint,
        getRemoteToolSmokeHint,
        getRemoteReadinessDetail,
        getRemoteLaunchDetail,
        getRemoteSmokeDetail,
        refreshRemotePanel,
        refreshRemoteReadiness,
        refreshRemotePTYProbe,
        refreshRemoteLaunchProbe,
        runRemoteSmoke,
        activateRemoteWithEmail,
        reconnectRemote,
        startRemoteSession,
        quickStartRemoteSession,
        installSelectedRemoteTool,
        saveRemoteConfigField,
        sendRemoteInput,
        killRemoteSession,
        interruptRemoteSession,
        refreshSessionsOnly,
        clearRemoteActivationState,
        invitationCodeRequired,
        invitationCode,
        setInvitationCode,
        invitationCodeError,
        providers,
        selectedProvider,
        setSelectedProvider,
    } = useRemotePanel({
        config,
        setConfig,
        setToolStatuses,
        getSelectedProjectForRemote,
        selectedProjectForLaunch,
        navTab,
        translate,
        formatText,
        localizeText,
        showToastMessage,
        onDemandInstallingTool,
        setOnDemandInstallingTool,
    });
    const aiAssistant = useAIAssistant({ refreshSessionsOnly });

    // ── Refresh active background task count for sidebar badge ──
    const remoteSessionsRef = useRef(remoteSessions);
    remoteSessionsRef.current = remoteSessions;

    useEffect(() => {
        let cancelled = false;
        const refresh = () => {
            let count = (remoteSessionsRef.current || []).filter((s: any) => !TERMINAL_SESSION_STATUSES.has(String(s.status || s.summary?.status || "").toLowerCase())).length;
            void (async () => {
                try {
                    const loops = await ListBackgroundLoops();
                    if (!cancelled) count += (loops || []).filter((l: any) => l.status === 'running').length;
                } catch { /* ignore */ }
                try {
                    const tasks = await ListScheduledTasks();
                    if (!cancelled) count += (tasks || []).filter((t: any) => t.status === 'active').length;
                } catch { /* ignore */ }
                if (!cancelled) setActiveTaskCount(count > 99 ? 99 : count);
            })();
        };
        refresh();
        const timer = setInterval(refresh, 10000);
        return () => { cancelled = true; clearInterval(timer); };
    }, []);

    useEffect(() => {
        let cancelled = false;
        const normalizeUsage = (stat?: SidebarTokenUsageStat | null) => {
            const input = stat?.input_tokens ?? stat?.InputTokens ?? 0;
            const output = stat?.output_tokens ?? stat?.OutputTokens ?? 0;
            const total = stat?.total_tokens ?? stat?.TotalTokens ?? input + output;
            return { input, output, total };
        };
        const normalizeProviderState = (data?: {
            providers?: Array<{ name?: string; Name?: string }>;
            Providers?: Array<{ name?: string; Name?: string }>;
            current?: string;
            Current?: string;
        } | null) => {
            const list = (data?.providers ?? data?.Providers ?? [])
                .map((provider) => provider?.name ?? provider?.Name ?? '')
                .filter(Boolean);
            const current = data?.current ?? data?.Current ?? '';
            return { providers: list, current };
        };
        const getUsageForProvider = (usageMap: Record<string, SidebarTokenUsageStat>, provider: string) => {
            if (!provider) return { input: 0, output: 0, total: 0 };
            const direct = usageMap[provider];
            if (direct) return normalizeUsage(direct);
            for (const alias of sidebarProviderAliases[provider] || []) {
                const stat = usageMap[alias];
                if (stat) return normalizeUsage(stat);
            }
            return { input: 0, output: 0, total: 0 };
        };
        const hasUsage = (usageMap: Record<string, SidebarTokenUsageStat>, provider: string) => {
            return getUsageForProvider(usageMap, provider).total > 0;
        };
        const getPreferredProvider = (providerNames: string[], currentProviderName: string, usageMap: Record<string, SidebarTokenUsageStat>) => {
            const providerWithUsage = providerNames.find((provider) => hasUsage(usageMap, provider));
            if (currentProviderName && providerNames.includes(currentProviderName) && (hasUsage(usageMap, currentProviderName) || !providerWithUsage)) {
                return currentProviderName;
            }
            return providerWithUsage || currentProviderName || providerNames[0] || '';
        };
        const refreshSidebarTokenUsage = async () => {
            try {
                const [usageMap, providerState] = await Promise.all([
                    GetAllLLMTokenUsage() as Promise<Record<string, SidebarTokenUsageStat> | null>,
                    GetMaclawLLMProviders() as Promise<{
                        providers?: Array<{ name?: string; Name?: string }>;
                        Providers?: Array<{ name?: string; Name?: string }>;
                        current?: string;
                        Current?: string;
                    } | null>,
                ]);
                const normalizedMap = usageMap || {};
                const normalizedProviderState = normalizeProviderState(providerState);
                const providerNames = normalizedProviderState.providers.length > 0
                    ? normalizedProviderState.providers
                    : providers.map((provider) => provider.name).filter(Boolean);
                const currentProviderName = getPreferredProvider(
                    providerNames,
                    normalizedProviderState.current || selectedProvider || providers[0]?.name || '',
                    normalizedMap,
                );
                const currentProviderUsage = getUsageForProvider(normalizedMap, currentProviderName);
                if (!cancelled) {
                    setSidebarCurrentProviderTokenUsage({ provider: currentProviderName, ...currentProviderUsage });
                }
            } catch {
                if (!cancelled) {
                    setSidebarCurrentProviderTokenUsage({ provider: '', input: 0, output: 0, total: 0 });
                }
            }
        };
        void refreshSidebarTokenUsage();
        const onTokenUsageChanged = () => { void refreshSidebarTokenUsage(); };
        EventsOn("llm-token-usage-changed", onTokenUsageChanged);
        return () => {
            cancelled = true;
            EventsOff("llm-token-usage-changed");
        };
    }, [providers, selectedProvider]);

    const activeRemoteSessionForTool = useMemo(() => {
        return remoteSessions.find((session) => {
            if (session.tool !== activeTool) return false;
            const status = String(session.status || session.summary?.status || "").toLowerCase();
            return !TERMINAL_SESSION_STATUSES.has(status);
        }) || null;
    }, [remoteSessions, activeTool]);

    // Show onboarding wizard if remote registration is not done (checked once on startup).
    const onboardingRegCheckDone = useRef(false);
    useEffect(() => {
        if (onboardingRegCheckDone.current || !config || remoteActivationStatus === null) return;
        onboardingRegCheckDone.current = true;
        if (!remoteActivationStatus.activated && !config.onboarding_done) {
            setShowMaclawLLMPopup(true);
            setShowStartupPopup(false);
        }
    }, [config, remoteActivationStatus]);
    const hasActiveRemoteSessionForTool = !!activeRemoteSessionForTool;

    useEffect(() => {
        setProjectCurrentPage(1);
    }, [projectSearchKeyword, projectSortMode]);

    useEffect(() => {
        if (projectCurrentPage !== safeProjectCurrentPage) {
            setProjectCurrentPage(safeProjectCurrentPage);
        }
    }, [projectCurrentPage, safeProjectCurrentPage]);

    useEffect(() => {
        if (!config?.projects || config.projects.length === 0) {
            if (selectedProjectForLaunch !== "") setSelectedProjectForLaunch("");
            return;
        }
        const exists = config.projects.some((p: any) => p.id === selectedProjectForLaunch);
        if (!exists) {
            const fallback = config.projects.find((p: any) => p.id === config.current_project) || config.projects[0];
            setSelectedProjectForLaunch(fallback.id);
        }
    }, [config, selectedProjectForLaunch]);

    // Extract provider name from model name
    // Examples: "AICodeMirror-Claude" -> "AICodeMirror", "Doubao-Codex" -> "Doubao", "GLM" -> "GLM"
    const getProviderPrefix = (modelName: string): string => {
        // Match pattern like "Provider-Tool" (e.g., "AICodeMirror-Claude", "DeepSeek-Codex")
        const match = modelName.match(/^(.+?)-(Claude|Gemini|Codex)$/i);
        if (match) {
            return match[1];
        }
        // For names without tool suffix, return the full name as provider
        return modelName;
    };

    const handleApiKeyChange = (newKey: string) => {
        if (!config) return;

        // Deep clone the entire config
        const configCopy = JSON.parse(JSON.stringify(config));

        // Get current model info
        const currentModel = configCopy[activeTool].models[activeTab];
        const currentModelName = currentModel.model_name;
        const isCurrentCustom = currentModel.is_custom;

        // Update current model's API key
        configCopy[activeTool].models[activeTab].api_key = newKey;

        const providerPrefix = getProviderPrefix(currentModelName);

        // Skip syncing for "Original" model and custom models
        if (providerPrefix !== "Original" && !isCurrentCustom) {
            TOOL_NAMES.forEach(tool => {
                if (configCopy[tool] && configCopy[tool].models && Array.isArray(configCopy[tool].models)) {
                    configCopy[tool].models.forEach((model: any, index: number) => {
                        if (tool === activeTool && index === activeTab) return;
                        if (model.is_custom) return;

                        const modelProvider = getProviderPrefix(model.model_name);
                        if (modelProvider === providerPrefix) {
                            configCopy[tool].models[index].api_key = newKey;
                        }
                    });
                }
            });
        }

        const newConfig = new main.AppConfig(configCopy);
        setConfig(newConfig);
    };

    const handleDeleteModel = () => {
        if (!config) return;
        const toolCfg = JSON.parse(JSON.stringify((config as any)[activeTool]));
        const modelToDelete = toolCfg.models[activeTab];
        if (modelToDelete.model_name === "Original") return;

        const message = t("confirmDeleteMessage").replace("{name}", modelToDelete.model_name);

        setConfirmDialog({
            show: true,
            title: t("confirmDelete"),
            message: message,
            onConfirm: () => {
                const newModels = toolCfg.models.filter((_: any, i: number) => i !== activeTab);
                const newConfig = new main.AppConfig({ ...config, [activeTool]: { ...toolCfg, models: newModels } });

                // Adjust active tab if it was the last one
                const newActiveTab = Math.max(0, activeTab - 1);
                setActiveTab(newActiveTab);

                setConfig(newConfig);
                setConfirmDialog({ ...confirmDialog, show: false });
                // We don't save immediately here to allow user to cancel or make other changes,
                // but the "Save Changes" button will call SaveConfig which triggers sync.
                // Actually, for sync to work, we need to save.
            }
        });
    };

    const handleModelUrlChange = (newUrl: string) => {
        if (!config) return;
        const toolCfg = JSON.parse(JSON.stringify((config as any)[activeTool]));
        toolCfg.models[activeTab].model_url = newUrl;
        const newConfig = new main.AppConfig({ ...config, [activeTool]: toolCfg });
        setConfig(newConfig);
    };

    // Get protocol type for current tool
    const getToolProtocol = (): 'anthropic' | 'gemini' | 'openai' => {
        if (activeTool === 'claude') {
            return 'anthropic';
        } else if (activeTool === 'gemini') {
            return 'gemini';
        } else {
            return 'openai'; // codex, opencode, codebuddy, iflow, kilo
        }
    };

    // Filter providers by current tool's protocol, excluding those already in the model list
    const getFilteredProviders = (): ProviderEndpoint[] => {
        const protocol = getToolProtocol();
        let filtered = knownProviderEndpoints.filter(p => p.protocol === protocol);

        if (providerFilter !== 'all') {
            filtered = filtered.filter(p => p.region === providerFilter);
        }

        // Exclude providers whose name already exists as a non-custom model
        if (config) {
            const toolCfg = (config as any)[activeTool];
            if (toolCfg?.models) {
                const existingNames = new Set(
                    toolCfg.models
                        .filter((m: any) => !m.is_custom)
                        .map((m: any) => (m.model_name || '').toLowerCase().trim())
                );
                filtered = filtered.filter(p => !existingNames.has(p.name.toLowerCase().trim()));
            }
        }

        return filtered;
    };

    // Handle provider selection
    const handleProviderSelect = (provider: ProviderEndpoint) => {
        setSelectedProviderForUrl(provider);
    };

    // Confirm provider selection and fill URL
    const confirmProviderSelection = () => {
        if (selectedProviderForUrl) {
            handleModelUrlChange(selectedProviderForUrl.url);
            setShowProviderSelector(false);
            setSelectedProviderForUrl(null);
        }
    };

    const handleModelNameChange = (name: string) => {
        if (!config) return;
        const toolCfg = JSON.parse(JSON.stringify((config as any)[activeTool]));
        const currentModel = toolCfg.models[activeTab];
        const oldName = currentModel.model_name;
        
        // Check for duplicate names (case-insensitive)
        const nameLower = name.toLowerCase().trim();
        const isDuplicate = toolCfg.models.some((m: any, idx: number) => {
            if (idx === activeTab) return false; // Skip current model
            return m.model_name.toLowerCase().trim() === nameLower;
        });
        
        if (isDuplicate) {
            // Show warning and don't update
            setStatus(lang === 'zh-Hans' ? `名称 "${name}" 已存在，请使用其他名称` : `Name "${name}" already exists, please use a different name`);
            return;
        }
        
        currentModel.model_name = name;
        
        // If the renamed model is the current_model, update current_model as well
        if (toolCfg.current_model === oldName) {
            toolCfg.current_model = name;
        }
        
        const newConfig = new main.AppConfig({ ...config, [activeTool]: toolCfg });
        setConfig(newConfig);
    };

    const handleModelIdChange = (id: string) => {
        if (!config) return;
        const toolCfg = JSON.parse(JSON.stringify((config as any)[activeTool]));
        toolCfg.models[activeTab].model_id = id;
        const newConfig = new main.AppConfig({ ...config, [activeTool]: toolCfg });
        setConfig(newConfig);
    };

    const handleWireApiChange = (api: string) => {
        if (!config) return;
        const toolCfg = JSON.parse(JSON.stringify((config as any)[activeTool]));
        toolCfg.models[activeTab].wire_api = api;
        const newConfig = new main.AppConfig({ ...config, [activeTool]: toolCfg });
        setConfig(newConfig);
    };

    const getDefaultModelId = (tool: string, provider: string) => {
        const p = provider.toLowerCase();
        if (tool === "claude") {
            if (p.includes("glm")) return "glm-4.7";
            if (p.includes("kimi")) return "kimi-k2-thinking";
            if (p.includes("doubao")) return "doubao-seed-code-preview-latest";
            if (p.includes("minimax")) return "MiniMax-M2.1";
            if (p.includes("aigocode")) return "claude-3-5-sonnet-20241022";
            if (p.includes("aicodemirror")) return "Haiku";
            if (p.includes("coderelay")) return "claude-3-5-sonnet-20241022";
            if (p.includes("摩尔线程")) return "GLM-4.7";
            if (p.includes("快手")) return "kat-coder-pro-v1";
        } else if (tool === "gemini") {
            return "gemini-2.0-flash-exp";
        } else if (tool === "codex") {
            if (p.includes("aigocode") || p.includes("aicodemirror") || p.includes("coderelay")) return "gpt-5.2-codex";
            if (p.includes("deepseek")) return "deepseek-chat";
            if (p.includes("glm")) return "glm-4.7";
            if (p.includes("doubao")) return "doubao-seed-code-preview-latest";
            if (p.includes("kimi")) return "kimi-for-coding";
            if (p.includes("minimax")) return "MiniMax-M2.1";
        } else if (tool === "opencode" || tool === "codebuddy" || tool === "iflow" || tool === "kilo") {
            if (p.includes("deepseek")) return "deepseek-chat";
            if (p.includes("glm")) return "glm-4.7";
            if (p.includes("doubao")) return "doubao-seed-code-preview-latest";
            if (p.includes("kimi")) return "kimi-for-coding";
            if (p.includes("minimax")) return "MiniMax-M2.1";
            if (p.includes("摩尔线程")) return "GLM-4.7";
            if (p.includes("快手")) return "kat-coder-pro-v1";
        }
        return "";
    };

    const handleModelSwitch = (modelName: string) => {
        if (!config) return;

        const toolCfg = (config as any)[activeTool];
        const targetModel = toolCfg.models.find((m: any) => m.model_name === modelName);
        if (modelName !== "Original" && (!targetModel || !targetModel.api_key || targetModel.api_key.trim() === "")) {
            setStatus("Please configure API Key first!");
            const idx = toolCfg.models.findIndex((m: any) => m.model_name === modelName);
            if (idx !== -1) setActiveTab(idx);

            setShowModelSettings(true);
            setTimeout(() => setStatus(""), 2000);
            return;
        }

        const newToolCfg = { ...toolCfg, current_model: modelName };
        const newConfig = new main.AppConfig({ ...config, [activeTool]: newToolCfg });
        setConfig(newConfig);
        setStatus(t("syncing"));
        SaveConfig(newConfig).then(() => {
            setStatus(t("switched"));
            setTimeout(() => setStatus(""), 1500);
        }).catch(err => {
            setStatus("Error syncing: " + err);
        });
    };

    const getCurrentProject = () => {
        if (!config || !config.projects) return null;
        return config.projects.find((p: any) => p.id === config.current_project) || config.projects[0];
    };

    const handleProjectSwitch = (projectId: string) => {
        if (!config) return;
        const newConfig = new main.AppConfig({ ...config, current_project: projectId });
        setConfig(newConfig);
        setSelectedProjectForLaunch(projectId);
        setStatus(t("projectSwitched"));
        setTimeout(() => setStatus(""), 1500);
        SaveConfig(newConfig);
    };

    const handleSelectDir = () => {
        if (!config) return;
        SelectProjectDir().then((dir) => {
            if (dir && dir.length > 0) {
                const currentProj = getCurrentProject();
                if (!currentProj) return;

                const newProjects = config.projects.map((p: any) =>
                    p.id === currentProj.id ? { ...p, path: dir } : p
                );

                const newConfig = new main.AppConfig({ ...config, projects: newProjects, project_dir: dir });
                setConfig(newConfig);
                setStatus(t("dirUpdated"));
                setTimeout(() => setStatus(""), 1500);
                SaveConfig(newConfig);
            }
        });
    };

    const handleYoloChange = (checked: boolean) => {
        if (!config) return;
        const currentProj = getCurrentProject();
        if (!currentProj) return;

        const newProjects = config.projects.map((p: any) =>
            p.id === currentProj.id ? { ...p, yolo_mode: checked } : p
        );

        const newConfig = new main.AppConfig({ ...config, projects: newProjects });
        setConfig(newConfig);
        setStatus(t("saved"));
        setTimeout(() => setStatus(""), 1500);
        SaveConfig(newConfig);
    };

    const openRemoteActivationModal = (toolName: string) => {
        const nextHubCenterURL = config?.remote_hubcenter_url || "";
        const nextEmail = config?.remote_email || "";
        setPendingRemoteLaunchTool(toolName);
        setRemoteActivationDraft({
            hub_url: config?.remote_hub_url || "",
            hubcenter_url: nextHubCenterURL,
            email: nextEmail,
        });
        setRemoteCenterHubs([]);
        setShowRemoteActivationModal(true);
        if (nextEmail.trim()) {
            void loadRemoteHubsFromCenter(nextHubCenterURL, nextEmail, false);
        }
    };

    const loadRemoteHubsFromCenter = async (centerURLArg?: string, emailArg?: string, notifyOnEmpty = true) => {
        const centerURL = (centerURLArg ?? remoteActivationDraft.hubcenter_url).trim();
        const email = (emailArg ?? remoteActivationDraft.email).trim();
        if (!email) {
            showToastMessage(t("remoteEmailRequired"), 3000);
            return;
        }
        setLoadingRemoteCenterHubs(true);
        try {
            const hubs = await ListRemoteHubs(centerURL, email) as RemoteCenterHubOption[];
            setRemoteCenterHubs(Array.isArray(hubs) ? hubs : []);
            if (Array.isArray(hubs) && hubs.length > 0) {
                setRemoteActivationDraft((prev) => {
                    if (prev.hub_url.trim()) {
                        return prev;
                    }
                    return { ...prev, hub_url: hubs[0].base_url || "" };
                });
            } else if (notifyOnEmpty) {
                showToastMessage(t("remoteNoRegisteredHubs"), 3000);
            }
        } catch (err) {
            console.error("Failed to load remote hubs from center:", err);
            setRemoteCenterHubs([]);
            showToastMessage(formatText("remoteLoadHubListFailed", { error: String(err) }), 4000);
        } finally {
            setLoadingRemoteCenterHubs(false);
        }
    };

    const activateRemoteFromDialog = async () => {
        if (!config) return;
        const hubURL = remoteActivationDraft.hub_url.trim();
        const hubCenterURL = remoteActivationDraft.hubcenter_url.trim();
        const email = remoteActivationDraft.email.trim();
        const newConfig = new main.AppConfig({
            ...config,
            remote_hub_url: hubURL,
            remote_hubcenter_url: hubCenterURL,
            remote_email: email,
            remote_enabled: true,
        });
        setConfig(newConfig);
        await SaveConfig(newConfig);
        const activated = await activateRemoteWithEmail();
        if (!activated) {
            return;
        }
        setShowRemoteActivationModal(false);
        if (pendingRemoteLaunchTool) {
            setStatus(lang === 'zh-Hans' ? '正在远程启动...' : lang === 'zh-Hant' ? '正在遠端啟動...' : 'Starting remotely...');
            setLaunchingTool(pendingRemoteLaunchTool);
            await quickStartRemoteSession(pendingRemoteLaunchTool as any);
            setPendingRemoteLaunchTool("");
            setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
        }
    };

    const handleAddNewProject = async () => {
        if (!config) return;

        let baseName = "Project";
        let newName = "";
        let i = 1;
        while (true) {
            newName = `${baseName} ${i}`;
            if (!config.projects.some((p: any) => p.name === newName)) break;
            i++;
        }

        const homeDir = await GetUserHomeDir();
        const newId = Math.random().toString(36).substr(2, 9);
        const newProject = {
            id: newId,
            name: newName,
            path: homeDir || "",
            yolo_mode: false
        };

        const newProjects = [...config.projects, newProject];
        const newConfig = new main.AppConfig({ ...config, projects: newProjects });
        setConfig(newConfig);
        SaveConfig(newConfig);
        setStatus(t("saved"));
        setTimeout(() => setStatus(""), 1500);
    };

    const handleOpenSubscribe = (modelName: string) => {
        const url = subscriptionUrls[modelName];
        if (url) {
            BrowserOpenURL(url);
        }
    };

    const save = () => {
        if (!config) return;

        // Sanitize: Ensure Custom models have a name (prevent empty tab button)
        const configCopy = JSON.parse(JSON.stringify(config));
        TOOL_NAMES.forEach(tool => {
            if (configCopy[tool] && configCopy[tool].models) {
                configCopy[tool].models.forEach((model: any) => {
                    if (model.is_custom && (!model.model_name || model.model_name.trim() === '')) {
                        model.model_name = 'Custom';
                    }
                });
            }
        });

        const sanitizedConfig = new main.AppConfig(configCopy);
        setConfig(sanitizedConfig);

        setStatus(t("saving"));
        SaveConfig(sanitizedConfig).then(() => {
            setStatus(t("saved"));
            setTimeout(() => {
                setStatus("");
                setShowModelSettings(false);
            }, 1000);
        }).catch(err => {
            setStatus("Error saving: " + err);
        });
    };

    const performSendLog = async () => {
        const subject = t("sendLogSubject");
        const logContent = envLogs.join('\n');

        try {
            // Get correct OS info from backend with fallback
            let sysInfo = { os: "unknown", arch: "unknown", os_version: "unknown" };
            try {
                sysInfo = await GetSystemInfo();
            } catch (e) {
                console.error("GetSystemInfo failed:", e);
                // Fallback if backend call fails
                sysInfo.os = /mac/i.test(navigator.platform) ? "darwin" : navigator.platform;
            }

            // Pack log to zip
            const zipPath = await PackLog(logContent);

            // Show in folder
            await ShowItemInFolder(zipPath);

            // Prepare mailto body
            const instruction = lang === 'zh-Hans'
                ? `请将刚刚打开的文件夹中的压缩包（aicoder_log_....zip）作为附件添加到此邮件中发送。\n\n`
                : lang === 'zh-Hant'
                    ? `請將剛剛打開的文件夾中的壓縮包（aicoder_log_....zip）作為附件添加到此郵件中發送。\n\n`
                    : `Please attach the zip file (aicoder_log_....zip) from the opened folder to this email.\n\n`;

            const body = `Product: ${brandInfo?.displayName || 'MaClaw'}
Version: ${appVersion}

System Information:
OS: ${sysInfo.os}
OS Version: ${sysInfo.os_version}
Architecture: ${sysInfo.arch}

${instruction}`;

            const mailtoLink = `mailto:znsoft@163.com?subject=${encodeURIComponent(subject)}&body=${encodeURIComponent(body)}`;

            await OpenSystemUrl(mailtoLink);
        } catch (e) {
            console.error("Failed to pack/send log:", e);
            showAlert("Failed to send log: " + e);
        }
    };

    if (isLoading) {
        return (
            <div style={{
                height: '100vh',
                display: 'flex',
                flexDirection: 'column',
                justifyContent: 'center',
                alignItems: 'center',
                backgroundColor: 'var(--theme-surface)',
                color: 'var(--theme-text-primary)',
                padding: '20px',
                textAlign: 'center',
                boxSizing: 'border-box',
                borderRadius: '12px',
                border: '1px solid var(--theme-border)',
                overflow: 'hidden'
            }}>
                <div style={{
                    height: '30px',
                    width: '100%',
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    zIndex: 999,
                    '--wails-draggable': 'drag'
                } as any}></div>
                <h2 style={{
                    background: 'linear-gradient(135deg, var(--theme-primary), var(--theme-primary-strong), var(--theme-link-color))',
                    WebkitBackgroundClip: 'text',
                    WebkitTextFillColor: 'transparent',
                    marginBottom: '20px',
                    display: 'inline-block',
                    fontWeight: 'bold'
                }}>{t("envCheckTitle")}</h2>
                <div style={{ width: '100%', height: '4px', backgroundColor: 'var(--theme-surface-muted)', borderRadius: '2px', overflow: 'hidden', marginBottom: '15px' }}>
                    <div style={{
                        width: '50%',
                        height: '100%',
                        backgroundColor: 'var(--theme-primary)',
                        borderRadius: '2px',
                        animation: 'indeterminate 1.5s infinite linear'
                    }}></div>
                </div>

                {showLogs ? (
                    <textarea
                        ref={logEndRef}
                        readOnly
                        value={envLogs.join('\n')}
                        style={{
                            width: '100%',
                            height: '240px',
                            padding: '10px',
                            fontSize: '0.85rem',
                            fontFamily: 'monospace',
                            color: 'var(--theme-text-secondary)',
                            backgroundColor: 'var(--theme-surface-muted)',
                            border: '1px solid var(--theme-border)',
                            borderRadius: '8px',
                            resize: 'none',
                            outline: 'none',
                            marginBottom: '10px'
                        }}
                    />
                ) : (
                    <div style={{ fontSize: '0.9rem', color: 'var(--theme-text-muted)', marginBottom: '15px', height: '20px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {envLogs.length > 0 ? envLogs[envLogs.length - 1] : t("initializing")}
                    </div>
                )}

                <div style={{ display: 'flex', gap: '15px', alignItems: 'center' }}>
                    <button
                        onClick={() => setShowLogs(!showLogs)}
                        style={{
                            background: 'none',
                            border: 'none',
                            color: 'var(--theme-link-color)',
                            fontSize: '0.8rem',
                            cursor: 'pointer',
                            textDecoration: 'underline'
                        }}
                    >
                        {showLogs ? (lang === 'zh-Hans' ? '隐藏详情' : 'Hide Details') : (lang === 'zh-Hans' ? '查看详情' : 'Show Details')}
                    </button>

                    {showLogs && (
                        isManualCheck ? (
                            <button onClick={() => {
                                setIsLoading(false);
                                setIsManualCheck(false);
                            }} className="btn-hide" style={{ borderColor: 'var(--theme-link-color)', color: 'var(--theme-link-color)', padding: '4px 12px' }}>
                                {t("close")}
                            </button>
                        ) : (
                            <button onClick={Quit} className="btn-hide" style={{ borderColor: 'var(--theme-danger)', color: 'var(--theme-danger)', padding: '4px 12px' }}>
                                {lang === 'zh-Hans' ? '退出程序' : 'Quit'}
                            </button>
                        )
                    )}
                </div>

                <style>{`
                    @keyframes indeterminate {
                        0% { transform: translateX(-100%); }
                        100% { transform: translateX(200%); }
                    }
                `}</style>
            </div>
        );
    }

    if (!config) return <div className="main-content" style={{ display: 'flex', justifyContent: 'center', alignItems: 'center' }}>{t("loadingConfig")}</div>;

    const toolCfg = isToolTab(navTab)
        ? (config as any)[navTab]
        : null;

    const currentProject = getCurrentProject();
    const settingsTabOptions = [
        {
            id: 'general' as const,
            label: lang === 'zh-Hans' ? '通用设置' : lang === 'zh-Hant' ? '通用設定' : 'General',
            desc: lang === 'zh-Hans' ? '语言、项目与运行环境' : lang === 'zh-Hant' ? '語言、專案與執行環境' : 'Language, projects, and environment',
        },
        {
            id: 'proxy' as const,
            label: lang === 'zh-Hans' ? '代理设置' : lang === 'zh-Hant' ? '代理設定' : 'Proxy',
            desc: lang === 'zh-Hans' ? '全局网络代理配置' : lang === 'zh-Hant' ? '全域網路代理配置' : 'Global network proxy configuration',
        },
        {
            id: 'ui' as const,
            label: lang === 'zh-Hans' ? '界面设置' : lang === 'zh-Hant' ? '介面設定' : 'UI Config',
            desc: lang === 'zh-Hans' ? '界面缩放与显示行为' : lang === 'zh-Hant' ? '介面縮放與顯示行為' : 'UI scaling and display behavior',
        },
        {
            id: 'display' as const,
            label: lang === 'zh-Hans' ? '开发工具' : lang === 'zh-Hant' ? '開發工具' : 'Dev CLI',
            desc: lang === 'zh-Hans' ? '工具显示与启动行为' : lang === 'zh-Hant' ? '工具顯示與啟動行為' : 'Tool visibility and startup behavior',
        },
        {
            id: 'remote' as const,
            label: lang === 'zh-Hans' ? '远程连接' : lang === 'zh-Hant' ? '遠端連線' : 'Remote',
            desc: lang === 'zh-Hans' ? '远程服务器地址与连接入口' : lang === 'zh-Hant' ? '遠端伺服器位址與連線入口' : 'Server addresses only',
        },
        {
            id: 'llm' as const,
            label: lang === 'zh-Hans' ? 'LLM 配置' : lang === 'zh-Hant' ? 'LLM 配置' : 'LLM Config',
            desc: lang === 'zh-Hans' ? '配置 MaClaw 使用的大模型服务' : lang === 'zh-Hant' ? '配置 MaClaw 使用的大模型服務' : 'Configure LLM for MaClaw agent',
        },
        {
            id: 'serviceRedeem' as const,
            label: lang === 'zh-Hans' ? '服务兑换' : lang === 'zh-Hant' ? '服務兌換' : 'Service Redeem',
            desc: lang === 'zh-Hans' ? '兑换充值卡并查看 Hub 模型服务授权' : lang === 'zh-Hant' ? '兌換儲值卡並查看 Hub 模型服務授權' : 'Redeem service cards and view Hub model service grants',
        },
        {
            id: 'search' as const,
            label: lang === 'zh-Hans' ? '搜索引擎' : lang === 'zh-Hant' ? '搜尋引擎' : 'Search Engine',
            desc: lang === 'zh-Hans' ? '配置搜索服务商与 API Key' : lang === 'zh-Hant' ? '配置搜尋服務商與 API Key' : 'Configure web search providers and API keys',
        },
        {
            id: 'role' as const,
            label: lang === 'zh-Hans' ? 'MaClaw 角色' : lang === 'zh-Hant' ? 'MaClaw 角色' : 'MaClaw Role',
            desc: lang === 'zh-Hans' ? '自定义 MaClaw Agent 名称与角色描述' : lang === 'zh-Hant' ? '自訂 MaClaw Agent 名稱與角色描述' : 'Customize MaClaw Agent name and role description',
        },
        {
            id: 'memory' as const,
            label: lang === 'zh-Hans' ? '记忆管理' : lang === 'zh-Hant' ? '記憶管理' : 'Memory',
            desc: lang === 'zh-Hans' ? '查看、编辑和管理 MaClaw 长期记忆' : lang === 'zh-Hant' ? '查看、編輯和管理 MaClaw 長期記憶' : 'View, edit and manage MaClaw long-term memory',
        },
        {
            id: 'embedding' as const,
            label: lang === 'zh-Hans' ? '嵌入模型' : lang === 'zh-Hant' ? '嵌入模型' : 'AI Model',
            desc: lang === 'zh-Hans' ? '向量检索与嵌入模型管理' : lang === 'zh-Hant' ? '向量檢索與嵌入模型管理' : 'Vector search and embedding model management',
        },
        {
            id: 'agentnet' as const,
            label: lang === 'zh-Hans' ? 'AgentNet' : lang === 'zh-Hant' ? 'AgentNet' : 'AgentNet',
            desc: lang === 'zh-Hans' ? 'AgentNet 去中心化 P2P Agent 网络' : lang === 'zh-Hant' ? 'AgentNet 去中心化 P2P Agent 網路' : 'AgentNet decentralized P2P agent network',
        },
        {
            id: 'im' as const,
            label: 'IM',
            desc: lang === 'zh-Hans' ? '配置 QQ Bot、Telegram Bot、微信等 IM 集成' : lang === 'zh-Hant' ? '配置 QQ Bot、Telegram Bot、微信等 IM 整合' : 'Configure QQ Bot, Telegram Bot, WeChat and other IM integrations',
        },
        {
            id: 'security' as const,
            label: lang === 'zh-Hans' ? '安全管理' : lang === 'zh-Hant' ? '安全管理' : 'Security',
            desc: lang === 'zh-Hans' ? '安全策略模式与审计日志' : lang === 'zh-Hant' ? '安全策略模式與稽核日誌' : 'Security policy mode and audit log',
        },
        {
            id: 'system' as const,
            label: lang === 'zh-Hans' ? '系统设置' : lang === 'zh-Hant' ? '系統設定' : 'System',
            desc: lang === 'zh-Hans' ? '心跳、熄屏等系统行为设置' : lang === 'zh-Hant' ? '心跳、熄屏等系統行為設定' : 'Heartbeat, screen dimming and other system settings',
        },
    ];
    const isRemoteCapableActiveTool = remoteToolMetadata.some(
        (meta) => meta.name === activeTool && meta.supports_remote === true
    );
    const isLiteMode = config?.ui_mode !== 'pro';

    return (
        <div
            className="app-viewport"
            style={{ ['--ui-scale' as any]: String(uiZoom) } as React.CSSProperties}
        >
            <div className="app-scale-layer">
                <div id="App">
            <div style={{
                height: '30px',
                width: isLiteMode ? '60px' : '180px',
                position: 'absolute',
                top: 0,
                left: 0,
                zIndex: 999,
                '--wails-draggable': 'drag'
            } as any}></div>

            <div className="sidebar" style={{ '--wails-draggable': 'no-drag', flexDirection: 'row', padding: 0, width: isLiteMode ? '60px' : '156px' } as any} data-ai-theme={aiThemeMode}>
                {/* Left Navigation Strip */}
                <div style={{
                    width: '60px',
                    borderRight: '1px solid var(--theme-border)',
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    padding: '6px 0',
                    background: aiThemeMode === 'dark'
                        ? 'linear-gradient(180deg, var(--theme-surface) 0%, var(--theme-page-bg) 68px, var(--theme-page-bg) 100%)'
                        : 'linear-gradient(180deg, var(--theme-surface) 0%, var(--theme-page-bg) 68px, var(--theme-page-bg) 100%)',
                    flexShrink: 0
                }}>
                    <div className="sidebar-header" style={{ padding: '10px 0 14px 0', justifyContent: 'center', width: '100%' }}>
                        <img src={currentIcon} alt="Logo" className="sidebar-logo" style={{ width: '44px', height: '44px', filter: 'drop-shadow(0 3px 10px rgba(217, 75, 61, 0.18))' }} />
                    </div>

                    <div
                        className={`sidebar-item left-nav-item ${navTab === 'remote' ? 'active' : ''}`}
                        onClick={() => switchTool('remote')}
                        style={{ flexDirection: 'column', padding: '6px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'remote' ? '3px solid var(--theme-text-muted)' : '3px solid transparent', justifyContent: 'center', position: 'relative' }}
                        title={lang === 'zh-Hans' ? '任务' : lang === 'zh-Hant' ? '任務' : 'Tasks'}
                    >
                        <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem', position: 'relative' }}>
                            📡
                            {activeTaskCount > 0 && (
                                <span style={{
                                    position: 'absolute',
                                    top: '-5px',
                                    right: '-8px',
                                    minWidth: '16px',
                                    height: '16px',
                                    lineHeight: '16px',
                                    fontSize: '9px',
                                    fontWeight: 700,
                                    textAlign: 'center',
                                    padding: activeTaskCount > 99 ? '0 2px' : '0 3px',
                                    borderRadius: '999px',
                                    background: 'var(--theme-danger)',
                                    color: '#ffffff',
                                    boxShadow: '0 1px 3px rgba(0,0,0,0.3)',
                                    zIndex: 10,
                                }}>
                                    {activeTaskCount > 99 ? '99+' : activeTaskCount}
                                </span>
                            )}
                        </span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{lang === 'zh-Hans' ? '任务' : lang === 'zh-Hant' ? '任務' : 'Tasks'}</span>
                    </div>

                    <div
                        className={`sidebar-item left-nav-item ${navTab === 'skills' ? 'active' : ''}`}
                        onClick={() => { switchTool('skills'); }}
                        style={{ flexDirection: 'column', padding: '6px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'skills' ? '3px solid var(--theme-text-muted)' : '3px solid transparent', justifyContent: 'center' }}
                        title={t("skills")}
                    >
                        <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem' }}>🧩</span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{t("skills")}</span>
                    </div>
                    <div
                        className={`sidebar-item left-nav-item ${navTab === 'mcp' ? 'active' : ''}`}
                        onClick={() => { switchTool('mcp'); }}
                        style={{ flexDirection: 'column', padding: '6px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'mcp' ? '3px solid var(--theme-text-muted)' : '3px solid transparent', justifyContent: 'center' }}
                        title="MCP"
                    >
                        <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem' }}>🔌</span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>MCP</span>
                    </div>

                    <div
                        className={`sidebar-item left-nav-item left-nav-item--ai ${navTab === 'ai' ? 'active' : ''}`}
                        onClick={() => { switchTool('ai'); }}
                        style={{
                            flexDirection: 'column', padding: '6px 0', width: '100%', gap: '4px',
                            borderLeft: 'none',
                            borderRight: navTab === 'ai' ? '3px solid var(--primary-color)' : '3px solid transparent',
                            justifyContent: 'center'
                        }}
                        title={lang === 'zh-Hans' ? 'AI 助手' : lang === 'zh-Hant' ? 'AI 助手' : 'AI Asst'}
                    >
                        <span className="sidebar-icon" title={(() => {
                            const llmOk = maclawLLMOnline;
                            const netOk = agentNetRunning;
                            const mobileOk = !!remoteActivationStatus?.activated;
                            if (isLiteMode) {
                                if (llmOk && netOk) return localizeText('All online', '全部在线', '全部在線');
                                const parts: string[] = [];
                                parts.push(llmOk ? 'LLM ✓' : 'LLM ✗');
                                parts.push(netOk ? localizeText('AgentNet ✓', '智网 ✓', '智網 ✓') : localizeText('AgentNet ✗', '智网 ✗', '智網 ✗'));
                                return parts.join('  ');
                            }
                            if (llmOk && netOk && mobileOk) return localizeText('All online', '全部在线', '全部在線');
                            const parts: string[] = [];
                            parts.push(llmOk ? 'LLM ✓' : 'LLM ✗');
                            parts.push(netOk ? localizeText('AgentNet ✓', '智网 ✓', '智網 ✓') : localizeText('AgentNet ✗', '智网 ✗', '智網 ✗'));
                            parts.push(mobileOk ? localizeText('Mobile ✓', '移动端 ✓', '移動端 ✓') : localizeText('Mobile ✗', '移动端 ✗', '移動端 ✗'));
                            return parts.join('  ');
                        })()} style={{
                            margin: 0,
                            display: 'inline-flex', alignItems: 'center', justifyContent: 'center',
                            width: '2rem', height: '2rem',
                            borderRadius: '50%',
                            padding: '3px',
                            background: (() => {
                                const llm = maclawLLMOnline;
                                const net = agentNetRunning;
                                if (isLiteMode) {
                                    return llm && net ? 'var(--theme-primary-strong)' : (!llm && !net ? 'var(--theme-text-muted)' : llm ? 'var(--theme-primary)' : 'var(--theme-text-muted)');
                                }
                                const mob = remoteActivationStatus?.activated;
                                return (llm && net && mob) ? 'var(--theme-primary-strong)' : (!llm && !net && !mob) ? 'var(--theme-text-muted)' : 'var(--theme-primary)';
                            })(),
                            boxShadow: (() => {
                                const allOn = isLiteMode
                                    ? (maclawLLMOnline && agentNetRunning)
                                    : (maclawLLMOnline && agentNetRunning && !!remoteActivationStatus?.activated);
                                const noneOn = isLiteMode
                                    ? (!maclawLLMOnline && !agentNetRunning)
                                    : (!maclawLLMOnline && !agentNetRunning && !remoteActivationStatus?.activated);
                                if (noneOn) return 'none';
                                if (allOn && navTab === 'ai') return '0 0 10px color-mix(in srgb, var(--theme-primary) 60%, transparent), 0 0 20px color-mix(in srgb, var(--theme-primary) 30%, transparent)';
                                if (allOn) return '0 0 6px color-mix(in srgb, var(--theme-primary) 40%, transparent), 0 0 12px color-mix(in srgb, var(--theme-primary) 15%, transparent)';
                                return '0 0 4px color-mix(in srgb, var(--theme-primary) 30%, transparent)';
                            })(),
                            transition: 'box-shadow 0.2s ease, background 0.3s ease'
                        }}>
                            <span style={{
                                display: 'flex', alignItems: 'center', justifyContent: 'center',
                                width: '100%', height: '100%',
                                borderRadius: '50%', background: aiThemeMode === 'dark' ? 'var(--theme-surface)' : 'var(--theme-surface)',
                                fontSize: '1.3rem', lineHeight: 1
                            }}>🦞</span>
                        </span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>
                            {lang === 'zh-Hans' ? 'AI 助手' : lang === 'zh-Hant' ? 'AI 助手' : 'AI Asst'}
                        </span>
                    </div>

                    <div style={{ flex: 1 }}></div>

                    {gossipAllowed && (
                        <div
                            className={`sidebar-item left-nav-item ${navTab === 'gossip' ? 'active' : ''}`}
                            onClick={() => { switchTool('gossip'); }}
                            style={{ flexDirection: 'column', padding: '6px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'gossip' ? '3px solid var(--theme-text-muted)' : '3px solid transparent', justifyContent: 'center' }}
                            title={t("gossip")}
                        >
                            <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem' }}>🗣️</span>
                            <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{t("gossip")}</span>
                        </div>
                    )}

                    <div
                        className={`sidebar-item left-nav-item ${navTab === 'agentnet' ? 'active' : ''}`}
                        onClick={() => { switchTool('agentnet'); }}
                        style={{ flexDirection: 'column', padding: '6px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'agentnet' ? '3px solid var(--theme-text-muted)' : '3px solid transparent', justifyContent: 'center' }}
                        title={lang === 'zh-Hans' ? '智网' : lang === 'zh-Hant' ? '智網' : 'AgentNet'}
                    >
                        <img src={agentnetIcon} alt="AgentNet" style={{ width: '22px', height: '22px', margin: 0 }} />
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{lang === 'zh-Hans' ? '智网' : lang === 'zh-Hant' ? '智網' : 'AgentNet'}</span>
                    </div>

                    <div
                        className={`sidebar-item left-nav-item ${navTab === 'settings' ? 'active' : ''}`}
                        onClick={() => { switchTool('settings'); }}
                        style={{ flexDirection: 'column', padding: '6px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'settings' ? '3px solid var(--theme-text-muted)' : '3px solid transparent', justifyContent: 'center' }}
                        title={t("settings")}
                    >
                        <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem' }}>⚙️</span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{t("settings")}</span>
                    </div>

                    <div
                        className={`sidebar-item left-nav-item ${navTab === 'about' ? 'active' : ''}`}
                        onClick={() => switchTool('about')}
                        style={{ flexDirection: 'column', padding: '6px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'about' ? '3px solid var(--theme-text-muted)' : '3px solid transparent', justifyContent: 'center' }}
                        title={t("about")}
                    >
                        <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem' }}>ℹ️</span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{t("about")}</span>
                    </div>
                </div>

                {/* Right Tool List */}
                <div style={{ flex: 1, padding: '5px 4px 4px', overflowY: 'auto', backgroundColor: 'var(--theme-page-bg)', display: isLiteMode ? 'none' : 'flex', flexDirection: 'column', minHeight: 0 }}>
                    <div style={{ width: '72%', height: '1px', background: 'linear-gradient(90deg, transparent, var(--theme-border), transparent)', margin: '0 auto 4px', flexShrink: 0, display: isLiteMode ? 'none' : undefined }}></div>
                    <div style={{ flex: 1, minHeight: 0, display: isLiteMode ? 'none' : 'flex', flexDirection: 'column', justifyContent: 'center' }}>
                    <div className="tool-grid" style={{ display: 'grid', gridTemplateColumns: '1fr', gap: '2px' }}>
                        <div className={`sidebar-item ${navTab === 'claude' ? 'active' : ''}`} onClick={() => switchTool('claude')}>
                            <span className="sidebar-icon">
                                <img src={claudecodeIcon} style={{ width: '1.4em', height: '1.4em', verticalAlign: 'middle' }} alt="Claude" />
                            </span> <span>Claude Code</span>
                        </div>
                        {config?.show_gemini !== false && (
                            <div className={`sidebar-item ${navTab === 'gemini' ? 'active' : ''}`} onClick={() => switchTool('gemini')}>
                                <span className="sidebar-icon">
                                    <img src={geminiIcon} style={{ width: '1.4em', height: '1.4em', verticalAlign: 'middle' }} alt="Gemini" />
                                </span> <span>Gemini CLI</span>
                            </div>
                        )}
                        {config?.show_codex !== false && (
                            <div className={`sidebar-item ${navTab === 'codex' ? 'active' : ''}`} onClick={() => switchTool('codex')}>
                                <span className="sidebar-icon">
                                    <img src={codexIcon} style={{ width: '1.4em', height: '1.4em', verticalAlign: 'middle' }} alt="Codex" />
                                </span> <span>CodeX</span>
                            </div>
                        )}
                        {config?.show_opencode !== false && (
                            <div className={`sidebar-item ${navTab === 'opencode' ? 'active' : ''}`} onClick={() => switchTool('opencode')}>
                                <span className="sidebar-icon">
                                    <img src={opencodeIcon} style={{ width: '1.4em', height: '1.4em', verticalAlign: 'middle' }} alt="OpenCode" />
                                </span> <span>OpenCode</span>
                            </div>
                        )}
                        {config?.show_codebuddy !== false && (
                            <div className={`sidebar-item ${navTab === 'codebuddy' ? 'active' : ''}`} onClick={() => switchTool('codebuddy')}>
                                <span className="sidebar-icon">
                                    <img src={codebuddyIcon} style={{ width: '1.4em', height: '1.4em', verticalAlign: 'middle' }} alt="CodeBuddy" />
                                </span> <span>CodeBuddy</span>
                            </div>
                        )}
                        {!isWindows && config?.show_cursor !== false && (
                            <div className={`sidebar-item ${navTab === 'cursor' ? 'active' : ''}`} onClick={() => switchTool('cursor')}>
                                <span className="sidebar-icon">
                                    <img src={cursorIcon} style={{ width: '1.4em', height: '1.4em', verticalAlign: 'middle' }} alt="Cursor" />
                                </span> <span>Cursor Agent</span>
                            </div>
                        )}
                        {config?.show_iflow !== false && (
                            <div className={`sidebar-item ${navTab === 'iflow' ? 'active' : ''}`} onClick={() => switchTool('iflow')}>
                                <span className="sidebar-icon">
                                    <img src={iflowIcon} style={{ width: '1.4em', height: '1.4em', verticalAlign: 'middle' }} alt="iFlow" />
                                </span> <span>iFlow CLI</span>
                            </div>
                        )}
                        {config?.show_kilo !== false && (
                            <div className={`sidebar-item ${navTab === 'kilo' ? 'active' : ''}`} onClick={() => switchTool('kilo')}>
                                <span className="sidebar-icon">
                                    <img src={kiloIcon} style={{ width: '1.4em', height: '1.4em', verticalAlign: 'middle' }} alt="Kilo Code" />
                                </span> <span>Kilo Code</span>
                            </div>
                        )}

                    </div>
                    </div>

                    {/* Status dashboard */}
                    <div style={{ flexShrink: 0, padding: '0 1px 0', marginTop: '0' }}>
                        <div style={{ width: '48%', height: '1px', background: aiThemeMode === 'dark' ? 'linear-gradient(90deg, transparent, rgba(71, 85, 105, 0.9), transparent)' : 'linear-gradient(90deg, transparent, rgba(225, 228, 232, 0.75), transparent)', margin: '1px auto 2px' }}></div>
                        <div style={{ width: '76px', margin: '0 auto', padding: '2px 2px', borderRadius: '5px', background: aiThemeMode === 'dark' ? 'rgba(15, 23, 42, 0.92)' : 'rgba(255,255,255,0.78)', border: aiThemeMode === 'dark' ? '1px solid rgba(71, 85, 105, 0.85)' : '1px solid rgba(225, 228, 232, 0.72)', minWidth: 0, boxShadow: aiThemeMode === 'dark' ? '0 6px 14px rgba(2, 6, 23, 0.28)' : 'none' }}>
                            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '2px', marginBottom: '2px' }}>
                                {[
                                    { label: 'LLM', on: maclawLLMOnline },
                                    { label: lang === 'zh-Hans' ? '智网' : lang === 'zh-Hant' ? '智網' : 'Net', on: agentNetRunning },
                                    { label: lang === 'zh-Hans' ? '移动' : lang === 'zh-Hant' ? '移動' : 'Mob', on: !!remoteActivationStatus?.activated },
                                    { label: 'IM', on: qqBotStatus === 'connected' || telegramStatus === 'connected' || weixinStatus === 'connected' || lansengerStatus === 'connected', link: 'im' },
                                ].map(({ label, on, link }) => (
                                    <div
                                        key={label}
                                        style={{
                                            display: 'flex',
                                            alignItems: 'center',
                                            justifyContent: 'center',
                                            gap: '2px',
                                            minWidth: 0,
                                            padding: '2px 0',
                                            borderRadius: '4px',
                                            background: aiThemeMode === 'dark'
                                                ? (on ? 'rgba(30, 41, 59, 0.96)' : 'rgba(15, 23, 42, 0.88)')
                                                : (on ? 'rgba(247, 248, 250, 0.92)' : 'rgba(247, 248, 250, 0.7)'),
                                            border: aiThemeMode === 'dark' ? '1px solid rgba(71, 85, 105, 0.72)' : '1px solid rgba(225, 228, 232, 0.72)',
                                            cursor: link ? 'pointer' : undefined,
                                        }}
                                        onClick={link ? () => { setNavTab('settings'); setSettingsTab(link as any); } : undefined}
                                        title={link && !on ? localizeText('Click to configure', '点击配置', '點擊配置') : undefined}
                                    >
                                        <span style={{ width: '4px', height: '4px', borderRadius: '50%', background: on ? 'var(--theme-primary-strong)' : (aiThemeMode === 'dark' ? 'rgba(148, 163, 184, 0.72)' : 'rgba(148, 163, 184, 0.65)'), boxShadow: on ? '0 0 4px color-mix(in srgb, var(--theme-primary-strong) 35%, transparent)' : 'none', display: 'inline-block' }}></span>
                                        <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: '0.42rem', color: on ? 'var(--theme-text-primary)' : 'var(--theme-text-muted)', fontWeight: 600 }}>{label}</span>
                                    </div>
                                ))}
                            </div>
                            <div style={{ marginBottom: '2px', minWidth: 0, borderRadius: '4px', background: aiThemeMode === 'dark' ? 'rgba(17, 24, 39, 0.96)' : 'rgba(247, 248, 250, 0.74)', border: aiThemeMode === 'dark' ? '1px solid rgba(71, 85, 105, 0.72)' : '1px solid rgba(225, 228, 232, 0.72)', padding: '3px 4px', display: 'grid', gridTemplateColumns: 'auto minmax(0, 1fr)', alignItems: 'center', gap: '4px' }}>
                                <div style={{ fontSize: '0.42rem', color: 'var(--theme-text-muted)', fontWeight: 600, lineHeight: 1, whiteSpace: 'nowrap' }}>
                                    {lang === 'zh-Hans' ? '服务商' : lang === 'zh-Hant' ? '服務商' : 'Provider'}
                                </div>
                                <div
                                    style={{ color: 'var(--theme-text-primary)', fontWeight: 700, fontSize: '0.46rem', lineHeight: 1.02, textAlign: 'right', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}
                                >
                                    {sidebarCurrentProviderTokenUsage.provider || (lang === 'zh-Hans' ? '未选择' : lang === 'zh-Hant' ? '未選擇' : 'Not selected')}
                                </div>
                            </div>
                            <div style={{ display: 'grid', gridTemplateColumns: '1fr', gap: '2px' }}>
                                {[
                                    { label: lang === 'zh-Hans' ? '入' : lang === 'zh-Hant' ? '入' : 'In', value: sidebarCurrentProviderTokenUsage.input, tone: 'muted' },
                                    { label: lang === 'zh-Hans' ? '出' : lang === 'zh-Hant' ? '出' : 'Out', value: sidebarCurrentProviderTokenUsage.output, tone: 'muted' },
                                    { label: lang === 'zh-Hans' ? '总' : lang === 'zh-Hant' ? '總' : 'All', value: sidebarCurrentProviderTokenUsage.total, tone: 'primary', bold: true },
                                ].map(({ label, value, tone, bold }) => (
                                    <div key={label} style={{ minWidth: 0, borderRadius: '4px', background: aiThemeMode === 'dark' ? 'rgba(17, 24, 39, 0.96)' : 'rgba(247, 248, 250, 0.74)', border: aiThemeMode === 'dark' ? '1px solid rgba(71, 85, 105, 0.72)' : '1px solid rgba(225, 228, 232, 0.72)', padding: '3px 3px', display: 'grid', gridTemplateColumns: 'auto minmax(0, 1fr)', alignItems: 'center', gap: '4px' }}>
                                        <div style={{ fontSize: '0.42rem', color: 'var(--theme-text-muted)', fontWeight: 600, lineHeight: 1, whiteSpace: 'nowrap' }}>{label}</div>
                                        <div style={{ color: tone === 'primary' ? 'var(--theme-text-primary)' : 'var(--theme-text-secondary)', fontWeight: bold ? 700 : 600, fontSize: bold ? '0.5rem' : '0.46rem', lineHeight: 1.02, textAlign: 'right', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', fontVariantNumeric: 'tabular-nums' }} title={value.toLocaleString()}>
                                            {value.toLocaleString()}
                                        </div>
                                    </div>
                                ))}
                            </div>
                        </div>
                    </div>
                </div>
            </div>

            <div className="main-container" data-ai-theme={aiThemeMode}>
                {/* AI assistant as main content (both lite and pro modes) */}
                {navTab === 'ai' ? (
                    <div style={{ position: 'relative', display: 'flex', flexDirection: 'column', width: '100%', height: '100%', minHeight: 0 }}>
                        <AIAssistantPanel
                        onClose={() => { switchTool('settings'); }}
                        lang={lang}
                        onThemeModeChange={setAIThemeMode}
                        state={{
                            messages: aiAssistant.messages,
                            progressMessages: aiAssistant.progressMessages,
                            sending: aiAssistant.sending,
                            streaming: aiAssistant.streaming,
                            visualBusy: aiAssistant.visualBusy,
                            ready: aiAssistant.ready,
                            initStatus: aiAssistant.initStatus,
                            selectedFilePath: aiAssistant.selectedFilePath,
                            submittedPrompts: aiAssistant.submittedPrompts,
                            draftInputValue: aiAssistant.draftInputValue,
                            trialReflectEnabled: aiAssistant.trialReflectEnabled,
                            scrollToTopSeq: aiAssistant.scrollToTopSeq,
                            onboardingIncomplete: !config?.onboarding_done && !showMaclawLLMPopup,
                            showTraceEntry: !!config?.show_ai_trace_entry,
                        }}
                        actions={{
                            browseFile: aiAssistant.browseFile,
                            clearSelectedFile: aiAssistant.clearSelectedFile,
                            sendMessage: aiAssistant.sendMessage,
                            clearHistory: aiAssistant.clearHistory,
                            recordSubmittedPrompt: aiAssistant.recordSubmittedPrompt,
                            setDraftInputValue: aiAssistant.setDraftInputValue,
                            executeAction: aiAssistant.executeAction,
                            refreshNews: aiAssistant.refreshNews,
                            onOpenOnboarding: () => setShowMaclawLLMPopup(true),
                            cancelSession: aiAssistant.cancelSession,
                            onOpenTutorial: () => switchTool('tutorial'),
                        }}
                        window={{
                            inline: true,
                            maximized: aiPanelMaximized,
                            onToggleMaximize: handleAIPanelMaximizeToggle,
                            onHideWindow: () => WindowHide(),
                        }}
                    />
                    </div>
                ) : (
                <><div className="top-header" style={{ '--wails-draggable': 'no-drag' } as any}>
                    <div className="top-header__bar" style={{ width: '100%' }}>
                        <h2 className="top-header__title" style={{ '--wails-draggable': 'drag', flex: 1 } as any}>
                            <span>
                                {navTab === 'claude' ? 'Claude Code' :
                                        navTab === 'gemini' ? 'Gemini CLI' :
                                            navTab === 'codex' ? 'OpenAI Codex' :
                                                navTab === 'opencode' ? 'OpenCode AI' :
                                                    navTab === 'codebuddy' ? 'CodeBuddy AI' :
                                                        navTab === 'cursor' ? 'Cursor Agent' :
                                                                            navTab === 'iflow' ? 'iFlow CLI' :
                                                                navTab === 'kilo' ? 'Kilo Code CLI' :
                                                                        navTab === 'projects' ? t("projectManagement") :
                                                                    navTab === 'skills' ? t("skills") :
                                                                        navTab === 'tutorial' ? t("tutorial") :
                                                                            navTab === 'gossip' ? t("gossip") :
                                                                            navTab === 'remote' ? (lang === 'zh-Hans' ? '任务管理' : lang === 'zh-Hant' ? '任務管理' : 'Task Management') :
                                                                                navTab === 'api-store' ? t("apiStore") :
                                                                                    navTab === 'mcp' ? 'MCP' :
                                                                                        navTab === 'settings' ? t("globalSettings") :
                                                                                            navTab === 'agentnet' ? (lang === 'zh-Hans' ? '智网' : lang === 'zh-Hant' ? '智網' : 'AgentNet') : t("about")}
                            </span>
                            {navTab === 'projects' && (
                                <>
                                    <button
                                        onClick={() => switchTool(activeTool)}
                                        className="btn-link"
                                        style={{
                                            marginLeft: '10px',
                                            fontSize: '0.8rem',
                                            padding: '4px 12px'
                                        }}
                                        title="Back"
                                    >&lt;&lt; {t("back") || "返回"}</button>
                                    <button
                                        className="btn-primary"
                                        style={{ marginLeft: '10px', padding: '4px 12px', fontSize: '0.8rem' }}
                                        onClick={handleAddNewProject}
                                    >{t("addNewProject")}</button>
                                </>
                            )}
                            {navTab === 'tutorial' && (
                                <button
                                    className="btn-link"
                                    style={{ marginLeft: '10px', fontSize: '0.8rem', padding: '4px 12px' }}
                                    onClick={async () => {
                                        try {
                                            setRefreshStatus(t("refreshing"));
                                            setTutorialContent('');
                                            const content = await ReadTutorial();
                                            setRefreshStatus(t("refreshSuccess"));
                                            setTutorialContent(content);
                                            setRefreshKey(prev => prev + 1);
                                            setTimeout(() => setRefreshStatus(''), 5000);
                                        } catch (err) {
                                            setRefreshStatus(t("refreshFailed") + err);
                                            setTimeout(() => setRefreshStatus(''), 5000);
                                        }
                                    }}
                                >
                                    {t("refreshMessage")}
                                </button>
                            )}
                            {isToolTab(navTab) && (
                                <>
                                    <button
                                        className="btn-link"
                                        onClick={() => setShowModelSettings(true)}
                                        style={{
                                            marginLeft: '10px',
                                            padding: '2px 8px',
                                            fontSize: '0.8rem',
                                            borderColor: 'var(--theme-primary)',
                                            color: 'var(--theme-primary)',
                                            '--wails-draggable': 'no-drag'
                                        } as any}
                                    >
                                        {lang === 'zh-Hans' || lang === 'zh-Hant' ? '服务商配置' : 'Provider Config'}
                                    </button>
                                    {isSkillTool(navTab) && (
                                        <button
                                            className="btn-link"
                                            onClick={() => {
                                                setSelectedSkillsToInstall([]);
                                                setShowInstallSkillModal(true);
                                            }}
                                            style={{
                                                marginLeft: '10px',
                                                padding: '2px 8px',
                                                fontSize: '0.8rem',
                                                borderColor: 'var(--theme-success)',
                                                color: 'var(--theme-success)',
                                                '--wails-draggable': 'no-drag'
                                            } as any}
                                        >
                                            {t("installSkills")}
                                        </button>
                                    )}
                                    <button
                                        className="btn-link"
                                        onClick={() => switchTool('api-store')}
                                        style={{
                                            marginLeft: '10px',
                                            padding: '2px 8px',
                                            fontSize: '0.8rem',
                                            borderColor: 'var(--theme-warning)',
                                            color: 'var(--theme-warning)',
                                            '--wails-draggable': 'no-drag'
                                        } as any}
                                    >
                                        {t("apiStore")}
                                    </button>
                                </>
                            )}
                        </h2>
                        <div className="window-controls" style={{ '--wails-draggable': 'no-drag', pointerEvents: 'auto', position: 'relative', zIndex: 10000 } as any}>
                            <button
                                onMouseDown={handleWindowHide}
                                className="btn-window-minimize"
                                title={t("hide")}
                                aria-label={t("hide")}
                                style={{ '--wails-draggable': 'no-drag', pointerEvents: 'auto', cursor: 'pointer', position: 'relative', zIndex: 10001 } as any}
                            >
                                <span aria-hidden="true" className="btn-window-minimize__icon" />
                            </button>
                        </div>
                    </div>
                </div>

                <div className="main-content elegant-scrollbar" style={{ overflowY: navTab === 'projects' ? 'hidden' : 'auto', paddingBottom: '20px', '--wails-draggable': 'no-drag' } as any}>
                    {navTab === 'tutorial' && (
                        <div style={{
                            width: '100%',
                            padding: '0 15px',
                            boxSizing: 'border-box'
                        }}>
                            <div style={{ marginBottom: '8px' }}>
                                <button
                                    className="btn-link"
                                    onClick={() => switchTool('ai')}
                                    style={{
                                        fontSize: '0.8rem',
                                        padding: '4px 12px',
                                        cursor: 'pointer',
                                        display: 'inline-flex',
                                        alignItems: 'center',
                                        gap: '4px',
                                    }}
                                >
                                    ← {lang === 'en' ? 'Back to AI Assistant' : lang === 'zh-Hant' ? '返回 AI 助手' : '返回 AI 助手'}
                                </button>
                            </div>
                            <div style={{
                                position: 'relative',
                                marginBottom: '5px'
                            }}>
                                {refreshStatus && (
                                    <div style={{
                                        position: 'absolute',
                                        top: '0',
                                        right: '0',
                                        zIndex: 100,
                                        padding: '4px 12px',
                                        backgroundColor: 'var(--theme-info-bg, rgba(224, 242, 254, 0.95))',
                                        borderRadius: '16px',
                                        color: 'var(--theme-primary)',
                                        fontSize: '0.75rem',
                                        fontWeight: 'bold',
                                        boxShadow: '0 4px 6px rgba(0,0,0,0.1)',
                                        backdropFilter: 'blur(4px)',
                                        animation: 'fadeIn 0.3s ease-out'
                                    }}>
                                        {refreshStatus}
                                    </div>
                                )}
                            </div>

                            <div className="markdown-content" style={{
                                backgroundColor: 'var(--theme-surface)',
                                padding: '20px',
                                borderRadius: '8px',
                                border: '1px solid var(--border-color)',
                                fontFamily: 'inherit',
                                fontSize: '0.75rem',
                                lineHeight: '1.6',
                                color: 'var(--theme-text-primary)',
                                marginBottom: '20px',
                                textAlign: 'left'
                            }}>
                                <ReactMarkdown
                                    key={refreshKey}
                                    remarkPlugins={[remarkGfm]}
                                    // @ts-ignore - rehype-raw type compatibility
                                    rehypePlugins={[rehypeRaw]}
                                    components={{ a: MarkdownLink }}
                                >
                                    {tutorialContent}
                                </ReactMarkdown>
                            </div>
                        </div>
                    )}
                    {navTab === 'gossip' && gossipAllowed && (
                        <GossipPanel lang={lang} />
                    )}
                    {navTab === 'remote' && (
                        <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
                            <div style={{ flex: 1, overflowY: 'auto', padding: '20px', overflowX: 'hidden' }}>
                                <RemoteSessionList
                                    remoteSessions={remoteSessions}
                                    remoteInputDrafts={remoteInputDrafts}
                                    setRemoteInputDrafts={setRemoteInputDrafts}
                                    interruptRemoteSession={interruptRemoteSession}
                                    killRemoteSession={killRemoteSession}
                                    refreshSessionsOnly={refreshSessionsOnly}
                                    showToastMessage={showToastMessage}
                                    translate={translate}
                                    formatText={formatText}
                                    localizeText={localizeText}
                                    lang={lang}
                                />

                            </div>
                        </div>
                    )}
                    {navTab === 'api-store' && (
                        <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>

                            <div style={{ flex: 1, overflowY: 'auto', padding: '20px', overflowX: 'hidden' }}>
                                <div style={{
                                    display: 'grid',
                                    gridTemplateColumns: 'repeat(4, 1fr)',
                                    gap: '12px',
                                    paddingBottom: '20px'
                                }}>
                                    {[
                                        { name: 'ChatFire', url: 'https://api.chatfire.cn/register?aff=jira', isRelay: true, hasSubscription: false },
                                        { name: '智谱龙虾', url: 'https://bigmodel.cn/glm-coding', isRelay: false, hasSubscription: true },
                                        { name: '智谱编程', url: 'https://bigmodel.cn/glm-coding', isRelay: false, hasSubscription: true },
                                        { name: '月之暗面', url: 'https://www.kimi.com/membership/pricing?from=upgrade_plan&track_id=1d2446f5-f45f-4ae5-961e-c0afe936a115', isRelay: false, hasSubscription: true },
                                        { name: '豆包', url: 'https://www.volcengine.com/activity/codingplan', isRelay: false, hasSubscription: true },
                                        { name: '腾讯云', url: 'https://cloud.tencent.com/act/pro/codingplan', isRelay: false, hasSubscription: true },
                                        { name: '讯飞星辰', url: 'https://www.xfyun.cn/doc/spark/CodingPlan.html', isRelay: false, hasSubscription: true },
                                        { name: 'MiniMax', url: 'https://platform.minimaxi.com/user-center/payment/coding-plan', isRelay: false, hasSubscription: true },
                                        { name: '百度千帆', url: 'https://cloud.baidu.com/product/codingplan.html', isRelay: false, hasSubscription: true },
                                        { name: 'DeepSeek', url: 'https://platform.deepseek.com/api_keys', isRelay: false, hasSubscription: false, isBilling: true },
                                        { name: '小米', url: 'https://platform.xiaomimimo.com/#/console/api-keys', isRelay: false, hasSubscription: false, isBilling: true },
                                        { name: '摩尔线程', url: 'https://code.mthreads.com/', isRelay: false, hasSubscription: true },
                                        { name: '快手', url: 'https://www.streamlake.com/marketing/coding-plan', isRelay: false, hasSubscription: true },
                                        { name: '阿里云', url: 'https://coding.dashscope.aliyuncs.com/', isRelay: false, hasSubscription: true },
                                    ].map((provider, index) => (
                                        <div
                                            key={index}
                                            style={{
                                                backgroundColor: 'var(--theme-surface)',
                                                border: '1px solid var(--theme-border)',
                                                borderRadius: '8px',
                                                padding: '8px 12px',
                                                display: 'flex',
                                                flexDirection: 'column',
                                                justifyContent: 'center',
                                                transition: 'all 0.2s ease',
                                                cursor: 'pointer',
                                                position: 'relative',
                                                minHeight: '42px'
                                            }}
                                            onMouseEnter={(e) => {
                                                e.currentTarget.style.boxShadow = '0 10px 24px rgba(15,23,42,0.18)';
                                                e.currentTarget.style.transform = 'translateY(-2px)';
                                            }}
                                            onMouseLeave={(e) => {
                                                e.currentTarget.style.boxShadow = 'none';
                                                e.currentTarget.style.transform = 'translateY(0)';
                                            }}
                                            onClick={() => BrowserOpenURL(provider.url)}
                                        >
                                            {provider.isRelay && (
                                                <div style={{
                                                    position: 'absolute',
                                                    top: '-6px',
                                                    right: '-6px',
                                                    backgroundColor: 'var(--theme-primary)',
                                                    color: 'var(--theme-text-primary)',
                                                    padding: '3px 10px',
                                                    borderRadius: '4px',
                                                    fontSize: '0.65rem',
                                                    fontWeight: 'bold',
                                                    boxShadow: '0 2px 4px rgba(15,23,42,0.18)'
                                                }}>
                                                    {t("relayService")}
                                                </div>
                                            )}
                                            {provider.hasSubscription && (
                                                <div style={{
                                                    position: 'absolute',
                                                    top: '-6px',
                                                    right: '-6px',
                                                    backgroundColor: 'var(--theme-danger)',
                                                    color: 'var(--theme-text-primary)',
                                                    padding: '3px 10px',
                                                    borderRadius: '4px',
                                                    fontSize: '0.65rem',
                                                    fontWeight: 'bold',
                                                    boxShadow: '0 2px 4px rgba(0,0,0,0.15)'
                                                }}>
                                                    {t("subscription")}
                                                </div>
                                            )}
                                            {(provider as any).isBilling && (
                                                <div style={{
                                                    position: 'absolute',
                                                    top: '-6px',
                                                    right: '-6px',
                                                    backgroundColor: 'var(--theme-warning)',
                                                    color: 'var(--theme-text-primary)',
                                                    padding: '3px 10px',
                                                    borderRadius: '4px',
                                                    fontSize: '0.65rem',
                                                    fontWeight: 'bold',
                                                    boxShadow: '0 2px 4px rgba(15,23,42,0.18)'
                                                }}>
                                                    {t("billing")}
                                                </div>
                                            )}
                                            <div style={{
                                                fontSize: '0.85rem',
                                                fontWeight: 600,
                                                color: 'var(--theme-primary)',
                                                marginBottom: '8px'
                                            }}>
                                                {provider.name}
                                            </div>
                                            <div style={{
                                                fontSize: '0.85rem',
                                                color: 'var(--theme-text-secondary)'
                                            }}>
                                                🛒
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        </div>
                    )}
                    {isToolTab(navTab) && (
                        <ToolConfiguration
                            toolName={navTab}
                            toolCfg={toolCfg}
                            showModelSettings={showModelSettings}
                            setShowModelSettings={setShowModelSettings}
                            handleModelSwitch={handleModelSwitch}
                            t={t}
                            lang={lang}
                        />
                    )}
                    {navTab === 'projects' && (
                        <div className="project-manager-panel">
                            <div className="project-manager-toolbar">
                                <input
                                    type="text"
                                    className="form-input"
                                    value={projectSearchKeyword}
                                    onChange={(e) => setProjectSearchKeyword(e.target.value)}
                                    placeholder={t("projectSearchPlaceholder")}
                                    spellCheck={false}
                                    autoComplete="off"
                                />
                                <select
                                    className="form-input"
                                    value={projectSortMode}
                                    onChange={(e) => setProjectSortMode(e.target.value as 'default' | 'name-asc' | 'name-desc' | 'path-asc' | 'path-desc')}
                                >
                                    <option value="default">{t("projectSortDefault")}</option>
                                    <option value="name-asc">{t("projectSortNameAsc")}</option>
                                    <option value="name-desc">{t("projectSortNameDesc")}</option>
                                    <option value="path-asc">{t("projectSortPathAsc")}</option>
                                    <option value="path-desc">{t("projectSortPathDesc")}</option>
                                </select>
                            </div>

                            <div className="project-manager-summary">
                                {filteredAndSortedProjects.length > 0 ? (
                                    <span>
                                        {t("projectShowing")} {projectPageStartIndex + 1}-{Math.min(projectPageStartIndex + PROJECT_PAGE_SIZE, filteredAndSortedProjects.length)} / {filteredAndSortedProjects.length} {t("projectTotal")}
                                    </span>
                                ) : (
                                    <span>{t("projectNoResults")}</span>
                                )}
                            </div>

                            <div className="project-manager-list elegant-scrollbar">
                                {pagedProjects.map((proj: any) => (
                                    <div key={proj.id} className="project-manager-item">
                                        <input
                                            type="text"
                                            className="form-input"
                                            data-field="project-item-name"
                                            data-id={proj.id}
                                            value={proj.name}
                                            onChange={(e) => {
                                                const newList = config.projects.map((p: any) => p.id === proj.id ? { ...p, name: e.target.value } : p);
                                                setConfig(new main.AppConfig({ ...config, projects: newList }));
                                            }}
                                            style={{ fontWeight: 'bold', border: 'none', padding: 0, fontSize: '0.9rem', width: '112px', flexShrink: 0, lineHeight: 1.1 }}
                                            spellCheck={false}
                                            autoComplete="off"
                                        />
                                        <div style={{ flex: 1, fontSize: '0.78rem', color: 'var(--theme-text-secondary)', backgroundColor: 'var(--theme-surface-muted)', padding: '3px 6px', borderRadius: '4px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', lineHeight: 1.1 }}>
                                            {proj.path}
                                        </div>

                                        <div style={{ display: 'flex', gap: '8px', alignItems: 'center', flexShrink: 0 }}>

                                            <button className="btn-link" onClick={() => {
                                                if (proj.path) {
                                                    OpenFileOrShowInFolder(proj.path).catch(() => {});
                                                }
                                            }}>{t("browse")}</button>

                                            <button className="btn-link" onClick={() => {
                                                SelectProjectDir().then(dir => {
                                                    if (dir) {
                                                        const newList = config.projects.map((p: any) => p.id === proj.id ? { ...p, path: dir } : p);
                                                        const newConfig = new main.AppConfig({ ...config, projects: newList });
                                                        setConfig(newConfig);
                                                        SaveConfig(newConfig);
                                                    }
                                                });
                                            }}>{t("change")}</button>

                                            <button
                                                style={{ color: 'var(--theme-danger)', background: 'none', border: 'none', cursor: 'pointer', fontSize: '0.85rem' }}
                                                onClick={() => {
                                                    if (config.projects.length > 1) {
                                                        const newList = config.projects.filter((p: any) => p.id !== proj.id);
                                                        const newConfig = new main.AppConfig({ ...config, projects: newList });
                                                        if (config.current_project === proj.id) newConfig.current_project = newList[0].id;
                                                        if (selectedProjectForLaunch === proj.id) setSelectedProjectForLaunch(newConfig.current_project);
                                                        setConfig(newConfig);
                                                        SaveConfig(newConfig);
                                                    }
                                                }}
                                            >
                                                {t("delete")}
                                            </button>
                                        </div>
                                    </div>
                                ))}
                            </div>

                            {filteredAndSortedProjects.length > 0 && (
                                <div className="project-manager-pagination">
                                    <button
                                        className="btn-link"
                                        onClick={() => setProjectCurrentPage(Math.max(1, safeProjectCurrentPage - 1))}
                                        disabled={safeProjectCurrentPage <= 1}
                                    >
                                        {t("prevPage")}
                                    </button>
                                    <span>{safeProjectCurrentPage} / {totalProjectPages}</span>
                                    <button
                                        className="btn-link"
                                        onClick={() => setProjectCurrentPage(Math.min(totalProjectPages, safeProjectCurrentPage + 1))}
                                        disabled={safeProjectCurrentPage >= totalProjectPages}
                                    >
                                        {t("nextPage")}
                                    </button>
                                </div>
                            )}
                        </div>
                    )}

                    {navTab === 'skills' && (
                        <div style={{ padding: '10px' }}>
                            <SkillsManagementPanel localizeText={localizeText} />
                        </div>
                    )}

                    {navTab === 'mcp' && (
                        <div style={{ padding: '10px' }}>
                            <MCPManagementPanel translate={translate} />
                        </div>
                    )}

                    {navTab === 'settings' && (
                        <div className="settings-shell" style={{ padding: '10px' }}>
                            <div className="settings-top-tabs">
                                {settingsTabOptions.filter(tab => !(isLiteMode && (tab.id === 'display' || tab.id === 'remote'))).map((tab) => (
                                    <button
                                        key={tab.id}
                                        type="button"
                                        className={`settings-top-tab ${settingsTab === tab.id ? 'active' : ''}`}
                                        onClick={() => setSettingsTab(tab.id)}
                                        title={tab.desc}
                                    >
                                        {tab.label}
                                    </button>
                                ))}
                            </div>
                            <div className="settings-panel" style={{ display: settingsTab === 'general' ? 'block' : 'none' }}>
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '20px', marginBottom: '15px' }}>
                                <div className="form-group" style={{ flex: '1', marginBottom: 0, display: 'flex', alignItems: 'center', gap: '10px' }}>
                                    <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap', fontSize: '0.8rem' }}>{t("language")}</label>
                                    <select value={lang} onChange={handleLangChange} className="form-input" style={{ width: 'auto', fontSize: '0.8rem', padding: '2px 8px', height: '28px' }}>
                                        <option value="en">English</option>
                                        <option value="zh-Hans">简体中文</option>
                                        <option value="zh-Hant">繁體中文</option>
                                    </select>
                                </div>
                                {!isLiteMode && <div style={{ display: 'flex', alignItems: 'center', gap: '6px', flexShrink: 0 }}>
                                    <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap', fontSize: '0.8rem' }}>{t("defaultLaunchModeLabel")}</label>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '3px', cursor: 'pointer', fontSize: '0.78rem' }}>
                                        <input type="radio" name="launchMode" checked={!config?.default_launch_mode || config.default_launch_mode === 'local'} onChange={() => { if (config) { const c = new main.AppConfig({ ...config, default_launch_mode: 'local' }); setConfig(c); SaveConfig(c); } }} />
                                        {t("localModeLabel")}
                                    </label>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '3px', cursor: 'pointer', fontSize: '0.78rem' }}>
                                        <input type="radio" name="launchMode" checked={config?.default_launch_mode === 'remote'} onChange={() => { if (config) { const c = new main.AppConfig({ ...config, default_launch_mode: 'remote' }); setConfig(c); SaveConfig(c); } }} />
                                        {t("remoteModeLabel")}
                                    </label>
                                </div>}
                                
                            </div>

                            {/* UI Mode selector in general settings */}
                            <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '15px', marginTop: '-5px', padding: '0 0 0 0' }}>
                                <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap', fontSize: '0.8rem' }}>{t("uiModeLabel")}</label>
                                <label style={{ display: 'flex', alignItems: 'center', gap: '3px', cursor: 'pointer', fontSize: '0.78rem' }}>
                                    <input type="radio" name="uiMode" checked={!isLiteMode} onChange={() => { if (config) { const c = new main.AppConfig({ ...config, ui_mode: 'pro' }); setConfig(c); SaveConfig(c); } }} />
                                    {t("uiModePro")}
                                </label>
                                <label style={{ display: 'flex', alignItems: 'center', gap: '3px', cursor: 'pointer', fontSize: '0.78rem' }}>
                                    <input type="radio" name="uiMode" checked={isLiteMode} onChange={() => { if (config) { const c = new main.AppConfig({ ...config, ui_mode: 'lite' }); setConfig(c); SaveConfig(c); const currentTab: string = navTab; if (currentTab === 'remote' || currentTab === 'skills' || currentTab === 'mcp' || isToolTab(currentTab)) { setNavTab('ai'); } if (settingsTab === 'display' || settingsTab === 'remote' || settingsTab === 'ui') { setSettingsTab('general'); } } }} />
                                    {t("uiModeLite")}
                                </label>
                                <span style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)' }}>
                                    {isLiteMode ? t("uiModeLiteDesc") : t("uiModeProDesc")}
                                </span>
                            </div>

                            <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '6px' }}>
                                <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '0.8rem' }}>
                                    <input
                                        type="checkbox"
                                        checked={config?.log_detail_enabled || false}
                                        onChange={(e) => {
                                            if (!config) return;
                                            const c = new main.AppConfig({ ...config, log_detail_enabled: e.target.checked });
                                            setConfig(c);
                                            SaveConfig(c);
                                        }}
                                    />
                                    <span>{lang === 'zh-Hans' ? '日志详情' : lang === 'zh-Hant' ? '日誌詳情' : 'Detailed logs'}</span>
                                </label>
                                <span style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)' }}>
                                    {lang === 'zh-Hans' ? '开启后显示更完整的运行日志；关闭时仅保留错误日志' : lang === 'zh-Hant' ? '開啟後顯示更完整的運行日誌；關閉時僅保留錯誤日誌' : 'Show more complete runtime logs; when off, only error logs are kept'}
                                </span>
                            </div>

                            <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '6px' }}>
                                <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer', fontSize: '0.8rem' }}>
                                    <input
                                        type="checkbox"
                                        checked={config?.llm_trajectory_logging || false}
                                        onChange={(e) => {
                                            if (!config) return;
                                            const c = new main.AppConfig({ ...config, llm_trajectory_logging: e.target.checked });
                                            setConfig(c);
                                            SaveConfig(c);
                                        }}
                                    />
                                    <span>{lang === 'zh-Hans' ? '记录 LLM 轨迹' : lang === 'zh-Hant' ? '記錄 LLM 軌跡' : 'Record LLM trajectory'}</span>
                                </label>
                                <span style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)' }}>
                                    {lang === 'zh-Hans' ? '保存 LLM 交互轨迹，用于分析与训练' : lang === 'zh-Hant' ? '保存 LLM 互動軌跡，用於分析與訓練' : 'Save LLM interaction trajectories for analysis and training'}
                                </span>
                            </div>
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'remote' ? 'block' : 'none' }}>
                                <RemoteSettingsPanel
                                    config={config}
                                    saveRemoteConfigField={saveRemoteConfigField}
                                    translate={translate}
                                    remoteBusy={remoteBusy}
                                    remoteActivationStatus={remoteActivationStatus}
                                    activateRemoteWithEmail={activateRemoteWithEmail}
                                    clearRemoteActivationState={clearRemoteActivationState}
                                    invitationCodeRequired={invitationCodeRequired}
                                    invitationCode={invitationCode}
                                    setInvitationCode={setInvitationCode}
                                    invitationCodeError={invitationCodeError}
                                />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'proxy' ? 'block' : 'none' }}>
                                {/* Enable toggle */}
                                <div style={{ marginBottom: '15px', display: 'flex', alignItems: 'center', gap: '10px' }}>
                                    <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', gap: '8px' }}>
                                        <input type="checkbox" checked={config?.default_proxy_enabled || false}
                                            onChange={(e) => { const nc = new main.AppConfig({ ...config, default_proxy_enabled: e.target.checked }); setConfig(nc); }}
                                        />
                                        <span style={{ fontWeight: 500 }}>{t("proxyEnabled")}</span>
                                    </label>
                                </div>

                                {/* Protocol + Host + Port row */}
                                <div style={{ display: 'flex', gap: '10px', marginBottom: '12px' }}>
                                    <div style={{ width: '110px', flexShrink: 0 }}>
                                        <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyProtocol")}</label>
                                        <select className="form-input" style={{ height: '34px' }}
                                            value={config?.default_proxy_protocol || 'http'}
                                            onChange={(e) => { const nc = new main.AppConfig({ ...config, default_proxy_protocol: e.target.value }); setConfig(nc); }}
                                        >
                                            <option value="http">HTTP</option>
                                            <option value="https">HTTPS</option>
                                            <option value="socks5">SOCKS5</option>
                                        </select>
                                    </div>
                                    <div style={{ flex: 1 }}>
                                        <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyHost")}</label>
                                        <input type="text" className="form-input" spellCheck={false}
                                            placeholder={t("proxyHostPlaceholder")}
                                            value={config?.default_proxy_host || ''}
                                            onChange={(e) => { setConfig(new main.AppConfig({ ...config, default_proxy_host: e.target.value })); }}
                                        />
                                    </div>
                                    <div style={{ width: '90px', flexShrink: 0 }}>
                                        <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyPort")}</label>
                                        <input type="text" className="form-input" spellCheck={false}
                                            placeholder={t("proxyPortPlaceholder")}
                                            value={config?.default_proxy_port || ''}
                                            onChange={(e) => { setConfig(new main.AppConfig({ ...config, default_proxy_port: e.target.value })); }}
                                        />
                                    </div>
                                </div>

                                {/* Username + Password row */}
                                <div style={{ display: 'flex', gap: '10px', marginBottom: '12px' }}>
                                    <div style={{ flex: 1 }}>
                                        <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyUsername")}</label>
                                        <input type="text" className="form-input" spellCheck={false} autoComplete="off"
                                            value={config?.default_proxy_username || ''}
                                            onChange={(e) => { setConfig(new main.AppConfig({ ...config, default_proxy_username: e.target.value })); }}
                                        />
                                    </div>
                                    <div style={{ flex: 1 }}>
                                        <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyPassword")}</label>
                                        <input type="password" className="form-input" autoComplete="new-password"
                                            value={config?.default_proxy_password || ''}
                                            onChange={(e) => { setConfig(new main.AppConfig({ ...config, default_proxy_password: e.target.value })); }}
                                        />
                                    </div>
                                </div>

                                {/* Bypass list */}
                                <div style={{ marginBottom: '12px' }}>
                                    <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyBypass")}</label>
                                    <textarea className="form-input" rows={2} spellCheck={false}
                                        placeholder={t("proxyBypassPlaceholder")}
                                        value={config?.default_proxy_bypass || ''}
                                        onChange={(e) => { setConfig(new main.AppConfig({ ...config, default_proxy_bypass: e.target.value })); }}
                                        style={{ resize: 'vertical', minHeight: '40px', fontFamily: 'monospace', fontSize: '0.78rem' }}
                                    />
                                    <div style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)', marginTop: '3px' }}>{t("proxyBypassHint")}</div>
                                </div>

                                {/* Scope checkboxes */}
                                <div style={{ marginBottom: '12px', padding: '10px', backgroundColor: 'var(--theme-info-bg)', borderRadius: '6px', border: '1px solid var(--theme-primary-soft)' }}>
                                    <label className="form-label" style={{ fontSize: '0.78rem', marginBottom: '8px', display: 'block' }}>{t("proxyScopeTitle")}</label>
                                    <div style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
                                        <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', gap: '8px', fontSize: '0.82rem' }}>
                                            <input type="checkbox" checked={config?.default_proxy_scope_maclaw || false}
                                                onChange={(e) => { setConfig(new main.AppConfig({ ...config, default_proxy_scope_maclaw: e.target.checked })); }}
                                            />
                                            {t("proxyScopeMaclaw")}
                                        </label>
                                        <label style={{ display: 'flex', alignItems: 'center', cursor: isWindows ? 'not-allowed' : 'pointer', gap: '8px', fontSize: '0.82rem', opacity: isWindows ? 0.45 : 0.75 }}>
                                            <input type="checkbox" checked={isWindows ? false : (config?.default_proxy_scope_coding_tools || false)}
                                                disabled={isWindows}
                                                onChange={(e) => { if (!isWindows) { setConfig(new main.AppConfig({ ...config, default_proxy_scope_coding_tools: e.target.checked })); } }}
                                            />
                                            {t("proxyScopeCodingTools")}
                                        </label>
                                        <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', gap: '8px', fontSize: '0.82rem' }}>
                                            <input type="checkbox" checked={config?.default_proxy_scope_agent || false}
                                                onChange={(e) => { setConfig(new main.AppConfig({ ...config, default_proxy_scope_agent: e.target.checked })); }}
                                            />
                                            {t("proxyScopeAgent")}
                                        </label>
                                    </div>
                                </div>

                                {/* Save button */}
                                <div style={{ display: 'flex', justifyContent: 'flex-end', marginTop: '20px' }}>
                                    <button className="btn-primary" onClick={() => {
                                        SaveConfig(config);
                                        try { (window as any).go?.main?.App?.SaveProxyConfig?.({
                                            enabled: config?.default_proxy_enabled || false,
                                            protocol: config?.default_proxy_protocol || 'http',
                                            host: config?.default_proxy_host || '',
                                            port: config?.default_proxy_port || '',
                                            username: config?.default_proxy_username || '',
                                            password: config?.default_proxy_password || '',
                                            bypass: config?.default_proxy_bypass || '',
                                            scope_maclaw: config?.default_proxy_scope_maclaw || false,
                                            scope_coding_tools: config?.default_proxy_scope_coding_tools || false,
                                            scope_agent: config?.default_proxy_scope_agent || false,
                                        }); } catch {}
                                    }} style={{ padding: '8px 16px' }}>
                                        {t("saveChanges")}
                                    </button>
                                </div>
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'llm' ? 'block' : 'none' }}>
                                <LLMConfigPanel lang={lang} codexModels={config?.codex?.models} onStatusChange={(online, configured) => { setMaclawLLMOnline(online); setMaclawLLMConfigured(configured); }} />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'serviceRedeem' ? 'block' : 'none' }}>
                                <HubServiceRedeemPanel
                                    lang={lang}
                                    onStatusChange={(serviceStatus) => {
                                        const active = !!serviceStatus?.active;
                                        setMaclawLLMConfigured(active);
                                        setMaclawLLMOnline(active);
                                    }}
                                />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'search' ? 'block' : 'none' }}>
                                <WebSearchConfigPanel lang={lang} />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'role' ? 'block' : 'none' }}>
                                <MaclawRolePanel config={config} saveRemoteConfigField={saveRemoteConfigField} lang={lang} />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'memory' ? 'block' : 'none' }}>
                                <MemoryManagementPanel lang={lang} />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'embedding' ? 'block' : 'none' }}>
                                <EmbeddingConfigPanel lang={lang} />
                                <ASRConfigPanel lang={lang} />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'agentnet' ? 'block' : 'none' }}>
                                <AgentNetPanel
                                    config={config}
                                    saveRemoteConfigField={saveRemoteConfigField}
                                    lang={lang}
                                    onRunningChange={setAgentNetRunning}
                                />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'im' ? 'block' : 'none' }}>
                                {/* IM sub-tabs */}
                                <div style={{ display: 'flex', gap: '6px', marginBottom: '16px', flexWrap: 'wrap' }}>
                                    {([
                                        { key: 'qq' as const, label: lang === 'zh-Hans' ? 'QQ 机器人' : lang === 'zh-Hant' ? 'QQ 機器人' : 'QQ Bot' },
                                        { key: 'telegram' as const, label: 'Telegram Bot' },
                                        { key: 'weixin' as const, label: lang === 'zh-Hans' ? '微信' : lang === 'zh-Hant' ? '微信' : 'WeChat' },
                                        ...(brandInfo?.id === 'qianxin' ? [{ key: 'lansenger' as const, label: lang === 'zh-Hans' ? '蓝信' : lang === 'zh-Hant' ? '藍信' : 'Lansenger' }] : []),
                                    ]).map((t) => (
                                        <button
                                            key={t.key}
                                            type="button"
                                            onClick={() => setImSubTab(t.key)}
                                            style={{
                                                padding: '4px 14px',
                                                borderRadius: '14px',
                                                border: imSubTab === t.key ? '1.5px solid var(--theme-primary)' : '1px solid var(--theme-border)',
                                                background: imSubTab === t.key ? 'var(--theme-info-bg)' : 'transparent',
                                                color: imSubTab === t.key ? 'var(--theme-primary)' : 'var(--theme-text-secondary)',
                                                fontWeight: imSubTab === t.key ? 600 : 400,
                                                fontSize: '0.75rem',
                                                cursor: 'pointer',
                                                transition: 'all 0.15s',
                                            }}
                                        >
                                            {t.label}
                                        </button>
                                    ))}
                                </div>

                                {/* QQ Bot tab */}
                                {imSubTab === 'qq' && (
                                <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
                                    <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginBottom: '12px', marginTop: 0 }}>
                                        {lang === 'zh-Hans'
                                            ? '配置你自己的 QQ 机器人，通过 QQ 与 MaClaw Agent 对话。'
                                            : lang === 'zh-Hant'
                                            ? '配置你自己的 QQ 機器人，透過 QQ 與 MaClaw Agent 對話。'
                                            : 'Configure your own QQ Bot to chat with MaClaw Agent via QQ.'}
                                    </p>

                                    <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '12px', flexWrap: 'wrap' }}>
                                        <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer', fontSize: '0.78rem' }}>
                                            <input
                                                type="checkbox"
                                                checked={config?.qqbot_enabled || false}
                                                onChange={async (e) => {
                                                    const enabled = e.target.checked;
                                                    await saveRemoteConfigField({ qqbot_enabled: enabled } as any);
                                                    if (enabled) {
                                                        try {
                                                            const s = await RestartQQBot();
                                                            setQQBotStatus(typeof s === 'string' ? s : 'disconnected');
                                                        } catch (err: any) {
                                                            console.error('[qqbot] restart failed:', err);
                                                        }
                                                    } else {
                                                        try { await RestartQQBot(); } catch {}
                                                        setQQBotStatus('disconnected');
                                                    }
                                                }}
                                            />
                                            {lang === 'zh-Hans' ? '启用 QQ 机器人' : lang === 'zh-Hant' ? '啟用 QQ 機器人' : 'Enable QQ Bot'}
                                        </label>
                                        <button
                                            type="button"
                                            style={{
                                                fontSize: '0.68rem',
                                                padding: '1px 8px',
                                                borderRadius: '4px',
                                                border: '1px solid var(--theme-primary)',
                                                background: 'transparent',
                                                color: 'var(--theme-primary)',
                                                cursor: 'pointer',
                                                whiteSpace: 'nowrap',
                                            }}
                                            onClick={() => BrowserOpenURL('https://q.qq.com/qqbot/openclaw/login.html')}
                                        >
                                            {lang === 'zh-Hans' ? '获取 AppID' : lang === 'zh-Hant' ? '取得 AppID' : 'Get AppID'}
                                        </button>
                                        {config?.qqbot_enabled && (
                                            <>
                                                <span style={{
                                                    fontSize: '0.7rem',
                                                    padding: '2px 8px',
                                                    borderRadius: '10px',
                                                    background: qqBotStatus === 'connected' ? 'var(--theme-success-bg)' : qqBotStatus === 'connecting' || qqBotStatus === 'reconnecting' ? 'var(--theme-warning-bg)' : 'var(--theme-danger-bg)',
                                                    color: qqBotStatus === 'connected' ? 'var(--theme-success)' : qqBotStatus === 'connecting' || qqBotStatus === 'reconnecting' ? 'var(--theme-warning)' : 'var(--theme-danger)',
                                                }}>
                                                    {qqBotStatus === 'connected' ? '● 已连接' : qqBotStatus === 'connecting' ? '◌ 连接中...' : qqBotStatus === 'reconnecting' ? '◌ 重连中...' : qqBotStatus === 'error' ? '✕ 错误' : '○ 未连接'}
                                                </span>
                                                <button
                                                    type="button"
                                                    style={{
                                                        fontSize: '0.68rem',
                                                        padding: '2px 8px',
                                                        borderRadius: '4px',
                                                        border: '1px solid var(--theme-border)',
                                                        background: 'transparent',
                                                        color: 'var(--theme-text-secondary)',
                                                        cursor: 'pointer',
                                                    }}
                                                    onClick={() => RestartQQBot().then(setQQBotStatus)}
                                                >
                                                    {lang === 'zh-Hans' ? '重启' : 'Restart'}
                                                </button>
                                            </>
                                        )}
                                    </div>

                                    {/* 单机/多机 mode selector */}
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '16px' }}>
                                        <span style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)' }}>
                                            {lang === 'zh-Hans' || lang === 'zh-Hant' ? '通道：' : 'Mode:'}
                                        </span>
                                        {[
                                            { value: true, label: lang === 'zh-Hans' || lang === 'zh-Hant' ? '🖥 单机' : '🖥 Local', desc: lang === 'zh-Hans' || lang === 'zh-Hant' ? '本地 LLM 直连' : 'Direct local LLM' },
                                            { value: false, label: lang === 'zh-Hans' || lang === 'zh-Hant' ? '🌐 多机' : '🌐 Remote', desc: lang === 'zh-Hans' || lang === 'zh-Hant' ? '通过 Hub 转发' : 'Via Hub' },
                                        ].map((opt) => (
                                            <button
                                                key={String(opt.value)}
                                                type="button"
                                                aria-label={opt.desc}
                                                title={opt.desc}
                                                style={{
                                                    padding: '4px 14px',
                                                    borderRadius: '14px',
                                                    border: qqBotLocalMode === opt.value ? '1.5px solid var(--theme-primary)' : '1px solid var(--theme-border)',
                                                    background: qqBotLocalMode === opt.value ? 'var(--theme-info-bg)' : 'transparent',
                                                    color: qqBotLocalMode === opt.value ? 'var(--theme-primary)' : 'var(--theme-text-secondary)',
                                                    fontWeight: qqBotLocalMode === opt.value ? 600 : 400,
                                                    fontSize: '0.75rem',
                                                    cursor: 'pointer',
                                                    transition: 'all 0.15s',
                                                }}
                                                onClick={() => {
                                                    const prev = qqBotLocalMode;
                                                    setQQBotLocalModeState(opt.value);
                                                    SetQQBotLocalMode(opt.value).then(() => {
                                                        LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                                                    }).catch((err: any) => {
                                                        setQQBotLocalModeState(prev);
                                                        showToastMessage(err?.message || err || '切换失败');
                                                    });
                                                }}
                                            >
                                                {opt.label}
                                            </button>
                                        ))}
                                    </div>

                                    <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '10px', maxWidth: '520px' }}>
                                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                            <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '62px' }}>App ID</label>
                                            <input
                                                type="text"
                                                value={config?.qqbot_app_id || ''}
                                                onChange={(e) => saveRemoteConfigField({ qqbot_app_id: e.target.value } as any)}
                                                placeholder="e.g. 102012345"
                                                style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)' }}
                                            />
                                        </div>
                                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                            <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '62px' }}>App Secret</label>
                                            <input
                                                type="password"
                                                value={config?.qqbot_app_secret || ''}
                                                onChange={(e) => saveRemoteConfigField({ qqbot_app_secret: e.target.value } as any)}
                                                placeholder="••••••••"
                                                style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)' }}
                                            />
                                        </div>
                                    </div>
                                </div>
                                )}

                                {/* Telegram Bot tab */}
                                {imSubTab === 'telegram' && (
                                <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
                                    <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginBottom: '12px', marginTop: 0 }}>
                                        {lang === 'zh-Hans'
                                            ? '配置你自己的 Telegram Bot，通过 Telegram 与 MaClaw Agent 对话。'
                                            : lang === 'zh-Hant'
                                            ? '配置你自己的 Telegram Bot，透過 Telegram 與 MaClaw Agent 對話。'
                                            : 'Configure your own Telegram Bot to chat with MaClaw Agent via Telegram.'}
                                    </p>

                                    <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '12px', flexWrap: 'wrap' }}>
                                        <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer', fontSize: '0.78rem' }}>
                                            <input
                                                type="checkbox"
                                                checked={(config as any)?.telegram_bot_enabled || false}
                                                onChange={async (e) => {
                                                    const enabled = e.target.checked;
                                                    await saveRemoteConfigField({ telegram_bot_enabled: enabled } as any);
                                                    if (enabled) {
                                                        try {
                                                            const s = await RestartTelegram();
                                                            setTelegramStatus(typeof s === 'string' ? s : 'disconnected');
                                                        } catch (err: any) {
                                                            console.error('[telegram] restart failed:', err);
                                                        }
                                                    } else {
                                                        try { await RestartTelegram(); } catch {}
                                                        setTelegramStatus('disconnected');
                                                    }
                                                }}
                                            />
                                            {lang === 'zh-Hans' ? '启用 Telegram Bot' : lang === 'zh-Hant' ? '啟用 Telegram Bot' : 'Enable Telegram Bot'}
                                        </label>
                                        <button
                                            type="button"
                                            style={{
                                                fontSize: '0.68rem',
                                                padding: '1px 8px',
                                                borderRadius: '4px',
                                                border: '1px solid var(--theme-primary)',
                                                background: 'transparent',
                                                color: 'var(--theme-primary)',
                                                cursor: 'pointer',
                                                whiteSpace: 'nowrap',
                                            }}
                                            onClick={() => BrowserOpenURL('https://open-claw.bot/docs/channels/telegram/')}
                                        >
                                            {lang === 'zh-Hans' ? '教程' : lang === 'zh-Hant' ? '教程' : 'Tutorial'}
                                        </button>
                                        {(config as any)?.telegram_bot_enabled && (
                                            <>
                                                <span style={{
                                                    fontSize: '0.7rem',
                                                    padding: '2px 8px',
                                                    borderRadius: '10px',
                                                    background: telegramStatus === 'connected' ? 'var(--theme-success-bg)' : telegramStatus === 'connecting' || telegramStatus === 'reconnecting' ? 'var(--theme-warning-bg)' : 'var(--theme-danger-bg)',
                                                    color: telegramStatus === 'connected' ? 'var(--theme-success)' : telegramStatus === 'connecting' || telegramStatus === 'reconnecting' ? 'var(--theme-warning)' : 'var(--theme-danger)',
                                                }}>
                                                    {telegramStatus === 'connected' ? '● 已连接' : telegramStatus === 'connecting' ? '◌ 连接中...' : telegramStatus === 'reconnecting' ? '◌ 重连中...' : telegramStatus === 'error' ? '✕ 错误' : '○ 未连接'}
                                                </span>
                                                <button
                                                    type="button"
                                                    style={{
                                                        fontSize: '0.68rem',
                                                        padding: '2px 8px',
                                                        borderRadius: '4px',
                                                        border: '1px solid var(--theme-border)',
                                                        background: 'transparent',
                                                        color: 'var(--theme-text-secondary)',
                                                        cursor: 'pointer',
                                                    }}
                                                    onClick={() => RestartTelegram().then(setTelegramStatus)}
                                                >
                                                    {lang === 'zh-Hans' ? '重启' : 'Restart'}
                                                </button>
                                            </>
                                        )}
                                    </div>

                                    {/* 单机/多机 mode selector */}
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '16px' }}>
                                        <span style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)' }}>
                                            {lang === 'zh-Hans' || lang === 'zh-Hant' ? '通道：' : 'Mode:'}
                                        </span>
                                        {[
                                            { value: true, label: lang === 'zh-Hans' || lang === 'zh-Hant' ? '🖥 单机' : '🖥 Local', desc: lang === 'zh-Hans' || lang === 'zh-Hant' ? '本地 LLM 直连' : 'Direct local LLM' },
                                            { value: false, label: lang === 'zh-Hans' || lang === 'zh-Hant' ? '🌐 多机' : '🌐 Remote', desc: lang === 'zh-Hans' || lang === 'zh-Hant' ? '通过 Hub 转发' : 'Via Hub' },
                                        ].map((opt) => (
                                            <button
                                                key={String(opt.value)}
                                                type="button"
                                                aria-label={opt.desc}
                                                title={opt.desc}
                                                style={{
                                                    padding: '4px 14px',
                                                    borderRadius: '14px',
                                                    border: telegramLocalMode === opt.value ? '1.5px solid var(--theme-primary)' : '1px solid var(--theme-border)',
                                                    background: telegramLocalMode === opt.value ? 'var(--theme-info-bg)' : 'transparent',
                                                    color: telegramLocalMode === opt.value ? 'var(--theme-primary)' : 'var(--theme-text-secondary)',
                                                    fontWeight: telegramLocalMode === opt.value ? 600 : 400,
                                                    fontSize: '0.75rem',
                                                    cursor: 'pointer',
                                                    transition: 'all 0.15s',
                                                }}
                                                onClick={() => {
                                                    const prev = telegramLocalMode;
                                                    setTelegramLocalModeState(opt.value);
                                                    SetTelegramLocalMode(opt.value).then(() => {
                                                        LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                                                    }).catch((err: any) => {
                                                        setTelegramLocalModeState(prev);
                                                        showToastMessage(err?.message || err || '切换失败');
                                                    });
                                                }}
                                            >
                                                {opt.label}
                                            </button>
                                        ))}
                                    </div>

                                    <div style={{ maxWidth: '520px' }}>
                                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                            <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-muted)', whiteSpace: 'nowrap', minWidth: '62px' }}>Bot Token</label>
                                            <input
                                                type="password"
                                                value={(config as any)?.telegram_bot_token || ''}
                                                onChange={(e) => saveRemoteConfigField({ telegram_bot_token: e.target.value } as any)}
                                                placeholder="e.g. 123456:ABC-DEF1234ghIkl-zyx57W2v1u123ew11"
                                                style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)' }}
                                            />
                                        </div>
                                    </div>
                                </div>
                                )}

                                {/* WeChat tab */}
                                {imSubTab === 'weixin' && (
                                <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
                                    <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginBottom: '12px', marginTop: 0 }}>
                                        {lang === 'zh-Hans' || lang === 'zh-Hant'
                                            ? '扫码登录微信，通过微信与 MaClaw Agent 对话。'
                                            : 'Scan QR code to log in to WeChat and chat with MaClaw Agent.'}
                                    </p>

                                    {/* Status + controls row */}
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '12px', flexWrap: 'wrap' }}>
                                        <span style={{
                                            fontSize: '0.7rem',
                                            padding: '2px 8px',
                                            borderRadius: '10px',
                                            background: weixinStatus === 'connected' ? 'var(--theme-success-bg)'
                                                : ['connecting', 'reconnecting', 'paused'].includes(weixinStatus) ? 'var(--theme-warning-bg)'
                                                : 'var(--theme-danger-bg)',
                                            color: weixinStatus === 'connected' ? 'var(--theme-success)'
                                                : ['connecting', 'reconnecting', 'paused'].includes(weixinStatus) ? 'var(--theme-warning)'
                                                : 'var(--theme-danger)',
                                        }}>
                                            {{ connected: '● 已连接', connecting: '◌ 连接中...', reconnecting: '◌ 重连中...', paused: '◌ 已暂停', error: '✕ 错误' }[weixinStatus] || '○ 未连接'}
                                        </span>
                                        {(config as any)?.weixin_account_id && (
                                            <span style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)' }}>
                                                ID: {(config as any).weixin_account_id}
                                            </span>
                                        )}
                                        {weixinStatus === 'connected' && (
                                            <>
                                                <button
                                                    type="button"
                                                    aria-label="Restart WeChat connection"
                                                    style={{ fontSize: '0.68rem', padding: '2px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', background: 'transparent', color: 'var(--theme-text-secondary)', cursor: 'pointer' }}
                                                    onClick={() => RestartWeixin().then(setWeixinStatus)}
                                                >
                                                    {lang === 'zh-Hans' ? '重启' : 'Restart'}
                                                </button>
                                                <button
                                                    type="button"
                                                    aria-label="Disconnect WeChat"
                                                    style={{ fontSize: '0.68rem', padding: '2px 8px', borderRadius: '4px', border: '1px solid var(--theme-danger)', background: 'transparent', color: 'var(--theme-danger)', cursor: 'pointer' }}
                                                    onClick={() => { StopWeixin(); setWeixinStatus('disconnected'); }}
                                                >
                                                    {lang === 'zh-Hans' ? '断开' : 'Disconnect'}
                                                </button>
                                            </>
                                        )}
                                    </div>

                                    {/* 单机/多机 mode selector */}
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '16px' }}>
                                        <span style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)' }}>
                                            {lang === 'zh-Hans' || lang === 'zh-Hant' ? '通道：' : 'Mode:'}
                                        </span>
                                        {((() => {
                                            const isCN = lang === 'zh-Hans' || lang === 'zh-Hant';
                                            return [
                                                { value: true, label: isCN ? '🖥 单机' : '🖥 Local', desc: isCN ? '本地 LLM 直连' : 'Direct local LLM' },
                                                { value: false, label: isCN ? '🌐 多机' : '🌐 Remote', desc: isCN ? '通过 Hub 转发' : 'Via Hub' },
                                            ];
                                        })()).map((opt) => (
                                            <button
                                                key={String(opt.value)}
                                                type="button"
                                                aria-label={opt.desc}
                                                title={opt.desc}
                                                style={{
                                                    padding: '4px 14px',
                                                    borderRadius: '14px',
                                                    border: weixinLocalMode === opt.value ? '1.5px solid var(--theme-primary)' : '1px solid var(--theme-border)',
                                                    background: weixinLocalMode === opt.value ? 'var(--theme-info-bg)' : 'transparent',
                                                    color: weixinLocalMode === opt.value ? 'var(--theme-primary)' : 'var(--theme-text-secondary)',
                                                    fontWeight: weixinLocalMode === opt.value ? 600 : 400,
                                                    fontSize: '0.75rem',
                                                    cursor: 'pointer',
                                                    transition: 'all 0.15s',
                                                }}
                                                onClick={() => {
                                                    const prev = weixinLocalMode;
                                                    setWeixinLocalModeState(opt.value);
                                                    SetWeixinLocalMode(opt.value).then(() => {
                                                        // Sync config state so subsequent SaveConfig calls
                                                        // don't overwrite weixin_local_mode with stale value
                                                        LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                                                    }).catch((err: any) => {
                                                        setWeixinLocalModeState(prev);
                                                        showToastMessage(err?.message || err || '切换失败');
                                                    });
                                                }}
                                            >
                                                {opt.label}
                                            </button>
                                        ))}
                                    </div>

                                    {/* QR Login section */}
                                    {weixinStatus !== 'connected' && (
                                    <div style={{ marginTop: '4px' }}>
                                        {!weixinQRCode && !weixinQRLoading && !weixinQRWaiting && (
                                            <button
                                                type="button"
                                                aria-label="Scan QR code to login WeChat"
                                                style={{
                                                    padding: '6px 18px',
                                                    borderRadius: '6px',
                                                    border: '1.5px solid var(--theme-primary)',
                                                    background: 'var(--theme-info-bg)',
                                                    color: 'var(--theme-primary)',
                                                    fontWeight: 600,
                                                    fontSize: '0.78rem',
                                                    cursor: 'pointer',
                                                }}
                                                onClick={async () => {
                                                    setWeixinQRError('');
                                                    setWeixinQRLoading(true);
                                                    try {
                                                        const res = await StartWeixinQRLogin();
                                                        if (res.error) {
                                                            setWeixinQRError(res.error);
                                                            setWeixinQRLoading(false);
                                                            return;
                                                        }
                                                        setWeixinQRCode(res.qrcode_url || '');
                                                        setWeixinQRLoading(false);
                                                        setWeixinQRWaiting(true);
                                                        const waitRes = await WaitWeixinQRLogin(res.qrcode_token || '');
                                                        setWeixinQRWaiting(false);
                                                        setWeixinQRCode('');
                                                        if (waitRes.error) {
                                                            setWeixinQRError(waitRes.error);
                                                        } else {
                                                            setWeixinStatus('connected');
                                                            LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                                                        }
                                                    } catch (e: any) {
                                                        setWeixinQRError(e?.message || String(e));
                                                        setWeixinQRLoading(false);
                                                        setWeixinQRWaiting(false);
                                                        setWeixinQRCode('');
                                                    }
                                                }}
                                            >
                                                {lang === 'zh-Hans' || lang === 'zh-Hant' ? '🔑 扫码登录微信' : '🔑 Scan QR to Login'}
                                            </button>
                                        )}

                                        {weixinQRLoading && (
                                            <p style={{ fontSize: '0.75rem', color: 'var(--theme-primary)' }}>
                                                {lang === 'zh-Hans' ? '正在获取二维码...' : 'Loading QR code...'}
                                            </p>
                                        )}

                                        {weixinQRCode && (
                                            <div style={{ textAlign: 'center', maxWidth: '280px' }}>
                                                <QRCodeSVG
                                                    value={weixinQRCode}
                                                    size={220}
                                                    level="M"
                                                    bgColor="var(--theme-surface)"
                                                    style={{ borderRadius: '8px', border: '1px solid var(--theme-border)', padding: '8px', background: 'var(--theme-surface)' }}
                                                />
                                                <p style={{ fontSize: '0.72rem', color: 'var(--theme-primary)', marginTop: '8px' }}>
                                                    {lang === 'zh-Hans' || lang === 'zh-Hant' ? '请用微信扫描上方二维码' : 'Scan the QR code with WeChat'}
                                                </p>
                                                {weixinQRWaiting && (
                                                    <p style={{ fontSize: '0.68rem', color: 'var(--theme-text-muted)' }}>
                                                        {lang === 'zh-Hans' ? '等待扫码确认中...' : 'Waiting for confirmation...'}
                                                    </p>
                                                )}
                                                <button
                                                    type="button"
                                                    aria-label="Cancel QR login"
                                                    style={{ marginTop: '10px', fontSize: '0.72rem', padding: '3px 14px', borderRadius: '4px', border: '1px solid var(--theme-border)', background: 'transparent', color: 'var(--theme-text-muted)', cursor: 'pointer' }}
                                                    onClick={() => {
                                                        setWeixinQRCode('');
                                                        setWeixinQRWaiting(false);
                                                        setWeixinQRLoading(false);
                                                    }}
                                                >
                                                    {lang === 'zh-Hans' || lang === 'zh-Hant' ? '取消' : 'Cancel'}
                                                </button>
                                            </div>
                                        )}

                                        {weixinQRError && (
                                            <p style={{ fontSize: '0.72rem', color: 'var(--theme-danger)', marginTop: '8px' }}>
                                                {weixinQRError}
                                            </p>
                                        )}
                                    </div>
                                    )}
                                </div>
                                )}

                                {/* Lansenger tab — only visible for qianxin/TigerClaw OEM */}
                                {imSubTab === 'lansenger' && brandInfo?.id === 'qianxin' && (
                                <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
                                    <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginBottom: '12px', marginTop: 0 }}>
                                        {lang === 'zh-Hans'
                                            ? '配置蓝信机器人凭证，通过 WebSocket 长连接接入蓝信与 MaClaw Agent 对话。'
                                            : lang === 'zh-Hant'
                                            ? '配置藍信機器人憑證，透過 WebSocket 長連接接入藍信與 MaClaw Agent 對話。'
                                            : 'Configure Lansenger bot credentials to connect via WebSocket and chat with MaClaw Agent.'}
                                    </p>

                                    <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '12px', flexWrap: 'wrap' }}>
                                        <label style={{ display: 'flex', alignItems: 'center', gap: '6px', cursor: 'pointer', fontSize: '0.78rem' }}>
                                            <input
                                                type="checkbox"
                                                checked={(config as any)?.lansenger_enabled || false}
                                                onChange={async (e) => {
                                                    if (!config) return;
                                                    const enabled = e.target.checked;
                                                    const next = new main.AppConfig({ ...config, lansenger_enabled: enabled } as any);
                                                    setConfig(next);
                                                    try { await SaveConfig(next); } catch (err) { console.error('[lansenger] save failed:', err); }
                                                    if (enabled) {
                                                        try {
                                                            const s = await RestartLansenger();
                                                            setLansengerStatus(typeof s === 'string' ? s : 'disconnected');
                                                        } catch (err: any) {
                                                            console.error('[lansenger] restart failed:', err);
                                                        }
                                                    } else {
                                                        try {
                                                            await StopLansenger();
                                                        } catch (err: any) {
                                                            console.error('[lansenger] stop failed:', err);
                                                        }
                                                        setLansengerStatus('disabled');
                                                    }
                                                }}
                                            />
                                            {lang === 'zh-Hans' ? '启用蓝信' : lang === 'zh-Hant' ? '啟用藍信' : 'Enable Lansenger'}
                                        </label>
                                        <span style={{
                                            fontSize: '0.7rem',
                                            padding: '2px 8px',
                                            borderRadius: '10px',
                                            background: lansengerStatus === 'connected' ? 'var(--theme-success-bg)' : lansengerStatus === 'disconnected' || lansengerStatus === 'disabled' ? 'var(--theme-surface-muted)' : lansengerStatus === 'error' ? 'var(--theme-danger-bg)' : 'var(--theme-warning-bg)',
                                            color: lansengerStatus === 'connected' ? 'var(--theme-success)' : lansengerStatus === 'disconnected' || lansengerStatus === 'disabled' ? 'var(--theme-text-secondary)' : lansengerStatus === 'error' ? 'var(--theme-danger)' : 'var(--theme-warning)',
                                        }}>
                                            {{ connected: '● 已连接', connecting: '◌ 连接中...', reconnecting: '◌ 重连中...', disconnected: '○ 未连接', disabled: '○ 未启用', error: '✕ 错误' }[lansengerStatus] || `◌ ${lansengerStatus}`}
                                        </span>
                                        <button
                                            type="button"
                                            style={{ fontSize: '0.68rem', padding: '2px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', background: 'transparent', color: 'var(--theme-text-secondary)', cursor: 'pointer' }}
                                            disabled={!(config as any)?.lansenger_enabled}
                                            onClick={async () => {
                                                try {
                                                    const s = await RestartLansenger();
                                                    setLansengerStatus(typeof s === 'string' ? s : 'disconnected');
                                                } catch (e: any) {
                                                    showToastMessage(e?.message || String(e));
                                                }
                                            }}
                                        >
                                            {lang === 'zh-Hans' ? '重新连接' : lang === 'zh-Hant' ? '重新連接' : 'Reconnect'}
                                        </button>
                                    </div>

                                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '16px' }}>
                                        <span style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)' }}>
                                            {lang === 'zh-Hans' || lang === 'zh-Hant' ? '通道：' : 'Mode:'}
                                        </span>
                                        {[
                                            { value: true, label: lang === 'zh-Hans' || lang === 'zh-Hant' ? '🖥 单机' : '🖥 Local', desc: lang === 'zh-Hans' || lang === 'zh-Hant' ? '本地 LLM 直连' : 'Direct local LLM' },
                                            { value: false, label: lang === 'zh-Hans' || lang === 'zh-Hant' ? '🌐 多机' : '🌐 Remote', desc: lang === 'zh-Hans' || lang === 'zh-Hant' ? '通过 Hub 转发' : 'Via Hub' },
                                        ].map((opt) => (
                                            <button
                                                key={String(opt.value)}
                                                type="button"
                                                aria-label={opt.desc}
                                                title={opt.desc}
                                                style={{
                                                    padding: '4px 14px',
                                                    borderRadius: '14px',
                                                    border: lansengerLocalMode === opt.value ? '1.5px solid var(--theme-primary)' : '1px solid var(--theme-border)',
                                                    background: lansengerLocalMode === opt.value ? 'var(--theme-info-bg)' : 'transparent',
                                                    color: lansengerLocalMode === opt.value ? 'var(--theme-primary)' : 'var(--theme-text-secondary)',
                                                    fontWeight: lansengerLocalMode === opt.value ? 600 : 400,
                                                    fontSize: '0.75rem',
                                                    cursor: 'pointer',
                                                    transition: 'all 0.15s',
                                                }}
                                                onClick={() => {
                                                    const prev = lansengerLocalMode;
                                                    setLansengerLocalModeState(opt.value);
                                                    SetLansengerLocalMode(opt.value).then(() => {
                                                        LoadConfig().then((c: any) => setConfig(c)).catch(() => {});
                                                    }).catch((err: any) => {
                                                        setLansengerLocalModeState(prev);
                                                        showToastMessage(err?.message || err || '切换失败');
                                                    });
                                                }}
                                            >
                                                {opt.label}
                                            </button>
                                        ))}
                                    </div>

                                    <div style={{ maxWidth: '680px', display: 'grid', gap: '10px' }}>
                                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                            <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '84px' }}>App ID</label>
                                            <input
                                                type="text"
                                                value={(config as any)?.lansenger_app_id || ''}
                                                onChange={(e) => saveRemoteConfigField({ lansenger_app_id: e.target.value } as any)}
                                                placeholder="2285568-16138496"
                                                autoComplete="off"
                                                spellCheck={false}
                                                style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)', cursor: 'text' }}
                                            />
                                        </div>
                                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                            <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '84px' }}>App Secret</label>
                                            <input
                                                type="password"
                                                value={(config as any)?.lansenger_app_secret || ''}
                                                onChange={(e) => saveRemoteConfigField({ lansenger_app_secret: e.target.value } as any)}
                                                placeholder="FC0CADED7561247CAA2D2C4E5DEF17B8"
                                                autoComplete="off"
                                                style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)', cursor: 'text' }}
                                            />
                                        </div>
                                        <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                            <label style={{ fontSize: '0.75rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', minWidth: '84px' }}>
                                                {lang === 'zh-Hans' || lang === 'zh-Hant' ? 'API 网关' : 'API Gateway'}
                                            </label>
                                            <input
                                                type="text"
                                                value={(config as any)?.lansenger_gateway_url || ''}
                                                onChange={(e) => saveRemoteConfigField({ lansenger_gateway_url: e.target.value } as any)}
                                                placeholder="https://apigw.lx.qianxin.com"
                                                autoComplete="off"
                                                spellCheck={false}
                                                style={{ flex: 1, padding: '6px 8px', borderRadius: '4px', border: '1px solid var(--theme-border)', fontSize: '0.78rem', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)', cursor: 'text' }}
                                            />
                                        </div>
                                        <div style={{ fontSize: '0.68rem', color: 'var(--theme-text-muted)', marginTop: '2px' }}>
                                            {lang === 'zh-Hans'
                                                ? '在蓝信PC客户端 → 个人机器人 → 创建机器人后获取 AppID 和 AppSecret。API 网关地址一般无需修改。'
                                                : 'Get AppID and AppSecret from Lansenger PC client → Personal Bot → Create Bot. API Gateway URL usually does not need to be changed.'}
                                        </div>
                                    </div>
                                </div>
                                )}
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'security' ? 'block' : 'none' }}>
                                <SecurityPolicyPanel config={config} saveRemoteConfigField={saveRemoteConfigField} lang={lang} />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'system' ? 'block' : 'none' }}>
                                <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
                                    <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '12px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
                                        {lang === 'zh-Hans' ? '系统设置' : lang === 'zh-Hant' ? '系統設置' : 'System Settings'}
                                    </h4>
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '16px', flexWrap: 'wrap' }}>
                                        <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                                            <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap' }}>{lang === 'zh-Hans' ? '心跳间隔（秒）' : lang === 'zh-Hant' ? '心跳間隔（秒）' : 'Heartbeat Interval (sec)'}</label>
                                            <input
                                                className="form-input"
                                                type="number"
                                                min={5}
                                                step={1}
                                                style={{ width: '70px' }}
                                                value={config?.remote_heartbeat_sec || 10}
                                                onChange={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Number(e.target.value || 10) })}
                                                onBlur={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Math.max(5, Number(e.target.value || 10)) })}
                                            />
                                        </div>
                                        <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                                            <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap' }}>{lang === 'zh-Hans' ? '熄屏时间（分钟）' : lang === 'zh-Hant' ? '熄屏時間（分鐘）' : 'Screen Dim Timeout (min)'}</label>
                                            <input
                                                className="form-input"
                                                type="number"
                                                min={0}
                                                step={1}
                                                style={{ width: '70px' }}
                                                value={(config as any)?.screen_dim_timeout_min ?? 3}
                                                onChange={(e) => saveRemoteConfigField({ screen_dim_timeout_min: Number(e.target.value || 0) } as any)}
                                                onBlur={(e) => saveRemoteConfigField({ screen_dim_timeout_min: Math.max(0, Number(e.target.value || 0)) } as any)}
                                                title={lang === 'zh-Hans' ? '无键鼠操作多少分钟后熄屏节能（0=禁用）。防锁屏开启时有效。' : 'Minutes of inactivity before screen dims (0=disabled). Effective when screen-lock prevention is on.'}
                                            />
                                        </div>
                                    </div>
                                    <div style={{ marginTop: '12px' }}>
                                        <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                            <input
                                                type="checkbox"
                                                checked={(config as any)?.workstation_mode === true}
                                                onChange={(e) => {
                                                    if (config) {
                                                        const newConfig = new main.AppConfig({ ...config, workstation_mode: e.target.checked } as any);
                                                        setConfig(newConfig);
                                                        SaveConfig(newConfig);
                                                    }
                                                }}
                                                style={{ width: '16px', height: '16px' }}
                                            />
                                            <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>
                                                {lang === 'zh-Hans' ? '工作站模式' : lang === 'zh-Hant' ? '工作站模式' : 'Workstation Mode'}
                                            </span>
                                        </label>
                                        <div style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)', marginTop: '4px', marginLeft: '24px', textAlign: 'left' }}>
                                            {lang === 'zh-Hans'
                                                ? '开启后不休眠、不锁屏，但允许黑屏。方便截屏测试和调试。'
                                                : lang === 'zh-Hant'
                                                ? '開啟後不休眠、不鎖屏，但允許黑屏。方便截屏測試和除錯。'
                                                : 'Prevents sleep & screen lock while allowing display off. Useful for screenshot testing and debugging.'}
                                        </div>
                                    </div>
                                </div>

                                {/* Diagnostics info block */}
                                <div className="form-group" style={{ marginTop: '16px', borderTop: '1px solid var(--theme-border)', paddingTop: '16px' }}>
                                    <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '12px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>
                                        {lang === 'zh-Hans' ? '诊断信息' : lang === 'zh-Hant' ? '診斷資訊' : 'Diagnostics'}
                                    </h4>
                                    <div style={{ fontSize: '0.75rem', fontFamily: 'monospace', color: 'var(--theme-text-secondary)', lineHeight: 1.8, background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '10px 12px', wordBreak: 'break-all', textAlign: 'left' }}>
                                        <div style={{ textAlign: 'left' }}>Machine ID: {config?.remote_machine_id || '(未激活)'}</div>
                                        <div style={{ textAlign: 'left' }}>User ID: {config?.remote_user_id || '(未激活)'}</div>
                                        <div style={{ textAlign: 'left' }}>Client ID: {config?.remote_client_id || '(未生成)'}</div>
                                        <div style={{ textAlign: 'left' }}>SN: {config?.remote_sn || '(未激活)'}</div>
                                        <div style={{ textAlign: 'left' }}>Hub URL: {config?.remote_hub_url || '(未设置)'}</div>
                                        <div style={{ textAlign: 'left' }}>Email: {config?.remote_email || '(未设置)'}</div>
                                        <div style={{ textAlign: 'left' }}>WeChat Mode: {(config as any)?.weixin_local_mode === false ? '多机 (Hub)' : '单机 (Local)'}</div>
                                    </div>
                                </div>
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'ui' ? 'block' : 'none' }}>
                                <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0', marginBottom: '16px' }}>
                                    <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '12px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>{lang === 'zh-Hans' ? '界面缩放' : lang === 'zh-Hant' ? '介面縮放' : 'UI Zoom'}</h4>
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                                        <input type="range" min={50} max={200} step={5} value={Math.round(uiZoom * 100)}
                                            onChange={e => {
                                                const v = Number(e.target.value) / 100;
                                                setUiZoom(v);
                                            }}
                                            onPointerUp={async (e) => {
                                                const v = Number((e.currentTarget as HTMLInputElement).value) / 100;
                                                setUiZoom(v);
                                                await SetUIZoomFactor(v).catch(() => {});
                                            }}
                                            style={{ flex: 1, accentColor: 'var(--theme-primary)' }} />
                                        <span style={{ fontSize: '0.78rem', color: 'var(--theme-text-secondary)', minWidth: '42px', textAlign: 'center' }}>{Math.round(uiZoom * 100)}%</span>
                                        <button onClick={() => { setUiZoom(1.0); SetUIZoomFactor(1.0).catch(() => {}); }}
                                            style={{ fontSize: '0.72rem', padding: '3px 10px', cursor: 'pointer', background: 'var(--theme-surface-muted)', color: 'var(--theme-text-secondary)', border: '1px solid var(--theme-border)', borderRadius: 4 }}>
                                            {lang === 'zh-Hans' ? '重置' : lang === 'zh-Hant' ? '重置' : 'Reset'}
                                        </button>
                                    </div>
                                    <p style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)', marginTop: '6px', marginBottom: 0 }}>
                                        {lang === 'zh-Hans' ? '调整界面整体缩放比例，适配高 DPI 屏幕或个人偏好。' : lang === 'zh-Hant' ? '調整介面整體縮放比例，適配高 DPI 螢幕或個人偏好。' : 'Adjust overall UI scale for HiDPI displays or personal preference.'}
                                    </p>
                                </div>
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'display' ? 'block' : 'none' }}>

                            {/* Default Coding Tool + Default Provider — same row */}
                            <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
                                <div style={{ display: 'flex', gap: '24px', alignItems: 'flex-start', flexWrap: 'wrap' }}>
                                    {/* Default Coding Tool */}
                                    <div style={{ flex: '1 1 0', minWidth: '180px', maxWidth: config?.default_tool ? undefined : '320px' }}>
                                        <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '8px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>{lang === 'zh-Hans' ? '默认编程工具' : lang === 'zh-Hant' ? '預設編程工具' : 'Default Coding Tool'}</h4>
                                        <select
                                            className="form-input"
                                            value={config?.default_tool || ''}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({
                                                        ...config,
                                                        default_tool: e.target.value,
                                                        default_tool_provider: '',
                                                    });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '100%', fontSize: '0.8rem', padding: '4px 8px', height: '30px' }}
                                        >
                                            <option value="">{lang === 'zh-Hans' ? 'Auto (品牌默认)' : lang === 'zh-Hant' ? 'Auto (品牌預設)' : 'Auto (Brand Default)'}</option>
                                            {remoteToolMetadata.map((tool) => (
                                                <option
                                                    key={tool.name}
                                                    value={tool.name}
                                                    disabled={!tool.installed}
                                                >
                                                    {tool.display_name}{!tool.installed ? (lang === 'zh-Hans' ? ' (未安装)' : lang === 'zh-Hant' ? ' (未安裝)' : ' (Not Installed)') : ''}
                                                </option>
                                            ))}
                                        </select>
                                        <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginTop: '6px' }}>
                                            {lang === 'zh-Hans' ? '选择 AI 编程会话默认使用的工具。Auto 将使用品牌默认工具。' :
                                                lang === 'zh-Hant' ? '選擇 AI 編程會話預設使用的工具。Auto 將使用品牌預設工具。' :
                                                    'Choose the default tool for AI coding sessions. Auto uses the brand default.'}
                                        </p>
                                    </div>
                                    {/* Default Provider — only visible when a specific tool is selected */}
                                    {config?.default_tool ? (
                                    <div style={{ flex: '1 1 0', minWidth: '180px' }}>
                                        <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '8px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>{lang === 'zh-Hans' ? '默认服务商' : lang === 'zh-Hant' ? '預設服務商' : 'Default Provider'}</h4>
                                        <select
                                            className="form-input"
                                            value={config?.default_tool_provider || ''}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({
                                                        ...config,
                                                        default_tool_provider: e.target.value,
                                                    });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '100%', fontSize: '0.8rem', padding: '4px 8px', height: '30px' }}
                                        >
                                            <option value="">{lang === 'zh-Hans' ? 'Auto (自动选择)' : lang === 'zh-Hant' ? 'Auto (自動選擇)' : 'Auto (Auto Select)'}</option>
                                            {toolProviders.map((provider) => (
                                                <option
                                                    key={provider.name}
                                                    value={provider.name}
                                                >
                                                    {provider.name}
                                                </option>
                                            ))}
                                        </select>
                                        <p style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', marginTop: '6px' }}>
                                            {lang === 'zh-Hans' ? '选择默认工具使用的服务商。Auto 将自动选择第一个可用的服务商。' :
                                                lang === 'zh-Hant' ? '選擇預設工具使用的服務商。Auto 將自動選擇第一個可用的服務商。' :
                                                    'Choose the default provider for the selected tool. Auto picks the first available provider.'}
                                        </p>
                                    </div>
                                    ) : null}
                                </div>
                            </div>

                            <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
                                <h4 style={{ fontSize: '0.8rem', color: 'var(--theme-primary)', marginBottom: '12px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>{lang === 'zh-Hans' ? '工具显示' : lang === 'zh-Hant' ? '工具顯示' : 'Tool Visibility'}</h4>
                                <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '10px' }}>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.show_gemini !== false}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({ ...config, show_gemini: e.target.checked });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '16px', height: '16px' }}
                                        />
                                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>Gemini CLI</span>
                                    </label>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.show_codex !== false}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({ ...config, show_codex: e.target.checked });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '16px', height: '16px' }}
                                        />
                                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>OpenAI Codex</span>
                                    </label>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.show_opencode !== false}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({ ...config, show_opencode: e.target.checked });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '16px', height: '16px' }}
                                        />
                                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>OpenCode AI</span>
                                    </label>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.show_codebuddy !== false}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({ ...config, show_codebuddy: e.target.checked });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '16px', height: '16px' }}
                                        />
                                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>CodeBuddy</span>
                                    </label>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: isWindows ? 'not-allowed' : 'pointer', opacity: isWindows ? 0.5 : 1 }}>
                                        <input
                                            type="checkbox"
                                            checked={isWindows ? false : config?.show_cursor !== false}
                                            disabled={isWindows}
                                            onChange={(e) => {
                                                if (config && !isWindows) {
                                                    const newConfig = new main.AppConfig({ ...config, show_cursor: e.target.checked });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '16px', height: '16px' }}
                                        />
                                        <span style={{ fontSize: '0.8rem', color: isWindows ? 'var(--theme-text-muted)' : 'var(--theme-text-secondary)' }}>Cursor Agent{isWindows ? ' (macOS/Linux)' : ''}</span>
                                    </label>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.show_iflow !== false}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({ ...config, show_iflow: e.target.checked });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '16px', height: '16px' }}
                                        />
                                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>iFlow CLI</span>
                                    </label>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.show_kilo !== false}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({ ...config, show_kilo: e.target.checked });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '16px', height: '16px' }}
                                        />
                                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>Kilo Code CLI</span>
                                    </label>
                                </div>
                            </div>



                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'general' ? 'block' : 'none' }}>
                            <div className="form-group" style={{ marginTop: '0', borderTop: 'none', paddingTop: '0' }}>
                                <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                    <input
                                        type="checkbox"
                                        checked={!config?.hide_startup_popup}
                                        onChange={(e) => {
                                            if (config) {
                                                const newConfig = new main.AppConfig({ ...config, hide_startup_popup: !e.target.checked });
                                                setConfig(newConfig);
                                                SaveConfig(newConfig);
                                            }
                                        }}
                                        style={{ width: '16px', height: '16px' }}
                                    />
                                    <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-primary)' }}>{t("showWelcomePage")}</span>
                                </label>
                                <p style={{ fontSize: '0.75rem', color: 'var(--theme-text-muted)', marginLeft: '24px', marginTop: '4px' }}>
                                    {lang === 'zh-Hans' ? '开启后，程序启动时将显示新手教学和快速入门链接' :
                                        lang === 'zh-Hant' ? '開啟後，程序啟動時將顯示新手教學和快速入門鏈接' :
                                            'When enabled, a welcome popup with tutorial links will be shown at startup.'}
                                </p>
                            </div>

                            <div className="form-group" style={{ marginTop: '10px', borderTop: '1px solid var(--theme-border-subtle)', paddingTop: '10px', display: 'flex', flexDirection: 'column', gap: '12px' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: '20px', flexWrap: 'wrap' }}>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.pause_env_check}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({ ...config, pause_env_check: e.target.checked });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '16px', height: '16px' }}
                                        />
                                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-primary)' }}>{t("pauseEnvCheck")}</span>
                                    </label>
                                    {/* Windows Terminal option - only show when available */}
                                    {hasWindowsTerminal && (
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.use_windows_terminal}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({ ...config, use_windows_terminal: e.target.checked });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '16px', height: '16px' }}
                                        />
                                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-primary)' }}>{t("useWindowsTerminal")}</span>
                                    </label>
                                    )}
                                </div>
                                {config?.pause_env_check && (
                                    <div style={{ marginLeft: '24px', display: 'flex', alignItems: 'center', gap: '8px' }}>
                                        <label style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>
                                            <span>{t("envCheckIntervalPrefix")}</span>
                                            <select
                                                value={envCheckInterval}
                                                onChange={(e) => {
                                                    const days = parseInt(e.target.value);
                                                    setEnvCheckInterval(days);
                                                    SetEnvCheckInterval(days);
                                                }}
                                                style={{
                                                    padding: '3px 6px',
                                                    borderRadius: '4px',
                                                    border: '1px solid var(--theme-border)',
                                                    fontSize: '0.8rem',
                                                    width: '60px'
                                                }}
                                            >
                                                {Array.from({ length: 29 }, (_, i) => i + 2).map(day => (
                                                    <option key={day} value={day}>{day}</option>
                                                ))}
                                            </select>
                                            <span>{t("envCheckIntervalSuffix")}</span>
                                        </label>
                                    </div>
                                )}
                            </div>

                            <div className="form-group" style={{ marginTop: '10px', borderTop: '1px solid var(--theme-border-subtle)', paddingTop: '10px' }}>
                                <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                    <input
                                        type="checkbox"
                                        checked={config?.maclaw_debug_tool_calls || false}
                                        onChange={(e) => {
                                            if (config) {
                                                const newConfig = new main.AppConfig({ ...config, maclaw_debug_tool_calls: e.target.checked });
                                                setConfig(newConfig);
                                                SaveConfig(newConfig);
                                            }
                                        }}
                                        style={{ width: '16px', height: '16px' }}
                                    />
                                    <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-primary)' }}>MaClaw Debug</span>
                                </label>
                                <p style={{ fontSize: '0.75rem', color: 'var(--theme-text-muted)', marginLeft: '24px', marginTop: '4px' }}>
                                    {lang === 'zh-Hans' || lang === 'zh' ? '开启后，远程会话中将显示工具调用过程（如"正在执行工具…"）；关闭后仅显示最终结果和错误信息' :
                                        lang === 'zh-Hant' ? '開啟後，遠端會話中將顯示工具調用過程；關閉後僅顯示最終結果和錯誤信息' :
                                            'When enabled, tool call progress (e.g. "Executing tool…") is shown during remote sessions. When disabled, only final results and errors are displayed.'}
                                </p>
                            </div>
                            </div>
                        </div>
                    )}

                    {navTab === 'agentnet' && (
                        <AgentNetTabContainer lang={lang} agentNetRunning={agentNetRunning} />
                    )}

                    {navTab === 'about' && (
                        <AboutPanel
                            currentIcon={currentIcon}
                            brandInfo={brandInfo}
                            appVersion={appVersion}
                            buildNumber={buildNumber}
                            thanksContent={thanksContent}
                            t={t}
                            onOpenWebsite={() => BrowserOpenURL(brandInfo?.websiteURL || "https://maclaw.top")}
                            onCheckUpdate={() => {
                                setStatus(t("checkingUpdate"));
                                CheckUpdate(appVersion).then(res => {
                                    console.log("CheckUpdate result:", res);
                                    setUpdateResult(res);
                                    setIsStartupUpdateCheck(false);
                                    setShowUpdateModal(true);
                                    setStatus("");
                                }).catch(err => {
                                    console.error("CheckUpdate error:", err);
                                    setStatus("检查更新失败: " + err);
                                    setUpdateResult({
                                        has_update: false,
                                        latest_version: "获取失败",
                                        release_url: ""
                                    });
                                    setIsStartupUpdateCheck(false);
                                    setShowUpdateModal(true);
                                });
                            }}
                            onShowInstallLog={() => setShowInstallLog(true)}
                            onOpenBugReport={() => BrowserOpenURL(brandInfo?.githubURL ? brandInfo.githubURL + "/issues/new" : "https://github.com/rapidai/maclaw/issues/new")}
                            onOpenGithub={() => BrowserOpenURL(brandInfo?.githubURL || "https://github.com/rapidai/maclaw")}
                        />
                    )}
                </div>

                {/* Global Action Bar (Footer) */}
                {config && isToolTab(navTab) && (
                    <div className="global-action-bar" style={{ '--wails-draggable': 'no-drag' } as any} data-ai-theme={aiThemeMode}>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: '5px', width: '100%', padding: '2px 0', '--wails-draggable': 'no-drag' } as any}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: '20px', justifyContent: 'flex-start' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                    {/* runnerStatus label removed */}
                                    <span style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--theme-primary)' , textTransform: 'capitalize' }}>{activeTool}</span>
                                    <span style={{ color: 'var(--theme-border)' }}>|</span>
                                    <span
                                        style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--theme-text-primary)' }}
                                        title={(config as any)[activeTool].current_model === "Original" ? t("original") : (config as any)[activeTool].current_model}
                                    >
                                        {(() => {
                                            const modelName = (config as any)[activeTool].current_model === "Original" ? t("original") : (config as any)[activeTool].current_model;
                                            return modelName.length > 10 ? `${modelName.slice(0, 4)}...${modelName.slice(-4)}` : modelName;
                                        })()}
                                    </span>
                                </div>
                                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                                {activeTool !== 'kilo' && (
                                    <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>
                                        <input
                                            type="checkbox"
                                            checked={resolvedLaunchProject?.yolo_mode || false}
                                            onChange={(e) => {
                                                updateResolvedLaunchProject((project) => {
                                                    const updated = { ...project, yolo_mode: e.target.checked };
                                                    if (!isWindows && e.target.checked) {
                                                        updated.admin_mode = false;
                                                    }
                                                    return updated;
                                                });
                                            }}
                                        />
                                        <span>{t("yoloModeLabel")}</span>
                                        {resolvedLaunchProject?.yolo_mode && (
                                            <span style={{
                                                marginLeft: '2px',
                                                backgroundColor: 'var(--theme-danger-bg)',
                                                color: 'var(--theme-danger)',
                                                padding: '0 4px',
                                                borderRadius: '3px',
                                                fontSize: '0.6rem',
                                                fontWeight: 'bold'
                                            }}>
                                                {t("danger")}
                                            </span>
                                        )}
                                    </label>
                                )}
                                {activeTool === 'claude' && (
                                    <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>
                                        <input
                                            type="checkbox"
                                            checked={resolvedLaunchProject?.team_mode || false}
                                            onChange={(e) => updateResolvedLaunchProject((project) => ({ ...project, team_mode: e.target.checked }))}
                                        />
                                        <span>{t("teamModeLabel")}</span>
                                    </label>
                                )}
                                {!isWindows && (
                                    <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>
                                        <input
                                            type="checkbox"
                                            checked={resolvedLaunchProject?.use_proxy || false}
                                            onChange={(e) => {
                                                if (e.target.checked && !resolvedLaunchProject?.proxy_host && !config?.default_proxy_host) {
                                                    setShowProxySettings(true);
                                                    return;
                                                }
                                                updateResolvedLaunchProject((project) => ({ ...project, use_proxy: e.target.checked }));
                                            }}
                                        />
                                        <span>{t("proxyMode")}</span>
                                        <span
                                            onClick={(e) => {
                                                e.preventDefault();
                                                e.stopPropagation();
                                                setShowProxySettings(true);
                                            }}
                                            style={{ marginLeft: '4px', cursor: 'pointer', opacity: 0.7 }}
                                            title={t("proxySettings")}
                                        >
                                            ⚙️
                                        </span>
                                    </label>
                                )}
                                <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>
                                    <input
                                        type="checkbox"
                                        checked={resolvedLaunchProject?.admin_mode || false}
                                        onChange={(e) => {
                                            updateResolvedLaunchProject((project) => {
                                                const updated = { ...project, admin_mode: e.target.checked };
                                                if (!isWindows && e.target.checked) {
                                                    updated.yolo_mode = false;
                                                }
                                                return updated;
                                            });
                                        }}
                                        style={{ marginRight: '6px' }}
                                    />
                                    <span>{isWindows ? t("adminModeLabel") : t("rootModeLabel")}</span>
                                </label>
                            </div>
                            </div>
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-start', gap: '15px' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                    <div style={{ display: 'inline-flex', padding: '3px', borderRadius: '999px', border: '1px solid var(--theme-border-subtle)', background: 'var(--theme-info-bg)' }}>
                                        <button
                                            type="button"
                                            onClick={() => {
                                                const newConfig = new main.AppConfig({ ...config, remote_enabled: false });
                                                setConfig(newConfig);
                                                SaveConfig(newConfig);
                                            }}
                                            style={{
                                                border: 'none',
                                                borderRadius: '999px',
                                                padding: '5px 12px',
                                                background: !config?.remote_enabled ? 'var(--theme-primary)' : 'transparent',
                                                color: !config?.remote_enabled ? 'var(--theme-text-primary)' : 'var(--theme-text-secondary)',
                                                fontSize: '0.78rem',
                                                fontWeight: 700,
                                                cursor: 'pointer'
                                            }}
                                        >
                                            {t("localModeLabel")}
                                        </button>
                                        <button
                                            type="button"
                                            onClick={() => {
                                                if (!isRemoteCapableActiveTool) return;
                                                const newConfig = new main.AppConfig({ ...config, remote_enabled: true });
                                                setConfig(newConfig);
                                                SaveConfig(newConfig);
                                            }}
                                            style={{
                                                border: 'none',
                                                borderRadius: '999px',
                                                padding: '5px 12px',
                                                background: config?.remote_enabled ? 'var(--theme-primary)' : 'transparent',
                                                color: config?.remote_enabled ? 'var(--theme-text-primary)' : 'var(--theme-text-secondary)',
                                                fontSize: '0.78rem',
                                                fontWeight: 700,
                                                cursor: isRemoteCapableActiveTool ? 'pointer' : 'not-allowed',
                                                opacity: isRemoteCapableActiveTool ? 1 : 0.4
                                            }}
                                            title={isRemoteCapableActiveTool ? t("remoteModeDesc") : (lang === 'zh-Hans' ? '当前工具暂不支持远程' : lang === 'zh-Hant' ? '目前工具暫不支援遠端' : 'This tool does not support remote mode')}
                                        >
                                            {t("remoteModeLabel")}
                                        </button>
                                    </div>
                                </div>
                                {config?.remote_enabled && (
                                    <div
                                        style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '4px 10px', background: remoteActivationStatus?.activated ? 'var(--theme-success-bg)' : 'var(--theme-warning-bg)', border: `1px solid ${remoteActivationStatus?.activated ? 'var(--theme-success)' : 'var(--theme-warning)'}`, borderRadius: '999px', cursor: remoteActivationStatus?.activated ? 'default' : 'pointer' }}
                                        onClick={() => {
                                            if (!remoteActivationStatus?.activated) {
                                                openRemoteActivationModal(activeTool);
                                            }
                                        }}
                                        title={remoteActivationStatus?.activated ? t("remoteActivated") : (lang === 'zh-Hans' ? '点击注册' : lang === 'zh-Hant' ? '點擊註冊' : 'Click to register')}
                                    >
                                        <span style={{ fontSize: '0.75rem', color: remoteActivationStatus?.activated ? 'var(--theme-success)' : 'var(--theme-warning)', whiteSpace: 'nowrap' }}>
                                            {remoteActivationStatus?.activated ? t("remoteActivated") : t("remoteRegister")}
                                        </span>
                                    </div>
                                )}
                                <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>
                                    <input
                                        type="checkbox"
                                        checked={resolvedLaunchProject?.python_project || false}
                                        onChange={(e) => updateResolvedLaunchProject((project) => ({ ...project, python_project: e.target.checked }))}
                                        style={{ marginRight: '6px' }}
                                    />
                                    <span>{t("pythonProjectLabel")}</span>
                                </label>
                                {resolvedLaunchProject?.python_project && (
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)' }}>{t("pythonEnvLabel")}:</span>
                                        <select
                                            value={resolvedLaunchProject?.python_env || ""}
                                            onChange={(e) => updateResolvedLaunchProject((project) => ({ ...project, python_env: e.target.value }))}
                                            style={{
                                                padding: '5px 8px',
                                                borderRadius: '4px',
                                                border: '1px solid var(--theme-border)',
                                                backgroundColor: 'var(--theme-surface)',
                                                fontSize: '0.85rem',
                                                color: 'var(--theme-text-primary)',
                                                cursor: 'pointer',
                                                maxWidth: '200px'
                                            }}
                                        >
                                            {pythonEnvironments.map((env: any, index: number) => (
                                                <option key={index} value={env.name}>
                                                    {env.name} {env.type === 'conda' ? '(Conda)' : ''}
                                                </option>
                                            ))}
                                        </select>
                                    </div>
                                )}
                            </div>
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%', gap: '10px', flexWrap: 'nowrap' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: '10px', flex: '1 1 auto', minWidth: 0 }}>
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flex: '1 1 auto', minWidth: 0 }}>
                                        <span style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)', whiteSpace: 'nowrap', lineHeight: 1 }}>{t("project")}:</span>
                                        <input
                                            type="text"
                                            className="form-input"
                                            value={launchProjectKeyword}
                                            onChange={(e) => setLaunchProjectKeyword(e.target.value)}
                                            placeholder={t("projectSearchPlaceholder")}
                                            style={{ height: '28px', width: '90px', minWidth: '70px', fontSize: '0.76rem', padding: '2px 8px', flexShrink: 1 }}
                                            spellCheck={false}
                                            autoComplete="off"
                                        />
                                        <select
                                            value={resolvedLaunchProject?.id || ""}
                                            onChange={(e) => setSelectedProjectForLaunch(e.target.value)}
                                            style={{
                                                padding: '5px 8px',
                                                borderRadius: '4px',
                                                border: '1px solid var(--theme-border)',
                                                backgroundColor: 'var(--theme-surface)',
                                                fontSize: '0.85rem',
                                                color: 'var(--theme-text-primary)',
                                                cursor: 'pointer',
                                                minWidth: '80px',
                                                maxWidth: '160px',
                                                flexShrink: 1
                                            }}
                                        >
                                            {launchProjectSelectOptions.length > 0 ? launchProjectSelectOptions.map((proj: any) => (
                                                <option key={proj.id} value={proj.id}>
                                                    {proj.name}
                                                </option>
                                            )) : (
                                                <option value="" disabled>{t("projectNoResults")}</option>
                                            )}
                                        </select>
                                        <button
                                            onClick={() => switchTool('projects')}
                                            style={{
                                                padding: '0',
                                                height: '20px',
                                                borderRadius: '6px',
                                                border: '1px solid var(--theme-border)',
                                                backgroundColor: 'var(--theme-surface-muted)',
                                                color: 'var(--theme-text-secondary)',
                                                fontSize: '0.85rem',
                                                fontWeight: '500',
                                                cursor: 'pointer',
                                                transition: 'all 0.2s',
                                                whiteSpace: 'nowrap',
                                                textAlign: 'center',
                                                width: '32px',
                                                display: 'flex',
                                                alignItems: 'center',
                                                justifyContent: 'center',
                                                flexShrink: 0
                                            }}
                                            onMouseEnter={(e) => {
                                                e.currentTarget.style.backgroundColor = 'var(--theme-border-subtle)';
                                                e.currentTarget.style.color = 'var(--theme-text-primary)';
                                            }}
                                            onMouseLeave={(e) => {
                                                e.currentTarget.style.backgroundColor = 'var(--theme-surface-muted)';
                                                e.currentTarget.style.color = 'var(--theme-text-secondary)';
                                            }}
                                        >
                                            ...
                                        </button>
                                    </div>
                                </div>
                                {/* Handoff: local → remote icon button */}
                                {!config?.remote_enabled && isRemoteCapableActiveTool && (
                                    <button
                                        type="button"
                                        title={lang === 'zh-Hans' ? '转为远程' : lang === 'zh-Hant' ? '轉為遠端' : 'Switch to Remote'}
                                        style={{
                                            width: '36px',
                                            height: '36px',
                                            borderRadius: '50%',
                                            border: '1px solid var(--theme-primary-soft)',
                                            background: 'linear-gradient(135deg, var(--theme-info-bg), var(--theme-surface))',
                                            color: 'var(--theme-primary-strong)',
                                            fontSize: '1rem',
                                            display: 'flex',
                                            alignItems: 'center',
                                            justifyContent: 'center',
                                            cursor: 'pointer',
                                            flexShrink: 0,
                                            transition: 'all 0.2s',
                                            padding: 0,
                                        }}
                                        onMouseEnter={(e) => {
                                            e.currentTarget.style.background = 'linear-gradient(135deg, var(--theme-primary-soft), var(--theme-info-bg))';
                                            e.currentTarget.style.borderColor = 'var(--theme-primary)';
                                        }}
                                        onMouseLeave={(e) => {
                                            e.currentTarget.style.background = 'linear-gradient(135deg, var(--theme-info-bg), var(--theme-surface))';
                                            e.currentTarget.style.borderColor = 'var(--theme-primary-soft)';
                                        }}
                                        onClick={async () => {
                                            if (!config?.remote_hub_url?.trim() || !remoteActivationStatus?.activated || !config?.remote_email?.trim()) {
                                                openRemoteActivationModal(activeTool);
                                                return;
                                            }
                                            setStatus(lang === 'zh-Hans' ? '正在转为远程...' : lang === 'zh-Hant' ? '正在轉為遠端...' : 'Switching to remote...');
                                            setLaunchingTool(activeTool);
                                            try {
                                                const newConfig = new main.AppConfig({ ...config, remote_enabled: true });
                                                setConfig(newConfig);
                                                await SaveConfig(newConfig);
                                                await quickStartRemoteSession(activeTool as any, "handoff");
                                                setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                            } catch (err) {
                                                setStatus("Error: " + err);
                                                setLaunchingTool("");
                                            }
                                        }}
                                    >
                                        ☁
                                    </button>
                                )}
                                <button
                                    className="btn-launch"
                                    style={{ padding: '8px 20px', textAlign: 'center', '--wails-draggable': 'no-drag', pointerEvents: 'auto', flexShrink: 0 } as any}
                                    disabled={onDemandInstallingTool === activeTool || backgroundInstallingTool === activeTool || launchingTool === activeTool}
                                    onClick={async () => {
                                        console.log("Launch button clicked. activeTool:", activeTool);
                                        if (config?.remote_enabled && hasActiveRemoteSessionForTool && activeRemoteSessionForTool?.id) {
                                            setLaunchingTool(activeTool);
                                            await killRemoteSession(activeRemoteSessionForTool.id);
                                            setStatus(lang === 'zh-Hans' ? '远程已停止' : lang === 'zh-Hant' ? '遠端已停止' : 'Remote stopped');
                                            setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                            return;
                                        }
                                        const selectedProj = resolvedLaunchProject;
                                        if (selectedProj && selectedProj.path && selectedProj.path.trim() !== "") {
                                            if (config?.remote_enabled) {
                                                if (remoteToolMetadata.length > 0 && !isRemoteCapableActiveTool) {
                                                    setStatus(lang === 'zh-Hans' ? '当前工具暂不支持远程启动' : lang === 'zh-Hant' ? '目前工具暫不支援遠端啟動' : 'This tool does not support remote launch');
                                                    return;
                                                }
                                                if (!config?.remote_hub_url?.trim() || !remoteActivationStatus?.activated || !config?.remote_email?.trim()) {
                                                    openRemoteActivationModal(activeTool);
                                                    return;
                                                }
                                                setStatus(lang === 'zh-Hans' ? '正在远程启动...' : lang === 'zh-Hant' ? '正在遠端啟動...' : 'Starting remotely...');
                                                setLaunchingTool(activeTool);
                                                try {
                                                    await quickStartRemoteSession(activeTool as any);
                                                    setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                                } catch (err) {
                                                    setStatus("Error: " + err);
                                                    setLaunchingTool("");
                                                }
                                                return;
                                            }
                                            // Check if tool is installed
                                            const toolStatus = toolStatuses?.find((s: any) => s.name === activeTool);
                                            if (toolStatus && !toolStatus.installed) {
                                                // Check if tool is being installed in background
                                                const isBeingInstalled = await IsToolBeingInstalled(activeTool);
                                                if (isBeingInstalled) {
                                                    // Tool is being installed in background, just wait
                                                    setStatus(lang === 'zh-Hans' ? `${activeTool} 正在后台安装中，请稍候...` : `${activeTool} is being installed in background, please wait...`);
                                                    setOnDemandInstallingTool(activeTool);
                                                    try {
                                                        await InstallToolOnDemand(activeTool);
                                                        // Refresh tool statuses
                                                        const updatedStatuses = await CheckToolsStatus();
                                                        setToolStatuses(updatedStatuses);
                                                        setStatus(lang === 'zh-Hans' ? `${activeTool} 安装完成` : `${activeTool} installed`);
                                                        setOnDemandInstallingTool("");
                                                        // Auto launch
                                                        setTimeout(async () => {
                                                            setStatus(lang === 'zh-Hans' ? "正在启动..." : "Launching...");
                                                            setLaunchingTool(activeTool);
                                                            try {
                                                                await LaunchTool(activeTool, selectedProj.yolo_mode, selectedProj.admin_mode || false, selectedProj.python_project || false, selectedProj.python_env || "", selectedProj.path || "", selectedProj.use_proxy || false);
                                                                setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                                            } catch (err) {
                                                                setStatus("Error: " + err);
                                                                setLaunchingTool("");
                                                            }
                                                        }, 500);
                                                    } catch (err) {
                                                        setStatus("Error: " + err);
                                                        setOnDemandInstallingTool("");
                                                    }
                                                    return;
                                                }
                                                
                                                // Tool not installed and not being installed, show install dialog
                                                setOnDemandInstallingTool(activeTool);
                                                setToolRepairStatus({show: true, toolName: activeTool, status: 'installing', message: ''});
                                                try {
                                                    await InstallToolOnDemand(activeTool);
                                                    // Refresh tool statuses
                                                    const updatedStatuses = await CheckToolsStatus();
                                                    setToolStatuses(updatedStatuses);
                                                    setToolRepairStatus({show: true, toolName: activeTool, status: 'success', message: ''});
                                                    
                                                    // Auto launch after successful installation
                                                    setTimeout(async () => {
                                                        setToolRepairStatus(prev => ({...prev, show: false}));
                                                        setOnDemandInstallingTool("");
                                                        // Launch the tool
                                                        setStatus(lang === 'zh-Hans' ? "正在启动..." : "Launching...");
                                                        setLaunchingTool(activeTool);
                                                        try {
                                                            await LaunchTool(activeTool, selectedProj.yolo_mode, selectedProj.admin_mode || false, selectedProj.python_project || false, selectedProj.python_env || "", selectedProj.path || "", selectedProj.use_proxy || false);
                                                            console.log("LaunchTool call returned successfully after install");
                                                            setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                                        } catch (err) {
                                                            console.error("LaunchTool call failed after install:", err);
                                                            setStatus("Error: " + err);
                                                            setLaunchingTool("");
                                                        }
                                                    }, 1500);
                                                    return;
                                                } catch (err) {
                                                    console.error("Failed to install tool on demand:", err);
                                                    setToolRepairStatus({show: true, toolName: activeTool, status: 'failed', message: String(err)});
                                                    setOnDemandInstallingTool("");
                                                    return;
                                                }
                                            }
                                            
                                            console.log("Launching tool with project:", selectedProj.name, "path:", selectedProj.path);
                                            setStatus(lang === 'zh-Hans' ? "正在启动..." : "Launching...");
                                            setLaunchingTool(activeTool);
                                            LaunchTool(activeTool, selectedProj.yolo_mode, selectedProj.admin_mode || false, selectedProj.python_project || false, selectedProj.python_env || "", selectedProj.path || "", selectedProj.use_proxy || false)
                                                .then(() => {
                                                    console.log("LaunchTool call returned successfully");
                                                    setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                                })
                                                .catch(err => {
                                                    console.error("LaunchTool call failed:", err);
                                                    setStatus("Error: " + err);
                                                    setLaunchingTool("");
                                                });
                                            // Update current project if different
                                            if (selectedProj.id !== config?.current_project) {
                                                handleProjectSwitch(selectedProj.id);
                                            }
                                        } else {
                                            console.error("No project found for launch ID:", selectedProjectForLaunch);
                                            setStatus(t("projectDirError"));
                                        }
                                    }}
                                >
                                    <span style={{ marginRight: '6px' }}>{config?.remote_enabled ? '☁' : '➤'}</span>
                                    {config?.remote_enabled
                                        ? (hasActiveRemoteSessionForTool ? t("remoteStopTool") : t("remoteStartTool"))
                                        : t("launch")}
                                </button>
                            </div>
                        </div>
                    </div>
                )}

                <div className="status-message" style={{ padding: '0 20px 4px 20px', minHeight: '20px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <span key={status} style={{ color: (status.includes("Error") || status.includes("!") || status.includes("first")) ? 'var(--theme-danger)' : 'var(--theme-success)' }}>
                        {status}
                    </span>
                    <div style={{ display: 'flex', alignItems: 'center', gap: '10px' }}>
                        {(() => {
                            const imConnected = qqBotStatus === 'connected' || telegramStatus === 'connected' || weixinStatus === 'connected';
                            const anyImConfigured = !!(config as any)?.qqbot_enabled || !!(config as any)?.telegram_enabled || !!(config as any)?.weixin_enabled;
                            const showImWarning = anyImConfigured && !imConnected;
                            if ((!maclawLLMOnline || !remoteActivationStatus?.activated || !agentNetRunning || showImWarning) && !(navTab === 'settings' && (settingsTab === 'llm' || settingsTab === 'serviceRedeem'))) {
                                const isImIssue = maclawLLMOnline && !!remoteActivationStatus?.activated && agentNetRunning && showImWarning;
                                return (
                            <span
                                style={{ fontSize: '0.72rem', color: 'var(--theme-warning)', cursor: 'pointer', display: 'flex', alignItems: 'center', gap: '3px' }}
                                onClick={() => { if (isImIssue) { setNavTab('settings'); setSettingsTab('im'); } else { setNavTab('settings'); setSettingsTab('llm'); } }}
                                title={localizeText('Click to configure', '点击配置', '點擊配置')}
                            >
                                <img src={(() => {
                                    if (!maclawLLMOnline && !remoteActivationStatus?.activated && !agentNetRunning) return lobsterOffline;
                                    return lobsterHalf;
                                })()} alt="" style={{ width: '14px', height: '14px' }} />
                                {!maclawLLMOnline
                                    ? (maclawLLMConfigured
                                        ? localizeText('LLM unreachable, remote commands unavailable', 'LLM 无法连接，无法响应远程命令', 'LLM 無法連接，無法響應遠程命令')
                                        : localizeText('LLM not configured, remote commands unavailable', 'MaClaw 未配置 LLM，无法响应远程命令', 'MaClaw 未配置 LLM，無法響應遠程命令'))
                                    : !remoteActivationStatus?.activated
                                        ? localizeText('Mobile not registered', '移动端未注册', '移動端未註冊')
                                        : !agentNetRunning
                                            ? localizeText('AgentNet not connected', '智网未连接', '智網未連接')
                                            : localizeText('IM not connected', 'IM 未连接', 'IM 未連接')}
                            </span>
                                );
                            }
                            return null;
                        })()}
                        {backgroundInstallStatus && (
                        <span style={{ 
                            fontSize: '0.75rem', 
                            color: backgroundInstallStatus.startsWith('✓') ? 'var(--theme-success)' : 'var(--theme-text-muted)',
                            display: 'flex',
                            alignItems: 'center',
                            gap: '4px'
                        }}>
                            {!backgroundInstallStatus.startsWith('✓') && (
                                <span style={{ 
                                    display: 'inline-block', 
                                    width: '10px', 
                                    height: '10px', 
                                    border: '2px solid var(--theme-text-muted)',
                                    borderTopColor: 'transparent',
                                    borderRadius: '50%',
                                    animation: 'spin 1s linear infinite'
                                }}></span>
                            )}
                            {backgroundInstallStatus}
                        </span>
                    )}
                    </div>
                </div>
            </>)}
            </div>

            {/* Modals */}
            {showRemoteActivationModal && (
                <div className="modal-overlay" onClick={() => { setShowRemoteActivationModal(false); setPendingRemoteLaunchTool(""); setRemoteCenterHubs([]); }}>
                    <div className="modal-content" style={{ width: '640px', maxWidth: '94vw', maxHeight: '82vh', textAlign: 'left', display: 'flex', flexDirection: 'column' }} onClick={(e) => e.stopPropagation()}>
                        <div className="modal-header">
                            <h3>{t("remoteActivationDialogTitle")}</h3>
                            <button className="btn-close" onClick={() => { setShowRemoteActivationModal(false); setPendingRemoteLaunchTool(""); setRemoteCenterHubs([]); }}>&times;</button>
                        </div>
                        <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '10px', overflowY: 'auto', paddingBottom: '10px' }}>
                            <div style={{ fontSize: '0.82rem', color: 'var(--theme-text-secondary)', lineHeight: 1.5 }}>
                                {t("remoteActivationDialogDesc")}
                            </div>
                            <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr)', gap: '10px' }}>
                                <div>
                                    <label className="form-label">{t("remoteHubCenterUrl")}</label>
                                    <input
                                        className="form-input"
                                        value={remoteActivationDraft.hubcenter_url}
                                        onChange={(e) => setRemoteActivationDraft((prev) => ({ ...prev, hubcenter_url: e.target.value }))}
                                        placeholder="http://127.0.0.1:9388"
                                        spellCheck={false}
                                    />
                                </div>
                                <div>
                                    <label className="form-label">{t("remoteEmail")}</label>
                                    <input
                                        className="form-input"
                                        value={remoteActivationDraft.email}
                                        onChange={(e) => setRemoteActivationDraft((prev) => ({ ...prev, email: e.target.value }))}
                                        placeholder="name@example.com"
                                        spellCheck={false}
                                    />
                                </div>
                            </div>
                            <div style={{ display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr)', gap: '10px', alignItems: 'end' }}>
                                <div>
                                    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px', marginBottom: '6px' }}>
                                        <label className="form-label" style={{ marginBottom: 0 }}>{t("remoteSelectRegisteredHub")}</label>
                                        <button
                                            className="btn-secondary"
                                            onClick={() => loadRemoteHubsFromCenter()}
                                            disabled={loadingRemoteCenterHubs}
                                            style={{ minWidth: '112px', height: '30px', padding: '4px 10px', fontSize: '0.78rem', flexShrink: 0 }}
                                        >
                                            {loadingRemoteCenterHubs ? t("remoteLoadingRegisteredHubs") : t("remoteLoadRegisteredHubs")}
                                        </button>
                                    </div>
                                    <select
                                        className="form-select"
                                        value={remoteCenterHubs.some((hub) => hub.base_url === remoteActivationDraft.hub_url.trim()) ? remoteActivationDraft.hub_url.trim() : ""}
                                        onChange={(e) => setRemoteActivationDraft((prev) => ({ ...prev, hub_url: e.target.value }))}
                                    >
                                        <option value="">
                                            {remoteCenterHubs.length > 0 ? t("remoteSelectRegisteredHub") : t("remoteNoRegisteredHubs")}
                                        </option>
                                        {remoteCenterHubs.map((hub) => (
                                            <option key={`${hub.hub_id}-${hub.base_url}`} value={hub.base_url}>
                                                {hub.name ? `${hub.name} (${hub.base_url})` : hub.base_url}
                                            </option>
                                        ))}
                                    </select>
                                </div>
                                <div>
                                    <label className="form-label">{t("remoteHubUrl")}</label>
                                    <input
                                        className="form-input"
                                        value={remoteActivationDraft.hub_url}
                                        onChange={(e) => setRemoteActivationDraft((prev) => ({ ...prev, hub_url: e.target.value }))}
                                        placeholder="https://hub.example.com"
                                        spellCheck={false}
                                    />
                                </div>
                            </div>
                            <div style={{ fontSize: '0.79rem', color: 'var(--theme-text-muted)', lineHeight: 1.5 }}>
                                {t("remoteHubManualOrSelect")}
                            </div>
                        </div>
                        <div className="modal-footer" style={{ marginTop: '0', flexShrink: 0 }}>
                            <button className="btn-secondary" onClick={() => { setShowRemoteActivationModal(false); setPendingRemoteLaunchTool(""); setRemoteCenterHubs([]); }}>{t("cancel")}</button>
                            <button className="btn-primary" onClick={activateRemoteFromDialog} disabled={remoteBusy === 'activate'}>
                                {remoteBusy === 'activate' ? t("remoteActivating") : t("remoteActivateAndLaunch")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {showInstallLog && (
                <div className="modal-overlay" onClick={() => setShowInstallLog(false)}>
                    <div className="modal-content" style={{ width: '600px', maxWidth: '90vw' }} onClick={e => e.stopPropagation()}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '15px' }}>
                            <h3 style={{ margin: 0, color: 'var(--theme-primary)' }}>{t("installLogTitle")}</h3>
                            <button className="modal-close" onClick={() => setShowInstallLog(false)}>&times;</button>
                        </div>
                        <div
                            className="elegant-scrollbar"
                            style={{
                                backgroundColor: 'var(--theme-surface-muted)',
                                color: 'var(--theme-text-primary)',
                                padding: '15px',
                                borderRadius: '8px',
                                height: '250px',
                                overflowY: 'auto',
                                fontFamily: 'monospace',
                                fontSize: '0.85rem',
                                whiteSpace: 'pre-wrap',
                                textAlign: 'left',
                                marginBottom: '15px'
                            }}>
                            {envLogs.length === 0 ? (
                                <div style={{ color: 'var(--theme-text-muted)', fontStyle: 'italic' }}>
                                    {t("initializing")}
                                </div>
                            ) : (
                                envLogs.map((log, index) => {
                                    const isError = /error|failed/i.test(log);
                                    return (
                                        <div key={index} style={{
                                            color: isError ? 'var(--theme-danger)' : 'inherit',
                                            marginBottom: '4px'
                                        }}>
                                            {isError ? `** ${log}` : log}
                                        </div>
                                    );
                                })
                            )}
                        </div>
                        <div style={{
                            display: 'flex',
                            justifyContent: 'flex-end',
                            gap: '10px'
                        }}>
                            <button
                                className="btn-link"
                                onClick={() => {
                                    const logText = envLogs.join('\n');
                                    navigator.clipboard.writeText(logText).then(() => {
                                        showToastMessage(t("logsCopied"));
                                    });
                                }}
                            >
                                {t("copyLog")}
                            </button>
                            <button
                                className="btn-link"
                                onClick={async () => {
                                    console.log('Send log button clicked');
                                    const hasError = envLogs.some(log => /error|failed/i.test(log));

                                    if (hasError) {
                                        // 有错误，直接发送
                                        await performSendLog();
                                    } else {
                                        // 没有错误，询问用户
                                        setConfirmDialog({
                                            show: true,
                                            title: t("confirmSendLog"),
                                            message: t("confirmSendLogMessage"),
                                            onConfirm: async () => {
                                                setConfirmDialog({ ...confirmDialog, show: false });
                                                await performSendLog();
                                            }
                                        });
                                    }
                                }}
                            >
                                {t("sendLog")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Tool Repair Progress Dialog */}
            {toolRepairStatus.show && (
                <div className="modal-overlay" style={{ backgroundColor: 'color-mix(in srgb, var(--theme-page-bg) 30%, transparent)' }}>
                    <div style={{
                        backgroundColor: 'var(--theme-surface)',
                        color: 'var(--theme-text-primary)',
                        borderRadius: '16px',
                        padding: '20px 28px',
                        textAlign: 'center',
                        boxShadow: '0 8px 32px rgba(0, 0, 0, 0.12)',
                        minWidth: '220px',
                        maxWidth: '280px',
                        border: '1px solid var(--theme-border)'
                    }}>
                        {toolRepairStatus.status === 'installing' && (
                            <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                                <div style={{
                                    width: '24px',
                                    height: '24px',
                                    border: '3px solid var(--theme-border)',
                                    borderTop: '3px solid var(--theme-primary)',
                                    borderRadius: '50%',
                                    animation: 'spin 0.8s linear infinite',
                                    flexShrink: 0
                                }}></div>
                                <span style={{ color: 'var(--theme-text-secondary)', fontSize: '0.9rem', fontWeight: 500 }}>
                                    {t("toolRepairInstalling").replace("{tool}", toolRepairStatus.toolName)}
                                </span>
                            </div>
                        )}
                        {toolRepairStatus.status === 'success' && (
                            <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                                <div style={{
                                    width: '28px',
                                    height: '28px',
                                    backgroundColor: 'var(--theme-success-bg)',
                                    borderRadius: '50%',
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    flexShrink: 0
                                }}>
                                    <span style={{ color: 'var(--theme-success)', fontSize: '16px' }}>✓</span>
                                </div>
                                <span style={{ color: 'var(--theme-success)', fontSize: '0.9rem', fontWeight: 500 }}>
                                    {t("toolRepairSuccess").replace("{tool}", toolRepairStatus.toolName)}
                                </span>
                            </div>
                        )}
                        {toolRepairStatus.status === 'failed' && (
                            <div>
                                <div style={{ display: 'flex', alignItems: 'center', gap: '12px', marginBottom: '12px' }}>
                                    <div style={{
                                        width: '28px',
                                        height: '28px',
                                        backgroundColor: 'var(--theme-danger-bg)',
                                        borderRadius: '50%',
                                        display: 'flex',
                                        alignItems: 'center',
                                        justifyContent: 'center',
                                        flexShrink: 0
                                    }}>
                                        <span style={{ color: 'var(--theme-danger)', fontSize: '14px' }}>✕</span>
                                    </div>
                                    <span style={{ color: 'var(--theme-danger)', fontSize: '0.9rem', fontWeight: 500 }}>
                                        {t("toolRepairFailed").replace("{tool}", toolRepairStatus.toolName)}
                                    </span>
                                </div>
                                <p style={{ color: 'var(--theme-text-muted)', fontSize: '0.8rem', margin: '0 0 12px 0', wordBreak: 'break-word', textAlign: 'left' }}>
                                    {toolRepairStatus.message}
                                </p>
                                <button
                                    style={{
                                        backgroundColor: 'var(--theme-surface-muted)',
                                        border: '1px solid var(--theme-border)',
                                        borderRadius: '8px',
                                        padding: '6px 16px',
                                        fontSize: '0.85rem',
                                        color: 'var(--theme-text-secondary)',
                                        cursor: 'pointer',
                                        fontWeight: 500
                                    }}
                                    onClick={() => setToolRepairStatus(prev => ({...prev, show: false}))}
                                >
                                    {t("close")}
                                </button>
                            </div>
                        )}
                    </div>
                </div>
            )}

            {showUpdateModal && updateResult && (
                <div className="modal-overlay">
                    <div className="modal-content" style={{ width: '400px', textAlign: 'left' }}>
                        <h3>{t("foundNewVersion")}</h3>
                        {updateResult.has_update ? (
                            <>
                                <div style={{ backgroundColor: 'var(--theme-info-bg)', padding: '12px', borderRadius: '6px', marginBottom: '15px', border: '1px solid var(--theme-primary-soft)' }}>
                                    <div style={{ fontSize: '0.85rem', color: 'var(--theme-text-muted)', marginBottom: '8px' }}>{t("currentVersion")}</div>
                                    <div style={{ fontSize: '1rem', fontWeight: '600', color: 'var(--theme-primary)', marginBottom: '12px' }}>v{appVersion}</div>
                                    <div style={{ fontSize: '0.85rem', color: 'var(--theme-text-muted)', marginBottom: '8px' }}>{t("latestVersion")}</div>
                                    <div style={{ fontSize: '1rem', fontWeight: '600', color: 'var(--theme-success)' }}>{updateResult.latest_version}</div>
                                </div>

                                <div style={{ marginTop: '15px' }}>
                                    {updateResult.download_unavailable ? (
                                        <div>
                                            <p style={{ margin: '10px 0', fontSize: '0.9rem', color: 'var(--theme-warning, #e6a23c)' }}>⚠️ {t("packageUnavailable")}</p>
                                            {updateResult.release_url && (
                                                <button className="btn-primary" style={{ width: '100%' }} onClick={() => BrowserOpenURL(updateResult.release_url)}>
                                                    {t("visitReleasePage")}
                                                </button>
                                            )}
                                        </div>
                                    ) : isDownloading ? (
                                        <div style={{ width: '100%' }}>
                                            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '8px', fontSize: '0.9rem' }}>
                                                <span>{t("downloading")}</span>
                                                <span>{downloadProgress}%</span>
                                            </div>
                                            <div style={{ width: '100%', height: '10px', backgroundColor: 'var(--theme-surface-muted)', borderRadius: '5px', overflow: 'hidden' }}>
                                                <div style={{ width: `${downloadProgress}%`, height: '100%', backgroundColor: 'var(--theme-primary)', transition: 'width 0.2s ease' }}></div>
                                            </div>
                                            <button
                                                className="btn-link"
                                                style={{ marginTop: '10px', color: 'var(--theme-danger)' }}
                                                onClick={handleCancelDownload}
                                            >
                                                {t("cancelDownload")}
                                            </button>
                                        </div>
                                    ) : installerPath ? (
                                        <div style={{ textAlign: 'center', padding: '10px' }}>
                                            <p style={{ color: 'var(--theme-success)', fontWeight: 'bold', marginBottom: '15px' }}>{t("downloadComplete")}</p>
                                            <button className="btn-primary" style={{ width: '100%' }} onClick={handleInstall}>
                                                {t("installNow")}
                                            </button>
                                        </div>
                                    ) : (
                                        <div>
                                            {downloadError && (
                                                <div style={{ marginBottom: '10px' }}>
                                                    <p style={{ color: 'var(--theme-danger)', fontSize: '0.85rem', marginBottom: '5px' }}>{t("downloadError").replace("{error}", downloadError)}</p>
                                                    <button className="btn-primary" style={{ width: '100%', backgroundColor: 'var(--theme-danger)' }} onClick={handleDownload}>
                                                        {t("retry")}
                                                    </button>
                                                </div>
                                            )}
                                            {!downloadError && (
                                                <>
                                                    <p style={{ margin: '10px 0', fontSize: '0.9rem', color: 'var(--theme-text-primary)' }}>{t("foundNewVersionMsg")}</p>
                                                    <button className="btn-primary" style={{ width: '100%' }} onClick={handleDownload}>
                                                        {t("downloadAndUpdate")}
                                                    </button>
                                                </>
                                            )}
                                        </div>
                                    )}
                                </div>
                            </>
                        ) : (
                            <div style={{ backgroundColor: 'var(--theme-info-bg)', padding: '12px', borderRadius: '6px', border: '1px solid var(--theme-primary-soft)' }}>
                                <div style={{ fontSize: '0.85rem', color: 'var(--theme-text-muted)', marginBottom: '8px' }}>{t("currentVersion")}</div>
                                <div style={{ fontSize: '1rem', fontWeight: '600', color: 'var(--theme-primary)', marginBottom: '12px' }}>v{appVersion}</div>
                                <div style={{ fontSize: '0.85rem', color: 'var(--theme-text-muted)', marginBottom: '8px' }}>{t("latestVersion")}</div>
                                <div style={{ fontSize: '1rem', fontWeight: '600', color: 'var(--theme-success)', marginBottom: '12px' }}>{updateResult.latest_version}</div>
                                <p style={{ margin: '0', fontSize: '0.9rem', color: 'var(--theme-success)', fontWeight: '500' }}>✓ {t("isLatestVersion")}</p>
                            </div>
                        )}
                        <div style={{ display: 'flex', gap: '10px', justifyContent: 'flex-end', marginTop: '20px' }}>
                            <button className="btn-primary" disabled={isDownloading} onClick={() => {
                                setShowUpdateModal(false);
                                // After closing update modal, show welcome page only if this was a startup check
                                if (isStartupUpdateCheck && config && !config.hide_startup_popup) {
                                    setShowStartupPopup(true);
                                }
                                // Reset the flag
                                setIsStartupUpdateCheck(false);
                                // Clear error if any
                                setDownloadError("");
                            }}>{t("close")}</button>
                        </div>
                    </div>
                </div>
            )}

            {showModelSettings && config && (
                <div className="modal-overlay">
                    <div className="modal-content" style={{ width: '529px', textAlign: 'left' }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
                            <h3 style={{ margin: 0, color: 'var(--theme-primary)' }}>{t("modelSettings")}</h3>
                            <button className="modal-close" onClick={() => setShowModelSettings(false)}>&times;</button>
                        </div>

                        <div style={{ marginBottom: '16px' }}>
                            {(() => {
                                const allModels = (config as any)[activeTool].models;
                                // Filter: show only non-Original models
                                const customModels = allModels.filter((m: any) => m.is_custom);
                                const nonCustomModels = allModels.filter((m: any) => !m.is_custom && m.model_name !== "Original");

                                // Always show all custom models (user can add/remove them)
                                const configurableModels = [...nonCustomModels, ...customModels];
                                const showArrows = configurableModels.length >= 5;

                                return (
                                    <div className="tabs" style={{ alignItems: 'center', minHeight: '40px' }}>
                                        {showArrows && (
                                            <div style={{ width: '30px', display: 'flex', justifyContent: 'center', flexShrink: 0 }}>
                                                {tabStartIndex > 0 && (
                                                    <button
                                                        onClick={() => setTabStartIndex(Math.max(0, tabStartIndex - 1))}
                                                        style={{
                                                            border: 'none', background: 'transparent', cursor: 'pointer',
                                                            padding: '6px 4px', color: 'var(--theme-text-muted)', fontSize: '1rem'
                                                        }}
                                                    >
                                                        ◀
                                                    </button>
                                                )}
                                            </div>
                                        )}

                                        <div style={{ flex: 1, display: 'flex', gap: '2px', overflow: 'hidden' }}>
                                            {(showArrows ? configurableModels.slice(tabStartIndex, tabStartIndex + 4) : configurableModels).map((model: any, index: number) => {
                                                const globalIndex = allModels.findIndex((m: any) => m.model_name === model.model_name);
                                                const name = model.model_name.toLowerCase();
                                                let badge = null;

                                                if (model.has_subscription) {
                                                    badge = { bg: 'var(--theme-danger)', label: t("subscription") };
                                                } else if (name.includes("glm") || name.includes("kimi") || name.includes("doubao") || name.includes("minimax")) {
                                                    badge = { bg: 'var(--theme-danger)', label: t("monthly") };
                                                } else if (name.includes("deepseek")) {
                                                    badge = { bg: 'var(--theme-warning)', label: t("premium") };
                                                } else if (name.includes("xiaomi")) {
                                                    badge = { bg: 'var(--theme-warning)', label: t("bigSpender") };
                                                } else if (model.is_custom) {
                                                    badge = { bg: 'var(--theme-text-muted)', label: t("customized") };
                                                } else if (["aicodemirror", "aigocode", "noin.ai", "gaccode", "chatfire", "coderelay"].some(p => name.includes(p))) {
                                                    badge = { bg: 'var(--theme-success)', label: t("forward") };
                                                }

                                                return (
                                                    <button
                                                        key={globalIndex}
                                                        className={`tab-button ${activeTab === globalIndex ? 'active' : ''}`}
                                                        onClick={() => setActiveTab(globalIndex)}
                                                        style={{ overflow: 'hidden', textOverflow: 'ellipsis', flexShrink: 0, position: 'relative' }}
                                                    >
                                                        {getModelDisplayName(model.model_name, lang)}
                                                        {badge && (
                                                            <span style={{
                                                                position: 'absolute',
                                                                top: '-6px',
                                                                right: '-6px',
                                                                backgroundColor: badge.bg,
                                                                color: 'var(--theme-text-primary)',
                                                                padding: '2px 6px',
                                                                borderRadius: '4px',
                                                                fontSize: '0.6rem',
                                                                fontWeight: 'bold',
                                                                boxShadow: '0 2px 4px rgba(0,0,0,0.15)',
                                                                whiteSpace: 'nowrap'
                                                            }}>
                                                                {badge.label}
                                                            </span>
                                                        )}
                                                    </button>
                                                );
                                            })}
                                        </div>

                                        {showArrows && (
                                            <div style={{ width: '30px', display: 'flex', justifyContent: 'center', flexShrink: 0 }}>
                                                {tabStartIndex + 4 < configurableModels.length && (
                                                    <button
                                                        onClick={() => setTabStartIndex(Math.min(configurableModels.length - 4, tabStartIndex + 1))}
                                                        style={{
                                                            border: 'none', background: 'transparent', cursor: 'pointer',
                                                            padding: '6px 4px', color: 'var(--theme-text-muted)', fontSize: '1rem'
                                                        }}
                                                    >
                                                        ▶
                                                    </button>
                                                )}
                                            </div>
                                        )}
                                    </div>
                                );
                            })()}
                        </div>

                        <div style={{ display: 'flex', gap: '16px' }}>
                            {(config as any)[activeTool].models[activeTab].is_custom && (
                                <div className="form-group" style={{ flex: 1 }}>
                                    <label className="form-label">{t("providerName")}</label>
                                    <input
                                        type="text"
                                        className="form-input"
                                        data-field="model-name"
                                        value={(config as any)[activeTool].models[activeTab].model_name}
                                        onChange={(e) => handleModelNameChange(e.target.value)}
                                        placeholder={t("customProviderPlaceholder")}
                                        spellCheck={false}
                                        autoComplete="off"
                                    />
                                </div>
                            )}

                            {(config as any)[activeTool].models[activeTab].model_name !== "Original" && (
                                <div className="form-group" style={{ flex: 1 }}>
                                    <label className="form-label">
                                        {t("modelName")}
                                        {activeTool === 'codebuddy' && <span style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)', marginLeft: '5px' }}>(Supports multiple, separated by comma)</span>}
                                    </label>
                                    <div style={{ position: 'relative' }}>
                                        <div style={{ display: 'flex', gap: '4px', alignItems: 'center' }}>
                                            <input
                                                type="text"
                                                className="form-input"
                                                data-field="model-id"
                                                style={{ flex: 1 }}
                                                value={(config as any)[activeTool].models[activeTab].model_id}
                                                onChange={(e) => handleModelIdChange(e.target.value)}
                                                placeholder={activeTool === 'codebuddy' ? "e.g. gpt-4,gpt-3.5-turbo" : (getDefaultModelId(activeTool, (config as any)[activeTool].models[activeTab].model_name) || "e.g. gpt-4")}
                                                spellCheck={false}
                                                autoComplete="off"
                                            />
                                            {(() => {
                                                const providerName = (config as any)[activeTool].models[activeTab].model_name;
                                                const models = (activeTool === 'claude' || (providerName !== '阿里云' && providerName !== 'aliyun')) ? recommendedModels[providerName] : undefined;
                                                if (!models || models.length === 0) return null;
                                                return (
                                                    <button
                                                        style={{ border: '1px solid var(--theme-border)', background: 'var(--theme-surface)', color: 'var(--theme-text-secondary)', borderRadius: '6px', padding: '6px 8px', cursor: 'pointer', fontSize: '0.8rem', whiteSpace: 'nowrap', flexShrink: 0 }}
                                                        onClick={() => setShowModelRecommend(!showModelRecommend)}
                                                        title="推荐模型"
                                                    >...</button>
                                                );
                                            })()}
                                        </div>
                                        {showModelRecommend && (() => {
                                            const providerName = (config as any)[activeTool].models[activeTab].model_name;
                                            const models = (activeTool === 'claude' || (providerName !== '阿里云' && providerName !== 'aliyun')) ? recommendedModels[providerName] : undefined;
                                            if (!models || models.length === 0) return null;
                                            return (
                                                <div style={{ position: 'absolute', top: '100%', right: 0, zIndex: 100, marginTop: '4px', background: 'var(--theme-surface)', border: '1px solid var(--theme-border)', borderRadius: '8px', boxShadow: '0 4px 12px rgba(0,0,0,0.15)', minWidth: '200px', maxHeight: '240px', overflowY: 'auto', padding: '4px 0' }}>
                                                    {models.map((m: any, i: number) => (
                                                        <div
                                                            key={i}
                                                            style={{ padding: '6px 12px', cursor: 'pointer', fontSize: '0.82rem', color: 'var(--theme-text-primary)', display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '8px' }}
                                                            onMouseEnter={(e) => (e.currentTarget.style.background = 'var(--theme-surface-muted)')}
                                                            onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
                                                            onClick={() => { handleModelIdChange(m.id); setShowModelRecommend(false); }}
                                                        >
                                                            <span>{m.id}</span>
                                                            {m.note && <span style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)' }}>{m.note}</span>}
                                                        </div>
                                                    ))}
                                                </div>
                                            );
                                        })()}
                                    </div>
                                </div>
                            )}
                        
                            {activeTool === "codex" && (
                                <div className="form-group" style={{ flex: 0, minWidth: '140px' }}>
                                    <label className="form-label">Wire API</label>
                                    <input
                                        type="text"
                                        className="form-input"
                                        data-field="wire-api"
                                        value={(config as any)[activeTool].models[activeTab].wire_api || ""}
                                        onChange={(e) => handleWireApiChange(e.target.value)}
                                        placeholder="chat"
                                        spellCheck={false}
                                        autoComplete="off"
                                    />
                                </div>
                            )}</div>

                        {(config as any)[activeTool].models[activeTab].model_name !== "Original" && (
                            <>

                                <div className="form-group">
                                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
                                        <label className="form-label" style={{ margin: 0 }}>{t("apiKey")}</label>
                                        {!(config as any)[activeTool].models[activeTab].is_custom && (
                                                <button
                                                    className="btn-link"
                                                    style={{ fontSize: '0.75rem', padding: '2px 8px' }}
                                                    onClick={() => handleOpenSubscribe((config as any)[activeTool].models[activeTab].model_name)}
                                                >
                                                    {t("getKey")}
                                                </button>
                                            )
                                        }
                                    </div>
                                    <input
                                        type="password"
                                        className="form-input"
                                        data-field="api-key"
                                        value={(config as any)[activeTool].models[activeTab].api_key}
                                        onChange={(e) => handleApiKeyChange(e.target.value)}
                                        placeholder={t("enterKey")}
                                        spellCheck={false}
                                        autoComplete="off"
                                    />
                                </div>

                                <div className="form-group">
                                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '8px' }}>
                                            <label className="form-label" style={{ margin: 0 }}>{t("apiEndpoint")}</label>
                                            {(config as any)[activeTool].models[activeTab].is_custom && (
                                                <button
                                                    className="btn-link"
                                                    style={{ fontSize: '0.75rem', padding: '2px 8px' }}
                                                    onClick={() => {
                                                        setProviderFilter('all');
                                                        setSelectedProviderForUrl(null);
                                                        setShowProviderSelector(true);
                                                    }}
                                                >
                                                    {t("knownProviders")}
                                                </button>
                                            )}
                                        </div>
                                        <input
                                            type="text"
                                            className="form-input"
                                            data-field="api-url"
                                            value={(config as any)[activeTool].models[activeTab].model_url}
                                            onChange={(e) => handleModelUrlChange(e.target.value)}
                                            placeholder="https://api.example.com/v1"
                                            spellCheck={false}
                                            autoComplete="off"
                                            readOnly={!(config as any)[activeTool].models[activeTab].is_custom}
                                            style={!(config as any)[activeTool].models[activeTab].is_custom ? { backgroundColor: 'var(--theme-surface-muted)', cursor: 'not-allowed', color: 'var(--theme-text-muted)' } : {}}
                                        />
                                    </div>
                            </>
                        )}

                        <div style={{ display: 'flex', gap: '10px', marginTop: '24px' }}>
                            <button className="btn-primary" style={{ flex: 1 }} onClick={save}>{t("saveChanges")}</button>
                            {(config as any)[activeTool].models[activeTab].is_custom && (
                                <button
                                    className="btn-hide"
                                    style={{ flex: 0.5, backgroundColor: 'var(--theme-danger)', color: 'var(--theme-text-primary)', border: '1px solid color-mix(in srgb, var(--theme-danger) 70%, var(--theme-surface) 30%)' }}
                                    onClick={() => {
                                        const allModels = (config as any)[activeTool].models;
                                        const customModels = allModels.filter((m: any) => m.is_custom);
                                        if (customModels.length <= 1) {
                                            showToastMessage(t("cannotRemoveLastCustom") || "Cannot remove the last custom provider");
                                            return;
                                        }
                                        setConfirmDialog({
                                            show: true,
                                            title: t("confirmDelete"),
                                            message: t("removeCustomProvider"),
                                            onConfirm: () => {
                                                const newModels = allModels.filter((_: any, idx: number) => idx !== activeTab);
                                                const toolCfg = { ...(config as any)[activeTool], models: newModels };
                                                const newConfig = new main.AppConfig({ ...config, [activeTool]: toolCfg });
                                                setConfig(newConfig);
                                                setActiveTab(0);
                                                setConfirmDialog({ ...confirmDialog, show: false });
                                            }
                                        });
                                    }}
                                >
                                    {t("delete")}
                                </button>
                            )}
                            {(() => {
                                const allModels = (config as any)[activeTool].models;
                                const customModels = allModels.filter((m: any) => m.is_custom);
                                const canAddMore = customModels.length < 6;
                                return canAddMore && (
                                    <button
                                        className="btn-hide"
                                        style={{ flex: 0.5, backgroundColor: 'var(--theme-info-bg)', color: 'var(--theme-primary)', border: '1px solid var(--theme-primary-soft)' }}
                                        onClick={() => {
                                            const customCount = customModels.length;
                                            if (customCount >= 6) {
                                                showToastMessage(t("maxCustomProviders"));
                                                return;
                                            }
                                            const newCustomName = customCount === 1 ? "Custom1" : `Custom${customCount}`;
                                            const newCustom = {
                                                model_name: newCustomName,
                                                model_id: "",
                                                model_url: "",
                                                api_key: "",
                                                wire_api: "",
                                                is_custom: true
                                            };
                                            // Ensure custom models are always at the end
                                            const nonCustom = allModels.filter((m: any) => !m.is_custom);
                                            const existingCustom = allModels.filter((m: any) => m.is_custom);
                                            const newModels = [...nonCustom, ...existingCustom, newCustom];
                                            const toolCfg = { ...(config as any)[activeTool], models: newModels };
                                            const newConfig = new main.AppConfig({ ...config, [activeTool]: toolCfg });
                                            setConfig(newConfig);
                                            // Switch to the newly added custom provider
                                            setActiveTab(newModels.length - 1);
                                            // Reset tab scroll to show the new tab
                                            const configurableCount = newModels.filter((m: any) => m.model_name !== "Original").length;
                                            setTabStartIndex(Math.max(0, configurableCount - 4));
                                        }}
                                    >
                                        + {t("addCustomProvider")}
                                    </button>
                                );
                            })()}
                            <button className="btn-hide" style={{ flex: 1 }} onClick={() => setShowModelSettings(false)}>{t("close")}</button>
                        </div>
                    </div>
                </div>
            )}

            {showProviderSelector && (
                <div className="modal-overlay" style={{ backgroundColor: 'rgba(0,0,0,0.35)', backdropFilter: 'blur(3px)' }} onClick={() => { setShowProviderSelector(false); setHoveredProvider(null); }}>
                    <div className="modal-content" style={{ maxWidth: '480px', maxHeight: '70vh', padding: '20px', borderRadius: '16px', border: 'none', boxShadow: '0 20px 40px rgba(0,0,0,0.12)' }} onClick={(e) => e.stopPropagation()}>
                        <h2 style={{ margin: '0 0 16px 0', fontSize: '1.1rem', fontWeight: 700, color: 'var(--theme-text-primary)', textAlign: 'center' }}>{t("selectProviderTitle")}</h2>
                        
                        {/* Filter pills */}
                        <div style={{ display: 'flex', gap: '6px', marginBottom: '14px', justifyContent: 'center' }}>
                            {(['all', 'china', 'global'] as const).map(f => (
                                <button
                                    key={f}
                                    onClick={() => setProviderFilter(f)}
                                    style={{
                                        padding: '5px 16px', fontSize: '0.8rem', borderRadius: '20px', border: 'none', cursor: 'pointer', fontWeight: 600,
                                        backgroundColor: providerFilter === f ? 'var(--theme-primary)' : 'var(--theme-surface-muted)',
                                        color: providerFilter === f ? 'var(--theme-text-primary)' : 'var(--theme-text-secondary)',
                                        transition: 'all 0.2s'
                                    }}
                                >
                                    {f === 'all' ? t("allProviders") : f === 'china' ? t("chinaProviders") : t("globalProviders")}
                                </button>
                            ))}
                        </div>

                        {/* Provider grid */}
                        <div style={{ maxHeight: 'calc(70vh - 180px)', overflowY: 'auto', padding: '2px' }}>
                            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(3, 1fr)', gap: '8px' }}>
                                {getFilteredProviders().map((provider, index) => {
                                    const isSelected = selectedProviderForUrl?.name === provider.name && selectedProviderForUrl?.url === provider.url;
                                    return (
                                        <div
                                            key={index}
                                            onClick={() => handleProviderSelect(provider)}
                                            onDoubleClick={() => { handleProviderSelect(provider); confirmProviderSelection(); }}
                                            onMouseEnter={(e) => {
                                                const rect = e.currentTarget.getBoundingClientRect();
                                                setHoveredProvider({ provider, x: rect.left + rect.width / 2, y: rect.top - 4 });
                                            }}
                                            onMouseLeave={() => setHoveredProvider(null)}
                                            style={{
                                                padding: '10px 8px', borderRadius: '10px', cursor: 'pointer', textAlign: 'center',
                                                border: isSelected ? '2px solid var(--theme-primary)' : '1.5px solid var(--theme-border-subtle)',
                                                backgroundColor: isSelected ? 'var(--theme-info-bg)' : 'var(--theme-surface)',
                                                transition: 'all 0.15s ease',
                                                boxShadow: isSelected ? '0 2px 8px rgba(59,130,246,0.15)' : '0 1px 3px rgba(15,23,42,0.08)',
                                                position: 'relative'
                                            }}
                                        >
                                            <div style={{ fontSize: '0.8rem', fontWeight: 600, color: isSelected ? 'var(--theme-primary)' : 'var(--theme-text-primary)', lineHeight: 1.3, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '3px' }}>
                                                <span title={provider.region === 'china' ? (lang === 'en' ? 'China' : '国内') : (lang === 'en' ? 'Global' : '国外')} style={{ fontSize: '0.7rem', flexShrink: 0 }}>{provider.region === 'china' ? '🇨🇳' : '🌐'}</span>
                                                <span style={{ overflow: 'hidden', textOverflow: 'ellipsis' }}>{provider.name}</span>
                                            </div>
                                        </div>
                                    );
                                })}
                            </div>
                        </div>

                        {/* Action buttons */}
                        <div style={{ display: 'flex', gap: '10px', marginTop: '14px' }}>
                            <button className="btn-primary" style={{ flex: 1, borderRadius: '10px' }} onClick={confirmProviderSelection} disabled={!selectedProviderForUrl}>{t("confirm")}</button>
                            <button className="btn-hide" style={{ flex: 1, borderRadius: '10px' }} onClick={() => { setShowProviderSelector(false); setSelectedProviderForUrl(null); setHoveredProvider(null); }}>{t("cancel")}</button>
                        </div>
                    </div>

                    {/* Tooltip bubble for URL */}
                    {hoveredProvider && (
                        <div style={{
                            position: 'fixed',
                            left: hoveredProvider.x,
                            top: hoveredProvider.y,
                            transform: 'translate(-50%, -100%)',
                            backgroundColor: 'var(--theme-surface)',
                            color: 'var(--theme-text-primary)',
                            padding: '6px 12px',
                            borderRadius: '8px',
                            fontSize: '0.75rem',
                            fontFamily: 'monospace',
                            whiteSpace: 'nowrap',
                            zIndex: 9999,
                            pointerEvents: 'none',
                            boxShadow: '0 4px 12px rgba(0,0,0,0.2)',
                            maxWidth: '360px',
                            overflow: 'hidden',
                            textOverflow: 'ellipsis'
                        }}>
                            {hoveredProvider.provider.url}
                            {hoveredProvider.provider.description && (
                                <div style={{ fontSize: '0.65rem', color: 'var(--theme-text-muted)', marginTop: '2px' }}>{hoveredProvider.provider.description}</div>
                            )}
                            {/* Arrow */}
                            <div style={{
                                position: 'absolute', bottom: '-5px', left: '50%', transform: 'translateX(-50%)',
                                width: 0, height: 0,
                                borderLeft: '6px solid transparent', borderRight: '6px solid transparent', borderTop: '6px solid var(--theme-surface)'
                            }} />
                        </div>
                    )}
                </div>
            )}

            {showStartupPopup && (
                <div className="modal-overlay" style={{ backgroundColor: 'color-mix(in srgb, var(--theme-page-bg) 55%, transparent)', backdropFilter: 'blur(4px)' }}>
                    <div className="modal-content" style={{
                        width: '320px',
                        textAlign: 'center',
                        padding: 0,
                        borderRadius: '16px',
                        overflow: 'hidden',
                        border: '1px solid var(--theme-border)',
                        backgroundColor: 'var(--theme-surface)',
                        boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.16), 0 10px 10px -5px rgba(0, 0, 0, 0.08)'
                    }}>
                        <div style={{
                            background: 'linear-gradient(135deg, var(--theme-info-bg) 0%, color-mix(in srgb, var(--theme-primary-soft) 65%, var(--theme-surface) 35%) 100%)',
                            padding: '25px 20px',
                            color: 'var(--theme-text-primary)',
                            position: 'relative',
                            borderBottom: '1px solid var(--theme-border)'
                        }}>
                            <button
                                className="modal-close"
                                onClick={() => setShowStartupPopup(false)}
                                style={{ color: 'var(--theme-text-muted)', opacity: 0.8, top: '10px', right: '15px', zIndex: 10 }}
                            >&times;</button>
                            <div style={{
                                fontSize: '2.5rem',
                                marginBottom: '10px',
                                background: 'linear-gradient(135deg, var(--theme-primary) 0%, var(--theme-primary-strong) 55%, var(--theme-link-color) 100%)',
                                WebkitBackgroundClip: 'text',
                                WebkitTextFillColor: 'transparent',
                                fontWeight: '900',
                                lineHeight: 1,
                                filter: 'drop-shadow(0 2px 4px color-mix(in srgb, var(--theme-primary) 18%, transparent))'
                            }}>{`</>`}</div>
                            <h3 style={{ margin: 0, color: 'var(--theme-text-primary)', fontSize: '1.2rem', fontWeight: 'bold' }}>{t("startupTitle")}</h3>
                            <p style={{
                                margin: '6px 0 0 0',
                                background: 'linear-gradient(135deg, var(--theme-primary), var(--theme-primary-strong), var(--theme-link-color))',
                                WebkitBackgroundClip: 'text',
                                WebkitTextFillColor: 'transparent',
                                fontSize: '0.95rem',
                                fontWeight: '700'
                            }}>
                                {t("slogan")}
                            </p>
                        </div>

                        <div style={{ padding: '20px 25px' }}>
                            <div style={{ display: 'flex', flexDirection: 'column', gap: '10px', marginBottom: '20px' }}>
                                <button
                                    style={{
                                        width: '100%',
                                        padding: '10px',
                                        borderRadius: '10px',
                                        fontSize: '0.95rem',
                                        fontWeight: '600',
                                        display: 'flex',
                                        alignItems: 'center',
                                        justifyContent: 'center',
                                        gap: '8px',
                                        background: 'linear-gradient(135deg, var(--theme-info-bg), color-mix(in srgb, var(--theme-primary-soft) 55%, var(--theme-surface) 45%))',
                                        color: 'var(--theme-primary)',
                                        border: '1px solid var(--theme-primary-soft)',
                                        boxShadow: '0 2px 4px color-mix(in srgb, var(--theme-primary) 16%, transparent)',
                                        cursor: 'pointer',
                                        transition: 'all 0.2s'
                                    }}
                                    onClick={() => {
                                        BrowserOpenURL("https://www.bilibili.com/video/BV1wmvoBnEF1");
                                    }}
                                >
                                    <span>🎬</span> {t("quickStart")}
                                </button>
                                <button
                                    className="btn-link"
                                    style={{
                                        padding: '10px',
                                        border: '1px solid var(--theme-border)',
                                        borderRadius: '10px',
                                        fontSize: '0.95rem',
                                        fontWeight: '500',
                                        color: 'var(--theme-text-secondary)',
                                        backgroundColor: 'var(--theme-surface)',
                                        display: 'flex',
                                        alignItems: 'center',
                                        justifyContent: 'center',
                                        gap: '8px',
                                        boxShadow: '0 1px 2px rgba(0,0,0,0.05)'
                                    }}
                                    onClick={() => {
                                        const manualUrl = (lang === 'zh-Hans' || lang === 'zh-Hant')
                                            ? "https://github.com/rapidai/maclaw/blob/main/UserManual_CN.md"
                                            : "https://github.com/rapidai/maclaw/blob/main/UserManual_EN.md";
                                        BrowserOpenURL(manualUrl);
                                    }}
                                >
                                    <span>📖</span> {t("manual")}
                                </button>
                            </div>

                            <div style={{
                                display: 'flex',
                                alignItems: 'center',
                                justifyContent: 'center',
                                gap: '8px'
                            }}>
                                <label style={{
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: '6px',
                                    cursor: 'pointer',
                                    fontSize: '0.8rem',
                                    color: 'var(--theme-text-muted)'
                                }}>
                                    <input
                                        type="checkbox"
                                        checked={config?.hide_startup_popup || false}
                                        style={{
                                            width: '14px',
                                            height: '14px',
                                            cursor: 'pointer'
                                        }}
                                        onChange={(e) => {
                                            if (config) {
                                                const newConfig = new main.AppConfig({ ...config, hide_startup_popup: e.target.checked });
                                                setConfig(newConfig);
                                                SaveConfig(newConfig);
                                            }
                                        }}
                                    />
                                    {t("dontShowAgain")}
                                </label>
                            </div>
                        </div>
                    </div>
                </div>
            )}

            {/* MaClaw Onboarding Wizard */}
            {showMaclawLLMPopup && (
                <OnboardingWizard
                    lang={lang}
                    hubUrl={config?.remote_hub_url || ""}
                    email={config?.remote_email || ""}
                    uiMode={config?.ui_mode || ""}
                    brandId={brandInfo?.id}
                    brandDisplayName={brandInfo?.displayName}
                    onClose={() => setShowMaclawLLMPopup(false)}
                    onLLMConfigured={() => {
                        setMaclawLLMOnline(true);
                        setMaclawLLMConfigured(true);
                    }}
                    onRegistered={() => {
                        refreshRemotePanel();
                    }}
                    onSaveField={(patch) => {
                        saveRemoteConfigField(patch as any);
                        // If ui_mode changed, update local state immediately for reactivity.
                        // The actual persist is handled by saveRemoteConfigField (which
                        // reloads config from backend first to avoid overwriting concurrent
                        // backend changes like SSO provider switch).
                        if (patch.ui_mode && config) {
                            setConfig(new main.AppConfig({ ...config, ...patch }));
                        }
                    }}
                />
            )}


            {/* Confirm Dialog */}
            {confirmDialog.show && (
                <div style={{
                    position: 'fixed',
                    top: 0,
                    left: 0,
                    right: 0,
                    bottom: 0,
                    backgroundColor: 'color-mix(in srgb, var(--theme-page-bg) 65%, transparent)',
                    backdropFilter: 'blur(4px)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    zIndex: 10000,
                    animation: 'fadeIn 0.2s ease-out'
                }}>
                    <div style={{
                        backgroundColor: 'var(--theme-surface)',
                        borderRadius: '12px',
                        padding: '24px',
                        minWidth: '360px',
                        maxWidth: '420px',
                        boxShadow: '0 20px 60px rgba(0, 0, 0, 0.24), 0 0 0 1px color-mix(in srgb, var(--theme-border) 65%, transparent)',
                        border: '1px solid var(--theme-border)',
                        animation: 'slideUp 0.3s ease-out',
                        position: 'relative',
                        color: 'var(--theme-text-primary)'
                    }}>
                        {/* Icon */}
                        <div style={{
                            width: '48px',
                            height: '48px',
                            borderRadius: '50%',
                            backgroundColor: 'var(--theme-danger-bg)',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            marginBottom: '16px',
                            border: '2px solid color-mix(in srgb, var(--theme-danger) 18%, transparent)'
                        }}>
                            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="var(--theme-danger)" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                                <circle cx="12" cy="12" r="10"></circle>
                                <line x1="12" y1="8" x2="12" y2="12"></line>
                                <line x1="12" y1="16" x2="12.01" y2="16"></line>
                            </svg>
                        </div>

                        {/* Title */}
                        <h3 style={{
                            margin: '0 0 8px 0',
                            fontSize: '1.15rem',
                            color: 'var(--theme-text-primary)',
                            fontWeight: '700',
                            letterSpacing: '-0.02em'                    }}>
                            {confirmDialog.title}
                        </h3>

                        {/* Message */}
                        <p style={{
                            margin: '0 0 20px 0',
                            color: 'var(--theme-text-secondary)',
                            fontSize: '0.9rem',
                            lineHeight: '1.5',
                            fontWeight: '400'
                        }}>
                            {confirmDialog.message}
                        </p>

                        {/* Buttons */}
                        <div style={{
                            display: 'flex',
                            justifyContent: 'flex-end',
                            gap: '10px'
                        }}>
                            <button
                                onClick={() => setConfirmDialog({ ...confirmDialog, show: false })}
                                style={{
                                    padding: '8px 20px',
                                    backgroundColor: 'var(--theme-surface-muted)',
                                    color: 'var(--theme-text-secondary)',
                                    border: '1px solid var(--theme-border)',
                                    borderRadius: '8px',
                                    cursor: 'pointer',
                                    fontSize: '0.875rem',
                                    fontWeight: '600',
                                    transition: 'all 0.2s',
                                    boxShadow: '0 1px 2px rgba(0, 0, 0, 0.05)'
                                }}
                                onMouseEnter={(e) => {
                                    e.currentTarget.style.backgroundColor = 'var(--theme-surface)';
                                    e.currentTarget.style.borderColor = 'var(--theme-primary-soft)';
                                    e.currentTarget.style.transform = 'translateY(-1px)';
                                    e.currentTarget.style.boxShadow = '0 2px 4px rgba(0, 0, 0, 0.1)';
                                }}
                                onMouseLeave={(e) => {
                                    e.currentTarget.style.backgroundColor = 'var(--theme-surface-muted)';
                                    e.currentTarget.style.borderColor = 'var(--theme-border)';
                                    e.currentTarget.style.transform = 'translateY(0)';
                                    e.currentTarget.style.boxShadow = '0 1px 2px rgba(0, 0, 0, 0.05)';
                                }}
                            >
                                {t("cancel")}
                            </button>
                            <button
                                onClick={confirmDialog.onConfirm}
                                style={{
                                    padding: '8px 20px',
                                    backgroundColor: 'var(--theme-danger)',
                                    color: 'var(--theme-text-primary)',
                                    border: '1px solid color-mix(in srgb, var(--theme-danger) 30%, transparent)',
                                    borderRadius: '8px',
                                    cursor: 'pointer',
                                    fontSize: '0.875rem',
                                    fontWeight: '600',
                                    transition: 'all 0.2s',
                                    boxShadow: '0 2px 4px color-mix(in srgb, var(--theme-danger) 35%, transparent)'
                                }}
                                onMouseEnter={(e) => {
                                    e.currentTarget.style.backgroundColor = 'color-mix(in srgb, var(--theme-danger) 88%, black 12%)';
                                    e.currentTarget.style.transform = 'translateY(-1px)';
                                    e.currentTarget.style.boxShadow = '0 4px 8px color-mix(in srgb, var(--theme-danger) 45%, transparent)';
                                }}
                                onMouseLeave={(e) => {
                                    e.currentTarget.style.backgroundColor = 'var(--theme-danger)';
                                    e.currentTarget.style.transform = 'translateY(0)';
                                    e.currentTarget.style.boxShadow = '0 2px 4px color-mix(in srgb, var(--theme-danger) 35%, transparent)';
                                }}
                            >
                                {t("confirm")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Proxy Settings Dialog (project-level only) */}
            {showProxySettings && config && (
                <div className="modal-overlay">
                    <div className="modal-content" style={{ width: '540px', textAlign: 'left' }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
                            <h3 style={{ margin: 0, color: 'var(--theme-primary)' }}>
                                {t("proxySettings")}
                            </h3>
                            <button className="modal-close" onClick={() => setShowProxySettings(false)}>&times;</button>
                        </div>

                        {config?.default_proxy_host && (
                            <div style={{ marginBottom: '15px', padding: '10px', backgroundColor: 'var(--theme-info-bg)', borderRadius: '6px', fontSize: '0.85rem', border: '1px solid var(--theme-primary-soft)' }}>
                                <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer' }}>
                                    <input
                                        type="checkbox"
                                        checked={(() => {
                                            const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                            return proj && !proj.proxy_host;
                                        })()}
                                        onChange={(e) => {
                                            const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                            if (proj && e.target.checked) {
                                                const newProjects = config.projects.map((p: any) =>
                                                    p.id === proj.id ? { ...p, proxy_host: '', proxy_port: '', proxy_username: '', proxy_password: '' } : p
                                                );
                                                const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                                setConfig(newConfig);
                                                SaveConfig(newConfig);
                                            }
                                        }}
                                        style={{ marginRight: '8px' }}
                                    />
                                    <span>{t("useDefaultProxy")} ({config.default_proxy_host}:{config.default_proxy_port})</span>
                                </label>
                            </div>
                        )}

                        {/* Host + Port row */}
                        <div style={{ display: 'flex', gap: '10px', marginBottom: '12px' }}>
                            <div style={{ flex: 1 }}>
                                <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyHost")}</label>
                                <input type="text" className="form-input" spellCheck={false}
                                    placeholder={t("proxyHostPlaceholder")}
                                    value={config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.proxy_host || ''}
                                    onChange={(e) => {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch); if (proj) { const np = config.projects.map((p: any) => p.id === proj.id ? { ...p, proxy_host: e.target.value } : p); setConfig(new main.AppConfig({ ...config, projects: np })); }
                                    }}
                                />
                            </div>
                            <div style={{ width: '90px', flexShrink: 0 }}>
                                <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyPort")}</label>
                                <input type="text" className="form-input" spellCheck={false}
                                    placeholder={t("proxyPortPlaceholder")}
                                    value={config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.proxy_port || ''}
                                    onChange={(e) => {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch); if (proj) { const np = config.projects.map((p: any) => p.id === proj.id ? { ...p, proxy_port: e.target.value } : p); setConfig(new main.AppConfig({ ...config, projects: np })); }
                                    }}
                                />
                            </div>
                        </div>

                        {/* Username + Password row */}
                        <div style={{ display: 'flex', gap: '10px', marginBottom: '12px' }}>
                            <div style={{ flex: 1 }}>
                                <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyUsername")}</label>
                                <input type="text" className="form-input" spellCheck={false} autoComplete="off"
                                    value={config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.proxy_username || ''}
                                    onChange={(e) => {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch); if (proj) { const np = config.projects.map((p: any) => p.id === proj.id ? { ...p, proxy_username: e.target.value } : p); setConfig(new main.AppConfig({ ...config, projects: np })); }
                                    }}
                                />
                            </div>
                            <div style={{ flex: 1 }}>
                                <label className="form-label" style={{ fontSize: '0.78rem' }}>{t("proxyPassword")}</label>
                                <input type="password" className="form-input" autoComplete="new-password"
                                    value={config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.proxy_password || ''}
                                    onChange={(e) => {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch); if (proj) { const np = config.projects.map((p: any) => p.id === proj.id ? { ...p, proxy_password: e.target.value } : p); setConfig(new main.AppConfig({ ...config, projects: np })); }
                                    }}
                                />
                            </div>
                        </div>

                        <div style={{ display: 'flex', gap: '10px', justifyContent: 'flex-end', marginTop: '20px' }}>
                            <button className="btn-secondary" onClick={() => setShowProxySettings(false)} style={{ padding: '8px 16px' }}>
                                {t("cancel")}
                            </button>
                            <button className="btn-primary" onClick={() => {
                                    SaveConfig(config);
                                    setShowProxySettings(false);
                                    const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                    if (proj && !proj.use_proxy) {
                                        const newProjects = config.projects.map((p: any) => p.id === proj.id ? { ...p, use_proxy: true } : p);
                                        const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                        setConfig(newConfig);
                                        SaveConfig(newConfig);
                                    }
                                }} style={{ padding: '8px 16px' }}>
                                {t("saveChanges")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {showInstallSkillModal && (
                <div className="modal-overlay">
                    <div className="modal-content" style={{ width: '500px', maxWidth: '95vw' }}>
                        <div className="modal-header" style={{ display: 'flex', flexWrap: 'wrap', gap: '10px', alignItems: 'center' }}>
                            <h3 style={{ margin: 0, color: 'var(--theme-success)', whiteSpace: 'nowrap' }}>{t("selectSkillsToInstall")}</h3>

                            <div style={{
                                display: 'flex',
                                alignItems: 'center',
                                gap: '12px',
                                padding: '4px 12px',
                                backgroundColor: 'var(--theme-surface-muted)',
                                borderRadius: '20px',
                                fontSize: '0.8rem',
                                marginLeft: '5px'
                            }}>
                                <span style={{ color: 'var(--theme-text-muted)', fontWeight: '500' }}>{t("installLocation")}</span>
                                <label style={{ display: 'flex', alignItems: 'center', gap: '4px', cursor: 'pointer', color: installLocation === 'user' ? 'var(--theme-success)' : 'var(--theme-text-secondary)', fontWeight: installLocation === 'user' ? 'bold' : 'normal' }}>
                                    <input
                                        type="radio"
                                        name="installLocation"
                                        checked={installLocation === 'user'}
                                        onChange={() => setInstallLocation('user')}
                                        style={{ margin: 0 }}
                                    /> {t("userLocation")}
                                </label>
                                <label style={{ display: 'flex', alignItems: 'center', gap: '4px', cursor: 'pointer', color: installLocation === 'project' ? 'var(--theme-success)' : 'var(--theme-text-secondary)', fontWeight: installLocation === 'project' ? 'bold' : 'normal' }}>
                                    <input
                                        type="radio"
                                        name="installLocation"
                                        checked={installLocation === 'project'}
                                        onChange={() => {
                                            setInstallLocation('project');
                                            if (config && config.current_project) {
                                                setInstallProject(config.current_project);
                                            }
                                        }}
                                        style={{ margin: 0 }}
                                    /> {t("projectLocation")}
                                </label>
                                {installLocation === 'project' && config?.projects && (
                                    <select
                                        value={installProject}
                                        onChange={(e) => setInstallProject(e.target.value)}
                                        style={{
                                            padding: '2px 4px',
                                            borderRadius: '4px',
                                            border: '1px solid var(--theme-border)',
                                            fontSize: '0.8rem',
                                            background: 'var(--theme-surface)',
                                            color: 'var(--theme-text-primary)',
                                            maxWidth: '120px'
                                        }}
                                    >
                                        {config.projects.map((proj: any) => (
                                            <option key={proj.id} value={proj.id}>
                                                {proj.name}
                                            </option>
                                        ))}
                                    </select>
                                )}
                            </div>

                            <button
                                onClick={() => { setShowInstallSkillModal(false); switchTool('skills'); }}
                                style={{
                                    background: 'none',
                                    border: '1px solid var(--theme-border)',
                                    borderRadius: '16px',
                                    padding: '4px 10px',
                                    fontSize: '0.8rem',
                                    cursor: 'pointer',
                                    color: 'var(--theme-link-color)',
                                    display: 'flex',
                                    alignItems: 'center',
                                    gap: '4px',
                                    whiteSpace: 'nowrap',
                                }}
                                title={t("skills")}
                            >
                                🛠️ {t("skills")}
                            </button>

                            <button onClick={() => setShowInstallSkillModal(false)} className="btn-close" style={{ marginLeft: 'auto' }}>&times;</button>
                        </div>
                        <div className="modal-body" style={{ maxHeight: '300px', overflowY: 'auto', padding: '10px 0' }}>
                            {(() => {
                                const filtered = installLocation === 'project' 
                                    ? skills.filter(s => s.type !== 'address') 
                                    : skills;
                                
                                if (filtered.length === 0) {
                                    return (
                                        <div style={{ textAlign: 'center', color: 'var(--theme-text-muted)', padding: '20px' }}>
                                            {t("noSkills")}
                                        </div>
                                    );
                                }

                                return (
                                    <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                                        {filtered.map((skill, idx) => (
                                            <label key={idx} style={{
                                                display: 'flex',
                                                alignItems: 'center',
                                                padding: '8px 12px',
                                                border: '1px solid var(--theme-border)',
                                                borderRadius: '6px',
                                                cursor: skill.installed ? 'not-allowed' : 'pointer',
                                                backgroundColor: selectedSkillsToInstall.includes(skill.name) ? 'var(--theme-success-bg)' : 'var(--theme-surface)',
                                                opacity: skill.installed ? 0.5 : 1,
                                                position: 'relative'
                                            }}>
                                                <input
                                                    type="checkbox"
                                                    checked={selectedSkillsToInstall.includes(skill.name)}
                                                    disabled={skill.installed}
                                                    onChange={(e) => {
                                                        if (e.target.checked) {
                                                            setSelectedSkillsToInstall([...selectedSkillsToInstall, skill.name]);
                                                        } else {
                                                            setSelectedSkillsToInstall(selectedSkillsToInstall.filter(n => n !== skill.name));
                                                        }
                                                    }}
                                                    style={{ marginRight: '10px' }}
                                                />
                                                <div style={{ flex: 1 }} title={skill.description}>
                                                    <div style={{ fontWeight: 'bold', fontSize: '0.9rem' }}>
                                                        {skill.name}
                                                        {skill.installed && (
                                                            <span style={{
                                                                marginLeft: '8px',
                                                                fontSize: '0.75rem',
                                                                color: 'var(--theme-success)',
                                                                backgroundColor: 'var(--theme-success-bg)',
                                                                padding: '2px 6px',
                                                                borderRadius: '4px',
                                                                fontWeight: 'normal'
                                                            }}>
                                                                {t("installed")}
                                                            </span>
                                                        )}
                                                    </div>
                                                </div>
                                            </label>
                                        ))}
                                    </div>
                                );
                            })()}
                        </div>
                        <div className="modal-footer" style={{ marginTop: '15px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                            {activeTool === 'claude' ? (
                                <button
                                    className="btn-link"
                                    style={{
                                        color: 'var(--theme-link-color)',
                                        fontSize: '0.85rem',
                                        padding: '4px 15px',
                                        display: 'flex',
                                        alignItems: 'center',
                                        gap: '6px',
                                        opacity: isMarketplaceInstalling ? 0.6 : 1,
                                        minWidth: '120px',
                                        justifyContent: 'center'
                                    }}
                                    disabled={isMarketplaceInstalling}
                                    onClick={async () => {
                                        setIsMarketplaceInstalling(true);
                                        try {
                                            await InstallDefaultMarketplace();
                                            showToastMessage("Marketplace installed successfully!");
                                        } catch (err) {
                                            showToastMessage("Error installing marketplace: " + err);
                                        } finally {
                                            setIsMarketplaceInstalling(false);
                                        }
                                    }}
                                >
                                    {isMarketplaceInstalling && (
                                        <div style={{
                                            width: '12px',
                                            height: '12px',
                                            border: '2px solid var(--theme-link-color)',
                                            borderTopColor: 'transparent',
                                            borderRadius: '50%',
                                            animation: 'spin 1s linear infinite'
                                        }}></div>
                                    )}
                                    {t("installDefaultMarketplace")}
                                </button>
                            ) : (
                                <div></div>
                            )}
                            <div style={{ display: 'flex', gap: '10px' }}>
                                <button className="btn-secondary" onClick={() => setShowInstallSkillModal(false)}>{t("cancel")}</button>
                                <button
                                    className="btn-primary"
                                    style={{
                                        backgroundColor: 'var(--theme-success)',
                                        borderColor: 'var(--theme-success)',
                                        display: 'flex',
                                        alignItems: 'center',
                                        gap: '6px',
                                        opacity: (selectedSkillsToInstall.length === 0 || isBatchInstalling) ? 0.6 : 1
                                    }}
                                    disabled={selectedSkillsToInstall.length === 0 || isBatchInstalling}
                                    onClick={async () => {
                                        setIsBatchInstalling(true);
                                        let successCount = 0;
                                        let failCount = 0;
                                        
                                        // Get project path if needed
                                        let targetProjectPath = "";
                                        if (installLocation === 'project') {
                                            const p = config?.projects?.find((proj: any) => proj.id === installProject);
                                            if (p) targetProjectPath = p.path;
                                        }

                                        for (const name of selectedSkillsToInstall) {
                                            const skill = skills.find(s => s.name === name);
                                            if (skill) {
                                                // Check for incompatibility
                                                const isGeminiOrCodex = activeTool?.toLowerCase() === 'gemini' || activeTool?.toLowerCase() === 'codex';
                                                if (isGeminiOrCodex && skill.type === 'address') {
                                                    console.warn(`Skill ${skill.name} is not supported for ${activeTool}`);
                                                    failCount++;
                                                    continue;
                                                }

                                                try {
                                                    await InstallSkill(skill.name, skill.description, skill.type, skill.value, installLocation, targetProjectPath, activeTool);
                                                    successCount++;
                                                } catch (e) {
                                                    console.error(e);
                                                    failCount++;
                                                }
                                            }
                                        }
                                        
                                        setIsBatchInstalling(false);
                                        setShowInstallSkillModal(false);
                                        
                                        if (failCount > 0) {
                                            const isGeminiOrCodex = activeTool?.toLowerCase() === 'gemini' || activeTool?.toLowerCase() === 'codex';
                                            if (isGeminiOrCodex && selectedSkillsToInstall.some(name => skills.find(s => s.name === name)?.type === 'address')) {
                                                showToastMessage(t("skillZipOnlyError"));
                                            } else {
                                                showToastMessage(`${successCount} installed, ${failCount} failed.`);
                                            }
                                        } else {
                                            showToastMessage(`${successCount} skills installed successfully.`);
                                        }
                                    }}
                                >
                                    {isBatchInstalling && (
                                        <div style={{
                                            width: '12px',
                                            height: '12px',
                                            border: '2px solid var(--theme-text-primary)',
                                            borderTopColor: 'transparent',
                                            borderRadius: '50%',
                                            animation: 'spin 1s linear infinite'
                                        }}></div>
                                    )}
                                    {t("install")}
                                </button>
                            </div>
                        </div>
                    </div>
                </div>
            )}

            {/* Toast notifications are now handled by global ToastProvider */}
                </div>
            </div>
        </div>
    );
}

export default App;
