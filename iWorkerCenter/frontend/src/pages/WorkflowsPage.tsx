import { useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type WorkflowDef = {
  id: string;
  name: string;
  description: string;
  trigger_type: string;
  status: string;
  created_at: string;
};

type WorkflowInstance = {
  id: string;
  definition_id: string;
  title: string;
  status: string;
  created_at: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const statusLabel = (s: string) => {
  switch (s) {
    case 'draft': return '草稿';
    case 'published': return '已发布';
    case 'disabled': return '已停用';
    case 'running': return '运行中';
    case 'completed': return '已完成';
    case 'rejected': return '已拒绝';
    default: return s;
  }
};

const triggerLabel = (t: string) => {
  switch (t) {
    case 'manual': return '手动触发';
    case 'scheduled': return '定时触发';
    case 'event': return '事件触发';
    default: return t;
  }
};

const defaultDefRows = [
  { name: '日报流转', trigger: '每天 18:00', description: '办公同事和生产同事之间的日报协作', status: '已发布' },
  { name: '异常升级', trigger: '命中高优先级规则', description: '高优先级异常自动转入复核链路', status: '草稿' },
  { name: '周报汇总', trigger: '每周五 16:00', description: '数据同事汇总各部门周报', status: '已发布' },
];

const defaultInstRows = [
  { title: '日报流转 #1', status: '运行中', created: '今天 18:00' },
  { title: '异常升级 #3', status: '已完成', created: '昨天 14:30' },
];

export function WorkflowsPage() {
  const [defRows, setDefRows] = useState(defaultDefRows);
  const [instRows, setInstRows] = useState(defaultInstRows);
  const [loading, setLoading] = useState(false);

  useEffect(() => {
    if (!hasWails()) return;
    setLoading(true);
    Promise.all([
      (window as any).go.main.App.ListWorkflows().catch(() => []),
      (window as any).go.main.App.ListWorkflowInstances().catch(() => []),
    ]).then(([defs, instances]: [WorkflowDef[], WorkflowInstance[]]) => {
      if (Array.isArray(defs) && defs.length > 0) {
        setDefRows(defs.map((d) => ({
          name: d.name,
          trigger: triggerLabel(d.trigger_type),
          description: d.description || '-',
          status: statusLabel(d.status),
        })));
      }
      if (Array.isArray(instances) && instances.length > 0) {
        setInstRows(instances.map((i) => ({
          title: i.title,
          status: statusLabel(i.status),
          created: i.created_at || '-',
        })));
      }
    }).finally(() => setLoading(false));
  }, []);

  return (
    <div className="center-page-stack">
      <SectionCard title="流程模板" desc={`共 ${defRows.length} 个流程模板。${loading ? ' 加载中...' : ''}`}>
        <DataTable
          columns={[
            { key: 'name', label: '流程名称' },
            { key: 'trigger', label: '触发方式' },
            { key: 'description', label: '描述' },
            { key: 'status', label: '状态' },
          ]}
          rows={defRows}
        />
      </SectionCard>
      <SectionCard title="流程实例" desc={`共 ${instRows.length} 个运行实例。`}>
        <DataTable
          columns={[
            { key: 'title', label: '实例标题' },
            { key: 'status', label: '状态' },
            { key: 'created', label: '创建时间' },
          ]}
          rows={instRows}
        />
      </SectionCard>
    </div>
  );
}
