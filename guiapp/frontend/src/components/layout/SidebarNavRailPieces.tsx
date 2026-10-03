import { AppsRailIcon, ExpertRailIcon, GossipIcon, ProFeaturesIcon, SettingsIcon, ToolsRailIcon } from './SidebarNavIcons';
import type { Dispatch, MutableRefObject, ReactNode, SetStateAction } from 'react';

/**
 * Sibling-menu arbitration for the nav rail's four popups.
 *
 * Only one popup may be open at a time, and each remembers its trigger element
 * (so focus can be returned on close) plus its anchor offset. That is four
 * pieces of state times four menus — a shape worth one shared implementation
 * rather than four near-identical closures in the rail component, which is
 * under a hard line-count budget.
 */
export type NavMenuKey = 'system' | 'extensions' | 'library' | 'pro';

export type NavMenuToggleState = {
    systemMenuOpen: boolean;
    extensionsMenuOpen: boolean;
    libraryMenuOpen: boolean;
    proMenuOpen: boolean;
    systemMenuOpenerRef: MutableRefObject<HTMLElement | null>;
    extensionsMenuOpenerRef: MutableRefObject<HTMLElement | null>;
    libraryMenuOpenerRef: MutableRefObject<HTMLElement | null>;
    proMenuOpenerRef: MutableRefObject<HTMLElement | null>;
    setSystemMenuOpen: Dispatch<SetStateAction<boolean>>;
    setExtensionsMenuOpen: Dispatch<SetStateAction<boolean>>;
    setLibraryMenuOpen: Dispatch<SetStateAction<boolean>>;
    setProMenuOpen: Dispatch<SetStateAction<boolean>>;
    setExtensionsMenuTop: Dispatch<SetStateAction<number>>;
    setLibraryMenuTop: Dispatch<SetStateAction<number>>;
    setProMenuTop: Dispatch<SetStateAction<number>>;
};

export function useNavMenuToggles(state: NavMenuToggleState) {
    const {
        systemMenuOpen, extensionsMenuOpen, libraryMenuOpen, proMenuOpen,
        systemMenuOpenerRef, extensionsMenuOpenerRef, libraryMenuOpenerRef, proMenuOpenerRef,
        setSystemMenuOpen, setExtensionsMenuOpen, setLibraryMenuOpen, setProMenuOpen,
        setExtensionsMenuTop, setLibraryMenuTop, setProMenuTop,
    } = state;

    const closeSiblingMenus = (keep: NavMenuKey) => {
        if (keep !== 'system') setSystemMenuOpen(false);
        if (keep !== 'extensions') setExtensionsMenuOpen(false);
        if (keep !== 'library') setLibraryMenuOpen(false);
        if (keep !== 'pro') setProMenuOpen(false);
    };

    // Each toggle only re-anchors when the menu is opening; toggling closed must
    // not move the popup to the trigger's current offset, which would make it
    // jump on the way out.
    const toggleSystemMenu = (target: HTMLElement) => {
        if (!systemMenuOpen) {
            systemMenuOpenerRef.current = target;
            closeSiblingMenus('system');
        }
        setSystemMenuOpen(prev => !prev);
    };
    const toggleExtensionsMenu = (target: HTMLElement) => {
        if (!extensionsMenuOpen) {
            extensionsMenuOpenerRef.current = target;
            setExtensionsMenuTop(target.offsetTop + target.offsetHeight / 2);
            closeSiblingMenus('extensions');
        }
        setExtensionsMenuOpen(prev => !prev);
    };
    const toggleLibraryMenu = (target: HTMLElement) => {
        if (!libraryMenuOpen) {
            libraryMenuOpenerRef.current = target;
            setLibraryMenuTop(target.offsetTop + target.offsetHeight / 2);
            closeSiblingMenus('library');
        }
        setLibraryMenuOpen(prev => !prev);
    };
    const toggleProMenu = (target: HTMLElement) => {
        if (!proMenuOpen) {
            proMenuOpenerRef.current = target;
            setProMenuTop(target.offsetTop + target.offsetHeight / 2);
            closeSiblingMenus('pro');
        }
        setProMenuOpen(prev => !prev);
    };

    return { closeSiblingMenus, toggleSystemMenu, toggleExtensionsMenu, toggleLibraryMenu, toggleProMenu };
}

