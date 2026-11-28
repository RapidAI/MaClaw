import { useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { useSafeBackdropDismiss } from '../../../hooks/useSafeBackdropDismiss';
import { useModalFocusTrap } from '../../../hooks/useModalFocusTrap';
import { isChineseLang, intlLocale } from '../../../i18n/langSelect';
import { tailText } from '../../../i18n/mnTail';
import {
    answeredSinceLast, applyMention, ATTENTION_LABEL, attentionOf, botName, defaultCollabApi, mentionCandidates, mentionQuery,
    type Attention, type BotRef, type CollabApi, type Room, type SaidItem,
} from './botCollab';

const ROOM_POLL_MS = 4000;

/* Small stroke icons (feather-style) so the dialog reads as chrome, not text. */
function Icon({ size = 14, children }: { size?: number; children: ReactNode }) {
    return (
        <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8"
            strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">{children}</svg>
    );
}
const IconUsers = ({ size = 18 }: { size?: number }) => (
    <Icon size={size}>
        <path d="M17 21v-2a4 4 0 0 0-4-4H7a4 4 0 0 0-4 4v2" /><circle cx="10" cy="7" r="4" />
        <path d="M23 21v-2a4 4 0 0 0-3-3.87" /><path d="M16 3.13a4 4 0 0 1 0 7.75" />
    </Icon>
);
const IconGear = () => (
    <Icon>
        <circle cx="12" cy="12" r="3" />
        <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 0 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 0 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 0 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 0 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 0 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33 1.65 1.65 0 0 0 1-1.51V3a2 2 0 0 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 0 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82 1.65 1.65 0 0 0 1.51 1H21a2 2 0 0 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </Icon>
);
const IconTrash = () => (
    <Icon>
        <polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" />
        <line x1="10" y1="11" x2="10" y2="17" /><line x1="14" y1="11" x2="14" y2="17" />
    </Icon>
);
const IconPlus = () => (
    <Icon><line x1="12" y1="5" x2="12" y2="19" /><line x1="5" y1="12" x2="19" y2="12" /></Icon>
);
const IconSend = () => (
    <Icon><line x1="22" y1="2" x2="11" y2="13" /><polygon points="22 2 15 22 11 13 2 9 22 2" /></Icon>
);
const IconClose = () => (
    <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
        <path d="M3.2 3.2 10.8 10.8M10.8 3.2 3.2 10.8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
    </svg>
);

/* Deterministic avatar colors: stable per bot id in both list and chat. */
const AVATAR_COLORS = ['#4f7cf0', '#8b5cf6', '#0ea5e9', '#10b981', '#f59e0b', '#ef5350', '#ec4899', '#14b8a6'];
function avatarColor(id: string): string {
    let h = 0;
    for (let i = 0; i < id.length; i++) h = (h * 31 + id.charCodeAt(i)) >>> 0;
    return AVATAR_COLORS[h % AVATAR_COLORS.length];
}
function initialsOf(name: string): string {
    const chars = Array.from((name || '').trim());
    if (!chars.length) return '?';
    // Latin: leading letters. CJK: the tail reads more naturally (小A → 小A).
    return /[\u4e00-\u9fff]/.test(chars[0]) ? chars.slice(-2).join('') : chars.slice(0, 2).join('').toUpperCase();
}
function MemberAvatar({ bot, size = 26 }: { bot: BotRef; size?: number }) {
    return (
        // Decorative: the neighbouring name already identifies the speaker.
        <span aria-hidden="true" className="bot-rooms__avatar" style={{ width: size, height: size, background: avatarColor(bot.id), fontSize: Math.round(size * 0.38) }}>
            {initialsOf(bot.title)}
        </span>
    );
}
function clockText(iso: unknown, lang: string): string {
    const t = Date.parse(String(iso || ''));
    if (!Number.isFinite(t)) return '';
    const d = new Date(t);
    const hh = String(d.getHours()).padStart(2, '0');
    const mm = String(d.getMinutes()).padStart(2, '0');
    if (new Date().toDateString() === d.toDateString()) return `${hh}:${mm}`;
    const mo = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    return isChineseLang(lang) ? `${mo}-${day} ${hh}:${mm}` : d.toLocaleDateString(intlLocale(lang), { month: 'short', day: 'numeric' }) + ` ${hh}:${mm}`;
}

/** Group rooms: list, create/edit (members, attention), and a room chat with @mentions. */
export function BotRoomsDialog({ bots, onClose, api = defaultCollabApi, pollMs = ROOM_POLL_MS, lang = 'zh-Hans' }: {
    bots: BotRef[];
    onClose: () => void;
    api?: CollabApi;
    pollMs?: number;
    lang?: string;
}) {
    const titleId = useId();
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const [rooms, setRooms] = useState<Room[]>([]);
    const [openId, setOpenId] = useState('');
    const [editing, setEditing] = useState<Room | 'new' | null>(null);
    const [notice, setNotice] = useState('');
    // Two-step delete: the first click arms the row's button, the second one
    // within the window really deletes, and the arm expires on its own so a
    // stray click can never take a room down unconfirmed.
    const [deleteArmedId, setDeleteArmedId] = useState('');
    const deleteArmRef = useRef<{ id: string; timer: number } | null>(null);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

    useEffect(() => () => {
        if (deleteArmRef.current) window.clearTimeout(deleteArmRef.current.timer);
    }, []);

    const armDelete = (id: string, run: () => void) => {
        const held = deleteArmRef.current;
        if (held) window.clearTimeout(held.timer);
        if (held?.id === id) {
            deleteArmRef.current = null;
            setDeleteArmedId('');
            run();
            return;
        }
        setDeleteArmedId(id);
        const timer = window.setTimeout(() => { deleteArmRef.current = null; setDeleteArmedId(''); }, 3000);
        deleteArmRef.current = { id, timer };
    };

    const reload = async () => {
        try {
            setRooms((await api.rooms()) || []);
        } catch (e) {
            setNotice(e instanceof Error && e.message ? e.message : tailText(lang, 'Could not load group chats', '读取群聊失败'));
        }
    };
    useEffect(() => { void reload(); /* eslint-disable-next-line react-hooks/exhaustive-deps */ }, []);

    const open = rooms.find(r => r.id === openId) || null;
    const zh = isChineseLang(lang);
    // Escape walks back out (editor → list → close) instead of dropping everything.
    const handleEscape = () => {
        if (editing) setEditing(null);
        else if (open) setOpenId('');
        else onClose();
    };
    const { onKeyDown } = useModalFocusTrap(dialogRef, { onEscape: handleEscape });

    const title = editing ? (editing === 'new'
        ? tailText(lang, 'New group chat', '新建群聊')
        : tailText(lang, 'Group settings', '群设置'))
        : open ? open.name
            : tailText(lang, 'Group chats', '群聊');
    const subtitle = editing === 'new' ? tailText(lang, 'Pick the bots and how closely they listen', '选择成员，以及它们听得多仔细')
        : editing ? editing.name
            : open ? tailText(lang, `${open.members.length} members`, `${open.members.length} 名成员`)
                : rooms.length ? tailText(lang, `${rooms.length} group chat${rooms.length > 1 ? 's' : ''}`, `${rooms.length} 个群聊`)
                    : '';

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-edit-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div className="modal-content custom-dialog bot-edit-dialog bot-rooms-dialog" ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} data-testid="bot-rooms-dialog" tabIndex={-1} {...dialogProps}>
                <div className="modal-header custom-dialog__header">
                    <div className="custom-dialog__heading">
                        <span className="bot-rooms__mark" aria-hidden="true"><IconUsers /></span>
                        <span className="bot-rooms__heading-text">
                            <h3 id={titleId} className="custom-dialog__title">{title}</h3>
                            {subtitle ? <span className="bot-rooms__subtitle">{subtitle}</span> : null}
                        </span>
                    </div>
                    <button type="button" className="btn-close custom-dialog__close" aria-label={tailText(lang, 'Close', '关闭')} onClick={onClose}>
                        <IconClose />
                    </button>
                </div>
                <div className="modal-body custom-dialog__body">
                    {editing ? (
                        <RoomEditor room={editing === 'new' ? null : editing} bots={bots} api={api} lang={lang}
                            onDone={async saved => { setEditing(null); await reload(); if (saved) setOpenId(saved.id); }} />
                    ) : open ? (
                        // key per room: a room switch remounts, so the transcript,
                        // the half-typed draft and the woken list never leak across rooms.
                        <RoomChat key={open.id} room={open} bots={bots} api={api} pollMs={pollMs} lang={lang} />
                    ) : (
                        <>
                            {rooms.length === 0 ? (
                                <div className="bot-rooms__empty" data-testid="bot-rooms-empty">
                                    <span className="bot-rooms__empty-icon" aria-hidden="true"><IconUsers size={24} /></span>
                                    <p className="bot-rooms__empty-title">{tailText(lang, 'No group chats yet', '还没有群聊')}</p>
                                    <p className="bot-rooms__empty-hint">{tailText(lang, 'Create one and bring a few bots in to talk together.', '建一个群，把几个机器人拉进来一起讨论。')}</p>
                                    <button type="button" className="btn-primary bot-rooms__empty-cta" data-testid="bot-room-empty-new" onClick={() => setEditing('new')}>
                                        <IconPlus />{tailText(lang, 'New group chat', '新建群聊')}
                                    </button>
                                </div>
                            ) : null}
                            <ul className="bot-rooms__list">
                                {rooms.map(r => {
                                    const names = r.members.map(m => botName(bots, m, lang));
                                    const stack = r.members.slice(0, 3).map(id => ({ id, title: bots.find(b => b.id === id)?.title || id }));
                                    return (
                                        <li key={r.id} className="bot-rooms__item">
                                            <button type="button" className="bot-rooms__open" data-testid={`bot-room-open-${r.id}`} onClick={() => setOpenId(r.id)}>
                                                <span className="bot-rooms__stack" aria-hidden="true">
                                                    {stack.map(m => (
                                                        <span key={m.id} className="bot-rooms__stack-chip" style={{ background: avatarColor(m.id) }}>{initialsOf(m.title)}</span>
                                                    ))}
                                                    {r.members.length > 3 ? <span className="bot-rooms__stack-chip bot-rooms__stack-chip--more">+{r.members.length - 3}</span> : null}
                                                </span>
                                                <span className="bot-rooms__meta">
                                                    <strong className="bot-rooms__name">{r.name}</strong>
                                                    <small className="bot-rooms__members-line">
                                                        {names.join(zh ? '、' : ', ')}
                                                    </small>
                                                </span>
                                                <span className="bot-rooms__count" aria-hidden="true" title={tailText(lang, `${r.members.length} members`, `${r.members.length} 名成员`)}>{r.members.length}</span>
                                            </button>
                                            <span className="bot-rooms__actions">
                                                <button type="button" className="bot-rooms__action" data-testid={`bot-room-edit-${r.id}`} onClick={() => setEditing(r)}>
                                                    <IconGear />{tailText(lang, 'Settings', '设置')}
                                                </button>
                                                <button type="button"
                                                    className={'bot-rooms__action bot-rooms__action--danger' + (deleteArmedId === r.id ? ' bot-rooms__action--armed' : '')}
                                                    aria-live="polite"
                                                    data-testid={`bot-room-delete-${r.id}`}
                                                    onClick={() => armDelete(r.id, () => {
                                                        void (async () => {
                                                            try { await api.deleteRoom(r.id); await reload(); } catch (e) { setNotice(e instanceof Error ? e.message : tailText(lang, 'Delete failed', '删除失败')); }
                                                        })();
                                                    })}>
                                                    <IconTrash />{deleteArmedId === r.id ? tailText(lang, 'Confirm delete', '确认删除') : tailText(lang, 'Delete', '删除')}
                                                </button>
                                            </span>
                                        </li>
                                    );
                                })}
                            </ul>
                        </>
                    )}
                    {notice ? <p className="custom-dialog__notice" role="alert">{notice}</p> : null}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    {open && !editing ? <button type="button" className="btn-secondary bot-room-back" data-testid="bot-room-back" onClick={() => setOpenId('')}>{tailText(lang, 'Back to list', '返回列表')}</button> : null}
                    {open && !editing ? <button type="button" className="btn-secondary" onClick={() => setEditing(open)}>{tailText(lang, 'Group settings', '群设置')}</button> : null}
                    {!open && !editing && rooms.length > 0 ? <button type="button" className="btn-primary bot-rooms__new" data-testid="bot-room-new" onClick={() => setEditing('new')}><IconPlus />{tailText(lang, 'New group chat', '新建群聊')}</button> : null}
                    <button type="button" className="btn-secondary" onClick={onClose}>{tailText(lang, 'Close', '关闭')}</button>
                </div>
            </div>
        </div>
    );
}

