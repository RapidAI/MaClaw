import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { useI18n } from '../i18n';

type CollabTask = {
  id: string;
  title: string;
  description?: string;
  from_colleague_id?: string;
  to_colleague_id?: string;
  to_role_code?: string;
  status: string;
  priority?: number;
  result?: string;
  workflow_step_instance_id?: string;
  created_at?: string;
  updated_at?: string;
};
type Colleague = { id: string; name: string; role_code?: string; role_name?: string; status?: string };
type CollabRow = { id: string; title: string; from: string; to: string; status: string; source: string; priority: string; updated: string };
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string } | null;

const emptyForm = { title: '', description: '', from_colleague_id: '', to_colleague_id: '', to_role_code: '', priority: '5' };

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) throw new Error(data?.error?.message || data?.message || `Request failed: ${resp.status}`);
  return data as T;
}

const shortID = (value?: string) => value ? value.slice(0, 12) : '-';
const workerName = (workers: Colleague[], id?: string) => workers.find(w => w.id === id)?.name || shortID(id);
const formatTime = (value?: string) => {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
};

const nextActions = (status: string): Array<'accept' | 'start' | 'reject'> => {
  if (status === 'pending') return ['accept', 'reject'];
  if (status === 'accepted') return ['start', 'reject'];
  if (status === 'in_progress') return ['reject'];
  return [];
};

