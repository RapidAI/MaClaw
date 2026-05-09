import { useEffect, useMemo, useState } from 'react';
import { MetricCard } from '../components/cards/MetricCard';
import { SectionCard } from '../components/cards/SectionCard';
import { fetchBootstrapStatus, isBootstrapComplete, type BootstrapStatus } from '../api/bootstrap';
import { fetchCloudStatus, type CloudStatus } from '../api/cloud';
import { fetchRuntimeStatus, type RuntimeStatus } from '../api/runtime';
import { useI18n } from '../i18n';
import type { CenterTab } from '../types';

type Metric = { label: string; value: string; hint: string };
type Item = { title: string; description: string; status: string; tone?: 'info' | 'warn' | 'ok' };
type Props = { onNavigate?: (tab: CenterTab) => void };
type CountResponse<T extends string> = Record<T, unknown[]>;
type AuditStats = { total_requests: number; ok_count: number; error_count: number; avg_latency_ms: number; top_work_type: string; top_error_work_type?: string };

async function requestJSON<T>(url: string): Promise<T | null> {
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    return await resp.json() as T;
  } catch {
    return null;
  }
}

const statusText = (status: string | undefined, t: (zh: string, en: string) => string) => {
  switch (status) {
    case 'ready': return t('就绪', 'Ready');
    case 'needs_bootstrap': return t('需要初始化', 'Needs bootstrap');
    case 'licensed': return t('已授权', 'Licensed');
    case 'pending': return t('待授权', 'Pending');
    case 'offline': return t('Cloud 离线', 'Cloud offline');
    case 'credential_mismatch': return t('凭据异常', 'Credential issue');
    case 'online': return t('在线', 'Online');
    case 'degraded': return t('降级', 'Degraded');
    case 'error': return t('异常', 'Error');
    case 'failure': return t('失败', 'Failure');
    case 'not_configured': return t('未配置', 'Not configured');
    default: return status || t('未知', 'Unknown');
  }
};

