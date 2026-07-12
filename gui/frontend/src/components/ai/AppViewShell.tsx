import { useEffect, useMemo, useState } from "react";
import type React from "react";
import type { AgentView, AppViewAction, AppViewNavItem } from "./agentViewTypes";
import type { Theme } from "./aiAssistantPanelTheme";
import { AgentTaskPanel } from "./AgentTaskPanel";
import { agentViewStrings } from "./agentViewI18n";

type AppView = Extract<AgentView, { type: "app_view" }>;

interface AppViewShellProps {
    view: AppView;
    onDismiss?: (viewId: string | undefined, data?: Record<string, unknown>) => void | Promise<void>;
    onResizeStart?: () => void;
    onToggleMaximize?: () => void;
    onSubmit?: (viewId: string | undefined, data: Record<string, unknown>) => void | Promise<void>;
    theme: Theme;
    lang?: string;
}

function asViewList(raw: AgentView | AgentView[] | undefined): AgentView[] {
    if (!raw) return [];
    return Array.isArray(raw) ? raw.filter(Boolean) : [raw];
}

function pickMainView(mains: AgentView[], nav: AppViewNavItem | undefined, index: number): AgentView | null {
    if (mains.length === 0) return null;
    if (nav?.targetViewId) {
        const hit = mains.find((item) => item && "id" in item && item.id === nav.targetViewId);
        if (hit) return hit;
    }
    const safe = Math.max(0, Math.min(index, mains.length - 1));
    return mains[safe] || mains[0];
}

function revisionFromAppView(view: AppView): number | undefined {
    if (typeof view.viewRevision === "number" && view.viewRevision > 0) return Math.trunc(view.viewRevision);
    const metaRev = view.meta?.viewRevision;
    if (typeof metaRev === "number" && metaRev > 0) return Math.trunc(metaRev);
    return undefined;
}

function schemaFromAppView(view: AppView): string | undefined {
    const meta = view.meta?.schemaVersion;
    if (typeof meta === "string" && meta.trim()) return meta.trim();
    return undefined;
}

/**
 * AppView workspace chrome: nav + main/side regions.
 * Nested region content reuses AgentTaskPanel (non-app_view types only).
 */
