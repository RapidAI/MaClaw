import type { MouseEvent as ReactMouseEvent } from 'react';
import type { SidebarHubCredits } from '../../types/appShell';
import { SidebarToolSelector } from './SidebarToolSelector';
import { SidebarRecentTasks, type RecentProject, type TaskContextMenu } from './SidebarRecentTasks';
import { SidebarSystemStatus } from './SidebarSystemStatus';

type SidebarAiPaneProps = {
    recentTasksPaneWidth: number;
    lang: string;
    maclawLLMOnline: boolean;
    agentNetRunning: boolean;
    remoteActivationStatus: any;
    qqBotStatus: string;
    telegramStatus: string;
    weixinStatus: string;
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
    handleRecentTasksResizeStart: (e: ReactMouseEvent<HTMLDivElement>) => void;
    isRecentTasksResizing: boolean;
    switchTool: (tool: string) => void;
};


export const SidebarAiPane = ({
    recentTasksPaneWidth,
    lang,
    maclawLLMOnline,
    agentNetRunning,
    remoteActivationStatus,
    qqBotStatus,
    telegramStatus,
    weixinStatus,
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
    handleRecentTasksResizeStart,
    isRecentTasksResizing,
    switchTool,
}: SidebarAiPaneProps) => (
    <>
<div style={{ width: `${recentTasksPaneWidth}px`, flexShrink: 0, display: 'flex', flexDirection: 'column', borderRight: '1px solid var(--theme-border)', background: 'var(--theme-page-bg)', minHeight: 0, overflow: 'hidden' }}>
    <SidebarToolSelector
        activeTool={activeTool}
        toolDropdownOpen={toolDropdownOpen}
        setToolDropdownOpen={setToolDropdownOpen}
        config={config}
        switchTool={switchTool}
    />

    <SidebarRecentTasks
        lang={lang}
        recentProjects={recentProjects}
        renamingTaskPath={renamingTaskPath}
        setRenamingTaskPath={setRenamingTaskPath}
        renameValue={renameValue}
        setRenameValue={setRenameValue}
        resumeRecentProject={resumeRecentProject}
        refreshRecentProjects={refreshRecentProjects}
        taskContextMenu={taskContextMenu}
        setTaskContextMenu={setTaskContextMenu}
        renameTask={renameTask}
        pinTask={pinTask}
        hideTask={hideTask}
    />

    <SidebarSystemStatus
        lang={lang}
        maclawLLMOnline={maclawLLMOnline}
        agentNetRunning={agentNetRunning}
        remoteActivationStatus={remoteActivationStatus}
        qqBotStatus={qqBotStatus}
        telegramStatus={telegramStatus}
        weixinStatus={weixinStatus}
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
    />
</div>
<div
    onMouseDown={handleRecentTasksResizeStart}
    title={lang === 'zh-Hans' ? '拖动调整最近任务宽度' : lang === 'zh-Hant' ? '拖動調整最近任務寬度' : 'Drag to resize recent tasks'}
    style={{
        width: '6px',
        flexShrink: 0,
        cursor: 'col-resize',
        background: isRecentTasksResizing ? 'color-mix(in srgb, var(--theme-primary) 42%, transparent)' : 'transparent',
        borderRight: '1px solid var(--theme-border)',
        transition: 'background 120ms ease',
        ['--wails-draggable' as any]: 'no-drag',
    }}
/>
</>
);
