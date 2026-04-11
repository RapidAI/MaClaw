import { useEffect, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type AuditStats = {
  total_requests: number;
  ok_count: number;
  error_count: number;
  avg_latency_ms: number;
  top_provider: string;
  top_work_type: string;
};

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const defaultRows = [
  { metric: '今日调用量', value: '0', trend: '-', scope: '全中心' },
  { metric: '成功率', value: '-', trend: '-', scope: '最近 24 小时' },
  { metric: '平均响应耗时', value: '-', trend: '-', scope: '核心任务' },
];

export function UsagePage() {
  const [rows, setRows] = useState(defaultRows);
  const [loading, setLoading] = useState(false);
  const [topInfo, setTopInfo] = useState<{ provider: string; workType: string }>({ provider: '', workType: '' });

  useEffect(() => {
    if (!hasWails()) return;
    setLoading(true);
    (window as any).go.main.App.GetAuditStats(24)
      .then((stats: AuditStats) => {
        if (!stats) return;
        const successRate = stats.total_requests > 0
          ? `${((stats.ok_count / stats.total_requests) * 100).toFixed(1)}%`
          : '-';
        setRows([
          { metric: '今日调用量', value: String(stats.total_requests), trend: `成功 ${stats.ok_count}`, scope: '最近 24 小时' },
          { metric: '成功率', value: successRate, trend: `失败 ${stats.error_count}`, scope: '最近 24 小时' },
          { metric: '平均响应耗时', value: `${stats.avg_latency_ms}ms`, trend: '-', scope: '核心任务' },
        ]);
        setTopInfo({ provider: stats.top_provider || '-', workType: stats.top_work_type || '-' });
      })
      .catch(() => {})
      .finally(() => setLoading(false));
  }, []);

  return (
    <div className="center-page-stack">
      <SectionCard title="使用情况" desc={`LLM 代理调用统计。${loading ? ' 加载中...' : ''}`}>
        <DataTable
          columns={[
            { key: 'metric', label: '指标' },
            { key: 'value', label: '当前值' },
            { key: 'trend', label: '详情' },
            { key: 'scope', label: '统计范围' },
          ]}
          rows={rows}
        />
      </SectionCard>
      {(topInfo.provider || topInfo.workType) && (
        <SectionCard title="热点观察" desc="最近 24 小时的高频使用模式。">
          <div className="item-list">
            {topInfo.provider && topInfo.provider !== '-' && (
              <div className="item-row">
                <strong>最常用提供商: {topInfo.provider}</strong>
                <p>该提供商在最近 24 小时内被调用最多</p>
                <span className="badge info">热点</span>
              </div>
            )}
            {topInfo.workType && topInfo.workType !== '-' && (
              <div className="item-row">
                <strong>最常见任务类型: {topInfo.workType}</strong>
                <p>该任务类型在最近 24 小时内出现最多</p>
                <span className="badge info">热点</span>
              </div>
            )}
          </div>
        </SectionCard>
      )}
    </div>
  );
}
