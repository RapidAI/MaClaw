import type { Dispatch, SetStateAction } from 'react';
import { SetChatFontSize, SetUIZoomFactor } from '../../../wailsjs/go/main/App';
import { main } from '../../../wailsjs/go/models';
import { localizeText } from '../../i18n';
import { assistantDarkSchemes, type AssistantDarkSchemeId } from '../ai/assistantDarkSchemes';

type UISettingsPanelProps = {
    config: main.AppConfig | null;
    lang: string;
    t: (key: string) => string;
    uiZoom: number;
    setUiZoom: Dispatch<SetStateAction<number>>;
    chatFontSize: number;
    setChatFontSize: Dispatch<SetStateAction<number>>;
    darkSchemeId: AssistantDarkSchemeId;
    setDarkSchemeId: (schemeId: AssistantDarkSchemeId) => void;
};

const textForLang = localizeText;

export const UISettingsPanel = ({
    config,
    lang,
    t,
    uiZoom,
    setUiZoom,
    chatFontSize,
    setChatFontSize,
    darkSchemeId,
    setDarkSchemeId,
}: UISettingsPanelProps) => (
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
                    value={Math.round(uiZoom * 100)}
                    onChange={(e) => {
                        const v = Number(e.target.value) / 100;
                        setUiZoom(v);
                    }}
                    onPointerUp={async (e) => {
                        const v = Number((e.currentTarget as HTMLInputElement).value) / 100;
                        setUiZoom(v);
                        await SetUIZoomFactor(v).catch(() => {});
                    }}
                    aria-label={textForLang(lang, 'UI Zoom', '\u754c\u9762\u7f29\u653e', '\u4ecb\u9762\u7e2e\u653e')}
                />
                <span className="ui-settings-value">{Math.round(uiZoom * 100)}%</span>
                <button
                    onClick={() => { setUiZoom(1.0); SetUIZoomFactor(1.0).catch(() => {}); }}
                >
                    {textForLang(lang, 'Reset', '\u91cd\u7f6e', '\u91cd\u7f6e')}
                </button>
            </div>
            <p>
                {textForLang(lang, 'Adjust overall UI scale for HiDPI displays or personal preference.', '\u8c03\u6574\u754c\u9762\u6574\u4f53\u7f29\u653e\u6bd4\u4f8b\uff0c\u9002\u914d\u9ad8 DPI \u5c4f\u5e55\u6216\u4e2a\u4eba\u504f\u597d\u3002', '\u8abf\u6574\u4ecb\u9762\u6574\u9ad4\u7e2e\u653e\u6bd4\u4f8b\uff0c\u9069\u914d\u9ad8 DPI \u87a2\u5e55\u6216\u500b\u4eba\u504f\u597d\u3002')}
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
                    onClick={() => { setChatFontSize(14); SetChatFontSize(14).catch(() => {}); }}
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