function RoomEditor({ room, bots, api, onDone, lang }: { room: Room | null; bots: BotRef[]; api: CollabApi; onDone: (saved: Room | null) => void; lang: string }) {
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
        if (!members.length) { setNotice(tailText(lang, 'Select at least one bot', '至少选一个机器人')); return; }
        setBusy(true);
        try {
            const att: Record<string, string> = {};
            for (const m of members) att[m] = attention[m];
            onDone(await api.saveRoom(room?.id || '', name.trim() || tailText(lang, 'Group chat', '群聊'), members, att));
        } catch (e) {
            setNotice(e instanceof Error && e.message ? e.message : tailText(lang, 'Could not save', '保存失败'));
        } finally {
            setBusy(false);
        }
    };
    return (
        <div className="bot-rooms__editor" data-testid="bot-room-editor">
            <label className="bot-rooms__field">
                <span className="bot-rooms__field-label">{tailText(lang, 'Group name', '群名')}</span>
                <input className="bot-rooms__name-input" data-testid="bot-room-name" maxLength={60} value={name} placeholder={tailText(lang, 'Group chat', '群聊')} onChange={e => setName(e.target.value)} />
            </label>
            <p className="bot-rooms__hint">{tailText(lang, 'Members and how closely they listen: everyone hears every line; mention-only responds when you @ them; mute stays quiet.', '成员与“听的程度”：每条都听＝每句话都会回应；被@才听＝只有你 @ 它时才回应；不听＝暂时静音。')}</p>
            <ul className="bot-rooms__members">
                {bots.map(b => {
                    const on = members.includes(b.id);
                    return (
                        <li key={b.id} className={on ? 'is-in' : 'is-out'}>
                            <label className="bot-rooms__member-main">
                                <input type="checkbox" data-testid={`bot-room-member-${b.id}`} checked={on}
                                    onChange={e => setMembers(prev => e.target.checked ? [...prev, b.id] : prev.filter(x => x !== b.id))} />
                                <MemberAvatar bot={b} size={24} />
                                <span className="bot-rooms__member-name">{b.title}{b.primary ? tailText(lang, ' (primary)', '（主）') : ''}</span>
                            </label>
                            <select data-testid={`bot-room-attention-${b.id}`} value={attention[b.id]} disabled={!on} aria-label={tailText(lang, `How ${b.title} listens`, `${b.title} 听得多仔细`)}
                                onChange={e => setAttention(prev => ({ ...prev, [b.id]: e.target.value as Attention }))}>
                                {(Object.keys(ATTENTION_LABEL) as Attention[]).map(k => <option key={k} value={k}>{tailText(lang, k === 'all' ? 'Hears every line' : k === 'mentions' ? 'Hears only @mentions' : 'Muted', ATTENTION_LABEL[k])}</option>)}
                            </select>
                        </li>
                    );
                })}
            </ul>
            {notice ? <p className="custom-dialog__notice" role="alert">{notice}</p> : null}
            <div className="bot-rooms__editor-actions">
                <button type="button" className="btn-secondary" disabled={busy} onClick={() => onDone(null)}>{tailText(lang, 'Cancel', '取消')}</button>
                <button type="button" className="btn-primary" data-testid="bot-room-save" disabled={busy} onClick={() => { void save(); }}>{tailText(lang, 'Save', '保存')}</button>
            </div>
        </div>
    );
}

