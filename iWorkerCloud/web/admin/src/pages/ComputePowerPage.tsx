import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  listProviders, createProvider, updateProvider, deleteProvider, toggleProvider, testProvider,
  listCenterPermissions, toggleCenterPermission, listCenterAssignments, assignProviderToCenter,
  unassignProviderFromCenter, listCenterCosts,
  type LLMProvider, type CenterPermission, type CenterCostRow,
} from '../api/compute';
const today = () => new Date().toISOString().slice(0, 10);
const monthAgo = () => { const d = new Date(); d.setMonth(d.getMonth() - 1); return d.toISOString().slice(0, 10); };
const formatMoney = (value: number) => `CNY ${value.toFixed(4)}`;

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
  const [notice, setNotice] = useState<{ tone: 'ok' | 'danger' | 'info'; text: string } | null>(null);
  const [loading, setLoading] = useState(false);
  const [savingProvider, setSavingProvider] = useState(false);
  const [busyAction, setBusyAction] = useState<string | null>(null);
  const [queryingCosts, setQueryingCosts] = useState(false);

  const load = () => {
    setLoading(true);
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
    }).catch((e: any) => setNotice({ tone: 'danger', text: e?.message || t('common.error') }))
      .finally(() => setLoading(false));
  };
  useEffect(load, []);
  const openAdd = () => { setEditId(null); setForm(emptyForm()); setNotice(null); setShowForm(true); };
  const openEdit = (p: LLMProvider) => {
    setEditId(p.id);
    setForm({ ...p, api_key: '' });
    setNotice(null);
    setShowForm(true);
  };
  const handleSave = async () => {
    if (!String(form.name || '').trim() || !String(form.base_url || '').trim() || !String(form.model || '').trim()) {
      setNotice({ tone: 'danger', text: t('compute.providerRequired') });
      return;
    }
    setSavingProvider(true);
    setNotice(null);
    try {
      if (editId) await updateProvider(editId, form as any);
      else await createProvider(form as any);
      setNotice({ tone: 'ok', text: t('compute.noticeSaved') });
      setShowForm(false);
      load();
    } catch (e: any) {
      setNotice({ tone: 'danger', text: e?.message || t('common.error') });
    } finally {
      setSavingProvider(false);
    }
  };
  const handleDelete = async (id: string) => {
    if (!confirm(t('compute.confirmDelete'))) return;
    setBusyAction(`delete:${id}`);
    setNotice(null);
    try {
      await deleteProvider(id);
      setNotice({ tone: 'ok', text: t('compute.noticeDeleted') });
      load();
    } catch (e: any) {
      setNotice({ tone: 'danger', text: e?.message || t('common.error') });
    } finally {
      setBusyAction(null);
    }
  };
  const handleToggle = async (id: string) => {
    setBusyAction(`toggle:${id}`);
    setNotice(null);
    try {
      await toggleProvider(id);
      setNotice({ tone: 'ok', text: t('compute.noticeUpdated') });
      load();
    } catch (e: any) {
      setNotice({ tone: 'danger', text: e?.message || t('common.error') });
    } finally {
      setBusyAction(null);
    }
  };
  const handleTest = async (id: string) => {
    setBusyAction(`test:${id}`);
    setNotice(null);
    try {
      const r = await testProvider(id).catch(() => ({ ok: false, latency_ms: 0, error: 'failed' }));
      setNotice({ tone: r.ok ? 'ok' : 'danger', text: r.ok ? `${t('compute.testOk')} (${r.latency_ms}ms)` : `${t('compute.testFailed')}: ${r.error}` });
    } finally {
      setBusyAction(null);
    }
  };
  const handlePermToggle = async (cid: string, cur: boolean) => {
    setBusyAction(`perm:${cid}`);
    setNotice(null);
    try {
      await toggleCenterPermission(cid, !cur);
      setNotice({ tone: 'ok', text: t('compute.noticeUpdated') });
      load();
    } catch (e: any) {
      setNotice({ tone: 'danger', text: e?.message || t('common.error') });
    } finally {
      setBusyAction(null);
    }
  };
  const handleAssignProvider = async (cid: string, providerId: string) => {
    setBusyAction(`assign:${cid}:${providerId}`);
    setNotice(null);
    try {
      await assignProviderToCenter(cid, providerId);
      setNotice({ tone: 'ok', text: t('compute.noticeAssigned') });
      load();
    } catch (e: any) {
      setNotice({ tone: 'danger', text: e?.message || t('common.error') });
    } finally {
      setBusyAction(null);
    }
  };
  const handleUnassignProvider = async (cid: string, providerId: string) => {
    setBusyAction(`assign:${cid}:${providerId}`);
    setNotice(null);
    try {
      await unassignProviderFromCenter(cid, providerId);
      setNotice({ tone: 'ok', text: t('compute.noticeAssigned') });
      load();
    } catch (e: any) {
      setNotice({ tone: 'danger', text: e?.message || t('common.error') });
    } finally {
      setBusyAction(null);
    }
  };
  const loadCosts = async () => {
    if (dateStart && dateEnd && dateStart > dateEnd) {
      setNotice({ tone: 'danger', text: t('compute.invalidDateRange') });
      return;
    }
    setQueryingCosts(true);
    setNotice(null);
    try {
      setCosts(await listCenterCosts({ period, start: dateStart, end: dateEnd }) ?? []);
    } catch (e: any) {
      setNotice({ tone: 'danger', text: e?.message || t('compute.queryFailed') });
    } finally {
      setQueryingCosts(false);
    }
  };

  const summary = useMemo(() => {
    const enabledProviders = providers.filter(provider => provider.enabled).length;
    const cloudManaged = permissions.filter(permission => !permission.compute_permission).length;
    const explicitAssignments = Object.values(assignments).filter(ids => ids.length > 0).length;
    const totalCost = costs.reduce((sum, row) => sum + row.total_cost, 0);
    const totalTokens = costs.reduce((sum, row) => sum + row.total_tokens, 0);
    return { enabledProviders, cloudManaged, explicitAssignments, totalCost, totalTokens };
  }, [assignments, costs, permissions, providers]);

  const set = (k: string, v: any) => setForm(f => ({ ...f, [k]: v }));

  return (
    <div className="cloud-page-stack compute-page-stack">
      <section className="cloud-brief card">
        <div>
          <div className="mini">{t('compute.eyebrow')}</div>
          <h3>{t('nav.compute')}</h3>
          <p>{t('compute.positionDesc')}</p>
        </div>
        <div className="cloud-brief-note">
          <strong>{t('compute.boundaryTitle')}</strong>
          <span>{t('compute.boundaryNote')}</span>
        </div>
      </section>

      <div className="metrics cloud-metrics ops-summary-grid">
        <div className="metric"><label>{t('compute.stats.providers')}</label><strong>{providers.length}</strong><span>{summary.enabledProviders} {t('compute.stats.enabledProviders')}</span></div>
        <div className="metric"><label>{t('compute.stats.centers')}</label><strong>{permissions.length}</strong><span>{summary.cloudManaged} {t('compute.stats.cloudManaged')}</span></div>
        <div className="metric"><label>{t('compute.stats.assignments')}</label><strong>{summary.explicitAssignments}</strong><span>{t('compute.stats.assignmentsHint')}</span></div>
        <div className="metric"><label>{t('compute.stats.cost')}</label><strong>{formatMoney(summary.totalCost)}</strong><span>{summary.totalTokens.toLocaleString()} {t('compute.totalTokens')}</span></div>
      </div>

      {notice && <div className={`notice ${notice.tone}`} style={{ marginBottom: 12 }}>{notice.text}</div>}
      <div className="cloud-tabbar">
        {(['providers', 'permissions', 'usage'] as const).map(k => (
          <button key={k} className={tab === k ? 'btn-primary' : 'btn-ghost'} onClick={() => setTab(k)}>
            {k === 'providers' ? t('compute.providers') : k === 'permissions' ? t('compute.centerPermissions') : t('compute.usageStats')}
          </button>
        ))}
      </div>
      {tab === 'providers' && (
        <div>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', marginBottom: 12 }}>
            <h3>{t('compute.providers')}</h3>
            <div style={{ display: 'flex', gap: 8 }}>
              <button className="btn-primary" onClick={openAdd}>{t('compute.addProvider')}</button>
              <button className="btn-ghost" onClick={load}>{loading ? t('common.loading') : t('common.refresh')}</button>
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
                <div><label>{t('compute.baseUrl')}</label><input value={form.base_url} onChange={e => set('base_url', e.target.value)} placeholder="https://api.openai.com/v1" /></div>
                <div><label>{t('compute.apiKey')}</label><input type="password" value={form.api_key} onChange={e => set('api_key', e.target.value)} placeholder={editId ? t('compute.keepApiKey') : 'sk-...'} /></div>
                <div><label>{t('compute.userAgent')}</label><input value={form.user_agent} onChange={e => set('user_agent', e.target.value)} /></div>
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
                <button className="btn-primary" disabled={savingProvider} onClick={handleSave}>{savingProvider ? t('common.loading') : t('common.save')}</button>
                <button className="btn-ghost" onClick={() => setShowForm(false)}>{t('common.cancel')}</button>
              </div>
            </div>
          )}

          {providers.length === 0 ? <div className="hint">{t('common.noData')}</div> : (
            <table className="data-table" style={{ width: '100%' }}>
              <thead><tr>
                <th>{t('compute.name')}</th><th>{t('compute.protocol')}</th><th>{t('compute.computeType')}</th>
                <th>{t('compute.model')}</th><th>{t('compute.userAgentShort')}</th><th>{t('compute.inputPrice')}</th><th>{t('compute.outputPrice')}</th>
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
                      <button className="btn-ghost" disabled={!!busyAction} onClick={() => openEdit(p)}>{t('common.edit')}</button>
                      <button className="btn-ghost" disabled={!!busyAction} onClick={() => handleToggle(p.id)}>{busyAction === `toggle:${p.id}` ? t('common.loading') : p.enabled ? t('compute.disable') : t('compute.enable')}</button>
                      <button className="btn-ghost" disabled={!!busyAction} onClick={() => handleTest(p.id)}>{busyAction === `test:${p.id}` ? t('common.loading') : t('compute.test')}</button>
                      <button className="btn-ghost" disabled={!!busyAction} style={{ color: '#d33' }} onClick={() => handleDelete(p.id)}>{busyAction === `delete:${p.id}` ? t('common.loading') : t('common.delete')}</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </div>
      )}
      {tab === 'permissions' && (
        <div>
          <h3>{t('compute.centerPermissions')}</h3>
          {permissions.length === 0 ? <div className="hint">{t('common.noData')}</div> : (
            <table className="data-table" style={{ width: '100%', marginTop: 12 }}>
              <thead><tr><th>{t('nav.centers')}</th><th>{t('compute.status')}</th><th>{t('compute.providerAssignments')}</th><th>{t('compute.actions')}</th></tr></thead>
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
                                  disabled={!!busyAction}
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
                        <button className={cp.compute_permission ? 'btn-ghost' : 'btn-primary'} disabled={!!busyAction} onClick={() => handlePermToggle(cp.center_id, cp.compute_permission)}>
                          {busyAction === `perm:${cp.center_id}` ? t('common.loading') : cp.compute_permission ? t('compute.revokePermission') : t('compute.grantPermission')}
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
            <button className="btn-primary" disabled={queryingCosts} onClick={loadCosts}>{queryingCosts ? t('common.loading') : t('compute.query')}</button>
          </div>

          {costs.length === 0 ? <div className="hint">{t('compute.selectDateRange')}</div> : (
            <>
              <div className="card" style={{ padding: 12, marginBottom: 12, fontWeight: 600 }}>
                {t('compute.totalCost')}: {formatMoney(costs.reduce((s, c) => s + c.total_cost, 0))}
                {' | '}{t('compute.totalTokens')}: {costs.reduce((s, c) => s + c.total_tokens, 0).toLocaleString()}
              </div>
              <table className="data-table" style={{ width: '100%' }}>
                <thead><tr>
                  <th>{t('nav.centers')}</th><th>{t('compute.inputTokens')}</th><th>{t('compute.outputTokens')}</th>
                  <th>{t('compute.totalTokens')}</th><th>{t('compute.inputCost')}</th><th>{t('compute.outputCost')}</th><th>{t('compute.totalCost')}</th>
                </tr></thead>
                <tbody>
                  {costs.map((c, i) => (
                    <tr key={i}>
                      <td>{c.center_name || c.center_id || '-'}</td>
                      <td>{c.total_input_tokens.toLocaleString()}</td><td>{c.total_output_tokens.toLocaleString()}</td>
                      <td>{c.total_tokens.toLocaleString()}</td>
                      <td>{formatMoney(c.input_cost)}</td><td>{formatMoney(c.output_cost)}</td><td>{formatMoney(c.total_cost)}</td>
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
