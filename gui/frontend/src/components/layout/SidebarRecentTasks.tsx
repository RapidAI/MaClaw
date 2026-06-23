import { useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { GetProjectScene } from '../../../wailsjs/go/main/App';
import { EventsEmit } from '../../../wailsjs/runtime';
import { EVENT_PROJECT_TASK_CLOSED } from '../../constants/events';
import { localizeText } from '../../i18n';
import { ProjectSearchIcon } from '../ai/ProjectSearchIcon';
import type { ProjectSceneDetail } from '../ai/ProjectSceneDetailPanel';
import { SidebarTaskEvidencePanel } from './SidebarTaskEvidencePanel';

export type RecentProject = {
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
    last_activity?: string;
    pinned?: boolean;
    has_output?: boolean;
};

export type TaskContextMenu = { x: number; y: number; projectPath: string; name: string; pinned: boolean } | null;

const taskIconForProject = (proj: RecentProject) => {
    if (proj.pinned) return 'PIN';
    return proj.tags?.includes('forked_task') ? 'REF' : 'TASK';
};

function emitProjectTaskClosed(projectPath: string) {
    try {
        EventsEmit(EVENT_PROJECT_TASK_CLOSED, projectPath);
    } catch (error) {
        console.warn('[SidebarRecentTasks] project close event emit failed:', error);
    }
}

type SidebarRecentTasksProps = {
    lang: string;
    themeMode?: 'light' | 'dark';
    recentProjects: RecentProject[];
    renamingTaskPath: string | null;
    setRenamingTaskPath: (path: string | null) => void;
    renameValue: string;
    setRenameValue: (value: string) => void;
    resumeRecentProject: (projectPath: string) => Promise<void> | void;
    continueWorkflowProject?: (projectPath: string) => Promise<void> | void;
    assistantReady?: boolean;
    onRecentTaskSwitchBlocked?: () => void;
    createRecentTask: (name: string) => Promise<void> | void;
    refreshRecentProjects: () => void;
    taskContextMenu: TaskContextMenu;
    setTaskContextMenu: (menu: TaskContextMenu) => void;
    renameTask: (projectPath: string, name: string) => Promise<unknown>;
    pinTask: (projectPath: string, pinned: boolean) => Promise<unknown>;
    hideTask: (projectPath: string) => Promise<unknown>;
};

const textForLang = localizeText;
// Must sit above existing fixed dropdowns/overlays that use z-index 99999.
const RECENT_TASK_CREATE_DIALOG_Z_INDEX = 100000;

const getPortalThemeMode = (themeMode?: 'light' | 'dark') => (
    themeMode || document.getElementById('App')?.getAttribute('data-ai-theme') || undefined
);

const normalizeTaskCommandInput = (value?: string | null) => {
    // Preserve newlines (multi-line task commands), only collapse horizontal whitespace per line
    const lines = (value || '').split('\n').map(line => line.trim().replace(/[ \t]+/g, ' '));
    // Remove leading/trailing empty lines, collapse 3+ consecutive empty lines to 2
    const trimmed = lines.join('\n').trim().replace(/\n{3,}/g, '\n\n');
    // Limit to 2000 characters (UTF-16 code units, consistent with HTML maxLength)
    return trimmed.slice(0, 2000);
};

export const SidebarRecentTasks = ({
    lang,
    themeMode,
    recentProjects,
    renamingTaskPath,
    setRenamingTaskPath,
    renameValue,
    setRenameValue,
    resumeRecentProject,
    continueWorkflowProject = () => {},
    assistantReady = true,
    onRecentTaskSwitchBlocked,
    createRecentTask,
    refreshRecentProjects,
    taskContextMenu,
    setTaskContextMenu,
    renameTask,
    pinTask,
    hideTask,
}: SidebarRecentTasksProps) => {
    const [creatingTask, setCreatingTask] = useState(false);
    const [createDialogOpen, setCreateDialogOpen] = useState(false);
    const [newTaskName, setNewTaskName] = useState('');
    const [sceneDetailPath, setSceneDetailPath] = useState<string | null>(null);
    const [sceneDetail, setSceneDetail] = useState<ProjectSceneDetail | null>(null);
    const [sceneDetailLoading, setSceneDetailLoading] = useState(false);
    const [openingTaskPath, setOpeningTaskPath] = useState<string | null>(null);
    const creatingTaskRef = useRef(false);
    const createBackdropMouseDownRef = useRef(false);
    const visibleRecentProjects = recentProjects.filter(proj => proj.has_output !== false);

    const openCreateDialog = () => {
        if (creatingTaskRef.current) return;
        if (taskContextMenu) setTaskContextMenu(null);
        setNewTaskName('');
        setCreateDialogOpen(true);
    };

    const closeCreateDialog = () => {
        if (creatingTaskRef.current) return;
        setCreateDialogOpen(false);
        setNewTaskName('');
    };

    const submitCreateTask = async () => {
        if (creatingTaskRef.current) return;
        const taskName = normalizeTaskCommandInput(newTaskName);
        if (!taskName) return;
        creatingTaskRef.current = true;
        setCreatingTask(true);
        try {
            await createRecentTask(taskName);
            setCreateDialogOpen(false);
            setNewTaskName('');
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
            console.error('[SidebarRecentTasks] GetProjectScene failed:', error);
            setSceneDetail({ project_path: projectPath, name: fallbackName || projectPath, recent_artifacts: [] });
        } finally {
            setSceneDetailLoading(false);
        }
    };

    const handleTaskDoubleClick = async (projectPath: string) => {
        if (renamingTaskPath) return;
        if (openingTaskPath === projectPath) return;
        if (!assistantReady) {
            onRecentTaskSwitchBlocked?.();
            return;
        }
        setOpeningTaskPath(projectPath);
        try {
            await resumeRecentProject(projectPath);
        } finally {
            setOpeningTaskPath(current => current === projectPath ? null : current);
        }
    };

    return (
    <div style={{ flex: 1, overflowY: 'auto', padding: '10px 8px 8px' }}>
        <div style={{ padding: '2px 8px 9px', display: 'flex', alignItems: 'center', gap: '6px', fontSize: '0.68rem', color: 'var(--theme-text-muted)', fontWeight: 700, letterSpacing: '0.02em' }}>
            <span>{textForLang(lang, 'Recent Tasks', '\u6700\u8fd1\u4efb\u52a1', '\u6700\u8fd1\u4efb\u52d9')}</span>
            <button
                type="button"
                onClick={openCreateDialog}
                disabled={creatingTask}
                aria-label={textForLang(lang, 'Create task', '创建任务', '建立任務')}
                title={textForLang(lang, 'Create task', '创建任务', '建立任務')}
                style={{ width: '18px', height: '18px', display: 'inline-flex', alignItems: 'center', justifyContent: 'center', border: '1px solid var(--theme-border)', borderRadius: '50%', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)', cursor: creatingTask ? 'default' : 'pointer', fontSize: '0.86rem', lineHeight: 1, fontWeight: 700, padding: 0, opacity: creatingTask ? 0.55 : 1 }}
            >
                +
            </button>
        </div>
        {visibleRecentProjects.length === 0 ? (
            <div style={{ padding: '24px 8px', textAlign: 'center', fontSize: '0.78rem', color: 'var(--theme-text-muted)', opacity: 0.65 }}>
                {textForLang(lang, 'No recent tasks', '\u6682\u65e0\u6700\u8fd1\u4efb\u52a1', '\u66ab\u7121\u6700\u8fd1\u4efb\u52d9')}
            </div>
        ) : visibleRecentProjects.map(proj => (
            <div key={proj.id || proj.project_path}>
                <div onDoubleClick={() => { void handleTaskDoubleClick(proj.project_path); }} onContextMenu={e => { e.preventDefault(); setTaskContextMenu({ x: e.clientX, y: e.clientY, projectPath: proj.project_path, name: proj.name || proj.project_path, pinned: !!proj.pinned }); }} style={{ display: 'flex', flexDirection: 'row', alignItems: 'flex-start', gap: '6px', padding: '7px 8px', borderRadius: '8px', cursor: openingTaskPath === proj.project_path ? 'progress' : 'pointer', transition: 'background 0.15s', opacity: openingTaskPath === proj.project_path ? 0.78 : 1 }} title={`${proj.name || proj.project_path}\n${proj.project_path}${proj.preview ? '\n' + proj.preview : ''}`} onMouseEnter={e => (e.currentTarget.style.background = 'color-mix(in srgb, var(--theme-text-primary) 7%, transparent)')} onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}>
                    <span style={{ flexShrink: 0, color: 'var(--theme-text-muted)', fontSize: '0.54rem', lineHeight: '1.35', width: '24px', textAlign: 'center', overflow: 'hidden', fontWeight: 800, letterSpacing: 0 }}>{taskIconForProject(proj)}</span>
                    <span style={{ minWidth: 0, flex: 1, textAlign: 'left' }}>
                        {proj.active_workflow && <span title={`${proj.active_workflow.type || 'workflow'} ${proj.active_workflow.phase || ''}`.trim()} style={{ display: 'inline-flex', maxWidth: '100%', marginBottom: '3px', padding: '1px 5px', borderRadius: '999px', border: '1px solid color-mix(in srgb, var(--theme-primary) 42%, transparent)', color: 'var(--theme-primary)', background: 'color-mix(in srgb, var(--theme-primary) 8%, transparent)', fontSize: '0.58rem', fontWeight: 700, lineHeight: 1.35, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{textForLang(lang, 'Stage output', '\u9636\u6bb5\u4ea7\u51fa', '\u968e\u6bb5\u7522\u51fa')}</span>}
                        {renamingTaskPath === proj.project_path ? <input autoFocus value={renameValue} onChange={e => setRenameValue(e.target.value)} onBlur={async () => { const trimmed = renameValue.trim(); if (trimmed && trimmed !== proj.name) { await renameTask(proj.project_path, trimmed); refreshRecentProjects(); } setRenamingTaskPath(null); }} onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); if (e.key === 'Escape') setRenamingTaskPath(null); }} onClick={e => e.stopPropagation()} style={{ width: '100%', fontSize: '0.74rem', fontWeight: 700, color: 'var(--theme-text-primary)', background: 'var(--theme-surface)', border: '1px solid var(--theme-primary)', borderRadius: '4px', padding: '2px 4px', outline: 'none' }} /> : <span style={{ display: 'block', fontWeight: 700, fontSize: '0.74rem', color: 'var(--theme-text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{proj.name || proj.project_path}</span>}
                        <span style={{ display: 'block', marginTop: '3px', color: 'var(--theme-text-muted)', fontSize: '0.66rem', lineHeight: 1.3, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{openingTaskPath === proj.project_path ? textForLang(lang, 'Restoring...', '恢复中...', '恢復中...') : (proj.preview || proj.project_path)}</span>
                        {openingTaskPath === proj.project_path && <span aria-label={textForLang(lang, 'Restoring task', '正在恢复任务', '正在恢復任務')} style={{ display: 'block', marginTop: '6px', height: '3px', overflow: 'hidden', borderRadius: '999px', background: 'color-mix(in srgb, var(--theme-primary) 18%, transparent)' }}><span style={{ display: 'block', width: '42%', height: '100%', borderRadius: 'inherit', background: 'var(--theme-primary)', animation: 'sidebar-task-restore-progress 0.9s ease-in-out infinite alternate' }} /></span>}
                    </span>
                    <button type="button" aria-label={textForLang(lang, 'Scene details', '\u4efb\u52a1\u8bc1\u636e\u8be6\u60c5', '\u4efb\u52d9\u8b49\u64da\u8a73\u60c5')} title={textForLang(lang, 'Scene details', '\u4efb\u52a1\u8bc1\u636e\u8be6\u60c5', '\u4efb\u52d9\u8b49\u64da\u8a73\u60c5')} onClick={e => { e.stopPropagation(); void openSceneDetail(proj.project_path, proj.name); }} disabled={sceneDetailLoading && sceneDetailPath === proj.project_path} style={{ border: 'none', background: 'transparent', color: 'var(--theme-primary)', opacity: sceneDetailLoading && sceneDetailPath === proj.project_path ? 0.4 : 0.78, cursor: 'pointer', width: '20px', height: '20px', padding: 0, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0 }}><ProjectSearchIcon name="info" size={13} /></button>
                </div>
                {sceneDetailPath === proj.project_path && <SidebarTaskEvidencePanel detail={sceneDetail} loading={sceneDetailLoading} lang={lang} onContinueWorkflow={(workflowProjectPath) => { void continueWorkflowProject(workflowProjectPath); }} />}
            </div>
        ))}

        {createDialogOpen && createPortal(
            <div
                className="modal-backdrop"
                data-ai-theme={getPortalThemeMode(themeMode)}
                style={{ zIndex: RECENT_TASK_CREATE_DIALOG_Z_INDEX }}
                onMouseDown={e => { createBackdropMouseDownRef.current = e.target === e.currentTarget; }}
                onClick={e => { if (e.target === e.currentTarget && createBackdropMouseDownRef.current) closeCreateDialog(); createBackdropMouseDownRef.current = false; }}
            >
                <form
                    className="modal-content"
                    role="dialog"
                    aria-modal="true"
                    aria-labelledby="recent-task-dialog-title"
                    onMouseDown={e => e.stopPropagation()}
                    onClick={e => e.stopPropagation()}
                    onKeyDown={e => { if (e.key === 'Escape') closeCreateDialog(); }}
                    onSubmit={e => { e.preventDefault(); void submitCreateTask(); }}
                    style={{ width: '420px', maxWidth: '92vw', textAlign: 'left' }}
                >
                    <div className="modal-header">
                        <h3 id="recent-task-dialog-title" style={{ fontSize: '0.88rem', margin: 0 }}>{textForLang(lang, 'Create task', '创建任务', '建立任務')}</h3>
                        <button type="button" className="btn-close" onClick={closeCreateDialog} disabled={creatingTask}>X</button>
                    </div>
                    <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                        <label style={{ fontSize: '0.74rem', fontWeight: 700, color: 'var(--theme-text-secondary)' }} htmlFor="recent-task-name-input">
                            {textForLang(lang, 'Task command', '任务命令', '任務命令')}
                        </label>
                        <textarea
                            id="recent-task-name-input"
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
                        {newTaskName.length > 1600 && (
                            <span style={{ fontSize: '0.66rem', color: newTaskName.length >= 2000 ? 'var(--theme-danger, #ef4444)' : 'var(--theme-text-muted)', textAlign: 'right' }}>
                                {newTaskName.length} / 2000
                            </span>
                        )}
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
                    { label: taskContextMenu.pinned ? textForLang(lang, 'Unpin', '\u53d6\u6d88\u7f6e\u9876', '\u53d6\u6d88\u7f6e\u9802') : textForLang(lang, 'Pin', '\u7f6e\u9876', '\u7f6e\u9802'), icon: 'PIN', action: async () => { await pinTask(taskContextMenu.projectPath, !taskContextMenu.pinned); refreshRecentProjects(); setTaskContextMenu(null); } },
                    { label: textForLang(lang, 'Remove', '\u5220\u9664', '\u522a\u9664'), icon: 'X', action: async () => { await hideTask(taskContextMenu.projectPath); emitProjectTaskClosed(taskContextMenu.projectPath); refreshRecentProjects(); setTaskContextMenu(null); } },
                ].map(item => <div key={item.label} onClick={item.action} style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '7px 12px', cursor: 'pointer', fontSize: '0.78rem', color: 'var(--theme-text-primary)' }}><span>{item.icon}</span><span>{item.label}</span></div>)}
            </div>
        </>)}
    </div>
    );
};
