import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

const modelRows = [
  { scene: '办公写作', primary: 'qwen3.5-plus', backup: 'glm-5', route: '按任务类型路由' },
  { scene: '数据分析', primary: 'MiniMax-M2.5', backup: 'qwen3-coder-plus', route: '优先高精度模型' },
  { scene: '生产汇总', primary: 'glm-4.7', backup: 'qwen3-max-2026-01-23', route: '失败时自动回退' },
];

const observations = [
  { title: '办公写作默认走 qwen3.5-plus', desc: '当前在高频场景下表现更稳定', tag: '默认策略' },
  { title: '生产汇总保留自动回退', desc: '主模型失败时切换备用模型以保障可用性', tag: '容错' },
];

const updates = [
  { title: '数据分析链路计划继续压测', desc: '准备验证高精度模型在高并发场景下的稳定性表现', tag: '待验证' },
  { title: '办公写作场景维持当前路由', desc: '短期内继续保留现有主备组合，减少频繁切换带来的波动', tag: '维持中' },
];

export function ModelRoutingPage() {
  return (
    <div className="center-page-stack">
      <SectionCard title="模型调度策略" desc="查看默认模型、备用模型和路由策略。">
        <DataTable
          columns={[
            { key: 'scene', label: '场景' },
            { key: 'primary', label: '默认模型' },
            { key: 'backup', label: '备用模型' },
            { key: 'route', label: '路由策略' },
          ]}
          rows={modelRows}
        />
      </SectionCard>
      <SectionCard title="策略观察" desc="补充当前模型策略侧重点和容错设计。">
        <div className="item-list">
          {observations.map((item) => (
            <div key={item.title} className="item-row">
              <strong>{item.title}</strong>
              <p>{item.desc}</p>
              <span className="badge info">{item.tag}</span>
            </div>
          ))}
        </div>
      </SectionCard>
      <SectionCard title="调整计划" desc="补充近期准备验证和继续维持的模型策略。">
        <div className="item-list">
          {updates.map((item) => (
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
