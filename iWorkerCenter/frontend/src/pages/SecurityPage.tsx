import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

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
  if (!resp.ok) throw new Error(data?.error?.message || data?.message || '请求失败: ' + resp.status);
  return data as T;
}

async function fetchJSON<T>(url: string): Promise<T | null> {
  try { return await requestJSON<T>(url); } catch { return null; }
}

const policyTypeLabel = (type: string) => {
  switch (type) {
    case 'keyword_block': return '关键词拦截';
    case 'model_allowlist': return '模型白名单';
    default: return type || '未知';
  }
};

const scopeLabel = (scope: string) => {
  if (!scope || scope === 'all') return '全部';
  if (scope.startsWith('role:')) return '角色：' + scope.slice(5);
  if (scope.startsWith('colleague:')) return 'iWorker：' + scope.slice(10);
  return scope;
};

const statusLabel = (status: string) => {
  switch (status) {
    case 'active': return '启用';
    case 'disabled': return '停用';
    default: return status || '未知';
  }
};

const splitCSV = (value: string) => value.split(/[\n,，]/).map(item => item.trim()).filter(Boolean);

export function SecurityPage() {
  const [policies, setPolicies] = useState<Policy[]>([]);
  const [hits, setHits] = useState<HitRecord[]>([]);
  const [form, setForm] = useState<PolicyForm>(emptyForm());
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<Message | null>(null);

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
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '加载安全策略失败' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const summary = useMemo(() => {
    const active = policies.filter(p => p.status === 'active').length;
    const keyword = policies.filter(p => p.policy_type === 'keyword_block').length;
    const model = policies.filter(p => p.policy_type === 'model_allowlist').length;
    return { active, keyword, model, hits: hits.length };
  }, [policies, hits]);

  const createPolicy = async () => {
    if (!form.name.trim()) {
      setMessage({ kind: 'warn', text: '请填写策略名称。' });
      return;
    }
    const keywords = splitCSV(form.keywords);
    const models = splitCSV(form.models);
    if (form.policy_type === 'keyword_block' && keywords.length === 0) {
      setMessage({ kind: 'warn', text: '请填写至少一个关键词。' });
      return;
    }
    if (form.policy_type === 'model_allowlist' && models.length === 0) {
      setMessage({ kind: 'warn', text: '请填写至少一个允许模型。' });
      return;
    }
    const rules = form.policy_type === 'keyword_block'
      ? { keywords, action: form.action || 'block' }
      : { models, action: 'block' };
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
      setMessage({ kind: 'ok', text: '安全策略已创建。' });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '创建安全策略失败' });
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
    detail: h.detail && h.detail.length > 56 ? h.detail.slice(0, 56) + '...' : h.detail || '-',
    time: h.created_at || '-',
  }));

  return (
    <div className="center-page-stack">
      <SectionCard title="安全治理总览" desc="Center 本地执行安全策略，约束 iWorker 的模型调用、输出内容和风险动作。Cloud 只协调授权和能力来源，不读取企业业务内容。">
        <div className="cloud-status-grid cloud-status-grid-wide">
          <StatusTile label="启用策略" value={String(summary.active)} tone="ok" />
          <StatusTile label="关键词策略" value={String(summary.keyword)} />
          <StatusTile label="模型限制" value={String(summary.model)} />
          <StatusTile label="近期命中" value={String(summary.hits)} tone={summary.hits ? 'warn' : 'ok'} />
        </div>
        <div className="cloud-actions"><button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>{loading ? '刷新中...' : '刷新安全数据'}</button></div>
        {message && <p className={'cloud-message ' + message.kind}>{message.text}</p>}
      </SectionCard>

      <SectionCard title="创建安全策略" desc="先支持最常用的关键词拦截和模型白名单。可按全部、角色或指定 iWorker 设置作用范围。">
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>策略名称</span><input value={form.name} onChange={e => setForm({ ...form, name: e.target.value })} placeholder="例如：禁止泄露密钥" /></label>
          <label className="cloud-field"><span>策略类型</span><select value={form.policy_type} onChange={e => setForm({ ...form, policy_type: e.target.value as PolicyForm['policy_type'] })}><option value="keyword_block">关键词拦截</option><option value="model_allowlist">模型白名单</option></select></label>
          <label className="cloud-field"><span>作用范围</span><input value={form.scope} onChange={e => setForm({ ...form, scope: e.target.value })} placeholder="all / role:data / colleague:xxx" /></label>
          <label className="cloud-field"><span>优先级</span><input type="number" value={form.priority} onChange={e => setForm({ ...form, priority: Number(e.target.value) || 0 })} /></label>
          {form.policy_type === 'keyword_block' ? (
            <label className="cloud-field cloud-field-wide"><span>关键词（逗号或换行分隔）</span><textarea rows={4} value={form.keywords} onChange={e => setForm({ ...form, keywords: e.target.value })} placeholder="密码, API Key, 私钥" /></label>
          ) : (
            <label className="cloud-field cloud-field-wide"><span>允许模型（逗号或换行分隔）</span><textarea rows={4} value={form.models} onChange={e => setForm({ ...form, models: e.target.value })} placeholder="gpt-4.1-mini, qwen-plus" /></label>
          )}
          <label className="cloud-field cloud-field-wide"><span>说明</span><input value={form.description} onChange={e => setForm({ ...form, description: e.target.value })} /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" type="button" onClick={createPolicy} disabled={busy}>{busy ? '创建中...' : '创建策略'}</button></div>
      </SectionCard>

      <SectionCard title="安全策略规则" desc={'共 ' + policies.length + ' 条策略，按优先级执行。'}>
        <DataTable columns={[{ key: 'name', label: '策略名称' }, { key: 'type', label: '类型' }, { key: 'scope', label: '作用范围' }, { key: 'priority', label: '优先级' }, { key: 'status', label: '状态' }]} rows={policyRows} />
        {policies.length === 0 && <p className="cloud-inline-note">暂无安全策略。建议至少创建密钥/隐私关键词拦截策略。</p>}
      </SectionCard>

      <SectionCard title="策略命中记录" desc="最近的安全策略触发记录，用于排查 iWorker 是否被规则阻断或降级。">
        <DataTable columns={[{ key: 'policy', label: '策略' }, { key: 'action', label: '动作' }, { key: 'actor', label: '触发者' }, { key: 'detail', label: '详情' }, { key: 'time', label: '时间' }]} rows={hitRows} />
        {hits.length === 0 && <p className="cloud-inline-note">暂无命中记录。</p>}
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return (
    <div className={'cloud-status-tile ' + (tone || '')}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
