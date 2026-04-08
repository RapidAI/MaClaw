import { MetricCard } from '../components/cards/MetricCard';
import { SectionCard } from '../components/cards/SectionCard';
import { alerts, metrics, recentItems } from '../mock/dashboard';

const trends = [
  { title: '能力下发批次', desc: '最近 7 天共 14 批，成功率持续稳定', tag: '下发趋势' },
  { title: '日均调用量', desc: '较上周提升 12%，主要来自办公同事', tag: '使用趋势' },
  { title: '活跃员工数', desc: '18 位数字员工持续参与日常事务', tag: '活跃度' },
];

export function OverviewPage() {
  return (
    <div className="center-page-stack">
      <div className="metric-grid">
        {metrics.map((metric) => (
          <MetricCard key={metric.label} label={metric.label} value={metric.value} hint={metric.hint} />
        ))}
      </div>
      <div className="panel-grid">
        <SectionCard title="最近运行概况" desc="帮助管理员快速了解当前中心运行状态。">
          <div className="item-list">
            {recentItems.map((item) => (
              <div key={item.title} className="item-row">
                <strong>{item.title}</strong>
                <p>{item.description}</p>
                <span className="badge info">{item.status}</span>
              </div>
            ))}
          </div>
        </SectionCard>
        <SectionCard title="待处理告警" desc="优先处理影响协作和安全的事项。">
          <div className="item-list">
            {alerts.map((item) => (
              <div key={item.title} className="item-row">
                <strong>{item.title}</strong>
                <p>{item.description}</p>
                <span className="badge warn">{item.status}</span>
              </div>
            ))}
          </div>
        </SectionCard>
      </div>
      <SectionCard title="近期趋势" desc="补充近期下发和使用变化，帮助快速判断运行走势。">
        <div className="item-list">
          {trends.map((item) => (
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
