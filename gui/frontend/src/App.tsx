import { useCallback, useEffect, useState, useRef, useMemo } from 'react';
import './App.css';
import { appVersion, buildNumber } from './version';
import appIcon from './assets/images/maclaw2.png';
import qianxinIcon from './assets/images/qianxin.png';
import lobsterOffline from './assets/images/lobster_offline.svg';
import lobsterHalf from './assets/images/lobster_half.svg';
import { CheckToolsStatus, CheckUpdate, InstallToolOnDemand, IsToolBeingInstalled, LoadConfig, SaveConfig, CheckEnvironment, ResizeWindow, LaunchTool, SelectProjectDir, SetLanguage, SetDefaultLaunchMode, GetUserHomeDir, ReadBBS, ReadTutorial, ReadThanks, ListPythonEnvironments, PackLog, ShowItemInFolder, GetSystemInfo, OpenSystemUrl, DownloadUpdate, CancelDownload, LaunchInstallerAndExit, ListSkills, ListSkillsWithInstallStatus, DeleteSkill, GetEnvCheckInterval, ShouldCheckEnvironment, UpdateLastEnvCheckTime, IsWindowsTerminalAvailable, ListRemoteHubs, ListToolProviders, PingMaclawLLM, GetQQBotStatus, GetTelegramStatus, GetWeixinStatus, GetWeixinLocalMode, GetQQBotLocalMode, GetTelegramLocalMode, GetLansengerStatus, GetLansengerLocalMode, GetThirdPartyGatewayStatus, GetThirdPartyGatewayLocalMode, IsGossipAllowed, GetBrandInfo, GetUIZoomFactor, GetChatFontSize, ListBackgroundLoops, GetAllLLMTokenUsage, GetMaclawLLMProviders, GetHubLLMServiceStatus, GroupDiscussionStatus, GroupDiscussionPublishProfile, GroupDiscussionProcessPendingInvites, GroupDiscussionAcceptInvite, GroupDiscussionRejectInvite, SearchProjects, CreateRecentTask, ResumeProject, RenameTask, PinTask, HideTask, GetDigitalEmployeeFeatureStatus, RespondDigitalEmployeeSensitiveRequest, FetchProviderModels } from "../wailsjs/go/main/App";

import { EventsOn, EventsOff, BrowserOpenURL, Quit, WindowHide, WindowFullscreen, WindowUnfullscreen, WindowIsFullscreen, WindowToggleMaximise, WindowIsMaximised } from "../wailsjs/runtime";
import { main } from "../wailsjs/go/models";
import { EVENT_PROJECT_INDEX_CHANGED, EVENT_TASKS_CHANGED } from './constants/events';
import { RemoteSettingsPanel } from './components/remote/RemoteSettingsPanel';
import { WebSearchConfigPanel } from './components/remote/WebSearchConfigPanel';
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
import { IMAuditPanel } from './components/remote/IMAuditPanel';
import { OnboardingWizard } from './components/remote/OnboardingWizard';
import { AIAssistantPanel } from './components/ai/AIAssistantPanel';
import type { VirtualEmployeeEntry } from './components/ai/VirtualEmployeeTab';
import { isDigitalEmployeeAuthorizationUsable, shouldShowDigitalEmployeeFeatureTabs } from './components/ai/digitalEmployeeFeature';
import type { HistoryDiscussionSummary } from './components/layout/SidebarHistorySessions';
import { activeCodingAgentProgress, latestCodingAgentTurnSnapshot } from './components/ai/CodingAgentProgressStatus';
import { readStoredAssistantThemeMode } from './components/ai/assistantThemeStorage';
import { PetSettingsPanel } from './components/PetSettingsPanel';
import { useAIAssistant } from './components/ai/useAIAssistant';
import { useDialog } from './components/CustomDialog';
import { buildHubCreditsURL } from './utils/hubCredits';
import { normalizeSidebarHubCredits } from './utils/sidebarHubCredits';
import { getSidebarUsageForProvider, selectSidebarCurrentProvider } from './utils/sidebarProviderSelection';
import { translations } from './i18n/appTranslations';
import { ToolConfiguration } from './components/tools/ToolConfiguration';
import { PROJECT_PAGE_SIZE, knownProviderEndpoints, recommendedModels, subscriptionUrls, getModelDisplayName, type ProviderEndpoint } from './config/providerCatalog';
import { TOOL_NAMES, isToolTab } from './config/toolCatalog';
import { getSettingsTabOptions, type SettingsTabId } from './config/settingsTabs';
import { SettingsTabsRail } from './components/settings/SettingsTabsRail';
import { GeneralSettingsPanel } from './components/settings/GeneralSettingsPanel';
import { KnowledgeSettingsPanel } from './components/settings/KnowledgeSettingsPanel';
import { MISDataSettingsPanel } from './components/settings/MISDataSettingsPanel';
import { UISettingsPanel } from './components/settings/UISettingsPanel';
import { ProgrammingToolsSettingsPanel } from './components/settings/ProgrammingToolsSettingsPanel';
import { GeneralAdvancedSettingsPanel } from './components/settings/GeneralAdvancedSettingsPanel';
import { SystemSettingsPanel } from './components/settings/SystemSettingsPanel';
import { ProxySettingsPanel } from './components/settings/ProxySettingsPanel';
import { IMSettingsPanel } from './components/settings/IMSettingsPanel';
import { AppSidebarShell } from './components/layout/AppSidebarShell';
import { FavoriteEmployeeReplacePicker } from './components/layout/FavoriteEmployeeReplacePicker';
import { countActiveSshBackgroundTasks, isActiveManageableBackgroundStatus } from './components/layout/backgroundTaskCount';
import { FavoriteEmployeeSettingsPanel } from './components/settings/FavoriteEmployeeSettingsPanel';
import { MAX_FAVORITE_EMPLOYEES, normalizeFavoriteEmployeeIds } from './components/settings/favoriteEmployees';
import { VirtualEmployeeSettingsPanel } from './components/settings/VirtualEmployeeSettingsPanel';
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
import { DataMigrationOverlay } from './components/DataMigrationOverlay';
import type { RemoteCenterHubOption, SidebarCurrentProviderTokenUsage, SidebarHubCredits, SidebarLLMProviderSummary, SidebarTokenUsageStat } from './types/appShell';





const APP_VERSION = appVersion
const MACLAW_CODE_REPOSITORY_URL = "https://github.com/rapidai/maclaw";


type SensitivePermissionRequest = {
    request_id: string;
    session_id?: string;
    query: string;
    timeout_seconds?: number;
};

type SidebarProviderStateWire = {
    providers?: Array<{ name?: string; Name?: string; url?: string; URL?: string; is_hub_service?: boolean; IsHubService?: boolean }>;
    Providers?: Array<{ name?: string; Name?: string; url?: string; URL?: string; is_hub_service?: boolean; IsHubService?: boolean }>;
    current?: string;
    Current?: string;
} | null;

function virtualEmployeeIdForMachine(machineId: string): string {
    const cleaned = String(machineId || '').trim().replace(/[\\/ ]/g, '_');
    return cleaned ? `ve_${cleaned}` : '';
}

function isOwnVirtualEmployeeId(id: string, machineId?: string): boolean {
    const normalizedId = String(id || '').trim().toLowerCase();
    const normalizedMachineId = String(machineId || '').trim().toLowerCase();
    if (!normalizedId || !normalizedMachineId) return false;
    return normalizedId === normalizedMachineId || normalizedId === virtualEmployeeIdForMachine(normalizedMachineId).toLowerCase();
}

