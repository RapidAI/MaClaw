import { describe, expect, it } from "vitest";
import { presetProviderDisplayName } from "../presetProviderLabel";

const MN_NAMES: Record<string, string> = {
    "MaClaw官方": "MaClaw",
    "智谱编程": "Zhipu",
    "火山引擎 Agent Plan": "Volc Agent",
    "腾讯TokenPlan": "TokenPlan",
    "讯飞星辰": "iFlytek",
    "星辰大模型": "TeleAI",
    "WorkBuddy 国内版": "WorkBuddy CN",
    "WorkBuddy 国际版": "WorkBuddy Global",
    "Qoder 国内版": "Qoder CN",
    "Qoder 国际版": "Qoder Global",
    "Trae 国内版": "Trae CN",
    "Trae 国际版": "Trae Global",
};

describe("presetProviderDisplayName", () => {
    it("localizes built-in provider names in Mongolian", () => {
        for (const [canonical, label] of Object.entries(MN_NAMES)) {
            expect(presetProviderDisplayName("mn", canonical)).toBe(label);
            expect(label).not.toMatch(/[\u3400-\u9fff]/);
        }
    });

    it("keeps the canonical name for Chinese, English, and custom providers", () => {
        expect(presetProviderDisplayName("zh-Hans", "智谱编程")).toBe("智谱编程");
        expect(presetProviderDisplayName("en", "WorkBuddy 国内版")).toBe("WorkBuddy 国内版");
        expect(presetProviderDisplayName("mn", "OpenAI")).toBe("OpenAI");
        expect(presetProviderDisplayName("mn", "Custom1")).toBe("Custom1");
        expect(presetProviderDisplayName("mn", "  ")).toBe("");
        expect(presetProviderDisplayName("mn", "讯飞星辰")).not.toBe(presetProviderDisplayName("mn", "星辰大模型"));
        expect(presetProviderDisplayName("mn", "智谱 GLM (Coding)")).toBe("Zhipu");
        expect(presetProviderDisplayName("mn", "Zhipu GLM Coding")).toBe("Zhipu");
        expect(presetProviderDisplayName("mn", "智谱龙虾")).toBe("Zhipu Lobster");
        expect(presetProviderDisplayName("mn", "智谱龙虾")).not.toBe(presetProviderDisplayName("mn", "LobsterAI"));
        expect(presetProviderDisplayName("mn", "Kimi (月之暗面)")).toBe("Moonshot");
        expect(presetProviderDisplayName("mn", "Kimi (月之暗面)")).not.toBe(presetProviderDisplayName("mn", "Kimi"));
        expect(presetProviderDisplayName("mn", "Kimi (月之暗面)")).not.toBe(presetProviderDisplayName("mn", "Kimi Code"));
        expect(presetProviderDisplayName("mn", "腾讯云")).toBe("Tencent");
        expect(presetProviderDisplayName("mn", "阿里云 (百炼)")).toBe("Bailian");
        expect(presetProviderDisplayName("mn", "阿里云")).toBe("Aliyun");
        expect(presetProviderDisplayName("mn", "百度千帆")).toBe("Qianfan");
        expect(presetProviderDisplayName("mn", "GLM(智谱)")).toBe("Zhipu");
        expect(presetProviderDisplayName("mn", "GLM (智谱)")).toBe("Zhipu");
        for (const label of Object.values(MN_NAMES)) {
            expect(label.length).toBeLessThanOrEqual(16);
        }
    });
});
