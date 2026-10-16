// Dev-only preview harness for EmailSettingsPanel.
// Serve with the vite dev server and open /email-preview.html — the Wails
// bridge is faked here so the panel renders with sample mailboxes and
// signatures outside the desktop shell.
import { createRoot } from 'react-dom/client';
import './App.css';
import { DialogProvider } from './components/CustomDialog';
import { EmailSettingsPanel } from './components/settings/EmailSettingsPanel';

// ?theme=dark|light mirrors how the real shell puts data-ai-theme on #App; the
// dark token blocks key off that attribute and cascade down from it.
// ?scheme=onyx|graphite|classic|aurora picks a named dark scheme like the shell.
// ?fixture=empty renders the no-mailboxes / no-signatures empty states.
const params = new URLSearchParams(window.location.search);
const themeParam = params.get('theme') || 'light';
const schemeParam = params.get('scheme');
const emptyFixture = params.get('fixture') === 'empty';
document.documentElement.setAttribute('data-boot-theme', themeParam);

const appAttrs: Record<string, string> = { 'data-nav-tab': 'settings', 'data-ai-theme': themeParam };
if (themeParam === 'dark' && schemeParam) appAttrs['data-ai-dark-scheme'] = schemeParam;

const accounts = emptyFixture ? [] : [
    {
        id: 'mb1',
        name: '工作邮箱',
        address: 'zhangsan@example.com',
        enabled: true,
        default: true,
        has_inbound: true,
        has_outbound: true,
        has_inbound_secret: true,
        has_outbound_secret: true,
        signature_id: 's1',
        inbound: { host: 'imap.example.com', port: 993, security: 'tls', username: 'zhangsan', box: 'INBOX' },
        outbound: { host: 'smtp.example.com', port: 587, security: 'starttls', username: 'zhangsan', from_address: 'zhangsan@example.com' },
    },
    {
        id: 'mb2',
        name: '个人QQ邮箱',
        address: 'lisi@qq.com',
        enabled: false,
        default: false,
        has_inbound: true,
        has_outbound: true,
        has_inbound_secret: false,
        has_outbound_secret: true,
        signature_id: '',
        inbound: { host: 'imap.qq.com', port: 993, security: 'tls', username: 'lisi', box: 'INBOX' },
        outbound: { host: 'smtp.qq.com', port: 465, security: 'tls', username: 'lisi', from_address: 'lisi@qq.com' },
    },
];

const signatures = emptyFixture ? [] : [
    { id: 's1', name: '工作签名', body: '—— 张三\n产品部 · Example 科技\n电话：010-8888-6666', default: true },
    { id: 's2', name: '个人签名', body: '祝好！\n张三', default: false },
];

const calls: unknown[][] = [];
const known: Record<string, (...args: unknown[]) => Promise<unknown>> = {
    MailboxAccountsList: () => Promise.resolve(JSON.parse(JSON.stringify(accounts))),
    MailboxSignaturesList: () => Promise.resolve(JSON.parse(JSON.stringify(signatures))),
    MailboxAccountSave: (payload: unknown) => { calls.push(['MailboxAccountSave', payload]); return Promise.resolve('mb1'); },
    MailboxSignatureSave: (payload: unknown) => { calls.push(['MailboxSignatureSave', payload]); return Promise.resolve('s1'); },
    // Mutating stubs so row actions actually change the sandbox fixture set.
    MailboxAccountDelete: (id: unknown) => {
        const i = accounts.findIndex(a => a.id === id);
        if (i >= 0) accounts.splice(i, 1);
        return Promise.resolve(undefined);
    },
    MailboxSignatureDelete: (id: unknown) => {
        const i = signatures.findIndex(s => s.id === id);
        if (i >= 0) signatures.splice(i, 1);
        return Promise.resolve(undefined);
    },
    MailboxAccountSetEnabled: (id: unknown, enabled: unknown) => {
        const a = accounts.find(x => x.id === id);
        if (a) a.enabled = Boolean(enabled);
        return Promise.resolve(undefined);
    },
    MailboxAccountSetDefault: (id: unknown) => {
        accounts.forEach(a => { a.default = a.id === id; });
        return Promise.resolve(undefined);
    },
    MailboxSignatureSetDefault: (id: unknown) => {
        signatures.forEach(s => { s.default = s.id === id; });
        return Promise.resolve(undefined);
    },
    // ~1s latency so the busy-disabled row state is observable in screenshots.
    MailboxAccountTest: (_id: unknown, side: unknown) => new Promise((resolve, reject) => {
        window.setTimeout(() => (side === 'inbound'
            ? resolve('收件连接正常')
            : reject(new Error('发件连接失败：无法连接 SMTP 服务器'))), 1000);
    }),
    // Draft probes resolve quickly so the footer chips are easy to trigger.
    MailboxAccountTestDraft: (spec: unknown, side: unknown) => new Promise((resolve, reject) => {
        window.setTimeout(() => (side === 'inbound'
            ? resolve('收件连接正常')
            : reject(new Error('发件连接失败：无法连接 SMTP 服务器'))), 350);
    }),
};

const appProxy = new Proxy({}, {
    get(_target, prop) {
        if (typeof prop !== 'string') return undefined;
        const knownCall = known[prop];
        if (knownCall) return knownCall;
        return (...args: unknown[]) => { calls.push([prop, ...args]); return Promise.resolve(undefined); };
    },
});

const bridgeWindow = window as unknown as { go?: unknown; runtime?: unknown };
bridgeWindow.go = { main: { App: appProxy } };
bridgeWindow.runtime = new Proxy({}, { get: () => () => undefined });
(window as unknown as { __mailCalls: unknown[][] }).__mailCalls = calls;

createRoot(document.getElementById('root')!).render(
    <DialogProvider>
        <div id="App" {...appAttrs}>
            <div className="main-content elegant-scrollbar app-main-content" data-nav-tab="settings">
                <div className="settings-shell settings-shell--padded settings-shell--solo">
                    <div className="settings-shell__body">
                        <EmailSettingsPanel lang="zh-Hans" />
                    </div>
                </div>
            </div>
        </div>
    </DialogProvider>,
);
