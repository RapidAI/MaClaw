import { useEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { AcceptCloudWorkspaceShare, CreateCloudWorkspaceShare, GetCloudWorkspaceShare, RemoveCloudWorkspaceShareRecipient, StopCloudWorkspaceShare, UpdateCloudWorkspaceShareRecipient } from '../../../wailsjs/go/main/App';
import { EventsEmit } from '../../../wailsjs/runtime';
import { EVENT_PROJECT_INDEX_CHANGED } from '../../constants/events';
import { useToast } from '../Toast';
import { extractErrorMessage } from '../ai/participantAddError';
import { useDialog } from '../CustomDialog';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';

const getPortalThemeMode = (themeMode?: string) => (
    themeMode || document.getElementById('App')?.getAttribute('data-ai-theme') || undefined
);
const getPortalDarkScheme = () => document.getElementById('App')?.getAttribute('data-ai-dark-scheme') || undefined;
const getPortalLightScheme = () => document.getElementById('App')?.getAttribute('data-ai-light-scheme') || undefined;

const textForLang = (lang: string, en: string, zh: string, zhHant = zh) => {
    if (lang === 'zh-Hant') return zhHant;
    if (lang.startsWith('zh')) return zh;
    return en;
};

type ShareFieldKeyEvent = {
    isComposing?: boolean;
    keyCode?: number;
    ctrlKey: boolean;
    metaKey: boolean;
    altKey: boolean;
    shiftKey: boolean;
    key: string;
    target: EventTarget | null;
    preventDefault(): void;
    stopPropagation(): void;
};

/** Keep Ctrl/Cmd+A inside the share field. A page-level select-all lands on the backdrop and closes the dialog. */
function selectAllInShareField(event: ShareFieldKeyEvent) {
    if (event.isComposing || event.keyCode === 229) return;
    if (!(event.ctrlKey || event.metaKey) || event.altKey || event.shiftKey) return;
    if (event.key.toLowerCase() !== 'a') return;
    const target = event.target;
    if (!(target instanceof HTMLInputElement) || target.disabled) return;
    if (!target.closest('.cwshare-dialog')) return;
    const textual = target.type === '' || target.type === 'text' || target.type === 'password' || target.type === 'search' || target.type === 'url' || target.type === 'email' || target.type === 'tel';
    if (!textual) {
        // A checkbox or button cannot hold the selection. Swallow the shortcut
        // so it does not select the page and dismiss the dialog.
        event.preventDefault();
        event.stopPropagation();
        return;
    }
    const end = target.value.length;
    try {
        target.setSelectionRange(0, end);
    } catch {
        // Leave the browser's own select-all when this field rejects a scripted range.
        return;
    }
    // Some engines accept the call but leave the caret unmoved. Cancelling the
    // default action then selects nothing and the page selection can still land
    // on the backdrop.
    if (target.selectionStart !== 0 || target.selectionEnd !== end) return;
    event.preventDefault();
    event.stopPropagation();
}

export type CloudWorkspaceShareTask = {
    name: string;
    workspaceId: string;
};

type ShareRecipient = {
    user_id?: string;
    UserID?: string;
    email?: string;
    Email?: string;
    display_name?: string;
    DisplayName?: string;
    home_hub?: string;
    HomeHub?: string;
    permission?: string;
    Permission?: string;
    accepted_at?: string;
    AcceptedAt?: string;
};

type ShareView = {
    share_url?: string;
    ShareURL?: string;
    token?: string;
    Token?: string;
    default_permission?: string;
    DefaultPermission?: string;
    password_set?: boolean;
    PasswordSet?: boolean;
    expires_at?: string;
    ExpiresAt?: string;
    recipients?: ShareRecipient[];
    Recipients?: ShareRecipient[];
};

type ShareTTL = 'never' | '1d' | '7d' | '30d' | '90d';

const SHARE_TTL_OPTIONS: { id: ShareTTL; en: string; zh: string; zhHant: string }[] = [
    { id: '1d', en: '1 day', zh: '1 天', zhHant: '1 天' },
    { id: '7d', en: '7 days', zh: '7 天', zhHant: '7 天' },
    { id: '30d', en: '30 days', zh: '30 天', zhHant: '30 天' },
    { id: '90d', en: '90 days', zh: '90 天', zhHant: '90 天' },
    { id: 'never', en: 'No expiry', zh: '永久', zhHant: '永久' },
];

export const cloudWorkspaceShareJoinLooksLikeOwn = (text: string) => {
    const lower = text.toLowerCase();
    return text.includes('自己的云端工作区')
        || text.includes('自己的雲端工作區')
        || lower.includes('your own cloud workspace')
        || lower.includes('cannot accept your own');
};

export const cloudWorkspaceShareJoinLooksLikeDeadLink = (text: string) => {
    const lower = text.toLowerCase();
    return text.includes('已过期')
        || text.includes('已過期')
        || text.includes('已停止分享')
        || lower.includes('has expired')
        || lower.includes('no longer active');
};

const ttlFromExpiresAt = (raw: string): ShareTTL => {
    const expires = Date.parse(raw);
    if (!raw.trim() || !Number.isFinite(expires)) return 'never';
    const hours = (expires - Date.now()) / 36e5;
    if (hours <= 36) return '1d';
    if (hours <= 24 * 10) return '7d';
    if (hours <= 24 * 45) return '30d';
    if (hours <= 24 * 120) return '90d';
    return 'never';
};

const shareText = (row: Record<string, unknown> | undefined, ...keys: string[]) => {
    if (!row) return '';
    for (const key of keys) {
        const value = row[key];
        if (typeof value === 'string' && value.trim()) return value.trim();
    }
    return '';
};

export function CloudWorkspaceShareDialog({
    open,
    lang,
    themeMode,
    task,
    onClose,
}: {
    open: boolean;
    lang: string;
    themeMode?: string;
    task: CloudWorkspaceShareTask | null;
    onClose: () => void;
}) {
    const { showConfirm } = useDialog();
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const [permission, setPermission] = useState<'read' | 'write'>('read');
    const [ttl, setTtl] = useState<ShareTTL>('never');
    const [password, setPassword] = useState('');
    const [clearPassword, setClearPassword] = useState(false);
    const [view, setView] = useState<ShareView | null>(null);
    const [busy, setBusy] = useState(false);
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !busy });
    const busyRef = useRef(false);
    const [error, setError] = useState('');
    const [copied, setCopied] = useState(false);
    const beginShareBusy = () => {
        if (busyRef.current) return false;
        busyRef.current = true;
        setBusy(true);
        return true;
    };
    const endShareBusy = () => {
        busyRef.current = false;
        setBusy(false);
    };

    const workspaceId = task?.workspaceId || '';

    useEffect(() => {
        if (!open || !workspaceId) {
            setView(null);
            setError('');
            setCopied(false);
            setPassword('');
            setClearPassword(false);
            setTtl('never');
            return;
        }
        let cancelled = false;
        busyRef.current = true;
        setBusy(true);
        GetCloudWorkspaceShare(workspaceId).then(raw => {
            if (cancelled) return;
            const next = (raw || {}) as ShareView;
            setView(next);
            const current = shareText(next as Record<string, unknown>, 'default_permission', 'DefaultPermission');
            if (current === 'write') setPermission('write');
            else setPermission('read');
            setTtl(ttlFromExpiresAt(shareText(next as Record<string, unknown>, 'expires_at', 'ExpiresAt')));
            setPassword('');
            setClearPassword(false);
        }).catch(err => {
            if (!cancelled) setError(extractErrorMessage(err) || textForLang(lang, 'Failed to load share', '加载分享失败', '載入分享失敗'));
        }).finally(() => {
            if (cancelled) return;
            busyRef.current = false;
            setBusy(false);
        });
        return () => { cancelled = true; };
    }, [lang, open, workspaceId]);

    useEffect(() => {
        if (!open) return;
        const onKeyDown = (event: KeyboardEvent) => {
            selectAllInShareField(event);
            if (event.defaultPrevented) return;
            if (event.key !== 'Escape' || event.isComposing || event.keyCode === 229) return;
            if (busy || document.querySelector('.custom-dialog')) return;
            event.preventDefault();
            event.stopImmediatePropagation();
            onClose();
        };
        window.addEventListener('keydown', onKeyDown, true);
        return () => window.removeEventListener('keydown', onKeyDown, true);
    }, [busy, onClose, open]);

    if (!open || !task) return null;

    const shareURL = shareText(view as Record<string, unknown> | undefined, 'share_url', 'ShareURL');
    const storedPermission = shareText(view as Record<string, unknown> | undefined, 'default_permission', 'DefaultPermission') === 'write' ? 'write' : 'read';
    const passwordSet = !!(view?.password_set || view?.PasswordSet);
    const storedTtl = ttlFromExpiresAt(shareText(view as Record<string, unknown> | undefined, 'expires_at', 'ExpiresAt'));
    const permissionDirty = !!shareURL && storedPermission !== permission;
    const settingsDirty = permissionDirty || (!!shareURL && (ttl !== storedTtl || !!password.trim() || clearPassword));
    const recipients = (view?.recipients || view?.Recipients || []) as ShareRecipient[];

    const generate = async () => {
        if (!beginShareBusy()) return;
        setError('');
        try {
            const next = await CreateCloudWorkspaceShare(workspaceId, permission, clearPassword ? '' : password, ttl !== storedTtl ? ttl : '', clearPassword) as ShareView;
            setView(next);
            setCopied(false);
            setPassword('');
            setClearPassword(false);
        } catch (err) {
            setError(extractErrorMessage(err) || textForLang(lang, 'Failed to create share link', '生成分享链接失败', '產生分享連結失敗'));
        } finally {
            endShareBusy();
        }
    };

    const copy = async () => {
        if (!shareURL) return;
        try {
            await navigator.clipboard.writeText(shareURL);
            setCopied(true);
        } catch {
            setError(textForLang(lang, 'Copy is unavailable. Select the link and copy it manually.', '无法自动复制，请选中链接后手动复制。', '無法自動複製，請選中連結後手動複製。'));
        }
    };

    const stopShare = async () => {
        if (busy) return;
        const confirmed = await showConfirm(
            textForLang(lang, 'The share link will stop working and every recipient will lose access.', '分享链接将立即失效，所有接受者都会失去打开权限。', '分享連結將立即失效，所有接受者都會失去開啟權限。'),
            textForLang(lang, 'Stop sharing', '停止分享', '停止分享'),
            { confirmText: textForLang(lang, 'Stop sharing', '停止分享', '停止分享'), cancelText: textForLang(lang, 'Cancel', '取消', '取消'), confirmVariant: 'danger' },
        );
        if (!confirmed) return;
        if (!beginShareBusy()) return;
        setError('');
        try {
            await StopCloudWorkspaceShare(workspaceId);
            setView({ recipients: [] });
        } catch (err) {
            setError(extractErrorMessage(err) || textForLang(lang, 'Failed to stop sharing', '停止分享失败', '停止分享失敗'));
        } finally {
            endShareBusy();
        }
    };

    const changeRecipient = async (userId: string, nextPermission: 'read' | 'write') => {
        if (!userId || !beginShareBusy()) return;
        setError('');
        try {
            await UpdateCloudWorkspaceShareRecipient(workspaceId, userId, nextPermission);
            const next = await GetCloudWorkspaceShare(workspaceId) as ShareView;
            setView(next);
        } catch (err) {
            setError(extractErrorMessage(err) || textForLang(lang, 'Failed to update permission', '更新权限失败', '更新權限失敗'));
        } finally {
            endShareBusy();
        }
    };

    const removeRecipient = async (userId: string, label: string) => {
        if (!userId || busy) return;
        const confirmed = await showConfirm(
            textForLang(lang, `Remove access for ${label}?`, `移除「${label}」的分享权限？`, `移除「${label}」的分享權限？`),
            textForLang(lang, 'Remove recipient', '移除接受者', '移除接受者'),
            { confirmText: textForLang(lang, 'Remove', '移除', '移除'), cancelText: textForLang(lang, 'Cancel', '取消', '取消'), confirmVariant: 'danger' },
        );
        if (!confirmed) return;
        if (!beginShareBusy()) return;
        setError('');
        try {
            await RemoveCloudWorkspaceShareRecipient(workspaceId, userId);
            const next = await GetCloudWorkspaceShare(workspaceId) as ShareView;
            setView(next);
        } catch (err) {
            setError(extractErrorMessage(err) || textForLang(lang, 'Failed to remove recipient', '移除接受者失败', '移除接受者失敗'));
        } finally {
            endShareBusy();
        }
    };

    return createPortal(
        <div
            className="modal-backdrop cwshare-backdrop"
            data-testid="task-cloud-share-dialog"
            data-ai-theme={getPortalThemeMode(themeMode)}
            data-ai-dark-scheme={getPortalDarkScheme()}
            data-ai-light-scheme={getPortalLightScheme()}
            {...backdropProps}
        >
            <div
                ref={dialogRef}
                className="modal-content cwshare-dialog cwshare-dialog--share"
                role="dialog"
                aria-modal="true"
                aria-labelledby="task-cloud-share-title"
                {...dialogProps}
            >
                <div className="modal-header">
                    <h3 id="task-cloud-share-title" className="cwshare-title">
                        {textForLang(lang, `Share “${task.name}”`, `分享「${task.name}」`, `分享「${task.name}」`)}
                    </h3>
                    <button type="button" className="btn-close" data-testid="task-cloud-share-close" aria-label={textForLang(lang, 'Close', '关闭', '關閉')} disabled={busy} onClick={onClose}>X</button>
                </div>
                <div className="modal-body cwshare-body">
                    <div className="cwshare-hint">
                        {textForLang(lang, 'Hub users who open the link can join this cloud workspace. Read-only viewers coexist; writers sync to your workspace and take the exclusive writer lock.', '打开链接的 Hub 用户会加入该云端工作区。只读可同时查看；可读写会同步到你的工作区，同一时间仍由一人写入。', '打開連結的 Hub 使用者會加入該雲端工作區。唯讀可同時查看；可讀寫會同步到你的工作區，同一時間仍由一人寫入。')}
                    </div>
                    <div>
                        <div className="cwshare-field-label">{textForLang(lang, 'Permission', '权限', '權限')}</div>
                        <div role="group" className="cwshare-permission-row">
                            {(['read', 'write'] as const).map(id => (
                                <button
                                    key={id}
                                    type="button"
                                    data-testid={`task-cloud-share-permission-${id}`}
                                    aria-pressed={permission === id}
                                    disabled={busy}
                                    onClick={() => setPermission(id)}
                                    style={{ flex: 1, border: permission === id ? '1px solid var(--theme-primary)' : '1px solid var(--theme-border)', borderRadius: '8px', background: permission === id ? 'color-mix(in srgb, var(--theme-primary) 12%, var(--theme-surface))' : 'var(--theme-surface)', color: permission === id ? 'var(--theme-primary)' : 'var(--theme-text-primary)', padding: '8px 10px', textAlign: 'left', cursor: busy ? 'default' : 'pointer' }}
                                >
                                    <span className="cwshare-option-name">{id === 'read' ? textForLang(lang, 'Read-only', '只读', '唯讀') : textForLang(lang, 'Read & write', '可读写', '可讀寫')}</span>
                                    <span className="cwshare-option-detail">
                                        {id === 'read'
                                            ? textForLang(lang, 'Open and view files', '可打开查看，不能改文件', '可開啟查看，不能改檔案')
                                            : textForLang(lang, 'Sync and edit your workspace', '挂载你的工作区并同步修改', '掛載你的工作區並同步修改')}
                                    </span>
                                </button>
                            ))}
                        </div>
                    </div>
                    <div>
                        <div className="cwshare-field-label">{textForLang(lang, 'Expiry', '有效期', '有效期')}</div>
                        <div role="group" className="cwshare-ttl-row">
                            {SHARE_TTL_OPTIONS.map(opt => (
                                <button
                                    key={opt.id}
                                    type="button"
                                    data-testid={`task-cloud-share-ttl-${opt.id}`}
                                    aria-pressed={ttl === opt.id}
                                    disabled={busy}
                                    onClick={() => setTtl(opt.id)}
                                    style={{ border: ttl === opt.id ? '1px solid var(--theme-primary)' : '1px solid var(--theme-border)', borderRadius: '8px', background: ttl === opt.id ? 'color-mix(in srgb, var(--theme-primary) 12%, var(--theme-surface))' : 'var(--theme-surface)', color: ttl === opt.id ? 'var(--theme-primary)' : 'var(--theme-text-primary)', padding: '6px 8px', fontSize: '0.7rem', fontWeight: 700, cursor: busy ? 'default' : 'pointer' }}
                                >
                                    {textForLang(lang, opt.en, opt.zh, opt.zhHant)}
                                </button>
                            ))}
                        </div>
                    </div>
                    <div>
                        <div className="cwshare-field-label">{textForLang(lang, 'Share password', '分享密码', '分享密碼')}</div>
                        <input
                            type="password"
                            data-testid="task-cloud-share-password"
                            autoComplete="new-password"
                            disabled={busy || clearPassword}
                            value={password}
                            onChange={e => { setPassword(e.target.value); setClearPassword(false); }}
                            onKeyDown={selectAllInShareField}
                            placeholder={passwordSet
                                ? textForLang(lang, 'Leave blank to keep the current password', '留空则保持现有密码', '留空則保持現有密碼')
                                : textForLang(lang, 'Optional. Recipients enter this to join.', '可选。打开链接时需要输入。', '可選。打開連結時需要輸入。')}
                            className="cwshare-input"
                        />
                        {passwordSet ? (
                            <label className="cwshare-clear-label">
                                <input type="checkbox" data-testid="task-cloud-share-clear-password" checked={clearPassword} disabled={busy} onChange={e => { setClearPassword(e.target.checked); if (e.target.checked) setPassword(''); }} />
                                {textForLang(lang, 'Remove password', '清除密码', '清除密碼')}
                            </label>
                        ) : null}
                    </div>
                    <div>
                        <div className="cwshare-field-label">{textForLang(lang, 'Share link', '分享链接', '分享連結')}</div>
                        {shareURL ? (
                            <div className="cwshare-link-column">
                                <div className="cwshare-link-row">
                                    <input readOnly value={shareURL} data-testid="task-cloud-share-url" className="cwshare-url" />
                                    <button type="button" className="btn-primary" data-testid="task-cloud-share-copy" disabled={busy} onClick={() => { void copy(); }}>{copied ? textForLang(lang, 'Copied', '已复制', '已複製') : textForLang(lang, 'Copy', '复制', '複製')}</button>
                                    <button type="button" className="btn-secondary" data-testid="task-cloud-share-stop" disabled={busy} onClick={() => { void stopShare(); }}>{textForLang(lang, 'Stop sharing', '停止分享', '停止分享')}</button>
                                </div>
                                {settingsDirty ? (
                                    <button type="button" className="btn-secondary cwshare-update-btn" data-testid="task-cloud-share-update-permission" disabled={busy} onClick={() => { void generate(); }}>
                                        {textForLang(lang, 'Update share settings', '更新分享设置', '更新分享設定')}
                                    </button>
                                ) : null}
                            </div>
                        ) : (
                            <button type="button" className="btn-primary" data-testid="task-cloud-share-generate" disabled={busy} onClick={() => { void generate(); }}>
                                {busy ? textForLang(lang, 'Working…', '处理中…', '處理中…') : textForLang(lang, 'Generate share link', '生成分享链接', '產生分享連結')}
                            </button>
                        )}
                    </div>
                    {error ? <div role="alert" data-testid="task-cloud-share-error" className="cwshare-alert cwshare-alert--error">{error}</div> : null}
                    <div>
                        <div className="cwshare-field-label">
                            {textForLang(lang, `Recipients · ${recipients.length}`, `接受者  共 ${recipients.length} 人`, `接受者  共 ${recipients.length} 人`)}
                        </div>
                        {recipients.length === 0 ? (
                            <div data-testid="task-cloud-share-empty" className="cwshare-empty">
                                {textForLang(lang, 'No one has opened this link yet.', '还没有人打开此链接。', '還沒有人打開此連結。')}
                            </div>
                        ) : recipients.map(rec => {
                            const id = shareText(rec as Record<string, unknown>, 'user_id', 'UserID');
                            const email = shareText(rec as Record<string, unknown>, 'email', 'Email', 'display_name', 'DisplayName') || id;
                            const homeHub = shareText(rec as Record<string, unknown>, 'home_hub', 'HomeHub');
                            const recPerm = shareText(rec as Record<string, unknown>, 'permission', 'Permission') === 'write' ? 'write' : 'read';
                            return (
                                <div key={id || email} data-testid="task-cloud-share-recipient" className="cwshare-recipient">
                                    <span className="cwshare-recipient-id">
                                        <span className="cwshare-recipient-email">{email}</span>
                                        <span className="cwshare-recipient-perm">
                                            {recPerm === 'write' ? textForLang(lang, 'Read & write', '可读写', '可讀寫') : textForLang(lang, 'Read-only', '只读', '唯讀')}
                                            {homeHub ? ` · ${homeHub}` : ''}
                                        </span>
                                    </span>
                                    <span className="cwshare-recipient-actions">
                                        <button type="button" data-testid="task-cloud-share-recipient-toggle" disabled={busy} onClick={() => { void changeRecipient(id, recPerm === 'write' ? 'read' : 'write'); }} style={{ border: 'none', background: 'transparent', color: 'var(--theme-primary)', cursor: busy ? 'default' : 'pointer', fontSize: '0.66rem' }}>
                                            {recPerm === 'write' ? textForLang(lang, 'Make read-only', '改为只读', '改為唯讀') : textForLang(lang, 'Make writable', '改为可读写', '改為可讀寫')}
                                        </button>
                                        <button type="button" data-testid="task-cloud-share-recipient-remove" disabled={busy} onClick={() => { void removeRecipient(id, email); }} style={{ border: 'none', background: 'transparent', color: 'var(--theme-danger, #b91c1c)', cursor: busy ? 'default' : 'pointer', fontSize: '0.66rem' }}>
                                            {textForLang(lang, 'Remove', '移除', '移除')}
                                        </button>
                                    </span>
                                </div>
                            );
                        })}
                    </div>
                </div>
            </div>
        </div>,
        document.body,
    );
}

