import { useEffect, useState } from 'react';

export const SCOPE_TABS = ['all', 'universal', 'go', 'python', 'typescript', 'cpp', 'rust', 'java', 'project'] as const;
export type ScopeTab = typeof SCOPE_TABS[number];
export const TAB_LABELS: Record<ScopeTab, [string, string, string]> = {
    all: ['All', '\u5168\u90e8', '\u5168\u90e8'],
    universal: ['General', '\u901a\u7528', '\u901a\u7528'],
    go: ['Go', 'Go', 'Go'],
    python: ['Python', 'Python', 'Python'],
    typescript: ['TypeScript', 'TS', 'TS'],
    cpp: ['C++', 'C++', 'C++'],
    rust: ['Rust', 'Rust', 'Rust'],
    java: ['Java', 'Java', 'Java'],
    project: ['Project', '\u9879\u76ee', '\u5c08\u6848'],
};

export function useDebouncedValue(value: string, delayMs: number): string {
    const [debounced, setDebounced] = useState(value);
    useEffect(() => {
        const timer = setTimeout(() => setDebounced(value), delayMs);
        return () => clearTimeout(timer);
    }, [value, delayMs]);
    return debounced;
}

export type ExperienceDraft = {
    id: string;
    title: string;
    category: string;
    scope: string;
    language: string;
    trigger_condition: string;
    content: string;
    code_snippet: string;
    status: string;
    confidence: number;
    recall_count: number;
    success_count: number;
    failure_count: number;
};

export function toDraft(raw: any): ExperienceDraft {
    return {
        id: String(raw?.id || ''),
        title: String(raw?.title || ''),
        category: String(raw?.category || 'pattern'),
        scope: String(raw?.scope || 'universal'),
        language: String(raw?.language || ''),
        trigger_condition: String(raw?.trigger_condition || ''),
        content: String(raw?.content || ''),
        code_snippet: String(raw?.code_snippet || ''),
        status: String(raw?.status || 'candidate'),
        confidence: Number(raw?.confidence || 0),
        recall_count: Number(raw?.recall_count || 0),
        success_count: Number(raw?.success_count || 0),
        failure_count: Number(raw?.failure_count || 0),
    };
}
