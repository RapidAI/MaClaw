import * as vscode from "vscode";
import { ChatViewProvider } from "./chatViewProvider";
import { LauncherViewProvider } from "./launcherViewProvider";
import { resolveBridgePath } from "./bridgeResolver";

// eslint-disable-next-line @typescript-eslint/no-var-requires
const pkg = require("../package.json") as { version: string };

export function activate(context: vscode.ExtensionContext): void {
  const provider = new ChatViewProvider(context, pkg.version);
  const launcher = new LauncherViewProvider(context, provider);
  context.subscriptions.push(
    provider,
    launcher,
    vscode.window.registerWebviewViewProvider(ChatViewProvider.viewType, provider, {
      webviewOptions: { retainContextWhenHidden: true },
    }),
    vscode.window.registerWebviewViewProvider(LauncherViewProvider.viewType, launcher, {
      webviewOptions: { retainContextWhenHidden: true },
    }),
    vscode.commands.registerCommand("maclaw-acp.openChat", () =>
      vscode.commands.executeCommand(`${ChatViewProvider.viewType}.focus`)
    ),
    vscode.commands.registerCommand("maclaw-acp.newSession", () => provider.newSession()),
    vscode.commands.registerCommand("maclaw-acp.cancelTurn", () => provider.cancelTurn())
  );

  // One-time welcome: right after the launcher installs the extension, open
  // the chat so the user sees where it lives. Never pops up again afterwards.
  const welcomed = context.globalState.get<boolean>("maclaw-acp.welcomed");
  if (!welcomed) {
    void context.globalState.update("maclaw-acp.welcomed", true);
    if (resolveBridgePath()) {
      setTimeout(() => {
        void vscode.commands.executeCommand(`${ChatViewProvider.viewType}.focus`);
      }, 1500);
    }
  }
}

export function deactivate(): void {
  /* provider.dispose() runs via subscriptions */
}
