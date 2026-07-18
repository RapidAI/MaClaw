/**
 * Package the extension into ../gui/vscode_ext_asset/maclaw-acp.vsix (+ version.txt)
 * so the MaClaw GUI can go:embed it and auto-install into VS Code.
 */
import { spawnSync } from "child_process";
import * as fs from "fs";
import * as path from "path";
import { fileURLToPath } from "url";

const root = path.dirname(path.dirname(fileURLToPath(import.meta.url)));
const pkg = JSON.parse(fs.readFileSync(path.join(root, "package.json"), "utf8"));
const version = pkg.version;

// Invoke vsce through node directly: no shell on any platform, so checkout
// paths containing spaces work and there is no cmd arg-concatenation risk.
const vsceJs = path.join(root, "node_modules", "@vscode", "vsce", "vsce");
const outVsix = path.join(root, `maclaw-acp-${version}.vsix`);

const run = spawnSync(
  process.execPath,
  // Everything is esbuild-bundled into dist/ — do NOT let vsce drag
  // node_modules production deps into the VSIX (23MB bloat, unneeded).
  [vsceJs, "package", "--allow-missing-repository", "--skip-license", "--no-dependencies", "-o", outVsix],
  { cwd: root, stdio: "inherit" }
);
if (run.error) {
  console.error("[package] failed to spawn vsce:", run.error.message);
  process.exit(1);
}
if (run.status !== 0) {
  process.exit(run.status ?? 1);
}

const assetDir = path.resolve(root, "..", "gui", "vscode_ext_asset");
fs.mkdirSync(assetDir, { recursive: true });
fs.copyFileSync(outVsix, path.join(assetDir, "maclaw-acp.vsix"));
fs.writeFileSync(path.join(assetDir, "version.txt"), version + "\n", "utf8");
console.log(`[package] embedded asset updated: ${assetDir} (v${version})`);
