import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent, type ReactNode } from 'react';
import { InputHistoryAutocomplete } from '../ai/InputHistoryAutocomplete';
import { useAssistantInputHistory } from '../ai/useAssistantInputHistory';
import { useInputHistoryAutocomplete } from '../ai/useInputHistoryAutocomplete';
import { useTextCompositionGuard } from '../ai/useTextCompositionGuard';
import { AssistantInputIcon, lightTheme } from '../ai/aiAssistantPanelTheme';
import { VoiceLevelVisualizer } from '../ai/aiAssistantControls';
import { useAIAssistantVoiceControls } from '../ai/useAIAssistantVoiceControls';
import { useVoiceInput } from '../ai/useVoiceInput';
import { shouldDispatchASRText } from '../ai/asrTextUtils';
import { LoadConfig } from '../../../wailsjs/go/main/App';
import { EventsOn } from '../../../wailsjs/runtime';
import { localizeText } from '../../i18n';
import { botInputHistory, rememberBotInput } from './desktopBotInputHistory';
import { filesFromTransfer } from './desktopBotMaterials';

interface DesktopBotComposerProps {
    userId: string;
    botId: string;
    lang: string;
    value: string;
    onChange: (next: string) => void;
    onSubmit: () => void;
    disabled?: boolean;
    placeholder: string;
    ariaLabel: string;
    sendLabel: string;
    // Attached materials: shown at the top of the form. Files dropped or
    // pasted onto the form go to onFiles. readyWithoutText lets the files
    // go alone; holdSend keeps the send off while one still uploads.
    leading?: ReactNode;
    onFiles?: (files: File[]) => void;
    readyWithoutText?: boolean;
    holdSend?: boolean;
    /** Voice input: a finished ASR segment is handed back for direct send,
     *  matching the assistant panel (voice never edits the draft). When
     *  absent the mic button is hidden. */
    onVoiceText?: (text: string) => void;
}

// The popup borrows the assistant list. These colors follow the bot surface
// tokens so the list does not pick up the assistant panel's own theme.
const historyTheme = {
    ...lightTheme,
    bg: 'var(--theme-surface, #fff)',
    text: 'var(--theme-text-primary, #1d2939)',
    textMuted: 'var(--theme-text-muted, #98a2b3)',
    inputBarBg: 'var(--theme-surface, #fff)',
    inputBarBorder: 'var(--theme-border, #e6e6e8)',
    fieldBorder: 'var(--theme-border, #e6e6e8)',
    btnColor: 'var(--mc-accent, #2f6fbc)',
};

