import { useCallback, useState } from "react";
import type { CSSProperties, ChangeEvent } from "react";

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
    options?: string[];   // for select type
    pattern?: string;     // regex for text validation
    placeholder?: string;
}

export interface ValidationError {
    field: string;
    message: string;
}

export interface InitiateWorkflowResponse {
    instance_id: string;
    status: string;
    created_at: string;
    version_id: string;
}

export interface WorkflowInitiationFormProps {
    /** Workflow ID to initiate */
    workflowId: string;
    /** Workflow display name */
    workflowName?: string;
    /** Form field schema from the workflow's Form_Node */
    schema: FormFieldSchema[];
    /** Submit handler — calls POST /api/v1/workflows/{id}/initiate */
    onSubmit?: (workflowId: string, formData: Record<string, unknown>) => Promise<InitiateWorkflowResponse>;
    /** Called after successful submission (navigate to instance detail) */
    onSuccess?: (response: InitiateWorkflowResponse) => void;
    /** Hub base URL for redirect */
    hubBaseUrl?: string;
}

// --- Component ---

export function WorkflowInitiationForm({
    workflowId,
    workflowName,
    schema,
    onSubmit,
    onSuccess,
    hubBaseUrl,
}: WorkflowInitiationFormProps) {
    const [formData, setFormData] = useState<Record<string, unknown>>(() => {
        const initial: Record<string, unknown> = {};
        for (const field of schema) {
            if (field.type === "boolean") {
                initial[field.name] = false;
            } else if (field.type === "number") {
                initial[field.name] = "";
            } else {
                initial[field.name] = "";
            }
        }
        return initial;
    });

    const [errors, setErrors] = useState<Record<string, string>>({});
    const [submitting, setSubmitting] = useState(false);
    const [submitError, setSubmitError] = useState<string | null>(null);
    const [submitted, setSubmitted] = useState(false);

    // --- Client-side validation ---
    const validateForm = useCallback((): boolean => {
        const newErrors: Record<string, string> = {};

        for (const field of schema) {
            const value = formData[field.name];

            // Required check
            if (field.required) {
                if (value === undefined || value === null || value === "") {
                    newErrors[field.name] = `${field.label} 为必填项`;
                    continue;
                }
            }

            // Skip further validation if empty and not required
            if (value === undefined || value === null || value === "") continue;

            const strValue = String(value);

            // Type-specific validation
            switch (field.type) {
                case "text":
                case "textarea": {
                    if (field.max_length && strValue.length > field.max_length) {
                        newErrors[field.name] = `${field.label} 不能超过 ${field.max_length} 个字符`;
                    }
                    if (field.pattern) {
                        try {
                            const regex = new RegExp(field.pattern);
                            if (!regex.test(strValue)) {
                                newErrors[field.name] = `${field.label} 格式不正确`;
                            }
                        } catch {
                            // Invalid regex pattern — skip validation
                        }
                    }
                    break;
                }
                case "number": {
                    const numValue = Number(value);
                    if (isNaN(numValue)) {
                        newErrors[field.name] = `${field.label} 必须为数字`;
                    } else {
                        if (field.min_value !== undefined && numValue < field.min_value) {
                            newErrors[field.name] = `${field.label} 不能小于 ${field.min_value}`;
                        }
                        if (field.max_value !== undefined && numValue > field.max_value) {
                            newErrors[field.name] = `${field.label} 不能大于 ${field.max_value}`;
                        }
                    }
                    break;
                }
                case "date": {
                    // Basic date format check (YYYY-MM-DD)
                    if (!/^\d{4}-\d{2}-\d{2}$/.test(strValue)) {
                        newErrors[field.name] = `${field.label} 日期格式不正确`;
                    }
                    break;
                }
                case "select": {
                    if (field.options && field.options.length > 0 && !field.options.includes(strValue)) {
                        newErrors[field.name] = `${field.label} 选项无效`;
                    }
                    break;
                }
                case "file": {
                    // File validation is minimal on client side
                    if (field.required && !strValue) {
                        newErrors[field.name] = `${field.label} 为必填项`;
                    }
                    break;
                }
                case "boolean": {
                    // Boolean is always valid
                    break;
                }
            }
        }

        setErrors(newErrors);
        return Object.keys(newErrors).length === 0;
    }, [schema, formData]);

    // --- Field change handler ---
    const handleFieldChange = (fieldName: string, value: unknown) => {
        setFormData((prev) => ({ ...prev, [fieldName]: value }));
        // Clear error for this field on change
        if (errors[fieldName]) {
            setErrors((prev) => {
                const next = { ...prev };
                delete next[fieldName];
                return next;
            });
        }
    };

    // --- Submit ---
    const handleSubmit = useCallback(async (e: React.FormEvent) => {
        e.preventDefault();
        setSubmitError(null);

        if (!validateForm()) return;
        if (!onSubmit) return;

        setSubmitting(true);
        try {
            // Build clean form data (convert number strings to numbers, etc.)
            const cleanData: Record<string, unknown> = {};
            for (const field of schema) {
                const value = formData[field.name];
                if (value === "" && !field.required) continue;

                if (field.type === "number" && value !== "") {
                    cleanData[field.name] = Number(value);
                } else if (field.type === "boolean") {
                    cleanData[field.name] = Boolean(value);
                } else {
                    cleanData[field.name] = value;
                }
            }

            const response = await onSubmit(workflowId, cleanData);
            setSubmitted(true);

            // Redirect to instance detail page within 2 seconds
            setTimeout(() => {
                if (onSuccess) {
                    onSuccess(response);
                } else if (hubBaseUrl) {
                    const base = hubBaseUrl.replace(/\/+$/, "");
                    window.location.href = `${base}/workflow/instances/${response.instance_id}`;
                }
            }, 1500);
        } catch (err: unknown) {
            if (err instanceof Error) {
                // Try to parse server validation errors
                try {
                    const parsed = JSON.parse(err.message);
                    if (parsed.errors && Array.isArray(parsed.errors)) {
                        const serverErrors: Record<string, string> = {};
                        for (const ve of parsed.errors as ValidationError[]) {
                            serverErrors[ve.field] = ve.message;
                        }
                        setErrors(serverErrors);
                        return;
                    }
                } catch {
                    // Not JSON, use as-is
                }
                setSubmitError(err.message);
            } else {
                setSubmitError("提交失败，请重试");
            }
        } finally {
            setSubmitting(false);
        }
    }, [validateForm, onSubmit, schema, formData, workflowId, onSuccess, hubBaseUrl]);

    // --- Success state ---
    if (submitted) {
        return (
            <div style={successContainerStyle} role="status" aria-live="polite">
                <span style={successIconStyle}>✅</span>
                <h3 style={successHeadingStyle}>提交成功</h3>
                <p style={successTextStyle}>工作流已发起，正在跳转到实例详情页...</p>
            </div>
        );
    }

    return (
        <div style={containerStyle}>
            {/* Header */}
            <div style={headerStyle}>
                <h2 style={headingStyle}>{workflowName || "发起工作流"}</h2>
                <p style={subheadingStyle}>请填写以下表单信息</p>
            </div>

            {/* Form */}
            <form onSubmit={handleSubmit} style={formStyle} noValidate aria-label="工作流发起表单">
                {schema.map((field) => (
                    <FormField
                        key={field.name}
                        field={field}
                        value={formData[field.name]}
                        error={errors[field.name]}
                        onChange={(value) => handleFieldChange(field.name, value)}
                    />
                ))}

                {/* Submit error */}
                {submitError && (
                    <div style={submitErrorStyle} role="alert">
                        {submitError}
                    </div>
                )}

                {/* Submit button */}
                <div style={submitContainerStyle}>
                    <button
                        type="submit"
                        disabled={submitting}
                        style={submitting ? submitButtonDisabledStyle : submitButtonStyle}
                        aria-label="提交表单"
                    >
                        {submitting ? "提交中..." : "提交"}
                    </button>
                </div>
            </form>
        </div>
    );
}

