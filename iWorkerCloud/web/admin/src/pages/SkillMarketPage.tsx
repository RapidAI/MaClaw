import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { createSkill, deleteSkill, listSkills, updateSkill, type CloudSkill, type SkillInput } from '../api/skills';

type Notice = { tone: 'ok' | 'danger' | 'info'; text: string };

const emptyDraft = (): SkillInput & { tagsText: string; packageText: string } => ({
  id: '',
  name: '',
  description: '',
  category: 'operations',
  version: '1.0.0',
  tags: [],
  tagsText: '',
  risk_level: 'low',
  status: 'active',
  price: 0,
  author: 'iWorkerCloud',
  author_email: '',
  package_format: 'skill.md',
  package_content_base64: '',
  packageText: '',
});

function toDraft(skill: CloudSkill): SkillInput & { tagsText: string; packageText: string } {
  return {
    id: skill.id,
    name: skill.name,
    description: skill.description,
    category: skill.category || 'operations',
    version: skill.version || '1.0.0',
    tags: skill.tags || [],
    tagsText: (skill.tags || []).join(', '),
    risk_level: skill.risk_level || 'low',
    status: skill.status || 'active',
    price: skill.price || 0,
    author: skill.author || 'iWorkerCloud',
    author_email: skill.author_email || '',
    package_format: skill.package_format || 'skill.md',
    package_content_base64: '',
    packageText: '',
  };
}

function encodePackage(text: string) {
  const trimmed = text.trim();
  if (!trimmed) return '';
  return btoa(unescape(encodeURIComponent(trimmed)));
}

function formatDate(value?: string) {
  if (!value) return '-';
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
}

function statusTone(status?: string) {
  if ((status || 'active') === 'active') return 'ok';
  if ((status || 'active') === 'draft') return 'warn';
  return 'danger';
}

