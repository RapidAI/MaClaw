import type { ReactNode } from "react";

type Translator = (en: string, zhHans: string, zhHant?: string) => string;

export function HubStatusBadge({ connecting, t }: { connecting: boolean; t: Translator }) {
    if (connecting) {
        return (
            <>
                <span aria-hidden="true" className="hub-status-spinner hub-status-spinner--small" />
                <span>{t("Hub connecting", "Hub 连接中", "Hub 連接中")}</span>
            </>
        );
    }
    return (
        <>
            <span aria-hidden="true" className="hub-status-check">✓</span>
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
            return <><span aria-hidden="true" className="hub-status-spinner" />{t("✅ Registered · Hub connecting...", "✅ 已注册 · 连接 Hub 中...", "✅ 已註冊 · 連接 Hub 中...")}</>;
        }
        return <>{t("✅ Registered", "✅ 已注册", "✅ 已註冊")}</>;
    }
    return <>{t("Register", "注册", "註冊")}</>;
}
