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
    case 'active': return '已共享';
    case 'draft': return '草稿';
    case 'archived': return '已归档';
    default: return status || '已共享';
  }
};

const defaultRows = [
  { topic: '异常复盘模板', source: '生产团队', level: '团队级', updated: '今天 09:20', status: '已共享' },
  { topic: '周报写作范式', source: '办公同事', level: '角色级', updated: '昨天 18:40', status: '审核中' },
  { topic: '质量分析口径', source: '质量团队', level: '企业级', updated: '周一 14:10', status: '已共享' },
];

export function KnowledgePage() {
  const [rows, setRows] = useState(defaultRows);
  const [loading, setLoading] = useState(false);
  const [totalCount, setTotalCount] = useState(defaultRows.length);

  useEffect(() => {
    if (!hasWails()) return;
    setLoading(true);
    (window as any).go.main.App.ListMemories()
      .then((mems: Memory[]) => {
        if (Array.isArray(mems) && mems.length > 0) {
          setTotalCount(mems.length);
          setRows(mems.map((m) => ({
            topic: m.title,
            source: m.scope || '通用',
            level: levelLabel(m.level),
            updated: m.updated_at || m.created_at || '-',
            status: statusLabel(m.status),
          })));
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  // Derive stats from rows
  const sharedCount = rows.filter((r) => r.status === '已共享').length;
  const levelCounts = rows.reduce<Record<string, number>>((acc, r) => {
    acc[r.level] = (acc[r.level] || 0) + 1;
    return acc;
  }, {});

  return (
    <div className="center-page-stack">
      <SectionCard title="经验共享" desc={`共 ${totalCount} 条共享记忆，${sharedCount} 条已生效。${loading ? ' 加载中...' : ''}`}>
        <DataTable
          columns={[
            { key: 'topic', label: '经验主题' },
            { key: 'source', label: '来源/范围' },
            { key: 'level', label: '级别' },
            { key: 'updated', label: '最近更新' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
      <SectionCard title="记忆分布" desc="按级别查看共享记忆的分布情况。">
        <div className="item-list">
          {Object.entries(levelCounts).map(([level, count]) => (
            <div key={level} className="item-row">
              <strong>{level}</strong>
              <p>{count} 条记忆</p>
              <span className="badge info">{level}</span>
            </div>
          ))}
        </div>
      </SectionCard>
    </div>
  );
}
