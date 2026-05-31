import { useCallback, useEffect, useRef, useState } from 'react';
import { GroupDiscussionListLocalHidden, GroupDiscussionListMine, GroupDiscussionSetLocalHidden } from '../../../wailsjs/go/main/App';
import { EventsOff, EventsOn } from '../../../wailsjs/runtime';
import { getHistoryDiscussionRelation, isHistoryDiscussionReadOnly } from '../ai/historyDiscussionUtils';

export type HistoryDiscussionSummary = {
    id?: string;
    topic?: string;
    question?: string;
    local_relation?: string;
    role?: string;
    readonly?: boolean;
    status?: string;
    participant_ids?: string[];
};

type Props = {
    lang: string;
    enabled?: boolean;
    onOpenDiscussion?: (discussion: HistoryDiscussionSummary) => void;
};

type ContextMenuState = {
    x: number;
    y: number;
    item: HistoryDiscussionSummary;
};

const textForLang = (lang: string, en: string, zh: string, zht: string) => lang === 'en' ? en : lang === 'zh-Hant' ? zht : zh;
const HISTORY_REFRESH_DELAY_MS = 150;

function safeEventsOn(eventName: string, callback: (...args: any[]) => void) {
    try {
        return EventsOn(eventName, callback);
    } catch {
        return undefined;
    }
}

function safeEventsOff(eventName: string) {
    try {
        EventsOff(eventName);
    } catch {
        // Runtime events are unavailable in a plain browser dev session.
    }
}

const eventDiscussionKind = (event: any): string => {
    const payload = event?.payload || event || {};
    const message = payload?.message || payload?.Message || {};
    return String(message?.kind || message?.Kind || payload?.kind || payload?.Kind || event?.kind || event?.Kind || '').trim().toLowerCase();
};

const discussionRenameEventInfo = (event: any): { discussionId: string; topic: string } => {
    const outer = event?.payload || event?.Payload || event || {};
    const payload = outer?.payload || outer?.Payload || outer;
    const discussionId = String(payload?.discussion_id || payload?.discussionId || payload?.session_id || payload?.sessionId || event?.discussion_id || event?.session_id || '').trim();
    const topic = String(payload?.topic || payload?.title || payload?.Topic || payload?.Title || event?.topic || event?.title || '').trim();
    return { discussionId, topic };
};


const relationMeta = (lang: string, relation: string) => {
    if (relation === 'initiated_by_me') {
        return {
            icon: '\u2197',
            label: textForLang(lang, 'Started by me', '\u6211\u53d1\u8d77\u7684', '\u6211\u767c\u8d77\u7684'),
        };
    }
    if (relation === 'owned_ve_invited') {
        return {
            icon: '\u2199',
            label: textForLang(lang, 'My digital employee invited', '\u6211\u7684\u6570\u5b57\u5458\u5de5\u53d7\u9080', '\u6211\u7684\u6578\u5b57\u54e1\u5de5\u53d7\u9080'),
        };
    }
    return {
        icon: '\u25e6',
        label: textForLang(lang, 'History session', '\u5386\u53f2\u4f1a\u8bdd', '\u6b77\u53f2\u6703\u8a71'),
    };
};

