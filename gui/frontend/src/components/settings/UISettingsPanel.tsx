import { useEffect, useRef, type Dispatch, type SetStateAction } from 'react';
import { SetChatFontSize, SetUIZoomFactor } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import {
    clampUIScale,
    recommendUIScale,
    UI_SCALE_AUTO,
    uiScaleEquals,
    uiScaleToPercent,
} from '../../utils/uiScale';
import { assistantDarkSchemes, type AssistantDarkSchemeId } from '../ai/assistantDarkSchemes';
import { assistantLightSchemes, type AssistantLightSchemeId } from '../ai/assistantLightSchemes';

type UISettingsPanelProps = {
    config: main.AppConfig | null;
    lang: string;
    t: (key: string) => string;
    uiZoom: number;
    setUiZoom: Dispatch<SetStateAction<number>>;
    /** When true, scale follows DPI/resolution recommendation. */
    uiZoomAuto: boolean;
    setUiZoomAuto: Dispatch<SetStateAction<boolean>>;
    chatFontSize: number;
    setChatFontSize: Dispatch<SetStateAction<number>>;
    darkSchemeId: AssistantDarkSchemeId;
    setDarkSchemeId: (schemeId: AssistantDarkSchemeId) => void;
    lightSchemeId: AssistantLightSchemeId;
    setLightSchemeId: (schemeId: AssistantLightSchemeId) => void;
};

const textForLang = localizeText;
const UI_ZOOM_PERSIST_DEBOUNCE_MS = 280;

export const UISettingsPanel = ({
    config,
    lang,
    t,
    uiZoom,
    setUiZoom,
    uiZoomAuto,
    setUiZoomAuto,
    chatFontSize,
    setChatFontSize,
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
        const auto = recommendUIScale();
        lastPersistedRef.current = UI_SCALE_AUTO;
        setUiZoomAuto(true);
        setUiZoom(auto);
        void SetUIZoomFactor(UI_SCALE_AUTO).catch(() => {});
    };

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
                    onChange={(e) => setChatFontSize(Number(e.target.value))}
                    onPointerUp={async (e) => {
                        const v = Number((e.currentTarget as HTMLInputElement).value);
                        setChatFontSize(v);
                        await SetChatFontSize(v).catch(() => {});
                    }}
                    aria-label={textForLang(lang, 'AI Assistant Font Size', 'AI \u52a9\u624b\u9762\u677f\u5b57\u53f7', 'AI \u52a9\u624b\u9762\u677f\u5b57\u865f')}
                />
                <span className="ui-settings-value">{chatFontSize}px</span>
                <button
                    type="button"
                    onClick={() => { setChatFontSize(14); void SetChatFontSize(14).catch(() => {}); }}
                >
                    {textForLang(lang, 'Reset', '\u91cd\u7f6e', '\u91cd\u7f6e')}
                </button>
            </div>
            <p>
                {textForLang(lang, 'Adjust the AI assistant chat area font size (12-24px) independently from UI zoom.', '\u72ec\u7acb\u8c03\u6574 AI \u52a9\u624b\u804a\u5929\u533a\u7684\u5b57\u4f53\u5927\u5c0f\uff0812-24px\uff09\uff0c\u4e0d\u5f71\u54cd\u754c\u9762\u7f29\u653e\u3002', '\u7368\u7acb\u8abf\u6574 AI \u52a9\u624b\u804a\u5929\u5340\u7684\u5b57\u9ad4\u5927\u5c0f\uff0812-24px\uff09\uff0c\u4e0d\u5f71\u97ff\u4ecb\u9762\u7e2e\u653e\u3002')}
            </p>
        </section>
    </div>
    );
};
