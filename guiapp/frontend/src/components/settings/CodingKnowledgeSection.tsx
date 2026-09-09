import { Dispatch, MutableRefObject, SetStateAction, useCallback, useEffect, useId, useRef, useState } from 'react';
import { CodingKnowledgeCapacity, CodingKnowledgeConfirm, CodingKnowledgeContributeToOrg, CodingKnowledgeCreateRevisionCandidate, CodingKnowledgeDelete, CodingKnowledgeEvict, CodingKnowledgeExportToFile, CodingKnowledgeGet, CodingKnowledgeGraduateToSteering, CodingKnowledgeImportFromFile, CodingKnowledgeLifecycle, CodingKnowledgeList, CodingKnowledgeMarkConflict, CodingKnowledgeResetFile, CodingKnowledgeSearch, CodingKnowledgeStats, CodingKnowledgeUpdate, DigitalAssetListContributableLibraries, DigitalAssetListMySubmissions, SelectCodingKnowledgeExportPath, SelectCodingKnowledgeImportFile } from '../../../wailsjs/go/main/App';
import { corelib, knowledge } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { useDialog } from '../CustomDialog';
import { cfgVal, saveConfigPatch } from './programmingToolsConfig';
import { CodingKnowledgeAuditDialog, CodingKnowledgeEditorDialog } from './CodingKnowledgeDialogs';
import { SCOPE_TABS, TAB_LABELS, toDraft, useDebouncedValue, type ExperienceDraft, type ScopeTab } from './codingKnowledgeHelpers';

type Props = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
    versionRef: MutableRefObject<number>;
};

const textForLang = localizeText;

