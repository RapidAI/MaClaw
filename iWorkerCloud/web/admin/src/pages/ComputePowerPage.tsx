import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  listProviders, createProvider, updateProvider, deleteProvider, toggleProvider, testProvider,
  listCenterPermissions, toggleCenterPermission, listCenterAssignments, assignProviderToCenter,
  unassignProviderFromCenter, listCenterCosts,
  type LLMProvider, type CenterPermission, type CenterCostRow,
} from '../api/compute';

/* ── tiny helpers ── */
const today = () => new Date().toISOString().slice(0, 10);
const monthAgo = () => { const d = new Date(); d.setMonth(d.getMonth() - 1); return d.toISOString().slice(0, 10); };

const emptyForm = (): Partial<LLMProvider> & { api_key: string } => ({
  name: '', base_url: '', api_key: '', protocol: 'openai', user_agent: 'openclaw',
  compute_type: 'general', model: '', priority: 0, description: '',
  input_price_per_mtoken: 0, output_price_per_mtoken: 0,
});

export function ComputePowerPage() {
  const { t } = useTranslation();
  const [tab, setTab] = useState<'providers' | 'permissions' | 'usage'>('providers');
  const [providers, setProviders] = useState<LLMProvider[]>([]);
  const [permissions, setPermissions] = useState<CenterPermission[]>([]);
  const [assignments, setAssignments] = useState<Record<string, string[]>>({});
  // form
  const [showForm, setShowForm] = useState(false);
  const [editId, setEditId] = useState<string | null>(null);
  const [form, setForm] = useState(emptyForm());
  // usage
  const [period, setPeriod] = useState('daily');
  const [dateStart, setDateStart] = useState(monthAgo());
  const [dateEnd, setDateEnd] = useState(today());
  const [costs, setCosts] = useState<CenterCostRow[]>([]);

  const load = () => {
    Promise.all([
      listProviders().catch(() => []),
      listCenterPermissions().catch(() => []),
    ]).then(([providerRows, centerRows]) => {
      setProviders(providerRows ?? []);
      setPermissions(centerRows ?? []);
      return Promise.all((centerRows ?? []).map(cp =>
        listCenterAssignments(cp.center_id)
          .then(ids => [cp.center_id, ids] as const)
          .catch(() => [cp.center_id, []] as const),
      ));
    }).then(entries => {
      setAssignments(Object.fromEntries(entries));
    }).catch(() => {});
  };
  useEffect(load, []);

  /* ── Provider CRUD ── */
  const openAdd = () => { setEditId(null); setForm(emptyForm()); setShowForm(true); };
  const openEdit = (p: LLMProvider) => {
    setEditId(p.id);
    setForm({ ...p, api_key: '' });
    setShowForm(true);
  };
  const handleSave = async () => {
    try {
      if (editId) await updateProvider(editId, form as any);
      else await createProvider(form as any);
      setShowForm(false); load();
    } catch (e: any) { alert(e.message); }
  };
  const handleDelete = async (id: string) => {
    if (!confirm(t('compute.confirmDelete'))) return;
    await deleteProvider(id).catch(() => {}); load();
  };
  const handleToggle = async (id: string) => { await toggleProvider(id).catch(() => {}); load(); };
  const handleTest = async (id: string) => {
    const r = await testProvider(id).catch(() => ({ ok: false, latency_ms: 0, error: 'failed' }));
    alert(r.ok ? `OK (${r.latency_ms}ms)` : `Failed: ${r.error}`);
  };

  /* ── Permissions ── */
  const handlePermToggle = async (cid: string, cur: boolean) => {
    await toggleCenterPermission(cid, !cur).catch(() => {}); load();
  };
  const handleAssignProvider = async (cid: string, providerId: string) => {
    await assignProviderToCenter(cid, providerId).catch((e: any) => alert(e.message)); load();
  };
  const handleUnassignProvider = async (cid: string, providerId: string) => {
    await unassignProviderFromCenter(cid, providerId).catch((e: any) => alert(e.message)); load();
  };

  /* ── Usage ── */
  const loadCosts = () => {
    listCenterCosts({ period, start: dateStart, end: dateEnd }).then(d => setCosts(d ?? [])).catch(() => {});
  };

  const set = (k: string, v: any) => setForm(f => ({ ...f, [k]: v }));

  return (
    <div>
      {/* Sub-tabs */}
      <div style={{ display: 'flex', gap: 8, marginBottom: 16 }}>
        {(['providers', 'permissions', 'usage'] as const).map(k => (
          <button key={k} className={tab === k ? 'btn-primary' : 'btn-ghost'} onClick={() => setTab(k)}>
            {k === 'providers' ? t('compute.providers') : k === 'permissions' ? t('compute.centerPermissions') : t('compute.usageStats')}
          </button>
        ))}
      </div>

      {/* ═══ Providers Tab ═══ */}
      {tab === 'providers' && (
        <div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
            <h3>{t('compute.providers')}</h3>
            <div style={{ display: 'flex', gap: 8 }}>
              <button className="btn-primary" onClick={openAdd}>{t('compute.addProvider')}</button>
              <button className="btn-ghost" onClick={load}>{t('common.refresh')}</button>
            </div>
          </div>

          {showForm && (
            <div className="card" style={{ padding: 16, marginBottom: 16 }}>
              <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                <div><label>{t('compute.name')}</label><input value={form.name} onChange={e => set('name', e.target.value)} /></div>
                <div><label>{t('compute.protocol')}</label>
                  <select value={form.protocol} onChange={e => set('protocol', e.target.value)}>
                    <option value="openai">OpenAI</option><option value="anthropic">Anthropic</option><option value="gemini">Gemini</option>
                  </select>
                </div>
                <div><label>Base URL</label><input value={form.base_url} onChange={e => set('base_url', e.target.value)} placeholder="https://api.openai.com/v1" /></div>
                <div><label>API Key</label><input type="password" value={form.api_key} onChange={e => set('api_key', e.target.value)} placeholder={editId ? '(leave blank to keep)' : 'sk-...'} /></div>
                <div><label>User-Agent</label><input value={form.user_agent} onChange={e => set('user_agent', e.target.value)} /></div>
                <div><label>{t('compute.computeType')}</label>
                  <select value={form.compute_type} onChange={e => set('compute_type', e.target.value)}>
                    <option value="general">{t('compute.typeGeneral')}</option><option value="coding">{t('compute.typeCoding')}</option><option value="document">{t('compute.typeDocument')}</option><option value="analysis">{t('compute.typeAnalysis')}</option>
                  </select>
                </div>
                <div><label>{t('compute.model')}</label><input value={form.model} onChange={e => set('model', e.target.value)} placeholder="gpt-4" /></div>
                <div><label>{t('compute.priority')}</label><input type="number" value={form.priority} onChange={e => set('priority', +e.target.value)} /></div>
                <div><label>{t('compute.inputPrice')}</label><input type="number" step="0.01" value={form.input_price_per_mtoken} onChange={e => set('input_price_per_mtoken', +e.target.value)} /></div>
                <div><label>{t('compute.outputPrice')}</label><input type="number" step="0.01" value={form.output_price_per_mtoken} onChange={e => set('output_price_per_mtoken', +e.target.value)} /></div>
              </div>
              <div><label>{t('compute.description')}</label><input value={form.description} onChange={e => set('description', e.target.value)} style={{ width: '100%' }} /></div>
              <div style={{ display: 'flex', gap: 8, marginTop: 12 }}>
                <button className="btn-primary" onClick={handleSave}>{t('common.save')}</button>
                <button className="btn-ghost" onClick={() => setShowForm(false)}>{t('common.cancel')}</button>
              </div>
            </div>
          )}

          {providers.length === 0 ? <div className="hint">{t('common.noData')}</div> : (
            <table className="data-table" style={{ width: '100%' }}>
              <thead><tr>
                <th>{t('compute.name')}</th><th>{t('compute.protocol')}</th><th>{t('compute.computeType')}</th>
                <th>{t('compute.model')}</th><th>UA</th><th>{t('compute.inputPrice')}</th><th>{t('compute.outputPrice')}</th>
                <th>{t('compute.status')}</th><th>{t('compute.actions')}</th>
              </tr></thead>
              <tbody>
                {providers.map(p => (
                  <tr key={p.id}>
                    <td>{p.name}</td><td>{p.protocol}</td><td>{p.compute_type}</td>
                    <td>{p.model}</td><td>{p.user_agent}</td>
                    <td>{p.input_price_per_mtoken ?? 0}</td><td>{p.output_price_per_mtoken ?? 0}</td>
                    <td><span className={`badge ${p.enabled ? 'ok' : 'danger'}`}>{p.enabled ? t('compute.enabled') : t('compute.disabled')}</span></td>
                    <td style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                      <button className="btn-ghost" onClick={() => openEdit(p)}>{t('common.edit')}</button>
                      <button className="btn-ghost" onClick={() => handleToggle(p.id)}>{p.enabled ? t('compute.disable') : t('compute.enable')}</button>
                      <button className="btn-ghost" onClick={() => handleTest(p.id)}>{t('compute.test')}</button>
                      <button className="btn-ghost" style={{ color: '#d33' }} onClick={() => handleDelete(p.id)}>{t('common.delete')}</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* Permissions Tab */}
      {tab === 'permissions' && (
        <div>
          <h3>{t('compute.centerPermissions')}</h3>
          {permissions.length === 0 ? <div className="hint">{t('common.noData')}</div> : (
            <table className="data-table" style={{ width: '100%', marginTop: 12 }}>
              <thead><tr><th>Center</th><th>{t('compute.status')}</th><th>{t('compute.providerAssignments')}</th><th>{t('compute.actions')}</th></tr></thead>
              <tbody>
                {permissions.map(cp => {
                  const assignedIds = assignments[cp.center_id] || [];
                  const explicit = assignedIds.length > 0;
                  return (
                    <tr key={cp.center_id}>
                      <td>{cp.center_name || cp.company_name || cp.center_id}</td>
                      <td><span className={`badge ${cp.compute_permission ? 'ok' : 'info'}`}>{cp.compute_permission ? t('compute.selfManaged') : t('compute.cloudManaged')}</span></td>
                      <td>
                        <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
                          <span className="hint" style={{ fontSize: 12 }}>
                            {explicit ? `${assignedIds.length} ${t('compute.assignedProviders')}` : t('compute.defaultAllProviders')}
                          </span>
                          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                            {providers.map(p => {
                              const selected = explicit && assignedIds.includes(p.id);
                              return (
                                <button
                                  key={p.id}
                                  className={selected ? 'btn-primary' : 'btn-ghost'}
                                  style={{ fontSize: 12, padding: '4px 8px' }}
                                  onClick={() => selected ? handleUnassignProvider(cp.center_id, p.id) : handleAssignProvider(cp.center_id, p.id)}
                                  title={explicit ? (selected ? t('compute.unassignProvider') : t('compute.assignProvider')) : t('compute.limitToProvider')}
                                >
                                  {selected ? '* ' : explicit ? '+ ' : ''}{p.name}
                                </button>
                              );
                            })}
                          </div>
                        </div>
                      </td>
                      <td>
                        <button className={cp.compute_permission ? 'btn-ghost' : 'btn-primary'} onClick={() => handlePermToggle(cp.center_id, cp.compute_permission)}>
                          {cp.compute_permission ? t('compute.revokePermission') : t('compute.grantPermission')}
                        </button>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      )}

      {/* ═══ Usage Tab ═══ */}
      {tab === 'usage' && (
        <div>
          <h3>{t('compute.usageStats')}</h3>
          <div style={{ display: 'flex', gap: 12, alignItems: 'flex-end', marginBottom: 16, flexWrap: 'wrap' }}>
            <div><label>{t('compute.period')}</label>
              <select value={period} onChange={e => setPeriod(e.target.value)}>
                <option value="daily">{t('compute.daily')}</option><option value="monthly">{t('compute.monthly')}</option>
              </select>
            </div>
            <div><label>{t('compute.startDate')}</label><input type="date" value={dateStart} onChange={e => setDateStart(e.target.value)} /></div>
            <div><label>{t('compute.endDate')}</label><input type="date" value={dateEnd} onChange={e => setDateEnd(e.target.value)} /></div>
            <button className="btn-primary" onClick={loadCosts}>{t('compute.query')}</button>
          </div>

          {costs.length === 0 ? <div className="hint">{t('compute.selectDateRange')}</div> : (
            <>
              <div className="card" style={{ padding: 12, marginBottom: 12, fontWeight: 600 }}>
                {t('compute.totalCost')}: ¥{costs.reduce((s, c) => s + c.total_cost, 0).toFixed(4)}
                {' | '}{t('compute.totalTokens')}: {costs.reduce((s, c) => s + c.total_tokens, 0).toLocaleString()}
              </div>
              <table className="data-table" style={{ width: '100%' }}>
                <thead><tr>
                  <th>Center</th><th>{t('compute.inputTokens')}</th><th>{t('compute.outputTokens')}</th>
                  <th>{t('compute.totalTokens')}</th><th>{t('compute.inputCost')}</th><th>{t('compute.outputCost')}</th><th>{t('compute.totalCost')}</th>
                </tr></thead>
                <tbody>
                  {costs.map((c, i) => (
                    <tr key={i}>
                      <td>{c.center_name || c.center_id || '-'}</td>
                      <td>{c.total_input_tokens.toLocaleString()}</td><td>{c.total_output_tokens.toLocaleString()}</td>
                      <td>{c.total_tokens.toLocaleString()}</td>
                      <td>¥{c.input_cost.toFixed(4)}</td><td>¥{c.output_cost.toFixed(4)}</td><td>¥{c.total_cost.toFixed(4)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </>
          )}
        </div>
      )}
    </div>
  );
}
