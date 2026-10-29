import { tailText } from "../../i18n/mnTail"
type Props = {
    disabled: boolean;
    isZh: boolean | string;
    resetting: boolean;
    onReset: () => void;
};

export function WelcomeResetButton({ disabled, isZh, resetting, onReset }: Props) {
    const lang = typeof isZh === "string" ? isZh : (isZh ? "zh-Hans" : "en");
    return <button
        type="button"
        className="im-settings-button"
        disabled={disabled}
        title={tailText(lang, 'Restore the embedded MaClaw recording', '恢复为 MaClaw 内嵌的默认录音')}
        onClick={onReset}
    >
        {resetting ? (tailText(lang, 'Resetting…', '重置中…')) : (tailText(lang, 'Reset', '重置'))}
    </button>;
}
