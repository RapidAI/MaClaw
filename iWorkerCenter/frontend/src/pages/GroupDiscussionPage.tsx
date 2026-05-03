import { useEffect, useMemo, useState } from 'react';
import { SectionCard } from '../components/cards/SectionCard';
import { DataTable } from '../components/table/DataTable';

type GroupExpert = {
  agent_id?: string;
  display_name?: string;
  skills?: string[];
  description?: string;
  model_class?: string;
  languages?: string[];
  discoverable?: boolean;
  available?: boolean;
  updated_at?: string;
};

type GroupDiscussion = {
  id?: string;
  status?: string;
  topic?: string;
  question?: string;
  result_summary?: string;
  participant_ids?: string[];
  answer_count?: number;
  expected_answer_count?: number;
  ready_to_summarize?: boolean;
  readiness_reason?: string;
  created_at?: string;
  updated_at?: string;
};

type Snapshot = {
  experts?: GroupExpert[];
  discussions?: GroupDiscussion[];
};

type ExpertRow = Record<'name' | 'skills' | 'model' | 'languages' | 'status' | 'updated', string>;
type DiscussionRow = Record<'topic' | 'participants' | 'progress' | 'status' | 'updated', string>;
type ResultRow = Record<'topic' | 'result' | 'participants' | 'updated', string>;

const defaultSnapshot: Snapshot = {
  experts: [],
  discussions: [],
};

async function fetchSnapshot(): Promise<Snapshot | null> {
  try {
    const resp = await fetch('/admin/a2a/group-discussions');
    if (!resp.ok) return null;
    return resp.json();
  } catch {
    return null;
  }
}

const formatTime = (value?: string) => {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
};

const statusLabel = (status?: string) => {
  switch (status) {
    case 'open': return '讨论中';
    case 'decided': return '已完成';
    case 'escalated': return '已升级';
    case 'closed': return '已关闭';
    default: return status || '-';
  }
};

const expertRows = (experts: GroupExpert[]): ExpertRow[] => experts.map((expert) => ({
  name: expert.display_name || expert.agent_id || 'MaClaw',
  skills: (expert.skills || []).slice(0, 5).join('、') || expert.description || '-',
  model: expert.model_class || '-',
  languages: (expert.languages || []).join('、') || '-',
  status: expert.available ? '在线可用' : expert.discoverable ? '可发现' : '隐藏',
  updated: formatTime(expert.updated_at),
}));

const discussionRows = (items: GroupDiscussion[]): DiscussionRow[] => items.map((item) => ({
  topic: item.topic || item.question || item.id || '-',
  participants: (item.participant_ids || []).join('、') || '-',
  progress: `${item.answer_count ?? 0}/${item.expected_answer_count ?? 1}${item.ready_to_summarize ? ' · 可收尾' : ''}`,
  status: statusLabel(item.status),
  updated: formatTime(item.updated_at || item.created_at),
}));

const resultRows = (items: GroupDiscussion[]): ResultRow[] => items
  .filter((item) => item.result_summary)
  .map((item) => ({
    topic: item.topic || item.question || item.id || '-',
    result: item.result_summary || '-',
    participants: (item.participant_ids || []).join('、') || '-',
    updated: formatTime(item.updated_at || item.created_at),
  }));

export function GroupDiscussionPage() {
  const [snapshot, setSnapshot] = useState<Snapshot>(defaultSnapshot);
  const [loading, setLoading] = useState(false);
  const [source, setSource] = useState('Center API');
  const [error, setError] = useState('');

  const load = async () => {
    setLoading(true);
    setError('');
    try {
      const data = await fetchSnapshot();
      if (data) {
        setSnapshot({ experts: data.experts || [], discussions: data.discussions || [] });
        setSource('Center API');
      } else {
        setSnapshot(defaultSnapshot);
        setSource('暂无数据');
      }
    } catch (err) {
      setError(err instanceof Error ? err.message : '加载群组讨论失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => { void load(); }, []);

  const experts = snapshot.experts || [];
  const discussions = snapshot.discussions || [];
  const results = useMemo(() => resultRows(discussions), [discussions]);
  const activeExperts = experts.filter((expert) => expert.available).length;
  const activeDiscussions = discussions.filter((item) => item.status === 'open').length;
  const readyDiscussions = discussions.filter((item) => item.ready_to_summarize).length;

  return (
    <div className="center-page-stack">
      <SectionCard title="群组讨论总览" desc="仅展示当前 Hub 内 MaClaw 专家、讨论主题、参与者和已产出的讨论结果。跨 Hub、HubCenter 和 AgentNet 不参与此列表。">
        <div className="cloud-status-grid">
          <StatusTile label="活跃专家" value={String(activeExperts)} tone="ok" />
          <StatusTile label="专家总数" value={String(experts.length)} />
          <StatusTile label="讨论中" value={String(activeDiscussions)} tone={activeDiscussions > 0 ? 'warn' : undefined} />
          <StatusTile label="可收尾" value={String(readyDiscussions)} tone={readyDiscussions > 0 ? 'ok' : undefined} />
          <StatusTile label="历史讨论" value={String(discussions.length)} />
        </div>
        <div className="cloud-actions">
          <button className="ghost" type="button" onClick={() => { void load(); }} disabled={loading}>{loading ? '刷新中' : '刷新群组讨论'}</button>
          <span className="cloud-inline-note">数据来源：{source} / 作用域：当前 Hub</span>
        </div>
        {error ? <p className="cloud-message danger">{error}</p> : null}
      </SectionCard>

      <SectionCard title="当前活跃 MaClaw 专家" desc="开启群组讨论且可发现的 MaClaw 会出现在这里；Hub 只展示其公开专家身份，不展示本机路径、密钥或私有上下文。">
        <DataTable
          columns={[
            { key: 'name', label: '专家' },
            { key: 'skills', label: '擅长方向' },
            { key: 'model', label: '模型能力' },
            { key: 'languages', label: '语言' },
            { key: 'status', label: '状态' },
            { key: 'updated', label: '最近心跳' },
          ]}
          rows={expertRows(experts)}
        />
        {experts.length === 0 ? <p style={{ color: '#888', padding: '8px 0' }}>暂无活跃 MaClaw 专家。请在 MaClaw 设置中开启“群组讨论”和“可被发现”。</p> : null}
      </SectionCard>

      <SectionCard title="历史讨论" desc="按最近更新时间展示当前 Hub 内的讨论主题和参与者。">
        <DataTable
          columns={[
            { key: 'topic', label: '主题 / 问题' },
            { key: 'participants', label: '参与者' },
            { key: 'progress', label: '答复进度' },
            { key: 'status', label: '状态' },
            { key: 'updated', label: '更新时间' },
          ]}
          rows={discussionRows(discussions)}
        />
        {discussions.length === 0 ? <p style={{ color: '#888', padding: '8px 0' }}>暂无历史讨论。</p> : null}
      </SectionCard>

      <SectionCard title="讨论结果" desc="展示已形成建议、决策或升级结论的讨论摘要。">
        <DataTable
          columns={[
            { key: 'topic', label: '主题' },
            { key: 'result', label: '结果摘要' },
            { key: 'participants', label: '参与者' },
            { key: 'updated', label: '完成时间' },
          ]}
          rows={results}
        />
        {results.length === 0 ? <p style={{ color: '#888', padding: '8px 0' }}>暂无讨论结果。</p> : null}
      </SectionCard>
    </div>
  );
}

function StatusTile({ label, value, tone }: { label: string; value: string; tone?: 'ok' | 'warn' }) {
  return (
    <div className={'cloud-status-tile ' + (tone || '')}>
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
