import { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { GossipSnapshot, GossipPublish, GossipComment, GossipRate, GossipGetComments } from '../../../wailsjs/go/main/App';
import { colors, radius, remoteCardStyle, remoteEmptyStateStyle, remoteLoadingStateStyle } from '../remote/styles';

interface GossipPost {
    id: string;
    nickname: string;
    content: string;
    category: string;
    score: number;
    votes: number;
    locked: boolean;
    created_at: string;
}

interface GossipCommentData {
    id: string;
    nickname: string;
    content: string;
    rating: number;
    created_at: string;
}

interface GossipPanelProps {
    lang: string;
}

type SortMode = 'newest' | 'hottest' | 'score';

const PAGE_SIZE = 10;
const POLL_INTERVAL = 30_000; // 30s

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

const categoryLabel = (lang: string, cat: string) => {
    if (cat === 'owner') return localizeText(lang, 'Boss Talk', '吐槽老板', '吐槽老闆');
    if (cat === 'project') return localizeText(lang, 'Project Gossip', '项目八卦', '專案八卦');
    if (cat === 'news') return localizeText(lang, 'Industry News', '业界新闻', '業界新聞');
    return cat;
};

const getDateTimeLocale = (lang: string) => (
    lang === 'zh-Hans' ? 'zh-CN' : lang === 'zh-Hant' ? 'zh-TW' : 'en-US'
);

const toolbarInputStyle = {
    padding: '5px 10px',
    borderRadius: radius.md,
    border: `1px solid ${colors.border}`,
    background: colors.surfaceMuted,
    color: colors.text,
    fontSize: '0.8rem',
    outline: 'none',
};

const toolbarButtonStyle = {
    padding: '5px 12px',
    borderRadius: radius.md,
    border: `1px solid ${colors.border}`,
    background: colors.surface,
    color: colors.text,
    fontSize: '0.8rem',
    cursor: 'pointer',
};

const primaryButtonStyle = (disabled: boolean) => ({
    marginLeft: 'auto',
    padding: '5px 16px',
    borderRadius: radius.md,
    border: `1px solid ${disabled ? colors.border : colors.primary}`,
    fontSize: '0.8rem',
    cursor: disabled ? 'default' : 'pointer',
    background: disabled ? colors.surfaceMuted : colors.primary,
    color: disabled ? colors.textMuted : colors.onPrimary,
});

const postCardStyle = {
    ...remoteCardStyle,
    marginBottom: '10px',
    padding: '12px 14px',
    fontSize: '0.8rem',
    lineHeight: 1.6,
    textAlign: 'left' as const,
};

const nestedCardStyle = {
    marginTop: '10px',
    padding: '10px 12px',
    borderRadius: radius.lg,
    background: colors.bg,
    border: `1px solid ${colors.border}`,
};

const categoryStyleMap = {
    owner: { background: colors.warningBg, color: colors.warning },
    project: { background: colors.infoBg, color: colors.primaryDark },
    news: { background: colors.successBg, color: colors.success },
} as const;

export function GossipPanel({ lang }: GossipPanelProps) {
    const [posts, setPosts] = useState<GossipPost[]>([]);
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState('');
    const [search, setSearch] = useState('');
    const [sort, setSort] = useState<SortMode>('newest');
    const [page, setPage] = useState(1);
    const etagRef = useRef('');

    // Publish form state
    const [showPublish, setShowPublish] = useState(false);
    const [publishContent, setPublishContent] = useState('');
    const [publishCategory, setPublishCategory] = useState('news');
    const [publishing, setPublishing] = useState(false);
    const [publishError, setPublishError] = useState('');

    // Per-post comment/rating state
    const [expandedComments, setExpandedComments] = useState<Record<string, boolean>>({});
    const [postComments, setPostComments] = useState<Record<string, GossipCommentData[]>>({});
    const [commentInputs, setCommentInputs] = useState<Record<string, string>>({});
    const [commentErrors, setCommentErrors] = useState<Record<string, string>>({});
    const [ratingErrors, setRatingErrors] = useState<Record<string, string>>({});

    const fetchSnapshot = useCallback(async (isPolling = false) => {
        if (!isPolling) setLoading(true);
        try {
            const result = await GossipSnapshot(etagRef.current);
            if (!result.changed) return; // equivalent to 304
            if (result.etag) etagRef.current = result.etag;
            setPosts(result.posts || []);
            setError('');
        } catch (err: any) {
            if (!isPolling) setError(err.message || localizeText(lang, 'Failed to load', '加载失败'));
        } finally {
            if (!isPolling) setLoading(false);
        }
    }, []);

    const handlePublish = useCallback(async () => {
        if (!publishContent.trim() || publishContent.length > 2000) return;
        setPublishing(true);
        setPublishError('');
        try {
            await GossipPublish(publishContent, publishCategory);
            setPublishContent('');
            setShowPublish(false);
            etagRef.current = '';
            fetchSnapshot();
        } catch (err: any) {
            setPublishError(err.message || localizeText(lang, 'Publish failed', '发布失败'));
        } finally {
            setPublishing(false);
        }
    }, [publishContent, publishCategory, fetchSnapshot, lang]);

    const fetchComments = useCallback(async (postID: string) => {
        try {
            const result = await GossipGetComments(postID, 1);
            setPostComments(prev => ({ ...prev, [postID]: result.comments || [] }));
            setCommentErrors(prev => ({ ...prev, [postID]: '' }));
        } catch (err: any) {
            setCommentErrors(prev => ({ ...prev, [postID]: err.message || localizeText(lang, 'Failed to load comments', '加载评论失败') }));
        }
    }, [lang]);

    const toggleComments = useCallback((postID: string) => {
        setExpandedComments(prev => {
            const next = { ...prev, [postID]: !prev[postID] };
            if (next[postID]) fetchComments(postID);
            return next;
        });
    }, [fetchComments]);

    const handleComment = useCallback(async (postID: string) => {
        const content = (commentInputs[postID] || '').trim();
        if (!content || content.length > 1000) return;
        setCommentErrors(prev => ({ ...prev, [postID]: '' }));
        try {
            await GossipComment(postID, content, 0);
            setCommentInputs(prev => ({ ...prev, [postID]: '' }));
            fetchComments(postID);
            etagRef.current = '';
            fetchSnapshot();
        } catch (err: any) {
            setCommentErrors(prev => ({ ...prev, [postID]: err.message || localizeText(lang, 'Comment failed', '评论失败') }));
        }
    }, [commentInputs, fetchComments, fetchSnapshot, lang]);

    const handleRate = useCallback(async (postID: string, rating: number) => {
        setRatingErrors(prev => ({ ...prev, [postID]: '' }));
        try {
            await GossipRate(postID, rating);
            etagRef.current = '';
            fetchSnapshot();
        } catch (err: any) {
            setRatingErrors(prev => ({ ...prev, [postID]: err.message || localizeText(lang, 'Rating failed', '评分失败') }));
        }
    }, [fetchSnapshot, lang]);

    // Initial fetch
    useEffect(() => {
        fetchSnapshot();
    }, [fetchSnapshot]);

    // Polling
    useEffect(() => {
        const timer = setInterval(() => fetchSnapshot(true), POLL_INTERVAL);
        return () => clearInterval(timer);
    }, [fetchSnapshot]);

    // Filtered + sorted + paginated
    const filtered = useMemo(() => {
        let list = posts;
        if (search.trim()) {
            const q = search.trim().toLowerCase();
            list = list.filter(p =>
                p.nickname.toLowerCase().includes(q) ||
                p.content.toLowerCase().includes(q) ||
                categoryLabel(lang, p.category).toLowerCase().includes(q)
            );
        }
        if (sort === 'newest') list = [...list].sort((a, b) => b.created_at.localeCompare(a.created_at));
        else if (sort === 'hottest') list = [...list].sort((a, b) => b.votes - a.votes || b.score - a.score);
        else if (sort === 'score') list = [...list].sort((a, b) => b.score - a.score || b.votes - a.votes);
        return list;
    }, [posts, search, sort, lang]);

    const totalPages = Math.max(1, Math.ceil(filtered.length / PAGE_SIZE));
    const currentPage = Math.min(page, totalPages);
    const pageItems = filtered.slice((currentPage - 1) * PAGE_SIZE, currentPage * PAGE_SIZE);

    // Reset page when search/sort changes
    useEffect(() => { setPage(1); }, [search, sort]);

    return (
        <div style={{ padding: '0 15px', width: '100%', boxSizing: 'border-box' }}>
            {/* Toolbar */}
            <div style={{ display: 'flex', gap: '8px', alignItems: 'center', marginBottom: '12px', flexWrap: 'wrap' }}>
                <input
                    type="text"
                    value={search}
                    onChange={e => setSearch(e.target.value)}
                    placeholder={localizeText(lang, 'Search nickname/content/category...', '搜索昵称/内容/分类...')}
                    style={{ ...toolbarInputStyle, flex: 1, minWidth: '140px' }}
                />
                <select
                    value={sort}
                    onChange={e => setSort(e.target.value as SortMode)}
                    style={{ ...toolbarInputStyle, padding: '5px 8px', cursor: 'pointer' }}
                >
                    <option value="newest">{localizeText(lang, 'Newest', '最新')}</option>
                    <option value="hottest">{localizeText(lang, 'Hottest', '最热')}</option>
                    <option value="score">{localizeText(lang, 'Score', '评分')}</option>
                </select>
                <button
                    onClick={() => { etagRef.current = ''; fetchSnapshot(); }}
                    style={toolbarButtonStyle}
                >
                    {localizeText(lang, 'Refresh', '刷新')}
                </button>
                <button
                    onClick={() => setShowPublish(v => !v)}
                    style={{ ...toolbarButtonStyle, background: showPublish ? colors.infoBg : colors.surface, color: showPublish ? colors.primaryDark : colors.text }}
                >
                    {localizeText(lang, 'Publish', '发布')}
                </button>
            </div>

            {/* Publish Form */}
            {showPublish && (
                <div style={{ ...postCardStyle, marginBottom: '12px', background: colors.bg }}>
                    <textarea
                        value={publishContent}
                        onChange={e => setPublishContent(e.target.value)}
                        placeholder={localizeText(lang, 'Write some gossip...', '写点什么八卦...')}
                        style={{
                            width: '100%', minHeight: '80px', padding: '8px 10px', borderRadius: radius.md,
                            border: `1px solid ${colors.border}`, fontSize: '0.8rem', resize: 'vertical',
                            outline: 'none', boxSizing: 'border-box', fontFamily: 'inherit',
                            background: colors.surfaceMuted, color: colors.text,
                        }}
                    />
                    <div style={{ display: 'flex', gap: '8px', alignItems: 'center', marginTop: '8px', flexWrap: 'wrap' }}>
                        <select
                            value={publishCategory}
                            onChange={e => setPublishCategory(e.target.value)}
                            style={{ ...toolbarInputStyle, padding: '5px 8px', cursor: 'pointer' }}
                        >
                            <option value="owner">{categoryLabel(lang, 'owner')}</option>
                            <option value="project">{categoryLabel(lang, 'project')}</option>
                            <option value="news">{categoryLabel(lang, 'news')}</option>
                        </select>
                        <span style={{
                            fontSize: '0.7rem',
                            color: publishContent.length > 2000 ? colors.danger : colors.textSecondary,
                        }}>
                            {publishContent.length}/2000
                        </span>
                        <button
                            onClick={handlePublish}
                            disabled={publishing || !publishContent.trim() || publishContent.length > 2000}
                            style={primaryButtonStyle(!publishContent.trim() || publishContent.length > 2000)}
                        >
                            {publishing ? localizeText(lang, 'Publishing...', '发布中...') : localizeText(lang, 'Submit', '提交')}
                        </button>
                    </div>
                    {publishError && (
                        <div style={{ marginTop: '8px', fontSize: '0.75rem', color: colors.danger }}>{publishError}</div>
                    )}
                </div>
            )}

            {/* Status */}
            {loading && <div style={{ ...remoteLoadingStateStyle, fontSize: '0.8rem' }}>{localizeText(lang, 'Loading...', '加载中...')}</div>}
            {error && <div style={{ textAlign: 'center', padding: '20px', color: colors.danger, fontSize: '0.8rem' }}>{error}</div>}

            {/* Posts */}
            {!loading && !error && pageItems.length === 0 && (
                <div style={{ ...remoteEmptyStateStyle, padding: '40px 20px', fontSize: '0.85rem' }}>
                    {search ? localizeText(lang, 'No matching gossip', '没有匹配的八卦') : localizeText(lang, 'No gossip yet', '暂无八卦')}
                </div>
            )}

            {pageItems.map(p => (
                <div key={p.id} style={postCardStyle}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
                        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                            <span style={{ fontWeight: 600, color: colors.primary }}>{p.nickname}</span>
                            <span style={{
                                padding: '1px 6px', borderRadius: '4px', fontSize: '0.65rem',
                                ...(categoryStyleMap[p.category as keyof typeof categoryStyleMap] || categoryStyleMap.news),
                            }}>
                                {categoryLabel(lang, p.category)}
                            </span>
                            {p.locked && <span style={{ fontSize: '0.65rem', color: colors.danger }}>🔒</span>}
                        </div>
                        <span style={{ fontSize: '0.7rem', color: colors.textMuted }}>
                            {new Date(p.created_at).toLocaleString(getDateTimeLocale(lang))}
                        </span>
                    </div>
                    <div style={{ color: colors.text, whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{p.content}</div>
                    <div style={{ display: 'flex', gap: '12px', marginTop: '6px', fontSize: '0.7rem', color: colors.textSecondary }}>
                        <span>⭐ {p.score > 0 ? (p.score / Math.max(p.votes, 1)).toFixed(1) : '-'}</span>
                        <span>👥 {p.votes} {localizeText(lang, 'votes', '票')}</span>
                    </div>

                    {/* Comment button + Rating stars */}
                    <div style={{ display: 'flex', gap: '12px', alignItems: 'center', marginTop: '8px', fontSize: '0.75rem' }}>
                        <button
                            onClick={() => toggleComments(p.id)}
                            style={{
                                padding: '3px 10px', borderRadius: '4px', border: `1px solid ${colors.border}`,
                                background: expandedComments[p.id] ? colors.infoBg : colors.surface,
                                fontSize: '0.75rem', cursor: 'pointer', color: colors.primary,
                            }}
                        >
                            {localizeText(lang, 'Comment', '评论')}
                        </button>
                        {p.locked ? (
                            <span style={{ fontSize: '0.7rem', color: colors.textMuted }}>🔒 {localizeText(lang, 'Locked', '已锁定')}</span>
                        ) : (
                            <span style={{ display: 'flex', gap: '2px', cursor: 'pointer', color: colors.warning }}>
                                {[1, 2, 3, 4, 5].map(star => (
                                    <span
                                        key={star}
                                        onClick={() => handleRate(p.id, star)}
                                        style={{ fontSize: '0.9rem', cursor: 'pointer' }}
                                        title={`${localizeText(lang, 'Rate', '评分')} ${star}`}
                                    >
                                        {star <= Math.round(p.score / Math.max(p.votes, 1)) ? '★' : '☆'}
                                    </span>
                                ))}
                            </span>
                        )}
                        {ratingErrors[p.id] && (
                            <span style={{ fontSize: '0.7rem', color: colors.danger }}>{ratingErrors[p.id]}</span>
                        )}
                    </div>

                    {/* Expanded comment area */}
                    {expandedComments[p.id] && (
                        <div style={nestedCardStyle}>
                            {/* Comments list */}
                            {(postComments[p.id] || []).length > 0 ? (
                                <div style={{ marginBottom: '8px' }}>
                                    {(postComments[p.id] || []).map(c => (
                                        <div key={c.id} style={{ marginBottom: '6px', fontSize: '0.75rem', lineHeight: 1.5 }}>
                                            <span style={{ fontWeight: 600, color: colors.primary }}>{c.nickname}</span>
                                            <span style={{ color: colors.text, marginLeft: '6px' }}>{c.content}</span>
                                            {c.rating > 0 && (
                                                <span style={{ marginLeft: '6px', color: colors.warning }}>
                                                    {[1, 2, 3, 4, 5].map(s => (
                                                        <span key={s}>{s <= c.rating ? '★' : '☆'}</span>
                                                    ))}
                                                </span>
                                            )}
                                            <span style={{ marginLeft: '8px', fontSize: '0.65rem', color: colors.textMuted }}>
                                                {new Date(c.created_at).toLocaleString(getDateTimeLocale(lang))}
                                            </span>
                                        </div>
                                    ))}
                                </div>
                            ) : (
                                <div style={{ fontSize: '0.75rem', color: colors.textMuted, marginBottom: '8px' }}>
                                    {localizeText(lang, 'No comments yet', '暂无评论')}
                                </div>
                            )}

                            {/* Comment input (hidden if locked) */}
                            {!p.locked && (
                                <>
                                    <div style={{ borderTop: `1px solid ${colors.border}`, paddingTop: '8px' }}>
                                        <textarea
                                            value={commentInputs[p.id] || ''}
                                            onChange={e => setCommentInputs(prev => ({ ...prev, [p.id]: e.target.value }))}
                                            placeholder={localizeText(lang, 'Write a comment...', '写评论...')}
                                            style={{
                                                width: '100%', minHeight: '50px', padding: '6px 8px', borderRadius: radius.md,
                                                border: `1px solid ${colors.border}`, fontSize: '0.75rem', resize: 'vertical',
                                                outline: 'none', boxSizing: 'border-box', fontFamily: 'inherit',
                                                background: colors.surfaceMuted, color: colors.text,
                                            }}
                                        />
                                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '6px' }}>
                                            <span style={{
                                                fontSize: '0.65rem',
                                                color: (commentInputs[p.id] || '').length > 1000 ? colors.danger : colors.textSecondary,
                                            }}>
                                                {(commentInputs[p.id] || '').length}/1000
                                            </span>
                                            <button
                                                onClick={() => handleComment(p.id)}
                                                disabled={!(commentInputs[p.id] || '').trim() || (commentInputs[p.id] || '').length > 1000}
                                                style={{
                                                    padding: '4px 12px', borderRadius: radius.md, border: `1px solid ${!(commentInputs[p.id] || '').trim() || (commentInputs[p.id] || '').length > 1000 ? colors.border : colors.primary}`,
                                                    fontSize: '0.75rem', cursor: (!(commentInputs[p.id] || '').trim() || (commentInputs[p.id] || '').length > 1000) ? 'default' : 'pointer',
                                                    background: (!(commentInputs[p.id] || '').trim() || (commentInputs[p.id] || '').length > 1000) ? colors.surfaceMuted : colors.primary,
                                                    color: (!(commentInputs[p.id] || '').trim() || (commentInputs[p.id] || '').length > 1000) ? colors.textMuted : colors.onPrimary,
                                                }}
                                            >
                                                {localizeText(lang, 'Submit', '提交')}
                                            </button>
                                        </div>
                                    </div>
                                </>
                            )}

                            {/* Error message */}
                            {commentErrors[p.id] && (
                                <div style={{ marginTop: '6px', fontSize: '0.7rem', color: colors.danger }}>{commentErrors[p.id]}</div>
                            )}
                        </div>
                    )}
                </div>
            ))}

            {/* Pagination */}
            {totalPages > 1 && (
                <div style={{ display: 'flex', justifyContent: 'center', alignItems: 'center', gap: '12px', padding: '10px 0', fontSize: '0.8rem' }}>
                    <button
                        disabled={currentPage <= 1}
                        onClick={() => setPage(p => Math.max(1, p - 1))}
                        style={{ ...toolbarButtonStyle, padding: '4px 10px', borderRadius: '4px', cursor: currentPage <= 1 ? 'default' : 'pointer', opacity: currentPage <= 1 ? 0.4 : 1 }}
                    >
                        ‹ {localizeText(lang, 'Prev', '上一页')}
                    </button>
                    <span style={{ color: colors.textSecondary }}>{currentPage} / {totalPages}</span>
                    <button
                        disabled={currentPage >= totalPages}
                        onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                        style={{ ...toolbarButtonStyle, padding: '4px 10px', borderRadius: '4px', cursor: currentPage >= totalPages ? 'default' : 'pointer', opacity: currentPage >= totalPages ? 0.4 : 1 }}
                    >
                        {localizeText(lang, 'Next', '下一页')} ›
                    </button>
                </div>
            )}

            {/* Summary */}
            <div style={{ textAlign: 'center', fontSize: '0.7rem', color: colors.textMuted, padding: '4px 0' }}>
                {localizeText(lang, `${filtered.length} total`, `共 ${filtered.length} 条`)}
            </div>
        </div>
    );
}