// --- Form Field Component ---

function FormField({
    field,
    value,
    error,
    onChange,
}: {
    field: FormFieldSchema;
    value: unknown;
    error?: string;
    onChange: (value: unknown) => void;
}) {
    const fieldId = `field-${field.name}`;
    const errorId = `error-${field.name}`;
    const hasError = !!error;

    const renderInput = () => {
        switch (field.type) {
            case "text":
                return (
                    <input
                        id={fieldId}
                        type="text"
                        value={String(value || "")}
                        onChange={(e: ChangeEvent<HTMLInputElement>) => onChange(e.target.value)}
                        placeholder={field.placeholder || ""}
                        maxLength={field.max_length}
                        style={hasError ? inputErrorStyle : inputStyle}
                        aria-invalid={hasError}
                        aria-describedby={hasError ? errorId : undefined}
                        aria-required={field.required}
                    />
                );

            case "textarea":
                return (
                    <textarea
                        id={fieldId}
                        value={String(value || "")}
                        onChange={(e: ChangeEvent<HTMLTextAreaElement>) => onChange(e.target.value)}
                        placeholder={field.placeholder || ""}
                        maxLength={field.max_length}
                        style={hasError ? textareaErrorStyle : textareaStyle}
                        aria-invalid={hasError}
                        aria-describedby={hasError ? errorId : undefined}
                        aria-required={field.required}
                    />
                );

            case "number":
                return (
                    <input
                        id={fieldId}
                        type="number"
                        value={String(value || "")}
                        onChange={(e: ChangeEvent<HTMLInputElement>) => onChange(e.target.value)}
                        min={field.min_value}
                        max={field.max_value}
                        placeholder={field.placeholder || ""}
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
                        value={String(value || "")}
                        onChange={(e: ChangeEvent<HTMLInputElement>) => onChange(e.target.value)}
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
                        value={String(value || "")}
                        onChange={(e: ChangeEvent<HTMLSelectElement>) => onChange(e.target.value)}
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
                        type="file"
                        onChange={(e: ChangeEvent<HTMLInputElement>) => {
                            const file = e.target.files?.[0];
                            onChange(file ? file.name : "");
                        }}
                        style={fileInputStyle}
                        aria-invalid={hasError}
                        aria-describedby={hasError ? errorId : undefined}
                        aria-required={field.required}
                    />
                );

            case "boolean":
                return (
                    <label style={checkboxLabelStyle}>
                        <input
                            id={fieldId}
                            type="checkbox"
                            checked={Boolean(value)}
                            onChange={(e: ChangeEvent<HTMLInputElement>) => onChange(e.target.checked)}
                            style={checkboxStyle}
                            aria-invalid={hasError}
                            aria-describedby={hasError ? errorId : undefined}
                        />
                        <span style={checkboxTextStyle}>{field.label}</span>
                    </label>
                );

            default:
                return (
                    <input
                        id={fieldId}
                        type="text"
                        value={String(value || "")}
                        onChange={(e: ChangeEvent<HTMLInputElement>) => onChange(e.target.value)}
                        style={hasError ? inputErrorStyle : inputStyle}
                        aria-invalid={hasError}
                        aria-describedby={hasError ? errorId : undefined}
                        aria-required={field.required}
                    />
                );
        }
    };

    return (
        <div style={fieldContainerStyle}>
            {/* Label (skip for boolean — label is inline) */}
            {field.type !== "boolean" && (
                <label htmlFor={fieldId} style={labelStyle}>
                    {field.label}
                    {field.required && <span style={requiredMarkStyle}> *</span>}
                </label>
            )}

            {/* Input */}
            {renderInput()}

            {/* Inline error */}
            {hasError && (
                <span id={errorId} style={fieldErrorStyle} role="alert">
                    {error}
                </span>
            )}

            {/* Hint: max length */}
            {field.max_length && field.type !== "boolean" && !hasError && (
                <span style={hintStyle}>最多 {field.max_length} 个字符</span>
            )}
        </div>
    );
}

