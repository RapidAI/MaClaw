import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { useI18n } from '../i18n';

type Colleague = {
  id: string;
  name: string;
  role_id?: string;
  role_name?: string;
  role_code?: string;
  description?: string;
  status?: string;
  strengths?: string[];
  tasks?: string[];
  created_at?: string;
};

type Role = {
  id: string;
  name: string;
  code: string;
  status: string;
  default_strengths?: string[];
  applicable_tasks?: string[];
};

type WorkStatus = {
  current_task?: string;
  current_detail?: string;
  active_count?: number;
  completed_count?: number;
  review_count?: number;
  blocked_count?: number;
  updated_at?: string;
};

type RuntimeInstance = {
  worker_id: string;
  instance_id: string;
  role: string;
  status: string;
  effective_status?: string;
  capabilities?: string[];
  memory_authority?: string;
  local_cache_mode?: string;
  work_status?: WorkStatus;
  last_heartbeat_at?: string;
  heartbeat_age_seconds?: number;
};

type EmployeeRow = {
  name: string;
  role: string;
  description: string;
  strengths: string;
  runtime: string;
  status: string;
  created: string;
};

type Message = { kind: 'ok' | 'warn' | 'danger'; text: string } | null;

const emptyForm = { name: '', role_id: '', description: '', strengths: '', tasks: '' };

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) throw new Error(data?.error?.message || data?.message || 'Request failed: ' + resp.status);
  return data as T;
}

const listFromText = (value: string) => value
  .replaceAll(String.fromCharCode(13), '')
  .replaceAll(String.fromCharCode(10), ',')
  .split(',')
  .map(item => item.trim())
  .filter(Boolean);

const formatTime = (value?: string) => {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
};

const aggregateWorkStatus = (items: RuntimeInstance[]): WorkStatus => items.reduce<WorkStatus>((acc, item) => {
  const work = item.work_status;
  if (!work) return acc;
  return {
    current_task: acc.current_task || work.current_task,
    current_detail: acc.current_detail || work.current_detail,
    active_count: Math.max(acc.active_count || 0, work.active_count || 0),
    completed_count: Math.max(acc.completed_count || 0, work.completed_count || 0),
    review_count: Math.max(acc.review_count || 0, work.review_count || 0),
    blocked_count: Math.max(acc.blocked_count || 0, work.blocked_count || 0),
    updated_at: [acc.updated_at, work.updated_at].filter(Boolean).sort().at(-1),
  };
}, {});

const pickPrimaryRuntime = (items: RuntimeInstance[]) => [...items].sort((a, b) => {
  const statusRank = (value?: string) => ({ busy: 0, online: 1, idle: 2, degraded: 3, offline: 4 }[value || ''] ?? 5);
  const statusDelta = statusRank(a.effective_status || a.status) - statusRank(b.effective_status || b.status);
  if (statusDelta !== 0) return statusDelta;
  return (a.heartbeat_age_seconds ?? Number.MAX_SAFE_INTEGER) - (b.heartbeat_age_seconds ?? Number.MAX_SAFE_INTEGER);
})[0];

