import React, { createContext, useContext, useState, useCallback, useEffect, useRef } from 'react';
import { EventsOn, EventsOff } from '../../wailsjs/runtime';

// ── Types ──

interface DialogState {
    open: boolean;
    title: string;
    message: string;
    mode: 'alert' | 'confirm';
}

interface DialogContextValue {
    showAlert: (message: string, title?: string) => Promise<void>;
    showConfirm: (message: string, title?: string) => Promise<boolean>;
}

const DialogContext = createContext<DialogContextValue | null>(null);

export function useDialog(): DialogContextValue {
    const ctx = useContext(DialogContext);
    if (!ctx) throw new Error('useDialog must be used within DialogProvider');
    return ctx;
}

// ── Provider ──

export function DialogProvider({ children }: { children: React.ReactNode }) {
    const [state, setState] = useState<DialogState>({
        open: false, title: '', message: '', mode: 'alert',
    });
    const resolveRef = useRef<((value: boolean) => void) | null>(null);

    const close = useCallback((result: boolean) => {
        resolveRef.current?.(result);
        resolveRef.current = null;
        setState(prev => ({ ...prev, open: false }));
    }, []);

    const showAlert = useCallback((message: string, title?: string): Promise<void> => {
        return new Promise(resolve => {
            resolveRef.current = () => resolve();
            setState({ open: true, title: title || '', message, mode: 'alert' });
        });
    }, []);

    const showConfirm = useCallback((message: string, title?: string): Promise<boolean> => {
        return new Promise(resolve => {
            resolveRef.current = resolve;
            setState({ open: true, title: title || '', message, mode: 'confirm' });
        });
    }, []);

    // Listen for Go backend "show-message" events (fire-and-forget info dialogs)
    useEffect(() => {
        const handler = (data: { title: string; message: string }) => {
            showAlert(data.message, data.title);
        };
        EventsOn('show-message', handler);
        return () => { EventsOff('show-message'); };
    }, [showAlert]);

    // Escape key
    useEffect(() => {
        if (!state.open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') close(state.mode === 'alert');
            if (e.key === 'Enter') close(true);
        };
        window.addEventListener('keydown', onKey);
        return () => window.removeEventListener('keydown', onKey);
    }, [state.open, state.mode, close]);

    return (
        <DialogContext.Provider value={{ showAlert, showConfirm }}>
            {children}
            {state.open && (
                <div className="modal-backdrop" onClick={() => close(state.mode === 'alert')}>
                    <div className="modal-content" onClick={e => e.stopPropagation()} style={{ width: '320px' }}>
                        {state.title && (
                            <div className="modal-header">
                                <h3 style={{ fontSize: '0.88rem', margin: 0 }}>{state.title}</h3>
                                <button className="btn-close" onClick={() => close(state.mode === 'alert')}>×</button>
                            </div>
                        )}
                        <div className="modal-body">
                            <p style={{ fontSize: '0.8rem', color: '#5a6577', margin: 0, wordBreak: 'break-word', whiteSpace: 'pre-wrap' }}>
                                {state.message}
                            </p>
                        </div>
                        <div className="modal-footer">
                            {state.mode === 'confirm' && (
                                <button className="btn-secondary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={() => close(false)}>
                                    取消
                                </button>
                            )}
                            <button className="btn-primary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={() => close(true)}>
                                确定
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </DialogContext.Provider>
    );
}
