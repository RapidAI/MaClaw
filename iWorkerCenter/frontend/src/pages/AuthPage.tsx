import { useCallback, useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';

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

type SubTab = 'ldap' | 'local' | 'oidc';

const api = async (method: string, path: string, body?: unknown) => {
  const opts: RequestInit = { method, headers: { 'Content-Type': 'application/json' } };
  if (body) opts.body = JSON.stringify(body);
  const r = await fetch(path, opts);
  const text = await r.text();
  const data = text ? JSON.parse(text) : null;
  if (!r.ok) throw new Error(data?.error?.message || data?.message || '请求失败: ' + r.status);
  return data;
};

export function AuthPage() {
  const [sub, setSub] = useState<SubTab>('local');
  return (
    <div className="center-page-stack">
      <SectionCard title="认证适配器" desc="Center 将 iWorker 认证抽象为适配器：小公司可直接使用本地账号，大型组织可接入 LDAP，后续可扩展 OIDC/OAuth 或零信任网关。">
        <div className="auth-adapter-tabs">
          <button className={sub === 'local' ? 'cloud-primary' : 'btn-secondary'} onClick={() => setSub('local')}>本地账号</button>
          <button className={sub === 'ldap' ? 'cloud-primary' : 'btn-secondary'} onClick={() => setSub('ldap')}>LDAP / AD</button>
          <button className={sub === 'oidc' ? 'cloud-primary' : 'btn-secondary'} onClick={() => setSub('oidc')}>OIDC / OAuth</button>
        </div>
      </SectionCard>
      {sub === 'ldap' ? <LDAPPanel /> : sub === 'local' ? <LocalAccountPanel /> : <OIDCPanel />}
    </div>
  );
}

function LDAPPanel() {
  const [cfg, setCfg] = useState<LDAPConfig>({ enabled: false, host: '', port: 389, use_tls: false, base_dn: '', bind_fmt: '{user}@example.com' });
  const [testUser, setTestUser] = useState('');
  const [testPass, setTestPass] = useState('');
  const [msg, setMsg] = useState<{ kind: 'ok' | 'warn' | 'danger'; text: string } | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => { api('GET', '/admin/diworker-auth/ldap').then(r => { if (r.data) setCfg(r.data); }).catch(() => {}); }, []);

  const save = async () => {
    setBusy(true);
    try {
      await api('POST', '/admin/diworker-auth/ldap', cfg);
      setMsg({ kind: 'ok', text: 'LDAP 配置已保存。' });
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : '保存失败' });
    } finally { setBusy(false); }
  };

  const test = async () => {
    setBusy(true);
    try {
      const r = await api('POST', '/admin/diworker-auth/ldap/test', { username: testUser, password: testPass });
      setMsg(r.data?.success ? { kind: 'ok', text: '认证成功。' } : { kind: 'danger', text: '认证失败：' + (r.data?.error || '未知错误') });
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : '测试失败' });
    } finally { setBusy(false); }
  };

  return (
    <SectionCard title="LDAP / Active Directory" desc="适合已有域控或统一目录的组织。Center 只保存连接配置，iWorker 登录时通过适配器验证身份。">
      <div className="cloud-form-grid">
        <label className="cloud-field"><span>启用 LDAP</span><select value={cfg.enabled ? 'yes' : 'no'} onChange={e => setCfg({ ...cfg, enabled: e.target.value === 'yes' })}><option value="yes">启用</option><option value="no">停用</option></select></label>
        <label className="cloud-field"><span>使用 TLS</span><select value={cfg.use_tls ? 'yes' : 'no'} onChange={e => setCfg({ ...cfg, use_tls: e.target.value === 'yes' })}><option value="yes">启用</option><option value="no">停用</option></select></label>
        <label className="cloud-field"><span>服务器地址</span><input value={cfg.host} onChange={e => setCfg({ ...cfg, host: e.target.value })} placeholder="dc.example.com" /></label>
        <label className="cloud-field"><span>端口</span><input type="number" value={cfg.port} onChange={e => setCfg({ ...cfg, port: Number(e.target.value) || 389 })} /></label>
        <label className="cloud-field"><span>Base DN</span><input value={cfg.base_dn} onChange={e => setCfg({ ...cfg, base_dn: e.target.value })} placeholder="dc=example,dc=com" /></label>
        <label className="cloud-field"><span>Bind 格式</span><input value={cfg.bind_fmt} onChange={e => setCfg({ ...cfg, bind_fmt: e.target.value })} placeholder="{user}@example.com" /></label>
      </div>
      <div className="cloud-actions"><button className="cloud-primary" onClick={save} disabled={busy}>{busy ? '保存中...' : '保存 LDAP 配置'}</button></div>
      <div className="cloud-form-section-title"><strong>测试认证</strong><span>测试账号仅用于验证目录连接，不会创建本地账号。</span></div>
      <div className="cloud-form-grid">
        <label className="cloud-field"><span>用户名</span><input value={testUser} onChange={e => setTestUser(e.target.value)} placeholder="testuser" /></label>
        <label className="cloud-field"><span>密码</span><input type="password" value={testPass} onChange={e => setTestPass(e.target.value)} /></label>
      </div>
      <div className="cloud-actions"><button className="btn-secondary" onClick={test} disabled={busy || !testUser || !testPass}>测试认证</button></div>
      {msg && <p className={'cloud-message ' + msg.kind}>{msg.text}</p>}
    </SectionCard>
  );
}

