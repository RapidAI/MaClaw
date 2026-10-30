import { AppsRailIcon, ExpertRailIcon, GossipIcon, ProFeaturesIcon, SettingsIcon } from './SidebarNavIcons';
import { requestRailMiddleFocus, useRailMiddleFocus } from './railMiddleFocus';
import { localizeText, normalizeLang } from '../../i18n';
import { miniAppShortLabel } from '../../i18n/maclawMiniAppLabels';
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
export type NavMenuKey = 'system' | 'extensions' | 'library' | 'pro' | 'connect';

export type NavMenuToggleState = {
    systemMenuOpen: boolean;
    extensionsMenuOpen: boolean;
    libraryMenuOpen: boolean;
    proMenuOpen: boolean;
    connectMenuOpen: boolean;
    systemMenuOpenerRef: MutableRefObject<HTMLElement | null>;
    extensionsMenuOpenerRef: MutableRefObject<HTMLElement | null>;
    libraryMenuOpenerRef: MutableRefObject<HTMLElement | null>;
    proMenuOpenerRef: MutableRefObject<HTMLElement | null>;
    connectMenuOpenerRef: MutableRefObject<HTMLElement | null>;
    setSystemMenuOpen: Dispatch<SetStateAction<boolean>>;
    setExtensionsMenuOpen: Dispatch<SetStateAction<boolean>>;
    setLibraryMenuOpen: Dispatch<SetStateAction<boolean>>;
    setProMenuOpen: Dispatch<SetStateAction<boolean>>;
    setConnectMenuOpen: Dispatch<SetStateAction<boolean>>;
    setExtensionsMenuTop: Dispatch<SetStateAction<number>>;
    setLibraryMenuTop: Dispatch<SetStateAction<number>>;
    setProMenuTop: Dispatch<SetStateAction<number>>;
    setConnectMenuTop: Dispatch<SetStateAction<number>>;
};

export function useNavMenuToggles(state: NavMenuToggleState) {
    const {
        systemMenuOpen, extensionsMenuOpen, libraryMenuOpen, proMenuOpen, connectMenuOpen,
        systemMenuOpenerRef, extensionsMenuOpenerRef, libraryMenuOpenerRef, proMenuOpenerRef, connectMenuOpenerRef,
        setSystemMenuOpen, setExtensionsMenuOpen, setLibraryMenuOpen, setProMenuOpen, setConnectMenuOpen,
        setExtensionsMenuTop, setLibraryMenuTop, setProMenuTop, setConnectMenuTop,
    } = state;

    // keep omitted closes every menu — used by scroll-to-dismiss.
    const closeSiblingMenus = (keep?: NavMenuKey) => {
        if (keep !== 'system') setSystemMenuOpen(false);
        if (keep !== 'extensions') setExtensionsMenuOpen(false);
        if (keep !== 'library') setLibraryMenuOpen(false);
        if (keep !== 'pro') setProMenuOpen(false);
        if (keep !== 'connect') setConnectMenuOpen(false);
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
            // Viewport coordinates: the popup is position fixed, so the anchor
            // must survive rail scrolling (offsetTop is layout-relative and
            // would point at the pre-scroll position).
            const rect = target.getBoundingClientRect();
            setExtensionsMenuTop(rect.top + rect.height / 2);
            closeSiblingMenus('extensions');
        }
        setExtensionsMenuOpen(prev => !prev);
    };
    const toggleLibraryMenu = (target: HTMLElement) => {
        if (!libraryMenuOpen) {
            libraryMenuOpenerRef.current = target;
            const rect = target.getBoundingClientRect();
            setLibraryMenuTop(rect.top + rect.height / 2);
            closeSiblingMenus('library');
        }
        setLibraryMenuOpen(prev => !prev);
    };
    const toggleProMenu = (target: HTMLElement) => {
        if (!proMenuOpen) {
            proMenuOpenerRef.current = target;
            const rect = target.getBoundingClientRect();
            setProMenuTop(rect.top + rect.height / 2);
            closeSiblingMenus('pro');
        }
        setProMenuOpen(prev => !prev);
    };
    const toggleConnectMenu = (target: HTMLElement) => {
        if (!connectMenuOpen) {
            connectMenuOpenerRef.current = target;
            const rect = target.getBoundingClientRect();
            setConnectMenuTop(rect.top + rect.height / 2);
            closeSiblingMenus('connect');
        }
        setConnectMenuOpen(prev => !prev);
    };

    return { closeSiblingMenus, toggleSystemMenu, toggleExtensionsMenu, toggleLibraryMenu, toggleProMenu, toggleConnectMenu };
}

export type ProFeatureTab = 'utilities' | 'workflows' | 'tools';

