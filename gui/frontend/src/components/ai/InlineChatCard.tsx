import { useState, useCallback, type CSSProperties } from "react";
import { IconAlert } from "./WorkbenchIcons";

// --- Generic Inline Chat Card System ---
// Renders interactive cards within the chat message stream.
// Each card type defines its own fields and actions.

export interface InlineChatCardField {
    key: string;
    label: string;
    type: "text" | "textarea" | "select";
    defaultValue?: string;
    placeholder?: string;
    required?: boolean;
    options?: string[];
}

export interface InlineChatCardAction {
    key: string;
    label: string;
    style?: "primary" | "default" | "danger";
}

export interface InlineChatCardData {
    id: string;
    type: string; // "skill_recording_done" | "ask_user" | "confirm" | ...
    title?: string;
    description?: string;
    fields?: InlineChatCardField[];
    actions: InlineChatCardAction[];
    metadata?: Record<string, any>;
    resolved?: boolean;
    resolvedAction?: string;
    resolvedValues?: Record<string, string>;
}

interface InlineChatCardProps {
    card: InlineChatCardData;
    onResolve: (cardId: string, action: string, values: Record<string, string>) => void;
    theme: {
        cardBg: string;
        cardBorder: string;
        textColor: string;
        mutedColor: string;
        accentColor: string;
        inputBg: string;
        inputBorder: string;
        buttonBg: string;
        buttonText: string;
        dangerColor: string;
    };
    lang?: string;
}

