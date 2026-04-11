import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

const rows = [
  { batch: 'DLV-031', target: '18 个客户端', content: '安全规则更新', result: '成功 17 / 失败 1' },
  { batch: 'DLV-032', target: '12 个客户端', content: '能力包升级', result: '成功 12 / 失败 0' },
  { batch: 'DLV-033', target: '8 个客户端', content: '模型路由策略', result: '处理中' },
];

const retries = [
  { title: 'DLV-031 有 1 个客户端下发失败', desc: '失败节点已进入自动重试队列，等待网络恢复后再次下发', tag: '待重试' },
  { title: '模型路由策略批次仍在处理中', desc: '当前已有 5 个客户端完成更新，其余节点继续轮询状态', tag: '进行中' },
];

export function DeliveryPage() {
  return (
    <div className="center-page-stack">
      <SectionCard title="下发管理" desc="查看下发批次、结果统计和失败重试入口。">
        <DataTable
          columns={[
            { key: 'batch', label: '批次' },
            { key: 'target', label: '目标范围' },
            { key: 'content', label: '下发内容' },
            { key: 'result', label: '结果' },
          ]}
          rows={rows}
        />
      </SectionCard>
      <SectionCard title="重试与观察" desc="补充最近失败批次、处理进展和重试状态。">
        <div className="item-list">
          {retries.map((item) => (
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
