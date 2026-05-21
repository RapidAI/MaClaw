import type { MouseEvent as ReactMouseEvent } from 'react';
import { SidebarAiPane } from './SidebarAiPane';
import { SidebarNavRail } from './SidebarNavRail';
import type { SidebarHubCredits } from '../../types/appShell';
import type { CodingAgentProgress, CodingAgentTurnSnapshot } from '../ai/CodingAgentProgressStatus';
import type { VirtualEmployeeEntry } from '../ai/VirtualEmployeeTab';
import type { FavoriteEmployeeSlot } from './FavoriteEmployeeButtons';
import type { HistoryDiscussionSummary } from './SidebarHistorySessions';
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
    showLansenger?: boolean;
    remoteActivationStatus: any;
    qqBotStatus: string;
    telegramStatus: string;
    weixinStatus: string;
    lansengerStatus: string;
    runningTaskCount: number;
    t: (key: string) => string;
    gossipAllowed: boolean;
    config: any;
    sidebarExpanded?: boolean;
    setSidebarExpanded?: (updater: (prev: boolean) => boolean) => void;
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
    onOpenVEConversation?: (ve: VirtualEmployeeEntry) => void;
    favoriteEmployees?: FavoriteEmployeeSlot[];
    veAuthorized?: boolean;
    digitalEmployeeFeatureStatus?: any;
    showDigitalEmployeeNavigation?: boolean;
    onOpenHistoryDiscussion?: (discussion: HistoryDiscussionSummary) => void;
    onStartVEConversation?: (veId: string) => void;
    onReorderFavorites?: (newOrder: string[]) => void;
    onSetFavoriteEmployee?: (ve: VirtualEmployeeEntry) => void;
    onRemoveFavoriteEmployee?: (ve: VirtualEmployeeEntry) => void;
    favoriteEmployeeIds?: string[];
    showCodingToolEntry?: boolean;
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
    showLansenger = false,
    remoteActivationStatus,
    qqBotStatus,
    telegramStatus,
    weixinStatus,
    lansengerStatus,
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
    onOpenVEConversation,
    favoriteEmployees = [],
    veAuthorized = false,
    digitalEmployeeFeatureStatus = null,
    showDigitalEmployeeNavigation,
    onOpenHistoryDiscussion,
    onStartVEConversation,
    onReorderFavorites,
    onSetFavoriteEmployee,
    onRemoveFavoriteEmployee,
    favoriteEmployeeIds = [],
    showCodingToolEntry = false,
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
                    remoteActivationStatus={remoteActivationStatus}
                    runningTaskCount={runningTaskCount}
                    t={t}
                    gossipAllowed={gossipAllowed}
                    config={config}
                    favoriteEmployees={favoriteEmployees}
                    veAuthorized={veAuthorized}
                    onStartVEConversation={onStartVEConversation || (() => {})}
                    onReorderFavorites={onReorderFavorites || (() => {})}
                />        {navTab === 'ai' && (
                    <SidebarAiPane
                        recentTasksPaneWidth={recentTasksPaneWidth}
                        lang={lang}
                        aiThemeMode={aiThemeMode}
                        maclawLLMOnline={maclawLLMOnline}
                        showLansenger={showLansenger}
                        remoteActivationStatus={remoteActivationStatus}
                        qqBotStatus={qqBotStatus}
                        telegramStatus={telegramStatus}
                        weixinStatus={weixinStatus}
                        lansengerStatus={lansengerStatus}
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
                        assistantReady={assistantReady}
                        onRecentTaskSwitchBlocked={onRecentTaskSwitchBlocked}
                        createRecentTask={createRecentTask}
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
                        codingAgentProgress={codingAgentProgress}
                        codingAgentTurnSnapshot={codingAgentTurnSnapshot}
                        handleRecentTasksResizeStart={handleRecentTasksResizeStart}
                        isRecentTasksResizing={isRecentTasksResizing}
                        switchTool={switchTool}
                        onOpenVEConversation={onOpenVEConversation}
                        onSetFavoriteEmployee={onSetFavoriteEmployee}
                        onRemoveFavoriteEmployee={onRemoveFavoriteEmployee}
                        favoriteEmployeeIds={favoriteEmployeeIds}
                        showCodingToolEntry={showCodingToolEntry}
                        digitalEmployeeFeatureStatus={digitalEmployeeFeatureStatus}
                        showDigitalEmployeeNavigation={showDigitalEmployeeNavigation}
                        onOpenHistoryDiscussion={onOpenHistoryDiscussion}
                    />
                )}
            </div>
</>
);


