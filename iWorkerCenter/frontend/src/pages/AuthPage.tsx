import { useCallback, useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { useI18n } from '../i18n';

type LDAPConfig = {
  enabled: boolean;
  host: string;
  port: number;
  use_tls: boolean;
  base_dn: string;
  bind_fmt: string;
};

type OIDCConfig = {
  enabled: boolean;
  issuer_url: string;
  client_id: string;
  client_secret?: string;
  redirect_url: string;
  scopes: string[];
  allowed_domains: string[];
};

type DiWorkerAccount = {
  id: string;
  username: string;
  identifier: string;
  expires_at: string | null;
  disabled: boolean;
  created_at: string;
};

type SubTab = 'local' | 'ldap' | 'oidc';
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };

const api = async (method: string, path: string, body?: unknown) => {
  const opts: RequestInit = { method, headers: { 'Content-Type': 'application/json' } };
  if (body) opts.body = JSON.stringify(body);
  const resp = await fetch(path, opts);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) throw new Error(data?.error?.message || data?.message || 'Request failed: ' + resp.status);
  return data;
};

const parseList = (value: string) => value.split(/[\n,]/).map(item => item.trim()).filter(Boolean);

export function AuthPage() {
  const { t } = useI18n();
  const [sub, setSub] = useState<SubTab>('local');
  return (
    <div className="center-page-stack">
      <SectionCard
        title={t('认证适配器', 'Authentication Adapters')}
        desc={t('Center 将 iWorker 认证抽象为适配器：小公司可直接使用本地账号，大型组织可接入 LDAP，OIDC/OAuth 作为零信任和企业 SSO 的预留扩展。', 'Center treats iWorker authentication as adapters: small companies can use local accounts, larger organizations can use LDAP, and OIDC/OAuth is reserved for zero-trust and enterprise SSO integration.')}
      >
        <div className="auth-adapter-tabs">
          <button className={sub === 'local' ? 'cloud-primary' : 'btn-secondary'} onClick={() => setSub('local')}>{t('本地账号', 'Local Accounts')}</button>
          <button className={sub === 'ldap' ? 'cloud-primary' : 'btn-secondary'} onClick={() => setSub('ldap')}>LDAP / AD</button>
          <button className={sub === 'oidc' ? 'cloud-primary' : 'btn-secondary'} onClick={() => setSub('oidc')}>OIDC / OAuth</button>
        </div>
      </SectionCard>
      {sub === 'ldap' ? <LDAPPanel /> : sub === 'local' ? <LocalAccountPanel /> : <OIDCPanel />}
    </div>
  );
}

