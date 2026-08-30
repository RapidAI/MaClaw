import type { Theme } from "./aiAssistantPanelTheme";

export type AssistantLightSchemeId = "default" | "notion" | "linear" | "github" | "stripe" | "vercel";

export type AssistantLightScheme = {
    id: AssistantLightSchemeId;
    storageValue: string;
    label: { en: string; zhHans: string; zhHant: string };
    description: { en: string; zhHans: string; zhHant: string };
    cssVars: {
        pageBg: string;
        surface: string;
        surfaceMuted: string;
        primary: string;
        primaryStrong: string;
        primarySoft: string;
        textPrimary: string;
        textSecondary: string;
        textMuted: string;
        border: string;
        borderSubtle: string;
        success: string;
        successBg: string;
        warning: string;
        warningBg: string;
        danger: string;
        dangerBg: string;
        linkColor: string;
        infoBg: string;
    };
    assistantTheme: Theme;
};

export const ASSISTANT_LIGHT_SCHEME_STORAGE_KEY = "maclaw.ai.lightScheme";
/** The palette used when no light-mode preference has been saved yet. */
export const DEFAULT_ASSISTANT_LIGHT_SCHEME_ID: AssistantLightSchemeId = "github";

/**
 * Classic Blue-Gray — the original calm blue-gray light theme.
 * Kept as an explicit option for users who prefer the previous appearance.
 */
export const defaultLightScheme: AssistantLightScheme = {
    id: "default",
    storageValue: "default",
    label: {
        en: "Classic Blue-Gray",
        zhHans: "经典蓝灰",
        zhHant: "經典藍灰",
    },
    description: {
        en: "The existing calm blue-gray light theme.",
        zhHans: "当前默认的沉稳蓝灰亮色方案。",
        zhHant: "當前預設的沉穩藍灰亮色方案。",
    },
    cssVars: {
        pageBg: "#f4f7fb",
        surface: "#ffffff",
        surfaceMuted: "#f0f4f9",
        primary: "#2f6fbc",
        primaryStrong: "#235a9e",
        primarySoft: "rgba(47, 111, 188, 0.10)",
        textPrimary: "#1c2733",
        textSecondary: "#44546a",
        textMuted: "#657384",
        border: "#d9e1ec",
        borderSubtle: "#e8eef5",
        success: "#4f7f6f",
        successBg: "#f3f7f5",
        warning: "#64748b",
        warningBg: "#f8fafc",
        danger: "#c43d34",
        dangerBg: "#fbf1f0",
        linkColor: "#2f6fbc",
        infoBg: "#f3f7fb",
    },
    assistantTheme: {
        bg: "#f4f7fb",
        titleBarBg: "#edf2f8",
        titleBarBorder: "#d9e1ec",
        titleText: "#44546a",
        text: "#1c2733",
        textMuted: "#657384",
        inputBarBg: "#ffffff",
        inputBarBorder: "#c9d6e4",
        inputText: "#1c2733",
        codeBg: "#edf2f8",
        codeText: "#44546a",
        codeBlockBg: "#f6f9fc",
        codeBlockBorder: "#d9e1ec",
        codeBlockLang: "#657384",
        borderLeft: "#d9e1ec",
        responseBorderLeft: "#8aa4bf",
        headingColor: "#1c2733",
        linkColor: "#2f6fbc",
        pathColor: "#44546a",
        promptColor: "#44546a",
        userColor: "#44546a",
        divider: "#d9e1ec",
        fieldBg: "#f6f9fc",
        fieldBorder: "#d9e1ec",
        fieldLabel: "#657384",
        errorText: "#c43d34",
        errorBg: "rgba(196, 61, 52, 0.06)",
        errorBorder: "#c43d34",
        emptyHint: "#657384",
        boldColor: "#141c26",
        italicColor: "#44546a",
        bulletColor: "#657384",
        quoteBorder: "#b7c5d4",
        quoteText: "#526579",
        btnColor: "#2f6fbc",
        btnBorder: "#b7c5d4",
        actionBtnColor: "#657384",
        closeBtnColor: "#657384",
        sendBtnColor: "#ffffff",
        sendBtnBorder: "#2f6fbc",
        sendBtnBg: "#2f6fbc",
    },
};

/**
 * Notion — warm off-white with brown-tinted text, inspired by Notion's clean workspace feel.
 * Warm, inviting, content-focused.
 */
