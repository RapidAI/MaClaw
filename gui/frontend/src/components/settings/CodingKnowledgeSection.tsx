import type { Dispatch, MutableRefObject, SetStateAction } from 'react';
import { useState, useEffect, useCallback, useRef, useId } from 'react';
import {
    CodingKnowledgeStats,
    CodingKnowledgeList,
    CodingKnowledgeGet,
    CodingKnowledgeUpdate,
    CodingKnowledgeDelete,
    CodingKnowledgeConfirm,
    CodingKnowledgeResetFile,
    CodingKnowledgeSearch,
    CodingKnowledgeExportToFile,
    CodingKnowledgeImportFromFile,
    CodingKnowledgeGraduateToSteering,
    CodingKnowledgeCapacity,
    CodingKnowledgeEvict,
    SelectCodingKnowledgeExportPath,
    SelectCodingKnowledgeImportFile,
} from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { useDialog } from '../CustomDialog';
import { cfgVal, saveConfigPatch } from './programmingToolsConfig';

type Props = {
    config: main.AppConfig | null;
    setConfig: Dispatch<SetStateAction<main.AppConfig | null>>;
    lang: string;
    versionRef: MutableRefObject<number>;
};

const textForLang = localizeText;
const SCOPE_TABS = ['all', 'universal', 'go', 'python', 'typescript', 'cpp', 'rust', 'java', 'project'] as const;
type ScopeTab = typeof SCOPE_TABS[number];
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

function useDebouncedValue(value: string, delayMs: number): string {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        const timer = setTimeout(() => setDebounced(value), delayMs);
        return () => clearTimeout(timer);
    }, [value, delayMs]);
    return debounced;
}

type ExperienceDraft = {
    id: string;
    title: string;
    category: string;
    scope: string;
    language: string;
    trigger_condition: string;
    content: string;
    code_snippet: string;
    status: string;
    confidence: number;
    recall_count: number;
    success_count: number;
    failure_count: number;
};

function toDraft(raw: any): ExperienceDraft {
    return {
        id: String(raw?.id || ''),
        title: String(raw?.title || ''),
        category: String(raw?.category || 'pattern'),
        scope: String(raw?.scope || 'universal'),
        language: String(raw?.language || ''),
        trigger_condition: String(raw?.trigger_condition || ''),
        content: String(raw?.content || ''),
        code_snippet: String(raw?.code_snippet || ''),
        status: String(raw?.status || 'candidate'),
        confidence: Number(raw?.confidence || 0),
        recall_count: Number(raw?.recall_count || 0),
        success_count: Number(raw?.success_count || 0),
        failure_count: Number(raw?.failure_count || 0),
    };
}

