import { type Dispatch, type MouseEvent as ReactMouseEvent, type SetStateAction } from 'react';
import { getAllToolOptions, isToolTab, normalizeToolTab } from '../../config/toolCatalog';
import { windowDragHandleProps } from '../../utils/windowDrag';
import { getHeaderTitle } from './mainTopHeaderTitle';
import { MainTopHeaderActions } from './MainTopHeaderActions';
import { WindowCloseIcon, WindowMaximizeIcon, WindowRestoreIcon } from './WindowControlIcons';
import { localizeText } from '../../i18n';


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
    const openNotifications = () => {
        // The title-bar bell is a toggle.  Sidebar notification rows still
        // dispatch the plain event below to open the panel without closing it.
        window.dispatchEvent(new CustomEvent('maclaw:open-system-notifications', { detail: { toggle: true } }));
    };
    const safeActiveTool = normalizeToolTab(activeTool);
    const toolOptions = getAllToolOptions();
    const showToolSwitcher = isToolTab(navTab);
    return (
    <div className="top-header" {...windowDragHandleProps(true, { userSelect: 'none' })} onDoubleClick={() => handleWindowMaximizeToggle()}>
        <div className="mth-row">
            <h2 className="mth-title">
                <span className="mc-header-brand" aria-label="MaClaw">
                    <span>MaClaw</span>
                </span>
                {navTab !== 'ai' && (showToolSwitcher ? (
                    <select
                        className="top-header-tool-select"
                        value={safeActiveTool}
                        aria-label={localizeText(lang, 'Coding tool', '编程工具')}
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
            <div className="top-header-window-controls mth-window-controls">
                <button className="mc-header-notification" data-testid="main-header-notifications" type="button" onClick={openNotifications} aria-label={localizeText(lang, 'Notifications', '通知')} title={localizeText(lang, 'Notifications', '通知')}><svg width="17" height="17" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M18 9a6 6 0 0 0-12 0c0 7-3 7-3 9h18c0-2-3-2-3-9" /><path d="M10 21h4" /></svg></button>
                <span className="mc-header-ready"><i aria-hidden="true" />{localizeText(lang, 'Ready', '准备就绪')}</span>
                <button
                    onMouseDown={handleWindowHide}
                    aria-label={localizeText(lang, 'Hide window', zhHans.hideWindow)}
                    title={localizeText(lang, 'Hide window', zhHans.hideWindow)}
                    style={windowControlBtnStyle}
                >
                    <WindowCloseIcon />
                </button>
                <button
                    onMouseDown={handleWindowMaximizeToggle}
                    aria-label={windowMaximized ? localizeText(lang, 'Restore window', zhHans.restoreWindow) : localizeText(lang, 'Maximize window', zhHans.maximizeWindow)}
                    title={windowMaximized ? localizeText(lang, 'Restore window', zhHans.restoreWindow) : localizeText(lang, 'Maximize window', zhHans.maximizeWindow)}
                    style={windowControlBtnStyle}
                >
                    {windowMaximized ? <WindowRestoreIcon /> : <WindowMaximizeIcon />}
                </button>
            </div>
        </div>
    </div>
    );
};
