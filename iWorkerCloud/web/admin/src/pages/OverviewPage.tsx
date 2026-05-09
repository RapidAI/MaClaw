import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getCenterManagement, type CenterManagement, type CenterManagementReport } from '../api/centers';

type CloudStat = {
  label: string;
  value: string;
  hint: string;
};

type CloudPillar = {
  eyebrow: string;
  title: string;
  tone: 'ok' | 'info' | 'warn';
  summary: string;
  detail: string;
  stats: string[];
};

type ActionQueueItem = {
  center: CenterManagement['center'];
  tone: 'danger' | 'warn' | 'info';
  title: string;
  reason: string;
  facts: string[];
};

const CENTER_FOCUS_KEY = 'iworkercloud_focus_center_id';

function goToCenters(centerId?: string) {
  if (centerId) sessionStorage.setItem(CENTER_FOCUS_KEY, centerId);
  window.location.hash = 'centers';
}

function formatRelativeTime(value?: string) {
  if (!value || value.startsWith('0001-')) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

export function OverviewPage() {
  const { t } = useTranslation();
  const [report, setReport] = useState<CenterManagementReport | null>(null);

  useEffect(() => {
    getCenterManagement().then(setReport).catch(() => setReport(null));
  }, []);

  const summary = report?.summary ?? {
    total_centers: 0,
    pending_centers: 0,
    active_licenses: 0,
    ready_centers: 0,
    needs_setup: 0,
    probe_failures: 0,
    unlicensed_centers: 0,
    workload_agent_instances: 0,
    workload_active_tasks: 0,
    workload_completed_tasks: 0,
    workload_review_tasks: 0,
    workload_blocked_tasks: 0,
    runtime_fallback_centers: 0,
    runtime_non_blocking_issues: 0,
    runtime_blocking_issues: 0,
    heartbeat_degraded_centers: 0,
    heartbeat_blocking_issues: 0,
  };

  const cloudStats = useMemo<CloudStat[]>(() => [
    {
      label: t('nav.centers'),
      value: String(summary.total_centers),
      hint: t('cloudOverview.statCenters'),
    },
    {
      label: t('centers.pending'),
      value: String(summary.pending_centers),
      hint: t('cloudOverview.statPending'),
    },
    {
      label: t('licenses.valid'),
      value: String(summary.active_licenses),
      hint: t('cloudOverview.statLicenses'),
    },
  ], [summary, t]);

  const managementStats = useMemo<CloudStat[]>(() => [
    {
      label: t('cloudOverview.ops.readyCenters'),
      value: String(summary.ready_centers),
      hint: t('cloudOverview.ops.readyCentersHint'),
    },
    {
      label: t('cloudOverview.ops.needsSetup'),
      value: String(summary.needs_setup),
      hint: t('cloudOverview.ops.needsSetupHint'),
    },
    {
      label: t('cloudOverview.ops.probeFailures'),
      value: String(summary.probe_failures),
      hint: t('cloudOverview.ops.probeFailuresHint'),
    },
    {
      label: t('cloudOverview.ops.serviceCapable'),
      value: String(summary.ready_centers),
      hint: t('cloudOverview.ops.serviceCapableHint'),
    },
    {
      label: t('cloudOverview.ops.unlicensed'),
      value: String(summary.unlicensed_centers),
      hint: t('cloudOverview.ops.unlicensedHint'),
    },
    {
      label: t('cloudOverview.ops.iworkerAgents'),
      value: String(summary.workload_agent_instances ?? 0),
      hint: t('cloudOverview.ops.iworkerAgentsHint'),
    },
    {
      label: t('cloudOverview.ops.activeWork'),
      value: String(summary.workload_active_tasks ?? 0),
      hint: t('cloudOverview.ops.activeWorkHint'),
    },
    {
      label: t('cloudOverview.ops.blockedOrReview'),
      value: String((summary.workload_blocked_tasks ?? 0) + (summary.workload_review_tasks ?? 0)),
      hint: t('cloudOverview.ops.blockedOrReviewHint'),
    },
    {
      label: t('cloudOverview.ops.localFallback'),
      value: String(summary.runtime_fallback_centers ?? 0),
      hint: t('cloudOverview.ops.localFallbackHint'),
    },
    {
      label: t('cloudOverview.ops.nonBlockingRuntime'),
      value: String(summary.runtime_non_blocking_issues ?? 0),
      hint: t('cloudOverview.ops.nonBlockingRuntimeHint'),
    },
    {
      label: t('cloudOverview.ops.blockingRuntime'),
      value: String(summary.runtime_blocking_issues ?? 0),
      hint: t('cloudOverview.ops.blockingRuntimeHint'),
    },
    {
      label: t('cloudOverview.ops.heartbeatDegraded'),
      value: String(summary.heartbeat_degraded_centers ?? 0),
      hint: t('cloudOverview.ops.heartbeatDegradedHint'),
    },
    {
      label: t('cloudOverview.ops.heartbeatBlocking'),
      value: String(summary.heartbeat_blocking_issues ?? 0),
      hint: t('cloudOverview.ops.heartbeatBlockingHint'),
    },
  ], [summary]);

  const actionQueue = useMemo<ActionQueueItem[]>(() => {
    const items = report?.items ?? [];
    const queued: ActionQueueItem[] = [];
    for (const item of items) {
      const center = item.center;
      const runtimeBlocking = item.runtime_status?.compute_sync_status?.status === 'failure' && !item.runtime_status.compute_sync_status.non_blocking;
      const heartbeatBlocking = item.runtime_status?.cloud_heartbeat?.status && !['online', 'disabled'].includes(item.runtime_status.cloud_heartbeat.status) && !item.runtime_status.cloud_heartbeat.non_blocking;
      const heartbeatNonBlocking = item.runtime_status?.cloud_heartbeat?.status && !['online', 'disabled'].includes(item.runtime_status.cloud_heartbeat.status) && item.runtime_status.cloud_heartbeat.non_blocking;
      const workload = item.iworker_readiness?.workload_summary;
      const facts = [
        center.last_sync_status ? t('cloudOverview.queue.sync', { status: center.last_sync_status }) : '',
        center.last_heartbeat ? t('cloudOverview.queue.heartbeat', { time: formatRelativeTime(center.last_heartbeat) }) : '',
        item.runtime_status?.cloud_heartbeat?.status ? t('cloudOverview.queue.cloudHeartbeat', { status: item.runtime_status.cloud_heartbeat.status }) : '',
        workload ? t('cloudOverview.queue.workload', { active: workload.active_count, blocked: workload.blocked_count, review: workload.review_count }) : '',
      ].filter(Boolean);

      if (runtimeBlocking) {
        queued.push({ center, tone: 'danger', title: t('cloudOverview.queue.runtimeBlocking'), reason: item.runtime_status?.compute_sync_status?.error || t('cloudOverview.queue.runtimeBlockingHint'), facts });
        continue;
      }
      if (heartbeatBlocking) {
        queued.push({ center, tone: 'danger', title: t('cloudOverview.queue.heartbeatBlocking'), reason: item.runtime_status?.cloud_heartbeat?.last_error || t('cloudOverview.queue.heartbeatBlockingHint'), facts });
        continue;
      }
      if (center.status === 'pending') {
        queued.push({ center, tone: 'warn', title: t('cloudOverview.queue.pendingActivation'), reason: t('cloudOverview.queue.pendingActivationHint'), facts });
        continue;
      }
      if (item.issues.includes('no_active_license')) {
        queued.push({ center, tone: 'warn', title: t('cloudOverview.queue.unlicensed'), reason: t('cloudOverview.queue.unlicensedHint'), facts });
        continue;
      }
      const highPriorityAction = item.recommended_actions?.find(action => action.priority === 'high');
      if (highPriorityAction) {
        queued.push({ center, tone: 'warn', title: highPriorityAction.label || t('cloudOverview.queue.highPriority'), reason: highPriorityAction.description || t('cloudOverview.queue.highPriorityHint'), facts });
        continue;
      }
      if (!item.iworker_operational_ready && center.iworker_readiness_status) {
        queued.push({ center, tone: 'info', title: t('cloudOverview.queue.iworkerSetup'), reason: t('cloudOverview.queue.iworkerSetupHint'), facts });
        continue;
      }
      if (heartbeatNonBlocking) {
        queued.push({ center, tone: 'info', title: t('cloudOverview.queue.heartbeatNonBlocking'), reason: t('cloudOverview.queue.heartbeatNonBlockingHint'), facts });
      }
    }
    return queued.slice(0, 5);
  }, [report, t]);

  const pillars = useMemo<CloudPillar[]>(() => [
    {
      eyebrow: t('cloudOverview.computeEyebrow'),
      title: t('cloudOverview.computeTitle'),
      tone: (summary.runtime_blocking_issues ?? 0) > 0 || summary.probe_failures > 0 ? 'warn' : (summary.runtime_fallback_centers ?? 0) > 0 ? 'info' : 'ok',
      summary: t('cloudOverview.computeSummary'),
      detail: t('cloudOverview.computeDetail'),
      stats: [t('cloudOverview.computePoint1'), t('cloudOverview.computePoint2'), t('cloudOverview.computePoint3')],
    },
    {
      eyebrow: t('cloudOverview.marketEyebrow'),
      title: t('cloudOverview.marketTitle'),
      tone: summary.unlicensed_centers > 0 ? 'warn' : 'ok',
      summary: t('cloudOverview.marketSummary'),
      detail: t('cloudOverview.marketDetail'),
      stats: [t('cloudOverview.marketPoint1'), t('cloudOverview.marketPoint2'), t('cloudOverview.marketPoint3')],
    },
    {
      eyebrow: t('cloudOverview.tenantEyebrow'),
      title: t('cloudOverview.tenantTitle'),
      tone: summary.needs_setup > 0 ? 'warn' : 'ok',
      summary: t('cloudOverview.tenantSummary'),
      detail: t('cloudOverview.tenantDetail'),
      stats: [t('cloudOverview.tenantPoint1'), t('cloudOverview.tenantPoint2'), t('cloudOverview.tenantPoint3')],
    },
  ], [summary, t]);

  return (
    <div className="cloud-overview-stack">
      <section className="cloud-brief card">
        <div>
          <div className="mini">iWorkerCloud</div>
          <h3>{t('cloudOverview.title')}</h3>
          <p>{t('cloudOverview.desc')}</p>
        </div>
        <div className="cloud-brief-note">
          <strong>{t('cloudOverview.positionTitle')}</strong>
          <span>{t('cloudOverview.positionDesc')}</span>
        </div>
      </section>

      <div className="metrics cloud-metrics">
        {cloudStats.map((item) => (
          <div key={item.label} className="metric">
            <label>{item.label}</label>
            <strong>{item.value}</strong>
            <span>{item.hint}</span>
          </div>
        ))}
      </div>

      <section className="card cloud-ops-card">
        <div className="item-head">
          <div>
            <span className="mini">{t('cloudOverview.ops.radar')}</span>
            <h3>{t('cloudOverview.ops.readinessTitle')}</h3>
          </div>
          <span className={`badge ${summary.probe_failures || summary.needs_setup || summary.unlicensed_centers || summary.runtime_blocking_issues ? 'warn' : 'ok'}`}>
            {summary.probe_failures || summary.needs_setup || summary.unlicensed_centers || summary.runtime_blocking_issues ? t('cloudOverview.watch') : t('cloudOverview.ready')}
          </span>
        </div>
        <p>{t('cloudOverview.ops.readinessDesc')}</p>
        <div className="cloud-ops-grid">
          {managementStats.map(item => (
            <div key={item.label} className="cloud-ops-metric">
              <label>{item.label}</label>
              <strong>{item.value}</strong>
              <span>{item.hint}</span>
            </div>
          ))}
        </div>
      </section>

      <section className="card cloud-action-queue-card">
        <div className="item-head">
          <div>
            <span className="mini">{t('cloudOverview.queue.eyebrow')}</span>
            <h3>{t('cloudOverview.queue.title')}</h3>
          </div>
          <button className="btn-ghost" onClick={() => goToCenters()}>{t('cloudOverview.queue.openCenters')}</button>
        </div>
        <p>{t('cloudOverview.queue.desc')}</p>
        {actionQueue.length === 0 ? (
          <div className="hint">{t('cloudOverview.queue.empty')}</div>
        ) : (
          <div className="cloud-action-queue-list">
            {actionQueue.map(item => (
              <button key={item.center.id + item.title} className={`cloud-action-queue-item ${item.tone}`} onClick={() => goToCenters(item.center.id)}>
                <div>
                  <span className={`badge ${item.tone}`}>{item.title}</span>
                  <strong>{item.center.company_name || item.center.id}</strong>
                  <small>{item.reason}</small>
                </div>
                <div className="cloud-action-queue-facts">
                  <span>ID: {item.center.id}</span>
                  {item.facts.map(fact => <span key={fact}>{fact}</span>)}
                </div>
              </button>
            ))}
          </div>
        )}
      </section>

      <div className="cloud-pillar-grid">
        {pillars.map((item) => (
          <section key={item.title} className="cloud-pillar-card card">
            <div className="item-head">
              <div>
                <span className="mini">{item.eyebrow}</span>
                <h3>{item.title}</h3>
              </div>
              <span className={`badge ${item.tone}`}>{item.tone === 'warn' ? t('cloudOverview.watch') : item.tone === 'info' ? t('cloudOverview.scale') : t('cloudOverview.ready')}</span>
            </div>
            <strong>{item.summary}</strong>
            <p>{item.detail}</p>
            <div className="cloud-pill-list">
              {item.stats.map((stat) => <span key={stat}>{stat}</span>)}
            </div>
          </section>
        ))}
      </div>
    </div>
  );
}


