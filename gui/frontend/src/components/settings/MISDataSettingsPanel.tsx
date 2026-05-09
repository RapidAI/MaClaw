import { useEffect, useMemo, useState } from 'react';
import {
    GetMISDataConfig,
    SaveMISDataConfig,
    TestMISDataConnection,
} from '../../../wailsjs/go/main/App';
import { colors, radius } from '../remote/styles';

type Props = {
    lang: string;
};

type MISDataConfig = {
    enabled?: boolean;
    endpoint?: string;
    token?: string;
    tenant_id?: string;
    user_id?: string;
    role?: string;
};

type ConnectionStatus = {
    ok?: boolean;
    auth_ok?: boolean;
    endpoint?: string;
    status?: string;
    engine?: string;
    schema_version?: number;
    error?: string;
};

const defaultConfig: MISDataConfig = {
    enabled: false,
    endpoint: 'http://127.0.0.1:18180',
    token: '',
    tenant_id: 'default',
    user_id: 'maclaw',
    role: 'data_user',
};

export function MISDataSettingsPanel({ lang }: Props) {
    const [config, setConfig] = useState<MISDataConfig>(defaultConfig);
    const [status, setStatus] = useState<ConnectionStatus | null>(null);
    const [busy, setBusy] = useState('');
    const [message, setMessage] = useState('');
    const [error, setError] = useState('');
    const t = useMemo(() => makeTranslator(lang), [lang]);

    useEffect(() => {
        let mounted = true;
        setBusy('load');
        GetMISDataConfig()
            .then((next: MISDataConfig) => {
                if (mounted) setConfig({ ...defaultConfig, ...(next || {}) });
            })
            .catch((err: any) => mounted && setError(err?.message || String(err)))
            .finally(() => mounted && setBusy(''));
        return () => { mounted = false; };
    }, []);

    const update = (patch: Partial<MISDataConfig>) => {
        setConfig(prev => ({ ...prev, ...patch }));
        setMessage('');
        setError('');
        setStatus(null);
    };

    const run = async (label: string, fn: () => Promise<void>) => {
        setBusy(label);
        setMessage('');
        setError('');
        try {
            await fn();
        } catch (err: any) {
            setError(err?.message || String(err));
        } finally {
            setBusy('');
        }
    };

    const save = () => run('save', async () => {
        await SaveMISDataConfig(config as any);
        setMessage(t('Saved', '已保存', '已儲存'));
    });

    const test = () => run('test', async () => {
        const result = await TestMISDataConnection(config as any) as ConnectionStatus;
        setStatus(result || null);
        if (result?.ok && result?.auth_ok) {
            setMessage(t('Connection verified', '连接已验证', '連線已驗證'));
        }
    });

    const canAct = !busy;

    return (
        <div style={{ display: 'grid', gap: 12 }}>
            <div style={headerStyle}>
                <div>
                    <h2 style={titleStyle}>MIS数据</h2>
                    <p style={subtleStyle}>{t('Configure MaClawDataSrv for enterprise sales, HR, and finance data.', '配置 MaClawDataSrv，用于公司销售、人力、财务等结构化数据。', '配置 MaClawDataSrv，用於公司銷售、人力、財務等結構化資料。')}</p>
                </div>
                <span style={badgeStyle(config.enabled ? 'on' : 'off')}>{config.enabled ? t('Enabled', '已启用', '已啟用') : t('Disabled', '未启用', '未啟用')}</span>
            </div>

            {error && <div role="alert" style={errorStyle}>{error}</div>}
            {message && <div role="status" style={successStyle}>{message}</div>}

            <section style={cardStyle}>
                <div style={sectionTitleStyle}>{t('Service Connection', '服务连接', '服務連線')}</div>
                <label style={checkRowStyle}>
                    <input type="checkbox" checked={!!config.enabled} onChange={e => update({ enabled: e.target.checked })} />
                    <span>{t('Enable MIS data tools for MaClaw agents', '启用 MaClaw agent 的 MIS 数据工具', '啟用 MaClaw agent 的 MIS 資料工具')}</span>
                </label>
                <div style={gridStyle}>
                    <Field label={t('Service URL', '服务地址', '服務位址')}>
                        <input value={config.endpoint || ''} onChange={e => update({ endpoint: e.target.value })} placeholder="http://127.0.0.1:18180" style={inputStyle} />
                    </Field>
                    <Field label="Tenant ID">
                        <input value={config.tenant_id || ''} onChange={e => update({ tenant_id: e.target.value })} placeholder="default" style={inputStyle} />
                    </Field>
                    <Field label="User ID">
                        <input value={config.user_id || ''} onChange={e => update({ user_id: e.target.value })} placeholder="maclaw" style={inputStyle} />
                    </Field>
                    <Field label="Role">
                        <select value={config.role || 'data_user'} onChange={e => update({ role: e.target.value })} style={inputStyle}>
                            <option value="data_user">data_user</option>
                            <option value="data_admin">data_admin</option>
                            <option value="data_auditor">data_auditor</option>
                        </select>
                    </Field>
                    <Field label="Token">
                        <input value={config.token || ''} onChange={e => update({ token: e.target.value })} type="password" placeholder="mcd_xxx" style={inputStyle} />
                    </Field>
                </div>
                <div style={actionsStyle}>
                    <button type="button" onClick={save} disabled={!canAct} style={primaryButtonStyle}>{busy === 'save' ? t('Saving...', '保存中...', '儲存中...') : t('Save', '保存', '儲存')}</button>
                    <button type="button" onClick={test} disabled={!canAct} style={buttonStyle}>{busy === 'test' ? t('Testing...', '测试中...', '測試中...') : t('Test Connection', '测试连接', '測試連線')}</button>
                </div>
            </section>

            <section style={cardStyle}>
                <div style={sectionTitleStyle}>{t('Connection Status', '连接状态', '連線狀態')}</div>
                {!status ? (
                    <div style={emptyStyle}>{t('No connection test yet.', '尚未测试连接。', '尚未測試連線。')}</div>
                ) : (
                    <div style={statusGridStyle}>
                        <Metric label={t('Service', '服务', '服務')} value={status.ok ? t('Ready', '就绪', '就緒') : t('Unavailable', '不可用', '不可用')} tone={status.ok ? 'good' : 'bad'} />
                        <Metric label={t('Auth', '认证', '認證')} value={status.auth_ok ? t('Passed', '通过', '通過') : t('Failed', '失败', '失敗')} tone={status.auth_ok ? 'good' : 'bad'} />
                        <Metric label={t('Engine', '引擎', '引擎')} value={status.engine || '-'} />
                        <Metric label={t('Schema', 'Schema', 'Schema')} value={status.schema_version ? String(status.schema_version) : '-'} />
                    </div>
                )}
                {status?.error && <div style={{ ...errorStyle, marginTop: 10 }}>{status.error}</div>}
            </section>
        </div>
    );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
    return <label style={{ display: 'grid', gap: 4 }}><span style={labelStyle}>{label}</span>{children}</label>;
}

