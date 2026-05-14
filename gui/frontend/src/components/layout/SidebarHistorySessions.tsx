import { useEffect, useState } from 'react';
import { GroupDiscussionListMine } from '../../../wailsjs/go/main/App';

export type HistoryDiscussionSummary = {
    id?: string;
    topic?: string;
    question?: string;
    local_relation?: string;
    readonly?: boolean;
    participant_ids?: string[];
};

type Props = {
    lang: string;
    enabled?: boolean;
    onOpenDiscussion?: (discussion: HistoryDiscussionSummary) => void;
};

const textForLang = (lang: string, en: string, zh: string, zht: string) => lang === 'en' ? en : lang === 'zh-Hant' ? zht : zh;

export const SidebarHistorySessions = ({ lang, enabled = true, onOpenDiscussion }: Props) => {
    const [items, setItems] = useState<HistoryDiscussionSummary[]>([]);
    const [loading, setLoading] = useState(false);
    useEffect(() => {
        if (!enabled) return;
        setLoading(true);
        GroupDiscussionListMine('all')
            .then((list: any) => setItems(Array.isArray(list) ? list : []))
            .catch(() => setItems([]))
            .finally(() => setLoading(false));
    }, [enabled]);
    if (!enabled) return null;
    return <div style={{ flex: 1, minHeight: 0, overflowY: 'auto', padding: '8px' }}>
        {loading && <div style={{ padding: 12, color: 'var(--theme-text-muted)', fontSize: 12 }}>{textForLang(lang, 'Loading...', '加载中...', '載入中...')}</div>}
        {!loading && items.length === 0 && <div style={{ padding: 12, color: 'var(--theme-text-muted)', fontSize: 12 }}>{textForLang(lang, 'No history sessions', '暂无历史会话', '暫無歷史會話')}</div>}
        {items.map((item, index) => {
            const relation = String(item.local_relation || '').toLowerCase();
            const readOnly = item.readonly === true || relation === 'owned_ve_invited';
            const title = item.topic || item.question || item.id || textForLang(lang, 'Untitled session', '未命名会话', '未命名會話');
            const icon = relation === 'initiated_by_me' ? '●' : '◇';
            return <button key={item.id || index} type="button" onDoubleClick={() => onOpenDiscussion?.(item)} onClick={() => {}} title={textForLang(lang, 'Double-click to open session', '双击打开会话', '雙擊打開會話')} style={{ width: '100%', display: 'flex', alignItems: 'center', gap: 8, border: '1px solid var(--theme-border)', background: 'var(--theme-surface)', color: 'var(--theme-text-primary)', borderRadius: 6, padding: '8px', marginBottom: 6, cursor: 'pointer', textAlign: 'left' }}>
                <span aria-hidden="true">{icon}</span>
                <span style={{ flex: 1, minWidth: 0, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap', fontSize: 12 }}>{title}</span>
                {readOnly && <span style={{ fontSize: 11, color: 'var(--theme-text-muted)' }}>{textForLang(lang, 'Read-only', '只读', '唯讀')}</span>}
            </button>;
        })}
    </div>;
};
