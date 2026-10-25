// Dev-only preview harness for EmailSettingsPanel and MailboxManageDialog.
// Serve with the vite dev server and open /email-preview.html — the Wails
// bridge is faked here so the panel renders with sample mailboxes and
// signatures outside the desktop shell. ?manage=mb1 opens the mail management
// dialog directly with a sample folder/message set.
import { createRoot } from 'react-dom/client';
import './App.css';
import { DialogProvider } from './components/CustomDialog';
import { EmailSettingsPanel } from './components/settings/EmailSettingsPanel';
import { MailboxManageDialog } from './components/settings/MailboxManageDialog';

// ?theme=dark|light mirrors how the real shell puts data-ai-theme on #App; the
// dark token blocks key off that attribute and cascade down from it.
// ?scheme=onyx|graphite|classic|aurora picks a named dark scheme like the shell.
// ?fixture=empty renders the no-mailboxes / no-signatures empty states.
const params = new URLSearchParams(window.location.search);
const themeParam = params.get('theme') || 'light';
const schemeParam = params.get('scheme');
const emptyFixture = params.get('fixture') === 'empty';
const manageId = params.get('manage') || '';
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

// Sample mail used by the ?manage fixture. One HTML mail (with a stylesheet,
// a remote tracking picture and a table) and one markdown note cover the
// reading pane's two rendering paths.
const sampleFolders = [
    { name: 'INBOX', special: 'inbox' },
    { name: 'Sent Messages', special: 'sent' },
    { name: 'Drafts', special: 'drafts' },
    { name: 'Junk', special: 'junk' },
    { name: 'Deleted Messages', special: 'trash' },
    { name: 'Archive', special: 'archive' },
    { name: '项目通知', special: '' },
];
const sampleUnseen: Record<string, number> = { INBOX: 3, Junk: 1 };
const sampleInbox = [
    {
        uid: 101, from: '"产品团队" <product@example.com>', to: 'zhangsan@example.com',
        subject: '【发布公告】MaClaw 8.1 正式发布', date: new Date().toISOString(), seen: false, has_attachment: true,
    },
    {
        uid: 102, from: '"王莉" <wangli@example.com>', to: 'zhangsan@example.com',
        subject: '周报：本周进展与下周计划', date: new Date(Date.now() - 26 * 3600 * 1000).toISOString(), seen: false,
    },
    {
        uid: 103, from: '"李强" <liqiang@example.com>', to: 'zhangsan@example.com',
        subject: '合同评审意见（第 3 稿）', date: new Date(Date.now() - 3 * 24 * 3600 * 1000).toISOString(), seen: true,
    },
];
const sampleHtmlBody = `<!doctype html><html><head><style>
body { font-family: sans-serif; }
h1 { color: #0a66c2; }
.release-table td { padding: 6px 12px; border: 1px solid #ccc; }
</style></head><body>
<h1>MaClaw 8.1 正式发布</h1>
<p>亲爱的用户，</p>
<p>我们很高兴地宣布 <b>MaClaw 8.1</b> 正式发布。本次更新的重点：</p>
<ul>
  <li>任务看板支持<b>多机器人协作</b></li>
  <li>邮件管理界面全面升级</li>
  <li>新增 12 项技能包</li>
</ul>
<table class="release-table"><tr><th>模块</th><th>变更</th></tr>
<tr><td>桌面端</td><td>界面专业化改造</td></tr>
<tr><td>服务端</td><td>发布流水线加固</td></tr></table>
<p><img src="https://tracking.example.com/pixel.gif" alt="活动横幅" width="480">（上方为远程图片，默认拦截）</p>
<p>查看 <a href="https://example.com/release/8.1">完整发布说明</a>。</p>
<p>—— 产品团队</p>
</body></html>`;
const sampleMarkdownBody = `# 本周进展\n\n## 已完成\n\n- 登录流程重构，工单减少 **40%**\n- 邮件管理界面支持 HTML/Markdown 渲染\n- 支持删除与标记已读\n\n## 下周计划\n\n1. 打通附件预览\n2. 联调自动归档\n\n> 风险：归档协议在 QQ 邮箱上的兼容性待验证。\n\n相关链接：[设计文档](https://example.com/design)`;
const samplePlainBody = '李强 写道：\n\n第 3 稿的修改意见如下：\n\n1. 第 2.1 条的违约金比例建议改为 20%\n2. 附件清单需要补充知识产权条款\n\n请 today 内回复确认。';

