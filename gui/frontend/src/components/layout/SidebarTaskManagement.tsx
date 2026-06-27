import { useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { GetProjectScene, SelectWorkingDir } from '../../../wailsjs/go/main/App';
import { EventsEmit } from '../../../wailsjs/runtime';
import { EVENT_PROJECT_TASK_CLOSED } from '../../constants/events';
import { localizeText } from '../../i18n';
import { ProjectSearchIcon } from '../ai/ProjectSearchIcon';
import type { ProjectSceneDetail } from '../ai/ProjectSceneDetailPanel';
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

export type TaskContextMenu = { x: number; y: number; projectPath: string; name: string; pinned: boolean } | null;

type TaskIconKind = 'pin' | 'reference' | 'task';

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

const taskIconKindForProject = (proj: TaskManagementItem): TaskIconKind => {
    if (proj.pinned) return 'pin';
    return proj.tags?.includes('forked_task') ? 'reference' : 'task';
};

const taskIconLabel = (kind: TaskIconKind, lang: string) => {
    if (kind === 'pin') return textForLang(lang, 'Pinned task', '\u7f6e\u9876\u4efb\u52a1', '\u7f6e\u9802\u4efb\u52d9');
    if (kind === 'reference') return textForLang(lang, 'Referenced task', '\u5f15\u7528\u4efb\u52a1', '\u5f15\u7528\u4efb\u52d9');
    return textForLang(lang, 'Task', '\u4efb\u52a1', '\u4efb\u52d9');
};

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
    createTask: (name: string, workingDir?: string) => Promise<void> | void;
    refreshTasks: () => void;
    taskContextMenu: TaskContextMenu;
    setTaskContextMenu: (menu: TaskContextMenu) => void;
    renameTask: (projectPath: string, name: string) => Promise<unknown>;
    pinTask: (projectPath: string, pinned: boolean) => Promise<unknown>;
    hideTask: (projectPath: string) => Promise<unknown>;
};

const textForLang = localizeText;
// Must sit above existing fixed dropdowns/overlays that use z-index 99999.
const TASK_CREATE_DIALOG_Z_INDEX = 100000;

const getPortalThemeMode = (themeMode?: 'light' | 'dark') => (
    themeMode || document.getElementById('App')?.getAttribute('data-ai-theme') || undefined
);

const getPortalDarkScheme = () => (
    document.getElementById('App')?.getAttribute('data-ai-dark-scheme') || undefined
);

