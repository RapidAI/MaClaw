import { useEffect, useMemo, useState, type MouseEvent as ReactMouseEvent } from 'react';
import type { SidebarHubCredits } from '../../types/appShell';
import type { CodingAgentProgress, CodingAgentTurnSnapshot } from '../ai/CodingAgentProgressStatus';
import { SidebarToolSelector } from './SidebarToolSelector';
import { SidebarRecentTasks, type RecentProject, type TaskContextMenu } from './SidebarRecentTasks';
import { SidebarSystemStatus } from './SidebarSystemStatus';
import { VirtualEmployeeTab, type VirtualEmployeeEntry } from '../ai/VirtualEmployeeTab';
import { darkTheme, lightTheme } from '../ai/aiAssistantPanelTheme';
import { SidebarMiddleTabs } from './SidebarMiddleTabs';
import { SidebarHistorySessions, type HistoryDiscussionSummary } from './SidebarHistorySessions';

type MiddleTab = 'tasks' | 'employees' | 'history';

export function shouldShowDigitalEmployeeMiddleTabs(status: any): boolean {
    if (!status?.visible) return false;
    const auth = status?.authorization || {};
    if (auth.active !== true) return false;
    if (Number(auth.quota || 0) <= 0) return false;
    if (Number(status?.actual_count || 0) <= 0) return false;
    return true;
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
    sidebarCurrentProviderTokenUsage: { provider: string; isHubService: boolean; input: number; output: number; total: number };
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
    showCodingToolEntry?: boolean;
    digitalEmployeeFeatureStatus?: any;
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
    showCodingToolEntry = false,
    digitalEmployeeFeatureStatus = null,
}: SidebarAiPaneProps) => {
    const [middleTab, setMiddleTab] = useState<MiddleTab>('tasks');
    const veTheme = useMemo(() => (aiThemeMode === 'dark' ? darkTheme : lightTheme), [aiThemeMode]);
    const showDigitalEmployeeTabs = shouldShowDigitalEmployeeMiddleTabs(digitalEmployeeFeatureStatus);
    const visibleTabs = useMemo<MiddleTab[]>(() => showDigitalEmployeeTabs ? ['tasks', 'employees', 'history'] : ['tasks'], [showDigitalEmployeeTabs]);
    useEffect(() => {
        if (!showDigitalEmployeeTabs && middleTab !== 'tasks') setMiddleTab('tasks');
    }, [middleTab, showDigitalEmployeeTabs]);
    const tabLabels: Record<MiddleTab, string> = {
        tasks: lang === 'en' ? 'Tasks' : lang === 'zh-Hant' ? '最近任務' : '最近任务',
        employees: lang === 'en' ? 'Digital' : lang === 'zh-Hant' ? '數字員工' : '数字员工',
        history: lang === 'en' ? 'History' : lang === 'zh-Hant' ? '歷史會話' : '历史会话',
    };
    return (
        <>
            <div style={{ width: `${recentTasksPaneWidth}px`, flexShrink: 0, display: 'flex', flexDirection: 'column', borderRight: '1px solid var(--theme-border)', background: 'var(--theme-page-bg)', minHeight: 0, overflow: 'hidden' }}>
                <SidebarToolSelector activeTool={activeTool} toolDropdownOpen={toolDropdownOpen} setToolDropdownOpen={setToolDropdownOpen} config={config} switchTool={switchTool} visible={showCodingToolEntry} />
                {visibleTabs.length > 1 && <SidebarMiddleTabs active={middleTab} labels={tabLabels} onChange={setMiddleTab} visibleTabs={visibleTabs} />}
                {middleTab === 'tasks' && <SidebarRecentTasks lang={lang} themeMode={aiThemeMode} recentProjects={recentProjects} renamingTaskPath={renamingTaskPath} setRenamingTaskPath={setRenamingTaskPath} renameValue={renameValue} setRenameValue={setRenameValue} resumeRecentProject={resumeRecentProject} assistantReady={assistantReady} onRecentTaskSwitchBlocked={onRecentTaskSwitchBlocked} createRecentTask={createRecentTask} refreshRecentProjects={refreshRecentProjects} taskContextMenu={taskContextMenu} setTaskContextMenu={setTaskContextMenu} renameTask={renameTask} pinTask={pinTask} hideTask={hideTask} />}
                {middleTab === 'employees' && showDigitalEmployeeTabs && <div style={{ flex: 1, minHeight: 0, overflow: 'hidden' }}><VirtualEmployeeTab lang={lang} theme={veTheme} onStartConversation={(ve) => onOpenVEConversation?.(ve)} onAddToGroup={(ve) => onOpenVEConversation?.(ve)} /></div>}
                {middleTab === 'history' && showDigitalEmployeeTabs && <SidebarHistorySessions lang={lang} enabled={(config as any)?.group_discussion?.enabled !== false} onOpenDiscussion={(discussion) => onOpenHistoryDiscussion?.(discussion)} />}
                <SidebarSystemStatus lang={lang} maclawLLMOnline={maclawLLMOnline} showLansenger={showLansenger} remoteActivationStatus={remoteActivationStatus} qqBotStatus={qqBotStatus} telegramStatus={telegramStatus} weixinStatus={weixinStatus} lansengerStatus={lansengerStatus} sidebarCurrentProviderTokenUsage={sidebarCurrentProviderTokenUsage} sidebarHubCredits={sidebarHubCredits} formatSidebarTokens={formatSidebarTokens} formatSidebarHubExpiry={formatSidebarHubExpiry} formatSidebarHubTotalCredits={formatSidebarHubTotalCredits} formatSidebarHubUsedCredits={formatSidebarHubUsedCredits} formatSidebarCredit={formatSidebarCredit} unlimitedHubCreditText={unlimitedHubCreditText} noHubAuthorizationText={noHubAuthorizationText} showHubCreditAction={showHubCreditAction} openHubCreditsPage={openHubCreditsPage} codingAgentProgress={codingAgentProgress} codingAgentTurnSnapshot={codingAgentTurnSnapshot} />
            </div>
            <div onMouseDown={handleRecentTasksResizeStart} title={lang === 'en' ? 'Drag to resize middle panel' : '拖动调整中间面板宽度'} style={{ width: '6px', flexShrink: 0, cursor: 'col-resize', background: isRecentTasksResizing ? 'color-mix(in srgb, var(--theme-primary) 42%, transparent)' : 'transparent', borderRight: '1px solid var(--theme-border)', transition: 'background 120ms ease', ['--wails-draggable' as any]: 'no-drag' }} />
        </>
    );
};
