import { useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { SectionCard } from '../components/cards/SectionCard';
import { createEndpoint, createRoutingPolicy, listEndpoints, listRoutingPolicies, updateEndpoint, updateRoutingPolicy, type ModelEndpoint, type RoutingPolicy } from '../api/models';

const emptyEndpoint = () => ({ name: '', protocol: 'openai', base_url: '', api_key: '', model: '', cost_tier: 'medium', priority: 0, features: '[]', status: 'active' });
const emptyPolicy = () => ({ name: '', description: '', work_type: '*', role_code: '*', endpoint_id: '', fallback_mode: 'next_priority', priority: 0, status: 'active' });

export function ModelRoutingPage() {
  const { t } = useTranslation();
  const [endpoints, setEndpoints] = useState<ModelEndpoint[]>([]);
  const [policies, setPolicies] = useState<RoutingPolicy[]>([]);
  const [endpointDraft, setEndpointDraft] = useState(emptyEndpoint());
  const [policyDraft, setPolicyDraft] = useState(emptyPolicy());
  const [showEndpointForm, setShowEndpointForm] = useState(false);
  const [showPolicyForm, setShowPolicyForm] = useState(false);
  const [loading, setLoading] = useState(true);
  const [message, setMessage] = useState('');

  const load = () => {
    setLoading(true);
    Promise.all([listEndpoints().catch(() => []), listRoutingPolicies().catch(() => [])])
      .then(([nextEndpoints, nextPolicies]) => {
        setEndpoints(nextEndpoints);
        setPolicies(nextPolicies);
        if (!policyDraft.endpoint_id && nextEndpoints.length > 0) setPolicyDraft(current => ({ ...current, endpoint_id: nextEndpoints[0].id }));
      })
      .catch(err => setMessage(err instanceof Error ? err.message : String(err)))
      .finally(() => setLoading(false));
  };

  useEffect(load, []);

  const endpointName = useMemo(() => Object.fromEntries(endpoints.map(endpoint => [endpoint.id, endpoint.name])), [endpoints]);

  const handleCreateEndpoint = async () => {
    setMessage('');
    try {
      await createEndpoint(endpointDraft);
      setEndpointDraft(emptyEndpoint());
      setShowEndpointForm(false);
      load();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    }
  };

  const handleCreatePolicy = async () => {
    setMessage('');
    try {
      await createRoutingPolicy(policyDraft);
      setPolicyDraft(emptyPolicy());
      setShowPolicyForm(false);
      load();
    } catch (err) {
      setMessage(err instanceof Error ? err.message : String(err));
    }
  };

  const toggleEndpoint = async (endpoint: ModelEndpoint) => {
    await updateEndpoint(endpoint.id, { ...endpoint, api_key: '', status: endpoint.status === 'active' ? 'disabled' : 'active' });
    load();
  };

  const togglePolicy = async (policy: RoutingPolicy) => {
    await updateRoutingPolicy(policy.id, { ...policy, status: policy.status === 'active' ? 'disabled' : 'active' });
    load();
  };

  const patchEndpoint = (key: keyof ReturnType<typeof emptyEndpoint>, value: string | number) => setEndpointDraft(current => ({ ...current, [key]: value }));
  const patchPolicy = (key: keyof ReturnType<typeof emptyPolicy>, value: string | number) => setPolicyDraft(current => ({ ...current, [key]: value }));

  return (
    <div className="center-page-stack model-routing-page">
      {message ? <div className="hint">{message}</div> : null}
      <SectionCard title={t('models.endpointsTitle')} desc={loading ? t('common.loading') : t('models.endpointsDesc')}>
        <div className="delivery-toolbar"><div className="cloud-pill-list"><span>{t('models.endpoints')}: {endpoints.length}</span></div><div className="actions"><button className="btn-ghost" onClick={load}>{t('common.refresh')}</button><button className="btn-primary" onClick={() => setShowEndpointForm(current => !current)}>{t('models.newEndpoint')}</button></div></div>
        {showEndpointForm ? <div className="delivery-editor card"><div className="delivery-editor-grid">
          <label><span>{t('models.name')}</span><input value={endpointDraft.name} onChange={event => patchEndpoint('name', event.target.value)} /></label>
          <label><span>{t('models.protocol')}</span><select value={endpointDraft.protocol} onChange={event => patchEndpoint('protocol', event.target.value)}><option value="openai">OpenAI</option><option value="anthropic">Anthropic</option><option value="gemini">Gemini</option></select></label>
          <label><span>Base URL</span><input value={endpointDraft.base_url} onChange={event => patchEndpoint('base_url', event.target.value)} placeholder="https://api.openai.com/v1" /></label>
          <label><span>API Key</span><input type="password" value={endpointDraft.api_key} onChange={event => patchEndpoint('api_key', event.target.value)} /></label>
          <label><span>{t('models.model')}</span><input value={endpointDraft.model} onChange={event => patchEndpoint('model', event.target.value)} /></label>
          <label><span>{t('models.costTier')}</span><select value={endpointDraft.cost_tier} onChange={event => patchEndpoint('cost_tier', event.target.value)}><option value="low">low</option><option value="medium">medium</option><option value="high">high</option></select></label>
          <label><span>{t('models.priority')}</span><input type="number" value={endpointDraft.priority} onChange={event => patchEndpoint('priority', Number(event.target.value))} /></label>
          <label><span>{t('models.features')}</span><input value={endpointDraft.features} onChange={event => patchEndpoint('features', event.target.value)} placeholder='["coding","analysis"]' /></label>
        </div><div className="actions"><button className="btn-primary" onClick={handleCreateEndpoint}>{t('common.create')}</button><button className="btn-ghost" onClick={() => setShowEndpointForm(false)}>{t('common.cancel')}</button></div></div> : null}
        <div className="delivery-list">{endpoints.length === 0 ? <div className="hint">{t('common.noData')}</div> : endpoints.map(endpoint => <div key={endpoint.id} className="item-row"><div className="item-head"><div><strong>{endpoint.name}</strong><p>{endpoint.base_url || endpoint.id}</p></div><span className={endpoint.status === 'active' ? 'badge ok' : 'badge warn'}>{endpoint.status}</span></div><div className="cloud-pill-list"><span>{endpoint.protocol}</span><span>{endpoint.model}</span><span>{endpoint.cost_tier}</span><span>{t('models.priority')}: {endpoint.priority}</span></div><div className="actions"><button className="btn-ghost" onClick={() => toggleEndpoint(endpoint)}>{endpoint.status === 'active' ? t('models.disable') : t('models.enable')}</button></div></div>)}</div>
      </SectionCard>

      <SectionCard title={t('models.policiesTitle')} desc={t('models.policiesDesc')}>
        <div className="delivery-toolbar"><div className="cloud-pill-list"><span>{t('models.policies')}: {policies.length}</span></div><div className="actions"><button className="btn-primary" onClick={() => setShowPolicyForm(current => !current)}>{t('models.newPolicy')}</button></div></div>
        {showPolicyForm ? <div className="delivery-editor card"><div className="delivery-editor-grid">
          <label><span>{t('models.name')}</span><input value={policyDraft.name} onChange={event => patchPolicy('name', event.target.value)} /></label>
          <label><span>{t('models.endpoint')}</span><select value={policyDraft.endpoint_id} onChange={event => patchPolicy('endpoint_id', event.target.value)}>{endpoints.map(endpoint => <option key={endpoint.id} value={endpoint.id}>{endpoint.name}</option>)}</select></label>
          <label><span>{t('models.workType')}</span><input value={policyDraft.work_type} onChange={event => patchPolicy('work_type', event.target.value)} /></label>
          <label><span>{t('models.roleCode')}</span><input value={policyDraft.role_code} onChange={event => patchPolicy('role_code', event.target.value)} /></label>
          <label><span>{t('models.fallback')}</span><select value={policyDraft.fallback_mode} onChange={event => patchPolicy('fallback_mode', event.target.value)}><option value="next_priority">next_priority</option><option value="any_tier">any_tier</option></select></label>
          <label><span>{t('models.priority')}</span><input type="number" value={policyDraft.priority} onChange={event => patchPolicy('priority', Number(event.target.value))} /></label>
          <label className="field-span-2"><span>{t('models.description')}</span><textarea value={policyDraft.description} onChange={event => patchPolicy('description', event.target.value)} rows={3} /></label>
        </div><div className="actions"><button className="btn-primary" disabled={!policyDraft.endpoint_id} onClick={handleCreatePolicy}>{t('common.create')}</button><button className="btn-ghost" onClick={() => setShowPolicyForm(false)}>{t('common.cancel')}</button></div></div> : null}
        <div className="delivery-list">{policies.length === 0 ? <div className="hint">{t('common.noData')}</div> : policies.map(policy => <div key={policy.id} className="item-row"><div className="item-head"><div><strong>{policy.name}</strong><p>{policy.description || policy.id}</p></div><span className={policy.status === 'active' ? 'badge ok' : 'badge warn'}>{policy.status}</span></div><div className="cloud-pill-list"><span>{t('models.endpoint')}: {endpointName[policy.endpoint_id] || policy.endpoint_id}</span><span>{t('models.workType')}: {policy.work_type}</span><span>{t('models.roleCode')}: {policy.role_code}</span><span>{policy.fallback_mode}</span><span>{t('models.priority')}: {policy.priority}</span></div><div className="actions"><button className="btn-ghost" onClick={() => togglePolicy(policy)}>{policy.status === 'active' ? t('models.disable') : t('models.enable')}</button></div></div>)}</div>
      </SectionCard>
    </div>
  );
}