function LocalAccountPanel() {
  const [accounts, setAccounts] = useState<DiWorkerAccount[]>([]);
  const [total, setTotal] = useState(0);
  const [form, setForm] = useState({ username: '', password: '', identifier: '', expiry_days: 0 });
  const [editId, setEditId] = useState('');
  const [msg, setMsg] = useState<{ kind: 'ok' | 'warn' | 'danger'; text: string } | null>(null);
  const [csvFile, setCsvFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const r = await api('GET', '/admin/diworker-auth/accounts?limit=200');
      if (r.data) { setAccounts(r.data.items || []); setTotal(r.data.total || 0); }
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : '加载账号失败' });
    }
  }, []);

  useEffect(() => { void load(); }, [load]);

  const reset = () => { setForm({ username: '', password: '', identifier: '', expiry_days: 0 }); setEditId(''); };

  const save = async () => {
    if (!form.username.trim()) { setMsg({ kind: 'warn', text: '请输入用户名。' }); return; }
    if (!editId && !form.password) { setMsg({ kind: 'warn', text: '请输入初始密码。' }); return; }
    setBusy(true);
    try {
      if (editId) {
        await api('PUT', '/admin/diworker-auth/accounts/' + editId, form);
        setMsg({ kind: 'ok', text: '账号已更新。' });
      } else {
        await api('POST', '/admin/diworker-auth/accounts', form);
        setMsg({ kind: 'ok', text: '账号已创建。' });
      }
      reset();
      await load();
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : '保存账号失败' });
    } finally { setBusy(false); }
  };

  const del = async (id: string) => {
    if (!confirm('确认删除这个本地认证账号？删除后对应 iWorker 将无法继续使用该账号登录。')) return;
    setBusy(true);
    try {
      await api('DELETE', '/admin/diworker-auth/accounts/' + id);
      setMsg({ kind: 'ok', text: '账号已删除。' });
      await load();
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : '删除失败' });
    } finally { setBusy(false); }
  };

  const edit = (a: DiWorkerAccount) => {
    setEditId(a.id);
    setForm({ username: a.username, password: '', identifier: a.identifier, expiry_days: 0 });
  };

  const importCSV = async () => {
    if (!csvFile) return;
    setBusy(true);
    try {
      const fd = new FormData();
      fd.append('file', csvFile);
      const r = await fetch('/admin/diworker-auth/import-csv', { method: 'POST', body: fd });
      const data = await r.json();
      if (!r.ok) throw new Error(data?.error?.message || '导入失败');
      const d = data.data || data;
      setMsg({ kind: 'ok', text: '导入完成：创建 ' + (d.created || 0) + '，跳过 ' + (d.skipped || 0) + (d.errors?.length ? '，错误：' + d.errors.join('; ') : '') });
      setCsvFile(null);
      await load();
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : '导入失败' });
    } finally { setBusy(false); }
  };

  return (
    <>
      <SectionCard title={editId ? '编辑本地账号' : '创建本地账号'} desc="适合没有 LDAP/域控的小公司。Center 可以直接维护 iWorker 登录账号，也支持 CSV 批量导入。">
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>用户名</span><input value={form.username} onChange={e => setForm({ ...form, username: e.target.value })} /></label>
          <label className="cloud-field"><span>密码{editId ? '（留空不修改）' : ''}</span><input type="password" value={form.password} onChange={e => setForm({ ...form, password: e.target.value })} /></label>
          <label className="cloud-field"><span>邮箱/标识</span><input value={form.identifier} onChange={e => setForm({ ...form, identifier: e.target.value })} /></label>
          <label className="cloud-field"><span>有效期天数（0=永久）</span><input type="number" value={form.expiry_days} onChange={e => setForm({ ...form, expiry_days: Number(e.target.value) || 0 })} /></label>
        </div>
        <div className="cloud-actions">
          <button className="cloud-primary" onClick={save} disabled={busy}>{busy ? '保存中...' : editId ? '更新账号' : '创建账号'}</button>
          {editId && <button className="btn-secondary" onClick={reset}>取消编辑</button>}
        </div>
        {msg && <p className={'cloud-message ' + msg.kind}>{msg.text}</p>}
      </SectionCard>

      <SectionCard title="CSV 批量导入" desc="格式：用户名,密码,邮箱/标识,有效期天数。每行一条，0 表示永久有效。">
        <div className="auth-import-row">
          <input type="file" accept=".csv,.txt" onChange={e => setCsvFile(e.target.files?.[0] || null)} />
          <button className="cloud-primary" onClick={importCSV} disabled={!csvFile || busy}>导入账号</button>
        </div>
      </SectionCard>

      <SectionCard title={'账号列表（共 ' + total + ' 个）'} desc="这些账号用于 iWorker 连接 Center 时认证。">
        <div className="data-table-wrap">
          <table className="data-table">
            <thead><tr><th>用户名</th><th>标识</th><th>有效期</th><th>状态</th><th>操作</th></tr></thead>
            <tbody>
              {accounts.map(a => (
                <tr key={a.id}>
                  <td>{a.username}</td>
                  <td>{a.identifier || '-'}</td>
                  <td>{a.expires_at ? new Date(a.expires_at).toLocaleDateString() : '永久'}</td>
                  <td><span className={a.disabled ? 'badge warn' : 'badge ok'}>{a.disabled ? '禁用' : '启用'}</span></td>
                  <td><div className="capability-row-actions"><button className="btn-secondary" onClick={() => edit(a)}>编辑</button><button className="btn-secondary" onClick={() => del(a.id)}>删除</button></div></td>
                </tr>
              ))}
              {accounts.length === 0 && <tr><td colSpan={5} style={{ color: 'var(--muted)', textAlign: 'center' }}>暂无账号</td></tr>}
            </tbody>
          </table>
        </div>
      </SectionCard>
    </>
  );
}

function OIDCPanel() {
  return (
    <SectionCard title="OIDC / OAuth / 零信任接入" desc="该适配器预留给企业 SSO、零信任网关或外部身份平台。当前版本先展示设计边界，避免把企业接入方案写死在 LDAP 或本地账号里。">
      <div className="cloud-continuity-grid">
        <div className="cloud-continuity-card"><strong>抽象方式</strong><p>认证提供方只负责身份校验，Center 继续负责 iWorker 授权、能力下发和本地业务策略。</p></div>
        <div className="cloud-continuity-card ok"><strong>推荐场景</strong><p>已经使用企业 SSO、设备信任、网关代理或短期令牌的小中大型组织。</p></div>
      </div>
      <p className="cloud-message warn">暂未启用配置表单。后续可按同一 Auth Adapter 接口接入 OIDC discovery、client_id、回调地址和 token 校验策略。</p>
    </SectionCard>
  );
}
