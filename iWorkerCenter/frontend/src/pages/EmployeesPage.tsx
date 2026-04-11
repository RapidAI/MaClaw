import { useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type Colleague = {
  id: string;
  name: string;
  role_name: string;
  role_code: string;
  description: string;
  status: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const defaultRows = [
  { name: '小迪', role: '办公同事', description: '擅长通知、纪要、周报', status: '启用中' },
  { name: '阿宁', role: '数据同事', description: '擅长表格整理、数据汇总', status: '启用中' },
  { name: '老陈', role: '生产同事', description: '擅长日报、交接班', status: '启用中' },
];

export function EmployeesPage() {
  const [rows, setRows] = useState(defaultRows);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!hasWails()) return;
    setLoading(true);
    (window as any).go.main.App.ListColleagues()
      .then((cols: Colleague[]) => {
        if (Array.isArray(cols) && cols.length > 0) {
          setRows(cols.map((c) => ({
            name: c.name,
            role: c.role_name || c.role_code || '通用',
            description: c.description,
            status: c.status === 'active' ? '启用中' : '已停用',
          })));
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="center-page-stack">
      <SectionCard title="数字员工列表" desc={`共 ${rows.length} 位数字员工。${loading ? '加载中...' : ''}`}>
        <DataTable
          columns={[
            { key: 'name', label: '员工名称' },
            { key: 'role', label: '角色' },
            { key: 'description', label: '描述' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}