function LDAPPanel() {
  const { t } = useI18n();
  const [cfg, setCfg] = useState<LDAPConfig>({ enabled: false, host: '', port: 389, use_tls: false, base_dn: '', bind_fmt: '{user}@example.com' });
  const [testUser, setTestUser] = useState('');
  const [testPass, setTestPass] = useState('');
  const [msg, setMsg] = useState<Message | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => { api('GET', '/admin/diworker-auth/ldap').then(result => { if (result.data) setCfg(result.data); }).catch(() => undefined); }, []);

  const save = async () => {
    setBusy(true);
    try {
      await api('POST', '/admin/diworker-auth/ldap', cfg);
      setMsg({ kind: 'ok', text: t('LDAP 配置已保存。', 'LDAP configuration saved.') });
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : t('保存失败。', 'Save failed.') });
    } finally {
      setBusy(false);
    }
  };

  const test = async () => {
    setBusy(true);
    try {
      const result = await api('POST', '/admin/diworker-auth/ldap/test', { username: testUser, password: testPass });
      setMsg(result.data?.success
        ? { kind: 'ok', text: t('认证成功。', 'Authentication succeeded.') }
        : { kind: 'danger', text: t('认证失败：', 'Authentication failed: ') + (result.data?.error || t('未知错误', 'Unknown error')) });
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : t('测试失败。', 'Test failed.') });
    } finally {
      setBusy(false);
    }
  };

  return (
    <SectionCard title="LDAP / Active Directory" desc={t('适合已有域控或统一目录的组织。Center 只保存连接配置，iWorker 登录时通过认证适配器验证身份。', 'For organizations with domain control or a shared directory. Center stores connection settings and validates iWorker identity through the authentication adapter during sign-in.')}>
      <div className="cloud-form-grid">
        <label className="cloud-field"><span>{t('启用 LDAP', 'Enable LDAP')}</span><select value={cfg.enabled ? 'yes' : 'no'} onChange={event => setCfg({ ...cfg, enabled: event.target.value === 'yes' })}><option value="yes">{t('启用', 'Enabled')}</option><option value="no">{t('停用', 'Disabled')}</option></select></label>
        <label className="cloud-field"><span>{t('使用 TLS', 'Use TLS')}</span><select value={cfg.use_tls ? 'yes' : 'no'} onChange={event => setCfg({ ...cfg, use_tls: event.target.value === 'yes' })}><option value="yes">{t('启用', 'Enabled')}</option><option value="no">{t('停用', 'Disabled')}</option></select></label>
        <label className="cloud-field"><span>{t('服务器地址', 'Server host')}</span><input value={cfg.host} onChange={event => setCfg({ ...cfg, host: event.target.value })} placeholder="dc.example.com" /></label>
        <label className="cloud-field"><span>{t('端口', 'Port')}</span><input type="number" value={cfg.port} onChange={event => setCfg({ ...cfg, port: Number(event.target.value) || 389 })} /></label>
        <label className="cloud-field"><span>Base DN</span><input value={cfg.base_dn} onChange={event => setCfg({ ...cfg, base_dn: event.target.value })} placeholder="dc=example,dc=com" /></label>
        <label className="cloud-field"><span>{t('Bind 格式', 'Bind format')}</span><input value={cfg.bind_fmt} onChange={event => setCfg({ ...cfg, bind_fmt: event.target.value })} placeholder="{user}@example.com" /></label>
      </div>
      <div className="cloud-actions"><button className="cloud-primary" onClick={save} disabled={busy}>{busy ? t('保存中...', 'Saving...') : t('保存 LDAP 配置', 'Save LDAP settings')}</button></div>
      <div className="cloud-form-section-title"><strong>{t('测试认证', 'Test Authentication')}</strong><span>{t('测试账号仅用于验证目录连接，不会创建本地账号。', 'The test account only verifies directory access and will not create a local account.')}</span></div>
      <div className="cloud-form-grid">
        <label className="cloud-field"><span>{t('用户名', 'Username')}</span><input value={testUser} onChange={event => setTestUser(event.target.value)} placeholder="testuser" /></label>
        <label className="cloud-field"><span>{t('密码', 'Password')}</span><input type="password" value={testPass} onChange={event => setTestPass(event.target.value)} /></label>
      </div>
      <div className="cloud-actions"><button className="btn-secondary" onClick={test} disabled={busy || !testUser || !testPass}>{t('测试认证', 'Test authentication')}</button></div>
      {msg ? <p className={'cloud-message ' + msg.kind}>{msg.text}</p> : null}
    </SectionCard>
  );
}

