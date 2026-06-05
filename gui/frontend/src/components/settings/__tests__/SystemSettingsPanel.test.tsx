// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { main } from "../../../../wailsjs/go/models";
import { SystemSettingsPanel } from "../SystemSettingsPanel";

vi.mock("../../../../wailsjs/go/main/App", () => ({
  SaveConfig: vi.fn(async () => undefined),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

function renderPanel(configPatch: Record<string, unknown> = {}) {
  const saveRemoteConfigField = vi.fn();
  render(
    <SystemSettingsPanel
      config={new main.AppConfig({
        remote_heartbeat_sec: 10,
        screen_dim_timeout_min: 3,
        agent_response_timeout_sec: 600,
        maclaw_llm_timeout_sec: 600,
        ...configPatch,
      })}
      setConfig={vi.fn()}
      lang="en"
      audioDevices={{ inputs: [], outputs: [], labelsAvailable: true, requestLabels: vi.fn() }}
      saveRemoteConfigField={saveRemoteConfigField}
      showToastMessage={vi.fn()}
    />
  );
  return { saveRemoteConfigField };
}

describe("SystemSettingsPanel", () => {
  it("renders agent and LLM timeout settings", () => {
    renderPanel({ agent_response_timeout_sec: 300, maclaw_llm_timeout_sec: 480 });

    expect(screen.getByDisplayValue("300")).toBeTruthy();
    expect(screen.getByDisplayValue("480")).toBeTruthy();
    expect(screen.getByTitle("How long the AI assistant may stay silent before the foreground request is marked timed out. Default: 600 seconds. Range: 240-600 seconds.")).toBeTruthy();
    expect(screen.getByTitle("HTTP timeout for MaClaw LLM calls. Default: 600 seconds. Range: 240-600 seconds.")).toBeTruthy();
  });

  it("clamps timeout edits to 240-600 seconds", () => {
    const { saveRemoteConfigField } = renderPanel();
    const agentInput = screen.getByTitle("How long the AI assistant may stay silent before the foreground request is marked timed out. Default: 600 seconds. Range: 240-600 seconds.");
    const llmInput = screen.getByTitle("HTTP timeout for MaClaw LLM calls. Default: 600 seconds. Range: 240-600 seconds.");

    fireEvent.change(agentInput, { target: { value: "120" } });
    fireEvent.change(llmInput, { target: { value: "900" } });

    expect(saveRemoteConfigField).toHaveBeenCalledWith({ agent_response_timeout_sec: 240 });
    expect(saveRemoteConfigField).toHaveBeenCalledWith({ maclaw_llm_timeout_sec: 600 });
  });

  it("clamps stale config values before rendering", () => {
    renderPanel({ agent_response_timeout_sec: 120, maclaw_llm_timeout_sec: 900 });

    expect(screen.getByDisplayValue("240")).toBeTruthy();
    expect(screen.getByDisplayValue("600")).toBeTruthy();
  });

  it("saves workstation mode through config patch flow", () => {
    const { saveRemoteConfigField } = renderPanel({ workstation_mode: false });

    fireEvent.click(screen.getByLabelText("Workstation Mode"));

    expect(saveRemoteConfigField).toHaveBeenCalledWith({ workstation_mode: true });
  });
});
