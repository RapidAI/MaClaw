import { useEffect, useState } from 'react';

type FeishuConfig = {
  app_id: string;
  app_secret: string;
  verification_token: string;
  encrypt_key: string;
  enabled: boolean;
};

type DingTalkConfig = {
  app_key: string;
  app_secret: string;
  robot_code: string;
  enabled: boolean;
};

type WeComConfig = {
  corp_id: string;
  corp_secret: string;
  agent_id: number;
  token: string;
  aes_key: string;
  enabled: boolean;
};

type IMConfig = {
  feishu?: FeishuConfig;
  dingtalk?: DingTalkConfig;
  wecom?: WeComConfig;
};

type IMTab = 'feishu' | 'dingtalk' | 'wecom';

const emptyFeishu: FeishuConfig = { app_id: '', app_secret: '', verification_token: '', encrypt_key: '', enabled: false };
const emptyDingTalk: DingTalkConfig = { app_key: '', app_secret: '', robot_code: '', enabled: false };
const emptyWeCom: WeComConfig = { corp_id: '', corp_secret: '', agent_id: 0, token: '', aes_key: '', enabled: false };

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

async function fetchJSON<T>(url: string): Promise<T | null> {
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    return resp.json();
  } catch { return null; }
}

async function putJSON(url: string, body: unknown): Promise<boolean> {
  try {
    const resp = await fetch(url, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    return resp.ok;
  } catch { return false; }
}

export function IMSettingsPage() {
  const [tab, setTab] = useState<IMTab>('feishu');
  const [config, setConfig] = useState<IMConfig>({});
  const [saving, setSaving] = useState(false);
  const [msg, setMsg] = useState('');

  useEffect(() => {
    // Try HTTP API first, fallback to Wails
    fetchJSON<IMConfig>('/admin/im-config').then(d => {
      if (d) { setConfig(d); return; }
      if (!hasWails()) return;
      (window as any).go.main.App.LoadIMConfig()
        .then((c: IMConfig) => { if (c) setConfig(c); })
        .catch(() => {});
    });
  }, []);

  const feishu = config.feishu || { ...emptyFeishu };
  const dingtalk = config.dingtalk || { ...emptyDingTalk };
  const wecom = config.wecom || { ...emptyWeCom };

  const updateFeishu = (patch: Partial<FeishuConfig>) => {
    setConfig(prev => ({ ...prev, feishu: { ...emptyFeishu, ...prev.feishu, ...patch } }));
  };
  const updateDingTalk = (patch: Partial<DingTalkConfig>) => {
    setConfig(prev => ({ ...prev, dingtalk: { ...emptyDingTalk, ...prev.dingtalk, ...patch } }));
  };
  const updateWeCom = (patch: Partial<WeComConfig>) => {
    setConfig(prev => ({ ...prev, wecom: { ...emptyWeCom, ...prev.wecom, ...patch } }));
  };

  const save = async () => {
    setSaving(true);
    setMsg('');
    let ok = await putJSON('/admin/im-config', config);
    if (!ok && hasWails()) {
      try {
        await (window as any).go.main.App.SaveIMConfig(config);
        ok = true;
      } catch { /* ignore */ }
    }
    setMsg(ok ? '保存成功' : '保存失败');
    setSaving(false);
    setTimeout(() => setMsg(''), 3000);
  };

  const tabs: { id: IMTab; label: string; icon: string }[] = [
    { id: 'feishu', label: '飞书', icon: '🐦' },
    { id: 'dingtalk', label: '钉钉', icon: '💬' },
    { id: 'wecom', label: '企业微信', icon: '💼' },
  ];

  return (
    <div style={{ display: 'flex', gap: 0, minHeight: 400 }}>
      {/* Left vertical tabs */}
      <div style={vtabBarStyle}>
        {tabs.map(t => (
          <button
            key={t.id}
            onClick={() => setTab(t.id)}
            style={tab === t.id ? { ...vtabStyle, ...vtabActiveStyle } : vtabStyle}
          >
            <span style={{ fontSize: 18 }}>{t.icon}</span>
            <span>{t.label}</span>
          </button>
        ))}
      </div>

      {/* Right content */}
      <div style={{ flex: 1, padding: '16px 24px' }}>
        {tab === 'feishu' && (
          <div>
            <h3 style={headingStyle}>飞书机器人配置</h3>
            <p style={descStyle}>配置飞书自建应用的凭证，用于接收和发送消息。Webhook 地址：<code>/webhook/feishu</code></p>
            <label style={labelStyle}>
              <input type="checkbox" checked={feishu.enabled} onChange={e => updateFeishu({ enabled: e.target.checked })} />
              启用飞书网关
            </label>
            <Field label="App ID" value={feishu.app_id} onChange={v => updateFeishu({ app_id: v })} placeholder="cli_xxxx" />
            <Field label="App Secret" value={feishu.app_secret} onChange={v => updateFeishu({ app_secret: v })} placeholder="飞书应用密钥" type="password" />
            <Field label="Verification Token" value={feishu.verification_token} onChange={v => updateFeishu({ verification_token: v })} placeholder="事件回调验证 Token（可选）" />
            <Field label="Encrypt Key" value={feishu.encrypt_key} onChange={v => updateFeishu({ encrypt_key: v })} placeholder="事件加密密钥（可选）" />
          </div>
        )}

        {tab === 'dingtalk' && (
          <div>
            <h3 style={headingStyle}>钉钉机器人配置</h3>
            <p style={descStyle}>配置钉钉企业内部机器人凭证。Webhook 地址：<code>/webhook/dingtalk</code></p>
            <label style={labelStyle}>
              <input type="checkbox" checked={dingtalk.enabled} onChange={e => updateDingTalk({ enabled: e.target.checked })} />
              启用钉钉网关
            </label>
            <Field label="App Key" value={dingtalk.app_key} onChange={v => updateDingTalk({ app_key: v })} placeholder="钉钉应用 AppKey" />
            <Field label="App Secret" value={dingtalk.app_secret} onChange={v => updateDingTalk({ app_secret: v })} placeholder="钉钉应用 AppSecret" type="password" />
            <Field label="Robot Code" value={dingtalk.robot_code} onChange={v => updateDingTalk({ robot_code: v })} placeholder="机器人编码（用于主动发消息）" />
          </div>
        )}

        {tab === 'wecom' && (
          <div>
            <h3 style={headingStyle}>企业微信配置</h3>
            <p style={descStyle}>配置企业微信自建应用凭证。Webhook 地址：<code>/webhook/wecom</code></p>
            <label style={labelStyle}>
              <input type="checkbox" checked={wecom.enabled} onChange={e => updateWeCom({ enabled: e.target.checked })} />
              启用企业微信网关
            </label>
            <Field label="Corp ID" value={wecom.corp_id} onChange={v => updateWeCom({ corp_id: v })} placeholder="企业 ID" />
            <Field label="Corp Secret" value={wecom.corp_secret} onChange={v => updateWeCom({ corp_secret: v })} placeholder="应用密钥" type="password" />
            <Field label="Agent ID" value={String(wecom.agent_id || '')} onChange={v => updateWeCom({ agent_id: parseInt(v) || 0 })} placeholder="应用 AgentId" />
            <Field label="Token" value={wecom.token} onChange={v => updateWeCom({ token: v })} placeholder="回调 Token（可选）" />
            <Field label="AES Key" value={wecom.aes_key} onChange={v => updateWeCom({ aes_key: v })} placeholder="回调 EncodingAESKey（可选）" />
          </div>
        )}

        <div style={{ marginTop: 24, display: 'flex', alignItems: 'center', gap: 12 }}>
          <button onClick={save} disabled={saving} style={saveBtnStyle}>
            {saving ? '保存中...' : '💾 保存配置'}
          </button>
          {msg && <span style={{ color: msg === '保存成功' ? '#2a7' : '#c33' }}>{msg}</span>}
        </div>
      </div>
    </div>
  );
}

function Field({ label, value, onChange, placeholder, type = 'text' }: {
  label: string; value: string; onChange: (v: string) => void; placeholder?: string; type?: string;
}) {
  return (
    <div style={{ marginBottom: 12 }}>
      <label style={{ display: 'block', fontSize: 13, color: '#555', marginBottom: 4 }}>{label}</label>
      <input
        type={type}
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={placeholder}
        style={inputStyle}
      />
    </div>
  );
}

const vtabBarStyle: React.CSSProperties = {
  display: 'flex', flexDirection: 'column', width: 120,
  borderRight: '1px solid #e5e5e5', background: '#fafafa', padding: '8px 0',
};
const vtabStyle: React.CSSProperties = {
  display: 'flex', flexDirection: 'column', alignItems: 'center', gap: 4,
  padding: '14px 8px', border: 'none', background: 'transparent',
  cursor: 'pointer', fontSize: 13, color: '#666', borderRight: '3px solid transparent',
};
const vtabActiveStyle: React.CSSProperties = {
  background: '#fff', color: '#1a1a1a', fontWeight: 600,
  borderRight: '3px solid #4a90d9',
};
const headingStyle: React.CSSProperties = { margin: '0 0 8px', fontSize: 16, fontWeight: 600 };
const descStyle: React.CSSProperties = { margin: '0 0 16px', fontSize: 13, color: '#888', lineHeight: 1.5 };
const labelStyle: React.CSSProperties = { display: 'flex', alignItems: 'center', gap: 6, marginBottom: 16, fontSize: 14, cursor: 'pointer' };
const inputStyle: React.CSSProperties = {
  width: '100%', maxWidth: 400, padding: '7px 10px', border: '1px solid #d0d0d0',
  borderRadius: 4, fontSize: 13, boxSizing: 'border-box',
};
const saveBtnStyle: React.CSSProperties = {
  padding: '8px 20px', borderRadius: 4, border: '1px solid #ccc',
  background: '#fff', cursor: 'pointer', fontSize: 14,
};
