import { useEffect, useState, useRef } from 'react';
import './App.css';
import { buildNumber } from './version';
import appIcon from './assets/images/appicon.png';
import claudecodeIcon from './assets/images/claudecode.png';
import codebuddyIcon from './assets/images/Codebuddy.png';
import codexIcon from './assets/images/Codex.png';
import geminiIcon from './assets/images/gemincli.png';
import iflowIcon from './assets/images/iflow.png';
import opencodeIcon from './assets/images/opencode.png';
import kiloIcon from './assets/images/KiloCode.png';
import kodeIcon from './assets/images/Kodecli.png';
import qoderIcon from './assets/images/qodercli.png';
import { CheckToolsStatus, InstallTool, InstallToolOnDemand, IsToolBeingInstalled, LoadConfig, SaveConfig, CheckEnvironment, ResizeWindow, WindowHide, LaunchTool, SelectProjectDir, SetLanguage, GetUserHomeDir, CheckUpdate, ShowMessage, ReadBBS, ReadTutorial, ReadThanks, ClipboardGetText, ListPythonEnvironments, PackLog, ShowItemInFolder, GetSystemInfo, OpenSystemUrl, DownloadUpdate, CancelDownload, LaunchInstallerAndExit, ListSkills, ListSkillsWithInstallStatus, AddSkill, DeleteSkill, SelectSkillFile, GetSkillsDir, SetEnvCheckInterval, GetEnvCheckInterval, ShouldCheckEnvironment, UpdateLastEnvCheckTime, InstallDefaultMarketplace, InstallSkill, IsWindowsTerminalAvailable } from "../wailsjs/go/main/App";
import { EventsOn, EventsOff, BrowserOpenURL, Quit } from "../wailsjs/runtime";
import { main } from "../wailsjs/go/models";
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import rehypeRaw from 'rehype-raw';

