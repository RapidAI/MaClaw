import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AIAssistantAttachmentPreviewDataURL, ConversationViewArchiveCount, LoadAIAssistantUIArchive, LoadProjectTabConversationArchive } from "../../../wailsjs/go/main/App";
import { lightTheme, type Theme } from "./aiAssistantPanelTheme";
import type { ChatMessage } from "./useAIAssistant";
import { archiveMessageView, CONVERSATION_ARCHIVE_RENDER_PAGE, foldWindowIntoArchiveRows, reuseArchiveRows, useConversationViewArchive } from "./useConversationViewArchive";

vi.mock("../../../wailsjs/go/main/App", () => ({
    AIAssistantAttachmentPreviewDataURL: vi.fn().mockResolvedValue("data:image/png;base64,abc"),
    ConversationViewArchiveCount: vi.fn().mockResolvedValue(0),
    LoadAIAssistantUIArchive: vi.fn().mockResolvedValue([]),
    LoadProjectTabConversationArchive: vi.fn().mockResolvedValue([]),
}));

const theme = { titleBarBorder: "#ccc", fieldBg: "#fff", textMuted: "#666", bg: "#fff", text: "#111" } as Theme;

function row(id: string, extra: Partial<ChatMessage> = {}): ChatMessage {
    return { id, role: "user", content: id, timestamp: 1, ...extra };
}

function Probe({ messages, scope = "desktop", theme: probeTheme = theme }: { messages: ChatMessage[]; scope?: string; theme?: Theme }) {
    const view = useConversationViewArchive({ messages, scope, enabled: true, lang: "zh-Hans", theme: probeTheme });
    return (
        <div>
            <div data-testid="visible-count">{view.visibleMessages.length}</div>
            <div data-testid="earlier-count">{view.earlierCount}</div>
            {view.banner}
            {view.dialog}
        </div>
    );
}

