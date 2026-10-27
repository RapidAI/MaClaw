import { useEffect, useId, useRef, useState } from 'react';
import { useSafeBackdropDismiss } from '../../../hooks/useSafeBackdropDismiss';
import {
    answeredSinceLast, applyMention, ATTENTION_LABEL, attentionOf, botName, defaultCollabApi, mentionCandidates, mentionQuery,
    type Attention, type BotRef, type CollabApi, type Room, type SaidItem,
} from './botCollab';

const ROOM_POLL_MS = 4000;

/** Group rooms: list, create/edit (members, attention), and a room chat with @mentions. */
export function BotRoomsDialog({ bots, onClose, api = defaultCollabApi, pollMs = ROOM_POLL_MS }: {
    bots: BotRef[];
    onClose: () => void;
    api?: CollabApi;
    pollMs?: number;
}) {
    const titleId = useId();
    const [rooms, setRooms] = useState<Room[]>([]);
    const [openId, setOpenId] = useState('');
    const [editing, setEditing] = useState<Room | 'new' | null>(null);
    const [notice, setNotice] = useState('');
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

    const reload = async () => {
        try {
            setRooms((await api.rooms()) || []);
        } catch (e) {
            setNotice(e instanceof Error && e.message ? e.message : '读取群聊失败');
        }
    };
    useEffect(() => { void reload(); /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, []);

    const open = rooms.find(r => r.id === openId) || null;
    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-edit-dialog-backdrop" {...backdropProps}>
            <div className="modal-content custom-dialog bot-edit-dialog bot-rooms-dialog" role="dialog" aria-modal="true" aria-labelledby={titleId} data-testid="bot-rooms-dialog" {...dialogProps}>
                <div className="modal-header custom-dialog__header">
                    <h3 id={titleId} className="custom-dialog__title">{open ? `群聊 · ${open.name}` : '群聊'}</h3>
                </div>
                <div className="modal-body custom-dialog__body">
                    {editing ? (
                        <RoomEditor room={editing === 'new' ? null : editing} bots={bots} api={api}
                            onDone={async saved => { setEditing(null); await reload(); if (saved) setOpenId(saved.id); }} />
                    ) : open ? (
                        <RoomChat room={open} bots={bots} api={api} pollMs={pollMs} />
                    ) : (
                        <>
                            {rooms.length === 0 ? <p data-testid="bot-rooms-empty">还没有群聊。建一个群，把几个机器人拉进来一起讨论。</p> : null}
                            <ul className="bot-rooms__list">
                                {rooms.map(r => (
                                    <li key={r.id}>
                                        <button type="button" className="bot-rooms__open" data-testid={`bot-room-open-${r.id}`} onClick={() => setOpenId(r.id)}>
                                            <strong>{r.name}</strong>
                                            <small>{r.members.map(m => botName(bots, m)).join('、')}</small>
                                        </button>
                                        <button type="button" className="btn-secondary" data-testid={`bot-room-edit-${r.id}`} onClick={() => setEditing(r)}>设置</button>
                                        <button type="button" className="btn-secondary" data-testid={`bot-room-delete-${r.id}`} onClick={async () => {
                                            try { await api.deleteRoom(r.id); await reload(); } catch (e) { setNotice(e instanceof Error ? e.message : '删除失败'); }
                                        }}>删除</button>
                                    </li>
                                ))}
                            </ul>
                        </>
                    )}
                    {notice ? <p className="custom-dialog__notice" role="alert">{notice}</p> : null}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    {open && !editing ? <button type="button" className="btn-secondary" data-testid="bot-room-back" onClick={() => setOpenId('')}>返回列表</button> : null}
                    {open && !editing ? <button type="button" className="btn-secondary" onClick={() => setEditing(open)}>群设置</button> : null}
                    {!open && !editing ? <button type="button" className="btn-primary" data-testid="bot-room-new" onClick={() => setEditing('new')}>新建群聊</button> : null}
                    <button type="button" className="btn-secondary" onClick={onClose}>关闭</button>
                </div>
            </div>
        </div>
    );
}

