import * as esbuild from "esbuild";

const watch = process.argv.includes("--watch");

const shared = {
  bundle: true,
  sourcemap: true,
  minify: !watch,
  logLevel: "info",
};

const extensionConfig = {
  ...shared,
  entryPoints: ["src/extension.ts"],
  outfile: "dist/extension.js",
  platform: "node",
  format: "cjs",
  target: "node18",
  external: ["vscode"],
};

const webviewConfig = {
  ...shared,
  entryPoints: ["webview/main.ts"],
  outfile: "dist/webview.js",
  platform: "browser",
  format: "iife",
  target: "es2022",
  loader: { ".css": "text" },
};

const launcherConfig = {
  ...shared,
  entryPoints: ["webview/launcher.ts"],
  outfile: "dist/launcher.js",
  platform: "browser",
  format: "iife",
  target: "es2022",
  loader: { ".css": "text" },
};

const smokeConfig = {
  ...shared,
  entryPoints: ["src/acpClient.ts"],
  outfile: "test/out/acpClient.cjs",
  platform: "node",
  format: "cjs",
  target: "node18",
};

const configTestConfig = {
  ...shared,
  entryPoints: ["src/maclawConfig.ts"],
  outfile: "test/out/maclawConfig.cjs",
  platform: "node",
  format: "cjs",
  target: "node18",
};

// chatViewProvider with `vscode` left external: test/queue-e2e.mjs installs a
// vscode stub via a require hook and drives the provider against the fake
// queue agent over real stdio JSON-RPC.
const queueTestConfig = {
  ...shared,
  entryPoints: ["src/chatViewProvider.ts"],
  outfile: "test/out/chatViewProvider.cjs",
  platform: "node",
  format: "cjs",
  target: "node18",
  external: ["vscode"],
};

for (const cfg of [extensionConfig, webviewConfig, launcherConfig, smokeConfig, configTestConfig, queueTestConfig]) {
  if (watch) {
    const ctx = await esbuild.context(cfg);
    await ctx.watch();
  } else {
    await esbuild.build(cfg);
  }
}