function Metric({ label, value, tone }: { label: string; value: string; tone?: 'good' | 'bad' }) {
    const color = tone === 'good' ? colors.success : tone === 'bad' ? colors.danger : colors.text;
    return <div style={metricStyle}><div style={metricLabelStyle}>{label}</div><div style={{ ...metricValueStyle, color }}>{value}</div></div>;
}

function makeTranslator(lang: string) {
    return (en: string, zhHans: string, zhHant: string = zhHans) => lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en;
}

const headerStyle: React.CSSProperties = { display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, border: `1px solid ${colors.border}`, borderRadius: radius.lg, padding: '12px 14px', background: colors.surface };
const titleStyle: React.CSSProperties = { margin: 0, fontSize: '1rem', color: colors.text };
const subtleStyle: React.CSSProperties = { margin: '4px 0 0', fontSize: '0.76rem', color: colors.textSecondary, lineHeight: 1.5 };
const cardStyle: React.CSSProperties = { border: `1px solid ${colors.border}`, borderRadius: radius.lg, padding: 12, background: colors.surface };
const sectionTitleStyle: React.CSSProperties = { fontSize: '0.82rem', fontWeight: 700, color: colors.text, marginBottom: 10 };
const gridStyle: React.CSSProperties = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 10 };
const inputStyle: React.CSSProperties = { width: '100%', boxSizing: 'border-box', border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.bg, color: colors.text, padding: '7px 9px', fontSize: '0.8rem' };
const labelStyle: React.CSSProperties = { fontSize: '0.72rem', color: colors.textSecondary, fontWeight: 600 };
const checkRowStyle: React.CSSProperties = { display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12, fontSize: '0.78rem', color: colors.text };
const actionsStyle: React.CSSProperties = { display: 'flex', gap: 8, justifyContent: 'flex-end', marginTop: 12, flexWrap: 'wrap' };
const buttonStyle: React.CSSProperties = { border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.surfaceMuted, color: colors.text, padding: '6px 12px', fontSize: '0.76rem', fontWeight: 700, cursor: 'pointer' };
const primaryButtonStyle: React.CSSProperties = { ...buttonStyle, border: `1px solid ${colors.primary}`, background: colors.primaryLight, color: colors.primaryDark };
const errorStyle: React.CSSProperties = { border: `1px solid ${colors.danger}`, borderRadius: radius.md, background: colors.dangerBg, color: colors.danger, padding: '7px 10px', fontSize: '0.76rem' };
const successStyle: React.CSSProperties = { border: `1px solid ${colors.success}`, borderRadius: radius.md, background: colors.successBg, color: colors.success, padding: '7px 10px', fontSize: '0.76rem' };
const emptyStyle: React.CSSProperties = { color: colors.textMuted, fontSize: '0.78rem', padding: '6px 0' };
const statusGridStyle: React.CSSProperties = { display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(140px, 1fr))', gap: 8 };
const metricStyle: React.CSSProperties = { border: `1px solid ${colors.border}`, borderRadius: radius.md, background: colors.bg, padding: '8px 10px' };
const metricLabelStyle: React.CSSProperties = { fontSize: '0.68rem', color: colors.textMuted, marginBottom: 4 };
const metricValueStyle: React.CSSProperties = { fontSize: '0.9rem', fontWeight: 700 };
const badgeStyle = (tone: 'on' | 'off'): React.CSSProperties => ({ border: `1px solid ${tone === 'on' ? colors.success : colors.border}`, borderRadius: radius.pill, background: tone === 'on' ? colors.successBg : colors.surfaceMuted, color: tone === 'on' ? colors.success : colors.textMuted, padding: '3px 10px', fontSize: '0.7rem', fontWeight: 700, whiteSpace: 'nowrap' });
