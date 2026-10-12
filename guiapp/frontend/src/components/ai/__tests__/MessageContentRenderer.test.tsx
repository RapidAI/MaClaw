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

    it("keeps the camel case that Mermaid's diagram detectors require", () => {
        // Regression: sanitising used to lower-case every diagram type, which
        // made mermaid 11.x throw "No diagram type detected matching given
        // configuration for text: sequencediagram …" at render time. The
        // detector regexes are case-sensitive even though the parsers are not.
        expect(sanitizeMermaidCode("sequenceDiagram\n  U->>N: GET /")).toBe("sequenceDiagram\n  U->>N: GET /");
        expect(sanitizeMermaidCode("SEQUENCEDIAGRAM\n  U->>N: GET /")).toBe("sequenceDiagram\n  U->>N: GET /");
        expect(sanitizeMermaidCode("StateDiagram-v2\n  [*] --> A")).toBe("stateDiagram-v2\n  [*] --> A");
        expect(sanitizeMermaidCode("ClassDiagram\n  class A")).toBe("classDiagram\n  class A");
        expect(sanitizeMermaidCode("ErDiagram\n  USER ||--o{ ORDER : places")).toBe("erDiagram\n  USER ||--o{ ORDER : places");
        expect(sanitizeMermaidCode("Gitgraph TB\n  commit")).toBe("gitGraph TB\n  commit");
        expect(sanitizeMermaidCode("Quadrantchart\n  title q")).toBe("quadrantChart\n  title q");
    });

    it("maps every sanitizer alias onto a keyword Mermaid's detector actually accepts", () => {
        // Authoritative detector regexes, extracted from
        // node_modules/mermaid@11.14.0/dist/mermaid.core.mjs
        // (`detector12 = … /^\s*sequenceDiagram/.test(txt)` …). If this table
        // drifts from the installed mermaid, diagrams fail detection again.
        const detectors: Array<[RegExp, string]> = [
            [/^\s*graph/, "graph"],
            [/^\s*flowchart/, "flowchart"],
            [/^\s*sequenceDiagram/, "sequenceDiagram"],
            [/^\s*stateDiagram-v2/, "stateDiagram-v2"],
            [/^\s*stateDiagram/, "stateDiagram"],
            [/^\s*classDiagram/, "classDiagram"],
            [/^\s*erDiagram/, "erDiagram"],
            [/^\s*gantt/, "gantt"],
            [/^\s*pie/, "pie"],
            [/^\s*gitGraph/, "gitGraph"],
            [/^\s*journey/, "journey"],
            [/^\s*mindmap/, "mindmap"],
            [/^\s*timeline/, "timeline"],
            [/^\s*quadrantChart/, "quadrantChart"],
            [/^\s*sankey(-beta)?/, "sankey-beta"],
            [/^\s*xychart(-beta)?/, "xychart-beta"],
        ];
        const aliases = [
            "Graph", "Flowchart", "SequenceDiagram", "StateDiagram", "StateDiagram-v2",
            "ClassDiagram", "ErDiagram", "Gantt", "Pie", "Gitgraph", "Journey", "Mindmap",
            "Timeline", "Quadrantchart", "Sankey-beta", "Xychart-beta",
        ];

        for (const alias of aliases) {
            for (const spelling of [alias, alias.toUpperCase()]) {
                const canonical = sanitizeMermaidCode(`${spelling} TD`).trim();
                const matched = detectors.some(([regex]) => regex.test(canonical));
                if (!matched) throw new Error(`sanitizer alias "${alias}" produced "${canonical}", which no mermaid 11.14 detector accepts`);
                expect(matched).toBe(true);
            }
        }
    });

    it("rejects diagrams that exceed the chat rendering node budget", () => {
        const oversized = Array.from({ length: 751 }, (_, i) => `N${i}[Node ${i}]`).join("\n");
        const denseEdges = Array.from({ length: 1501 }, (_, i) => `A --> B${i}`).join("\n");

        expect(exceedsMermaidRenderBudget(oversized)).toBe(true);
        expect(exceedsMermaidRenderBudget(denseEdges)).toBe(true);
        expect(exceedsMermaidRenderBudget("graph TD\nA[Start] --> B[Finish]")).toBe(false);
    });
});
