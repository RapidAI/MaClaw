import { useState } from 'react';
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
  iconBg: string;
  iconColor: string;
  description: string;
  badge?: string;
};

const installedTools: ToolItem[] = [
  { id: 'capability-evolver', name: 'capability-evolver', icon: '⚙️', iconBg: '#f3f4f6', iconColor: '#374151', description: 'API key for remote memory graph service.' },
  { id: 'openclaw-assets', name: 'openclaw-assets-to-diworker', icon: 'O', iconBg: '#dcfce7', iconColor: '#166534', description: '将 OpenClaw 用户的个人资产迁移到 DiWorker 对应位置。' },
  { id: 'libtv-skill', name: 'libtv-skill', icon: 'L', iconBg: '#fce7f3', iconColor: '#be185d', description: '通过 liblib.tv 的 AI 能力生成和编辑图片/视频。' },
  { id: 'pptx', name: 'pptx', icon: 'P', iconBg: '#e9d5ff', iconColor: '#7c3aed', description: 'PowerPoint 演示文稿创建、编辑和分析技能。', badge: '官方' },
];

const catalogTabs = ['推荐', 'SkillHub', '套件'] as const;

const catalogTools: ToolItem[] = [
  { id: 'tencent-doc', name: '腾讯文档', icon: '📄', iconBg: '#dbeafe', iconColor: '#2563eb', description: '腾讯文档操作（创建、查询、编辑多种在线文档）' },
  { id: 'tencent-meeting', name: '腾讯会议', icon: '🎥', iconBg: '#dcfce7', iconColor: '#16a34a', description: '腾讯会议管理（预约、录制、转写、纪要）' },
  { id: 'tencent-ima', name: '腾讯ima', icon: '📝', iconBg: '#fef3c7', iconColor: '#d97706', description: 'ima笔记与知识库管理（读取、写入、检索）' },
  { id: 'neodata', name: 'NeoData金融搜索服务', icon: 'N', iconBg: '#e0e7ff', iconColor: '#4f46e5', description: '全球多市场金融数据搜索服务，自然语言查询股票、基金等。' },
  { id: 'cnb-cool', name: 'cnb.cool', icon: '🔧', iconBg: '#fce7f3', iconColor: '#be185d', description: 'CNB 平台全功能操作（仓库、Issue、PR、流水线）' },
  { id: 'brand-design', name: '品牌设计风格专家', icon: '🎨', iconBg: '#fef3c7', iconColor: '#d97706', description: '54 个知名网站设计系统模板，一键复用品牌级 UI 风格' },
];

