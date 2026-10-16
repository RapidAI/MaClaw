import { useCallback } from "react";

export function useAssistantPreviewResize(setSplitRatio: (ratio: number) => void) {
    return useCallback((startEvent?: MouseEvent | PointerEvent | number) => {
        if (typeof startEvent === "number") {
            setSplitRatio(Math.max(0.2, Math.min(0.8, startEvent)));
            return;
        }
        const container = document.querySelector('[data-testid="ai-panel-root"]') as HTMLElement | null;
        if (!container) return;

        const pointerId = startEvent && "pointerId" in startEvent ? startEvent.pointerId : null;
        const updateRatio = (clientX: number) => {
            const rect = container.getBoundingClientRect();
            if (rect.width <= 0) return;
            const nextRatio = Math.max(0.2, Math.min(0.8, (clientX - rect.left) / rect.width));
            setSplitRatio(nextRatio);
        };

        const onPointerMove = (e: PointerEvent) => {
            if (pointerId !== null && e.pointerId !== pointerId) return;
            e.preventDefault();
            updateRatio(e.clientX);
        };
        const onMouseMove = (e: MouseEvent) => updateRatio(e.clientX);
        const stopResize = () => {
            document.removeEventListener("pointermove", onPointerMove);
            document.removeEventListener("pointerup", stopResize);
            document.removeEventListener("pointercancel", stopResize);
            document.removeEventListener("mousemove", onMouseMove);
            document.removeEventListener("mouseup", stopResize);
            document.body.style.cursor = "";
            document.body.style.userSelect = "";
        };
        document.body.style.cursor = "col-resize";
        document.body.style.userSelect = "none";
        document.addEventListener("pointermove", onPointerMove, { passive: false });
        document.addEventListener("pointerup", stopResize);
        document.addEventListener("pointercancel", stopResize);
        document.addEventListener("mousemove", onMouseMove);
        document.addEventListener("mouseup", stopResize);
    }, [setSplitRatio]);
}
