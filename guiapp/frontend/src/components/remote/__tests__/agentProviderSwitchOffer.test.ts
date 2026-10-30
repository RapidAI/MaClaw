import { describe, expect, it } from "vitest";
import { presentDialogMessage } from "../../CustomDialog";
import { agentProviderSwitchConfirmCopy, agentProviderSwitchPlan, assistantAlreadyUsesProvider } from "../agentProviderSwitchOffer";

const zh = (en: string, zhHans: string) => zhHans;

describe("assistantAlreadyUsesProvider", () => {
    it("matches the assistant by provider name or id", () => {
        const state = {
            providers: [{ id: "deepseek", name: "DeepSeek" }],
            assistant: { provider_id: "deepseek", provider_name: "DeepSeek" },
        };
        expect(assistantAlreadyUsesProvider(state, " deepseek ")).toBe(true);
        expect(assistantAlreadyUsesProvider(state, "OpenAI")).toBe(false);
        expect(assistantAlreadyUsesProvider(null, "DeepSeek")).toBe(false);
    });

    it("matches when only the provider id is present on the assistant summary", () => {
        expect(assistantAlreadyUsesProvider({
            providers: [{ ID: "Custom1", Name: "Custom1" }],
            assistant: { providerID: "custom1" },
        }, "Custom1")).toBe(true);
    });
});

describe("agentProviderSwitchPlan", () => {
    const state = {
        providers: [{ id: "deepseek", name: "DeepSeek" }],
        assistant: { provider_id: "deepseek", provider_name: "DeepSeek", model: "deepseek-coder" },
    };

    it("asks when the assistant uses another provider or the assignment cannot be read", () => {
        expect(agentProviderSwitchPlan(state, "OpenAI", "gpt-5")).toBe("confirm");
        expect(agentProviderSwitchPlan(null, "DeepSeek", "deepseek-v4")).toBe("confirm");
        expect(agentProviderSwitchPlan(undefined, "DeepSeek")).toBe("confirm");
    });

    it("focuses without asking when the saved provider is already assigned to a different model", () => {
        expect(agentProviderSwitchPlan(state, " deepseek ", "DeepSeek-V4")).toBe("focus");
        expect(agentProviderSwitchPlan({
            providers: [{ ID: "Custom1", Name: "Custom1" }],
            assistant: { providerID: "custom1", Model: "old" },
        }, "Custom1", "new")).toBe("focus");
    });

    it("does nothing when the provider and model already match", () => {
        expect(agentProviderSwitchPlan(state, "DeepSeek", "deepseek-coder")).toBe("none");
        expect(agentProviderSwitchPlan(state, "DeepSeek", " DeepSeek-Coder ")).toBe("none");
        expect(agentProviderSwitchPlan(state, "DeepSeek", "")).toBe("none");
        expect(agentProviderSwitchPlan(state, "  ", "gpt")).toBe("none");
    });

    it("follows the saved assignment when the effective summary already names the new provider", () => {
        const providers = [
            { id: "openai", name: "OpenAI" },
            { id: "deepseek", name: "DeepSeek" },
        ];
        expect(agentProviderSwitchPlan({
            providers,
            profiles: { assistant: { provider_id: "openai", model: "gpt-5" } },
            assistant: { provider_id: "deepseek", provider_name: "DeepSeek", model: "deepseek-v4" },
        }, "DeepSeek", "deepseek-v4")).toBe("confirm");
        expect(agentProviderSwitchPlan({
            providers: [{ id: "deepseek", name: "DeepSeek" }],
            profiles: { assistant: { provider_id: "deepseek", model: "deepseek-coder" } },
            assistant: { provider_name: "DeepSeek", model: "deepseek-v4" },
        }, "DeepSeek", "deepseek-v4")).toBe("focus");
        expect(agentProviderSwitchPlan({
            providers: [{ id: "deepseek", name: "DeepSeek" }],
            profiles: { assistant: { provider_id: "deepseek", model: "deepseek-v4" } },
            assistant: { provider_name: "DeepSeek", model: "deepseek-coder" },
        }, "DeepSeek", "deepseek-v4")).toBe("none");
    });
});

describe("agentProviderSwitchConfirmCopy", () => {
    it("lifts the provider name and keeps the save instruction", () => {
        const copy = agentProviderSwitchConfirmCopy(zh, "DeepSeek");
        expect(copy.title).toBe("切换服务商");
        const presented = presentDialogMessage(copy.message);
        expect(presented.lead).toBe("是否将 Agent 的服务商切换为？");
        expect(presented.subject).toBe("DeepSeek");
        expect(presented.follow).toBe("请选择模型并保存配置，即可生效。");
    });
});
