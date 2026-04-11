import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { setupTenant } from '../api/auth';

export function SetupTenantPage({ onSetupComplete }: { onSetupComplete: () => void }) {
  const { t } = useTranslation();
  const [tenantName, setTenantName] = useState('');
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try {
      await setupTenant({ tenant_name: tenantName, admin_username: username, admin_password: password });
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
          <input value={tenantName} onChange={e => setTenantName(e.target.value)} placeholder="Company name" style={inputStyle} />
          <input value={username} onChange={e => setUsername(e.target.value)} placeholder={t('login.username')} style={inputStyle} />
          <input type="password" value={password} onChange={e => setPassword(e.target.value)} placeholder={t('login.password')} style={inputStyle} />
          {error && <p style={{ color: '#c33', fontSize: 13 }}>{error}</p>}
          <button type="submit" style={btnStyle}>{t('common.confirm')}</button>
        </form>
      </div>
    </div>
  );
}

const inputStyle: React.CSSProperties = { width: '100%', padding: '8px 10px', border: '1px solid #d0d0d0', borderRadius: 4, fontSize: 14, boxSizing: 'border-box', marginBottom: 12 };
const btnStyle: React.CSSProperties = { width: '100%', padding: 10, borderRadius: 4, border: 'none', background: '#4a90d9', color: '#fff', fontSize: 15, cursor: 'pointer' };
