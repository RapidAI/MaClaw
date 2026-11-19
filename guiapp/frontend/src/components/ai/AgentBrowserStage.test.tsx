import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";
import { AgentBrowserStage, containedImagePoint, mergeAgentBrowserStage, type AgentBrowserStageState } from "./AgentBrowserStage";

const base: AgentBrowserStageState = {
    open: true,
    sessionId: "browser-session-1",
    url: "https://example.com",
    title: "Example",
    frame: "",
    width: 1280,
    height: 800,
    userHold: false,
    askReason: "",
    seq: 1,
};

describe("containedImagePoint", () => {
    it("maps a point inside the letterboxed image to a fraction", () => {
        const point = containedImagePoint(200, 100, 100, 100, 75, 50);
        expect(point).toEqual({ x: 0.25, y: 0.5 });
    });

    it("ignores clicks in the empty bars", () => {
        expect(containedImagePoint(200, 100, 100, 100, 10, 50)).toBeNull();
    });
});

describe("mergeAgentBrowserStage", () => {
    it("keeps the previous frame when a newer event omits it", () => {
        const prev = { ...base, frame: "abc", seq: 2 };
        const next = mergeAgentBrowserStage(prev, { open: true, seq: 3, url: "https://example.com/next" });
        expect(next?.frame).toBe("abc");
        expect(next?.url).toBe("https://example.com/next");
    });

    it("drops an older sequence", () => {
        const prev = { ...base, seq: 5, title: "now" };
        expect(mergeAgentBrowserStage(prev, { open: true, seq: 4, title: "old" })?.title).toBe("now");
    });
});

