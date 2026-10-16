import { localizeText } from '../../i18n';

type Props = {
    lang: string;
    stats: any;
    capacityMax: number;
    autoSaveMode: string;
};

/** Knowledge-base counters plus the "candidates awaiting review" hint. */
export function CodingKnowledgeStatsBar({ lang, stats, capacityMax, autoSaveMode }: Props) {
    const totalCount = stats?.total_count || 0;
    return (
        <>
            {totalCount > 0 ? (
                <div className="prog-tools__kb-stats" role="status" aria-live="polite">
                    <div className="prog-tools__kb-stat">
                        <span className="prog-tools__kb-stat-value">{totalCount}</span>
                        <span className="prog-tools__kb-stat-label">{localizeText(lang, 'Total', '总计', '總計')}</span>
                    </div>
                    <div className="prog-tools__kb-stat">
                        <span className="prog-tools__kb-stat-value">/{capacityMax}</span>
                        <span className="prog-tools__kb-stat-label">{localizeText(lang, 'Capacity', '容量', '容量')}</span>
                    </div>
                    <div className="prog-tools__kb-stat" data-type="verified">
                        <span className="prog-tools__kb-stat-value">{stats?.verified_count || 0}</span>
                        <span className="prog-tools__kb-stat-label">{localizeText(lang, 'Verified', '已验证', '已驗證')}</span>
                    </div>
                    <div className="prog-tools__kb-stat" data-type="active">
                        <span className="prog-tools__kb-stat-value">{stats?.active_count || 0}</span>
                        <span className="prog-tools__kb-stat-label">{localizeText(lang, 'Active', '活跃', '活躍')}</span>
                    </div>
                    <div className="prog-tools__kb-stat" data-type="candidate">
                        <span className="prog-tools__kb-stat-value">{stats?.candidate_count || 0}</span>
                        <span className="prog-tools__kb-stat-label">{localizeText(lang, 'Candidate', '候选', '候選')}</span>
                    </div>
                </div>
            ) : (
                <div className="prog-tools__kb-stats-empty" role="status">
                    {localizeText(lang, 'Knowledge base is empty — experiences will be collected as you code.', '知识库为空，编程时将自动积累经验。', '知識庫為空，程式設計時將自動積累經驗。')}
                </div>
            )}
            {autoSaveMode === 'auto' && (stats?.candidate_count || 0) > 0 ? (
                <div className="prog-tools__kb-action-msg" role="status">
                    {localizeText(
                        lang,
                        `${stats.candidate_count} candidate(s) awaiting review.`,
                        `${stats.candidate_count} 条候选经验待审核。`,
                        `${stats.candidate_count} 條候選經驗待審核。`,
                    )}
                </div>
            ) : null}
        </>
    );
}
