import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

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

type EmployeeRow = {
  name: string;
  role: string;
  description: string;
  strengths: string;
  status: string;
  created: string;
};

type Message = { kind: 'ok' | 'warn' | 'danger'; text: string } | null;

const emptyForm = { name: '', role_id: '', description: '', strengths: '', tasks: '' };

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) {
    throw new Error(data?.error?.message || data?.message || 'Request failed: ' + resp.status);
  }
  return data as T;
}

const listFromText = (value: string) => value
  .replaceAll(String.fromCharCode(13), '')
  .replaceAll(String.fromCharCode(10), ',')
  .split(',')
  .map(item => item.trim())
  .filter(Boolean);

const statusLabel = (status?: string) => {
  switch (status) {
    case 'active': return 'Active';
    case 'disabled': return 'Disabled';
    case 'offline': return 'Offline';
    default: return status || 'Unknown';
  }
};

const toRows = (cols: Colleague[]): EmployeeRow[] => cols.map((c) => ({
  name: c.name || c.id,
  role: c.role_name || c.role_code || c.role_id || 'General',
  description: c.description || '-',
  strengths: (c.strengths || c.tasks || []).slice(0, 5).join(', ') || '-',
  status: statusLabel(c.status),
  created: c.created_at || '-',
}));

export function EmployeesPage() {
  const [colleagues, setColleagues] = useState<Colleague[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [form, setForm] = useState(emptyForm);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message>(null);

  const load = async () => {
    setLoading(true);
    setMessage(null);
    try {
      const [colResp, roleResp] = await Promise.all([
        requestJSON<{ colleagues?: Colleague[] }>('/admin/colleagues'),
        requestJSON<{ roles?: Role[] }>('/admin/roles'),
      ]);
      const activeRoles = (roleResp.roles || []).filter(role => role.status !== 'disabled');
      setColleagues(colResp.colleagues || []);
      setRoles(activeRoles);
      setForm(current => current.role_id || activeRoles.length === 0 ? current : { ...current, role_id: activeRoles[0].id });
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Failed to load iWorkers.' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const summary = useMemo(() => {
    const active = colleagues.filter(row => row.status === 'active' || !row.status).length;
    const disabled = colleagues.filter(row => row.status === 'disabled' || row.status === 'offline').length;
    const roleCount = new Set(colleagues.map(row => row.role_id || row.role_code).filter(Boolean)).size;
    return { active, disabled, roleCount };
  }, [colleagues]);

  const rows = useMemo(() => toRows(colleagues), [colleagues]);

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
      setMessage({ kind: 'ok', text: 'iWorker created and ready for Center dispatch.' });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Create failed.' });
    } finally {
      setBusy('');
    }
  };

  const setStatus = async (colleague: Colleague, status: 'active' | 'disabled') => {
    setBusy(colleague.id + ':' + status);
    setMessage(null);
    try {
      await requestJSON('/admin/colleagues/' + colleague.id + '/status', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status }),
      });
      setMessage({ kind: 'ok', text: (colleague.name || colleague.id) + ' is now ' + status + '.' });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : 'Status update failed.' });
    } finally {
      setBusy('');
    }
  };

  return (
    <div className="center-page-stack">
      <SectionCard title="Digital workforce" desc="Manage the iWorkers that receive work from iWorkerCenter, collaborate with human employees, and execute Center-delivered skills.">
        <div className="cloud-status-grid">
          <StatusTile label="iWorkers" value={String(colleagues.length)} tone="ok" />
          <StatusTile label="Active" value={String(summary.active)} tone="ok" />
          <StatusTile label="Roles in use" value={String(summary.roleCount)} />
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>{loading ? 'Refreshing...' : 'Refresh iWorkers'}</button>
          <span className="cloud-inline-note">Source: Center API</span>
        </div>
        {summary.disabled > 0 ? <p className="cloud-message warn">{summary.disabled} iWorker(s) are disabled or offline. Related dispatch paths may need human confirmation.</p> : null}
        {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
      </SectionCard>

      <SectionCard title="Create iWorker" desc="Create a digital colleague for small teams without relying on external directory provisioning.">
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>Name</span><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} /></label>
          <label className="cloud-field"><span>Role</span><select value={form.role_id} onChange={e => setForm({ ...form, role_id: e.target.value })}>{roles.length === 0 ? <option value="">No active roles</option> : roles.map(role => <option key={role.id} value={role.id}>{role.name} / {role.code}</option>)}</select></label>
          <label className="cloud-field cloud-field-wide"><span>Description</span><textarea rows={3} value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} /></label>
          <label className="cloud-field"><span>Strengths</span><textarea rows={3} value={form.strengths} onChange={e => setForm({ ...form, strengths: e.target.value })} placeholder="Comma or newline separated" /></label>
          <label className="cloud-field"><span>Typical tasks</span><textarea rows={3} value={form.tasks} onChange={e => setForm({ ...form, tasks: e.target.value })} placeholder="Comma or newline separated" /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" type="button" disabled={busy === 'create' || !form.name.trim() || !form.role_id} onClick={() => { void createColleague(); }}>{busy === 'create' ? 'Creating...' : 'Create iWorker'}</button></div>
      </SectionCard>

      <SectionCard title="iWorker list" desc={'Total ' + colleagues.length + ' digital colleagues.'}>
        <DataTable columns={[{ key: 'name', label: 'Name' }, { key: 'role', label: 'Role' }, { key: 'description', label: 'Description' }, { key: 'strengths', label: 'Strengths' }, { key: 'status', label: 'Status' }, { key: 'created', label: 'Created' }]} rows={rows} />
        <div className="capability-row-actions" style={{ marginTop: 12 }}>
          {colleagues.map(colleague => <button key={colleague.id} className="btn-secondary" type="button" disabled={Boolean(busy) || !colleague.id} onClick={() => { void setStatus(colleague, colleague.status === 'disabled' ? 'active' : 'disabled'); }}>{colleague.status === 'disabled' ? 'Enable ' : 'Disable '}{colleague.name || colleague.id}</button>)}
        </div>
        {colleagues.length === 0 ? <p className="cloud-inline-note">No iWorkers yet. Apply a bootstrap plan or create one here before dispatching work.</p> : null}
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}
