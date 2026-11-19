import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { useModalFocusTrap } from '../../hooks/useModalFocusTrap';
import { localizeText } from '../../i18n';
import { EventsOn } from '../../../wailsjs/runtime';
import { ActivateDesktopNode, ClearDesktopNodeResult, ConfigureDesktopNodeToken, DeployDesktopNode, ListDesktopNodes, RedeployDesktopNode, UpgradeDesktopNode, UseCloudDesktop } from '../../../wailsjs/go/main/App';

export type DesktopNodeEntry = {
    id: string;
    name: string;
    mode: string;
    host?: string;
    port?: number;
    user?: string;
    status: string;
    message?: string;
    node_id?: string;
    desktopd_url?: string;
    desktopd_port?: number;
    desktopd_addr?: string;
    with_srv?: boolean;
    srv_token_set?: boolean;
    last_kind?: string;
    last_result?: string;
    last_at?: string;
    registered?: boolean;
    online?: boolean;
    active?: boolean;
    memory?: string;
    cpus?: string;
};

type DesktopNodesView = {
    enabled?: boolean;
    hub_error?: string;
    nodes?: DesktopNodeEntry[];
};

function words(lang: string) {
    const L = (en: string, zh: string, hant?: string) => localizeText(lang, en, zh, hant);
    return {
        title: L('Intranet desktops', '内网桌面', '內網桌面'),
        hint: L(
            'Each row is a desktopd this app configured. Install and update keep running after this window closes. A finished run stays here until you clear it. Update recompiles the latest code; the access token and Hub link stay.',
            '每一行是已配置的 desktopd。安装和更新在关掉这个窗口后继续，完成情况会留在这里，可以清除。更新会用最新代码重新编译并部署，访问令牌和 Hub 连接保持不变。',
            '每一行是已設定的 desktopd。安裝和更新在關掉這個視窗後繼續，完成情況會留在這裡，可以清除。更新會用最新程式碼重新編譯並部署，存取權杖和 Hub 連線保持不變。',
        ),
        background: L('Running in the background. Closing this window does not stop it.', '已在后台进行，关掉窗口不会中断。', '已在背景進行，關掉視窗不會中斷。'),
        lastResult: L('Last result', '上次结果', '上次結果'),
        clear: L('Clear', '清除', '清除'),
        installWhat: L('What to install', '安装内容', '安裝內容'),
        empty: L('No intranet desktop yet.', '还没有内网桌面。', '還沒有內網桌面。'),
        update: L('Update', '更新', '更新'),
        updating: L('Updating…', '更新中…', '更新中…'),
        password: L('SSH password', 'SSH 密码', 'SSH 密碼'),
        start: L('Start update', '开始更新', '開始更新'),
        cancel: L('Cancel', '取消', '取消'),
        close: L('Close', '关闭', '關閉'),
        noSSH: L('No SSH login saved, so this desktop cannot be updated from here.', '没有保存 SSH 登录信息，无法从这里更新。', '沒有儲存 SSH 登入資訊，無法從這裡更新。'),
        online: L('Online', '在线', '線上'),
        offline: L('Offline', '离线', '離線'),
        active: L('In use', '使用中', '使用中'),
        useThis: L('Use this computer', '使用这台', '使用這台'),
        useCloud: L('Use the cloud computer', '改用云端电脑', '改用雲端電腦'),
        add: L('Deploy another', '部署一台', '部署一台'),
        host: L('IP', 'IP', 'IP'),
        port: L('Port', '端口', '連接埠'),
        portNote: L(
            'Open each row\'s desktopd port (default 18081) on the intranet and this app reaches that desktop directly instead of relaying through Hub. An install bound to 127.0.0.1 must first change DESKTOPD_ADDR to :18081 and restart. Do not expose this port to the internet.',
            '在内网放行每行的 desktopd 端口（默认 18081）后，本机可以直连那台桌面，不经 Hub 中转。绑在 127.0.0.1 的旧安装要先把 DESKTOPD_ADDR 改为 :18081 再重启。不要把这个端口映射到公网。',
            '在內網放行每行的 desktopd 連接埠（預設 18081）後，本機可以直連那台桌面，不經 Hub 中轉。綁在 127.0.0.1 的舊安裝要先把 DESKTOPD_ADDR 改為 :18081 再重啟。不要把這個連接埠映射到公網。',
        ),
        portLoopback: L(
            'localhost only — direct connection needs a LAN bind',
            '只监听本机——要直连需改为内网绑定',
            '只監聽本機——要直連需改為內網綁定',
        ),
        user: L('User', '用户名', '使用者'),
        deploy: L('Deploy', '部署', '部署'),
        deploying: L('Deploying…', '部署中…', '部署中…'),
        redeploy: L('Deploy again', '重新部署', '重新部署'),
        desktopOnly: L('Desktop only', '只安装桌面', '只安裝桌面'),
        withSrv: L('MaClawSrv and desktop', '同时安装 MaClawSrv 和桌面', '同時安裝 MaClawSrv 和桌面'),
        tokenHint: L(
            'The admin secret and Hub token are written from this page. They are not shown.',
            '管理密钥和 Hub 令牌在这个页面写入，不会显示出来。',
            '管理金鑰和 Hub 權杖在這個頁面寫入，不會顯示出來。',
        ),
        tokenReady: L('Token configured', '令牌已配置', '權杖已設定'),
        tokenMissing: L('Token not configured', '令牌未配置', '權杖未設定'),
        configure: L('Configure token', '配置令牌', '設定權杖'),
        loading: L('Loading…', '加载中…', '載入中…'),
        status: {
            upgrading: L('Updating', '更新中', '更新中'),
            deploying: L('Deploying', '部署中', '部署中'),
            registering: L('Registering', '注册中', '註冊中'),
            ready: L('Ready', '可用', '可用'),
            failed: L('Failed', '失败', '失敗'),
            imported: L('Imported', '已导入', '已匯入'),
        } as Record<string, string>,
    };
}

