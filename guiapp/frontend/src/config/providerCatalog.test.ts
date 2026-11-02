import { describe, expect, it } from "vitest";
import { getModelDisplayName, localizeModelNote } from "./providerCatalog";

describe("getModelDisplayName", () => {
    it("uses the short Mongolian provider label for saved Chinese names", () => {
        expect(getModelDisplayName("腾讯云", "mn")).toBe("Tencent");
        expect(getModelDisplayName("腾讯TokenPlan", "mn")).toBe("TokenPlan");
        expect(getModelDisplayName("星辰大模型", "mn")).toBe("TeleAI");
        expect(getModelDisplayName("百度千帆", "mn")).toBe("Qianfan");
        expect(getModelDisplayName("阿里云", "mn")).toBe("Aliyun");
        expect(getModelDisplayName("摩尔线程", "mn")).toBe("MooreThreads");
        expect(getModelDisplayName("快手", "mn")).toBe("Kuaishou");
        expect(getModelDisplayName("OpenAI", "mn")).toBe("OpenAI");
    });

    it("keeps Chinese and English tool labels unchanged", () => {
        expect(getModelDisplayName("GLM", "zh-Hans")).toBe("智谱");
        expect(getModelDisplayName("腾讯云", "zh-Hans")).toBe("腾讯云");
        expect(getModelDisplayName("腾讯云", "en")).toBe("腾讯云");
        expect(getModelDisplayName("摩尔线程", "en")).toBe("MooreThreads");
    });

    it("localizes recommended-model notes without changing the stored id", () => {
        expect(localizeModelNote("mn", "智能路由")).toBe("Smart route");
        expect(localizeModelNote("mn", "旗舰")).toBe("Flagship");
        expect(localizeModelNote("mn", "轻量")).toBe("Lite");
        expect(localizeModelNote("mn", "默认")).toBe("Анхдагч");
        expect(localizeModelNote("mn", "支持图片理解")).toBe("Vision");
        expect(localizeModelNote("zh-Hans", "旗舰")).toBe("旗舰");
        expect(localizeModelNote("en", "默认")).toBe("Default");
        expect(localizeModelNote("en", "Auto")).toBe("Auto");
    });
});
