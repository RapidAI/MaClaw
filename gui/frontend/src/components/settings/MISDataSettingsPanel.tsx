import { useEffect, useMemo, useState } from 'react';
import type { ReactNode } from 'react';
import { GetMISDataConfig, SaveMISDataConfig, TestMISDataConnection } from '../../../wailsjs/go/main/App';

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
        setMessage(t('Saved', '\u5df2\u4fdd\u5b58', '\u5df2\u5132\u5b58'));
    });

    const test = () => run('test', async () => {
        const result = await TestMISDataConnection(config as any) as ConnectionStatus;
        setStatus(result || null);
        if (result?.ok && result?.auth_ok) {
            setMessage(t('Connection verified', '\u8fde\u63a5\u5df2\u9a8c\u8bc1', '\u9023\u7dda\u5df2\u9a57\u8b49'));
        }
    });

    const canAct = !busy;

    return (
        <div className="mis-data-settings-panel">
            <div className="mis-data-settings-header">
                <div>
                    <h2>{t('MIS Data', 'MIS \u6570\u636e', 'MIS \u8cc7\u6599')}</h2>
                    <p>{t('Configure MaClawDataSrv for enterprise sales, HR, and finance data.', '\u914d\u7f6e MaClawDataSrv\uff0c\u7528\u4e8e\u516c\u53f8\u9500\u552e\u3001\u4eba\u529b\u3001\u8d22\u52a1\u7b49\u7ed3\u6784\u5316\u6570\u636e\u3002', '\u914d\u7f6e MaClawDataSrv\uff0c\u7528\u65bc\u516c\u53f8\u92b7\u552e\u3001\u4eba\u529b\u3001\u8ca1\u52d9\u7b49\u7d50\u69cb\u5316\u8cc7\u6599\u3002')}</p>
                </div>
                <span className="mis-data-settings-badge" data-enabled={config.enabled ? 'true' : 'false'}>
                    {config.enabled ? t('Enabled', '\u5df2\u542f\u7528', '\u5df2\u555f\u7528') : t('Disabled', '\u672a\u542f\u7528', '\u672a\u555f\u7528')}
                </span>
            </div>

            {error && <div role="alert" className="mis-data-settings-alert mis-data-settings-alert--error">{error}</div>}
            {message && <div role="status" className="mis-data-settings-alert mis-data-settings-alert--success">{message}</div>}

            <section className="mis-data-settings-card">
                <div className="mis-data-settings-card-title">{t('Service Connection', '\u670d\u52a1\u8fde\u63a5', '\u670d\u52d9\u9023\u7dda')}</div>
                <label className="mis-data-settings-toggle">
                    <input type="checkbox" aria-label={t('Enable MIS data tools for MaClaw agents', '\u542f\u7528 MaClaw agent \u7684 MIS \u6570\u636e\u5de5\u5177', '\u555f\u7528 MaClaw agent \u7684 MIS \u8cc7\u6599\u5de5\u5177')} checked={!!config.enabled} onChange={e => update({ enabled: e.target.checked })} />
                    <span>{t('Enable MIS data tools for MaClaw agents', '\u542f\u7528 MaClaw agent \u7684 MIS \u6570\u636e\u5de5\u5177', '\u555f\u7528 MaClaw agent \u7684 MIS \u8cc7\u6599\u5de5\u5177')}</span>
                </label>
                <div className="mis-data-settings-grid">
                    <Field label={t('Service URL', '\u670d\u52a1\u5730\u5740', '\u670d\u52d9\u4f4d\u5740')}>
                        <input value={config.endpoint || ''} onChange={e => update({ endpoint: e.target.value })} placeholder="http://127.0.0.1:18180" />
                    </Field>
                    <Field label="Tenant ID">
                        <input value={config.tenant_id || ''} onChange={e => update({ tenant_id: e.target.value })} placeholder="default" />
                    </Field>
                    <Field label="User ID">
                        <input value={config.user_id || ''} onChange={e => update({ user_id: e.target.value })} placeholder="maclaw" />
                    </Field>
                    <Field label="Role">
                        <select value={config.role || 'data_user'} onChange={e => update({ role: e.target.value })}>
                            <option value="data_user">data_user</option>
                            <option value="data_admin">data_admin</option>
                            <option value="data_auditor">data_auditor</option>
                        </select>
                    </Field>
                    <Field label="Token">
                        <input value={config.token || ''} onChange={e => update({ token: e.target.value })} type="password" placeholder="mcd_xxx" autoComplete="off" />
                    </Field>
                </div>
                <div className="mis-data-settings-actions">
                    <button type="button" onClick={save} disabled={!canAct} className="mis-data-settings-primary">{busy === 'save' ? t('Saving...', '\u4fdd\u5b58\u4e2d...', '\u5132\u5b58\u4e2d...') : t('Save', '\u4fdd\u5b58', '\u5132\u5b58')}</button>
                    <button type="button" onClick={test} disabled={!canAct}>{busy === 'test' ? t('Testing...', '\u6d4b\u8bd5\u4e2d...', '\u6e2c\u8a66\u4e2d...') : t('Test Connection', '\u6d4b\u8bd5\u8fde\u63a5', '\u6e2c\u8a66\u9023\u7dda')}</button>
                </div>
            </section>

            <section className="mis-data-settings-card">
                <div className="mis-data-settings-card-title">{t('Connection Status', '\u8fde\u63a5\u72b6\u6001', '\u9023\u7dda\u72c0\u614b')}</div>
                {!status ? (
                    <div className="mis-data-settings-empty">{t('No connection test yet.', '\u5c1a\u672a\u6d4b\u8bd5\u8fde\u63a5\u3002', '\u5c1a\u672a\u6e2c\u8a66\u9023\u7dda\u3002')}</div>
                ) : (
                    <div className="mis-data-settings-status-grid">
                        <Metric label={t('Service', '\u670d\u52a1', '\u670d\u52d9')} value={status.ok ? t('Ready', '\u5c31\u7eea', '\u5c31\u7dd2') : t('Unavailable', '\u4e0d\u53ef\u7528', '\u4e0d\u53ef\u7528')} tone={status.ok ? 'good' : 'bad'} />
                        <Metric label={t('Auth', '\u8ba4\u8bc1', '\u8a8d\u8b49')} value={status.auth_ok ? t('Passed', '\u901a\u8fc7', '\u901a\u904e') : t('Failed', '\u5931\u8d25', '\u5931\u6557')} tone={status.auth_ok ? 'good' : 'bad'} />
                        <Metric label={t('Engine', '\u5f15\u64ce', '\u5f15\u64ce')} value={status.engine || '-'} />
                        <Metric label="Schema" value={status.schema_version ? String(status.schema_version) : '-'} />
                    </div>
                )}
                {status?.error && <div className="mis-data-settings-alert mis-data-settings-alert--error">{status.error}</div>}
            </section>
        </div>
    );
}

function Field({ label, children }: { label: string; children: ReactNode }) {
    return <label className="mis-data-settings-field"><span>{label}</span>{children}</label>;
}

function Metric({ label, value, tone }: { label: string; value: string; tone?: 'good' | 'bad' }) {
    return <div className="mis-data-settings-metric" data-tone={tone || 'neutral'}><div>{label}</div><strong>{value}</strong></div>;
}

function makeTranslator(lang: string) {
    return (en: string, zhHans: string, zhHant: string = zhHans) => lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en;
}