function RoomEditor({ room, bots, api, onDone }: { room: Room | null; bots: BotRef[]; api: CollabApi; onDone: (saved: Room | null) => void }) {
    const [name, setName] = useState(room?.name || '');
    const [members, setMembers] = useState<string[]>(room?.members || bots.map(b => b.id));
    const [attention, setAttention] = useState<Record<string, Attention>>(() => {
        const out: Record<string, Attention> = {};
        for (const b of bots) out[b.id] = room ? attentionOf(room, b) : (b.primary ? 'all' : 'mentions');
        return out;
    });
    const [busy, setBusy] = useState(false);
    const [notice, setNotice] = useState('');
    const save = async () => {
        if (!members.length) { setNotice('至少选一个机器人'); return; }
        setBusy(true);
        try {
            const att: Record<string, string> = {};
            for (const m of members) att[m] = attention[m];
            onDone(await api.saveRoom(room?.id || '', name.trim() || '群聊', members, att));
        } catch (e) {
            setNotice(e instanceof Error && e.message ? e.message : '保存失败');
        } finally {
            setBusy(false);
        }
    };
    return (
        <div className="bot-rooms__editor" data-testid="bot-room-editor">
            <label className="custom-dialog__label">群名
                <input data-testid="bot-room-name" maxLength={60} value={name} placeholder="群聊" onChange={e => setName(e.target.value)} />
            </label>
            <p className="bot-rooms__hint">成员与“听的程度”：每条都听＝每句话都会回应；被@才听＝只有你 @ 它时才回应；不听＝暂时静音。</p>
            <ul className="bot-rooms__members">
                {bots.map(b => (
                    <li key={b.id}>
                        <label>
                            <input type="checkbox" data-testid={`bot-room-member-${b.id}`} checked={members.includes(b.id)}
                                onChange={e => setMembers(prev => e.target.checked ? [...prev, b.id] : prev.filter(x => x !== b.id))} />
                            {b.title}{b.primary ? '（主）' : ''}
                        </label>
                        <select data-testid={`bot-room-attention-${b.id}`} value={attention[b.id]} disabled={!members.includes(b.id)}
                            onChange={e => setAttention(prev => ({ ...prev, [b.id]: e.target.value as Attention }))}>
                            {(Object.keys(ATTENTION_LABEL) as Attention[]).map(k => <option key={k} value={k}>{ATTENTION_LABEL[k]}</option>)}
                        </select>
                    </li>
                ))}
            </ul>
            {notice ? <p className="custom-dialog__notice" role="alert">{notice}</p> : null}
            <div className="bot-rooms__editor-actions">
                <button type="button" className="btn-secondary" disabled={busy} onClick={() => onDone(null)}>取消</button>
                <button type="button" className="btn-primary" data-testid="bot-room-save" disabled={busy} onClick={() => { void save(); }}>保存</button>
            </div>
        </div>
    );
}

function RoomChat({ room, bots, api, pollMs }: { room: Room; bots: BotRef[]; api: CollabApi; pollMs: number }) {
    const [items, setItems] = useState<SaidItem[]>([]);
    const [draft, setDraft] = useState('');
    const [caret, setCaret] = useState(0);
    const [woken, setWoken] = useState<string[]>([]);
    const [sending, setSending] = useState(false);
    const [notice, setNotice] = useState('');
    const inputRef = useRef<HTMLTextAreaElement>(null);
    const members = room.members.map(id => bots.find(b => b.id === id) || { id, title: id });

    useEffect(() => {
        let live = true;
        const tick = async () => {
            try { const s = await api.said(room.id); if (live) setItems(s || []); } catch { /* keep the last view */ }
        };
        void tick();
        const timer = window.setInterval(() => { void tick(); }, pollMs);
        return () => { live = false; window.clearInterval(timer); };
    }, [api, room.id, pollMs]);

    const mq = mentionQuery(draft, caret);
    const candidates = mq ? mentionCandidates(members, mq.query) : [];
    const answered = answeredSinceLast(items);
    const pick = (bot: BotRef) => {
        if (!mq) return;
        const next = applyMention(draft, mq.start, caret, bot);
        setDraft(next.text);
        setCaret(next.caret);
        requestAnimationFrame(() => { inputRef.current?.focus(); inputRef.current?.setSelectionRange(next.caret, next.caret); });
    };
    const send = async () => {
        const text = draft.trim();
        if (!text || sending) return;
        setSending(true);
        setNotice('');
        try {
            const out = await api.postRoom(room.id, text);
            setDraft('');
            setCaret(0);
            setWoken(out.woken || []);
            setItems(prev => [...prev, out.message]);
        } catch (e) {
            setNotice(e instanceof Error && e.message ? e.message : '发送失败');
        } finally {
            setSending(false);
        }
    };
    return (
        <div className="bot-room-chat" data-testid="bot-room-chat">
            <div className="bot-room-chat__log" data-testid="bot-room-log">
                {items.length === 0 ? <p>还没有消息。用 @名字 点名某个机器人。</p> : null}
                {items.map(it => (
                    <div key={it.id} className={`bot-room-chat__msg${it.from === 'human' ? ' is-me' : ''}`} data-testid={`bot-room-msg-${it.id}`}>
                        <strong>{botName(bots, it.from)}</strong>
                        <span>{it.text}</span>
                    </div>
                ))}
            </div>
            <p className="bot-room-chat__answered" data-testid="bot-room-answered">
                {answered.length ? `已回答：${answered.map(id => botName(bots, id)).join('、')}`
                    : woken.length ? `等待回答：${woken.map(id => botName(bots, id)).join('、')}` : ''}
            </p>
            {candidates.length ? (
                <ul className="bot-room-chat__mentions" role="listbox" data-testid="bot-room-mentions">
                    {candidates.map(b => (
                        <li key={b.id}><button type="button" role="option" aria-selected={false} data-testid={`bot-room-mention-${b.id}`} onMouseDown={e => { e.preventDefault(); pick(b); }}>@{b.title}</button></li>
                    ))}
                </ul>
            ) : null}
            <div className="bot-room-chat__form">
                <textarea ref={inputRef} data-testid="bot-room-input" rows={2} value={draft} placeholder="说点什么，@ 可以点名机器人"
                    onChange={e => { setDraft(e.target.value); setCaret(e.target.selectionStart ?? e.target.value.length); }}
                    onSelect={e => setCaret((e.target as HTMLTextAreaElement).selectionStart ?? 0)}
                    onKeyDown={e => {
                        if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                            e.preventDefault();
                            if (candidates.length) pick(candidates[0]); else void send();
                        }
                    }} />
                <button type="button" className="btn-primary" data-testid="bot-room-send" disabled={sending || !draft.trim()} onClick={() => { void send(); }}>发送</button>
            </div>
            {notice ? <p className="custom-dialog__notice" role="alert">{notice}</p> : null}
        </div>
    );
}
