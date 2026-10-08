import React from "react";

export const WORKBUDDY_CHINA_PROVIDER = "WorkBuddy 国内版";
export const WORKBUDDY_GLOBAL_PROVIDER = "WorkBuddy 国际版";
export const KIMI_CODE_PROVIDER = "Kimi Code";
export const QODER_CN_PROVIDER = "Qoder 国内版";
export const QODER_GLOBAL_PROVIDER = "Qoder 国际版";
export const TRAE_CN_PROVIDER = "Trae 国内版";
export const TRAE_GLOBAL_PROVIDER = "Trae 国际版";
export const LOBSTERAI_PROVIDER = "LobsterAI";

/** GUI and TUI display names for the Zhipu coding preset (one provider). */
const ZHIPU_CODING_PROVIDER_NAMES = ["智谱编程", "智谱 GLM (Coding)", "Zhipu GLM Coding"];

export function isWorkBuddyProvider(name: string | null | undefined) {
    return name === WORKBUDDY_CHINA_PROVIDER || name === WORKBUDDY_GLOBAL_PROVIDER;
}

export function isKimiCodeProvider(name: string | null | undefined) {
    return name === KIMI_CODE_PROVIDER;
}

export function isQoderProvider(name: string | null | undefined) {
    return name === QODER_CN_PROVIDER || name === QODER_GLOBAL_PROVIDER;
}

export function isTraeProvider(name: string | null | undefined) {
    return name === TRAE_CN_PROVIDER || name === TRAE_GLOBAL_PROVIDER;
}

export function isLobsterAIProvider(name: string | null | undefined) {
    return name === LOBSTERAI_PROVIDER;
}

/** Mirrors corelib.IsZhipuCodingProviderName: GUI preset, TUI CN and EN aliases. */
export function isZhipuCodingProvider(name: string | null | undefined) {
    const trimmed = (name || "").trim();
    return ZHIPU_CODING_PROVIDER_NAMES.some(candidate => candidate.toLowerCase() === trimmed.toLowerCase());
}

function KimiCodeLogo() {
    return (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M14.2 3.1a8.4 8.4 0 1 0 6.6 13.4A6.7 6.7 0 0 1 14.2 3.1Z" fill="currentColor"/>
        </svg>
    );
}

function WorkBuddyLogo() {
    return (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M3.2 7.2 8.1 17.2h2.1L12 9.4l1.8 7.8h2.1l4.9-10" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"/>
            <circle cx="18.6" cy="5.4" r="1.35" fill="currentColor"/>
        </svg>
    );
}

function QoderLogo() {
    return (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M12 2.8a9.2 9.2 0 1 0 5.9 16.3l2.6 2.6" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round"/>
            <path d="m15.6 15.6 4.8-4.8" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round"/>
        </svg>
    );
}

/**
 * Official brand assets, embedded as data URIs.
 * Trae: the dark-green site favicon from trae.cn (the green frame + twin dots
 * mark). LobsterAI: the desktop app icon from the official open-source repo.
 * Both are baked-in-color bitmap logos, so they keep their identity on light
 * and dark surfaces where the currentColor line-art logos would not.
 */