type SidebarBrandHeaderProps = {
    brandId?: string;
    currentIcon: string;
    brandSidebarName: string;
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
    extensionsLabel: string;
    extensionsMenuOpen: boolean;
    onToggleExtensionsMenu?: (target: HTMLElement) => void;
    libraryMenuOpen?: boolean;
    onToggleLibraryMenu?: (target: HTMLElement) => void;
    /** True while the settings page shows the Knowledge tab opened from the library menu. */
    knowledgeActive?: boolean;
    /** True while the LaTeX template library page is open. */
    latexTemplatesActive?: boolean;
    workflowLabel?: string;
    utilitiesLabel: string;
    utilitiesTitle?: string;
    toolsLabel?: string;
    toolsTitle?: string;
    /** Live running-task total, rendered as a badge on the Tasks entry. */
    runningTaskCount?: number;
    /** Prefers the background-task view when the Tasks entry is activated. */
    onOpenBackgroundTasks?: () => void;
    /** Popup state for the 专业功能 rail entry (AI experts + workflows). */
    proMenuOpen?: boolean;
    onToggleProMenu?: (target: HTMLElement) => void;
    /** Rail label for the professional-features entry. Falls back to language inference. */
    proFeaturesLabel?: string;
    /** Workflow catalog stays reachable from the professional-features menu. */
    showWorkflowEntry?: boolean;
};

const railItemLabelStyle = { fontSize: '0.72rem', lineHeight: 1.15, fontWeight: 700, textAlign: 'center', width: '100%' } as const;

const sharedHeaderStyle = { justifyContent: 'flex-start', width: '100%', flexDirection: 'column' } as const;
const maclawHeaderStyle = { ...sharedHeaderStyle, height: '64px', padding: '0 0 2px 0', gap: '0' } as const;
const tigerClawHeaderStyle = { ...sharedHeaderStyle, padding: '2px 0 0 0', gap: '2px' } as const;

export const SidebarBrandHeader = ({ brandId, currentIcon, brandSidebarName }: SidebarBrandHeaderProps) => {
    const isTigerClaw = brandId === 'qianxin';
    return (
        <div className={`sidebar-header ${isTigerClaw ? 'sidebar-header--tiger' : 'sidebar-header--maclaw'}`} style={isTigerClaw ? tigerClawHeaderStyle : maclawHeaderStyle}>
            {isTigerClaw ? (
                <img src={currentIcon} alt="Logo" className="sidebar-logo snrp-brand-logo" />
            ) : (
                <span className="mc-sidebar-brand-mark mc-sidebar-brand-mark--compact" aria-label="MaClaw">
                    <svg viewBox="0 0 80 80" focusable="false" aria-hidden="true">
                        <path d="M14 58V22l26 25 26-25v36" fill="none" stroke="currentColor" strokeWidth="10.5" strokeLinecap="round" strokeLinejoin="round" />
                    </svg>
                </span>
            )}
            {isTigerClaw && <div className="snrp-brand-name">{brandSidebarName}</div>}
        </div>
    );
};

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
    /** Positive count rendered as a corner badge; hidden when 0 or absent. */
    badgeCount?: number;
    /** Appended to the accessible name so the badge is not visual-only. */
    badgeLabel?: string;
};