const sampleReads: Record<string, unknown> = {
    '101': {
        uid: 101, from: '"产品团队" <product@example.com>', to: 'zhangsan@example.com',
        subject: '【发布公告】MaClaw 8.1 正式发布', date: new Date().toISOString(), seen: false,
        body: 'MaClaw 8.1 正式发布：任务看板多机器人协作、邮件管理界面升级、新增 12 项技能包。详见 https://example.com/release/8.1',
        html: sampleHtmlBody,
        attachments: ['release-notes.pdf', 'banner.png'],
    },
    '102': {
        uid: 102, from: '"王莉" <wangli@example.com>', to: 'zhangsan@example.com',
        subject: '周报：本周进展与下周计划', date: new Date(Date.now() - 26 * 3600 * 1000).toISOString(), seen: false,
        body: sampleMarkdownBody,
    },
    '103': {
        uid: 103, from: '"李强" <liqiang@example.com>', to: 'zhangsan@example.com',
        subject: '合同评审意见（第 3 稿）', date: new Date(Date.now() - 3 * 24 * 3600 * 1000).toISOString(), seen: true,
        body: samplePlainBody,
    },
};

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
    // Mail management dialog fixtures (?manage=mb1).
    MailboxBrowseStart: () => Promise.resolve({
        folders: JSON.parse(JSON.stringify(sampleFolders)),
        box: 'INBOX',
        messages: JSON.parse(JSON.stringify(sampleInbox)),
        unseen: { ...sampleUnseen },
    }),
    MailboxBrowseMessages: (_id: unknown, box: unknown) => Promise.resolve(
        box === 'INBOX' ? JSON.parse(JSON.stringify(sampleInbox)) : [],
    ),
    MailboxBrowseRead: (_id: unknown, _box: unknown, uid: unknown) => {
        const view = sampleReads[String(uid)];
        return view ? Promise.resolve(JSON.parse(JSON.stringify(view))) : Promise.reject(new Error('邮件不存在'));
    },
    MailboxBrowseMarkRead: (_id: unknown, _box: unknown, uids: unknown, read: unknown) => {
        calls.push(['MailboxBrowseMarkRead', uids, read]);
        for (const uid of (uids as number[]) || []) {
            const row = sampleInbox.find(m => m.uid === uid);
            if (row) row.seen = Boolean(read);
        }
        return Promise.resolve({ updated: ((uids as number[]) || []).length });
    },
    MailboxBrowseDelete: (_id: unknown, _box: unknown, uids: unknown) => {
        calls.push(['MailboxBrowseDelete', uids]);
        for (const uid of (uids as number[]) || []) {
            const i = sampleInbox.findIndex(m => m.uid === uid);
            if (i >= 0) sampleInbox.splice(i, 1);
        }
        return Promise.resolve({ moved: ((uids as number[]) || []).length });
    },
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

const manageAccount = accounts.find(a => a.id === manageId);

createRoot(document.getElementById('root')!).render(
    <DialogProvider>
        <div id="App" {...appAttrs}>
            {manageAccount ? (
                <MailboxManageDialog lang="zh-Hans" account={{ id: manageAccount.id, name: manageAccount.name, address: manageAccount.address }} onClose={() => { window.location.href = '/email-preview.html'; }} />
            ) : (
                <div className="main-content elegant-scrollbar app-main-content" data-nav-tab="settings">
                    <div className="settings-shell settings-shell--padded settings-shell--solo">
                        <div className="settings-shell__body">
                            <EmailSettingsPanel lang="zh-Hans" />
                        </div>
                    </div>
                </div>
            )}
        </div>
    </DialogProvider>,
);