/** Destinations under 专业功能, in menu order. One list so the rail button and the popup cannot disagree. */
export function visibleProFeatureTabs(flags: {
    showUtilitiesEntry: boolean;
    showWorkflowEntry: boolean;
    showToolsEntry: boolean;
}): ProFeatureTab[] {
    const tabs: ProFeatureTab[] = [];
    if (flags.showUtilitiesEntry) tabs.push('utilities');
    if (flags.showWorkflowEntry) tabs.push('workflows');
    if (flags.showToolsEntry) tabs.push('tools');
    return tabs;
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
    /** Split navigation: Tools joins AI experts and workflows under 专业功能. */
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
    /** Live running-task total, rendered as a badge on the Tasks entry. */
    runningTaskCount?: number;
    /** Prefers the background-task view when the Tasks entry is activated. */
    onOpenBackgroundTasks?: () => void;
    /** Popup state for the 专业功能 rail entry (AI experts, workflows, tools). */
    proMenuOpen?: boolean;
    onToggleProMenu?: (target: HTMLElement) => void;
    /** Rail label for the professional-features entry. Falls back to language inference. */
    proFeaturesLabel?: string;
    /** Workflow catalog stays reachable from the professional-features menu. */
    showWorkflowEntry?: boolean;
    connectLabel?: string;
    connectTitle?: string;
    connectMenuOpen?: boolean;
    onToggleConnectMenu?: (target: HTMLElement) => void;
    /** Solo IM, mailbox or hardware, opened from 连接. */
    connectSoloActive?: boolean;
    /** Display language. Cyrillic samples used to fall through to Chinese. */
    lang?: string;
    /** Current settings tab, used to mark IM, 邮箱 or 硬件 inside the connect menu. */
    settingsTab?: string;
};

const railItemLabelStyle = { fontSize: '0.72rem', lineHeight: 1.15, fontWeight: 700, textAlign: 'center', width: '100%' } as const;

const sharedHeaderStyle = { justifyContent: 'flex-start', width: '100%', flexDirection: 'column' } as const;
const maclawHeaderStyle = { ...sharedHeaderStyle, height: '56px', padding: '0', gap: '0' } as const;
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

