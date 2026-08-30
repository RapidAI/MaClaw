import { getToolLabel, isToolTab } from '../../config/toolCatalog';
import { miniAppShortLabel } from '../../i18n/maclawMiniAppLabels';
import { utilitiesPageTitle } from '../../i18n/utilitiesLabels';

const zhHans = {
    taskManagement: '\u4efb\u52a1\u76d1\u63a7',
    workflows: '\u5de5\u4f5c\u6d41',
};

const zhHant = {
    taskManagement: '\u4efb\u52d9\u76e3\u63a7',
    workflows: '\u5de5\u4f5c\u6d41',
};

export const getHeaderTitle = (navTab: string, lang: string, t: (key: string) => string) => (
    isToolTab(navTab) ? getToolLabel(navTab) :
        navTab === 'projects' ? t('projectManagement') :
            navTab === 'apps' ? miniAppShortLabel(lang) :
                navTab === 'utilities' ? utilitiesPageTitle(lang) :
                    navTab === 'workflows' ? (lang === 'zh-Hans' ? zhHans.workflows : lang === 'zh-Hant' ? zhHant.workflows : 'Workflows') :
                navTab === 'skills' ? t('skills') :
                    navTab === 'tutorial' ? t('tutorial') :
                        navTab === 'gossip' ? t('gossip') :
                            navTab === 'remote' ? (lang === 'zh-Hans' ? zhHans.taskManagement : lang === 'zh-Hant' ? zhHant.taskManagement : 'Task Monitor') :
                                navTab === 'api-store' ? t('apiStore') :
                                    navTab === 'mcp' ? 'MCP' :
                                        navTab === 'settings' ? t('globalSettings') :
                                            t('about')
);
