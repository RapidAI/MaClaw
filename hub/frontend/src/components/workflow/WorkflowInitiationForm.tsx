import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { CSSProperties, FormEvent, ReactNode } from "react";

// --- Types ---

export type FieldType = "text" | "number" | "date" | "select" | "file" | "textarea" | "boolean";

export interface FormFieldSchema {
    name: string;
    label: string;
    type: FieldType;
    required: boolean;
    max_length?: number;
    min_value?: number;
    max_value?: number;
    options?: string[];
    pattern?: string;
}

export interface WorkflowInitiationFormProps {
    workflowId: string;
    schema: FormFieldSchema[];
    apiBaseUrl?: string;
    onSuccess?: (instanceId: string) => void;
}

interface FieldError {
    field: string;
    message: string;
}

interface InitiateResponse {
    instance_id: string;
    status: string;
    created_at: string;
    version_id: string;
}

// --- Validation Helpers ---

function validateField(field: FormFieldSchema, value: unknown): string | null {
    const strVal = typeof value === "string" ? value : "";
    const isEmpty = value === undefined || value === null || strVal.trim() === "";

    // Required check
    if (field.required && isEmpty) {
        return `${field.label} 为必填项`;
    }

    // Skip further validation if empty and not required
    if (isEmpty) return null;

    switch (field.type) {
        case "text":
        case "textarea": {
            if (field.max_length && strVal.length > field.max_length) {
                return `${field.label} 不能超过 ${field.max_length} 个字符`;
            }
            if (field.pattern) {
                try {
                    const re = new RegExp(field.pattern);
                    if (!re.test(strVal)) {
                        return `${field.label} 格式不正确`;
                    }
                } catch {
                    // Invalid regex in schema — skip pattern validation
                }
            }
            break;
        }
        case "number": {
            const numVal = Number(value);
            if (isNaN(numVal)) {
                return `${field.label} 必须为数字`;
            }
            if (field.min_value !== undefined && numVal < field.min_value) {
                return `${field.label} 不能小于 ${field.min_value}`;
            }
            if (field.max_value !== undefined && numVal > field.max_value) {
                return `${field.label} 不能大于 ${field.max_value}`;
            }
            break;
        }
        case "date": {
            // Basic ISO date format check (YYYY-MM-DD)
            if (!/^\d{4}-\d{2}-\d{2}$/.test(strVal)) {
                return `${field.label} 日期格式不正确`;
            }
            const d = new Date(strVal);
            if (isNaN(d.getTime())) {
                return `${field.label} 不是有效日期`;
            }
            break;
        }
        case "select": {
            if (field.options && field.options.length > 0 && !field.options.includes(strVal)) {
                return `${field.label} 必须从选项中选择`;
            }
            break;
        }
        case "file": {
            // File field: just check non-empty (path or filename)
            break;
        }
        case "boolean": {
            // Boolean is always valid (true/false)
            break;
        }
    }

    return null;
}

function validateAllFields(
    schema: FormFieldSchema[],
    values: Record<string, unknown>
): FieldError[] {
    const errors: FieldError[] = [];
    for (const field of schema) {
        const err = validateField(field, values[field.name]);
        if (err) {
            errors.push({ field: field.name, message: err });
        }
    }
    return errors;
}

// --- Component ---

