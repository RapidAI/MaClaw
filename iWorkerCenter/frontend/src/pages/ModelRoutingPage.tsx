import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type Provider = {
  id: string;
  name: string;
  protocol: string;
  base_url: string;
  model: string;
  priority: number;
  features: string[];
  description: string;
  enabled: boolean;
  cost_tier: string;
};

type CenterSettings = {
  providers: Provider[];
  work_type_keywords?: Record<string, string[]>;
  work_type_tier?: Record<string, string>;
  role_provider_boost?: Record<string, string[]>;
};

type DBEndpoint = {
  id: string;
  name: string;
  protocol: string;
  base_url?: string;
  api_key?: string;
  model: string;
  cost_tier: string;
  priority: number;
  features?: string;
  status: string;
};

type DBPolicy = {
  id: string;
  name: string;
  description?: string;
  work_type: string;
  role_code: string;
  endpoint_id: string;
  fallback_mode: string;
  priority: number;
  status: string;
};

type Message = { kind: 'ok' | 'warn' | 'danger'; text: string };

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const emptyEndpoint = () => ({ name: '', protocol: 'openai', base_url: '', api_key: '', model: '', cost_tier: 'medium', priority: 10, features: '[]' });
const emptyPolicy = () => ({ name: '', description: '', work_type: '*', role_code: '*', endpoint_id: '', fallback_mode: 'next_priority', priority: 10 });

const tierLabel = (tier: string) => {
  switch (tier) {
    case 'low': return '低成本';
    case 'medium': return '均衡';
    case 'high': return '高能力';
    default: return tier || '未设置';
  }
};

const statusLabel = (status: string) => {
  switch (status) {
    case 'active': return '启用';
    case 'disabled': return '停用';
    default: return status || '未知';
  }
};

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

