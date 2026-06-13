import type { Dispatch, SetStateAction } from 'react';
import { useState, useEffect, useCallback } from 'react';
import { LoadConfig, PatchConfigFields, SetDefaultLaunchMode, KnowledgeStats, KnowledgeSearch } from '../../../wailsjs/go/main/App';
const CodingKnowledgeStats = KnowledgeStats;
const CodingKnowledgeList = async (_args?: any) => [] as any[];
const CodingKnowledgeDelete = async (_id: string) => {};
const CodingKnowledgeConfirm = async (_id: string) => {};
const CodingKnowledgeReset = async () => {};
const CodingKnowledgeResetFile = async (_path?: string) => {};
const CodingKnowledgeSearch = async (query: string, _limit?: number) => KnowledgeSearch({ query } as any);
import { main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { getAllToolOptions } from '../../config/toolCatalog';

type ProgrammingToolsSettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
};

const textForLang = localizeText;

const saveConfigPatch = (
    config: main.AppConfig | null,
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>,
    patch: Record<string, any>,
) => {
    if (!config) return;
    const next = new main.AppConfig({ ...config, ...patch } as any);
    setConfig(next);
    PatchConfigFields(patch).then((saved) => {
        setConfig(new main.AppConfig(saved));
    }).catch((err) => {
        console.error('Failed to patch programming tool settings:', err);
        setConfig(config);
    });
};

const saveLaunchMode = async (
    config: main.AppConfig | null,
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>,
    mode: 'local' | 'remote',
) => {
    if (!config) return;
    const next = new main.AppConfig({
        ...config,
        default_launch_mode: mode,
        remote_enabled: mode === 'remote',
    } as any);
    setConfig(next);
    try {
        await SetDefaultLaunchMode(mode);
        const freshConfig = await LoadConfig();
        setConfig(freshConfig);
    } catch (err) {
        console.error('Failed to save launch mode:', err);
        setConfig(config);
    }
};

const visibleToolOptions = getAllToolOptions().filter(tool => tool.id !== 'claude');

export const ProgrammingToolsSettingsPanel = ({ config, setConfig, lang }: ProgrammingToolsSettingsPanelProps) => {
    const launchMode = config?.default_launch_mode === 'remote' ? 'remote' : 'local';

    return (
        <div className="settings-panel programming-tools-settings">
            <div className="programming-tools-settings__entry-card">
                <label className="programming-tools-settings__toggle">
                    <input
                        type="checkbox"
                        checked={!!(config as any)?.show_coding_tool_entry}
                        onChange={(e) => saveConfigPatch(config, setConfig, { show_coding_tool_entry: e.target.checked })}
                    />
                    <span>{textForLang(lang, 'Show coding tool entry in sidebar', '在侧边栏显示编程工具入口', '在側邊欄顯示程式工具入口')}</span>
                </label>
            </div>

            <div className="programming-tools-settings__visibility-card">
                <div className="programming-tools-settings__section-title">
                    {textForLang(lang, 'Visible Coding Tools', '可见的编程工具', '可見的程式工具')}
                </div>
                <div className="programming-tools-settings__visibility-grid">
                    {visibleToolOptions.map((tool) => {
                        const key = `show_${tool.id}`;
                        return (
                            <label className="programming-tools-settings__toggle" key={tool.id}>
                                <input
                                    type="checkbox"
                                    checked={(config as any)?.[key] !== false}
                                    onChange={(e) => saveConfigPatch(config, setConfig, { [key]: e.target.checked })}
                                />
                                <span>{tool.name}</span>
                            </label>
                        );
                    })}
                </div>
            </div>

            <div className="programming-tools-settings__launch-card">
                <div className="programming-tools-settings__section-title">
                    {textForLang(lang, 'Default Launch Mode', '默认启动模式', '預設啟動模式')}
                </div>
                <div className="programming-tools-settings__mode-options">
                    <label className="programming-tools-settings__mode-option" data-active={launchMode === 'local'}>
                        <input
                            type="radio"
                            name="launchMode"
                            checked={launchMode === 'local'}
                            onChange={() => { void saveLaunchMode(config, setConfig, 'local'); }}
                        />
                        <span>{textForLang(lang, 'Local', '本地', '本機')}</span>
                    </label>
                    <label className="programming-tools-settings__mode-option" data-active={launchMode === 'remote'}>
                        <input
                            type="radio"
                            name="launchMode"
                            checked={launchMode === 'remote'}
                            onChange={() => { void saveLaunchMode(config, setConfig, 'remote'); }}
                        />
                        <span>{textForLang(lang, 'Remote', '远程', '遠端')}</span>
                    </label>
                </div>
            </div>

            {/* Coding Knowledge Base Management */}
            <CodingKnowledgeSection config={config} setConfig={setConfig} lang={lang} />
        </div>
    );
};


