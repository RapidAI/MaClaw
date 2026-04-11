import { useEffect, useState } from 'react';
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
  id: string; name: string; protocol: string; model: string;
  cost_tier: string; priority: number; status: string;
};

type DBPolicy = {
  id: string; name: string; work_type: string; role_code: string;
  endpoint_id: string; fallback_mode: string; priority: number; status: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const tierLabel = (tier: string) => {
  switch (tier) {
    case 'low': return '💰 低成本';
    case 'medium': return '💰💰 中等';
    case 'high': return '💰💰💰 高成本';
    default: return tier || '未设置';
  }
};

async function fetchJSON<T>(url: string): Promise<T | null> {
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    return resp.json();
  } catch { return null; }
}

export function ModelRoutingPage() {
  const [providerRows, setProviderRows] = useState<Array<Record<string, string>>>([]);
  const [routingRows, setRoutingRows] = useState<Array<Record<string, string>>>([]);
  const [dbEndpoints, setDbEndpoints] = useState<DBEndpoint[]>([]);
  const [dbPolicies, setDbPolicies] = useState<DBPolicy[]>([]);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    // Load DB-managed endpoints and policies
    fetchJSON<{ endpoints: DBEndpoint[] }>('/admin/model-endpoints').then(d => {
      if (d?.endpoints) setDbEndpoints(d.endpoints);
    });
    fetchJSON<{ policies: DBPolicy[] }>('/admin/model-routing-policies').then(d => {
      if (d?.policies) setDbPolicies(d.policies);
    });

    // Load settings-based providers (existing)
    if (!hasWails()) return;
    setLoading(true);
    (window as any).go.main.App.LoadCenterSettings()
      .then((settings: CenterSettings) => {
        if (!settings) return;
        const providers = settings.providers || [];
        setProviderRows(providers.map((p) => ({
          name: p.name || p.id,
          model: p.model,
          protocol: p.protocol || 'openai',
          tier: tierLabel(p.cost_tier),
          priority: String(p.priority),
          status: p.enabled ? '✅ 启用' : '⏸ 停用',
        })));

        if (settings.work_type_tier && Object.keys(settings.work_type_tier).length > 0) {
          const rows = Object.entries(settings.work_type_tier).map(([workType, tier]) => {
            const tierProviders = providers.filter((p) => p.enabled && p.cost_tier === tier);
            return {
              scene: workType,
              primary: tierProviders[0]?.model || '-',
              backup: tierProviders[1]?.model || '-',
              route: `${tierLabel(tier)} 路由`,
            };
          });
          setRoutingRows(rows);
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  const endpointRows = dbEndpoints.map(e => ({
    name: e.name, model: e.model, protocol: e.protocol,
    tier: tierLabel(e.cost_tier), priority: String(e.priority), status: e.status,
  }));

  const policyRows = dbPolicies.map(p => ({
    name: p.name, work_type: p.work_type === '*' ? '全部' : p.work_type,
    role: p.role_code === '*' ? '全部' : p.role_code,
    fallback: p.fallback_mode, priority: String(p.priority), status: p.status,
  }));

  return (
    <div className="center-page-stack">
      {providerRows.length > 0 && (
        <SectionCard title="配置文件提供商" desc={`来自 settings.json 的提供商配置。${loading ? ' 加载中...' : ''}`}>
          <DataTable
            columns={[
              { key: 'name', label: '名称' },
              { key: 'model', label: '模型' },
              { key: 'protocol', label: '协议' },
              { key: 'tier', label: '成本层级' },
              { key: 'priority', label: '优先级' },
              { key: 'status', label: '状态' },
            ]}
            rows={providerRows}
          />
        </SectionCard>
      )}

      <SectionCard title="模型端点（DB 管理）" desc="数据库管理的模型端点，支持动态增删改。">
        <DataTable
          columns={[
            { key: 'name', label: '名称' },
            { key: 'model', label: '模型' },
            { key: 'protocol', label: '协议' },
            { key: 'tier', label: '成本层级' },
            { key: 'priority', label: '优先级' },
            { key: 'status', label: '状态' },
          ]}
          rows={endpointRows}
        />
        {dbEndpoints.length === 0 && <p style={{ color: '#888', padding: '8px 0' }}>暂无 DB 端点，可通过 API 创建。</p>}
      </SectionCard>

      <SectionCard title="路由策略（DB 管理）" desc="按工作类型和角色匹配端点的路由规则。">
        <DataTable
          columns={[
            { key: 'name', label: '策略名称' },
            { key: 'work_type', label: '工作类型' },
            { key: 'role', label: '角色' },
            { key: 'fallback', label: '回退模式' },
            { key: 'priority', label: '优先级' },
            { key: 'status', label: '状态' },
          ]}
          rows={policyRows}
        />
        {dbPolicies.length === 0 && <p style={{ color: '#888', padding: '8px 0' }}>暂无路由策略。</p>}
      </SectionCard>

      {routingRows.length > 0 && (
        <SectionCard title="配置文件路由策略" desc="来自 settings.json 的路由规则。">
          <DataTable
            columns={[
              { key: 'scene', label: '场景' },
              { key: 'primary', label: '默认模型' },
              { key: 'backup', label: '备用模型' },
              { key: 'route', label: '路由策略' },
            ]}
            rows={routingRows}
          />
        </SectionCard>
      )}
    </div>
  );
}