export function CloudWorkspaceShareJoinDialog({
    open,
    lang,
    themeMode,
    shareURL,
    onClose,
}: {
    open: boolean;
    lang: string;
    themeMode?: string;
    shareURL: string;
    onClose: () => void;
}) {
    const { showToast } = useToast();
    const [password, setPassword] = useState('');
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !busy });

    useEffect(() => {
        if (!open) {
            setPassword('');
            setError('');
            setBusy(false);
        }
    }, [open]);

    if (!open || !shareURL.trim()) return null;

    const submit = async () => {
        if (busy) return;
        setBusy(true);
        setError('');
        try {
            const result = await AcceptCloudWorkspaceShare(shareURL, password) as { name?: string; Name?: string; tags?: string[]; Tags?: string[] };
            const name = String(result?.name || result?.Name || '').trim()
                || textForLang(lang, 'Cloud workspace', '云端工作区', '雲端工作區');
            const tags = result?.tags || result?.Tags || [];
            const from = tags.map(tag => String(tag || '')).find(tag => tag.startsWith('cloud_workspace_shared_from:'))?.slice('cloud_workspace_shared_from:'.length) || '';
            showToast(
                from
                    ? textForLang(lang, `Joined “${name}” (from ${from})`, `已加入「${name}」（来自 ${from}）`, `已加入「${name}」（來自 ${from}）`)
                    : textForLang(lang, `Joined shared cloud workspace “${name}”`, `已加入分享的云端工作区「${name}」`, `已加入分享的雲端工作區「${name}」`),
                'success',
                3500,
            );
            EventsEmit(EVENT_PROJECT_INDEX_CHANGED);
            onClose();
        } catch (err) {
            const text = extractErrorMessage(err) || textForLang(lang, 'Failed to join cloud workspace', '加入云端工作区失败', '加入雲端工作區失敗');
            if (cloudWorkspaceShareJoinLooksLikeOwn(text)) {
                showToast(text, 'info', 3500);
                onClose();
                return;
            }
            if (cloudWorkspaceShareJoinLooksLikeDeadLink(text)) {
                showToast(text, 'error', 3500);
                onClose();
                return;
            }
            setError(text);
        } finally {
            setBusy(false);
        }
    };

    return createPortal(
        <div
            className="modal-backdrop cwshare-backdrop"
            data-testid="task-cloud-share-join-dialog"
            data-ai-theme={getPortalThemeMode(themeMode)}
            data-ai-dark-scheme={getPortalDarkScheme()}
            data-ai-light-scheme={getPortalLightScheme()}
            {...backdropProps}
        >
            <div
                className="modal-content cwshare-dialog cwshare-dialog--join"
                role="dialog"
                aria-modal="true"
                aria-labelledby="task-cloud-share-join-title"
                {...dialogProps}
            >
                <div className="modal-header">
                    <h3 id="task-cloud-share-join-title" className="cwshare-title">
                        {textForLang(lang, 'Enter share password', '输入分享密码', '輸入分享密碼')}
                    </h3>
                    <button type="button" className="btn-close" data-testid="task-cloud-share-join-close" aria-label={textForLang(lang, 'Close', '关闭', '關閉')} disabled={busy} onClick={onClose}>X</button>
                </div>
                <div className="modal-body cwshare-body cwshare-body--join">
                    <div className="cwshare-hint">
                        {textForLang(lang, 'This cloud workspace share is password-protected.', '该云端工作区分享设有密码。', '該雲端工作區分享設有密碼。')}
                    </div>
                    <input
                        type="password"
                        data-testid="task-cloud-share-join-password"
                        autoComplete="current-password"
                        autoFocus
                        disabled={busy}
                        value={password}
                        onChange={e => setPassword(e.target.value)}
                        onKeyDown={e => { selectAllInShareField(e); if (!e.defaultPrevented && e.key === 'Enter') void submit(); }}
                        className="cwshare-input"
                    />
                    {error ? <div role="alert" data-testid="task-cloud-share-join-error" className="cwshare-alert cwshare-alert--error">{error}</div> : null}
                </div>
                <div className="modal-footer cwshare-footer">
                    <button type="button" className="btn-secondary" disabled={busy} onClick={onClose}>{textForLang(lang, 'Cancel', '取消', '取消')}</button>
                    <button type="button" className="btn-primary" data-testid="task-cloud-share-join-submit" disabled={busy || !password.trim()} onClick={() => { void submit(); }}>
                        {busy ? textForLang(lang, 'Joining…', '正在加入…', '正在加入…') : textForLang(lang, 'Join', '加入', '加入')}
                    </button>
                </div>
            </div>
        </div>,
        document.body,
    );
}
