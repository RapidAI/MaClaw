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

export function EmployeesPage() {
  const { t } = useI18n();
  const [colleagues, setColleagues] = useState<Colleague[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [form, setForm] = useState(emptyForm);
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message>(null);

  const statusLabel = (status?: string) => {
    switch (status) {
      case 'active': return t('启用', 'Active');
      case 'disabled': return t('禁用', 'Disabled');
      case 'offline': return t('离线', 'Offline');
      default: return status || t('未知', 'Unknown');
    }
  };

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
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : t('加载 iWorker 失败。', 'Failed to load iWorkers.') });
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

  const rows = useMemo<EmployeeRow[]>(() => colleagues.map((c) => ({
    name: c.name || c.id,
    role: c.role_name || c.role_code || c.role_id || t('通用', 'General'),
    description: c.description || '-',
    strengths: (c.strengths || c.tasks || []).slice(0, 5).join(', ') || '-',
    status: statusLabel(c.status),
    created: c.created_at || '-',
  })), [colleagues, t]);

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
      setMessage({ kind: 'ok', text: t('iWorker 已创建，可接收 Center 下发任务。', 'iWorker created and ready for Center dispatch.') });
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
      await requestJSON('/admin/colleagues/' + colleague.id + '/status', {
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
      <SectionCard title={t('数字员工队伍', 'Digital Workforce')} desc={t('管理接收 iWorkerCenter 下发任务、与人类员工协作并执行能力包的 iWorker。', 'Manage iWorkers that receive work from iWorkerCenter, collaborate with human employees, and execute delivered skills.')}>
        <div className="cloud-status-grid">
          <StatusTile label="iWorkers" value={String(colleagues.length)} tone="ok" />
          <StatusTile label={t('启用', 'Active')} value={String(summary.active)} tone="ok" />
          <StatusTile label={t('使用中的角色', 'Roles in use')} value={String(summary.roleCount)} />
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>{loading ? t('刷新中...', 'Refreshing...') : t('刷新 iWorker', 'Refresh iWorkers')}</button>
          <span className="cloud-inline-note">{t('来源：Center API', 'Source: Center API')}</span>
        </div>
        {summary.disabled > 0 ? <p className="cloud-message warn">{t(summary.disabled + ' 个 iWorker 已禁用或离线，相关下发路径可能需要人工确认。', summary.disabled + ' iWorker(s) are disabled or offline. Related dispatch paths may need human confirmation.')}</p> : null}
        {message ? <p className={'cloud-message ' + message.kind}>{message.text}</p> : null}
      </SectionCard>

      <SectionCard title={t('创建 iWorker', 'Create iWorker')} desc={t('为小团队直接创建数字同事，不依赖外部目录系统预置身份。', 'Create a digital colleague directly for small teams without relying on external directory provisioning.')}>
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>{t('名称', 'Name')}</span><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} /></label>
          <label className="cloud-field"><span>{t('角色', 'Role')}</span><select value={form.role_id} onChange={e => setForm({ ...form, role_id: e.target.value })}>{roles.length === 0 ? <option value="">{t('没有启用角色', 'No active roles')}</option> : roles.map(role => <option key={role.id} value={role.id}>{role.name} / {role.code}</option>)}</select></label>
          <label className="cloud-field cloud-field-wide"><span>{t('描述', 'Description')}</span><textarea rows={3} value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} /></label>
          <label className="cloud-field"><span>{t('擅长能力', 'Strengths')}</span><textarea rows={3} value={form.strengths} onChange={e => setForm({ ...form, strengths: e.target.value })} placeholder={t('用逗号或换行分隔', 'Comma or newline separated')} /></label>
          <label className="cloud-field"><span>{t('典型任务', 'Typical tasks')}</span><textarea rows={3} value={form.tasks} onChange={e => setForm({ ...form, tasks: e.target.value })} placeholder={t('用逗号或换行分隔', 'Comma or newline separated')} /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" type="button" disabled={busy === 'create' || !form.name.trim() || !form.role_id} onClick={() => { void createColleague(); }}>{busy === 'create' ? t('创建中...', 'Creating...') : t('创建 iWorker', 'Create iWorker')}</button></div>
      </SectionCard>

      <SectionCard title={t('iWorker 列表', 'iWorker List')} desc={t('共 ' + colleagues.length + ' 位数字同事。', 'Total ' + colleagues.length + ' digital colleagues.')}>
        <DataTable columns={[{ key: 'name', label: t('名称', 'Name') }, { key: 'role', label: t('角色', 'Role') }, { key: 'description', label: t('描述', 'Description') }, { key: 'strengths', label: t('擅长能力', 'Strengths') }, { key: 'status', label: t('状态', 'Status') }, { key: 'created', label: t('创建时间', 'Created') }]} rows={rows} />
        <div className="capability-row-actions" style={{ marginTop: 12 }}>
          {colleagues.map(colleague => <button key={colleague.id} className="btn-secondary" type="button" disabled={Boolean(busy) || !colleague.id} onClick={() => { void setStatus(colleague, colleague.status === 'disabled' ? 'active' : 'disabled'); }}>{colleague.status === 'disabled' ? t('启用 ', 'Enable ') : t('禁用 ', 'Disable ')}{colleague.name || colleague.id}</button>)}
        </div>
        {colleagues.length === 0 ? <p className="cloud-inline-note">{t('还没有 iWorker。请先应用初始化计划，或在这里手工创建。', 'No iWorkers yet. Apply a bootstrap plan or create one here before dispatching work.')}</p> : null}
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return <div className={'cloud-status-tile ' + (tone || '')}><span>{label}</span><strong>{value}</strong></div>;
}
