import { useEffect, useId, type CSSProperties } from "react";
import { colors } from "./styles";
import { wizardGhostButtonStyle, wizardPrimaryButtonStyle } from "./OnboardingWizardShared";
import { useSafeBackdropDismiss } from "../../hooks/useSafeBackdropDismiss";

type Translator = (zh: string, en: string, zhHant?: string) => string;

interface Props {
    onBackToOnline: () => void;
    onClose: () => void;
    t: Translator;
}

export function OfflineModeNoticeDialog({ onBackToOnline, onClose, t }: Props) {
    const titleId = useId();
    const descId = useId();
    const { backdropProps, dialogProps } = useSafeBackdropDismiss(onClose);

    useEffect(() => {
        const onKeyDown = (event: KeyboardEvent) => {
            if (event.key === "Escape") onClose();
        };
        window.addEventListener("keydown", onKeyDown);
        return () => window.removeEventListener("keydown", onKeyDown);
    }, [onClose]);

    return (
        <div
            style={{ position: "fixed", top: 0, left: 0, right: 0, bottom: 0, background: "rgba(0,0,0,0.35)", display: "flex", alignItems: "center", justifyContent: "center", zIndex: 10000, WebkitAppRegion: "no-drag", "--wails-draggable": "no-drag" } as CSSProperties}
            {...backdropProps}
        >
            <div
                role="dialog"
                aria-modal="true"
                aria-labelledby={titleId}
                aria-describedby={descId}
                style={{ background: colors.surface, borderRadius: 16, padding: "22px 26px", maxWidth: 420, width: "90%", boxShadow: "0 16px 40px rgba(0,0,0,0.18)", WebkitAppRegion: "no-drag", "--wails-draggable": "no-drag" } as CSSProperties}
                {...dialogProps}
            >
                <div id={titleId} style={{ fontSize: 16, fontWeight: 700, marginBottom: 10, color: colors.text }}>{t("离网模式提示", "Offline Mode Notice", "離網模式提示")}</div>
                <div id={descId} style={{ fontSize: 14, color: colors.textSecondary, lineHeight: 1.65, marginBottom: 16 }}>{t("离网模式仅建议在涉密网络中使用。启用后将跳过 Hub 注册，远程控制、微信绑定、联网搜索等联网功能会受限。", "Offline mode is intended only for classified or restricted networks. It skips Hub registration, and online features such as remote control, WeChat binding, and web search will be limited.", "離網模式僅建議在涉密網路中使用。啟用後將跳過 Hub 註冊，遠端控制、微信綁定、聯網搜尋等聯網功能會受限。")}</div>
                <div style={{ display: "flex", gap: 10, justifyContent: "flex-end" }}>
                    <button type="button" onClick={onBackToOnline} style={{ ...wizardGhostButtonStyle, padding: "8px 18px", fontSize: "0.8rem", color: colors.text }}>{t("切回联网模式", "Back to online mode", "切回聯網模式")}</button>
                    <button type="button" autoFocus onClick={onClose} style={{ ...wizardPrimaryButtonStyle, width: "auto", padding: "8px 18px", fontSize: "0.8rem" }}>{t("我知道了", "I understand", "我知道了")}</button>
                </div>
            </div>
        </div>
    );
}
