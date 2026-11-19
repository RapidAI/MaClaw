import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { createRef } from "react";
import { PinnedMessageBanner } from "./PinnedMessageBanner";
import type { PinnedMessage } from "../ai/usePinnedNotifications";

const { browserOpenURL } = vi.hoisted(() => ({
  browserOpenURL: vi.fn(),
}));

vi.mock("../../../wailsjs/runtime", () => ({
  BrowserOpenURL: browserOpenURL,
  EventsOn: vi.fn(() => () => {}),
}));

function pin(overrides: Partial<PinnedMessage> = {}): PinnedMessage {
  return {
    id: "pin-1",
    title: "Hy4 flash 正式发布",
    content: "Agent 任务又快又省！限时免费体验",
    link_url: "https://example.com/launch",
    button_text: "",
    pinned_forever: false,
    created_at: "2026-11-01T00:00:00Z",
    ...overrides,
  };
}

function anchor(): React.RefObject<HTMLDivElement | null> {
  const ref = createRef<HTMLDivElement | null>();
  return ref;
}

describe("PinnedMessageBanner", () => {
  beforeEach(() => {
    browserOpenURL.mockClear();
  });

  it("shows the message with the default CTA label when button_text is empty", () => {
    const { getByText } = render(
      <PinnedMessageBanner messages={[pin()]} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    expect(getByText("Hy4 flash 正式发布")).toBeTruthy();
    expect(getByText("查看详情")).toBeTruthy();
  });

  it("uses the admin-configured button label and opens the link on click", () => {
    render(
      <PinnedMessageBanner
        messages={[pin({ button_text: "立即使用" })]}
        lang="zh-Hans"
        anchorRef={anchor()}
        onDismiss={() => {}}
      />,
    );
    fireEvent.click(screen.getByText("立即使用"));
    expect(browserOpenURL).toHaveBeenCalledWith("https://example.com/launch");
  });

  it("hides the CTA when the message carries no link", () => {
    render(
      <PinnedMessageBanner
        messages={[pin({ link_url: "" })]}
        lang="zh-Hans"
        anchorRef={anchor()}
        onDismiss={() => {}}
      />,
    );
    expect(screen.queryByText("查看详情")).toBeNull();
    expect(screen.queryByText("立即使用")).toBeNull();
  });

  it("pages through the slides with the arrows and the dots, permanent first", () => {
    const messages = [
      pin({ id: "p-perm", title: "永久置顶", pinned_forever: true }),
      pin({ id: "p-1", title: "第一条" }),
      pin({ id: "p-2", title: "第二条" }),
    ];
    const { container } = render(
      <PinnedMessageBanner messages={messages} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    // The permanent pin is the leftmost (first) slide.
    expect(screen.getByText("永久置顶")).toBeTruthy();
    const prev = screen.getByLabelText("上一条置顶消息") as HTMLButtonElement;
    const next = screen.getByLabelText("下一条置顶消息") as HTMLButtonElement;
    expect(prev.disabled).toBe(true);
    fireEvent.click(next);
    expect(screen.getByText("第一条")).toBeTruthy();
    // The dots jump directly to a slide.
    const dots = Array.from(container.querySelectorAll(".mc-pinned-banner__dot")) as HTMLButtonElement[];
    expect(dots).toHaveLength(3);
    fireEvent.click(dots[2]);
    expect(screen.getByText("第二条")).toBeTruthy();
    expect(next.disabled).toBe(true);
  });

  it("close button dismisses every visible message via onDismiss", () => {
    const onDismiss = vi.fn();
    render(
      <PinnedMessageBanner
        messages={[pin(), pin({ id: "pin-2", title: "第二条" })]}
        lang="zh-Hans"
        anchorRef={anchor()}
        onDismiss={onDismiss}
      />,
    );
    // Hover-only reveal is CSS-driven; the button stays focusable/clickable.
    const close = screen.getByLabelText(/关闭置顶消息/);
    fireEvent.click(close);
    expect(onDismiss).toHaveBeenCalledTimes(1);
  });

  it("renders nothing without visible messages", () => {
    const { container } = render(
      <PinnedMessageBanner messages={[]} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    expect(container.querySelector(".mc-pinned-banner")).toBeNull();
  });
});