const normalizeTaskCommandInput = (value?: string | null) => {
    // Preserve newlines (multi-line task commands), only collapse horizontal whitespace per line
    const lines = (value || '').split('\n').map(line => line.trim().replace(/[ \t]+/g, ' '));
    // Remove leading/trailing empty lines, collapse 3+ consecutive empty lines to 2
    const trimmed = lines.join('\n').trim().replace(/\n{3,}/g, '\n\n');
    // Limit to 2000 characters (UTF-16 code units, consistent with HTML maxLength)
    return trimmed.slice(0, 2000);
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
}: SidebarTaskManagementProps) => {
    const [creatingTask, setCreatingTask] = useState(false);
    const [createDialogOpen, setCreateDialogOpen] = useState(false);
    const [newTaskName, setNewTaskName] = useState('');
    const [newTaskWorkingDir, setNewTaskWorkingDir] = useState('');
    const [selectingWorkingDir, setSelectingWorkingDir] = useState(false);
    const [sceneDetailPath, setSceneDetailPath] = useState<string | null>(null);
    const [sceneDetail, setSceneDetail] = useState<ProjectSceneDetail | null>(null);
    const [sceneDetailLoading, setSceneDetailLoading] = useState(false);
    const [openingTaskPath, setOpeningTaskPath] = useState<string | null>(null);
    const creatingTaskRef = useRef(false);
    const createBackdropMouseDownRef = useRef(false);
    const visibleTasks = tasks.filter(proj => proj.has_output !== false);

    const openCreateDialog = () => {
        if (creatingTaskRef.current) return;
        if (taskContextMenu) setTaskContextMenu(null);
        setNewTaskName('');
        // Use the most recent task's working directory as default
        const lastDir = tasks.find(t => t.working_dir)?.working_dir || '';
        setNewTaskWorkingDir(lastDir);
        setCreateDialogOpen(true);
    };

    const closeCreateDialog = () => {
        if (creatingTaskRef.current) return;
        setCreateDialogOpen(false);
        setNewTaskName('');
        setNewTaskWorkingDir('');
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
        creatingTaskRef.current = true;
        setCreatingTask(true);
        try {
            const workingDir = newTaskWorkingDir.trim();
            if (workingDir) {
                await createTask(taskName, workingDir);
            } else {
                await createTask(taskName);
            }
            setCreateDialogOpen(false);
            setNewTaskName('');
            setNewTaskWorkingDir('');
        } finally {
            creatingTaskRef.current = false;
            setCreatingTask(false);
        }
    };

    const openSceneDetail = async (projectPath: string, fallbackName?: string) => {
        if (sceneDetailPath === projectPath) {
            setSceneDetailPath(null);
            setSceneDetail(null);
            return;
        }
        setSceneDetailPath(projectPath);
        setSceneDetailLoading(true);
        try {
            const detail = await GetProjectScene(projectPath);
            setSceneDetail((detail || null) as ProjectSceneDetail | null);
        } catch (error) {
            console.error('[SidebarTaskManagement] GetProjectScene failed:', error);
            setSceneDetail({ project_path: projectPath, name: fallbackName || projectPath, recent_artifacts: [] });
        } finally {
            setSceneDetailLoading(false);
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
                    onClick={openCreateDialog}
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
        {visibleTasks.length === 0 ? (
            <div style={{ padding: '24px 8px', textAlign: 'center', fontSize: '0.78rem', color: 'var(--theme-text-muted)', opacity: 0.65 }}>
                {textForLang(lang, 'No tasks', '\u6682\u65e0\u4efb\u52a1', '\u66ab\u7121\u4efb\u52d9')}
            </div>
        ) : visibleTasks.map(proj => {
            const taskIconKind = taskIconKindForProject(proj);
            return <div key={proj.id || proj.project_path}>
                <div onDoubleClick={() => { void handleTaskDoubleClick(proj.project_path); }} onContextMenu={e => { e.preventDefault(); setTaskContextMenu({ x: e.clientX, y: e.clientY, projectPath: proj.project_path, name: proj.name || proj.project_path, pinned: !!proj.pinned }); }} style={{ display: 'flex', flexDirection: 'row', alignItems: 'flex-start', gap: '6px', padding: '7px 8px', borderRadius: '8px', cursor: openingTaskPath === proj.project_path ? 'progress' : 'pointer', transition: 'background 0.15s', opacity: openingTaskPath === proj.project_path ? 0.78 : 1 }} title={`${proj.name || proj.project_path}\n${proj.project_path}${proj.preview ? '\n' + proj.preview : ''}`} onMouseEnter={e => (e.currentTarget.style.background = 'color-mix(in srgb, var(--theme-text-primary) 7%, transparent)')} onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}>
                    <TaskTypeIcon kind={taskIconKind} lang={lang} />
                    <span style={{ minWidth: 0, flex: 1, textAlign: 'left' }}>
                        {proj.active_workflow && <span title={`${proj.active_workflow.type || 'workflow'} ${proj.active_workflow.phase || ''}`.trim()} style={{ display: 'inline-flex', maxWidth: '100%', marginBottom: '3px', padding: '1px 5px', borderRadius: '999px', border: '1px solid color-mix(in srgb, var(--theme-primary) 42%, transparent)', color: 'var(--theme-primary)', background: 'color-mix(in srgb, var(--theme-primary) 8%, transparent)', fontSize: '0.58rem', fontWeight: 700, lineHeight: 1.35, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{textForLang(lang, 'Stage output', '\u9636\u6bb5\u4ea7\u51fa', '\u968e\u6bb5\u7522\u51fa')}</span>}
                        {renamingTaskPath === proj.project_path ? <input autoFocus value={renameValue} onChange={e => setRenameValue(e.target.value)} onBlur={async () => { const trimmed = renameValue.trim(); if (trimmed && trimmed !== proj.name) { await renameTask(proj.project_path, trimmed); refreshTasks(); } setRenamingTaskPath(null); }} onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); if (e.key === 'Escape') setRenamingTaskPath(null); }} onClick={e => e.stopPropagation()} style={{ width: '100%', fontSize: '0.74rem', fontWeight: 700, color: 'var(--theme-text-primary)', background: 'var(--theme-surface)', border: '1px solid var(--theme-primary)', borderRadius: '4px', padding: '2px 4px', outline: 'none' }} /> : <span style={{ display: 'block', fontWeight: 700, fontSize: '0.74rem', color: 'var(--theme-text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{proj.name || proj.project_path}</span>}
                        <span style={{ display: 'block', marginTop: '3px', color: 'var(--theme-text-muted)', fontSize: '0.66rem', lineHeight: 1.3, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{openingTaskPath === proj.project_path ? textForLang(lang, 'Restoring...', '恢复中...', '恢復中...') : (proj.preview || proj.project_path)}</span>
                        {openingTaskPath === proj.project_path && <span aria-label={textForLang(lang, 'Restoring task', '正在恢复任务', '正在恢復任務')} style={{ display: 'block', marginTop: '6px', height: '3px', overflow: 'hidden', borderRadius: '999px', background: 'color-mix(in srgb, var(--theme-primary) 18%, transparent)' }}><span style={{ display: 'block', width: '42%', height: '100%', borderRadius: 'inherit', background: 'var(--theme-primary)', animation: 'sidebar-task-restore-progress 0.9s ease-in-out infinite alternate' }} /></span>}
                    </span>
                    <button type="button" aria-label={textForLang(lang, 'Scene details', '\u4efb\u52a1\u8bc1\u636e\u8be6\u60c5', '\u4efb\u52d9\u8b49\u64da\u8a73\u60c5')} title={textForLang(lang, 'Scene details', '\u4efb\u52a1\u8bc1\u636e\u8be6\u60c5', '\u4efb\u52d9\u8b49\u64da\u8a73\u60c5')} onClick={e => { e.stopPropagation(); void openSceneDetail(proj.project_path, proj.name); }} disabled={sceneDetailLoading && sceneDetailPath === proj.project_path} style={{ border: 'none', background: 'transparent', color: 'var(--theme-primary)', opacity: sceneDetailLoading && sceneDetailPath === proj.project_path ? 0.4 : 0.78, cursor: 'pointer', width: '20px', height: '20px', padding: 0, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}><ProjectSearchIcon name="info" size={13} /></button>
                </div>
                {sceneDetailPath === proj.project_path && <SidebarTaskEvidencePanel detail={sceneDetail} loading={sceneDetailLoading} lang={lang} onContinueWorkflow={(workflowProjectPath) => { void continueWorkflowProject(workflowProjectPath); }} />}
            </div>
        })}

        {createDialogOpen && createPortal(
            <div
                className="modal-backdrop"
                data-ai-theme={getPortalThemeMode(themeMode)}
                data-ai-dark-scheme={getPortalDarkScheme()}
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
                        <h3 id="task-management-dialog-title" style={{ fontSize: '0.88rem', margin: 0 }}>{textForLang(lang, 'Create task', '创建任务', '建立任務')}</h3>
                        <button type="button" className="btn-close" onClick={closeCreateDialog} disabled={creatingTask}>X</button>
                    </div>
                    <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                        <label style={{ fontSize: '0.74rem', fontWeight: 700, color: 'var(--theme-text-secondary)' }} htmlFor="task-management-name-input">
                            {textForLang(lang, 'Task command', '任务命令', '任務命令')}
                        </label>
                        <textarea
                            id="task-management-name-input"
                            autoFocus
                            value={newTaskName}
                            onChange={e => setNewTaskName(e.target.value)}
                            onKeyDown={e => { if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) { e.preventDefault(); void submitCreateTask(); } }}
                            placeholder={textForLang(lang, 'Describe the task you want to perform (Ctrl+Enter to submit)', '描述你想要执行的任务（Ctrl+Enter 提交）', '描述你想要執行的任務（Ctrl+Enter 提交）')}
                            disabled={creatingTask}
                            maxLength={2000}
                            rows={6}
                            style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.82rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '8px 10px', outline: 'none', resize: 'vertical', fontFamily: 'inherit', lineHeight: 1.5, minHeight: '80px', maxHeight: '280px', transition: 'border-color 0.15s, box-shadow 0.15s' }}
                            onFocus={e => { e.currentTarget.style.borderColor = 'var(--theme-primary)'; e.currentTarget.style.boxShadow = '0 0 0 2px color-mix(in srgb, var(--theme-primary) 20%, transparent)'; }}
                            onBlur={e => { e.currentTarget.style.borderColor = ''; e.currentTarget.style.boxShadow = ''; }}
                        />
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
                            {newTaskName.length > 1600 && (
                                <span style={{ flexShrink: 0, fontSize: '0.66rem', color: newTaskName.length >= 2000 ? 'var(--theme-danger, #ef4444)' : 'var(--theme-text-muted)', textAlign: 'right' }}>
                                    {newTaskName.length} / 2000
                                </span>
                            )}
                        </div>
                    </div>
                    <div className="modal-footer">
                        <button type="button" className="btn-secondary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={closeCreateDialog} disabled={creatingTask}>
                            {textForLang(lang, 'Cancel', '取消', '取消')}
                        </button>
                        <button type="submit" className="btn-primary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} disabled={creatingTask || !newTaskName.trim()}>
                            {textForLang(lang, 'OK', '确定', '確定')}
                        </button>
                    </div>
                </form>
            </div>,
            document.body,
        )}

        {taskContextMenu && (<>
            <div style={{ position: 'fixed', inset: 0, zIndex: 9998 }} onClick={() => setTaskContextMenu(null)} />
            <div style={{ position: 'fixed', left: taskContextMenu.x, top: taskContextMenu.y, zIndex: 9999, background: 'var(--theme-page-bg)', border: '1px solid var(--theme-border)', borderRadius: '6px', boxShadow: '0 4px 12px rgba(0,0,0,0.18)', padding: '4px 0', minWidth: '132px' }}>
                {[
                    { label: textForLang(lang, 'Rename', '\u91cd\u547d\u540d', '\u91cd\u547d\u540d'), icon: 'edit', action: () => { setRenamingTaskPath(taskContextMenu.projectPath); setRenameValue(taskContextMenu.name); setTaskContextMenu(null); } },
                    { label: taskContextMenu.pinned ? textForLang(lang, 'Unpin', '\u53d6\u6d88\u7f6e\u9876', '\u53d6\u6d88\u7f6e\u9802') : textForLang(lang, 'Pin', '\u7f6e\u9876', '\u7f6e\u9802'), icon: 'PIN', action: async () => { await pinTask(taskContextMenu.projectPath, !taskContextMenu.pinned); refreshTasks(); setTaskContextMenu(null); } },
                    { label: textForLang(lang, 'Remove', '\u5220\u9664', '\u522a\u9664'), icon: 'X', action: async () => { await hideTask(taskContextMenu.projectPath); emitProjectTaskClosed(taskContextMenu.projectPath); refreshTasks(); setTaskContextMenu(null); } },
                ].map(item => <div key={item.label} onClick={item.action} style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '7px 12px', cursor: 'pointer', fontSize: '0.78rem', color: 'var(--theme-text-primary)' }}><span>{item.icon}</span><span>{item.label}</span></div>)}
            </div>
        </>)}
    </div>
    );
};
