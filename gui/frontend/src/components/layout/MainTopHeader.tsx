import type { Dispatch, MouseEvent as ReactMouseEvent, SetStateAction } from 'react';
import { getToolLabel, getVisibleToolOptions, isToolTab } from '../../config/toolCatalog';
import { getHeaderTitle } from './mainTopHeaderTitle';
import { MainTopHeaderActions } from './MainTopHeaderActions';
import { WindowCloseIcon, WindowMaximizeIcon, WindowRestoreIcon } from './WindowControlIcons';

interface MainTopHeaderProps {
    navTab: string;
    lang: string;
    t: (key: string) => string;
    activeTool: string;
    config: any;
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
    lang,
    t,
    activeTool,
    config,
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
    const visibleToolOptions = getVisibleToolOptions(config);
    const toolOptions = visibleToolOptions.some((tool) => tool.id === activeTool)
        ? visibleToolOptions
        : [{ id: activeTool, name: getToolLabel(activeTool) }, ...visibleToolOptions];
    const showToolSwitcher = isToolTab(navTab);
    return (
    <div className="top-header" style={{ '--wails-draggable': 'drag' } as any} onDoubleClick={() => handleWindowMaximizeToggle()}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%' }}>
            <h2 style={{ margin: 0, fontSize: '1.05rem', color: 'var(--theme-text-primary)', fontWeight: 'bold', marginLeft: '20px', '--wails-draggable': 'drag', flex: 1, display: 'flex', alignItems: 'center' } as any}>
                {showToolSwitcher ? (
                    <select
                        className="top-header-tool-select"
                        value={activeTool}
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
                    <span>{getHeaderTitle(navTab, lang, t)}</span>
                )}
                <MainTopHeaderActions
                    navTab={navTab}
                    lang={lang}
                    t={t}
                    activeTool={activeTool}
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
