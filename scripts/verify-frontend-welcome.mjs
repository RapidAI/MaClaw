#!/usr/bin/env node
/**
 * Fail CI if gui/frontend/dist is missing the current AI assistant welcome page.
 *
 * TigerClaw / MetaStaff OEM builds share the same frontend embed as MaClaw.
 * Without this check, a stale or incomplete frontend dist can ship and look
 * brand-specific ("Tiger has old welcome cards") when it is really a packaging
 * regression.
 *
 * Usage:
 *   node scripts/verify-frontend-welcome.mjs
 *   node scripts/verify-frontend-welcome.mjs --dist gui/frontend/dist
 *   node scripts/verify-frontend-welcome.mjs --binary dist/TigerClaw.exe
 */
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const __dirname = path.dirname(fileURLToPath(import.meta.url));
const repoRoot = path.resolve(__dirname, "..");

function resolveUserPath(p) {
  // Prefer absolute paths; otherwise resolve relative to cwd first, then repo root.
  if (path.isAbsolute(p)) return p;
  const fromCwd = path.resolve(process.cwd(), p);
  if (fs.existsSync(fromCwd)) return fromCwd;
  return path.resolve(repoRoot, p);
}

function parseArgs(argv) {
  let dist = path.join(repoRoot, "gui", "frontend", "dist");
  let binary = "";
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--dist" && argv[i + 1]) {
      dist = resolveUserPath(argv[++i]);
    } else if (arg === "--binary" && argv[i + 1]) {
      binary = resolveUserPath(argv[++i]);
    } else if (arg === "--help" || arg === "-h") {
      console.log(`Usage: node scripts/verify-frontend-welcome.mjs [--dist DIR] [--binary FILE]`);
      process.exit(0);
    }
  }
  return { dist, binary };
}

/** New welcome scenario copy + param dialog markers that must ship. */
const REQUIRED_MARKERS = [
  "Implement a feature",
  "Hotfix on the server",
  "welcome-prompt-param-overlay",
  "welcome-prompt-param-dialog",
];

/** Known retired welcome cards; presence means an old frontend was embedded. */
const FORBIDDEN_MARKERS = [
  "实现一个后台功能闭环",
  "定位并修复一个线上问题",
  "为项目补齐部署和环境说明",
  "接入一个第三方 API",
];

function walkFiles(dir, out = []) {
  if (!fs.existsSync(dir)) return out;
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) walkFiles(full, out);
    else out.push(full);
  }
  return out;
}

function readUtf8Safe(filePath) {
  try {
    return fs.readFileSync(filePath, "utf8");
  } catch {
    return "";
  }
}

function shouldScanDistFile(filePath) {
  // Skip huge third-party chunks (mermaid/katex/cytoscape) — welcome markers live
  // in app bundles (AIAssistantPanel / index), not vendor diagrams.
  const base = path.basename(filePath).toLowerCase();
  if (base.startsWith("mermaid.") || base.startsWith("katex") || base.startsWith("cytoscape")) {
    return false;
  }
  if (base.startsWith("diagram-") || base.includes("diagram-")) return false;
  return /\.(js|html|mjs)$/i.test(filePath);
}

function verifyDist(distDir) {
  const failures = [];
  if (!fs.existsSync(distDir)) {
    failures.push(`frontend dist missing: ${distDir}`);
    return failures;
  }
  const indexHtml = path.join(distDir, "index.html");
  if (!fs.existsSync(indexHtml)) {
    failures.push(`frontend dist missing index.html: ${indexHtml}`);
  }

  const files = walkFiles(distDir).filter(shouldScanDistFile);
  if (files.length === 0) {
    failures.push(`frontend dist has no scannable js/html assets under ${distDir}`);
    return failures;
  }

  // Per-file scan (no mega-string). Always scan every file for forbidden cards;
  // only required-marker lookup can stop early once all are found.
  const missing = new Set(REQUIRED_MARKERS);
  const forbiddenHits = new Set();
  for (const file of files) {
    const text = readUtf8Safe(file);
    if (!text) continue;
    if (missing.size > 0) {
      for (const marker of [...missing]) {
        if (text.includes(marker)) missing.delete(marker);
      }
    }
    for (const marker of FORBIDDEN_MARKERS) {
      if (!forbiddenHits.has(marker) && text.includes(marker)) {
        forbiddenHits.add(marker);
        failures.push(
          `frontend dist still contains retired welcome card: ${JSON.stringify(marker)} in ${path.relative(distDir, file)}`,
        );
      }
    }
  }
  for (const marker of missing) {
    failures.push(`frontend dist is missing required welcome marker: ${JSON.stringify(marker)}`);
  }
  return failures;
}

function bufferIncludesAscii(buf, ascii) {
  return buf.includes(Buffer.from(ascii, "utf8"));
}

function verifyBinary(binaryPath) {
  const failures = [];
  if (!binaryPath) return failures;
  if (!fs.existsSync(binaryPath)) {
    failures.push(`binary not found for welcome verification: ${binaryPath}`);
    return failures;
  }
  // Single Buffer scan (no latin1 string copy of ~80MB PE).
  const buf = fs.readFileSync(binaryPath);
  const binaryRequired = ["Implement a feature", "welcome-prompt-param-overlay"];
  for (const marker of binaryRequired) {
    if (!bufferIncludesAscii(buf, marker)) {
      failures.push(`binary ${path.basename(binaryPath)} missing embedded welcome marker: ${JSON.stringify(marker)}`);
    }
  }
  for (const marker of FORBIDDEN_MARKERS) {
    if (bufferIncludesAscii(buf, marker)) {
      failures.push(`binary ${path.basename(binaryPath)} still embeds retired welcome card: ${JSON.stringify(marker)}`);
    }
  }
  return failures;
}

const { dist, binary } = parseArgs(process.argv.slice(2));
// When only --binary is requested, skip dist checks unless dist exists (avoids
// false failures on runners that already cleaned frontend/dist after embed).
const checkDist = !binary || fs.existsSync(dist);
const failures = [
  ...(checkDist ? verifyDist(dist) : []),
  ...verifyBinary(binary),
];
if (failures.length > 0) {
  console.error("[verify-frontend-welcome] FAILED:");
  for (const f of failures) console.error(`  - ${f}`);
  console.error(
    "\nOEM brands (TigerClaw/MetaStaff) embed the same gui/frontend/dist as MaClaw.\n" +
      "Rebuild frontend with `npm run build` in gui/frontend, then rebuild the GUI binary.",
  );
  process.exit(1);
}
console.log(
  `[verify-frontend-welcome] OK${checkDist ? ` dist=${dist}` : ""}${binary ? ` binary=${binary}` : ""}`,
);
