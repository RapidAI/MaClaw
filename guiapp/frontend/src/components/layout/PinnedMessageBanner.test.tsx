import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createRef } from "react";
import { PINNED_BANNER_SLIDE_MS, PinnedMessageBanner } from "./PinnedMessageBanner";
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
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
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

  it("pages through the slides with the dots, permanent first", () => {
    const messages = [
      pin({ id: "p-perm", title: "永久置顶", pinned_forever: true }),
      pin({ id: "p-1", title: "第一条" }),
      pin({ id: "p-2", title: "第二条" }),
    ];
    const { container } = render(
      <PinnedMessageBanner messages={messages} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    // The permanent pin is the leftmost (first) slide. Dots are the only pager.
    expect(screen.getByText("永久置顶")).toBeTruthy();
    expect(container.querySelector(".mc-pinned-banner__arrow")).toBeNull();
    const dots = Array.from(container.querySelectorAll(".mc-pinned-banner__dot")) as HTMLButtonElement[];
    expect(dots).toHaveLength(3);
    fireEvent.click(dots[1]);
    expect(screen.getByText("第一条")).toBeTruthy();
    fireEvent.click(dots[2]);
    expect(screen.getByText("第二条")).toBeTruthy();
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

  it("advances left to right every 10 seconds and wraps to the first slide", () => {
    const messages = [
      pin({ id: "p-perm", title: "永久置顶", pinned_forever: true }),
      pin({ id: "p-1", title: "第一条" }),
      pin({ id: "p-2", title: "第二条" }),
    ];
    render(
      <PinnedMessageBanner messages={messages} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    expect(screen.getByText("永久置顶")).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS);
    });
    expect(screen.getByText("第一条")).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS);
    });
    expect(screen.getByText("第二条")).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS - 1);
    });
    expect(screen.getByText("第二条")).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.getByText("永久置顶")).toBeTruthy();
  });

  it("pauses the rotation while the pointer is over the card and starts a fresh 10 seconds after it leaves", () => {
    const messages = [
      pin({ id: "p-1", title: "第一条" }),
      pin({ id: "p-2", title: "第二条" }),
    ];
    const { container } = render(
      <PinnedMessageBanner messages={messages} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    const banner = container.querySelector(".mc-pinned-banner");
    expect(banner).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS - 1);
    });
    fireEvent.mouseEnter(banner!);
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS * 3);
    });
    expect(screen.getByText("第一条")).toBeTruthy();
    fireEvent.mouseLeave(banner!);
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS - 1);
    });
    expect(screen.getByText("第一条")).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.getByText("第二条")).toBeTruthy();
  });

  it("holds the current slide while the account popover hides the card, then shows it for a fresh 10 seconds", async () => {
    const messages = [
      pin({ id: "p-1", title: "第一条" }),
      pin({ id: "p-2", title: "第二条" }),
    ];
    const ref = createRef<HTMLDivElement>();
    const { container } = render(
      <div className="sidebar-system-status">
        <div className="mc-workbench-status-popover" data-open="false" />
        <div ref={ref} />
        <PinnedMessageBanner messages={messages} lang="zh-Hans" anchorRef={ref} onDismiss={() => {}} />
      </div>,
    );
    const popover = container.querySelector(".mc-workbench-status-popover");
    expect(popover).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS - 1);
    });
    await act(async () => {
      popover!.setAttribute("data-open", "true");
    });
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS * 3);
    });
    expect(screen.getByText("第一条")).toBeTruthy();
    await act(async () => {
      popover!.setAttribute("data-open", "false");
    });
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS - 1);
    });
    expect(screen.getByText("第一条")).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.getByText("第二条")).toBeTruthy();
  });

  it("resumes a fresh 10 seconds when the pointer leaves the window", () => {
    const messages = [
      pin({ id: "p-1", title: "第一条" }),
      pin({ id: "p-2", title: "第二条" }),
    ];
    const { container } = render(
      <PinnedMessageBanner messages={messages} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    const banner = container.querySelector(".mc-pinned-banner");
    expect(banner).toBeTruthy();
    fireEvent.mouseEnter(banner!);
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS * 3);
    });
    expect(screen.getByText("第一条")).toBeTruthy();
    fireEvent.mouseLeave(document.documentElement);
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS - 1);
    });
    expect(screen.getByText("第一条")).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.getByText("第二条")).toBeTruthy();
  });

  it("does not resume while the pointer is still over the card if a document mouseout has no target", () => {
    const messages = [
      pin({ id: "p-1", title: "第一条" }),
      pin({ id: "p-2", title: "第二条" }),
    ];
    const { container } = render(
      <PinnedMessageBanner messages={messages} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    const banner = container.querySelector(".mc-pinned-banner");
    expect(banner).toBeTruthy();
    fireEvent.mouseEnter(banner!);
    fireEvent.mouseOut(document, { relatedTarget: null });
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS * 3);
    });
    expect(screen.getByText("第一条")).toBeTruthy();
  });

  it("stays on the slide being read when a new pin is inserted ahead of it", () => {
    const initial = [
      pin({ id: "a", title: "正在看" }),
      pin({ id: "b", title: "下一条" }),
    ];
    const { rerender } = render(
      <PinnedMessageBanner messages={initial} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    expect(screen.getByText("正在看")).toBeTruthy();
    rerender(
      <PinnedMessageBanner
        messages={[pin({ id: "x", title: "新插入" }), ...initial]}
        lang="zh-Hans"
        anchorRef={anchor()}
        onDismiss={() => {}}
      />,
    );
    expect(screen.getByText("正在看")).toBeTruthy();
    expect(screen.queryByText("新插入")).toBeNull();
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS);
    });
    expect(screen.getByText("下一条")).toBeTruthy();
  });

  it("keeps the remaining time when the same slides arrive in a new array", () => {
    const initial = [
      pin({ id: "a", title: "正在看" }),
      pin({ id: "b", title: "下一条" }),
    ];
    const { rerender } = render(
      <PinnedMessageBanner messages={initial} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS - 1000);
    });
    rerender(
      <PinnedMessageBanner
        messages={initial.map((message) => ({ ...message }))}
        lang="zh-Hans"
        anchorRef={anchor()}
        onDismiss={() => {}}
      />,
    );
    act(() => {
      vi.advanceTimersByTime(999);
    });
    expect(screen.getByText("正在看")).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(1);
    });
    expect(screen.getByText("下一条")).toBeTruthy();
  });

  it("falls back to the first slide when the current one disappears", () => {
    const initial = [
      pin({ id: "a", title: "第一条" }),
      pin({ id: "b", title: "正在看" }),
      pin({ id: "c", title: "下一条" }),
    ];
    const { rerender } = render(
      <PinnedMessageBanner messages={initial} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS);
    });
    expect(screen.getByText("正在看")).toBeTruthy();
    rerender(
      <PinnedMessageBanner
        messages={[initial[0], initial[2]]}
        lang="zh-Hans"
        anchorRef={anchor()}
        onDismiss={() => {}}
      />,
    );
    expect(screen.getByText("第一条")).toBeTruthy();
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS);
    });
    expect(screen.getByText("下一条")).toBeTruthy();
  });

  it("holds the slide while a control on the card has focus", () => {
    const messages = [
      pin({ id: "p-1", title: "第一条" }),
      pin({ id: "p-2", title: "第二条" }),
    ];
    render(
      <PinnedMessageBanner messages={messages} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    fireEvent.focus(screen.getByText("查看详情"));
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS * 3);
    });
    expect(screen.getByText("第一条")).toBeTruthy();
    fireEvent.blur(screen.getByText("查看详情"));
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS);
    });
    expect(screen.getByText("第二条")).toBeTruthy();
  });

  it("does not keep the clock stopped after a pointer click once the pointer leaves", () => {
    const messages = [
      pin({ id: "p-1", title: "第一条" }),
      pin({ id: "p-2", title: "第二条" }),
    ];
    const { container } = render(
      <PinnedMessageBanner messages={messages} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    const banner = container.querySelector(".mc-pinned-banner");
    const dot = container.querySelector(".mc-pinned-banner__dot");
    expect(banner).toBeTruthy();
    expect(dot).toBeTruthy();
    fireEvent.pointerDown(dot!);
    fireEvent.focus(dot!);
    fireEvent.pointerUp(dot!);
    fireEvent.mouseLeave(banner!);
    act(() => {
      vi.advanceTimersByTime(PINNED_BANNER_SLIDE_MS);
    });
    expect(screen.getByText("第二条")).toBeTruthy();
  });

  it("renders nothing without visible messages", () => {
    const { container } = render(
      <PinnedMessageBanner messages={[]} lang="zh-Hans" anchorRef={anchor()} onDismiss={() => {}} />,
    );
    expect(container.querySelector(".mc-pinned-banner")).toBeNull();
  });
});
