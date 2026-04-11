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
  status: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const defaultRows = [
  { name: '会议纪要', category: '办公', version: '1.0.0', source: '本地', status: '启用中' },
  { name: '异常上报', category: '生产', version: '1.0.0', source: '本地', status: '启用中' },
  { name: '质量分析', category: '质量', version: '1.0.0', source: '本地', status: '启用中' },
];

export function PackagesPage() {
  const [rows, setRows] = useState(defaultRows);

  useEffect(() => {
    if (!hasWails()) return;
    (window as any).go.main.App.ListCapabilities()
      .then((caps: Capability[]) => {
        if (Array.isArray(caps) && caps.length > 0) {
          setRows(caps.map((c) => ({
            name: c.name,
            category: c.category || '通用',
            version: c.version,
            source: c.source || '本地',
            status: c.status === 'active' ? '启用中' : '已停用',
          })));
        }
      })
      .catch(() => {});
  }, []);

  return (
    <div className="center-page-stack">
      <SectionCard title="能力包列表" desc={`共 ${rows.length} 个能力包。管理能力包来源、版本和分发状态。`}>
        <DataTable
          columns={[
            { key: 'name', label: '能力包名称' },
            { key: 'category', label: '分类' },
            { key: 'version', label: '版本' },
            { key: 'source', label: '来源' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}
