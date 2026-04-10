import type { CSSProperties, ReactNode } from "react";

const hubSpinnerStyle: CSSProperties = {
    width: 10,
    height: 10,
    borderRadius: "50%",
    border: "2px solid var(--theme-primary-soft, rgba(99,102,241,0.45))",
    borderTopColor: "var(--theme-primary)",
    display: "inline-block",
    animation: "spin 0.8s linear infinite",
    flexShrink: 0,
};

type Translator = (en: string, zhHans: string, zhHant?: string) => string;

export function HubStatusBadge({ connecting, t }: { connecting: boolean; t: Translator }) {
    if (connecting) {
        return (
            <>
                <span aria-hidden="true" style={{ ...hubSpinnerStyle, width: 9, height: 9, borderColor: "var(--theme-primary-soft)", borderTopColor: "var(--theme-primary)" }} />
                <span>{t("Hub connecting", "Hub 连接中", "Hub 連接中")}</span>
            </>
        );
    }
    return (
        <>
            <span aria-hidden="true" style={{ display: "inline-flex", alignItems: "center", justifyContent: "center", minWidth: 18, height: 18, padding: "0 6px", borderRadius: 999, background: "var(--theme-success-bg)", color: "var(--theme-success)", fontWeight: 700 }}>✓</span>
            <span>{t("Hub connected", "Hub 已连接", "Hub 已連接")}</span>
        </>
    );
}

export function HubRegisterButtonContent({ regBusy, regDone, hubConnecting, t }: { regBusy: boolean; regDone: boolean; hubConnecting: boolean; t: Translator }): ReactNode {
    if (regBusy) {
        return <>{t("Registering...", "注册中...", "註冊中...")}</>;
    }
    if (regDone) {
        if (hubConnecting) {
            return <><span aria-hidden="true" style={hubSpinnerStyle} />{t("✅ Registered · Hub connecting...", "✅ 已注册 · 连接 Hub 中...", "✅ 已註冊 · 連接 Hub 中...")}</>;
        }
        return <>{t("✅ Registered", "✅ 已注册", "✅ 已註冊")}</>;
    }
    return <>{t("Register", "注册", "註冊")}</>;
}
