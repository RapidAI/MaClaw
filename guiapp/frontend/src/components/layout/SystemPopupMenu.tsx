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
}

export function SystemPopupMenu({ items, onSelect, onClose, returnFocus, ariaLabel = 'System menu', anchorTop, excludeTriggerSelector, menuId = 'system-popup-menu', testIdPrefix = 'system-menu' }: SystemPopupMenuProps) {
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

    return (
        <div
            ref={menuRef}
            id={menuId}
            data-testid={menuId}
            role="menu"
            aria-label={ariaLabel}
            aria-orientation="horizontal"
            style={{
                position: 'absolute',
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
            {visibleItems.map((item, index) => (
                <button
                    key={item.id}
                    ref={node => { itemRefs.current[index] = node; }}
                    autoFocus={index === 0}
                    data-testid={`${testIdPrefix}-${item.id}`}
                    role="menuitem"
                    type="button"
                    onClick={() => { onSelect(item.id); onClose(); }}
                    onKeyDown={event => {
                        if (event.key === 'ArrowLeft' || event.key === 'ArrowRight' || event.key === 'Home' || event.key === 'End') {
                            event.preventDefault();
                            const current = itemRefs.current.indexOf(event.currentTarget);
                            if (current < 0 || itemRefs.current.length < 2) return;
                            const next = event.key === 'Home'
                                ? 0
                                : event.key === 'End'
                                    ? itemRefs.current.length - 1
                                    : (current + (event.key === 'ArrowRight' ? 1 : -1) + itemRefs.current.length) % itemRefs.current.length;
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
                        background: 'transparent',
                        color: 'inherit',
                        font: 'inherit',
                    }}
                    onMouseEnter={e => { (e.currentTarget as HTMLElement).style.background = 'var(--theme-hover)'; }}
                    onMouseLeave={e => { (e.currentTarget as HTMLElement).style.background = ''; }}
                >
                    <span style={{ fontSize: '1.1rem', lineHeight: 1, position: 'relative' }}>
                        <span style={{ display: 'inline-flex', color: 'var(--theme-text-secondary)', opacity: 0.85 }}>
                            {item.icon}
                        </span>
                        {item.badge != null && item.badge > 0 && (
                            <span style={{
                                position: 'absolute', top: '-4px', right: '-8px',
                                minWidth: '16px', height: '16px', lineHeight: '16px',
                                fontSize: '9px', fontWeight: 700, textAlign: 'center',
                                padding: '0 3px', borderRadius: 'var(--radius-pill, 999px)',
                                background: 'var(--theme-danger)', color: '#fff',
                                boxShadow: 'var(--shadow-sm, 0 1px 2px rgba(30,58,95,0.06))',
                            }}>
                                {item.badge > 99 ? '99+' : item.badge}
                            </span>
                        )}
                    </span>
                    <span style={{ fontSize: '0.65rem', fontWeight: 600, color: 'var(--theme-text-primary)' }}>
                        {item.label}
                    </span>
                </button>
            ))}
        </div>
    );
}
