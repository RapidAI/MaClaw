import { useEffect, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { createPolicy, listHits, listPolicies, updatePolicy, type SecurityHit, type SecurityPolicy } from '../api/security';

const emptyDraft = () => ({
  name: '',
  policy_type: 'keyword_block',
  description: '',
  scope: 'all',
  priority: 10,
  keywords: '',
  action: 'block',
  blockedModels: '',
});

function parseRules(policy: SecurityPolicy) {
  try { return JSON.parse(policy.rules || '{}'); } catch { return {}; }
}

export function SecurityPage() {
  const { t } = useTranslation();
  const [policies, setPolicies] = useState<SecurityPolicy[]>([]);
  const [hits, setHits] = useState<SecurityHit[]>([]);
  const [draft, setDraft] = useState(emptyDraft());
  const [showForm, setShowForm] = useState(false);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState('');

  const load = () => {
    setLoading(true);
    Promise.all([listPolicies().catch(() => []), listHits().catch(() => [])])
      .then(([nextPolicies, nextHits]) => { setPolicies(nextPolicies); setHits(nextHits); })
      .catch(err => setMessage(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const patchDraft = (key: keyof ReturnType<typeof emptyDraft>, value: string | number) => setDraft(current => ({ ...current, [key]: value }));

  const buildRules = () => draft.policy_type === 'model_restrict'
    ? { blocked_models: draft.blockedModels.split(',').map(item => item.trim()).filter(Boolean) }
    : { keywords: draft.keywords.split(',').map(item => item.trim()).filter(Boolean), action: draft.action };

  const handleCreate = async () => {
    setMessage('');
    try {
      await createPolicy({ name: draft.name, policy_type: draft.policy_type, description: draft.description, scope: draft.scope, priority: Number(draft.priority || 0), rules: buildRules() } as any);
      setDraft(emptyDraft());
      setShowForm(false);
      setMessage(t('security.created'));
      load();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    }
  };

  const handleToggle = async (policy: SecurityPolicy) => {
    const rules = parseRules(policy);
    await updatePolicy(policy.id, { ...policy, rules, status: policy.status === 'active' ? 'disabled' : 'active' } as any);
    load();
  };

  return (
    <div className="center-page-stack security-page">
      {message ? <div className="hint">{message}</div> : null}
      <SectionCard title={t('security.title')} desc={loading ? t('common.loading') : t('security.desc')}>
        <div className="delivery-toolbar">
          <div className="cloud-pill-list"><span>{t('security.policies')}: {policies.length}</span><span>{t('security.hits')}: {hits.length}</span></div>
          <div className="actions"><button className="btn-ghost" onClick={load}>{t('common.refresh')}</button><button className="btn-primary" onClick={() => setShowForm(current => !current)}>{t('security.newPolicy')}</button></div>
        </div>
        {showForm ? <div className="delivery-editor card">
          <div className="delivery-editor-grid">
            <label><span>{t('security.name')}</span><input value={draft.name} onChange={event => patchDraft('name', event.target.value)} /></label>
            <label><span>{t('security.type')}</span><select value={draft.policy_type} onChange={event => patchDraft('policy_type', event.target.value)}><option value="keyword_block">keyword_block</option><option value="model_restrict">model_restrict</option></select></label>
            <label><span>{t('security.scope')}</span><input value={draft.scope} onChange={event => patchDraft('scope', event.target.value)} placeholder="all / role:sales / colleague:id" /></label>
            <label><span>{t('security.priority')}</span><input type="number" value={draft.priority} onChange={event => patchDraft('priority', Number(event.target.value))} /></label>
            {draft.policy_type === 'model_restrict' ? <label className="field-span-2"><span>{t('security.blockedModels')}</span><input value={draft.blockedModels} onChange={event => patchDraft('blockedModels', event.target.value)} placeholder="gpt-4, gpt-4-turbo" /></label> : <>
              <label><span>{t('security.keywords')}</span><input value={draft.keywords} onChange={event => patchDraft('keywords', event.target.value)} placeholder="password, secret" /></label>
              <label><span>{t('security.action')}</span><select value={draft.action} onChange={event => patchDraft('action', event.target.value)}><option value="block">block</option><option value="warn">warn</option><option value="log">log</option></select></label>
            </>}
            <label className="field-span-2"><span>{t('security.description')}</span><textarea value={draft.description} onChange={event => patchDraft('description', event.target.value)} rows={3} /></label>
          </div>
          <div className="actions"><button className="btn-primary" onClick={handleCreate}>{t('common.create')}</button><button className="btn-ghost" onClick={() => setShowForm(false)}>{t('common.cancel')}</button></div>
        </div> : null}
        <div className="delivery-list">
          {policies.length === 0 ? <div className="hint">{t('common.noData')}</div> : policies.map(policy => {
            const rules = parseRules(policy);
            const detail = policy.policy_type === 'model_restrict' ? (rules.blocked_models || []).join(', ') : (rules.keywords || []).join(', ');
            return <div key={policy.id} className="item-row">
              <div className="item-head"><div><strong>{policy.name}</strong><p>{policy.description || policy.id}</p></div><span className={policy.status === 'active' ? 'badge ok' : 'badge warn'}>{policy.status}</span></div>
              <div className="cloud-pill-list"><span>{policy.policy_type}</span><span>{policy.scope}</span><span>{t('security.priority')}: {policy.priority}</span><span>{detail || '-'}</span></div>
              <div className="actions"><button className="btn-ghost" onClick={() => handleToggle(policy)}>{policy.status === 'active' ? t('security.disable') : t('security.enable')}</button></div>
            </div>;
          })}
        </div>
      </SectionCard>
      <SectionCard title={t('security.recentHits')} desc={t('security.recentHitsDesc')}>
        <div className="delivery-list">{hits.length === 0 ? <div className="hint">{t('common.noData')}</div> : hits.map(hit => <div key={hit.id} className="item-row"><div className="item-head"><strong>{hit.policy_name || hit.policy_id}</strong><span className="badge warn">{hit.action}</span></div><p>{hit.detail}</p><div className="item-meta">{hit.actor_id || '-'} | {hit.created_at}</div></div>)}</div>
      </SectionCard>
    </div>
  );
}
