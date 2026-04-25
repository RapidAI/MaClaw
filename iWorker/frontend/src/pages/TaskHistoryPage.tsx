import { useMemo, useState } from 'react';
import type { HistoryTaskItem } from '../types';

type Props = {
  tasks: HistoryTaskItem[];
  onResumeTask: (task: HistoryTaskItem) => void;
  onViewResult: (task: HistoryTaskItem) => void;
  onCloneTask: (task: HistoryTaskItem) => void;
  onDeleteTask: (task: HistoryTaskItem) => void;
  viewedTask: HistoryTaskItem | null;
};

type ToolItem = {
  id: string;
  name: string;
  icon: string;
  description: string;
  badge?: string;
};

const installedTools: ToolItem[] = [
  { id: 'capability-evolver', name: 'capability-evolver', icon: 'CE', description: '用于沉淀和演化组织能力图谱。' },
  { id: 'openclaw-assets', name: 'openclaw-assets-to-iworker', icon: 'OA', description: '将 OpenClaw 个人资产迁移到 iWorker 对应目录。' },
  { id: 'libtv-skill', name: 'libtv-skill', icon: 'LT', description: '接入图像与视频生成能力，扩展内容生产场景。' },
  { id: 'pptx', name: 'pptx', icon: 'P', description: '演示文稿创建、编辑与分析能力。', badge: '官方' },
];

const catalogTabs = ['推荐', 'SkillHub', '套件'] as const;

const catalogTools: ToolItem[] = [
  { id: 'tencent-doc', name: '腾讯文档', icon: 'TD', description: '创建、读取和协作文档内容。' },
  { id: 'tencent-meeting', name: '腾讯会议', icon: 'TM', description: '预约会议、管理录制与整理纪要。' },
  { id: 'tencent-ima', name: '腾讯 ima', icon: 'TI', description: '连接知识库与检索能力。' },
  { id: 'neodata', name: 'NeoData', icon: 'ND', description: '金融检索与数据洞察服务。' },
  { id: 'cnb-cool', name: 'cnb.cool', icon: 'CN', description: '研发协作平台连接能力。' },
  { id: 'brand-design', name: '品牌设计专家', icon: 'BD', description: '复用品牌视觉资产，快速生成设计方案。' },
];

