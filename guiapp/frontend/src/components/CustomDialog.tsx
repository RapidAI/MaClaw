import React, { createContext, Fragment, useContext, useState, useCallback, useEffect, useId, useMemo, useRef } from 'react';
import { EventsOn } from '../../wailsjs/runtime';
import { ResolveFrontendConfirm } from '../../wailsjs/go/main/App';
import { EVENT_SHOW_CONFIRM } from '../constants/events';

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

export type DialogPresentation = {
    lead: string;
    subject?: string;
    /** Explanatory paragraphs that follow a lifted question. */
    follow?: string;
    /** A supplementary list, such as linked tasks that will also be deleted. */
    detail?: string;
    notice?: string;
};

const DIALOG_NOTICE_RE = /此操作不可撤[销銷]|此操作不可復原|this (?:action )?cannot be undone/i;

/**
 * Lift a quoted subject and an irreversibility line out of a flat sentence.
 * A quote is lifted only when the sentence still reads after it leaves:
 * the quote is the last word of a question, or it is the object of
 * 「及/和/and all」. Mid-sentence quotes stay in the lead.
 */
export function presentDialogMessage(message: string): DialogPresentation {
    const trimmed = message.replace(/\r\n/g, '\n').trim();
    if (!trimmed) return { lead: '' };

    let notice: string | undefined;
    let rest = trimmed;
    const noticeMatch = DIALOG_NOTICE_RE.exec(trimmed);
    const noticeBoundary = noticeMatch?.index;
    const boundaryChar = noticeBoundary === undefined ? undefined : trimmed[noticeBoundary + noticeMatch![0].length];
    if (noticeMatch && noticeBoundary !== undefined && (boundaryChar === undefined || /[。．.!！\n]/.test(boundaryChar))) {
        let afterNotice = noticeBoundary + noticeMatch[0].length;
        while (afterNotice < trimmed.length && /[。．.!！]/.test(trimmed[afterNotice])) afterNotice += 1;
        notice = trimmed.slice(noticeBoundary, afterNotice).replace(/[。．.!！]+$/g, '').trim();
        const rawTail = trimmed.slice(afterNotice);
        const tail = rawTail.trim();
        const beforeRaw = trimmed.slice(0, noticeBoundary);
        if (!tail) {
            rest = beforeRaw.replace(/[，,、\s]+$/g, '').trim();
        } else if (/^[ \t]*\n/.test(rawTail)) {
            rest = [beforeRaw.replace(/[，,、\s]+$/g, '').trim(), tail].filter(Boolean).join('\n');
        } else {
            // The notice sat in the middle of one line. Keep the surrounding
            // words together instead of inventing a paragraph break.
            rest = joinDialogParts(beforeRaw.trimEnd(), tail);
        }
    }

    const quotes = Array.from(rest.matchAll(/「([^」]+)」|“([^”]+)”|"([^"]+)"/g));
    if (quotes.length !== 1 || quotes[0].index === undefined) {
        return { ...splitLeadDetail(rest), notice };
    }

    const quote = quotes[0];
    const subject = (quote[1] || quote[2] || quote[3] || '').trim();
    const before = rest.slice(0, quote.index).trim();
    if (!subject || !before) return { ...splitLeadDetail(rest), notice };

    const afterRaw = rest.slice(quote.index + quote[0].length).replace(/^\s+/, '');
    const lineBreak = afterRaw.indexOf('\n');
    const afterFirst = (lineBreak === -1 ? afterRaw : afterRaw.slice(0, lineBreak)).trim();
    const detailFromAfter = lineBreak === -1 ? '' : afterRaw.slice(lineBreak + 1).trim();
    const closing = sentenceClosing(afterFirst);
    // Rewrite only when the quote is the whole object of a bare verb
    // ("永久删除「名称」及…"). "删除文件夹「名称」及其…" and
    // "Delete folder “name” and all …" already name the thing.
    const continues = /^(?:及(?!其)|和(?!其)|与(?!其)|並(?!其)|并(?!其)|and\s+all\b)/i.test(afterFirst)
        && /^(?:永久删除|永久刪除|删除|刪除|移除|卸载|卸載|permanently delete|delete|remove)$/i.test(before);
    if (closing === null && !continues) {
        return { ...splitLeadDetail(rest), notice };
    }

    const lead = closing !== null
        ? `${before}${closing}`
        : joinDialogParts(before, continuedObject(afterFirst, `${before}\n${afterFirst}`));
    const presented: DialogPresentation = { lead, subject, notice };
    if (!detailFromAfter) return presented;
    // A list is a separate block. Any other following paragraph is still
    // the message, not a footnote.
    if (isSupplementaryList(detailFromAfter)) presented.detail = detailFromAfter;
    else presented.follow = detailFromAfter;
    return presented;
}

