import React, { createContext, useContext, useState, useCallback, useEffect, useId, useMemo, useRef } from 'react';
import { EventsOn } from '../../wailsjs/runtime';

const localizeText = (lang: string | undefined, en: string, zhHans: string, zhHant: string = zhHans) => (
    lang === 'zh-Hans' ? zhHans : lang === 'zh-Hant' ? zhHant : en
);

// Above application overlays; task creation and guided flows reserve higher layers.
const DIALOG_Z_INDEX = 120000;

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

const dismissResultForMode = (mode: DialogMode): DialogResult => (
    mode === 'alert' ? true : mode === 'prompt' ? null : false
);

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

type PendingDialog = {
    mode: DialogMode;
    resolve: (value: DialogResult) => void;
};

const DialogContext = createContext<DialogContextValue | null>(null);

export function useDialog(): DialogContextValue {
    const ctx = useContext(DialogContext);
    if (!ctx) {
        // Do not fall back to browser dialogs: they break the application's
        // visual language and can interrupt a desktop workflow unexpectedly.
        // A provider is mounted at the app root; this defensive path only
        // protects HMR/lazy-chunk failures by safely cancelling the action.
        if (!dialogFallbackWarned) {
            dialogFallbackWarned = true;
            console.warn('[useDialog] DialogContext is null — safely cancelling dialog requests. Ensure the component is rendered within <DialogProvider>.');
        }
        return dialogFallback;
    }
    return ctx;
}

/** Stable reference fallback when DialogContext is unavailable. */
let dialogFallbackWarned = false;
const dialogFallback: DialogContextValue = {
    showAlert: async () => undefined,
    showConfirm: async () => false,
    showPrompt: async () => null,
};

// ── Provider ──

