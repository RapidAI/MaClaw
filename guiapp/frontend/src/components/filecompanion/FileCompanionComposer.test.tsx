// @vitest-environment jsdom
import { useState } from "react";
import { beforeEach, describe, expect, it } from "vitest";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { FileCompanionComposer } from "./FileCompanionComposer";
import { FILE_COMPANION_INPUT_HISTORY_KEY, rememberFileCompanionInput } from "./fileCompanionInputHistory";

const ASSISTANT_PROMPT_HISTORY_KEY = "ai-assistant-prompt-history";

function Harness({ path = "a.pdf", busy = false }: { path?: string; busy?: boolean }) {
    const [value, setValue] = useState("");
    const [sent, setSent] = useState<string[]>([]);
    return (
        <>
            <FileCompanionComposer
                path={path}
                value={value}
                busy={busy}
                onChange={setValue}
                onSubmit={() => {
                    setSent((prev) => [...prev, value.trim()]);
                    setValue("");
                }}
            />
            <div data-testid="sent">{sent.join("|")}</div>
        </>
    );
}

function input(): HTMLTextAreaElement {
    return screen.getByTestId("file-companion-input") as HTMLTextAreaElement;
}

beforeEach(() => {
    localStorage.clear();
});

describe("FileCompanionComposer", () => {
    it("walks sent questions with the arrow keys and restores the draft on Escape", async () => {
        render(<Harness />);
        fireEvent.change(input(), { target: { value: "文件里有什么" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        fireEvent.change(input(), { target: { value: "再总结一下" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        expect(screen.getByTestId("sent").textContent).toBe("文件里有什么|再总结一下");

        fireEvent.change(input(), { target: { value: "草稿" } });
        input().setSelectionRange(2, 2);
        fireEvent.keyDown(input(), { key: "ArrowUp" });
        await waitFor(() => expect(input().value).toBe("再总结一下"));
        input().setSelectionRange(input().value.length, input().value.length);
        fireEvent.keyDown(input(), { key: "ArrowUp" });
        await waitFor(() => expect(input().value).toBe("文件里有什么"));
        fireEvent.keyDown(input(), { key: "Escape" });
        await waitFor(() => expect(input().value).toBe("草稿"));
    });

    it("completes a prefix without sending", async () => {
        render(<Harness />);
        fireEvent.change(input(), { target: { value: "文件里有什么" } });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        fireEvent.change(input(), { target: { value: "文件里" } });
        const list = await screen.findByTestId("file-companion-input-history");
        expect(list.textContent).toContain("文件里有什么");
        fireEvent.keyDown(input(), { key: "Enter" });
        await waitFor(() => expect(input().value).toBe("文件里有什么"));
        expect(screen.getByTestId("sent").textContent).toBe("文件里有什么");
        expect(screen.queryByTestId("file-companion-input-history")).toBeNull();
    });

    it("ignores the assistant prompt history", async () => {
        localStorage.setItem(ASSISTANT_PROMPT_HISTORY_KEY, JSON.stringify(["助手里的一句"]));
        rememberFileCompanionInput("文件里有什么");
        render(<Harness />);
        input().setSelectionRange(0, 0);
        fireEvent.keyDown(input(), { key: "ArrowUp" });
        await waitFor(() => expect(input().value).toBe("文件里有什么"));
        fireEvent.change(input(), { target: { value: "助手" } });
        expect(screen.queryByTestId("file-companion-input-history")).toBeNull();
        expect(JSON.parse(localStorage.getItem(ASSISTANT_PROMPT_HISTORY_KEY) || "[]")).toEqual(["助手里的一句"]);
        expect(localStorage.getItem(FILE_COMPANION_INPUT_HISTORY_KEY)).toContain("文件里有什么");
    });

    it("puts the draft back on the file being left", async () => {
        rememberFileCompanionInput("旧问题");
        function TwoFiles() {
            const [file, setFile] = useState("a.pdf");
            const [drafts, setDrafts] = useState<Record<string, string>>({ "a.pdf": "", "b.pdf": "" });
            return (
                <>
                    <FileCompanionComposer
                        path={file}
                        value={drafts[file] ?? ""}
                        onChange={(next) => setDrafts((current) => ({ ...current, [file]: next }))}
                        onSubmit={() => undefined}
                    />
                    <button type="button" data-testid="switch-file" onClick={() => setFile("b.pdf")}>switch</button>
                    <div data-testid="draft-a">{drafts["a.pdf"]}</div>
                    <div data-testid="draft-b">{drafts["b.pdf"]}</div>
                </>
            );
        }
        render(<TwoFiles />);
        fireEvent.change(input(), { target: { value: "草稿" } });
        input().setSelectionRange(2, 2);
        fireEvent.keyDown(input(), { key: "ArrowUp" });
        await waitFor(() => expect(input().value).toBe("旧问题"));
        fireEvent.click(screen.getByTestId("switch-file"));
        await waitFor(() => expect(screen.getByTestId("draft-a").textContent).toBe("草稿"));
        expect(input().value).toBe("");
        expect(screen.getByTestId("draft-b").textContent).toBe("");
    });

    it("keeps another file's history walk when a late accept lands", async () => {
        rememberFileCompanionInput("旧问题");
        let resolveSend: (value: boolean) => void = () => {};
        function TwoFiles() {
            const [file, setFile] = useState("a.pdf");
            const [drafts, setDrafts] = useState<Record<string, string>>({ "a.pdf": "请改写", "b.pdf": "" });
            return (
                <>
                    <FileCompanionComposer
                        path={file}
                        value={drafts[file] ?? ""}
                        onChange={(next) => setDrafts((current) => ({ ...current, [file]: next }))}
                        onSubmit={() => new Promise<boolean>((resolve) => { resolveSend = resolve; })}
                    />
                    <button type="button" data-testid="switch-file" onClick={() => setFile("b.pdf")}>switch</button>
                    <div data-testid="draft-b">{drafts["b.pdf"]}</div>
                </>
            );
        }
        render(<TwoFiles />);
        fireEvent.click(screen.getByTestId("file-companion-send"));
        fireEvent.click(screen.getByTestId("switch-file"));
        input().setSelectionRange(0, 0);
        fireEvent.keyDown(input(), { key: "ArrowUp" });
        await waitFor(() => expect(input().value).toBe("旧问题"));
        await act(async () => { resolveSend(true); });
        await waitFor(() => expect(localStorage.getItem(FILE_COMPANION_INPUT_HISTORY_KEY)).toContain("请改写"));
        fireEvent.keyDown(input(), { key: "Escape" });
        await waitFor(() => expect(input().value).toBe(""));
        expect(screen.getByTestId("draft-b").textContent).toBe("");
    });

    it("does not store a question the host refuses", async () => {
        function Refusing() {
            const [value, setValue] = useState("请改写");
            return (
                <FileCompanionComposer
                    path="a.pdf"
                    value={value}
                    onChange={setValue}
                    onSubmit={async () => false}
                />
            );
        }
        render(<Refusing />);
        fireEvent.click(screen.getByTestId("file-companion-send"));
        await act(async () => {
            await Promise.resolve();
            await Promise.resolve();
        });
        expect(input().value).toBe("请改写");
        expect(localStorage.getItem(FILE_COMPANION_INPUT_HISTORY_KEY)).toBeNull();
    });

    it("does not store a question while a turn is in flight", () => {
        render(<Harness busy />);
        fireEvent.change(input(), { target: { value: "先别发出去" } });
        fireEvent.keyDown(input(), { key: "Enter" });
        fireEvent.click(screen.getByTestId("file-companion-send"));
        expect(screen.getByTestId("sent").textContent).toBe("");
        expect(input().value).toBe("先别发出去");
        expect(localStorage.getItem(FILE_COMPANION_INPUT_HISTORY_KEY)).toBeNull();
    });
});
