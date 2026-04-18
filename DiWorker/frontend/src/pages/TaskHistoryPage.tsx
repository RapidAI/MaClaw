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
  iconBgDark?: string;
  iconColorDark?: string;
  description: string;
  badge?: string;
};

const installedTools: ToolItem[] = [
  { id: 'capability-evolver', name: 'capability-evolver', icon: '⚙️', iconBg: '#f3f4f6', iconColor: '#374151', iconBgDark: '#1f2937', iconColorDark: '#e5e7eb', description: 'API key for remote memory graph service.' },
  { id: 'openclaw-assets', name: 'openclaw-assets-to-diworker', icon: 'O', iconBg: '#dcfce7', iconColor: '#166534', iconBgDark: '#14532d', iconColorDark: '#bbf7d0', description: '将 OpenClaw 用户的个人资产迁移到 DiWorker 对应位置。' },
  { id: 'libtv-skill', name: 'libtv-skill', icon: 'L', iconBg: '#fce7f3', iconColor: '#be185d', iconBgDark: '#831843', iconColorDark: '#fbcfe8', description: '通过 liblib.tv 的 AI 能力生成和编辑图片/视频。' },
  { id: 'pptx', name: 'pptx', icon: 'P', iconBg: '#e9d5ff', iconColor: '#7c3aed', iconBgDark: '#581c87', iconColorDark: '#e9d5ff', description: 'PowerPoint 演示文稿创建、编辑和分析技能。', badge: '官方' },
];

const catalogTabs = ['推荐', 'SkillHub', '套件'] as const;

const catalogTools: ToolItem[] = [
  { id: 'tencent-doc', name: '腾讯文档', icon: '📄', iconBg: '#dbeafe', iconColor: '#2563eb', iconBgDark: '#1e3a8a', iconColorDark: '#bfdbfe', description: '腾讯文档操作（创建、查询、编辑多种在线文档）' },
  { id: 'tencent-meeting', name: '腾讯会议', icon: '🎥', iconBg: '#dcfce7', iconColor: '#16a34a', iconBgDark: '#14532d', iconColorDark: '#bbf7d0', description: '腾讯会议管理（预约、录制、转写、纪要）' },
  { id: 'tencent-ima', name: '腾讯ima', icon: '📝', iconBg: '#fef3c7', iconColor: '#d97706', iconBgDark: '#78350f', iconColorDark: '#fde68a', description: 'ima笔记与知识库管理（读取、写入、检索）' },
  { id: 'neodata', name: 'NeoData金融搜索服务', icon: 'N', iconBg: '#e0e7ff', iconColor: '#4f46e5', iconBgDark: '#312e81', iconColorDark: '#c7d2fe', description: '全球多市场金融数据搜索服务，自然语言查询股票、基金等。' },
  { id: 'cnb-cool', name: 'cnb.cool', icon: '🔧', iconBg: '#fce7f3', iconColor: '#be185d', iconBgDark: '#831843', iconColorDark: '#fbcfe8', description: 'CNB 平台全功能操作（仓库、Issue、PR、流水线）' },
  { id: 'brand-design', name: '品牌设计风格专家', icon: '🎨', iconBg: '#fef3c7', iconColor: '#d97706', iconBgDark: '#78350f', iconColorDark: '#fde68a', description: '54 个知名网站设计系统模板，一键复用品牌级 UI 风格' },
];