export function InlineChatCard({ card, onResolve, theme, lang = "zh" }: InlineChatCardProps) {
    const [values, setValues] = useState<Record<string, string>>(() => {
        const defaults: Record<string, string> = {};
        for (const field of card.fields || []) {
            defaults[field.key] = field.defaultValue || "";
        }
        return defaults;
    });

    const handleFieldChange = useCallback((key: string, value: string) => {
        setValues(prev => ({ ...prev, [key]: value }));
    }, []);

    const handleAction = useCallback((actionKey: string) => {
        onResolve(card.id, actionKey, values);
    }, [card.id, onResolve, values]);

    const isResolved = card.resolved === true;

    const containerStyle: CSSProperties = {
        background: theme.cardBg,
        border: `1px solid ${theme.cardBorder}`,
        borderRadius: "10px",
        padding: "14px 16px",
        margin: "8px 0",
        opacity: isResolved ? 0.7 : 1,
        pointerEvents: isResolved ? "none" : "auto",
        transition: "opacity 200ms ease",
    };

    const titleStyle: CSSProperties = {
        fontSize: "13px",
        fontWeight: 600,
        color: theme.textColor,
        marginBottom: card.description ? "4px" : "10px",
    };

    const descStyle: CSSProperties = {
        fontSize: "12px",
        color: theme.mutedColor,
        marginBottom: "10px",
        lineHeight: 1.4,
    };

    const fieldLabelStyle: CSSProperties = {
        fontSize: "11px",
        fontWeight: 500,
        color: theme.mutedColor,
        marginBottom: "3px",
    };

    const inputStyle: CSSProperties = {
        width: "100%",
        padding: "6px 10px",
        fontSize: "12px",
        borderRadius: "6px",
        border: `1px solid ${theme.inputBorder}`,
        background: theme.inputBg,
        color: theme.textColor,
        outline: "none",
        boxSizing: "border-box",
    };

    const metadataStyle: CSSProperties = {
        fontSize: "11px",
        color: theme.mutedColor,
        lineHeight: 1.5,
        margin: "8px 0",
        padding: "8px 10px",
        background: `${theme.inputBg}`,
        borderRadius: "6px",
        maxHeight: "120px",
        overflow: "auto",
        fontFamily: "monospace",
    };

    const actionsStyle: CSSProperties = {
        display: "flex",
        gap: "8px",
        marginTop: "12px",
        justifyContent: "flex-end",
    };

    return (
        <div style={containerStyle} data-testid={`inline-card-${card.type}`}>
            {card.title && <div style={titleStyle}>{card.title}</div>}
            {card.description && <div style={descStyle}>{card.description}</div>}

            {/* Metadata display (e.g. operation summary) */}
            {card.metadata?.summary && Array.isArray(card.metadata.summary) && (
                <div style={metadataStyle}>
                    {(card.metadata.summary as string[]).map((line, i) => (
                        <div key={i}>{line}</div>
                    ))}
                </div>
            )}

            {/* Security warnings */}
            {card.metadata?.security_warnings && Array.isArray(card.metadata.security_warnings) && card.metadata.security_warnings.length > 0 && (
                <div style={{ ...metadataStyle, background: "rgba(220, 38, 38, 0.08)", border: "1px solid rgba(220, 38, 38, 0.2)", color: theme.dangerColor }}>
                    <div style={{ fontWeight: 600, marginBottom: "4px", display: "flex", alignItems: "center", gap: 6 }}>
                        <IconAlert size={14} color="currentColor" />
                        {lang === "en" ? "Security Notice" : "安全提示"}
                    </div>
                    {(card.metadata.security_warnings as string[]).map((w, i) => (
                        <div key={i} style={{ fontSize: "10px" }}>{w}</div>
                    ))}
                    <div style={{ marginTop: "4px", fontSize: "10px", opacity: 0.8 }}>
                        {lang === "en"
                            ? "Review the recorded steps before saving. Credentials in commands/files will be saved to disk."
                            : "保存前请检查录制步骤。命令/文件中的凭据将被保存到磁盘。"}
                    </div>
                </div>
            )}

            {/* Fields */}
            {(card.fields || []).map(field => (
                <div key={field.key} style={{ marginBottom: "10px" }}>
                    <div style={fieldLabelStyle}>{field.label}</div>
                    {field.type === "textarea" ? (
                        <textarea
                            value={values[field.key] || ""}
                            onChange={e => handleFieldChange(field.key, e.target.value)}
                            placeholder={field.placeholder}
                            style={{ ...inputStyle, resize: "vertical", minHeight: "60px" }}
                            disabled={isResolved}
                        />
                    ) : field.type === "select" ? (
                        <select
                            value={values[field.key] || ""}
                            onChange={e => handleFieldChange(field.key, e.target.value)}
                            style={inputStyle}
                            disabled={isResolved}
                        >
                            {(field.options || []).map(opt => (
                                <option key={opt} value={opt}>{opt}</option>
                            ))}
                        </select>
                    ) : (
                        <input
                            type="text"
                            value={values[field.key] || ""}
                            onChange={e => handleFieldChange(field.key, e.target.value)}
                            placeholder={field.placeholder}
                            style={inputStyle}
                            disabled={isResolved}
                        />
                    )}
                </div>
            ))}

            {/* Resolved state indicator */}
            {isResolved && card.resolvedAction && (
                <div style={{ fontSize: "12px", color: card.resolvedAction === "cancel" ? theme.mutedColor : theme.accentColor, marginTop: "8px", fontWeight: 500 }}>
                    {card.resolvedAction === "save" ? (lang === "en" ? "Skill saved successfully." : "Skill 已保存成功。") :
                     card.resolvedAction === "cancel" ? (lang === "en" ? "Dismissed." : "已关闭。") :
                     String(card.resolvedAction)}
                </div>
            )}

            {/* Action buttons */}
            {!isResolved && (
                <div style={actionsStyle}>
                    {card.actions.map(action => {
                        const isPrimary = action.style === "primary";
                        const isDanger = action.style === "danger";
                        const btnStyle: CSSProperties = {
                            padding: "6px 16px",
                            fontSize: "12px",
                            fontWeight: 500,
                            borderRadius: "6px",
                            cursor: "pointer",
                            border: isPrimary ? "none" : `1px solid ${theme.cardBorder}`,
                            background: isPrimary ? theme.accentColor : isDanger ? theme.dangerColor : "transparent",
                            color: isPrimary || isDanger ? "#fff" : theme.textColor,
                            transition: "all 150ms ease",
                        };
                        return (
                            <button
                                key={action.key}
                                onClick={() => handleAction(action.key)}
                                style={btnStyle}
                            >
                                {action.label}
                            </button>
                        );
                    })}
                </div>
            )}
        </div>
    );
}
