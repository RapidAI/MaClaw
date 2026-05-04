import { useEffect, useState } from 'react';
import { MetricCard } from '../components/cards/MetricCard';
import { SectionCard } from '../components/cards/SectionCard';
import { alerts as mockAlerts, recentItems as mockRecent } from '../mock/dashboard';
import { fetchBootstrapStatus, isBootstrapComplete, type BootstrapStatus } from '../api/bootstrap';
import { fetchCloudStatus, type CloudStatus } from '../api/cloud';
import type { CenterTab } from '../types';

type Metric = { label: string; value: string; hint: string };
type Item = { title: string; description: string; status: string };
type Props = { onNavigate?: (tab: CenterTab) => void };
type AuditStats = { total_requests: number; ok_count: number; error_count: number; avg_latency_ms: number; top_provider: string; top_work_type: string };
type CountResponse<T extends string> = Record<T, unknown[]>;

async function requestJSON<T>(url: string): Promise<T | null> {
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    return await resp.json() as T;
  } catch {
    return null;
  }
}

export function OverviewPage({ onNavigate }: Props) {
  const [metrics, setMetrics] = useState<Metric[]>([
    { label: '数字员工', value: '-', hint: '正在加载' },
    { label: '能力包', value: '-', hint: '正在加载' },
    { label: '共享记忆', value: '-', hint: '正在加载' },
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
      requestJSON<CountResponse<'colleagues'>>('/admin/colleagues'),
      requestJSON<CountResponse<'capabilities'>>('/admin/capabilities'),
      requestJSON<CountResponse<'memories'>>('/admin/memories'),
      requestJSON<CountResponse<'workflows'>>('/admin/workflows'),
      requestJSON<{ instances?: unknown[]; workflow_instances?: unknown[] }>('/admin/workflow-instances'),
      requestJSON<CountResponse<'collaborations'>>('/admin/collaborations'),
      requestJSON<AuditStats>('/admin/audit/stats?hours=24'),
    ]).then(([boot, cloudStatus, colleagues, caps, memories, workflows, instances, collabs, auditStats]) => {
      setBootstrap(boot);
      setCloud(cloudStatus);

      const colCount = colleagues?.colleagues?.length ?? 0;
      const capCount = caps?.capabilities?.length ?? 0;
      const memCount = memories?.memories?.length ?? 0;
      const wfCount = workflows?.workflows?.length ?? 0;
      const instCount = instances?.instances?.length ?? instances?.workflow_instances?.length ?? 0;
      const collabCount = collabs?.collaborations?.length ?? 0;

      setMetrics([
        { label: '数字员工', value: String(colCount), hint: colCount ? '已登记 ' + colCount + ' 位' : '可通过 bootstrap 或数字员工页创建' },
        { label: '能力包', value: String(capCount), hint: capCount ? 'Skill/MCP 可审核和下发' : '等待安装或从 Cloud 导入' },
        { label: '共享记忆', value: String(memCount), hint: memCount ? '企业知识库 ' + memCount + ' 条' : '可在经验共享页沉淀' },
        { label: '流程模板', value: String(wfCount), hint: wfCount ? wfCount + ' 个流程模板' : 'bootstrap 可创建首批模板' },
        { label: '流程实例', value: String(instCount), hint: instCount ? instCount + ' 个运行实例' : '首批任务尚未启动' },
        { label: '协作任务', value: String(collabCount), hint: collabCount ? collabCount + ' 条协作记录' : '暂无待处理协作' },
      ]);

      if (auditStats && auditStats.total_requests > 0) {
        const successRate = ((auditStats.ok_count / auditStats.total_requests) * 100).toFixed(0);
        setExtraMetrics([
          { label: '今日 LLM 调用', value: String(auditStats.total_requests), hint: '成功率 ' + successRate + '%' },
          { label: '平均延迟', value: auditStats.avg_latency_ms + 'ms', hint: auditStats.top_provider ? '最常用：' + auditStats.top_provider : '暂无主供应商' },
        ]);
      } else {
        setExtraMetrics([]);
      }

      const recentItems: Item[] = [];
      if (instCount > 0) recentItems.push({ title: instCount + ' 个流程实例', description: '可在流程设计中查看运行和卡点。', status: '运行记录' });
      if (collabCount > 0) recentItems.push({ title: collabCount + ' 条协作任务', description: '数字员工协作和人工介入都在这里汇总。', status: '协作' });
      if (auditStats && auditStats.total_requests > 0) recentItems.push({ title: '今日 ' + auditStats.total_requests + ' 次 LLM 调用', description: '失败 ' + auditStats.error_count + ' 次，平均延迟 ' + auditStats.avg_latency_ms + 'ms。', status: auditStats.error_count > 0 ? '需关注' : '正常' });
      if (recentItems.length > 0) setRecent(recentItems);

      const nextAlerts: Item[] = [];
      if (!isBootstrapComplete(boot)) nextAlerts.push({ title: '当前租户尚未完成初始化', description: '建议先通过单位初始化向导创建组织骨架、首批 iWorker 和首批任务。', status: '待处理' });
      if (!cloudStatus?.registered) nextAlerts.push({ title: '尚未注册到 iWorkerCloud', description: '不影响本地运行；注册后可获得授权、算力协调和能力市场。', status: '可稍后' });
      if (capCount === 0) nextAlerts.push({ title: '还没有可下发能力包', description: '安装或导入 MCP/Skill 后，才能分配给 iWorker 客户端。', status: '建议配置' });
      setAlerts(nextAlerts.length ? nextAlerts : mockAlerts);
    });
  }, []);

  const bootReady = isBootstrapComplete(bootstrap);
  const cloudReady = Boolean(cloud?.registered);

  return (
    <div className="center-page-stack">
      <div className="metric-grid">
        {metrics.map((metric) => <MetricCard key={metric.label} label={metric.label} value={metric.value} hint={metric.hint} />)}
        {extraMetrics.map((metric) => <MetricCard key={metric.label} label={metric.label} value={metric.value} hint={metric.hint} />)}
      </div>

      <SectionCard title="上线链路" desc="Center 先本地 Bootstrap，再下发能力给 iWorker；Cloud 只作为注册、授权、算力和能力市场控制面。">
        <div className="cloud-step-list overview-step-list">
          <div className={bootReady ? 'cloud-step is-done' : 'cloud-step is-pending'}>
            <span>1</span><strong>单位初始化</strong><p>{bootReady ? '组织骨架已具备，可继续扩展 iWorker 和流程。' : '需要先保存并应用启动计划。'}</p>
            <button type="button" className="btn-secondary" onClick={() => onNavigate?.('bootstrap')}>{bootReady ? '查看初始化' : '打开向导'}</button>
          </div>
          <div className="cloud-step is-done">
            <span>2</span><strong>本地业务连续</strong><p>Center/iWorker 本地运行不依赖 Cloud 在线。</p>
            <button type="button" className="btn-secondary" onClick={() => onNavigate?.('employees')}>查看 iWorker</button>
          </div>
          <div className={cloudReady ? 'cloud-step is-done' : 'cloud-step is-pending'}>
            <span>3</span><strong>Cloud 注册</strong><p>{cloudReady ? '已获得 Cloud 授权和控制面心跳。' : '可稍后注册，不阻断本地业务。'}</p>
            <button type="button" className="btn-secondary" onClick={() => onNavigate?.('cloud')}>{cloudReady ? '查看注册' : '去注册'}</button>
          </div>
          <div className="cloud-step is-pending">
            <span>4</span><strong>能力下发</strong><p>审核 Skill/MCP 后下发到对应 iWorker，并在客户端展示状态。</p>
            <button type="button" className="btn-secondary" onClick={() => onNavigate?.('packages')}>管理能力包</button>
          </div>
        </div>
      </SectionCard>

      <div className="panel-grid">
        <SectionCard title="运行概况" desc="实时数据帮助管理员快速了解 Center 状态。">
          <div className="item-list">
            {recent.map((item) => <div key={item.title} className="item-row"><strong>{item.title}</strong><p>{item.description}</p><span className="badge info">{item.status}</span></div>)}
          </div>
        </SectionCard>
        <SectionCard title="待处理告警" desc="优先处理影响协作、安全和运行连续性的事项。">
          <div className="item-list">
            {alerts.map((item) => <div key={item.title} className="item-row"><strong>{item.title}</strong><p>{item.description}</p><span className="badge warn">{item.status}</span></div>)}
          </div>
        </SectionCard>
      </div>
    </div>
  );
}
