import { useState } from 'react';
import { useTranslation } from 'react-i18next';
import { doSetup } from '../api/auth';

export function SetupPage({ onDone }: { onDone: () => void }) {
  const { t } = useTranslation();
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [confirmPassword, setConfirmPassword] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (password !== confirmPassword) {
      setError(t('setup.passwordMismatch'));
      return;
    }
    setLoading(true);
    try {
      await doSetup(username.trim(), password);
      onDone();
    } catch (err: any) {
      setError(err.message || t('common.error'));
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="auth-shell">
      <div className="auth-card">
        <div className="mini">iWorkerCloud</div>
        <h2>{t('setup.title')}</h2>
        <p className="auth-description">{t('setup.description')}</p>
        <form onSubmit={handleSubmit} style={{ display: 'grid', gap: 14 }}>
          <div><label>{t('login.username')}</label><input value={username} onChange={e => setUsername(e.target.value)} autoComplete="username" /></div>
          <div><label>{t('login.password')}</label><input type="password" value={password} onChange={e => setPassword(e.target.value)} autoComplete="new-password" /></div>
          <div><label>{t('setup.confirmPassword')}</label><input type="password" value={confirmPassword} onChange={e => setConfirmPassword(e.target.value)} autoComplete="new-password" /></div>
          {error && <p className="auth-error auth-error-tight">{error}</p>}
          <button type="submit" className="btn-primary" disabled={loading || !username.trim() || password.length < 6 || !confirmPassword}>{loading ? t('common.loading') : t('common.confirm')}</button>
        </form>
      </div>
    </div>
  );
}
