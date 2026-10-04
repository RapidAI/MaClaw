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

    it("does not freeze inside a code fence whose opener is glued to the previous line", () => {
        const state = createIncrementalRenderState();
        const completed = "Completed paragraph.\n\n".repeat(80);
        const inside = "more code that stays fenced\n\n".repeat(30);
        const glued = `Heading\`\`\`\ncode line\n\n${inside}\`\`\`\n\nAfter the block.\n\n${"Tail paragraph.\n\n".repeat(20)}`;
        const content = completed + glued;

        renderContentIncremental(content, lightTheme, state);

        const upTo = state.frozen?.contentUpTo ?? 0;
        expect(upTo).toBeGreaterThan(0);
        const frozen = content.slice(0, upTo);
        const tail = content.slice(upTo);
        if (frozen.includes("Heading```")) {
            expect(frozen).toContain("\n```\n");
        }
        expect(tail.startsWith("more code")).toBe(false);
        expect(tail.startsWith("code line")).toBe(false);
    });

    it("does not freeze inside an unfinished longer code fence", () => {
        const state = createIncrementalRenderState();
        const completed = "Completed paragraph.\n\n".repeat(120);
        const partialMermaid = `\`\`\`\`mermaid\ngraph TD\n\`\`\`\n\n${"A --> B\n".repeat(80)}`;

        renderContentIncremental(`${completed}${partialMermaid}`, lightTheme, state);

        expect(state.frozen?.contentUpTo).toBe(completed.length);
        expect(state.lastTailContent).toContain("\`\`\`\`mermaid");
    });

    it("does not let backticks inside a display formula open a fence", () => {
        const state = createIncrementalRenderState();
        const completed = "Completed paragraph.\n\n".repeat(120);
        // A standalone closer after the opener would end a false fence, and the
        // blank lines below would then freeze in the middle of the formula.
        const openFormula = `$$ x\`\`\`\n\`\`\`\n${"E = mc^2\n\n".repeat(48)}`;

        renderContentIncremental(`${completed}${openFormula}`, lightTheme, state);

        expect(state.frozen?.contentUpTo).toBe(completed.length);

        const resolved = `${completed}${openFormula}$$\n\n${"Later paragraph.\n\n".repeat(24)}Active tail`;
        const { container } = render(<div>{renderContentIncremental(resolved, lightTheme, state)}</div>);

        expect(container.querySelectorAll('[data-testid="assistant-display-math"]')).toHaveLength(1);
        expect(container.textContent).not.toContain("$$");
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

    it("still freezes on paragraph breaks when thinking spacers are omitted", () => {
        const state = createIncrementalRenderState();
        const content = `${"Thought line about the search order.\n\n".repeat(80)}Active tail`;

        const { container } = render(
            <div>{renderContentIncremental(content, lightTheme, state, { omitBlankSpacers: true })}</div>,
        );

        expect(state.frozen?.contentUpTo).toBeGreaterThan(0);
        const spacers = Array.from(container.querySelectorAll("div")).filter((node) => node.textContent === "\u00A0");
        expect(spacers.length).toBe(0);
        expect(container.textContent).toContain("Thought line about the search order.");
        expect(container.textContent).toContain("Active tail");
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
