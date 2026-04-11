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
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
      <div className="card" style={{ width: 400, padding: 32 }}>
        <h2>{t('setup.title')}</h2>
        <p style={{ color: '#888' }}>{t('setup.description')}</p>
        <form onSubmit={handleSubmit} style={{ display: 'grid', gap: 14 }}>
          <div><label>{t('login.username')}</label><input value={username} onChange={e => setUsername(e.target.value)} /></div>
          <div><label>{t('login.password')}</label><input type="password" value={password} onChange={e => setPassword(e.target.value)} /></div>
          {error && <p style={{ color: '#c33', fontSize: 13, margin: 0 }}>{error}</p>}
          <button type="submit" className="btn-primary">{t('common.confirm')}</button>
        </form>
      </div>
    </div>
  );
}