const TRAE_ICON_DATA = "data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAADAAAAAwCAYAAABXAvmHAAAACXBIWXMAAAsTAAALEwEAmpwYAAAAAXNSR0IArs4c6QAAAARnQU1BAACxjwv8YQUAAAF9SURBVHgB7Zm9SsNQFMf/EYWiVB2KKGLBScHFN9BJurmoa0dn9QGqD1CdHbtWFzd10jfQQdBJqIgKGYw4VCR+nGD0ptqmsSectD0/CKXpbXJ+3HvOyYfVP5B+RxvTgzZHBaRRAWlUQBoVkEYFpOkNGzC0MgtJnPJZw99DBcZ2FiFJmIDmgDSdn8S1PB9d4aFwiDgY2cwhnZuK9J/IAq5TxevNI+Lg7amKqGgO/DrgYIplTNPnAyN9E8OYPF5FZmOu7hgKPrufZ+svkXOgHhR8di/vfWbW5719dvE0MMYPPjUz6m3E3doBWoFlBszgfUjCnAkzeB+6TGl1JuJNYuOJk/W1ccMiQGW1slwKlFe7eAJ7+2cJuZ8lsrJUwsvF/fc+us5JxBIiTIna4H1MCY7gCbYkJij464Xdhg3Jl3D/0bT+gj0HmummXMET2omlCc2By/GtwHeq3dO3BSQFXULSdN8dGdV6p3yOpGDpOzJhVEAaFZBGBaRRAWlUQJoPEeV5RYlN/QYAAAAASUVORK5CYII=";
const LOBSTERAI_ICON_DATA = "data:image/png;base64,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";

function TraeLogo() {
    return (
        <img
            src={TRAE_ICON_DATA}
            width={14}
            height={14}
            alt=""
            aria-hidden="true"
            draggable={false}
            style={{ borderRadius: 3, flexShrink: 0 }}
        />
    );
}

function LobsterAILogo() {
    return (
        <img
            src={LOBSTERAI_ICON_DATA}
            width={14}
            height={14}
            alt=""
            aria-hidden="true"
            draggable={false}
            style={{ borderRadius: 3, flexShrink: 0 }}
        />
    );
}



/**
 * Zhipu (智谱) brand mark — the "Z" badge used across bigmodel.cn. Drawn as a
 * filled rounded square with the Z knocked out so it stays legible on both
 * light and dark surfaces at chip size.
 */
export function ZhipuLogo() {
    return (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path
                fillRule="evenodd"
                clipRule="evenodd"
                d="M6.2 2.5h11.6a3.7 3.7 0 0 1 3.7 3.7v11.6a3.7 3.7 0 0 1-3.7 3.7H6.2a3.7 3.7 0 0 1-3.7-3.7V6.2a3.7 3.7 0 0 1 3.7-3.7Zm1.5 5.2v1.8l5.4 0-5.4 5.4v1.8h9v-1.8h-5.4l5.4-5.4v-1.8h-9Z"
                fill="currentColor"
            />
        </svg>
    );
}

