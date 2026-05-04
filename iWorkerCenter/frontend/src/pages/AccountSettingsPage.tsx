import { useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { useI18n } from '../i18n';

type TenantMode = 'dedicated' | 'multi_tenant';
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };

async function fetchJSON<T>(url: string): Promise<T | null> {
  try { const resp = await fetch(url); if (!resp.ok) return null; return resp.json(); } catch { return null; }
}

async function putJSON(url: string, body: unknown): Promise<{ ok: boolean; msg?: string }> {
  try {
    const resp = await fetch(url, { method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) return { ok: false, msg: data?.error?.message || 'Operation failed' };
    return { ok: true };
  } catch { return { ok: false, msg: 'Network error' }; }
}

export function AccountSettingsPage() {
  const { t } = useI18n();
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
    setEmailMsg(r.ok ? { kind: 'ok', text: t('邮箱已更新。', 'Email updated.') } : { kind: 'danger', text: t('更新失败：', 'Update failed: ') + r.msg });
  };

  const saveTenantMode = async () => {
    const r = await putJSON('/admin/system/tenant-mode', { mode: tenantMode });
    setTenantModeMsg(r.ok ? { kind: 'ok', text: t('租户模式已保存。', 'Tenant mode saved.') } : { kind: 'danger', text: t('保存失败：', 'Save failed: ') + r.msg });
  };

  const savePassword = async () => {
    if (newPwd !== confirmPwd) { setPwdMsg({ kind: 'warn', text: t('两次输入的新密码不一致。', 'The two new passwords do not match.') }); return; }
    if (newPwd.length < 4) { setPwdMsg({ kind: 'warn', text: t('新密码至少 4 个字符。', 'The new password must be at least 4 characters.') }); return; }
    const r = await putJSON('/admin/password', { old_password: oldPwd, new_password: newPwd });
    if (r.ok) { setPwdMsg({ kind: 'ok', text: t('密码已更新。', 'Password updated.') }); setOldPwd(''); setNewPwd(''); setConfirmPwd(''); }
    else setPwdMsg({ kind: 'danger', text: t('更新失败：', 'Update failed: ') + r.msg });
  };

  return (
    <div className="center-page-stack">
      <SectionCard title={t('管理员资料', 'Administrator Profile')} desc={t('维护管理员邮箱，用于 iWorkerCloud 注册、授权审核和系统通知。', 'Maintain the administrator email for iWorkerCloud registration, license review, and system notifications.')}>
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>{t('用户名', 'Username')}</span><input type="text" value={username} disabled /></label>
          <label className="cloud-field"><span>{t('管理员邮箱', 'Administrator email')}</span><input type="email" value={email} onChange={e => setEmail(e.target.value)} placeholder="admin@example.com" /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" onClick={saveEmail}>{t('保存邮箱', 'Save email')}</button></div>
        {emailMsg && <p className={'cloud-message ' + emailMsg.kind}>{emailMsg.text}</p>}
      </SectionCard>

      <SectionCard title={t('租户模式', 'Tenant Mode')} desc={t('设置当前 iWorkerCenter 是企业专用中心，还是允许托管多个公司租户。Cloud 不参与企业业务，租户业务仍由本 Center 本地隔离处理。', 'Choose whether this iWorkerCenter serves one company or hosts multiple tenants. Cloud does not participate in enterprise business data; tenant work remains locally isolated in this Center.')}>
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>{t('运行模式', 'Operating mode')}</span><select value={tenantMode} onChange={e => setTenantMode(e.target.value as TenantMode)}><option value="dedicated">{t('专用模式', 'Dedicated')}</option><option value="multi_tenant">{t('多租户模式', 'Multi-tenant')}</option></select></label>
          <div className="cloud-continuity-card"><strong>{tenantMode === 'dedicated' ? t('专用中心', 'Dedicated center') : t('多租户中心', 'Multi-tenant center')}</strong><p>{tenantMode === 'dedicated' ? t('适合单一企业部署，配置更简单。', 'Best for one company with simpler operations.') : t('适合服务多个单位或部门，但业务数据仍在 Center 本地隔离管理。', 'Best for multiple companies or departments while business data remains locally isolated.')}</p></div>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" onClick={saveTenantMode}>{t('保存租户模式', 'Save tenant mode')}</button></div>
        {tenantModeMsg && <p className={'cloud-message ' + tenantModeMsg.kind}>{tenantModeMsg.text}</p>}
      </SectionCard>

      <SectionCard title={t('修改密码', 'Change Password')} desc={t('修改管理员登录密码。为了安全，保存后请使用新密码重新验证登录。', 'Change the administrator login password. For safety, sign in again with the new password after saving.')}>
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>{t('当前密码', 'Current password')}</span><input type="password" value={oldPwd} onChange={e => setOldPwd(e.target.value)} /></label>
          <label className="cloud-field"><span>{t('新密码', 'New password')}</span><input type="password" value={newPwd} onChange={e => setNewPwd(e.target.value)} placeholder={t('至少 4 个字符', 'At least 4 characters')} /></label>
          <label className="cloud-field"><span>{t('确认新密码', 'Confirm new password')}</span><input type="password" value={confirmPwd} onChange={e => setConfirmPwd(e.target.value)} /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" onClick={savePassword}>{t('修改密码', 'Change password')}</button></div>
        {pwdMsg && <p className={'cloud-message ' + pwdMsg.kind}>{pwdMsg.text}</p>}
      </SectionCard>
    </div>
  );
}
