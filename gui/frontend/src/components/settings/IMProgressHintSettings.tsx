import { textForLang } from './imSettingsShared';

type IMProgressHintSettingsProps = {
    lang: string;
    enabled: boolean;
    onChange: (enabled: boolean) => void;
};

export const IMProgressHintSettings = ({ lang, enabled, onChange }: IMProgressHintSettingsProps) => (
    <section className="im-settings-card im-settings-channel">
        <div className="im-settings-toolbar">
            <label className="im-settings-toggle">
                <input
                    type="checkbox"
                    aria-label={textForLang(lang, 'Show progress hints', '\u663e\u793a\u63d0\u793a\u4fe1\u606f', '\u986f\u793a\u63d0\u793a\u8cc7\u8a0a')}
                    checked={enabled}
                    onChange={(e) => onChange(e.target.checked)}
                />
                <span>{textForLang(lang, 'Show progress hints', '\u663e\u793a\u63d0\u793a\u4fe1\u606f', '\u986f\u793a\u63d0\u793a\u8cc7\u8a0a')}</span>
            </label>
        </div>
        <p className="im-settings-description">
            {textForLang(lang, 'When off, IM only shows the first progress note and the final result.', '\u5173\u95ed\u540e\uff0cIM \u53ea\u663e\u793a\u7b2c\u4e00\u6761\u8fdb\u5ea6\u63d0\u793a\u548c\u6700\u7ec8\u7ed3\u679c\u3002', '\u95dc\u9589\u5f8c\uff0cIM \u53ea\u986f\u793a\u7b2c\u4e00\u689d\u9032\u5ea6\u63d0\u793a\u548c\u6700\u7d42\u7d50\u679c\u3002')}
        </p>
    </section>
);
