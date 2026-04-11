import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { loadIMConfig, saveIMConfig, type IMConfig } from '../api/im';

export function IMSettingsPage() {
  const { t } = useTranslation();
  const [config, setConfig] = useState<IMConfig>({});
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState('');

  useEffect(() => {
    loadIMConfig()
      .then(setConfig)
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const handleSave = async () => {
    setSaving(true);
    setMsg('');
    try {
      await saveIMConfig(config);
      setMsg(t('common.success'));
    } catch (err: any) {
      setMsg(err.message || t('common.error'));
    }
    setSaving(false);
  };

  if (loading) return <div style={{ padding: 24, color: '#888' }}>{t('common.loading')}</div>;

  return (
    <div className="center-page-stack">
      {/* Feishu */}
      <SectionCard title={t('im.feishu')} desc={t('im.feishuDesc')}>
        <div style={gridStyle}>
          <Field label="App ID" value={config.feishu?.app_id || ''} onChange={v => setConfig({ ...config, feishu: { ...config.feishu!, app_id: v, app_secret: config.feishu?.app_secret || '' } })} />
          <Field label="App Secret" value={config.feishu?.app_secret || ''} onChange={v => setConfig({ ...config, feishu: { ...config.feishu!, app_secret: v, app_id: config.feishu?.app_id || '' } })} password />
          <Field label="Verification Token" value={config.feishu?.verification_token || ''} onChange={v => setConfig({ ...config, feishu: { ...config.feishu!, verification_token: v, app_id: config.feishu?.app_id || '', app_secret: config.feishu?.app_secret || '' } })} />
          <Field label="Encrypt Key" value={config.feishu?.encrypt_key || ''} onChange={v => setConfig({ ...config, feishu: { ...config.feishu!, encrypt_key: v, app_id: config.feishu?.app_id || '', app_secret: config.feishu?.app_secret || '' } })} />
        </div>
      </SectionCard>

      {/* DingTalk */}
      <SectionCard title={t('im.dingtalk')} desc={t('im.dingtalkDesc')}>
        <div style={gridStyle}>
          <Field label="App Key" value={config.dingtalk?.app_key || ''} onChange={v => setConfig({ ...config, dingtalk: { ...config.dingtalk!, app_key: v, app_secret: config.dingtalk?.app_secret || '' } })} />
          <Field label="App Secret" value={config.dingtalk?.app_secret || ''} onChange={v => setConfig({ ...config, dingtalk: { ...config.dingtalk!, app_secret: v, app_key: config.dingtalk?.app_key || '' } })} password />
        </div>
      </SectionCard>

      {/* WeCom */}
      <SectionCard title={t('im.wecom')} desc={t('im.wecomDesc')}>
        <div style={gridStyle}>
          <Field label="Corp ID" value={config.wecom?.corp_id || ''} onChange={v => setConfig({ ...config, wecom: { ...config.wecom!, corp_id: v, agent_id: String(config.wecom?.agent_id || ''), secret: config.wecom?.secret || '' } })} />
          <Field label="Agent ID" value={String(config.wecom?.agent_id || '')} onChange={v => setConfig({ ...config, wecom: { ...config.wecom!, agent_id: v, corp_id: config.wecom?.corp_id || '', secret: config.wecom?.secret || '' } })} />
          <Field label="Secret" value={config.wecom?.secret || ''} onChange={v => setConfig({ ...config, wecom: { ...config.wecom!, secret: v, corp_id: config.wecom?.corp_id || '', agent_id: String(config.wecom?.agent_id || '') } })} password />
        </div>
      </SectionCard>

      <div style={{ display: 'flex', gap: 12, alignItems: 'center' }}>
        <button onClick={handleSave} disabled={saving} style={btnStyle}>
          {saving ? '...' : t('common.save')}
        </button>
        {msg && <span style={{ fontSize: 13 }}>{msg}</span>}
      </div>
    </div>
  );
}

function Field({ label, value, onChange, password }: { label: string; value: string; onChange: (v: string) => void; password?: boolean }) {
  return (
    <div>
      <label style={{ display: 'block', fontSize: 12, fontWeight: 700, color: '#5f7692', marginBottom: 4 }}>{label}</label>
      <input
        type={password ? 'password' : 'text'}
        value={value}
        onChange={e => onChange(e.target.value)}
        style={{ width: '100%', padding: '8px 10px', border: '1px solid #d0d0d0', borderRadius: 4, fontSize: 14, boxSizing: 'border-box' as const }}
      />
    </div>
  );
}

const gridStyle: React.CSSProperties = { display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12, marginBottom: 8 };
const btnStyle: React.CSSProperties = { padding: '8px 24px', borderRadius: 8, border: 'none', background: '#4a90d9', color: '#fff', fontSize: 14, cursor: 'pointer', fontWeight: 600 };
