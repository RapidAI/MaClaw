import type { Dispatch, MouseEvent as ReactMouseEvent, SetStateAction } from 'react';
import { ReadTutorial } from '../../../wailsjs/go/main/App';
import { isSkillTool, isToolTab } from '../../config/toolCatalog';
import { getHeaderTitle } from './mainTopHeaderTitle';

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
    back: '\u8fd4\u56de',
    providerConfig: '\u670d\u52a1\u5546\u914d\u7f6e',
    minimizeWindow: '\u6700\u5c0f\u5316\u7a97\u53e3',
};

const zhHant = {
    providerConfig: '\u670d\u52d9\u5546\u914d\u7f6e',
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
                {navTab === 'projects' && (
                    <>
                        <button
                            onClick={() => switchTool(activeTool)}
                            className="btn-link"
                            style={{
                                marginLeft: '10px',
                                fontSize: '0.8rem',
                                padding: '4px 12px',
                            }}
                            title="Back"
                        >&lt;&lt; {t('back') || zhHans.back}</button>
                        <button
                            className="btn-primary"
                            style={{ marginLeft: '10px', padding: '4px 12px', fontSize: '0.8rem' }}
                            onClick={handleAddNewProject}
                        >{t('addNewProject')}</button>
                    </>
                )}
                {navTab === 'tutorial' && (
                    <button
                        className="btn-link"
                        style={{ marginLeft: '10px', fontSize: '0.8rem', padding: '4px 12px' }}
                        onClick={async () => {
                            try {
                                setRefreshStatus(t('refreshing'));
                                setTutorialContent('');
                                const content = await ReadTutorial();
                                setRefreshStatus(t('refreshSuccess'));
                                setTutorialContent(content);
                                setRefreshKey(prev => prev + 1);
                                setTimeout(() => setRefreshStatus(''), 5000);
                            } catch (err) {
                                setRefreshStatus(t('refreshFailed') + err);
                                setTimeout(() => setRefreshStatus(''), 5000);
                            }
                        }}
                    >
                        {t('refreshMessage')}
                    </button>
                )}
                {isToolTab(navTab) && (
                    <>
                        <button
                            className="btn-link"
                            onClick={() => setShowModelSettings(true)}
                            style={{
                                marginLeft: '10px',
                                padding: '2px 8px',
                                fontSize: '0.8rem',
                                borderColor: '#6366f1',
                                color: '#6366f1',
                                '--wails-draggable': 'no-drag',
                            } as any}
                        >
                            {lang === 'zh-Hans' ? zhHans.providerConfig : lang === 'zh-Hant' ? zhHant.providerConfig : 'Provider Config'}
                        </button>
                        {isSkillTool(navTab) && (
                            <button
                                className="btn-link"
                                onClick={() => {
                                    setSelectedSkillsToInstall([]);
                                    setShowInstallSkillModal(true);
                                }}
                                style={{
                                    marginLeft: '10px',
                                    padding: '2px 8px',
                                    fontSize: '0.8rem',
                                    borderColor: '#10b981',
                                    color: '#10b981',
                                    '--wails-draggable': 'no-drag',
                                } as any}
                            >
                                {t('installSkills')}
                            </button>
                        )}
                        <button
                            className="btn-link"
                            onClick={() => switchTool('api-store')}
                            style={{
                                marginLeft: '10px',
                                padding: '2px 8px',
                                fontSize: '0.8rem',
                                borderColor: '#c65c37',
                                color: '#c65c37',
                                '--wails-draggable': 'no-drag',
                            } as any}
                        >
                            {t('apiStore')}
                        </button>
                    </>
                )}
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
