import { useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type Capability = {
  id: string;
  name: string;
  description: string;
  category: string;
  version: string;
  source: string;
  risk_level: string;
  status: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

async function fetchJSON<T>(url: string): Promise<T | null> {
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    return resp.json();
  } catch { return null; }
}

const statusLabel = (s: string) => {
  switch (s) {
    case 'active': return '✅ 启用';
    case 'pending_review': return '🔍 待审核';
    case 'rejected': return '❌ 已拒绝';
    case 'disabled': return '⏸ 已停用';
    default: return s;
  }
};

const riskLabel = (r: string) => {
  switch (r) {
    case 'low': return '🟢 低';
    case 'medium': return '🟡 中';
    case 'high': return '🔴 高';
    default: return r || '低';
  }
};

export function PackagesPage() {
  const [caps, setCaps] = useState<Capability[]>([]);
  const [pendingCaps, setPendingCaps] = useState<Capability[]>([]);

  const loadCaps = () => {
    // Try API first
    fetchJSON<{ capabilities: Capability[] }>('/admin/capabilities').then(d => {
      if (d?.capabilities) {
        setCaps(d.capabilities.filter(c => c.status !== 'pending_review'));
        setPendingCaps(d.capabilities.filter(c => c.status === 'pending_review'));
        return;
      }
      // Fallback to Wails
      if (!hasWails()) return;
      (window as any).go.main.App.ListCapabilities()
        .then((list: Capability[]) => {
          if (Array.isArray(list)) {
            setCaps(list.filter(c => c.status !== 'pending_review'));
            setPendingCaps(list.filter(c => c.status === 'pending_review'));
          }
        })
        .catch(() => {});
    });
  };

  useEffect(() => { loadCaps(); }, []);

  const activeRows = caps.map(c => ({
    name: c.name,
    category: c.category || '通用',
    version: c.version,
    source: c.source?.startsWith('hubcenter:') ? '🌐 HubCenter' : '📁 本地',
    risk: riskLabel(c.risk_level),
    status: statusLabel(c.status),
  }));

  const pendingRows = pendingCaps.map(c => ({
    name: c.name,
    category: c.category || '通用',
    version: c.version,
    source: c.source?.startsWith('hubcenter:') ? '🌐 HubCenter' : '📁 本地',
    risk: riskLabel(c.risk_level),
    id: c.id,
  }));

  return (
    <div className="center-page-stack">
      {pendingCaps.length > 0 && (
        <SectionCard title="待审核能力包" desc={`${pendingCaps.length} 个从 HubCenter 导入的能力包等待审核。`}>
          <DataTable
            columns={[
              { key: 'name', label: '名称' },
              { key: 'category', label: '分类' },
              { key: 'version', label: '版本' },
              { key: 'source', label: '来源' },
              { key: 'risk', label: '风险等级' },
            ]}
            rows={pendingRows}
          />
        </SectionCard>
      )}

      <SectionCard title="能力包列表" desc={`共 ${caps.length} 个能力包。支持本地创建和从 HubCenter 导入。`}>
        <DataTable
          columns={[
            { key: 'name', label: '名称' },
            { key: 'category', label: '分类' },
            { key: 'version', label: '版本' },
            { key: 'source', label: '来源' },
            { key: 'risk', label: '风险等级' },
            { key: 'status', label: '状态' },
          ]}
          rows={activeRows}
        />
        {caps.length === 0 && <p style={{ color: '#888', padding: '8px 0' }}>暂无能力包。</p>}
      </SectionCard>
    </div>
  );
}