export function WorkflowInitiationForm({
    workflowId,
    schema,
    apiBaseUrl,
    onSuccess,
}: WorkflowInitiationFormProps) {
    const [values, setValues] = useState<Record<string, unknown>>(() => {
        const initial: Record<string, unknown> = {};
        for (const field of schema) {
            if (field.type === "boolean") {
                initial[field.name] = false;
            } else {
                initial[field.name] = "";
            }
        }
        return initial;
    });

    const [errors, setErrors] = useState<Record<string, string>>({});
    const [touched, setTouched] = useState<Record<string, boolean>>({});
    const [submitting, setSubmitting] = useState(false);
    const [submitError, setSubmitError] = useState<string | null>(null);
    const [submitSuccess, setSubmitSuccess] = useState(false);

    const redirectTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

    // Cleanup redirect timer on unmount
    useEffect(() => {
        return () => {
            if (redirectTimerRef.current) {
                clearTimeout(redirectTimerRef.current);
            }
        };
    }, []);

    const baseUrl = useMemo(() => {
        return (apiBaseUrl || "").replace(/\/+$/, "");
    }, [apiBaseUrl]);

    const setFieldValue = useCallback((name: string, value: unknown) => {
        setValues((prev) => ({ ...prev, [name]: value }));
        // Clear error on change
        setErrors((prev) => {
            if (prev[name]) {
                const next = { ...prev };
                delete next[name];
                return next;
            }
            return prev;
        });
    }, []);

    const handleBlur = useCallback((field: FormFieldSchema) => {
        setTouched((prev) => ({ ...prev, [field.name]: true }));
        const err = validateField(field, values[field.name]);
        setErrors((prev) => {
            if (err) {
                return { ...prev, [field.name]: err };
            }
            const next = { ...prev };
            delete next[field.name];
            return next;
        });
    }, [values]);

    const handleSubmit = useCallback(async (e: FormEvent) => {
        e.preventDefault();
        setSubmitError(null);

        // Mark all fields as touched
        const allTouched: Record<string, boolean> = {};
        for (const field of schema) {
            allTouched[field.name] = true;
        }
        setTouched(allTouched);

        // Validate all fields
        const validationErrors = validateAllFields(schema, values);
        if (validationErrors.length > 0) {
            const errMap: Record<string, string> = {};
            for (const ve of validationErrors) {
                errMap[ve.field] = ve.message;
            }
            setErrors(errMap);
            return;
        }

        // Build form_data payload
        const formData: Record<string, unknown> = {};
        for (const field of schema) {
            const val = values[field.name];
            if (field.type === "number" && val !== "" && val !== undefined) {
                formData[field.name] = Number(val);
            } else if (field.type === "boolean") {
                formData[field.name] = Boolean(val);
            } else {
                formData[field.name] = val;
            }
        }

        setSubmitting(true);

        try {
            const url = `${baseUrl}/api/v1/workflows/${encodeURIComponent(workflowId)}/initiate`;
            const resp = await fetch(url, {
                method: "POST",
                headers: { "Content-Type": "application/json" },
                body: JSON.stringify({
                    form_data: formData,
                    channel: "hub_page",
                }),
            });

            if (resp.status === 201) {
                const data: InitiateResponse = await resp.json();
                setSubmitSuccess(true);

                // Redirect within 2 seconds
                redirectTimerRef.current = setTimeout(() => {
                    if (onSuccess) {
                        onSuccess(data.instance_id);
                    } else {
                        // Default: navigate to instance detail page
                        const detailUrl = `${baseUrl}/workflow/instances/${data.instance_id}`;
                        window.location.href = detailUrl;
                    }
                }, 1500);
            } else if (resp.status === 400) {
                // Server-side validation errors
                const body = await resp.json();
                if (body.errors && Array.isArray(body.errors)) {
                    const serverErrors: Record<string, string> = {};
                    for (const err of body.errors as FieldError[]) {
                        serverErrors[err.field] = err.message;
                    }
                    setErrors(serverErrors);
                    setSubmitError("表单数据验证失败，请检查标红字段");
                } else {
                    setSubmitError(body.error || "提交失败 (400)");
                }
            } else if (resp.status === 401) {
                setSubmitError("认证失败，请重新登录");
            } else if (resp.status === 429) {
                setSubmitError("请求过于频繁，请稍后再试");
            } else {
                const text = await resp.text();
                setSubmitError(`提交失败 (${resp.status}): ${text.slice(0, 200)}`);
            }
        } catch (err: unknown) {
            const msg = err instanceof Error ? err.message : String(err);
            setSubmitError(`网络错误: ${msg}`);
        } finally {
            setSubmitting(false);
        }
    }, [schema, values, baseUrl, workflowId, onSuccess]);

    return (
        <form onSubmit={handleSubmit} style={formContainerStyle} noValidate aria-label="工作流发起表单">
            {/* Form Fields */}
            {schema.map((field) => (
                <FormFieldRenderer
                    key={field.name}
                    field={field}
                    value={values[field.name]}
                    error={touched[field.name] ? errors[field.name] : undefined}
                    onChange={(val) => setFieldValue(field.name, val)}
                    onBlur={() => handleBlur(field)}
                    disabled={submitting || submitSuccess}
                />
            ))}

            {/* Submit Error */}
            {submitError && (
                <div style={submitErrorStyle} role="alert" aria-live="polite">
                    {submitError}
                </div>
            )}

            {/* Success Message */}
            {submitSuccess && (
                <div style={submitSuccessStyle} role="status" aria-live="polite">
                    工作流已成功发起，正在跳转到实例详情页...
                </div>
            )}

            {/* Submit Button */}
            <button
                type="submit"
                disabled={submitting || submitSuccess}
                style={submitting || submitSuccess ? submitBtnDisabledStyle : submitBtnStyle}
                aria-busy={submitting}
            >
                {submitting ? "提交中..." : submitSuccess ? "已提交" : "提交"}
            </button>
        </form>
    );
}

