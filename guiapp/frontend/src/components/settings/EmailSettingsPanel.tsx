import { useCallback, useEffect, useRef, useState } from 'react';
import { useDialog } from '../CustomDialog';
import { IconMail } from '../ai/WorkbenchIcons';
import {
    MailboxAccountDelete,
    MailboxAccountSave,
    MailboxAccountSetDefault,
    MailboxAccountSetEnabled,
    MailboxAccountSetSecret,
    MailboxAccountTest,
    MailboxAccountsList,
    MailboxSignatureDelete,
    MailboxSignatureSave,
    MailboxSignatureSetDefault,
    MailboxSignaturesList,
} from '../../../wailsjs/go/main/App';

type EndpointSummary = {
    host?: string;
    port?: number;
    security?: string;
    username?: string;
    box?: string;
    from_address?: string;
};

type MailboxSummary = {
    id: string;
    name: string;
    address: string;
    enabled: boolean;
    default: boolean;
    has_inbound: boolean;
    has_outbound: boolean;
    has_inbound_secret: boolean;
    has_outbound_secret: boolean;
    signature_id?: string;
    inbound?: EndpointSummary;
    outbound?: EndpointSummary;
};

type SignatureSummary = {
    id: string;
    name: string;
    body: string;
    default: boolean;
};

type EndpointDraft = {
    host: string;
    port: string;
    security: string;
    username: string;
    box: string;
    from_address: string;
    password: string;
};

const emptyEndpoint = (): EndpointDraft => ({
    host: '', port: '', security: 'tls', username: '', box: 'INBOX', from_address: '', password: '',
});

function textFor(lang: string, en: string, hans: string, hant: string) {
    if (lang === 'en') return en;
    if (lang === 'zh-Hant') return hant;
    return hans;
}

function draftFromEndpoint(ep: EndpointSummary | undefined, inbound: boolean): EndpointDraft {
    return {
        host: ep?.host || '',
        port: ep?.port ? String(ep.port) : '',
        security: ep?.security || (inbound ? 'tls' : 'starttls'),
        username: ep?.username || '',
        box: inbound ? (ep?.box || 'INBOX') : '',
        from_address: ep?.from_address || '',
        password: '',
    };
}

// Blank port means the protocol default. Any other text must be a real port;
// dropping it would silently save that default instead of the value the user typed.
function resolvePort(draft: EndpointDraft): number | undefined | 'invalid' {
    if (!draft.host.trim()) return undefined;
    const text = draft.port.trim();
    if (!text) return undefined;
    if (!/^\d{1,5}$/.test(text)) return 'invalid';
    const port = Number(text);
    if (port < 1 || port > 65535) return 'invalid';
    return port;
}

function endpointPayload(draft: EndpointDraft, inbound: boolean, port?: number) {
    if (!draft.host.trim()) return undefined;
    return {
        host: draft.host.trim(),
        ...(port ? { port } : {}),
        security: draft.security || 'tls',
        username: draft.username.trim(),
        ...(inbound ? { box: draft.box.trim() || 'INBOX' } : {}),
        ...(!inbound && draft.from_address.trim() ? { from_address: draft.from_address.trim() } : {}),
    };
}

