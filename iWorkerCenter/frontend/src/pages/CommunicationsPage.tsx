import { useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type CollabTask = {
  id: string;
  title: string;
  from_colleague_id: string;
  to_colleague_id: string;
  to_role_code: string;
  status: string;
  created_at: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const statusLabel = (s: string) => {
  switch (s) {
    case 'pending': return '待处理';
    case 'accepted': return '已接受';
    case 'in_progress': return '处理中';
    case 'completed': return '已完成';
    case 'rejected': return '已拒绝';
    default: return s;
  }
};

const defaultRows = [
  { id: 'MSG-2048', title: '周报整理', from: '小迪', to: '阿宁', status: '已完成' },
  { id: 'MSG-2049', title: '异常说明', from: '老陈', to: '小周', status: '处理中' },
  { id: 'MSG-2050', title: '汇报润色', from: '阿宁', to: '小迪', status: '待处理' },
];

export function CommunicationsPage() {
  const [rows, setRows] = useState(defaultRows);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!hasWails()) return;
    setLoading(true);
    (window as any).go.main.App.ListCollaborations()
      .then((tasks: CollabTask[]) => {
        if (Array.isArray(tasks) && tasks.length > 0) {
          setRows(tasks.map((t) => ({
            id: t.id.slice(0, 12),
            title: t.title,
            from: t.from_colleague_id.slice(0, 8),
            to: t.to_colleague_id.slice(0, 8),
            status: statusLabel(t.status),
          })));
        }
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="center-page-stack">
      <SectionCard title="协作委托记录" desc={`共 ${rows.length} 条协作记录。${loading ? ' 加载中...' : ''}`}>
        <DataTable
          columns={[
            { key: 'id', label: '任务 ID' },
            { key: 'title', label: '任务标题' },
            { key: 'from', label: '发起方' },
            { key: 'to', label: '接收方' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
    </div>
  );
}