export function CommunicationsPage() {
  const { t } = useI18n();
  const [tasks, setTasks] = useState<CollabTask[]>([]);
  const [workers, setWorkers] = useState<Colleague[]>([]);
  const [form, setForm] = useState(emptyForm);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message>(null);

  const statusLabel = (status: string) => ({
    pending: t('待处理', 'Pending'),
    accepted: t('已接受', 'Accepted'),
    in_progress: t('进行中', 'In progress'),
    completed: t('已完成', 'Completed'),
    done: t('已完成', 'Completed'),
    rejected: t('已拒绝', 'Rejected'),
  }[status] || status || t('未知', 'Unknown'));

  const actionLabel = (action: 'accept' | 'start' | 'reject') => ({
    accept: t('接受', 'Accept'),
    start: t('开始', 'Start'),
    reject: t('拒绝', 'Reject'),
  }[action]);

  const sourceLabel = (task: CollabTask) => task.workflow_step_instance_id ? t('流程交接', 'Workflow handoff') : t('手工交接', 'Manual handoff');

  const load = async () => {
    setLoading(true);
    setMessage(null);
    try {
      const [taskResp, workerResp] = await Promise.all([
        requestJSON<{ collaborations?: CollabTask[]; tasks?: CollabTask[] }>('/admin/collaborations'),
        requestJSON<{ colleagues?: Colleague[] }>('/admin/colleagues'),
      ]);
      const nextWorkers = workerResp.colleagues || [];
      setTasks(taskResp.collaborations || taskResp.tasks || []);
      setWorkers(nextWorkers);
      setForm(current => current.from_colleague_id || nextWorkers.length === 0 ? current : { ...current, from_colleague_id: nextWorkers[0].id, to_colleague_id: nextWorkers[0].id });
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('加载协作任务失败。', 'Failed to load collaboration tasks.') });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const summary = useMemo(() => ({
    pending: tasks.filter(task => task.status === 'pending' || task.status === 'accepted').length,
    running: tasks.filter(task => task.status === 'in_progress').length,
    completed: tasks.filter(task => task.status === 'completed' || task.status === 'done').length,
    workflowBacked: tasks.filter(task => task.workflow_step_instance_id).length,
  }), [tasks]);

  const rows = useMemo<CollabRow[]>(() => tasks.map(task => ({
    id: shortID(task.id),
    title: task.title || task.id,
    from: workerName(workers, task.from_colleague_id),
    to: task.to_role_code || workerName(workers, task.to_colleague_id),
    status: statusLabel(task.status),
    source: sourceLabel(task),
    priority: String(task.priority ?? '-'),
    updated: formatTime(task.updated_at || task.created_at),
  })), [tasks, workers, t]);

  const createTask = async () => {
    setBusy('create');
    setMessage(null);
    try {
      await requestJSON('/admin/collaborations', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          title: form.title.trim(),
          description: form.description.trim(),
          from_colleague_id: form.from_colleague_id,
          to_colleague_id: form.to_colleague_id,
          to_role_code: form.to_role_code.trim(),
          priority: Number(form.priority) || 5,
        }),
      });
      setForm({ ...emptyForm, from_colleague_id: form.from_colleague_id, to_colleague_id: form.to_colleague_id });
      setMessage({ kind: 'ok', text: t('协作任务已创建。', 'Collaboration task created.') });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('创建失败。', 'Create failed.') });
    } finally {
      setBusy('');
    }
  };

  const transitionTask = async (task: CollabTask, action: 'accept' | 'start' | 'reject') => {
    setBusy(`${task.id}:${action}`);
    setMessage(null);
    try {
      const transitionURL = `/runtime/collaboration/${encodeURIComponent(task.id)}/${encodeURIComponent(action)}`;
      const resp = await requestJSON<{ task?: CollabTask }>(transitionURL, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          actor_id: task.to_colleague_id || task.to_role_code || 'admin-console',
          result: '',
          note: 'operator action from iWorkerCenter collaboration console',
        }),
      });
      if (resp.task) {
        setTasks(current => current.map(item => item.id === resp.task?.id ? resp.task : item));
      }
      setMessage({ kind: 'ok', text: `${task.title || task.id} ${actionLabel(action)}.` });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('状态推进失败。', 'Failed to update task state.') });
    } finally {
      setBusy('');
    }
  };

  return (
    <div className="center-page-stack">
      <SectionCard
        title={t('协作流转', 'Collaboration Flow')}
        desc={t(
          '跟踪 iWorker 到 iWorker、iWorker 到人类员工的交接。待处理项应在 iWorker 客户端显示为醒目的干预提醒。',
          'Track iWorker-to-iWorker and iWorker-to-human handoffs. Pending items should surface as visible intervention reminders in the iWorker client.',
        )}
      >
        <div className="cloud-status-grid cloud-status-grid-wide">
          <StatusTile label={t('待处理', 'Pending')} value={String(summary.pending)} tone={summary.pending ? 'warn' : 'ok'} />
          <StatusTile label={t('运行中', 'Running')} value={String(summary.running)} />
          <StatusTile label={t('已完成', 'Completed')} value={String(summary.completed)} tone="ok" />
          <StatusTile label={t('流程交接', 'Workflow-backed')} value={String(summary.workflowBacked)} />
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>
            {loading ? t('刷新中...', 'Refreshing...') : t('刷新任务', 'Refresh tasks')}
          </button>
          <span className="cloud-inline-note">{t('来源：Center API', 'Source: Center API')}</span>
        </div>
        {summary.pending > 0 ? (
          <p className="cloud-message warn">
            {t(`${summary.pending} 个任务正在等待处理。请确认目标 iWorker 是否显示了人工干预卡片。`, `${summary.pending} task(s) are waiting for action. Check whether the target iWorker has a visible intervention card.`)}
          </p>
        ) : null}
        {message ? <p className={`cloud-message ${message.kind}`}>{message.text}</p> : null}
      </SectionCard>

      <SectionCard
        title={t('创建交接', 'Create Handoff')}
        desc={t(
          '当人类员工或操作员需要把工作导入数字员工队伍时，可以创建一个手工协作项。',
          'Create a manual collaboration item when a human employee or operator needs to direct work into the digital workforce.',
        )}
      >
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>{t('标题', 'Title')}</span><input value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} /></label>
          <label className="cloud-field"><span>{t('优先级', 'Priority')}</span><input type="number" min="1" max="10" value={form.priority} onChange={e => setForm({ ...form, priority: e.target.value })} /></label>
          <label className="cloud-field"><span>{t('发起 iWorker', 'From iWorker')}</span><select value={form.from_colleague_id} onChange={e => setForm({ ...form, from_colleague_id: e.target.value })}>{workers.length === 0 ? <option value="">{t('无 iWorker', 'No iWorkers')}</option> : workers.map(worker => <option key={worker.id} value={worker.id}>{worker.name}</option>)}</select></label>
          <label className="cloud-field"><span>{t('目标 iWorker', 'To iWorker')}</span><select value={form.to_colleague_id} onChange={e => setForm({ ...form, to_colleague_id: e.target.value })}>{workers.length === 0 ? <option value="">{t('无 iWorker', 'No iWorkers')}</option> : workers.map(worker => <option key={worker.id} value={worker.id}>{worker.name}</option>)}</select></label>
          <label className="cloud-field"><span>{t('角色路由覆盖', 'Role route override')}</span><input value={form.to_role_code} onChange={e => setForm({ ...form, to_role_code: e.target.value })} placeholder={t('可选角色代码', 'Optional role code')} /></label>
          <label className="cloud-field cloud-field-wide"><span>{t('描述', 'Description')}</span><textarea rows={4} value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} /></label>
        </div>
        <div className="cloud-actions">
          <button className="cloud-primary" type="button" disabled={busy === 'create' || !form.title.trim() || !form.from_colleague_id || (!form.to_colleague_id && !form.to_role_code.trim())} onClick={() => { void createTask(); }}>
            {busy === 'create' ? t('创建中...', 'Creating...') : t('创建交接', 'Create handoff')}
          </button>
        </div>
      </SectionCard>

      <SectionCard
        title={t('交接记录', 'Handoff Records')}
        desc={t(
          `共 ${tasks.length} 个协作任务。这里用于观察 Center 与 iWorker 客户端之间的推送、接收、开始和拒绝闭环；完成动作应由 iWorker 工作台回写真正结果。`,
          `Total ${tasks.length} collaboration tasks. Use this page to observe the Center and iWorker client push, accept, start, and reject loop; completion should be written back by the iWorker workbench with a real result.`,
        )}
      >
        <DataTable
          columns={[
            { key: 'id', label: t('任务 ID', 'Task ID') },
            { key: 'title', label: t('标题', 'Title') },
            { key: 'from', label: t('发起方', 'From') },
            { key: 'to', label: t('目标', 'To') },
            { key: 'status', label: t('状态', 'Status') },
            { key: 'source', label: t('来源', 'Source') },
            { key: 'priority', label: t('优先级', 'Priority') },
            { key: 'updated', label: t('更新时间', 'Updated') },
          ]}
          rows={rows}
        />
        {tasks.length > 0 ? (
          <div className="capability-row-actions" style={{ marginTop: 12 }}>
            {tasks.map(task => {
              const actions = nextActions(task.status);
              if (actions.length === 0) return null;
              return (
                <span key={task.id} className="capability-bind-controls">
                  <strong>{task.title || shortID(task.id)}</strong>
                  {actions.map(action => (
                    <button key={action} className="btn-secondary" type="button" disabled={Boolean(busy)} onClick={() => { void transitionTask(task, action); }}>
                      {busy === `${task.id}:${action}` ? t('处理中...', 'Working...') : actionLabel(action)}
                    </button>
                  ))}
                </span>
              );
            })}
          </div>
        ) : null}
        {tasks.length === 0 ? <p className="cloud-inline-note">{t('暂无协作记录。流程或手工交接创建的任务会显示在这里。', 'No collaboration records yet. Tasks created by workflows or manual handoffs will appear here.')}</p> : null}
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={`cloud-status-tile ${tone || ''}`}><span>{label}</span><strong>{value}</strong></div>;
}