// --- Styles ---

const containerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "20px",
    padding: "24px",
    fontFamily: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
    maxWidth: "600px",
    margin: "0 auto",
};

const headerStyle: CSSProperties = {
    marginBottom: "8px",
};

const headingStyle: CSSProperties = {
    margin: 0,
    fontSize: "1.2rem",
    fontWeight: 600,
    color: "#212121",
};

const subheadingStyle: CSSProperties = {
    margin: "4px 0 0",
    fontSize: "0.82rem",
    color: "#888",
};

const formStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "16px",
};

const fieldContainerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    gap: "4px",
};

const labelStyle: CSSProperties = {
    fontSize: "0.82rem",
    fontWeight: 500,
    color: "#333",
};

const requiredMarkStyle: CSSProperties = {
    color: "#c62828",
};

const inputStyle: CSSProperties = {
    padding: "10px 12px",
    fontSize: "0.85rem",
    border: "1px solid #ddd",
    borderRadius: "6px",
    outline: "none",
    transition: "border-color 0.15s",
    boxSizing: "border-box",
};

const inputErrorStyle: CSSProperties = {
    ...inputStyle,
    borderColor: "#c62828",
};

const textareaStyle: CSSProperties = {
    ...inputStyle,
    minHeight: "100px",
    resize: "vertical",
    fontFamily: "inherit",
};

