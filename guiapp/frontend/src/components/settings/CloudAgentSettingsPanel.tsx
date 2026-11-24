import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import {
    DeployDesktopNode,
    GetCloudSrvChoice,
    ProbeCloudSrvSSH,
    SetCloudSrvChoice,
} from '../../../wailsjs/go/main/App';
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

type CloudSrvProbe = {
    ok: boolean;
    method?: string;
    message: string;
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

export const CloudAgentSettingsPanel = ({ lang, showToastMessage }: CloudAgentSettingsPanelProps) => {
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

    const choiceInitialized = useRef(false);
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
    }, []);

    useEffect(() => { void refresh(); }, [refresh]);

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
            });
            setInstallNote(t(
                'Install started on ' + host.trim() + '. The node shows up here once it registers on the Hub.',
                '已开始安装 ' + host.trim() + '，注册到 Hub 后会出现在上面的节点列表里。',
            ));
            showToastMessage(t('Private maclawsrv install started.', '私有 maclawsrv 安装已开始。'));
            for (const delay of [5000, 15000, 30000, 60000]) {
                window.setTimeout(() => { void refresh(); }, delay);
            }
            void rec;
        } catch (err) {
            showToastMessage(errorText(err) || t('Install failed.', '安装失败。'));
        } finally {
            setInstalling(false);
        }
    };

    return (
        <div className="cloud-agent-settings">
            <section className="im-settings-card">
                <p className="im-settings-description">
                    {t(
                        'Choose which maclawsrv runs your cloud tasks. Public uses the system default from the Hub admin; private runs on a maclawsrv you installed yourself.',
                        '选择云端任务跑在哪个 maclawsrv 上。公有使用系统缺省；私有使用你自己安装的 maclawsrv。',
                    )}
                </p>
                <div className="im-settings-toolbar" style={{ flexDirection: 'column', alignItems: 'stretch', gap: 8 }}>
                    <label className="im-settings-description" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <input type="radio" name="cloud-srv-choice" checked={choice === 'public'} disabled={saving} onChange={() => setChoice('public')} />
                        {t('Public maclawsrv (default)', '公有 maclawsrv（默认）')}
                    </label>
                    <label className="im-settings-description" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                        <input type="radio" name="cloud-srv-choice" checked={choice === 'private'} disabled={saving} onChange={() => setChoice('private')} />
                        {t('Private maclawsrv', '私有 maclawsrv')}
                    </label>
                    {choice === 'private' && (
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
                    )}
                    {choice === 'private' && selectedNode && !selectedNode.online && (
                        <p className="im-settings-description" style={{ color: 'var(--theme-danger)' }}>
                            {t('This node is offline right now; cloud tasks will fail until its link comes back.', '这个节点当前离线，链路恢复前云端任务会执行失败。')}
                        </p>
                    )}
                    {choice === 'private' && state.nodes.length === 0 && !loading && (
                        <p className="im-settings-description">
                            {t('No private maclawsrv yet. Install one below, then pick it here.', '还没有私有 maclawsrv，先在下方安装一个再回来选择。')}
                        </p>
                    )}
                </div>
                <div className="im-settings-toolbar" style={{ marginTop: 10 }}>
                    <button type="button" className="im-settings-button im-settings-button--accent" disabled={saving || loading} onClick={() => void save()}>
                        {saving ? t('Saving…', '保存中…') : t('Save', '保存')}
                    </button>
                    <button type="button" className="im-settings-button" disabled={loading} onClick={() => void refresh()}>
                        {t('Refresh', '刷新')}
                    </button>
                </div>
                {loadError && <p className="im-settings-description" style={{ color: 'var(--theme-danger)' }}>{loadError}</p>}
            </section>

            <section className="im-settings-card">
                <p className="im-settings-description">
                    {t(
                        'Install your own maclawsrv on a Linux server over SSH. The account must be root or able to sudo; use Check to verify before installing. The server registers on this Hub automatically when done.',
                        '通过 SSH 把私有 maclawsrv 一键装到你的 Linux 服务器。账号需要是 root 或能 sudo，建议先用「检测」确认；装完会自动注册到当前连接的 Hub。',
                    )}
                </p>
                <div className="im-settings-toolbar" style={{ flexDirection: 'column', alignItems: 'stretch', gap: 8 }}>
                    <input className="form-input" placeholder={t('Server IP or hostname', '服务器 IP 或主机名')} value={host} disabled={probing || installing} onChange={(event) => setHost(event.target.value)} />
                    <input className="form-input" placeholder={t('SSH port (default 22)', 'SSH 端口（默认 22）')} value={port} disabled={probing || installing} onChange={(event) => setPort(event.target.value)} />
                    <input className="form-input" placeholder={t('SSH user', 'SSH 用户名')} value={user} disabled={probing || installing} onChange={(event) => setUser(event.target.value)} />
                    <input className="form-input" type="password" placeholder={t('SSH password', 'SSH 密码')} value={password} disabled={probing || installing} onChange={(event) => setPassword(event.target.value)} />
                </div>
                <div className="im-settings-toolbar" style={{ marginTop: 10 }}>
                    <button type="button" className="im-settings-button" disabled={probing || installing || !host.trim() || !user.trim() || !password} onClick={() => void runProbe()}>
                        {probing ? t('Checking…', '检测中…') : t('Check sudo', '检测 sudo')}
                    </button>
                    <button type="button" className="im-settings-button im-settings-button--accent" disabled={probing || installing || !host.trim() || !user.trim() || !password} onClick={() => void install()}>
                        {installing ? t('Installing…', '安装中…') : t('Install private maclawsrv', '安装私有 maclawsrv')}
                    </button>
                </div>
                {probe && (
                    <p className="im-settings-description" style={{ color: probe.ok ? undefined : 'var(--theme-danger)' }}>
                        {probe.message}
                    </p>
                )}
                {installNote && <p className="im-settings-description">{installNote}</p>}
            </section>
        </div>
    );
};