export function TaskHistoryPage({ tasks, onResumeTask, onViewResult, onCloneTask, onDeleteTask, viewedTask }: Props) {
  const [activeTab, setActiveTab] = useState<typeof catalogTabs[number]>('推荐');
  const [searchQuery, setSearchQuery] = useState('');

  const filteredCatalog = searchQuery
    ? catalogTools.filter((t) => t.name.includes(searchQuery) || t.description.includes(searchQuery))
    : catalogTools;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: '16px', flexShrink: 0 }}>
        <div>
          <h2 style={{ fontSize: '18px', fontWeight: 700, color: '#111827', margin: '0 0 4px' }}>工具</h2>
          <p style={{ fontSize: '13px', color: '#9ca3af', margin: 0 }}>赋予 DiWorker 更强大的能力</p>
        </div>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
          {/* Search */}
          <div style={{
            display: 'flex', alignItems: 'center', gap: '6px',
            padding: '6px 10px', borderRadius: '8px',
            border: '1px solid #e5e7eb', background: '#ffffff',
            width: '180px',
          }}>
            <span style={{ color: '#9ca3af', fontSize: '13px' }}>🔍</span>
            <input
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="搜索技能"
              spellCheck={false}
              style={{ border: 'none', background: 'transparent', outline: 'none', fontSize: '12px', color: '#1f2937', width: '100%', padding: 0 }}
            />
          </div>
          {/* Add button */}
          <button type="button" style={{
            display: 'flex', alignItems: 'center', gap: '4px',
            padding: '6px 14px', borderRadius: '8px', border: '1px solid #e5e7eb',
            background: '#ffffff', color: '#374151', fontSize: '12px', fontWeight: 600, cursor: 'pointer',
          }}>
            + 添加技能
          </button>
        </div>
      </div>

      {/* Scrollable content */}
      <div style={{ flex: 1, overflow: 'auto', paddingRight: '4px' }}>
        {/* Installed section */}
        <div style={{ marginBottom: '24px' }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '12px' }}>
            <span style={{ fontSize: '13px', fontWeight: 700, color: '#111827' }}>已安装</span>
            <span style={{
              padding: '1px 8px', borderRadius: '10px',
              background: '#dcfce7', color: '#166534',
              fontSize: '11px', fontWeight: 600,
            }}>{installedTools.length}</span>
            <span style={{ marginLeft: 'auto', color: '#9ca3af', fontSize: '14px', cursor: 'pointer' }}>⋯</span>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '8px' }}>
            {installedTools.map((tool) => (
              <div key={tool.id} style={{
                display: 'flex', gap: '10px', padding: '12px',
                borderRadius: '10px', border: '1px solid #e5e7eb', background: '#ffffff',
                cursor: 'pointer', transition: 'border-color 0.15s',
              }}
                onMouseEnter={(e) => { e.currentTarget.style.borderColor = '#d1d5db'; }}
                onMouseLeave={(e) => { e.currentTarget.style.borderColor = '#e5e7eb'; }}
              >
                <div style={{
                  width: '36px', height: '36px', borderRadius: '8px',
                  background: tool.iconBg, color: tool.iconColor,
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  fontSize: '14px', fontWeight: 700, flexShrink: 0,
                }}>
                  {tool.icon}
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                    <span style={{ fontSize: '13px', fontWeight: 600, color: '#111827' }}>{tool.name}</span>
                    {tool.badge && (
                      <span style={{
                        padding: '1px 6px', borderRadius: '4px',
                        background: '#dbeafe', color: '#2563eb',
                        fontSize: '10px', fontWeight: 600,
                      }}>{tool.badge}</span>
                    )}
                  </div>
                  <p style={{
                    fontSize: '11px', color: '#6b7280', margin: '2px 0 0',
                    lineHeight: '1.4', overflow: 'hidden', textOverflow: 'ellipsis',
                    display: '-webkit-box', WebkitLineClamp: 2, WebkitBoxOrient: 'vertical' as const,
                  }}>{tool.description}</p>
                </div>
              </div>
            ))}
          </div>

          <button type="button" style={{
            display: 'block', margin: '8px auto 0', padding: '4px 12px',
            border: 'none', background: 'transparent',
            color: '#6b7280', fontSize: '12px', cursor: 'pointer',
          }}>
            显示更多
          </button>
        </div>

        {/* Catalog section */}
        <div>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '12px' }}>
            {/* Tabs */}
            <div style={{ display: 'flex', gap: '0', borderBottom: '2px solid #f3f4f6' }}>
              {catalogTabs.map((tab) => (
                <button
                  key={tab}
                  type="button"
                  onClick={() => setActiveTab(tab)}
                  style={{
                    padding: '6px 14px', border: 'none', background: 'transparent',
                    fontSize: '13px', fontWeight: activeTab === tab ? 700 : 500,
                    color: activeTab === tab ? '#111827' : '#6b7280',
                    borderBottom: activeTab === tab ? '2px solid #111827' : '2px solid transparent',
                    marginBottom: '-2px', cursor: 'pointer',
                  }}
                >
                  {tab}
                </button>
              ))}
            </div>
            {/* MCP button */}
            <button type="button" style={{
              display: 'flex', alignItems: 'center', gap: '4px',
              padding: '5px 12px', borderRadius: '8px', border: '1px solid #e5e7eb',
              background: '#ffffff', color: '#374151', fontSize: '12px', fontWeight: 600, cursor: 'pointer',
            }}>
              🖥️ MCP 服务器
            </button>
          </div>

          {/* Tool list */}
          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '6px' }}>
            {filteredCatalog.map((tool) => (
              <div key={tool.id} style={{
                display: 'flex', gap: '10px', padding: '10px 12px',
                borderRadius: '8px', cursor: 'pointer',
                transition: 'background 0.12s',
              }}
                onMouseEnter={(e) => { e.currentTarget.style.background = '#f9fafb'; }}
                onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent'; }}
              >
                <div style={{
                  width: '32px', height: '32px', borderRadius: '8px',
                  background: tool.iconBg, color: tool.iconColor,
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  fontSize: '13px', fontWeight: 700, flexShrink: 0,
                }}>
                  {tool.icon}
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <span style={{ fontSize: '13px', fontWeight: 600, color: '#111827' }}>{tool.name}</span>
                  <p style={{
                    fontSize: '11px', color: '#6b7280', margin: '2px 0 0',
                    lineHeight: '1.4', overflow: 'hidden', textOverflow: 'ellipsis',
                    display: '-webkit-box', WebkitLineClamp: 1, WebkitBoxOrient: 'vertical' as const,
                  }}>{tool.description}</p>
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  );
}
