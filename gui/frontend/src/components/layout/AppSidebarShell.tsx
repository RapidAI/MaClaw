import type { MouseEvent as ReactMouseEvent } from 'react';
import { SidebarAiPane } from './SidebarAiPane';
import { SidebarNavRail } from './SidebarNavRail';
import type { SidebarCreditDisplayFormatters, SidebarCurrentProviderTokenUsage, SidebarHubCredits } from '../../types/appShell';
import type { CodingAgentProgress, CodingAgentTurnSnapshot } from '../ai/CodingAgentProgressStatus';
import type { VirtualEmployeeEntry } from '../ai/VirtualEmployeeTab';
import type { FavoriteEmployeeSlot } from './FavoriteEmployeeButtons';
import type { HistoryDiscussionSummary } from './SidebarHistorySessions';
import type { TaskManagementItem, TaskContextMenu } from './SidebarTaskManagement';
import { SIDEBAR_AI_PANE_GAP, SIDEBAR_NAV_RAIL_WIDTH } from './sidebarLayout';
import type { AssistantDarkSchemeId } from '../ai/assistantDarkSchemes';
import type { AssistantLightSchemeId } from '../ai/assistantLightSchemes';
interface AppSidebarShellProps extends SidebarCreditDisplayFormatters {
    navTab: string;
    taskManagementPaneWidth: number;
    aiThemeMode: 'light' | 'dark';
    aiDarkSchemeId: AssistantDarkSchemeId;
    aiLightSchemeId?: AssistantLightSchemeId;
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
    backgroundTaskCount?: number;
    onOpenBackgroundTasks?: () => void;
    t: (key: string) => string;
    gossipAllowed: boolean;
    config: any;
    sidebarExpanded?: boolean;
    setSidebarExpanded?: (updater: (prev: boolean) => boolean) => void;
    activeTool: string;
    toolDropdownOpen: boolean;
    setToolDropdownOpen: (updater: (prev: boolean) => boolean) => void;
    tasks: TaskManagementItem[];
    renamingTaskPath: string | null;
    setRenamingTaskPath: (path: string | null) => void;
    renameValue: string;
    setRenameValue: (value: string) => void;
    resumeTask: (projectPath: string, task?: TaskManagementItem) => Promise<void> | void;
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
    hideTask: (projectPath: string, tags?: string[]) => Promise<unknown>;
    /** Open project-tab paths; tasks with open tabs cannot be removed from the list menu. */
    openProjectTabPaths?: string[];
    openExpertTabIDs?: string[];
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
    onOpenVEConversation?: (ve: VirtualEmployeeEntry) => void;
    favoriteEmployees?: FavoriteEmployeeSlot[];
    veAuthorized?: boolean;
    digitalEmployeeFeatureStatus?: any;
    showDigitalEmployeeNavigation?: boolean;
    onOpenHistoryDiscussion?: (discussion: HistoryDiscussionSummary) => void;
    onStartVEConversation?: (veId: string) => void;
    onReorderFavorites?: (newOrder: string[]) => void;
    onRenameFavoriteEmployee?: (veId: string, name: string) => void | Promise<void>;
    onSetFavoriteEmployee?: (ve: VirtualEmployeeEntry) => void;
    onRemoveFavoriteEmployee?: (ve: VirtualEmployeeEntry) => void;
    onRemoveFavoriteEmployeeById?: (veId: string) => void;
    favoriteEmployeeIds?: string[]; favoriteEmployeeNames?: Record<string, string>;
    showCodingToolEntry?: boolean;
    showAppEntry?: boolean;
    showWorkflowEntry?: boolean;
	showUtilitiesEntry?: boolean;
    utilitiesLabel?: string;
    availableProviders?: Array<{ name: string; url: string; isHubService: boolean; model?: string; models?: string[] }>;
    onSwitchProvider?: (providerName: string) => void;
    currentModel?: string;
    modelOptions?: string[];
    modelsLoading?: boolean;
    onSwitchModel?: (modelId: string) => void;
    onOpenModelMenu?: () => void;
    moaSticky?: {
        available: boolean;
        active: boolean;
        label?: string;
        preset?: string;
        presets?: Array<{ id: string; display_name?: string; ref_count?: number; enabled?: boolean }>;
    };
    onToggleMoASticky?: (on: boolean, presetId?: string) => void;
}
export const AppSidebarShell = ({
    navTab,
    taskManagementPaneWidth,
    aiThemeMode,
    aiDarkSchemeId,
    aiLightSchemeId,
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
    backgroundTaskCount = 0,
    onOpenBackgroundTasks,
    t,
    gossipAllowed,
    config,
    sidebarExpanded,
    setSidebarExpanded,
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
    openExpertTabIDs,
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
    onOpenVEConversation,
    favoriteEmployees = [],
    veAuthorized = false,
    digitalEmployeeFeatureStatus = null,
    showDigitalEmployeeNavigation,
    onOpenHistoryDiscussion,
    onStartVEConversation,
    onReorderFavorites,
    onRenameFavoriteEmployee,
    onSetFavoriteEmployee,
    onRemoveFavoriteEmployee,
    onRemoveFavoriteEmployeeById,
    favoriteEmployeeIds = [], favoriteEmployeeNames = {},
    showCodingToolEntry = false,
    showAppEntry = false,
    showWorkflowEntry = true,
	showUtilitiesEntry = true,
    utilitiesLabel,
    availableProviders = [],
    onSwitchProvider,
    currentModel = '',
    modelOptions = [],
    modelsLoading = false,
    onSwitchModel,
    onOpenModelMenu,
    moaSticky,
    onToggleMoASticky,
}: AppSidebarShellProps) => (
<>
            <div style={{
                height: '30px',
                width: navTab === 'ai' ? `${SIDEBAR_NAV_RAIL_WIDTH + taskManagementPaneWidth + SIDEBAR_AI_PANE_GAP}px` : `${SIDEBAR_NAV_RAIL_WIDTH}px`,
                position: 'absolute',
                top: 0,
                left: 0,
                zIndex: 999,
                '--wails-draggable': 'drag'
            } as any}></div>

            <div className="sidebar" style={{ '--wails-draggable': 'no-drag', flexDirection: 'row', padding: 0, width: navTab === 'ai' ? `${SIDEBAR_NAV_RAIL_WIDTH + taskManagementPaneWidth + SIDEBAR_AI_PANE_GAP}px` : `${SIDEBAR_NAV_RAIL_WIDTH}px` } as any} data-ai-theme={aiThemeMode} data-ai-dark-scheme={aiThemeMode === 'dark' ? aiDarkSchemeId : undefined} data-ai-light-scheme={aiThemeMode === 'light' && aiLightSchemeId && aiLightSchemeId !== 'default' ? aiLightSchemeId : undefined}>
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
                    onRemoveFavorite={onRemoveFavoriteEmployeeById || (() => {})}
                    onRenameFavorite={onRenameFavoriteEmployee || (() => {})}
                    showAppEntry={showAppEntry}
                    showWorkflowEntry={showWorkflowEntry}
					showUtilitiesEntry={showUtilitiesEntry}
                    utilitiesLabel={utilitiesLabel}
                />        {navTab === 'ai' && (
                    <SidebarAiPane
                        taskManagementPaneWidth={taskManagementPaneWidth}
                        lang={lang}
                        aiThemeMode={aiThemeMode}
                        maclawLLMOnline={maclawLLMOnline}
                        showLansenger={showLansenger}
                        remoteActivationStatus={remoteActivationStatus}
                        qqBotStatus={qqBotStatus}
                        telegramStatus={telegramStatus}
                        weixinStatus={weixinStatus}
                        lansengerStatus={lansengerStatus}
                        backgroundTaskCount={backgroundTaskCount}
                        onOpenBackgroundTasks={onOpenBackgroundTasks}
                        config={config}
                        activeTool={activeTool}
                        toolDropdownOpen={toolDropdownOpen}
                        setToolDropdownOpen={setToolDropdownOpen}
                        tasks={tasks}
                        renamingTaskPath={renamingTaskPath}
                        setRenamingTaskPath={setRenamingTaskPath}
                        renameValue={renameValue}
                        setRenameValue={setRenameValue}
                        resumeTask={resumeTask}
                        continueWorkflowProject={continueWorkflowProject}
                        assistantReady={assistantReady}
                        onTaskSwitchBlocked={onTaskSwitchBlocked}
                        createTask={createTask}
                        refreshTasks={refreshTasks}
                        taskContextMenu={taskContextMenu}
                        setTaskContextMenu={setTaskContextMenu}
                        renameTask={renameTask}
                        pinTask={pinTask}
                        hideTask={hideTask}
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
                        openServiceRedeemPage={openServiceRedeemPage} openLLMSettingsPage={openLLMSettingsPage} openHubCardStorePage={openHubCardStorePage}
                        codingAgentProgress={codingAgentProgress}
                        codingAgentTurnSnapshot={codingAgentTurnSnapshot}
                        handleTaskManagementResizeStart={handleTaskManagementResizeStart}
                        isTaskManagementResizing={isTaskManagementResizing}
                        switchTool={switchTool}
                        onOpenVEConversation={onOpenVEConversation}
                        onSetFavoriteEmployee={onSetFavoriteEmployee}
                        onRemoveFavoriteEmployee={onRemoveFavoriteEmployee}
                        favoriteEmployeeIds={favoriteEmployeeIds} favoriteEmployeeNames={favoriteEmployeeNames} onRenameEmployee={(ve, name) => onRenameFavoriteEmployee?.(String(ve.machine_id || ve.id || '').trim() || ve.id, name)}
                        showCodingToolEntry={showCodingToolEntry}
                        digitalEmployeeFeatureStatus={digitalEmployeeFeatureStatus}
                        showDigitalEmployeeNavigation={showDigitalEmployeeNavigation}
                        onOpenHistoryDiscussion={onOpenHistoryDiscussion}
                        availableProviders={availableProviders}
                        onSwitchProvider={onSwitchProvider}
                        currentModel={currentModel}
                        modelOptions={modelOptions}
                        modelsLoading={modelsLoading}
                        onSwitchModel={onSwitchModel}
                        onOpenModelMenu={onOpenModelMenu}
                        moaSticky={moaSticky}
                        onToggleMoASticky={onToggleMoASticky}
                    />
                )}
            </div>
</>
);
