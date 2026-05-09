import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';
import { useI18n } from '../i18n';

type AuditStats = {
  total_requests: number;
  ok_count: number;
  error_count: number;
  avg_latency_ms: number;
  mcp_events: number;
  skill_events: number;
  collaboration_events: number;
  model_events: number;
  mcp_errors: number;
  skill_errors: number;
  collaboration_errors: number;
  model_errors: number;
  top_provider: string;
  top_work_type: string;
  top_error_work_type: string;
  last_error_at: string;
};

type AuditLog = {
  id: string;
  request_id: string;
  provider_id: string;
  model: string;
  work_type: string;
  cost_tier: string;
  status: string;
  latency_ms: number;
  input_tokens: number;
  summary: string;
  error_msg: string;
  created_at: string;
};

type AuditScope = 'all' | 'mcp' | 'skill' | 'collaboration' | 'model' | 'errors';
type UsageRow = { metric: string; value: string; detail: string; scope: string };

const hasWails = () => typeof window !== 'undefined' && typeof (window as Window & { go?: unknown }).go !== 'undefined';

const emptyStats: AuditStats = {
  total_requests: 0,
  ok_count: 0,
  error_count: 0,
  avg_latency_ms: 0,
  mcp_events: 0,
  skill_events: 0,
  collaboration_events: 0,
  model_events: 0,
  mcp_errors: 0,
  skill_errors: 0,
  collaboration_errors: 0,
  model_errors: 0,
  top_provider: '',
  top_work_type: '',
  top_error_work_type: '',
  last_error_at: '',
};

async function fetchJSON<T>(url: string): Promise<T | null> {
  try {
    const resp = await fetch(url);
    if (!resp.ok) return null;
    return resp.json();
  } catch {
    return null;
  }
}

