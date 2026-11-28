import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
    DeleteCloudSrvNode,
    DeployDesktopNode,
    GetCloudSrvChoice,
    GetMaclawLLMProfilePanelState,
    ListCloudSrvNodes,
    ProbeCloudSrvSSH,
    SetCloudSrvChoice,
    UpgradeCloudSrvNode,
} from '../../../wailsjs/go/main/App';
import { localizeText } from '../../i18n';
import { openSettingsTab } from '../../utils/settingsNavigation';
import { useDialog } from '../CustomDialog';

type CloudSrvNode = {
    node_id: string;
    name: string;
    address: string;
    online: boolean;
};

type CloudSrvState = {
    node_id: string;
    nodes: CloudSrvNode[];
};

// The 云端 Agent default-model line of the LLM assignment panel state. The
// card is read-only: adjustments live in 大模型配置 → 云端 Agent.
type CloudLLMSummary = {
    provider_id?: string;
    provider_name?: string;
    model?: string;
    inherit_assistant?: boolean;
    configured?: boolean;
};

type CloudLLMPanelState = {
    cloud?: CloudLLMSummary;
};

type CloudSrvProbe = {
    ok: boolean;
    method?: string;
    message: string;
};

type CloudSrvNodeView = {
    node_id?: string;
    name: string;
    host: string;
    port?: number;
    user: string;
    registered?: boolean;
    online?: boolean;
    busy?: boolean;
};

type CloudAgentSettingsPanelProps = {
    lang: string;
    showToastMessage: (message: string) => void;
};

const textForLang = localizeText;

const errorText = (err: unknown) => {
    if (err && typeof err === 'object' && 'message' in err) return String((err as { message?: unknown }).message || '');
    return String(err || '');
};

const CloudIcon = () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M17.5 19a4.5 4.5 0 0 0 .42-8.98 6.5 6.5 0 0 0-12.7 1.61A4 4 0 0 0 6 19h11.5Z" />
    </svg>
);

const CpuIcon = () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <rect x="5" y="5" width="14" height="14" rx="2" />
        <rect x="10" y="10" width="4" height="4" />
        <path d="M9 2v3M15 2v3M9 19v3M15 19v3M2 9h3M2 15h3M19 9h3M19 15h3" />
    </svg>
);

const ServerIcon = () => (
    <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <rect x="3" y="4" width="18" height="7" rx="2" />
        <rect x="3" y="13" width="18" height="7" rx="2" />
        <path d="M7 7.5h.01M7 16.5h.01" />
    </svg>
);

