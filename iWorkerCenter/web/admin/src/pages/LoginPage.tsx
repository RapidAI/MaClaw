import { useState, useEffect } from 'react';
import { useTranslation } from 'react-i18next';

type CaptchaData = { captcha_id: string; question: string };
type TenantItem = { id: string; company_name: string };

export function LoginPage({ onLogin }: { onLogin: () => void }) {
  const { t } = useTranslation();
  const [tenants, setTenants] = useState<TenantItem[]>([]);
  const [selectedTenantId, setSelectedTenantId] = useState('');
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [captcha, setCaptcha] = useState<CaptchaData | null>(null);
  const [captchaAnswer, setCaptchaAnswer] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    fetch('/auth/tenants').then(r => r.ok ? r.json() : null)
      .then(d => {
        const list = d?.tenants || [];
        setTenants(list);
        if (list.length === 1) setSelectedTenantId(list[0].id);
      }).catch(() => {});
    loadCaptcha();
  }, []);

  const loadCaptcha = () => {
    fetch('/auth/captcha').then(r => r.ok ? r.json() : null)
      .then(d => { if (d) { setCaptcha(d); setCaptchaAnswer(''); } })
      .catch(() => {});
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      const resp = await fetch('/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          tenant_id: selectedTenantId,
          username, password,
          captcha_id: captcha?.captcha_id,
          captcha_answer: parseInt(captchaAnswer) || 0,
        }),
      });
      const data = await resp.json();
      if (resp.ok && data.status === 'ok') {
        onLogin();
      } else {
        setError(data?.error?.message || data?.message || t('login.error'));
        loadCaptcha();
      }
    } catch {
      setError(t('common.error'));
    }
    setLoading(false);
  };

  return (
    <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'center', minHeight: '100vh', background: '#f0f2f5' }}>
      <div style={{ width: 360, padding: '32px 28px', background: '#fff', borderRadius: 8, boxShadow: '0 2px 12px rgba(0,0,0,0.08)' }}>
        <h2 style={{ margin: '0 0 4px', fontSize: 20, textAlign: 'center' }}>{t('app.title')}</h2>
        <p style={{ margin: '0 0 24px', fontSize: 13, color: '#888', textAlign: 'center' }}>iWorkerCenter</p>
        <form onSubmit={handleSubmit}>
          {tenants.length > 1 && (
            <div style={{ marginBottom: 16 }}>
              <select value={selectedTenantId} onChange={e => setSelectedTenantId(e.target.value)}
                style={inputStyle}>
                <option value="">--</option>
                {tenants.map(t => <option key={t.id} value={t.id}>{t.company_name}</option>)}
              </select>
            </div>
          )}
          <div style={{ marginBottom: 16 }}>
            <label style={labelStyle}>{t('login.username')}</label>
            <input type="text" value={username} onChange={e => setUsername(e.target.value)} style={inputStyle} autoFocus />
          </div>
          <div style={{ marginBottom: 16 }}>
            <label style={labelStyle}>{t('login.password')}</label>
            <input type="password" value={password} onChange={e => setPassword(e.target.value)} style={inputStyle} />
          </div>
          {captcha && (
            <div style={{ marginBottom: 16 }}>
              <label style={labelStyle}>{captcha.question} <button type="button" onClick={loadCaptcha} style={{ border: 'none', background: 'transparent', cursor: 'pointer' }}>🔄</button></label>
              <input type="number" value={captchaAnswer} onChange={e => setCaptchaAnswer(e.target.value)} style={inputStyle} />
            </div>
          )}
          {error && <p style={{ color: '#c33', fontSize: 13 }}>{error}</p>}
          <button type="submit" disabled={loading} style={btnStyle}>
            {loading ? '...' : t('login.submit')}
          </button>
        </form>
      </div>
    </div>
  );
}

const inputStyle: React.CSSProperties = { width: '100%', padding: '8px 10px', border: '1px solid #d0d0d0', borderRadius: 4, fontSize: 14, boxSizing: 'border-box' };
const labelStyle: React.CSSProperties = { display: 'block', fontSize: 13, color: '#555', marginBottom: 6 };
const btnStyle: React.CSSProperties = { width: '100%', padding: 10, borderRadius: 4, border: 'none', background: '#4a90d9', color: '#fff', fontSize: 15, cursor: 'pointer', fontWeight: 600 };