const SemanticNavItem = ({ id, label, legacyLabel, icon, active, current, onClick, title, testId, aiStyle = false, visible = true, menuTrigger, badgeCount, badgeLabel }: SemanticNavItemProps) => {
    const badgeTotal = Math.max(0, Math.trunc(Number(badgeCount) || 0));
    const showBadge = badgeTotal > 0;
    const resolvedTitle = showBadge && badgeLabel ? `${title || label}${badgeLabel}` : (title || label);
    const resolvedAriaLabel = showBadge && badgeLabel ? `${label}${badgeLabel}` : label;
    return (
        <button
            type="button"
            data-reference-nav={id}
            data-testid={testId}
            className={'sidebar-item left-nav-item' + (aiStyle ? ' left-nav-item--ai' : '') + (active ? ' active' : '')}
            onClick={onClick}
            title={resolvedTitle}
            aria-label={resolvedAriaLabel}
            aria-current={(current ?? active) ? 'page' : undefined}
            {...(menuTrigger ? { 'aria-haspopup': 'menu' as const, 'aria-expanded': menuTrigger.expanded, 'aria-controls': menuTrigger.controls } : {})}
            style={aiStyle ? (visible ? undefined : { display: 'none' }) : { flexDirection: 'column', padding: '5px 0', width: '100%', gap: '4px', border: 'none', justifyContent: 'center', position: 'relative', ...(visible ? {} : { display: 'none' }) }}
        >
            <span className="sidebar-icon" style={{ margin: 0, display: 'inline-flex', color: active ? 'var(--theme-primary-strong)' : 'var(--theme-text-primary)', position: 'relative' }}>
                {icon}
                {showBadge && (
                    <span className="left-nav-item__badge" data-testid={testId ? `${testId}-badge` : undefined} aria-hidden="true">
                        {badgeTotal > 99 ? '99+' : badgeTotal}
                    </span>
                )}
            </span>
            <span className={aiStyle ? 'ai-nav-label' : undefined} style={aiStyle ? undefined : railItemLabelStyle}>{label}</span>
            {legacyLabel && legacyLabel !== label && <span aria-hidden="true" className="snrp-legacy-label">{legacyLabel}</span>}
        </button>
    );
};

