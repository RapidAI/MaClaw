import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import {
  createDiWorkerAccount,
  deleteDiWorkerAccount,
  importDiWorkerAccounts,
  listDiWorkerAccounts,
  listDiWorkerAuthMethods,
  loadLDAPConfig,
  loadOIDCConfig,
  saveLDAPConfig,
  saveOIDCConfig,
  testLDAPLogin,
  updateDiWorkerAccount,
  type DiWorkerAccount,
  type DiWorkerAuthMethodStatus,
  type DiWorkerLDAPConfig,
  type DiWorkerOIDCConfig,
} from '../api/diworkerAuth';

const defaultLDAPConfig: DiWorkerLDAPConfig = {
  enabled: false,
  host: '',
  port: 389,
  use_tls: false,
  base_dn: '',
  bind_fmt: '{user}@example.com',
};

const defaultOIDCConfig: DiWorkerOIDCConfig = {
  enabled: false,
  issuer_url: '',
  client_id: '',
  client_secret: '',
  redirect_url: '',
  scopes: ['openid', 'profile', 'email'],
  allowed_domains: [],
};

export function AuthPage() {
  const { t } = useTranslation();
  const [ldap, setLDAP] = useState<DiWorkerLDAPConfig>(defaultLDAPConfig);
  const [oidc, setOIDC] = useState<DiWorkerOIDCConfig>(defaultOIDCConfig);
  const [methods, setMethods] = useState<DiWorkerAuthMethodStatus[]>([]);
  const [accounts, setAccounts] = useState<DiWorkerAccount[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [savingLDAP, setSavingLDAP] = useState(false);
  const [savingOIDC, setSavingOIDC] = useState(false);
  const [accountSaving, setAccountSaving] = useState(false);
  const [message, setMessage] = useState('');
  const [error, setError] = useState('');
  const [ldapTest, setLDAPTest] = useState({ username: '', password: '' });
  const [draft, setDraft] = useState({ username: '', password: '', identifier: '', expiryDays: '' });
  const [importText, setImportText] = useState('');
  const [importing, setImporting] = useState(false);

  const refreshAccounts = async () => {
    const next = await listDiWorkerAccounts(200, 0);
    setAccounts(next.items || []);
    setTotal(next.total || 0);
  };

  useEffect(() => {
    let cancelled = false;
    Promise.all([loadLDAPConfig().catch(() => defaultLDAPConfig), loadOIDCConfig().catch(() => defaultOIDCConfig), listDiWorkerAuthMethods().catch(() => ({ methods: [] })), listDiWorkerAccounts(200, 0)])
      .then(([ldapConfig, oidcConfig, methodList, accountList]) => {
        if (cancelled) return;
        setLDAP({ ...defaultLDAPConfig, ...ldapConfig });
        setOIDC({ ...defaultOIDCConfig, ...oidcConfig, client_secret: '' });
        setMethods(methodList.methods || []);
        setAccounts(accountList.items || []);
        setTotal(accountList.total || 0);
      })
      .catch((err: Error) => {
        if (!cancelled) setError(err.message || t('common.error'));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => { cancelled = true; };
  }, [t]);

  const handleSaveLDAP = async () => {
    setSavingLDAP(true);
    setMessage('');
    setError('');
    try {
      await saveLDAPConfig({ ...ldap, port: Number(ldap.port) || 389 });
      setMessage('LDAP settings saved.');
    } catch (err: any) {
      setError(err.message || t('common.error'));
    } finally {
      setSavingLDAP(false);
    }
  };

  const refreshMethods = async () => {
    const next = await listDiWorkerAuthMethods();
    setMethods(next.methods || []);
  };

  const handleSaveOIDC = async () => {
    setSavingOIDC(true);
    setMessage('');
    setError('');
    try {
      await saveOIDCConfig(oidc);
      await refreshMethods();
      setMessage('OIDC/OAuth provider saved as reserved. Runtime verification will be enabled when the adapter is implemented.');
    } catch (err: any) {
      setError(err.message || t('common.error'));
    } finally {
      setSavingOIDC(false);
    }
  };
  const handleTestLDAP = async () => {
    setMessage('');
    setError('');
    try {
      const result = await testLDAPLogin(ldapTest.username, ldapTest.password);
      if (result.success) {
        setMessage('LDAP test succeeded.');
      } else {
        setError(result.error || 'LDAP test failed.');
      }
    } catch (err: any) {
      setError(err.message || t('common.error'));
    }
  };

  const handleCreateAccount = async () => {
    if (!draft.username.trim() || !draft.password.trim()) {
      setError('Username and password are required.');
      return;
    }
    setAccountSaving(true);
    setMessage('');
    setError('');
    try {
      await createDiWorkerAccount({
        username: draft.username.trim(),
        password: draft.password,
        identifier: draft.identifier.trim(),
        expiry_days: Number(draft.expiryDays) || 0,
      });
      setDraft({ username: '', password: '', identifier: '', expiryDays: '' });
      await refreshAccounts();
      setMessage('Local account created.');
    } catch (err: any) {
      setError(err.message || t('common.error'));
    } finally {
      setAccountSaving(false);
    }
  };

  const handleImportAccounts = async () => {
    const csvText = importText.trim();
    if (!csvText) {
      setError('Paste CSV first. Format: username,password,identifier,expiry_days');
      return;
    }
    setImporting(true);
    setMessage('');
    setError('');
    try {
      const result = await importDiWorkerAccounts(csvText);
      await refreshAccounts();
      setImportText('');
      const errors = result.errors?.length ? ` ${result.errors.slice(0, 3).join(' / ')}` : '';
      setMessage(`Imported ${result.created} account${result.created === 1 ? '' : 's'}, skipped ${result.skipped}.${errors}`);
    } catch (err: any) {
      setError(err.message || t('common.error'));
    } finally {
      setImporting(false);
    }
  };
  const handleToggleAccount = async (account: DiWorkerAccount) => {
    setMessage('');
    setError('');
    try {
      await updateDiWorkerAccount(account.id, { disabled: !account.disabled });
      await refreshAccounts();
      setMessage(account.disabled ? 'Account enabled.' : 'Account disabled.');
    } catch (err: any) {
      setError(err.message || t('common.error'));
    }
  };

  const handleDeleteAccount = async (account: DiWorkerAccount) => {
    if (!window.confirm(`Delete account ${account.username}?`)) return;
    setMessage('');
    setError('');
    try {
      await deleteDiWorkerAccount(account.id);
      await refreshAccounts();
      setMessage('Account deleted.');
    } catch (err: any) {
      setError(err.message || t('common.error'));
    }
  };

  if (loading) return <div style={{ padding: 24, color: '#64748b' }}>{t('common.loading')}</div>;

  return (
    <div className="center-page-stack">
      <SectionCard title={t('nav.auth')} desc="Prepare the human identity used when a desktop iWorker binds to this Center.">
        <div style={noticeStyle}>
          <strong>Enrollment rule</strong>
          <span>For local accounts, put allowed worker IDs in Identifier, separated by comma, semicolon, or space. Use * to allow any worker. LDAP uses the username with the same matching rule.</span>
        </div>
        <div style={statusStyle}>
          <span>Total local accounts: {total}</span>
          <span>LDAP: {ldap.enabled ? 'enabled' : 'disabled'}</span>
        </div>
        {message ? <p style={successStyle}>{message}</p> : null}
        {error ? <p style={errorStyle}>{error}</p> : null}
      </SectionCard>

      <SectionCard title="LDAP" desc="Optional enterprise directory authentication for iWorker enrollment.">
        <div style={formGridStyle}>
          <label style={checkboxStyle}>
            <input type="checkbox" checked={ldap.enabled} onChange={(event) => setLDAP({ ...ldap, enabled: event.target.checked })} />
            Enabled
          </label>
          <Field label="Host" value={ldap.host} onChange={(value) => setLDAP({ ...ldap, host: value })} placeholder="ldap.example.com" />
          <Field label="Port" value={String(ldap.port || '')} onChange={(value) => setLDAP({ ...ldap, port: Number(value) || 0 })} placeholder="389" />
          <label style={checkboxStyle}>
            <input type="checkbox" checked={ldap.use_tls} onChange={(event) => setLDAP({ ...ldap, use_tls: event.target.checked, port: event.target.checked && ldap.port === 389 ? 636 : ldap.port })} />
            Use TLS
          </label>
          <Field label="Base DN" value={ldap.base_dn} onChange={(value) => setLDAP({ ...ldap, base_dn: value })} placeholder="dc=example,dc=com" />
          <Field label="Bind format" value={ldap.bind_fmt} onChange={(value) => setLDAP({ ...ldap, bind_fmt: value })} placeholder="{user}@example.com" />
        </div>
        <div style={actionsStyle}>
          <button style={primaryButtonStyle} onClick={handleSaveLDAP} disabled={savingLDAP}>{savingLDAP ? 'Saving...' : 'Save LDAP'}</button>
        </div>
        <div style={testGridStyle}>
          <Field label="Test username" value={ldapTest.username} onChange={(value) => setLDAPTest({ ...ldapTest, username: value })} />
          <Field label="Test password" value={ldapTest.password} onChange={(value) => setLDAPTest({ ...ldapTest, password: value })} password />
          <button style={secondaryButtonStyle} onClick={handleTestLDAP} disabled={!ldapTest.username.trim() || !ldapTest.password.trim()}>Test LDAP</button>
        </div>
      </SectionCard>

            <SectionCard title="OIDC / OAuth SSO" desc="Reserved adapter for zero-trust identity providers. Save the intended provider shape now; runtime verification remains disabled until the adapter is implemented.">
        <div style={formGridStyle}>
          <label style={checkboxStyle}>
            <input type="checkbox" checked={oidc.enabled} onChange={(event) => setOIDC({ ...oidc, enabled: event.target.checked })} />
            Reserve this provider
          </label>
          <Field label="Issuer URL" value={oidc.issuer_url} onChange={(value) => setOIDC({ ...oidc, issuer_url: value })} placeholder="https://idp.example.com" />
          <Field label="Client ID" value={oidc.client_id} onChange={(value) => setOIDC({ ...oidc, client_id: value })} placeholder="iworker-center" />
          <Field label="Client secret" value={oidc.client_secret || ''} onChange={(value) => setOIDC({ ...oidc, client_secret: value })} placeholder="Leave blank to keep existing" password />
          <Field label="Redirect URL" value={oidc.redirect_url} onChange={(value) => setOIDC({ ...oidc, redirect_url: value })} placeholder="https://center.example.com/diworker-auth/oidc/callback" />
          <Field label="Scopes" value={oidc.scopes.join(' ')} onChange={(value) => setOIDC({ ...oidc, scopes: value.split(/[\s,]+/).map((item) => item.trim()).filter(Boolean) })} placeholder="openid profile email" />
          <Field label="Allowed domains" value={oidc.allowed_domains.join(', ')} onChange={(value) => setOIDC({ ...oidc, allowed_domains: value.split(/[，,]/).map((item) => item.trim()).filter(Boolean) })} placeholder="example.com" />
        </div>
        <div style={actionsStyle}>
          <button style={secondaryButtonStyle} onClick={handleSaveOIDC} disabled={savingOIDC}>{savingOIDC ? 'Saving...' : 'Save Reserved Provider'}</button>
          <span style={hintStyle}>Enrollment method aliases oauth/oauth2/sso are normalized to oidc.</span>
        </div>
      </SectionCard>
      <SectionCard title="Local Accounts" desc="Create accounts that can approve iWorker desktop enrollment.">
        <div style={formGridStyle}>
          <Field label="Username" value={draft.username} onChange={(value) => setDraft({ ...draft, username: value })} placeholder="alice" />
          <Field label="Password" value={draft.password} onChange={(value) => setDraft({ ...draft, password: value })} password />
          <Field label="Identifier / allowed workers" value={draft.identifier} onChange={(value) => setDraft({ ...draft, identifier: value })} placeholder="worker-ops or *" />
          <Field label="Expiry days" value={draft.expiryDays} onChange={(value) => setDraft({ ...draft, expiryDays: value })} placeholder="0 = never" />
        </div>
        <div style={actionsStyle}>
          <button style={primaryButtonStyle} onClick={handleCreateAccount} disabled={accountSaving}>{accountSaving ? 'Creating...' : 'Create Account'}</button>
        </div>
        <div style={importBoxStyle}>
          <label style={fieldStyle}>
            Import CSV
            <textarea value={importText} onChange={(event) => setImportText(event.target.value)} placeholder={'username,password,identifier,expiry_days\nalice,Secret123,worker-ops,0\nbob,Secret123,*,30'} style={textareaStyle} />
          </label>
          <div style={actionsStyle}>
            <button style={secondaryButtonStyle} onClick={handleImportAccounts} disabled={importing || !importText.trim()}>{importing ? 'Importing...' : 'Import Accounts'}</button>
            <span style={hintStyle}>Identifier limits which iWorker can be bound. Use * for any worker.</span>
          </div>
        </div>
        <div style={tableWrapStyle}>
          <table style={tableStyle}>
            <thead>
              <tr>
                <th style={thStyle}>Username</th>
                <th style={thStyle}>Identifier</th>
                <th style={thStyle}>Expires</th>
                <th style={thStyle}>Status</th>
                <th style={thStyle}>Actions</th>
              </tr>
            </thead>
            <tbody>
              {accounts.map((account) => (
                <tr key={account.id}>
                  <td style={tdStyle}>{account.username}</td>
                  <td style={tdStyle}>{account.identifier || '-'}</td>
                  <td style={tdStyle}>{account.expires_at ? new Date(account.expires_at).toLocaleDateString() : 'Never'}</td>
                  <td style={tdStyle}>{account.disabled ? 'Disabled' : 'Active'}</td>
                  <td style={tdStyle}>
                    <div style={rowActionsStyle}>
                      <button style={secondaryButtonStyle} onClick={() => handleToggleAccount(account)}>{account.disabled ? 'Enable' : 'Disable'}</button>
                      <button style={dangerButtonStyle} onClick={() => handleDeleteAccount(account)}>Delete</button>
                    </div>
                  </td>
                </tr>
              ))}
              {accounts.length === 0 ? (
                <tr><td style={emptyStyle} colSpan={5}>No local accounts yet.</td></tr>
              ) : null}
            </tbody>
          </table>
        </div>
      </SectionCard>
    </div>
  );
}

function Field({ label, value, onChange, placeholder, password }: { label: string; value: string; onChange: (value: string) => void; placeholder?: string; password?: boolean }) {
  return (
    <label style={fieldStyle}>
      <span>{label}</span>
      <input type={password ? 'password' : 'text'} value={value} onChange={(event) => onChange(event.target.value)} placeholder={placeholder} style={inputStyle} />
    </label>
  );
}

const noticeStyle = { display: 'grid', gap: 4, padding: 12, border: '1px solid #dbe7f3', borderRadius: 8, background: '#f7fbff', color: '#334155', fontSize: 13 };
const statusStyle = { display: 'flex', gap: 10, flexWrap: 'wrap' as const, marginTop: 12, color: '#64748b', fontSize: 13 };
const methodGridStyle = { display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: 10, marginTop: 12 };
const methodCardStyle = { display: 'grid', gap: 4, padding: 10, border: '1px solid #e2e8f0', borderRadius: 8, background: '#ffffff', color: '#334155', fontSize: 12 };
const successStyle = { margin: '12px 0 0', color: '#166534', fontSize: 13 };
const errorStyle = { margin: '12px 0 0', color: '#b91c1c', fontSize: 13 };
const formGridStyle = { display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: 12 };
const testGridStyle = { display: 'grid', gridTemplateColumns: 'minmax(0, 1fr) minmax(0, 1fr) auto', gap: 12, alignItems: 'end', marginTop: 12 };
const fieldStyle = { display: 'grid', gap: 5, color: '#475569', fontSize: 12, fontWeight: 700 };
const inputStyle = { width: '100%', minHeight: 36, padding: '7px 10px', border: '1px solid #cbd5e1', borderRadius: 6, fontSize: 14, boxSizing: 'border-box' as const };
const checkboxStyle = { minHeight: 36, display: 'flex', alignItems: 'center', gap: 8, color: '#475569', fontSize: 13, fontWeight: 700 };
const actionsStyle = { display: 'flex', gap: 10, marginTop: 14, alignItems: 'center', flexWrap: 'wrap' as const };
const importBoxStyle = { display: 'grid', gap: 10, marginTop: 16, padding: 12, border: '1px solid #e2e8f0', borderRadius: 8, background: '#fbfdff' };
const textareaStyle = { ...inputStyle, minHeight: 92, resize: 'vertical' as const, fontFamily: 'ui-monospace, SFMono-Regular, Menlo, Consolas, monospace' };
const hintStyle = { color: '#64748b', fontSize: 12 };
const primaryButtonStyle = { minHeight: 36, padding: '0 16px', border: 'none', borderRadius: 7, background: '#111827', color: '#fff', fontWeight: 700, cursor: 'pointer' };
const secondaryButtonStyle = { minHeight: 32, padding: '0 12px', border: '1px solid #cbd5e1', borderRadius: 7, background: '#fff', color: '#334155', fontWeight: 700, cursor: 'pointer' };
const dangerButtonStyle = { ...secondaryButtonStyle, color: '#b91c1c', border: '1px solid #fecaca' };
const tableWrapStyle = { overflowX: 'auto' as const, marginTop: 16, border: '1px solid #e2e8f0', borderRadius: 8 };
const tableStyle = { width: '100%', borderCollapse: 'collapse' as const, fontSize: 13 };
const thStyle = { padding: '10px 12px', textAlign: 'left' as const, color: '#475569', background: '#f8fafc', borderBottom: '1px solid #e2e8f0' };
const tdStyle = { padding: '10px 12px', borderBottom: '1px solid #edf2f7', color: '#334155', verticalAlign: 'middle' as const };
const rowActionsStyle = { display: 'flex', gap: 8, flexWrap: 'wrap' as const };
const emptyStyle = { ...tdStyle, textAlign: 'center' as const, color: '#94a3b8' };