export function AppViewShell({
    view,
    onDismiss,
    onResizeStart,
    onToggleMaximize,
    onSubmit,
    theme,
    lang,
}: AppViewShellProps) {
    const s = useMemo(() => agentViewStrings(lang || "en"), [lang]);
    const mains = useMemo(() => asViewList(view.regions?.main), [view.regions?.main]);
    const sides = useMemo(() => asViewList(view.regions?.side), [view.regions?.side]);
    const navItems = useMemo(() => (view.regions?.nav || []).filter((item) => item && item.id && item.label), [view.regions?.nav]);
    const [activeNavId, setActiveNavId] = useState<string>(() => navItems[0]?.id || "");

    useEffect(() => {
        if (navItems.length === 0) {
            setActiveNavId("");
            return;
        }
        if (!navItems.some((item) => item.id === activeNavId)) {
            setActiveNavId(navItems[0].id);
        }
    }, [activeNavId, navItems, view.id]);

    const activeNav = navItems.find((item) => item.id === activeNavId) || navItems[0];
    const activeIndex = Math.max(0, navItems.findIndex((item) => item.id === activeNavId));
    const mainView = pickMainView(mains, activeNav, activeIndex > 0 ? activeIndex : 0);
    const sideView = sides[0] || null;

    const wrapSubmit = (innerId: string | undefined, data: Record<string, unknown>) => {
        if (!onSubmit) return;
        const rev = revisionFromAppView(view);
        const schema = schemaFromAppView(view);
        const payload: Record<string, unknown> = {
            ...data,
            _app_id: view.appId,
            _session_id: view.sessionId || "desktop-user",
            _inner_view_id: innerId || "",
            _region: "main",
        };
        if (rev != null) payload._agent_view_revision = rev;
        if (schema) payload._agent_view_schema_version = schema;
        // Submit against the workspace id so revision registry matches emit.
        void onSubmit(view.id, payload);
    };

    const wrapDismiss = (innerId: string | undefined, data?: Record<string, unknown>) => {
        if (!onDismiss) return;
        void onDismiss(view.id, {
            ...(data || {}),
            _app_id: view.appId,
            _session_id: view.sessionId || "desktop-user",
            _inner_view_id: innerId || "",
        });
    };

    const panelStyle: React.CSSProperties = {
        display: "flex",
        flexDirection: "column",
        height: "100%",
        minHeight: 0,
        background: theme.bg,
        borderLeft: `1px solid ${theme.divider}`,
        position: "relative",
    };
    const buttonStyle: React.CSSProperties = {
        borderRadius: 8,
        border: `1px solid ${theme.divider}`,
        background: theme.fieldBg,
        color: theme.text,
        padding: "6px 12px",
        fontSize: 12,
        cursor: "pointer",
    };
    const primaryButtonStyle: React.CSSProperties = {
        ...buttonStyle,
        background: theme.sendBtnBg,
        color: theme.sendBtnColor,
        border: `1px solid ${theme.sendBtnBorder}`,
        fontWeight: 600,
    };

    const footerActions: AppViewAction[] = (view.actions || view.regions?.footer?.actions || []) as AppViewAction[];

    return (
        <section style={panelStyle} data-testid="app-view-shell" data-app-id={view.appId}>
            <div
                role="separator"
                aria-orientation="vertical"
                onMouseDown={onResizeStart}
                style={{ width: 6, cursor: "col-resize", position: "absolute", height: "100%", zIndex: 2 }}
            />
            <header
                data-testid="app-view-header"
                onDoubleClick={(event) => {
                    if (event.target instanceof HTMLElement && event.target.closest("button")) return;
                    onToggleMaximize?.();
                }}
                style={{
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "space-between",
                    gap: 12,
                    padding: "12px 14px",
                    borderBottom: `1px solid ${theme.divider}`,
                    background: theme.titleBarBg,
                }}
            >
                <div style={{ minWidth: 0 }}>
                    <div style={{ color: theme.titleText, fontWeight: 700, fontSize: 14, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                        {view.title}
                    </div>
                    <div style={{ color: theme.textMuted, fontSize: 11, marginTop: 4, display: "flex", gap: 8, flexWrap: "wrap" }}>
                        <span>app:{view.appId}</span>
                        {view.layout ? <span>{view.layout}</span> : null}
                        {revisionFromAppView(view) != null ? <span>rev:{revisionFromAppView(view)}</span> : null}
                    </div>
                    {view.description && (
                        <div style={{ color: theme.textMuted, fontSize: 12, marginTop: 6, lineHeight: 1.45 }}>{view.description}</div>
                    )}
                </div>
                {onDismiss && (
                    <button type="button" onClick={() => wrapDismiss(view.id, {})} style={{ ...buttonStyle, color: theme.closeBtnColor }}>
                        {s.close}
                    </button>
                )}
            </header>

            {navItems.length > 0 && (
                <nav
                    data-testid="app-view-nav"
                    style={{
                        display: "flex",
                        gap: 6,
                        padding: "8px 12px",
                        borderBottom: `1px solid ${theme.divider}`,
                        overflowX: "auto",
                        background: theme.bg,
                    }}
                >
                    {navItems.map((item) => {
                        const active = item.id === (activeNav?.id || "");
                        return (
                            <button
                                key={item.id}
                                type="button"
                                data-testid={`app-view-nav-${item.id}`}
                                onClick={() => setActiveNavId(item.id)}
                                style={{
                                    ...buttonStyle,
                                    borderColor: active ? theme.sendBtnBorder : theme.divider,
                                    background: active ? theme.sendBtnBg : theme.fieldBg,
                                    color: active ? theme.sendBtnColor : theme.text,
                                    fontWeight: active ? 600 : 500,
                                    whiteSpace: "nowrap",
                                }}
                            >
                                {item.label}
                            </button>
                        );
                    })}
                </nav>
            )}

            <div style={{ flex: 1, minHeight: 0, display: "flex", overflow: "hidden" }}>
                <div style={{ flex: sideView ? 1.4 : 1, minWidth: 0, minHeight: 0, display: "flex", flexDirection: "column" }}>
                    {mainView ? (
                        <div style={{ flex: 1, minHeight: 0 }} data-testid="app-view-main">
                            <AgentTaskPanel
                                view={mainView}
                                theme={theme}
                                lang={lang}
                                onSubmit={wrapSubmit}
                                onDismiss={onDismiss ? wrapDismiss : undefined}
                            />
                        </div>
                    ) : (
                        <div style={{ padding: 16, color: theme.textMuted, fontSize: 12 }}>No main region</div>
                    )}
                </div>
                {sideView && (
                    <div
                        data-testid="app-view-side"
                        style={{
                            flex: 0.9,
                            minWidth: 220,
                            maxWidth: 360,
                            minHeight: 0,
                            borderLeft: `1px solid ${theme.divider}`,
                            display: "flex",
                            flexDirection: "column",
                        }}
                    >
                        <AgentTaskPanel view={sideView} theme={theme} lang={lang} onSubmit={wrapSubmit} />
                    </div>
                )}
            </div>

            {footerActions.length > 0 && (
                <footer
                    data-testid="app-view-footer"
                    style={{
                        display: "flex",
                        justifyContent: "flex-end",
                        gap: 8,
                        padding: "10px 14px",
                        borderTop: `1px solid ${theme.divider}`,
                        background: theme.titleBarBg,
                    }}
                >
                    {footerActions.map((action, index) => {
                        const innerId =
                            action.viewId ||
                            (mainView && "id" in mainView ? (mainView as { id?: string }).id : undefined);
                        return (
                            <button
                                key={action.id || `${action.label}-${index}`}
                                type="button"
                                style={action.primary ? primaryButtonStyle : buttonStyle}
                                onClick={() => wrapSubmit(innerId, action.data || {})}
                            >
                                {action.label}
                            </button>
                        );
                    })}
                </footer>
            )}
        </section>
    );
}
