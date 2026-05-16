const zhHans = {
    taskManagement: '\u4efb\u52a1\u76d1\u63a7',
};

const zhHant = {
    taskManagement: '\u4efb\u52d9\u76e3\u63a7',
};

export const getHeaderTitle = (navTab: string, lang: string, t: (key: string) => string) => (
    navTab === 'claude' ? 'Claude Code' :
        navTab === 'gemini' ? 'Gemini CLI' :
            navTab === 'codex' ? 'OpenAI Codex' :
                navTab === 'opencode' ? 'OpenCode AI' :
                    navTab === 'codebuddy' ? 'CodeBuddy AI' :
                        navTab === 'cursor' ? 'Cursor Agent' :
                            navTab === 'iflow' ? 'iFlow CLI' :
                                navTab === 'kilo' ? 'Kilo Code CLI' :
                                    navTab === 'projects' ? t('projectManagement') :
                                        navTab === 'skills' ? t('skills') :
                                            navTab === 'tutorial' ? t('tutorial') :
                                                navTab === 'gossip' ? t('gossip') :
                                                    navTab === 'remote' ? (lang === 'zh-Hans' ? zhHans.taskManagement : lang === 'zh-Hant' ? zhHant.taskManagement : 'Task Monitor') :
                                                        navTab === 'api-store' ? t('apiStore') :
                                                            navTab === 'mcp' ? 'MCP' :
                                                                navTab === 'settings' ? t('globalSettings') :
                                                                    t('about')
);