function RoomChat({ room, bots, api, pollMs, lang }: { room: Room; bots: BotRef[]; api: CollabApi; pollMs: number; lang: string }) {
    const [items, setItems] = useState<SaidItem[]>([]);
    const [draft, setDraft] = useState('');
    const [caret, setCaret] = useState(0);
    const [woken, setWoken] = useState<string[]>([]);
    const [sending, setSending] = useState(false);
    const [notice, setNotice] = useState('');
    const inputRef = useRef<HTMLTextAreaElement>(null);
    const logRef = useRef<HTMLDivElement>(null);
    // Stick-to-bottom: the log only pins while the reader is near the bottom,
    // so a 4s poll never yanks someone who scrolled up to read history.
    const stickRef = useRef(true);
    const members = room.members.map(id => bots.find(b => b.id === id) || { id, title: id });

    useEffect(() => {
        let live = true;
        const tick = async () => {
            try {
                const s = (await api.said(room.id)) || [];
                // The room log is append-only: same length + same last id means
                // nothing new, so skip the state write instead of re-rendering
                // the whole transcript every poll.
                if (live) setItems(prev => {
                    const last = prev.length - 1;
                    if (last >= 0 && prev.length === s.length && prev[last]?.id === s[last]?.id) return prev;
                    return s;
                });
            } catch { /* keep the last view */ }
        };
        void tick();
        const timer = window.setInterval(() => { void tick(); }, pollMs);
        return () => { live = false; window.clearInterval(timer); };
    }, [api, room.id, pollMs]);

    // The composer takes focus when a chat opens, like every chat surface.
    useEffect(() => {
        inputRef.current?.focus({ preventScroll: true });
    }, []);

    // Fresh messages pin to the bottom only while the reader stays near it.
    useEffect(() => {
        if (!stickRef.current) return;
        const el = logRef.current;
        if (el) el.scrollTop = el.scrollHeight;
    }, [items]);

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
    // The composer grows with its content (clamped by CSS max-height) and
    // snaps back to the resting height once the message goes out.
    const autoGrow = () => {
        const el = inputRef.current;
        if (!el) return;
        el.style.height = 'auto';
        el.style.height = Math.min(el.scrollHeight, 120) + 'px';
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
            if (inputRef.current) inputRef.current.style.height = '';
            setWoken(out.woken || []);
            // The message the user just wrote must be visible even if they had
            // scrolled up: re-enable the pin before it lands.
            stickRef.current = true;
            setItems(prev => [...prev, out.message]);
        } catch (e) {
            setNotice(e instanceof Error && e.message ? e.message : tailText(lang, 'Could not send', '发送失败'));
        } finally {
            setSending(false);
        }
    };
    return (
        <div className="bot-room-chat" data-testid="bot-room-chat">
            <div className="bot-room-chat__log" data-testid="bot-room-log" ref={logRef}
                onScroll={e => {
                    const el = e.currentTarget;
                    stickRef.current = el.scrollHeight - el.scrollTop - el.clientHeight < 80;
                }}>
                {items.length === 0 ? <p className="bot-room-chat__log-empty">{tailText(lang, 'No messages yet. Use @name to call a bot.', '还没有消息。用 @名字 点名某个机器人。')}</p> : null}
                {items.map(it => {
                    const me = it.from === 'human';
                    const name = botName(bots, it.from, lang);
                    const time = clockText(it.created_at, lang);
                    return (
                        <div key={it.id} className={`bot-room-chat__msg${me ? ' is-me' : ''}`} data-testid={`bot-room-msg-${it.id}`}>
                            <MemberAvatar bot={{ id: it.from, title: name }} />
                            <div className="bot-room-chat__bubble">
                                <span className="bot-room-chat__meta">
                                    <strong>{name}</strong>
                                    {time ? <time>{time}</time> : null}
                                </span>
                                <span className="bot-room-chat__text">{it.text}</span>
                            </div>
                        </div>
                    );
                })}
            </div>
            <p className="bot-room-chat__answered" data-testid="bot-room-answered">
                {answered.length ? tailText(lang, `Answered: ${answered.map(id => botName(bots, id, lang)).join(isChineseLang(lang) ? '、' : ', ')}`, `已回答：${answered.map(id => botName(bots, id, lang)).join('、')}`)
                    : woken.length ? tailText(lang, `Waiting for an answer: ${woken.map(id => botName(bots, id, lang)).join(isChineseLang(lang) ? '、' : ', ')}`, `等待回答：${woken.map(id => botName(bots, id, lang)).join('、')}`) : ''}
            </p>
            {candidates.length ? (
                <ul className="bot-room-chat__mentions" role="listbox" data-testid="bot-room-mentions">
                    {candidates.map(b => (
                        <li key={b.id}>
                            <button type="button" role="option" aria-selected={false} data-testid={`bot-room-mention-${b.id}`} onMouseDown={e => { e.preventDefault(); pick(b); }}>
                                <MemberAvatar bot={b} size={20} />
                                <span>@{b.title}</span>
                            </button>
                        </li>
                    ))}
                </ul>
            ) : null}
            <div className="bot-room-chat__form">
                <textarea ref={inputRef} data-testid="bot-room-input" rows={2} value={draft} placeholder={tailText(lang, 'Say something. @ calls a bot.', '说点什么，@ 可以点名机器人')}
                    onChange={e => { setDraft(e.target.value); setCaret(e.target.selectionStart ?? e.target.value.length); autoGrow(); }}
                    onSelect={e => setCaret((e.target as HTMLTextAreaElement).selectionStart ?? 0)}
                    onKeyDown={e => {
                        if (e.key === 'Enter' && !e.shiftKey && !e.nativeEvent.isComposing) {
                            e.preventDefault();
                            if (candidates.length) pick(candidates[0]); else void send();
                        }
                    }} />
                <button type="button" className="btn-primary bot-room-chat__send" data-testid="bot-room-send" disabled={sending || !draft.trim()} onClick={() => { void send(); }}>
                    <IconSend />{tailText(lang, 'Send', '发送')}
                </button>
            </div>
            {notice ? <p className="custom-dialog__notice" role="alert">{notice}</p> : null}
        </div>
    );
}
