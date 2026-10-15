import { act, renderHook } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { useAssistantInputHistory, type ComposerAttachmentSnapshot } from "../useAssistantInputHistory";

const shot = { filePath: "D:/shots/see.png", fileName: "see.png", extension: ".png", isImage: true };
const draftImage = { filePath: "D:/shots/draft.png", fileName: "draft.png", extension: ".png", isImage: true };

function renderHistory(options?: { prompts?: string[]; attachments?: typeof shot[][] }) {
    const prompts = options?.prompts ?? ["see this"];
    const attachments = options?.attachments ?? [[shot]];
    let snapshot: ComposerAttachmentSnapshot = { pending: [draftImage], selectedFilePaths: ["D:/docs/note.txt"] };
    const appliedValues: string[] = [];
    const appliedAttachments: ComposerAttachmentSnapshot[] = [];
    const input = { value: "draft", selectionStart: 5, selectionEnd: 5 } as HTMLTextAreaElement;
    const hook = renderHook(() => useAssistantInputHistory({
        applyInputValue: (next) => {
            appliedValues.push(next);
            input.value = next;
        },
        applyInputAttachments: (next) => {
            snapshot = {
                pending: next.pending.map(item => ({ ...item })),
                selectedFilePaths: [...next.selectedFilePaths],
            };
            appliedAttachments.push(snapshot);
        },
        readInputAttachments: () => snapshot,
        inputRef: { current: input },
        inputValue: input.value,
        submittedPrompts: prompts,
        submittedPromptAttachments: attachments,
    }));
    return { ...hook, appliedValues, appliedAttachments, input };
}

describe("useAssistantInputHistory attachments", () => {
    it("restores images for a recalled input and puts the draft files back on escape", () => {
        const { result, appliedValues, appliedAttachments } = renderHistory();

        act(() => { result.current.recallHistory("up"); });
        expect(appliedValues).toEqual(["see this"]);
        expect(appliedAttachments.at(-1)).toEqual({ pending: [shot], selectedFilePaths: [] });

        act(() => { expect(result.current.exitHistoryBrowsing()).toBe(true); });
        expect(appliedValues.at(-1)).toBe("draft");
        expect(appliedAttachments.at(-1)).toEqual({ pending: [draftImage], selectedFilePaths: ["D:/docs/note.txt"] });
    });

    it("keeps attachment edits made while browsing history", () => {
        const kept = { filePath: "D:/shots/kept.png", fileName: "kept.png", extension: ".png", isImage: true };
        let snapshot: ComposerAttachmentSnapshot = { pending: [], selectedFilePaths: [] };
        const applied: ComposerAttachmentSnapshot[] = [];
        const input = { value: "", selectionStart: 0, selectionEnd: 0 } as HTMLTextAreaElement;
        const { result } = renderHook(() => useAssistantInputHistory({
            applyInputValue: (next) => { input.value = next; },
            applyInputAttachments: (next) => {
                snapshot = { pending: next.pending.map(item => ({ ...item })), selectedFilePaths: [...next.selectedFilePaths] };
                applied.push(snapshot);
            },
            readInputAttachments: () => snapshot,
            inputRef: { current: input },
            inputValue: input.value,
            submittedPrompts: ["older", "newer"],
            submittedPromptAttachments: [[shot], [shot, kept]],
        }));

        act(() => { result.current.recallHistory("up"); });
        expect(applied.at(-1)?.pending.map(item => item.fileName)).toEqual(["see.png", "kept.png"]);

        act(() => {
            snapshot = { pending: [kept], selectedFilePaths: [] };
            result.current.recallHistory("up");
        });
        expect(applied.at(-1)?.pending).toEqual([shot]);

        act(() => { result.current.recallHistory("down"); });
        expect(applied.at(-1)?.pending).toEqual([kept]);
    });

    it("accepts a history completion together with its images", () => {
        const accept = vi.fn();
        const { result } = renderHook(() => useAssistantInputHistory({
            applyInputValue: () => {},
            applyInputAttachments: accept,
            inputRef: { current: null },
            inputValue: "see",
            submittedPrompts: ["see this", "see this"],
            submittedPromptAttachments: [[], [shot]],
        }));

        act(() => { result.current.acceptHistoryText("see this"); });
        expect(accept).toHaveBeenCalledWith({ pending: [shot], selectedFilePaths: [] });
    });
});
