export const subscriptionUrls: { [key: string]: string } = {
    "GLM": "https://bigmodel.cn/glm-coding",
    "Kimi": "https://www.kimi.com/membership/pricing?from=upgrade_plan&track_id=1d2446f5-f45f-4ae5-961e-c0afe936a115",
    "Doubao": "https://www.volcengine.com/activity/codingplan",
    "腾讯云": "https://cloud.tencent.com/document/product/1772/128947",
    "讯飞星辰": "https://www.xfyun.cn/doc/spark/CodingPlan.html",
    "MiniMax": "https://platform.minimaxi.com/user-center/payment/coding-plan",
    "百度千帆": "https://cloud.baidu.com/product/codingplan.html",
    "Codex": "https://www.aicodemirror.com/register?invitecode=CZPPWZ",
    "Gemini": "https://www.aicodemirror.com/register?invitecode=CZPPWZ",
    "DeepSeek": "https://platform.deepseek.com/api_keys",
    "ChatFire": "https://api.chatfire.cn/register?aff=jira",
    "XiaoMi": "https://platform.xiaomimimo.com/#/console/api-keys",
    "摩尔线程": "https://code.mthreads.com/",
    "快手": "https://www.streamlake.com/marketing/coding-plan",
    "阿里云": "https://coding.dashscope.aliyuncs.com/"
};

// Known provider API endpoints database
// Organized by protocol type: anthropic (Claude), gemini, openai (Codex)
export interface ProviderEndpoint {
    name: string;
    url: string;
    protocol: 'anthropic' | 'gemini' | 'openai';
    region: 'china' | 'global';
    description?: string;
}

export const sidebarProviderAliases: Record<string, string[]> = {
    "智谱": ["GLM(智谱)", "GLM (智谱)"],
    "GLM(智谱)": ["智谱", "GLM (智谱)"],
    "GLM (智谱)": ["智谱", "GLM(智谱)"],
};

export const PROJECT_PAGE_SIZE = 5;

export const knownProviderEndpoints: ProviderEndpoint[] = [
    // Anthropic Protocol (Claude)
    { name: "Claude Official", url: "https://api.anthropic.com/v1", protocol: "anthropic", region: "global", description: "Official Claude API" },
    { name: "MiniMax", url: "https://api.minimaxi.com/anthropic", protocol: "anthropic", region: "china" },
    { name: "DeepSeek", url: "https://api.deepseek.com/anthropic", protocol: "anthropic", region: "china" },
    { name: "腾讯云", url: "https://api.lkeap.cloud.tencent.com/coding/anthropic", protocol: "anthropic", region: "china", description: "Tencent Cloud Claude-compatible endpoint" },
    { name: "ChatFire", url: "https://api.chatfire.cn/v1", protocol: "anthropic", region: "china" },
    { name: "OpenRouter", url: "https://openrouter.ai/api", protocol: "anthropic", region: "global" },

    // Gemini Protocol
    { name: "Google Gemini Official", url: "https://generativelanguage.googleapis.com/v1beta", protocol: "gemini", region: "global", description: "Official Google Gemini API" },

    // OpenAI Protocol (Codex)
    { name: "OpenAI Official", url: "https://api.openai.com/v1", protocol: "openai", region: "global", description: "Official OpenAI API" },
    { name: "xAI (Grok)", url: "https://api.x.ai/v1", protocol: "openai", region: "global", description: "xAI Grok API" },
    { name: "GLM", url: "https://open.bigmodel.cn/api/paas/v4", protocol: "openai", region: "china" },
    { name: "Kimi", url: "https://api.kimi.com/coding/v1", protocol: "openai", region: "china" },
    { name: "Doubao", url: "https://ark.cn-beijing.volces.com/api/coding", protocol: "openai", region: "china" },
    { name: "腾讯云", url: "https://api.lkeap.cloud.tencent.com/coding/v3", protocol: "openai", region: "china", description: "Tencent Cloud OpenAI-compatible endpoint" },
    { name: "Doubao Codex", url: "https://ark.cn-beijing.volces.com/api/coding/v3", protocol: "openai", region: "china" },
    { name: "DeepSeek Codex", url: "https://api.aicodemirror.com/api/codex/backend-api/codex", protocol: "openai", region: "china" },
    { name: "OpenRouter", url: "https://openrouter.ai/api/v1", protocol: "openai", region: "global" },
    { name: "Together AI", url: "https://api.together.xyz/v1", protocol: "openai", region: "global" },
    { name: "Groq", url: "https://api.groq.com/openai/v1", protocol: "openai", region: "global" },
    { name: "Perplexity", url: "https://api.perplexity.ai", protocol: "openai", region: "global" },
];



// Recommended model IDs per provider (used for model name suggestions)
export const recommendedModels: { [provider: string]: { id: string; note?: string }[] } = {
    "GLM": [{ id: "glm-4.7" }],
    "Kimi": [{ id: "kimi-k2-thinking" }, { id: "kimi-for-coding" }],
    "Doubao": [{ id: "doubao-seed-code-preview-latest" }],
    "MiniMax": [{ id: "MiniMax-M2.1" }],
    "DeepSeek": [{ id: "deepseek-chat" }],
    "ChatFire": [{ id: "sonnet" }, { id: "gpt-5.1-codex-mini" }, { id: "gpt-4o" }, { id: "gemini-2.5-pro" }],
    "XiaoMi": [{ id: "mimo-v2-flash" }],
    "摩尔线程": [{ id: "GLM-4.7" }],
    "快手": [{ id: "kat-coder-pro-v1" }],
    "腾讯云": [
        { id: "glm-5", note: "默认" },
        { id: "tc-code-latest", note: "Auto" },
        { id: "hunyuan-2.0-instruct" },
        { id: "hunyuan-2.0-thinking" },
        { id: "hunyuan-t1" },
        { id: "hunyuan-turbos" },
        { id: "minimax-m2.5" },
        { id: "kimi-k2.5" },
    ],
    "阿里云": [
        { id: "qwen3.5-plus", note: "支持图片理解" },
        { id: "kimi-k2.5", note: "支持图片理解" },
        { id: "glm-5" },
        { id: "MiniMax-M2.5" },
        { id: "qwen3-max-2026-01-23" },
        { id: "qwen3-coder-next" },
        { id: "qwen3-coder-plus" },
        { id: "glm-4.7" },
    ],
};

// Localized display names for providers that use non-English ModelName identifiers
const providerDisplayNames: { [lang: string]: { [key: string]: string } } = {
    "en": {
        "摩尔线程": "MooreThreads",
        "快手": "Kuaishou"
    },
    "zh-Hans": {
        "摩尔线程": "摩尔线程",
        "快手": "快手"
    },
    "zh-Hant": {
        "摩尔线程": "摩爾線程",
        "快手": "快手"
    }
};

// Get localized display name for a model, falling back to the raw name
export const getModelDisplayName = (modelName: string, lang: string): string => {
    const effectiveLang = lang === 'zh' ? 'zh-Hans' : lang;
    return providerDisplayNames[effectiveLang]?.[modelName] ?? providerDisplayNames["en"]?.[modelName] ?? modelName;
};
