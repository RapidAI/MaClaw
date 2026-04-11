import { useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type Memory = {
  id: string;
  title: string;
  content: string;
  level: string;
  scope: string;
  tags: string[];
  version: number;
  status: string;
  created_at: string;
  updated_at: string;
};

type DedupResult = {
  merged: number;
  expired: number;
  scanned: number;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const levelLabel = (level: string) => {
  switch (level) {
    case 'enterprise': return '企业级';
    case 'role': return '角色级';
    case 'team': return '团队级';
    default: return level || '通用';
  }
};

const statusLabel = (status: string) => {
  switch (status) {
    case 'active': return '✅ 已共享';
    case 'draft': return '📝 草稿';
    case 'merged': return '🔗 已合并';
    case 'expired': return '⏰ 已过期';
    case 'disabled': return '⏸ 已停用';
    default: return status || '已共享';
  }
};

async function fetchJSON<T>(url: string): Promise<T | null> {
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    return resp.json();
  } catch { return null; }
}

export function KnowledgePage() {
  const [memories, setMemories] = useState<Memory[]>([]);
  const [loading, setLoading] = useState(false);
  const [dedupResult, setDedupResult] = useState<DedupResult | null>(null);
  const [dedupRunning, setDedupRunning] = useState(false);

  const loadMemories = () => {
    setLoading(true);
    fetchJSON<{ memories: Memory[] }>('/admin/memories').then(d => {
      if (d?.memories) {
        setMemories(d.memories);
        setLoading(false);
        return;
      }
      // Fallback to Wails
      if (!hasWails()) { setLoading(false); return; }
      (window as any).go.main.App.ListMemories()
        .then((mems: Memory[]) => {
          if (Array.isArray(mems)) setMemories(mems);
        })
        .catch(() => {})
        .finally(() => setLoading(false));
    });
  };

  useEffect(() => { loadMemories(); }, []);

  const runDedup = async () => {
    setDedupRunning(true);
    try {
      const resp = await fetch('/admin/memories/dedup', { method: 'POST' });
      if (resp.ok) {
        const result: DedupResult = await resp.json();
        setDedupResult(result);
        loadMemories(); // Refresh
      }
    } catch { /* ignore */ }
    setDedupRunning(false);
  };

  const activeMemories = memories.filter(m => m.status === 'active');
  const autoExtracted = memories.filter(m => m.tags?.includes('自动提取'));

  const rows = memories.map(m => ({
    topic: m.title,
    source: m.scope || '通用',
    level: levelLabel(m.level),
    tags: (m.tags || []).join(', '),
    updated: m.updated_at || m.created_at || '-',
    status: statusLabel(m.status),
  }));

  const levelCounts = memories.reduce<Record<string, number>>((acc, m) => {
    if (m.status !== 'active') return acc;
    const l = levelLabel(m.level);
    acc[l] = (acc[l] || 0) + 1;
    return acc;
  }, {});

  return (
    <div className="center-page-stack">
      <SectionCard title="经验共享" desc={`共 ${memories.length} 条记忆，${activeMemories.length} 条生效中，${autoExtracted.length} 条自动提取。${loading ? ' 加载中...' : ''}`}>
        <DataTable
          columns={[
            { key: 'topic', label: '主题' },
            { key: 'source', label: '范围' },
            { key: 'level', label: '级别' },
            { key: 'tags', label: '标签' },
            { key: 'updated', label: '更新时间' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>

      <SectionCard title="记忆维护" desc="去重合并近似经验，清理过期自动提取记忆（>90天）。">
        <div style={{ display: 'flex', gap: '12px', alignItems: 'center', padding: '8px 0' }}>
          <button
            onClick={runDedup}
            disabled={dedupRunning}
            style={{ padding: '6px 16px', borderRadius: '4px', border: '1px solid #ccc', cursor: 'pointer' }}
          >
            {dedupRunning ? '执行中...' : '🧹 执行去重与清理'}
          </button>
          {dedupResult && (
            <span style={{ color: '#666' }}>
              扫描 {dedupResult.scanned} 条，合并 {dedupResult.merged} 条，过期 {dedupResult.expired} 条
            </span>
          )}
        </div>
      </SectionCard>

      <SectionCard title="记忆分布" desc="按级别查看共享记忆的分布。">
        <div className="item-list">
          {Object.entries(levelCounts).map(([level, count]) => (
            <div key={level} className="item-row">
              <strong>{level}</strong>
              <p>{count} 条记忆</p>
              <span className="badge info">{level}</span>
            </div>
          ))}
          {Object.keys(levelCounts).length === 0 && <p style={{ color: '#888' }}>暂无活跃记忆。</p>}
        </div>
      </SectionCard>
    </div>
  );
}
