import { useEffect, useLayoutEffect, useMemo, useState, type CSSProperties, type MouseEvent as ReactMouseEvent, type PointerEvent as ReactPointerEvent } from 'react';
import type { SidebarCreditDisplayFormatters, SidebarCurrentProviderTokenUsage, SidebarHubCredits } from '../../types/appShell';
import { EVENT_OPEN_CREATE_CODING_TASK } from '../../constants/events';
import type { CodingAgentProgress, CodingAgentTurnSnapshot } from '../ai/CodingAgentProgressStatus';
import { SidebarToolSelector } from './SidebarToolSelector';
import type { TaskManagementItem, TaskContextMenu } from './SidebarTaskManagement';
import { SidebarTasksPane, middleContentSlotStyle } from './SidebarTasksPane';
import type { ActiveAssistantTaskIdentity } from '../ai/aiAssistantPanelSessionUtils';
import type { ExpertDefinition } from '../ai/expertTypes';
import type { LatexExpertTaskOptions } from '../../utils/latexTemplates';
import { isLansengerChannelEnabled, SidebarSystemStatus } from './SidebarSystemStatus';
import { VirtualEmployeeTab, type VirtualEmployeeEntry } from '../ai/VirtualEmployeeTab';
import { getAssistantDarkScheme, type AssistantDarkSchemeId } from '../ai/assistantDarkSchemes';
import { DEFAULT_ASSISTANT_LIGHT_SCHEME_ID, getAssistantLightScheme, type AssistantLightSchemeId } from '../ai/assistantLightSchemes';
import { SidebarMiddleTabs } from './SidebarMiddleTabs';
import { SidebarHistorySessions, type HistoryDiscussionSummary } from './SidebarHistorySessions';
import { isDigitalEmployeeAuthorizationUsable, shouldShowDigitalEmployeeFeatureTabs } from '../ai/digitalEmployeeFeature';
import { acknowledgeRailMiddleFocus, overrideRailMiddleFocus, peekPendingRailMiddleFocus, requestRailMiddleFocus, usePendingRailMiddleFocus } from './railMiddleFocus';
import type { LLMProfileStatusSummary } from './SidebarSystemStatus';
import type { WorkbenchTaskCounts } from './backgroundTaskCount';
import { useBotAccessEnabled, useInProgressBotTaskCount } from './useInProgressBotTaskCount';
import { desktopBotAccountId } from '../bots/desktopBots';

export { isDigitalEmployeeAuthorizationUsable } from '../ai/digitalEmployeeFeature';

type MiddleTab = 'tasks' | 'employees' | 'history';

const middlePaneInsetPx = 6;

const middlePaneStyle: CSSProperties = {
    flex: 1,
    minHeight: 0,
    overflow: 'hidden',
    display: 'flex',
    flexDirection: 'column',
    padding: `0 ${middlePaneInsetPx}px`,
    boxSizing: 'border-box',
};

export function shouldShowDigitalEmployeeMiddleTabs(status: any, nowMs = Date.now()): boolean {
    return shouldShowDigitalEmployeeFeatureTabs(status, nowMs);
}

