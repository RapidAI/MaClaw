import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { useState, useEffect, useCallback, useRef, useId } from 'react';
import { LoadConfig, PatchConfigFields, SetDefaultLaunchMode, KnowledgeStats, KnowledgeSearch } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { getAllToolOptions } from '../../config/toolCatalog';
import { useDialog } from '../CustomDialog';

// Wails bindings stubs — these functions are not yet generated but the
// component references them. Replace with actual imports when available.
const CodingKnowledgeStats = KnowledgeStats;
const CodingKnowledgeList = async (_args?: any) => [] as any[];
const CodingKnowledgeDelete = async (_id: string) => {};
const CodingKnowledgeConfirm = async (_id: string) => {};
const CodingKnowledgeResetFile = async (_path?: string) => {};
const CodingKnowledgeSearch = async (query: string, _limit?: number) => KnowledgeSearch({ query } as any);

type ProgrammingToolsSettingsPanelProps = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
};

const textForLang = localizeText;

/** Typed config accessor to reduce `as any` casts */
const cfgVal = <T,>(config: main.AppConfig | null, key: string, fallback: T): T => {
    if (!config) return fallback;
    const val = (config as unknown as Record<string, unknown>)[key];
    return (val === undefined || val === null) ? fallback : val as T;
};

/**
 * Patch config with optimistic update + stale-response protection.
 * Uses a monotonic version counter so that if two rapid patches overlap,
 * the .then() of the first one won't overwrite the optimistic state of
 * the second. The counter ref is passed in from the component instance.
 */
const saveConfigPatch = (
    config: main.AppConfig | null,
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>,
    patch: Record<string, any>,
    versionRef: MutableRefObject<number>,
) => {
    if (!config) return;
    const myVersion = ++versionRef.current;
    const next = new main.AppConfig({ ...config, ...patch } as any);
    setConfig(next);
    PatchConfigFields(patch).then((saved) => {
        if (myVersion === versionRef.current) {
            setConfig(new main.AppConfig(saved));
        }
    }).catch((err) => {
        console.error('Failed to patch programming tool settings:', err);
        if (myVersion === versionRef.current) {
            setConfig(config);
        }
    });
};

const saveLaunchMode = (
    config: main.AppConfig | null,
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>,
    mode: 'local' | 'remote',
    versionRef: MutableRefObject<number>,
) => {
    if (!config) return;
    const myVersion = ++versionRef.current;
    const next = new main.AppConfig({
        ...config,
        default_launch_mode: mode,
        remote_enabled: mode === 'remote',
    } as any);
    setConfig(next);
    SetDefaultLaunchMode(mode).then(() => LoadConfig()).then((freshConfig) => {
        if (myVersion === versionRef.current) {
            setConfig(freshConfig);
        }
    }).catch((err) => {
        console.error('Failed to save launch mode:', err);
        if (myVersion === versionRef.current) {
            setConfig(config);
        }
    });
};

const visibleToolOptions = getAllToolOptions().filter(tool => tool.id !== 'claude');

/** Debounce hook for search input */
function useDebouncedValue(value: string, delayMs: number): string {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        const timer = setTimeout(() => setDebounced(value), delayMs);
        return () => clearTimeout(timer);
    }, [value, delayMs]);
    return debounced;
}

