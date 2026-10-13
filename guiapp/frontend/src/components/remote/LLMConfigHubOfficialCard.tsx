import { hubOfficialStatus } from "./LLMConfigPanelShared";
import { capabilityModelMenuLabel } from "../../utils/capabilityModelLabel";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

type HubOfficial = ReturnType<typeof hubOfficialStatus>;

/** Details card shown when the hub-managed MaClaw Official service is selected. */
export function LLMConfigHubOfficialCard({
    t,
    hubOfficial,
    hubModelOptions,
    selectedHubModel,
    onSelectModel,
}: {
    t: Translate;
    hubOfficial: HubOfficial;
    hubModelOptions: string[];
    selectedHubModel: string;
    onSelectModel: (modelId: string) => void;
}) {
    return (
        <div className="llm-config-hub-card">
            <div style={{ display: "grid", gap: 14 }}>
                <div style={{ display: "flex", justifyContent: "space-between", alignItems: "flex-start", gap: 12 }}>
                    <div>
                        <div style={{ fontSize: "0.92rem", fontWeight: 800, color: "var(--theme-text-primary)", marginBottom: 6 }}>
                            {t("MaClaw Official", "MaClaw 官方")}
                        </div>
                        <div style={{ fontSize: "0.76rem", color: "var(--theme-text-secondary)", lineHeight: 1.7, maxWidth: 620 }}>
                            {t("Your account is authorized for MaClaw official LLM service. Requests are routed through the managed Hub service without exposing external API details.", "当前账号已开通 MaClaw 官方模型服务。请求将通过 Hub 托管服务安全转发，无需展示对外接口信息。")}
                        </div>
                    </div>
                    <span className="llm-config-hub-card__status">
                        {hubOfficial.label}
                    </span>
                </div>

                <div style={{ display: "grid", gridTemplateColumns: "minmax(120px, 0.7fr) minmax(220px, 1.6fr) minmax(120px, 0.7fr)", gap: 10 }}>
                    <div className="llm-config-hub-card__cell">
                        <div style={{ fontSize: "0.68rem", color: "var(--theme-text-muted)", marginBottom: 5, fontWeight: 700 }}>{t("Service Status", "服务状态")}</div>
                        <div style={{ fontSize: "0.82rem", color: "var(--theme-text-primary)", fontWeight: 800 }}>{hubOfficial.label}</div>
                    </div>
                    <div className="llm-config-hub-card__cell">
                        <div id="hub-official-model-label" style={{ fontSize: "0.68rem", color: "var(--theme-text-muted)", marginBottom: 6, fontWeight: 700 }}>{t("Model", "模型")}</div>
                        <div role="radiogroup" aria-labelledby="hub-official-model-label" style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                            {hubModelOptions.map(modelId => {
                                const active = modelId.toLowerCase() === selectedHubModel.toLowerCase();
                                return (
                                    <button
                                        key={modelId}
                                        type="button"
                                        role="radio"
                                        aria-checked={active}
                                        className={"llm-config-option-btn" + (active ? " is-active" : "")}
                                        onClick={() => onSelectModel(modelId)}
                                    >
                                        {capabilityModelMenuLabel(modelId)}
                                    </button>
                                );
                            })}
                        </div>
                    </div>
                    <div className="llm-config-hub-card__cell">
                        <div style={{ fontSize: "0.68rem", color: "var(--theme-text-muted)", marginBottom: 5, fontWeight: 700 }}>{t("Configuration", "配置方式")}</div>
                        <div style={{ fontSize: "0.82rem", color: "var(--theme-text-primary)", fontWeight: 800 }}>{t("No setup needed", "无需配置")}</div>
                    </div>
                </div>

                <div style={{ fontSize: "0.72rem", color: "var(--theme-text-muted)", lineHeight: 1.6 }}>
                    {hubOfficial.detail || t("Use this managed service directly, or select another provider above if you need to override it.", "可直接使用此托管服务；如需改用其他服务商，可在上方切换。")}
                </div>
            </div>
        </div>
    );
}
