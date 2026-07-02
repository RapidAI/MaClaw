import { useState, useCallback, useMemo, useEffect, useRef } from "react";
import { QRCodeSVG } from "qrcode.react";
import { colors } from "./styles";
import type { LLMProvider } from "./LLMConfigPanelShared";
import { NONE_PROVIDER, HUB_SERVICE_PROVIDER_NAME } from "./LLMConfigPanelShared";
import { CreateMobileLLMDesktopQRSession, FetchProviderModels } from "../../../wailsjs/go/main/App";

interface MobileQRCodeDialogProps {
    open: boolean;
    onClose: () => void;
    providers: LLMProvider[];
    currentName: string;
    lang?: string;
}

const MODELS_MAX_COUNT = 30;

export function MobileQRCodeDialog({ open, onClose, providers, currentName, lang }: MobileQRCodeDialogProps) {
    const t = useCallback((en: string, zhHans: string, zhHant: string = zhHans) =>
        lang === "zh-Hans" ? zhHans : lang === "zh-Hant" ? zhHant : en, [lang]);

    // Only show providers that have url and key configured (excluding "None" and Hub official)
    const availableProviders = useMemo(() =>
        providers.filter(p =>
            p.name !== NONE_PROVIDER &&
            p.name !== HUB_SERVICE_PROVIDER_NAME &&
            p.url && p.key
        ), [providers]);

    const [selectedIdx, setSelectedIdx] = useState(0);
    const [modelList, setModelList] = useState<string[]>([]);
    const [fetchingModels, setFetchingModels] = useState(false);
    const [modelsFetched, setModelsFetched] = useState(false);
    const [qrValue, setQrValue] = useState("");
    const [qrError, setQrError] = useState("");
    const [qrExpiresAt, setQrExpiresAt] = useState("");
    const [creatingQr, setCreatingQr] = useState(false);

    // Reset state only on open transition (false → true).
    // We track previous `open` to detect the rising edge, avoiding
    // spurious resets when providers array reference changes mid-session.
    const prevOpenRef = useRef(false);
    useEffect(() => {
        if (open && !prevOpenRef.current) {
            // Dialog just opened — sync selection to current provider
            const idx = availableProviders.findIndex(p => p.name === currentName);
            setSelectedIdx(idx >= 0 ? idx : 0);
            setModelList([]);
            setModelsFetched(false);
            setQrValue("");
            setQrError("");
            setQrExpiresAt("");
        }
        prevOpenRef.current = open;
    }, [open, availableProviders, currentName]);

    // Guard against selectedIdx going out of bounds
    const safeIdx = availableProviders.length > 0
        ? Math.min(selectedIdx, availableProviders.length - 1)
        : 0;
    const selectedProvider = availableProviders[safeIdx] || null;

    // Track which provider the current fetch is for, to discard stale results
    const fetchProviderRef = useRef<string | null>(null);

    const handleProviderChange = useCallback((idx: number) => {
        setSelectedIdx(idx);
        setModelList([]);
        setModelsFetched(false);
        setFetchingModels(false); // Cancel visual state of any in-flight fetch for previous provider
        fetchProviderRef.current = null; // Invalidate any pending async result
        setQrValue("");
        setQrError("");
        setQrExpiresAt("");
    }, []);

    const handleFetchModels = useCallback(async () => {
        if (!selectedProvider) return;
        const providerName = selectedProvider.name;
        fetchProviderRef.current = providerName;
        setFetchingModels(true);
        try {
            const models = await FetchProviderModels(
                selectedProvider.url,
                selectedProvider.key,
                selectedProvider.protocol || "openai",
                ""
            );
            // Discard result if user switched provider during fetch
            if (fetchProviderRef.current !== providerName) return;
            if (Array.isArray(models)) {
                setModelList(models.map((m: any) => m.id || m.name || String(m)).filter(Boolean));
            }
            setModelsFetched(true);
        } catch (e) {
            if (fetchProviderRef.current !== providerName) return;
            console.warn("[MobileQRCode] fetch models failed:", e);
            setModelList([]);
            setModelsFetched(true);
        } finally {
            // Only clear loading if this fetch is still the active one
            if (fetchProviderRef.current === providerName) {
                setFetchingModels(false);
            }
        }
    }, [selectedProvider]);

    const { qrModels, modelsTruncated } = useMemo(() => {
        if (!selectedProvider) return { qrModels: [] as string[], modelsTruncated: false };
        const rawModels = modelList.length > 0
            ? modelList
            : (selectedProvider.model ? [selectedProvider.model] : []);
        return {
            qrModels: rawModels.slice(0, MODELS_MAX_COUNT),
            modelsTruncated: rawModels.length > MODELS_MAX_COUNT,
        };
    }, [selectedProvider, modelList]);

    useEffect(() => {
        if (!open || !selectedProvider) return;
        let cancelled = false;
        setCreatingQr(true);
        setQrValue("");
        setQrError("");
        setQrExpiresAt("");
        CreateMobileLLMDesktopQRSession(
            selectedProvider.name,
            selectedProvider.url,
            selectedProvider.key,
            selectedProvider.model,
            qrModels,
            selectedProvider.protocol || "openai",
        ).then((session) => {
            if (cancelled) return;
            setQrValue(session?.qr_payload || "");
            setQrExpiresAt(session?.expires_at || "");
            if (!session?.qr_payload) {
                setQrError(t("Hub did not return a QR payload.", "Hub 未返回二维码载荷。"));
            }
        }).catch((error) => {
            if (cancelled) return;
            const message = error instanceof Error ? error.message : String(error || "");
            setQrError(message || t("Failed to create mobile QR session.", "创建移动端二维码会话失败。"));
        }).finally(() => {
            if (!cancelled) setCreatingQr(false);
        });
        return () => {
            cancelled = true;
        }
    }, [open, selectedProvider, qrModels, t]);

    // Auto-focus overlay for keyboard events (Escape to close).
    // useEffect runs after render, so overlayRef.current is available.
    const overlayRef = useRef<HTMLDivElement>(null);
    useEffect(() => {
        if (open) {
            // Defer to next frame to ensure DOM is painted after conditional render
            requestAnimationFrame(() => overlayRef.current?.focus());
        }
    }, [open]);

    if (!open) return null;

    return (
        <div ref={overlayRef} tabIndex={-1} role="dialog" aria-modal="true"
             aria-label={t("Mobile QR Code", "移动端二维码")}
             className="llm-qr-dialog-overlay" style={{
            position: "fixed", top: 0, left: 0, right: 0, bottom: 0,
            background: "rgba(0,0,0,0.4)", display: "flex",
            alignItems: "center", justifyContent: "center", zIndex: 9999,
            outline: "none",
        }} onClick={(e) => { if (e.target === e.currentTarget) onClose(); }}
           onKeyDown={(e) => { if (e.key === "Escape") onClose(); }}
        >
            <div className="llm-qr-dialog" style={{
                background: colors.surface, borderRadius: 12, padding: "24px 28px",
                maxWidth: 440, width: "92%", maxHeight: "85vh", overflowY: "auto",
                boxShadow: "0 16px 48px rgba(0,0,0,0.22)",
            }}>
                {/* Header */}
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 18 }}>
                    <span style={{ fontSize: "0.92rem", fontWeight: 700, color: colors.text }}>
                        {t("Mobile QR Code", "移动端二维码")}
                    </span>
                    <button onClick={onClose} aria-label={t("Close", "关闭")} style={{
                        border: "none", background: "transparent", cursor: "pointer",
                        fontSize: "0.68rem", fontWeight: 700, color: colors.textSecondary, padding: "2px 6px",
                    }}>CLOSE</button>
                </div>

                {/* No providers warning */}
                {availableProviders.length === 0 && (
                    <div style={{
                        padding: "12px 16px", borderRadius: 6, fontSize: "0.78rem", lineHeight: 1.5,
                        background: colors.dangerBg, border: `1px solid ${colors.danger}`, color: colors.danger,
                    }}>
                        {t(
                            "No configured providers available. Please configure and test a provider first.",
                            "暂无已配置的服务商。请先配置并测试通过一个服务商。"
                        )}
                    </div>
                )}

                {/* Provider selector */}
                {availableProviders.length > 0 && (
                    <>
                        <div style={{ marginBottom: 14 }}>
                            <label style={{ fontSize: "0.76rem", color: colors.textSecondary, marginBottom: 4, display: "block" }}>
                                {t("Select Provider", "选择服务商")}
                            </label>
                            <div style={{ display: "flex", gap: 4, flexWrap: "wrap" }}>
                                {availableProviders.map((p, i) => (
                                    <button key={p.name} onClick={() => handleProviderChange(i)} style={{
                                        fontSize: "0.76rem", padding: "5px 14px", cursor: "pointer",
                                        background: safeIdx === i ? colors.primaryLight : colors.surface,
                                        color: safeIdx === i ? colors.primaryDark : colors.text,
                                        border: `1px solid ${safeIdx === i ? colors.primary : colors.border}`,
                                        borderRadius: 4, transition: "all 0.15s",
                                    }}>
                                        {p.name}
                                    </button>
                                ))}
                            </div>
                        </div>

                        {/* Fetch models button */}
                        {selectedProvider && (
                            <div style={{ marginBottom: 14, display: "flex", alignItems: "center", gap: 10 }}>
                                <button onClick={handleFetchModels} disabled={fetchingModels} style={{
                                    fontSize: "0.72rem", padding: "4px 12px", cursor: fetchingModels ? "wait" : "pointer",
                                    background: colors.surface, color: colors.primaryDark,
                                    border: `1px solid ${colors.primary}`, borderRadius: 4,
                                    opacity: fetchingModels ? 0.6 : 1,
                                }}>
                                    {fetchingModels
                                        ? t("Fetching...", "获取中...")
                                        : t("Fetch Model List", "获取模型列表")}
                                </button>
                                {modelsFetched && (
                                    <span style={{ fontSize: "0.7rem", color: colors.textSecondary }}>
                                        {modelList.length > 0
                                            ? t(`${modelList.length} models`, `${modelList.length} 个模型`)
                                            : t("No models found, using current model", "未获取到模型列表，使用当前模型")}
                                    </span>
                                )}
                            </div>
                        )}

                        {creatingQr && (
                            <div style={{
                                padding: "10px 14px", borderRadius: 6, fontSize: "0.74rem",
                                background: colors.bg, border: `1px solid ${colors.border}`,
                                color: colors.textSecondary, marginBottom: 14,
                            }}>
                                {t("Creating one-time mobile QR session...", "正在创建一次性移动端二维码会话...")}
                            </div>
                        )}

                        {qrError && (
                            <div style={{
                                padding: "10px 14px", borderRadius: 6, fontSize: "0.74rem", lineHeight: 1.5,
                                background: colors.dangerBg, border: `1px solid ${colors.danger}`,
                                color: colors.danger, marginBottom: 14,
                            }}>
                                {qrError}
                            </div>
                        )}

                        {/* QR Code display */}
                        {qrValue && (
                            <div style={{ textAlign: "center", marginBottom: 16 }}>
                                <div style={{
                                    display: "inline-block", padding: 16,
                                    background: "#ffffff", borderRadius: 8,
                                    border: `1px solid ${colors.border}`,
                                }}>
                                    <QRCodeSVG
                                        value={qrValue}
                                        size={220}
                                        level="M"
                                        bgColor="#ffffff"
                                        fgColor="#000000"
                                    />
                                </div>
                                <p style={{ marginTop: 10, fontSize: "0.74rem", color: colors.textSecondary, lineHeight: 1.5 }}>
                                    {t(
                                        "Scan with mobile app to import LLM configuration",
                                        "使用移动端 App 扫描二维码导入大模型配置"
                                    )}
                                </p>
                                {qrExpiresAt && (
                                    <p style={{ marginTop: 4, fontSize: "0.68rem", color: colors.textMuted }}>
                                        {t("Expires at", "有效期至")}: {qrExpiresAt}
                                    </p>
                                )}
                                {modelsTruncated && (
                                    <p style={{ marginTop: 4, fontSize: "0.68rem", color: colors.textMuted }}>
                                        {t(
                                            "Model list truncated to fit QR code capacity",
                                            "模型列表已截断以适配二维码容量"
                                        )}
                                    </p>
                                )}
                            </div>
                        )}

                        {/* Security notice */}
                        {selectedProvider && (
                            <div style={{
                                padding: "8px 12px", borderRadius: 4, fontSize: "0.68rem", lineHeight: 1.5,
                                background: `color-mix(in srgb, ${colors.warning} 8%, transparent)`,
                                border: `1px solid color-mix(in srgb, ${colors.warning} 30%, transparent)`,
                                color: colors.textSecondary, marginBottom: 12,
                            }}>
                                {t(
                                    "The QR code is a one-time Hub authorization session and does not contain your API Key.",
                                    "二维码是一次性 Hub 授权会话，不包含 API Key 明文。"
                                )}
                            </div>
                        )}

                        {/* Provider info summary */}
                        {selectedProvider && (
                            <div style={{
                                padding: "10px 14px", borderRadius: 6, fontSize: "0.72rem",
                                background: colors.bg, border: `1px solid ${colors.border}`,
                                lineHeight: 1.7, color: colors.textSecondary,
                            }}>
                                <div><strong>{t("Provider", "服务商")}:</strong> {selectedProvider.name}</div>
                                <div style={{ wordBreak: "break-all" }}><strong>URL:</strong> {selectedProvider.url}</div>
                                <div><strong>API Key:</strong> {selectedProvider.key.length > 12 ? `${selectedProvider.key.slice(0, 8)}...${selectedProvider.key.slice(-4)}` : "***"}</div>
                                <div><strong>{t("Current Model", "当前模型")}:</strong> {selectedProvider.model || "-"}</div>
                                {selectedProvider.protocol && selectedProvider.protocol !== "openai" && (
                                    <div><strong>{t("Protocol", "协议")}:</strong> {selectedProvider.protocol}</div>
                                )}
                                {modelList.length > 0 && (
                                    <div style={{ wordBreak: "break-all" }}>
                                        <strong>{t("Models", "模型列表")}:</strong> {modelList.slice(0, 10).join(", ")}{modelList.length > 10 ? ` (+${modelList.length - 10})` : ""}
                                    </div>
                                )}
                            </div>
                        )}
                    </>
                )}
            </div>
        </div>
    );
}