// ---------------------------------------------------------------------------
// Coding Knowledge Base Management Section
// ---------------------------------------------------------------------------

const SCOPE_TABS = ['all', 'universal', 'go', 'python', 'typescript', 'cpp', 'rust', 'java', 'project'] as const;
type ScopeTab = typeof SCOPE_TABS[number];

const STATUS_ICONS: Record<string, string> = {
    verified: '\u2705',
    active: '\u25CF',
    candidate: '\u25CB',
    deprecated: '\u26A0\uFE0F',
};

function CodingKnowledgeSection({ config, setConfig, lang }: ProgrammingToolsSettingsPanelProps) {
    const [stats, setStats] = useState<any>(null);
    const [experiences, setExperiences] = useState<any[]>([]);
    const [activeTab, setActiveTab] = useState<ScopeTab>('all');
    const [searchQuery, setSearchQuery] = useState('');
    const [expanded, setExpanded] = useState(false);

    const autoSaveMode = (config as any)?.coding_knowledge_auto_save_mode || 'observe';
    const saveStrategy = (config as any)?.coding_knowledge_save_strategy || 'on_retry_success';

    const loadStats = useCallback(async () => {
        try {
            const s = await CodingKnowledgeStats();
            setStats(s);
        } catch { /* ignore */ }
    }, []);

    const loadExperiences = useCallback(async () => {
        try {
            let results: any[];
            if (searchQuery.trim()) {
                results = await CodingKnowledgeSearch(searchQuery, 50);
            } else {
                const filter: any = { limit: 100 };
                if (activeTab !== 'all') {
                    if (activeTab === 'universal' || activeTab === 'project') {
                        filter.scope = activeTab;
                    } else {
                        filter.scope = 'language';
                        filter.language = activeTab;
                    }
                }
                results = await CodingKnowledgeList(filter);
            }
            setExperiences(results || []);
        } catch { setExperiences([]); }
    }, [activeTab, searchQuery]);

    useEffect(() => { if (expanded) { void loadStats(); void loadExperiences(); } }, [expanded, loadStats, loadExperiences]);

    const handleDelete = async (id: string) => {
        try {
            await CodingKnowledgeDelete(id);
            void loadStats();
            void loadExperiences();
        } catch (err) { console.error('delete experience:', err); }
    };

    const handleConfirm = async (id: string) => {
        try {
            await CodingKnowledgeConfirm(id);
            void loadStats();
            void loadExperiences();
        } catch (err) { console.error('confirm experience:', err); }
    };

    const handleReset = async () => {
        if (!window.confirm(textForLang(lang, 'Reset all coding experiences? This cannot be undone.', '清空所有编程经验？此操作不可撤销。', '清空所有程式經驗？此操作不可撤銷。'))) return;
        try {
            await CodingKnowledgeResetFile();
            void loadStats();
            void loadExperiences();
        } catch (err) { console.error('reset knowledge:', err); }
    };

    return (
        <div className="programming-tools-settings__knowledge-card">
            <div className="programming-tools-settings__section-title" onClick={() => setExpanded(!expanded)} style={{ cursor: 'pointer' }}>
                {expanded ? '\u25BC' : '\u25B6'} {textForLang(lang, 'Coding Knowledge Base', '编程知识库', '程式知識庫')}
                {stats && <span style={{ marginLeft: 8, fontSize: '0.85em', opacity: 0.7 }}>
                    ({stats.total_count || 0} {textForLang(lang, 'entries', '条', '條')})
                </span>}
            </div>

            {expanded && (
                <div className="programming-tools-settings__knowledge-body">
                    {/* Config row */}
                    <div className="programming-tools-settings__knowledge-config">
                        <label>
                            {textForLang(lang, 'Auto-save mode:', '自动沉淀:', '自動沉澱:')}
                            <select value={autoSaveMode} onChange={(e) => saveConfigPatch(config, setConfig, { coding_knowledge_auto_save_mode: e.target.value })}>
                                <option value="observe">{textForLang(lang, 'Observe (candidate)', '观察模式（候选）', '觀察模式（候選）')}</option>
                                <option value="auto">{textForLang(lang, 'Auto (active)', '自动（直接生效）', '自動（直接生效）')}</option>
                                <option value="off">{textForLang(lang, 'Off', '关闭', '關閉')}</option>
                            </select>
                        </label>
                        <label>
                            {textForLang(lang, 'Save strategy:', '沉淀策略:', '沉澱策略:')}
                            <select value={saveStrategy} onChange={(e) => saveConfigPatch(config, setConfig, { coding_knowledge_save_strategy: e.target.value })}>
                                <option value="on_retry_success">{textForLang(lang, 'On retry success', '重试成功时', '重試成功時')}</option>
                                <option value="on_success">{textForLang(lang, 'On success', '成功时', '成功時')}</option>
                                <option value="always">{textForLang(lang, 'Always', '始终', '始終')}</option>
                                <option value="off">{textForLang(lang, 'Off', '关闭', '關閉')}</option>
                            </select>
                        </label>
                        <button className="programming-tools-settings__btn-danger" onClick={handleReset}>
                            {textForLang(lang, 'Reset All', '一键重置', '一鍵重置')}
                        </button>
                    </div>

                    {/* Stats bar */}
                    {stats && (
                        <div className="programming-tools-settings__knowledge-stats">
                            <span>{STATUS_ICONS.verified} {stats.verified_count || 0}</span>
                            <span>{STATUS_ICONS.active} {stats.active_count || 0}</span>
                            <span>{STATUS_ICONS.candidate} {stats.candidate_count || 0}</span>
                            <span>{STATUS_ICONS.deprecated} {stats.deprecated_count || 0}</span>
                        </div>
                    )}

                    {/* Scope tabs */}
                    <div className="programming-tools-settings__knowledge-tabs">
                        {SCOPE_TABS.map((tab) => (
                            <button
                                key={tab}
                                className={activeTab === tab ? 'active' : ''}
                                onClick={() => { setActiveTab(tab); setSearchQuery(''); }}
                            >
                                {tab === 'all' ? textForLang(lang, 'All', '全部', '全部')
                                    : tab === 'universal' ? textForLang(lang, 'Universal', '通用', '通用')
                                    : tab === 'project' ? textForLang(lang, 'Project', '项目', '專案')
                                    : tab.charAt(0).toUpperCase() + tab.slice(1)}
                            </button>
                        ))}
                    </div>

                    {/* Search */}
                    <input
                        className="programming-tools-settings__knowledge-search"
                        placeholder={textForLang(lang, 'Search experiences...', '搜索经验...', '搜尋經驗...')}
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                        onKeyDown={(e) => { if (e.key === 'Enter') void loadExperiences(); }}
                    />

                    {/* Experience list */}
                    <div className="programming-tools-settings__knowledge-list">
                        {experiences.length === 0 && (
                            <div className="programming-tools-settings__knowledge-empty">
                                {textForLang(lang, 'No experiences yet.', '暂无经验。', '暫無經驗。')}
                            </div>
                        )}
                        {experiences.map((exp) => (
                            <div key={exp.id} className="programming-tools-settings__knowledge-item" data-status={exp.status}>
                                <div className="programming-tools-settings__knowledge-item-header">
                                    <span className="programming-tools-settings__knowledge-item-icon">{STATUS_ICONS[exp.status] || ''}</span>
                                    <span className="programming-tools-settings__knowledge-item-title">{exp.title}</span>
                                    <span className="programming-tools-settings__knowledge-item-conf">conf: {(exp.confidence || 1.0).toFixed(1)}</span>
                                </div>
                                <div className="programming-tools-settings__knowledge-item-meta">
                                    {exp.category && <span>{exp.category}</span>}
                                    {exp.scope && <span>{exp.scope}{exp.language ? `:${exp.language}` : ''}</span>}
                                    {exp.recall_count > 0 && <span>{textForLang(lang, `recalled ${exp.recall_count}x`, `召回 ${exp.recall_count} 次`, `召回 ${exp.recall_count} 次`)}</span>}
                                </div>
                                <div className="programming-tools-settings__knowledge-item-actions">
                                    {exp.status === 'candidate' && (
                                        <button onClick={() => void handleConfirm(exp.id)}>
                                            {textForLang(lang, 'Confirm', '确认', '確認')}
                                        </button>
                                    )}
                                    <button onClick={() => void handleDelete(exp.id)}>
                                        {textForLang(lang, 'Delete', '删除', '刪除')}
                                    </button>
                                </div>
                            </div>
                        ))}
                    </div>
                </div>
            )}
        </div>
    );
}