export function EmailSettingsPanel({ lang }: { lang: string }) {
    const t = useCallback((en: string, hans: string, hant: string) => textFor(lang, en, hans, hant), [lang]);
    const { showConfirm } = useDialog();
    const [accounts, setAccounts] = useState<MailboxSummary[]>([]);
    const [signatures, setSignatures] = useState<SignatureSummary[]>([]);
    const [notice, setNotice] = useState('');
    const [noticeTone, setNoticeTone] = useState<'ok' | 'err'>('ok');
    const [editingId, setEditingId] = useState('');
    const [name, setName] = useState('');
    const [address, setAddress] = useState('');
    const [enabled, setEnabled] = useState(true);
    const [makeDefault, setMakeDefault] = useState(false);
    const [signatureId, setSignatureId] = useState('');
    const [inbound, setInbound] = useState<EndpointDraft>(emptyEndpoint());
    const [outbound, setOutbound] = useState<EndpointDraft>({ ...emptyEndpoint(), security: 'starttls', box: '' });
    const [tests, setTests] = useState<Record<string, string>>({});
    const [sigName, setSigName] = useState('');
    const [sigBody, setSigBody] = useState('');
    const [sigDefault, setSigDefault] = useState(false);
    const [editingSig, setEditingSig] = useState('');
    const accountEditorRef = useRef<HTMLDivElement>(null);
    const signatureEditorRef = useRef<HTMLDivElement>(null);
    const editingIdRef = useRef('');
    const editingSigRef = useRef('');
    const resetAccountRef = useRef<() => void>(() => {});
    const resetSignatureRef = useRef<() => void>(() => {});
    const busyRef = useRef(false);
    const [busy, setBusy] = useState(false);
    editingIdRef.current = editingId;
    editingSigRef.current = editingSig;

    const report = (text: string, tone: 'ok' | 'err') => {
        setNoticeTone(tone);
        setNotice(text);
    };

    const reload = useCallback(async () => {
        const [nextAccounts, nextSigs] = await Promise.all([
            MailboxAccountsList() as Promise<MailboxSummary[]>,
            MailboxSignaturesList() as Promise<SignatureSummary[]>,
        ]);
        const loadedAccounts = nextAccounts || [];
        const loadedSignatures = nextSigs || [];
        setAccounts(loadedAccounts);
        setSignatures(loadedSignatures);
        setSignatureId(prev => (prev && !loadedSignatures.some(sig => sig.id === prev) ? '' : prev));
        if (editingIdRef.current && !loadedAccounts.some(item => item.id === editingIdRef.current)) {
            resetAccountRef.current();
        }
        if (editingSigRef.current && !loadedSignatures.some(item => item.id === editingSigRef.current)) {
            resetSignatureRef.current();
        }
        return { accounts: loadedAccounts, signatures: loadedSignatures };
    }, []);

    const runExclusive = async (action: () => Promise<void>) => {
        if (busyRef.current) return;
        busyRef.current = true;
        setBusy(true);
        try {
            await action();
        } finally {
            busyRef.current = false;
            setBusy(false);
        }
    };

    useEffect(() => {
        void reload().catch(err => {
            setNoticeTone('err');
            setNotice(String(err?.message || err));
        });
    }, [reload]);

    const resetAccount = () => {
        editingIdRef.current = '';
        setEditingId('');
        setName('');
        setAddress('');
        setEnabled(true);
        setMakeDefault(false);
        setSignatureId('');
        setInbound(emptyEndpoint());
        setOutbound({ ...emptyEndpoint(), security: 'starttls', box: '' });
    };
    resetAccountRef.current = resetAccount;

    const syncEditedFlags = (list: MailboxSummary[]) => {
        const id = editingIdRef.current;
        if (!id) return;
        const fresh = list.find(item => item.id === id);
        if (!fresh) return;
        setEnabled(fresh.enabled);
        setMakeDefault(fresh.default);
    };

    const commitAccountChange = async (action: () => Promise<unknown>) => {
        await runExclusive(async () => {
            try {
                await action();
                syncEditedFlags((await reload()).accounts);
            } catch (err: any) {
                report(String(err?.message || err), 'err');
            }
        });
    };

    const syncEditedSignature = (list: SignatureSummary[]) => {
        const id = editingSigRef.current;
        if (!id) return;
        const fresh = list.find(item => item.id === id);
        if (!fresh) return;
        setSigDefault(fresh.default);
    };

    const resetSignature = () => {
        editingSigRef.current = '';
        setSigName('');
        setSigBody('');
        setSigDefault(false);
        setEditingSig('');
    };
    resetSignatureRef.current = resetSignature;

    const saveAccount = async () => {
        await runExclusive(async () => { await saveAccountBody(); });
    };

    const clearAccountTests = (id: string) => {
        setTests(prev => {
            if (!prev[`${id}:inbound`] && !prev[`${id}:outbound`]) return prev;
            const next = { ...prev };
            delete next[`${id}:inbound`];
            delete next[`${id}:outbound`];
            return next;
        });
    };

    const saveAccountBody = async () => {
        setNotice('');
        const inboundPort = resolvePort(inbound);
        const outboundPort = resolvePort(outbound);
        if (inboundPort === 'invalid' || outboundPort === 'invalid') {
            report(t('Port must be between 1 and 65535.', '端口必须在 1 到 65535 之间', '連接埠必須在 1 到 65535 之間'), 'err');
            return;
        }
        try {
            const id = await MailboxAccountSave({
                ...(editingId ? { id: editingId } : {}),
                name, address, enabled,
                default: makeDefault,
                signature_id: signatureId,
                inbound: endpointPayload(inbound, true, inboundPort),
                outbound: endpointPayload(outbound, false, outboundPort),
            });
            clearAccountTests(id);
            editingIdRef.current = id;
            setEditingId(id);
            await reload();
            if (inbound.password.trim()) await MailboxAccountSetSecret(id, 'inbound', inbound.password);
            if (outbound.password.trim()) await MailboxAccountSetSecret(id, 'outbound', outbound.password);
            report(t('Saved', '已保存', '已保存'), 'ok');
            resetAccount();
            await reload();
        } catch (err: any) {
            report(String(err?.message || err), 'err');
        }
    };

    const saveSignature = async () => {
        await runExclusive(async () => { await saveSignatureBody(); });
    };

    const saveSignatureBody = async () => {
        setNotice('');
        try {
            await MailboxSignatureSave({
                ...(editingSig ? { id: editingSig } : {}),
                name: sigName,
                body: sigBody,
                default: sigDefault,
            });
            resetSignature();
            report(t('Saved', '已保存', '已保存'), 'ok');
            await reload();
        } catch (err: any) {
            report(String(err?.message || err), 'err');
        }
    };

    const editAccount = (account: MailboxSummary) => {
        if (busyRef.current) return;
        setEditingId(account.id);
        setName(account.name);
        setAddress(account.address);
        setEnabled(account.enabled);
        setMakeDefault(account.default);
        setSignatureId(account.signature_id || '');
        setInbound(draftFromEndpoint(account.inbound, true));
        setOutbound(draftFromEndpoint(account.outbound, false));
        requestAnimationFrame(() => accountEditorRef.current?.scrollIntoView({ block: 'nearest' }));
    };

    const editSignature = (sig: SignatureSummary) => {
        if (busyRef.current) return;
        setEditingSig(sig.id);
        setSigName(sig.name);
        setSigBody(sig.body);
        setSigDefault(sig.default);
        requestAnimationFrame(() => signatureEditorRef.current?.scrollIntoView({ block: 'nearest' }));
    };

    const confirmDelete = async (message: string, action: () => Promise<unknown>) => {
        if (busyRef.current) return;
        const confirmed = await showConfirm(
            message,
            t('Confirm delete', '确认删除', '確認刪除'),
            { confirmVariant: 'danger' },
        );
        if (!confirmed) return;
        await runExclusive(async () => {
            try {
                await action();
                const loaded = await reload();
                syncEditedFlags(loaded.accounts);
                syncEditedSignature(loaded.signatures);
            } catch (err: any) {
                report(String(err?.message || err), 'err');
            }
        });
    };

    const runTest = async (id: string, side: 'inbound' | 'outbound') => {
        try {
            const text = await MailboxAccountTest(id, side);
            setTests(prev => ({ ...prev, [`${id}:${side}`]: text }));
        } catch (err: any) {
            setTests(prev => ({ ...prev, [`${id}:${side}`]: String(err?.message || err) }));
        }
    };

    const startNewAccount = () => {
        if (busyRef.current) return;
        resetAccount();
        requestAnimationFrame(() => accountEditorRef.current?.scrollIntoView({ block: 'nearest' }));
    };

    const startNewSignature = () => {
        if (busyRef.current) return;
        resetSignature();
        requestAnimationFrame(() => signatureEditorRef.current?.scrollIntoView({ block: 'nearest' }));
    };

    const editingAccount = accounts.find(item => item.id === editingId);
    const editingSignature = signatures.find(item => item.id === editingSig);

    return (
        <div className="settings-content settings-panel email-settings-panel" data-testid="email-settings-panel">
            <p className="email-settings-lead">{t(
                'Manage the mailboxes used to send and receive mail, and the signatures appended to outgoing messages.',
                '管理可用于收发邮件的邮箱，以及随邮件附加的签名。',
                '管理可用於收發郵件的郵箱，以及隨郵件附加的簽名。',
            )}</p>
            {notice && <p className={`email-settings-notice${noticeTone === 'err' ? ' email-settings-notice--err' : ''}`} role="status" data-testid="email-settings-notice">{notice}</p>}

            <section className="system-settings-card email-settings-section" aria-labelledby="email-settings-mailboxes">
                <div className="email-settings-section__head">
                    <div className="email-settings-section__title">
                        <h4 id="email-settings-mailboxes">{t('Mailboxes', '邮箱', '郵箱')}</h4>
                        <p className="email-settings-section__desc">{t(
                            'Paused mailboxes are skipped; the default mailbox is used when a task does not name one.',
                            '已停用的邮箱不参与收发；不指定邮箱名时使用默认邮箱。',
                            '已停用的郵箱不參與收發；不指定郵箱名時使用預設郵箱。',
                        )}</p>
                    </div>
                    <button type="button" className="email-settings-button email-settings-button--add" disabled={busy} onClick={startNewAccount}>{t('Add mailbox', '新增邮箱', '新增郵箱')}</button>
                </div>
                {accounts.length === 0 ? <p className="email-settings-empty">{t('No mailboxes yet.', '还没有邮箱。', '還沒有郵箱。')}</p> : (
                    <ul className="email-settings-list">
                        {accounts.map(account => (
                            <li key={account.id} className={`email-settings-row${editingId === account.id ? ' email-settings-row--editing' : ''}`} data-testid={`mailbox-account-${account.id}`}>
                                <div className="email-settings-row__head">
                                    <span className="email-settings-avatar" aria-hidden="true"><IconMail size={16} /></span>
                                    <div className="email-settings-row__main">
                                        <div className="email-settings-row__title">
                                            <strong>{account.name}</strong>
                                            <span className="email-settings-address">{account.address}</span>
                                            {account.default && <span className="email-settings-badge">{t('default', '默认', '預設')}</span>}
                                            {!account.enabled && <span className="email-settings-badge email-settings-badge--muted">{t('paused', '已停用', '已停用')}</span>}
                                        </div>
                                        {(account.has_inbound || account.has_outbound) && <div className="email-settings-meta">
                                            {account.has_inbound && <span className={`email-settings-meta__item${account.has_inbound_secret ? ' email-settings-meta__item--ok' : ''}`}>{t('Receive password', '收件密码', '收件密碼')}: {account.has_inbound_secret ? t('saved', '已保存', '已保存') : t('not set', '未设置', '未設定')}</span>}
                                            {account.has_outbound && <span className={`email-settings-meta__item${account.has_outbound_secret ? ' email-settings-meta__item--ok' : ''}`}>{t('Send password', '发件密码', '發件密碼')}: {account.has_outbound_secret ? t('saved', '已保存', '已保存') : t('not set', '未设置', '未設定')}</span>}
                                        </div>}
                                    </div>
                                    <div className="email-settings-row__actions">
                                        <button type="button" className="email-settings-button" disabled={busy} onClick={() => editAccount(account)}>{t('Edit', '编辑', '編輯')}</button>
                                        <button type="button" className="email-settings-button email-settings-button--ghost" disabled={busy} onClick={() => { void commitAccountChange(() => MailboxAccountSetEnabled(account.id, !account.enabled)); }}>{account.enabled ? t('Pause', '停用', '停用') : t('Enable', '启用', '啟用')}</button>
                                        {!account.default && account.enabled && <button type="button" className="email-settings-button email-settings-button--ghost" disabled={busy} onClick={() => { void commitAccountChange(() => MailboxAccountSetDefault(account.id)); }}>{t('Make default', '设为默认', '設為預設')}</button>}
                                        {account.has_inbound && <button type="button" className="email-settings-button email-settings-button--ghost" disabled={busy} onClick={() => { void runExclusive(async () => { await runTest(account.id, 'inbound'); }); }}>{t('Test receive', '测试收件', '測試收件')}</button>}
                                        {account.has_outbound && <button type="button" className="email-settings-button email-settings-button--ghost" disabled={busy} onClick={() => { void runExclusive(async () => { await runTest(account.id, 'outbound'); }); }}>{t('Test send', '测试发件', '測試發件')}</button>}
                                        <button type="button" className="email-settings-button email-settings-button--danger" disabled={busy} onClick={() => { void confirmDelete(t(`Delete mailbox “${account.name}”? This cannot be undone.`, `删除邮箱“${account.name}”？此操作不可撤销。`, `刪除郵箱“${account.name}”？此操作不可撤銷。`), async () => { await MailboxAccountDelete(account.id); clearAccountTests(account.id); if (editingIdRef.current === account.id) resetAccount(); }); }}>{t('Delete', '删除', '刪除')}</button>
                                    </div>
                                </div>
                                {(tests[`${account.id}:inbound`] || tests[`${account.id}:outbound`]) && (
                                    <div className="email-settings-tests" role="status">
                                        {tests[`${account.id}:inbound`] && <p className={`email-settings-test email-settings-test--${tests[`${account.id}:inbound`].endsWith('正常') ? 'ok' : 'err'}`}>{tests[`${account.id}:inbound`]}</p>}
                                        {tests[`${account.id}:outbound`] && <p className={`email-settings-test email-settings-test--${tests[`${account.id}:outbound`].endsWith('正常') ? 'ok' : 'err'}`}>{tests[`${account.id}:outbound`]}</p>}
                                    </div>
                                )}
                            </li>
                        ))}
                    </ul>
                )}

                <div className="email-settings-editor" ref={accountEditorRef}>
                    <div className={`email-settings-editing${editingAccount ? '' : ' email-settings-editing--new'}`} role="status">
                        {editingAccount
                            ? t(`Editing “${editingAccount.name}”`, `正在编辑“${editingAccount.name}”`, `正在編輯“${editingAccount.name}”`)
                            : t('New mailbox', '新增邮箱', '新增郵箱')}
                    </div>
                    <div className="email-settings-grid email-settings-grid--account">
                        <label className="email-settings-field">{t('Name', '名字', '名字')}<input className="form-input" value={name} onChange={event => setName(event.target.value)} /></label>
                        <label className="email-settings-field">{t('Address', '邮箱地址', '郵箱地址')}<input className="form-input" value={address} onChange={event => setAddress(event.target.value)} /></label>
                        <label className="email-settings-field">{t('Signature', '签名', '簽名')}
                            <select className="form-input" value={signatureId} onChange={event => setSignatureId(event.target.value)}>
                                <option value="">{t('Use the public default', '使用公共默认签名', '使用公共預設簽名')}</option>
                                {signatures.map(sig => <option key={sig.id} value={sig.id}>{sig.name}</option>)}
                            </select>
                        </label>
                    </div>
                    <fieldset className="email-settings-endpoint">
                        <legend>{t('Receive (IMAP)', '收件 (IMAP)', '收件 (IMAP)')}</legend>
                        <div className="email-settings-endpoint__grid">
                            <label className="email-settings-field">{t('Host', '主机', '主機')}<input className="form-input" placeholder={t('Host', '主机', '主機')} value={inbound.host} onChange={event => setInbound({ ...inbound, host: event.target.value })} /></label>
                            <label className="email-settings-field">{t('Port', '端口', '連接埠')}<input className="form-input" placeholder={t('Port', '端口', '連接埠')} value={inbound.port} onChange={event => setInbound({ ...inbound, port: event.target.value })} /></label>
                            <label className="email-settings-field">{t('Security', '安全', '安全')}<select className="form-input" value={inbound.security} onChange={event => setInbound({ ...inbound, security: event.target.value })}>
                                <option value="tls">TLS</option>
                                <option value="starttls">STARTTLS</option>
                                <option value="none">none</option>
                            </select></label>
                            <label className="email-settings-field">{t('Username', '用户名', '使用者名稱')}<input className="form-input" placeholder={t('Username', '用户名', '使用者名稱')} value={inbound.username} onChange={event => setInbound({ ...inbound, username: event.target.value })} /></label>
                            <label className="email-settings-field">{t('Folder', '文件夹', '資料夾')}<input className="form-input" placeholder={t('Folder', '文件夹', '資料夾')} value={inbound.box} onChange={event => setInbound({ ...inbound, box: event.target.value })} /></label>
                            <label className="email-settings-field">{t('Password', '密码', '密碼')}<input className="form-input" type="password" autoComplete="new-password" placeholder={t('Password or app password, blank keeps the saved one', '密码或授权码，留空则不修改', '密碼或授權碼，留空則不修改')} value={inbound.password} onChange={event => setInbound({ ...inbound, password: event.target.value })} /><span className="email-settings-hint">{t('Blank keeps the saved password or app password.', '留空则不修改已保存的密码或授权码。', '留空則不修改已保存的密碼或授權碼。')}</span></label>
                        </div>
                    </fieldset>
                    <fieldset className="email-settings-endpoint">
                        <legend>{t('Send (SMTP)', '发件 (SMTP)', '發件 (SMTP)')}</legend>
                        <div className="email-settings-endpoint__grid">
                            <label className="email-settings-field">{t('Host', '主机', '主機')}<input className="form-input" placeholder={t('Host', '主机', '主機')} value={outbound.host} onChange={event => setOutbound({ ...outbound, host: event.target.value })} /></label>
                            <label className="email-settings-field">{t('Port', '端口', '連接埠')}<input className="form-input" placeholder={t('Port', '端口', '連接埠')} value={outbound.port} onChange={event => setOutbound({ ...outbound, port: event.target.value })} /></label>
                            <label className="email-settings-field">{t('Security', '安全', '安全')}<select className="form-input" value={outbound.security} onChange={event => setOutbound({ ...outbound, security: event.target.value })}>
                                <option value="tls">TLS</option>
                                <option value="starttls">STARTTLS</option>
                                <option value="none">none</option>
                            </select></label>
                            <label className="email-settings-field">{t('Username', '用户名', '使用者名稱')}<input className="form-input" placeholder={t('Username', '用户名', '使用者名稱')} value={outbound.username} onChange={event => setOutbound({ ...outbound, username: event.target.value })} /></label>
                            <label className="email-settings-field">{t('From address', '发件地址', '發件地址')}<input className="form-input" placeholder={t('From address', '发件地址', '發件地址')} value={outbound.from_address} onChange={event => setOutbound({ ...outbound, from_address: event.target.value })} /></label>
                            <label className="email-settings-field">{t('Password', '密码', '密碼')}<input className="form-input" type="password" autoComplete="new-password" placeholder={t('Password or app password, blank keeps the saved one', '密码或授权码，留空则不修改', '密碼或授權碼，留空則不修改')} value={outbound.password} onChange={event => setOutbound({ ...outbound, password: event.target.value })} /><span className="email-settings-hint">{t('Blank keeps the saved password or app password.', '留空则不修改已保存的密码或授权码。', '留空則不修改已保存的密碼或授權碼。')}</span></label>
                        </div>
                    </fieldset>
                    <div className="email-settings-editor__footer">
                        <label className="email-settings-check"><input type="checkbox" checked={enabled} onChange={event => setEnabled(event.target.checked)} />{t('Enabled', '启用', '啟用')}</label>
                        <label className="email-settings-check"><input type="checkbox" checked={makeDefault} onChange={event => setMakeDefault(event.target.checked)} />{t('Default mailbox', '默认邮箱', '預設郵箱')}</label>
                        <div className="email-settings-editor__actions">
                            {editingId && <button type="button" className="email-settings-button" disabled={busy} onClick={() => { if (busyRef.current) return; resetAccount(); }}>{t('Cancel', '取消', '取消')}</button>}
                            <button type="button" className="email-settings-button email-settings-button--save" disabled={busy} onClick={() => { void saveAccount(); }}>{t('Save', '保存', '保存')}</button>
                        </div>
                    </div>
                </div>
            </section>

            <section className="system-settings-card email-settings-section" aria-labelledby="email-settings-signatures">
                <div className="email-settings-section__head">
                    <div className="email-settings-section__title">
                        <h4 id="email-settings-signatures">{t('Signatures', '签名', '簽名')}</h4>
                        <p className="email-settings-section__desc">{t(
                            "Appended to outgoing mail. {{邮箱名}}, {{邮箱}} and {{发件人}} are replaced with the sending mailbox's details; a mailbox can pick its own signature, otherwise the public default applies.",
                            '发信时附加在邮件末尾。{{邮箱名}}、{{邮箱}}、{{发件人}} 会替换为发件邮箱的信息；邮箱可指定自己的签名，未指定时使用公共默认签名。',
                            '發信時附加在郵件末尾。{{邮箱名}}、{{邮箱}}、{{发件人}} 會替換為發件郵箱的信息；郵箱可指定自己的簽名，未指定時使用公共預設簽名。',
                        )}</p>
                    </div>
                    <button type="button" className="email-settings-button email-settings-button--add" disabled={busy} onClick={startNewSignature}>{t('Add signature', '新增签名', '新增簽名')}</button>
                </div>
                {signatures.length === 0 ? <p className="email-settings-empty">{t('No signatures yet.', '还没有签名。', '還沒有簽名。')}</p> : (
                    <ul className="email-settings-list">
                        {signatures.map(sig => (
                            <li key={sig.id} className={`email-settings-row${editingSig === sig.id ? ' email-settings-row--editing' : ''}`}>
                                <div className="email-settings-row__head">
                                    <div className="email-settings-row__main">
                                        <div className="email-settings-row__title">
                                            <strong>{sig.name}</strong>
                                            {sig.default && <span className="email-settings-badge">{t('public default', '公共默认', '公共預設')}</span>}
                                        </div>
                                    </div>
                                    <div className="email-settings-row__actions">
                                        <button type="button" className="email-settings-button" disabled={busy} onClick={() => editSignature(sig)}>{t('Edit', '编辑', '編輯')}</button>
                                        {!sig.default && <button type="button" className="email-settings-button email-settings-button--ghost" disabled={busy} onClick={() => { void runExclusive(async () => { try { await MailboxSignatureSetDefault(sig.id); syncEditedSignature((await reload()).signatures); } catch (err: any) { report(String(err?.message || err), 'err'); } }); }}>{t('Make public default', '设为公共默认', '設為公共預設')}</button>}
                                        <button type="button" className="email-settings-button email-settings-button--danger" disabled={busy} onClick={() => { void confirmDelete(t(`Delete signature “${sig.name}”? This cannot be undone.`, `删除签名“${sig.name}”？此操作不可撤销。`, `刪除簽名“${sig.name}”？此操作不可撤銷。`), async () => { await MailboxSignatureDelete(sig.id); if (editingSigRef.current === sig.id) resetSignature(); }); }}>{t('Delete', '删除', '刪除')}</button>
                                    </div>
                                </div>
                                <pre className="email-settings-signature">{sig.body}</pre>
                            </li>
                        ))}
                    </ul>
                )}
                <div className="email-settings-editor" ref={signatureEditorRef}>
                    <div className={`email-settings-editing${editingSignature ? '' : ' email-settings-editing--new'}`} role="status">
                        {editingSignature
                            ? t(`Editing “${editingSignature.name}”`, `正在编辑“${editingSignature.name}”`, `正在編輯“${editingSignature.name}”`)
                            : t('New signature', '新增签名', '新增簽名')}
                    </div>
                    <div className="email-settings-grid email-settings-grid--signature">
                        <label className="email-settings-field">{t('Signature name', '签名名字', '簽名名字')}<input className="form-input" value={sigName} onChange={event => setSigName(event.target.value)} /></label>
                        <label className="email-settings-field">{t('Signature', '签名内容', '簽名內容')}<textarea className="form-input" value={sigBody} onChange={event => setSigBody(event.target.value)} rows={4} /></label>
                    </div>
                    <div className="email-settings-editor__footer">
                        <label className="email-settings-check"><input type="checkbox" checked={sigDefault} onChange={event => setSigDefault(event.target.checked)} />{t('Public default', '公共默认签名', '公共預設簽名')}</label>
                        <div className="email-settings-editor__actions">
                            {editingSig && <button type="button" className="email-settings-button" disabled={busy} onClick={() => { if (busyRef.current) return; resetSignature(); }}>{t('Cancel', '取消', '取消')}</button>}
                            <button type="button" className="email-settings-button email-settings-button--save" disabled={busy} onClick={() => { void saveSignature(); }}>{t('Save signature', '保存签名', '保存簽名')}</button>
                        </div>
                    </div>
                </div>
            </section>
        </div>
    );
}
