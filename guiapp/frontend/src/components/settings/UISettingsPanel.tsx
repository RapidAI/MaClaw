import { GetChatFontFamily, GetCodingFontFamily, SetChatFontFamily, SetCodingFontFamily, SetUIZoomFactor } from '../../../wailsjs/go/main/App';
import { type Dispatch, type SetStateAction, useEffect, useRef } from 'react';
import { corelib, main } from '../../../wailsjs/go/models';
import {
    ASSISTANT_FONT_SYSTEM,
    assistantFontOptions,
    chatFontFamilySaveQueue,
    CODING_FONT_SAMPLE,
    codingFontFamilySaveQueue,
    CONVERSATION_FONT_SAMPLE,
    claimSerialAttempt,
    enqueueSerial,
    markSerialAttemptStarted,
    normalizeAssistantFontId,
    type AssistantFontAudience,
    type SerialQueue,
} from '../ai/assistantFonts';
import { localizeText } from '../../i18n';
import {
    clampUIScale,
    recommendUIScale,
    UI_SCALE_AUTO,
    uiScaleEquals,
    uiScaleToPercent,
} from '../../utils/uiScale';
import { useChatFontSizeCommit, type ChatFontSizeDraftRef } from './useChatFontSizeCommit';
import { assistantDarkSchemes, type AssistantDarkSchemeId } from '../ai/assistantDarkSchemes';
import { assistantLightSchemes, type AssistantLightSchemeId } from '../ai/assistantLightSchemes';

type UISettingsPanelProps = {
    config: corelib.AppConfig | null;
    lang: string;
    t: (key: string) => string;
    uiZoom: number;
    setUiZoom: Dispatch<SetStateAction<number>>;
    /** When true, scale follows DPI/resolution recommendation. */
    uiZoomAuto: boolean;
    setUiZoomAuto: Dispatch<SetStateAction<boolean>>;
    /** Auto policy supplied by the native shell (Win10 frameless WebView2). */
    disableAutoUIScaleTransform?: boolean;
    chatFontSize: number;
    setChatFontSize: Dispatch<SetStateAction<number>>;
    /** Unsaved size. Config snapshots that differ from it must not move the slider. */
    chatFontSizeDraftRef?: ChatFontSizeDraftRef;
    /** Unsaved conversation face. A full config echo must not clear the card. */
    chatFontFamilyDraftRef?: { current: string | null };
    /** Unsaved programming face. A full config echo must not clear the card. */
    codingFontFamilyDraftRef?: { current: string | null };
    /** Conversation-assistant face. Empty or omitted keeps the current font. */
    chatFontFamily?: string;
    setChatFontFamily?: (fontId: string) => void;
    /** Programming-assistant face. Empty or omitted keeps the current font. */
    codingFontFamily?: string;
    setCodingFontFamily?: (fontId: string) => void;
    darkSchemeId: AssistantDarkSchemeId;
    setDarkSchemeId: (schemeId: AssistantDarkSchemeId) => void;
    lightSchemeId: AssistantLightSchemeId;
    setLightSchemeId: (schemeId: AssistantLightSchemeId) => void;
};

const textForLang = localizeText;
const UI_ZOOM_PERSIST_DEBOUNCE_MS = 280;

function settleFontDraft(draftRef: { current: string | null } | undefined, fontId: string) {
    if (draftRef && draftRef.current === fontId) draftRef.current = null;
}

