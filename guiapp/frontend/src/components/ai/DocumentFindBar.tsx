import { useEffect, useRef } from "react";
import { tailText } from "../../i18n/mnTail";

function findFieldOpen(input: HTMLElement): boolean {
    let node: HTMLElement | null = input;
    const view = input.ownerDocument.defaultView;
    while (node) {
        if (node.hidden) return false;
        const style = view?.getComputedStyle(node);
        if (style && (style.display === "none" || style.visibility === "hidden")) return false;
        node = node.parentElement;
    }
    return true;
}

export function documentFindCountLabel(lang: string, query: string, count: number, active: number, busy: boolean): string {
    if (!query.trim()) return "";
    if (busy && count <= 0) return tailText(lang, "Searching…", "搜索中…");
    if (count <= 0) return tailText(lang, "No matches", "无匹配");
    const shown = active >= 0 && active < count ? active + 1 : 1;
    // A scan still walking later pages has not reported the full total yet.
    return busy ? `${shown} / ${count}…` : `${shown} / ${count}`;
}

export function DocumentFindBar({
    lang,
    query,
    onQueryChange,
    count,
    active,
    busy = false,
    onPrev,
    onNext,
}: {
    lang: string;
    query: string;
    onQueryChange: (value: string) => void;
    count: number;
    active: number;
    busy?: boolean;
    onPrev: () => void;
    onNext: () => void;
}) {
    const inputRef = useRef<HTMLInputElement>(null);
    useEffect(() => {
        const onKey = (event: KeyboardEvent) => {
            if (event.altKey || event.shiftKey || event.repeat) return;
            if (!(event.ctrlKey || event.metaKey)) return;
            if (event.key.toLowerCase() !== "f") return;
            const input = inputRef.current;
            if (!input || !findFieldOpen(input)) return;
            event.preventDefault();
            event.stopPropagation();
            input.focus();
            input.select();
        };
        window.addEventListener("keydown", onKey, true);
        return () => window.removeEventListener("keydown", onKey, true);
    }, []);

    const label = documentFindCountLabel(lang, query, count, active, busy);
    const idle = count <= 0;
    return (
        <div className="companion-doc-find" data-testid="companion-doc-find" role="search">
            <input
                ref={inputRef}
                className="companion-doc-find-input"
                data-testid="companion-doc-find-input"
                type="text"
                value={query}
                autoComplete="off"
                spellCheck={false}
                placeholder={tailText(lang, "Find", "搜索")}
                aria-label={tailText(lang, "Find in document", "在文档中搜索")}
                title={tailText(lang, "Find in document (Ctrl+F)", "在文档中搜索（Ctrl+F）")}
                onChange={(event) => onQueryChange(event.target.value)}
                onKeyDown={(event) => {
                    if (event.nativeEvent.isComposing) return;
                    if (event.key === "Enter") {
                        event.preventDefault();
                        event.stopPropagation();
                        if (event.shiftKey) onPrev();
                        else onNext();
                        return;
                    }
                    if (event.key === "Escape") {
                        event.preventDefault();
                        event.stopPropagation();
                        onQueryChange("");
                    }
                }}
            />
            <span className="companion-doc-find-count" data-testid="companion-doc-find-count" aria-live="polite">
                {label}
            </span>
            <button
                type="button"
                data-testid="companion-doc-find-prev"
                aria-label={tailText(lang, "Previous match", "上一个")}
                title={tailText(lang, "Previous match", "上一个")}
                disabled={idle}
                onClick={onPrev}
            >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="m6 14 6-6 6 6" />
                </svg>
            </button>
            <button
                type="button"
                data-testid="companion-doc-find-next"
                aria-label={tailText(lang, "Next match", "下一个")}
                title={tailText(lang, "Next match", "下一个")}
                disabled={idle}
                onClick={onNext}
            >
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.9" strokeLinecap="round" strokeLinejoin="round" aria-hidden="true">
                    <path d="m6 10 6 6 6-6" />
                </svg>
            </button>
        </div>
    );
}
