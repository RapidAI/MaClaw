import { useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';

type TenantMode = 'dedicated' | 'multi_tenant';
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };

async function fetchJSON<T>(url: string): Promise<T | null> {
  try { const resp = await fetch(url); if (!resp.ok) return null; return resp.json(); } catch { return null; }
}

async function putJSON(url: string, body: unknown): Promise<{ ok: boolean; msg?: string }> {
  try {
    const resp = await fetch(url, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) return { ok: false, msg: data?.error?.message || '操作失败' };
    return { ok: true };
  } catch { return { ok: false, msg: '网络错误' }; }
}

export function AccountSettingsPage() {
  const [email, setEmail] = useState('');
  const [emailMsg, setEmailMsg] = useState<Message | null>(null);
  const [oldPwd, setOldPwd] = useState('');
  const [newPwd, setNewPwd] = useState('');
  const [confirmPwd, setConfirmPwd] = useState('');
  const [pwdMsg, setPwdMsg] = useState<Message | null>(null);
  const [username, setUsername] = useState('');
  const [tenantMode, setTenantMode] = useState<TenantMode>('dedicated');
  const [tenantModeMsg, setTenantModeMsg] = useState<Message | null>(null);

  useEffect(() => {
    fetchJSON<{ mode: TenantMode }>('/admin/system/tenant-mode').then(d => { if (d?.mode) setTenantMode(d.mode); });
    fetchJSON<{ username: string; email: string }>('/admin/profile').then(d => { if (d) { setEmail(d.email || ''); setUsername(d.username || 'admin'); } });
  }, []);

  const saveEmail = async () => {
    const r = await putJSON('/admin/profile', { email });
    setEmailMsg(r.ok ? { kind: 'ok', text: '邮箱已更新。' } : { kind: 'danger', text: '更新失败：' + r.msg });
  };

  const saveTenantMode = async () => {
    const r = await putJSON('/admin/system/tenant-mode', { mode: tenantMode });
    setTenantModeMsg(r.ok ? { kind: 'ok', text: '租户模式已保存。' } : { kind: 'danger', text: '保存失败：' + r.msg });
  };

  const savePassword = async () => {
    if (newPwd !== confirmPwd) { setPwdMsg({ kind: 'warn', text: '两次输入的新密码不一致。' }); return; }
    if (newPwd.length < 4) { setPwdMsg({ kind: 'warn', text: '新密码至少 4 个字符。' }); return; }
    const r = await putJSON('/admin/password', { old_password: oldPwd, new_password: newPwd });
    if (r.ok) { setPwdMsg({ kind: 'ok', text: '密码已更新。' }); setOldPwd(''); setNewPwd(''); setConfirmPwd(''); }
    else setPwdMsg({ kind: 'danger', text: '更新失败：' + r.msg });
  };

  return (
    <div className="center-page-stack">
      <SectionCard title="管理员资料" desc="修改管理员邮箱，用于 iWorkerCloud 注册、授权审核和系统通知。">
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>用户名</span><input type="text" value={username} disabled /></label>
          <label className="cloud-field"><span>管理员邮箱</span><input type="email" value={email} onChange={e => setEmail(e.target.value)} placeholder="admin@example.com" /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" onClick={saveEmail}>保存邮箱</button></div>
        {emailMsg && <p className={'cloud-message ' + emailMsg.kind}>{emailMsg.text}</p>}
      </SectionCard>

      <SectionCard title="租户模式" desc="设置当前 iWorkerCenter 是企业专用中心，还是允许托管多个公司租户。Cloud 不参与企业业务，租户业务仍由本 Center 本地处理。">
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>运行模式</span><select value={tenantMode} onChange={e => setTenantMode(e.target.value as TenantMode)}><option value="dedicated">专用模式</option><option value="multi_tenant">多租户模式</option></select></label>
          <div className="cloud-continuity-card"><strong>{tenantMode === 'dedicated' ? '专用中心' : '多租户中心'}</strong><p>{tenantMode === 'dedicated' ? '适合单一企业部署，配置更简单。' : '适合服务多个单位/部门，但业务数据仍在 Center 本地隔离管理。'}</p></div>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" onClick={saveTenantMode}>保存租户模式</button></div>
        {tenantModeMsg && <p className={'cloud-message ' + tenantModeMsg.kind}>{tenantModeMsg.text}</p>}
      </SectionCard>

      <SectionCard title="修改密码" desc="修改管理员登录密码。为了安全，保存后请使用新密码重新验证登录。">
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>当前密码</span><input type="password" value={oldPwd} onChange={e => setOldPwd(e.target.value)} /></label>
          <label className="cloud-field"><span>新密码</span><input type="password" value={newPwd} onChange={e => setNewPwd(e.target.value)} placeholder="至少 4 个字符" /></label>
          <label className="cloud-field"><span>确认新密码</span><input type="password" value={confirmPwd} onChange={e => setConfirmPwd(e.target.value)} /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" onClick={savePassword}>修改密码</button></div>
        {pwdMsg && <p className={'cloud-message ' + pwdMsg.kind}>{pwdMsg.text}</p>}
      </SectionCard>
    </div>
  );
}
