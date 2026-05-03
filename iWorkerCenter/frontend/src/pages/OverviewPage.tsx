import { useEffect, useState } from 'react';
import { MetricCard } from '../components/cards/MetricCard';
import { SectionCard } from '../components/cards/SectionCard';
import { alerts as mockAlerts, recentItems as mockRecent } from '../mock/dashboard';
import { fetchBootstrapStatus, type BootstrapStatus } from '../api/bootstrap';
import { fetchCloudStatus, type CloudStatus } from '../api/cloud';

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
  const [bootstrap, setBootstrap] = useState<BootstrapStatus | null>(null);
  const [cloud, setCloud] = useState<CloudStatus | null>(null);

  useEffect(() => {
    void Promise.all([
      fetchBootstrapStatus().catch(() => null),
      fetchCloudStatus().catch(() => null),
    ]).then(([boot, cloudStatus]) => {
      setBootstrap(boot);
      setCloud(cloudStatus);
    });
  }, []);

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
        { label: '数字员工', value: String(colCount), hint: '已注册 ' + colCount + ' 位' },
        { label: '能力包', value: String(capCount), hint: '已安装 ' + capCount + ' 个 Skill/MCP' },
        { label: '共享记忆', value: String(memCount), hint: '企业知识库 ' + memCount + ' 条' },
        { label: '流程模板', value: String(wfCount), hint: wfCount + ' 个流程模板' },
        { label: '流程实例', value: String(instCount), hint: instCount + ' 个运行实例' },
        { label: '协作任务', value: String(collabCount), hint: collabCount + ' 条协作记录' },
      ]);

      const stats = auditStats as AuditStats | null;
      if (stats && stats.total_requests > 0) {
        const successRate = ((stats.ok_count / stats.total_requests) * 100).toFixed(0);
        setExtraMetrics([
          { label: '今日 LLM 调用', value: String(stats.total_requests), hint: '成功率 ' + successRate + '%' },
          { label: '平均延迟', value: stats.avg_latency_ms + 'ms', hint: stats.top_provider ? '最常用: ' + stats.top_provider : '' },
        ]);
      }

      const recentItems: Item[] = [];
      if (instCount > 0) {
        const running = Array.isArray(instances) ? instances.filter((item: any) => item.status === 'running').length : 0;
        recentItems.push({ title: instCount + ' 个流程实例', description: '其中 ' + running + ' 个正在运行', status: running > 0 ? '活跃' : '空闲' });
      }
      if (collabCount > 0) {
        const pending = Array.isArray(collabs) ? collabs.filter((item: any) => item.status === 'pending').length : 0;
        recentItems.push({ title: collabCount + ' 条协作任务', description: pending + ' 条待处理', status: pending > 0 ? '待处理' : '已处理' });
      }
      if (stats && stats.total_requests > 0) {
        recentItems.push({ title: '今日 ' + stats.total_requests + ' 次 LLM 调用', description: '失败 ' + stats.error_count + ' 次，平均延迟 ' + stats.avg_latency_ms + 'ms', status: stats.error_count > 0 ? '需关注' : '正常' });
      }
      if (recentItems.length > 0) setRecent(recentItems);

      if (dashboard && Array.isArray(dashboard.alerts) && dashboard.alerts.length > 0) {
        setAlerts(dashboard.alerts);
      }
    });
  }, []);

  const bootReady = Boolean(bootstrap?.ready_to_start || bootstrap?.last_run || bootstrap?.applied_assets?.length);
  const cloudReady = Boolean(cloud?.registered);

  return (
    <div className="center-page-stack">
      <div className="metric-grid">
        {metrics.map((metric) => <MetricCard key={metric.label} label={metric.label} value={metric.value} hint={metric.hint} />)}
        {extraMetrics.map((metric) => <MetricCard key={metric.label} label={metric.label} value={metric.value} hint={metric.hint} />)}
      </div>

      <SectionCard title="上线链路" desc="Center 先本地 Bootstrap，再下发能力给 iWorker；Cloud 只作为注册、授权、算力和能力市场控制面。">
        <div className="cloud-step-list">
          <div className={bootReady ? 'cloud-step is-done' : 'cloud-step is-pending'}><span>1</span><strong>单位初始化</strong><p>{bootReady ? '组织骨架已具备，可继续扩展 iWorker 和流程。' : '需要先保存并应用启动计划。'}</p></div>
          <div className="cloud-step is-done"><span>2</span><strong>本地业务连续</strong><p>Center/iWorker 本地运行不依赖 Cloud 在线。</p></div>
          <div className={cloudReady ? 'cloud-step is-done' : 'cloud-step is-pending'}><span>3</span><strong>Cloud 注册</strong><p>{cloudReady ? '已获得 Cloud 授权和控制面心跳。' : '可稍后注册，不阻断本地业务。'}</p></div>
          <div className="cloud-step is-pending"><span>4</span><strong>能力下发</strong><p>审核 Skill/MCP 后下发到对应 iWorker，并在客户端展示状态。</p></div>
        </div>
      </SectionCard>

      <div className="panel-grid">
        <SectionCard title="运行概况" desc="实时数据帮助管理员快速了解 Center 状态。">
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
        <SectionCard title="待处理告警" desc="优先处理影响协作、安全和运行连续性的事项。">
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
