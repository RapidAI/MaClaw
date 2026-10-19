import { describe, expect, it } from "vitest";
import {
    ASSISTANT_SYSTEM_FONT,
    acceptDraftedValue,
    assistantFontOptions,
    assistantMessageFontStyle,
    chatFontSizeSaveQueue,
    clampChatFontSize,
    decideConfigChatFontSize,
    decideConfigFontFamily,
    enqueueSerial,
    normalizeAssistantFontId,
    resetAssistantFontSaveQueues,
    snapshotConfirmsDraft,
} from "../assistantFonts";

/** CSS custom properties are not part of CSSProperties' index signature. */
const customProps = (style: object) => style as unknown as Record<string, string | undefined>;

describe("assistant fonts", () => {
    it("keeps the current face as the default for both assistants", () => {
        expect(normalizeAssistantFontId("conversation", "")).toBe("system");
        expect(normalizeAssistantFontId("coding", undefined)).toBe("system");
        expect(assistantMessageFontStyle("conversation", "system").fontFamily).toBe(ASSISTANT_SYSTEM_FONT);
        expect(assistantMessageFontStyle("coding", "").fontFamily).toBe(ASSISTANT_SYSTEM_FONT);
        expect(customProps(assistantMessageFontStyle("conversation", "system"))["--assistant-code-font"]).toBeUndefined();
        expect(customProps(assistantMessageFontStyle("coding", "nope"))["--assistant-code-font"]).toBeUndefined();
    });

    it("gives the conversation assistant reading faces and Maple Mono", () => {
        const ids = assistantFontOptions("conversation").map((option) => option.id);
        expect(ids).toEqual(["system", "maple-mono", "dengxian", "sarasa-gothic", "lxgw-wenkai"]);
        expect(ids).not.toContain("jetbrains-mono");
        const maple = assistantMessageFontStyle("conversation", "maple-mono");
        expect(String(maple.fontFamily).indexOf("Maple Mono CN")).toBeLessThan(String(maple.fontFamily).indexOf("MaClaw Maple Mono"));
        expect(customProps(maple)["--assistant-code-font"]).toContain("MaClaw Maple Mono");
        const reading = assistantMessageFontStyle("conversation", "lxgw-wenkai");
        expect(reading.fontFamily).toContain("LXGW WenKai");
        expect(customProps(reading)["--assistant-code-font"]).toBeUndefined();
    });

    it("limits the programming assistant to programming faces", () => {
        const ids = assistantFontOptions("coding").map((option) => option.id);
        expect(ids).toEqual(["system", "maple-mono", "jetbrains-mono", "cascadia-code", "fira-code", "sarasa-mono"]);
        expect(ids).not.toContain("dengxian");
        expect(normalizeAssistantFontId("coding", "dengxian")).toBe("system");
        expect(normalizeAssistantFontId("conversation", "jetbrains-mono")).toBe("system");
        const jetbrains = assistantMessageFontStyle("coding", "jetbrains-mono");
        expect(jetbrains.fontFamily).toContain("MaClaw JetBrains Mono");
        expect(customProps(jetbrains)["--assistant-code-font"]).toContain("MaClaw JetBrains Mono");
        const cascadia = assistantMessageFontStyle("coding", "cascadia-code");
        expect(String(cascadia.fontFamily).includes("Cascadia Mono")).toBe(false);
        expect(String(cascadia.fontFamily).indexOf("Cascadia Code")).toBeLessThan(String(cascadia.fontFamily).indexOf("MaClaw Cascadia Code"));
        const cascadiaOption = assistantFontOptions("coding").find((option) => option.id === "cascadia-code");
        expect(cascadiaOption?.description.zhHans).toContain("连字");
        expect(cascadiaOption?.description.zhHans).not.toContain("安装完整");
    });

    it("clamps the shared chat size to an integer from 12 through 24", () => {
        expect(clampChatFontSize(14.6)).toBe(15);
        expect(clampChatFontSize(30)).toBe(24);
        expect(clampChatFontSize(11)).toBe(12);
        expect(clampChatFontSize(Number.NaN)).toBe(14);
    });

    it("keeps an unsaved chat size when a config snapshot still has the previous one", () => {
        expect(decideConfigChatFontSize(30, null)).toEqual({ action: "apply", size: 24 });
        expect(decideConfigChatFontSize(14.6, 15)).toEqual({ action: "apply", size: 15 });
        expect(decideConfigChatFontSize(18, 18)).toEqual({ action: "apply", size: 18 });
        expect(decideConfigChatFontSize(14, 18)).toEqual({ action: "keep" });
        expect(decideConfigChatFontSize(11, 18)).toEqual({ action: "invalid" });
        expect(acceptDraftedValue("maple-mono", "system")).toBeNull();
        expect(acceptDraftedValue("system", null)).toBe("system");
        expect(acceptDraftedValue("system", "system")).toBe("system");
        expect(snapshotConfirmsDraft(18, 18, true)).toBe(true);
        expect(snapshotConfirmsDraft(18, 18, false)).toBe(false);
        expect(snapshotConfirmsDraft(18, null, false)).toBe(true);
    });

    it("treats an omitted font family as the stored default, not as no change", () => {
        expect(decideConfigFontFamily("conversation", "maple-mono", null, "system")).toEqual({ action: "apply", id: "maple-mono" });
        expect(decideConfigFontFamily("conversation", "jetbrains-mono", null, "system")).toEqual({ action: "apply", id: "system" });
        expect(decideConfigFontFamily("conversation", "", "maple-mono", "maple-mono")).toEqual({ action: "keep" });
        expect(decideConfigFontFamily("coding", "dengxian", null, "system")).toEqual({ action: "apply", id: "system" });
        // json omitempty drops the empty string. Already on system: nothing to move.
        expect(decideConfigFontFamily("conversation", undefined, null, "system")).toEqual({ action: "keep" });
        expect(decideConfigFontFamily("coding", undefined, "jetbrains-mono", "jetbrains-mono")).toEqual({ action: "keep" });
        // A custom face is on screen and this snapshot did not carry an id. Read disk.
        expect(decideConfigFontFamily("conversation", undefined, null, "maple-mono")).toEqual({ action: "read" });
        expect(decideConfigFontFamily("coding", null, null, "fira-code")).toEqual({ action: "read" });
    });

    it("runs one field's saves in order and skips a value replaced before it is sent", async () => {
        resetAssistantFontSaveQueues();
        const sent: string[] = [];
        let releaseFirst: () => void = () => {};
        const latest = { current: "second" };
        enqueueSerial(chatFontSizeSaveQueue, () => new Promise<void>((resolve) => {
            releaseFirst = () => {
                sent.push("first");
                resolve();
            };
        }));
        enqueueSerial(chatFontSizeSaveQueue, () => {
            if (latest.current !== "second") return;
            sent.push("second");
        });
        latest.current = "third";
        enqueueSerial(chatFontSizeSaveQueue, () => {
            sent.push("third");
        });
        expect(sent).toEqual([]);
        releaseFirst();
        await chatFontSizeSaveQueue.tail;
        expect(sent).toEqual(["first", "third"]);
        resetAssistantFontSaveQueues();
    });
});