const ConnectRailIcon = () => (
    <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.7" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
        <path d="M8 12h8" /><path d="M9 8.5 5.5 12 9 15.5" /><path d="M15 8.5 18.5 12 15 15.5" /><circle cx="12" cy="12" r="9" />
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

export const SidebarPrimaryNav = ({ lang, navTab, aiAssistantLabel, appsLabel, showAppEntry, showUtilitiesEntry = true, showToolsEntry = false, switchTool, extensionsLabel, extensionsMenuOpen, onToggleExtensionsMenu, libraryMenuOpen = false, onToggleLibraryMenu, knowledgeActive = false, latexTemplatesActive = false, workflowLabel, utilitiesLabel, utilitiesTitle, runningTaskCount = 0, onOpenBackgroundTasks, proMenuOpen = false, onToggleProMenu, proFeaturesLabel: proFeaturesLabelProp, showWorkflowEntry = true, connectLabel, connectTitle, connectMenuOpen = false, onToggleConnectMenu, connectSoloActive = false }: SidebarPrimaryNavProps) => {
    // Callers that omit lang still have to be told apart. Cyrillic is its own
    // case: treating it as "not English" used to print the Simplified labels.
    const languageSample = `${aiAssistantLabel} ${appsLabel} ${utilitiesLabel} ${workflowLabel || ''}`;
    const inferred = /^[\x00-\x7F\s&]+$/.test(languageSample)
        ? 'en'
        : /[\u0400-\u04FF]/.test(languageSample)
            ? 'mn'
            // 程/置 are shared by Simplified and Traditional (for example 小程序).
            : /[專體務設]/.test(languageSample) ? 'zh-Hant' : 'zh-Hans';
    const resolved = lang && String(lang).trim() ? normalizeLang(lang) : inferred;
    const text = (en: string, zhHans: string, zhHant: string) => localizeText(resolved, en, zhHans, zhHant);
    const labels = {
        workbench: text('Workbench', '工作台', '工作台'),
        tasks: text('Tasks', '任务', '任務'),
        apps: resolved === 'mn' ? miniAppShortLabel('mn') : text('Mini apps', '小程序', '小程式'),
        experts: text('AI experts', 'AI 专家', 'AI 專家'),
        employees: text('Digital employees', '数字员工', '數字員工'),
        files: text('Library', '资料库', '資料庫'),
        settings: text('Settings', '设置', '設定'),
    };

    // Keep the old combined entry available to isolated embedders/tests that
    // do not opt into the split navigation. The packaged app replaces the
    // direct AI 专家 rail item with a 专业功能 menu (AI experts, workflows, tools).
    // A single remaining destination opens directly, so disabling entries
    // does not leave a one-item popup.
    const proFeaturesLabel = proFeaturesLabelProp || text('Features', '专业功能', '專業功能');
    const useProMenu = showToolsEntry && !!onToggleProMenu;
    const proTargets = visibleProFeatureTabs({
        showUtilitiesEntry,
        showWorkflowEntry,
        showToolsEntry,
    });
    const proMenuAvailable = useProMenu && proTargets.length > 1;
    const proDirectTarget = useProMenu && proTargets.length === 1 ? proTargets[0] : '';
    const expertLabel = useProMenu ? proFeaturesLabel : (showToolsEntry ? labels.experts : (utilitiesLabel || labels.experts));
    const expertTitle = useProMenu ? proFeaturesLabel : (showToolsEntry ? (utilitiesTitle || labels.experts) : (utilitiesTitle || utilitiesLabel || labels.experts));
    const proPageActive = (proTargets as readonly string[]).includes(navTab);
    // Workbench and digital employees share the assistant page. The highlight
    // follows which middle pane is actually showing, not merely navTab === 'ai'.
    const middleFocus = useRailMiddleFocus();
    const assistantSurface = navTab === 'ai';
    const employeesActive = assistantSurface && middleFocus === 'employees';
    const workbenchActive = assistantSurface && !employeesActive;

    return (
        <>
            <SemanticNavItem
                id="workbench"
                label={labels.workbench}
                legacyLabel={aiAssistantLabel}
                icon={<div className="ai-nav-icon-badge" aria-hidden="true"><HomeRailIcon /></div>}
                active={workbenchActive}
                onClick={() => { switchTool('ai'); requestRailMiddleFocus('tasks'); }}
                title={aiAssistantLabel}
                aiStyle
            />
            <div aria-hidden="true" className="snrp-divider" />
            <SemanticNavItem id="tasks" label={labels.tasks} icon={<TaskRailIcon />} active={navTab === 'remote'} onClick={() => { if (onOpenBackgroundTasks) onOpenBackgroundTasks(); else switchTool('remote'); }} title={text('Task monitor', '任务监控', '任務監控')} testId="sidebar-task-monitor-nav" badgeCount={runningTaskCount} badgeLabel={resolved === 'zh-Hant' ? `：${Math.max(0, Math.trunc(Number(runningTaskCount) || 0))} 個執行中` : resolved === 'zh-Hans' ? `：${Math.max(0, Math.trunc(Number(runningTaskCount) || 0))} 个执行中` : resolved === 'mn' ? `: ${Math.max(0, Math.trunc(Number(runningTaskCount) || 0))} ажиллаж байна` : `: ${Math.max(0, Math.trunc(Number(runningTaskCount) || 0))} running`} />
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
            <SemanticNavItem id="employees" label={labels.employees} icon={<GossipIcon />} active={employeesActive} onClick={() => { switchTool('ai'); requestRailMiddleFocus('employees'); }} title={labels.employees} testId="sidebar-digital-employees-nav" />
            <SemanticNavItem id="extensions" label={extensionsLabel} icon={<ExtensionsRailIcon />} active={extensionsMenuOpen || navTab === 'skills' || navTab === 'mcp'} current={navTab === 'skills' || navTab === 'mcp'} onClick={event => { if (onToggleExtensionsMenu) onToggleExtensionsMenu(event.currentTarget); }} title={extensionsLabel} testId="sidebar-extensions-nav" menuTrigger={{ expanded: extensionsMenuOpen, controls: 'extensions-popup-menu' }} />
            <SemanticNavItem id="files" label={labels.files} icon={<FolderRailIcon />} active={libraryMenuOpen || navTab === 'files' || knowledgeActive || latexTemplatesActive} current={navTab === 'files' || knowledgeActive || latexTemplatesActive} onClick={event => { if (onToggleLibraryMenu) onToggleLibraryMenu(event.currentTarget); }} title={labels.files} testId="sidebar-files-nav" menuTrigger={{ expanded: libraryMenuOpen, controls: 'library-popup-menu' }} />
            <SemanticNavItem id="connect" label={connectLabel || text('Connect', '连接', '連接')} icon={<ConnectRailIcon />} active={connectMenuOpen || connectSoloActive} current={connectSoloActive} onClick={event => { if (onToggleConnectMenu) onToggleConnectMenu(event.currentTarget); }} title={connectTitle || connectLabel || text('Connect chat, email and hardware', '连接即时通讯、邮箱和硬件', '連接即時通訊、郵箱和硬體')} testId="sidebar-connect-nav" menuTrigger={{ expanded: connectMenuOpen, controls: 'connect-popup-menu' }} />
            <SemanticNavItem id="settings" label={labels.settings} icon={<SettingsIcon />} active={navTab === 'settings' && !knowledgeActive && !connectSoloActive} onClick={() => switchTool('settings')} title={labels.settings} testId="sidebar-settings-nav" />
        </>
    );
};
