import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

const rows = [
  { name: '日报流转', trigger: '每天 18:00', owner: '办公同事', status: '运行中' },
  { name: '异常升级', trigger: '命中高优先级规则', owner: '生产同事', status: '待确认' },
  { name: '周报汇总', trigger: '每周五 16:00', owner: '数据同事', status: '已发布' },
];

const recentRuns = [
  { title: '日报流转今日已执行 18 次', desc: '主要覆盖办公同事和生产同事之间的协作', tag: '运行稳定' },
  { title: '异常升级最近命中 3 次', desc: '高优先级异常已自动转入复核链路', tag: '需要关注' },
];

export function WorkflowsPage() {
  return (
    <div className="center-page-stack">
      <SectionCard title="流程设计" desc="查看事务模板、触发方式和当前发布状态。">
        <DataTable
          columns={[
            { key: 'name', label: '流程名称' },
            { key: 'trigger', label: '触发方式' },
            { key: 'owner', label: '默认负责人' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
      <SectionCard title="最近运行" desc="补充最近执行情况和异常命中，帮助快速判断流程状态。">
        <div className="item-list">
          {recentRuns.map((item) => (
            <div key={item.title} className="item-row">
              <strong>{item.title}</strong>
              <p>{item.desc}</p>
              <span className="badge info">{item.tag}</span>
            </div>
          ))}
        </div>
      </SectionCard>
    </div>
  );
}
