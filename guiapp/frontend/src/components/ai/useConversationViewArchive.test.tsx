import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AIAssistantAttachmentPreviewDataURL, ConversationViewArchiveCount, LoadAIAssistantUIArchive, LoadProjectTabConversationArchive } from "../../../wailsjs/go/main/App";
import { lightTheme, type Theme } from "./aiAssistantPanelTheme";
import type { ChatMessage } from "./useAIAssistant";
import { CONVERSATION_ARCHIVE_RENDER_PAGE, reuseArchiveRows, useConversationViewArchive } from "./useConversationViewArchive";

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
        await waitFor(() => expect(document.activeElement).toBe(dialog));
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
        let resolveDesktop: (value: unknown) => void = () => {};
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
        let resolveDesktop: (value: unknown) => void = () => {};
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
});
