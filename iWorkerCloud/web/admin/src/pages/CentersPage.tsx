import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  listCenters,
  confirmTrial,
  confirmManual,
  disableCenter,
  enableCenter,
  deleteCenter,
  updateCenterIntegration,
  provisionTenant,
  probeCenter,
  type Center,
  type CenterIntegrationPatch,
  type ProvisionTenantRequest,
  type CloudControlMode,
} from '../api/centers';

type IntegrationDraft = CenterIntegrationPatch;
type TenantDraft = ProvisionTenantRequest;

const controlModeLabels: Record<CloudControlMode, string> = {
  cloud_managed: 'Cloud managed',
  self_managed: 'Self managed',
  hybrid: 'Hybrid',
};

function createDraft(center: Center): IntegrationDraft {
  return {
    base_url: center.base_url ?? '',
    supports_multi_tenant: center.supports_multi_tenant ?? true,
    tenant_count: center.tenant_count ?? 0,
    cloud_control_mode: center.cloud_control_mode ?? 'cloud_managed',
    last_sync_status: center.last_sync_status ?? 'configured',
  };
}

function createTenantDraft(): TenantDraft {
  return {
    company_name: '',
    legal_person: '',
    email: '',
    address: '',
    admin_username: 'admin',
    admin_password: '',
  };
}