export const ProgrammingToolsSettingsPanel = ({ config, setConfig, lang }: ProgrammingToolsSettingsPanelProps) => {
    const launchMode = config?.default_launch_mode === 'remote' ? 'remote' : 'local';
    const versionRef = useRef(0);
    const uid = useId();

    const patch = (p: Record<string, any>) => saveConfigPatch(config, setConfig, p, versionRef);
    const setMode = (m: 'local' | 'remote') => saveLaunchMode(config, setConfig, m, versionRef);

    return (
        <div className="settings-panel prog-tools">
            {/* Section 1: Tool Configuration */}
            <section className="prog-tools__section" aria-labelledby={`${uid}-config-heading`}>
                <h3 className="prog-tools__section-title" id={`${uid}-config-heading`}>
                    <svg className="prog-tools__section-icon" viewBox="0 0 20 20" fill="currentColor" width="16" height="16" aria-hidden="true">
                        <path d="M10 3.5a1.5 1.5 0 013 0V4a1 1 0 001 1h3a1 1 0 011 1v3a1 1 0 01-1 1h-.5a1.5 1.5 0 000 3h.5a1 1 0 011 1v3a1 1 0 01-1 1h-3a1 1 0 01-1-1v-.5a1.5 1.5 0 00-3 0v.5a1 1 0 01-1 1H6a1 1 0 01-1-1v-3a1 1 0 00-1-1h-.5a1.5 1.5 0 010-3H4a1 1 0 001-1V6a1 1 0 011-1h3a1 1 0 001-1v-.5z" />
                    </svg>
                    {textForLang(lang, 'Tool Configuration', '工具配置', '工具配置')}
                </h3>
                <div className="prog-tools__card">
                    {/* Sidebar Entry Toggle */}
                    <div className="prog-tools__field">
                        <span className="prog-tools__field-label" id={`${uid}-sidebar-label`}>
                            {textForLang(lang, 'Sidebar Entry', '侧边栏入口', '側邊欄入口')}
                        </span>
                        <label className="prog-tools__switch" role="switch" aria-checked={!!cfgVal(config, 'show_coding_tool_entry', false)} aria-labelledby={`${uid}-sidebar-label`}>
                            <input
                                type="checkbox"
                                checked={cfgVal(config, 'show_coding_tool_entry', false)}
                                onChange={(e) => patch({ show_coding_tool_entry: e.target.checked })}
                            />
                            <span className="prog-tools__switch-slider" aria-hidden="true" />
                        </label>
                    </div>

                    <div className="prog-tools__divider" aria-hidden="true" />

                    {/* Visible Tools */}
                    <div className="prog-tools__field prog-tools__field--vertical">
                        <span className="prog-tools__field-label">
                            {textForLang(lang, 'Enabled Tools', '启用的工具', '啟用的工具')}
                        </span>
                        <div className="prog-tools__tool-grid" role="group" aria-label={textForLang(lang, 'Enabled Tools', '启用的工具', '啟用的工具')}>
                            {visibleToolOptions.map((tool) => {
                                const key = `show_${tool.id}`;
                                const checked = cfgVal(config, key, true);
                                return (
                                    <label className="prog-tools__tool-chip" key={tool.id} data-active={checked}>
                                        <input
                                            type="checkbox"
                                            checked={checked}
                                            onChange={(e) => patch({ [key]: e.target.checked })}
                                            aria-label={tool.name}
                                        />
                                        <span className="prog-tools__tool-chip-label">{tool.name}</span>
                                    </label>
                                );
                            })}
                        </div>
                    </div>

                    <div className="prog-tools__divider" aria-hidden="true" />

                    {/* Launch Mode */}
                    <div className="prog-tools__field">
                        <span className="prog-tools__field-label" id={`${uid}-mode-label`}>
                            {textForLang(lang, 'Default Mode', '默认模式', '預設模式')}
                        </span>
                        <div className="prog-tools__mode-toggle" role="group" aria-labelledby={`${uid}-mode-label`}>
                            <button
                                className="prog-tools__mode-btn"
                                data-active={launchMode === 'local'}
                                aria-pressed={launchMode === 'local'}
                                onClick={() => setMode('local')}
                            >
                                <svg viewBox="0 0 20 20" fill="currentColor" width="14" height="14" aria-hidden="true">
                                    <path fillRule="evenodd" d="M2 5a2 2 0 012-2h12a2 2 0 012 2v7a2 2 0 01-2 2H4a2 2 0 01-2-2V5zm14 0H4v7h12V5zM7 16a1 1 0 011-1h4a1 1 0 110 2H8a1 1 0 01-1-1z" clipRule="evenodd" />
                                </svg>
                                {textForLang(lang, 'Local', '本地', '本機')}
                            </button>
                            <button
                                className="prog-tools__mode-btn"
                                data-active={launchMode === 'remote'}
                                aria-pressed={launchMode === 'remote'}
                                onClick={() => setMode('remote')}
                            >
                                <svg viewBox="0 0 20 20" fill="currentColor" width="14" height="14" aria-hidden="true">
                                    <path fillRule="evenodd" d="M5.05 3.636a1 1 0 010 1.414 7 7 0 000 9.9 1 1 0 11-1.414 1.414 9 9 0 010-12.728 1 1 0 011.414 0zm9.9 0a9 9 0 010 12.728 1 1 0 11-1.414-1.414 7 7 0 000-9.9 1 1 0 011.414-1.414zM7.879 6.464a1 1 0 010 1.414 3 3 0 000 4.243 1 1 0 11-1.415 1.414 5 5 0 010-7.07 1 1 0 011.415 0zm4.242 0a5 5 0 010 7.072 1 1 0 01-1.415-1.415 3 3 0 000-4.242 1 1 0 011.415-1.415zM10 9a1 1 0 100 2 1 1 0 000-2z" clipRule="evenodd" />
                                </svg>
                                {textForLang(lang, 'Remote', '远程', '遠端')}
                            </button>
                        </div>
                    </div>
                </div>
            </section>

            {/* Section 2: Knowledge Base */}
            <section className="prog-tools__section" aria-labelledby={`${uid}-kb-heading`}>
                <h3 className="prog-tools__section-title" id={`${uid}-kb-heading`}>
                    <svg className="prog-tools__section-icon" viewBox="0 0 20 20" fill="currentColor" width="16" height="16" aria-hidden="true">
                        <path d="M9 4.804A7.968 7.968 0 005.5 4c-1.255 0-2.443.29-3.5.804v10A7.969 7.969 0 015.5 14c1.669 0 3.218.51 4.5 1.385A7.962 7.962 0 0114.5 14c1.255 0 2.443.29 3.5.804v-10A7.968 7.968 0 0014.5 4c-1.255 0-2.443.29-3.5.804V12a1 1 0 11-2 0V4.804z" />
                    </svg>
                    {textForLang(lang, 'Knowledge Base', '编程知识库', '程式知識庫')}
                </h3>
                <CodingKnowledgeSection config={config} setConfig={setConfig} lang={lang} versionRef={versionRef} />
            </section>
        </div>
    );
};


