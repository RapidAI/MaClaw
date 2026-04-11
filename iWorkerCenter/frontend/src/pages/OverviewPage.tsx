import { useEffect, useState } from 'react';
import { MetricCard } from '../components/cards/MetricCard';
import { SectionCard } from '../components/cards/SectionCard';
import { alerts as mockAlerts, metrics as mockMetrics, recentItems as mockRecent } from '../mock/dashboard';

type Metric = { label: string; value: string; hint: string };
type Item = { title: string; description: string; status: string };

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

export function OverviewPage() {
  const [metrics, setMetrics] = useState<Metric[]>(mockMetrics);
  const [recent, setRecent] = useState<Item[]>(mockRecent);
  const [alerts, setAlerts] = useState<Item[]>(mockAlerts);
  const [colleagues, setColleagues] = useState<number>(0);
  const [capabilities, setCaps] = useState<number>(0);
  const [memories, setMemories] = useState<number>(0);

  useEffect(() => {
    if (!hasWails()) return;
    // Fetch real counts from Wails bindings
    Promise.all([
      (window as any).go.main.App.ListColleagues().catch(() => []),
      (window as any).go.main.App.ListCapabilities().catch(() => []),
      (window as any).go.main.App.ListMemories().catch(() => []),
      (window as any).go.main.App.GetDashboardData().catch(() => null),
    ]).then(([cols, caps, mems, dashboard]) => {
      const colCount = Array.isArray(cols) ? cols.length : 0;
      const capCount = Array.isArray(caps) ? caps.length : 0;
      const memCount = Array.isArray(mems) ? mems.length : 0;
      setColleagues(colCount);
      setCaps(capCount);
      setMemories(memCount);

      // Build real metrics
      setMetrics([
        { label: '数字员工总数', value: String(colCount), hint: `已注册 ${colCount} 位数字员工` },
        { label: '能力包总数', value: String(capCount), hint: `已安装 ${capCount} 个能力包` },
        { label: '共享记忆条数', value: String(memCount), hint: `企业知识库 ${memCount} 条` },
      ]);

      // Use dashboard data for alerts/recent if available
      if (dashboard) {
        if (Array.isArray(dashboard.alerts) && dashboard.alerts.length > 0) {
          setAlerts(dashboard.alerts);
        }
        if (Array.isArray(dashboard.recent) && dashboard.recent.length > 0) {
          setRecent(dashboard.recent);
        }
      }
    });
  }, []);

  return (
    <div className="center-page-stack">
      <div className="metric-grid">
        {metrics.map((metric) => (
          <MetricCard key={metric.label} label={metric.label} value={metric.value} hint={metric.hint} />
        ))}
      </div>
      <div className="panel-grid">
        <SectionCard title="最近运行概况" desc="帮助管理员快速了解当前中心运行状态。">
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
        <SectionCard title="待处理告警" desc="优先处理影响协作和安全的事项。">
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