export const notionLightScheme: AssistantLightScheme = {
    id: "notion",
    storageValue: "notion",
    label: {
        en: "Notion Warmth",
        zhHans: "暖白笔记",
        zhHant: "暖白筆記",
    },
    description: {
        en: "Warm off-white with soft brown accents, inspired by Notion.",
        zhHans: "柔和暖白底色搭配棕色调文字，灵感来自 Notion。",
        zhHant: "柔和暖白底色搭配棕色調文字，靈感來自 Notion。",
    },
    cssVars: {
        pageBg: "#ffffff",
        surface: "#ffffff",
        surfaceMuted: "#fbfbfa",
        primary: "#2f3437",
        primaryStrong: "#1a1d1f",
        primarySoft: "rgba(47, 52, 55, 0.06)",
        textPrimary: "#37352f",
        textSecondary: "#55534e",
        textMuted: "#73716e",
        border: "#e9e9e7",
        borderSubtle: "#f1f1ef",
        success: "#0f7b6c",
        successBg: "rgba(15, 123, 108, 0.06)",
        warning: "#d9730d",
        warningBg: "rgba(217, 115, 13, 0.06)",
        danger: "#e03e3e",
        dangerBg: "rgba(224, 62, 62, 0.06)",
        linkColor: "#2f3437",
        infoBg: "rgba(47, 52, 55, 0.04)",
    },
    assistantTheme: {
        bg: "#ffffff",
        titleBarBg: "#fbfbfa",
        titleBarBorder: "#e9e9e7",
        titleText: "#37352f",
        text: "#37352f",
        textMuted: "#73716e",
        inputBarBg: "#ffffff",
        inputBarBorder: "#e3e2e0",
        inputText: "#37352f",
        codeBg: "#f7f6f3",
        codeText: "#eb5757",
        codeBlockBg: "#f7f6f3",
        codeBlockBorder: "#e9e9e7",
        codeBlockLang: "#73716e",
        borderLeft: "#e9e9e7",
        responseBorderLeft: "#d4d3d0",
        headingColor: "#37352f",
        linkColor: "#2f3437",
        pathColor: "#55534e",
        promptColor: "#55534e",
        userColor: "#37352f",
        divider: "#ededec",
        fieldBg: "#fbfbfa",
        fieldBorder: "#e9e9e7",
        fieldLabel: "#73716e",
        errorText: "#e03e3e",
        errorBg: "rgba(224, 62, 62, 0.05)",
        errorBorder: "#e03e3e",
        emptyHint: "#73716e",
        boldColor: "#37352f",
        italicColor: "#55534e",
        bulletColor: "#73716e",
        quoteBorder: "#d4d3d0",
        quoteText: "#6b6966",
        btnColor: "#2f3437",
        btnBorder: "#d4d3d0",
        actionBtnColor: "#6f6e6b",
        closeBtnColor: "#6f6e6b",
        sendBtnColor: "#ffffff",
        sendBtnBorder: "#2f3437",
        sendBtnBg: "#2f3437",
    },
};

/**
 * Linear — crisp near-white with indigo/violet accent, inspired by Linear's modern SaaS aesthetic.
 * Sharp, focused, modern product tool feel.
 */
