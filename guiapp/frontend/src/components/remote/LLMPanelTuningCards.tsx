import type { ReactNode } from 'react';
import { colors } from "./styles";
import { inputStyle, labelStyle } from "./LLMConfigPanelShared";

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

/** Shared card chrome for the inline LLM tuning settings. */
function tuningCard(children: ReactNode): ReactNode {
    return (
        <div className="llm-config-card" style={{
            marginBottom: 16, padding: "12px 16px", borderRadius: 6,
            border: `1px solid ${colors.border}`, background: colors.surface,
        }}>
            {children}
        </div>
    );
}

/** Card header: title + muted hint. */
function tuningHeader(title: ReactNode, hint: ReactNode): ReactNode {
    return (
        <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 8 }}>
            <label style={{ ...labelStyle, marginBottom: 0 }}>
                {title}
                <span style={{ fontSize: "0.68rem", color: colors.textMuted, fontWeight: 400, marginLeft: 6 }}>
                    {hint}
                </span>
            </label>
        </div>
    );
}

/** Agent max iterations slider + numeric input. */
export function LLMMaxIterationsCard({ maxIter, setMaxIter, t }: {
    maxIter: number;
    setMaxIter: (v: number) => void;
    t: Translate;
}) {
    const apply = (v: number) => { setMaxIter(v); };
    return tuningCard(<>
        {tuningHeader(
            t("Agent Max Iterations", "Agent 最大推理轮数"),
            t("0=unlimited, default 300", "0=不限制，默认 300"),
        )}
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <input type="range" min={0} max={300} step={1} value={maxIter}
                onChange={e => { const v = Number(e.target.value); apply(v); }}
                style={{ flex: 1, accentColor: "var(--theme-primary)" }} />
            <input type="number" min={0} max={300} value={maxIter}
                onChange={e => { const v = Math.max(0, Math.min(300, Number(e.target.value) || 0)); apply(v); }}
                style={{ ...inputStyle, width: 60, textAlign: "center" as const }} />
            <span style={{ fontSize: "0.72rem", color: colors.textSecondary, whiteSpace: "nowrap" }}>
                {maxIter === 0 ? t("Unlimited", "不限制") : `${maxIter} ${t("rounds", "轮")}`}
            </span>
        </div>
    </>);
}

/** CodingSubAgent parallelism slider + numeric input. */
export function LLMSubAgentConcurrencyCard({ subAgentConc, setSubAgentConc, t }: {
    subAgentConc: number;
    setSubAgentConc: (v: number) => void;
    t: Translate;
}) {
    const apply = (v: number) => { setSubAgentConc(v); };
    return tuningCard(<>
        {tuningHeader(
            t("CodingSubAgent Concurrency", "CodingSubAgent 并发数"),
            t("Parallel tasks without dependencies, default 2", "无依赖任务并行数，默认 2"),
        )}
        <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
            <input type="range" min={1} max={10} step={1} value={subAgentConc}
                onChange={e => { const v = Number(e.target.value); apply(v); }}
                style={{ flex: 1, accentColor: "var(--theme-primary)" }} />
            <input type="number" min={1} max={10} value={subAgentConc}
                onChange={e => { const v = Math.max(1, Math.min(10, Number(e.target.value) || 1)); apply(v); }}
                style={{ ...inputStyle, width: 60, textAlign: "center" as const }} />
            <span style={{ fontSize: "0.72rem", color: colors.textSecondary, whiteSpace: "nowrap" }}>
                {subAgentConc === 1 ? t("Sequential", "顺序执行") : `${subAgentConc} ${t("parallel", "路并行")}`}
            </span>
        </div>
    </>);
}

/** Global thinking (reasoning) mode toggle. */
export function LLMThinkingModeCard({ thinkingMode, thinkingModeSaving, thinkingModeError, saveThinkingMode, t }: {
    thinkingMode: "enabled" | "disabled";
    thinkingModeSaving: boolean;
    thinkingModeError: string | null;
    saveThinkingMode: (mode: "enabled" | "disabled") => void;
    t: Translate;
}) {
    return tuningCard(<>
        {tuningHeader(
            t("Thinking (Reasoning)", "推理（思考过程）"),
            t("Global; translated to each provider's supported control", "全局设置；会按服务商支持的参数转换"),
        )}
        <div style={{ display: "flex", gap: 6, flexWrap: "wrap" }} role="group" aria-label={t("Thinking mode", "推理模式")} aria-busy={thinkingModeSaving}>
            {(["enabled", "disabled"] as const).map(mode => {
                const active = thinkingMode === mode;
                return (
                    <button key={mode}
                        data-testid={`thinking-mode-${mode}`}
                        type="button"
                        aria-pressed={active}
                        disabled={thinkingModeSaving}
                        onClick={() => { saveThinkingMode(mode); }}
                        style={{
                            fontSize: "0.76rem", padding: "5px 16px", cursor: thinkingModeSaving ? "wait" : "pointer",
                            background: active ? colors.primaryLight : colors.surface,
                            color: active ? colors.primaryDark : colors.textSecondary,
                            border: `1px solid ${active ? colors.primary : colors.border}`,
                            borderRadius: 4, transition: "all 0.15s", opacity: thinkingModeSaving ? 0.7 : 1,
                        }}>
                        {mode === "enabled" ? t("On", "开启") : t("Off", "关闭")}
                    </button>
                );
            })}
        </div>
        <p style={{ fontSize: "0.68rem", color: colors.textMuted, margin: "6px 0 0 0", lineHeight: 1.4 }}>
            {thinkingMode === "enabled"
                ? t("Enabled on new requests using the provider's native control. The chat panel shows reasoning only when the provider returns it.", "已在后续请求中按服务商原生参数开启；仅当服务商返回推理内容时，助手面板才会显示“思考过程”。")
                : t("Disabled on new requests using the provider's native control. Models without a hard off switch use their lowest reasoning level.", "已在后续请求中按服务商原生参数关闭；没有硬关闭能力的模型会使用最低推理强度。")}
        </p>
        {thinkingModeError && <p role="alert" style={{ fontSize: "0.7rem", color: colors.danger, margin: "6px 0 0", lineHeight: 1.4 }}>{thinkingModeError}</p>}
    </>);
}