export function TaskHistoryPage({ tasks, onResumeTask, onViewResult, onCloneTask, onDeleteTask, viewedTask }: Props) {
  const [activeTab, setActiveTab] = useState<typeof catalogTabs[number]>('推荐');
  const [searchQuery, setSearchQuery] = useState('');

  const filteredCatalog = searchQuery
    ? catalogTools.filter((t) => t.name.includes(searchQuery) || t.description.includes(searchQuery))
    : catalogTools;

  const prefersDark = typeof window !== 'undefined' && window.matchMedia('(prefers-color-scheme: dark)').matches;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      {/* Header */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', marginBottom: '16px', flexShrink: 0 }}>
        <div>
          <h2 style={{ fontSize: '18px', fontWeight: 700, color: 'var(--dw-tools-title)', margin: '0 0 4px' }}>工具</h2>
          <p style={{ fontSize: '13px', color: 'var(--dw-tools-subtitle)', margin: 0 }}>赋予 DiWorker 更强大的能力</p>
        </div>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
          {/* Search */}
          <div style={{
            display: 'flex', alignItems: 'center', gap: '6px',
            padding: '6px 10px', borderRadius: '8px',
            border: '1px solid var(--dw-tools-input-border)', background: 'var(--dw-tools-input-bg)',
            width: '180px',
          }}>
            <span style={{ color: 'var(--dw-tools-subtitle)', fontSize: '13px' }}>🔍</span>
            <input
              value={searchQuery}
              onChange={(e) => setSearchQuery(e.target.value)}
              placeholder="搜索技能"
              spellCheck={false}
              style={{ border: 'none', background: 'transparent', outline: 'none', fontSize: '12px', color: 'var(--dw-tools-input-text)', width: '100%', padding: 0 }}
            />
          </div>
          {/* Add button */}
          <button type="button" style={{
            display: 'flex', alignItems: 'center', gap: '4px',
            padding: '6px 14px', borderRadius: '8px', border: '1px solid var(--dw-tools-input-border)',
            background: 'var(--dw-tools-input-bg)', color: 'var(--dw-tools-button-text)', fontSize: '12px', fontWeight: 600, cursor: 'pointer',
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
            <span style={{ fontSize: '13px', fontWeight: 700, color: 'var(--dw-tools-title)' }}>已安装</span>
            <span style={{
              padding: '1px 8px', borderRadius: '10px',
              background: 'var(--dw-tools-badge-bg)', color: 'var(--dw-tools-badge-text)',
              fontSize: '11px', fontWeight: 600,
            }}>{installedTools.length}</span>
            <span style={{ marginLeft: 'auto', color: 'var(--dw-tools-subtitle)', fontSize: '14px', cursor: 'pointer' }}>⋯</span>
          </div>

          <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '8px' }}>
            {installedTools.map((tool) => (
              <div key={tool.id} style={{
                display: 'flex', gap: '10px', padding: '12px',
                borderRadius: '10px', border: '1px solid var(--dw-tools-card-border)', background: 'var(--dw-tools-card-bg)',
                cursor: 'pointer', transition: 'border-color 0.15s',
              }}
                onMouseEnter={(e) => { e.currentTarget.style.borderColor = 'var(--dw-tools-card-border-hover)'; }}
                onMouseLeave={(e) => { e.currentTarget.style.borderColor = 'var(--dw-tools-card-border)'; }}
              >
                <div style={{
                  width: '36px', height: '36px', borderRadius: '8px',
                  background: prefersDark ? (tool.iconBgDark || tool.iconBg) : tool.iconBg,
                  color: prefersDark ? (tool.iconColorDark || tool.iconColor) : tool.iconColor,
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  fontSize: '14px', fontWeight: 700, flexShrink: 0,
                }}>
                  {tool.icon}
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <div style={{ display: 'flex', alignItems: 'center', gap: '6px' }}>
                    <span style={{ fontSize: '13px', fontWeight: 600, color: 'var(--dw-tools-title)' }}>{tool.name}</span>
                    {tool.badge && (
                      <span style={{
                        padding: '1px 6px', borderRadius: '4px',
                        background: 'var(--dw-tools-official-badge-bg)', color: 'var(--dw-tools-official-badge-text)',
                        fontSize: '10px', fontWeight: 600,
                      }}>{tool.badge}</span>
                    )}
                  </div>
                  <p style={{
                    fontSize: '11px', color: 'var(--dw-tools-muted-text)', margin: '2px 0 0',
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
            color: 'var(--dw-tools-muted-text)', fontSize: '12px', cursor: 'pointer',
          }}>
            显示更多
          </button>
        </div>

        {/* Catalog section */}
        <div>
          <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '12px' }}>
            {/* Tabs */}
            <div style={{ display: 'flex', gap: '0', borderBottom: '2px solid var(--dw-tools-tab-border)' }}>
              {catalogTabs.map((tab) => (
                <button
                  key={tab}
                  type="button"
                  onClick={() => setActiveTab(tab)}
                  style={{
                    padding: '6px 14px', border: 'none', background: 'transparent',
                    fontSize: '13px', fontWeight: activeTab === tab ? 700 : 500,
                    color: activeTab === tab ? 'var(--dw-tools-tab-active)' : 'var(--dw-tools-tab-idle)',
                    borderBottom: activeTab === tab ? '2px solid var(--dw-tools-tab-active)' : '2px solid transparent',
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
              padding: '5px 12px', borderRadius: '8px', border: '1px solid var(--dw-tools-input-border)',
              background: 'var(--dw-tools-input-bg)', color: 'var(--dw-tools-button-text)', fontSize: '12px', fontWeight: 600, cursor: 'pointer',
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
                onMouseEnter={(e) => { e.currentTarget.style.background = 'var(--dw-tools-row-hover)'; }}
                onMouseLeave={(e) => { e.currentTarget.style.background = 'transparent'; }}
              >
                <div style={{
                  width: '32px', height: '32px', borderRadius: '8px',
                  background: prefersDark ? (tool.iconBgDark || tool.iconBg) : tool.iconBg,
                  color: prefersDark ? (tool.iconColorDark || tool.iconColor) : tool.iconColor,
                  display: 'flex', alignItems: 'center', justifyContent: 'center',
                  fontSize: '13px', fontWeight: 700, flexShrink: 0,
                }}>
                  {tool.icon}
                </div>
                <div style={{ flex: 1, minWidth: 0 }}>
                  <span style={{ fontSize: '13px', fontWeight: 600, color: 'var(--dw-tools-title)' }}>{tool.name}</span>
                  <p style={{
                    fontSize: '11px', color: 'var(--dw-tools-muted-text)', margin: '2px 0 0',
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