export const linearLightScheme: AssistantLightScheme = {
    id: "linear",
    storageValue: "linear",
    label: {
        en: "Linear Indigo",
        zhHans: "靛蓝利落",
        zhHant: "靛藍俐落",
    },
    description: {
        en: "Crisp whites with indigo-violet accents, inspired by Linear.",
        zhHans: "纯净白底搭配靛蓝紫色强调，灵感来自 Linear。",
        zhHant: "純淨白底搭配靛藍紫色強調，靈感來自 Linear。",
    },
    cssVars: {
        pageBg: "#fbfbfd",
        surface: "#ffffff",
        surfaceMuted: "#f5f5f8",
        primary: "#5e6ad2",
        primaryStrong: "#4850b8",
        primarySoft: "rgba(94, 106, 210, 0.08)",
        textPrimary: "#1b1c20",
        textSecondary: "#3c3e44",
        textMuted: "#6b7078",
        border: "#e4e5ea",
        borderSubtle: "#eeeff2",
        success: "#26b583",
        successBg: "rgba(38, 181, 131, 0.07)",
        warning: "#f2994a",
        warningBg: "rgba(242, 153, 74, 0.07)",
        danger: "#eb5757",
        dangerBg: "rgba(235, 87, 87, 0.06)",
        linkColor: "#5e6ad2",
        infoBg: "rgba(94, 106, 210, 0.05)",
    },
    assistantTheme: {
        bg: "#fbfbfd",
        titleBarBg: "#f5f5f8",
        titleBarBorder: "#e4e5ea",
        titleText: "#1b1c20",
        text: "#1b1c20",
        textMuted: "#6b7078",
        inputBarBg: "#ffffff",
        inputBarBorder: "#dcdde2",
        inputText: "#1b1c20",
        codeBg: "#f2f3f7",
        codeText: "#5e6ad2",
        codeBlockBg: "#f8f8fb",
        codeBlockBorder: "#e4e5ea",
        codeBlockLang: "#6b7078",
        borderLeft: "#e4e5ea",
        responseBorderLeft: "#b8bce0",
        headingColor: "#1b1c20",
        linkColor: "#5e6ad2",
        pathColor: "#3c3e44",
        promptColor: "#3c3e44",
        userColor: "#1b1c20",
        divider: "#eeeff2",
        fieldBg: "#f8f8fb",
        fieldBorder: "#e4e5ea",
        fieldLabel: "#6b7078",
        errorText: "#eb5757",
        errorBg: "rgba(235, 87, 87, 0.05)",
        errorBorder: "#eb5757",
        emptyHint: "#6b7078",
        boldColor: "#1b1c20",
        italicColor: "#3c3e44",
        bulletColor: "#6b7078",
        quoteBorder: "#b8bce0",
        quoteText: "#5a5d64",
        btnColor: "#5560c9",
        btnBorder: "#c4c8e4",
        actionBtnColor: "#6b7280",
        closeBtnColor: "#6b7280",
        sendBtnColor: "#ffffff",
        sendBtnBorder: "#5e6ad2",
        sendBtnBg: "#5e6ad2",
    },
};

/**
 * GitHub — cool gray background with blue accent links, inspired by GitHub's Primer design system.
 * Familiar, developer-friendly, neutral.
 */
export const githubLightScheme: AssistantLightScheme = {
    id: "github",
    storageValue: "github",
    label: {
        en: "GitHub Primer",
        zhHans: "GitHub 风格",
        zhHant: "GitHub 風格",
    },
    description: {
        en: "Cool gray surfaces with blue links, inspired by GitHub Primer.",
        zhHans: "冷灰背景搭配蓝色链接，灵感来自 GitHub Primer 设计系统。",
        zhHant: "冷灰背景搭配藍色連結，靈感來自 GitHub Primer 設計系統。",
    },
    cssVars: {
        pageBg: "#f6f8fa",
        surface: "#ffffff",
        surfaceMuted: "#f6f8fa",
        primary: "#0969da",
        primaryStrong: "#0550ae",
        primarySoft: "rgba(9, 105, 218, 0.08)",
        textPrimary: "#1f2328",
        textSecondary: "#424a53",
        textMuted: "#656d76",
        border: "#d0d7de",
        borderSubtle: "#e1e7ed",
        success: "#1a7f37",
        successBg: "rgba(26, 127, 55, 0.07)",
        warning: "#9a6700",
        warningBg: "rgba(154, 103, 0, 0.07)",
        danger: "#cf222e",
        dangerBg: "rgba(207, 34, 46, 0.06)",
        linkColor: "#0969da",
        infoBg: "rgba(9, 105, 218, 0.06)",
    },
    assistantTheme: {
        bg: "#f6f8fa",
        titleBarBg: "#f0f3f6",
        titleBarBorder: "#d0d7de",
        titleText: "#1f2328",
        text: "#1f2328",
        textMuted: "#656d76",
        inputBarBg: "#ffffff",
        inputBarBorder: "#d0d7de",
        inputText: "#1f2328",
        codeBg: "#eff2f5",
        codeText: "#0550ae",
        codeBlockBg: "#f6f8fa",
        codeBlockBorder: "#d0d7de",
        codeBlockLang: "#656d76",
        borderLeft: "#d0d7de",
        responseBorderLeft: "#a8c5e2",
        headingColor: "#1f2328",
        linkColor: "#0969da",
        pathColor: "#424a53",
        promptColor: "#424a53",
        userColor: "#1f2328",
        divider: "#d8dee4",
        fieldBg: "#f6f8fa",
        fieldBorder: "#d0d7de",
        fieldLabel: "#656d76",
        errorText: "#cf222e",
        errorBg: "rgba(207, 34, 46, 0.05)",
        errorBorder: "#cf222e",
        emptyHint: "#656d76",
        boldColor: "#1f2328",
        italicColor: "#424a53",
        bulletColor: "#656d76",
        quoteBorder: "#a8c5e2",
        quoteText: "#57606a",
        btnColor: "#0969da",
        btnBorder: "#afcde9",
        actionBtnColor: "#656d76",
        closeBtnColor: "#656d76",
        sendBtnColor: "#ffffff",
        sendBtnBorder: "#0969da",
        sendBtnBg: "#0969da",
    },
};

