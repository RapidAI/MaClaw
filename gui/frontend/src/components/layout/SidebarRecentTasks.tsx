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
};

const textForLang = (lang: string, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' || lang === 'zh' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

export const SidebarRecentTasks = ({
    lang,
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
}: SidebarRecentTasksProps) => (
    <div style={{ flex: 1, overflowY: 'auto', padding: '10px 8px 8px' }}>
        <div style={{ padding: '2px 8px 9px', fontSize: '0.68rem', color: 'var(--theme-text-muted)', fontWeight: 700, letterSpacing: '0.02em' }}>
            {textForLang(lang, 'Recent Tasks', '\u6700\u8fd1\u4efb\u52a1', '\u6700\u8fd1\u4efb\u52d9')}
        </div>
        {recentProjects.length === 0 ? (
            <div style={{ padding: '24px 8px', textAlign: 'center', fontSize: '0.78rem', color: 'var(--theme-text-muted)', opacity: 0.65 }}>
                {textForLang(lang, 'No recent tasks', '\u6682\u65e0\u6700\u8fd1\u4efb\u52a1', '\u66ab\u7121\u6700\u8fd1\u4efb\u52d9')}
            </div>
        ) : recentProjects.map(proj => (
            <div key={proj.id || proj.project_path} onClick={() => { if (!renamingTaskPath) void resumeRecentProject(proj.project_path); }} onContextMenu={e => { e.preventDefault(); setTaskContextMenu({ x: e.clientX, y: e.clientY, projectPath: proj.project_path, name: proj.name || proj.project_path, pinned: !!proj.pinned }); }} style={{ display: 'grid', gridTemplateColumns: '20px minmax(0, 1fr)', gap: '7px', padding: '8px 8px', borderRadius: '8px', cursor: 'pointer', transition: 'background 0.15s' }} title={`${proj.name || proj.project_path}\n${proj.project_path}${proj.preview ? '\n' + proj.preview : ''}`} onMouseEnter={e => (e.currentTarget.style.background = 'color-mix(in srgb, var(--theme-text-primary) 7%, transparent)')} onMouseLeave={e => (e.currentTarget.style.background = 'transparent')}>
                <span style={{ color: '#ff3b73', fontSize: '0.92rem', lineHeight: '1.1rem' }}>{proj.pinned ? '\uD83D\uDCCC' : '\uD83D\uDE80'}</span>
                <span style={{ minWidth: 0 }}>
                    {renamingTaskPath === proj.project_path ? <input autoFocus value={renameValue} onChange={e => setRenameValue(e.target.value)} onBlur={async () => { const trimmed = renameValue.trim(); if (trimmed && trimmed !== proj.name) { await renameTask(proj.project_path, trimmed); refreshRecentProjects(); } setRenamingTaskPath(null); }} onKeyDown={e => { if (e.key === 'Enter') (e.target as HTMLInputElement).blur(); if (e.key === 'Escape') setRenamingTaskPath(null); }} onClick={e => e.stopPropagation()} style={{ width: '100%', fontSize: '0.74rem', fontWeight: 700, color: 'var(--theme-text-primary)', background: 'var(--theme-surface)', border: '1px solid var(--theme-primary)', borderRadius: '4px', padding: '2px 4px', outline: 'none' }} /> : <span style={{ display: 'block', fontWeight: 700, fontSize: '0.74rem', color: 'var(--theme-text-primary)', whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{proj.name || proj.project_path}</span>}
                    <span style={{ display: 'block', marginTop: '4px', color: 'var(--theme-text-muted)', fontSize: '0.68rem', lineHeight: 1.32, whiteSpace: 'nowrap', overflow: 'hidden', textOverflow: 'ellipsis' }}>{proj.preview || proj.project_path}</span>
                </span>
            </div>
        ))}

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
