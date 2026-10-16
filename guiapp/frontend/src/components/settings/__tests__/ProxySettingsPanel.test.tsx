// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { SaveProxyConfig, TestProxyConfig } from '../../../../wailsjs/go/main/App';
import { corelib } from '../../../../wailsjs/go/models';
import { ProxySettingsPanel } from "../ProxySettingsPanel";

vi.mock("../../../../wailsjs/go/main/App", () => ({
  SaveProxyConfig: vi.fn(async () => undefined),
  TestProxyConfig: vi.fn(async () => ({ ok: true, status: 204, latency_ms: 42, egress_ip: "203.0.113.9" })),
}));

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

const t = (key: string) => key;

function renderHarness(initial: corelib.AppConfig, showToastMessage?: (message: string, duration?: number) => void) {
  const Harness = () => {
    const [config, setConfig] = useState<corelib.AppConfig | null>(initial);
    return <ProxySettingsPanel config={config} setConfig={setConfig} isWindows={false} lang="en" t={t} showToastMessage={showToastMessage} />;
  };
  render(<Harness />);
}

describe("ProxySettingsPanel", () => {
  it("saves proxy settings and shows a toast", async () => {
    const showToastMessage = vi.fn();
    renderHarness(new corelib.AppConfig({
      default_proxy_enabled: false,
      default_proxy_protocol: "http",
      default_proxy_host: "",
      default_proxy_port: "",
    } as any), showToastMessage);

    fireEvent.click(screen.getByLabelText("proxyEnabled"));
    fireEvent.change(screen.getByPlaceholderText("proxyHostPlaceholder"), { target: { value: "proxy.example.com" } });
    fireEvent.change(screen.getByPlaceholderText("proxyPortPlaceholder"), { target: { value: "1080" } });
    fireEvent.click(screen.getByText("Save"));

    await waitFor(() => expect(SaveProxyConfig).toHaveBeenCalledWith(expect.objectContaining({
      enabled: true,
      protocol: "http",
      host: "proxy.example.com",
      port: "1080",
      scope_maclaw: true,
    })));
    expect((await screen.findByRole("status")).textContent).toContain("saved");
    expect(showToastMessage).toHaveBeenCalledWith("saved");
  });

  it("shows a save failure banner when Wails returns a string error", async () => {
    const showToastMessage = vi.fn();
    vi.mocked(SaveProxyConfig).mockRejectedValueOnce("disk full");
    renderHarness(new corelib.AppConfig({
      default_proxy_enabled: true,
      default_proxy_protocol: "http",
      default_proxy_host: "127.0.0.1",
      default_proxy_port: "7890",
    } as any), showToastMessage);

    fireEvent.click(screen.getByText("Save"));

    expect((await screen.findByRole("status")).textContent).toContain("disk full");
    expect(showToastMessage).toHaveBeenCalledWith("proxySaveFailed: disk full", 5000);
  });

  it("tests the current proxy form and shows the result", async () => {
    const showToastMessage = vi.fn();
    renderHarness(new corelib.AppConfig({
      default_proxy_enabled: true,
      default_proxy_protocol: "http",
      default_proxy_host: "127.0.0.1",
      default_proxy_port: "7890",
    } as any), showToastMessage);

    fireEvent.click(screen.getByText("proxyTest"));

    await waitFor(() => expect(TestProxyConfig).toHaveBeenCalledWith(expect.objectContaining({
      enabled: true,
      host: "127.0.0.1",
      port: "7890",
    })));
    expect((await screen.findByRole("status")).textContent).toContain("proxyTestOK");
    expect(showToastMessage).not.toHaveBeenCalled();
  });

  it("checks MacClaw scope when enabling proxy with no scopes", () => {
    renderHarness(new corelib.AppConfig({
      default_proxy_enabled: false,
      default_proxy_protocol: "http",
      default_proxy_host: "127.0.0.1",
      default_proxy_port: "1080",
    } as any));

    fireEvent.click(screen.getByLabelText("proxyEnabled"));
    expect((screen.getByLabelText("proxyScopeMaclaw") as HTMLInputElement).checked).toBe(true);
  });

  it("disables test when the port is invalid", () => {
    renderHarness(new corelib.AppConfig({
      default_proxy_enabled: true,
      default_proxy_protocol: "http",
      default_proxy_host: "127.0.0.1",
      default_proxy_port: "99999",
    } as any));

    expect((screen.getByText("proxyTest") as HTMLButtonElement).disabled).toBe(true);
  });

  it("blocks save while a proxy test is in flight", async () => {
    let finish: (value: { ok: boolean; status: number; latency_ms: number }) => void = () => undefined;
    vi.mocked(TestProxyConfig).mockImplementationOnce(() => new Promise((resolve) => {
      finish = resolve;
    }));
    renderHarness(new corelib.AppConfig({
      default_proxy_enabled: true,
      default_proxy_protocol: "http",
      default_proxy_host: "127.0.0.1",
      default_proxy_port: "7890",
    } as any));

    fireEvent.click(screen.getByText("proxyTest"));
    const testingBtn = await screen.findByText("proxyTesting");
    expect((testingBtn as HTMLButtonElement).disabled).toBe(true);
    expect((screen.getByText("Save") as HTMLButtonElement).disabled).toBe(true);
    fireEvent.click(screen.getByText("Save"));
    expect(SaveProxyConfig).not.toHaveBeenCalled();
    finish({ ok: true, status: 204, latency_ms: 1 });
    await waitFor(() => expect((screen.getByText("Save") as HTMLButtonElement).disabled).toBe(false));
  });
});
