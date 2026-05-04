import { useCallback } from "react";

export function useAssistantPreviewResize(setSplitRatio: (ratio: number) => void) {
    return useCallback(() => {
        const container = document.querySelector('[data-testid="ai-panel-root"]') as HTMLElement | null;
        if (!container) return;
        const onMouseMove = (e: MouseEvent) => {
            const rect = container.getBoundingClientRect();
            if (rect.width <= 0) return;
            const nextRatio = Math.max(0.2, Math.min(0.8, (e.clientX - rect.left) / rect.width));
            setSplitRatio(nextRatio);
        };
        const onMouseUp = () => {
            document.removeEventListener("mousemove", onMouseMove);
            document.removeEventListener("mouseup", onMouseUp);
            document.body.style.cursor = "";
            document.body.style.userSelect = "";
        };
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
        document.addEventListener("mousemove", onMouseMove);
        document.addEventListener("mouseup", onMouseUp);
    }, [setSplitRatio]);
}