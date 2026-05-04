import { useCallback, useEffect, useState, useRef, useMemo } from 'react';
import './App.css';
import { buildNumber } from './version';
import appIcon from './assets/images/maclaw2.png';
import qianxinIcon from './assets/images/qianxin.png';
import lobsterOffline from './assets/images/lobster_offline.svg';
import lobsterHalf from './assets/images/lobster_half.svg';
import { CheckToolsStatus, CheckUpdate, InstallToolOnDemand, IsToolBeingInstalled, LoadConfig, SaveConfig, CheckEnvironment, ResizeWindow, LaunchTool, SelectProjectDir, SetLanguage, GetUserHomeDir, ReadBBS, ReadTutorial, ReadThanks, ListPythonEnvironments, PackLog, ShowItemInFolder, GetSystemInfo, OpenSystemUrl, DownloadUpdate, CancelDownload, LaunchInstallerAndExit, ListSkills, ListSkillsWithInstallStatus, DeleteSkill, GetEnvCheckInterval, ShouldCheckEnvironment, UpdateLastEnvCheckTime, IsWindowsTerminalAvailable, ListRemoteHubs, ListToolProviders, PingMaclawLLM, AgentNetIsRunning, AgentNetEnsureDaemonWithDownload, AgentNetStopDaemon, GetQQBotStatus, GetTelegramStatus, GetWeixinStatus, GetWeixinLocalMode, GetQQBotLocalMode, GetTelegramLocalMode, GetThirdPartyGatewayStatus, GetThirdPartyGatewayLocalMode, IsGossipAllowed, GetBrandInfo, GetUIZoomFactor, GetChatFontSize, ListBackgroundLoops, GetAllLLMTokenUsage, GetMaclawLLMProviders, GetHubLLMServiceStatus, GroupDiscussionStatus, GroupDiscussionPublishProfile, GroupDiscussionProcessPendingInvites, GroupDiscussionAcceptInvite, GroupDiscussionRejectInvite, SearchProjects, ResumeProject, RenameTask, PinTask, HideTask } from "../wailsjs/go/main/App";

import { EventsOn, EventsOff, BrowserOpenURL, Quit, WindowHide, WindowFullscreen, WindowUnfullscreen } from "../wailsjs/runtime";
import { main } from "../wailsjs/go/models";
import { RemoteSettingsPanel } from './components/remote/RemoteSettingsPanel';
import { SecurityPolicyPanel } from './components/remote/SecurityPolicyPanel';
import { useRemotePanel } from './components/remote/useRemotePanel';
import { TERMINAL_SESSION_STATUSES } from './components/remote/types';
import { LLMConfigPanel } from './components/remote/LLMConfigPanel';
import { HubServiceRedeemPanel } from './components/remote/HubServiceRedeemPanel';
import { EmbeddingConfigPanel } from './components/remote/EmbeddingConfigPanel';
import { ASRConfigPanel } from './components/remote/ASRConfigPanel';
import { TTSConfigPanel } from './components/remote/TTSConfigPanel';
import { useAudioDevices } from './components/ai/useAudioDevices';
import { MemoryManagementPanel } from './components/remote/MemoryManagementPanel';
import { AgentNetPanel } from './components/remote/AgentNetPanel';
import { AgentNetTabContainer } from './components/remote/AgentNetTabContainer';
import { GroupDiscussionSettingsPanel } from './components/remote/GroupDiscussionSettingsPanel';
import { IMAuditPanel } from './components/remote/IMAuditPanel';
import { OnboardingWizard } from './components/remote/OnboardingWizard';
import { AIAssistantPanel } from './components/ai/AIAssistantPanel';
import { readStoredAssistantThemeMode } from './components/ai/assistantThemeStorage';
import { PetSettingsPanel } from './components/PetSettingsPanel';
import { useAIAssistant } from './components/ai/useAIAssistant';
import { useDialog } from './components/CustomDialog';
import { buildHubCreditsURL } from './utils/hubCredits';
import { translations } from './i18n/appTranslations';
import { ToolConfiguration } from './components/tools/ToolConfiguration';
import { PROJECT_PAGE_SIZE, knownProviderEndpoints, recommendedModels, sidebarProviderAliases, subscriptionUrls, getModelDisplayName, type ProviderEndpoint } from './config/providerCatalog';
import { TOOL_NAMES, isToolTab } from './config/toolCatalog';
import { getSettingsTabOptions, type SettingsTabId } from './config/settingsTabs';
import { SettingsTabsRail } from './components/settings/SettingsTabsRail';
import { GeneralSettingsPanel } from './components/settings/GeneralSettingsPanel';
import { UISettingsPanel } from './components/settings/UISettingsPanel';
import { ProgrammingToolsSettingsPanel } from './components/settings/ProgrammingToolsSettingsPanel';
import { GeneralAdvancedSettingsPanel } from './components/settings/GeneralAdvancedSettingsPanel';
import { SystemSettingsPanel } from './components/settings/SystemSettingsPanel';
import { ProxySettingsPanel } from './components/settings/ProxySettingsPanel';
import { IMSettingsPanel } from './components/settings/IMSettingsPanel';
import { AppSidebarShell } from './components/layout/AppSidebarShell';
import { MainTopHeader } from './components/layout/MainTopHeader';
import { AppStatusMessageBar } from './components/layout/AppStatusMessageBar';
import { TutorialPage } from './components/pages/TutorialPage';
import { ApiStorePage } from './components/pages/ApiStorePage';
import { ProjectManagerPage } from './components/pages/ProjectManagerPage';
import { RemoteSessionsPage } from './components/pages/RemoteSessionsPage';
import { SkillsPage } from './components/pages/SkillsPage';
import { MCPPage } from './components/pages/MCPPage';
import { GossipPage } from './components/pages/GossipPage';
import { StartupPopup } from './components/modals/StartupPopup';
import { ThanksModal } from './components/modals/ThanksModal';
import { AboutPanel } from './components/AboutPanel';
import { ToolRepairProgressDialog } from './components/modals/ToolRepairProgressDialog';
import { UpdateModal } from './components/modals/UpdateModal';
import { InstallLogModal } from './components/modals/InstallLogModal';
import { ProjectProxySettingsDialog } from './components/modals/ProjectProxySettingsDialog';
import { InstallSkillModal } from './components/modals/InstallSkillModal';
import { RemoteActivationDialog } from './components/modals/RemoteActivationDialog';
import { ProviderSelectorDialog } from './components/modals/ProviderSelectorDialog';
import { ConfirmDialog } from './components/modals/ConfirmDialog';
import type { RemoteCenterHubOption, SidebarHubCredits, SidebarHubServiceStatus, SidebarLLMProviderSummary, SidebarTokenUsageStat } from './types/appShell';





const APP_VERSION = "5.4.2.9920"
const MACLAW_CODE_REPOSITORY_URL = "https://github.com/rapidai/maclaw";


