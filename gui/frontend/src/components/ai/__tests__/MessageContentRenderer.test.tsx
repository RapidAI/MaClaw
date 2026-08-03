import React from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MessageContentRenderer } from "../MessageContentRenderer";
import { lightTheme } from "../aiAssistantPanelTheme";
import { exceedsMermaidRenderBudget, isMermaidCodeFence, sanitizeMermaidCode } from "../AssistantMermaidDiagram";

describe("MessageContentRenderer role prefix display guard", () => {
    it("strips a leading Browser role prefix before rendering assistant content", () => {
        render(<MessageContentRenderer content="Browser: 现在情况清楚了。" theme={lightTheme} />);

        expect(screen.getByText("现在情况清楚了。")).toBeTruthy();
        expect(screen.queryByText(/Browser:/)).toBeNull();
    });

    it("does not strip user-authored content", () => {
        render(<MessageContentRenderer content="Browser: 我输入的文本" theme={lightTheme} isUser />);

        expect(screen.getByText("Browser: 我输入的文本")).toBeTruthy();
    });

    it("strips leading decorative pictographs from assistant content only", () => {
        // Display strip is applied inside the shared markdown pipeline.
        render(<MessageContentRenderer content={"\u{1F680} 部署完成"} theme={lightTheme} />);

        expect(screen.getByText("部署完成")).toBeTruthy();
        expect(screen.queryByText(/\u{1F680}/u)).toBeNull();
    });

    it("recognizes Mermaid fenced-block language tags case-insensitively", () => {
        expect(isMermaidCodeFence("mermaid")).toBe(true);
        expect(isMermaidCodeFence("MERMAID title=architecture")).toBe(true);
        expect(isMermaidCodeFence("typescript")).toBe(false);
    });

    it("normalizes common Mermaid keyword casing without changing node labels", () => {
        const source = "Graph TD\nSubgraph Platform\nA[Graph service]\nEnd";

        expect(sanitizeMermaidCode(source)).toBe("graph TD\nsubgraph Platform\nA[Graph service]\nend");
    });

    it("rejects diagrams that exceed the chat rendering node budget", () => {
        const oversized = Array.from({ length: 751 }, (_, i) => `N${i}[Node ${i}]`).join("\n");
        const denseEdges = Array.from({ length: 1501 }, (_, i) => `A --> B${i}`).join("\n");

        expect(exceedsMermaidRenderBudget(oversized)).toBe(true);
        expect(exceedsMermaidRenderBudget(denseEdges)).toBe(true);
        expect(exceedsMermaidRenderBudget("graph TD\nA[Start] --> B[Finish]")).toBe(false);
    });
});
