import type { Theme } from "./aiAssistantPanelTheme";

export type AssistantDarkSchemeId = "graphite" | "classic" | "aurora";

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
        en: "Neutral editor-like blacks and grays with restrained status color.",
        zhHans: "\u63a5\u8fd1\u7f16\u8f91\u5668\u7684\u4e2d\u6027\u9ed1\u7070\uff0c\u4f4e\u9971\u548c\u72b6\u6001\u8272\u3002",
        zhHant: "\u63a5\u8fd1\u7de8\u8f2f\u5668\u7684\u4e2d\u6027\u9ed1\u7070\uff0c\u4f4e\u98fd\u548c\u72c0\u614b\u8272\u3002",
    },
    cssVars: {
        pageBg: "#111111",
        surface: "#1b1b1b",
        surfaceMuted: "#242424",
        primary: "#c9d1d9",
        primaryStrong: "#f0f0f0",
        primarySoft: "rgba(201, 209, 217, 0.14)",
        textPrimary: "#e6e6e6",
        textSecondary: "#c4c4c4",
        textMuted: "#8f8f8f",
        border: "#343434",
        borderSubtle: "#2a2a2a",
        success: "#3fb950",
        successBg: "rgba(63, 185, 80, 0.13)",
        warning: "#d0b36a",
        warningBg: "rgba(208, 179, 106, 0.12)",
        danger: "#f47067",
        dangerBg: "rgba(244, 112, 103, 0.12)",
        linkColor: "#aeb6bf",
        infoBg: "rgba(201, 209, 217, 0.10)",
    },
    assistantTheme: {
        bg: "#111111",
        titleBarBg: "#181818",
        titleBarBorder: "#343434",
        titleText: "#f0f0f0",
        text: "#e0e0e0",
        textMuted: "#8f8f8f",
        inputBarBg: "#1f1f1f",
        inputBarBorder: "#3a3a3a",
        inputText: "#ededed",
        codeBg: "#252526",
        codeText: "#d4d4d4",
        codeBlockBg: "#151515",
        codeBlockBorder: "#3a3a3a",
        codeBlockLang: "#b7b7b7",
        borderLeft: "#343434",
        responseBorderLeft: "#6e7681",
        headingColor: "#f5f5f5",
        linkColor: "#aeb6bf",
        pathColor: "#c9d1d9",
        promptColor: "#d4d4d4",
        userColor: "#e0e0e0",
        divider: "#2a2a2a",
        fieldBg: "#1e1e1e",
        fieldBorder: "#3a3a3a",
        fieldLabel: "#a0a0a0",
        errorText: "#f47067",
        errorBg: "rgba(244, 112, 103, 0.10)",
        errorBorder: "#b85c56",
        emptyHint: "#777777",
        boldColor: "#ffffff",
        italicColor: "#d4d4d4",
        bulletColor: "#8f8f8f",
        quoteBorder: "#5f656c",
        quoteText: "#c4c4c4",
        actionBtnColor: "#b7b7b7",
        closeBtnColor: "#b7b7b7",
        btnColor: "#d4d4d4",
        btnBorder: "#5a5a5a",
        sendBtnColor: "#111111",
        sendBtnBorder: "#d4d4d4",
        sendBtnBg: "#d4d4d4",
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
        textMuted: "#94a3b8",
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
        textMuted: "#94a3b8",
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
        fieldBorder: "#334155",
        fieldLabel: "#94a3b8",
        errorText: "#e07a72",
        errorBg: "rgba(196, 61, 52, 0.10)",
        errorBorder: "#b95b52",
        emptyHint: "#64748b",
        boldColor: "#f8fafc",
        italicColor: "#e2e8f0",
        bulletColor: "#64748b",
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
        textMuted: "#8da2b1",
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
        fieldBorder: "#263c4c",
        fieldLabel: "#9fb2bf",
        errorText: "#f08a84",
        errorBg: "rgba(240, 138, 132, 0.10)",
        errorBorder: "#c96f69",
        emptyHint: "#6f8595",
        boldColor: "#f4fbff",
        italicColor: "#dcebf3",
        bulletColor: "#86d9b5",
        quoteBorder: "#38bdf8",
        quoteText: "#c5dbe6",
        actionBtnColor: "#b8cad6",
        closeBtnColor: "#b8cad6",
        btnColor: "#a8e5ff",
        btnBorder: "#2e5a70",
        sendBtnColor: "#041018",
        sendBtnBorder: "#7dd3fc",
        sendBtnBg: "#7dd3fc",
    },
};

export const assistantDarkSchemes = [graphiteDarkScheme, classicDarkScheme, auroraDarkScheme] as const;

export function isAssistantDarkSchemeId(value: unknown): value is AssistantDarkSchemeId {
    return value === "graphite" || value === "classic" || value === "aurora";
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