/** Inline SVG logos for known LLM providers, keyed by provider name. */
export const PROVIDER_LOGOS: Record<string, React.ReactNode> = {
    OpenAI: (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" fill="currentColor" viewBox="0 0 16 16">
            <path d="M14.949 6.547a3.94 3.94 0 0 0-.348-3.273 4.11 4.11 0 0 0-4.4-1.934A4.1 4.1 0 0 0 8.423.2 4.15 4.15 0 0 0 6.305.086a4.1 4.1 0 0 0-1.891.948 4.04 4.04 0 0 0-1.158 1.753 4.1 4.1 0 0 0-1.563.679A4 4 0 0 0 .554 4.72a3.99 3.99 0 0 0 .502 4.731 3.94 3.94 0 0 0 .346 3.274 4.11 4.11 0 0 0 4.402 1.933c.382.425.852.764 1.377.995.526.231 1.095.35 1.67.346 1.78.002 3.358-1.132 3.901-2.804a4.1 4.1 0 0 0 1.563-.68 4 4 0 0 0 1.14-1.253 3.99 3.99 0 0 0-.506-4.716m-6.097 8.406a3.05 3.05 0 0 1-1.945-.694l.096-.054 3.23-1.838a.53.53 0 0 0 .265-.455v-4.49l1.366.778q.02.011.025.035v3.722c-.003 1.653-1.361 2.992-3.037 2.996m-6.53-2.75a2.95 2.95 0 0 1-.36-2.01l.095.057L5.29 12.09a.53.53 0 0 0 .527 0l3.949-2.246v1.555a.05.05 0 0 1-.022.041L6.473 13.3c-1.454.826-3.311.335-4.15-1.098m-.85-6.94A3.02 3.02 0 0 1 3.07 3.949v3.785a.51.51 0 0 0 .262.451l3.93 2.237-1.366.779a.05.05 0 0 1-.048 0L2.585 9.342a2.98 2.98 0 0 1-1.113-4.094zm11.216 2.571L8.747 5.576l1.362-.776a.05.05 0 0 1 .048 0l3.265 1.86a3 3 0 0 1 1.173 1.207 2.96 2.96 0 0 1-.27 3.2 3.05 3.05 0 0 1-1.36.997V8.279a.52.52 0 0 0-.276-.445m1.36-2.015-.097-.057-3.226-1.855a.53.53 0 0 0-.53 0L6.249 6.153V4.598a.04.04 0 0 1 .019-.04L9.533 2.7a3.07 3.07 0 0 1 3.257.139c.474.325.843.778 1.066 1.303.223.526.289 1.103.191 1.664zM5.503 8.575 4.139 7.8a.05.05 0 0 1-.026-.037V4.049c0-.57.166-1.127.476-1.607s.752-.864 1.275-1.105a3.08 3.08 0 0 1 3.234.41l-.096.054-3.23 1.838a.53.53 0 0 0-.265.455zm.742-1.577 1.758-1 1.762 1v2l-1.755 1-1.762-1z"/>
        </svg>
    ),
    "DeepSeek": (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none">
            <path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm0 3c1.66 0 3 1.34 3 3s-1.34 3-3 3-3-1.34-3-3 1.34-3 3-3zm0 14.2c-2.5 0-4.71-1.28-6-3.22.03-1.99 4-3.08 6-3.08 1.99 0 5.97 1.09 6 3.08-1.29 1.94-3.5 3.22-6 3.22z" fill="currentColor"/>
        </svg>
    ),
    Qwen: (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M12 3.2a7.3 7.3 0 1 0 5.6 12l3.2 3.2" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round"/>
            <path d="M12 7.5a3 3 0 1 0 0 6 3 3 0 0 0 0-6Z" fill="currentColor"/>
            <path d="M17.6 15.2 21 18.6" stroke="currentColor" strokeWidth="2" strokeLinecap="round"/>
        </svg>
    ),
    "智谱编程": <ZhipuLogo />,
    Codex: (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" fill="currentColor" viewBox="0 0 16 16" aria-hidden="true">
            <path d="M14.949 6.547a3.94 3.94 0 0 0-.348-3.273 4.11 4.11 0 0 0-4.4-1.934A4.1 4.1 0 0 0 8.423.2 4.15 4.15 0 0 0 6.305.086a4.1 4.1 0 0 0-1.891.948 4.04 4.04 0 0 0-1.158 1.753 4.1 4.1 0 0 0-1.563.679A4 4 0 0 0 .554 4.72a3.99 3.99 0 0 0 .502 4.731 3.94 3.94 0 0 0 .346 3.274 4.11 4.11 0 0 0 4.402 1.933c.382.425.852.764 1.377.995.526.231 1.095.35 1.67.346 1.78.002 3.358-1.132 3.901-2.804a4.1 4.1 0 0 0 1.563-.68 4 4 0 0 0 1.14-1.253 3.99 3.99 0 0 0-.506-4.716"/>
        </svg>
    ),
    "Claude Code": (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M4 20 10.1 4h3.8L20 20h-3.4l-1.4-3.8H8.8L7.4 20H4Zm5.8-6.7h4.4L12 7.1l-2.2 6.2Z" fill="currentColor"/>
        </svg>
    ),
    OpenCode: (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M8 7 3 12l5 5M16 7l5 5-5 5M13 5l-2 14" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round"/>
        </svg>
    ),
    Anthropic: (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M4 20 10.1 4h3.8L20 20h-3.4l-1.4-3.8H8.8L7.4 20H4Zm5.8-6.7h4.4L12 7.1l-2.2 6.2Z" fill="currentColor"/>
        </svg>
    ),
    "GitHub Copilot": (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M12 2.7a9.3 9.3 0 0 0-2.94 18.12c.47.09.64-.2.64-.45v-1.63c-2.61.57-3.16-1.1-3.16-1.1-.42-1.08-1.05-1.37-1.05-1.37-.86-.58.07-.57.07-.57.95.07 1.45.98 1.45.98.85 1.45 2.22 1.03 2.76.79.09-.61.33-1.03.6-1.27-2.08-.24-4.27-1.04-4.27-4.64 0-1.03.37-1.87.97-2.53-.1-.24-.42-1.2.09-2.5 0 0 .79-.25 2.56.97A8.9 8.9 0 0 1 12 7.42c.78 0 1.56.1 2.29.31 1.77-1.22 2.55-.97 2.55-.97.51 1.3.19 2.26.1 2.5.6.66.96 1.5.96 2.53 0 3.61-2.2 4.4-4.28 4.63.34.29.64.84.64 1.69v2.5c0 .25.17.55.65.45A9.3 9.3 0 0 0 12 2.7Z" fill="currentColor"/>
        </svg>
    ),
    "xAI-Grok": (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M5 4h3.3l2.9 4.5L14 4h3.2l-4.3 6.5L18.7 20h-3.3l-4-6.2L7.2 20H4l5.7-8.7L5 4Z" fill="currentColor"/>
        </svg>
    ),
    "\u706b\u5c71\u5f15\u64ce Agent Plan": (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none" aria-hidden="true">
            <path d="M4 6.2 11.9 2 20 6.2v3.4l-8.1 4.2L4 9.6V6.2Z" fill="currentColor"/>
            <path d="M4 11.5 11.9 15.7 20 11.5v3.3L11.9 19 4 14.8v-3.3Z" fill="currentColor" opacity=".72"/>
            <path d="M4 16.7 11.9 20.9 20 16.7V20l-8.1 4.2L4 20v-3.3Z" fill="currentColor" opacity=".45" transform="translate(0 -2.2)"/>
        </svg>
    ),
    MiniMax: (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none">
            <path d="M2 20V4l5 8 5-8 5 8 5-8v16" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" strokeLinejoin="round" fill="none"/>
        </svg>
    ),
    Kimi: (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none">
            <circle cx="12" cy="12" r="10" stroke="currentColor" strokeWidth="2" fill="none"/>
            <circle cx="12" cy="10" r="4" fill="currentColor"/>
            <path d="M6 20c0-3.314 2.686-6 6-6s6 2.686 6 6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" fill="none"/>
        </svg>
    ),
    [KIMI_CODE_PROVIDER]: <KimiCodeLogo />,
    "讯飞星辰": (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none">
            <path d="M12 2L3 7v10l9 5 9-5V7l-9-5z" stroke="currentColor" strokeWidth="1.8" fill="none"/>
            <circle cx="12" cy="12" r="3" fill="currentColor"/>
            <path d="M12 5v3M12 16v3M5.5 8.5l2.6 1.5M15.9 14l2.6 1.5M5.5 15.5l2.6-1.5M15.9 10l2.6-1.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"/>
        </svg>
    ),
    [WORKBUDDY_CHINA_PROVIDER]: <WorkBuddyLogo />,
    [WORKBUDDY_GLOBAL_PROVIDER]: <WorkBuddyLogo />,
    [QODER_CN_PROVIDER]: <QoderLogo />,
    [QODER_GLOBAL_PROVIDER]: <QoderLogo />,
    [TRAE_CN_PROVIDER]: <TraeLogo />,
    [TRAE_GLOBAL_PROVIDER]: <TraeLogo />,
    [LOBSTERAI_PROVIDER]: <LobsterAILogo />,
};
