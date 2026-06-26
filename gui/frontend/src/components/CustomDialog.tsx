import React, { createContext, useContext, useState, useCallback, useEffect, useRef } from 'react';
import { EventsOn } from '../../wailsjs/runtime';

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

/** Read the current theme from the #App element so the dialog inherits dark-mode variables. */
function getCurrentTheme(): string | undefined {
    return document.getElementById('App')?.getAttribute('data-ai-theme') || undefined;
}

function getCurrentDarkScheme(): string | undefined {
    return document.getElementById('App')?.getAttribute('data-ai-dark-scheme') || undefined;
}

// ── Types ──

interface DialogState {
    open: boolean;
    title: string;
    message: string;
    mode: 'alert' | 'confirm';
    lang?: string;
    theme?: string;
    darkScheme?: string;
    confirmText?: string;
    cancelText?: string;
}

interface ConfirmOptions {
    confirmText?: string;
    cancelText?: string;
}

interface DialogContextValue {
    showAlert: (message: string, title?: string) => Promise<void>;
    showConfirm: (message: string, title?: string, options?: ConfirmOptions) => Promise<boolean>;
}

const DialogContext = createContext<DialogContextValue | null>(null);

export function useDialog(): DialogContextValue {
    const ctx = useContext(DialogContext);
    if (!ctx) {
        // Defensive fallback: instead of throwing (which would bubble to the
        // global error handler and potentially destroy the entire React tree),
        // return a degraded implementation using native browser dialogs.
        // This can happen during HMR, lazy-chunk reload, or React context loss.
        if (!dialogFallbackWarned) {
            dialogFallbackWarned = true;
            console.warn('[useDialog] DialogContext is null — falling back to native dialogs. This likely means a component using useDialog is rendered outside <DialogProvider>.');
        }
        return dialogFallback;
    }
    return ctx;
}

/** Stable reference fallback when DialogContext is unavailable. */
let dialogFallbackWarned = false;
const dialogFallback: DialogContextValue = {
    showAlert: async (message: string, title?: string) => { window.alert(title ? `${title}\n\n${message}` : message); },
    showConfirm: async (message: string, _title?: string) => window.confirm(message),
};

// ── Provider ──

export function DialogProvider({ children }: { children: React.ReactNode }) {
    const [state, setState] = useState<DialogState>({
        open: false, title: '', message: '', mode: 'alert', lang: 'en',
    });
    const resolveRef = useRef<((value: boolean) => void) | null>(null);
    const backdropMouseDownRef = useRef(false);

    const close = useCallback((result: boolean) => {
        resolveRef.current?.(result);
        resolveRef.current = null;
        setState(prev => ({ ...prev, open: false }));
    }, []);

    const showAlert = useCallback((message: string, title?: string): Promise<void> => {
        return new Promise(resolve => {
            // Resolve any pending dialog to prevent Promise leak when showAlert
            // is called while another dialog is already open (e.g. rapid backend events).
            resolveRef.current?.(false);
            resolveRef.current = () => resolve();
            setState({ open: true, title: title || '', message, mode: 'alert', lang: document.documentElement.lang || 'en', theme: getCurrentTheme(), darkScheme: getCurrentDarkScheme() });
        });
    }, []);

    const showConfirm = useCallback((message: string, title?: string, options?: ConfirmOptions): Promise<boolean> => {
        return new Promise(resolve => {
            // Resolve any pending dialog (dismiss as "cancel") to prevent Promise leak.
            resolveRef.current?.(false);
            resolveRef.current = resolve;
            setState({ open: true, title: title || '', message, mode: 'confirm', lang: document.documentElement.lang || 'en', theme: getCurrentTheme(), darkScheme: getCurrentDarkScheme(), confirmText: options?.confirmText, cancelText: options?.cancelText });
        });
    }, []);

    // Listen for Go backend "show-message" events (fire-and-forget info dialogs)
    useEffect(() => {
        const handler = (data: { title: string; message: string }) => {
            showAlert(data.message, data.title);
        };
        const unsubscribe = EventsOn('show-message', handler);
        return unsubscribe;
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
                <div className="modal-backdrop" data-ai-theme={state.theme} data-ai-dark-scheme={state.darkScheme}
                    onMouseDown={e => { backdropMouseDownRef.current = e.target === e.currentTarget; }}
                    onClick={e => { if (e.target === e.currentTarget && backdropMouseDownRef.current) close(state.mode === 'alert'); backdropMouseDownRef.current = false; }}
                >
                    <div className="modal-content" onClick={e => e.stopPropagation()} style={{ width: '320px' }}>
                        {state.title && (
                            <div className="modal-header">
                                <h3 style={{ fontSize: '0.88rem', margin: 0 }}>{state.title}</h3>
                                <button className="btn-close" onClick={() => close(state.mode === 'alert')}>×</button>
                            </div>
                        )}
                        <div className="modal-body">
                            <p style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)', margin: 0, wordBreak: 'break-word', whiteSpace: 'pre-wrap' }}>
                                {state.message}
                            </p>
                        </div>
                        <div className="modal-footer">
                            {state.mode === 'confirm' && (
                                <button className="btn-secondary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={() => close(false)}>
                                    {state.cancelText || localizeText(state.lang, 'Cancel', '取消')}
                                </button>
                            )}
                            <button className="btn-primary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={() => close(true)}>
                                {state.confirmText || localizeText(state.lang, 'OK', '确定')}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </DialogContext.Provider>
    );
}
