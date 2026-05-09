import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { useI18n } from '../i18n';

type WorkflowDef = { id: string; name: string; description?: string; trigger_type?: string; status: string; created_at?: string };
type WorkflowInstance = { id: string; definition_id?: string; title: string; initiator_id?: string; current_step_id?: string; status: string; created_at?: string; updated_at?: string };
type WorkflowDefRow = { name: string; trigger: string; description: string; status: string; created: string };
type WorkflowInstRow = { title: string; definition: string; step: string; initiator: string; status: string; updated: string };
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string } | null;

const emptyForm = { name: '', description: '', trigger_type: 'manual', step_name: '', assignee_role_code: '', timeout_minutes: '60' };

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) throw new Error(data?.error?.message || data?.message || `Request failed: ${resp.status}`);
  return data as T;
}

const formatTime = (value?: string) => {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
};

export function WorkflowsPage() {
  const { t } = useI18n();
  const [defs, setDefs] = useState<WorkflowDef[]>([]);
  const [instances, setInstances] = useState<WorkflowInstance[]>([]);
  const [form, setForm] = useState(emptyForm);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message>(null);

  const statusLabel = (status: string) => ({
    draft: t('草稿', 'Draft'),
    published: t('已发布', 'Published'),
    disabled: t('已停用', 'Disabled'),
    running: t('运行中', 'Running'),
    completed: t('已完成', 'Completed'),
    rejected: t('已拒绝', 'Rejected'),
    pending: t('待处理', 'Pending'),
    in_progress: t('进行中', 'In progress'),
  }[status] || status || t('未知', 'Unknown'));

  const triggerLabel = (trigger?: string) => ({
    manual: t('手动', 'Manual'),
    scheduled: t('定时', 'Scheduled'),
    event: t('事件', 'Event'),
  }[trigger || ''] || trigger || t('手动', 'Manual'));

  const load = async () => {
    setLoading(true);
    setMessage(null);
    try {
      const [defsResp, instancesResp] = await Promise.all([
        requestJSON<{ workflows?: WorkflowDef[] }>('/admin/workflows'),
        requestJSON<{ instances?: WorkflowInstance[]; workflow_instances?: WorkflowInstance[] }>('/admin/workflow-instances'),
      ]);
      setDefs(defsResp.workflows || []);
      setInstances(instancesResp.instances || instancesResp.workflow_instances || []);
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('加载流程失败。', 'Failed to load workflows.') });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const summary = useMemo(() => ({
    published: defs.filter(row => row.status === 'published').length,
    draft: defs.filter(row => row.status === 'draft').length,
    running: instances.filter(row => row.status === 'running' || row.status === 'in_progress').length,
  }), [defs, instances]);

  const defRows = useMemo<WorkflowDefRow[]>(() => defs.map(def => ({
    name: def.name || def.id,
    trigger: triggerLabel(def.trigger_type),
    description: def.description || '-',
    status: statusLabel(def.status),
    created: formatTime(def.created_at),
  })), [defs, t]);

  const instRows = useMemo<WorkflowInstRow[]>(() => instances.map(instance => ({
    title: instance.title || instance.id,
    definition: instance.definition_id || '-',
    step: instance.current_step_id || '-',
    initiator: instance.initiator_id || '-',
    status: statusLabel(instance.status),
    updated: formatTime(instance.updated_at || instance.created_at),
  })), [instances, t]);

  const createWorkflow = async () => {
    setBusy('create');
    setMessage(null);
    try {
      await requestJSON('/admin/workflows', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: form.name.trim(),
          description: form.description.trim(),
          trigger_type: form.trigger_type,
          steps: [{
            step_code: 'step_1',
            step_name: form.step_name.trim() || form.name.trim(),
            step_type: 'processing',
            assignee_mode: form.assignee_role_code.trim() ? 'by_role' : 'manual',
            assignee_role_code: form.assignee_role_code.trim(),
            timeout_minutes: Number(form.timeout_minutes) || 60,
            reject_rule: 'end_process',
          }],
        }),
      });
      setForm(emptyForm);
      setMessage({ kind: 'ok', text: t('流程草稿已创建。', 'Workflow draft created.') });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('创建失败。', 'Create failed.') });
    } finally {
      setBusy('');
    }
  };

  const publishWorkflow = async (def: WorkflowDef) => {
    setBusy(`${def.id}:publish`);
    setMessage(null);
    try {
      await requestJSON(`/admin/workflows/${encodeURIComponent(def.id)}/publish`, { method: 'POST' });
      setMessage({ kind: 'ok', text: t(`${def.name} 已发布，可由 Center 启动并推送给 iWorker。`, `${def.name} published and can be started by Center for iWorkers.`) });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('发布失败。', 'Publish failed.') });
    } finally {
      setBusy('');
    }
  };

  const startWorkflow = async (def: WorkflowDef) => {
    setBusy(`${def.id}:start`);
    setMessage(null);
    try {
      await requestJSON('/runtime/workflows/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ definition_id: def.id, title: `${def.name} run`, initiator_id: 'admin-console' }),
      });
      setMessage({ kind: 'ok', text: t(`${def.name} 已启动。GoalWatch 会持续监控步骤状态，必要时推送到 iWorker。`, `${def.name} started. GoalWatch will monitor step state and push to iWorker when needed.`) });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('启动失败。', 'Start failed.') });
    } finally {
      setBusy('');
    }
  };

  return (
    <div className="center-page-stack">
      <SectionCard
        title={t('流程运行', 'Workflow Operations')}
        desc={t(
          '流程把数字同事、人类确认、MCP/Skill 执行和业务规则连接成可重复的运行路径。启动后的步骤会进入 Center 运行时，并可由 GoalWatch 推送给 iWorker。',
          'Workflows connect digital colleagues, human confirmations, MCP/Skill execution, and business rules into repeatable run paths. Started steps enter Center runtime and can be pushed to iWorker by GoalWatch.',
        )}
      >
        <div className="cloud-status-grid">
          <StatusTile label={t('已发布', 'Published')} value={String(summary.published)} tone="ok" />
          <StatusTile label={t('草稿', 'Drafts')} value={String(summary.draft)} tone={summary.draft ? 'warn' : 'ok'} />
          <StatusTile label={t('运行中', 'Running')} value={String(summary.running)} />
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>
            {loading ? t('刷新中...', 'Refreshing...') : t('刷新流程', 'Refresh workflows')}
          </button>
          <span className="cloud-inline-note">{t('来源：Center API', 'Source: Center API')}</span>
        </div>
        {message ? <p className={`cloud-message ${message.kind}`}>{message.text}</p> : null}
      </SectionCard>

      <SectionCard
        title={t('创建流程', 'Create Workflow')}
        desc={t(
          '先创建一个最小单步骤流程，路由确认后再发布并启动。后续可扩展为多步骤、人工审核节点和 MCP/Skill 节点。',
          'Create a minimal single-step workflow first, then publish and start it when routing is ready. Later this can expand into multi-step, human-review, and MCP/Skill nodes.',
        )}
      >
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>{t('名称', 'Name')}</span><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} /></label>
          <label className="cloud-field"><span>{t('触发方式', 'Trigger')}</span><select value={form.trigger_type} onChange={e => setForm({ ...form, trigger_type: e.target.value })}><option value="manual">{t('手动', 'Manual')}</option><option value="scheduled">{t('定时', 'Scheduled')}</option><option value="event">{t('事件', 'Event')}</option></select></label>
          <label className="cloud-field"><span>{t('第一步', 'First step')}</span><input value={form.step_name} onChange={e => setForm({ ...form, step_name: e.target.value })} /></label>
          <label className="cloud-field"><span>{t('处理角色代码', 'Assignee role code')}</span><input value={form.assignee_role_code} onChange={e => setForm({ ...form, assignee_role_code: e.target.value })} placeholder="office / data / quality" /></label>
          <label className="cloud-field"><span>{t('超时分钟', 'Timeout minutes')}</span><input type="number" min="1" value={form.timeout_minutes} onChange={e => setForm({ ...form, timeout_minutes: e.target.value })} /></label>
          <label className="cloud-field cloud-field-wide"><span>{t('描述', 'Description')}</span><textarea rows={4} value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} /></label>
        </div>
        <div className="cloud-actions">
          <button className="cloud-primary" type="button" disabled={busy === 'create' || !form.name.trim()} onClick={() => { void createWorkflow(); }}>
            {busy === 'create' ? t('创建中...', 'Creating...') : t('创建草稿', 'Create draft')}
          </button>
        </div>
      </SectionCard>

      <SectionCard
        title={t('流程模板', 'Workflow Templates')}
        desc={t(
          `共 ${defs.length} 个流程定义。发布后可以直接从本页启动，启动会创建流程实例和第一步协作任务。`,
          `Total ${defs.length} workflow definitions. After publishing, start one here to create a workflow instance and first-step collaboration task.`,
        )}
      >
        <DataTable
          columns={[
            { key: 'name', label: t('名称', 'Name') },
            { key: 'trigger', label: t('触发', 'Trigger') },
            { key: 'description', label: t('描述', 'Description') },
            { key: 'status', label: t('状态', 'Status') },
            { key: 'created', label: t('创建时间', 'Created') },
          ]}
          rows={defRows}
        />
        {defs.length > 0 ? (
          <div className="capability-row-actions" style={{ marginTop: 12 }}>
            {defs.map(def => (
              <span key={def.id} className="capability-bind-controls">
                <button className="btn-secondary" type="button" disabled={Boolean(busy) || def.status === 'published'} onClick={() => { void publishWorkflow(def); }}>
                  {busy === `${def.id}:publish` ? t('发布中...', 'Publishing...') : t('发布 ', 'Publish ') + def.name}
                </button>
                <button className="btn-secondary" type="button" disabled={Boolean(busy) || def.status !== 'published'} onClick={() => { void startWorkflow(def); }}>
                  {busy === `${def.id}:start` ? t('启动中...', 'Starting...') : t('启动', 'Start')}
                </button>
              </span>
            ))}
          </div>
        ) : null}
        {defs.length === 0 ? <p className="cloud-inline-note">{t('还没有流程模板。初始化可以创建首批模板，也可以在上方创建草稿。', 'No workflow templates yet. Bootstrap can create initial templates, or you can create a draft above.')}</p> : null}
      </SectionCard>

      <SectionCard title={t('流程实例', 'Workflow Instances')} desc={t(`共 ${instances.length} 个运行中或历史实例。`, `Total ${instances.length} running or historical instances.`)}>
        <DataTable columns={[
          { key: 'title', label: t('标题', 'Title') },
          { key: 'definition', label: t('流程定义', 'Definition') },
          { key: 'step', label: t('当前步骤', 'Current step') },
          { key: 'initiator', label: t('发起人', 'Initiator') },
          { key: 'status', label: t('状态', 'Status') },
          { key: 'updated', label: t('更新时间', 'Updated') },
        ]} rows={instRows} />
        {instances.length === 0 ? <p className="cloud-inline-note">{t('还没有流程实例。已发布流程可以从本页启动。', 'No workflow instances yet. Published workflows can be started from this page.')}</p> : null}
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={`cloud-status-tile ${tone || ''}`}><span>{label}</span><strong>{value}</strong></div>;
}
