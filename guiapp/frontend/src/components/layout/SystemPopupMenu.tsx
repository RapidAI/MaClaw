import { useEffect, useRef } from 'react';
import { SIDEBAR_NAV_RAIL_WIDTH } from './sidebarLayout';

export interface SystemMenuItem {
    id: string;
    icon: React.ReactNode;
    label: string;
    visible: boolean;
    badge?: number;
}

interface SystemPopupMenuProps {
    items: SystemMenuItem[];
    onSelect: (id: string) => void;
    onClose: () => void;
    returnFocus?: () => HTMLElement | null;
    ariaLabel?: string;
    /** When set, vertically center the menu at this offset (px) instead of the rail bottom. */
    anchorTop?: number;
    /** Extra CSS selector whose clicks must not count as outside clicks (the opening trigger). */
    excludeTriggerSelector?: string;
    menuId?: string;
    testIdPrefix?: string;
    /** Menu item that matches the page already open. */
    activeId?: string;
}

export function SystemPopupMenu({ items, onSelect, onClose, returnFocus, ariaLabel = 'System menu', anchorTop, excludeTriggerSelector, menuId = 'system-popup-menu', testIdPrefix = 'system-menu', activeId }: SystemPopupMenuProps) {
    const menuRef = useRef<HTMLDivElement>(null);
    const itemRefs = useRef<Array<HTMLButtonElement | null>>([]);

    const restoreFocus = () => {
        const target = returnFocus?.();
        if (!target) return;
        window.requestAnimationFrame(() => target.focus());
    };

    useEffect(() => {
        const handleClickOutside = (e: MouseEvent) => {
            if (!menuRef.current || menuRef.current.contains(e.target as Node)) return;
            // The rail trigger owns the open/close toggle.  Treating its
            // mousedown as an outside click races the trigger's onClick:
            // onClose() runs first, then the trigger toggles the now-closed
            // state back open.  The static preview already excludes both
            // production trigger variants, so keep the event chain identical.
            const target = e.target as Element | null;
            const triggerSelector = '[data-testid="system-menu-trigger"], .mc-legacy-rail-footer .left-nav-item[role="button"]'
                + (excludeTriggerSelector ? `, ${excludeTriggerSelector}` : '');
            if (target?.closest(triggerSelector)) return;
            onClose();
        };
        const handleEscape = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                e.preventDefault();
                e.stopImmediatePropagation();
                onClose();
                restoreFocus();
            }
        };
        // Delay click listener to avoid immediate close from the same click that opened the menu
        const timer = setTimeout(() => document.addEventListener('mousedown', handleClickOutside), 0);
        document.addEventListener('keydown', handleEscape);
        return () => {
            clearTimeout(timer);
            document.removeEventListener('mousedown', handleClickOutside);
            document.removeEventListener('keydown', handleEscape);
        };
    }, [onClose, excludeTriggerSelector]);

    const visibleItems = items.filter(item => item.visible);
    const initialFocusIndex = Math.max(0, visibleItems.findIndex(item => item.id === activeId));

    return (
        <div
            ref={menuRef}
            id={menuId}
            data-testid={menuId}
            role="menu"
            aria-label={ariaLabel}
            aria-orientation="horizontal"
            style={{
                // Fixed, not absolute: the menus must escape the nav rail's
                // scroll container. The rail's overflow-x hidden would clip an
                // absolutely positioned menu that starts at the rail's right
                // edge, and a scrolled rail would drag the menu with it.
                position: 'fixed',
                left: `${SIDEBAR_NAV_RAIL_WIDTH}px`,
                ...(anchorTop != null ? { top: `${anchorTop}px`, transform: 'translateY(-50%)' } : { bottom: '8px' }),
                display: 'flex',
                flexDirection: 'row',
                gap: '2px',
                padding: '6px 8px',
                borderRadius: 'var(--radius-md, 10px)',
                border: '1px solid var(--theme-border)',
                background: 'var(--theme-surface)',
                boxShadow: 'var(--shadow-md, 0 1px 2px rgba(30,58,95,0.05), 0 4px 12px -2px rgba(30,58,95,0.10))',
                zIndex: 9999,
                whiteSpace: 'nowrap',
                maxWidth: 'calc(100vw - 80px)',
                overflowX: 'auto',
            }}
        >
            {visibleItems.map((item, index) => {
                const current = activeId === item.id;
                return (
                <button
                    key={item.id}
                    ref={node => { itemRefs.current[index] = node; }}
                    autoFocus={index === initialFocusIndex}
                    data-testid={`${testIdPrefix}-${item.id}`}
                    role="menuitem"
                    type="button"
                    aria-current={current ? 'page' : undefined}
                    onClick={() => { onSelect(item.id); onClose(); }}
                    onKeyDown={event => {
                        if (event.key === 'ArrowLeft' || event.key === 'ArrowRight' || event.key === 'Home' || event.key === 'End') {
                            event.preventDefault();
                            const currentIndex = itemRefs.current.indexOf(event.currentTarget);
                            if (currentIndex < 0 || itemRefs.current.length < 2) return;
                            const next = event.key === 'Home'
                                ? 0
                                : event.key === 'End'
                                    ? itemRefs.current.length - 1
                                    : (currentIndex + (event.key === 'ArrowRight' ? 1 : -1) + itemRefs.current.length) % itemRefs.current.length;
                            itemRefs.current[next]?.focus();
                            return;
                        }
                        if (event.key !== 'Enter' && event.key !== ' ') return;
                        event.preventDefault();
                        onSelect(item.id);
                        onClose();
                    }}
                    style={{
                        display: 'flex',
                        flexDirection: 'column',
                        alignItems: 'center',
                        gap: '3px',
                        padding: '6px 10px',
                        borderRadius: 'var(--radius-sm, 6px)',
                        cursor: 'pointer',
                        position: 'relative',
                        transition: 'background 0.15s',
                        border: 'none',
                        background: current ? 'var(--theme-primary-soft)' : 'transparent',
                        color: current ? 'var(--theme-primary-strong, var(--theme-primary))' : 'inherit',
                        font: 'inherit',
                    }}
                    onMouseEnter={e => { (e.currentTarget as HTMLElement).style.background = 'var(--theme-hover)'; }}
                    onMouseLeave={e => { e.currentTarget.style.background = current ? 'var(--theme-primary-soft)' : ''; }}
                >
                    <span className="spm-icon-wrap">
                        <span className="spm-icon">
                            {item.icon}
                        </span>
                        {item.badge != null && item.badge > 0 && (
                            <span className="spm-badge">
                                {item.badge > 99 ? '99+' : item.badge}
                            </span>
                        )}
                    </span>
                    <span className="spm-label">
                        {item.label}
                    </span>
                </button>
                );
            })}
        </div>
    );
}