// --- Field Renderer ---

interface FormFieldRendererProps {
    field: FormFieldSchema;
    value: unknown;
    error?: string;
    onChange: (value: unknown) => void;
    onBlur: () => void;
    disabled: boolean;
}

function FormFieldRenderer({ field, value, error, onChange, onBlur, disabled }: FormFieldRendererProps) {
    const strVal = typeof value === "string" ? value : String(value ?? "");
    const hasError = !!error;
    const fieldId = `wf-field-${field.name}`;
    const errorId = `wf-error-${field.name}`;

    const renderInput = (): ReactNode => {
        switch (field.type) {
            case "text":
                return (
                    <div style={inputWrapperStyle}>
                        <input
                            id={fieldId}
                            type="text"
                            value={strVal}
                            onChange={(e) => onChange(e.target.value)}
                            onBlur={onBlur}
                            disabled={disabled}
                            maxLength={field.max_length}
                            style={hasError ? inputErrorStyle : inputStyle}
                            aria-invalid={hasError}
                            aria-describedby={hasError ? errorId : undefined}
                            aria-required={field.required}
                        />
                        {field.max_length && (
                            <CharCounter current={strVal.length} max={field.max_length} />
                        )}
                    </div>
                );

            case "number":
                return (
                    <input
                        id={fieldId}
                        type="number"
                        value={strVal}
                        onChange={(e) => onChange(e.target.value)}
                        onBlur={onBlur}
                        disabled={disabled}
                        min={field.min_value}
                        max={field.max_value}
                        style={hasError ? inputErrorStyle : inputStyle}
                        aria-invalid={hasError}
                        aria-describedby={hasError ? errorId : undefined}
                        aria-required={field.required}
                    />
                );

            case "date":
                return (
                    <input
                        id={fieldId}
                        type="date"
                        value={strVal}
                        onChange={(e) => onChange(e.target.value)}
                        onBlur={onBlur}
                        disabled={disabled}
                        style={hasError ? inputErrorStyle : inputStyle}
                        aria-invalid={hasError}
                        aria-describedby={hasError ? errorId : undefined}
                        aria-required={field.required}
                    />
                );

            case "select":
                return (
                    <select
                        id={fieldId}
                        value={strVal}
                        onChange={(e) => onChange(e.target.value)}
                        onBlur={onBlur}
                        disabled={disabled}
                        style={hasError ? selectErrorStyle : selectStyle}
                        aria-invalid={hasError}
                        aria-describedby={hasError ? errorId : undefined}
                        aria-required={field.required}
                    >
                        <option value="">请选择...</option>
                        {(field.options || []).map((opt) => (
                            <option key={opt} value={opt}>{opt}</option>
                        ))}
                    </select>
                );

            case "file":
                return (
                    <input
                        id={fieldId}
                        type="text"
                        value={strVal}
                        onChange={(e) => onChange(e.target.value)}
                        onBlur={onBlur}
                        disabled={disabled}
                        placeholder="输入文件路径"
                        style={hasError ? inputErrorStyle : inputStyle}
                        aria-invalid={hasError}
                        aria-describedby={hasError ? errorId : undefined}
                        aria-required={field.required}
                    />
                );

            case "textarea":
                return (
                    <div style={inputWrapperStyle}>
                        <textarea
                            id={fieldId}
                            value={strVal}
                            onChange={(e) => onChange(e.target.value)}
                            onBlur={onBlur}
                            disabled={disabled}
                            maxLength={field.max_length}
                            rows={4}
                            style={hasError ? textareaErrorStyle : textareaStyle}
                            aria-invalid={hasError}
                            aria-describedby={hasError ? errorId : undefined}
                            aria-required={field.required}
                        />
                        {field.max_length && (
                            <CharCounter current={strVal.length} max={field.max_length} />
                        )}
                    </div>
                );

            case "boolean":
                return (
                    <label style={checkboxLabelStyle}>
                        <input
                            id={fieldId}
                            type="checkbox"
                            checked={Boolean(value)}
                            onChange={(e) => onChange(e.target.checked)}
                            onBlur={onBlur}
                            disabled={disabled}
                            style={checkboxStyle}
                            aria-invalid={hasError}
                            aria-describedby={hasError ? errorId : undefined}
                        />
                        <span style={checkboxTextStyle}>{field.label}</span>
                    </label>
                );

            default:
                return null;
        }
    };

    // Boolean fields render label inline with checkbox
    if (field.type === "boolean") {
        return (
            <div style={fieldGroupStyle}>
                {renderInput()}
                {hasError && (
                    <span id={errorId} style={errorTextStyle} role="alert">
                        {error}
                    </span>
                )}
            </div>
        );
    }

    return (
        <div style={fieldGroupStyle}>
            <label htmlFor={fieldId} style={labelStyle}>
                {field.label}
                {field.required && <span style={requiredMarkStyle}> *</span>}
            </label>
            {renderInput()}
            {hasError && (
                <span id={errorId} style={errorTextStyle} role="alert">
                    {error}
                </span>
            )}
        </div>
    );
}

