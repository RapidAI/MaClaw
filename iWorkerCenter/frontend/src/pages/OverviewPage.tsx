import { useEffect, useState } from 'react';
import { MetricCard } from '../components/cards/MetricCard';
import { SectionCard } from '../components/cards/SectionCard';
import { alerts as mockAlerts, recentItems as mockRecent } from '../mock/dashboard';

type Metric = { label: string; value: string; hint: string };
type Item = { title: string; description: string; status: string };
type AuditStats = {
  total_requests: number;
  ok_count: number;
  error_count: number;
  avg_latency_ms: number;
  top_provider: string;
  top_work_type: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

export function OverviewPage() {
  const [metrics, setMetrics] = useState<Metric[]>([
    { label: '数字员工总数', value: '-', hint: '加载中...' },
    { label: '能力包总数', value: '-', hint: '加载中...' },
    { label: '共享记忆条数', value: '-', hint: '加载中...' },
  ]);
  const [extraMetrics, setExtraMetrics] = useState<Metric[]>([]);
  const [recent, setRecent] = useState<Item[]>(mockRecent);
  const [alerts, setAlerts] = useState<Item[]>(mockAlerts);

  useEffect(() => {
    if (!hasWails()) return;

    Promise.all([
      (window as any).go.main.App.ListColleagues().catch(() => []),
      (window as any).go.main.App.ListCapabilities().catch(() => []),
      (window as any).go.main.App.ListMemories().catch(() => []),
      (window as any).go.main.App.ListWorkflows().catch(() => []),
      (window as any).go.main.App.ListWorkflowInstances().catch(() => []),
      (window as any).go.main.App.ListCollaborations().catch(() => []),
      (window as any).go.main.App.GetAuditStats(24).catch(() => null),
      (window as any).go.main.App.GetDashboardData().catch(() => null),
    ]).then(([cols, caps, mems, workflows, instances, collabs, auditStats, dashboard]) => {
      const colCount = Array.isArray(cols) ? cols.length : 0;
      const capCount = Array.isArray(caps) ? caps.length : 0;
      const memCount = Array.isArray(mems) ? mems.length : 0;
      const wfCount = Array.isArray(workflows) ? workflows.length : 0;
      const instCount = Array.isArray(instances) ? instances.length : 0;
      const collabCount = Array.isArray(collabs) ? collabs.length : 0;

      setMetrics([
        { label: '数字员工', value: String(colCount), hint: `已注册 ${colCount} 位` },
        { label: '能力包', value: String(capCount), hint: `已安装 ${capCount} 个` },
        { label: '共享记忆', value: String(memCount), hint: `企业知识库 ${memCount} 条` },
        { label: '工作流模板', value: String(wfCount), hint: `${wfCount} 个流程模板` },
        { label: '流程实例', value: String(instCount), hint: `${instCount} 个运行实例` },
        { label: '协作任务', value: String(collabCount), hint: `${collabCount} 条协作记录` },
      ]);

      // Audit stats as extra metrics
      const stats = auditStats as AuditStats | null;
      if (stats && stats.total_requests > 0) {
        const successRate = ((stats.ok_count / stats.total_requests) * 100).toFixed(0);
        setExtraMetrics([
          { label: '今日 LLM 调用', value: String(stats.total_requests), hint: `成功率 ${successRate}%` },
          { label: '平均延迟', value: `${stats.avg_latency_ms}ms`, hint: stats.top_provider ? `最常用: ${stats.top_provider}` : '' },
        ]);
      }

      // Build real recent items from live data
      const recentItems: Item[] = [];
      if (instCount > 0) {
        const running = Array.isArray(instances) ? instances.filter((i: any) => i.status === 'running').length : 0;
        recentItems.push({ title: `${instCount} 个流程实例`, description: `其中 ${running} 个正在运行`, status: running > 0 ? '活跃' : '空闲' });
      }
      if (collabCount > 0) {
        const pending = Array.isArray(collabs) ? collabs.filter((c: any) => c.status === 'pending').length : 0;
        recentItems.push({ title: `${collabCount} 条协作任务`, description: `${pending} 条待处理`, status: pending > 0 ? '待处理' : '已处理' });
      }
      if (stats && stats.total_requests > 0) {
        recentItems.push({ title: `今日 ${stats.total_requests} 次 LLM 调用`, description: `失败 ${stats.error_count} 次，平均 ${stats.avg_latency_ms}ms`, status: stats.error_count > 0 ? '需关注' : '正常' });
      }
      if (recentItems.length > 0) {
        setRecent(recentItems);
      }

      // Dashboard alerts
      if (dashboard) {
        if (Array.isArray(dashboard.alerts) && dashboard.alerts.length > 0) {
          setAlerts(dashboard.alerts);
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
        {extraMetrics.map((metric) => (
          <MetricCard key={metric.label} label={metric.label} value={metric.value} hint={metric.hint} />
        ))}
      </div>
      <div className="panel-grid">
        <SectionCard title="运行概况" desc="实时数据，帮助管理员快速了解中心状态。">
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
