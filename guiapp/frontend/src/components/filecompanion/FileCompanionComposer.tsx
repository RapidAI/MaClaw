import { useCallback, useEffect, useId, useRef, useState, type KeyboardEvent } from "react";
import { InputHistoryAutocomplete } from "../ai/InputHistoryAutocomplete";
import { lightTheme } from "../ai/aiAssistantPanelTheme";
import { useAssistantInputHistory } from "../ai/useAssistantInputHistory";
import { useInputHistoryAutocomplete } from "../ai/useInputHistoryAutocomplete";
import { useTextCompositionGuard } from "../ai/useTextCompositionGuard";
import { isHistoryResetCommandText } from "../ai/composeAction";
import { fileCompanionInputHistory, rememberFileCompanionInput } from "./fileCompanionInputHistory";

interface FileCompanionComposerProps {
    path: string;
    value: string;
    onChange: (next: string) => void;
    // false means the turn was not accepted, so the line stays out of history.
    onSubmit: () => void | boolean | Promise<void | boolean>;
    busy?: boolean;
}

export function FileCompanionComposer({ path, value, onChange, onSubmit, busy = false }: FileCompanionComposerProps) {
    const inputRef = useRef<HTMLTextAreaElement>(null);
    const pathRef = useRef(path);
    const textComposition = useTextCompositionGuard();
    const [isComposing, setIsComposing] = useState(false);
    const [history, setHistory] = useState<string[]>(() => fileCompanionInputHistory());
    const listboxDomId = useId();
    const historyListboxId = `file-companion-input-history-${listboxDomId}`;

    const applyInputValue = useCallback((next: string) => {
        onChange(next);
        const target = path;
        requestAnimationFrame(() => {
            // A file switch restores the previous draft after this box already
            // shows the other file. Do not move the caret there.
            if (pathRef.current !== target) return;
            const el = inputRef.current;
            if (!el) return;
            el.focus();
            el.setSelectionRange(next.length, next.length);
        });
    }, [onChange, path]);

    const { exitHistoryBrowsing, isSelectionCollapsedAtBoundary, recallHistory, rememberHistoryEdit, resetHistoryBrowsing } = useAssistantInputHistory({
        applyInputValue,
        inputRef,
        inputValue: value,
        submittedPrompts: history,
    });

    // pathRef stays on the file this walk belongs to until the effect below
    // restores that file's draft. Updating it during render would point the
    // restore at the file just opened.
    const restoreRef = useRef(exitHistoryBrowsing);
    if (pathRef.current === path) {
        restoreRef.current = exitHistoryBrowsing;
    }
    useEffect(() => {
        if (pathRef.current === path) return;
        restoreRef.current();
        pathRef.current = path;
        restoreRef.current = () => false;
    }, [path]);

    const applyAutocompleteValue = useCallback((next: string) => {
        rememberHistoryEdit(next);
        onChange(next);
    }, [onChange, rememberHistoryEdit]);

    const autocomplete = useInputHistoryAutocomplete({
        inputValue: value,
        submittedPrompts: history,
        applyInputValue: applyAutocompleteValue,
        inputRef,
        disabled: isComposing || busy,
    });

    const commitTyped = (text: string) => {
        setHistory(rememberFileCompanionInput(text));
        // Acceptance can land after the user has switched files or replaced
        // the draft (↑, or typing while a dirty save is still in flight).
        // Resetting the walk then drops the draft they were putting back.
        if (pathRef.current !== path) return;
        const shown = (inputRef.current?.value ?? "").trim();
        if (shown !== "" && shown !== text) return;
        resetHistoryBrowsing();
    };

    const submitTyped = () => {
        const text = value.trim();
        // /clear still runs while a reply is on the way. Other text waits.
        if (!text || (busy && !isHistoryResetCommandText(text))) return;
        const accepted = onSubmit();
        // A sync host (and a host that already returned) records in this turn.
        // `await` would defer the write by a microtask, so ↑ in the same turn
        // would still walk the previous list. A promise records when it settles
        // to anything other than false — the window resolves at acceptance,
        // before the model reply.
        if (accepted && typeof accepted === "object" && typeof accepted.then === "function") {
            void accepted.then((ok) => {
                if (ok === false) return;
                commitTyped(text);
            }, () => undefined);
            return;
        }
        if (accepted === false) return;
        commitTyped(text);
    };

    const onKeyDown = (event: KeyboardEvent<HTMLTextAreaElement>) => {
        if (textComposition.shouldIgnoreKeyDown(event)) return;
        if (autocomplete.handleKeyDown(event)) return;
        if (event.key === "ArrowUp" && isSelectionCollapsedAtBoundary("up")) {
            if (recallHistory("up")) {
                event.preventDefault();
                return;
            }
        }
        if (event.key === "ArrowDown" && isSelectionCollapsedAtBoundary("down")) {
            if (recallHistory("down")) {
                event.preventDefault();
                return;
            }
        }
        if (event.key === "Escape") {
            if (exitHistoryBrowsing()) event.preventDefault();
            return;
        }
        const native = event.nativeEvent;
        if (event.key !== "Enter" || event.shiftKey || native.isComposing || native.keyCode === 229) return;
        event.preventDefault();
        submitTyped();
    };

    return (
        <form
            onSubmit={(event) => {
                event.preventDefault();
                submitTyped();
            }}
            style={{ position: "relative", padding: 12, background: "#ffffff", borderTop: "1px solid #e2e8f0" }}
        >
            <InputHistoryAutocomplete
                open={autocomplete.open}
                matches={autocomplete.matches}
                selectedIndex={autocomplete.selectedIndex}
                prefix={value}
                listboxId={historyListboxId}
                onSelectIndex={autocomplete.setSelectedIndex}
                onAccept={autocomplete.accept}
                theme={lightTheme}
                lang="zh-Hans"
                testId="file-companion-input-history"
                itemTestIdPrefix="file-companion-input-history-item"
            />
            <div style={{ display: "flex", gap: 8, alignItems: "flex-end", border: "1px solid #e2e8f0", borderRadius: 12, padding: 8, background: "#ffffff" }}>
                <textarea
                    ref={inputRef}
                    data-testid="file-companion-input"
                    aria-autocomplete="list"
                    aria-expanded={autocomplete.open}
                    aria-controls={autocomplete.open ? historyListboxId : undefined}
                    aria-activedescendant={autocomplete.open ? `${historyListboxId}-option-${autocomplete.selectedIndex}` : undefined}
                    value={value}
                    placeholder="输入问题，Enter 发送"
                    onChange={(event) => {
                        rememberHistoryEdit(event.target.value);
                        onChange(event.target.value);
                    }}
                    onCompositionStart={() => {
                        setIsComposing(true);
                        textComposition.onCompositionStart();
                    }}
                    onCompositionEnd={() => {
                        setIsComposing(false);
                        textComposition.onCompositionEnd();
                    }}
                    onKeyDown={onKeyDown}
                    style={{ flex: 1, minHeight: 44, maxHeight: 160, border: "none", outline: "none", resize: "none", padding: "6px 4px", font: "inherit", fontSize: 14, lineHeight: 1.5, background: "transparent", color: "#1e293b" }}
                />
                <button type="submit" className="file-companion-send" data-testid="file-companion-send" disabled={!busy && !value.trim()} aria-busy={busy}>发送</button>
            </div>
            <p style={{ margin: "6px 2px 0", fontSize: 12, color: "#94a3b8" }}>Enter 发送，Shift+Enter 换行，↑↓ 翻看已发送的内容，/clear 清空</p>
        </form>
    );
}
