import { useEffect, useState, type CSSProperties } from 'react';
import { SectionCard } from '../components/cards/SectionCard';

type TenantMode = 'dedicated' | 'multi_tenant';

async function fetchJSON<T>(url: string): Promise<T | null> {
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    return resp.json();
  } catch {
    return null;
  }
}

async function putJSON(url: string, body: unknown): Promise<{ ok: boolean; msg?: string }> {
  try {
    const resp = await fetch(url, {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body),
    });
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) return { ok: false, msg: data?.error?.message || '操作失败' };
    return { ok: true };
  } catch {
    return { ok: false, msg: '网络错误' };
  }
}

export function AccountSettingsPage() {
  const [email, setEmail] = useState('');
  const [emailMsg, setEmailMsg] = useState('');
  const [oldPwd, setOldPwd] = useState('');
  const [newPwd, setNewPwd] = useState('');
  const [confirmPwd, setConfirmPwd] = useState('');
  const [pwdMsg, setPwdMsg] = useState('');
  const [username, setUsername] = useState('');
  const [tenantMode, setTenantMode] = useState<TenantMode>('dedicated');
  const [tenantModeMsg, setTenantModeMsg] = useState('');

  useEffect(() => {
    fetchJSON<{ mode: TenantMode }>('/admin/system/tenant-mode').then(d => {
      if (d?.mode) setTenantMode(d.mode);
    });
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
    setEmailMsg(r.ok ? '邮箱已更新' : `更新失败：${r.msg}`);
    setTimeout(() => setEmailMsg(''), 3000);
  };

  const saveTenantMode = async () => {
    setTenantModeMsg('');
    const r = await putJSON('/admin/system/tenant-mode', { mode: tenantMode });
    setTenantModeMsg(r.ok ? '租户模式已保存' : `保存失败：${r.msg}`);
    setTimeout(() => setTenantModeMsg(''), 3000);
  };

  const savePassword = async () => {
    setPwdMsg('');
    if (newPwd !== confirmPwd) {
      setPwdMsg('两次输入的新密码不一致');
      return;
    }
    if (newPwd.length < 4) {
      setPwdMsg('新密码至少 4 个字符');
      return;
    }
    const r = await putJSON('/admin/password', { old_password: oldPwd, new_password: newPwd });
    if (r.ok) {
      setPwdMsg('密码已更新');
      setOldPwd('');
      setNewPwd('');
      setConfirmPwd('');
    } else {
      setPwdMsg(`更新失败：${r.msg}`);
    }
    setTimeout(() => setPwdMsg(''), 4000);
  };

  return (
    <div className="center-page-stack">
      <SectionCard title="管理员资料" desc="修改管理员邮箱，用于向 iWorkerCloud 注册和接收审核信息。">
        <div style={{ maxWidth: 420 }}>
          <div style={fieldStyle}>
            <label style={labelStyle}>用户名</label>
            <input type="text" value={username} disabled style={{ ...inputStyle, background: '#f5f5f5' }} />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>管理员邮箱</label>
            <input
              type="email"
              value={email}
              onChange={e => setEmail(e.target.value)}
              placeholder="admin@example.com"
              style={inputStyle}
            />
          </div>
          <div style={actionRowStyle}>
            <button onClick={saveEmail} style={btnStyle}>保存邮箱</button>
            {emailMsg && <span style={msgStyle}>{emailMsg}</span>}
          </div>
        </div>
      </SectionCard>

      <SectionCard title="租户模式" desc="设置当前 iWorkerCenter 是专用中心，还是允许 iWorkerCloud 远程管理多个公司租户。">
        <div style={{ maxWidth: 520 }}>
          <div style={fieldStyle}>
            <label style={labelStyle}>运行模式</label>
            <select
              value={tenantMode}
              onChange={e => setTenantMode(e.target.value as TenantMode)}
              style={inputStyle}
            >
              <option value="dedicated">专用模式</option>
              <option value="multi_tenant">多租户模式</option>
            </select>
          </div>
          <p style={hintStyle}>
            启用多租户后，已授权的 iWorkerCloud 可以远程查看、新增、修改和删除本 Center 的公司租户。
          </p>
          <div style={actionRowStyle}>
            <button onClick={saveTenantMode} style={btnStyle}>保存租户模式</button>
            {tenantModeMsg && <span style={msgStyle}>{tenantModeMsg}</span>}
          </div>
        </div>
      </SectionCard>

      <SectionCard title="修改密码" desc="修改管理员登录密码。">
        <div style={{ maxWidth: 420 }}>
          <div style={fieldStyle}>
            <label style={labelStyle}>当前密码</label>
            <input
              type="password"
              value={oldPwd}
              onChange={e => setOldPwd(e.target.value)}
              placeholder="输入当前密码"
              style={inputStyle}
            />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>新密码</label>
            <input
              type="password"
              value={newPwd}
              onChange={e => setNewPwd(e.target.value)}
              placeholder="输入新密码，至少 4 位"
              style={inputStyle}
            />
          </div>
          <div style={fieldStyle}>
            <label style={labelStyle}>确认新密码</label>
            <input
              type="password"
              value={confirmPwd}
              onChange={e => setConfirmPwd(e.target.value)}
              placeholder="再次输入新密码"
              style={inputStyle}
            />
          </div>
          <div style={actionRowStyle}>
            <button onClick={savePassword} style={btnStyle}>修改密码</button>
            {pwdMsg && <span style={msgStyle}>{pwdMsg}</span>}
          </div>
        </div>
      </SectionCard>
    </div>
  );
}

const fieldStyle: CSSProperties = { marginBottom: 12 };
const labelStyle: CSSProperties = { display: 'block', fontSize: 13, color: '#555', marginBottom: 4 };
const inputStyle: CSSProperties = {
  width: '100%',
  padding: '7px 10px',
  border: '1px solid #d0d0d0',
  borderRadius: 4,
  fontSize: 13,
  boxSizing: 'border-box',
};
const btnStyle: CSSProperties = {
  padding: '7px 18px',
  borderRadius: 4,
  border: '1px solid #ccc',
  background: '#fff',
  cursor: 'pointer',
  fontSize: 13,
};
const actionRowStyle: CSSProperties = { display: 'flex', alignItems: 'center', gap: 12, marginTop: 8 };
const hintStyle: CSSProperties = { margin: '0 0 12px', fontSize: 13, color: '#666', lineHeight: 1.6 };
const msgStyle: CSSProperties = { fontSize: 13 };