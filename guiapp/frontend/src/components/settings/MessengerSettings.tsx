import { Dispatch, SetStateAction, useEffect, useRef, useState } from 'react';
import { GetMessengerConnectionError, GetMessengerStatus, RestartMessenger } from '../../../wailsjs/go/main/App';
import { corelib } from '../../../wailsjs/go/models';
import { BrowserOpenURL, EventsOn } from '../../../wailsjs/runtime';
import { ConnectionStatusBadge } from './ConnectionStatusBadge';
import { restartLabel, textForLang, watchLabel } from './imSettingsShared';

type MessengerConfig = {
    messenger_enabled?: boolean;
    messenger_page_id?: string;
    messenger_page_access_token?: string;
    messenger_app_secret?: string;
    messenger_verify_token?: string;
    messenger_graph_api_version?: string;
    messenger_webhook_path?: string;
    messenger_listen_host?: string;
    messenger_listen_port?: number;
    messenger_allow_from?: string[];
    messenger_dm_policy?: string;
    messenger_default_psid?: string;
    messenger_public_base_url?: string;
};

type MessengerSettingsProps = {
    config: corelib.AppConfig | null;
    setConfig: Dispatch<SetStateAction<corelib.AppConfig | null>>;
    lang: string;
    saveRemoteConfigField: (patch: Record<string, any>) => any;
    setIMAuditPlatform: Dispatch<SetStateAction<string | null>>;
};

const DEFAULT_PATH = '/facebook-messenger/webhook';
const DEFAULT_PORT = 8791;
const MESSENGER_INCOMPLETE = 'Facebook Messenger needs a Page ID, page access token, app secret, and verify token';

// createLatestTicket numbers overlapping status reads. An older read must not
// overwrite the gateway state observed by a newer read.
export function createLatestTicket(): { issue: () => number; matches: (ticket: number) => boolean } {
    let current = 0;
    return {
        issue() {
            current += 1;
            return current;
        },
        matches(ticket: number) {
            return ticket === current;
        },
    };
}

function webhookPath(raw: string | undefined): string {
    let path = (raw || '').trim();
    if (!path || /^\/+$/.test(path)) return DEFAULT_PATH;
    if (!path.startsWith('/')) path = `/${path}`;
    if (path.length > 1) path = path.replace(/\/+$/, '');
    return path || DEFAULT_PATH;
}

function listenHostForURL(raw: string | undefined): string {
    const host = (raw || '127.0.0.1').trim() || '127.0.0.1';
    if (host === '0.0.0.0' || host === '::' || host === '[::]') return '127.0.0.1';
    if (host.includes(':') && !host.startsWith('[')) return `[${host}]`;
    return host;
}

export function messengerPublicBaseIsInsecure(base: string | undefined): boolean {
    return String(base || '').trim().toLowerCase().startsWith('http://');
}

function publicBaseForCallback(raw: string | undefined): string {
    const trimmed = String(raw || '').trim();
    if (!trimmed) return '';
    try {
        const url = new URL(trimmed);
        if (url.protocol !== 'http:' && url.protocol !== 'https:') return trimmed.replace(/\/+$/, '');
        url.username = '';
        url.password = '';
        url.search = '';
        url.hash = '';
        return url.toString().replace(/\/+$/, '');
    } catch {
        return trimmed.replace(/\/+$/, '');
    }
}

export function messengerCallbackURL(config: MessengerConfig | null | undefined): { callback: string; local: string } {
    const path = webhookPath(config?.messenger_webhook_path);
    const port = Number(config?.messenger_listen_port) > 0 ? Number(config?.messenger_listen_port) : DEFAULT_PORT;
    const local = `http://${listenHostForURL(config?.messenger_listen_host)}:${port}${path}`;
    const base = publicBaseForCallback(config?.messenger_public_base_url);
    if (!base) return { callback: local, local };
    const callback = base.endsWith(path) ? base : `${base}${path}`;
    return { callback, local };
}

