import { getToolLabel, isToolTab } from '../../config/toolCatalog';
import { miniAppShortLabel } from '../../i18n/maclawMiniAppLabels';
import { expertsPageTitle, toolsPageTitle, utilitiesPageTitle } from '../../i18n/utilitiesLabels';
import { LATEX_TEMPLATES_NAV_TAB } from '../../utils/latexTemplates';

const zhHans = {
    taskManagement: '\u4efb\u52a1\u76d1\u63a7',
    workflows: '\u5de5\u4f5c\u6d41',
};

const zhHant = {
    taskManagement: '\u4efb\u52d9\u76e3\u63a7',
    workflows: '\u5de5\u4f5c\u6d41',
};

function settingsPageTitle(lang: string, t: (key: string) => string, settingsTab?: string) {
    if (settingsTab === 'knowledge') {
        if (lang === 'zh-Hans') return '知识库';
        if (lang === 'zh-Hant') return '知識庫';
        return 'Knowledge base';
    }
    if (settingsTab === 'email') {
        if (lang === 'zh-Hans') return '邮箱配置';
        if (lang === 'zh-Hant') return '郵箱配置';
        return 'Mailbox configuration';
    }
    if (settingsTab === 'im') {
        if (lang === 'zh-Hans') return '即时通讯';
        if (lang === 'zh-Hant') return '即時通訊';
        return 'Messaging';
    }
    return t('globalSettings');
}

export const getHeaderTitle = (navTab: string, lang: string, t: (key: string) => string, splitUtilities = false, settingsTab?: string) => (
    isToolTab(navTab) ? getToolLabel(navTab) :
        navTab === 'projects' ? t('projectManagement') :
            navTab === 'apps' ? miniAppShortLabel(lang) :
                navTab === 'utilities' ? (splitUtilities ? expertsPageTitle(lang) : utilitiesPageTitle(lang)) :
                    navTab === 'tools' ? toolsPageTitle(lang) :
                    navTab === 'workflows' ? (lang === 'zh-Hans' ? zhHans.workflows : lang === 'zh-Hant' ? zhHant.workflows : 'Workflows') :
                navTab === 'skills' ? t('skills') :
                    navTab === 'tutorial' ? t('tutorial') :
                        navTab === 'gossip' ? t('gossip') :
                            navTab === 'remote' ? (lang === 'zh-Hans' ? zhHans.taskManagement : lang === 'zh-Hant' ? zhHant.taskManagement : 'Task Monitor') :
                                navTab === 'bots' ? (lang === 'en' ? 'Bot management' : 'Bot 管理') :
                            navTab === 'files' ? (lang === 'zh-Hans' ? '云盘' : lang === 'zh-Hant' ? '雲端硬碟' : 'Cloud drive') :
                                navTab === 'api-store' ? t('apiStore') :
                                    navTab === 'mcp' ? 'MCP' :
                                        navTab === 'settings' ? settingsPageTitle(lang, t, settingsTab) :
                                            navTab === LATEX_TEMPLATES_NAV_TAB ? (lang === 'zh-Hant' ? 'LaTeX 模板' : lang === 'en' ? 'LaTeX Templates' : 'LaTeX 模板') :
                                            t('about')
);