function sentenceClosing(afterFirst: string): string | null {
    return afterFirst === '？' || afterFirst === '?' ? afterFirst : null;
}

function continuedObject(afterFirst: string, sample: string): string {
    const item = /[刪檔復雲註嗎麼裡後個務]/.test(sample) ? '此項' : '此项';
    return afterFirst
        .replace(/^(及|和|与|並|并)/, `${item}$1`)
        .replace(/^and\s+all\b/i, 'this item and all');
}

function joinDialogParts(before: string, after: string): string {
    if (!before) return after;
    if (!after) return before;
    const needsSpace = /[A-Za-z0-9.!?]$/.test(before) && /^[A-Za-z0-9]/.test(after);
    return `${before}${needsSpace ? ' ' : ''}${after}`.replace(/[ \t]{2,}/g, ' ').trim();
}

function splitLeadDetail(text: string): Pick<DialogPresentation, 'lead' | 'detail'> {
    const lineBreak = text.indexOf('\n');
    if (lineBreak === -1) return { lead: text };
    const lead = text.slice(0, lineBreak).trim();
    const detail = text.slice(lineBreak + 1).trim();
    if (!detail || !isSupplementaryList(detail)) return { lead: detail ? text : lead };
    return { lead, detail };
}

function isSupplementaryList(detail: string): boolean {
    return /^(?:同时将|同時將|以下)/.test(detail) || /^The following\b/i.test(detail) || /^[·•]/m.test(detail);
}

function dialogSubjectKicker(title: string): string | undefined {
    if (/任務/.test(title)) return '任務';
    if (/工作區/.test(title)) return '工作區';
    if (/任务/.test(title) || (/\btask\b/i.test(title) && !/[\u4e00-\u9fff]/.test(title))) {
        return /\btask\b/i.test(title) && !/[\u4e00-\u9fff]/.test(title) ? 'Task' : '任务';
    }
    if (/工作区/.test(title) || (/\bworkspace\b/i.test(title) && !/[\u4e00-\u9fff]/.test(title))) {
        return /\bworkspace\b/i.test(title) && !/[\u4e00-\u9fff]/.test(title) ? 'Workspace' : '工作区';
    }
    return undefined;
}

