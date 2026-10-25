import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { MouseEvent as ReactMouseEvent, ReactNode } from 'react';
import { createPortal } from 'react-dom';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { usePortalThemeAttributes } from '../../hooks/usePortalThemeAttributes';
import { BrowserOpenURL } from '../../../wailsjs/runtime';
import { useDialog } from '../CustomDialog';
import { IconCheck, IconCross, IconPaperclip, IconRefresh } from '../ai/WorkbenchIcons';
import { sanitizeEmailHtml } from '../../utils/emailHtml';
import {
    MailboxBrowseDelete,
    MailboxBrowseMarkRead,
    MailboxBrowseMessages,
    MailboxBrowseRead,
    MailboxBrowseStart,
} from '../../../wailsjs/go/main/App';

// Chunk limits per server call. Keep in sync with mailbox.DeleteUIDCap and
// mailbox.OrganizeMaxUIDs on the Go side; the batch bar splits larger
// selections into sequential calls.
const DELETE_CHUNK = 100;
const MARK_CHUNK = 200;

function chunkUIDs(uids: number[], size: number): number[][] {
    const out: number[][] = [];
    for (let i = 0; i < uids.length; i += size) {
        out.push(uids.slice(i, i + size));
    }
    return out;
}

type FolderRow = { name: string; special?: string };
type MessageRow = {
    uid: number;
    from?: string;
    to?: string;
    subject?: string;
    date?: string;
    seen?: boolean;
    has_attachment?: boolean;
};
type MessageView = MessageRow & { body?: string; html?: string; truncated?: boolean; attachments?: string[] };
type StartPage = {
    folders?: FolderRow[];
    box?: string;
    messages?: MessageRow[];
    unseen?: Record<string, number>;
    error?: string;
};

function textFor(lang: string, en: string, hans: string, hant: string) {
    if (lang === 'en') return en;
    if (lang === 'zh-Hant') return hant;
    return hans;
}

function errText(err: unknown) {
    if (err && typeof err === 'object' && 'message' in err && typeof (err as { message: unknown }).message === 'string') {
        return (err as { message: string }).message;
    }
    return String(err || '');
}

/* ------------------------------------------------------------------ */
/* SVG icons (24×24 stroke, matching WorkbenchIcons conventions)       */
/* ------------------------------------------------------------------ */

type IconProps = { size?: number; className?: string };

function mailSvg({ size = 16, className, children }: IconProps & { children: ReactNode }) {
    return (
        <svg
            width={size}
            height={size}
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            strokeWidth={1.65}
            strokeLinecap="round"
            strokeLinejoin="round"
            aria-hidden="true"
            focusable="false"
            className={className}
            style={{ display: 'block', flexShrink: 0 }}
        >
            {children}
        </svg>
    );
}

const IconInbox = (p: IconProps) => mailSvg({ ...p, children: (
    <><polyline points="22 12 16 12 14 15 10 15 8 12 2 12" /><path d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z" /></>
) });

const IconSend = (p: IconProps) => mailSvg({ ...p, children: (
    <><line x1="22" y1="2" x2="11" y2="13" /><polygon points="22 2 15 22 11 13 2 9 22 2" /></>
) });

const IconDraft = (p: IconProps) => mailSvg({ ...p, children: (
    <><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z" /><polyline points="14 2 14 8 20 8" /><line x1="16" y1="13" x2="8" y2="13" /><line x1="16" y1="17" x2="8" y2="17" /></>
) });

const IconJunk = (p: IconProps) => mailSvg({ ...p, children: (
    <><path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z" /><line x1="12" y1="8" x2="12" y2="12" /><line x1="12" y1="16" x2="12.01" y2="16" /></>
) });

const IconTrash = (p: IconProps) => mailSvg({ ...p, children: (
    <><polyline points="3 6 5 6 21 6" /><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2" /></>
) });

const IconArchive = (p: IconProps) => mailSvg({ ...p, children: (
    <><polyline points="21 8 21 21 3 21 3 8" /><rect x="1" y="3" width="22" height="5" /><line x1="10" y1="12" x2="14" y2="12" /></>
) });

const IconLayers = (p: IconProps) => mailSvg({ ...p, children: (
    <><polygon points="12 2 2 7 12 12 22 7 12 2" /><polyline points="2 17 12 22 22 17" /><polyline points="2 12 12 17 22 12" /></>
) });

const IconMailRead = (p: IconProps) => mailSvg({ ...p, children: (
    <><path d="M4 4h16c1.1 0 2 .9 2 2v12c0 1.1-.9 2-2 2H4c-1.1 0-2-.9-2-2V6c0-1.1.9-2 2-2z" /><polyline points="22,6 12,13 2,6" /></>
) });

