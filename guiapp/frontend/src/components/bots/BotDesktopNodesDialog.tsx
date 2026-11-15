import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from 'react';
import { useSafeBackdropDismiss } from '../../hooks/useSafeBackdropDismiss';
import { useModalFocusTrap } from '../../hooks/useModalFocusTrap';
import { localizeText } from '../../i18n';
import { EventsOn } from '../../../wailsjs/runtime';
import { DeployDesktopNode, ListDesktopNodes, UpgradeDesktopNode } from '../../../wailsjs/go/main/App';

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
    registered?: boolean;
    online?: boolean;
    active?: boolean;
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
            'Each row is a desktopd this app configured. Update sends the assistant to that machine to compile and deploy the latest code. The access token and Hub link stay.',
            '每一行是已配置的 desktopd。更新会派助手到那台机器上，用最新代码重新编译并部署。访问令牌和 Hub 连接保持不变。',
            '每一行是已設定的 desktopd。更新會派助手到那台機器上，用最新程式碼重新編譯並部署。存取權杖和 Hub 連線保持不變。',
        ),
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
        add: L('Deploy another', '部署一台', '部署一台'),
        host: L('IP', 'IP', 'IP'),
        port: L('Port', '端口', '連接埠'),
        user: L('User', '用户名', '使用者'),
        deploy: L('Deploy', '部署', '部署'),
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
    const working = workingID !== '';
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose, { enabled: !working });
    const { onKeyDown } = useModalFocusTrap(dialogRef, { onEscape: () => { if (!working) onClose(); } });

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
            await DeployDesktopNode({ host: host.trim(), port: Number(port) || 22, user: user.trim(), password: deployPassword });
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
            <div className="modal-content custom-dialog bot-llm-dialog" ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} data-testid="bot-desktop-nodes-dialog" tabIndex={-1} {...dialogProps}>
                <div className="bot-llm-dialog__head">
                    <div className="modal-header custom-dialog__header">
                        <div className="custom-dialog__heading">
                            <h3 id={titleId} className="custom-dialog__title">{heading}</h3>
                        </div>
                        <button type="button" className="btn-close custom-dialog__close" aria-label={w.close} disabled={working} onClick={onClose}>
                            <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true"><path d="M3.2 3.2 10.8 10.8M10.8 3.2 3.2 10.8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" /></svg>
                        </button>
                    </div>
                    {tabs}
                </div>
                <div className="modal-body custom-dialog__body">
                    <h4 className="bot-llm-dialog__section">{w.title}</h4>
                    <p className="bot-llm-dialog__hint">{w.hint}</p>
                    {hubError ? <p className="bot-llm-dialog__notice" role="status" data-testid="bot-desktop-nodes-hub">{hubError}</p> : null}
                    {nodes === null ? <p className="bot-llm-dialog__subhint">{w.loading}</p> : null}
                    {nodes && nodes.length === 0 ? <p className="bot-llm-dialog__subhint" data-testid="bot-desktop-nodes-empty">{w.empty}</p> : null}
                    {nodes && nodes.length > 0 ? (
                        <ul className="bot-desktop-nodes" data-testid="bot-desktop-nodes">
                            {nodes.map(node => {
                                const canUpdate = node.mode === 'ssh' && !!node.host;
                                const rowBusy = busy(node) || workingID === node.id;
                                return (
                                    <li key={node.id} className="bot-desktop-nodes__row" data-testid={`bot-desktop-node-${node.id}`}>
                                        <div className="bot-desktop-nodes__main">
                                            <span className="bot-desktop-nodes__name">{node.name || targetOf(node)}</span>
                                            <span className="bot-cap__meta">
                                                {targetOf(node)}
                                                {node.status ? ` · ${w.status[node.status] || node.status}` : ''}
                                                {node.registered ? ` · ${node.online ? w.online : w.offline}` : ''}
                                                {node.active ? ` · ${w.active}` : ''}
                                            </span>
                                            {node.message ? <span className="bot-cap__meta">{node.message}</span> : null}
                                            {!canUpdate ? <span className="bot-cap__meta">{w.noSSH}</span> : null}
                                        </div>
                                        <div className="bot-desktop-nodes__actions">
                                            {canUpdate && askID === node.id ? (
                                                <form
                                                    className="bot-desktop-nodes__confirm"
                                                    onSubmit={event => { event.preventDefault(); void startUpdate(node.id); }}
                                                >
                                                    <input
                                                        type="password"
                                                        className="form-input"
                                                        autoComplete="current-password"
                                                        placeholder={w.password}
                                                        aria-label={w.password}
                                                        value={password}
                                                        disabled={working}
                                                        data-testid={`bot-desktop-update-password-${node.id}`}
                                                        onChange={event => setPassword(event.target.value)}
                                                    />
                                                    <button type="submit" className="btn-primary" disabled={working || password === ''} data-testid={`bot-desktop-update-go-${node.id}`}>{w.start}</button>
                                                    <button type="button" className="btn-secondary" disabled={working} onClick={() => { setAskID(''); setPassword(''); }}>{w.cancel}</button>
                                                </form>
                                            ) : (
                                                <button
                                                    type="button"
                                                    className="btn-secondary"
                                                    disabled={!canUpdate || rowBusy}
                                                    data-testid={`bot-desktop-update-${node.id}`}
                                                    onClick={() => { setAskID(node.id); setPassword(''); setNotice(''); }}
                                                >
                                                    {rowBusy ? w.updating : w.update}
                                                </button>
                                            )}
                                        </div>
                                    </li>
                                );
                            })}
                        </ul>
                    ) : null}
                    {notice ? <p className="bot-llm-dialog__notice" role="status" data-testid="bot-desktop-nodes-notice">{notice}</p> : null}
                    <div className="bot-cap__actions">
                        <button type="button" className="btn-secondary" data-testid="bot-desktop-deploy-toggle" disabled={working} onClick={() => setDeployOpen(open => !open)}>{w.add}</button>
                    </div>
                    {deployOpen ? (
                        <form className="bot-cap__form" data-testid="bot-desktop-deploy" onSubmit={event => { event.preventDefault(); void deploy(); }}>
                            <input className="form-input" placeholder={w.host} aria-label={w.host} value={host} disabled={working} data-testid="bot-desktop-deploy-host" onChange={event => setHost(event.target.value)} />
                            <input className="form-input" placeholder={w.port} aria-label={w.port} value={port} disabled={working} data-testid="bot-desktop-deploy-port" onChange={event => setPort(event.target.value)} />
                            <input className="form-input" placeholder={w.user} aria-label={w.user} value={user} disabled={working} data-testid="bot-desktop-deploy-user" onChange={event => setUser(event.target.value)} />
                            <input type="password" className="form-input" placeholder={w.password} aria-label={w.password} autoComplete="current-password" value={deployPassword} disabled={working} data-testid="bot-desktop-deploy-password" onChange={event => setDeployPassword(event.target.value)} />
                            <button type="submit" className="btn-primary" disabled={working || !host.trim() || !user.trim() || deployPassword === ''} data-testid="bot-desktop-deploy-go">{w.deploy}</button>
                        </form>
                    ) : null}
                </div>
                <div className="modal-footer custom-dialog__footer">
                    <button type="button" className="btn-secondary" onClick={onClose} disabled={working}>{w.close}</button>
                </div>
            </div>
        </div>
    );
}
