import { useCallback, useEffect, useState } from "react";

const INPUT_STACK_MIN_HEIGHT = 72;
const INPUT_STACK_MAX_HEIGHT = 420;
const INPUT_TEXTAREA_DEFAULT_MAX_HEIGHT = 120;

export function useResizableAssistantInput(
    inputRef: React.MutableRefObject<HTMLTextAreaElement | null>,
    onResizeEnd?: () => void,
) {
    const [inputAreaHeight, setInputAreaHeight] = useState<number | null>(null);

    const resizeInput = useCallback((target?: HTMLTextAreaElement) => {
        const input = target ?? inputRef.current;
        if (!input) return;
        if (inputAreaHeight) {
            input.style.height = "100%";
            return;
        }
        input.style.height = "auto";
        input.style.height = Math.min(input.scrollHeight, INPUT_TEXTAREA_DEFAULT_MAX_HEIGHT) + "px";
    }, [inputAreaHeight, inputRef]);

    useEffect(() => {
        resizeInput();
    }, [inputAreaHeight, resizeInput]);

    const startInputResize = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
        e.preventDefault();
        const stack = e.currentTarget.nextElementSibling as HTMLElement | null;
        const startY = e.clientY;
        const startHeight = inputAreaHeight ?? stack?.getBoundingClientRect().height ?? 150;
        const onMouseMove = (moveEvent: MouseEvent) => {
            const next = Math.max(INPUT_STACK_MIN_HEIGHT, Math.min(INPUT_STACK_MAX_HEIGHT, startHeight - (moveEvent.clientY - startY)));
            setInputAreaHeight(next);
        };
        const onMouseUp = () => {
            document.removeEventListener("mousemove", onMouseMove);
            document.removeEventListener("mouseup", onMouseUp);
            document.body.style.cursor = "";
            document.body.style.userSelect = "";
            onResizeEnd?.();
        };
        document.body.style.cursor = "ns-resize";
        document.body.style.userSelect = "none";
        document.addEventListener("mousemove", onMouseMove);
        document.addEventListener("mouseup", onMouseUp);
    }, [inputAreaHeight, onResizeEnd]);

    return { inputAreaHeight, resizeInput, startInputResize };
}