export const SidebarHistorySessions = ({ lang, enabled = true, onOpenDiscussion }: Props) => {
    const [items, setItems] = useState<HistoryDiscussionSummary[]>([]);
    const [loading, setLoading] = useState(false);
    const [showHidden, setShowHidden] = useState(false);
    const [contextMenu, setContextMenu] = useState<ContextMenuState | null>(null);
    const loadSeqRef = useRef(0);
    const refreshTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    const loadItems = useCallback(async () => {
        if (!enabled) return;
        const seq = loadSeqRef.current + 1;
        loadSeqRef.current = seq;
        setLoading(true);
        try {
            const list = showHidden ? await GroupDiscussionListLocalHidden() : await GroupDiscussionListMine('all');
            if (loadSeqRef.current === seq) setItems(Array.isArray(list) ? list : []);
        } catch {
            if (loadSeqRef.current === seq) setItems([]);
        } finally {
            if (loadSeqRef.current === seq) setLoading(false);
        }
    }, [enabled, showHidden]);

    useEffect(() => {
        void loadItems();
    }, [loadItems]);

    const scheduleLoadItems = useCallback(() => {
        if (refreshTimerRef.current) clearTimeout(refreshTimerRef.current);
        refreshTimerRef.current = setTimeout(() => {
            refreshTimerRef.current = null;
            void loadItems();
        }, HISTORY_REFRESH_DELAY_MS);
    }, [loadItems]);

    useEffect(() => {
        if (!enabled) return;
        const refreshNonStream = (event: any) => {
            const kind = eventDiscussionKind(event);
            if (kind === 'stream_chunk' || kind === 'stream_end') return;
            scheduleLoadItems();
        };
        const applyRename = (event: any) => {
            const eventType = String(event?.type || event?.Type || '').trim();
            if (eventType && eventType !== 've:discussion_rename') return;
            const { discussionId, topic } = discussionRenameEventInfo(event);
            if (!discussionId || !topic) return;
            setItems(prev => prev.map(item => String(item.id || '').trim() === discussionId ? { ...item, topic } : item));
            scheduleLoadItems();
        };
        const offDiscussion = safeEventsOn('ve-event', refreshNonStream);
        const offRename = safeEventsOn('ve:discussion_rename', applyRename);
        const offStreamEnd = safeEventsOn('ve:stream_end', scheduleLoadItems);
        return () => {
            if (refreshTimerRef.current) {
                clearTimeout(refreshTimerRef.current);
                refreshTimerRef.current = null;
            }
            if (typeof offDiscussion === 'function') offDiscussion();
            else safeEventsOff('ve-event');
            if (typeof offRename === 'function') offRename();
            else safeEventsOff('ve:discussion_rename');
            if (typeof offStreamEnd === 'function') offStreamEnd();
            else safeEventsOff('ve:stream_end');
        };
    }, [enabled, scheduleLoadItems]);

    useEffect(() => {
        if (!contextMenu) return;
        const close = () => setContextMenu(null);
        document.addEventListener('click', close);
        return () => document.removeEventListener('click', close);
    }, [contextMenu]);

    const setHidden = async (item: HistoryDiscussionSummary, hidden: boolean) => {
        const id = String(item.id || '').trim();
        if (!id) return;
        await GroupDiscussionSetLocalHidden(id, hidden);
        setContextMenu(null);
        await loadItems();
    };

    if (!enabled) return null;
    return <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '8px', position: 'relative' }}>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8, marginBottom: 8 }}>
            <span style={{ color: 'var(--theme-text-muted)', fontSize: 11 }}>{showHidden ? textForLang(lang, 'Hidden sessions', '\u5df2\u9690\u85cf\u4f1a\u8bdd', '\u5df2\u96b1\u85cf\u6703\u8a71') : textForLang(lang, 'History sessions', '\u5386\u53f2\u4f1a\u8bdd', '\u6b77\u53f2\u6703\u8a71')}</span>
            <button type="button" onClick={() => setShowHidden((v) => !v)} style={{ border: '1px solid var(--theme-border)', background: 'transparent', color: 'var(--theme-text-secondary)', borderRadius: 5, padding: '2px 7px', cursor: 'pointer', fontSize: 11 }}>
                {showHidden ? textForLang(lang, 'Show visible', '\u770b\u53ef\u89c1', '\u770b\u53ef\u898b') : textForLang(lang, 'Hidden', '\u5df2\u9690\u85cf', '\u5df2\u96b1\u85cf')}
            </button>
        </div>
        {loading && <div style={{ padding: 12, color: 'var(--theme-text-muted)', fontSize: 12 }}>{textForLang(lang, 'Loading...', '\u52a0\u8f7d\u4e2d...', '\u8f09\u5165\u4e2d...')}</div>}
        {!loading && items.length === 0 && <div style={{ padding: 12, color: 'var(--theme-text-muted)', fontSize: 12 }}>{showHidden ? textForLang(lang, 'No hidden sessions', '\u6682\u65e0\u9690\u85cf\u4f1a\u8bdd', '\u66ab\u7121\u96b1\u85cf\u6703\u8a71') : textForLang(lang, 'No history sessions', '\u6682\u65e0\u5386\u53f2\u4f1a\u8bdd', '\u66ab\u7121\u6b77\u53f2\u6703\u8a71')}</div>}
        {items.map((item, index) => {
            const relation = getHistoryDiscussionRelation(item);
            const readOnly = isHistoryDiscussionReadOnly(item);
            const title = item.topic || item.question || item.id || textForLang(lang, 'Untitled session', '\u672a\u547d\u540d\u4f1a\u8bdd', '\u672a\u547d\u540d\u6703\u8a71');
            const meta = relationMeta(lang, relation);
            return <button key={item.id || index} type="button" onClick={() => onOpenDiscussion?.(item)} onKeyDown={(event) => { if (event.key === 'Enter' || event.key === ' ') { event.preventDefault(); onOpenDiscussion?.(item); } }} onContextMenu={(event) => { event.preventDefault(); setContextMenu({ x: event.clientX, y: event.clientY, item }); }} title={textForLang(lang, 'Click, press Enter, or right-click to open session', '\u70b9\u51fb\u3001\u6309 Enter \u6216\u53f3\u952e\u6253\u5f00\u4f1a\u8bdd', '\u9ede\u64ca\u3001\u6309 Enter \u6216\u53f3\u9375\u6253\u958b\u6703\u8a71')} style={{ width: '100%', display: 'flex', alignItems: 'center', gap: 8, border: '1px solid var(--theme-border)', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)', borderRadius: 6, padding: '8px', marginBottom: 6, cursor: 'pointer', textAlign: 'left' }}>
                <span aria-hidden="true" title={meta.label} style={{ width: 22, height: 20, borderRadius: 4, display: 'inline-flex', alignItems: 'center', justifyContent: 'center', flexShrink: 0, background: relation === 'initiated_by_me' ? 'var(--theme-info-bg)' : 'var(--theme-surface-muted)', color: relation === 'initiated_by_me' ? 'var(--theme-primary)' : 'var(--theme-text-secondary)', fontSize: 14, fontWeight: 700, lineHeight: 1 }}>{meta.icon}</span>
                <span style={{ flex: 1, minWidth: 0, display: 'flex', flexDirection: 'column', gap: 2 }}>
                    <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 12 }}>{title}</span>
                    <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 10, color: 'var(--theme-text-muted)' }}>{meta.label}</span>
                </span>
                {readOnly && <span style={{ fontSize: 11, color: 'var(--theme-text-muted)', flexShrink: 0 }}>{textForLang(lang, 'Read-only', '\u53ea\u8bfb', '\u552f\u8b80')}</span>}
            </button>;
        })}
        {contextMenu && <div role="menu" style={{ position: 'fixed', left: contextMenu.x, top: contextMenu.y, zIndex: 9999, minWidth: 132, padding: '4px 0', border: '1px solid var(--theme-border)', borderRadius: 6, background: 'var(--theme-surface)', boxShadow: '0 6px 18px rgba(0,0,0,0.16)' }}>
            <button type="button" role="menuitem" onClick={() => { onOpenDiscussion?.(contextMenu.item); setContextMenu(null); }} style={menuItemStyle}>{textForLang(lang, 'Open session', '\u6253\u5f00\u4f1a\u8bdd', '\u6253\u958b\u6703\u8a71')}</button>
            <button type="button" role="menuitem" onClick={() => void setHidden(contextMenu.item, !showHidden)} style={menuItemStyle}>{showHidden ? textForLang(lang, 'Restore', '\u6062\u590d\u663e\u793a', '\u6062\u5fa9\u986f\u793a') : textForLang(lang, 'Hide locally', '\u672c\u5730\u9690\u85cf', '\u672c\u5730\u96b1\u85cf')}</button>
        </div>}
    </div>;
};

const menuItemStyle = {
    width: '100%',
    border: 0,
    background: 'transparent',
    color: 'var(--theme-text-primary)',
    padding: '6px 12px',
    cursor: 'pointer',
    fontSize: 12,
    textAlign: 'left' as const,
};
