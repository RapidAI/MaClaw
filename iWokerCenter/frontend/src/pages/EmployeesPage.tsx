import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

const rows = [
  { name: '小迪', role: '办公同事', model: '默认办公模型', status: '启用中' },
  { name: '阿宁', role: '数据同事', model: '数据分析模型', status: '启用中' },
  { name: '老陈', role: '生产同事', model: '生产汇总模型', status: '待下发' },
];

const states = [
  { title: '办公同事活跃度较高', desc: '今日承担周报、纪要和邮件类任务最多', tag: '活跃' },
  { title: '生产同事待下发', desc: '当前模型策略更新后仍需重新推送客户端', tag: '待处理' },
];

const assignments = [
  { title: '办公同事负责高频事务', desc: '当前覆盖周报、纪要、邮件草稿等标准办公场景', tag: '主力角色' },
  { title: '数据与生产同事仍有待办', desc: '需要继续完成模型策略同步和客户端下发确认', tag: '待同步' },
];

export function EmployeesPage() {
  return (
    <div className="center-page-stack">
      <SectionCard title="数字员工列表" desc="管理身份、角色、能力偏好和模型策略。">
        <DataTable
          columns={[
            { key: 'name', label: '员工名称' },
            { key: 'role', label: '员工类型' },
            { key: 'model', label: '默认模型策略' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
      <SectionCard title="员工状态观察" desc="补充数字员工最近活跃度和待处理状态。">
        <div className="item-list">
          {states.map((item) => (
            <div key={item.title} className="item-row">
              <strong>{item.title}</strong>
              <p>{item.desc}</p>
              <span className="badge info">{item.tag}</span>
            </div>
          ))}
        </div>
      </SectionCard>
      <SectionCard title="角色分工与待办" desc="补充当前角色分工重点和仍需处理的同步事项。">
        <div className="item-list">
          {assignments.map((item) => (
            <div key={item.title} className="item-row">
              <strong>{item.title}</strong>
              <p>{item.desc}</p>
              <span className="badge warn">{item.tag}</span>
            </div>
          ))}
        </div>
      </SectionCard>
    </div>
  );
}