export function ModelRoutingPage() {
  const [settingsRows, setSettingsRows] = useState<Array<Record<string, string>>>([]);
  const [settingsRoutingRows, setSettingsRoutingRows] = useState<Array<Record<string, string>>>([]);
  const [dbEndpoints, setDbEndpoints] = useState<DBEndpoint[]>([]);
  const [dbPolicies, setDbPolicies] = useState<DBPolicy[]>([]);
  const [endpointForm, setEndpointForm] = useState(emptyEndpoint());
  const [policyForm, setPolicyForm] = useState(emptyPolicy());
  const [loading, setLoading] = useState(false);
  const [busy, setBusy] = useState('');
  const [message, setMessage] = useState<Message | null>(null);

  const load = async () => {
    setLoading(true);
    setMessage(null);
    try {
      const [endpointResp, policyResp] = await Promise.all([
        fetchJSON<{ endpoints: DBEndpoint[] }>('/admin/model-endpoints'),
        fetchJSON<{ policies: DBPolicy[] }>('/admin/model-routing-policies'),
      ]);
      setDbEndpoints(endpointResp?.endpoints || []);
      setDbPolicies(policyResp?.policies || []);
      if (!policyForm.endpoint_id && endpointResp?.endpoints?.[0]) {
        setPolicyForm(current => ({ ...current, endpoint_id: endpointResp.endpoints[0].id }));
      }
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '加载模型配置失败' });
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
    if (!hasWails()) return;
    (window as any).go.main.App.LoadCenterSettings()
      .then((settings: CenterSettings) => {
        const providers = settings?.providers || [];
        setSettingsRows(providers.map((p) => ({
          name: p.name || p.id,
          model: p.model,
          protocol: p.protocol || 'openai',
          tier: tierLabel(p.cost_tier),
          priority: String(p.priority),
          status: p.enabled ? '启用' : '停用',
        })));
        if (settings?.work_type_tier && Object.keys(settings.work_type_tier).length > 0) {
          setSettingsRoutingRows(Object.entries(settings.work_type_tier).map(([workType, tier]) => {
            const tierProviders = providers.filter((p) => p.enabled && p.cost_tier === tier);
            return { scene: workType, primary: tierProviders[0]?.model || '-', backup: tierProviders[1]?.model || '-', route: tierLabel(tier) + ' 路由' };
          }));
        }
      })
      .catch(() => {});
  }, []);

  const summary = useMemo(() => {
    const activeEndpoints = dbEndpoints.filter(e => e.status === 'active').length;
    const activePolicies = dbPolicies.filter(p => p.status === 'active').length;
    const highTier = dbEndpoints.filter(e => e.cost_tier === 'high').length;
    return { activeEndpoints, activePolicies, highTier };
  }, [dbEndpoints, dbPolicies]);

  const createEndpoint = async () => {
    if (!endpointForm.name.trim() || !endpointForm.model.trim()) {
      setMessage({ kind: 'warn', text: '请填写端点名称和模型名称。' });
      return;
    }
    setBusy('endpoint');
    setMessage(null);
    try {
      await requestJSON<DBEndpoint>('/admin/model-endpoints', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...endpointForm, priority: Number(endpointForm.priority) || 0 }),
      });
      setEndpointForm(emptyEndpoint());
      setMessage({ kind: 'ok', text: '模型端点已创建。' });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '创建模型端点失败' });
    } finally {
      setBusy('');
    }
  };

  const createPolicy = async () => {
    if (!policyForm.name.trim() || !policyForm.endpoint_id.trim()) {
      setMessage({ kind: 'warn', text: '请填写策略名称并选择模型端点。' });
      return;
    }
    setBusy('policy');
    setMessage(null);
    try {
      await requestJSON<DBPolicy>('/admin/model-routing-policies', {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...policyForm, priority: Number(policyForm.priority) || 0 }),
      });
      setPolicyForm({ ...emptyPolicy(), endpoint_id: dbEndpoints[0]?.id || '' });
      setMessage({ kind: 'ok', text: '模型路由策略已创建。' });
      await load();
    } catch (err) {
      setMessage({ kind: 'danger', text: err instanceof Error ? err.message : '创建路由策略失败' });
    } finally {
      setBusy('');
    }
  };

  const endpointRows = dbEndpoints.map(e => ({
    name: e.name,
    model: e.model,
    protocol: e.protocol,
    tier: tierLabel(e.cost_tier),
    priority: String(e.priority),
    status: statusLabel(e.status),
  }));

  const policyRows = dbPolicies.map(p => ({
    name: p.name,
    work_type: p.work_type === '*' ? '全部' : p.work_type,
    role: p.role_code === '*' ? '全部' : p.role_code,
    endpoint: dbEndpoints.find(e => e.id === p.endpoint_id)?.name || p.endpoint_id || '-',
    fallback: p.fallback_mode === 'next_priority' ? '按优先级回退' : p.fallback_mode,
    priority: String(p.priority),
    status: statusLabel(p.status),
  }));

  return (
    <div className="center-page-stack">
      <SectionCard title="模型调度总览" desc="Center 负责本地模型端点和路由策略，iWorker 执行任务时按工作类型、角色和优先级选择模型。Cloud 离线不影响已保存的本地路由。">
        <div className="cloud-status-grid">
          <StatusTile label="启用端点" value={String(summary.activeEndpoints)} tone="ok" />
          <StatusTile label="启用策略" value={String(summary.activePolicies)} tone="ok" />
          <StatusTile label="高能力端点" value={String(summary.highTier)} tone={summary.highTier ? 'warn' : 'ok'} />
        </div>
        <div className="cloud-actions"><button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>{loading ? '刷新中...' : '刷新模型配置'}</button></div>
        {message && <p className={'cloud-message ' + message.kind}>{message.text}</p>}
      </SectionCard>

      <SectionCard title="新增模型端点" desc="端点保存到 Center 本地数据库。API Key 只在创建/更新时提交，列表中会被后端脱敏。">
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>端点名称</span><input value={endpointForm.name} onChange={e => setEndpointForm({ ...endpointForm, name: e.target.value })} placeholder="例如：公司 OpenAI 网关" /></label>
          <label className="cloud-field"><span>协议</span><select value={endpointForm.protocol} onChange={e => setEndpointForm({ ...endpointForm, protocol: e.target.value })}><option value="openai">OpenAI Compatible</option><option value="anthropic">Anthropic</option></select></label>
          <label className="cloud-field"><span>Base URL</span><input value={endpointForm.base_url} onChange={e => setEndpointForm({ ...endpointForm, base_url: e.target.value })} placeholder="https://api.example.com/v1" /></label>
          <label className="cloud-field"><span>API Key</span><input type="password" value={endpointForm.api_key} onChange={e => setEndpointForm({ ...endpointForm, api_key: e.target.value })} placeholder="可留空用于本地代理" /></label>
          <label className="cloud-field"><span>模型</span><input value={endpointForm.model} onChange={e => setEndpointForm({ ...endpointForm, model: e.target.value })} placeholder="gpt-4.1-mini / qwen-plus / ..." /></label>
          <label className="cloud-field"><span>成本层级</span><select value={endpointForm.cost_tier} onChange={e => setEndpointForm({ ...endpointForm, cost_tier: e.target.value })}><option value="low">低成本</option><option value="medium">均衡</option><option value="high">高能力</option></select></label>
          <label className="cloud-field"><span>优先级</span><input type="number" value={endpointForm.priority} onChange={e => setEndpointForm({ ...endpointForm, priority: Number(e.target.value) || 0 })} /></label>
          <label className="cloud-field"><span>特性 JSON</span><input value={endpointForm.features} onChange={e => setEndpointForm({ ...endpointForm, features: e.target.value })} placeholder='["tool", "vision"]' /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" onClick={createEndpoint} disabled={busy === 'endpoint'}>{busy === 'endpoint' ? '创建中...' : '创建端点'}</button></div>
      </SectionCard>

      <SectionCard title="模型端点" desc={'共 ' + dbEndpoints.length + ' 个数据库管理端点。'}>
        <DataTable columns={[{ key: 'name', label: '名称' }, { key: 'model', label: '模型' }, { key: 'protocol', label: '协议' }, { key: 'tier', label: '成本层级' }, { key: 'priority', label: '优先级' }, { key: 'status', label: '状态' }]} rows={endpointRows} />
        {dbEndpoints.length === 0 && <p className="cloud-inline-note">暂无数据库端点。请先创建至少一个模型端点，iWorker 才能使用 Center 的动态路由。</p>}
      </SectionCard>

      <SectionCard title="新增路由策略" desc="策略按优先级匹配工作类型和角色。未命中时使用启用端点的优先级回退。">
        <div className="cloud-form-grid">
          <label className="cloud-field"><span>策略名称</span><input value={policyForm.name} onChange={e => setPolicyForm({ ...policyForm, name: e.target.value })} placeholder="例如：数据任务优先低成本模型" /></label>
          <label className="cloud-field"><span>目标端点</span><select value={policyForm.endpoint_id} onChange={e => setPolicyForm({ ...policyForm, endpoint_id: e.target.value })}>{dbEndpoints.length === 0 ? <option value="">暂无端点</option> : dbEndpoints.map(e => <option key={e.id} value={e.id}>{e.name} · {e.model}</option>)}</select></label>
          <label className="cloud-field"><span>工作类型</span><input value={policyForm.work_type} onChange={e => setPolicyForm({ ...policyForm, work_type: e.target.value })} placeholder="* / data_cleaning / writing" /></label>
          <label className="cloud-field"><span>角色代码</span><input value={policyForm.role_code} onChange={e => setPolicyForm({ ...policyForm, role_code: e.target.value })} placeholder="* / data / office" /></label>
          <label className="cloud-field"><span>回退模式</span><select value={policyForm.fallback_mode} onChange={e => setPolicyForm({ ...policyForm, fallback_mode: e.target.value })}><option value="next_priority">按优先级回退</option><option value="any_tier">任意可用端点</option></select></label>
          <label className="cloud-field"><span>优先级</span><input type="number" value={policyForm.priority} onChange={e => setPolicyForm({ ...policyForm, priority: Number(e.target.value) || 0 })} /></label>
          <label className="cloud-field cloud-field-wide"><span>说明</span><input value={policyForm.description} onChange={e => setPolicyForm({ ...policyForm, description: e.target.value })} /></label>
        </div>
        <div className="cloud-actions"><button className="cloud-primary" onClick={createPolicy} disabled={busy === 'policy' || dbEndpoints.length === 0}>{busy === 'policy' ? '创建中...' : '创建路由策略'}</button></div>
      </SectionCard>

      <SectionCard title="路由策略" desc={'共 ' + dbPolicies.length + ' 条数据库管理策略。'}>
        <DataTable columns={[{ key: 'name', label: '策略名称' }, { key: 'work_type', label: '工作类型' }, { key: 'role', label: '角色' }, { key: 'endpoint', label: '目标端点' }, { key: 'fallback', label: '回退模式' }, { key: 'priority', label: '优先级' }, { key: 'status', label: '状态' }]} rows={policyRows} />
        {dbPolicies.length === 0 && <p className="cloud-inline-note">暂无路由策略。没有策略时，Center 会按端点优先级选择默认模型。</p>}
      </SectionCard>

      {settingsRows.length > 0 && (
        <SectionCard title="配置文件提供商" desc="来自 settings.json 的兼容配置，保留用于迁移和回退。">
          <DataTable columns={[{ key: 'name', label: '名称' }, { key: 'model', label: '模型' }, { key: 'protocol', label: '协议' }, { key: 'tier', label: '成本层级' }, { key: 'priority', label: '优先级' }, { key: 'status', label: '状态' }]} rows={settingsRows} />
        </SectionCard>
      )}

      {settingsRoutingRows.length > 0 && (
        <SectionCard title="配置文件路由策略" desc="来自 settings.json 的路由规则。">
          <DataTable columns={[{ key: 'scene', label: '场景' }, { key: 'primary', label: '默认模型' }, { key: 'backup', label: '备用模型' }, { key: 'route', label: '路由策略' }]} rows={settingsRoutingRows} />
        </SectionCard>
      )}
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
