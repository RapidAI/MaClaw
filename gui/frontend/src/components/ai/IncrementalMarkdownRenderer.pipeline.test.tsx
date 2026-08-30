// @vitest-environment jsdom
import { describe, expect, it } from "vitest";
import { render } from "@testing-library/react";
import { createIncrementalRenderState, renderContentIncremental } from "./IncrementalMarkdownRenderer";
import { renderMessage } from "./aiAssistantMarkdown";
import { lightTheme } from "./aiAssistantPanelTheme";

// Simulates the AIAssistantPanel wiring for the last streaming assistant
// message: renderMessage with the incremental content renderer callback and a
// persistent incremental state, driven by token-flush-sized content updates.
describe("streaming long-message render pipeline (panel wiring)", () => {
    it("keeps the streamed tail visible while a long list grows", () => {
        const intro = "抱歉，PDF生成工具当前不可用。但我已经完成了全网搜索，整理了张惠妹完整的歌曲信息。以下是详细报告：";
        const items = Array.from(
            { length: 60 },
            (_, i) => `${i + 1}. 《歌曲${i + 1}》 — 专辑《专辑${i + 1}》（${1996 + (i % 25)}年，时长 4:0${i % 10}）`,
        ).join("\n\n");
        const full = `${intro}\n\n${items}`;

        const incRef = { messageId: "", state: createIncrementalRenderState() };
        const renderStep = (content: string) => renderMessage(
            { id: "m1", role: "assistant", content, timestamp: 1 } as never,
            () => {},
            lightTheme,
            true,
            "已保存",
            "zh",
            true,
            (formatted: string) => {
                if (incRef.messageId !== "m1") {
                    incRef.messageId = "m1";
                    incRef.state = createIncrementalRenderState();
                }
                return renderContentIncremental(formatted, lightTheme, incRef.state);
            },
            undefined,
            false,
        );

        const steps = 40;
        const squash = (s: string) => s.replace(/\s+/g, "");
        const { container, rerender } = render(<div>{renderStep(full.slice(0, 80))}</div>);
        for (let step = 1; step <= steps; step++) {
            const partial = full.slice(0, Math.floor((full.length * step) / steps));
            rerender(<div>{renderStep(partial)}</div>);
            // The tail of whatever has streamed so far must be in the DOM.
            const tailLine = squash(partial.split("\n").filter((l) => l.trim()).at(-1)!).slice(0, 24);
            expect(squash(container.textContent || ""), `step ${step}/${steps}: tail ${JSON.stringify(tailLine)} missing`).toContain(tailLine);
        }
        expect(squash(container.textContent || "")).toContain("歌曲60");
    });
});
