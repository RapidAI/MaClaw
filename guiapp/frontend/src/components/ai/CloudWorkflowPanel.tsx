import { useEffect, useMemo, useState, type CSSProperties } from "react";
import { localizeText } from "../../i18n";
import { remoteAssistantSend, type CloudWorkflowFormField, type RemoteAssistantSession } from "../layout/remoteAssistantSession";
import { submitCloudWorkflowForm } from "../layout/cloudAgentAssistant";
import type { Theme } from "./aiAssistantPanelTheme";
import { formFieldInputStyle, formFieldLabelColor, primaryFilledButtonStyle } from "./aiAssistantPanelTheme";

/**
 * The cloud task's V2 workflow surface. It mirrors the local task panel's
 * three gates — phase list, the gated phase's input form, and the
 * confirm/cancel review — onto the instance's workflow_state frames. The
 * machine lives on the instance, so every action here is a cloud op or a
 * message, never a local binding.
 */
export function CloudWorkflowPanel({ session, lang, theme: t }: { session: RemoteAssistantSession; lang: string; theme: Theme }) {
    const workflow = session.cloudWorkflow;
    const phases = workflow?.phases || [];
    const [formValues, setFormValues] = useState<Record<string, unknown>>({});
    const [variantID, setVariantID] = useState("");
    const [submitting, setSubmitting] = useState(false);
    const schema = workflow?.awaiting_form ? workflow.form_schema : null;
    const variant = useMemo(() => {
        const variants = schema?.variants || [];
        if (!variants.length) return null;
        return variants.find(item => item.id === variantID) || variants[0];
    }, [schema, variantID]);
    const fields: CloudWorkflowFormField[] = variant ? variant.fields : (schema?.fields || []);
    // A new gated phase replaces the draft — stale values from the previous
    // form must not leak onto the next schema's names.
    const formKey = `${workflow?.current_phase || ""}|${workflow?.awaiting_form ? "1" : "0"}`;
    useEffect(() => {
        setFormValues({});
        setVariantID("");
        setSubmitting(false);
    }, [formKey]);

    if (!session.cloud || !workflow || !phases.length) return null;
    const status = String(workflow.status || "");
    const active = phases.find(phase => phase.id === workflow.current_phase) || phases.find(phase => phase.status === "running" || phase.status === "waiting_confirm");

    const setValue = (name: string, value: unknown) => setFormValues(current => ({ ...current, [name]: value }));
    const fieldValue = (field: CloudWorkflowFormField): unknown => {
        if (Object.prototype.hasOwnProperty.call(formValues, field.name)) return formValues[field.name];
        return field.default ?? (field.type === "boolean" ? false : field.type === "multiselect" ? [] : "");
    };

    const missingRequired = fields.filter(field => field.required && field.type !== "hidden" && field.type !== "boolean")
        .filter(field => {
            const value = fieldValue(field);
            if (Array.isArray(value)) return value.length === 0;
            return !String(value ?? "").trim();
        });

    const submitForm = () => {
        if (submitting || missingRequired.length) return;
        const values: Record<string, unknown> = {};
        for (const field of fields) {
            if (field.type === "hidden") continue;
            values[field.name] = fieldValue(field);
        }
        if (variant) values._agent_view_variant = variant.id;
        setSubmitting(true);
        void submitCloudWorkflowForm(values).finally(() => setSubmitting(false));
    };

    const reviewSend = (text: string) => { void remoteAssistantSend(text); };

    const container: CSSProperties = {
        margin: "0 auto 8px",
        width: "min(100%, 720px)",
        border: `1px solid ${t.inputBarBorder}`,
        borderRadius: "12px",
        background: t.inputBarBg,
        padding: "10px 14px",
        boxSizing: "border-box",
    };
    const phaseRow = (phase: { id?: string; name?: string; status?: string }, index: number) => {
        const statusText = String(phase.status || "pending");
        const isActive = phase.id === workflow.current_phase;
        const tone = statusText === "completed" ? "#3ba55d"
            : statusText === "skipped" || statusText === "cancelled" ? t.textMuted
            : isActive || statusText === "waiting_confirm" || statusText === "running" || statusText === "executing" ? t.btnColor
            : t.textMuted;
        const mark = statusText === "completed" ? "✓"
            : statusText === "skipped" ? "–"
            : statusText === "cancelled" ? "✕"
            : isActive ? "●" : "○";
        return (
            <div key={phase.id || index} style={{ display: "flex", alignItems: "center", gap: 8, padding: "2px 0", color: tone }}>
                <span style={{ width: 14, textAlign: "center", flexShrink: 0 }}>{mark}</span>
                <span style={{ fontSize: 12, fontWeight: isActive ? 600 : 400, color: isActive ? t.text : tone }}>
                    {phase.name || phase.id}
                </span>
                {statusText === "waiting_confirm" && (
                    <span style={{ fontSize: 11, color: t.textMuted }}>{localizeText(lang, "awaiting review", "待确认", "待確認")}</span>
                )}
            </div>
        );
    };

    const renderField = (field: CloudWorkflowFormField) => {
        if (!field.name || field.type === "hidden") return null;
        const type = field.type || "text";
        const value = fieldValue(field);
        const label = (
            <div style={{ fontSize: 12, color: formFieldLabelColor(t), marginBottom: 4 }}>
                {field.label || field.name}{field.required ? " *" : ""}
                {field.description ? <span style={{ marginLeft: 6, color: t.textMuted, fontSize: 11 }}>{field.description}</span> : null}
            </div>
        );
        const inputStyle = { ...formFieldInputStyle(t), width: "100%", boxSizing: "border-box" as const, borderRadius: 8, padding: "7px 10px", fontSize: 13 };
        let control: React.ReactNode;
        if (type === "textarea") {
            control = <textarea value={String(value ?? "")} placeholder={field.placeholder} rows={3} onChange={event => setValue(field.name, event.target.value)} style={{ ...inputStyle, resize: "vertical" }} />;
        } else if (type === "select") {
            control = (
                <select value={String(value ?? "")} onChange={event => setValue(field.name, event.target.value)} style={inputStyle}>
                    <option value="">{field.placeholder || localizeText(lang, "Select", "请选择", "請選擇")}</option>
                    {(field.options || []).map(option => <option key={option.value} value={option.value}>{option.label || option.value}</option>)}
                </select>
            );
        } else if (type === "multiselect") {
            const selected = Array.isArray(value) ? value.map(item => String(item)) : [];
            control = (
                <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                    {(field.options || []).map(option => {
                        const on = selected.includes(option.value);
                        return (
                            <button
                                key={option.value}
                                type="button"
                                onClick={() => setValue(field.name, on ? selected.filter(item => item !== option.value) : [...selected, option.value])}
                                style={{
                                    border: `1px solid ${on ? t.btnColor : t.fieldBorder}`,
                                    background: on ? `color-mix(in srgb, ${t.btnColor} 14%, ${t.fieldBg})` : t.fieldBg,
                                    color: on ? t.text : t.textMuted,
                                    borderRadius: 999, padding: "4px 12px", fontSize: 12, cursor: "pointer",
                                }}
                            >{option.label || option.value}</button>
                        );
                    })}
                </div>
            );
        } else if (type === "boolean") {
            control = (
                <label style={{ display: "inline-flex", alignItems: "center", gap: 6, fontSize: 13, color: t.text, cursor: "pointer" }}>
                    <input type="checkbox" checked={value === true} onChange={event => setValue(field.name, event.target.checked)} />
                    {field.placeholder || localizeText(lang, "Yes", "是", "是")}
                </label>
            );
        } else {
            // text/number/date/file — a file field types an instance path.
            const inputType = type === "number" ? "number" : type === "date" ? "date" : "text";
            const placeholder = field.placeholder || (type === "file" ? localizeText(lang, "Path inside the instance", "实例内的文件路径", "實例內的文件路徑") : "");
            control = <input type={inputType} value={String(value ?? "")} placeholder={placeholder} onChange={event => setValue(field.name, type === "number" ? event.target.value : event.target.value)} style={inputStyle} />;
        }
        return <div key={field.name} style={{ marginTop: 8 }}>{label}{control}</div>;
    };

    return (
        <div style={container} data-testid="cloud-workflow-panel">
            <div style={{ display: "flex", alignItems: "center", gap: 8, marginBottom: 6 }}>
                <span style={{ fontSize: 12, fontWeight: 600, color: t.text }}>
                    {localizeText(lang, "Workflow", "工作流", "工作流")}
                    {workflow.type ? ` · ${workflow.type}` : ""}
                </span>
                <span style={{ fontSize: 11, color: t.textMuted }}>
                    {status === "completed" ? localizeText(lang, "completed", "已完成", "已完成")
                        : status === "cancelled" ? localizeText(lang, "cancelled", "已取消", "已取消")
                        : workflow.suspended ? localizeText(lang, "paused", "已暂停", "已暫停")
                        : active?.name || ""}
                </span>
            </div>
            <div>{phases.map(phaseRow)}</div>
            {workflow.suspended && status === "active" && (
                <div style={{ marginTop: 8, display: "flex", alignItems: "center", gap: 8 }}>
                    <span style={{ fontSize: 12, color: t.textMuted }}>{localizeText(lang, "This workflow was paused. Resume it?", "该工作流已暂停。要继续吗？", "該工作流已暫停。要繼續嗎？")}</span>
                    <button type="button" onClick={() => reviewSend("继续")} style={{ ...primaryFilledButtonStyle(t), borderRadius: 8, padding: "5px 14px", fontSize: 12, cursor: "pointer" }}>
                        {localizeText(lang, "Resume", "继续", "繼續")}
                    </button>
                    <button type="button" onClick={() => reviewSend("取消")} style={{ border: `1px solid ${t.fieldBorder}`, background: "transparent", color: t.textMuted, borderRadius: 8, padding: "5px 14px", fontSize: 12, cursor: "pointer" }}>
                        {localizeText(lang, "Cancel", "取消", "取消")}
                    </button>
                </div>
            )}
            {workflow.pending_review && !workflow.suspended && status === "active" && (
                <div style={{ marginTop: 8, display: "flex", alignItems: "center", gap: 8, flexWrap: "wrap" }}>
                    <span style={{ fontSize: 12, color: t.text }}>
                        {localizeText(lang, "Phase output is waiting for your review.", "该阶段产出等待你确认。", "該階段產出等待你確認。")}
                    </span>
                    <button type="button" onClick={() => reviewSend("确认")} style={{ ...primaryFilledButtonStyle(t), borderRadius: 8, padding: "5px 14px", fontSize: 12, cursor: "pointer" }}>
                        {localizeText(lang, "Confirm", "确认", "確認")}
                    </button>
                    <button type="button" onClick={() => reviewSend("取消")} style={{ border: `1px solid ${t.fieldBorder}`, background: "transparent", color: t.textMuted, borderRadius: 8, padding: "5px 14px", fontSize: 12, cursor: "pointer" }}>
                        {localizeText(lang, "Cancel workflow", "取消工作流", "取消工作流")}
                    </button>
                    <span style={{ fontSize: 11, color: t.textMuted }}>{localizeText(lang, "Type feedback to regenerate.", "回复修改意见可重新生成。", "回覆修改意見可重新生成。")}</span>
                </div>
            )}
            {schema && !workflow.suspended && status === "active" && (
                <div style={{ marginTop: 8, borderTop: `1px solid ${t.inputBarBorder}`, paddingTop: 8 }}>
                    {schema.title ? <div style={{ fontSize: 12, fontWeight: 600, color: t.text }}>{schema.title}</div> : null}
                    {schema.description ? <div style={{ fontSize: 11, color: t.textMuted, marginTop: 2 }}>{schema.description}</div> : null}
                    {(schema.variants || []).length > 0 && (
                        <div style={{ display: "flex", gap: 6, marginTop: 8 }}>
                            {(schema.variants || []).map(item => (
                                <button
                                    key={item.id}
                                    type="button"
                                    onClick={() => { setVariantID(item.id); setFormValues({}); }}
                                    style={{
                                        border: `1px solid ${item.id === (variant?.id || "") ? t.btnColor : t.fieldBorder}`,
                                        background: item.id === (variant?.id || "") ? `color-mix(in srgb, ${t.btnColor} 14%, ${t.fieldBg})` : t.fieldBg,
                                        color: t.text, borderRadius: 8, padding: "4px 12px", fontSize: 12, cursor: "pointer",
                                    }}
                                >{item.label || item.id}</button>
                            ))}
                        </div>
                    )}
                    {fields.map(renderField)}
                    <div style={{ marginTop: 10, display: "flex", alignItems: "center", gap: 8 }}>
                        <button
                            type="button"
                            disabled={submitting || missingRequired.length > 0}
                            onClick={submitForm}
                            style={{ ...primaryFilledButtonStyle(t), borderRadius: 8, padding: "6px 18px", fontSize: 12, cursor: submitting || missingRequired.length ? "default" : "pointer", opacity: submitting || missingRequired.length ? 0.6 : 1 }}
                        >
                            {submitting ? localizeText(lang, "Submitting…", "提交中…", "提交中…") : localizeText(lang, "Submit", "提交", "提交")}
                        </button>
                        {missingRequired.length > 0 && (
                            <span style={{ fontSize: 11, color: t.textMuted }}>{localizeText(lang, "Fill in the required fields", "请填写必填项", "請填寫必填項")}</span>
                        )}
                    </div>
                </div>
            )}
        </div>
    );
}
