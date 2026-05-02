import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import {
  getCenterManagement,
  confirmTrial,
  confirmManual,
  disableCenter,
  enableCenter,
  deleteCenter,
  updateCenterIntegration,
  probeCenter,
  fetchCenterRuntimeSnapshot,
  getServiceReadiness,
  type Center,
  type CenterManagement,
  type CenterIntegrationPatch,
  type CloudControlMode,
  type CenterServiceReadiness,
  type CenterProbeResult,
} from '../api/centers';

type IntegrationDraft = CenterIntegrationPatch;
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
};

const formatDateTime = (value?: string) => {
  if (!value || value.startsWith('0001-')) return 'No heartbeat yet';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
};

const serviceBadgeClass = (status?: string) => {
  if (status === 'heartbeat_ok' || status === 'probe_ok') return 'ok';
  if (status === 'registered' || status === 'configured' || status === 'probe_missing_base_url' || !status) return 'warn';
  return 'danger';
};

function createDraft(center: Center): IntegrationDraft {
  return {
    base_url: center.base_url ?? '',
    cloud_control_mode: center.cloud_control_mode ?? 'cloud_managed',
    last_sync_status: center.last_sync_status ?? 'configured',
  };
}


export function CentersPage() {
  const { t } = useTranslation();
  const [centers, setCenters] = useState<Center[]>([]);
  const [management, setManagement] = useState<Record<string, CenterManagement>>({});
  const [drafts, setDrafts] = useState<Record<string, IntegrationDraft>>({});
  const [probing, setProbing] = useState<string | null>(null);
  const [probeRuntime, setProbeRuntime] = useState<Record<string, CenterProbeResult>>({});
  const [runtimeRefreshing, setRuntimeRefreshing] = useState<Record<string, boolean>>({});
  const [readinessResult, setReadinessResult] = useState<Record<string, CenterServiceReadiness>>({});
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


  const handleSaveIntegration = async (center: Center) => {
    const draft = drafts[center.id] ?? createDraft(center);
    setSaving(center.id);
    setError('');
    try {
      const updated = await updateCenterIntegration(center.id, {
        ...draft,
        base_url: draft.base_url.trim(),
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


  const handleCheckServiceReadiness = async (center: Center) => {
    setCheckingReadiness(center.id);
    setError('');
    try {
      const readiness = await getServiceReadiness(center.id);
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
    try {
      const response = await probeCenter(center.id);
      setCenters(prev => prev.map(item => item.id === center.id ? response.center : item));
      setDrafts(prev => ({ ...prev, [center.id]: createDraft(response.center) }));
      setProbeRuntime(prev => ({ ...prev, [center.id]: response.probe }));
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
        setError('Use the service action buttons below.');
    }
  };

  const recommendedActionLabel = (code: string) => {
    switch (code) {
      case 'activate_center':
        return 'Activate trial';
      case 'configure_base_url':
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
    if (code === 'configure_base_url') return saving === center.id;
    return false;
  };

  if (centers.length === 0) return <div className="hint">{t('centers.empty')}</div>;

  return (
    <div className="cloud-center-stack">
      <div className="hint">
        iWorkerCloud is our iWorkerCenter management center. It manages connected iWorkerCenter service instances for authorization, connectivity, compute distribution, upgrades, and skill entitlement, but it does not participate in customer company management, tenant administration, planning, or enterprise operations.
        {Object.keys(runtimeRefreshing).length > 0 ? ' Refreshing platform runtime snapshots...' : ''}
      </div>
      {error && <div className="hint danger">{error}</div>}
      <div className="list">
        {centers.map(center => {
          const draft = drafts[center.id] ?? createDraft(center);
          const managementItem = management[center.id];
          const serviceReadiness = readinessResult[center.id];
          const runtime = probeRuntime[center.id];
          const workload = managementItem?.iworker_readiness?.workload_summary;
          const isRuntimeRefreshing = runtimeRefreshing[center.id] === true;
          return (
            <div key={center.id} className="item cloud-center-card">
              <div className="item-head">
                <div>
                  <span className="item-title">{center.company_name}</span>
                  <div className="item-meta">ID: {center.id}</div>
                </div>
                <span className={`badge ${center.status === 'active' ? 'ok' : center.status === 'pending' ? 'warn' : 'danger'}`}>
                  {t(`centers.${center.status}`)}
                </span>
              </div>

              <div className="cloud-center-facts">
                <span>{controlModeLabels[center.cloud_control_mode ?? 'cloud_managed']}</span>
                <span className={`badge ${serviceBadgeClass(center.last_sync_status)}`}>Service: {syncStatusLabels[center.last_sync_status || ''] ?? center.last_sync_status ?? 'not configured'}</span>
                {managementItem && <span>Commercial: {managementItem.commercial_status}</span>}
                {managementItem && <span>Connectivity: {managementItem.connectivity}</span>}
                <span className={`badge ${center.iworker_ready ? 'ok' : center.iworker_readiness_status ? 'warn' : 'warn'}`}>iWorker: {center.iworker_readiness_status || 'not reported'}</span>
                <span>Agent instances: {workload?.agent_instance_count ?? center.iworker_agent_instance_count ?? managementItem?.iworker_readiness?.agent_instance_count ?? 0}</span>
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

              {managementItem && <div className={`cloud-posture-panel ${managementItem.iworker_operational_ready ? 'ready' : 'watch'}`}>
                <div>
                  <label>iWorker operating readiness</label>
                  <strong>{managementItem.iworker_operational_ready ? 'Ready for pushed work' : center.iworker_readiness_status ? 'Needs iWorker setup' : 'Waiting for heartbeat'}</strong>
                  <span>{managementItem.iworker_operational_ready ? 'This Center reports that iWorker can receive Center-pushed work and collaborate with human employees.' : 'Cloud only tracks platform readiness signals reported by Center, such as agent runtime, GoalWatch, and aggregate workload status. Customer business setup stays inside iWorkerCenter.'}</span>
                </div>
                <div className="cloud-issue-list">
                  <span className={(managementItem.iworker_readiness?.agent_runtime_ready ?? false) ? 'ok' : 'warn'}>agent runtime: {(managementItem.iworker_readiness?.agent_runtime_ready ?? false) ? 'ready' : 'not ready'}</span>
                  <span className={(managementItem.iworker_readiness?.goalwatch_ready ?? false) ? 'ok' : 'warn'}>GoalWatch: {(managementItem.iworker_readiness?.goalwatch_ready ?? false) ? 'ready' : 'not ready'}</span>
                  <span>agent instances: {workload?.agent_instance_count ?? center.iworker_agent_instance_count ?? managementItem.iworker_readiness?.agent_instance_count ?? 0}</span>
                  {workload ? <>
                    <span className={workload.active_count > 0 ? 'ok' : 'warn'}>active: {workload.active_count}</span>
                    <span>completed: {workload.completed_count}</span>
                    <span className={workload.review_count > 0 ? 'warn' : 'ok'}>review: {workload.review_count}</span>
                    <span className={workload.blocked_count > 0 ? 'danger' : 'ok'}>blocked: {workload.blocked_count}</span>
                    {workload.updated_at ? <span>workload sync: {formatDateTime(workload.updated_at)}</span> : null}
                  </> : null}
                  <span className={center.iworker_ready ? 'ok' : 'warn'}>status: {center.iworker_readiness_status || 'not reported'}</span>
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
                  <label>Last sync status</label>
                  <input
                    value={draft.last_sync_status}
                    placeholder="configured / probe_ok / heartbeat_ok"
                    onChange={event => patchDraft(center.id, { last_sync_status: event.target.value })}
                  />
                </div>
              </div>

              {serviceReadiness && <div className={`cloud-posture-panel ${serviceReadiness.allowed ? 'ready' : 'watch'}`}>
                <div>
                  <label>Service coordination gate</label>
                  <strong>{serviceReadiness.allowed ? 'Allowed' : 'Blocked'}</strong>
                  <span>{serviceReadiness.allowed ? 'Cloud may coordinate platform services for this iWorkerCenter.' : 'Cloud will not enable service coordination until the blocking issues are resolved.'}</span>
                </div>
                <div className="cloud-issue-list">
                  {serviceReadiness.issues.length === 0
                    ? <span className="ok">No service blockers</span>
                    : serviceReadiness.issues.map(issue => <span key={issue} className="warn">{issueLabels[issue] ?? issue}</span>)}
                </div>
              </div>}

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
                  onClick={() => handleCheckServiceReadiness(center)}
                >
                  {checkingReadiness === center.id ? 'Checking...' : 'Check service gate'}
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


