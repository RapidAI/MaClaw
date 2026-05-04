import { useEffect, useMemo, useState } from 'react';
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
  type ManagementSummary,
  type CenterIntegrationPatch,
  type CloudControlMode,
  type CenterServiceReadiness,
  type CenterProbeResult,
  type CenterRuntimeStatus,
} from '../api/centers';

type IntegrationDraft = CenterIntegrationPatch;
type LicenseDurationMode = 'month' | 'quarter' | 'year' | 'multi_year' | 'permanent';
type ManualLicenseDraft = { duration_mode: LicenseDurationMode; custom_years: string; modules: string[] };
type Notice = { tone: 'ok' | 'danger' | 'info'; text: string };

const licenseModuleOptions = ['compute', 'skill_market', 'upgrade', 'support', 'all'];
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

const CENTER_FOCUS_KEY = 'iworkercloud_focus_center_id';

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
  if (!value || value.startsWith('0001-')) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
};

const serviceBadgeClass = (status?: string) => {
  if (status === 'heartbeat_ok' || status === 'probe_ok') return 'ok';
  if (status === 'registered' || status === 'configured' || status === 'probe_missing_base_url' || !status) return 'warn';
  return 'danger';
};

const runtimeModeLabel = (mode?: CenterRuntimeStatus['runtime_provider_mode']) => {
  switch (mode) {
    case 'cloud_sync':
      return 'cloud_sync';
    case 'local_self_managed':
      return 'local_self_managed';
    case 'local_settings_fallback':
      return 'local_settings_fallback';
    default:
      return 'not_reported';
  }
};

const runtimeContinuityKey = (runtime?: CenterRuntimeStatus) => {
  if (!runtime) return 'notLoaded';
  if (runtime.runtime_provider_mode === 'local_settings_fallback') return 'localFallback';
  if (runtime.compute_sync_status?.status === 'failure' && runtime.compute_sync_status.non_blocking) return 'nonBlockingSyncIssue';
  if (runtime.runtime_provider_mode === 'cloud_sync') return 'cloudSync';
  if (runtime.runtime_provider_mode === 'local_self_managed') return 'selfManaged';
  return 'platformOnly';
};

const runtimeContinuityClass = (runtime?: CenterRuntimeStatus) => {
  if (!runtime) return 'watch';
  if (runtime.compute_sync_status?.status === 'failure' && !runtime.compute_sync_status.non_blocking) return 'watch';
  return runtime.ok ? 'ready' : 'watch';
};

const durationDays = (mode: LicenseDurationMode, customYears: string) => {
  if (mode === 'permanent') return 0;
  if (mode === 'month') return 30;
  if (mode === 'quarter') return 90;
  if (mode === 'year') return 365;
  const years = Math.max(2, Number.parseInt(customYears, 10) || 2);
  return years * 365;
};

