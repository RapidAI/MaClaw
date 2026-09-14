// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { CONTINUE_EDIT_TASK_RESULT_EVENT } from "../taskResultPreview";
import { useContinueEditTaskResultDraft } from "../useContinueEditTaskResultDraft";

describe("useContinueEditTaskResultDraft", () => {
    it("fills an empty composer and requests a focus pass", () => {
        const inputRef: { current: HTMLTextAreaElement | null } = { current: null };
        const textarea = document.createElement("textarea");
        document.body.appendChild(textarea);
        inputRef.current = textarea;
        const updateInputValue = vi.fn();
        const resizeInput = vi.fn();

        renderHook(() => useContinueEditTaskResultDraft({
            lang: "zh",
            inputRef,
            updateInputValue,
            resizeInput,
        }));

        act(() => {
            window.dispatchEvent(new CustomEvent(CONTINUE_EDIT_TASK_RESULT_EVENT, {
                detail: { path: "C:\\docs\\report.docx" },
            }));
        });

        expect(updateInputValue).toHaveBeenCalledWith("请继续修改文档「report.docx」（C:\\docs\\report.docx）：\n");
        textarea.remove();
    });

    it("does not overwrite composer text that already names the file", () => {
        const inputRef: { current: HTMLTextAreaElement | null } = { current: null };
        const textarea = document.createElement("textarea");
        textarea.value = "请继续修改文档「report.docx」（C:\\docs\\report.docx）：\n再短一点";
        document.body.appendChild(textarea);
        inputRef.current = textarea;
        const updateInputValue = vi.fn();

        renderHook(() => useContinueEditTaskResultDraft({
            lang: "zh",
            inputRef,
            updateInputValue,
            resizeInput: vi.fn(),
        }));

        act(() => {
            window.dispatchEvent(new CustomEvent(CONTINUE_EDIT_TASK_RESULT_EVENT, {
                detail: { path: "C:\\docs\\report.docx" },
            }));
        });

        expect(updateInputValue).not.toHaveBeenCalled();
        textarea.remove();
    });
});
