import { useEffect, useState } from 'react';
import { MetricCard } from '../components/cards/MetricCard';
import { SectionCard } from '../components/cards/SectionCard';
import { fetchBootstrapStatus, isBootstrapComplete, type BootstrapStatus } from '../api/bootstrap';
import { fetchCloudStatus, type CloudStatus } from '../api/cloud';
import { useI18n } from '../i18n';
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
  const { language, t } = useI18n();
  const loading = t('正在加载', 'Loading');
  const [metrics, setMetrics] = useState<Metric[]>([
    { label: t('数字员工', 'Digital colleagues'), value: '-', hint: loading },
    { label: t('能力包', 'Capability packages'), value: '-', hint: loading },
    { label: t('共享记忆', 'Shared memories'), value: '-', hint: loading },
  ]);
  const [extraMetrics, setExtraMetrics] = useState<Metric[]>([]);
  const [recent, setRecent] = useState<Item[]>([]);
  const [alerts, setAlerts] = useState<Item[]>([]);
  const [bootstrap, setBootstrap] = useState<BootstrapStatus | null>(null);
  const [cloud, setCloud] = useState<CloudStatus | null>(null);

  useEffect(() => {
    setMetrics([
      { label: t('数字员工', 'Digital colleagues'), value: '-', hint: t('正在加载', 'Loading') },
      { label: t('能力包', 'Capability packages'), value: '-', hint: t('正在加载', 'Loading') },
      { label: t('共享记忆', 'Shared memories'), value: '-', hint: t('正在加载', 'Loading') },
    ]);

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
        { label: t('数字员工', 'Digital colleagues'), value: String(colCount), hint: colCount ? t('已登记 ' + colCount + ' 位', colCount + ' registered') : t('可通过初始化向导或数字员工页创建', 'Create them from bootstrap or the digital colleagues page') },
        { label: t('能力包', 'Capability packages'), value: String(capCount), hint: capCount ? t('Skill/MCP 可审核和下发', 'Skill/MCP packages can be reviewed and assigned') : t('等待安装或从 Cloud 导入', 'Waiting for local install or Cloud import') },
        { label: t('共享记忆', 'Shared memories'), value: String(memCount), hint: memCount ? t('企业知识库 ' + memCount + ' 条', memCount + ' enterprise knowledge entries') : t('可在经验共享页沉淀', 'Capture them in the knowledge sharing page') },
        { label: t('流程模板', 'Workflow templates'), value: String(wfCount), hint: wfCount ? t(wfCount + ' 个流程模板', wfCount + ' workflow templates') : t('初始化可创建首批模板', 'Bootstrap can create the first templates') },
        { label: t('流程实例', 'Workflow runs'), value: String(instCount), hint: instCount ? t(instCount + ' 个运行实例', instCount + ' active or historical runs') : t('首批任务尚未启动', 'No first-wave task has started yet') },
        { label: t('协作任务', 'Collaboration tasks'), value: String(collabCount), hint: collabCount ? t(collabCount + ' 条协作记录', collabCount + ' collaboration records') : t('暂无待处理协作', 'No collaboration item needs attention') },
      ]);

      if (auditStats && auditStats.total_requests > 0) {
        const successRate = ((auditStats.ok_count / auditStats.total_requests) * 100).toFixed(0);
        setExtraMetrics([
          { label: t('今日 LLM 调用', 'LLM calls today'), value: String(auditStats.total_requests), hint: t('成功率 ' + successRate + '%', successRate + '% success rate') },
          { label: t('平均延迟', 'Average latency'), value: auditStats.avg_latency_ms + 'ms', hint: auditStats.top_provider ? t('最常用：' + auditStats.top_provider, 'Most used: ' + auditStats.top_provider) : t('暂无主供应商', 'No primary provider yet') },
        ]);
      } else {
        setExtraMetrics([]);
      }

      const recentItems: Item[] = [];
      if (instCount > 0) recentItems.push({ title: t(instCount + ' 个流程实例', instCount + ' workflow runs'), description: t('可在流程设计中查看运行、卡点和人工介入记录。', 'Review execution, blockers, and human intervention from workflow design.'), status: t('运行记录', 'Runs') });
      if (collabCount > 0) recentItems.push({ title: t(collabCount + ' 条协作任务', collabCount + ' collaboration tasks'), description: t('数字员工协作和人工介入都在这里汇总。', 'Digital colleague collaboration and human intervention are summarized here.'), status: t('协作', 'Collaboration') });
      if (auditStats && auditStats.total_requests > 0) recentItems.push({ title: t('今日 ' + auditStats.total_requests + ' 次 LLM 调用', auditStats.total_requests + ' LLM calls today'), description: t('失败 ' + auditStats.error_count + ' 次，平均延迟 ' + auditStats.avg_latency_ms + 'ms。', auditStats.error_count + ' failed, average latency ' + auditStats.avg_latency_ms + 'ms.'), status: auditStats.error_count > 0 ? t('需关注', 'Needs attention') : t('正常', 'Healthy') });
      setRecent(recentItems.length ? recentItems : [{ title: t('暂无运行记录', 'No activity yet'), description: t('完成初始化并启动首批任务后，这里会显示 iWorker 与人类员工的协作进展。', 'After bootstrap and first-wave tasks start, this area will show collaboration progress.'), status: t('待启动', 'Not started') }]);

      const nextAlerts: Item[] = [];
      if (!isBootstrapComplete(boot)) nextAlerts.push({ title: t('当前租户尚未完成初始化', 'Current tenant is not bootstrapped'), description: t('建议先通过单位初始化向导创建组织骨架、首批 iWorker 和首批任务。', 'Use the bootstrap wizard to create the organization shape, first iWorkers, and first tasks.'), status: t('待处理', 'Pending') });
      if (!cloudStatus?.registered) nextAlerts.push({ title: t('尚未注册到 iWorkerCloud', 'Not registered with iWorkerCloud'), description: t('不影响本地运行；注册后可获得授权、算力协调和能力市场。', 'Local work continues; registration enables licensing, compute coordination, and the capability market.'), status: t('可稍后', 'Optional') });
      if (capCount === 0) nextAlerts.push({ title: t('还没有可下发能力包', 'No assignable capability package'), description: t('安装或导入 MCP/Skill 后，才能分配给 iWorker 客户端。', 'Install or import MCP/Skill packages before assigning them to iWorker clients.'), status: t('建议配置', 'Recommended') });
      setAlerts(nextAlerts.length ? nextAlerts : [{ title: t('关键链路正常', 'Core path looks healthy'), description: t('当前没有需要管理员立即处理的启动或注册事项。', 'There is no bootstrap or registration item that needs immediate admin action.'), status: t('正常', 'Healthy') }]);
    });
  }, [language, t]);

  const bootReady = isBootstrapComplete(bootstrap);
  const cloudReady = Boolean(cloud?.registered);

  return (
    <div className="center-page-stack">
      <div className="metric-grid">
        {metrics.map((metric) => <MetricCard key={metric.label} label={metric.label} value={metric.value} hint={metric.hint} />)}
        {extraMetrics.map((metric) => <MetricCard key={metric.label} label={metric.label} value={metric.value} hint={metric.hint} />)}
      </div>

      <SectionCard title={t('上线链路', 'Launch Path')} desc={t('Center 先完成本地初始化，再向 iWorker 下发能力；Cloud 只承担注册、授权、算力协调和能力市场控制面。', 'Center bootstraps locally first, then assigns capabilities to iWorkers. Cloud remains the control plane for registration, licensing, compute coordination, and the capability market.')}>
        <div className="cloud-step-list overview-step-list">
          <div className={bootReady ? 'cloud-step is-done' : 'cloud-step is-pending'}>
            <span>1</span><strong>{t('单位初始化', 'Tenant Bootstrap')}</strong><p>{bootReady ? t('组织骨架已经具备，可继续扩展 iWorker 和流程。', 'The organization shape is ready; iWorkers and workflows can expand from here.') : t('需要先保存并应用启动计划。', 'Save and apply the bootstrap plan first.')}</p>
            <button type="button" className="btn-secondary" onClick={() => onNavigate?.('bootstrap')}>{bootReady ? t('查看初始化', 'View bootstrap') : t('打开向导', 'Open wizard')}</button>
          </div>
          <div className="cloud-step is-done">
            <span>2</span><strong>{t('本地业务连续', 'Local Continuity')}</strong><p>{t('Center/iWorker 本地运行不依赖 Cloud 在线。', 'Center and iWorker keep running locally when Cloud is offline.')}</p>
            <button type="button" className="btn-secondary" onClick={() => onNavigate?.('employees')}>{t('查看 iWorker', 'View iWorkers')}</button>
          </div>
          <div className={cloudReady ? 'cloud-step is-done' : 'cloud-step is-pending'}>
            <span>3</span><strong>{t('Cloud 注册', 'Cloud Registration')}</strong><p>{cloudReady ? t('已获得 Cloud 注册状态，可查看授权和算力信息。', 'Cloud registration is available; licensing and compute status can be reviewed.') : t('可稍后注册，不阻断本地业务。', 'Registration can happen later and does not block local work.')}</p>
            <button type="button" className="btn-secondary" onClick={() => onNavigate?.('cloud')}>{cloudReady ? t('查看注册', 'View registration') : t('去注册', 'Register')}</button>
          </div>
          <div className="cloud-step is-pending">
            <span>4</span><strong>{t('能力下发', 'Capability Assignment')}</strong><p>{t('审核 Skill/MCP 后下发到对应 iWorker，并在客户端展示安装与启用状态。', 'Review Skill/MCP packages, assign them to iWorkers, and show installed/enabled status on the client.')}</p>
            <button type="button" className="btn-secondary" onClick={() => onNavigate?.('packages')}>{t('管理能力包', 'Manage packages')}</button>
          </div>
        </div>
      </SectionCard>

      <div className="panel-grid">
        <SectionCard title={t('运行概况', 'Operations')} desc={t('实时数据帮助管理员快速了解 Center 状态。', 'Live data helps administrators understand Center status quickly.')}>
          <div className="item-list">
            {recent.map((item) => <div key={item.title} className="item-row"><strong>{item.title}</strong><p>{item.description}</p><span className="badge info">{item.status}</span></div>)}
          </div>
        </SectionCard>
        <SectionCard title={t('待处理告警', 'Attention Queue')} desc={t('优先处理影响协作、安全和运行连续性的事项。', 'Prioritize items that affect collaboration, security, or continuity.')}>
          <div className="item-list">
            {alerts.map((item) => <div key={item.title} className="item-row"><strong>{item.title}</strong><p>{item.description}</p><span className="badge warn">{item.status}</span></div>)}
          </div>
        </SectionCard>
      </div>
    </div>
  );
}
