import { useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';

async function fetchJSON<T>(url: string): Promise<T | null> {
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    return resp.json();
  } catch { return null; }
}

async function putJSON(url: string, body: unknown): Promise<{ ok: boolean; msg?: string }> {
  try {
    const resp = await fetch(url, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const data = await resp.json();
    if (!resp.ok) return { ok: false, msg: data?.error?.message || '操作失败' };
    return { ok: true };
  } catch { return { ok: false, msg: '网络错误' }; }
}

export function AccountSettingsPage() {
  const [email, setEmail] = useState('');
  const [emailMsg, setEmailMsg] = useState('');
  const [oldPwd, setOldPwd] = useState('');
  const [newPwd, setNewPwd] = useState('');
  const [confirmPwd, setConfirmPwd] = useState('');
  const [pwdMsg, setPwdMsg] = useState('');
  const [username, setUsername] = useState('');

  useEffect(() => {
    fetchJSON<{ username: string; email: string }>('/admin/profile').then(d => {
      if (d) {
        setEmail(d.email || '');
        setUsername(d.username || 'admin');
      }
    });
  }, []);

  const saveEmail = async () => {
    setEmailMsg('');
    const r = await putJSON('/admin/profile', { email });
    setEmailMsg(r.ok ? '✅ 邮箱已更新' : `❌ ${r.msg}`);
    setTimeout(() => setEmailMsg(''), 3000);
  };

  const savePassword = async () => {
    setPwdMsg('');
    if (newPwd !== confirmPwd) {
      setPwdMsg('❌ 两次输入的新密码不一致');
      return;
    }
    if (newPwd.length < 4) {
      setPwdMsg('❌ 新密码至少 4 个字符');
      return;
    }
    const r = await putJSON('/admin/password', { old_password: oldPwd, new_password: newPwd });
    if (r.ok) {
      setPwdMsg('✅ 密码已更新');
      setOldPwd('');
      setNewPwd('');
      setConfirmPwd('');
    } else {
      setPwdMsg(`❌ ${r.msg}`);
    }
    setTimeout(() => setPwdMsg(''), 4000);
  };

  return (
    <div className="center-page-stack">
      <SectionCard title="管理员资料" desc="修改管理邮箱（用于向 iWorker 云端中心注册）。">
        <div style={{ maxWidth: 420 }}>
          <div style={fieldStyle}>
            <label style={labelStyle}>用户名</label>
            <input type="text" value={username} disabled style={{ ...inputStyle, background: '#f5f5f5' }} />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>管理邮箱</label>
            <input type="email" value={email} onChange={e => setEmail(e.target.value)}
              placeholder="admin@example.com" style={inputStyle} />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 8 }}>
            <button onClick={saveEmail} style={btnStyle}>保存邮箱</button>
            {emailMsg && <span style={{ fontSize: 13 }}>{emailMsg}</span>}
          </div>
        </div>
      </SectionCard>

      <SectionCard title="修改密码" desc="修改管理员登录密码。">
        <div style={{ maxWidth: 420 }}>
          <div style={fieldStyle}>
            <label style={labelStyle}>当前密码</label>
            <input type="password" value={oldPwd} onChange={e => setOldPwd(e.target.value)}
              placeholder="输入当前密码" style={inputStyle} />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>新密码</label>
            <input type="password" value={newPwd} onChange={e => setNewPwd(e.target.value)}
              placeholder="输入新密码（至少 4 位）" style={inputStyle} />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>确认新密码</label>
            <input type="password" value={confirmPwd} onChange={e => setConfirmPwd(e.target.value)}
              placeholder="再次输入新密码" style={inputStyle} />
          </div>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, marginTop: 8 }}>
            <button onClick={savePassword} style={btnStyle}>修改密码</button>
            {pwdMsg && <span style={{ fontSize: 13 }}>{pwdMsg}</span>}
          </div>
        </div>
      </SectionCard>
    </div>
  );
}

const fieldStyle: React.CSSProperties = { marginBottom: 12 };
const labelStyle: React.CSSProperties = { display: 'block', fontSize: 13, color: '#555', marginBottom: 4 };
const inputStyle: React.CSSProperties = {
  width: '100%', padding: '7px 10px', border: '1px solid #d0d0d0',
  borderRadius: 4, fontSize: 13, boxSizing: 'border-box',
};
const btnStyle: React.CSSProperties = {
  padding: '7px 18px', borderRadius: 4, border: '1px solid #ccc',
  background: '#fff', cursor: 'pointer', fontSize: 13,
};
