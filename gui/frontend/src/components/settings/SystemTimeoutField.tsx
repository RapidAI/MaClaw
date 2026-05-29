type SystemTimeoutFieldProps = {
    label: string;
    value: number;
    fieldName: string;
    title: string;
    saveRemoteConfigField: (patch: Record<string, any>) => void;
};

const MIN_SYSTEM_TIMEOUT_SEC = 240;
const DEFAULT_SYSTEM_TIMEOUT_SEC = 600;
const MAX_SYSTEM_TIMEOUT_SEC = 600;

export const clampSystemTimeoutSec = (value: number) => Math.min(MAX_SYSTEM_TIMEOUT_SEC, Math.max(MIN_SYSTEM_TIMEOUT_SEC, Number.isFinite(value) ? Math.floor(value) : DEFAULT_SYSTEM_TIMEOUT_SEC));

export const SystemTimeoutField = ({ label, value, fieldName, title, saveRemoteConfigField }: SystemTimeoutFieldProps) => {
    const clampedValue = clampSystemTimeoutSec(Number(value));
    const saveClampedValue = (rawValue: string) => {
        saveRemoteConfigField({ [fieldName]: clampSystemTimeoutSec(Number(rawValue || DEFAULT_SYSTEM_TIMEOUT_SEC)) } as any);
    };

    return (
        <label className="system-settings-field">
            <span>{label}</span>
            <input
                className="form-input"
                type="number"
                min={MIN_SYSTEM_TIMEOUT_SEC}
                max={MAX_SYSTEM_TIMEOUT_SEC}
                step={30}
                value={clampedValue}
                onChange={(e) => saveClampedValue(e.target.value)}
                onBlur={(e) => saveClampedValue(e.target.value)}
                title={title}
            />
        </label>
    );
};