function App() {
    const { showAlert } = useDialog();
    const [config, setConfig] = useState<main.AppConfig | null>(null);
    const [navTab, setNavTab] = useState<string>("ai");
    const audioDevices = useAudioDevices();
    const [aiPanelMaximized, setAiPanelMaximized] = useState(false);
    const navTabRef = useRef(navTab);
    useEffect(() => { navTabRef.current = navTab; }, [navTab]);
    const [bbsContent, setBbsContent] = useState<string>("");
    const [tutorialContent, setTutorialContent] = useState<string>("");
    const [thanksContent, setThanksContent] = useState<string>(""); // New state for thanks content
    const [showThanksModal, setShowThanksModal] = useState<boolean>(false); // New state for thanks modal
    const [refreshStatus, setRefreshStatus] = useState<string>("");
    const [lastUpdateTime, setLastUpdateTime] = useState<string>("");
    const [refreshKey, setRefreshKey] = useState<number>(0);
    const [activeTool, setActiveTool] = useState<string>("claude");
    const [recentTasksPaneWidth, setRecentTasksPaneWidth] = useState(180);
    const [isRecentTasksResizing, setIsRecentTasksResizing] = useState(false);
    const recentTasksResizeStartX = useRef(0);
    const recentTasksResizeStartWidth = useRef(180);
    const [sidebarExpanded, setSidebarExpanded] = useState(false);
    const [toolDropdownOpen, setToolDropdownOpen] = useState(false);
    const [taskContextMenu, setTaskContextMenu] = useState<{ x: number; y: number; projectPath: string; name: string; pinned: boolean } | null>(null);
    const [renamingTaskPath, setRenamingTaskPath] = useState<string | null>(null);
    const [renameValue, setRenameValue] = useState("");
    const [recentProjects, setRecentProjects] = useState<Array<{ id?: string; name?: string; project_path: string; workflow_type?: string; preview?: string; last_activity?: string; pinned?: boolean }>>([]);
    const [status, setStatus] = useState("");
    const [activeTab, setActiveTab] = useState(0);
    const [tabStartIndex, setTabStartIndex] = useState(0);
    const [settingsTab, setSettingsTab] = useState<SettingsTabId>('general');
    const [imSubTab, setImSubTab] = useState<'qq' | 'telegram' | 'weixin' | 'thirdparty'>('qq');
    const [qqBotStatus, setQQBotStatus] = useState<string>('disconnected');
    const [qqBotLocalMode, setQQBotLocalModeState] = useState<boolean>(true);
    const [telegramStatus, setTelegramStatus] = useState<string>('disconnected');
    const [telegramLocalMode, setTelegramLocalModeState] = useState<boolean>(true);
    const [weixinStatus, setWeixinStatus] = useState<string>('disconnected');
    const [weixinLocalMode, setWeixinLocalModeState] = useState<boolean>(true);
    const [thirdPartyGatewayStatus, setThirdPartyGatewayStatus] = useState<string>('disconnected');
    const [thirdPartyGatewayLocalMode, setThirdPartyGatewayLocalModeState] = useState<boolean>(true);
    const [imAuditPlatform, setIMAuditPlatform] = useState<string | null>(null);
    const [weixinQRCode, setWeixinQRCode] = useState<string>('');
    const [weixinQRLoading, setWeixinQRLoading] = useState<boolean>(false);
    const [weixinQRWaiting, setWeixinQRWaiting] = useState<boolean>(false);
    const [weixinQRError, setWeixinQRError] = useState<string>('');
    const [installLocation, setInstallLocation] = useState<'user' | 'project'>('user');
    const [installProject, setInstallProject] = useState<string>("");
    const [isBatchInstalling, setIsBatchInstalling] = useState(false);
    const [isMarketplaceInstalling, setIsMarketplaceInstalling] = useState(false);
    const [isLoading, setIsLoading] = useState(true);
    const [toolProviders, setToolProviders] = useState<Array<{ name: string; valid: boolean; builtin: boolean }>>([]);
    const [isManualCheck, setIsManualCheck] = useState(false);
    const [showStartupPopup, setShowStartupPopup] = useState(false);
    const [showMaclawLLMPopup, setShowMaclawLLMPopup] = useState(false);
    const [pythonEnvironments, setPythonEnvironments] = useState<any[]>([]);
    const [envCheckInterval, setEnvCheckInterval] = useState<number>(7);
    const [uiZoom, setUiZoom] = useState<number>(1.0);
    const [chatFontSize, setChatFontSize] = useState<number>(14);

    const updateSidebarNavVisibility = useCallback((key: 'show_nav_mcp' | 'show_nav_gossip' | 'show_nav_agentnet', visible: boolean) => {
        if (!config) return;
        const newConfig = new main.AppConfig({ ...config, [key]: visible } as any);
        setConfig(newConfig);
        SaveConfig(newConfig);
    }, [config]);

    const imAuditBtnStyle: React.CSSProperties = {
        fontSize: '0.68rem',
        padding: '2px 10px',
        borderRadius: '4px',
        border: '1px solid var(--theme-primary)',
        background: 'transparent',
        color: 'var(--theme-primary)',
        cursor: 'pointer',
        whiteSpace: 'nowrap',
    };

    // Brand info from backend
    const [brandInfo, setBrandInfo] = useState<{id: string, displayName: string, displayNameCN: string, slogan: string, author: string, businessContact: string, websiteURL: string, githubURL: string, iconPath: string} | null>(null);
    const currentIcon = brandInfo?.id === 'qianxin' ? qianxinIcon : appIcon;
    const [aiThemeMode, setAIThemeMode] = useState<'light' | 'dark'>(() => {
        return readStoredAssistantThemeMode();
    });
    const brandDisplayTitle = brandInfo ? `${brandInfo.displayNameCN} ${brandInfo.displayName}` : '\u7801\u5361\u9f99 MaClaw';
    const brandSidebarName = brandInfo?.displayName || 'MaClaw';

    // MaClaw LLM online status (lobster indicator)
    const [maclawLLMOnline, setMaclawLLMOnline] = useState<boolean>(false);
    const [maclawLLMConfigured, setMaclawLLMConfigured] = useState<boolean>(false);
    const [sidebarCurrentProviderTokenUsage, setSidebarCurrentProviderTokenUsage] = useState<{ provider: string; isHubService: boolean; input: number; output: number; total: number }>({ provider: '', isHubService: false, input: 0, output: 0, total: 0 });
    const [sidebarHubCredits, setSidebarHubCredits] = useState<SidebarHubCredits | null>(null);
    const maclawLLMFirstPingDone = useRef(false);

    // AgentNet P2P network running status (globe indicator)
    const [agentNetRunning, setAgentNetRunning] = useState<boolean>(false);
    const maclawLLMFirstPingResult = useRef<{online: boolean; configured: boolean} | null>(null);

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
    useEffect(() => {
        if (navTab !== 'ai' && aiPanelMaximized) {
            WindowUnfullscreen();
            setAiPanelMaximized(false);
        }
    }, [navTab, aiPanelMaximized]);

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
    const [toastMessage, setToastMessage] = useState<string>("");
    const [showToast, setShowToast] = useState(false);
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

    useEffect(() => {
        if (navTab !== 'about' || thanksContent.trim()) return;
        let cancelled = false;
        ReadThanks()
            .then((content) => {
                if (!cancelled) setThanksContent(content || "");
            })
            .catch((err) => console.error("Failed to preload thanks content:", err));
        return () => { cancelled = true; };
    }, [navTab, thanksContent]);

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
            setAiPanelMaximized(false);
            return;
        }
        WindowFullscreen();
        setAiPanelMaximized(true);
    };

    const handleWindowHide = (e: React.MouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
        WindowHide();
    };

    const handleRecentTasksResizeStart = (e: React.MouseEvent<HTMLDivElement>) => {
        e.preventDefault();
        e.stopPropagation();
        recentTasksResizeStartX.current = e.clientX;
        recentTasksResizeStartWidth.current = recentTasksPaneWidth;
        setIsRecentTasksResizing(true);
    };

    useEffect(() => {
        if (!isRecentTasksResizing) return;
        const handleMove = (event: MouseEvent) => {
            const nextWidth = recentTasksResizeStartWidth.current + event.clientX - recentTasksResizeStartX.current;
            setRecentTasksPaneWidth(Math.min(300, Math.max(140, nextWidth)));
        };
        const handleUp = () => setIsRecentTasksResizing(false);
        window.addEventListener('mousemove', handleMove);
        window.addEventListener('mouseup', handleUp);
        return () => {
            window.removeEventListener('mousemove', handleMove);
            window.removeEventListener('mouseup', handleUp);
        };
    }, [isRecentTasksResizing]);

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
            GetChatFontSize().then((s) => {
                if (s >= 12) {
                    setChatFontSize(s);
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
            GetChatFontSize().then((s) => {
                if (s >= 12) {
                    setChatFontSize(s);
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
        });
        // Fetch initial QQ Bot status
        GetQQBotStatus().then(setQQBotStatus).catch(() => {});
        GetQQBotLocalMode().then(setQQBotLocalModeState).catch(() => {});

        // Telegram Bot status listener
        EventsOn("telegram-status-changed", (status: string) => {
            setTelegramStatus(status);
        });
        GetTelegramStatus().then(setTelegramStatus).catch(() => {});
        GetTelegramLocalMode().then(setTelegramLocalModeState).catch(() => {});

        // WeChat status listener
        EventsOn("weixin-status-changed", (status: string) => {
            setWeixinStatus(status);
        });
        GetWeixinStatus().then(setWeixinStatus).catch(() => {});
        GetWeixinLocalMode().then(setWeixinLocalModeState).catch(() => {});

        EventsOn("thirdparty-gateway-status-changed", (status: string) => {
            setThirdPartyGatewayStatus(status);
        });
        GetThirdPartyGatewayStatus().then(setThirdPartyGatewayStatus).catch(() => {});
        GetThirdPartyGatewayLocalMode().then(setThirdPartyGatewayLocalModeState).catch(() => {});

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
            EventsOff("thirdparty-gateway-status-changed");
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
        setToolDropdownOpen(false);
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

    const t = translate;
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

    const [groupDiscussionStatus, setGroupDiscussionStatus] = useState<any>(null);
    const groupDiscussionConfig = (config as any)?.group_discussion || {};

    const refreshGroupDiscussionStatus = useCallback(async () => {
        try {
            await GroupDiscussionProcessPendingInvites();
        } catch {
            // Policy processing is best-effort; still show current Hub status if possible.
        }
        try {
            const nextStatus = await GroupDiscussionStatus();
            setGroupDiscussionStatus(nextStatus);
        } catch (error) {
            setGroupDiscussionStatus((prev: any) => ({ ...(prev || {}), error: String(error) }));
        }
    }, []);

    const publishGroupDiscussionProfile = useCallback(async () => {
        try {
            await GroupDiscussionPublishProfile();
            await refreshGroupDiscussionStatus();
        } catch (error) {
            setGroupDiscussionStatus((prev: any) => ({ ...(prev || {}), error: String(error) }));
        }
    }, [refreshGroupDiscussionStatus]);

    const handleGroupDiscussionAcceptInvite = useCallback(async (inviteId: string) => {
        await GroupDiscussionAcceptInvite(inviteId, { accepted_by: "user" });
        await refreshGroupDiscussionStatus();
    }, [refreshGroupDiscussionStatus]);

    const handleGroupDiscussionRejectInvite = useCallback(async (inviteId: string) => {
        await GroupDiscussionRejectInvite(inviteId, { reason: "rejected_by_user" });
        await refreshGroupDiscussionStatus();
    }, [refreshGroupDiscussionStatus]);

    useEffect(() => {
        if (!config) return;
        if (groupDiscussionConfig.enabled === false) {
            setGroupDiscussionStatus({ enabled: false, discoverable: false, pending_invites: [] });
            return;
        }
        refreshGroupDiscussionStatus();
        const timer = window.setInterval(refreshGroupDiscussionStatus, 60000);
        return () => window.clearInterval(timer);
    }, [config, groupDiscussionConfig.enabled, refreshGroupDiscussionStatus]);

    useEffect(() => {
        if (!config || groupDiscussionConfig.enabled === false || groupDiscussionConfig.discoverable === false) return;
        publishGroupDiscussionProfile();
        const timer = window.setInterval(publishGroupDiscussionProfile, 300000);
        return () => window.clearInterval(timer);
    }, [config, groupDiscussionConfig.enabled, groupDiscussionConfig.discoverable, publishGroupDiscussionProfile]);

    const aiAssistant = useAIAssistant({ refreshSessionsOnly });
    const refreshRecentProjects = useCallback(() => {
        SearchProjects("", 10).then((r: any) => setRecentProjects(r || [])).catch(() => setRecentProjects([]));
    }, []);

    useEffect(() => {
        if (navTab === 'ai') refreshRecentProjects();
    }, [navTab, refreshRecentProjects]);

    useEffect(() => {
        const refresh = () => {
            if (navTabRef.current === 'ai') refreshRecentProjects();
        };
        EventsOn("project-index:changed", refresh);
        EventsOn("tasks:changed", refresh);
        return () => {
            EventsOff("project-index:changed");
            EventsOff("tasks:changed");
        };
    }, [refreshRecentProjects]);

    const resumeRecentProject = useCallback(async (projectPath: string) => {
        try {
            const msg = await ResumeProject(projectPath);
            if (msg) {
                switchTool('ai');
                await aiAssistant.sendMessage(msg);
            }
        } catch (error) {
            console.error("ResumeProject failed:", error);
        }
    }, [aiAssistant]);

    useEffect(() => {
        let cancelled = false;
        const normalizeUsage = (stat?: SidebarTokenUsageStat | null) => {
            const input = stat?.input_tokens ?? stat?.InputTokens ?? 0;
            const output = stat?.output_tokens ?? stat?.OutputTokens ?? 0;
            const total = stat?.total_tokens ?? stat?.TotalTokens ?? input + output;
            return { input, output, total };
        };
        const normalizeProviderState = (data?: {
            providers?: Array<{ name?: string; Name?: string; url?: string; URL?: string; is_hub_service?: boolean; IsHubService?: boolean }>;
            Providers?: Array<{ name?: string; Name?: string; url?: string; URL?: string; is_hub_service?: boolean; IsHubService?: boolean }>;
            current?: string;
            Current?: string;
        } | null) => {
            const list = (data?.providers ?? data?.Providers ?? [])
                .map((provider): SidebarLLMProviderSummary => ({
                    name: provider?.name ?? provider?.Name ?? '',
                    url: provider?.url ?? provider?.URL ?? '',
                    isHubService: !!(provider?.is_hub_service ?? provider?.IsHubService),
                }))
                .filter((provider) => !!provider.name);
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
        const getPreferredProvider = (providerSummaries: SidebarLLMProviderSummary[], currentProviderName: string, usageMap: Record<string, SidebarTokenUsageStat>) => {
            const providerNames = providerSummaries.map((provider) => provider.name);
            if (currentProviderName && providerSummaries.some((provider) => provider.name === currentProviderName && provider.isHubService)) return currentProviderName;
            const providerWithUsage = providerNames.find((provider) => hasUsage(usageMap, provider));
            if (currentProviderName && providerNames.includes(currentProviderName) && (hasUsage(usageMap, currentProviderName) || !providerWithUsage)) {
                return currentProviderName;
            }
            return providerWithUsage || currentProviderName || providerNames[0] || '';
        };
        const normalizeHubCredits = (status?: SidebarHubServiceStatus | null): SidebarHubCredits | null => {
            const active = status?.active ?? status?.Active ?? false;
            if (!active) return { authorized: false, total: 0, used: 0, remaining: 0, tokensPerCredit: 0, expiresAt: '', unlimited: false };
            const grants = status?.credit_grants ?? status?.CreditGrants ?? status?.active_grants ?? status?.ActiveGrants ?? [];
            let total = 0;
            let used = 0;
            let remaining = 0;
            for (const grant of grants) {
                total += Number(grant.credits_total ?? grant.CreditsTotal ?? 0);
                used += Number(grant.credits_used ?? grant.CreditsUsed ?? 0);
                remaining += Number(grant.credits_remaining ?? grant.CreditsRemaining ?? 0);
            }
            total = Number(status?.credits_total ?? status?.CreditsTotal ?? total);
            used = Number(status?.credits_used ?? status?.CreditsUsed ?? used);
            remaining = Number(status?.credits_remaining ?? status?.CreditsRemaining ?? remaining);
            const available = Number(status?.credits_available ?? status?.CreditsAvailable ?? 0);
            if (remaining <= 0 && available > 0) remaining = available;
            if (total <= 0 && remaining > 0) total = used + remaining;
            const unlimited = total <= 0;
            const nearestGrantExpiry = grants
                .map((grant) => String(grant.expires_at ?? grant.ExpiresAt ?? ''))
                .filter(Boolean)
                .sort()[0] || '';
            return { authorized: true, total, used, remaining, tokensPerCredit: Number(status?.tokens_per_credit ?? status?.TokensPerCredit ?? 0), expiresAt: String(status?.effective_expires_at ?? status?.EffectiveExpiresAt ?? status?.nearest_expires_at ?? status?.NearestExpiresAt ?? nearestGrantExpiry), unlimited };
        };
        const normalizeProviderURL = (value?: string) => String(value || '').trim().replace(/\/+$/, '');
        const refreshSidebarTokenUsage = async () => {
            try {
                const [usageMap, providerState] = await Promise.all([
                    GetAllLLMTokenUsage() as Promise<Record<string, SidebarTokenUsageStat> | null>,
                    GetMaclawLLMProviders() as Promise<{
                        providers?: Array<{ name?: string; Name?: string; url?: string; URL?: string; is_hub_service?: boolean; IsHubService?: boolean }>;
                        Providers?: Array<{ name?: string; Name?: string; url?: string; URL?: string; is_hub_service?: boolean; IsHubService?: boolean }>;
                        current?: string;
                        Current?: string;
                    } | null>,
                ]);
                const normalizedMap = usageMap || {};
                const normalizedProviderState = normalizeProviderState(providerState);
                const providerSummaries = normalizedProviderState.providers.length > 0
                    ? normalizedProviderState.providers
                    : providers.map((provider) => ({ name: provider.name, url: (provider as any).url || (provider as any).URL || '', isHubService: !!(provider as any).is_hub_service || !!(provider as any).IsHubService })).filter((provider) => !!provider.name);
                const currentProviderName = getPreferredProvider(
                    providerSummaries,
                    normalizedProviderState.current || selectedProvider || providers[0]?.name || '',
                    normalizedMap,
                );
                const currentProviderUsage = getUsageForProvider(normalizedMap, currentProviderName);
                const currentProvider = providerSummaries.find((provider) => provider.name === currentProviderName);
                let hubStatus: SidebarHubServiceStatus | null = null;
                try {
                    hubStatus = await GetHubLLMServiceStatus() as SidebarHubServiceStatus;
                } catch {
                    hubStatus = null;
                }
                const hubServiceURL = normalizeProviderURL(hubStatus?.hub_llm_base_url ?? hubStatus?.HubLLMBaseURL);
                const currentProviderURL = normalizeProviderURL(currentProvider?.url);
                const currentProviderIsHubService = !!currentProvider?.isHubService || (!!hubServiceURL && !!currentProviderURL && hubServiceURL === currentProviderURL);
                const hubCredits = currentProviderIsHubService ? normalizeHubCredits(hubStatus) : null;
                if (!cancelled) {
                    setSidebarCurrentProviderTokenUsage({ provider: currentProviderName, isHubService: currentProviderIsHubService, ...currentProviderUsage });
                    setSidebarHubCredits(hubCredits);
                }
            } catch {
                if (!cancelled) {
                    setSidebarCurrentProviderTokenUsage({ provider: '', isHubService: false, input: 0, output: 0, total: 0 });
                    setSidebarHubCredits(null);
                }
            }
        };
        const delayedRefreshTimers = new Set<number>();
        const queueDelayedRefresh = (delayMs: number) => {
            const timer = window.setTimeout(() => {
                delayedRefreshTimers.delete(timer);
                void refreshSidebarTokenUsage();
            }, delayMs);
            delayedRefreshTimers.add(timer);
        };
        void refreshSidebarTokenUsage();
        const onTokenUsageChanged = () => {
            void refreshSidebarTokenUsage();
            queueDelayedRefresh(2500);
        };
        EventsOn("llm-token-usage-changed", onTokenUsageChanged);
        EventsOn("hub-llm-service-changed", onTokenUsageChanged);
        const usageRefreshTimer = window.setInterval(() => { void refreshSidebarTokenUsage(); }, 10 * 60 * 1000);
        return () => {
            cancelled = true;
            window.clearInterval(usageRefreshTimer);
            delayedRefreshTimers.forEach((timer) => window.clearTimeout(timer));
            EventsOff("llm-token-usage-changed");
            EventsOff("hub-llm-service-changed");
        };
    }, [providers, selectedProvider]);

    const openHubCreditsPage = useCallback(() => {
        const url = buildHubCreditsURL((config as any)?.remote_hub_url, (config as any)?.remote_viewer_token);
        if (url) {
            BrowserOpenURL(url);
            return;
        }
        showAlert(lang === 'zh-Hans' ? 'Hub \u767b\u5f55\u4fe1\u606f\u7f3a\u5931\uff0c\u6682\u65f6\u65e0\u6cd5\u6253\u5f00 Credits \u9875\u9762\u3002' : 'Hub login information is missing, so the Credits page cannot be opened.');
    }, [config, lang, showAlert]);

    const formatSidebarCredit = useCallback((value: number) => {
        if (!Number.isFinite(value)) return '0';
        if (Math.abs(value) >= 1000) return value.toLocaleString(undefined, { maximumFractionDigits: 0 });
        if (Math.abs(value) >= 10) return value.toLocaleString(undefined, { maximumFractionDigits: 1 });
        return value.toLocaleString(undefined, { maximumFractionDigits: 2 });
    }, []);

    const formatSidebarTokens = useCallback((value: number) => {
        if (!Number.isFinite(value) || value <= 0) return '0';
        if (value >= 1000000) return `${(value / 1000000).toFixed(value >= 10000000 ? 0 : 1)}M`;
        if (value >= 1000) return `${(value / 1000).toFixed(value >= 10000 ? 0 : 1)}K`;
        return String(value);
    }, []);

    const formatSidebarExpiry = useCallback((value?: string) => {
        if (!value) return '-';
        const d = new Date(value);
        if (Number.isNaN(d.getTime())) return value;
        return d.toLocaleDateString(undefined, { year: '2-digit', month: '2-digit', day: '2-digit' });
    }, []);

    const noHubAuthorizationText = lang === 'zh-Hans' ? '\u65e0' : lang === 'zh-Hant' ? '\u7121' : 'None';
    const longTermHubExpiryText = lang === 'zh-Hans' ? '\u957f\u671f' : lang === 'zh-Hant' ? '\u9577\u671f' : 'Long-term';
    const unlimitedHubCreditText = lang === 'zh-Hans' ? '\u65e0\u9650' : lang === 'zh-Hant' ? '\u7121\u9650' : 'Unlimited';
    const freeUseHubCreditText = lang === 'zh-Hans' ? '\u7545\u7528' : lang === 'zh-Hant' ? '\u66a2\u7528' : 'Free use';

    const formatSidebarHubExpiry = useCallback((credits: SidebarHubCredits | null) => {
        if (!credits) return '-';
        if (!credits.authorized) return noHubAuthorizationText;
        if (!credits.expiresAt) return longTermHubExpiryText;
        return formatSidebarExpiry(credits.expiresAt);
    }, [formatSidebarExpiry, longTermHubExpiryText, noHubAuthorizationText]);

    const formatSidebarHubTotalCredits = useCallback((credits: SidebarHubCredits | null) => {
        if (!credits) return '-';
        if (!credits.authorized) return noHubAuthorizationText;
        if (credits.unlimited) return unlimitedHubCreditText;
        return formatSidebarCredit(credits.total);
    }, [formatSidebarCredit, noHubAuthorizationText, unlimitedHubCreditText]);

    const formatSidebarHubUsedCredits = useCallback((credits: SidebarHubCredits | null) => {
        if (!credits) return '-';
        if (!credits.authorized) return noHubAuthorizationText;
        return formatSidebarCredit(credits.used);
    }, [formatSidebarCredit, noHubAuthorizationText]);

    const showHubCreditAction = !!sidebarHubCredits && (!sidebarHubCredits.authorized || (sidebarHubCredits.total > 0 && sidebarHubCredits.remaining / sidebarHubCredits.total < 0.2));

    const activeRemoteSessionForTool = useMemo(() => {
        return remoteSessions.find((session) => {
            if (session.tool !== activeTool) return false;
            const status = String(session.status || session.summary?.status || "").toLowerCase();
            return !TERMINAL_SESSION_STATUSES.has(status);
        }) || null;
    }, [remoteSessions, activeTool]);

    // Track background loops for sidebar badge
    const [sidebarBgLoops, setSidebarBgLoops] = useState<any[]>([]);
    useEffect(() => {
        let cancelled = false;
        const refresh = async () => {
            try {
                const loops = await ListBackgroundLoops();
                if (!cancelled) setSidebarBgLoops(loops || []);
            } catch { if (!cancelled) setSidebarBgLoops([]); }
        };
        refresh();
        const cleanup = EventsOn("background-loops-changed", refresh);
        const timer = setInterval(refresh, 5000);
        return () => {
            cancelled = true;
            clearInterval(timer);
            if (typeof cleanup === "function") cleanup(); else EventsOff("background-loops-changed");
        };
    }, []);

    // Count running (non-terminal) sessions + background loops for the sidebar badge
    const runningTaskCount = useMemo(() => {
        const remoteCount = remoteSessions.filter((session) => {
            const status = String(session.status || session.summary?.status || "").toLowerCase();
            return !TERMINAL_SESSION_STATUSES.has(status);
        }).length;
        const bgCount = sidebarBgLoops.filter((l: any) => l.status === "running" || l.status === "paused").length;
        return remoteCount + bgCount;
    }, [remoteSessions, sidebarBgLoops]);

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
            showAlert("Failed to send log: " + e);
        }
    };

    if (isLoading) {
        return (
            <div data-ai-theme={aiThemeMode} style={{
                height: '100vh',
                display: 'flex',
                flexDirection: 'column',
                justifyContent: 'center',
                alignItems: 'center',
                backgroundColor: 'var(--theme-page-bg)',
                color: 'var(--theme-text-primary)',
                padding: '20px',
                textAlign: 'center',
                boxSizing: 'border-box',
                borderRadius: '12px',
                border: '1px solid var(--theme-border)',
                overflow: 'hidden',
                ...(aiThemeMode === 'dark' ? {
                    '--theme-page-bg': '#0b1220',
                    '--theme-surface': '#111827',
                    '--theme-surface-muted': '#0f172a',
                    '--theme-text-primary': '#e5e7eb',
                    '--theme-text-secondary': '#cbd5e1',
                    '--theme-text-muted': '#94a3b8',
                    '--theme-border': '#334155'
                } : {})
            } as any}>
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
                    background: 'linear-gradient(135deg, #6366f1, #8b5cf6, #a855f7)',
                    WebkitBackgroundClip: 'text',
                    WebkitTextFillColor: 'transparent',
                    marginBottom: '20px',
                    display: 'inline-block',
                    fontWeight: 'bold'
                }}>{t("envCheckTitle")}</h2>
                <div style={{ width: '100%', height: '4px', backgroundColor: 'var(--theme-surface-muted)', borderRadius: '2px', overflow: 'hidden', marginBottom: '15px', border: '1px solid var(--theme-border)' }}>
                    <div style={{
                        width: '50%',
                        height: '100%',
                        backgroundColor: '#6366f1',
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
                            color: 'var(--theme-text-primary)',
                            backgroundColor: 'var(--theme-surface)',
                            border: '1px solid var(--theme-border)',
                            borderRadius: '8px',
                            resize: 'none',
                            outline: 'none',
                            marginBottom: '10px'
                        }}
                    />
                ) : (
                    <div style={{ fontSize: '0.9rem', color: 'var(--theme-text-secondary)', marginBottom: '15px', height: '20px', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {envLogs.length > 0 ? envLogs[envLogs.length - 1] : t("initializing")}
                    </div>
                )}

                <div style={{ display: 'flex', gap: '15px', alignItems: 'center' }}>
                    <button
                        onClick={() => setShowLogs(!showLogs)}
                        style={{
                            background: 'none',
                            border: 'none',
                            color: '#6366f1',
                            fontSize: '0.8rem',
                            cursor: 'pointer',
                            textDecoration: 'underline'
                        }}
                    >
                        {showLogs ? (lang === 'zh-Hans' ? '\u9690\u85cf\u8be6\u60c5' : lang === 'zh-Hant' ? '\u96b1\u85cf\u8a73\u60c5' : 'Hide Details') : (lang === 'zh-Hans' ? '\u67e5\u770b\u8be6\u60c5' : lang === 'zh-Hant' ? '\u67e5\u770b\u8a73\u60c5' : 'Show Details')}
                    </button>

                    {showLogs && (
                        isManualCheck ? (
                            <button onClick={() => {
                                setIsLoading(false);
                                setIsManualCheck(false);
                            }} className="btn-hide" style={{ borderColor: '#6366f1', color: '#6366f1', padding: '4px 12px' }}>
                                {lang === 'zh-Hans' ? '\u6536\u8d77' : lang === 'zh-Hant' ? '\u6536\u8d77' : 'Hide'}
                            </button>
                        ) : (
                            <button onClick={Quit} className="btn-hide" style={{ borderColor: '#ef4444', color: '#ef4444', padding: '4px 12px' }}>
                                {lang === 'zh-Hans' ? '\u9000\u51fa\u7a0b\u5e8f' : lang === 'zh-Hant' ? '\u9000\u51fa\u7a0b\u5f0f' : 'Quit'}
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
    const settingsTabOptions = getSettingsTabOptions(lang);
    const isRemoteCapableActiveTool = remoteToolMetadata.some(
        (meta) => meta.name === activeTool && meta.supports_remote === true
    );
    const launchMode = config?.default_launch_mode === 'remote' ? 'remote' : 'local';
    const launchRemoteEnabled = launchMode === 'remote';
    return (
        <div
            className="app-viewport"
            style={{ ['--ui-scale' as any]: String(uiZoom) } as React.CSSProperties}
        >
            <div className="app-scale-layer">
                <div id="App" data-ai-theme={aiThemeMode}>
            <AppSidebarShell
                navTab={navTab}
                recentTasksPaneWidth={recentTasksPaneWidth}
                aiThemeMode={aiThemeMode}
                brandInfo={brandInfo}
                currentIcon={currentIcon}
                brandSidebarName={brandSidebarName}
                switchTool={switchTool}
                lang={lang}
                maclawLLMOnline={maclawLLMOnline}
                agentNetRunning={agentNetRunning}
                remoteActivationStatus={remoteActivationStatus}
                qqBotStatus={qqBotStatus}
                telegramStatus={telegramStatus}
                weixinStatus={weixinStatus}
                runningTaskCount={runningTaskCount}
                t={t}
                gossipAllowed={gossipAllowed}
                config={config}
                sidebarExpanded={sidebarExpanded}
                setSidebarExpanded={setSidebarExpanded}
                activeTool={activeTool}
                toolDropdownOpen={toolDropdownOpen}
                setToolDropdownOpen={setToolDropdownOpen}
                recentProjects={recentProjects}
                renamingTaskPath={renamingTaskPath}
                setRenamingTaskPath={setRenamingTaskPath}
                renameValue={renameValue}
                setRenameValue={setRenameValue}
                resumeRecentProject={resumeRecentProject}
                refreshRecentProjects={refreshRecentProjects}
                taskContextMenu={taskContextMenu}
                setTaskContextMenu={setTaskContextMenu}
                renameTask={RenameTask}
                pinTask={PinTask}
                hideTask={HideTask}
                sidebarCurrentProviderTokenUsage={sidebarCurrentProviderTokenUsage}
                sidebarHubCredits={sidebarHubCredits}
                formatSidebarTokens={formatSidebarTokens}
                formatSidebarHubExpiry={formatSidebarHubExpiry}
                formatSidebarHubTotalCredits={formatSidebarHubTotalCredits}
                formatSidebarHubUsedCredits={formatSidebarHubUsedCredits}
                formatSidebarCredit={formatSidebarCredit}
                unlimitedHubCreditText={unlimitedHubCreditText}
                noHubAuthorizationText={noHubAuthorizationText}
                showHubCreditAction={showHubCreditAction}
                openHubCreditsPage={openHubCreditsPage}
                handleRecentTasksResizeStart={handleRecentTasksResizeStart}
                isRecentTasksResizing={isRecentTasksResizing}
            />
            <div className="main-container" data-ai-theme={aiThemeMode}>
                {/* AI assistant as main content (both lite and pro modes) */}
                {navTab === 'ai' ? (
                    <div style={{ position: 'relative', display: 'flex', flexDirection: 'column', width: '100%', height: '100%', minHeight: 0 }}>
                        <AIAssistantPanel
                            onClose={() => { switchTool('settings'); }}
                            lang={lang}
                            chatFontSize={chatFontSize}
                            themeMode={aiThemeMode}
                            onThemeModeChange={setAIThemeMode}
                            audioInputDeviceId={(config as any)?.audio_input_device_id || ''}
                            audioOutputDeviceId={(config as any)?.audio_output_device_id || ''}
                            groupDiscussion={{
                                config: groupDiscussionConfig,
                                status: groupDiscussionStatus,
                                onRefreshStatus: refreshGroupDiscussionStatus,
                                onPublishProfile: publishGroupDiscussionProfile,
                                onAcceptInvite: handleGroupDiscussionAcceptInvite,
                                onRejectInvite: handleGroupDiscussionRejectInvite,
                            }}
                            state={{
                                messages: aiAssistant.messages,
                                progressMessages: aiAssistant.progressMessages,
                                sending: aiAssistant.sending,
                                streaming: aiAssistant.streaming,
                                visualBusy: aiAssistant.visualBusy,
                                ready: aiAssistant.ready,
                                initStatus: aiAssistant.initStatus,
                                selectedFilePath: (aiAssistant as any).selectedFilePath || ((aiAssistant as any).selectedFilePaths?.[0] ?? ""),
                                selectedFilePaths: (aiAssistant as any).selectedFilePaths || [],
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
                                removeSelectedFile: (aiAssistant as any).removeSelectedFile,
                                sendMessage: aiAssistant.sendMessage,
                                clearHistory: aiAssistant.clearHistory,
                                recordSubmittedPrompt: aiAssistant.recordSubmittedPrompt,
                                setDraftInputValue: aiAssistant.setDraftInputValue,
                                executeAction: aiAssistant.executeAction,
                                refreshNews: aiAssistant.refreshNews,
                                onOpenOnboarding: () => setShowMaclawLLMPopup(true),
                                cancelSession: aiAssistant.cancelSession,
                                onTaskPrefsChanged: () => { void refreshSessionsOnly(); },
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
                <>
                <MainTopHeader
                    navTab={navTab}
                    lang={lang}
                    t={t}
                    activeTool={activeTool}
                    switchTool={switchTool}
                    handleAddNewProject={handleAddNewProject}
                    setRefreshStatus={setRefreshStatus}
                    setTutorialContent={setTutorialContent}
                    setRefreshKey={setRefreshKey}
                    setShowModelSettings={setShowModelSettings}
                    setSelectedSkillsToInstall={setSelectedSkillsToInstall}
                    setShowInstallSkillModal={setShowInstallSkillModal}
                    handleWindowHide={handleWindowHide}
                />

                <div className="main-content elegant-scrollbar" style={{ overflowY: navTab === 'projects' ? 'hidden' : 'auto', paddingBottom: '20px', '--wails-draggable': 'no-drag' } as any}>
                    {navTab === 'tutorial' && (
                        <TutorialPage
                            lang={lang}
                            refreshStatus={refreshStatus}
                            refreshKey={refreshKey}
                            tutorialContent={tutorialContent}
                            switchTool={switchTool}
                        />
                    )}
                    {navTab === 'gossip' && gossipAllowed && (
                        <GossipPage lang={lang} />
                    )}
                    {navTab === 'remote' && (
                        <RemoteSessionsPage
                            lang={lang}
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
                        />
                    )}
                    {navTab === 'api-store' && (
                        <ApiStorePage
                            lang={lang}
                            t={t}
                            chatFontSize={chatFontSize}
                            setChatFontSize={setChatFontSize}
                        />
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
                        <ProjectManagerPage
                            config={config}
                            setConfig={setConfig}
                            t={t}
                            projectSearchKeyword={projectSearchKeyword}
                            setProjectSearchKeyword={setProjectSearchKeyword}
                            projectSortMode={projectSortMode}
                            setProjectSortMode={setProjectSortMode}
                            filteredAndSortedProjects={filteredAndSortedProjects}
                            pagedProjects={pagedProjects}
                            projectPageStartIndex={projectPageStartIndex}
                            projectPageSize={PROJECT_PAGE_SIZE}
                            safeProjectCurrentPage={safeProjectCurrentPage}
                            totalProjectPages={totalProjectPages}
                            setProjectCurrentPage={setProjectCurrentPage}
                            selectedProjectForLaunch={selectedProjectForLaunch}
                            setSelectedProjectForLaunch={setSelectedProjectForLaunch}
                        />
                    )}

                    {navTab === 'skills' && (
                        <SkillsPage localizeText={localizeText} />
                    )}

                    {navTab === 'mcp' && (
                        <MCPPage translate={translate} />
                    )}

                    {navTab === 'settings' && (
                        <div className="settings-shell" style={{ padding: '10px' }}>
                            <SettingsTabsRail
                                tabs={settingsTabOptions}
                                activeTab={settingsTab}
                                onChange={setSettingsTab}
                            />
                            <div style={{ display: settingsTab === 'general' ? 'block' : 'none' }}>
                                <GeneralSettingsPanel
                                    config={config}
                                    setConfig={setConfig}
                                    lang={lang}
                                    t={t}
                                    onLanguageChange={handleLangChange}
                                />
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

                            <div className="settings-panel" style={{ display: settingsTab === 'pet' ? 'block' : 'none' }}>
                                <PetSettingsPanel
                                    config={config}
                                    lang={lang}
                                    setConfig={setConfig}
                                    saveConfig={SaveConfig}
                                />
                            </div>

                            <div style={{ display: settingsTab === 'proxy' ? 'block' : 'none' }}>
                                <ProxySettingsPanel
                                    config={config}
                                    setConfig={setConfig}
                                    isWindows={isWindows}
                                    lang={lang}
                                    t={t}
                                />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'llm' ? 'block' : 'none' }}>
                                <LLMConfigPanel lang={lang} codexModels={config?.codex?.models} onStatusChange={(online: boolean, configured: boolean) => { setMaclawLLMOnline(online); setMaclawLLMConfigured(configured); }} />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'redeem' ? 'block' : 'none' }}>
                                <HubServiceRedeemPanel lang={lang} />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'memory' ? 'block' : 'none' }}>
                                <MemoryManagementPanel lang={lang} />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'embedding' ? 'block' : 'none' }}>
                                <EmbeddingConfigPanel lang={lang} />
                                <ASRConfigPanel lang={lang} />
                                <TTSConfigPanel lang={lang} />
                            </div>


                            <div className="settings-panel" style={{ display: settingsTab === 'agentnet' ? 'block' : 'none' }}>
                                <AgentNetPanel
                                    lang={lang}
                                    config={config}
                                    saveRemoteConfigField={saveRemoteConfigField}
                                    onRunningChange={setAgentNetRunning}
                                />
                            </div>

                            <div className="settings-panel" style={{ display: settingsTab === 'groupDiscussion' ? 'block' : 'none' }}>
                                <GroupDiscussionSettingsPanel config={config} saveRemoteConfigField={saveRemoteConfigField} lang={lang} />
                            </div>

                            <IMSettingsPanel
                                settingsTab={settingsTab}
                                config={config}
                                setConfig={setConfig}
                                lang={lang}
                                imSubTab={imSubTab}
                                setImSubTab={setImSubTab}
                                imAuditPlatform={imAuditPlatform}
                                setIMAuditPlatform={setIMAuditPlatform}
                                imAuditBtnStyle={imAuditBtnStyle}
                                saveRemoteConfigField={saveRemoteConfigField}
                                showToastMessage={showToastMessage}
                                qqBotStatus={qqBotStatus}
                                setQQBotStatus={setQQBotStatus}
                                qqBotLocalMode={qqBotLocalMode}
                                setQQBotLocalModeState={setQQBotLocalModeState}
                                telegramStatus={telegramStatus}
                                setTelegramStatus={setTelegramStatus}
                                telegramLocalMode={telegramLocalMode}
                                setTelegramLocalModeState={setTelegramLocalModeState}
                                weixinStatus={weixinStatus}
                                setWeixinStatus={setWeixinStatus}
                                weixinLocalMode={weixinLocalMode}
                                setWeixinLocalModeState={setWeixinLocalModeState}
                                thirdPartyGatewayStatus={thirdPartyGatewayStatus}
                                setThirdPartyGatewayStatus={setThirdPartyGatewayStatus}
                                thirdPartyGatewayLocalMode={thirdPartyGatewayLocalMode}
                                setThirdPartyGatewayLocalModeState={setThirdPartyGatewayLocalModeState}
                                weixinQRCode={weixinQRCode}
                                setWeixinQRCode={setWeixinQRCode}
                                weixinQRLoading={weixinQRLoading}
                                setWeixinQRLoading={setWeixinQRLoading}
                                weixinQRWaiting={weixinQRWaiting}
                                setWeixinQRWaiting={setWeixinQRWaiting}
                                weixinQRError={weixinQRError}
                                setWeixinQRError={setWeixinQRError}
                            />

                            <div className="settings-panel" style={{ display: settingsTab === 'security' ? 'block' : 'none' }}>
                                <SecurityPolicyPanel config={config} saveRemoteConfigField={saveRemoteConfigField} lang={lang} />
                            </div>

                            <div style={{ display: settingsTab === 'system' ? 'block' : 'none' }}>
                                <SystemSettingsPanel
                                    config={config}
                                    setConfig={setConfig}
                                    lang={lang}
                                    audioDevices={audioDevices}
                                    saveRemoteConfigField={saveRemoteConfigField}
                                    showToastMessage={showToastMessage}
                                />
                            </div>

                            <div style={{ display: settingsTab === 'ui' ? 'block' : 'none' }}>
                                <UISettingsPanel
                                    config={config}
                                    lang={lang}
                                    t={t}
                                    uiZoom={uiZoom}
                                    setUiZoom={setUiZoom}
                                    chatFontSize={chatFontSize}
                                    setChatFontSize={setChatFontSize}
                                    gossipAllowed={gossipAllowed}
                                    updateSidebarNavVisibility={updateSidebarNavVisibility}
                                />
                            </div>

                            <div style={{ display: settingsTab === 'display' ? 'block' : 'none' }}>
                                <ProgrammingToolsSettingsPanel
                                    config={config}
                                    setConfig={setConfig}
                                    lang={lang}
                                    remoteToolMetadata={remoteToolMetadata}
                                    toolProviders={toolProviders}
                                />
                            </div>

                            <div style={{ display: settingsTab === 'general' ? 'block' : 'none' }}>
                                <GeneralAdvancedSettingsPanel
                                    config={config}
                                    setConfig={setConfig}
                                    lang={lang}
                                    t={t}
                                    hasWindowsTerminal={hasWindowsTerminal}
                                    envCheckInterval={envCheckInterval}
                                    setEnvCheckInterval={setEnvCheckInterval}
                                />
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
                            appVersion={APP_VERSION}
                            buildNumber={buildNumber}
                            thanksContent={thanksContent}
                            t={t}
                            onOpenWebsite={() => BrowserOpenURL(brandInfo?.websiteURL || "https://maclaw.top")}
                            onCheckUpdate={() => {
                                setStatus(t("checkingUpdate"));
                                CheckUpdate(APP_VERSION).then((res: any) => {
                                    console.log("CheckUpdate result:", res);
                                    setUpdateResult(res);
                                    setIsStartupUpdateCheck(false);
                                    setShowUpdateModal(true);
                                    setStatus("");
                                }).catch((err: any) => {
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
                            onOpenGithub={() => BrowserOpenURL(MACLAW_CODE_REPOSITORY_URL)}
                        />
                    )}
                </div>

                {/* Global Action Bar (Footer) */}
                {config && isToolTab(navTab) && (
                    <div className="global-action-bar" data-ai-theme={aiThemeMode} style={{ '--wails-draggable': 'no-drag' } as any}>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: '4px', width: '100%', padding: '2px 0', '--wails-draggable': 'no-drag' } as any}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: '20px', justifyContent: 'flex-start' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: '8px' }}>
                                    {/* runnerStatus label removed */}
                                    <span style={{ fontSize: '0.85rem', fontWeight: 600, color: 'var(--primary-color)', textTransform: 'capitalize' }}>{activeTool}</span>
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
                                <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flexWrap: 'wrap' }}>
                                {activeTool !== 'kilo' && (
                                    <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: '#6b7280' }}>
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
                                            checked={resolvedLaunchProject?.team_mode || false}
                                            onChange={(e) => updateResolvedLaunchProject((project) => ({ ...project, team_mode: e.target.checked }))}
                                        />
                                        <span>{t("teamModeLabel")}</span>
                                    </label>
                                )}
                                {!isWindows && (
                                    <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: '#6b7280' }}>
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
                                <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: '#6b7280' }}>
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
                                    <div style={{ display: 'inline-flex', padding: '3px', borderRadius: '999px', border: '1px solid #e0e7ff', background: '#eef2ff' }}>
                                        <button
                                            type="button"
                                            onClick={() => {
                                                const newConfig = new main.AppConfig({ ...config, default_launch_mode: 'local', remote_enabled: false });
                                                setConfig(newConfig);
                                                SaveConfig(newConfig);
                                            }}
                                            style={{
                                                border: 'none',
                                                borderRadius: '999px',
                                                padding: '5px 12px',
                                                background: !launchRemoteEnabled ? '#6366f1' : 'transparent',
                                                color: !launchRemoteEnabled ? '#ffffff' : '#475569',
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
                                                const newConfig = new main.AppConfig({ ...config, default_launch_mode: 'remote', remote_enabled: true });
                                                setConfig(newConfig);
                                                SaveConfig(newConfig);
                                            }}
                                            style={{
                                                border: 'none',
                                                borderRadius: '999px',
                                                padding: '5px 12px',
                                                background: launchRemoteEnabled ? '#6366f1' : 'transparent',
                                                color: launchRemoteEnabled ? '#ffffff' : '#475569',
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
                                {launchRemoteEnabled && (
                                    <div
                                        style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '4px 10px', background: remoteActivationStatus?.activated ? '#f0fdf4' : '#fffbeb', border: `1px solid ${remoteActivationStatus?.activated ? '#bbf7d0' : '#fde68a'}`, borderRadius: '999px', cursor: remoteActivationStatus?.activated ? 'default' : 'pointer' }}
                                        onClick={() => {
                                            if (!remoteActivationStatus?.activated) {
                                                openRemoteActivationModal(activeTool);
                                            }
                                        }}
                                        title={remoteActivationStatus?.activated ? t("remoteActivated") : (lang === 'zh-Hans' ? '点击注册' : lang === 'zh-Hant' ? '點擊註冊' : 'Click to register')}
                                    >
                                        <span style={{ fontSize: '0.75rem', color: remoteActivationStatus?.activated ? '#16a34a' : '#d97706', whiteSpace: 'nowrap' }}>
                                            {remoteActivationStatus?.activated ? t("remoteActivated") : t("remoteRegister")}
                                        </span>
                                    </div>
                                )}
                                <label style={{ display: 'flex', alignItems: 'center', cursor: 'pointer', fontSize: '0.8rem', color: '#6b7280' }}>
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
                                        <span style={{ fontSize: '0.8rem', color: '#6b7280' }}>{t("pythonEnvLabel")}:</span>
                                        <select
                                            value={resolvedLaunchProject?.python_env || ""}
                                            onChange={(e) => updateResolvedLaunchProject((project) => ({ ...project, python_env: e.target.value }))}
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
                            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', width: '100%', gap: '10px', flexWrap: 'nowrap' }}>
                                <div style={{ display: 'flex', alignItems: 'center', gap: '10px', flex: '1 1 auto', minWidth: 0 }}>
                                    <div style={{ display: 'flex', alignItems: 'center', gap: '8px', flex: '1 1 auto', minWidth: 0 }}>
                                        <span style={{ fontSize: '0.8rem', color: '#6b7280', whiteSpace: 'nowrap', lineHeight: 1 }}>{t("project")}:</span>
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
                                                border: '1px solid #d1d5db',
                                                backgroundColor: '#ffffff',
                                                fontSize: '0.85rem',
                                                color: '#374151',
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
                                                border: '1px solid #d1d5db',
                                                backgroundColor: '#f3f4f6',
                                                color: '#6b7280',
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
                                </div>
                                {/* Handoff: local → remote icon button */}
                                {!launchRemoteEnabled && isRemoteCapableActiveTool && (
                                    <button
                                        type="button"
                                        title={lang === 'zh-Hans' ? '转为远程' : lang === 'zh-Hant' ? '轉為遠端' : 'Switch to Remote'}
                                        style={{
                                            width: '36px',
                                            height: '36px',
                                            borderRadius: '50%',
                                            border: '1px solid #c4b5fd',
                                            background: 'linear-gradient(135deg, #ede9fe, #f5f3ff)',
                                            color: '#7c3aed',
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
                                            e.currentTarget.style.background = 'linear-gradient(135deg, #ddd6fe, #ede9fe)';
                                            e.currentTarget.style.borderColor = '#a78bfa';
                                        }}
                                        onMouseLeave={(e) => {
                                            e.currentTarget.style.background = 'linear-gradient(135deg, #ede9fe, #f5f3ff)';
                                            e.currentTarget.style.borderColor = '#c4b5fd';
                                        }}
                                        onClick={async () => {
                                            if (!config?.remote_hub_url?.trim() || !remoteActivationStatus?.activated || !config?.remote_email?.trim()) {
                                                openRemoteActivationModal(activeTool);
                                                return;
                                            }
                                            setStatus(lang === 'zh-Hans' ? '正在转为远程...' : lang === 'zh-Hant' ? '正在轉為遠端...' : 'Switching to remote...');
                                            setLaunchingTool(activeTool);
                                            try {
                                                const newConfig = new main.AppConfig({ ...config, default_launch_mode: 'remote', remote_enabled: true });
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
                                        if (launchRemoteEnabled && hasActiveRemoteSessionForTool && activeRemoteSessionForTool?.id) {
                                            setLaunchingTool(activeTool);
                                            await killRemoteSession(activeRemoteSessionForTool.id);
                                            setStatus(lang === 'zh-Hans' ? '远程已停止' : lang === 'zh-Hant' ? '遠端已停止' : 'Remote stopped');
                                            setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                            return;
                                        }
                                        const selectedProj = resolvedLaunchProject;
                                        if (selectedProj && selectedProj.path && selectedProj.path.trim() !== "") {
                                            if (launchRemoteEnabled) {
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
                                    <span style={{ marginRight: '6px' }}>{launchRemoteEnabled ? '☁' : '➤'}</span>
                                    {launchRemoteEnabled
                                        ? (hasActiveRemoteSessionForTool ? t("remoteStopTool") : t("remoteStartTool"))
                                        : t("launch")}
                                </button>
                            </div>
                        </div>
                    </div>
                )}

                <AppStatusMessageBar
                    status={status}
                    lang={lang}
                    config={config}
                    qqBotStatus={qqBotStatus}
                    telegramStatus={telegramStatus}
                    weixinStatus={weixinStatus}
                    maclawLLMOnline={maclawLLMOnline}
                    maclawLLMConfigured={maclawLLMConfigured}
                    remoteActivated={!!remoteActivationStatus?.activated}
                    agentNetRunning={agentNetRunning}
                    navTab={navTab}
                    settingsTab={settingsTab}
                    backgroundInstallStatus={backgroundInstallStatus}
                    lobsterOffline={lobsterOffline}
                    lobsterHalf={lobsterHalf}
                    onOpenIMSettings={() => { setNavTab('settings'); setSettingsTab('im'); }}
                    onOpenLLMSettings={() => { setNavTab('settings'); setSettingsTab('llm'); }}
                />
            </>)}
            </div>

            {/* Modals */}
            {showRemoteActivationModal && (
                <RemoteActivationDialog
                    draft={remoteActivationDraft}
                    setDraft={setRemoteActivationDraft}
                    remoteCenterHubs={remoteCenterHubs}
                    loadingRemoteCenterHubs={loadingRemoteCenterHubs}
                    remoteBusy={remoteBusy}
                    t={t}
                    onLoadRemoteHubs={() => loadRemoteHubsFromCenter()}
                    onActivate={activateRemoteFromDialog}
                    onClose={() => { setShowRemoteActivationModal(false); setPendingRemoteLaunchTool(""); setRemoteCenterHubs([]); }}
                />
            )}

            {showInstallLog && (
                <InstallLogModal
                    envLogs={envLogs}
                    t={t}
                    onClose={() => setShowInstallLog(false)}
                    onCopied={() => showToastMessage(t("logsCopied"))}
                    onSendLog={async (hasError) => {
                        if (hasError) {
                            await performSendLog();
                        } else {
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
                />
            )}

            {/* Tool Repair Progress Dialog */}
            {toolRepairStatus.show && (
                <ToolRepairProgressDialog
                    status={toolRepairStatus}
                    t={t}
                    onClose={() => setToolRepairStatus(prev => ({ ...prev, show: false }))}
                />
            )}

            {showUpdateModal && updateResult && (
                <UpdateModal
                    updateResult={updateResult}
                    appVersion={APP_VERSION}
                    isDownloading={isDownloading}
                    downloadProgress={downloadProgress}
                    installerPath={installerPath}
                    downloadError={downloadError}
                    t={t}
                    onCancelDownload={handleCancelDownload}
                    onDownload={handleDownload}
                    onInstall={handleInstall}
                    onClose={() => {
                        setShowUpdateModal(false);
                        if (isStartupUpdateCheck && config && !config.hide_startup_popup) {
                            setShowStartupPopup(true);
                        }
                        setIsStartupUpdateCheck(false);
                        setDownloadError("");
                    }}
                />
            )}

            {showModelSettings && config && (
                <div className="modal-overlay">
                    <div className="modal-content" style={{ width: '529px', textAlign: 'left' }}>
                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '20px' }}>
                            <h3 style={{ margin: 0, color: '#6366f1' }}>{t("modelSettings")}</h3>
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
                                                const name = model.model_name.toLowerCase();
                                                let badge = null;

                                                if (model.has_subscription) {
                                                    badge = { bg: '#ec4899', label: t("subscription") };
                                                } else if (name.includes("glm") || name.includes("kimi") || name.includes("doubao") || name.includes("minimax")) {
                                                    badge = { bg: '#ec4899', label: t("monthly") };
                                                } else if (name.includes("deepseek")) {
                                                    badge = { bg: '#f59e0b', label: t("premium") };
                                                } else if (name.includes("xiaomi")) {
                                                    badge = { bg: '#f59e0b', label: t("bigSpender") };
                                                } else if (model.is_custom) {
                                                    badge = { bg: '#9ca3af', label: t("customized") };
                                                } else if (["aicodemirror", "aigocode", "noin.ai", "gaccode", "chatfire", "coderelay"].some(p => name.includes(p))) {
                                                    badge = { bg: '#14b8a6', label: t("forward") };
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
                                                                color: '#fff',
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
                                        {activeTool === 'codebuddy' && <span style={{ fontSize: '0.7rem', color: '#94a3b8', marginLeft: '5px' }}>(Supports multiple, separated by comma)</span>}
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
                                            style={!(config as any)[activeTool].models[activeTab].is_custom ? { backgroundColor: '#f3f4f6', cursor: 'not-allowed', color: '#9ca3af' } : {}}
                                        />
                                    </div>
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
                                        style={{ flex: 0.5, backgroundColor: '#93c5fd', color: '#4338ca', border: '1px solid #93c5fd' }}
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
                <ProviderSelectorDialog
                    providers={getFilteredProviders()}
                    providerFilter={providerFilter}
                    setProviderFilter={setProviderFilter}
                    selectedProvider={selectedProviderForUrl}
                    setSelectedProvider={setSelectedProviderForUrl}
                    hoveredProvider={hoveredProvider}
                    setHoveredProvider={setHoveredProvider}
                    lang={lang}
                    t={t}
                    onConfirm={confirmProviderSelection}
                    onClose={() => { setShowProviderSelector(false); setSelectedProviderForUrl(null); setHoveredProvider(null); }}
                />
            )}

            {showStartupPopup && (
                <StartupPopup
                    config={config}
                    setConfig={setConfig}
                    lang={lang}
                    t={t}
                    onClose={() => setShowStartupPopup(false)}
                />
            )}

            {/* MaClaw Onboarding Wizard */}
            {/* MaClaw Onboarding Wizard */}
            {showMaclawLLMPopup && (
                <OnboardingWizard
                    lang={lang}
                    hubUrl={config?.remote_hub_url || ""}
                    email={config?.remote_email || ""}
                    brandId={brandInfo?.id}
                    brandDisplayName={brandInfo?.displayName}
                    onClose={() => setShowMaclawLLMPopup(false)}
                    onLLMConfigured={() => {
                        setMaclawLLMOnline(true);
                        setMaclawLLMConfigured(true);
                        // Reload config to pick up CodeGen model injected into tool configs by SSO.
                        // Delay slightly to avoid racing with SaveConfig writing the file.
                        setTimeout(() => {
                            LoadConfig().then((c: any) => setConfig(c)).catch((err) => {
                                console.error("Failed to reload config after LLM configured:", err);
                                // Retry once after a short delay
                                setTimeout(() => {
                                    LoadConfig().then((c: any) => setConfig(c)).catch((err2) => {
                                        console.error("Retry reload config also failed:", err2);
                                    });
                                }, 1000);
                            });
                        }, 500);
                    }}
                    onRegistered={async () => {
                        console.info("[onboarding] App:onRegistered:start");
                        await refreshRemotePanel();
                        console.info("[onboarding] App:onRegistered:done");
                    }}
                    onSaveField={(patch) => {
                        setTimeout(() => {
                            void saveRemoteConfigField(patch as any);
                        }, 0);
                    }}
                />
            )}

            {/* Thanks Modal */}
            {showThanksModal && (
                <ThanksModal
                    content={thanksContent}
                    t={t}
                    onClose={() => setShowThanksModal(false)}
                />
            )}

            {/* Confirm Dialog */}
            {confirmDialog.show && (
                <ConfirmDialog
                    title={confirmDialog.title}
                    message={confirmDialog.message}
                    t={t}
                    onCancel={() => setConfirmDialog({ ...confirmDialog, show: false })}
                    onConfirm={confirmDialog.onConfirm}
                />
            )}

            {/* Proxy Settings Dialog (project-level only) */}
            {showProxySettings && config && (
                <ProjectProxySettingsDialog
                    config={config}
                    selectedProjectForLaunch={selectedProjectForLaunch}
                    setConfig={setConfig}
                    t={t}
                    saveLabel={localizeText("Save", "??", "??")}
                    onClose={() => setShowProxySettings(false)}
                />
            )}

            {showInstallSkillModal && config && (
                <InstallSkillModal
                    config={config}
                    skills={skills}
                    activeTool={activeTool}
                    installLocation={installLocation}
                    setInstallLocation={setInstallLocation}
                    installProject={installProject}
                    setInstallProject={setInstallProject}
                    selectedSkillsToInstall={selectedSkillsToInstall}
                    setSelectedSkillsToInstall={setSelectedSkillsToInstall}
                    isBatchInstalling={isBatchInstalling}
                    setIsBatchInstalling={setIsBatchInstalling}
                    isMarketplaceInstalling={isMarketplaceInstalling}
                    setIsMarketplaceInstalling={setIsMarketplaceInstalling}
                    t={t}
                    switchTool={switchTool}
                    showToastMessage={showToastMessage}
                    onClose={() => setShowInstallSkillModal(false)}
                />
            )}

            {showToast && (
                <div className="toast">
                    {toastMessage}
                </div>
            )}
                </div>
            </div>
        </div>
    );
}

export default App;
