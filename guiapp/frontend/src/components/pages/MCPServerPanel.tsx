import { useCallback, useEffect, useState } from 'react';
import { ClipboardSetText, EventsOn } from '../../../wailsjs/runtime';
import { useDialog } from '../CustomDialog';
import { tailText } from '../../i18n/mnTail';

type MCPHostStatus = {
    enabled?: boolean;
    running?: boolean;
    url?: string;
    port?: number;
    apiKey?: string;
    command?: string;
    stdioConfig?: string;
    httpConfig?: string;
    lanEnabled?: boolean;
    lanURL?: string;
    lanHTTPConfig?: string;
    error?: string;
};

type MCPAccessEntry = {
    time?: string;
    ip?: string;
    apiKey?: string;
    action?: string;
    task?: string;
    projectPath?: string;
};

type MCPApp = {
    GetMCPHostStatus?: () => Promise<MCPHostStatus>;
    PatchConfigFields?: (patch: Record<string, unknown>) => Promise<unknown>;
    GenerateMCPHostAPIKey?: () => Promise<MCPHostStatus>;
    GetMCPAccessLog?: () => Promise<MCPAccessEntry[]>;
    ClearMCPAccessLog?: () => Promise<unknown>;
};

function mcpApp(): MCPApp | null {
    const app = (window as unknown as { go?: { main?: { App?: MCPApp } } }).go?.main?.App;
    return app ?? null;
}

async function copyText(text: string): Promise<boolean> {
    try {
        await ClipboardSetText(text);
        return true;
    } catch {
        try {
            await navigator.clipboard.writeText(text);
            return true;
        } catch {
            return false;
        }
    }
}

const MCP_TOOLS = [
    ['list_models', '列出可用服务商和模型，不返回密钥'],
    ['create_task', '创建会话、本机编程或远程编程任务，可指定服务商和模型'],
    ['get_task', '查看状态、模型、工作目录、轮次、结束原因和每步结果'],
    ['send_command', '向任务发送命令。决定要带上这次的确认编号，用过即失效'],
    ['stop_task', '停止正在运行的任务'],
    ['list_tasks', '列出这台电脑上的最近任务'],
    ['read_log', '按游标读取进展、工具调用和错误'],
    ['list_artifacts', '列出工作目录里的文件、大小和哈希'],
    ['read_artifact', '在工作目录内读取或下载文件'],
    ['get_request', '按请求编号查询创建或指令是否已受理'],
    ['get_capabilities', '查看接口版本、功能、模型配置和浏览器测试的具体方法'],
    ['retry_steps', '按步骤编号重试失败项，保留已完成结果'],
    ['browser_test', '打开、观察、点击、按键、截图并断言任务页面'],
    ['pack_artifacts', '打包选定的任务文件，返回清单和哈希'],
] as const;

