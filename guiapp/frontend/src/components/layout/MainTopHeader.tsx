import { useState, type Dispatch, type MouseEvent as ReactMouseEvent, type SetStateAction } from 'react';
import { getAllToolOptions, isToolTab, normalizeToolTab } from '../../config/toolCatalog';
import { getHeaderTitle } from './mainTopHeaderTitle';
import { MainTopHeaderActions } from './MainTopHeaderActions';
import { WindowCloseIcon, WindowMaximizeIcon, WindowRestoreIcon } from './WindowControlIcons';

interface MainTopHeaderProps {
    navTab: string;
    /** Current settings tab; lets the header name the Knowledge page opened from the library menu. */
    settingsTab?: string;
    lang: string;
    t: (key: string) => string;
    activeTool: string;
    switchTool: (tool: string) => void;
    handleAddNewProject: () => void;
    setRefreshStatus: Dispatch<SetStateAction<string>>;
    setTutorialContent: Dispatch<SetStateAction<string>>;
    setRefreshKey: Dispatch<SetStateAction<number>>;
    setShowModelSettings: Dispatch<SetStateAction<boolean>>;
    setSelectedSkillsToInstall: Dispatch<SetStateAction<string[]>>;
    setShowInstallSkillModal: Dispatch<SetStateAction<boolean>>;
    handleWindowHide: (e: ReactMouseEvent) => void;
    handleWindowMaximizeToggle: (e?: ReactMouseEvent) => void;
    windowMaximized: boolean;
}

const zhHans = {
    hideWindow: '\u9690\u85cf\u7a97\u53e3',
    maximizeWindow: '\u6700\u5927\u5316\u7a97\u53e3',
    restoreWindow: '\u8fd8\u539f\u7a97\u53e3',
};

const windowControlBtnStyle = {
    '--wails-draggable': 'no-drag',
    pointerEvents: 'auto',
    cursor: 'pointer',
    position: 'relative',
    zIndex: 10001,
    width: '36px',
    height: '28px',
    display: 'inline-flex',
    alignItems: 'center',
    justifyContent: 'center',
    background: 'transparent',
    border: 'none',
    borderRadius: '4px',
    padding: 0,
    lineHeight: 1,
    flexShrink: 0,
    color: 'var(--theme-text-secondary)',
    transition: 'background 120ms ease, color 120ms ease',
} as any;

export const MainTopHeader = ({
    navTab,
    settingsTab,
    lang,
    t,
    activeTool,
    switchTool,
    handleAddNewProject,
    setRefreshStatus,
    setTutorialContent,
    setRefreshKey,
    setShowModelSettings,
    setSelectedSkillsToInstall,
    setShowInstallSkillModal,
    handleWindowHide,
    handleWindowMaximizeToggle,
    windowMaximized,
}: MainTopHeaderProps) => {
    const [searchText, setSearchText] = useState('');
    const openTaskSearch = () => {
        window.dispatchEvent(new CustomEvent('maclaw:open-task-search', { detail: { query: searchText } }));
    };
    const openNotifications = () => {
        // The title-bar bell is a toggle.  Sidebar notification rows still
        // dispatch the plain event below to open the panel without closing it.
        window.dispatchEvent(new CustomEvent('maclaw:open-system-notifications', { detail: { toggle: true } }));
    };
    const safeActiveTool = normalizeToolTab(activeTool);
    const toolOptions = getAllToolOptions();
    const showToolSwitcher = isToolTab(navTab);
    return (
    <div className="top-header" data-window-drag style={{ '--wails-draggable': 'drag', userSelect: 'none' } as any} onDoubleClick={() => handleWindowMaximizeToggle()}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%' }}>
            <h2 style={{ margin: 0, fontSize: '1.05rem', color: 'var(--theme-text-primary)', fontWeight: 'bold', marginLeft: '20px', '--wails-draggable': 'drag', flex: 1, display: 'flex', alignItems: 'center' } as any}>
                <span className="mc-header-brand" aria-label="MaClaw">
                    <span>MaClaw</span>
                </span>
                {navTab !== 'ai' && (showToolSwitcher ? (
                    <select
                        className="top-header-tool-select"
                        value={safeActiveTool}
                        aria-label={lang === 'en' ? 'Coding tool' : '\u7f16\u7a0b\u5de5\u5177'}
                        onMouseDown={(event) => event.stopPropagation()}
                        onClick={(event) => event.stopPropagation()}
                        onDoubleClick={(event) => event.stopPropagation()}
                        onChange={(event) => switchTool(event.target.value)}
                    >
                        {toolOptions.map((tool) => (
                            <option key={tool.id} value={tool.id}>{tool.name}</option>
                        ))}
                    </select>
                ) : (
                    <span>{getHeaderTitle(navTab, lang, t, true, settingsTab)}</span>
                ))}
                <MainTopHeaderActions
                    navTab={navTab}
                    lang={lang}
                    t={t}
                    activeTool={safeActiveTool}
                    switchTool={switchTool}
                    handleAddNewProject={handleAddNewProject}
                    setRefreshStatus={setRefreshStatus}
                    setTutorialContent={setTutorialContent}
                    setRefreshKey={setRefreshKey}
                    setShowModelSettings={setShowModelSettings}
                    setSelectedSkillsToInstall={setSelectedSkillsToInstall}
                    setShowInstallSkillModal={setShowInstallSkillModal}
                />
            </h2>
            <div className="top-header-window-controls" style={{ display: 'flex', gap: '4px', '--wails-draggable': 'no-drag', marginRight: '5px', pointerEvents: 'auto', position: 'relative', zIndex: 10000 } as any}>
                <span className="mc-header-search-wrap">
                    <svg className="mc-header-search-icon" width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><circle cx="11" cy="11" r="6.5" /><path d="m16 16 4.5 4.5" /></svg>
                    <input className="mc-header-search" value={searchText} onChange={(event) => setSearchText(event.target.value)} onKeyDown={(event) => { if (event.key === 'Enter') openTaskSearch(); }} placeholder={lang === 'en' ? 'Search tasks, files, knowledge...' : '搜索任务、文件、知识…'} aria-label={lang === 'en' ? 'Search' : '搜索'} />
                </span>
                <button className="mc-header-notification" data-testid="main-header-notifications" type="button" onClick={openNotifications} aria-label={lang === 'en' ? 'Notifications' : '通知'} title={lang === 'en' ? 'Notifications' : '通知'}><svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M18 9a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9" /><path d="M10 21h4" /></svg></button>
                <span className="mc-header-ready"><i aria-hidden="true" />{lang === 'en' ? 'Ready' : '准备就绪'}</span>
                <button
                    onMouseDown={handleWindowHide}
                    aria-label={lang === 'en' ? 'Hide window' : zhHans.hideWindow}
                    title={lang === 'en' ? 'Hide window' : zhHans.hideWindow}
                    style={windowControlBtnStyle}
                >
                    <WindowCloseIcon />
                </button>
                <button
                    onMouseDown={handleWindowMaximizeToggle}
                    aria-label={windowMaximized ? (lang === 'en' ? 'Restore window' : zhHans.restoreWindow) : (lang === 'en' ? 'Maximize window' : zhHans.maximizeWindow)}
                    title={windowMaximized ? (lang === 'en' ? 'Restore window' : zhHans.restoreWindow) : (lang === 'en' ? 'Maximize window' : zhHans.maximizeWindow)}
                    style={windowControlBtnStyle}
                >
                    {windowMaximized ? <WindowRestoreIcon /> : <WindowMaximizeIcon />}
                </button>
            </div>
        </div>
    </div>
    );
};
