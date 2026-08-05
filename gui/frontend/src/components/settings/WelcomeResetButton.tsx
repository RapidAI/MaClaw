type Props = {
    disabled: boolean;
    isZh: boolean;
    resetting: boolean;
    onReset: () => void;
};

export function WelcomeResetButton({ disabled, isZh, resetting, onReset }: Props) {
    return <button
        type="button"
        className="im-settings-button"
        disabled={disabled}
        title={isZh ? '恢复为 MaClaw 内嵌的默认录音' : 'Restore the embedded MaClaw recording'}
        onClick={onReset}
    >
        {resetting ? (isZh ? '重置中…' : 'Resetting…') : (isZh ? '重置' : 'Reset')}
    </button>;
}