function formatTime(value: string) {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function shorten(value: string, limit = 180) {
  const text = value.trim();
  if (text.length <= limit) return text || '-';
  return `${text.slice(0, limit - 1)}...`;
}

function parseDetailFields(value: string) {
  const fields: Array<{ key: string; value: string }> = [];
  for (const part of value.split('|')) {
    const index = part.indexOf(':');
    if (index <= 0) continue;
    const key = part.slice(0, index).trim();
    const fieldValue = part.slice(index + 1).trim();
    if (key || fieldValue) fields.push({ key, value: fieldValue || '-' });
  }
  return fields;
}

function isScopeMatch(log: AuditLog, scope: AuditScope) {
  const workType = log.work_type || '';
  if (scope === 'all') return true;
  if (scope === 'errors') return log.status === 'error';
  if (scope === 'mcp') return workType.startsWith('mcp_');
  if (scope === 'skill') return workType.startsWith('skill_') || workType.includes('capability');
  if (scope === 'collaboration') return workType.includes('collaboration') || workType.includes('role_routing');
  if (scope === 'model') return Boolean(log.model || log.provider_id) && !workType.startsWith('mcp_') && !workType.startsWith('skill_');
  return true;
}

function categoryButtonClass(active: boolean, errorCount: number) {
  return [active ? 'is-active' : '', errorCount > 0 ? 'is-warn' : ''].filter(Boolean).join(' ');
}

function categoryErrorBadge(errorCount: number, label: string) {
  return errorCount > 0 ? <small>{label} {errorCount}</small> : null;
}

export function UsagePage() {
  const { t } = useI18n();
  const [stats, setStats] = useState<AuditStats>(emptyStats);
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [loading, setLoading] = useState(false);
  const [source, setSource] = useState('Center API');
  const [error, setError] = useState('');
  const [scope, setScope] = useState<AuditScope>('all');
  const [statusFilter, setStatusFilter] = useState('all');
  const [query, setQuery] = useState('');
  const [filterVersion, setFilterVersion] = useState(0);
  const [selectedLogId, setSelectedLogId] = useState('');

  const rowsFromStats = (value: AuditStats): UsageRow[] => {
    const successRate = value.total_requests > 0 ? `${((value.ok_count / value.total_requests) * 100).toFixed(1)}%` : '-';
    return [
      {
        metric: t('本地事件', 'Local events'),
        value: String(value.total_requests),
        detail: t('成功 ', 'Succeeded ') + value.ok_count,
        scope: t('最近 24 小时', 'Last 24 hours'),
      },
      {
        metric: t('成功率', 'Success rate'),
        value: successRate,
        detail: t('失败 ', 'Failed ') + value.error_count,
        scope: t('最近 24 小时', 'Last 24 hours'),
      },
      {
        metric: t('平均响应耗时', 'Average latency'),
        value: value.avg_latency_ms ? `${value.avg_latency_ms}ms` : '-',
        detail: value.top_work_type ? t('最常见类型：', 'Top event type: ') + value.top_work_type : '-',
        scope: t('治理与模型活动', 'Governance and model activity'),
      },
      {
        metric: 'MCP',
        value: String(value.mcp_events || 0),
        detail: value.mcp_errors ? t('错误 ', 'Errors ') + value.mcp_errors : t('企业 MCP 安装、启停与更新', 'Enterprise MCP installs, status changes, and updates'),
        scope: t('最近 24 小时', 'Last 24 hours'),
      },
      {
        metric: 'Skill',
        value: String(value.skill_events || 0),
        detail: value.skill_errors ? t('错误 ', 'Errors ') + value.skill_errors : t('Skill 演进、审核与能力治理', 'Skill evolution, review, and capability governance'),
        scope: t('最近 24 小时', 'Last 24 hours'),
      },
      {
        metric: t('协作', 'Collaboration'),
        value: String(value.collaboration_events || 0),
        detail: value.collaboration_errors ? t('错误 ', 'Errors ') + value.collaboration_errors : t('数字员工协作、路由与人工介入', 'Digital colleague collaboration, routing, and human intervention'),
        scope: t('最近 24 小时', 'Last 24 hours'),
      },
      {
        metric: t('模型活动', 'Model activity'),
        value: String(value.model_events || 0),
        detail: value.model_errors ? t('错误 ', 'Errors ') + value.model_errors : t('本地模型代理与路由调用', 'Local model proxy and routing calls'),
        scope: t('最近 24 小时', 'Last 24 hours'),
      },
    ];
  };

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const params = new URLSearchParams({ limit: '120' });
      if (statusFilter !== 'all') params.set('status', statusFilter);
      if (scope !== 'all') params.set('category', scope);
      if (query.trim()) params.set('q', query.trim());
      const [statsData, logsData] = await Promise.all([
        fetchJSON<AuditStats>('/admin/audit/stats?hours=24'),
        fetchJSON<{ logs?: AuditLog[] }>(`/admin/audit/logs?${params.toString()}`),
      ]);
      if (statsData) {
        setStats({ ...emptyStats, ...statsData });
        setSource('Center API');
      } else if (hasWails()) {
        const localStats = await (window as any).go.main.App.GetAuditStats(24);
        setStats(localStats ? { ...emptyStats, ...localStats } : emptyStats);
        setSource(localStats ? t('本地运行时', 'Local runtime') : t('无数据', 'No data'));
      } else {
        setStats(emptyStats);
        setSource(t('无数据', 'No data'));
      }
      const nextLogs = Array.isArray(logsData?.logs) ? logsData.logs : [];
      const scopedLogs = nextLogs.filter(log => isScopeMatch(log, scope));
      setLogs(scopedLogs);
      setSelectedLogId(current => scopedLogs.some(log => log.id === current) ? current : scopedLogs[0]?.id || '');
    } catch (err) {
      setError(err instanceof Error ? err.message : t('加载使用与审计数据失败。', 'Failed to load usage and audit data.'));
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    void load();
  }, [scope, statusFilter, filterVersion]);

  const rows = useMemo(() => rowsFromStats(stats), [stats, t]);
  const selectedLog = useMemo(() => logs.find(log => log.id === selectedLogId) || null, [logs, selectedLogId]);
  const successRate = stats.total_requests > 0 ? Math.round((stats.ok_count / stats.total_requests) * 100) : 0;

  return (
    <div className="center-page-stack">
      <SectionCard
        title={t('使用情况', 'Usage')}
        desc={t(
          '查看最近 24 小时的模型调用、错误、延迟和治理审计，用来判断 Center 与 iWorker 的运行负载。Cloud 不参与企业业务调用明细。',
          'Review model calls, errors, latency, and governance audit events from the last 24 hours. Cloud does not participate in enterprise business call details.',
        )}
      >
        <div className="cloud-status-grid">
          <StatusTile label={t('本地事件', 'Local events')} value={String(stats.total_requests)} tone="ok" />
          <StatusTile label={t('成功率', 'Success rate')} value={stats.total_requests ? `${successRate}%` : '-'} tone={stats.error_count ? 'warn' : 'ok'} />
          <StatusTile label={t('平均耗时', 'Average latency')} value={stats.avg_latency_ms ? `${stats.avg_latency_ms}ms` : '-'} />
          <StatusTile label={t('当前列表', 'Current list')} value={String(logs.length)} tone={logs.some(log => log.status === 'error') ? 'warn' : 'ok'} />
        </div>
        {stats.error_count > 0 ? (
          <div className="audit-health-strip">
            <strong>{t('错误热点', 'Error hotspot')}</strong>
            <span>{stats.top_error_work_type || t('未分类错误', 'Unclassified errors')}</span>
            <small>{stats.last_error_at ? t('最后错误：', 'Last error: ') + formatTime(stats.last_error_at) : t('最近 24 小时内有错误。', 'Errors occurred in the last 24 hours.')}</small>
            <button type="button" onClick={() => { setScope('errors'); setStatusFilter('error'); }}>{t('查看错误', 'View errors')}</button>
          </div>
        ) : null}
        <div className="audit-category-strip">
          <button type="button" className={categoryButtonClass(scope === 'mcp', stats.mcp_errors)} onClick={() => setScope('mcp')}>
            MCP <strong>{stats.mcp_events || 0}</strong>{categoryErrorBadge(stats.mcp_errors, t('错', 'Err'))}
          </button>
          <button type="button" className={categoryButtonClass(scope === 'skill', stats.skill_errors)} onClick={() => setScope('skill')}>
            Skill <strong>{stats.skill_events || 0}</strong>{categoryErrorBadge(stats.skill_errors, t('错', 'Err'))}
          </button>
          <button type="button" className={categoryButtonClass(scope === 'collaboration', stats.collaboration_errors)} onClick={() => setScope('collaboration')}>
            {t('协作', 'Collaboration')} <strong>{stats.collaboration_events || 0}</strong>{categoryErrorBadge(stats.collaboration_errors, t('错', 'Err'))}
          </button>
          <button type="button" className={categoryButtonClass(scope === 'model', stats.model_errors)} onClick={() => setScope('model')}>
            {t('模型', 'Model')} <strong>{stats.model_events || 0}</strong>{categoryErrorBadge(stats.model_errors, t('错', 'Err'))}
          </button>
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>
            {loading ? t('刷新中...', 'Refreshing...') : t('刷新统计', 'Refresh')}
          </button>
          <span className="cloud-inline-note">
            {t('数据来源：', 'Data source: ')}
            {source}
          </span>
        </div>
        {error ? <p className="cloud-message danger">{error}</p> : null}
      </SectionCard>

      <SectionCard
        title={t('活动统计', 'Activity Statistics')}
        desc={t(
          'Center 本地活动统计帮助管理员判断是否需要调整模型配额、算力分配、任务节流或治理规则。',
          'Center local activity statistics help administrators tune model quota, compute allocation, task throttling, or governance rules.',
        )}
      >
        <DataTable
          columns={[
            { key: 'metric', label: t('指标', 'Metric') },
            { key: 'value', label: t('当前值', 'Value') },
            { key: 'detail', label: t('详情', 'Detail') },
            { key: 'scope', label: t('统计范围', 'Scope') },
          ]}
          rows={rows}
        />
      </SectionCard>

      <SectionCard
        title={t('最近审计事件', 'Recent Audit Events')}
        desc={t(
          '按类型、状态和关键字定位 Center 本地治理事件，包括 MCP 安装/启停、Skill 演进、协作任务和模型调用错误。',
          'Filter local Center governance events by type, status, and keyword, including MCP management, skill evolution, collaboration tasks, and model call errors.',
        )}
      >
        <div className="cloud-form-grid">
          <label className="cloud-field">
            <span>{t('事件范围', 'Event scope')}</span>
            <select value={scope} onChange={event => setScope(event.target.value as AuditScope)}>
              <option value="all">{t('全部事件', 'All events')}</option>
              <option value="mcp">MCP</option>
              <option value="skill">Skill</option>
              <option value="collaboration">{t('协作', 'Collaboration')}</option>
              <option value="model">{t('模型调用', 'Model calls')}</option>
              <option value="errors">{t('错误事件', 'Errors')}</option>
            </select>
          </label>
          <label className="cloud-field">
            <span>{t('状态', 'Status')}</span>
            <select value={statusFilter} onChange={event => setStatusFilter(event.target.value)}>
              <option value="all">{t('全部状态', 'All statuses')}</option>
              <option value="ok">{t('正常', 'OK')}</option>
              <option value="error">{t('错误', 'Error')}</option>
            </select>
          </label>
          <label className="cloud-field cloud-field-wide">
            <span>{t('搜索', 'Search')}</span>
            <input value={query} onChange={event => setQuery(event.target.value)} placeholder={t('按摘要、详情、请求 ID 或事件类型搜索', 'Search summary, detail, request ID, or event type')} />
          </label>
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>
            {t('应用筛选', 'Apply filters')}
          </button>
          <button className="ghost" type="button" onClick={() => { setScope('all'); setStatusFilter('all'); setQuery(''); setFilterVersion(value => value + 1); }} disabled={loading}>
            {t('清除筛选', 'Clear filters')}
          </button>
        </div>
        {logs.length ? (
          <div className="audit-console-grid">
            <div className="data-table-wrap">
              <table className="data-table audit-event-table">
                <thead>
                  <tr>
                    <th>{t('时间', 'Time')}</th>
                    <th>{t('事件类型', 'Event type')}</th>
                    <th>{t('状态', 'Status')}</th>
                    <th>{t('摘要', 'Summary')}</th>
                    <th>{t('详情', 'Detail')}</th>
                  </tr>
                </thead>
                <tbody>
                  {logs.map(log => (
                    <tr key={log.id} className={log.id === selectedLogId ? 'is-selected' : ''} onClick={() => setSelectedLogId(log.id)}>
                      <td>{formatTime(log.created_at)}</td>
                      <td>{log.work_type || '-'}</td>
                      <td><span className={log.status === 'error' ? 'badge warn' : 'badge ok'}>{log.status || '-'}</span></td>
                      <td>{shorten(log.summary, 120)}</td>
                      <td>{shorten(log.error_msg || log.request_id || log.provider_id)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            {selectedLog ? (
              <AuditDetailPanel log={selectedLog} t={t} />
            ) : null}
          </div>
        ) : (
          <div className="empty-state">
            <strong>{t('暂无匹配事件', 'No matching events')}</strong>
            <p>{t('调整筛选条件，或等待 Center 产生 MCP 管理、Skill 演进、协作或模型调用记录。', 'Adjust filters, or wait for Center to produce MCP management, skill evolution, collaboration, or model call records.')}</p>
          </div>
        )}
      </SectionCard>

      {(stats.top_provider || stats.top_work_type) && (
        <SectionCard
          title={t('热点观察', 'Hotspots')}
          desc={t('最近 24 小时的高频使用模式。', 'High-frequency usage patterns from the last 24 hours.')}
        >
          <div className="item-list">
            {stats.top_provider && (
              <div className="item-row">
                <strong>{t('最常用提供商：', 'Top provider: ') + stats.top_provider}</strong>
                <p>{t('该提供商在最近 24 小时内被调用最多。', 'This provider was used most often in the last 24 hours.')}</p>
                <span className="badge info">{t('热点', 'Hotspot')}</span>
              </div>
            )}
            {stats.top_work_type && (
              <div className="item-row">
                <strong>{t('最常见事件类型：', 'Top event type: ') + stats.top_work_type}</strong>
                <p>{t('该事件类型在最近 24 小时内出现最多。', 'This event type appeared most often in the last 24 hours.')}</p>
                <span className="badge info">{t('热点', 'Hotspot')}</span>
              </div>
            )}
          </div>
        </SectionCard>
      )}
    </div>
  );
}

function AuditDetailPanel({ log, t }: { log: AuditLog; t: (zh: string, en: string) => string }) {
  const detailFields = parseDetailFields(log.error_msg || '');
  const metaFields = [
    { key: t('请求 ID', 'Request ID'), value: log.request_id || '-' },
    { key: t('提供商', 'Provider'), value: log.provider_id || '-' },
    { key: t('模型', 'Model'), value: log.model || '-' },
    { key: t('成本层级', 'Cost tier'), value: log.cost_tier || '-' },
    { key: t('延迟', 'Latency'), value: log.latency_ms ? `${log.latency_ms}ms` : '-' },
    { key: t('输入 Token', 'Input tokens'), value: log.input_tokens ? String(log.input_tokens) : '-' },
  ];
  return (
    <aside className="audit-detail-panel">
      <div className="audit-detail-head">
        <span className={log.status === 'error' ? 'badge warn' : 'badge ok'}>{log.status || '-'}</span>
        <strong>{log.work_type || t('审计事件', 'Audit event')}</strong>
        <small>{formatTime(log.created_at)}</small>
      </div>
      <p>{log.summary || t('无摘要', 'No summary')}</p>
      <div className="audit-detail-fields">
        {detailFields.length ? detailFields.map(field => (
          <div key={field.key}>
            <span>{field.key}</span>
            <strong>{field.value}</strong>
          </div>
        )) : (
          <div>
            <span>{t('详情', 'Detail')}</span>
            <strong>{log.error_msg || '-'}</strong>
          </div>
        )}
      </div>
      <div className="audit-detail-fields audit-detail-fields-muted">
        {metaFields.map(field => (
          <div key={field.key}>
            <span>{field.key}</span>
            <strong>{field.value}</strong>
          </div>
        ))}
      </div>
    </aside>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return (
    <div className={`cloud-status-tile ${tone || ''}`}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
