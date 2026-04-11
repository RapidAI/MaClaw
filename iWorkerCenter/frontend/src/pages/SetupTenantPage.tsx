import { useState } from 'react';

export function SetupTenantPage({ onSetupComplete }: { onSetupComplete: () => void }) {
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
    if (!companyName.trim()) { setError('请输入企业名称'); return; }
    if (!email.trim()) { setError('请输入企业邮箱'); return; }
    if (!adminUsername.trim()) { setError('请输入管理员用户名'); return; }
    if (!adminPassword) { setError('请输入管理员密码'); return; }
    if (adminPassword !== confirmPassword) { setError('两次密码输入不一致'); return; }
    if (adminPassword.length < 4) { setError('密码至少 4 个字符'); return; }

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
        onSetupComplete();
      } else {
        setError(data?.error || '创建失败');
      }
    } catch {
      setError('网络错误');
    }
    setLoading(false);
  };

  return (
    <div style={containerStyle}>
      <div style={cardStyle}>
        <h2 style={{ margin: '0 0 4px', fontSize: 20, textAlign: 'center' }}>欢迎使用数字员工中心</h2>
        <p style={{ margin: '0 0 24px', fontSize: 13, color: '#888', textAlign: 'center' }}>
          首次使用，请填写企业信息并创建管理员账户
        </p>

        <form onSubmit={handleSubmit}>
          <fieldset style={sectionStyle}>
            <legend style={legendStyle}>企业信息</legend>
            <div style={fieldStyle}>
              <label style={labelStyle}>企业名称 *</label>
              <input type="text" value={companyName} onChange={e => setCompanyName(e.target.value)}
                style={inputStyle} placeholder="如：XX科技有限公司" autoFocus />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>法人代表</label>
              <input type="text" value={legalPerson} onChange={e => setLegalPerson(e.target.value)}
                style={inputStyle} placeholder="法人代表姓名" />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>企业邮箱 *</label>
              <input type="email" value={email} onChange={e => setEmail(e.target.value)}
                style={inputStyle} placeholder="admin@example.com" />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>企业地址</label>
              <input type="text" value={address} onChange={e => setAddress(e.target.value)}
                style={inputStyle} placeholder="企业注册地址" />
            </div>
          </fieldset>

          <fieldset style={sectionStyle}>
            <legend style={legendStyle}>管理员账户</legend>
            <div style={fieldStyle}>
              <label style={labelStyle}>用户名 *</label>
              <input type="text" value={adminUsername} onChange={e => setAdminUsername(e.target.value)}
                style={inputStyle} />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>密码 *</label>
              <input type="password" value={adminPassword} onChange={e => setAdminPassword(e.target.value)}
                style={inputStyle} placeholder="至少 4 个字符" />
            </div>
            <div style={fieldStyle}>
              <label style={labelStyle}>确认密码 *</label>
              <input type="password" value={confirmPassword} onChange={e => setConfirmPassword(e.target.value)}
                style={inputStyle} placeholder="再次输入密码" />
            </div>
          </fieldset>

          {error && <p style={{ color: '#c33', fontSize: 13, margin: '0 0 12px' }}>{error}</p>}
          <button type="submit" disabled={loading} style={submitBtnStyle}>
            {loading ? '创建中...' : '创建企业并开始使用'}
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
  width: 420, padding: '32px 28px', background: '#fff',
  borderRadius: 8, boxShadow: '0 2px 12px rgba(0,0,0,0.08)',
};
const sectionStyle: React.CSSProperties = {
  border: '1px solid #e8e8e8', borderRadius: 6, padding: '16px 16px 8px',
  marginBottom: 20,
};
const legendStyle: React.CSSProperties = {
  fontSize: 14, fontWeight: 600, color: '#333', padding: '0 6px',
};
const fieldStyle: React.CSSProperties = { marginBottom: 14 };
const labelStyle: React.CSSProperties = {
  display: 'block', fontSize: 13, color: '#555', marginBottom: 4,
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
