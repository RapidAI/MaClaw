#!/usr/bin/env node
/**
 * Resolve the product version for CI / packaging.
 *
 * Priority:
 *   1. Git tag (V6.5.2.11519 / v6.5.2.11519)
 *   2. gui/frontend/src/version.ts appVersion
 *   3. wails.json productVersion, optionally patched with build_number
 *
 * Prints GitHub Actions GITHUB_ENV lines to stdout:
 *   VERSION=...
 *   VERSION_SOURCE=...
 *
 * Also rewrites gui/frontend/src/version.ts so frontend and backend match.
 *
 * Usage:
 *   node scripts/resolve-release-version.mjs [refName]
 *   node scripts/resolve-release-version.mjs --print   # human-readable only
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "..");

function readText(rel) {
  const full = path.join(repoRoot, rel);
  if (!fs.existsSync(full)) return "";
  return fs.readFileSync(full, "utf8");
}

function parseArgs(argv) {
  let refName = process.env.GITHUB_REF_NAME || "";
  let printOnly = false;
  let writeVersionFile = true;
  for (const arg of argv) {
    if (arg === "--print" || arg === "-p") {
      printOnly = true;
      // Dry-run: do not rewrite version.ts when inspecting a tag/version.
      writeVersionFile = false;
    } else if (arg === "--write") {
      writeVersionFile = true;
    } else if (arg === "--no-write") {
      writeVersionFile = false;
    } else if (!arg.startsWith("-") && arg) {
      refName = arg;
    }
  }
  // CI always needs version.ts rewritten so the frontend build matches backend.
  if (process.env.GITHUB_ACTIONS === "true" || process.env.CI === "true") {
    writeVersionFile = true;
  }
  return { refName, printOnly, writeVersionFile };
}

function resolveVersion(refName) {
  const tagMatch = String(refName || "").match(/^[Vv](\d+\.\d+\.\d+(?:\.\d+)?)$/);
  if (tagMatch) {
    return { version: tagMatch[1], source: "git tag" };
  }

  const versionTs = readText("gui/frontend/src/version.ts");
  const appMatch = versionTs.match(/appVersion\s*=\s*['"]([^'"]+)['"]/);
  if (appMatch && appMatch[1].trim()) {
    return { version: appMatch[1].trim(), source: "version.ts" };
  }

  let version = "";
  let source = "wails.json";
  try {
    const cfg = JSON.parse(readText("wails.json") || "{}");
    version = String(cfg?.info?.productVersion || "").trim();
  } catch {
    version = "";
  }

  const bn = readText("build_number").trim();
  if (version && /^\d+\.\d+\.\d+/.test(version) && /^\d+$/.test(bn)) {
    const parts = version.split(".");
    if (parts.length >= 4) {
      parts[3] = bn;
      version = parts.join(".");
    } else {
      version = `${parts.slice(0, 3).join(".")}.${bn}`;
    }
    source = "wails.json+build_number";
  }

  if (!version) {
    return { version: "", source: "missing" };
  }
  return { version, source };
}

function writeVersionTs(version) {
  const parts = version.split(".");
  const buildNum = parts.length >= 4 ? parts[3] : "0";
  const content = `export const buildNumber = '${buildNum}';\nexport const appVersion = '${version}';\n`;
  const out = path.join(repoRoot, "gui", "frontend", "src", "version.ts");
  fs.mkdirSync(path.dirname(out), { recursive: true });
  fs.writeFileSync(out, content, "utf8");
}

const { refName, printOnly, writeVersionFile } = parseArgs(process.argv.slice(2));
const { version, source } = resolveVersion(refName);

if (!version) {
  console.error(
    "[resolve-release-version] FAILED: could not resolve product version " +
      `(ref=${JSON.stringify(refName)}). Expected a Vx.y.z tag, version.ts, or wails.json.`,
  );
  process.exit(1);
}

if (!/^\d+\.\d+\.\d+(\.\d+)?$/.test(version)) {
  console.error(
    `[resolve-release-version] FAILED: invalid version format: ${JSON.stringify(version)}`,
  );
  process.exit(1);
}

if (writeVersionFile) {
  writeVersionTs(version);
}

if (printOnly) {
  console.log(`version=${version}`);
  console.log(`source=${source}`);
} else {
  // GITHUB_ENV compatible (also harmless for local shells).
  console.log(`VERSION=${version}`);
  console.log(`VERSION_SOURCE=${source}`);
}

console.error(
  `[resolve-release-version] ${source} -> ${version}` +
    `${writeVersionFile ? " (version.ts updated)" : " (dry-run)"}`,
);
