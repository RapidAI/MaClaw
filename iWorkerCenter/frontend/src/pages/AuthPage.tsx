import { useCallback, useEffect, useState } from 'react';

type LDAPConfig = {
  enabled: boolean;
  host: string;
  port: number;
  use_tls: boolean;
  base_dn: string;
  bind_fmt: string;
};

type DiWorkerAccount = {
  id: string;
  username: string;
  identifier: string;
  expires_at: string | null;
  disabled: boolean;
  created_at: string;
};

type SubTab = 'ldap' | 'local';

const api = async (method: string, path: string, body?: unknown) => {
  const opts: RequestInit = { method, headers: { 'Content-Type': 'application/json' } };
  if (body) opts.body = JSON.stringify(body);
  const r = await fetch(path, opts);
  return r.json();
};

export function AuthPage() {
  const [sub, setSub] = useState<SubTab>('ldap');
  return (
    <div>
      <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        <button className={sub === 'ldap' ? 'btn-primary' : 'btn-ghost'} onClick={() => setSub('ldap')}>LDAP 认证</button>
        <button className={sub === 'local' ? 'btn-primary' : 'btn-ghost'} onClick={() => setSub('local')}>本地账户</button>
      </div>
      {sub === 'ldap' ? <LDAPPanel /> : <LocalAccountPanel />}
    </div>
  );
}

function LDAPPanel() {
  const [cfg, setCfg] = useState<LDAPConfig>({ enabled: false, host: '', port: 389, use_tls: false, base_dn: '', bind_fmt: '{user}@example.com' });
  const [testUser, setTestUser] = useState('');
  const [testPass, setTestPass] = useState('');
  const [msg, setMsg] = useState('');

  useEffect(() => { api('GET', '/admin/diworker-auth/ldap').then(r => { if (r.data) setCfg(r.data); }); }, []);

  const save = async () => {
    await api('POST', '/admin/diworker-auth/ldap', cfg);
    setMsg('LDAP 配置已保存');
  };

  const test = async () => {
    const r = await api('POST', '/admin/diworker-auth/ldap/test', { username: testUser, password: testPass });
    setMsg(r.data?.success ? '✅ 认证成功' : `❌ 认证失败: ${r.data?.error || r.error?.message || '未知错误'}`);
  };

  return (
    <div className="item" style={{ display: 'grid', gap: 14 }}>
      <div className="item-title">LDAP / Active Directory 认证</div>
      <div className="item-meta">配置上游 LDAP 认证服务器（如 Windows Domain Server），数字员工通过域账号认证。</div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <label style={{ margin: 0, fontWeight: 700 }}>启用 LDAP</label>
        <input type="checkbox" checked={cfg.enabled} onChange={e => setCfg({ ...cfg, enabled: e.target.checked })} style={{ width: 'auto', height: 'auto' }} />
      </div>
      <div className="grid2">
        <div><label>服务器地址</label><input value={cfg.host} onChange={e => setCfg({ ...cfg, host: e.target.value })} placeholder="dc.example.com" /></div>
        <div><label>端口</label><input type="number" value={cfg.port} onChange={e => setCfg({ ...cfg, port: +e.target.value })} /></div>
      </div>
      <div className="grid2">
        <div><label>Base DN</label><input value={cfg.base_dn} onChange={e => setCfg({ ...cfg, base_dn: e.target.value })} placeholder="dc=example,dc=com" /></div>
        <div><label>Bind 格式</label><input value={cfg.bind_fmt} onChange={e => setCfg({ ...cfg, bind_fmt: e.target.value })} placeholder="{user}@example.com" /></div>
      </div>
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <label style={{ margin: 0, fontWeight: 700 }}>使用 TLS</label>
        <input type="checkbox" checked={cfg.use_tls} onChange={e => setCfg({ ...cfg, use_tls: e.target.checked })} style={{ width: 'auto', height: 'auto' }} />
      </div>
      <div className="actions"><button className="btn-primary" onClick={save}>保存配置</button></div>
      <div style={{ borderTop: '1px solid rgba(20,33,54,.06)', paddingTop: 14 }}>
        <div className="item-title">测试连接</div>
        <div className="grid2" style={{ marginTop: 8 }}>
          <div><label>用户名</label><input value={testUser} onChange={e => setTestUser(e.target.value)} placeholder="testuser" /></div>
          <div><label>密码</label><input type="password" value={testPass} onChange={e => setTestPass(e.target.value)} /></div>
        </div>
        <div className="actions" style={{ marginTop: 8 }}><button className="btn-ghost" onClick={test}>测试认证</button></div>
      </div>
      {msg && <div style={{ fontSize: 13, color: 'var(--muted)', marginTop: 4 }}>{msg}</div>}
    </div>
  );
}

