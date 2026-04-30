import { useEffect, useState } from 'react';
import { ApiError } from '../api/client';
import { useTranslation } from 'react-i18next';
import {
  getCenterManagement,
  confirmTrial,
  confirmManual,
  disableCenter,
  enableCenter,
  deleteCenter,
  updateCenterIntegration,
  provisionTenant,
  probeCenter,
  fetchCenterRuntimeSnapshot,
  getProvisionReadiness,
  type Center,
  type CenterManagement,
  type CenterIntegrationPatch,
  type ProvisionTenantRequest,
  type CloudControlMode,
  type CenterProvisionReadiness,
  type CenterProbeResult,
} from '../api/centers';

type IntegrationDraft = CenterIntegrationPatch;
type TenantDraft = ProvisionTenantRequest;
type ProvisionNotReadyBody = {
  error?: string;
  message?: string;
  readiness?: CenterProvisionReadiness;
};

function readinessFromError(err: unknown): CenterProvisionReadiness | null {
  if (!(err instanceof ApiError)) return null;
  const body = err.body as ProvisionNotReadyBody;
  return body?.readiness ?? null;
}


const postureLabels: Record<string, string> = {
  ready: 'Ready',
  needs_setup: 'Needs setup',
  connectivity_risk: 'Connectivity risk',
  commercial_hold: 'Commercial hold',
  watch: 'Watch',
};

const issueLabels: Record<string, string> = {
  center_not_active: 'Center not active',
  missing_base_url: 'Missing base URL',
  multi_tenant_not_confirmed: 'Multi-tenant not confirmed',
  probe_failed: 'Probe failed',
  probe_missing_base_url: 'Probe missing base URL',
  probe_not_iworkercenter: 'Endpoint is not iWorkerCenter service',
  heartbeat_not_iworkercenter: 'Heartbeat identity failed',
  no_active_license: 'No active license',
  service_identity_not_verified: 'Service identity not verified',
};

const controlModeLabels: Record<CloudControlMode, string> = {
  cloud_managed: 'Cloud managed',
  self_managed: 'Self managed',
  hybrid: 'Hybrid',
};

const syncStatusLabels: Record<string, string> = {
  registered: 'Registered',
  configured: 'Configured',
  probe_ok: 'Service probe verified',
  probe_failed: 'Service probe failed',
  probe_missing_base_url: 'Missing service URL',
  probe_not_iworkercenter: 'Wrong service endpoint',
  heartbeat_ok: 'Heartbeat online',
  heartbeat_not_iworkercenter: 'Heartbeat identity failed',
  tenant_provisioned: 'Tenant provisioned',
};

const formatDateTime = (value?: string) => {
  if (!value || value.startsWith('0001-')) return 'No heartbeat yet';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
};

