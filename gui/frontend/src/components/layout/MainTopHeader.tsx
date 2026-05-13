import type { Dispatch, MouseEvent as ReactMouseEvent, SetStateAction } from 'react';
import { getHeaderTitle } from './mainTopHeaderTitle';
import { MainTopHeaderActions } from './MainTopHeaderActions';

interface MainTopHeaderProps {
    navTab: string;
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
    handleWindowMaximizeToggle: (e: ReactMouseEvent) => void;
    windowMaximized: boolean;
}

const zhHans = {
    minimizeWindow: '\u6700\u5c0f\u5316\u7a97\u53e3',
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
}: MainTopHeaderProps) => (
    <div className="top-header" style={{ '--wails-draggable': 'drag' } as any}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', width: '100%' }}>
            <h2 style={{ margin: 0, fontSize: '1.05rem', color: 'var(--theme-text-primary)', fontWeight: 'bold', marginLeft: '20px', '--wails-draggable': 'drag', flex: 1, display: 'flex', alignItems: 'center' } as any}>
                <span>{getHeaderTitle(navTab, lang, t)}</span>
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
                    aria-label={lang === 'en' ? 'Minimize window' : zhHans.minimizeWindow}
                    title={lang === 'en' ? 'Minimize window' : zhHans.minimizeWindow}
                    style={windowControlBtnStyle}
                >
                    <span style={{ width: '10px', borderTop: '1.5px solid currentColor', transform: 'translateY(4px)' }} />
                </button>
                <button
                    onMouseDown={handleWindowMaximizeToggle}
                    aria-label={windowMaximized ? (lang === 'en' ? 'Restore window' : zhHans.restoreWindow) : (lang === 'en' ? 'Maximize window' : zhHans.maximizeWindow)}
                    title={windowMaximized ? (lang === 'en' ? 'Restore window' : zhHans.restoreWindow) : (lang === 'en' ? 'Maximize window' : zhHans.maximizeWindow)}
                    style={windowControlBtnStyle}
                >
                    {windowMaximized ? (
                        <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.2">
                            <rect x="0.6" y="2.6" width="6.8" height="6.8" rx="0.5" />
                            <path d="M2.6 2.6V1.1a0.5 0.5 0 0 1 0.5-0.5h6.3a0.5 0.5 0 0 1 0.5 0.5v6.3a0.5 0.5 0 0 1-0.5 0.5H8.4" />
                        </svg>
                    ) : (
                        <svg width="10" height="10" viewBox="0 0 10 10" fill="none" stroke="currentColor" strokeWidth="1.2">
                            <rect x="0.6" y="0.6" width="8.8" height="8.8" rx="0.5" />
                        </svg>
                    )}
                </button>
            </div>
        </div>
    </div>
);
