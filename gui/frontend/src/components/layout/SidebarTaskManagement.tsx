import { useEffect, useMemo, useRef, useState, type CSSProperties } from 'react';
import { createPortal } from 'react-dom';
import { CloudWorkspaceEntitlement, CreateCloudWorkspace, DeleteCloudWorkspace, ForceDeleteCloudWorkspace, GetProjectScene, GetRemoteCodingTaskMeta, PrepareCloudWorkspace, RenameCloudWorkspace, RestoreCloudWorkspace, RestoreCloudWorkspaceTasks, SelectWorkingDir, TestRemoteSSHConnection, UpdateRemoteCodingTaskMeta } from '../../../wailsjs/go/main/App';
import { EventsEmit } from '../../../wailsjs/runtime';
import { EVENT_OPEN_CREATE_CODING_TASK, EVENT_PROJECT_TASK_CLOSED, type OpenCreateCodingTaskDetail } from '../../constants/events';
import { localizeText } from '../../i18n';
import { ProjectSearchIcon } from '../ai/ProjectSearchIcon';
import type { ProjectSceneDetail } from '../ai/ProjectSceneDetailPanel';
import { agentModeFromTaskTags, cloudWorkspaceIdFromPath, cloudWorkspaceIdFromTags, CODING_TASK_COMMAND_MAX_LEN, isCloudWorkspaceTask, isPureCodingTaskTags, isRemoteMaintenanceTaskTags, rememberCloudWorkspaceDisplayNames, REVEAL_CLOUD_WORKSPACE_FILES_EVENT, remoteCodingMetaFromTaskTags, remoteHostFromTaskTags, type PureCodingAgentMode } from '../ai/codingTaskMode';
import { coerceActiveAssistantTask, expertIDFromTaskTags, normalizeProjectSessionPath, type ActiveAssistantTaskIdentity } from '../ai/aiAssistantPanelSessionUtils';
import { extractErrorMessage } from '../ai/participantAddError';
import { normalizeWorkflowStatus, WorkflowStatus } from '../ai/workflowStatus';
import { useDialog } from '../CustomDialog';
import { SidebarTaskEvidencePanel } from './SidebarTaskEvidencePanel';

export type TaskManagementItem = {
    id?: string;
    name?: string;
    project_path: string;
    workflow_type?: string;
    active_workflow?: {
        id?: string;
        type?: string;
        phase?: string;
        status?: string;
        project_path?: string;
        pending_review?: boolean;
    };
    preview?: string;
    tags?: string[];
    working_dir?: string;
    created_at?: string;
    last_activity?: string;
    pinned?: boolean;
    has_output?: boolean;
};

export type TaskContextMenu = {
    x: number;
    y: number;
    projectPath: string;
    name: string;
    pinned: boolean;
    /** True when this task is a remote pure-coding environment. */
    isRemoteCoding?: boolean;
    tags?: string[];
    workingDir?: string;
} | null;

/** Re-export for task sidebar consumers that already import from this module. */
export { expertIDFromTaskTags };

type TaskIconKind = 'pin' | 'reference' | 'coding' | 'remote_coding' | 'cloud_workspace' | 'task';

type TaskWorkflowStatusTone = 'info' | 'warning' | 'danger' | 'success' | 'neutral';

type TaskWorkflowStatus = {
    label: string;
    detail?: string;
    tone: TaskWorkflowStatusTone;
};

const TASK_WORKFLOW_STATUS_COLORS: Record<TaskWorkflowStatusTone, { border: string; color: string; background: string }> = {
    info: { border: 'color-mix(in srgb, var(--theme-primary) 42%, transparent)', color: 'var(--theme-primary)', background: 'color-mix(in srgb, var(--theme-primary) 8%, transparent)' },
    warning: { border: 'color-mix(in srgb, #d97706 48%, transparent)', color: '#a16207', background: 'color-mix(in srgb, #f59e0b 12%, transparent)' },
    danger: { border: 'color-mix(in srgb, #dc2626 46%, transparent)', color: '#b91c1c', background: 'color-mix(in srgb, #dc2626 10%, transparent)' },
    success: { border: 'color-mix(in srgb, #16a34a 44%, transparent)', color: '#15803d', background: 'color-mix(in srgb, #22c55e 10%, transparent)' },
    neutral: { border: 'color-mix(in srgb, var(--theme-text-muted) 42%, transparent)', color: 'var(--theme-text-muted)', background: 'color-mix(in srgb, var(--theme-text-muted) 9%, transparent)' },
};

const TASK_ICON_PROPS = {
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.7,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
};

const CREATE_TASK_ICON_PROPS = {
    fill: 'none',
    stroke: 'currentColor',
    strokeWidth: 1.9,
    strokeLinecap: 'round' as const,
    strokeLinejoin: 'round' as const,
};

const TASK_HEADER_ACTION_BUTTON_STYLE: CSSProperties = {
    width: '22px',
    height: '22px',
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    flexShrink: 0,
    border: '1px solid color-mix(in srgb, var(--theme-primary) 44%, var(--theme-border))',
    borderRadius: '6px',
    background: 'color-mix(in srgb, var(--theme-primary) 13%, var(--theme-surface))',
    color: 'var(--theme-primary)',
    lineHeight: 1,
    padding: 0,
    boxShadow: 'inset 0 0 0 1px color-mix(in srgb, var(--theme-primary) 10%, transparent)',
};

const taskHeaderActionButtonStyle = (busy = false): CSSProperties => ({
    ...TASK_HEADER_ACTION_BUTTON_STYLE,
    cursor: busy ? 'default' : 'pointer',
    opacity: busy ? 0.55 : 1,
});

const TASK_HEADER_ICON_STYLE: CSSProperties = { display: 'block', flexShrink: 0 };

/** Primary list icon: cloud workspaces take priority so the environment stays visible. */
const taskIconKindForProject = (proj: TaskManagementItem): TaskIconKind => {
    // Cloud workspace tasks get a dedicated marker so they remain obvious in
    // the list even when they also carry coding/remote tags.
    if (isCloudWorkspaceTask(proj)) return 'cloud_workspace';
    const mode = agentModeFromTaskTags(proj.tags);
    if (mode === 'remote_coding_dev') return 'remote_coding';
    if (mode === 'coding_dev') return 'coding';
    if (proj.pinned) return 'pin';
    if (proj.tags?.includes('forked_task')) return 'reference';
    return 'task';
};

const isPureCodingTask = (proj: TaskManagementItem): boolean => isPureCodingTaskTags(proj.tags);

const isRemoteCodingTask = (proj: TaskManagementItem): boolean =>
    agentModeFromTaskTags(proj.tags) === 'remote_coding_dev';

const isRemoteMaintenanceTask = (proj: TaskManagementItem): boolean =>
    isRemoteCodingTask(proj) && isRemoteMaintenanceTaskTags(proj.tags);

const taskIconLabel = (kind: TaskIconKind, lang: string, maintenance = false) => {
    if (kind === 'pin') return textForLang(lang, 'Pinned task', '\u7f6e\u9876\u4efb\u52a1', '\u7f6e\u9802\u4efb\u52d9');
    if (kind === 'reference') return textForLang(lang, 'Referenced task', '\u5f15\u7528\u4efb\u52a1', '\u5f15\u7528\u4efb\u52d9');
    if (kind === 'remote_coding') return maintenance
        ? textForLang(lang, 'Remote maintenance', '\u8fdc\u7a0b\u7ef4\u62a4', '\u9060\u7aef\u7dad\u8b77')
        : textForLang(lang, 'Remote pure coding environment', '\u8fdc\u7a0b\u7eaf\u7f16\u7a0b\u73af\u5883', '\u9060\u7aef\u7d14\u7a0b\u5f0f\u74b0\u5883');
    if (kind === 'cloud_workspace') return textForLang(lang, 'Cloud workspace task', '\u4e91\u7aef\u5de5\u4f5c\u533a\u4efb\u52a1', '\u96f2\u7aef\u5de5\u4f5c\u5340\u4efb\u52d9');
    if (kind === 'coding') return textForLang(lang, 'Local pure coding environment', '\u672c\u5730\u7eaf\u7f16\u7a0b\u73af\u5883', '\u672c\u6a5f\u7d14\u7a0b\u5f0f\u74b0\u5883');
    return textForLang(lang, 'Task', '\u4efb\u52a1', '\u4efb\u52d9');
};

const pureCodingBadgeLabel = (proj: TaskManagementItem, lang: string) => {
    if (isRemoteCodingTask(proj)) {
        const host = remoteHostFromTaskTags(proj.tags);
        const base = isRemoteMaintenanceTask(proj)
            ? textForLang(lang, 'Remote maintenance', '\u8fdc\u7a0b\u7ef4\u62a4', '\u9060\u7aef\u7dad\u8b77')
            : textForLang(lang, 'Remote coding', '\u8fdc\u7a0b\u7f16\u7a0b', '\u9060\u7aef\u7a0b\u5f0f');
        return host ? `${base} · ${host}` : base;
    }
    if (agentModeFromTaskTags(proj.tags) === 'coding_dev') {
        return textForLang(lang, 'Pure coding', '\u7eaf\u7f16\u7a0b', '\u7d14\u7a0b\u5f0f');
    }
    return '';
};

const cloudWorkspaceBadgeLabel = (proj: TaskManagementItem, lang: string) => {
    if (!isCloudWorkspaceTask(proj)) return '';
    return textForLang(lang, 'Cloud workspace', '\u4e91\u7aef\u5de5\u4f5c\u533a', '\u96f2\u7aef\u5de5\u4f5c\u5340');
};

function readableWorkflowPhase(phase?: string): string {
    const value = (phase || '').trim();
    if (!value) return '';
    return value
        .replace(/[_-]+/g, ' ')
        .replace(/\s+/g, ' ')
        .replace(/\b\w/g, (letter) => letter.toUpperCase());
}

/** The sidebar has workflow snapshots, not live agent runtime data. */
export function workflowStatusForTask(
    workflow: TaskManagementItem['active_workflow'],
    lang: string,
): TaskWorkflowStatus | null {
    if (!workflow) return null;
    const phase = readableWorkflowPhase(workflow.phase);
    const status = normalizeWorkflowStatus(workflow.status);
    const detail = phase || undefined;
    // Terminal status wins over any stale pending-review bit persisted by an
    // earlier phase update.
    if (status === WorkflowStatus.Cancelled) {
        return { label: textForLang(lang, 'Cancelled', '已取消', '已取消'), detail, tone: 'neutral' };
    }
    if (status === WorkflowStatus.Completed) {
        return { label: textForLang(lang, 'Completed', '已完成', '已完成'), detail, tone: 'success' };
    }
    if (workflow.pending_review) {
        return { label: textForLang(lang, 'Review needed', '待审核', '待審核'), detail, tone: 'warning' };
    }
    if (/(fail|error|blocked)/.test(String(workflow.status || '').trim().toLowerCase())) {
        return { label: textForLang(lang, 'Needs attention', '需要处理', '需要處理'), detail, tone: 'danger' };
    }
    return { label: textForLang(lang, 'In progress', '进行中', '進行中'), detail, tone: 'info' };
}

/** Stable, explicit creation timestamp for a user-managed task. */
export function taskCreationLabel(value: string | undefined, lang: string): string {
    const timestamp = Date.parse(value || '');
    if (!Number.isFinite(timestamp)) return '';
    const date = new Date(timestamp);
    const pad = (part: number) => String(part).padStart(2, '0');
    const dateText = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
    return textForLang(lang, `Created ${dateText}`, `创建于 ${dateText}`, `建立於 ${dateText}`);
}

const TaskTypeIcon = ({ kind, lang, maintenance = false }: { kind: TaskIconKind; lang: string; maintenance?: boolean }) => {
    const label = taskIconLabel(kind, lang, maintenance);
    const iconColor = kind === 'cloud_workspace'
        ? 'color-mix(in srgb, #8b5cf6 78%, var(--theme-text-primary))'
        : 'var(--theme-text-muted)';

    return (
        <span
            aria-label={label}
            title={label}
            data-testid={kind === 'cloud_workspace' ? 'task-cloud-workspace-icon' : undefined}
            style={{ flexShrink: 0, width: '24px', height: '18px', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', color: iconColor, opacity: 0.92 }}
        >
            <svg width="15" height="15" viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: 'block' }}>
                {kind === 'pin' && (
                    <>
                        <path {...TASK_ICON_PROPS} d="M15 4 20 9" />
                        <path {...TASK_ICON_PROPS} d="M14 10 8 16" />
                        <path {...TASK_ICON_PROPS} d="M5 19 8 16" />
                        <path {...TASK_ICON_PROPS} d="M8.5 5.5 18.5 15.5" />
                        <path {...TASK_ICON_PROPS} d="M10 4 20 14" />
                    </>
                )}
                {kind === 'reference' && (
                    <>
                        <path {...TASK_ICON_PROPS} d="M7 4h8l4 4v12H7a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z" />
                        <path {...TASK_ICON_PROPS} d="M15 4v5h5" />
                        <path {...TASK_ICON_PROPS} d="M9 13h6" />
                        <path {...TASK_ICON_PROPS} d="M9 17h4" />
                    </>
                )}
                {kind === 'coding' && (
                    <>
                        <path {...TASK_ICON_PROPS} d="M8 8 4.5 12 8 16" />
                        <path {...TASK_ICON_PROPS} d="m16 8 3.5 4L16 16" />
                        <path {...TASK_ICON_PROPS} d="m13 7-2 10" />
                    </>
                )}
                {kind === 'remote_coding' && (
                    <>
                        <rect {...TASK_ICON_PROPS} x="3" y="5" width="18" height="12" rx="2" />
                        <path {...TASK_ICON_PROPS} d="M8 9h3M8 12h5" />
                        <path {...TASK_ICON_PROPS} d="m15 9 2 1.5L15 12" />
                    </>
                )}
                {kind === 'cloud_workspace' && (
                    <>
                        <path {...TASK_ICON_PROPS} d="M7.5 18.5h10a4 4 0 1 0-.35-7.98A6 6 0 1 0 7.5 18.5z" />
                        <path {...TASK_ICON_PROPS} d="M12 13v5" />
                        <path {...TASK_ICON_PROPS} d="m9.5 15.5 2.5 2.5 2.5-2.5" />
                    </>
                )}
                {kind === 'task' && (
                    <>
                        <path {...TASK_ICON_PROPS} d="M8 6h11" />
                        <path {...TASK_ICON_PROPS} d="M8 12h11" />
                        <path {...TASK_ICON_PROPS} d="M8 18h11" />
                        <path {...TASK_ICON_PROPS} d="m3.5 6 1 1 2-2" />
                        <path {...TASK_ICON_PROPS} d="m3.5 12 1 1 2-2" />
                        <path {...TASK_ICON_PROPS} d="m3.5 18 1 1 2-2" />
                    </>
                )}
            </svg>
        </span>
    );
};

const CreateTaskIcon = () => (
    <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={TASK_HEADER_ICON_STYLE}>
        <path {...CREATE_TASK_ICON_PROPS} d="M7 4h7l4 4v12H7a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z" />
        <path {...CREATE_TASK_ICON_PROPS} d="M14 4v5h5" />
        <path {...CREATE_TASK_ICON_PROPS} d="M9 14h6" />
        <path {...CREATE_TASK_ICON_PROPS} d="M12 11v6" />
    </svg>
);

const CloudComputingIcon = ({ size = 16 }: { size?: number } = {}) => (
    <svg width={size} height={size} viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={TASK_HEADER_ICON_STYLE}>
        <path {...CREATE_TASK_ICON_PROPS} d="M17.5 19H9a7 7 0 1 1 6.71-9h1.79a4.5 4.5 0 1 1 0 9z" />
    </svg>
);

const SaveTaskIcon = () => (
    <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={TASK_HEADER_ICON_STYLE}>
        <path {...CREATE_TASK_ICON_PROPS} d="M6 4h10l2 2v14H6z" />
        <path {...CREATE_TASK_ICON_PROPS} d="M9 4v6h6V4" />
        <path {...CREATE_TASK_ICON_PROPS} d="M9 16h6" />
    </svg>
);

function emitProjectTaskClosed(projectPath: string) {
    try {
        EventsEmit(EVENT_PROJECT_TASK_CLOSED, projectPath);
    } catch (error) {
        console.warn('[SidebarTaskManagement] project close event emit failed:', error);
    }
}

function requestSaveCurrentChatAsTask() {
    window.dispatchEvent(new CustomEvent('ai-save-current-chat-as-task'));
}

type SidebarTaskManagementProps = {
    lang: string;
    themeMode?: 'light' | 'dark';
    tasks: TaskManagementItem[];
    renamingTaskPath: string | null;
    setRenamingTaskPath: (path: string | null) => void;
    renameValue: string;
    setRenameValue: (value: string) => void;
    resumeTask: (projectPath: string, task?: TaskManagementItem) => Promise<void> | void;
    continueWorkflowProject?: (projectPath: string) => Promise<void> | void;
    assistantReady?: boolean;
    onTaskSwitchBlocked?: () => void;
    /**
     * mode="coding_dev" | "remote_coding_dev" creates a programming task.
     * remote is required when mode is remote_coding_dev (SSH credentials).
     */
    createTask: (
        name: string,
        workingDir?: string,
        mode?: 'coding_dev' | 'remote_coding_dev',
        remote?: { host: string; port: number; user: string; password: string; workDir: string; safety?: 'diagnosis' },
        workspaceId?: string,
    ) => Promise<void> | void;
    refreshTasks: () => void;
    taskContextMenu: TaskContextMenu;
    setTaskContextMenu: (menu: TaskContextMenu) => void;
    renameTask: (projectPath: string, name: string) => Promise<unknown>;
    pinTask: (projectPath: string, pinned: boolean) => Promise<unknown>;
    hideTask: (projectPath: string, tags?: string[]) => Promise<unknown>;
    /**
     * Project paths that currently have an open assistant tab.
     * Open tabs cannot be removed from the task list context menu.
     */
    openProjectTabPaths?: string[];
    /** Expert IDs currently open in the assistant; their durable rows cannot be removed. */
    openExpertTabIDs?: string[];
    /** Currently visible assistant tab. Null/empty means no task-list row is selected. */
    activeAssistantTask?: ActiveAssistantTaskIdentity | null;
    /** False while the sidebar is showing employees/history so scroll waits until the list is shown. */
    taskListVisible?: boolean;
};

/** True when projectPath matches an open project tab (path-normalized). Exported for tests. */
export function isProjectTabOpen(projectPath: string, openProjectTabPaths?: Iterable<string> | null): boolean {
    const target = normalizeProjectSessionPath(projectPath);
    if (!target || !openProjectTabPaths) return false;
    // openProjectTabPaths is already normalized when published by AIAssistantPanel,
    // but re-normalize for callers that pass raw/mixed separators (tests, future callers).
    for (const p of openProjectTabPaths) {
        if (normalizeProjectSessionPath(p) === target) return true;
    }
    return false;
}

/** True when this durable task row is the currently visible AI assistant tab. */
export function isActiveTaskRow(
    proj: Pick<TaskManagementItem, 'project_path' | 'tags' | 'working_dir'>,
    active?: ActiveAssistantTaskIdentity | null,
): boolean {
    const canonical = coerceActiveAssistantTask(active);
    if (!canonical) return false;
    const expertID = expertIDFromTaskTags(proj.tags);
    if (canonical.expertId) return expertID === canonical.expertId;
    // Expert rows stay bound to expert tabs even if a project tab shares a path.
    if (expertID) return false;
    const activePath = canonical.projectPath || '';
    const target = normalizeProjectSessionPath(proj.project_path);
    if (target && target === activePath) return true;
    const workDir = normalizeProjectSessionPath(proj.working_dir);
    if (workDir && workDir === activePath) return true;
    // Cloud resume rebinds the tab onto a cache path that may differ from the
    // durable list row; match by workspace id so the highlight still lands.
    const rowWorkspaceId = cloudWorkspaceIdFromTags(proj.tags)
        || cloudWorkspaceIdFromPath(proj.project_path)
        || cloudWorkspaceIdFromPath(proj.working_dir);
    const activeWorkspaceId = canonical.cloudWorkspaceId || cloudWorkspaceIdFromPath(activePath);
    return !!rowWorkspaceId && rowWorkspaceId === activeWorkspaceId;
}

const textForLang = localizeText;

type TaskContextMenuItem = {
    label: string;
    icon: string;
    action: () => void | Promise<void>;
    disabled?: boolean;
    title?: string;
    testId?: string;
};

