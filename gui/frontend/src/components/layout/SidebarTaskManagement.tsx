import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { GetProjectScene, GetRemoteCodingTaskMeta, SelectWorkingDir, TestRemoteSSHConnection, UpdateRemoteCodingTaskMeta } from '../../../wailsjs/go/main/App';
import { EventsEmit } from '../../../wailsjs/runtime';
import { EVENT_OPEN_CREATE_CODING_TASK, EVENT_PROJECT_TASK_CLOSED, type OpenCreateCodingTaskDetail } from '../../constants/events';
import { localizeText } from '../../i18n';
import { ProjectSearchIcon } from '../ai/ProjectSearchIcon';
import type { ProjectSceneDetail } from '../ai/ProjectSceneDetailPanel';
import { agentModeFromTaskTags, CODING_TASK_COMMAND_MAX_LEN, isPureCodingTaskTags, remoteCodingMetaFromTaskTags, remoteHostFromTaskTags, type PureCodingAgentMode } from '../ai/codingTaskMode';
import { normalizeProjectSessionPath } from '../ai/aiAssistantPanelSessionUtils';
import { extractErrorMessage } from '../ai/participantAddError';
import { normalizeWorkflowStatus, WorkflowStatus } from '../ai/workflowStatus';
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
} | null;

type TaskIconKind = 'pin' | 'reference' | 'coding' | 'remote_coding' | 'task';

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

