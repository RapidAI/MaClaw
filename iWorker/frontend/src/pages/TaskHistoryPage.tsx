import { useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import type { CenterInstalledTools, HistoryTaskItem } from '../types';

type Props = {
  tasks: HistoryTaskItem[];
  installedTools: CenterInstalledTools;
  installedToolsLoading: boolean;
  installedToolsError: string;
  onRefreshInstalledTools: () => void | Promise<CenterInstalledTools | undefined>;
  onResumeTask: (task: HistoryTaskItem) => void;
  onViewResult: (task: HistoryTaskItem) => void;
  onCloneTask: (task: HistoryTaskItem) => void;
  onDeleteTask: (task: HistoryTaskItem) => void;
  viewedTask: HistoryTaskItem | null;
};

type ToolItem = { id: string; nameKey: string; fallbackName: string; icon: string; descKey: string; fallbackDesc: string; badgeKey?: string; fallbackBadge?: string };

const fallbackInstalledTools: ToolItem[] = [
  { id: 'capability-evolver', nameKey: 'history.tools.capability.name', fallbackName: 'capability-evolver', icon: 'CE', descKey: 'history.tools.capability.desc', fallbackDesc: 'Evolves organizational capability maps from reusable work evidence.' },
  { id: 'openclaw-assets', nameKey: 'history.tools.assets.name', fallbackName: 'openclaw-assets-to-iworker', icon: 'OA', descKey: 'history.tools.assets.desc', fallbackDesc: 'Migrates personal OpenClaw assets into the matching iWorker directories.' },
  { id: 'libtv-skill', nameKey: 'history.tools.libtv.name', fallbackName: 'libtv-skill', icon: 'LT', descKey: 'history.tools.libtv.desc', fallbackDesc: 'Connects image and video generation capabilities for content production.' },
  { id: 'pptx', nameKey: 'history.tools.pptx.name', fallbackName: 'pptx', icon: 'P', descKey: 'history.tools.pptx.desc', fallbackDesc: 'Creates, edits, and analyzes presentation documents.', badgeKey: 'history.official', fallbackBadge: 'Official' },
];
const catalogTabs = ['recommended', 'skillhub', 'suite'] as const;
const catalogTools: ToolItem[] = [
  { id: 'tencent-doc', nameKey: 'history.catalog.tencentDoc.name', fallbackName: 'Tencent Docs', icon: 'TD', descKey: 'history.catalog.tencentDoc.desc', fallbackDesc: 'Create, read, and collaborate on document content.' },
  { id: 'tencent-meeting', nameKey: 'history.catalog.tencentMeeting.name', fallbackName: 'Tencent Meeting', icon: 'TM', descKey: 'history.catalog.tencentMeeting.desc', fallbackDesc: 'Schedule meetings, manage recordings, and organize minutes.' },
  { id: 'tencent-ima', nameKey: 'history.catalog.tencentIma.name', fallbackName: 'Tencent ima', icon: 'TI', descKey: 'history.catalog.tencentIma.desc', fallbackDesc: 'Connect knowledge base and search capabilities.' },
  { id: 'neodata', nameKey: 'history.catalog.neodata.name', fallbackName: 'NeoData', icon: 'ND', descKey: 'history.catalog.neodata.desc', fallbackDesc: 'Financial search and data insight service.' },
  { id: 'cnb-cool', nameKey: 'history.catalog.cnb.name', fallbackName: 'cnb.cool', icon: 'CN', descKey: 'history.catalog.cnb.desc', fallbackDesc: 'Connects development collaboration platform capabilities.' },
  { id: 'brand-design', nameKey: 'history.catalog.brand.name', fallbackName: 'Brand design expert', icon: 'BD', descKey: 'history.catalog.brand.desc', fallbackDesc: 'Reuses brand visual assets to generate design proposals quickly.' },
];

export function TaskHistoryPage({ tasks, installedTools, installedToolsLoading, installedToolsError, onRefreshInstalledTools, onResumeTask, onViewResult, onCloneTask, onDeleteTask, viewedTask }: Props) {
  const { t } = useTranslation();
  const [activeTab, setActiveTab] = useState<typeof catalogTabs[number]>('recommended');
  const [searchQuery, setSearchQuery] = useState('');
  const localizedCatalog = useMemo(() => catalogTools.map((tool) => ({ ...tool, name: t(tool.nameKey, tool.fallbackName), description: t(tool.descKey, tool.fallbackDesc) })), [t]);
  const centerToolRows = useMemo(() => [
    ...installedTools.skills.map((skill) => ({
      id: `skill-${skill.capabilityId}`,
      kind: 'Skill',
      name: skill.name || skill.capabilityId,
      description: skill.entry?.description || (skill.entry?.triggers || []).join(', ') || t('history.centerSkillDesc', 'Center-distributed iWorker skill.'),
      badge: `${skill.source || 'iWorkerCenter'} / ${skill.version || '-'} / ${skill.riskLevel || 'low'}`,
      icon: 'SK',
    })),
    ...installedTools.mcpServers.map((server) => ({
      id: `mcp-${server.id}`,
      kind: 'MCP',
      name: server.name || server.id,
      description: server.description || server.endpoint || server.command || t('history.centerMcpDesc', 'Center-installed MCP service.'),
      badge: `${server.status || 'enabled'} / ${server.departmentId || 'all'} / ${server.riskLevel || 'medium'}`,
      icon: 'MC',
    })),
  ], [installedTools.mcpServers, installedTools.skills, t]);
  const installedSourceLabel = installedTools.source === 'center' ? (installedTools.stale ? t('history.centerStale', 'Center stale') : t('history.centerLive', 'Center live')) : installedTools.source === 'cache' || installedTools.stale ? t('history.cachedSnapshot', 'Cached snapshot') : installedTools.source === 'unavailable' ? t('history.unavailable', 'Unavailable') : t('history.localFallback', 'Local fallback');
  const filteredCatalog = useMemo(() => {
    if (!searchQuery) return localizedCatalog;
    const query = searchQuery.toLowerCase();
    return localizedCatalog.filter((tool) => tool.name.toLowerCase().includes(query) || tool.description.toLowerCase().includes(query));
  }, [localizedCatalog, searchQuery]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: '20px', height: '100%', overflow: 'auto' }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: '16px', flexWrap: 'wrap' }}>
        <div>
          <h2 style={{ fontSize: '18px', fontWeight: 700, color: 'var(--dw-tools-title)', margin: '0 0 4px' }}>{t('history.title', 'Tools and tasks')}</h2>
          <p style={{ fontSize: '13px', color: 'var(--dw-tools-subtitle)', margin: 0 }}>{t('history.subtitle', 'Extend iWorker capability while keeping reusable entry points for recent work.')}</p>
        </div>
        <div style={{ display: 'flex', gap: '8px', alignItems: 'center' }}>
          <input value={searchQuery} onChange={(e) => setSearchQuery(e.target.value)} placeholder={t('history.searchTools', 'Search tools')} spellCheck={false} style={{ width: '200px', padding: '8px 10px', borderRadius: '8px', border: '1px solid var(--dw-tools-input-border)', background: 'var(--dw-tools-input-bg)', color: 'var(--dw-tools-input-text)', fontSize: '12px', outline: 'none' }} />
          <button type="button" onClick={() => { void onRefreshInstalledTools(); }} disabled={installedToolsLoading} style={{ padding: '8px 14px', borderRadius: '8px', border: '1px solid var(--dw-tools-input-border)', background: 'var(--dw-tools-input-bg)', color: 'var(--dw-tools-button-text)', fontSize: '12px', fontWeight: 600, cursor: installedToolsLoading ? 'not-allowed' : 'pointer' }}>{installedToolsLoading ? t('history.syncing', 'Syncing') : t('history.refreshCenterTools', 'Refresh Center tools')}</button>
        </div>
      </div>
      <section>
        <div style={{ display: 'flex', alignItems: 'center', gap: '8px', marginBottom: '12px' }}>
          <span style={{ fontSize: '13px', fontWeight: 700, color: 'var(--dw-tools-title)' }}>{t('history.installed', 'Installed')}</span>
          <span style={{ padding: '1px 8px', borderRadius: '10px', background: 'var(--dw-tools-badge-bg)', color: 'var(--dw-tools-badge-text)', fontSize: '11px', fontWeight: 600 }}>{centerToolRows.length}</span>
          <span style={{ fontSize: '12px', color: 'var(--dw-tools-muted-text)' }}>{installedSourceLabel}{installedTools.cachedAt ? ` / ${installedTools.cachedAt}` : ''}</span>
        </div>
        {installedToolsError ? <p style={{ fontSize: '12px', color: '#b91c1c', margin: '0 0 10px' }}>{installedToolsError}</p> : null}
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '8px' }}>
          {(centerToolRows.length > 0 ? centerToolRows : fallbackInstalledTools.map((tool) => ({ id: tool.id, kind: t('history.localFallback', 'Local fallback'), name: t(tool.nameKey, tool.fallbackName), description: t(tool.descKey, tool.fallbackDesc), badge: tool.badgeKey ? t(tool.badgeKey, tool.fallbackBadge || '') : t('history.displayOnly', 'display only'), icon: tool.icon }))).map((tool) => (
            <div key={tool.id} style={{ display: 'flex', gap: '10px', padding: '12px', borderRadius: '10px', border: '1px solid var(--dw-tools-card-border)', background: 'var(--dw-tools-card-bg)' }}>
              <div style={{ width: '36px', height: '36px', borderRadius: '8px', background: tool.kind === 'MCP' ? '#ecfdf5' : '#eef2ff', color: tool.kind === 'MCP' ? '#166534' : '#3730a3', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '12px', fontWeight: 700, flexShrink: 0 }}>{tool.icon}</div>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div style={{ display: 'flex', alignItems: 'center', gap: '6px', flexWrap: 'wrap' }}>
                  <span style={{ fontSize: '13px', fontWeight: 600, color: 'var(--dw-tools-title)' }}>{tool.name}</span>
                  <span style={{ padding: '1px 6px', borderRadius: '4px', background: 'var(--dw-tools-official-badge-bg)', color: 'var(--dw-tools-official-badge-text)', fontSize: '10px', fontWeight: 600 }}>{tool.kind}</span>
                </div>
                <p style={{ fontSize: '11px', color: 'var(--dw-tools-muted-text)', margin: '4px 0 0', lineHeight: '1.5' }}>{tool.description}</p>
                <p style={{ fontSize: '10px', color: 'var(--dw-tools-muted-text)', margin: '4px 0 0' }}>{tool.badge}</p>
              </div>
            </div>
          ))}
        </div>
      </section>
      <section>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '12px', gap: '12px', flexWrap: 'wrap' }}>
          <div style={{ display: 'flex', gap: '0', borderBottom: '2px solid var(--dw-tools-tab-border)' }}>
            {catalogTabs.map((tab) => (
              <button key={tab} type="button" onClick={() => setActiveTab(tab)} style={{ padding: '6px 14px', border: 'none', background: 'transparent', fontSize: '13px', fontWeight: activeTab === tab ? 700 : 500, color: activeTab === tab ? 'var(--dw-tools-tab-active)' : 'var(--dw-tools-tab-idle)', borderBottom: activeTab === tab ? '2px solid var(--dw-tools-tab-active)' : '2px solid transparent', marginBottom: '-2px', cursor: 'pointer' }}>{t(`history.tabs.${tab}`)}</button>
            ))}
          </div>
          <button type="button" style={{ padding: '6px 12px', borderRadius: '8px', border: '1px solid var(--dw-tools-input-border)', background: 'var(--dw-tools-input-bg)', color: 'var(--dw-tools-button-text)', fontSize: '12px', fontWeight: 600, cursor: 'pointer' }}>{t('history.mcpServices', 'MCP services')}</button>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(2, minmax(0, 1fr))', gap: '6px' }}>
          {filteredCatalog.map((tool) => (
            <div key={tool.id} style={{ display: 'flex', gap: '10px', padding: '10px 12px', borderRadius: '8px' }}>
              <div style={{ width: '32px', height: '32px', borderRadius: '8px', background: '#f3f4f6', color: '#111827', display: 'flex', alignItems: 'center', justifyContent: 'center', fontSize: '12px', fontWeight: 700, flexShrink: 0 }}>{tool.icon}</div>
              <div style={{ flex: 1, minWidth: 0 }}><span style={{ fontSize: '13px', fontWeight: 600, color: 'var(--dw-tools-title)' }}>{tool.name}</span><p style={{ fontSize: '11px', color: 'var(--dw-tools-muted-text)', margin: '2px 0 0', lineHeight: '1.5' }}>{tool.description}</p></div>
            </div>
          ))}
        </div>
      </section>
      <section>
        <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '12px' }}>
          <h3 style={{ margin: 0, fontSize: '14px', color: 'var(--dw-tools-title)' }}>{t('history.recentTasks', 'Recent tasks')}</h3>
          <span style={{ fontSize: '12px', color: 'var(--dw-tools-subtitle)' }}>{t('history.recordCount', { count: tasks.length, defaultValue: '{{count}} records' })}</span>
        </div>
        <div style={{ display: 'grid', gap: '8px' }}>
          {tasks.map((task) => (
            <div key={task.id} style={{ padding: '12px', borderRadius: '10px', border: '1px solid var(--dw-tools-card-border)', background: viewedTask?.id === task.id ? 'var(--dw-tools-row-hover)' : 'var(--dw-tools-card-bg)' }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: '12px', alignItems: 'flex-start' }}><div><div style={{ fontSize: '13px', fontWeight: 600, color: 'var(--dw-tools-title)' }}>{task.title}</div><div style={{ fontSize: '11px', color: 'var(--dw-tools-muted-text)', marginTop: '4px' }}>{task.owner} / {task.updatedAt}</div></div><span style={{ fontSize: '11px', color: 'var(--dw-tools-muted-text)' }}>{task.status}</span></div>
              <p style={{ fontSize: '12px', color: 'var(--dw-tools-subtitle)', margin: '8px 0 0', lineHeight: '1.5' }}>{task.description}</p>
              <div style={{ display: 'flex', gap: '8px', marginTop: '10px', flexWrap: 'wrap' }}>
                <button type="button" onClick={() => onViewResult(task)} className="secondary">{t('history.viewResult', 'View result')}</button>
                <button type="button" onClick={() => onResumeTask(task)} className="secondary">{t('history.continueEditing', 'Continue editing')}</button>
                <button type="button" onClick={() => onCloneTask(task)} className="secondary">{t('history.cloneTask', 'Clone task')}</button>
                <button type="button" onClick={() => onDeleteTask(task)} className="secondary">{t('history.delete', 'Delete')}</button>
              </div>
            </div>
          ))}
        </div>
      </section>
    </div>
  );
}