/**
 * Stripe — pure white with refined purple/blue accent, inspired by Stripe's documentation aesthetic.
 * Clean, professional, elegant.
 */
export const stripeLightScheme: AssistantLightScheme = {
    id: "stripe",
    storageValue: "stripe",
    label: {
        en: "Stripe Elegance",
        zhHans: "优雅紫白",
        zhHant: "優雅紫白",
    },
    description: {
        en: "Pure white with refined purple accents, inspired by Stripe.",
        zhHans: "纯净白底搭配精致紫色强调，灵感来自 Stripe。",
        zhHant: "純淨白底搭配精緻紫色強調，靈感來自 Stripe。",
    },
    cssVars: {
        pageBg: "#ffffff",
        surface: "#ffffff",
        surfaceMuted: "#f8f9fa",
        primary: "#635bff",
        primaryStrong: "#4b45c7",
        primarySoft: "rgba(99, 91, 255, 0.06)",
        textPrimary: "#1a1f36",
        textSecondary: "#3c4257",
        textMuted: "#697386",
        border: "#e3e8ee",
        borderSubtle: "#f0f2f5",
        success: "#0d9f6e",
        successBg: "rgba(13, 159, 110, 0.06)",
        warning: "#cb7e1a",
        warningBg: "rgba(203, 126, 26, 0.06)",
        danger: "#cd3d45",
        dangerBg: "rgba(205, 61, 69, 0.05)",
        linkColor: "#635bff",
        infoBg: "rgba(99, 91, 255, 0.04)",
    },
    assistantTheme: {
        bg: "#ffffff",
        titleBarBg: "#f8f9fa",
        titleBarBorder: "#e3e8ee",
        titleText: "#1a1f36",
        text: "#1a1f36",
        textMuted: "#697386",
        inputBarBg: "#ffffff",
        inputBarBorder: "#e3e8ee",
        inputText: "#1a1f36",
        codeBg: "#f7f8fa",
        codeText: "#635bff",
        codeBlockBg: "#fafbfc",
        codeBlockBorder: "#e3e8ee",
        codeBlockLang: "#697386",
        borderLeft: "#e3e8ee",
        responseBorderLeft: "#c4c1f7",
        headingColor: "#1a1f36",
        linkColor: "#635bff",
        pathColor: "#3c4257",
        promptColor: "#3c4257",
        userColor: "#1a1f36",
        divider: "#eef0f3",
        fieldBg: "#fafbfc",
        fieldBorder: "#e3e8ee",
        fieldLabel: "#697386",
        errorText: "#cd3d45",
        errorBg: "rgba(205, 61, 69, 0.04)",
        errorBorder: "#cd3d45",
        emptyHint: "#697386",
        boldColor: "#1a1f36",
        italicColor: "#3c4257",
        bulletColor: "#697386",
        quoteBorder: "#c4c1f7",
        quoteText: "#525f7f",
        btnColor: "#635bff",
        btnBorder: "#c4c1f7",
        actionBtnColor: "#697386",
        closeBtnColor: "#697386",
        sendBtnColor: "#ffffff",
        sendBtnBorder: "#635bff",
        sendBtnBg: "#635bff",
    },
};

/**
 * Vercel — high-contrast black-on-white with pure black accent, inspired by Vercel's stark design.
 * Minimalist, high-contrast, modern.
 */
