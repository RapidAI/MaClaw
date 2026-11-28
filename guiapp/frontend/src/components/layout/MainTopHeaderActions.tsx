import { type Dispatch, type MouseEvent as ReactMouseEvent, type SetStateAction } from 'react';
import { ReadTutorial } from '../../../wailsjs/go/main/App';
import { isSkillTool, isToolTab } from '../../config/toolCatalog';
import { localizeText } from '../../i18n';

const zhHans = {
    back: '\u8fd4\u56de',
    providerConfig: '\u670d\u52a1\u5546\u914d\u7f6e',
};

const zhHant = {
    providerConfig: '\u670d\u52d9\u5546\u914d\u7f6e',
};

type MainTopHeaderActionsProps = {
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
};

// These buttons render inside the window-drag title. Plain <button>s are
// already excluded from drag arming, but a bubbling double-click would toggle
// maximize on the header — same guard as SidebarToggleCluster / mth-bot-actions.
const keepHeaderBehavior = (event: ReactMouseEvent) => {
    event.stopPropagation();
};

export const MainTopHeaderActions = ({
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
}: MainTopHeaderActionsProps) => {
    // Mobile documents entry lives on AI assistant title bar (icon) instead of main chrome.
    return (
    <>
        {navTab === 'projects' && (
            <>
                <button onMouseDown={keepHeaderBehavior} onDoubleClick={keepHeaderBehavior} onClick={() => switchTool(activeTool)} className="btn-link" style={{ marginLeft: '10px', fontSize: '0.8rem', padding: '4px 12px' }} title="Back">
                    &lt;&lt; {t('back') || zhHans.back}
                </button>
                <button onMouseDown={keepHeaderBehavior} onDoubleClick={keepHeaderBehavior} className="btn-primary" style={{ marginLeft: '10px', padding: '4px 12px', fontSize: '0.8rem' }} onClick={handleAddNewProject}>
                    {t('addNewProject')}
                </button>
            </>
        )}
        {navTab === 'tutorial' && (
            <button
                className="btn-link"
                onMouseDown={keepHeaderBehavior}
                onDoubleClick={keepHeaderBehavior}
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
                <button onMouseDown={keepHeaderBehavior} onDoubleClick={keepHeaderBehavior} className="btn-link" onClick={() => setShowModelSettings(true)} style={{ marginLeft: '10px', padding: '2px 8px', fontSize: '0.8rem', borderColor: 'var(--theme-primary)', color: 'var(--theme-primary)', '--wails-draggable': 'no-drag' } as any}>
                    {localizeText(lang, 'Provider Config', zhHans.providerConfig, zhHant.providerConfig)}
                </button>
                {isSkillTool(navTab) && (
                    <button onMouseDown={keepHeaderBehavior} onDoubleClick={keepHeaderBehavior} className="btn-link" onClick={() => { setSelectedSkillsToInstall([]); setShowInstallSkillModal(true); }} style={{ marginLeft: '10px', padding: '2px 8px', fontSize: '0.8rem', borderColor: 'var(--theme-success)', color: 'var(--theme-success)', '--wails-draggable': 'no-drag' } as any}>
                        {t('installSkills')}
                    </button>
                )}
                <button onMouseDown={keepHeaderBehavior} onDoubleClick={keepHeaderBehavior} className="btn-link" onClick={() => switchTool('api-store')} style={{ marginLeft: '10px', padding: '2px 8px', fontSize: '0.8rem', borderColor: 'var(--theme-warning)', color: 'var(--theme-warning)', '--wails-draggable': 'no-drag' } as any}>
                    {t('apiStore')}
                </button>
            </>
        )}
    </>
    );
};
