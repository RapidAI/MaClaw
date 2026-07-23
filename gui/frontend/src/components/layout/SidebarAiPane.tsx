import { useEffect, useMemo, useState, type CSSProperties, type MouseEvent as ReactMouseEvent } from 'react';
import type { SidebarCreditDisplayFormatters, SidebarCurrentProviderTokenUsage, SidebarHubCredits } from '../../types/appShell';
import { EVENT_OPEN_CREATE_CODING_TASK } from '../../constants/events';
import type { CodingAgentProgress, CodingAgentTurnSnapshot } from '../ai/CodingAgentProgressStatus';
import { SidebarToolSelector } from './SidebarToolSelector';
import { SidebarTaskManagement, type TaskManagementItem, type TaskContextMenu } from './SidebarTaskManagement';
import { SidebarSystemStatus } from './SidebarSystemStatus';
import { VirtualEmployeeTab, type VirtualEmployeeEntry } from '../ai/VirtualEmployeeTab';
import { darkTheme, lightTheme } from '../ai/aiAssistantPanelTheme';
import { SidebarMiddleTabs } from './SidebarMiddleTabs';
import { SidebarHistorySessions, type HistoryDiscussionSummary } from './SidebarHistorySessions';
import { isDigitalEmployeeAuthorizationUsable, shouldShowDigitalEmployeeFeatureTabs } from '../ai/digitalEmployeeFeature';

export { isDigitalEmployeeAuthorizationUsable } from '../ai/digitalEmployeeFeature';

type MiddleTab = 'tasks' | 'employees' | 'history';

const middlePaneInsetPx = 6;

const middleContentSlotStyle: CSSProperties = {
    flex: 1,
    minHeight: 0,
    overflow: 'hidden',
    display: 'flex',
    flexDirection: 'column',
};

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
    maclawLLMOnline: boolean;
    showLansenger?: boolean;
    remoteActivationStatus: any;
    qqBotStatus: string;
    telegramStatus: string;
    weixinStatus: string;
    lansengerStatus: string;
    backgroundTaskCount?: number;
    onOpenBackgroundTasks?: () => void;
    config: any;
    activeTool: string;
    toolDropdownOpen: boolean;
    setToolDropdownOpen: (updater: (prev: boolean) => boolean) => void;
    tasks: TaskManagementItem[];
    renamingTaskPath: string | null;
    setRenamingTaskPath: (path: string | null) => void;
    renameValue: string;
    setRenameValue: (value: string) => void;
    resumeTask: (projectPath: string) => Promise<void> | void;
    continueWorkflowProject?: (projectPath: string) => Promise<void> | void;
    assistantReady?: boolean;
    onTaskSwitchBlocked?: () => void;
    createTask: (
        name: string,
        workingDir?: string,
        mode?: 'coding_dev' | 'remote_coding_dev',
        remote?: { host: string; port: number; user: string; password: string; workDir: string },
    ) => Promise<void> | void;
    refreshTasks: () => void;
    taskContextMenu: TaskContextMenu;
    setTaskContextMenu: (menu: TaskContextMenu) => void;
    renameTask: (projectPath: string, name: string) => Promise<unknown>;
    pinTask: (projectPath: string, pinned: boolean) => Promise<unknown>;
    hideTask: (projectPath: string) => Promise<unknown>;
    /** Open project-tab paths; tasks with open tabs cannot be removed from the list menu. */
    openProjectTabPaths?: string[];
    sidebarCurrentProviderTokenUsage: SidebarCurrentProviderTokenUsage;
    sidebarHubCredits: SidebarHubCredits | null;
    unlimitedHubCreditText: string;
    noHubAuthorizationText: string;
    showHubCreditAction: boolean;
    openHubCreditsPage: () => void;
    openServiceRedeemPage?: () => void;
    openLLMSettingsPage?: () => void;
    openHubCardStorePage?: () => void;
    codingAgentProgress?: CodingAgentProgress | null;
    codingAgentTurnSnapshot?: CodingAgentTurnSnapshot | null;
    handleTaskManagementResizeStart: (e: ReactMouseEvent<HTMLDivElement>) => void;
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
    onSwitchProvider?: (providerName: string) => void;
    /** Current LLM model id for the active provider. */
    currentModel?: string;
    /** Model options for the active provider (fetched + configured fallback). */
    modelOptions?: string[];
    modelsLoading?: boolean;
    onSwitchModel?: (modelId: string) => void;
    /** Called when the provider/model menu opens so parent can refresh the model list. */
    onOpenModelMenu?: () => void;
    moaSticky?: {
        available: boolean;
        active: boolean;
        label?: string;
        preset?: string;
        presets?: Array<{ id: string; display_name?: string; ref_count?: number; enabled?: boolean }>;
    };
    onToggleMoASticky?: (on: boolean, presetId?: string) => void;
};

