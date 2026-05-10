import { describe, expect, it } from 'vitest';
import {
    getOnboardingFlow,
    getOnboardingStepDone,
    getOnboardingStepLabels,
    isCurrentOnboardingStep,
    isOnboardingComplete,
    isTigerClawBrand,
} from '../onboardingFlow';

describe('onboardingFlow', () => {
    it('keeps TigerClaw to SSO plus WeChat without LLM setup', () => {
        const flow = getOnboardingFlow({ brandId: 'qianxin', freeTrial: false });

        expect(isTigerClawBrand('qianxin')).toBe(true);
        expect(flow.isTigerclaw).toBe(true);
        expect(flow.steps).toEqual(['sso', 'wechat']);
        expect(flow.totalSteps).toBe(2);
        expect(flow.wxStep).toBe(2);
        expect(flow.llmStep).toBeNull();
        expect(getOnboardingStepLabels(flow, 'en')).toEqual(['SSO Auth', 'WeChat']);
        expect(getOnboardingStepDone(flow, { regDone: true, llmDone: true, wxCompleted: false })).toEqual([false, true, false]);
        expect(isOnboardingComplete(flow, { regDone: true, llmDone: true, wxCompleted: true })).toBe(true);
    });

    it('keeps standard free trial to register plus WeChat', () => {
        const flow = getOnboardingFlow({ brandId: undefined, freeTrial: true });

        expect(flow.isTigerclaw).toBe(false);
        expect(flow.steps).toEqual(['register', 'wechat']);
        expect(flow.totalSteps).toBe(2);
        expect(flow.wxStep).toBe(2);
        expect(flow.llmStep).toBeNull();
        expect(getOnboardingStepLabels(flow, 'zh-Hans')).toEqual(['邮箱注册', '绑定微信']);
        expect(getOnboardingStepDone(flow, { regDone: true, llmDone: false, wxCompleted: true })).toEqual([false, true, true]);
        expect(isOnboardingComplete(flow, { regDone: true, llmDone: false, wxCompleted: true })).toBe(true);
    });

    it('adds LLM only when standard free trial is disabled', () => {
        const flow = getOnboardingFlow({ brandId: 'maclaw', freeTrial: false });

        expect(flow.steps).toEqual(['register', 'llm', 'wechat']);
        expect(flow.totalSteps).toBe(3);
        expect(flow.wxStep).toBe(3);
        expect(flow.llmStep).toBe(2);
        expect(getOnboardingStepLabels(flow, 'zh-Hant')).toEqual(['郵箱註冊', '配置 LLM', '綁定微信']);
        expect(isCurrentOnboardingStep(flow, 2, 'llm')).toBe(true);
        expect(getOnboardingStepDone(flow, { regDone: true, llmDone: false, wxCompleted: true })).toEqual([false, true, false, true]);
        expect(isOnboardingComplete(flow, { regDone: true, llmDone: false, wxCompleted: true })).toBe(false);
    });

    it('uses mode plus LLM only in offline mode', () => {
        const flow = getOnboardingFlow({ brandId: 'maclaw', freeTrial: true, offlineMode: true });

        expect(flow.steps).toEqual(['mode', 'llm']);
        expect(flow.totalSteps).toBe(2);
        expect(flow.wxStep).toBe(0);
        expect(flow.llmStep).toBe(2);
        expect(getOnboardingStepLabels(flow, 'en')).toEqual(['Mode', 'LLM']);
        expect(getOnboardingStepDone(flow, { regDone: true, llmDone: false, wxCompleted: false })).toEqual([false, true, false]);
        expect(isOnboardingComplete(flow, { regDone: true, llmDone: true, wxCompleted: false })).toBe(true);
    });
});
