import { describe, expect, it } from "vitest";
import { appendPromptHistory, findNewestHistoryAttachments, promptHistoryFromPersisted } from "../promptHistory";

const shot = { filePath: "D:/shots/see.png", fileName: "see.png", extension: ".png", isImage: true };

describe("prompt history attachments", () => {
    it("keeps images with the submitted text and drops thumbnail payloads", () => {
        const history = appendPromptHistory([], "see this", [{ ...shot, thumbnailDataUrl: "data:image/png;base64,huge" } as typeof shot & { thumbnailDataUrl: string }]);
        expect(history).toEqual([{ text: "see this", attachments: [shot] }]);
    });

    it("keeps an image-only submission and a later text with different images", () => {
        const other = { filePath: "D:/shots/other.png", fileName: "other.png", extension: ".png", isImage: true };
        const history = appendPromptHistory(
            appendPromptHistory([], "see this", [shot]),
            "see this",
            [other],
        );
        const withImageOnly = appendPromptHistory(history, "   ", [shot]);
        expect(withImageOnly.map(entry => entry.text)).toEqual(["see this", "see this", ""]);
        expect(withImageOnly[2].attachments).toEqual([shot]);
        expect(findNewestHistoryAttachments(withImageOnly.map(entry => entry.text), withImageOnly.map(entry => entry.attachments), "see this")).toEqual([other]);
    });

    it("does not record the same text and images twice in a row", () => {
        const once = appendPromptHistory([], "see this", [shot]);
        expect(appendPromptHistory(once, "see this", [shot])).toBe(once);
    });

    it("restores attachment rows saved beside prompt text", () => {
        const restored = promptHistoryFromPersisted(
            ["see this", ""],
            [[{ filePath: " D:/shots/see.png ", fileName: "see.png", extension: ".png", isImage: true }], [{ filePath: "D:/shots/alone.png" }]],
        );
        expect(restored).toEqual([
            { text: "see this", attachments: [shot] },
            { text: "", attachments: [{ filePath: "D:/shots/alone.png", fileName: "alone.png", extension: ".png", isImage: true }] },
        ]);
    });
});
