import { AppsRailIcon, ExpertRailIcon, GossipIcon, SettingsIcon, ToolsRailIcon } from './SidebarNavIcons';
import { IconRankBadge } from '../ai/WorkbenchIcons';
import type { ReactNode } from 'react';

type SidebarMedal = {
    rank: number;
    tokenRank: number;
    durationRank: number;
    totalUsers: number;
    rankChange?: number; // positive = up, negative = down, 0 or undefined = no change
    trophyThreshold: number; // hub-configured: ranks <= this use trophy icon, beyond use medal
};

type SidebarBrandHeaderProps = {
    brandId?: string;
    currentIcon: string;
    brandSidebarName: string;
};

type SidebarMedalBadgeProps = {
    medal: SidebarMedal;
    lang: string;
};

type SidebarLinkedMedalProps = {
    medal: SidebarMedal;
    lang: string;
    title: string;
    onClick: () => void;
};

type SidebarPrimaryNavProps = {
    navTab: string;
    aiAssistantLabel: string;
    appsLabel: string;
    showAppEntry: boolean;
    showUtilitiesEntry?: boolean;
    /** Split navigation mode: expose a dedicated Tools rail item. */
    showToolsEntry?: boolean;
    switchTool: (tool: string) => void;
    onOpenBackgroundTasks?: () => void;
    remoteSessionTab?: 'remote' | 'background' | 'scheduled' | 'passthrough';
    extensionsLabel: string;
    extensionsMenuOpen: boolean;
    onToggleExtensionsMenu?: (target: HTMLElement) => void;
    libraryMenuOpen?: boolean;
    onToggleLibraryMenu?: (target: HTMLElement) => void;
    /** True while the settings page shows the Knowledge tab opened from the library menu. */
    knowledgeActive?: boolean;
    workflowLabel?: string;
    utilitiesLabel: string;
    utilitiesTitle?: string;
    toolsLabel?: string;
    toolsTitle?: string;
};

const railItemLabelStyle = { fontSize: '0.72rem', lineHeight: 1.15, fontWeight: 700, textAlign: 'center', width: '100%' } as const;

const sharedHeaderStyle = { justifyContent: 'flex-start', width: '100%', flexDirection: 'column' } as const;
const maclawHeaderStyle = { ...sharedHeaderStyle, height: '64px', padding: '0 0 2px 0', gap: '0' } as const;
const tigerClawHeaderStyle = { ...sharedHeaderStyle, height: '56px', padding: '4px 0 2px 0', gap: '1px' } as const;

export const SidebarBrandHeader = ({ brandId, currentIcon, brandSidebarName }: SidebarBrandHeaderProps) => {
    const isTigerClaw = brandId === 'qianxin';
    return (
        <div className={`sidebar-header ${isTigerClaw ? 'sidebar-header--tiger' : 'sidebar-header--maclaw'}`} style={isTigerClaw ? tigerClawHeaderStyle : maclawHeaderStyle}>
            {isTigerClaw ? (
                <img src={currentIcon} alt="Logo" className="sidebar-logo" style={{ width: '30px', height: '30px', objectFit: 'contain' }} />
            ) : (
                <span className="mc-sidebar-brand-mark mc-sidebar-brand-mark--compact" aria-label="MaClaw">
                    <svg viewBox="0 0 80 80" focusable="false" aria-hidden="true">
                        <path d="M14 58V22l26 25 26-25v36" fill="none" stroke="currentColor" strokeWidth="10.5" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                </span>
            )}
            {isTigerClaw && <div style={{ color: 'var(--theme-primary-strong)', fontSize: '0.64rem', fontWeight: 800, lineHeight: 1, fontFamily: 'Georgia, serif' }}>{brandSidebarName}</div>}
        </div>
    );
};

