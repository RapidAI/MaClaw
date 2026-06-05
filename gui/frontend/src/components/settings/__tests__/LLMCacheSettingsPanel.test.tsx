// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useState } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { main } from "../../../../wailsjs/go/models";
import { PatchConfigFields } from "../../../../wailsjs/go/main/App";
import { LLMCacheSettingsPanel } from "../LLMCacheSettingsPanel";

vi.mock("../../../../wailsjs/go/main/App", () => ({
  PatchConfigFields: vi.fn(async (patch: Record<string, unknown>) => new main.AppConfig({ maclaw_llm_current_provider: "MaClaw Official", ...patch } as any)),
}));

beforeEach(() => {
  (PatchConfigFields as any).mockResolvedValue(new main.AppConfig({ maclaw_llm_current_provider: "MaClaw Official" } as any));
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

    await waitFor(() => expect(PatchConfigFields).toHaveBeenCalledTimes(1));
    expect(showToastMessage).toHaveBeenCalledWith("保存成功");
  });

  it("saves only cache settings as a config patch", async () => {
    const Harness = () => {
      const [cfg, setCfg] = useState<main.AppConfig | null>(new main.AppConfig({ maclaw_llm_current_provider: "stale", llm_prompt_cache: { enabled: true } } as any));
      return <LLMCacheSettingsPanel config={cfg} setConfig={setCfg} lang="en" />;
    };
    render(
      <Harness />
    );

    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(PatchConfigFields).toHaveBeenCalledTimes(1));
    expect(PatchConfigFields).toHaveBeenCalledWith(expect.objectContaining({
      llm_prompt_cache: expect.objectContaining({ enabled: true }),
    }));
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

    await waitFor(() => expect(PatchConfigFields).toHaveBeenCalledTimes(1));
    const patch = (PatchConfigFields as any).mock.calls[0][0];
    expect(patch.llm_prompt_cache.enabled).toBe(true);
    expect(patch.llm_prompt_cache.openai_enabled).toBe(true);
    expect(patch.llm_prompt_cache.anthropic_enabled).toBe(true);
    expect(patch.llm_prompt_cache.stream_synthesis_enabled).toBe(true);
  });

  it("updates config from patch response", async () => {
    const showToastMessage = vi.fn();
    const setConfig = vi.fn();
    (PatchConfigFields as any).mockResolvedValueOnce(new main.AppConfig({ maclaw_llm_current_provider: "MaClaw Official", llm_prompt_cache: { enabled: true } } as any));
    render(
      <LLMCacheSettingsPanel
        config={new main.AppConfig({ llm_prompt_cache: { enabled: true } } as any)}
        setConfig={setConfig}
        lang="en"
        showToastMessage={showToastMessage}
      />
    );

    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(PatchConfigFields).toHaveBeenCalledTimes(1));
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

    await waitFor(() => expect(PatchConfigFields).toHaveBeenCalledTimes(1));
    const patch = (PatchConfigFields as any).mock.calls[0][0];
    expect(patch.llm_prompt_cache.enabled).toBe(true);
    expect(patch.llm_prompt_cache.openai_enabled).toBe(true);
    expect(patch.llm_prompt_cache.anthropic_enabled).toBe(true);
    expect(patch.llm_prompt_cache.stream_synthesis_enabled).toBe(true);
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
