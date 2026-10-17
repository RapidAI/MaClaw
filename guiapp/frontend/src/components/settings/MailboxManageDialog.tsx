import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { usePortalThemeAttributes } from '../../hooks/usePortalThemeAttributes';
import {
    MailboxBrowseMessages,
    MailboxBrowseRead,
    MailboxBrowseStart,
} from '../../../wailsjs/go/main/App';

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
type MessageView = MessageRow & { body?: string; truncated?: boolean; attachments?: string[] };
type StartPage = {
    folders?: FolderRow[];
    box?: string;
    messages?: MessageRow[];
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
    const portalThemeAttributes = usePortalThemeAttributes();
    const closeRef = useRef<HTMLButtonElement>(null);
    const onCloseRef = useRef(onClose);
    onCloseRef.current = onClose;
    const folderGen = useRef(0);
    const messageGen = useRef(0);
    const readGen = useRef(0);
    const [folders, setFolders] = useState<FolderRow[]>([]);
    const [box, setBox] = useState('');
    const [messages, setMessages] = useState<MessageRow[]>([]);
    const [reading, setReading] = useState<MessageView | null>(null);
    const [selectedUid, setSelectedUid] = useState<number | null>(null);
    const [error, setError] = useState('');
    const [loading, setLoading] = useState<'folders' | 'messages' | 'read' | ''>('folders');
    const [startNonce, setStartNonce] = useState(0);

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
        void MailboxBrowseStart(account.id).then(page => {
            if (mine !== folderGen.current) return;
            const start = (page || {}) as StartPage;
            const rows = Array.isArray(start.folders) ? start.folders : [];
            setFolders(rows);
            setBox(start.box || '');
            setMessages(Array.isArray(start.messages) ? start.messages : []);
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

    const openMessage = (uid: number) => {
        const mine = ++readGen.current;
        setError('');
        setSelectedUid(uid);
        setReading(null);
        setLoading('read');
        void MailboxBrowseRead(account.id, box, uid).then(view => {
            if (mine !== readGen.current) return;
            setReading(view as MessageView);
            setLoading('');
        }).catch((err: unknown) => {
            if (mine !== readGen.current) return;
            setError(errText(err));
            setLoading('');
        });
    };

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
                    <div>
                        <h3 id="mailbox-manage-title">{t('Manage mail', '邮箱管理', '郵箱管理')}</h3>
                        <p className="mailbox-manage__account">{account.name} · {account.address}</p>
                    </div>
                    <button
                        ref={closeRef}
                        type="button"
                        className="mailbox-manage__close"
                        aria-label={t('Close', '关闭', '關閉')}
                        onClick={onClose}
                    >×</button>
                </header>
                {error && <p className="email-settings-notice email-settings-notice--err" role="alert">{error}</p>}
                <div className="mailbox-manage__body">
                    <ul className="mailbox-manage__folders" aria-label={t('Folders', '文件夹', '資料夾')}>
                        {folders.map(folder => (
                            <li key={folder.name}>
                                <button
                                    type="button"
                                    className={`mailbox-manage__folder${folder.name === box ? ' mailbox-manage__folder--current' : ''}`}
                                    aria-current={folder.name === box ? 'true' : undefined}
                                    title={folder.name}
                                    onClick={() => selectFolder(folder.name)}
                                >{folderLabel(t, folder)}</button>
                            </li>
                        ))}
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
                    <ul className="mailbox-manage__messages" aria-label={t('Messages', '邮件', '郵件')}>
                        {messages.map(message => (
                            <li key={message.uid}>
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
                                        {message.has_attachment && <span className="mailbox-manage__attach">{t('Attachment', '附件', '附件')}</span>}
                                        <span className="mailbox-manage__when">{formatWhen(message.date)}</span>
                                    </span>
                                    <span className="mailbox-manage__subject" title={message.subject?.trim() || undefined}>
                                        {message.subject?.trim() || t('(no subject)', '（无主题）', '（無主題）')}
                                    </span>
                                </button>
                            </li>
                        ))}
                        {loading === 'messages' && <li className="mailbox-manage__empty">{t('Loading messages…', '正在读取邮件…', '正在讀取郵件…')}</li>}
                        {box && loading !== 'messages' && messages.length === 0 && !error && <li className="mailbox-manage__empty">{t('This folder is empty.', '这个文件夹里没有邮件。', '這個資料夾裡沒有郵件。')}</li>}
                    </ul>
                    <section className="mailbox-manage__pane">
                        {loading === 'read' && <div className="mailbox-manage__placeholder"><p>{t('Opening…', '正在打开…', '正在打開…')}</p></div>}
                        {reading && (
                            <article className="mailbox-manage__view" data-testid="mailbox-manage-view">
                                <header className="mailbox-manage__view__head">
                                    <h4>{reading.subject?.trim() || t('(no subject)', '（无主题）', '（無主題）')}</h4>
                                    <p>{reading.from}{reading.date ? ` · ${formatWhenFull(reading.date)}` : ''}</p>
                                    {reading.to && <p>{t('To', '收件人', '收件人')}: {reading.to}</p>}
                                    {!!reading.attachments?.length && <p>{t('Attachments', '附件', '附件')}: {reading.attachments.join(', ')}</p>}
                                </header>
                                <div className="mailbox-manage__view__body">
                                    <pre>{reading.body || t('(no text)', '（没有正文）', '（沒有正文）')}</pre>
                                    {reading.truncated && <p>{t('The rest of this message was not loaded.', '正文后面的内容没有继续加载。', '正文後面的內容沒有繼續載入。')}</p>}
                                </div>
                            </article>
                        )}
                        {!reading && loading !== 'read' && (
                            <div className="mailbox-manage__placeholder">
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
