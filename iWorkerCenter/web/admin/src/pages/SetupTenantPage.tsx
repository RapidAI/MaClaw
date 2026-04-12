import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { setupTenant } from '../api/auth';

export function SetupTenantPage({ onSetupComplete }: { onSetupComplete: () => void }) {
  const { t } = useTranslation();
  const [companyName, setCompanyName] = useState('');
  const [email, setEmail] = useState('');
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (password !== confirmPassword) {
      setError(t('setup.passwordMismatch') || '两次输入的密码不一致');
      return;
    }
    if (!password || password.length < 4) {
      setError(t('setup.passwordTooShort') || '密码至少 4 位');
      return;
    }
    try {
      await setupTenant({ company_name: companyName, email, admin_username: username, admin_password: password });
      onSetupComplete();
    } catch (err: any) {
      setError(err.message || t('common.error'));
    }
  };

  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
      <div style={{ width: 400, padding: '32px 28px', background: '#fff', borderRadius: 8, boxShadow: '0 2px 12px rgba(0,0,0,0.08)' }}>
        <h2>{t('setup.title')}</h2>
        <p style={{ color: '#888', fontSize: 13 }}>{t('setup.description')}</p>
        <form onSubmit={handleSubmit}>
          <input value={companyName} onChange={e => setCompanyName(e.target.value)} placeholder={t('setup.companyName') || '企业名称'} style={inputStyle} required />
          <input type="email" value={email} onChange={e => setEmail(e.target.value)} placeholder={t('setup.email') || '管理员邮箱'} style={inputStyle} required />
          <input value={username} onChange={e => setUsername(e.target.value)} placeholder={t('login.username')} style={inputStyle} required />
          <input type="password" value={password} onChange={e => setPassword(e.target.value)} placeholder={t('setup.password') || '设置密码'} style={inputStyle} required />
          <input type="password" value={confirmPassword} onChange={e => setConfirmPassword(e.target.value)} placeholder={t('setup.confirmPassword') || '再次输入密码'} style={{
            ...inputStyle,
            borderColor: confirmPassword && password !== confirmPassword ? '#c33' : '#d0d0d0',
          }} required />
          {error && <p style={{ color: '#c33', fontSize: 13, margin: '4px 0 8px' }}>{error}</p>}
          <button type="submit" style={btnStyle}>{t('common.confirm')}</button>
        </form>
      </div>
    </div>
  );
}

const inputStyle: React.CSSProperties = { width: '100%', padding: '8px 10px', border: '1px solid #d0d0d0', borderRadius: 4, fontSize: 14, boxSizing: 'border-box', marginBottom: 12 };
const btnStyle: React.CSSProperties = { width: '100%', padding: 10, borderRadius: 4, border: 'none', background: '#4a90d9', color: '#fff', fontSize: 15, cursor: 'pointer' };