export const SidebarMedalBadge = ({ medal, lang }: SidebarMedalBadgeProps) => {
    const rank = medal.rank;
    const rankChange = medal.rankChange || 0;

    const rankText = rank > 0 ? (lang === 'en' ? `#${rank}` : `第${rank}名`) : (lang === 'en' ? 'Rank' : '排行');

    return (
        <div style={{ display: 'flex', flexDirection: 'column', alignItems: 'center', width: '100%' }}>
            {/* Decorative divider line between "About" and ranking */}
            <div
                aria-hidden="true"
                style={{
                    width: '70%',
                    height: '1px',
                    margin: '3px 0 2px 0',
                    background: 'linear-gradient(90deg, transparent 0%, rgba(139,157,195,0.12) 15%, rgba(139,157,195,0.35) 50%, rgba(139,157,195,0.12) 85%, transparent 100%)',
                    boxShadow: '0 1px 1px rgba(0,0,0,0.4)',
                }}
            />

            <div
                className="sidebar-medal-badge"
                title={(() => {
                    const parts: string[] = [];
                    const totalText = medal.totalUsers > 0 ? String(medal.totalUsers) : '-';
                    const tokenRankText = medal.tokenRank > 0 ? String(medal.tokenRank) : '-';
                    const durationRankText = medal.durationRank > 0 ? String(medal.durationRank) : '-';
                    parts.push(lang === 'en' ? `Token #${tokenRankText}/${totalText}` : `Token 第${tokenRankText}/${totalText}名`);
                    parts.push(lang === 'en' ? `Online #${durationRankText}/${totalText}` : `在线 第${durationRankText}/${totalText}名`);
                    const prefix = lang === 'en' ? 'This month: ' : '本月排名: ';
                    return prefix + parts.join(', ');
                })()}
                style={{
                    display: 'flex',
                    flexDirection: 'column',
                    alignItems: 'center',
                    padding: '2px 0 5px 0',
                    width: '100%',
                    cursor: 'pointer',
                    userSelect: 'none',
                    minHeight: '40px',
                    justifyContent: 'center',
                }}
            >
                <span
                    className="sidebar-medal-icon"
                    style={{ lineHeight: 1, display: 'flex', alignItems: 'center' }}
                >
                    <IconRankBadge rank={rank} size={20} />
                </span>
                <div style={{ display: 'flex', alignItems: 'center', gap: '2px', marginTop: '2px' }}>
                    <span style={{ fontSize: '0.62rem', lineHeight: 1, color: 'var(--theme-text)', fontWeight: 700 }}>
                        {rankText}
                    </span>
                    {rankChange > 0 && (
                        <span style={{ fontSize: '0.55rem', fontWeight: 700, color: '#ef4444', lineHeight: 1 }}>↑{rankChange}</span>
                    )}
                    {rankChange < 0 && (
                        <span style={{ fontSize: '0.55rem', fontWeight: 700, color: '#22c55e', lineHeight: 1 }}>↓{Math.abs(rankChange)}</span>
                    )}
                </div>
            </div>
        </div>
    );
};

export const SidebarLinkedMedal = ({ medal, lang, title, onClick }: SidebarLinkedMedalProps) => (
    <div onClick={onClick} style={{ cursor: 'pointer', width: '100%' }} title={title}>
        <SidebarMedalBadge medal={medal} lang={lang} />
    </div>
);

const HomeRailIcon = () => (
    <svg className="ai-nav-icon" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="m3 10 9-7 9 7" /><path d="M5 9v11h14V9" /><path d="M9 20v-6h6v6" />
    </svg>
);

const TaskRailIcon = () => (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <rect x="4" y="3" width="16" height="18" rx="2" /><path d="m8 12 2.2 2.2L16 8.5" /><path d="M8 6.8h8" />
    </svg>
);

const ExtensionsRailIcon = () => (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <rect x="3.5" y="3.5" width="7" height="7" rx="1.5" /><rect x="13.5" y="3.5" width="7" height="7" rx="1.5" /><rect x="3.5" y="13.5" width="7" height="7" rx="1.5" /><path d="M17 13.5v7M13.5 17h7" />
    </svg>
);

const FolderRailIcon = () => (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M3 6.5a2 2 0 0 1 2-2h5l2 2h7a2 2 0 0 1 2 2v8.5a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z" />
    </svg>
);