function publishAssistantFont(
    fontId: string,
    draftRef: { current: string | null } | undefined,
    setFont: ((fontId: string) => void) | undefined,
    save: (fontId: string) => Promise<void>,
    readStored: () => Promise<string>,
    queue: SerialQueue,
) {
    if (draftRef) draftRef.current = fontId;
    setFont?.(fontId);
    // The draft id is not an attempt. Choosing the same face again must not be
    // settled or skipped by the save that is already in flight.
    const attempt = claimSerialAttempt(queue);
    enqueueSerial(queue, () => {
        if (queue.attempt !== attempt) return;
        markSerialAttemptStarted(queue, attempt);
        return save(fontId).then(() => {
            if (queue.attempt !== attempt) return;
            settleFontDraft(draftRef, fontId);
        }, () => {
            // A failed save emits no config snapshot. Read the stored id back
            // when this attempt is still newest. A failed read leaves the card
            // the user chose, so the same card can be clicked again.
            if (queue.attempt !== attempt) return;
            return readStored().then((stored) => {
                if (queue.attempt !== attempt) return;
                settleFontDraft(draftRef, fontId);
                if (stored !== fontId) setFont?.(stored);
            }, () => {
                if (queue.attempt !== attempt) return;
                settleFontDraft(draftRef, fontId);
            });
        });
    });
}

function AssistantFontChoices({
    audience,
    lang,
    selected,
    onSelect,
}: {
    audience: AssistantFontAudience;
    lang: string;
    selected: string;
    onSelect: (fontId: string) => void;
}) {
    const sample = audience === "coding" ? CODING_FONT_SAMPLE : CONVERSATION_FONT_SAMPLE;
    const legend = audience === "coding"
        ? textForLang(lang, "Coding Font", "编程助手字体", "編程助手字體")
        : textForLang(lang, "Conversation Font", "会话助手字体", "會話助手字體");
    return (
        <div className="ui-font-grid" role="radiogroup" aria-label={legend}>
            {assistantFontOptions(audience).map((option) => {
                const isSelected = normalizeAssistantFontId(audience, selected) === option.id;
                return (
                    <label
                        key={option.id}
                        className="ui-font-card"
                        data-selected={isSelected ? "true" : undefined}
                        data-testid={`assistant-font-${audience}-${option.id}`}
                    >
                        <input
                            type="radio"
                            name={`assistant-font-${audience}`}
                            checked={isSelected}
                            onChange={() => onSelect(option.id)}
                            onClick={() => {
                                // change does not fire when this card is already selected.
                                // A failed save leaves it selected, so the same choice must retry.
                                if (isSelected) onSelect(option.id);
                            }}
                        />
                        <span className="ui-font-card__title">
                            {textForLang(lang, option.label.en, option.label.zhHans, option.label.zhHant)}
                        </span>
                        <span className="ui-font-card__desc">
                            {textForLang(lang, option.description.en, option.description.zhHans, option.description.zhHant)}
                        </span>
                        <span className="ui-font-card__sample" style={{ fontFamily: option.contentFamily }}>
                            {sample}
                        </span>
                    </label>
                );
            })}
        </div>
    );
}

