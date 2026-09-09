type SystemTimeoutFieldProps = {
    label: string;
    value: number;
    fieldName: string;
    title: string;
    saveRemoteConfigField: (patch: Record<string, any>) => void;
    min?: number;
    max?: number;
    defaultValue?: number;
    step?: number;
};

const MIN_SYSTEM_TIMEOUT_SEC = 240;
const DEFAULT_SYSTEM_TIMEOUT_SEC = 600;
const MAX_SYSTEM_TIMEOUT_SEC = 600;

export const clampSystemTimeoutSec = (
    value: number,
    min = MIN_SYSTEM_TIMEOUT_SEC,
    max = MAX_SYSTEM_TIMEOUT_SEC,
    defaultValue = DEFAULT_SYSTEM_TIMEOUT_SEC,
) => Math.min(max, Math.max(min, Number.isFinite(value) ? Math.floor(value) : defaultValue));

export const SystemTimeoutField = ({
    label,
    value,
    fieldName,
    title,
    saveRemoteConfigField,
    min = MIN_SYSTEM_TIMEOUT_SEC,
    max = MAX_SYSTEM_TIMEOUT_SEC,
    defaultValue = DEFAULT_SYSTEM_TIMEOUT_SEC,
    step = 30,
}: SystemTimeoutFieldProps) => {
    const clampedValue = clampSystemTimeoutSec(Number(value), min, max, defaultValue);
    const saveClampedValue = (rawValue: string) => {
        saveRemoteConfigField({ [fieldName]: clampSystemTimeoutSec(Number(rawValue || defaultValue), min, max, defaultValue) } as any);
    };

    return (
        <label className="system-settings-field">
            <span>{label}</span>
            <input
                className="form-input"
                type="number"
                min={min}
                max={max}
                step={step}
                value={clampedValue}
                onChange={(e) => saveClampedValue(e.target.value)}
                onBlur={(e) => saveClampedValue(e.target.value)}
                title={title}
            />
        </label>
    );
};
