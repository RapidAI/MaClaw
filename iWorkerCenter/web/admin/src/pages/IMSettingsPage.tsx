import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { loadIMConfig, saveIMConfig, type IMConfig } from '../api/im';

function Guide({ steps, link, linkText }: { steps: string[]; link: string; linkText: string }) {
  return (
    <div style={{ background: '#f0f6ff', border: '1px dashed #a8c8f0', borderRadius: 8, padding: '12px 14px', marginBottom: 14, fontSize: 13, color: '#2c5f96', lineHeight: 1.7 }}>
      <strong style={{ display: 'block', marginBottom: 4 }}>📖 如何获取参数？</strong>
      <ol style={{ margin: '4px 0 8px', paddingLeft: 20 }}>
        {steps.map((s, i) => <li key={i}>{s}</li>)}
      </ol>
      <a href={link} target="_blank" rel="noopener noreferrer" style={{ color: '#2563eb', textDecoration: 'underline' }}>{linkText}</a>
    </div>
  );
}

function EnableToggle({ label, checked, onChange }: { label: string; checked: boolean; onChange: (v: boolean) => void }) {
  return (
    <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 12, padding: '8px 12px', borderRadius: 8, background: checked ? '#e8f5e9' : '#f5f5f5', border: `1px solid ${checked ? '#81c784' : '#ddd'}` }}>
      <input type="checkbox" checked={checked} onChange={e => onChange(e.target.checked)} style={{ width: 18, height: 18, cursor: 'pointer' }} />
      <span style={{ fontWeight: 700, fontSize: 14, color: checked ? '#2e7d32' : '#888' }}>{label}</span>
      <span style={{ fontSize: 12, color: checked ? '#4caf50' : '#aaa', marginLeft: 4 }}>{checked ? '已启用' : '未启用'}</span>
    </div>
  );
}

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

  const feishuEnabled = config.feishu?.enabled ?? false;
  const dingtalkEnabled = config.dingtalk?.enabled ?? false;
  const wecomEnabled = config.wecom?.enabled ?? false;

  return (
    <div className="center-page-stack">
      {/* Feishu */}
      <SectionCard title={t('im.feishu')} desc={t('im.feishuDesc')}>
        <EnableToggle label="启用飞书网关" checked={feishuEnabled} onChange={v => setConfig({ ...config, feishu: { ...config.feishu!, app_id: config.feishu?.app_id || '', app_secret: config.feishu?.app_secret || '', enabled: v } })} />
        <Guide
          steps={[
            '登录飞书开放平台 open.feishu.cn，创建「企业自建应用」',
            '在「凭证与基础信息」页面获取 App ID 和 App Secret',
            '在「事件订阅」页面获取 Verification Token 和 Encrypt Key',
            '事件请求地址填写: https://你的域名:9377/feishu/event',
            '开启「机器人」能力，并在权限管理中添加消息相关权限',
          ]}
          link="https://open.feishu.cn/document/home/introduction-to-custom-app-development/self-built-application-development-process"
          linkText="📘 飞书自建应用开发文档"
        />
        <div style={{ ...gridStyle, opacity: feishuEnabled ? 1 : 0.5, pointerEvents: feishuEnabled ? 'auto' : 'none' }}>
          <Field label="App ID" value={config.feishu?.app_id || ''} onChange={v => setConfig({ ...config, feishu: { ...config.feishu!, app_id: v, app_secret: config.feishu?.app_secret || '' } })} />
          <Field label="App Secret" value={config.feishu?.app_secret || ''} onChange={v => setConfig({ ...config, feishu: { ...config.feishu!, app_secret: v, app_id: config.feishu?.app_id || '' } })} password />
          <Field label="Verification Token" value={config.feishu?.verification_token || ''} onChange={v => setConfig({ ...config, feishu: { ...config.feishu!, verification_token: v, app_id: config.feishu?.app_id || '', app_secret: config.feishu?.app_secret || '' } })} />
          <Field label="Encrypt Key" value={config.feishu?.encrypt_key || ''} onChange={v => setConfig({ ...config, feishu: { ...config.feishu!, encrypt_key: v, app_id: config.feishu?.app_id || '', app_secret: config.feishu?.app_secret || '' } })} />
        </div>
      </SectionCard>

      {/* DingTalk */}
      <SectionCard title={t('im.dingtalk')} desc={t('im.dingtalkDesc')}>
        <EnableToggle label="启用钉钉网关" checked={dingtalkEnabled} onChange={v => setConfig({ ...config, dingtalk: { ...config.dingtalk!, app_key: config.dingtalk?.app_key || '', app_secret: config.dingtalk?.app_secret || '', enabled: v } })} />
        <Guide
          steps={[
            '登录钉钉开放平台 open.dingtalk.com，创建「企业内部应用」',
            '在「应用信息」页面获取 App Key（即 Client ID）和 App Secret（即 Client Secret）',
            '在「机器人与消息推送」中开启机器人能力',
            '消息接收地址填写: https://你的域名:9377/dingtalk/callback',
          ]}
          link="https://open.dingtalk.com/document/orgapp/create-an-enterprise-internal-application"
          linkText="📘 钉钉企业内部应用开发文档"
        />
        <div style={{ ...gridStyle, opacity: dingtalkEnabled ? 1 : 0.5, pointerEvents: dingtalkEnabled ? 'auto' : 'none' }}>
          <Field label="App Key" value={config.dingtalk?.app_key || ''} onChange={v => setConfig({ ...config, dingtalk: { ...config.dingtalk!, app_key: v, app_secret: config.dingtalk?.app_secret || '' } })} />
          <Field label="App Secret" value={config.dingtalk?.app_secret || ''} onChange={v => setConfig({ ...config, dingtalk: { ...config.dingtalk!, app_secret: v, app_key: config.dingtalk?.app_key || '' } })} password />
        </div>
      </SectionCard>

      {/* WeCom */}
      <SectionCard title={t('im.wecom')} desc={t('im.wecomDesc')}>
        <EnableToggle label="启用企业微信网关" checked={wecomEnabled} onChange={v => setConfig({ ...config, wecom: { ...config.wecom!, corp_id: config.wecom?.corp_id || '', agent_id: String(config.wecom?.agent_id || ''), secret: config.wecom?.secret || '', enabled: v } })} />
        <Guide
          steps={[
            '登录企业微信管理后台 work.weixin.qq.com',
            '在「我的企业」页面获取 Corp ID（企业 ID）',
            '在「应用管理」中创建自建应用，获取 Agent ID 和 Secret',
            '在应用的「接收消息」设置中配置回调地址: https://你的域名:9377/wecom/callback',
            '需要在「可信域名」中添加你的服务器域名',
          ]}
          link="https://developer.work.weixin.qq.com/document/path/90556"
          linkText="📘 企业微信自建应用开发文档"
        />
        <div style={{ ...gridStyle, opacity: wecomEnabled ? 1 : 0.5, pointerEvents: wecomEnabled ? 'auto' : 'none' }}>
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