const textareaErrorStyle: CSSProperties = {
    ...textareaStyle,
    borderColor: "#c62828",
};

const selectStyle: CSSProperties = {
    ...inputStyle,
    background: "#fff",
    cursor: "pointer",
};

const selectErrorStyle: CSSProperties = {
    ...selectStyle,
    borderColor: "#c62828",
};

const fileInputStyle: CSSProperties = {
    padding: "8px 0",
    fontSize: "0.82rem",
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

const fieldErrorStyle: CSSProperties = {
    fontSize: "0.75rem",
    color: "#c62828",
    marginTop: "2px",
};

const hintStyle: CSSProperties = {
    fontSize: "0.7rem",
    color: "#999",
};

const submitContainerStyle: CSSProperties = {
    display: "flex",
    justifyContent: "flex-end",
    paddingTop: "12px",
    borderTop: "1px solid #eee",
};

const submitButtonStyle: CSSProperties = {
    padding: "10px 32px",
    fontSize: "0.88rem",
    fontWeight: 600,
    background: "#1565c0",
    color: "#fff",
    border: "none",
    borderRadius: "6px",
    cursor: "pointer",
    transition: "background 0.15s",
};

const submitButtonDisabledStyle: CSSProperties = {
    ...submitButtonStyle,
    opacity: 0.5,
    cursor: "not-allowed",
};

const submitErrorStyle: CSSProperties = {
    padding: "10px 14px",
    background: "#fce4ec",
    border: "1px solid #ef9a9a",
    borderRadius: "6px",
    fontSize: "0.8rem",
    color: "#c62828",
};

const successContainerStyle: CSSProperties = {
    display: "flex",
    flexDirection: "column",
    alignItems: "center",
    justifyContent: "center",
    padding: "60px 24px",
    textAlign: "center",
};

const successIconStyle: CSSProperties = {
    fontSize: "2.5rem",
    marginBottom: "12px",
};

const successHeadingStyle: CSSProperties = {
    margin: 0,
    fontSize: "1.1rem",
    fontWeight: 600,
    color: "#2e7d32",
};

const successTextStyle: CSSProperties = {
    margin: "8px 0 0",
    fontSize: "0.85rem",
    color: "#666",
};