export const vercelLightScheme: AssistantLightScheme = {
    id: "vercel",
    storageValue: "vercel",
    label: {
        en: "Vercel Stark",
        zhHans: "极简黑白",
        zhHant: "極簡黑白",
    },
    description: {
        en: "High-contrast black-on-white with minimal color, inspired by Vercel.",
        zhHans: "高对比度黑白极简设计，灵感来自 Vercel。",
        zhHant: "高對比度黑白極簡設計，靈感來自 Vercel。",
    },
    cssVars: {
        pageBg: "#ffffff",
        surface: "#ffffff",
        surfaceMuted: "#fafafa",
        primary: "#000000",
        primaryStrong: "#000000",
        primarySoft: "rgba(0, 0, 0, 0.05)",
        textPrimary: "#171717",
        textSecondary: "#404040",
        textMuted: "#737373",
        border: "#e5e5e5",
        borderSubtle: "#f0f0f0",
        success: "#17b169",
        successBg: "rgba(23, 177, 105, 0.06)",
        warning: "#f5a623",
        warningBg: "rgba(245, 166, 35, 0.06)",
        danger: "#e5484d",
        dangerBg: "rgba(229, 72, 77, 0.05)",
        linkColor: "#000000",
        infoBg: "rgba(0, 0, 0, 0.03)",
    },
    assistantTheme: {
        bg: "#ffffff",
        titleBarBg: "#fafafa",
        titleBarBorder: "#eaeaea",
        titleText: "#171717",
        text: "#171717",
        textMuted: "#737373",
        inputBarBg: "#ffffff",
        inputBarBorder: "#e5e5e5",
        inputText: "#171717",
        codeBg: "#f5f5f5",
        codeText: "#e5484d",
        codeBlockBg: "#fafafa",
        codeBlockBorder: "#eaeaea",
        codeBlockLang: "#737373",
        borderLeft: "#eaeaea",
        responseBorderLeft: "#c9c9c9",
        headingColor: "#171717",
        linkColor: "#000000",
        pathColor: "#404040",
        promptColor: "#404040",
        userColor: "#171717",
        divider: "#eaeaea",
        fieldBg: "#fafafa",
        fieldBorder: "#e5e5e5",
        fieldLabel: "#737373",
        errorText: "#e5484d",
        errorBg: "rgba(229, 72, 77, 0.04)",
        errorBorder: "#e5484d",
        emptyHint: "#737373",
        boldColor: "#000000",
        italicColor: "#404040",
        bulletColor: "#737373",
        quoteBorder: "#c9c9c9",
        quoteText: "#525252",
        btnColor: "#000000",
        btnBorder: "#d4d4d4",
        actionBtnColor: "#737373",
        closeBtnColor: "#737373",
        sendBtnColor: "#ffffff",
        sendBtnBorder: "#000000",
        sendBtnBg: "#000000",
    },
};

// Keep GitHub first so it is the primary/default choice shown in settings.
export const assistantLightSchemes = [githubLightScheme, defaultLightScheme, notionLightScheme, linearLightScheme, stripeLightScheme, vercelLightScheme] as const;

export function isAssistantLightSchemeId(value: unknown): value is AssistantLightSchemeId {
    return value === "default" || value === "notion" || value === "linear" || value === "github" || value === "stripe" || value === "vercel";
}

export function getAssistantLightScheme(id: unknown): AssistantLightScheme {
    return assistantLightSchemes.find((scheme) => scheme.id === id)
        || assistantLightSchemes.find((scheme) => scheme.id === DEFAULT_ASSISTANT_LIGHT_SCHEME_ID)
        || githubLightScheme;
}

export function readStoredAssistantLightSchemeId(): AssistantLightSchemeId {
    if (typeof window === "undefined") return DEFAULT_ASSISTANT_LIGHT_SCHEME_ID;
    try {
        const stored = window.localStorage.getItem(ASSISTANT_LIGHT_SCHEME_STORAGE_KEY);
        return isAssistantLightSchemeId(stored) ? stored : DEFAULT_ASSISTANT_LIGHT_SCHEME_ID;
    } catch {
        return DEFAULT_ASSISTANT_LIGHT_SCHEME_ID;
    }
}

export function writeStoredAssistantLightSchemeId(schemeId: AssistantLightSchemeId): void {
    if (typeof window === "undefined") return;
    try {
        window.localStorage.setItem(ASSISTANT_LIGHT_SCHEME_STORAGE_KEY, schemeId);
    } catch {
        // Ignore storage failures in restricted webviews.
    }
}
