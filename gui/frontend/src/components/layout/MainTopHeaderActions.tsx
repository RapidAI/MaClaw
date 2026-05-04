import type { Dispatch, SetStateAction } from 'react';
import { ReadTutorial } from '../../../wailsjs/go/main/App';
import { isSkillTool, isToolTab } from '../../config/toolCatalog';

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
}: MainTopHeaderActionsProps) => (
    <>
        {navTab === 'projects' && (
            <>
                <button onClick={() => switchTool(activeTool)} className="btn-link" style={{ marginLeft: '10px', fontSize: '0.8rem', padding: '4px 12px' }} title="Back">
                    &lt;&lt; {t('back') || zhHans.back}
                </button>
                <button className="btn-primary" style={{ marginLeft: '10px', padding: '4px 12px', fontSize: '0.8rem' }} onClick={handleAddNewProject}>
                    {t('addNewProject')}
                </button>
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
                <button className="btn-link" onClick={() => setShowModelSettings(true)} style={{ marginLeft: '10px', padding: '2px 8px', fontSize: '0.8rem', borderColor: 'var(--theme-primary)', color: 'var(--theme-primary)', '--wails-draggable': 'no-drag' } as any}>
                    {lang === 'zh-Hans' ? zhHans.providerConfig : lang === 'zh-Hant' ? zhHant.providerConfig : 'Provider Config'}
                </button>
                {isSkillTool(navTab) && (
                    <button className="btn-link" onClick={() => { setSelectedSkillsToInstall([]); setShowInstallSkillModal(true); }} style={{ marginLeft: '10px', padding: '2px 8px', fontSize: '0.8rem', borderColor: 'var(--theme-success)', color: 'var(--theme-success)', '--wails-draggable': 'no-drag' } as any}>
                        {t('installSkills')}
                    </button>
                )}
                <button className="btn-link" onClick={() => switchTool('api-store')} style={{ marginLeft: '10px', padding: '2px 8px', fontSize: '0.8rem', borderColor: 'var(--theme-warning)', color: 'var(--theme-warning)', '--wails-draggable': 'no-drag' } as any}>
                    {t('apiStore')}
                </button>
            </>
        )}
    </>
);
