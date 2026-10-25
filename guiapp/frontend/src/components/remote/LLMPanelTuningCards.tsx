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

export type ThinkingLevel = "auto" | "low" | "high" | "max" | "none";

const THINKING_LEVELS: { id: ThinkingLevel; en: string; zh: string }[] = [
    { id: "auto", en: "Auto", zh: "自动" },
    { id: "low", en: "Low", zh: "低" },
    { id: "high", en: "High", zh: "高" },
    { id: "max", en: "Max", zh: "最大" },
    { id: "none", en: "Off", zh: "关闭" },
];

function thinkingLevelNote(level: ThinkingLevel, t: Translate): string {
    switch (level) {
        case "low":
            return t("Light reasoning on providers that accept an effort. Others turn thinking on.", "支持档位的服务商使用轻度思考，其余服务商开启思考。");
        case "high":
            return t("Stronger reasoning on providers that accept an effort. Others turn thinking on.", "支持档位的服务商使用增强思考，其余服务商开启思考。");
        case "max":
            return t("Deepest reasoning the provider accepts. Others turn thinking on.", "使用该服务商允许的最深思考；没有档位的服务商只开启思考。");
        case "none":
            return t("Turns thinking off where the provider allows it. Models that must think use their lowest level.", "能关闭的服务商会关闭思考；不能关闭的模型改用它允许的最低档。");
        default:
            return t("Leaves each provider's default in place. If a model rejects turning thinking off, the request retries at the lowest level it allows.", "沿用各服务商的默认行为。如果模型拒绝关闭思考，会按它允许的最低档自动重试。");
    }
}

/** Global thinking level. Auto is the default and lets the system choose. */
export function LLMThinkingModeCard({ thinkingMode, thinkingModeSaving, thinkingModeError, saveThinkingMode, t }: {
    thinkingMode: ThinkingLevel;
    thinkingModeSaving: boolean;
    thinkingModeError: string | null;
    saveThinkingMode: (mode: ThinkingLevel) => void;
    t: Translate;
}) {
    return tuningRow(
        t("Thinking level", "思考档位"),
        t("Global. Auto is the default; other levels use each provider's native control.", "全局设置。默认自动，其它档位按服务商支持的参数转换。"),
        <>
            <div className="llm-config-option-group" role="group" aria-label={t("Thinking level", "思考档位")} aria-busy={thinkingModeSaving}>
                {THINKING_LEVELS.map(level => {
                    const active = thinkingMode === level.id;
                    return (
                        <button key={level.id}
                            data-testid={`thinking-mode-${level.id}`}
                            type="button"
                            aria-pressed={active}
                            disabled={thinkingModeSaving}
                            className={"llm-config-option-btn" + (active ? " is-active" : "")}
                            onClick={() => { saveThinkingMode(level.id); }}>
                            {t(level.en, level.zh)}
                        </button>
                    );
                })}
            </div>
            <p className="llm-tuning-row__note">{thinkingLevelNote(thinkingMode, t)}</p>
            {thinkingModeError && <p role="alert" className="llm-tuning-row__note llm-tuning-row__note--danger">{thinkingModeError}</p>}
        </>
    );
}