function App() {
    const { showAlert } = useDialog();
    const [config, setConfig] = useState<main.AppConfig | null>(null);
    const [navTab, setNavTab] = useState<string>("ai");
    const audioDevices = useAudioDevices();
    const [aiPanelMaximized, setAiPanelMaximized] = useState(false);
    const [windowMaximized, setWindowMaximized] = useState(false);
    useEffect(() => {
        let debounceTimer: ReturnType<typeof setTimeout> | null = null;
        const syncMaximized = () => {
            if (debounceTimer) clearTimeout(debounceTimer);
            debounceTimer = setTimeout(() => {
                // Window state is a 3-state enum: normal | maximised | fullscreen.
                // The title bar restore button should show "restore" icon when the
                // window is in ANY non-normal state (maximised OR fullscreen).
                Promise.all([WindowIsMaximised(), WindowIsFullscreen()]).then(
                    ([isMax, isFs]) => setWindowMaximized(isMax || isFs)
                );
            }, 150);
        };
        window.addEventListener('resize', syncMaximized);
        return () => {
            window.removeEventListener('resize', syncMaximized);
            if (debounceTimer) clearTimeout(debounceTimer);
        };
    }, []);
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
    const [codexConfigUpdateCount, setCodexConfigUpdateCount] = useState(0);
    const [recentTasksPaneWidth, setRecentTasksPaneWidth] = useState(260);
    const [isRecentTasksResizing, setIsRecentTasksResizing] = useState(false);
    const recentTasksResizeStartX = useRef(0);
    const recentTasksResizeStartWidth = useRef(380);
    const [toolDropdownOpen, setToolDropdownOpen] = useState(false);
    const [taskContextMenu, setTaskContextMenu] = useState<{ x: number; y: number; projectPath: string; name: string; pinned: boolean } | null>(null);
    const [renamingTaskPath, setRenamingTaskPath] = useState<string | null>(null);
    const [renameValue, setRenameValue] = useState("");
    const [recentProjects, setRecentProjects] = useState<Array<{ id?: string; name?: string; project_path: string; workflow_type?: string; preview?: string; last_activity?: string; pinned?: boolean; has_output?: boolean }>>([]);
    const recentProjectsRef = useRef(recentProjects);
    recentProjectsRef.current = recentProjects;
    const [status, setStatus] = useState("");
    const [activeTab, setActiveTab] = useState(0);
    const [tabStartIndex, setTabStartIndex] = useState(0);
    const [settingsTab, setSettingsTab] = useState<SettingsTabId>('general');
    const [memoryTraceFocus, setMemoryTraceFocus] = useState<{ value: string; seq: number }>({ value: "", seq: 0 });
    const [imSubTab, setImSubTab] = useState<'qq' | 'telegram' | 'weixin' | 'lansenger' | 'thirdparty'>('qq');
    const [qqBotStatus, setQQBotStatus] = useState<string>('disconnected');
    const [qqBotLocalMode, setQQBotLocalModeState] = useState<boolean>(true);
    const [telegramStatus, setTelegramStatus] = useState<string>('disconnected');
    const [telegramLocalMode, setTelegramLocalModeState] = useState<boolean>(true);
    const [weixinStatus, setWeixinStatus] = useState<string>('disconnected');
    const [weixinLocalMode, setWeixinLocalModeState] = useState<boolean>(true);
    const [thirdPartyGatewayStatus, setThirdPartyGatewayStatus] = useState<string>('disconnected');
    const [thirdPartyGatewayLocalMode, setThirdPartyGatewayLocalModeState] = useState<boolean>(true);
    const [lansengerStatus, setLansengerStatus] = useState<string>('disconnected');
    const [lansengerLocalMode, setLansengerLocalModeState] = useState<boolean>(true);
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
    const [pendingVEOpen, setPendingVEOpen] = useState<VirtualEmployeeEntry | null>(null);
    const [pendingHistoryDiscussionOpen, setPendingHistoryDiscussionOpen] = useState<HistoryDiscussionSummary | null>(null);
    const [pendingProjectTabOpen, setPendingProjectTabOpen] = useState<{ projectPath: string; taskTitle: string; initialMessage?: string; autoSend?: boolean } | null>(null);

    // --- Favorite Employees state ---
    const [favoriteEmployeeIds, setFavoriteEmployeeIds] = useState<string[]>([]);
    const [veList, setVeList] = useState<VirtualEmployeeEntry[]>([]);
    const [digitalEmployeeFeatureStatus, setDigitalEmployeeFeatureStatus] = useState<any>({ visible: false, actual_count: 0 });
    const [showFavReplacePicker, setShowFavReplacePicker] = useState<{ ve: VirtualEmployeeEntry } | null>(null);

    // Load favorite employees from config
    useEffect(() => {
        setFavoriteEmployeeIds(normalizeFavoriteEmployeeIds(config?.favorite_employees));
    }, [config?.favorite_employees]);

    // Fetch VE list for sidebar favorites resolution
    useEffect(() => {
        if (!config?.remote_hub_url || !config?.remote_machine_id) return;
        let cancelled = false;
        let retryTimer: ReturnType<typeof setTimeout> | undefined;
        const fetchVeList = () => {
            import("../wailsjs/go/main/App").then((mod) => {
                if (cancelled) return;
                if ((mod as any).ListVirtualEmployees) {
                    (mod as any).ListVirtualEmployees().then((list: VirtualEmployeeEntry[]) => {
                        if (!cancelled && Array.isArray(list)) {
                            setVeList(list);
                        }
                    }).catch(() => {
                        // Retry once after 5s on failure to handle transient Hub unavailability
                        if (!cancelled && !retryTimer) {
                            retryTimer = setTimeout(() => { retryTimer = undefined; fetchVeList(); }, 5000);
                        }
                    });
                }
            }).catch(() => {});
        };
        fetchVeList();
        // Refresh on VE status changes
        const unsub1 = EventsOn("ve:list_update", fetchVeList);
        const unsub2 = EventsOn("ve:status_change", fetchVeList);
        return () => {
            cancelled = true;
            if (retryTimer) clearTimeout(retryTimer);
            if (typeof unsub1 === "function") unsub1(); else EventsOff("ve:list_update");
            if (typeof unsub2 === "function") unsub2(); else EventsOff("ve:status_change");
        };
    }, [config?.remote_hub_url, config?.remote_machine_id]);

    const refreshDigitalEmployeeFeatureStatus = useCallback(() => {
        return GetDigitalEmployeeFeatureStatus()
            .then((status: any) => setDigitalEmployeeFeatureStatus(status || { visible: false }))
            .catch(() => setDigitalEmployeeFeatureStatus({ visible: false, reason: 'unavailable' }));
    }, []);

    useEffect(() => {
        let cancelled = false;
        const refresh = () => {
            GetDigitalEmployeeFeatureStatus()
                .then((status: any) => { if (!cancelled) setDigitalEmployeeFeatureStatus(status || { visible: false }); })
                .catch(() => { if (!cancelled) setDigitalEmployeeFeatureStatus({ visible: false, reason: 'unavailable' }); });
        };
        refresh();
        const subscriptions = [
            ["digital-employee-authorization-changed", EventsOn("digital-employee-authorization-changed", refresh)] as const,
            ["ve:status_change", EventsOn("ve:status_change", refresh)] as const,
            ["ve:list_update", EventsOn("ve:list_update", refresh)] as const,
            ["ve:approved", EventsOn("ve:approved", refresh)] as const,
            ["ve:rejected", EventsOn("ve:rejected", refresh)] as const,
            ["ve:disabled", EventsOn("ve:disabled", refresh)] as const,
        ];
        return () => {
            cancelled = true;
            subscriptions.forEach(([name, unsubscribe]) => {
                if (typeof unsubscribe === "function") unsubscribe();
                else EventsOff(name);
            });
        };
    }, [config?.remote_hub_url, config?.remote_machine_id, veList.length]);

    useEffect(() => {
        let cancelled = false;
        let timer: number | undefined;
        const expiresAt = digitalEmployeeFeatureStatus?.authorization?.expires_at;
        const schedule = () => {
            if (!expiresAt) return;
            const expiresAtMs = new Date(expiresAt).getTime();
            if (!Number.isFinite(expiresAtMs)) return;
            const remainingMs = expiresAtMs - Date.now();
            if (remainingMs <= 0) {
                void refreshDigitalEmployeeFeatureStatus();
                return;
            }
            timer = window.setTimeout(() => {
                if (cancelled) return;
                if (new Date(expiresAt).getTime() <= Date.now()) {
                    void refreshDigitalEmployeeFeatureStatus();
                } else {
                    schedule();
                }
            }, Math.min(remainingMs + 1000, 60 * 60 * 1000));
        };
        schedule();
        return () => {
            cancelled = true;
            if (timer !== undefined) window.clearTimeout(timer);
        };
    }, [digitalEmployeeFeatureStatus?.authorization?.expires_at, refreshDigitalEmployeeFeatureStatus]);

    // Settings still require usable authorization, but the main navigation should stay available
    // when Hub configuration, cached employees, or favorites already prove the feature is reachable.
    const digitalEmployeeAuthorizationUsable = isDigitalEmployeeAuthorizationUsable(digitalEmployeeFeatureStatus?.authorization);
    const hasDigitalEmployeeHubConfig = Boolean(config?.remote_hub_url && config?.remote_machine_id);
    const veNavigationAvailable = shouldShowDigitalEmployeeFeatureTabs(digitalEmployeeFeatureStatus)
        || digitalEmployeeAuthorizationUsable
        || hasDigitalEmployeeHubConfig
        || veList.length > 0
        || favoriteEmployeeIds.length > 0;
    const veAuthorized = veNavigationAvailable;
    const veSettingsAuthorized = digitalEmployeeAuthorizationUsable;
    useEffect(() => {
        if (!veNavigationAvailable && settingsTab === 'virtualEmployee') setSettingsTab('general');
    }, [settingsTab, veNavigationAvailable]);
    useEffect(() => {
        if (veAuthorized) return;
        setPendingVEOpen(null);
        setPendingHistoryDiscussionOpen(null);
    }, [veAuthorized]);
    // Resolve favorite IDs to display slots
    const favoriteEmployeeSlots = useMemo(() => {
        return favoriteEmployeeIds.flatMap(id => {
            if (isOwnVirtualEmployeeId(id, config?.remote_machine_id)) return [];
            const ve = veList.find(v => v.id === id);
            if (ve && (isOwnVirtualEmployeeId(ve.id, config?.remote_machine_id) || isOwnVirtualEmployeeId(ve.machine_id || '', config?.remote_machine_id))) return [];
            return { veId: id, name: ve?.name || id.slice(0, 6), online: ve?.online_status === 'online', skillDescription: ve?.skill_description || '' };
        });
    }, [favoriteEmployeeIds, veList, config?.remote_machine_id]);

    const updateFavoriteEmployees = useCallback(async (newList: string[]) => {
        const normalized = normalizeFavoriteEmployeeIds(newList);
        setFavoriteEmployeeIds(normalized);
        try {
            const latest = await LoadConfig();
            const updated = new main.AppConfig({ ...latest, favorite_employees: normalized } as any);
            await SaveConfig(updated);
            setConfig(updated);
        } catch {}
    }, []);

    const handleSetFavoriteEmployee = useCallback((ve: VirtualEmployeeEntry) => {
        if (favoriteEmployeeIds.includes(ve.id)) return;
        // Ensure the VE is in veList so favoriteEmployeeSlots can resolve it immediately.
        // Without this, if veList hasn't loaded yet or the fetch failed, the slot would
        // show a fallback name (id.slice(0,6)) instead of the actual VE name.
        setVeList(prev => prev.some(v => v.id === ve.id) ? prev : [...prev, ve]);
        if (favoriteEmployeeIds.length < MAX_FAVORITE_EMPLOYEES) {
            updateFavoriteEmployees([...favoriteEmployeeIds, ve.id]);
        } else {
            setShowFavReplacePicker({ ve });
        }
    }, [favoriteEmployeeIds, updateFavoriteEmployees]);

    const handleReplaceFavorite = useCallback((index: number) => {
        if (!showFavReplacePicker) return;
        if (index < 0 || index >= favoriteEmployeeIds.length) {
            setShowFavReplacePicker(null);
            return;
        }
        const ve = showFavReplacePicker.ve;
        // Ensure the VE is in veList for immediate resolution (same as handleSetFavoriteEmployee)
        setVeList(prev => prev.some(v => v.id === ve.id) ? prev : [...prev, ve]);
        const newList = [...favoriteEmployeeIds];
        newList[index] = ve.id;
        updateFavoriteEmployees(newList);
        setShowFavReplacePicker(null);
    }, [favoriteEmployeeIds, showFavReplacePicker, updateFavoriteEmployees]);

    const handleRemoveFavoriteEmployee = useCallback((veId: string) => {
        updateFavoriteEmployees(favoriteEmployeeIds.filter(id => id !== veId));
    }, [favoriteEmployeeIds, updateFavoriteEmployees]);

    const handleReorderFavorites = useCallback((newOrder: string[]) => {
        updateFavoriteEmployees(newOrder);
    }, [updateFavoriteEmployees]);

    const handleStartFavoriteVEConversation = useCallback((veId: string) => {
        const ve = veList.find(v => v.id === veId);
        if (ve) {
            setPendingVEOpen(ve);
        } else {
            // VE not in list yet (still loading or removed): create a minimal entry to open the tab
            setPendingVEOpen({ id: veId, name: veId.slice(0, 8), skill_description: '', access_policy: 'public', status: 'active', online_status: 'offline' });
        }
    }, [veList]);

    // Brand info from backend
    const [brandInfo, setBrandInfo] = useState<{id: string, displayName: string, displayNameCN: string, slogan: string, author: string, businessContact: string, websiteURL: string, githubURL: string, iconPath: string} | null>(null);
    const [brandInfoLoaded, setBrandInfoLoaded] = useState(false);
    const currentIcon = brandInfo?.id === 'qianxin' ? qianxinIcon : appIcon;
    const [aiThemeMode, setAIThemeMode] = useState<'light' | 'dark'>(() => {
        return readStoredAssistantThemeMode();
    });
    const brandDisplayTitle = brandInfo ? `${brandInfo.displayNameCN} ${brandInfo.displayName}` : '\u7801\u5361\u9f99 MaClaw';
    const brandSidebarName = brandInfo?.displayName || 'MaClaw';
    const isTigerClawBrand = brandInfo?.id === 'qianxin';
    
    // MaClaw LLM online status (lobster indicator)
    const [maclawLLMOnline, setMaclawLLMOnline] = useState<boolean>(false);
    const [maclawLLMConfigured, setMaclawLLMConfigured] = useState<boolean>(false);
    const [sidebarCurrentProviderTokenUsage, setSidebarCurrentProviderTokenUsage] = useState<SidebarCurrentProviderTokenUsage>({ provider: '', isHubService: false, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 });
    const [sidebarHubCredits, setSidebarHubCredits] = useState<SidebarHubCredits | null>(null);
    const sidebarTokenUsageSeqRef = useRef(0);
    const maclawLLMFirstPingDone = useRef(false);    const maclawLLMFirstPingResult = useRef<{online: boolean; configured: boolean} | null>(null);

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

    // Clear fetched model list when switching provider tabs
    useEffect(() => { setFetchedModelList([]); }, [activeTab, activeTool]);

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
    const [sensitivePermissionRequest, setSensitivePermissionRequest] = useState<SensitivePermissionRequest | null>(null);
    const sensitivePermissionRequestRef = useRef<SensitivePermissionRequest | null>(null);
    const [sensitivePermissionQueue, setSensitivePermissionQueue] = useState<SensitivePermissionRequest[]>([]);
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
    const [fetchedModelList, setFetchedModelList] = useState<{id: string; name?: string}[]>([]);
    const [fetchingModelList, setFetchingModelList] = useState(false);
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


    useEffect(() => {
        sensitivePermissionRequestRef.current = sensitivePermissionRequest;
    }, [sensitivePermissionRequest]);

    useEffect(() => {
        const unsubscribe = EventsOn('digital-employee-sensitive-request', (payload: SensitivePermissionRequest) => {
            const requestId = String(payload?.request_id || '').trim();
            if (!requestId) return;
            const request = { ...payload, request_id: requestId };
            const current = sensitivePermissionRequestRef.current;
            if (!current) {
                sensitivePermissionRequestRef.current = request;
                setSensitivePermissionRequest(request);
            } else if (current.request_id !== requestId) {
                setSensitivePermissionQueue(queue => queue.some(item => item.request_id === requestId) ? queue : [...queue, request]);
            }
            const timeoutMs = Math.max(1, Number(payload?.timeout_seconds || 60)) * 1000;
            window.setTimeout(() => {
                setSensitivePermissionRequest(current => {
                    if (current?.request_id !== requestId) return current;
                    sensitivePermissionRequestRef.current = null;
                    return null;
                });
                setSensitivePermissionQueue(queue => queue.filter(item => item.request_id !== requestId));
            }, timeoutMs);
        });
        return () => {
            if (typeof unsubscribe === 'function') unsubscribe();
            else EventsOff('digital-employee-sensitive-request');
        };
    }, []);

    useEffect(() => {
        if (sensitivePermissionRequest || sensitivePermissionQueue.length === 0) return;
        sensitivePermissionRequestRef.current = sensitivePermissionQueue[0];
        setSensitivePermissionRequest(sensitivePermissionQueue[0]);
        setSensitivePermissionQueue(queue => queue.slice(1));
    }, [sensitivePermissionQueue, sensitivePermissionRequest]);

    const respondSensitivePermission = useCallback(async (decision: 'allow' | 'deny') => {
        const request = sensitivePermissionRequest;
        if (!request) return;
        sensitivePermissionRequestRef.current = null;
        setSensitivePermissionRequest(null);
        setSensitivePermissionQueue(queue => queue.filter(item => item.request_id !== request.request_id));
        try {
            await RespondDigitalEmployeeSensitiveRequest(request.request_id, decision);
        } catch (err: any) {
            showToastMessage(err?.message || String(err || localizeText('Failed to respond', '响应失败', '回應失敗')));
        }
    }, [sensitivePermissionRequest]);

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
        if (name === "Claude Official Documentation Skill Package" || name === "\u8d85\u80fd\u529b\u6280\u80fd\u5305") {
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

    const handleWindowMaximizeToggle = (e?: React.MouseEvent) => {
        if (e) {
            e.preventDefault();
            e.stopPropagation();
        }
        // If the AI panel is in fullscreen mode, WindowToggleMaximise won't
        // Windows quirk: drag-to-maximize can break if we are already fullscreen.
        // (aiPanelMaximized) to avoid an async round-trip on every click.
        if (aiPanelMaximized) {
            WindowUnfullscreen();
            setAiPanelMaximized(false);
            setWindowMaximized(false);
        } else {
            setWindowMaximized(m => !m); // optimistic update for instant icon feedback
            WindowToggleMaximise();
        }
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
            setRecentTasksPaneWidth(Math.min(460, Math.max(180, nextWidth)));
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
        }).catch(() => {
            setBrandInfo(null);
        }).finally(() => {
            setBrandInfoLoaded(true);
        });

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
            ResizeWindow(867, 554);
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
                    // Users can open it manually via the provider config button.
                }
            }
        }).catch(err => {
            console.error("Failed to load config on startup:", err);
            setStatus(localizeText("Error loading config: ", "加载配置失败：", "載入設定失敗：") + err);
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
                    // Avoid leaving the UI stuck on loading config forever.
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
            // Sync with tray menu changes, but don't yank the user away from
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
            setBackgroundInstallStatus(`Checking ${toolName}...`);
            setBackgroundInstallingTool("");  // Clear previous tool's installing state
        });

        EventsOn("tool-installing", (toolName: string) => {
            setBackgroundInstallStatus(`Installing ${toolName}...`);
            setBackgroundInstallingTool(toolName);
        });

        EventsOn("tool-updating", (toolName: string) => {
            setBackgroundInstallStatus(`Updating ${toolName}...`);
            setBackgroundInstallingTool(toolName);
        });

        EventsOn("tool-installed", (toolName: string) => {
            console.log("Tool installed in background:", toolName);
            setBackgroundInstallStatus(`${toolName} installed`);
            setBackgroundInstallingTool("");
            setTimeout(() => setBackgroundInstallStatus(""), 3000);
            // Refresh tool statuses
            CheckToolsStatus().then(statuses => {
                setToolStatuses(statuses);
            });
        });

        EventsOn("tool-updated", (toolName: string) => {
            console.log("Tool updated in background:", toolName);
            setBackgroundInstallStatus(`${toolName} updated`);
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

    useEffect(() => {
        if (!brandInfoLoaded) return;
        if (!isTigerClawBrand) {
            setLansengerStatus('disconnected');
            setLansengerLocalModeState(true);
            return;
        }
        EventsOn("lansenger-status-changed", (status: string) => {
            setLansengerStatus(status);
        });
        GetLansengerStatus().then(setLansengerStatus).catch(() => {});
        GetLansengerLocalMode().then(setLansengerLocalModeState).catch(() => {});
        return () => {
            EventsOff("lansenger-status-changed");
        };
    }, [brandInfoLoaded, isTigerClawBrand]);

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
            // message tab removed: redirect to AI assistant
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
            // Don't persist 'ai' as active_tool; it's a UI nav state, not a coding tool
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

    useEffect(() => {
                const visibleSettingsTabs = getSettingsTabOptions(lang, {});
        if (!visibleSettingsTabs.some((tab) => tab.id === settingsTab)) {
            setSettingsTab('general');
        }
    }, [isTigerClawBrand, lang, navTab, settingsTab]);

    useEffect(() => {
        if (!isTigerClawBrand && imSubTab === 'lansenger') {
            setImSubTab('qq');
        }
    }, [isTigerClawBrand, imSubTab]);

    const handleSkillContext = (e: React.MouseEvent, skillName: string) => {
        e.preventDefault();
        e.stopPropagation();

        if (skillName === "Claude Official Documentation Skill Package" || skillName === "\u8d85\u80fd\u529b\u6280\u80fd\u5305") {
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

    const handleOpenExperienceTrace = useCallback((focus?: string) => {
        setMemoryTraceFocus((prev) => ({ value: String(focus || "").trim(), seq: prev.seq + 1 }));
        setNavTab('settings');
        setSettingsTab('memory');
    }, []);

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
    const codingAgentTurnSnapshot = useMemo(
        () => aiAssistant.sending ? latestCodingAgentTurnSnapshot(aiAssistant.progressMessages || []) : null,
        [aiAssistant.sending, aiAssistant.progressMessages],
    );
    const codingAgentProgress = useMemo(
        () => codingAgentTurnSnapshot?.latest || activeCodingAgentProgress(aiAssistant.progressMessages || [], aiAssistant.sending),
        [codingAgentTurnSnapshot, aiAssistant.sending, aiAssistant.progressMessages],
    );
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
        EventsOn(EVENT_PROJECT_INDEX_CHANGED, refresh);
        EventsOn(EVENT_TASKS_CHANGED, refresh);
        return () => {
            EventsOff(EVENT_PROJECT_INDEX_CHANGED);
            EventsOff(EVENT_TASKS_CHANGED);
        };
    }, [refreshRecentProjects]);

    const resumeRecentProject = useCallback(async (projectPath: string) => {
        try {
            switchTool('ai');
            // Find the task title from recentProjects for the tab label
            const proj = recentProjectsRef.current.find(p => p.project_path === projectPath);
            const title = proj?.name || projectPath.split(/[\\/]/).pop() || projectPath;
            // Open (or activate) the project tab. If the tab is new, autoSend
            // sends the task title as the first message. If the tab already has
            // conversation history (duplicate), it just activates without sending.
            setPendingProjectTabOpen({
                projectPath,
                taskTitle: title,
                autoSend: true,
            });
        } catch (error) {
            console.error("resumeRecentProject failed:", error);
        }
    }, [switchTool]);

    const createRecentTask = useCallback(async (name: string) => {
        const taskName = name.trim();
        if (!taskName) return;
        try {
            const created = await CreateRecentTask(taskName);
            if (created?.project_path) {
                setRecentProjects(prev => [created, ...prev.filter(item => item.project_path !== created.project_path)].slice(0, 10));
            } else {
                refreshRecentProjects();
                return;
            }
            switchTool('ai');
            setPendingProjectTabOpen({
                projectPath: created.project_path,
                taskTitle: created.name || taskName,
                autoSend: true,
            });
            refreshRecentProjects();
        } catch (error) {
            console.error("CreateRecentTask failed:", error);
        }
    }, [refreshRecentProjects, switchTool]);

    const normalizeSidebarProviderState = useCallback((data?: SidebarProviderStateWire) => {
        const list = (data?.providers ?? data?.Providers ?? [])
            .map((provider): SidebarLLMProviderSummary => ({
                name: provider?.name ?? provider?.Name ?? '',
                url: provider?.url ?? provider?.URL ?? '',
                isHubService: !!(provider?.is_hub_service ?? provider?.IsHubService),
            }))
            .filter((provider) => !!provider.name);
        const current = data?.current ?? data?.Current ?? '';
        return { providers: list, current };
    }, []);

    const refreshSidebarTokenUsage = useCallback(async () => {
        const refreshSeq = ++sidebarTokenUsageSeqRef.current;
        const normalizeProviderURL = (value?: string) => String(value || '').trim().replace(/\/+$/, '');
        try {
            const [usageMap, providerState] = await Promise.all([
                GetAllLLMTokenUsage() as Promise<Record<string, SidebarTokenUsageStat> | null>,
                GetMaclawLLMProviders() as Promise<SidebarProviderStateWire>,
            ]);
            const normalizedMap = usageMap || {};
            const normalizedProviderState = normalizeSidebarProviderState(providerState);
            const providerSummaries = normalizedProviderState.providers.length > 0
                ? normalizedProviderState.providers
                : providers.map((provider) => ({ name: provider.name, url: (provider as any).url || (provider as any).URL || '', isHubService: !!(provider as any).is_hub_service || !!(provider as any).IsHubService })).filter((provider) => !!provider.name);
            const currentProviderName = selectSidebarCurrentProvider(
                providerSummaries,
                normalizedProviderState.current || selectedProvider || providers[0]?.name || '',
                normalizedMap,
            );
            const currentProviderUsage = getSidebarUsageForProvider(normalizedMap, currentProviderName);
            const currentProvider = providerSummaries.find((provider) => provider.name === currentProviderName);
            let hubStatus: Awaited<ReturnType<typeof GetHubLLMServiceStatus>> | null = null;
            try {
                hubStatus = await GetHubLLMServiceStatus();
            } catch {
                hubStatus = null;
            }
            const hubServiceURL = normalizeProviderURL(hubStatus?.hub_llm_base_url ?? hubStatus?.HubLLMBaseURL);
            const currentProviderURL = normalizeProviderURL(currentProvider?.url);
            const currentProviderIsHubService = !!currentProvider?.isHubService || (!!hubServiceURL && !!currentProviderURL && hubServiceURL === currentProviderURL);
            const hubCredits = currentProviderIsHubService ? normalizeSidebarHubCredits(hubStatus) : null;
            if (refreshSeq !== sidebarTokenUsageSeqRef.current) return;
            setSidebarCurrentProviderTokenUsage({ provider: currentProviderName, isHubService: currentProviderIsHubService, ...currentProviderUsage });
            setSidebarHubCredits(hubCredits);
        } catch {
            if (refreshSeq !== sidebarTokenUsageSeqRef.current) return;
            setSidebarCurrentProviderTokenUsage({ provider: '', isHubService: false, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0 });
            setSidebarHubCredits(null);
        }
    }, [normalizeSidebarProviderState, providers, selectedProvider]);

    useEffect(() => {
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
        const offTokenUsageChanged = EventsOn("llm-token-usage-changed", onTokenUsageChanged);
        const offHubLLMServiceChanged = EventsOn("hub-llm-service-changed", onTokenUsageChanged);
        const usageRefreshTimer = window.setInterval(() => { void refreshSidebarTokenUsage(); }, 10 * 60 * 1000);
        return () => {
            sidebarTokenUsageSeqRef.current += 1;
            window.clearInterval(usageRefreshTimer);
            delayedRefreshTimers.forEach((timer) => window.clearTimeout(timer));
            if (typeof offTokenUsageChanged === 'function') offTokenUsageChanged(); else EventsOff("llm-token-usage-changed");
            if (typeof offHubLLMServiceChanged === 'function') offHubLLMServiceChanged(); else EventsOff("hub-llm-service-changed");
        };
    }, [refreshSidebarTokenUsage]);

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

    // Track manageable background loops for the sidebar badge and system status.
    const [sidebarBgLoops, setSidebarBgLoops] = useState<any[]>([]);
    useEffect(() => {
        let cancelled = false;
        const refresh = async () => {
            try {
                const loops = await ListBackgroundLoops();
                if (!cancelled) setSidebarBgLoops(Array.isArray(loops) ? loops : []);
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
        const bgCount = sidebarBgLoops.filter((loop: any) => isActiveManageableBackgroundStatus(loop?.status ?? loop?.Status)).length;
        return remoteCount + bgCount;
    }, [remoteSessions, sidebarBgLoops]);

    const sshBackgroundTaskCount = useMemo(() => countActiveSshBackgroundTasks(sidebarBgLoops), [sidebarBgLoops]);

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
        setFetchedModelList([]);

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
        setFetchedModelList([]);
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

        // Exclude providers already used by another model to avoid duplicate custom tabs.
        if (config) {
            const toolCfg = (config as any)[activeTool];
            if (toolCfg?.models) {
                const existingNames = new Set(
                    toolCfg.models
                        .filter((_: any, idx: number) => idx !== activeTab)
                        .map((m: any) => (m.model_name || '').toLowerCase().trim())
                );
                filtered = filtered.filter(p => !existingNames.has(p.name.toLowerCase().trim()));
            }
        }

        return filtered;
    };

    // Confirm provider selection and fill URL
    const confirmProviderSelection = (provider?: ProviderEndpoint) => {
        const selectedProvider = provider || selectedProviderForUrl;
        if (selectedProvider) {
            if (config) {
                setFetchedModelList([]);
                const toolCfg = JSON.parse(JSON.stringify((config as any)[activeTool]));
                const currentModel = toolCfg.models?.[activeTab];
                if (currentModel) {
                    const oldName = currentModel.model_name;
                    currentModel.model_url = selectedProvider.url;
                    if (currentModel.is_custom) {
                        const otherNames = new Set(
                            toolCfg.models
                                .filter((_: any, idx: number) => idx !== activeTab)
                                .map((m: any) => (m.model_name || '').toLowerCase().trim())
                        );
                        const shouldUseProviderName = !oldName || /^Custom\d*$/i.test(oldName.trim());
                        if (shouldUseProviderName) {
                            let nextName = selectedProvider.name;
                            let suffix = 2;
                            while (otherNames.has(nextName.toLowerCase().trim())) {
                                nextName = `${selectedProvider.name} ${suffix}`;
                                suffix += 1;
                            }
                            currentModel.model_name = nextName;
                            if (toolCfg.current_model === oldName) {
                                toolCfg.current_model = nextName;
                            }
                        }
                    }
                    if (!currentModel.model_id) {
                        currentModel.model_id = getDefaultModelId(activeTool, selectedProvider.name);
                    }
                    if (activeTool === 'codex' && !currentModel.wire_api) {
                        currentModel.wire_api = 'responses';
                    }
                    const newConfig = new main.AppConfig({ ...config, [activeTool]: toolCfg });
                    setConfig(newConfig);
                }
            } else {
                handleModelUrlChange(selectedProvider.url);
            }
            setShowProviderSelector(false);
            setSelectedProviderForUrl(null);
            setHoveredProvider(null);
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
            setStatus(localizeText(
                `Name "${name}" already exists, please use a different name`,
                `名称“${name}”已存在，请使用其他名称`,
                `名稱「${name}」已存在，請使用其他名稱`,
            ));
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

    const getWireApiValue = () => {
        if (!config) return "";
        const model = (config as any)[activeTool]?.models?.[activeTab];
        const wireApi = model?.wire_api || "";
        return activeTool === "codex" && !wireApi ? "responses" : wireApi;
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

    const getKnownModelOptions = (tool: string, provider: string): { id: string; name?: string }[] => {
        const names = [provider, getProviderPrefix(provider)].filter(Boolean);
        const recommended = names.flatMap(name => recommendedModels[name] || []);
        const options = recommended.map(model => ({
            id: model.id,
            name: model.note ? `${model.id} (${model.note})` : model.id,
        }));
        const defaultId = getDefaultModelId(tool, provider);
        if (defaultId && !options.some(model => model.id === defaultId)) {
            options.unshift({ id: defaultId, name: defaultId });
        }
        return options;
    };

    const handleModelSwitch = (modelName: string) => {
        if (!config) return;

        const toolCfg = (config as any)[activeTool];
        if (!toolCfg || toolCfg.current_model === modelName) return;
        const targetModel = toolCfg.models.find((m: any) => m.model_name === modelName);
        if (modelName !== "Original" && (!targetModel || !targetModel.api_key || targetModel.api_key.trim() === "")) {
            setStatus(localizeText("Please configure API Key first!", "请先配置 API Key！", "請先配置 API Key！"));
            const idx = toolCfg.models.findIndex((m: any) => m.model_name === modelName);
            if (idx !== -1) setActiveTab(idx);

            setShowModelSettings(true);
            setTimeout(() => setStatus(""), 2000);
            return;
        }

        const newToolCfg = { ...toolCfg, current_model: modelName };
        const newConfig = new main.AppConfig({ ...config, [activeTool]: newToolCfg });
        const isCodexProviderSwitch = activeTool === "codex";
        setConfig(newConfig);
        setStatus(t("syncing"));
        if (isCodexProviderSwitch) {
            setCodexConfigUpdateCount((count) => count + 1);
        }
        SaveConfig(newConfig).then(() => {
            setStatus(t("switched"));
            setTimeout(() => setStatus(""), 1500);
        }).catch(err => {
            setStatus(localizeText("Error syncing: ", "同步失败：", "同步失敗：") + err);
        }).finally(() => {
            if (isCodexProviderSwitch) {
                setCodexConfigUpdateCount((count) => Math.max(0, count - 1));
            }
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
            setStatus(lang === 'zh-Hans' ? '正在启动远程...' : lang === 'zh-Hant' ? '正在啟動遠端...' : 'Starting remotely...');
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
            setStatus(localizeText("Error saving: ", "保存失败：", "儲存失敗：") + err);
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
                ? `Please attach the zip file (aicoder_log_....zip) from the opened folder to this email.\n\n`
                : lang === 'zh-Hant'
                    ? `Please attach the zip file (aicoder_log_....zip) from the opened folder to this email.\n\n`
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
            <div data-ai-theme={aiThemeMode} className="app-loading-shell">
                <div className="app-loading-drag-zone" />
                <h2 className="app-loading-title">{t("envCheckTitle")}</h2>
                <div className="app-loading-progress" aria-hidden="true">
                    <div className="app-loading-progress__bar" />
                </div>

                {showLogs ? (
                    <textarea
                        ref={logEndRef}
                        readOnly
                        value={envLogs.join('\n')}
                        className="app-loading-log"
                    />
                ) : (
                    <div className="app-loading-status">
                        {envLogs.length > 0 ? envLogs[envLogs.length - 1] : t("initializing")}
                    </div>
                )}

                <div className="app-loading-actions">
                    <button
                        onClick={() => setShowLogs(!showLogs)}
                        className="app-loading-link"
                    >
                        {showLogs ? (lang === 'zh-Hans' ? '\u9690\u85cf\u8be6\u60c5' : lang === 'zh-Hant' ? '\u96b1\u85cf\u8a73\u60c5' : 'Hide Details') : (lang === 'zh-Hans' ? '\u67e5\u770b\u8be6\u60c5' : lang === 'zh-Hant' ? '\u67e5\u770b\u8a73\u60c5' : 'Show Details')}
                    </button>

                    {showLogs && (
                        isManualCheck ? (
                            <button onClick={() => {
                                setIsLoading(false);
                                setIsManualCheck(false);
                            }} className="btn-hide app-loading-action app-loading-action--primary">
                                {lang === 'zh-Hans' ? '\u6536\u8d77' : lang === 'zh-Hant' ? '\u6536\u8d77' : 'Hide'}
                            </button>
                        ) : (
                            <button onClick={Quit} className="btn-hide app-loading-action app-loading-action--danger">
                                {lang === 'zh-Hans' ? '\u9000\u51fa\u7a0b\u5e8f' : lang === 'zh-Hant' ? '\u9000\u51fa\u7a0b\u5f0f' : 'Quit'}
                            </button>
                        )
                    )}
                </div>

            </div>
        );
    }

    if (!config) return <div className="main-content app-config-loading">{t("loadingConfig")}</div>;

    const toolCfg = isToolTab(navTab)
        ? (config as any)[navTab]
        : null;

    const currentProject = getCurrentProject();
    const settingsTabOptions = getSettingsTabOptions(lang, { hideVirtualEmployee: !veNavigationAvailable });
    const isRemoteCapableActiveTool = remoteToolMetadata.some(
        (meta) => meta.name === activeTool && meta.supports_remote === true
    );
    const launchMode = config?.default_launch_mode === 'remote' ? 'remote' : 'local';
    const launchRemoteEnabled = launchMode === 'remote';
    const setLaunchMode = async (mode: 'local' | 'remote') => {
        if (!config) return;
        const newConfig = new main.AppConfig({
            ...config,
            default_launch_mode: mode,
            remote_enabled: mode === 'remote',
        });
        setConfig(newConfig);
        try {
            await SetDefaultLaunchMode(mode);
            const freshConfig = await LoadConfig();
            setConfig(freshConfig);
        } catch (err) {
            setStatus(localizeText("Error: ", "错误：", "錯誤：") + err);
            try {
                const freshConfig = await LoadConfig();
                setConfig(freshConfig);
            } catch {
                // Keep the optimistic UI state if recovery load fails.
            }
        }
    };
    const codexConfigUpdating = codexConfigUpdateCount > 0;
    return (
        <div
            className="app-viewport"
            style={{ ['--ui-scale' as any]: String(uiZoom) } as React.CSSProperties}
        >
            <DataMigrationOverlay />
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
                showLansenger={isTigerClawBrand}
                remoteActivationStatus={remoteActivationStatus}
                qqBotStatus={qqBotStatus}
                telegramStatus={telegramStatus}
                weixinStatus={weixinStatus}
                lansengerStatus={lansengerStatus}
                runningTaskCount={runningTaskCount}
                sshBackgroundTaskCount={sshBackgroundTaskCount}
                t={t}
                gossipAllowed={gossipAllowed}
                config={config}
                activeTool={activeTool}
                toolDropdownOpen={toolDropdownOpen}
                setToolDropdownOpen={setToolDropdownOpen}
                recentProjects={recentProjects}
                renamingTaskPath={renamingTaskPath}
                setRenamingTaskPath={setRenamingTaskPath}
                renameValue={renameValue}
                setRenameValue={setRenameValue}
                resumeRecentProject={resumeRecentProject}
                assistantReady={aiAssistant.ready}
                onRecentTaskSwitchBlocked={() => showToastMessage(localizeText('System is warming up. Please switch later.', '系统正在预热，请稍后切换。', '系統正在預熱，請稍後切換。'))}
                createRecentTask={createRecentTask}
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
                codingAgentProgress={codingAgentProgress}
                codingAgentTurnSnapshot={codingAgentTurnSnapshot}
                handleRecentTasksResizeStart={handleRecentTasksResizeStart}
                isRecentTasksResizing={isRecentTasksResizing}
                onOpenVEConversation={(ve) => { switchTool('ai'); setPendingVEOpen(ve); }}
                onOpenHistoryDiscussion={(discussion) => { switchTool('ai'); setPendingHistoryDiscussionOpen(discussion); }}
                favoriteEmployees={favoriteEmployeeSlots}
                veAuthorized={veAuthorized}
                digitalEmployeeFeatureStatus={digitalEmployeeFeatureStatus}
                showDigitalEmployeeNavigation={veNavigationAvailable}
                onStartVEConversation={handleStartFavoriteVEConversation}
                onReorderFavorites={handleReorderFavorites}
                onSetFavoriteEmployee={handleSetFavoriteEmployee}
                onRemoveFavoriteEmployee={(ve) => handleRemoveFavoriteEmployee(ve.id)}
                favoriteEmployeeIds={favoriteEmployeeIds}
                showCodingToolEntry={!!(config as any)?.show_coding_tool_entry}
            />
            <div className="main-container" data-ai-theme={aiThemeMode}>
                {/* AI assistant as main content (both lite and pro modes) */}
                {navTab === 'ai' ? (
                    <div className="ai-main-panel-shell">
                        <AIAssistantPanel
                            onClose={() => { switchTool('settings'); }}
                            lang={lang}
                            chatFontSize={chatFontSize}
                            themeMode={aiThemeMode}
                            onThemeModeChange={setAIThemeMode}
                            audioInputDeviceId={(config as any)?.audio_input_device_id || ''}
                            audioOutputDeviceId={(config as any)?.audio_output_device_id || ''}
                            pendingVEOpen={pendingVEOpen}
                            onPendingVEOpenHandled={() => setPendingVEOpen(null)}
                            pendingHistoryDiscussionOpen={pendingHistoryDiscussionOpen}
                            onPendingHistoryDiscussionOpenHandled={() => setPendingHistoryDiscussionOpen(null)}
                            pendingProjectTabOpen={pendingProjectTabOpen}
                            onPendingProjectTabOpenHandled={() => setPendingProjectTabOpen(null)}
                            state={{
                                ...aiAssistant.panelState,
                                selectedFilePath: aiAssistant.selectedFilePaths?.[0] ?? "",
                                onboardingIncomplete: !config?.onboarding_done && !showMaclawLLMPopup,
                                showTraceEntry: !!config?.show_ai_trace_entry,
                            }}
                            actions={{
                                ...aiAssistant.panelActions,
                                onOpenOnboarding: () => setShowMaclawLLMPopup(true),
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
                    handleWindowMaximizeToggle={handleWindowMaximizeToggle}
                    windowMaximized={windowMaximized}
                />

                <div className="main-content elegant-scrollbar app-main-content" data-nav-tab={navTab}>
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
                        <div className="settings-shell settings-shell--padded">
                            <SettingsTabsRail
                                tabs={settingsTabOptions}
                                activeTab={settingsTab}
                                onChange={setSettingsTab}
                            />
                            <div className="settings-content settings-content--stacked" hidden={settingsTab !== 'general'}>
                                <GeneralSettingsPanel
                                    config={config}
                                    setConfig={setConfig}
                                    lang={lang}
                                    t={t}
                                    onLanguageChange={handleLangChange}
                                />
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

                            <div className="settings-content settings-panel" hidden={settingsTab !== 'remote'}>
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

                            <div className="settings-content settings-panel" hidden={settingsTab !== 'searchEngine'}>
                                <WebSearchConfigPanel lang={lang} />
                            </div>

                            <div className="settings-content settings-panel" hidden={settingsTab !== 'pet'}>
                                <PetSettingsPanel
                                    config={config}
                                    lang={lang}
                                    setConfig={setConfig}
                                    saveConfig={SaveConfig}
                                />
                            </div>

                            <div className="settings-content" hidden={settingsTab !== 'proxy'}>
                                <ProxySettingsPanel
                                    config={config}
                                    setConfig={setConfig}
                                    isWindows={isWindows}
                                    lang={lang}
                                    t={t}
                                />
                            </div>

                            <div className="settings-content settings-panel" hidden={settingsTab !== 'llm'}>
                                <LLMConfigPanel
                                    lang={lang}
                                    codexModels={config?.codex?.models}
                                    onStatusChange={(online: boolean, configured: boolean) => { setMaclawLLMOnline(online); setMaclawLLMConfigured(configured); }}
                                    onProviderChanged={() => { void refreshSidebarTokenUsage(); }}
                                />
                            </div>

                            <div className="settings-content settings-panel" hidden={settingsTab !== 'redeem'}>
                                <HubServiceRedeemPanel lang={lang} />
                            </div>

                            <div className="settings-content settings-panel" hidden={settingsTab !== 'memory'}>
                                <MemoryManagementPanel lang={lang} traceFocus={memoryTraceFocus} />
                            </div>

                            <div className="settings-content settings-panel" hidden={settingsTab !== 'knowledge'}>
                                <KnowledgeSettingsPanel lang={lang} />
                            </div>

                            <div className="settings-content settings-panel" hidden={settingsTab !== 'misData'}>
                                <MISDataSettingsPanel lang={lang} />
                            </div>
                            <div className="settings-content settings-panel" hidden={settingsTab !== 'embedding'}>
                                <EmbeddingConfigPanel lang={lang} />
                                <ASRConfigPanel lang={lang} />
                                <TTSConfigPanel lang={lang} />
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
                                showLansenger={isTigerClawBrand}
                                lansengerStatus={lansengerStatus}
                                setLansengerStatus={setLansengerStatus}
                                lansengerLocalMode={lansengerLocalMode}
                                setLansengerLocalModeState={setLansengerLocalModeState}
                                weixinQRCode={weixinQRCode}
                                setWeixinQRCode={setWeixinQRCode}
                                weixinQRLoading={weixinQRLoading}
                                setWeixinQRLoading={setWeixinQRLoading}
                                weixinQRWaiting={weixinQRWaiting}
                                setWeixinQRWaiting={setWeixinQRWaiting}
                                weixinQRError={weixinQRError}
                                setWeixinQRError={setWeixinQRError}
                            />

                            <div className="settings-content settings-panel" hidden={settingsTab !== 'security'}>
                                <SecurityPolicyPanel config={config} saveRemoteConfigField={saveRemoteConfigField} lang={lang} />
                            </div>
                            {veNavigationAvailable && (
                                <div className="settings-content settings-panel" hidden={settingsTab !== 'virtualEmployee'}>
                                    <div className="settings-ve-section">
                                        <FavoriteEmployeeSettingsPanel
                                            favoriteEmployeeIds={favoriteEmployeeIds}
                                            veList={veList}
                                            onAdd={(veId) => handleSetFavoriteEmployee(veList.find(v => v.id === veId) || { id: veId, name: veId, skill_description: '', access_policy: 'public', status: 'active', online_status: 'offline' })}
                                            onRemove={handleRemoveFavoriteEmployee}
                                            onReorder={handleReorderFavorites}
                                            lang={lang}
                                        />
                                    </div>
                                    {veSettingsAuthorized && (
                                        <div className="settings-ve-section settings-ve-section--divided">
                                            <VirtualEmployeeSettingsPanel remoteMachineId={config?.remote_machine_id || ''} lang={lang} />
                                        </div>
                                    )}
                                </div>
                            )}

                            <div className="settings-content" hidden={settingsTab !== 'system'}>
                                <SystemSettingsPanel
                                    config={config}
                                    setConfig={setConfig}
                                    lang={lang}
                                    audioDevices={audioDevices}
                                    saveRemoteConfigField={saveRemoteConfigField}
                                    showToastMessage={showToastMessage}
                                />
                            </div>

                            <div className="settings-content" hidden={settingsTab !== 'ui'}>
                                <UISettingsPanel
                                    config={config}
                                    lang={lang}
                                    t={t}
                                    uiZoom={uiZoom}
                                    setUiZoom={setUiZoom}
                                    chatFontSize={chatFontSize}
                                    setChatFontSize={setChatFontSize}
                                />
                            </div>

                            <div className="settings-content" hidden={settingsTab !== 'display'}>
                                <ProgrammingToolsSettingsPanel
                                    config={config}
                                    setConfig={setConfig}
                                    lang={lang}
                                    remoteToolMetadata={remoteToolMetadata}
                                    toolProviders={toolProviders}
                                />
                            </div>
                        </div>
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
                                    setStatus((lang === 'zh-Hans' ? '检查更新失败：' : lang === 'zh-Hant' ? '檢查更新失敗：' : 'Update check failed: ') + err);
                                    setUpdateResult({
                                        has_update: false,
                                        latest_version: lang === 'zh-Hans' ? "获取失败" : lang === 'zh-Hant' ? "取得失敗" : "Fetch failed",
                                        release_url: ""
                                    });
                                    setIsStartupUpdateCheck(false);
                                    setShowUpdateModal(true);
                                });
                            }}
                            onShowInstallLog={() => setShowInstallLog(true)}
                            onOpenBugReport={() => BrowserOpenURL("https://github.com/rapidai/maclaw/issues/new")}
                            onOpenGithub={() => BrowserOpenURL(MACLAW_CODE_REPOSITORY_URL)}
                        />
                    )}
                </div>

                {/* Global Action Bar (Footer) */}
                {config && isToolTab(navTab) && (
                    <div className="global-action-bar" data-ai-theme={aiThemeMode}>
                        <div className="coding-launch-panel wails-no-drag">
                            <div className="coding-launch-meta-row">
                                <div className="coding-launch-summary">
                                    {/* runnerStatus label removed */}
                                    <span className="coding-launch-tool-name">{activeTool}</span>
                                    <span
                                        className="coding-launch-provider-name"
                                        title={(config as any)[activeTool].current_model === "Original" ? t("original") : (config as any)[activeTool].current_model}
                                    >
                                        {(() => {
                                            const modelName = (config as any)[activeTool].current_model === "Original" ? t("original") : (config as any)[activeTool].current_model;
                                            return modelName.length > 10 ? `${modelName.slice(0, 4)}...${modelName.slice(-4)}` : modelName;
                                        })()}
                                    </span>
                                </div>
                                <div className="coding-launch-options">
                                {activeTool !== 'kilo' && (
                                    <label className="coding-launch-check">
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
                                            <span className="coding-launch-danger-badge">
                                                {t("danger")}
                                            </span>
                                        )}
                                    </label>
                                )}
                                {activeTool === 'claude' && (
                                    <label className="coding-launch-check">
                                        <input
                                            type="checkbox"
                                            checked={resolvedLaunchProject?.team_mode || false}
                                            onChange={(e) => updateResolvedLaunchProject((project) => ({ ...project, team_mode: e.target.checked }))}
                                        />
                                        <span>{t("teamModeLabel")}</span>
                                    </label>
                                )}
                                {!isWindows && (
                                    <label className="coding-launch-check">
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
                                            className="coding-launch-inline-action"
                                            role="button"
                                            tabIndex={0}
                                            onClick={(e) => {
                                                e.preventDefault();
                                                e.stopPropagation();
                                                setShowProxySettings(true);
                                            }}
                                            onKeyDown={(e) => {
                                                if (e.key === 'Enter' || e.key === ' ') {
                                                    e.preventDefault();
                                                    e.stopPropagation();
                                                    setShowProxySettings(true);
                                                }
                                            }}
                                            title={t("proxySettings")}
                                        >
                                            {lang === 'zh-Hans' ? '设置' : lang === 'zh-Hant' ? '設定' : 'Edit'}
                                        </span>
                                    </label>
                                )}
                                <label className="coding-launch-check">
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
                                    />
                                    <span>{isWindows ? t("adminModeLabel") : t("rootModeLabel")}</span>
                                </label>
                            </div>
                            </div>
                            <div className="coding-launch-mode-row">
                                <div className="coding-launch-mode-group">
                                    <div className="coding-launch-segmented">
                                        <button
                                            type="button"
                                            onClick={() => { void setLaunchMode('local'); }}
                                            className={!launchRemoteEnabled ? 'is-active' : ''}
                                        >
                                            {t("localModeLabel")}
                                        </button>
                                        <button
                                            type="button"
                                            onClick={() => {
                                                if (!isRemoteCapableActiveTool) return;
                                                void setLaunchMode('remote');
                                            }}
                                            className={launchRemoteEnabled ? 'is-active' : ''}
                                            title={isRemoteCapableActiveTool ? t("remoteModeDesc") : localizeText("This tool does not support remote mode", "此工具不支持远程模式", "此工具不支援遠端模式")}
                                        >
                                            {t("remoteModeLabel")}
                                        </button>
                                    </div>
                                </div>
                                {launchRemoteEnabled && (
                                    <div
                                        className={`coding-launch-status-pill ${remoteActivationStatus?.activated ? 'is-active' : 'needs-action'}`}
                                        onClick={() => {
                                            if (!remoteActivationStatus?.activated) {
                                                openRemoteActivationModal(activeTool);
                                            }
                                        }}
                                        title={remoteActivationStatus?.activated ? t("remoteActivated") : (lang === 'zh-Hans' ? '点击注册' : lang === 'zh-Hant' ? '點擊註冊' : 'Click to register')}
                                    >
                                        <span>
                                            {remoteActivationStatus?.activated ? t("remoteActivated") : t("remoteRegister")}
                                        </span>
                                    </div>
                                )}
                                <label className="coding-launch-check">
                                    <input
                                        type="checkbox"
                                        checked={resolvedLaunchProject?.python_project || false}
                                        onChange={(e) => updateResolvedLaunchProject((project) => ({ ...project, python_project: e.target.checked }))}
                                    />
                                    <span>{t("pythonProjectLabel")}</span>
                                </label>
                                {resolvedLaunchProject?.python_project && (
                                    <div className="coding-launch-python-env">
                                        <span className="coding-launch-python-label">{t("pythonEnvLabel")}:</span>
                                        <select
                                            value={resolvedLaunchProject?.python_env || ""}
                                            onChange={(e) => updateResolvedLaunchProject((project) => ({ ...project, python_env: e.target.value }))}
                                            className="coding-launch-python-select"
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
                            <div className="coding-launch-project-row">
                                <div className="coding-launch-project-main">
                                    <div className="coding-launch-project-picker">
                                        <span className="coding-launch-project-label">{t("project")}</span>
                                        <input
                                            type="text"
                                            className="form-input coding-launch-project-search"
                                            value={launchProjectKeyword}
                                            onChange={(e) => setLaunchProjectKeyword(e.target.value)}
                                            placeholder={t("projectSearchPlaceholder")}
                                            spellCheck={false}
                                            autoComplete="off"
                                        />
                                        <select
                                            value={resolvedLaunchProject?.id || ""}
                                            onChange={(e) => setSelectedProjectForLaunch(e.target.value)}
                                            className="coding-launch-select"
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
                                            className="coding-launch-manage"
                                        >
                                            ...
                                        </button>
                                    </div>
                                </div>
                                {/* Handoff: local to remote icon button */}
                                {!launchRemoteEnabled && isRemoteCapableActiveTool && (
                                    <button
                                        type="button"
                                        title={lang === 'zh-Hans' ? '切换到远程' : lang === 'zh-Hant' ? '切換到遠端' : 'Switch to Remote'}
                                        className="coding-launch-handoff"
                                        onClick={async () => {
                                            if (!config?.remote_hub_url?.trim() || !remoteActivationStatus?.activated || !config?.remote_email?.trim()) {
                                                openRemoteActivationModal(activeTool);
                                                return;
                                            }
                                            setStatus(lang === 'zh-Hans' ? '正在切换到远程...' : lang === 'zh-Hant' ? '正在切換到遠端...' : 'Switching to remote...');
                                            setLaunchingTool(activeTool);
                                            try {
                                                await setLaunchMode('remote');
                                                await quickStartRemoteSession(activeTool as any, "handoff");
                                                setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                            } catch (err) {
                                                setStatus(localizeText("Error: ", "错误：", "錯誤：") + err);
                                                setLaunchingTool("");
                                            }
                                        }}
                                    >
                                        ↗
                                    </button>
                                )}
                                <button
                                    className="btn-launch coding-launch-button"
                                    disabled={onDemandInstallingTool === activeTool || backgroundInstallingTool === activeTool || launchingTool === activeTool}
                                    onClick={async () => {
                                        console.log("Launch button clicked. activeTool:", activeTool);
                                        if (launchRemoteEnabled && hasActiveRemoteSessionForTool && activeRemoteSessionForTool?.id) {
                                            setLaunchingTool(activeTool);
                                            await killRemoteSession(activeRemoteSessionForTool.id);
                                            setStatus(localizeText('Remote stopped', '远程已停止', '遠端已停止'));
                                            setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                            return;
                                        }
                                        const selectedProj = resolvedLaunchProject;
                                        if (selectedProj && selectedProj.path && selectedProj.path.trim() !== "") {
                                            if (launchRemoteEnabled) {
                                                if (remoteToolMetadata.length > 0 && !isRemoteCapableActiveTool) {
                                                    setStatus(localizeText("This tool does not support remote launch", "此工具不支持远程启动", "此工具不支援遠端啟動"));
                                                    return;
                                                }
                                                if (!config?.remote_hub_url?.trim() || !remoteActivationStatus?.activated || !config?.remote_email?.trim()) {
                                                    openRemoteActivationModal(activeTool);
                                                    return;
                                                }
                                                setStatus(localizeText("Starting remotely...", "正在启动远程...", "正在啟動遠端..."));
                                                setLaunchingTool(activeTool);
                                                try {
                                                    await quickStartRemoteSession(activeTool as any);
                                                    setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                                } catch (err) {
                                                    setStatus(localizeText("Error: ", "错误：", "錯誤：") + err);
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
                                                    setStatus(localizeText(
                                                        `${activeTool} is being installed in background, please wait...`,
                                                        `${activeTool} 正在后台安装，请稍候...`,
                                                        `${activeTool} 正在背景安裝，請稍候...`,
                                                    ));
                                                    setOnDemandInstallingTool(activeTool);
                                                    try {
                                                        await InstallToolOnDemand(activeTool);
                                                        // Refresh tool statuses
                                                        const updatedStatuses = await CheckToolsStatus();
                                                        setToolStatuses(updatedStatuses);
                                                        setStatus(localizeText(`${activeTool} installed`, `${activeTool} 已安装`, `${activeTool} 已安裝`));
                                                        setOnDemandInstallingTool("");
                                                        // Auto launch
                                                        setTimeout(async () => {
                                                            setStatus(localizeText("Launching...", "启动中...", "啟動中..."));
                                                            setLaunchingTool(activeTool);
                                                            try {
                                                                await LaunchTool(activeTool, selectedProj.yolo_mode, selectedProj.admin_mode || false, selectedProj.python_project || false, selectedProj.python_env || "", selectedProj.path || "", selectedProj.use_proxy || false);
                                                                setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                                            } catch (err) {
                                                                setStatus(localizeText("Error: ", "错误：", "錯誤：") + err);
                                                                setLaunchingTool("");
                                                            }
                                                        }, 500);
                                                    } catch (err) {
                                                        setStatus(localizeText("Error: ", "错误：", "錯誤：") + err);
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
                                                        setStatus(localizeText("Launching...", "启动中...", "啟動中..."));
                                                        setLaunchingTool(activeTool);
                                                        try {
                                                            await LaunchTool(activeTool, selectedProj.yolo_mode, selectedProj.admin_mode || false, selectedProj.python_project || false, selectedProj.python_env || "", selectedProj.path || "", selectedProj.use_proxy || false);
                                                            console.log("LaunchTool call returned successfully after install");
                                                            setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                                        } catch (err) {
                                                            console.error("LaunchTool call failed after install:", err);
                                                            setStatus(localizeText("Error: ", "错误：", "錯誤：") + err);
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
                                            setStatus(localizeText("Launching...", "启动中...", "啟動中..."));
                                            setLaunchingTool(activeTool);
                                            LaunchTool(activeTool, selectedProj.yolo_mode, selectedProj.admin_mode || false, selectedProj.python_project || false, selectedProj.python_env || "", selectedProj.path || "", selectedProj.use_proxy || false)
                                                .then(() => {
                                                    console.log("LaunchTool call returned successfully");
                                                    setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                                })
                                                .catch(err => {
                                                    console.error("LaunchTool call failed:", err);
                                                    setStatus(localizeText("Error: ", "错误：", "錯誤：") + err);
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
                                    <span className="coding-launch-state-dot" aria-hidden="true" />
                                    {launchRemoteEnabled
                                        ? (hasActiveRemoteSessionForTool ? t("remoteStopTool") : t("remoteStartTool"))
                                        : t("launch")}
                                </button>
                            </div>
                        </div>
                    </div>
                )}

                {codexConfigUpdating && (
                    <div className="codex-config-progress-overlay" role="status" aria-live="polite">
                        <div className="codex-config-progress-panel">
                            <div className="codex-config-progress-title">
                                {lang === 'zh-Hans' || lang === 'zh'
                                    ? "Updating Codex configuration"
                                    : lang === 'zh-Hant'
                                        ? "Updating Codex configuration"
                                        : 'Updating Codex configuration'}
                            </div>
                            <div className="codex-config-progress-track" aria-hidden="true">
                                <div className="codex-config-progress-bar" />
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
                    lansengerStatus={lansengerStatus}
                    maclawLLMOnline={maclawLLMOnline}
                    maclawLLMConfigured={maclawLLMConfigured}
                    remoteActivated={!!remoteActivationStatus?.activated}
                    showLansenger={isTigerClawBrand}
                    navTab={navTab}
                    settingsTab={settingsTab}
                    backgroundInstallStatus={backgroundInstallStatus}
                    lobsterOffline={lobsterOffline}
                    lobsterHalf={lobsterHalf}
                    onOpenIMSettings={() => { setNavTab('settings'); setSettingsTab('im'); }}
                    onOpenLLMSettings={() => { setNavTab('settings'); setSettingsTab('llm'); }}
                    codingAgentProgress={codingAgentProgress}
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
                    onUpdateResultChange={setUpdateResult}
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
                    <div className="modal-content provider-config-modal">
                        <div className="provider-config-header">
                            <h3>{t("modelSettings")}</h3>
                            <button className="modal-close" onClick={() => setShowModelSettings(false)}>&times;</button>
                        </div>

                        <div className="provider-config-model-tabs-wrap">
                            {(() => {
                                const allModels = (config as any)[activeTool].models;
                                // Filter: show only non-Original models
                                const customModels = allModels.filter((m: any) => m.is_custom);
                                const nonCustomModels = allModels.filter((m: any) => !m.is_custom && m.model_name !== "Original");

                                // Always show all custom models (user can add/remove them)
                                const configurableModels = [...nonCustomModels, ...customModels];
                                const showArrows = configurableModels.length >= 5;

                                return (
                                    <div className="tabs provider-config-tabs">
                                        {showArrows && (
                                            <div className="provider-config-arrow-slot">
                                                {tabStartIndex > 0 && (
                                                    <button
                                                        className="provider-config-arrow-button"
                                                        onClick={() => setTabStartIndex(Math.max(0, tabStartIndex - 1))}
                                                        aria-label={localizeText("Previous providers", "上一页服务商", "上一頁服務商")}
                                                    >
                                                        {'<'}
                                                    </button>
                                                )}
                                            </div>
                                        )}

                                        <div className="provider-config-tab-strip">
                                            {(showArrows ? configurableModels.slice(tabStartIndex, tabStartIndex + 4) : configurableModels).map((model: any, index: number) => {
                                                const globalIndex = allModels.findIndex((m: any) => m.model_name === model.model_name);
                                                const name = model.model_name.toLowerCase();
                                                let badge: { tone: 'accent' | 'warning' | 'neutral' | 'success'; label: string } | null = null;

                                                if (model.has_subscription) {
                                                    badge = { tone: 'accent', label: t("subscription") };
                                                } else if (name.includes("glm") || name.includes("kimi") || name.includes("doubao") || name.includes("minimax")) {
                                                    badge = { tone: 'accent', label: t("monthly") };
                                                } else if (name.includes("deepseek")) {
                                                    badge = { tone: 'warning', label: t("premium") };
                                                } else if (name.includes("xiaomi")) {
                                                    badge = { tone: 'warning', label: t("bigSpender") };
                                                } else if (model.is_custom) {
                                                    badge = { tone: 'neutral', label: t("customized") };
                                                } else if (["aicodemirror", "aigocode", "noin.ai", "gaccode", "chatfire", "coderelay"].some(p => name.includes(p))) {
                                                    badge = { tone: 'success', label: t("forward") };
                                                }

                                                return (
                                                    <button
                                                        key={globalIndex}
                                                        className={`tab-button provider-config-tab-button ${activeTab === globalIndex ? 'active' : ''}`}
                                                        onClick={() => setActiveTab(globalIndex)}
                                                    >
                                                        {getModelDisplayName(model.model_name, lang)}
                                                        {badge && (
                                                            <span className="provider-config-tab-badge" data-tone={badge.tone}>
                                                                {badge.label}
                                                            </span>
                                                        )}
                                                    </button>
                                                );
                                            })}
                                        </div>

                                        {showArrows && (
                                            <div className="provider-config-arrow-slot">
                                                {tabStartIndex + 4 < configurableModels.length && (
                                                    <button
                                                        className="provider-config-arrow-button"
                                                        onClick={() => setTabStartIndex(Math.min(configurableModels.length - 4, tabStartIndex + 1))}
                                                        aria-label={localizeText("Next providers", "下一页服务商", "下一頁服務商")}
                                                    >
                                                        {'>'}
                                                    </button>
                                                )}
                                            </div>
                                        )}
                                    </div>
                                );
                            })()}
                        </div>

                        <div className="provider-config-form-row">
                            {(config as any)[activeTool].models[activeTab].is_custom && (
                                <div className="form-group provider-config-form-group">
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
                                <div className="form-group provider-config-form-group">
                                    <label className="form-label">
                                        {t("modelName")}
                                        {activeTool === 'codebuddy' && <span className="provider-config-model-label-hint">{localizeText("(Supports multiple, separated by comma)", "（支持多个，用逗号分隔）", "（支援多個，用逗號分隔）")}</span>}
                                    </label>
                                    <div className="provider-config-model-select-shell">
                                        <div className="provider-config-model-select-row">
                                            {(() => {
                                                const currentModel = (config as any)[activeTool].models[activeTab];
                                                const modelOptions = fetchedModelList.length > 0
                                                    ? fetchedModelList
                                                    : getKnownModelOptions(activeTool, currentModel.model_name);
                                                return (
                                                    <select
                                                        className="form-input provider-config-model-select"
                                                        data-field="model-id"
                                                        value={currentModel.model_id || ''}
                                                        onChange={(e) => handleModelIdChange(e.target.value)}
                                                        disabled={fetchingModelList || modelOptions.length === 0}
                                                    >
                                                        <option value="">
                                                            {fetchingModelList
                                                                ? localizeText("Loading...", "加载中...", "載入中...")
                                                                : modelOptions.length === 0
                                                                    ? localizeText("Click Models to fetch first", "请先点击“模型”获取列表", "請先點擊「模型」取得列表")
                                                                    : localizeText("Select a model", "选择模型", "選擇模型")}
                                                        </option>
                                                        {currentModel.model_id && !modelOptions.some(m => m.id === currentModel.model_id) && (
                                                            <option value={currentModel.model_id}>
                                                                {localizeText("Current: ", "当前：", "目前：")}{currentModel.model_id}
                                                            </option>
                                                        )}
                                                        {modelOptions.map((m, i) => (
                                                            <option key={`${m.id}-${i}`} value={m.id}>
                                                                {m.name && m.name !== m.id ? `${m.name} (${m.id})` : m.id}
                                                            </option>
                                                        ))}
                                                    </select>
                                                );
                                            })()}
                                            <button
                                                className="btn-link provider-config-fetch-button"
                                                disabled={fetchingModelList}
                                                data-loading={fetchingModelList ? 'true' : 'false'}
                                                onClick={async () => {
                                                    const currentModel = (config as any)[activeTool]?.models?.[activeTab];
                                                    if (!currentModel) return;
                                                    const url = currentModel.model_url;
                                                    const key = currentModel.api_key;
                                                    if (!url || !key) {
                                                        setStatus(localizeText("Please fill in API URL and API Key first", "请先填写 API URL 和 API Key", "請先填寫 API URL 和 API Key"));
                                                        return;
                                                    }
                                                    const protocol = activeTool === 'claude' ? 'anthropic' : 'openai';
                                                    setFetchingModelList(true);
                                                    setFetchedModelList([]);
                                                    try {
                                                        const models = await FetchProviderModels(url, key, protocol);
                                                        if (models && models.length > 0) {
                                                            setFetchedModelList(models.map((m: any) => ({ id: m.id || '', name: m.name || '' })));
                                                        } else {
                                                            setStatus(localizeText("Provider returned empty model list", "服务商返回的模型列表为空", "服務商返回的模型列表為空"));
                                                        }
                                                    } catch (e) {
                                                        setStatus(localizeText("Fetch models failed: ", "获取模型列表失败：", "取得模型列表失敗：") + e);
                                                    } finally {
                                                        setFetchingModelList(false);
                                                    }
                                                }}
                                                title={localizeText("Fetch available models from provider", "从服务商获取可用模型", "從服務商取得可用模型")}
                                            >
                                                {fetchingModelList
                                                    ? localizeText("Loading...", "加载中...", "載入中...")
                                                    : localizeText("Models", "模型", "模型")}
                                            </button>
                                        </div>
                                    </div>
                                </div>
                            )}

                            {activeTool === "codex" && (
                                <div className="form-group provider-config-form-group--wire">
                                    <label className="form-label">{localizeText("Wire API", "Wire API", "Wire API")}</label>
                    <input
                        type="text"
                        className="form-input"
                        data-field="wire-api"
                        value={getWireApiValue()}
                        onChange={(e) => handleWireApiChange(e.target.value)}
                        placeholder="responses"
                        spellCheck={false}
                                        autoComplete="off"
                                    />
                                </div>
                            )}</div>

                        {(config as any)[activeTool].models[activeTab].model_name !== "Original" && (
                            <>

                                <div className="form-group">
                                    <div className="provider-config-field-head">
                                        <label className="form-label provider-config-field-label">{t("apiKey")}</label>
                                        {!(config as any)[activeTool].models[activeTab].is_custom && (
                                                <button
                                                    className="btn-link provider-config-link-small"
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
                                        <div className="provider-config-field-head">
                                            <label className="form-label provider-config-field-label">{t("apiEndpoint")}</label>
                                            {(config as any)[activeTool].models[activeTab].is_custom && (
                                                <button
                                                    className="btn-link provider-config-link-small"
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
                                            data-readonly={!(config as any)[activeTool].models[activeTab].is_custom ? 'true' : 'false'}
                                        />
                                    </div>
                            </>
                        )}

                        <div className="provider-config-actions">
                            <button className="btn-primary provider-config-action-primary" onClick={save}>{t("saveChanges")}</button>
                            {(config as any)[activeTool].models[activeTab].is_custom && (
                                <button
                                    className="btn-hide provider-config-action-danger"
                                    onClick={() => {
                                        const allModels = (config as any)[activeTool].models;
                                        const customModels = allModels.filter((m: any) => m.is_custom);
                                        if (customModels.length <= 1) {
                                            showToastMessage(t("cannotRemoveLastCustom"));
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
                                        className="btn-hide provider-config-action-info"
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
                                                wire_api: activeTool === "codex" ? "responses" : "",
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
                            <button className="btn-hide provider-config-action-secondary" onClick={() => setShowModelSettings(false)}>{t("close")}</button>
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
                    t={t}
                    localizeText={localizeText}
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

            {sensitivePermissionRequest && (
                <div className="modal-overlay" data-testid="sensitive-permission-dialog">
                    <div className="modal-content sensitive-permission-modal">
                        <h3>{localizeText('Sensitive Information Request', '\u654f\u611f\u4fe1\u606f\u67e5\u8be2\u786e\u8ba4', '\u654f\u611f\u8cc7\u8a0a\u67e5\u8a62\u78ba\u8a8d')}</h3>
                        <p className="sensitive-permission-modal__copy">
                            {localizeText('A digital employee is requesting permission to answer a password or sensitive information query.', '\u6570\u5b57\u5458\u5de5\u6b63\u5728\u8bf7\u6c42\u8bb8\u53ef\uff0c\u4ee5\u56de\u590d\u5bc6\u7801\u6216\u654f\u611f\u4fe1\u606f\u67e5\u8be2\u3002', '\u6578\u5b57\u54e1\u5de5\u6b63\u5728\u8acb\u6c42\u8a31\u53ef\uff0c\u4ee5\u56de\u8986\u5bc6\u78bc\u6216\u654f\u611f\u8cc7\u8a0a\u67e5\u8a62\u3002')}
                        </p>
                        <div className="sensitive-permission-modal__query">
                            {sensitivePermissionRequest.query}
                        </div>
                        <p className="sensitive-permission-modal__note">
                            {localizeText('No response within 1 minute will be treated as denied.', '1 \u5206\u949f\u5185\u672a\u54cd\u5e94\u5c06\u9ed8\u8ba4\u62d2\u7edd\u3002', '1 \u5206\u9418\u5167\u672a\u56de\u61c9\u5c07\u9810\u8a2d\u62d2\u7d55\u3002')}
                            {sensitivePermissionQueue.length > 0 ? ' ' + localizeText('{count} pending request(s).', '\u8fd8\u6709 {count} \u4e2a\u5f85\u786e\u8ba4\u8bf7\u6c42\u3002', '\u9084\u6709 {count} \u500b\u5f85\u78ba\u8a8d\u8acb\u6c42\u3002').replace('{count}', String(sensitivePermissionQueue.length)) : ''}
                        </p>
                        <div className="modal-actions sensitive-permission-modal__actions">
                            <button className="btn-secondary" onClick={() => respondSensitivePermission('deny')}>{localizeText('Deny', '\u62d2\u7edd', '\u62d2\u7d55')}</button>
                            <button className="btn-primary" onClick={() => respondSensitivePermission('allow')}>{localizeText('Allow', '\u5141\u8bb8', '\u5141\u8a31')}</button>
                        </div>
                    </div>
                </div>
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

            {/* Favorite Employee Replace Picker */}
            {showFavReplacePicker && (
                <FavoriteEmployeeReplacePicker
                    currentSlots={favoriteEmployeeSlots.map(s => ({ veId: s.veId, name: s.name }))}
                    newVeName={showFavReplacePicker.ve.name}
                    onReplace={handleReplaceFavorite}
                    onCancel={() => setShowFavReplacePicker(null)}
                    lang={lang}
                />
            )}

            {/* Proxy Settings Dialog (project-level only) */}
            {showProxySettings && config && (
                <ProjectProxySettingsDialog
                    config={config}
                    selectedProjectForLaunch={selectedProjectForLaunch}
                    setConfig={setConfig}
                    t={t}
                    saveLabel={localizeText("Save", "保存", "儲存")}
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




