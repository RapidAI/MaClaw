import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';

type FeishuConfig = { app_id: string; app_secret: string; verification_token: string; encrypt_key: string; enabled: boolean };
type DingTalkConfig = { app_key: string; app_secret: string; robot_code: string; enabled: boolean };
type WeComConfig = { corp_id: string; corp_secret: string; agent_id: number; token: string; aes_key: string; enabled: boolean };
type IMConfig = { feishu?: FeishuConfig; dingtalk?: DingTalkConfig; wecom?: WeComConfig };
type IMTab = 'feishu' | 'dingtalk' | 'wecom';
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };

const emptyFeishu: FeishuConfig = { app_id: '', app_secret: '', verification_token: '', encrypt_key: '', enabled: false };
const emptyDingTalk: DingTalkConfig = { app_key: '', app_secret: '', robot_code: '', enabled: false };
const emptyWeCom: WeComConfig = { corp_id: '', corp_secret: '', agent_id: 0, token: '', aes_key: '', enabled: false };
const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

async function fetchJSON<T>(url: string): Promise<T | null> {
  try { const resp = await fetch(url); if (!resp.ok) return null; return resp.json(); } catch { return null; }
}

async function putJSON(url: string, body: unknown): Promise<boolean> {
  try {
    const resp = await fetch(url, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    return resp.ok;
  } catch { return false; }
}

export function IMSettingsPage() {
  const [tab, setTab] = useState<IMTab>('feishu');
  const [config, setConfig] = useState<IMConfig>({});
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState<Message | null>(null);

  const load = async () => {
    const data = await fetchJSON<IMConfig>('/admin/im-config');
    if (data) { setConfig(data); return; }
    if (!hasWails()) return;
    try {
      const local = await (window as any).go.main.App.LoadIMConfig();
      if (local) setConfig(local);
    } catch { /* ignore */ }
  };

  useEffect(() => { void load(); }, []);

  const feishu = config.feishu || { ...emptyFeishu };
  const dingtalk = config.dingtalk || { ...emptyDingTalk };
  const wecom = config.wecom || { ...emptyWeCom };

  const enabledCount = useMemo(() => [feishu.enabled, dingtalk.enabled, wecom.enabled].filter(Boolean).length, [feishu.enabled, dingtalk.enabled, wecom.enabled]);

  const updateFeishu = (patch: Partial<FeishuConfig>) => setConfig(prev => ({ ...prev, feishu: { ...emptyFeishu, ...prev.feishu, ...patch } }));
  const updateDingTalk = (patch: Partial<DingTalkConfig>) => setConfig(prev => ({ ...prev, dingtalk: { ...emptyDingTalk, ...prev.dingtalk, ...patch } }));
  const updateWeCom = (patch: Partial<WeComConfig>) => setConfig(prev => ({ ...prev, wecom: { ...emptyWeCom, ...prev.wecom, ...patch } }));

  const save = async () => {
    setSaving(true);
    setMessage(null);
    let ok = await putJSON('/admin/im-config', config);
    if (!ok && hasWails()) {
      try { await (window as any).go.main.App.SaveIMConfig(config); ok = true; } catch { /* ignore */ }
    }
    setMessage(ok ? { kind: 'ok', text: 'IM 配置已保存。' } : { kind: 'danger', text: '保存 IM 配置失败。' });
    setSaving(false);
  };

  return (
    <div className="center-page-stack">
      <SectionCard title="IM 网关" desc="配置企业通讯入口，让 iWorker 能把人工确认、异常提醒和协作消息推送到人类员工常用工具。配置保存在本地 Center。">
        <div className="cloud-status-grid">
          <StatusTile label="已启用网关" value={String(enabledCount)} tone={enabledCount ? 'ok' : 'warn'} />
          <StatusTile label="回调入口" value="3" />
          <StatusTile label="人工提醒" value="本地推送" tone="ok" />
        </div>
        <div className="auth-adapter-tabs">
          <button className={tab === 'feishu' ? 'cloud-primary' : 'btn-secondary'} onClick={() => setTab('feishu')}>飞书</button>
          <button className={tab === 'dingtalk' ? 'cloud-primary' : 'btn-secondary'} onClick={() => setTab('dingtalk')}>钉钉</button>
          <button className={tab === 'wecom' ? 'cloud-primary' : 'btn-secondary'} onClick={() => setTab('wecom')}>企业微信</button>
        </div>
        {message && <p className={'cloud-message ' + message.kind}>{message.text}</p>}
      </SectionCard>

      {tab === 'feishu' && (
        <SectionCard title="飞书机器人配置" desc="Webhook 地址：/webhook/feishu。用于接收事件回调和主动发送协作提醒。">
          <div className="cloud-form-grid">
            <EnabledField enabled={feishu.enabled} onChange={enabled => updateFeishu({ enabled })} />
            <Field label="App ID" value={feishu.app_id} onChange={v => updateFeishu({ app_id: v })} placeholder="cli_xxxx" />
            <Field label="App Secret" value={feishu.app_secret} onChange={v => updateFeishu({ app_secret: v })} type="password" />
            <Field label="Verification Token" value={feishu.verification_token} onChange={v => updateFeishu({ verification_token: v })} />
            <Field label="Encrypt Key" value={feishu.encrypt_key} onChange={v => updateFeishu({ encrypt_key: v })} />
          </div>
        </SectionCard>
      )}

      {tab === 'dingtalk' && (
        <SectionCard title="钉钉机器人配置" desc="Webhook 地址：/webhook/dingtalk。用于企业内部机器人消息和人工干预提醒。">
          <div className="cloud-form-grid">
            <EnabledField enabled={dingtalk.enabled} onChange={enabled => updateDingTalk({ enabled })} />
            <Field label="App Key" value={dingtalk.app_key} onChange={v => updateDingTalk({ app_key: v })} />
            <Field label="App Secret" value={dingtalk.app_secret} onChange={v => updateDingTalk({ app_secret: v })} type="password" />
            <Field label="Robot Code" value={dingtalk.robot_code} onChange={v => updateDingTalk({ robot_code: v })} />
          </div>
        </SectionCard>
      )}

      {tab === 'wecom' && (
        <SectionCard title="企业微信配置" desc="Webhook 地址：/webhook/wecom。用于企业微信自建应用消息和回调事件。">
          <div className="cloud-form-grid">
            <EnabledField enabled={wecom.enabled} onChange={enabled => updateWeCom({ enabled })} />
            <Field label="Corp ID" value={wecom.corp_id} onChange={v => updateWeCom({ corp_id: v })} />
            <Field label="Corp Secret" value={wecom.corp_secret} onChange={v => updateWeCom({ corp_secret: v })} type="password" />
            <Field label="Agent ID" value={String(wecom.agent_id || '')} onChange={v => updateWeCom({ agent_id: Number(v) || 0 })} />
            <Field label="Token" value={wecom.token} onChange={v => updateWeCom({ token: v })} />
            <Field label="AES Key" value={wecom.aes_key} onChange={v => updateWeCom({ aes_key: v })} />
          </div>
        </SectionCard>
      )}

      <SectionCard title="保存配置" desc="保存后 iWorker 的人工确认、任务异常和协作提醒可通过启用的网关发送给员工。">
        <div className="cloud-actions"><button className="cloud-primary" onClick={save} disabled={saving}>{saving ? '保存中...' : '保存 IM 配置'}</button></div>
      </SectionCard>
    </div>
  );
}

function Field({ label, value, onChange, placeholder, type = 'text' }: { label: string; value: string; onChange: (v: string) => void; placeholder?: string; type?: string }) {
  return <label className="cloud-field"><span>{label}</span><input type={type} value={value} onChange={e => onChange(e.target.value)} placeholder={placeholder} /></label>;
}

function EnabledField({ enabled, onChange }: { enabled: boolean; onChange: (enabled: boolean) => void }) {
  return <label className="cloud-field"><span>启用状态</span><select value={enabled ? 'yes' : 'no'} onChange={e => onChange(e.target.value === 'yes')}><option value="yes">启用</option><option value="no">停用</option></select></label>;
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}
