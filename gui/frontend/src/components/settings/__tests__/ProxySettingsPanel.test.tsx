// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from '@testing-library/react';
import { useState } from 'react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { corelib, main } from '../../../../wailsjs/go/models';
import { ProxySettingsPanel } from "../ProxySettingsPanel";

afterEach(() => {
  cleanup();
  vi.restoreAllMocks();
  delete (window as any).go;
});

const t = (key: string) => key;

function renderHarness(initial: corelib.AppConfig) {
  const saveProxyConfig = vi.fn(async () => undefined);
  (window as any).go = { main: { App: { SaveProxyConfig: saveProxyConfig } } };
  const Harness = () => {
    const [config, setConfig] = useState<corelib.AppConfig | null>(initial);
    return <ProxySettingsPanel config={config} setConfig={setConfig} isWindows={false} lang="en" t={t} />;
  };
  render(<Harness />);
  return { saveProxyConfig };
}

describe("ProxySettingsPanel", () => {
  it("saves proxy settings through SaveProxyConfig only", () => {
    const { saveProxyConfig } = renderHarness(new corelib.AppConfig({
      default_proxy_enabled: false,
      default_proxy_protocol: "http",
      default_proxy_host: "",
      default_proxy_port: "",
    } as any));

    fireEvent.click(screen.getByLabelText("proxyEnabled"));
    fireEvent.change(screen.getByPlaceholderText("proxyHostPlaceholder"), { target: { value: "proxy.example.com" } });
    fireEvent.change(screen.getByPlaceholderText("proxyPortPlaceholder"), { target: { value: "1080" } });
    fireEvent.click(screen.getByText("Save"));

    expect(saveProxyConfig).toHaveBeenCalledWith(expect.objectContaining({
      enabled: true,
      protocol: "http",
      host: "proxy.example.com",
      port: "1080",
    }));
  });
});
