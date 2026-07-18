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

for (const cfg of [extensionConfig, webviewConfig, launcherConfig, smokeConfig, configTestConfig]) {
  if (watch) {
    const ctx = await esbuild.context(cfg);
    await ctx.watch();
  } else {
    await esbuild.build(cfg);
  }
}