function LocalAccountPanel() {
  const [accounts, setAccounts] = useState<DiWorkerAccount[]>([]);
  const [total, setTotal] = useState(0);
  const [form, setForm] = useState({ username: '', password: '', identifier: '', expiry_days: 0 });
  const [editId, setEditId] = useState('');
  const [msg, setMsg] = useState('');
  const [csvFile, setCsvFile] = useState<File | null>(null);

  const load = useCallback(async () => {
    const r = await api('GET', '/admin/diworker-auth/accounts?limit=200');
    if (r.data) { setAccounts(r.data.items || []); setTotal(r.data.total || 0); }
  }, []);

  useEffect(() => { load(); }, [load]);

  const save = async () => {
    if (editId) {
      // Only send expiry_days if user explicitly set it (non-zero means set, 0 means permanent)
      const payload: Record<string, unknown> = { ...form };
      if (form.expiry_days === 0 && !form.password) {
        // When editing, 0 means "set to permanent"; omit to leave unchanged
        // But we keep it — user sees "0=永久" in the field
      }
      await api('PUT', `/admin/diworker-auth/accounts/${editId}`, payload);
      setMsg('已更新');
    } else {
      if (!form.username || !form.password) { setMsg('用户名和密码必填'); return; }
      const r = await api('POST', '/admin/diworker-auth/accounts', form);
      if (r.error) { setMsg(`创建失败: ${r.error.message}`); return; }
      setMsg('已创建');
    }
    setForm({ username: '', password: '', identifier: '', expiry_days: 0 });
    setEditId('');
    load();
  };

  const del = async (id: string) => {
    if (!confirm('确认删除？')) return;
    await api('DELETE', `/admin/diworker-auth/accounts/${id}`);
    load();
  };

  const edit = (a: DiWorkerAccount) => {
    setEditId(a.id);
    setForm({ username: a.username, password: '', identifier: a.identifier, expiry_days: 0 });
  };

  const importCSV = async () => {
    if (!csvFile) return;
    const fd = new FormData();
    fd.append('file', csvFile);
    const r = await fetch('/admin/diworker-auth/import-csv', { method: 'POST', body: fd });
    const data = await r.json();
    const d = data.data || data;
    setMsg(`导入完成: 创建 ${d.created || 0}, 跳过 ${d.skipped || 0}${d.errors?.length ? ', 错误: ' + d.errors.join('; ') : ''}`);
    setCsvFile(null);
    load();
  };

  return (
    <div style={{ display: 'grid', gap: 16 }}>
      <div className="item" style={{ display: 'grid', gap: 14 }}>
        <div className="item-title">{editId ? '编辑账户' : '创建账户'}</div>
        <div className="grid2">
          <div><label>用户名</label><input value={form.username} onChange={e => setForm({ ...form, username: e.target.value })} /></div>
          <div><label>密码{editId ? '（留空不修改）' : ''}</label><input type="password" value={form.password} onChange={e => setForm({ ...form, password: e.target.value })} /></div>
        </div>
        <div className="grid2">
          <div><label>邮箱/标识</label><input value={form.identifier} onChange={e => setForm({ ...form, identifier: e.target.value })} /></div>
          <div><label>有效期（天，0=永久）</label><input type="number" value={form.expiry_days} onChange={e => setForm({ ...form, expiry_days: +e.target.value })} /></div>
        </div>
        <div className="actions">
          <button className="btn-primary" onClick={save}>{editId ? '更新' : '创建'}</button>
          {editId && <button className="btn-ghost" onClick={() => { setEditId(''); setForm({ username: '', password: '', identifier: '', expiry_days: 0 }); }}>取消</button>}
        </div>
        {msg && <div style={{ fontSize: 13, color: 'var(--muted)' }}>{msg}</div>}
      </div>

      <div className="item" style={{ display: 'grid', gap: 14 }}>
        <div className="item-title">CSV 批量导入</div>
        <div className="item-meta">格式：用户名,密码,邮箱/标识,有效期天数（0=永久）。每行一条。</div>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
          <input type="file" accept=".csv,.txt" onChange={e => setCsvFile(e.target.files?.[0] || null)} />
          <button className="btn-primary" onClick={importCSV} disabled={!csvFile}>导入</button>
        </div>
      </div>

      <div className="item">
        <div className="item-title">账户列表（共 {total} 个）</div>
        <table style={{ width: '100%', marginTop: 12, borderCollapse: 'collapse', fontSize: 13 }}>
          <thead><tr style={{ borderBottom: '1px solid var(--line)', textAlign: 'left' }}>
            <th style={{ padding: '8px 6px' }}>用户名</th><th>标识</th><th>有效期</th><th>状态</th><th>操作</th>
          </tr></thead>
          <tbody>
            {accounts.map(a => (
              <tr key={a.id} style={{ borderBottom: '1px solid var(--line)' }}>
                <td style={{ padding: '8px 6px' }}>{a.username}</td>
                <td>{a.identifier || '-'}</td>
                <td>{a.expires_at ? new Date(a.expires_at).toLocaleDateString() : '永久'}</td>
                <td>{a.disabled ? <span className="badge danger">禁用</span> : <span className="badge ok">启用</span>}</td>
                <td style={{ display: 'flex', gap: 4 }}>
                  <button className="btn-ghost" style={{ height: 28, fontSize: 12 }} onClick={() => edit(a)}>编辑</button>
                  <button className="btn-danger" style={{ height: 28, fontSize: 12 }} onClick={() => del(a.id)}>删除</button>
                </td>
              </tr>
            ))}
            {accounts.length === 0 && <tr><td colSpan={5} style={{ padding: 16, color: 'var(--muted)', textAlign: 'center' }}>暂无账户</td></tr>}
          </tbody>
        </table>
      </div>
    </div>
  );
}