function buildTaskContextMenuItems(opts: {
    lang: string;
    menu: NonNullable<TaskContextMenu>;
    openProjectTabPaths?: string[];
    openExpertTabIDs?: string[];
    setRenamingTaskPath: (path: string | null) => void;
    setRenameValue: (value: string) => void;
    setTaskContextMenu: (menu: TaskContextMenu) => void;
    pinTask: (projectPath: string, pinned: boolean) => Promise<unknown>;
    removeTask: (projectPath: string, tags?: string[]) => Promise<void>;
    refreshTasks: () => void;
    openEditRemoteDialog: (projectPath: string, name: string, tags?: string[]) => void | Promise<void>;
    browseCloudWorkspace?: (workspaceId: string, projectPath?: string, tags?: string[], workingDir?: string) => void | Promise<void>;
}): TaskContextMenuItem[] {
    const {
        lang, menu, openProjectTabPaths, openExpertTabIDs, setRenamingTaskPath, setRenameValue,
        setTaskContextMenu, pinTask, removeTask, refreshTasks, openEditRemoteDialog, browseCloudWorkspace,
    } = opts;
    const expertID = expertIDFromTaskTags(menu.tags);
    const tabOpen = isProjectTabOpen(menu.projectPath, openProjectTabPaths)
        || (!!expertID && (openExpertTabIDs || []).includes(expertID));
    const removeBlockedHint = textForLang(
        lang,
        'Close the task tab before removing it from the list.',
        '请先关闭任务标签页，再从列表删除。',
        '請先關閉任務標籤頁，再從列表刪除。',
    );
    const items: TaskContextMenuItem[] = [
        {
            label: textForLang(lang, 'Rename', '重命名', '重命名'),
            icon: 'edit',
            action: () => {
                setRenamingTaskPath(menu.projectPath);
                setRenameValue(menu.name);
                setTaskContextMenu(null);
            },
        },
    ];
    const cloudWorkspaceId = cloudWorkspaceIdFromTags(menu.tags);
    if (cloudWorkspaceId) {
        items.push({
            label: textForLang(lang, 'Browse', '浏览', '瀏覽'),
            icon: 'DIR',
            testId: 'task-context-browse-cloud',
            title: textForLang(lang, 'Pull remote files and open the in-app cloud file browser', '拉取远程文件并在右侧浏览区打开', '拉取遠端檔案並在右側瀏覽區開啟'),
            action: () => {
                setTaskContextMenu(null);
                void browseCloudWorkspace?.(cloudWorkspaceId, menu.projectPath, menu.tags, menu.workingDir);
            },
        });
    }
    if (menu.isRemoteCoding) {
        items.push({
            label: textForLang(lang, 'Edit remote SSH…', '编辑远程 SSH…', '編輯遠端 SSH…'),
            icon: 'SSH',
            testId: 'task-context-edit-remote-ssh',
            action: () => { void openEditRemoteDialog(menu.projectPath, menu.name, menu.tags); },
        });
    }
    items.push({
        label: menu.pinned
            ? textForLang(lang, 'Unpin', '取消置顶', '取消置頂')
            : textForLang(lang, 'Pin', '置顶', '置頂'),
        icon: 'PIN',
        action: async () => {
            await pinTask(menu.projectPath, !menu.pinned);
            refreshTasks();
            setTaskContextMenu(null);
        },
    });
    items.push({
        label: textForLang(lang, 'Remove', '删除', '刪除'),
        icon: 'X',
        testId: 'task-context-remove',
        disabled: tabOpen,
        title: tabOpen ? removeBlockedHint : undefined,
        action: async () => {
            // Re-check at click time in case a tab opened while the menu was visible.
            const expertStillOpen = !!expertID && (openExpertTabIDs || []).includes(expertID);
            if (isProjectTabOpen(menu.projectPath, openProjectTabPaths) || expertStillOpen) {
                return;
            }
            // Keep the existing one-argument call for ordinary tasks so legacy
            // callers/mocks retain their contract; expert rows need tags for
            // the guarded hide path to identify their open tab.
            // Close the menu first so the in-row progress state is visible
            // immediately while the backend removes the task.
            setTaskContextMenu(null);
            if (expertID) {
                await removeTask(menu.projectPath, menu.tags);
            } else {
                await removeTask(menu.projectPath);
            }
        },
    });
    return items;
}
// Must sit above existing fixed dropdowns/overlays that use z-index 99999.
const TASK_CREATE_DIALOG_Z_INDEX = 100000;

const getPortalThemeMode = (themeMode?: 'light' | 'dark') => (
    themeMode || document.getElementById('App')?.getAttribute('data-ai-theme') || undefined
);

const getPortalDarkScheme = () => (
    document.getElementById('App')?.getAttribute('data-ai-dark-scheme') || undefined
);

const getPortalLightScheme = () => (
    document.getElementById('App')?.getAttribute('data-ai-light-scheme') || undefined
);

const normalizeTaskCommandInput = (value?: string | null) => {
    // Preserve newlines (multi-line task commands), only collapse horizontal whitespace per line
    const lines = (value || '').split('\n').map(line => line.trim().replace(/[ \t]+/g, ' '));
    // Remove leading/trailing empty lines, collapse 3+ consecutive empty lines to 2
    const trimmed = lines.join('\n').trim().replace(/\n{3,}/g, '\n\n');
    // Limit characters (UTF-16 code units, consistent with HTML maxLength)
    return trimmed.slice(0, CODING_TASK_COMMAND_MAX_LEN);
};

/** An empty port means the conventional SSH port; malformed input must not silently become 22. */
const parseRemotePort = (value: string): number | null => {
    const trimmed = value.trim();
    if (!trimmed) return 22;
    if (!/^\d+$/.test(trimmed)) return null;
    const port = Number(trimmed);
    return Number.isInteger(port) && port > 0 && port < 65536 ? port : null;
};

const generatedTaskTitle = (lang: string, mode: '' | 'coding_dev' | 'remote_coding_dev', cloud = false) => {
    if (cloud) return textForLang(lang, 'New cloud workspace task', '新建云端工作区任务', '新建雲端工作區任務');
    if (mode === 'coding_dev') return textForLang(lang, 'New local coding task', '新建本地编程任务', '新建本機程式任務');
    if (mode === 'remote_coding_dev') return textForLang(lang, 'New remote coding task', '新建远程编程任务', '新建遠端程式任務');
    return textForLang(lang, 'New task', '新建任务', '新建任務');
};

type CloudWorkspaceLease = {
    held?: boolean;
    machine_id?: string;
    machine_name?: string;
    is_self?: boolean;
    expires_at?: string;
};

type CloudWorkspaceRow = {
    id?: string;
    name?: string;
    used_bytes?: number;
    updated_at?: string;
    lease_in_use?: boolean;
    lease_holder?: string;
    lease?: CloudWorkspaceLease;
};

type CloudWorkspaceDeletedRow = {
    id?: string;
    name?: string;
    used_bytes?: number;
    updated_at?: string;
    deleted_at?: string;
    purge_after?: string;
};

type CloudEntitlementState = {
    enabled?: boolean;
    hub_unavailable?: boolean;
    banner?: string;
    reason?: string;
    quota?: number;
    used?: number;
    workspaces?: CloudWorkspaceRow[];
    deleted?: CloudWorkspaceDeletedRow[];
};

const cloudWorkspaceText = (value: unknown, ...keys: string[]): string => {
    if (!value || typeof value !== 'object') return '';
    const row = value as Record<string, unknown>;
    for (const key of keys) {
        const item = row[key];
        if (typeof item === 'string' && item.trim()) return item;
    }
    return '';
};

const cloudWorkspaceNumber = (value: unknown, ...keys: string[]): number => {
    if (!value || typeof value !== 'object') return 0;
    const row = value as Record<string, unknown>;
    for (const key of keys) {
        const item = row[key];
        if (typeof item === 'number' && Number.isFinite(item)) return item;
        if (typeof item === 'string' && item.trim()) {
            const parsed = Number(item);
            if (Number.isFinite(parsed)) return parsed;
        }
    }
    return 0;
};

const cloudWorkspaceFlag = (value: unknown, ...keys: string[]): boolean => {
    if (!value || typeof value !== 'object') return false;
    const row = value as Record<string, unknown>;
    return keys.some(key => row[key] === true);
};

const cloudWorkspaceList = (value: unknown, ...keys: string[]): unknown[] => {
    if (!value || typeof value !== 'object') return [];
    const row = value as Record<string, unknown>;
    for (const key of keys) {
        const item = row[key];
        if (Array.isArray(item)) return item;
    }
    return [];
};

const asCloudWorkspaceRow = (value: unknown): CloudWorkspaceRow | null => {
    if (!value || typeof value !== 'object') return null;
    const row = value as CloudWorkspaceRow;
    const id = cloudWorkspaceText(row, 'id', 'ID').trim();
    if (!id) return null;
    let leaseInUse = cloudWorkspaceFlag(row, 'lease_in_use', 'LeaseInUse');
    let leaseHolder = cloudWorkspaceText(row, 'lease_holder', 'LeaseHolder');
    const nested = (row.lease && typeof row.lease === 'object' ? row.lease : undefined)
        || ((value as { Lease?: CloudWorkspaceRow['lease'] }).Lease && typeof (value as { Lease?: CloudWorkspaceRow['lease'] }).Lease === 'object'
            ? (value as { Lease?: CloudWorkspaceRow['lease'] }).Lease
            : undefined);
    if (!leaseInUse && !leaseHolder && cloudWorkspaceFlag(nested, 'held', 'Held') && !cloudWorkspaceFlag(nested, 'is_self', 'IsSelf')) {
        leaseInUse = true;
        leaseHolder = cloudWorkspaceText(nested, 'machine_name', 'MachineName', 'machine_id', 'MachineID');
    }
    return {
        id,
        name: cloudWorkspaceText(row, 'name', 'Name'),
        used_bytes: cloudWorkspaceNumber(row, 'used_bytes', 'UsedBytes'),
        updated_at: cloudWorkspaceText(row, 'updated_at', 'UpdatedAt'),
        lease_in_use: leaseInUse,
        lease_holder: leaseHolder,
    };
};

const CloudWorkspaceLeaseNote = ({ row, lang }: { row: CloudWorkspaceRow; lang: string }) => {
    if (!row.lease_in_use) return null;
    return (
        <span data-testid="task-cloud-workspace-lease" style={{ display: 'block', marginTop: '2px', fontSize: '0.64rem', color: '#a16207' }}>
            {row.lease_holder
                ? textForLang(lang, `In use on another device (${row.lease_holder})`, `占用中（其他设备：${row.lease_holder}）`, `佔用中（其他裝置：${row.lease_holder}）`)
                : textForLang(lang, 'In use on another device', '占用中（其他设备）', '佔用中（其他裝置）')}
        </span>
    );
};

const asCloudWorkspaceDeletedRow = (value: unknown): CloudWorkspaceDeletedRow | null => {
    if (!value || typeof value !== 'object') return null;
    const id = cloudWorkspaceText(value, 'id', 'ID').trim();
    if (!id) return null;
    return {
        id,
        name: cloudWorkspaceText(value, 'name', 'Name'),
        used_bytes: cloudWorkspaceNumber(value, 'used_bytes', 'UsedBytes'),
        updated_at: cloudWorkspaceText(value, 'updated_at', 'UpdatedAt'),
        deleted_at: cloudWorkspaceText(value, 'deleted_at', 'DeletedAt'),
        purge_after: cloudWorkspaceText(value, 'purge_after', 'PurgeAfter'),
    };
};

const dropCloudWorkspaceFromEntitlement = (
    prev: CloudEntitlementState | null | undefined,
    workspaceId: string,
    deleted?: CloudWorkspaceDeletedRow | null,
): CloudEntitlementState | null => {
    if (!prev) return prev ?? null;
    const id = workspaceId.trim();
    if (!id) return prev;
    const current = (prev.workspaces || []).find(row => row.id === id);
    const workspaces = (prev.workspaces || []).filter(row => row.id !== id);
    const rest = (prev.deleted || []).filter(row => row.id !== id);
    const nextDeleted = deleted || (current
        ? { id, name: current.name, used_bytes: current.used_bytes, deleted_at: new Date().toISOString() }
        : null);
    return {
        ...prev,
        used: workspaces.length,
        workspaces,
        deleted: nextDeleted ? [nextDeleted, ...rest] : rest,
    };
};

const forceDeleteCloudWorkspaceMessage = (lang: string, name: string): string => {
    const trimmed = name.trim();
    if (trimmed) {
        return textForLang(
            lang,
            `Permanently delete “${trimmed}” and all remote files? This cannot be undone.`,
            `永久删除「${trimmed}」及全部远程文件？此操作不可撤销。`,
            `永久刪除「${trimmed}」及全部遠端檔案？此操作不可復原。`,
        );
    }
    return textForLang(
        lang,
        'Permanently delete this workspace and all remote files? This cannot be undone.',
        '永久删除此工作区及全部远程文件？此操作不可撤销。',
        '永久刪除此工作區及全部遠端檔案？此操作不可復原。',
    );
};

const cloudOverviewSummary = (lang: string, current: number, quota: number, bound: number, blank: number): { text: string; hint: string } => {
    const quotaLimit = Number(quota) || 0;
    if (quotaLimit > 0) {
        return {
            text: textForLang(
                lang,
                `${current} of ${quotaLimit} workspaces · ${bound} linked to a task · ${blank} unlinked`,
                `现有 ${current} 个工作区 / 最多 ${quotaLimit} 个 · ${bound} 个已关联任务 · ${blank} 个未关联`,
                `現有 ${current} 個工作區 / 最多 ${quotaLimit} 個 · ${bound} 個已關聯任務 · ${blank} 個未關聯`,
            ),
            hint: textForLang(
                lang,
                `Hub allows up to ${quotaLimit} cloud workspaces`,
                `Hub 管理员最多允许 ${quotaLimit} 个云端工作区`,
                `Hub 管理員最多允許 ${quotaLimit} 個雲端工作區`,
            ),
        };
    }
    return {
        text: textForLang(
            lang,
            `${current} workspaces · ${bound} linked to a task · ${blank} unlinked`,
            `共 ${current} 个工作区 · ${bound} 个已关联任务 · ${blank} 个未关联`,
            `共 ${current} 個工作區 · ${bound} 個已關聯任務 · ${blank} 個未關聯`,
        ),
        hint: '',
    };
};

const normalizeCloudEntitlement = (ent: CloudEntitlementState | null | undefined): CloudEntitlementState => ({
    enabled: cloudWorkspaceFlag(ent, 'enabled', 'Enabled'),
    hub_unavailable: cloudWorkspaceFlag(ent, 'hub_unavailable', 'HubUnavailable'),
    banner: cloudWorkspaceText(ent, 'banner', 'Banner'),
    reason: cloudWorkspaceText(ent, 'reason', 'Reason'),
    quota: cloudWorkspaceNumber(ent, 'quota', 'Quota'),
    used: cloudWorkspaceNumber(ent, 'used', 'Used'),
    workspaces: cloudWorkspaceList(ent, 'workspaces', 'Workspaces')
        .map(asCloudWorkspaceRow)
        .filter((row): row is CloudWorkspaceRow => !!row),
    deleted: cloudWorkspaceList(ent, 'deleted', 'Deleted')
        .map(asCloudWorkspaceDeletedRow)
        .filter((row): row is CloudWorkspaceDeletedRow => !!row),
});

const CLOUD_RESTORE_LIST_WAIT_MS = 8000;

const cloudWorkspaceIdFromTask = (task: unknown): string => {
    if (!task || typeof task !== 'object') return '';
    const rec = task as Record<string, unknown>;
    const tagsRaw = rec.tags ?? rec.Tags;
    const tags = Array.isArray(tagsRaw) ? tagsRaw.map(item => String(item)) : undefined;
    const projectPath = String(rec.project_path ?? rec.ProjectPath ?? rec.projectPath ?? '');
    const workingDir = String(rec.working_dir ?? rec.WorkingDir ?? rec.workingDir ?? '');
    return (
        cloudWorkspaceIdFromTags(tags)
        || cloudWorkspaceIdFromPath(projectPath)
        || cloudWorkspaceIdFromPath(workingDir)
        || ''
    ).trim();
};

const taskForCloudWorkspace = (taskList: TaskManagementItem[], workspaceId: string): TaskManagementItem | undefined => {
    const id = workspaceId.trim();
    if (!id) return undefined;
    return taskList.find(task => cloudWorkspaceIdFromTask(task) === id);
};

const cloudWorkspaceIdsFromTaskRows = (rows: unknown): Set<string> => {
    const ids = new Set<string>();
    if (!Array.isArray(rows)) return ids;
    for (const row of rows) {
        const id = cloudWorkspaceIdFromTask(row);
        if (id) ids.add(id);
    }
    return ids;
};

const pickDefaultCloudWorkspaceId = (rows: CloudWorkspaceRow[] | undefined, skipIds?: Set<string>): string => {
    const list = rows || [];
    const unusedFree = list.find(row => {
        const id = (row.id || '').trim();
        return id && !row.lease_in_use && !skipIds?.has(id);
    });
    const unusedAny = list.find(row => {
        const id = (row.id || '').trim();
        return id && !skipIds?.has(id);
    });
    const free = list.find(row => !!(row.id || '').trim() && !row.lease_in_use);
    const any = list.find(row => !!(row.id || '').trim());
    return ((unusedFree || unusedAny || free || any)?.id || '').trim();
};

const disabledCloudEntitlement = (): CloudEntitlementState => ({
    enabled: false,
    hub_unavailable: false,
    banner: '',
    reason: '',
    workspaces: [],
    deleted: [],
});

const formatCloudWorkspaceBytes = (bytes: number, _lang: string): string => {
    const value = Number.isFinite(bytes) ? Math.max(0, bytes) : 0;
    if (value < 1024) return `${value} B`;
    if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`;
    if (value < 1024 * 1024 * 1024) return `${(value / (1024 * 1024)).toFixed(1)} MB`;
    return `${(value / (1024 * 1024 * 1024)).toFixed(1)} GB`;
};

const formatCloudWorkspaceLastUsed = (value: string | undefined, lang: string): string => {
    const timestamp = Date.parse(value || '');
    if (!Number.isFinite(timestamp)) return '';
    const date = new Date(timestamp);
    const pad = (part: number) => String(part).padStart(2, '0');
    const dateText = `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
    return textForLang(lang, `Last used ${dateText}`, `上次使用 ${dateText}`, `上次使用 ${dateText}`);
};

const cloudWorkspaceDeniedReason = (lang: string, ent: CloudEntitlementState | null): string => {
    if (ent?.enabled === true) return '';
    if (ent?.hub_unavailable) {
        return ent.banner?.trim()
            || textForLang(lang, 'Hub is unavailable; cloud workspaces are temporarily unavailable.', 'Hub 不可用，云端工作区暂不可用', 'Hub 不可用，雲端工作區暫不可用');
    }
    if (ent == null) {
        return textForLang(lang, 'Checking cloud workspace access…', '正在检查云端工作区权限…', '正在檢查雲端工作區權限…');
    }
    if (ent.reason === 'machine_unbound') {
        return textForLang(lang, 'This PC is not bound to a Hub user. Bind it, then create a cloud workspace task.', '本机尚未绑定 Hub 用户。请先绑定后再创建云端任务。', '本機尚未綁定 Hub 使用者。請先綁定後再建立雲端任務。');
    }
    if (ent.reason === 'not_granted') {
        return textForLang(lang, 'An admin has not enabled cloud workspaces for you.', '管理员尚未向你开放云端工作区。', '管理員尚未向你開放雲端工作區。');
    }
    return textForLang(
        lang,
        'Cloud workspace is not enabled for this device. Bind this PC to a Hub user, and ask an admin to turn it on.',
        '当前设备未开放云端工作区。请确认本机已绑定 Hub 用户，且管理员已全员或按部门开通。',
        '目前裝置未開放雲端工作區。請確認本機已綁定 Hub 使用者，且管理員已全員或按部門開通。',
    );
};

type CreateTaskTypeId = 'chat' | 'coding_dev' | 'remote_coding_dev' | 'cloud';

