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
}

export function SystemPopupMenu({ items, onSelect, onClose }: SystemPopupMenuProps) {
    const menuRef = useRef<HTMLDivElement>(null);

    useEffect(() => {
        const handleClickOutside = (e: MouseEvent) => {
            if (menuRef.current && !menuRef.current.contains(e.target as Node)) {
                onClose();
            }
        };
        const handleEscape = (e: KeyboardEvent) => {
            if (e.key === 'Escape') onClose();
        };
        // Delay click listener to avoid immediate close from the same click that opened the menu
        const timer = setTimeout(() => document.addEventListener('mousedown', handleClickOutside), 0);
        document.addEventListener('keydown', handleEscape);
        return () => {
            clearTimeout(timer);
            document.removeEventListener('mousedown', handleClickOutside);
            document.removeEventListener('keydown', handleEscape);
        };
    }, [onClose]);

    const visibleItems = items.filter(item => item.visible);

    return (
        <div
            ref={menuRef}
            data-testid="system-popup-menu"
            role="menu"
            aria-label="System menu"
            style={{
                position: 'absolute',
                left: `${SIDEBAR_NAV_RAIL_WIDTH}px`,
                bottom: '8px',
                display: 'flex',
                flexDirection: 'row',
                gap: '2px',
                padding: '6px 8px',
                borderRadius: '10px',
                border: '1px solid var(--theme-border)',
                background: 'var(--theme-surface)',
                boxShadow: '0 4px 16px rgba(0,0,0,0.12)',
                zIndex: 9999,
                whiteSpace: 'nowrap',
                maxWidth: 'calc(100vw - 80px)',
                overflowX: 'auto',
            }}
        >
            {visibleItems.map(item => (
                <div
                    key={item.id}
                    data-testid={`system-menu-${item.id}`}
                    role="menuitem"
                    onClick={() => { onSelect(item.id); onClose(); }}
                    style={{
                        display: 'flex',
                        flexDirection: 'column',
                        alignItems: 'center',
                        gap: '3px',
                        padding: '6px 10px',
                        borderRadius: '8px',
                        cursor: 'pointer',
                        position: 'relative',
                        transition: 'background 0.15s',
                    }}
                    onMouseEnter={e => { (e.currentTarget as HTMLElement).style.background = 'var(--theme-hover)'; }}
                    onMouseLeave={e => { (e.currentTarget as HTMLElement).style.background = ''; }}
                >
                    <span style={{ fontSize: '1.1rem', lineHeight: 1, position: 'relative' }}>
                        <span style={{ filter: 'grayscale(1) saturate(0) brightness(0.68)', opacity: 0.96, display: 'inline-block' }}>
                            {item.icon}
                        </span>
                        {item.badge != null && item.badge > 0 && (
                            <span style={{
                                position: 'absolute', top: '-4px', right: '-8px',
                                minWidth: '16px', height: '16px', lineHeight: '16px',
                                fontSize: '9px', fontWeight: 700, textAlign: 'center',
                                padding: '0 3px', borderRadius: '999px',
                                background: 'var(--theme-danger)', color: '#fff',
                                boxShadow: '0 1px 2px rgba(0,0,0,0.2)',
                            }}>
                                {item.badge > 99 ? '99+' : item.badge}
                            </span>
                        )}
                    </span>
                    <span style={{ fontSize: '0.65rem', fontWeight: 600, color: 'var(--theme-text-primary)' }}>
                        {item.label}
                    </span>
                </div>
            ))}
        </div>
    );
}
