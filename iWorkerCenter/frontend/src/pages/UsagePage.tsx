import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

const rows = [
  { metric: '今日调用量', value: '3,248', trend: '+12%', scope: '全中心' },
  { metric: '活跃数字员工', value: '18', trend: '+2', scope: '最近 24 小时' },
  { metric: '平均响应耗时', value: '4.2s', trend: '-0.6s', scope: '核心任务' },
];

const hotspots = [
  { title: '办公同事调用量最高', desc: '周报、纪要和邮件草稿占比持续提升', tag: '热点' },
  { title: '数据同事响应更快', desc: '最近 7 天平均响应耗时下降明显', tag: '优化中' },
];

export function UsagePage() {
  return (
    <div className="center-page-stack">
      <SectionCard title="使用情况" desc="跟踪调用量、活跃度和关键趋势变化。">
        <DataTable
          columns={[
            { key: 'metric', label: '指标' },
            { key: 'value', label: '当前值' },
            { key: 'trend', label: '趋势' },
            { key: 'scope', label: '统计范围' },
          ]}
          rows={rows}
        />
      </SectionCard>
      <SectionCard title="热点观察" desc="补充高频使用场景和近期变化。">
        <div className="item-list">
          {hotspots.map((item) => (
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
