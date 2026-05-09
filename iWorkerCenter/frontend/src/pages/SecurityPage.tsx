import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { useI18n } from '../i18n';

type Policy = {
  id: string;
  name: string;
  policy_type: string;
  description: string;
  rules: string;
  scope: string;
  priority: number;
  status: string;
};

type HitRecord = {
  id: string;
  policy_name: string;
  actor_id: string;
  action: string;
  detail: string;
  created_at: string;
};

type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };

type PolicyForm = {
  name: string;
  policy_type: 'keyword_block' | 'model_allowlist';
  description: string;
  keywords: string;
  models: string;
  action: string;
  scope: string;
  priority: number;
};

const emptyForm = (): PolicyForm => ({
  name: '',
  policy_type: 'keyword_block',
  description: '',
  keywords: '',
  models: '',
  action: 'block',
  scope: 'all',
  priority: 10,
});

async function requestJSON<T>(url: string, init?: RequestInit): Promise<T> {
  const resp = await fetch(url, init);
  const text = await resp.text();
  const data = text ? JSON.parse(text) : null;
  if (!resp.ok) {
    throw new Error(data?.error?.message || data?.message || `Request failed: ${resp.status}`);
  }
  return data as T;
}

async function fetchJSON<T>(url: string): Promise<T | null> {
  try {
    return await requestJSON<T>(url);
  } catch {
    return null;
  }
}

const splitCSV = (value: string) =>
  value
    .replaceAll(String.fromCharCode(13), '')
    .replaceAll(String.fromCharCode(10), ',')
    .split(',')
    .map(item => item.trim())
    .filter(Boolean);

