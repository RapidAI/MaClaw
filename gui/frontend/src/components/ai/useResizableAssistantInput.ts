import { useCallback, useEffect, useState } from "react";

export function useResizableAssistantInput(inputRef: React.MutableRefObject<HTMLTextAreaElement | null>, inputValue: string) {
    const [inputAreaHeight, setInputAreaHeight] = useState<number | null>(null);

    const resizeInput = useCallback(() => {
        if (!inputRef.current) return;
        const maxHeight = inputAreaHeight ?? 120;
        inputRef.current.style.height = "auto";
        inputRef.current.style.height = Math.min(inputRef.current.scrollHeight, maxHeight) + "px";
    }, [inputAreaHeight, inputRef]);

    useEffect(() => {
        if (!inputRef.current) return;
        resizeInput();
    }, [inputValue, resizeInput, inputRef]);

    const startInputResize = useCallback((e: React.MouseEvent<HTMLDivElement>) => {
        e.preventDefault();
        const startY = e.clientY;
        const startHeight = inputAreaHeight ?? 120;
        const onMouseMove = (moveEvent: MouseEvent) => {
            const next = Math.max(56, Math.min(260, startHeight - (moveEvent.clientY - startY)));
            setInputAreaHeight(next);
        };
        const onMouseUp = () => {
            document.removeEventListener("mousemove", onMouseMove);
            document.removeEventListener("mouseup", onMouseUp);
            document.body.style.cursor = "";
            document.body.style.userSelect = "";
        };
        document.body.style.cursor = "ns-resize";
        document.body.style.userSelect = "none";
        document.addEventListener("mousemove", onMouseMove);
        document.addEventListener("mouseup", onMouseUp);
    }, [inputAreaHeight]);

    return { inputAreaHeight, resizeInput, startInputResize };
}