const subscriptionUrls: { [key: string]: string } = {
    "GLM": "https://bigmodel.cn/glm-coding",
    "Kimi": "https://www.kimi.com/membership/pricing?from=upgrade_plan&track_id=1d2446f5-f45f-4ae5-961e-c0afe936a115",
    "Doubao": "https://www.volcengine.com/activity/codingplan",
    "MiniMax": "https://platform.minimaxi.com/user-center/payment/coding-plan",
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

const knownProviderEndpoints: ProviderEndpoint[] = [
    // Anthropic Protocol (Claude)
    { name: "Claude Official", url: "https://api.anthropic.com/v1", protocol: "anthropic", region: "global", description: "Official Claude API" },
    { name: "MiniMax", url: "https://api.minimaxi.com/anthropic", protocol: "anthropic", region: "china" },
    { name: "DeepSeek", url: "https://api.deepseek.com/anthropic", protocol: "anthropic", region: "china" },
    { name: "ChatFire", url: "https://api.chatfire.cn/v1", protocol: "anthropic", region: "china" },
    { name: "OpenRouter", url: "https://openrouter.ai/api", protocol: "anthropic", region: "global" },
    
    // Gemini Protocol
    { name: "Google Gemini Official", url: "https://generativelanguage.googleapis.com/v1beta", protocol: "gemini", region: "global", description: "Official Google Gemini API" },

    // OpenAI Protocol (Codex)
    { name: "OpenAI Official", url: "https://api.openai.com/v1", protocol: "openai", region: "global", description: "Official OpenAI API" },
    { name: "xAI (Grok)", url: "https://api.x.ai/v1", protocol: "openai", region: "global", description: "xAI Grok API" },
    { name: "GLM", url: "https://open.bigmodel.cn/api/paas/v4", protocol: "openai", region: "china" },
    { name: "Kimi", url: "https://api.kimi.com/coding/v1", protocol: "openai", region: "china" },
    { name: "Doubao", url: "https://ark.cn-beijing.volces.com/api/coding", protocol: "openai", region: "china" },
    { name: "Doubao Codex", url: "https://ark.cn-beijing.volces.com/api/coding/v3", protocol: "openai", region: "china" },
    { name: "DeepSeek Codex", url: "https://api.aicodemirror.com/api/codex/backend-api/codex", protocol: "openai", region: "china" },
    { name: "OpenRouter", url: "https://openrouter.ai/api/v1", protocol: "openai", region: "global" },
    { name: "Together AI", url: "https://api.together.xyz/v1", protocol: "openai", region: "global" },
    { name: "Groq", url: "https://api.groq.com/openai/v1", protocol: "openai", region: "global" },
    { name: "Perplexity", url: "https://api.perplexity.ai", protocol: "openai", region: "global" },
];



// Recommended model IDs per provider (used for model name suggestions)
const recommendedModels: { [provider: string]: { id: string; note?: string }[] } = {
    "GLM": [{ id: "glm-4.7" }],
    "Kimi": [{ id: "kimi-k2-thinking" }, { id: "kimi-for-coding" }],
    "Doubao": [{ id: "doubao-seed-code-preview-latest" }],
    "MiniMax": [{ id: "MiniMax-M2.1" }],
    "DeepSeek": [{ id: "deepseek-chat" }],
    "ChatFire": [{ id: "sonnet" }, { id: "gpt-5.1-codex-mini" }, { id: "gpt-4o" }, { id: "gemini-2.5-pro" }],
    "XiaoMi": [{ id: "mimo-v2-flash" }],
    "摩尔线程": [{ id: "GLM-4.7" }],
    "快手": [{ id: "kat-coder-pro-v1" }],
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
const APP_VERSION = "4.0.0.9100"

// Tool name constants to avoid repeated string arrays
const TOOL_NAMES = ['claude', 'gemini', 'codex', 'opencode', 'codebuddy', 'qoder', 'iflow', 'kilo', 'kode'] as const;
const SKILL_TOOLS = ['claude', 'gemini', 'codex'] as const;
const isToolTab = (tab: string): boolean => (TOOL_NAMES as readonly string[]).includes(tab);
const isSkillTool = (tab: string): boolean => (SKILL_TOOLS as readonly string[]).includes(tab);

// Shared badge style for model buttons
const badgeBaseStyle: React.CSSProperties = {
    position: 'absolute',
    top: '-8px',
    right: '0px',
    color: 'white',
    fontSize: '10px',
    padding: '1px 5px',
    borderRadius: '4px',
    fontWeight: 'bold',
    zIndex: 10,
    transform: 'scale(0.85)',
    boxShadow: '0 1px 3px rgba(0,0,0,0.2)'
};

// Reusable markdown link component
const MarkdownLink = ({ node, ...props }: any) => (
    <a
        {...props}
        onClick={(e: React.MouseEvent) => {
            e.preventDefault();
            if (props.href) BrowserOpenURL(props.href);
        }}
        style={{ cursor: 'pointer', color: '#3b82f6', textDecoration: 'underline' }}
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
        "title": "AICoder",
        "about": "About",
        "cs146s": "Course",
        "introVideo": "Beginner",
        "thanks": "Thanks",
        "hide": "Hide",
        "launch": "Start Coding",
        "project": "Project",
        "projectDir": "Project Directory",
        "change": "Change",
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
        "qoder": "Qoder CLI",
        "qoderDesc": "Qoder AI Programming Assistant",
        "iflow": "iFlow CLI",
        "iflowDesc": "iFlow AI Programming Assistant",
        "kilo": "Kilo Code CLI",
        "kiloDesc": "Kilo Code AI Programming Assistant",
        "kode": "Kode CLI",
        "kodeDesc": "Kode AI Programming Assistant",
        "bugReport": "Problem Feedback",
        "businessCooperation": "Business: WeChat znsoft",
        "original": "Original",
        "message": "Message",
        "tutorial": "Tutorial",
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
        "startupTitle": "Welcome to AICoder",
        "showMore": "Show More",
        "showLess": "Show Less",
        "installLog": "View Log",
        "installLogTitle": "Installation Logs",
        "sendLog": "Send Log",
        "sendLogSubject": "AICoder Environment Log",
        "confirmDelete": "Confirm Delete",
        "confirmDeleteMessage": "Are you sure you want to delete provider \"{name}\"?",
        "confirmSendLog": "Confirm Send",
        "confirmSendLogMessage": "No errors detected in logs. Send anyway?",
        "cancel": "Cancel",
        "confirm": "Confirm",
        "slogan": "AI programmers get the job!",
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
        "browse": "Browse",
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
        "isLatestVersion": "Already up to date",
        "billing": "Billing",
        "placeholderName": "e.g., Frontend Design",
        "placeholderDesc": "Description...",
        "placeholderAddress": "@anthropics/...",
        "placeholderZip": "Select .zip file",
        "cannotDeleteSystemSkill": "System skill package cannot be deleted.",
        "systemDefault": "System Default",
        "envCheckTitle": "AICoder Environment Setup",
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
        "filterByRegion": "Filter by Region"
    },
    "zh-Hans": {
        "title": "AICoder",
        "about": "关于",
        "manual": "文档指南",
        "cs146s": "在线课程",
        "introVideo": "入门视频",
        "thanks": "鸣谢",
        "hide": "隐藏",
        "launch": "开始编程",
        "project": "项目",
        "projectDir": "项目目录",
        "change": "更改",
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
        "runnerStatus": "当前环境",
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
        "qoder": "Qoder CLI",
        "qoderDesc": "Qoder AI 辅助编程",
        "iflow": "iFlow CLI",
        "iflowDesc": "iFlow AI 辅助编程",
        "kilo": "Kilo Code CLI",
        "kiloDesc": "Kilo Code AI 辅助编程",
        "kode": "Kode CLI",
        "kodeDesc": "Kode AI 辅助编程",
        "bugReport": "问题反馈",
        "businessCooperation": "商业合作：微信 znsoft",
        "original": "原厂",
        "message": "消息",
        "tutorial": "教程",
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
        "startupTitle": "欢迎使用 AICoder",
        "showMore": "更多",
        "showLess": "收起",
        "installLog": "查看日志",
        "installLogTitle": "环境检查与安装日志",
        "sendLog": "发送日志",
        "sendLogSubject": "AICoder环境安装日志",
        "confirmDelete": "确认删除",
        "confirmDeleteMessage": "确定要删除服务商 \"{name}\" 吗？",
        "confirmSendLog": "确认发送",
        "confirmSendLogMessage": "日志中没有检测到错误，是否仍要发送日志？",
        "cancel": "取消",
        "confirm": "确定",
        "slogan": "会AI编程者得工作！",
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
        "browse": "浏览",
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
        "isLatestVersion": "已是最新版本",
        "billing": "计费",
        "placeholderName": "例如：前端设计",
        "placeholderDesc": "描述...",
        "placeholderAddress": "@anthropics/...",
        "placeholderZip": "选择 .zip 文件",
        "cannotDeleteSystemSkill": "系统技能包不能删除。",
        "systemDefault": "系统默认",
        "envCheckTitle": "AICoder 运行环境检测安装",
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
        "filterByRegion": "按地区筛选"
    },
    "zh-Hant": {
        "title": "AICoder",
        "about": "關於",
        "manual": "文檔指南",
        "cs146s": "線上課程",
        "introVideo": "入門視頻",
        "thanks": "鳴謝",
        "hide": "隱藏",
        "launch": "開始編程",
        "project": "專案",
        "projectDir": "專案目錄",
        "change": "變更",
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
        "qoder": "Qoder CLI",
        "qoderDesc": "Qoder AI 輔助編程",
        "iflow": "iFlow CLI",
        "iflowDesc": "iFlow AI 輔助編程",
        "kilo": "Kilo Code CLI",
        "kiloDesc": "Kilo Code AI 輔助編程",
        "kode": "Kode CLI",
        "kodeDesc": "Kode AI 輔助編程",
        "bugReport": "問題反饋",
        "businessCooperation": "商業合作：微信 znsoft",
        "original": "原廠",
        "message": "消息",
        "tutorial": "教程",
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
        "startupTitle": "歡迎使用 AICoder",
        "showMore": "更多",
        "showLess": "收起",
        "installLog": "查看日誌",
        "installLogTitle": "環境檢查與安裝日誌",
        "sendLog": "發送日誌",
        "sendLogSubject": "AICoder環境安裝日誌",
        "confirmDelete": "確認刪除",
        "confirmDeleteMessage": "確定要刪除服務商 \"{name}\" 嗎？",
        "confirmSendLog": "確認發送",
        "confirmSendLogMessage": "日誌中沒有檢測到錯誤，是否仍要發送日誌？",
        "cancel": "取消",
        "confirm": "確定",
        "slogan": "會AI編程者得工作！",
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
        "browse": "瀏覽",
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
        "isLatestVersion": "已是最新版本",
        "billing": "計費",
        "placeholderName": "例如：前端設計",
        "placeholderDesc": "描述...",
        "placeholderAddress": "@anthropics/...",
        "placeholderZip": "選擇 .zip 文件",
        "cannotDeleteSystemSkill": "系統技能包不能刪除。",
        "systemDefault": "系統默認",
        "envCheckTitle": "AICoder 運行環境檢測安裝",
        "selectProvider": "選擇服務商",
        "knownProviders": "已知服務商",
        "providerList": "服務商列表",
        "selectProviderTitle": "選擇 API 服務商",
        "chinaProviders": "國內服務商",
        "globalProviders": "國外服務商",
        "allProviders": "全部服務商",
        "filterByRegion": "按地區篩選"
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
        return <div style={{ padding: '15px', color: '#6b7280' }}>Loading configuration...</div>;
    }

    const getBadge = (model: any): { bg: string; label: string } | null => {
        const name = model.model_name.toLowerCase();
        if (model.model_name === "Original") return { bg: '#3b82f6', label: t("originalFlag") };
        if (name.includes("glm") || name.includes("kimi") || name.includes("doubao") || name.includes("minimax"))
            return { bg: '#ec4899', label: t("monthly") };
        if (name.includes("deepseek")) return { bg: '#f59e0b', label: t("premium") };
        if (name.includes("xiaomi")) return { bg: '#f59e0b', label: t("bigSpender") };
        if (model.is_custom) return { bg: '#9ca3af', label: t("customized") };
        if (["aicodemirror", "aigocode", "noin.ai", "gaccode", "chatfire", "coderelay"].some(p => name.includes(p)))
            return { bg: '#14b8a6', label: t("forward") };
        return null;
    };

    return (
        <div style={{
            backgroundColor: '#f8faff',
            padding: '10px 12px',
            borderRadius: '12px',
            border: '1px solid rgba(96, 165, 250, 0.1)',
            marginBottom: '10px'
        }}>
            <div className="model-switcher" style={{
                display: 'grid',
                gridTemplateColumns: 'repeat(3, 1fr)',
                gap: '8px',
                width: '100%',
                paddingTop: '6px',
                paddingBottom: '6px',
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
                                minWidth: '94px',
                                padding: '3px 4px',
                                fontSize: '0.75rem',
                                borderBottom: (model.api_key && model.api_key.trim() !== "") ? '3px solid #60a5fa' : '1px solid var(--border-color)',
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
    const [config, setConfig] = useState<main.AppConfig | null>(null);
    const [navTab, setNavTab] = useState<string>("claude");
    const [bbsContent, setBbsContent] = useState<string>("");
    const [tutorialContent, setTutorialContent] = useState<string>("");
    const [thanksContent, setThanksContent] = useState<string>(""); // New state for thanks content
    const [showThanksModal, setShowThanksModal] = useState<boolean>(false); // New state for thanks modal
    const [refreshStatus, setRefreshStatus] = useState<string>("");
    const [lastUpdateTime, setLastUpdateTime] = useState<string>("");
    const [refreshKey, setRefreshKey] = useState<number>(0);
    const [activeTool, setActiveTool] = useState<string>("claude");
    const [status, setStatus] = useState("");
    const [activeTab, setActiveTab] = useState(0);
    const [tabStartIndex, setTabStartIndex] = useState(0);
    const [installLocation, setInstallLocation] = useState<'user' | 'project'>('user');
    const [installProject, setInstallProject] = useState<string>("");
    const [isBatchInstalling, setIsBatchInstalling] = useState(false);
    const [isMarketplaceInstalling, setIsMarketplaceInstalling] = useState(false);
    const [isLoading, setIsLoading] = useState(true);
    const [isManualCheck, setIsManualCheck] = useState(false);
    const [showStartupPopup, setShowStartupPopup] = useState(false);
    const [pythonEnvironments, setPythonEnvironments] = useState<any[]>([]);
    const [envCheckInterval, setEnvCheckInterval] = useState<number>(7);

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
    const [proxyEditMode, setProxyEditMode] = useState<'global' | 'project'>('global');

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
    const [toastMessage, setToastMessage] = useState<string>("");
    const [showToast, setShowToast] = useState(false);

    const [skills, setSkills] = useState<main.Skill[]>([]);
    const [showAddSkillModal, setShowAddSkillModal] = useState(false);
    const [newSkillName, setNewSkillName] = useState("");
    const [newSkillDesc, setNewSkillDesc] = useState("");
    const [newSkillType, setNewSkillType] = useState("address");
    const [newSkillValue, setNewSkillValue] = useState("");
    const [selectedSkill, setSelectedSkill] = useState<string | null>(null);
    const [skillContextMenu, setSkillContextMenu] = useState<{ x: number, y: number, visible: boolean, skillName: string | null }>({
        x: 0, y: 0, visible: false, skillName: null
    });

    const [contextMenu, setContextMenu] = useState<{ x: number, y: number, visible: boolean, target: HTMLInputElement | null }>({
        x: 0, y: 0, visible: false, target: null
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

    const handleContextMenu = (e: React.MouseEvent, target: HTMLInputElement) => {
        e.preventDefault();
        setContextMenu({
            x: e.clientX,
            y: e.clientY,
            visible: true,
            target: target
        });
    };

    const closeContextMenu = () => {
        setContextMenu({ ...contextMenu, visible: false });
    };

    const showToastMessage = (message: string, duration: number = 3000) => {
        setToastMessage(message);
        setShowToast(true);
        setTimeout(() => {
            setShowToast(false);
        }, duration);
    };

    const handleShowThanks = async () => {
        try {
            const content = await ReadThanks();
            setThanksContent(content);
            setShowThanksModal(true);
        } catch (err) {
            console.error("Failed to read thanks content:", err);
            showToastMessage(t("refreshFailed") + err, 5000);
        }
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

        const fileName = isWindows ? "AICoder-Setup.exe" : "AICoder-Universal.pkg";

        try {
            const path = await DownloadUpdate(downloadUrl, fileName);
            setInstallerPath(path);
        } catch (err: any) {
            console.error("Download error:", err);
            // Error is handled by the event listener
        }
    };

    const handleCancelDownload = () => {
        const fileName = isWindows ? "AICoder-Setup.exe" : "AICoder-Universal.pkg";
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
            closeContextMenu();
            setSkillContextMenu(prev => ({ ...prev, visible: false }));
        };
        window.addEventListener('click', handleClick);
        return () => window.removeEventListener('click', handleClick);
    }, [contextMenu, skillContextMenu]);

    const getClipboardText = async () => {
        try {
            const text = await ClipboardGetText();
            return text || "";
        } catch (err) {
            console.error("ClipboardGetText failed:", err);
            // Browser API fallback if backend fails
            try {
                if (navigator.clipboard && navigator.clipboard.readText) {
                    return await navigator.clipboard.readText();
                }
            } catch (e) { }
            return "";
        }
    };

    const handleContextAction = async (action: string) => {
        const target = contextMenu.target;
        if (!target) return;

        target.focus();

        switch (action) {
            case 'selectAll':
                target.select();
                break;
            case 'copy':
                document.execCommand('copy');
                break;
            case 'cut':
                document.execCommand('cut');
                break;
            case 'paste':
                const text = await getClipboardText();
                if (text) {
                    // Modern approach using setRangeText if supported, or manual
                    const start = target.selectionStart || 0;
                    const end = target.selectionEnd || 0;
                    const val = target.value;
                    const newVal = val.substring(0, start) + text + val.substring(end);

                    // Generic React-compatible input update
                    const nativeInputValueSetter = Object.getOwnPropertyDescriptor(window.HTMLInputElement.prototype, "value")?.set;
                    if (nativeInputValueSetter) {
                        nativeInputValueSetter.call(target, newVal);
                    } else {
                        target.value = newVal;
                    }
                    const event = new Event('input', { bubbles: true });
                    target.dispatchEvent(event);
                }
                break;
        }
        closeContextMenu();
    };

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
            ResizeWindow(657, 440);
            setIsLoading(false);
            setIsManualCheck(false);
        };

        EventsOn("env-log", logHandler);
        EventsOn("env-check-done", doneHandler);
        EventsOn("show-env-logs", () => {
            setEnvLogs([]);
            setShowLogs(true);
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
            setConfig(cfg);

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
                // Default to message tab on startup as requested
                const tool = "message";
                setNavTab(tool);

                // Keep track of the last active tool for settings/launch logic
                const lastActiveTool = cfg.active_tool || "claude";
                if (isToolTab(lastActiveTool)) {
                    setActiveTool(lastActiveTool);
                }

                ReadBBS().then(content => setBbsContent(content)).catch(err => console.error(err));

                const toolCfg = (cfg as any)[lastActiveTool];
                if (toolCfg && toolCfg.models) {
                    const idx = toolCfg.models.findIndex((m: any) => m.model_name === toolCfg.current_model);
                    if (idx !== -1) setActiveTab(idx);

                    // Check if any model has an API key configured for the active tool
                    if (isToolTab(lastActiveTool)) {
                        const hasAnyApiKey = toolCfg.models.some((m: any) => m.api_key && m.api_key.trim() !== "");
                        if (!hasAnyApiKey) {
                            setShowModelSettings(true);
                        }
                    }
                }
            }
        }).catch(err => {
            setStatus("Error loading config: " + err);
        });

        // Listen for external config changes (e.g. from Tray)
        const handleConfigChange = (cfg: main.AppConfig) => {
            setConfig(cfg);
            // Sync with tray menu changes
            const tool = cfg.active_tool || "message";
            setNavTab(tool);
            if (tool === 'claude' || tool === 'gemini' || tool === 'codex' || tool === 'opencode' || tool === 'codebuddy' || tool === 'iflow' || tool === 'kilo' || tool === 'kode') {
                setActiveTool(tool);
                const toolCfg = (cfg as any)[tool];
                if (toolCfg && toolCfg.models) {
                    const idx = toolCfg.models.findIndex((m: any) => m.model_name === toolCfg.current_model);
                    if (idx !== -1) setActiveTab(idx);
                }
            }
        };
        EventsOn("config-changed", handleConfigChange);
        EventsOn("config-updated", handleConfigChange);

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

        return () => {
            EventsOff("env-log");
            EventsOff("env-check-done");
            EventsOff("download-progress");
            EventsOff("config-changed");
            EventsOff("config-updated");
            EventsOff("tool-checking");
            EventsOff("tool-installing");
            EventsOff("tool-updating");
            EventsOff("tool-installed");
            EventsOff("tool-updated");
            EventsOff("tools-install-done");
        };
    }, []);

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
            setShowModelSettings(false);
            ReadBBS().then(content => setBbsContent(content)).catch(err => console.error(err));
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
            const newConfig = new main.AppConfig({ ...config, active_tool: tool });
            setConfig(newConfig);
            SaveConfig(newConfig);

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

    const t = (key: string) => {
        return translations[lang][key] || translations["en"][key] || key;
    };

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
            return 'openai'; // codex, opencode, codebuddy, qoder, iflow, kilo, kode
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
        } else if (tool === "opencode" || tool === "codebuddy" || tool === "qoder" || tool === "iflow" || tool === "kilo" || tool === "kode") {
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

            const body = `Product: AICoder
Version: ${APP_VERSION}

System Information:
OS: ${sysInfo.os}
OS Version: ${sysInfo.os_version}
Architecture: ${sysInfo.arch}

${instruction}`;

            const mailtoLink = `mailto:znsoft@163.com?subject=${encodeURIComponent(subject)}&body=${encodeURIComponent(body)}`;

            await OpenSystemUrl(mailtoLink);
        } catch (e) {
            console.error("Failed to pack/send log:", e);
            alert("Failed to send log: " + e);
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
                backgroundColor: '#fff',
                padding: '20px',
                textAlign: 'center',
                boxSizing: 'border-box',
                borderRadius: '12px',
                border: '1px solid rgba(0, 0, 0, 0.15)',
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
                    background: 'linear-gradient(to right, #60a5fa, #a855f7, #ec4899)',
                    WebkitBackgroundClip: 'text',
                    WebkitTextFillColor: 'transparent',
                    marginBottom: '20px',
                    display: 'inline-block',
                    fontWeight: 'bold'
                }}>{t("envCheckTitle")}</h2>
                <div style={{ width: '100%', height: '4px', backgroundColor: '#e2e8f0', borderRadius: '2px', overflow: 'hidden', marginBottom: '15px' }}>
                    <div style={{
                        width: '50%',
                        height: '100%',
                        backgroundColor: '#60a5fa',
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
                            color: '#4b5563',
                            backgroundColor: '#fffdfa',
                            border: '1px solid #e2e8f0',
                            borderRadius: '8px',
                            resize: 'none',
                            outline: 'none',
                            marginBottom: '10px'
                        }}
                    />
                ) : (
                    <div style={{ fontSize: '0.9rem', color: '#6b7280', marginBottom: '15px', height: '20px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {envLogs.length > 0 ? envLogs[envLogs.length - 1] : t("initializing")}
                    </div>
                )}

                <div style={{ display: 'flex', gap: '15px', alignItems: 'center' }}>
                    <button
                        onClick={() => setShowLogs(!showLogs)}
                        style={{
                            background: 'none',
                            border: 'none',
                            color: '#60a5fa',
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
                            }} className="btn-hide" style={{ borderColor: '#60a5fa', color: '#60a5fa', padding: '4px 12px' }}>
                                {t("close")}
                            </button>
                        ) : (
                            <button onClick={Quit} className="btn-hide" style={{ borderColor: '#ef4444', color: '#ef4444', padding: '4px 12px' }}>
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

    return (
        <div id="App">
            <div style={{
                height: '30px',
                width: '180px',
                position: 'absolute',
                top: 0,
                left: 0,
                zIndex: 999,
                '--wails-draggable': 'drag'
            } as any}></div>

            <div className="sidebar" style={{ '--wails-draggable': 'no-drag', flexDirection: 'row', padding: 0, width: '180px' } as any}>
                {/* Left Navigation Strip */}
                <div style={{
                    width: '60px',
                    borderRight: '1px solid var(--border-color)',
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    padding: '10px 0',
                    backgroundColor: '#f8fafc',
                    flexShrink: 0
                }}>
                    <div className="sidebar-header" style={{ padding: '0 0 10px 0', justifyContent: 'center', width: '100%' }}>
                        <img src={appIcon} alt="Logo" className="sidebar-logo" style={{ width: '28px', height: '28px' }} />
                    </div>
                    <div style={{
                        fontSize: '0.7rem',
                        fontWeight: 'bold',
                        textAlign: 'center',
                        marginBottom: '15px',
                        background: 'linear-gradient(to right, #60a5fa, #a855f7, #ec4899)',
                        WebkitBackgroundClip: 'text',
                        WebkitTextFillColor: 'transparent',
                        display: 'inline-block',
                        width: '100%'
                    }}>AICoder</div>

                    <div
                        className={`sidebar-item ${navTab === 'message' ? 'active' : ''}`}
                        onClick={() => switchTool('message')}
                        style={{ flexDirection: 'column', padding: '10px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'message' ? '3px solid var(--primary-color)' : '3px solid transparent', justifyContent: 'center' }}
                        title={t("message")}
                    >
                        <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem' }}>💬</span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{t("message")}</span>
                    </div>
                    <div
                        className={`sidebar-item ${navTab === 'tutorial' ? 'active' : ''}`}
                        onClick={() => switchTool('tutorial')}
                        style={{ flexDirection: 'column', padding: '10px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'tutorial' ? '3px solid var(--primary-color)' : '3px solid transparent', justifyContent: 'center' }}
                        title={t("tutorial")}
                    >
                        <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem' }}>📚</span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{t("tutorial")}</span>
                    </div>
                    <div
                        className={`sidebar-item ${navTab === 'api-store' ? 'active' : ''}`}
                        onClick={() => switchTool('api-store')}
                        style={{ flexDirection: 'column', padding: '10px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'api-store' ? '3px solid var(--primary-color)' : '3px solid transparent', justifyContent: 'center' }}
                        title={t("apiStore")}
                    >
                        <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem' }}>🛒</span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{t("apiStore")}</span>
                    </div>

                    <div style={{ flex: 1 }}></div>

                    <div
                        className={`sidebar-item ${navTab === 'skills' ? 'active' : ''}`}
                        onClick={() => switchTool('skills')}
                        style={{ flexDirection: 'column', padding: '10px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'skills' ? '3px solid var(--primary-color)' : '3px solid transparent', justifyContent: 'center' }}
                        title={t("skills")}
                    >
                        <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem' }}>🛠️</span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{t("skills")}</span>
                    </div>

                    <div
                        className={`sidebar-item ${navTab === 'settings' ? 'active' : ''}`}
                        onClick={() => switchTool('settings')}
                        style={{ flexDirection: 'column', padding: '10px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'settings' ? '3px solid var(--primary-color)' : '3px solid transparent', justifyContent: 'center' }}
                        title={t("settings")}
                    >
                        <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem' }}>⚙️</span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{t("settings")}</span>
                    </div>
                    <div
                        className={`sidebar-item ${navTab === 'about' ? 'active' : ''}`}
                        onClick={() => switchTool('about')}
                        style={{ flexDirection: 'column', padding: '10px 0', width: '100%', gap: '4px', borderLeft: 'none', borderRight: navTab === 'about' ? '3px solid var(--primary-color)' : '3px solid transparent', justifyContent: 'center' }}
                        title={t("about")}
                    >
                        <span className="sidebar-icon" style={{ margin: 0, fontSize: '1.2rem' }}>ℹ️</span>
                        <span style={{ fontSize: '0.65rem', lineHeight: 1 }}>{t("about")}</span>
                    </div>
                </div>

                {/* Right Tool List */}
                <div style={{ flex: 1, padding: '10px', overflowY: 'auto', backgroundColor: '#fff', display: 'flex', flexDirection: 'column', justifyContent: 'center' }}>
                    <div className="tool-grid" style={{ display: 'grid', gridTemplateColumns: '1fr', gap: '4px' }}>
                        <div className={`sidebar-item ${navTab === 'claude' ? 'active' : ''}`} onClick={() => switchTool('claude')}>
                            <span className="sidebar-icon">
                                <img src={claudecodeIcon} style={{ width: '1.1em', height: '1.1em', verticalAlign: 'middle' }} alt="Claude" />
                            </span> <span>Claude Code</span>
                        </div>
                        {config?.show_gemini !== false && (
                            <div className={`sidebar-item ${navTab === 'gemini' ? 'active' : ''}`} onClick={() => switchTool('gemini')}>
                                <span className="sidebar-icon">
                                    <img src={geminiIcon} style={{ width: '1.1em', height: '1.1em', verticalAlign: 'middle' }} alt="Gemini" />
                                </span> <span>Gemini CLI</span>
                            </div>
                        )}
                        {config?.show_codex !== false && (
                            <div className={`sidebar-item ${navTab === 'codex' ? 'active' : ''}`} onClick={() => switchTool('codex')}>
                                <span className="sidebar-icon">
                                    <img src={codexIcon} style={{ width: '1.1em', height: '1.1em', verticalAlign: 'middle' }} alt="Codex" />
                                </span> <span>CodeX</span>
                            </div>
                        )}
                        {config?.show_opencode !== false && (
                            <div className={`sidebar-item ${navTab === 'opencode' ? 'active' : ''}`} onClick={() => switchTool('opencode')}>
                                <span className="sidebar-icon">
                                    <img src={opencodeIcon} style={{ width: '1.1em', height: '1.1em', verticalAlign: 'middle' }} alt="OpenCode" />
                                </span> <span>OpenCode</span>
                            </div>
                        )}
                        {config?.show_codebuddy !== false && (
                            <div className={`sidebar-item ${navTab === 'codebuddy' ? 'active' : ''}`} onClick={() => switchTool('codebuddy')}>
                                <span className="sidebar-icon">
                                    <img src={codebuddyIcon} style={{ width: '1.1em', height: '1.1em', verticalAlign: 'middle' }} alt="CodeBuddy" />
                                </span> <span>CodeBuddy</span>
                            </div>
                        )}
                        {config?.show_iflow !== false && (
                            <div className={`sidebar-item ${navTab === 'iflow' ? 'active' : ''}`} onClick={() => switchTool('iflow')}>
                                <span className="sidebar-icon">
                                    <img src={iflowIcon} style={{ width: '1.1em', height: '1.1em', verticalAlign: 'middle' }} alt="iFlow" />
                                </span> <span>iFlow CLI</span>
                            </div>
                        )}
                        {config?.show_kilo !== false && (
                            <div className={`sidebar-item ${navTab === 'kilo' ? 'active' : ''}`} onClick={() => switchTool('kilo')}>
                                <span className="sidebar-icon">
                                    <img src={kiloIcon} style={{ width: '1.1em', height: '1.1em', verticalAlign: 'middle' }} alt="Kilo Code" />
                                </span> <span>Kilo Code</span>
                            </div>
                        )}
                        {config?.show_kode !== false && (
                            <div className={`sidebar-item ${navTab === 'kode' ? 'active' : ''}`} onClick={() => switchTool('kode')}>
                                <span className="sidebar-icon">
                                    <img src={kodeIcon} style={{ width: '1.1em', height: '1.1em', verticalAlign: 'middle' }} alt="Kode CLI" />
                                </span> <span>Kode CLI</span>
                            </div>
                        )}
                        {config?.show_qoder !== false && (
                            <div className={`sidebar-item ${navTab === 'qoder' ? 'active' : ''}`} onClick={() => switchTool('qoder')}>
                                <span className="sidebar-icon">
                                    <img src={qoderIcon} style={{ width: '1.1em', height: '1.1em', verticalAlign: 'middle' }} alt="Qoder" />
                                </span> <span>Qoder CLI</span>
                            </div>
                        )}
                    </div>
                </div>
            </div>

            <div className="main-container">
                <div className="top-header" style={{ '--wails-draggable': 'no-drag' } as any}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%' }}>
                        <h2 style={{ margin: 0, fontSize: '1.1rem', color: '#60a5fa', fontWeight: 'bold', marginLeft: '20px', '--wails-draggable': 'drag', flex: 1, display: 'flex', alignItems: 'center' } as any}>
                            <span>
                                {navTab === 'message' ? t("message") :
                                    navTab === 'claude' ? 'Claude Code' :
                                        navTab === 'gemini' ? 'Gemini CLI' :
                                            navTab === 'codex' ? 'OpenAI Codex' :
                                                navTab === 'opencode' ? 'OpenCode AI' :
                                                    navTab === 'codebuddy' ? 'CodeBuddy AI' :
                                                        navTab === 'qoder' ? 'Qoder CLI' :
                                                            navTab === 'iflow' ? 'iFlow CLI' :
                                                                navTab === 'kilo' ? 'Kilo Code CLI' :
                                                                    navTab === 'kode' ? 'Kode CLI' :
                                                                        navTab === 'projects' ? t("projectManagement") :
                                                                    navTab === 'skills' ? t("skills") :
                                                                        navTab === 'tutorial' ? t("tutorial") :
                                                                            navTab === 'api-store' ? t("apiStore") :
                                                                                navTab === 'settings' ? t("globalSettings") : t("about")}
                            </span>
                            {navTab === 'message' && (
                                <>
                                    <button
                                        className="btn-link"
                                        style={{ marginLeft: '10px', fontSize: '0.8rem', padding: '4px 12px' }}
                                        onClick={async () => {
                                            try {
                                                setRefreshStatus(t("refreshing"));
                                                setBbsContent('');
                                                const startTime = Date.now();
                                                const content = await ReadBBS();
                                                const elapsed = Date.now() - startTime;
                                                const preview = content.substring(0, 50).replace(/\n/g, ' ');
                                                const now = new Date();
                                                const timeStr = `${now.getHours()}:${String(now.getMinutes()).padStart(2, '0')}:${String(now.getSeconds()).padStart(2, '0')}`;
                                                setRefreshStatus(t("refreshSuccess"));
                                                setBbsContent(content);
                                                setRefreshKey(prev => prev + 1);
                                                setLastUpdateTime(timeStr);
                                                setTimeout(() => setRefreshStatus(''), 5000);
                                            } catch (err) {
                                                setRefreshStatus(t("refreshFailed") + err);
                                                setTimeout(() => setRefreshStatus(''), 5000);
                                            }
                                        }}
                                    >
                                        {t("refreshMessage")}
                                    </button>
                                    <button
                                        className="btn-link"
                                        style={{ marginLeft: '10px', fontSize: '0.8rem', padding: '4px 12px' }}
                                        onClick={handleShowThanks}
                                    >
                                        {t("thanks")}
                                    </button>
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
                                            borderColor: '#60a5fa',
                                            color: '#60a5fa',
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
                                                borderColor: '#10b981',
                                                color: '#10b981',
                                                '--wails-draggable': 'no-drag'
                                            } as any}
                                        >
                                            {t("installSkills")}
                                        </button>
                                    )}
                                </>
                            )}
                            {navTab === 'skills' && (
                                <div style={{ display: 'flex', gap: '10px', marginLeft: '10px', alignItems: 'center' }}>
                                    <button
                                        className="btn-link"
                                        onClick={() => {
                                            setNewSkillName("");
                                            setNewSkillDesc("");
                                            // Default to zip for gemini/codex
                                            const isGeminiOrCodex = activeTool?.toLowerCase() === 'gemini' || activeTool?.toLowerCase() === 'codex';
                                            setNewSkillType(isGeminiOrCodex ? "zip" : "address");
                                            setNewSkillValue("");
                                            setShowAddSkillModal(true);
                                        }}
                                        style={{
                                            padding: '2px 8px',
                                            fontSize: '0.8rem',
                                            borderColor: '#60a5fa',
                                            color: '#60a5fa',
                                            '--wails-draggable': 'no-drag'
                                        } as any}
                                    >
                                        {t("addSkill")}
                                    </button>
                                    <button
                                        className="btn-link"
                                        disabled={!selectedSkill}
                                        style={{
                                            padding: '2px 8px',
                                            fontSize: '0.8rem',
                                            opacity: selectedSkill ? 1 : 0.5,
                                            cursor: selectedSkill ? 'pointer' : 'not-allowed',
                                            borderColor: '#ef4444',
                                            color: '#ef4444',
                                            '--wails-draggable': 'no-drag'
                                        } as any}
                                        onClick={() => {
                                            if (selectedSkill) handleDeleteSkill(selectedSkill);
                                        }}
                                    >
                                        {t("delete")}
                                    </button>
                                </div>
                            )}
                        </h2>
                        <div style={{ display: 'flex', gap: '10px', '--wails-draggable': 'no-drag', marginRight: '5px', pointerEvents: 'auto', position: 'relative', zIndex: 10000 } as any}>
                            <button
                                onMouseDown={handleWindowHide}
                                className="btn-hide"
                                style={{ '--wails-draggable': 'no-drag', pointerEvents: 'auto', cursor: 'pointer', position: 'relative', zIndex: 10001 } as any}
                            >
                                {t("hide")}
                            </button>
                        </div>
                    </div>
                </div>

                <div className={`main-content ${navTab === 'tutorial' || navTab === 'message' ? 'elegant-scrollbar' : 'no-scrollbar'} ${navTab === 'settings' || navTab === 'about' ? '' : ''}`} style={{ overflowY: 'auto', paddingBottom: '20px', '--wails-draggable': 'no-drag' } as any}>
                    {navTab === 'message' && (
                        <div style={{
                            width: '100%',
                            padding: '0 15px',
                            boxSizing: 'border-box'
                        }}>
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
                                        backgroundColor: 'rgba(224, 242, 254, 0.95)',
                                        borderRadius: '16px',
                                        color: '#0369a1',
                                        fontSize: '0.75rem',
                                        fontWeight: 'bold',
                                        boxShadow: '0 4px 6px rgba(0,0,0,0.1)',
                                        backdropFilter: 'blur(4px)',
                                        animation: 'fadeIn 0.3s ease-out'
                                    }}>
                                        {refreshStatus}
                                    </div>
                                )}

                                {lastUpdateTime && !refreshStatus && (
                                    <div style={{
                                        position: 'absolute',
                                        top: '0',
                                        right: '0',
                                        zIndex: 90,
                                        padding: '4px 10px',
                                        backgroundColor: 'rgba(240, 253, 244, 0.9)',
                                        borderRadius: '4px',
                                        color: '#15803d',
                                        fontSize: '0.7rem',
                                        backdropFilter: 'blur(2px)'
                                    }}>
                                        {t("lastUpdate")}{lastUpdateTime}
                                    </div>
                                )}
                            </div>

                            <div className="markdown-content" style={{
                                backgroundColor: '#fff',
                                padding: '20px',
                                borderRadius: '8px',
                                border: '1px solid var(--border-color)',
                                fontFamily: 'inherit',
                                fontSize: '0.75rem',
                                lineHeight: '1.6',
                                color: '#374151',
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
                                    {bbsContent}
                                </ReactMarkdown>
                            </div>
                        </div>
                    )}
                    {navTab === 'tutorial' && (
                        <div style={{
                            width: '100%',
                            padding: '0 15px',
                            boxSizing: 'border-box'
                        }}>
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
                                        backgroundColor: 'rgba(224, 242, 254, 0.95)',
                                        borderRadius: '16px',
                                        color: '#0369a1',
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
                                backgroundColor: '#fff',
                                padding: '20px',
                                borderRadius: '8px',
                                border: '1px solid var(--border-color)',
                                fontFamily: 'inherit',
                                fontSize: '0.75rem',
                                lineHeight: '1.6',
                                color: '#374151',
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
                                        { name: '智谱', url: 'https://bigmodel.cn/glm-coding', isRelay: false, hasSubscription: true },
                                        { name: '月之暗面', url: 'https://www.kimi.com/membership/pricing?from=upgrade_plan&track_id=1d2446f5-f45f-4ae5-961e-c0afe936a115', isRelay: false, hasSubscription: true },
                                        { name: '豆包', url: 'https://www.volcengine.com/activity/codingplan', isRelay: false, hasSubscription: true },
                                        { name: 'MiniMax', url: 'https://platform.minimaxi.com/user-center/payment/coding-plan', isRelay: false, hasSubscription: true },
                                        { name: 'DeepSeek', url: 'https://platform.deepseek.com/api_keys', isRelay: false, hasSubscription: false, isBilling: true },
                                        { name: '小米', url: 'https://platform.xiaomimimo.com/#/console/api-keys', isRelay: false, hasSubscription: false, isBilling: true },
                                        { name: '摩尔线程', url: 'https://code.mthreads.com/', isRelay: false, hasSubscription: true },
                                        { name: '快手', url: 'https://www.streamlake.com/marketing/coding-plan', isRelay: false, hasSubscription: true },
                                        { name: '阿里云', url: 'https://coding.dashscope.aliyuncs.com/', isRelay: false, hasSubscription: true },
                                    ].map((provider, index) => (
                                        <div
                                            key={index}
                                            style={{
                                                backgroundColor: '#fff',
                                                border: '1px solid var(--border-color)',
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
                                                e.currentTarget.style.boxShadow = '0 4px 12px rgba(0,0,0,0.1)';
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
                                                    backgroundColor: '#3b82f6',
                                                    color: '#fff',
                                                    padding: '3px 10px',
                                                    borderRadius: '4px',
                                                    fontSize: '0.65rem',
                                                    fontWeight: 'bold',
                                                    boxShadow: '0 2px 4px rgba(0,0,0,0.15)'
                                                }}>
                                                    {t("relayService")}
                                                </div>
                                            )}
                                            {provider.hasSubscription && (
                                                <div style={{
                                                    position: 'absolute',
                                                    top: '-6px',
                                                    right: '-6px',
                                                    backgroundColor: '#ec4899',
                                                    color: '#fff',
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
                                                    backgroundColor: '#f59e0b',
                                                    color: '#fff',
                                                    padding: '3px 10px',
                                                    borderRadius: '4px',
                                                    fontSize: '0.65rem',
                                                    fontWeight: 'bold',
                                                    boxShadow: '0 2px 4px rgba(0,0,0,0.15)'
                                                }}>
                                                    {t("billing")}
                                                </div>
                                            )}
                                            <div style={{
                                                fontSize: '0.85rem',
                                                fontWeight: 600,
                                                color: '#3b82f6',
                                                marginBottom: '8px'
                                            }}>
                                                {provider.name}
                                            </div>
                                            <div style={{
                                                fontSize: '0.85rem',
                                                color: '#6b7280'
                                            }}>
                                                🛒
                                            </div>
                                        </div>
                                    ))}
                                </div>
                            </div>
                        </div>
                    )}
                    {navTab === 'skills' && (
                        <div style={{ display: 'flex', flexDirection: 'column', height: '100%' }}>
                            <div style={{ flex: 1, overflowY: 'auto', padding: '20px' }}>
                                {skills.length === 0 ? (
                                    <div style={{ textAlign: 'center', color: '#6b7280', marginTop: '40px' }}>{t("noSkills")}</div>
                                ) : (
                                    <div className="skills-list" style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
                                        {skills.map((skill, idx) => (
                                            <div
                                                key={idx}
                                                onClick={() => setSelectedSkill(skill.name)}
                                                onContextMenu={(e) => handleSkillContext(e, skill.name)}
                                                title={skill.description}
                                                style={{
                                                    border: selectedSkill === skill.name ? '2px solid var(--primary-color)' : '1px solid var(--border-color)',
                                                    borderRadius: '8px',
                                                    padding: '10px',
                                                    backgroundColor: selectedSkill === skill.name ? '#f0f9ff' : '#fff',
                                                    display: 'flex',
                                                    flexDirection: 'column',
                                                    gap: '4px',
                                                    cursor: 'default',
                                                    transition: 'all 0.2s',
                                                    position: 'relative',
                                                    marginTop: (skill.name === "Claude Official Documentation Skill Package" || skill.name === "超能力技能包") ? '6px' : '0px'
                                                }}
                                            >
                                                {(skill.name === "Claude Official Documentation Skill Package" || skill.name === "超能力技能包") && (
                                                    <span style={{
                                                        position: 'absolute',
                                                        top: '-8px',
                                                        right: '4px',
                                                        backgroundColor: '#3b82f6',
                                                        color: 'white',
                                                        fontSize: '10px',
                                                        padding: '1px 6px',
                                                        borderRadius: '4px',
                                                        fontWeight: 'bold',
                                                        zIndex: 10,
                                                        boxShadow: '0 1px 3px rgba(0,0,0,0.2)',
                                                        transform: 'scale(0.9)'
                                                    }}>
                                                        {t("systemDefault")}
                                                    </span>
                                                )}
                                                <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                                                    <span style={{ fontWeight: 'bold', fontSize: '1.05rem' }}>{skill.name}</span>
                                                </div>
                                                <div style={{ fontSize: '0.75rem', color: '#9ca3af', display: 'flex', gap: '10px', marginTop: '2px' }}>
                                                    <span style={{ backgroundColor: '#f3f4f6', padding: '2px 6px', borderRadius: '4px' }}>
                                                        {skill.type === 'address' ? t("skillAddress") : t("skillZip")}
                                                    </span>
                                                    <span style={{ fontFamily: 'monospace', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', maxWidth: '300px' }}>
                                                        {skill.value}
                                                    </span>
                                                </div>
                                            </div>
                                        ))}
                                    </div>
                                )}
                            </div>

                            {skillContextMenu.visible && (
                                <div style={{
                                    position: 'fixed',
                                    top: skillContextMenu.y,
                                    left: skillContextMenu.x,
                                    backgroundColor: 'white',
                                    border: '1px solid #e2e8f0',
                                    borderRadius: '8px',
                                    boxShadow: '0 4px 12px rgba(0,0,0,0.1)',
                                    zIndex: 3000,
                                    padding: '5px 0',
                                    minWidth: '120px'
                                }}>
                                    <div className="context-menu-item" onClick={() => {
                                        if (skillContextMenu.skillName) handleDeleteSkill(skillContextMenu.skillName);
                                        setSkillContextMenu({ ...skillContextMenu, visible: false });
                                    }}>
                                        {t("delete")}
                                    </div>
                                </div>
                            )}

                            {showAddSkillModal && (
                                <div className="modal-backdrop">
                                    <div className="modal-content" style={{ width: '90%', maxWidth: '500px' }}>
                                        <div className="modal-header">
                                            <h3>{t("addSkill")}</h3>
                                            <button onClick={() => setShowAddSkillModal(false)} className="btn-close">&times;</button>
                                        </div>
                                        <div className="modal-body">
                                            <div className="form-group">
                                                <label>{t("skillType")}</label>
                                                <div style={{ display: 'flex', gap: '20px', marginTop: '5px' }}>
                                                    {(activeTool?.toLowerCase() !== 'gemini' && activeTool?.toLowerCase() !== 'codex') && (
                                                        <label style={{ display: 'flex', alignItems: 'center', gap: '5px', cursor: 'pointer' }}>
                                                            <input
                                                                type="radio"
                                                                checked={newSkillType === 'address'}
                                                                onChange={() => setNewSkillType('address')}
                                                            /> {t("skillAddress")}
                                                        </label>
                                                    )}
                                                    <label style={{ display: 'flex', alignItems: 'center', gap: '5px', cursor: 'pointer' }}>
                                                        <input
                                                            type="radio"
                                                            checked={newSkillType === 'zip'}
                                                            onChange={() => setNewSkillType('zip')}
                                                        /> {t("skillZip")}
                                                    </label>
                                                </div>
                                            </div>

                                            <div className="form-group">
                                                <label>{t("skillName")}</label>
                                                <input
                                                    type="text"
                                                    className="form-input"
                                                    value={newSkillName}
                                                    onChange={(e) => setNewSkillName(e.target.value)}
                                                    placeholder={t("placeholderName")}
                                                />
                                            </div>

                                            <div className="form-group">
                                                <label>{t("skillDesc")}</label>
                                                <textarea
                                                    className="form-input"
                                                    value={newSkillDesc}
                                                    onChange={(e) => setNewSkillDesc(e.target.value)}
                                                    rows={3}
                                                    placeholder={t("placeholderDesc")}
                                                />
                                            </div>

                                            <div className="form-group">
                                                <label>{newSkillType === 'address' ? t("skillAddress") : t("skillPath")}</label>
                                                {newSkillType === 'address' ? (
                                                    <input
                                                        type="text"
                                                        className="form-input"
                                                        value={newSkillValue}
                                                        onChange={(e) => setNewSkillValue(e.target.value)}
                                                        placeholder={t("placeholderAddress")}
                                                    />
                                                ) : (
                                                    <div style={{ display: 'flex', gap: '10px' }}>
                                                        <input
                                                            type="text"
                                                            className="form-input"
                                                            style={{ flex: 1, minWidth: '0' }}
                                                            value={newSkillValue}
                                                            readOnly
                                                            placeholder={t("placeholderZip")}
                                                        />
                                                        <button
                                                            className="btn-secondary"
                                                            style={{ flexShrink: 0 }}
                                                            onClick={async () => {
                                                                const path = await SelectSkillFile();
                                                                if (path) setNewSkillValue(path);
                                                            }}
                                                        >
                                                            {t("browse")}
                                                        </button>
                                                    </div>
                                                )}
                                            </div>
                                        </div>
                                        <div className="modal-footer">
                                            <button className="btn-secondary" onClick={() => setShowAddSkillModal(false)}>{t("cancel")}</button>
                                            <button className="btn-primary" onClick={async () => {
                                                if (!newSkillName || !newSkillValue) {
                                                    showToastMessage(t("skillRequiredError"));
                                                    return;
                                                }
                                                try {
                                                    await AddSkill(newSkillName, newSkillDesc, newSkillType, newSkillValue, activeTool);
                                                    setShowAddSkillModal(false);
                                                    const list = await ListSkills(activeTool);
                                                    setSkills(list || []);
                                                    showToastMessage(t("skillAdded"));
                                                } catch (err) {
                                                    showToastMessage(t("skillAddError").replace("{error}", err as string));
                                                }
                                            }}>{t("confirm")}</button>
                                        </div>
                                    </div>
                                </div>
                            )}
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
                        <div style={{ padding: '5px 10px' }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: '10px', marginBottom: '8px' }}>
                                <button
                                    onClick={() => switchTool(activeTool)}
                                    style={{
                                        background: 'none',
                                        border: 'none',
                                        cursor: 'pointer',
                                        fontSize: '1.2rem',
                                        color: 'var(--primary-color)',
                                        padding: '0 4px'
                                    }}
                                    title="Back"
                                >&lt;&lt;</button>
                                <button className="btn-primary" style={{ padding: '3px 12px', fontSize: '0.85rem' }} onClick={handleAddNewProject}>{t("addNewProject")}</button>
                            </div>

                            <div style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                                {config && config.projects && config.projects.map((proj: any) => (
                                    <div key={proj.id} style={{
                                        padding: '6px 12px',
                                        backgroundColor: '#fff',
                                        borderRadius: '8px',
                                        border: '1px solid var(--border-color)',
                                        display: 'flex',
                                        flexDirection: 'row',
                                        alignItems: 'center',
                                        gap: '10px'
                                    }}>
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
                                            onContextMenu={(e) => handleContextMenu(e, e.currentTarget)}
                                            style={{ fontWeight: 'bold', border: 'none', padding: 0, fontSize: '1rem', width: '120px', flexShrink: 0 }}
                                            spellCheck={false}
                                            autoComplete="off"
                                        />                                        <div style={{ flex: 1, fontSize: '0.85rem', color: '#6b7280', backgroundColor: '#f9fafb', padding: '6px', borderRadius: '4px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                            {proj.path}
                                        </div>

                                        <div style={{ display: 'flex', gap: '10px', alignItems: 'center', flexShrink: 0 }}>

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
                                                style={{ color: '#ef4444', background: 'none', border: 'none', cursor: 'pointer', fontSize: '0.85rem' }}
                                                onClick={() => {
                                                    if (config.projects.length > 1) {
                                                        const newList = config.projects.filter((p: any) => p.id !== proj.id);
                                                        const newConfig = new main.AppConfig({ ...config, projects: newList });
                                                        if (config.current_project === proj.id) newConfig.current_project = newList[0].id;
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
                        </div>
                    )}

                    {navTab === 'settings' && (
                        <div style={{ padding: '10px' }}>
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '20px', marginBottom: '15px' }}>
                                <div className="form-group" style={{ flex: '1', marginBottom: 0, display: 'flex', alignItems: 'center', gap: '10px' }}>
                                    <label className="form-label" style={{ marginBottom: 0, whiteSpace: 'nowrap', fontSize: '0.8rem' }}>{t("language")}</label>
                                    <select value={lang} onChange={handleLangChange} className="form-input" style={{ width: 'auto', fontSize: '0.8rem', padding: '2px 8px', height: '28px' }}>
                                        <option value="en">English</option>
                                        <option value="zh-Hans">简体中文</option>
                                        <option value="zh-Hant">繁體中文</option>
                                    </select>
                                </div>
                                <button
                                    className="btn-link"
                                    onClick={() => switchTool('projects')}
                                    style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '2px 12px', border: '1px solid var(--border-color)', height: '24px', borderRadius: '12px', fontSize: '0.7rem' }}
                                >
                                    <span>📂</span> {t("manageProjects")}
                                </button>
                                {!isWindows && (
                                    <button
                                        className="btn-link"
                                        onClick={() => {
                                            setProxyEditMode('global');
                                            setShowProxySettings(true);
                                        }}
                                        style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '2px 12px', border: '1px solid var(--border-color)', height: '24px', borderRadius: '12px', fontSize: '0.7rem' }}
                                    >
                                        <span>🌐</span> {t("proxySettings")}
                                    </button>
                                )}
                            </div>

                            <div className="form-group" style={{ marginTop: '10px', borderTop: '1px solid #f1f5f9', paddingTop: '10px' }}>
                                <h4 style={{ fontSize: '0.8rem', color: '#60a5fa', marginBottom: '12px', marginTop: 0, textTransform: 'uppercase', letterSpacing: '0.025em' }}>{lang === 'zh-Hans' ? '工具显示' : lang === 'zh-Hant' ? '工具顯示' : 'Tool Visibility'}</h4>
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
                                        <span style={{ fontSize: '0.8rem', color: '#4b5563' }}>Gemini CLI</span>
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
                                        <span style={{ fontSize: '0.8rem', color: '#4b5563' }}>OpenAI Codex</span>
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
                                        <span style={{ fontSize: '0.8rem', color: '#4b5563' }}>OpenCode AI</span>
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
                                        <span style={{ fontSize: '0.8rem', color: '#4b5563' }}>CodeBuddy</span>
                                    </label>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.show_qoder !== false}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({ ...config, show_qoder: e.target.checked });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '16px', height: '16px' }}
                                        />
                                        <span style={{ fontSize: '0.8rem', color: '#4b5563' }}>Qoder CLI</span>
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
                                        <span style={{ fontSize: '0.8rem', color: '#4b5563' }}>iFlow CLI</span>
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
                                        <span style={{ fontSize: '0.8rem', color: '#4b5563' }}>Kilo Code CLI</span>
                                    </label>
                                    <label style={{ display: 'flex', alignItems: 'center', gap: '8px', cursor: 'pointer' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.show_kode !== false}
                                            onChange={(e) => {
                                                if (config) {
                                                    const newConfig = new main.AppConfig({ ...config, show_kode: e.target.checked });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ width: '16px', height: '16px' }}
                                        />
                                        <span style={{ fontSize: '0.8rem', color: '#4b5563' }}>Kode CLI</span>
                                    </label>
                                </div>
                            </div>

                            <div className="form-group" style={{ marginTop: '10px', borderTop: '1px solid #f1f5f9', paddingTop: '10px' }}>
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
                                    <span style={{ fontSize: '0.8rem', color: '#374151' }}>{t("showWelcomePage")}</span>
                                </label>
                                <p style={{ fontSize: '0.75rem', color: '#9ca3af', marginLeft: '24px', marginTop: '4px' }}>
                                    {lang === 'zh-Hans' ? '开启后，程序启动时将显示新手教学和快速入门链接' :
                                        lang === 'zh-Hant' ? '開啟後，程序啟動時將顯示新手教學和快速入門鏈接' :
                                            'When enabled, a welcome popup with tutorial links will be shown at startup.'}
                                </p>
                            </div>

                            <div className="form-group" style={{ marginTop: '10px', borderTop: '1px solid #f1f5f9', paddingTop: '10px', display: 'flex', flexDirection: 'column', gap: '12px' }}>
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
                                        <span style={{ fontSize: '0.8rem', color: '#374151' }}>{t("pauseEnvCheck")}</span>
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
                                        <span style={{ fontSize: '0.8rem', color: '#374151' }}>{t("useWindowsTerminal")}</span>
                                    </label>
                                    )}
                                </div>
                                {config?.pause_env_check && (
                                    <div style={{ marginLeft: '24px', display: 'flex', alignItems: 'center', gap: '8px' }}>
                                        <label style={{ display: 'flex', alignItems: 'center', gap: '6px', fontSize: '0.8rem', color: '#6b7280' }}>
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
                                                    border: '1px solid #d1d5db',
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
                        </div>
                    )}

                    {navTab === 'about' && (
                        <div style={{
                            padding: '20px',
                            display: 'flex',
                            flexDirection: 'column',
                            alignItems: 'center',
                            textAlign: 'center',
                            height: '100%',
                            justifyContent: 'center',
                            boxSizing: 'border-box'
                        }}>
                            <img src={appIcon} alt="Logo" style={{ width: '64px', height: '64px', marginBottom: '15px' }} />
                            <h2 style={{
                                margin: '0 0 4px 0',
                                background: 'linear-gradient(to right, #60a5fa, #a855f7, #ec4899)',
                                WebkitBackgroundClip: 'text',
                                WebkitTextFillColor: 'transparent',
                                display: 'inline-block',
                                fontWeight: 'bold'
                            }}>RapidAI AICoder</h2>
                            <div style={{
                                fontSize: '1rem',
                                fontWeight: 'bold',
                                background: 'linear-gradient(to right, #60a5fa, #a855f7, #ec4899)',
                                WebkitBackgroundClip: 'text',
                                WebkitTextFillColor: 'transparent',
                                marginBottom: '4px',
                                display: 'inline-block'
                            }}>
                                {t("slogan")}
                            </div>
                            <br />
                            <div style={{
                                fontSize: '0.9rem',
                                fontWeight: 'bold',
                                background: 'linear-gradient(to right, #60a5fa, #a855f7, #ec4899)',
                                WebkitBackgroundClip: 'text',
                                WebkitTextFillColor: 'transparent',
                                marginBottom: '12px',
                                display: 'inline-block'
                            }}>
                                {lang === 'zh-Hans' || lang === 'zh-Hant' ? '真正的Vibe Coder只使用命令行。' : 'Real Vibe Coders only use the command line.'}
                            </div>
                            <div style={{ fontSize: '1rem', color: '#374151', marginBottom: '5px' }}>{t("version")} {APP_VERSION}</div>
                            <div style={{ fontSize: '0.9rem', color: '#9ca3af', marginBottom: '5px' }}>{t("businessCooperation")}</div>
                            <div style={{ fontSize: '0.9rem', color: '#6b7280', marginBottom: '20px' }}>{t("author")}: Dr. Daniel</div>

                            <div style={{ display: 'flex', flexDirection: 'column', gap: '12px', alignItems: 'center' }}>
                                <div style={{ display: 'flex', gap: '6px', justifyContent: 'center', flexWrap: 'wrap' }}>
                                    <button className="btn-link" style={{ fontSize: '0.75rem', padding: '2px 6px' }} onClick={() => BrowserOpenURL("https://aicoder.rapidai.tech/")}>{t("officialWebsite")}</button>
                                    <button
                                        className="btn-link"
                                        style={{ fontSize: '0.75rem', padding: '2px 6px' }}
                                        onClick={() => {
                                            setStatus(t("checkingUpdate"));
                                            CheckUpdate(APP_VERSION).then(res => {
                                                console.log("CheckUpdate result:", res);
                                                setUpdateResult(res);
                                                setIsStartupUpdateCheck(false);
                                                setShowUpdateModal(true);
                                                setStatus("");
                                            }).catch(err => {
                                                console.error("CheckUpdate error:", err);
                                                setStatus("检查更新失败: " + err);
                                                // 显示一个错误结果
                                                setUpdateResult({
                                                    has_update: false,
                                                    latest_version: "获取失败",
                                                    release_url: ""
                                                });
                                                setIsStartupUpdateCheck(false);
                                                setShowUpdateModal(true);
                                            });
                                        }}
                                    >
                                        {t("onlineUpdate")}
                                    </button>
                                    <button className="btn-link" style={{ fontSize: '0.75rem', padding: '2px 6px' }} onClick={() => setShowInstallLog(true)}>{t("installLog")}</button>
                                    <button className="btn-link" style={{ fontSize: '0.75rem', padding: '2px 6px' }} onClick={() => BrowserOpenURL("https://github.com/RapidAI/aicoder/issues/new")}>{t("bugReport")}</button>
                                    <button className="btn-link" style={{ fontSize: '0.75rem', padding: '2px 6px' }} onClick={() => BrowserOpenURL("https://github.com/RapidAI/aicoder")}>GitHub</button>
                                </div>
                            </div>
                        </div>
                    )}
                </div>

                {/* Global Action Bar (Footer) */}
                {config && isToolTab(navTab) && (
                    <div className="global-action-bar" style={{ '--wails-draggable': 'no-drag' } as any}>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: '5px', width: '100%', padding: '2px 0', '--wails-draggable': 'no-drag' } as any}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: '20px', justifyContent: 'flex-start' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                    <span style={{ fontSize: '0.75rem', color: '#9ca3af' }}>{t("runnerStatus")}:</span>
                                    <span style={{ fontSize: '0.85rem', fontWeight: 600, color: '#60a5fa', textTransform: 'capitalize' }}>{activeTool}</span>
                                    <span style={{ color: '#d1d5db' }}>|</span>
                                    <span
                                        style={{ fontSize: '0.85rem', fontWeight: 600, color: '#374151' }}
                                        title={(config as any)[activeTool].current_model === "Original" ? t("original") : (config as any)[activeTool].current_model}
                                    >
                                        {(() => {
                                            const modelName = (config as any)[activeTool].current_model === "Original" ? t("original") : (config as any)[activeTool].current_model;
                                            return modelName.length > 10 ? `${modelName.slice(0, 4)}...${modelName.slice(-4)}` : modelName;
                                        })()}
                                    </span>
                                </div>
                                {activeTool !== 'kilo' && (
                                    <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: '#6b7280' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.yolo_mode || false}
                                            onChange={(e) => {
                                                const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                                if (proj) {
                                                    const isWindows = /window/i.test(navigator.userAgent);
                                                    const newProjects = config.projects.map((p: any) => {
                                                        if (p.id === proj.id) {
                                                            const updated = { ...p, yolo_mode: e.target.checked };
                                                            // On non-Windows, yolo and admin are mutually exclusive
                                                            if (!isWindows && e.target.checked) {
                                                                updated.admin_mode = false;
                                                            }
                                                            return updated;
                                                        }
                                                        return p;
                                                    });
                                                    const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ marginRight: '6px' }}
                                        />
                                        <span>{t("yoloModeLabel")}</span>
                                        {config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.yolo_mode && (
                                            <span style={{
                                                marginLeft: '2px',
                                                backgroundColor: '#fee2e2',
                                                color: '#ef4444',
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
                                    <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: '#6b7280' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.team_mode || false}
                                            onChange={(e) => {
                                                const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                                if (proj) {
                                                    const newProjects = config.projects.map((p: any) =>
                                                        p.id === proj.id ? { ...p, team_mode: e.target.checked } : p
                                                    );
                                                    const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ marginRight: '6px' }}
                                        />
                                        <span>{t("teamModeLabel")}</span>
                                    </label>
                                )}
                                {!isWindows && (
                                    <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: '#6b7280' }}>
                                        <input
                                            type="checkbox"
                                            checked={config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.use_proxy || false}
                                            onChange={(e) => {
                                                const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                                if (proj) {
                                                    // If checking but not configured, show dialog
                                                    if (e.target.checked && !proj.proxy_host && !config?.default_proxy_host) {
                                                        setProxyEditMode('project');
                                                        setShowProxySettings(true);
                                                        return;
                                                    }

                                                    const newProjects = config.projects.map((p: any) =>
                                                        p.id === proj.id ? { ...p, use_proxy: e.target.checked } : p
                                                    );
                                                    const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{ marginRight: '6px' }}
                                        />
                                        <span>{t("proxyMode")}</span>
                                        <span
                                            onClick={(e) => {
                                                e.preventDefault();
                                                e.stopPropagation();
                                                setProxyEditMode('project');
                                                setShowProxySettings(true);
                                            }}
                                            style={{ marginLeft: '4px', cursor: 'pointer', opacity: 0.7 }}
                                            title={t("proxySettings")}
                                        >
                                            ⚙️
                                        </span>
                                    </label>
                                )}
                            </div>
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'flex-start', gap: '15px' }}>
                                <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: '#6b7280' }}>
                                    <input
                                        type="checkbox"
                                        checked={config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.admin_mode || false}
                                        onChange={(e) => {
                                            const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                            if (proj) {
                                                const isWindows = /window/i.test(navigator.userAgent);
                                                const newProjects = config.projects.map((p: any) => {
                                                    if (p.id === proj.id) {
                                                        const updated = { ...p, admin_mode: e.target.checked };
                                                        // On non-Windows, yolo and admin are mutually exclusive
                                                        if (!isWindows && e.target.checked) {
                                                            updated.yolo_mode = false;
                                                        }
                                                        return updated;
                                                    }
                                                    return p;
                                                });
                                                const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                                setConfig(newConfig);
                                                SaveConfig(newConfig);
                                            }
                                        }}
                                        style={{ marginRight: '6px' }}
                                    />
                                    <span>{/window/i.test(navigator.userAgent) ? t("adminModeLabel") : t("rootModeLabel")}</span>
                                </label>
                                <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: '#6b7280' }}>
                                    <input
                                        type="checkbox"
                                        checked={config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.python_project || false}
                                        onChange={(e) => {
                                            const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                            if (proj) {
                                                const newProjects = config.projects.map((p: any) =>
                                                    p.id === proj.id ? { ...p, python_project: e.target.checked } : p
                                                );
                                                const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                                setConfig(newConfig);
                                                SaveConfig(newConfig);
                                            }
                                        }}
                                        style={{ marginRight: '6px' }}
                                    />
                                    <span>{t("pythonProjectLabel")}</span>
                                </label>
                                {config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.python_project && (
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                        <span style={{ fontSize: '0.8rem', color: '#6b7280' }}>{t("pythonEnvLabel")}:</span>
                                        <select
                                            value={config?.projects?.find((p: any) => p.id === selectedProjectForLaunch)?.python_env || ""}
                                            onChange={(e) => {
                                                const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                                if (proj) {
                                                    const newProjects = config.projects.map((p: any) =>
                                                        p.id === proj.id ? { ...p, python_env: e.target.value } : p
                                                    );
                                                    const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                                    setConfig(newConfig);
                                                    SaveConfig(newConfig);
                                                }
                                            }}
                                            style={{
                                                padding: '5px 8px',
                                                borderRadius: '4px',
                                                border: '1px solid #d1d5db',
                                                backgroundColor: '#ffffff',
                                                fontSize: '0.85rem',
                                                color: '#374151',
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
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: '15px' }}>
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                        <span style={{ fontSize: '0.8rem', color: '#6b7280' }}>{t("project")}:</span>
                                        <select
                                            value={selectedProjectForLaunch}
                                            onChange={(e) => setSelectedProjectForLaunch(e.target.value)}
                                            style={{
                                                padding: '5px 8px',
                                                borderRadius: '4px',
                                                border: '1px solid #d1d5db',
                                                backgroundColor: '#ffffff',
                                                fontSize: '0.85rem',
                                                color: '#374151',
                                                cursor: 'pointer',
                                                maxWidth: '200px'
                                            }}
                                        >
                                            {config?.projects?.map((proj: any) => (
                                                <option key={proj.id} value={proj.id}>
                                                    {proj.name}
                                                </option>
                                            ))}
                                        </select>
                                    </div>
                                    <button
                                        onClick={() => switchTool('projects')}
                                        style={{
                                            padding: '0',
                                            height: '20px',
                                            borderRadius: '6px',
                                            border: '1px solid #d1d5db',
                                            backgroundColor: '#f3f4f6',
                                            color: '#6b7280',
                                            fontSize: '0.85rem',
                                            fontWeight: '500',
                                            cursor: 'pointer',
                                            transition: 'all 0.2s',
                                            whiteSpace: 'normal',
                                            textAlign: 'center',
                                            width: '32px',
                                            display: 'flex',
                                            alignItems: 'center',
                                            justifyContent: 'center'
                                        }}
                                        onMouseEnter={(e) => {
                                            e.currentTarget.style.backgroundColor = '#e5e7eb';
                                            e.currentTarget.style.color = '#4b5563';
                                        }}
                                        onMouseLeave={(e) => {
                                            e.currentTarget.style.backgroundColor = '#f3f4f6';
                                            e.currentTarget.style.color = '#6b7280';
                                        }}
                                    >
                                        ...
                                    </button>
                                </div>
                                <button
                                    className="btn-launch"
                                    style={{ padding: '8px 24px', textAlign: 'center', '--wails-draggable': 'no-drag', pointerEvents: 'auto' } as any}
                                    disabled={onDemandInstallingTool === activeTool || backgroundInstallingTool === activeTool || launchingTool === activeTool}
                                    onClick={async () => {
                                        console.log("Launch button clicked. activeTool:", activeTool);
                                        const selectedProj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                        if (selectedProj) {
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
                                            if (selectedProjectForLaunch !== config?.current_project) {
                                                handleProjectSwitch(selectedProjectForLaunch);
                                            }
                                        } else {
                                            console.error("No project found for launch ID:", selectedProjectForLaunch);
                                            setStatus(t("projectDirError"));
                                        }
                                    }}
                                >
                                    <span style={{ marginRight: '6px' }}>➤</span>{t("launch")}
                                </button>
                            </div>
                        </div>
                    </div>
                )}

                <div className="status-message" style={{ padding: '0 20px 4px 20px', minHeight: '20px', display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
                    <span key={status} style={{ color: (status.includes("Error") || status.includes("!") || status.includes("first")) ? '#ef4444' : '#10b981' }}>
                        {status}
                    </span>
                    {backgroundInstallStatus && (
                        <span style={{ 
                            fontSize: '0.75rem', 
                            color: backgroundInstallStatus.startsWith('✓') ? '#10b981' : '#9ca3af',
                            display: 'flex',
                            alignItems: 'center',
                            gap: '4px'
                        }}>
                            {!backgroundInstallStatus.startsWith('✓') && (
                                <span style={{ 
                                    display: 'inline-block', 
                                    width: '10px', 
                                    height: '10px', 
                                    border: '2px solid #9ca3af',
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

            {/* Modals */}
            {showInstallLog && (
                <div className="modal-overlay" onClick={() => setShowInstallLog(false)}>
                    <div className="modal-content" style={{ width: '600px', maxWidth: '90vw' }} onClick={e => e.stopPropagation()}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '15px' }}>
                            <h3 style={{ margin: 0, color: '#60a5fa' }}>{t("installLogTitle")}</h3>
                            <button className="modal-close" onClick={() => setShowInstallLog(false)}>&times;</button>
                        </div>
                        <div
                            className="elegant-scrollbar"
                            style={{
                                backgroundColor: '#1e293b',
                                color: '#1f2937',
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
                                <div style={{ color: '#94a3b8', fontStyle: 'italic' }}>
                                    {t("initializing")}
                                </div>
                            ) : (
                                envLogs.map((log, index) => {
                                    const isError = /error|failed/i.test(log);
                                    return (
                                        <div key={index} style={{
                                            color: isError ? '#ef4444' : 'inherit',
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
                <div className="modal-overlay" style={{ backgroundColor: 'rgba(0, 0, 0, 0.3)' }}>
                    <div style={{
                        backgroundColor: 'white',
                        borderRadius: '16px',
                        padding: '20px 28px',
                        textAlign: 'center',
                        boxShadow: '0 8px 32px rgba(0, 0, 0, 0.12)',
                        minWidth: '220px',
                        maxWidth: '280px'
                    }}>
                        {toolRepairStatus.status === 'installing' && (
                            <div style={{ display: 'flex', alignItems: 'center', gap: '14px' }}>
                                <div style={{
                                    width: '24px',
                                    height: '24px',
                                    border: '3px solid #e2e8f0',
                                    borderTop: '3px solid #3b82f6',
                                    borderRadius: '50%',
                                    animation: 'spin 0.8s linear infinite',
                                    flexShrink: 0
                                }}></div>
                                <span style={{ color: '#475569', fontSize: '0.9rem', fontWeight: 500 }}>
                                    {t("toolRepairInstalling").replace("{tool}", toolRepairStatus.toolName)}
                                </span>
                            </div>
                        )}
                        {toolRepairStatus.status === 'success' && (
                            <div style={{ display: 'flex', alignItems: 'center', gap: '12px' }}>
                                <div style={{
                                    width: '28px',
                                    height: '28px',
                                    backgroundColor: '#dcfce7',
                                    borderRadius: '50%',
                                    display: 'flex',
                                    alignItems: 'center',
                                    justifyContent: 'center',
                                    flexShrink: 0
                                }}>
                                    <span style={{ color: '#16a34a', fontSize: '16px' }}>✓</span>
                                </div>
                                <span style={{ color: '#16a34a', fontSize: '0.9rem', fontWeight: 500 }}>
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
                                        backgroundColor: '#fee2e2',
                                        borderRadius: '50%',
                                        display: 'flex',
                                        alignItems: 'center',
                                        justifyContent: 'center',
                                        flexShrink: 0
                                    }}>
                                        <span style={{ color: '#dc2626', fontSize: '14px' }}>✕</span>
                                    </div>
                                    <span style={{ color: '#dc2626', fontSize: '0.9rem', fontWeight: 500 }}>
                                        {t("toolRepairFailed").replace("{tool}", toolRepairStatus.toolName)}
                                    </span>
                                </div>
                                <p style={{ color: '#6b7280', fontSize: '0.8rem', margin: '0 0 12px 0', wordBreak: 'break-word', textAlign: 'left' }}>
                                    {toolRepairStatus.message}
                                </p>
                                <button
                                    style={{
                                        backgroundColor: '#f1f5f9',
                                        border: 'none',
                                        borderRadius: '8px',
                                        padding: '6px 16px',
                                        fontSize: '0.85rem',
                                        color: '#475569',
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
                                <div style={{ backgroundColor: '#f0f9ff', padding: '12px', borderRadius: '6px', marginBottom: '15px', border: '1px solid #e0f2fe' }}>
                                    <div style={{ fontSize: '0.85rem', color: '#6b7280', marginBottom: '8px' }}>{t("currentVersion")}</div>
                                    <div style={{ fontSize: '1rem', fontWeight: '600', color: '#1e40af', marginBottom: '12px' }}>v{APP_VERSION}</div>
                                    <div style={{ fontSize: '0.85rem', color: '#6b7280', marginBottom: '8px' }}>{t("latestVersion")}</div>
                                    <div style={{ fontSize: '1rem', fontWeight: '600', color: '#059669' }}>{updateResult.latest_version}</div>
                                </div>

                                <div style={{ marginTop: '15px' }}>
                                    {isDownloading ? (
                                        <div style={{ width: '100%' }}>
                                            <div style={{ display: 'flex', justifyContent: 'space-between', marginBottom: '8px', fontSize: '0.9rem' }}>
                                                <span>{t("downloading")}</span>
                                                <span>{downloadProgress}%</span>
                                            </div>
                                            <div style={{ width: '100%', height: '10px', backgroundColor: '#e2e8f0', borderRadius: '5px', overflow: 'hidden' }}>
                                                <div style={{ width: `${downloadProgress}%`, height: '100%', backgroundColor: '#3b82f6', transition: 'width 0.2s ease' }}></div>
                                            </div>
                                            <button
                                                className="btn-link"
                                                style={{ marginTop: '10px', color: '#ef4444' }}
                                                onClick={handleCancelDownload}
                                            >
                                                {t("cancelDownload")}
                                            </button>
                                        </div>
                                    ) : installerPath ? (
                                        <div style={{ textAlign: 'center', padding: '10px' }}>
                                            <p style={{ color: '#059669', fontWeight: 'bold', marginBottom: '15px' }}>{t("downloadComplete")}</p>
                                            <button className="btn-primary" style={{ width: '100%' }} onClick={handleInstall}>
                                                {t("installNow")}
                                            </button>
                                        </div>
                                    ) : (
                                        <div>
                                            {downloadError && (
                                                <div style={{ marginBottom: '10px' }}>
                                                    <p style={{ color: '#ef4444', fontSize: '0.85rem', marginBottom: '5px' }}>{t("downloadError").replace("{error}", downloadError)}</p>
                                                    <button className="btn-primary" style={{ width: '100%', backgroundColor: '#ef4444' }} onClick={handleDownload}>
                                                        {t("retry")}
                                                    </button>
                                                </div>
                                            )}
                                            {!downloadError && (
                                                <>
                                                    <p style={{ margin: '10px 0', fontSize: '0.9rem', color: '#374151' }}>{t("foundNewVersionMsg")}</p>
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
                            <div style={{ backgroundColor: '#f0f9ff', padding: '12px', borderRadius: '6px', border: '1px solid #e0f2fe' }}>
                                <div style={{ fontSize: '0.85rem', color: '#6b7280', marginBottom: '8px' }}>{t("currentVersion")}</div>
                                <div style={{ fontSize: '1rem', fontWeight: '600', color: '#1e40af', marginBottom: '12px' }}>v{APP_VERSION}</div>
                                <div style={{ fontSize: '0.85rem', color: '#6b7280', marginBottom: '8px' }}>{t("latestVersion")}</div>
                                <div style={{ fontSize: '1rem', fontWeight: '600', color: '#059669', marginBottom: '12px' }}>{updateResult.latest_version}</div>
                                <p style={{ margin: '0', fontSize: '0.9rem', color: '#059669', fontWeight: '500' }}>✓ {t("isLatestVersion")}</p>
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
                            <h3 style={{ margin: 0, color: '#60a5fa' }}>{t("modelSettings")}</h3>
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
                                                            padding: '6px 4px', color: '#9ca3af', fontSize: '1rem'
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
                                                return (
                                                    <button
                                                        key={globalIndex}
                                                        className={`tab-button ${activeTab === globalIndex ? 'active' : ''}`}
                                                        onClick={() => setActiveTab(globalIndex)}
                                                        style={{ overflow: 'hidden', textOverflow: 'ellipsis', flexShrink: 0 }}
                                                    >
                                                        {getModelDisplayName(model.model_name, lang)}
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
                                                            padding: '6px 4px', color: '#9ca3af', fontSize: '1rem'
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
                                        onContextMenu={(e) => handleContextMenu(e, e.currentTarget)}
                                        placeholder={t("customProviderPlaceholder")}
                                        spellCheck={false}
                                        autoComplete="off"
                                    />
                                </div>
                            )}

                            {(config as any)[activeTool].models[activeTab].model_name !== "Original" && activeTool !== 'qoder' && (
                                <div className="form-group" style={{ flex: 1 }}>
                                    <label className="form-label">
                                        {t("modelName")}
                                        {(activeTool === 'codebuddy' || activeTool === 'qoder') && <span style={{ fontSize: '0.7rem', color: '#94a3b8', marginLeft: '5px' }}>(Supports multiple, separated by comma)</span>}
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
                                                onContextMenu={(e) => handleContextMenu(e, e.currentTarget)}
                                                placeholder={(activeTool === 'codebuddy' || activeTool === 'qoder') ? "e.g. gpt-4,gpt-3.5-turbo" : (getDefaultModelId(activeTool, (config as any)[activeTool].models[activeTab].model_name) || "e.g. gpt-4")}
                                                spellCheck={false}
                                                autoComplete="off"
                                            />
                                            {(() => {
                                                const providerName = (config as any)[activeTool].models[activeTab].model_name;
                                                const models = (activeTool === 'claude' || (providerName !== '阿里云' && providerName !== 'aliyun')) ? recommendedModels[providerName] : undefined;
                                                if (!models || models.length === 0) return null;
                                                return (
                                                    <button
                                                        style={{ border: '1px solid #d1d5db', background: '#ffffff', color: '#374151', borderRadius: '6px', padding: '6px 8px', cursor: 'pointer', fontSize: '0.8rem', whiteSpace: 'nowrap', flexShrink: 0 }}
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
                                                <div style={{ position: 'absolute', top: '100%', right: 0, zIndex: 100, marginTop: '4px', background: '#ffffff', border: '1px solid #d1d5db', borderRadius: '8px', boxShadow: '0 4px 12px rgba(0,0,0,0.15)', minWidth: '200px', maxHeight: '240px', overflowY: 'auto', padding: '4px 0' }}>
                                                    {models.map((m: any, i: number) => (
                                                        <div
                                                            key={i}
                                                            style={{ padding: '6px 12px', cursor: 'pointer', fontSize: '0.82rem', color: '#1f2937', display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: '8px' }}
                                                            onMouseEnter={(e) => (e.currentTarget.style.background = '#f3f4f6')}
                                                            onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
                                                            onClick={() => { handleModelIdChange(m.id); setShowModelRecommend(false); }}
                                                        >
                                                            <span>{m.id}</span>
                                                            {m.note && <span style={{ fontSize: '0.7rem', color: '#9ca3af' }}>{m.note}</span>}
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
                                        onContextMenu={(e) => handleContextMenu(e, e.currentTarget)}
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
                                        <label className="form-label" style={{ margin: 0 }}>{activeTool === 'qoder' ? t("personalToken") : t("apiKey")}</label>
                                        {activeTool === 'qoder' ? (
                                            <button
                                                className="btn-link"
                                                style={{ fontSize: '0.75rem', padding: '2px 8px' }}
                                                onClick={() => BrowserOpenURL("https://qoder.com/account/integrations")}
                                            >
                                                {t("getToken")}
                                            </button>
                                        ) : (
                                            !(config as any)[activeTool].models[activeTab].is_custom && (
                                                <button
                                                    className="btn-link"
                                                    style={{ fontSize: '0.75rem', padding: '2px 8px' }}
                                                    onClick={() => handleOpenSubscribe((config as any)[activeTool].models[activeTab].model_name)}
                                                >
                                                    {t("getKey")}
                                                </button>
                                            )
                                        )}
                                    </div>
                                    <input
                                        type="password"
                                        className="form-input"
                                        data-field="api-key"
                                        value={(config as any)[activeTool].models[activeTab].api_key}
                                        onChange={(e) => handleApiKeyChange(e.target.value)}
                                        onContextMenu={(e) => handleContextMenu(e, e.currentTarget)}
                                        placeholder={activeTool === 'qoder' ? t("personalToken") : t("enterKey")}
                                        spellCheck={false}
                                        autoComplete="off"
                                    />
                                </div>

                                {activeTool !== 'qoder' && (
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
                                            onContextMenu={(e) => handleContextMenu(e, e.currentTarget)}
                                            placeholder="https://api.example.com/v1"
                                            spellCheck={false}
                                            autoComplete="off"
                                            readOnly={!(config as any)[activeTool].models[activeTab].is_custom}
                                            style={!(config as any)[activeTool].models[activeTab].is_custom ? { backgroundColor: '#f3f4f6', cursor: 'not-allowed', color: '#9ca3af' } : {}}
                                        />
                                    </div>
                                )}
                            </>
                        )}

                        <div style={{ display: 'flex', gap: '10px', marginTop: '24px' }}>
                            <button className="btn-primary" style={{ flex: 1 }} onClick={save}>{t("saveChanges")}</button>
                            {(config as any)[activeTool].models[activeTab].is_custom && (
                                <button
                                    className="btn-hide"
                                    style={{ flex: 0.5, backgroundColor: '#fca5a5', color: '#991b1b', border: '1px solid #fca5a5' }}
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
                                        style={{ flex: 0.5, backgroundColor: '#93c5fd', color: '#1e40af', border: '1px solid #93c5fd' }}
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

            {contextMenu.visible && (
                <div style={{
                    position: 'fixed',
                    top: contextMenu.y,
                    left: contextMenu.x,
                    backgroundColor: 'white',
                    border: '1px solid #e2e8f0',
                    borderRadius: '8px',
                    boxShadow: '0 4px 12px rgba(0,0,0,0.1)',
                    zIndex: 3000,
                    padding: '5px 0',
                    minWidth: '120px'
                }}>
                    <div className="context-menu-item" onClick={() => handleContextAction('selectAll')}>{t("selectAll")}</div>
                    <div style={{ height: '1px', backgroundColor: '#f1f5f9', margin: '4px 0' }}></div>
                    <div className="context-menu-item" onClick={() => handleContextAction('copy')}>{t("copy")}</div>
                    <div className="context-menu-item" onClick={() => handleContextAction('cut')}>{t("cut")}</div>
                    <div className="context-menu-item" onClick={() => handleContextAction('paste')}>{t("contextPaste")}</div>
                </div>
            )}

            {showProviderSelector && (
                <div className="modal-overlay" style={{ backgroundColor: 'rgba(0,0,0,0.35)', backdropFilter: 'blur(3px)' }} onClick={() => { setShowProviderSelector(false); setHoveredProvider(null); }}>
                    <div className="modal-content" style={{ maxWidth: '480px', maxHeight: '70vh', padding: '20px', borderRadius: '16px', border: 'none', boxShadow: '0 20px 40px rgba(0,0,0,0.12)' }} onClick={(e) => e.stopPropagation()}>
                        <h2 style={{ margin: '0 0 16px 0', fontSize: '1.1rem', fontWeight: 700, color: '#1e293b', textAlign: 'center' }}>{t("selectProviderTitle")}</h2>
                        
                        {/* Filter pills */}
                        <div style={{ display: 'flex', gap: '6px', marginBottom: '14px', justifyContent: 'center' }}>
                            {(['all', 'china', 'global'] as const).map(f => (
                                <button
                                    key={f}
                                    onClick={() => setProviderFilter(f)}
                                    style={{
                                        padding: '5px 16px', fontSize: '0.8rem', borderRadius: '20px', border: 'none', cursor: 'pointer', fontWeight: 600,
                                        backgroundColor: providerFilter === f ? '#3b82f6' : '#f1f5f9',
                                        color: providerFilter === f ? '#fff' : '#64748b',
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
                                                border: isSelected ? '2px solid #3b82f6' : '1.5px solid #e8ecf1',
                                                backgroundColor: isSelected ? '#eff6ff' : '#fff',
                                                transition: 'all 0.15s ease',
                                                boxShadow: isSelected ? '0 2px 8px rgba(59,130,246,0.15)' : '0 1px 3px rgba(0,0,0,0.04)',
                                                position: 'relative'
                                            }}
                                        >
                                            <div style={{ fontSize: '0.8rem', fontWeight: 600, color: isSelected ? '#2563eb' : '#334155', lineHeight: 1.3, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', display: 'flex', alignItems: 'center', justifyContent: 'center', gap: '3px' }}>
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
                            backgroundColor: '#1e293b',
                            color: '#1f2937',
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
                                <div style={{ fontSize: '0.65rem', color: '#94a3b8', marginTop: '2px' }}>{hoveredProvider.provider.description}</div>
                            )}
                            {/* Arrow */}
                            <div style={{
                                position: 'absolute', bottom: '-5px', left: '50%', transform: 'translateX(-50%)',
                                width: 0, height: 0,
                                borderLeft: '6px solid transparent', borderRight: '6px solid transparent', borderTop: '6px solid #1e293b'
                            }} />
                        </div>
                    )}
                </div>
            )}

            {showStartupPopup && (
                <div className="modal-overlay" style={{ backgroundColor: 'rgba(0, 0, 0, 0.4)', backdropFilter: 'blur(4px)' }}>
                    <div className="modal-content" style={{
                        width: '320px',
                        textAlign: 'center',
                        padding: 0,
                        borderRadius: '16px',
                        overflow: 'hidden',
                        border: 'none',
                        boxShadow: '0 20px 25px -5px rgba(0, 0, 0, 0.1), 0 10px 10px -5px rgba(0, 0, 0, 0.04)'
                    }}>
                        <div style={{
                            background: 'linear-gradient(135deg, #f0f9ff 0%, #e0f2fe 100%)',
                            padding: '25px 20px',
                            color: '#1e293b',
                            position: 'relative',
                            borderBottom: '1px solid #e2e8f0'
                        }}>
                            <button
                                className="modal-close"
                                onClick={() => setShowStartupPopup(false)}
                                style={{ color: '#9ca3af', opacity: 0.8, top: '10px', right: '15px', zIndex: 10 }}
                            >&times;</button>
                            <div style={{
                                fontSize: '2.5rem',
                                marginBottom: '10px',
                                background: 'linear-gradient(135deg, #3b82f6 0%, #8b5cf6 100%)',
                                WebkitBackgroundClip: 'text',
                                WebkitTextFillColor: 'transparent',
                                fontWeight: '900',
                                lineHeight: 1,
                                filter: 'drop-shadow(0 2px 4px rgba(59, 130, 246, 0.1))'
                            }}>{`</>`}</div>
                            <h3 style={{ margin: 0, color: '#0f172a', fontSize: '1.2rem', fontWeight: 'bold' }}>{t("startupTitle")}</h3>
                            <p style={{
                                margin: '6px 0 0 0',
                                background: 'linear-gradient(to right, #2563eb, #9333ea, #db2777)',
                                WebkitBackgroundClip: 'text',
                                WebkitTextFillColor: 'transparent',
                                fontSize: '0.95rem',
                                fontWeight: '700'
                            }}>
                                {t("slogan")}
                            </p>
                            <p style={{
                                margin: '4px 0 0 0',
                                background: 'linear-gradient(to right, #2563eb, #9333ea, #db2777)',
                                WebkitBackgroundClip: 'text',
                                WebkitTextFillColor: 'transparent',
                                fontSize: '0.85rem',
                                fontWeight: '700'
                            }}>
                                {lang === 'zh-Hans' || lang === 'zh-Hant' ? '真正的Vibe Coder只使用命令行。' : 'Real Vibe Coders only use the command line.'}
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
                                        background: 'linear-gradient(135deg, #eff6ff, #dbeafe)',
                                        color: '#1e40af',
                                        border: '1px solid #bfdbfe',
                                        boxShadow: '0 2px 4px rgba(59, 130, 246, 0.1)',
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
                                        border: '1px solid #e2e8f0',
                                        borderRadius: '10px',
                                        fontSize: '0.95rem',
                                        fontWeight: '500',
                                        color: '#9ca3af',
                                        backgroundColor: '#ffffff',
                                        display: 'flex',
                                        alignItems: 'center',
                                        justifyContent: 'center',
                                        gap: '8px',
                                        boxShadow: '0 1px 2px rgba(0,0,0,0.05)'
                                    }}
                                    onClick={() => {
                                        const manualUrl = (lang === 'zh-Hans' || lang === 'zh-Hant')
                                            ? "https://github.com/RapidAI/aicoder/blob/main/UserManual_CN.md"
                                            : "https://github.com/RapidAI/aicoder/blob/main/UserManual_EN.md";
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
                                    color: '#94a3b8'
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

            {/* Thanks Modal */}
            {showThanksModal && (
                <div className="modal-overlay">
                    <div className="modal-content elegant-scrollbar" style={{ maxWidth: '600px', maxHeight: '80vh', overflowY: 'auto' }}>
                        <h3 style={{ marginTop: 0, marginBottom: '15px', color: '#60a5fa' }}>{t("thanks")}</h3>
                        <div className="markdown-content" style={{
                            backgroundColor: '#fff',
                            padding: '10px',
                            borderRadius: '4px',
                            border: '1px solid var(--border-color)',
                            fontFamily: 'inherit',
                            fontSize: '0.8rem',
                            lineHeight: '1.6',
                            color: '#374151',
                            textAlign: 'left',
                            whiteSpace: 'pre-wrap',
                            wordBreak: 'break-word'
                        }}>
                            <ReactMarkdown
                                remarkPlugins={[remarkGfm]}
                                // @ts-ignore
                                rehypePlugins={[rehypeRaw]}
                                components={{ a: MarkdownLink }}
                            >
                                {thanksContent}
                            </ReactMarkdown>
                        </div>
                        <button onClick={() => setShowThanksModal(false)} className="btn-secondary" style={{ marginTop: '20px' }}>
                            {t("close")}
                        </button>
                    </div>
                </div>
            )}

            {/* Confirm Dialog */}
            {confirmDialog.show && (
                <div style={{
                    position: 'fixed',
                    top: 0,
                    left: 0,
                    right: 0,
                    bottom: 0,
                    backgroundColor: 'rgba(0, 0, 0, 0.6)',
                    backdropFilter: 'blur(4px)',
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'center',
                    zIndex: 10000,
                    animation: 'fadeIn 0.2s ease-out'
                }}>
                    <div style={{
                        backgroundColor: '#ffffff',
                        borderRadius: '12px',
                        padding: '24px',
                        minWidth: '360px',
                        maxWidth: '420px',
                        boxShadow: '0 20px 60px rgba(0, 0, 0, 0.3), 0 0 0 1px rgba(0, 0, 0, 0.05)',
                        border: 'none',
                        animation: 'slideUp 0.3s ease-out',
                        position: 'relative'
                    }}>
                        {/* Icon */}
                        <div style={{
                            width: '48px',
                            height: '48px',
                            borderRadius: '50%',
                            backgroundColor: '#fef2f2',
                            display: 'flex',
                            alignItems: 'center',
                            justifyContent: 'center',
                            marginBottom: '16px',
                            border: '2px solid #fee2e2'
                        }}>
                            <svg width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="#ef4444" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round">
                                <circle cx="12" cy="12" r="10"></circle>
                                <line x1="12" y1="8" x2="12" y2="12"></line>
                                <line x1="12" y1="16" x2="12.01" y2="16"></line>
                            </svg>
                        </div>

                        {/* Title */}
                        <h3 style={{
                            margin: '0 0 8px 0',
                            fontSize: '1.15rem',
                            color: '#1f2937',
                            fontWeight: '700',
                            letterSpacing: '-0.02em'                    }}>
                            {confirmDialog.title}
                        </h3>

                        {/* Message */}
                        <p style={{
                            margin: '0 0 20px 0',
                            color: '#6b7280',
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
                                    backgroundColor: '#f9fafb',
                                    color: '#374151',
                                    border: '1px solid #e5e7eb',
                                    borderRadius: '8px',
                                    cursor: 'pointer',
                                    fontSize: '0.875rem',
                                    fontWeight: '600',
                                    transition: 'all 0.2s',
                                    boxShadow: '0 1px 2px rgba(0, 0, 0, 0.05)'
                                }}
                                onMouseEnter={(e) => {
                                    e.currentTarget.style.backgroundColor = '#f3f4f6';
                                    e.currentTarget.style.borderColor = '#d1d5db';
                                    e.currentTarget.style.transform = 'translateY(-1px)';
                                    e.currentTarget.style.boxShadow = '0 2px 4px rgba(0, 0, 0, 0.1)';
                                }}
                                onMouseLeave={(e) => {
                                    e.currentTarget.style.backgroundColor = '#f9fafb';
                                    e.currentTarget.style.borderColor = '#e5e7eb';
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
                                    backgroundColor: '#ef4444',
                                    color: 'white',
                                    border: 'none',
                                    borderRadius: '8px',
                                    cursor: 'pointer',
                                    fontSize: '0.875rem',
                                    fontWeight: '600',
                                    transition: 'all 0.2s',
                                    boxShadow: '0 2px 4px rgba(239, 68, 68, 0.3)'
                                }}
                                onMouseEnter={(e) => {
                                    e.currentTarget.style.backgroundColor = '#dc2626';
                                    e.currentTarget.style.transform = 'translateY(-1px)';
                                    e.currentTarget.style.boxShadow = '0 4px 8px rgba(239, 68, 68, 0.4)';
                                }}
                                onMouseLeave={(e) => {
                                    e.currentTarget.style.backgroundColor = '#ef4444';
                                    e.currentTarget.style.transform = 'translateY(0)';
                                    e.currentTarget.style.boxShadow = '0 2px 4px rgba(239, 68, 68, 0.3)';
                                }}
                            >
                                {t("confirm")}
                            </button>
                        </div>
                    </div>
                </div>
            )}

            {/* Proxy Settings Dialog */}
            {showProxySettings && config && (
                <div className="modal-overlay">
                    <div className="modal-content" style={{ width: '500px', textAlign: 'left' }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
                            <h3 style={{ margin: 0, color: '#60a5fa' }}>
                                {proxyEditMode === 'global' ? t("proxySettings") + " - " + (lang === 'zh-Hans' ? '全局默认' : lang === 'zh-Hant' ? '全局預設' : 'Global Default') : t("proxySettings")}
                            </h3>
                            <button className="modal-close" onClick={() => setShowProxySettings(false)}>&times;</button>
                        </div>

                        {proxyEditMode === 'project' && config?.default_proxy_host && (
                            <div style={{ marginBottom: '15px', padding: '10px', backgroundColor: '#f0f9ff', borderRadius: '6px', fontSize: '0.85rem' }}>
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
                                                    p.id === proj.id ? {
                                                        ...p,
                                                        proxy_host: '',
                                                        proxy_port: '',
                                                        proxy_username: '',
                                                        proxy_password: ''
                                                    } : p
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

                        <div className="form-group">
                            <label className="form-label">{t("proxyHost")}</label>
                            <input
                                type="text"
                                className="form-input"
                                value={(() => {
                                    if (proxyEditMode === 'global') {
                                        return config?.default_proxy_host || '';
                                    } else {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                        return proj?.proxy_host || '';
                                    }
                                })()}
                                onChange={(e) => {
                                    if (proxyEditMode === 'global') {
                                        const newConfig = new main.AppConfig({ ...config, default_proxy_host: e.target.value });
                                        setConfig(newConfig);
                                    } else {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                        if (proj) {
                                            const newProjects = config.projects.map((p: any) =>
                                                p.id === proj.id ? { ...p, proxy_host: e.target.value } : p
                                            );
                                            const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                            setConfig(newConfig);
                                        }
                                    }
                                }}
                                placeholder={t("proxyHostPlaceholder")}
                                spellCheck={false}
                            />
                        </div>

                        <div className="form-group">
                            <label className="form-label">{t("proxyPort")}</label>
                            <input
                                type="text"
                                className="form-input"
                                value={(() => {
                                    if (proxyEditMode === 'global') {
                                        return config?.default_proxy_port || '';
                                    } else {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                        return proj?.proxy_port || '';
                                    }
                                })()}
                                onChange={(e) => {
                                    if (proxyEditMode === 'global') {
                                        const newConfig = new main.AppConfig({ ...config, default_proxy_port: e.target.value });
                                        setConfig(newConfig);
                                    } else {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                        if (proj) {
                                            const newProjects = config.projects.map((p: any) =>
                                                p.id === proj.id ? { ...p, proxy_port: e.target.value } : p
                                            );
                                            const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                            setConfig(newConfig);
                                        }
                                    }
                                }}
                                placeholder={t("proxyPortPlaceholder")}
                                spellCheck={false}
                            />
                        </div>

                        <div className="form-group">
                            <label className="form-label">{t("proxyUsername")}</label>
                            <input
                                type="text"
                                className="form-input"
                                value={(() => {
                                    if (proxyEditMode === 'global') {
                                        return config?.default_proxy_username || '';
                                    } else {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                        return proj?.proxy_username || '';
                                    }
                                })()}
                                onChange={(e) => {
                                    if (proxyEditMode === 'global') {
                                        const newConfig = new main.AppConfig({ ...config, default_proxy_username: e.target.value });
                                        setConfig(newConfig);
                                    } else {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                        if (proj) {
                                            const newProjects = config.projects.map((p: any) =>
                                                p.id === proj.id ? { ...p, proxy_username: e.target.value } : p
                                            );
                                            const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                            setConfig(newConfig);
                                        }
                                    }
                                }}
                                spellCheck={false}
                                autoComplete="off"
                            />
                        </div>

                        <div className="form-group">
                            <label className="form-label">{t("proxyPassword")}</label>
                            <input
                                type="password"
                                className="form-input"
                                value={(() => {
                                    if (proxyEditMode === 'global') {
                                        return config?.default_proxy_password || '';
                                    } else {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                        return proj?.proxy_password || '';
                                    }
                                })()}
                                onChange={(e) => {
                                    if (proxyEditMode === 'global') {
                                        const newConfig = new main.AppConfig({ ...config, default_proxy_password: e.target.value });
                                        setConfig(newConfig);
                                    } else {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                        if (proj) {
                                            const newProjects = config.projects.map((p: any) =>
                                                p.id === proj.id ? { ...p, proxy_password: e.target.value } : p
                                            );
                                            const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                            setConfig(newConfig);
                                        }
                                    }
                                }}
                                autoComplete="new-password"
                            />
                        </div>

                        <div style={{ display: 'flex', gap: '10px', justifyContent: 'flex-end', marginTop: '20px' }}>
                            <button
                                className="btn-secondary"
                                onClick={() => setShowProxySettings(false)}
                                style={{ padding: '8px 16px' }}
                            >
                                {t("cancel")}
                            </button>
                            <button
                                className="btn-primary"
                                onClick={() => {
                                    SaveConfig(config);
                                    setShowProxySettings(false);

                                    // Auto-enable use_proxy after configuration (project mode only)
                                    if (proxyEditMode === 'project') {
                                        const proj = config?.projects?.find((p: any) => p.id === selectedProjectForLaunch);
                                        if (proj && !proj.use_proxy) {
                                            const newProjects = config.projects.map((p: any) =>
                                                p.id === proj.id ? { ...p, use_proxy: true } : p
                                            );
                                            const newConfig = new main.AppConfig({ ...config, projects: newProjects });
                                            setConfig(newConfig);
                                            SaveConfig(newConfig);
                                        }
                                    }
                                }}
                                style={{ padding: '8px 16px' }}
                            >
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
                            <h3 style={{ margin: 0, color: '#10b981', whiteSpace: 'nowrap' }}>{t("selectSkillsToInstall")}</h3>
                            
                            <div style={{ 
                                display: 'flex', 
                                alignItems: 'center', 
                                gap: '12px', 
                                padding: '4px 12px', 
                                backgroundColor: '#f3f4f6', 
                                borderRadius: '20px',
                                fontSize: '0.8rem',
                                marginLeft: '5px'
                            }}>
                                <span style={{ color: '#6b7280', fontWeight: '500' }}>{t("installLocation")}</span>
                                <label style={{ display: 'flex', alignItems: 'center', gap: '4px', cursor: 'pointer', color: installLocation === 'user' ? '#10b981' : '#4b5563', fontWeight: installLocation === 'user' ? 'bold' : 'normal' }}>
                                    <input 
                                        type="radio" 
                                        name="installLocation" 
                                        checked={installLocation === 'user'} 
                                        onChange={() => setInstallLocation('user')}
                                        style={{ margin: 0 }}
                                    /> {t("userLocation")}
                                </label>
                                <label style={{ display: 'flex', alignItems: 'center', gap: '4px', cursor: 'pointer', color: installLocation === 'project' ? '#10b981' : '#4b5563', fontWeight: installLocation === 'project' ? 'bold' : 'normal' }}>
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
                                            border: '1px solid #d1d5db',
                                            fontSize: '0.8rem',
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

                            <button onClick={() => setShowInstallSkillModal(false)} className="btn-close" style={{ marginLeft: 'auto' }}>&times;</button>
                        </div>
                        <div className="modal-body" style={{ maxHeight: '300px', overflowY: 'auto', padding: '10px 0' }}>
                            {(() => {
                                const filtered = installLocation === 'project' 
                                    ? skills.filter(s => s.type !== 'address') 
                                    : skills;
                                
                                if (filtered.length === 0) {
                                    return (
                                        <div style={{ textAlign: 'center', color: '#6b7280', padding: '20px' }}>
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
                                                border: '1px solid var(--border-color)',
                                                borderRadius: '6px',
                                                cursor: skill.installed ? 'not-allowed' : 'pointer',
                                                backgroundColor: selectedSkillsToInstall.includes(skill.name) ? '#f0fdf4' : '#fff',
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
                                                                color: '#10b981',
                                                                backgroundColor: '#d1fae5',
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
                                        color: '#3b82f6', 
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
                                            border: '2px solid #3b82f6',
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
                                        backgroundColor: '#10b981', 
                                        borderColor: '#10b981',
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
                                            border: '2px solid white',
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

            {showToast && (
                <div className="toast">
                    {toastMessage}
                </div>
            )}
        </div>
    );
}

export default App;