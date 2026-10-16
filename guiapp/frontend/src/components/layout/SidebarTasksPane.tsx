import type { CSSProperties } from 'react';
import { SidebarTaskManagement, type SidebarTaskManagementProps } from './SidebarTaskManagement';

/** Content slot shared by the middle panes; also used by the shell wrapper. */
export const middleContentSlotStyle: CSSProperties = {
    flex: 1,
    minHeight: 0,
    overflow: 'hidden',
    display: 'flex',
    flexDirection: 'column',
};

/** Same box as the other middle panes, hidden instead of unmounted on other tabs. */
const tasksPaneStyle = (active: boolean): CSSProperties => ({ ...middleContentSlotStyle, display: active ? 'flex' : 'none' });

type SidebarTasksPaneProps = Pick<SidebarTaskManagementProps,
    | 'lang' | 'themeMode' | 'tasks' | 'tasksLoading' | 'cloudTasksLoading'
    | 'renamingTaskPath' | 'setRenamingTaskPath' | 'renameValue' | 'setRenameValue'
    | 'resumeTask' | 'continueWorkflowProject' | 'assistantReady' | 'onTaskSwitchBlocked'
    | 'createTask' | 'onCreateExpertTask' | 'refreshTasks' | 'taskContextMenu' | 'setTaskContextMenu'
    | 'renameTask' | 'pinTask' | 'hideTask' | 'activateTask'
    | 'openProjectTabPaths' | 'openProjectTabIdentities' | 'openExpertTabIDs'
    | 'activeAssistantTask' | 'activeAssistantTaskRunning' | 'busyTaskRuns'
    | 'showCloudWorkspaceManagement' | 'showCloudWorkspaceCreation' | 'restoreCloudWorkspaceTasks'
    | 'taskListVisible'>;

/** Tasks middle pane: owns the keep-mounted wrapper and the task-management surface. */
export function SidebarTasksPane(props: SidebarTasksPaneProps) {
    return (
        <div
            data-testid="sidebar-middle-pane-tasks"
            // Keep mounted (hidden) on other middle tabs so welcome coding events still open create dialog.
            style={tasksPaneStyle(props.taskListVisible === true)}
        >
            <SidebarTaskManagement {...props} />
        </div>
    );
}
