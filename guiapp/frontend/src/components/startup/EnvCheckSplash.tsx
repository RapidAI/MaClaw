import { useEffect, useId, useRef, useState, type Ref } from 'react';
import { MaClawGuiMark } from './MaClawGuiMark';
import {
    ENV_RUNTIME_ORDER,
    EnvCheckBackdrop,
    EnvRuntimeIcon,
    envRuntimeLabelKeys,
    resolveRuntimeProgress,
} from './EnvCheckIcons';

const RUNTIME_STATE_KEY = {
    done: 'envCheckRuntimeDone',
    active: 'envCheckRuntimeActive',
    pending: 'envCheckRuntimePending',
    skipped: 'envCheckRuntimeSkipped',
} as const;

export type EnvCheckSplashProps = {
    themeMode: string;
    darkSchemeId?: string;
    lightSchemeId?: string;
    nativeRounded: boolean;
    useCSSWindowCorners: boolean;
    isLegacyWindowsFrameless: boolean;
    t: (key: string) => string;
    envLogs: string[];
    showLogs: boolean;
    isManualCheck: boolean;
    logEndRef: Ref<HTMLTextAreaElement>;
    onToggleLogs: () => void;
    onDismiss: () => void;
    onQuit: () => void;
};

export function EnvCheckSplash({
    themeMode,
    darkSchemeId,
    lightSchemeId,
    nativeRounded,
    useCSSWindowCorners,
    isLegacyWindowsFrameless,
    t,
    envLogs,
    showLogs,
    isManualCheck,
    logEndRef,
    onToggleLogs,
    onDismiss,
    onQuit,
}: EnvCheckSplashProps) {
    const [confirmQuit, setConfirmQuit] = useState(false);
    const cancelQuitRef = useRef<HTMLButtonElement>(null);
    const quitDialogRef = useRef<HTMLDivElement>(null);
    const contentRef = useRef<HTMLDivElement>(null);
    const titleId = useId();
    const quitTitleId = useId();
    const quitMessageId = useId();
    const status = envLogs.length > 0 ? envLogs[envLogs.length - 1] : t('envCheckPreparingStatus');
    const { active: activeRuntime, done: doneRuntimes, skipped: skippedRuntimes, percent } = resolveRuntimeProgress(envLogs);

    useEffect(() => {
        contentRef.current?.toggleAttribute('inert', confirmQuit);
    }, [confirmQuit]);

    useEffect(() => {
        if (!confirmQuit) return;
        const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
        cancelQuitRef.current?.focus();
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === 'Escape') {
                event.preventDefault();
                setConfirmQuit(false);
                return;
            }
            if (event.key !== 'Tab') return;
            const buttons = quitDialogRef.current?.querySelectorAll('button');
            if (!buttons || buttons.length === 0) return;
            const first = buttons[0];
            const last = buttons[buttons.length - 1];
            if (!(first instanceof HTMLElement) || !(last instanceof HTMLElement)) return;
            if (event.shiftKey && document.activeElement === first) {
                event.preventDefault();
                last.focus();
            } else if (!event.shiftKey && document.activeElement === last) {
                event.preventDefault();
                first.focus();
            }
        };
        window.addEventListener('keydown', onKeyDown, true);
        return () => {
            window.removeEventListener('keydown', onKeyDown, true);
            previous?.focus();
        };
    }, [confirmQuit]);

    const toggleLogs = () => {
        setConfirmQuit(false);
        onToggleLogs();
    };

    return (
        <div
            data-ai-theme={themeMode}
            data-ai-dark-scheme={themeMode === 'dark' ? darkSchemeId : undefined}
            data-ai-light-scheme={themeMode === 'light' ? lightSchemeId : undefined}
            data-native-rounded={nativeRounded ? 'true' : undefined}
            data-css-window-corners={useCSSWindowCorners ? 'true' : 'false'}
            data-windows-legacy-frameless={isLegacyWindowsFrameless ? 'true' : undefined}
            className="app-loading-shell"
            aria-busy="true"
        >
            <div className="app-loading-drag-zone" data-window-drag />
            <EnvCheckBackdrop className="app-loading-backdrop" />
            <div className={`app-loading-card${showLogs ? ' app-loading-card--logs' : ''}${confirmQuit ? ' app-loading-card--quit' : ''}`}>
                <div
                    ref={contentRef}
                    className="app-loading-content"
                    aria-hidden={confirmQuit || undefined}
                >
                    <div className="app-loading-hero">
                        <div className="app-loading-mascot">
                            <MaClawGuiMark className="app-loading-mascot__mark" title="MaClaw" />
                        </div>
                        <div className="app-loading-copy">
                            <h1 id={titleId} className="app-loading-title">{t('envCheckTitle')}</h1>
                            <div className="app-loading-prepare">
                                {showLogs ? null : <p className="app-loading-subtitle">{t('envCheckSubtitle')}</p>}
                                <div
                                    className="app-loading-progress"
                                    role="progressbar"
                                    aria-valuemin={0}
                                    aria-valuemax={100}
                                    aria-valuenow={percent}
                                    aria-valuetext={status}
                                    aria-labelledby={titleId}
                                >
                                    <div className="app-loading-progress__bar" style={{ width: `${percent}%` }} />
                                </div>
                                <div className="app-loading-status" role="status" aria-live="polite">
                                    {status}
                                </div>
                                <div className="app-loading-actions">
                                    <button type="button" onClick={toggleLogs} className="app-loading-link">
                                        {showLogs ? t('envCheckHideDetails') : t('envCheckShowDetails')}
                                    </button>
                                    {showLogs && (
                                        isManualCheck ? (
                                            <button
                                                type="button"
                                                onClick={onDismiss}
                                                className="btn-hide app-loading-action app-loading-action--primary"
                                            >
                                                {t('envCheckDismiss')}
                                            </button>
                                        ) : (
                                            <button
                                                type="button"
                                                onClick={() => setConfirmQuit(true)}
                                                className="btn-hide app-loading-action app-loading-action--danger"
                                            >
                                                {t('envCheckQuit')}
                                            </button>
                                        )
                                    )}
                                </div>
                            </div>
                        </div>
                    </div>
                    {showLogs ? (
                        <textarea
                            ref={logEndRef}
                            readOnly
                            value={envLogs.join('\n')}
                            className="app-loading-log"
                            aria-label={t('envCheckLogLabel')}
                        />
                    ) : null}
                    <ul
                        className="app-loading-runtimes"
                        hidden={showLogs}
                        aria-label={t('envCheckRuntimesLabel')}
                    >
                        {ENV_RUNTIME_ORDER.map((id) => {
                            const labels = envRuntimeLabelKeys(id);
                            const active = activeRuntime === id;
                            const done = doneRuntimes.has(id);
                            const skipped = skippedRuntimes.has(id);
                            const state = done ? 'done' : skipped ? 'skipped' : active ? 'active' : 'pending';
                            return (
                                <li
                                    key={id}
                                    className={`app-loading-runtime app-loading-runtime--${id}${active ? ' is-active' : ''}${done ? ' is-done' : ''}${skipped ? ' is-skipped' : ''}`}
                                    aria-current={active ? 'step' : undefined}
                                >
                                    <EnvRuntimeIcon id={id} className="app-loading-runtime__icon" />
                                    <span className="app-loading-runtime__title">{t(labels.title)}</span>
                                    <span className="app-loading-runtime__desc">{t(labels.desc)}</span>
                                    <span className={`app-loading-runtime__state app-loading-runtime__state--${state}`}>
                                        {t(RUNTIME_STATE_KEY[state])}
                                    </span>
                                </li>
                            );
                        })}
                    </ul>
                </div>
                {confirmQuit ? (
                    <div
                        ref={quitDialogRef}
                        className="app-loading-quit"
                        role="alertdialog"
                        aria-modal="true"
                        aria-labelledby={quitTitleId}
                        aria-describedby={quitMessageId}
                    >
                        <p id={quitTitleId} className="app-loading-quit__title">{t('envCheckExitWarningTitle')}</p>
                        <p id={quitMessageId} className="app-loading-quit__message">{t('envCheckExitWarningMessage')}</p>
                        <div className="app-loading-actions">
                            <button
                                ref={cancelQuitRef}
                                type="button"
                                onClick={() => setConfirmQuit(false)}
                                className="btn-hide app-loading-action app-loading-action--primary"
                            >
                                {t('envCheckExitCancel')}
                            </button>
                            <button type="button" onClick={onQuit} className="btn-hide app-loading-action app-loading-action--danger">
                                {t('envCheckExitConfirm')}
                            </button>
                        </div>
                    </div>
                ) : null}
            </div>
        </div>
    );
}
