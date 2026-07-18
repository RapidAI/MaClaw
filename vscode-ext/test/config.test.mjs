/**
 * maclawConfig unit test: provider list read, byte-preserving current-provider
 * switch, ambiguous-key fallback, escaping, BOM/CRLF, atomic write.
 * Run: npm run build && node test/config.test.mjs
 */
import assert from "node:assert/strict";
import * as fs from "fs";
import * as os from "os";
import * as path from "path";
import { readMaclawLLMConfig, writeCurrentProvider } from "./out/maclawConfig.cjs";

const dir = fs.mkdtempSync(path.join(os.tmpdir(), "maclaw-cfg-"));
const file = path.join(dir, "config.json");
process.env.MACLAW_VSEXT_CONFIG = file;

let failed = false;
const check = (name, fn) => {
  try {
    fn();
    console.log(`  ok ${name}`);
  } catch (err) {
    failed = true;
    console.error(`  FAIL ${name}:`, err);
  }
};

// --- 1) read + byte-preserving switch (name contains "$") -------------------
const original = `{
  "thirdparty_gateway_enabled": true,
  "maclaw_llm_providers": [
    {"name": "深度求索", "url": "https://api.deepseek.com", "key": "sk-secret", "model": "deepseek-chat"},
    {"name": "Kimi $Pro", "url": "https://api.moonshot.cn", "key": "sk-other", "model": "kimi-k2"}
  ],
  "maclaw_llm_current_provider": "深度求索",
  "working_directory": "D:\\\\work"
}
`;

check("read providers + current", () => {
  fs.writeFileSync(file, original, "utf8");
  const state = readMaclawLLMConfig();
  assert.equal(state.providers.length, 2);
  assert.equal(state.providers[0].name, "深度求索");
  assert.equal(state.providers[1].model, "kimi-k2");
  assert.equal(state.current, "深度求索");
});

check("byte-preserving switch ($ in name, secrets untouched)", () => {
  fs.writeFileSync(file, original, "utf8");
  writeCurrentProvider("Kimi $Pro");
  const after = fs.readFileSync(file, "utf8");
  assert.equal(readMaclawLLMConfig().current, "Kimi $Pro");
  assert.ok(after.includes('"key": "sk-secret"'));
  assert.equal(
    after.replace('"maclaw_llm_current_provider": "Kimi $Pro"', '"maclaw_llm_current_provider": "深度求索"'),
    original,
    "only the current-provider value changed"
  );
});

// --- 2) escaping -------------------------------------------------------------
check("escaped quotes in name stay valid JSON", () => {
  fs.writeFileSync(file, original, "utf8");
  writeCurrentProvider('Kimi "X"');
  const parsed = JSON.parse(fs.readFileSync(file, "utf8"));
  assert.equal(parsed.maclaw_llm_current_provider, 'Kimi "X"');
});

check("existing escaped value matches exactly once", () => {
  const withEscaped = original.replace('"深度求索",\n  "working', '"Kimi \\"Pro\\"",\n  "working');
  fs.writeFileSync(file, withEscaped, "utf8");
  writeCurrentProvider("深度求索");
  assert.equal(readMaclawLLMConfig().current, "深度求索");
  // Escaped-value case also took the byte-preserving path: swap back → identical.
  const after = fs.readFileSync(file, "utf8");
  assert.equal(
    after.replace('"maclaw_llm_current_provider": "深度求索"', '"maclaw_llm_current_provider": "Kimi \\"Pro\\""'),
    withEscaped
  );
});

// --- 3) ambiguous key → full rewrite fallback --------------------------------
check("bait key text in another value → fallback rewrite (bait preserved)", () => {
  const baited = `{
  "notes": "see \\"maclaw_llm_current_provider\\": \\"bait\\" docs",
  "maclaw_llm_current_provider": "深度求索",
  "maclaw_llm_providers": [{"name": "深度求索", "model": "m", "url": "u"}]
}
`;
  fs.writeFileSync(file, baited, "utf8");
  writeCurrentProvider("Other");
  const parsed = JSON.parse(fs.readFileSync(file, "utf8"));
  assert.equal(parsed.maclaw_llm_current_provider, "Other");
  assert.ok(parsed.notes.includes("bait"), "bait value untouched");
});

check("duplicate key → fallback rewrite", () => {
  const dup = `{"maclaw_llm_current_provider": "A", "maclaw_llm_current_provider": "B", "x": 1}\n`;
  fs.writeFileSync(file, dup, "utf8");
  writeCurrentProvider("深度求索");
  const parsed = JSON.parse(fs.readFileSync(file, "utf8"));
  assert.equal(parsed.maclaw_llm_current_provider, "深度求索");
  assert.equal(parsed.x, 1);
});

check("absent key → added by rewrite", () => {
  fs.writeFileSync(file, '{"a": 1}\n', "utf8");
  writeCurrentProvider("深度求索");
  assert.equal(readMaclawLLMConfig().current, "深度求索");
  assert.equal(JSON.parse(fs.readFileSync(file, "utf8")).a, 1);
});

// --- 4) encodings ------------------------------------------------------------
check("BOM: read parses, write preserves BOM", () => {
  const bom = String.fromCharCode(0xfeff) + original;
  fs.writeFileSync(file, bom, "utf8");
  assert.equal(readMaclawLLMConfig().current, "深度求索");
  writeCurrentProvider("Kimi $Pro");
  const after = fs.readFileSync(file, "utf8");
  assert.equal(after.charCodeAt(0), 0xfeff, "BOM preserved");
  assert.equal(readMaclawLLMConfig().current, "Kimi $Pro");
});

check("CRLF round-trip preserved", () => {
  const crlf = original.replace(/\n/g, "\r\n");
  fs.writeFileSync(file, crlf, "utf8");
  writeCurrentProvider("Kimi $Pro");
  const after = fs.readFileSync(file, "utf8");
  assert.ok(after.includes("\r\n"), "CRLF preserved");
  assert.equal(
    after.replace('"maclaw_llm_current_provider": "Kimi $Pro"', '"maclaw_llm_current_provider": "深度求索"'),
    crlf
  );
});

// --- 5) misc ------------------------------------------------------------------
check("duplicate provider names deduped (first wins)", () => {
  fs.writeFileSync(file, original.replace('"Kimi $Pro"', '"深度求索"'), "utf8");
  const state = readMaclawLLMConfig();
  assert.equal(state.providers.length, 1);
});

check("empty name rejected", () => {
  assert.throws(() => writeCurrentProvider(""), /empty/);
});

check("unreadable config → undefined (not throw)", () => {
  fs.rmSync(file, { force: true });
  assert.equal(readMaclawLLMConfig(), undefined);
});

fs.rmSync(dir, { recursive: true, force: true });
if (failed) {
  process.exit(1);
}
console.log("[config-test] OK — all cases passed");