export function CodingKnowledgeSection({ config, setConfig, lang, versionRef }: Props) {
    const { showConfirm } = useDialog();
    const [stats, setStats] = useState<any>(null);
    const [capacity, setCapacity] = useState<any>(null);
    const [experiences, setExperiences] = useState<any[]>([]);
    const [activeTab, setActiveTab] = useState<ScopeTab>('all');
    const [searchQuery, setSearchQuery] = useState('');
    const [loading, setLoading] = useState(false);
    const [editorOpen, setEditorOpen] = useState(false);
    const [editorSaving, setEditorSaving] = useState(false);
    const [draft, setDraft] = useState<ExperienceDraft | null>(null);
    const [actionMessage, setActionMessage] = useState('');
    const mountedRef = useRef(true);
    const uid = useId();
    const autoSaveMode = cfgVal(config, 'coding_knowledge_auto_save_mode', 'observe');
    const saveStrategy = cfgVal(config, 'coding_knowledge_save_strategy', 'on_retry_success');
    const maxTotal = cfgVal(config, 'coding_knowledge_max_total', 1000);
    const maxPerProject = cfgVal(config, 'coding_knowledge_max_per_project', 200);
    const patch = (p: Record<string, any>) => saveConfigPatch(config, setConfig, p, versionRef);
    const debouncedSearch = useDebouncedValue(searchQuery, 350);

    const loadStats = useCallback(async () => {
        try {
            const [s, cap] = await Promise.all([CodingKnowledgeStats(), CodingKnowledgeCapacity()]);
            if (mountedRef.current) {
                setStats(s);
                setCapacity(cap);
            }
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
                    if (activeTab === 'universal' || activeTab === 'project') filter.scope = activeTab;
                    else {
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

    useEffect(() => () => { mountedRef.current = false; }, []);
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

    const openEditor = async (id: string) => {
        try {
            const raw = await CodingKnowledgeGet(id);
            setDraft(toDraft(raw));
            setEditorOpen(true);
            setActionMessage('');
        } catch (err) { console.error('get experience:', err); }
    };

    const handleSaveDraft = async () => {
        if (!draft) return;
        setEditorSaving(true);
        try {
            await CodingKnowledgeUpdate(draft);
            setActionMessage(textForLang(lang, 'Experience saved.', '经验已保存。', '經驗已保存。'));
            setEditorOpen(false);
            setDraft(null);
            void loadStats();
            void loadExperiences();
        } catch (err) {
            console.error('save experience:', err);
        } finally {
            setEditorSaving(false);
        }
    };

    const handleExport = async () => {
        try {
            const path = await SelectCodingKnowledgeExportPath();
            if (!path) return;
            await CodingKnowledgeExportToFile(path);
            setActionMessage(textForLang(lang, 'Exported experiences.', '已导出经验。', '已匯出經驗。'));
        } catch (err) { console.error('export knowledge:', err); }
    };

    const handleImport = async () => {
        try {
            const path = await SelectCodingKnowledgeImportFile();
            if (!path) return;
            const count = await CodingKnowledgeImportFromFile(path);
            setActionMessage(textForLang(lang, `Imported ${count} experiences.`, `已导入 ${count} 条经验。`, `已匯入 ${count} 條經驗。`));
            void loadStats();
            void loadExperiences();
        } catch (err) { console.error('import knowledge:', err); }
    };

    const handleGraduate = async (id: string) => {
        try {
            await CodingKnowledgeGraduateToSteering(id);
            void loadStats();
            void loadExperiences();
        } catch (err) { console.error('graduate experience:', err); }
    };

    const handleEvict = async () => {
        try {
            const n = await CodingKnowledgeEvict();
            setActionMessage(textForLang(lang, `Evicted ${n} experiences.`, `已淘汰 ${n} 条经验。`, `已淘汰 ${n} 條經驗。`));
            void loadStats();
            void loadExperiences();
        } catch (err) { console.error('evict knowledge:', err); }
    };

    const totalCount = stats?.total_count || 0;
    const capacityMax = capacity?.max_total ?? maxTotal;

    return (
        <div className="prog-tools__card prog-tools__kb">
            {totalCount > 0 ? (
                <div className="prog-tools__kb-stats" role="status" aria-live="polite">
                    <div className="prog-tools__kb-stat">
                        <span className="prog-tools__kb-stat-value">{totalCount}</span>
                        <span className="prog-tools__kb-stat-label">{textForLang(lang, 'Total', '总计', '總計')}</span>
                    </div>
                    <div className="prog-tools__kb-stat">
                        <span className="prog-tools__kb-stat-value">/{capacityMax}</span>
                        <span className="prog-tools__kb-stat-label">{textForLang(lang, 'Capacity', '容量', '容量')}</span>
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

            <div className="prog-tools__kb-config">
                <div className="prog-tools__kb-config-item">
                    <label className="prog-tools__kb-config-label" htmlFor={`${uid}-auto-save`}>{textForLang(lang, 'Auto-save', '自动沉淀', '自動沉澱')}</label>
                    <select id={`${uid}-auto-save`} className="prog-tools__select" value={autoSaveMode} onChange={(e) => patch({ coding_knowledge_auto_save_mode: e.target.value })}>
                        <option value="observe">{textForLang(lang, 'Observe (candidate)', '观察（候选）', '觀察（候選）')}</option>
                        <option value="auto">{textForLang(lang, 'Auto (active)', '自动（生效）', '自動（生效）')}</option>
                        <option value="off">{textForLang(lang, 'Off', '关闭', '關閉')}</option>
                    </select>
                </div>
                <div className="prog-tools__kb-config-item">
                    <label className="prog-tools__kb-config-label" htmlFor={`${uid}-strategy`}>{textForLang(lang, 'Strategy', '策略', '策略')}</label>
                    <select id={`${uid}-strategy`} className="prog-tools__select" value={saveStrategy} onChange={(e) => patch({ coding_knowledge_save_strategy: e.target.value })}>
                        <option value="on_retry_success">{textForLang(lang, 'On retry success', '重试成功时', '重試成功時')}</option>
                        <option value="on_success">{textForLang(lang, 'On success', '成功时', '成功時')}</option>
                        <option value="always">{textForLang(lang, 'Always', '始终', '始終')}</option>
                        <option value="off">{textForLang(lang, 'Off', '关闭', '關閉')}</option>
                    </select>
                </div>
                <div className="prog-tools__kb-config-item">
                    <label className="prog-tools__kb-config-label" htmlFor={`${uid}-max-total`}>{textForLang(lang, 'Max total', '总数上限', '總數上限')}</label>
                    <input
                        id={`${uid}-max-total`}
                        type="number"
                        className="prog-tools__select"
                        aria-label={textForLang(lang, 'Max total', '总数上限', '總數上限')}
                        value={maxTotal}
                        onChange={(e) => patch({ coding_knowledge_max_total: Number(e.target.value) || 0 })}
                    />
                </div>
                <div className="prog-tools__kb-config-item">
                    <label className="prog-tools__kb-config-label" htmlFor={`${uid}-max-project`}>{textForLang(lang, 'Max per project', '单项目上限', '單專案上限')}</label>
                    <input
                        id={`${uid}-max-project`}
                        type="number"
                        className="prog-tools__select"
                        value={maxPerProject}
                        onChange={(e) => patch({ coding_knowledge_max_per_project: Number(e.target.value) || 0 })}
                    />
                </div>
                <button type="button" className="prog-tools__kb-btn" onClick={() => void handleExport()}>{textForLang(lang, 'Export', '导出', '匯出')}</button>
                <button type="button" className="prog-tools__kb-btn" onClick={() => void handleImport()}>{textForLang(lang, 'Import', '导入', '匯入')}</button>
                <button type="button" className="prog-tools__kb-btn" onClick={() => void handleEvict()}>{textForLang(lang, 'Run eviction', '执行淘汰', '執行淘汰')}</button>
                <button
                    className="prog-tools__btn-reset prog-tools__btn-reset--danger"
                    onClick={() => void handleReset()}
                    aria-label={textForLang(lang, 'Clear all knowledge', '清空所有知识', '清空所有知識')}
                    title={textForLang(lang, 'Clear all knowledge', '清空所有知识', '清空所有知識')}
                >
                    {textForLang(lang, 'Clear', '清空', '清空')}
                </button>
            </div>

            {actionMessage && <div className="prog-tools__kb-action-msg" role="status">{actionMessage}</div>}

            <div className="prog-tools__kb-toolbar">
                <div className="prog-tools__kb-tabs" role="tablist" aria-label={textForLang(lang, 'Filter by scope', '按范围筛选', '按範圍篩選')}>
                    {SCOPE_TABS.map((tab) => (
                        <button key={tab} role="tab" className="prog-tools__kb-tab" data-active={activeTab === tab} aria-selected={activeTab === tab} onClick={() => { setActiveTab(tab); setSearchQuery(''); }}>
                            {textForLang(lang, TAB_LABELS[tab][0], TAB_LABELS[tab][1], TAB_LABELS[tab][2])}
                        </button>
                    ))}
                </div>
                <div className="prog-tools__kb-search">
                    <input
                        type="search"
                        placeholder={textForLang(lang, 'Search...', '搜索...', '搜尋...')}
                        value={searchQuery}
                        onChange={(e) => setSearchQuery(e.target.value)}
                        aria-label={textForLang(lang, 'Search knowledge base', '搜索知识库', '搜尋知識庫')}
                    />
                </div>
            </div>

            <div className="prog-tools__kb-list" role="list" aria-busy={loading}>
                {loading && experiences.length === 0 ? (
                    <div className="prog-tools__kb-empty"><span>{textForLang(lang, 'Loading...', '加载中...', '載入中...')}</span></div>
                ) : experiences.length === 0 ? (
                    <div className="prog-tools__kb-empty">
                        <span>{debouncedSearch.trim()
                            ? textForLang(lang, 'No results found', '未找到匹配结果', '未找到匹配結果')
                            : textForLang(lang, 'No experiences yet', '暂无经验记录', '暫無經驗記錄')}</span>
                    </div>
                ) : (
                    experiences.map((exp) => (
                        <div key={exp.id} className="prog-tools__kb-item" data-status={exp.status} role="listitem">
                            <div className="prog-tools__kb-item-main">
                                <span className="prog-tools__kb-item-title">{exp.title}</span>
                            </div>
                            <div className="prog-tools__kb-item-meta">
                                {exp.category && <span className="prog-tools__kb-tag">{exp.category}</span>}
                                {exp.scope && <span className="prog-tools__kb-tag">{exp.scope}{exp.language ? `:${exp.language}` : ''}</span>}
                                {exp.recall_count > 0 && <span className="prog-tools__kb-tag prog-tools__kb-tag--count">{exp.recall_count}×</span>}
                            </div>
                            <div className="prog-tools__kb-item-actions">
                                <button type="button" className="prog-tools__kb-btn" onClick={() => void openEditor(exp.id)}>{textForLang(lang, 'Edit', '编辑', '編輯')}</button>
                                {exp.status === 'candidate' && (
                                    <button type="button" className="prog-tools__kb-btn prog-tools__kb-btn--confirm" onClick={() => void handleConfirm(exp.id)}>
                                        {textForLang(lang, 'Confirm', '确认', '確認')}
                                    </button>
                                )}
                                {exp.status === 'verified' && (
                                    <button type="button" className="prog-tools__kb-btn" onClick={() => void handleGraduate(exp.id)}>
                                        {textForLang(lang, 'Graduate', '毕业', '畢業')}
                                    </button>
                                )}
                                <button type="button" className="prog-tools__kb-btn prog-tools__kb-btn--delete" onClick={() => void handleDelete(exp.id)}>
                                    {textForLang(lang, 'Delete', '删除', '刪除')}
                                </button>
                            </div>
                        </div>
                    ))
                )}
            </div>

            {editorOpen && draft && (
                <div className="prog-tools__kb-editor" role="dialog" aria-label={textForLang(lang, 'Edit Experience', '编辑经验', '編輯經驗')}>
                    <h4>{textForLang(lang, 'Edit Experience', '编辑经验', '編輯經驗')}</h4>
                    <label>
                        {textForLang(lang, 'Title', '标题', '標題')}
                        <input value={draft.title} onChange={(e) => setDraft({ ...draft, title: e.target.value })} />
                    </label>
                    <label>
                        {textForLang(lang, 'Content', '内容', '內容')}
                        <textarea value={draft.content} onChange={(e) => setDraft({ ...draft, content: e.target.value })} />
                    </label>
                    <div className="prog-tools__kb-editor-actions">
                        <button type="button" className="prog-tools__kb-btn" onClick={() => { setEditorOpen(false); setDraft(null); }} disabled={editorSaving}>
                            {textForLang(lang, 'Cancel', '取消', '取消')}
                        </button>
                        <button type="button" className="prog-tools__kb-btn prog-tools__kb-btn--confirm" onClick={() => void handleSaveDraft()} disabled={editorSaving}>
                            {textForLang(lang, 'Save', '保存', '保存')}
                        </button>
                    </div>
                </div>
            )}
        </div>
    );
}