type SidebarAiPaneProps = SidebarCreditDisplayFormatters & {
    taskManagementPaneWidth: number;
    lang: string;
    aiThemeMode?: 'light' | 'dark';
    aiLightSchemeId?: AssistantLightSchemeId;
    aiDarkSchemeId?: AssistantDarkSchemeId;
    maclawLLMOnline: boolean;
    showLansenger?: boolean;
    remoteActivationStatus: any;
    qqBotStatus: string;
    telegramStatus: string;
    weixinStatus: string;
    lansengerStatus: string;
    backgroundTaskCount?: number;
    workbenchTaskCounts?: WorkbenchTaskCounts;
    onOpenBackgroundTasks?: () => void;
    /** Keep cloud workspace/project controls for external coding surfaces. */
    showCloudWorkspaceManagement?: boolean;
    /** Show cloud task creation while keeping project management controls hidden. */
    showCloudWorkspaceCreation?: boolean;
    /** Restore durable cloud task rows while keeping project controls hidden. */
    restoreCloudWorkspaceTasks?: boolean;
    config: any;
    activeTool: string;
    toolDropdownOpen: boolean;
    setToolDropdownOpen: (updater: (prev: boolean) => boolean) => void;
    tasks: TaskManagementItem[];
    /** True while the initial/refresh ListTasks request is in flight. */
    tasksLoading?: boolean;
    /** True while a cloud workspace restore is syncing tasks in the background. */
    cloudTasksLoading?: boolean;
    renamingTaskPath: string | null;
    setRenamingTaskPath: (path: string | null) => void;
    renameValue: string;
    setRenameValue: (value: string) => void;
    resumeTask: (projectPath: string, task?: TaskManagementItem) => Promise<boolean | void> | boolean | void;
    continueWorkflowProject?: (projectPath: string) => Promise<void> | void;
    assistantReady?: boolean;
    onTaskSwitchBlocked?: () => void;
    createTask: (
        name: string,
        workingDir?: string,
        mode?: 'coding_dev' | 'remote_coding_dev',
        remote?: { host: string; port: number; user: string; password: string; workDir: string },
        workspaceId?: string,
    ) => Promise<void> | void;
    /** Expert-task creation path used by the chat task type in the create dialog. */
    onCreateExpertTask?: (expert: ExpertDefinition, options?: LatexExpertTaskOptions) => Promise<void> | void;
    refreshTasks: () => void;
    taskContextMenu: TaskContextMenu;
    setTaskContextMenu: (menu: TaskContextMenu) => void;
    renameTask: (projectPath: string, name: string) => Promise<unknown>;
    pinTask: (projectPath: string, pinned: boolean) => Promise<unknown>;
    hideTask: (projectPath: string, tags?: string[], force?: boolean) => Promise<unknown>;
    /** Focus-only activation for a task whose assistant tab is already open. */
    activateTask?: (projectPath: string, task?: TaskManagementItem) => void;
    /** Open project-tab paths; removal stays available but warns when the tab is open. */
    openProjectTabPaths?: string[];
    openProjectTabIdentities?: Array<{ projectPath: string; cloudWorkspaceId?: string }>;
    openExpertTabIDs?: string[];
    /** Currently visible assistant tab. Null/empty clears the task-list highlight. */
    activeAssistantTask?: ActiveAssistantTaskIdentity | null;
    /** Live "executing" signal for that tab; merges with the durable snapshot in status stats. */
    activeAssistantTaskRunning?: boolean;
    /** Busy task identities (normalized project paths + expert IDs) across all assistant tabs, incl. detached runs. */
    busyTaskRuns?: { projectPaths: string[]; expertIds: string[] };
    sidebarCurrentProviderTokenUsage: SidebarCurrentProviderTokenUsage;
    sidebarHubCredits: SidebarHubCredits | null;
    unlimitedHubCreditText: string;
    noHubAuthorizationText: string;
    showHubCreditAction: boolean;
    openHubCreditsPage: () => void;
    openServiceRedeemPage?: () => void;
    openLLMSettingsPage?: () => void;
    openIMSettingsPage?: () => void;
    openHubCardStorePage?: () => void;
    codingAgentProgress?: CodingAgentProgress | null;
    codingAgentTurnSnapshot?: CodingAgentTurnSnapshot | null;
    handleTaskManagementResizeStart: (e: ReactMouseEvent<HTMLDivElement> | ReactPointerEvent<HTMLDivElement> | number) => void;
    isTaskManagementResizing: boolean;
    switchTool: (tool: string) => void;
    onOpenVEConversation?: (ve: VirtualEmployeeEntry) => void;
    onOpenHistoryDiscussion?: (discussion: HistoryDiscussionSummary) => void;
    onSetFavoriteEmployee?: (ve: VirtualEmployeeEntry) => void;
    onRemoveFavoriteEmployee?: (ve: VirtualEmployeeEntry) => void;
    /** Authoritative favorite IDs from parent state (includes optimistic updates) */
    favoriteEmployeeIds?: string[];
    favoriteEmployeeNames?: Record<string, string>;
    onRenameEmployee?: (ve: VirtualEmployeeEntry, name: string) => void | Promise<void>;
    showCodingToolEntry?: boolean;
    digitalEmployeeFeatureStatus?: any;
    showDigitalEmployeeNavigation?: boolean;
    /** List of confirmed-available providers for the quick-switch dropdown. */
    availableProviders?: Array<{ name: string; url: string; isHubService: boolean; model?: string; models?: string[] }>;
    /** Called when user picks a different provider from the dropdown. */
    onSwitchProvider?: (providerID: string) => void;
    /** Current LLM model id for the active provider. */
    currentModel?: string;
    /** Model options for the active provider (fetched + configured fallback). */
    modelOptions?: string[];
    modelMultipliers?: Record<string, number>;
    modelsLoading?: boolean;
    onSwitchModel?: (modelId: string) => void;
    /** Called when the provider/model menu opens so parent can refresh the model list. */
    onOpenModelMenu?: () => void;
    onDismissModelMenu?: () => void;
    providerSelectionPending?: boolean;
    profileSavePending?: boolean;
    moaSticky?: {
        available: boolean;
        active: boolean;
        label?: string;
        preset?: string;
        presets?: Array<{ id: string; display_name?: string; ref_count?: number; enabled?: boolean }>;
    };
    onToggleMoASticky?: (on: boolean, presetId?: string) => void;
    profileSummaries?: { assistant?: LLMProfileStatusSummary; coding?: LLMProfileStatusSummary } | null;
    activeProfile?: 'assistant' | 'coding' | 'none';
    codingInheritsAssistant?: boolean;
};

