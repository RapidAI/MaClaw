import { useCallback, useEffect, useState } from 'react';
import { ClipboardSetText } from '../../../wailsjs/runtime';
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
    error?: string;
};

type MCPApp = {
    GetMCPHostStatus?: () => Promise<MCPHostStatus>;
    PatchConfigFields?: (patch: Record<string, unknown>) => Promise<unknown>;
    GenerateMCPHostAPIKey?: () => Promise<MCPHostStatus>;
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
    ['create_task', '创建会话或编程任务，可指定服务商和模型'],
    ['get_task', '查看状态和结果'],
    ['stop_task', '停止正在运行的任务'],
    ['list_tasks', '列出这台电脑上的最近任务'],
] as const;

export function MCPServerPanel({ lang, onBack }: { lang: string; onBack: () => void }) {
    const { showConfirm } = useDialog();
    const [status, setStatus] = useState<MCPHostStatus>({ enabled: false, running: false });
    const [busy, setBusy] = useState(false);
    const [error, setError] = useState('');
    const [copied, setCopied] = useState('');

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
        return () => {
            cancelled = true;
        };
    }, []);

    const enabled = status.enabled === true;
    const running = enabled && status.running === true;

    const toggle = async () => {
        const app = mcpApp();
        if (!app?.PatchConfigFields) {
            setError(tailText(lang, 'Cannot change the switch yet', '现在还不能改这个开关'));
            return;
        }
        setBusy(true);
        setError('');
        try {
            await app.PatchConfigFields({ mcp_host_enabled: !enabled });
            await reload();
        } catch (err) {
            setError(err instanceof Error ? err.message : String(err));
        } finally {
            setBusy(false);
        }
    };

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
        </div>
    );
}