export const UISettingsPanel = ({
    config,
    lang,
    t,
    uiZoom,
    setUiZoom,
    uiZoomAuto,
    setUiZoomAuto,
    disableAutoUIScaleTransform = false,
    chatFontSize,
    setChatFontSize,
    chatFontSizeDraftRef,
    chatFontFamilyDraftRef,
    codingFontFamilyDraftRef,
    chatFontFamily = ASSISTANT_FONT_SYSTEM,
    setChatFontFamily,
    codingFontFamily = ASSISTANT_FONT_SYSTEM,
    setCodingFontFamily,
    darkSchemeId,
    setDarkSchemeId,
    lightSchemeId,
    setLightSchemeId,
}: UISettingsPanelProps) => {
    const zoomPercent = uiScaleToPercent(uiZoom);
    const zoomLabel = uiZoomAuto
        ? textForLang(
            lang,
            `Auto (${zoomPercent}%)`,
            `\u81ea\u52a8 (${zoomPercent}%)`,
            `\u81ea\u52d5 (${zoomPercent}%)`,
        )
        : `${zoomPercent}%`;

    // Last value written to config: number = manual scale, UI_SCALE_AUTO = Auto mode.
    // Avoids redundant writes and the blur→Auto race (blur must not clobber Auto).
    const lastPersistedRef = useRef<number>(uiZoomAuto ? UI_SCALE_AUTO : uiZoom);
    const persistTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    useEffect(() => () => {
        if (persistTimerRef.current != null) {
            clearTimeout(persistTimerRef.current);
        }
    }, []);

    const clearPersistTimer = () => {
        if (persistTimerRef.current != null) {
            clearTimeout(persistTimerRef.current);
            persistTimerRef.current = null;
        }
    };

    const persistManualUIZoom = (rawPercent: number, opts?: { debounce?: boolean }) => {
        const v = clampUIScale(rawPercent / 100);
        setUiZoomAuto(false);
        setUiZoom(v);

        const write = () => {
            if (lastPersistedRef.current !== UI_SCALE_AUTO && uiScaleEquals(lastPersistedRef.current, v)) {
                return;
            }
            lastPersistedRef.current = v;
            void SetUIZoomFactor(v).catch(() => {});
        };

        clearPersistTimer();
        if (opts?.debounce) {
            persistTimerRef.current = setTimeout(write, UI_ZOOM_PERSIST_DEBOUNCE_MS);
        } else {
            write();
        }
        return v;
    };

    const restoreAutoUIZoom = () => {
        clearPersistTimer();
        const auto = recommendUIScale({ disableAutoTransform: disableAutoUIScaleTransform });
        lastPersistedRef.current = UI_SCALE_AUTO;
        setUiZoomAuto(true);
        setUiZoom(auto);
        void SetUIZoomFactor(UI_SCALE_AUTO).catch(() => {});
    };
    const { commit: commitChatFontSize, rangeHandlers: chatFontSizeHandlers } = useChatFontSizeCommit(chatFontSize, setChatFontSize, chatFontSizeDraftRef);

    return (
    <div className="settings-panel ui-settings-panel">
        <section className="ui-settings-card ui-settings-card--themes">
            <h4>
                {textForLang(lang, 'Dark Mode Palette', '\u6697\u9ed1\u6a21\u5f0f\u914d\u8272', '\u6697\u9ed1\u6a21\u5f0f\u914d\u8272')}
            </h4>
            <div className="ui-dark-scheme-grid" role="radiogroup" aria-label={textForLang(lang, 'Dark Mode Palette', '\u6697\u9ed1\u6a21\u5f0f\u914d\u8272', '\u6697\u9ed1\u6a21\u5f0f\u914d\u8272')}>
                {assistantDarkSchemes.map((scheme) => {
                    const selected = darkSchemeId === scheme.id;
                    return (
                        <label
                            key={scheme.id}
                            className="ui-dark-scheme-card"
                            data-selected={selected ? 'true' : undefined}
                            style={{
                                ['--scheme-page-bg' as any]: scheme.cssVars.pageBg,
                                ['--scheme-surface' as any]: scheme.cssVars.surface,
                                ['--scheme-surface-muted' as any]: scheme.cssVars.surfaceMuted,
                                ['--scheme-primary' as any]: scheme.cssVars.primary,
                                ['--scheme-primary-strong' as any]: scheme.cssVars.primaryStrong,
                                ['--scheme-text-primary' as any]: scheme.cssVars.textPrimary,
                                ['--scheme-text-secondary' as any]: scheme.cssVars.textSecondary,
                                ['--scheme-border' as any]: scheme.cssVars.border,
                                ['--scheme-success' as any]: scheme.cssVars.success,
                            }}
                        >
                            <input
                                type="radio"
                                name="ai-dark-scheme"
                                checked={selected}
                                onChange={() => setDarkSchemeId(scheme.id)}
                            />
                            <span className="ui-dark-scheme-copy">
                                <span className="ui-dark-scheme-title">
                                    {textForLang(lang, scheme.label.en, scheme.label.zhHans, scheme.label.zhHant)}
                                </span>
                                <span className="ui-dark-scheme-desc">
                                    {textForLang(lang, scheme.description.en, scheme.description.zhHans, scheme.description.zhHant)}
                                </span>
                            </span>
                            <span className="ui-dark-scheme-preview" aria-hidden="true">
                                <span className="ui-dark-scheme-preview__rail">
                                    <span />
                                    <span />
                                    <span />
                                </span>
                                <span className="ui-dark-scheme-preview__panel">
                                    <span className="ui-dark-scheme-preview__topline" />
                                    <span className="ui-dark-scheme-preview__row" />
                                    <span className="ui-dark-scheme-preview__row ui-dark-scheme-preview__row--short" />
                                    <span className="ui-dark-scheme-preview__chips">
                                        <span />
                                        <span />
                                    </span>
                                </span>
                            </span>
                        </label>
                    );
                })}
            </div>
            <p>
                {textForLang(lang, 'When the AI assistant title-bar switch enters dark mode, it uses the selected palette for the app shell and assistant panel.', '\u5f53 AI \u52a9\u624b\u9876\u90e8\u6697\u9ed1/\u666e\u901a\u5207\u6362\u8fdb\u5165\u6697\u9ed1\u6a21\u5f0f\u65f6\uff0c\u5c06\u4f7f\u7528\u8fd9\u91cc\u9009\u4e2d\u7684\u914d\u8272\u65b9\u6848\u3002', '\u7576 AI \u52a9\u624b\u9802\u90e8\u6697\u9ed1/\u666e\u901a\u5207\u63db\u9032\u5165\u6697\u9ed1\u6a21\u5f0f\u6642\uff0c\u5c07\u4f7f\u7528\u9019\u88e1\u9078\u4e2d\u7684\u914d\u8272\u65b9\u6848\u3002')}
            </p>
        </section>

        <section className="ui-settings-card ui-settings-card--themes">
            <h4>
                {textForLang(lang, 'Light Mode Palette', '\u666e\u901a\u6a21\u5f0f\u914d\u8272', '\u666e\u901a\u6a21\u5f0f\u914d\u8272')}
            </h4>
            <div className="ui-dark-scheme-grid" role="radiogroup" aria-label={textForLang(lang, 'Light Mode Palette', '\u666e\u901a\u6a21\u5f0f\u914d\u8272', '\u666e\u901a\u6a21\u5f0f\u914d\u8272')}>
                {assistantLightSchemes.map((scheme) => {
                    const selected = lightSchemeId === scheme.id;
                    return (
                        <label
                            key={scheme.id}
                            className="ui-dark-scheme-card"
                            data-selected={selected ? 'true' : undefined}
                            style={{
                                ['--scheme-page-bg' as any]: scheme.cssVars.pageBg,
                                ['--scheme-surface' as any]: scheme.cssVars.surface,
                                ['--scheme-surface-muted' as any]: scheme.cssVars.surfaceMuted,
                                ['--scheme-primary' as any]: scheme.cssVars.primary,
                                ['--scheme-primary-strong' as any]: scheme.cssVars.primaryStrong,
                                ['--scheme-text-primary' as any]: scheme.cssVars.textPrimary,
                                ['--scheme-text-secondary' as any]: scheme.cssVars.textSecondary,
                                ['--scheme-border' as any]: scheme.cssVars.border,
                                ['--scheme-success' as any]: scheme.cssVars.success,
                            }}
                        >
                            <input
                                type="radio"
                                name="ai-light-scheme"
                                checked={selected}
                                onChange={() => setLightSchemeId(scheme.id)}
                            />
                            <span className="ui-dark-scheme-copy">
                                <span className="ui-dark-scheme-title">
                                    {textForLang(lang, scheme.label.en, scheme.label.zhHans, scheme.label.zhHant)}
                                </span>
                                <span className="ui-dark-scheme-desc">
                                    {textForLang(lang, scheme.description.en, scheme.description.zhHans, scheme.description.zhHant)}
                                </span>
                            </span>
                            <span className="ui-dark-scheme-preview" aria-hidden="true">
                                <span className="ui-dark-scheme-preview__rail">
                                    <span />
                                    <span />
                                    <span />
                                </span>
                                <span className="ui-dark-scheme-preview__panel">
                                    <span className="ui-dark-scheme-preview__topline" />
                                    <span className="ui-dark-scheme-preview__row" />
                                    <span className="ui-dark-scheme-preview__row ui-dark-scheme-preview__row--short" />
                                    <span className="ui-dark-scheme-preview__chips">
                                        <span />
                                        <span />
                                    </span>
                                </span>
                            </span>
                        </label>
                    );
                })}
            </div>
            <p>
                {textForLang(lang, 'When the AI assistant is in light mode, it uses the selected palette for the app shell and assistant panel.', '\u5f53 AI \u52a9\u624b\u5904\u4e8e\u666e\u901a\u6a21\u5f0f\u65f6\uff0c\u5c06\u4f7f\u7528\u8fd9\u91cc\u9009\u4e2d\u7684\u914d\u8272\u65b9\u6848\u3002', '\u7576 AI \u52a9\u624b\u8655\u65bc\u666e\u901a\u6a21\u5f0f\u6642\uff0c\u5c07\u4f7f\u7528\u9019\u88e1\u9078\u4e2d\u7684\u914d\u8272\u65b9\u6848\u3002')}
            </p>
        </section>

        <section className="ui-settings-card">
            <h4>
                {textForLang(lang, 'UI Zoom', '\u754c\u9762\u7f29\u653e', '\u4ecb\u9762\u7e2e\u653e')}
            </h4>
            <div className="ui-settings-slider-row">
                <input
                    type="range"
                    min={50}
                    max={200}
                    step={5}
                    value={zoomPercent}
                    onChange={(e) => {
                        // Live preview + debounced persist (covers drag-release outside thumb).
                        // No onBlur: focusing "Auto" would otherwise re-write manual and race Auto(0).
                        persistManualUIZoom(Number(e.target.value), { debounce: true });
                    }}
                    onPointerUp={(e) => {
                        persistManualUIZoom(Number((e.currentTarget as HTMLInputElement).value));
                    }}
                    onKeyUp={(e) => {
                        if (
                            e.key !== 'ArrowLeft'
                            && e.key !== 'ArrowRight'
                            && e.key !== 'Home'
                            && e.key !== 'End'
                            && e.key !== 'PageUp'
                            && e.key !== 'PageDown'
                        ) {
                            return;
                        }
                        persistManualUIZoom(Number((e.currentTarget as HTMLInputElement).value));
                    }}
                    aria-label={textForLang(lang, 'UI Zoom', '\u754c\u9762\u7f29\u653e', '\u4ecb\u9762\u7e2e\u653e')}
                />
                <span className="ui-settings-value">{zoomLabel}</span>
                <button
                    type="button"
                    onClick={restoreAutoUIZoom}
                >
                    {textForLang(lang, 'Auto', '\u81ea\u52a8', '\u81ea\u52d5')}
                </button>
            </div>
            <p>
                {textForLang(
                    lang,
                    'Default is Auto: scale follows display DPI and resolution so low-res screens stay readable and HiDPI is not double-scaled. Drag the slider to override.',
                    '\u9ed8\u8ba4\u4e3a\u81ea\u52a8\uff1a\u6839\u636e\u5c4f\u5e55 DPI \u4e0e\u5206\u8fa8\u7387\u8c03\u6574\u7f29\u653e\uff0c\u4f4e\u5206\u5c4f\u66f4\u6613\u8bfb\u3001\u9ad8 DPI \u4e0d\u4f1a\u91cd\u590d\u653e\u5927\u3002\u62d6\u52a8\u6ed1\u5757\u53ef\u624b\u52a8\u8986\u76d6\u3002',
                    '\u9810\u8a2d\u70ba\u81ea\u52d5\uff1a\u6839\u64da\u87a2\u5e55 DPI \u8207\u5206\u8fa8\u7387\u8abf\u6574\u7e2e\u653e\uff0c\u4f4e\u5206\u5c4f\u66f4\u6613\u8b80\u3001\u9ad8 DPI \u4e0d\u6703\u91cd\u8907\u653e\u5927\u3002\u62d6\u52d5\u6ed1\u687f\u53ef\u624b\u52d5\u8986\u84cb\u3002',
                )}
            </p>
        </section>

        <section className="ui-settings-card">
            <h4>
                {textForLang(lang, 'AI Assistant Font Size', 'AI \u52a9\u624b\u9762\u677f\u5b57\u53f7', 'AI \u52a9\u624b\u9762\u677f\u5b57\u865f')}
            </h4>
            <div className="ui-settings-slider-row">
                <input
                    type="range"
                    min={12}
                    max={24}
                    step={1}
                    value={chatFontSize}
                    {...chatFontSizeHandlers}
                    aria-label={textForLang(lang, 'AI Assistant Font Size', 'AI \u52a9\u624b\u9762\u677f\u5b57\u53f7', 'AI \u52a9\u624b\u9762\u677f\u5b57\u865f')}
                />
                <span className="ui-settings-value">{chatFontSize}px</span>
                <button
                    type="button"
                    onClick={() => commitChatFontSize(14)}
                >
                    {textForLang(lang, 'Reset', '\u91cd\u7f6e', '\u91cd\u7f6e')}
                </button>
            </div>
            <p>
                {textForLang(lang, 'Adjust the AI assistant chat area font size (12-24px) independently from UI zoom.', '\u72ec\u7acb\u8c03\u6574 AI \u52a9\u624b\u804a\u5929\u533a\u7684\u5b57\u4f53\u5927\u5c0f\uff0812-24px\uff09\uff0c\u4e0d\u5f71\u54cd\u754c\u9762\u7f29\u653e\u3002', '\u7368\u7acb\u8abf\u6574 AI \u52a9\u624b\u804a\u5929\u5340\u7684\u5b57\u9ad4\u5927\u5c0f\uff0812-24px\uff09\uff0c\u4e0d\u5f71\u97ff\u4ecb\u9762\u7e2e\u653e\u3002')}
            </p>
        </section>

        <section className="ui-settings-card">
            <h4>
                {textForLang(lang, "Conversation Font", "会话助手字体", "會話助手字體")}
            </h4>
            <AssistantFontChoices
                audience="conversation"
                lang={lang}
                selected={chatFontFamily}
                onSelect={(fontId) => {
                    publishAssistantFont(
                        fontId,
                        chatFontFamilyDraftRef,
                        setChatFontFamily,
                        SetChatFontFamily,
                        () => GetChatFontFamily().then((id) => normalizeAssistantFontId("conversation", id)),
                        chatFontFamilySaveQueue,
                    );
                }}
            />
            <p>
                {textForLang(
                    lang,
                    "Only the conversation assistant uses this face. The default is the current font. A reading face keeps code blocks in monospace.",
                    "只影响会话助手里的正文。默认是现在的字体。选了阅读字体时，代码块仍用等宽字体。",
                    "只影響會話助手裡的正文。預設是現在的字體。選了閱讀字體時，程式碼區塊仍用等寬字體。",
                )}
            </p>
        </section>

        <section className="ui-settings-card">
            <h4>
                {textForLang(lang, "Coding Font", "编程助手字体", "編程助手字體")}
            </h4>
            <AssistantFontChoices
                audience="coding"
                lang={lang}
                selected={codingFontFamily}
                onSelect={(fontId) => {
                    publishAssistantFont(
                        fontId,
                        codingFontFamilyDraftRef,
                        setCodingFontFamily,
                        SetCodingFontFamily,
                        () => GetCodingFontFamily().then((id) => normalizeAssistantFontId("coding", id)),
                        codingFontFamilySaveQueue,
                    );
                }}
            />
            <p>
                {textForLang(
                    lang,
                    "Only the programming assistant uses this face, including its code. The default is the current font. The list is limited to programming faces.",
                    "只影响编程助手里的正文和代码。默认是现在的字体。这里只列出适合编程的字体。",
                    "只影響編程助手裡的正文和程式碼。預設是現在的字體。這裡只列出適合編程的字體。",
                )}
            </p>
        </section>
    </div>
    );
};
