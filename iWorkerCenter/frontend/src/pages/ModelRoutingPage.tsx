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

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const tierLabel = (tier: string) => {
  switch (tier) {
    case 'low': return '💰 低成本';
    case 'medium': return '💰💰 中等';
    case 'high': return '💰💰💰 高成本';
    default: return tier || '未设置';
  }
};

const defaultModelRows = [
  { scene: '办公写作', primary: 'qwen3.5-plus', backup: 'glm-5', route: '按任务类型路由' },
  { scene: '数据分析', primary: 'MiniMax-M2.5', backup: 'qwen3-coder-plus', route: '优先高精度模型' },
  { scene: '生产汇总', primary: 'glm-4.7', backup: 'qwen3-max-2026-01-23', route: '失败时自动回退' },
];

export function ModelRoutingPage() {
  const [providerRows, setProviderRows] = useState<Array<Record<string, string>>>([]);
  const [routingRows, setRoutingRows] = useState(defaultModelRows);
  const [loading, setLoading] = useState(false);
  const [providerCount, setProviderCount] = useState(0);

  useEffect(() => {
    if (!hasWails()) return;
    setLoading(true);
    (window as any).go.main.App.LoadCenterSettings()
      .then((settings: CenterSettings) => {
        if (!settings) return;

        // Build provider table
        const providers = settings.providers || [];
        setProviderCount(providers.length);
        setProviderRows(providers.map((p) => ({
          name: p.name || p.id,
          model: p.model,
          protocol: p.protocol || 'openai',
          tier: tierLabel(p.cost_tier),
          priority: String(p.priority),
          status: p.enabled ? '✅ 启用' : '⏸ 停用',
        })));

        // Build routing rules table from work_type_tier
        if (settings.work_type_tier && Object.keys(settings.work_type_tier).length > 0) {
          const rows = Object.entries(settings.work_type_tier).map(([workType, tier]) => {
            // Find matching providers for this tier
            const tierProviders = providers.filter((p) => p.enabled && p.cost_tier === tier);
            const primary = tierProviders[0]?.model || '-';
            const backup = tierProviders[1]?.model || '-';
            return {
              scene: workType,
              primary,
              backup,
              route: `${tierLabel(tier)} 路由`,
            };
          });
          setRoutingRows(rows);
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="center-page-stack">
      {providerRows.length > 0 && (
        <SectionCard title="模型提供商" desc={`共 ${providerCount} 个提供商。${loading ? ' 加载中...' : ''}`}>
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
      <SectionCard title="模型调度策略" desc="查看默认模型、备用模型和路由策略。">
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
    </div>
  );
}
