import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

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
  created_at?: string;
  updated_at?: string;
};

type Colleague = { id: string; name: string; role_code?: string; role_name?: string; status?: string };
type CollabRow = { id: string; title: string; from: string; to: string; status: string; priority: string; updated: string };
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string } | null;

const emptyForm = { title: '', description: '', from_colleague_id: '', to_colleague_id: '', to_role_code: '', priority: '5' };

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) {
    throw new Error(data?.error?.message || data?.message || 'Request failed: ' + resp.status);
  }
  return data as T;
}

const statusLabel = (status: string) => {
  switch (status) {
    case 'pending': return 'Pending';
    case 'accepted': return 'Accepted';
    case 'in_progress': return 'In progress';
    case 'completed':
    case 'done': return 'Completed';
    case 'rejected': return 'Rejected';
    default: return status || 'Unknown';
  }
};

const shortID = (value?: string) => value ? value.slice(0, 12) : '-';
const workerName = (workers: Colleague[], id?: string) => workers.find(w => w.id === id)?.name || shortID(id);

const toRows = (tasks: CollabTask[], workers: Colleague[]): CollabRow[] => tasks.map(task => ({
  id: shortID(task.id),
  title: task.title || task.id,
  from: workerName(workers, task.from_colleague_id),
  to: task.to_role_code || workerName(workers, task.to_colleague_id),
  status: statusLabel(task.status),
  priority: String(task.priority ?? '-'),
  updated: task.updated_at || task.created_at || '-',
}));

export function CommunicationsPage() {
  const [tasks, setTasks] = useState<CollabTask[]>([]);
  const [workers, setWorkers] = useState<Colleague[]>([]);
  const [form, setForm] = useState(emptyForm);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message>(null);

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
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Failed to load collaboration tasks.' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const summary = useMemo(() => {
    const pending = tasks.filter(task => task.status === 'pending' || task.status === 'accepted').length;
    const running = tasks.filter(task => task.status === 'in_progress').length;
    const completed = tasks.filter(task => task.status === 'completed' || task.status === 'done').length;
    const roleRouted = tasks.filter(task => !task.to_colleague_id && task.to_role_code).length;
    return { pending, running, completed, roleRouted };
  }, [tasks]);

  const rows = useMemo(() => toRows(tasks, workers), [tasks, workers]);

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
      setMessage({ kind: 'ok', text: 'Collaboration task created.' });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Create failed.' });
    } finally {
      setBusy('');
    }
  };

  return (
    <div className="center-page-stack">
      <SectionCard title="Collaboration flow" desc="Track iWorker-to-iWorker and iWorker-to-human handoffs. Pending items should also surface as visible reminders in the iWorker client.">
        <div className="cloud-status-grid cloud-status-grid-wide">
          <StatusTile label="Pending" value={String(summary.pending)} tone={summary.pending ? 'warn' : 'ok'} />
          <StatusTile label="Running" value={String(summary.running)} />
          <StatusTile label="Completed" value={String(summary.completed)} tone="ok" />
          <StatusTile label="Role routed" value={String(summary.roleRouted)} />
        </div>
        <div className="cloud-actions"><button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>{loading ? 'Refreshing...' : 'Refresh tasks'}</button><span className="cloud-inline-note">Source: Center API</span></div>
        {summary.pending > 0 ? <p className="cloud-message warn">{summary.pending} task(s) are waiting for action. Check whether the target iWorker has a visible intervention card.</p> : null}
        {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
      </SectionCard>

      <SectionCard title="Create handoff" desc="Create a manual collaboration item when a human employee or operator needs to direct work into the digital workforce.">
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>Title</span><input value={form.title} onChange={e => setForm({ ...form, title: e.target.value })} /></label>
          <label className="cloud-field"><span>Priority</span><input type="number" min="1" max="10" value={form.priority} onChange={e => setForm({ ...form, priority: e.target.value })} /></label>
          <label className="cloud-field"><span>From iWorker</span><select value={form.from_colleague_id} onChange={e => setForm({ ...form, from_colleague_id: e.target.value })}>{workers.length === 0 ? <option value="">No iWorkers</option> : workers.map(worker => <option key={worker.id} value={worker.id}>{worker.name}</option>)}</select></label>
          <label className="cloud-field"><span>To iWorker</span><select value={form.to_colleague_id} onChange={e => setForm({ ...form, to_colleague_id: e.target.value })}>{workers.length === 0 ? <option value="">No iWorkers</option> : workers.map(worker => <option key={worker.id} value={worker.id}>{worker.name}</option>)}</select></label>
          <label className="cloud-field"><span>Role route override</span><input value={form.to_role_code} onChange={e => setForm({ ...form, to_role_code: e.target.value })} placeholder="Optional role code" /></label>
          <label className="cloud-field cloud-field-wide"><span>Description</span><textarea rows={4} value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" type="button" disabled={busy === 'create' || !form.title.trim() || !form.from_colleague_id || (!form.to_colleague_id && !form.to_role_code.trim())} onClick={() => { void createTask(); }}>{busy === 'create' ? 'Creating...' : 'Create handoff'}</button></div>
      </SectionCard>

      <SectionCard title="Handoff records" desc={'Total ' + tasks.length + ' collaboration tasks.'}>
        <DataTable columns={[{ key: 'id', label: 'Task ID' }, { key: 'title', label: 'Title' }, { key: 'from', label: 'From' }, { key: 'to', label: 'To' }, { key: 'status', label: 'Status' }, { key: 'priority', label: 'Priority' }, { key: 'updated', label: 'Updated' }]} rows={rows} />
        {tasks.length === 0 ? <p className="cloud-inline-note">No collaboration records yet. Tasks created by workflows or manual handoffs will appear here.</p> : null}
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}
