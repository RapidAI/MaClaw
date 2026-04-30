import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import {
  getComputeStatus, listComputeProviders, testComputeProvider,
  createComputeProvider, updateComputeProvider, deleteComputeProvider,
  syncFromCloud, switchComputeSource, getSyncStatus,
  type ComputeProvider, type ComputeStatus, type ComputeSyncStatus,
} from '../api/compute';

const emptyForm = (): Partial<ComputeProvider> & { api_key: string } => ({
  name: '', base_url: '', api_key: '', protocol: 'openai', user_agent: 'openclaw',
  compute_type: 'general', model: '', priority: 0, description: '',
  input_price_per_mtoken: 0, output_price_per_mtoken: 0,
});

export function ComputePowerPage() {
  const { t } = useTranslation();
  const [status, setStatus] = useState<ComputeStatus | null>(null);
  const [providers, setProviders] = useState<ComputeProvider[]>([]);
  const [syncStatus, setSyncStatus] = useState<ComputeSyncStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [showForm, setShowForm] = useState(false);
  const [editId, setEditId] = useState<string | null>(null);
  const [form, setForm] = useState(emptyForm());

  const load = () => {
    setLoading(true);
    Promise.all([
      getComputeStatus().catch(() => null),
      listComputeProviders().catch(() => []),
      getSyncStatus().catch(() => null),
    ]).then(([s, p, sync]) => {
      setStatus(s);
      setProviders(p);
      setSyncStatus(s?.sync_status || sync);
    }).finally(() => setLoading(false));
  };
  useEffect(load, []);

  const isCloud = !status || status.compute_source === 'cloud';
  const canLocal = status?.compute_permission === true;
  const isLocal = status != null && status.compute_source === 'local';
  const syncTone = syncStatus?.status === 'success' ? '#287a3e' : syncStatus?.status === 'waiting_for_credentials' ? '#b98219' : syncStatus?.status === 'failure' ? '#b92b27' : '#5f7692';
  const syncLabel = syncStatus?.status === 'waiting_for_credentials'
    ? 'Waiting for Cloud registration credentials'
    : syncStatus?.status || 'pending';

  const handleSync = async () => {
    const next = await syncFromCloud().catch(() => null);
    if (next) setSyncStatus(next);
    load();
  };
  const handleSwitch = async (src: 'cloud' | 'local') => { await switchComputeSource(src).catch(() => {}); load(); };
  const handleTest = async (id: string) => {
    const p = providers.find(x => x.id === id);
    const r = await testComputeProvider(id, p).catch(() => ({ ok: false, latency_ms: 0, error: 'failed' }));
    alert(r.ok ? `OK (${r.latency_ms}ms)` : `Failed: ${r.error}`);
  };

  const openAdd = () => { setEditId(null); setForm(emptyForm()); setShowForm(true); };
  const openEdit = (p: ComputeProvider) => { setEditId(p.id); setForm({ ...p, api_key: '' }); setShowForm(true); };
  const set = (k: string, v: any) => setForm(f => ({ ...f, [k]: v }));

  const handleSave = async () => {
    try {
      if (editId) await updateComputeProvider(editId, form as any);
      else await createComputeProvider(form as any);
      setShowForm(false); load();
    } catch (e: any) { alert(e.message); }
  };
  const handleDelete = async (id: string) => {
    if (!confirm(t('compute.confirmDelete'))) return;
    await deleteComputeProvider(id).catch(() => {}); load();
  };

  const rows = providers.map(p => ({
    name: p.name,
    protocol: p.protocol,
    compute_type: p.compute_type || '-',
    user_agent: p.user_agent || '-',
    model: p.model,
    enabled: p.enabled ? '✓' : '✗',
    source: isCloud ? t('compute.fromCloud') : t('compute.local'),
  }));

  return (
    <div className="center-page-stack">
      <SectionCard title={t('compute.title')} desc={loading ? t('common.loading') : undefined}>
        {/* Mode bar */}
        <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap', marginBottom: 16 }}>
          <span className={`badge ${isCloud ? 'info' : 'ok'}`}>
            {isCloud ? t('compute.modeCloud') : t('compute.modeLocal')}
          </span>
          {syncStatus && (
            <span style={{ fontSize: 13, color: syncTone }}>
              Sync: {syncLabel}{syncStatus.provider_count >= 0 ? ` · ${syncStatus.provider_count} providers` : ''}
            </span>
          )}
          {syncStatus?.error && syncStatus.status !== 'success' && (
            <span style={{ fontSize: 13, color: syncTone }}>{syncStatus.error}</span>
          )}
          {status?.last_sync_at && (
            <span style={{ fontSize: 13, color: '#5f7692' }}>{t('compute.lastSync')}: {new Date(status.last_sync_at).toLocaleString()}</span>
          )}
          {isCloud && <button className="btn-ghost" onClick={handleSync} style={{ height: 32, fontSize: 13 }}>{t('compute.syncNow')}</button>}
          {canLocal && isCloud && <button className="btn-ghost" onClick={() => handleSwitch('local')} style={{ height: 32, fontSize: 13 }}>{t('compute.switchLocal')}</button>}
          {isLocal && <button className="btn-ghost" onClick={() => handleSwitch('cloud')} style={{ height: 32, fontSize: 13 }}>{t('compute.switchCloud')}</button>}
          {!canLocal && isCloud && <span style={{ fontSize: 13, color: '#b98219' }}>{t('compute.noPermission')}</span>}
        </div>

        {/* Local mode: add button */}
        {isLocal && canLocal && (
          <div style={{ display: 'flex', justifyContent: 'flex-end', marginBottom: 12 }}>
            <button className="btn-primary" onClick={openAdd}>{t('compute.addProvider')}</button>
          </div>
        )}

        {/* Add/Edit form (local mode only) */}
        {showForm && isLocal && (
          <div className="card" style={{ padding: 16, marginBottom: 16 }}>
            <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <div><label>{t('compute.providerName')}</label><input value={form.name} onChange={e => set('name', e.target.value)} /></div>
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

        {/* Cloud mode: read-only label */}
        {isCloud && providers.length > 0 && (
          <div style={{ fontSize: 13, color: '#5f7692', marginBottom: 8 }}>📡 {t('compute.fromCloud')}</div>
        )}

        {/* Provider table */}
        {isCloud ? (
          <DataTable
            columns={[
              { key: 'name', label: t('compute.providerName') },
              { key: 'protocol', label: t('compute.protocol') },
              { key: 'compute_type', label: t('compute.computeType') },
              { key: 'model', label: t('compute.model') },
              { key: 'user_agent', label: 'User-Agent' },
              { key: 'enabled', label: t('compute.enabled') },
              { key: 'source', label: t('compute.source') },
            ]}
            rows={rows}
          />
        ) : (
          /* Local mode: full table with actions */
          providers.length === 0 ? <div className="hint">{t('common.noData')}</div> : (
            <table className="data-table" style={{ width: '100%' }}>
              <thead><tr>
                <th>{t('compute.providerName')}</th><th>{t('compute.protocol')}</th><th>{t('compute.computeType')}</th>
                <th>{t('compute.model')}</th><th>UA</th><th>{t('compute.enabled')}</th><th>{t('compute.actions')}</th>
              </tr></thead>
              <tbody>
                {providers.map(p => (
                  <tr key={p.id}>
                    <td>{p.name}</td><td>{p.protocol}</td><td>{p.compute_type}</td>
                    <td>{p.model}</td><td>{p.user_agent}</td>
                    <td>{p.enabled ? '✓' : '✗'}</td>
                    <td style={{ display: 'flex', gap: 4, flexWrap: 'wrap' }}>
                      <button className="btn-ghost" onClick={() => openEdit(p)}>{t('common.edit')}</button>
                      <button className="btn-ghost" onClick={() => handleTest(p.id)}>{t('compute.test')}</button>
                      <button className="btn-ghost" style={{ color: '#d33' }} onClick={() => handleDelete(p.id)}>{t('common.delete')}</button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )
        )}
      </SectionCard>
    </div>
  );
}
