// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { main } from "../../../../wailsjs/go/models";
import { LoadConfig, SaveConfig } from "../../../../wailsjs/go/main/App";
import { LLMCacheSettingsPanel } from "../LLMCacheSettingsPanel";

vi.mock("../../../../wailsjs/go/main/App", () => ({
  LoadConfig: vi.fn(),
  SaveConfig: vi.fn(async () => undefined),
}));

beforeEach(() => {
  (LoadConfig as any).mockResolvedValue(new main.AppConfig({ maclaw_llm_current_provider: "MaClaw Official" } as any));
});

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
    expect(LoadConfig).toHaveBeenCalledTimes(2);
    expect(showToastMessage).toHaveBeenCalledWith("保存成功");
  });

  it("merges cache changes into the latest config before saving", async () => {
    (LoadConfig as any).mockResolvedValue(new main.AppConfig({ maclaw_llm_current_provider: "MaClaw Official" } as any));
    const Harness = () => {
      const [cfg, setCfg] = useState<main.AppConfig | null>(new main.AppConfig({ maclaw_llm_current_provider: "stale", llm_prompt_cache: { enabled: true } } as any));
      return <LLMCacheSettingsPanel config={cfg} setConfig={setCfg} lang="en" />;
    };
    render(
      <Harness />
    );

    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(SaveConfig).toHaveBeenCalledTimes(1));
    const saved = (SaveConfig as any).mock.calls[0][0];
    expect(saved.maclaw_llm_current_provider).toBe("MaClaw Official");
    expect(saved.llm_prompt_cache.enabled).toBe(true);
  });

  it("keeps cache enabled when optional sub-switches are omitted", async () => {
    render(
      <LLMCacheSettingsPanel
        config={new main.AppConfig({ llm_prompt_cache: { enabled: true } } as any)}
        setConfig={vi.fn()}
        lang="en"
      />
    );

    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(SaveConfig).toHaveBeenCalledTimes(1));
    const saved = (SaveConfig as any).mock.calls[0][0];
    expect(saved.llm_prompt_cache.enabled).toBe(true);
    expect(saved.llm_prompt_cache.openai_enabled).toBe(true);
    expect(saved.llm_prompt_cache.anthropic_enabled).toBe(true);
    expect(saved.llm_prompt_cache.stream_synthesis_enabled).toBe(true);
  });

  it("keeps save successful when post-save reload fails", async () => {
    const showToastMessage = vi.fn();
    const setConfig = vi.fn();
    (LoadConfig as any)
      .mockResolvedValueOnce(new main.AppConfig({ maclaw_llm_current_provider: "MaClaw Official" } as any))
      .mockRejectedValueOnce(new Error("reload failed"));
    render(
      <LLMCacheSettingsPanel
        config={new main.AppConfig({ llm_prompt_cache: { enabled: true } } as any)}
        setConfig={setConfig}
        lang="en"
        showToastMessage={showToastMessage}
      />
    );

    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(SaveConfig).toHaveBeenCalledTimes(1));
    expect(setConfig).toHaveBeenCalledWith(expect.objectContaining({ maclaw_llm_current_provider: "MaClaw Official" }));
    expect(showToastMessage).toHaveBeenCalledWith("Saved successfully");
  });

  it("turning the master switch on restores default cache scopes when all scopes were off", async () => {
    const Harness = () => {
      const [cfg, setCfg] = useState<main.AppConfig | null>(new main.AppConfig({ llm_prompt_cache: { enabled: false, openai_enabled: false, anthropic_enabled: false, stream_synthesis_enabled: false } } as any));
      return <LLMCacheSettingsPanel config={cfg} setConfig={setCfg} lang="en" />;
    };
    render(<Harness />);

    fireEvent.click(screen.getByLabelText("Enable local LLM cache"));
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(SaveConfig).toHaveBeenCalledTimes(1));
    const saved = (SaveConfig as any).mock.calls[0][0];
    expect(saved.llm_prompt_cache.enabled).toBe(true);
    expect(saved.llm_prompt_cache.openai_enabled).toBe(true);
    expect(saved.llm_prompt_cache.anthropic_enabled).toBe(true);
    expect(saved.llm_prompt_cache.stream_synthesis_enabled).toBe(true);
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
