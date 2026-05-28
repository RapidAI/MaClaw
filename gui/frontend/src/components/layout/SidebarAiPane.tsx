import { useEffect, useMemo, useState, type CSSProperties, type MouseEvent as ReactMouseEvent } from 'react';
import type { SidebarCurrentProviderTokenUsage, SidebarHubCredits } from '../../types/appShell';
import type { CodingAgentProgress, CodingAgentTurnSnapshot } from '../ai/CodingAgentProgressStatus';
import { SidebarToolSelector } from './SidebarToolSelector';
import { SidebarRecentTasks, type RecentProject, type TaskContextMenu } from './SidebarRecentTasks';
import { SidebarSystemStatus } from './SidebarSystemStatus';
import { VirtualEmployeeTab, type VirtualEmployeeEntry } from '../ai/VirtualEmployeeTab';
import { darkTheme, lightTheme } from '../ai/aiAssistantPanelTheme';
import { SidebarMiddleTabs } from './SidebarMiddleTabs';
import { SidebarHistorySessions, type HistoryDiscussionSummary } from './SidebarHistorySessions';
import { isDigitalEmployeeAuthorizationUsable, shouldShowDigitalEmployeeFeatureTabs } from '../ai/digitalEmployeeFeature';

export { isDigitalEmployeeAuthorizationUsable } from '../ai/digitalEmployeeFeature';

type MiddleTab = 'tasks' | 'employees' | 'history';

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
};

export function shouldShowDigitalEmployeeMiddleTabs(status: any, nowMs = Date.now()): boolean {
    return shouldShowDigitalEmployeeFeatureTabs(status, nowMs);
}

type SidebarAiPaneProps = {
    recentTasksPaneWidth: number;
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
    config: any;
    activeTool: string;
    toolDropdownOpen: boolean;
    setToolDropdownOpen: (updater: (prev: boolean) => boolean) => void;
    recentProjects: RecentProject[];
    renamingTaskPath: string | null;
    setRenamingTaskPath: (path: string | null) => void;
    renameValue: string;
    setRenameValue: (value: string) => void;
    resumeRecentProject: (projectPath: string) => Promise<void> | void;
    assistantReady?: boolean;
    onRecentTaskSwitchBlocked?: () => void;
    createRecentTask: (name: string) => Promise<void> | void;
    refreshRecentProjects: () => void;
    taskContextMenu: TaskContextMenu;
    setTaskContextMenu: (menu: TaskContextMenu) => void;
    renameTask: (projectPath: string, name: string) => Promise<unknown>;
    pinTask: (projectPath: string, pinned: boolean) => Promise<unknown>;
    hideTask: (projectPath: string) => Promise<unknown>;
    sidebarCurrentProviderTokenUsage: SidebarCurrentProviderTokenUsage;
    sidebarHubCredits: SidebarHubCredits | null;
    formatSidebarTokens: (value: number) => string;
    formatSidebarHubExpiry: (credits: SidebarHubCredits | null) => string;
    formatSidebarHubTotalCredits: (credits: SidebarHubCredits | null) => string;
    formatSidebarHubUsedCredits: (credits: SidebarHubCredits | null) => string;
    formatSidebarCredit: (value: number) => string;
    unlimitedHubCreditText: string;
    noHubAuthorizationText: string;
    showHubCreditAction: boolean;
    openHubCreditsPage: () => void;
    codingAgentProgress?: CodingAgentProgress | null;
    codingAgentTurnSnapshot?: CodingAgentTurnSnapshot | null;
    handleRecentTasksResizeStart: (e: ReactMouseEvent<HTMLDivElement>) => void;
    isRecentTasksResizing: boolean;
    switchTool: (tool: string) => void;
    onOpenVEConversation?: (ve: VirtualEmployeeEntry) => void;
    onOpenHistoryDiscussion?: (discussion: HistoryDiscussionSummary) => void;
    onSetFavoriteEmployee?: (ve: VirtualEmployeeEntry) => void;
    onRemoveFavoriteEmployee?: (ve: VirtualEmployeeEntry) => void;
    /** Authoritative favorite IDs from parent state (includes optimistic updates) */
    favoriteEmployeeIds?: string[];
    showCodingToolEntry?: boolean;
    digitalEmployeeFeatureStatus?: any;
    showDigitalEmployeeNavigation?: boolean;
};

