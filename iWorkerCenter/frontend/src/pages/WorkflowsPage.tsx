import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type WorkflowDef = { id: string; name: string; description?: string; trigger_type?: string; status: string; created_at?: string };
type WorkflowInstance = { id: string; definition_id?: string; title: string; status: string; created_at?: string; updated_at?: string };
type WorkflowDefRow = { name: string; trigger: string; description: string; status: string; created: string };
type WorkflowInstRow = { title: string; status: string; updated: string };
type Message = { kind: 'ok' | 'warn' | 'danger'; text: string } | null;

const emptyForm = { name: '', description: '', trigger_type: 'manual', step_name: '', assignee_role_code: '', timeout_minutes: '60' };

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
    case 'draft': return 'Draft';
    case 'published': return 'Published';
    case 'disabled': return 'Disabled';
    case 'running': return 'Running';
    case 'completed': return 'Completed';
    case 'rejected': return 'Rejected';
    default: return status || 'Unknown';
  }
};

const triggerLabel = (trigger?: string) => {
  switch (trigger) {
    case 'manual': return 'Manual';
    case 'scheduled': return 'Scheduled';
    case 'event': return 'Event';
    default: return trigger || 'Manual';
  }
};

const defRowsFrom = (defs: WorkflowDef[]): WorkflowDefRow[] => defs.map(def => ({
  name: def.name || def.id,
  trigger: triggerLabel(def.trigger_type),
  description: def.description || '-',
  status: statusLabel(def.status),
  created: def.created_at || '-',
}));

const instRowsFrom = (instances: WorkflowInstance[]): WorkflowInstRow[] => instances.map(instance => ({
  title: instance.title || instance.id,
  status: statusLabel(instance.status),
  updated: instance.updated_at || instance.created_at || '-',
}));

export function WorkflowsPage() {
  const [defs, setDefs] = useState<WorkflowDef[]>([]);
  const [instances, setInstances] = useState<WorkflowInstance[]>([]);
  const [form, setForm] = useState(emptyForm);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message>(null);

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
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Failed to load workflows.' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const summary = useMemo(() => {
    const published = defs.filter(row => row.status === 'published').length;
    const draft = defs.filter(row => row.status === 'draft').length;
    const running = instances.filter(row => row.status === 'running').length;
    return { published, draft, running };
  }, [defs, instances]);

  const defRows = useMemo(() => defRowsFrom(defs), [defs]);
  const instRows = useMemo(() => instRowsFrom(instances), [instances]);

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
      setMessage({ kind: 'ok', text: 'Workflow draft created.' });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Create failed.' });
    } finally {
      setBusy('');
    }
  };

  const publishWorkflow = async (def: WorkflowDef) => {
    setBusy(def.id + ':publish');
    setMessage(null);
    try {
      await requestJSON('/admin/workflows/' + def.id + '/publish', { method: 'POST' });
      setMessage({ kind: 'ok', text: def.name + ' published to iWorkers.' });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Publish failed.' });
    } finally {
      setBusy('');
    }
  };

  const startWorkflow = async (def: WorkflowDef) => {
    setBusy(def.id + ':start');
    setMessage(null);
    try {
      await requestJSON('/runtime/workflows/start', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ definition_id: def.id, title: def.name + ' run', initiator_id: 'admin-console' }),
      });
      setMessage({ kind: 'ok', text: def.name + ' started.' });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Start failed.' });
    } finally {
      setBusy('');
    }
  };

  return (
    <div className="center-page-stack">
      <SectionCard title="Workflow operations" desc="Workflows connect digital colleagues, human confirmations, MCP/Skill execution, and business rules into a repeatable run path.">
        <div className="cloud-status-grid">
          <StatusTile label="Published" value={String(summary.published)} tone="ok" />
          <StatusTile label="Drafts" value={String(summary.draft)} tone={summary.draft ? 'warn' : 'ok'} />
          <StatusTile label="Running" value={String(summary.running)} />
        </div>
        <div className="cloud-actions"><button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>{loading ? 'Refreshing...' : 'Refresh workflows'}</button><span className="cloud-inline-note">Source: Center API</span></div>
        {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
      </SectionCard>

      <SectionCard title="Create workflow" desc="Create a minimal single-step workflow first, then publish and start it when the routing path is ready.">
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>Name</span><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} /></label>
          <label className="cloud-field"><span>Trigger</span><select value={form.trigger_type} onChange={e => setForm({ ...form, trigger_type: e.target.value })}><option value="manual">Manual</option><option value="scheduled">Scheduled</option><option value="event">Event</option></select></label>
          <label className="cloud-field"><span>First step</span><input value={form.step_name} onChange={e => setForm({ ...form, step_name: e.target.value })} /></label>
          <label className="cloud-field"><span>Assignee role code</span><input value={form.assignee_role_code} onChange={e => setForm({ ...form, assignee_role_code: e.target.value })} placeholder="office / data / quality" /></label>
          <label className="cloud-field"><span>Timeout minutes</span><input type="number" min="1" value={form.timeout_minutes} onChange={e => setForm({ ...form, timeout_minutes: e.target.value })} /></label>
          <label className="cloud-field cloud-field-wide"><span>Description</span><textarea rows={4} value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" type="button" disabled={busy === 'create' || !form.name.trim()} onClick={() => { void createWorkflow(); }}>{busy === 'create' ? 'Creating...' : 'Create draft'}</button></div>
      </SectionCard>

      <SectionCard title="Workflow templates" desc={'Total ' + defs.length + ' workflow definitions.'}>
        <DataTable columns={[{ key: 'name', label: 'Name' }, { key: 'trigger', label: 'Trigger' }, { key: 'description', label: 'Description' }, { key: 'status', label: 'Status' }, { key: 'created', label: 'Created' }]} rows={defRows} />
        <div className="capability-row-actions" style={{ marginTop: 12 }}>
          {defs.map(def => <span key={def.id} className="capability-bind-controls"><button className="btn-secondary" type="button" disabled={Boolean(busy) || def.status === 'published'} onClick={() => { void publishWorkflow(def); }}>Publish {def.name}</button><button className="btn-secondary" type="button" disabled={Boolean(busy) || def.status !== 'published'} onClick={() => { void startWorkflow(def); }}>Start</button></span>)}
        </div>
        {defs.length === 0 ? <p className="cloud-inline-note">No workflow templates yet. Bootstrap can create initial templates, or you can create a draft above.</p> : null}
      </SectionCard>

      <SectionCard title="Workflow instances" desc={'Total ' + instances.length + ' running or historical instances.'}>
        <DataTable columns={[{ key: 'title', label: 'Title' }, { key: 'status', label: 'Status' }, { key: 'updated', label: 'Updated' }]} rows={instRows} />
        {instances.length === 0 ? <p className="cloud-inline-note">No workflow instances yet. Published workflows can be started from this page.</p> : null}
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}