export function TaskHistoryPage({ tasks, onResumeTask, onViewResult, onCloneTask, onDeleteTask, viewedTask }: Props) {
  const [activeTab, setActiveTab] = useState<typeof catalogTabs[number]>('推荐');
  const [searchQuery, setSearchQuery] = useState('');

  const filteredCatalog = useMemo(() => {
    if (!searchQuery) {
      return catalogTools;
    }
    return catalogTools.filter((tool) => tool.name.includes(searchQuery) || tool.description.includes(searchQuery));
  }, [searchQuery]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px', height: '100%', overflow: 'auto' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '16px', flexWrap: 'wrap' }}>
        <div>
          <h2 style={{ fontSize: '18px', fontWeight: 700, color: 'var(--dw-tools-title)', margin: '0 0 4px' }}>工具与任务</h2>
          <p style={{ fontSize: '13px', color: 'var(--dw-tools-subtitle)', margin: 0 }}>为 iWorker 扩展能力，同时保留最近任务的复用入口。</p>
        </div>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
          <input
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="搜索工具"
            spellCheck={false}
            style={{
              width: '200px',
              padding: '8px 10px',
              borderRadius: '8px',
              border: '1px solid var(--dw-tools-input-border)',
              background: 'var(--dw-tools-input-bg)',
              color: 'var(--dw-tools-input-text)',
              fontSize: '12px',
              outline: 'none',
            }}
          />
          <button
            type="button"
            style={{
              padding: '8px 14px',
              borderRadius: '8px',
              border: '1px solid var(--dw-tools-input-border)',
              background: 'var(--dw-tools-input-bg)',
              color: 'var(--dw-tools-button-text)',
              fontSize: '12px',
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            添加工具
          </button>
        </div>
      </div>

      <section>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '12px' }}>
          <span style={{ fontSize: '13px', fontWeight: 700, color: 'var(--dw-tools-title)' }}>已安装</span>
          <span style={{ padding: '1px 8px', borderRadius: '10px', background: 'var(--dw-tools-badge-bg)', color: 'var(--dw-tools-badge-text)', fontSize: '11px', fontWeight: 600 }}>{installedTools.length}</span>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '8px' }}>
          {installedTools.map((tool) => (
            <div key={tool.id} style={{ display: 'flex', gap: '10px', padding: '12px', borderRadius: '10px', border: '1px solid var(--dw-tools-card-border)', background: 'var(--dw-tools-card-bg)' }}>
              <div style={{ width: '36px', height: '36px', borderRadius: '8px', background: '#eef2ff', color: '#3730a3', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '12px', fontWeight: 700, flexShrink: 0 }}>{tool.icon}</div>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                  <span style={{ fontSize: '13px', fontWeight: 600, color: 'var(--dw-tools-title)' }}>{tool.name}</span>
                  {tool.badge ? <span style={{ padding: '1px 6px', borderRadius: '4px', background: 'var(--dw-tools-official-badge-bg)', color: 'var(--dw-tools-official-badge-text)', fontSize: '10px', fontWeight: 600 }}>{tool.badge}</span> : null}
                </div>
                <p style={{ fontSize: '11px', color: 'var(--dw-tools-muted-text)', margin: '4px 0 0', lineHeight: '1.5' }}>{tool.description}</p>
              </div>
            </div>
          ))}
        </div>
      </section>

      <section>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '12px', gap: '12px', flexWrap: 'wrap' }}>
          <div style={{ display: 'flex', gap: '0', borderBottom: '2px solid var(--dw-tools-tab-border)' }}>
            {catalogTabs.map((tab) => (
              <button
                key={tab}
                type="button"
                onClick={() => setActiveTab(tab)}
                style={{
                  padding: '6px 14px',
                  border: 'none',
                  background: 'transparent',
                  fontSize: '13px',
                  fontWeight: activeTab === tab ? 700 : 500,
                  color: activeTab === tab ? 'var(--dw-tools-tab-active)' : 'var(--dw-tools-tab-idle)',
                  borderBottom: activeTab === tab ? '2px solid var(--dw-tools-tab-active)' : '2px solid transparent',
                  marginBottom: '-2px',
                  cursor: 'pointer',
                }}
              >
                {tab}
              </button>
            ))}
          </div>
          <button
            type="button"
            style={{
              padding: '6px 12px',
              borderRadius: '8px',
              border: '1px solid var(--dw-tools-input-border)',
              background: 'var(--dw-tools-input-bg)',
              color: 'var(--dw-tools-button-text)',
              fontSize: '12px',
              fontWeight: 600,
              cursor: 'pointer',
            }}
          >
            MCP 服务
          </button>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '6px' }}>
          {filteredCatalog.map((tool) => (
            <div key={tool.id} style={{ display: 'flex', gap: '10px', padding: '10px 12px', borderRadius: '8px' }}>
              <div style={{ width: '32px', height: '32px', borderRadius: '8px', background: '#f3f4f6', color: '#111827', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '12px', fontWeight: 700, flexShrink: 0 }}>{tool.icon}</div>
              <div style={{ flex: 1, minWidth: 0 }}>
                <span style={{ fontSize: '13px', fontWeight: 600, color: 'var(--dw-tools-title)' }}>{tool.name}</span>
                <p style={{ fontSize: '11px', color: 'var(--dw-tools-muted-text)', margin: '2px 0 0', lineHeight: '1.5' }}>{tool.description}</p>
              </div>
            </div>
          ))}
        </div>
      </section>

      <section>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '12px' }}>
          <h3 style={{ margin: 0, fontSize: '14px', color: 'var(--dw-tools-title)' }}>最近任务</h3>
          <span style={{ fontSize: '12px', color: 'var(--dw-tools-subtitle)' }}>{tasks.length} 条记录</span>
        </div>
        <div style={{ display: 'grid', gap: '8px' }}>
          {tasks.map((task) => (
            <div key={task.id} style={{ padding: '12px', borderRadius: '10px', border: '1px solid var(--dw-tools-card-border)', background: viewedTask?.id === task.id ? 'var(--dw-tools-row-hover)' : 'var(--dw-tools-card-bg)' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: '12px', alignItems: 'flex-start' }}>
                <div>
                  <div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--dw-tools-title)' }}>{task.title}</div>
                  <div style={{ fontSize: '11px', color: 'var(--dw-tools-muted-text)', marginTop: '4px' }}>{task.owner} · {task.updatedAt}</div>
                </div>
                <span style={{ fontSize: '11px', color: 'var(--dw-tools-muted-text)' }}>{task.status}</span>
              </div>
              <p style={{ fontSize: '12px', color: 'var(--dw-tools-subtitle)', margin: '8px 0 0', lineHeight: '1.5' }}>{task.description}</p>
              <div style={{ display: 'flex', gap: '8px', marginTop: '10px', flexWrap: 'wrap' }}>
                <button type="button" onClick={() => onViewResult(task)} className="secondary">查看结果</button>
                <button type="button" onClick={() => onResumeTask(task)} className="secondary">继续编辑</button>
                <button type="button" onClick={() => onCloneTask(task)} className="secondary">复制任务</button>
                <button type="button" onClick={() => onDeleteTask(task)} className="secondary">删除</button>
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