export function CentersPage() {
  const { t } = useTranslation();
  const [centers, setCenters] = useState<Center[]>([]);
  const [drafts, setDrafts] = useState<Record<string, IntegrationDraft>>({});
  const [tenantDrafts, setTenantDrafts] = useState<Record<string, TenantDraft>>({});
  const [provisioning, setProvisioning] = useState<string | null>(null);
  const [probing, setProbing] = useState<string | null>(null);
  const [provisionResult, setProvisionResult] = useState<Record<string, string>>({});
  const [probeResult, setProbeResult] = useState<Record<string, string>>({});
  const [saving, setSaving] = useState<string | null>(null);
  const [error, setError] = useState<string>('');

  const load = () => {
    setError('');
    listCenters()
      .then(data => {
        const nextCenters = data ?? [];
        setCenters(nextCenters);
        setDrafts(Object.fromEntries(nextCenters.map(center => [center.id, createDraft(center)])));
        setTenantDrafts(prev => Object.fromEntries(nextCenters.map(center => [center.id, prev[center.id] ?? createTenantDraft()])));
      })
      .catch(err => setError(err instanceof Error ? err.message : String(err)));
  };

  useEffect(load, []);

  const patchDraft = (id: string, patch: Partial<IntegrationDraft>) => {
    setDrafts(prev => ({
      ...prev,
      [id]: { ...(prev[id] ?? createDraft(centers.find(center => center.id === id)!)), ...patch },
    }));
  };

  const patchTenantDraft = (id: string, patch: Partial<TenantDraft>) => {
    setTenantDrafts(prev => ({
      ...prev,
      [id]: { ...(prev[id] ?? createTenantDraft()), ...patch },
    }));
  };

  const handleSaveIntegration = async (center: Center) => {
    const draft = drafts[center.id] ?? createDraft(center);
    setSaving(center.id);
    setError('');
    try {
      const updated = await updateCenterIntegration(center.id, {
        ...draft,
        base_url: draft.base_url.trim(),
        tenant_count: Number(draft.tenant_count) || 0,
        last_sync_status: draft.last_sync_status.trim() || 'configured',
      });
      setCenters(prev => prev.map(item => item.id === center.id ? updated : item));
      setDrafts(prev => ({ ...prev, [center.id]: createDraft(updated) }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(null);
    }
  };

  const handleConfirmManual = async (id: string) => {
    const days = prompt('Days (0=long-term):', '30');
    if (days === null) return;
    await confirmManual(id, ['compute'], parseInt(days, 10) || 30);
    load();
  };

  const handleProvisionTenant = async (center: Center) => {
    const draft = tenantDrafts[center.id] ?? createTenantDraft();
    if (!draft.company_name.trim() || !draft.email.trim() || !draft.admin_username.trim() || !draft.admin_password.trim()) {
      setError('company name, email, admin username, and admin password are required');
      return;
    }
    setProvisioning(center.id);
    setError('');
    setProvisionResult(prev => ({ ...prev, [center.id]: '' }));
    try {
      const result = await provisionTenant(center.id, {
        company_name: draft.company_name.trim(),
        legal_person: draft.legal_person.trim(),
        email: draft.email.trim(),
        address: draft.address.trim(),
        admin_username: draft.admin_username.trim(),
        admin_password: draft.admin_password,
      });
      setProvisionResult(prev => ({
        ...prev,
        [center.id]: `Provisioned ${result.tenant_id || 'tenant'}: ${result.message || result.status || 'ok'}`,
      }));
      setTenantDrafts(prev => ({ ...prev, [center.id]: createTenantDraft() }));
      load();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setProvisioning(null);
    }
  };

  const handleProbeCenter = async (center: Center) => {
    setProbing(center.id);
    setError('');
    setProbeResult(prev => ({ ...prev, [center.id]: '' }));
    try {
      const response = await probeCenter(center.id);
      setCenters(prev => prev.map(item => item.id === center.id ? response.center : item));
      setDrafts(prev => ({ ...prev, [center.id]: createDraft(response.center) }));
      setProbeResult(prev => ({
        ...prev,
        [center.id]: `${response.probe.ok ? 'Reachable' : 'Unreachable'}: ${response.probe.message}`,
      }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setProbing(null);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm('Delete?')) return;
    await deleteCenter(id);
    load();
  };

  if (centers.length === 0) return <div className="hint">{t('centers.empty')}</div>;

  return (
    <div className="cloud-center-stack">
      <div className="hint">
        iWorkerCloud does not recreate customer organizations. It directly manages connected multi-tenant iWorkerCenter deployments through their integration endpoint, authorization state, compute distribution, and skill-market entitlement.
      </div>
      {error && <div className="hint danger">{error}</div>}
      <div className="list">
        {centers.map(center => {
          const draft = drafts[center.id] ?? createDraft(center);
          const tenantDraft = tenantDrafts[center.id] ?? createTenantDraft();
          return (
            <div key={center.id} className="item cloud-center-card">
              <div className="item-head">
                <div>
                  <span className="item-title">{center.company_name}</span>
                  <div className="item-meta">ID: {center.id} | {center.admin_email}</div>
                </div>
                <span className={`badge ${center.status === 'active' ? 'ok' : center.status === 'pending' ? 'warn' : 'danger'}`}>
                  {t(`centers.${center.status}`)}
                </span>
              </div>

              <div className="cloud-center-facts">
                <span>Tenants: {center.tenant_count ?? 0}</span>
                <span>{center.supports_multi_tenant ? 'Multi-tenant ready' : 'Single tenant / unknown'}</span>
                <span>{controlModeLabels[center.cloud_control_mode ?? 'cloud_managed']}</span>
                <span>Sync: {center.last_sync_status || 'not configured'}</span>
                {center.created_at && <span>Registered: {new Date(center.created_at).toLocaleString()}</span>}
              </div>

              <div className="cloud-integration-panel">
                <div className="field-span-2">
                  <label>iWorkerCenter Base URL</label>
                  <input
                    value={draft.base_url}
                    placeholder="https://center.example.com"
                    onChange={event => patchDraft(center.id, { base_url: event.target.value })}
                  />
                </div>
                <div>
                  <label>Tenant count</label>
                  <input
                    type="number"
                    min="0"
                    value={draft.tenant_count}
                    onChange={event => patchDraft(center.id, { tenant_count: Number(event.target.value) })}
                  />
                </div>
                <div>
                  <label>Cloud control mode</label>
                  <select
                    value={draft.cloud_control_mode}
                    onChange={event => patchDraft(center.id, { cloud_control_mode: event.target.value as CloudControlMode })}
                  >
                    {Object.entries(controlModeLabels).map(([value, label]) => (
                      <option key={value} value={value}>{label}</option>
                    ))}
                  </select>
                </div>
                <div>
                  <label>Multi-tenant support</label>
                  <select
                    value={draft.supports_multi_tenant ? 'yes' : 'no'}
                    onChange={event => patchDraft(center.id, { supports_multi_tenant: event.target.value === 'yes' })}
                  >
                    <option value="yes">Supported</option>
                    <option value="no">Not yet / unknown</option>
                  </select>
                </div>
                <div>
                  <label>Last sync status</label>
                  <input
                    value={draft.last_sync_status}
                    placeholder="configured / synced / failed"
                    onChange={event => patchDraft(center.id, { last_sync_status: event.target.value })}
                  />
                </div>
              </div>

              <div className="cloud-provision-panel">
                <div className="field-span-2">
                  <label>Provision customer tenant</label>
                  <p>Cloud triggers tenant creation on the connected multi-tenant iWorkerCenter. The customer organization and runtime still live inside that Center.</p>
                </div>
                <div>
                  <label>Company name</label>
                  <input value={tenantDraft.company_name} onChange={event => patchTenantDraft(center.id, { company_name: event.target.value })} />
                </div>
                <div>
                  <label>Legal person</label>
                  <input value={tenantDraft.legal_person} onChange={event => patchTenantDraft(center.id, { legal_person: event.target.value })} />
                </div>
                <div>
                  <label>Admin email</label>
                  <input value={tenantDraft.email} onChange={event => patchTenantDraft(center.id, { email: event.target.value })} />
                </div>
                <div>
                  <label>Address</label>
                  <input value={tenantDraft.address} onChange={event => patchTenantDraft(center.id, { address: event.target.value })} />
                </div>
                <div>
                  <label>Admin username</label>
                  <input value={tenantDraft.admin_username} onChange={event => patchTenantDraft(center.id, { admin_username: event.target.value })} />
                </div>
                <div>
                  <label>Initial password</label>
                  <input type="password" value={tenantDraft.admin_password} onChange={event => patchTenantDraft(center.id, { admin_password: event.target.value })} />
                </div>
                {provisionResult[center.id] && <div className="hint ok field-span-2">{provisionResult[center.id]}</div>}
                {probeResult[center.id] && <div className={`hint ${probeResult[center.id].startsWith('Reachable') ? 'ok' : 'danger'} field-span-2`}>{probeResult[center.id]}</div>}
              </div>

              <div className="actions">
                <button className="btn-primary" disabled={saving === center.id} onClick={() => handleSaveIntegration(center)}>
                  {saving === center.id ? 'Saving...' : 'Save integration'}
                </button>
                <button
                  className="btn-ghost"
                  disabled={probing === center.id || !center.base_url}
                  onClick={() => handleProbeCenter(center)}
                >
                  {probing === center.id ? 'Testing...' : 'Test connection'}
                </button>
                <button
                  className="btn-secondary"
                  disabled={provisioning === center.id || !center.base_url || center.status !== 'active'}
                  onClick={() => handleProvisionTenant(center)}
                >
                  {provisioning === center.id ? 'Provisioning...' : 'Provision tenant'}
                </button>
                {center.status === 'pending' && <>
                  <button className="btn-secondary" onClick={() => { confirmTrial(center.id).then(load); }}>{t('centers.confirmTrial')}</button>
                  <button className="btn-secondary" onClick={() => handleConfirmManual(center.id)}>{t('centers.confirmManual')}</button>
                </>}
                {center.status === 'active' && <button className="btn-danger" onClick={() => { disableCenter(center.id).then(load); }}>{t('centers.disable')}</button>}
                {center.status === 'disabled' && <button className="btn-secondary" onClick={() => { enableCenter(center.id).then(load); }}>{t('centers.enable')}</button>}
                <button className="btn-danger" onClick={() => handleDelete(center.id)}>{t('centers.delete')}</button>
              </div>
            </div>
          );
        })}
      </div>
    </div>
  );
}