// --- Character Counter ---

function CharCounter({ current, max }: { current: number; max: number }) {
    const isOver = current > max;
    return (
        <span style={{ ...charCounterStyle, color: isOver ? "#c62828" : "#9e9e9e" }}>
            {current}/{max}
        </span>
    );
}

// --- Styles ---

const formContainerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "20px",
    maxWidth: "640px",
    margin: "0 auto",
    padding: "24px",
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
};

const fieldGroupStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "4px",
};

const labelStyle: CSSProperties = {
    fontSize: "0.85rem",
    fontWeight: 500,
    color: "#333",
};

const requiredMarkStyle: CSSProperties = {
    color: "#c62828",
    fontWeight: 600,
};

const inputWrapperStyle: CSSProperties = {
    position: "relative",
};

const inputBaseStyle: CSSProperties = {
    width: "100%",
    padding: "8px 12px",
    fontSize: "0.85rem",
    border: "1px solid #ddd",
    borderRadius: "6px",
    outline: "none",
    transition: "border-color 0.15s, box-shadow 0.15s",
    boxSizing: "border-box",
    color: "#333",
    background: "#fff",
};

const inputStyle: CSSProperties = {
    ...inputBaseStyle,
};

const inputErrorStyle: CSSProperties = {
    ...inputBaseStyle,
    borderColor: "#c62828",
    boxShadow: "0 0 0 2px rgba(198, 40, 40, 0.1)",
};

const selectStyle: CSSProperties = {
    ...inputBaseStyle,
    appearance: "auto",
};

const selectErrorStyle: CSSProperties = {
    ...inputErrorStyle,
    appearance: "auto",
};

const textareaStyle: CSSProperties = {
    ...inputBaseStyle,
    resize: "vertical",
    minHeight: "80px",
};

const textareaErrorStyle: CSSProperties = {
    ...inputErrorStyle,
    resize: "vertical",
    minHeight: "80px",
};

const checkboxLabelStyle: CSSProperties = {
    display: "flex",
    alignItems: "center",
    gap: "8px",
    cursor: "pointer",
    padding: "4px 0",
};

const checkboxStyle: CSSProperties = {
    width: "16px",
    height: "16px",
    cursor: "pointer",
};

const checkboxTextStyle: CSSProperties = {
    fontSize: "0.85rem",
    color: "#333",
};

const charCounterStyle: CSSProperties = {
    position: "absolute",
    right: "8px",
    bottom: "6px",
    fontSize: "0.7rem",
    pointerEvents: "none",
};

const errorTextStyle: CSSProperties = {
    fontSize: "0.75rem",
    color: "#c62828",
    marginTop: "2px",
};

const submitErrorStyle: CSSProperties = {
    padding: "10px 14px",
    background: "#fbe9e7",
    border: "1px solid #ffccbc",
    borderRadius: "6px",
    color: "#d84315",
    fontSize: "0.82rem",
};

const submitSuccessStyle: CSSProperties = {
    padding: "10px 14px",
    background: "#e8f5e9",
    border: "1px solid #c8e6c9",
    borderRadius: "6px",
    color: "#2e7d32",
    fontSize: "0.82rem",
};

const submitBtnBase: CSSProperties = {
    padding: "10px 24px",
    fontSize: "0.88rem",
    fontWeight: 600,
    border: "none",
    borderRadius: "6px",
    cursor: "pointer",
    transition: "background 0.15s, opacity 0.15s",
    alignSelf: "flex-start",
};

const submitBtnStyle: CSSProperties = {
    ...submitBtnBase,
    background: "#1565c0",
    color: "#fff",
};

const submitBtnDisabledStyle: CSSProperties = {
    ...submitBtnBase,
    background: "#90caf9",
    color: "#fff",
    cursor: "not-allowed",
    opacity: 0.7,
};