export const SidebarPrimaryNav = ({ navTab, aiAssistantLabel, appsLabel, showAppEntry, showUtilitiesEntry = true, showToolsEntry = false, switchTool, extensionsLabel, extensionsMenuOpen, onToggleExtensionsMenu, libraryMenuOpen = false, onToggleLibraryMenu, knowledgeActive = false, latexTemplatesActive = false, workflowLabel, utilitiesLabel, utilitiesTitle, toolsLabel, toolsTitle, runningTaskCount = 0, onOpenBackgroundTasks, proMenuOpen = false, onToggleProMenu, proFeaturesLabel: proFeaturesLabelProp, showWorkflowEntry = true }: SidebarPrimaryNavProps) => {
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
    // do not opt into the split navigation. The packaged app replaces the
    // direct AI 专家 rail item with a 专业功能 menu (AI experts + workflows).
    // A single remaining destination opens directly, so disabling one entry
    // does not leave a one-item popup.
    const proFeaturesLabel = proFeaturesLabelProp || (isEnglish ? 'Features' : isTraditional ? '專業功能' : '专业功能');
    const useProMenu = showToolsEntry && !!onToggleProMenu;
    const proTargets = [
        showUtilitiesEntry ? 'utilities' : '',
        showWorkflowEntry ? 'workflows' : '',
    ].filter(Boolean);
    const proMenuAvailable = useProMenu && proTargets.length > 1;
    const proDirectTarget = useProMenu && proTargets.length === 1 ? proTargets[0] : '';
    const expertLabel = useProMenu ? proFeaturesLabel : (showToolsEntry ? labels.experts : (utilitiesLabel || labels.experts));
    const expertTitle = useProMenu ? proFeaturesLabel : (showToolsEntry ? (utilitiesTitle || labels.experts) : (utilitiesTitle || utilitiesLabel || labels.experts));
    const proPageActive = (showUtilitiesEntry && navTab === 'utilities') || (showWorkflowEntry && navTab === 'workflows');
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
            <div aria-hidden="true" className="snrp-divider" />
            <SemanticNavItem id="tasks" label={labels.tasks} icon={<TaskRailIcon />} active={navTab === 'remote'} onClick={() => { if (onOpenBackgroundTasks) onOpenBackgroundTasks(); else switchTool('remote'); }} title={isEnglish ? 'Task monitor' : isTraditional ? '任務監控' : '任务监控'} testId="sidebar-task-monitor-nav" badgeCount={runningTaskCount} badgeLabel={isEnglish ? `: ${Math.max(0, Math.trunc(Number(runningTaskCount) || 0))} running` : isTraditional ? `：${Math.max(0, Math.trunc(Number(runningTaskCount) || 0))} 個執行中` : `：${Math.max(0, Math.trunc(Number(runningTaskCount) || 0))} 个执行中`} />
            {showAppEntry && <SemanticNavItem id="apps" label={labels.apps} legacyLabel={appsLabel} icon={<AppsRailIcon />} active={navTab === 'apps'} onClick={() => switchTool('apps')} title={appsLabel} testId="sidebar-apps-nav" />}
            <SemanticNavItem
                id="experts"
                label={expertLabel}
                legacyLabel={useProMenu || showToolsEntry ? undefined : utilitiesLabel}
                icon={useProMenu ? <ProFeaturesIcon /> : <ExpertRailIcon />}
                active={useProMenu ? ((proMenuAvailable && proMenuOpen) || proPageActive) : navTab === 'utilities'}
                current={useProMenu ? proPageActive : undefined}
                onClick={event => {
                    if (proMenuAvailable && onToggleProMenu) onToggleProMenu(event.currentTarget);
                    else switchTool(proDirectTarget || 'utilities');
                }}
                title={expertTitle}
                testId="sidebar-utilities-nav"
                visible={useProMenu ? proTargets.length > 0 : showUtilitiesEntry}
                menuTrigger={proMenuAvailable ? { expanded: proMenuOpen, controls: 'pro-features-popup-menu' } : undefined}
            />
            <SemanticNavItem id="tools" label={toolLabel} icon={<ToolsRailIcon />} active={navTab === 'tools'} onClick={() => switchTool('tools')} title={toolsTitle || toolLabel} testId="sidebar-tools-nav" visible={showToolsEntry} />
            <SemanticNavItem id="employees" label={labels.employees} icon={<GossipIcon />} active={false} onClick={() => { switchTool('ai'); emitRailIntent('maclaw:focus-digital-employees'); }} title={labels.employees} testId="sidebar-digital-employees-nav" />
            <SemanticNavItem id="extensions" label={extensionsLabel} icon={<ExtensionsRailIcon />} active={extensionsMenuOpen || navTab === 'skills' || navTab === 'mcp'} current={navTab === 'skills' || navTab === 'mcp'} onClick={event => { if (onToggleExtensionsMenu) onToggleExtensionsMenu(event.currentTarget); }} title={extensionsLabel} testId="sidebar-extensions-nav" menuTrigger={{ expanded: extensionsMenuOpen, controls: 'extensions-popup-menu' }} />
            <SemanticNavItem id="files" label={labels.files} icon={<FolderRailIcon />} active={libraryMenuOpen || navTab === 'files' || knowledgeActive || latexTemplatesActive} current={navTab === 'files' || knowledgeActive || latexTemplatesActive} onClick={event => { if (onToggleLibraryMenu) onToggleLibraryMenu(event.currentTarget); }} title={labels.files} testId="sidebar-files-nav" menuTrigger={{ expanded: libraryMenuOpen, controls: 'library-popup-menu' }} />
            <SemanticNavItem id="settings" label={labels.settings} icon={<SettingsIcon />} active={navTab === 'settings' && !knowledgeActive} onClick={() => switchTool('settings')} title={labels.settings} testId="sidebar-settings-nav" />
        </>
    );
};
