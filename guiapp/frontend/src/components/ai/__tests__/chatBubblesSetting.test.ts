import { readFileSync } from "node:fs";
import { dirname, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { describe, expect, it } from "vitest";
import { applyChatBubblesSnapshot, chatBubblesEchoClearsDraft, chatBubblesFromRead, publishChatBubbles, resetChatBubblesSaveQueue } from "../chatBubblesSetting";

const partials = resolve(dirname(fileURLToPath(import.meta.url)), "../../../styles/partials");

describe("applyChatBubblesSnapshot", () => {
    it("keeps an unsaved choice ahead of an older snapshot", () => {
        expect(applyChatBubblesSnapshot(true, false)).toBeNull();
        expect(applyChatBubblesSnapshot(false, true)).toBeNull();
        expect(applyChatBubblesSnapshot(undefined, false)).toBeNull();
        expect(applyChatBubblesSnapshot(null, null)).toBeNull();
    });

    it("accepts a snapshot that matches the draft or has no draft", () => {
        expect(applyChatBubblesSnapshot(false, null)).toEqual({ enabled: false, clearDraft: false });
        expect(applyChatBubblesSnapshot(true, null)).toEqual({ enabled: true, clearDraft: false });
        expect(applyChatBubblesSnapshot(false, false)).toEqual({ enabled: false, clearDraft: true });
    });

    it("drops a read that started before a newer choice or echo", () => {
        expect(chatBubblesFromRead(true, null, 1, 2)).toBeNull();
        expect(chatBubblesFromRead(false, null, 0, 1)).toBeNull();
        expect(chatBubblesFromRead(false, null, 2, 2)).toEqual({ enabled: false, clearDraft: false });
        expect(chatBubblesFromRead(true, false, 2, 2)).toBeNull();
    });

    it("keeps the draft until the save of the shown value has started", () => {
        expect(chatBubblesEchoClearsDraft(false, false, false)).toBe(false);
        expect(chatBubblesEchoClearsDraft(false, false, true)).toBe(true);
        expect(chatBubblesEchoClearsDraft(true, false, true)).toBe(false);
        expect(chatBubblesEchoClearsDraft(false, null, true)).toBe(false);
    });
});

describe("publishChatBubbles", () => {
    it("does not write a value that was replaced before its turn", async () => {
        resetChatBubblesSaveQueue();
        const pending: Array<{ enabled: boolean; done: () => void }> = [];
        const save = (enabled: boolean) => new Promise<void>((done) => {
            pending.push({ enabled, done });
        });
        const draft = { current: null as boolean | null };
        const shown: boolean[] = [];
        const publish = (enabled: boolean) => publishChatBubbles(enabled, draft, (value) => shown.push(value), save, async () => true);

        publish(false);
        publish(true);
        publish(false);
        expect(pending.map((item) => item.enabled)).toEqual([false]);
        expect(shown).toEqual([false, true, false]);
        expect(draft.current).toBe(false);

        pending[0].done();
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(pending.map((item) => item.enabled)).toEqual([false, false]);
        expect(draft.current).toBe(false);
        resetChatBubblesSaveQueue();
    });
});

function ruleBody(css: string, selector: string): string {
    let from = 0;
    while (from < css.length) {
        const start = css.indexOf(selector, from);
        if (start < 0) return "";
        const next = css[start + selector.length];
        from = start + selector.length;
        if (next !== "{" && next !== "," && next !== " " && next !== "\n") continue;
        const open = css.indexOf("{", from);
        const end = open < 0 ? -1 : css.indexOf("}", open);
        if (open < 0 || end < 0) return "";
        return css.slice(open + 1, end);
    }
    return "";
}

describe("assistant bubble frame stylesheet", () => {
    it("hides the frame and keeps the text measure when the setting is off", () => {
        const pet = readFileSync(resolve(partials, "100-pet-store-mc.css"), "utf8");
        const responsive = readFileSync(resolve(partials, "120-app-responsive.css"), "utf8");
        const user = ".mc-conversation-body [data-testid^='assistant-chat-user-bubble-']";
        const ai = ".mc-conversation-body [data-testid^='assistant-chat-ai-bubble-']";
        const off = ":not([data-chat-bubble='off'])";

        expect(ruleBody(pet, user)).toContain("padding:");
        expect(ruleBody(pet, user)).not.toContain("background:");
        expect(ruleBody(pet, `${user}${off}`)).toContain("background:");
        expect(ruleBody(pet, `${user}${off}`)).not.toContain("padding:");
        expect(ruleBody(pet, ai)).toContain("padding:");
        expect(ruleBody(pet, `${ai}${off}`)).toContain("border:");
        expect(ruleBody(pet, `${ai}${off}`)).toContain("box-shadow:");

        expect(ruleBody(responsive, `#App ${user}`)).toMatch(/(^|\n)\s*color:/);
        expect(ruleBody(responsive, `#App ${user}`)).not.toContain("background:");
        expect(ruleBody(responsive, `#App ${user}${off}`)).toContain("background:");
        expect(ruleBody(responsive, `#App ${user}${off}`)).not.toMatch(/(^|\n)\s*color:/);
        expect(ruleBody(responsive, `#App ${ai}${off}`)).toContain("box-shadow:");
    });
});