const serviceBadgeClass = (status?: string) => {
  if (status === 'heartbeat_ok' || status === 'probe_ok' || status === 'tenant_provisioned') return 'ok';
  if (status === 'registered' || status === 'configured' || status === 'probe_missing_base_url' || !status) return 'warn';
  return 'danger';
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
  const [management, setManagement] = useState<Record<string, CenterManagement>>({});
  const [drafts, setDrafts] = useState<Record<string, IntegrationDraft>>({});
  const [tenantDrafts, setTenantDrafts] = useState<Record<string, TenantDraft>>({});
  const [provisioning, setProvisioning] = useState<string | null>(null);
  const [probing, setProbing] = useState<string | null>(null);
  const [provisionResult, setProvisionResult] = useState<Record<string, string>>({});
  const [probeResult, setProbeResult] = useState<Record<string, string>>({});
  const [probeRuntime, setProbeRuntime] = useState<Record<string, CenterProbeResult>>({});
  const [runtimeRefreshing, setRuntimeRefreshing] = useState<Record<string, boolean>>({});
  const [readinessResult, setReadinessResult] = useState<Record<string, CenterProvisionReadiness>>({});
  const [checkingReadiness, setCheckingReadiness] = useState<string | null>(null);
  const [saving, setSaving] = useState<string | null>(null);
  const [error, setError] = useState<string>('');

  const refreshRuntimeSnapshots = async (items = centers) => {
    const candidates = items.filter(center => center.base_url && center.status !== 'disabled');
    if (candidates.length === 0) return;
    const refreshingIds = Object.fromEntries(candidates.map(center => [center.id, true]));
    setRuntimeRefreshing(prev => ({ ...prev, ...refreshingIds }));
    try {
      const results = await Promise.all(candidates.map(center =>
        fetchCenterRuntimeSnapshot(center.id)
          .then(snapshot => ({ centerId: center.id, snapshot }))
          .catch(() => null),
      ));
      const nextRuntime: Record<string, CenterProbeResult> = {};
      for (const item of results) {
        if (!item) continue;
        nextRuntime[item.centerId] = item.snapshot;
      }
      if (Object.keys(nextRuntime).length > 0) {
        setProbeRuntime(prev => ({ ...prev, ...nextRuntime }));
      }
    } finally {
      setRuntimeRefreshing(prev => {
        const next = { ...prev };
        for (const center of candidates) delete next[center.id];
        return next;
      });
    }
  };

  const load = () => {
    setError('');
    getCenterManagement()
      .then(report => {
        const nextItems = report.items ?? [];
        const nextCenters = nextItems.map(item => item.center);
        setCenters(nextCenters);
        setManagement(Object.fromEntries(nextItems.map(item => [item.center.id, item])));
        setDrafts(Object.fromEntries(nextCenters.map(center => [center.id, createDraft(center)])));
        setTenantDrafts(prev => Object.fromEntries(nextCenters.map(center => [center.id, prev[center.id] ?? createTenantDraft()])));
        void refreshRuntimeSnapshots(nextCenters);
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
      load();
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
      const readiness = readinessFromError(err);
      if (readiness) {
        setReadinessResult(prev => ({ ...prev, [center.id]: readiness }));
      }
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setProvisioning(null);
    }
  };

  const handleCheckProvisionReadiness = async (center: Center) => {
    setCheckingReadiness(center.id);
    setError('');
    try {
      const readiness = await getProvisionReadiness(center.id);
      setReadinessResult(prev => ({ ...prev, [center.id]: readiness }));
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setCheckingReadiness(null);
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
      setProbeRuntime(prev => ({ ...prev, [center.id]: response.probe }));
      setProbeResult(prev => ({
        ...prev,
        [center.id]: `${response.probe.ok ? 'Verified iWorkerCenter service' : 'Service check failed'}: ${response.probe.message}`,
      }));
      load();
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

  const handleRecommendedAction = async (center: Center, code: string) => {
    switch (code) {
      case 'activate_center':
        await confirmTrial(center.id);
        load();
        return;
      case 'configure_base_url':
      case 'confirm_multi_tenant':
        await handleSaveIntegration(center);
        return;
      case 'test_connection':
      case 'verify_center_service_identity':
        await handleProbeCenter(center);
        return;
      case 'issue_license':
        await handleConfirmManual(center.id);
        return;
      default:
        setError('Fill the required tenant form or use the action buttons below.');
    }
  };

  const recommendedActionLabel = (code: string) => {
    switch (code) {
      case 'activate_center':
        return 'Activate trial';
      case 'configure_base_url':
      case 'confirm_multi_tenant':
        return 'Save integration';
      case 'test_connection':
        return 'Test now';
      case 'verify_center_service_identity':
        return 'Verify identity';
      case 'issue_license':
        return 'Issue license';
      case 'ready_for_service_management':
        return 'Review services';
      default:
        return 'Review';
    }
  };

  const isRecommendedActionDisabled = (center: Center, code: string) => {
    if (code === 'test_connection' || code === 'verify_center_service_identity') return probing === center.id || !center.base_url;
    if (code === 'configure_base_url' || code === 'confirm_multi_tenant') return saving === center.id;
    return false;
  };

  if (centers.length === 0) return <div className="hint">{t('centers.empty')}</div>;

  return (
    <div className="cloud-center-stack">
      <div className="hint">
        iWorkerCloud is our iWorkerCenter management center. It manages connected multi-tenant iWorkerCenter instances for authorization, connectivity, compute distribution, upgrades, and skill entitlement, but it does not participate in customer company management, planning, or enterprise operations.
        {Object.keys(runtimeRefreshing).length > 0 ? ' Refreshing platform runtime snapshots...' : ''}
      </div>
      {error && <div className="hint danger">{error}</div>}
      <div className="list">
        {centers.map(center => {
          const draft = drafts[center.id] ?? createDraft(center);
          const tenantDraft = tenantDrafts[center.id] ?? createTenantDraft();
          const managementItem = management[center.id];
          const provisionReadiness = readinessResult[center.id];
          const runtime = probeRuntime[center.id];
          const isRuntimeRefreshing = runtimeRefreshing[center.id] === true;
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
                <span className={`badge ${serviceBadgeClass(center.last_sync_status)}`}>Service: {syncStatusLabels[center.last_sync_status || ''] ?? center.last_sync_status ?? 'not configured'}</span>
                {managementItem && <span>Commercial: {managementItem.commercial_status}</span>}
                {managementItem && <span>Connectivity: {managementItem.connectivity}</span>}
                <span>Last heartbeat: {formatDateTime(center.last_heartbeat)}</span>
                {center.created_at && <span>Registered: {new Date(center.created_at).toLocaleString()}</span>}
              </div>

              {managementItem && <div className={`cloud-posture-panel ${managementItem.ready ? 'ready' : 'watch'}`}>
                <div>
                  <label>Management readiness</label>
                  <strong>{postureLabels[managementItem.management_posture] ?? managementItem.management_posture}</strong>
                  <span>{managementItem.ready ? 'This Center is ready for iWorkerCenter management services.' : 'This Center needs management-center setup before cloud services are enabled smoothly.'}</span>
                </div>
                <div className="cloud-issue-list">
                  {managementItem.issues.length === 0
                    ? <span className="ok">No blocking issues</span>
                    : managementItem.issues.map(issue => <span key={issue} className="warn">{issueLabels[issue] ?? issue}</span>)}
                </div>
              </div>}

              {runtime && <div className={`cloud-posture-panel ${runtime.ok ? 'ready' : 'watch'}`}>
                <div>
                  <label>Platform runtime snapshot</label>
                  <strong>{runtime.runtime_provider_mode || 'unknown runtime'}</strong>
                  <span>{runtime.message}</span>
                </div>
                <div className="cloud-issue-list">
                  <span className={runtime.compute_source === 'cloud' ? 'ok' : 'warn'}>compute source: {runtime.compute_source || 'unknown'}</span>
                  <span className={runtime.compute_sync_status?.status === 'success' ? 'ok' : 'warn'}>sync: {runtime.compute_sync_status?.status || 'unknown'}</span>
                  <span>runtime providers: {runtime.provider_count ?? 0}</span>
                  <span>cloud providers: {runtime.cloud_provider_count ?? runtime.compute_sync_status?.provider_count ?? 0}</span>
                  <span>{runtime.compute_permission ? 'self-management allowed' : 'cloud-managed'}</span>
                  {runtime.compute_sync_status?.last_sync_at ? <span>last sync: {formatDateTime(runtime.compute_sync_status.last_sync_at)}</span> : null}
                  {runtime.compute_sync_status?.error ? <span className="warn">{runtime.compute_sync_status.error}</span> : null}
                </div>
              </div>}

              {managementItem && <div className="cloud-action-panel">
                <div className="field-span-2">
                  <label>Recommended service-control actions</label>
                </div>
                {(managementItem.recommended_actions ?? []).map(action => (
                  <div key={action.code} className={`cloud-action-card ${action.priority}`}>
                    <span>{action.priority}</span>
                    <strong>{action.label}</strong>
                    <p>{action.description}</p>
                    <button
                      className="btn-ghost cloud-action-button"
                      disabled={isRecommendedActionDisabled(center, action.code)}
                      onClick={() => handleRecommendedAction(center, action.code)}
                    >
                      {recommendedActionLabel(action.code)}
                    </button>
                  </div>
                ))}
              </div>}

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
                    placeholder="configured / probe_ok / heartbeat_ok"
                    onChange={event => patchDraft(center.id, { last_sync_status: event.target.value })}
                  />
                </div>
              </div>

              {provisionReadiness && <div className={`cloud-posture-panel ${provisionReadiness.allowed ? 'ready' : 'watch'}`}>
                <div>
                  <label>Provision gate</label>
                  <strong>{provisionReadiness.allowed ? 'Allowed' : 'Blocked'}</strong>
                  <span>{provisionReadiness.allowed ? 'Cloud may request tenant container creation for this iWorkerCenter.' : 'Cloud will not provision tenants until the blocking issues are resolved.'}</span>
                </div>
                <div className="cloud-issue-list">
                  {provisionReadiness.issues.length === 0
                    ? <span className="ok">No provision blockers</span>
                    : provisionReadiness.issues.map(issue => <span key={issue} className="warn">{issueLabels[issue] ?? issue}</span>)}
                </div>
              </div>}
              <div className="cloud-provision-panel">
                <div className="field-span-2">
                  <label>Provision customer tenant container</label>
                  <p>Cloud may request tenant container creation on a connected multi-tenant iWorkerCenter for system management. Customer organization management and enterprise operations remain inside that iWorkerCenter, not in iWorkerCloud.</p>
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
                {probeResult[center.id] && <div className={`hint ${probeResult[center.id].startsWith('Verified') ? 'ok' : 'danger'} field-span-2`}>{probeResult[center.id]}</div>}
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
                  className="btn-ghost"
                  disabled={isRuntimeRefreshing || !center.base_url}
                  onClick={() => refreshRuntimeSnapshots([center])}
                >
                  {isRuntimeRefreshing ? 'Refreshing runtime...' : 'Refresh runtime'}
                </button>
                <button
                  className="btn-ghost"
                  disabled={checkingReadiness === center.id}
                  onClick={() => handleCheckProvisionReadiness(center)}
                >
                  {checkingReadiness === center.id ? 'Checking...' : 'Check provision gate'}
                </button>
                <button
                  className="btn-secondary"
                  disabled={provisioning === center.id || !center.base_url || center.status !== 'active' || provisionReadiness?.allowed === false}
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


