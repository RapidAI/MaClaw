import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { getCenterManagement, type CenterManagementReport } from '../api/centers';

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
    multi_tenant_centers: 0,
    tenant_count: 0,
    unlicensed_centers: 0,
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
      label: 'Ready centers',
      value: String(summary.ready_centers),
      hint: 'Active, licensed, multi-tenant, reachable iWorkerCenter instances ready for iWorkerCenter management services.',
    },
    {
      label: 'Needs setup',
      value: String(summary.needs_setup),
      hint: 'Centers missing base URL or multi-tenant capability confirmation.',
    },
    {
      label: 'Probe failures',
      value: String(summary.probe_failures),
      hint: 'Centers whose latest cloud-side health probe failed.',
    },
    {
      label: 'Multi-tenant',
      value: String(summary.multi_tenant_centers),
      hint: 'Connected deployments declared ready for tenant-aware management.',
    },
    {
      label: 'Tenants tracked',
      value: String(summary.tenant_count),
      hint: 'Customer tenants tracked for licensing and platform service purposes only.',
    },
    {
      label: 'Unlicensed',
      value: String(summary.unlicensed_centers),
      hint: 'Centers without an active commercial or trial entitlement.',
    },
  ], [summary]);

  const pillars = useMemo<CloudPillar[]>(() => [
    {
      eyebrow: t('cloudOverview.computeEyebrow'),
      title: t('cloudOverview.computeTitle'),
      tone: summary.probe_failures > 0 ? 'warn' : 'info',
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
            <span className="mini">Service control radar</span>
            <h3>Center service readiness</h3>
          </div>
          <span className={`badge ${summary.probe_failures || summary.needs_setup || summary.unlicensed_centers ? 'warn' : 'ok'}`}>
            {summary.probe_failures || summary.needs_setup || summary.unlicensed_centers ? 'Watch' : 'Ready'}
          </span>
        </div>
        <p>Cloud should know which connected iWorkerCenters are commercially authorized, reachable, multi-tenant capable, and ready for our iWorkerCenter management services. It does not participate in customer company management or enterprise operations.</p>
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