const IconMailUnread = (p: IconProps) => mailSvg({ ...p, children: (
    <><path d="M17.94 17.94A10.07 10.07 0 0 1 12 20c-7 0-11-8-11-8a18.45 18.45 0 0 1 5.06-5.94M9.9 4.24A9.12 9.12 0 0 1 12 4c7 0 11 8 11 8a18.5 18.5 0 0 1-2.16 3.19m-6.72-1.07a3 3 0 1 1-4.24-4.24" /><line x1="1" y1="1" x2="23" y2="23" /></>
) });

const FOLDER_ICONS: Record<string, (p: IconProps) => ReactNode> = {
    inbox: IconInbox,
    drafts: IconDraft,
    sent: IconSend,
    junk: IconJunk,
    trash: IconTrash,
    archive: IconArchive,
    all: IconLayers,
};

function FolderGlyph({ special }: { special?: string }) {
    const Glyph = (special && FOLDER_ICONS[special]) || IconInbox;
    return <Glyph size={15} className="mailbox-manage__folder-glyph" />;
}

/* ------------------------------------------------------------------ */
/* Formatting helpers                                                  */
/* ------------------------------------------------------------------ */

// Mail lists read best compact: time only for today, month/day this year,
// full date once another year is involved.
function formatWhen(raw?: string) {
    const value = raw?.trim() || '';
    if (!value) return '';
    const parsed = Date.parse(value);
    if (Number.isNaN(parsed)) return value;
    const date = new Date(parsed);
    const now = new Date();
    const time = date.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
    if (date.toDateString() === now.toDateString()) return time;
    const monthOptions = { month: 'short', day: 'numeric' } as const;
    if (date.getFullYear() !== now.getFullYear()) {
        return date.toLocaleDateString(undefined, { ...monthOptions, year: 'numeric' });
    }
    return `${date.toLocaleDateString(undefined, monthOptions)} ${time}`;
}

// The reading pane is where mail gets quoted, filed and replied to, so its
// header needs the full precise date — no year-dropping shortcuts here.
function formatWhenFull(raw?: string) {
    const value = raw?.trim() || '';
    if (!value) return '';
    const parsed = Date.parse(value);
    if (Number.isNaN(parsed)) return value;
    return new Date(parsed).toLocaleString(undefined, {
        year: 'numeric',
        month: '2-digit',
        day: '2-digit',
        hour: '2-digit',
        minute: '2-digit',
    });
}

// Prefer the display name in `"Name" <addr>` forms; plain addresses pass through.
function shortFrom(raw?: string) {
    const value = raw?.trim() || '';
    if (!value) return '';
    const named = /^"?([^"]+?)"?\s*<[^>]*>$/.exec(value);
    if (named?.[1]?.trim()) return named[1].trim();
    return value;
}

/** The bare address inside `"Name" <addr>`; empty when the input is only a name. */
function fromAddress(raw?: string) {
    const match = /<([^>]+)>/.exec(raw?.trim() || '');
    return match?.[1]?.trim() || '';
}

// A sender avatar chip: the first grapheme of the display name on a stable hue.
function senderHue(raw?: string) {
    let hash = 0;
    const key = raw || '';
    for (let i = 0; i < key.length; i++) hash = (hash * 31 + key.charCodeAt(i)) % 360;
    return hash;
}

function folderLabel(t: (en: string, hans: string, hant: string) => string, folder: FolderRow) {
    const role = ({
        inbox: t('Inbox', '收件箱', '收件匣'),
        drafts: t('Drafts', '草稿', '草稿'),
        sent: t('Sent', '已发送', '已傳送'),
        junk: t('Junk', '垃圾邮件', '垃圾郵件'),
        trash: t('Trash', '已删除', '已刪除'),
        archive: t('Archive', '归档', '封存'),
        all: t('All mail', '全部邮件', '全部郵件'),
    } as Record<string, string>)[folder.special || ''];
    return role || folder.name;
}