export function SecurityPage() {
  const { t } = useI18n();
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [hits, setHits] = useState<HitRecord[]>([]);
  const [form, setForm] = useState<PolicyForm>(emptyForm());
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<Message | null>(null);

  const policyTypeLabel = (type: string) =>
    ({
      keyword_block: t('关键词拦截', 'Keyword block'),
      model_allowlist: t('模型白名单', 'Model allowlist'),
    })[type] || type || t('未知', 'Unknown');

  const scopeLabel = (scope: string) => {
    if (!scope || scope === 'all') return t('全部', 'All');
    if (scope.startsWith('role:')) return t('角色：', 'Role: ') + scope.slice(5);
    if (scope.startsWith('colleague:')) return 'iWorker: ' + scope.slice(10);
    return scope;
  };

  const statusLabel = (status: string) =>
    ({
      active: t('启用', 'Active'),
      disabled: t('停用', 'Disabled'),
    })[status] || status || t('未知', 'Unknown');

  const load = async () => {
    setLoading(true);
    setMessage(null);
    try {
      const [policyResp, hitResp] = await Promise.all([
        fetchJSON<{ policies: Policy[] }>('/admin/security/policies'),
        fetchJSON<{ hits: HitRecord[] }>('/admin/security/hits'),
      ]);
      setPolicies(policyResp?.policies || []);
      setHits((hitResp?.hits || []).slice(0, 30));
    } catch (err) {
      setMessage({
        kind: 'danger',
        text: err instanceof Error ? err.message : t('加载安全策略失败。', 'Failed to load security policy.'),
      });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, []);

  const summary = useMemo(
    () => ({
      active: policies.filter(p => p.status === 'active').length,
      keyword: policies.filter(p => p.policy_type === 'keyword_block').length,
      model: policies.filter(p => p.policy_type === 'model_allowlist').length,
      hits: hits.length,
    }),
    [policies, hits],
  );

  const createPolicy = async () => {
    if (!form.name.trim()) {
      setMessage({ kind: 'warn', text: t('请填写策略名称。', 'Enter a policy name.') });
      return;
    }
    const keywords = splitCSV(form.keywords);
    const models = splitCSV(form.models);
    if (form.policy_type === 'keyword_block' && keywords.length === 0) {
      setMessage({ kind: 'warn', text: t('请填写至少一个关键词。', 'Enter at least one keyword.') });
      return;
    }
    if (form.policy_type === 'model_allowlist' && models.length === 0) {
      setMessage({ kind: 'warn', text: t('请填写至少一个允许模型。', 'Enter at least one allowed model.') });
      return;
    }
    const rules = form.policy_type === 'keyword_block' ? { keywords, action: form.action || 'block' } : { models, action: 'block' };
    setBusy(true);
    setMessage(null);
    try {
      await requestJSON<Policy>('/admin/security/policies', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({
          name: form.name,
          policy_type: form.policy_type,
          description: form.description,
          rules,
          scope: form.scope || 'all',
          priority: Number(form.priority) || 0,
        }),
      });
      setForm(emptyForm());
      setMessage({ kind: 'ok', text: t('安全策略已创建。', 'Security policy created.') });
      await load();
    } catch (err) {
      setMessage({
        kind: 'danger',
        text: err instanceof Error ? err.message : t('创建安全策略失败。', 'Failed to create security policy.'),
      });
    } finally {
      setBusy(false);
    }
  };

  const policyRows = policies.map(p => ({
    name: p.name,
    type: policyTypeLabel(p.policy_type),
    scope: scopeLabel(p.scope),
    priority: String(p.priority),
    status: statusLabel(p.status),
  }));

  const hitRows = hits.map(h => ({
    policy: h.policy_name,
    action: h.action,
    actor: h.actor_id || '-',
    detail: h.detail && h.detail.length > 56 ? `${h.detail.slice(0, 56)}...` : h.detail || '-',
    time: h.created_at || '-',
  }));

  return (
    <div className="center-page-stack">
      <SectionCard
        title={t('安全治理总览', 'Security Governance')}
        desc={t(
          'Center 本地执行安全策略，约束 iWorker 的模型调用、输出内容和风险动作。Cloud 只协调授权和能力来源，不读取企业业务内容。',
          'Center enforces security policy locally, constraining iWorker model calls, output content, and risky actions. Cloud only coordinates authorization and capability sources; it does not read enterprise business content.',
        )}
      >
        <div className="cloud-status-grid cloud-status-grid-wide">
          <StatusTile label={t('启用策略', 'Active policies')} value={String(summary.active)} tone="ok" />
          <StatusTile label={t('关键词策略', 'Keyword policies')} value={String(summary.keyword)} />
          <StatusTile label={t('模型限制', 'Model restrictions')} value={String(summary.model)} />
          <StatusTile label={t('近期命中', 'Recent hits')} value={String(summary.hits)} tone={summary.hits ? 'warn' : 'ok'} />
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>
            {loading ? t('刷新中...', 'Refreshing...') : t('刷新安全数据', 'Refresh security data')}
          </button>
        </div>
        {message && <p className={`cloud-message ${message.kind}`}>{message.text}</p>}
      </SectionCard>

      <SectionCard
        title={t('创建安全策略', 'Create Security Policy')}
        desc={t(
          '先支持最常用的关键词拦截和模型白名单。可按全部、角色或指定 iWorker 设置作用范围。',
          'Start with common keyword blocking and model allowlists. Scope policies globally, by role, or by a specific iWorker.',
        )}
      >
        <div className="cloud-form-grid">
          <label className="cloud-field">
            <span>{t('策略名称', 'Policy name')}</span>
            <input
              value={form.name}
              onChange={e => setForm({ ...form, name: e.target.value })}
              placeholder={t('例如：禁止泄露密钥', 'Example: block secret leakage')}
            />
          </label>
          <label className="cloud-field">
            <span>{t('策略类型', 'Policy type')}</span>
            <select value={form.policy_type} onChange={e => setForm({ ...form, policy_type: e.target.value as PolicyForm['policy_type'] })}>
              <option value="keyword_block">{t('关键词拦截', 'Keyword block')}</option>
              <option value="model_allowlist">{t('模型白名单', 'Model allowlist')}</option>
            </select>
          </label>
          <label className="cloud-field">
            <span>{t('作用范围', 'Scope')}</span>
            <input value={form.scope} onChange={e => setForm({ ...form, scope: e.target.value })} placeholder="all / role:data / colleague:xxx" />
          </label>
          <label className="cloud-field">
            <span>{t('优先级', 'Priority')}</span>
            <input type="number" value={form.priority} onChange={e => setForm({ ...form, priority: Number(e.target.value) || 0 })} />
          </label>
          {form.policy_type === 'keyword_block' ? (
            <label className="cloud-field cloud-field-wide">
              <span>{t('关键词（逗号或换行分隔）', 'Keywords (comma or newline separated)')}</span>
              <textarea rows={4} value={form.keywords} onChange={e => setForm({ ...form, keywords: e.target.value })} placeholder="password, API Key, private key" />
            </label>
          ) : (
            <label className="cloud-field cloud-field-wide">
              <span>{t('允许模型（逗号或换行分隔）', 'Allowed models (comma or newline separated)')}</span>
              <textarea rows={4} value={form.models} onChange={e => setForm({ ...form, models: e.target.value })} placeholder="gpt-4.1-mini, qwen-plus" />
            </label>
          )}
          <label className="cloud-field cloud-field-wide">
            <span>{t('说明', 'Description')}</span>
            <input value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} />
          </label>
        </div>
        <div className="cloud-actions">
          <button className="cloud-primary" type="button" onClick={createPolicy} disabled={busy}>
            {busy ? t('创建中...', 'Creating...') : t('创建策略', 'Create policy')}
          </button>
        </div>
      </SectionCard>

      <SectionCard
        title={t('安全策略规则', 'Security Policy Rules')}
        desc={t('共 ' + policies.length + ' 条策略，按优先级执行。', 'Total ' + policies.length + ' policies, evaluated by priority.')}
      >
        <DataTable
          columns={[
            { key: 'name', label: t('策略名称', 'Policy') },
            { key: 'type', label: t('类型', 'Type') },
            { key: 'scope', label: t('作用范围', 'Scope') },
            { key: 'priority', label: t('优先级', 'Priority') },
            { key: 'status', label: t('状态', 'Status') },
          ]}
          rows={policyRows}
        />
        {policies.length === 0 && (
          <p className="cloud-inline-note">{t('暂无安全策略。建议至少创建密钥/隐私关键词拦截策略。', 'No security policies yet. At least one secret/privacy keyword block policy is recommended.')}</p>
        )}
      </SectionCard>

      <SectionCard
        title={t('策略命中记录', 'Policy Hit Records')}
        desc={t(
          '最近的安全策略触发记录，用于排查 iWorker 是否被规则阻断或降级。',
          'Recent policy hits help diagnose whether an iWorker was blocked or downgraded by rules.',
        )}
      >
        <DataTable
          columns={[
            { key: 'policy', label: t('策略', 'Policy') },
            { key: 'action', label: t('动作', 'Action') },
            { key: 'actor', label: t('触发者', 'Actor') },
            { key: 'detail', label: t('详情', 'Detail') },
            { key: 'time', label: t('时间', 'Time') },
          ]}
          rows={hitRows}
        />
        {hits.length === 0 && <p className="cloud-inline-note">{t('暂无命中记录。', 'No hit records.')}</p>}
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return (
    <div className={`cloud-status-tile ${tone || ''}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
