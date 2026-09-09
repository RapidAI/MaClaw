// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
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

    it("keeps a display formula in the active tail until its closing delimiter arrives", () => {
        const state = createIncrementalRenderState();
        const completed = "Completed paragraph.\n\n".repeat(120);
        // Long enough that the incremental renderer would otherwise see the
        // internal blank lines before its active-tail safety margin.
        const openFormula = `$$\n${"E = mc^2\n\n".repeat(48)}`;

        renderContentIncremental(`${completed}${openFormula}`, lightTheme, state);

        // The blank line within the unfinished formula must not become a frozen
        // boundary; otherwise its closing delimiter would be rendered separately.
        expect(state.frozen?.contentUpTo).toBe(completed.length);

        const resolved = `${completed}${openFormula}$$\n\n${"Later paragraph.\n\n".repeat(24)}Active tail`;
        const { container } = render(<div>{renderContentIncremental(resolved, lightTheme, state)}</div>);

        expect(container.querySelectorAll('[data-testid="assistant-display-math"]')).toHaveLength(1);
        expect(container.textContent).not.toContain("$$");
    });

    it("treats a closing delimiter on the final TeX line as a safe freeze boundary", () => {
        const state = createIncrementalRenderState();
        const completed = "Completed paragraph.\n\n".repeat(120);
        const formula = `$$\n${"x^2 + y^2 = z^2\n".repeat(48)}\\end{aligned}$$\n\n`;
        const content = `${completed}${formula}${"Later paragraph.\n\n".repeat(24)}Active tail`;

        const { container } = render(<div>{renderContentIncremental(content, lightTheme, state)}</div>);

        expect(state.frozen?.contentUpTo).toBeGreaterThan(completed.length);
        expect(container.querySelectorAll('[data-testid="assistant-display-math"]')).toHaveLength(1);
        expect(container.textContent).not.toContain("$$");
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