export function OverviewPage({ onNavigate }: Props) {
  const { language, t } = useI18n();
  const [metrics, setMetrics] = useState<Metric[]>([]);
  const [recent, setRecent] = useState<Item[]>([]);
  const [alerts, setAlerts] = useState<Item[]>([]);
  const [bootstrap, setBootstrap] = useState<BootstrapStatus | null>(null);
  const [cloud, setCloud] = useState<CloudStatus | null>(null);
  const [runtime, setRuntime] = useState<RuntimeStatus | null>(null);

  useEffect(() => {
    setMetrics([
      { label: t('数字员工', 'Digital colleagues'), value: '-', hint: t('正在加载', 'Loading') },
      { label: t('能力包', 'Capability packages'), value: '-', hint: t('正在加载', 'Loading') },
      { label: t('共享记忆', 'Shared memories'), value: '-', hint: t('正在加载', 'Loading') },
    ]);

    void Promise.all([
      fetchBootstrapStatus().catch(() => null),
      fetchCloudStatus().catch(() => null),
      fetchRuntimeStatus().catch(() => null),
      requestJSON<CountResponse<'colleagues'>>('/admin/colleagues'),
      requestJSON<CountResponse<'capabilities'>>('/admin/capabilities'),
      requestJSON<CountResponse<'memories'>>('/admin/memories'),
      requestJSON<CountResponse<'workflows'>>('/admin/workflows'),
      requestJSON<{ instances?: unknown[]; workflow_instances?: unknown[] }>('/admin/workflow-instances'),
      requestJSON<CountResponse<'collaborations'>>('/admin/collaborations'),
      requestJSON<AuditStats>('/admin/audit/stats?hours=24'),
    ]).then(([boot, cloudStatus, runtimeStatus, colleagues, caps, memories, workflows, instances, collabs, auditStats]) => {
      setBootstrap(boot);
      setCloud(cloudStatus);
      setRuntime(runtimeStatus);

      const colCount = colleagues?.colleagues?.length ?? 0;
      const capCount = caps?.capabilities?.length ?? 0;
      const memCount = memories?.memories?.length ?? 0;
      const wfCount = workflows?.workflows?.length ?? 0;
      const runCount = instances?.instances?.length ?? instances?.workflow_instances?.length ?? 0;
      const collabCount = collabs?.collaborations?.length ?? 0;
      const agentCount = runtimeStatus?.iworker_readiness?.agent_instance_count ?? 0;
      const cloudHeartbeat = runtimeStatus?.cloud_heartbeat;
      const cloudHeartbeatLabel = cloudHeartbeat
        ? `${statusText(cloudHeartbeat.status, t)} / ${cloudHeartbeat.non_blocking ? t('非阻塞', 'non-blocking') : t('阻塞', 'blocking')}`
        : t('未启用心跳', 'Heartbeat not enabled');

      setMetrics([
        { label: t('数字员工', 'Digital colleagues'), value: String(colCount), hint: colCount ? t(`${colCount} 位已登记`, `${colCount} registered`) : t('从初始化或数字员工页面创建', 'Create from bootstrap or the digital workforce page') },
        { label: t('运行实例', 'Runtime instances'), value: String(agentCount), hint: runtimeStatus?.iworker_readiness?.agent_runtime_ready ? t('iWorker 心跳正常', 'iWorker heartbeat is healthy') : t('等待 iWorker 心跳', 'Waiting for iWorker heartbeat') },
        { label: t('能力包', 'Capability packages'), value: String(capCount), hint: capCount ? t('Skill/MCP 可审核并下发', 'Skill/MCP packages can be reviewed and assigned') : t('等待安装或从 Cloud 导入', 'Waiting for install or Cloud import') },
        { label: t('共享记忆', 'Shared memories'), value: String(memCount), hint: memCount ? t(`${memCount} 条企业知识`, `${memCount} enterprise knowledge entries`) : t('可在经验共享中沉淀', 'Capture them in knowledge sharing') },
        { label: t('流程运行', 'Workflow runs'), value: String(runCount), hint: wfCount ? t(`${wfCount} 个流程模板`, `${wfCount} workflow templates`) : t('尚未创建流程模板', 'No workflow template yet') },
        { label: t('Cloud 心跳', 'Cloud heartbeat'), value: cloudHeartbeat ? statusText(cloudHeartbeat.status, t) : '-', hint: cloudHeartbeatLabel },
      ]);

      const recentItems: Item[] = [];
      if (runCount > 0) recentItems.push({ title: t(`${runCount} 个流程运行`, `${runCount} workflow runs`), description: t('可在流程设计中查看运行、卡点和人工介入记录。', 'Review execution, blockers, and human intervention from workflow design.'), status: t('流程', 'Workflow') });
      if (collabCount > 0) recentItems.push({ title: t(`${collabCount} 条协作任务`, `${collabCount} collaboration tasks`), description: t('数字员工协作和人工介入都汇总在 Center 侧。', 'Digital coworker collaboration and human intervention are summarized in Center.'), status: t('协作', 'Collaboration') });
      if (auditStats && auditStats.total_requests > 0) {
        const successRate = Math.round((auditStats.ok_count / auditStats.total_requests) * 100);
        recentItems.push({ title: t(`今日 ${auditStats.total_requests} 条审计事件`, `${auditStats.total_requests} audit events today`), description: auditStats.error_count > 0 ? t(`失败 ${auditStats.error_count} 条，热点：${auditStats.top_error_work_type || '未分类'}。`, `${auditStats.error_count} failed, hotspot: ${auditStats.top_error_work_type || 'unclassified'}.`) : t(`成功率 ${successRate}%，平均耗时 ${auditStats.avg_latency_ms}ms。`, `${successRate}% success rate, ${auditStats.avg_latency_ms}ms average latency.`), status: auditStats.error_count > 0 ? t('需关注', 'Needs attention') : t('健康', 'Healthy'), tone: auditStats.error_count > 0 ? 'warn' : 'ok' });
      }
      setRecent(recentItems.length ? recentItems : [{ title: t('暂无运行记录', 'No activity yet'), description: t('完成初始化并启动首批任务后，这里会显示 iWorker 与人类员工的协作进展。', 'After bootstrap and first tasks start, this area will show iWorker and human collaboration progress.'), status: t('待启动', 'Not started') }]);

      const nextAlerts: Item[] = [];
      if (cloudStatus?.cache_error) nextAlerts.push({ title: t('Cloud 本地缓存异常', 'Cloud local cache error'), description: cloudStatus.cache_error, status: t('需要处理', 'Needs attention'), tone: 'warn' });
      if (!isBootstrapComplete(boot)) nextAlerts.push({ title: t('当前租户尚未完成初始化', 'Current tenant is not bootstrapped'), description: t('先通过单位初始化向导创建组织骨架、首批 iWorker 和首批任务。', 'Use the bootstrap wizard to create the organization shape, first iWorkers, and first tasks.'), status: t('待处理', 'Pending'), tone: 'warn' });
      if (!cloudStatus?.registered) nextAlerts.push({ title: t('尚未注册到 iWorkerCloud', 'Not registered with iWorkerCloud'), description: t('本地业务继续运行；注册后可获得授权、算力协调和能力市场。', 'Local work continues; registration enables licensing, compute coordination, and the capability market.'), status: t('可稍后', 'Optional'), tone: 'info' });
      if (cloudStatus?.registered && cloudStatus.status !== 'licensed') nextAlerts.push({ title: t('Cloud 授权未就绪', 'Cloud authorization is not ready'), description: t('这只影响 Cloud 授权、算力和市场同步，不阻断 Center 到 iWorker 的本地任务、记忆和已下发能力。', 'This only affects Cloud licensing, compute, and marketplace sync; it does not block local Center-to-iWorker tasks, memory, or delivered capabilities.'), status: statusText(cloudStatus.status, t), tone: 'warn' });
      if (runtimeStatus?.compute_sync_status?.status === 'failure' && runtimeStatus.compute_sync_status.non_blocking) nextAlerts.push({ title: t('Cloud 算力同步失败，本地已降级', 'Cloud compute sync failed; local fallback is active'), description: t('Center 正在使用本地模型配置继续运行，Cloud 恢复后会重新同步。', 'Center continues with local model settings and will resync after Cloud recovers.'), status: t('非阻塞', 'Non-blocking'), tone: 'warn' });
      if (capCount === 0) nextAlerts.push({ title: t('还没有可下发能力包', 'No assignable capability package'), description: t('安装或导入 MCP/Skill 后，才能分配给 iWorker 客户端。', 'Install or import MCP/Skill packages before assigning them to iWorker clients.'), status: t('建议配置', 'Recommended'), tone: 'info' });
      setAlerts(nextAlerts.length ? nextAlerts : [{ title: t('关键链路正常', 'Core path looks healthy'), description: t('当前没有需要管理员立即处理的启动、授权或运行连续性事项。', 'No bootstrap, authorization, or continuity item needs immediate admin action.'), status: t('正常', 'Healthy'), tone: 'ok' }]);
    });
  }, [language, t]);

  const bootReady = isBootstrapComplete(bootstrap);
  const cloudReady = Boolean(cloud?.registered);
  const localContinuityReady = Boolean(runtime?.iworker_readiness?.ready || runtime?.compute_sync_status?.non_blocking || cloud?.non_blocking);
  const runtimeMode = runtime?.runtime_provider_mode || 'settings';
  const cloudHeartbeat = runtime?.cloud_heartbeat;

  const launchSteps = useMemo(() => [
    {
      number: '1',
      done: bootReady,
      title: t('单位初始化', 'Tenant bootstrap'),
      detail: bootReady ? t('组织骨架已就绪，可继续扩展 iWorker、流程和能力包。', 'The organization shape is ready; iWorkers, workflows, and packages can expand from here.') : t('需要先完成初始化向导。', 'Complete the bootstrap wizard first.'),
      action: t('打开初始化', 'Open bootstrap'),
      tab: 'bootstrap' as CenterTab,
    },
    {
      number: '2',
      done: localContinuityReady,
      title: t('本地业务连续', 'Local continuity'),
      detail: t('Cloud 离线时，Center/iWorker 仍按本地策略处理任务、记忆、MCP/Skill 和人工协作。', 'When Cloud is offline, Center/iWorker keep handling tasks, memory, MCP/Skill, and human collaboration by local policy.'),
      action: t('查看 iWorker', 'View iWorkers'),
      tab: 'employees' as CenterTab,
    },
    {
      number: '3',
      done: cloudReady,
      title: t('Cloud 注册', 'Cloud registration'),
      detail: cloudReady ? t('已保存注册身份，可查看授权、心跳和算力状态。', 'Registration identity is saved; licensing, heartbeat, and compute status can be reviewed.') : t('可稍后注册，不阻塞本地业务。', 'Registration can happen later and does not block local work.'),
      action: t('查看注册', 'View registration'),
      tab: 'cloud' as CenterTab,
    },
    {
      number: '4',
      done: metrics.some(metric => metric.label === t('能力包', 'Capability packages') && metric.value !== '0' && metric.value !== '-'),
      title: t('能力下发', 'Capability assignment'),
      detail: t('审核 Skill/MCP 后下发到对应 iWorker，并在客户端展示安装与启用状态。', 'Review Skill/MCP packages, assign them to iWorkers, and show installed/enabled status on the client.'),
      action: t('管理能力包', 'Manage packages'),
      tab: 'packages' as CenterTab,
    },
  ], [bootReady, cloudReady, localContinuityReady, metrics, t]);

  return (
    <div className="center-page-stack">
      <div className="metric-grid">
        {metrics.map(metric => <MetricCard key={metric.label} label={metric.label} value={metric.value} hint={metric.hint} />)}
      </div>

      <SectionCard title={t('上线链路', 'Launch path')} desc={t('Center 先完成本地初始化，再向 iWorker 下发任务、记忆和能力；Cloud 只承担注册、授权、算力协调和能力市场控制面。', 'Center bootstraps locally first, then delivers tasks, memory, and capabilities to iWorkers. Cloud remains the control plane for registration, licensing, compute coordination, and the capability marketplace.')}>
        <div className="cloud-step-list overview-step-list">
          {launchSteps.map(step => (
            <div key={step.number} className={step.done ? 'cloud-step is-done' : 'cloud-step is-pending'}>
              <span>{step.number}</span>
              <strong>{step.title}</strong>
              <p>{step.detail}</p>
              <button type="button" className="btn-secondary" onClick={() => onNavigate?.(step.tab)}>{step.action}</button>
            </div>
          ))}
        </div>
      </SectionCard>

      <div className="panel-grid">
        <SectionCard title={t('运行概况', 'Operations')} desc={t('从 Center 本地状态汇总 iWorker、流程、协作、审计和算力来源。', 'Summarizes iWorker, workflow, collaboration, audit, and compute source from local Center state.')}>
          <div className="item-list">
            <div className="item-row">
              <strong>{t('运行模式', 'Runtime mode')}</strong>
              <p>{t('当前算力来源', 'Current compute source')}: {runtime?.compute_source || t('本地配置', 'local settings')} / {runtimeMode}</p>
              <span className="badge info">{runtime?.provider_count ?? 0} {t('个 Provider', 'providers')}</span>
            </div>
            {recent.map(item => <div key={item.title} className="item-row"><strong>{item.title}</strong><p>{item.description}</p><span className={`badge ${item.tone === 'warn' ? 'warn' : 'info'}`}>{item.status}</span></div>)}
          </div>
        </SectionCard>

        <SectionCard title={t('连续性与告警', 'Continuity and attention')} desc={t('Cloud 失联只影响控制面同步；Center 到 iWorker 的本地业务链路应保持可用。', 'Cloud loss only affects control-plane sync; the local Center-to-iWorker business path should remain available.')}>
          <div className="item-list">
            <div className="item-row">
              <strong>{t('Cloud 心跳边界', 'Cloud heartbeat boundary')}</strong>
              <p>{cloudHeartbeat?.business_impact === 'none_local_center_and_iworker_continue' ? t('业务影响：无。Center/iWorker 本地链路继续运行。', 'Business impact: none. The local Center/iWorker path continues running.') : t('未启用 Cloud 心跳或暂无心跳快照。', 'Cloud heartbeat is not enabled or no snapshot is available yet.')}</p>
              <span className={`badge ${cloudHeartbeat?.status === 'online' ? 'info' : 'warn'}`}>{cloudHeartbeat ? statusText(cloudHeartbeat.status, t) : t('未启用', 'Disabled')}</span>
            </div>
            {alerts.map(item => <div key={item.title} className="item-row"><strong>{item.title}</strong><p>{item.description}</p><span className={`badge ${item.tone === 'ok' ? 'info' : item.tone === 'info' ? 'info' : 'warn'}`}>{item.status}</span></div>)}
          </div>
        </SectionCard>
      </div>
    </div>
  );
}