export const SidebarAiPane = ({
    taskManagementPaneWidth,
    lang,
    aiThemeMode,
    maclawLLMOnline,
    showLansenger = false,
    remoteActivationStatus,
    qqBotStatus,
    telegramStatus,
    weixinStatus,
    lansengerStatus,
    backgroundTaskCount = 0,
    onOpenBackgroundTasks,
    config,
    activeTool,
    toolDropdownOpen,
    setToolDropdownOpen,
    tasks,
    renamingTaskPath,
    setRenamingTaskPath,
    renameValue,
    setRenameValue,
    resumeTask,
    continueWorkflowProject,
    assistantReady = true,
    onTaskSwitchBlocked,
    createTask,
    refreshTasks,
    taskContextMenu,
    setTaskContextMenu,
    renameTask,
    pinTask,
    hideTask,
    openProjectTabPaths,
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
    modelsLoading = false,
    onSwitchModel,
    onOpenModelMenu,
    moaSticky,
    onToggleMoASticky,
}: SidebarAiPaneProps) => {
    const [middleTab, setMiddleTab] = useState<MiddleTab>('tasks');
    const veTheme = useMemo(() => (aiThemeMode === 'dark' ? darkTheme : lightTheme), [aiThemeMode]);
    const showDigitalEmployeeTabs = showDigitalEmployeeNavigation ?? shouldShowDigitalEmployeeMiddleTabs(digitalEmployeeFeatureStatus);
    const visibleTabs = useMemo<MiddleTab[]>(() => showDigitalEmployeeTabs ? ['tasks', 'employees', 'history'] : ['tasks'], [showDigitalEmployeeTabs]);
    useEffect(() => {
        if (!showDigitalEmployeeTabs && middleTab !== 'tasks') setMiddleTab('tasks');
    }, [middleTab, showDigitalEmployeeTabs]);

    // Welcome software-dev cards open the create-task dialog in TaskManagement. Keep the
    // tasks pane mounted (hidden) so the listener stays alive on employees/history tabs,
    // and jump back to "tasks" so the dialog context is clear.
    useEffect(() => {
        const handler = () => setMiddleTab('tasks');
        window.addEventListener(EVENT_OPEN_CREATE_CODING_TASK, handler);
        return () => window.removeEventListener(EVENT_OPEN_CREATE_CODING_TASK, handler);
    }, []);

    // Favorite employees - use authoritative IDs from parent (includes optimistic updates)
    const tabLabels: Record<MiddleTab, string> = {
        tasks: lang === 'en' ? 'Task Management' : lang === 'zh-Hant' ? '任務管理' : '任务管理',
        employees: lang === 'en' ? 'Digital Employees' : lang === 'zh-Hant' ? '數字員工' : '数字员工',
        history: lang === 'en' ? 'History' : lang === 'zh-Hant' ? '歷史會話' : '历史会话',
    };
    return (
        <>
            <div style={{ width: `${taskManagementPaneWidth}px`, flexShrink: 0, display: 'flex', flexDirection: 'column', borderRight: '1px solid var(--theme-border)', background: 'var(--theme-page-bg)', minHeight: 0, overflow: 'hidden' }}>
                <SidebarToolSelector activeTool={activeTool} toolDropdownOpen={toolDropdownOpen} setToolDropdownOpen={setToolDropdownOpen} config={config} switchTool={switchTool} visible={showCodingToolEntry} />
                {visibleTabs.length > 1 && <SidebarMiddleTabs active={middleTab} labels={tabLabels} onChange={setMiddleTab} visibleTabs={visibleTabs} />}
                <div data-testid="sidebar-ai-content-slot" style={middleContentSlotStyle}>
                    <div
                        data-testid="sidebar-middle-pane-tasks"
                        // Keep mounted (hidden) on other middle tabs so welcome coding events still open create dialog.
                        style={{
                            display: middleTab === 'tasks' ? 'flex' : 'none',
                            flex: 1,
                            minHeight: 0,
                            overflow: 'hidden',
                            flexDirection: 'column',
                        }}
                    >
                        <SidebarTaskManagement lang={lang} themeMode={aiThemeMode} tasks={tasks} renamingTaskPath={renamingTaskPath} setRenamingTaskPath={setRenamingTaskPath} renameValue={renameValue} setRenameValue={setRenameValue} resumeTask={resumeTask} continueWorkflowProject={continueWorkflowProject} assistantReady={assistantReady} onTaskSwitchBlocked={onTaskSwitchBlocked} createTask={createTask} refreshTasks={refreshTasks} taskContextMenu={taskContextMenu} setTaskContextMenu={setTaskContextMenu} renameTask={renameTask} pinTask={pinTask} hideTask={hideTask} openProjectTabPaths={openProjectTabPaths} />
                    </div>
                    {middleTab === 'employees' && showDigitalEmployeeTabs && <div data-testid="sidebar-middle-pane-employees" style={middlePaneStyle}><VirtualEmployeeTab lang={lang} theme={veTheme} onStartConversation={(ve) => onOpenVEConversation?.(ve)} favoriteEmployeeIds={favoriteEmployeeIds} favoriteEmployeeNames={favoriteEmployeeNames} onSetFavorite={onSetFavoriteEmployee} onRemoveFavorite={onRemoveFavoriteEmployee} onRenameEmployee={onRenameEmployee} /></div>}
                    {middleTab === 'history' && showDigitalEmployeeTabs && <div data-testid="sidebar-middle-pane-history" style={middlePaneStyle}><SidebarHistorySessions lang={lang} enabled={showDigitalEmployeeTabs} onOpenDiscussion={(discussion) => onOpenHistoryDiscussion?.(discussion)} /></div>}
                </div>
                <SidebarSystemStatus lang={lang} maclawLLMOnline={maclawLLMOnline} showLansenger={showLansenger} remoteActivationStatus={remoteActivationStatus} qqBotStatus={qqBotStatus} telegramStatus={telegramStatus} weixinStatus={weixinStatus} lansengerStatus={lansengerStatus} backgroundTaskCount={backgroundTaskCount} onOpenBackgroundTasks={onOpenBackgroundTasks} localLLMCacheEnabled={(config as any)?.llm_prompt_cache?.enabled === true} sidebarCurrentProviderTokenUsage={sidebarCurrentProviderTokenUsage} sidebarHubCredits={sidebarHubCredits} formatSidebarTokens={formatSidebarTokens} formatSidebarHubExpiry={formatSidebarHubExpiry} formatSidebarHubTotalCredits={formatSidebarHubTotalCredits} formatSidebarHubUsedCredits={formatSidebarHubUsedCredits} formatSidebarCredit={formatSidebarCredit} unlimitedHubCreditText={unlimitedHubCreditText} noHubAuthorizationText={noHubAuthorizationText} showHubCreditAction={showHubCreditAction} openHubCreditsPage={openHubCreditsPage} openServiceRedeemPage={openServiceRedeemPage} openLLMSettingsPage={openLLMSettingsPage} openHubCardStorePage={openHubCardStorePage} codingAgentProgress={codingAgentProgress} codingAgentTurnSnapshot={codingAgentTurnSnapshot} isDark={aiThemeMode === 'dark'} availableProviders={availableProviders} onSwitchProvider={onSwitchProvider} currentModel={currentModel} modelOptions={modelOptions} modelsLoading={modelsLoading} onSwitchModel={onSwitchModel} onOpenModelMenu={onOpenModelMenu} moaSticky={moaSticky} onToggleMoASticky={onToggleMoASticky} />
            </div>
            <div onMouseDown={handleTaskManagementResizeStart} title={lang === 'en' ? 'Drag to resize middle panel' : lang === 'zh-Hant' ? '拖動調整中間面板寬度' : '拖动调整中间面板宽度'} style={{ width: '6px', flexShrink: 0, cursor: 'col-resize', background: isTaskManagementResizing ? 'color-mix(in srgb, var(--theme-primary) 42%, transparent)' : 'transparent', borderRight: '1px solid var(--theme-border)', transition: 'background 120ms ease', ['--wails-draggable' as any]: 'no-drag' }} />
        </>
    );
};
