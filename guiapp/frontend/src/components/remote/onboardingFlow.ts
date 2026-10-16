export type OnboardingLang = 'en' | 'zh-Hans' | 'zh-Hant' | string | undefined;

export type OnboardingStepId = 'mode' | 'register' | 'sso' | 'llm' | 'wechat';

export interface OnboardingFlow {
    readonly isTigerclaw: boolean;
    readonly steps: readonly OnboardingStepId[];
    readonly totalSteps: number;
    readonly wxStep: number;
    readonly llmStep: number | null;
}

export interface OnboardingStepDoneState {
    readonly regDone: boolean;
    readonly llmDone: boolean;
    readonly wxCompleted: boolean;
}

export const TIGERCLAW_BRAND_ID = 'qianxin';

const STEP_LABELS: Record<OnboardingStepId, { en: string; zhHans: string; zhHant: string }> = {
    mode: {
        en: 'Mode',
        zhHans: '\u8fd0\u884c\u6a21\u5f0f',
        zhHant: '\u904b\u884c\u6a21\u5f0f',
    },
    register: {
        en: 'Account',
        zhHans: '\u8d26\u53f7\u6ce8\u518c',
        zhHant: '\u5e33\u865f\u8a3b\u518a',
    },
    sso: {
        en: 'SSO Auth',
        zhHans: '\u4f01\u4e1a\u8ba4\u8bc1',
        zhHant: '\u4f01\u696d\u8a8d\u8b49',
    },
    llm: {
        en: 'LLM',
        zhHans: '\u914d\u7f6e LLM',
        zhHant: '\u914d\u7f6e LLM',
    },
    wechat: {
        en: 'WeChat',
        zhHans: '\u7ed1\u5b9a\u5fae\u4fe1',
        zhHant: '\u7d81\u5b9a\u5fae\u4fe1',
    },
};

const localizeLabel = (lang: OnboardingLang, step: OnboardingStepId): string => {
    const labels = STEP_LABELS[step];
    if (lang === 'zh-Hans') return labels.zhHans;
    if (lang === 'zh-Hant') return labels.zhHant;
    return labels.en;
};

export const isTigerClawBrand = (brandId?: string): boolean => brandId === TIGERCLAW_BRAND_ID;

export const getOnboardingFlow = ({
    brandId,
    freeTrial,
    offlineMode = false,
}: {
    brandId?: string;
    freeTrial: boolean;
    offlineMode?: boolean;
}): OnboardingFlow => {
    const isTigerclaw = isTigerClawBrand(brandId);
    const steps: readonly OnboardingStepId[] = isTigerclaw
        ? ['sso', 'wechat']
        : offlineMode
            ? ['mode', 'llm']
        : freeTrial
            ? ['register', 'wechat']
            : ['register', 'llm', 'wechat'];

    return {
        isTigerclaw,
        steps,
        totalSteps: steps.length,
        wxStep: steps.indexOf('wechat') + 1,
        llmStep: steps.includes('llm') ? steps.indexOf('llm') + 1 : null,
    };
};

export const getOnboardingStepLabels = (flow: OnboardingFlow, lang: OnboardingLang): string[] => (
    flow.steps.map((step) => localizeLabel(lang, step))
);

export const getOnboardingStepDone = (
    flow: OnboardingFlow,
    { regDone, llmDone, wxCompleted }: OnboardingStepDoneState,
): boolean[] => {
    const doneByStep: Record<OnboardingStepId, boolean> = {
        mode: regDone,
        register: regDone,
        sso: regDone && llmDone,
        llm: llmDone,
        wechat: wxCompleted,
    };
    return [false, ...flow.steps.map((step) => doneByStep[step])];
};

export const isCurrentOnboardingStep = (
    flow: OnboardingFlow,
    stepIndex: number,
    stepId: OnboardingStepId,
): boolean => flow.steps[stepIndex - 1] === stepId;

export const isOnboardingComplete = (flow: OnboardingFlow, state: OnboardingStepDoneState): boolean => (
    flow.steps.every((step) => {
        if (step === 'register') return state.regDone;
        if (step === 'mode') return state.regDone;
        if (step === 'sso') return state.regDone && state.llmDone;
        if (step === 'llm') return state.llmDone;
        return state.wxCompleted;
    })
);
