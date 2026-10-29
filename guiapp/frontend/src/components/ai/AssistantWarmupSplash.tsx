import type React from "react";
import type { AIAssistantInitStatus } from "./useAIAssistant";
import { chromeText } from "../../i18n/chromeText";

/** Warm-up stages shown while the panel is not ready, in the order the
 * backend reports them (connecting → loading → warming → ready). The splash
 * disappears on ready/degraded, so those two never render a step. */
const WARMUP_STEPS: ReadonlyArray<{ key: AIAssistantInitStatus; labelEn: string; labelZhHans: string; labelZhHant: string; progress: number }> = [
    { key: "connecting", labelEn: "Connect", labelZhHans: "连接服务", labelZhHant: "連線服務", progress: 18 },
    { key: "loading", labelEn: "Load components", labelZhHans: "加载组件", labelZhHant: "載入元件", progress: 58 },
    { key: "warming", labelEn: "Warm up", labelZhHans: "预热就绪", labelZhHant: "預熱就緒", progress: 88 },
];

function warmupStageIndex(status: AIAssistantInitStatus | undefined): number {
    const idx = WARMUP_STEPS.findIndex((step) => step.key === (status ?? "connecting"));
    return idx < 0 ? 0 : idx;
}

function warmupStepLabel(step: (typeof WARMUP_STEPS)[number], lang: string): string {
    return chromeText(lang, step.labelEn, step.labelZhHans, step.labelZhHant);
}

interface AssistantWarmupSplashProps {
    initLabel: string;
    /** Live warm-up stage; drives the stepper + progress. */
    initStatus?: AIAssistantInitStatus;
    lang: string;
    brandLine: React.ReactNode;
}

/** Pre-ready splash under the brand mark: staged progress bar, three-step
 * indicator and the live status line. All colors come from theme tokens so
 * light/dark schemes restyle it without component changes. */
export function AssistantWarmupSplash({ initLabel, initStatus, lang, brandLine }: AssistantWarmupSplashProps) {
    const current = warmupStageIndex(initStatus);
    return (
        <div className="acb-loading acb-warmup">
            {brandLine}
            <div className="acb-warmup-panel" role="status" aria-live="polite">
                <div className="acb-warmup-progress" aria-hidden="true">
                    <span
                        className="acb-warmup-progress__fill"
                        style={{ width: `${WARMUP_STEPS[current]?.progress ?? 18}%` }}
                    />
                </div>
                <ol className="acb-warmup-steps">
                    {WARMUP_STEPS.map((step, index) => {
                        const state = index < current ? "done" : index === current ? "active" : "todo";
                        return (
                            <li
                                key={step.key}
                                className={`acb-warmup-step acb-warmup-step--${state}`}
                                aria-current={state === "active" ? "step" : undefined}
                            >
                                <span className="acb-warmup-step__dot" aria-hidden="true">
                                    {state === "done" ? (
                                        <svg width="10" height="10" viewBox="0 0 12 12" fill="none">
                                            <path d="m2.5 6.5 2.2 2.2L9.5 3.9" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" />
                                        </svg>
                                    ) : null}
                                </span>
                                <span className="acb-warmup-step__label">{warmupStepLabel(step, lang)}</span>
                            </li>
                        );
                    })}
                </ol>
                <div className="acb-warmup-status">{initLabel}</div>
            </div>
        </div>
    );
}
