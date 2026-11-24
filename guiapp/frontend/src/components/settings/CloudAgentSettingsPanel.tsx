import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
    DeployDesktopNode,
    GetCloudSrvChoice,
    GetMaclawLLMProviders,
    ListCloudAgents,
    PatchConfigFields,
    ProbeCloudSrvSSH,
    ResetCloudAgentLLM,
    SetCloudAgentLLM,
    SetCloudSrvChoice,
} from '../../../wailsjs/go/main/App';
import { corelib } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';

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

type CloudAgentRow = {
    id: string;
    name: string;
    kind: string;
    status: string;
    llm_mode: string;
    llm_label: string;
    llm_model: string;
};

type CloudAgentList = {
    instances: CloudAgentRow[];
    enabled: boolean;
};

type MaclawLLMProvider = {
    id?: string;
    name: string;
    url: string;
    key: string;
    model: string;
    models?: string[];
    is_hub_service?: boolean;
};

type MaclawLLMProviderList = {
    providers: MaclawLLMProvider[];
    current: string;
};

type CloudSrvProbe = {
    ok: boolean;
    method?: string;
    message: string;
};

type CloudAgentSettingsPanelProps = {
    lang: string;
    showToastMessage: (message: string) => void;
    config: corelib.AppConfig | null;
    setConfig: (config: corelib.AppConfig) => void;
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

export const CloudAgentSettingsPanel = ({ lang, showToastMessage, config, setConfig }: CloudAgentSettingsPanelProps) => {
    const t = useCallback((en: string, zhHans: string, zhHant?: string) => textForLang(lang, en, zhHans, zhHant || zhHans), [lang]);

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
    const [installNote, setInstallNote] = useState('');
    const [installOpen, setInstallOpen] = useState(false);

    const [instances, setInstances] = useState<CloudAgentRow[]>([]);
    const [providers, setProviders] = useState<MaclawLLMProvider[]>([]);
    // 云端 Agent 全局默认模型：'' = MaClaw 官方，否则是服务商的稳定 ID（或名称）。
    // 保存扇出到现有实例；新建实例由 CreateCloudAgent 在 Go 侧自动应用。
    const [defaultProvider, setDefaultProvider] = useState('');
    const [defaultModel, setDefaultModel] = useState('');
    const [savingDefault, setSavingDefault] = useState(false);

    const choiceInitialized = useRef(false);
    const defaultSeeded = useRef(false);
    // Install-start schedules delayed node-list refreshes; clear them on
    // unmount so a closed settings panel never sets state afterwards.
    const refreshTimersRef = useRef<number[]>([]);
    useEffect(() => () => {
        for (const id of refreshTimersRef.current) window.clearTimeout(id);
        refreshTimersRef.current = [];
    }, []);
    const refresh = useCallback(async () => {
        try {
            const next = await GetCloudSrvChoice() as CloudSrvState;
            const st: CloudSrvState = { node_id: String(next?.node_id || ''), nodes: Array.isArray(next?.nodes) ? next.nodes : [] };
            setState(st);
            // Seed the form once from the saved choice; later refreshes must
            // not clobber what the user is currently picking.
            if (!choiceInitialized.current) {
                choiceInitialized.current = true;
                setChoice(st.node_id ? 'private' : 'public');
                setNodeID(st.node_id);
            }
            setLoadError('');
        } catch (err) {
            setLoadError(errorText(err));
        } finally {
            setLoading(false);
        }
        try {
            const list = await ListCloudAgents() as CloudAgentList;
            setInstances(Array.isArray(list?.instances) ? list.instances : []);
        } catch (err) {
            void err;
        }
        try {
            const llm = await GetMaclawLLMProviders() as MaclawLLMProviderList;
            setProviders(Array.isArray(llm?.providers) ? llm.providers : []);
        } catch (err) {
            void err;
        }
    }, []);

    useEffect(() => { void refresh(); }, [refresh]);

    // Seed the default-model pick once from the saved desktop config; later
    // config syncs must not clobber an in-progress edit.
    useEffect(() => {
        if (defaultSeeded.current || !config) return;
        defaultSeeded.current = true;
        setDefaultProvider(String(config.cloud_agent_llm_provider || ''));
        setDefaultModel(String(config.cloud_agent_llm_model || ''));
    }, [config]);

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

    // Only providers with real endpoint credentials can serve a cloud
    // instance: hub-service rows point back at this Hub's official API and
    // the server would reject them anyway.
    const usableProviders = useMemo(
        () => providers.filter((p) => !p.is_hub_service && String(p.url || '').trim() !== '' && String(p.key || '').trim() !== ''),
        [providers],
    );

    // Stable select key: the provider ID when present, else the display name.
    // The Go default resolver matches by ID first, then by name.
    const providerKey = useCallback((p: MaclawLLMProvider) => {
        const id = String(p.id ?? '').trim();
        return id !== '' ? id : p.name;
    }, []);

    const modelOptionsFor = useCallback((key: string): string[] => {
        const provider = usableProviders.find((p) => providerKey(p) === key);
        if (!provider) return [];
        const list = Array.isArray(provider.models) && provider.models.length > 0
            ? provider.models
            : (provider.model ? [provider.model] : []);
        return list;
    }, [usableProviders, providerKey]);

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

    // Save the global default and fan it out to every existing instance right
    // away; new instances inherit it through CreateCloudAgent on the Go side.
    const saveDefault = async () => {
        const provider = defaultProvider === ''
            ? null
            : usableProviders.find((p) => providerKey(p) === defaultProvider);
        if (defaultProvider !== '' && !provider) {
            // The stored pick names a provider that no longer exists; the
            // select keeps it visible as 已删除的服务商 until re-picked.
            showToastMessage(t('The picked provider is gone; pick another one.', '所选服务商已不存在，请重新选择。'));
            return;
        }
        const model = defaultProvider === '' ? '' : (defaultModel || '').trim() || provider!.model || '';
        setSavingDefault(true);
        try {
            const next = await PatchConfigFields({
                cloud_agent_llm_provider: defaultProvider,
                cloud_agent_llm_model: model,
            }) as corelib.AppConfig;
            setConfig(next);
            let failed = 0;
            for (const row of instances) {
                try {
                    if (defaultProvider === '') {
                        if (row.llm_mode !== 'official') await ResetCloudAgentLLM(row.id);
                    } else if (provider) {
                        const updated = await SetCloudAgentLLM(row.id, {
                            url: provider.url,
                            key: provider.key,
                            model,
                            label: provider.name,
                        }) as CloudAgentRow;
                        setInstances((prev) => prev.map((r) => (r.id === row.id ? updated : r)));
                    }
                } catch (err) {
                    failed++;
                    void err;
                }
            }
            if (failed === 0) {
                showToastMessage(t(
                    'Default model saved' + (instances.length > 0 ? ' and applied to all instances.' : '. New instances will use it.'),
                    '默认模型已保存' + (instances.length > 0 ? '，并已应用到全部实例。' : '，新实例将直接使用该设置。'),
                ));
            } else {
                showToastMessage(t(
                    'Default saved; ' + failed + ' instance(s) could not be updated right now.',
                    '默认模型已保存；有 ' + failed + ' 个实例暂时未能更新。',
                ));
            }
            await refresh();
        } catch (err) {
            showToastMessage(errorText(err) || t('Save failed.', '保存失败。'));
        } finally {
            setSavingDefault(false);
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
            });
            setInstallNote(t(
                'Install started on ' + host.trim() + '. The node shows up here once it registers on the Hub.',
                '已开始安装 ' + host.trim() + '，注册到 Hub 后会出现在节点列表里。',
            ));
            showToastMessage(t('Private maclawsrv install started.', '私有 maclawsrv 安装已开始。'));
            for (const delay of [5000, 15000, 30000, 60000]) {
                refreshTimersRef.current.push(window.setTimeout(() => { void refresh(); }, delay));
            }
            void rec;
        } catch (err) {
            showToastMessage(errorText(err) || t('Install failed.', '安装失败。'));
        } finally {
            setInstalling(false);
        }
    };

    const installFormValid = host.trim() !== '' && user.trim() !== '' && password !== '';
    const installBusy = probing || installing;

    // Editing any SSH field invalidates the previous probe result and install
    // note: they describe the connection that was checked, not the edited one.
    const setInstallField = (setter: (value: string) => void, value: string) => {
        setter(value);
        setProbe(null);
        setInstallNote('');
    };

    const currentModelText = (row: CloudAgentRow) => (
        row.llm_mode === 'custom'
            ? [row.llm_label, row.llm_model].filter(Boolean).join(' · ') || t('Custom', '自定义')
            : t('MaClaw official (default)', 'MaClaw 官方（默认）')
    );

    // Hub instance statuses (hub/internal/cloudagent/model.go): unknown or
    // deleted rows render no status chip at all.
    const instanceStatusChip = (status: string): { label: string; tone: string } | null => {
        switch (status) {
            case 'running':
                return { label: t('Running', '运行中'), tone: 'success' };
            case 'idle':
                return { label: t('Idle', '空闲'), tone: '' };
            case 'provisioning':
                return { label: t('Provisioning', '部署中'), tone: 'info' };
            case 'stopped':
                return { label: t('Stopped', '已停止'), tone: '' };
            case 'provision_failed':
                return { label: t('Provision failed', '部署失败'), tone: 'danger' };
            default:
                return null;
        }
    };

    const defaultModelOptions = useMemo(
        () => (defaultModel && !modelOptionsFor(defaultProvider).includes(defaultModel)
            ? [defaultModel, ...modelOptionsFor(defaultProvider)]
            : modelOptionsFor(defaultProvider)),
        [defaultProvider, defaultModel, modelOptionsFor],
    );

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
                                'One model for every cloud instance. Saving applies it to all existing instances right away, and new instances pick it up automatically. The provider key is stored only on the maclawsrv that runs the instance.',
                                '所有云端实例共用一个模型。保存后立即应用到全部现有实例，新建实例也会直接使用该设置。密钥只保存在跑实例的 maclawsrv 上。',
                            )}
                        </p>
                    </div>
                </div>

                <div className="cloud-agent-default-row">
                    <select
                        className="form-input cloud-agent-select"
                        value={defaultProvider}
                        disabled={savingDefault}
                        onChange={(event) => {
                            const key = event.target.value;
                            setDefaultProvider(key);
                            const provider = usableProviders.find((p) => providerKey(p) === key);
                            setDefaultModel(provider ? (modelOptionsFor(key)[0] || provider.model || '') : '');
                        }}
                    >
                        <option value="">{t('MaClaw official (default)', 'MaClaw 官方（默认）')}</option>
                        {usableProviders.map((p) => (
                            <option key={providerKey(p)} value={providerKey(p)}>{p.name}</option>
                        ))}
                        {defaultProvider !== '' && !usableProviders.some((p) => providerKey(p) === defaultProvider) && (
                            <option value={defaultProvider}>{t('Removed provider', '已删除的服务商')}</option>
                        )}
                    </select>
                    {defaultProvider !== '' && usableProviders.some((p) => providerKey(p) === defaultProvider) && (
                        <select
                            className="form-input cloud-agent-select"
                            value={defaultModel}
                            disabled={savingDefault}
                            onChange={(event) => setDefaultModel(event.target.value)}
                        >
                            {defaultModelOptions.length === 0 && <option value="">{t('Model (provider default)', '模型（服务商默认）')}</option>}
                            {defaultModelOptions.map((m) => (
                                <option key={m} value={m}>{m}</option>
                            ))}
                        </select>
                    )}
                    <button
                        type="button"
                        className="im-settings-button im-settings-button--accent"
                        disabled={savingDefault}
                        onClick={() => void saveDefault()}
                    >
                        {savingDefault ? t('Applying…', '应用中…') : t('Save & apply', '保存并应用')}
                    </button>
                </div>

                {instances.length > 0 ? (
                    <div className="cloud-agent-instance-list">
                        {instances.map((row) => {
                            const statusChip = instanceStatusChip(row.status);
                            return (
                                <div key={row.id} className="cloud-agent-instance">
                                    <div className="cloud-agent-instance__meta">
                                        <span className="cloud-agent-instance__name">{row.name || row.id}</span>
                                        <span className="cloud-agent-instance__tags">
                                            {statusChip && (
                                                <span className={'cloud-agent-chip' + (statusChip.tone ? ' cloud-agent-chip--' + statusChip.tone : '')}>
                                                    {statusChip.label}
                                                </span>
                                            )}
                                            <span className="cloud-agent-chip">
                                                {row.kind === 'coding' ? t('coding', '编程') : t('session', '会话')}
                                            </span>
                                            <span className="cloud-agent-chip cloud-agent-chip--current">
                                                {currentModelText(row)}
                                            </span>
                                        </span>
                                    </div>
                                </div>
                            );
                        })}
                    </div>
                ) : (
                    !loading && (
                        <p className="cloud-agent-banner cloud-agent-banner--info">
                            {t('No cloud instances yet. New instances will use this model from creation.', '还没有云端实例，新建实例将直接使用该模型。')}
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
                                'Install your own maclawsrv on a Linux server over SSH. The account must be root or able to sudo; use Check to verify before installing. The server registers on this Hub automatically when done.',
                                '通过 SSH 把私有 maclawsrv 一键装到你的 Linux 服务器。账号需要是 root 或能 sudo，建议先用「检测」确认；装完会自动注册到当前连接的 Hub。',
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
                                {installing ? t('Installing…', '安装中…') : t('Install', '安装')}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
};