export function CodingKnowledgeSection({ config, setConfig, lang, versionRef }: Props) {
    const { showConfirm, showPrompt } = useDialog();
    const [stats, setStats] = useState<any>(null);
    const [capacity, setCapacity] = useState<any>(null);
    const [experiences, setExperiences] = useState<any[]>([]);
    const [activeTab, setActiveTab] = useState<ScopeTab>('all');
    const [searchQuery, setSearchQuery] = useState('');
    const [loading, setLoading] = useState(false);
    const [editorOpen, setEditorOpen] = useState(false);
    const [editorSaving, setEditorSaving] = useState(false);
    const [draft, setDraft] = useState<ExperienceDraft | null>(null);
    const [auditExperience, setAuditExperience] = useState<any | null>(null);
    const [auditEvents, setAuditEvents] = useState<knowledge.CodingExperienceLifecycleEvent[]>([]);
    const [actionMessage, setActionMessage] = useState('');
    const [selectedIDs, setSelectedIDs] = useState<string[]>([]);
    const [techLibs, setTechLibs] = useState<Array<{ id?: string; name?: string }>>([]);
    const [contributeLibraryID, setContributeLibraryID] = useState('');
    const [contributeSummary, setContributeSummary] = useState('');
    const [mySubmissions, setMySubmissions] = useState<any[]>([]);
    const mountedRef = useRef(true);
    const uid = useId();
    const autoSaveMode = cfgVal(config, 'coding_knowledge_auto_save_mode', 'observe');
    const saveStrategy = cfgVal(config, 'coding_knowledge_save_strategy', 'on_retry_success');
    const maxTotal = cfgVal(config, 'coding_knowledge_max_total', 1000);
    const maxPerProject = cfgVal(config, 'coding_knowledge_max_per_project', 200);
	const maxReviewedPerProject = cfgVal(config, 'coding_knowledge_max_reviewed_per_project', 100);
	const maxReviewedTokensPerProject = cfgVal(config, 'coding_knowledge_max_reviewed_tokens_per_project', 30000);
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
    useEffect(() => {
        void DigitalAssetListContributableLibraries('technical').then(items => {
            if (!mountedRef.current) return;
            setTechLibs(items || []);
            if (!contributeLibraryID && items?.length) setContributeLibraryID(String(items[0].id || ''));
        }).catch(() => undefined);
        void DigitalAssetListMySubmissions().then(items => {
            if (mountedRef.current) setMySubmissions(items || []);
        }).catch(() => undefined);
    }, []);

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

    const handleMarkConflict = async (id: string) => {
        const reason = await showPrompt(
            textForLang(lang, 'Why should this experience be retired? This reason is retained in its limited audit history.', '请说明为何要退役该经验。此原因会保留在其有限审计历史中。', '請說明為何要退役該經驗。此原因會保留在其有限稽核歷史中。'),
            textForLang(lang, 'Mark conflict', '标记冲突', '標記衝突'),
            { placeholder: textForLang(lang, 'Conflict reason', '冲突原因', '衝突原因') },
        );
        if (!reason?.trim()) return;
        try {
            await CodingKnowledgeMarkConflict(id, '', reason.trim());
            setActionMessage(textForLang(lang, 'Experience retired as conflicted.', '经验已因冲突退役。', '經驗已因衝突退役。'));
            void loadStats();
            void loadExperiences();
        } catch (err) { console.error('mark experience conflict:', err); }
    };

    const handleCreateRevision = async (id: string) => {
        const reason = await showPrompt(
            textForLang(lang, 'Why is a revised candidate needed? The retired experience will remain unchanged.', '请说明为什么需要修订候选项。已退役经验将保持不变。', '請說明為什麼需要修訂候選項。已退役經驗將保持不變。'),
            textForLang(lang, 'Create revision', '创建修订', '建立修訂'),
            { placeholder: textForLang(lang, 'Revision reason', '修订原因', '修訂原因') },
        );
        if (!reason?.trim()) return;
        try {
            const candidate = await CodingKnowledgeCreateRevisionCandidate(id, reason.trim());
            setActionMessage(textForLang(lang, `Created revision candidate: ${candidate.title || candidate.id}.`, `已创建修订候选项：${candidate.title || candidate.id}。`, `已建立修訂候選項：${candidate.title || candidate.id}。`));
            void loadStats();
            void loadExperiences();
        } catch (err) { console.error('create experience revision:', err); }
    };

    const openAudit = async (exp: any) => {
        try {
            const events = await CodingKnowledgeLifecycle(exp.id);
            setAuditExperience(exp);
            setAuditEvents(events || []);
        } catch (err) { console.error('load experience lifecycle:', err); }
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
            await CodingKnowledgeUpdate(draft as knowledge.CodingExperience);
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

    const handleContribute = async () => {
        if (!selectedIDs.length) {
            setActionMessage(textForLang(lang, 'Select active or verified experiences first.', '请先勾选已确认或已审核的编码经验。', '請先勾選已確認或已審核的編碼經驗。'));
            return;
        }
        const summary = contributeSummary.trim();
        if (!summary) {
            setActionMessage(textForLang(lang, 'Write why this helps the organization.', '请写明这条经验对组织的价值。', '請寫明這條經驗對組織的價值。'));
            return;
        }
        try {
            const libs = await DigitalAssetListContributableLibraries('technical');
            setTechLibs(libs || []);
            const libraryID = contributeLibraryID || String(libs?.[0]?.id || '');
            if (!libraryID) {
                setActionMessage(textForLang(lang, 'No technical digital-asset library accepts submissions.', '没有可投稿的技术数字资产库。', '沒有可投稿的技術數字資產庫。'));
                return;
            }
            if (!contributeLibraryID) setContributeLibraryID(libraryID);
            const result = await CodingKnowledgeContributeToOrg({
                library_id: libraryID,
                title: '',
                summary,
                experience_ids: selectedIDs,
            } as any);
            setActionMessage(textForLang(lang, `Submitted ${result.item_count || selectedIDs.length} experience(s) for review.`, `已投稿 ${result.item_count || selectedIDs.length} 条经验，等待审批。`, `已投稿 ${result.item_count || selectedIDs.length} 條經驗，等待審批。`));
            const items = await DigitalAssetListMySubmissions();
            if (mountedRef.current) setMySubmissions(items || []);
        } catch (err: any) {
            setActionMessage(String(err?.message || err || textForLang(lang, 'Contribute failed.', '投稿失败。', '投稿失敗。')));
        }
    };

    const toggleSelected = (id: string, status: string) => {
        if (status !== 'active' && status !== 'verified') return;
        setSelectedIDs(prev => prev.includes(id) ? prev.filter(x => x !== id) : [...prev, id]);
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
                        <option value="auto">{textForLang(lang, 'Auto (review required)', '自动（需审核）', '自動（需審核）')}</option>
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
				<div className="prog-tools__kb-config-item">
					<label className="prog-tools__kb-config-label" htmlFor={`${uid}-max-reviewed-project`}>{textForLang(lang, 'Reviewed / project', '每项目已审核上限', '每專案已審核上限')}</label>
					<input
						id={`${uid}-max-reviewed-project`}
						type="number"
						min="1"
						className="prog-tools__select"
						value={maxReviewedPerProject}
						onChange={(e) => patch({ coding_knowledge_max_reviewed_per_project: Number(e.target.value) || 0 })}
					/>
				</div>
				<div className="prog-tools__kb-config-item">
					<label className="prog-tools__kb-config-label" htmlFor={`${uid}-max-reviewed-tokens`}>{textForLang(lang, 'Reviewed tokens / project', '每项目已审核 token 上限', '每專案已審核 token 上限')}</label>
					<input
						id={`${uid}-max-reviewed-tokens`}
						type="number"
						min="1"
						step="100"
						className="prog-tools__select"
						value={maxReviewedTokensPerProject}
						onChange={(e) => patch({ coding_knowledge_max_reviewed_tokens_per_project: Number(e.target.value) || 0 })}
					/>
				</div>
                <button type="button" className="prog-tools__kb-btn" onClick={() => void handleExport()}>{textForLang(lang, 'Export', '导出', '匯出')}</button>
                <button type="button" className="prog-tools__kb-btn" onClick={() => void handleImport()}>{textForLang(lang, 'Import', '导入', '匯入')}</button>
                <button type="button" className="prog-tools__kb-btn" onClick={() => void handleContribute()}>{textForLang(lang, 'Submit to organization', '投稿到组织', '投稿到組織')}</button>
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

            <div className="prog-tools__kb-config" style={{ marginTop: 8 }}>
                <select className="prog-tools__select" value={contributeLibraryID} onChange={(e) => setContributeLibraryID(e.target.value)} aria-label={textForLang(lang, 'Target technical library', '目标技术库', '目標技術庫')}>
                    <option value="">{textForLang(lang, 'Target technical library', '目标技术库', '目標技術庫')}</option>
                    {techLibs.map(lib => <option key={lib.id} value={lib.id}>{lib.name || lib.id}</option>)}
                </select>
                <input className="prog-tools__select" value={contributeSummary} onChange={(e) => setContributeSummary(e.target.value)} placeholder={textForLang(lang, 'Why this helps the organization', '为什么这对组织有用', '為什麼這對組織有用')} />
            </div>
            {actionMessage && <div className="prog-tools__kb-action-msg" role="status">{actionMessage}</div>}
            {mySubmissions.length ? (
                <div className="prog-tools__kb-action-msg" role="status">
                    {textForLang(lang, 'My contributions', '我的投稿', '我的投稿')}: {mySubmissions.slice(0, 5).map(item => `${item.title || item.id} (${item.status})`).join(' · ')}
                </div>
            ) : null}

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
                                {(exp.status === 'active' || exp.status === 'verified') ? (
                                    <input type="checkbox" checked={selectedIDs.includes(exp.id)} onChange={() => toggleSelected(exp.id, exp.status)} aria-label={textForLang(lang, 'Select for organization', '选择投稿', '選擇投稿')} />
                                ) : null}
                                <span className="prog-tools__kb-item-title">{exp.title}</span>
                            </div>
                            <div className="prog-tools__kb-item-meta">
                                {exp.category && <span className="prog-tools__kb-tag">{exp.category}</span>}
                                {exp.scope && <span className="prog-tools__kb-tag">{exp.scope}{exp.language ? `:${exp.language}` : ''}</span>}
                                {exp.recall_count > 0 && <span className="prog-tools__kb-tag prog-tools__kb-tag--count">{exp.recall_count}×</span>}
                            </div>
                            <div className="prog-tools__kb-item-actions">
                                <button type="button" className="prog-tools__kb-btn" onClick={() => void openEditor(exp.id)}>{textForLang(lang, 'Edit', '编辑', '編輯')}</button>
                                <button type="button" className="prog-tools__kb-btn" onClick={() => void openAudit(exp)}>{textForLang(lang, 'Audit', '审计', '稽核')}</button>
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
                                {exp.status !== 'deprecated' && (
                                    <button type="button" className="prog-tools__kb-btn" onClick={() => void handleMarkConflict(exp.id)}>
                                        {textForLang(lang, 'Mark conflict', '标记冲突', '標記衝突')}
                                    </button>
                                )}
                                {exp.status === 'deprecated' && (
                                    <button type="button" className="prog-tools__kb-btn prog-tools__kb-btn--confirm" onClick={() => void handleCreateRevision(exp.id)}>
                                        {textForLang(lang, 'Create revision', '创建修订', '建立修訂')}
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
                <CodingKnowledgeEditorDialog
                    lang={lang}
                    draft={draft}
                    editorSaving={editorSaving}
                    onChange={setDraft}
                    onCancel={() => { setEditorOpen(false); setDraft(null); }}
                    onSave={() => { void handleSaveDraft(); }}
                />
            )}

            {auditExperience && (
                <CodingKnowledgeAuditDialog
                    lang={lang}
                    experience={auditExperience}
                    events={auditEvents}
                    onClose={() => { setAuditExperience(null); setAuditEvents([]); }}
                />
            )}
        </div>
    );
}
