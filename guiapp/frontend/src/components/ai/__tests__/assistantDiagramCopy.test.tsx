import { describe, expect, it } from "vitest";
import { localizeText } from "../../../i18n";
import { getMermaidFallbackMessage, mermaidBudgetMessage, prepareMermaidExportSource } from "../AssistantMermaidDiagram";

describe("diagram UI copy follows the UI language", () => {
    it("localises the messages the component itself generates", () => {
        expect(localizeText("zh-Hans", "Rendering diagram…", "正在渲染图表…", "正在渲染圖表…")).toBe("正在渲染图表…");
        expect(localizeText("en", "Rendering diagram…", "正在渲染图表…", "正在渲染圖表…")).toBe("Rendering diagram…");
        expect(localizeText("zh-Hant", "Rendering diagram…", "正在渲染图表…", "正在渲染圖表…")).toBe("正在渲染圖表…");
    });

    it("keeps Mermaid's own complaint verbatim behind a localised lead sentence", () => {
        // The localised sentence is what the reader reads; the technical detail is
        // the only route to "what did I write wrong", so it must survive intact.
        const en = getMermaidFallbackMessage("Parse error on line 3", "en");
        expect(en).toBe("Unable to render this Mermaid diagram. Parse error on line 3");

        const zh = getMermaidFallbackMessage("Parse error on line 3", "zh-Hans");
        expect(zh).toContain("无法渲染此 Mermaid 图表。");
        expect(zh).toContain("Parse error on line 3");
    });

    it("localises the empty / over-budget refusals", () => {
        const empty = localizeText("zh-Hans", "The Mermaid code block is empty.", "Mermaid 代码块为空。", "Mermaid 程式碼區塊為空。");
        expect(empty).toBe("Mermaid 代码块为空。");

        const budget = mermaidBudgetMessage("zh-Hans");
        expect(budget).toContain("渲染上限");
        expect(budget).toMatch(/750/);
        expect(budget).toMatch(/1,500/);
        expect(getMermaidFallbackMessage(budget, "zh-Hans")).toBe(budget);
        expect(getMermaidFallbackMessage("Mermaid 代码块为空。", "zh-Hans")).toBe("Mermaid 代码块为空。");
    });
});

describe("export keeps agreeing with the screen", () => {
    it("still canonicalises casing after localisation changes", () => {
        expect(prepareMermaidExportSource("sequencediagram\n  A->>B: hi")).toContain("sequenceDiagram");
    });
});

describe("failure copy follows the language the component renders with", () => {
    // Regression guard: WorkflowDocPreview once baked localizeText(...) into the
    // error state at throw time. Then a language switch left the stale translation
    // on screen, because failure state is exactly what keeps the error branch
    // mounted — there is no later render to pick up the new language.
    it("picks wording from the language argument, not from a stored translation", () => {
        const wording = (lang: string | undefined, loadFailure: boolean) => localizeText(
            lang,
            loadFailure ? "Failed to load Mermaid" : "Mermaid render error",
            loadFailure ? "Mermaid 加载失败" : "Mermaid 渲染错误",
            loadFailure ? "Mermaid 載入失敗" : "Mermaid 渲染錯誤",
        );

        expect(wording("en", false)).toBe("Mermaid render error");
        expect(wording("zh-Hans", false)).toBe("Mermaid 渲染错误");
        expect(wording("zh-Hant", false)).toBe("Mermaid 渲染錯誤");
        expect(wording("zh-Hans", true)).toBe("Mermaid 加载失败");
    });

    it("surfaces Mermaid's own complaint instead of hiding it behind generic copy", () => {
        // getMermaidFallbackMessage prefixes localised copy but must never swallow
        // the technical detail: it is the reader's only clue to what they wrote wrong.
        const localised = getMermaidFallbackMessage("No diagram type detected", "zh-Hans");
        expect(localised).toContain("无法渲染此 Mermaid 图表。");
        expect(localised).toContain("No diagram type detected");
    });
});
