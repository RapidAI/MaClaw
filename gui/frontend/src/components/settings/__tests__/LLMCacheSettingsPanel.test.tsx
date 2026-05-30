// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { main } from "../../../../wailsjs/go/models";
import { SaveConfig } from "../../../../wailsjs/go/main/App";
import { LLMCacheSettingsPanel } from "../LLMCacheSettingsPanel";

vi.mock("../../../../wailsjs/go/main/App", () => ({
  SaveConfig: vi.fn(async () => undefined),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

describe("LLMCacheSettingsPanel", () => {
  it("shows toast after saving cache settings", async () => {
    const showToastMessage = vi.fn();
    render(
      <LLMCacheSettingsPanel
        config={new main.AppConfig({ llm_prompt_cache: { enabled: true, cache_dir: "D:/maclaw/cache" } } as any)}
        setConfig={vi.fn()}
        lang="zh-Hans"
        showToastMessage={showToastMessage}
      />
    );

    fireEvent.click(screen.getByText("保存"));

    await waitFor(() => expect(SaveConfig).toHaveBeenCalledTimes(1));
    expect(showToastMessage).toHaveBeenCalledWith("保存成功");
  });

  it("renders cache directory as a full-width text input", () => {
    render(
      <LLMCacheSettingsPanel
        config={new main.AppConfig({ llm_prompt_cache: { enabled: true, cache_dir: "D:/very/long/maclaw/cache/path" } } as any)}
        setConfig={vi.fn()}
        lang="en"
      />
    );

    const input = screen.getByDisplayValue("D:/very/long/maclaw/cache/path");
    expect(input.classList.contains("llm-cache-settings-dir-input")).toBe(true);
    expect(input.getAttribute("title")).toBe("D:/very/long/maclaw/cache/path");
  });
});
