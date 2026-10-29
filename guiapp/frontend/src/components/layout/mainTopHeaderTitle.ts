import { getSettingsTabOptions } from '../../config/settingsTabs';
import { getToolLabel, isToolTab } from '../../config/toolCatalog';
import { miniAppShortLabel } from '../../i18n/maclawMiniAppLabels';
import { localizeText, normalizeLang } from '../../i18n/langSelect';
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

/** Normalized lang → tab id → rail label. The header runs on the render-hot
 *  path; building the 25-entry tab list each time is wasted work. */
const settingsTitleCache = new Map<string, Map<string, string>>();

/**
 * Name the header after the active settings tab (same label the rail shows),
 * so "AI 模型" reads as "AI 模型" instead of a generic 全局设置.
 * Unknown / missing tab ids fall back to the generic settings title.
 */
function settingsPageTitle(lang: string, t: (key: string) => string, settingsTab?: string) {
    if (settingsTab) {
        const normalized = normalizeLang(lang);
        let titles = settingsTitleCache.get(normalized);
        if (!titles) {
            titles = new Map(getSettingsTabOptions(normalized).map((tab) => [tab.id, tab.label]));
            settingsTitleCache.set(normalized, titles);
        }
        const label = titles.get(settingsTab);
        if (label) return label;
    }
    return t('globalSettings');
}

export const getHeaderTitle = (navTab: string, lang: string, t: (key: string) => string, splitUtilities = false, settingsTab?: string) => (
    isToolTab(navTab) ? getToolLabel(navTab) :
        navTab === 'projects' ? t('projectManagement') :
            navTab === 'apps' ? miniAppShortLabel(lang) :
                navTab === 'utilities' ? (splitUtilities ? expertsPageTitle(lang) : utilitiesPageTitle(lang)) :
                    navTab === 'tools' ? toolsPageTitle(lang) :
                    navTab === 'workflows' ? localizeText(lang, 'Workflows', zhHans.workflows, zhHant.workflows) :
                navTab === 'skills' ? t('skills') :
                    navTab === 'tutorial' ? t('tutorial') :
                        navTab === 'gossip' ? t('gossip') :
                            navTab === 'remote' ? localizeText(lang, 'Task Monitor', zhHans.taskManagement, zhHant.taskManagement) :
                                navTab === 'bots' ? localizeText(lang, 'Bot management', 'Bot 管理') :
                            navTab === 'files' ? localizeText(lang, 'Cloud drive', '云盘', '雲端硬碟') :
                                navTab === 'api-store' ? t('apiStore') :
                                    navTab === 'mcp' ? 'MCP' :
                                        navTab === 'settings' ? settingsPageTitle(lang, t, settingsTab) :
                                            navTab === LATEX_TEMPLATES_NAV_TAB ? localizeText(lang, 'LaTeX Templates', 'LaTeX 模板', 'LaTeX 模板') :
                                            t('about')
);