describe("AgentBrowserStage", () => {
    it("offers watch and take control, and reports the choice", () => {
        const onSetHold = vi.fn();
        render(<AgentBrowserStage state={base} lang="zh-Hans" onSetHold={onSetHold} />);
        expect(screen.getByTestId("agent-browser-watch")).toBeTruthy();
        fireEvent.click(screen.getByTestId("agent-browser-take-control"));
        expect(onSetHold).toHaveBeenCalledWith(true);
        expect(screen.getByTestId("agent-browser-shield")).toBeTruthy();
    });

    it("sends a pointer fraction only while the person holds the page", () => {
        const onInput = vi.fn();
        const box = { left: 0, top: 0, right: 100, bottom: 100, width: 100, height: 100, x: 0, y: 0, toJSON() { return {}; } };
        const { rerender } = render(<AgentBrowserStage state={{ ...base, frame: "aGk=", width: 100, height: 100 }} lang="en" onInput={onInput} />);
        const img = screen.getByTestId("agent-browser-frame") as HTMLImageElement;
        img.getBoundingClientRect = () => box;
        fireEvent.pointerDown(img, { clientX: 25, clientY: 40, pointerId: 1 });
        expect(onInput).not.toHaveBeenCalled();
        rerender(<AgentBrowserStage state={{ ...base, frame: "aGk=", width: 100, height: 100, userHold: true }} lang="en" onInput={onInput} />);
        const held = screen.getByTestId("agent-browser-frame") as HTMLImageElement;
        held.getBoundingClientRect = () => box;
        fireEvent.pointerDown(held, { clientX: 25, clientY: 40, pointerId: 1 });
        expect(onInput).toHaveBeenCalled();
        const payload = JSON.parse(String(onInput.mock.calls[0][0]));
        expect(payload.kind).toBe("down");
        expect(payload.x).toBeCloseTo(0.25);
        expect(payload.y).toBeCloseTo(0.4);
    });

    it("asks the person to confirm payment before the button enables", () => {
        const onConfirm = vi.fn();
        render(<AgentBrowserStage
            state={{
                ...base,
                confirmAmount: "9.98",
                confirmItem: "椰子水饮料 1250mL×2瓶",
                confirmShop: "京喜自营官方店",
                confirmAddress: "北京海淀区上地佳园",
                askReason: "confirm",
            }}
            lang="zh-Hans"
            onConfirm={onConfirm}
        />);
        expect(screen.getByTestId("agent-browser-confirm-card").textContent).toContain("确认支付 ¥9.98");
        expect(screen.getByTestId("agent-browser-confirm-card").textContent).toContain("椰子水");
        expect(screen.getByTestId("agent-browser-confirm-card").textContent).toContain("上地佳园");
        const submit = screen.getByTestId("agent-browser-confirm-submit") as HTMLButtonElement;
        expect(submit.disabled).toBe(true);
        fireEvent.click(screen.getByTestId("agent-browser-confirm-check"));
        expect(submit.disabled).toBe(false);
        fireEvent.click(submit);
        expect(onConfirm).toHaveBeenCalledWith(true);
        expect(screen.getByTestId("agent-browser-shield")).toBeTruthy();
    });

    it("highlights the page and waits when the person must finish the step", () => {
        const onFinish = vi.fn();
        render(<AgentBrowserStage
            state={{ ...base, userHold: true, attention: true, askReason: "secret", host: "item.jd.com", frame: "aGk=" }}
            lang="zh-Hans"
            onFinish={onFinish}
        />);
        expect(screen.getByTestId("agent-browser-stage").className).toContain("is-attention");
        expect(screen.getByTestId("agent-browser-waiting").textContent).toContain("等待你操作");
        expect(screen.getByText(/item\.jd\.com/)).toBeTruthy();
        fireEvent.click(screen.getByTestId("agent-browser-attention-done"));
        expect(onFinish).toHaveBeenCalledWith(true);
        fireEvent.click(screen.getByTestId("agent-browser-attention-cancel"));
        expect(onFinish).toHaveBeenCalledWith(false);
        expect(screen.queryByTestId("agent-browser-shield")).toBeNull();
    });

    it("fills a code on the card and does not put it in the page chrome", async () => {
        const onSecret = vi.fn().mockResolvedValue(undefined);
        render(<AgentBrowserStage
            state={{ ...base, secretMethod: "otp", secretDigits: 6, secretSubmission: "enter", host: "passport.jd.com", frame: "aGk=" }}
            lang="zh-Hans"
            onSecret={onSecret}
        />);
        expect(screen.getByTestId("agent-browser-secret-card").textContent).toContain("输入验证码");
        expect(screen.getByTestId("agent-browser-secret-card").textContent).toContain("passport.jd.com");
        expect(screen.getByTestId("agent-browser-shield")).toBeTruthy();
        expect(screen.queryByTestId("agent-browser-take-control")).toBeNull();
        const input = screen.getByTestId("agent-browser-secret-input") as HTMLInputElement;
        const submit = screen.getByTestId("agent-browser-secret-submit") as HTMLButtonElement;
        expect(submit.disabled).toBe(true);
        fireEvent.change(input, { target: { value: "123456" } });
        expect(submit.disabled).toBe(false);
        expect(screen.getByTestId("agent-browser-stage").textContent).not.toContain("请在对话");
        fireEvent.click(submit);
        expect(onSecret).toHaveBeenCalledWith("123456", true);
        await waitFor(() => expect((screen.getByTestId("agent-browser-secret-input") as HTMLInputElement).value).toBe(""));
        expect(screen.getByTestId("agent-browser-stage").textContent).not.toContain("123456");
        expect(screen.getByTestId("agent-browser-shield")).toBeTruthy();
    });

    it("writes the bar into the clicked field before Enter", async () => {
        const onInput = vi.fn().mockResolvedValue(undefined);
        const box = { left: 0, top: 0, right: 100, bottom: 100, width: 100, height: 100, x: 0, y: 0, toJSON() { return {}; } };
        render(<AgentBrowserStage state={{ ...base, frame: "aGk=", width: 100, height: 100, userHold: true }} lang="zh-Hans" onInput={onInput} />);
        const img = screen.getByTestId("agent-browser-frame") as HTMLImageElement;
        img.getBoundingClientRect = () => box;
        fireEvent.pointerDown(img, { clientX: 25, clientY: 40, pointerId: 1 });
        const keys = screen.getByTestId("agent-browser-keys") as HTMLInputElement;
        expect(keys.placeholder).toContain("输入框");
        fireEvent.input(keys, { target: { value: "139" } });
        fireEvent.keyDown(keys, { key: "Backspace" });
        await waitFor(() => {
            const payloads = onInput.mock.calls.map((call) => JSON.parse(String(call[0])));
            expect(payloads.some((item) => item.kind === "text" && item.text === "139")).toBe(true);
            expect(payloads.some((item) => item.kind === "keydown" && item.key === "Backspace")).toBe(false);
        });
        fireEvent.keyDown(keys, { key: "Enter" });
        await waitFor(() => {
            const payloads = onInput.mock.calls.map((call) => JSON.parse(String(call[0])));
            const keyAt = payloads.findLastIndex((item) => item.kind === "keydown");
            const textAt = payloads.findLastIndex((item) => item.kind === "text");
            expect(keyAt).toBeGreaterThan(textAt);
            expect(payloads[keyAt].key).toBe("Enter");
        });
    });

    it("does not send Enter when the field write fails", async () => {
        const onInput = vi.fn((body: string) => {
            const payload = JSON.parse(body) as { kind?: string };
            if (payload.kind === "text") return Promise.reject(new Error("no caret"));
            return Promise.resolve();
        });
        render(<AgentBrowserStage state={{ ...base, frame: "aGk=", width: 100, height: 100, userHold: true }} lang="zh-Hans" onInput={onInput} />);
        const keys = screen.getByTestId("agent-browser-keys") as HTMLInputElement;
        fireEvent.input(keys, { target: { value: "139" } });
        fireEvent.keyDown(keys, { key: "Enter" });
        await waitFor(() => {
            const kinds = onInput.mock.calls.map((call) => JSON.parse(String(call[0])).kind as string);
            expect(kinds).toContain("text");
        });
        await new Promise((resolve) => setTimeout(resolve, 0));
        const kinds = onInput.mock.calls.map((call) => JSON.parse(String(call[0])).kind as string);
        expect(kinds).not.toContain("keydown");
        expect(kinds).not.toContain("keyup");
    });

    it("clears the bar only when the page focuses a different field", async () => {
        const onInput = vi.fn((body: string) => {
            const payload = JSON.parse(body) as { kind?: string; y?: number };
            if (payload.kind === "down" && (payload.y || 0) > 0.7) {
                return Promise.reject(new Error("viewport caret moved"));
            }
            return Promise.resolve();
        });
        render(<AgentBrowserStage state={{ ...base, frame: "aGk=", width: 100, height: 100, userHold: true }} lang="zh-Hans" onInput={onInput} />);
        const img = screen.getByTestId("agent-browser-frame") as HTMLImageElement;
        const box = { left: 0, top: 0, right: 100, bottom: 100, width: 100, height: 100, x: 0, y: 0, toJSON() { return {}; } };
        img.getBoundingClientRect = () => box;
        const keys = screen.getByTestId("agent-browser-keys") as HTMLInputElement;
        fireEvent.pointerDown(img, { clientX: 25, clientY: 40, pointerId: 1 });
        expect(document.activeElement).toBe(keys);
        fireEvent.input(keys, { target: { value: "139" } });
        fireEvent.pointerDown(img, { clientX: 90, clientY: 40, pointerId: 1 });
        await new Promise((resolve) => setTimeout(resolve, 0));
        expect(keys.value).toBe("139");
        fireEvent.pointerDown(img, { clientX: 25, clientY: 90, pointerId: 1 });
        await waitFor(() => expect(keys.value).toBe(""));
        expect(document.activeElement).toBe(keys);
        fireEvent.input(keys, { target: { value: "6" } });
        await waitFor(() => {
            const texts = onInput.mock.calls
                .map((call) => JSON.parse(String(call[0])) as { kind?: string; text?: string; gen?: number })
                .filter((item) => item.kind === "text");
            expect(texts[0]).toMatchObject({ text: "139", gen: 0 });
            expect(texts[texts.length - 1]).toMatchObject({ text: "6", gen: 1 });
        });
    });
});