function messengerDetailText(detail: string, missing: boolean, lang: string): string {
    if (missing && (!detail || detail === MESSENGER_INCOMPLETE)) {
        return textForLang(
            lang,
            'Add a Page ID, page access token, app secret, and verify token.',
            '请填写 Page ID、主页访问令牌、应用密钥和验证口令。',
            '請填寫 Page ID、粉絲專頁存取權杖、應用程式密鑰和驗證口令。',
        );
    }
    return detail;
}

function useBlurField(saved: string) {
    const [draft, setDraft] = useState(saved);
    const [focused, setFocused] = useState(false);
    const draftRef = useRef(saved);
    const savedRef = useRef(saved);
    savedRef.current = saved;
    useEffect(() => {
        if (!focused) {
            draftRef.current = saved;
            setDraft(saved);
        }
    }, [saved, focused]);
    return {
        value: draft,
        onFocus: () => {
            draftRef.current = savedRef.current;
            setDraft(savedRef.current);
            setFocused(true);
        },
        onChange: (value: string) => {
            draftRef.current = value;
            setDraft(value);
        },
        commit: (save: (value: string) => void) => {
            setFocused(false);
            const next = draftRef.current;
            if (next.trim() === savedRef.current.trim()) return;
            save(next);
        },
    };
}

function allowText(ids: string[] | undefined): string {
    return (ids || []).join('\n');
}

