import type { Theme } from "./aiAssistantPanelTheme";

export type AssistantDarkSchemeId = "graphite" | "classic" | "aurora" | "ember" | "violet";

export type AssistantDarkScheme = {
    id: AssistantDarkSchemeId;
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

export const ASSISTANT_DARK_SCHEME_STORAGE_KEY = "maclaw.ai.darkScheme";

export const graphiteDarkScheme: AssistantDarkScheme = {
    id: "graphite",
    storageValue: "graphite",
    label: {
        en: "Graphite Workbench",
        zhHans: "\u9ed1\u7070\u5de5\u4f5c\u53f0",
        zhHant: "\u9ed1\u7070\u5de5\u4f5c\u53f0",
    },
    description: {
        en: "Navy-tinted editor grays with restrained status color.",
        zhHans: "\u85cf\u9752\u7070\u7f16\u8f91\u5668\u914d\u8272\uff0c\u4f4e\u9971\u548c\u72b6\u6001\u8272\u3002",
        zhHant: "\u85cf\u9752\u7070\u7de8\u8f2f\u5668\u914d\u8272\uff0c\u4f4e\u98fd\u548c\u72c0\u614b\u8272\u3002",
    },
    cssVars: {
        pageBg: "#0f141b",
        surface: "#161c26",
        surfaceMuted: "#1b2230",
        primary: "#7ea8e0",
        primaryStrong: "#a5c4ee",
        primarySoft: "rgba(126, 168, 224, 0.14)",
        textPrimary: "#e3e9f1",
        textSecondary: "#b4c0cf",
        textMuted: "#7f8ea1",
        border: "#2b3444",
        borderSubtle: "#232b39",
        success: "#3fb950",
        successBg: "rgba(63, 185, 80, 0.13)",
        warning: "#d0b36a",
        warningBg: "rgba(208, 179, 106, 0.12)",
        danger: "#f47067",
        dangerBg: "rgba(244, 112, 103, 0.12)",
        linkColor: "#7ea8e0",
        infoBg: "rgba(126, 168, 224, 0.10)",
    },
    assistantTheme: {
        bg: "#0f141b",
        titleBarBg: "#161c26",
        titleBarBorder: "#2b3444",
        titleText: "#e3e9f1",
        text: "#e3e9f1",
        textMuted: "#7f8ea1",
        inputBarBg: "#181f2a",
        inputBarBorder: "#2b3444",
        inputText: "#e3e9f1",
        codeBg: "#1b2230",
        codeText: "#b4c0cf",
        codeBlockBg: "#121822",
        codeBlockBorder: "#2b3444",
        codeBlockLang: "#7f8ea1",
        borderLeft: "#2b3444",
        responseBorderLeft: "#5f7ea8",
        headingColor: "#e9eff7",
        linkColor: "#7ea8e0",
        pathColor: "#7ea8e0",
        promptColor: "#b4c0cf",
        userColor: "#e3e9f1",
        divider: "#232b39",
        fieldBg: "#1b2230",
        fieldBorder: "#3b4760",
        fieldLabel: "#bfccdb",
        errorText: "#f47067",
        errorBg: "rgba(244, 112, 103, 0.10)",
        errorBorder: "#b85c56",
        emptyHint: "#5f6f84",
        boldColor: "#f4f8fc",
        italicColor: "#d3dce8",
        bulletColor: "#7f8ea1",
        quoteBorder: "#3d4c64",
        quoteText: "#b4c0cf",
        actionBtnColor: "#8ea0b5",
        closeBtnColor: "#8ea0b5",
        btnColor: "#7ea8e0",
        btnBorder: "#4a5b78",
        sendBtnColor: "#0f141b",
        sendBtnBorder: "#7ea8e0",
        sendBtnBg: "#7ea8e0",
    },
};

export const classicDarkScheme: AssistantDarkScheme = {
    id: "classic",
    storageValue: "classic",
    label: {
        en: "Classic Slate",
        zhHans: "\u7ecf\u5178\u84dd\u7070",
        zhHant: "\u7d93\u5178\u85cd\u7070",
    },
    description: {
        en: "The existing calm blue-gray dark theme.",
        zhHans: "\u4fdd\u7559\u539f\u6709\u7684\u6c89\u7a33\u84dd\u7070\u6697\u8272\u65b9\u6848\u3002",
        zhHant: "\u4fdd\u7559\u539f\u6709\u7684\u6c89\u7a69\u85cd\u7070\u6697\u8272\u65b9\u6848\u3002",
    },
    cssVars: {
        pageBg: "#0b1220",
        surface: "#111827",
        surfaceMuted: "#0f172a",
        primary: "#8fb4dc",
        primaryStrong: "#5f89b8",
        primarySoft: "rgba(96, 140, 184, 0.18)",
        textPrimary: "#e5e7eb",
        textSecondary: "#cbd5e1",
        textMuted: "#a8b8c8",
        border: "#334155",
        borderSubtle: "#1e293b",
        success: "#7aa89a",
        successBg: "rgba(122, 168, 154, 0.12)",
        warning: "#c7d7e8",
        warningBg: "rgba(148, 163, 184, 0.12)",
        danger: "#e07a72",
        dangerBg: "rgba(196, 61, 52, 0.12)",
        linkColor: "#9bc2ea",
        infoBg: "rgba(96, 140, 184, 0.14)",
    },
    assistantTheme: {
        bg: "#0b1220",
        titleBarBg: "#111827",
        titleBarBorder: "#334155",
        titleText: "#f1f5f9",
        text: "#e2e8f0",
        textMuted: "#a8b8c8",
        inputBarBg: "#0f172a",
        inputBarBorder: "#334155",
        inputText: "#e5e7eb",
        codeBg: "#1e293b",
        codeText: "#b7d3ef",
        codeBlockBg: "#0f172a",
        codeBlockBorder: "#1e3a5f",
        codeBlockLang: "#8fb4dc",
        borderLeft: "#334155",
        responseBorderLeft: "#5b7898",
        headingColor: "#d9e7f5",
        linkColor: "#9bc2ea",
        pathColor: "#b7d3ef",
        promptColor: "#b7d3ef",
        userColor: "#c7d7e8",
        divider: "#1e293b",
        fieldBg: "#111827",
        fieldBorder: "#475569",
        fieldLabel: "#cbd5e1",
        errorText: "#e07a72",
        errorBg: "rgba(196, 61, 52, 0.10)",
        errorBorder: "#b95b52",
        emptyHint: "#7a8a9b",
        boldColor: "#f8fafc",
        italicColor: "#e2e8f0",
        bulletColor: "#8a9ab0",
        quoteBorder: "#5b7898",
        quoteText: "#c7d7e8",
        actionBtnColor: "#cbd5e1",
        closeBtnColor: "#cbd5e1",
        btnColor: "#b7d3ef",
        btnBorder: "#5b7898",
        sendBtnColor: "#ffffff",
        sendBtnBorder: "#5b7898",
        sendBtnBg: "#2f5f98",
    },
};

export const auroraDarkScheme: AssistantDarkScheme = {
    id: "aurora",
    storageValue: "aurora",
    label: {
        en: "Aurora Console",
        zhHans: "\u6781\u5149\u63a7\u5236\u53f0",
        zhHant: "\u6975\u5149\u63a7\u5236\u53f0",
    },
    description: {
        en: "Deep ink surfaces with cyan-blue controls and soft green status color.",
        zhHans: "\u6df1\u58a8\u84dd\u5c42\u7ea7\u3001\u9752\u84dd\u63a7\u4ef6\u4e0e\u67d4\u548c\u7eff\u8272\u72b6\u6001\u8272\u3002",
        zhHant: "\u6df1\u58a8\u85cd\u5c64\u7d1a\u3001\u9752\u85cd\u63a7\u4ef6\u8207\u67d4\u548c\u7da0\u8272\u72c0\u614b\u8272\u3002",
    },
    cssVars: {
        pageBg: "#071018",
        surface: "#0d1721",
        surfaceMuted: "#111f2b",
        primary: "#7dd3fc",
        primaryStrong: "#38bdf8",
        primarySoft: "rgba(56, 189, 248, 0.16)",
        textPrimary: "#e6f1f8",
        textSecondary: "#b8cad6",
        textMuted: "#8297a6",
        border: "#223646",
        borderSubtle: "#182936",
        success: "#86d9b5",
        successBg: "rgba(134, 217, 181, 0.13)",
        warning: "#d5c786",
        warningBg: "rgba(213, 199, 134, 0.12)",
        danger: "#f08a84",
        dangerBg: "rgba(240, 138, 132, 0.12)",
        linkColor: "#93ddff",
        infoBg: "rgba(56, 189, 248, 0.13)",
    },
    assistantTheme: {
        bg: "#071018",
        titleBarBg: "#0b1520",
        titleBarBorder: "#223646",
        titleText: "#e6f1f8",
        text: "#dcebf3",
        textMuted: "#8297a6",
        inputBarBg: "#0d1721",
        inputBarBorder: "#263c4c",
        inputText: "#e6f1f8",
        codeBg: "#102434",
        codeText: "#a8e5ff",
        codeBlockBg: "#09141d",
        codeBlockBorder: "#1e4960",
        codeBlockLang: "#7dd3fc",
        borderLeft: "#223646",
        responseBorderLeft: "#38bdf8",
        headingColor: "#f0f8fc",
        linkColor: "#93ddff",
        pathColor: "#a8e5ff",
        promptColor: "#a8e5ff",
        userColor: "#d2e5ef",
        divider: "#182936",
        fieldBg: "#0d1721",
        fieldBorder: "#3a5568",
        fieldLabel: "#c5d6e2",
        errorText: "#f08a84",
        errorBg: "rgba(240, 138, 132, 0.10)",
        errorBorder: "#c96f69",
        emptyHint: "#7d95a5",
        boldColor: "#f4fbff",
        italicColor: "#dcebf3",
        bulletColor: "#86d9b5",
        quoteBorder: "#38bdf8",
        quoteText: "#c5dbe6",
        actionBtnColor: "#b8cad6",
        closeBtnColor: "#b8cad6",
        btnColor: "#a8e5ff",
        btnBorder: "#3d7590",
        sendBtnColor: "#041018",
        sendBtnBorder: "#7dd3fc",
        sendBtnBg: "#7dd3fc",
    },
};

export const emberDarkScheme: AssistantDarkScheme = {
    id: "ember",
    storageValue: "ember",
    label: {
        en: "Ember Forge",
        zhHans: "\u7194\u5ca9\u953b\u9020",
        zhHant: "\u7194\u5ca9\u935b\u9020",
    },
    description: {
        en: "Warm dark surfaces with steel-blue accents.",
        zhHans: "\u6e29\u6696\u6df1\u68d5\u5e95\u8272\u642d\u914d\u94a2\u84dd\u5f3a\u8c03\u3002",
        zhHant: "\u6eab\u6696\u6df1\u68d5\u5e95\u8272\u642d\u914d\u92fc\u85cd\u5f37\u8abf\u3002",
    },
    cssVars: {
        pageBg: "#140e0a",
        surface: "#1e1612",
        surfaceMuted: "#251c16",
        primary: "#8fb0d4",
        primaryStrong: "#a9c2e2",
        primarySoft: "rgba(143, 176, 212, 0.14)",
        textPrimary: "#f2e6d8",
        textSecondary: "#cbb8a4",
        textMuted: "#9a8570",
        border: "#3a2e24",
        borderSubtle: "#2d2118",
        success: "#6db88a",
        successBg: "rgba(109, 184, 138, 0.13)",
        warning: "#e0c06c",
        warningBg: "rgba(224, 192, 108, 0.12)",
        danger: "#e06b5e",
        dangerBg: "rgba(224, 107, 94, 0.12)",
        linkColor: "#8fb0d4",
        infoBg: "rgba(143, 176, 212, 0.10)",
    },
    assistantTheme: {
        bg: "#140e0a",
        titleBarBg: "#1a1310",
        titleBarBorder: "#3a2e24",
        titleText: "#f5ede4",
        text: "#ede2d4",
        textMuted: "#b09a82",
        inputBarBg: "#1e1612",
        inputBarBorder: "#45362a",
        inputText: "#f2e6d8",
        codeBg: "#241a12",
        codeText: "#f5d8a8",
        codeBlockBg: "#18100b",
        codeBlockBorder: "#4a3828",
        codeBlockLang: "#8fb0d4",
        borderLeft: "#3a2e24",
        responseBorderLeft: "#6f8ab0",
        headingColor: "#faf0e2",
        linkColor: "#8fb0d4",
        pathColor: "#a9c2e2",
        promptColor: "#a9c2e2",
        userColor: "#e8d4be",
        divider: "#2d2118",
        fieldBg: "#1e1612",
        fieldBorder: "#5a4636",
        fieldLabel: "#d4c0a8",
        errorText: "#e06b5e",
        errorBg: "rgba(224, 107, 94, 0.10)",
        errorBorder: "#b85548",
        emptyHint: "#9c846c",
        boldColor: "#fff8f0",
        italicColor: "#ede2d4",
        bulletColor: "#b09a82",
        quoteBorder: "#6f8ab0",
        quoteText: "#d4c0a8",
        actionBtnColor: "#cbb8a4",
        closeBtnColor: "#cbb8a4",
        btnColor: "#8fb0d4",
        btnBorder: "#516a88",
        sendBtnColor: "#140e0a",
        sendBtnBorder: "#8fb0d4",
        sendBtnBg: "#8fb0d4",
    },
};

export const violetDarkScheme: AssistantDarkScheme = {
    id: "violet",
    storageValue: "violet",
    label: {
        en: "Violet",
        zhHans: "\u7d2b\u7070",
        zhHant: "\u7d2b\u7070",
    },
    description: {
        en: "Dark purple-gray surface with steel-blue accents.",
        zhHans: "\u6df1\u7d2b\u7070\u8868\u9762\uff0c\u94a2\u84dd\u7528\u4e8e\u5f3a\u8c03\u4e0e\u94fe\u63a5\u3002",
        zhHant: "\u6df1\u7d2b\u7070\u8868\u9762\uff0c\u92fc\u85cd\u7528\u65bc\u5f37\u8abf\u8207\u9023\u7d50\u3002",
    },
    cssVars: {
        pageBg: "#0e0a18",
        surface: "#150f24",
        surfaceMuted: "#1b1330",
        primary: "#9db9e8",
        primaryStrong: "#b7cef2",
        primarySoft: "rgba(157, 185, 232, 0.14)",
        textPrimary: "#ebe4f5",
        textSecondary: "#c8b8dc",
        textMuted: "#8d7da8",
        border: "#33284a",
        borderSubtle: "#261d3a",
        success: "#7cdba8",
        successBg: "rgba(124, 219, 168, 0.13)",
        warning: "#d4c27a",
        warningBg: "rgba(212, 194, 122, 0.12)",
        danger: "#ef8078",
        dangerBg: "rgba(239, 128, 120, 0.12)",
        linkColor: "#9db9e8",
        infoBg: "rgba(157, 185, 232, 0.10)",
    },
    assistantTheme: {
        bg: "#0e0a18",
        titleBarBg: "#120e20",
        titleBarBorder: "#33284a",
        titleText: "#f0eaf8",
        text: "#e4dcf0",
        textMuted: "#a898c0",
        inputBarBg: "#150f24",
        inputBarBorder: "#3d3058",
        inputText: "#ebe4f5",
        codeBg: "#1a1230",
        codeText: "#d4b8f0",
        codeBlockBg: "#100c1e",
        codeBlockBorder: "#3d2e68",
        codeBlockLang: "#9db9e8",
        borderLeft: "#33284a",
        responseBorderLeft: "#7390bd",
        headingColor: "#f6f0ff",
        linkColor: "#9db9e8",
        pathColor: "#b7cef2",
        promptColor: "#b7cef2",
        userColor: "#d8cce8",
        divider: "#261d3a",
        fieldBg: "#150f24",
        fieldBorder: "#4d3d6c",
        fieldLabel: "#d0c0e8",
        errorText: "#ef8078",
        errorBg: "rgba(239, 128, 120, 0.10)",
        errorBorder: "#c46058",
        emptyHint: "#8e7ea8",
        boldColor: "#faf5ff",
        italicColor: "#e4dcf0",
        bulletColor: "#a898c0",
        quoteBorder: "#7390bd",
        quoteText: "#c8b8dc",
        actionBtnColor: "#c8b8dc",
        closeBtnColor: "#c8b8dc",
        btnColor: "#9db9e8",
        btnBorder: "#5a7292",
        sendBtnColor: "#0e0a18",
        sendBtnBorder: "#9db9e8",
        sendBtnBg: "#9db9e8",
    },
};

export const assistantDarkSchemes = [graphiteDarkScheme, classicDarkScheme, auroraDarkScheme, emberDarkScheme, violetDarkScheme] as const;

export function isAssistantDarkSchemeId(value: unknown): value is AssistantDarkSchemeId {
    return value === "graphite" || value === "classic" || value === "aurora" || value === "ember" || value === "violet";
}

export function getAssistantDarkScheme(id: unknown): AssistantDarkScheme {
    return assistantDarkSchemes.find((scheme) => scheme.id === id) || graphiteDarkScheme;
}

export function readStoredAssistantDarkSchemeId(): AssistantDarkSchemeId {
    if (typeof window === "undefined") return "graphite";
    try {
        const stored = window.localStorage.getItem(ASSISTANT_DARK_SCHEME_STORAGE_KEY);
        return isAssistantDarkSchemeId(stored) ? stored : "graphite";
    } catch {
        return "graphite";
    }
}

export function writeStoredAssistantDarkSchemeId(schemeId: AssistantDarkSchemeId): void {
    if (typeof window === "undefined") return;
    try {
        window.localStorage.setItem(ASSISTANT_DARK_SCHEME_STORAGE_KEY, schemeId);
    } catch {
        // Ignore storage failures in restricted webviews.
    }
}
