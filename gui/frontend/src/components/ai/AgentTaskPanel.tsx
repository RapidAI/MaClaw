import { useEffect, useMemo, useRef, useState } from "react";
import type React from "react";
import type { AgentView, AgentViewField, AgentViewOption, AgentViewTableColumn, AgentViewVariant, AgentViewWizardStep } from "./agentViewTypes";
import type { Theme } from "./aiAssistantPanelTheme";
import { agentViewStrings, type AgentViewStrings } from "./agentViewI18n";

interface AgentTaskPanelProps {
    view: AgentView;
    onDismiss?: (viewId: string | undefined) => void | Promise<void>;
    onResizeStart?: () => void;
    onToggleMaximize?: () => void;
    onSubmit?: (viewId: string | undefined, data: Record<string, unknown>) => void | Promise<void>;
    theme: Theme;
    lang?: string;
}

function optionValue(option: string | AgentViewOption): string {
    return typeof option === "string" ? option : option.value;
}

function optionLabel(option: string | AgentViewOption): string {
    return typeof option === "string" ? option : option.label;
}

function normalizeMultiValue(value: unknown): string[] {
    if (Array.isArray(value)) return value.map((item) => String(item));
    if (typeof value === "string" && value.trim()) return value.split(",").map((item) => item.trim()).filter(Boolean);
    return [];
}

function allowedOptionValues(options?: Array<string | AgentViewOption>): string[] {
    return (options || []).map(optionValue).filter((value) => value.trim() !== "");
}

function normalizeTableRows(value: unknown): Array<Record<string, unknown>> {
    if (!Array.isArray(value)) return [];
    return value.map((item) => {
        if (item && typeof item === "object" && !Array.isArray(item)) {
            return { ...(item as Record<string, unknown>) };
        }
        return { value: item };
    });
}

function normalizeObjectValue(value: unknown): Record<string, unknown> {
    if (value && typeof value === "object" && !Array.isArray(value)) {
        return { ...(value as Record<string, unknown>) };
    }
    return {};
}

function inferTableColumns(field: AgentViewField, rows: Array<Record<string, unknown>>): AgentViewTableColumn[] {
    if (field.columns && field.columns.length > 0) return field.columns;
    const names = new Set<string>();
    for (const row of rows.slice(0, 5)) {
        for (const key of Object.keys(row)) names.add(key);
    }
    if (names.size === 0) return [{ name: "description", label: "Description" }, { name: "amount", label: "Amount", type: "number" }];
    return Array.from(names).map((name) => ({ name, label: name, type: typeof rows.find((row) => typeof row[name] === "number")?.[name] === "number" ? "number" : "text" }));
}

function inferObjectColumns(field: AgentViewField, value: Record<string, unknown>): AgentViewTableColumn[] {
    if (field.columns && field.columns.length > 0) return field.columns;
    const names = Object.keys(value);
    if (names.length === 0) return [{ name: "key", label: "Key" }, { name: "value", label: "Value" }];
    return names.map((name) => ({ name, label: name, type: typeof value[name] === "number" ? "number" : typeof value[name] === "boolean" ? "boolean" : "text" }));
}

function isMissingFormValue(value: unknown): boolean {
    if (Array.isArray(value)) return value.length === 0;
    if (value && typeof value === "object") return Object.keys(value as Record<string, unknown>).length === 0;
    return !formatValue(value).trim();
}

function nestedRequiredMissing(field: AgentViewField, value: unknown): string[] {
    if (field.type === "object_form") {
        const objectValue = normalizeObjectValue(value);
        return inferObjectColumns(field, objectValue)
            .filter((column) => column.required && isMissingFormValue(objectValue[column.name]))
            .map((column) => `${field.label || field.name}.${column.label || column.name}`);
    }
    if (field.type === "array_table") {
        const rows = normalizeTableRows(value);
        const columns = inferTableColumns(field, rows).filter((column) => column.required);
        if (rows.length === 0 || columns.length === 0) return [];
        const missing: string[] = [];
        rows.forEach((row, rowIndex) => {
            columns.forEach((column) => {
                if (isMissingFormValue(row[column.name])) {
                    missing.push(`${field.label || field.name}[${rowIndex + 1}].${column.label || column.name}`);
                }
            });
        });
        return missing;
    }
    return [];
}

function isMultipleOf(value: number, step: number): boolean {
    if (!Number.isFinite(value) || !Number.isFinite(step) || step <= 0) return true;
    const quotient = value / step;
    return Math.abs(quotient - Math.round(quotient)) < 1e-9;
}

function numberValidationError(label: string, value: unknown, min?: number, max?: number, exclusiveMin?: number, exclusiveMax?: number, step?: number): string | null {
    if (value === "" || value === null || value === undefined) return null;
    const numberValue = typeof value === "number" ? value : Number(value);
    if (!Number.isFinite(numberValue)) return `${label} must be a valid number`;
    if (typeof min === "number" && numberValue < min) return `${label} must be at least ${min}`;
    if (typeof max === "number" && numberValue > max) return `${label} must be at most ${max}`;
    if (typeof exclusiveMin === "number" && numberValue <= exclusiveMin) return `${label} must be greater than ${exclusiveMin}`;
    if (typeof exclusiveMax === "number" && numberValue >= exclusiveMax) return `${label} must be less than ${exclusiveMax}`;
    if (typeof step === "number" && !isMultipleOf(numberValue, step)) return `${label} must be a multiple of ${step}`;
    return null;
}

