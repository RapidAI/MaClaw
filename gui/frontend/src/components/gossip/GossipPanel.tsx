import { useState, useEffect, useMemo, useCallback, useRef } from 'react';
import { GossipSnapshot, GossipPublish, GossipComment, GossipRate, GossipGetComments } from '../../../wailsjs/go/main/App';

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
    const map: Record<string, Record<string, string>> = {
        owner:   { zh: '吐槽老板', en: 'Boss Talk' },
        project: { zh: '项目八卦', en: 'Project Gossip' },
        news:    { zh: '业界新闻', en: 'Industry News' },
    };
    return map[cat]?.[lang === 'zh-Hans' || lang === 'zh-Hant' ? 'zh' : 'en'] ?? cat;
};

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
                    style={{ flex: 1, minWidth: '140px', padding: '5px 10px', borderRadius: '6px', border: '1px solid #d1d5db', fontSize: '0.8rem', outline: 'none' }}
                />
                <select
                    value={sort}
                    onChange={e => setSort(e.target.value as SortMode)}
                    style={{ padding: '5px 8px', borderRadius: '6px', border: '1px solid #d1d5db', fontSize: '0.8rem', outline: 'none', cursor: 'pointer' }}
                >
                    <option value="newest">{localizeText(lang, 'Newest', '最新')}</option>
                    <option value="hottest">{localizeText(lang, 'Hottest', '最热')}</option>
                    <option value="score">{localizeText(lang, 'Score', '评分')}</option>
                </select>
                <button
                    onClick={() => { etagRef.current = ''; fetchSnapshot(); }}
                    style={{ padding: '5px 12px', borderRadius: '6px', border: '1px solid #d1d5db', background: '#fff', fontSize: '0.8rem', cursor: 'pointer' }}
                >
                    {localizeText(lang, 'Refresh', '刷新')}
                </button>
                <button
                    onClick={() => setShowPublish(v => !v)}
                    style={{ padding: '5px 12px', borderRadius: '6px', border: '1px solid #d1d5db', background: showPublish ? '#eef2ff' : '#fff', fontSize: '0.8rem', cursor: 'pointer' }}
                >
                    {localizeText(lang, 'Publish', '发布')}
                </button>
            </div>

            {/* Publish Form */}
            {showPublish && (
                <div style={{ marginBottom: '12px', padding: '12px 14px', borderRadius: '10px', background: '#f9fafb', border: '1px solid #e5e7eb' }}>
                    <textarea
                        value={publishContent}
                        onChange={e => setPublishContent(e.target.value)}
                        placeholder={localizeText(lang, 'Write some gossip...', '写点什么八卦...')}
                        style={{
                            width: '100%', minHeight: '80px', padding: '8px 10px', borderRadius: '6px',
                            border: '1px solid #d1d5db', fontSize: '0.8rem', resize: 'vertical',
                            outline: 'none', boxSizing: 'border-box', fontFamily: 'inherit'
                        }}
                    />
                    <div style={{ display: 'flex', gap: '8px', alignItems: 'center', marginTop: '8px', flexWrap: 'wrap' }}>
                        <select
                            value={publishCategory}
                            onChange={e => setPublishCategory(e.target.value)}
                            style={{ padding: '5px 8px', borderRadius: '6px', border: '1px solid #d1d5db', fontSize: '0.8rem', outline: 'none', cursor: 'pointer' }}
                        >
                            <option value="owner">{categoryLabel(lang, 'owner')}</option>
                            <option value="project">{categoryLabel(lang, 'project')}</option>
                            <option value="news">{categoryLabel(lang, 'news')}</option>
                        </select>
                        <span style={{
                            fontSize: '0.7rem',
                            color: publishContent.length > 2000 ? '#ef4444' : '#6b7280'
                        }}>
                            {publishContent.length}/2000
                        </span>
                        <button
                            onClick={handlePublish}
                            disabled={publishing || !publishContent.trim() || publishContent.length > 2000}
                            style={{
                                marginLeft: 'auto', padding: '5px 16px', borderRadius: '6px',
                                border: '1px solid #d1d5db', fontSize: '0.8rem', cursor: 'pointer',
                                background: (!publishContent.trim() || publishContent.length > 2000) ? '#f3f4f6' : '#4f46e5',
                                color: (!publishContent.trim() || publishContent.length > 2000) ? '#9ca3af' : '#fff',
                                opacity: publishing ? 0.6 : 1
                            }}
                        >
                            {publishing ? localizeText(lang, 'Publishing...', '发布中...') : localizeText(lang, 'Submit', '提交')}
                        </button>
                    </div>
                    {publishError && (
                        <div style={{ marginTop: '8px', fontSize: '0.75rem', color: '#ef4444' }}>{publishError}</div>
                    )}
                </div>
            )}

            {/* Status */}
            {loading && <div style={{ textAlign: 'center', padding: '20px', color: '#6b7280', fontSize: '0.8rem' }}>{localizeText(lang, 'Loading...', '加载中...')}</div>}
            {error && <div style={{ textAlign: 'center', padding: '20px', color: '#ef4444', fontSize: '0.8rem' }}>{error}</div>}

            {/* Posts */}
            {!loading && !error && pageItems.length === 0 && (
                <div style={{ textAlign: 'center', padding: '40px 20px', color: '#9ca3af', fontSize: '0.85rem' }}>
                    {search ? localizeText(lang, 'No matching gossip', '没有匹配的八卦') : localizeText(lang, 'No gossip yet', '暂无八卦')}
                </div>
            )}

            {pageItems.map(p => (
                <div key={p.id} style={{
                    marginBottom: '10px', padding: '12px 14px', borderRadius: '10px',
                    background: '#fff', border: '1px solid #e5e7eb',
                    fontSize: '0.8rem', lineHeight: 1.6, textAlign: 'left'
                }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: '6px' }}>
                        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
                            <span style={{ fontWeight: 600, color: '#4f46e5' }}>{p.nickname}</span>
                            <span style={{
                                padding: '1px 6px', borderRadius: '4px', fontSize: '0.65rem',
                                background: p.category === 'owner' ? '#fef3c7' : p.category === 'project' ? '#dbeafe' : '#d1fae5',
                                color: p.category === 'owner' ? '#92400e' : p.category === 'project' ? '#1e40af' : '#065f46'
                            }}>
                                {categoryLabel(lang, p.category)}
                            </span>
                            {p.locked && <span style={{ fontSize: '0.65rem', color: '#ef4444' }}>🔒</span>}
                        </div>
                        <span style={{ fontSize: '0.7rem', color: '#9ca3af' }}>
                            {new Date(p.created_at).toLocaleString()}
                        </span>
                    </div>
                    <div style={{ color: '#374151', whiteSpace: 'pre-wrap', wordBreak: 'break-word' }}>{p.content}</div>
                    <div style={{ display: 'flex', gap: '12px', marginTop: '6px', fontSize: '0.7rem', color: '#6b7280' }}>
                        <span>⭐ {p.score > 0 ? (p.score / Math.max(p.votes, 1)).toFixed(1) : '-'}</span>
                        <span>👥 {p.votes} {localizeText(lang, 'votes', '票')}</span>
                    </div>

                    {/* Comment button + Rating stars */}
                    <div style={{ display: 'flex', gap: '12px', alignItems: 'center', marginTop: '8px', fontSize: '0.75rem' }}>
                        <button
                            onClick={() => toggleComments(p.id)}
                            style={{
                                padding: '3px 10px', borderRadius: '4px', border: '1px solid #d1d5db',
                                background: expandedComments[p.id] ? '#eef2ff' : '#fff',
                                fontSize: '0.75rem', cursor: 'pointer', color: '#4f46e5'
                            }}
                        >
                            {localizeText(lang, 'Comment', '评论')}
                        </button>
                        {p.locked ? (
                            <span style={{ fontSize: '0.7rem', color: '#9ca3af' }}>🔒 {localizeText(lang, 'Locked', '已锁定')}</span>
                        ) : (
                            <span style={{ display: 'flex', gap: '2px', cursor: 'pointer' }}>
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
                            <span style={{ fontSize: '0.7rem', color: '#ef4444' }}>{ratingErrors[p.id]}</span>
                        )}
                    </div>

                    {/* Expanded comment area */}
                    {expandedComments[p.id] && (
                        <div style={{ marginTop: '10px', padding: '10px 12px', borderRadius: '8px', background: '#f9fafb', border: '1px solid #e5e7eb' }}>
                            {/* Comments list */}
                            {(postComments[p.id] || []).length > 0 ? (
                                <div style={{ marginBottom: '8px' }}>
                                    {(postComments[p.id] || []).map(c => (
                                        <div key={c.id} style={{ marginBottom: '6px', fontSize: '0.75rem', lineHeight: 1.5 }}>
                                            <span style={{ fontWeight: 600, color: '#4f46e5' }}>{c.nickname}</span>
                                            <span style={{ color: '#374151', marginLeft: '6px' }}>{c.content}</span>
                                            {c.rating > 0 && (
                                                <span style={{ marginLeft: '6px', color: '#f59e0b' }}>
                                                    {[1, 2, 3, 4, 5].map(s => (
                                                        <span key={s}>{s <= c.rating ? '★' : '☆'}</span>
                                                    ))}
                                                </span>
                                            )}
                                            <span style={{ marginLeft: '8px', fontSize: '0.65rem', color: '#9ca3af' }}>
                                                {new Date(c.created_at).toLocaleString()}
                                            </span>
                                        </div>
                                    ))}
                                </div>
                            ) : (
                                <div style={{ fontSize: '0.75rem', color: '#9ca3af', marginBottom: '8px' }}>
                                    {localizeText(lang, 'No comments yet', '暂无评论')}
                                </div>
                            )}

                            {/* Comment input (hidden if locked) */}
                            {!p.locked && (
                                <>
                                    <div style={{ borderTop: '1px solid #e5e7eb', paddingTop: '8px' }}>
                                        <textarea
                                            value={commentInputs[p.id] || ''}
                                            onChange={e => setCommentInputs(prev => ({ ...prev, [p.id]: e.target.value }))}
                                            placeholder={localizeText(lang, 'Write a comment...', '写评论...')}
                                            style={{
                                                width: '100%', minHeight: '50px', padding: '6px 8px', borderRadius: '6px',
                                                border: '1px solid #d1d5db', fontSize: '0.75rem', resize: 'vertical',
                                                outline: 'none', boxSizing: 'border-box', fontFamily: 'inherit'
                                            }}
                                        />
                                        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginTop: '6px' }}>
                                            <span style={{
                                                fontSize: '0.65rem',
                                                color: (commentInputs[p.id] || '').length > 1000 ? '#ef4444' : '#6b7280'
                                            }}>
                                                {(commentInputs[p.id] || '').length}/1000
                                            </span>
                                            <button
                                                onClick={() => handleComment(p.id)}
                                                disabled={!(commentInputs[p.id] || '').trim() || (commentInputs[p.id] || '').length > 1000}
                                                style={{
                                                    padding: '4px 12px', borderRadius: '6px', border: '1px solid #d1d5db',
                                                    fontSize: '0.75rem', cursor: 'pointer',
                                                    background: (!(commentInputs[p.id] || '').trim() || (commentInputs[p.id] || '').length > 1000) ? '#f3f4f6' : '#4f46e5',
                                                    color: (!(commentInputs[p.id] || '').trim() || (commentInputs[p.id] || '').length > 1000) ? '#9ca3af' : '#fff'
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
                                <div style={{ marginTop: '6px', fontSize: '0.7rem', color: '#ef4444' }}>{commentErrors[p.id]}</div>
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
                        style={{ padding: '4px 10px', borderRadius: '4px', border: '1px solid #d1d5db', background: '#fff', cursor: currentPage <= 1 ? 'default' : 'pointer', opacity: currentPage <= 1 ? 0.4 : 1 }}
                    >
                        ‹ {localizeText(lang, 'Prev', '上一页')}
                    </button>
                    <span style={{ color: '#6b7280' }}>{currentPage} / {totalPages}</span>
                    <button
                        disabled={currentPage >= totalPages}
                        onClick={() => setPage(p => Math.min(totalPages, p + 1))}
                        style={{ padding: '4px 10px', borderRadius: '4px', border: '1px solid #d1d5db', background: '#fff', cursor: currentPage >= totalPages ? 'default' : 'pointer', opacity: currentPage >= totalPages ? 0.4 : 1 }}
                    >
                        {localizeText(lang, 'Next', '下一页')} ›
                    </button>
                </div>
            )}

            {/* Summary */}
            <div style={{ textAlign: 'center', fontSize: '0.7rem', color: '#9ca3af', padding: '4px 0' }}>
                {localizeText(lang, `${filtered.length} total`, `共 ${filtered.length} 条`)}
            </div>
        </div>
    );
}
