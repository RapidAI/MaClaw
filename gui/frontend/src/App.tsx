import { BugReportScreenshotPreviewDataURL, CancelDownload, CheckEnvironment, CheckToolsStatus, ClampMaximizedWindowToWorkArea, CreateExpertTask, CreateRemoteCodingTask, CreateRemoteOpsDiagnosisTask, CreateTask, CreateTaskWithMode, DeleteSkill, DeleteTask, DownloadUpdate, DownloadUpdateWithSHA256, EnsureCodingWorkbenchArmed, FetchProviderModels, GetAdaptiveWindowSize, GetAllLLMTokenUsage, GetBrandInfo, GetChatFontSize, GetDigitalEmployeeFeatureStatus, GetEnvCheckInterval, GetFramelessTopInset, GetHubLLMServiceStatus, GetLansengerLocalMode, GetLansengerStatus, GetMaclawLLMProviders, GetMoASessionState, GetQQBotLocalMode, GetQQBotStatus, GetSystemInfo, GetTelegramLocalMode, GetTelegramStatus, GetThirdPartyGatewayLocalMode, GetThirdPartyGatewayStatus, GetUIZoomFactor, GetUserHomeDir, GetWeixinLocalMode, GetWeixinStatus, GroupDiscussionAcceptInvite, GroupDiscussionProcessPendingInvites, GroupDiscussionPublishProfile, GroupDiscussionRejectInvite, GroupDiscussionStatus, HasPendingBugReportUpload, HideTask, InstallToolOnDemand, IsGossipAllowed, IsNativeRoundedCorners, IsToolBeingInstalled, IsWindowsTerminalAvailable, LaunchInstallerAndExit, LaunchTool, ListBackgroundLoops, ListMyBugReports, ListPythonEnvironments, ListRemoteHubs, ListSkills, ListSkillsWithInstallStatus, ListTasks, LoadConfigForUI, OpenSystemUrl, PackLog, PatchConfigFields, PinTask, PingMaclawLLM, PrepareLocalCodingEnvironment, PrepareRemoteCodingEnvironment, PrepareRemoteOpsDiagnosisEnvironment, ReadBBS, ReadThanks, ReadTutorial, RenameTask, ResizeWindow, RespondDigitalEmployeeSensitiveRequest, ResumeTask, RetryBugReportUpload, SaveConfig, SelectBugReportScreenshots, SelectProjectDir, SetBugReportEnabled, SetDefaultLaunchMode, SetLanguage, SetMaclawLLMCurrentModel, SetMoASticky, SetMoAStickyPreset, ShouldCheckEnvironment, ShowItemInFolder, SubmitBugReport, UpdateLastEnvCheckTime } from '../wailsjs/go/main/App';
import { BrowserOpenURL, EventsOff, EventsOn, Quit, WindowHide, WindowIsFullscreen, WindowIsMaximised, WindowToggleMaximise, WindowUnmaximise } from '../wailsjs/runtime';
import { appVersion, buildNumber } from './version';
import appIcon from './assets/images/maclaw-agent-mark.svg';
import qianxinIcon from './assets/images/qianxin.png';
import lobsterOffline from './assets/images/lobster_offline.svg';
import lobsterHalf from './assets/images/lobster_half.svg';
import { Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react';
import './App.css';
import { a2a, corelib } from '../wailsjs/go/models';
import { EVENT_APP_UPDATE_AVAILABLE, EVENT_PROJECT_INDEX_CHANGED, EVENT_TASKS_CHANGED } from './constants/events';
import { useRemotePanel } from './components/remote/useRemotePanel';
import { TERMINAL_SESSION_STATUSES } from './components/remote/types';
import { useAudioDevices } from './components/ai/useAudioDevices';
import { IMAuditPanel } from './components/remote/IMAuditPanel';
import { OnboardingWizard } from './components/remote/OnboardingWizard';
import type { AssistantUpdatePayload } from './components/ai/AssistantUpdateNotice';
import type { VirtualEmployeeEntry } from './components/ai/VirtualEmployeeTab';

function clipboardImageExtension(mimeType: string): string {
    switch (mimeType.toLowerCase()) {
        case 'image/png': return 'png';
        case 'image/jpeg': return 'jpg';
        case 'image/webp': return 'webp';
        case 'image/bmp': return 'bmp';
        case 'image/gif': return 'gif';
        default: return 'png';
    }
}

/** Toggle maximise, then clamp to work area (Win10 frameless taskbar overflow). */
async function toggleMaximiseAndClampWorkArea(): Promise<void> {
    try {
        // Snapshot before toggle: after restore, skip clamp entirely.
        const wasMax = await WindowIsMaximised().catch(() => false);
        WindowToggleMaximise();
        if (wasMax) {
            // Restored to normal — no work-area clamp needed.
            return;
        }
        // Entering maximised: wait for OS geometry then clamp under taskbar.
        await new Promise((r) => setTimeout(r, 80));
        await ClampMaximizedWindowToWorkArea();
    } catch {
        // Window state APIs may be unavailable during shutdown.
    }
}

function readClipboardImageBase64(file: Blob): Promise<string> {
    return new Promise((resolve, reject) => {
        const reader = new FileReader();
        reader.onload = () => resolve(String(reader.result || '').split(',')[1] || '');
        reader.onerror = () => reject(reader.error || new Error('Unable to read pasted image'));
        reader.readAsDataURL(file);
    });
}

const BUG_REPORT_SCREENSHOT_MAX_BYTES = 10 * 1024 * 1024;

function appendProblemReportScreenshotPaths(previous: string[], paths: unknown[]): string[] {
    return [...previous, ...paths]
        .filter((value): value is string => typeof value === 'string' && value.trim() !== '')
        .filter((value, index, all) => all.indexOf(value) === index)
        .slice(0, 12);
}
import { isVirtualEmployeeOnline } from './components/ai/virtualEmployeeStatus';
import { participantIdentityKeys, participantIdentityMatches } from './components/ai/participantIdentity';
import { isDigitalEmployeeAuthorizationUsable, shouldShowDigitalEmployeeFeatureTabs } from './components/ai/digitalEmployeeFeature';
import type { HistoryDiscussionSummary } from './components/layout/SidebarHistorySessions';
import { activeCodingAgentProgress, latestCodingAgentTurnSnapshot } from './components/ai/CodingAgentProgressStatus';
import { readStoredAssistantThemeMode } from './components/ai/assistantThemeStorage';
import { agentModeFromTaskTags, remoteHostFromTaskTags } from './components/ai/codingTaskMode';
import { normalizeCodingTaskLaunch, type CodingTaskLaunch } from './components/ai/codingTaskLaunch';
import { saveRemoteSSHPassword } from './components/ai/welcomeTaskMemory';
import { getAssistantDarkScheme, readStoredAssistantDarkSchemeId, writeStoredAssistantDarkSchemeId, type AssistantDarkSchemeId } from './components/ai/assistantDarkSchemes';
import { getAssistantLightScheme, readStoredAssistantLightSchemeId, writeStoredAssistantLightSchemeId, type AssistantLightSchemeId } from './components/ai/assistantLightSchemes';
import { useAIAssistant, emitDesktopPetState } from './components/ai/useAIAssistant';
import { START_VOICE_INPUT_EVENT } from './components/ai/useVoiceInput';
import { useDialog } from './components/CustomDialog';
import { buildHubCardStoreURL, buildHubCreditsURL, buildHubMaclawAppManualURL } from './utils/hubCredits';
import { inferProviderModelFetchProtocol } from './utils/providerModelFetchProtocol';
import { normalizeSidebarHubCredits } from './utils/sidebarHubCredits';
import { getSidebarUsageForProvider, selectSidebarCurrentProvider } from './utils/sidebarProviderSelection';
import { buildSidebarModelOptions } from './utils/sidebarModelOptions';
import { applySavedUIZoomFactor, recommendUIScale, subscribeDisplayScaleChanges, uiScaleEquals } from './utils/uiScale';
import { getWailsAppModule } from './utils/wailsAppModule';
import { translations } from './i18n/appTranslations';
import { miniAppLabels, pickMiniAppLabel } from './i18n/maclawMiniAppLabels';
import { ToolConfiguration } from './components/tools/ToolConfiguration';
import { PROJECT_PAGE_SIZE, knownProviderEndpoints, recommendedModels, subscriptionUrls, getModelDisplayName, type ProviderEndpoint } from './config/providerCatalog';
import { TOOL_NAMES, getToolLabel, isToolTab, normalizeToolTab } from './config/toolCatalog';
import { getSettingsTabOptions, resolveSettingsTabId, type SettingsTabId } from './config/settingsTabs';
import { SettingsPage } from './components/settings/SettingsPage';
import { AppSidebarShell } from './components/layout/AppSidebarShell';
import { isProjectTabOpen } from './components/layout/SidebarTaskManagement';
import { purgeDeletedProjectTabLocalCache } from './components/ai/aiAssistantPanelSessionUtils';
import { FavoriteEmployeeReplacePicker } from './components/layout/FavoriteEmployeeReplacePicker';
import { countActiveBackgroundLoops } from './components/layout/backgroundTaskCount';
import { MAX_USER_FAVORITES, normalizeFavoriteEmployeeIds } from './components/settings/favoriteEmployees';
import { MainTopHeader } from './components/layout/MainTopHeader';
import { AppStatusMessageBar } from './components/layout/AppStatusMessageBar';
import { ThanksModal } from './components/modals/ThanksModal';
import { AboutPanel } from './components/AboutPanel';
import { ToolRepairProgressDialog } from './components/modals/ToolRepairProgressDialog';
import { UpdateModal } from './components/modals/UpdateModal';
import { checkAppUpdate } from './utils/checkAppUpdate';
import { InstallLogModal } from './components/modals/InstallLogModal';
import { ProjectProxySettingsDialog } from './components/modals/ProjectProxySettingsDialog';
import { InstallSkillModal } from './components/modals/InstallSkillModal';
import { RemoteActivationDialog } from './components/modals/RemoteActivationDialog';
import { ProviderSelectorDialog } from './components/modals/ProviderSelectorDialog';
import { ConfirmDialog } from './components/modals/ConfirmDialog';
import { DataMigrationOverlay } from './components/DataMigrationOverlay';
import type { RemoteCenterHubOption, SidebarCurrentProviderTokenUsage, SidebarHubCredits, SidebarLLMProviderSummary, SidebarTokenUsageStat } from './types/appShell';
import { AIAssistantPanel, TutorialPage, ApiStorePage, ProjectManagerPage, RemoteSessionsPage, AppsPage, SkillsPage, MCPPage, GossipPage, WorkflowsPage, UtilitiesPage } from './appLazyComponents';
import { meetingRecordCommand, meetingRecordFailMessage, meetingRecordTaskTitle } from './components/pages/utilitiesMeetingRecord';
import { parseExpertListJSON, type ExpertDefinition } from './components/ai/expertTypes';

const APP_VERSION = appVersion
const MACLAW_CODE_REPOSITORY_URL = "https://github.com/rapidai/maclaw";
const DISMISSED_APP_UPDATE_VERSION_KEY = "maclaw:dismissed-app-update-version";
const unavailableDigitalEmployeeFeatureStatus = { visible: false, reason: 'unavailable' };

function callBackend<T>(call: () => T | Promise<T>): Promise<T> {
    return Promise.resolve().then(call);
}

const EXPERT_TASK_SOURCE_PREFIX = 'source:expert:';

/** Return the persistent expert identity encoded on a task-management row. */
export function expertIDFromTaskTags(tags?: string[] | null): string {
    for (const rawTag of tags || []) {
        const tag = String(rawTag || '').trim();
        if (!tag.startsWith(EXPERT_TASK_SOURCE_PREFIX)) continue;
        const expertID = tag.slice(EXPERT_TASK_SOURCE_PREFIX.length).trim();
        if (expertID) return expertID;
    }
    return '';
}

function safeEventsOn(eventName: string, callback: (...args: any[]) => void) {
    try {
        return EventsOn(eventName, callback);
    } catch {
        return undefined;
    }
}

function safeEventsOff(eventName: string, ...additionalEventNames: string[]) {
    try {
        EventsOff(eventName, ...additionalEventNames);
    } catch {
        // Runtime events are unavailable in a plain browser dev session.
    }
}

function safeBrowserOpenURL(url: string) {
    try {
        BrowserOpenURL(url);
    } catch {
        window.open(url, "_blank", "noopener,noreferrer");
    }
}
function fetchDigitalEmployeeFeatureStatus() {
    return callBackend(() => GetDigitalEmployeeFeatureStatus())
        .then((status: any) => status || { visible: false })
        .catch(() => unavailableDigitalEmployeeFeatureStatus);
}

type SensitivePermissionRequest = {
    request_id: string;
    session_id?: string;
    query: string;
    timeout_seconds?: number;
};

type SidebarProviderStateWire = {
    providers?: Array<{
        name?: string; Name?: string;
        url?: string; URL?: string;
        key?: string; Key?: string;
        model?: string; Model?: string;
        models?: string[]; Models?: string[];
        protocol?: string; Protocol?: string;
        agent_type?: string; AgentType?: string;
        auth_type?: string; AuthType?: string;
        is_hub_service?: boolean; IsHubService?: boolean;
    }>;
    Providers?: Array<{
        name?: string; Name?: string;
        url?: string; URL?: string;
        key?: string; Key?: string;
        model?: string; Model?: string;
        models?: string[]; Models?: string[];
        protocol?: string; Protocol?: string;
        agent_type?: string; AgentType?: string;
        auth_type?: string; AuthType?: string;
        is_hub_service?: boolean; IsHubService?: boolean;
    }>;
    current?: string;
    Current?: string;
} | null;

const VE_EVENT_PROFILE_OVERRIDE_TTL_MS = 60_000;
const VE_SIDEBAR_LIST_POLL_BASE_MS = 45_000;
const VE_SIDEBAR_LIST_POLL_MAX_MS = 180_000;
const VE_SIDEBAR_LIST_POLL_JITTER_MS = 10_000;
const VE_SIDEBAR_LIST_EVENT_THROTTLE_MS = 1_500;

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

function normalizeFavoriteEmployeeNames(value: unknown): Record<string, string> {
    if (!value || typeof value !== 'object' || Array.isArray(value)) return {};
    return Object.entries(value as Record<string, unknown>).reduce<Record<string, string>>((acc, [key, rawName]) => {
        const id = String(key || '').trim();
        const name = String(rawName || '').trim();
        if (id && name) acc[id] = name;
        return acc;
    }, {});
}

function favoriteEmployeeAliasIds(veId: string, veList: VirtualEmployeeEntry[]): string[] {
    const aliases = new Set<string>();
    const add = (value?: string) => {
        const id = String(value || '').trim();
        if (!id) return;
        aliases.add(id);
        participantIdentityKeys(id).forEach(key => aliases.add(key));
    };
    add(veId);
    const ve = veList.find(v => participantIdentityMatches(v.id, veId) || participantIdentityMatches(v.machine_id, veId));
    add(ve?.id);
    add(ve?.machine_id);
    return Array.from(aliases);
}

function favoriteEmployeeIDInAliasSet(id: string, aliases: Set<string>): boolean {
    const normalized = String(id || '').trim();
    if (!normalized) return false;
    if (aliases.has(normalized)) return true;
    return participantIdentityKeys(normalized).some(key => aliases.has(key));
}

function filterOnlineVirtualEmployees(list: VirtualEmployeeEntry[]): VirtualEmployeeEntry[] {
    return Array.isArray(list) ? list.filter(isVirtualEmployeeOnline) : [];
}

type VirtualEmployeeEventPatch = Partial<VirtualEmployeeEntry> & { id?: string; machine_id?: string };

function readStringField(source: Record<string, any>, ...keys: string[]): string | undefined {
    for (const key of keys) {
        if (Object.prototype.hasOwnProperty.call(source, key)) return String(source[key] || '').trim();
    }
    return undefined;
}

function normalizeVEEventOnlineStatus(value: string | undefined): "online" | "offline" | undefined {
    const normalized = String(value || '').trim().toLowerCase();
    if (normalized === 'offline') return 'offline';
    if (normalized === 'online') return 'online';
    return undefined;
}

function readArrayField(source: Record<string, any>, ...keys: string[]): string[] | undefined {
    for (const key of keys) {
        if (Object.prototype.hasOwnProperty.call(source, key)) return Array.isArray(source[key]) ? source[key] : [];
    }
    return undefined;
}

function virtualEmployeeFromEventPayload(eventData: any): VirtualEmployeeEventPatch | null {
    const payload = eventData?.payload && typeof eventData.payload === 'object' ? eventData.payload : eventData;
    const employee = payload?.employee || payload?.Employee || payload?.virtual_employee || payload?.ve;
    if (!employee || typeof employee !== 'object') {
        const root = eventData && typeof eventData === 'object' ? eventData : {};
        const source = payload && typeof payload === 'object' ? payload : {};
        const id = readStringField(source, 've_id', 'veId', 'id', 'ID') || readStringField(root, 've_id', 'veId', 'id', 'ID') || '';
        const machineId = readStringField(source, 'machine_id', 'machineId', 'MachineID') || readStringField(root, 'machine_id', 'machineId', 'MachineID') || '';
        if (!id && !machineId) return null;
        const patch: VirtualEmployeeEventPatch = { id, machine_id: machineId };
        const onlineStatus = normalizeVEEventOnlineStatus(
            readStringField(source, 'online_status', 'onlineStatus', 'OnlineStatus', 'status', 'Status') ||
            readStringField(root, 'online_status', 'onlineStatus', 'OnlineStatus', 'status', 'Status')
        );
        if (onlineStatus) patch.online_status = onlineStatus;
        return patch;
    }
    const id = readStringField(employee, 'id', 'ID') || '';
    const machineId = readStringField(employee, 'machine_id', 'MachineID') || '';
    if (!id && !machineId) return null;
    const patch: VirtualEmployeeEventPatch = { id, machine_id: machineId };
    const name = readStringField(employee, 'name', 'Name');
    if (name !== undefined) patch.name = name;
    const skillDescription = readStringField(employee, 'skill_description', 'SkillDescription');
    if (skillDescription !== undefined) patch.skill_description = skillDescription;
    const avatarDataURL = readStringField(employee, 'avatar_data_url', 'AvatarDataURL');
    if (avatarDataURL !== undefined) patch.avatar_data_url = avatarDataURL;
    const rawPolicy = readStringField(employee, 'access_policy', 'AccessPolicy');
    if (rawPolicy !== undefined) patch.access_policy = rawPolicy === 'whitelist' || rawPolicy === 'blacklist' || rawPolicy === 'per_request' ? rawPolicy : 'public';
    const status = readStringField(employee, 'status', 'Status');
    if (status !== undefined) patch.status = status;
    const rawOnlineStatus = readStringField(employee, 'online_status', 'OnlineStatus')?.toLowerCase();
    if (rawOnlineStatus !== undefined) patch.online_status = rawOnlineStatus === 'offline' ? 'offline' : 'online';
    if (Object.prototype.hasOwnProperty.call(employee, 'resident') || Object.prototype.hasOwnProperty.call(employee, 'Resident')) {
        patch.resident = Boolean(employee.resident || employee.Resident);
    }
    const registeredAt = readStringField(employee, 'registered_at', 'RegisteredAt');
    if (registeredAt !== undefined) patch.registered_at = registeredAt;
    const whitelist = readArrayField(employee, 'whitelist', 'Whitelist');
    if (whitelist !== undefined) patch.whitelist = whitelist;
    return patch;
}

function completeVirtualEmployeeEntry(next: VirtualEmployeeEventPatch): VirtualEmployeeEntry {
    const id = String(next.id || next.machine_id || '').trim();
    return {
        id,
        machine_id: next.machine_id,
        name: next.name || id.slice(0, 8),
        skill_description: next.skill_description || '',
        avatar_data_url: next.avatar_data_url,
        access_policy: next.access_policy || 'public',
        status: next.status || 'active',
        online_status: next.online_status || 'online',
        resident: next.resident,
        registered_at: next.registered_at,
        whitelist: next.whitelist,
    };
}

function mergeVirtualEmployeeEntry(current: VirtualEmployeeEntry, next: VirtualEmployeeEventPatch): VirtualEmployeeEntry {
    const merged = { ...current };
    (['id', 'machine_id', 'name', 'skill_description', 'avatar_data_url', 'access_policy', 'status', 'online_status', 'resident', 'registered_at', 'whitelist'] as const).forEach((key) => {
        if (next[key] !== undefined) (merged as any)[key] = next[key];
    });
    return merged;
}

function mergeVirtualEmployeeList(prev: VirtualEmployeeEntry[], next: VirtualEmployeeEventPatch): VirtualEmployeeEntry[] {
    if (!next) return prev;
    const nextId = String(next.id || '').trim();
    const nextMachineId = String(next.machine_id || '').trim();
    const index = prev.findIndex(item => participantIdentityMatches(item.id, nextId) || participantIdentityMatches(item.machine_id, nextId) || participantIdentityMatches(item.id, nextMachineId) || participantIdentityMatches(item.machine_id, nextMachineId));
    if (index < 0) return next.online_status === 'offline' ? prev : [...prev, completeVirtualEmployeeEntry(next)];
    const merged = [...prev];
    merged[index] = mergeVirtualEmployeeEntry(merged[index], next);
    return merged;
}

function virtualEmployeeOverrideKey(employee: VirtualEmployeeEventPatch): string {
    return String(employee.machine_id || employee.id || '').trim();
}

function applyVirtualEmployeeOverrides(list: VirtualEmployeeEntry[], overrides: Map<string, { employee: VirtualEmployeeEventPatch; expiresAt: number }>, now = Date.now()): VirtualEmployeeEntry[] {
    let next = list;
    overrides.forEach((entry, key) => {
        if (entry.expiresAt <= now) {
            overrides.delete(key);
            return;
        }
        next = mergeVirtualEmployeeList(next, entry.employee);
    });
    return next;
}

function residentVirtualEmployeeAliases(ve: VirtualEmployeeEntry): string[] {
    return favoriteEmployeeAliasIds(String(ve.machine_id || ve.id || '').trim() || ve.id, [ve]);
}

function App() {
    console.log('[startup-trace] App component render begin');
    const logStartupTrace = (stage: string, extra?: Record<string, unknown>) => {
        const logFn = (window as any).go?.main?.App?.LogFrontendDiagnostic;
        const payload = { tag: 'startup-trace', stage, ts: Date.now(), ...extra };
        console.log('[startup-trace]', stage, extra || '');
        if (typeof logFn === 'function') {
            try { void Promise.resolve(logFn(payload)).catch(() => {}); } catch {}
        }
    };
    logStartupTrace('app-render-begin');
    const { showAlert, showConfirm } = useDialog();
    const [config, setConfig] = useState<corelib.AppConfig | null>(null);
    const [navTab, setNavTab] = useState<string>("ai");
    // Keep Utilities alive after its first visit so long-running virtual-repository
    // work continues when the user briefly switches to the AI assistant.
    const [utilitiesPageVisited, setUtilitiesPageVisited] = useState(false);
    const [remoteInitialSessionTab, setRemoteInitialSessionTab] = useState<"remote" | "background">("remote");
    const audioDevices = useAudioDevices();
    const [aiPanelMaximized, setAiPanelMaximized] = useState(false);
    const aiPanelMaximizedWindowRef = useRef(false);
    const aiPanelMaximizeSeqRef = useRef(0);
    const [windowMaximized, setWindowMaximized] = useState(false);
    const restoreAIPanelOwnedWindowMaximize = useCallback(async () => {
        aiPanelMaximizedWindowRef.current = false;
        try {
            WindowUnmaximise();
            setWindowMaximized(false);
            return true;
        } catch {
            // Window state can be unavailable while closing; CSS restore still applies.
        }
        return false;
    }, []);
    useEffect(() => {
        let debounceTimer: ReturnType<typeof setTimeout> | null = null;
        let clampTimer: ReturnType<typeof setTimeout> | null = null;
        const requestClamp = () => {
            // Coalesce rapid resize clamps (monitor move / DPI); backend also
            // no-ops when geometry already matches work area.
            if (clampTimer) clearTimeout(clampTimer);
            clampTimer = setTimeout(() => {
                void callBackend(() => ClampMaximizedWindowToWorkArea()).catch(() => undefined);
            }, 80);
        };
        const applyWindowState = (isMax: boolean, isFs: boolean) => {
            const maximised = !!(isMax || isFs);
            setWindowMaximized(maximised);
            // Clamp when maximised (mount / toggle / resize / DPI).
            if (maximised) {
                requestClamp();
            }
        };
        const syncMaximized = (immediate = false) => {
            const run = () => {
                // Window state is a 3-state enum: normal | maximised | fullscreen.
                // The title bar restore button should show "restore" icon when the
                // window is in ANY non-normal state (maximised OR fullscreen).
                Promise.all([callBackend(() => WindowIsMaximised()), callBackend(() => WindowIsFullscreen())]).then(
                    ([isMax, isFs]) => applyWindowState(!!isMax, !!isFs)
                ).catch(() => undefined);
            };
            if (immediate) {
                run();
                return;
            }
            if (debounceTimer) clearTimeout(debounceTimer);
            debounceTimer = setTimeout(run, 150);
        };
        const onResize = () => syncMaximized(false);
        // Immediate sync on mount so data-maximized is correct before first paint settles
        // (startup may already be maximised on low-res displays).
        syncMaximized(true);
        window.addEventListener('resize', onResize);
        return () => {
            window.removeEventListener('resize', onResize);
            if (debounceTimer) clearTimeout(debounceTimer);
            if (clampTimer) clearTimeout(clampTimer);
        };
    }, []);
    const navTabRef = useRef(navTab);
    const startupNavAppliedRef = useRef(false);
    const setNavTabNow = useCallback((tab: string) => {
        if (tab === 'utilities') setUtilitiesPageVisited(true);
        navTabRef.current = tab;
        setNavTab(tab);
    }, []);
    const showAppEntryEnabled = config?.show_app_entry !== false;
    const showWorkflowEntryEnabled = config?.show_workflow_entry !== false;
	const showUtilitiesEntryEnabled = (config as any)?.show_utilities_entry !== false;
    useEffect(() => {
        const openAppsPanel = () => {
            if (showAppEntryEnabled) setNavTabNow('apps');
        };
        window.addEventListener('maclaw:open-apps-panel', openAppsPanel);
        return () => window.removeEventListener('maclaw:open-apps-panel', openAppsPanel);
    }, [setNavTabNow, showAppEntryEnabled]);
    useEffect(() => {
        const runSkill = (e: Event) => {
            const detail = (e as CustomEvent).detail;
            const name = detail?.name;
            if (!name) return;
            // Switch to AI tab and send a run command through the chat.
            // Use a retry loop to handle the case where AIAssistantPanel hasn't
            // mounted its inject-chat-message listener yet after tab switch.
            setNavTabNow('ai');
            const text = `run skill "${name}"`;
            let attempts = 0;
            const tryInject = () => {
                attempts++;
                const event = new CustomEvent("maclaw:inject-chat-message", {
                    detail: { text },
                    cancelable: true, // Required for preventDefault to work
                });
                window.dispatchEvent(event);
                // If nobody called preventDefault, the listener may not be ready yet.
                // Retry up to 5 times with increasing delay.
                if (!event.defaultPrevented && attempts < 5) {
                    setTimeout(tryInject, attempts * 150);
                }
            };
            setTimeout(tryInject, 50);
        };
        window.addEventListener('maclaw:run-skill', runSkill);
        return () => window.removeEventListener('maclaw:run-skill', runSkill);
    }, [setNavTabNow]);
    useEffect(() => {
        if (!showAppEntryEnabled && navTab === 'apps') setNavTabNow('ai');
    }, [navTab, setNavTabNow, showAppEntryEnabled]);
    useEffect(() => {
        if (!showWorkflowEntryEnabled && navTab === 'workflows') setNavTabNow('ai');
    }, [navTab, setNavTabNow, showWorkflowEntryEnabled]);
	useEffect(() => {
		if (!showUtilitiesEntryEnabled && navTab === 'utilities') setNavTabNow('ai');
	}, [navTab, setNavTabNow, showUtilitiesEntryEnabled]);
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
    const [taskManagementPaneWidth, setTaskManagementPaneWidth] = useState(260);
    const [isTaskManagementResizing, setIsTaskManagementResizing] = useState(false);
    const taskManagementResizeStartX = useRef(0);
    const taskManagementResizeStartWidth = useRef(380);
    const [toolDropdownOpen, setToolDropdownOpen] = useState(false);
    const [taskContextMenu, setTaskContextMenu] = useState<{ x: number; y: number; projectPath: string; name: string; pinned: boolean; isRemoteCoding?: boolean; tags?: string[] } | null>(null);
    /** Project paths with an open AI assistant tab — used to block task-list remove. */
    const [openProjectTabPaths, setOpenProjectTabPaths] = useState<string[]>([]);
    /** Expert IDs with an open AI assistant tab — protects their durable task rows. */
    const [openExpertTabIDs, setOpenExpertTabIDs] = useState<string[]>([]);
    const handleOpenProjectTabsChange = useCallback((paths: string[]) => {
        setOpenProjectTabPaths(prev => {
            // Order-independent equality: tab reorder should not thrash sidebar props.
            if (prev.length === paths.length) {
                const prevSet = new Set(prev);
                if (paths.every((p) => prevSet.has(p))) {
                    return prev;
                }
            }
            return paths;
        });
    }, []);
    const handleOpenExpertTabsChange = useCallback((expertIDs: string[]) => {
        setOpenExpertTabIDs(prev => {
            if (prev.length === expertIDs.length) {
                const prevSet = new Set(prev);
                if (expertIDs.every(id => prevSet.has(id))) return prev;
            }
            return expertIDs;
        });
    }, []);
    const hideTaskGuarded = useCallback(async (projectPath: string, tags?: string[]) => {
        // Keep the guard: an open task must be closed before its state is deleted.
        if (isProjectTabOpen(projectPath, openProjectTabPaths)) {
            return;
        }
        const expertID = expertIDFromTaskTags(tags);
        if (expertID && openExpertTabIDs.includes(expertID)) {
            return;
        }
        await DeleteTask(projectPath);
        purgeDeletedProjectTabLocalCache(projectPath);
    }, [openExpertTabIDs, openProjectTabPaths]);
    const [renamingTaskPath, setRenamingTaskPath] = useState<string | null>(null);
    const [renameValue, setRenameValue] = useState("");
    const [taskItems, setTaskItems] = useState<Array<{ id?: string; name?: string; project_path: string; workflow_type?: string; active_workflow?: { id?: string; type?: string; phase?: string; status?: string; project_path?: string; pending_review?: boolean }; preview?: string; tags?: string[]; last_activity?: string; pinned?: boolean; has_output?: boolean }>>([]);
    const taskItemsRef = useRef(taskItems);
    taskItemsRef.current = taskItems;
    // A ListTasks response can complete after a task was just created locally.
    // Keep a monotonically increasing generation so an older response never
    // overwrites that optimistic, durable task-management entry.
    const taskRefreshGenerationRef = useRef(0);
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
    const [isManualCheck, setIsManualCheck] = useState(false);
    /** Allows every loading-shell exit to use the same safe window transition. */
    const loadingShellCtlRef = useRef<{
        finish: (reason: string) => void;
        begin: (reason: string) => number;
        complete: (reason: string) => void;
    } | null>(null);
    // Onboarding is rendered through a portal and must not appear above the
    // compact environment-check shell. It becomes eligible only after the
    // environment-check completion path has resized the native window.
    const [mainWindowReadyForOverlays, setMainWindowReadyForOverlays] = useState(false);
    const mainWindowReadyForOverlaysRef = useRef(false);
    const [showMaclawLLMPopup, setShowMaclawLLMPopup] = useState(false);
    const [hubAuthRejectedPrompt, setHubAuthRejectedPrompt] = useState(false);
    const [pythonEnvironments, setPythonEnvironments] = useState<any[]>([]);
    const [envCheckInterval, setEnvCheckInterval] = useState<number>(7);
    const [uiZoom, setUiZoom] = useState<number>(1);
    const [uiZoomAuto, setUiZoomAuto] = useState<boolean>(true);
    const [chatFontSize, setChatFontSize] = useState<number>(14);
    const [pendingVEOpen, setPendingVEOpen] = useState<VirtualEmployeeEntry | null>(null);
    const [pendingHistoryDiscussionOpen, setPendingHistoryDiscussionOpen] = useState<HistoryDiscussionSummary | null>(null);
    const [pendingProjectTabOpen, setPendingProjectTabOpen] = useState<CodingTaskLaunch | null>(null);
    const [pendingExpertOpen, setPendingExpertOpen] = useState<{ expert: ExpertDefinition } | null>(null);

    // Onboarding is a portal rendered outside #App. Route every request
    // through this guard so it cannot appear while the compact environment
    // check shell is still the visible application surface.
    const onboardingRequestedRef = useRef(false);
    const requestOnboarding = useCallback(() => {
        onboardingRequestedRef.current = true;
        if (mainWindowReadyForOverlaysRef.current) {
            setShowMaclawLLMPopup(true);
        }
    }, []);

    useEffect(() => {
        if (mainWindowReadyForOverlays && onboardingRequestedRef.current) {
            setShowMaclawLLMPopup(true);
        }
    }, [mainWindowReadyForOverlays]);

    // --- Favorite Employees state ---
    const [favoriteEmployeeIds, setFavoriteEmployeeIds] = useState<string[]>([]);
    const [favoriteEmployeeNames, setFavoriteEmployeeNames] = useState<Record<string, string>>({});
    const favoriteEmployeeIdsRef = useRef<string[]>([]);
    const favoriteEmployeeNamesRef = useRef<Record<string, string>>({});
    const [veList, setVeList] = useState<VirtualEmployeeEntry[]>([]);
    const veEventProfileOverridesRef = useRef<Map<string, { employee: VirtualEmployeeEventPatch; expiresAt: number }>>(new Map());
    const [digitalEmployeeFeatureStatus, setDigitalEmployeeFeatureStatus] = useState<any>({ visible: false, actual_count: 0 });
    const digitalEmployeeStatusInFlightRef = useRef<Promise<void> | null>(null);
    const digitalEmployeeStatusRefreshAgainRef = useRef(false);
    const digitalEmployeeStatusGenerationRef = useRef(0);
    const [showFavReplacePicker, setShowFavReplacePicker] = useState<{ ve: VirtualEmployeeEntry } | null>(null);

    // Load favorite employees from config
    useEffect(() => {
        setFavoriteEmployeeIds(normalizeFavoriteEmployeeIds(config?.favorite_employees));
    }, [config?.favorite_employees]);
    useEffect(() => { favoriteEmployeeIdsRef.current = favoriteEmployeeIds; }, [favoriteEmployeeIds]);

    useEffect(() => {
        setFavoriteEmployeeNames(normalizeFavoriteEmployeeNames((config as any)?.favorite_employee_names));
    }, [(config as any)?.favorite_employee_names]);
    useEffect(() => { favoriteEmployeeNamesRef.current = favoriteEmployeeNames; }, [favoriteEmployeeNames]);

    // Fetch VE list for sidebar favorites resolution
    useEffect(() => {
        if (!config?.remote_hub_url || !config?.remote_machine_id) {
            veEventProfileOverridesRef.current.clear();
            setVeList([]);
            return;
        }
        veEventProfileOverridesRef.current.clear();
        let cancelled = false;
        let pollTimer: ReturnType<typeof setTimeout> | undefined;
        let eventThrottleTimer: ReturnType<typeof setTimeout> | undefined;
        let pendingEventRefresh = false;
        let consecutiveFailures = 0;
        let skipNextPoll = false;
        let fetchInFlight: Promise<void> | undefined;
        let fetchAfterInFlight = false;
        let requestSeq = 0;
        const scheduleNextPoll = () => {
            if (cancelled) return;
            if (pollTimer) clearTimeout(pollTimer);
            const failureMultiplier = Math.max(1, 2 ** Math.min(consecutiveFailures, 3));
            const delay = Math.min(VE_SIDEBAR_LIST_POLL_MAX_MS, VE_SIDEBAR_LIST_POLL_BASE_MS * failureMultiplier);
            const jitter = Math.floor(Math.random() * VE_SIDEBAR_LIST_POLL_JITTER_MS);
            pollTimer = setTimeout(() => {
                pollTimer = undefined;
                if (skipNextPoll) {
                    skipNextPoll = false;
                    scheduleNextPoll();
                    return;
                }
                fetchVeList().finally(scheduleNextPoll);
            }, delay + jitter);
        };
        const fetchVeList = () => {
            if (fetchInFlight) {
                fetchAfterInFlight = true;
                return fetchInFlight;
            }
            const seq = requestSeq + 1;
            requestSeq = seq;
            fetchInFlight = (async () => {
                const mod = await getWailsAppModule();
                if (cancelled || seq !== requestSeq) return;
                if ((mod as any).ListVirtualEmployees) {
                    const list: VirtualEmployeeEntry[] = await (mod as any).ListVirtualEmployees();
                    if (!cancelled && seq === requestSeq) {
                        if (!Array.isArray(list)) {
                            throw new Error("ListVirtualEmployees returned a non-array response");
                        }
                        consecutiveFailures = 0;
                        setVeList(applyVirtualEmployeeOverrides(filterOnlineVirtualEmployees(list), veEventProfileOverridesRef.current));
                    }
                }
            })().catch(() => {
                if (!cancelled && seq === requestSeq) consecutiveFailures += 1;
            }).finally(() => {
                fetchInFlight = undefined;
                if (!cancelled && fetchAfterInFlight) {
                    fetchAfterInFlight = false;
                    void fetchVeList();
                }
            });
            return fetchInFlight;
        };
        const requestEventRefresh = () => {
            if (cancelled) return;
            if (eventThrottleTimer) {
                pendingEventRefresh = true;
                return;
            }
            skipNextPoll = true;
            void fetchVeList();
            eventThrottleTimer = setTimeout(() => {
                eventThrottleTimer = undefined;
                if (pendingEventRefresh) {
                    pendingEventRefresh = false;
                    skipNextPoll = true;
                    void fetchVeList();
                }
            }, VE_SIDEBAR_LIST_EVENT_THROTTLE_MS);
        };
        const applyVEEvent = (eventData: any) => {
            if (cancelled) return;
            const employee = virtualEmployeeFromEventPayload(eventData);
            if (employee) {
                const key = virtualEmployeeOverrideKey(employee);
                if (key) veEventProfileOverridesRef.current.set(key, { employee, expiresAt: Date.now() + VE_EVENT_PROFILE_OVERRIDE_TTL_MS });
                setVeList(prev => mergeVirtualEmployeeList(prev, employee));
            }
            requestEventRefresh();
        };
        fetchVeList().finally(scheduleNextPoll);
        // Refresh on VE status changes
        const unsub1 = safeEventsOn("ve:list_update", applyVEEvent);
        const unsub2 = safeEventsOn("ve:status_change", applyVEEvent);
        return () => {
            cancelled = true;
            if (pollTimer) clearTimeout(pollTimer);
            if (eventThrottleTimer) clearTimeout(eventThrottleTimer);
            if (typeof unsub1 === "function") unsub1(); else safeEventsOff("ve:list_update");
            if (typeof unsub2 === "function") unsub2(); else safeEventsOff("ve:status_change");
        };
    }, [config?.remote_hub_url, config?.remote_machine_id]);

    const refreshDigitalEmployeeFeatureStatus = useCallback(() => {
        if (digitalEmployeeStatusInFlightRef.current) {
            digitalEmployeeStatusRefreshAgainRef.current = true;
            return digitalEmployeeStatusInFlightRef.current;
        }
        const generation = digitalEmployeeStatusGenerationRef.current;
        const request = fetchDigitalEmployeeFeatureStatus()
            .then((status) => {
                if (digitalEmployeeStatusGenerationRef.current === generation) {
                    setDigitalEmployeeFeatureStatus(status);
                }
            })
            .finally(() => {
                digitalEmployeeStatusInFlightRef.current = null;
                if (digitalEmployeeStatusRefreshAgainRef.current) {
                    digitalEmployeeStatusRefreshAgainRef.current = false;
                    void refreshDigitalEmployeeFeatureStatus();
                }
            });
        digitalEmployeeStatusInFlightRef.current = request;
        return request;
    }, []);

    useEffect(() => {
        digitalEmployeeStatusGenerationRef.current += 1;
        const refresh = () => {
            refreshDigitalEmployeeFeatureStatus()
                .then(() => undefined)
                .catch(() => undefined);
        };
        refresh();
        const subscriptions = [
            ["digital-employee-authorization-changed", safeEventsOn("digital-employee-authorization-changed", refresh)] as const,
            ["ve:status_change", safeEventsOn("ve:status_change", refresh)] as const,
            ["ve:list_update", safeEventsOn("ve:list_update", refresh)] as const,
            ["ve:approved", safeEventsOn("ve:approved", refresh)] as const,
            ["ve:rejected", safeEventsOn("ve:rejected", refresh)] as const,
            ["ve:disabled", safeEventsOn("ve:disabled", refresh)] as const,
        ];
        return () => {
            subscriptions.forEach(([name, unsubscribe]) => {
                if (typeof unsubscribe === "function") unsubscribe();
                else safeEventsOff(name);
            });
        };
    }, [config?.remote_hub_url, config?.remote_machine_id, refreshDigitalEmployeeFeatureStatus]);

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
    // Sync setState only — startTransition deferred paint and caused intermittent blank settings.
    const selectSettingsTab = useCallback((tab: SettingsTabId) => {
        setSettingsTab(resolveSettingsTabId(tab, { hideVirtualEmployee: !veNavigationAvailable }));
    }, [veNavigationAvailable]);
    const patchConfigFieldsForSettings = useCallback(
        (patch: Record<string, any>) => callBackend(() => PatchConfigFields(patch)),
        [],
    );
    const handleSettingsLLMStatusChange = useCallback((online: boolean, configured: boolean) => {
        setMaclawLLMOnline(online);
        setMaclawLLMConfigured(configured);
    }, []);
    useEffect(() => {
        const openSettings = (e: Event) => {
            const detail = (e as CustomEvent<{ tab?: string }>).detail;
            const tab = resolveSettingsTabId(detail?.tab, { hideVirtualEmployee: !veNavigationAvailable });
            setNavTabNow('settings');
            selectSettingsTab(tab);
        };
        window.addEventListener('maclaw:open-settings', openSettings);
        return () => window.removeEventListener('maclaw:open-settings', openSettings);
    }, [setNavTabNow, selectSettingsTab, veNavigationAvailable]);
    const openMISDataSettings = useCallback(() => {
        setNavTabNow('settings');
        selectSettingsTab('misData');
    }, [setNavTabNow, selectSettingsTab]);
    useEffect(() => {
        if (veAuthorized) return;
        setPendingVEOpen(null);
        setPendingHistoryDiscussionOpen(null);
    }, [veAuthorized]);

    const residentEmployeeAliasSet = useMemo(() => {
        const aliases = new Set<string>();
        veList.filter(ve => ve.resident).forEach(ve => {
            residentVirtualEmployeeAliases(ve).forEach(alias => {
                const normalized = String(alias || '').trim();
                if (normalized) aliases.add(normalized);
            });
        });
        return aliases;
    }, [veList]);

    const userFavoriteEmployeeIds = useMemo(() => {
        return favoriteEmployeeIds.filter(id => !favoriteEmployeeIDInAliasSet(id, residentEmployeeAliasSet));
    }, [favoriteEmployeeIds, residentEmployeeAliasSet]);

    // Resolve favorite IDs to display slots
    const favoriteEmployeeSlots = useMemo(() => {
        const residentVE = veList.find(ve => {
            if (!ve.resident || !isVirtualEmployeeOnline(ve)) return false;
            if (isOwnVirtualEmployeeId(ve.id, config?.remote_machine_id) || isOwnVirtualEmployeeId(ve.machine_id || '', config?.remote_machine_id)) return false;
            return true;
        });
        const residentSlots = residentVE ? (() => {
            const participantId = String(residentVE.machine_id || residentVE.id || '').trim();
            if (!participantId) return [];
            const customName = favoriteEmployeeNames[participantId] || favoriteEmployeeNames[residentVE.id] || (residentVE.machine_id ? favoriteEmployeeNames[residentVE.machine_id] : '');
            return [{ veId: participantId, name: customName || residentVE.name || participantId.slice(0, 6), online: true, skillDescription: residentVE.skill_description || '', avatarDataURL: residentVE.avatar_data_url || '', resident: true, accessPolicy: residentVE.access_policy || '', registeredAt: residentVE.registered_at || '', machineId: residentVE.machine_id || '', allowedDepartments: residentVE.whitelist || [] }];
        })() : [];
        const userSlots = userFavoriteEmployeeIds.flatMap(id => {
            if (isOwnVirtualEmployeeId(id, config?.remote_machine_id)) return [];
            const ve = veList.find(v => participantIdentityMatches(v.id, id) || participantIdentityMatches(v.machine_id, id));
            if (!ve || !isVirtualEmployeeOnline(ve)) return [];
            if (ve && (isOwnVirtualEmployeeId(ve.id, config?.remote_machine_id) || isOwnVirtualEmployeeId(ve.machine_id || '', config?.remote_machine_id))) return [];
            const participantId = String(ve?.machine_id || id).trim();
            if ([id, ve.id, ve.machine_id || '', participantId].some(alias => favoriteEmployeeIDInAliasSet(alias, residentEmployeeAliasSet))) return [];
            const customName = favoriteEmployeeNames[id] || favoriteEmployeeNames[participantId] || (ve?.id ? favoriteEmployeeNames[ve.id] : '');
            return { veId: participantId, name: customName || ve?.name || id.slice(0, 6), online: isVirtualEmployeeOnline(ve), skillDescription: ve?.skill_description || '', avatarDataURL: ve?.avatar_data_url || '', accessPolicy: ve?.access_policy || '', registeredAt: ve?.registered_at || '', machineId: ve?.machine_id || '', allowedDepartments: ve?.whitelist || [] };
        });
        return [...residentSlots, ...userSlots];
    }, [userFavoriteEmployeeIds, veList, config?.remote_machine_id, favoriteEmployeeNames, residentEmployeeAliasSet]);

    const favoriteEmployeeReplaceSlots = useMemo(() => {
        return userFavoriteEmployeeIds.map(id => {
            const ve = veList.find(v => participantIdentityMatches(v.id, id) || participantIdentityMatches(v.machine_id, id));
            const participantId = String(ve?.machine_id || id).trim();
            const customName = favoriteEmployeeNames[id] || favoriteEmployeeNames[participantId] || (ve?.id ? favoriteEmployeeNames[ve.id] : '');
            return { veId: participantId || id, name: customName || ve?.name || id.slice(0, 6) };
        });
    }, [userFavoriteEmployeeIds, favoriteEmployeeNames, veList]);

    const saveFavoriteEmployeeConfig = useCallback(async (newList: string[], newNames: Record<string, string>, options?: { throwOnError?: boolean }) => {
        const normalized = normalizeFavoriteEmployeeIds(newList);
        const normalizedNames = normalizeFavoriteEmployeeNames(newNames);
        const previousIds = favoriteEmployeeIdsRef.current;
        const previousNames = favoriteEmployeeNamesRef.current;
        setFavoriteEmployeeIds(normalized);
        setFavoriteEmployeeNames(normalizedNames);
        try {
            const updated = await callBackend(() => PatchConfigFields({ favorite_employees: normalized, favorite_employee_names: normalizedNames }));
            setConfig(new corelib.AppConfig(updated));
        } catch (error) {
            if (options?.throwOnError) {
                setFavoriteEmployeeIds(previousIds);
                setFavoriteEmployeeNames(previousNames);
                throw error;
            }
        }
    }, []);

    const updateFavoriteEmployees = useCallback(async (newList: string[]) => {
        await saveFavoriteEmployeeConfig(newList, favoriteEmployeeNames);
    }, [favoriteEmployeeNames, saveFavoriteEmployeeConfig]);

    const updateFavoriteEmployeeNames = useCallback(async (newNames: Record<string, string>) => {
        await saveFavoriteEmployeeConfig(favoriteEmployeeIds, newNames, { throwOnError: true });
    }, [favoriteEmployeeIds, saveFavoriteEmployeeConfig]);

    const handleSetFavoriteEmployee = useCallback((ve: VirtualEmployeeEntry) => {
        if (ve.resident) return;
        const favoriteId = String(ve.machine_id || ve.id || '').trim();
        if (!favoriteId) return;
        if (favoriteEmployeeIds.some(id => participantIdentityMatches(id, favoriteId) || participantIdentityMatches(id, ve.id) || participantIdentityMatches(id, ve.machine_id))) return;
        // Ensure the VE is in veList so favoriteEmployeeSlots can resolve it immediately.
        // Without this, if veList hasn't loaded yet or the fetch failed, the slot would
        // show a fallback name (id.slice(0,6)) instead of the actual VE name.
        if (isVirtualEmployeeOnline(ve)) setVeList(prev => prev.some(v => v.id === ve.id || (!!v.machine_id && !!ve.machine_id && v.machine_id === ve.machine_id)) ? prev : [...prev, ve]);
        if (userFavoriteEmployeeIds.length < MAX_USER_FAVORITES) {
            updateFavoriteEmployees([...userFavoriteEmployeeIds, favoriteId]);
        } else {
            setShowFavReplacePicker({ ve });
        }
    }, [favoriteEmployeeIds, updateFavoriteEmployees, userFavoriteEmployeeIds]);

    const handleAddFavoriteEmployeeFromSettings = useCallback((veId: string) => {
        const existing = veList.find(v => participantIdentityMatches(v.id, veId) || participantIdentityMatches(v.machine_id, veId));
        handleSetFavoriteEmployee(existing || {
            id: veId,
            name: veId,
            skill_description: '',
            access_policy: 'public',
            status: 'active',
            online_status: 'online',
        });
    }, [handleSetFavoriteEmployee, veList]);

    const handleReplaceFavorite = useCallback((index: number) => {
        if (!showFavReplacePicker) return;
        if (index < 0 || index >= userFavoriteEmployeeIds.length) {
            setShowFavReplacePicker(null);
            return;
        }
        const ve = showFavReplacePicker.ve;
        if (ve.resident) {
            setShowFavReplacePicker(null);
            return;
        }
        // Ensure the VE is in veList for immediate resolution (same as handleSetFavoriteEmployee)
        if (isVirtualEmployeeOnline(ve)) setVeList(prev => prev.some(v => v.id === ve.id || (!!v.machine_id && !!ve.machine_id && v.machine_id === ve.machine_id)) ? prev : [...prev, ve]);
        const newList = [...userFavoriteEmployeeIds];
        const previousId = newList[index];
        newList[index] = String(ve.machine_id || ve.id || '').trim() || ve.id;
        const nextNames = { ...favoriteEmployeeNames };
        favoriteEmployeeAliasIds(previousId, veList).forEach(id => { delete nextNames[id]; });
        saveFavoriteEmployeeConfig(newList, nextNames);
        setShowFavReplacePicker(null);
    }, [favoriteEmployeeNames, saveFavoriteEmployeeConfig, showFavReplacePicker, userFavoriteEmployeeIds, veList]);

    const handleRemoveFavoriteEmployee = useCallback((veId: string) => {
        const target = veList.find(v => participantIdentityMatches(v.id, veId) || participantIdentityMatches(v.machine_id, veId));
        if (target?.resident) return;
        const aliases = new Set(favoriteEmployeeAliasIds(veId, veList));
        const nextIds = favoriteEmployeeIds.filter(id => !favoriteEmployeeIDInAliasSet(id, aliases));
        const nextNames = { ...favoriteEmployeeNames };
        aliases.forEach(id => { delete nextNames[id]; });
        saveFavoriteEmployeeConfig(nextIds, nextNames);
    }, [favoriteEmployeeIds, favoriteEmployeeNames, saveFavoriteEmployeeConfig, veList]);

    const handleRenameFavoriteEmployee = useCallback(async (veId: string, name: string) => {
        const nextName = name.trim();
        if (!nextName) return;
        const ve = veList.find(v => participantIdentityMatches(v.id, veId) || participantIdentityMatches(v.machine_id, veId));
        const favoriteId = favoriteEmployeeIds.find(id => participantIdentityMatches(id, veId) || participantIdentityMatches(id, ve?.id) || participantIdentityMatches(id, ve?.machine_id)) || veId;
        await updateFavoriteEmployeeNames({ ...favoriteEmployeeNames, [favoriteId]: nextName });
        setVeList(prev => prev.map(item => (
            participantIdentityMatches(item.id, veId) || participantIdentityMatches(item.machine_id, veId) || participantIdentityMatches(item.id, ve?.id) || participantIdentityMatches(item.machine_id, ve?.machine_id)
                ? { ...item, name: nextName }
                : item
        )));
    }, [favoriteEmployeeIds, favoriteEmployeeNames, updateFavoriteEmployeeNames, veList]);

    const handleReorderFavorites = useCallback((newOrder: string[]) => {
        updateFavoriteEmployees(newOrder.filter(id => !favoriteEmployeeIDInAliasSet(id, residentEmployeeAliasSet)));
    }, [residentEmployeeAliasSet, updateFavoriteEmployees]);

    const handleStartFavoriteVEConversation = useCallback((veId: string) => {
        const ve = veList.find(v => participantIdentityMatches(v.id, veId) || participantIdentityMatches(v.machine_id, veId));
        if (ve) {
            setPendingVEOpen(ve);
        } else {
            // VE not in list yet (still loading or removed): create a minimal entry and let Hub/runtime decide reachability.
            setPendingVEOpen({ id: veId, name: veId.slice(0, 8), skill_description: '', access_policy: 'public', status: 'active', online_status: 'online' });
        }
    }, [veList]);

    // Brand info from backend
    const [brandInfo, setBrandInfo] = useState<{id: string, displayName: string, displayNameCN: string, slogan: string, author: string, businessContact: string, websiteURL: string, githubURL: string, iconPath: string} | null>(null);
    const [brandInfoLoaded, setBrandInfoLoaded] = useState(false);
    const currentIcon = brandInfo?.id === 'qianxin' ? qianxinIcon : appIcon;
    const [aiThemeMode, setAIThemeMode] = useState<'light' | 'dark'>(() => {
        return readStoredAssistantThemeMode();
    });
    const [aiDarkSchemeId, setAIDarkSchemeId] = useState<AssistantDarkSchemeId>(() => {
        return readStoredAssistantDarkSchemeId();
    });
    const handleAIDarkSchemeChange = useCallback((schemeId: AssistantDarkSchemeId) => {
        setAIDarkSchemeId(schemeId);
        writeStoredAssistantDarkSchemeId(schemeId);
    }, []);
    const [aiLightSchemeId, setAILightSchemeId] = useState<AssistantLightSchemeId>(() => {
        return readStoredAssistantLightSchemeId();
    });
    const handleAILightSchemeChange = useCallback((schemeId: AssistantLightSchemeId) => {
        setAILightSchemeId(schemeId);
        writeStoredAssistantLightSchemeId(schemeId);
    }, []);
    // macOS / Windows 11: OS provides native rounded corners for the window.
    // When true, CSS border-radius/border/box-shadow on #App are removed.
    const [nativeRounded, setNativeRounded] = useState(() => {
        // Synchronous best-guess for SSR/initial render: macOS always has native
        // rounding. navigator.platform is deprecated but available synchronously.
        return /mac/i.test(navigator.platform);
    });
    // Windows 10 uses an opaque frameless WebView2 shell to prevent DWM from
    // clipping the outer pixels at scaled DPI. Do not add CSS window decoration
    // there either: it can read as a cropped frame. Linux keeps the CSS frame;
    // macOS / Windows 11 use native window corners instead.
    const [useCSSWindowCorners] = useState(() => {
        const platform = navigator.platform || '';
        return !/win|mac/i.test(platform);
    });
    // Win10's frameless WebView2 host already applies display DPI. Applying an
    // additional fractional CSS transform to the entire root can make the DWM
    // surface exceed its client rectangle and crop every outer edge. Win11 has
    // native frameless support, so it retains the normal Auto recommendation.
    const isWindowsHost = /win/i.test(navigator.platform || '');
    const disableAutoUIScaleTransform = isWindowsHost && !nativeRounded;
    const disableAutoUIScaleTransformRef = useRef(disableAutoUIScaleTransform);
    disableAutoUIScaleTransformRef.current = disableAutoUIScaleTransform;
    const applyFramelessTopInset = useCallback((topInset: unknown) => {
        const insetPx = typeof topInset === 'number' && topInset > 0 ? Math.min(Math.round(topInset), 16) : 0;
        document.documentElement.style.setProperty('--dwm-top-offset', `${insetPx}px`);
    }, []);
    const refreshFramelessTopInset = useCallback(() => {
        void callBackend(() => GetFramelessTopInset())
            .then(applyFramelessTopInset)
            .catch(() => applyFramelessTopInset(0));
    }, [applyFramelessTopInset]);
    // Confirm from backend (authoritative) on mount.
    // Clear boot-time inline colours so the CSS theme token owns the opaque
    // shell; writing a literal scheme colour here can fringe at fractional DPI.
    useEffect(() => {
        Promise.all([
            callBackend(() => IsNativeRoundedCorners()).catch(() => null),
            callBackend(() => GetFramelessTopInset()).catch(() => 0),
        ]).then(([rounded, topInset]) => {
            if (rounded !== null) {
                setNativeRounded(rounded);
            }
            document.documentElement.style.backgroundColor = '';
            document.body.style.backgroundColor = '';
            // Win10 DWM may reserve an invisible top frame on frameless windows.
            // CSS #App uses padding-top: var(--dwm-top-offset) to compensate.
            // Set on :root only — do not set on .app-viewport (would shadow).
            applyFramelessTopInset(topInset);
        });
    }, [applyFramelessTopInset]);
    useEffect(() => {
        let timer: ReturnType<typeof setTimeout> | undefined;
        const refreshAfterResize = () => {
            if (timer) clearTimeout(timer);
            // Moving between monitors can change the window DPI without a
            // remount. Refresh the native metric after WebView2 settles.
            timer = setTimeout(refreshFramelessTopInset, 150);
        };
        window.addEventListener('resize', refreshAfterResize);
        return () => {
            window.removeEventListener('resize', refreshAfterResize);
            if (timer) clearTimeout(timer);
        };
    }, [refreshFramelessTopInset]);
    // When zoom is Auto, re-evaluate after display DPI / resolution changes
    // (monitor move, OS scale change, RDP, etc.). Win10 intentionally keeps
    // Auto at 100% to avoid a fractional root transform; manual UI zoom is
    // still applied exactly as selected in UI Settings.
    useEffect(() => {
        if (!uiZoomAuto) {
            return;
        }
        const applyRecommended = () => {
            const next = recommendUIScale({ disableAutoTransform: disableAutoUIScaleTransform });
            setUiZoom((prev) => (uiScaleEquals(prev, next) ? prev : next));
        };
        applyRecommended();
        if (disableAutoUIScaleTransform) {
            return;
        }
        return subscribeDisplayScaleChanges(applyRecommended);
    }, [disableAutoUIScaleTransform, uiZoomAuto]);
    // Keep the native window and web content on the same scheme token without
    // creating a transparent shell at fractional DPI.
    useEffect(() => {
        const shellPageBg = aiThemeMode === 'dark'
            ? getAssistantDarkScheme(aiDarkSchemeId).cssVars.pageBg
            : getAssistantLightScheme(aiLightSchemeId).cssVars.pageBg;
        document.documentElement.style.setProperty('--theme-page-bg', shellPageBg);
        document.body.style.setProperty('--theme-page-bg', shellPageBg);
        document.documentElement.style.backgroundColor = '';
        document.body.style.backgroundColor = '';
    }, [aiThemeMode, aiDarkSchemeId, aiLightSchemeId]);
    const brandDisplayTitle = brandInfo ? `${brandInfo.displayNameCN} ${brandInfo.displayName}` : '\u7801\u5361\u9f99 MaClaw';
    const brandSidebarName = brandInfo?.displayName || 'MaClaw';
    
    // MaClaw LLM online status (lobster indicator)
    const [maclawLLMOnline, setMaclawLLMOnline] = useState<boolean>(false);
    const [maclawLLMConfigured, setMaclawLLMConfigured] = useState<boolean>(false);
    const [sidebarCurrentProviderTokenUsage, setSidebarCurrentProviderTokenUsage] = useState<SidebarCurrentProviderTokenUsage>({ provider: '', isHubService: false, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0, localCacheRequests: 0, localCacheHits: 0 });
    const [sidebarHubCredits, setSidebarHubCredits] = useState<SidebarHubCredits | null>(null);
    const [moaSession, setMoaSession] = useState<{
        available: boolean;
        active: boolean;
        label?: string;
        preset?: string;
        presets?: Array<{ id: string; display_name?: string; ref_count?: number; enabled?: boolean }>;
    }>({ available: false, active: false });
    const [sidebarProviderSummaries, setSidebarProviderSummaries] = useState<SidebarLLMProviderSummary[]>([]);
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
        if (navTab !== 'ai' && aiPanelMaximized) {
            aiPanelMaximizeSeqRef.current += 1;
            void restoreAIPanelOwnedWindowMaximize();
            setAiPanelMaximized(false);
        }
    }, [navTab, aiPanelMaximized, restoreAIPanelOwnedWindowMaximize]);

    useEffect(() => {
        if (showModelSettings && activeTab === 0) {
            setActiveTab(1);
        }
    }, [showModelSettings, activeTab]);

    // Clear fetched model list when switching provider tabs
    useEffect(() => {
        setFetchedModelList([]);
        setModelListOpen(false);
    }, [activeTab, activeTool]);

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
            callBackend(() => ListSkillsWithInstallStatus(activeTool, installLocation, targetProjectPath))
                .then(list => setSkills(list || []))
                .catch(err => console.error('Failed to load skills:', err));
        }
    }, [showInstallSkillModal, installLocation, installProject, activeTool, config]);

    // Load skills when navigating to skills tab
    useEffect(() => {
        if (navTab === 'skills') {
            callBackend(() => ListSkills(activeTool))
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
    const [showProblemReport, setShowProblemReport] = useState(false);
    const [problemOS, setProblemOS] = useState('');
    const [problemDescription, setProblemDescription] = useState('');
    const [problemScreenshots, setProblemScreenshots] = useState<string[]>([]);
    const [problemScreenshotPreviews, setProblemScreenshotPreviews] = useState<Record<string, string>>({});
    const [problemBusy, setProblemBusy] = useState(false);
    const [problemMessage, setProblemMessage] = useState('');
    const [problemRetryAvailable, setProblemRetryAvailable] = useState(false);
    const [myProblemReports, setMyProblemReports] = useState<any[]>([]);
    const problemDescriptionRef = useRef<HTMLTextAreaElement | null>(null);
    const problemReportDialogRef = useRef<HTMLDivElement | null>(null);
    const problemReportReturnFocusRef = useRef<HTMLElement | null>(null);
    const problemBusyRef = useRef(problemBusy);
    problemBusyRef.current = problemBusy;
    useEffect(() => {
        let cancelled = false;
        if (problemScreenshots.length === 0) {
            setProblemScreenshotPreviews(previous => Object.keys(previous).length === 0 ? previous : {});
            return;
        }
        const activePaths = new Set(problemScreenshots);
        setProblemScreenshotPreviews(previous => {
            const keys = Object.keys(previous);
            if (keys.every(path => activePaths.has(path))) return previous;
            return Object.fromEntries(Object.entries(previous).filter(([path]) => activePaths.has(path)));
        });
        // The effect only reruns when the selected paths change. Avoid an
        // in-flight marker here: React StrictMode intentionally remounts effects
        // during development, and a cancelled first request must not suppress the
        // replacement request.
        const missing = problemScreenshots.filter(path => !problemScreenshotPreviews[path]);
        if (missing.length === 0) return;
        void Promise.all(missing.map(async path => {
            try { return [path, await BugReportScreenshotPreviewDataURL(path)] as const; }
            catch { return [path, ''] as const; }
        })).then(entries => {
            if (cancelled) return;
            setProblemScreenshotPreviews(previous => {
                const next = Object.fromEntries(Object.entries(previous).filter(([path]) => problemScreenshots.includes(path)));
                entries.forEach(([path, preview]) => { if (preview) next[path] = preview; });
                return next;
            });
        });
        return () => { cancelled = true; };
    }, [problemScreenshots]);
    const problemReportStatusLabel = (status: unknown) => {
        const normalized = String(status || '').trim().toLowerCase();
        const keys: Record<string, string> = {
            pending: 'problemReportStatusPending', fixed: 'problemReportStatusFixed', deferred: 'problemReportStatusDeferred',
            rejected: 'problemReportStatusRejected', archived: 'problemReportStatusArchived',
        };
        return keys[normalized] ? t(keys[normalized]) : String(status || '');
    };
    useEffect(() => {
        if (!showProblemReport) return;
        const handleKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape' && !problemBusyRef.current) {
                setShowProblemReport(false);
                return;
            }
            if (event.key !== 'Tab') return;
            const dialog = problemReportDialogRef.current;
            if (!dialog) return;
            const focusable = Array.from(dialog.querySelectorAll<HTMLElement>(
                'button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled), [href], [tabindex]:not([tabindex="-1"])'
            )).filter(element => !element.hasAttribute('hidden'));
            if (!focusable.length) {
                event.preventDefault();
                return;
            }
            const first = focusable[0];
            const last = focusable[focusable.length - 1];
            if (event.shiftKey && document.activeElement === first) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && document.activeElement === last) {
                event.preventDefault();
                first.focus();
            }
        };
        window.addEventListener('keydown', handleKeyDown);
        const frame = window.requestAnimationFrame(() => problemDescriptionRef.current?.focus());
        return () => {
            window.cancelAnimationFrame(frame);
            window.removeEventListener('keydown', handleKeyDown);
            problemReportReturnFocusRef.current?.focus();
            problemReportReturnFocusRef.current = null;
        };
    }, [showProblemReport]);
    const [updateResult, setUpdateResult] = useState<any>(null);
    const [appUpdateAvailable, setAppUpdateAvailable] = useState<AssistantUpdatePayload | null>(null);
    const [isDownloading, setIsDownloading] = useState(false);
    const [downloadProgress, setDownloadProgress] = useState(0);
    const [activeDownloadSource, setActiveDownloadSource] = useState("");
    const [downloadError, setDownloadError] = useState("");
    const [installerPath, setInstallerPath] = useState("");
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
    const toastTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const [sensitivePermissionRequest, setSensitivePermissionRequest] = useState<SensitivePermissionRequest | null>(null);
    const sensitivePermissionRequestRef = useRef<SensitivePermissionRequest | null>(null);
    const [sensitivePermissionQueue, setSensitivePermissionQueue] = useState<SensitivePermissionRequest[]>([]);
    const [skills, setSkills] = useState<corelib.Skill[]>([]);

    const [showAddSkillModal, setShowAddSkillModal] = useState(false);
    const [gossipAllowed, setGossipAllowed] = useState(true);
    const [showRemoteActivationModal, setShowRemoteActivationModal] = useState(false);
    const [pendingRemoteLaunchTool, setPendingRemoteLaunchTool] = useState<string>("");
    const [remoteActivationDraft, setRemoteActivationDraft] = useState({ hub_id: "", hub_url: "", hubcenter_url: "", email: "" });
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
    const [modelListOpen, setModelListOpen] = useState(false);
    const [providerFilter, setProviderFilter] = useState<'all' | 'china' | 'global'>('all');
    const [selectedProviderForUrl, setSelectedProviderForUrl] = useState<ProviderEndpoint | null>(null);
    const [hoveredProvider, setHoveredProvider] = useState<{ provider: ProviderEndpoint, x: number, y: number } | null>(null);

    const showToastMessage = useCallback((message: string, duration: number = 3000) => {
        setToastMessage(message);
        setShowToast(true);
        if (toastTimerRef.current) {
            clearTimeout(toastTimerRef.current);
        }
        toastTimerRef.current = setTimeout(() => {
            setShowToast(false);
            toastTimerRef.current = null;
        }, duration);
    }, []);

    useEffect(() => () => {
        if (toastTimerRef.current) {
            clearTimeout(toastTimerRef.current);
            toastTimerRef.current = null;
        }
    }, []);


    useEffect(() => {
        sensitivePermissionRequestRef.current = sensitivePermissionRequest;
    }, [sensitivePermissionRequest]);

    useEffect(() => {
        const unsubscribe = safeEventsOn('digital-employee-sensitive-request', (payload: SensitivePermissionRequest) => {
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
            // Brief pet reaction for a sensitive authorization prompt. It briefly
            // overrides an in-progress speaking state; the next speaking push (≤900ms
            // throttle) reasserts itself, and the TTL drops the pet back to idle.
            emitDesktopPetState('alert', 've:sensitive-request', 1500);
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
            else safeEventsOff('digital-employee-sensitive-request');
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
            await callBackend(() => RespondDigitalEmployeeSensitiveRequest(request.request_id, decision));
        } catch (err: any) {
            showToastMessage(err?.message || String(err || localizeText('Failed to respond', '响应失败', '回應失敗')));
        }
    }, [sensitivePermissionRequest]);

    const handleShowThanks = async () => {
        try {
            const content = await callBackend(() => ReadThanks());
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
        callBackend(() => ReadThanks())
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
                    await callBackend(() => DeleteSkill(name, activeTool));
                    const list = await callBackend(() => ListSkills(activeTool));
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

    const handleOpenAppUpdate = useCallback(() => {
        if (!appUpdateAvailable) return;
        setUpdateResult(appUpdateAvailable);
        setShowUpdateModal(true);
    }, [appUpdateAvailable]);

    const handleDismissAppUpdate = useCallback((latestVersion: string) => {
        const normalizedVersion = String(latestVersion || "").trim();
        if (normalizedVersion) {
            try {
                window.localStorage.setItem(DISMISSED_APP_UPDATE_VERSION_KEY, normalizedVersion);
            } catch {
                // Ignore storage failures; clearing in-memory notice still honors this click.
            }
        }
        setAppUpdateAvailable(null);
    }, []);

    const handleDownload = async () => {
        if (!updateResult) return;
        // Use download_url if available (added in backend update), fallback to release_url
        const downloadUrl = updateResult.download_url || updateResult.release_url;
        if (!downloadUrl) return;

        setIsDownloading(true);
        setDownloadProgress(0);
        setActiveDownloadSource("");
        setDownloadError("");
        setInstallerPath("");

        const fileName = isWindows ? "MaClaw-Setup.exe" : "MaClaw-Universal.pkg";
        const expectedSHA256 = updateResult.sha256 || "";

        try {
            const path = await DownloadUpdateWithSHA256(downloadUrl, fileName, expectedSHA256);
            setInstallerPath(path);
            // The promise result is authoritative; progress events are auxiliary
            // and may be unavailable in browser-based development sessions.
            setDownloadProgress(100);
            setIsDownloading(false);
        } catch (err: any) {
            console.error("Download error:", err);
            // Some failures (for example, an unavailable Downloads folder) happen
            // before the backend can emit a download-progress error event.
            setDownloadError(err instanceof Error ? err.message : String(err));
            setIsDownloading(false);
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
                showToastMessage(t("installLaunchError").replace("{error}", err as string));
            }
        }
    };

    const handleAIPanelMaximizeToggle = async () => {
        if (aiPanelMaximized) {
            aiPanelMaximizeSeqRef.current += 1;
            await restoreAIPanelOwnedWindowMaximize();
            setAiPanelMaximized(false);
            return;
        }
        const maximizeSeq = ++aiPanelMaximizeSeqRef.current;
        setAiPanelMaximized(true);
        try {
            const alreadyMaximized = await callBackend(() => WindowIsMaximised());
            if (maximizeSeq !== aiPanelMaximizeSeqRef.current || navTabRef.current !== 'ai') return;
            if (!alreadyMaximized) {
                void toggleMaximiseAndClampWorkArea();
                aiPanelMaximizedWindowRef.current = true;
            }
        } catch {
            aiPanelMaximizedWindowRef.current = false;
        }
    };

    const handleWindowHide = (e: React.MouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
        void callBackend(() => WindowHide());
    };

    const handleWindowMaximizeToggle = (e?: React.MouseEvent) => {
        if (e) {
            e.preventDefault();
            e.stopPropagation();
        }
        // AI panel maximization may use native maximize, but never native
        // fullscreen because WebView2 IME input can fail there.
        if (aiPanelMaximized) {
            aiPanelMaximizeSeqRef.current += 1;
            void restoreAIPanelOwnedWindowMaximize();
            setAiPanelMaximized(false);
        } else {
            setWindowMaximized(m => !m); // optimistic update for instant icon feedback
            void toggleMaximiseAndClampWorkArea();
        }
    };

    const handleTaskManagementResizeStart = (e: React.MouseEvent<HTMLDivElement>) => {
        e.preventDefault();
        e.stopPropagation();
        taskManagementResizeStartX.current = e.clientX;
        taskManagementResizeStartWidth.current = taskManagementPaneWidth;
        setIsTaskManagementResizing(true);
    };

    useEffect(() => {
        if (!isTaskManagementResizing) return;
        const handleMove = (event: MouseEvent) => {
            const nextWidth = taskManagementResizeStartWidth.current + event.clientX - taskManagementResizeStartX.current;
            setTaskManagementPaneWidth(Math.min(460, Math.max(180, nextWidth)));
        };
        const handleUp = () => setIsTaskManagementResizing(false);
        window.addEventListener('mousemove', handleMove);
        window.addEventListener('mouseup', handleUp);
        return () => {
            window.removeEventListener('mousemove', handleMove);
            window.removeEventListener('mouseup', handleUp);
        };
    }, [isTaskManagementResizing]);

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
        void callBackend(() => SetLanguage(initialLang));

        // Load brand info from backend
        callBackend(() => GetBrandInfo()).then((info: any) => {
            setBrandInfo(info);
        }).catch(() => {
            setBrandInfo(null);
        }).finally(() => {
            setBrandInfoLoaded(true);
        });

        // Detect OS from backend for Windows Terminal check
        callBackend(() => GetSystemInfo()).then(info => {
            if (info.os === "windows") {
                callBackend(() => IsWindowsTerminalAvailable()).then(available => {
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
        // loadingShellSeq increments when a new loading session starts (startup or
        // manual env re-check). finishLoadingShell only applies to the active seq so
        // a late timeout from the first boot cannot close a later manual check early,
        // and a permanent "finished" flag cannot block manual re-checks.
        let loadingShellSeq = 0;
        let completedWindowTransitionSeq = -1;
        let loadingSafetyTimer: number | null = null;
        const clearLoadingSafetyTimer = () => {
            if (loadingSafetyTimer != null) {
                window.clearTimeout(loadingSafetyTimer);
                loadingSafetyTimer = null;
            }
        };
        const finishLoadingShell = (reason: string, seq: number = loadingShellSeq) => {
            // Ignore stale completions from a previous loading session.
            if (seq !== loadingShellSeq) return;
            clearLoadingSafetyTimer();
            logStartupTrace('finish-loading-shell', { reason, seq });
            setIsLoading(false);
            setIsManualCheck(false);
            // Avoid a second blank gate: isLoading=false with config still null
            // renders only "loadingConfig" (looks like a white screen).
            // Use an explicit empty projects array so later config.projects.* access is safe.
            setConfig((prev) => {
                if (prev) return prev;
                logStartupTrace('config-placeholder-applied', { reason, seq });
                return new corelib.AppConfig({ projects: [] });
            });
        };
        const scheduleMainWindowOverlayReadiness = (seq: number) => {
            // Onboarding is a portal outside #App. Give the workbench two paint
            // frames after the native resize has completed before admitting
            // overlays, avoiding a portal over the compact loading shell.
            window.requestAnimationFrame(() => {
                window.requestAnimationFrame(() => {
                    if (seq === loadingShellSeq) {
                        mainWindowReadyForOverlaysRef.current = true;
                        setMainWindowReadyForOverlays(true);
                    }
                });
            });
        };
        const resizeMainWindow = async () => {
            try {
                const size: any = await callBackend(() => GetAdaptiveWindowSize());
                const w = size?.width || 1360;
                const h = size?.height || 850;
                await callBackend(() => ResizeWindow(w, h));
                return true;
            } catch {
                try {
                    await callBackend(() => ResizeWindow(1360, 850));
                    return true;
                } catch {
                    return false;
                }
            }
        };
        const completeWindowTransition = async (reason: string, seq: number) => {
            // The event and the safety timer can race. Complete the native
            // resize and the React loading-shell transition exactly once for
            // each startup/manual-check sequence.
            if (seq !== loadingShellSeq || completedWindowTransitionSeq === seq) return;
            completedWindowTransitionSeq = seq;
            clearLoadingSafetyTimer();
            logStartupTrace('complete-window-transition', { reason, seq });

            // Never leave the app in the compact environment-check geometry.
            // Keep the resize promise alive after the loading-shell timeout:
            // Wails can acknowledge a native resize later than a busy startup
            // WebView. Closing the shell is safe then, but showing a portaled
            // onboarding dialog is not safe until the resize itself completed.
            const resizePromise = resizeMainWindow();
            void resizePromise.then((resized) => {
                if (resized) scheduleMainWindowOverlayReadiness(seq);
            });
            try {
                await Promise.race([resizePromise, new Promise<void>((resolve) => setTimeout(resolve, 2500))]);
            } finally {
                finishLoadingShell(reason, seq);
            }
        };
        const armLoadingSafetyTimer = (seq: number) => {
            clearLoadingSafetyTimer();
            // Safety net: if env-check-done is missed (event race) or never emitted,
            // promote the window to the main workbench instead of leaving the
            // compact environment-check geometry behind a portal.
            loadingSafetyTimer = window.setTimeout(() => {
                if (seq === loadingShellSeq) {
                    logStartupTrace('env-check-done-timeout', { seq });
                    void completeWindowTransition('timeout', seq);
                }
            }, 15000);
        };
        const beginLoadingShell = (reason: string) => {
            loadingShellSeq += 1;
            const seq = loadingShellSeq;
            logStartupTrace('begin-loading-shell', { reason, seq });
            // Manual environment checks can start after the initial workbench
            // is ready. Reset the guard synchronously so a portal request that
            // arrives before React's next render still cannot cover this shell.
            mainWindowReadyForOverlaysRef.current = false;
            setMainWindowReadyForOverlays(false);
            setIsLoading(true);
            armLoadingSafetyTimer(seq);
            return seq;
        };
        loadingShellCtlRef.current = {
            finish: (reason: string) => { void completeWindowTransition(reason, loadingShellSeq); },
            begin: beginLoadingShell,
            complete: (reason: string) => { void completeWindowTransition(reason, loadingShellSeq); },
        };
        // Startup already mounts with isLoading=true — arm the first safety timer.
        armLoadingSafetyTimer(loadingShellSeq);
        const doneHandler = async () => {
            const seq = loadingShellSeq;
            logStartupTrace('env-check-done-received', { seq });
            await completeWindowTransition('env-check-done', seq);
        };

        safeEventsOn("env-log", logHandler);
        safeEventsOn("env-check-done", doneHandler);
        safeEventsOn("show-env-logs", () => {
            setEnvLogs([]);
            setShowLogs(true);
            setIsManualCheck(true);
        });

        // Tool repair events
        safeEventsOn("tool-repair-start", (toolName: string) => {
            setToolRepairStatus({show: true, toolName, status: 'installing', message: ''});
        });
        safeEventsOn("tool-repair-success", (toolName: string, version: string) => {
            setToolRepairStatus({show: true, toolName, status: 'success', message: version});
            // Auto-close after 2 seconds on success
            setTimeout(() => {
                setToolRepairStatus(prev => ({...prev, show: false}));
            }, 2000);
        });
        safeEventsOn("tool-repair-failed", (toolName: string, error: string) => {
            setToolRepairStatus({show: true, toolName, status: 'failed', message: error});
        });

        safeEventsOn("download-progress", (data: any) => {
            console.log("Download progress event:", data);
            if (data.status === "downloading") {
                setDownloadProgress(Math.floor(data.percentage));
                if (data.source) setActiveDownloadSource(String(data.source));
            } else if (data.status === "verifying") {
                // SHA256 verification in progress — keep showing progress at 100%
                setDownloadProgress(100);
                setIsDownloading(true);
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

        safeEventsOn(EVENT_APP_UPDATE_AVAILABLE, (result: AssistantUpdatePayload) => {
            const payload = result as AssistantUpdatePayload & Record<string, unknown>;
            if (!(payload.has_update || payload.HasUpdate)) return;
            const latestVersion = String(payload.latest_version || payload.LatestVersion || "").trim();
            if (latestVersion) {
                try {
                    if (window.localStorage.getItem(DISMISSED_APP_UPDATE_VERSION_KEY) === latestVersion) return;
                } catch {
                    // localStorage may be unavailable in some embedded/dev contexts.
                }
            }
            setAppUpdateAvailable({ ...result, has_update: true, latest_version: latestVersion });
        });

        void callBackend(() => CheckEnvironment(false)); // Start checks

        // Load environment check interval and check if due
        callBackend(() => GetEnvCheckInterval()).then(val => setEnvCheckInterval(val)).catch(() => {});

        callBackend(() => ShouldCheckEnvironment()).then(due => {
            if (due) {
                // Fetch interval again to use in message
                callBackend(() => GetEnvCheckInterval()).then(days => {
                    const currentLang = initialLang;
                    const localT = (key: string) => translations[currentLang][key] || translations["en"][key] || key;

                    setConfirmDialog({
                        show: true,
                        title: localT("envCheckDueTitle"),
                        message: localT("envCheckDueMessage").replace("{days}", days.toString()),
                        onConfirm: () => {
                            setConfirmDialog(prev => ({ ...prev, show: false }));
                            void callBackend(() => UpdateLastEnvCheckTime());
                            setEnvLogs([]);
                            setShowLogs(true);
                            setIsManualCheck(true);
                            loadingShellCtlRef.current?.begin('manual-env-check');
                            void callBackend(() => CheckEnvironment(true));
                        },
                        onCancel: () => {
                            setConfirmDialog(prev => ({ ...prev, show: false }));
                            void callBackend(() => UpdateLastEnvCheckTime()); // Reset timer even if cancelled
                        }
                    });
                }).catch(() => {});
            }
        }).catch(() => {});

        // Load Python environments
        callBackend(() => ListPythonEnvironments()).then((envs) => {
            setPythonEnvironments(envs);
        }).catch(err => {
            console.error("Failed to load Python environments:", err);
        });

        // Config Logic — LoadConfigForUI omits NLSkills (skills come from ListNLSkills).
        callBackend(() => LoadConfigForUI()).then((cfg) => {
            logStartupTrace('config-loaded-ok', { hasConfig: !!cfg, keys: cfg ? Object.keys(cfg).length : 0 });
            // Apply default launch mode setting on startup
            if (cfg.default_launch_mode === 'remote') {
                cfg.remote_enabled = true;
            } else if (cfg.default_launch_mode === 'local') {
                cfg.remote_enabled = false;
            }
            setConfig(cfg);

            // Apply saved UI zoom factor (0 = Auto / DPI-adaptive)
            callBackend(() => GetUIZoomFactor()).then((z) => {
                applySavedUIZoomFactor(z, setUiZoomAuto, setUiZoom, {
                    disableAutoTransform: disableAutoUIScaleTransformRef.current,
                });
            }).catch(() => {
                applySavedUIZoomFactor(0, setUiZoomAuto, setUiZoom, {
                    disableAutoTransform: disableAutoUIScaleTransformRef.current,
                });
            });
            callBackend(() => GetChatFontSize()).then((s) => {
                if (s >= 12) {
                    setChatFontSize(s);
                }
            }).catch(() => {});

            if (!cfg.pause_env_check) {
                checkTools();
            }

            if (cfg && cfg.language) {
                setLang(cfg.language);
                void callBackend(() => SetLanguage(cfg.language));
            }

            // Automatic update check on startup disabled - use "Online Update" button instead
            if (cfg && cfg.current_project) {
                setSelectedProjectForLaunch(cfg.current_project);
            } else if (cfg && cfg.projects && cfg.projects.length > 0) {
                setSelectedProjectForLaunch(cfg.projects[0].id);
            }
            if (cfg) {
                // Both modes default to AI assistant panel on startup
                if (!startupNavAppliedRef.current) {
                    startupNavAppliedRef.current = true;
                    if (navTabRef.current === "ai") setNavTabNow("ai");
                }

                // Keep track of the last active tool for settings/launch logic
                const lastActiveTool = normalizeToolTab(cfg.active_tool);
                setActiveTool(lastActiveTool);

                callBackend(() => ReadBBS()).then(content => setBbsContent(content)).catch(err => console.error(err));

                const toolCfg = (cfg as any)[lastActiveTool];
                if (toolCfg && toolCfg.models) {
                    const idx = toolCfg.models.findIndex((m: any) => m.model_name === toolCfg.current_model);
                    if (idx !== -1) setActiveTab(idx);

                    // NOTE: removed auto-popup of provider config when no API key is set.
                    // Users can open it manually via the provider config button.
                }
            }
        }).catch(err => {
            logStartupTrace('config-load-failed', { error: String(err) });
            console.error("Failed to load config on startup:", err);
            setStatus(localizeText("Error loading config: ", "加载配置失败：", "載入設定失敗：") + err);
            // Fallback: retry once after a short delay. If the config file was
            // being written by a concurrent SaveConfig, it should be ready now.
            setTimeout(() => {
                callBackend(() => LoadConfigForUI()).then((cfg) => {
                    setConfig(cfg);
                    // The first config load can race a concurrent write. Keep the
                    // retry path in sync with the normal startup path so a
                    // recovered Auto setting does not remain at the bootstrap
                    // 100% scale on Win11/Linux/macOS.
                    callBackend(() => GetUIZoomFactor()).then((z) => {
                        applySavedUIZoomFactor(z, setUiZoomAuto, setUiZoom, {
                            disableAutoTransform: disableAutoUIScaleTransformRef.current,
                        });
                    }).catch(() => {
                        applySavedUIZoomFactor(0, setUiZoomAuto, setUiZoom, {
                            disableAutoTransform: disableAutoUIScaleTransformRef.current,
                        });
                    });
                    if (cfg && cfg.language) {
                        setLang(cfg.language);
                        void callBackend(() => SetLanguage(cfg.language));
                    }
                    // Config recovered late — leave loading shell (and clear safety timer).
                    if (loadingShellCtlRef.current) {
                        loadingShellCtlRef.current.finish('config-retry-ok');
                    } else {
                        setIsLoading(false);
                    }
                }).catch(err2 => {
                    console.error("Retry load config also failed:", err2);
                    // Placeholder config is applied inside finishLoadingShell when prev is null.
                    if (loadingShellCtlRef.current) {
                        loadingShellCtlRef.current.finish('config-retry-failed');
                    } else {
                        setConfig(new corelib.AppConfig({ projects: [] }));
                        setIsLoading(false);
                    }
                });
            }, 1500);
        });

        // Listen for external config changes (e.g. from Tray)
        const applyConfigChange = (cfg: corelib.AppConfig) => {
            setConfig(cfg);
            // Prefer the payload's zoom field to avoid an extra backend round-trip.
            if (typeof cfg.ui_zoom_factor === 'number') {
                applySavedUIZoomFactor(cfg.ui_zoom_factor, setUiZoomAuto, setUiZoom, {
                    disableAutoTransform: disableAutoUIScaleTransformRef.current,
                });
            } else {
                callBackend(() => GetUIZoomFactor()).then((z) => {
                    applySavedUIZoomFactor(z, setUiZoomAuto, setUiZoom, {
                        disableAutoTransform: disableAutoUIScaleTransformRef.current,
                    });
                }).catch(() => {});
            }
            if (typeof cfg.chat_font_size === 'number' && cfg.chat_font_size >= 12) {
                setChatFontSize(cfg.chat_font_size);
            } else {
                callBackend(() => GetChatFontSize()).then((s) => {
                    if (s >= 12) {
                        setChatFontSize(s);
                    }
                }).catch(() => {});
            }
            const tool = normalizeToolTab(cfg.active_tool);
            setActiveTool(tool);
            const toolCfg = (cfg as any)[tool];
            if (toolCfg && toolCfg.models) {
                const idx = toolCfg.models.findIndex((m: any) => m.model_name === toolCfg.current_model);
                if (idx !== -1) setActiveTab(idx);
            }
            // Config refreshes are frequent background events; they must not
            // navigate away from the user's current page.
        };
        const handleConfigChange = (cfg?: corelib.AppConfig) => {
            if (cfg) {
                applyConfigChange(cfg);
                return;
            }
            void callBackend(() => LoadConfigForUI()).then(applyConfigChange).catch((err) => {
                console.warn('Failed to reload config after config event:', err);
            });
        };
        safeEventsOn("config-changed", handleConfigChange);
        safeEventsOn("config-updated", handleConfigChange);

        // QQ Bot status listener
        safeEventsOn("qqbot-status-changed", (status: string) => {
            setQQBotStatus(status);
        });
        // Fetch initial QQ Bot status
        callBackend(() => GetQQBotStatus()).then(setQQBotStatus).catch(() => {});
        callBackend(() => GetQQBotLocalMode()).then(setQQBotLocalModeState).catch(() => {});

        // Telegram Bot status listener
        safeEventsOn("telegram-status-changed", (status: string) => {
            setTelegramStatus(status);
        });
        callBackend(() => GetTelegramStatus()).then(setTelegramStatus).catch(() => {});
        callBackend(() => GetTelegramLocalMode()).then(setTelegramLocalModeState).catch(() => {});

        // WeChat status listener
        safeEventsOn("weixin-status-changed", (status: string) => {
            setWeixinStatus(status);
        });
        callBackend(() => GetWeixinStatus()).then(setWeixinStatus).catch(() => {});
        callBackend(() => GetWeixinLocalMode()).then(setWeixinLocalModeState).catch(() => {});

        safeEventsOn("thirdparty-gateway-status-changed", (status: string) => {
            setThirdPartyGatewayStatus(status);
        });
        callBackend(() => GetThirdPartyGatewayStatus()).then(setThirdPartyGatewayStatus).catch(() => {});
        callBackend(() => GetThirdPartyGatewayLocalMode()).then(setThirdPartyGatewayLocalModeState).catch(() => {});

        // Listen for background tool installation events
        safeEventsOn("tool-checking", (toolName: string) => {
            setBackgroundInstallStatus(`Checking ${toolName}...`);
            setBackgroundInstallingTool("");  // Clear previous tool's installing state
        });

        safeEventsOn("tool-installing", (toolName: string) => {
            setBackgroundInstallStatus(`Installing ${toolName}...`);
            setBackgroundInstallingTool(toolName);
        });

        safeEventsOn("tool-updating", (toolName: string) => {
            setBackgroundInstallStatus(`Updating ${toolName}...`);
            setBackgroundInstallingTool(toolName);
        });

        safeEventsOn("tool-installed", (toolName: string) => {
            console.log("Tool installed in background:", toolName);
            setBackgroundInstallStatus(`${toolName} installed`);
            setBackgroundInstallingTool("");
            setTimeout(() => setBackgroundInstallStatus(""), 3000);
            // Refresh tool statuses
            callBackend(() => CheckToolsStatus()).then(statuses => {
                setToolStatuses(statuses);
            }).catch(() => {});
        });

        safeEventsOn("tool-updated", (toolName: string) => {
            console.log("Tool updated in background:", toolName);
            setBackgroundInstallStatus(`${toolName} updated`);
            setBackgroundInstallingTool("");
            setTimeout(() => setBackgroundInstallStatus(""), 3000);
            // Refresh tool statuses
            callBackend(() => CheckToolsStatus()).then(statuses => {
                setToolStatuses(statuses);
            }).catch(() => {});
        });

        safeEventsOn("tools-install-done", () => {
            console.log("Background tool installation complete");
            setBackgroundInstallStatus("");
            setBackgroundInstallingTool("");
            // Final refresh of tool statuses
            callBackend(() => CheckToolsStatus()).then(statuses => {
                setToolStatuses(statuses);
            }).catch(() => {});
        });

        // Hub security policy: refresh gossip visibility when policy changes (Req 6.1)
        callBackend(() => IsGossipAllowed()).then(setGossipAllowed).catch(() => {});
        safeEventsOn("hub-security-policy-changed", () => {
            callBackend(() => IsGossipAllowed()).then(setGossipAllowed).catch(() => {});
        });

        // Hub auth rejected: admin unbound this user — prompt to re-register.
        safeEventsOn("hub-auth-rejected", () => {
            setHubAuthRejectedPrompt(true);
        });

        return () => {
            clearLoadingSafetyTimer();
            loadingShellCtlRef.current = null;
            safeEventsOff("env-log");
            safeEventsOff("env-check-done");
            safeEventsOff("show-env-logs");
            safeEventsOff("tool-repair-start");
            safeEventsOff("tool-repair-success");
            safeEventsOff("tool-repair-failed");
            safeEventsOff("download-progress");
            safeEventsOff(EVENT_APP_UPDATE_AVAILABLE);
            safeEventsOff("config-changed");
            safeEventsOff("config-updated");
            safeEventsOff("qqbot-status-changed");
            safeEventsOff("telegram-status-changed");
            safeEventsOff("weixin-status-changed");
            safeEventsOff("thirdparty-gateway-status-changed");
            safeEventsOff("tool-checking");
            safeEventsOff("tool-installing");
            safeEventsOff("tool-updating");
            safeEventsOff("tool-installed");
            safeEventsOff("tool-updated");
            safeEventsOff("tools-install-done");
            safeEventsOff("hub-security-policy-changed");
            safeEventsOff("hub-auth-rejected");
        };
    }, []);

    useEffect(() => {
        if (!brandInfoLoaded) return;
        safeEventsOn("lansenger-status-changed", (status: string) => {
            setLansengerStatus(status);
        });
        callBackend(() => GetLansengerStatus()).then(setLansengerStatus).catch(() => {});
        callBackend(() => GetLansengerLocalMode()).then(setLansengerLocalModeState).catch(() => {});
        return () => {
            safeEventsOff("lansenger-status-changed");
        };
    }, [brandInfoLoaded]);

    // Poll MaClaw LLM status every 60 seconds.
    // Also re-ping immediately when the user navigates to/from the LLM settings
    // tab (settingsTab changes), which covers the "just saved config" scenario.
    useEffect(() => {
        const pingLLM = () => {
            callBackend(() => PingMaclawLLM()).then((s: any) => {
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
    //
    // Resilience: a single failed ping (hub 502, network blip during startup) must
    // not trigger the popup. We retry once after 5s before deciding. If the second
    // attempt also fails, show the popup. If it succeeds, skip it.
    useEffect(() => {
        if (!config || !maclawLLMFirstPingResult.current) return;
        const { online } = maclawLLMFirstPingResult.current;
        maclawLLMFirstPingResult.current = null; // consume — only fires once

        if (online) return; // LLM is reachable — no popup needed

        // First ping failed — retry once after a short delay before showing popup
        const retryTimer = setTimeout(() => {
            callBackend(() => PingMaclawLLM()).then((s: any) => {
                if (!s?.online) {
                    requestOnboarding();
                }
                // If online now (transient failure recovered), silently skip popup
            }).catch(() => {
                requestOnboarding();
            });
        }, 5000);
        return () => clearTimeout(retryTimer);
    }, [config, maclawLLMOnline, requestOnboarding]);

    const checkTools = async () => {
        try {
            const statuses = await callBackend(() => CheckToolsStatus());
            setToolStatuses(statuses);
            // Tools are now installed in background by the backend
            // No need to install here - just update the status
        } catch (err) {
            console.error("Failed to check tools:", err);
        }
    };

    const applyLanguage = useCallback((newLang: string) => {
        setLang(newLang);
        void callBackend(() => SetLanguage(newLang));
        if (config) {
            const newConfig = new corelib.AppConfig({ ...config, language: newLang });
            setConfig(newConfig);
            void callBackend(() => PatchConfigFields({ language: newLang })).then((saved) => setConfig(new corelib.AppConfig(saved))).catch((err) => console.error('Failed to save language:', err));
        }
    }, [config]);

    const handleLangChange = (e: React.ChangeEvent<HTMLSelectElement>) => {
        applyLanguage(e.target.value);
    };

    const switchTool = (tool: string) => {
        setNavTabNow(tool);
        setToolDropdownOpen(false);
        if (tool === 'remote') setRemoteInitialSessionTab('remote');
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
            callBackend(() => ReadTutorial()).then(content => setTutorialContent(content)).catch(err => console.error(err));
        }

        if (tool === 'skills') {
            setShowModelSettings(false);
            callBackend(() => ListSkills(activeTool)).then(list => setSkills(list || [])).catch(err => console.error(err));
        }

        if (config) {
            // Don't persist 'ai' as active_tool; it's a UI nav state, not a coding tool
            if (isToolTab(tool)) {
                const newConfig = new corelib.AppConfig({ ...config, active_tool: tool });
                setConfig(newConfig);
                void callBackend(() => PatchConfigFields({ active_tool: tool })).then((saved) => setConfig(new corelib.AppConfig(saved))).catch((err) => console.error('Failed to save active tool:', err));
            }

            const toolCfg = (config as any)[tool];
            if (toolCfg && toolCfg.models) {
                const idx = toolCfg.models.findIndex((m: any) => m.model_name === toolCfg.current_model);
                if (idx !== -1) setActiveTab(idx);
            }
        }
    };

    const openBackgroundTaskMonitor = useCallback(() => {
        setRemoteInitialSessionTab('background');
        setNavTabNow('remote');
        setToolDropdownOpen(false);
    }, [setNavTabNow]);

    useEffect(() => {
        // Keep settingsTab aligned with the tabs actually shown in the rail
        // (e.g. virtualEmployee may be hidden when DE navigation is unavailable).
        const next = resolveSettingsTabId(settingsTab, { hideVirtualEmployee: !veNavigationAvailable });
        if (next !== settingsTab) setSettingsTab(next);
    }, [settingsTab, veNavigationAvailable]);

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
    const launchProjectSelectOptions = useMemo(() => {
        if (normalizedLaunchProjectKeyword.length > 0) return launchProjectOptions;
        if (!resolvedLaunchProject) return launchProjectOptions;
        if (launchProjectOptions.some((p: any) => p.id === resolvedLaunchProject.id)) {
            return launchProjectOptions;
        }
        return [resolvedLaunchProject, ...launchProjectOptions];
    }, [launchProjectOptions, normalizedLaunchProjectKeyword, resolvedLaunchProject]);
    const launchProjectSelectValue = useMemo(() => {
        if (!resolvedLaunchProject) return launchProjectSelectOptions[0]?.id || "";
        return launchProjectSelectOptions.some((p: any) => p.id === resolvedLaunchProject.id)
            ? resolvedLaunchProject.id
            : launchProjectSelectOptions[0]?.id || "";
    }, [launchProjectSelectOptions, resolvedLaunchProject]);
    const activeLaunchProject = useMemo(() => (
        launchProjectSelectOptions.find((p: any) => p.id === launchProjectSelectValue) || null
    ), [launchProjectSelectOptions, launchProjectSelectValue]);
    const launchPanelProject = activeLaunchProject || resolvedLaunchProject;
    const hasSelectableLaunchProject = !!activeLaunchProject;
    const updateResolvedLaunchProject = (updater: (project: any) => any) => {
        if (!config?.projects || !launchPanelProject) return;
        const newProjects = config.projects.map((project: any) =>
            project.id === launchPanelProject.id ? updater(project) : project
        );
        const newConfig = new corelib.AppConfig({ ...config, projects: newProjects });
        setConfig(newConfig);
        void callBackend(() => PatchConfigFields({ projects: newProjects })).then((saved) => setConfig(new corelib.AppConfig(saved))).catch((err) => console.error('Failed to save launch project:', err));
    };
    useEffect(() => {
        if (launchProjectSelectOptions.length === 0) return;
        const nextId = launchProjectSelectOptions[0]?.id || "";
        if (!nextId) return;
        if (resolvedLaunchProject && launchProjectSelectOptions.some((p: any) => p.id === resolvedLaunchProject.id)) return;
        if (selectedProjectForLaunch === nextId) return;
        setSelectedProjectForLaunch(nextId);
    }, [launchProjectSelectOptions, resolvedLaunchProject, selectedProjectForLaunch]);
    const getSelectedProjectForRemote = () => activeLaunchProject?.path || "";
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
        await GroupDiscussionAcceptInvite(inviteId, { accepted_by: "user" } as unknown as a2a.GroupInvitationResponse);
        await refreshGroupDiscussionStatus();
    }, [refreshGroupDiscussionStatus]);

    const handleGroupDiscussionRejectInvite = useCallback(async (inviteId: string) => {
        await GroupDiscussionRejectInvite(inviteId, { reason: "rejected_by_user" } as a2a.GroupInvitationResponse);
        await refreshGroupDiscussionStatus();
    }, [refreshGroupDiscussionStatus]);

    const handleOpenExperienceTrace = useCallback((focus?: string) => {
        setMemoryTraceFocus((prev) => ({ value: String(focus || "").trim(), seq: prev.seq + 1 }));
        setNavTabNow('settings');
        selectSettingsTab('memory');
    }, [selectSettingsTab]);

    useEffect(() => {
        const openPetSettings = () => {
            setNavTabNow('settings');
            selectSettingsTab('pet');
        };
        const unsubscribe = safeEventsOn('open-pet-settings', openPetSettings);
        return unsubscribe;
    }, [selectSettingsTab]);

    useEffect(() => {
        // Desktop pet click (OnFloatingButtonClicked in Go) shows the main
        // window and emits this event with { source, voice }. Switch to the AI
        // assistant panel; when the pet is configured for voice conversation
        // (voice=true), also ask the assistant panel to open voice input.
        const handleSwitchToAIPanel = (payload: any) => {
            setNavTabNow('ai');
            if (payload && payload.voice === true) {
                // The AI panel stays mounted (only hidden), so its voice input
                // hook is already listening for this window event.
                window.dispatchEvent(new CustomEvent(START_VOICE_INPUT_EVENT));
            }
        };
        const unsubscribe = safeEventsOn('switch-to-ai-panel', handleSwitchToAIPanel);
        return unsubscribe;
    }, [setNavTabNow]);

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

    const aiAssistant = useAIAssistant({ refreshSessionsOnly, lang, showConfirm });
    const codingAgentTurnSnapshot = useMemo(
        () => aiAssistant.sending ? latestCodingAgentTurnSnapshot(aiAssistant.progressMessages || []) : null,
        [aiAssistant.sending, aiAssistant.progressMessages],
    );
    const codingAgentProgress = useMemo(
        () => codingAgentTurnSnapshot?.latest || activeCodingAgentProgress(aiAssistant.progressMessages || [], aiAssistant.sending),
        [codingAgentTurnSnapshot, aiAssistant.sending, aiAssistant.progressMessages],
    );
    const refreshTasks = useCallback(() => {
        const generation = ++taskRefreshGenerationRef.current;
        callBackend(() => ListTasks(50)).then((r: any) => {
            if (generation === taskRefreshGenerationRef.current) {
                setTaskItems(r || []);
            }
        }).catch(() => {
            // Preserve the last known list on a transient refresh failure rather
            // than making a newly-created task disappear from the sidebar.
        });
    }, []);
    /** Single registration gateway for every route that opens an AI expert. */
    const ensureExpertTask = useCallback(async (expert: ExpertDefinition) => {
        try {
            const created = await CreateExpertTask(expert.id, expert.name);
            if (!created?.project_path) {
                throw new Error('task record was not created');
            }
            // Invalidate any ListTasks call that started before creation. Its
            // stale response must not erase the just-created expert entry.
            taskRefreshGenerationRef.current += 1;
            setTaskItems(prev => [created, ...prev.filter(item => item.project_path !== created.project_path)].slice(0, 50));
        } catch (error) {
            console.error('[task_management] create expert task failed:', error);
            showAlert(lang === 'zh-Hans'
                ? '无法打开专家：任务项未能保存到左侧任务管理。请稍后重试。'
                : lang === 'zh-Hant'
                    ? '無法開啟專家：任務項未能儲存到左側任務管理。請稍後重試。'
                    : 'The expert could not be opened because its task could not be saved to Task Management. Please try again.');
            refreshTasks();
            throw error;
        }
    }, [lang, refreshTasks, showAlert]);
    /** One frontend gateway for every project/coding task handoff into the AI tab. */
    const openCodingTask = useCallback((launch: Partial<CodingTaskLaunch>) => {
        const normalized = normalizeCodingTaskLaunch(launch);
        if (!normalized) return false;
        setPendingProjectTabOpen(normalized);
        switchTool('ai');
        refreshTasks();
        return true;
    }, [refreshTasks, switchTool]);

    useEffect(() => {
        if (navTab === 'ai') refreshTasks();
    }, [navTab, refreshTasks]);

    useEffect(() => {
        return safeEventsOn('im-startmenu-task-created', (raw: any) => {
            const payload = typeof raw === 'string' ? (() => { try { return JSON.parse(raw); } catch { return {}; } })() : (raw || {});
            const projectPath = String(payload.project_path || '').trim();
            if (!projectPath) return;
            const agentMode = payload.agent_mode === 'coding_dev' || payload.agent_mode === 'remote_coding_dev'
                ? payload.agent_mode
                : undefined;
            openCodingTask({
                projectPath,
                taskTitle: String(payload.task_title || '').trim() || projectPath,
                initialMessage: String(payload.initial_message || '').trim(),
                autoSend: payload.auto_send !== false,
                prepareMode: payload.prepare_mode === 'restore-context' ? 'restore-context' : 'new-agent',
                agentMode,
                remoteHost: String(payload.remote_host || '').trim() || undefined,
                remoteSafety: payload.remote_safety === 'diagnosis' ? 'diagnosis' : undefined,
                remoteNeedsReconnect: payload.remote_needs_reconnect === true,
				imPlatform: String(payload.im_platform || '').trim() || undefined,
				imTargetUID: String(payload.im_target_uid || '').trim() || undefined,
				imIsGroup: payload.im_is_group === true,
            });
        });
    }, [openCodingTask]);

    useEffect(() => {
        const refresh = () => {
            if (navTabRef.current === 'ai') refreshTasks();
        };
        const offProjectIndexChanged = safeEventsOn(EVENT_PROJECT_INDEX_CHANGED, refresh);
        const offTasksChanged = safeEventsOn(EVENT_TASKS_CHANGED, refresh);
        return () => {
            if (typeof offProjectIndexChanged === 'function') offProjectIndexChanged(); else safeEventsOff(EVENT_PROJECT_INDEX_CHANGED);
            if (typeof offTasksChanged === 'function') offTasksChanged(); else safeEventsOff(EVENT_TASKS_CHANGED);
        };
    }, [refreshTasks]);

    const resumeTask = useCallback(async (projectPath: string, task?: { project_path?: string; name?: string; tags?: string[] }) => {
        const startedAt = performance.now();
        try {
            // Normalize separators so Windows path variants still match list tags.
            const norm = (p: string) => p.replace(/\\/g, '/').replace(/\/+$/, '').toLowerCase();
            const want = norm(projectPath);
            const currentRow = task && norm(task.project_path || '') === want ? task : undefined;
            const proj = currentRow || taskItemsRef.current.find(p => norm(p.project_path || '') === want);
            console.info("[task_management] open requested", { taskPath: projectPath, foundInList: !!proj });
            const expertID = expertIDFromTaskTags(proj?.tags);
            if (expertID) {
                const app = await getWailsAppModule();
                const rawExperts = typeof app.ListExperts === 'function' ? await app.ListExperts() : '';
                const expert = parseExpertListJSON(rawExperts).find(item => item.id === expertID);
                if (!expert) {
                    showAlert(lang === 'zh-Hans'
                        ? '该 AI 专家已不可用。请从 AI 专家页面选择其他专家，或移除此任务项。'
                        : lang === 'zh-Hant'
                            ? '此 AI 專家已不可用。請從 AI 專家頁面選擇其他專家，或移除此任務項。'
                            : 'This AI expert is no longer available. Choose another expert from AI Experts or remove this task.');
                    return;
                }
                setPendingExpertOpen({ expert });
                switchTool('ai');
                refreshTasks();
                return;
            }
            await ResumeTask(projectPath);
            const title = proj?.name || projectPath.split(/[\\/]/).pop() || projectPath;
            // Tags from the task list are the source of truth for pure coding restore.
            // Prefer durable tags from the history list; Ensure can also recover
            // kind from sticky/disk when the list cache is briefly stale.
            let agentMode: 'coding_dev' | 'remote_coding_dev' | undefined = agentModeFromTaskTags(proj?.tags);
            let remoteHost = remoteHostFromTaskTags(proj?.tags);
            let remoteSafety: 'diagnosis' | undefined;
            // Re-arm pure coding workbench from sticky/disk memory so multi-turn
            // SubAgent routing continues after tab reopen or process restart.
            // Even when tags are briefly missing from the list cache, Ensure can
            // recover kind from sticky/disk + project index.
            let remoteNeedsReconnect = false;
            try {
                const armStatus = await EnsureCodingWorkbenchArmed(projectPath);
                if (!agentMode) {
                    if (armStatus?.kind === 'remote') agentMode = 'remote_coding_dev';
                    else if (armStatus?.kind === 'local') agentMode = 'coding_dev';
                }
                if (agentMode === 'remote_coding_dev') {
                    remoteNeedsReconnect = !!(armStatus?.needs_reconnect);
                    remoteSafety = armStatus?.remote_safety === 'diagnosis' ? 'diagnosis' : undefined;
                    if (!remoteHost && armStatus?.remote_host) {
                        remoteHost = armStatus.remote_host;
                    }
                }
                console.info("[task_management] coding workbench arm", {
                    taskPath: projectPath,
                    agentMode: agentMode || null,
                    kind: armStatus?.kind || null,
                    armed: !!armStatus?.armed,
                    needsReconnect: remoteNeedsReconnect,
                    turns: armStatus?.turn_count ?? 0,
                });
            } catch (armErr) {
                console.warn("[task_management] EnsureCodingWorkbenchArmed failed", armErr);
                if (agentMode === 'remote_coding_dev') remoteNeedsReconnect = true;
            }
            console.info("[task_management] task ready", { taskPath: projectPath, title, autoSend: false, agentMode, elapsedMs: Math.round(performance.now() - startedAt) });
            openCodingTask({
                projectPath,
                taskTitle: title,
                autoSend: false,
                agentMode,
                remoteHost,
                remoteSafety,
                remoteNeedsReconnect: agentMode === 'remote_coding_dev' ? remoteNeedsReconnect : undefined,
            });
        } catch (error) {
            console.error("resumeTask failed:", error);
        }
    }, [lang, openCodingTask, refreshTasks, showAlert, switchTool]);

    const continueWorkflowProject = useCallback(async (projectPath: string) => {
        const sourcePath = projectPath.trim();
        if (!sourcePath) return;
        try {
            const proj = taskItemsRef.current.find(p => p.project_path === sourcePath || p.active_workflow?.project_path === sourcePath);
            const title = proj?.name || sourcePath.split(/[\\/]/).pop() || sourcePath;
            openCodingTask({
                projectPath: sourcePath,
                taskTitle: title,
                autoSend: false,
            });
        } catch (error) {
            console.error("continueWorkflowProject failed:", error);
        }
    }, [openCodingTask]);

    const createTask = useCallback(async (
        name: string,
        workingDir?: string,
        mode?: 'coding_dev' | 'remote_coding_dev',
        remote?: { host: string; port: number; user: string; password: string; workDir: string; safety?: 'diagnosis' },
    ) => {
        const taskName = name.trim();
        if (!taskName) return;
        try {
            let created: Awaited<ReturnType<typeof CreateTask>> | null = null;
            let agentMode: 'coding_dev' | 'remote_coding_dev' | undefined;
            let remoteHost: string | undefined;
            let remoteSafety: 'diagnosis' | undefined;
            const localWorkDir = (workingDir || '').trim();

            if (mode === 'remote_coding_dev') {
                if (!remote?.host?.trim() || !remote?.user?.trim() || !remote?.password || !remote?.workDir?.trim()) {
                    throw new Error('远程编程需要填写主机、用户名、密码和远程工作目录');
                }
                agentMode = 'remote_coding_dev';
                remoteHost = remote.host.trim();
                remoteSafety = remote.safety === 'diagnosis' ? 'diagnosis' : undefined;
                const createRemoteTask = remote.safety === 'diagnosis'
                    ? CreateRemoteOpsDiagnosisTask
                    : CreateRemoteCodingTask;
                created = await createRemoteTask(
                    taskName,
                    remoteHost,
                    remote.user.trim(),
                    remote.workDir.trim(),
                    remote.port || 22,
                );
                if (!created?.project_path) {
                    throw new Error('创建远程编程任务失败');
                }
                // Connect SSH and arm RemoteCodingSubAgent before opening the tab so
                // autoSend runs pure remote coding (with source preview events).
                try {
                    const prepareRemoteEnvironment = remote.safety === 'diagnosis'
                        ? PrepareRemoteOpsDiagnosisEnvironment
                        : PrepareRemoteCodingEnvironment;
                    await prepareRemoteEnvironment(
                        created.project_path,
                        remoteHost,
                        remote.user.trim(),
                        remote.password,
                        remote.workDir.trim(),
                        remote.port || 22,
                    );
                } catch (prepareError) {
                    // The task may be a reused remote-project record. Keep it on
                    // transient SSH failures so the user can reconnect from the
                    // same task/tab; hiding it here could hide an existing task.
                    throw prepareError;
                }
                // A production-incident password should be entered explicitly
                // for every diagnosis. Standard remote development retains its
                // existing reconnect convenience behavior.
                if (remote.safety !== 'diagnosis') {
                    saveRemoteSSHPassword(
                        remoteHost,
                        remote.user.trim(),
                        remote.password,
                        remote.port || 22,
                        remote.workDir.trim(),
                    );
                }
            } else if (mode === 'coding_dev') {
                agentMode = 'coding_dev';
                created = await CreateTaskWithMode(taskName, localWorkDir, agentMode);
                if (!created?.project_path) {
                    throw new Error('创建编程开发任务失败');
                }
                // Arm local CodingSubAgent so the first auto-sent message executes
                // with tool-using coding path and source preview file events.
                try {
                    await PrepareLocalCodingEnvironment(created.project_path, localWorkDir);
                } catch (prepareError) {
                    // Task record was already created; hide it so failed attempts
                    // don't pile up as orphan tasks in the task list.
                    try {
                        await HideTask(created.project_path);
                    } catch (hideError) {
                        console.warn("HideTask after prepare failure failed:", hideError);
                    }
                    throw prepareError;
                }
            } else {
                created = await CreateTask(taskName, localWorkDir);
            }

            if (created?.project_path) {
                setTaskItems(prev => [created!, ...prev.filter(item => item.project_path !== created!.project_path)].slice(0, 10));
            } else {
                refreshTasks();
                return;
            }
            openCodingTask({
                projectPath: created.project_path,
                taskTitle: created.name || taskName.split('\n')[0]?.trim() || taskName,
                initialMessage: taskName,
                prepareMode: 'new-agent',
                autoSend: true,
                agentMode,
                remoteHost,
                remoteSafety,
            });
        } catch (error) {
            console.error("CreateTask failed:", error);
            // Re-throw so the create dialog can surface the error (e.g. SSH connect failed).
            throw error;
        }
    }, [openCodingTask, switchTool]);

    const meetingRecordInFlightRef = useRef(false);
    /**
     * Utilities → 会议记录.
     * Task title is human-readable; auto-sent text is a tool-router start-recording marker.
     */
    const startMeetingRecord = useCallback(async () => {
        if (meetingRecordInFlightRef.current) return;
        meetingRecordInFlightRef.current = true;
        const taskTitle = meetingRecordTaskTitle(lang);
        const failMsg = meetingRecordFailMessage(lang);
        const command = meetingRecordCommand(lang);
        try {
            const created = await CreateTask(taskTitle, '');
            if (!created?.project_path) {
                showToastMessage(failMsg, 4000);
                return;
            }
            setTaskItems(prev => [created, ...prev.filter(item => item.project_path !== created.project_path)].slice(0, 10));
            // Unified task handoff keeps the tab request and navigation atomic.
            openCodingTask({
                projectPath: created.project_path,
                taskTitle: created.name || taskTitle,
                initialMessage: command,
                prepareMode: 'new-agent',
                autoSend: true,
            });
        } catch (error) {
            console.error('startMeetingRecord failed:', error);
            showToastMessage(failMsg, 4000);
        } finally {
            meetingRecordInFlightRef.current = false;
        }
    }, [lang, openCodingTask, showToastMessage]);

    const normalizeSidebarProviderState = useCallback((data?: SidebarProviderStateWire) => {
        const list = (data?.providers ?? data?.Providers ?? [])
            .map((provider): SidebarLLMProviderSummary => {
                const url = provider?.url ?? provider?.URL ?? '';
                const key = provider?.key ?? provider?.Key ?? '';
                const isHub = !!(provider?.is_hub_service ?? provider?.IsHubService);
                const model = String(provider?.model ?? provider?.Model ?? '').trim();
                const rawModels = provider?.models ?? provider?.Models ?? [];
                const models = Array.isArray(rawModels)
                    ? rawModels.map((m) => String(m || '').trim()).filter(Boolean)
                    : [];
                return {
                    name: provider?.name ?? provider?.Name ?? '',
                    url,
                    isHubService: isHub,
                    configured: isHub || (!!url && !!key),
                    model,
                    models,
                    protocol: String(provider?.protocol ?? provider?.Protocol ?? '').trim() || undefined,
                    agentType: String(provider?.agent_type ?? provider?.AgentType ?? '').trim() || undefined,
                    authType: String(provider?.auth_type ?? provider?.AuthType ?? '').trim() || undefined,
                };
            })
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
                hubStatus = await callBackend(() => GetHubLLMServiceStatus());
            } catch {
                hubStatus = null;
            }
            const hubServiceURL = normalizeProviderURL(hubStatus?.hub_llm_base_url);
            const currentProviderURL = normalizeProviderURL(currentProvider?.url);
            const currentProviderIsHubService = !!currentProvider?.isHubService || (!!hubServiceURL && !!currentProviderURL && hubServiceURL === currentProviderURL);
            const hubCredits = normalizeSidebarHubCredits(hubStatus);
            if (refreshSeq !== sidebarTokenUsageSeqRef.current) return;
            setSidebarCurrentProviderTokenUsage({ provider: currentProviderName, isHubService: currentProviderIsHubService, ...currentProviderUsage });
            setSidebarHubCredits(hubCredits);
            setSidebarProviderSummaries(providerSummaries);
        } catch {
            if (refreshSeq !== sidebarTokenUsageSeqRef.current) return;
            setSidebarCurrentProviderTokenUsage({ provider: '', isHubService: false, input: 0, output: 0, total: 0, cachedInput: 0, cacheWrite: 0, requests: 0, cachedRequests: 0, localCacheRequests: 0, localCacheHits: 0 });
            setSidebarHubCredits(null);
            setSidebarProviderSummaries([]);
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
        // Debounce token usage change events: during an active agent loop, the
        // "llm-token-usage-changed" event fires after EVERY LLM call (potentially
        // every 3-10 seconds). Each refresh triggers GetMaclawLLMProviders →
        // syncHubLLMServiceStatusIntoConfig → Hub HTTP request. Without debounce,
        // a 20-iteration agent loop generates 20 Hub requests in rapid succession.
        // With debounce, we coalesce them into at most one refresh per 10 seconds.
        let tokenUsageDebounceTimer: number | undefined;
        const onTokenUsageChanged = () => {
            if (tokenUsageDebounceTimer !== undefined) {
                window.clearTimeout(tokenUsageDebounceTimer);
            }
            tokenUsageDebounceTimer = window.setTimeout(() => {
                tokenUsageDebounceTimer = undefined;
                void refreshSidebarTokenUsage();
            }, 10_000);
        };
        const offTokenUsageChanged = safeEventsOn("llm-token-usage-changed", onTokenUsageChanged);
        const offHubLLMServiceChanged = safeEventsOn("hub-llm-service-changed", () => {
            // hub-llm-service-changed is rare (redeem, provider switch) — refresh immediately.
            void refreshSidebarTokenUsage();
        });
        const usageRefreshTimer = window.setInterval(() => { void refreshSidebarTokenUsage(); }, 60 * 1000);
        return () => {
            sidebarTokenUsageSeqRef.current += 1;
            window.clearInterval(usageRefreshTimer);
            if (tokenUsageDebounceTimer !== undefined) window.clearTimeout(tokenUsageDebounceTimer);
            delayedRefreshTimers.forEach((timer) => window.clearTimeout(timer));
            if (typeof offTokenUsageChanged === 'function') offTokenUsageChanged(); else safeEventsOff("llm-token-usage-changed");
            if (typeof offHubLLMServiceChanged === 'function') offHubLLMServiceChanged(); else safeEventsOff("hub-llm-service-changed");
        };
    }, [refreshSidebarTokenUsage]);

    const handleLLMProviderChanged = useCallback(() => {
        void refreshSidebarTokenUsage();
        void callBackend(() => LoadConfigForUI()).then((freshConfig) => setConfig(freshConfig)).catch((err) => {
            console.warn('Failed to reload config after LLM provider change:', err);
        });
    }, [refreshSidebarTokenUsage]);

    const openHubCreditsPage = useCallback(() => {
        const url = buildHubCreditsURL((config as any)?.remote_hub_url, (config as any)?.remote_viewer_token, (config as any)?.remote_tenant_id, (config as any)?.remote_email, (config as any)?.remote_user_id, (config as any)?.remote_mobile);
        if (url) {
            safeBrowserOpenURL(url);
            return;
        }
        showAlert(lang === 'zh-Hans' ? 'Hub \u5730\u5740\u7f3a\u5931\uff0c\u6682\u65f6\u65e0\u6cd5\u6253\u5f00 Credits \u9875\u9762\u3002' : 'Hub URL is missing, so the Credits page cannot be opened.');
    }, [config, lang, showAlert]);

    const openServiceRedeemPage = useCallback(() => {
        setNavTabNow('settings');
        selectSettingsTab('redeem');
    }, [selectSettingsTab]);

    const openLLMSettingsPage = useCallback(() => {
        setNavTabNow('settings');
        selectSettingsTab('llm');
    }, [selectSettingsTab]);

    // ── Provider quick-switch: compute available list + handler ──
    const availableProvidersForSwitch = useMemo((): SidebarLLMProviderSummary[] => {
        // Only include providers that are confirmed available:
        // - Official hub provider: only if LLM is online AND hub credits authorized + not expired/exhausted
        // - Third-party providers: only if LLM is online AND configured (has url/key/model)
        if (!maclawLLMOnline) return [];
        const currentName = sidebarCurrentProviderTokenUsage.provider;
        const result: SidebarLLMProviderSummary[] = [];
        // Current provider is always "available" (it's what we just pinged successfully)
        if (currentName) {
            const currentSummary = sidebarProviderSummaries.find((p) => p.name === currentName);
            result.push({
                name: currentName,
                url: currentSummary?.url || '',
                isHubService: sidebarCurrentProviderTokenUsage.isHubService,
                model: currentSummary?.model,
                models: currentSummary?.models,
                protocol: currentSummary?.protocol,
                agentType: currentSummary?.agentType,
                authType: currentSummary?.authType,
                configured: true,
            });
        }
        // Add other configured providers from the backend's GetMaclawLLMProviders() result
        for (const p of sidebarProviderSummaries) {
            if (!p.name || p.name === currentName) continue;
            // Skip unconfigured providers (no URL+key for third-party, or not a hub service)
            if (!p.configured) continue;
            // Official hub provider: only include if hub credits are authorized and not expired/exhausted
            if (p.isHubService) {
                if (!sidebarHubCredits?.authorized) continue;
                const hubStatus = String(sidebarHubCredits.status || '').toLowerCase();
                if (hubStatus === 'expired' || hubStatus === 'exhausted') continue;
                // period_limited with service stopped = effectively unavailable
                if (hubStatus === 'period_limited' && sidebarHubCredits.serviceActive === false) continue;
            }
            result.push(p);
        }
        return result;
    }, [maclawLLMOnline, sidebarCurrentProviderTokenUsage, sidebarProviderSummaries, sidebarHubCredits]);

    const handleQuickSwitchProvider = useCallback((providerName: string) => {
        // Optimistic update: immediately show new provider name in sidebar
        setSidebarCurrentProviderTokenUsage(prev => ({ ...prev, provider: providerName }));
        // Switching main provider clears sticky multi-model council on backend.
        setMoaSession(prev => ({ ...prev, active: false }));
        callBackend(() => PatchConfigFields({ maclaw_llm_current_provider: providerName })).then((saved) => {
            setConfig(new corelib.AppConfig(saved));
            // Refresh sidebar display with authoritative data from backend
            void refreshSidebarTokenUsage();
            // Toast notification
            const toastMsg = lang === 'en'
                ? `Switched to "${providerName}". Effective on next AI request.`
                : lang === 'zh-Hant'
                    ? `\u5df2\u5207\u63db\u70ba\u300c${providerName}\u300d\uff0c\u4e0b\u6b21 AI \u5c0d\u8a71\u6642\u751f\u6548`
                    : `\u5df2\u5207\u6362\u4e3a\u300c${providerName}\u300d\uff0c\u4e0b\u6b21 AI \u5bf9\u8bdd\u65f6\u751f\u6548`;
            showToastMessage?.(toastMsg);
        }).catch((err) => {
            console.error('Failed to switch LLM provider:', err);
            // Revert optimistic update on failure
            void refreshSidebarTokenUsage();
            const errMsg = lang === 'en'
                ? `Failed to switch provider: ${err}`
                : lang === 'zh-Hant'
                    ? `\u5207\u63db\u670d\u52d9\u5546\u5931\u6557\uff1a${err}`
                    : `\u5207\u6362\u670d\u52a1\u5546\u5931\u8d25\uff1a${err}`;
            showAlert(errMsg);
        });
    }, [lang, refreshSidebarTokenUsage, showAlert]);

    // Sidebar model quick-switch: options + current model for the active provider.
    const [sidebarModelOptions, setSidebarModelOptions] = useState<string[]>([]);
    const [sidebarCurrentModel, setSidebarCurrentModel] = useState('');
    const [sidebarModelsLoading, setSidebarModelsLoading] = useState(false);
    const sidebarModelsSeqRef = useRef(0);
    const sidebarProviderSummariesRef = useRef(sidebarProviderSummaries);
    const availableProvidersForSwitchRef = useRef(availableProvidersForSwitch);
    sidebarProviderSummariesRef.current = sidebarProviderSummaries;
    availableProvidersForSwitchRef.current = availableProvidersForSwitch;

    const lookupProviderSnapshot = useCallback((providerName: string) => {
        const name = String(providerName || '').trim();
        return sidebarProviderSummariesRef.current.find((p) => p.name === name)
            || availableProvidersForSwitchRef.current.find((p) => p.name === name)
            || null;
    }, []);

    // Provider name change only: drop previous catalog to avoid cross-provider flash.
    useEffect(() => {
        const currentName = sidebarCurrentProviderTokenUsage.provider;
        const current = lookupProviderSnapshot(currentName);
        const configured = String(current?.model || '').trim();
        setSidebarCurrentModel(configured);
        setSidebarModelOptions(buildSidebarModelOptions({
            configuredModel: configured,
            cachedModels: current?.models,
        }));
        sidebarModelsSeqRef.current += 1;
        setSidebarModelsLoading(false);
    }, [sidebarCurrentProviderTokenUsage.provider, lookupProviderSnapshot]);

    // Summaries arrived/updated for the same provider: seed configured model without wiping a fetched catalog.
    useEffect(() => {
        const currentName = sidebarCurrentProviderTokenUsage.provider;
        const current = lookupProviderSnapshot(currentName);
        const configured = String(current?.model || '').trim();
        if (!configured) return;
        setSidebarCurrentModel((prev) => (prev === configured ? prev : configured));
        setSidebarModelOptions((prev) => {
            if (prev.length === 0) {
                return buildSidebarModelOptions({
                    configuredModel: configured,
                    cachedModels: current?.models,
                });
            }
            if (prev.includes(configured)) return prev;
            return buildSidebarModelOptions({
                configuredModel: configured,
                cachedModels: current?.models,
                fetchedModels: prev,
            });
        });
    }, [sidebarProviderSummaries, sidebarCurrentProviderTokenUsage.provider, lookupProviderSnapshot]);

    const refreshSidebarModelOptions = useCallback(async () => {
        const seq = ++sidebarModelsSeqRef.current;
        const providerAtStart = sidebarCurrentProviderTokenUsage.provider;
        // Immediate local fallback so the open menu never shows another provider's models.
        const snapshot = lookupProviderSnapshot(providerAtStart);
        const snapshotConfigured = String(snapshot?.model || '').trim();
        setSidebarCurrentModel(snapshotConfigured);
        setSidebarModelOptions(buildSidebarModelOptions({
            configuredModel: snapshotConfigured,
            cachedModels: snapshot?.models,
        }));
        setSidebarModelsLoading(true);
        try {
            // Prefer full provider payload (includes key for fetch); summaries may omit secrets.
            const raw = await callBackend(() => GetMaclawLLMProviders()) as SidebarProviderStateWire;
            if (seq !== sidebarModelsSeqRef.current) return;
            const normalized = normalizeSidebarProviderState(raw);
            const currentName = normalized.current || providerAtStart;
            const list = (raw?.providers ?? raw?.Providers ?? []) as Array<Record<string, any>>;
            const wire = list.find((p) => String(p?.name ?? p?.Name ?? '') === currentName)
                || list.find((p) => String(p?.name ?? p?.Name ?? '') === providerAtStart);
            const summary = normalized.providers.find((p) => p.name === currentName)
                || normalized.providers.find((p) => p.name === providerAtStart);
            const configured = String(wire?.model ?? wire?.Model ?? summary?.model ?? snapshotConfigured).trim();
            const cached = Array.isArray(wire?.models ?? wire?.Models)
                ? (wire?.models ?? wire?.Models).map((m: any) => String(m || '').trim()).filter(Boolean)
                : (summary?.models || snapshot?.models || []);
            const url = String(wire?.url ?? wire?.URL ?? summary?.url ?? snapshot?.url ?? '').trim();
            const key = String(wire?.key ?? wire?.Key ?? '').trim();
            const protocol = String(wire?.protocol ?? wire?.Protocol ?? summary?.protocol ?? 'openai').trim() || 'openai';
            const agentType = String(wire?.agent_type ?? wire?.AgentType ?? summary?.agentType ?? 'openclaw').trim() || 'openclaw';
            const authType = String(wire?.auth_type ?? wire?.AuthType ?? summary?.authType ?? '').trim().toLowerCase();
            const isManagedAuth = authType === 'oauth' || authType === 'sso';
            const isHub = !!(summary?.isHubService || wire?.is_hub_service || wire?.IsHubService);

            let fetched: string[] = [];
            if (url && (key || isManagedAuth || isHub)) {
                try {
                    const items = await callBackend(() => FetchProviderModels(url, key, protocol, agentType)) as Array<{ id?: string; name?: string }>;
                    if (seq !== sidebarModelsSeqRef.current) return;
                    fetched = (items || []).map((m) => String(m?.id || m?.name || '').trim()).filter(Boolean);
                } catch {
                    // Fall back to configured / cached models below.
                    fetched = [];
                }
            }

            if (seq !== sidebarModelsSeqRef.current) return;
            const options = buildSidebarModelOptions({
                configuredModel: configured,
                cachedModels: cached,
                fetchedModels: fetched,
            });
            setSidebarCurrentModel(configured);
            setSidebarModelOptions(options);
        } catch {
            if (seq !== sidebarModelsSeqRef.current) return;
            const current = lookupProviderSnapshot(providerAtStart);
            const configured = String(current?.model || snapshotConfigured).trim();
            setSidebarCurrentModel(configured);
            setSidebarModelOptions(buildSidebarModelOptions({
                configuredModel: configured,
                cachedModels: current?.models,
            }));
        } finally {
            if (seq === sidebarModelsSeqRef.current) setSidebarModelsLoading(false);
        }
    }, [
        normalizeSidebarProviderState,
        sidebarCurrentProviderTokenUsage.provider,
        lookupProviderSnapshot,
    ]);

    const handleQuickSwitchModel = useCallback((modelId: string) => {
        const next = String(modelId || '').trim();
        if (!next) return;
        const previous = sidebarCurrentModel;
        setSidebarCurrentModel(next);
        setSidebarModelOptions((prev) => (
            prev.includes(next) ? prev : buildSidebarModelOptions({ configuredModel: next, fetchedModels: prev })
        ));
        callBackend(() => SetMaclawLLMCurrentModel(next)).then(() => {
            void refreshSidebarTokenUsage();
            const toastMsg = lang === 'en'
                ? `Model switched to "${next}". Effective on next AI request.`
                : lang === 'zh-Hant'
                    ? `\u5df2\u5207\u63db\u6a21\u578b\u70ba\u300c${next}\u300d\uff0c\u4e0b\u6b21 AI \u5c0d\u8a71\u6642\u751f\u6548`
                    : `\u5df2\u5207\u6362\u6a21\u578b\u4e3a\u300c${next}\u300d\uff0c\u4e0b\u6b21 AI \u5bf9\u8bdd\u65f6\u751f\u6548`;
            showToastMessage?.(toastMsg);
        }).catch((err) => {
            console.error('Failed to switch LLM model:', err);
            setSidebarCurrentModel(previous);
            void refreshSidebarTokenUsage();
            void refreshSidebarModelOptions();
            const errMsg = lang === 'en'
                ? `Failed to switch model: ${err}`
                : lang === 'zh-Hant'
                    ? `\u5207\u63db\u6a21\u578b\u5931\u6557\uff1a${err}`
                    : `\u5207\u6362\u6a21\u578b\u5931\u8d25\uff1a${err}`;
            showAlert(errMsg);
        });
    }, [lang, refreshSidebarTokenUsage, refreshSidebarModelOptions, showAlert, sidebarCurrentModel]);

    const refreshMoASession = useCallback(() => {
        callBackend(() => GetMoASessionState()).then((raw: any) => {
            const presets = Array.isArray(raw?.presets)
                ? raw.presets.filter((p: any) => p && typeof p === 'object')
                : [];
            setMoaSession({
                available: !!raw?.available,
                active: !!raw?.sticky,
                label: String(raw?.display_name || raw?.preset || '').trim() || undefined,
                preset: String(raw?.preset || '').trim() || undefined,
                presets,
            });
        }).catch(() => {
            setMoaSession({ available: false, active: false });
        });
    }, []);

    useEffect(() => {
        refreshMoASession();
        const off = safeEventsOn('moa-session-changed', () => refreshMoASession());
        return () => {
            if (typeof off === 'function') off();
            else safeEventsOff('moa-session-changed');
        };
    }, [refreshMoASession]);

    const handleToggleMoASticky = useCallback((on: boolean, presetId?: string) => {
        const op = on
            ? () => SetMoAStickyPreset(presetId && presetId !== '_default_' ? presetId : '_default_')
            : () => SetMoASticky(false);
        callBackend(op).then(() => {
            setMoaSession(prev => ({ ...prev, active: on, preset: on ? (presetId || prev.preset) : undefined }));
            const toastMsg = on
                ? (lang === 'en'
                    ? 'Multi-model council kept on for this session.'
                    : lang === 'zh-Hant'
                        ? '\u5df2\u958b\u555f\u672c\u6703\u8a71\u591a\u6a21\u578b\u6703\u8a3a'
                        : '\u5df2\u5f00\u542f\u672c\u4f1a\u8bdd\u591a\u6a21\u578b\u4f1a\u8bca')
                : (lang === 'en'
                    ? 'Multi-model council session mode turned off.'
                    : lang === 'zh-Hant'
                        ? '\u5df2\u95dc\u9589\u672c\u6703\u8a71\u591a\u6a21\u578b\u6703\u8a3a'
                        : '\u5df2\u5173\u95ed\u672c\u4f1a\u8bdd\u591a\u6a21\u578b\u4f1a\u8bca');
            showToastMessage?.(toastMsg);
            refreshMoASession();
        }).catch((err) => {
            const errMsg = lang === 'en'
                ? `Failed to toggle multi-model council: ${err}`
                : lang === 'zh-Hant'
                    ? `\u5207\u63db\u591a\u6a21\u578b\u6703\u8a3a\u5931\u6557\uff1a${err}`
                    : `\u5207\u6362\u591a\u6a21\u578b\u4f1a\u8bca\u5931\u8d25\uff1a${err}`;
            showAlert(errMsg);
            refreshMoASession();
        });
    }, [lang, refreshMoASession, showAlert]);

    const openHubCardStorePage = useCallback(async () => {
        try {
            const freshConfig = await callBackend(() => LoadConfigForUI());
            const sourceConfig = freshConfig || config;
            const url = buildHubCardStoreURL((sourceConfig as any)?.remote_hub_url, (sourceConfig as any)?.remote_tenant_id, (sourceConfig as any)?.remote_email, (sourceConfig as any)?.remote_viewer_token, (sourceConfig as any)?.remote_hubcenter_url, (sourceConfig as any)?.remote_hub_id, undefined, (sourceConfig as any)?.remote_tenant_name, (sourceConfig as any)?.remote_user_id, (sourceConfig as any)?.remote_mobile);
            if (url) {
                safeBrowserOpenURL(url);
                return;
            }
            showAlert(lang === 'zh-Hans' ? 'Hub \u5730\u5740\u7f3a\u5931\uff0c\u6682\u65f6\u65e0\u6cd5\u6253\u5f00\u670d\u52a1\u5361\u5546\u5e97\u3002' : lang === 'zh-Hant' ? 'Hub \u4f4d\u5740\u7f3a\u5931\uff0c\u66ab\u6642\u7121\u6cd5\u6253\u958b\u670d\u52d9\u5361\u5546\u5e97\u3002' : 'Hub URL is missing, so the card store cannot be opened.');
        } catch (error) {
            showAlert(String(error || (lang === 'zh-Hans' ? '\u6253\u5f00\u670d\u52a1\u5361\u5546\u5e97\u5931\u8d25' : lang === 'zh-Hant' ? '\u6253\u958b\u670d\u52d9\u5361\u5546\u5e97\u5931\u6557' : 'Failed to open card store')));
        }
    }, [config, lang, showAlert]);

    const openMaclawAppManualPage = useCallback(async () => {
        try {
            const freshConfig = await callBackend(() => LoadConfigForUI());
            const sourceConfig = freshConfig || config;
            const url = buildHubMaclawAppManualURL((sourceConfig as any)?.remote_hub_url, lang);
            if (url) {
                safeBrowserOpenURL(url);
                return;
            }
            showAlert(pickMiniAppLabel(lang, miniAppLabels.manualMissingHub));
        } catch (error) {
            showAlert(String(error || pickMiniAppLabel(lang, miniAppLabels.manualOpenFailed)));
        }
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

    const showHubCreditAction = !!sidebarHubCredits && (
        sidebarHubCredits.status === 'period_limited' ||
        (sidebarHubCredits.authorized && !sidebarHubCredits.unlimited && sidebarHubCredits.remaining < 500)
    );

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
            } catch {
                if (!cancelled) {
                    setSidebarBgLoops([]);
                }
            }
        };
        refresh();
        const cleanup = safeEventsOn("background-loops-changed", refresh);
        const timer = setInterval(refresh, 5000);
        return () => {
            cancelled = true;
            clearInterval(timer);
            if (typeof cleanup === "function") cleanup(); else safeEventsOff("background-loops-changed");
        };
    }, []);

    const activeBackgroundLoopCount = useMemo(() => countActiveBackgroundLoops(sidebarBgLoops), [sidebarBgLoops]);

    // Count running (non-terminal) sessions + background loops for the sidebar badge
    const runningTaskCount = useMemo(() => {
        const remoteCount = remoteSessions.filter((session) => {
            const status = String(session.status || session.summary?.status || "").toLowerCase();
            return !TERMINAL_SESSION_STATUSES.has(status);
        }).length;
        return remoteCount + activeBackgroundLoopCount;
    }, [remoteSessions, activeBackgroundLoopCount]);

    const backgroundTaskCount = useMemo(() => {
        const aiSessionCount = remoteSessions.filter((session) => {
            if ((session.launch_source || "") !== "ai") return false;
            const status = String(session.status || session.summary?.status || "").toLowerCase();
            return !TERMINAL_SESSION_STATUSES.has(status);
        }).length;
        return activeBackgroundLoopCount + aiSessionCount;
    }, [remoteSessions, activeBackgroundLoopCount]);

    // Show onboarding only after the environment-check completion path has
    // resized and painted the main workbench. The wizard is portaled to
    // document.body, so rendering it earlier would place it over the compact
    // environment-check shell.
    const onboardingRegCheckDone = useRef(false);
    useEffect(() => {
        if (onboardingRegCheckDone.current || !mainWindowReadyForOverlays || !config || remoteActivationStatus === null) return;
        onboardingRegCheckDone.current = true;
        if (!remoteActivationStatus.activated && !config.onboarding_done) {
            requestOnboarding();
        }
    }, [config, mainWindowReadyForOverlays, remoteActivationStatus, requestOnboarding]);
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

        const newConfig = new corelib.AppConfig(configCopy);
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
                const newConfig = new corelib.AppConfig({ ...config, [activeTool]: { ...toolCfg, models: newModels } });

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
        const newConfig = new corelib.AppConfig({ ...config, [activeTool]: toolCfg });
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
                    const newConfig = new corelib.AppConfig({ ...config, [activeTool]: toolCfg });
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

        const newConfig = new corelib.AppConfig({ ...config, [activeTool]: toolCfg });
        setConfig(newConfig);
    };

    const handleModelIdChange = (id: string) => {
        if (!config) return;
        const toolCfg = JSON.parse(JSON.stringify((config as any)[activeTool]));
        toolCfg.models[activeTab].model_id = id;
        const newConfig = new corelib.AppConfig({ ...config, [activeTool]: toolCfg });
        setConfig(newConfig);
    };

    const handleWireApiChange = (api: string) => {
        if (!config) return;
        const toolCfg = JSON.parse(JSON.stringify((config as any)[activeTool]));
        toolCfg.models[activeTab].wire_api = api;
        const newConfig = new corelib.AppConfig({ ...config, [activeTool]: toolCfg });
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
            if (p.includes("glm")) return "glm-5.1";
            if (p.includes("doubao")) return "doubao-seed-code-preview-latest";
            if (p.includes("kimi")) return "kimi-for-coding";
            if (p.includes("minimax")) return "MiniMax-M2.1";
        } else if (tool === "opencode" || tool === "codebuddy" || tool === "iflow" || tool === "kilo") {
            if (p.includes("deepseek")) return "deepseek-chat";
            if (p.includes("glm")) return "glm-5.1";
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
        const newConfig = new corelib.AppConfig({ ...config, [activeTool]: newToolCfg });
        const isCodexProviderSwitch = activeTool === "codex";
        setConfig(newConfig);
        setStatus(t("syncing"));
        if (isCodexProviderSwitch) {
            setCodexConfigUpdateCount((count) => count + 1);
        }
        callBackend(() => PatchConfigFields({ tool_current_model: { tool: activeTool, model: modelName } })).then((saved) => {
            setConfig(new corelib.AppConfig(saved));
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
        const newConfig = new corelib.AppConfig({ ...config, current_project: projectId });
        setConfig(newConfig);
        setSelectedProjectForLaunch(projectId);
        setStatus(t("projectSwitched"));
        setTimeout(() => setStatus(""), 1500);
        void callBackend(() => PatchConfigFields({ current_project: projectId })).then((saved) => setConfig(new corelib.AppConfig(saved))).catch((err) => console.error('Failed to save current project:', err));
    };

    const handleSelectDir = () => {
        if (!config) return;
        callBackend(() => SelectProjectDir()).then((dir) => {
            if (dir && dir.length > 0) {
                const currentProj = getCurrentProject();
                if (!currentProj) return;
                const projects = config.projects || [];

                const newProjects = projects.map((p: any) =>
                    p.id === currentProj.id ? { ...p, path: dir } : p
                );

                const newConfig = new corelib.AppConfig({ ...config, projects: newProjects, project_dir: dir });
                setConfig(newConfig);
                setStatus(t("dirUpdated"));
                setTimeout(() => setStatus(""), 1500);
                void callBackend(() => PatchConfigFields({ projects: newProjects })).then((saved) => setConfig(new corelib.AppConfig(saved))).catch((err) => console.error('Failed to save project directory:', err));
            }
        });
    };

    const handleYoloChange = (checked: boolean) => {
        if (!config) return;
        const currentProj = getCurrentProject();
        if (!currentProj) return;
        const projects = config.projects || [];

        const newProjects = projects.map((p: any) =>
            p.id === currentProj.id ? { ...p, yolo_mode: checked } : p
        );

        const newConfig = new corelib.AppConfig({ ...config, projects: newProjects });
        setConfig(newConfig);
        setStatus(t("saved"));
        setTimeout(() => setStatus(""), 1500);
        void callBackend(() => PatchConfigFields({ projects: newProjects })).then((saved) => setConfig(new corelib.AppConfig(saved))).catch((err) => console.error('Failed to save project yolo mode:', err));
    };

    const openRemoteActivationModal = (toolName: string) => {
        const configuredHubCenterURL = config?.remote_hubcenter_url || "";
        const nextHubCenterURL = /(?:^|\/\/|\[)(?:127(?:\.\d{1,3}){3}|0\.0\.0\.0|::1|localhost)(?::|\]|\/|$)/i.test(configuredHubCenterURL) ? "" : configuredHubCenterURL;
        const nextEmail = config?.remote_email || "";
        setPendingRemoteLaunchTool(toolName);
        setRemoteActivationDraft({
            hub_id: (config as any)?.remote_hub_id || "",
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
            const hubs = await callBackend(() => ListRemoteHubs(centerURL, email)) as RemoteCenterHubOption[];
            setRemoteCenterHubs(Array.isArray(hubs) ? hubs : []);
            if (Array.isArray(hubs) && hubs.length > 0) {
                setRemoteActivationDraft((prev) => {
                    const currentHubURL = prev.hub_url.trim().replace(/\/+$/, "");
                    if (!prev.hub_id.trim() && currentHubURL) {
                        const matched = hubs.find((hub) => String(hub.base_url || "").trim().replace(/\/+$/, "") === currentHubURL);
                        if (matched?.hub_id) {
                            return { ...prev, hub_id: matched.hub_id, hub_url: matched.base_url || prev.hub_url };
                        }
                    }
                    if (prev.hub_url.trim()) {
                        return prev;
                    }
                    return { ...prev, hub_id: hubs[0].hub_id || "", hub_url: hubs[0].base_url || "" };
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
        const hubID = remoteActivationDraft.hub_id.trim();
        const hubCenterURL = remoteActivationDraft.hubcenter_url.trim();
        const safeHubCenterURL = /(?:^|\/\/|\[)(?:127(?:\.\d{1,3}){3}|0\.0\.0\.0|::1|localhost)(?::|\]|\/|$)/i.test(hubCenterURL) ? "" : hubCenterURL;
        const email = remoteActivationDraft.email.trim();
        const newConfig = new corelib.AppConfig({
            ...config,
            remote_hub_id: hubID,
            remote_hub_url: hubURL,
            remote_hubcenter_url: safeHubCenterURL,
            remote_email: email,
            remote_enabled: true,
        });
        setConfig(newConfig);
        const saved = await callBackend(() => PatchConfigFields({
            remote_hub_id: hubID,
            remote_hub_url: hubURL,
            remote_hubcenter_url: safeHubCenterURL,
            remote_email: email,
            remote_enabled: true,
        }));
        setConfig(new corelib.AppConfig(saved));
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

        const existingProjects = config.projects || [];
        let baseName = "Project";
        let newName = "";
        let i = 1;
        while (true) {
            newName = `${baseName} ${i}`;
            if (!existingProjects.some((p: any) => p.name === newName)) break;
            i++;
        }

        const homeDir = await callBackend(() => GetUserHomeDir());
        const newId = Math.random().toString(36).substr(2, 9);
        const newProject = {
            id: newId,
            name: newName,
            path: homeDir || "",
            yolo_mode: false
        };

        const newProjects = [...existingProjects, newProject];
        const newConfig = new corelib.AppConfig({ ...config, projects: newProjects });
        setConfig(newConfig);
        void callBackend(() => PatchConfigFields({ projects: newProjects })).then((saved) => setConfig(new corelib.AppConfig(saved))).catch((err) => console.error('Failed to save new project:', err));
        setStatus(t("saved"));
        setTimeout(() => setStatus(""), 1500);
    };

    const handleOpenSubscribe = (modelName: string) => {
        const url = subscriptionUrls[modelName];
        if (url) {
            safeBrowserOpenURL(url);
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

        // Only patch the tool configs + active_tool that the Model Settings panel edits.
        // Using PatchConfigFields (atomic load→merge→save under configMu) instead of
        // Full-overwrite save eliminates the TOCTOU race where a stale frontend
        // snapshot overwrites backend-owned fields (credentials, LLM provider, onboarding).
        const patch: Record<string, any> = { active_tool: configCopy.active_tool };
        TOOL_NAMES.forEach(tool => {
            if (configCopy[tool]) {
                patch[tool] = configCopy[tool];
            }
        });

        setConfig(new corelib.AppConfig(configCopy));
        setStatus(t("saving"));
        callBackend(() => PatchConfigFields(patch)).then((saved) => {
            setConfig(new corelib.AppConfig(saved));
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

    // Hooks must run on every render before any early return (isLoading / !config).
    // Placing these after the loading gate caused: "Rendered more hooks than during
    // the previous render" → full black screen when env-check finished.
    const settingsTabOptions = useMemo(
        () => getSettingsTabOptions(lang, { hideVirtualEmployee: !veNavigationAvailable }),
        [lang, veNavigationAvailable],
    );
    const resolvedSettingsTab = useMemo(
        () => resolveSettingsTabId(settingsTab, { hideVirtualEmployee: !veNavigationAvailable }),
        [settingsTab, veNavigationAvailable],
    );

    if (isLoading) {
        logStartupTrace('render-gate-isLoading', { envLogsCount: envLogs.length, isManualCheck });
        return (
            <div data-ai-theme={aiThemeMode} data-ai-dark-scheme={aiThemeMode === 'dark' ? aiDarkSchemeId : undefined} data-ai-light-scheme={aiThemeMode === 'light' && aiLightSchemeId !== 'default' ? aiLightSchemeId : undefined} data-native-rounded={nativeRounded ? "true" : undefined} data-css-window-corners={useCSSWindowCorners ? "true" : "false"} className="app-loading-shell">
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
                                if (loadingShellCtlRef.current) {
                                    // A manual check can be dismissed before its completion
                                    // event. Still restore the main geometry first so an
                                    // onboarding portal can never inherit the compact shell.
                                    loadingShellCtlRef.current.complete('user-dismiss');
                                } else {
                                    setIsLoading(false);
                                    setIsManualCheck(false);
                                }
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

    if (!config) {
        logStartupTrace('render-gate-no-config');
        return (
            <div
                className="main-content app-config-loading"
                data-ai-theme={aiThemeMode}
                role="status"
                aria-live="polite"
            >
                {t("loadingConfig")}
            </div>
        );
    }

    // Shared props for the shell status cluster (AI inline quick-settings row +
    // tool-page full-width strip). Keep one object so the two call sites cannot drift.
    const appStatusMessageBarProps = {
        status,
        lang,
        config,
        qqBotStatus,
        telegramStatus,
        weixinStatus,
        lansengerStatus,
        maclawLLMOnline,
        maclawLLMConfigured,
        remoteActivated: !!remoteActivationStatus?.activated,
        showLansenger: true as const,
        navTab,
        settingsTab: resolvedSettingsTab,
        backgroundInstallStatus,
        lobsterOffline,
        lobsterHalf,
        onOpenIMSettings: () => { setNavTabNow('settings'); selectSettingsTab('im'); },
        onOpenLLMSettings: () => { setNavTabNow('settings'); selectSettingsTab('llm'); },
        codingAgentProgress,
        isDark: aiThemeMode === 'dark',
    };

    const toolCfg = isToolTab(navTab)
        ? (config as any)[navTab]
        : null;

    const currentProject = getCurrentProject();
    const isRemoteCapableActiveTool = remoteToolMetadata.some(
        (meta) => meta.name === activeTool && meta.supports_remote === true
    );
    const launchMode = config?.default_launch_mode === 'remote' ? 'remote' : 'local';
    const launchRemoteEnabled = launchMode === 'remote';
    const setLaunchMode = async (mode: 'local' | 'remote') => {
        if (!config) return;
        const newConfig = new corelib.AppConfig({
            ...config,
            default_launch_mode: mode,
            remote_enabled: mode === 'remote',
        });
        setConfig(newConfig);
        try {
            await callBackend(() => SetDefaultLaunchMode(mode));
            const freshConfig = await callBackend(() => LoadConfigForUI());
            setConfig(freshConfig);
        } catch (err) {
            setStatus(localizeText("Error: ", "错误：", "錯誤：") + err);
            try {
                const freshConfig = await callBackend(() => LoadConfigForUI());
                setConfig(freshConfig);
            } catch {
                // Keep the optimistic UI state if recovery load fails.
            }
        }
    };
    const codexConfigUpdating = codexConfigUpdateCount > 0;
    const virtualEmployeeLayoutClassName = veSettingsAuthorized
        ? 'settings-ve-layout'
        : 'settings-ve-layout settings-ve-layout--favorites-only';
    return (
        <div
            className="app-viewport"
            data-ai-theme={aiThemeMode}
            data-ai-dark-scheme={aiThemeMode === 'dark' ? aiDarkSchemeId : undefined}
            data-ai-light-scheme={aiThemeMode === 'light' && aiLightSchemeId !== 'default' ? aiLightSchemeId : undefined}
            style={{ ['--ui-scale' as any]: String(uiZoom) } as React.CSSProperties}
        >
            <DataMigrationOverlay />
            <div className="app-scale-layer">
                <div id="App" data-ai-theme={aiThemeMode} data-ai-dark-scheme={aiThemeMode === 'dark' ? aiDarkSchemeId : undefined} data-ai-light-scheme={aiThemeMode === 'light' && aiLightSchemeId !== 'default' ? aiLightSchemeId : undefined} data-native-rounded={nativeRounded ? "true" : undefined} data-css-window-corners={useCSSWindowCorners ? "true" : "false"} data-maximized={windowMaximized ? "true" : undefined}>
            <AppSidebarShell
                navTab={navTab}
                taskManagementPaneWidth={taskManagementPaneWidth}
                aiThemeMode={aiThemeMode}
                aiDarkSchemeId={aiDarkSchemeId}
                aiLightSchemeId={aiLightSchemeId}
                brandInfo={brandInfo}
                currentIcon={currentIcon}
                brandSidebarName={brandSidebarName}
                switchTool={switchTool}
                lang={lang}
                maclawLLMOnline={maclawLLMOnline}
                showLansenger
                remoteActivationStatus={remoteActivationStatus}
                qqBotStatus={qqBotStatus}
                telegramStatus={telegramStatus}
                weixinStatus={weixinStatus}
                lansengerStatus={lansengerStatus}
                runningTaskCount={runningTaskCount}
                backgroundTaskCount={backgroundTaskCount}
                onOpenBackgroundTasks={openBackgroundTaskMonitor}
                t={t}
                gossipAllowed={gossipAllowed}
                config={config}
                activeTool={activeTool}
                toolDropdownOpen={toolDropdownOpen}
                setToolDropdownOpen={setToolDropdownOpen}
                tasks={taskItems}
                renamingTaskPath={renamingTaskPath}
                setRenamingTaskPath={setRenamingTaskPath}
                renameValue={renameValue}
                setRenameValue={setRenameValue}
                resumeTask={resumeTask}
                continueWorkflowProject={continueWorkflowProject}
                assistantReady={aiAssistant.ready}
                onTaskSwitchBlocked={() => showToastMessage(localizeText('System is warming up. Please switch later.', '系统正在预热，请稍后切换。', '系統正在預熱，請稍後切換。'))}
                createTask={createTask}
                refreshTasks={refreshTasks}
                taskContextMenu={taskContextMenu}
                setTaskContextMenu={setTaskContextMenu}
                renameTask={RenameTask}
                pinTask={PinTask}
                hideTask={hideTaskGuarded}
                openProjectTabPaths={openProjectTabPaths}
                openExpertTabIDs={openExpertTabIDs}
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
                openServiceRedeemPage={openServiceRedeemPage}
                openLLMSettingsPage={openLLMSettingsPage}
                openHubCardStorePage={openHubCardStorePage}
                codingAgentProgress={codingAgentProgress}
                codingAgentTurnSnapshot={codingAgentTurnSnapshot}
                handleTaskManagementResizeStart={handleTaskManagementResizeStart}
                isTaskManagementResizing={isTaskManagementResizing}
                onOpenVEConversation={(ve) => { switchTool('ai'); setPendingVEOpen(ve); }}
                onOpenHistoryDiscussion={(discussion) => { switchTool('ai'); setPendingHistoryDiscussionOpen(discussion); }}
                favoriteEmployees={favoriteEmployeeSlots}
                veAuthorized={veAuthorized}
                digitalEmployeeFeatureStatus={digitalEmployeeFeatureStatus}
                showDigitalEmployeeNavigation={veNavigationAvailable}
                onStartVEConversation={handleStartFavoriteVEConversation}
                onReorderFavorites={handleReorderFavorites}
                onRenameFavoriteEmployee={handleRenameFavoriteEmployee}
                onSetFavoriteEmployee={handleSetFavoriteEmployee}
                onRemoveFavoriteEmployeeById={handleRemoveFavoriteEmployee}
                onRemoveFavoriteEmployee={(ve) => handleRemoveFavoriteEmployee(ve.id)}
                favoriteEmployeeIds={userFavoriteEmployeeIds}
                favoriteEmployeeNames={favoriteEmployeeNames}
                showAppEntry={showAppEntryEnabled}
                showWorkflowEntry={showWorkflowEntryEnabled}
				showUtilitiesEntry={showUtilitiesEntryEnabled}
                showCodingToolEntry={!!(config as any)?.show_coding_tool_entry}
                availableProviders={availableProvidersForSwitch}
                onSwitchProvider={handleQuickSwitchProvider}
                currentModel={sidebarCurrentModel}
                modelOptions={sidebarModelOptions}
                modelsLoading={sidebarModelsLoading}
                onSwitchModel={handleQuickSwitchModel}
                onOpenModelMenu={refreshSidebarModelOptions}
                moaSticky={moaSession}
                onToggleMoASticky={handleToggleMoASticky}
            />
            <div className="main-container" data-ai-theme={aiThemeMode} data-ai-dark-scheme={aiThemeMode === 'dark' ? aiDarkSchemeId : undefined} data-ai-light-scheme={aiThemeMode === 'light' && aiLightSchemeId !== 'default' ? aiLightSchemeId : undefined}>
                {/* AI and non-AI panes use separate Suspense roots so a page chunk load
                    never blanks the shared header/status chrome (fallback={null} used to). */}
                {/* Keep the assistant mounted while another page is open. Project tabs own
                    live streams, drafts and restore work; unmounting this tree on a brief
                    trip to System/Background Tasks re-runs tab restoration and can replace
                    the active project view with the empty welcome screen. */}
                <div className="ai-main-panel-host" hidden={navTab !== 'ai'}>
                    <Suspense fallback={
                        <div className="app-main-content-loading" role="status" aria-live="polite">
                            {lang === 'zh-Hans' ? '加载助手…' : lang === 'zh-Hant' ? '載入助手…' : 'Loading assistant…'}
                        </div>
                    }>
                    <div className="ai-main-panel-shell">
                    <AIAssistantPanel
                            onClose={() => { switchTool('settings'); }}
                            lang={lang}
                            chatFontSize={chatFontSize}
                            themeMode={aiThemeMode}
                            darkSchemeId={aiDarkSchemeId}
                            lightSchemeId={aiLightSchemeId}
                            onThemeModeChange={setAIThemeMode}
                            audioInputDeviceId={(config as any)?.audio_input_device_id || ''}
                            audioOutputDeviceId={(config as any)?.audio_output_device_id || ''}
                            pendingVEOpen={pendingVEOpen}
                            onPendingVEOpenHandled={() => setPendingVEOpen(null)}
                            pendingHistoryDiscussionOpen={pendingHistoryDiscussionOpen}
                            onPendingHistoryDiscussionOpenHandled={() => setPendingHistoryDiscussionOpen(null)}
                            pendingProjectTabOpen={pendingProjectTabOpen}
                            onPendingProjectTabOpenHandled={() => setPendingProjectTabOpen(null)}
                            pendingExpertOpen={pendingExpertOpen}
                            onPendingExpertOpenHandled={() => setPendingExpertOpen(null)}
                            onEnsureExpertTask={ensureExpertTask}
                            onOpenProjectTabsChange={handleOpenProjectTabsChange}
                            onOpenExpertTabsChange={handleOpenExpertTabsChange}
                            appUpdateAvailable={appUpdateAvailable}
                            onOpenAppUpdate={handleOpenAppUpdate}
                            onDismissAppUpdate={handleDismissAppUpdate}
                            availableProviders={availableProvidersForSwitch}
                            onSwitchProvider={handleQuickSwitchProvider}
                            currentModel={sidebarCurrentModel}
                            modelOptions={sidebarModelOptions}
                            modelsLoading={sidebarModelsLoading}
                            onSwitchModel={handleQuickSwitchModel}
                            onOpenModelMenu={refreshSidebarModelOptions}
                            onLanguageChange={applyLanguage}
                            statusSlot={(
                                <AppStatusMessageBar
                                    variant="inline"
                                    {...appStatusMessageBarProps}
                                />
                            )}
                            state={{
                                ...aiAssistant.panelState,
                                selectedFilePath: aiAssistant.selectedFilePaths?.[0] ?? "",
                                onboardingIncomplete: !config?.onboarding_done && !showMaclawLLMPopup,
                                showTraceEntry: !!config?.show_ai_trace_entry,
                                active: navTab === 'ai',
                            }}
                            actions={{
                                ...aiAssistant.panelActions,
                                onOpenOnboarding: requestOnboarding,
                                onTaskPrefsChanged: () => { void refreshSessionsOnly(); },
                            }}
                            window={{
                                inline: true,
                                maximized: aiPanelMaximized,
                                onToggleMaximize: handleAIPanelMaximizeToggle,
                                onHideWindow: () => { void callBackend(() => WindowHide()); },
                            }}
                    />
                    </div>
                    </Suspense>
                </div>
                {navTab !== 'ai' && <MainTopHeader
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
                />}

                {navTab !== 'ai' && navTab !== 'utilities' && <div className="main-content elegant-scrollbar app-main-content" data-nav-tab={navTab}>
                {/* Settings is outside page Suspense. General panels are eager so the default
                    open path never depends on a lazy chunk (OEM intermittent blank fix). */}
                {navTab === 'settings' ? (
                        <SettingsPage
                            tabs={settingsTabOptions}
                            activeTab={resolvedSettingsTab}
                            onChangeTab={selectSettingsTab}
                            lang={lang}
                            t={t}
                            localizeText={localizeText}
                            config={config}
                            setConfig={setConfig}
                            onLanguageChange={handleLangChange}
                            hasWindowsTerminal={hasWindowsTerminal}
                            envCheckInterval={envCheckInterval}
                            setEnvCheckInterval={setEnvCheckInterval}
                            isWindows={isWindows}
                            patchConfigFields={patchConfigFieldsForSettings}
                            onLLMStatusChange={handleSettingsLLMStatusChange}
                            onProviderChanged={handleLLMProviderChanged}
                            showToastMessage={showToastMessage}
                            memoryTraceFocus={memoryTraceFocus}
                            imSubTab={imSubTab}
                            setImSubTab={setImSubTab}
                            imAuditPlatform={imAuditPlatform}
                            setIMAuditPlatform={setIMAuditPlatform}
                            saveRemoteConfigField={saveRemoteConfigField}
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
                            showLansenger
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
                            veNavigationAvailable={veNavigationAvailable}
                            veSettingsAuthorized={veSettingsAuthorized}
                            virtualEmployeeLayoutClassName={virtualEmployeeLayoutClassName}
                            userFavoriteEmployeeIds={userFavoriteEmployeeIds}
                            veList={veList}
                            onAddFavoriteEmployee={handleAddFavoriteEmployeeFromSettings}
                            onRemoveFavoriteEmployee={handleRemoveFavoriteEmployee}
                            onReorderFavorites={handleReorderFavorites}
                            audioDevices={audioDevices}
                            uiZoom={uiZoom}
                            setUiZoom={setUiZoom}
                            uiZoomAuto={uiZoomAuto}
                            setUiZoomAuto={setUiZoomAuto}
                            disableAutoUIScaleTransform={disableAutoUIScaleTransform}
                            chatFontSize={chatFontSize}
                            setChatFontSize={setChatFontSize}
                            darkSchemeId={aiDarkSchemeId}
                            setDarkSchemeId={handleAIDarkSchemeChange}
                            lightSchemeId={aiLightSchemeId}
                            setLightSchemeId={handleAILightSchemeChange}
                        />
                ) : (
                <Suspense
                    fallback={
                        <div className="app-main-content-loading" role="status" aria-live="polite">
                            {localizeText('Loading…', '\u52a0\u8f7d\u4e2d\u2026', '\u8f09\u5165\u4e2d\u2026')}
                        </div>
                    }
                >
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
                            initialSessionTab={remoteInitialSessionTab}
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

                    {showAppEntryEnabled && (
                        <div style={{ display: navTab === 'apps' ? undefined : 'none', height: '100%', minHeight: 0 }} hidden={navTab !== 'apps'}>
                            <Suspense fallback={null}>
                                <AppsPage lang={lang} onOpenMISDataSettings={openMISDataSettings} onOpenManual={openMaclawAppManualPage} />
                            </Suspense>
                        </div>
                    )}

                    {navTab === 'workflows' && (
                        <WorkflowsPage lang={lang} switchToAI={() => setNavTabNow('ai')} />
                    )}

                    {navTab === 'skills' && (
                        <SkillsPage localizeText={localizeText} />
                    )}

                    {navTab === 'mcp' && (
                        <MCPPage translate={translate} />
                    )}

                    {navTab === 'about' && (
                        <AboutPanel
                            currentIcon={currentIcon}
                            brandInfo={brandInfo}
                            appVersion={APP_VERSION}
                            buildNumber={buildNumber}
                            thanksContent={thanksContent}
                            config={config}
                            t={t}
                            onOpenWebsite={() => safeBrowserOpenURL(brandInfo?.websiteURL || "https://maclaw.top")}
                            onCheckUpdate={() => {
                                setStatus(t("checkingUpdate"));
                                const preferBeta = Boolean(config?.prefer_beta_channel);
                                checkAppUpdate(APP_VERSION, preferBeta).then((res: any) => {
                                    console.log(preferBeta ? "CheckUpdateBeta result:" : "CheckUpdate result:", res);
                                    setUpdateResult(res);
                                    setShowUpdateModal(true);
                                    setStatus("");
                                }).catch((err: any) => {
                                    console.error(preferBeta ? "CheckUpdateBeta error:" : "CheckUpdate error:", err);
                                    setStatus((lang === 'zh-Hans' ? '检查更新失败：' : lang === 'zh-Hant' ? '檢查更新失敗：' : 'Update check failed: ') + err);
                                    setUpdateResult({
                                        has_update: false,
                                        check_failed: true,
                                        message: lang === 'zh-Hans' ? '无法检查更新，请检查网络后重试。' : lang === 'zh-Hant' ? '無法檢查更新，請檢查網路後重試。' : 'Unable to check for updates. Check your connection and try again.',
                                        release_url: ""
                                    });
                                    setShowUpdateModal(true);
                                });
                            }}
                            onShowInstallLog={() => setShowInstallLog(true)}
                            onOpenProblemReport={() => {
                                problemReportReturnFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
                                setProblemMessage('');
                                setShowProblemReport(true);
                                if (!problemOS) GetSystemInfo()
                                    .then((info: any) => {
                                        const detectedOS = [info?.os, info?.os_version].filter(Boolean).join(' ') || navigator.userAgent;
                                        setProblemOS(current => current || detectedOS);
                                    })
                                    .catch(() => setProblemOS(current => current || navigator.userAgent));
                                ListMyBugReports().then((result: any) => setMyProblemReports(Array.isArray(result?.items) ? result.items : [])).catch(() => setMyProblemReports([]));
                                HasPendingBugReportUpload().then(setProblemRetryAvailable).catch(() => setProblemRetryAvailable(false));
                            }}
                            onOpenBugReport={() => safeBrowserOpenURL("https://github.com/rapidai/maclaw/issues/new")}
                            onOpenGithub={() => BrowserOpenURL(MACLAW_CODE_REPOSITORY_URL)}
                            onRegister={requestOnboarding}
                            onClearRegistration={async () => {
                                await clearRemoteActivationState();
                                // clearRemoteActivationState already refreshes config + activation status
                                // via refreshRemotePanel(). Additionally clear onboarding_done flag
                                // so the wizard can be re-triggered on next "Register" click.
                                try {
                                    const saved = await PatchConfigFields({ onboarding_done: false });
                                    setConfig(new corelib.AppConfig(saved));
                                } catch (e) {
                                    console.error("Failed to clear onboarding_done:", e);
                                }
                            }}
                            onRegistrationContactUpdated={() => {
                                void callBackend(() => LoadConfigForUI()).then((freshConfig) => setConfig(new corelib.AppConfig(freshConfig))).catch((err) => console.error("Failed to refresh registration contact:", err));
                            }}
                        />
                    )}
                </Suspense>
                )}
                </div>}
				{showUtilitiesEntryEnabled && (navTab === 'utilities' || utilitiesPageVisited) && (
                    <div className="main-content elegant-scrollbar app-main-content" data-nav-tab="utilities" hidden={navTab !== 'utilities'}>
                        <Suspense fallback={
                            <div className="app-main-content-loading" role="status" aria-live="polite">
                                {localizeText('Loading…', '加载中…', '載入中…')}
                            </div>
                        }>
                            <UtilitiesPage lang={lang} active={navTab === 'utilities'} onStartMeetingRecord={startMeetingRecord} onOpenExpert={(expert) => {
                                switchTool('ai');
                                setPendingExpertOpen({ expert });
                            }} onOpenVirtualRepositoryTask={(launch) => {
                                openCodingTask({ projectPath: launch.project_path, taskTitle: launch.task_title, prepareMode: 'new-agent', autoSend: false, agentMode: launch.agent_mode, remoteHost: launch.remote_host, remoteNeedsReconnect: false });
                            }} />
                        </Suspense>
                    </div>
                )}

                {/* Global Action Bar (Footer) */}
                {config && isToolTab(navTab) && (
                    <div className="global-action-bar" data-ai-theme={aiThemeMode} data-ai-dark-scheme={aiThemeMode === 'dark' ? aiDarkSchemeId : undefined} data-ai-light-scheme={aiThemeMode === 'light' && aiLightSchemeId !== 'default' ? aiLightSchemeId : undefined}>
                        <div className="coding-launch-panel wails-no-drag">
                            <div className="coding-launch-meta-row">
                                <div className="coding-launch-summary">
                                    {/* runnerStatus label removed */}
                                    <span className="coding-launch-tool-name">{getToolLabel(activeTool)}</span>
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
                                <label className="coding-launch-check">
                                    <input
                                        type="checkbox"
                                        checked={launchPanelProject?.admin_mode || false}
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
                                <label className="coding-launch-check">
                                    <input
                                        type="checkbox"
                                        checked={launchPanelProject?.python_project || false}
                                        onChange={(e) => updateResolvedLaunchProject((project) => ({ ...project, python_project: e.target.checked }))}
                                    />
                                    <span>{t("pythonProjectLabel")}</span>
                                </label>
                                {launchPanelProject?.python_project && (
                                    <div className="coding-launch-python-env">
                                        <span className="coding-launch-python-label">{t("pythonEnvLabel")}:</span>
                                        <select
                                            value={launchPanelProject?.python_env || ""}
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
                                {activeTool === 'claude' && (
                                    <label className="coding-launch-check">
                                        <input
                                            type="checkbox"
                                            checked={launchPanelProject?.team_mode || false}
                                            onChange={(e) => updateResolvedLaunchProject((project) => ({ ...project, team_mode: e.target.checked }))}
                                        />
                                        <span>{t("teamModeLabel")}</span>
                                    </label>
                                )}
                                {activeTool !== 'kilo' && (
                                    <label className="coding-launch-check">
                                        <input
                                            type="checkbox"
                                            checked={launchPanelProject?.yolo_mode || false}
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
                                        {launchPanelProject?.yolo_mode && (
                                            <span className="coding-launch-danger-badge">
                                                {t("danger")}
                                            </span>
                                        )}
                                    </label>
                                )}
                                {!isWindows && (
                                    <label className="coding-launch-check">
                                        <input
                                            type="checkbox"
                                            checked={launchPanelProject?.use_proxy || false}
                                            onChange={(e) => {
                                                if (e.target.checked && !launchPanelProject?.proxy_host && !config?.default_proxy_host) {
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
                                            value={launchProjectSelectValue}
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
                                            type="button"
                                            onClick={() => switchTool('projects')}
                                            className="coding-launch-manage"
                                            title={t("projectManagement")}
                                            aria-label={t("projectManagement")}
                                        >
                                            ...
                                        </button>
                                    </div>
                                </div>
                                <button
                                    className="btn-launch coding-launch-button"
                                    disabled={onDemandInstallingTool === activeTool || backgroundInstallingTool === activeTool || launchingTool === activeTool || (!hasActiveRemoteSessionForTool && !hasSelectableLaunchProject)}
                                    aria-busy={launchingTool === activeTool || onDemandInstallingTool === activeTool || backgroundInstallingTool === activeTool}
                                    onClick={async () => {
                                        console.log("Launch button clicked. activeTool:", activeTool);
                                        if (launchRemoteEnabled && hasActiveRemoteSessionForTool && activeRemoteSessionForTool?.id) {
                                            setLaunchingTool(activeTool);
                                            await killRemoteSession(activeRemoteSessionForTool.id);
                                            setStatus(localizeText('Remote stopped', '远程已停止', '遠端已停止'));
                                            setTimeout(() => { setStatus(""); setLaunchingTool(""); }, 2000);
                                            return;
                                        }
                                        const selectedProj = activeLaunchProject;
                                        if (selectedProj && selectedProj.path && selectedProj.path.trim() !== "") {
                                            if (launchRemoteEnabled) {
                                                if (remoteToolMetadata.length > 0 && !isRemoteCapableActiveTool) {
                                                    setStatus(localizeText("This tool does not support remote launch", "此工具不支持远程启动", "此工具不支持遠端啟動"));
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
                                                        `${activeTool} 正在后台安装，请稍候…`,
                                                        `${activeTool} 正在背景安裝，請稍候…`,
                                                    ));
                                                    setOnDemandInstallingTool(activeTool);
                                                    try {
                                                        await callBackend(() => InstallToolOnDemand(activeTool));
                                                        // Refresh tool statuses
                                                        const updatedStatuses = await callBackend(() => CheckToolsStatus());
                                                        setToolStatuses(updatedStatuses);
                                                        setStatus(localizeText(`${activeTool} installed`, `${activeTool} 安装成功`, `${activeTool} 安裝成功`));
                                                        setOnDemandInstallingTool("");
                                                        // Auto launch
                                                        setTimeout(async () => {
                                                            setStatus(localizeText("Launching...", "启动中...", "啟動中..."));
                                                            setLaunchingTool(activeTool);
                                                            try {
                                                                await callBackend(() => LaunchTool(activeTool, selectedProj.yolo_mode, selectedProj.admin_mode || false, selectedProj.python_project || false, selectedProj.python_env || "", selectedProj.path || "", selectedProj.use_proxy || false));
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
                                                    await callBackend(() => InstallToolOnDemand(activeTool));
                                                    // Refresh tool statuses
                                                    const updatedStatuses = await callBackend(() => CheckToolsStatus());
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
                                                            await callBackend(() => LaunchTool(activeTool, selectedProj.yolo_mode, selectedProj.admin_mode || false, selectedProj.python_project || false, selectedProj.python_env || "", selectedProj.path || "", selectedProj.use_proxy || false));
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
                                            callBackend(() => LaunchTool(activeTool, selectedProj.yolo_mode, selectedProj.admin_mode || false, selectedProj.python_project || false, selectedProj.python_env || "", selectedProj.path || "", selectedProj.use_proxy || false))
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
                                        ? (launchingTool === activeTool ? t("launchStarting") : (hasActiveRemoteSessionForTool ? t("remoteStopTool") : t("remoteStartTool")))
                                        : (launchingTool === activeTool ? t("launchStarting") : (onDemandInstallingTool === activeTool || backgroundInstallingTool === activeTool ? t("installing") : t("launch")))}
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

                {/* AI tab hosts this bar inline in the quick-settings row; keep a
                    full-width strip only on tool / settings / utilities pages. */}
                {navTab !== 'ai' && (
                    <AppStatusMessageBar {...appStatusMessageBarProps} />
                )}
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

            {showProblemReport && (
                <div className="modal-overlay" role="presentation" onMouseDown={(event) => {
                    if (event.target === event.currentTarget && !problemBusy) setShowProblemReport(false);
                }}>
                    <div ref={problemReportDialogRef} className="modal-content problem-report-dialog" role="dialog" aria-modal="true" aria-busy={problemBusy} aria-labelledby="problem-report-title" aria-describedby="problem-report-description">
                        <div className="about-contact-dialog__header problem-report-dialog__header">
                            <h3 id="problem-report-title">{t('problemReport')}</h3>
                            <button className="modal-close" disabled={problemBusy} onClick={() => setShowProblemReport(false)} aria-label={t('close')}>&times;</button>
                        </div>
                        <p className="about-contact-dialog__desc problem-report-dialog__intro" id="problem-report-description">{t('problemReportDesc')}</p>
                        <div className="setting-row problem-report-dialog__collection">
                            <div className="problem-report-dialog__collection-copy"><strong>{t('problemReportEnable')}</strong><div className="setting-desc">{t('problemReportEnableDesc')}</div></div>
                            <label className="switch"><input type="checkbox" checked={Boolean(config?.bug_report_enabled)} disabled={problemBusy} onChange={async (event) => {
                                const requested = event.target.checked;
                                setProblemBusy(true);
                                setProblemMessage('');
                                try {
                                    const saved = await SetBugReportEnabled(requested);
                                    setConfig(new corelib.AppConfig(saved));
                                } catch (err: any) {
                                    // This controlled input remains bound to the last persisted
                                    // config snapshot, so a failed native write naturally rolls
                                    // back without mutating the DOM behind React's back.
                                    setProblemMessage(String(err?.message || err));
                                } finally {
                                    setProblemBusy(false);
                                }
                            }} /><span className="slider"></span></label>
                        </div>
                        <div className="problem-report-dialog__version" aria-label="MaClaw GUI version"><span>MaClaw GUI</span><code>{APP_VERSION}</code></div>
                        <label className="problem-report-dialog__field"><span className="form-label">{t('problemReportOS')} <em>*</em></span><input className="form-input" value={problemOS} onChange={event => setProblemOS(event.target.value)} /></label>
                        <label className="problem-report-dialog__field"><span className="form-label">{t('problemReportDescription')} <em>*</em></span><textarea ref={problemDescriptionRef} className="form-input" rows={5} value={problemDescription} onChange={event => setProblemDescription(event.target.value)} placeholder={t('problemReportDescriptionPlaceholder')} /></label>
                        <div
                            className="problem-report-dialog__field problem-report-dialog__attachments-editor"
                            onPaste={async (event) => {
                                if (problemBusy || problemScreenshots.length >= 12) return;
                                const images = Array.from(event.clipboardData?.items || [])
                                    .filter(item => item.kind === 'file' && item.type.startsWith('image/'))
                                    .map(item => item.getAsFile())
                                    .filter((file): file is File => file !== null);
                                if (!images.length) return;
                                event.preventDefault();
                                try {
                                    const slots = Math.max(0, 12 - problemScreenshots.length);
                                    const accepted = images.slice(0, slots);
                                    const oversized = accepted.find(file => file.size > BUG_REPORT_SCREENSHOT_MAX_BYTES);
                                    if (oversized) throw new Error(`Screenshot ${oversized.name || ''} exceeds the 10 MB limit`);
                                    const savePastedImage = (window as any).go?.main?.App?.SavePastedImage;
                                    if (typeof savePastedImage !== 'function') throw new Error('Pasted image support is unavailable');
                                    const paths = await Promise.all(accepted.map(async file => {
                                        const base64 = await readClipboardImageBase64(file);
                                        return savePastedImage(base64, clipboardImageExtension(file.type));
                                    }));
                                    setProblemScreenshots(prev => appendProblemReportScreenshotPaths(prev, paths));
                                } catch (err: any) {
                                    setProblemMessage(String(err?.message || err));
                                }
                            }}
                        >
                            <div className="problem-report-dialog__attachment-head">
                                <span className="form-label">{t('problemReportScreenshots')}</span>
                                <button type="button" className="btn-link about-action-button" disabled={problemBusy || problemScreenshots.length >= 12} onClick={async () => {
                                    try {
                                        // Wails may resolve to null when the native file dialog is cancelled.
                                        // Treat cancellation exactly like selecting no files so the report dialog remains mounted.
                                        const result = await SelectBugReportScreenshots();
                                        const files = Array.isArray(result) ? result : [];
                                        if (!files.length) return;
                                        setProblemScreenshots(prev => appendProblemReportScreenshotPaths(prev, files));
                                    } catch (err: any) {
                                        setProblemMessage(String(err?.message || err));
                                    }
                                }}>{t('problemReportAddScreenshots')}</button>
                            </div>
                            {problemScreenshots.length > 0 ? (
                                <>
                                    <ul className="problem-report-dialog__attachment-list" aria-label={t('problemReportScreenshots')}>
                                        {problemScreenshots.map((path, index) => {
                                            const name = path.split(/[\\/]/).pop() || path;
                                            const preview = problemScreenshotPreviews[path];
                                            return (
                                                <li key={path}>
                                                    {preview ? <img className="problem-report-dialog__attachment-preview" src={preview} alt={name} /> : <span className="problem-report-dialog__attachment-placeholder" aria-hidden="true">IMG</span>}
                                                    <span className="problem-report-dialog__attachment-name" title={path}>{name}</span>
                                                    <button
                                                        type="button"
                                                        className="btn-link problem-report-dialog__attachment-remove"
                                                        aria-label={t('problemReportRemoveScreenshot').replace('{name}', name)}
                                                        title={t('problemReportRemoveScreenshot').replace('{name}', name)}
                                                        disabled={problemBusy}
                                                        onClick={() => {
                                                            setProblemScreenshots(prev => prev.filter((_, itemIndex) => itemIndex !== index));
                                                            setProblemScreenshotPreviews(prev => {
                                                                if (!(path in prev)) return prev;
                                                                const next = { ...prev };
                                                                delete next[path];
                                                                return next;
                                                            });
                                                        }}
                                                    >
                                                        &times;
                                                    </button>
                                                </li>
                                            );
                                        })}
                                    </ul>
                                    <div className="setting-desc">{t('problemReportScreenshotCount').replace('{count}', String(problemScreenshots.length))}</div>
                                </>
                            ) : (
                                <div className="setting-desc">{t('problemReportNoScreenshots')} {t('problemReportPasteScreenshot')}</div>
                            )}
                        </div>
                        {myProblemReports.length > 0 && (
                            <section className="problem-report-dialog__history" aria-labelledby="problem-report-history-title">
                                <span className="form-label" id="problem-report-history-title">{t('myProblemReports')}</span>
                                <div className="problem-report-dialog__history-list">
                                    {myProblemReports.map(report => (
                                        <div className="problem-report-dialog__history-item" key={report.id}>
                                            <code title={String(report.id)}>{report.id}</code>
                                            <span>{problemReportStatusLabel(report.status)}</span>
                                        </div>
                                    ))}
                                </div>
                            </section>
                        )}
                        {problemMessage && <div className="about-contact-dialog__message problem-report-dialog__message" role="status">{problemMessage}</div>}
                        <div className="modal-actions problem-report-dialog__actions">
                            <button className="btn-hide" disabled={problemBusy} onClick={() => setShowProblemReport(false)}>
                                {t('close')}
                            </button>
                            {problemRetryAvailable && (
                                <button
                                    className="btn-link about-action-button"
                                    disabled={problemBusy || !problemOS.trim() || !problemDescription.trim()}
                                    onClick={async () => {
                                        setProblemBusy(true);
                                        setProblemMessage('');
                                        try {
                                            const report = await RetryBugReportUpload(problemOS, problemDescription, problemScreenshots);
                                            setProblemRetryAvailable(false);
                                            setProblemMessage(t('problemReportSubmitted').replace('{id}', String(report?.id || '')));
                                            setProblemDescription('');
                                            setProblemScreenshots([]);
                                            const mine = await ListMyBugReports();
                                            setMyProblemReports(Array.isArray((mine as any)?.items) ? (mine as any).items : []);
                                        } catch (err: any) {
                                            setProblemMessage(String(err?.message || err));
                                            setProblemRetryAvailable(await HasPendingBugReportUpload().catch(() => false));
                                        } finally {
                                            setProblemBusy(false);
                                        }
                                    }}
                                >
                                    {problemBusy ? t('loading') : t('problemReportRetry')}
                                </button>
                            )}
                            <button
                                className="btn-primary"
                                disabled={problemBusy || !problemOS.trim() || !problemDescription.trim()}
                                onClick={async () => {
                                    setProblemBusy(true);
                                    setProblemMessage('');
                                    try {
                                        const report = await SubmitBugReport(problemOS, problemDescription, problemScreenshots);
                                        setProblemRetryAvailable(false);
                                        setProblemMessage(t('problemReportSubmitted').replace('{id}', String(report?.id || '')));
                                        setProblemDescription('');
                                        setProblemScreenshots([]);
                                        const mine = await ListMyBugReports();
                                        setMyProblemReports(Array.isArray((mine as any)?.items) ? (mine as any).items : []);
                                    } catch (err: any) {
                                        setProblemMessage(String(err?.message || err));
                                        setProblemRetryAvailable(await HasPendingBugReportUpload().catch(() => false));
                                    } finally {
                                        setProblemBusy(false);
                                    }
                                }}
                            >
                                {problemBusy ? t('loading') : t('problemReportSubmit')}
                            </button>
                        </div>
                    </div>
                </div>
            )}
            {showUpdateModal && updateResult && (
                <UpdateModal
                    updateResult={updateResult}
                    appVersion={APP_VERSION}
                    isDownloading={isDownloading}
                    downloadProgress={downloadProgress}
                    activeDownloadSource={activeDownloadSource}
                    installerPath={installerPath}
                    downloadError={downloadError}
                    preferBetaChannel={config?.prefer_beta_channel}
                    t={t}
                    onCancelDownload={handleCancelDownload}
                    onDownload={handleDownload}
                    onInstall={handleInstall}
                    onUpdateResultChange={setUpdateResult}
                    onClose={() => {
                        setShowUpdateModal(false);
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
                                                } else if (["aicodemirror", "aigocode", "noin.ai", "gaccode", "coderelay"].some(p => name.includes(p))) {
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
                                                const currentModelId = currentModel.model_id || '';
                                                const normalizedQuery = currentModelId.trim().toLowerCase();
                                                const matchingModelOptions = normalizedQuery
                                                    ? modelOptions.filter((m) => {
                                                        const id = String(m.id || '').toLowerCase();
                                                        const name = String(m.name || '').toLowerCase();
                                                        return id.includes(normalizedQuery) || name.includes(normalizedQuery);
                                                    })
                                                    : modelOptions;
                                                const visibleModelOptions = matchingModelOptions.length > 0 ? matchingModelOptions : modelOptions;
                                                const optionsId = `provider-config-model-options-${activeTool}-${activeTab}`;
                                                return (
                                                    <div
                                                        className="provider-config-model-combobox"
                                                        onBlur={(e) => {
                                                            const nextFocus = e.relatedTarget as Node | null;
                                                            if (!nextFocus || !e.currentTarget.contains(nextFocus)) {
                                                                setModelListOpen(false);
                                                            }
                                                        }}
                                                    >
                                                        <input
                                                            type="text"
                                                            className="form-input provider-config-model-select"
                                                            data-field="model-id"
                                                            role="combobox"
                                                            aria-autocomplete="list"
                                                            aria-haspopup="listbox"
                                                            aria-expanded={modelListOpen && modelOptions.length > 0}
                                                            aria-controls={optionsId}
                                                            value={currentModelId}
                                                            onChange={(e) => {
                                                                handleModelIdChange(e.target.value);
                                                                if (modelOptions.length > 0) setModelListOpen(true);
                                                            }}
                                                            onFocus={() => {
                                                                if (modelOptions.length > 0) setModelListOpen(true);
                                                            }}
                                                            onKeyDown={(e) => {
                                                                if (e.key === 'Escape') setModelListOpen(false);
                                                                if (e.key === 'ArrowDown' && modelOptions.length > 0) {
                                                                    setModelListOpen(true);
                                                                    window.setTimeout(() => {
                                                                        document.getElementById(optionsId)?.querySelector<HTMLButtonElement>('.provider-config-model-option')?.focus();
                                                                    }, 0);
                                                                }
                                                                if (e.key === 'Enter' && modelOptions.length > 0) {
                                                                    setModelListOpen(true);
                                                                }
                                                            }}
                                                            placeholder={fetchingModelList
                                                                ? localizeText("Loading...", "加载中...", "載入中...")
                                                                : modelOptions.length > 0
                                                                    ? localizeText("Select or type model name", "选择或输入模型名称", "選擇或輸入模型名稱")
                                                                    : localizeText("Type model name or click Models", "输入模型名称或点击《模型》获取", "輸入模型名稱或點擊《模型》獲取")}
                                                            disabled={fetchingModelList}
                                                            autoCapitalize="off"
                                                            autoCorrect="off"
                                                            spellCheck={false}
                                                            autoComplete="off"
                                                        />
                                                        <button
                                                            type="button"
                                                            className="provider-config-model-toggle"
                                                            aria-label={localizeText("Show model list", "显示模型列表", "顯示模型列表")}
                                                            aria-haspopup="listbox"
                                                            aria-expanded={modelListOpen && modelOptions.length > 0}
                                                            disabled={fetchingModelList || modelOptions.length === 0}
                                                            onClick={() => setModelListOpen((open) => modelOptions.length > 0 ? !open : false)}
                                                        >
                                                            v
                                                        </button>
                                                        {modelListOpen && modelOptions.length > 0 && (
                                                            <div
                                                                id={optionsId}
                                                                className="provider-config-model-options"
                                                                role="listbox"
                                                            >
                                                                {visibleModelOptions.map((m, i) => (
                                                                    <button
                                                                        key={`${m.id}-${i}`}
                                                                        type="button"
                                                                        className="provider-config-model-option"
                                                                        role="option"
                                                                        aria-selected={m.id === currentModelId}
                                                                        onMouseDown={(e) => e.preventDefault()}
                                                                        onClick={() => {
                                                                            handleModelIdChange(m.id);
                                                                            setModelListOpen(false);
                                                                        }}
                                                                    >
                                                                        <span className="provider-config-model-option-id">{m.id}</span>
                                                                        {m.name && m.name !== m.id && (
                                                                            <span className="provider-config-model-option-name">{m.name}</span>
                                                                        )}
                                                                    </button>
                                                                ))}
                                                            </div>
                                                        )}
                                                    </div>
                                                );
                                                })()}
                                            <button
                                                type="button"
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
                                                    const protocol = inferProviderModelFetchProtocol(activeTool, currentModel.model_url);
                                                    setFetchingModelList(true);
                                                    setFetchedModelList([]);
                                                    setModelListOpen(false);
                                                    try {
                                                        const userAgent = currentModel.agent_type || currentModel.user_agent || 'openclaw';
                                                        const models = await FetchProviderModels(url, key, protocol, userAgent);
                                                        if (models && models.length > 0) {
                                                            setFetchedModelList(models.map((m: any) => ({ id: m.id || '', name: m.name || '' })));
                                                            setModelListOpen(true);
                                                        } else {
                                                            setModelListOpen(false);
                                                            setStatus(localizeText("Provider returned empty model list", "服务商返回的模型列表为空", "服務商返回的模型列表為空"));
                                                        }
                                                    } catch (e) {
                                                        setModelListOpen(false);
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
                                                const newConfig = new corelib.AppConfig({ ...config, [activeTool]: toolCfg });
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
                                            const newConfig = new corelib.AppConfig({ ...config, [activeTool]: toolCfg });
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
                            callBackend(() => LoadConfigForUI()).then((c: any) => setConfig(c)).catch((err) => {
                                console.error("Failed to reload config after LLM configured:", err);
                                // Retry once after a short delay
                                setTimeout(() => {
                                    callBackend(() => LoadConfigForUI()).then((c: any) => setConfig(c)).catch((err2) => {
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
                    onMigrationCompleted={async () => {
                        try {
                            const restored = await callBackend(() => LoadConfigForUI());
                            setConfig(new corelib.AppConfig(restored));
                        } catch (error) {
                            console.warn('[onboarding] failed to refresh restored config snapshot:', error);
                        }
                        try {
                            await refreshRemotePanel();
                        } catch (error) {
                            console.warn('[onboarding] failed to refresh remote panel after migration:', error);
                        }
                    }}
                    onOnboardingCompleted={async () => {
                        const saved = await callBackend(() => PatchConfigFields({ onboarding_done: true }));
                        setConfig(new corelib.AppConfig(saved));
                    }}
                    onSaveField={(patch) => {
                        return saveRemoteConfigField(patch as any);
                    }}
                />
            )}

            {hubAuthRejectedPrompt && (
                <ConfirmDialog
                    title={localizeText('Hub Binding Expired', 'Hub 绑定已失效', 'Hub 綁定已失效')}
                    message={localizeText(
                        'Your Hub account has been unbound by the administrator. Would you like to re-register?',
                        '管理员已解除您的 Hub 绑定，是否重新注册？',
                        '管理員已解除您的 Hub 綁定，是否重新註冊？'
                    )}
                    t={(key) => key === 'cancel' ? localizeText('Later', '稍后', '稍後') : localizeText('Re-register', '重新注册', '重新註冊')}
                    onCancel={() => setHubAuthRejectedPrompt(false)}
                    onConfirm={() => { setHubAuthRejectedPrompt(false); requestOnboarding(); }}
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
                    currentSlots={favoriteEmployeeReplaceSlots}
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
