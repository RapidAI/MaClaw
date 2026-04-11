import { useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type ConfigBundle = {
  id: string;
  version: number;
  content_type: string;
  status: string;
  note: string;
  created_at: string;
  published_at: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const statusLabel = (s: string) => {
  switch (s) {
    case 'draft': return '草稿';
    case 'published': return '已发布';
    default: return s;
  }
};

const defaultRows = [
  { version: 'v1', content: '全量配置', note: '初始配置包', status: '已发布' },
];

export function DeliveryPage() {
  const [rows, setRows] = useState(defaultRows);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!hasWails()) return;
    setLoading(true);
    (window as any).go.main.App.ListConfigBundles()
      .then((bundles: ConfigBundle[]) => {
        if (Array.isArray(bundles) && bundles.length > 0) {
          setRows(bundles.map((b) => ({
            version: `v${b.version}`,
            content: b.content_type === 'full' ? '全量配置' : '增量配置',
            note: b.note || '-',
            status: statusLabel(b.status),
          })));
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="center-page-stack">
      <SectionCard title="配置下发" desc={`共 ${rows.length} 个配置包。${loading ? ' 加载中...' : ''}`}>
        <DataTable
          columns={[
            { key: 'version', label: '版本' },
            { key: 'content', label: '类型' },
            { key: 'note', label: '备注' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}
