import { useState } from "react";
import { main } from "../../../wailsjs/go/models";
import { ListRemoteHubs } from "../../../wailsjs/go/main/App";
import type { RemoteActivationStatus } from "./types";

interface HubOption {
    hub_id: string;
    name: string;
    base_url: string;
}

type Props = {
    config: main.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    translate: (key: string) => string;
    remoteBusy: string;
    remoteActivationStatus: RemoteActivationStatus | null;
    activateRemoteWithEmail: () => void;
    invitationCodeRequired: boolean;
    invitationCode: string;
    setInvitationCode: (code: string) => void;
    invitationCodeError: string;
};

export function RemoteSettingsPanel({
    config,
    saveRemoteConfigField,
    translate,
    remoteBusy,
    remoteActivationStatus,
    activateRemoteWithEmail,
    invitationCodeRequired,
    invitationCode,
    setInvitationCode,
    invitationCodeError,
}: Props) {
    const [hubList, setHubList] = useState<HubOption[]>([]);
    const [loadingHubs, setLoadingHubs] = useState(false);
    const [hubProbeError, setHubProbeError] = useState("");

    const probeHubs = async () => {
        const centerURL = (config?.remote_hubcenter_url || "").trim();
        const email = (config?.remote_email || "").trim();
        if (!email) {
            setHubProbeError("请先填写绑定邮件");
            return;
        }
        setLoadingHubs(true);
        setHubProbeError("");
        setHubList([]);
        try {
            const hubs = await ListRemoteHubs(centerURL, email) as HubOption[];
            setHubList(Array.isArray(hubs) ? hubs : []);
            if (!hubs || hubs.length === 0) {
                setHubProbeError("未发现可用的 Hub");
            }
        } catch (err) {
            setHubProbeError(String(err));
            setHubList([]);
        } finally {
            setLoadingHubs(false);
        }
    };

    const handleHubSelect = (value: string) => {
        if (value) {
            saveRemoteConfigField({ remote_hub_url: value });
        }
    };

    return (
        <div className="settings-panel">
            <div className="settings-panel-header">
                <div>
                    <h3 className="settings-panel-title">远程接入设置</h3>
                    <p className="settings-panel-desc">
                        配置 Hub Center、Hub 地址、邮箱和心跳参数，用于接入和维护远程实例。
                    </p>
                </div>
            </div>

            {/* Row 1: Hub Center + 心跳间隔 */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr 160px", gap: "10px" }}>
                <div className="form-group" style={{ marginBottom: 0 }}>
                    <label className="form-label">{translate("remoteHubCenterUrl")}</label>
                    <input
                        className="form-input"
                        value={config?.remote_hubcenter_url || ""}
                        onChange={(e) => saveRemoteConfigField({ remote_hubcenter_url: e.target.value })}
                        onBlur={(e) => saveRemoteConfigField({ remote_hubcenter_url: e.target.value.trim() })}
                        placeholder="http://hubs.mypapers.top:9388"
                        spellCheck={false}
                    />
                </div>
                <div className="form-group" style={{ marginBottom: 0 }}>
                    <label className="form-label">心跳间隔（秒）</label>
                    <input
                        className="form-input"
                        type="number"
                        min={5}
                        step={1}
                        value={config?.remote_heartbeat_sec || 10}
                        onChange={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Number(e.target.value || 10) })}
                        onBlur={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Math.max(5, Number(e.target.value || 10)) })}
                    />
                </div>
            </div>

            {/* Row 2: Hub 探测 + 选择/手工输入 */}
            <div style={{ display: "grid", gridTemplateColumns: "1fr auto", gap: "10px", alignItems: "end", marginTop: "10px" }}>
                <div className="form-group" style={{ marginBottom: 0 }}>
                    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: "6px" }}>
                        <label className="form-label" style={{ marginBottom: 0 }}>Hub 地址</label>
                        <button
                            className="btn-secondary"
                            onClick={probeHubs}
                            disabled={loadingHubs}
                            style={{ minWidth: "90px", height: "26px", padding: "2px 10px", fontSize: "0.78rem", flexShrink: 0 }}
                        >
                            {loadingHubs ? "探测中..." : "探测可用 Hub"}
                        </button>
                    </div>
                    {hubList.length > 0 && (
                        <select
                            className="form-select"
                            value={hubList.some((h) => h.base_url === (config?.remote_hub_url || "").trim()) ? (config?.remote_hub_url || "").trim() : ""}
                            onChange={(e) => handleHubSelect(e.target.value)}
                            style={{ marginBottom: "6px" }}
                        >
                            <option value="">-- 从列表中选择 --</option>
                            {hubList.map((hub) => (
                                <option key={`${hub.hub_id}-${hub.base_url}`} value={hub.base_url}>
                                    {hub.name ? `${hub.name} (${hub.base_url})` : hub.base_url}
                                </option>
                            ))}
                        </select>
                    )}
                    <input
                        className="form-input"
                        value={config?.remote_hub_url || ""}
                        onChange={(e) => saveRemoteConfigField({ remote_hub_url: e.target.value })}
                        onBlur={(e) => saveRemoteConfigField({ remote_hub_url: e.target.value.trim() })}
                        placeholder="https://hub.example.com（可从上方列表选择或手工输入）"
                        spellCheck={false}
                    />
                    {hubProbeError && (
                        <div style={{ fontSize: "0.78rem", color: "#ef4444", marginTop: "4px" }}>{hubProbeError}</div>
                    )}
                </div>
            </div>

            {/* Row 3: 邮件 + 邀请码（同一行） */}
            <div style={{ display: "grid", gridTemplateColumns: invitationCodeRequired ? "1fr 1fr" : "1fr", gap: "10px", marginTop: "10px" }}>
                <div className="form-group" style={{ marginBottom: 0 }}>
                    <label className="form-label">{translate("remoteBindEmail")}</label>
                    <input
                        className="form-input"
                        value={config?.remote_email || ""}
                        onChange={(e) => saveRemoteConfigField({ remote_email: e.target.value })}
                        onBlur={(e) => saveRemoteConfigField({ remote_email: e.target.value.trim() })}
                        placeholder="name@example.com"
                        spellCheck={false}
                    />
                </div>
                {invitationCodeRequired && (
                    <div className="form-group" style={{ marginBottom: 0 }}>
                        <label className="form-label">邀请码</label>
                        <input
                            className="form-input"
                            value={invitationCode}
                            onChange={(e) => setInvitationCode(e.target.value.toUpperCase())}
                            placeholder="请输入邀请码"
                            spellCheck={false}
                            maxLength={10}
                            style={invitationCodeError ? { borderColor: "#ef4444" } : undefined}
                        />
                        {invitationCodeError && (
                            <div style={{ fontSize: "0.78rem", color: "#ef4444", marginTop: "4px" }}>{invitationCodeError}</div>
                        )}
                    </div>
                )}
            </div>

            {/* Row 4: 注册按钮独立一行 */}
            <div style={{ marginTop: "10px" }}>
                <button className="btn-primary remote-activation-button" style={{ width: "100%" }} disabled={!!remoteBusy} onClick={activateRemoteWithEmail}>
                    {remoteBusy === "activate" ? "注册中..." : "注册"}
                </button>
            </div>

            <div className="info-text" style={{ marginTop: "10px", textAlign: "left" }}>
                {remoteActivationStatus?.activated
                    ? `${translate("remoteActivation")}: ${translate("remoteActivated")} ${remoteActivationStatus.email ? `(${remoteActivationStatus.email})` : ""}`
                    : `${translate("remoteActivation")}: ${translate("remoteNotActivated")}`}{" | "}{translate("remoteModeDesc")}
            </div>
        </div>
    );
}