export function DesktopBotComposer({
    userId,
    botId,
    lang,
    value,
    onChange,
    onSubmit,
    disabled = false,
    placeholder,
    ariaLabel,
    sendLabel,
    leading,
    onFiles,
    readyWithoutText = false,
    holdSend = false,
    onVoiceText,
}: DesktopBotComposerProps) {
    const [dragging, setDragging] = useState(false);
    const inputRef = useRef<HTMLTextAreaElement | null>(null);
    const textComposition = useTextCompositionGuard();
    const [isComposing, setIsComposing] = useState(false);
    const [history, setHistory] = useState<string[]>(() => botInputHistory(userId));
    const seenScope = useRef({ userId, botId });
    const exitHistoryRef = useRef<() => boolean>(() => false);
    const listboxDomId = useId();
    const historyListboxId = `desktop-bot-input-history-${listboxDomId}`;

    const applyInputValue = useCallback((next: string) => {
        onChange(next);
        requestAnimationFrame(() => {
            const el = inputRef.current;
            if (!el) return;
            el.focus();
            el.setSelectionRange(next.length, next.length);
        });
    }, [onChange]);

    const { exitHistoryBrowsing, isSelectionCollapsedAtBoundary, recallHistory, rememberHistoryEdit, resetHistoryBrowsing } = useAssistantInputHistory({
        applyInputValue,
        inputRef,
        inputValue: value,
        submittedPrompts: history,
    });

    exitHistoryRef.current = exitHistoryBrowsing;
    // Mount already read this account. A later account loads its own cache.
    // Switching bots leaves the ↑↓ walk and puts back the draft from before it,
    // so the recalled line is not stuck in the other bot's box.
    useEffect(() => {
        const prev = seenScope.current;
        const userChanged = prev.userId !== userId;
        const botChanged = prev.botId !== botId;
        seenScope.current = { userId, botId };
        if (!userChanged && !botChanged) return;
        if (userChanged) setHistory(botInputHistory(userId));
        if (!exitHistoryRef.current()) resetHistoryBrowsing();
    }, [botId, resetHistoryBrowsing, userId]);

    const applyAutocompleteValue = useCallback((next: string) => {
        rememberHistoryEdit(next);
        onChange(next);
    }, [onChange, rememberHistoryEdit]);

    const autocomplete = useInputHistoryAutocomplete({
        inputValue: value,
        submittedPrompts: history,
        applyInputValue: applyAutocompleteValue,
        inputRef,
        disabled: isComposing,
    });

    // Voice input shares the assistant's ASR pipeline but never the pet: no
    // pet-state signals, no "didn't hear" spoken retry, no global start event
    // (the assistant panel stays mounted and would double-open the mic), and
    // no command filter — the bot sends every recognized utterance, not only
    // speech the normalizer classifies as a command.
    const [voiceDeviceId, setVoiceDeviceId] = useState('');
    const voiceEnabled = !!onVoiceText;
    useEffect(() => {
        // Gate on the boolean, not onVoiceText — the workspace passes a fresh
        // closure each render, which would re-run this effect (LoadConfig +
        // event resubscribe) on every chat repaint.
        if (!voiceEnabled) return;
        let live = true;
        const load = () => {
            LoadConfig().then((cfg) => {
                const id = cfg?.audio_input_device_id;
                if (live && typeof id === 'string') setVoiceDeviceId(id);
            }).catch(() => {});
        };
        load();
        // Pick up a mic-device change made in settings while this page is open.
        const offChanged = EventsOn('config-changed', load);
        const offUpdated = EventsOn('config-updated', load);
        return () => {
            live = false;
            if (typeof offChanged === 'function') offChanged();
            if (typeof offUpdated === 'function') offUpdated();
        };
    }, [voiceEnabled]);
    const voiceInput = useVoiceInput(
        useCallback((spoken: string) => {
            // Same guards as submitTyped: never dispatch while the composer is
            // disabled or a material upload is still in flight (holdSend).
            if (!shouldDispatchASRText(spoken) || disabled || holdSend) return;
            onVoiceText?.(spoken);
        }, [onVoiceText, disabled, holdSend]),
        voiceDeviceId,
        { petIntegration: false, externalStartEvent: false, commandFilter: false },
    );
    const { finishVoicePointer, handleVoiceClick, handleVoicePointerDown, handleVoicePointerLeave } = useAIAssistantVoiceControls({
        inputRef,
        petFocusInputSeq: 0,
        petVoiceStartSeq: 0,
        ready: !disabled,
        voiceInput,
    });
    const voiceDisabled = disabled || voiceInput.state === 'transcribing' || !voiceInput.asrReady;

    const submitTyped = () => {
        const text = value.trim();
        if ((!text && !readyWithoutText) || disabled || holdSend) return;
        if (text) setHistory(rememberBotInput(userId, text));
        resetHistoryBrowsing();
        onSubmit();
    };

    const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (textComposition.shouldIgnoreKeyDown(event)) return;
        if (autocomplete.handleKeyDown(event)) return;
        if (event.key === 'ArrowUp' && isSelectionCollapsedAtBoundary('up')) {
            if (recallHistory('up')) {
                event.preventDefault();
                return;
            }
        }
        if (event.key === 'ArrowDown' && isSelectionCollapsedAtBoundary('down')) {
            if (recallHistory('down')) {
                event.preventDefault();
                return;
            }
        }
        if (event.key === 'Escape') {
            if (exitHistoryBrowsing()) event.preventDefault();
            return;
        }
        // Shift+Enter stays a newline. Any other Enter sends, including Ctrl+Enter.
        if (event.key === 'Enter' && !event.shiftKey) {
            event.preventDefault();
            submitTyped();
        }
    };

    return (
        <form
            className={`desktop-bot-chat__form${dragging ? ' is-dragging' : ''}`}
            onSubmit={event => {
                event.preventDefault();
                submitTyped();
            }}
            onDragOver={onFiles ? event => { if (Array.from(event.dataTransfer?.types || []).includes('Files')) { event.preventDefault(); setDragging(true); } } : undefined}
            onDragLeave={onFiles ? () => setDragging(false) : undefined}
            onDrop={onFiles ? event => {
                setDragging(false);
                const files = filesFromTransfer(event.dataTransfer);
                if (files.length === 0) return;
                event.preventDefault();
                onFiles(files);
            } : undefined}
            onPasteCapture={onFiles ? event => {
                const files = filesFromTransfer(event.clipboardData);
                if (files.length === 0) return;
                event.preventDefault();
                onFiles(files);
            } : undefined}
        >
            {leading}
            <InputHistoryAutocomplete
                open={autocomplete.open}
                matches={autocomplete.matches}
                selectedIndex={autocomplete.selectedIndex}
                prefix={value}
                listboxId={historyListboxId}
                onSelectIndex={autocomplete.setSelectedIndex}
                onAccept={autocomplete.accept}
                theme={historyTheme}
                lang={lang}
                testId="desktop-bot-input-history"
                itemTestIdPrefix="desktop-bot-input-history-item"
            />
            <textarea
                ref={inputRef}
                data-testid="desktop-bot-command"
                aria-label={ariaLabel}
                aria-autocomplete="list"
                aria-expanded={autocomplete.open}
                aria-controls={autocomplete.open ? historyListboxId : undefined}
                aria-activedescendant={autocomplete.open ? `${historyListboxId}-option-${autocomplete.selectedIndex}` : undefined}
                placeholder={placeholder}
                value={value}
                onChange={event => {
                    rememberHistoryEdit(event.target.value);
                    onChange(event.target.value);
                }}
                onCompositionStart={() => {
                    setIsComposing(true);
                    textComposition.onCompositionStart();
                }}
                onCompositionEnd={() => {
                    setIsComposing(false);
                    textComposition.onCompositionEnd();
                }}
                onKeyDown={onKeyDown}
            />
            {onVoiceText ? (
                <button
                    type="button"
                    className={`desktop-bot-chat__voice${voiceInput.state === 'listening' ? ' is-listening' : ''}`}
                    data-testid="desktop-bot-voice-input"
                    onClick={handleVoiceClick}
                    onPointerDown={handleVoicePointerDown}
                    onPointerUp={finishVoicePointer}
                    onPointerCancel={finishVoicePointer}
                    onPointerLeave={handleVoicePointerLeave}
                    onContextMenu={event => event.preventDefault()}
                    disabled={voiceDisabled}
                    title={!voiceInput.asrReady
                        ? localizeText(lang, 'Voice input unavailable - enable the ASR model in settings first', '语音输入不可用，请先在设置中启用 ASR 模型', '語音輸入不可用，請先在設定中啟用 ASR 模型')
                        : voiceInput.state === 'listening'
                            ? localizeText(lang, 'Listening - click to stop', '监听中，点击停止', '監聽中，點擊停止')
                            : voiceInput.state === 'transcribing'
                                ? localizeText(lang, 'Transcribing...', '识别中…', '識別中…')
                                : localizeText(lang, 'Voice input', '语音输入', '語音輸入')}
                    aria-label={localizeText(lang, 'Voice input', '语音输入', '語音輸入')}
                >
                    {voiceInput.state === 'transcribing' ? (
                        <span aria-hidden="true" className="desktop-bot-chat__voice-spinner" />
                    ) : voiceInput.state === 'listening' ? (
                        <VoiceLevelVisualizer
                            onAudioLevelRef={voiceInput.onAudioLevelRef}
                            isSpeaking={voiceInput.isSpeaking}
                            themeColor="var(--theme-text-secondary, #475569)"
                            speakingColor="var(--mc-accent, #2f6fbc)"
                        />
                    ) : (
                        <AssistantInputIcon name="mic" size={14} />
                    )}
                </button>
            ) : null}
            {onVoiceText && voiceInput.error ? (
                <span className="desktop-bot-chat__voice-error" role="status" title={voiceInput.error}>{voiceInput.error}</span>
            ) : null}
            <button type="submit" className="desktop-bot-chat__send" data-testid="desktop-bot-send" disabled={(!value.trim() && !readyWithoutText) || disabled || holdSend}>{sendLabel}</button>
        </form>
    );
}
