import { main } from "../../../wailsjs/go/models";
import type { RemoteActivationStatus } from "./types";

type Props = {
    config: main.AppConfig | null;
    saveRemoteConfigField: (patch: Partial<main.AppConfig>) => void;
    translate: (key: string) => string;
    remoteBusy: string;
    remoteActivationStatus: RemoteActivationStatus | null;
    activateRemoteWithEmail: () => void;
};

export function RemoteSettingsPanel({
    config,
    saveRemoteConfigField,
    translate,
    remoteBusy,
    remoteActivationStatus,
    activateRemoteWithEmail,
}: Props) {
    return (
        <div className="settings-panel">
            <div className="settings-panel-header">
                <div>
                    <h3 className="settings-panel-title">远程接入设置</h3>
                    <p className="settings-panel-desc">
                        配置 Hub 地址、邮箱和心跳参数，用于接入和维护远程实例。
                    </p>
                </div>
            </div>

            <div style={{ display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))", gap: "10px" }}>
                <div className="form-group" style={{ marginBottom: 0 }}>
                    <label className="form-label">{translate("remoteHubUrl")}</label>
                    <input
                        className="form-input"
                        value={config?.remote_hub_url || ""}
                        onChange={(e) => saveRemoteConfigField({ remote_hub_url: e.target.value })}
                        onBlur={(e) => saveRemoteConfigField({ remote_hub_url: e.target.value.trim() })}
                        placeholder="https://hub.example.com"
                        spellCheck={false}
                    />
                </div>

                <div className="form-group" style={{ marginBottom: 0 }}>
                    <label className="form-label">{translate("remoteHubCenterUrl")}</label>
                    <input
                        className="form-input"
                        value={config?.remote_hubcenter_url || ""}
                        onChange={(e) => saveRemoteConfigField({ remote_hubcenter_url: e.target.value })}
                        onBlur={(e) => saveRemoteConfigField({ remote_hubcenter_url: e.target.value.trim() })}
                        placeholder="http://hubs.rapidai.tech"
                        spellCheck={false}
                    />
                </div>

                <div className="form-group" style={{ marginBottom: 0 }}>
                    <label className="form-label">心跳间隔（秒）</label>
                    <input
                        className="form-input"
                        type="number"
                        min={30}
                        step={1}
                        value={config?.remote_heartbeat_sec || 60}
                        onChange={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Number(e.target.value || 60) })}
                        onBlur={(e) => saveRemoteConfigField({ remote_heartbeat_sec: Math.max(30, Number(e.target.value || 60)) })}
                    />
                </div>
            </div>

            <div className="remote-activation-row">
                <div className="form-group remote-activation-field" style={{ marginBottom: 0 }}>
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
                <button className="btn-primary remote-activation-button" disabled={!!remoteBusy} onClick={activateRemoteWithEmail}>
                    {remoteBusy === "activate" ? "激活中..." : "激活"}
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