const hiddenLegacyLabelStyle = {
    position: 'absolute',
    width: '1px',
    height: '1px',
    padding: 0,
    margin: '-1px',
    overflow: 'hidden',
    clip: 'rect(0, 0, 0, 0)',
    whiteSpace: 'nowrap',
    border: 0,
} as const;

type SemanticNavItemProps = {
    id: string;
    label: string;
    legacyLabel?: string;
    icon: ReactNode;
    active: boolean;
    /** aria-current target. Defaults to `active`; menu triggers separate the two. */
    current?: boolean;
    onClick: (event: React.MouseEvent<HTMLButtonElement>) => void;
    title?: string;
    testId?: string;
    aiStyle?: boolean;
    visible?: boolean;
    menuTrigger?: { expanded: boolean; controls: string };
};

const SemanticNavItem = ({ id, label, legacyLabel, icon, active, current, onClick, title, testId, aiStyle = false, visible = true, menuTrigger }: SemanticNavItemProps) => (
    <button
        type="button"
        data-reference-nav={id}
        data-testid={testId}
        className={'sidebar-item left-nav-item' + (aiStyle ? ' left-nav-item--ai' : '') + (active ? ' active' : '')}
        onClick={onClick}
        title={title || label}
        aria-label={label}
        aria-current={(current ?? active) ? 'page' : undefined}
        {...(menuTrigger ? { 'aria-haspopup': 'menu' as const, 'aria-expanded': menuTrigger.expanded, 'aria-controls': menuTrigger.controls } : {})}
        style={aiStyle ? (visible ? undefined : { display: 'none' }) : { flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', border: 'none', justifyContent: 'center', position: 'relative', ...(visible ? {} : { display: 'none' }) }}
    >
        <span className="sidebar-icon" style={{ margin: 0, display: 'inline-flex', color: active ? 'var(--theme-primary-strong)' : 'var(--theme-text-primary)' }}>{icon}</span>
        <span className={aiStyle ? 'ai-nav-label' : undefined} style={aiStyle ? undefined : railItemLabelStyle}>{label}</span>
        {legacyLabel && legacyLabel !== label && <span aria-hidden="true" style={hiddenLegacyLabelStyle}>{legacyLabel}</span>}
    </button>
);

export const SidebarPrimaryNav = ({ navTab, aiAssistantLabel, appsLabel, showAppEntry, showUtilitiesEntry = true, showToolsEntry = false, switchTool, onOpenBackgroundTasks, remoteSessionTab = 'remote', extensionsLabel, extensionsMenuOpen, onToggleExtensionsMenu, libraryMenuOpen = false, onToggleLibraryMenu, knowledgeActive = false, workflowLabel, utilitiesLabel, utilitiesTitle, toolsLabel, toolsTitle }: SidebarPrimaryNavProps) => {
    // The rail is also used by the English and Traditional-Chinese builds. The
    // existing localized labels are the only language signal available here,
    // so infer the display language without changing the parent component API.
    const languageSample = `${aiAssistantLabel} ${appsLabel} ${utilitiesLabel} ${workflowLabel || ''}`;
    const isEnglish = /^[\x00-\x7F\s&]+$/.test(languageSample);
    // 程/置 are shared by Simplified and Traditional Chinese (for example
    // 小程序), so use characters whose forms actually differ. Otherwise the
    // split rail would label a zh-Hans build as AI 專家.
    const isTraditional = !isEnglish && /[專體務設]/.test(languageSample);
    const labels = isEnglish
        ? { workbench: 'Workbench', tasks: 'Tasks', apps: 'Mini apps', experts: 'AI experts', tools: 'Tools', employees: 'Digital employees', files: 'Library', settings: 'Settings' }
        : isTraditional
            ? { workbench: '工作台', tasks: '任務', apps: '小程式', experts: 'AI 專家', tools: '工具', employees: '數字員工', files: '資料庫', settings: '設定' }
        : { workbench: '工作台', tasks: '任务', apps: '小程序', experts: 'AI 专家', tools: '工具', employees: '数字员工', files: '资料库', settings: '设置' };

    // Keep the old combined entry available to isolated embedders/tests that
    // do not opt into the split navigation, while the packaged app uses the
    // dedicated AI 专家 + 工具 entries.
    const expertLabel = showToolsEntry ? labels.experts : (utilitiesLabel || labels.experts);
    const expertTitle = showToolsEntry ? (utilitiesTitle || labels.experts) : (utilitiesTitle || utilitiesLabel || labels.experts);
    const toolLabel = toolsLabel || labels.tools || (isEnglish ? 'Tools' : '工具');

    const emitRailIntent = (name: string) => {
        if (typeof window !== 'undefined') window.dispatchEvent(new CustomEvent(name));
    };

    return (
        <>
            <SemanticNavItem
                id="workbench"
                label={labels.workbench}
                legacyLabel={aiAssistantLabel}
                icon={<div className="ai-nav-icon-badge" aria-hidden="true"><HomeRailIcon /></div>}
                active={navTab === 'ai'}
                onClick={() => switchTool('ai')}
                title={aiAssistantLabel}
                aiStyle
            />
            <div aria-hidden="true" style={{ width: '70%', height: '2px', margin: '4px 0 6px 0', borderRadius: '1px', background: 'linear-gradient(90deg, transparent 0%, var(--theme-border) 20%, var(--theme-text-muted) 50%, var(--theme-border) 80%, transparent 100%)', opacity: 0.5 }} />
            <SemanticNavItem id="tasks" label={labels.tasks} icon={<TaskRailIcon />} active={navTab === 'remote' && remoteSessionTab !== 'scheduled'} onClick={onOpenBackgroundTasks || (() => switchTool('remote'))} title={isEnglish ? 'Background task monitor' : isTraditional ? '後台任務監控' : '后台任务监控'} testId="sidebar-task-monitor-nav" />
            {showAppEntry && <SemanticNavItem id="apps" label={labels.apps} legacyLabel={appsLabel} icon={<AppsRailIcon />} active={navTab === 'apps'} onClick={() => switchTool('apps')} title={appsLabel} testId="sidebar-apps-nav" />}
            <SemanticNavItem id="experts" label={expertLabel} legacyLabel={showToolsEntry ? undefined : utilitiesLabel} icon={<ExpertRailIcon />} active={navTab === 'utilities'} onClick={() => switchTool('utilities')} title={expertTitle} testId="sidebar-utilities-nav" visible={showUtilitiesEntry} />
            <SemanticNavItem id="tools" label={toolLabel} icon={<ToolsRailIcon />} active={navTab === 'tools'} onClick={() => switchTool('tools')} title={toolsTitle || toolLabel} testId="sidebar-tools-nav" visible={showToolsEntry} />
            <SemanticNavItem id="employees" label={labels.employees} icon={<GossipIcon />} active={false} onClick={() => { switchTool('ai'); emitRailIntent('maclaw:focus-digital-employees'); }} title={labels.employees} testId="sidebar-digital-employees-nav" />
            <SemanticNavItem id="extensions" label={extensionsLabel} icon={<ExtensionsRailIcon />} active={extensionsMenuOpen || navTab === 'skills' || navTab === 'mcp'} current={navTab === 'skills' || navTab === 'mcp'} onClick={event => { if (onToggleExtensionsMenu) onToggleExtensionsMenu(event.currentTarget); }} title={extensionsLabel} testId="sidebar-extensions-nav" menuTrigger={{ expanded: extensionsMenuOpen, controls: 'extensions-popup-menu' }} />
            <SemanticNavItem id="files" label={labels.files} icon={<FolderRailIcon />} active={libraryMenuOpen || navTab === 'files' || knowledgeActive} current={navTab === 'files' || knowledgeActive} onClick={event => { if (onToggleLibraryMenu) onToggleLibraryMenu(event.currentTarget); }} title={labels.files} testId="sidebar-files-nav" menuTrigger={{ expanded: libraryMenuOpen, controls: 'library-popup-menu' }} />
            <SemanticNavItem id="settings" label={labels.settings} icon={<SettingsIcon />} active={navTab === 'settings' && !knowledgeActive} onClick={() => switchTool('settings')} title={labels.settings} testId="sidebar-settings-nav" />
        </>
    );
};
