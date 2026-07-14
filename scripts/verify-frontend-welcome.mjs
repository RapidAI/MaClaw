#!/usr/bin/env node
/**
 * Fail CI / local packaging if gui/frontend/dist (or a built GUI binary)
 * is missing the current AI assistant welcome page.
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
 *   node scripts/verify-frontend-welcome.mjs --source
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
  let checkSource = false;
  let distSpecified = false;
  for (let i = 0; i < argv.length; i++) {
    const arg = argv[i];
    if (arg === "--dist" && argv[i + 1]) {
      dist = resolveUserPath(argv[++i]);
      distSpecified = true;
    } else if (arg === "--binary" && argv[i + 1]) {
      binary = resolveUserPath(argv[++i]);
    } else if (arg === "--source") {
      checkSource = true;
    } else if (arg === "--help" || arg === "-h") {
      console.log(
        `Usage: node scripts/verify-frontend-welcome.mjs [--dist DIR] [--binary FILE] [--source]`,
      );
      process.exit(0);
    }
  }
  return { dist, binary, checkSource, distSpecified };
}

/** New welcome scenario copy + param dialog markers that must ship. */
const REQUIRED_MARKERS = [
  "Implement a feature",
  "Hotfix on the server",
  "welcome-prompt-param-overlay",
  "welcome-prompt-param-dialog",
];

/**
 * UI chrome markers that are only meaningful after the full welcome shell is
 * embedded into a GUI binary (not required in raw dist chunk scan alone —
 * they live in the same welcome module as the scenario cards).
 */
const BINARY_EXTRA_MARKERS = [
  "\u89d2\u8272", // 角色
  "\u526a\u8d34\u677f\u8bc6\u522b", // 剪贴板识别
];

/**
 * Retired welcome-card copy. It is intentionally retained only as a CI
 * regression signature: no production source or bundled asset may contain it.
 */
const FORBIDDEN_MARKERS = [
  "\u5b9e\u73b0\u4e00\u4e2a\u540e\u53f0\u529f\u80fd\u95ed\u73af",
  "\u5b9a\u4f4d\u5e76\u4fee\u590d\u4e00\u4e2a\u7ebf\u4e0a\u95ee\u9898",
  "\u4e3a\u9879\u76ee\u8865\u9f50\u90e8\u7f72\u548c\u73af\u5883\u8bf4\u660e",
  "\u63a5\u5165\u4e00\u4e2a\u7b2c\u4e09\u65b9 API",
];

/** Source files that must still carry the new welcome contract. */
const SOURCE_REQUIRED_FILES = [
  "gui/frontend/src/components/ai/welcomeScenarioTasks.ts",
  "gui/frontend/src/components/ai/WelcomePromptParamDialog.tsx",
  "gui/frontend/src/components/ai/AssistantWelcomeView.tsx",
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
  // Skip maps / source maps and pure CSS; markers are string literals in JS/HTML.
  if (/\.(map|css|svg|png|jpg|jpeg|webp|woff2?|ttf|eot)$/i.test(base)) return false;
  return /\.(js|html|mjs)$/i.test(filePath);
}

function verifySource() {
  const failures = [];
  const texts = [];
  for (const rel of SOURCE_REQUIRED_FILES) {
    const full = path.join(repoRoot, rel);
    if (!fs.existsSync(full)) {
      failures.push(`welcome source missing: ${rel}`);
      continue;
    }
    texts.push(readUtf8Safe(full));
  }
  const joined = texts.join("\n");
  for (const marker of REQUIRED_MARKERS) {
    if (!joined.includes(marker)) {
      failures.push(`welcome source is missing required marker: ${JSON.stringify(marker)}`);
    }
  }
  for (const marker of BINARY_EXTRA_MARKERS) {
    if (!joined.includes(marker)) {
      failures.push(`welcome source is missing binary chrome marker: ${JSON.stringify(marker)}`);
    }
  }
  for (const marker of FORBIDDEN_MARKERS) {
    if (joined.includes(marker)) {
      failures.push(`welcome source still contains retired welcome card: ${JSON.stringify(marker)}`);
    }
  }
  return failures;
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

function bufferIncludesUtf8(buf, text) {
  return buf.includes(Buffer.from(text, "utf8"));
}

function verifyBinary(binaryPath) {
  const failures = [];
  if (!binaryPath) return failures;
  if (!fs.existsSync(binaryPath)) {
    failures.push(`binary not found for welcome verification: ${binaryPath}`);
    return failures;
  }
  const st = fs.statSync(binaryPath);
  if (!st.isFile() || st.size < 1024 * 100) {
    failures.push(
      `binary ${path.basename(binaryPath)} is too small to be a GUI embed (${st.size} bytes)`,
    );
    return failures;
  }
  // Single Buffer scan (no latin1 string copy of ~80MB PE).
  const buf = fs.readFileSync(binaryPath);
  const binaryRequired = [
    // At least one scenario title + dialog shell + welcome chrome.
    REQUIRED_MARKERS[0],
    "welcome-prompt-param-overlay",
    ...BINARY_EXTRA_MARKERS,
  ];
  for (const marker of binaryRequired) {
    if (!bufferIncludesUtf8(buf, marker)) {
      failures.push(
        `binary ${path.basename(binaryPath)} missing embedded welcome marker: ${JSON.stringify(marker)}`,
      );
    }
  }
  for (const marker of FORBIDDEN_MARKERS) {
    if (bufferIncludesUtf8(buf, marker)) {
      failures.push(
        `binary ${path.basename(binaryPath)} still embeds retired welcome card: ${JSON.stringify(marker)}`,
      );
    }
  }
  return failures;
}

const { dist, binary, checkSource, distSpecified } = parseArgs(process.argv.slice(2));
// Dist is checked by default (no flags), when --dist is set, or when --binary
// is used and a dist dir still exists. `--source` alone is source-only.
const checkDist = binary
  ? fs.existsSync(dist)
  : distSpecified || !checkSource;
// Source contract is cheap and catches copy drift before/with embed checks.
const shouldCheckSource = checkSource || checkDist || !!binary;
const failures = [
  ...(shouldCheckSource ? verifySource() : []),
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
  `[verify-frontend-welcome] OK` +
    `${shouldCheckSource ? " source" : ""}` +
    `${checkDist ? ` dist=${dist}` : ""}` +
    `${binary ? ` binary=${binary}` : ""}`,
);
