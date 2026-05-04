import { useState } from 'react';
import { useI18n } from '../i18n';

type SetupTenantPageProps = {
  onSetupComplete: (tenantID?: string) => void;
};

export function SetupTenantPage({ onSetupComplete }: SetupTenantPageProps) {
  const { t } = useI18n();
  const [companyName, setCompanyName] = useState('');
  const [legalPerson, setLegalPerson] = useState('');
  const [email, setEmail] = useState('');
  const [address, setAddress] = useState('');
  const [adminUsername, setAdminUsername] = useState('admin');
  const [adminPassword, setAdminPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (!companyName.trim()) { setError(t('请输入企业名称。', 'Enter the company name.')); return; }
    if (!email.trim()) { setError(t('请输入企业邮箱。', 'Enter the company email.')); return; }
    if (!adminUsername.trim()) { setError(t('请输入管理员用户名。', 'Enter the administrator username.')); return; }
    if (!adminPassword) { setError(t('请输入管理员密码。', 'Enter the administrator password.')); return; }
    if (adminPassword !== confirmPassword) { setError(t('两次密码输入不一致。', 'The two passwords do not match.')); return; }
    if (adminPassword.length < 4) { setError(t('密码至少 4 个字符。', 'Password must be at least 4 characters.')); return; }

    setLoading(true);
    try {
      const resp = await fetch('/auth/setup-tenant', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          company_name: companyName.trim(),
          legal_person: legalPerson.trim(),
          email: email.trim(),
          address: address.trim(),
          admin_username: adminUsername.trim(),
          admin_password: adminPassword,
        }),
      });
      const data = await resp.json();
      if (resp.ok && data.tenant_id) {
        onSetupComplete(data.tenant_id);
      } else {
        setError(data?.error?.message || data?.error || t('创建失败。', 'Create failed.'));
      }
    } catch {
      setError(t('网络错误。', 'Network error.'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="setup-shell">
      <div className="setup-card">
        <div className="mini">iWorkerCenter Bootstrap</div>
        <h2>{t('欢迎使用数字员工中心', 'Welcome to Digital Workforce Center')}</h2>
        <p>{t('首次使用时先创建本地单位/租户和管理员账号。创建完成后请用管理员账号登录；登录后系统会自动打开单位初始化向导。', 'Create the local company/tenant and administrator account first. After setup, sign in with the administrator account; the bootstrap wizard will open automatically.')}</p>
        <form onSubmit={handleSubmit}>
          <fieldset>
            <legend>{t('企业信息', 'Company Information')}</legend>
            <label>{t('企业名称 *', 'Company name *')}</label>
            <input type="text" value={companyName} onChange={e => setCompanyName(e.target.value)} placeholder={t('例如：XX 科技有限公司', 'Example: Acme Technology Ltd.')} autoFocus />
            <label>{t('法人代表', 'Legal representative')}</label>
            <input type="text" value={legalPerson} onChange={e => setLegalPerson(e.target.value)} placeholder={t('法人代表姓名', 'Legal representative name')} />
            <label>{t('企业邮箱 *', 'Company email *')}</label>
            <input type="email" value={email} onChange={e => setEmail(e.target.value)} placeholder="admin@example.com" />
            <label>{t('企业地址', 'Company address')}</label>
            <input type="text" value={address} onChange={e => setAddress(e.target.value)} placeholder={t('企业注册地址或办公地址', 'Registered or office address')} />
          </fieldset>
          <fieldset>
            <legend>{t('管理员账号', 'Administrator Account')}</legend>
            <label>{t('用户名 *', 'Username *')}</label>
            <input type="text" value={adminUsername} onChange={e => setAdminUsername(e.target.value)} />
            <label>{t('密码 *', 'Password *')}</label>
            <input type="password" value={adminPassword} onChange={e => setAdminPassword(e.target.value)} placeholder={t('至少 4 个字符', 'At least 4 characters')} />
            <label>{t('确认密码 *', 'Confirm password *')}</label>
            <input type="password" value={confirmPassword} onChange={e => setConfirmPassword(e.target.value)} placeholder={t('再次输入密码', 'Enter password again')} />
          </fieldset>
          {error && <p className="cloud-message danger">{error}</p>}
          <button type="submit" disabled={loading} className="cloud-primary setup-submit">{loading ? t('创建中...', 'Creating...') : t('创建企业并进入登录', 'Create company and continue to sign-in')}</button>
        </form>
      </div>
    </div>
  );
}
