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
        setError(data?.error?.message || data?.error || '创建失败');
      }
    } catch {
      setError('网络错误');
    }
    setLoading(false);
  };

  return (
    <div className="setup-shell">
      <div className="setup-card">
        <div className="mini">iWorkerCenter Bootstrap</div>
        <h2>欢迎使用数字员工中心</h2>
        <p>首次使用时先创建本地单位/租户和管理员账号。进入系统后，可继续完成单位初始化、iWorker 编组、MCP/Skill 下发和 Cloud 注册。</p>

        <form onSubmit={handleSubmit}>
          <fieldset>
            <legend>企业信息</legend>
            <label>企业名称 *</label>
            <input type="text" value={companyName} onChange={e => setCompanyName(e.target.value)} placeholder="例如：XX 科技有限公司" autoFocus />
            <label>法人代表</label>
            <input type="text" value={legalPerson} onChange={e => setLegalPerson(e.target.value)} placeholder="法人代表姓名" />
            <label>企业邮箱 *</label>
            <input type="email" value={email} onChange={e => setEmail(e.target.value)} placeholder="admin@example.com" />
            <label>企业地址</label>
            <input type="text" value={address} onChange={e => setAddress(e.target.value)} placeholder="企业注册地址或办公地址" />
          </fieldset>

          <fieldset>
            <legend>管理员账号</legend>
            <label>用户名 *</label>
            <input type="text" value={adminUsername} onChange={e => setAdminUsername(e.target.value)} />
            <label>密码 *</label>
            <input type="password" value={adminPassword} onChange={e => setAdminPassword(e.target.value)} placeholder="至少 4 个字符" />
            <label>确认密码 *</label>
            <input type="password" value={confirmPassword} onChange={e => setConfirmPassword(e.target.value)} placeholder="再次输入密码" />
          </fieldset>

          {error && <p className="cloud-message danger">{error}</p>}
          <button type="submit" disabled={loading} className="cloud-primary setup-submit">
            {loading ? '创建中...' : '创建企业并进入初始化'}
          </button>
        </form>
      </div>
    </div>
  );
}
