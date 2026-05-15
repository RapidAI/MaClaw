import { useRef, useState } from 'react';

export type RecentProject = {
    id?: string;
    name?: string;
    project_path: string;
    workflow_type?: string;
    preview?: string;
    last_activity?: string;
    pinned?: boolean;
};

export type TaskContextMenu = { x: number; y: number; projectPath: string; name: string; pinned: boolean } | null;

type SidebarRecentTasksProps = {
    lang: string;
    themeMode?: 'light' | 'dark';
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
};

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' || lang === 'zh' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

const normalizeTaskNameInput = (value?: string | null) => {
    const normalized = (value || '').trim().replace(/\s+/g, ' ');
    return Array.from(normalized).slice(0, 120).join('');
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
    const creatingTaskRef = useRef(false);
    const createBackdropMouseDownRef = useRef(false);

    const openCreateDialog = () => {
        if (creatingTaskRef.current) return;
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
        const taskName = normalizeTaskNameInput(newTaskName);
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

    const handleTaskDoubleClick = (projectPath: string) => {
        if (renamingTaskPath) return;
        if (!assistantReady) {
            onRecentTaskSwitchBlocked?.();
            return;
        }
        void resumeRecentProject(projectPath);
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
        {recentProjects.length === 0 ? (
            <div style={{ padding: '24px 8px', textAlign: 'center', fontSize: '0.78rem', color: 'var(--theme-text-muted)', opacity: 0.65 }}>
                {textForLang(lang, 'No recent tasks', '\u6682\u65e0\u6700\u8fd1\u4efb\u52a1', '\u66ab\u7121\u6700\u8fd1\u4efb\u52d9')}
            </div>
        ) : recentProjects.map(proj => (
            <div key={proj.id || proj.project_path} onDoubleClick={() => handleTaskDoubleClick(proj.project_path)} onContextMenu={e => { e.preventDefault(); setTaskContextMenu({ x: e.clientX, y: e.clientY, projectPath: proj.project_path, name: proj.name || proj.project_path, pinned: !!proj.pinned }); }} style={{ display: 'flex', flexDirection: 'row', alignItems: 'flex-start', gap: '6px', padding: '7px 8px', borderRadius: '8px', cursor: 'pointer', transition: 'background 0.15s' }} title={`${proj.name || proj.project_path}\n${proj.project_path}${proj.preview ? '\n' + proj.preview : ''}`} onMouseEnter={e => (e.currentTarget.style.background = 'color-mix(in srgb, var(--theme-text-primary) 7%, transparent)')} onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}>
                <span style={{ flexShrink: 0, color: '#ff3b73', fontSize: '0.82rem', lineHeight: '1.2', width: '16px', textAlign: 'center', overflow: 'hidden' }}>{proj.pinned ? '\uD83D\uDCCC' : '\uD83D\uDE80'}</span>
                <span style={{ minWidth: 0, flex: 1, textAlign: 'left' }}>
                    {renamingTaskPath === proj.project_path ? <input autoFocus value={renameValue} onChange={e => setRenameValue(e.target.value)} onBlur={async () => { const trimmed = renameValue.trim(); if (trimmed && trimmed !== proj.name) { await renameTask(proj.project_path, trimmed); refreshRecentProjects(); } setRenamingTaskPath(null); }} onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); if (e.key === 'Escape') setRenamingTaskPath(null); }} onClick={e => e.stopPropagation()} style={{ width: '100%', fontSize: '0.74rem', fontWeight: 700, color: 'var(--theme-text-primary)', background: 'var(--theme-surface)', border: '1px solid var(--theme-primary)', borderRadius: '4px', padding: '2px 4px', outline: 'none' }} /> : <span style={{ display: 'block', fontWeight: 700, fontSize: '0.74rem', color: 'var(--theme-text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{proj.name || proj.project_path}</span>}
                    <span style={{ display: 'block', marginTop: '3px', color: 'var(--theme-text-muted)', fontSize: '0.66rem', lineHeight: 1.3, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis', textAlign: 'left' }}>{proj.preview || proj.project_path}</span>
                </span>
            </div>
        ))}

        {createDialogOpen && (
            <div
                className="modal-backdrop"
                data-ai-theme={themeMode || undefined}
                onMouseDown={e => { createBackdropMouseDownRef.current = e.target === e.currentTarget; }}
                onClick={e => { if (e.target === e.currentTarget && createBackdropMouseDownRef.current) closeCreateDialog(); createBackdropMouseDownRef.current = false; }}
            >
                <form
                    className="modal-content"
                    onMouseDown={e => e.stopPropagation()}
                    onClick={e => e.stopPropagation()}
                    onKeyDown={e => { if (e.key === 'Escape') closeCreateDialog(); }}
                    onSubmit={e => { e.preventDefault(); void submitCreateTask(); }}
                    style={{ width: '360px', maxWidth: '92vw', textAlign: 'left' }}
                >
                    <div className="modal-header">
                        <h3 style={{ fontSize: '0.88rem', margin: 0 }}>{textForLang(lang, 'Create task', '创建任务', '建立任務')}</h3>
                        <button type="button" className="btn-close" onClick={closeCreateDialog} disabled={creatingTask}>×</button>
                    </div>
                    <div className="modal-body" style={{ display: 'flex', flexDirection: 'column', gap: '8px' }}>
                        <label style={{ fontSize: '0.74rem', fontWeight: 700, color: 'var(--theme-text-secondary)' }} htmlFor="recent-task-name-input">
                            {textForLang(lang, 'Task command', '任务命令', '任務命令')}
                        </label>
                        <input
                            id="recent-task-name-input"
                            autoFocus
                            value={newTaskName}
                            onChange={e => setNewTaskName(e.target.value)}
                            placeholder={textForLang(lang, 'Enter a task command', '请输入新任务的命令', '請輸入新任務的命令')}
                            disabled={creatingTask}
                            style={{ width: '100%', boxSizing: 'border-box', fontSize: '0.82rem', color: 'var(--theme-text-primary)', background: 'var(--theme-surface-muted)', border: '1px solid var(--theme-border)', borderRadius: '6px', padding: '8px 10px', outline: 'none' }}
                        />
                    </div>
                    <div className="modal-footer">
                        <button type="button" className="btn-secondary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={closeCreateDialog} disabled={creatingTask}>
                            {textForLang(lang, 'Cancel', '取消', '取消')}
                        </button>
                        <button type="submit" className="btn-primary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} disabled={creatingTask || !normalizeTaskNameInput(newTaskName)}>
                            {textForLang(lang, 'OK', '确定', '確定')}
                        </button>
                    </div>
                </form>
            </div>
        )}

        {taskContextMenu && (<>
            <div style={{ position: 'fixed', inset: 0, zIndex: 9998 }} onClick={() => setTaskContextMenu(null)} />
            <div style={{ position: 'fixed', left: taskContextMenu.x, top: taskContextMenu.y, zIndex: 9999, background: 'var(--theme-page-bg)', border: '1px solid var(--theme-border)', borderRadius: '6px', boxShadow: '0 4px 12px rgba(0,0,0,0.18)', padding: '4px 0', minWidth: '132px' }}>
                {[
                    { label: textForLang(lang, 'Rename', '\u91cd\u547d\u540d', '\u91cd\u547d\u540d'), icon: '\u270F\uFE0F', action: () => { setRenamingTaskPath(taskContextMenu.projectPath); setRenameValue(taskContextMenu.name); setTaskContextMenu(null); } },
                    { label: taskContextMenu.pinned ? textForLang(lang, 'Unpin', '\u53d6\u6d88\u7f6e\u9876', '\u53d6\u6d88\u7f6e\u9802') : textForLang(lang, 'Pin', '\u7f6e\u9876', '\u7f6e\u9802'), icon: '\uD83D\uDCCC', action: async () => { await pinTask(taskContextMenu.projectPath, !taskContextMenu.pinned); refreshRecentProjects(); setTaskContextMenu(null); } },
                    { label: textForLang(lang, 'Remove', '\u5220\u9664', '\u522a\u9664'), icon: '\uD83D\uDDD1\uFE0F', action: async () => { await hideTask(taskContextMenu.projectPath); refreshRecentProjects(); setTaskContextMenu(null); } },
                ].map(item => <div key={item.label} onClick={item.action} style={{ display: 'flex', alignItems: 'center', gap: '8px', padding: '7px 12px', cursor: 'pointer', fontSize: '0.78rem', color: 'var(--theme-text-primary)' }}><span>{item.icon}</span><span>{item.label}</span></div>)}
            </div>
        </>)}
    </div>
    );
};