// ---------------------------------------------------------------------------
// Coding Knowledge Base Section
// ---------------------------------------------------------------------------

const SCOPE_TABS = ['all', 'universal', 'go', 'python', 'typescript', 'cpp', 'rust', 'java', 'project'] as const;
type ScopeTab = typeof SCOPE_TABS[number];

const STATUS_ICONS: Record<string, string> = {
    verified: '\u2705',
    active: '\u25CF',
    candidate: '\u25CB',
    deprecated: '\u26A0\uFE0F',
};

const TAB_LABELS: Record<ScopeTab, [string, string, string]> = {
    all: ['All', '全部', '全部'],
    universal: ['General', '通用', '通用'],
    go: ['Go', 'Go', 'Go'],
    python: ['Python', 'Python', 'Python'],
    typescript: ['TypeScript', 'TS', 'TS'],
    cpp: ['C++', 'C++', 'C++'],
    rust: ['Rust', 'Rust', 'Rust'],
    java: ['Java', 'Java', 'Java'],
    project: ['Project', '项目', '專案'],
};

function CodingKnowledgeSection({ config, setConfig, lang, versionRef }: ProgrammingToolsSettingsPanelProps & { versionRef: MutableRefObject<number> }) {
    const { showConfirm } = useDialog();
    const [stats, setStats] = useState<any>(null);
    const [experiences, setExperiences] = useState<any[]>([]);
    const [activeTab, setActiveTab] = useState<ScopeTab>('all');
    const [searchQuery, setSearchQuery] = useState('');
    const [loading, setLoading] = useState(false);
    const mountedRef = useRef(true);
    const uid = useId();

    const autoSaveMode = cfgVal(config, 'coding_knowledge_auto_save_mode', 'observe');
    const saveStrategy = cfgVal(config, 'coding_knowledge_save_strategy', 'on_retry_success');
    const patch = (p: Record<string, any>) => saveConfigPatch(config, setConfig, p, versionRef);

    // Debounce search by 350ms to avoid API flood on every keystroke
    const debouncedSearch = useDebouncedValue(searchQuery, 350);

    const loadStats = useCallback(async () => {
        try {
            const s = await CodingKnowledgeStats();
            if (mountedRef.current) setStats(s);
        } catch { /* ignore */ }
    }, []);

    const loadExperiences = useCallback(async () => {
        if (!mountedRef.current) return;
        setLoading(true);
        try {
            let results: any[];
            if (debouncedSearch.trim()) {
                results = await CodingKnowledgeSearch(debouncedSearch, 50);
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
            if (mountedRef.current) setExperiences(results || []);
        } catch {
            if (mountedRef.current) setExperiences([]);
        } finally {
            if (mountedRef.current) setLoading(false);
        }
    }, [activeTab, debouncedSearch]);

    useEffect(() => { return () => { mountedRef.current = false; }; }, []);
    useEffect(() => { void loadStats(); void loadExperiences(); }, [loadStats, loadExperiences]);

    const handleDelete = async (id: string) => {
        const confirmed = await showConfirm(
            textForLang(lang, 'Delete this experience? This cannot be undone.', '删除这条经验？此操作不可撤销。', '刪除這條經驗？此操作不可撤銷。'),
            textForLang(lang, 'Confirm Delete', '确认删除', '確認刪除'),
        );
        if (!confirmed) return;
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
        const confirmed = await showConfirm(
            textForLang(lang, 'Reset all coding experiences? This cannot be undone.', '清空所有编程经验？此操作不可撤销。', '清空所有程式經驗？此操作不可撤銷。'),
            textForLang(lang, 'Confirm Reset', '确认清空', '確認清空'),
        );
        if (!confirmed) return;
        try {
            await CodingKnowledgeResetFile();
            void loadStats();
            void loadExperiences();
        } catch (err) { console.error('reset knowledge:', err); }
    };

    const totalCount = stats?.total_count || 0;

    return (
        <div className="prog-tools__card prog-tools__kb">
            {/* Stats Banner — collapse to single line when empty */}
            {totalCount > 0 ? (
                <div className="prog-tools__kb-stats" role="status" aria-live="polite">
                    <div className="prog-tools__kb-stat">
                        <span className="prog-tools__kb-stat-value">{totalCount}</span>
                        <span className="prog-tools__kb-stat-label">{textForLang(lang, 'Total', '总计', '總計')}</span>
                    </div>
                    <div className="prog-tools__kb-stat" data-type="verified">
                        <span className="prog-tools__kb-stat-value">{stats?.verified_count || 0}</span>
                        <span className="prog-tools__kb-stat-label">{textForLang(lang, 'Verified', '已验证', '已驗證')}</span>
                    </div>
                    <div className="prog-tools__kb-stat" data-type="active">
                        <span className="prog-tools__kb-stat-value">{stats?.active_count || 0}</span>
                        <span className="prog-tools__kb-stat-label">{textForLang(lang, 'Active', '活跃', '活躍')}</span>
                    </div>
                    <div className="prog-tools__kb-stat" data-type="candidate">
                        <span className="prog-tools__kb-stat-value">{stats?.candidate_count || 0}</span>
                        <span className="prog-tools__kb-stat-label">{textForLang(lang, 'Candidate', '候选', '候選')}</span>
                    </div>
                </div>
            ) : (
                <div className="prog-tools__kb-stats-empty" role="status">
                    {textForLang(lang, 'Knowledge base is empty — experiences will be collected as you code.', '知识库为空，编程时将自动积累经验。', '知識庫為空，程式設計時將自動積累經驗。')}
                </div>
            )}

            {/* Config Row */}
            <div className="prog-tools__kb-config">
                <div className="prog-tools__kb-config-item">
                    <label className="prog-tools__kb-config-label" htmlFor={`${uid}-auto-save`}>
                        {textForLang(lang, 'Auto-save', '自动沉淀', '自動沉澱')}
                    </label>
                    <select
                        id={`${uid}-auto-save`}
                        className="prog-tools__select"
                        value={autoSaveMode}
                        onChange={(e) => patch({ coding_knowledge_auto_save_mode: e.target.value })}
                    >
                        <option value="observe">{textForLang(lang, 'Observe (candidate)', '观察（候选）', '觀察（候選）')}</option>
                        <option value="auto">{textForLang(lang, 'Auto (active)', '自动（生效）', '自動（生效）')}</option>
                        <option value="off">{textForLang(lang, 'Off', '关闭', '關閉')}</option>
                    </select>
                </div>
                <div className="prog-tools__kb-config-item">
                    <label className="prog-tools__kb-config-label" htmlFor={`${uid}-strategy`}>
                        {textForLang(lang, 'Strategy', '策略', '策略')}
                    </label>
                    <select
                        id={`${uid}-strategy`}
                        className="prog-tools__select"
                        value={saveStrategy}
                        onChange={(e) => patch({ coding_knowledge_save_strategy: e.target.value })}
                    >
                        <option value="on_retry_success">{textForLang(lang, 'On retry success', '重试成功时', '重試成功時')}</option>
                        <option value="on_success">{textForLang(lang, 'On success', '成功时', '成功時')}</option>
                        <option value="always">{textForLang(lang, 'Always', '始终', '始終')}</option>
                        <option value="off">{textForLang(lang, 'Off', '关闭', '關閉')}</option>
                    </select>
                </div>
                <button
                    className="prog-tools__btn-reset"
                    onClick={handleReset}
                    aria-label={textForLang(lang, 'Reset all knowledge', '重置所有知识', '重置所有知識')}
                    title={textForLang(lang, 'Reset all knowledge', '重置所有知识', '重置所有知識')}
                >
                    <svg viewBox="0 0 20 20" fill="currentColor" width="14" height="14" aria-hidden="true">
                        <path fillRule="evenodd" d="M4 2a1 1 0 011 1v2.101a7.002 7.002 0 0111.601 2.566 1 1 0 11-1.885.666A5.002 5.002 0 005.999 7H9a1 1 0 010 2H4a1 1 0 01-1-1V3a1 1 0 011-1zm.008 9.057a1 1 0 011.276.61A5.002 5.002 0 0014.001 13H11a1 1 0 110-2h5a1 1 0 011 1v5a1 1 0 11-2 0v-2.101a7.002 7.002 0 01-11.601-2.566 1 1 0 01.61-1.276z" clipRule="evenodd" />
                    </svg>
                </button>
            </div>

            {/* Scope Tabs + Search */}
            <div className="prog-tools__kb-toolbar">
                <div className="prog-tools__kb-tabs" role="tablist" aria-label={textForLang(lang, 'Filter by scope', '按范围筛选', '按範圍篩選')}>
                    {SCOPE_TABS.map((tab) => (
                        <button
                            key={tab}
                            role="tab"
                            className="prog-tools__kb-tab"
                            data-active={activeTab === tab}
                            aria-selected={activeTab === tab}
                            tabIndex={activeTab === tab ? 0 : -1}
                            onClick={() => { setActiveTab(tab); setSearchQuery(''); }}
                        >
                            {textForLang(lang, TAB_LABELS[tab][0], TAB_LABELS[tab][1], TAB_LABELS[tab][2])}
                        </button>
                    ))}
                </div>
                <div className="prog-tools__kb-search">
                    <svg className="prog-tools__kb-search-icon" viewBox="0 0 20 20" fill="currentColor" width="14" height="14" aria-hidden="true">
                        <path fillRule="evenodd" d="M8 4a4 4 0 100 8 4 4 0 000-8zM2 8a6 6 0 1110.89 3.476l4.817 4.817a1 1 0 01-1.414 1.414l-4.816-4.816A6 6 0 012 8z" clipRule="evenodd" />
                    </svg>
                    <input
                        type="search"
                        placeholder={textForLang(lang, 'Search...', '搜索...', '搜尋...')}
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                        aria-label={textForLang(lang, 'Search knowledge base', '搜索知识库', '搜尋知識庫')}
                    />
                </div>
            </div>

            {/* Experience List */}
            <div className="prog-tools__kb-list" role="list" aria-busy={loading}>
                {loading && experiences.length === 0 ? (
                    <div className="prog-tools__kb-empty">
                        <span>{textForLang(lang, 'Loading...', '加载中...', '載入中...')}</span>
                    </div>
                ) : experiences.length === 0 ? (
                    <div className="prog-tools__kb-empty">
                        <svg viewBox="0 0 20 20" fill="currentColor" width="28" height="28" opacity={0.3} aria-hidden="true">
                            <path d="M9 4.804A7.968 7.968 0 005.5 4c-1.255 0-2.443.29-3.5.804v10A7.969 7.969 0 015.5 14c1.669 0 3.218.51 4.5 1.385A7.962 7.962 0 0114.5 14c1.255 0 2.443.29 3.5.804v-10A7.968 7.968 0 0014.5 4c-1.255 0-2.443.29-3.5.804V12a1 1 0 11-2 0V4.804z" />
                        </svg>
                        <span>{debouncedSearch.trim()
                            ? textForLang(lang, 'No results found', '未找到匹配结果', '未找到匹配結果')
                            : textForLang(lang, 'No experiences yet', '暂无经验记录', '暫無經驗記錄')
                        }</span>
                    </div>
                ) : (
                    experiences.map((exp) => (
                        <div key={exp.id} className="prog-tools__kb-item" data-status={exp.status} role="listitem">
                            <div className="prog-tools__kb-item-main">
                                <span className="prog-tools__kb-item-status" aria-hidden="true">{STATUS_ICONS[exp.status] || ''}</span>
                                <span className="prog-tools__kb-item-title">{exp.title}</span>
                            </div>
                            <div className="prog-tools__kb-item-meta">
                                {exp.category && <span className="prog-tools__kb-tag">{exp.category}</span>}
                                {exp.scope && <span className="prog-tools__kb-tag">{exp.scope}{exp.language ? `:${exp.language}` : ''}</span>}
                                {exp.recall_count > 0 && <span className="prog-tools__kb-tag prog-tools__kb-tag--count">{exp.recall_count}×</span>}
                            </div>
                            <div className="prog-tools__kb-item-actions">
                                {exp.status === 'candidate' && (
                                    <button className="prog-tools__kb-btn prog-tools__kb-btn--confirm" onClick={() => void handleConfirm(exp.id)}>
                                        {textForLang(lang, 'Confirm', '确认', '確認')}
                                    </button>
                                )}
                                <button className="prog-tools__kb-btn prog-tools__kb-btn--delete" onClick={() => void handleDelete(exp.id)}>
                                    {textForLang(lang, 'Delete', '删除', '刪除')}
                                </button>
                            </div>
                        </div>
                    ))
                )}
            </div>
        </div>
    );
}
