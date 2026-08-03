// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { createIncrementalRenderState, renderContentIncremental } from "./IncrementalMarkdownRenderer";
import { darkTheme, lightTheme } from "./aiAssistantPanelTheme";

describe("renderContentIncremental", () => {
    it("resets cached output when same-length streaming text is replaced", () => {
        const state = createIncrementalRenderState();
        const first = `${"Completed paragraph.\n\n".repeat(120)}First tail`;
        const replacement = `${"Completed paragraph.\n\n".repeat(120)}Other tail`;

        renderContentIncremental(first, lightTheme, state);
        renderContentIncremental(replacement, lightTheme, state);

        expect(state.lastContent).toBe(replacement);
        expect(state.lastTailContent).toContain("Other tail");
    });

    it("freezes completed prose before an unfinished Mermaid fence", () => {
        const state = createIncrementalRenderState();
        const completed = "Completed paragraph.\n\n".repeat(120);
        const partialMermaid = `\`\`\`mermaid\ngraph TD\n${"A --> B\n".repeat(80)}`;

        renderContentIncremental(`${completed}${partialMermaid}`, lightTheme, state);

        expect(state.frozen?.contentUpTo).toBeGreaterThan(0);
        expect(state.lastTailContent).toContain("```mermaid");
    });

    it("keeps tilde-fenced Mermaid source in the active tail while it is unfinished", () => {
        const state = createIncrementalRenderState();
        const completed = "Completed paragraph.\n\n".repeat(120);
        const partialMermaid = `~~~mermaid\ngraph TD\n${"A --> B\n".repeat(80)}`;

        renderContentIncremental(`${completed}${partialMermaid}`, lightTheme, state);

        expect(state.frozen?.contentUpTo).toBeGreaterThan(0);
        expect(state.lastTailContent).toContain("~~~mermaid");
    });

    it("does not freeze inside an unfinished longer code fence", () => {
        const state = createIncrementalRenderState();
        const completed = "Completed paragraph.\n\n".repeat(120);
        const partialMermaid = `\`\`\`\`mermaid\ngraph TD\n\`\`\`\n\n${"A --> B\n".repeat(80)}`;

        renderContentIncremental(`${completed}${partialMermaid}`, lightTheme, state);

        expect(state.frozen?.contentUpTo).toBe(completed.length);
        expect(state.lastTailContent).toContain("\`\`\`\`mermaid");
    });

    it("rebuilds frozen nodes when the theme changes during streaming", () => {
        const state = createIncrementalRenderState();
        const content = `${"Completed paragraph.\n\n".repeat(120)}Active tail`;

        renderContentIncremental(content, lightTheme, state);
        const lightFrozenNodes = state.frozen?.nodes;
        renderContentIncremental(content, darkTheme, state);

        expect(state.lastTheme).toBe(darkTheme);
        expect(state.frozen?.nodes).not.toBe(lightFrozenNodes);
    });
});
