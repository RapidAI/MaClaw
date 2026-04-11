import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { MetricCard } from '../components/cards/MetricCard';
import { SectionCard } from '../components/cards/SectionCard';
import { fetchDashboard } from '../api/dashboard';
import { listColleagues } from '../api/colleagues';
import { listCapabilities } from '../api/capabilities';
import { listMemories } from '../api/memories';
import type { Metric, DashboardItem } from '../types';

export function OverviewPage() {
  const { t } = useTranslation();
  const [metrics, setMetrics] = useState<Metric[]>([]);
  const [alerts, setAlerts] = useState<DashboardItem[]>([]);
  const [recent, setRecent] = useState<DashboardItem[]>([]);

  useEffect(() => {
    Promise.all([
      listColleagues().catch(() => []),
      listCapabilities().catch(() => []),
      listMemories().catch(() => []),
      fetchDashboard().catch(() => null),
    ]).then(([cols, caps, mems, dashboard]) => {
      setMetrics([
        { label: t('nav.employees'), value: String(cols.length), hint: `${cols.length}` },
        { label: t('nav.packages'), value: String(caps.length), hint: `${caps.length}` },
        { label: t('nav.knowledge'), value: String(mems.length), hint: `${mems.length}` },
      ]);
      if (dashboard) {
        setAlerts(dashboard.alerts || []);
        setRecent(dashboard.recent || []);
      }
    });
  }, [t]);

  return (
    <div className="center-page-stack">
      <div className="metric-grid">
        {metrics.map((m) => (
          <MetricCard key={m.label} label={m.label} value={m.value} hint={m.hint} />
        ))}
      </div>
      <div className="panel-grid">
        <SectionCard title={t('nav.overview')}>
          <div className="item-list">
            {recent.map((item) => (
              <div key={item.title} className="item-row">
                <strong>{item.title}</strong>
                <p>{item.description}</p>
                <span className="badge info">{item.status}</span>
              </div>
            ))}
          </div>
        </SectionCard>
        <SectionCard title={t('common.error')}>
          <div className="item-list">
            {alerts.map((item) => (
              <div key={item.title} className="item-row">
                <strong>{item.title}</strong>
                <p>{item.description}</p>
                <span className="badge warn">{item.status}</span>
              </div>
            ))}
          </div>
        </SectionCard>
      </div>
    </div>
  );
}
