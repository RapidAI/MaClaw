import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { assignRole, createColleague, listColleagues, type Colleague } from '../api/colleagues';
import { createRole, listRoles, type Role } from '../api/roles';

const emptyRole = () => ({ code: '', name: '', description: '' });
const emptyColleague = () => ({ name: '', code: '', role_id: '' });

export function EmployeesPage() {
  const { t } = useTranslation();
  const [colleagues, setColleagues] = useState<Colleague[]>([]);
  const [roles, setRoles] = useState<Role[]>([]);
  const [roleDraft, setRoleDraft] = useState(emptyRole());
  const [colleagueDraft, setColleagueDraft] = useState(emptyColleague());
  const [selectedRoleByColleague, setSelectedRoleByColleague] = useState<Record<string, string>>({});
  const [showRoleForm, setShowRoleForm] = useState(false);
  const [showColleagueForm, setShowColleagueForm] = useState(false);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState('');
  const [saving, setSaving] = useState(false);

  const load = () => {
    setLoading(true);
    Promise.all([listColleagues().catch(() => []), listRoles().catch(() => [])])
      .then(([nextColleagues, nextRoles]) => {
        setColleagues(nextColleagues);
        setRoles(nextRoles);
        setSelectedRoleByColleague(Object.fromEntries(nextColleagues.map(colleague => [colleague.id, colleague.role_id || ''])));
        if (!colleagueDraft.role_id && nextRoles.length > 0) setColleagueDraft(current => ({ ...current, role_id: nextRoles[0].id }));
      })
      .catch(err => setMessage(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const roleName = useMemo(() => Object.fromEntries(roles.map(role => [role.id, role.name])), [roles]);

  const handleCreateRole = async () => {
    setSaving(true);
    setMessage('');
    try {
      await createRole(roleDraft);
      setRoleDraft(emptyRole());
      setShowRoleForm(false);
      setMessage(t('employees.roleCreated'));
      load();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const handleCreateColleague = async () => {
    setSaving(true);
    setMessage('');
    try {
      await createColleague(colleagueDraft);
      setColleagueDraft(emptyColleague());
      setShowColleagueForm(false);
      setMessage(t('employees.workerCreated'));
      load();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    } finally {
      setSaving(false);
    }
  };

  const handleAssignRole = async (colleague: Colleague) => {
    const nextRoleID = selectedRoleByColleague[colleague.id];
    if (!nextRoleID || nextRoleID === colleague.role_id) return;
    setMessage('');
    try {
      await assignRole(colleague.id, nextRoleID, 'admin_console_update');
      setMessage(t('employees.roleAssigned'));
      load();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="center-page-stack employees-page">
      {message ? <div className="hint">{message}</div> : null}
      <div className="metric-grid employee-metrics">
        <div className="metric-card card"><label>{t('employees.totalWorkers')}</label><strong>{colleagues.length}</strong><span>{t('employees.totalWorkersHint')}</span></div>
        <div className="metric-card card"><label>{t('employees.roles')}</label><strong>{roles.length}</strong><span>{t('employees.rolesHint')}</span></div>
        <div className="metric-card card"><label>{t('employees.active')}</label><strong>{colleagues.filter(c => c.status === 'active').length}</strong><span>{t('employees.activeHint')}</span></div>
      </div>

      <SectionCard title={t('employees.rolesTitle')} desc={loading ? t('common.loading') : t('employees.rolesDesc')}>
        <div className="delivery-toolbar"><div className="cloud-pill-list"><span>{t('employees.roles')}: {roles.length}</span></div><div className="actions"><button className="btn-ghost" onClick={load}>{t('common.refresh')}</button><button className="btn-primary" onClick={() => setShowRoleForm(current => !current)}>{t('employees.newRole')}</button></div></div>
        {showRoleForm ? <div className="delivery-editor card"><div className="delivery-editor-grid">
          <label><span>{t('employees.roleCode')}</span><input value={roleDraft.code} onChange={event => setRoleDraft(current => ({ ...current, code: event.target.value }))} placeholder="finance" /></label>
          <label><span>{t('employees.roleName')}</span><input value={roleDraft.name} onChange={event => setRoleDraft(current => ({ ...current, name: event.target.value }))} /></label>
          <label className="field-span-2"><span>{t('employees.description')}</span><textarea rows={3} value={roleDraft.description} onChange={event => setRoleDraft(current => ({ ...current, description: event.target.value }))} /></label>
        </div><div className="actions"><button className="btn-primary" disabled={saving || !roleDraft.code || !roleDraft.name} onClick={handleCreateRole}>{saving ? t('common.loading') : t('common.create')}</button><button className="btn-ghost" onClick={() => setShowRoleForm(false)}>{t('common.cancel')}</button></div></div> : null}
        <div className="delivery-list">{roles.length === 0 ? <div className="hint">{t('employees.noRoles')}</div> : roles.map(role => <div key={role.id} className="item-row"><div className="item-head"><div><strong>{role.name}</strong><p>{role.description || role.code}</p></div><span className="badge info">{role.code}</span></div></div>)}</div>
      </SectionCard>

      <SectionCard title={t('employees.workersTitle')} desc={t('employees.workersDesc')}>
        <div className="delivery-toolbar"><div className="cloud-pill-list"><span>{t('employees.totalWorkers')}: {colleagues.length}</span></div><div className="actions"><button className="btn-primary" onClick={() => setShowColleagueForm(current => !current)}>{t('employees.newWorker')}</button></div></div>
        {showColleagueForm ? <div className="delivery-editor card"><div className="delivery-editor-grid">
          <label><span>{t('employees.workerName')}</span><input value={colleagueDraft.name} onChange={event => setColleagueDraft(current => ({ ...current, name: event.target.value }))} /></label>
          <label><span>{t('employees.workerCode')}</span><input value={colleagueDraft.code} onChange={event => setColleagueDraft(current => ({ ...current, code: event.target.value }))} placeholder="fin-analyst-01" /></label>
          <label className="field-span-2"><span>{t('employees.role')}</span><select value={colleagueDraft.role_id} onChange={event => setColleagueDraft(current => ({ ...current, role_id: event.target.value }))}>{roles.map(role => <option key={role.id} value={role.id}>{role.name}</option>)}</select></label>
        </div><div className="actions"><button className="btn-primary" disabled={saving || !colleagueDraft.name || !colleagueDraft.code} onClick={handleCreateColleague}>{saving ? t('common.loading') : t('common.create')}</button><button className="btn-ghost" onClick={() => setShowColleagueForm(false)}>{t('common.cancel')}</button></div></div> : null}
        <div className="delivery-list">{colleagues.length === 0 ? <div className="hint">{t('employees.noWorkers')}</div> : colleagues.map(colleague => <div key={colleague.id} className="item-row employee-card"><div className="item-head"><div><strong>{colleague.name}</strong><p>{colleague.code || colleague.id}</p></div><span className={colleague.status === 'active' ? 'badge ok' : 'badge warn'}>{colleague.status}</span></div><div className="employee-role-row"><select value={selectedRoleByColleague[colleague.id] || ''} onChange={event => setSelectedRoleByColleague(current => ({ ...current, [colleague.id]: event.target.value }))}>{roles.map(role => <option key={role.id} value={role.id}>{role.name}</option>)}</select><button className="btn-ghost" disabled={(selectedRoleByColleague[colleague.id] || '') === colleague.role_id} onClick={() => handleAssignRole(colleague)}>{t('employees.assignRole')}</button><span className="item-meta">{t('employees.currentRole')}: {colleague.role_name || roleName[colleague.role_id] || '-'}</span></div></div>)}</div>
      </SectionCard>
    </div>
  );
}