export function SkillMarketPage() {
  const { t } = useTranslation();
  const [skills, setSkills] = useState<CloudSkill[]>([]);
  const [query, setQuery] = useState('');
  const [statusFilter, setStatusFilter] = useState('all');
  const [riskFilter, setRiskFilter] = useState('all');
  const [packageFilter, setPackageFilter] = useState('all');
  const [draft, setDraft] = useState(emptyDraft());
  const [editingId, setEditingId] = useState<string | null>(null);
  const [showForm, setShowForm] = useState(false);
  const [loading, setLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [statusUpdatingId, setStatusUpdatingId] = useState<string | null>(null);
  const [deletingId, setDeletingId] = useState<string | null>(null);
  const [notice, setNotice] = useState<Notice | null>(null);
  const [error, setError] = useState('');

  const statusLabel = (value?: string) => t(`skills.status.${value || 'active'}`, { defaultValue: value || 'active' });
  const riskLabel = (value?: string) => t(`skills.risk.${value || 'low'}`, { defaultValue: value || 'low' });

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      setSkills(await listSkills());
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    load();
  }, []);

  const marketStats = useMemo(() => ({
    active: skills.filter(skill => (skill.status || 'active') === 'active').length,
    draft: skills.filter(skill => (skill.status || 'active') === 'draft').length,
    disabled: skills.filter(skill => (skill.status || 'active') === 'disabled').length,
    highRisk: skills.filter(skill => (skill.risk_level || 'low') === 'high').length,
    packaged: skills.filter(skill => !!skill.package_sha256).length,
  }), [skills]);

  const filtered = useMemo(() => {
    const normalized = query.trim().toLowerCase();
    return skills.filter(skill => {
      const matchesQuery = !normalized || [skill.id, skill.name, skill.description, skill.category, skill.status, skill.risk_level, ...(skill.tags || [])]
        .filter(Boolean)
        .some(value => String(value).toLowerCase().includes(normalized));
      const matchesStatus = statusFilter === 'all' || (skill.status || 'active') === statusFilter;
      const matchesRisk = riskFilter === 'all' || (skill.risk_level || 'low') === riskFilter;
      const matchesPackage = packageFilter === 'all' || (packageFilter === 'packaged' ? !!skill.package_sha256 : !skill.package_sha256);
      return matchesQuery && matchesStatus && matchesRisk && matchesPackage;
    });
  }, [packageFilter, skills, query, riskFilter, statusFilter]);

  const patchDraft = (key: keyof typeof draft, value: string | number) => setDraft(current => ({ ...current, [key]: value }));

  const openCreate = () => {
    setEditingId(null);
    setDraft(emptyDraft());
    setShowForm(true);
    setError('');
    setNotice(null);
  };

  const openEdit = (skill: CloudSkill) => {
    setEditingId(skill.id);
    setDraft(toDraft(skill));
    setShowForm(true);
    setError('');
    setNotice(null);
  };

  const buildInput = (): SkillInput => ({
    id: draft.id.trim(),
    name: draft.name.trim(),
    description: draft.description.trim(),
    category: draft.category.trim() || 'general',
    version: draft.version.trim() || '1.0.0',
    tags: draft.tagsText.split(',').map(tag => tag.trim()).filter(Boolean),
    risk_level: draft.risk_level,
    status: draft.status,
    price: Number(draft.price || 0),
    author: draft.author?.trim(),
    author_email: draft.author_email?.trim(),
    package_format: draft.package_format?.trim() || 'skill.md',
    package_content_base64: encodePackage(draft.packageText),
  });

  const handleSave = async () => {
    setSaving(true);
    setError('');
    setNotice(null);
    try {
      const input = buildInput();
      if (editingId) await updateSkill(editingId, input);
      else await createSkill(input);
      setShowForm(false);
      setNotice({ tone: 'ok', text: t('skills.noticeSaved') });
      await load();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setError(message);
      setNotice({ tone: 'danger', text: message });
    } finally {
      setSaving(false);
    }
  };

  const handleStatus = async (skill: CloudSkill, status: string) => {
    setStatusUpdatingId(skill.id);
    setError('');
    setNotice(null);
    try {
      await updateSkill(skill.id, {
        id: skill.id,
        name: skill.name,
        description: skill.description,
        category: skill.category || 'operations',
        version: skill.version || '1.0.0',
        tags: skill.tags || [],
        risk_level: skill.risk_level || 'low',
        status,
        price: skill.price || 0,
        author: skill.author || 'iWorkerCloud',
        author_email: skill.author_email || '',
        package_format: skill.package_format || 'skill.md',
        package_content_base64: '',
      });
      setNotice({ tone: 'ok', text: t('skills.noticeStatusUpdated') });
      await load();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setError(message);
      setNotice({ tone: 'danger', text: message });
    } finally {
      setStatusUpdatingId(null);
    }
  };

  const handleDelete = async (skill: CloudSkill) => {
    if (!confirm(t('skills.confirmDelete'))) return;
    setDeletingId(skill.id);
    setError('');
    setNotice(null);
    try {
      await deleteSkill(skill.id);
      setNotice({ tone: 'ok', text: t('skills.noticeDeleted') });
      await load();
    } catch (err) {
      const message = err instanceof Error ? err.message : String(err);
      setError(message);
      setNotice({ tone: 'danger', text: message });
    } finally {
      setDeletingId(null);
    }
  };

  return (
    <div className="cloud-overview-stack cloud-page-stack">
      <section className="cloud-brief card">
        <div>
          <div className="mini">{t('skills.marketEyebrow')}</div>
          <h3>{t('skills.title')}</h3>
          <p>{t('skills.desc')}</p>
        </div>
        <div className="cloud-brief-note">
          <strong>{t('skills.positionTitle')}</strong>
          <span>{t('skills.positionDesc')}</span>
        </div>
      </section>

      <div className="metrics cloud-metrics ops-summary-grid">
        <div className="metric"><label>{t('skills.stats.active')}</label><strong>{marketStats.active}</strong><span>{t('skills.stats.activeHint')}</span></div>
        <div className="metric"><label>{t('skills.stats.draft')}</label><strong>{marketStats.draft}</strong><span>{marketStats.disabled} {t('skills.stats.disabledHint')}</span></div>
        <div className="metric"><label>{t('skills.stats.highRisk')}</label><strong>{marketStats.highRisk}</strong><span>{t('skills.stats.highRiskHint')}</span></div>
        <div className="metric"><label>{t('skills.stats.packaged')}</label><strong>{marketStats.packaged}</strong><span>{skills.length - marketStats.packaged} {t('skills.stats.unpackedHint')}</span></div>
      </div>

      <div className="head cloud-toolbar">
        <div className="cloud-filter-row">
          <input value={query} onChange={event => setQuery(event.target.value)} placeholder={t('skills.search')} />
          <select value={statusFilter} onChange={event => setStatusFilter(event.target.value)}>
            <option value="all">{t('skills.filters.allStatus')}</option>
            <option value="active">{statusLabel('active')}</option>
            <option value="draft">{statusLabel('draft')}</option>
            <option value="disabled">{statusLabel('disabled')}</option>
          </select>
          <select value={riskFilter} onChange={event => setRiskFilter(event.target.value)}>
            <option value="all">{t('skills.filters.allRisk')}</option>
            <option value="low">{riskLabel('low')}</option>
            <option value="medium">{riskLabel('medium')}</option>
            <option value="high">{riskLabel('high')}</option>
          </select>
          <select value={packageFilter} onChange={event => setPackageFilter(event.target.value)}>
            <option value="all">{t('skills.filters.allPackage')}</option>
            <option value="packaged">{t('skills.filters.packaged')}</option>
            <option value="unpackaged">{t('skills.filters.unpackaged')}</option>
          </select>
          {loading ? <span className="hint">{t('common.loading')}</span> : null}
        </div>
        <div className="actions">
          <button className="btn-ghost" onClick={load}>{t('common.refresh')}</button>
          <button className="btn-primary" onClick={openCreate}>{t('skills.newSkill')}</button>
        </div>
      </div>

      {notice ? <div className={`notice ${notice.tone}`}>{notice.text}</div> : null}
      {error && !notice ? <div className="hint danger">{error}</div> : null}

      {showForm ? <section className="stage-card skill-editor">
        <div className="skill-editor-grid">
          <div><label>{t('skills.fields.id')}</label><input value={draft.id} disabled={!!editingId} onChange={event => patchDraft('id', event.target.value)} placeholder="goal-recovery-loop" /></div>
          <div><label>{t('skills.fields.name')}</label><input value={draft.name} onChange={event => patchDraft('name', event.target.value)} /></div>
          <div><label>{t('skills.fields.category')}</label><input value={draft.category} onChange={event => patchDraft('category', event.target.value)} /></div>
          <div><label>{t('skills.fields.version')}</label><input value={draft.version} onChange={event => patchDraft('version', event.target.value)} /></div>
          <div><label>{t('skills.fields.risk')}</label><select value={draft.risk_level} onChange={event => patchDraft('risk_level', event.target.value)}><option value="low">{riskLabel('low')}</option><option value="medium">{riskLabel('medium')}</option><option value="high">{riskLabel('high')}</option></select></div>
          <div><label>{t('skills.fields.status')}</label><select value={draft.status} onChange={event => patchDraft('status', event.target.value)}><option value="active">{statusLabel('active')}</option><option value="draft">{statusLabel('draft')}</option><option value="disabled">{statusLabel('disabled')}</option></select></div>
          <div><label>{t('skills.fields.price')}</label><input type="number" value={draft.price} onChange={event => patchDraft('price', Number(event.target.value))} /></div>
          <div><label>{t('skills.fields.author')}</label><input value={draft.author || ''} onChange={event => patchDraft('author', event.target.value)} /></div>
          <div className="field-span-2"><label>{t('skills.fields.tags')}</label><input value={draft.tagsText} onChange={event => patchDraft('tagsText', event.target.value)} placeholder="goalwatch, autonomy, recovery" /></div>
          <div className="field-span-2"><label>{t('skills.fields.description')}</label><textarea value={draft.description} onChange={event => patchDraft('description', event.target.value)} rows={3} /></div>
          <div><label>{t('skills.fields.packageFormat')}</label><input value={draft.package_format || ''} onChange={event => patchDraft('package_format', event.target.value)} /></div>
          <div><label>{t('skills.fields.authorEmail')}</label><input value={draft.author_email || ''} onChange={event => patchDraft('author_email', event.target.value)} /></div>
          <div className="field-span-2"><label>{t('skills.fields.packageText')}</label><textarea value={draft.packageText} onChange={event => patchDraft('packageText', event.target.value)} rows={7} placeholder={t('skills.packagePlaceholder')} /></div>
        </div>
        <div className="actions">
          <button className="btn-primary" disabled={saving || !draft.id.trim() || !draft.name.trim()} onClick={handleSave}>{saving ? t('common.loading') : t('common.save')}</button>
          <button className="btn-ghost" onClick={() => setShowForm(false)}>{t('common.cancel')}</button>
        </div>
      </section> : null}

      {filtered.length === 0 ? <div className="hint">{t('skills.empty')}</div> : <div className="cloud-market-grid skill-market-list">
        {filtered.map(skill => (
          <section key={skill.id} className="cloud-pillar-card card skill-card">
            <div className="item-head">
              <div>
                <span className="mini">{skill.category || t('skills.defaultCategory')} / {skill.version || '1.0.0'}</span>
                <h3>{skill.name}</h3>
              </div>
              <span className={`badge ${statusTone(skill.status)}`}>{statusLabel(skill.status)}</span>
            </div>
            <strong>{skill.id}</strong>
            <p>{skill.description}</p>
            <div className="cloud-pill-list">
              {(skill.tags || []).map(tag => <span key={tag}>{tag}</span>)}
              <span>{t('skills.fields.risk')}: {riskLabel(skill.risk_level)}</span>
              <span>{t('skills.fields.price')}: {skill.price || 0}</span>
              <span>{t('skills.downloads')}: {skill.download_count || skill.downloads || 0}</span>
              {skill.package_sha256 ? <span>sha256: {skill.package_sha256.slice(0, 12)}...</span> : <span>{t('skills.noPackage')}</span>}
            </div>
            {(skill.status || 'active') === 'active' && !skill.package_sha256 ? <div className="notice info">{t('skills.packageWarning')}</div> : null}
            <div className="item-meta">{t('skills.updatedAt')}: {formatDate(skill.updated_at || skill.created_at)}</div>
            <div className="actions">
              <button className="btn-ghost" onClick={() => openEdit(skill)}>{t('common.edit')}</button>
              {skill.status !== 'active' ? <button className="btn-secondary" disabled={statusUpdatingId === skill.id || deletingId === skill.id} onClick={() => handleStatus(skill, 'active')}>{statusUpdatingId === skill.id ? t('common.loading') : t('skills.activate')}</button> : <button className="btn-ghost" disabled={statusUpdatingId === skill.id || deletingId === skill.id} onClick={() => handleStatus(skill, 'disabled')}>{statusUpdatingId === skill.id ? t('common.loading') : t('skills.disable')}</button>}
              <button className="btn-danger" disabled={deletingId === skill.id || statusUpdatingId === skill.id} onClick={() => handleDelete(skill)}>{deletingId === skill.id ? t('common.loading') : t('common.delete')}</button>
            </div>
          </section>
        ))}
      </div>}
    </div>
  );
}
