import type { MouseEvent as ReactMouseEvent } from 'react';
import { SidebarAiPane } from './SidebarAiPane';
import { SidebarNavRail } from './SidebarNavRail';
import type { SidebarHubCredits } from '../../types/appShell';
import { SIDEBAR_AI_PANE_GAP, SIDEBAR_NAV_RAIL_WIDTH } from './sidebarLayout';

type RecentProject = {
    id?: string;
    name?: string;
    project_path: string;
    workflow_type?: string;
    preview?: string;
    last_activity?: string;
    pinned?: boolean;
};

type TaskContextMenu = { x: number; y: number; projectPath: string; name: string; pinned: boolean } | null;

interface AppSidebarShellProps {
    navTab: string;
    recentTasksPaneWidth: number;
    aiThemeMode: 'light' | 'dark';
    brandInfo: { id: string } | null;
    currentIcon: string;
    brandSidebarName: string;
    switchTool: (tool: string) => void;
    lang: string;
    maclawLLMOnline: boolean;
    agentNetRunning: boolean;
    remoteActivationStatus: any;
    qqBotStatus: string;
    telegramStatus: string;
    weixinStatus: string;
    runningTaskCount: number;
    t: (key: string) => string;
    gossipAllowed: boolean;
    config: any;
    sidebarExpanded: boolean;
    setSidebarExpanded: (updater: (prev: boolean) => boolean) => void;
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
}


export const AppSidebarShell = ({
    navTab,
    recentTasksPaneWidth,
    aiThemeMode,
    brandInfo,
    currentIcon,
    brandSidebarName,
    switchTool,
    lang,
    maclawLLMOnline,
    agentNetRunning,
    remoteActivationStatus,
    qqBotStatus,
    telegramStatus,
    weixinStatus,
    runningTaskCount,
    t,
    gossipAllowed,
    config,
    sidebarExpanded,
    setSidebarExpanded,
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
}: AppSidebarShellProps) => (
<>
            <div style={{
                height: '30px',
                width: navTab === 'ai' ? `${SIDEBAR_NAV_RAIL_WIDTH + recentTasksPaneWidth + SIDEBAR_AI_PANE_GAP}px` : `${SIDEBAR_NAV_RAIL_WIDTH}px`,
                position: 'absolute',
                top: 0,
                left: 0,
                zIndex: 999,
                '--wails-draggable': 'drag'
            } as any}></div>

            <div className="sidebar" style={{ '--wails-draggable': 'no-drag', flexDirection: 'row', padding: 0, width: navTab === 'ai' ? `${SIDEBAR_NAV_RAIL_WIDTH + recentTasksPaneWidth + SIDEBAR_AI_PANE_GAP}px` : `${SIDEBAR_NAV_RAIL_WIDTH}px` } as any} data-ai-theme={aiThemeMode}>
                          <SidebarNavRail
                    navTab={navTab}
                    brandInfo={brandInfo}
                    currentIcon={currentIcon}
                    brandSidebarName={brandSidebarName}
                    switchTool={switchTool}
                    lang={lang}
                    maclawLLMOnline={maclawLLMOnline}
                    agentNetRunning={agentNetRunning}
                    remoteActivationStatus={remoteActivationStatus}
                    runningTaskCount={runningTaskCount}
                    t={t}
                    gossipAllowed={gossipAllowed}
                    config={config}
                    sidebarExpanded={sidebarExpanded}
                    setSidebarExpanded={setSidebarExpanded}
                />        {navTab === 'ai' && (
                    <SidebarAiPane
                        recentTasksPaneWidth={recentTasksPaneWidth}
                        lang={lang}
                        maclawLLMOnline={maclawLLMOnline}
                        agentNetRunning={agentNetRunning}
                        remoteActivationStatus={remoteActivationStatus}
                        qqBotStatus={qqBotStatus}
                        telegramStatus={telegramStatus}
                        weixinStatus={weixinStatus}
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
                        refreshRecentProjects={refreshRecentProjects}
                        taskContextMenu={taskContextMenu}
                        setTaskContextMenu={setTaskContextMenu}
                        renameTask={renameTask}
                        pinTask={pinTask}
                        hideTask={hideTask}
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
                        switchTool={switchTool}
                    />
                )}
            </div>
</>
);