/** Primary list icon: pure coding modes take priority over pin so the environment stays visible. */
const taskIconKindForProject = (proj: TaskManagementItem): TaskIconKind => {
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

const taskIconLabel = (kind: TaskIconKind, lang: string) => {
    if (kind === 'pin') return textForLang(lang, 'Pinned task', '\u7f6e\u9876\u4efb\u52a1', '\u7f6e\u9802\u4efb\u52d9');
    if (kind === 'reference') return textForLang(lang, 'Referenced task', '\u5f15\u7528\u4efb\u52a1', '\u5f15\u7528\u4efb\u52d9');
    if (kind === 'remote_coding') return textForLang(lang, 'Remote pure coding environment', '\u8fdc\u7a0b\u7eaf\u7f16\u7a0b\u73af\u5883', '\u9060\u7aef\u7d14\u7a0b\u5f0f\u74b0\u5883');
    if (kind === 'coding') return textForLang(lang, 'Local pure coding environment', '\u672c\u5730\u7eaf\u7f16\u7a0b\u73af\u5883', '\u672c\u6a5f\u7d14\u7a0b\u5f0f\u74b0\u5883');
    return textForLang(lang, 'Task', '\u4efb\u52a1', '\u4efb\u52d9');
};

const pureCodingBadgeLabel = (proj: TaskManagementItem, lang: string) => {
    if (isRemoteCodingTask(proj)) {
        const host = remoteHostFromTaskTags(proj.tags);
        const base = textForLang(lang, 'Remote coding', '\u8fdc\u7a0b\u7f16\u7a0b', '\u9060\u7aef\u7a0b\u5f0f');
        return host ? `${base} · ${host}` : base;
    }
    if (agentModeFromTaskTags(proj.tags) === 'coding_dev') {
        return textForLang(lang, 'Pure coding', '\u7eaf\u7f16\u7a0b', '\u7d14\u7a0b\u5f0f');
    }
    return '';
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

export function taskActivityLabel(value: string | undefined, lang: string, now = Date.now()): string {
    const timestamp = Date.parse(value || '');
    if (!Number.isFinite(timestamp)) return '';
    const seconds = Math.max(0, Math.round((now - timestamp) / 1000));
    if (seconds < 60) return textForLang(lang, 'Updated just now', '刚刚更新', '剛剛更新');
    const minutes = Math.floor(seconds / 60);
    if (minutes < 60) return textForLang(lang, `Updated ${minutes}m ago`, `${minutes} 分钟前更新`, `${minutes} 分鐘前更新`);
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return textForLang(lang, `Updated ${hours}h ago`, `${hours} 小时前更新`, `${hours} 小時前更新`);
    const days = Math.floor(hours / 24);
    if (days < 7) return textForLang(lang, `Updated ${days}d ago`, `${days} 天前更新`, `${days} 天前更新`);
    const date = new Date(timestamp);
    const dateText = Number.isNaN(date.getTime()) ? '' : date.toISOString().slice(0, 10);
    return dateText ? textForLang(lang, `Updated ${dateText}`, `${dateText} 更新`, `${dateText} 更新`) : '';
}

const TaskTypeIcon = ({ kind, lang }: { kind: TaskIconKind; lang: string }) => {
    const label = taskIconLabel(kind, lang);

    return (
        <span
            aria-label={label}
            title={label}
            style={{ flexShrink: 0, width: '24px', height: '18px', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', color: 'var(--theme-text-muted)', opacity: 0.92 }}
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
    <svg width="16" height="16" viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: 'block' }}>
        <path {...CREATE_TASK_ICON_PROPS} d="M7 4h7l4 4v12H7a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2z" />
        <path {...CREATE_TASK_ICON_PROPS} d="M14 4v5h5" />
        <path {...CREATE_TASK_ICON_PROPS} d="M9 14h6" />
        <path {...CREATE_TASK_ICON_PROPS} d="M12 11v6" />
    </svg>
);

const SaveTaskIcon = () => (
    <svg width="13" height="13" viewBox="0 0 24 24" aria-hidden="true" focusable="false" style={{ display: 'block', flexShrink: 0 }}>
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
    resumeTask: (projectPath: string) => Promise<void> | void;
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
        remote?: { host: string; port: number; user: string; password: string; workDir: string },
    ) => Promise<void> | void;
    refreshTasks: () => void;
    taskContextMenu: TaskContextMenu;
    setTaskContextMenu: (menu: TaskContextMenu) => void;
    renameTask: (projectPath: string, name: string) => Promise<unknown>;
    pinTask: (projectPath: string, pinned: boolean) => Promise<unknown>;
    hideTask: (projectPath: string) => Promise<unknown>;
    /**
     * Project paths that currently have an open assistant tab.
     * Open tabs cannot be removed from the task list context menu.
     */
    openProjectTabPaths?: string[];
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
    setRenamingTaskPath: (path: string | null) => void;
    setRenameValue: (value: string) => void;
    setTaskContextMenu: (menu: TaskContextMenu) => void;
    pinTask: (projectPath: string, pinned: boolean) => Promise<unknown>;
    hideTask: (projectPath: string) => Promise<unknown>;
    refreshTasks: () => void;
    openEditRemoteDialog: (projectPath: string, name: string, tags?: string[]) => void | Promise<void>;
}): TaskContextMenuItem[] {
    const {
        lang, menu, openProjectTabPaths, setRenamingTaskPath, setRenameValue,
        setTaskContextMenu, pinTask, hideTask, refreshTasks, openEditRemoteDialog,
    } = opts;
    const tabOpen = isProjectTabOpen(menu.projectPath, openProjectTabPaths);
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
            if (isProjectTabOpen(menu.projectPath, openProjectTabPaths)) {
                return;
            }
            await hideTask(menu.projectPath);
            emitProjectTaskClosed(menu.projectPath);
            refreshTasks();
            setTaskContextMenu(null);
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

const TASK_COMMAND_INPUT_ID = 'task-management-name-input';

const normalizeTaskCommandInput = (value?: string | null) => {
    // Preserve newlines (multi-line task commands), only collapse horizontal whitespace per line
    const lines = (value || '').split('\n').map(line => line.trim().replace(/[ \t]+/g, ' '));
    // Remove leading/trailing empty lines, collapse 3+ consecutive empty lines to 2
    const trimmed = lines.join('\n').trim().replace(/\n{3,}/g, '\n\n');
    // Limit characters (UTF-16 code units, consistent with HTML maxLength)
    return trimmed.slice(0, CODING_TASK_COMMAND_MAX_LEN);
};


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
}: SidebarTaskManagementProps) => {
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
    const [createError, setCreateError] = useState('');
    const [selectingWorkingDir, setSelectingWorkingDir] = useState(false);
    const [sceneDetailPath, setSceneDetailPath] = useState<string | null>(null);
    const [sceneDetail, setSceneDetail] = useState<ProjectSceneDetail | null>(null);
    const [sceneDetailLoading, setSceneDetailLoading] = useState(false);
    const [sceneDetailError, setSceneDetailError] = useState('');
    /** Invalidates an older evidence request when a row is closed or another row opens. */
    const sceneDetailRequestGenRef = useRef(0);
    const [openingTaskPath, setOpeningTaskPath] = useState<string | null>(null);
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
    const mountedRef = useRef(true);
    useEffect(() => {
        mountedRef.current = true;
        return () => {
            mountedRef.current = false;
            if (selectPlaceholderTimerRef.current != null) {
                clearTimeout(selectPlaceholderTimerRef.current);
                selectPlaceholderTimerRef.current = null;
            }
            selectPlaceholderGenRef.current += 1;
        };
    }, []);
    const visibleTasks = tasks.filter(proj => proj.has_output !== false);

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
            if (mode === 'coding_dev') {
                // Keep command focused for review of the filled task text.
                const el = document.getElementById(TASK_COMMAND_INPUT_ID) as HTMLTextAreaElement | null;
                el?.focus();
            }
        }, 0);
    };

    const openCreateDialog = (prefill?: {
        mode?: '' | PureCodingAgentMode;
        name?: string;
        workingDir?: string;
        remote?: OpenCreateCodingTaskDetail['remote'];
    }, opts?: { force?: boolean }) => {
        // The in-flight create guard applies to the manual "+" path. Event-driven
        // fallbacks pass force so a request is never dropped silently; dialog
        // controls stay disabled by `creatingTask` until the in-flight create settles.
        if (editSaving || editTesting) return;
        if (!opts?.force && creatingTaskRef.current) return;
        if (taskContextMenu) setTaskContextMenu(null);
        // Avoid stacking create + edit modals.
        if (editRemoteOpen) {
            editRemoteFetchGenRef.current += 1;
            setEditRemoteOpen(false);
            resetEditRemoteFields();
        }
        const mode = prefill?.mode ?? '';
        // Welcome templates: normalize whitespace + clamp to textarea maxLength.
        const name = typeof prefill?.name === 'string'
            ? normalizeTaskCommandInput(prefill.name)
            : '';
        setNewTaskName(name);
        setNewTaskMode(mode);
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
        setCreateDialogOpen(true);
        // A forced welcome event may replace the form while another create is
        // still in flight. Its eventual completion must not close or overwrite
        // this newer dialog.
        createTaskGenRef.current += 1;
        schedulePostOpenFocus(mode);
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
                                const portNum = Number(remote.port) || 22;
                                await createTask(taskName, undefined, 'remote_coding_dev', {
                                    host: remote.host.trim(),
                                    port: portNum > 0 && portNum < 65536 ? portNum : 22,
                                    user: remote.user.trim(),
                                    password: remote.password,
                                    workDir: remote.workDir.trim(),
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
        setCreateError('');
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

    const submitCreateTask = async () => {
        if (creatingTaskRef.current) return;
        const taskName = normalizeTaskCommandInput(newTaskName);
        if (!taskName) return;
        const isRemoteCreate = newTaskMode === 'remote_coding_dev';
        if (isRemoteCreate) {
            if (!remoteHost.trim() || !remoteUser.trim() || !remotePassword || !remoteWorkDir.trim()) {
                setCreateError(textForLang(lang, 'Please fill host, username, password, and remote work directory.', '请填写主机、用户名、密码和远程工作目录。', '請填寫主機、使用者名稱、密碼和遠端工作目錄。'));
                return;
            }
        }
        creatingTaskRef.current = true;
        const createGen = createTaskGenRef.current;
        setCreatingTask(true);
        setCreatingTaskMode(newTaskMode);
        setCreateError('');
        // SSH setup can take several seconds. Move remote creation out of the
        // modal immediately so the task-list connection progress stays visible,
        // matching the AI welcome-guide flow. Keep form state until success so
        // a failed connection can reopen with every field intact for retry.
        if (isRemoteCreate) {
            cancelSelectPlaceholderTimer();
            setCreateDialogOpen(false);
        }
        try {
            const workingDir = newTaskWorkingDir.trim();
            if (isRemoteCreate) {
                const portNum = Number.parseInt(remotePort.trim() || '22', 10);
                await createTask(taskName, undefined, 'remote_coding_dev', {
                    host: remoteHost.trim(),
                    port: Number.isFinite(portNum) && portNum > 0 && portNum < 65536 ? portNum : 22,
                    user: remoteUser.trim(),
                    password: remotePassword,
                    workDir: remoteWorkDir.trim(),
                });
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
                setCreateError('');
            }
        } catch (error) {
            if (mountedRef.current && createTaskGenRef.current === createGen) {
                setCreateError(extractErrorMessage(error) || textForLang(lang, 'Failed to create task', '创建任务失败', '建立任務失敗'));
                if (isRemoteCreate) {
                    setCreateDialogOpen(true);
                    schedulePostOpenFocus('remote_coding_dev', 'task-remote-password');
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

    const handleTaskDoubleClick = async (projectPath: string) => {
        if (renamingTaskPath) return;
        if (openingTaskPath === projectPath) return;
        if (!assistantReady) {
            onTaskSwitchBlocked?.();
            return;
        }
        setOpeningTaskPath(projectPath);
        try {
            await resumeTask(projectPath);
        } finally {
            setOpeningTaskPath(current => current === projectPath ? null : current);
        }
    };

    return (
    <div style={{ flex: 1, overflowY: 'auto', padding: '10px 8px 8px' }}>
        <div style={{ padding: '2px 8px 9px', display: 'flex', alignItems: 'center', gap: '6px', fontSize: '0.68rem', color: 'var(--theme-text-muted)', fontWeight: 700, letterSpacing: '0.02em' }}>
            <span style={{ display: 'inline-flex', alignItems: 'center', gap: '6px', minWidth: 0 }}>
                <span>{textForLang(lang, 'New Task', '\u65b0\u5efa\u4efb\u52a1', '\u65b0\u5efa\u4efb\u52d9')}</span>
                <button
                    type="button"
                    onClick={() => openCreateDialog()}
                    disabled={creatingTask}
                    aria-label={textForLang(lang, 'Create task', '创建任务', '建立任務')}
                    title={textForLang(lang, 'Create task', '创建任务', '建立任務')}
                    style={{ width: '22px', height: '22px', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', border: '1px solid color-mix(in srgb, var(--theme-primary) 44%, var(--theme-border))', borderRadius: '6px', background: 'color-mix(in srgb, var(--theme-primary) 13%, var(--theme-surface))', color: 'var(--theme-primary)', cursor: creatingTask ? 'default' : 'pointer', lineHeight: 1, padding: 0, opacity: creatingTask ? 0.55 : 1, boxShadow: 'inset 0 0 0 1px color-mix(in srgb, var(--theme-primary) 10%, transparent)' }}
                >
                    <CreateTaskIcon />
                </button>
            </span>
            <span style={{ marginLeft: 'auto', display: 'inline-flex', alignItems: 'center', gap: '6px', minWidth: 0 }}>
                <span>{textForLang(lang, 'Save as Task', '保存为任务', '保存為任務')}</span>
                <button
                    type="button"
                    onClick={requestSaveCurrentChatAsTask}
                    aria-label={textForLang(lang, 'Save current chat as task', '保存当前对话为任务', '保存目前對話為任務')}
                    title={textForLang(lang, 'Save current chat as task', '保存当前对话为任务', '保存目前對話為任務')}
                    style={{ width: '22px', height: '22px', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', border: '1px solid color-mix(in srgb, var(--theme-primary) 44%, var(--theme-border))', borderRadius: '6px', background: 'color-mix(in srgb, var(--theme-primary) 13%, var(--theme-surface))', color: 'var(--theme-primary)', cursor: 'pointer', lineHeight: 1, padding: 0, boxShadow: 'inset 0 0 0 1px color-mix(in srgb, var(--theme-primary) 10%, transparent)' }}
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
                    : textForLang(lang, 'Creating task…', '正在创建任务…', '正在建立任務…')}</span>
                <span style={{ display: 'block', marginTop: '6px', height: '3px', overflow: 'hidden', borderRadius: '999px', background: 'color-mix(in srgb, var(--theme-primary) 18%, transparent)' }}>
                    <span className="sidebar-task-progress__bar" style={{ display: 'block', width: '42%', height: '100%', borderRadius: 'inherit', background: 'var(--theme-primary)', animation: 'sidebar-task-restore-progress 0.9s ease-in-out infinite alternate' }} />
                </span>
            </div>
        )}
        {visibleTasks.length === 0 ? (
            <div style={{ padding: '24px 8px', textAlign: 'center', fontSize: '0.78rem', color: 'var(--theme-text-muted)', opacity: 0.65 }}>
                {textForLang(lang, 'No tasks', '\u6682\u65e0\u4efb\u52a1', '\u66ab\u7121\u4efb\u52d9')}
            </div>
        ) : visibleTasks.map(proj => {
            const taskIconKind = taskIconKindForProject(proj);
            const codingBadge = pureCodingBadgeLabel(proj, lang);
            const pureCoding = isPureCodingTask(proj);
            const workflowStatus = workflowStatusForTask(proj.active_workflow, lang);
            const activityLabel = taskActivityLabel(proj.last_activity, lang);
            return <div key={proj.id || proj.project_path} data-task-kind={taskIconKind} data-pure-coding={pureCoding ? 'true' : 'false'}>
                <div onDoubleClick={() => { void handleTaskDoubleClick(proj.project_path); }} onContextMenu={e => { e.preventDefault(); setTaskContextMenu({ x: e.clientX, y: e.clientY, projectPath: proj.project_path, name: proj.name || proj.project_path, pinned: !!proj.pinned, isRemoteCoding: isRemoteCodingTask(proj), tags: proj.tags }); }} style={{ display: 'flex', flexDirection: 'row', alignItems: 'flex-start', gap: '6px', padding: '7px 8px', borderRadius: '8px', cursor: openingTaskPath === proj.project_path ? 'progress' : 'pointer', transition: 'background 0.15s', opacity: openingTaskPath === proj.project_path ? 0.78 : 1 }} title={`${proj.name || proj.project_path}\n${proj.project_path}${workflowStatus ? '\n' + [workflowStatus.label, workflowStatus.detail].filter(Boolean).join(' · ') : ''}${codingBadge ? '\n' + codingBadge : ''}${activityLabel ? '\n' + activityLabel : ''}${proj.preview ? '\n' + proj.preview : ''}`} onMouseEnter={e => (e.currentTarget.style.background = 'color-mix(in srgb, var(--theme-text-primary) 7%, transparent)')} onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}>
                    <TaskTypeIcon kind={taskIconKind} lang={lang} />
                    <span style={{ minWidth: 0, flex: 1, textAlign: 'left' }}>
                        {(workflowStatus || codingBadge || proj.pinned) && (
                            <span style={{ display: 'flex', flexWrap: 'wrap', gap: '4px', marginBottom: '3px' }}>
                                {proj.pinned && !pureCoding && (
                                    <span data-testid="task-pinned-badge" style={{ display: 'inline-flex', maxWidth: '100%', padding: '1px 5px', borderRadius: '999px', border: '1px solid color-mix(in srgb, var(--theme-text-muted) 36%, transparent)', color: 'var(--theme-text-muted)', background: 'color-mix(in srgb, var(--theme-text-muted) 8%, transparent)', fontSize: '0.58rem', fontWeight: 700, lineHeight: 1.35 }}>{textForLang(lang, 'Pinned', '\u7f6e\u9876', '\u7f6e\u9802')}</span>
                                )}
                                {codingBadge && (
                                    <span
                                        data-testid={isRemoteCodingTask(proj) ? 'task-remote-coding-badge' : 'task-coding-badge'}
                                        title={taskIconLabel(taskIconKind, lang)}
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
                                {workflowStatus && <span data-testid="task-workflow-status" aria-label={`${textForLang(lang, 'Task status', '任务状态', '任務狀態')}: ${workflowStatus.label}${workflowStatus.detail ? ` · ${workflowStatus.detail}` : ''}`} title={`${proj.active_workflow?.type || 'workflow'}${workflowStatus.detail ? ` · ${workflowStatus.detail}` : ''}`} style={{ display: 'inline-flex', maxWidth: '100%', padding: '1px 5px', borderRadius: '999px', border: `1px solid ${TASK_WORKFLOW_STATUS_COLORS[workflowStatus.tone].border}`, color: TASK_WORKFLOW_STATUS_COLORS[workflowStatus.tone].color, background: TASK_WORKFLOW_STATUS_COLORS[workflowStatus.tone].background, fontSize: '0.58rem', fontWeight: 700, lineHeight: 1.35, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{workflowStatus.label}{workflowStatus.detail ? ` · ${workflowStatus.detail}` : ''}</span>}
                            </span>
                        )}
                        {renamingTaskPath === proj.project_path ? <input autoFocus value={renameValue} onChange={e => setRenameValue(e.target.value)} onBlur={async () => { const trimmed = renameValue.trim(); if (trimmed && trimmed !== proj.name) { await renameTask(proj.project_path, trimmed); refreshTasks(); } setRenamingTaskPath(null); }} onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); if (e.key === 'Escape') setRenamingTaskPath(null); }} onClick={e => e.stopPropagation()} style={{ width: '100%', fontSize: '0.74rem', fontWeight: 700, color: 'var(--theme-text-primary)', background: 'var(--theme-surface)', border: '1px solid var(--theme-primary)', borderRadius: '4px', padding: '2px 4px', outline: 'none' }} /> : <span style={{ display: 'block', fontWeight: 700, fontSize: '0.74rem', color: 'var(--theme-text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{proj.name || proj.project_path}</span>}
                        <span style={{ display: 'block', marginTop: '3px', color: 'var(--theme-text-muted)', fontSize: '0.66rem', lineHeight: 1.3, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{openingTaskPath === proj.project_path ? textForLang(lang, pureCoding ? 'Restoring pure coding environment...' : 'Restoring...', pureCoding ? '正在恢复纯编程环境...' : '恢复中...', pureCoding ? '正在恢復純程式環境...' : '恢復中...') : (proj.preview || proj.project_path)}</span>
                        {openingTaskPath !== proj.project_path && activityLabel && <span data-testid="task-last-activity" style={{ display: 'block', marginTop: '2px', color: 'var(--theme-text-muted)', fontSize: '0.6rem', lineHeight: 1.25, opacity: 0.82, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{activityLabel}</span>}
                        {openingTaskPath === proj.project_path && <span aria-label={textForLang(lang, pureCoding ? 'Restoring pure coding environment' : 'Restoring task', pureCoding ? '正在恢复纯编程环境' : '正在恢复任务', pureCoding ? '正在恢復純程式環境' : '正在恢復任務')} style={{ display: 'block', marginTop: '6px', height: '3px', overflow: 'hidden', borderRadius: '999px', background: 'color-mix(in srgb, var(--theme-primary) 18%, transparent)' }}><span className="sidebar-task-progress__bar" style={{ display: 'block', width: '42%', height: '100%', borderRadius: 'inherit', background: 'var(--theme-primary)', animation: 'sidebar-task-restore-progress 0.9s ease-in-out infinite alternate' }} /></span>}
                    </span>
                    <button type="button" aria-label={textForLang(lang, 'Scene details', '\u4efb\u52a1\u8bc1\u636e\u8be6\u60c5', '\u4efb\u52d9\u8b49\u64da\u8a73\u60c5')} title={textForLang(lang, 'Scene details', '\u4efb\u52a1\u8bc1\u636e\u8be6\u60c5', '\u4efb\u52d9\u8b49\u64da\u8a73\u60c5')} onClick={e => { e.stopPropagation(); void openSceneDetail(proj.project_path, proj.name); }} disabled={sceneDetailLoading && sceneDetailPath === proj.project_path} style={{ border: 'none', background: 'transparent', color: 'var(--theme-primary)', opacity: sceneDetailLoading && sceneDetailPath === proj.project_path ? 0.4 : 0.78, cursor: 'pointer', width: '20px', height: '20px', padding: 0, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}><ProjectSearchIcon name="info" size={13} /></button>
                </div>
                {sceneDetailPath === proj.project_path && <SidebarTaskEvidencePanel detail={sceneDetail} loading={sceneDetailLoading} lang={lang} onContinueWorkflow={continueWorkflowProject} error={sceneDetailError} onRetry={() => { void openSceneDetail(proj.project_path, proj.name, true); }} />}
            </div>
        })}

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
                            {newTaskMode === 'coding_dev'
                                ? textForLang(lang, 'Create local coding task', '创建本地编程任务', '建立本機程式任務')
                                : newTaskMode === 'remote_coding_dev'
                                    ? textForLang(lang, 'Create remote coding task', '创建远程编程任务', '建立遠端程式任務')
                                    : textForLang(lang, 'Create task', '创建任务', '建立任務')}
                        </h3>
                        <button type="button" className="btn-close" onClick={closeCreateDialog} disabled={creatingTask}>X</button>
                    </div>
                    <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                        <label style={{ fontSize: '0.74rem', fontWeight: 700, color: 'var(--theme-text-secondary)' }} htmlFor="task-management-name-input">
                            {textForLang(lang, 'Task command', '任务命令', '任務命令')}
                        </label>
                        <textarea
                            id={TASK_COMMAND_INPUT_ID}
                            autoFocus
                            value={newTaskName}
                            onChange={e => setNewTaskName(e.target.value)}
                            onKeyDown={e => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); void submitCreateTask(); } }}
                            placeholder={textForLang(lang, 'Describe the task you want to perform (Ctrl+Enter to submit)', '描述你想要执行的任务（Ctrl+Enter 提交）', '描述你想要執行的任務（Ctrl+Enter 提交）')}
                            disabled={creatingTask}
                            maxLength={CODING_TASK_COMMAND_MAX_LEN}
                            rows={newTaskMode === 'remote_coding_dev' || newTaskMode === 'coding_dev' ? 7 : 6}
                            style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.82rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '8px 10px', outline: 'none', resize: 'vertical', fontFamily: 'inherit', lineHeight: 1.5, minHeight: '80px', maxHeight: '280px', transition: 'border-color 0.15s, box-shadow 0.15s' }}
                            onFocus={e => { e.currentTarget.style.borderColor = 'var(--theme-primary)'; e.currentTarget.style.boxShadow = '0 0 0 2px color-mix(in srgb, var(--theme-primary) 20%, transparent)'; }}
                            onBlur={e => { e.currentTarget.style.borderColor = ''; e.currentTarget.style.boxShadow = ''; }}
                        />
                        {newTaskMode !== 'remote_coding_dev' && (
                        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: '10px', minHeight: '30px', paddingTop: '2px' }}>
                            <div style={{ display: 'flex', alignItems: 'center', gap: '7px', minWidth: 0 }}>
                                <span style={{ display: 'inline-flex', alignItems: 'center', gap: '5px', color: 'var(--theme-text-secondary)', fontSize: '0.72rem', whiteSpace: 'nowrap' }}>
                                    <ProjectSearchIcon name="desktop" size={14} />
                                    {textForLang(lang, 'Task directory', '\u4efb\u52a1\u76ee\u5f55', '\u4efb\u52d9\u76ee\u9304')}
                                </span>
                                <button
                                    type="button"
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
                            {newTaskName.length > Math.floor(CODING_TASK_COMMAND_MAX_LEN * 0.8) && (
                                <span style={{ flexShrink: 0, fontSize: '0.66rem', color: newTaskName.length >= CODING_TASK_COMMAND_MAX_LEN ? 'var(--theme-danger, #ef4444)' : 'var(--theme-text-muted)', textAlign: 'right' }}>
                                    {newTaskName.length} / {CODING_TASK_COMMAND_MAX_LEN}
                                </span>
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
                    <div className="modal-footer" style={{ justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '8px' }}>
                        <div role="group" aria-label={textForLang(lang, 'Task mode', '任务模式', '任務模式')} style={{ display: 'inline-flex', alignItems: 'center', gap: '4px', marginRight: 'auto', flexWrap: 'wrap', padding: '2px', borderRadius: '8px', background: 'color-mix(in srgb, var(--theme-text-muted) 8%, transparent)' }}>
                            {([
                                { id: '', label: textForLang(lang, 'Chat', '对话', '對話'), title: textForLang(lang, 'Ordinary task chat', '普通对话任务', '普通對話任務') },
                                { id: 'coding_dev' as const, label: textForLang(lang, 'Coding', '本地编程', '本機程式'), title: textForLang(lang, 'Local pure coding environment', '本地纯血编程环境', '本機純血程式開發環境') },
                                { id: 'remote_coding_dev' as const, label: textForLang(lang, 'Remote', '远程编程', '遠端程式'), title: textForLang(lang, 'Remote pure coding over SSH', '远程纯血编程环境（SSH）', '遠端純血程式開發環境（SSH）') },
                            ] as const).map(opt => {
                                const active = newTaskMode === opt.id;
                                return (
                                    <button
                                        key={opt.id || 'chat'}
                                        type="button"
                                        id={opt.id === 'coding_dev' ? 'task-management-coding-mode' : (opt.id === 'remote_coding_dev' ? 'task-management-remote-coding-mode' : 'task-management-chat-mode')}
                                        aria-pressed={active}
                                        aria-label={opt.label}
                                        title={opt.title}
                                        disabled={creatingTask}
                                        onClick={() => {
                                            if (newTaskMode === opt.id) return;
                                            setNewTaskMode(opt.id);
                                            setCreateError('');
                                            // Switching mode mid-dialog: fill blanks from last coding/remote task.
                                            applyEnvDefaultsForMode(opt.id, false);
                                        }}
                                        style={{
                                            border: 'none',
                                            borderRadius: '6px',
                                            padding: '4px 10px',
                                            fontSize: '0.72rem',
                                            fontWeight: active ? 700 : 500,
                                            cursor: creatingTask ? 'default' : 'pointer',
                                            color: active ? 'var(--theme-primary)' : 'var(--theme-text-secondary)',
                                            background: active ? 'color-mix(in srgb, var(--theme-primary) 14%, var(--theme-surface, transparent))' : 'transparent',
                                            boxShadow: active ? 'inset 0 0 0 1px color-mix(in srgb, var(--theme-primary) 35%, transparent)' : 'none',
                                            opacity: creatingTask ? 0.55 : 1,
                                        }}
                                    >
                                        {opt.label}
                                    </button>
                                );
                            })}
                        </div>
                        <div style={{ display: 'inline-flex', alignItems: 'center', gap: '8px' }}>
                            <button type="button" className="btn-secondary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={closeCreateDialog} disabled={creatingTask}>
                                {textForLang(lang, 'Cancel', '取消', '取消')}
                            </button>
                            <button
                                type="submit"
                                className="btn-primary"
                                style={{ fontSize: '0.78rem', padding: '4px 14px' }}
                                disabled={
                                    creatingTask
                                    || !newTaskName.trim()
                                    || (newTaskMode === 'remote_coding_dev' && (!remoteHost.trim() || !remoteUser.trim() || !remotePassword || !remoteWorkDir.trim()))
                                }
                            >
                                {textForLang(lang, 'OK', '确定', '確定')}
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
                    setRenamingTaskPath,
                    setRenameValue,
                    setTaskContextMenu,
                    pinTask,
                    hideTask,
                    refreshTasks,
                    openEditRemoteDialog,
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
                    <div className="modal-footer" style={{ justifyContent: 'space-between', alignItems: 'center', flexWrap: 'wrap', gap: '8px' }}>
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
