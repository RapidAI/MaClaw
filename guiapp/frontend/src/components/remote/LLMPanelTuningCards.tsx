import type { ReactNode } from 'react';

type Translate = (en: string, zhHans: string, zhHant?: string) => string;

/** Shared card chrome for the inline LLM tuning settings.
    Each export renders one `.llm-tuning-row` section; LLMConfigPanel stacks
    all three inside a single `.llm-config-card.llm-tuning-stack` card so the
    runtime knobs read as one settings group instead of three loose cards. */
function tuningRow(title: ReactNode, hint: ReactNode, children: ReactNode): ReactNode {
    return (
        <div className="llm-tuning-row">
            <div className="llm-tuning-row__head">
                <span className="llm-tuning-row__title">{title}</span>
                <span className="llm-tuning-row__hint">{hint}</span>
            </div>
            {children}
        </div>
    );
}

/** Agent max iterations slider + numeric input. */
export function LLMMaxIterationsCard({ maxIter, setMaxIter, t }: {
    maxIter: number;
    setMaxIter: (v: number) => void;
    t: Translate;
}) {
    return tuningRow(
        t("Agent Max Iterations", "Agent 最大推理轮数"),
        t("0=unlimited, default 300", "0=不限制，默认 300"),
        <div className="llm-tuning-row__controls">
            <input type="range" min={0} max={300} step={1} value={maxIter}
                className="llm-tuning-slider"
                aria-label={t("Agent Max Iterations", "Agent 最大推理轮数")}
                onChange={e => { setMaxIter(Number(e.target.value)); }} />
            <input type="number" min={0} max={300} value={maxIter}
                className="llm-tuning-number"
                aria-label={t("Agent Max Iterations", "Agent 最大推理轮数")}
                onChange={e => { setMaxIter(Math.max(0, Math.min(300, Number(e.target.value) || 0))); }} />
            <span className="llm-tuning-meta">
                {maxIter === 0 ? t("Unlimited", "不限制") : `${maxIter} ${t("rounds", "轮")}`}
            </span>
        </div>
    );
}

/** CodingSubAgent parallelism slider + numeric input. */
export function LLMSubAgentConcurrencyCard({ subAgentConc, setSubAgentConc, t }: {
    subAgentConc: number;
    setSubAgentConc: (v: number) => void;
    t: Translate;
}) {
    return tuningRow(
        t("CodingSubAgent Concurrency", "CodingSubAgent 并发数"),
        t("Parallel tasks without dependencies, default 2", "无依赖任务并行数，默认 2"),
        <div className="llm-tuning-row__controls">
            <input type="range" min={1} max={10} step={1} value={subAgentConc}
                className="llm-tuning-slider"
                aria-label={t("CodingSubAgent Concurrency", "CodingSubAgent 并发数")}
                onChange={e => { setSubAgentConc(Number(e.target.value)); }} />
            <input type="number" min={1} max={10} value={subAgentConc}
                className="llm-tuning-number"
                aria-label={t("CodingSubAgent Concurrency", "CodingSubAgent 并发数")}
                onChange={e => { setSubAgentConc(Math.max(1, Math.min(10, Number(e.target.value) || 1))); }} />
            <span className="llm-tuning-meta">
                {subAgentConc === 1 ? t("Sequential", "顺序执行") : `${subAgentConc} ${t("parallel", "路并行")}`}
            </span>
        </div>
    );
}

/** Global thinking (reasoning) mode toggle. */
export function LLMThinkingModeCard({ thinkingMode, thinkingModeSaving, thinkingModeError, saveThinkingMode, t }: {
    thinkingMode: "enabled" | "disabled";
    thinkingModeSaving: boolean;
    thinkingModeError: string | null;
    saveThinkingMode: (mode: "enabled" | "disabled") => void;
    t: Translate;
}) {
    return tuningRow(
        t("Thinking (Reasoning)", "推理（思考过程）"),
        t("Global; translated to each provider's supported control", "全局设置；会按服务商支持的参数转换"),
        <>
            <div className="llm-config-option-group" role="group" aria-label={t("Thinking mode", "推理模式")} aria-busy={thinkingModeSaving}>
                {(["enabled", "disabled"] as const).map(mode => {
                    const active = thinkingMode === mode;
                    return (
                        <button key={mode}
                            data-testid={`thinking-mode-${mode}`}
                            type="button"
                            aria-pressed={active}
                            disabled={thinkingModeSaving}
                            className={"llm-config-option-btn" + (active ? " is-active" : "")}
                            onClick={() => { saveThinkingMode(mode); }}>
                            {mode === "enabled" ? t("On", "开启") : t("Off", "关闭")}
                        </button>
                    );
                })}
            </div>
            <p className="llm-tuning-row__note">
                {thinkingMode === "enabled"
                    ? t("Enabled on new requests using the provider's native control. The chat panel shows reasoning only when the provider returns it.", "已在后续请求中按服务商原生参数开启；仅当服务商返回推理内容时，助手面板才会显示“思考过程”。")
                    : t("Disabled on new requests using the provider's native control. Models without a hard off switch use their lowest reasoning level.", "已在后续请求中按服务商原生参数关闭；没有硬关闭能力的模型会使用最低推理强度。")}
            </p>
            {thinkingModeError && <p role="alert" className="llm-tuning-row__note llm-tuning-row__note--danger">{thinkingModeError}</p>}
        </>
    );
}