export const MessengerSettings = ({
    config,
    lang,
    saveRemoteConfigField,
    setIMAuditPlatform,
}: MessengerSettingsProps) => {
    const messenger = config as (corelib.AppConfig & MessengerConfig) | null;
    const [status, setStatus] = useState('disconnected');
    const [detail, setDetail] = useState('');
    const statusTicket = useRef(createLatestTicket());
    const refreshStatusRef = useRef<() => void>(() => {});
    const [allowDraft, setAllowDraft] = useState(allowText(messenger?.messenger_allow_from));
    const [allowFocused, setAllowFocused] = useState(false);
    const allowDraftRef = useRef(allowDraft);
    const savedAllow = allowText(messenger?.messenger_allow_from);
    const enabled = messenger?.messenger_enabled === true;
    const pageID = useBlurField(messenger?.messenger_page_id || '');
    const pageToken = useBlurField(messenger?.messenger_page_access_token || '');
    const appSecret = useBlurField(messenger?.messenger_app_secret || '');
    const verifyToken = useBlurField(messenger?.messenger_verify_token || '');
    const defaultPSID = useBlurField(messenger?.messenger_default_psid || '');
    const publicBase = useBlurField(messenger?.messenger_public_base_url || '');
    const webhookPathField = useBlurField(messenger?.messenger_webhook_path || '');
    const listenHost = useBlurField(messenger?.messenger_listen_host || '');
    const listenPort = useBlurField(messenger?.messenger_listen_port ? String(messenger.messenger_listen_port) : '');
    const graphVersion = useBlurField(messenger?.messenger_graph_api_version || '');
    const urls = messengerCallbackURL({
        ...messenger,
        messenger_public_base_url: publicBase.value,
        messenger_webhook_path: webhookPathField.value,
        messenger_listen_host: listenHost.value,
        messenger_listen_port: listenPort.value.trim() === '' ? 0 : Number(listenPort.value),
    });
    const missingCreds = enabled && !(
        (messenger?.messenger_page_id || '').trim()
        && (messenger?.messenger_page_access_token || '').trim()
        && (messenger?.messenger_app_secret || '').trim()
        && (messenger?.messenger_verify_token || '').trim()
    );
    const shownDetail = messengerDetailText(detail, missingCreds, lang);

    useEffect(() => {
        if (!allowFocused) {
            allowDraftRef.current = savedAllow;
            setAllowDraft(savedAllow);
        }
    }, [savedAllow, allowFocused]);

    useEffect(() => {
        let disposed = false;
        const refresh = () => {
            const ticket = statusTicket.current.issue();
            void GetMessengerStatus().then((next) => {
                if (!disposed && statusTicket.current.matches(ticket)) setStatus(next || 'disconnected');
            }).catch(() => {});
            void GetMessengerConnectionError().then((next) => {
                if (!disposed && statusTicket.current.matches(ticket)) setDetail(next || '');
            }).catch(() => {});
        };
        refreshStatusRef.current = refresh;
        refresh();
        const off = EventsOn('messenger-status-changed', () => {
            refresh();
        });
        return () => {
            disposed = true;
            refreshStatusRef.current = () => {};
            if (typeof off === 'function') off();
        };
    }, [enabled]);

    const commitAllow = () => {
        setAllowFocused(false);
        const ids = allowDraftRef.current.split(/[\s,]+/).map((item) => item.trim()).filter(Boolean);
        const prev = messenger?.messenger_allow_from || [];
        if (ids.join('\n') === prev.join('\n')) return;
        void saveRemoteConfigField({ messenger_allow_from: ids });
    };

    return (
        <section className="im-settings-card im-settings-channel">
            <p className="im-settings-description">
                {textForLang(
                    lang,
                    'Connect a Facebook Page through the Messenger Platform. MaClaw listens for Meta webhooks and replies with the Page access token. Meta requires a public HTTPS callback, so point a tunnel at the local listener.',
                    '用 Facebook 主页的 Messenger 平台收消息。MaClaw 在本机接收 Meta Webhook，并用主页访问令牌回复。Meta 要求公网 HTTPS 回调，请把隧道指到下面的本地地址。',
                    '用 Facebook 粉絲專頁的 Messenger 平台收訊息。MaClaw 在本機接收 Meta Webhook，並用粉絲專頁存取權杖回覆。Meta 要求公開 HTTPS 回呼，請把隧道指到下面的本機位址。',
                )}
            </p>
            <div className="im-settings-toolbar">
                <label className="im-settings-toggle">
                    <input
                        type="checkbox"
                        aria-label={textForLang(lang, 'Enable Facebook Messenger', '启用 Facebook Messenger', '啟用 Facebook Messenger')}
                        checked={enabled}
                        onChange={(e) => { void saveRemoteConfigField({ messenger_enabled: e.target.checked }); }}
                    />
                    <span>{textForLang(lang, 'Enable Facebook Messenger', '启用 Facebook Messenger', '啟用 Facebook Messenger')}</span>
                </label>
                <button type="button" className="im-settings-button im-settings-button--accent" onClick={() => BrowserOpenURL('https://github.com/rubysworld/openclaw-facebook-messenger#meta-app-setup')}>
                    {textForLang(lang, 'Tutorial', '教程', '教程')}
                </button>
                {enabled && (
                    <>
                        <ConnectionStatusBadge status={status} lang={lang} />
                        <button type="button" className="im-settings-button" onClick={() => { void RestartMessenger().finally(() => refreshStatusRef.current()); }}>
                            {restartLabel(lang)}
                        </button>
                    </>
                )}
                <button type="button" className="im-settings-button im-settings-button--audit" onClick={() => setIMAuditPlatform('messenger_local')}>
                    {watchLabel(lang)}
                </button>
            </div>
            <div className="im-settings-grid">
                <label className="im-settings-field">
                    <span>Page ID</span>
                    <input value={pageID.value} autoComplete="off" placeholder="Facebook Page ID" onFocus={pageID.onFocus} onChange={(e) => pageID.onChange(e.target.value)} onBlur={() => pageID.commit((value) => { void saveRemoteConfigField({ messenger_page_id: value }); })} />
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Page access token', '主页访问令牌', '粉絲專頁存取權杖')}</span>
                    <input type="password" value={pageToken.value} autoComplete="off" onFocus={pageToken.onFocus} onChange={(e) => pageToken.onChange(e.target.value)} onBlur={() => pageToken.commit((value) => { void saveRemoteConfigField({ messenger_page_access_token: value }); })} />
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Meta app secret', 'Meta 应用密钥', 'Meta 應用程式密鑰')}</span>
                    <input type="password" value={appSecret.value} autoComplete="off" onFocus={appSecret.onFocus} onChange={(e) => appSecret.onChange(e.target.value)} onBlur={() => appSecret.commit((value) => { void saveRemoteConfigField({ messenger_app_secret: value }); })} />
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Webhook verify token', 'Webhook 验证口令', 'Webhook 驗證口令')}</span>
                    <input type="password" value={verifyToken.value} autoComplete="off" onFocus={verifyToken.onFocus} onChange={(e) => verifyToken.onChange(e.target.value)} onBlur={() => verifyToken.commit((value) => { void saveRemoteConfigField({ messenger_verify_token: value }); })} />
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Who can message', '谁可以发消息', '誰可以發訊息')}</span>
                    <select
                        aria-label={textForLang(lang, 'Who can message', '谁可以发消息', '誰可以發訊息')}
                        value={messenger?.messenger_dm_policy || 'allowlist'}
                        onChange={(e) => { void saveRemoteConfigField({ messenger_dm_policy: e.target.value }); }}
                    >
                        <option value="allowlist">{textForLang(lang, 'Allow list', '仅允许名单', '僅允許名單')}</option>
                        <option value="open">{textForLang(lang, 'Anyone', '所有人', '所有人')}</option>
                        <option value="disabled">{textForLang(lang, 'Ignore inbound', '忽略收到的消息', '忽略收到的訊息')}</option>
                    </select>
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Default PSID', '默认 PSID', '預設 PSID')}</span>
                    <input value={defaultPSID.value} autoComplete="off" placeholder={textForLang(lang, 'PSID used when a task says self', '定时任务发给自己时使用的 PSID', '定時任務發給自己時使用的 PSID')} onFocus={defaultPSID.onFocus} onChange={(e) => defaultPSID.onChange(e.target.value)} onBlur={() => defaultPSID.commit((value) => { void saveRemoteConfigField({ messenger_default_psid: value }); })} />
                </label>
            </div>
            <label className="im-settings-field" style={{ marginTop: 12 }}>
                <span>{textForLang(lang, 'Allowed PSIDs', '允许的 PSID', '允許的 PSID')}</span>
                <textarea
                    rows={3}
                    value={allowDraft}
                    placeholder={textForLang(lang, 'One Page-scoped ID per line. Use * to allow every sender.', '每行一个 PSID。填 * 表示允许所有人。', '每行一個 PSID。填 * 表示允許所有人。')}
                    onFocus={() => {
                        allowDraftRef.current = savedAllow;
                        setAllowDraft(savedAllow);
                        setAllowFocused(true);
                    }}
                    onChange={(e) => {
                        allowDraftRef.current = e.target.value;
                        setAllowDraft(e.target.value);
                    }}
                    onBlur={commitAllow}
                />
            </label>
            {(messenger?.messenger_dm_policy || 'allowlist') === 'allowlist' && (messenger?.messenger_allow_from || []).length === 0 && (
                <p className="im-settings-description" style={{ marginTop: 8, marginBottom: 0 }}>
                    {textForLang(
                        lang,
                        'An empty allow list drops every sender. Add PSIDs, or choose Anyone.',
                        '允许名单为空时，所有发信人都会被丢弃。请填写 PSID，或改成所有人。',
                        '允許名單為空時，所有發訊人都會被捨棄。請填寫 PSID，或改成所有人。',
                    )}
                </p>
            )}
            <div className="im-settings-grid" style={{ marginTop: 12 }}>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Public callback base URL', '公网回调根地址', '公開回呼根網址')}</span>
                    <input value={publicBase.value} autoComplete="off" placeholder="https://example.com" onFocus={publicBase.onFocus} onChange={(e) => publicBase.onChange(e.target.value)} onBlur={() => publicBase.commit((value) => { void saveRemoteConfigField({ messenger_public_base_url: value }); })} />
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Callback URL for Meta', '填到 Meta 的回调地址', '填到 Meta 的回呼網址')}</span>
                    <input readOnly value={urls.callback} />
                </label>
                {messengerPublicBaseIsInsecure(publicBase.value) && (
                    <p className="im-settings-description" style={{ margin: 0 }}>
                        {textForLang(
                            lang,
                            'Meta only calls an HTTPS address. An http:// base cannot be the callback.',
                            'Meta 只接受 HTTPS 回调。http:// 地址不能填到 Meta。',
                            'Meta 只接受 HTTPS 回呼。http:// 網址不能填到 Meta。',
                        )}
                    </p>
                )}
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Local listener', '本机监听地址', '本機監聽位址')}</span>
                    <input readOnly value={urls.local} />
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Webhook path', 'Webhook 路径', 'Webhook 路徑')}</span>
                    <input value={webhookPathField.value} placeholder={DEFAULT_PATH} onFocus={webhookPathField.onFocus} onChange={(e) => webhookPathField.onChange(e.target.value)} onBlur={() => webhookPathField.commit((value) => { void saveRemoteConfigField({ messenger_webhook_path: value }); })} />
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Listen host', '监听地址', '監聽位址')}</span>
                    <input value={listenHost.value} placeholder="127.0.0.1" onFocus={listenHost.onFocus} onChange={(e) => listenHost.onChange(e.target.value)} onBlur={() => listenHost.commit((value) => { void saveRemoteConfigField({ messenger_listen_host: value }); })} />
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Listen port', '监听端口', '監聽埠')}</span>
                    <input
                        type="number"
                        min={1}
                        max={65535}
                        value={listenPort.value}
                        placeholder={String(DEFAULT_PORT)}
                        onFocus={listenPort.onFocus}
                        onChange={(e) => listenPort.onChange(e.target.value)}
                        onBlur={() => listenPort.commit((raw) => {
                            const trimmed = raw.trim();
                            const port = trimmed === '' ? 0 : Number(trimmed);
                            if (trimmed !== '' && (!Number.isInteger(port) || port < 1 || port > 65535)) return;
                            void saveRemoteConfigField({ messenger_listen_port: port });
                        })}
                    />
                </label>
                <label className="im-settings-field">
                    <span>{textForLang(lang, 'Graph API version', 'Graph API 版本', 'Graph API 版本')}</span>
                    <input value={graphVersion.value} placeholder="v25.0" onFocus={graphVersion.onFocus} onChange={(e) => graphVersion.onChange(e.target.value)} onBlur={() => graphVersion.commit((value) => { void saveRemoteConfigField({ messenger_graph_api_version: value }); })} />
                </label>
            </div>
            <p className="im-settings-description" style={{ marginTop: 8, marginBottom: 0 }}>
                {textForLang(
                    lang,
                    'In the Meta app, subscribe the Page to messages, messaging_postbacks, messaging_referrals, message_edits, message_deliveries, and message_reads. The verify token must match. Replies use the standard 24-hour response window.',
                    '在 Meta 应用里把主页订阅到 messages、messaging_postbacks、messaging_referrals、message_edits、message_deliveries 和 message_reads。验证口令必须一致。回复走 Messenger 普通的 24 小时窗口。',
                    '在 Meta 應用程式裡把粉絲專頁訂閱到 messages、messaging_postbacks、messaging_referrals、message_edits、message_deliveries 和 message_reads。驗證口令必須一致。回覆走 Messenger 一般的 24 小時窗口。',
                )}
            </p>
            {shownDetail && <p className="im-settings-description" role="alert">{shownDetail}</p>}
        </section>
    );
};
