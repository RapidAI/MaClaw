import React, { createContext, useContext, useState, useCallback, useEffect, useMemo, useRef } from 'react';
import { EventsOn } from '../../wailsjs/runtime';

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

// Above nested app dialogs (e.g. LLM config overlay uses zIndex 9999).
const DIALOG_Z_INDEX = 11000;

/** Read the current theme from the #App element so the dialog inherits dark-mode variables. */
function getCurrentTheme(): string | undefined {
    return document.getElementById('App')?.getAttribute('data-ai-theme') || undefined;
}

function getCurrentDarkScheme(): string | undefined {
    return document.getElementById('App')?.getAttribute('data-ai-dark-scheme') || undefined;
}

function getCurrentLightScheme(): string | undefined {
    return document.getElementById('App')?.getAttribute('data-ai-light-scheme') || undefined;
}

// ── Types ──

type DialogMode = 'alert' | 'confirm' | 'prompt';
type DialogResult = boolean | string | null;

interface DialogState {
    open: boolean;
    title: string;
    message: string;
    mode: DialogMode;
    lang?: string;
    theme?: string;
    darkScheme?: string;
    lightScheme?: string;
    confirmText?: string;
    cancelText?: string;
    confirmVariant?: 'primary' | 'danger';
    placeholder?: string;
}

interface ConfirmOptions {
    confirmText?: string;
    cancelText?: string;
    confirmVariant?: 'primary' | 'danger';
}

interface PromptOptions {
    confirmText?: string;
    cancelText?: string;
    defaultValue?: string;
    placeholder?: string;
}

interface DialogContextValue {
    showAlert: (message: string, title?: string) => Promise<void>;
    showConfirm: (message: string, title?: string, options?: ConfirmOptions) => Promise<boolean>;
    showPrompt: (message: string, title?: string, options?: PromptOptions) => Promise<string | null>;
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
    showPrompt: async (message: string, _title?: string, options?: PromptOptions) => window.prompt(message, options?.defaultValue ?? ''),
};

// ── Provider ──