export const SidebarAiPane = ({
    recentTasksPaneWidth,
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
    config,
    activeTool,
    toolDropdownOpen,
    setToolDropdownOpen,
    recentProjects,
    renamingTaskPath,
    setRenamingTaskPath,
    renameValue,
    setRenameValue,
    resumeRecentProject,
    assistantReady = true,
    onRecentTaskSwitchBlocked,
    createRecentTask,
    refreshRecentProjects,
    taskContextMenu,
    setTaskContextMenu,
    renameTask,
    pinTask,
    hideTask,
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
    codingAgentProgress = null,
    codingAgentTurnSnapshot = null,
    handleRecentTasksResizeStart,
    isRecentTasksResizing,
    switchTool,
    onOpenVEConversation,
    onOpenHistoryDiscussion,
    onSetFavoriteEmployee,
    onRemoveFavoriteEmployee,
    favoriteEmployeeIds = [],
    showCodingToolEntry = false,
    digitalEmployeeFeatureStatus = null,
    showDigitalEmployeeNavigation,
}: SidebarAiPaneProps) => {
    const [middleTab, setMiddleTab] = useState<MiddleTab>('tasks');
    const veTheme = useMemo(() => (aiThemeMode === 'dark' ? darkTheme : lightTheme), [aiThemeMode]);
    const showDigitalEmployeeTabs = showDigitalEmployeeNavigation ?? shouldShowDigitalEmployeeMiddleTabs(digitalEmployeeFeatureStatus);
    const visibleTabs = useMemo<MiddleTab[]>(() => showDigitalEmployeeTabs ? ['tasks', 'employees', 'history'] : ['tasks'], [showDigitalEmployeeTabs]);
    useEffect(() => {
        if (!showDigitalEmployeeTabs && middleTab !== 'tasks') setMiddleTab('tasks');
    }, [middleTab, showDigitalEmployeeTabs]);

    // Favorite employees - use authoritative IDs from parent (includes optimistic updates)
    const tabLabels: Record<MiddleTab, string> = {
        tasks: lang === 'en' ? 'Tasks' : lang === 'zh-Hant' ? '最近任務' : '最近任务',
        employees: lang === 'en' ? 'Digital Employees' : lang === 'zh-Hant' ? '數字員工' : '数字员工',
        history: lang === 'en' ? 'History' : lang === 'zh-Hant' ? '歷史會話' : '历史会话',
    };
    return (
        <>
            <div style={{ width: `${recentTasksPaneWidth}px`, flexShrink: 0, display: 'flex', flexDirection: 'column', borderRight: '1px solid var(--theme-border)', background: 'var(--theme-page-bg)', minHeight: 0, overflow: 'hidden' }}>
                <SidebarToolSelector activeTool={activeTool} toolDropdownOpen={toolDropdownOpen} setToolDropdownOpen={setToolDropdownOpen} config={config} switchTool={switchTool} visible={showCodingToolEntry} />
                {visibleTabs.length > 1 && <SidebarMiddleTabs active={middleTab} labels={tabLabels} onChange={setMiddleTab} visibleTabs={visibleTabs} />}
                <div data-testid="sidebar-ai-content-slot" style={middleContentSlotStyle}>
                    {middleTab === 'tasks' && <SidebarRecentTasks lang={lang} themeMode={aiThemeMode} recentProjects={recentProjects} renamingTaskPath={renamingTaskPath} setRenamingTaskPath={setRenamingTaskPath} renameValue={renameValue} setRenameValue={setRenameValue} resumeRecentProject={resumeRecentProject} assistantReady={assistantReady} onRecentTaskSwitchBlocked={onRecentTaskSwitchBlocked} createRecentTask={createRecentTask} refreshRecentProjects={refreshRecentProjects} taskContextMenu={taskContextMenu} setTaskContextMenu={setTaskContextMenu} renameTask={renameTask} pinTask={pinTask} hideTask={hideTask} />}
                    {middleTab === 'employees' && showDigitalEmployeeTabs && <div style={middlePaneStyle}><VirtualEmployeeTab lang={lang} theme={veTheme} onStartConversation={(ve) => onOpenVEConversation?.(ve)} favoriteEmployeeIds={favoriteEmployeeIds} onSetFavorite={onSetFavoriteEmployee} onRemoveFavorite={onRemoveFavoriteEmployee} /></div>}
                    {middleTab === 'history' && showDigitalEmployeeTabs && <div style={middlePaneStyle}><SidebarHistorySessions lang={lang} enabled={(config as any)?.group_discussion?.enabled !== false} onOpenDiscussion={(discussion) => onOpenHistoryDiscussion?.(discussion)} /></div>}
                </div>
                <SidebarSystemStatus lang={lang} maclawLLMOnline={maclawLLMOnline} showLansenger={showLansenger} remoteActivationStatus={remoteActivationStatus} qqBotStatus={qqBotStatus} telegramStatus={telegramStatus} weixinStatus={weixinStatus} lansengerStatus={lansengerStatus} backgroundTaskCount={backgroundTaskCount} sidebarCurrentProviderTokenUsage={sidebarCurrentProviderTokenUsage} sidebarHubCredits={sidebarHubCredits} formatSidebarTokens={formatSidebarTokens} formatSidebarHubExpiry={formatSidebarHubExpiry} formatSidebarHubTotalCredits={formatSidebarHubTotalCredits} formatSidebarHubUsedCredits={formatSidebarHubUsedCredits} formatSidebarCredit={formatSidebarCredit} unlimitedHubCreditText={unlimitedHubCreditText} noHubAuthorizationText={noHubAuthorizationText} showHubCreditAction={showHubCreditAction} openHubCreditsPage={openHubCreditsPage} codingAgentProgress={codingAgentProgress} codingAgentTurnSnapshot={codingAgentTurnSnapshot} />
            </div>
            <div onMouseDown={handleRecentTasksResizeStart} title={lang === 'en' ? 'Drag to resize middle panel' : lang === 'zh-Hant' ? '拖動調整中間面板寬度' : '拖动调整中间面板宽度'} style={{ width: '6px', flexShrink: 0, cursor: 'col-resize', background: isRecentTasksResizing ? 'color-mix(in srgb, var(--theme-primary) 42%, transparent)' : 'transparent', borderRight: '1px solid var(--theme-border)', transition: 'background 120ms ease', ['--wails-draggable' as any]: 'no-drag' }} />
        </>
    );
};