export function MCPServerPanel({ lang, onBack }: { lang: string; onBack: () => void }) {
    const { showConfirm } = useDialog();
    const [status, setStatus] = useState<MCPHostStatus>({ enabled: false, running: false });
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const [copied, setCopied] = useState('');
    const [accessLog, setAccessLog] = useState<MCPAccessEntry[]>([]);

    const loadAccessLog = useCallback(async () => {
        const app = mcpApp();
        if (!app?.GetMCPAccessLog) return;
        try {
            const entries = await app.GetMCPAccessLog();
            setAccessLog(Array.isArray(entries) ? entries : []);
        } catch {
            // The access log is auxiliary: a failed refresh keeps showing the previous list.
        }
    }, []);

    const reload = useCallback(async () => {
        const app = mcpApp();
        if (!app?.GetMCPHostStatus) {
            setError(tailText(lang, 'MCP status is unavailable', '读不到 MCP 状态'));
            return;
        }
        const next = await app.GetMCPHostStatus();
        setStatus(next || { enabled: false, running: false });
        setError('');
    }, [lang]);

    useEffect(() => {
        let cancelled = false;
        void (async () => {
            try {
                const app = mcpApp();
                if (!app?.GetMCPHostStatus) return;
                const next = await app.GetMCPHostStatus();
                if (!cancelled) setStatus(next || { enabled: false, running: false });
            } catch (err) {
                if (!cancelled) setError(err instanceof Error ? err.message : String(err));
            }
        })();
        void loadAccessLog();
        // MCP mirrors every remote user turn as mcp-host-message: new access
        // entries coincide with it, so the open panel refreshes live.
        const offLogEvent = EventsOn('mcp-host-message', () => { void loadAccessLog(); });
        return () => {
            cancelled = true;
            offLogEvent?.();
        };
    }, [loadAccessLog]);

    const enabled = status.enabled === true;
    const running = enabled && status.running === true;
    const lanEnabled = status.lanEnabled === true;

    const patchMCPField = async (field: string, value: boolean, missing: string) => {
        const app = mcpApp();
        if (!app?.PatchConfigFields) {
            setError(missing);
            return;
        }
        setBusy(true);
        setError('');
        try {
            await app.PatchConfigFields({ [field]: value });
            await reload();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setBusy(false);
        }
    };

    const toggle = () => patchMCPField('mcp_host_enabled', !enabled, tailText(lang, 'Cannot change the switch yet', '现在还不能改这个开关'));

    const toggleLan = () => patchMCPField('mcp_host_lan_enabled', !lanEnabled, tailText(lang, 'Cannot change the switch yet', '现在还不能改这个开关'));

    const generateKey = async () => {
        const ok = await showConfirm(
            tailText(lang, 'The old key stops working immediately. Update every client that saved it.', '旧密钥会立刻失效，已经保存的客户端要改成新密钥。', '舊密鑰會立刻失效，已經保存的客戶端要改成新密鑰。'),
            tailText(lang, 'Generate a new API key?', '生成新的 API key？', '生成新的 API key？'),
            { confirmVariant: 'danger' },
        );
        if (!ok) return;
        const app = mcpApp();
        if (!app?.GenerateMCPHostAPIKey) {
            setError(tailText(lang, 'Cannot generate a key yet', '现在还不能生成密钥'));
            return;
        }
        setBusy(true);
        setError('');
        try {
            const next = await app.GenerateMCPHostAPIKey();
            if (next) setStatus(next);
            else await reload();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setBusy(false);
        }
    };

    const copy = async (id: string, text: string) => {
        if (!text) return;
        const ok = await copyText(text);
        if (ok) {
            setCopied(id);
            window.setTimeout(() => setCopied((current) => (current === id ? '' : current)), 1600);
        }
    };

    const clearAccessLog = async () => {
        const ok = await showConfirm(
            tailText(lang, 'Clear every recorded MCP access entry? This cannot be undone.', '确定清除全部 MCP 访问记录吗？清除后无法恢复。', '確定清除全部 MCP 存取記錄嗎？清除後無法恢復。'),
            tailText(lang, 'Clear access log', '清除访问日志', '清除存取日誌'),
            { confirmVariant: 'danger' },
        );
        if (!ok) return;
        const app = mcpApp();
        if (!app?.ClearMCPAccessLog) {
            setError(tailText(lang, 'Cannot clear the access log yet', '现在还不能清除访问日志'));
            return;
        }
        setBusy(true);
        setError('');
        try {
            await app.ClearMCPAccessLog();
            await loadAccessLog();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setBusy(false);
        }
    };

    return (
        <div className="utilities-page utilities-deploy" data-testid="mcp-server-panel" data-mode="tools">
            <div className="utilities-page__header utilities-deploy__header">
                <button type="button" className="utilities-deploy__back" onClick={onBack}>
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth={2} strokeLinecap="round" strokeLinejoin="round" aria-hidden="true"><path d="M14 6l-6 6 6 6" /></svg>
                    {tailText(lang, 'Back', '返回', '返回')}
                </button>
                <div className="utilities-deploy__hero">
                    <div className="utilities-deploy__hero-text">
                        <h2 className="utilities-page__title">{tailText(lang, 'MCP server', 'MCP服务器')}</h2>
                        <p className="utilities-page__subtitle">
                            {tailText(
                                lang,
                                'This computer is the session service. VS Code ACP, remote control, and MCP are different ways in, and they share create, stop, and result.',
                                '这台电脑是会话服务。VS Code 的 ACP、远控和 MCP 是不同的接入方式，创建、停止和查看结果走同一套能力。',
                            )}
                        </p>
                    </div>
                </div>
            </div>
            {error ? <p className="utilities-error" role="alert">{error}</p> : null}
            {status.error ? <p className="utilities-error" role="alert">{status.error}</p> : null}
            <section className="utilities-deploy-card">
                <div className="utilities-deploy-card__head">
                    <h3 className="utilities-deploy-card__title">{tailText(lang, 'Enable MCP server', '启用 MCP 服务器')}</h3>
                    <button
                        type="button"
                        role="switch"
                        className="utilities-btn"
                        aria-checked={enabled}
                        data-testid="mcp-server-enabled"
                        disabled={busy}
                        onClick={() => { void toggle(); }}
                    >
                        {enabled ? tailText(lang, 'On', '已开启') : tailText(lang, 'Off', '已关闭')}
                    </button>
                </div>
                <p className="utilities-page__subtitle">
                    {tailText(
                        lang,
                        'The server stays off until you turn it on. Connection details appear after it is listening.',
                        '默认关闭。打开并开始监听之后，才显示接入信息。',
                    )}
                </p>
                {enabled ? (
                    <>
                        <div className="utilities-deploy-card__head" data-testid="mcp-server-lan-row">
                            <h3 className="utilities-deploy-card__title">{tailText(lang, 'Allow LAN access', '允许局域网接入', '允許區域網路接入')}</h3>
                            <button
                                type="button"
                                role="switch"
                                className="utilities-btn"
                                aria-checked={lanEnabled}
                                data-testid="mcp-server-lan"
                                disabled={busy}
                                onClick={() => { void toggleLan(); }}
                            >
                                {lanEnabled ? tailText(lang, 'On', '已开启') : tailText(lang, 'Off', '已关闭')}
                            </button>
                        </div>
                        <p className="utilities-page__subtitle">
                            {tailText(
                                lang,
                                'LAN devices connect with the same API key. Windows Firewall may ask to allow access on the first connection.',
                                '局域网设备用同一个 API key 连接。首次连接时 Windows 防火墙可能提示放行。',
                                '區域網路裝置用同一個 API key 連線。首次連線時 Windows 防火牆可能提示放行。',
                            )}
                        </p>
                    </>
                ) : null}
                {running ? (
                    <>
                        <p className="utilities-status-chip" data-state="running">
                            <span className="utilities-status-chip__dot" aria-hidden />
                            {status.url}
                        </p>
                        <p className="utilities-page__subtitle">
                            {tailText(
                                lang,
                                'MCP has no top-level apiKey field. HTTP puts the key in Authorization: Bearer. The stdio config has no secret; the local proxy reads the key on this computer.',
                                'MCP 协议没有顶层 apiKey 字段。HTTP 接入把密钥放在 Authorization: Bearer。stdio 配置不含密钥，本机代理会自己读取。',
                            )}
                        </p>
                        <label className="utilities-deploy-field">
                            <span>API key</span>
                            <input className="utilities-input" data-testid="mcp-server-apikey" readOnly value={status.apiKey || ''} />
                        </label>
                        <div className="utilities-deploy-card__head">
                            <h3 className="utilities-deploy-card__title">stdio</h3>
                            <button type="button" className="utilities-btn utilities-btn--ghost" onClick={() => { void copy('stdio', status.stdioConfig || ''); }}>
                                {copied === 'stdio' ? tailText(lang, 'Copied', '已复制') : tailText(lang, 'Copy', '复制')}
                            </button>
                        </div>
                        <textarea className="utilities-input" data-testid="mcp-server-stdio" readOnly rows={5} value={status.stdioConfig || ''} />
                        <div className="utilities-deploy-card__head">
                            <h3 className="utilities-deploy-card__title">HTTP</h3>
                            <button type="button" className="utilities-btn utilities-btn--ghost" onClick={() => { void copy('http', status.httpConfig || ''); }}>
                                {copied === 'http' ? tailText(lang, 'Copied', '已复制') : tailText(lang, 'Copy', '复制')}
                            </button>
                        </div>
                        <textarea className="utilities-input" data-testid="mcp-server-http" readOnly rows={6} value={status.httpConfig || ''} />
                        {lanEnabled ? (
                            <div data-testid="mcp-server-lan-block">
                                <div className="utilities-deploy-card__head">
                                    <h3 className="utilities-deploy-card__title">{tailText(lang, 'HTTP (LAN)', 'HTTP（局域网）', 'HTTP（區域網路）')}</h3>
                                    {status.lanURL && status.lanHTTPConfig ? (
                                        <button type="button" className="utilities-btn utilities-btn--ghost" onClick={() => { void copy('http-lan', status.lanHTTPConfig || ''); }}>
                                            {copied === 'http-lan' ? tailText(lang, 'Copied', '已复制') : tailText(lang, 'Copy', '复制')}
                                        </button>
                                    ) : null}
                                </div>
                                {status.lanURL && status.lanHTTPConfig ? (
                                    <>
                                        <p className="utilities-status-chip" data-state="running">
                                            <span className="utilities-status-chip__dot" aria-hidden />
                                            {status.lanURL}
                                        </p>
                                        <textarea className="utilities-input" data-testid="mcp-server-http-lan" readOnly rows={6} value={status.lanHTTPConfig || ''} />
                                    </>
                                ) : (
                                    <p className="utilities-page__subtitle">
                                        {tailText(lang, 'No LAN address detected on this computer.', '没有在这台电脑上找到局域网地址。', '沒有在這台電腦上找到區域網路地址。')}
                                    </p>
                                )}
                            </div>
                        ) : null}
                        <button
                            type="button"
                            className="utilities-btn"
                            data-testid="mcp-server-generate-key"
                            disabled={busy}
                            onClick={() => { void generateKey(); }}
                        >
                            {tailText(lang, 'Generate API key', '生成 API key')}
                        </button>
                        <div data-testid="mcp-server-tools">
                            <h3 className="utilities-deploy-card__title">{tailText(lang, 'Tools', '工具')}</h3>
                            <ul>
                                {MCP_TOOLS.map(([name, desc]) => (
                                    <li key={name}><code>{name}</code> {desc}</li>
                                ))}
                            </ul>
                        </div>
                    </>
                ) : null}
            </section>
            <section className="utilities-deploy-card" data-testid="mcp-server-access-log">
                <div className="utilities-deploy-card__head">
                    <h3 className="utilities-deploy-card__title">{tailText(lang, 'Access log', '访问日志', '存取日誌')}</h3>
                    <button
                        type="button"
                        className="utilities-btn utilities-btn--quiet-danger"
                        data-testid="mcp-server-access-clear"
                        disabled={busy || accessLog.length === 0}
                        onClick={() => { void clearAccessLog(); }}
                    >
                        {tailText(lang, 'Clear log', '清除日志', '清除日誌')}
                    </button>
                </div>
                <p className="utilities-page__subtitle">
                    {tailText(
                        lang,
                        'Tasks started through MCP record the caller IP, the API key, and the task name.',
                        '通过 MCP 发起的任务会记录来源 IP、API key 和任务名。',
                        '透過 MCP 發起的任務會記錄來源 IP、API key 和任務名。',
                    )}
                </p>
                {accessLog.length === 0 ? (
                    <p className="utilities-page__subtitle" data-testid="mcp-server-access-empty">
                        {tailText(lang, 'No entries yet.', '暂无记录。', '暫無記錄。')}
                    </p>
                ) : (
                    <ul className="utilities-access-log">
                        {accessLog.map((entry, index) => (
                            <li key={index} className="utilities-access-log__row" data-testid="mcp-server-access-row">
                                <p className="utilities-access-log__meta">
                                    <span>{entry.time}</span>
                                    <span>{entry.ip}</span>
                                    <span>{entry.apiKey}</span>
                                    <code className="utilities-access-log__action">{entry.action}</code>
                                </p>
                                <p className="utilities-access-log__task">
                                    {entry.task}
                                    {entry.projectPath ? <span className="utilities-access-log__path">{entry.projectPath}</span> : null}
                                </p>
                            </li>
                        ))}
                    </ul>
                )}
            </section>
        </div>
    );
}
