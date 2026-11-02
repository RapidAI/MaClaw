import { normalizeLang } from "../../i18n/langSelect";

/**
 * Display labels for built-in LLM provider names.
 * The stored name stays the canonical identifier (OAuth, logos, saves).
 * Mongolian UI must not show the Chinese portion of those presets.
 */
const MN_PRESET_PROVIDER_NAMES: Record<string, string> = {
    "MaClaw官方": "MaClaw",
    "MaClaw 官方": "MaClaw",
    "MaClaw模型服务": "MaClaw LLM",
    "智谱编程": "Zhipu",
    "智谱 GLM (Coding)": "Zhipu",
    "Zhipu GLM Coding": "Zhipu",
    // Distinct from the LobsterAI preset.
    "智谱龙虾": "Zhipu Lobster",
    "火山引擎 Agent Plan": "Volc Agent",
    "火山引擎TokenPlan": "Volc TokenPlan",
    "火山引擎TokenPlan (Anthropic)": "Volc TokenPlan Ant",
    "火山引擎TokenPlan Anthropic": "Volc TokenPlan Ant",
    "腾讯TokenPlan": "TokenPlan",
    "腾讯云": "Tencent",
    "讯飞星辰": "iFlytek",
    // China Telecom TeleAI. Distinct from iFlytek (讯飞星辰).
    "星辰大模型": "TeleAI",
    // Distinct from the Kimi and Kimi Code presets.
    "Kimi (月之暗面)": "Moonshot",
    "Doubao (豆包)": "Doubao",
    "阿里云 (百炼)": "Bailian",
    "阿里云": "Aliyun",
    "百度千帆": "Qianfan",
    "智谱": "Zhipu",
    "GLM(智谱)": "Zhipu",
    "GLM (智谱)": "Zhipu",
    "WorkBuddy 国内版": "WorkBuddy CN",
    "WorkBuddy 国际版": "WorkBuddy Global",
    "Qoder 国内版": "Qoder CN",
    "Qoder 国际版": "Qoder Global",
    "Trae 国内版": "Trae CN",
    "Trae 国际版": "Trae Global",
};

export function presetProviderDisplayName(lang: string | undefined | null, name: string | undefined | null): string {
    const raw = String(name || "").trim();
    if (!raw || normalizeLang(lang) !== "mn") return raw;
    return MN_PRESET_PROVIDER_NAMES[raw] ?? raw;
}