export function DialogProvider({ children }: { children: React.ReactNode }) {
	const titleId = useId();
	const messageId = useId();
    const [state, setState] = useState<DialogState>({
        open: false, title: '', message: '', mode: 'alert', lang: 'en',
    });
    const [inputValue, setInputValue] = useState('');
    const inputValueRef = useRef('');
    const pendingDialogRef = useRef<PendingDialog | null>(null);
    const backdropMouseDownRef = useRef(false);
    const inputRef = useRef<HTMLInputElement | null>(null);
    const dialogRef = useRef<HTMLDivElement | null>(null);
    const previousFocusRef = useRef<HTMLElement | null>(null);

    const setPromptInput = useCallback((value: string) => {
        inputValueRef.current = value;
        setInputValue(value);
    }, []);

    const close = useCallback((result: DialogResult) => {
        pendingDialogRef.current?.resolve(result);
        pendingDialogRef.current = null;
        setState(prev => ({ ...prev, open: false }));
        inputValueRef.current = '';
        setInputValue('');
    }, []);

    const captureInvokingFocus = useCallback(() => {
        // A new request replaces the visible dialog. Preserve the original
        // invoking control so closing the replacement does not restore focus
        // to a button that belonged to the dialog it just replaced.
        if (!previousFocusRef.current?.isConnected) {
            previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        }
    }, []);

    const showAlert = useCallback((message: string, title?: string): Promise<void> => {
        return new Promise(resolve => {
            // Resolve any pending dialog to prevent Promise leak when showAlert
            // is called while another dialog is already open (e.g. rapid backend events).
            pendingDialogRef.current?.resolve(dismissResultForMode(pendingDialogRef.current.mode));
            pendingDialogRef.current = { mode: 'alert', resolve: () => resolve() };
            captureInvokingFocus();
            setPromptInput('');
            setState({ open: true, title: title || '', message, mode: 'alert', lang: document.documentElement.lang || 'en', theme: getCurrentTheme(), darkScheme: getCurrentDarkScheme(), lightScheme: getCurrentLightScheme() });
        });
    }, [captureInvokingFocus, setPromptInput]);

    const showConfirm = useCallback((message: string, title?: string, options?: ConfirmOptions): Promise<boolean> => {
        return new Promise(resolve => {
            // Resolve any pending dialog (dismiss as "cancel") to prevent Promise leak.
            pendingDialogRef.current?.resolve(dismissResultForMode(pendingDialogRef.current.mode));
            pendingDialogRef.current = { mode: 'confirm', resolve: (value) => resolve(Boolean(value)) };
            captureInvokingFocus();
            setPromptInput('');
            setState({ open: true, title: title || '', message, mode: 'confirm', lang: document.documentElement.lang || 'en', theme: getCurrentTheme(), darkScheme: getCurrentDarkScheme(), lightScheme: getCurrentLightScheme(), confirmText: options?.confirmText, cancelText: options?.cancelText, confirmVariant: options?.confirmVariant });
        });
    }, [captureInvokingFocus, setPromptInput]);

    const showPrompt = useCallback((message: string, title?: string, options?: PromptOptions): Promise<string | null> => {
        return new Promise(resolve => {
            // Resolve any pending dialog (dismiss as cancel) to prevent Promise leak.
            pendingDialogRef.current?.resolve(dismissResultForMode(pendingDialogRef.current.mode));
            pendingDialogRef.current = { mode: 'prompt', resolve: (value) => {
                if (typeof value === 'string') resolve(value);
                else resolve(null);
            } };
            captureInvokingFocus();
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
    }, [captureInvokingFocus, setPromptInput]);

    // Navigation, HMR, and test teardown can unmount the provider while a
    // caller awaits a dialog result. Resolve it as a safe cancellation rather
    // than leaving the caller suspended forever.
    useEffect(() => () => {
        // Promise wrappers normalize this safe dismissal for confirm/prompt;
        // alert callers simply resume without a result.
        const pending = pendingDialogRef.current;
        pendingDialogRef.current = null;
        pending?.resolve(dismissResultForMode(pending.mode));
        const previousFocus = previousFocusRef.current;
        previousFocusRef.current = null;
        if (previousFocus?.isConnected) previousFocus.focus();
    }, []);

    // Listen for Go backend "show-message" events (fire-and-forget info dialogs)
    useEffect(() => {
        const handler = (data: { title: string; message: string }) => {
            showAlert(data.message, data.title);
        };
        const unsubscribe = EventsOn('show-message', handler);
        return unsubscribe;
    }, [showAlert]);

    // Move focus into the modal and restore it to the invoking control on close.
    useEffect(() => {
        if (!state.open) {
            const previousFocus = previousFocusRef.current;
            previousFocusRef.current = null;
            if (previousFocus?.isConnected) previousFocus.focus();
            return;
        }
        const id = window.setTimeout(() => {
            if (state.mode === 'prompt') {
                inputRef.current?.focus();
                inputRef.current?.select();
            } else {
                dialogRef.current?.querySelector<HTMLButtonElement>('.modal-footer button:last-child')?.focus();
            }
        }, 0);
        return () => window.clearTimeout(id);
    }, [state.open, state.mode]);

    // Prevent browser text fields behind the modal from receiving keyboard
    // input when a custom dialog owns the interaction.
    useEffect(() => {
        if (!state.open) return;
        const handleFocusIn = (event: FocusEvent) => {
            if (event.target instanceof Node && !dialogRef.current?.contains(event.target)) {
                const fallback = state.mode === 'prompt'
                    ? inputRef.current
                    : dialogRef.current?.querySelector<HTMLButtonElement>('.modal-footer button:last-child');
                fallback?.focus();
            }
        };
        document.addEventListener('focusin', handleFocusIn, true);
        return () => document.removeEventListener('focusin', handleFocusIn, true);
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
                // A focused button owns Enter. In particular, this keeps a
                // keyboard user on Cancel from accidentally confirming a
                // non-destructive dialog through the window-level shortcut.
                if (e.target instanceof HTMLButtonElement) return;
                if (state.mode === 'prompt') {
                    // Submit from the prompt input without rebinding on every keystroke.
                    e.preventDefault();
                    e.stopImmediatePropagation();
                    close(inputValueRef.current);
                    return;
                }
                if (state.confirmVariant === 'danger') {
                    e.preventDefault();
                    return;
                }
                close(true);
                return;
            }
            if (e.key === 'Tab') {
                const focusable = dialogRef.current?.querySelectorAll<HTMLElement>(
                    'button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [href]',
                );
                if (!focusable?.length) return;
                const items = Array.from(focusable);
                const currentIndex = items.indexOf(document.activeElement as HTMLElement);
                const nextIndex = e.shiftKey
                    ? (currentIndex <= 0 ? items.length - 1 : currentIndex - 1)
                    : (currentIndex === items.length - 1 ? 0 : currentIndex + 1);
                e.preventDefault();
                e.stopImmediatePropagation();
                items[nextIndex].focus();
            }
        };
        // Capture phase so we run before other window keydown handlers (e.g. nested
        // LLM config Escape-to-close) and stopImmediatePropagation can take effect.
        window.addEventListener('keydown', onKey, true);
        return () => window.removeEventListener('keydown', onKey, true);
    }, [state.open, state.mode, state.confirmVariant, close]);

    const dismissResult = dismissResultForMode(state.mode);
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
                    <div
						className="modal-content custom-dialog"
						ref={dialogRef}
						role="dialog"
						aria-modal="true"
						aria-labelledby={state.title ? titleId : undefined}
						aria-label={state.title ? undefined : localizeText(state.lang, 'Dialog', '对话框')}
						aria-describedby={messageId}
						onClick={e => e.stopPropagation()}
						style={{
							width: state.mode === 'prompt' ? 'min(420px, calc(100vw - 32px))' : 'min(320px, calc(100vw - 32px))',
							maxHeight: 'calc(100dvh - 32px)',
							overflowY: 'auto',
						}}
					>
                        {state.title && (
                            <div className="modal-header">
                                <h3 id={titleId} style={{ fontSize: '0.88rem', margin: 0 }}>{state.title}</h3>
                                <button type="button" className="btn-close" aria-label={localizeText(state.lang, 'Close', '关闭')} onClick={() => close(dismissResult)}>×</button>
                            </div>
                        )}
                        <div className="modal-body">
                            <p id={messageId} style={{ fontSize: '0.8rem', color: 'var(--theme-text-secondary)', margin: 0, wordBreak: 'break-word', whiteSpace: 'pre-wrap' }}>
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
                                <button type="button" className="btn-secondary" style={{ fontSize: '0.78rem', padding: '4px 14px' }} onClick={() => close(state.mode === 'prompt' ? null : false)}>
                                    {state.cancelText || localizeText(state.lang, 'Cancel', '取消')}
                                </button>
                            )}
                            <button
                                type="button"
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