export const SidebarTaskManagement = ({
    lang,
    themeMode,
    tasks,
    renamingTaskPath,
    setRenamingTaskPath,
    renameValue,
    setRenameValue,
    resumeTask,
    continueWorkflowProject = () => {},
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
    activeAssistantTask,
    taskListVisible = true,
}: SidebarTaskManagementProps) => {
    const { showConfirm } = useDialog();
    const [creatingTask, setCreatingTask] = useState(false);
    const [creatingTaskMode, setCreatingTaskMode] = useState<'' | PureCodingAgentMode>('');
    const [createDialogOpen, setCreateDialogOpen] = useState(false);
    const [newTaskName, setNewTaskName] = useState('');
    const [newTaskWorkingDir, setNewTaskWorkingDir] = useState('');
    /** '' | coding_dev | remote_coding_dev */
    const [newTaskMode, setNewTaskMode] = useState<'' | 'coding_dev' | 'remote_coding_dev'>('');
    const [remoteHost, setRemoteHost] = useState('');
    const [remotePort, setRemotePort] = useState('22');
    const [remoteUser, setRemoteUser] = useState('');
    const [remotePassword, setRemotePassword] = useState('');
    const [remoteWorkDir, setRemoteWorkDir] = useState('');
    /** Preserve the welcome incident posture if SSH setup needs a manual retry. */
    const [remoteSafety, setRemoteSafety] = useState<OpenCreateCodingTaskDetail['remoteSafety']>();
    const [createError, setCreateError] = useState('');
    const [taskListNotice, setTaskListNotice] = useState('');
    /** Last Hub entitlement for this process session. Null means still loading. */
    const [cloudEntitlement, setCloudEntitlement] = useState<CloudEntitlementState | null>(null);
    const [workspaceKind, setWorkspaceKind] = useState<'local' | 'cloud'>('local');
    const [selectedCloudWorkspaceId, setSelectedCloudWorkspaceId] = useState('');
    const [renamingCloudWorkspaceId, setRenamingCloudWorkspaceId] = useState('');
    const [renameCloudWorkspaceValue, setRenameCloudWorkspaceValue] = useState('');
    const [deleteConfirmCloudWorkspaceId, setDeleteConfirmCloudWorkspaceId] = useState('');
    const [cloudWorkspaceBusy, setCloudWorkspaceBusy] = useState(false);
    const [cloudOverviewOpen, setCloudOverviewOpen] = useState(false);
    const [cloudRestorePending, setCloudRestorePending] = useState(false);
    const [forceDeletingCloudWorkspaceId, setForceDeletingCloudWorkspaceId] = useState('');
    const pendingRestoreWorkspaceIdsRef = useRef<Set<string> | null>(null);
    const releasedCloudWorkspaceIdsRef = useRef<Set<string>>(new Set());
    const restoreWaitTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const tasksRef = useRef(tasks);
    tasksRef.current = tasks;
    const cloudOverviewBackdropMouseDownRef = useRef(false);
    const overviewFetchGenRef = useRef(0);
    const cloudOverviewDialogRef = useRef<HTMLDivElement | null>(null);
    const forceDeleteConfirmOpenRef = useRef(false);
    const cloudWorkspaceBusyRef = useRef(false);
    const cloudEntitlementRef = useRef(cloudEntitlement);
    cloudEntitlementRef.current = cloudEntitlement;
    const closeCloudOverview = () => {
        overviewFetchGenRef.current += 1;
        setDeleteConfirmCloudWorkspaceId('');
        setCreateError('');
        setCloudOverviewOpen(false);
    };
    const [selectingWorkingDir, setSelectingWorkingDir] = useState(false);
    const [sceneDetailPath, setSceneDetailPath] = useState<string | null>(null);
    const [sceneDetail, setSceneDetail] = useState<ProjectSceneDetail | null>(null);
    const [sceneDetailLoading, setSceneDetailLoading] = useState(false);
    const [sceneDetailError, setSceneDetailError] = useState('');
    /** Invalidates an older evidence request when a row is closed or another row opens. */
    const sceneDetailRequestGenRef = useRef(0);
    const [openingTaskPath, setOpeningTaskPath] = useState<string | null>(null);
    /** Task rows currently being removed from the durable task list. */
    const [removingTaskPaths, setRemovingTaskPaths] = useState<Set<string>>(() => new Set());
    /** Keep errors per task so concurrent removal failures remain actionable. */
    const [removeErrors, setRemoveErrors] = useState<Map<string, string>>(() => new Map());
    // State updates are asynchronous; keep the authoritative in-flight set in
    // a ref so a rapid second click cannot start a duplicate backend deletion.
    const removingTaskPathsRef = useRef<Set<string>>(new Set());
    const creatingTaskRef = useRef(false);
    /** Invalidates completion UI from a create whose form was superseded by a newer request. */
    const createTaskGenRef = useRef(0);
    const createBackdropMouseDownRef = useRef(false);
    /** Cancels stale placeholder-selection timers when dialog re-opens or unmounts. */
    const selectPlaceholderTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
    const selectPlaceholderGenRef = useRef(0);
    /** Edit remote SSH settings dialog (from task list context menu). */
    const [editRemoteOpen, setEditRemoteOpen] = useState(false);
    const [editRemotePath, setEditRemotePath] = useState('');
    const [editRemoteName, setEditRemoteName] = useState('');
    const [editHost, setEditHost] = useState('');
    const [editPort, setEditPort] = useState('22');
    const [editUser, setEditUser] = useState('');
    const [editPassword, setEditPassword] = useState('');
    const [editWorkDir, setEditWorkDir] = useState('');
    const [editError, setEditError] = useState('');
    const [editInfo, setEditInfo] = useState('');
    const [editSaving, setEditSaving] = useState(false);
    const [editTesting, setEditTesting] = useState(false);
    const editBackdropMouseDownRef = useRef(false);
    /** Bumps on each open so a stale GetRemoteCodingTaskMeta cannot overwrite a newer dialog. */
    const editRemoteFetchGenRef = useRef(0);
    /** Drops stale CloudWorkspaceEntitlement results when the dialog is reopened. */
    const entitlementFetchGenRef = useRef(0);
    const mountedRef = useRef(true);
    const finishCloudRestoreWait = () => {
        pendingRestoreWorkspaceIdsRef.current = null;
        if (restoreWaitTimerRef.current != null) {
            clearTimeout(restoreWaitTimerRef.current);
            restoreWaitTimerRef.current = null;
        }
        if (mountedRef.current) setCloudRestorePending(false);
    };
    useEffect(() => {
        if (cloudEntitlement) rememberCloudWorkspaceDisplayNames(cloudEntitlement);
    }, [cloudEntitlement]);
    useEffect(() => {
        if (!taskListNotice) return;
        const timer = window.setTimeout(() => setTaskListNotice(''), 8000);
        return () => window.clearTimeout(timer);
    }, [taskListNotice]);
    useEffect(() => {
        mountedRef.current = true;
        const entitlementGen = ++entitlementFetchGenRef.current;
        if (typeof CloudWorkspaceEntitlement === 'function') {
            void (async () => {
                try {
                    const ent = await CloudWorkspaceEntitlement() as CloudEntitlementState | null;
                    if (!mountedRef.current) return;
                    const next = normalizeCloudEntitlement(ent);
                    // Opening the create dialog or overview must not cancel Hub →
                    // sidebar restore: those paths bump their own fetch gens.
                    if (entitlementFetchGenRef.current === entitlementGen) {
                        setCloudEntitlement(next);
                    }
                    if (next.enabled && !next.hub_unavailable && typeof RestoreCloudWorkspaceTasks === 'function') {
                        setCloudRestorePending(true);
                        let restored: unknown = [];
                        try {
                            restored = await RestoreCloudWorkspaceTasks();
                        } catch {
                            // Keep the local task list if Hub restore fails.
                        }
                        if (!mountedRef.current) return;
                        refreshTasks();
                        const ids = cloudWorkspaceIdsFromTaskRows(restored);
                        for (const id of releasedCloudWorkspaceIdsRef.current) ids.delete(id);
                        if (ids.size === 0) {
                            finishCloudRestoreWait();
                            return;
                        }
                        const alreadyVisible = tasksRef.current.filter(proj => proj.has_output !== false);
                        let missing = false;
                        for (const id of ids) {
                            if (!taskForCloudWorkspace(alreadyVisible, id)) {
                                missing = true;
                                break;
                            }
                        }
                        if (!missing) {
                            finishCloudRestoreWait();
                            return;
                        }
                        pendingRestoreWorkspaceIdsRef.current = ids;
                        if (restoreWaitTimerRef.current != null) clearTimeout(restoreWaitTimerRef.current);
                        restoreWaitTimerRef.current = setTimeout(() => {
                            finishCloudRestoreWait();
                        }, CLOUD_RESTORE_LIST_WAIT_MS);
                    }
                } catch {
                    if (!mountedRef.current || entitlementFetchGenRef.current !== entitlementGen) return;
                    setCloudEntitlement(prev => {
                        if (prev == null) {
                            return { enabled: false, hub_unavailable: true, banner: '', workspaces: [], deleted: [] };
                        }
                        if (prev.hub_unavailable) return prev;
                        return { ...prev, hub_unavailable: true };
                    });
                }
            })();
        } else {
            setCloudEntitlement(disabledCloudEntitlement());
        }
        return () => {
            mountedRef.current = false;
            removingTaskPathsRef.current.clear();
            pendingRestoreWorkspaceIdsRef.current = null;
            if (restoreWaitTimerRef.current != null) {
                clearTimeout(restoreWaitTimerRef.current);
                restoreWaitTimerRef.current = null;
            }
            if (selectPlaceholderTimerRef.current != null) {
                clearTimeout(selectPlaceholderTimerRef.current);
                selectPlaceholderTimerRef.current = null;
            }
            selectPlaceholderGenRef.current += 1;
        };
    }, []);
    const visibleTasks = tasks.filter(proj => proj.has_output !== false);
    const taskListRef = useRef<HTMLDivElement | null>(null);
    const activeRowPresent = visibleTasks.some(proj => isActiveTaskRow(proj, activeAssistantTask));
    useEffect(() => {
        if (!taskListVisible || !activeRowPresent) return;
        const el = taskListRef.current?.querySelector('[data-active="true"]') as HTMLElement | null;
        if (!el || typeof el.scrollIntoView !== 'function') return;
        el.scrollIntoView({ block: 'nearest', inline: 'nearest' });
    }, [taskListVisible, activeRowPresent, activeAssistantTask]);
    useEffect(() => {
        const ids = pendingRestoreWorkspaceIdsRef.current;
        if (!ids || ids.size === 0) return;
        const released = releasedCloudWorkspaceIdsRef.current;
        for (const id of ids) {
            if (released.has(id)) continue;
            if (!taskForCloudWorkspace(visibleTasks, id)) return;
        }
        finishCloudRestoreWait();
    }, [tasks]);

    const resetEditRemoteFields = () => {
        setEditRemotePath('');
        setEditRemoteName('');
        setEditHost('');
        setEditPort('22');
        setEditUser('');
        setEditPassword('');
        setEditWorkDir('');
        setEditError('');
        setEditInfo('');
    };

    const lastLocalCodingWorkDir = () => (
        tasks.find(t => agentModeFromTaskTags(t.tags) === 'coding_dev' && t.working_dir)?.working_dir
        || tasks.find(t => t.working_dir)?.working_dir
        || ''
    );

    const lastRemoteCodingMeta = () => {
        const lastRemote = tasks.find(t => agentModeFromTaskTags(t.tags) === 'remote_coding_dev');
        return remoteCodingMetaFromTaskTags(lastRemote?.tags);
    };

    /** Apply env defaults for a task mode. force=true overwrites (dialog open); false only fills blanks (mode toggle). */
    const applyEnvDefaultsForMode = (mode: '' | PureCodingAgentMode, force: boolean) => {
        if (mode === 'remote_coding_dev') {
            if (force) setNewTaskWorkingDir('');
            const meta = lastRemoteCodingMeta();
            if (force) {
                setRemoteHost(meta.host || '');
                setRemotePort(String(meta.port || 22));
                setRemoteUser(meta.user || '');
                setRemotePassword('');
                setRemoteWorkDir(meta.workDir || '');
            } else {
                // Keep any values the user already typed; never auto-fill password.
                setRemoteHost(prev => prev.trim() || meta.host || '');
                setRemoteUser(prev => prev.trim() || meta.user || '');
                setRemoteWorkDir(prev => prev.trim() || meta.workDir || '');
                setRemotePort(prev => {
                    const cur = prev.trim();
                    if (cur && cur !== '22') return prev;
                    return String(meta.port || 22);
                });
            }
            return;
        }
        // Local coding / ordinary chat: workdir defaults.
        if (force) {
            setNewTaskWorkingDir(lastLocalCodingWorkDir());
            setRemoteHost('');
            setRemotePort('22');
            setRemoteUser('');
            setRemotePassword('');
            setRemoteWorkDir('');
        } else {
            setNewTaskWorkingDir(prev => prev.trim() || lastLocalCodingWorkDir());
            // Drop password when leaving remote mode so it does not linger in form state.
            setRemotePassword('');
        }
    };

    const cancelSelectPlaceholderTimer = () => {
        if (selectPlaceholderTimerRef.current != null) {
            clearTimeout(selectPlaceholderTimerRef.current);
            selectPlaceholderTimerRef.current = null;
        }
        selectPlaceholderGenRef.current += 1;
    };

    const schedulePostOpenFocus = (mode: '' | PureCodingAgentMode, preferredFieldId?: string) => {
        cancelSelectPlaceholderTimer();
        const gen = selectPlaceholderGenRef.current;
        // After portaled dialog commits (setTimeout > rAF under React 18 batching).
        selectPlaceholderTimerRef.current = setTimeout(() => {
            selectPlaceholderTimerRef.current = null;
            if (!mountedRef.current || selectPlaceholderGenRef.current !== gen) return;
            if (preferredFieldId) {
                const preferred = document.getElementById(preferredFieldId) as HTMLInputElement | HTMLTextAreaElement | null;
                if (preferred && !preferred.disabled) {
                    preferred.focus();
                    if ('select' in preferred) preferred.select();
                    return;
                }
            }
            // Welcome param dialog fills the command before open; jump to empty env fields.
            if (mode === 'remote_coding_dev') {
                const ids = ['task-remote-host', 'task-remote-user', 'task-remote-password', 'task-remote-workdir'] as const;
                for (const id of ids) {
                    const el = document.getElementById(id) as HTMLInputElement | null;
                    if (el && !el.value.trim()) {
                        el.focus();
                        return;
                    }
                }
                (document.getElementById('task-remote-host') as HTMLInputElement | null)?.focus();
                return;
            }
            (document.getElementById('task-working-directory') as HTMLButtonElement | null)?.focus();
        }, 0);
    };

    const openCreateDialog = (prefill?: {
        mode?: '' | PureCodingAgentMode;
        name?: string;
        workingDir?: string;
        remote?: OpenCreateCodingTaskDetail['remote'];
        remoteSafety?: OpenCreateCodingTaskDetail['remoteSafety'];
        cloud?: boolean;
        cloudWorkspaceId?: string;
    }, opts?: { force?: boolean }) => {
        // The in-flight create guard applies to the manual "+" path. Event-driven
        // fallbacks pass force so a request is never dropped silently; dialog
        // controls stay disabled by `creatingTask` until the in-flight create settles.
        if (editSaving || editTesting) return;
        if (!opts?.force && creatingTaskRef.current) return;
        if (taskContextMenu) setTaskContextMenu(null);
        closeCloudOverview();
        // Avoid stacking create + edit modals.
        if (editRemoteOpen) {
            editRemoteFetchGenRef.current += 1;
            setEditRemoteOpen(false);
            resetEditRemoteFields();
        }
        const openAsCloud = !!(prefill?.cloud || prefill?.cloudWorkspaceId);
        const mode = openAsCloud ? '' : (prefill?.mode ?? '');
        // Welcome templates: normalize whitespace + clamp to textarea maxLength.
        const name = typeof prefill?.name === 'string'
            ? normalizeTaskCommandInput(prefill.name)
            : '';
        setNewTaskName(name);
        setNewTaskMode(mode);
        setRemoteSafety(mode === 'remote_coding_dev' ? prefill?.remoteSafety : undefined);
        applyEnvDefaultsForMode(mode, true);
        // Welcome param dialog may already collect env — prefer those over defaults.
        if (mode === 'coding_dev' && typeof prefill?.workingDir === 'string' && prefill.workingDir.trim()) {
            setNewTaskWorkingDir(prefill.workingDir.trim());
        }
        if (mode === 'remote_coding_dev' && prefill?.remote) {
            const r = prefill.remote;
            if (r.host?.trim()) setRemoteHost(r.host.trim());
            if (r.user?.trim()) setRemoteUser(r.user.trim());
            if (r.workDir?.trim()) setRemoteWorkDir(r.workDir.trim());
            if (r.port > 0) setRemotePort(String(r.port));
            if (r.password) setRemotePassword(r.password);
        }
        setCreateError('');
        setTaskListNotice('');
        setWorkspaceKind(openAsCloud ? 'cloud' : 'local');
        setSelectedCloudWorkspaceId((prefill?.cloudWorkspaceId || '').trim());
        setRenamingCloudWorkspaceId('');
        setRenameCloudWorkspaceValue('');
        setDeleteConfirmCloudWorkspaceId('');
        setCreateDialogOpen(true);
        // A forced welcome event may replace the form while another create is
        // still in flight. Its eventual completion must not close or overwrite
        // this newer dialog.
        createTaskGenRef.current += 1;
        schedulePostOpenFocus(mode, openAsCloud ? 'task-management-cloud-workspace-mode' : undefined);
        const entitlementGen = ++entitlementFetchGenRef.current;
        void (async () => {
            const applyEntitlement = (next: CloudEntitlementState) => {
                if (!mountedRef.current || entitlementFetchGenRef.current !== entitlementGen) return;
                setCloudEntitlement(next);
            };
            if (typeof CloudWorkspaceEntitlement !== 'function') {
                applyEntitlement(disabledCloudEntitlement());
                return;
            }
            try {
                const ent = await CloudWorkspaceEntitlement() as CloudEntitlementState | null;
                applyEntitlement(normalizeCloudEntitlement(ent));
            } catch {
                // Binding throw: keep last successful enabled; never fake a grant revocation.
                if (!mountedRef.current || entitlementFetchGenRef.current !== entitlementGen) return;
                setCloudEntitlement(prev => {
                    if (prev == null) {
                        return { enabled: false, hub_unavailable: true, banner: '', workspaces: [], deleted: [] };
                    }
                    if (prev.hub_unavailable) return prev;
                    return { ...prev, hub_unavailable: true };
                });
            }
        })();
    };
    const openCreateDialogRef = useRef(openCreateDialog);
    openCreateDialogRef.current = openCreateDialog;

    // Welcome software-dev cards: open create-task or auto-create when env is complete.
    useEffect(() => {
        const handler = (event: Event) => {
            const detail = (event as CustomEvent<OpenCreateCodingTaskDetail>).detail || {};
            const mode = detail.mode === 'remote_coding_dev' || detail.mode === 'coding_dev'
                ? detail.mode
                : undefined;
            if (!mode) return;
            const name = typeof detail.name === 'string' ? detail.name : '';
            const workingDir = typeof detail.workingDir === 'string' ? detail.workingDir : undefined;
            const remote = detail.remote;
            const remoteSafety = detail.remoteSafety;

            if (detail.autoCreate) {
                const taskName = normalizeTaskCommandInput(name);
                // Another create in flight: fall back to prefilled dialog instead of dropping the request.
                if (taskName && !creatingTaskRef.current) {
                    void (async () => {
                        const createGen = ++createTaskGenRef.current;
                        creatingTaskRef.current = true;
                        setCreatingTask(true);
                        setCreatingTaskMode(mode);
                        setCreateError('');
                        try {
                            if (mode === 'remote_coding_dev') {
                                if (!remote?.host?.trim() || !remote?.user?.trim() || !remote?.password || !remote?.workDir?.trim()) {
                                    throw new Error('remote env incomplete');
                                }
                                const portNum = remote.port == null
                                    ? 22
                                    : Number.isInteger(remote.port) && remote.port > 0 && remote.port < 65536
                                        ? remote.port
                                        : null;
                                if (portNum == null) {
                                    throw new Error(textForLang(lang, 'Port must be a whole number from 1 to 65535.', '端口必须是 1 到 65535 之间的整数。', '連接埠必須是 1 到 65535 之間的整數。'));
                                }
                                await createTask(taskName, undefined, 'remote_coding_dev', {
                                    host: remote.host.trim(),
                                    port: portNum,
                                    user: remote.user.trim(),
                                    password: remote.password,
                                    workDir: remote.workDir.trim(),
                                    safety: remoteSafety,
                                });
                            } else {
                                const dir = (workingDir || '').trim();
                                if (dir) await createTask(taskName, dir, 'coding_dev');
                                else await createTask(taskName, undefined, 'coding_dev');
                            }
                        } catch (err) {
                            console.error('[SidebarTaskManagement] autoCreate coding task failed:', err);
                            // Release the in-flight flag first: openCreateDialog refuses to
                            // open while creatingTaskRef is set, which would silently swallow
                            // the failure (the welcome param dialog is already closed).
                            creatingTaskRef.current = false;
                            if (mountedRef.current) {
                                setCreatingTask(false);
                                setCreatingTaskMode('');
                            }
                            // Fall back to dialog with prefilled fields so user can fix and retry.
                            if (mountedRef.current && createTaskGenRef.current === createGen) {
                                openCreateDialogRef.current({
                                    mode,
                                    name: taskName,
                                    workingDir,
                                    remote,
                                    remoteSafety,
                                });
                                const msg = extractErrorMessage(err);
                                if (msg) setCreateError(msg);
                                if (mode === 'remote_coding_dev') {
                                    schedulePostOpenFocus(mode, 'task-remote-password');
                                }
                            }
                        } finally {
                            creatingTaskRef.current = false;
                            if (mountedRef.current) {
                                setCreatingTask(false);
                                setCreatingTaskMode('');
                            }
                        }
                    })();
                    return;
                }
            }

            // Fall back to the prefilled dialog (force: a create may still be in
            // flight — the dialog shows it as busy instead of dropping the request).
            openCreateDialogRef.current(
                {
                    mode,
                    name,
                    workingDir,
                    remote,
                    remoteSafety,
                },
                { force: true },
            );
        };
        window.addEventListener(EVENT_OPEN_CREATE_CODING_TASK, handler);
        return () => window.removeEventListener(EVENT_OPEN_CREATE_CODING_TASK, handler);
    }, [createTask]);

    const closeCreateDialog = () => {
        if (creatingTaskRef.current) return;
        cancelSelectPlaceholderTimer();
        setCreateDialogOpen(false);
        setNewTaskName('');
        setNewTaskWorkingDir('');
        setNewTaskMode('');
        setRemoteHost('');
        setRemotePort('22');
        setRemoteUser('');
        setRemotePassword('');
        setRemoteWorkDir('');
        setRemoteSafety(undefined);
        setCreateError('');
        setWorkspaceKind('local');
        setSelectedCloudWorkspaceId('');
        setRenamingCloudWorkspaceId('');
        setRenameCloudWorkspaceValue('');
        setDeleteConfirmCloudWorkspaceId('');
        setCloudWorkspaceBusy(false);
    };

    const closeEditRemoteDialog = () => {
        if (editSaving || editTesting) return;
        editRemoteFetchGenRef.current += 1; // invalidate in-flight meta fetch
        setEditRemoteOpen(false);
        resetEditRemoteFields();
    };

    const openEditRemoteDialog = async (projectPath: string, name: string, tags?: string[]) => {
        if (editSaving || editTesting) return;
        setTaskContextMenu(null);
        // Avoid stacking create + edit modals.
        setCreateDialogOpen(false);
        setEditError('');
        setEditInfo('');
        setEditPassword('');
        setEditRemotePath(projectPath);
        setEditRemoteName(name);
        // Prefill from tags immediately, then refresh from backend.
        const fromTags = remoteCodingMetaFromTaskTags(tags);
        setEditHost(fromTags.host);
        setEditUser(fromTags.user);
        setEditPort(String(fromTags.port || 22));
        setEditWorkDir(fromTags.workDir);
        setEditRemoteOpen(true);
        const fetchGen = ++editRemoteFetchGenRef.current;
        try {
            const meta = await GetRemoteCodingTaskMeta(projectPath);
            if (!mountedRef.current || fetchGen !== editRemoteFetchGenRef.current) return;
            // Prefer backend meta when present; keep tag prefill when fields are empty.
            if (meta?.host != null && String(meta.host).trim()) setEditHost(String(meta.host).trim());
            if (meta?.user != null && String(meta.user).trim()) setEditUser(String(meta.user).trim());
            if (meta?.port != null && Number(meta.port) > 0) setEditPort(String(meta.port));
            if (meta?.work_dir != null && String(meta.work_dir).trim()) setEditWorkDir(String(meta.work_dir).trim());
        } catch {
            /* tags prefill is enough */
        }
    };

    const parseEditPort = () => {
        const portNum = Number.parseInt(editPort.trim() || '22', 10);
        return Number.isFinite(portNum) && portNum > 0 && portNum < 65536 ? portNum : 22;
    };

    const submitEditRemote = async () => {
        if (editSaving || editTesting || !editRemotePath) return;
        const pathAtSave = editRemotePath;
        const host = editHost.trim();
        const user = editUser.trim();
        const workDir = editWorkDir.trim();
        const port = parseEditPort();
        if (!host || !user || !workDir) {
            setEditError(textForLang(lang, 'Please fill host, username, and remote work directory.', '请填写主机、用户名和远程工作目录。', '請填寫主機、使用者名稱和遠端工作目錄。'));
            return;
        }
        const genAtSave = editRemoteFetchGenRef.current;
        setEditSaving(true);
        setEditError('');
        setEditInfo('');
        try {
            await UpdateRemoteCodingTaskMeta(pathAtSave, host, user, workDir, port);
            if (!mountedRef.current) return;
            refreshTasks();
            // Dialog stays open for optional Test SSH; only show status if still on this task.
            if (editRemotePath === pathAtSave && editRemoteFetchGenRef.current === genAtSave) {
                setEditInfo(textForLang(lang, 'Remote settings saved. Password is not stored — reconnect with the password when needed.', '远程设置已保存。密码不会落盘，下次连接时请重新输入。', '遠端設定已儲存。密碼不會落盤，下次連線時請重新輸入。'));
            }
        } catch (error) {
            if (mountedRef.current && editRemotePath === pathAtSave && editRemoteFetchGenRef.current === genAtSave) {
                setEditError(extractErrorMessage(error) || textForLang(lang, 'Failed to save remote settings', '保存远程设置失败', '儲存遠端設定失敗'));
            }
        } finally {
            if (mountedRef.current) setEditSaving(false);
        }
    };

    const testEditRemoteSSH = async () => {
        if (editSaving || editTesting) return;
        const pathAtTest = editRemotePath;
        const genAtTest = editRemoteFetchGenRef.current;
        const host = editHost.trim();
        const user = editUser.trim();
        const password = editPassword;
        const workDir = editWorkDir.trim();
        const port = parseEditPort();
        if (!host || !user || !password || !workDir) {
            setEditError(textForLang(lang, 'Please fill host, username, password, and remote work directory to test.', '测试连接需填写主机、用户名、密码和远程工作目录。', '測試連線需填寫主機、使用者名稱、密碼和遠端工作目錄。'));
            return;
        }
        setEditTesting(true);
        setEditError('');
        setEditInfo('');
        try {
            const msg = await TestRemoteSSHConnection(host, user, password, workDir, port);
            // In-dialog status only — avoid a second modal on top of the edit dialog.
            if (mountedRef.current && editRemotePath === pathAtTest && editRemoteFetchGenRef.current === genAtTest) {
                setEditInfo(msg || textForLang(lang, 'SSH connection OK', 'SSH 连接成功', 'SSH 連線成功'));
            }
        } catch (error) {
            if (mountedRef.current && editRemotePath === pathAtTest && editRemoteFetchGenRef.current === genAtTest) {
                setEditError(extractErrorMessage(error) || textForLang(lang, 'SSH connection failed', 'SSH 连接失败', 'SSH 連線失敗'));
            }
        } finally {
            if (mountedRef.current) setEditTesting(false);
        }
    };

    const selectWorkingDir = async () => {
        if (creatingTaskRef.current || selectingWorkingDir) return;
        setSelectingWorkingDir(true);
        try {
            const dir = await SelectWorkingDir();
            if (dir) setNewTaskWorkingDir(dir);
        } catch (error) {
            console.error('[SidebarTaskManagement] SelectWorkingDir failed:', error);
        } finally {
            setSelectingWorkingDir(false);
        }
    };

    const browseCloudWorkspace = async (workspaceId: string, projectPath?: string, tags?: string[], workingDir?: string) => {
        const id = workspaceId.trim();
        if (!id) return;
        try {
            const path = (projectPath || '').trim();
            if (!path) {
                throw new Error(textForLang(lang, 'Cloud workspace folder is not available.', '云端工作区目录不可用', '雲端工作區目錄不可用'));
            }
            await resumeTask(path, { project_path: path, tags });
            window.dispatchEvent(new CustomEvent(REVEAL_CLOUD_WORKSPACE_FILES_EVENT, {
                detail: { projectPath: path, workingDir: (workingDir || '').trim() },
            }));
        } catch (error) {
            console.error('[SidebarTaskManagement] browse cloud workspace failed:', error);
        }
    };

    const cloudGranted = cloudEntitlement?.enabled === true;
    const cloudQuota = Number(cloudEntitlement?.quota) || 0;
    const cloudCreateSelected = cloudGranted && workspaceKind === 'cloud' && newTaskMode !== 'remote_coding_dev';
    const cloudWorkspaces = cloudEntitlement?.workspaces || [];
    const cloudUsed = Math.max(Number(cloudEntitlement?.used) || 0, cloudWorkspaces.length);
    const cloudQuotaReached = cloudGranted && cloudQuota > 0 && cloudUsed >= cloudQuota;
    const cloudDeleted = cloudEntitlement?.deleted || [];
    const cloudDeniedReason = cloudWorkspaceDeniedReason(lang, cloudEntitlement);
    const boundCloudWorkspaceIds = useMemo(() => {
        const ids = new Set<string>();
        for (const task of tasks) {
            const id = cloudWorkspaceIdFromTask(task);
            if (id) ids.add(id);
        }
        return ids;
    }, [tasks]);
    const boundCloudWorkspaces = useMemo(
        () => cloudWorkspaces.filter(row => !!taskForCloudWorkspace(visibleTasks, row.id || '')),
        [cloudWorkspaces, visibleTasks],
    );
    const blankCloudWorkspaces = useMemo(
        () => cloudWorkspaces.filter(row => !taskForCloudWorkspace(visibleTasks, row.id || '')),
        [cloudWorkspaces, visibleTasks],
    );
    const overviewSummary = cloudOverviewSummary(
        lang,
        cloudWorkspaces.length,
        cloudQuota,
        boundCloudWorkspaces.length,
        blankCloudWorkspaces.length,
    );

    const reloadCloudEntitlement = async () => {
        if (typeof CloudWorkspaceEntitlement !== 'function') return;
        const overviewGen = ++overviewFetchGenRef.current;
        try {
            const ent = await CloudWorkspaceEntitlement() as CloudEntitlementState | null;
            if (!mountedRef.current || overviewFetchGenRef.current !== overviewGen) return;
            setCloudEntitlement(normalizeCloudEntitlement(ent));
        } catch {
            if (!mountedRef.current || overviewFetchGenRef.current !== overviewGen) return;
            setCloudEntitlement(prev => {
                if (prev == null) {
                    return { enabled: false, hub_unavailable: true, banner: '', workspaces: [], deleted: [] };
                }
                if (prev.hub_unavailable) return prev;
                return { ...prev, hub_unavailable: true };
            });
        }
    };

    const openCloudOverview = () => {
        if (!cloudGranted || creatingTaskRef.current) return;
        if (taskContextMenu) setTaskContextMenu(null);
        if (createDialogOpen) closeCreateDialog();
        if (editRemoteOpen) closeEditRemoteDialog();
        setCreateError('');
        setCloudOverviewOpen(true);
        // Invalidate an in-flight create-dialog entitlement apply without
        // cancelling Hub → sidebar restore (restore no longer keys off this gen).
        entitlementFetchGenRef.current += 1;
        void reloadCloudEntitlement();
    };

    const openBoundCloudWorkspace = (workspaceId: string) => {
        const task = taskForCloudWorkspace(visibleTasks, workspaceId);
        if (!task) {
            if (cloudRestorePending) return;
            openCreateDialog({ cloud: true, cloudWorkspaceId: workspaceId });
            return;
        }
        if (!assistantReady) {
            onTaskSwitchBlocked?.();
            return;
        }
        closeCloudOverview();
        void handleTaskDoubleClick(task);
    };

    const openBlankCloudWorkspace = (workspaceId?: string) => {
        if (cloudRestorePending) return;
        openCreateDialog({ cloud: true, cloudWorkspaceId: workspaceId });
    };

    useEffect(() => {
        if (!cloudOverviewOpen || cloudGranted) return;
        if (forceDeleteConfirmOpenRef.current) return;
        closeCloudOverview();
    }, [cloudGranted, cloudOverviewOpen]);

    useEffect(() => {
        if (!cloudOverviewOpen) return;
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key !== 'Escape') return;
            // This capture listener is registered before CustomDialog's, so skip while a confirm owns Escape.
            if (forceDeleteConfirmOpenRef.current || document.querySelector('.custom-dialog')) return;
            event.preventDefault();
            closeCloudOverview();
        };
        window.addEventListener('keydown', onKeyDown, true);
        const focusTimer = window.setTimeout(() => {
            cloudOverviewDialogRef.current?.focus();
        }, 0);
        return () => {
            window.removeEventListener('keydown', onKeyDown, true);
            window.clearTimeout(focusTimer);
        };
    }, [cloudOverviewOpen]);

    useEffect(() => {
        if (!cloudCreateSelected) return;
        const rows = cloudEntitlement?.workspaces || [];
        if (selectedCloudWorkspaceId && rows.some(row => row.id === selectedCloudWorkspaceId)) return;
        const firstId = pickDefaultCloudWorkspaceId(rows, boundCloudWorkspaceIds);
        if (firstId) setSelectedCloudWorkspaceId(firstId);
    }, [boundCloudWorkspaceIds, cloudCreateSelected, cloudEntitlement, selectedCloudWorkspaceId]);

    const resetCloudWorkspaceEditors = () => {
        setRenamingCloudWorkspaceId('');
        setRenameCloudWorkspaceValue('');
        setDeleteConfirmCloudWorkspaceId('');
    };

    const selectCreateTaskType = (id: CreateTaskTypeId) => {
        if (creatingTask || cloudWorkspaceBusy) return;
        if (id === 'cloud') {
            if (!cloudGranted) return;
            if (workspaceKind === 'cloud') return;
            setWorkspaceKind('cloud');
            if (newTaskMode !== '') {
                setNewTaskMode('');
                setRemoteSafety(undefined);
                applyEnvDefaultsForMode('', false);
            }
            setCreateError('');
            resetCloudWorkspaceEditors();
            if (!selectedCloudWorkspaceId) {
                const firstId = pickDefaultCloudWorkspaceId(cloudWorkspaces, boundCloudWorkspaceIds);
                if (firstId) setSelectedCloudWorkspaceId(firstId);
            }
            return;
        }
        const nextMode: '' | 'coding_dev' | 'remote_coding_dev' = id === 'coding_dev'
            ? 'coding_dev'
            : id === 'remote_coding_dev'
                ? 'remote_coding_dev'
                : '';
        if (newTaskMode === nextMode && workspaceKind !== 'cloud') return;
        setNewTaskMode(nextMode);
        setWorkspaceKind('local');
        if (nextMode === 'remote_coding_dev') {
            setSelectedCloudWorkspaceId('');
            resetCloudWorkspaceEditors();
        } else {
            setRemoteSafety(undefined);
        }
        setCreateError('');
        applyEnvDefaultsForMode(nextMode, false);
    };

    const provisionNewCloudWorkspace = async (): Promise<string> => {
        if (typeof CreateCloudWorkspace !== 'function') {
            throw new Error(textForLang(lang, 'Failed to create cloud workspace', '新建云端工作区失败', '新建雲端工作區失敗'));
        }
        const created = asCloudWorkspaceRow(await CreateCloudWorkspace(''));
        const id = (created?.id || '').trim();
        if (!id) throw new Error(textForLang(lang, 'Failed to create cloud workspace', '新建云端工作区失败', '新建雲端工作區失敗'));
        entitlementFetchGenRef.current += 1;
        setCloudEntitlement(prev => {
            const workspaces = [...(prev?.workspaces || []).filter(row => row.id !== id), created!];
            const deleted = (prev?.deleted || []).filter(row => row.id !== id);
            return {
                ...(prev || {}),
                enabled: true,
                used: workspaces.length,
                workspaces,
                deleted,
            };
        });
        setSelectedCloudWorkspaceId(id);
        resetCloudWorkspaceEditors();
        return id;
    };

    const createNewCloudWorkspace = async () => {
        if (!cloudCreateSelected || cloudWorkspaceBusy || creatingTaskRef.current || cloudQuotaReached) return;
        setCloudWorkspaceBusy(true);
        setCreateError('');
        try {
            await provisionNewCloudWorkspace();
        } catch (error) {
            setCreateError(extractErrorMessage(error) || textForLang(lang, 'Failed to create cloud workspace', '新建云端工作区失败', '新建雲端工作區失敗'));
        } finally {
            if (mountedRef.current) setCloudWorkspaceBusy(false);
        }
    };

    const renameSelectedCloudWorkspace = async (id: string) => {
        const workspaceId = id.trim();
        const name = renameCloudWorkspaceValue.trim();
        if (!workspaceId || !name || typeof RenameCloudWorkspace !== 'function') {
            resetCloudWorkspaceEditors();
            return;
        }
        if (cloudWorkspaceBusy || creatingTaskRef.current) return;
        setCloudWorkspaceBusy(true);
        setCreateError('');
        try {
            const renamed = asCloudWorkspaceRow(await RenameCloudWorkspace(workspaceId, name)) || { id: workspaceId, name };
            entitlementFetchGenRef.current += 1;
            setCloudEntitlement(prev => ({
                ...(prev || {}),
                workspaces: (prev?.workspaces || []).map(row => row.id === workspaceId ? { ...row, ...renamed, id: workspaceId } : row),
            }));
            resetCloudWorkspaceEditors();
        } catch (error) {
            setCreateError(extractErrorMessage(error) || textForLang(lang, 'Failed to rename cloud workspace', '重命名云端工作区失败', '重命名雲端工作區失敗'));
        } finally {
            if (mountedRef.current) setCloudWorkspaceBusy(false);
        }
    };

    const deleteSelectedCloudWorkspace = async (id: string) => {
        const workspaceId = id.trim();
        if (!workspaceId || typeof DeleteCloudWorkspace !== 'function') return;
        if (cloudWorkspaceBusy || creatingTaskRef.current) return;
        setCloudWorkspaceBusy(true);
        setCreateError('');
        try {
            const deletedRaw = await DeleteCloudWorkspace(workspaceId);
            entitlementFetchGenRef.current += 1;
            overviewFetchGenRef.current += 1;
            const current = cloudWorkspaces.find(row => row.id === workspaceId);
            const deleted = asCloudWorkspaceDeletedRow(deletedRaw) || {
                id: workspaceId,
                name: current?.name || '',
                used_bytes: current?.used_bytes || 0,
                deleted_at: new Date().toISOString(),
            };
            setCloudEntitlement(prev => dropCloudWorkspaceFromEntitlement(prev, workspaceId, deleted));
            if (selectedCloudWorkspaceId === workspaceId) setSelectedCloudWorkspaceId('');
            resetCloudWorkspaceEditors();
            refreshTasks();
        } catch (error) {
            setCreateError(extractErrorMessage(error) || textForLang(lang, 'Failed to delete cloud workspace', '删除云端工作区失败', '刪除雲端工作區失敗'));
        } finally {
            if (mountedRef.current) setCloudWorkspaceBusy(false);
        }
    };

    const forceDeleteCloudWorkspace = async (id: string) => {
        const workspaceId = id.trim();
        if (!workspaceId || cloudWorkspaceBusyRef.current || creatingTaskRef.current) return;
        if (typeof ForceDeleteCloudWorkspace !== 'function') return;
        cloudWorkspaceBusyRef.current = true;
        setCloudWorkspaceBusy(true);
        setForceDeletingCloudWorkspaceId(workspaceId);
        setCreateError('');
        entitlementFetchGenRef.current += 1;
        overviewFetchGenRef.current += 1;
        try {
            await ForceDeleteCloudWorkspace(workspaceId);
            setCloudEntitlement(prev => {
                if (!prev) return prev;
                const workspaces = (prev.workspaces || []).filter(row => row.id !== workspaceId);
                const deleted = (prev.deleted || []).filter(row => row.id !== workspaceId);
                return { ...prev, workspaces, deleted, used: workspaces.length };
            });
            refreshTasks();
        } catch (error) {
            if (mountedRef.current) {
                setCreateError(extractErrorMessage(error) || textForLang(lang, 'Failed to permanently delete cloud workspace', '强制删除云端工作区失败', '強制刪除雲端工作區失敗'));
            }
        } finally {
            cloudWorkspaceBusyRef.current = false;
            if (mountedRef.current) {
                setCloudWorkspaceBusy(false);
                setForceDeletingCloudWorkspaceId('');
            }
        }
    };

    const confirmForceDeleteCloudWorkspace = async (id: string) => {
        const workspaceId = id.trim();
        if (!workspaceId || cloudWorkspaceBusy || creatingTaskRef.current || cloudRestorePending) return;
        if (typeof ForceDeleteCloudWorkspace !== 'function') return;
        if (forceDeleteConfirmOpenRef.current) return;
        const name = (cloudDeleted.find(row => row.id === workspaceId)?.name || '').trim();
        forceDeleteConfirmOpenRef.current = true;
        let confirmed = false;
        try {
            confirmed = await showConfirm(
                forceDeleteCloudWorkspaceMessage(lang, name),
                textForLang(lang, 'Delete permanently', '强制删除', '強制刪除'),
                {
                    confirmText: textForLang(lang, 'OK', '确定', '確定'),
                    cancelText: textForLang(lang, 'Cancel', '取消', '取消'),
                    confirmVariant: 'danger',
                },
            );
        } finally {
            forceDeleteConfirmOpenRef.current = false;
        }
        if (!confirmed || !mountedRef.current || creatingTaskRef.current) return;
        const stillDeleted = (cloudEntitlementRef.current?.deleted || []).some(row => row.id === workspaceId);
        if (!stillDeleted) {
            setCreateError(textForLang(lang, 'This workspace is no longer in Recently deleted.', '该工作区已不在「最近删除」中。', '該工作區已不在「最近刪除」中。'));
            return;
        }
        await forceDeleteCloudWorkspace(workspaceId);
    };

    const restoreDeletedCloudWorkspace = async (id: string) => {
        const workspaceId = id.trim();
        if (!workspaceId || typeof RestoreCloudWorkspace !== 'function') return;
        if (cloudWorkspaceBusy || creatingTaskRef.current || cloudQuotaReached) return;
        setCloudWorkspaceBusy(true);
        setCreateError('');
        try {
            const restored = asCloudWorkspaceRow(await RestoreCloudWorkspace(workspaceId));
            entitlementFetchGenRef.current += 1;
            overviewFetchGenRef.current += 1;
            const fallback = cloudDeleted.find(row => row.id === workspaceId);
            const next = restored || {
                id: workspaceId,
                name: fallback?.name || '',
                used_bytes: fallback?.used_bytes || 0,
            };
            setCloudEntitlement(prev => {
                const workspaces = [...(prev?.workspaces || []).filter(row => row.id !== workspaceId), next];
                const deleted = (prev?.deleted || []).filter(row => row.id !== workspaceId);
                return {
                    ...(prev || {}),
                    used: workspaces.length,
                    workspaces,
                    deleted,
                };
            });
            setSelectedCloudWorkspaceId(workspaceId);
            releasedCloudWorkspaceIdsRef.current.delete(workspaceId);
            resetCloudWorkspaceEditors();
            if (typeof RestoreCloudWorkspaceTasks === 'function') {
                try {
                    await RestoreCloudWorkspaceTasks();
                } catch {
                    // Workspace is restored; the task list can catch up on the next refresh.
                }
            }
            refreshTasks();
        } catch (error) {
            setCreateError(extractErrorMessage(error) || textForLang(lang, 'Failed to restore cloud workspace', '恢复云端工作区失败', '恢復雲端工作區失敗'));
        } finally {
            if (mountedRef.current) setCloudWorkspaceBusy(false);
        }
    };

    const submitCreateTask = async (opts?: { resumeExisting?: boolean }) => {
        if (creatingTaskRef.current) return;
        // A task-management creation establishes an environment, not an agent
        // instruction. Welcome flows may still supply a title, otherwise use a
        // localized generated title for the durable task record.
        const taskName = normalizeTaskCommandInput(newTaskName) || generatedTaskTitle(lang, newTaskMode, cloudCreateSelected);
        const isRemoteCreate = newTaskMode === 'remote_coding_dev';
        const resumeExisting = opts?.resumeExisting === true;
        if (isRemoteCreate) {
            if (!remoteHost.trim() || !remoteUser.trim() || !remotePassword || !remoteWorkDir.trim()) {
                setCreateError(textForLang(lang, 'Please fill host, username, password, and remote work directory.', '请填写主机、用户名、密码和远程工作目录。', '請填寫主機、使用者名稱、密碼和遠端工作目錄。'));
                return;
            }
            if (parseRemotePort(remotePort) == null) {
                setCreateError(textForLang(lang, 'Port must be a whole number from 1 to 65535.', '端口必须是 1 到 65535 之间的整数。', '連接埠必須是 1 到 65535 之間的整數。'));
                return;
            }
        }
        const selectedCloudId = selectedCloudWorkspaceId.trim();
        if (!isRemoteCreate && cloudCreateSelected) {
            if (!selectedCloudId) {
                setCreateError(textForLang(lang, 'Select a cloud workspace first.', '请先选择一个云端工作区。', '請先選擇一個雲端工作區。'));
                return;
            }
            if (!resumeExisting && boundCloudWorkspaceIds.has(selectedCloudId) && cloudQuotaReached) {
                setCreateError(textForLang(lang, 'Cloud workspace quota reached. Open the existing task, or delete a workspace first.', '已达云端工作区配额。可打开现有任务，或先删除一个工作区。', '已達雲端工作區配額。可打開現有任務，或先刪除一個工作區。'));
                return;
            }
        }
        creatingTaskRef.current = true;
        const createGen = createTaskGenRef.current;
        setCreatingTask(true);
        setCreatingTaskMode(newTaskMode);
        setCreateError('');
        let provisionedCloudId = '';
        // SSH / cloud-workspace prepare can take several seconds. Leave the
        // modal immediately so the task-list progress stays visible. Keep form
        // state until success so a failed attempt can reopen with fields intact.
        if (isRemoteCreate || cloudCreateSelected) {
            cancelSelectPlaceholderTimer();
            setCreateDialogOpen(false);
        }
        try {
            const workingDir = newTaskWorkingDir.trim();
            if (isRemoteCreate) {
                const portNum = parseRemotePort(remotePort);
                if (portNum == null) return;
                await createTask(taskName, undefined, 'remote_coding_dev', {
                    host: remoteHost.trim(),
                    port: portNum,
                    user: remoteUser.trim(),
                    password: remotePassword,
                    workDir: remoteWorkDir.trim(),
                    safety: remoteSafety,
                });
            } else if (cloudCreateSelected) {
                let cloudId = selectedCloudId;
                if (!resumeExisting && boundCloudWorkspaceIds.has(selectedCloudId)) {
                    cloudId = await provisionNewCloudWorkspace();
                    provisionedCloudId = cloudId;
                }
                if (newTaskMode === 'coding_dev') {
                    await createTask(taskName, undefined, 'coding_dev', undefined, cloudId);
                } else {
                    await createTask(taskName, undefined, undefined, undefined, cloudId);
                }
            } else if (newTaskMode === 'coding_dev') {
                if (workingDir) await createTask(taskName, workingDir, 'coding_dev');
                else await createTask(taskName, undefined, 'coding_dev');
            } else if (workingDir) {
                await createTask(taskName, workingDir);
            } else {
                await createTask(taskName);
            }
            if (mountedRef.current && createTaskGenRef.current === createGen) {
                setCreateDialogOpen(false);
                setNewTaskName('');
                setNewTaskWorkingDir('');
                setNewTaskMode('');
                setRemoteHost('');
                setRemotePort('22');
                setRemoteUser('');
                setRemotePassword('');
                setRemoteWorkDir('');
                setRemoteSafety(undefined);
                setCreateError('');
                setWorkspaceKind('local');
                setSelectedCloudWorkspaceId('');
                resetCloudWorkspaceEditors();
                if (resumeExisting) {
                    setTaskListNotice(textForLang(
                        lang,
                        'Opened the existing cloud-workspace task.',
                        '已打开该云端工作区的现有任务。',
                        '已打開該雲端工作區的現有任務。',
                    ));
                }
            }
        } catch (error) {
            if (provisionedCloudId && typeof DeleteCloudWorkspace === 'function') {
                try {
                    await DeleteCloudWorkspace(provisionedCloudId);
                    if (mountedRef.current) {
                        setCloudEntitlement(prev => dropCloudWorkspaceFromEntitlement(prev, provisionedCloudId));
                        if (selectedCloudWorkspaceId === provisionedCloudId) setSelectedCloudWorkspaceId('');
                    }
                } catch {
                    // Keep the original create error; the leftover slot can be deleted from the overview.
                }
            }
            if (mountedRef.current && createTaskGenRef.current === createGen) {
                setCreateError(extractErrorMessage(error) || textForLang(lang, 'Failed to create task', '创建任务失败', '建立任務失敗'));
                if (isRemoteCreate) {
                    setCreateDialogOpen(true);
                    schedulePostOpenFocus('remote_coding_dev', 'task-remote-password');
                } else if (cloudCreateSelected) {
                    setCreateDialogOpen(true);
                }
            }
        } finally {
            creatingTaskRef.current = false;
            if (mountedRef.current) {
                setCreatingTask(false);
                setCreatingTaskMode('');
            }
        }
    };

    const openSceneDetail = async (projectPath: string, fallbackName?: string, reload = false) => {
        if (sceneDetailPath === projectPath && !reload) {
            sceneDetailRequestGenRef.current += 1;
            setSceneDetailPath(null);
            setSceneDetail(null);
            setSceneDetailLoading(false);
            setSceneDetailError('');
            return;
        }
        const requestGen = ++sceneDetailRequestGenRef.current;
        setSceneDetailPath(projectPath);
        // Never leave evidence from the previously expanded task visible while
        // the replacement request is still resolving.
        setSceneDetail(null);
        setSceneDetailError('');
        setSceneDetailLoading(true);
        try {
            const detail = await GetProjectScene(projectPath);
            if (mountedRef.current && sceneDetailRequestGenRef.current === requestGen) {
                setSceneDetail((detail || null) as ProjectSceneDetail | null);
            }
        } catch (error) {
            console.error('[SidebarTaskManagement] GetProjectScene failed:', error);
            if (mountedRef.current && sceneDetailRequestGenRef.current === requestGen) {
                setSceneDetail({ project_path: projectPath, name: fallbackName || projectPath, recent_artifacts: [] });
                setSceneDetailError(textForLang(lang, 'Could not load evidence', '无法加载证据', '無法載入證據'));
            }
        } finally {
            if (mountedRef.current && sceneDetailRequestGenRef.current === requestGen) {
                setSceneDetailLoading(false);
            }
        }
    };

    const handleTaskDoubleClick = async (task: TaskManagementItem) => {
        const projectPath = task.project_path;
        if (renamingTaskPath || removingTaskPaths.has(projectPath)) return;
        if (openingTaskPath === projectPath) return;
        if (!assistantReady) {
            onTaskSwitchBlocked?.();
            return;
        }
        setOpeningTaskPath(projectPath);
        try {
            // Pass the rendered row too. It is the authoritative identity at the
            // moment of interaction, avoiding a stale parent list cache from
            // misrouting an expert row as a generic project task.
            await resumeTask(projectPath, task);
            if (isCloudWorkspaceTask(task)) {
                window.dispatchEvent(new CustomEvent(REVEAL_CLOUD_WORKSPACE_FILES_EVENT, {
                    detail: { projectPath, workingDir: task.working_dir || '' },
                }));
            }
        } finally {
            setOpeningTaskPath(current => current === projectPath ? null : current);
        }
    };

    const handleRemoveTask = async (projectPath: string, tags?: string[]) => {
        if (removingTaskPathsRef.current.has(projectPath)) return;
        removingTaskPathsRef.current.add(projectPath);
        setRemovingTaskPaths(new Set(removingTaskPathsRef.current));
        setRemoveErrors(current => {
            if (!current.has(projectPath)) return current;
            const next = new Map(current);
            next.delete(projectPath);
            return next;
        });
        try {
            // Retain the one-argument backend call for ordinary tasks.
            // Expert rows pass their tags so the guard can keep their history safe.
            const removed = tags
                ? await hideTask(projectPath, tags)
                : await hideTask(projectPath);
            if (removed === false) {
                return;
            }
            emitProjectTaskClosed(projectPath);
            refreshTasks();
            const targetPath = normalizeProjectSessionPath(projectPath);
            const workspaceId = cloudWorkspaceIdFromTags(tags)
                || cloudWorkspaceIdFromTask(tasks.find(item => normalizeProjectSessionPath(item.project_path) === targetPath));
            if (workspaceId) {
                // Invalidate in-flight entitlement applies so a late mount/create
                // fetch cannot put the deleted workspace back as an unlinked row.
                entitlementFetchGenRef.current += 1;
                releasedCloudWorkspaceIdsRef.current.add(workspaceId);
                setCloudEntitlement(prev => dropCloudWorkspaceFromEntitlement(prev, workspaceId));
                void reloadCloudEntitlement();
                const pending = pendingRestoreWorkspaceIdsRef.current;
                if (pending?.has(workspaceId)) {
                    pending.delete(workspaceId);
                    if (pending.size === 0) finishCloudRestoreWait();
                }
            }
        } catch (error) {
            console.error('[SidebarTaskManagement] Remove task failed:', error);
            if (mountedRef.current) {
                setRemoveErrors(current => {
                    const next = new Map(current);
                    next.set(projectPath, extractErrorMessage(error) || textForLang(lang, 'Failed to remove task', '删除任务失败', '刪除任務失敗'));
                    return next;
                });
            }
        } finally {
            removingTaskPathsRef.current.delete(projectPath);
            if (mountedRef.current) setRemovingTaskPaths(new Set(removingTaskPathsRef.current));
        }
    };

    return (
    <div ref={taskListRef} style={{ flex: 1, overflowY: 'auto', padding: '10px 8px 8px' }}>
        <div style={{ padding: '2px 8px 9px', display: 'flex', alignItems: 'center', gap: '6px', fontSize: '0.68rem', color: 'var(--theme-text-muted)', fontWeight: 700, letterSpacing: '0.02em' }}>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', minWidth: 0 }}>
                <span>{textForLang(lang, 'New Task', '\u65b0\u5efa\u4efb\u52a1', '\u65b0\u5efa\u4efb\u52d9')}</span>
                <button
                    type="button"
                    onClick={() => openCreateDialog()}
                    disabled={creatingTask}
                    aria-label={textForLang(lang, 'Create task', '创建任务', '建立任務')}
                    title={textForLang(lang, 'Create task', '创建任务', '建立任務')}
                    style={taskHeaderActionButtonStyle(creatingTask)}
                >
                    <CreateTaskIcon />
                </button>
                {cloudGranted && (
                    <button
                        type="button"
                        data-testid="task-cloud-overview"
                        onClick={openCloudOverview}
                        disabled={creatingTask}
                        aria-expanded={cloudOverviewOpen}
                        aria-haspopup="dialog"
                        aria-controls="task-cloud-overview-panel"
                        aria-label={textForLang(lang, 'Cloud workspaces', '云端工作区', '雲端工作區')}
                        title={textForLang(lang, 'Cloud workspaces — continue on another PC', '云端工作区，换电脑也能继续', '雲端工作區，換電腦也能繼續')}
                        style={taskHeaderActionButtonStyle(creatingTask)}
                    >
                        <CloudComputingIcon />
                    </button>
                )}
            </span>
            <span style={{ marginLeft: 'auto', display: 'inline-flex', alignItems: 'center', gap: '6px', minWidth: 0 }}>
                <span>{textForLang(lang, 'Save as Task', '保存为任务', '保存為任務')}</span>
                <button
                    type="button"
                    onClick={requestSaveCurrentChatAsTask}
                    aria-label={textForLang(lang, 'Save current chat as task', '保存当前对话为任务', '保存目前對話為任務')}
                    title={textForLang(lang, 'Save current chat as task', '保存当前对话为任务', '保存目前對話為任務')}
                    style={taskHeaderActionButtonStyle()}
                >
                    <SaveTaskIcon />
                </button>
            </span>
        </div>
        {creatingTask && !createDialogOpen && (
            <div
                role="status"
                data-testid="task-autocreate-progress"
                style={{ margin: '0 8px 8px', padding: '7px 10px', borderRadius: '6px', border: '1px solid color-mix(in srgb, var(--theme-primary) 30%, var(--theme-border))', background: 'color-mix(in srgb, var(--theme-primary) 7%, transparent)', color: 'var(--theme-text-secondary)', fontSize: '0.72rem', lineHeight: 1.4 }}
            >
                <span>{creatingTaskMode === 'remote_coding_dev'
                    ? textForLang(lang, 'Connecting SSH and creating the remote task…', '正在连接 SSH 并创建远程任务…', '正在連線 SSH 並建立遠端任務…')
                    : workspaceKind === 'cloud'
                        ? textForLang(lang, 'Opening cloud workspace…', '正在打开云端工作区…', '正在打開雲端工作區…')
                        : textForLang(lang, 'Creating task…', '正在创建任务…', '正在建立任務…')}</span>
                <span style={{ display: 'block', marginTop: '6px', height: '3px', overflow: 'hidden', borderRadius: '999px', background: 'color-mix(in srgb, var(--theme-primary) 18%, transparent)' }}>
                    <span className="sidebar-task-progress__bar" style={{ display: 'block', width: '42%', height: '100%', borderRadius: 'inherit', background: 'var(--theme-primary)', animation: 'sidebar-task-restore-progress 0.9s ease-in-out infinite alternate' }} />
                </span>
            </div>
        )}
        {taskListNotice ? (
            <div role="status" data-testid="task-list-notice" style={{ margin: '0 8px 8px', padding: '7px 10px', borderRadius: '6px', border: '1px solid color-mix(in srgb, var(--theme-primary) 32%, var(--theme-border))', background: 'color-mix(in srgb, var(--theme-primary) 9%, transparent)', color: 'var(--theme-text-primary)', fontSize: '0.72rem', lineHeight: 1.4, fontWeight: 500 }}>
                {taskListNotice}
            </div>
        ) : null}
        {visibleTasks.length === 0 ? (
            <div style={{ padding: '24px 8px', textAlign: 'center', fontSize: '0.78rem', color: 'var(--theme-text-muted)', opacity: 0.65 }}>
                {textForLang(lang, 'No tasks', '\u6682\u65e0\u4efb\u52a1', '\u66ab\u7121\u4efb\u52d9')}
            </div>
        ) : visibleTasks.map(proj => {
            const taskIconKind = taskIconKindForProject(proj);
            const remoteMaintenance = isRemoteMaintenanceTask(proj);
            const codingBadge = pureCodingBadgeLabel(proj, lang);
            const cloudBadge = cloudWorkspaceBadgeLabel(proj, lang);
            const pureCoding = isPureCodingTask(proj);
            const workflowStatus = workflowStatusForTask(proj.active_workflow, lang);
            const createdAtLabel = taskCreationLabel(proj.created_at, lang);
            const isRemoving = removingTaskPaths.has(proj.project_path);
            const removalError = removeErrors.get(proj.project_path) || '';
            const isActive = isActiveTaskRow(proj, activeAssistantTask);
            const isBusy = openingTaskPath === proj.project_path || isRemoving;
            const rowStyle: CSSProperties = {
                display: 'flex',
                flexDirection: 'row',
                alignItems: 'flex-start',
                gap: '6px',
                padding: '7px 8px',
                borderRadius: '8px',
                cursor: isBusy ? 'progress' : 'pointer',
                opacity: isBusy ? 0.78 : 1,
                ...(isActive ? {
                    background: 'var(--theme-surface)',
                    boxShadow: 'inset 3px 0 0 var(--theme-primary), inset 0 0 0 1px color-mix(in srgb, var(--theme-primary) 28%, var(--theme-border))',
                } : {}),
            };
            return <div key={proj.id || proj.project_path} data-task-kind={taskIconKind} data-pure-coding={pureCoding ? 'true' : 'false'} data-testid="sidebar-task-row" data-active={isActive ? 'true' : 'false'} data-task-path={proj.project_path}>
                <div className={`sidebar-task-row${isActive ? ' is-active' : ''}${isBusy ? ' is-busy' : ''}`} onDoubleClick={() => { void handleTaskDoubleClick(proj); }} onContextMenu={e => { e.preventDefault(); if (isRemoving) return; setTaskContextMenu({ x: e.clientX, y: e.clientY, projectPath: proj.project_path, name: proj.name || proj.project_path, pinned: !!proj.pinned, isRemoteCoding: isRemoteCodingTask(proj), tags: proj.tags, workingDir: proj.working_dir }); }} style={rowStyle} title={`${proj.name || proj.project_path}\n${proj.project_path}${workflowStatus ? '\n' + [workflowStatus.label, workflowStatus.detail].filter(Boolean).join(' · ') : ''}${cloudBadge ? '\n' + cloudBadge : ''}${codingBadge ? '\n' + codingBadge : ''}${createdAtLabel ? '\n' + createdAtLabel : ''}${proj.preview ? '\n' + proj.preview : ''}`} aria-current={isActive ? 'true' : undefined}>
                    <TaskTypeIcon kind={taskIconKind} lang={lang} maintenance={remoteMaintenance} />
                    <span style={{ minWidth: 0, flex: 1, textAlign: 'left' }}>
                        {(workflowStatus || codingBadge || cloudBadge || proj.pinned) && (
                            <span style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', marginBottom: '3px' }}>
                                {proj.pinned && !pureCoding && (
                                    <span data-testid="task-pinned-badge" style={{ display: 'inline-flex', maxWidth: '100%', padding: '1px 5px', borderRadius: '999px', border: '1px solid color-mix(in srgb, var(--theme-text-muted) 36%, transparent)', color: 'var(--theme-text-muted)', background: 'color-mix(in srgb, var(--theme-text-muted) 8%, transparent)', fontSize: '0.58rem', fontWeight: 700, lineHeight: 1.35 }}>{textForLang(lang, 'Pinned', '\u7f6e\u9876', '\u7f6e\u9802')}</span>
                                )}
                                {codingBadge && (
                                    <span
                                        data-testid={isRemoteCodingTask(proj) ? 'task-remote-coding-badge' : 'task-coding-badge'}
                                        title={taskIconLabel(taskIconKind, lang, remoteMaintenance)}
                                        style={{
                                            display: 'inline-flex',
                                            maxWidth: '100%',
                                            padding: '1px 5px',
                                            borderRadius: '999px',
                                            border: isRemoteCodingTask(proj)
                                                ? '1px solid color-mix(in srgb, #0ea5e9 48%, transparent)'
                                                : '1px solid color-mix(in srgb, #22c55e 48%, transparent)',
                                            color: isRemoteCodingTask(proj) ? '#0284c7' : '#15803d',
                                            background: isRemoteCodingTask(proj)
                                                ? 'color-mix(in srgb, #0ea5e9 12%, transparent)'
                                                : 'color-mix(in srgb, #22c55e 12%, transparent)',
                                            fontSize: '0.58rem',
                                            fontWeight: 700,
                                            lineHeight: 1.35,
                                            whiteSpace: 'nowrap',
                                            overflow: 'hidden',
                                            textOverflow: 'ellipsis',
                                        }}
                                    >{codingBadge}</span>
                                )}
                                {cloudBadge && (
                                    <span
                                        data-testid="task-cloud-workspace-badge"
                                        aria-label={cloudBadge}
                                        title={cloudBadge}
                                        style={{
                                            display: 'inline-flex',
                                            maxWidth: '100%',
                                            padding: '1px 5px',
                                            borderRadius: '999px',
                                            border: '1px solid color-mix(in srgb, #8b5cf6 52%, transparent)',
                                            color: 'color-mix(in srgb, #8b5cf6 78%, var(--theme-text-primary))',
                                            background: 'color-mix(in srgb, #8b5cf6 14%, transparent)',
                                            fontSize: '0.58rem',
                                            fontWeight: 700,
                                            lineHeight: 1.35,
                                            whiteSpace: 'nowrap',
                                            overflow: 'hidden',
                                            textOverflow: 'ellipsis',
                                        }}
                                    >{cloudBadge}</span>
                                )}
                                {workflowStatus && <span data-testid="task-workflow-status" aria-label={`${textForLang(lang, 'Task status', '任务状态', '任務狀態')}: ${workflowStatus.label}${workflowStatus.detail ? ` · ${workflowStatus.detail}` : ''}`} title={`${proj.active_workflow?.type || 'workflow'}${workflowStatus.detail ? ` · ${workflowStatus.detail}` : ''}`} style={{ display: 'inline-flex', maxWidth: '100%', padding: '1px 5px', borderRadius: '999px', border: `1px solid ${TASK_WORKFLOW_STATUS_COLORS[workflowStatus.tone].border}`, color: TASK_WORKFLOW_STATUS_COLORS[workflowStatus.tone].color, background: TASK_WORKFLOW_STATUS_COLORS[workflowStatus.tone].background, fontSize: '0.58rem', fontWeight: 700, lineHeight: 1.35, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{workflowStatus.label}{workflowStatus.detail ? ` · ${workflowStatus.detail}` : ''}</span>}
                            </span>
                        )}
                        {renamingTaskPath === proj.project_path ? <input autoFocus value={renameValue} onChange={e => setRenameValue(e.target.value)} onBlur={async () => { const trimmed = renameValue.trim(); if (trimmed && trimmed !== proj.name) { await renameTask(proj.project_path, trimmed); refreshTasks(); } setRenamingTaskPath(null); }} onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); if (e.key === 'Escape') setRenamingTaskPath(null); }} onClick={e => e.stopPropagation()} style={{ width: '100%', fontSize: '0.74rem', fontWeight: 700, color: 'var(--theme-text-primary)', background: 'var(--theme-surface)', border: '1px solid var(--theme-primary)', borderRadius: '4px', padding: '2px 4px', outline: 'none' }} /> : <span style={{ display: 'block', fontWeight: 700, fontSize: '0.74rem', color: 'var(--theme-text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{proj.name || proj.project_path}</span>}
                        <span style={{ display: 'block', marginTop: '3px', color: 'var(--theme-text-muted)', fontSize: '0.66rem', lineHeight: 1.3, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{isRemoving ? textForLang(lang, 'Removing task...', '正在删除任务...', '正在刪除任務...') : openingTaskPath === proj.project_path ? textForLang(lang, pureCoding ? 'Restoring pure coding environment...' : 'Restoring...', pureCoding ? '正在恢复纯编程环境...' : '恢复中...', pureCoding ? '正在恢復純程式環境...' : '恢復中...') : (proj.preview || proj.project_path)}</span>
                        {!isRemoving && openingTaskPath !== proj.project_path && createdAtLabel && <span data-testid="task-created-at" style={{ display: 'block', marginTop: '2px', color: 'var(--theme-text-muted)', fontSize: '0.6rem', lineHeight: 1.25, opacity: 0.82, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{createdAtLabel}</span>}
                        {(openingTaskPath === proj.project_path || isRemoving) && <span data-testid={isRemoving ? 'task-remove-progress' : undefined} role={isRemoving ? 'status' : undefined} aria-label={isRemoving ? textForLang(lang, 'Removing task', '正在删除任务', '正在刪除任務') : textForLang(lang, pureCoding ? 'Restoring pure coding environment' : 'Restoring task', pureCoding ? '正在恢复纯编程环境' : '正在恢复任务', pureCoding ? '正在恢復純程式環境' : '正在恢復任務')} style={{ display: 'block', marginTop: '6px', height: '3px', overflow: 'hidden', borderRadius: '999px', background: 'color-mix(in srgb, var(--theme-primary) 18%, transparent)' }}><span className="sidebar-task-progress__bar" style={{ display: 'block', width: '42%', height: '100%', borderRadius: 'inherit', background: 'var(--theme-primary)', animation: 'sidebar-task-restore-progress 0.9s ease-in-out infinite alternate' }} /></span>}
                        {removalError && <span data-testid="task-remove-error" role="alert" style={{ display: 'block', marginTop: '4px', color: 'var(--theme-danger, #b91c1c)', fontSize: '0.62rem', lineHeight: 1.3, textAlign: 'left' }}>{removalError}</span>}
                    </span>
                    <button type="button" aria-label={textForLang(lang, 'Scene details', '\u4efb\u52a1\u8bc1\u636e\u8be6\u60c5', '\u4efb\u52d9\u8b49\u64da\u8a73\u60c5')} title={textForLang(lang, 'Scene details', '\u4efb\u52a1\u8bc1\u636e\u8be6\u60c5', '\u4efb\u52d9\u8b49\u64da\u8a73\u60c5')} onClick={e => { e.stopPropagation(); if (!isRemoving) void openSceneDetail(proj.project_path, proj.name); }} disabled={isRemoving || (sceneDetailLoading && sceneDetailPath === proj.project_path)} style={{ border: 'none', background: 'transparent', color: 'var(--theme-primary)', opacity: isRemoving || (sceneDetailLoading && sceneDetailPath === proj.project_path) ? 0.4 : 0.78, cursor: isRemoving ? 'progress' : 'pointer', width: '20px', height: '20px', padding: 0, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}><ProjectSearchIcon name="info" size={13} /></button>
                </div>
                {sceneDetailPath === proj.project_path && <SidebarTaskEvidencePanel detail={sceneDetail} loading={sceneDetailLoading} lang={lang} onContinueWorkflow={continueWorkflowProject} error={sceneDetailError} onRetry={() => { void openSceneDetail(proj.project_path, proj.name, true); }} />}
            </div>
        })}

        {cloudOverviewOpen && createPortal(
            <div
                className="modal-backdrop"
                data-testid="task-cloud-overview-dialog"
                data-ai-theme={getPortalThemeMode(themeMode)}
                data-ai-dark-scheme={getPortalDarkScheme()}
                data-ai-light-scheme={getPortalLightScheme()}
                style={{ zIndex: TASK_CREATE_DIALOG_Z_INDEX }}
                onMouseDown={e => { cloudOverviewBackdropMouseDownRef.current = e.target === e.currentTarget; }}
                onClick={e => {
                    if (e.target === e.currentTarget && cloudOverviewBackdropMouseDownRef.current) closeCloudOverview();
                    cloudOverviewBackdropMouseDownRef.current = false;
                }}
            >
                <div
                    ref={cloudOverviewDialogRef}
                    id="task-cloud-overview-panel"
                    className="modal-content"
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="task-cloud-overview-title"
                    aria-busy={cloudRestorePending || undefined}
                    tabIndex={-1}
                    onMouseDown={e => e.stopPropagation()}
                    onClick={e => e.stopPropagation()}
                    style={{ width: '380px', maxWidth: '92vw', textAlign: 'left', outline: 'none' }}
                >
                    <div className="modal-header">
                        <h3 id="task-cloud-overview-title" style={{ fontSize: '0.88rem', margin: 0 }}>
                            {textForLang(lang, 'Cloud workspaces', '云端工作区', '雲端工作區')}
                        </h3>
                        <button
                            type="button"
                            className="btn-close"
                            data-testid="task-cloud-overview-close"
                            aria-label={textForLang(lang, 'Close', '关闭', '關閉')}
                            title={textForLang(lang, 'Close', '关闭', '關閉')}
                            onClick={closeCloudOverview}
                        >X</button>
                    </div>
                    <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                        {cloudEntitlement?.hub_unavailable ? (
                            <div
                                role="status"
                                data-testid="task-cloud-overview-hub-banner"
                                style={{ padding: '8px 10px', borderRadius: '8px', border: '1px solid color-mix(in srgb, #d97706 48%, var(--theme-border))', background: 'color-mix(in srgb, #f59e0b 12%, transparent)', color: '#a16207', fontSize: '0.72rem', lineHeight: 1.45 }}
                            >
                                {cloudEntitlement.banner?.trim()
                                    || textForLang(lang, 'Hub is unavailable; showing the last known workspaces.', 'Hub 不可用，正在显示上次已知的工作区。', 'Hub 不可用，正在顯示上次已知的工作區。')}
                            </div>
                        ) : null}
                        {cloudRestorePending ? (
                            <div role="status" data-testid="task-cloud-overview-syncing" style={{ fontSize: '0.7rem', color: 'var(--theme-text-secondary)', lineHeight: 1.45 }}>
                                {textForLang(lang, 'Syncing cloud tasks…', '正在同步云端任务…', '正在同步雲端任務…')}
                            </div>
                        ) : null}
                        {createError ? (
                            <div role="alert" data-testid="task-cloud-overview-error" style={{ padding: '7px 10px', borderRadius: '6px', border: '1px solid color-mix(in srgb, var(--theme-danger, #ef4444) 35%, var(--theme-border))', background: 'color-mix(in srgb, var(--theme-danger, #ef4444) 10%, transparent)', color: 'var(--theme-danger, #ef4444)', fontSize: '0.72rem', lineHeight: 1.4 }}>
                                {createError}
                            </div>
                        ) : null}
                        <div
                            data-testid="task-cloud-overview-summary"
                            title={overviewSummary.hint || undefined}
                            style={{ fontSize: '0.72rem', color: 'var(--theme-text-secondary)', lineHeight: 1.45 }}
                        >
                            {overviewSummary.text}
                        </div>
                        <div>
                            <div style={{ marginBottom: '6px', fontSize: '0.72rem', fontWeight: 700, color: 'var(--theme-text-secondary)' }}>
                                {textForLang(lang, 'Linked to a task', '已关联任务', '已關聯任務')}
                            </div>
                            {boundCloudWorkspaces.length === 0 ? (
                                <div style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)', lineHeight: 1.4 }}>
                                    {cloudRestorePending
                                        ? textForLang(lang, 'Syncing linked tasks…', '正在同步已关联任务…', '正在同步已關聯任務…')
                                        : textForLang(lang, 'No cloud workspace is linked to a task yet.', '还没有关联到任务的云端工作区。', '還沒有關聯到任務的雲端工作區。')}
                                </div>
                            ) : boundCloudWorkspaces.map(row => {
                                const id = (row.id || '').trim();
                                const task = taskForCloudWorkspace(visibleTasks, id);
                                return (
                                    <button
                                        key={`bound-${id}`}
                                        type="button"
                                        data-testid="task-cloud-overview-bound"
                                        data-workspace-id={id}
                                        disabled={creatingTask || cloudWorkspaceBusy}
                                        onClick={() => openBoundCloudWorkspace(id)}
                                        style={{ width: '100%', marginBottom: '6px', border: '1px solid color-mix(in srgb, var(--theme-primary) 28%, var(--theme-border))', borderRadius: '8px', background: 'color-mix(in srgb, var(--theme-primary) 8%, var(--theme-surface))', color: 'inherit', textAlign: 'left', cursor: creatingTask || cloudWorkspaceBusy ? 'default' : 'pointer', padding: '8px 10px' }}
                                    >
                                        <span style={{ display: 'block', fontSize: '0.76rem', fontWeight: 700, color: 'var(--theme-text-primary)' }}>{row.name || id}</span>
                                        <span style={{ display: 'block', marginTop: '2px', fontSize: '0.64rem', color: 'var(--theme-primary)', lineHeight: 1.35 }}>
                                            {task?.name
                                                ? textForLang(lang, `Open task: ${task.name}`, `打开任务：${task.name}`, `打開任務：${task.name}`)
                                                : textForLang(lang, 'Open linked task', '打开关联任务', '打開關聯任務')}
                                        </span>
                                        <CloudWorkspaceLeaseNote row={row} lang={lang} />
                                    </button>
                                );
                            })}
                        </div>
                        <div>
                            <div style={{ marginBottom: '6px', fontSize: '0.72rem', fontWeight: 700, color: 'var(--theme-text-secondary)' }}>
                                {textForLang(lang, 'Unlinked workspaces', '未关联任务', '未關聯任務')}
                            </div>
                            {blankCloudWorkspaces.length === 0 ? (
                                <div style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)', lineHeight: 1.4 }}>
                                    {textForLang(lang, 'No unlinked cloud workspace.', '没有未关联任务的云端工作区。', '沒有未關聯任務的雲端工作區。')}
                                </div>
                            ) : blankCloudWorkspaces.map(row => {
                                const id = (row.id || '').trim();
                                const confirmingDelete = deleteConfirmCloudWorkspaceId === id;
                                const busy = creatingTask || cloudWorkspaceBusy || cloudRestorePending;
                                return (
                                    <div
                                        key={`blank-${id}`}
                                        data-workspace-id={id}
                                        style={{ width: '100%', marginBottom: '6px', border: '1px solid var(--theme-border)', borderRadius: '8px', background: 'var(--theme-surface)', padding: '8px 10px', opacity: busy ? 0.55 : 1 }}
                                    >
                                        <button
                                            type="button"
                                            data-testid="task-cloud-overview-blank"
                                            data-workspace-id={id}
                                            disabled={busy}
                                            onClick={() => openBlankCloudWorkspace(id)}
                                            style={{ width: '100%', border: 'none', background: 'transparent', color: 'inherit', textAlign: 'left', cursor: busy ? 'default' : 'pointer', padding: 0 }}
                                        >
                                            <span style={{ display: 'block', fontSize: '0.76rem', fontWeight: 700, color: 'var(--theme-text-primary)' }}>{row.name || id}</span>
                                            <span style={{ display: 'block', marginTop: '2px', fontSize: '0.64rem', color: 'var(--theme-text-muted)', lineHeight: 1.35 }}>
                                                {textForLang(lang, 'Unlinked · create a cloud task', '未关联任务 · 创建云端任务', '未關聯任務 · 建立雲端任務')}
                                            </span>
                                            <CloudWorkspaceLeaseNote row={row} lang={lang} />
                                        </button>
                                        {confirmingDelete ? (
                                            <div data-testid="task-cloud-overview-blank-delete-confirm" style={{ display: 'flex', flexDirection: 'column', gap: '6px', marginTop: '8px' }}>
                                                <span style={{ fontSize: '0.66rem', color: 'var(--theme-text-secondary)', lineHeight: 1.4 }}>
                                                    {textForLang(lang, 'Deleted workspaces can be restored from Recently deleted within 7 days.', '删除后 7 天内可从「最近删除」恢复。', '刪除後 7 天內可從「最近刪除」恢復。')}
                                                </span>
                                                <div style={{ display: 'flex', gap: '6px' }}>
                                                    <button type="button" className="btn-primary" style={{ fontSize: '0.66rem', padding: '3px 8px' }} disabled={busy} onClick={() => { void deleteSelectedCloudWorkspace(id); }}>{textForLang(lang, 'Delete', '确认删除', '確認刪除')}</button>
                                                    <button type="button" className="btn-secondary" style={{ fontSize: '0.66rem', padding: '3px 8px' }} disabled={busy} onClick={resetCloudWorkspaceEditors}>{textForLang(lang, 'Cancel', '取消', '取消')}</button>
                                                </div>
                                            </div>
                                        ) : (
                                            <button
                                                type="button"
                                                data-testid="task-cloud-overview-blank-delete"
                                                disabled={busy}
                                                onClick={() => { setDeleteConfirmCloudWorkspaceId(id); setRenamingCloudWorkspaceId(''); }}
                                                style={{ display: 'block', marginTop: '6px', border: 'none', background: 'transparent', color: 'var(--theme-text-muted)', cursor: busy ? 'default' : 'pointer', fontSize: '0.66rem', padding: 0 }}
                                            >{textForLang(lang, 'Delete workspace', '删除工作区', '刪除工作區')}</button>
                                        )}
                                    </div>
                                );
                            })}
                        </div>
                        {cloudDeleted.length > 0 && (
                            <div data-testid="task-cloud-overview-deleted" style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
                                <div style={{ fontSize: '0.72rem', fontWeight: 700, color: 'var(--theme-text-secondary)' }}>
                                    {textForLang(lang, 'Recently deleted', '最近删除', '最近刪除')}
                                </div>
                                {cloudDeleted.map(row => (
                                    <div key={`overview-deleted-${row.id}`} data-workspace-id={row.id} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px', padding: '6px 8px', borderRadius: '7px', border: '1px dashed var(--theme-border)' }}>
                                        <span style={{ minWidth: 0 }}>
                                            <span style={{ display: 'block', fontSize: '0.72rem', color: 'var(--theme-text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{row.name || row.id}</span>
                                            <span style={{ display: 'block', fontSize: '0.62rem', color: 'var(--theme-text-muted)' }}>
                                                {textForLang(lang, 'Restorable for 7 days', '7 天内可恢复', '7 天內可恢復')}
                                            </span>
                                            {forceDeletingCloudWorkspaceId === row.id && <span data-testid="task-cloud-overview-force-delete-progress" role="status" aria-label={textForLang(lang, 'Permanently deleting workspace and remote files', '正在永久删除工作区及远程文件', '正在永久刪除工作區及遠端檔案')} style={{ display: 'block', marginTop: '4px', width: '130px', height: '3px', overflow: 'hidden', borderRadius: '999px', background: 'color-mix(in srgb, #dc2626 18%, transparent)' }}><span style={{ display: 'block', width: '45%', height: '100%', borderRadius: 'inherit', background: '#dc2626', animation: 'sidebar-task-restore-progress 0.9s ease-in-out infinite alternate' }} /></span>}
                                        </span>
                                        <span style={{ display: 'inline-flex', gap: '5px', flexShrink: 0 }}>
                                        <button
                                            type="button"
                                            data-testid="task-cloud-overview-restore"
                                            disabled={creatingTask || cloudWorkspaceBusy || cloudRestorePending || cloudQuotaReached}
                                            title={cloudQuotaReached
                                                ? textForLang(lang, 'Cloud workspace quota reached', '已达云端工作区配额', '已達雲端工作區配額')
                                                : textForLang(lang, 'Restore', '恢复', '恢復')}
                                            onClick={() => { void restoreDeletedCloudWorkspace(row.id || ''); }}
                                            style={{ flexShrink: 0, border: '1px solid var(--theme-border)', borderRadius: '6px', background: 'var(--theme-surface)', color: 'var(--theme-primary)', cursor: creatingTask || cloudWorkspaceBusy || cloudRestorePending || cloudQuotaReached ? 'default' : 'pointer', padding: '3px 8px', fontSize: '0.66rem', opacity: creatingTask || cloudWorkspaceBusy || cloudRestorePending || cloudQuotaReached ? 0.5 : 1 }}
                                        >
                                            {textForLang(lang, 'Restore', '恢复', '恢復')}
                                        </button>
                                        <button
                                            type="button"
                                            data-testid="task-cloud-overview-force-delete"
                                            aria-label={
                                                (row.name || '').trim()
                                                    ? textForLang(lang, `Delete “${row.name}” permanently`, `强制删除「${row.name}」`, `強制刪除「${row.name}」`)
                                                    : textForLang(lang, 'Delete permanently', '强制删除', '強制刪除')
                                            }
                                            disabled={creatingTask || cloudWorkspaceBusy || cloudRestorePending}
                                            onClick={() => { void confirmForceDeleteCloudWorkspace(row.id || ''); }}
                                            style={{ flexShrink: 0, border: '1px solid color-mix(in srgb, #dc2626 48%, var(--theme-border))', borderRadius: '6px', background: 'color-mix(in srgb, #dc2626 8%, transparent)', color: '#b91c1c', padding: '3px 6px', fontSize: '0.62rem' }}
                                        >
                                            {textForLang(lang, 'Delete permanently', '强制删除', '強制刪除')}
                                        </button>
                                        </span>
                                    </div>
                                ))}
                            </div>
                        )}
                        <button
                            type="button"
                            data-testid="task-cloud-overview-new"
                            disabled={creatingTask || cloudWorkspaceBusy || cloudRestorePending}
                            onClick={() => openBlankCloudWorkspace()}
                            style={{ border: '1px solid color-mix(in srgb, #d97706 45%, var(--theme-border))', borderRadius: '8px', background: 'color-mix(in srgb, #f59e0b 12%, var(--theme-surface))', color: '#a16207', cursor: creatingTask || cloudWorkspaceBusy || cloudRestorePending ? 'default' : 'pointer', padding: '8px 10px', fontSize: '0.74rem', fontWeight: 700, textAlign: 'left', opacity: creatingTask || cloudWorkspaceBusy || cloudRestorePending ? 0.55 : 1 }}
                        >
                            {textForLang(lang, 'New cloud task', '新建云端任务', '新建雲端任務')}
                        </button>
                    </div>
                </div>
            </div>,
            document.body,
        )}

        {createDialogOpen && createPortal(
            <div
                className="modal-backdrop"
                data-ai-theme={getPortalThemeMode(themeMode)}
                data-ai-dark-scheme={getPortalDarkScheme()}
                data-ai-light-scheme={getPortalLightScheme()}
                style={{ zIndex: TASK_CREATE_DIALOG_Z_INDEX }}
                onMouseDown={e => { createBackdropMouseDownRef.current = e.target === e.currentTarget; }}
                onClick={e => { if (e.target === e.currentTarget && createBackdropMouseDownRef.current) closeCreateDialog(); createBackdropMouseDownRef.current = false; }}
            >
                <form
                    className="modal-content"
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="task-management-dialog-title"
                    onMouseDown={e => e.stopPropagation()}
                    onClick={e => e.stopPropagation()}
                    onKeyDown={e => { if (e.key === 'Escape') closeCreateDialog(); }}
                    onSubmit={e => { e.preventDefault(); void submitCreateTask(); }}
                    style={{ width: '420px', maxWidth: '92vw', textAlign: 'left' }}
                >
                    <div className="modal-header">
                        <h3 id="task-management-dialog-title" style={{ fontSize: '0.88rem', margin: 0 }}>
                            {cloudCreateSelected
                                ? textForLang(lang, 'Create cloud workspace task', '创建云端工作区任务', '建立雲端工作區任務')
                                : newTaskMode === 'coding_dev'
                                    ? textForLang(lang, 'Create local coding task', '创建本地编程任务', '建立本機程式任務')
                                    : newTaskMode === 'remote_coding_dev'
                                        ? textForLang(lang, 'Create remote coding task', '创建远程编程任务', '建立遠端程式任務')
                                        : textForLang(lang, 'Create task', '创建任务', '建立任務')}
                        </h3>
                        <button type="button" className="btn-close" onClick={closeCreateDialog} disabled={creatingTask}>X</button>
                    </div>
                    <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '12px' }}>
                        <div data-testid="task-create-guidance" style={{ padding: '9px 10px', borderRadius: '8px', border: '1px solid color-mix(in srgb, var(--theme-primary) 24%, var(--theme-border))', background: 'color-mix(in srgb, var(--theme-primary) 6%, transparent)', color: 'var(--theme-text-secondary)', fontSize: '0.72rem', lineHeight: 1.5 }}>
                            {textForLang(lang, 'Choose a task type, including Cloud workspace. After it opens, enter your request in the AI assistant.', '先选择任务类型（含云端工作区）；创建后请直接在 AI 助手中输入任务命令。', '先選擇任務類型（含雲端工作區）；建立後請直接在 AI 助手中輸入任務命令。')}
                        </div>
                        {cloudEntitlement?.hub_unavailable && (
                            <div
                                role="status"
                                data-testid="task-cloud-workspace-hub-banner"
                                style={{ padding: '8px 10px', borderRadius: '8px', border: '1px solid color-mix(in srgb, #d97706 48%, var(--theme-border))', background: 'color-mix(in srgb, #f59e0b 12%, transparent)', color: '#a16207', fontSize: '0.72rem', lineHeight: 1.45 }}
                            >
                                {cloudEntitlement.banner?.trim()
                                    || textForLang(lang, 'Hub is unavailable; cloud workspaces are temporarily unavailable.', 'Hub 不可用，云端工作区暂不可用', 'Hub 不可用，雲端工作區暫不可用')}
                            </div>
                        )}
                        <div>
                            <div style={{ marginBottom: '6px', fontSize: '0.74rem', fontWeight: 700, color: 'var(--theme-text-secondary)' }}>{textForLang(lang, 'Task type', '任务类型', '任務類型')}</div>
                            <div role="group" data-testid="task-workspace-kind" aria-label={textForLang(lang, 'Task type', '任务类型', '任務類型')} style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '6px' }}>
                                {([
                                    { id: 'chat' as const, testId: 'task-workspace-kind-local', label: textForLang(lang, 'Chat', '对话', '對話'), detail: textForLang(lang, 'General assistant', '通用助手', '通用助手') },
                                    { id: 'coding_dev' as const, label: textForLang(lang, 'Coding', '本地编程', '本機程式'), detail: textForLang(lang, 'Local workspace', '本地工作目录', '本機工作目錄') },
                                    { id: 'remote_coding_dev' as const, label: textForLang(lang, 'Remote', '远程编程', '遠端程式'), detail: textForLang(lang, 'SSH workspace', 'SSH 工作目录', 'SSH 工作目錄') },
                                    { id: 'cloud' as const, testId: 'task-workspace-kind-cloud', label: textForLang(lang, 'Cloud workspace', '云端工作区', '雲端工作區'), detail: textForLang(lang, 'Hub sync, continue on another PC', 'Hub 同步，换电脑继续', 'Hub 同步，換電腦繼續') },
                                ] satisfies Array<{ id: CreateTaskTypeId; testId?: string; label: string; detail: string }>).map(opt => {
                                    const isCloud = opt.id === 'cloud';
                                    const nextMode: '' | 'coding_dev' | 'remote_coding_dev' = opt.id === 'coding_dev'
                                        ? 'coding_dev'
                                        : opt.id === 'remote_coding_dev'
                                            ? 'remote_coding_dev'
                                            : '';
                                    const active = isCloud
                                        ? workspaceKind === 'cloud'
                                        : workspaceKind !== 'cloud' && newTaskMode === nextMode;
                                    const cloudLocked = isCloud && !cloudGranted;
                                    const disabled = creatingTask || cloudWorkspaceBusy || cloudLocked;
                                    const buttonId = opt.id === 'coding_dev'
                                        ? 'task-management-coding-mode'
                                        : opt.id === 'remote_coding_dev'
                                            ? 'task-management-remote-coding-mode'
                                            : isCloud
                                                ? 'task-management-cloud-workspace-mode'
                                                : 'task-management-chat-mode';
                                    return (
                                        <button
                                            key={`create-type-${opt.id}`}
                                            type="button"
                                            id={buttonId}
                                            data-testid={opt.testId}
                                            aria-pressed={active}
                                            aria-label={opt.label}
                                            title={cloudLocked ? cloudDeniedReason : opt.detail}
                                            disabled={disabled}
                                            onClick={() => selectCreateTaskType(opt.id)}
                                            style={{ minWidth: 0, minHeight: '58px', border: active ? '1px solid color-mix(in srgb, var(--theme-primary) 58%, var(--theme-border))' : '1px solid var(--theme-border)', borderRadius: '8px', padding: '7px 8px', textAlign: 'left', cursor: disabled ? 'default' : 'pointer', color: active ? 'var(--theme-primary)' : 'var(--theme-text-primary)', background: active ? 'color-mix(in srgb, var(--theme-primary) 10%, var(--theme-surface))' : 'var(--theme-surface-muted)', opacity: disabled ? 0.55 : 1 }}
                                        >
                                            <span style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: '0.74rem', fontWeight: 700 }}>{opt.label}</span>
                                            <span style={{ display: 'block', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', marginTop: '3px', color: active ? 'var(--theme-primary)' : 'var(--theme-text-muted)', fontSize: '0.64rem', opacity: active ? 0.88 : 1 }}>{opt.detail}</span>
                                        </button>
                                    );
                                })}
                            </div>
                            {!cloudGranted && !cloudEntitlement?.hub_unavailable && (
                                <div data-testid="task-cloud-workspace-denied" style={{ marginTop: '6px', fontSize: '0.68rem', color: 'var(--theme-text-muted)', lineHeight: 1.45 }}>
                                    {cloudDeniedReason}
                                </div>
                            )}
                        </div>
                        {newTaskMode !== 'remote_coding_dev' && !cloudCreateSelected && (
                        <div style={{ display: 'flex', flexDirection: 'column', gap: '7px', padding: '9px 10px', borderRadius: '8px', border: '1px solid var(--theme-border)', background: 'var(--theme-surface-muted)' }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: '7px', minWidth: 0 }}>
                                <span style={{ display: 'inline-flex', alignItems: 'center', gap: '5px', color: 'var(--theme-text-secondary)', fontSize: '0.72rem', whiteSpace: 'nowrap' }}>
                                    <ProjectSearchIcon name="desktop" size={14} />
                                    {textForLang(lang, 'Working directory', '\u5de5\u4f5c\u76ee\u5f55', '\u5de5\u4f5c\u76ee\u9304')}
                                </span>
                                <button
                                    type="button"
                                    id="task-working-directory"
                                    onClick={() => { void selectWorkingDir(); }}
                                    disabled={creatingTask || selectingWorkingDir}
                                    aria-label={textForLang(lang, 'Choose working folder', '\u9009\u62e9\u5de5\u4f5c\u6587\u4ef6\u5939', '\u9078\u64c7\u5de5\u4f5c\u8cc7\u6599\u593e')}
                                    title={newTaskWorkingDir || textForLang(lang, 'Choose working folder', '\u9009\u62e9\u5de5\u4f5c\u6587\u4ef6\u5939', '\u9078\u64c7\u5de5\u4f5c\u8cc7\u6599\u593e')}
                                    style={{ maxWidth: '250px', minWidth: 0, display: 'inline-flex', alignItems: 'center', gap: '5px', border: '1px solid color-mix(in srgb, var(--theme-primary) 22%, transparent)', borderRadius: '6px', background: 'color-mix(in srgb, var(--theme-primary) 9%, transparent)', color: 'var(--theme-primary)', cursor: creatingTask || selectingWorkingDir ? 'default' : 'pointer', padding: '5px 8px', fontSize: '0.72rem', lineHeight: 1.2, opacity: creatingTask || selectingWorkingDir ? 0.58 : 1 }}
                                >
                                    <ProjectSearchIcon name="folder" size={14} />
                                    <span style={{ minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                                        {newTaskWorkingDir || (selectingWorkingDir ? textForLang(lang, 'Choosing...', '\u9009\u62e9\u4e2d...', '\u9078\u64c7\u4e2d...') : textForLang(lang, 'Choose folder', '\u9009\u62e9\u6587\u4ef6\u5939', '\u9078\u64c7\u8cc7\u6599\u593e'))}
                                    </span>
                                </button>
                                {newTaskWorkingDir && !creatingTask && (
                                    <button
                                        type="button"
                                        onClick={() => setNewTaskWorkingDir('')}
                                        aria-label={textForLang(lang, 'Clear directory', '\u6e05\u9664\u76ee\u5f55', '\u6e05\u9664\u76ee\u9304')}
                                        title={textForLang(lang, 'Clear directory', '\u6e05\u9664\u76ee\u5f55', '\u6e05\u9664\u76ee\u9304')}
                                        style={{ flexShrink: 0, width: '18px', height: '18px', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', border: 'none', borderRadius: '50%', background: 'color-mix(in srgb, var(--theme-text-muted) 15%, transparent)', color: 'var(--theme-text-muted)', cursor: 'pointer', padding: 0, fontSize: '11px', lineHeight: 1 }}
                                    >×</button>
                                )}
                            </div>
                        </div>
                        )}
                        {cloudCreateSelected && (
                            <div data-testid="task-cloud-workspace-list" style={{ display: 'flex', flexDirection: 'column', gap: '8px', padding: '9px 10px', borderRadius: '8px', border: '1px solid var(--theme-border)', background: 'var(--theme-surface-muted)' }}>
                                <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px' }}>
                                    <span style={{ fontSize: '0.72rem', fontWeight: 700, color: 'var(--theme-text-secondary)' }}>
                                        {textForLang(lang, 'Cloud workspaces', '云端工作区', '雲端工作區')}
                                        {cloudQuota > 0 ? ` (${cloudWorkspaces.length}/${cloudQuota})` : ''}
                                    </span>
                                    <button
                                        type="button"
                                        data-testid="task-cloud-workspace-create"
                                        disabled={creatingTask || cloudWorkspaceBusy || cloudQuotaReached}
                                        title={cloudQuotaReached
                                            ? textForLang(lang, 'Cloud workspace quota reached', '已达云端工作区配额', '已達雲端工作區配額')
                                            : textForLang(lang, 'New cloud workspace', '新建云端工作区', '新建雲端工作區')}
                                        onClick={() => { void createNewCloudWorkspace(); }}
                                        style={{ border: '1px solid color-mix(in srgb, var(--theme-primary) 28%, var(--theme-border))', borderRadius: '6px', background: 'color-mix(in srgb, var(--theme-primary) 9%, transparent)', color: 'var(--theme-primary)', cursor: creatingTask || cloudWorkspaceBusy || cloudQuotaReached ? 'default' : 'pointer', padding: '4px 8px', fontSize: '0.68rem', fontWeight: 700, opacity: creatingTask || cloudWorkspaceBusy || cloudQuotaReached ? 0.5 : 1 }}
                                    >
                                        {textForLang(lang, 'New cloud workspace', '新建云端工作区', '新建雲端工作區')}
                                    </button>
                                </div>
                                {cloudWorkspaces.length === 0 && (
                                    <div style={{ fontSize: '0.7rem', color: 'var(--theme-text-muted)', lineHeight: 1.4 }}>
                                        {textForLang(lang, 'No cloud workspace yet. Create one to continue.', '暂无云端工作区，请先新建。', '暫無雲端工作區，請先新建。')}
                                    </div>
                                )}
                                {cloudWorkspaces.map(row => {
                                    const selected = selectedCloudWorkspaceId === row.id;
                                    const lastUsed = formatCloudWorkspaceLastUsed(row.updated_at, lang);
                                    const sizeText = formatCloudWorkspaceBytes(Number(row.used_bytes) || 0, lang);
                                    const renaming = renamingCloudWorkspaceId === row.id;
                                    const confirmingDelete = deleteConfirmCloudWorkspaceId === row.id;
                                    return (
                                        <div
                                            key={row.id}
                                            data-testid="task-cloud-workspace-row"
                                            data-workspace-id={row.id}
                                            style={{ display: 'flex', flexDirection: 'column', gap: '4px', padding: '7px 8px', borderRadius: '7px', border: selected ? '1px solid color-mix(in srgb, var(--theme-primary) 58%, var(--theme-border))' : '1px solid var(--theme-border)', background: selected ? 'color-mix(in srgb, var(--theme-primary) 8%, var(--theme-surface))' : 'var(--theme-surface)' }}
                                        >
                                            <button
                                                type="button"
                                                disabled={creatingTask || cloudWorkspaceBusy}
                                                onClick={() => { setSelectedCloudWorkspaceId(row.id || ''); setCreateError(''); }}
                                                style={{ border: 'none', background: 'transparent', padding: 0, textAlign: 'left', cursor: creatingTask || cloudWorkspaceBusy ? 'default' : 'pointer', color: 'inherit' }}
                                            >
                                                <span style={{ display: 'block', fontSize: '0.76rem', fontWeight: 700, color: 'var(--theme-text-primary)' }}>{row.name || row.id}</span>
                                                <span style={{ display: 'block', marginTop: '2px', fontSize: '0.64rem', color: 'var(--theme-text-muted)', lineHeight: 1.35 }}>
                                                    {[lastUsed, sizeText].filter(Boolean).join(' · ')}
                                                </span>
                                                {boundCloudWorkspaceIds.has(row.id || '') && (
                                                    <span data-testid="task-cloud-workspace-bound" style={{ display: 'block', marginTop: '2px', fontSize: '0.64rem', color: 'var(--theme-primary)', lineHeight: 1.35 }}>
                                                        {textForLang(lang, 'Already has a task. Create & open makes a new workspace and a new panel.', '已有任务。「创建并打开」会再建一个工作区并打开新面板。', '已有任務。「建立並開啟」會再建一個工作區並開啟新面板。')}
                                                    </span>
                                                )}
                                                <CloudWorkspaceLeaseNote row={row} lang={lang} />
                                            </button>
                                            {renaming ? (
                                                <div style={{ display: 'flex', gap: '6px', alignItems: 'center' }}>
                                                    <input
                                                        value={renameCloudWorkspaceValue}
                                                        onChange={e => setRenameCloudWorkspaceValue(e.target.value)}
                                                        disabled={cloudWorkspaceBusy}
                                                        aria-label={textForLang(lang, 'Workspace name', '工作区名称', '工作區名稱')}
                                                        onKeyDown={e => {
                                                            if (e.key === 'Enter') { e.preventDefault(); void renameSelectedCloudWorkspace(row.id || ''); }
                                                            if (e.key === 'Escape') { e.preventDefault(); resetCloudWorkspaceEditors(); }
                                                        }}
                                                        style={{ flex: 1, minWidth: 0, boxSizing: 'border-box', fontSize: '0.74rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '4px 6px', outline: 'none' }}
                                                    />
                                                    <button type="button" className="btn-primary" style={{ fontSize: '0.66rem', padding: '3px 8px' }} disabled={cloudWorkspaceBusy || !renameCloudWorkspaceValue.trim()} onClick={() => { void renameSelectedCloudWorkspace(row.id || ''); }}>{textForLang(lang, 'Save', '保存', '儲存')}</button>
                                                    <button type="button" className="btn-secondary" style={{ fontSize: '0.66rem', padding: '3px 8px' }} disabled={cloudWorkspaceBusy} onClick={resetCloudWorkspaceEditors}>{textForLang(lang, 'Cancel', '取消', '取消')}</button>
                                                </div>
                                            ) : confirmingDelete ? (
                                                <div data-testid="task-cloud-workspace-delete-confirm" style={{ display: 'flex', flexDirection: 'column', gap: '6px' }}>
                                                    <span style={{ fontSize: '0.66rem', color: 'var(--theme-text-secondary)', lineHeight: 1.4 }}>
                                                        {textForLang(lang, 'Deleted workspaces can be restored from Recently deleted within 7 days.', '删除后 7 天内可从「最近删除」恢复。', '刪除後 7 天內可從「最近刪除」恢復。')}
                                                    </span>
                                                    <div style={{ display: 'flex', gap: '6px' }}>
                                                        <button type="button" className="btn-primary" style={{ fontSize: '0.66rem', padding: '3px 8px' }} disabled={cloudWorkspaceBusy} onClick={() => { void deleteSelectedCloudWorkspace(row.id || ''); }}>{textForLang(lang, 'Delete', '确认删除', '確認刪除')}</button>
                                                        <button type="button" className="btn-secondary" style={{ fontSize: '0.66rem', padding: '3px 8px' }} disabled={cloudWorkspaceBusy} onClick={resetCloudWorkspaceEditors}>{textForLang(lang, 'Cancel', '取消', '取消')}</button>
                                                    </div>
                                                </div>
                                            ) : (
                                                <div style={{ display: 'flex', gap: '6px' }}>
                                                    <button
                                                        type="button"
                                                        data-testid="task-cloud-workspace-rename"
                                                        disabled={creatingTask || cloudWorkspaceBusy}
                                                        onClick={() => { setRenamingCloudWorkspaceId(row.id || ''); setRenameCloudWorkspaceValue(row.name || ''); setDeleteConfirmCloudWorkspaceId(''); }}
                                                        style={{ border: 'none', background: 'transparent', color: 'var(--theme-primary)', cursor: 'pointer', fontSize: '0.66rem', padding: 0 }}
                                                    >{textForLang(lang, 'Rename', '重命名', '重命名')}</button>
                                                    <button
                                                        type="button"
                                                        data-testid="task-cloud-workspace-delete"
                                                        disabled={creatingTask || cloudWorkspaceBusy}
                                                        onClick={() => { setDeleteConfirmCloudWorkspaceId(row.id || ''); setRenamingCloudWorkspaceId(''); }}
                                                        style={{ border: 'none', background: 'transparent', color: 'var(--theme-text-muted)', cursor: 'pointer', fontSize: '0.66rem', padding: 0 }}
                                                    >{textForLang(lang, 'Delete', '删除', '刪除')}</button>
                                                </div>
                                            )}
                                        </div>
                                    );
                                })}
                                {cloudDeleted.length > 0 && (
                                    <div data-testid="task-cloud-workspace-deleted" style={{ display: 'flex', flexDirection: 'column', gap: '6px', marginTop: '4px' }}>
                                        <div style={{ fontSize: '0.7rem', fontWeight: 700, color: 'var(--theme-text-secondary)' }}>
                                            {textForLang(lang, 'Recently deleted', '最近删除', '最近刪除')}
                                        </div>
                                        {cloudDeleted.map(row => (
                                            <div key={row.id} style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '8px', padding: '6px 8px', borderRadius: '7px', border: '1px dashed var(--theme-border)' }}>
                                                <span style={{ minWidth: 0 }}>
                                                    <span style={{ display: 'block', fontSize: '0.72rem', color: 'var(--theme-text-primary)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{row.name || row.id}</span>
                                                    <span style={{ display: 'block', fontSize: '0.62rem', color: 'var(--theme-text-muted)' }}>
                                                        {textForLang(lang, 'Restorable for 7 days', '7 天内可恢复', '7 天內可恢復')}
                                                    </span>
                                                </span>
                                                <button
                                                    type="button"
                                                    data-testid="task-cloud-workspace-restore"
                                                    disabled={creatingTask || cloudWorkspaceBusy || cloudQuotaReached}
                                                    title={cloudQuotaReached
                                                        ? textForLang(lang, 'Cloud workspace quota reached', '已达云端工作区配额', '已達雲端工作區配額')
                                                        : textForLang(lang, 'Restore', '恢复', '恢復')}
                                                    onClick={() => { void restoreDeletedCloudWorkspace(row.id || ''); }}
                                                    style={{ flexShrink: 0, border: '1px solid var(--theme-border)', borderRadius: '6px', background: 'var(--theme-surface)', color: 'var(--theme-primary)', cursor: creatingTask || cloudWorkspaceBusy || cloudQuotaReached ? 'default' : 'pointer', padding: '3px 8px', fontSize: '0.66rem', opacity: creatingTask || cloudWorkspaceBusy || cloudQuotaReached ? 0.5 : 1 }}
                                                >
                                                    {textForLang(lang, 'Restore', '恢复', '恢復')}
                                                </button>
                                            </div>
                                        ))}
                                    </div>
                                )}
                            </div>
                        )}
                        {newTaskMode === 'remote_coding_dev' && (
                            <div data-testid="remote-coding-fields" style={{ display: 'flex', flexDirection: 'column', gap: '8px', padding: '8px 10px', borderRadius: '8px', border: '1px solid color-mix(in srgb, var(--theme-primary) 28%, var(--theme-border))', background: 'color-mix(in srgb, var(--theme-primary) 7%, transparent)' }}>
                                <div style={{ fontSize: '0.72rem', fontWeight: 700, color: 'var(--theme-primary)' }}>
                                    {textForLang(lang, 'Remote SSH connection', '远程 SSH 连接', '遠端 SSH 連線')}
                                </div>
                                <div style={{ display: 'grid', gridTemplateColumns: '1fr 88px', gap: '8px' }}>
                                    <div>
                                        <label htmlFor="task-remote-host" style={{ display: 'block', fontSize: '0.68rem', color: 'var(--theme-text-secondary)', marginBottom: 4 }}>{textForLang(lang, 'Host / domain', '主机 IP / 域名', '主機 IP / 網域')}</label>
                                        <input id="task-remote-host" value={remoteHost} onChange={e => setRemoteHost(e.target.value)} disabled={creatingTask} placeholder="192.168.1.10" autoComplete="off" style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.78rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '6px 8px', outline: 'none' }} />
                                    </div>
                                    <div>
                                        <label htmlFor="task-remote-port" style={{ display: 'block', fontSize: '0.68rem', color: 'var(--theme-text-secondary)', marginBottom: 4 }}>{textForLang(lang, 'Port', '端口', '連接埠')}</label>
                                        <input id="task-remote-port" value={remotePort} onChange={e => setRemotePort(e.target.value)} disabled={creatingTask} inputMode="numeric" placeholder="22" style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.78rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '6px 8px', outline: 'none' }} />
                                    </div>
                                </div>
                                <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px' }}>
                                    <div>
                                        <label htmlFor="task-remote-user" style={{ display: 'block', fontSize: '0.68rem', color: 'var(--theme-text-secondary)', marginBottom: 4 }}>{textForLang(lang, 'Username', '用户名', '使用者名稱')}</label>
                                        <input id="task-remote-user" value={remoteUser} onChange={e => setRemoteUser(e.target.value)} disabled={creatingTask} placeholder="root" autoComplete="username" style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.78rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '6px 8px', outline: 'none' }} />
                                    </div>
                                    <div>
                                        <label htmlFor="task-remote-password" style={{ display: 'block', fontSize: '0.68rem', color: 'var(--theme-text-secondary)', marginBottom: 4 }}>{textForLang(lang, 'Password', '密码', '密碼')}</label>
                                        <input id="task-remote-password" type="password" value={remotePassword} onChange={e => setRemotePassword(e.target.value)} disabled={creatingTask} autoComplete="current-password" style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.78rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '6px 8px', outline: 'none' }} />
                                    </div>
                                </div>
                                <div>
                                    <label htmlFor="task-remote-workdir" style={{ display: 'block', fontSize: '0.68rem', color: 'var(--theme-text-secondary)', marginBottom: 4 }}>{textForLang(lang, 'Remote work directory', '远程工作目录', '遠端工作目錄')}</label>
                                    <input id="task-remote-workdir" value={remoteWorkDir} onChange={e => setRemoteWorkDir(e.target.value)} disabled={creatingTask} placeholder="/home/user/project" style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.78rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '6px 8px', outline: 'none' }} />
                                </div>
                                <div style={{ fontSize: '0.66rem', color: 'var(--theme-text-muted)', lineHeight: 1.4 }}>
                                    {textForLang(lang, 'Password is remembered only on this device for reconnect; it is never saved to the task or uploaded. Source preview opens on the right like local coding.', '密码仅保存在本机用于断线重连，不会写入任务或上传。执行时右侧会像本地编程一样显示源码预览。', '密碼僅儲存在本機用於斷線重連，不會寫入任務或上傳。執行時右側會像本機程式開發一樣顯示原始碼預覽。')}
                                </div>
                            </div>
                        )}
                    </div>
                    {createError && (
                        <div role="alert" data-testid="create-task-error" style={{ marginTop: '8px', padding: '7px 10px', borderRadius: '6px', border: '1px solid color-mix(in srgb, var(--theme-danger, #ef4444) 35%, var(--theme-border))', background: 'color-mix(in srgb, var(--theme-danger, #ef4444) 10%, transparent)', color: 'var(--theme-danger, #ef4444)', fontSize: '0.72rem', lineHeight: 1.4 }}>
                            {createError}
                        </div>
                    )}
                    <div className="modal-footer" style={{ justifyContent: 'flex-end', alignItems: 'center', flexWrap: 'wrap', gap: '8px' }}>
                        <div style={{ display: 'inline-flex', alignItems: 'center', gap: '8px' }}>
                            <button type="button" className="btn-secondary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={closeCreateDialog} disabled={creatingTask}>
                                {textForLang(lang, 'Cancel', '取消', '取消')}
                            </button>
                            {cloudCreateSelected && boundCloudWorkspaceIds.has(selectedCloudWorkspaceId.trim()) ? (
                                <button
                                    type="button"
                                    className="btn-secondary"
                                    data-testid="task-cloud-open-existing"
                                    style={{ fontSize: '0.78rem', padding: '4px 14px' }}
                                    disabled={creatingTask || cloudWorkspaceBusy}
                                    onClick={() => { void submitCreateTask({ resumeExisting: true }); }}
                                >
                                    {textForLang(lang, 'Open existing', '打开现有任务', '打開現有任務')}
                                </button>
                            ) : null}
                            <button
                                type="submit"
                                className="btn-primary"
                                style={{ fontSize: '0.78rem', padding: '4px 14px' }}
                                disabled={
                                    creatingTask
                                    || cloudWorkspaceBusy
                                    || (newTaskMode === 'remote_coding_dev' && (!remoteHost.trim() || !remoteUser.trim() || !remotePassword || !remoteWorkDir.trim()))
                                    || (cloudCreateSelected && !selectedCloudWorkspaceId.trim())
                                    || (cloudCreateSelected && boundCloudWorkspaceIds.has(selectedCloudWorkspaceId.trim()) && cloudQuotaReached)
                                }
                            >
                                {textForLang(lang, 'Create & open', '创建并打开', '建立並開啟')}
                            </button>
                        </div>
                    </div>
                </form>
            </div>,
            document.body,
        )}

        {taskContextMenu && (<>
            <div style={{ position: 'fixed', inset: 0, zIndex: 9998 }} onClick={() => setTaskContextMenu(null)} />
            <div data-testid="task-context-menu" style={{ position: 'fixed', left: taskContextMenu.x, top: taskContextMenu.y, zIndex: 9999, background: 'var(--theme-page-bg)', border: '1px solid var(--theme-border)', borderRadius: '6px', boxShadow: '0 4px 12px rgba(0,0,0,0.18)', padding: '4px 0', minWidth: '168px' }}>
                {buildTaskContextMenuItems({
                    lang,
                    menu: taskContextMenu,
                    openProjectTabPaths,
                    openExpertTabIDs,
                    setRenamingTaskPath,
                    setRenameValue,
                    setTaskContextMenu,
                    pinTask,
                    removeTask: handleRemoveTask,
                    refreshTasks,
                    openEditRemoteDialog,
                    browseCloudWorkspace,
                }).map(item => (
                    <div
                        key={item.label}
                        data-testid={item.testId}
                        data-disabled={item.disabled ? 'true' : undefined}
                        role="menuitem"
                        aria-disabled={item.disabled || undefined}
                        title={item.title}
                        onClick={() => {
                            if (item.disabled) return;
                            void item.action();
                        }}
                        style={{
                            display: 'flex',
                            alignItems: 'center',
                            gap: '8px',
                            padding: '7px 12px',
                            cursor: item.disabled ? 'not-allowed' : 'pointer',
                            fontSize: '0.78rem',
                            color: item.disabled ? 'var(--theme-text-muted)' : 'var(--theme-text-primary)',
                            opacity: item.disabled ? 0.55 : 1,
                        }}
                        onMouseEnter={e => {
                            if (item.disabled) return;
                            e.currentTarget.style.background = 'color-mix(in srgb, var(--theme-text-primary) 8%, transparent)';
                        }}
                        onMouseLeave={e => { e.currentTarget.style.background = 'transparent'; }}
                    >
                        <span style={{ width: 28, flexShrink: 0, opacity: 0.75, fontSize: '0.68rem' }}>{item.icon}</span>
                        <span>{item.label}</span>
                    </div>
                ))}
            </div>
        </>)}

        {editRemoteOpen && createPortal(
            <div
                className="modal-backdrop"
                data-testid="edit-remote-ssh-dialog"
                data-ai-theme={getPortalThemeMode(themeMode)}
                data-ai-dark-scheme={getPortalDarkScheme()}
                data-ai-light-scheme={getPortalLightScheme()}
                style={{ zIndex: TASK_CREATE_DIALOG_Z_INDEX }}
                onMouseDown={e => { editBackdropMouseDownRef.current = e.target === e.currentTarget; }}
                onClick={e => { if (e.target === e.currentTarget && editBackdropMouseDownRef.current) closeEditRemoteDialog(); editBackdropMouseDownRef.current = false; }}
            >
                <form
                    className="modal-content"
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="edit-remote-ssh-title"
                    onClick={e => e.stopPropagation()}
                    onKeyDown={e => { if (e.key === 'Escape') closeEditRemoteDialog(); }}
                    onSubmit={e => { e.preventDefault(); void submitEditRemote(); }}
                    style={{ width: '440px', maxWidth: '92vw', textAlign: 'left' }}
                >
                    <div className="modal-header">
                        <h3 id="edit-remote-ssh-title" style={{ fontSize: '0.88rem', margin: 0 }}>
                            {textForLang(lang, 'Edit remote SSH', '编辑远程 SSH', '編輯遠端 SSH')}
                        </h3>
                        <button type="button" className="btn-close" onClick={closeEditRemoteDialog} disabled={editSaving || editTesting}>×</button>
                    </div>
                    <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '10px' }}>
                        <div style={{ fontSize: '0.72rem', color: 'var(--theme-text-muted)', lineHeight: 1.4 }}>
                            {editRemoteName || editRemotePath}
                        </div>
                        <div data-testid="edit-remote-coding-fields" style={{ display: 'flex', flexDirection: 'column', gap: '8px', padding: '8px 10px', borderRadius: '8px', border: '1px solid color-mix(in srgb, var(--theme-primary) 28%, var(--theme-border))', background: 'color-mix(in srgb, var(--theme-primary) 7%, transparent)' }}>
                            <div style={{ display: 'grid', gridTemplateColumns: '1fr 88px', gap: '8px' }}>
                                <div>
                                    <label htmlFor="edit-remote-host" style={{ display: 'block', fontSize: '0.68rem', color: 'var(--theme-text-secondary)', marginBottom: 4 }}>{textForLang(lang, 'Host / domain', '主机 IP / 域名', '主機 IP / 網域')}</label>
                                    <input id="edit-remote-host" data-testid="edit-remote-host" value={editHost} onChange={e => setEditHost(e.target.value)} disabled={editSaving || editTesting} placeholder="192.168.1.10" autoComplete="off" autoFocus style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.78rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '6px 8px', outline: 'none' }} />
                                </div>
                                <div>
                                    <label htmlFor="edit-remote-port" style={{ display: 'block', fontSize: '0.68rem', color: 'var(--theme-text-secondary)', marginBottom: 4 }}>{textForLang(lang, 'Port', '端口', '連接埠')}</label>
                                    <input id="edit-remote-port" data-testid="edit-remote-port" value={editPort} onChange={e => setEditPort(e.target.value)} disabled={editSaving || editTesting} inputMode="numeric" placeholder="22" style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.78rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '6px 8px', outline: 'none' }} />
                                </div>
                            </div>
                            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: '8px' }}>
                                <div>
                                    <label htmlFor="edit-remote-user" style={{ display: 'block', fontSize: '0.68rem', color: 'var(--theme-text-secondary)', marginBottom: 4 }}>{textForLang(lang, 'Username', '用户名', '使用者名稱')}</label>
                                    <input id="edit-remote-user" data-testid="edit-remote-user" value={editUser} onChange={e => setEditUser(e.target.value)} disabled={editSaving || editTesting} placeholder="root" autoComplete="username" style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.78rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '6px 8px', outline: 'none' }} />
                                </div>
                                <div>
                                    <label htmlFor="edit-remote-password" style={{ display: 'block', fontSize: '0.68rem', color: 'var(--theme-text-secondary)', marginBottom: 4 }}>{textForLang(lang, 'Password', '密码', '密碼')}</label>
                                    <input id="edit-remote-password" data-testid="edit-remote-password" type="password" value={editPassword} onChange={e => setEditPassword(e.target.value)} disabled={editSaving || editTesting} autoComplete="off" placeholder={textForLang(lang, 'Not saved', '不落盘', '不落盤')} style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.78rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '6px 8px', outline: 'none' }} />
                                </div>
                            </div>
                            <div>
                                <label htmlFor="edit-remote-workdir" style={{ display: 'block', fontSize: '0.68rem', color: 'var(--theme-text-secondary)', marginBottom: 4 }}>{textForLang(lang, 'Remote work directory', '远程工作目录', '遠端工作目錄')}</label>
                                <input id="edit-remote-workdir" data-testid="edit-remote-workdir" value={editWorkDir} onChange={e => setEditWorkDir(e.target.value)} disabled={editSaving || editTesting} placeholder="/home/user/project" style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.78rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '6px 8px', outline: 'none' }} />
                            </div>
                            <div style={{ fontSize: '0.66rem', color: 'var(--theme-text-muted)', lineHeight: 1.4 }}>
                                {textForLang(lang, 'Host / user / port / workdir are saved on the task. Password is only used for Test / reconnect and is never stored.', '主机、用户、端口、工作目录会保存到任务；密码仅用于测试/重连，永不落盘。', '主機、使用者、連接埠、工作目錄會儲存到任務；密碼僅用於測試/重連，永不落盤。')}
                            </div>
                        </div>
                        {editError && (
                            <div role="alert" data-testid="edit-remote-error" style={{ padding: '7px 10px', borderRadius: '6px', border: '1px solid color-mix(in srgb, var(--theme-danger, #ef4444) 35%, var(--theme-border))', background: 'color-mix(in srgb, var(--theme-danger, #ef4444) 10%, transparent)', color: 'var(--theme-danger, #ef4444)', fontSize: '0.72rem', lineHeight: 1.4 }}>
                                {editError}
                            </div>
                        )}
                        {editInfo && !editError && (
                            <div role="status" data-testid="edit-remote-info" style={{ padding: '7px 10px', borderRadius: '6px', border: '1px solid color-mix(in srgb, #16a34a 35%, var(--theme-border))', background: 'color-mix(in srgb, #16a34a 10%, transparent)', color: 'var(--theme-text-primary)', fontSize: '0.72rem', lineHeight: 1.4 }}>
                                {editInfo}
                            </div>
                        )}
                    </div>
                    <div className="modal-footer" style={{ justifyContent: 'flex-end', alignItems: 'center', flexWrap: 'wrap', gap: '8px' }}>
                        <button
                            type="button"
                            data-testid="edit-remote-test-ssh"
                            className="btn-secondary"
                            style={{ fontSize: '0.78rem', padding: '4px 14px' }}
                            disabled={editSaving || editTesting || !editHost.trim() || !editUser.trim() || !editPassword || !editWorkDir.trim()}
                            onClick={() => { void testEditRemoteSSH(); }}
                        >
                            {editTesting
                                ? textForLang(lang, 'Testing…', '测试中…', '測試中…')
                                : textForLang(lang, 'Test SSH', '测试连接', '測試連線')}
                        </button>
                        <div style={{ display: 'inline-flex', alignItems: 'center', gap: '8px' }}>
                            <button type="button" className="btn-secondary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={closeEditRemoteDialog} disabled={editSaving || editTesting}>
                                {textForLang(lang, 'Close', '关闭', '關閉')}
                            </button>
                            <button
                                type="submit"
                                data-testid="edit-remote-save"
                                className="btn-primary"
                                style={{ fontSize: '0.78rem', padding: '4px 14px' }}
                                disabled={editSaving || editTesting || !editHost.trim() || !editUser.trim() || !editWorkDir.trim()}
                            >
                                {editSaving
                                    ? textForLang(lang, 'Saving…', '保存中…', '儲存中…')
                                    : textForLang(lang, 'Save', '保存', '儲存')}
                            </button>
                        </div>
                    </div>
                </form>
            </div>,
            document.body,
        )}
    </div>
    );
};
