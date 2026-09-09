import { colors, radius } from "./styles";

export type LLMConfigToastData = {
    ok: boolean;
    title: string;
    detail?: string;
};

interface LLMConfigToastProps {
    toast: LLMConfigToastData | null;
}

export function LLMConfigToast({ toast }: LLMConfigToastProps) {
    if (!toast) return null;

    return (
        <div role={toast.ok ? "status" : "alert"} aria-live="polite" style={{
            position: "fixed", top: 42, left: "50%", transform: "translateX(-50%)",
            zIndex: "var(--z-toast, 10050)", width: "min(92vw, 380px)", padding: "8px 12px",
            borderRadius: radius.md, fontSize: "0.74rem", lineHeight: 1.45,
            background: colors.surface,
            border: `1px solid ${toast.ok ? colors.success : colors.danger}`,
            boxShadow: "var(--shadow-md, 0 4px 12px -2px rgba(30,58,95,0.12))",
            color: colors.text,
        }}>
            <div style={{ fontWeight: 700, color: toast.ok ? colors.success : colors.danger }}>
                {toast.ok ? "OK" : "ERR"} {toast.title}
            </div>
            {toast.detail && (
                <div style={{ marginTop: 3, color: colors.textSecondary, whiteSpace: "pre-wrap", wordBreak: "break-word" }}>
                    {toast.detail}
                </div>
            )}
        </div>
    );
}