function createManualLicenseDraft(): ManualLicenseDraft {
  return { duration_mode: 'month', custom_years: '2', modules: ['compute'] };
}

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
  const [summary, setSummary] = useState<ManagementSummary | null>(null);
  const [query, setQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');
  const [focusedCenterId, setFocusedCenterId] = useState('');
  const [loading, setLoading] = useState(false);
  const [management, setManagement] = useState<Record<string, CenterManagement>>({});
  const [drafts, setDrafts] = useState<Record<string, IntegrationDraft>>({});
  const [probing, setProbing] = useState<string | null>(null);
  const [probeRuntime, setProbeRuntime] = useState<Record<string, CenterProbeResult>>({});
  const [runtimeRefreshing, setRuntimeRefreshing] = useState<Record<string, boolean>>({});
  const [readinessResult, setReadinessResult] = useState<Record<string, CenterServiceReadiness>>({});
  const [checkingReadiness, setCheckingReadiness] = useState<string | null>(null);
  const [saving, setSaving] = useState<string | null>(null);
  const [manualLicenseDrafts, setManualLicenseDrafts] = useState<Record<string, ManualLicenseDraft>>({});
  const [licensing, setLicensing] = useState<string | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [error, setError] = useState<string>('');

  const postureLabel = (value?: string) => t(`centers.posture.${value || 'unknown'}`, { defaultValue: postureLabels[value || ''] ?? value ?? '-' });
  const issueLabel = (value: string) => t(`centers.issues.${value}`, { defaultValue: issueLabels[value] ?? value });
  const controlModeLabel = (value?: CloudControlMode) => t(`centers.controlModes.${value || 'cloud_managed'}`, { defaultValue: controlModeLabels[value || 'cloud_managed'] });
  const syncStatusLabel = (value?: string) => t(`centers.syncStatus.${value || 'not_configured'}`, { defaultValue: syncStatusLabels[value || ''] ?? value ?? 'not configured' });
  const runtimeModeDisplay = (value?: CenterRuntimeStatus['runtime_provider_mode']) => t(`centers.runtimeModes.${runtimeModeLabel(value)}`);
  const runtimeContinuityText = (runtime?: CenterRuntimeStatus) => runtime?.message || t(`centers.runtimeContinuity.${runtimeContinuityKey(runtime)}`);
  const runtimeValue = (key: string, value?: string | boolean) => {
    const normalized = value === undefined || value === null || value === '' ? 'unknown' : String(value);
    return t(`centers.runtimeValues.${normalized}`, { defaultValue: normalized === 'unknown' ? t('centers.runtimeValues.unknown') : normalized });
  };
  const licenseModuleLabel = (module: string) => t(`licenses.moduleLabels.${module}`, { defaultValue: module });
  const showError = (err: unknown) => {
    const message = err instanceof Error ? err.message : String(err);
    setError(message);
    setNotice({ tone: 'danger', text: message });
  };

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
    setLoading(true);
    setError('');
    getCenterManagement()
      .then(report => {
        const nextItems = report.items ?? [];
        const nextCenters = nextItems.map(item => item.center);
        setSummary(report.summary ?? null);
        setCenters(nextCenters);
        setManagement(Object.fromEntries(nextItems.map(item => [item.center.id, item])));
        setDrafts(Object.fromEntries(nextCenters.map(center => [center.id, createDraft(center)])));
        setManualLicenseDrafts(prev => {
          const next = { ...prev };
          for (const center of nextCenters) {
            if (!next[center.id]) next[center.id] = createManualLicenseDraft();
          }
          return next;
        });
        void refreshRuntimeSnapshots(nextCenters);
      })
      .catch(err => setError(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  useEffect(() => {
    const focusId = sessionStorage.getItem(CENTER_FOCUS_KEY);
    if (!focusId) return;
    sessionStorage.removeItem(CENTER_FOCUS_KEY);
    setFocusedCenterId(focusId);
    setQuery(focusId);
    setStatusFilter('all');
    window.setTimeout(() => {
      document.getElementById('center-' + CSS.escape(focusId))?.scrollIntoView({ behavior: 'smooth', block: 'start' });
    }, 150);
  }, []);

  const patchDraft = (id: string, patch: Partial<IntegrationDraft>) => {
    if (notice) setNotice(null);
    setDrafts(prev => ({
      ...prev,
      [id]: { ...(prev[id] ?? createDraft(centers.find(center => center.id === id)!)), ...patch },
    }));
  };


  const handleSaveIntegration = async (center: Center) => {
    const draft = drafts[center.id] ?? createDraft(center);
    setSaving(center.id);
    setError('');
    setNotice(null);
    try {
      const updated = await updateCenterIntegration(center.id, {
        ...draft,
        base_url: draft.base_url.trim(),
        last_sync_status: draft.last_sync_status.trim() || 'configured',
      });
      setCenters(prev => prev.map(item => item.id === center.id ? updated : item));
      setDrafts(prev => ({ ...prev, [center.id]: createDraft(updated) }));
      setNotice({ tone: 'ok', text: t('centers.noticeIntegrationSaved') });
      load();
    } catch (err) {
      showError(err);
    } finally {
      setSaving(null);
    }
  };

  const patchManualLicenseDraft = (centerID: string, patch: Partial<ManualLicenseDraft>) => {
    setManualLicenseDrafts(prev => ({
      ...prev,
      [centerID]: { ...(prev[centerID] ?? createManualLicenseDraft()), ...patch },
    }));
  };

  const toggleManualLicenseModule = (centerID: string, module: string) => {
    const current = manualLicenseDrafts[centerID] ?? createManualLicenseDraft();
    const modules = current.modules.includes(module)
      ? current.modules.filter(item => item !== module)
      : [...current.modules, module];
    patchManualLicenseDraft(centerID, { modules: modules.length > 0 ? modules : ['compute'] });
  };

  const handleConfirmTrial = async (center: Center) => {
    setLicensing(center.id);
    setError('');
    setNotice(null);
    try {
      await confirmTrial(center.id);
      setNotice({ tone: 'ok', text: t('centers.noticeTrialActivated') });
      load();
    } catch (err) {
      showError(err);
    } finally {
      setLicensing(null);
    }
  };

  const handleConfirmManual = async (center: Center) => {
    const draft = manualLicenseDrafts[center.id] ?? createManualLicenseDraft();
    const days = durationDays(draft.duration_mode, draft.custom_years);
    setLicensing(center.id);
    setError('');
    setNotice(null);
    try {
      await confirmManual(center.id, draft.modules.length > 0 ? draft.modules : ['compute'], days);
      setNotice({ tone: 'ok', text: t('centers.noticeManualAuthorized') });
      load();
    } catch (err) {
      showError(err);
    } finally {
      setLicensing(null);
    }
  };

  const handleCheckServiceReadiness = async (center: Center) => {
    setCheckingReadiness(center.id);
    setError('');
    setNotice(null);
    try {
      const readiness = await getServiceReadiness(center.id);
      setReadinessResult(prev => ({ ...prev, [center.id]: readiness }));
      setNotice({ tone: readiness.allowed ? 'ok' : 'info', text: readiness.allowed ? t('centers.noticeServiceReady') : t('centers.noticeServiceBlocked') });
    } catch (err) {
      showError(err);
    } finally {
      setCheckingReadiness(null);
    }
  };
  const handleProbeCenter = async (center: Center) => {
    setProbing(center.id);
    setError('');
    setNotice(null);
    try {
      const response = await probeCenter(center.id);
      setCenters(prev => prev.map(item => item.id === center.id ? response.center : item));
      setDrafts(prev => ({ ...prev, [center.id]: createDraft(response.center) }));
      setProbeRuntime(prev => ({ ...prev, [center.id]: response.probe }));
      setNotice({ tone: response.probe.ok ? 'ok' : 'danger', text: response.probe.ok ? t('centers.noticeProbeOk') : t('centers.noticeProbeFailed') });
      load();
    } catch (err) {
      showError(err);
    } finally {
      setProbing(null);
    }
  };

  const handleDelete = async (id: string) => {
    if (!confirm(t('centers.confirmDelete'))) return;
    setError('');
    setNotice(null);
    try {
      await deleteCenter(id);
      setNotice({ tone: 'ok', text: t('centers.noticeDeleted') });
      load();
    } catch (err) {
      showError(err);
    }
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
        await handleConfirmManual(center);
        return;
      default:
        setError(t('centers.useServiceActions'));
    }
  };

  const recommendedActionLabel = (code: string) => {
    switch (code) {
      case 'activate_center':
        return t('centers.actions.activateTrial');
      case 'configure_base_url':
        return t('centers.actions.saveIntegration');
      case 'test_connection':
        return t('centers.actions.testNow');
      case 'verify_center_service_identity':
        return t('centers.actions.verifyIdentity');
      case 'issue_license':
        return t('centers.actions.issueLicense');
      case 'ready_for_service_management':
        return t('centers.actions.reviewServices');
      default:
        return t('centers.actions.review');
    }
  };

  const isRecommendedActionDisabled = (center: Center, code: string) => {
    if (code === 'test_connection' || code === 'verify_center_service_identity') return probing === center.id || !center.base_url;
    if (code === 'configure_base_url') return saving === center.id;
    return false;
  };

  const pendingAuthorizationCenters = useMemo(() => centers.filter(center => {
    const item = management[center.id];
    return center.status === 'pending' || item?.issues.includes('no_active_license');
  }), [centers, management]);

  const filteredCenters = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    return centers.filter(center => {
      const matchesStatus = statusFilter === 'all' || center.status === statusFilter;
      const matchesQuery = !normalized || [center.id, center.company_name, center.admin_email, center.admin_phone, center.base_url, center.last_sync_status]
        .filter(Boolean)
        .some(value => String(value).toLowerCase().includes(normalized));
      return matchesStatus && matchesQuery;
    });
  }, [centers, query, statusFilter]);

  const centerStats = summary ?? {
    total_centers: centers.length,
    pending_centers: centers.filter(center => center.status === 'pending').length,
    active_licenses: 0,
    ready_centers: Object.values(management).filter(item => item.ready).length,
    needs_setup: Object.values(management).filter(item => !item.ready).length,
    probe_failures: 0,
    unlicensed_centers: 0,
  };

  return (
    <div className="cloud-center-stack cloud-page-stack">
      <section className="cloud-brief card">
        <div>
          <div className="mini">{t('centers.eyebrow')}</div>
          <h3>{t('centers.title')}</h3>
          <p>{t('centers.positionDesc')}</p>
        </div>
        <div className="cloud-brief-note">
          <strong>{t('centers.boundaryTitle')}</strong>
          <span>{t('centers.boundaryNote')}</span>
        </div>
      </section>

      <div className="metrics cloud-metrics ops-summary-grid">
        <div className="metric"><label>{t('centers.stats.total')}</label><strong>{centerStats.total_centers}</strong><span>{t('centers.stats.totalHint')}</span></div>
        <div className="metric"><label>{t('centers.stats.pending')}</label><strong>{centerStats.pending_centers}</strong><span>{t('centers.stats.pendingHint')}</span></div>
        <div className="metric"><label>{t('centers.stats.ready')}</label><strong>{centerStats.ready_centers}</strong><span>{centerStats.needs_setup} {t('centers.stats.needsSetup')}</span></div>
        <div className="metric"><label>{t('centers.stats.licensed')}</label><strong>{centerStats.active_licenses}</strong><span>{centerStats.unlicensed_centers} {t('centers.stats.unlicensed')}</span></div>
      </div>

      <div className="head cloud-toolbar">
        <div className="cloud-filter-row">
          <input value={query} onChange={event => { setQuery(event.target.value); if (focusedCenterId) setFocusedCenterId(''); }} placeholder={t('centers.search')} />
          <select value={statusFilter} onChange={event => { setStatusFilter(event.target.value); if (focusedCenterId) setFocusedCenterId(''); }}>
            <option value="all">{t('centers.allStatus')}</option>
            <option value="pending">{t('centers.pending')}</option>
            <option value="active">{t('centers.active')}</option>
            <option value="disabled">{t('centers.disabled')}</option>
          </select>
        </div>
        <div className="actions">
          {focusedCenterId ? <button className="btn-ghost" onClick={() => { setFocusedCenterId(''); setQuery(''); }}>{t('centers.clearFocus')}</button> : null}
          <button className="btn-ghost" onClick={load}>{loading ? t('common.loading') : t('common.refresh')}</button>
        </div>
      </div>

      <div className="hint">
        {t('centers.controlPlaneHint')}
        {Object.keys(runtimeRefreshing).length > 0 ? ` ${t('centers.refreshingRuntime')}` : ''}
      </div>
      {notice ? <div className={`notice ${notice.tone}`}>{notice.text}</div> : null}
      {error && !notice ? <div className="hint danger">{error}</div> : null}

      {pendingAuthorizationCenters.length > 0 ? <section className="cloud-auth-queue">
        <div>
          <label>{t('centers.authorizationQueueTitle')}</label>
          <strong>{t('centers.authorizationQueueCount', { count: pendingAuthorizationCenters.length })}</strong>
          <span>{t('centers.authorizationQueueDesc')}</span>
        </div>
        <div className="cloud-auth-queue-list">
          {pendingAuthorizationCenters.map(center => (
            <div key={center.id} className="cloud-auth-queue-row">
              <span>{center.company_name || center.id}</span>
              <small>{center.id}</small>
              <button className="btn-secondary" disabled={licensing === center.id} onClick={() => handleConfirmTrial(center)}>
                {licensing === center.id ? t('centers.actions.issuing') : t('centers.confirmTrial')}
              </button>
              <button className="btn-ghost" disabled={licensing === center.id} onClick={() => handleConfirmManual(center)}>
                {t('centers.actions.issueManual')}
              </button>
            </div>
          ))}
        </div>
      </section> : null}
      {filteredCenters.length === 0 ? <div className="hint">{centers.length === 0 ? t('centers.empty') : t('centers.noMatch')}</div> : <div className="list">
        {filteredCenters.map(center => {
          const draft = drafts[center.id] ?? createDraft(center);
          const managementItem = management[center.id];
          const serviceReadiness = readinessResult[center.id];
          const runtime = probeRuntime[center.id] || managementItem?.runtime_status;
          const workload = managementItem?.iworker_readiness?.workload_summary;
          const manualLicenseDraft = manualLicenseDrafts[center.id] ?? createManualLicenseDraft();
          const isRuntimeRefreshing = runtimeRefreshing[center.id] === true;
          return (
            <div id={'center-' + center.id} key={center.id} className={`item cloud-center-card ${focusedCenterId === center.id ? 'focused' : ''}`}>
              <div className="item-head">
                <div>
                  <span className="item-title">{center.company_name}</span>
                  <div className="item-meta">{t('centers.fields.id')}: {center.id}</div>
                </div>
                <span className={`badge ${center.status === 'active' ? 'ok' : center.status === 'pending' ? 'warn' : 'danger'}`}>
                  {t(`centers.${center.status}`)}
                </span>
              </div>

              <div className="cloud-center-facts">
                <span>{controlModeLabel(center.cloud_control_mode)}</span>
                <span className={`badge ${serviceBadgeClass(center.last_sync_status)}`}>{t('centers.fields.service')}: {syncStatusLabel(center.last_sync_status)}</span>
                {managementItem && <span>{t('centers.fields.commercial')}: {t(`centers.commercial.${managementItem.commercial_status}`, { defaultValue: managementItem.commercial_status })}</span>}
                {managementItem && <span>{t('centers.fields.connectivity')}: {t(`centers.connectivity.${managementItem.connectivity}`, { defaultValue: managementItem.connectivity })}</span>}
                <span className={`badge ${center.iworker_ready ? 'ok' : center.iworker_readiness_status ? 'warn' : 'warn'}`}>iWorker: {center.iworker_readiness_status || t('centers.readiness.notReported')}</span>
                <span>{t('centers.readiness.agentInstances')}: {workload?.agent_instance_count ?? center.iworker_agent_instance_count ?? managementItem?.iworker_readiness?.agent_instance_count ?? 0}</span>
                <span>{t('centers.fields.tenantMode')}: {center.supports_multi_tenant === true ? t('centers.tenant.modeMulti') : center.supports_multi_tenant === false ? t('centers.tenant.modeDedicated') : t('centers.tenant.notReported')}</span>
                {typeof center.tenant_count === 'number' && <span>{t('centers.fields.tenants')}: {center.tenant_count}</span>}
                <span>{t('centers.fields.lastHeartbeat')}: {formatDateTime(center.last_heartbeat) || t('centers.noHeartbeat')}</span>
                {center.created_at && <span>{t('centers.fields.registered')}: {new Date(center.created_at).toLocaleString()}</span>}
              </div>

              <div className="cloud-review-panel">
                <div>
                  <label>{t('centers.sections.registrationReview')}</label>
                  <strong>{center.company_name || t('centers.unnamedCompany')}</strong>
                  <span>{t('centers.registrationReviewHint')}</span>
                </div>
                <div className="cloud-issue-list">
                  <span>{t('centers.fields.legal')}: {center.legal_person || t('centers.notProvided')}</span>
                  <span>{t('centers.fields.email')}: {center.admin_email || t('centers.notProvided')}</span>
                  <span>{t('centers.fields.phone')}: {center.admin_phone || t('centers.notProvided')}</span>
                  <span>{t('centers.fields.address')}: {center.address || t('centers.notProvided')}</span>
                </div>
              </div>

              <div className="cloud-tenant-panel">
                <div className="cloud-tenant-head">
                  <div>
                    <label>{t('centers.sections.tenantBoundary')}</label>
                    <strong>{center.supports_multi_tenant === true ? t('centers.tenant.multiTenant') : center.supports_multi_tenant === false ? t('centers.tenant.dedicated') : t('centers.tenant.notReported')}</strong>
                    <span>{t('centers.tenantBoundaryHint')}</span>
                  </div>
                  <div className="cloud-tenant-actions">
                    {typeof center.tenant_count === 'number' ? <span className="badge info">{t('centers.tenant.countReported', { count: center.tenant_count })}</span> : <span className="badge warn">{t('centers.tenant.noCount')}</span>}
                  </div>
                </div>
              </div>

              {managementItem && <div className={`cloud-posture-panel ${managementItem.ready ? 'ready' : 'watch'}`}>
                <div>
                  <label>{t('centers.sections.managementReadiness')}</label>
                  <strong>{postureLabel(managementItem.management_posture)}</strong>
                  <span>{managementItem.ready ? t('centers.readiness.managementReadyHint') : t('centers.readiness.managementSetupHint')}</span>
                </div>
                <div className="cloud-issue-list">
                  {managementItem.issues.length === 0
                    ? <span className="ok">{t('centers.readiness.noBlockingIssues')}</span>
                    : managementItem.issues.map(issue => <span key={issue} className="warn">{issueLabel(issue)}</span>)}
                </div>
              </div>}

              {managementItem && <div className={`cloud-posture-panel ${managementItem.iworker_operational_ready ? 'ready' : 'watch'}`}>
                <div>
                  <label>{t('centers.sections.iworkerReadiness')}</label>
                  <strong>{managementItem.iworker_operational_ready ? t('centers.readiness.readyForPushedWork') : center.iworker_readiness_status ? t('centers.readiness.needsIWorkerSetup') : t('centers.readiness.waitingHeartbeat')}</strong>
                  <span>{managementItem.iworker_operational_ready ? t('centers.readiness.iworkerReadyHint') : t('centers.readiness.iworkerSetupHint')}</span>
                </div>
                <div className="cloud-issue-list">
                  <span className={(managementItem.iworker_readiness?.agent_runtime_ready ?? false) ? 'ok' : 'warn'}>{t('centers.readiness.agentRuntime')}: {(managementItem.iworker_readiness?.agent_runtime_ready ?? false) ? t('centers.readiness.ready') : t('centers.readiness.notReady')}</span>
                  <span className={(managementItem.iworker_readiness?.goalwatch_ready ?? false) ? 'ok' : 'warn'}>GoalWatch: {(managementItem.iworker_readiness?.goalwatch_ready ?? false) ? t('centers.readiness.ready') : t('centers.readiness.notReady')}</span>
                  <span>{t('centers.readiness.agentInstances')}: {workload?.agent_instance_count ?? center.iworker_agent_instance_count ?? managementItem.iworker_readiness?.agent_instance_count ?? 0}</span>
                  {workload ? <>
                    <span className={workload.active_count > 0 ? 'ok' : 'warn'}>{t('centers.readiness.active')}: {workload.active_count}</span>
                    <span>{t('centers.readiness.completed')}: {workload.completed_count}</span>
                    <span className={workload.review_count > 0 ? 'warn' : 'ok'}>{t('centers.readiness.review')}: {workload.review_count}</span>
                    <span className={workload.blocked_count > 0 ? 'danger' : 'ok'}>{t('centers.readiness.blocked')}: {workload.blocked_count}</span>
                    {workload.updated_at ? <span>{t('centers.readiness.workloadSync')}: {formatDateTime(workload.updated_at)}</span> : null}
                  </> : null}
                  <span className={center.iworker_ready ? 'ok' : 'warn'}>{t('compute.status')}: {center.iworker_readiness_status || t('centers.readiness.notReported')}</span>
                </div>
              </div>}

              {runtime && <div className={`cloud-posture-panel ${runtimeContinuityClass(runtime)}`}>
                <div>
                  <label>{t('centers.sections.runtimeSnapshot')}</label>
                  <strong>{runtimeModeDisplay(runtime.runtime_provider_mode)}</strong>
                  <span>{runtimeContinuityText(runtime)}</span>
                </div>
                <div className="cloud-issue-list">
                  <span className={runtime.compute_source === 'cloud' ? 'ok' : 'warn'}>{t('centers.runtime.computeSource')}: {runtimeValue('computeSource', runtime.compute_source)}</span>
                  <span className={runtime.compute_sync_status?.status === 'success' ? 'ok' : runtime.compute_sync_status?.non_blocking ? 'warn' : 'danger'}>{t('centers.runtime.sync')}: {runtimeValue('sync', runtime.compute_sync_status?.status)}</span>
                  {runtime.compute_sync_status?.non_blocking ? <span className="ok">{t('centers.runtime.nonBlocking')}</span> : null}
                  {runtime.compute_sync_status?.runtime_impact ? <span>{t('centers.runtime.impact')}: {runtime.compute_sync_status.runtime_impact}</span> : null}
                  <span>{t('centers.runtime.runtimeProviders')}: {runtime.provider_count ?? 0}</span>
                  <span>{t('centers.runtime.cloudProviders')}: {runtime.cloud_provider_count ?? runtime.compute_sync_status?.provider_count ?? 0}</span>
                  <span>{runtime.compute_permission ? t('centers.runtime.selfManagementAllowed') : t('centers.runtime.cloudManaged')}</span>
                  {runtime.compute_sync_status?.last_sync_at ? <span>{t('centers.runtime.lastSync')}: {formatDateTime(runtime.compute_sync_status.last_sync_at)}</span> : null}
                  {runtime.compute_sync_status?.error ? <span className="warn">{runtime.compute_sync_status.error}</span> : null}
                </div>
              </div>}

              {managementItem && <div className="cloud-action-panel">
                <div className="field-span-2">
                  <label>{t('centers.sections.recommendedActions')}</label>
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
                  <label>{t('centers.fields.baseUrl')}</label>
                  <input
                    value={draft.base_url}
                    placeholder="https://center.example.com"
                    onChange={event => patchDraft(center.id, { base_url: event.target.value })}
                  />
                </div>
                <div>
                  <label>{t('centers.fields.controlMode')}</label>
                  <select
                    value={draft.cloud_control_mode}
                    onChange={event => patchDraft(center.id, { cloud_control_mode: event.target.value as CloudControlMode })}
                  >
                    {Object.keys(controlModeLabels).map(value => (
                      <option key={value} value={value}>{controlModeLabel(value as CloudControlMode)}</option>
                    ))}
                  </select>
                </div>
                <div>
                  <label>{t('centers.fields.lastSyncStatus')}</label>
                  <input
                    value={draft.last_sync_status}
                    placeholder="configured / probe_ok / heartbeat_ok"
                    onChange={event => patchDraft(center.id, { last_sync_status: event.target.value })}
                  />
                </div>
              </div>

              {serviceReadiness && <div className={`cloud-posture-panel ${serviceReadiness.allowed ? 'ready' : 'watch'}`}>
                <div>
                  <label>{t('centers.sections.serviceGate')}</label>
                  <strong>{serviceReadiness.allowed ? t('centers.readiness.allowed') : t('centers.readiness.blockedGate')}</strong>
                  <span>{serviceReadiness.allowed ? t('centers.readiness.serviceAllowedHint') : t('centers.readiness.serviceBlockedHint')}</span>
                </div>
                <div className="cloud-issue-list">
                  {serviceReadiness.issues.length === 0
                    ? <span className="ok">{t('centers.readiness.noServiceBlockers')}</span>
                    : serviceReadiness.issues.map(issue => <span key={issue} className="warn">{issueLabel(issue)}</span>)}
                </div>
              </div>}

              <div className="cloud-license-panel">
                <div>
                  <label>{t('centers.sections.manualAuthorization')}</label>
                  <strong>{t('centers.manualAuthorizationTitle')}</strong>
                  <span>{t('centers.manualAuthorizationHint')}</span>
                </div>
                <div className="cloud-license-controls">
                  <label>
                    <span>{t('licenses.duration')}</span>
                    <select
                      value={manualLicenseDraft.duration_mode}
                      onChange={event => patchManualLicenseDraft(center.id, { duration_mode: event.target.value as LicenseDurationMode })}
                    >
                      <option value="month">{t('licenses.durationOptions.month')}</option>
                      <option value="quarter">{t('licenses.durationOptions.quarter')}</option>
                      <option value="year">{t('licenses.durationOptions.year')}</option>
                      <option value="multi_year">{t('licenses.durationOptions.multiYear')}</option>
                      <option value="permanent">{t('licenses.durationOptions.permanent')}</option>
                    </select>
                  </label>
                  {manualLicenseDraft.duration_mode === 'multi_year' ? <label>
                    <span>{t('licenses.customYears')}</span>
                    <input
                      type="number"
                      min="2"
                      value={manualLicenseDraft.custom_years}
                      onChange={event => patchManualLicenseDraft(center.id, { custom_years: event.target.value })}
                    />
                  </label> : <label>
                    <span>{t('licenses.calculatedDays')}</span>
                    <input readOnly value={durationDays(manualLicenseDraft.duration_mode, manualLicenseDraft.custom_years) === 0 ? t('licenses.permanentValue') : String(durationDays(manualLicenseDraft.duration_mode, manualLicenseDraft.custom_years))} />
                  </label>}
                  <div className="cloud-license-modules">
                    {licenseModuleOptions.map(module => (
                      <label key={module} className="module-check">
                        <input
                          type="checkbox"
                          checked={manualLicenseDraft.modules.includes(module)}
                          onChange={() => toggleManualLicenseModule(center.id, module)}
                        />
                        {licenseModuleLabel(module)}
                      </label>
                    ))}
                  </div>
                  <button className="btn-secondary" disabled={licensing === center.id} onClick={() => handleConfirmManual(center)}>
                    {licensing === center.id ? t('centers.actions.issuing') : t('centers.actions.issueManual')}
                  </button>
                </div>
              </div>

              <div className="actions">
                <button className="btn-primary" disabled={saving === center.id} onClick={() => handleSaveIntegration(center)}>
                  {saving === center.id ? t('centers.actions.saving') : t('centers.actions.saveIntegration')}
                </button>
                <button
                  className="btn-ghost"
                  disabled={probing === center.id || !center.base_url}
                  onClick={() => handleProbeCenter(center)}
                >
                  {probing === center.id ? t('centers.actions.testing') : t('centers.actions.testConnection')}
                </button>
                <button
                  className="btn-ghost"
                  disabled={isRuntimeRefreshing || !center.base_url}
                  onClick={() => refreshRuntimeSnapshots([center])}
                >
                  {isRuntimeRefreshing ? t('centers.actions.refreshingRuntime') : t('centers.actions.refreshRuntime')}
                </button>
                <button
                  className="btn-ghost"
                  disabled={checkingReadiness === center.id}
                  onClick={() => handleCheckServiceReadiness(center)}
                >
                  {checkingReadiness === center.id ? t('centers.actions.checking') : t('centers.actions.checkServiceGate')}
                </button>
                {center.status === 'pending' && <button className="btn-secondary" disabled={licensing === center.id} onClick={() => handleConfirmTrial(center)}>{licensing === center.id ? t('centers.actions.issuing') : t('centers.confirmTrial')}</button>}
                {center.status === 'active' && <button className="btn-danger" onClick={() => { setNotice(null); disableCenter(center.id).then(() => { setNotice({ tone: 'ok', text: t('centers.noticeDisabled') }); load(); }).catch(showError); }}>{t('centers.disable')}</button>}
                {center.status === 'disabled' && <button className="btn-secondary" onClick={() => { setNotice(null); enableCenter(center.id).then(() => { setNotice({ tone: 'ok', text: t('centers.noticeEnabled') }); load(); }).catch(showError); }}>{t('centers.enable')}</button>}
                <button className="btn-danger" onClick={() => handleDelete(center.id)}>{t('centers.delete')}</button>
              </div>
            </div>
          );
        })}
      </div>}
    </div>
  );
}


