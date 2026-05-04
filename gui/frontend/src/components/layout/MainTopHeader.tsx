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
}

const zhHans = {
    minimizeWindow: '\u6700\u5c0f\u5316\u7a97\u53e3',
};

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
}: MainTopHeaderProps) => (
    <div className="top-header" style={{ '--wails-draggable': 'no-drag' } as any}>
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
            <div style={{ display: 'flex', gap: '10px', '--wails-draggable': 'no-drag', marginRight: '5px', pointerEvents: 'auto', position: 'relative', zIndex: 10000 } as any}>
                <button
                    onMouseDown={handleWindowHide}
                    aria-label={lang === 'en' ? 'Minimize window' : zhHans.minimizeWindow}
                    title={lang === 'en' ? 'Minimize window' : zhHans.minimizeWindow}
                    style={{
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
                    } as any}
                >
                    <span style={{ width: '10px', borderTop: '1.5px solid currentColor', transform: 'translateY(4px)' }} />
                </button>
            </div>
        </div>
    </div>
);
