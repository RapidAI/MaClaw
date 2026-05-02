import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { loadCaptcha, doLogin } from '../api/auth';

export function LoginPage({ onLogin }: { onLogin: () => void }) {
  const { t } = useTranslation();
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [captchaId, setCaptchaId] = useState('');
  const [captchaQ, setCaptchaQ] = useState('');
  const [answer, setAnswer] = useState('');
  const [error, setError] = useState('');

  const refreshCaptcha = () => {
    loadCaptcha().then(c => { setCaptchaId(c.id); setCaptchaQ(c.question); setAnswer(''); }).catch(() => {});
  };

  useEffect(() => { refreshCaptcha(); }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    try {
      await doLogin(username, password, captchaId, answer);
      onLogin();
    } catch (err: any) {
      setError(err.message || t('login.error'));
      refreshCaptcha();
    }
  };

  return (
    <div className="auth-shell">
      <div className="auth-card">
        <div className="mini">iWorkerCloud</div>
        <h2>{t('login.title')}</h2>
        <form onSubmit={handleSubmit} style={{ display: 'grid', gap: 14 }}>
          <div><label>{t('login.username')}</label><input value={username} onChange={e => setUsername(e.target.value)} /></div>
          <div><label>{t('login.password')}</label><input type="password" value={password} onChange={e => setPassword(e.target.value)} /></div>
          <div>
            <label>{captchaQ || '...'} <button type="button" onClick={refreshCaptcha} style={{ border: 'none', background: 'transparent', cursor: 'pointer' }}>🔄</button></label>
            <input value={answer} onChange={e => setAnswer(e.target.value)} placeholder="Answer" />
          </div>
          {error && <p className="auth-error auth-error-tight">{error}</p>}
          <button type="submit" className="btn-primary">{t('login.submit')}</button>
        </form>
      </div>
    </div>
  );
}
