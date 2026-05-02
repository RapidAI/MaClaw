import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { loadIMConfig, saveIMConfig, type IMConfig } from '../api/im';

type ProviderKey = 'feishu' | 'dingtalk' | 'wecom';

function providerStatus(config: IMConfig, key: ProviderKey) {
  if (key === 'feishu') return config.feishu?.enabled === true;
  if (key === 'dingtalk') return config.dingtalk?.enabled === true;
  return config.wecom?.enabled === true;
}

export function IMSettingsPage() {
  const { t } = useTranslation();
  const [config, setConfig] = useState<IMConfig>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState('');

  const load = () => {
    setLoading(true);
    loadIMConfig().then(setConfig).catch(err => setMessage(err instanceof Error ? err.message : String(err))).finally(() => setLoading(false));
  };

  useEffect(load, []);

  const handleSave = async () => {
    setSaving(true);
    setMessage('');
    try {
      await saveIMConfig(config);
      setMessage(t('im.saved'));
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const setFeishu = (patch: Partial<NonNullable<IMConfig['feishu']>>) => setConfig(current => ({ ...current, feishu: { ...(current.feishu || {}), app_id: current.feishu?.app_id || '', app_secret: current.feishu?.app_secret || '', ...patch } }));
  const setDingtalk = (patch: Partial<NonNullable<IMConfig['dingtalk']>>) => setConfig(current => ({ ...current, dingtalk: { ...(current.dingtalk || {}), app_key: current.dingtalk?.app_key || '', app_secret: current.dingtalk?.app_secret || '', ...patch } }));
  const setWecom = (patch: Partial<NonNullable<IMConfig['wecom']>>) => setConfig(current => ({ ...current, wecom: { ...(current.wecom || {}), corp_id: current.wecom?.corp_id || '', agent_id: current.wecom?.agent_id || '', secret: current.wecom?.secret || '', ...patch } }));

  if (loading) return <div className="hint">{t('common.loading')}</div>;

  return (
    <div className="center-page-stack im-settings-page">
      {message ? <div className="hint">{message}</div> : null}
      <div className="metric-grid im-status-grid">
        {(['feishu', 'dingtalk', 'wecom'] as ProviderKey[]).map(key => <div key={key} className="metric-card card"><label>{t(`im.${key}`)}</label><strong>{providerStatus(config, key) ? t('im.enabled') : t('im.disabled')}</strong><span>{t(`im.${key}Callback`)}</span></div>)}
      </div>

      <SectionCard title={t('im.feishu')} desc={t('im.feishuDesc')}>
        <GatewayToggle enabled={config.feishu?.enabled === true} onChange={enabled => setFeishu({ enabled })} label={t('im.enableFeishu')} />
        <div className="gateway-form">
          <Field label="App ID" value={config.feishu?.app_id || ''} onChange={value => setFeishu({ app_id: value })} />
          <Field label="App Secret" value={config.feishu?.app_secret || ''} onChange={value => setFeishu({ app_secret: value })} password />
          <Field label="Verification Token" value={config.feishu?.verification_token || ''} onChange={value => setFeishu({ verification_token: value })} />
          <Field label="Encrypt Key" value={config.feishu?.encrypt_key || ''} onChange={value => setFeishu({ encrypt_key: value })} />
        </div>
      </SectionCard>

      <SectionCard title={t('im.dingtalk')} desc={t('im.dingtalkDesc')}>
        <GatewayToggle enabled={config.dingtalk?.enabled === true} onChange={enabled => setDingtalk({ enabled })} label={t('im.enableDingtalk')} />
        <div className="gateway-form">
          <Field label="App Key" value={config.dingtalk?.app_key || ''} onChange={value => setDingtalk({ app_key: value })} />
          <Field label="App Secret" value={config.dingtalk?.app_secret || ''} onChange={value => setDingtalk({ app_secret: value })} password />
          <Field label="Robot Code" value={config.dingtalk?.robot_code || ''} onChange={value => setDingtalk({ robot_code: value })} />
        </div>
      </SectionCard>

      <SectionCard title={t('im.wecom')} desc={t('im.wecomDesc')}>
        <GatewayToggle enabled={config.wecom?.enabled === true} onChange={enabled => setWecom({ enabled })} label={t('im.enableWecom')} />
        <div className="gateway-form">
          <Field label="Corp ID" value={config.wecom?.corp_id || ''} onChange={value => setWecom({ corp_id: value })} />
          <Field label="Agent ID" value={String(config.wecom?.agent_id || '')} onChange={value => setWecom({ agent_id: value })} />
          <Field label="Secret" value={config.wecom?.secret || config.wecom?.corp_secret || ''} onChange={value => setWecom({ secret: value, corp_secret: value })} password />
          <Field label="Token" value={config.wecom?.token || ''} onChange={value => setWecom({ token: value })} />
          <Field label="AES Key" value={config.wecom?.aes_key || ''} onChange={value => setWecom({ aes_key: value })} />
        </div>
      </SectionCard>

      <div className="actions"><button className="btn-primary" disabled={saving} onClick={handleSave}>{saving ? t('common.loading') : t('common.save')}</button><button className="btn-ghost" onClick={load}>{t('common.refresh')}</button></div>
    </div>
  );
}

function GatewayToggle({ enabled, onChange, label }: { enabled: boolean; onChange: (enabled: boolean) => void; label: string }) {
  return <label className="gateway-toggle"><input type="checkbox" checked={enabled} onChange={event => onChange(event.target.checked)} /><span>{label}</span><strong>{enabled ? 'ON' : 'OFF'}</strong></label>;
}

function Field({ label, value, onChange, password }: { label: string; value: string; onChange: (value: string) => void; password?: boolean }) {
  return <label><span>{label}</span><input type={password ? 'password' : 'text'} value={value} onChange={event => onChange(event.target.value)} /></label>;
}
