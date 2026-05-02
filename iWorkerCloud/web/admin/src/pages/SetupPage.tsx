import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { doSetup } from '../api/auth';

export function SetupPage({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    try { await doSetup(username, password); onDone(); }
    catch (err: any) { setError(err.message); }
  };

  return (
    <div className="auth-shell">
      <div className="auth-card">
        <h2>{t('setup.title')}</h2>
        <p className="auth-description">{t('setup.description')}</p>
        <form onSubmit={handleSubmit} style={{ display: 'grid', gap: 14 }}>
          <div><label>{t('login.username')}</label><input value={username} onChange={e => setUsername(e.target.value)} /></div>
          <div><label>{t('login.password')}</label><input type="password" value={password} onChange={e => setPassword(e.target.value)} /></div>
          {error && <p className="auth-error auth-error-tight">{error}</p>}
          <button type="submit" className="btn-primary">{t('common.confirm')}</button>
        </form>
      </div>
    </div>
  );
}