export const SidebarAiPane = ({
    taskManagementPaneWidth,
    lang,
    aiThemeMode,
    aiLightSchemeId = DEFAULT_ASSISTANT_LIGHT_SCHEME_ID,
    aiDarkSchemeId,
    maclawLLMOnline,
    showLansenger = false,
    remoteActivationStatus,
    qqBotStatus,
    telegramStatus,
    weixinStatus,
    lansengerStatus,
    backgroundTaskCount = 0,
    workbenchTaskCounts,
    onOpenBackgroundTasks,
    showCloudWorkspaceManagement,
    showCloudWorkspaceCreation,
    restoreCloudWorkspaceTasks,
    config,
    activeTool,
    toolDropdownOpen,
    setToolDropdownOpen,
    tasks,
    tasksLoading = false,
    cloudTasksLoading = false,
    renamingTaskPath,
    setRenamingTaskPath,
    renameValue,
    setRenameValue,
    resumeTask,
    continueWorkflowProject,
    assistantReady = true,
    onTaskSwitchBlocked,
    createTask,
    onCreateExpertTask,
    refreshTasks,
    taskContextMenu,
    setTaskContextMenu,
    renameTask,
    pinTask,
    hideTask,
    activateTask,
    openProjectTabPaths,
    openProjectTabIdentities,
    openExpertTabIDs,
    activeAssistantTask,
    activeAssistantTaskRunning = false,
    busyTaskRuns,
    sidebarCurrentProviderTokenUsage,
    sidebarHubCredits,
    formatSidebarTokens,
    formatSidebarHubExpiry,
    formatSidebarHubTotalCredits,
    formatSidebarHubUsedCredits,
    formatSidebarCredit,
    unlimitedHubCreditText,
    noHubAuthorizationText,
    showHubCreditAction,
    openHubCreditsPage,
    openServiceRedeemPage,
    openLLMSettingsPage,
    openIMSettingsPage,
    openHubCardStorePage,
    codingAgentProgress = null,
    codingAgentTurnSnapshot = null,
    handleTaskManagementResizeStart,
    isTaskManagementResizing,
    switchTool,
    onOpenVEConversation,
    onOpenHistoryDiscussion,
    onSetFavoriteEmployee,
    onRemoveFavoriteEmployee,
    favoriteEmployeeIds = [],
    favoriteEmployeeNames = {},
    onRenameEmployee,
    showCodingToolEntry = false,
    digitalEmployeeFeatureStatus = null,
    showDigitalEmployeeNavigation,
    availableProviders = [],
    onSwitchProvider,
    currentModel = '',
    modelOptions = [],
    modelMultipliers,
    modelsLoading = false,
    onSwitchModel,
    onOpenModelMenu,
    onDismissModelMenu,
    providerSelectionPending,
    profileSavePending,
    moaSticky,
    onToggleMoASticky,
    profileSummaries,
    activeProfile,
    codingInheritsAssistant,
}: SidebarAiPaneProps) => {
    const botAllowed = useBotAccessEnabled();
    const inProgressBotTaskCount = useInProgressBotTaskCount(desktopBotAccountId(config), botAllowed);
    const showDigitalEmployeeTabs = showDigitalEmployeeNavigation ?? shouldShowDigitalEmployeeMiddleTabs(digitalEmployeeFeatureStatus);
    // A rail click that arrived while this pane was unmounted is already pending.
    // Seed the first paint from it so the task list does not flash first.
    const [middleTab, setMiddleTab] = useState<MiddleTab>(() => (
        showDigitalEmployeeTabs && peekPendingRailMiddleFocus() === 'employees' ? 'employees' : 'tasks'
    ));
    const veTheme = useMemo(() => (
        aiThemeMode === 'dark'
            ? getAssistantDarkScheme(aiDarkSchemeId).assistantTheme
            : getAssistantLightScheme(aiLightSchemeId).assistantTheme
    ), [aiThemeMode, aiDarkSchemeId, aiLightSchemeId]);
    const visibleTabs = useMemo<MiddleTab[]>(() => showDigitalEmployeeTabs ? ['tasks', 'employees', 'history'] : ['tasks'], [showDigitalEmployeeTabs]);
    const pendingFocus = usePendingRailMiddleFocus();
    useLayoutEffect(() => {
        if (!showDigitalEmployeeTabs && middleTab !== 'tasks') {
            setMiddleTab('tasks');
            overrideRailMiddleFocus('tasks');
        }
    }, [middleTab, showDigitalEmployeeTabs]);

    // A rail click that arrived before this pane mounted is already pending.
    // Drop a highlight left behind by an earlier digital-employee visit when
    // this mount is the task list (search, task activate, and the other paths
    // back to the assistant).
    useLayoutEffect(() => {
        if (peekPendingRailMiddleFocus()) return;
        if (middleTab !== 'employees') acknowledgeRailMiddleFocus('tasks');
        // Initial middle tab only. Later clicks arrive as a new pending value.
        // eslint-disable-next-line react-hooks/exhaustive-deps
    }, []);

    useLayoutEffect(() => {
        // Gate closed: leave the employees click queued. The highlight was
        // never moved, so there is nothing to revert.
        if (pendingFocus === 'employees' && !showDigitalEmployeeTabs) return;
        if (!pendingFocus) return;
        setMiddleTab(pendingFocus);
        acknowledgeRailMiddleFocus(pendingFocus);
    }, [pendingFocus, showDigitalEmployeeTabs]);

    // Welcome software-dev cards open the create-task dialog in TaskManagement. Keep the
    // tasks pane mounted (hidden) so the listener stays alive on employees/history tabs,
    // and jump back to "tasks" so the dialog context is clear.
    useEffect(() => {
        const handler = () => { requestRailMiddleFocus('tasks'); };
        window.addEventListener(EVENT_OPEN_CREATE_CODING_TASK, handler);
        return () => window.removeEventListener(EVENT_OPEN_CREATE_CODING_TASK, handler);
    }, []);

    // Favorite employees - use authoritative IDs from parent (includes optimistic updates)
    const tabLabels: Record<MiddleTab, string> = {
        tasks: lang === 'en' ? 'Tasks' : lang === 'zh-Hant' ? '任務' : '任务',
        employees: lang === 'en' ? 'AI Experts' : lang === 'zh-Hant' ? 'AI 專家' : 'AI 专家',
        history: lang === 'en' ? 'History' : lang === 'zh-Hant' ? '歷史會話' : '历史会话',
    };
    return (
        <>
            <div className="mc-sidebar-shell office-agent-sidebar" style={{ width: `${taskManagementPaneWidth}px`, flexShrink: 0, display: 'flex', flexDirection: 'column', background: 'var(--theme-page-bg)', minHeight: 0, overflow: 'hidden' }}>
                <SidebarToolSelector activeTool={activeTool} toolDropdownOpen={toolDropdownOpen} setToolDropdownOpen={setToolDropdownOpen} config={config} switchTool={switchTool} visible={showCodingToolEntry} />
                {visibleTabs.length > 1 && <SidebarMiddleTabs active={middleTab} labels={tabLabels} onChange={setMiddleTab} visibleTabs={visibleTabs} />}
                <div data-testid="sidebar-ai-content-slot" style={middleContentSlotStyle}>
                    <SidebarTasksPane {...{ lang, themeMode: aiThemeMode, tasks, tasksLoading, cloudTasksLoading, renamingTaskPath, setRenamingTaskPath, renameValue, setRenameValue, resumeTask, continueWorkflowProject, assistantReady, onTaskSwitchBlocked, createTask, onCreateExpertTask, refreshTasks, taskContextMenu, setTaskContextMenu, renameTask, pinTask, hideTask, activateTask, openProjectTabPaths, openProjectTabIdentities, openExpertTabIDs, activeAssistantTask, activeAssistantTaskRunning, busyTaskRuns, taskListVisible: middleTab === 'tasks', showCloudWorkspaceManagement: showCloudWorkspaceManagement ?? showCodingToolEntry, showCloudWorkspaceCreation: showCloudWorkspaceCreation ?? showCloudWorkspaceManagement ?? showCodingToolEntry, restoreCloudWorkspaceTasks }} />
                    {middleTab === 'employees' && showDigitalEmployeeTabs && <div data-testid="sidebar-middle-pane-employees" style={middlePaneStyle}><VirtualEmployeeTab lang={lang} theme={veTheme} onStartConversation={(ve) => onOpenVEConversation?.(ve)} favoriteEmployeeIds={favoriteEmployeeIds} favoriteEmployeeNames={favoriteEmployeeNames} onSetFavorite={onSetFavoriteEmployee} onRemoveFavorite={onRemoveFavoriteEmployee} onRenameEmployee={onRenameEmployee} /></div>}
                    {middleTab === 'history' && showDigitalEmployeeTabs && <div data-testid="sidebar-middle-pane-history" style={middlePaneStyle}><SidebarHistorySessions lang={lang} enabled={showDigitalEmployeeTabs} onOpenDiscussion={(discussion) => onOpenHistoryDiscussion?.(discussion)} /></div>}
                </div>
                <SidebarSystemStatus lang={lang} maclawLLMOnline={maclawLLMOnline} showLansenger={showLansenger} remoteActivationStatus={remoteActivationStatus} qqBotStatus={qqBotStatus} qqBotEnabled={config?.qqbot_enabled === true} telegramStatus={telegramStatus} telegramEnabled={config?.telegram_bot_enabled === true} weixinStatus={weixinStatus} weixinEnabled={config?.weixin_enabled === true} lansengerStatus={lansengerStatus} lansengerEnabled={isLansengerChannelEnabled(config)} backgroundTaskCount={backgroundTaskCount} workbenchTaskCounts={workbenchTaskCounts} botTaskCount={botAllowed ? inProgressBotTaskCount : undefined} onOpenBackgroundTasks={onOpenBackgroundTasks} localLLMCacheEnabled={(config as any)?.llm_prompt_cache?.enabled === true} sidebarCurrentProviderTokenUsage={sidebarCurrentProviderTokenUsage} sidebarHubCredits={sidebarHubCredits} formatSidebarTokens={formatSidebarTokens} formatSidebarHubExpiry={formatSidebarHubExpiry} formatSidebarHubTotalCredits={formatSidebarHubTotalCredits} formatSidebarHubUsedCredits={formatSidebarHubUsedCredits} formatSidebarCredit={formatSidebarCredit} unlimitedHubCreditText={unlimitedHubCreditText} noHubAuthorizationText={noHubAuthorizationText} showHubCreditAction={showHubCreditAction} openHubCreditsPage={openHubCreditsPage} openServiceRedeemPage={openServiceRedeemPage} openLLMSettingsPage={openLLMSettingsPage} openIMSettingsPage={openIMSettingsPage} openHubCardStorePage={openHubCardStorePage} codingAgentProgress={codingAgentProgress} codingAgentTurnSnapshot={codingAgentTurnSnapshot} isDark={aiThemeMode === 'dark'} availableProviders={availableProviders} onSwitchProvider={onSwitchProvider} currentModel={currentModel} modelOptions={modelOptions} modelMultipliers={modelMultipliers} modelsLoading={modelsLoading} onSwitchModel={onSwitchModel} onOpenModelMenu={onOpenModelMenu} onDismissModelMenu={onDismissModelMenu} moaSticky={moaSticky} onToggleMoASticky={onToggleMoASticky} profileSummaries={profileSummaries} activeProfile={activeProfile} codingInheritsAssistant={codingInheritsAssistant} providerSelectionPending={providerSelectionPending} profileSavePending={profileSavePending} />
            </div>
            <div
                className="mc-task-pane-resize-handle"
                data-testid="task-pane-resize-handle"
                role="separator"
                aria-orientation="vertical"
                aria-valuemin={180}
                aria-valuemax={460}
                aria-valuenow={Math.round(taskManagementPaneWidth)}
                aria-label={lang === 'en' ? 'Resize task panel' : lang === 'zh-Hant' ? '調整任務面板寬度' : '调整任务面板宽度'}
                tabIndex={0}
                onPointerDown={(event) => {
                    handleTaskManagementResizeStart(event);
                    event.currentTarget.setPointerCapture?.(event.pointerId);
                }}
                onPointerUp={(event) => {
                    if (event.currentTarget.hasPointerCapture?.(event.pointerId)) event.currentTarget.releasePointerCapture?.(event.pointerId);
                }}
                onPointerCancel={(event) => {
                    if (event.currentTarget.hasPointerCapture?.(event.pointerId)) event.currentTarget.releasePointerCapture?.(event.pointerId);
                }}
                onMouseDown={(event) => {
                    if (typeof window.PointerEvent === 'undefined') handleTaskManagementResizeStart(event);
                }}
                onKeyDown={(event) => {
                    const delta = event.key === 'ArrowLeft' ? -16 : event.key === 'ArrowRight' ? 16 : 0;
                    if (delta !== 0) {
                        event.preventDefault();
                        handleTaskManagementResizeStart(taskManagementPaneWidth + delta);
                    } else if (event.key === 'Home' || event.key === 'End') {
                        event.preventDefault();
                        handleTaskManagementResizeStart(event.key === 'Home' ? 180 : 460);
                    }
                }}
                title={lang === 'en' ? 'Drag to resize middle panel' : lang === 'zh-Hant' ? '拖動調整中間面板寬度' : '拖动调整中间面板宽度'}
                style={{ width: '12px', marginLeft: '-3px', marginRight: '-3px', position: 'relative', zIndex: 40, flexShrink: 0, cursor: 'col-resize', background: 'transparent', touchAction: 'none', userSelect: 'none', pointerEvents: 'auto', ['WebkitAppRegion' as any]: 'no-drag', ['--wails-draggable' as any]: 'no-drag', ['--mc-task-pane-divider-color' as any]: isTaskManagementResizing ? 'var(--theme-primary)' : undefined }}
            />
        </>
    );
};
