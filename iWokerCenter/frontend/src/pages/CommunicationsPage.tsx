import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

const rows = [
  { id: 'MSG-2048', from: '小迪', to: '阿宁', task: '周报整理', status: '已完成' },
  { id: 'MSG-2049', from: '老陈', to: '小周', task: '异常说明', status: '处理中' },
  { id: 'MSG-2050', from: '阿宁', to: '小迪', task: '汇报润色', status: '待返回' },
];

const observations = [
  { title: '阿宁向小迪发起的润色请求待返回', desc: '当前停留在结果回传阶段，建议优先查看处理中链路', tag: '待跟进' },
  { title: '周报整理协作链路已稳定完成', desc: '高频任务在最近批次中保持较高完成率', tag: '运行稳定' },
];

export function CommunicationsPage() {
  return (
    <div className="center-page-stack">
      <SectionCard title="员工通讯记录" desc="查看数字员工之间的协作请求、处理状态与结果返回。">
        <DataTable
          columns={[
            { key: 'id', label: '通讯 ID' },
            { key: 'from', label: '发起方' },
            { key: 'to', label: '接收方' },
            { key: 'task', label: '所属任务' },
            { key: 'status', label: '状态' },
          ]}
          rows={rows}
        />
      </SectionCard>
      <SectionCard title="协作观察" desc="补充最近待返回事项和高频协作状态。">
        <div className="item-list">
          {observations.map((item) => (
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
