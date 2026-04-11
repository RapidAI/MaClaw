import { useState, useEffect } from 'react';

type CaptchaData = { captcha_id: string; question: string };
type TenantItem = { id: string; company_name: string };

async function fetchJSON<T>(url: string): Promise<T | null> {
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    return resp.json();
  } catch { return null; }
}

export function LoginPage({ onLogin }: { onLogin: () => void }) {
  const [tenants, setTenants] = useState<TenantItem[]>([]);
  const [selectedTenantId, setSelectedTenantId] = useState('');
  const [username, setUsername] = useState('admin');
  const [password, setPassword] = useState('');
  const [captcha, setCaptcha] = useState<CaptchaData | null>(null);
  const [captchaAnswer, setCaptchaAnswer] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);
  const [tenantsLoading, setTenantsLoading] = useState(true);

  // Load tenant list
  useEffect(() => {
    fetchJSON<{ tenants: TenantItem[] }>('/auth/tenants').then(d => {
      const list = d?.tenants || [];
      setTenants(list);
      // Auto-select if only one tenant
      if (list.length === 1) {
        setSelectedTenantId(list[0].id);
      }
      setTenantsLoading(false);
    });
  }, []);

  const loadCaptcha = () => {
    fetchJSON<CaptchaData>('/auth/captcha').then(d => {
      if (d) { setCaptcha(d); setCaptchaAnswer(''); }
    });
  };

  useEffect(() => { loadCaptcha(); }, []);

  const showTenantSelector = tenants.length > 1;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    if (showTenantSelector && !selectedTenantId) { setError('请选择企业'); return; }
    if (!captcha) { setError('验证码加载失败，请刷新'); return; }
    setLoading(true);
    try {
      const resp = await fetch('/auth/login', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          tenant_id: selectedTenantId,
          username,
          password,
          captcha_id: captcha.captcha_id,
          captcha_answer: parseInt(captchaAnswer) || 0,
        }),
      });
      const data = await resp.json();
      if (resp.ok && data.status === 'ok') {
        onLogin();
      } else {
        setError(data?.error?.message || data?.message || '登录失败');
        loadCaptcha();
      }
    } catch {
      setError('网络错误');
    }
    setLoading(false);
  };

  if (tenantsLoading) {
    return (
      <div style={containerStyle}>
        <div style={{ color: '#888' }}>加载中...</div>
      </div>
    );
  }

  return (
    <div style={containerStyle}>
      <div style={cardStyle}>
        <h2 style={{ margin: '0 0 4px', fontSize: 20, textAlign: 'center' }}>数字员工中心</h2>
        <p style={{ margin: '0 0 24px', fontSize: 13, color: '#888', textAlign: 'center' }}>iWorkerCenter 管理登录</p>

        <form onSubmit={handleSubmit}>
          {showTenantSelector && (
            <div style={fieldStyle}>
              <label style={labelStyle}>选择企业</label>
              <select value={selectedTenantId} onChange={e => setSelectedTenantId(e.target.value)}
                style={inputStyle}>
                <option value="">-- 请选择企业 --</option>
                {tenants.map(t => (
                  <option key={t.id} value={t.id}>{t.company_name}</option>
                ))}
              </select>
            </div>
          )}
          <div style={fieldStyle}>
            <label style={labelStyle}>用户名</label>
            <input type="text" value={username} onChange={e => setUsername(e.target.value)}
              style={inputStyle} autoFocus />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>密码</label>
            <input type="password" value={password} onChange={e => setPassword(e.target.value)}
              style={inputStyle} placeholder="输入密码" />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>
              验证码：{captcha ? <span style={{ fontWeight: 600, color: '#1a1a1a', fontSize: 15 }}>{captcha.question}</span> : '加载中...'}
              <button type="button" onClick={loadCaptcha} style={refreshBtnStyle}>🔄</button>
            </label>
            <input type="number" value={captchaAnswer} onChange={e => setCaptchaAnswer(e.target.value)}
              style={inputStyle} placeholder="输入计算结果" />
          </div>
          {error && <p style={{ color: '#c33', fontSize: 13, margin: '0 0 12px' }}>{error}</p>}
          <button type="submit" disabled={loading} style={submitBtnStyle}>
            {loading ? '登录中...' : '登 录'}
          </button>
        </form>
      </div>
    </div>
  );
}

const containerStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', justifyContent: 'center',
  minHeight: '100vh', background: '#f0f2f5',
};
const cardStyle: React.CSSProperties = {
  width: 360, padding: '32px 28px', background: '#fff',
  borderRadius: 8, boxShadow: '0 2px 12px rgba(0,0,0,0.08)',
};
const fieldStyle: React.CSSProperties = { marginBottom: 16 };
const labelStyle: React.CSSProperties = {
  display: 'flex', alignItems: 'center', gap: 6,
  fontSize: 13, color: '#555', marginBottom: 6,
};
const inputStyle: React.CSSProperties = {
  width: '100%', padding: '8px 10px', border: '1px solid #d0d0d0',
  borderRadius: 4, fontSize: 14, boxSizing: 'border-box',
};
const submitBtnStyle: React.CSSProperties = {
  width: '100%', padding: '10px', borderRadius: 4, border: 'none',
  background: '#4a90d9', color: '#fff', fontSize: 15, cursor: 'pointer',
  fontWeight: 600,
};
const refreshBtnStyle: React.CSSProperties = {
  border: 'none', background: 'transparent', cursor: 'pointer',
  fontSize: 14, padding: '0 4px',
};
