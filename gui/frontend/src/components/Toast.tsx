import React, { createContext, useContext, useState, useCallback, useEffect, useRef } from 'react';
import { EventsOn, EventsOff } from '../../wailsjs/runtime';

// ── Types ──

export type ToastType = 'success' | 'error' | 'info' | 'warning';

interface ToastItem {
    id: number;
    message: string;
    type: ToastType;
    duration: number;
}

interface ToastContextValue {
    showToast: (message: string, type?: ToastType, duration?: number) => number;
    dismissToast: (id: number) => void;
}

const ToastContext = createContext<ToastContextValue | null>(null);

export function useToast(): ToastContextValue {
    const ctx = useContext(ToastContext);
    if (!ctx) throw new Error('useToast must be used within ToastProvider');
    return ctx;
}

// ── Styles ──

const TYPE_STYLES: Record<ToastType, { bg: string; color: string; border: string; icon: string }> = {
    success: {
        bg: 'var(--theme-success-bg, #e6f9e6)',
        color: 'var(--theme-success, #22c55e)',
        border: 'var(--theme-success, #22c55e)',
        icon: '✓',
    },
    error: {
        bg: 'var(--theme-danger-bg, #fde8e8)',
        color: 'var(--theme-danger, #ef4444)',
        border: 'var(--theme-danger, #ef4444)',
        icon: '✕',
    },
    warning: {
        bg: 'var(--theme-warning-bg, #fff8e1)',
        color: 'var(--theme-warning, #f59e0b)',
        border: 'var(--theme-warning, #f59e0b)',
        icon: '⚠',
    },
    info: {
        bg: 'var(--theme-info-bg, #e8f0fe)',
        color: 'var(--theme-primary, #3b82f6)',
        border: 'var(--theme-primary, #3b82f6)',
        icon: 'ℹ',
    },
};

const MAX_TOASTS = 5;
let nextId = 0;

// ── Single Toast Item ──

function ToastItemView({ item, onDismiss }: { item: ToastItem; onDismiss: (id: number) => void }) {
    const style = TYPE_STYLES[item.type];
    return (
        <div
            role="alert"
            onClick={() => onDismiss(item.id)}
            style={{
                display: 'flex',
                alignItems: 'center',
                gap: '8px',
                padding: '8px 14px',
                borderRadius: '8px',
                fontSize: '0.8rem',
                fontWeight: 500,
                background: style.bg,
                color: style.color,
                border: `1px solid color-mix(in srgb, ${style.border} 30%, transparent)`,
                boxShadow: '0 2px 8px rgba(0,0,0,0.12)',
                cursor: 'pointer',
                maxWidth: '420px',
                wordBreak: 'break-word',
                animation: 'toast-slide-in 0.25s ease-out',
                backdropFilter: 'blur(6px)',
            }}
        >
            <span style={{ fontSize: '0.9rem', flexShrink: 0 }}>{style.icon}</span>
            <span style={{ flex: 1 }}>{item.message}</span>
        </div>
    );
}

// ── Provider ──

export function ToastProvider({ children }: { children: React.ReactNode }) {
    const [toasts, setToasts] = useState<ToastItem[]>([]);
    const timersRef = useRef<Map<number, ReturnType<typeof setTimeout>>>(new Map());

    const dismiss = useCallback((id: number) => {
        setToasts(prev => prev.filter(t => t.id !== id));
        const timer = timersRef.current.get(id);
        if (timer) { clearTimeout(timer); timersRef.current.delete(id); }
    }, []);

    const showToast = useCallback((message: string, type: ToastType = 'info', duration: number = 3000): number => {
        const id = ++nextId;
        setToasts(prev => [...prev.slice(-(MAX_TOASTS - 1)), { id, message, type, duration }]);
        if (duration > 0) {
            const timer = setTimeout(() => dismiss(id), duration);
            timersRef.current.set(id, timer);
        }
        return id;
    }, [dismiss]);

    // Listen for Go backend "show-toast" events
    useEffect(() => {
        const handler = (data: { message: string; type?: string; duration?: number }) => {
            const t = (data.type as ToastType) || 'info';
            showToast(data.message, t, data.duration ?? 3000);
        };
        EventsOn('show-toast', handler);
        return () => { EventsOff('show-toast'); };
    }, [showToast]);

    // Cleanup timers on unmount
    useEffect(() => {
        return () => {
            timersRef.current.forEach(t => clearTimeout(t));
        };
    }, []);

    return (
        <ToastContext.Provider value={{ showToast, dismissToast: dismiss }}>
            {children}
            {/* Toast container — fixed bottom-center */}
            {toasts.length > 0 && (
                <div
                    style={{
                        position: 'fixed',
                        bottom: '24px',
                        left: '50%',
                        transform: 'translateX(-50%)',
                        zIndex: 6000,
                        display: 'flex',
                        flexDirection: 'column',
                        gap: '6px',
                        alignItems: 'center',
                        pointerEvents: 'auto',
                    }}
                >
                    {toasts.map(item => (
                        <ToastItemView key={item.id} item={item} onDismiss={dismiss} />
                    ))}
                </div>
            )}
        </ToastContext.Provider>
    );
}