// Plain text reads as Markdown only when it carries Markdown shapes. A
// marker-free note stays pre-wrapped text: collapsing its single newlines
// into spaces would scramble a plain-text letter.
const MARKDOWN_HINT = /(^|\n)[ ]{0,3}(?:#{1,6}\s|[-*+]\s|\d+[.)]\s|>\s|```)|\*\*[^*\n]+\*\*|`[^`\n]+`|\[[^\]\n]+\]\([^)\n]+\)|(^\|\S.+\|$)/m;

function looksLikeMarkdown(text: string): boolean {
    return MARKDOWN_HINT.test(text || '');
}

/* ------------------------------------------------------------------ */
/* The dialog                                                          */
/* ------------------------------------------------------------------ */

export function MailboxManageDialog({
    lang,
    account,
    onClose,
}: {
    lang: string;
    account: { id: string; name: string; address: string };
    onClose: () => void;
}) {
    const t = (en: string, hans: string, hant: string) => textFor(lang, en, hans, hant);
    const { showConfirm } = useDialog();
    const portalThemeAttributes = usePortalThemeAttributes();
    const closeRef = useRef<HTMLButtonElement>(null);
    const onCloseRef = useRef(onClose);
    onCloseRef.current = onClose;
    const folderGen = useRef(0);
    const messageGen = useRef(0);
    const readGen = useRef(0);
    const [folders, setFolders] = useState<FolderRow[]>([]);
    const [box, setBox] = useState('');
    const [unseen, setUnseen] = useState<Record<string, number>>({});
    const [messages, setMessages] = useState<MessageRow[]>([]);
    const [reading, setReading] = useState<MessageView | null>(null);
    const [selectedUid, setSelectedUid] = useState<number | null>(null);
    const [error, setError] = useState('');
    const [loading, setLoading] = useState<'folders' | 'messages' | 'read' | ''>('folders');
    const [startNonce, setStartNonce] = useState(0);
    const [actionUid, setActionUid] = useState<number | null>(null);
    const [batchBusy, setBatchBusy] = useState(false);
    // Selected message uids of the current folder. Cleared when the folder or
    // the account changes; intersected with the fresh list on refresh.
    const [selection, setSelection] = useState<Set<number>>(new Set());
    // Rendering controls. `plainView` flips the reading pane between the
    // sanitized HTML and the plain text; `showRemote` re-renders with remote
    // pictures after the user asks for them.
    const [plainView, setPlainView] = useState(false);
    const [showRemote, setShowRemote] = useState(false);

    useEffect(() => {
        closeRef.current?.focus();
        const onKey = (event: KeyboardEvent) => {
            if (event.key === 'Escape') onCloseRef.current();
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, []);

    useEffect(() => {
        const mine = ++folderGen.current;
        messageGen.current += 1;
        readGen.current += 1;
        setError('');
        setLoading('folders');
        setReading(null);
        setSelectedUid(null);
        setFolders([]);
        setMessages([]);
        setUnseen({});
        setSelection(new Set());
        void MailboxBrowseStart(account.id).then(page => {
            if (mine !== folderGen.current) return;
            const start = (page || {}) as StartPage;
            const rows = Array.isArray(start.folders) ? start.folders : [];
            setFolders(rows);
            setBox(start.box || '');
            setMessages(Array.isArray(start.messages) ? start.messages : []);
            setUnseen(start.unseen && typeof start.unseen === 'object' ? start.unseen : {});
            setError(start.error || '');
            setLoading('');
        }).catch((err: unknown) => {
            if (mine !== folderGen.current) return;
            setError(errText(err));
            setLoading('');
        });
        return () => {
            folderGen.current += 1;
        };
    }, [account.id, startNonce]);

    const adjustUnseen = useCallback((folder: string, delta: number) => {
        if (!folder) return;
        setUnseen(prev => {
            const current = prev[folder];
            if (current === undefined) return prev;
            const next = Math.max(0, current + delta);
            if (next === current) return prev;
            return { ...prev, [folder]: next };
        });
    }, []);

    const selectFolder = (name: string) => {
        // Rapid switches stay safe: every bump of messageGen retires the
        // previous folder's in-flight response, so no loading guard is needed.
        if (name === box && !error) return;
        readGen.current += 1;
        const mine = ++messageGen.current;
        setBox(name);
        setError('');
        setLoading('messages');
        setReading(null);
        setSelectedUid(null);
        setPlainView(false);
        setShowRemote(false);
        setSelection(new Set());
        setMessages([]);
        void MailboxBrowseMessages(account.id, name, 30).then(list => {
            if (mine !== messageGen.current) return;
            setMessages(Array.isArray(list) ? list as MessageRow[] : []);
            setLoading('');
        }).catch((err: unknown) => {
            if (mine !== messageGen.current) return;
            setMessages([]);
            setError(errText(err));
            setLoading('');
        });
    };

    // Mark one message read or unread and keep the list, badge and reading
    // pane honest. The optimistic flip lands first; the server call just
    // decides whether it stays. An automatic mark (from opening a mail) fails
    // silently — POP3 mailboxes have no server-side seen flag at all, and a
    // transient IMAP failure resyncs on the next reload — while a manual
    // toggle reports the rollback.
    const applySeen = async (uid: number, seen: boolean, silent = false) => {
        const folder = box;
        const wasSeen = messages.find(m => m.uid === uid)?.seen ?? reading?.seen;
        if (folder && wasSeen === !seen) {
            setMessages(prev => prev.map(m => (m.uid === uid ? { ...m, seen } : m)));
            setReading(prev => (prev && prev.uid === uid ? { ...prev, seen } : prev));
            adjustUnseen(folder, seen ? -1 : 1);
        }
        try {
            await MailboxBrowseMarkRead(account.id, folder, [uid], seen);
        } catch {
            setMessages(prev => prev.map(m => (m.uid === uid ? { ...m, seen: !seen } : m)));
            setReading(prev => (prev && prev.uid === uid ? { ...prev, seen: !seen } : prev));
            adjustUnseen(folder, seen ? 1 : -1);
            if (!silent) {
                setError(t('Could not reach the server; the change was rolled back.', '无法连接服务器，本次修改已回退。', '無法連接伺服器，本次修改已回退。'));
            }
        }
    };

    const openMessage = (uid: number) => {
        const mine = ++readGen.current;
        setError('');
        setSelectedUid(uid);
        setReading(null);
        setPlainView(false);
        setShowRemote(false);
        setLoading('read');
        void MailboxBrowseRead(account.id, box, uid).then(view => {
            if (mine !== readGen.current) return;
            const next = view as MessageView;
            setReading(next);
            setLoading('');
            // Reading a message marks it read, like every mainstream client.
            if (!next.seen) void applySeen(uid, true, true);
        }).catch((err: unknown) => {
            if (mine !== readGen.current) return;
            setError(errText(err));
            setLoading('');
        });
    };

    const toggleSeen = async (message: MessageRow) => {
        if (actionUid !== null || batchBusy) return;
        setActionUid(message.uid);
        try {
            await applySeen(message.uid, !message.seen);
        } finally {
            setActionUid(null);
        }
    };

    /* -------------------------------------------------------------- */
    /* Multi-select and batch operations                                */
    /* -------------------------------------------------------------- */

    const toggleSelect = (uid: number) => {
        setSelection(prev => {
            const next = new Set(prev);
            if (next.has(uid)) next.delete(uid);
            else next.add(uid);
            return next;
        });
    };

    const allSelected = messages.length > 0 && messages.every(m => selection.has(m.uid));

    const toggleSelectAll = () => {
        setSelection(allSelected ? new Set() : new Set(messages.map(m => m.uid)));
    };

    // Batch delete moves every selected message to the trash in chunks of
    // DELETE_CHUNK. Chunks apply sequentially; the first failure stops the
    // run and the already-moved part stays deleted (reported, not rolled back).
    const deleteSelected = async () => {
        if (batchBusy) return;
        const rows = messages.filter(m => selection.has(m.uid));
        if (rows.length === 0) return;
        const confirmed = await showConfirm(
            t(
                `Move ${rows.length} selected message(s) to the trash folder?`,
                `将选中的 ${rows.length} 封邮件移到已删除文件夹？`,
                `將選中的 ${rows.length} 封郵件移到已刪除資料夾？`,
            ),
            t('Delete mail', '删除邮件', '刪除郵件'),
            { confirmVariant: 'danger' },
        );
        if (!confirmed) return;
        setBatchBusy(true);
        setError('');
        try {
            const done: number[] = [];
            let failure: unknown = null;
            for (const part of chunkUIDs(rows.map(m => m.uid), DELETE_CHUNK)) {
                try {
                    await MailboxBrowseDelete(account.id, box, part);
                    done.push(...part);
                } catch (err: unknown) {
                    failure = err;
                    break;
                }
            }
            if (done.length > 0) {
                const unreadGone = rows.filter(m => done.includes(m.uid) && !m.seen).length;
                if (unreadGone > 0) adjustUnseen(box, -unreadGone);
                setMessages(prev => prev.filter(m => !done.includes(m.uid)));
                setSelection(prev => new Set([...prev].filter(uid => !done.includes(uid))));
                if (reading && done.includes(reading.uid)) {
                    readGen.current += 1;
                    setReading(null);
                    setSelectedUid(null);
                }
            }
            if (failure) {
                setError(errText(failure) + (done.length > 0
                    ? t(' Some messages were deleted.', '（前面的邮件已删除，后面的没有执行。）', '（前面的郵件已刪除，後面的沒有執行。）')
                    : ''));
            }
        } finally {
            setBatchBusy(false);
        }
    };

    // Batch read/unread flips the rows first and lets the server calls keep
    // them. Failed chunks roll back their own uids, so the pane stays honest.
    const markSelected = async (seen: boolean) => {
        if (batchBusy) return;
        const targets = messages.filter(m => selection.has(m.uid) && Boolean(m.seen) !== seen);
        if (targets.length === 0) return;
        setBatchBusy(true);
        setError('');
        const affected = targets.map(m => m.uid);
        setMessages(prev => prev.map(m => (affected.includes(m.uid) ? { ...m, seen } : m)));
        setReading(prev => (prev && affected.includes(prev.uid) ? { ...prev, seen } : prev));
        adjustUnseen(box, seen ? -affected.length : affected.length);
        const undone: number[] = [];
        try {
            for (const part of chunkUIDs(affected, MARK_CHUNK)) {
                try {
                    await MailboxBrowseMarkRead(account.id, box, part, seen);
                } catch {
                    undone.push(...part);
                }
            }
            if (undone.length > 0) {
                setMessages(prev => prev.map(m => (undone.includes(m.uid) ? { ...m, seen: !seen } : m)));
                setReading(prev => (prev && undone.includes(prev.uid) ? { ...prev, seen: !seen } : prev));
                adjustUnseen(box, seen ? undone.length : -undone.length);
                setError(t('Could not reach the server; some changes were rolled back.', '无法连接服务器，部分修改已回退。', '無法連接伺服器，部分修改已回退。'));
            }
        } finally {
            setBatchBusy(false);
        }
    };

    const deleteMessage = async (message: MessageRow) => {
        if (actionUid !== null || batchBusy) return;
        const subject = message.subject?.trim() || t('(no subject)', '（无主题）', '（無主題）');
        const confirmed = await showConfirm(
            t(
                `Move “${subject}” to the trash folder?`,
                `将「${subject}」移到已删除文件夹？`,
                `將「${subject}」移到已刪除資料夾？`,
            ),
            t('Delete mail', '删除邮件', '刪除郵件'),
            { confirmVariant: 'danger' },
        );
        if (!confirmed) return;
        setActionUid(message.uid);
        setError('');
        try {
            await MailboxBrowseDelete(account.id, box, [message.uid]);
            if (!message.seen) adjustUnseen(box, -1);
            setMessages(prev => prev.filter(m => m.uid !== message.uid));
            setSelection(prev => {
                if (!prev.has(message.uid)) return prev;
                const next = new Set(prev);
                next.delete(message.uid);
                return next;
            });
            if (selectedUid === message.uid) {
                readGen.current += 1;
                setReading(null);
                setSelectedUid(null);
            }
        } catch (err: unknown) {
            setError(errText(err));
        } finally {
            setActionUid(null);
        }
    };

    // Refresh re-syncs folders, unread badges and the current folder's list.
    // The selection keeps only the uids that survive the reload.
    const refresh = () => {
        if (loading || batchBusy) return;
        const mine = ++folderGen.current;
        setLoading('messages');
        setError('');
        void MailboxBrowseStart(account.id).then(async page => {
            if (mine !== folderGen.current) return;
            const start = (page || {}) as StartPage;
            if (Array.isArray(start.folders)) setFolders(start.folders);
            if (start.unseen && typeof start.unseen === 'object') setUnseen(start.unseen);
            const target = start.box && start.box !== box ? box : start.box || box;
            if (!target) {
                setMessages(Array.isArray(start.messages) ? start.messages : []);
                setBox(start.box || '');
                setLoading('');
                return;
            }
            try {
                const list = await MailboxBrowseMessages(account.id, target, 30);
                if (mine !== folderGen.current) return;
                const fresh = Array.isArray(list) ? list as MessageRow[] : [];
                setMessages(fresh);
                setSelection(prev => new Set([...prev].filter(uid => fresh.some(m => m.uid === uid))));
                setError('');
            } catch (err: unknown) {
                if (mine !== folderGen.current) return;
                setError(errText(err));
            }
            setLoading('');
        }).catch((err: unknown) => {
            if (mine !== folderGen.current) return;
            setError(errText(err));
            setLoading('');
        });
    };

    const readMessageUid = reading?.uid ?? null;
    const sanitized = useMemo(() => {
        if (!reading || plainView) return { html: '', hasBlockedImages: false };
        return sanitizeEmailHtml(reading.html || '', showRemote);
    }, [reading, plainView, showRemote]);
    const hasHtmlPart = Boolean(reading?.html && reading.html.trim());
    const bodyText = reading?.body || '';
    const markdownBody = useMemo(() => (bodyText && looksLikeMarkdown(bodyText) ? bodyText : ''), [bodyText]);

    // Links inside rendered HTML go through the system browser; the sanitizer
    // already removed every other scheme.
    const onHtmlClick = (event: ReactMouseEvent<HTMLDivElement>) => {
        const anchor = (event.target as HTMLElement).closest?.('a');
        if (!anchor) return;
        event.preventDefault();
        const href = anchor.getAttribute('href') || '';
        if (/^(https?:|mailto:|tel:)/i.test(href)) BrowserOpenURL(href);
    };

    const sender = reading?.from || '';
    const senderAddr = fromAddress(sender);

    const dialog = (
        <div
            className="mailbox-manage-overlay"
            {...portalThemeAttributes}
            onMouseDown={event => { if (event.target === event.currentTarget) onClose(); }}
        >
            <div
                className="mailbox-manage"
                role="dialog"
                aria-modal="true"
                aria-labelledby="mailbox-manage-title"
                data-testid="mailbox-manage-dialog"
            >
                <header className="mailbox-manage__header">
                    <div className="mailbox-manage__header-main">
                        <h3 id="mailbox-manage-title">
                            <IconMailRead size={17} className="mailbox-manage__title-glyph" />
                            {t('Manage mail', '邮箱管理', '郵箱管理')}
                        </h3>
                        <p className="mailbox-manage__account">{account.name} · {account.address}</p>
                    </div>
                    <div className="mailbox-manage__header-actions">
                        <button
                            type="button"
                            className="mailbox-manage__tool"
                            title={t('Refresh', '刷新', '重新整理')}
                            aria-label={t('Refresh', '刷新', '重新整理')}
                            disabled={loading !== ''}
                            onClick={refresh}
                        ><IconRefresh size={15} /></button>
                        <button
                            ref={closeRef}
                            type="button"
                            className="mailbox-manage__close"
                            aria-label={t('Close', '关闭', '關閉')}
                            onClick={onClose}
                        >×</button>
                    </div>
                </header>
                {error && <p className="email-settings-notice email-settings-notice--err mailbox-manage__error" role="alert">{error}</p>}
                <div className="mailbox-manage__body">
                    <ul className="mailbox-manage__folders" aria-label={t('Folders', '文件夹', '資料夾')}>
                        {folders.map(folder => {
                            const count = unseen[folder.name] || 0;
                            return (
                                <li key={folder.name}>
                                    <button
                                        type="button"
                                        className={`mailbox-manage__folder${folder.name === box ? ' mailbox-manage__folder--current' : ''}`}
                                        aria-current={folder.name === box ? 'true' : undefined}
                                        title={folder.name}
                                        onClick={() => selectFolder(folder.name)}
                                    >
                                        <FolderGlyph special={folder.special} />
                                        <span className="mailbox-manage__folder-name">{folderLabel(t, folder)}</span>
                                        {count > 0 && <span className="mailbox-manage__badge" aria-label={t('{{count}} unread', `${count} 封未读`, `${count} 封未讀`).replace('{{count}}', String(count))}>{count > 99 ? '99+' : count}</span>}
                                    </button>
                                </li>
                            );
                        })}
                        {loading === 'folders' && <li className="mailbox-manage__empty">{t('Loading folders…', '正在读取文件夹…', '正在讀取資料夾…')}</li>}
                        {loading !== 'folders' && folders.length === 0 && !error && <li className="mailbox-manage__empty">{t('No folders.', '没有文件夹。', '沒有資料夾。')}</li>}
                        {loading !== 'folders' && folders.length === 0 && !!error && (
                            <li>
                                <button type="button" className="mailbox-manage__folder" onClick={() => setStartNonce(nonce => nonce + 1)}>
                                    {t('Try again', '再试一次', '再試一次')}
                                </button>
                            </li>
                        )}
                    </ul>
                    <div className="mailbox-manage__list-col">
                        <div className="mailbox-manage__batchbar" data-testid="mailbox-manage-batchbar">
                            <button
                                type="button"
                                role="checkbox"
                                aria-checked={allSelected ? 'true' : 'false'}
                                aria-label={t('Select all', '全选', '全選')}
                                title={t('Select all', '全选', '全選')}
                                className={`mailbox-manage__check mailbox-manage__check--all${allSelected ? ' mailbox-manage__check--on' : ''}`}
                                disabled={batchBusy}
                                onClick={toggleSelectAll}
                            >{allSelected && <IconCheck size={11} />}</button>
                            <span className="mailbox-manage__batchbar__label">
                                {selection.size > 0
                                    ? t('{{count}} selected', `已选 ${selection.size} 封`, `已選 ${selection.size} 封`).replace('{{count}}', String(selection.size))
                                    : t('Select all', '全选', '全選')}
                            </span>
                            {selection.size > 0 && (
                                <div className="mailbox-manage__batchbar__actions">
                                    <button
                                        type="button"
                                        className="mailbox-manage__tool"
                                        title={t('Mark as read', '标为已读', '標為已讀')}
                                        aria-label={t('Mark as read', '标为已读', '標為已讀')}
                                        disabled={batchBusy}
                                        onClick={() => { void markSelected(true); }}
                                    ><IconMailRead size={14} /></button>
                                    <button
                                        type="button"
                                        className="mailbox-manage__tool"
                                        title={t('Mark as unread', '标为未读', '標為未讀')}
                                        aria-label={t('Mark as unread', '标为未读', '標為未讀')}
                                        disabled={batchBusy}
                                        onClick={() => { void markSelected(false); }}
                                    ><IconMailUnread size={14} /></button>
                                    <button
                                        type="button"
                                        className="mailbox-manage__tool mailbox-manage__tool--danger"
                                        title={t('Delete selected', '删除所选', '刪除所選')}
                                        aria-label={t('Delete selected', '删除所选', '刪除所選')}
                                        disabled={batchBusy}
                                        data-testid="mailbox-manage-batch-delete"
                                        onClick={() => { void deleteSelected(); }}
                                    ><IconTrash size={14} /></button>
                                    <button
                                        type="button"
                                        className="mailbox-manage__tool"
                                        title={t('Clear selection', '取消选择', '取消選擇')}
                                        aria-label={t('Clear selection', '取消选择', '取消選擇')}
                                        disabled={batchBusy}
                                        onClick={() => setSelection(new Set())}
                                    ><IconCross size={14} /></button>
                                </div>
                            )}
                        </div>
                        <ul className="mailbox-manage__messages" aria-label={t('Messages', '邮件', '郵件')}>
                            {messages.map(message => (
                                <li
                                    key={message.uid}
                                    className={`mailbox-manage__item${selection.has(message.uid) ? ' mailbox-manage__item--checked' : ''}${selectedUid === message.uid ? ' mailbox-manage__item--current' : ''}`}
                                >
                                    <button
                                        type="button"
                                        role="checkbox"
                                        aria-checked={selection.has(message.uid) ? 'true' : 'false'}
                                        aria-label={t('Select message', '选择邮件', '選擇郵件')}
                                        className={`mailbox-manage__check${selection.has(message.uid) ? ' mailbox-manage__check--on' : ''}`}
                                        disabled={batchBusy}
                                        onClick={() => toggleSelect(message.uid)}
                                    >{selection.has(message.uid) && <IconCheck size={11} />}</button>
                                    <button
                                        type="button"
                                        className={`mailbox-manage__message${message.seen ? '' : ' mailbox-manage__message--unread'}${selectedUid === message.uid ? ' mailbox-manage__message--current' : ''}`}
                                        aria-current={selectedUid === message.uid ? 'true' : undefined}
                                        onClick={() => openMessage(message.uid)}
                                    >
                                        <span className="mailbox-manage__row-top">
                                            <span className="mailbox-manage__from" title={message.from || undefined}>
                                                {shortFrom(message.from) || t('(unknown sender)', '（未知发件人）', '（未知寄件人）')}
                                            </span>
                                            {message.has_attachment && <IconPaperclip size={12} className="mailbox-manage__attach" />}
                                            <span className="mailbox-manage__when">{formatWhen(message.date)}</span>
                                        </span>
                                        <span className="mailbox-manage__subject" title={message.subject?.trim() || undefined}>
                                            {message.subject?.trim() || t('(no subject)', '（无主题）', '（無主題）')}
                                        </span>
                                    </button>
                                    <div className="mailbox-manage__row-actions">
                                        <button
                                            type="button"
                                            className="mailbox-manage__tool"
                                            title={message.seen
                                                ? t('Mark as unread', '标为未读', '標為未讀')
                                                : t('Mark as read', '标为已读', '標為已讀')}
                                            aria-label={message.seen
                                                ? t('Mark as unread', '标为未读', '標為未讀')
                                                : t('Mark as read', '标为已读', '標為已讀')}
                                            disabled={actionUid !== null || batchBusy}
                                            onClick={event => { event.stopPropagation(); void toggleSeen(message); }}
                                        >{message.seen ? <IconMailUnread size={14} /> : <IconMailRead size={14} />}</button>
                                        <button
                                            type="button"
                                            className="mailbox-manage__tool mailbox-manage__tool--danger"
                                            title={t('Delete', '删除', '刪除')}
                                            aria-label={t('Delete', '删除', '刪除')}
                                            disabled={actionUid !== null || batchBusy}
                                            onClick={event => { event.stopPropagation(); void deleteMessage(message); }}
                                        ><IconTrash size={14} /></button>
                                    </div>
                                </li>
                            ))}
                            {loading === 'messages' && <li className="mailbox-manage__empty">{t('Loading messages…', '正在读取邮件…', '正在讀取郵件…')}</li>}
                            {box && loading !== 'messages' && messages.length === 0 && !error && <li className="mailbox-manage__empty">{t('This folder is empty.', '这个文件夹里没有邮件。', '這個資料夾裡沒有郵件。')}</li>}
                        </ul>
                    </div>
                    <section className="mailbox-manage__pane">
                        {loading === 'read' && <div className="mailbox-manage__placeholder"><p>{t('Opening…', '正在打开…', '正在打開…')}</p></div>}
                        {reading && (
                            <article className="mailbox-manage__view" data-testid="mailbox-manage-view">
                                <header className="mailbox-manage__view__head">
                                    <div className="mailbox-manage__view__title-row">
                                        <h4>{reading.subject?.trim() || t('(no subject)', '（无主题）', '（無主題）')}</h4>
                                        <div className="mailbox-manage__view__tools">
                                            {hasHtmlPart && (
                                                <div className="mailbox-manage__seg" role="group" aria-label={t('Body format', '正文格式', '正文格式')}>
                                                    <button
                                                        type="button"
                                                        className={`mailbox-manage__seg__btn${!plainView ? ' mailbox-manage__seg__btn--on' : ''}`}
                                                        onClick={() => setPlainView(false)}
                                                    >{t('Rich', '渲染', '渲染')}</button>
                                                    <button
                                                        type="button"
                                                        className={`mailbox-manage__seg__btn${plainView ? ' mailbox-manage__seg__btn--on' : ''}`}
                                                        onClick={() => setPlainView(true)}
                                                    >{t('Plain text', '纯文本', '純文字')}</button>
                                                </div>
                                            )}
                                            <button
                                                type="button"
                                                className="mailbox-manage__tool"
                                                title={reading.seen
                                                    ? t('Mark as unread', '标为未读', '標為未讀')
                                                    : t('Mark as read', '标为已读', '標為已讀')}
                                                aria-label={reading.seen
                                                    ? t('Mark as unread', '标为未读', '標為未讀')
                                                    : t('Mark as read', '标为已读', '標為已讀')}
                                                disabled={actionUid !== null}
                                                onClick={() => { void toggleSeen(reading); }}
                                            >{reading.seen ? <IconMailUnread size={14} /> : <IconMailRead size={14} />}</button>
                                            <button
                                                type="button"
                                                className="mailbox-manage__tool mailbox-manage__tool--danger"
                                                title={t('Delete', '删除', '刪除')}
                                                aria-label={t('Delete', '删除', '刪除')}
                                                disabled={actionUid !== null}
                                                onClick={() => { void deleteMessage(reading); }}
                                            ><IconTrash size={14} /></button>
                                        </div>
                                    </div>
                                    <div className="mailbox-manage__view__meta">
                                        <span className="mailbox-manage__avatar" style={{ background: `hsl(${senderHue(sender)} 42% 46%)` }} aria-hidden="true">
                                            {(shortFrom(sender) || '?').slice(0, 1).toUpperCase()}
                                        </span>
                                        <div className="mailbox-manage__view__meta-text">
                                            <p className="mailbox-manage__view__from">
                                                <strong>{shortFrom(sender) || t('(unknown sender)', '（未知发件人）', '（未知寄件人）')}</strong>
                                                {senderAddr && <span className="mailbox-manage__view__addr">{senderAddr}</span>}
                                                {reading.date && <span className="mailbox-manage__view__date">{formatWhenFull(reading.date)}</span>}
                                            </p>
                                            {reading.to && <p className="mailbox-manage__view__to">{t('To', '收件人', '收件人')}: {reading.to}</p>}
                                        </div>
                                    </div>
                                    {!!reading.attachments?.length && (
                                        <div className="mailbox-manage__att-row">
                                            <IconPaperclip size={13} className="mailbox-manage__att-glyph" />
                                            {reading.attachments.map((name, index) => (
                                                <span key={`${index}-${name}`} className="mailbox-manage__att-chip" title={name}>{name}</span>
                                            ))}
                                        </div>
                                    )}
                                    {sanitized.hasBlockedImages && !plainView && (
                                        <div className="mailbox-manage__remote-bar">
                                            <span>{t('Remote pictures are hidden.', '已隐藏远程图片。', '已隱藏遠端圖片。')}</span>
                                            <button type="button" onClick={() => setShowRemote(true)}>
                                                {t('Show pictures', '显示图片', '顯示圖片')}
                                            </button>
                                        </div>
                                    )}
                                </header>
                                <div className="mailbox-manage__view__body">
                                    {!plainView && hasHtmlPart && (
                                        <div
                                            className="email-html-body mailbox-manage__html"
                                            data-testid="mailbox-manage-html"
                                            onClick={onHtmlClick}
                                            dangerouslySetInnerHTML={{ __html: sanitized.html }}
                                        />
                                    )}
                                    {(plainView || !hasHtmlPart) && (markdownBody ? (
                                        <div className="mailbox-md" data-testid="mailbox-manage-markdown">
                                            <ReactMarkdown
                                                remarkPlugins={[remarkGfm]}
                                                components={{
                                                    a: ({ href, children }) => {
                                                        const safeHref = typeof href === 'string' && /^(https?:\/\/|mailto:|tel:)/i.test(href) ? href : '';
                                                        if (!safeHref) return <span>{children}</span>;
                                                        return (
                                                            <a
                                                                href={safeHref}
                                                                onClick={event => { event.preventDefault(); BrowserOpenURL(safeHref); }}
                                                            >{children}</a>
                                                        );
                                                    },
                                                    // Remote images stay unloaded from markdown too.
                                                    img: ({ alt }) => (alt ? <span className="mailbox-md__img-alt">[{alt}]</span> : null),
                                                }}
                                            >
                                                {markdownBody}
                                            </ReactMarkdown>
                                        </div>
                                    ) : (
                                        <div className="mailbox-manage__plaintext" data-testid="mailbox-manage-plaintext">{bodyText || t('(no text)', '（没有正文）', '（沒有正文）')}</div>
                                    ))}
                                    {reading.truncated && <p className="mailbox-manage__truncated">{t('The rest of this message was not loaded.', '正文后面的内容没有继续加载。', '正文後面的內容沒有繼續載入。')}</p>}
                                </div>
                            </article>
                        )}
                        {!reading && loading !== 'read' && (
                            <div className="mailbox-manage__placeholder">
                                <IconMailRead size={30} className="mailbox-manage__placeholder-glyph" />
                                <p>{t('Select a message to read it here.', '选择一封邮件，在这里查看内容。', '選擇一封郵件，在這裡查看內容。')}</p>
                            </div>
                        )}
                    </section>
                </div>
            </div>
        </div>
    );
    // The settings page scales and clips its content. A dialog left inside
    // that page is cut off, so this one is drawn on the document root.
    return typeof document === 'undefined' ? dialog : createPortal(dialog, document.body);
}