function targetOf(node: DesktopNodeEntry): string {
    if (node.mode === 'ssh' && node.host) {
        const port = node.port && node.port !== 22 ? `:${node.port}` : '';
        return `${node.user || ''}@${node.host}${port}`.replace(/^@/, '');
    }
    return node.desktopd_url || node.node_id || '';
}

function busy(node: DesktopNodeEntry): boolean {
    return node.status === 'upgrading' || node.status === 'deploying' || node.status === 'registering';
}

function desktopSizeLabel(memory: string | undefined, cpus: string | undefined): string {
    const mem = (memory || '').trim().toUpperCase();
    const cpu = (cpus || '').trim();
    if (mem && cpu) return `${mem} · ${cpu} CPU`;
    return mem || (cpu ? `${cpu} CPU` : '');
}

function installed(node: DesktopNodeEntry): boolean {
    return !!(node.node_id || node.registered || node.status === 'ready' || node.status === 'imported');
}

// desktopdPortOf is the port desktopd listens on for this row. The record
// carries it after a deploy; a direct-mode URL carries it in the host part;
// anything else falls back to the 18081 default.
function desktopdPortOf(node: DesktopNodeEntry): number {
    if (node.desktopd_port && node.desktopd_port > 0) return node.desktopd_port;
    const raw = (node.desktopd_url || '').trim();
    if (raw) {
        try {
            const parsed = new URL(raw.includes('://') ? raw : `http://${raw}`);
            const port = Number(parsed.port);
            if (port > 0) return port;
        } catch { /* fall back to the default */ }
    }
    // A stored bind like ":18082" or "10.0.0.5:18082" also carries the port.
    const bind = /:(\d+)$/.exec((node.desktopd_addr || '').trim());
    if (bind) {
        const port = Number(bind[1]);
        if (port > 0) return port;
    }
    return 18081;
}

