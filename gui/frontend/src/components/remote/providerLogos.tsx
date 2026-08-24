import React from "react";

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
    "智谱编程": (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none">
            <path d="M4 4h16v3.2H9.6L20 11.6v3.2H8v-3.2h10.4L4 7.2V4z" fill="currentColor"/>
            <path d="M4 16.8h16V20H4v-3.2z" fill="currentColor"/>
        </svg>
    ),
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
    "讯飞星辰": (
        <svg xmlns="http://www.w3.org/2000/svg" width="14" height="14" viewBox="0 0 24 24" fill="none">
            <path d="M12 2L3 7v10l9 5 9-5V7l-9-5z" stroke="currentColor" strokeWidth="1.8" fill="none"/>
            <circle cx="12" cy="12" r="3" fill="currentColor"/>
            <path d="M12 5v3M12 16v3M5.5 8.5l2.6 1.5M15.9 14l2.6 1.5M5.5 15.5l2.6-1.5M15.9 10l2.6-1.5" stroke="currentColor" strokeWidth="1.5" strokeLinecap="round"/>
        </svg>
    ),
};