describe("useConversationViewArchive", () => {
    beforeEach(() => {
        vi.mocked(AIAssistantAttachmentPreviewDataURL).mockReset().mockResolvedValue("data:image/png;base64,abc");
        vi.mocked(ConversationViewArchiveCount).mockReset().mockResolvedValue(0);
        vi.mocked(LoadAIAssistantUIArchive).mockReset().mockResolvedValue([]);
        vi.mocked(LoadProjectTabConversationArchive).mockReset().mockResolvedValue([]);
    });

    it("mounts only the hot tail and offers the earlier rows", async () => {
        const messages = Array.from({ length: 45 }, (_, index) => row(`m${index}`, { content: `body ${index}` }));
        render(<Probe messages={messages} />);
        expect(screen.getByTestId("visible-count").textContent).toBe("40");
        expect(screen.getByTestId("earlier-count").textContent).toBe("5");
        expect(screen.getByTestId("conversation-archive-banner").textContent).toContain("查看更早的 5 条对话");
        await waitFor(() => expect(ConversationViewArchiveCount).toHaveBeenCalledWith("desktop"));
    });

    it("uses the disk archive count when it is larger than the in-memory prefix", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(12);
        render(<Probe messages={[row("m1")]} />);
        await waitFor(() => expect(screen.getByTestId("conversation-archive-banner").textContent).toContain("12"));
    });

    it("opens a viewer and skips rows already on screen", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(2);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old", role: "assistant", content: "archived body", sessionKey: "desktop-user" },
            { id: "m1", role: "user", content: "already visible", sessionKey: "" },
            { id: "proj", role: "user", content: "project row", sessionKey: "desktop-user:D:/tasks/one" },
        ] as never);
        render(<Probe messages={[row("m1", { content: "already visible" })]} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        expect(await screen.findByText("archived body")).toBeTruthy();
        expect(screen.queryByText("project row")).toBeNull();
        expect(screen.getAllByTestId("conversation-archive-message")).toHaveLength(1);
        fireEvent.keyDown(window, { key: "Escape" });
        await waitFor(() => expect(screen.queryByTestId("conversation-archive-dialog")).toBeNull());
    });

    it("renders earlier assistant text with the chat markdown pipeline", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(2);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old-ai", role: "assistant", content: "结论是 **重点**\n\n```ts\nconst ok = true;\n```", sessionKey: "desktop-user" },
            { id: "old-user", role: "user", content: "请保留 **星号**", sessionKey: "desktop-user" },
        ] as never);
        render(<Probe messages={[row("hot")]} theme={lightTheme} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        const dialog = await screen.findByTestId("conversation-archive-dialog");
        expect(dialog.querySelector("strong")?.textContent).toBe("重点");
        expect(dialog.querySelector("[data-assistant-code]")).toBeTruthy();
        expect(dialog.textContent).not.toContain("**重点**");
        expect(dialog.textContent).not.toContain("```");
        expect(dialog.textContent).toContain("请保留 **星号**");
    });

    it("hides host prompt blocks and settled tool transcripts", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(2);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            {
                id: "old-user",
                role: "user",
                content: "看看这张图\n\n[用户选择的本地文件路径]\nC:\\Users\\demo\\shot.png\nFor image files, the host sends them directly to a vision-capable model when available. Analyze attached images first; do not re-capture them or use read_file on image bytes. Use OCR only for exact text when needed.",
                sessionKey: "desktop-user",
            },
            {
                id: "old-ai",
                role: "assistant",
                content: "草稿过程\n\n<!--maclaw-tool:call-1-->\n\n**最终结论**已写好\n\n## 执行报告\nsecret audit",
                sessionKey: "desktop-user",
                toolCalls: [{ id: "call-1", name: "bash", action: "执行命令", detail: "echo" }],
            },
        ] as never);
        render(<Probe messages={[row("hot")]} theme={lightTheme} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        const dialog = await screen.findByTestId("conversation-archive-dialog");
        expect(dialog.textContent).toContain("看看这张图");
        const preview = await screen.findByTestId("attachment-image-thumbnail");
        expect(preview.getAttribute("aria-label")).toContain("shot.png");
        expect(dialog.textContent).not.toContain("用户选择的本地文件路径");
        expect(dialog.textContent).not.toContain("C:\\Users\\demo\\shot.png");
        expect(dialog.textContent).not.toContain("vision-capable");
        expect(dialog.querySelector("strong")?.textContent).toBe("最终结论");
        expect(dialog.textContent).not.toContain("草稿过程");
        expect(dialog.textContent).not.toContain("secret audit");
        expect(dialog.textContent).not.toContain("maclaw-tool");
    });

    it("renders a settled thinking trail only after it is opened", async () => {
        const answer = "这是已经完成的正式回答，长度足够留在正文里。".repeat(4);
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            {
                id: "old-ai",
                role: "assistant",
                content: answer,
                reasoning: "先核对资料，再给出 **要点**。",
                sessionKey: "desktop-user",
            },
        ] as never);
        render(<Probe messages={[row("hot")]} theme={lightTheme} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        const dialog = await screen.findByTestId("conversation-archive-dialog");
        expect(dialog.textContent).toContain("这是已经完成的正式回答");
        expect(dialog.textContent).not.toContain("先核对资料");
        fireEvent.click(screen.getByTestId("conversation-archive-reasoning"));
        expect(dialog.querySelector("strong")?.textContent).toBe("要点");
    });

    it("shows a short reasoning-only reply as the message", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old-ai", role: "assistant", content: "", reasoning: "只有一段简短思路。", sessionKey: "desktop-user" },
        ] as never);
        render(<Probe messages={[row("hot")]} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        expect(await screen.findByText("只有一段简短思路。")).toBeTruthy();
        expect(screen.queryByTestId("conversation-archive-reasoning")).toBeNull();
    });

    it("mounts one page until the user asks for more", async () => {
        const total = CONVERSATION_ARCHIVE_RENDER_PAGE + 3;
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(total);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue(Array.from({ length: total }, (_, index) => ({
            id: `old-${index}`,
            role: "user",
            content: `earlier row ${index}`,
            sessionKey: "desktop-user",
        })) as never);
        render(<Probe messages={[row("hot")]} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        await screen.findByText("earlier row 0");
        expect(screen.getAllByTestId("conversation-archive-message")).toHaveLength(CONVERSATION_ARCHIVE_RENDER_PAGE);
        expect(screen.queryByText(`earlier row ${CONVERSATION_ARCHIVE_RENDER_PAGE}`)).toBeNull();
        fireEvent.click(screen.getByTestId("conversation-archive-more"));
        expect(screen.getAllByTestId("conversation-archive-message")).toHaveLength(total);
        expect(screen.queryByTestId("conversation-archive-more")).toBeNull();
    });

    it("keeps already shown pages when the open archive reloads", async () => {
        const total = CONVERSATION_ARCHIVE_RENDER_PAGE + 3;
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(total);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue(Array.from({ length: total }, (_, index) => ({
            id: `old-${index}`,
            role: "user",
            content: `earlier row ${index}`,
            sessionKey: "desktop-user",
        })) as never);
        render(<Probe messages={[row("hot")]} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        await screen.findByText("earlier row 0");
        fireEvent.click(screen.getByTestId("conversation-archive-more"));
        expect(screen.getAllByTestId("conversation-archive-message")).toHaveLength(total);
        fireEvent.click(screen.getByTestId("conversation-archive-banner"));
        await waitFor(() => expect(screen.queryByText("加载中...")).toBeNull());
        expect(screen.getAllByTestId("conversation-archive-message")).toHaveLength(total);
    });

    it("shows an image-only screenshot and hides a stray capture on a text reply", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(3);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "shot", role: "assistant", content: "", thumbnailBase64: "abc", sessionKey: "desktop-user" },
            { id: "text", role: "assistant", content: "正文还在", thumbnailBase64: "zzz", sessionKey: "desktop-user" },
            { id: "saved", role: "assistant", content: "", localFilePath: "C:\\Users\\demo\\screen.png", sessionKey: "desktop-user" },
        ] as never);
        render(<Probe messages={[row("hot")]} theme={lightTheme} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        await waitFor(() => expect(screen.getAllByTestId("screenshot-preview-block")).toHaveLength(2));
        expect(screen.getByText("正文还在")).toBeTruthy();
        expect(screen.getByText("screen.png")).toBeTruthy();
        expect(screen.queryByText("（无文本）")).toBeNull();
        expect(screen.getByTestId("conversation-archive-dialog").textContent).not.toContain("C:\\Users\\demo\\screen.png");
    });

    it("shows a saved image that the path list omitted and hides a field-only capture", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(3);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            {
                id: "split",
                role: "assistant",
                content: "",
                localFilePath: "C:\\Users\\demo\\shot.png",
                localFilePaths: ["C:\\Users\\demo\\report.pdf"],
                sessionKey: "desktop-user",
            },
            {
                id: "turn",
                role: "assistant",
                content: "",
                thumbnailBase64: "LEAK",
                fields: [{ label: "Turn", value: "fast · primary" }],
                sessionKey: "desktop-user",
            },
            {
                id: "echo",
                role: "assistant",
                content: "",
                thumbnailBase64: "LEAK",
                fields: [{ label: "Browser", value: "内部诊断" }],
                sessionKey: "desktop-user",
            },
        ] as never);
        render(<Probe messages={[row("hot")]} theme={lightTheme} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        await waitFor(() => expect(screen.getAllByTestId("screenshot-preview-block")).toHaveLength(1));
        const dialog = screen.getByTestId("conversation-archive-dialog");
        expect(dialog.textContent).toContain("shot.png");
        expect(dialog.textContent).toContain("report.pdf");
        expect(dialog.textContent).toContain("Turn");
        expect(dialog.textContent).toContain("fast · primary");
        expect(dialog.textContent).not.toContain("内部诊断");
        expect(dialog.textContent).not.toContain("LEAK");
    });

    it("does not reuse a row whose field value changed", () => {
        const kept = row("kept", { role: "assistant", content: "same", fields: [{ label: "Turn", value: "before" }] });
        const next = { ...kept, fields: [{ label: "Turn", value: "after" }] };
        expect(reuseArchiveRows([kept], [next])[0]).toBe(next);
    });

    it("does not reuse a row whose screenshot bytes changed", () => {
        const kept = row("kept", { role: "assistant", content: "", thumbnailBase64: "aaa" });
        const replaced = { ...kept, thumbnailBase64: "bbb" };
        expect(reuseArchiveRows([kept], [replaced])[0]).toBe(replaced);
        const renamedPath = { ...kept, thumbnailBase64: "aaa", localFilePaths: ["C:/b.pdf"] };
        const previousPath = { ...kept, thumbnailBase64: "aaa", localFilePaths: ["C:/a.pdf"] };
        expect(reuseArchiveRows([previousPath], [renamedPath])[0]).toBe(renamedPath);
    });

    it("does not reuse a row whose attachment name changed", () => {
        const kept = row("kept", {
            content: "看图",
            attachments: [{ filePath: "C:/a.png", fileName: "a.png", extension: ".png", isImage: true }],
        });
        const renamed = {
            ...kept,
            attachments: [{ filePath: "C:/b.png", fileName: "b.png", extension: ".png", isImage: true }],
        };
        expect(reuseArchiveRows([kept], [renamed])[0]).toBe(renamed);
    });

    it("reuses unchanged rows when a new archive row is appended", () => {
        const kept = row("kept", { content: "same body" });
        const added = row("added", { content: "new body" });
        const reloaded = { ...kept };
        const merged = reuseArchiveRows([kept], [reloaded, added]);
        expect(merged[0]).toBe(kept);
        expect(merged[1]).toBe(added);
    });

    it("keeps one row when the archive repeats an id", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(2);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old", role: "user", content: "once", sessionKey: "desktop-user" },
            { id: "old", role: "user", content: "twice", sessionKey: "desktop-user" },
        ] as never);
        render(<Probe messages={[row("hot")]} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        expect(await screen.findByText("once")).toBeTruthy();
        expect(screen.queryByText("twice")).toBeNull();
        expect(screen.getAllByTestId("conversation-archive-message")).toHaveLength(1);
    });

    it("moves focus into the dialog and restores it on close", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old", role: "user", content: "earlier", sessionKey: "desktop-user" },
        ] as never);
        render(<Probe messages={[row("hot")]} />);
        const banner = await screen.findByTestId("conversation-archive-banner");
        banner.focus();
        fireEvent.click(banner);
        const dialog = await screen.findByTestId("conversation-archive-dialog");
        expect(dialog.hasAttribute("data-scroll-intent-boundary")).toBe(true);
        expect(dialog.hasAttribute("data-nested-scroll")).toBe(false);
        await waitFor(() => expect(document.activeElement).toBe(dialog));
        const frameWheel = new WheelEvent("wheel", { deltaY: -30, bubbles: true, cancelable: true });
        dialog.dispatchEvent(frameWheel);
        expect(frameWheel.defaultPrevented).toBe(true);
        const listWheel = new WheelEvent("wheel", { deltaY: -30, bubbles: true, cancelable: true });
        screen.getByTestId("conversation-archive-scroller").dispatchEvent(listWheel);
        expect(listWheel.defaultPrevented).toBe(false);
        fireEvent.keyDown(window, { key: "Escape" });
        await waitFor(() => expect(screen.queryByTestId("conversation-archive-dialog")).toBeNull());
        expect(document.activeElement).toBe(banner);
    });

    it("does not walk every page when the scroller has no layout", async () => {
        class AlwaysVisibleObserver {
            readonly root = null;
            readonly rootMargin = "";
            readonly thresholds = [] as number[];
            constructor(private readonly callback: IntersectionObserverCallback) {}
            observe() {
                this.callback([{ isIntersecting: true } as IntersectionObserverEntry], this as unknown as IntersectionObserver);
            }
            unobserve() {}
            disconnect() {}
            takeRecords() { return []; }
        }
        vi.stubGlobal("IntersectionObserver", AlwaysVisibleObserver);
        try {
            const total = CONVERSATION_ARCHIVE_RENDER_PAGE + 3;
            vi.mocked(ConversationViewArchiveCount).mockResolvedValue(total);
            vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue(Array.from({ length: total }, (_, index) => ({
                id: `old-${index}`,
                role: "user",
                content: `earlier row ${index}`,
                sessionKey: "desktop-user",
            })) as never);
            render(<Probe messages={[row("hot")]} />);
            fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
            await screen.findByText("earlier row 0");
            expect(screen.getAllByTestId("conversation-archive-message")).toHaveLength(CONVERSATION_ARCHIVE_RENDER_PAGE);
        } finally {
            vi.unstubAllGlobals();
        }
    });

    it("drops a row that moved into the live transcript while the archive was loading", async () => {
        let resolveDesktop: (value: Record<string, any>[] | PromiseLike<Record<string, any>[]>) => void = () => {};
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadAIAssistantUIArchive).mockImplementation(() => new Promise((resolve) => {
            resolveDesktop = resolve;
        }));
        const { rerender } = render(<Probe messages={[row("hot")]} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        rerender(<Probe messages={[row("hot"), row("old", { content: "now on screen", sessionKey: "desktop-user" })]} />);
        await act(async () => {
            resolveDesktop([{ id: "old", role: "user", content: "archived copy", sessionKey: "desktop-user" }]);
        });
        expect(screen.queryByText("archived copy")).toBeNull();
        expect(await screen.findByText("没有更早的对话")).toBeTruthy();
    });

    it("keeps an opened thinking trail mounted when the same archive reloads", async () => {
        const answer = "这是已经完成的正式回答，长度足够留在正文里。".repeat(4);
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            {
                id: "old-ai",
                role: "assistant",
                content: answer,
                reasoning: "先核对资料，再给出 **要点**。",
                sessionKey: "desktop-user",
            },
        ] as never);
        render(<Probe messages={[row("hot")]} theme={lightTheme} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        fireEvent.click(await screen.findByTestId("conversation-archive-reasoning"));
        expect(screen.getByText("要点").tagName).toBe("STRONG");
        fireEvent.click(screen.getByTestId("conversation-archive-banner"));
        await waitFor(() => expect(screen.queryByText("加载中...")).toBeNull());
        expect(screen.getByText("要点").tagName).toBe("STRONG");
    });

    it("ignores an archive load that finishes after the scope changes", async () => {
        let resolveDesktop: (value: Record<string, any>[] | PromiseLike<Record<string, any>[]>) => void = () => {};
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadAIAssistantUIArchive).mockImplementation(() => new Promise((resolve) => {
            resolveDesktop = resolve;
        }));
        vi.mocked(LoadProjectTabConversationArchive).mockResolvedValue([
            { id: "tab-row", role: "user", content: "tab body" },
        ] as never);
        const { rerender } = render(<Probe messages={[row("hot")]} />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        rerender(<Probe messages={[row("hot")]} scope="tab:proj-1" />);
        await waitFor(() => expect(screen.queryByTestId("conversation-archive-dialog")).toBeNull());
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        expect(await screen.findByText("tab body")).toBeTruthy();
        await act(async () => {
            resolveDesktop([{ id: "stale", role: "assistant", content: "stale desktop", sessionKey: "desktop-user" }]);
        });
        expect(screen.queryByText("stale desktop")).toBeNull();
        expect(screen.getByText("tab body")).toBeTruthy();
    });

    it("loads a project tab archive by tab id", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadProjectTabConversationArchive).mockResolvedValue([
            { id: "tab-old", role: "user", content: "tab archive" },
        ] as never);
        render(<Probe messages={[row("hot")]} scope="tab:proj-1" />);
        fireEvent.click(await screen.findByTestId("conversation-archive-banner"));
        expect(await screen.findByText("tab archive")).toBeTruthy();
        expect(LoadProjectTabConversationArchive).toHaveBeenCalledWith("proj-1");
    });

    it("hides a goal continuation header in an archived assistant reply", () => {
        const header = "[系统续接][execute] 按步骤执行目标：开发 c++ 贪吃蛇游戏";
        const view = archiveMessageView({
            id: "archived-goal",
            role: "assistant",
            content: `${header}\n\n本轮只能提交步骤 4：实现绘制与界面。`,
            timestamp: 1,
        });
        expect(view.body).toBe("本轮只能提交步骤 4：实现绘制与界面。");
        expect(view.body).not.toContain("[系统续接]");
    });

    it("loads earlier rows for the locator without opening the viewer", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old-user", role: "user", content: "更早的问题", sessionKey: "desktop-user" },
        ] as never);
        const hot = Array.from({ length: 40 }, (_, index) => row(`h${index}`, { content: `hot ${index}` }));
        function WarmProbe() {
            const view = useConversationViewArchive({ messages: [row("early", { content: "更早" }), ...hot], scope: "desktop", enabled: true, lang: "zh-Hans", theme });
            return (
                <div>
                    <button type="button" data-testid="warm" onClick={() => view.ensureArchiveRows()}>warm</button>
                    <div data-testid="locator-ids">{view.locatorMessages.map((message) => message.id).join(",")}</div>
                    {view.dialog}
                </div>
            );
        }
        render(<WarmProbe />);
        fireEvent.click(screen.getByTestId("warm"));
        await waitFor(() => expect(screen.getByTestId("locator-ids").textContent).toContain("old-user,early,"));
        expect(screen.queryByTestId("conversation-archive-dialog")).toBeNull();
        expect(LoadAIAssistantUIArchive).toHaveBeenCalledTimes(1);
        fireEvent.click(screen.getByTestId("warm"));
        expect(LoadAIAssistantUIArchive).toHaveBeenCalledTimes(1);
    });

    it("reveals a command that left the hot window after the archive was read", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old-user", role: "user", content: "更早的问题", sessionKey: "desktop-user" },
        ] as never);
        const hot = Array.from({ length: 40 }, (_, index) => row(`h${index}`, { content: `hot ${index}` }));
        const initial = [row("early", { content: "更早" }), ...hot];
        function SlideProbe({ messages }: { messages: ChatMessage[] }) {
            const view = useConversationViewArchive({ messages, scope: "desktop", enabled: true, lang: "zh-Hans", theme });
            return (
                <div>
                    <button type="button" data-testid="warm" onClick={() => view.ensureArchiveRows()}>warm</button>
                    <button type="button" data-testid="reveal" onClick={() => view.revealArchiveMessage("h0")}>reveal</button>
                    <div data-testid="locator-ids">{view.locatorMessages.map((message) => message.id).join(",")}</div>
                    {view.dialog}
                </div>
            );
        }
        const { rerender } = render(<SlideProbe messages={initial} />);
        fireEvent.click(screen.getByTestId("warm"));
        await waitFor(() => expect(screen.getByTestId("locator-ids").textContent).toContain("old-user,early,h0,"));
        rerender(<SlideProbe messages={[...initial, row("extra"), row("newest", { content: "newest" })]} />);
        fireEvent.click(screen.getByTestId("reveal"));
        const rowNode = await screen.findByText("hot 0");
        expect(rowNode.closest("[data-locator-turn]")?.getAttribute("data-locator-turn")).toBe("h0");
        expect(LoadAIAssistantUIArchive).toHaveBeenCalledTimes(1);
    });

    it("drops warmed archive rows when the transcript is cleared on the same scope", async () => {
        const counts: Array<(count: number) => void> = [];
        vi.mocked(ConversationViewArchiveCount).mockImplementation(() => new Promise((resolve) => {
            counts.push(resolve);
        }));
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old-user", role: "user", content: "更早的问题", sessionKey: "desktop-user" },
        ] as never);
        function ClearProbe({ messages }: { messages: ChatMessage[] }) {
            const view = useConversationViewArchive({ messages, scope: "desktop", enabled: true, lang: "zh-Hans", theme });
            return (
                <div>
                    <button type="button" data-testid="warm" onClick={() => view.ensureArchiveRows()}>warm</button>
                    <button type="button" data-testid="reveal" onClick={() => view.revealArchiveMessage("old-user")}>reveal</button>
                    <div data-testid="locator-ids">{view.locatorMessages.map((message) => message.id).join(",")}</div>
                    <div data-testid="earlier-count">{view.earlierCount}</div>
                    {view.dialog}
                </div>
            );
        }
        const { rerender } = render(<ClearProbe messages={[row("hot", { content: "当前问题" })]} />);
        await act(async () => { counts[0]?.(1); });
        fireEvent.click(screen.getByTestId("warm"));
        await waitFor(() => expect(screen.getByTestId("locator-ids").textContent).toContain("old-user"));
        fireEvent.click(screen.getByTestId("reveal"));
        expect(await screen.findByTestId("conversation-archive-dialog")).toBeTruthy();
        const loads = vi.mocked(LoadAIAssistantUIArchive).mock.calls.length;
        rerender(<ClearProbe messages={[]} />);
        await act(async () => { counts[1]?.(9); });
        expect(screen.getByTestId("locator-ids").textContent).toBe("");
        expect(screen.getByTestId("earlier-count").textContent).toBe("0");
        expect(screen.queryByTestId("conversation-archive-dialog")).toBeNull();
        fireEvent.click(screen.getByTestId("warm"));
        expect(LoadAIAssistantUIArchive).toHaveBeenCalledTimes(loads);
        rerender(<ClearProbe messages={[row("fresh", { content: "新问题" })]} />);
        await act(async () => { counts[counts.length - 1]?.(0); });
        expect(screen.getByTestId("locator-ids").textContent).toBe("fresh");
        expect(screen.getByTestId("earlier-count").textContent).toBe("0");
    });

    it("discards an archive read that finishes after the transcript is cleared", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        let resolveArchive: ((rows: any) => void) | null = null;
        vi.mocked(LoadAIAssistantUIArchive).mockImplementation(() => new Promise((resolve) => {
            resolveArchive = resolve;
        }));
        function ClearProbe({ messages }: { messages: ChatMessage[] }) {
            const view = useConversationViewArchive({ messages, scope: "desktop", enabled: true, lang: "zh-Hans", theme });
            return (
                <div>
                    <button type="button" data-testid="warm" onClick={() => view.ensureArchiveRows()}>warm</button>
                    <div data-testid="locator-ids">{view.locatorMessages.map((message) => message.id).join(",")}</div>
                    <div data-testid="earlier-count">{view.earlierCount}</div>
                </div>
            );
        }
        const { rerender } = render(<ClearProbe messages={[row("hot")]} />);
        await waitFor(() => expect(screen.getByTestId("earlier-count").textContent).toBe("1"));
        fireEvent.click(screen.getByTestId("warm"));
        expect(LoadAIAssistantUIArchive).toHaveBeenCalledTimes(1);
        rerender(<ClearProbe messages={[]} />);
        await act(async () => {
            resolveArchive?.([{ id: "old-user", role: "user", content: "更早的问题", sessionKey: "desktop-user" }]);
        });
        expect(screen.getByTestId("locator-ids").textContent).toBe("");
        expect(screen.getByTestId("earlier-count").textContent).toBe("0");
    });

    it("loads a scope that opens empty instead of treating the switch as a clear", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadProjectTabConversationArchive).mockResolvedValue([
            { id: "tab-old", role: "user", content: "tab archive" },
        ] as never);
        function ScopeProbe({ messages, scope }: { messages: ChatMessage[]; scope: string }) {
            const view = useConversationViewArchive({ messages, scope, enabled: true, lang: "zh-Hans", theme });
            return (
                <div>
                    <button type="button" data-testid="warm" onClick={() => view.ensureArchiveRows()}>warm</button>
                    <div data-testid="locator-ids">{view.locatorMessages.map((message) => message.id).join(",")}</div>
                    <div data-testid="earlier-count">{view.earlierCount}</div>
                </div>
            );
        }
        const { rerender } = render(<ScopeProbe messages={[row("hot")]} scope="desktop" />);
        await waitFor(() => expect(ConversationViewArchiveCount).toHaveBeenCalledWith("desktop"));
        rerender(<ScopeProbe messages={[]} scope="tab:proj-1" />);
        await waitFor(() => expect(screen.getByTestId("earlier-count").textContent).toBe("1"));
        fireEvent.click(screen.getByTestId("warm"));
        await waitFor(() => expect(screen.getByTestId("locator-ids").textContent).toContain("tab-old"));
        expect(LoadProjectTabConversationArchive).toHaveBeenCalledWith("proj-1");
    });

    it("still reads the archive count when the transcript starts empty", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(4);
        render(<Probe messages={[]} />);
        await waitFor(() => expect(screen.getByTestId("earlier-count").textContent).toBe("4"));
    });

    it("folds slid-out rows onto the archive snapshot and drops rows that are live again", () => {
        const disk = [row("disk", { content: "disk" })];
        const slid = row("h0", { content: "hot 0" });
        const live = new Set(["newest"]);
        expect(foldWindowIntoArchiveRows([], [slid], live)).toEqual([]);
        const next = foldWindowIntoArchiveRows(disk, [row("disk", { content: "disk" }), slid], live);
        expect(next.map((message) => message.id)).toEqual(["disk", "h0"]);
        expect(next[0]).toBe(disk[0]);
        expect(next[1]).toBe(slid);
        expect(foldWindowIntoArchiveRows(next, [slid], live)).toBe(next);
        const back = foldWindowIntoArchiveRows(next, [], new Set(["h0"]));
        expect(back.map((message) => message.id)).toEqual(["disk"]);
    });

    it("opens the archive on the requested earlier user row", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old-user", role: "user", content: "更早的问题", sessionKey: "desktop-user" },
        ] as never);
        function RevealProbe() {
            const view = useConversationViewArchive({ messages: [row("hot")], scope: "desktop", enabled: true, lang: "zh-Hans", theme });
            return (
                <div>
                    <button type="button" data-testid="reveal" onClick={() => view.revealArchiveMessage("old-user")}>reveal</button>
                    {view.dialog}
                </div>
            );
        }
        render(<RevealProbe />);
        fireEvent.click(screen.getByTestId("reveal"));
        const rowNode = await screen.findByText("更早的问题");
        expect(rowNode.closest("[data-locator-turn]")?.getAttribute("data-locator-turn")).toBe("old-user");
    });

    it("scrolls a scaled archive row in CSS pixels", async () => {
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(1);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old-user", role: "user", content: "更早的问题", sessionKey: "desktop-user" },
        ] as never);
        const rect = (top: number, height: number): DOMRect => ({
            top, bottom: top + height, left: 0, right: 10, width: 10, height, x: 0, y: top, toJSON() { return {}; },
        });
        const originalRect = HTMLElement.prototype.getBoundingClientRect;
        const clientHeight = Object.getOwnPropertyDescriptor(Element.prototype, "clientHeight");
        HTMLElement.prototype.getBoundingClientRect = function () {
            if (this.getAttribute("data-testid") === "conversation-archive-scroller") return rect(0, 200);
            if (this.hasAttribute("data-locator-turn")) return rect(400, 40);
            return originalRect.call(this);
        };
        Object.defineProperty(Element.prototype, "clientHeight", {
            configurable: true,
            get() {
                if (this instanceof Element && this.getAttribute("data-testid") === "conversation-archive-scroller") return 100;
                return clientHeight?.get?.call(this) ?? 0;
            },
        });
        try {
            function RevealProbe() {
                const view = useConversationViewArchive({ messages: [row("hot")], scope: "desktop", enabled: true, lang: "zh-Hans", theme });
                return (
                    <div>
                        <button type="button" data-testid="reveal" onClick={() => view.revealArchiveMessage("old-user")}>reveal</button>
                        {view.dialog}
                    </div>
                );
            }
            render(<RevealProbe />);
            fireEvent.click(screen.getByTestId("reveal"));
            await screen.findByText("更早的问题");
            // 400 visual px is 200 CSS px at scale 2, then the 12px gap.
            expect(screen.getByTestId("conversation-archive-scroller").scrollTop).toBe(188);
        } finally {
            HTMLElement.prototype.getBoundingClientRect = originalRect;
            if (clientHeight) Object.defineProperty(Element.prototype, "clientHeight", clientHeight);
        }
    });

    it("mounts the archive page that contains the jumped user row", async () => {
        const total = CONVERSATION_ARCHIVE_RENDER_PAGE + 2;
        vi.mocked(ConversationViewArchiveCount).mockResolvedValue(total);
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue(Array.from({ length: total }, (_, index) => ({
            id: `old-${index}`,
            role: "user",
            content: `earlier row ${index}`,
            sessionKey: "desktop-user",
        })) as never);
        function RevealProbe() {
            const view = useConversationViewArchive({ messages: [row("hot")], scope: "desktop", enabled: true, lang: "zh-Hans", theme });
            return (
                <div>
                    <button type="button" data-testid="reveal" onClick={() => view.revealArchiveMessage(`old-${total - 1}`)}>reveal</button>
                    {view.dialog}
                </div>
            );
        }
        render(<RevealProbe />);
        fireEvent.click(screen.getByTestId("reveal"));
        expect(await screen.findByText(`earlier row ${total - 1}`)).toBeTruthy();
    });

    it("keeps locator rows stable while an assistant reply streams", () => {
        const seen: ChatMessage[][] = [];
        function StableProbe({ messages }: { messages: ChatMessage[] }) {
            const view = useConversationViewArchive({ messages, scope: "desktop", enabled: true, lang: "zh-Hans", theme });
            seen.push(view.locatorMessages);
            return null;
        }
        const user = row("u1", { content: "继续" });
        const { rerender } = render(<StableProbe messages={[user, row("a1", { role: "assistant", content: "a" })]} />);
        const stable = seen[seen.length - 1];
        expect(stable.map((message) => message.id)).toEqual(["u1"]);
        rerender(<StableProbe messages={[{ ...user }, row("a1", { role: "assistant", content: "ab" })]} />);
        expect(seen[seen.length - 1]).toBe(stable);
        rerender(<StableProbe messages={[row("u1", { content: "继续改" }), row("a1", { role: "assistant", content: "ab" })]} />);
        expect(seen[seen.length - 1]).not.toBe(stable);
        expect(seen[seen.length - 1][0]?.content).toBe("继续改");
    });

    it("loads the archive after the count arrives when the locator asked first", async () => {
        let resolveCount: (count: number) => void = () => {};
        vi.mocked(ConversationViewArchiveCount).mockImplementation(() => new Promise((resolve) => {
            resolveCount = resolve;
        }));
        vi.mocked(LoadAIAssistantUIArchive).mockResolvedValue([
            { id: "old-user", role: "user", content: "更早的问题", sessionKey: "desktop-user" },
        ] as never);
        const hot = Array.from({ length: 10 }, (_, index) => row(`h${index}`, { content: `hot ${index}` }));
        function WarmProbe() {
            const view = useConversationViewArchive({ messages: hot, scope: "desktop", enabled: true, lang: "zh-Hans", theme });
            return (
                <div>
                    <button type="button" data-testid="warm" onClick={() => view.ensureArchiveRows()}>warm</button>
                    <div data-testid="locator-ids">{view.locatorMessages.map((message) => message.id).join(",")}</div>
                </div>
            );
        }
        render(<WarmProbe />);
        fireEvent.click(screen.getByTestId("warm"));
        expect(LoadAIAssistantUIArchive).not.toHaveBeenCalled();
        await act(async () => { resolveCount(1); });
        await waitFor(() => expect(screen.getByTestId("locator-ids").textContent).toContain("old-user"));
        expect(LoadAIAssistantUIArchive).toHaveBeenCalledTimes(1);
    });
});