// desktopdBindHost is the address part of the DESKTOPD_ADDR bind. Empty
// means all interfaces; loopback means nothing off the machine can dial it.
function desktopdBindHost(node: DesktopNodeEntry): string {
    const raw = (node.desktopd_addr || '').trim();
    if (raw.startsWith('[')) {
        const end = raw.indexOf(']');
        return end > 0 ? raw.slice(1, end) : raw;
    }
    // A bare IPv6 literal holds several colons and no port to strip.
    return raw.indexOf(':') === raw.lastIndexOf(':') ? raw.replace(/:\d+$/, '') : raw;
}

function desktopdLoopback(node: DesktopNodeEntry): boolean {
    // ::ffff:127.x is the IPv4-mapped spelling; 0:0:0:0:0:0:0:1 is ::1 spelled
    // out — both read as loopback the same way Go's net.IP.IsLoopback does.
    const host = desktopdBindHost(node).toLowerCase().replace(/^::ffff:/, '');
    return host === 'localhost' || host === '::1' || host === '0:0:0:0:0:0:0:1' || host.startsWith('127.');
}

// desktopdBindLabel renders the bind like it is configured: ":18081" for
// every interface, "127.0.0.1:18081" for loopback-only.
function desktopdBindLabel(node: DesktopNodeEntry): string {
    const port = desktopdPortOf(node);
    const host = desktopdBindHost(node);
    if (host === '') return `desktopd :${port}`;
    return `desktopd ${host.includes(':') ? `[${host}]` : host}:${port}`;
}

/** Backend stores LastAt as RFC3339 UTC; render it as a short local "YYYY-MM-DD HH:mm". */
function whenOf(value: string | undefined): string {
    if (!value) return '';
    const at = new Date(value);
    if (Number.isNaN(at.getTime())) return '';
    const pad = (n: number) => String(n).padStart(2, '0');
    return `${at.getFullYear()}-${pad(at.getMonth() + 1)}-${pad(at.getDate())} ${pad(at.getHours())}:${pad(at.getMinutes())}`;
}

type Props = {
    heading: string;
    tabs?: ReactNode;
    lang: string;
    onClose: () => void;
};

