import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';
import { loadCaptcha, doLogin } from '../api/auth';

export function LoginPage({ onLogin, noticeKey }: { onLogin: () => void; noticeKey?: string }) {
  const { t } = useTranslation();
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [captchaId, setCaptchaId] = useState('');
  const [captchaQ, setCaptchaQ] = useState('');
  const [answer, setAnswer] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [captchaLoading, setCaptchaLoading] = useState(false);

  const refreshCaptcha = () => {
    setCaptchaLoading(true);
    loadCaptcha()
      .then(c => {
        setCaptchaId(c.id);
        setCaptchaQ(c.question);
        setAnswer('');
      })
      .catch(() => setError(t('login.captchaLoadFailed')))
      .finally(() => setCaptchaLoading(false));
  };

  useEffect(() => { refreshCaptcha(); }, []);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await doLogin(username.trim(), password, captchaId, answer.trim());
      onLogin();
    } catch (err: any) {
      setError(err.message || t('login.error'));
      refreshCaptcha();
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="auth-shell">
      <div className="auth-card">
        <div className="mini">iWorkerCloud</div>
        <h2>{t('login.title')}</h2>
        <p className="auth-description">{t('login.description')}</p>
        <form onSubmit={handleSubmit} style={{ display: 'grid', gap: 14 }}>
          <div><label>{t('login.username')}</label><input value={username} onChange={e => setUsername(e.target.value)} autoComplete="username" /></div>
          <div><label>{t('login.password')}</label><input type="password" value={password} onChange={e => setPassword(e.target.value)} autoComplete="current-password" /></div>
          <div>
            <label>{captchaQ || t('login.captchaLoading')} <button type="button" onClick={refreshCaptcha} className="captcha-refresh" disabled={captchaLoading}>{t('login.refreshCaptcha')}</button></label>
            <input value={answer} onChange={e => setAnswer(e.target.value)} placeholder={t('login.captchaAnswer')} inputMode="numeric" />
          </div>
          {error ? <p className="auth-error auth-error-tight">{error}</p> : noticeKey ? <p className="auth-info auth-error-tight">{t(noticeKey)}</p> : null}
          <button type="submit" className="btn-primary" disabled={loading || !username.trim() || !password || !captchaId || !answer.trim()}>{loading ? t('common.loading') : t('login.submit')}</button>
        </form>
      </div>
    </div>
  );
}