export const CloudAgentSettingsPanel = ({ lang, showToastMessage }: CloudAgentSettingsPanelProps) => {
    const t = useCallback((en: string, zhHans: string, zhHant?: string) => textForLang(lang, en, zhHans, zhHant || zhHans), [lang]);
    const { showConfirm } = useDialog();

    const [state, setState] = useState<CloudSrvState>({ node_id: '', nodes: [] });
    const [choice, setChoice] = useState<'public' | 'private'>('public');
    const [nodeID, setNodeID] = useState('');
    const [loading, setLoading] = useState(true);
    const [saving, setSaving] = useState(false);
    const [loadError, setLoadError] = useState('');

    const [host, setHost] = useState('');
    const [port, setPort] = useState('22');
    const [user, setUser] = useState('');
    const [password, setPassword] = useState('');
    const [probing, setProbing] = useState(false);
    const [probe, setProbe] = useState<CloudSrvProbe | null>(null);
    const [installing, setInstalling] = useState(false);
    // deploying = the install agent is running on the server. It ends when a
    // NEW node registers on the Hub (refresh detects it) or after a timeout —
    // until then the install button must not allow a duplicate install.
    const [deploying, setDeploying] = useState(false);
    const [installNote, setInstallNote] = useState('');
    const [installOpen, setInstallOpen] = useState(false);

    // Resolved 云端 Agent default-model line from the LLM assignment read
    // model; the card is informational and jumps to 大模型配置 for edits.
    const [llmState, setLlmState] = useState<CloudLLMPanelState | null>(null);

    // Installed srv-only nodes (私有云端服务) with update/delete actions.
    const [srvNodes, setSrvNodes] = useState<CloudSrvNodeView[]>([]);
    const [upgradeTarget, setUpgradeTarget] = useState('');
    const [upgradePassword, setUpgradePassword] = useState('');
    const [upgrading, setUpgrading] = useState(false);

    const choiceInitialized = useRef(false);
    // Deploy watch: the node ids that existed BEFORE the install started. A
    // refresh that surfaces an id outside this set means the new node has
    // registered and the install is done. Null = not watching.
    const deployWatchRef = useRef<Set<string> | null>(null);
    // Install-start schedules delayed node-list refreshes; clear them on
    // unmount so a closed settings panel never sets state afterwards.
    const refreshTimersRef = useRef<number[]>([]);
    useEffect(() => () => {
        for (const id of refreshTimersRef.current) window.clearTimeout(id);
        refreshTimersRef.current = [];
    }, []);
    const refresh = useCallback(async () => {
        // All three reads in flight at once — a refresh is issued on every
        // mount, save, and install watch tick, so serial roundtrips add up.
        const [choiceRes, llmRes, srvRes] = await Promise.allSettled([
            GetCloudSrvChoice(),
            GetMaclawLLMProfilePanelState(),
            ListCloudSrvNodes(),
        ]);
        if (choiceRes.status === 'fulfilled') {
            const next = choiceRes.value as CloudSrvState;
            const st: CloudSrvState = { node_id: String(next?.node_id || ''), nodes: Array.isArray(next?.nodes) ? next.nodes : [] };
            setState(st);
            // A deploy watch that finds a fresh node id: the install agent
            // finished registering. Release the install button and preselect
            // the new node (the user still confirms with 保存 to switch).
            const watch = deployWatchRef.current;
            if (watch) {
                const fresh = st.nodes.find((node) => !watch.has(node.node_id));
                if (fresh) {
                    deployWatchRef.current = null;
                    setDeploying(false);
                    setInstallNote(t(
                        'Install finished: ' + (fresh.name || fresh.node_id) + ' has registered on the Hub.',
                        '安装完成：' + (fresh.name || fresh.node_id) + ' 已注册到 Hub。',
                    ));
                    choiceInitialized.current = true;
                    setChoice('private');
                    setNodeID(fresh.node_id);
                }
            }
            // Seed the form once from the saved choice; later refreshes must
            // not clobber what the user is currently picking.
            if (!choiceInitialized.current) {
                choiceInitialized.current = true;
                setChoice(st.node_id ? 'private' : 'public');
                setNodeID(st.node_id);
            }
            setLoadError('');
        } else {
            setLoadError(errorText(choiceRes.reason));
        }
        setLoading(false);
        if (llmRes.status === 'fulfilled') {
            const panel = llmRes.value as CloudLLMPanelState;
            setLlmState({ cloud: panel?.cloud || undefined });
        } else {
            setLlmState(null);
        }
        if (srvRes.status === 'fulfilled') {
            const nodes = srvRes.value as CloudSrvNodeView[];
            setSrvNodes(Array.isArray(nodes) ? nodes : []);
        }
    }, [t]);

    useEffect(() => { void refresh(); }, [refresh]);

    // Install dialog: Esc closes, like the other settings dialogs. Opening
    // moves focus into the first field; closing restores it to the control
    // that opened the dialog so keyboard users are not dropped on <body>.
    const installDialogRef = useRef<HTMLDivElement | null>(null);
    useEffect(() => {
        if (!installOpen) return;
        const invoker = document.activeElement as HTMLElement | null;
        const onKey = (event: KeyboardEvent) => {
            if (event.key === 'Escape') setInstallOpen(false);
        };
        window.addEventListener('keydown', onKey);
        installDialogRef.current?.querySelector<HTMLInputElement>('input')?.focus();
        return () => {
            window.removeEventListener('keydown', onKey);
            invoker?.focus?.();
        };
    }, [installOpen]);

    const selectedNode = useMemo(
        () => state.nodes.find((node) => node.node_id === nodeID) || null,
        [state.nodes, nodeID],
    );

    const save = async () => {
        const target = choice === 'private' ? nodeID : '';
        if (choice === 'private' && !target) {
            showToastMessage(t('Pick a private maclawsrv node first.', '请先选择私有 maclawsrv 节点。'));
            return;
        }
        setSaving(true);
        try {
            const next = await SetCloudSrvChoice(target) as CloudSrvState;
            const st: CloudSrvState = { node_id: String(next?.node_id || ''), nodes: Array.isArray(next?.nodes) ? next.nodes : [] };
            setState(st);
            choiceInitialized.current = true;
            setChoice(st.node_id ? 'private' : 'public');
            setNodeID(st.node_id);
            showToastMessage(t('Cloud agent srv choice saved.', '云端 Agent 的连接方式已保存。'));
        } catch (err) {
            showToastMessage(errorText(err) || t('Save failed.', '保存失败。'));
        } finally {
            setSaving(false);
        }
    };

    const runProbe = async () => {
        setProbing(true);
        setProbe(null);
        try {
            const result = await ProbeCloudSrvSSH({ host: host.trim(), port: Number(port) || 22, user: user.trim(), password }) as CloudSrvProbe;
            setProbe(result);
        } catch (err) {
            setProbe({ ok: false, message: errorText(err) });
        } finally {
            setProbing(false);
        }
    };

    const install = async () => {
        if (!host.trim() || !user.trim() || !password) {
            showToastMessage(t('Fill in host, user and password first.', '请先填写服务器地址、用户名和密码。'));
            return;
        }
        setInstalling(true);
        setInstallNote('');
        try {
            const rec = await DeployDesktopNode({
                host: host.trim(),
                port: Number(port) || 22,
                user: user.trim(),
                password,
                with_srv: true,
                srv_only: true,
            });
            // Watch for the new node so the install button stays in its
            // spinner state (no duplicate installs) until registration lands.
            deployWatchRef.current = new Set(state.nodes.map((node) => node.node_id));
            setDeploying(true);
            setInstallNote(t(
                'Install agent started on ' + host.trim() + '. Waiting for it to register on the Hub…',
                '安装 agent 已在 ' + host.trim() + ' 上启动，等待注册到 Hub…',
            ));
            showToastMessage(t('Private maclawsrv install started.', '私有 maclawsrv 安装已开始。'));
            for (const delay of [5000, 15000, 30000, 60000, 90000, 120000, 150000]) {
                refreshTimersRef.current.push(window.setTimeout(() => { void refresh(); }, delay));
            }
            // Failsafe: if the node never registers (server install failed
            // silently), release the button instead of spinning forever.
            refreshTimersRef.current.push(window.setTimeout(() => {
                if (!deployWatchRef.current) return;
                deployWatchRef.current = null;
                setDeploying(false);
                setInstallNote(t(
                    'The node has not registered yet. The install may still be running on the server — use Refresh to check, or try installing again.',
                    '节点尚未注册。安装可能仍在服务器后台进行——可点「刷新」查看，或稍后重试安装。',
                ));
            }, 180000));
            void rec;
        } catch (err) {
            showToastMessage(errorText(err) || t('Install failed.', '安装失败。'));
        } finally {
            setInstalling(false);
        }
    };

    const installFormValid = host.trim() !== '' && user.trim() !== '' && password !== '';
    const installBusy = probing || installing || deploying;

    const startUpgrade = async (node: CloudSrvNodeView) => {
        if (!upgradePassword) {
            showToastMessage(t('Enter the SSH password first.', '请先输入 SSH 密码。'));
            return;
        }
        setUpgrading(true);
        try {
            await UpgradeCloudSrvNode(node.node_id || node.host, upgradePassword);
            showToastMessage(t(
                'Upgrade started for ' + (node.name || node.host) + '. Watch the task list for progress.',
                (node.name || node.host) + ' 的升级已开始，可在任务列表查看进度。',
            ));
            setUpgradeTarget('');
            setUpgradePassword('');
            // Poll the row status while the upgrade agent works on the server.
            for (const delay of [30000, 90000, 180000, 300000, 420000]) {
                refreshTimersRef.current.push(window.setTimeout(() => { void refresh(); }, delay));
            }
            await refresh();
        } catch (err) {
            showToastMessage(errorText(err) || t('Upgrade failed to start.', '升级启动失败。'));
        } finally {
            setUpgrading(false);
        }
    };

    const removeNode = async (node: CloudSrvNodeView) => {
        const label = node.name || node.host;
        const ok = await showConfirm(
            t(
                'Delete ' + label + '? The node unregisters on the Hub and the saved SSH target is forgotten. The software on the server keeps running and can be installed again.',
                '删除 ' + label + '？该节点将在 Hub 上取消注册，本机保存的 SSH 配置会被清除。服务器上的软件不会卸载，之后可以重新安装。',
            ),
            t('Delete private node', '删除私有节点'),
        );
        if (!ok) return;
        try {
            await DeleteCloudSrvNode(node.node_id || node.host);
            showToastMessage(t(label + ' deleted.', label + ' 已删除。'));
            await refresh();
        } catch (err) {
            showToastMessage(errorText(err) || t('Delete failed.', '删除失败。'));
        }
    };

    // Editing any SSH field invalidates the previous probe result and install
    // note: they describe the connection that was checked, not the edited one.
    const setInstallField = (setter: (value: string) => void, value: string) => {
        setter(value);
        setProbe(null);
        setInstallNote('');
    };

    // The resolved cloud default from the LLM panel state: follow shows the
    // assistant's live pick, a custom row names its provider, and an unusable
    // row degrades to the official display — the Go resolver
    // (maclawLLMCloudProfileSummary) applies the same rule the runtime does.
    const cloudSummary = llmState?.cloud;
    const cloudModeLabel = cloudSummary
        ? (cloudSummary.inherit_assistant
            ? t('Follow AI assistant', '跟随普通 AI 助手')
            : cloudSummary.provider_name
                ? t('Custom', '自定义')
                : t('MaClaw official', 'MaClaw 官方'))
        : null;
    const cloudDetailLabel = cloudSummary?.provider_name
        ? [cloudSummary.provider_name, cloudSummary.model].filter(Boolean).join(' · ')
        : '';

    return (
        <div className="cloud-agent-settings">
            <section className="im-settings-card">
                <div className="cloud-agent-section-head">
                    <span className="cloud-agent-section-icon"><CloudIcon /></span>
                    <div className="cloud-agent-section-head__text">
                        <h3 className="cloud-agent-section-title">{t('Cloud connection', '云端连接')}</h3>
                        <p className="cloud-agent-section-sub">
                            {t(
                                'Choose which maclawsrv runs your cloud tasks. Public uses the system default from the Hub admin; private runs on a maclawsrv you installed yourself.',
                                '选择云端任务跑在哪个 maclawsrv 上。公有使用系统缺省；私有使用你自己安装的 maclawsrv。',
                            )}
                        </p>
                    </div>
                </div>

                <div className="cloud-agent-choice-grid" role="radiogroup" aria-label={t('Cloud connection mode', '云端连接方式')}>
                    <label className="cloud-agent-choice" data-active={choice === 'public'}>
                        <input
                            type="radio"
                            name="cloud-srv-choice"
                            checked={choice === 'public'}
                            disabled={saving}
                            onChange={() => setChoice('public')}
                        />
                        <span className="cloud-agent-choice__body">
                            <span className="cloud-agent-choice__title">{t('Public maclawsrv (default)', '公有 maclawsrv（默认）')}</span>
                            <span className="cloud-agent-choice__desc">{t('Managed by the Hub admin, zero setup.', '由 Hub 管理员统一维护，无需自行部署。')}</span>
                        </span>
                    </label>
                    <label className="cloud-agent-choice" data-active={choice === 'private'}>
                        <input
                            type="radio"
                            name="cloud-srv-choice"
                            checked={choice === 'private'}
                            disabled={saving}
                            onChange={() => setChoice('private')}
                        />
                        <span className="cloud-agent-choice__body">
                            <span className="cloud-agent-choice__title">{t('Private maclawsrv', '私有 maclawsrv')}</span>
                            <span className="cloud-agent-choice__desc">{t('Runs on your own server, installed below.', '跑在你自己的服务器上，可在下方安装。')}</span>
                        </span>
                    </label>
                </div>

                {choice === 'private' && (
                    <div className="cloud-agent-node-picker">
                        <select
                            className="form-input"
                            value={nodeID}
                            disabled={saving}
                            onChange={(event) => setNodeID(event.target.value)}
                        >
                            <option value="">{t('Select a node…', '选择节点…')}</option>
                            {state.nodes.map((node) => (
                                <option key={node.node_id} value={node.node_id}>
                                    {node.name || node.node_id}{node.address ? ' · ' + node.address : ''}{node.online ? '' : ' · ' + t('offline', '离线')}
                                </option>
                            ))}
                        </select>
                        {selectedNode && (
                            <span className="cloud-agent-status" data-online={selectedNode.online}>
                                <span className="cloud-agent-status__dot" />
                                {selectedNode.online ? t('Online', '在线') : t('Offline', '离线')}
                            </span>
                        )}
                    </div>
                )}
                {choice === 'private' && selectedNode && !selectedNode.online && (
                    <p role="status" className="cloud-agent-banner cloud-agent-banner--error">
                        {t('This node is offline right now; cloud tasks will fail until its link comes back.', '这个节点当前离线，链路恢复前云端任务会执行失败。')}
                    </p>
                )}
                {choice === 'private' && state.nodes.length === 0 && !loading && (
                    <p className="cloud-agent-banner cloud-agent-banner--info">
                        {t('No private maclawsrv yet. Install one below, then pick it here.', '还没有私有 maclawsrv，先在下方安装一个再回来选择。')}
                    </p>
                )}

                <div className="cloud-agent-actions cloud-agent-actions--start">
                    <button type="button" className="im-settings-button im-settings-button--accent" disabled={saving || loading} onClick={() => void save()}>
                        {saving ? t('Saving…', '保存中…') : t('Save', '保存')}
                    </button>
                    <button type="button" className="im-settings-button" disabled={loading} onClick={() => void refresh()}>
                        {t('Refresh', '刷新')}
                    </button>
                </div>
                {loadError && <p role="status" className="cloud-agent-banner cloud-agent-banner--error">{loadError}</p>}
            </section>

            <section className="im-settings-card">
                <div className="cloud-agent-section-head">
                    <span className="cloud-agent-section-icon"><CpuIcon /></span>
                    <div className="cloud-agent-section-head__text">
                        <h3 className="cloud-agent-section-title">{t('Default model', '默认模型')}</h3>
                        <p className="cloud-agent-section-sub">
                            {t(
                                'The default model for cloud tasks. Adjust it in 大模型配置 → 云端 Agent; saving applies it to every existing instance, and new instances pick it up automatically.',
                                '云端任务的默认模型。在「大模型配置 → 云端 Agent」中调整；保存后应用到全部实例，新建实例自动使用。',
                            )}
                        </p>
                    </div>
                </div>

                {cloudSummary ? (
                    <div className="cloud-agent-actions cloud-agent-actions--start">
                        <span className="cloud-agent-chip">{cloudModeLabel}</span>
                        {cloudDetailLabel && <span className="cloud-agent-chip cloud-agent-chip--current">{cloudDetailLabel}</span>}
                        <button
                            type="button"
                            className="im-settings-button im-settings-button--accent"
                            onClick={() => { openSettingsTab('llm'); }}
                        >
                            {t('Settings', '设置')}
                        </button>
                    </div>
                ) : (
                    !loading && (
                        <p className="cloud-agent-banner cloud-agent-banner--info">
                            {t('Could not read the current model setting.', '暂时无法读取当前模型设置。')}
                        </p>
                    )
                )}
            </section>

            <section className="im-settings-card">
                <div className="cloud-agent-section-head">
                    <span className="cloud-agent-section-icon"><ServerIcon /></span>
                    <div className="cloud-agent-section-head__text">
                        <h3 className="cloud-agent-section-title">{t('Private cloud service', '私有云端服务')}</h3>
                        <p className="cloud-agent-section-sub">
                            {t(
                                'Install your own maclawsrv on a Linux server over SSH — MaClawSrv only, no desktop (desktopd). The account must be root or able to sudo; use Check to verify before installing. The server registers on this Hub automatically when done.',
                                '通过 SSH 把私有 maclawsrv 一键装到你的 Linux 服务器。只安装 MaClawSrv 和隧道，不部署桌面（desktopd）。账号需要是 root 或能 sudo，建议先用「检测」确认；装完会自动注册到当前连接的 Hub。',
                            )}
                        </p>
                    </div>
                </div>
                <div className="cloud-agent-deploy-row">
                    {!loading && (
                        <span className="cloud-agent-deploy-count">
                            {t(
                                state.nodes.length === 1 ? '1 private node installed.' : state.nodes.length + ' private nodes installed.',
                                state.nodes.length === 1 ? '已安装 1 个私有节点。' : '已安装 ' + state.nodes.length + ' 个私有节点。',
                            )}
                        </span>
                    )}
                    {/* Always enabled: reopening mid-install is how the user
                        gets back to the live progress in the dialog. */}
                    <button
                        type="button"
                        className="im-settings-button im-settings-button--accent"
                        onClick={() => setInstallOpen(true)}
                    >
                        {t('Install private cloud service', '安装私有云端服务')}
                    </button>
                </div>
                {installNote && !installOpen && (
                    <p role="status" className="cloud-agent-banner cloud-agent-banner--info">{installNote}</p>
                )}
                {srvNodes.length > 0 && (
                    <div className="cloud-agent-srv-list">
                        {srvNodes.map((node) => {
                            const rowKey = node.node_id || node.host;
                            return (
                                <div key={rowKey} className="cloud-agent-srv-row">
                                    <div className="cloud-agent-srv-row__meta">
                                        <span className="cloud-agent-srv-row__name">{node.name || node.host}</span>
                                        <span className="cloud-agent-srv-row__addr">
                                            {node.user}@{node.host}{node.port && node.port !== 22 ? ':' + node.port : ''}
                                        </span>
                                    </div>
                                    <span className="cloud-agent-status" data-online={!!node.registered && !!node.online}>
                                        <span className="cloud-agent-status__dot" />
                                        {node.busy
                                            ? t('Working…', '任务进行中…')
                                            : node.online
                                                ? t('Online', '在线')
                                                : node.registered
                                                    ? t('Offline', '离线')
                                                    : t('Not registered', '未注册')}
                                    </span>
                                    <div className="cloud-agent-srv-row__actions">
                                        {upgradeTarget === rowKey ? (
                                            <>
                                                <input
                                                    type="password"
                                                    className="form-input cloud-agent-pw"
                                                    placeholder={t('SSH password', 'SSH 密码')}
                                                    value={upgradePassword}
                                                    autoComplete="new-password"
                                                    disabled={upgrading}
                                                    onChange={(event) => setUpgradePassword(event.target.value)}
                                                />
                                                <button
                                                    type="button"
                                                    className="im-settings-button im-settings-button--accent"
                                                    disabled={upgrading || !upgradePassword}
                                                    onClick={() => void startUpgrade(node)}
                                                >
                                                    {upgrading && <span className="cloud-agent-spinner" aria-hidden="true" />}
                                                    {upgrading ? t('Upgrading…', '升级中…') : t('Confirm upgrade', '确认升级')}
                                                </button>
                                                <button
                                                    type="button"
                                                    className="im-settings-button"
                                                    disabled={upgrading}
                                                    onClick={() => {
                                                        setUpgradeTarget('');
                                                        setUpgradePassword('');
                                                    }}
                                                >
                                                    {t('Cancel', '取消')}
                                                </button>
                                            </>
                                        ) : (
                                            <>
                                                <button
                                                    type="button"
                                                    className="im-settings-button"
                                                    disabled={node.busy || upgrading}
                                                    onClick={() => {
                                                        setUpgradeTarget(rowKey);
                                                        setUpgradePassword('');
                                                    }}
                                                >
                                                    {node.busy && <span className="cloud-agent-spinner" aria-hidden="true" />}
                                                    {node.busy ? t('Working…', '任务进行中…') : t('Update', '更新')}
                                                </button>
                                                <button
                                                    type="button"
                                                    className="im-settings-button im-settings-button--danger"
                                                    disabled={node.busy || upgrading}
                                                    onClick={() => void removeNode(node)}
                                                >
                                                    {t('Delete', '删除')}
                                                </button>
                                            </>
                                        )}
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                )}
            </section>

            {installOpen && (
                <div
                    className="modal-overlay"
                    onMouseDown={(event) => {
                        if (event.target === event.currentTarget) setInstallOpen(false);
                    }}
                >
                    <div ref={installDialogRef} className="modal-content cloud-agent-install-modal" role="dialog" aria-modal="true" aria-labelledby="cloud-agent-install-title">
                        <div className="cloud-agent-modal-head">
                            <h3 id="cloud-agent-install-title">{t('Install private cloud service', '安装私有云端服务')}</h3>
                            <button className="modal-close" onClick={() => setInstallOpen(false)} aria-label={t('Close', '关闭')}>&times;</button>
                        </div>
                        <p className="cloud-agent-section-sub">
                            {t(
                                'The install runs over SSH. The account must be root or able to sudo; use Check to verify before installing. The server registers on this Hub automatically when done.',
                                '安装通过 SSH 进行，账号需要是 root 或能 sudo，建议先用「检测」确认；装完会自动注册到当前连接的 Hub。',
                            )}
                        </p>
                        <div className="cloud-agent-install-form">
                            <div className="cloud-agent-field cloud-agent-field--wide">
                                <label htmlFor="cloud-agent-host">{t('Server IP or hostname', '服务器 IP 或主机名')}</label>
                                <input
                                    id="cloud-agent-host"
                                    className="form-input"
                                    placeholder={t('e.g. 203.0.113.10', '例如 203.0.113.10')}
                                    value={host}
                                    disabled={installBusy}
                                    onChange={(event) => setInstallField(setHost, event.target.value)}
                                />
                            </div>
                            <div className="cloud-agent-field">
                                <label htmlFor="cloud-agent-port">{t('SSH port', 'SSH 端口')}</label>
                                <input
                                    id="cloud-agent-port"
                                    className="form-input"
                                    placeholder="22"
                                    value={port}
                                    disabled={installBusy}
                                    onChange={(event) => setInstallField(setPort, event.target.value)}
                                />
                            </div>
                            <div className="cloud-agent-field">
                                <label htmlFor="cloud-agent-user">{t('SSH user', 'SSH 用户名')}</label>
                                <input
                                    id="cloud-agent-user"
                                    className="form-input"
                                    placeholder="root"
                                    value={user}
                                    disabled={installBusy}
                                    onChange={(event) => setInstallField(setUser, event.target.value)}
                                />
                            </div>
                            <div className="cloud-agent-field cloud-agent-field--wide">
                                <label htmlFor="cloud-agent-password">{t('SSH password', 'SSH 密码')}</label>
                                <input
                                    id="cloud-agent-password"
                                    className="form-input"
                                    type="password"
                                    autoComplete="new-password"
                                    value={password}
                                    disabled={installBusy}
                                    onChange={(event) => setInstallField(setPassword, event.target.value)}
                                />
                            </div>
                        </div>
                        {probe && (
                            <p role="status" className={probe.ok ? 'cloud-agent-banner cloud-agent-banner--ok' : 'cloud-agent-banner cloud-agent-banner--error'}>
                                {probe.message}
                            </p>
                        )}
                        {installNote && (
                            <p role="status" className="cloud-agent-banner cloud-agent-banner--info">{installNote}</p>
                        )}
                        <div className="cloud-agent-actions">
                            <button
                                type="button"
                                className="im-settings-button"
                                disabled={installBusy || !installFormValid}
                                onClick={() => void runProbe()}
                            >
                                {probing ? t('Checking…', '检测中…') : t('Check sudo', '检测 sudo')}
                            </button>
                            <button
                                type="button"
                                className="im-settings-button im-settings-button--accent"
                                disabled={installBusy || !installFormValid}
                                onClick={() => void install()}
                            >
                                {(installing || deploying) && <span className="cloud-agent-spinner" aria-hidden="true" />}
                                {installing || deploying ? t('Installing…', '安装中…') : t('Install', '安装')}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
};