/** Desktop tab: configured intranet desktopds, with an update that redeploys the latest code. */
export function BotDesktopNodesDialog({ heading, tabs, lang, onClose }: Props) {
    const w = words(lang);
    const titleId = useId();
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const [nodes, setNodes] = useState<DesktopNodeEntry[] | null>(null);
    const [hubError, setHubError] = useState('');
    const [notice, setNotice] = useState('');
    const [askID, setAskID] = useState('');
    const [password, setPassword] = useState('');
    const [workingID, setWorkingID] = useState('');
    const [deployOpen, setDeployOpen] = useState(false);
    const [host, setHost] = useState('');
    const [port, setPort] = useState('22');
    const [user, setUser] = useState('');
    const [deployPassword, setDeployPassword] = useState('');
    const [withSrv, setWithSrv] = useState(false);
    const [rowWithSrv, setRowWithSrv] = useState(false);
    const [askKind, setAskKind] = useState<'update' | 'token' | 'deploy'>('update');
    const working = workingID !== '';
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);
    const { onKeyDown } = useModalFocusTrap(dialogRef, { onEscape: onClose });

    const applyView = useCallback((view: DesktopNodesView | null) => {
        setNodes(view?.nodes ?? []);
        setHubError(view?.hub_error || '');
    }, []);

    const reload = useCallback(async () => {
        applyView(await ListDesktopNodes() as DesktopNodesView);
    }, [applyView]);

    useEffect(() => {
        let live = true;
        ListDesktopNodes().then((view: DesktopNodesView) => {
            if (live) applyView(view);
        }).catch((error: unknown) => {
            if (live) setNotice(error instanceof Error ? error.message : String(error));
        });
        const off = EventsOn('desktop-nodes-changed', () => {
            if (live) void reload().catch(() => {});
        });
        return () => { live = false; off(); };
    }, [applyView, reload]);

    const startToken = async (id: string) => {
        if (working) return;
        setWorkingID(id);
        setNotice('');
        try {
            await ConfigureDesktopNodeToken(id, password);
            setPassword('');
            setAskID('');
            await reload();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : String(error));
        } finally {
            setWorkingID('');
        }
    };

    const startRedeploy = async (id: string) => {
        if (working) return;
        setWorkingID(id);
        setNotice('');
        try {
            await RedeployDesktopNode(id, password, rowWithSrv);
            setPassword('');
            setAskID('');
            await reload();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : String(error));
        } finally {
            setWorkingID('');
        }
    };

    const useNode = async (id: string) => {
        if (working) return;
        setWorkingID(id);
        setNotice('');
        try {
            await ActivateDesktopNode(id);
            await reload();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : String(error));
        } finally {
            setWorkingID('');
        }
    };

    const useCloud = async () => {
        if (working) return;
        setWorkingID('cloud');
        setNotice('');
        try {
            await UseCloudDesktop();
            await reload();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : String(error));
        } finally {
            setWorkingID('');
        }
    };

    const clearResult = async (id: string) => {
        setNotice('');
        try {
            await ClearDesktopNodeResult(id);
            await reload();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : String(error));
        }
    };

    const startUpdate = async (id: string) => {
        if (working) return;
        setWorkingID(id);
        setNotice('');
        try {
            await UpgradeDesktopNode(id, password);
            setPassword('');
            setAskID('');
            await reload();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : String(error));
        } finally {
            setWorkingID('');
        }
    };

    const deploy = async () => {
        if (working) return;
        setWorkingID('deploy');
        setNotice('');
        try {
            await DeployDesktopNode({ host: host.trim(), port: Number(port) || 22, user: user.trim(), password: deployPassword, with_srv: withSrv });
            setDeployPassword('');
            setDeployOpen(false);
            await reload();
        } catch (error) {
            setNotice(error instanceof Error ? error.message : String(error));
        } finally {
            setWorkingID('');
        }
    };

    return (
        <div className="modal-backdrop custom-dialog-backdrop bot-llm-dialog-backdrop" {...backdropProps} onKeyDown={onKeyDown}>
            <div className="modal-content custom-dialog bot-llm-dialog bot-llm-dialog--nodes" ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} data-testid="bot-desktop-nodes-dialog" tabIndex={-1} {...dialogProps}>
                <div className="bot-llm-dialog__head">
                    <div className="modal-header custom-dialog__header">
                        <div className="custom-dialog__heading">
                            <h3 id={titleId} className="custom-dialog__title">{heading}</h3>
                        </div>
                        <button type="button" className="btn-close custom-dialog__close" aria-label={w.close} onClick={onClose}>
                            <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M3.2 3.2 10.8 10.8M10.8 3.2 3.2 10.8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" /></svg>
                        </button>
                    </div>
                    {tabs}
                </div>
                <div className="modal-body custom-dialog__body">
                    <h4 className="bot-llm-dialog__section">{w.title}</h4>
                    <p className="bot-llm-dialog__hint">{w.hint}</p>
                    <p className="bot-llm-dialog__hint">{w.portNote}</p>
                    {hubError ? <p className="bot-llm-dialog__notice" role="status" data-testid="bot-desktop-nodes-hub">{hubError}</p> : null}
                    {nodes === null ? <p className="bot-desktop-nodes__note">{w.loading}</p> : null}
                    {nodes && nodes.length === 0 ? <p className="bot-desktop-nodes__note" data-testid="bot-desktop-nodes-empty">{w.empty}</p> : null}
                    {nodes && nodes.some(busy) ? <p className="bot-desktop-nodes__bg" data-testid="bot-desktop-background">{w.background}</p> : null}
                    {nodes && nodes.length > 0 ? (
                        <ul className="bot-desktop-nodes" data-testid="bot-desktop-nodes">
                            {nodes.map(node => {
                                const canUpdate = node.mode === 'ssh' && !!node.host;
                                const rowBusy = busy(node) || working;
                                const fresh = !installed(node);
                                const report = (node.last_result || '').trim();
                                const target = targetOf(node);
                                const live = node.message && node.message !== report ? node.message : '';
                                const action = rowBusy
                                    ? (node.status === 'upgrading' ? w.updating : w.deploying)
                                    : (fresh ? (node.status === 'failed' ? w.redeploy : w.deploy) : w.update);
                                const statusLabel = node.status ? (w.status[node.status] || node.status) : '';
                                const finishedAt = whenOf(node.last_at);
                                const sizeLabel = desktopSizeLabel(node.memory, node.cpus);
                                const badgeCls = node.status === 'ready'
                                    ? 'bot-desktop-nodes__badge--ready'
                                    : node.status === 'failed'
                                        ? 'bot-desktop-nodes__badge--failed'
                                        : rowBusy
                                            ? 'bot-desktop-nodes__badge--busy'
                                            : 'bot-desktop-nodes__badge--idle';
                                return (
                                    <li
                                        key={node.id}
                                        className={`bot-desktop-nodes__row${rowBusy ? ' is-busy' : ''}${node.status === 'failed' ? ' is-failed' : ''}`}
                                        data-testid={`bot-desktop-node-${node.id}`}
                                    >
                                        <div className="bot-desktop-nodes__main">
                                            <div className="bot-desktop-nodes__head">
                                                <span className="bot-desktop-nodes__name">{node.name || target}</span>
                                                {/* 无名称时目标已充当名称，不再重复渲染一遍。 */}
                                                {target && node.name ? <span className="bot-desktop-nodes__target">{target}</span> : null}
                                                {node.mode !== 'direct' ? (
                                                    <span
                                                        className="bot-desktop-nodes__target"
                                                        data-testid={`bot-desktop-port-${node.id}`}
                                                        title={desktopdLoopback(node) ? w.portLoopback : undefined}
                                                    >
                                                        {desktopdBindLabel(node)}
                                                    </span>
                                                ) : null}
                                                {statusLabel ? <span className={`bot-desktop-nodes__badge ${badgeCls}`}>{statusLabel}</span> : null}
                                            </div>
                                            {node.registered || node.active || node.with_srv || sizeLabel ? (
                                                <div className="bot-desktop-nodes__tags">
                                                    {node.registered ? (
                                                        <span className={`bot-desktop-nodes__tag${node.online ? ' is-online' : ' is-offline'}`}>{node.online ? w.online : w.offline}</span>
                                                    ) : null}
                                                    {node.active ? <span className="bot-desktop-nodes__tag">{w.active}</span> : null}
                                                    {node.with_srv ? <span className="bot-desktop-nodes__tag">{node.srv_token_set ? w.tokenReady : w.tokenMissing}</span> : null}
                                                    {sizeLabel ? <span className="bot-desktop-nodes__tag" data-testid={`bot-desktop-size-${node.id}`}>{sizeLabel}</span> : null}
                                                </div>
                                            ) : null}
                                            {live ? <p className="bot-desktop-nodes__live">{live}</p> : null}
                                        </div>
                                        <div className="bot-desktop-nodes__actions">
                                            <button
                                                type="button"
                                                className="btn-secondary"
                                                disabled={!canUpdate || rowBusy}
                                                data-testid={`bot-desktop-update-${node.id}`}
                                                onClick={() => { setAskKind(fresh ? 'deploy' : 'update'); setRowWithSrv(!!node.with_srv); setAskID(node.id); setPassword(''); setNotice(''); }}
                                            >
                                                {action}
                                            </button>
                                            {installed(node) && !node.active ? (
                                                <button
                                                    type="button"
                                                    className="btn-secondary"
                                                    disabled={rowBusy}
                                                    data-testid={`bot-desktop-use-${node.id}`}
                                                    onClick={() => { void useNode(node.id); }}
                                                >
                                                    {w.useThis}
                                                </button>
                                            ) : null}
                                            {node.active ? (
                                                <button
                                                    type="button"
                                                    className="btn-secondary"
                                                    disabled={rowBusy}
                                                    data-testid={`bot-desktop-cloud-${node.id}`}
                                                    onClick={() => { void useCloud(); }}
                                                >
                                                    {w.useCloud}
                                                </button>
                                            ) : null}
                                            {node.with_srv ? (
                                                <button
                                                    type="button"
                                                    className="btn-secondary"
                                                    disabled={!canUpdate || rowBusy}
                                                    data-testid={`bot-desktop-token-${node.id}`}
                                                    onClick={() => { setAskKind('token'); setAskID(node.id); setPassword(''); setNotice(''); }}
                                                >
                                                    {w.configure}
                                                </button>
                                            ) : null}
                                        </div>
                                        {report ? (
                                            <div className="bot-desktop-nodes__report" data-testid={`bot-desktop-result-${node.id}`}>
                                                <div className="bot-desktop-nodes__report-head">
                                                    <span className="bot-desktop-nodes__report-label">
                                                        {w.lastResult}{node.last_kind === 'upgrade' ? ` · ${w.update}` : node.last_kind === 'deploy' ? ` · ${w.deploy}` : ''}
                                                        {finishedAt ? ` · ${finishedAt}` : ''}
                                                    </span>
                                                    <button type="button" className="bot-desktop-nodes__clear" data-testid={`bot-desktop-clear-${node.id}`} onClick={() => { void clearResult(node.id); }}>{w.clear}</button>
                                                </div>
                                                <pre className="bot-desktop-nodes__log">{report}</pre>
                                            </div>
                                        ) : null}
                                        {!canUpdate ? <p className="bot-desktop-nodes__warn">{w.noSSH}</p> : null}
                                        {canUpdate && askID === node.id ? (
                                            <form
                                                className="bot-desktop-nodes__confirm"
                                                onSubmit={event => {
                                                    event.preventDefault();
                                                    if (askKind === 'token') void startToken(node.id);
                                                    else if (askKind === 'deploy') void startRedeploy(node.id);
                                                    else void startUpdate(node.id);
                                                }}
                                            >
                                                <div className="bot-desktop-nodes__field">
                                                    <label className="bot-desktop-nodes__label" htmlFor={`bot-desktop-pw-${node.id}`}>{w.password}</label>
                                                    <input
                                                        type="password"
                                                        id={`bot-desktop-pw-${node.id}`}
                                                        className="form-input"
                                                        autoComplete="current-password"
                                                        aria-label={w.password}
                                                        value={password}
                                                        disabled={working}
                                                        data-testid={`bot-desktop-update-password-${node.id}`}
                                                        onChange={event => setPassword(event.target.value)}
                                                    />
                                                </div>
                                                {askKind === 'deploy' ? (
                                                    <fieldset className="bot-desktop-nodes__choice">
                                                        <legend>{w.installWhat}</legend>
                                                        <div className="bot-desktop-nodes__choice-row">
                                                            <label className={`bot-desktop-nodes__choice-item${!rowWithSrv ? ' is-selected' : ''}`}>
                                                                <input type="radio" name={`desktop-row-${node.id}`} checked={!rowWithSrv} disabled={working} onChange={() => setRowWithSrv(false)} />
                                                                <span>{w.desktopOnly}</span>
                                                            </label>
                                                            <label className={`bot-desktop-nodes__choice-item${rowWithSrv ? ' is-selected' : ''}`}>
                                                                <input type="radio" name={`desktop-row-${node.id}`} checked={rowWithSrv} disabled={working} data-testid={`bot-desktop-row-srv-${node.id}`} onChange={() => setRowWithSrv(true)} />
                                                                <span>{w.withSrv}</span>
                                                            </label>
                                                        </div>
                                                    </fieldset>
                                                ) : null}
                                                <div className="bot-desktop-nodes__confirm-actions">
                                                    <button type="submit" className="btn-primary" disabled={working || password === ''} data-testid={`bot-desktop-update-go-${node.id}`}>{askKind === 'token' ? w.configure : askKind === 'deploy' ? w.deploy : w.start}</button>
                                                    <button type="button" className="btn-secondary" disabled={working} onClick={() => { setAskID(''); setPassword(''); }}>{w.cancel}</button>
                                                </div>
                                            </form>
                                        ) : null}
                                    </li>
                                );
                            })}
                        </ul>
                    ) : null}
                    {notice ? <p className="bot-llm-dialog__notice" role="status" data-testid="bot-desktop-nodes-notice">{notice}</p> : null}
                    <button type="button" className="bot-desktop-nodes__addtoggle" data-testid="bot-desktop-deploy-toggle" aria-expanded={deployOpen} disabled={working} onClick={() => setDeployOpen(open => !open)}>
                        <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M7 2.8v8.4M2.8 7h8.4" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" /></svg>
                        {w.add}
                    </button>
                    {deployOpen ? (
                        <form className="bot-desktop-nodes__add" data-testid="bot-desktop-deploy" onSubmit={event => { event.preventDefault(); void deploy(); }}>
                            <div className="bot-desktop-nodes__addgrid">
                                <div className="bot-desktop-nodes__field">
                                    <label className="bot-desktop-nodes__label" htmlFor="bot-desktop-add-host">{w.host}</label>
                                    <input id="bot-desktop-add-host" className="form-input" placeholder="192.168.1.49" aria-label={w.host} value={host} disabled={working} data-testid="bot-desktop-deploy-host" onChange={event => setHost(event.target.value)} />
                                </div>
                                <div className="bot-desktop-nodes__field">
                                    <label className="bot-desktop-nodes__label" htmlFor="bot-desktop-add-port">{w.port}</label>
                                    <input id="bot-desktop-add-port" className="form-input" inputMode="numeric" aria-label={w.port} value={port} disabled={working} data-testid="bot-desktop-deploy-port" onChange={event => setPort(event.target.value)} />
                                </div>
                                <div className="bot-desktop-nodes__field">
                                    <label className="bot-desktop-nodes__label" htmlFor="bot-desktop-add-user">{w.user}</label>
                                    <input id="bot-desktop-add-user" className="form-input" placeholder="root" aria-label={w.user} value={user} disabled={working} data-testid="bot-desktop-deploy-user" onChange={event => setUser(event.target.value)} />
                                </div>
                                <div className="bot-desktop-nodes__field">
                                    <label className="bot-desktop-nodes__label" htmlFor="bot-desktop-add-password">{w.password}</label>
                                    <input id="bot-desktop-add-password" type="password" className="form-input" autoComplete="current-password" aria-label={w.password} value={deployPassword} disabled={working} data-testid="bot-desktop-deploy-password" onChange={event => setDeployPassword(event.target.value)} />
                                </div>
                            </div>
                            <fieldset className="bot-desktop-nodes__choice">
                                <legend>{w.installWhat}</legend>
                                <div className="bot-desktop-nodes__choice-row">
                                    <label className={`bot-desktop-nodes__choice-item${!withSrv ? ' is-selected' : ''}`}>
                                        <input type="radio" name="desktop-install" checked={!withSrv} disabled={working} data-testid="bot-desktop-install-desktop" onChange={() => setWithSrv(false)} />
                                        <span>{w.desktopOnly}</span>
                                    </label>
                                    <label className={`bot-desktop-nodes__choice-item${withSrv ? ' is-selected' : ''}`}>
                                        <input type="radio" name="desktop-install" checked={withSrv} disabled={working} data-testid="bot-desktop-install-srv" onChange={() => setWithSrv(true)} />
                                        <span>{w.withSrv}</span>
                                    </label>
                                </div>
                                {withSrv ? <p className="bot-desktop-nodes__choice-hint">{w.tokenHint}</p> : null}
                            </fieldset>
                            <div className="bot-desktop-nodes__confirm-actions">
                                <button type="submit" className="btn-primary" disabled={working || !host.trim() || !user.trim() || deployPassword === ''} data-testid="bot-desktop-deploy-go">{w.deploy}</button>
                            </div>
                        </form>
                    ) : null}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button type="button" className="btn-secondary" onClick={onClose}>{w.close}</button>
                </div>
            </div>
        </div>
    );
}