function LocalAccountPanel() {
  const { t } = useI18n();
  const [accounts, setAccounts] = useState<DiWorkerAccount[]>([]);
  const [total, setTotal] = useState(0);
  const [form, setForm] = useState({ username: '', password: '', identifier: '', expiry_days: 0 });
  const [editId, setEditId] = useState('');
  const [msg, setMsg] = useState<Message | null>(null);
  const [csvFile, setCsvFile] = useState<File | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    try {
      const result = await api('GET', '/admin/diworker-auth/accounts?limit=200');
      if (result.data) {
        setAccounts(result.data.items || []);
        setTotal(result.data.total || 0);
      }
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : t('加载账号失败。', 'Failed to load accounts.') });
    }
  }, [t]);

  useEffect(() => { void load(); }, [load]);

  const reset = () => { setForm({ username: '', password: '', identifier: '', expiry_days: 0 }); setEditId(''); };

  const save = async () => {
    if (!form.username.trim()) { setMsg({ kind: 'warn', text: t('请输入用户名。', 'Enter a username.') }); return; }
    if (!editId && !form.password) { setMsg({ kind: 'warn', text: t('请输入初始密码。', 'Enter an initial password.') }); return; }
    setBusy(true);
    try {
      if (editId) {
        await api('PUT', '/admin/diworker-auth/accounts/' + editId, form);
        setMsg({ kind: 'ok', text: t('账号已更新。', 'Account updated.') });
      } else {
        await api('POST', '/admin/diworker-auth/accounts', form);
        setMsg({ kind: 'ok', text: t('账号已创建。', 'Account created.') });
      }
      reset();
      await load();
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : t('保存账号失败。', 'Failed to save account.') });
    } finally {
      setBusy(false);
    }
  };

  const remove = async (id: string) => {
    if (!confirm(t('确认删除这个本地认证账号？删除后对应 iWorker 将无法继续使用该账号登录。', 'Delete this local authentication account? The corresponding iWorker will no longer be able to sign in with it.'))) return;
    setBusy(true);
    try {
      await api('DELETE', '/admin/diworker-auth/accounts/' + id);
      setMsg({ kind: 'ok', text: t('账号已删除。', 'Account deleted.') });
      await load();
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : t('删除失败。', 'Delete failed.') });
    } finally {
      setBusy(false);
    }
  };

  const edit = (account: DiWorkerAccount) => {
    setEditId(account.id);
    setForm({ username: account.username, password: '', identifier: account.identifier, expiry_days: 0 });
  };

  const importCSV = async () => {
    if (!csvFile) return;
    setBusy(true);
    try {
      const fd = new FormData();
      fd.append('file', csvFile);
      const resp = await fetch('/admin/diworker-auth/import-csv', { method: 'POST', body: fd });
      const data = await resp.json();
      if (!resp.ok) throw new Error(data?.error?.message || t('导入失败。', 'Import failed.'));
      const result = data.data || data;
      setMsg({ kind: 'ok', text: t('导入完成：创建 ', 'Import complete: created ') + (result.created || 0) + t('，跳过 ', ', skipped ') + (result.skipped || 0) + (result.errors?.length ? t('，错误：', ', errors: ') + result.errors.join('; ') : '') });
      setCsvFile(null);
      await load();
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : t('导入失败。', 'Import failed.') });
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <SectionCard title={editId ? t('编辑本地账号', 'Edit Local Account') : t('创建本地账号', 'Create Local Account')} desc={t('适合没有 LDAP/域控的小公司。Center 可以直接维护 iWorker 登录账号，也支持 CSV 批量导入。', 'For smaller companies without LDAP or domain control. Center can maintain iWorker sign-in accounts directly and supports CSV import.')}>
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>{t('用户名', 'Username')}</span><input value={form.username} onChange={event => setForm({ ...form, username: event.target.value })} /></label>
          <label className="cloud-field"><span>{t('密码', 'Password')}{editId ? t('（留空不修改）', ' (leave empty to keep)') : ''}</span><input type="password" value={form.password} onChange={event => setForm({ ...form, password: event.target.value })} /></label>
          <label className="cloud-field"><span>{t('邮箱/标识', 'Email / identifier')}</span><input value={form.identifier} onChange={event => setForm({ ...form, identifier: event.target.value })} /></label>
          <label className="cloud-field"><span>{t('有效期天数（0=永久）', 'Validity days (0=permanent)')}</span><input type="number" value={form.expiry_days} onChange={event => setForm({ ...form, expiry_days: Number(event.target.value) || 0 })} /></label>
        </div>
        <div className="cloud-actions">
          <button className="cloud-primary" onClick={save} disabled={busy}>{busy ? t('保存中...', 'Saving...') : editId ? t('更新账号', 'Update account') : t('创建账号', 'Create account')}</button>
          {editId ? <button className="btn-secondary" onClick={reset}>{t('取消编辑', 'Cancel edit')}</button> : null}
        </div>
        {msg ? <p className={'cloud-message ' + msg.kind}>{msg.text}</p> : null}
      </SectionCard>

      <SectionCard title={t('CSV 批量导入', 'CSV Import')} desc={t('格式：用户名,密码,邮箱/标识,有效期天数。每行一条，0 表示永久有效。', 'Format: username,password,email/identifier,validity days. One account per line; 0 means permanent.')}>
        <div className="auth-import-row">
          <input type="file" accept=".csv,.txt" onChange={event => setCsvFile(event.target.files?.[0] || null)} />
          <button className="cloud-primary" onClick={importCSV} disabled={!csvFile || busy}>{t('导入账号', 'Import accounts')}</button>
        </div>
      </SectionCard>

      <SectionCard title={t('账号列表（共 ', 'Accounts (') + total + t(' 个）', ')')} desc={t('这些账号用于 iWorker 连接 Center 时认证。', 'These accounts authenticate iWorkers when they connect to Center.')}>
        <div className="data-table-wrap">
          <table className="data-table">
            <thead><tr><th>{t('用户名', 'Username')}</th><th>{t('标识', 'Identifier')}</th><th>{t('有效期', 'Expires')}</th><th>{t('状态', 'Status')}</th><th>{t('操作', 'Actions')}</th></tr></thead>
            <tbody>
              {accounts.map(account => (
                <tr key={account.id}>
                  <td>{account.username}</td>
                  <td>{account.identifier || '-'}</td>
                  <td>{account.expires_at ? new Date(account.expires_at).toLocaleDateString() : t('永久', 'Permanent')}</td>
                  <td><span className={account.disabled ? 'badge warn' : 'badge ok'}>{account.disabled ? t('禁用', 'Disabled') : t('启用', 'Enabled')}</span></td>
                  <td><div className="capability-row-actions"><button className="btn-secondary" onClick={() => edit(account)}>{t('编辑', 'Edit')}</button><button className="btn-secondary" onClick={() => remove(account.id)}>{t('删除', 'Delete')}</button></div></td>
                </tr>
              ))}
              {accounts.length === 0 ? <tr><td colSpan={5} style={{ color: 'var(--muted)', textAlign: 'center' }}>{t('暂无账号', 'No accounts')}</td></tr> : null}
            </tbody>
          </table>
        </div>
      </SectionCard>
    </>
  );
}

