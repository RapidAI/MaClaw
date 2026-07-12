import React from "react";
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { MessageContentRenderer } from "../MessageContentRenderer";
import { lightTheme } from "../aiAssistantPanelTheme";

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
});
