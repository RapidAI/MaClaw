import type { CSSProperties, ReactNode } from "react";

const hubSpinnerStyle: CSSProperties = {
    width: 10,
    height: 10,
    borderRadius: "50%",
    border: "2px solid rgba(255,255,255,0.45)",
    borderTopColor: "#fff",
    display: "inline-block",
    animation: "spin 0.8s linear infinite",
    flexShrink: 0,
};

type Translator = (zh: string, en: string) => string;

export function HubStatusBadge({ connecting, t }: { connecting: boolean; t: Translator }) {
    if (connecting) {
        return (
            <>
                <span aria-hidden="true" style={{ ...hubSpinnerStyle, width: 9, height: 9, borderColor: "rgba(37,99,235,0.25)", borderTopColor: "#2563eb" }} />
                <span>{t("Hub 连接中", "Hub connecting")}</span>
            </>
        );
    }
    return (
        <>
            <span aria-hidden="true" style={{ display: "inline-flex", alignItems: "center", justifyContent: "center", minWidth: 18, height: 18, padding: "0 6px", borderRadius: 999, background: "rgba(34,197,94,0.14)", color: "#16a34a", fontWeight: 700 }}>✓</span>
            <span>{t("Hub 已连接", "Hub connected")}</span>
        </>
    );
}

export function HubRegisterButtonContent({ regBusy, regDone, hubConnecting, t }: { regBusy: boolean; regDone: boolean; hubConnecting: boolean; t: Translator }): ReactNode {
    if (regBusy) {
        return <>{t("注册中...", "Registering...")}</>;
    }
    if (regDone) {
        if (hubConnecting) {
            return <><span aria-hidden="true" style={hubSpinnerStyle} />{t("✅ 已注册 · 连接 Hub 中...", "✅ Registered · Hub connecting...")}</>;
        }
        return <>{t("✅ 已注册", "✅ Registered")}</>;
    }
    return <>{t("注册", "Register")}</>;
}