function DialogLead({ text }: { text: string }) {
    const parts = text.split(/(「[^」]*」|“[^”]*”|"[^"]*")/g);
    return (
        <>
            {parts.map((part, index) => {
                const quoted = part.match(/^「([^」]*)」$|^“([^”]*)”$|^"([^"]*)"$/);
                if (!quoted) return <Fragment key={index}>{part}</Fragment>;
                return <span key={index} className="custom-dialog__chip">{quoted[1] || quoted[2] || quoted[3]}</span>;
            })}
        </>
    );
}

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

    // Backend confirm prompts (cloud workspace conflict, steal, …) must use this
    // dialog — Wails MessageDialog falls back to the OS alert on Windows.
    useEffect(() => {
        const handler = async (data: {
            id?: string;
            title?: string;
            message?: string;
            confirmText?: string;
            cancelText?: string;
            confirmVariant?: 'primary' | 'danger';
        }) => {
            const id = typeof data?.id === 'string' ? data.id.trim() : '';
            if (!id) return;
            const confirmed = await showConfirm(String(data.message || ''), data.title, {
                confirmText: data.confirmText,
                cancelText: data.cancelText,
                confirmVariant: data.confirmVariant === 'danger' ? 'danger' : undefined,
            });
            try {
                await ResolveFrontendConfirm(id, confirmed);
            } catch {
                // Expired or already handled.
            }
        };
        const unsubscribe = EventsOn(EVENT_SHOW_CONFIRM, handler);
        return unsubscribe;
    }, [showConfirm]);

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
    const presentation = presentDialogMessage(state.message);
    const subjectKicker = presentation.subject ? dialogSubjectKicker(state.title) : undefined;
    const dialogApi = useMemo(
        () => ({ showAlert, showConfirm, showPrompt }),
        [showAlert, showConfirm, showPrompt],
    );

    return (
        <DialogContext.Provider value={dialogApi}>
            {children}
            {state.open && (
                <div
                    className="modal-backdrop custom-dialog-backdrop"
                    data-ai-theme={state.theme}
                    data-ai-dark-scheme={state.darkScheme}
                    data-ai-light-scheme={state.lightScheme}
                    style={{ zIndex: DIALOG_Z_INDEX }}
                    onMouseDown={e => { backdropMouseDownRef.current = e.target === e.currentTarget; }}
                    onClick={e => { if (e.target === e.currentTarget && backdropMouseDownRef.current) close(dismissResult); backdropMouseDownRef.current = false; }}
                >
                    <div
						className={`modal-content custom-dialog custom-dialog--${state.mode}${state.confirmVariant === 'danger' ? ' custom-dialog--danger' : ''}`}
						ref={dialogRef}
						role="dialog"
						aria-modal="true"
						aria-labelledby={state.title ? titleId : undefined}
						aria-label={state.title ? undefined : localizeText(state.lang, 'Dialog', '对话框')}
						aria-describedby={messageId}
						onClick={e => e.stopPropagation()}
					>
                        {state.title && (
                            <div className="modal-header custom-dialog__header">
                                <div className="custom-dialog__heading">
                                    <span className={`custom-dialog__mark custom-dialog__mark--${state.confirmVariant === 'danger' ? 'danger' : 'info'}`} aria-hidden="true">
                                        {state.confirmVariant === 'danger' ? (
                                            <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
                                                <path d="M10 2.4 17.6 16.4H2.4L10 2.4Z" stroke="currentColor" strokeWidth="1.6" strokeLinejoin="round" />
                                                <path d="M10 7.6v3.8" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
                                                <circle cx="10" cy="13.7" r="0.8" fill="currentColor" />
                                            </svg>
                                        ) : (
                                            <svg width="20" height="20" viewBox="0 0 20 20" fill="none">
                                                <circle cx="10" cy="10" r="7.2" stroke="currentColor" strokeWidth="1.6" />
                                                <path d="M10 9v4.2" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" />
                                                <circle cx="10" cy="6.5" r="0.8" fill="currentColor" />
                                            </svg>
                                        )}
                                    </span>
                                    <h3 id={titleId} className="custom-dialog__title">{state.title}</h3>
                                </div>
                                <button type="button" className="btn-close custom-dialog__close" aria-label={localizeText(state.lang, 'Close', '关闭')} onClick={() => close(dismissResult)}>
                                    <svg width="14" height="14" viewBox="0 0 14 14" aria-hidden="true">
                                        <path d="M3.2 3.2 10.8 10.8M10.8 3.2 3.2 10.8" stroke="currentColor" strokeWidth="1.4" strokeLinecap="round" />
                                    </svg>
                                </button>
                            </div>
                        )}
                        <div className="modal-body custom-dialog__body" id={messageId}>
                            {presentation.lead && (
                                <p className={/[？?]\s*$/.test(presentation.lead) ? 'custom-dialog__lead custom-dialog__lead--ask' : 'custom-dialog__lead'}>
                                    <DialogLead text={presentation.lead} />
                                </p>
                            )}
                            {presentation.subject && (
                                <div className="custom-dialog__subject">
                                    {subjectKicker && <span className="custom-dialog__subject-kicker">{subjectKicker}</span>}
                                    <p className="custom-dialog__subject-name">{presentation.subject}</p>
                                </div>
                            )}
                            {presentation.follow && (
                                <p className="custom-dialog__lead custom-dialog__follow">
                                    <DialogLead text={presentation.follow} />
                                </p>
                            )}
                            {presentation.detail && (
                                <p className="custom-dialog__detail">{presentation.detail}</p>
                            )}
                            {presentation.notice && (
                                <p className="custom-dialog__notice" role="note">{presentation.notice}</p>
                            )}
                            {state.mode === 'prompt' && (
                                <input
                                    ref={inputRef}
                                    className="form-input custom-dialog__input"
                                    type="text"
                                    value={inputValue}
                                    placeholder={state.placeholder || ''}
                                    onChange={e => setPromptInput(e.target.value)}
                                    autoComplete="off"
                                    spellCheck={false}
                                />
                            )}
                        </div>
                        <div className="modal-footer custom-dialog__footer">
                            {(state.mode === 'confirm' || state.mode === 'prompt') && (
                                <button type="button" className="btn-secondary" onClick={() => close(state.mode === 'prompt' ? null : false)}>
                                    {state.cancelText || localizeText(state.lang, 'Cancel', '取消')}
                                </button>
                            )}
                            <button
                                type="button"
                                className={state.confirmVariant === 'danger' ? 'btn-secondary btn-danger' : 'btn-primary'}
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