function formatValidationError(label: string, text: string, format?: string): string | null {
    const normalized = (format || "").trim().toLowerCase();
    if (!text.trim()) return null;
    if (!normalized) return null;
    if (normalized === "email") {
        if (!/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(text)) return `${label} must be a valid email`;
        return null;
    }
    if (normalized === "uri" || normalized === "url" || normalized === "uri-reference") {
        try {
            const url = new URL(text);
            if (!url.protocol || !url.host) return `${label} must be a valid URL`;
        } catch {
            return `${label} must be a valid URL`;
        }
        return null;
    }
    if (normalized === "uuid") {
        if (!/^[0-9a-f]{8}-[0-9a-f]{4}-[1-5][0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i.test(text)) return `${label} must be a valid UUID`;
        return null;
    }
    if (normalized === "date") {
        if (!/^\d{4}-\d{2}-\d{2}$/.test(text) || Number.isNaN(Date.parse(`${text}T00:00:00`))) return `${label} must be a valid date`;
        return null;
    }
    if (normalized === "date-time" || normalized === "datetime") {
        if (Number.isNaN(Date.parse(text))) return `${label} must be a valid date and time`;
        return null;
    }
    return null;
}

function textValidationError(label: string, value: unknown, minLength?: number, maxLength?: number, pattern?: string, format?: string): string | null {
    if (value === "" || value === null || value === undefined) return null;
    const text = formatValue(value);
    if (typeof minLength === "number" && text.length < minLength) return `${label} must be at least ${minLength} characters`;
    if (typeof maxLength === "number" && text.length > maxLength) return `${label} must be at most ${maxLength} characters`;
    if (pattern) {
        try {
            if (!new RegExp(pattern).test(text)) return `${label} has an invalid format`;
        } catch {
            return null;
        }
    }
    const formatError = formatValidationError(label, text, format);
    if (formatError) return formatError;
    return null;
}

function optionValidationErrors(label: string, value: unknown, options?: Array<string | AgentViewOption>, multiple = false): string[] {
    const allowed = allowedOptionValues(options);
    if (allowed.length === 0 || value === "" || value === null || value === undefined) return [];
    const selected = multiple ? normalizeMultiValue(value) : [formatValue(value)];
    const invalid = selected.filter((item) => item.trim() !== "" && !allowed.includes(item));
    if (invalid.length === 0) return [];
    return [`${label} must be one of: ${allowed.join(", ")}`];
}

function valuesEqual(left: unknown, right: unknown): boolean {
    if (left === right) return true;
    return formatValue(left) === formatValue(right);
}

function stableValueKey(value: unknown): string {
    if (value && typeof value === "object" && !Array.isArray(value)) {
        const entries = Object.entries(value as Record<string, unknown>)
            .sort(([left], [right]) => left.localeCompare(right))
            .map(([key, nested]) => [key, stableValueKey(nested)]);
        return JSON.stringify(entries);
    }
    if (Array.isArray(value)) return JSON.stringify(value.map(stableValueKey));
    return formatValue(value);
}

function duplicateArrayItemError(label: string, value: unknown, uniqueItems?: boolean): string | null {
    if (!uniqueItems || !Array.isArray(value)) return null;
    const seen = new Set<string>();
    for (const item of value) {
        const key = stableValueKey(item);
        if (seen.has(key)) return `${label} must not contain duplicate items`;
        seen.add(key);
    }
    return null;
}

function constValidationError(label: string, value: unknown, constValue: unknown): string | null {
    if (constValue === undefined || value === "" || value === null || value === undefined) return null;
    if (valuesEqual(value, constValue)) return null;
    return `${label} must be ${formatValue(constValue)}`;
}

function isTextLikeType(type: AgentViewField["type"] | AgentViewTableColumn["type"]): boolean {
    return !type || type === "text" || type === "textarea" || type === "file" || type === "user_ref" || type === "department_ref" || type === "business_ref";
}

function isSensitiveFormat(format?: string): boolean {
    const normalized = (format || "").trim().toLowerCase();
    return normalized === "password" || normalized === "secret" || normalized === "token";
}

function fieldValidationErrors(field: AgentViewField, value: unknown): string[] {
    const label = field.label || field.name;
    const errors: string[] = [];
    if (field.required && isMissingFormValue(value)) {
        errors.push(label);
        return errors;
    }
    const constError = constValidationError(label, value, field.constValue);
    if (constError) errors.push(constError);
    errors.push(...nestedRequiredMissing(field, value));
    if (field.type === "number") {
        const error = numberValidationError(label, value, field.min, field.max, field.exclusiveMin, field.exclusiveMax, field.step);
        if (error) errors.push(error);
    }
    if (isTextLikeType(field.type)) {
        const error = textValidationError(label, value, field.minLength, field.maxLength, field.pattern, field.format);
        if (error) errors.push(error);
    }
    if (field.type === "date" || field.type === "datetime") {
        const error = formatValidationError(label, formatValue(value), field.type === "datetime" ? "date-time" : "date");
        if (error) errors.push(error);
    }
    if (field.type === "select" || field.type === "business_ref" || field.type === "user_ref" || field.type === "department_ref") {
        errors.push(...optionValidationErrors(label, value, field.options));
    }
    if (field.type === "multiselect") {
        errors.push(...optionValidationErrors(label, value, field.options, true));
    }
    if (field.type === "multiselect" || field.type === "array_table") {
        const itemCount = Array.isArray(value) ? value.length : 0;
        if (typeof field.minItems === "number" && itemCount < field.minItems) errors.push(`${label} needs at least ${field.minItems} item(s)`);
        if (typeof field.maxItems === "number" && itemCount > field.maxItems) errors.push(`${label} allows at most ${field.maxItems} item(s)`);
        const duplicateError = duplicateArrayItemError(label, value, field.uniqueItems);
        if (duplicateError) errors.push(duplicateError);
    }
    if (field.type === "object_form") {
        const objectValue = normalizeObjectValue(value);
        const columns = inferObjectColumns(field, objectValue);
        errors.push(...nestedDependentRequiredErrors(label, columns, objectValue, field.dependentRequired));
        for (const column of columns) {
            const nestedLabel = `${label}.${column.label || column.name}`;
            const constError = constValidationError(nestedLabel, objectValue[column.name], column.constValue);
            if (constError) errors.push(constError);
            if (column.type === "number") {
                const error = numberValidationError(nestedLabel, objectValue[column.name], column.min, column.max, column.exclusiveMin, column.exclusiveMax, column.step);
                if (error) errors.push(error);
            }
            if (isTextLikeType(column.type)) {
                const error = textValidationError(nestedLabel, objectValue[column.name], column.minLength, column.maxLength, column.pattern, column.format);
                if (error) errors.push(error);
            }
            if (column.type === "date") {
                const error = formatValidationError(nestedLabel, formatValue(objectValue[column.name]), "date");
                if (error) errors.push(error);
            }
            if (column.type === "select") {
                errors.push(...optionValidationErrors(nestedLabel, objectValue[column.name], column.options));
            }
        }
    }
    if (field.type === "array_table") {
        const rows = normalizeTableRows(value);
        const columns = inferTableColumns(field, rows);
        rows.forEach((row, rowIndex) => {
            errors.push(...nestedDependentRequiredErrors(`${label}[${rowIndex + 1}]`, columns, row, field.dependentRequired));
            columns.forEach((column) => {
                const nestedLabel = `${label}[${rowIndex + 1}].${column.label || column.name}`;
                const constError = constValidationError(nestedLabel, row[column.name], column.constValue);
                if (constError) errors.push(constError);
                if (column.type === "number") {
                    const error = numberValidationError(nestedLabel, row[column.name], column.min, column.max, column.exclusiveMin, column.exclusiveMax, column.step);
                    if (error) errors.push(error);
                }
                if (isTextLikeType(column.type)) {
                    const error = textValidationError(nestedLabel, row[column.name], column.minLength, column.maxLength, column.pattern, column.format);
                    if (error) errors.push(error);
                }
                if (column.type === "date") {
                    const error = formatValidationError(nestedLabel, formatValue(row[column.name]), "date");
                    if (error) errors.push(error);
                }
                if (column.type === "select") {
                    errors.push(...optionValidationErrors(nestedLabel, row[column.name], column.options));
                }
            });
        });
    }
    return errors;
}

function dependentRequiredErrors(fields: AgentViewField[], data: Record<string, unknown>, dependentRequired?: Record<string, string[]>): string[] {
    if (!dependentRequired) return [];
    const byName = new Map(fields.map((field) => [field.name, field]));
    const errors: string[] = [];
    Object.entries(dependentRequired).forEach(([trigger, requiredFields]) => {
        if (isMissingFormValue(data[trigger])) return;
        const triggerLabel = byName.get(trigger)?.label || trigger;
        requiredFields.forEach((requiredName) => {
            if (!isMissingFormValue(data[requiredName])) return;
            const requiredLabel = byName.get(requiredName)?.label || requiredName;
            errors.push(`${requiredLabel} is required when ${triggerLabel} is provided`);
        });
    });
    return errors;
}

function nestedDependentRequiredErrors(prefix: string, columns: AgentViewTableColumn[], data: Record<string, unknown>, dependentRequired?: Record<string, string[]>): string[] {
    if (!dependentRequired) return [];
    const byName = new Map(columns.map((column) => [column.name, column]));
    const errors: string[] = [];
    Object.entries(dependentRequired).forEach(([trigger, requiredColumns]) => {
        if (isMissingFormValue(data[trigger])) return;
        const triggerLabel = byName.get(trigger)?.label || trigger;
        requiredColumns.forEach((requiredName) => {
            if (!isMissingFormValue(data[requiredName])) return;
            const requiredLabel = byName.get(requiredName)?.label || requiredName;
            errors.push(`${prefix}.${requiredLabel} is required when ${triggerLabel} is provided`);
        });
    });
    return errors;
}

function initialFormValue(fields: AgentViewField[], variant?: AgentViewVariant): Record<string, unknown> {
    const values: Record<string, unknown> = {};
    if (variant) {
        values._agent_view_variant = variant.id;
    }
    for (const field of [...fields, ...(variant?.fields || [])]) {
        if (field.value !== undefined) {
            values[field.name] = field.value;
        } else if (field.defaultValue !== undefined) {
            values[field.name] = field.defaultValue;
        } else if (field.type === "multiselect" || field.type === "array_table") {
            values[field.name] = [];
        } else if (field.type === "object_form") {
            values[field.name] = {};
        } else if (field.type === "boolean") {
            values[field.name] = false;
        } else {
            values[field.name] = "";
        }
    }
    return values;
}

function initialWizardValue(steps: AgentViewWizardStep[]): Record<string, unknown> {
    const values: Record<string, unknown> = {};
    for (const step of steps) {
        Object.assign(values, initialFormValue(step.fields));
    }
    return values;
}

function activeVariantFor(view: AgentView, variantId?: string): AgentViewVariant | undefined {
    if (view.type !== "form" || !view.variants || view.variants.length === 0) return undefined;
    return view.variants.find((variant) => variant.id === variantId) || view.variants[0];
}

function visibleFormFields(view: AgentView, variant?: AgentViewVariant): AgentViewField[] {
    if (view.type !== "form") return [];
    return [...view.fields, ...(variant?.fields || [])];
}

function formSubmissionPayload(fields: AgentViewField[], data: Record<string, unknown>, variant?: AgentViewVariant): Record<string, unknown> {
    const payload: Record<string, unknown> = {};
    if (variant) {
        payload._agent_view_variant = variant.id;
    }
    for (const field of fields) {
        if (Object.prototype.hasOwnProperty.call(data, field.name)) {
            payload[field.name] = data[field.name];
        }
    }
    return payload;
}

function wizardSubmissionPayload(steps: AgentViewWizardStep[], data: Record<string, unknown>): Record<string, unknown> {
    const payload: Record<string, unknown> = {};
    for (const step of steps) {
        for (const field of step.fields) {
            if (Object.prototype.hasOwnProperty.call(data, field.name)) {
                payload[field.name] = data[field.name];
            }
        }
    }
    return payload;
}

function initialResourceSelection(view: AgentView): string | string[] {
    if (view.type !== "resource_picker") return "";
    if (view.value !== undefined) return view.value;
    return view.multiple ? [] : "";
}

function initialFieldMapping(view: AgentView): Record<string, string> {
    if (view.type !== "field_mapper") return {};
    const existing = view.value || {};
    const sourceByLower = new Map(view.sourceFields.map((field) => [field.trim().toLowerCase(), field]));
    const mapping: Record<string, string> = {};
    for (const target of view.targetFields) {
        mapping[target.name] = existing[target.name] || sourceByLower.get(target.name.trim().toLowerCase()) || "";
    }
    return mapping;
}

function fieldMapperValidationErrors(fields: AgentViewField[], mapping: Record<string, string>): string[] {
    return fields
        .filter((field) => field.required && !stringsTrim(mapping[field.name]))
        .map((field) => `${field.label || field.name} needs a source field`);
}

function stringsTrim(value: unknown): string {
    return typeof value === "string" ? value.trim() : "";
}

function formatValue(value: unknown): string {
    if (value === null || value === undefined) return "";
    if (typeof value === "string") return value;
    if (typeof value === "number" || typeof value === "boolean") return String(value);
    return JSON.stringify(value, null, 2);
}

function renderDataValue(value: unknown, theme: Theme, depth = 0): React.ReactNode {
    if (value === null || value === undefined || value === "") {
        return <span style={{ color: theme.textMuted }}>-</span>;
    }
    if (typeof value === "string" || typeof value === "number" || typeof value === "boolean") {
        return <span>{String(value)}</span>;
    }
    if (Array.isArray(value)) {
        if (value.length === 0) return <span style={{ color: theme.textMuted }}>-</span>;
        return (
            <div style={{ display: "grid", gap: 4 }}>
                {value.slice(0, 8).map((item, index) => (
                    <div key={index} style={{ color: theme.text }}>
                        {renderDataValue(item, theme, depth + 1)}
                    </div>
                ))}
                {value.length > 8 && <span style={{ color: theme.textMuted }}>+{value.length - 8} more</span>}
            </div>
        );
    }
    if (typeof value === "object") {
        const entries = Object.entries(value as Record<string, unknown>);
        if (entries.length === 0) return <span style={{ color: theme.textMuted }}>-</span>;
        if (depth >= 2) return <span style={{ color: theme.textMuted }}>{entries.length} fields</span>;
        return (
            <div style={{ display: "grid", gap: 5 }}>
                {entries.slice(0, 10).map(([key, nested]) => (
                    <div key={key} style={{ display: "grid", gridTemplateColumns: "110px 1fr", gap: 8 }}>
                        <span style={{ color: theme.textMuted }}>{key}</span>
                        <span>{renderDataValue(nested, theme, depth + 1)}</span>
                    </div>
                ))}
                {entries.length > 10 && <span style={{ color: theme.textMuted }}>+{entries.length - 10} more fields</span>}
            </div>
        );
    }
    return <span>{String(value)}</span>;
}

function renderField(
    field: AgentViewField,
    value: unknown,
    setValue: (name: string, next: unknown) => void,
    theme: Theme,
    s: AgentViewStrings,
) {
    if (field.type === "hidden") return null;
    const label = field.label || field.name;
    const controlId = `agent-view-field-${field.name.replace(/[^A-Za-z0-9_-]/g, "_")}`;
    const commonInputStyle: React.CSSProperties = {
        width: "100%",
        boxSizing: "border-box",
        border: `1px solid ${theme.fieldBorder}`,
        background: theme.fieldBg,
        color: theme.inputText,
        borderRadius: 6,
        padding: "8px 10px",
        fontSize: 13,
        fontFamily: "inherit",
        outline: "none",
    };
    const readOnlyInputStyle: React.CSSProperties = field.readOnly ? { opacity: 0.72, cursor: "not-allowed" } : {};
    const labelStyle: React.CSSProperties = {
        display: "flex",
        gap: 6,
        alignItems: "baseline",
        color: theme.fieldLabel,
        fontSize: 12,
        fontWeight: 600,
    };
    const descriptionStyle: React.CSSProperties = {
        color: theme.textMuted,
        fontSize: 12,
        lineHeight: 1.4,
        marginTop: 4,
    };
    const errorStyle: React.CSSProperties = {
        color: theme.errorText,
        fontSize: 12,
        lineHeight: 1.4,
    };

    let control: React.ReactNode;
    if (field.type === "textarea") {
        control = (
            <textarea
                id={controlId}
                value={formatValue(value)}
                placeholder={field.placeholder}
                minLength={field.minLength}
                maxLength={field.maxLength}
                readOnly={field.readOnly}
                onChange={(event) => {
                    if (!field.readOnly) setValue(field.name, event.target.value);
                }}
                rows={4}
                style={{ ...commonInputStyle, ...readOnlyInputStyle, resize: "vertical", minHeight: 84 }}
            />
        );
    } else if (field.type === "object_form") {
        const objectValue = normalizeObjectValue(value);
        const columns = inferObjectColumns(field, objectValue);
        const updateObjectField = (column: AgentViewTableColumn, next: unknown) => {
            if (field.readOnly || column.readOnly) return;
            setValue(field.name, { ...objectValue, [column.name]: next });
        };
        control = (
            <div style={{ display: "grid", gap: 8, border: `1px solid ${theme.fieldBorder}`, borderRadius: 6, padding: 10, background: theme.fieldBg }}>
                {columns.map((column) => {
                    const nestedValue = objectValue[column.name];
                    return (
                        <label key={column.name} style={{ display: "grid", gridTemplateColumns: "120px 1fr", gap: 8, alignItems: "center" }}>
                            <span style={{ color: theme.fieldLabel, fontSize: 12 }}>
                                {column.label || column.name}
                                {column.required && <span style={{ color: theme.errorText }}> *</span>}
                            </span>
                            {column.type === "boolean" ? (
                                <input
                                    type="checkbox"
                                    checked={Boolean(nestedValue)}
                                    disabled={field.readOnly || column.readOnly}
                                    onChange={(event) => updateObjectField(column, event.target.checked)}
                                />
                            ) : column.type === "select" ? (
                                <select value={formatValue(nestedValue)} disabled={field.readOnly || column.readOnly} onChange={(event) => updateObjectField(column, event.target.value)} style={{ ...commonInputStyle, ...(field.readOnly || column.readOnly ? { opacity: 0.72, cursor: "not-allowed" } : {}) }}>
                                    <option value="">{s.selectPlaceholder}</option>
                                    {(column.options || []).map((option) => (
                                        <option key={optionValue(option)} value={optionValue(option)}>{optionLabel(option)}</option>
                                    ))}
                                </select>
                            ) : (
                                <input
                                    type={column.sensitive || isSensitiveFormat(column.format) ? "password" : column.type === "number" ? "number" : column.type === "date" ? "date" : "text"}
                                    value={formatValue(nestedValue)}
                                    min={column.min}
                                    max={column.max}
                                    step={column.step}
                                    minLength={column.minLength}
                                    maxLength={column.maxLength}
                                    pattern={column.pattern}
                                    readOnly={field.readOnly || column.readOnly}
                                    onChange={(event) => updateObjectField(column, column.type === "number" ? (event.target.value === "" ? "" : Number(event.target.value)) : event.target.value)}
                                    style={{ ...commonInputStyle, ...(field.readOnly || column.readOnly ? { opacity: 0.72, cursor: "not-allowed" } : {}) }}
                                />
                            )}
                        </label>
                    );
                })}
            </div>
        );
    } else if (field.type === "array_table") {
        const rows = normalizeTableRows(value);
        const columns = inferTableColumns(field, rows);
        const updateCell = (rowIndex: number, column: AgentViewTableColumn, next: unknown) => {
            if (field.readOnly || column.readOnly) return;
            const nextRows = rows.map((row, index) => index === rowIndex ? { ...row, [column.name]: next } : row);
            setValue(field.name, nextRows);
        };
        control = (
            <div style={{ display: "grid", gap: 8 }}>
                <div style={{ overflowX: "auto", border: `1px solid ${theme.fieldBorder}`, borderRadius: 6 }}>
                    <table style={{ width: "100%", borderCollapse: "collapse", minWidth: Math.max(360, columns.length * 130) }}>
                        <thead>
                            <tr>
                                {columns.map((column) => (
                                    <th key={column.name} style={{ textAlign: "left", color: theme.fieldLabel, fontSize: 12, padding: "7px 8px", borderBottom: `1px solid ${theme.divider}`, background: theme.titleBarBg }}>
                                        {column.label || column.name}
                                        {column.required && <span style={{ color: theme.errorText }}> *</span>}
                                    </th>
                                ))}
                                <th style={{ width: 44, borderBottom: `1px solid ${theme.divider}`, background: theme.titleBarBg }} />
                            </tr>
                        </thead>
                        <tbody>
                            {rows.map((row, rowIndex) => (
                                <tr key={rowIndex}>
                                    {columns.map((column) => {
                                        const cellValue = row[column.name];
                                        const cellReadOnly = field.readOnly || column.readOnly;
                                        const cellStyle: React.CSSProperties = { ...commonInputStyle, ...(cellReadOnly ? { opacity: 0.72, cursor: "not-allowed" } : {}), border: "none", borderRadius: 0, background: "transparent", padding: "7px 8px" };
                                        return (
                                            <td key={column.name} style={{ borderBottom: `1px solid ${theme.divider}`, verticalAlign: "top" }}>
                                                {column.type === "boolean" ? (
                                                    <input
                                                        type="checkbox"
                                                        checked={Boolean(cellValue)}
                                                        disabled={cellReadOnly}
                                                        onChange={(event) => updateCell(rowIndex, column, event.target.checked)}
                                                        style={{ margin: 8 }}
                                                    />
                                                ) : column.type === "select" ? (
                                                    <select value={formatValue(cellValue)} disabled={cellReadOnly} onChange={(event) => updateCell(rowIndex, column, event.target.value)} style={cellStyle}>
                                                        <option value="">{s.selectPlaceholder}</option>
                                                        {(column.options || []).map((option) => (
                                                            <option key={optionValue(option)} value={optionValue(option)}>{optionLabel(option)}</option>
                                                        ))}
                                                    </select>
                                                ) : (
                                                    <input
                                                        type={column.sensitive || isSensitiveFormat(column.format) ? "password" : column.type === "number" ? "number" : column.type === "date" ? "date" : "text"}
                                                        value={formatValue(cellValue)}
                                                        min={column.min}
                                                        max={column.max}
                                                        step={column.step}
                                                        minLength={column.minLength}
                                                        maxLength={column.maxLength}
                                                        pattern={column.pattern}
                                                        readOnly={cellReadOnly}
                                                        onChange={(event) => updateCell(rowIndex, column, column.type === "number" ? (event.target.value === "" ? "" : Number(event.target.value)) : event.target.value)}
                                                        style={cellStyle}
                                                    />
                                                )}
                                            </td>
                                        );
                                    })}
                                    <td style={{ borderBottom: `1px solid ${theme.divider}`, textAlign: "center" }}>
                                        <button
                                            type="button"
                                            disabled={field.readOnly}
                                            onClick={() => {
                                                if (!field.readOnly) setValue(field.name, rows.filter((_, index) => index !== rowIndex));
                                            }}
                                            style={{ border: "none", background: "transparent", color: theme.closeBtnColor, cursor: "pointer", padding: 8 }}
                                            aria-label={`Remove row ${rowIndex + 1}`}
                                        >
                                            x
                                        </button>
                                    </td>
                                </tr>
                            ))}
                            {rows.length === 0 && (
                                <tr>
                                    <td colSpan={columns.length + 1} style={{ color: theme.textMuted, fontSize: 12, padding: 10 }}>{s.noRows}</td>
                                </tr>
                            )}
                        </tbody>
                    </table>
                </div>
                <button
                    type="button"
                    disabled={field.readOnly}
                    onClick={() => {
                        if (!field.readOnly) setValue(field.name, [...rows, Object.fromEntries(columns.map((column) => [column.name, column.type === "boolean" ? false : column.type === "number" ? 0 : ""]))]);
                    }}
                    style={{ alignSelf: "flex-start", border: `1px solid ${theme.btnBorder}`, background: "transparent", color: theme.btnColor, borderRadius: 6, padding: "6px 9px", cursor: field.readOnly ? "not-allowed" : "pointer", opacity: field.readOnly ? 0.6 : 1, fontSize: 12 }}
                >
                    {s.addRow}
                </button>
            </div>
        );
    } else if (field.type === "multiselect") {
        const selectedValues = normalizeMultiValue(value);
        control = (
            <select
                id={controlId}
                multiple
                value={selectedValues}
                disabled={field.readOnly}
                onChange={(event) => {
                    if (field.readOnly) return;
                    const next = Array.from(event.currentTarget.selectedOptions).map((option) => option.value);
                    setValue(field.name, next);
                }}
                style={{ ...commonInputStyle, ...readOnlyInputStyle, minHeight: 96 }}
            >
                {(field.options || []).map((option) => (
                    <option key={optionValue(option)} value={optionValue(option)}>
                        {optionLabel(option)}
                    </option>
                ))}
            </select>
        );
    } else if (field.type === "select" || field.type === "business_ref" || field.type === "user_ref" || field.type === "department_ref") {
        control = (
            <select
                id={controlId}
                value={formatValue(value)}
                disabled={field.readOnly}
                onChange={(event) => {
                    if (!field.readOnly) setValue(field.name, event.target.value);
                }}
                style={{ ...commonInputStyle, ...readOnlyInputStyle }}
            >
                <option value="">{field.placeholder || s.selectPlaceholder}</option>
                {(field.options || []).map((option) => (
                    <option key={optionValue(option)} value={optionValue(option)}>
                        {optionLabel(option)}
                    </option>
                ))}
            </select>
        );
    } else if (field.type === "boolean") {
        control = (
            <label style={{ display: "inline-flex", alignItems: "center", gap: 8, color: theme.text }}>
                <input
                    id={controlId}
                    type="checkbox"
                    checked={Boolean(value)}
                    disabled={field.readOnly}
                    onChange={(event) => {
                        if (!field.readOnly) setValue(field.name, event.target.checked);
                    }}
                />
                {s.enabled}
            </label>
        );
    } else {
        const inputType = field.sensitive || isSensitiveFormat(field.format) ? "password" : field.type === "number" ? "number" : field.type === "date" ? "date" : field.type === "datetime" ? "datetime-local" : field.format === "email" ? "email" : field.format === "url" || field.format === "uri" ? "url" : field.type === "file" ? "text" : "text";
        control = (
            <input
                id={controlId}
                type={inputType}
                value={formatValue(value)}
                min={field.min}
                max={field.max}
                step={field.step}
                minLength={field.minLength}
                maxLength={field.maxLength}
                pattern={field.pattern}
                readOnly={field.readOnly}
                placeholder={field.placeholder}
                onChange={(event) => {
                    if (!field.readOnly) setValue(field.name, field.type === "number" ? (event.target.value === "" ? "" : Number(event.target.value)) : event.target.value);
                }}
                style={{ ...commonInputStyle, ...readOnlyInputStyle }}
            />
        );
    }

    return (
        <div key={field.name} style={{ display: "flex", flexDirection: "column", gap: 6 }}>
            <label htmlFor={controlId} style={labelStyle}>
                <span>{label}</span>
                {field.required && <span style={{ color: theme.errorText }}>*</span>}
            </label>
            {control}
            {field.error && <div style={errorStyle}>{field.error}</div>}
            {field.description && <div style={descriptionStyle}>{field.description}</div>}
        </div>
    );
}

function keyValueList(data: Record<string, unknown> | undefined, theme: Theme) {
    if (!data || Object.keys(data).length === 0) return null;
    return (
        <div style={{ display: "grid", gap: 8 }}>
            {Object.entries(data).map(([key, value]) => (
                <div key={key} style={{ display: "grid", gridTemplateColumns: "120px 1fr", gap: 8, alignItems: "start" }}>
                    <div style={{ color: theme.textMuted, fontSize: 12 }}>{key}</div>
                    <div style={{ color: theme.text, fontSize: 12, minWidth: 0, wordBreak: "break-word" }}>
                        {renderDataValue(value, theme)}
                    </div>
                </div>
            ))}
        </div>
    );
}

function isPanelHeaderInteractiveTarget(target: EventTarget | null, currentTarget: HTMLElement): boolean {
    if (!(target instanceof HTMLElement) || target === currentTarget) return false;
    return !!target.closest('button, a, input, select, textarea, [role="button"], [data-preview-no-maximize="true"]');
}

export function AgentTaskPanel({ view, onDismiss, onResizeStart, onToggleMaximize, onSubmit, theme, lang }: AgentTaskPanelProps) {
    const s = useMemo(() => agentViewStrings(lang || "en"), [lang]);
    const suppressNextHeaderDoubleClickRef = useRef(false);
    const [activeVariantId, setActiveVariantId] = useState<string | undefined>(() => {
        const variant = activeVariantFor(view);
        return variant?.id;
    });
    const [formData, setFormData] = useState<Record<string, unknown>>(() => {
        const variant = activeVariantFor(view, activeVariantId);
        return view.type === "form" ? initialFormValue(view.fields, variant) : {};
    });
    const [wizardStepIndex, setWizardStepIndex] = useState(0);
    const [wizardData, setWizardData] = useState<Record<string, unknown>>(() => view.type === "wizard" ? initialWizardValue(view.steps) : {});
    const [tableRows, setTableRows] = useState<Array<Record<string, unknown>>>(() => view.type === "table_editor" ? normalizeTableRows(view.rows) : []);
    const [resourceSelection, setResourceSelection] = useState<string | string[]>(() => initialResourceSelection(view));
    const [fieldMapping, setFieldMapping] = useState<Record<string, string>>(() => initialFieldMapping(view));
    const [submitting, setSubmitting] = useState(false);
    useEffect(() => {
        const variant = activeVariantFor(view);
        setActiveVariantId(variant?.id);
        setFormData(view.type === "form" ? initialFormValue(view.fields, variant) : {});
        setWizardStepIndex(0);
        setWizardData(view.type === "wizard" ? initialWizardValue(view.steps) : {});
        setTableRows(view.type === "table_editor" ? normalizeTableRows(view.rows) : []);
        setResourceSelection(initialResourceSelection(view));
        setFieldMapping(initialFieldMapping(view));
        setSubmitting(false);
    }, [view]);
    const activeVariant = activeVariantFor(view, activeVariantId);
    const renderedFields = visibleFormFields(view, activeVariant);
    const activeWizardStep = view.type === "wizard" ? view.steps[Math.min(wizardStepIndex, Math.max(0, view.steps.length - 1))] : undefined;

    const validationErrors = useMemo(() => {
        if (view.type !== "form") return [];
        const fields = visibleFormFields(view, activeVariant);
        return [
            ...fields.flatMap((field) => fieldValidationErrors(field, formData[field.name])),
            ...dependentRequiredErrors(fields, formData, { ...(view.dependentRequired || {}), ...(activeVariant?.dependentRequired || {}) }),
        ];
    }, [activeVariant, formData, view]);
    const wizardValidationErrors = useMemo(() => {
        if (view.type !== "wizard" || !activeWizardStep) return [];
        return [
            ...activeWizardStep.fields.flatMap((field) => fieldValidationErrors(field, wizardData[field.name])),
            ...dependentRequiredErrors(activeWizardStep.fields, wizardData, activeWizardStep.dependentRequired),
        ];
    }, [activeWizardStep, view, wizardData]);
    const allWizardValidationErrors = useMemo(() => {
        if (view.type !== "wizard") return [];
        return view.steps.flatMap((step) => [
            ...step.fields.flatMap((field) => fieldValidationErrors(field, wizardData[field.name])),
            ...dependentRequiredErrors(step.fields, wizardData, step.dependentRequired),
        ]);
    }, [view, wizardData]);
    const tableEditorField = useMemo<AgentViewField | undefined>(() => {
        if (view.type !== "table_editor") return undefined;
        return {
            name: "rows",
            label: "Rows",
            type: "array_table",
            columns: view.columns,
            value: tableRows,
            minItems: view.minItems,
            maxItems: view.maxItems,
            uniqueItems: view.uniqueItems,
            dependentRequired: view.dependentRequired,
        };
    }, [tableRows, view]);
    const tableValidationErrors = useMemo(() => tableEditorField ? fieldValidationErrors(tableEditorField, tableRows) : [], [tableEditorField, tableRows]);
    const resourceValidationErrors = useMemo(() => {
        if (view.type !== "resource_picker") return [];
        if (view.multiple) return normalizeMultiValue(resourceSelection).length === 0 ? [`Select at least one ${view.resourceType || "resource"}`] : [];
        return stringsTrim(resourceSelection) === "" ? [`Select a ${view.resourceType || "resource"}`] : [];
    }, [resourceSelection, view]);
    const fieldMappingErrors = useMemo(() => view.type === "field_mapper" ? fieldMapperValidationErrors(view.targetFields, fieldMapping) : [], [fieldMapping, view]);

    const setFieldValue = (name: string, next: unknown) => {
        setFormData((current) => ({ ...current, [name]: next }));
    };
    const setWizardFieldValue = (name: string, next: unknown) => {
        setWizardData((current) => ({ ...current, [name]: next }));
    };
    const setMappedSource = (targetName: string, sourceName: string) => {
        setFieldMapping((current) => ({ ...current, [targetName]: sourceName }));
    };

    const selectVariant = (variant: AgentViewVariant) => {
        setActiveVariantId(variant.id);
        setFormData((current) => {
            const base = initialFormValue(view.type === "form" ? view.fields : [], variant);
            for (const field of view.type === "form" ? view.fields : []) {
                if (current[field.name] !== undefined) base[field.name] = current[field.name];
            }
            base._agent_view_variant = variant.id;
            return base;
        });
    };
    const handleHeaderMouseDown = (event: React.MouseEvent<HTMLElement>) => {
        if (isPanelHeaderInteractiveTarget(event.target, event.currentTarget)) return;
        if (event.detail !== 2) return;
        event.preventDefault();
        suppressNextHeaderDoubleClickRef.current = true;
        onToggleMaximize?.();
    };
    const handleHeaderDoubleClick = (event: React.MouseEvent<HTMLElement>) => {
        if (isPanelHeaderInteractiveTarget(event.target, event.currentTarget)) return;
        if (suppressNextHeaderDoubleClickRef.current) {
            suppressNextHeaderDoubleClickRef.current = false;
            return;
        }
        onToggleMaximize?.();
    };
    const submitAgentView = async (nextData: Record<string, unknown>, overrideViewId?: string) => {
        if (submitting) return;
        setSubmitting(true);
        try {
            await onSubmit?.(overrideViewId || view.id, nextData);
        } finally {
            setSubmitting(false);
        }
    };

    const panelStyle: React.CSSProperties = {
        height: "100%",
        minWidth: 0,
        display: "flex",
        flexDirection: "column",
        background: theme.bg,
        color: theme.text,
        borderLeft: `1px solid ${theme.divider}`,
        fontFamily: "'Cascadia Code', 'Cascadia Mono', 'Consolas', 'Courier New', monospace",
    };
    const buttonStyle: React.CSSProperties = {
        border: `1px solid ${theme.btnBorder}`,
        background: "transparent",
        color: theme.btnColor,
        borderRadius: 6,
        padding: "7px 10px",
        cursor: "pointer",
        fontSize: 12,
    };
    const primaryButtonStyle: React.CSSProperties = {
        ...buttonStyle,
        background: theme.sendBtnBorder,
        color: theme.sendBtnColor,
    };

    return (
        <section style={panelStyle} data-testid="agent-task-panel">
            <div
                role="separator"
                aria-orientation="vertical"
                onMouseDown={onResizeStart}
                style={{ width: 6, cursor: "col-resize", position: "absolute", height: "100%" }}
            />
            <header
                onMouseDown={handleHeaderMouseDown}
                onDoubleClick={handleHeaderDoubleClick}
                style={{ display: "flex", alignItems: "center", justifyContent: "space-between", gap: 12, padding: "12px 14px", borderBottom: `1px solid ${theme.divider}`, background: theme.titleBarBg, "--wails-draggable": "no-drag" } as React.CSSProperties}
            >
                <div style={{ minWidth: 0 }}>
                    <div style={{ color: theme.titleText, fontWeight: 700, fontSize: 14, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                        {view.title}
                    </div>
                    {view.description && <div style={{ color: theme.textMuted, fontSize: 12, marginTop: 4, lineHeight: 1.4 }}>{view.description}</div>}
                </div>
                {onDismiss && (
                    <button type="button" onClick={() => onDismiss(view.id)} style={{ ...buttonStyle, borderColor: theme.divider, color: theme.closeBtnColor, "--wails-draggable": "no-drag" } as React.CSSProperties}>
                        {s.close}
                    </button>
                )}
            </header>
            <div style={{ flex: 1, minHeight: 0, overflow: "auto", padding: 14 }}>
                {view.type === "form" && (
                    <form
                        style={{ display: "flex", flexDirection: "column", gap: 14 }}
                        onSubmit={(event) => {
                            event.preventDefault();
                            if (validationErrors.length > 0 || submitting) return;
                            void submitAgentView(formSubmissionPayload(renderedFields, formData, activeVariant));
                        }}
                    >
                        {view.formErrors && view.formErrors.length > 0 && (
                            <div style={{ color: theme.errorText, background: theme.errorBg, border: `1px solid ${theme.errorBorder}`, borderRadius: 6, padding: 10, fontSize: 12, lineHeight: 1.5 }}>
                                {view.formErrors.map((error) => <div key={error}>{error}</div>)}
                            </div>
                        )}
                        {view.variants && view.variants.length > 0 && (
                            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
                                <label htmlFor="agent-view-variant-mode" style={{ color: theme.fieldLabel, fontSize: 12, fontWeight: 600 }}>{s.mode}</label>
                                <select
                                    id="agent-view-variant-mode"
                                    value={activeVariant?.id || ""}
                                    onChange={(event) => {
                                        const next = view.variants?.find((variant) => variant.id === event.target.value);
                                        if (next) selectVariant(next);
                                    }}
                                    style={{
                                        width: "100%",
                                        boxSizing: "border-box",
                                        border: `1px solid ${theme.fieldBorder}`,
                                        background: theme.fieldBg,
                                        color: theme.inputText,
                                        borderRadius: 6,
                                        padding: "8px 10px",
                                        fontSize: 13,
                                        fontFamily: "inherit",
                                        outline: "none",
                                    }}
                                >
                                    {view.variants.map((variant) => (
                                        <option key={variant.id} value={variant.id}>{variant.label}</option>
                                    ))}
                                </select>
                                {activeVariant?.description && <div style={{ color: theme.textMuted, fontSize: 12, lineHeight: 1.4 }}>{activeVariant.description}</div>}
                            </div>
                        )}
                        {renderedFields.map((field) => renderField(field, formData[field.name], setFieldValue, theme, s))}
                        {validationErrors.length > 0 && (
                            <div style={{ color: theme.errorText, background: theme.errorBg, border: `1px solid ${theme.errorBorder}`, borderRadius: 6, padding: 10, fontSize: 12 }}>
                                {s.pleaseFix}{validationErrors.join(", ")}
                            </div>
                        )}
                        <button type="submit" disabled={submitting} style={{ ...primaryButtonStyle, opacity: submitting ? 0.72 : 1, cursor: submitting ? "wait" : "pointer" }}>{submitting ? s.submitting : view.submitLabel || s.submit}</button>
                    </form>
                )}
                {view.type === "wizard" && activeWizardStep && (
                    <form
                        style={{ display: "flex", flexDirection: "column", gap: 14 }}
                        onSubmit={(event) => {
                            event.preventDefault();
                            if (submitting) return;
                            if (wizardStepIndex < view.steps.length - 1) {
                                if (wizardValidationErrors.length === 0) setWizardStepIndex((current) => Math.min(current + 1, view.steps.length - 1));
                                return;
                            }
                            if (allWizardValidationErrors.length > 0) return;
                            void submitAgentView(wizardSubmissionPayload(view.steps, wizardData));
                        }}
                    >
                        {view.formErrors && view.formErrors.length > 0 && (
                            <div style={{ color: theme.errorText, background: theme.errorBg, border: `1px solid ${theme.errorBorder}`, borderRadius: 6, padding: 10, fontSize: 12, lineHeight: 1.5 }}>
                                {view.formErrors.map((error) => <div key={error}>{error}</div>)}
                            </div>
                        )}
                        <div style={{ display: "grid", gap: 8 }}>
                            <div style={{ display: "flex", gap: 6 }}>
                                {view.steps.map((step, index) => (
                                    <button
                                        key={step.id}
                                        type="button"
                                        onClick={() => setWizardStepIndex(index)}
                                        style={{ flex: 1, height: 6, border: "none", borderRadius: 6, padding: 0, cursor: "pointer", background: index <= wizardStepIndex ? theme.sendBtnBorder : theme.divider }}
                                        aria-label={step.title}
                                    />
                                ))}
                            </div>
                            <div>
                                <div style={{ color: theme.fieldLabel, fontSize: 12 }}>{s.stepOf(wizardStepIndex + 1, view.steps.length)}</div>
                                <div style={{ fontWeight: 700, marginTop: 4 }}>{activeWizardStep.title}</div>
                                {activeWizardStep.description && <div style={{ color: theme.textMuted, fontSize: 12, lineHeight: 1.4, marginTop: 4 }}>{activeWizardStep.description}</div>}
                            </div>
                        </div>
                        {activeWizardStep.fields.map((field) => renderField(field, wizardData[field.name], setWizardFieldValue, theme, s))}
                        {wizardValidationErrors.length > 0 && (
                            <div style={{ color: theme.errorText, background: theme.errorBg, border: `1px solid ${theme.errorBorder}`, borderRadius: 6, padding: 10, fontSize: 12 }}>
                                {s.pleaseFix}{wizardValidationErrors.join(", ")}
                            </div>
                        )}
                        <div style={{ display: "flex", gap: 8, justifyContent: "space-between" }}>
                            <button type="button" disabled={wizardStepIndex === 0 || submitting} onClick={() => setWizardStepIndex((current) => Math.max(0, current - 1))} style={{ ...buttonStyle, opacity: wizardStepIndex === 0 || submitting ? 0.6 : 1, cursor: wizardStepIndex === 0 || submitting ? "not-allowed" : "pointer" }}>{s.back}</button>
                            <button type="submit" disabled={submitting} style={{ ...primaryButtonStyle, opacity: submitting ? 0.72 : 1, cursor: submitting ? "wait" : "pointer" }}>
                                {submitting ? s.submitting : wizardStepIndex < view.steps.length - 1 ? s.next : view.submitLabel || s.submit}
                            </button>
                        </div>
                    </form>
                )}
                {view.type === "table_editor" && tableEditorField && (
                    <form
                        style={{ display: "flex", flexDirection: "column", gap: 14 }}
                        onSubmit={(event) => {
                            event.preventDefault();
                            if (tableValidationErrors.length > 0 || submitting) return;
                            void submitAgentView({ ...(view.hiddenData || {}), [view.dataKey || "rows"]: tableRows });
                        }}
                    >
                        {view.formErrors && view.formErrors.length > 0 && (
                            <div style={{ color: theme.errorText, background: theme.errorBg, border: `1px solid ${theme.errorBorder}`, borderRadius: 6, padding: 10, fontSize: 12, lineHeight: 1.5 }}>
                                {view.formErrors.map((error) => <div key={error}>{error}</div>)}
                            </div>
                        )}
                        {renderField(tableEditorField, tableRows, (_name, next) => setTableRows(normalizeTableRows(next)), theme, s)}
                        {tableValidationErrors.length > 0 && (
                            <div style={{ color: theme.errorText, background: theme.errorBg, border: `1px solid ${theme.errorBorder}`, borderRadius: 6, padding: 10, fontSize: 12 }}>
                                {s.pleaseFix}{tableValidationErrors.join(", ")}
                            </div>
                        )}
                        <button type="submit" disabled={submitting} style={{ ...primaryButtonStyle, opacity: submitting ? 0.72 : 1, cursor: submitting ? "wait" : "pointer" }}>{submitting ? s.submitting : view.submitLabel || s.submit}</button>
                    </form>
                )}
                {view.type === "resource_picker" && (
                    <form
                        style={{ display: "flex", flexDirection: "column", gap: 14 }}
                        onSubmit={(event) => {
                            event.preventDefault();
                            if (resourceValidationErrors.length > 0 || submitting) return;
                            void submitAgentView({ ...(view.hiddenData || {}), [view.dataKey || "selected"]: resourceSelection });
                        }}
                    >
                        <select
                            multiple={view.multiple}
                            value={view.multiple ? normalizeMultiValue(resourceSelection) : stringsTrim(resourceSelection)}
                            onChange={(event) => {
                                if (view.multiple) {
                                    setResourceSelection(Array.from(event.currentTarget.selectedOptions).map((option) => option.value));
                                } else {
                                    setResourceSelection(event.target.value);
                                }
                            }}
                            style={{
                                width: "100%",
                                minHeight: view.multiple ? 150 : undefined,
                                boxSizing: "border-box",
                                border: `1px solid ${theme.fieldBorder}`,
                                background: theme.fieldBg,
                                color: theme.inputText,
                                borderRadius: 6,
                                padding: "8px 10px",
                                fontSize: 13,
                                fontFamily: "inherit",
                                outline: "none",
                            }}
                        >
                            {!view.multiple && <option value="">{s.selectPlaceholder}</option>}
                            {view.options.map((option) => (
                                <option key={option.value} value={option.value}>
                                    {[option.label, option.status].filter(Boolean).join(" - ")}
                                </option>
                            ))}
                        </select>
                        <div style={{ display: "grid", gap: 8 }}>
                            {view.options.filter((option) => view.multiple ? normalizeMultiValue(resourceSelection).includes(option.value) : resourceSelection === option.value).map((option) => (
                                <div key={option.value} style={{ border: `1px solid ${theme.divider}`, borderRadius: 8, padding: 10, background: theme.fieldBg }}>
                                    <div style={{ fontWeight: 700 }}>{option.label}</div>
                                    {option.description && <div style={{ color: theme.textMuted, fontSize: 12, marginTop: 4 }}>{option.description}</div>}
                                    {option.data && keyValueList(option.data, theme)}
                                </div>
                            ))}
                        </div>
                        {resourceValidationErrors.length > 0 && (
                            <div style={{ color: theme.errorText, background: theme.errorBg, border: `1px solid ${theme.errorBorder}`, borderRadius: 6, padding: 10, fontSize: 12 }}>
                                {s.pleaseFix}{resourceValidationErrors.join(", ")}
                            </div>
                        )}
                        <button type="submit" disabled={submitting} style={{ ...primaryButtonStyle, opacity: submitting ? 0.72 : 1, cursor: submitting ? "wait" : "pointer" }}>{submitting ? s.submitting : view.submitLabel || s.select}</button>
                    </form>
                )}
                {view.type === "field_mapper" && (
                    <form
                        style={{ display: "flex", flexDirection: "column", gap: 14 }}
                        onSubmit={(event) => {
                            event.preventDefault();
                            if (fieldMappingErrors.length > 0 || submitting) return;
                            void submitAgentView({ ...(view.hiddenData || {}), [view.dataKey || "mapping"]: fieldMapping });
                        }}
                    >
                        <div style={{ display: "grid", gap: 8 }}>
                            {view.targetFields.map((target) => (
                                <label key={target.name} style={{ display: "grid", gridTemplateColumns: "minmax(110px, 0.8fr) minmax(140px, 1fr)", gap: 10, alignItems: "center" }}>
                                    <span style={{ color: theme.fieldLabel, fontSize: 12, fontWeight: 600 }}>
                                        {target.label || target.name}
                                        {target.required && <span style={{ color: theme.errorText }}> *</span>}
                                    </span>
                                    <select
                                        value={fieldMapping[target.name] || ""}
                                        onChange={(event) => setMappedSource(target.name, event.target.value)}
                                        style={{
                                            width: "100%",
                                            boxSizing: "border-box",
                                            border: `1px solid ${theme.fieldBorder}`,
                                            background: theme.fieldBg,
                                            color: theme.inputText,
                                            borderRadius: 6,
                                            padding: "8px 10px",
                                            fontSize: 13,
                                            fontFamily: "inherit",
                                            outline: "none",
                                        }}
                                    >
                                        <option value="">{s.ignore}</option>
                                        {view.sourceFields.map((source) => (
                                            <option key={source} value={source}>{source}</option>
                                        ))}
                                    </select>
                                </label>
                            ))}
                        </div>
                        {fieldMappingErrors.length > 0 && (
                            <div style={{ color: theme.errorText, background: theme.errorBg, border: `1px solid ${theme.errorBorder}`, borderRadius: 6, padding: 10, fontSize: 12 }}>
                                {s.pleaseFix}{fieldMappingErrors.join(", ")}
                            </div>
                        )}
                        <button type="submit" disabled={submitting} style={{ ...primaryButtonStyle, opacity: submitting ? 0.72 : 1, cursor: submitting ? "wait" : "pointer" }}>{submitting ? s.submitting : view.submitLabel || s.applyMapping}</button>
                    </form>
                )}
                {view.type === "approval" && (
                    <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
                        <div style={{ border: `1px solid ${theme.divider}`, borderRadius: 8, padding: 12, background: theme.fieldBg }}>
                            <div style={{ fontWeight: 700, marginBottom: 8 }}>{view.action.summary}</div>
                            {view.action.risk && <div style={{ color: theme.textMuted, fontSize: 12 }}>{s.risk}: {view.action.risk}</div>}
                        </div>
                        {view.action.effects && view.action.effects.length > 0 && (
                            <div>
                                <div style={{ color: theme.fieldLabel, fontSize: 12, fontWeight: 700, marginBottom: 8 }}>{s.effects}</div>
                                <ul style={{ margin: 0, paddingLeft: 18 }}>{view.action.effects.map((effect) => <li key={effect}>{effect}</li>)}</ul>
                            </div>
                        )}
                        {view.action.reviewData && Object.keys(view.action.reviewData).length > 0 && (
                            <div>
                                <div style={{ color: theme.fieldLabel, fontSize: 12, fontWeight: 700, marginBottom: 8 }}>{s.data}</div>
                                {keyValueList(view.action.reviewData, theme)}
                            </div>
                        )}
                        {keyValueList(view.action.parameters, theme)}
                        <div style={{ display: "flex", gap: 8 }}>
                            <button type="button" disabled={submitting} style={{ ...primaryButtonStyle, opacity: submitting ? 0.72 : 1, cursor: submitting ? "wait" : "pointer" }} onClick={() => void submitAgentView({ approved: true, parameters: view.action.parameters || {} })}>{submitting ? s.submitting : view.approveLabel || s.approve}</button>
                            <button type="button" disabled={submitting} style={{ ...buttonStyle, opacity: submitting ? 0.72 : 1, cursor: submitting ? "wait" : "pointer" }} onClick={() => void submitAgentView({ approved: false, parameters: view.action.parameters || {} })}>{view.rejectLabel || s.reject}</button>
                        </div>
                    </div>
                )}
                {view.type === "progress" && (
                    <div style={{ display: "grid", gap: 10 }}>
                        {view.steps.map((step, index) => (
                            <div key={step.id || `${step.title}-${index}`} style={{ display: "grid", gridTemplateColumns: "72px 1fr", gap: 10, borderBottom: `1px solid ${theme.divider}`, paddingBottom: 10 }}>
                                <span style={{ color: step.status === "error" ? theme.errorText : theme.textMuted, fontSize: 12 }}>{step.status || s.pending}</span>
                                <div>
                                    <div style={{ fontWeight: 700 }}>{step.title}</div>
                                    {step.description && <div style={{ color: theme.textMuted, fontSize: 12, marginTop: 4 }}>{step.description}</div>}
                                </div>
                            </div>
                        ))}
                        {view.actions && view.actions.length > 0 && (
                            <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
                                {view.actions.map((action) => (
                                    <button
                                        key={`${action.viewId || view.id || "progress"}-${action.label}`}
                                        type="button"
                                        disabled={submitting}
                                        style={{ ...(action.primary ? primaryButtonStyle : buttonStyle), opacity: submitting ? 0.72 : 1, cursor: submitting ? "wait" : "pointer" }}
                                        onClick={() => void submitAgentView(action.data || {}, action.viewId)}
                                    >
                                        {submitting ? s.submitting : action.label}
                                    </button>
                                ))}
                            </div>
                        )}
                    </div>
                )}
                {view.type === "result_browser" && (
                    <div style={{ display: "grid", gap: 10 }}>
                        {view.results.map((result, index) => (
                            <article key={result.id || `${result.title}-${index}`} style={{ border: `1px solid ${theme.divider}`, borderRadius: 8, padding: 12, background: theme.fieldBg }}>
                                <div style={{ display: "flex", justifyContent: "space-between", gap: 8 }}>
                                    <strong>{result.title}</strong>
                                    {result.status && <span style={{ color: theme.textMuted, fontSize: 12 }}>{result.status}</span>}
                                </div>
                                {result.subtitle && <div style={{ color: theme.textMuted, fontSize: 12, marginTop: 4 }}>{result.subtitle}</div>}
                                {keyValueList(result.data, theme)}
                                {result.actions && result.actions.length > 0 && (
                                    <div style={{ display: "flex", gap: 8, marginTop: 10, flexWrap: "wrap" }}>
                                        {result.actions.map((action) => (
                                            <button
                                                key={`${action.viewId || view.id || "result"}-${action.label}`}
                                                type="button"
                                                disabled={submitting}
                                                style={{ ...(action.primary ? primaryButtonStyle : buttonStyle), opacity: submitting ? 0.72 : 1, cursor: submitting ? "wait" : "pointer" }}
                                                onClick={() => void submitAgentView(action.data || {}, action.viewId)}
                                            >
                                                {submitting ? s.submitting : action.label}
                                            </button>
                                        ))}
                                    </div>
                                )}
                            </article>
                        ))}
                    </div>
                )}
                {view.type === "artifact" && (
                    <div style={{ border: `1px solid ${theme.divider}`, borderRadius: 8, padding: 12, background: theme.fieldBg }}>
                        <div style={{ fontWeight: 700 }}>{view.artifact.label || view.title}</div>
                        {view.artifact.kind && <div style={{ color: theme.textMuted, fontSize: 12, marginTop: 4 }}>{s.kind}: {view.artifact.kind}</div>}
                        {view.artifact.uri && <div style={{ color: theme.linkColor, fontSize: 12, marginTop: 4, wordBreak: "break-all" }}>{view.artifact.uri}</div>}
                        {view.artifact.summary && <div style={{ color: theme.text, fontSize: 13, marginTop: 10, whiteSpace: "pre-wrap" }}>{view.artifact.summary}</div>}
                    </div>
                )}
            </div>
        </section>
    );
}