export function DialogProvider({ children }: { children: React.ReactNode }) {
    const [state, setState] = useState<DialogState>({
        open: false, title: '', message: '', mode: 'alert', lang: 'en',
    });
    const [inputValue, setInputValue] = useState('');
    const inputValueRef = useRef('');
    const resolveRef = useRef<((value: DialogResult) => void) | null>(null);
    const backdropMouseDownRef = useRef(false);
    const inputRef = useRef<HTMLInputElement | null>(null);

    const setPromptInput = useCallback((value: string) => {
        inputValueRef.current = value;
        setInputValue(value);
    }, []);

    const close = useCallback((result: DialogResult) => {
        resolveRef.current?.(result);
        resolveRef.current = null;
        setState(prev => ({ ...prev, open: false }));
        inputValueRef.current = '';
        setInputValue('');
    }, []);

    const showAlert = useCallback((message: string, title?: string): Promise<void> => {
        return new Promise(resolve => {
            // Resolve any pending dialog to prevent Promise leak when showAlert
            // is called while another dialog is already open (e.g. rapid backend events).
            resolveRef.current?.(false);
            resolveRef.current = () => resolve();
            setPromptInput('');
            setState({ open: true, title: title || '', message, mode: 'alert', lang: document.documentElement.lang || 'en', theme: getCurrentTheme(), darkScheme: getCurrentDarkScheme(), lightScheme: getCurrentLightScheme() });
        });
    }, [setPromptInput]);

    const showConfirm = useCallback((message: string, title?: string, options?: ConfirmOptions): Promise<boolean> => {
        return new Promise(resolve => {
            // Resolve any pending dialog (dismiss as "cancel") to prevent Promise leak.
            resolveRef.current?.(false);
            resolveRef.current = (value) => resolve(Boolean(value));
            setPromptInput('');
            setState({ open: true, title: title || '', message, mode: 'confirm', lang: document.documentElement.lang || 'en', theme: getCurrentTheme(), darkScheme: getCurrentDarkScheme(), lightScheme: getCurrentLightScheme(), confirmText: options?.confirmText, cancelText: options?.cancelText, confirmVariant: options?.confirmVariant });
        });
    }, [setPromptInput]);

    const showPrompt = useCallback((message: string, title?: string, options?: PromptOptions): Promise<string | null> => {
        return new Promise(resolve => {
            // Resolve any pending dialog (dismiss as cancel) to prevent Promise leak.
            resolveRef.current?.(null);
            resolveRef.current = (value) => {
                if (typeof value === 'string') resolve(value);
                else resolve(null);
            };
            const initial = options?.defaultValue ?? '';
            setPromptInput(initial);
            setState({
                open: true,
                title: title || '',
                message,
                mode: 'prompt',
                lang: document.documentElement.lang || 'en',
                theme: getCurrentTheme(),
                darkScheme: getCurrentDarkScheme(),
                lightScheme: getCurrentLightScheme(),
                confirmText: options?.confirmText,
                cancelText: options?.cancelText,
                placeholder: options?.placeholder,
            });
        });
    }, [setPromptInput]);

    // Listen for Go backend "show-message" events (fire-and-forget info dialogs)
    useEffect(() => {
        const handler = (data: { title: string; message: string }) => {
            showAlert(data.message, data.title);
        };
        const unsubscribe = EventsOn('show-message', handler);
        return unsubscribe;
    }, [showAlert]);

    // Auto-focus prompt input when dialog opens
    useEffect(() => {
        if (!state.open || state.mode !== 'prompt') return;
        const id = window.setTimeout(() => {
            inputRef.current?.focus();
            inputRef.current?.select();
        }, 0);
        return () => window.clearTimeout(id);
    }, [state.open, state.mode]);

    // Escape / Enter keys — use inputValueRef so we do not rebind on every keystroke.
    useEffect(() => {
        if (!state.open) return;
        const onKey = (e: KeyboardEvent) => {
            if (e.key === 'Escape') {
                // stopImmediatePropagation: other window listeners (e.g. LLM config
                // Escape-to-close) must not also run while our dialog owns focus.
                e.preventDefault();
                e.stopImmediatePropagation();
                if (state.mode === 'prompt') close(null);
                else close(state.mode === 'alert');
                return;
            }
            if (e.key === 'Enter') {
                // Ignore Enter while IME is composing (e.g. Chinese input method).
                if (e.isComposing || (e as KeyboardEvent & { keyCode?: number }).keyCode === 229) {
                    return;
                }
                if (state.mode === 'prompt') {
                    // Allow Enter from input to submit; avoid double-handling button focus cases.
                    if (e.target instanceof HTMLButtonElement) return;
                    e.preventDefault();
                    e.stopImmediatePropagation();
                    close(inputValueRef.current);
                    return;
                }
                if (state.confirmVariant === 'danger') {
                    if (!(e.target instanceof HTMLButtonElement)) e.preventDefault();
                    return;
                }
                close(true);
            }
        };
        // Capture phase so we run before other window keydown handlers (e.g. nested
        // LLM config Escape-to-close) and stopImmediatePropagation can take effect.
        window.addEventListener('keydown', onKey, true);
        return () => window.removeEventListener('keydown', onKey, true);
    }, [state.open, state.mode, state.confirmVariant, close]);

    const dismissResult: DialogResult = state.mode === 'alert' ? true : state.mode === 'prompt' ? null : false;
    const dialogApi = useMemo(
        () => ({ showAlert, showConfirm, showPrompt }),
        [showAlert, showConfirm, showPrompt],
    );

    return (
        <DialogContext.Provider value={dialogApi}>
            {children}
            {state.open && (
                <div
                    className="modal-backdrop"
                    data-ai-theme={state.theme}
                    data-ai-dark-scheme={state.darkScheme}
                    data-ai-light-scheme={state.lightScheme}
                    style={{ zIndex: DIALOG_Z_INDEX }}
                    onMouseDown={e => { backdropMouseDownRef.current = e.target === e.currentTarget; }}
                    onClick={e => { if (e.target === e.currentTarget && backdropMouseDownRef.current) close(dismissResult); backdropMouseDownRef.current = false; }}
                >
                    <div className="modal-content" onClick={e => e.stopPropagation()} style={{ width: state.mode === 'prompt' ? '420px' : '320px' }}>
                        {state.title && (
                            <div className="modal-header">
                                <h3 style={{ fontSize: '0.88rem', margin: 0 }}>{state.title}</h3>
                                <button className="btn-close" onClick={() => close(dismissResult)}>×</button>
                            </div>
                        )}
                        <div className="modal-body">
                            <p style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)', margin: 0, wordBreak: 'break-word', whiteSpace: 'pre-wrap' }}>
                                {state.message}
                            </p>
                            {state.mode === 'prompt' && (
                                <input
                                    ref={inputRef}
                                    className="form-input"
                                    type="text"
                                    value={inputValue}
                                    placeholder={state.placeholder || ''}
                                    onChange={e => setPromptInput(e.target.value)}
                                    style={{
                                        width: '100%',
                                        marginTop: '12px',
                                        boxSizing: 'border-box',
                                        fontSize: '0.82rem',
                                        padding: '8px 10px',
                                    }}
                                    autoComplete="off"
                                    spellCheck={false}
                                />
                            )}
                        </div>
                        <div className="modal-footer">
                            {(state.mode === 'confirm' || state.mode === 'prompt') && (
                                <button className="btn-secondary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={() => close(state.mode === 'prompt' ? null : false)}>
                                    {state.cancelText || localizeText(state.lang, 'Cancel', '取消')}
                                </button>
                            )}
                            <button
                                className={state.confirmVariant === 'danger' ? 'btn-secondary btn-danger' : 'btn-primary'}
                                style={{ fontSize: '0.78rem', padding: '4px 14px' }}
                                onClick={() => close(state.mode === 'prompt' ? inputValueRef.current : true)}
                            >
                                {state.confirmText || localizeText(state.lang, 'OK', '确定')}
                            </button>
                        </div>
                    </div>
                </div>
            )}
        </DialogContext.Provider>
    );
}