function OIDCPanel() {
  const { t } = useI18n();
  const [cfg, setCfg] = useState<OIDCConfig>({ enabled: false, issuer_url: '', client_id: '', client_secret: '', redirect_url: '', scopes: ['openid', 'profile', 'email'], allowed_domains: [] });
  const [scopesText, setScopesText] = useState('openid, profile, email');
  const [domainsText, setDomainsText] = useState('');
  const [msg, setMsg] = useState<Message | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    api('GET', '/admin/diworker-auth/oidc')
      .then(result => {
        if (!result.data) return;
        const next = result.data as OIDCConfig;
        setCfg({ ...next, client_secret: '' });
        setScopesText((next.scopes || []).join(', '));
        setDomainsText((next.allowed_domains || []).join(', '));
      })
      .catch(() => undefined);
  }, []);

  const save = async () => {
    setBusy(true);
    try {
      await api('POST', '/admin/diworker-auth/oidc', {
        ...cfg,
        scopes: parseList(scopesText),
        allowed_domains: parseList(domainsText),
      });
      setMsg({ kind: 'ok', text: t('OIDC/OAuth 适配器配置已保存为预留状态。当前版本不会启用实际登录。', 'OIDC/OAuth adapter settings saved as reserved. This version does not enable live sign-in yet.') });
    } catch (err) {
      setMsg({ kind: 'danger', text: err instanceof Error ? err.message : t('保存失败。', 'Save failed.') });
    } finally {
      setBusy(false);
    }
  };

  return (
    <SectionCard title={t('OIDC / OAuth / 零信任接入', 'OIDC / OAuth / Zero-Trust Access')} desc={t('该适配器预留给企业 SSO、零信任网关或外部身份平台。当前版本可保存配置草案，但仍标记为 reserved，不会参与实际 iWorker 登录。', 'This adapter is reserved for enterprise SSO, zero-trust gateways, or external identity platforms. This version can save a configuration draft, but it remains reserved and does not participate in live iWorker sign-in.')}>
      <div className="cloud-continuity-grid">
        <div className="cloud-continuity-card"><strong>{t('抽象方式', 'Adapter boundary')}</strong><p>{t('认证提供方只负责身份校验。Center 继续负责 iWorker 授权、能力下发、本地业务策略和审计。', 'The identity provider only verifies identity. Center remains responsible for iWorker authorization, capability assignment, local business policy, and audit.')}</p></div>
        <div className="cloud-continuity-card ok"><strong>{t('推荐场景', 'Recommended use')}</strong><p>{t('已经使用企业 SSO、设备信任、网关代理或短期令牌的中大型组织。', 'Medium and large organizations already using enterprise SSO, device trust, gateway proxies, or short-lived tokens.')}</p></div>
      </div>
      <div className="cloud-form-grid">
        <label className="cloud-field"><span>{t('启用草案', 'Enable draft')}</span><select value={cfg.enabled ? 'yes' : 'no'} onChange={event => setCfg({ ...cfg, enabled: event.target.value === 'yes' })}><option value="yes">{t('启用', 'Enabled')}</option><option value="no">{t('停用', 'Disabled')}</option></select></label>
        <label className="cloud-field"><span>Issuer URL</span><input value={cfg.issuer_url} onChange={event => setCfg({ ...cfg, issuer_url: event.target.value })} placeholder="https://idp.example.com" /></label>
        <label className="cloud-field"><span>Client ID</span><input value={cfg.client_id} onChange={event => setCfg({ ...cfg, client_id: event.target.value })} /></label>
        <label className="cloud-field"><span>Client Secret</span><input type="password" value={cfg.client_secret || ''} onChange={event => setCfg({ ...cfg, client_secret: event.target.value })} placeholder={t('留空则保持当前密钥', 'Leave empty to keep current secret')} /></label>
        <label className="cloud-field"><span>Redirect URL</span><input value={cfg.redirect_url} onChange={event => setCfg({ ...cfg, redirect_url: event.target.value })} placeholder="https://center.example.com/auth/oidc/callback" /></label>
        <label className="cloud-field"><span>{t('Scopes', 'Scopes')}</span><input value={scopesText} onChange={event => setScopesText(event.target.value)} placeholder="openid, profile, email" /></label>
        <label className="cloud-field cloud-field-wide"><span>{t('允许邮箱域', 'Allowed email domains')}</span><input value={domainsText} onChange={event => setDomainsText(event.target.value)} placeholder="example.com, example.org" /></label>
      </div>
      <p className="cloud-message warn">{t('状态：预留适配器。配置会保存，便于未来接入 OIDC discovery、回调和 token 校验；当前不会对 iWorker 登录生效。', 'Status: reserved adapter. Settings are saved for future OIDC discovery, callback, and token validation; they do not affect iWorker sign-in yet.')}</p>
      <div className="cloud-actions"><button className="cloud-primary" onClick={save} disabled={busy}>{busy ? t('保存中...', 'Saving...') : t('保存预留配置', 'Save reserved config')}</button></div>
      {msg ? <p className={'cloud-message ' + msg.kind}>{msg.text}</p> : null}
    </SectionCard>
  );
}