export function EmployeesPage() {
  const { t } = useI18n();
  const [colleagues, setColleagues] = useState<Colleague[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [instances, setInstances] = useState<RuntimeInstance[]>([]);
  const [form, setForm] = useState(emptyForm);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message>(null);

  const statusLabel = (status?: string) => {
    switch (status) {
      case 'active': return t('启用', 'Active');
      case 'disabled': return t('停用', 'Disabled');
      case 'offline': return t('离线', 'Offline');
      default: return status || t('未知', 'Unknown');
    }
  };

  const runtimeStatusLabel = (status?: string) => {
    switch (status) {
      case 'online': return t('在线', 'Online');
      case 'busy': return t('忙碌', 'Busy');
      case 'idle': return t('空闲', 'Idle');
      case 'degraded': return t('降级', 'Degraded');
      case 'offline': return t('离线', 'Offline');
      default: return status || t('无心跳', 'No heartbeat');
    }
  };

  const load = async () => {
    setLoading(true);
    setMessage(null);
    try {
      const [colResp, roleResp, runtimeResp] = await Promise.all([
        requestJSON<{ colleagues?: Colleague[] }>('/admin/colleagues'),
        requestJSON<{ roles?: Role[] }>('/admin/roles'),
        requestJSON<{ instances?: RuntimeInstance[] }>('/admin/iworker/instances?offline_after_seconds=120').catch(() => ({ instances: [] as RuntimeInstance[] })),
      ]);
      const activeRoles = (roleResp.roles || []).filter(role => role.status !== 'disabled');
      setColleagues(colResp.colleagues || []);
      setRoles(activeRoles);
      setInstances(runtimeResp.instances || []);
      setForm(current => current.role_id || activeRoles.length === 0 ? current : { ...current, role_id: activeRoles[0].id });
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('加载 iWorker 失败。', 'Failed to load iWorkers.') });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const runtimeByWorker = useMemo(() => {
    const grouped = new Map<string, RuntimeInstance[]>();
    for (const instance of instances) {
      const list = grouped.get(instance.worker_id) || [];
      list.push(instance);
      grouped.set(instance.worker_id, list);
    }
    return grouped;
  }, [instances]);

  const summary = useMemo(() => {
    const active = colleagues.filter(row => row.status === 'active' || !row.status).length;
    const disabled = colleagues.filter(row => row.status === 'disabled' || row.status === 'offline').length;
    const roleCount = new Set(colleagues.map(row => row.role_id || row.role_code).filter(Boolean)).size;
    const online = instances.filter(row => ['online', 'busy', 'idle'].includes(row.effective_status || row.status)).length;
    const workerWork = Array.from(runtimeByWorker.values()).map(aggregateWorkStatus);
    const blocked = workerWork.reduce((sum, work) => sum + (work.blocked_count || 0), 0);
    const review = workerWork.reduce((sum, work) => sum + (work.review_count || 0), 0);
    return { active, disabled, roleCount, online, blocked, review };
  }, [colleagues, instances, runtimeByWorker]);

  const rows = useMemo<EmployeeRow[]>(() => colleagues.map((colleague) => {
    const runtimeItems = runtimeByWorker.get(colleague.id) || [];
    const primaryRuntime = pickPrimaryRuntime(runtimeItems);
    const work = aggregateWorkStatus(runtimeItems);
    return {
      name: colleague.name || colleague.id,
      role: colleague.role_name || colleague.role_code || colleague.role_id || t('通用', 'General'),
      description: colleague.description || '-',
      strengths: (colleague.strengths || colleague.tasks || []).slice(0, 5).join(', ') || '-',
      runtime: primaryRuntime ? runtimeStatusLabel(primaryRuntime.effective_status || primaryRuntime.status) + ' / ' + (work.current_task || t('暂无当前任务', 'No current task')) : t('未连接', 'Not connected'),
      status: statusLabel(colleague.status),
      created: formatTime(colleague.created_at),
    };
  }), [colleagues, runtimeByWorker, t]);

  const runtimeRows = useMemo(() => instances.map(instance => {
    const work = instance.work_status;
    return {
      worker: instance.worker_id || '-',
      instance: instance.instance_id || '-',
      role: instance.role || '-',
      status: runtimeStatusLabel(instance.effective_status || instance.status),
      current: work?.current_task || '-',
      progress: `${t('进行中', 'Active')} ${work?.active_count || 0} / ${t('完成', 'Done')} ${work?.completed_count || 0} / ${t('待人工', 'Review')} ${work?.review_count || 0} / ${t('阻塞', 'Blocked')} ${work?.blocked_count || 0}`,
      heartbeat: instance.heartbeat_age_seconds !== undefined ? instance.heartbeat_age_seconds + 's / ' + formatTime(instance.last_heartbeat_at) : formatTime(instance.last_heartbeat_at),
      capabilities: String(instance.capabilities?.length || 0),
    };
  }), [instances, t]);

  const createColleague = async () => {
    setBusy('create');
    setMessage(null);
    try {
      await requestJSON('/admin/colleagues', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: form.name.trim(),
          role_id: form.role_id,
          description: form.description.trim(),
          strengths: listFromText(form.strengths),
          tasks: listFromText(form.tasks),
        }),
      });
      setForm({ ...emptyForm, role_id: form.role_id });
      setMessage({ kind: 'ok', text: t('iWorker 已创建，可以接收 Center 下发任务。', 'iWorker created and ready for Center dispatch.') });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('创建失败。', 'Create failed.') });
    } finally {
      setBusy('');
    }
  };

  const setStatus = async (colleague: Colleague, status: 'active' | 'disabled') => {
    setBusy(colleague.id + ':' + status);
    setMessage(null);
    try {
      await requestJSON('/admin/colleagues/' + encodeURIComponent(colleague.id) + '/status', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status }),
      });
      setMessage({ kind: 'ok', text: (colleague.name || colleague.id) + t(' 状态已更新。', ' status updated.') });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('状态更新失败。', 'Status update failed.') });
    } finally {
      setBusy('');
    }
  };

  return (
    <div className="center-page-stack">
      <SectionCard
        title={t('数字员工队列', 'Digital Workforce')}
        desc={t(
          '管理接收 iWorkerCenter 下发任务、与人类员工协作并执行能力包的 iWorker。运行状态来自 iWorker 心跳，方便管理员判断数字同事当前是否可用。',
          'Manage iWorkers that receive work from iWorkerCenter, collaborate with human employees, and execute delivered skills. Runtime status comes from iWorker heartbeats so administrators can see whether each digital colleague is available.',
        )}
      >
        <div className="cloud-status-grid">
          <StatusTile label="iWorkers" value={String(colleagues.length)} tone="ok" />
          <StatusTile label={t('启用', 'Active')} value={String(summary.active)} tone="ok" />
          <StatusTile label={t('在线实例', 'Online Instances')} value={String(summary.online)} tone={summary.online ? 'ok' : 'warn'} />
          <StatusTile label={t('使用中的角色', 'Roles in Use')} value={String(summary.roleCount)} />
          <StatusTile label={t('待人工确认', 'Need Review')} value={String(summary.review)} tone={summary.review ? 'warn' : 'ok'} />
          <StatusTile label={t('阻塞任务', 'Blocked Tasks')} value={String(summary.blocked)} tone={summary.blocked ? 'warn' : 'ok'} />
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>{loading ? t('刷新中...', 'Refreshing...') : t('刷新 iWorker', 'Refresh iWorkers')}</button>
          <span className="cloud-inline-note">{t('来源：Center API / iWorker 心跳', 'Source: Center API / iWorker heartbeat')}</span>
        </div>
        {summary.disabled > 0 ? <p className="cloud-message warn">{t(summary.disabled + ' 个 iWorker 已停用或离线，相关下发路径可能需要人工确认。', summary.disabled + ' iWorker(s) are disabled or offline. Related dispatch paths may need human confirmation.')}</p> : null}
        {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
      </SectionCard>

      <SectionCard
        title={t('创建 iWorker', 'Create iWorker')}
        desc={t(
          '为小团队直接创建数字同事，不依赖外部目录系统预置身份。后续可通过本地账号、LDAP 或 OIDC/OAuth 适配器完成认证。',
          'Create a digital colleague directly for small teams without relying on external directory provisioning. Authentication can later use local accounts, LDAP, or OIDC/OAuth adapters.',
        )}
      >
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>{t('名称', 'Name')}</span><input value={form.name} onChange={event => setForm({ ...form, name: event.target.value })} /></label>
          <label className="cloud-field"><span>{t('角色', 'Role')}</span><select value={form.role_id} onChange={event => setForm({ ...form, role_id: event.target.value })}>{roles.length === 0 ? <option value="">{t('没有启用角色', 'No active roles')}</option> : roles.map(role => <option key={role.id} value={role.id}>{role.name} / {role.code}</option>)}</select></label>
          <label className="cloud-field cloud-field-wide"><span>{t('描述', 'Description')}</span><textarea rows={3} value={form.description} onChange={event => setForm({ ...form, description: event.target.value })} /></label>
          <label className="cloud-field"><span>{t('擅长能力', 'Strengths')}</span><textarea rows={3} value={form.strengths} onChange={event => setForm({ ...form, strengths: event.target.value })} placeholder={t('用逗号或换行分隔', 'Comma or newline separated')} /></label>
          <label className="cloud-field"><span>{t('典型任务', 'Typical Tasks')}</span><textarea rows={3} value={form.tasks} onChange={event => setForm({ ...form, tasks: event.target.value })} placeholder={t('用逗号或换行分隔', 'Comma or newline separated')} /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" type="button" disabled={busy === 'create' || !form.name.trim() || !form.role_id} onClick={() => { void createColleague(); }}>{busy === 'create' ? t('创建中...', 'Creating...') : t('创建 iWorker', 'Create iWorker')}</button></div>
      </SectionCard>

      <SectionCard
        title={t('iWorker 列表', 'iWorker List')}
        desc={t(
          '列表同时显示最近心跳中的运行状态，便于查看进行中任务和人工干预需求。',
          'The list includes recent heartbeat runtime status so ongoing work and human-intervention needs are visible.',
        )}
      >
        <DataTable columns={[{ key: 'name', label: t('名称', 'Name') }, { key: 'role', label: t('角色', 'Role') }, { key: 'description', label: t('描述', 'Description') }, { key: 'strengths', label: t('擅长能力', 'Strengths') }, { key: 'runtime', label: t('运行状态', 'Runtime') }, { key: 'status', label: t('账号状态', 'Account') }, { key: 'created', label: t('创建时间', 'Created') }]} rows={rows} />
        <div className="capability-row-actions" style={{ marginTop: 12 }}>
          {colleagues.map(colleague => <button key={colleague.id} className="btn-secondary" type="button" disabled={Boolean(busy) || !colleague.id} onClick={() => { void setStatus(colleague, colleague.status === 'disabled' ? 'active' : 'disabled'); }}>{colleague.status === 'disabled' ? t('启用 ', 'Enable ') : t('停用 ', 'Disable ')}{colleague.name || colleague.id}</button>)}
        </div>
        {colleagues.length === 0 ? <p className="cloud-inline-note">{t('还没有 iWorker。请先应用初始化计划，或在这里手工创建。', 'No iWorkers yet. Apply a bootstrap plan or create one here before dispatching work.')}</p> : null}
      </SectionCard>

      <SectionCard
        title={t('运行实例与工作状态', 'Runtime Instances and Work Status')}
        desc={t(
          'iWorker 自动运行时会通过心跳上报当前任务、已完成任务、待人工确认和阻塞数。人类员工需要介入时，iWorker 端会以提醒式界面展示。',
          'When iWorker runs automatically, it reports current work, completed work, review needs, and blocked counts through heartbeat. When humans need to intervene, iWorker shows a reminder-style UI.',
        )}
      >
        <DataTable columns={[
          { key: 'worker', label: 'iWorker' },
          { key: 'instance', label: t('实例', 'Instance') },
          { key: 'role', label: t('运行角色', 'Runtime Role') },
          { key: 'status', label: t('状态', 'Status') },
          { key: 'current', label: t('当前任务', 'Current Task') },
          { key: 'progress', label: t('工作进度', 'Progress') },
          { key: 'heartbeat', label: t('心跳', 'Heartbeat') },
          { key: 'capabilities', label: t('能力数', 'Capabilities') },
        ]} rows={runtimeRows} />
        {instances.length === 0 ? <p className="cloud-inline-note">{t('暂无 iWorker 心跳。iWorker 绑定 Center 并启动后，这里会显示其实例、任务和能力状态。', 'No iWorker heartbeat yet. After iWorker binds to Center and starts, instances, work, and capability status appear here.')}</p> : null}
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}
